package intent

import (
	"regexp"
	"strconv"
	"strings"
)

// Fast is a turn matched by the fast grammar: no model, never in the brain's history.
type Fast struct {
	Kind   string // stop, thanks, time, quiet, talk, it, media, volume, mute, undo, yes, no, my_words, tool
	Action string // media: play, pause, next, previous; volume: set, up, down; mute: mute, unmute; it: show, hide, read
	Level  int    // volume set: 0 to 100
	Call   Call   // tool: the intent it runs, as the brain would call it
}

// Calls is the fast match as tool calls (media, volume and tools), for the phrase set.
func (f Fast) Calls() []Call {
	switch f.Kind {
	case "media":
		return []Call{{Name: "media", Args: map[string]any{"action": f.Action}}}
	case "volume", "mute":
		a := map[string]any{"action": f.Action}
		if f.Action == "set" {
			a["level"] = float64(f.Level)
		}
		return []Call{{Name: "volume", Args: a}}
	case "tool":
		return []Call{f.Call}
	}
	return []Call{{Name: "fast:" + f.Kind}}
}

func toolCall(name string, kv ...any) Fast {
	args := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		if v, ok := kv[i+1].(string); ok && v == "" {
			continue
		}
		args[kv[i].(string)] = kv[i+1]
	}
	return Fast{Kind: "tool", Call: Call{Name: name, Args: args}}
}

// Stop words: a turn that is only one of them ends without an answer.
var stopRE = regexp.MustCompile(`^(stop|stop it|stop that|cancel|cancel it|cancel that|never ?mind|shut up|enough|stop talking)$`)

// IsStop says whether text is only a stop word.
func IsStop(text string) bool { return stopRE.MatchString(norm(text)) }

