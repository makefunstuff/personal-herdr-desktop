package daemon

// Thinking before a brain turn. LFM3-Omni closes its <think> block at once on its own, so vcd asks it to
// think in a short request of its own (a started thought it has to finish), then gives the conclusion to
// the turn as a private note. Nothing of it is spoken.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"herdr-voice-computer/internal/intent"
)

const thinkPrompt = `You are %s's voice assistant, thinking privately before you answer out loud. In two or three short ` +
	`sentences work out: what %s actually wants, whether one of your tools does it (and with which argument, taken ` +
	`from the context or their words, never invented), or what a warm, useful spoken answer should say. Note anything ` +
	`from earlier in the conversation or from the notes that matters. Your tools: %s.`

// think returns the brain's private reasoning about the owner's words, or "" (off, too short, or slow).
func (d *Daemon) think(ctx context.Context, line, text string, recent []Turn) string {
	if !d.cfg.Think || len(strings.Fields(text)) < 3 {
		return ""
	}
	var names []string
	for _, t := range intent.Catalogue() {
		names = append(names, t.Name)
	}
	var b strings.Builder
	for _, t := range recent {
		if t.You != "" {
			fmt.Fprintf(&b, "%s said: %s\n", d.cfg.Owner, t.You)
		}
		if t.Computer != "" {
			fmt.Fprintf(&b, "You said: %s\n", t.Computer)
		}
	}
	if line != "" {
		fmt.Fprintf(&b, "Context: %s\n", line)
	}
	fmt.Fprintf(&b, "%s now says: %s", d.cfg.Owner, text)
	body := map[string]any{"model": d.quickModel(), "stream": false, "max_tokens": 80, "temperature": 0.3,
		"stop": []string{"</think>"}, "chat_template_kwargs": map[string]any{"enable_thinking": true},
		"messages": []any{
			map[string]any{"role": "system", "content": fmt.Sprintf(thinkPrompt, d.cfg.Owner, d.cfg.Owner, strings.Join(names, ", "))},
			map[string]any{"role": "user", "content": b.String()},
			map[string]any{"role": "assistant", "content": "<think>\nLet me think. " + d.cfg.Owner + " wants"},
		}}
	raw, _ := json.Marshal(body)
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	t0 := time.Now()
	resp, err := d.voice.Post(cctx, d.cfg.Chat, "/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(resp, &out) != nil || len(out.Choices) == 0 {
		return ""
	}
	th := thoughtOf(out.Choices[0].Message.Content, d.cfg.Owner)
	lb, _ := json.Marshal(map[string]any{"event": "think", "ms": time.Since(t0).Milliseconds(), "thought": oneLine(th, 240)})
	d.log.Println(string(lb))
	return th
}

// thoughtOf is the thought alone: the started words put back, tags and a cut-off last sentence dropped.
func thoughtOf(content, owner string) string {
	s := content
	if i := strings.Index(s, "<think>"); i >= 0 {
		s = s[i+len("<think>"):]
	}
	if i := strings.Index(s, "</think>"); i >= 0 {
		s = s[:i]
	}
	s = strings.Join(strings.Fields(s), " ")
	if !strings.HasPrefix(s, "Let me think.") {
		s = "Let me think. " + owner + " wants " + s
	}
	s = strings.TrimSpace(strings.TrimPrefix(s, "Let me think."))
	if i := strings.LastIndexAny(s, ".!?"); i > 40 && i < len(s)-1 {
		s = s[:i+1]
	}
	return s
}
