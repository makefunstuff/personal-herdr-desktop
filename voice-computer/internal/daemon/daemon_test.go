package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"herdr-voice-computer/internal/audio"
	"herdr-voice-computer/internal/config"
	"herdr-voice-computer/internal/herdr"
	"herdr-voice-computer/internal/macos"
	"herdr-voice-computer/internal/pomo"
	"herdr-voice-computer/internal/voice/voicetest"
)

// The mic gives a steady loud level at the real rate (16 kHz PCM16) until killed; the speaker swallows
// audio; osascript is never run.
const micTone = `while :; do head -c 3200 /dev/zero | tr "\\0" "@"; sleep 0.1; done`

func init() {
	audio.RecordCmd = func() *exec.Cmd { return exec.Command("sh", "-c", micTone) }
	audio.PlayCmd = func() *exec.Cmd { return exec.Command("sh", "-c", "cat > /dev/null") }
	macos.Script = func(ctx context.Context, src string) (string, error) {
		if src == "get volume settings" {
			return "output volume:40, input volume:50, alert volume:100, output muted:false", nil
		}
		return "", nil
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fakeHerdr answers herdr-app ls|status from a world it holds, and records every other command.
type fakeHerdr struct {
	mu     sync.Mutex
	ls     string
	status string
	ran    []string
	screen string
}

func (f *fakeHerdr) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case len(args) == 1 && args[0] == "ls":
		return []byte(f.ls), nil
	case len(args) == 1 && args[0] == "status":
		return []byte(f.status), nil
	case len(args) > 0 && args[0] == "read":
		return []byte(f.screen), nil
	}
	f.ran = append(f.ran, name+" "+strings.Join(args, " "))
	return []byte("{}"), nil
}

func (f *fakeHerdr) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.ran...)
}

// setStatus changes one terminal's status in the world.
func (f *fakeHerdr) setStatus(t *testing.T, terminal, status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var w map[string]any
	json.Unmarshal([]byte(f.ls), &w)
	for _, sp := range w["spaces"].([]any) {
		for _, tb := range sp.(map[string]any)["tabs"].([]any) {
			for _, x := range tb.(map[string]any)["terminals"].([]any) {
				if m := x.(map[string]any); m["terminal"] == terminal {
					m["status"] = status
				}
			}
		}
	}
	b, _ := json.Marshal(w)
	f.ls = string(b)
}

type rig struct {
	t    *testing.T
	f    *voicetest.Fake
	h    *fakeHerdr
	d    *Daemon
	logs *syncBuf
	cfg  config.Config
}

func newRig(t *testing.T, edit func(*config.Config)) *rig {
	t.Helper()
	f := voicetest.New()
	t.Cleanup(f.Close)
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	os.WriteFile(key, []byte(f.Key+"\n"), 0o600)
	cfg := config.Default()
	ep := config.Endpoint{URL: f.URL, Model: "test-chat", KeyFile: key}
	cfg.STT, cfg.TTS, cfg.Chat = ep, ep, ep
	cfg.OverlayHideS = 1
	cfg.Think, cfg.Learn = false, false // their own brain requests are tested apart
	cfg.NotesFile = filepath.Join(dir, "notes.md")
	cfg.MemoryFile = filepath.Join(dir, "memory.md")
	cfg.NotesDir = filepath.Join(dir, "desk-notes")
	cfg.Search.Enabled = false
	cfg.Embed.URL = ""
	cfg.Owner = "Alex"
	cfg.Instructions = config.Instructions(cfg.Name, cfg.Owner, "") // not this machine's git name
	cfg.Shepherdrs = nil
	cfg.AnnounceGapS = 0
	if edit != nil {
		edit(&cfg)
	}
	ls, err := os.ReadFile("../herdr/testdata/ls.json")
	if err != nil {
		t.Fatal(err)
	}
	h := &fakeHerdr{ls: string(ls), status: `{"layout": "workbench", "space": "sp1", "tab": "tb1", "agent": "t3"}`,
		screen: "Running the test suite\n12 passed, 1 failing in parser_test.go"}
	t.Setenv("VC_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("VC_DATA_DIR", filepath.Join(dir, "data"))
	if edit != nil {
		edit(&cfg) // (again: a test may set what the defaults above cleared)
	}
	logs := &syncBuf{}
	d := New(cfg, log.New(logs, "", 0))
	d.herdr = &herdr.Client{App: "herdr-app", HC: "hc", Run: h.run}
	d.LoadPrefs(filepath.Join(dir, "prefs.json"))
	d.checkHealth(context.Background(), true)
	ctx, cancel := context.WithCancel(context.Background())
	go d.playReplies(ctx)
	t.Cleanup(cancel)
	t.Cleanup(d.shutdown)
	rg := &rig{t: t, f: f, h: h, d: d, logs: logs, cfg: cfg}
	rg.waitFor("the warm-up", func() bool { return strings.Contains(logs.String(), `"event":"warm"`) })
	return rg
}

// waitFor polls cond for up to 5 s.
func (r *rig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.t.Fatalf("timed out waiting for %s; state %+v; log:\n%s", what, r.d.Snapshot(), r.logs)
}

// say sends a typed turn and waits until it has settled (or a card is open).
func (r *rig) say(text string) State {
	r.t.Helper()
	if err := r.d.TurnText(text); err != nil {
		r.t.Fatal(err)
	}
	r.waitFor("the turn "+text, func() bool {
		s := r.d.Snapshot()
		last := false
		for i := max(0, len(s.Turns)-3); i < len(s.Turns); i++ {
			last = last || s.Turns[i].You == text
		}
		return (last || s.Card != nil) && s.State != Thinking && !r.d.batchOpenNoCard()
	})
	return r.d.Snapshot()
}

func (d *Daemon) batchOpenNoCard() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return (d.batch != nil && d.st.Card == nil) || d.turn != nil
}

func (r *rig) spoken() []string {
	r.f.Lock()
	defer r.f.Unlock()
	return append([]string{}, r.f.Spoken...)
}

// chats are the brain's streamed turn requests (not the warm-up or a summary).
func (r *rig) chats() []map[string]any {
	r.f.Lock()
	defer r.f.Unlock()
	var out []map[string]any
	for _, c := range r.f.Chats {
		if s, _ := c["stream"].(bool); s {
			out = append(out, c)
		}
	}
	return out
}

