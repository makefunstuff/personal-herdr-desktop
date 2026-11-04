package daemon

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The secretary's own words for what vcd says without the brain: a warm, brief executive assistant's,
// a few ways each so it doesn't sound canned. A variant is never the one said last for the same thing,
// and only some use the owner's name ({name}). {x} placeholders are filled from args; every fact in
// them comes from Herdr, Shepherdr, the clock or the owner, never from a model.
var phrases = map[string][]string{
	"needs.none":     {"Nothing right now.", "You're all clear, {name}.", "Nothing's waiting on you.", "All quiet. Want a quick briefing?"},
	"needs.close1":   {"Want me to bring it up?", "Shall I open it?", ""},
	"brief.clear":    {"You're clear.", "Nothing needs you.", "You're all set, {name}.", ""},
	"brief.quiet":    {"Quiet since {since}: nothing finished and nothing's running.", "Nothing's moved since {since}, and nothing's running."},
	"focus.agent":    {"Here's {label}, {status}.", "{Label}'s up, {status}.", "Bringing up {label}. It's {status}."},
	"focus.space":    {"You're in {space} now.", "Here's {space}.", "Over to {space}, {name}."},
	"step":           {"{Label}, {status}.", "Next up, {label}. It's {status}."},
	"tab.new":        {"New tab in {space}{with}.", "Opened a tab in {space}{with}.", "Done, a fresh tab in {space}{with}."},
	"send.ask":       {"Send {label}: {text}?", "Want me to tell {label}: {text}?", "Shall I pass this to {label}: {text}?"},
	"send.done":      {"Sent to {label}.", "Done, {label} has it.", "Passed it on to {label}."},
	"send.declined":  {"Okay, I won't send it.", "Got it, not sending.", "No problem, dropped it."},
	"tab.declined":   {"Okay, no new tab.", "Got it, leaving it."},
	"layout":         {"Switched to {layout}.", "{Layout} it is, looking sharp.", "There you go, {layout}. Nice change of scenery.", "All set, {name}, {layout} layout."},
	"panel.show":     {"Here's the Shepherdr panel.", "Shepherdr panel's up.", "Opened Shepherdr for you."},
	"panel.hide":     {"Panel's away.", "Hid the Shepherdr panel.", "Shepherdr's tucked away."},
	"panel.toggle":   {"Toggled the Shepherdr panel.", "Flipped the Shepherdr panel."},
	"panel.none":     {"This layout doesn't have a Shepherdr panel.", "There's no Shepherdr panel in this layout."},
	"note":           {"Noted in today's notes.", "Got it, it's in today's notes.", "Written down for you."},
	"remind.set":     {"I'll remind you at {time}{what}.", "Sure, I'll nudge you at {time}{what}.", "Reminder set for {time}{what}."},
	"remind.fire":    {"{Name}, reminder: {text}.", "Quick reminder: {text}.", "It's time: {text}."},
	"remind.none":    {"You don't have any reminders.", "No reminders right now."},
	"remind.cancel":  {"Cancelled the reminder{what}.", "Done, that reminder's gone{what}."},
	"thanks":         {"Anytime, {name}.", "You got it. Happy to help.", "Of course, that's what I'm here for."},
	"hello":          {"Hey {name}, good to hear you. What can I do for you?", "Hi! I'm right here. What's on your mind?", "Hey, what's up? Happy to help."},
	"announce.on":    {"Okay, I'll let you know when something finishes or needs you.", "Got it, I'll keep you posted.", "Sure, {name}, I'll speak up when something changes."},
	"math":           {"I'd only be guessing there, sorry. A calculator's the safer bet.", "Sums aren't something I'd trust myself with. Want me to look it up instead?"},
	"time":           {"It's {time}.", "It's {time}, {day}.", "{Time} right now."},
	"undo":           {"Undone.", "Put it back.", "Done, back to where it was."},
	"agent.unknown":  {"I can't find {x}. Which agent did you mean?", "I don't see an agent called {x}.", "Hmm, there's no {x} that I can see."},
	"failed":         {"That didn't go through: {x}.", "Hmm, that didn't work: {x}.", "Sorry, that failed: {x}."},
	"remember.fact":  {"Got it, I'll remember that.", "Noted, I'll keep that in mind.", "Okay, I've got that."},
	"remember.fix":   {"Thanks, I'll get that right next time.", "Got it, corrected.", "Noted, my mistake."},
	"remember.style": {"Got it.", "Okay, noted.", "Sure, from now on."},
	"remember.secret": {"I'd rather not keep secrets like that. A password manager's the safer place.",
		"I won't keep that one: secrets are better in your password manager."},
	"forget.done": {"Done, I've forgotten that.", "Gone.", "Okay, forgotten."},
	"forget.none": {"I don't have anything like that.", "Nothing like that in my notes about you."},
	"recall.none": {"Nothing yet. Tell me what to remember.", "I don't know much about you yet, {name}."},
}

