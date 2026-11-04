// Package daemon is vcd: push-to-talk turns, the computer's session with its brain, speech out, the
// secretary (briefings, announcements, notes, reminders) and the state the Herdr mod shows.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"herdr-voice-computer/internal/audio"
	"herdr-voice-computer/internal/config"
	"herdr-voice-computer/internal/convo"
	"herdr-voice-computer/internal/desk"
	"herdr-voice-computer/internal/herdr"
	"herdr-voice-computer/internal/index"
	"herdr-voice-computer/internal/intent"
	"herdr-voice-computer/internal/memory"
	"herdr-voice-computer/internal/pomo"
	"herdr-voice-computer/internal/search"
	"herdr-voice-computer/internal/shepherdr"
	"herdr-voice-computer/internal/voice"
)

var thinkTagRE = regexp.MustCompile(`</?think>`)

type Daemon struct {
	cfg   config.Config
	voice *voice.Client
	ext   ext // the backend's own checks, if it has any
	hub   hub
	log   *log.Logger

	mu    sync.Mutex
	st    State
	turns []Turn

	// The key press being held: its recorder, when it went down and the turn's number.
	rec       *audio.Recorder
	dict      *audio.Recorder // a dictation take (dictate.go)
	pressAt   time.Time
	listening time.Duration // from `vc ptt start` to the Listening state going out
	lastStop  time.Time     // the latest `vc ptt stop`
	gen       int           // bumped by every press: work for an older turn stops

	sess      *voice.Session
	idle      *time.Timer
	turn      *turnRun // the turn the session is answering, if any
	player    *audio.Player
	sayer     *audio.Player      // vc say
	sayCancel context.CancelFunc // stops a vc say still waiting for its speech
	sayGen    int
	dialMu    sync.Mutex // one dial at a time: a second turn waits and takes the first's session
	hideGen   int
	sessGame  bool // the session's brain is a slower stand-in
	gameNoted map[*voice.Session]bool
	health    healthInfo
	stopping  bool

	herdr        *herdr.Client
	sheps        []*shepherdr.Client // [0] is this Mac's own, when configured
	sec          secretary
	lastLog      *turnLog // the last turn's timings, for `vc turn`
	toastSeq     int
	desk         *desk.Desk
	pomo         *pomo.Timer
	search       *search.Client
	index        *index.Index
	indexMu      sync.Mutex
	searchCalls  string
	voices       voices
	mem          *memory.Memory // what the owner told it to keep (markdown on disk)
	memStale     bool           // the memory changed: the next turn starts a session that knows it
	convo        *convo.Store
	sinceSummary int
	lastLogAt    time.Time
	fillerOn     bool      // a filler is being said: the answer waits for it
	micBlocked   string    // why the mic gives only silence ("" while it works)
	lastSpoke    time.Time // when the computer last stopped speaking (announcements wait for quiet)
	sayFrom      time.Time // the release a spoken outcome answers (its first audio is timed from it)
	prefs        string    // the preferences file (quiet)
	running      atomic.Bool
	sendMu       sync.Mutex    // held across closing a batch and sending its outputs, and a turn's items: never under d.mu
	batch        *batch        // the tool calls being answered (planned, run, or waiting on a card)
	lead         *audio.Player // what the brain said before its tool calls, still playing out
	cardSeq      int
	confirmHeld  bool    // SUPER + ALT + F7 is down: the next release confirms
	asked        []asked // the last candidates asked back, by label
	undoVolume   int     // the level before the last volume change (Undo)
	undoOK       bool
	untrusted    string // where untrusted text in the session came from: every intent needs a confirm
	// Agent replies (replies.go).
	replies     replySource
	replyQ      []agentReply
	lastReply   *agentReply
	replyWake   chan struct{}
	replyP      *audio.Player // the reply playing, as d.sayer
	replyCur    *agentReply   // the reply being fetched or played
	pausedReply *agentReply   // stopped by a press still held: played again after the turn
	lastActive  time.Time     // the owner's last press or vc say, or voxtype busy
}

// healthInfo is the last health check.
type healthInfo struct {
	off, why string
	game     bool // the backend answers with a slower stand-in
	paused   bool // the backend asked the voice computer to wait: no computer session
	// The STT endpoint's own state, for dictation: "" while it works, else an Off code and why.
	dict, dictWhy string
}

// turnRun is one turn sent to the brain, with its timings for the latency log.
type turnRun struct {
	gen       int
	you       string
	answer    strings.Builder
	awaiting  bool // response.create sent, response.created not yet seen
	respID    string
	log       turnLog
	released  time.Time
	firstText bool
	last      time.Time     // the last event for this turn, for the watchdog
	calls     []intent.Call // its tool calls, answered once the response is done
	prior     string        // what the brain said before, when this is the response to tool outputs
	round     int           // 0: the turn itself; 1: the response to its outputs
	readOut   string        // read with vc say once this has played (window titles, file names)
}

// turnLog is one line of the latency log: times only, never words.
type turnLog struct {
	Event       string             `json:"event"` // "turn"
	Kind        string             `json:"kind"`  // brain, empty, stop, tap, error
	ListeningMS int64              `json:"listening_ms"`
	MicMS       int64              `json:"mic_ms"`
	HeldMS      int64              `json:"held_ms"`
	ASRMS       int64              `json:"asr_ms"`
	FastMatch   bool               `json:"fast_match,omitempty"` // the fast grammar took it (no brain)
	FirstCallMS int64              `json:"first_call_ms,omitempty"`
	SayFirstMS  int64              `json:"say_first_audio_ms,omitempty"` // vcd's own spoken outcome: its first audio, from the release
	Session     string             `json:"session,omitempty"`            // new or warm
	DialMS      int64              `json:"dial_ms,omitempty"`
	FirstTextMS int64              `json:"first_text_ms,omitempty"`
	FirstAudio  int64              `json:"first_audio_ms,omitempty"`
	DoneMS      int64              `json:"done_ms,omitempty"`
	Status      string             `json:"status,omitempty"`
	Brain       string             `json:"brain,omitempty"`
	BrainLoaded *bool              `json:"brain_loaded,omitempty"`
	Game        bool               `json:"game"`
	Capped      bool               `json:"capped,omitempty"`
	Muted       bool               `json:"muted,omitempty"`
	Intents     []string           `json:"intents,omitempty"` // the tool calls' names
	FastMS      int64              `json:"fast_ms,omitempty"` // a fast intent done, from the release
	Service     map[string]float64 `json:"x_timings,omitempty"`
}