// plainChats are the requests outside the session (summaries), not the warm-up.
func (r *rig) plainChats() []map[string]any {
	r.f.Lock()
	defer r.f.Unlock()
	var out []map[string]any
	for _, c := range r.f.Chats {
		if s, _ := c["stream"].(bool); !s && c["max_tokens"] != float64(1) {
			out = append(out, c)
		}
	}
	return out
}

func has(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// A brain turn: heard, answered by the brain, kept in memory (never on disk), and replayed into a new
// session once the old one closed.
func TestBrainTurnAndHistoryInMemory(t *testing.T) {
	r := newRig(t, nil)
	r.f.Lock()
	r.f.Answer = []string{"I am Herdr, the voice of this Mac."}
	r.f.Unlock()
	s := r.say("who are you")
	if len(s.Turns) != 1 || s.Turns[0].You != "who are you" || s.Turns[0].Computer != "I am Herdr, the voice of this Mac." {
		t.Fatalf("turns: %+v", s.Turns)
	}
	req, _ := json.Marshal(r.chats()[0])
	for _, want := range []string{`"role":"system"`, `You are Herdr, Alex's executive assistant inside Herdr`, `"name":"send_to_agent"`,
		`(Context, not said by the owner: time `, `layout workbench`, `focused agent Work (working)`, `who are you`} {
		if !strings.Contains(string(req), want) {
			t.Errorf("the chat request lacks %s", want)
		}
	}
	// The session goes (idle close); the next turn replays the history into a new one.
	r.d.mu.Lock()
	r.d.dropSessionLocked()
	r.d.mu.Unlock()
	r.say("and what can you do")
	req, _ = json.Marshal(r.chats()[1]["messages"])
	if !strings.Contains(string(req), `"content":"I am Herdr, the voice of this Mac.","role":"assistant"`) ||
		!strings.Contains(string(req), `who are you`) {
		t.Fatalf("history not replayed: %s", req)
	}
	// Memory is RAM only: the log has timings, never words.
	if l := r.logs.String(); strings.Contains(l, "who are you") || strings.Contains(l, "voice of this Mac") {
		t.Fatalf("words in the log:\n%s", l)
	}
	if !strings.Contains(r.logs.String(), `"kind":"brain"`) {
		t.Fatalf("no turn logged:\n%s", r.logs)
	}
}

// History keeps the last `history` turns.
func TestHistoryIsCapped(t *testing.T) {
	r := newRig(t, func(c *config.Config) { c.History = 2 })
	for _, s := range []string{"one", "two", "three"} {
		r.say(s)
	}
	if s := r.d.Snapshot(); len(s.Turns) != 2 || s.Turns[0].You != "two" {
		t.Fatalf("turns: %+v", s.Turns)
	}
}

// Fast commands run without the brain and say their outcome.
func TestFastCommandsNeedNoBrain(t *testing.T) {
	r := newRig(t, nil)
	r.say("switch to the fortress layout")
	r.say("Show the shepherder panel.")
	r.say("hide it")
	r.say("focus the semantic grep agent")
	r.say("go to homelab")
	r.say("next agent")
	cmds := r.h.commands()
	for _, want := range []string{`herdr-app do layout.switch {"name":"fortress"}`, `herdr-app do layout.show {"id":"shepherdr"}`,
		`herdr-app do layout.hide {"id":"shepherdr"}`, `herdr-app do agent.focus {"terminal":"gpubox/t82"}`,
		`herdr-app do space.select {"id":"gpubox/sp6"}`, `herdr-app do agent.next`} {
		if !has(cmds, want) {
			t.Errorf("didn't run %s: %q", want, cmds)
		}
	}
	if n := len(r.chats()); n != 0 {
		t.Fatalf("the brain was asked %d times", n)
	}
	r.waitFor("the outcome spoken", func() bool { return hasPart(r.spoken(), "ortress") })
	if !strings.Contains(r.logs.String(), `"fast_match":true`) {
		t.Fatalf("not logged as fast:\n%s", r.logs)
	}
}

// Sending words to an agent asks first: "no" sends nothing, "yes" sends with hc send.
func TestSendAsksFirst(t *testing.T) {
	r := newRig(t, nil)
	s := r.say("Tell the homelab agent to run the tests.")
	if s.Card == nil || s.Card.Text != "run the tests." || !s.Card.Voice || !strings.Contains(s.Card.Ask, "homelab") {
		t.Fatalf("card: %+v", s.Card)
	}
	r.say("no")
	for _, c := range r.h.commands() {
		if strings.HasPrefix(c, "hc send") {
			t.Fatalf("sent after a no: %s", c)
		}
	}
	if s := r.d.Snapshot(); s.Card != nil {
		t.Fatalf("card still open: %+v", s.Card)
	}
	r.say("reply to semantic grep sema 4: yes, merge it")
	r.say("yes")
	r.waitFor("the send", func() bool { return has(r.h.commands(), "hc send gpubox/t82 yes, merge it") })
}

// "Tell me a joke" names no agent: the brain hears it.
func TestTellMeGoesToTheBrain(t *testing.T) {
	r := newRig(t, nil)
	r.say("tell my parents hello")
	if len(r.chats()) != 1 {
		t.Fatalf("the brain wasn't asked")
	}
}

// A brain tool call runs, and vcd says the outcome itself: no second brain round, and what agents
// wrote never goes into the brain's history.
func TestBrainToolCallOutcomeIsSpokenByVcd(t *testing.T) {
	r := newRig(t, nil)
	r.f.Lock()
	r.f.ToolCalls = [][]voicetest.ToolCall{{{Name: "agent_doing", Args: `{"agent": "homelab"}`}}}
	r.f.Answer = []string{"The homelab agent runs its tests; one fails."}
	r.f.Unlock()
	r.say("could you check on homelab for me")
	// one chat for the turn, one for the summary (outside the session), none for an outcome round
	r.waitFor("the summary spoken", func() bool { return has(r.spoken(), "The homelab agent runs its tests; one fails.") })
	if n := len(r.chats()); n != 1 {
		t.Fatalf("%d turn chats", n)
	}
	sums := r.plainChats()
	if len(sums) != 1 {
		t.Fatalf("%d summaries", len(sums))
	}
	sum, _ := json.Marshal(sums[0])
	if !strings.Contains(string(sum), "parser_test.go") || strings.Contains(string(sum), `"tools"`) {
		t.Fatalf("the summary request: %s", sum)
	}
	r.say("thanks")
	r.f.Lock()
	r.f.Answer = []string{"Okay."}
	r.f.Unlock()
	r.say("and now?")
	hist, _ := json.Marshal(r.chats()[1]["messages"])
	if strings.Contains(string(hist), "parser_test") || !strings.Contains(string(hist), "told the owner what the agent is doing") {
		t.Fatalf("the brain's history: %s", hist)
	}
}

// Announcements: off on the first launch; once on, a finished and a blocked agent are said together
// in one line through the reply queue.
func TestAnnouncements(t *testing.T) {
	r := newRig(t, nil)
	if !r.d.Snapshot().RepliesMuted {
		t.Fatal("not quiet on the first launch")
	}
	w := func() herdr.World {
		w, _ := r.d.herdr.World(context.Background())
		return w
	}
	now := time.Now()
	r.d.look(w(), nil, now) // primes
	r.h.setStatus(t, "gpubox/t81", "done")
	r.d.look(w(), nil, now.Add(time.Second))
	r.d.look(w(), nil, now.Add(10*time.Second))
	r.d.mu.Lock()
	q := len(r.d.replyQ)
	r.d.mu.Unlock()
	if q != 0 {
		t.Fatal("announced while quiet")
	}
	r.say("talk to me")
	r.h.setStatus(t, "gpubox/t81", "working")
	r.d.look(w(), nil, now.Add(20*time.Second))
	r.h.setStatus(t, "gpubox/t81", "done")
	r.h.setStatus(t, "gpubox/t92", "blocked")
	r.d.look(w(), []need{{src: "this Mac", who: "homelab", what: "Merge the login fix?", key: "k1"}}, now.Add(21*time.Second))
	r.d.look(w(), []need{{src: "this Mac", who: "homelab", what: "Merge the login fix?", key: "k1"}}, now.Add(30*time.Second))
	want := "Homelab is asking you something: merge the login fix. Semantic grep nvim SEMA 3 finished."
	r.waitFor("the announcement", func() bool { return has(r.spoken(), want) })
	if s := r.d.Snapshot(); len(s.Inbox) != 2 || s.Inbox[0].What != "Merge the login fix?" || s.Inbox[1].Terminal != "gpubox/t82" {
		t.Fatalf("inbox: %+v", s.Inbox)
	}
	b, _ := os.ReadFile(r.d.prefs)
	if string(b) != `{"quiet":false}` {
		t.Fatalf("prefs: %s", b)
	}
}

func TestBriefingAndNeeds(t *testing.T) {
	r := newRig(t, nil)
	w, _ := r.d.herdr.World(context.Background())
	r.d.look(w, nil, time.Now())
	r.h.setStatus(t, "gpubox/t81", "idle")
	_, say := r.d.briefing(context.Background(), mustWorld(r))
	if !strings.HasPrefix(say, "Semantic grep nvim SEMA 4 is waiting on you. Work's working. Semantic grep nvim SEMA 3 finished since ") {
		t.Fatalf("briefing: %q", say)
	}
	_, say = r.d.briefing(context.Background(), mustWorld(r))
	if !strings.Contains(say, "Semantic grep nvim SEMA 4 is waiting on you. Work's working.") || strings.Contains(say, "finished") {
		t.Fatalf("second briefing: %q", say)
	}
	s := r.say("what needs me")
	if !strings.HasPrefix(s.Answer, "Semantic grep nvim SEMA 4 is waiting on you.") {
		t.Fatalf("needs: %q", s.Answer)
	}
}

func mustWorld(r *rig) herdr.World {
	w, err := r.d.herdr.World(context.Background())
	if err != nil {
		r.t.Fatal(err)
	}
	return w
}

// A Shepherdr's needs are read from its snapshot and said; the question's words never reach the brain.
func TestShepherdrNeeds(t *testing.T) {
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"places": [{"id": "p1", "name": "homelab", "status": "need", "chat": "c1"}],
			"tasks": [], "needs": [{"chat": "c1", "question": {"id": "q", "kind": "choice", "title": "Merge **PR #12** now?"}}]}`))
	}))
	defer sh.Close()
	r := newRig(t, func(c *config.Config) {
		c.Shepherdrs = []config.Shepherdr{{Name: "this Mac", URL: sh.URL}}
	})
	r.f.Lock()
	r.f.ToolCalls = [][]voicetest.ToolCall{{{Name: "needs_me", Args: `{}`}}}
	r.f.Unlock()
	s := r.say("anything for me")
	if !strings.Contains(s.Answer, "Homelab needs you: it asks merge the PR now.") {
		t.Fatalf("answer: %q", s.Answer)
	}
	r.f.Lock()
	r.f.Answer = []string{"Okay."}
	r.f.Unlock()
	r.say("ok then")
	hist, _ := json.Marshal(r.chats()[1]["messages"])
	if strings.Contains(string(hist), "Merge") {
		t.Fatalf("the question reached the brain: %s", hist)
	}
}

func TestNotesAndReminders(t *testing.T) {
	r := newRig(t, nil)
	r.say("note: buy milk")
	r.say("Take a note that the demo is Friday.")
	day := filepath.Join(r.cfg.NotesDir, "daily", time.Now().Format("2006-01-02")+".md")
	b, _ := os.ReadFile(day)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 4 || lines[0] != "# "+time.Now().Format("2006-01-02") || !strings.HasSuffix(lines[2], " buy milk") || !strings.HasPrefix(lines[2], "- ") {
		t.Fatalf("notes file: %q", b)
	}
	s := r.say("what are my notes today")
	if s.Answer != "2 notes today. buy milk. the demo is Friday." {
		t.Fatalf("notes: %q", s.Answer)
	}
	if s := r.say("read my last note"); !strings.Contains(s.Answer, "the demo is Friday") {
		t.Fatalf("last note: %q", s.Answer)
	}
	r.say("remind me in 20 minutes to stretch")
	r.say("remind me in 2 seconds to drink water")
	if s := r.d.Snapshot(); len(s.Reminders) != 2 {
		t.Fatalf("reminders: %+v", s.Reminders)
	}
	r.waitFor("the reminder", func() bool { s := r.d.Snapshot(); return s.Toast != nil && len(s.Reminders) == 1 })
	if s := r.d.Snapshot(); len(s.Reminders) != 1 || s.Toast == nil || s.Toast.Body != "Reminder: drink water" {
		t.Fatalf("after: %+v %+v", s.Reminders, s.Toast)
	}
	s = r.say("cancel the reminder about stretching")
	if !strings.Contains(s.Answer, "reminder") || !strings.Contains(s.Answer, "to stretch") || len(s.Reminders) != 0 {
		t.Fatalf("cancel: %q %+v", s.Answer, s.Reminders)
	}
	// both reminders are in the notes, ticked: one rang, one was cancelled
	b, _ = os.ReadFile(day)
	if !strings.Contains(string(b), "- [x] ") || strings.Count(string(b), "- [x] ") != 2 || strings.Contains(string(b), "- [ ] ") {
		t.Fatalf("reminders in the notes (%s):\n%s", day, b)
	}
}

// A press of the talk key: heard through the STT endpoint, then routed.
func TestTalkKeyTurn(t *testing.T) {
	r := newRig(t, nil)
	r.f.Lock()
	r.f.Transcripts = []string{"theme pixel"}
	r.f.Unlock()
	if !r.d.Talk(time.Now()) {
		t.Fatal("the mic didn't open")
	}
	r.waitFor("the mic", func() bool {
		r.d.mu.Lock()
		defer r.d.mu.Unlock()
		return r.d.rec != nil && !r.d.rec.OpenedAt().IsZero()
	})
	time.Sleep(500 * time.Millisecond)
	r.d.Talk(time.Now())
	r.waitFor("the layout", func() bool { return has(r.h.commands(), `herdr-app do layout.switch {"name":"pixel"}`) })
	r.waitFor("the log", func() bool { return strings.Contains(r.logs.String(), `"asr_ms"`) })
}

// The mod's HTTP way in wants the token.
func TestHTTPNeedsTheToken(t *testing.T) {
	t.Setenv("VC_STATE_DIR", t.TempDir())
	r := newRig(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.d.ServeHTTP(ctx, 18799)
	time.Sleep(200 * time.Millisecond)
	resp, err := http.Get("http://127.0.0.1:18799/state")
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("no token: %v %v", resp, err)
	}
	tok, _ := os.ReadFile(TokenPath())
	req, _ := http.NewRequest("POST", "http://127.0.0.1:18799/cmd", strings.NewReader(`{"cmd":"quiet","arg":"off"}`))
	req.Header.Set("X-VC-Token", strings.TrimSpace(string(tok)))
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 || r.d.Snapshot().RepliesMuted {
		t.Fatalf("with the token: %v %v", resp, err)
	}
}

// An open confirm takes "nope", "don't" and "cancel" before anything else: nothing is sent, and the
// turn says it asked.
func TestConfirmAnswersFirst(t *testing.T) {
	r := newRig(t, nil)
	for _, no := range []string{"Nope.", "Don't.", "No, don't send it.", "cancel"} {
		s := r.say("tell the homelab agent to run the tests")
		if s.Card == nil || !strings.Contains(s.Answer, "homelab") || !strings.HasSuffix(s.Answer, "run the tests? Say yes or no.") {
			t.Fatalf("asked: %+v %q", s.Card, s.Answer)
		}
		s = r.say(no)
		r.waitFor("the card to close", func() bool { return r.d.Snapshot().Card == nil })
		s = r.d.Snapshot()
		asked := false
		for _, x := range s.Turns {
			asked = asked || (x.You == "tell the homelab agent to run the tests" && strings.Contains(x.Computer, "run the tests? Say yes or no."))
		}
		if !asked {
			t.Fatalf("%q: the turns %+v", no, s.Turns)
		}
	}
	for _, c := range r.h.commands() {
		if strings.HasPrefix(c, "hc send") {
			t.Fatalf("sent: %s", c)
		}
	}
	if len(r.chats()) != 0 {
		t.Fatalf("an answer to the card reached the brain")
	}
}

// A mic that gives digital silence (macOS blocks it) turns the computer off with why, not an empty turn.
func TestBlockedMicSaysWhy(t *testing.T) {
	old := audio.RecordCmd
	audio.RecordCmd = func() *exec.Cmd {
		return exec.Command("sh", "-c", `while :; do head -c 3200 /dev/zero; sleep 0.1; done`)
	}
	defer func() { audio.RecordCmd = old }()
	t.Setenv("VC_LAUNCHED_BY", "Herdr Voice")
	r := newRig(t, nil)
	r.d.Talk(time.Now())
	r.waitFor("the mic", func() bool {
		r.d.mu.Lock()
		defer r.d.mu.Unlock()
		return r.d.rec != nil && !r.d.rec.OpenedAt().IsZero()
	})
	time.Sleep(400 * time.Millisecond)
	r.d.Talk(time.Now())
	r.waitFor("off", func() bool { return r.d.Snapshot().Off == OffMic })
	if s := r.d.Snapshot(); s.State != Off || !strings.Contains(s.Why, "allow Herdr Voice in System Settings → Privacy & Security → Microphone") {
		t.Fatalf("state: %+v", s)
	}
}

// One voice at a time: an announcement waits while the mic is open and while the turn's answer is
// said, then comes after a moment of quiet; a stale one is dropped.
func TestAnnouncementWaitsForTheTurn(t *testing.T) {
	r := newRig(t, nil)
	r.say("talk to me")
	r.waitFor("the reply to talk to me", func() bool { return len(r.spoken()) == 1 })
	r.f.Lock()
	r.f.Transcripts = []string{"theme pixel"}
	r.f.Unlock()
	r.d.Talk(time.Now())
	r.waitFor("the mic", func() bool {
		r.d.mu.Lock()
		defer r.d.mu.Unlock()
		return r.d.rec != nil && !r.d.rec.OpenedAt().IsZero()
	})
	r.d.queueReply(agentReply{from: r.cfg.Name, text: "Homelab finished.", at: time.Now(), sent: time.Now()})
	r.d.queueReply(agentReply{from: r.cfg.Name, text: "Old news.", at: time.Now().Add(-3 * time.Minute)})
	time.Sleep(2500 * time.Millisecond)
	if has(r.spoken(), "Homelab finished.") {
		t.Fatal("an announcement played while the mic was open")
	}
	r.d.Talk(time.Now())
	r.waitFor("both", func() bool { return has(r.spoken(), "Homelab finished.") })
	sp := r.spoken()
	if len(sp) != 3 || !strings.Contains(strings.ToLower(sp[1]), "pixel") || sp[2] != "Homelab finished." {
		t.Fatalf("spoken in this order: %q", sp)
	}
}

// Pressing talk stops an announcement at once.
func TestTalkStopsAnAnnouncement(t *testing.T) {
	r := newRig(t, nil)
	r.say("talk to me")
	r.waitFor("the reply to talk to me", func() bool { return len(r.spoken()) == 1 })
	r.f.Lock()
	r.f.SpeechDelay = 3 * time.Second // the announcement's speech is still coming
	r.f.Unlock()
	time.Sleep(2 * time.Second) // past the quiet after the owner's press
	r.d.queueReply(agentReply{from: r.cfg.Name, text: "Homelab finished.", at: time.Now(), sent: time.Now()})
	r.waitFor("the announcement to start", func() bool { r.d.mu.Lock(); defer r.d.mu.Unlock(); return r.d.replyCur != nil })
	r.d.Talk(time.Now())
	r.d.mu.Lock()
	cur, rec := r.d.replyCur, r.d.rec
	r.d.mu.Unlock()
	if cur != nil || rec == nil {
		t.Fatalf("the announcement goes on over the mic: %v %v", cur, rec)
	}
	r.d.Cancel()
}

func hasPart(xs []string, part string) bool {
	for _, x := range xs {
		if strings.Contains(x, part) {
			return true
		}
	}
	return false
}

// The secretary's replies vary: never the same one twice in a row, and the owner's name only sometimes.
func TestRepliesVary(t *testing.T) {
	r := newRig(t, nil)
	prev, named, seen := "", 0, map[string]bool{}
	for i := 0; i < 60; i++ {
		x := r.d.phrase("thanks", nil)
		if x == prev {
			t.Fatalf("said %q twice in a row", x)
		}
		if strings.Contains(x, "Alex") {
			named++
		}
		if strings.Contains(x, "{") {
			t.Fatalf("a placeholder left: %q", x)
		}
		prev, seen[x] = x, true
	}
	if len(seen) < 3 || named == 0 || named > 40 {
		t.Fatalf("%d variants, the name %d times of 60", len(seen), named)
	}
	if x := r.d.phrase("layout", map[string]string{"layout": "fortress"}); !strings.Contains(strings.ToLower(x), "fortress") {
		t.Fatalf("layout: %q", x)
	}
}

// Memory: "remember that …" keeps a fact (in the third person), style feedback a style note with its
// acknowledgment, a secret is refused, the next session's instructions carry it, "forget" takes it out.
func TestMemory(t *testing.T) {
	r := newRig(t, nil)
	s := r.say("Remember that my main project is homelab.")
	if !hasPart([]string{s.Answer}, "") || s.Answer == "" {
		t.Fatalf("no acknowledgment")
	}
	s = r.say("be shorter")
	if s.Answer != "Got it, shorter from now on." && s.Answer != "Sure, I'll keep it brief." {
		t.Fatalf("style ack: %q", s.Answer)
	}
	s = r.say("remember my password is hunter2")
	if !strings.Contains(s.Answer, "password manager") {
		t.Fatalf("secret: %q", s.Answer)
	}
	b, _ := os.ReadFile(r.cfg.MemoryFile)
	if !strings.Contains(string(b), "- Alex's main project is homelab.") || !strings.Contains(string(b), "- Keep answers very short.") ||
		strings.Contains(string(b), "hunter2") {
		t.Fatalf("memory file:\n%s", b)
	}
	r.say("don't call me that")
	for i := 0; i < 20; i++ {
		if x := r.d.phrase("thanks", nil); strings.Contains(x, "Alex") || strings.Contains(x, "{") {
			t.Fatalf("used the name after being asked not to: %q", x)
		}
	}
	s = r.say("what do you remember about me")
	if !strings.Contains(s.Answer, "Alex's main project is homelab") {
		t.Fatalf("recall: %q", s.Answer)
	}
	r.say("who am I")
	req, _ := json.Marshal(r.chats()[0]["messages"])
	for _, want := range []string{"What you know from earlier conversations with Alex", "Keep answers very short.", "main project is homelab"} {
		if !strings.Contains(string(req), want) {
			t.Fatalf("the instructions lack %q", want)
		}
	}
	if strings.Index(string(req), "Keep answers very short.") > strings.Index(string(req), "main project is homelab") {
		t.Fatalf("style must come before facts")
	}
	r.say("forget my main project")
	b, _ = os.ReadFile(r.cfg.MemoryFile)
	if strings.Contains(string(b), "homelab") {
		t.Fatalf("not forgotten:\n%s", b)
	}
	// The brain's own remember call works the same, and its words reach the next session.
	r.f.Lock()
	r.f.ToolCalls = [][]voicetest.ToolCall{{{Name: "remember", Args: `{"kind": "correction", "text": "SEMA is semantic grep, not a person."}`}}}
	r.f.Unlock()
	s = r.say("no, SEMA is semantic grep, not a person")
	if b, _ = os.ReadFile(r.cfg.MemoryFile); !strings.Contains(string(b), "## Corrections\n\n- SEMA is semantic grep, not a person.") {
		t.Fatalf("correction:\n%s", b)
	}
	logs := r.logs.String()
	for _, word := range []string{"homelab", "hunter2", "SEMA is"} {
		if strings.Contains(logs, word) {
			t.Fatalf("%q in the log", word)
		}
	}
}

// The briefing leads with what waits, then what's almost done (Shepherdr's own progress), then runs.
func TestBriefingSaysProgress(t *testing.T) {
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"places": [{"id": "p1", "name": "semantic-grep-nvim", "status": "work", "chat": "c1"}],
			"tasks": [{"id": "SEMA-3", "name": "notes", "place": "p1", "status": "work", "report": {"progress": 90, "eta": 5, "msg": "Benchmarking."}}], "needs": []}`))
	}))
	defer sh.Close()
	r := newRig(t, func(c *config.Config) { c.Shepherdrs = []config.Shepherdr{{Name: "this Mac", URL: sh.URL}} })
	_, say := r.d.briefing(context.Background(), mustWorld(r))
	if !strings.HasPrefix(say, "Semantic grep nvim SEMA 4 is waiting on you. Semantic grep nvim SEMA 3's almost done, benchmarking. Work's working.") {
		t.Fatalf("briefing: %q", say)
	}
}

