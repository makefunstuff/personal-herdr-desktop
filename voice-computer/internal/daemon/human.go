package daemon

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Human timing and wording: a short filler when a tool takes a moment, no robotic tics, numbers said
// the way a person rounds them.

// slowTools may take longer than a breath: a filler covers the wait.
var slowTools = map[string]bool{"web_search": true, "read_page": true, "browse": true, "read_app": true, "agent_doing": true, "read_latest": true,
	"recall": true, "briefing": true, "needs_me": true}

// fillerAfter says a filler if the tool is still running after 0.8 s; the returned func ends the wait.
func (d *Daemon) fillerAfter(tool string, gen int) func() {
	if !slowTools[tool] {
		return func() {}
	}
	done := make(chan struct{})
	t0 := time.Now()
	go func() {
		select {
		case <-done:
			return
		case <-time.After(800 * time.Millisecond):
		}
		d.mu.Lock()
		if d.gen != gen || d.rec != nil || d.st.Muted {
			d.mu.Unlock()
			return
		}
		d.fillerOn = true
		d.mu.Unlock()
		key := "filler"
		if tool == "web_search" || tool == "read_page" {
			key = "filler.web"
		}
		text := d.phrase(key, nil)
		b, _ := json.Marshal(map[string]any{"event": "filler", "after_ms": time.Since(t0).Milliseconds()})
		d.log.Println(string(b))
		d.say(text, true)
		d.mu.Lock()
		d.fillerOn = false
		d.mu.Unlock()
	}()
	return func() { close(done) }
}

func init() {
	phrases["filler"] = []string{"One sec.", "Let me check.", "Okay, looking.", "Hang on a moment."}
	phrases["filler.web"] = []string{"Let me look that up.", "One sec, looking.", "Checking the web."}
}

// Tics a person wouldn't say: dropped from the brain's sentences.
var (
	ticSentenceRE = regexp.MustCompile(`(?i)^(as an? (ai|language model|assistant|virtual assistant)\b[^.!?]*[.!?]|i('m| am) (just )?an? (ai|language model|virtual assistant)\b[^.!?]*[.!?]|here (is|are) the (information|details|results)( you (requested|asked for))?[.!:]?|task completed successfully[.!]?|i hope this helps[.!]?|let me know if you need anything else[.!]?)$`)
	ticPrefixRE   = regexp.MustCompile(`(?i)^(as an? (ai|language model|assistant)[^,]*,\s*|certainly[!,.]\s+|absolutely[!,.]\s+)`)
)

// Humanize rewrites one sentence the brain says: tics gone ("" drops it).
func Humanize(s string) string {
	t := strings.TrimSpace(s)
	if ticSentenceRE.MatchString(t) {
		return ""
	}
	t = ticPrefixRE.ReplaceAllString(t, "")
	if t != "" {
		t = strings.ToUpper(t[:1]) + t[1:]
	}
	return t
}

// progressWords says a progress percentage as a person would.
func progressWords(pct int) string {
	switch {
	case pct >= 90:
		return "almost done"
	case pct >= 75:
		return "nearly there"
	case pct >= 55:
		return "more than halfway"
	case pct >= 40:
		return "about halfway"
	case pct >= 20:
		return "a good way in"
	}
	return "just getting going"
}

// etaWords rounds minutes left: "any minute now", "about 15 minutes to go", "about four hours to go".
func etaWords(min int) string {
	words := []string{"", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"}
	switch {
	case min <= 2:
		return "any minute now"
	case min < 10:
		return "a few minutes to go"
	case min < 50:
		return fmt.Sprintf("about %d minutes to go", (min+2)/5*5)
	case min < 90:
		return "about an hour to go"
	}
	if h := (min + 30) / 60; h < len(words) {
		return "about " + words[h] + " hours to go"
	}
	return "several hours to go"
}
