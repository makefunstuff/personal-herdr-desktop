package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"herdr-voice-computer/internal/herdr"
	"herdr-voice-computer/internal/intent"
	"herdr-voice-computer/internal/macos"
	"herdr-voice-computer/internal/voice"
)

// A batch is the tool calls of one brain response. Every call gets an output before anything else is
// sent to that session, so the brain never sees a new request after a call it has no answer for.
type batch struct {
	sess     *voice.Session // nil for a fast turn's: its outcome is only spoken
	you      string         // the turn's transcript
	prior    string         // what the brain said before its calls
	round    int            // 0 for the turn's own response, 1 for the response to its outputs
	released time.Time
	calls    []*callState
	respond  bool // once all are answered, ask the brain to say how it went
	closed   bool
	readOut  string // read out with vc say once the outcome has played
	gen      int    // d.gen when it began: a press since means the owner moved on, so no spoken outcome
}

type callState struct {
	call   intent.Call
	act    *action
	output string
	say    string // what vcd says of it
	done   bool
	run    bool // its action is running (confirmed, or tier 0): nothing else may answer it
	card   *Card
	timer  *time.Timer
}

// action is a planned intent: the card it needs (tier 1 and 2), what it runs, or, when nothing will
// run (asked back, refused, failed), its output now. A run's result is what vcd says out loud, and the
// brain's tool output too.
type action struct {
	tier     int
	minTier  int // a window matched only by its title (an app's or a website's words) needs a card
	card     Card
	run      func(ctx context.Context, text string) (string, []Done, error)
	out      string
	choices  []Choice
	readOut  string
	declined string // what is said when the owner says no to its card
}

func notDone(format string, a ...any) *action {
	return &action{out: "Not done: " + fmt.Sprintf(format, a...)}
}

// asked is a candidate vcd asked back about: "the second one" or its name picks it in the next turn.
type asked struct {
	kind  string // agent, space
	label string
	agent herdr.Agent
	space herdr.Space
}

// ---- tool calls

// startBatchLocked takes over a response that ended with tool calls.
func (d *Daemon) startBatchLocked(s *voice.Session, r *turnRun) {
	b := &batch{sess: s, you: r.you, prior: joinText(r.prior, r.answer.String()), round: r.round,
		released: r.released, respond: true, gen: d.gen}
	for _, c := range r.calls {
		b.calls = append(b.calls, &callState{call: c})
	}
	d.batch = b
	if d.player != nil { // what it said before its calls plays out (a tap still stops it); the outcome gets its own player
		p := d.player
		d.player = nil
		d.lead = p
		go func() {
			p.Drain()
			d.mu.Lock()
			if d.lead == p {
				d.lead = nil
			}
			d.lastSpoke = time.Now()
			d.mu.Unlock()
		}()
	}
	if d.idle != nil {
		d.idle.Stop()
	}
	d.st.State = Thinking
	d.publishLocked()
	go d.handleCalls(b)
}

// handleCalls plans each call, runs the tier-0 ones, then shows the first card or finishes.
func (d *Daemon) handleCalls(b *batch) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	d.mu.Lock()
	untrusted := d.untrusted
	d.mu.Unlock()
	switch {
	case len(b.calls) > 3:
		d.answerAll(b, "Not done: more than three things at once. Ask the owner to say them one at a time.")
	case b.round >= 1:
		// The response to tool outputs never acts: an ask-back is answered by the owner's next turn,
		// never by the brain picking a candidate itself .
		d.answerAll(b, "Not done: only the owner's own request runs a tool. Ask the owner.")
		d.mu.Lock()
		b.respond = false
		d.mu.Unlock()
	default:
		w, werr := d.herdr.World(ctx)
		for _, c := range b.calls {
			act := d.plan(ctx, c.call, w, werr, untrusted)
			d.mu.Lock()
			if b.closed {
				d.mu.Unlock()
				d.advance(b) // (it settles a Thinking left behind by the close)
				return
			}
			c.act = act
			if act.run == nil {
				c.output, c.done, c.say = act.out, true, spoken(act.out)
			}
			if len(act.choices) > 0 {
				d.st.Choices = append(d.st.Choices, act.choices...)
				b.readOut = joinText(b.readOut, act.readOut)
			}
			run := act.run != nil && act.tier == intent.Tier0
			c.run = run
			d.mu.Unlock()
			if run {
				stopFiller := d.fillerAfter(c.call.Name, b.gen)
				out, done := runAction(ctx, act, "")
				stopFiller()
				d.mu.Lock()
				c.output, c.done, c.run, c.say = out, true, false, spoken(out)
				d.st.Done = append(d.st.Done, done...)
				d.publishLocked()
				d.mu.Unlock()
			}
		}
	}
	d.advance(b)
}

