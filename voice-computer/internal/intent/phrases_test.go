package intent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"herdr-voice-computer/internal/audio"
	"herdr-voice-computer/internal/config"
	"herdr-voice-computer/internal/daemon"
	"herdr-voice-computer/internal/intent"
	"herdr-voice-computer/internal/voice"
)

type want struct {
	Name  string         `json:"name"`
	Slots map[string]any `json:"slots"`
}

type phrase struct {
	Say     string   `json:"say"`
	Kind    string   `json:"kind"`
	Intents []want   `json:"intents"`
	Has     []string `json:"has"`
	Not     []string `json:"not"`
	Note    string   `json:"note"`
}

type set struct {
	Context string   `json:"context"`
	Phrases []phrase `json:"phrases"`
}

func load(t *testing.T) set {
	t.Helper()
	data, err := os.ReadFile("testdata/phrases.json")
	if err != nil {
		t.Fatal(err)
	}
	var s set
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// The set is well formed, the fast grammar takes exactly the fast phrases, and no other phrase.
func TestPhraseSetAndFastGrammar(t *testing.T) {
	s := load(t)
	if !strings.HasPrefix(s.Context, daemon.ContextPrefix) {
		t.Fatalf("the set's context line isn't shaped like vcd's: %q", s.Context)
	}
	kinds := map[string]int{}
	for _, p := range s.Phrases {
		kinds[p.Kind]++
		for _, w := range p.Intents {
			for _, n := range strings.Split(w.Name, "|") {
				if !intent.Known(n) && !strings.HasPrefix(n, "fast:") {
					t.Errorf("%q: no intent %q", p.Say, n)
				}
			}
		}
		f, ok := intent.Match(p.Say, false)
		switch p.Kind {
		case "fast":
			if !ok || len(p.Intents) != 1 || !fastIs(f, p.Intents[0]) || !slotsExact(f, p.Intents[0]) {
				t.Errorf("fast %q: matched %v %+v", p.Say, ok, f)
			}
		case "intent", "talk", "grounded", "unknown":
			if ok {
				t.Errorf("%s %q was taken by the fast grammar: %+v", p.Kind, p.Say, f)
			}
			if (p.Kind == "intent") != (len(p.Intents) > 0) {
				t.Errorf("%q: intents for kind %s", p.Say, p.Kind)
			}
		default:
			t.Errorf("%q: kind %q", p.Say, p.Kind)
		}
	}
	if kinds["unknown"] < 5 || kinds["fast"] < 40 || kinds["intent"] < 8 || kinds["talk"] < 3 || kinds["grounded"] < 2 {
		t.Errorf("the set is thin: %v", kinds)
	}
	// Card answers are fast only while a card is open.
	for _, y := range []string{"Yes.", "yeah", "Do it.", "No.", "don't", "Send my words."} {
		if _, ok := intent.Match(y, false); ok {
			t.Errorf("%q matched with no card", y)
		}
		if _, ok := intent.Match(y, true); !ok {
			t.Errorf("%q didn't match with a card", y)
		}
	}
	for say, kind := range map[string]string{"Okay, do it.": "yes", "Yes, please do.": "yes", "Sure, go ahead.": "yes",
		"Yeah, do it.": "yes", "Move it right.": "", "Okay, move it right.": "", "Go right.": "", "Yes, do that.": "yes", "Do that.": "yes",
		"Sure, that's fine.": "yes", "Okay, that's fine.": "yes", "Alright.": "yes", "Alrighty.": "", "Yes, send it.": "yes", "Right, do it.": "yes", "Yes, right.": "yes", "That is right.": "yes",
		"Close it, yes.": "yes", "Yes, go ahead and close it.": "yes", "Okay, close it right now.": "", "Okay, close that one.": "", "Yes, close it.": "yes", "Yes, move it.": "yes", "That's right.": "yes", "Okay, open it.": "yes", "Go for it.": "yes", "Please do.": "yes", "No, don't.": "no",
		"Don't do it.": "no", "Not now.": "no", "No, don't send it.": "no", "Nope, not that one.": "no", "Do not send that.": "no", "No thank you.": "no", "Cancel that.": "stop", "Stop it.": "stop",
		"Open it.": "", "Yes, and close Slack too.": ""} {
		f, ok := intent.Match(say, true)
		if (kind == "" && ok && f.Kind != "media" && f.Kind != "volume" && f.Kind != "tool") || (kind != "" && f.Kind != kind) {
			t.Errorf("with a card, %q: %+v %v, want %q", say, f, ok, kind)
		}
	}
	if f, _ := intent.Match("Undo that.", false); f.Kind != "undo" {
		t.Errorf("undo: %+v", f)
	}
	if f, _ := intent.Match("Send my words.", true); f.Kind != "my_words" {
		t.Errorf("send my words: %+v", f)
	}
	if !intent.IsStop("Never mind.") || intent.IsStop("never mind the volume") {
		t.Errorf("stop words")
	}
	if f, _ := intent.Match("What time is it?", false); f.Kind != "time" {
		t.Errorf("time: %+v", f)
	}
	if f, _ := intent.Match("Thanks, that's all.", false); f.Kind != "thanks" {
		t.Errorf("thanks: %+v", f)
	}
	for _, say := range []string{"Tell me a joke.", "Tell me about yourself.", "Actually, what is homelab doing?", "No, what is the time?"} {
		if f, ok := intent.Match(say, false); ok {
			t.Errorf("%q is for the brain: %+v", say, f)
		}
	}
}

// fastIs says whether a fast match is the intent the set wants (mute is volume's mute).
func fastIs(f intent.Fast, w want) bool {
	c := f.Calls()
	return len(c) == 1 && nameIs(c[0].Name, w.Name) && slotsMatch(c[0], w.Slots)
}

// slotsExact: a fast match gives no slot the set doesn't name (the grammar never adds one).
func slotsExact(f intent.Fast, w want) bool {
	for k := range f.Calls()[0].Args {
		if _, ok := w.Slots[k]; !ok {
			return false
		}
	}
	return true
}

// got is one phrase's result through the brain.
type got struct {
	calls    []intent.Call
	answer   string
	firstOut time.Duration // the first tool call (the card can show then) or the first text
	err      error
}

// Hooks for a backend's own live checks (before the run, and each session's brain), and where its
// brain's sampling lives in chat_extra.
var (
	liveCheck     func(t *testing.T, ctx context.Context, vc *voice.Client)
	liveBrain     func(t *testing.T, model string)
	brainExtra    = func(cfg config.Config) map[string]any { return cfg.ChatExtra }
	setBrainExtra = func(cfg *config.Config, extra map[string]any) { cfg.ChatExtra = extra }
	cfgVoice      string
)

var ticRE = regexp.MustCompile(`(?i)\bas an? (ai|language model)\b|\bi('m| am) an? (ai|language model)\b|task completed successfully|here is the information`)

var helperRE = regexp.MustCompile(`(?i)\?|\b(want me|shall i|should i|can i|let me|i can|happy to|i could|would you like)\b`)

func sentencesIn(s string) int {
	return len(regexp.MustCompile(`[.!?…]+(\s|$)`).FindAllString(strings.TrimSpace(s), -1))
}

var dontKnowRE = regexp.MustCompile(`(?i)(don.?t know|only be guessing|be guessing|do not know|not sure|no idea|can.?t (tell|say|answer|know|check|see|find|look)|cannot|no way to|unable|don.?t have|not able|isn.?t something|beyond what)`)

// TestPhrasesLive runs the set against the configured backend's brain, with vcd's own session
// (instructions, catalogue, chat_extra) and a fixed context line:
//
//	VC_LIVE=1 go test ./internal/intent -run TestPhrasesLive -v
//
// VC_THINKING=on or off sets chat_template_kwargs.enable_thinking. VC_SPOKEN=1 says each phrase with
// the TTS endpoint and hears it with the STT endpoint first: that checks the path is wired, not how
// well it hears you.
func TestPhrasesLive(t *testing.T) {
	if os.Getenv("VC_LIVE") == "" {
		t.Skip("VC_LIVE=1 runs it against the configured backend")
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if th := os.Getenv("VC_THINKING"); th == "on" || th == "off" {
		extra := map[string]any{}
		for k, v := range brainExtra(cfg) {
			extra[k] = v
		}
		extra["chat_template_kwargs"] = map[string]any{"enable_thinking": th == "on"}
		setBrainExtra(&cfg, extra)
	}
	spoken := os.Getenv("VC_SPOKEN") != ""
	cfgVoice = cfg.Voice
	vc := voice.New(cfg)
	ctx := context.Background()
	if liveCheck != nil {
		liveCheck(t, ctx, vc)
	}
	s := load(t)
	type row struct {
		p      phrase
		heard  string
		fast   bool
		g      got
		intent bool // the right intents
		slots  bool // and their slots
		ok     bool
		why    string
	}
	var rows []row
	var outLat, textLat []time.Duration
	for _, p := range s.Phrases {
		r := row{p: p, heard: p.Say}
		if spoken {
			r.heard, err = hear(vc, p.Say)
			if err != nil {
				t.Fatalf("%q: %v", p.Say, err)
			}
		}
		if f, ok := intent.Match(r.heard, false); ok {
			r.fast = true
			r.g.calls = f.Calls()
		} else {
			r.g = ask(t, vc, cfg, s.Context, r.heard)
			if r.g.err != nil { // a busy speech server (the live vcd talks too): once more
				time.Sleep(time.Second)
				r.g = ask(t, vc, cfg, s.Context, r.heard)
			}
			if r.g.err != nil {
				t.Fatalf("%q: %v", p.Say, r.g.err)
			}
			if len(r.g.calls) > 0 {
				outLat = append(outLat, r.g.firstOut)
			} else if r.g.firstOut > 0 {
				textLat = append(textLat, r.g.firstOut)
			}
		}
		r.intent, r.slots, r.why = score(p, r.g)
		if ticRE.MatchString(r.g.answer) { // human-like: no robotic tics, ever
			r.slots, r.why = false, "a robotic tic"
		}
		r.ok = r.intent && r.slots
		rows = append(rows, r)
	}

	n := map[string]int{}
	pass := map[string]int{}
	intents, slots, actions := 0, 0, 0
	bIntents, bSlots, bActions := 0, 0, 0 // the brain's alone: fast phrases pass by construction
	for _, r := range rows {
		n[r.p.Kind]++
		if r.ok {
			pass[r.p.Kind]++
		}
		if r.p.Kind == "fast" || r.p.Kind == "intent" {
			actions++
			if r.intent {
				intents++
			}
			if r.intent && r.slots {
				slots++
			}
			if r.p.Kind == "intent" {
				bActions++
				if r.intent {
					bIntents++
				}
				if r.intent && r.slots {
					bSlots++
				}
			}
		}
		mark := "ok  "
		if !r.ok {
			mark = "FAIL"
		}
		heard := ""
		if spoken && r.heard != r.p.Say {
			heard = fmt.Sprintf(" (heard %q)", r.heard)
		}
		because := ""
		if !r.ok && r.why != "" {
			because = " [" + r.why + "]"
		}
		t.Logf("%s %-8s %q%s -> %s%s", mark, r.p.Kind, r.p.Say, heard, describe(r.g, r.fast), because)
	}
	pct := func(a, b int) float64 { return 100 * float64(a) / float64(max(b, 1)) }
	mode := "thinking off"
	if k, _ := brainExtra(cfg)["chat_template_kwargs"].(map[string]any); k["enable_thinking"] == true {
		mode = "thinking on"
	}
	if strings.Contains(strings.Join(cfg.Brain.Args, " "), "--reasoning on") {
		mode = "thinking on (llama-server --reasoning on)"
	}
	t.Logf("%s, %d phrases%s", mode, len(rows), map[bool]string{true: ", spoken by the TTS endpoint and heard by the STT endpoint"}[spoken])
	t.Logf("exact intent %.1f%% (%d/%d), exact slots %.1f%% (%d/%d) [targets 95%% / 90%%]",
		pct(intents, actions), intents, actions, pct(slots, actions), slots, actions)
	t.Logf("the brain's phrases alone: intent %.1f%% (%d/%d), slots %.1f%% (%d/%d)", pct(bIntents, bActions), bIntents,
		bActions, pct(bSlots, bActions), bSlots, bActions)
	t.Logf("talk only with no action %d/%d; grounded answers %d/%d; unknown, said it doesn't know %d/%d",
		pass["talk"], n["talk"], pass["grounded"], n["grounded"], pass["unknown"], n["unknown"])
	t.Logf("brain to first tool call p50 %v p90 %v (n %d); to first text p50 %v p90 %v (n %d)",
		pq(outLat, 50), pq(outLat, 90), len(outLat), pq(textLat, 50), pq(textLat, 90), len(textLat))
	if pct(intents, actions) < 95 || pct(slots, actions) < 90 {
		t.Errorf("under the bar: intent %.1f%%, slots %.1f%%", pct(intents, actions), pct(slots, actions))
	}
	if pass["talk"] != n["talk"] {
		t.Errorf("a talk-only phrase became an action")
	}
	if pass["unknown"] != n["unknown"] {
		t.Errorf("the brain answered %d question(s) from its own knowledge", n["unknown"]-pass["unknown"])
	}
	if pct(pass["grounded"], n["grounded"]) < 90 {
		t.Errorf("grounded answers %d/%d", pass["grounded"], n["grounded"])
	}
}

// ask sends one phrase to a fresh computer session, as vcd sends a turn, and collects the response.
func ask(t *testing.T, vc *voice.Client, cfg config.Config, contextLine, text string) got {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s, err := vc.Dial(ctx, daemon.ComputerSession(cfg))
	if err != nil {
		return got{err: err}
	}
	defer s.Close()
	if liveBrain != nil {
		liveBrain(t, s.Model())
	}
	var g got
	if err := s.Say(ctx, "user", contextLine); err != nil {
		return got{err: err}
	}
	if err := s.Say(ctx, "user", text); err != nil {
		return got{err: err}
	}
	t0 := time.Now()
	if err := s.Respond(ctx); err != nil {
		return got{err: err}
	}
	var b strings.Builder
	for {
		select {
		case ev, ok := <-s.Events:
			if !ok {
				return got{err: fmt.Errorf("the session ended")}
			}
			switch ev.Type {
			case "response.output_audio_transcript.delta":
				if g.firstOut == 0 {
					g.firstOut = time.Since(t0)
				}
				b.WriteString(ev.Delta + " ")
			case "response.function_call_arguments.done":
				if len(g.calls) == 0 {
					g.firstOut = time.Since(t0)
				}
				c, err := intent.ParseCall(ev.CallID, ev.Name, ev.Arguments)
				if err != nil {
					c.Args = nil
				}
				g.calls = append(g.calls, c)
			case "response.done":
				g.answer = strings.TrimSpace(regexp.MustCompile(`</?think>`).ReplaceAllString(b.String(), ""))
				return g
			case "error":
				if ev.Error != nil {
					return got{err: fmt.Errorf("%s", ev.Error.Message)}
				}
			}
		case <-ctx.Done():
			return got{err: ctx.Err()}
		}
	}
}

// hear says text with the TTS endpoint and transcribes it with the STT endpoint.
func hear(vc *voice.Client, text string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rd, err := vc.Speech(ctx, text, cfgVoice)
	if err != nil {
		return "", err
	}
	pcm, err := io.ReadAll(rd)
	rd.Close()
	if err != nil {
		return "", err
	}
	return vc.Transcribe(ctx, audio.To16k(pcm))
}

// score says whether the result has the right intents (in any order) and their slots, or, for a
// phrase with no intent, whether no action came and the answer is what its kind needs.
func score(p phrase, g got) (intentOK, slotsOK bool, why string) {
	switch p.Kind {
	case "talk":
		// a helper, not a command box: two sentences or more, and an offer or a question
		return len(g.calls) == 0, helperRE.MatchString(g.answer) && sentencesIn(g.answer) >= 2, "doesn't sound like a helper"
	case "grounded":
		if len(g.calls) > 0 {
			return false, false, "an action"
		}
		return true, hasAny(g.answer, p.Has), "the answer lacks the fact"
	case "unknown":
		if len(g.calls) > 0 {
			return false, false, "an action"
		}
		for _, bad := range p.Not {
			if strings.Contains(strings.ToLower(g.answer), bad) {
				return true, false, "made up: " + bad
			}
		}
		if n := madeUpNumber(p.Say, g.answer); n != "" {
			return true, false, "made up a number: " + n
		}
		return true, dontKnowRE.MatchString(g.answer), "didn't say it doesn't know"
	}
	if len(g.calls) != len(p.Intents) {
		return false, false, ""
	}
	used := make([]bool, len(g.calls))
	intentOK, slotsOK = true, true
	for _, w := range p.Intents {
		found, slots := false, false
		for i, c := range g.calls {
			if used[i] || !nameIs(c.Name, w.Name) {
				continue
			}
			used[i], found, slots = true, true, slotsMatch(c, w.Slots)
			break
		}
		intentOK = intentOK && found
		slotsOK = slotsOK && slots
	}
	return intentOK, slotsOK, ""
}

func nameIs(got, want string) bool {
	for _, n := range strings.Split(want, "|") {
		if n == got {
			return true
		}
	}
	return false
}

func slotsMatch(c intent.Call, want map[string]any) bool {
	for k, w := range want {
		var v any
		var has bool
		if k == "duration_s" {
			m, okM := c.Int("minutes")
			s, okS := c.Int("seconds")
			v, has = float64(m*60+s), okM || okS
		} else {
			for _, kk := range strings.Split(k, "|") {
				if x, ok := c.Args[kk]; ok {
					v, has = x, true
					break
				}
			}
		}
		if !has || !valueIs(c, v, w) {
			return false
		}
	}
	return true
}

func valueIs(c intent.Call, v, w any) bool {
	switch w := w.(type) {
	case bool:
		switch v := v.(type) {
		case bool:
			return v == w
		case string:
			return v == fmt.Sprint(w)
		}
		return false
	case []any:
		for _, x := range w {
			if valueIs(c, v, x) {
				return true
			}
		}
		return false
	case float64:
		switch v := v.(type) {
		case float64:
			return v == w
		case string:
			n, ok := intent.Number(v)
			return ok && float64(n) == w
		}
		return false
	case string:
		got := plain(fmt.Sprint(v))
		switch {
		case w == "*":
			return got != ""
		case strings.HasPrefix(w, "~"):
			return strings.Contains(got, plain(w[1:]))
		}
		return got == plain(w)
	}
	return false
}

func plain(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}), " ")
}

