package daemon

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"herdr-voice-computer/internal/herdr"
	"herdr-voice-computer/internal/index"
	"herdr-voice-computer/internal/intent"
	"herdr-voice-computer/internal/memory"
	"herdr-voice-computer/internal/shepherdr"
)

// Delegation: "ask the homelab coordinator to investigate that issue we discussed yesterday" finds the
// notes (and turns) the owner means, drafts a brief with them, asks, and sends it through the
// coordinator's Shepherdr chat once the owner says yes.

var refRE = regexp.MustCompile(`(?i)\b(that|this|the) (issue|problem|bug|thing|idea|plan|error|crash|question|stuff|note)\b|\bwe (discussed|talked about|spoke about|mentioned)\b|\bi (noted|wrote|mentioned|said)\b|\bfrom my notes\b|\b(yesterday|this morning|earlier today|last week|last night)\b`)

// dateRange is the days a reference points at ("yesterday", "this morning", "last week").
func dateRange(text string, now time.Time) (from, to time.Time, ok bool) {
	t := strings.ToLower(text)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case strings.Contains(t, "yesterday"), strings.Contains(t, "last night"):
		return day.AddDate(0, 0, -1), day, true
	case strings.Contains(t, "this morning"), strings.Contains(t, "earlier today"), strings.Contains(t, " today"):
		return day, day.AddDate(0, 0, 1), true
	case strings.Contains(t, "last week"):
		return day.AddDate(0, 0, -7), day, true
	}
	return time.Time{}, time.Time{}, false
}

// briefContext is what the owner's reference means: up to 3 note lines ("2026-11-03 21:40 text") and the
// conversation's best match; refs names the notes for the daily note. need says the request refers to
// something but nothing matched (ask back instead of sending something vague).
func (d *Daemon) briefContext(ctx context.Context, request string, now time.Time) (lines []string, convo string, refs []string, need bool) {
	if !refRE.MatchString(request) {
		return nil, "", nil, false
	}
	from, to, dated := dateRange(request, now)
	if d.index != nil {
		d.syncIndex()
		hits, _ := d.index.Search(ctx, request, 10, 0.3)
		style := map[string]bool{}
		for _, x := range d.mem.Items(memory.Style) {
			style[x] = true
		}
		type scored struct {
			h index.Hit
			s float64
		}
		var best []scored
		for _, h := range hits {
			if strings.HasPrefix(h.Source, "http") || style[h.Text] || memory.IsSecret(h.Text) || h.Source == "conversation" {
				continue
			}
			s := h.Score
			if dated {
				if t, err := time.ParseInLocation("2006-01-02", h.Date, now.Location()); err == nil && !t.Before(from) && t.Before(to) {
					s += 0.15
				} else {
					s -= 0.1
				}
			}
			if s >= 0.5 {
				best = append(best, scored{h, s})
			}
		}
		if dated { // "yesterday": when a note from then matches, only those
			var in []scored
			for _, b := range best {
				if t, err := time.ParseInLocation("2006-01-02", b.h.Date, now.Location()); err == nil && !t.Before(from) && t.Before(to) {
					in = append(in, b)
				}
			}
			if len(in) > 0 {
				best = in
			}
		}
		for i := 1; i < len(best); i++ { // by adjusted score
			for j := i; j > 0 && best[j].s > best[j-1].s; j-- {
				best[j], best[j-1] = best[j-1], best[j]
			}
		}
		for i, b := range best {
			if i == 3 {
				break
			}
			when := b.h.Date
			text := strings.TrimSpace(b.h.Text)
			if b.h.Source == "memory" {
				lines = append(lines, "- (what "+d.cfg.Owner+" told me) "+text)
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s %s", when, text))
			ref := when
			if m := timeLead.FindString(text); m != "" {
				ref += " " + strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(m, "[ ] "), "[x] "))
			}
			refs = append(refs, ref)
		}
	}
	// The conversation: the earlier turn that best shares the request's words.
	q := herdr.Words(refRE.ReplaceAllString(request, " "))
	d.mu.Lock()
	turns := append([]Turn{}, d.turns...)
	d.mu.Unlock()
	bestScore := 0.0
	for _, t := range turns {
		if len(q) == 0 || memory.IsSecret(t.You) {
			continue
		}
		w := herdr.Words(t.You)
		hits := 0
		for _, x := range q {
			for _, y := range w {
				if len(x) > 3 && x == y {
					hits++
					break
				}
			}
		}
		if s := float64(hits) / float64(len(q)); s > bestScore && s >= 0.4 {
			bestScore, convo = s, oneLine(t.You, 300)
		}
	}
	return lines, convo, refs, len(lines) == 0 && convo == ""
}