var (
	pauseRE       = regexp.MustCompile(`^(pause|pause (the |that |this )?(music|song|track|video|playback|it|media))$`)
	playRE        = regexp.MustCompile(`^(play|resume|unpause|keep playing|play (the )?(music|song|it|media)|resume (the )?(music|playback|it))$`)
	nextRE        = regexp.MustCompile(`^(next track|next song|skip|skip (this |the )?(track|song|this|it))$`)
	prevRE        = regexp.MustCompile(`^(previous track|previous song|last (track|song)|go back a (track|song))$`)
	upRE          = regexp.MustCompile(`^(volume up|louder|turn it up|turn (the )?(volume|sound) up|turn up (the )?(volume|sound))$`)
	downRE        = regexp.MustCompile(`^(volume down|quieter|softer|turn it down|turn (the )?(volume|sound) down|turn down (the )?(volume|sound))$`)
	setRE         = regexp.MustCompile(`^(set (the )?)?(volume|sound)( to| at)? (.+?)( percent| %)?$`)
	muteRE        = regexp.MustCompile(`^mute( (the )?(sound|audio|volume|speakers|it))?$`)
	unmuteRE      = regexp.MustCompile(`^unmute( (the )?(sound|audio|volume|speakers|it))?$`)
	undoRE        = regexp.MustCompile(`^(undo|undo that|undo it|take that back|put it back)$`)
	helloRE       = regexp.MustCompile(`^(hey|hi|hello|hey there|hi there|hello there|okay|ok|alright|all right|herdr|hey herdr|are you there|you there)$`)
	unitsRE       = regexp.MustCompile(`\bhow many (seconds|minutes|hours|days|weeks|months|ounces|grams|inches|centimeters|meters|feet|miles|kilometers) (are )?in\b|\bconvert\b|\bin (dollars|euros|pounds|celsius|fahrenheit)\b`)
	sumQuestionRE = regexp.MustCompile(`^(what is|whats|what s|how much is|calculate|compute|work out|tell me|convert|roughly how many|how many)\b`)
	timeRE        = regexp.MustCompile(`^(what time is it|whats the time|what is the time|what s the time|the time|what day is it|what day is today|whats the date|what is the date|whats todays date|what is todays date|what date is it)( now| today)?$`)
	freshRE       = regexp.MustCompile(`^(forget (this|our|the whole) conversation|start fresh|start over|new conversation|clear our conversation|forget everything we said)$`)
	forgetTalkRE  = regexp.MustCompile(`^forget (?:what|everything) (?:i said|i told you|we said) about (.+)$`)
	changeRE      = regexp.MustCompile(`(?i)^\s*(?:change|make) it (?:to|say)[:,]?\s+(.+?)\s*$`)
	saidRE        = regexp.MustCompile(`^what did (?:the )?(.+?) (?:say|reply|answer|write)(?: back)?$`)
	thanksRE      = regexp.MustCompile(`^(thanks|thank you|cheers|great|perfect|ok thanks|okay thanks|thats all|thats it|that is all|thanks thats all|thank you thats all|no thanks|nothing else)$`)
	wordsRE       = regexp.MustCompile(`^(send|use|take) (my|my own) words( instead)?$`)

	quietRE = regexp.MustCompile(`^(quiet|be quiet|quiet mode|quiet please|go quiet|stay quiet|hush|shush|no announcements|stop (the )?announcements|announcements off|mute (the )?(announcements|replies|agents))$`)
	talkRE  = regexp.MustCompile(`^(talk to me|announcements on|turn (the )?announcements on|you can talk( again)?|unquiet|speak up|unmute (the )?(announcements|replies|agents))$`)

	layoutRE = regexp.MustCompile(`^(?:(?:switch|change|go|set)(?: the)?(?: layout| theme)?(?: to)?(?: the)? |use the |theme |layout )?(pixel|win31|workbench|fortress|main)(?: layout| theme)?$`)
	panelRE  = regexp.MustCompile(`^(show|open|hide|close|toggle) (?:the )?(?:shepherdr|shepherdrs)(?: panel)?$|^(?:the )?shepherdr(?: panel)? (on|off)$`)
	itRE     = regexp.MustCompile(`^(show|hide|close|read) (it|that)( again| out)?$|^read (it|that) (again|out)$`)
	needsRE  = regexp.MustCompile(`^(what needs me|what needs my attention|what needs attention|who needs me|what is waiting( on me| for me)?|whats waiting( on me| for me)?|is anything waiting( on me| for me)?|anything waiting( on me| for me)?|does anything need me|does anyone need me|what do i need to do)$`)
	briefRE  = regexp.MustCompile(`^(brief me|briefing|give me a briefing|give me the briefing|what did i miss|catch me up|what happened|whats new|status report|morning briefing)$`)
	agentsRE = regexp.MustCompile(`^(list (the |my )?agents|what agents are there|which agents are running|what are (my |the )?agents doing|what is everyone doing|whats everyone doing|whats running|what is running)$`)
	stepRE   = regexp.MustCompile(`^(next|previous|prev|last) agent$`)
	notesRE  = regexp.MustCompile(`^(what are my notes|read (me )?my notes|list (my )?notes|my notes|what did i note)( today| from today| for today)?$`)
	remsRE   = regexp.MustCompile(`^(list (my )?reminders|what are my reminders|what reminders (do i have|are set)|my reminders|any reminders)$`)
	cancelRE = regexp.MustCompile(`^(?:cancel|delete|remove|drop) (?:the |my )?(?:last )?(?:reminder|timer)(?: (?:for|about|to) (.+))?$`)
	doingRE  = regexp.MustCompile(`^(?:what is|whats|what s|what are) (.+?) (?:doing|up to|working on)(?: now| right now)?$`)
	readRE   = regexp.MustCompile(`^read (?:me )?(?:the )?(?:latest |last )?(?:message|reply) (?:from|of) (.+)$|^read (.+?)s? (?:latest |last )(?:message|reply)$`)
	focusRE  = regexp.MustCompile(`^(?:focus on|focus|show me|bring up|open) (.+)$`)
	goRE     = regexp.MustCompile(`^(?:go to|switch to|jump to|take me to|go over to) (.+)$`)
	tabRE    = regexp.MustCompile(`^(?:open |make |start |create )?(?:a )?new tab(?: (?:in|on) (.+?))?(?: (?:with|running) (?:a |an )?(claude|codex|shell|terminal)(?: code)?)?$`)
	tabRE2   = regexp.MustCompile(`^(?:open |make |start |create )?(?:a )?new tab (?:with|running) (?:a |an )?(claude|codex|shell|terminal)(?: code)? (?:in|on) (.+)$`)

	// On the transcript's own words (case and punctuation kept for the text).
	tellRE   = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:tell|reply to|answer|message|send(?: a message)? to|say to|ask)\s+(.+?)\s*$`)
	noteRE   = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:note|take a note|make a note|note down|write down|jot down)(?:\s+that)?\s*[:,.-]?\s+(.+?)\s*$`)
	remindRE = regexp.MustCompile(`(?i)^\s*(?:please\s+)?remind me in\s+(.+?)\s+(seconds?|secs?|minutes?|mins?|hours?)\s*(?:to|that|about|of)?\s*(.*?)[.!]?\s*$`)
	remind2  = regexp.MustCompile(`(?i)^\s*(?:please\s+)?remind me (?:to|that|about)\s+(.+?)\s+in\s+(.+?)\s+(seconds?|secs?|minutes?|mins?|hours?)[.!]?\s*$`)
	tellMeRE = regexp.MustCompile(`(?i)^(me|us|you|yourself)\b`)
	timerRE  = regexp.MustCompile(`^(?:set )?(?:a )?timer (?:for )?(.+?) (seconds?|secs?|minutes?|mins?|hours?)$|^(.+?) (seconds?|minutes?|hours?) timer$`)
)