func New(cfg config.Config, logger *log.Logger) *Daemon {
	d := &Daemon{cfg: cfg, log: logger, voice: voice.New(cfg)}
	d.herdr = herdr.New(cfg.HerdrApp, cfg.HC)
	d.mem = memory.New(cfg.MemoryFile, cfg.Owner)
	d.extras()
	d.loadConversation()
	for _, s := range cfg.Shepherdrs {
		d.sheps = append(d.sheps, &shepherdr.Client{Name: s.Name, URL: s.URL, Machine: s.Machine, HC: cfg.HC})
	}
	d.gameNoted = map[*voice.Session]bool{}
	d.replyWake = make(chan struct{}, 1)
	if len(cfg.ReplySpaces) > 0 && len(d.sheps) > 0 {
		d.replies = d.sheps[0]
	}
	d.sec.init()
	d.st = State{Type: "state", State: Off, Off: OffService, Why: "Checking the backend", Turns: []Turn{},
		TalkKey: cfg.TalkKey, ConfirmKey: cfg.ConfirmKey, Name: cfg.Name, Inbox: []InboxItem{}, Reminders: []ReminderInfo{}}
	d.health = healthInfo{off: OffService, why: d.st.Why}
	return d
}

// Run checks the backend now and every few seconds, until ctx ends.
func (d *Daemon) Run(ctx context.Context) {
	go d.watchReplies(ctx)
	go d.playReplies(ctx)
	go d.watchAgents(ctx)
	go d.watchPomo(ctx)
	go func() { // the index follows the notes and memory.md (only changed files are embedded)
		for ctx.Err() == nil {
			d.syncIndex()
			select {
			case <-ctx.Done():
			case <-time.After(30 * time.Second):
			}
		}
	}()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for i := 0; ; i++ {
		d.checkHealth(ctx, i%6 == 0) // the full check every 30 s, a quick one every 5 s
		select {
		case <-ctx.Done():
			d.shutdown()
			return
		case <-t.C:
		}
	}
}

func (d *Daemon) shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopping = true
	if d.rec != nil {
		d.rec.Stop()
		d.rec = nil
	}
	d.stopOutputLocked()
	if s := d.sess; s != nil {
		d.sess = nil
		go s.Close()
	}
}

// ---- state

// publishLocked sends the state to every watcher; d.mu is held.
func (d *Daemon) publishLocked() {
	d.extraStateLocked()
	d.st.Turns = append([]Turn{}, d.turns...)
	d.st.Version++
	d.hub.version.Store(d.st.Version)
	b, _ := json.Marshal(d.st)
	d.hub.send(b, false)
}

func (d *Daemon) level(l float64) { d.hub.send(levelLine(l), true) }

// Snapshot is the state now.
func (d *Daemon) Snapshot() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.extraStateLocked()
	s := d.st
	s.Turns = append([]Turn{}, d.turns...)
	return s
}

// Watch returns a channel of state and level lines (the state first) and a func to stop watching.
func (d *Daemon) Watch() (<-chan []byte, func()) {
	ch := d.hub.subscribe()
	d.mu.Lock()
	st := d.st // the state now, to this watcher only (no new version: other watchers have it)
	st.Turns = append([]Turn{}, d.turns...)
	b, _ := json.Marshal(st)
	d.mu.Unlock()
	select {
	case ch <- b:
	default:
	}
	return ch, func() { d.hub.unsubscribe(ch) }
}

// settleLocked puts the bar back to ready (or off) after a turn, and hides the overlay later.
func (d *Daemon) settleLocked() {
	if d.health.off != "" {
		d.st.State, d.st.Off, d.st.Why = Off, d.health.off, d.health.why
	} else if d.micBlocked != "" { // the talk key still works: the next recording with sound clears it
		d.st.State, d.st.Off, d.st.Why = Off, OffMic, d.micBlocked
	} else {
		d.st.State, d.st.Off = Ready, ""
		d.readyFlagsLocked()
	}
	d.hideLaterLocked()
	d.publishLocked()
}

func (d *Daemon) hideLaterLocked() {
	d.hideGen++
	g := d.hideGen
	time.AfterFunc(d.cfg.OverlayHide(), func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.hideGen == g && d.rec == nil && d.turn == nil && d.st.Card == nil && d.st.Overlay {
			d.st.Overlay = false
			d.publishLocked()
		}
	})
}

// Hide hides the overlay now (vc hide, or a click on it).
func (d *Daemon) Hide() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.st.Card != nil {
		return // the overlay holds an open card: it goes when the card is answered
	}
	d.hideGen++
	d.st.Overlay = false
	d.publishLocked()
}

func (d *Daemon) SetMute(arg string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch arg {
	case "on":
		d.st.Muted = true
	case "off":
		d.st.Muted = false
	case "toggle", "":
		d.st.Muted = !d.st.Muted
	default:
		return d.st.Muted, fmt.Errorf("mute takes on, off or toggle")
	}
	if d.st.Muted {
		d.pausedReply = nil
		d.stopReplyLocked()
		for _, r := range d.replyQ {
			d.logReply(r, "dropped", "muted")
		}
		d.replyQ = nil
		if d.player != nil {
			d.player.Stop()
			d.player = nil
		}
		if d.sayCancel != nil {
			d.sayCancel()
			d.sayCancel = nil
		}
		if d.sayer != nil {
			d.sayer.Stop()
			d.sayer = nil
		}
	}
	d.publishLocked()
	return d.st.Muted, nil
}

// readyFlagsLocked sets the slow and paused marks and the why line for a working computer.
func (d *Daemon) readyFlagsLocked() {
	d.st.Game = d.health.game || d.sessGame
	d.st.Voice = d.health.paused
	switch {
	case d.st.Voice:
		d.st.Why = pausedWhy
	case d.st.Game:
		d.st.Why = slowWhy
	default:
		d.st.Why = ""
	}
}

// noteGameLocked reads the session's brain: a slower stand-in may answer.
func (d *Daemon) noteGameLocked(s *voice.Session) {
	d.sessGame = d.slow(s.Model())
	if d.st.State != Off {
		d.readyFlagsLocked()
	}
}

// ---- push to talk