// fakeEmbedServer embeds by keywords (a dimension per word of a small vocabulary).
func fakeEmbedServer(t *testing.T) string {
	vocab := []string{"milk", "demo", "router", "canberra", "australia", "lovelace"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var data []map[string]any
		for i, in := range req.Input {
			v := make([]float32, len(vocab)+1)
			v[len(vocab)] = 0.05
			for j, w := range vocab {
				if strings.Contains(strings.ToLower(in), w) {
					v[j] = 1
				}
			}
			data = append(data, map[string]any{"index": i, "embedding": v})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// A knowledge question is looked up (DeepSeek, a fake here), said in its source's words, cached, and
// counted; the log has no words of it.
func TestWebSearch(t *testing.T) {
	var searches atomic.Int32
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searches.Add(1)
		w.Write([]byte(`{"results":[{"title":"Ada Lovelace","url":"https://en.wikipedia.org/wiki/Ada_Lovelace","snippet":"s","summary":"Ada Lovelace was an English mathematician. She wrote the first published algorithm. She worked with Charles Babbage. She died in 1852.","rerank_score":2.9,"site_name":"Wikipedia","date":null,"authority":4}]}`))
	}))
	defer web.Close()
	key := filepath.Join(t.TempDir(), "ds.key")
	os.WriteFile(key, []byte("k"), 0o600)
	embed := fakeEmbedServer(t)
	r := newRig(t, func(c *config.Config) {
		c.Search = config.SearchConfig{Enabled: true, DeepSeekKeyFile: key}
		c.Embed = config.Endpoint{URL: embed, Model: "e"}
	})
	r.d.search.DeepSeekURL = web.URL
	s := r.say("Who is Ada Lovelace?")
	if !strings.HasSuffix(s.Answer, "Ada Lovelace was an English mathematician. She wrote the first published algorithm. She worked with Charles Babbage.") ||
		!strings.Contains(s.Answer, "Wikipedia") {
		t.Fatalf("answer: %q", s.Answer)
	}
	if s.Search.Backend != "deepseek" || s.Search.CallsToday != 1 {
		t.Fatalf("search state: %+v", s.Search)
	}
	r.waitFor("the cache", func() bool { _, ok := r.d.index.Cached("who is ada lovelace", time.Hour); return ok })
	r.say("who is ada lovelace")
	if searches.Load() != 1 {
		t.Fatalf("asked the web %d times", searches.Load())
	}
	// "be shorter": one sentence
	r.say("be shorter")
	r.d.index = nil // no cache: a fresh search
	s = r.say("who is ada lovelace")
	if strings.Count(s.Answer, ".") != 1 {
		t.Fatalf("short answer: %q", s.Answer)
	}
	if l := r.logs.String(); strings.Contains(strings.ToLower(l), "lovelace") || !strings.Contains(l, `"event":"search"`) {
		t.Fatalf("log:\n%s", l)
	}
	b, _ := json.Marshal(r.d.Snapshot())
	if !strings.Contains(string(b), `"search":{"backend":"deepseek","calls_today":2}`) {
		t.Fatalf("status: %s", b)
	}
}