// spoken layout names as speech recognition writes them
var layoutWords = strings.NewReplacer("windows 31", "win31", "win 31", "win31", "windows thirty one", "win31",
	"win thirty one", "win31", "windows 3 1", "win31", "win 3 1", "win31", "work bench", "workbench", "pixel art", "pixel")

// Match runs the fast grammar on a transcript. Yes, no and "send my words" match only while a card
// is open; otherwise they go to the brain, which may have just asked something.
func Match(text string, cardOpen bool) (Fast, bool) {
	t := norm(text)
	t = layoutWords.Replace(t)
	t = shepherdrWords(t)
	switch {
	case t == "":
		return Fast{}, false
	case stopRE.MatchString(t):
		return Fast{Kind: "stop"}, true
	case quietRE.MatchString(t):
		return Fast{Kind: "quiet"}, true
	case talkRE.MatchString(t):
		return Fast{Kind: "talk"}, true
	case cardOpen && answer(t, yesKeys, yesWords):
		return Fast{Kind: "yes"}, true
	case cardOpen && (answer(t, noKeys, noWords) || leadingNo(t)):
		return Fast{Kind: "no"}, true
	case cardOpen && wordsRE.MatchString(t):
		return Fast{Kind: "my_words"}, true
	case cardOpen && changeRE.MatchString(text):
		return Fast{Kind: "edit", Action: changeRE.FindStringSubmatch(text)[1]}, true
	case undoRE.MatchString(t):
		return Fast{Kind: "undo"}, true
	case thanksRE.MatchString(t):
		return Fast{Kind: "thanks"}, true
	case helloRE.MatchString(t):
		return Fast{Kind: "hello"}, true
	case (mathRE.MatchString(t) || unitsRE.MatchString(t)) && sumQuestionRE.MatchString(t):
		return Fast{Kind: "math"}, true
	case timeRE.MatchString(t):
		return Fast{Kind: "time"}, true
	case pauseRE.MatchString(t):
		return Fast{Kind: "media", Action: "pause"}, true
	case playRE.MatchString(t):
		return Fast{Kind: "media", Action: "play"}, true
	case nextRE.MatchString(t):
		return Fast{Kind: "media", Action: "next"}, true
	case prevRE.MatchString(t):
		return Fast{Kind: "media", Action: "previous"}, true
	case upRE.MatchString(t):
		return Fast{Kind: "volume", Action: "up"}, true
	case downRE.MatchString(t):
		return Fast{Kind: "volume", Action: "down"}, true
	case muteRE.MatchString(t):
		return Fast{Kind: "mute", Action: "mute"}, true
	case unmuteRE.MatchString(t):
		return Fast{Kind: "mute", Action: "unmute"}, true
	case needsRE.MatchString(t):
		return toolCall("needs_me"), true
	case briefRE.MatchString(t):
		return toolCall("briefing"), true
	case agentsRE.MatchString(t):
		return toolCall("list_agents"), true
	case notesRE.MatchString(t):
		return toolCall("list_notes"), true
	case remsRE.MatchString(t):
		return toolCall("list_reminders"), true
	}
	if m := setRE.FindStringSubmatch(t); m != nil {
		if n, ok := Number(m[5]); ok && n >= 0 && n <= 100 {
			return Fast{Kind: "volume", Action: "set", Level: n}, true
		}
	}
	if m := itRE.FindStringSubmatch(t); m != nil {
		a := m[1]
		if a == "" || m[4] != "" {
			a = "read"
		}
		if a == "close" {
			a = "hide"
		}
		return Fast{Kind: "it", Action: a}, true
	}
	if m := layoutRE.FindStringSubmatch(t); m != nil {
		return toolCall("switch_layout", "layout", m[1]), true
	}
	if m := panelRE.FindStringSubmatch(t); m != nil {
		show := m[1] == "show" || m[1] == "open" || m[2] == "on"
		if m[1] == "toggle" {
			return Fast{Kind: "it", Action: "toggle_panel"}, true
		}
		return toolCall("shepherdr_panel", "show", show), true
	}
	if m := stepRE.FindStringSubmatch(t); m != nil {
		dir := "next"
		if m[1] != "next" {
			dir = "previous"
		}
		return toolCall("agent_step", "direction", dir), true
	}
	if m := cancelRE.FindStringSubmatch(t); m != nil {
		return toolCall("cancel_reminder", "which", m[1]), true
	}
	if m := remindRE.FindStringSubmatch(text); m != nil {
		if f, ok := reminder(m[1], m[2], m[3]); ok {
			return f, true
		}
	}
	if m := remind2.FindStringSubmatch(text); m != nil {
		if f, ok := reminder(m[2], m[3], m[1]); ok {
			return f, true
		}
	}
	if m := timerRE.FindStringSubmatch(t); m != nil {
		n, u := m[1], m[2]
		if n == "" {
			n, u = m[3], m[4]
		}
		if f, ok := reminder(n, u, ""); ok {
			return f, true
		}
	}
	if m := callMeRE.FindStringSubmatch(t); m != nil && len(strings.Fields(m[1])) <= 2 {
		name := strings.Title(m[1])
		return toolCall("remember", "kind", "style", "text", "Call the owner "+name+".", "ack", "Sure, "+name+" it is."), true
	}
	for _, sf := range styleFeedback {
		if sf.re.MatchString(t) {
			return toolCall("remember", "kind", "style", "text", sf.note), true
		}
	}
	if m := fromNowRE.FindStringSubmatch(text); m != nil {
		return toolCall("remember", "kind", "style", "text", strings.TrimRight(m[1], ".!")), true
	}
	if m := preferRE.FindStringSubmatch(text); m != nil {
		return toolCall("remember", "kind", "style", "text", "Prefers that "+strings.TrimRight(m[1], ".!")), true
	}
	if m := correctRE.FindStringSubmatch(text); m != nil && !cardOpen && !questionRE.MatchString(m[1]) && !strings.HasSuffix(strings.TrimSpace(text), "?") {
		return toolCall("remember", "kind", "correction", "text", strings.TrimRight(m[1], ".!")), true
	}
	if m := remind3.FindStringSubmatch(text); m != nil {
		if f, ok := reminder(m[1], m[2], m[3]); ok {
			return f, true
		}
	}
	if recallRE.MatchString(t) {
		return toolCall("about_me"), true
	}
	if m := rememberRE.FindStringSubmatch(text); m != nil {
		kind := "fact"
		if styleWordsRE.MatchString(m[1]) {
			kind = "style"
		}
		return toolCall("remember", "kind", kind, "text", strings.TrimRight(m[1], ".!")), true
	}
	if freshRE.MatchString(t) {
		return toolCall("forget_conversation"), true
	}
	if m := forgetTalkRE.FindStringSubmatch(t); m != nil {
		return toolCall("forget_talk", "about", m[1]), true
	}
	if m := forgetRE.FindStringSubmatch(text); m != nil && !forgetItRE.MatchString(t) {
		return toolCall("forget", "text", strings.TrimRight(m[1], ".!?")), true
	}
	if m := noteRE.FindStringSubmatch(text); m != nil && !strings.HasPrefix(strings.ToLower(m[1]), "my notes") {
		return toolCall("write_note", "text", m[1]), true
	}
	if m := tellRE.FindStringSubmatch(text); m != nil && len(strings.Fields(m[1])) >= 2 && !tellMeRE.MatchString(m[1]) {
		// The agent's name and the words aren't told apart here: vcd tries the longest name it knows.
		return toolCall("send_to_agent", "say", m[1]), true
	}
	if m := tabRE2.FindStringSubmatch(t); m != nil {
		return toolCall("new_tab", "space", m[2], "agent", agentWord(m[1])), true
	}
	if m := tabRE.FindStringSubmatch(t); m != nil {
		return toolCall("new_tab", "space", m[1], "agent", agentWord(m[2])), true
	}
	if m := doingRE.FindStringSubmatch(t); m != nil && !strings.HasPrefix(m[1], "you") {
		return toolCall("agent_doing", "agent", m[1]), true
	}
	if f, ok := fastMore(t); ok {
		return f, true
	}
	if m := saidRE.FindStringSubmatch(t); m != nil {
		return toolCall("read_latest", "agent", m[1]), true
	}
	if m := readRE.FindStringSubmatch(t); m != nil {
		return toolCall("read_latest", "agent", m[1]+m[2]), true
	}
	if m := goRE.FindStringSubmatch(t); m != nil {
		return toolCall("go_to_space", "space", m[1]), true
	}
	if m := focusRE.FindStringSubmatch(t); m != nil {
		return toolCall("focus", "name", m[1]), true
	}
	return Fast{}, false
}