// PTTStart is the key going down. at is when `vc ptt start` ran.
func (d *Daemon) PTTStart(at time.Time) {
	// Paused: check again now, so a press right after the pause ends works without waiting for the 30 s check.
	d.mu.Lock()
	paused := d.health.paused
	d.mu.Unlock()
	if paused && d.ext != nil {
		if d.ext.resumed(context.Background()) {
			d.mu.Lock()
			d.health.paused = false
			if d.st.State != Off {
				d.readyFlagsLocked()
			}
			d.publishLocked()
			d.mu.Unlock()
		}
	}
	d.pttStart(at)
}

func (d *Daemon) pttStart(at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.rec != nil || d.stopping {
		return // a repeated press while held
	}
	d.confirmHeld = false // a plain F7 press: a confirm-key press whose release raced ahead is over
	d.lastActive = time.Now()
	d.gen++
	// A press while it speaks or thinks stops that first (a tap is just that; a hold goes on to a turn).
	if d.turn != nil || d.player != nil || d.lead != nil || d.sayer != nil || d.sayCancel != nil {
		d.pauseReplyLocked()
		d.stopOutputLocked()
		b, _ := json.Marshal(map[string]any{"event": "stop", "stop_ms": time.Since(at).Milliseconds()})
		d.log.Println(string(b))
	}
	if !at.After(d.lastStop) && d.lastStop.Sub(at) < 2*time.Second {
		// and cancel the open card, as a tap does.
		d.cancelCardsLocked()
		// Its release already came (the two binds' vc processes raced): it was a tap. Stop, open no mic.
		d.endPauseLocked(true)
		d.st.Overlay = false
		d.settleLocked()
		return
	}
	// A turn still being transcribed or dialled is dropped by the gen bump: leave its Thinking behind.
	if d.st.State == Thinking || d.st.State == Speaking {
		d.st.State = Ready
	}
	d.hideGen++
	d.pressAt = at
	if d.health.off != "" || d.health.paused {
		d.endPauseLocked(true) // no turn will follow
	}
	if d.health.off != "" {
		d.st.Overlay, d.st.Heard, d.st.From, d.st.Answer, d.st.Note = true, "", "", "", d.health.why
		d.settleLocked()
		return
	}
	if d.health.paused { // paused: no mic, nothing sent
		d.st.Overlay = false
		d.settleLocked()
		go d.notify(pausedNote)
		return
	}
	rec, err := audio.Record(d.cfg.MaxTurn(), d.level)
	if err != nil {
		d.st.Overlay, d.st.Note = true, "The mic didn't open: "+err.Error()
		d.endPauseLocked(true)
		d.settleLocked()
		return
	}
	d.rec = rec
	d.st.State, d.st.Overlay, d.st.Heard, d.st.From, d.st.Answer, d.st.Note = Listening, true, "", "", "", ""
	d.st.Done = nil
	d.publishLocked()
	d.listening = time.Since(at)
	gen := d.gen
	go func() {
		select {
		case <-rec.Full(): // 60 s: the mic closes and those 60 s are the turn
			d.release(gen, time.Now(), true)
		case <-rec.Done(): // released (or cancelled) before the cap, unless it filled just now
			select {
			case <-rec.Full():
				d.release(gen, time.Now(), true)
			default:
			}
		}
	}()
}

// PTTStop is the key coming up.
func (d *Daemon) PTTStop(at time.Time) {
	d.mu.Lock()
	if at.After(d.lastStop) {
		d.lastStop = at
	}
	held := d.confirmHeld && d.rec == nil
	d.mu.Unlock()
	if held { // SUPER or ALT went up first: this plain F7 release is the confirm key's
		d.ConfirmKey("up")
		return
	}
	d.release(-1, at, false)
}

// release ends the held press: a tap stops, a hold sends the turn. gen -1 means whichever is held.
func (d *Daemon) release(gen int, at time.Time, capped bool) {
	d.mu.Lock()
	if d.rec == nil || (gen >= 0 && gen != d.gen) {
		d.mu.Unlock()
		return
	}
	rec := d.rec
	d.rec = nil
	gen = d.gen
	held := at.Sub(d.pressAt)
	pressAt, listening := d.pressAt, d.listening
	d.mu.Unlock()

	pcm := rec.Stop()
	d.level(0)
	var micOpen time.Duration
	if t := rec.OpenedAt(); !t.IsZero() {
		micOpen = t.Sub(pressAt)
	}
	tl := turnLog{Event: "turn", ListeningMS: listening.Milliseconds(), MicMS: micOpen.Milliseconds(),
		HeldMS: held.Milliseconds(), Capped: capped}

	d.mu.Lock()
	defer d.mu.Unlock()
	if gen != d.gen {
		return
	}
	tl.Game = d.health.game // the session's brain may add it once the answer comes
	if held < d.cfg.Tap() {
		// A tap: stop. The press already stopped any speech; the audio is thrown away.
		d.endPauseLocked(true)
		tl.Kind = "tap"
		d.logTurn(tl)
		d.cancelCardsLocked() // a tap cancels the open card too
		d.st.Overlay = false
		d.settleLocked()
		return
	}
	if len(pcm) < audio.InRate*2/5 { // under 0.2 s: the mic gave nothing (the service refuses it too)
		tl.Kind = "error"
		d.logTurn(tl)
		d.st.Note = "No sound from the mic"
		d.endPauseLocked(true) // no brain turn: a reply it stopped stays stopped
		d.settleLocked()
		return
	}
	if audio.Blocked(pcm) { // macOS gave digital silence: the mic is blocked for the app that started vcd
		tl.Kind = "mic_blocked"
		d.logTurn(tl)
		d.micBlocked = MicBlockedWhy()
		d.st.Note = d.micBlocked
		d.st.Overlay = true
		d.endPauseLocked(true)
		d.settleLocked()
		return
	}
	d.micBlocked = ""      // sound came: the mic works
	if audio.Silent(pcm) { // nothing was said (or the mic is muted): nothing is sent
		tl.Kind = "empty"
		d.logTurn(tl)
		d.st.Note = "Didn't hear anything: is the mic on?"
		d.endPauseLocked(true) // no brain turn: a reply it stopped stays stopped
		d.settleLocked()
		return
	}
	d.st.State = Thinking
	d.publishLocked()
	go d.sendTurn(gen, pcm, at, tl)
}

// sendTurn transcribes the turn outside the session, then sends the words to the brain.
func (d *Daemon) sendTurn(gen int, pcm []byte, released time.Time, tl turnLog) {
	d.sendTurnWith(gen, pcm, "", released, tl)
}

