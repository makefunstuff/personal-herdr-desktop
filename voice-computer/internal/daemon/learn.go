package daemon

// Learning from a conversation: after a brain turn, the local brain is asked (in a request of its own,
// after the answer was spoken) whether the owner said something lasting about themselves or about how the
// secretary should talk. What it finds goes to memory.md, the same file "remember" writes, so the
// secretary's picture of the owner and its manner grow from talking, not only from explicit commands.
// Only what the owner's own words carry is kept: a note whose words aren't mostly in what they said is
// dropped (a small model invents), and so is anything that looks like a secret.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"herdr-voice-computer/internal/memory"
)

const learnPrompt = `You keep a voice assistant's memory of its owner, %[1]s. From %[1]s's latest words only, list what ` +
	`is worth remembering for weeks: facts about %[1]s (their work, projects, people, tastes, plans, routines, ` +
	`feelings that last) and how %[1]s wants the assistant to talk or behave. Write each as one short third-person ` +
	`sentence using %[1]s's own words ("%[1]s's main project is homelab", "%[1]s likes short answers in the morning"). ` +
	`Skip passing requests, commands to the computer, questions, small talk, what they're doing right now (which ` +
	`space, what they're looking at), complaints about you, and anything already remembered. Most turns hold nothing: ` +
	`then answer with empty lists. ` +
	`Never keep passwords, keys or secrets. Answer with JSON only: {"facts": [...], "style": [...]}, empty lists when ` +
	`there is nothing.`

var learnWordRE = regexp.MustCompile(`[a-z0-9]+`)

var learnStop = map[string]bool{"the": true, "a": true, "an": true, "and": true, "or": true, "to": true, "of": true,
	"in": true, "on": true, "is": true, "are": true, "be": true, "for": true, "with": true, "that": true, "it": true,
	"their": true, "they": true, "them": true, "s": true, "likes": true, "wants": true, "prefers": true, "has": true,
	"his": true, "her": true, "assistant": true, "answers": true, "when": true, "at": true, "by": true}

// grounded says whether most of a note's words come from what the owner said (their name aside).
func grounded(note, said, owner string) bool {
	have := map[string]bool{}
	for _, w := range learnWordRE.FindAllString(strings.ToLower(said), -1) {
		have[strings.TrimSuffix(w, "s")] = true
	}
	own := strings.ToLower(owner)
	total, hit := 0, 0
	for _, w := range learnWordRE.FindAllString(strings.ToLower(note), -1) {
		if learnStop[w] || w == own || len(w) < 3 {
			continue
		}
		total++
		if have[strings.TrimSuffix(w, "s")] {
			hit++
		}
	}
	return total > 0 && hit*3 >= total*2
}

// learn runs after a brain turn's answer: what the owner's words hold that lasts goes to memory.
func (d *Daemon) learn(you string) {
	if !d.cfg.Learn || d.mem == nil || len(strings.Fields(you)) < 4 || memory.IsSecret(you) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	body := map[string]any{"model": d.cfg.Chat.Model, "stream": false, "max_tokens": 160, "temperature": 0.1,
		"messages": []any{
			map[string]any{"role": "system", "content": fmt.Sprintf(learnPrompt, d.cfg.Owner)},
			map[string]any{"role": "user", "content": "Already remembered (never repeat these, even reworded):\n" + knownNotes(d.mem) + "\n\n" + d.cfg.Owner + " said: " + you},
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
	found := parseLearned(out.Choices[0].Message.Content)
	var kept []string
	for kind, notes := range map[string][]string{memory.Fact: found.Facts, memory.Style: found.Style} {
		for i, n := range notes {
			if i >= 2 || !grounded(n, you, d.cfg.Owner) || passing(n, d.cfg.Owner) {
				continue
			}
			if kind == memory.Fact && aboutManner.MatchString(n) {
				kind = memory.Style // "likes it when you joke": how to talk, not a fact
			}
			if k, err := d.mem.Add(kind, n); err == nil {
				kept = append(kept, kind+": "+k)
			}
		}
	}
	if len(kept) > 0 {
		lb, _ := json.Marshal(map[string]any{"event": "learned", "notes": kept})
		d.log.Println(string(lb))
		d.mu.Lock()
		d.memStale = true // the next turn's session carries it
		d.mu.Unlock()
	}
}

type learned struct {
	Facts []string `json:"facts"`
	Style []string `json:"style"`
}

// parseLearned reads the JSON object in a reply (a fence or words around it allowed).
func parseLearned(s string) learned {
	s = thinkTagRE.ReplaceAllString(s, " ")
	var l learned
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		_ = json.Unmarshal([]byte(s[i:j+1]), &l)
	}
	return l
}

// aboutManner: a note about how the assistant should talk or act.
var aboutManner = regexp.MustCompile(`(?i)\b(you|your|the assistant)\b`)

// passingRE: what holds for today only, or a quote rather than a note.
var passingRE = regexp.MustCompile(`(?i)\b(today|tonight|this evening|this morning|right now|at the moment|currently|just now|all evening|all day)\b`)

// passing says whether a note is today's state ("Alex is tired today") or a quote ("Alex said …"), not
// something that lasts.
func passing(note, owner string) bool {
	return passingRE.MatchString(note) || strings.HasPrefix(strings.ToLower(note), strings.ToLower(owner)+" said")
}

// knownNotes is what memory already holds, a line each ("(nothing yet)" when empty).
func knownNotes(m *memory.Memory) string {
	var b strings.Builder
	for _, k := range []string{memory.Fact, memory.Style, memory.Correction} {
		for _, x := range m.Items(k) {
			b.WriteString("- " + x + "\n")
		}
	}
	if b.Len() == 0 {
		return "(nothing yet)"
	}
	return strings.TrimSpace(b.String())
}