func (d *Daemon) answerAll(b *batch, out string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range b.calls {
		c.output, c.done, c.say = out, true, spoken(out)
	}
}

// spoken is what vcd says of a tool's output: "Done: On the fortress layout." says "On the fortress layout."
func spoken(out string) string {
	if _, say, ok := strings.Cut(out, sayMark); ok {
		return say
	}
	for _, p := range []string{"Done: ", "Not done: ", "Not done yet: "} {
		if strings.HasPrefix(out, p) {
			out = strings.TrimPrefix(out, p)
			if p == "Not done: " && !strings.HasPrefix(out, "I ") {
				out = "Not done: " + out
			}
			break
		}
	}
	if out != "" {
		out = strings.ToUpper(out[:1]) + out[1:]
	}
	return out
}

func runAction(ctx context.Context, a *action, text string) (string, []Done) {
	out, done, err := a.run(ctx, text)
	if err != nil {
		return "Not done: " + err.Error(), []Done{{Text: "Failed: " + err.Error()}}
	}
	return out, done
}

// advance shows the next card, or, once every call has its output, sends the outputs and asks the
// brain for the spoken outcome.
func (d *Daemon) advance(b *batch) {
	d.sendMu.Lock()
	defer d.sendMu.Unlock()
	d.mu.Lock()
	if b.closed || d.batch != b { // closed meanwhile (a new turn, a dropped session): leave no Thinking behind
		if d.st.State == Thinking && d.rec == nil && d.turn == nil && d.batch == nil {
			d.settleLocked()
		}
		d.mu.Unlock()
		return
	}
	for _, c := range b.calls {
		if !c.done {
			if c.card == nil && !c.run {
				d.showCardLocked(b, c)
			}
			d.mu.Unlock()
			return
		}
	}
	outs := d.closeBatchLocked(b)
	s := b.sess
	// vcd says the outcome itself (the tools' results are its words): no second brain round.
	var says []string
	for _, c := range b.calls {
		if c.say != "" {
			says = append(says, c.say)
		}
	}
	say := strings.Join(says, " ")
	// A command with a little life: one grounded suggestion after it, at most (never after what already
	// says what waits, and never after a confirm's outcome).
	if b.sess == nil && len(b.calls) == 1 && b.calls[0].act != nil && b.calls[0].act.tier == intent.Tier0 && suggestAfter[b.calls[0].call.Name] {
		d.mu.Unlock()
		sug := d.suggestion()
		d.mu.Lock()
		say = joinText(say, sug)
	}
	d.addTurnLocked(Turn{You: b.you, Computer: joinText(b.prior, say), At: time.Now().Unix()})
	speak := b.respond && say != "" && d.gen == b.gen && d.rec == nil && d.turn == nil
	if s != nil && d.turn == nil {
		d.idleCloseLocked(s)
	}
	if d.rec == nil && d.turn == nil && (d.st.State == Thinking || d.st.State == Ready) {
		if speak {
			d.st.Answer = joinText(b.prior, say)
		}
		d.settleLocked()
	} else {
		d.publishLocked()
	}
	d.mu.Unlock()

	if s != nil {
		ctx, cancel := context.WithTimeout(context.Background(), voice.Timeout)
		defer cancel()
		if err := sendOutputs(ctx, s, outs); err != nil {
			d.mu.Lock()
			if d.sess == s {
				d.dropSessionLocked()
			}
			d.mu.Unlock()
		}
	}
	if speak {
		d.mu.Lock()
		d.sayFrom = b.released
		d.mu.Unlock()
		d.Say(say)
	}
	if b.readOut != "" {
		d.readOutLater(b.readOut)
	}
}

type output struct{ id, text string }

func sendOutputs(ctx context.Context, s *voice.Session, outs []output) error {
	for _, o := range outs {
		if err := s.Output(ctx, o.id, o.text); err != nil {
			return err
		}
	}
	return nil
}

// closeBatchLocked ends the batch: an open card goes, and every call without an output gets one
// saying it wasn't done. It returns the outputs to send (the caller holds sendMu).
func (d *Daemon) closeBatchLocked(b *batch) []output {
	b.closed = true
	if d.batch == b {
		d.batch = nil
	}
	var outs []output
	for _, c := range b.calls {
		if c.timer != nil {
			c.timer.Stop()
		}
		switch {
		case c.run: // it runs on: the owner went on before its result came
			c.output = "Started, as the owner asked; its result isn't known yet (the owner went on)."
		case !c.done:
			c.output, c.done = "Not done: the owner went on to something else first.", true
		}
		brain, _, _ := strings.Cut(c.output, sayMark) // what an agent wrote is said, never given to the brain
		outs = append(outs, output{c.call.ID, brain})
	}
	if d.st.Card != nil {
		d.st.Card = nil
		d.publishLocked()
	}
	if b.sess == nil {
		return nil
	}
	return outs
}