func agentWord(w string) string {
	if w == "terminal" {
		return "shell"
	}
	return w
}

// reminder reads "20", "minutes", "stretch" as a remind call.
func reminder(n, unit, text string) (Fast, bool) {
	k, ok := Number(n)
	if !ok {
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "a", "one", "an":
			k, ok = 1, true
		case "a few", "few":
			k, ok = 3, true
		case "a quarter of an", "quarter of an":
			if strings.HasPrefix(strings.ToLower(unit), "hour") {
				return toolCall("remind", "minutes", float64(15), "text", strings.TrimSpace(text)), true
			}
		case "half an", "half a":
			if strings.HasPrefix(unit, "hour") {
				return toolCall("remind", "minutes", float64(30), "text", strings.TrimSpace(text)), true
			}
		}
	}
	if !ok || k <= 0 {
		return Fast{}, false
	}
	unit = strings.ToLower(unit)
	switch {
	case strings.HasPrefix(unit, "s"):
		return toolCall("remind", "seconds", float64(k), "text", strings.TrimSpace(text)), true
	case strings.HasPrefix(unit, "h"):
		return toolCall("remind", "minutes", float64(k*60), "text", strings.TrimSpace(text)), true
	}
	return toolCall("remind", "minutes", float64(k), "text", strings.TrimSpace(text)), true
}