var (
	callMeRE = regexp.MustCompile(`(?i)^call the owner (.+?)\.?$`)
	noNameRE = regexp.MustCompile(`(?i)don't use the owner's name|do not use the owner's name`)
)

// voices keeps what was said last for each phrase, so the next one differs.
type voices struct {
	mu   sync.Mutex
	last map[string]int
}

// phrase picks a variant of key, fills its placeholders and the owner's name ({name}; a style note may
// rename them or say not to use it).
func (d *Daemon) phrase(key string, args map[string]string) string {
	vs := phrases[key]
	if len(vs) == 0 {
		return ""
	}
	d.voices.mu.Lock()
	if d.voices.last == nil {
		d.voices.last = map[string]int{}
	}
	last, seen := d.voices.last[key]
	i := rand.IntN(len(vs))
	if seen && len(vs) > 1 && i == last {
		i = (i + 1 + rand.IntN(len(vs)-1)) % len(vs)
	}
	d.voices.last[key] = i
	d.voices.mu.Unlock()
	return d.fill(vs[i], args)
}

func (d *Daemon) fill(s string, args map[string]string) string {
	for k, v := range args {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
		s = strings.ReplaceAll(s, "{"+capFirst(k)+"}", capFirst(v))
	}
	name := d.ownerName()
	if name == "" {
		for _, cut := range []string{", {name}", " {name}", "{Name}, ", "{name}, "} {
			s = strings.ReplaceAll(s, cut, "")
		}
		s = capFirst(s)
	}
	s = strings.ReplaceAll(s, "{name}", name)
	s = strings.ReplaceAll(s, "{Name}", name)
	return strings.TrimSpace(s)
}

// ownerName is how to call the owner: the newest style note decides ("Call the owner Yuri.", "Don't
// use the owner's name."), else the config's.
func (d *Daemon) ownerName() string {
	if d.mem != nil {
		items := d.mem.Items("style")
		for i := len(items) - 1; i >= 0; i-- {
			if noNameRE.MatchString(items[i]) {
				return ""
			}
			if m := callMeRE.FindStringSubmatch(items[i]); m != nil {
				return m[1]
			}
		}
	}
	return d.cfg.Owner
}

// clockArgs are the time words for a phrase.
func clockArgs(t time.Time) map[string]string {
	return map[string]string{"time": t.Format("15:04"), "day": t.Format("Monday 2 January")}
}

// suggestion is one useful next step, grounded in what vcd knows right now (an agent waiting on the
// owner, a reminder coming up), or "": the same one at most once in 10 minutes, and none when the
// owner asked for short answers or fewer questions.
func (d *Daemon) suggestion() string {
	if d.mem != nil {
		if _, ok := d.mem.HasStyle(regexp.MustCompile(`(?i)short|brief|less|fewer|one sentence`)); ok {
			return ""
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sec.suggested == nil {
		d.sec.suggested = map[string]time.Time{}
	}
	fresh := func(key string) bool {
		if at, ok := d.sec.suggested[key]; ok && time.Since(at) < 10*time.Minute {
			return false
		}
		d.sec.suggested[key] = time.Now()
		return true
	}
	for _, it := range d.st.Inbox {
		if fresh("inbox:" + it.Who + it.What) {
			if it.What != "" {
				return d.fill(d.pick1("suggest.ask", map[string]string{"who": it.Who}), nil)
			}
			return d.fill(d.pick1("suggest.wait", map[string]string{"who": it.Who}), nil)
		}
	}
	for _, r := range d.sec.reminders {
		if until := time.Until(r.due); until > 0 && until < 15*time.Minute && fresh(fmt.Sprint("rem:", r.id)) {
			what := strings.TrimPrefix(r.text, "to ")
			if what == "" {
				what = "the thing you set"
			}
			return d.fill(d.pick1("suggest.rem", map[string]string{"what": what, "time": r.due.Format("15:04")}), nil)
		}
	}
	return ""
}

// pick1 is phrase for callers that hold d.mu (phrase doesn't take it).
func (d *Daemon) pick1(key string, args map[string]string) string { return d.phrase(key, args) }

func init() {
	phrases["suggest.wait"] = []string{"By the way, {who} is waiting on you. Want me to bring it up?", "{Who}'s still waiting for you, if you have a minute."}
	phrases["suggest.ask"] = []string{"By the way, {who} has a question for you. Want to hear it?", "{Who} is waiting on an answer from you, by the way."}
	phrases["suggest.rem"] = []string{"Heads up, your reminder to {what} is at {time}.", "Just so you know, I'll remind you to {what} at {time}."}
}