func TestWebDown(t *testing.T) {
	r := newRig(t, func(c *config.Config) { c.Search = config.SearchConfig{Enabled: true} })
	r.d.search.DDGURL, r.d.search.WikiURL, r.d.search.DeepSeekKey = "http://127.0.0.1:1", "http://127.0.0.1:1", ""
	if s := r.say("look up the rust language"); !strings.Contains(s.Answer, "web") {
		t.Fatalf("answer: %q", s.Answer)
	}
}

// Notes are indexed and recalled ("find my note about …"), and a brain turn gets the relevant ones.
func TestRecall(t *testing.T) {
	embed := fakeEmbedServer(t)
	r := newRig(t, func(c *config.Config) { c.Embed = config.Endpoint{URL: embed, Model: "e"} })
	r.say("note: the demo is on Friday")
	r.say("note: buy milk")
	r.waitFor("the index", func() bool { r.d.syncIndex(); return r.d.index.Len() >= 2 })
	s := r.say("find my note about the demo")
	if s.Answer != "Your note from today says: the demo is on Friday." && s.Answer != "Today, you wrote: the demo is on Friday." {
		t.Fatalf("recall: %q", s.Answer)
	}
	if s := r.say("find my note about kubernetes"); !strings.Contains(s.Answer, "kubernetes") {
		t.Fatalf("nothing: %q", s.Answer)
	}
	r.say("is the demo still on?")
	req, _ := json.Marshal(r.chats()[0]["messages"])
	got := string(req)
	if i := strings.Index(got, "(From the owner's notes"); i >= 0 {
		got = got[i : i+strings.Index(got[i:], `\n`)]
	}
	if !strings.HasPrefix(got, "(From the owner's notes") || !strings.Contains(got, "the demo is on Friday") || strings.Contains(got, "buy milk") {
		t.Fatalf("auto-recall: %s", req)
	}
}