// Notes, recall, the web and the pomodoro.
var (
	lastNoteRE = regexp.MustCompile(`^(read (me )?my last note|what was my last note|whats my last note|my last note|what did i write last)$`)
	findNoteRE = regexp.MustCompile(`^(?:find my notes? (?:about|on)|what did i (?:write|note|say) about|what do my notes say about|search my notes for|what do you know about) (.+?)(?: from my notes| in my notes)?$`)
	lookRE     = regexp.MustCompile(`^(?:look up|search the web for|search online for|search for|google|find out about|find out|look online for) (.+)$`)
	onWebRE    = regexp.MustCompile(`^((?:what|who|where|when) (?:is|was|are|were) .+?) (?:on the web|online|on the internet)$`)
	askWebRE   = regexp.MustCompile(`^(?:who is|who was|who were|who are|what is an?|what is the|whats the|whats an?|what was the|what are|where is|when was|when did|when is) (.+)$`)
	mathRE     = regexp.MustCompile(`\b(square root|squared|cubed|to the power|percent of|divided by|multiplied by|times|plus|minus)\b`)
	mineRE     = regexp.MustCompile(`\b(my|your|you|me|i|this|that|it|herdr|shepherdr|agent|agents|space|tab|layout|reminder|reminders|pomodoro|note|notes|timer)\b`)
	moreRE     = regexp.MustCompile(`^(tell me more|read me more|more about (that|it)|tell me more about (that|it)|read (me )?that page|read the page|what does (that|the) page say|go on|keep going)$`)

	pomoStartRE = regexp.MustCompile(`^(?:start|begin|set|run)? ?(?:an? )?(?:(\S+) minutes? )?(?:pomodoro|focus session|focus block|pomodoro timer)(?: timer)?(?: for (\S+(?: \S+)?) minutes?)?(?: (?:on|for) (.+))?$`)
	pomoBreakRE = regexp.MustCompile(`^(?:take|start|have) (?:a |an? )?(short |long )?break$|^(break time|i need a break|time for a break)$`)
	pomoPauseRE = regexp.MustCompile(`^pause (?:the )?(?:pomodoro|timer|focus|focus session|break)$`)
	pomoGoRE    = regexp.MustCompile(`^(?:resume|continue|unpause) (?:the )?(?:pomodoro|timer|focus|focus session|break)$`)
	pomoStopRE  = regexp.MustCompile(`^(?:stop|cancel|end|finish) (?:the )?(?:pomodoro|focus|focus session|break|timer)$`)
	pomoLeftRE  = regexp.MustCompile(`^(how much time (is )?left|how long (is )?left|how much longer|time left|how many minutes (are )?left)$`)
	pomoAddRE   = regexp.MustCompile(`^add (\S+(?: \S+)?) (?:more )?minutes?(?: to (?:the )?(?:pomodoro|timer|focus|break))?$`)
	pomoStatsRE = regexp.MustCompile(`^how many pomodoros(?: did i (?:do|finish|complete))?( today| this week)?$|^pomodoro stats$`)
)