// draft is the brief the coordinator gets: the request in the owner's words, the matched notes word for
// word, the conversation's line, and who sent it (under about 1,500 characters).
func (d *Daemon) draft(request string, lines []string, convo string) string {
	owner := d.cfg.Owner
	if owner == "" {
		owner = "the owner"
	}
	var b strings.Builder
	b.WriteString(capFirst(strings.TrimRight(oneLine(request, 400), ".!")) + ".")
	if len(lines) > 0 {
		b.WriteString("\n\nContext from " + owner + "'s notes:\n" + strings.Join(lines, "\n"))
	}
	if convo != "" {
		b.WriteString("\n\nFrom our conversation: \"" + convo + "\"")
	}
	sig := "\n\nSent by " + owner + "'s voice assistant."
	out := b.String()
	if len(out)+len(sig) > 1500 {
		out = out[:1500-len(sig)-1] + "…"
	}
	return out + sig
}

func (d *Daemon) planDelegate(ag herdr.Agent, chat string, src *shepherdr.Client, request string) *action {
	label := ag.Label()
	if ag.TabIndex == 0 || strings.EqualFold(ag.Tab, "coordinator") {
		label += " coordinator"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	lines, convo, refs, need := d.briefContext(ctx, request, time.Now())
	cancel()
	if need {
		_, _, dated := dateRange(request, time.Now())
		key := "delegate.unknown"
		if dated {
			key = "delegate.unknown.dated"
		}
		return &action{out: both("found no note it refers to; asked what it was", d.phrase(key, map[string]string{"when": dayHint(request)}))}
	}
	text := d.draft(request, lines, convo)
	ref := ""
	if len(refs) > 0 {
		ref = " (from note " + strings.Join(refs, ", ") + ")"
	}
	short := strings.TrimRight(oneLine(request, 120), ".!?")
	a := runner{func(ctx context.Context, words string) (string, []Done, error) {
		if words == "" {
			words = text
		}
		if err := src.Send(ctx, chat, words, fmt.Sprintf("hvc%d", time.Now().UnixMilli())); err != nil {
			return "", nil, err
		}
		d.desk.Note(time.Now(), "→ "+label+": "+short+ref)
		go d.syncIndex()
		return both("sent the brief to the "+label, d.phrase("delegate.sent", map[string]string{"label": label})), []Done{{Text: "Sent to the " + label}}, nil
	}}.act("Ask the "+label, chat, "Send")
	a.card.Text = text
	with := ""
	switch {
	case len(refs) == 1:
		with = d.phrase("delegate.with1", map[string]string{"when": dayWords(strings.Fields(refs[0])[0])})
	case len(refs) > 1:
		with = " with your notes on it"
	case convo != "":
		with = " with what you said earlier"
	}
	a.card.Ask = d.phrase("delegate.ask", map[string]string{"label": label, "request": lowerFirst(short), "with": with})
	a.declined = d.phrase("send.declined", nil)
	a.minTier = intent.Tier1
	return a
}

func dayHint(text string) string {
	for _, w := range []string{"yesterday", "this morning", "earlier today", "last week", "last night", "today"} {
		if strings.Contains(strings.ToLower(text), w) {
			return w
		}
	}
	return ""
}

// EditCard is "change it to …": the open brief's request becomes the new words (its context stays);
// another card's text becomes them. The question is asked again.
func (d *Daemon) EditCard(words string) error {
	d.mu.Lock()
	_, c := d.openCardLocked()
	if c == nil {
		d.mu.Unlock()
		return ErrNoCard
	}
	old := c.card.Text
	parts := strings.SplitN(old, "\n\n", 2)
	nw := capFirst(strings.TrimRight(oneLine(words, 400), ".!")) + "."
	if len(parts) == 2 && strings.Contains(old, "voice assistant.") {
		c.card.Text = nw + "\n\n" + parts[1]
	} else {
		c.card.Text = strings.TrimRight(oneLine(words, 2000), ".!") + "."
	}
	c.card.Ask = d.phrase("delegate.edited", map[string]string{"text": strings.TrimRight(nw, ".")})
	c.card.Expires = time.Now().Add(d.cfg.CardTimeout()).UnixMilli()
	c.timer.Reset(d.cfg.CardTimeout())
	cp := *c.card
	d.st.Card = &cp
	d.st.Answer = cp.Ask + " Say yes or no."
	d.publishLocked()
	ask := d.st.Answer
	d.mu.Unlock()
	go d.Say(ask)
	return nil
}

func init() {
	phrases["delegate.ask"] = []string{"I'll ask the {label} to {request}{with}. Send it?", "Shall I brief the {label} to {request}{with}?", "Okay: the {label} gets asked to {request}{with}. Send?"}
	phrases["delegate.with1"] = []string{", with your note from {when}", ", including your note from {when}"}
	phrases["delegate.sent"] = []string{"Sent to the {label}.", "Done, the {label} has the brief.", "Brief's with the {label} now."}
	phrases["delegate.edited"] = []string{"Changed it to: {text}. Send it now?", "Okay, now it says: {text}. Send?"}
	phrases["delegate.unknown"] = []string{"I don't see a note about that. What was it about?", "I'm not sure which one you mean. Can you give me a word or two about it?"}
	phrases["delegate.unknown.dated"] = []string{"I don't see a note about that from {when}. What was it about?", "Nothing in your notes from {when} matches. Remind me what it was?"}
}