// The pomodoro: started, ended (a line, a toast, the desk notes), stats; the widgets' JSON.
func TestPomodoro(t *testing.T) {
	r := newRig(t, nil)
	s := r.say("start a pomodoro for 30 minutes on semantic grep")
	if s.Pomo.State != "focus" || s.Pomo.PlannedS != 1800 || *s.Pomo.Task != "semantic grep" || s.Pomo.EndsAt == nil {
		t.Fatalf("pomo: %+v", s.Pomo)
	}
	if s := r.say("how much time is left"); !strings.Contains(s.Answer, "30 minutes") {
		t.Fatalf("left: %q", s.Answer)
	}
	out := r.d.Dispatch(Request{Cmd: "pomo", Arg: `{"action":"pause"}`}).(map[string]any)
	if out["ok"] != true || out["pomo"].(pomo.Info).State != "paused" {
		t.Fatalf("widget pause: %v", out)
	}
	r.d.Dispatch(Request{Cmd: "pomo", Arg: `{"action":"resume"}`})
	// it ends: as if its time had come
	r.d.pomo.Start(time.Now().Add(-26*time.Minute), 25, "semantic grep")
	ev, ok := r.d.pomo.Tick(time.Now())
	if !ok {
		t.Fatal("no end")
	}
	r.d.pomoEnded(ev)
	if sp := r.spoken(); !hasPart(sp, "25 minutes") && !hasPart(sp, "Take 5") {
		t.Fatalf("spoken: %q", sp)
	}
	if s := r.d.Snapshot(); s.Toast == nil || !strings.HasPrefix(s.Toast.Body, "Focus done: 25 minutes on semantic grep.") || s.Pomo.State != "idle" || s.Pomo.Cycle != 1 {
		t.Fatalf("after: %+v %+v", s.Toast, s.Pomo)
	}
	b, _ := os.ReadFile(filepath.Join(r.cfg.NotesDir, "daily", time.Now().Format("2006-01-02")+".md"))
	if !strings.Contains(string(b), "🍅 25m focus: semantic grep") {
		t.Fatalf("notes: %s", b)
	}
	if s := r.say("how many pomodoros today"); !strings.HasPrefix(s.Answer, "1 pomodoro today, 1 this week") {
		t.Fatalf("stats: %q", s.Answer)
	}
	lines := pomo.ReadLog(r.d.pomo.LogPath)
	if len(lines) != 2 || lines[0].Completed || !lines[1].Completed { // the 30-minute one was stopped by the new start
		t.Fatalf("log: %+v", lines)
	}
	b, _ = json.Marshal(r.d.Snapshot().Pomo)
	if string(b) != `{"state":"idle","kind":"","task":null,"ends_at":null,"started_at":null,"planned_s":0,"left_s":0,"cycle":1}` {
		t.Fatalf("pomo json: %s", b)
	}
}