// fastMore is the pomodoro, notes, recall and web grammar (after the Herdr commands).
func fastMore(t string) (Fast, bool) {
	switch {
	case lastNoteRE.MatchString(t):
		return toolCall("last_note"), true
	case moreRE.MatchString(t):
		return toolCall("read_page"), true
	case pomoPauseRE.MatchString(t):
		return toolCall("pomodoro", "action", "pause"), true
	case pomoGoRE.MatchString(t):
		return toolCall("pomodoro", "action", "resume"), true
	case pomoStopRE.MatchString(t):
		return toolCall("pomodoro", "action", "stop"), true
	case pomoLeftRE.MatchString(t):
		return toolCall("pomodoro", "action", "status"), true
	case pomoStatsRE.MatchString(t):
		return toolCall("pomodoro", "action", "stats"), true
	}
	if m := pomoBreakRE.FindStringSubmatch(t); m != nil {
		if strings.TrimSpace(m[1]) == "long" {
			return toolCall("pomodoro", "action", "break", "minutes", float64(15)), true
		}
		return toolCall("pomodoro", "action", "break"), true
	}
	if m := pomoAddRE.FindStringSubmatch(t); m != nil {
		if n, ok := Number(m[1]); ok && n > 0 {
			return toolCall("pomodoro", "action", "add", "minutes", float64(n)), true
		}
	}
	if m := pomoStartRE.FindStringSubmatch(t); m != nil {
		f := toolCall("pomodoro", "action", "start", "task", m[3])
		for _, x := range []string{m[1], m[2]} {
			if n, ok := Number(x); ok && n > 0 {
				f.Call.Args["minutes"] = float64(n)
			}
		}
		return f, true
	}
	if m := findNoteRE.FindStringSubmatch(t); m != nil && m[1] != "me" {
		return toolCall("recall", "query", m[1]), true
	}
	if m := lookRE.FindStringSubmatch(t); m != nil {
		return toolCall("web_search", "query", m[1]), true
	}
	if m := onWebRE.FindStringSubmatch(t); m != nil {
		return toolCall("web_search", "query", m[1]), true
	}
	if m := askWebRE.FindStringSubmatch(t); m != nil && !mathRE.MatchString(t) && !mineRE.MatchString(m[1]) {
		return toolCall("web_search", "query", t), true
	}
	return Fast{}, false
}