// sendTurnText sends typed words as a turn, as if heard.
func (d *Daemon) sendTurnText(gen int, text string, released time.Time, tl turnLog) {
	d.sendTurnWith(gen, nil, text, released, tl)
}

func (d *Daemon) sendTurnWith(gen int, pcm []byte, typed string, released time.Time, tl turnLog) {
	ctx, cancel := context.WithTimeout(context.Background(), voice.Timeout)
	defer cancel()

	// First, the backend's own check, if it has one: whether to pause, and whether the brain is loaded.
	var merr error
	var pause bool
	if d.ext != nil {
		tl.BrainLoaded, pause, merr = d.ext.preTurn(ctx)
	}
	if merr != nil {
		d.mu.Lock()
		if gen == d.gen {
			tl.Kind = "error"
			d.logTurn(tl)
			d.st.Note = "Couldn't check the backend: " + plainErr(merr)
			d.noteHealthErrLocked(merr)
			d.endPauseLocked(true) // no brain turn: a reply it stopped stays stopped
			d.settleLocked()
		}
		d.mu.Unlock()
		return
	}
	if pause {
		d.mu.Lock()
		if gen == d.gen {
			d.refuseVoiceModeLocked(tl)
		}
		d.mu.Unlock()
		return
	}

	// The context item is read while the turn is transcribed, so it costs the turn nothing.
	ctxLine := make(chan string, 1)
	go func() { ctxLine <- d.contextLine(ctx) }()

	t0 := time.Now()
	text, err := typed, error(nil)
	if pcm != nil {
		text, err = d.voice.Transcribe(ctx, pcm)
		tl.ASRMS = time.Since(t0).Milliseconds()
	}

	d.mu.Lock()
	if gen != d.gen {
		d.mu.Unlock()
		return
	}
	_, card := d.openCardLocked()
	fast, isFast := intent.Match(text, card != nil)
	d.mu.Unlock()
	if isFast && fast.Kind == "math" && !d.localBrain() {
		isFast = false // a capable brain does sums; only the small local one is kept from them
	}
	if err == nil && isFast && !d.fastResolves(ctx, fast) {
		if app, ok := asApp(fast); ok {
			fast = app // "open firefox": no space or agent by that name, but an app
		} else {
			isFast = false // "tell me a joke": no agent, space or app by that name, so the brain hears it
		}
	}
	d.mu.Lock()
	if gen != d.gen {
		d.mu.Unlock()
		return
	}
	switch {
	case err != nil:
		tl.Kind = "error"
		d.st.Note = "Couldn't hear that: " + plainErr(err)
		d.noteHealthErrLocked(err)
		d.logTurn(tl)
		d.endPauseLocked(true) // no brain turn: a reply it stopped stays stopped
		d.settleLocked()
		d.mu.Unlock()
		return
	case text == "":
		tl.Kind = "empty"
		d.st.Note = "Didn't catch that"
		d.logTurn(tl)
		d.endPauseLocked(true) // no brain turn: a reply it stopped stays stopped
		d.settleLocked()
		d.mu.Unlock()
		return
	case isFast:
		tl.FastMatch = true
		d.st.Heard = text
		d.endPauseLocked(true) // no brain turn: a reply it stopped stays stopped
		d.publishLocked()
		d.mu.Unlock()
		d.fastTurn(gen, fast, released, tl)
		return
	}
	tl.Kind = "brain"
	d.st.Heard, d.st.Choices = text, nil
	d.publishLocked()

	d.mu.Unlock()

	d.mu.Lock()
	if d.memStale && d.turn == nil {
		d.memStale = false
		d.dropSessionLocked() // the next session's instructions carry what was just remembered
	}
	d.mu.Unlock()
	sess, isNew, dial, err := d.session(ctx)
	d.mu.Lock()
	if gen != d.gen {
		if sess != nil && d.sess == sess && d.turn == nil && d.batch == nil {
			d.idleCloseLocked(sess) // a press came while it dialled: don't leave the call open
		}
		d.mu.Unlock()
		return
	}
	if err != nil {
		tl.Kind = "error"
		d.st.Note = "Couldn't reach the computer's brain: " + plainErr(err)
		d.noteHealthErrLocked(err)
		d.logTurn(tl)
		d.endPauseLocked(true) // no brain turn: a reply it stopped stays stopped
		d.settleLocked()
		d.mu.Unlock()
		return
	}
	tl.Session, tl.DialMS = "warm", 0
	if isNew {
		tl.Session, tl.DialMS = "new", dial.Milliseconds()
	}
	if d.sess != sess { // dropped (idle close, a pause) while we got it
		tl.Kind = "error"
		d.st.Note = "That didn't go through: try again"
		d.logTurn(tl)
		d.endPauseLocked(true) // no brain turn: a reply it stopped stays stopped
		d.settleLocked()
		d.mu.Unlock()
		return
	}
	if !d.allowed(sess.Model()) { // the backend paused since the check
		d.refuseVoiceModeLocked(tl)
		d.mu.Unlock()
		return
	}
	tl.Brain = sess.Model()
	r := &turnRun{gen: gen, you: text, awaiting: true, log: tl, released: released, last: time.Now()}
	d.turn = r
	if d.idle != nil {
		d.idle.Stop()
	}
	if d.st.Game && !d.gameNoted[sess] {
		d.gameNoted[sess] = true
		d.st.Note = slowNote
	}
	d.publishLocked()
	d.mu.Unlock()

	var line string
	select {
	case line = <-ctxLine:
	case <-time.After(300 * time.Millisecond):
	}
	if got := d.retrieve(ctx, text); got != "" {
		line = joinText(line, got)
	}
	d.mu.Lock()
	recent := d.recentTurnsLocked()
	d.mu.Unlock()
	if len(recent) > 4 {
		recent = recent[len(recent)-4:]
	}
	if th := d.think(ctx, line, text, recent); th != "" {
		line = joinText(line, "(Your private thinking before you answer; never say it or mention it: "+th+")")
	}
	// A card still open is cancelled first, and every call of it answered, so the brain never sees a
	// new request after a tool call it has no answer for.
	d.sendMu.Lock()
	d.mu.Lock()
	outs := d.flushBatchLocked(sess)
	d.mu.Unlock()
	err = sendOutputs(ctx, sess, outs)
	if err == nil && line != "" {
		err = sess.Say(ctx, "user", line)
	}
	if err == nil {
		err = sess.Say(ctx, "user", text)
	}
	if err == nil {
		err = sess.Respond(ctx)
	}
	d.sendMu.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		// A restarting backend fails turns for a few seconds.
		d.failTurnLocked(r, "That didn't go through (the backend may be restarting): "+plainErr(err))
		d.dropSessionLocked()
		return
	}
	// The service may go quiet (a refused brain answers response.create with only an error, a stuck
	// backend never answers): a turn with no event for turnTimeout ends. A long answer keeps it alive.
	go d.watchTurn(r)
}