// A command says what it did, with at most one suggestion grounded in what waits, not repeated, and
// none once the owner asked for short answers.
func TestCommandsSoundLikeAHelper(t *testing.T) {
	r := newRig(t, nil)
	r.d.look(mustWorld(r), nil, time.Now()) // the inbox: SEMA 4 waits
	s := r.say("theme pixel")
	if !strings.Contains(strings.ToLower(s.Answer), "pixel") || !strings.Contains(s.Answer, "Semantic grep nvim SEMA 4") && !strings.Contains(s.Answer, "semantic grep nvim SEMA 4") {
		t.Fatalf("first: %q", s.Answer)
	}
	if s := r.say("theme fortress"); strings.Contains(s.Answer, "SEMA 4") {
		t.Fatalf("the same suggestion again: %q", s.Answer)
	}
	if s := r.say("hello"); strings.Count(s.Answer, ".")+strings.Count(s.Answer, "?")+strings.Count(s.Answer, "!") < 2 {
		t.Fatalf("hello: %q", s.Answer)
	}
	r.d.sec.suggested = nil
	r.say("be shorter")
	if s := r.say("theme pixel"); strings.Contains(s.Answer, "SEMA") {
		t.Fatalf("suggested after be shorter: %q", s.Answer)
	}
}

// Shepherdr's open questions (asks) wait on the owner like needs: titled from their chat, announced,
// and a task that finishes is announced too.
func TestShepherdrAsksAndFinishedTasks(t *testing.T) {
	var mu sync.Mutex
	status := "work"
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/snapshot":
			fmt.Fprintf(w, `{"places": [{"id": "p1", "name": "homelab", "status": "need", "chat": "p:m:homelab"}],
				"tasks": [{"id": "HOME-16", "name": "scan", "place": "p1", "status": %q, "agent": {"chat": "HOME-16/Agent"}}],
				"needs": [], "asks": {"p:m:homelab": "q7"}}`, status)
		case "/api/chat":
			w.Write([]byte(`{"chat": {"messages": [{"id": "m1", "who": "agent", "at": 1, "question": {"id": "q7", "kind": "choice", "title": "Reboot the router tonight?"}}]}}`))
		}
	}))
	defer sh.Close()
	r := newRig(t, func(c *config.Config) { c.Shepherdrs = []config.Shepherdr{{Name: "gpubox", URL: sh.URL}} })
	if s := r.say("what needs me"); !strings.Contains(s.Answer, "Homelab coordinator needs you: it asks reboot the router tonight.") {
		t.Fatalf("needs: %q", s.Answer)
	}
	r.say("talk to me")
	r.d.mu.Lock()
	r.d.sec.askTitles = map[string]string{}
	r.d.sec.snapAt = map[string]time.Time{}
	r.d.mu.Unlock()
	mu.Lock()
	status = "done"
	mu.Unlock()
	now := time.Now()
	needs := r.d.readNeeds(context.Background())
	r.d.look(mustWorld(r), needs, now.Add(10*time.Second))
	r.d.look(mustWorld(r), needs, now.Add(30*time.Second))
	r.waitFor("the finished task", func() bool { return hasPart(r.spoken(), "Homelab HOME 16 finished.") })
}

