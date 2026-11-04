package daemon

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"herdr-voice-computer/internal/intent"
	"herdr-voice-computer/internal/memory"
)

// ownerWords turns the owner's own "I", "my", "me" into their name, so a note reads right to the brain
// ("my main project is homelab" is "Alex's main project is homelab").
func (d *Daemon) ownerWords(text string) string {
	o := d.cfg.Owner
	if o == "" {
		o = "The owner"
	}
	for _, r := range []struct{ re, to string }{
		{`(?i)\bI am\b|\bI'm\b`, o + " is"}, {`(?i)\bI've\b`, o + " has"}, {`(?i)\bI\b`, o},
		{`(?i)\bmy\b`, o + "'s"}, {`(?i)\bmine\b`, o + "'s"}, {`(?i)\bme\b`, o},
	} {
		text = regexp.MustCompile(r.re).ReplaceAllString(text, r.to)
	}
	return text
}

// styleAcks are the acknowledgments for the style notes the fast grammar makes.
var styleAcks = map[string][]string{
	"Keep answers very short.":              {"Got it, shorter from now on.", "Sure, I'll keep it brief."},
	"A light joke now and then is welcome.": {"Noted, I'll lighten up.", "Ha, okay, I'll loosen up a bit."},
	"Don't use the owner's name.":           {"Okay, no more names.", "Got it, I'll leave your name out."},
	"Ask fewer follow-up questions.":        {"Got it, fewer questions.", "Sure, I'll ask less."},
	"Be a bit more formal.":                 {"Certainly. I'll keep it more formal."},
	"Be casual and relaxed.":                {"Sure thing, I'll keep it relaxed."},
}

func (d *Daemon) planRemember(c intent.Call) *action {
	kind, text, ack := c.Str("kind"), c.Str("text"), c.Str("ack")
	if text == "" {
		return notDone("what should I remember? Ask the owner.")
	}
	return runner{func(ctx context.Context, _ string) (string, []Done, error) {
		if kind == memory.Fact || kind == "" {
			text = d.ownerWords(text)
		}
		kept, err := d.mem.Add(kind, text)
		if errors.Is(err, memory.ErrSecret) {
			return both("refused: it looks like a secret", d.phrase("remember.secret", nil)), nil, nil
		}
		if err != nil {
			return "", nil, err
		}
		d.mu.Lock()
		d.memStale = true
		d.mu.Unlock()
		say := ""
		switch {
		case ack != "":
			say = d.fill(ack, nil)
		case kind == memory.Style:
			if _, ok := styleAcks[kept]; ok {
				say = d.phrase("ack:"+kept, nil)
			} else {
				say = d.phrase("remember.style", nil)
			}
		case kind == memory.Correction:
			say = d.phrase("remember.fix", nil)
		default:
			say = d.phrase("remember.fact", nil)
		}
		return both("remembered: "+kept, say), []Done{{Text: "Remembered: " + kept}}, nil
	}}.act("Remember", "", "Remember")
}

func (d *Daemon) planForget(c intent.Call) *action {
	text := c.Str("text")
	return tell(func(ctx context.Context) (string, string) {
		gone, err := d.mem.Forget(d.ownerWords(text))
		if gone == "" && err == nil {
			gone, err = d.mem.Forget(text)
		}
		if err != nil || gone == "" {
			return "found nothing like it", d.phrase("forget.none", nil)
		}
		d.mu.Lock()
		d.memStale = true
		d.mu.Unlock()
		return "forgot: " + gone, d.phrase("forget.done", nil)
	})
}

// recall says what it knows of the owner: a few facts, then how they like it to talk.
func (d *Daemon) recall() (string, string) {
	d.mem.Reload()
	facts, style := d.mem.Items(memory.Fact), d.mem.Items(memory.Style)
	if len(facts) == 0 && len(style) == 0 {
		return "told the owner it remembers nothing yet", d.phrase("recall.none", nil)
	}
	var parts []string
	if len(facts) > 3 {
		facts = facts[len(facts)-3:]
	}
	for _, f := range facts {
		parts = append(parts, strings.TrimRight(f, "."))
	}
	say := ""
	if len(parts) > 0 {
		say = "Here's what I know: " + strings.Join(parts, "; ") + "."
	}
	if len(style) > 0 {
		say = joinText(say, "And you like me to: "+lowerFirst(strings.TrimRight(style[len(style)-1], "."))+".")
	}
	return "told the owner what it remembers", say
}

func init() {
	for note, acks := range styleAcks {
		phrases["ack:"+note] = acks
	}
}
