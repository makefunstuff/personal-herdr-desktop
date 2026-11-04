package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"herdr-voice-computer/internal/audio"
	"herdr-voice-computer/internal/shepherdr"
	"herdr-voice-computer/internal/speech"
)

// Agent replies: what coordinators write to the owner in their Shepherdr chats is spoken on the desktop,
// in the agents' voice, outside any session. It is text to speak only: it never reaches the brain, its
// session or the turns replayed into one, and it is never logged.

// replySource reads Shepherdr with vcd's own credential (shepherdr.Client; a fake in tests).
type replySource interface {
	Spaces(ctx context.Context) ([]shepherdr.Space, error)
	Chat(ctx context.Context, space string, limit int) ([]shepherdr.Message, error)
}

// agentReply is one agent reply waiting to be spoken.
type agentReply struct {
	from string // the space, as configured
	text string // already plain speech
	news bool   // its turn began with Shepherdr's news, not the owner: it waits for the owner to be idle
	at   time.Time
	sent time.Time // when the agent wrote it
}

const (
	chatLimit = 20
	// One shepherdr voice command. Longer than the command's own 60 s: a request killed before its answer
	// stays in Shepherdr's queue for good, and 32 of them stop vcd's credential.
	shepherdrTimeout = 90 * time.Second
	replyGap         = 2 * time.Second         // quiet after the owner's last press before any reply
	ownerTurn        = 10 * time.Minute        // a reply this soon after the owner's message answers it
	replyStable      = 20 * time.Second        // a reply written mid-turn is spoken once no newer one came for this long
	replyExpire      = 2 * time.Minute         // a reply or announcement held longer than this is dropped (the inbox still shows it)
	quietAfterSpeech = 1500 * time.Millisecond // after the computer spoke, before an announcement
	replyQueueAt     = 5                       // most replies waiting; the oldest goes first
)

// The spoken phrases that mute and unmute agent replies (fast intents: they never reach the brain).
var (
	repliesOffRE = regexp.MustCompile(`(?i)^\W*(be quiet|mute (the )?(replies|agents|coordinators))\W*$`)
	repliesOnRE  = regexp.MustCompile(`(?i)^\W*(talk to me|unmute (the )?(replies|agents|coordinators))\W*$`)
)

// chatWatch is what vcd knows of one watched chat.
type chatWatch struct {
	seen      map[string]bool
	primed    bool // the chat was read once: what was there before vcd started is never spoken
	status    string
	lastFetch time.Time
}