// Delegation: the reference is resolved from the notes (yesterday's, not today's), a brief is drafted
// and asked about; no sends nothing, an edit changes the request, yes sends it through the Shepherdr
// chat; a reference to nothing is asked back.
func TestDelegateWithContext(t *testing.T) {
	var mu sync.Mutex
	var sent []map[string]any
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/snapshot":
			w.Write([]byte(`{"places": [{"id": "p1", "name": "homelab", "status": "idle", "chat": "p:m:homelab"}], "tasks": [], "needs": [], "asks": {}}`))
		case "/api/action":
			var m map[string]any
			json.NewDecoder(r.Body).Decode(&m)
			mu.Lock()
			sent = append(sent, m)
			mu.Unlock()
			w.Write([]byte(`{"ok": true}`))
		}
	}))
	defer sh.Close()
	embed := fakeEmbedServer(t)
	r := newRig(t, func(c *config.Config) {
		c.Shepherdrs = []config.Shepherdr{{Name: "gpubox", URL: sh.URL, HerdrMachine: "gpubox"}}
		c.Embed = config.Endpoint{URL: embed, Model: "e"}
	})
	yesterday := time.Now().AddDate(0, 0, -1)
	r.d.desk.Note(yesterday.Add(-time.Hour), "the router keeps dropping every evening")
	r.d.desk.Note(time.Now(), "router firmware is updated")
	r.d.desk.Note(time.Now(), "buy milk")
	s := r.say("Ask the homelab coordinator to investigate the router issue we discussed yesterday.")
	if s.Card == nil || !strings.Contains(s.Card.Ask, "homelab coordinator") || !strings.Contains(s.Card.Ask, "yesterday") {
		t.Fatalf("card: %+v", s.Card)
	}
	if !strings.HasPrefix(s.Card.Text, "Investigate the router issue we discussed yesterday.\n\nContext from Alex's notes:\n- "+yesterday.Format("2006-01-02")) ||
		!strings.Contains(s.Card.Text, "the router keeps dropping every evening") || strings.Contains(s.Card.Text, "firmware") ||
		strings.Contains(s.Card.Text, "milk") || !strings.HasSuffix(s.Card.Text, "\n\nSent by Alex's voice assistant.") {
		t.Fatalf("draft:\n%s", s.Card.Text)
	}
	r.say("no")
	mu.Lock()
	if len(sent) != 0 {
		t.Fatalf("sent after a no: %v", sent)
	}
	mu.Unlock()
	r.say("Ask the homelab coordinator to investigate the router issue we discussed yesterday.")
	s = r.say("Change it to look into the router drops tonight.")
	if s.Card == nil || !strings.HasPrefix(s.Card.Text, "Look into the router drops tonight.\n\nContext from") {
		t.Fatalf("edit: %+v", s.Card)
	}
	r.say("yes")
	r.waitFor("the send", func() bool { mu.Lock(); defer mu.Unlock(); return len(sent) == 1 })
	mu.Lock()
	m := sent[0]
	mu.Unlock()
	if m["type"] != "send" || m["chat"] != "p:m:homelab" || !strings.HasPrefix(fmt.Sprint(m["text"]), "Look into the router drops tonight.") ||
		!strings.HasPrefix(fmt.Sprint(m["cid"]), "hvc") {
		t.Fatalf("sent: %v", m)
	}
	b, _ := os.ReadFile(r.d.desk.Path(time.Now()))
	if !strings.Contains(string(b), "→ homelab coordinator: investigate the router issue we discussed yesterday (from note "+yesterday.Format("2006-01-02")) {
		t.Fatalf("daily note:\n%s", b)
	}
	for _, c := range r.h.commands() {
		if strings.HasPrefix(c, "hc send") {
			t.Fatalf("typed into a terminal: %s", c)
		}
	}
	s = r.say("Ask the homelab coordinator to fix that thing from last week.")
	if s.Card != nil || !strings.Contains(s.Answer, "last week") {
		t.Fatalf("ask back: %q %+v", s.Answer, s.Card)
	}
}

