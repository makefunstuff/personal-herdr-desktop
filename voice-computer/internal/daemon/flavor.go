package daemon

// Flavor: what vcd says on its own (an agent's news, a reminder, a task done, a command's outcome) is said
// in the secretary's voice, never the same way twice: dry one time, a joke, a light tease or a fun remark
// another. Every fact, name, number and id stays as it was; the notification keeps the plain words. A slow
// or failed rewrite says the plain words. Off with "flavor": false.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"herdr-voice-computer/internal/speech"
)

const flavorPrompt = `You are %[2]s, %[1]s's voice assistant, with a cheeky, unpredictable streak. Say the line below out ` +
	`loud your own way. Keep every fact, name, number, task id and agent name exactly as given, and keep its meaning. ` +
	`Surprise %[1]s: sometimes dry, sometimes playful, a quick joke, a light tease or a fun aside, now and then just ` +
	`plain; never start the way you started recently. One or two short spoken sentences (three only if the line needs ` +
	`them). No markdown, no emoji, no quotes around it, nothing but the words to say.`

var flavorMu sync.Mutex
var recentOpenings []string

// flavorModel: the cloud brain's quick model where there is one (DeepSeek's chat), else the brain's.
func (d *Daemon) flavorModel() string {
	if d.cfg.FlavorModel != "" {
		return d.cfg.FlavorModel
	}
	if strings.Contains(d.cfg.Chat.URL, "deepseek.com") {
		return "deepseek-chat"
	}
	return d.cfg.Chat.Model
}

// flavor is text said the secretary's way, or text itself (off, local brain, slow, or a rewrite that lost a
// number). wait bounds it: a command's outcome can't wait long.
func (d *Daemon) flavor(text string, wait time.Duration) string {
	text = strings.TrimSpace(text)
	if !d.cfg.Flavor || text == "" || d.localBrain() {
		return text
	}
	flavorMu.Lock()
	avoid := strings.Join(recentOpenings, " | ")
	flavorMu.Unlock()
	persona := personaNotes()
	user := "Line to say: " + text
	if avoid != "" {
		user += "\nYour recent openings (don't reuse): " + avoid
	}
	if persona != "" {
		user += "\nWho you are with them:\n" + persona
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	body := map[string]any{"model": d.flavorModel(), "stream": false, "max_tokens": 120, "temperature": 1.2,
		"messages": []any{
			map[string]any{"role": "system", "content": fmt.Sprintf(flavorPrompt, d.cfg.Owner, d.cfg.Name)},
			map[string]any{"role": "user", "content": user},
		}}
	raw, _ := json.Marshal(body)
	resp, err := d.voice.Post(ctx, d.cfg.Chat, "/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return text
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(resp, &out) != nil || len(out.Choices) == 0 {
		return text
	}
	said := strings.Trim(speech.Plain(thinkTagRE.ReplaceAllString(out.Choices[0].Message.Content, " ")), "\"' ")
	if said == "" || !keepsFigures(text, said) {
		return text
	}
	flavorMu.Lock()
	if f := strings.Fields(said); len(f) > 0 {
		recentOpenings = append(recentOpenings, strings.Join(f[:min(4, len(f))], " "))
		if len(recentOpenings) > 6 {
			recentOpenings = recentOpenings[1:]
		}
	}
	flavorMu.Unlock()
	return said
}

// keepsFigures: every number and id-like token of the plain line is still in the rewrite.
func keepsFigures(plain, said string) bool {
	for _, w := range strings.Fields(plain) {
		w = strings.Trim(w, ".,:;!?()\"'")
		if w != "" && strings.ContainsAny(w, "0123456789") && !strings.Contains(said, w) {
			return false
		}
	}
	return true
}

// sometimes is true about one time in n: a command's outcome is flavored now and then, not always.
func sometimes(n int) bool { return rand.Intn(n) == 0 }