// flushBatchLocked closes the open batch before a new turn goes to the brain, and returns the outputs
// to send first (the caller holds sendMu). A batch of another session is just dropped.
func (d *Daemon) flushBatchLocked(s *voice.Session) []output {
	b := d.batch
	if b == nil {
		return nil
	}
	outs := d.closeBatchLocked(b)
	d.addTurnLocked(Turn{You: b.you, Computer: b.prior, At: time.Now().Unix()})
	if b.sess != s {
		return nil
	}
	return outs
}

// ---- confirms
//
// A "card" here is a spoken or key confirm, never a clickable card (nothing in the voice computer is clicked):
// one line in the overlay and a notification, answered by voice ("yes", "do it", "no", "send my
// words") or SUPER + ALT + F7; a timeout is a no.

func (d *Daemon) showCardLocked(b *batch, c *callState) {
	d.cardSeq++
	card := c.act.card
	card.ID = "card" + strconv.Itoa(d.cardSeq)
	card.Intent = c.call.Name
	card.Tier = c.act.tier
	card.Voice = c.act.tier == intent.Tier1
	card.Expires = time.Now().Add(d.cfg.CardTimeout()).UnixMilli()
	card.YouSaid = b.you // always shown: a misheard word is caught before anything runs (2.1)
	if d.untrusted != "" {
		card.Untrusted = d.untrusted
	}
	var carded []*callState
	for _, x := range b.calls {
		if x.act != nil && x.act.tier > intent.Tier0 && x.act.run != nil {
			carded = append(carded, x)
		}
	}
	for i, x := range carded {
		if x == c && len(carded) > 1 {
			card.Of = fmt.Sprintf("%d of %d", i+1, len(carded))
		}
	}
	c.card = &card
	cp := card
	d.st.Card = &cp
	d.st.Overlay = true
	d.hideGen++
	id := card.ID
	c.timer = time.AfterFunc(d.cfg.CardTimeout(), func() { d.AnswerCard(id, "timeout") })
	if d.st.State == Thinking {
		d.st.State = Ready
		d.readyFlagsLocked()
	}
	// Asked out loud and shown: answered by voice ("yes" / "no"), the confirm key or the overlay.
	how := "say yes or no, or press " + d.cfg.ConfirmKey
	if !card.Voice {
		how = "press " + d.cfg.ConfirmKey + " (not by voice)"
	}
	go d.notify("Confirm: " + card.Title + " · " + how + "; no answer in " + durationWords(d.cfg.CardTimeoutS) + " is a no")
	ask := card.Ask
	if ask == "" {
		ask = card.Title + "?"
	}
	if card.Voice {
		ask += " Say yes or no."
	}
	d.st.Answer = ask
	b.prior = joinText(b.prior, ask) // the turn says it asked, then what came of it
	go d.Say(ask)
	b2, _ := json.Marshal(map[string]any{"event": "card", "intent": c.call.Name, "tier": card.Tier,
		"card_ms": time.Since(b.released).Milliseconds()})
	d.log.Println(string(b2))
	d.publishLocked()
}

var ErrNoCard = errors.New("no card is open")

// AnswerCard answers the open card (id "" for whichever is open): confirm, decline, timeout or
// cancel. A timeout or a cancel (F7 tap, a stop word, a submap) ends the rest of the turn's cards too.
func (d *Daemon) AnswerCard(id, how string) error {
	d.mu.Lock()
	b, c := d.openCardLocked()
	if c == nil || (id != "" && c.card.ID != id) {
		d.mu.Unlock()
		return ErrNoCard
	}
	c.timer.Stop()
	d.st.Card = nil
	switch how {
	case "confirm":
		act, text := c.act, c.card.Text
		c.run = true // a second confirm, a cancel or a new turn can't answer it now
		if d.rec == nil {
			d.st.State = Thinking
		}
		b.gen = d.gen // the owner's answer: the outcome is spoken
		b.released = time.Now()
		d.publishLocked()
		d.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		out, done := runAction(ctx, act, text)
		cancel()
		d.mu.Lock()
		c.output, c.done, c.run, c.say = out, true, false, spoken(out)
		d.st.Done = append(d.st.Done, done...)
		d.publishLocked()
	case "decline":
		c.output, c.done, c.say = "Not done: the owner said no.", true, c.act.declined
		if c.say == "" {
			c.say = d.phrase("send.declined", nil)
		}
		b.gen = d.gen
		d.st.Done = append(d.st.Done, Done{Text: "Didn't: " + c.card.Title})
	case "timeout", "cancel":
		why := "Not done: the owner cancelled it."
		if how == "timeout" {
			why = "Not done: the card timed out with no answer."
			d.st.Note = "The card timed out"
		}
		for _, x := range b.calls {
			if !x.done && !x.run {
				x.output, x.done = why, true
			}
		}
		b.respond = false
	default:
		d.mu.Unlock()
		return fmt.Errorf("a card is answered with confirm, decline, timeout or cancel")
	}
	d.publishLocked()
	d.mu.Unlock()
	d.advance(b)
	return nil
}