func hasAny(answer string, xs []string) bool {
	a := strings.ToLower(answer)
	for _, x := range xs {
		if strings.Contains(a, x) {
			return true
		}
	}
	return false
}

func describe(g got, fast bool) string {
	var parts []string
	for _, c := range g.calls {
		b, _ := json.Marshal(c.Args)
		parts = append(parts, c.Name+string(b))
	}
	out := strings.Join(parts, " + ")
	if fast {
		out = "fast " + out
	}
	if g.answer != "" {
		if out != "" {
			out += " "
		}
		out += fmt.Sprintf("says %q", g.answer)
	}
	if out == "" {
		out = "(nothing)"
	}
	return out
}

func pq(xs []time.Duration, q int) time.Duration {
	if len(xs) == 0 {
		return 0
	}
	s := append([]time.Duration{}, xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[(len(s)-1)*q/100].Round(time.Millisecond)
}

var numberWordRE = regexp.MustCompile(`(?i)\b(\d[\d,.]*|zero|one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety|hundred|thousand|million|billion)\b`)

// madeUpNumber is a number in an unknown question's answer that the question doesn't have: any answer
// to "what is twelve times seven?" with a number in it is made up, the wrong one ("seventy-two") too.
// "one" is let through ("that one", "no one").
func madeUpNumber(question, answer string) string {
	in := map[string]bool{"one": true}
	for _, w := range numberWordRE.FindAllString(strings.ToLower(question), -1) {
		in[strings.TrimRight(w, ".,")] = true
	}
	for _, w := range numberWordRE.FindAllString(strings.ToLower(answer), -1) {
		if w = strings.TrimRight(w, ".,"); !in[w] {
			return w
		}
	}
	return ""
}