// fastTurn does what the fast grammar matched: a stop word, a card answer, or a tier-0 desktop
// action, whose outcome is spoken with vc say, outside the session.
func (d *Daemon) fastTurn(gen int, f intent.Fast, released time.Time, tl turnLog) {
	tl.Kind = "fast"
	tl.Intents = []string{f.Kind}
	var say string
	var err error
	switch f.Kind {
	case "stop":
		tl.Kind, tl.Intents = "stop", nil
		d.mu.Lock()
		d.cancelCardsLocked()
		d.st.Note = "Stopped"
		d.mu.Unlock()
	case "yes":
		d.mu.Lock()
		d.ownConfirmLocked(gen)
		_, c := d.openCardLocked()
		voiceOK, id := c != nil && c.card.Voice, ""
		if c != nil {
			id = c.card.ID
		}
		d.mu.Unlock()
		if voiceOK { // that card, by its id: a tier-2 card shown meanwhile is never confirmed by voice
			err = d.AnswerCard(id, "confirm")
		} else {
			say = "That one needs the confirm key, " + strings.ReplaceAll(d.cfg.ConfirmKey, " + ", " ") + "."
		}
	case "no":
		d.mu.Lock()
		d.ownConfirmLocked(gen)
		d.mu.Unlock()
		err = d.AnswerCard("", "decline")
	case "undo":
		if err = d.Undo(); err == nil {
			say = d.phrase("undo", nil)
		}
	case "thanks":
		tl.Intents = nil
		say = joinText(d.phrase("thanks", nil), d.suggestion())
	case "hello":
		tl.Intents = nil
		if s := d.suggestion(); s != "" {
			say = joinText(strings.SplitN(d.phrase("hello", nil), ".", 2)[0]+".", s)
		} else {
			say = d.phrase("hello", nil)
		}
	case "math": // the honesty rule, kept by the grammar: no sums from a language model
		tl.Intents = nil
		say = d.phrase("math", nil)
	case "time": // the clock's words, never the model's
		tl.Intents = nil
		say = d.phrase("time", clockArgs(time.Now()))
	case "quiet", "talk":
		d.mu.Lock()
		d.setRepliesMutedLocked(f.Kind == "quiet")
		d.savePrefsLocked()
		if f.Kind == "quiet" {
			d.st.Note = "Quiet: no announcements until you say \"talk to me\""
		} else {
			d.st.Note = "Announcements on"
			say = d.phrase("announce.on", nil)
		}
		d.mu.Unlock()
	case "it", "tool":
		calls := d.fastCalls(f)
		tl.Intents = nil
		for _, c := range calls {
			tl.Intents = append(tl.Intents, c.Name)
		}
		tl.FastMS = time.Since(released).Milliseconds()
		d.mu.Lock()
		d.logTurn(tl)
		d.st.Choices = nil
		d.startFastBatchLocked(gen, calls, released)
		d.mu.Unlock()
		return
	case "edit":
		err = d.EditCard(f.Action)
	case "my_words":
		var swapped bool
		swapped, err = d.MyWords()
		if err == nil && !swapped {
			say = "This card has no words to swap."
		}
	default:
		var done []Done
		say, done, err = d.runFast(f)
		if err == nil {
			d.mu.Lock()
			d.st.Done = append(d.st.Done, done...)
			d.mu.Unlock()
		}
	}
	tl.FastMS = time.Since(released).Milliseconds()
	d.mu.Lock()
	heard := d.st.Heard
	if err != nil {
		tl.Status = "failed"
		d.st.Note = "Not done: " + plainErr(err)
		say = d.st.Note
	}
	d.logTurn(tl)
	if gen == d.gen && d.turn == nil { // (a confirm's outcome may be on its way: it sets the state)
		d.settleLocked()
	} else {
		d.publishLocked()
	}
	if say != "" && err == nil {
		d.st.Answer = say
	}
	if say != "" || heard != "" {
		d.addTurnLocked(Turn{You: heard, Computer: say, At: time.Now().Unix()})
		d.publishLocked()
	}
	d.mu.Unlock()
	if say != "" {
		go d.Say(say)
	}
}

const (
	turnTimeout = 45 * time.Second
)

// failTurnLocked ends r with a note, if it is still the turn being answered.
func (d *Daemon) failTurnLocked(r *turnRun, note string) {
	if d.turn != r {
		return
	}
	d.endPauseLocked(true)
	d.turn = nil
	r.log.Kind, r.log.Status = "error", "failed"
	d.logTurn(r.log)
	d.st.Note = note
	if d.sess != nil {
		s := d.sess
		go func() { // the service may still be writing it: stop that, so it doesn't land unheard in the history
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s.Cancel(ctx)
		}()
		// vcd keeps this history: a refused request may be refused again, so start over (the last
		// turns are replayed into the next session).
		d.dropSessionLocked()
	}
	if d.player != nil {
		d.player.Stop()
		d.player = nil
	}
	d.settleLocked()
}

// session returns the open computer session, or opens one and replays the last turns into it.
func (d *Daemon) session(ctx context.Context) (*voice.Session, bool, time.Duration, error) {
	d.dialMu.Lock()
	defer d.dialMu.Unlock()
	d.mu.Lock()
	if s := d.sess; s != nil {
		d.mu.Unlock()
		return s, false, 0, nil
	}
	replay := d.recentTurnsLocked()
	d.mu.Unlock()

	t0 := time.Now()
	s, err := d.voice.Dial(ctx, d.computerSession())
	if err != nil {
		return nil, false, 0, err
	}
	for _, t := range replay {
		if err := s.Say(ctx, "user", t.You); err != nil {
			s.Close()
			return nil, false, 0, err
		}
		if t.Computer != "" {
			if err := s.Say(ctx, "assistant", t.Computer); err != nil {
				s.Close()
				return nil, false, 0, err
			}
		}
	}
	dial := time.Since(t0)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sess != nil { // (only one turn runs at a time, but be safe)
		go s.Close()
		return d.sess, false, 0, nil
	}
	d.sess = s
	d.st.Brain = s.Model()
	d.noteGameLocked(s)
	d.idleCloseLocked(s)
	go d.events(s)
	return s, true, dial, nil
}