func (d *Daemon) openCardLocked() (*batch, *callState) {
	b := d.batch
	if b == nil || b.closed {
		return nil, nil
	}
	for _, c := range b.calls {
		if c.card != nil && !c.done && !c.run {
			return b, c
		}
	}
	return b, nil
}

// cancelCardsLocked cancels the open card and the rest of its turn's (a tap, a stop word, a submap).
func (d *Daemon) cancelCardsLocked() {
	if _, c := d.openCardLocked(); c != nil {
		go d.AnswerCard(c.card.ID, "cancel")
	}
}

// ConfirmKey is SUPER + ALT + F7. "down" is its press; "up" (its release, or the plain F7 release
// that Hyprland fires when SUPER or ALT was let go first) confirms, once per press. "" confirms the
// open card at once (vc confirm by hand); any other arg is a card id (that one only).
func (d *Daemon) ConfirmKey(arg string) error {
	switch arg {
	case "down":
		d.mu.Lock()
		d.confirmHeld = true
		d.mu.Unlock()
		return nil
	case "up":
		d.mu.Lock()
		held := d.confirmHeld
		d.confirmHeld = false
		d.mu.Unlock()
		if !held {
			return nil // this press already confirmed (the other release bind fired too)
		}
		if err := d.AnswerCard("", "confirm"); err != ErrNoCard {
			return err
		}
		return nil // the key does nothing without a card
	}
	return d.AnswerCard(arg, "confirm")
}

// MyWords swaps the transcript in for the brain's wording on the open card, and shows it again; it
// never sends.
func (d *Daemon) MyWords() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, c := d.openCardLocked()
	if c == nil {
		return false, ErrNoCard
	}
	if c.card.Text == "" {
		return false, nil
	}
	c.card.Text = c.card.YouSaid
	c.card.Expires = time.Now().Add(d.cfg.CardTimeout()).UnixMilli()
	c.timer.Reset(d.cfg.CardTimeout())
	cp := *c.card
	d.st.Card = &cp
	d.st.Overlay = true
	d.hideGen++
	d.publishLocked()
	return true, nil
}

