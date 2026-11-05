package daemon

// The persona grows from talking. Every personaEvery brain turns, in the background, the brain rereads the
// recent conversation and its current persona and rewrites persona.md: a few lines on who it is with the
// owner (its tone with them, running jokes, what lands and what doesn't, how they're doing lately). The file
// joins the standing instructions of the next session. It's the owner's to read and edit; facts about them
// stay in memory.md, and nothing here may contradict the honesty rules.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"herdr-voice-computer/internal/config"
	"herdr-voice-computer/internal/speech"
)

const personaEvery = 12

const personaPrompt = `You are %[2]s, %[1]s's voice assistant, reflecting privately on how you two get along. From ` +
	`your current persona notes and your recent conversation, rewrite the notes: at most eight short lines on who ` +
	`you are with %[1]s. Your tone with them, running jokes or nicknames that came up, what they respond well to and ` +
	`what annoys them, topics they enjoy, how they've been doing lately. Keep what still holds, change what the ` +
	`conversation shows was off, add what's new; drop anything you can't ground in what was said. Never invent ` +
	`facts about them, never note secrets, and never give yourself rules that break honesty. Write in the first ` +
	`person ("I tease %[1]s a little about…"), plain lines starting with "- ", nothing else.`

func personaPath() string { return filepath.Join(config.DataDir(), "persona.md") }

// personaNotes is the persona as it stands ("" before the first reflection).
func personaNotes() string {
	b, err := os.ReadFile(personaPath())
	if err != nil {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "- ") {
			lines = append(lines, strings.TrimSpace(l))
		}
	}
	return strings.Join(lines, "\n")
}

// evolvePersona rereads the last turns and rewrites persona.md.
func (d *Daemon) evolvePersona() {
	if !d.cfg.Learn || d.convo == nil {
		return
	}
	turns := d.convo.Last(40)
	var b strings.Builder
	for _, t := range turns {
		if t.You != "" {
			fmt.Fprintf(&b, "%s: %s\n", d.cfg.Owner, t.You)
		}
		if t.Computer != "" {
			fmt.Fprintf(&b, "You: %s\n", t.Computer)
		}
	}
	if b.Len() < 200 {
		return
	}
	cur := personaNotes()
	if cur == "" {
		cur = "(none yet: this is your first reflection)"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	body := map[string]any{"model": d.cfg.Chat.Model, "stream": false, "max_tokens": 1500,
		"messages": []any{
			map[string]any{"role": "system", "content": fmt.Sprintf(personaPrompt, d.cfg.Owner, d.cfg.Name)},
			map[string]any{"role": "user", "content": "Your persona notes now:\n" + cur + "\n\nYour recent conversation:\n" + b.String()},
		}}
	raw, _ := json.Marshal(body)
	resp, err := d.voice.Post(ctx, d.cfg.Chat, "/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(resp, &out) != nil || len(out.Choices) == 0 {
		return
	}
	var lines []string
	for _, l := range strings.Split(thinkTagRE.ReplaceAllString(out.Choices[0].Message.Content, " "), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "- ") && len(lines) < 8 {
			lines = append(lines, "- "+speech.Plain(strings.TrimPrefix(l, "- ")))
		}
	}
	if len(lines) == 0 {
		return
	}
	text := "# Who I am with " + d.cfg.Owner + "\n\n<!-- Rewritten from our talks every few turns. Yours to read and edit. -->\n\n" + strings.Join(lines, "\n") + "\n"
	if old, _ := os.ReadFile(personaPath()); len(old) > 0 {
		_ = os.WriteFile(personaPath()+".prev", old, 0o600)
	}
	if err := os.WriteFile(personaPath(), []byte(text), 0o600); err != nil {
		return
	}
	lb, _ := json.Marshal(map[string]any{"event": "persona", "lines": len(lines)})
	d.log.Println(string(lb))
	d.mu.Lock()
	d.memStale = true // the next session carries it
	d.mu.Unlock()
}