// events handles one session's server events until it ends.
func (d *Daemon) events(s *voice.Session) {
	for ev := range s.Events {
		d.mu.Lock()
		if d.sess == s {
			d.onEvent(s, ev)
		}
		d.mu.Unlock()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sess != s {
		return
	}
	// The session ended on the backend's side (it restarted).
	d.endPauseLocked(true)
	d.sess = nil
	d.forgetSessionLocked(s)
	if r := d.turn; r != nil {
		d.turn = nil
		r.log.Kind, r.log.Status = "error", "dropped"
		d.logTurn(r.log)
		d.st.Note = "Lost the backend mid-answer"
		d.settleLocked()
	}
}

// onEvent handles a server event; d.mu is held.
func (d *Daemon) onEvent(s *voice.Session, ev voice.Event) {
	r := d.turn
	if r != nil {
		r.last = time.Now()
	}
	switch ev.Type {
	case "response.function_call_arguments.done":
		if r == nil || r.awaiting {
			return
		}
		c, err := intent.ParseCall(ev.CallID, ev.Name, ev.Arguments)
		if err != nil {
			c.Args = nil // answered "not readable"
		}
		if len(r.calls) == 0 && r.round == 0 {
			r.log.FirstCallMS = time.Since(r.released).Milliseconds()
		}
		r.calls = append(r.calls, c)
	case "response.created":
		if r != nil && r.awaiting {
			r.awaiting = false
			if ev.Response != nil {
				r.respID = ev.Response.ID
			}
		}
	case "response.output_audio_transcript.delta":
		if r == nil || r.awaiting {
			return
		}
		if !r.firstText {
			r.firstText = true
			r.log.FirstTextMS = time.Since(r.released).Milliseconds()
		}
		// A small model may write a stray </think> into its answer: never shown or replayed.
		delta := strings.TrimSpace(thinkTagRE.ReplaceAllString(ev.Delta, " "))
		if delta == "" {
			return
		}
		if r.answer.Len() > 0 || r.prior != "" {
			r.answer.WriteString(" ")
		}
		r.answer.WriteString(delta)
		d.st.Answer = r.answer.String()
		d.publishLocked()
	case "response.output_audio.delta":
		if r == nil || r.awaiting {
			return
		}
		pcm, err := ev.Audio()
		if err != nil || len(pcm) == 0 {
			return
		}
		if r.log.FirstAudio == 0 {
			r.log.FirstAudio = time.Since(r.released).Milliseconds()
		}
		if d.st.Muted {
			return
		}
		if d.sayer != nil { // the answer comes first
			d.sayer.Stop()
			d.sayer = nil
		}
		if d.player == nil {
			p, err := audio.Play()
			if err != nil {
				d.st.Note = "The speaker didn't open: " + err.Error()
				d.publishLocked()
				return
			}
			d.player = p
		}
		if d.st.State != Speaking {
			d.st.State = Speaking
			d.publishLocked()
		}
		d.player.Write(pcm)
		d.level(audio.Level(pcm))
	case "response.done":
		if r == nil || r.awaiting || (ev.Response != nil && r.respID != "" && ev.Response.ID != r.respID) {
			return
		}
		d.turn = nil
		d.endPauseLocked(false) // the owner's turn is answered: a reply the hold stopped plays after it
		if ev.Response != nil {
			r.log.Status, r.log.Service = ev.Response.Status, ev.Response.Timings
		}
		// The backend may pick the brain each turn (a slower stand-in): log the one that answered.
		r.log.Brain = s.Model()
		r.log.Game = r.log.Game || d.slow(r.log.Brain)
		r.log.DoneMS = time.Since(r.released).Milliseconds()
		r.log.Muted = d.st.Muted
		for _, c := range r.calls {
			r.log.Intents = append(r.log.Intents, c.Name)
		}
		d.logTurn(r.log)
		if len(r.calls) > 0 { // its calls are answered first; the turn is kept once the outcome is said
			d.startBatchLocked(s, r)
			return
		}
		d.addTurnLocked(Turn{You: r.you, Computer: joinText(r.prior, r.answer.String()), At: time.Now().Unix()})
		d.idleCloseLocked(s)
		if d.player == nil {
			d.settleLocked()
			d.readOutLater(r.readOut)
			return
		}
		// Ready once the speaker has played it all.
		p, gen, readOut := d.player, d.gen, r.readOut
		go func() {
			done := make(chan struct{})
			go func() { p.Drain(); close(done) }()
			t := time.NewTicker(66 * time.Millisecond)
			defer t.Stop()
		wait:
			for {
				select {
				case <-done:
					break wait
				case <-t.C:
					if p.Remaining() > 0 {
						d.level(0.3) // the output's own level is in the past by now; keep the meter alive
					}
				}
			}
			d.level(0)
			d.mu.Lock()
			defer d.mu.Unlock()
			if d.player == p {
				d.player = nil
			}
			d.lastSpoke = time.Now()
			if d.gen == gen && d.rec == nil && d.turn == nil {
				d.settleLocked()
				d.readOutLater(readOut)
			}
		}()
	case "error":
		if ev.Error == nil || r == nil {
			return
		}
		if r.awaiting { // refused before it began: no response.done will come
			d.failTurnLocked(r, ev.Error.Message)
			return
		}
		d.st.Note = ev.Error.Message
		d.publishLocked()
	}
}

func (d *Daemon) addTurnLocked(t Turn) {
	d.turns = append(d.turns, t)
	if d.convo != nil { // kept on this Mac, so the thread survives restarts
		var tools []string
		if d.lastLog != nil && time.Since(d.lastLogAt) < time.Minute {
			tools = d.lastLog.Intents
		}
		d.convo.Append(convo.Turn{At: t.At, You: t.You, Computer: t.Computer, Kind: d.lastKind(), Tools: tools})
		if d.lastKind() == "brain" && t.You != "" {
			go d.learn(t.You)
		}
		d.sinceSummary++
		if d.sinceSummary >= 10 {
			d.sinceSummary = 0
			go d.summarizeOlder()
		}
	}
	if over := len(d.turns) - d.cfg.History; over > 0 {
		d.turns = d.turns[over:]
	}
}

// idleCloseLocked closes the session after the idle time, so an idle session holds nothing on the backend.
func (d *Daemon) idleCloseLocked(s *voice.Session) {
	if d.idle != nil {
		d.idle.Stop()
	}
	d.idle = time.AfterFunc(d.cfg.IdleClose(), func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.sess == s && d.turn == nil && d.batch == nil {
			d.dropSessionLocked()
		}
	})
}