// Memory: "remember that …", "forget …", "what do you remember about me", and what the owner says
// of how it talks, which becomes a style note.
var (
	rememberRE    = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:remember|keep in mind|don't forget)(?:\s+that)?[:,]?\s+(.+?)\s*$`)
	forgetRE      = regexp.MustCompile(`(?i)^\s*(?:please\s+)?forget(?:\s+that|\s+about)?\s+(.+?)\s*$`)
	forgetItRE    = regexp.MustCompile(`^forget (it|about it|that)$`)
	recallRE      = regexp.MustCompile(`^(what do you (remember|know) about me|what have you (remembered|learned) about me|what do you remember|tell me what you know about me)$`)
	fromNowRE     = regexp.MustCompile(`(?i)^\s*(?:from now on|going forward|in (?:the )?future)[,:]?\s+(.{6,}?)\s*$`)
	preferRE      = regexp.MustCompile(`(?i)^\s*I(?:'d| would)? prefer (?:it )?(?:that |if )?(you .{4,}?)\s*$`)
	correctRE     = regexp.MustCompile(`(?i)^\s*(?:no|nope|actually|wrong)[,.!]+\s+((?:[\w-]+\s+){0,4}(?:is|are|was|isn't|is not|aren't)\s+.{3,}?)\s*$`)
	questionRE    = regexp.MustCompile(`(?i)^(what|who|where|when|why|how|which|is|are|was|does|do|can)\b`)
	remind3       = regexp.MustCompile(`(?i)^\s*in\s+(.+?)\s+(seconds?|minutes?|mins?|hours?)[,]?\s+remind me\s+(?:to|about|of)\s+(.+?)[.!]?\s*$`)
	callMeRE      = regexp.MustCompile(`^(?:please )?call me ([a-z]+(?: [a-z]+)?)$`)
	styleWordsRE  = regexp.MustCompile(`(?i)\b(talk|answer|answers|speak|reply|replies|joke|jokes|questions|shorter|longer|formal|casual|call me)\b`)
	styleFeedback = []struct {
		re   *regexp.Regexp
		note string
	}{
		{regexp.MustCompile(`^(be shorter|shorter|shorter please|keep it short|keep it shorter|talk less|less talking|too long|too wordy|shorter answers|you talk too much)$`), "Keep answers very short."},
		{regexp.MustCompile(`^(you can joke|feel free to joke|be funny|be funnier|a bit of humou?r is fine|joke a little|you can be funny)$`), "A light joke now and then is welcome."},
		{regexp.MustCompile(`^(dont call me that|stop calling me that|dont use my name|stop using my name|no names)$`), "Don't use the owner's name."},
		{regexp.MustCompile(`^(too many questions|stop asking (me )?questions|fewer questions|ask fewer questions|less questions|dont ask so many questions)$`), "Ask fewer follow-up questions."},
		{regexp.MustCompile(`^(be more formal|more formal|more formal please)$`), "Be a bit more formal."},
		{regexp.MustCompile(`^(be more casual|more casual|less formal|relax)$`), "Be casual and relaxed."},
	}
)

var shepRE = regexp.MustCompile(`\b(shepherder|shepherd r|shepherd|shepard|sheppard|shepherds|shepardr|sheperd)\b`)

func shepherdrWords(t string) string { return shepRE.ReplaceAllString(t, "shepherdr") }