// watchReplies asks Shepherdr every poll whether the watched coordinators are working, and reads a chat
// when its coordinator's status changes (and every few polls anyway), until ctx ends.
func (d *Daemon) watchReplies(ctx context.Context) {
	if d.replies == nil || len(d.cfg.ReplySpaces) == 0 {
		return
	}
	watch := map[string]*chatWatch{}
	lastErr := ""
	backoff := time.Duration(0)
	for {
		t0 := time.Now()
		err := d.pollReplies(ctx, watch)
		// Shepherdr queues each request (32 at most per credential) and serves them one by one: never ask
		// faster than it answers, and back off while it fails.
		wait := max(d.cfg.ReplyPoll(), time.Since(t0))
		if err != nil {
			backoff = min(max(2*backoff, d.cfg.ReplyPoll()), time.Minute)
			wait = max(wait, backoff)
		} else {
			backoff = 0
		}
		if msg := fmt.Sprint(err); err != nil && msg != lastErr && ctx.Err() == nil {
			b, _ := json.Marshal(map[string]any{"event": "replies", "error": plainShepherdrErr(err)})
			d.log.Println(string(b))
		}
		lastErr = fmt.Sprint(err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func plainShepherdrErr(err error) string {
	s := err.Error()
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

func (d *Daemon) pollReplies(ctx context.Context, watch map[string]*chatWatch) error {
	d.mu.Lock()
	silent := d.st.Muted || d.st.RepliesMuted || d.health.paused
	d.mu.Unlock()
	if silent { // nothing could be spoken: don't ask Shepherdr, and read each chat afresh afterwards
		for _, w := range watch {
			w.primed = false
		}
		return nil
	}
	sctx, cancel := context.WithTimeout(ctx, shepherdrTimeout)
	spaces, err := d.replies.Spaces(sctx)
	cancel()
	if err != nil {
		return err
	}
	// The chats to read: a coordinator's status changed, a chat not read yet, or not read for 5 polls.
	// Read one at a time (one request in Shepherdr's queue), each with its own time limit.
	type read struct {
		name string
		sp   shepherdr.Space
		w    *chatWatch
		msgs []shepherdr.Message
		err  error
		read bool
	}
	var reads []*read
	var errs []error
	for _, name := range d.cfg.ReplySpaces {
		sp := findSpace(spaces, name)
		if sp == nil {
			errs = append(errs, fmt.Errorf("no space %q in Shepherdr", name))
			continue
		}
		w := watch[name]
		if w == nil {
			w = &chatWatch{seen: map[string]bool{}}
			watch[name] = w
		}
		if w.primed && sp.Status == w.status && time.Since(w.lastFetch) < 5*d.cfg.ReplyPoll() {
			continue
		}
		reads = append(reads, &read{name: name, sp: *sp, w: w})
	}
	for _, r := range reads {
		cctx, cancel := context.WithTimeout(ctx, shepherdrTimeout)
		r.msgs, r.err = d.replies.Chat(cctx, r.sp.ID, chatLimit)
		cancel()
		r.read = true
		if r.err != nil {
			break // Shepherdr is struggling: the rest wait for the next poll
		}
	}
	for _, r := range reads {
		if !r.read {
			continue // not asked this time (an earlier read failed)
		}
		if r.err != nil {
			errs = append(errs, r.err) // its status stays unread: the next poll reads it again
			continue
		}
		r.w.status, r.w.lastFetch = r.sp.Status, time.Now()
		if rep, ok := r.w.take(r.msgs, r.sp.Status == "work", time.Now()); ok {
			rep.from = r.name
			d.queueReply(rep)
		}
	}
	return errors.Join(errs...)
}

// findSpace is the space named name (or with that id). Two of one name (a home space on each machine):
// the one on the machine with the most spaces, which is the main one.
func findSpace(spaces []shepherdr.Space, name string) *shepherdr.Space {
	var hits []int
	count := map[string]int{}
	for i, s := range spaces {
		if s.ID == name {
			return &spaces[i]
		}
		count[machineOf(s.ID)]++
		if strings.EqualFold(s.Space, name) {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		return nil
	}
	best := hits[0]
	for _, i := range hits[1:] {
		if count[machineOf(spaces[i].ID)] > count[machineOf(spaces[best].ID)] {
			best = i
		}
	}
	return &spaces[best]
}

// machineOf is the machine part of a space id, p:<machine>:<path>.
func machineOf(id string) string {
	parts := strings.SplitN(id, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

// take is the reply to speak from the chat's latest messages, if a new one is ready: the newest agent
// text not seen yet, once its coordinator has finished the turn (or has written nothing newer for
// replyStable). Shepherdr's news cards, choice cards and the owner's own words are never spoken.
func (w *chatWatch) take(msgs []shepherdr.Message, working bool, now time.Time) (agentReply, bool) {
	window := map[string]bool{}
	for _, m := range msgs {
		window[m.ID] = true
	}
	for id := range w.seen { // forget what scrolled out of the window
		if !window[id] {
			delete(w.seen, id)
		}
	}
	markAll := func() {
		for _, m := range msgs {
			w.seen[m.ID] = true
		}
	}
	if !w.primed {
		w.primed = true
		markAll()
		return agentReply{}, false
	}
	pick := -1
	for i, m := range msgs {
		if !w.seen[m.ID] && agentText(m) {
			pick = i
		}
	}
	if pick < 0 {
		markAll()
		return agentReply{}, false
	}
	m := msgs[pick]
	sent := time.UnixMilli(int64(m.At))
	if working && now.Sub(sent) < replyStable {
		return agentReply{}, false // the turn goes on: it may write the real answer next
	}
	markAll()
	text := speech.Plain(m.Text)
	if text == "" {
		return agentReply{}, false
	}
	return agentReply{text: text, news: newsTurn(msgs, pick), at: now, sent: sent}, true
}

// Shepherdr's own plain lines in a chat (a stopped send, a browser handoff, setup and cleanup notes), by
// their ids: Shepherdr writes them, not the agent, so they are never spoken.
var shepherdrLineRE = regexp.MustCompile(`^(cancelled|delivery|setup|permission|kept|cleanup|base-sync|browser-cleanup|native-|github-|pilot-wake|artifact-|activity-)`)

func agentText(m shepherdr.Message) bool {
	return m.Who == "agent" && strings.TrimSpace(m.Text) != "" && !m.IsCard() && m.Question == nil &&
		!shepherdrLineRE.MatchString(m.ID) && !strings.Contains(m.Text, "belonged to the previous session")
}

// newsTurn says whether the turn that wrote msgs[i] began with something other than the owner's words:
// Shepherdr's news, a heartbeat or another tagged line (those aren't in the chat). Activity rows can't
// tell (their ids name the agent's log, not the chat message), so: the owner's message is the latest
// thing before the reply that isn't the agent's own (its earlier words, activity rows), and came within
// ownerTurn. Not knowing counts as news: it waits for the owner to be idle.
func newsTurn(msgs []shepherdr.Message, i int) bool {
	for j := i - 1; j >= 0; j-- {
		m := msgs[j]
		switch {
		case m.Who == "you":
			return msgs[i].At-m.At > float64(ownerTurn.Milliseconds())
		case m.IsCard() || m.Question != nil:
			return true
		}
	}
	return true
}

// queueReply keeps r to speak when the owner and the computer are quiet; dropped when speech is muted,
// agent replies are muted or the backend paused vcd (the repeat key still has it).
func (d *Daemon) queueReply(r agentReply) {
	d.mu.Lock()
	defer d.mu.Unlock()
	last := r
	d.lastReply = &last
	why := ""
	switch {
	case d.st.Muted:
		why = "muted"
	case d.st.RepliesMuted:
		why = "replies_muted"
	case d.health.paused:
		why = "voice_mode"
	}
	if why != "" {
		d.logReply(r, "dropped", why)
		return
	}
	d.replyQ = append(d.replyQ, r)
	if over := len(d.replyQ) - replyQueueAt; over > 0 {
		for _, old := range d.replyQ[:over] {
			d.logReply(old, "dropped", "queue_full")
		}
		d.replyQ = d.replyQ[over:]
	}
	d.logReply(r, "queued", "")
	d.wakeRepliesLocked()
}

func (d *Daemon) wakeRepliesLocked() {
	select {
	case d.replyWake <- struct{}{}:
	default:
	}
}

// logReply logs what happened to a reply: its space and times, never its words.
func (d *Daemon) logReply(r agentReply, what, why string) {
	m := map[string]any{"event": "reply", "what": what, "space": r.from, "news": r.news}
	if why != "" {
		m["why"] = why
	}
	if !r.sent.IsZero() {
		m["lag_ms"] = time.Since(r.sent).Milliseconds() // from the agent writing it
	}
	b, _ := json.Marshal(m)
	d.log.Println(string(b))
}

// playReplies speaks queued replies one at a time, each when nothing else is heard or held, until ctx ends.
func (d *Daemon) playReplies(ctx context.Context) {
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.replyWake:
		case <-t.C:
		}
		d.mu.Lock()
		r, ok := d.nextReplyLocked()
		d.mu.Unlock()
		if ok {
			d.playReply(r)
		}
	}
}

// voxtypeBusy: there is no dictation on the Mac port (kept so the reply logic reads as the original's).
func voxtypeBusy(string) bool { return false }

// quietLocked says whether nothing is heard or held: no key down, no turn, no answer or vc say playing.
func (d *Daemon) quietLocked() bool {
	return !d.st.Muted && !d.st.RepliesMuted && d.health.off == "" && !d.health.paused && !d.stopping && d.rec == nil && d.turn == nil &&
		d.player == nil && d.sayer == nil && d.sayCancel == nil && d.st.State == Ready &&
		d.lead == nil && d.batch == nil && d.st.Card == nil // nor tool calls being answered, nor a card open
}

// nextReplyLocked takes the next reply to speak now, if the moment is right.
func (d *Daemon) nextReplyLocked() (agentReply, bool) {
	if d.health.paused && len(d.replyQ) > 0 { // paused: nothing is spoken, and nothing waits to be
		for _, r := range d.replyQ {
			d.logReply(r, "dropped", "voice_mode")
		}
		d.replyQ = nil
	}
	keep := d.replyQ[:0]
	for _, r := range d.replyQ {
		if time.Since(r.at) > replyExpire {
			d.logReply(r, "dropped", "expired")
			continue
		}
		keep = append(keep, r)
	}
	d.replyQ = keep
	if len(d.replyQ) == 0 || !d.quietLocked() {
		return agentReply{}, false
	}
	if time.Since(d.lastActive) < replyGap {
		return agentReply{}, false // the owner just pressed (a tap to stop, say): not straight after
	}
	if time.Since(d.lastSpoke) < quietAfterSpeech {
		return agentReply{}, false // the turn's answer just ended: a moment of quiet first
	}
	idle := time.Since(d.lastActive) >= d.cfg.ReplyIdle()
	for _, news := range []bool{false, true} { // replies to the owner first, then news once the owner is idle
		for i, r := range d.replyQ {
			if r.news == news && (!news || idle) {
				d.replyQ = append(d.replyQ[:i:i], d.replyQ[i+1:]...)
				if r.from == d.cfg.Name { // the secretary's announcements waiting together are said as one
					keep := d.replyQ[:0]
					for _, x := range d.replyQ {
						if x.from == r.from {
							r.text = joinText(r.text, x.text)
						} else {
							keep = append(keep, x)
						}
					}
					d.replyQ = keep
				}
				return r, true
			}
		}
	}
	return agentReply{}, false
}

// playReply speaks r in the agents' voice and shows it in the overlay. Whatever stops it decides what
// becomes of it (stopReplyLocked, pauseReplyLocked): a hold of F7 plays it again after the owner's turn,
// a tap or vc cancel drops it, vc say goes first and it waits, mute drops it.
func (d *Daemon) playReply(r agentReply) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cur := &r
	d.mu.Lock()
	if !d.quietLocked() {
		if d.st.Muted || d.st.RepliesMuted || d.health.paused {
			d.mu.Unlock()
			return // muted or paused just now: it goes, as the rest of the queue did
		}
		d.replyQ = append([]agentReply{r}, d.replyQ...)
		d.mu.Unlock()
		return
	}
	d.sayCancel = cancel
	d.sayGen++
	mine := d.sayGen
	d.replyCur = cur
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		if d.sayGen == mine {
			d.sayCancel = nil
		}
		if d.replyCur == cur {
			d.replyCur = nil
		}
		d.mu.Unlock()
	}()
	body, err := d.voice.Speech(ctx, r.text, d.cfg.ReplyVoice)
	if err != nil {
		if ctx.Err() == nil {
			d.logReply(r, "dropped", "speech: "+plainErr(err))
		}
		return
	}
	defer body.Close()
	p, err := audio.Play()
	if err != nil {
		d.logReply(r, "dropped", "speaker: "+err.Error())
		return
	}
	d.mu.Lock()
	if ctx.Err() != nil || d.replyCur != cur {
		d.mu.Unlock()
		p.Stop()
		return
	}
	if !d.quietAfterFetchLocked() { // something began while the speech came: wait for it
		d.replyCur = nil
		d.replyQ = append([]agentReply{r}, d.replyQ...)
		d.mu.Unlock()
		p.Stop()
		return
	}
	d.sayer, d.replyP = p, p
	d.hideGen++
	d.st.State, d.st.Overlay = Speaking, true
	d.st.Heard, d.st.From, d.st.Answer, d.st.Note = "", r.from, r.text, ""
	d.publishLocked()
	d.logReply(r, "spoken", "")
	d.mu.Unlock()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() { // F9 dictation began (it doesn't go through vcd): the reply stops now and plays after it
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-t.C:
				if !voxtypeBusy("") {
					continue
				}
				d.mu.Lock()
				if d.replyCur == cur {
					d.stopReplyLocked()
					d.replyQ = append([]agentReply{r}, d.replyQ...)
				}
				d.mu.Unlock()
				return
			}
		}
	}()

	buf := make([]byte, 4800)
	carry := []byte{}
	for {
		n, err := body.Read(buf)
		if n > 0 {
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
	defer d.mu.Unlock()
	if d.replyCur == cur {
		d.replyCur = nil // heard in full: nothing may stop and replay it now
	}
	if d.sayer == p {
		d.sayer = nil
		if d.st.State == Speaking && d.rec == nil && d.turn == nil {
			d.settleLocked()
		}
	}
}

// quietAfterFetchLocked: nothing but this reply's own fetch (d.sayCancel) is going on.
func (d *Daemon) quietAfterFetchLocked() bool {
	return !d.st.Muted && !d.st.RepliesMuted && d.health.off == "" && !d.health.paused && d.rec == nil &&
		d.turn == nil && d.player == nil && d.sayer == nil && d.st.State == Ready && !voxtypeBusy("") &&
		d.lead == nil && d.batch == nil && d.st.Card == nil
}

// stopReplyLocked stops the reply being fetched or played, if any, and returns it (nil if none).
func (d *Daemon) stopReplyLocked() *agentReply {
	r := d.replyCur
	if r == nil {
		return nil
	}
	d.replyCur = nil
	if d.sayCancel != nil {
		d.sayCancel()
		d.sayCancel = nil
	}
	if d.sayer != nil && d.sayer == d.replyP {
		d.sayer.Stop()
		d.sayer = nil
	}
	d.st.From, d.st.Answer = "", ""
	if d.st.State == Speaking && d.rec == nil && d.turn == nil {
		d.settleLocked()
	}
	return r
}

// pauseReplyLocked: a press stops the reply; it plays again if the press is a hold (after the owner's
// turn), not if it is a tap (stop).
func (d *Daemon) pauseReplyLocked() {
	if r := d.replyCur; r != nil {
		d.replyCur = nil
		d.pausedReply = r
	}
}

// endPauseLocked is the press ending: a hold puts the paused reply back first in line, a tap drops it.
func (d *Daemon) endPauseLocked(tap bool) {
	r := d.pausedReply
	d.pausedReply = nil
	if r == nil {
		return
	}
	if tap || d.st.Muted || d.st.RepliesMuted || d.health.paused {
		d.logReply(*r, "dropped", "stopped")
		return
	}
	d.replyQ = append([]agentReply{*r}, d.replyQ...)
}

// SetReplies mutes (off) or unmutes (on) agent replies, toggles them, or repeats the last one.
func (d *Daemon) SetReplies(arg string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch arg {
	case "off":
		d.setRepliesMutedLocked(true)
	case "on":
		d.setRepliesMutedLocked(false)
	case "toggle", "":
		d.setRepliesMutedLocked(!d.st.RepliesMuted)
	case "repeat":
		if d.lastReply == nil {
			return d.st.RepliesMuted, errors.New("no agent reply yet")
		}
		if d.st.Muted {
			return d.st.RepliesMuted, errors.New("speech is muted")
		}
		if d.st.RepliesMuted {
			return true, errors.New("agent replies are muted: say \"talk to me\" first")
		}
		if d.health.off != "" || d.health.paused {
			return false, errors.New("the voice computer can't speak now")
		}
		r := *d.lastReply
		r.news, r.at = false, time.Now() // asked for: now, not when idle
		keep := d.replyQ[:0]
		for _, q := range d.replyQ { // the same reply still waiting goes: it plays now
			if q.text != r.text || q.from != r.from {
				keep = append(keep, q)
			}
		}
		d.replyQ = keep
		d.replyQ = append([]agentReply{r}, d.replyQ...)
		d.wakeRepliesLocked()
	default:
		return d.st.RepliesMuted, errors.New("replies takes on, off, toggle or repeat")
	}
	d.publishLocked()
	return d.st.RepliesMuted, nil
}

// setRepliesMutedLocked mutes or unmutes agent replies; muting drops those waiting and stops one playing.
func (d *Daemon) setRepliesMutedLocked(muted bool) {
	d.st.RepliesMuted = muted
	if !muted {
		return
	}
	for _, r := range d.replyQ {
		d.logReply(r, "dropped", "replies_muted")
	}
	d.replyQ, d.pausedReply = nil, nil
	d.stopReplyLocked()
}