// Undo takes back the last volume change (vc undo).
func (d *Daemon) Undo() error {
	d.mu.Lock()
	level, ok := d.undoVolume, d.undoOK
	d.undoOK = false
	d.mu.Unlock()
	if !ok {
		return errors.New("nothing to undo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := macos.SetVolume(ctx, level); err != nil {
		return err
	}
	d.mu.Lock()
	for i := range d.st.Done {
		d.st.Done[i].Undo = false
	}
	d.st.Done = append(d.st.Done, Done{Text: fmt.Sprintf("Volume back to %d%%", level)})
	d.publishLocked()
	d.mu.Unlock()
	return nil
}

// ---- fast intents

// runFast does a fast desktop intent (media, volume, mute) and says what was done.
func (d *Daemon) runFast(f intent.Fast) (string, []Done, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	switch f.Kind {
	case "media":
		return d.doMedia(ctx, f.Action)
	case "volume":
		return d.doVolume(ctx, f.Action, f.Level, true)
	case "mute":
		return d.doVolume(ctx, f.Action, 0, false)
	}
	return "", nil, fmt.Errorf("not a fast intent")
}

const volumeStep = 10

func (d *Daemon) doVolume(ctx context.Context, action string, level int, hasLevel bool) (string, []Done, error) {
	switch action {
	case "mute", "unmute":
		changed, err := macos.SetMute(ctx, action == "mute")
		if err != nil {
			return "", nil, err
		}
		word := map[string]string{"mute": "Muted", "unmute": "Unmuted"}[action]
		if !changed {
			word = map[string]string{"mute": "Already muted", "unmute": "Not muted"}[action]
		}
		return word, []Done{{Text: word}}, nil
	case "set":
		if !hasLevel || level < 0 || level > 100 {
			return "", nil, errors.New("a volume is 0 to 100")
		}
		before, err := macos.SetVolume(ctx, level)
		if err != nil {
			return "", nil, err
		}
		d.noteUndo(before)
		return fmt.Sprintf("Volume at %d%%", level), []Done{{Text: fmt.Sprintf("Volume %d%% → %d%%", before, level), Undo: true}}, nil
	case "up", "down":
		step := volumeStep
		if action == "down" {
			step = -step
		}
		before, after, err := macos.StepVolume(ctx, step)
		if err != nil {
			return "", nil, err
		}
		d.noteUndo(before)
		return fmt.Sprintf("Volume at %d%%", after), []Done{{Text: fmt.Sprintf("Volume %d%% → %d%%", before, after), Undo: true}}, nil
	}
	return "", nil, errors.New("volume takes set, up, down, mute or unmute")
}

func (d *Daemon) noteUndo(before int) {
	d.mu.Lock()
	d.undoVolume, d.undoOK = before, true
	d.mu.Unlock()
}

func (d *Daemon) doMedia(ctx context.Context, action string) (string, []Done, error) {
	if err := macos.MediaAction(ctx, action); err != nil {
		return "", nil, err
	}
	word := map[string]string{"play": "Playing", "pause": "Paused", "next": "Next track",
		"previous": "Previous track"}[action]
	return word, []Done{{Text: word}}, nil
}

// ---- the context item

// contextLine is the turn's context for the brain, the only facts it may say besides its tools'
// results: the time and date, the shown layout, the focused space, tab and agent, how many agents work
// or wait, the spaces' and agents' names (to pick from), whether the secretary is quiet, the reminders.
// Never an agent's screen or words.
func (d *Daemon) contextLine(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	parts := []string{time.Now().Format("time 15:04, Monday 2 January 2006")}
	if w, err := d.herdr.World(ctx); err == nil {
		if w.Status.Layout != "" {
			parts = append(parts, "layout "+w.Status.Layout)
		}
		if sp, ok := w.ActiveSpace(); ok {
			parts = append(parts, "focused space "+herdr.Spoken(sp.Name))
		}
		if a, ok := w.Focused(); ok {
			parts = append(parts, "focused agent "+a.Label()+" ("+a.Status+")")
		}
		n := map[string]int{}
		var names []string
		for _, a := range w.Agents() {
			n[a.Status]++
			names = append(names, a.Label())
		}
		parts = append(parts, fmt.Sprintf("%d agents working, %d waiting on the owner, %d finished, %d idle",
			n["working"], n["blocked"], n["done"], n["idle"]))
		var spaces []string
		for _, sp := range w.Spaces {
			spaces = append(spaces, herdr.Spoken(sp.Name))
		}
		parts = append(parts, "spaces: "+strings.Join(spaces, ", "))
		if len(names) > 0 && len(names) <= 30 {
			parts = append(parts, "agents: "+strings.Join(names, ", "))
		}
	}
	d.mu.Lock()
	if d.st.RepliesMuted {
		parts = append(parts, "quiet: on")
	} else {
		parts = append(parts, "quiet: off")
	}
	parts = append(parts, fmt.Sprintf("%d reminders set", len(d.sec.reminders)))
	d.mu.Unlock()
	if level, muted, err := macos.Volume(ctx); err == nil {
		if muted {
			parts = append(parts, fmt.Sprintf("volume %d%%, muted", level))
		} else {
			parts = append(parts, fmt.Sprintf("volume %d%%", level))
		}
	}
	return ContextPrefix + strings.Join(parts, "; ") + ")"
}

// ContextPrefix starts the context item, so the brain can tell it from the owner's words.
const ContextPrefix = "(Context, not said by the owner: "

func joinText(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " " + b
}

// ownConfirmLocked: the press that answered the confirm by voice is the owner's own turn for its
// batch, not a move on to something else, so the outcome is still spoken.
func (d *Daemon) ownConfirmLocked(gen int) {
	if d.batch != nil && gen == d.gen { // (a newer press means the owner moved on after all)
		d.batch.gen = d.gen
	}
}

// suggestAfter are the commands a grounded suggestion may follow.
var suggestAfter = map[string]bool{"switch_layout": true, "shepherdr_panel": true, "go_to_space": true, "write_note": true,
	"agent_step": true, "list_notes": true, "pomodoro": true}