func (d *Daemon) dropSessionLocked() {
	if s := d.sess; s != nil {
		d.sess = nil
		if b := d.batch; b != nil && b.sess == s { // its calls die with it
			d.closeBatchLocked(b)
			d.addTurnLocked(Turn{You: b.you, Computer: b.prior, At: time.Now().Unix()})
		}
		d.asked = nil
		d.forgetSessionLocked(s)
		go s.Close() // a close can wait seconds on the network: never under d.mu
	}
}

// stopOutputLocked stops the answer: the service stops writing it, and the speaker drops what it
// buffered. Whatever comes for it later is ignored.
func (d *Daemon) stopOutputLocked() {
	if r := d.turn; r != nil {
		d.turn = nil
		r.log.Kind, r.log.Status = "brain", "cancelled"
		r.log.DoneMS = time.Since(r.released).Milliseconds()
		d.logTurn(r.log)
		if r.answer.Len() > 0 || r.prior != "" {
			d.addTurnLocked(Turn{You: r.you, Computer: joinText(r.prior, r.answer.String()) + " …", At: time.Now().Unix()})
		}
		if d.sess != nil {
			s := d.sess
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				s.Cancel(ctx)
			}()
			d.idleCloseLocked(s)
		}
	}
	if d.player != nil {
		d.player.Stop()
		d.player = nil
	}
	if d.lead != nil {
		go d.lead.Stop()
		d.lead = nil
	}
	if d.sayCancel != nil {
		d.sayCancel()
		d.sayCancel = nil
	}
	if d.sayer != nil {
		d.sayer.Stop()
		d.sayer = nil
	}
	if d.st.State == Speaking || d.st.State == Thinking {
		d.st.State = Ready
		d.st.Note = "Stopped"
	}
}

// Cancel stops speech, any turn and any card (vc cancel).
func (d *Daemon) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pausedReply = nil
	d.stopReplyLocked()
	d.gen++
	d.cancelCardsLocked()
	d.confirmHeld = false
	if d.rec != nil {
		rec := d.rec
		d.rec = nil
		go rec.Stop()
		d.level(0)
	}
	d.stopOutputLocked()
	d.settleLocked()
}

// Say speaks text outside the session (vc say), and returns when it has been heard.
func (d *Daemon) Say(text string) error { return d.say(text, false) }

// say speaks text; a filler ("One sec, looking.") is said while a slow tool runs, and the answer after
// it waits for it to end (one voice at a time).
func (d *Daemon) say(text string, filler bool) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("nothing to say")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// A press, vc cancel or mute stops it from here on, even before the speech has come back.
	d.mu.Lock()
	if d.health.off != "" || d.health.paused { // off, or paused: speech would use the backend
		why := d.health.why
		if d.health.paused {
			why = pausedNote
		}
		d.mu.Unlock()
		return errors.New(why)
	}
	if d.st.Muted {
		d.mu.Unlock()
		return nil
	}
	d.lastActive = time.Now()
	if r := d.stopReplyLocked(); r != nil { // vc say goes first; the reply waits
		d.replyQ = append([]agentReply{*r}, d.replyQ...)
	}
	if d.sayCancel != nil {
		d.sayCancel()
	}
	d.sayCancel = cancel
	d.sayGen++
	mine := d.sayGen
	defer func() { // done: leave no handle behind (a later press would log a stop for nothing)
		d.mu.Lock()
		if d.sayGen == mine {
			d.sayCancel = nil
		}
		d.mu.Unlock()
	}()
	d.mu.Unlock()
	said := time.Now()
	body, err := d.voice.Speech(ctx, text, d.cfg.Voice)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return nil
		}
		return err
	}
	defer body.Close()
	// One voice at a time: what the brain is still saying plays out first (its text before its tool
	// calls), then this; a press or vc cancel meanwhile ends the wait (ctx).
	for {
		d.mu.Lock()
		busy := d.lead != nil || d.player != nil || (!filler && d.fillerOn)
		d.mu.Unlock()
		if !busy || ctx.Err() != nil {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if ctx.Err() != nil {
		return nil
	}
	p, err := audio.Play()
	if err != nil {
		return err
	}
	d.mu.Lock()
	if ctx.Err() != nil || d.st.Muted || d.rec != nil || d.turn != nil || d.player != nil ||
		d.st.State == Listening || d.st.State == Speaking { // stopped meanwhile, or a turn is on
		d.mu.Unlock()
		p.Stop()
		return nil
	}
	if d.sayer != nil {
		d.sayer.Stop()
	}
	d.sayer = p
	d.st.State = Speaking
	d.publishLocked()
	d.mu.Unlock()
	buf := make([]byte, 4800)
	carry := []byte{}
	first := true
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if first {
				first = false
				d.noteSayAudio(said)
			}
			data := append(carry, buf[:n]...)
			cut := len(data) &^ 1
			if p.Write(data[:cut]) != nil {
				break
			}
			d.level(audio.Level(data[:cut]))
			carry = append([]byte{}, data[cut:]...)
		}
		if err != nil {
			break
		}
	}
	p.Drain()
	d.level(0)
	d.mu.Lock()
	d.lastSpoke = time.Now()
	if d.sayer == p {
		d.sayer = nil
		if d.st.State == Speaking && d.rec == nil && d.turn == nil && d.batch == nil {
			d.settleLocked()
		}
	}
	d.mu.Unlock()
	return nil
}

// ---- health