// A card answer is a short turn of only these words, with at least one key word ("Okay, do it.", "Yes,
// please do.", "Sure, go ahead.", "No, don't."); anything else goes to the brain.
var (
	yesKeys  = set("yes", "yeah", "yep", "yup", "sure", "ok", "okay", "confirm", "confirmed", "go", "do", "correct", "absolutely")
	yesWords = set("yes", "yeah", "yep", "yup", "sure", "ok", "okay", "confirm", "confirmed", "go", "do", "correct",
		"absolutely", "ahead", "it", "that", "thats", "for", "please", "send", "open", "move", "close", "fine", "thanks", "and", "right")
	// A verb in a yes only as "open it", "move it", "close it"; "right" only first or after a yes word, never
	// after a verb or "go" ("move it right", "go right" are corrections).
	yesVerbs = set("open", "move", "close", "send")
	noKeys   = set("no", "nope", "nah", "dont", "not")
	noWords  = set("no", "nope", "nah", "dont", "not", "do", "it", "that", "thanks", "please", "thank", "you", "way", "now")
)

// leadingNo: with a confirm open, a turn that starts with a no ("No, don't send it", "Nope, not that
// one", "Cancel it please") is a no: declining never acts, so it is never left to the brain.
func leadingNo(t string) bool {
	f := strings.Fields(t)
	return len(f) > 0 && len(f) <= 10 && (noKeys[f[0]] || f[0] == "cancel" || f[0] == "stop" || (f[0] == "do" && len(f) > 1 && f[1] == "not"))
}

func set(ws ...string) map[string]bool {
	m := map[string]bool{}
	for _, w := range ws {
		m[w] = true
	}
	return m
}

var rightRE = regexp.MustCompile(`\b(thats right|that is right|all right|alright)\b`)

func answer(t string, keys, allowed map[string]bool) bool {
	// "That's right" and "all right" are yeses; "right" alone is a direction ("move it right").
	t = rightRE.ReplaceAllString(t, "yes")
	f := strings.Fields(t)
	if len(f) == 0 || len(f) > 6 {
		return false
	}
	key := false
	for i, w := range f {
		if !allowed[w] {
			return false
		}
		if yesVerbs[w] && !(i+1 < len(f) && f[i+1] == "it") {
			return false
		}
		if w == "right" && i > 0 && (!keys[f[i-1]] || f[i-1] == "go" || f[i-1] == "do") {
			return false
		}
		key = key || keys[w]
	}
	return key
}

// norm lowercases a transcript to plain words: punctuation gone ("Don't." is "dont"), "%" kept,
// and a leading "please" / "hey" / "computer" or a trailing "please" / "thanks" dropped.
func norm(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '%':
			b.WriteRune(r)
		case r == '\'' || r == '’':
		default:
			b.WriteRune(' ')
		}
	}
	f := strings.Fields(b.String())
	for len(f) > 1 && (f[0] == "please" || f[0] == "hey" || f[0] == "computer" || f[0] == "herdr" || f[0] == "herder") {
		f = f[1:]
	}
	for len(f) > 1 {
		switch last := f[len(f)-1]; {
		case last == "please" || last == "thanks":
			f = f[:len(f)-1]
			continue
		case last == "you" && len(f) > 2 && f[len(f)-2] == "thank":
			f = f[:len(f)-2]
			continue
		}
		break
	}
	return strings.Join(f, " ")
}

var ones = map[string]int{"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7,
	"eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15,
	"sixteen": 16, "seventeen": 17, "eighteen": 18, "nineteen": 19}
var tens = map[string]int{"twenty": 20, "thirty": 30, "forty": 40, "fifty": 50, "sixty": 60, "seventy": 70,
	"eighty": 80, "ninety": 90}

// Number reads "40", "40%", "forty", "forty five", "forty-five" or "a hundred" (0 to 100 in words).
func Number(s string) (int, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.ToLower(s)), "%"))
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	f := strings.Fields(strings.ReplaceAll(s, "-", " "))
	switch {
	case len(f) == 0:
		return 0, false
	case len(f) <= 2 && f[len(f)-1] == "hundred" && (len(f) == 1 || f[0] == "a" || f[0] == "one"):
		return 100, true
	case len(f) == 1:
		if n, ok := ones[f[0]]; ok {
			return n, true
		}
		n, ok := tens[f[0]]
		return n, ok
	case len(f) == 2:
		t, ok1 := tens[f[0]]
		o, ok2 := ones[f[1]]
		if ok1 && ok2 && o > 0 && o < 10 {
			return t + o, true
		}
	}
	return 0, false
}