// The conversation is kept on this Mac: a new vcd reads it back (the turns, the summary in the
// instructions); the replay keeps the newest turns within the budget; forgetting clears it.
func TestConversationPersists(t *testing.T) {
	r := newRig(t, func(c *config.Config) { c.ContextTokens = 60 })
	r.f.Lock()
	r.f.Answer = []string{"Sounds like the router again."}
	r.f.Unlock()
	for i := 0; i < 12; i++ {
		r.say(fmt.Sprintf("the router dropped again, number %d", i))
	}
	r.waitFor("the summary", func() bool { return strings.Contains(r.d.convo.Summary(), "number 0") })
	// a restart: same folders, a new daemon
	logs := &syncBuf{}
	d2 := New(r.cfg, log.New(logs, "", 0))
	if len(d2.turns) != 12 || d2.turns[11].You != "the router dropped again, number 11" {
		t.Fatalf("reloaded %d turns", len(d2.turns))
	}
	d2.mu.Lock()
	recent := d2.recentTurnsLocked()
	d2.mu.Unlock()
	if len(recent) == 0 || len(recent) >= 12 || recent[len(recent)-1].You != "the router dropped again, number 11" {
		t.Fatalf("replay within the budget: %d turns", len(recent))
	}
	if sc := d2.computerSession(); !strings.Contains(sc.Instructions, "Earlier conversations with Alex") ||
		!strings.Contains(sc.Instructions, `you said "the router dropped again, number 0"`) {
		t.Fatalf("summary not in the instructions:\n%s", sc.Instructions)
	}
	r.say("forget what I said about number 3")
	for _, x := range r.d.convo.All() {
		if strings.Contains(x.You, "again, number 3") {
			t.Fatal("not forgotten")
		}
	}
	s := r.say("start fresh")
	if s.Card == nil {
		t.Fatalf("no confirm: %q", s.Answer)
	}
	r.say("yes")
	r.waitFor("cleared", func() bool {
		for _, x := range r.d.convo.All() {
			if strings.Contains(x.You, "router") {
				return false
			}
		}
		return r.d.convo.Summary() == ""
	})
	if l := logs.String() + r.logs.String(); strings.Contains(l, "router") {
		t.Fatal("words in the log")
	}
}

func TestHumanWords(t *testing.T) {
	for in, want := range map[string]string{"As an AI, I can't feel tired.": "", "Here is the information you requested.": "",
		"Certainly! The homelab agent is idle.": "The homelab agent is idle.", "Task completed successfully.": "", "Sure, done.": "Sure, done."} {
		if got := Humanize(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
	if progressWords(92) != "almost done" || etaWords(240) != "about four hours to go" || etaWords(14) != "about 15 minutes to go" {
		t.Fatal("rounding")
	}
}

// A slow lookup gets a filler first, then the answer, one after the other.
func TestFillerBeforeASlowTool(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		w.Write([]byte(`{"results":[{"title":"Ada","url":"https://x/ada","summary":"Ada Lovelace was a mathematician.","rerank_score":2,"site_name":"Wikipedia"}]}`))
	}))
	defer web.Close()
	key := filepath.Join(t.TempDir(), "k")
	os.WriteFile(key, []byte("k"), 0o600)
	r := newRig(t, func(c *config.Config) { c.Search = config.SearchConfig{Enabled: true, DeepSeekKeyFile: key} })
	r.d.search.DeepSeekURL = web.URL
	r.say("who is ada lovelace")
	r.waitFor("both", func() bool { return len(r.spoken()) == 2 })
	sp := r.spoken()
	if !has(phrases["filler.web"], sp[0]) || !strings.Contains(sp[1], "Ada Lovelace was a mathematician.") {
		t.Fatalf("spoken: %q", sp)
	}
	if !strings.Contains(r.logs.String(), `"event":"filler"`) {
		t.Fatal("no filler logged")
	}
}