// checkHealth works out whether the computer can listen: the keys and the endpoints (and the backend's
// own checks, if it has them).
func (d *Daemon) checkHealth(ctx context.Context, full bool) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var h healthInfo
	d.mu.Lock()
	paused := d.health.paused
	d.mu.Unlock()
	keep := false // the pause not read this time: keep what the daemon knows when writing
	unit := false
	if d.ext == nil {
		var skip bool
		if h, skip = d.genericHealth(ctx, full); skip {
			return
		}
	} else {
		if paused && !full {
			return // paused: only the 30 s check calls the backend
		}
		h, keep, unit = d.ext.health(ctx, full)
		// (one service)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if keep {
		h.paused = d.health.paused || unit
	}
	if h.paused { // paused: no reply goes on speaking
		d.pausedReply = nil
		d.stopReplyLocked()
	}
	// The backend paused: an idle computer session would keep it counted busy.
	if h.paused && d.sess != nil && d.turn == nil {
		d.dropSessionLocked()
	}
	// The open session still names the CPU stand-in until its next turn: the game file says it ended.
	gameEnded := !h.game && d.sessGame
	if gameEnded {
		d.sessGame = false
	}
	if h.off == "" && d.health.off != "" { // the models just came up: warm the brain's prompt cache
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			t0 := time.Now()
			err := d.voice.Warm(ctx, d.computerSession())
			b, _ := json.Marshal(map[string]any{"event": "warm", "ms": time.Since(t0).Milliseconds(), "ok": err == nil})
			d.log.Println(string(b))
		}()
	}
	changed := h.off != d.health.off || h.why != d.health.why || h.game != d.health.game ||
		h.paused != d.health.paused || gameEnded
	oldWhy := d.health.why
	d.health = h
	if !changed {
		return
	}
	if d.rec == nil && d.turn == nil && (d.st.State == Ready || d.st.State == Off) {
		if h.off != "" {
			d.st.State, d.st.Off, d.st.Why = Off, h.off, h.why
		} else {
			d.st.State, d.st.Off = Ready, ""
			if d.micBlocked != "" {
				d.st.State, d.st.Off, d.st.Why = Off, OffMic, d.micBlocked
			}
			// A press while Off or paused said why: once that is over it no longer holds.
			if (oldWhy != "" && d.st.Note == oldWhy) || (!h.paused && d.st.Note == pausedNote) {
				d.st.Note = ""
			}
		}
	}
	if d.st.State != Off {
		d.readyFlagsLocked()
	}
	d.publishLocked()
}

// noteHealthErrLocked turns a refused key seen mid-turn into the off state at once.
func (d *Daemon) noteHealthErrLocked(err error) {
	switch {
	case errors.Is(err, voice.ErrRefused):
		d.health.off, d.health.why = OffRefused, "The backend refused the key"
	case errors.Is(err, voice.ErrNoKey):
		d.health.off, d.health.why = OffKey, "No API key: "+strings.TrimPrefix(err.Error(), voice.ErrNoKey.Error()+": ")
	}
}

func plainErr(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "it took too long"
	case errors.Is(err, voice.ErrRefused):
		return "the key was refused"
	}
	return err.Error()
}

// logTurn logs a turn's timings (never words); d.mu is held.
func (d *Daemon) logTurn(t turnLog) {
	b, _ := json.Marshal(t)
	d.log.Println(string(b))
	if t.Event == "turn" {
		cp := t
		d.lastLog, d.lastLogAt = &cp, time.Now()
		d.st.Last = &cp
	}
}

// forgetSessionLocked drops what vcd knew about a session that ended: its slow mark goes with it.
func (d *Daemon) forgetSessionLocked(s *voice.Session) {
	delete(d.gameNoted, s)
	d.sessGame = false
	if d.st.State != Off {
		d.readyFlagsLocked()
		d.publishLocked()
	}
}

// watchTurn ends r if the service sends nothing for it for turnTimeout.
func (d *Daemon) watchTurn(r *turnRun) {
	t := time.NewTicker(turnTimeout / 9)
	defer t.Stop()
	for range t.C {
		d.mu.Lock()
		if d.turn != r {
			d.mu.Unlock()
			return
		}
		if time.Since(r.last) > turnTimeout {
			d.failTurnLocked(r, "The brain didn't answer")
			d.mu.Unlock()
			return
		}
		d.mu.Unlock()
	}
}

// refuseVoiceModeLocked ends a turn because the backend is paused: no sessions, the paused mark, and a
// notification (nothing spoken: speech would use the backend too).
func (d *Daemon) refuseVoiceModeLocked(tl turnLog) {
	tl.Kind = "paused"
	d.health.paused = true
	d.st.Note = pausedNote
	d.dropSessionLocked()
	d.logTurn(tl)
	d.settleLocked()
	go d.notify(pausedNote)
}

// readOutLater reads out what vcd shows but never gives the brain (window titles, file names), once
// the brain's question has played.
func (d *Daemon) readOutLater(text string) {
	if text != "" {
		go d.Say(text)
	}
}

// computerSession is ComputerSession with what the secretary remembers about the owner (read afresh:
// the owner may have edited the file), about 1500 characters, newest first.
func (d *Daemon) computerSession() voice.SessionConfig {
	sc := ComputerSession(d.cfg)
	if d.mem != nil {
		d.mem.Reload()
		if m := d.mem.Prompt(1500); m != "" {
			sc.Instructions += "\n\n" + m
		}
	}
	if d.convo != nil {
		if sum := d.convo.Summary(); sum != "" {
			if len(sum) > 2000 {
				sum = "…" + sum[len(sum)-2000:]
			}
			sc.Instructions += "\n\nEarlier conversations with " + d.cfg.Owner + " (what was actually said, oldest first; you may refer back to them):\n" + stripComments(sum)
		}
	}
	return sc
}

// ComputerSession is what vcd sets on the computer's session: the standing instructions and the intent
// catalogue (fixed, so the server's prompt cache holds them), with the config's chat_extra.
func ComputerSession(cfg config.Config) voice.SessionConfig {
	return voice.SessionConfig{
		Voice: cfg.Voice, Extra: cfg.ChatExtra,
		Instructions: cfg.Instructions + "\n\n" + intent.ToolInstructions, Tools: intent.Catalogue(),
		TurnDetection: nil, MaxOutputTokens: cfg.MaxOutputTokens, MaxSentences: cfg.MaxSentences, ContextTokens: cfg.ContextTokens,
		Rewrite: Humanize,
	}
}

// localBrain says whether the brain runs on this Mac (the small model, kept from sums and guesses).
func (d *Daemon) localBrain() bool {
	u := d.cfg.Chat.URL
	return strings.Contains(u, "127.0.0.1") || strings.Contains(u, "localhost")
}
