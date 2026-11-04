package daemon

import (
	"encoding/json"
	"herdr-voice-computer/internal/pomo"
	"sync"
	"sync/atomic"
)

// State is what the Herdr mod shows: the mic button, the overlay and the inbox (docs/vc-socket.md).
type State struct {
	Type         string `json:"type"` // "state"
	State        string `json:"state"`
	Off          string `json:"off"`
	Why          string `json:"why"`
	Game         bool   `json:"game"`
	Voice        bool   `json:"voice_mode"`
	Brain        string `json:"brain"`
	Muted        bool   `json:"muted"`
	RepliesMuted bool   `json:"replies_muted"` // agent replies are not spoken
	Overlay      bool   `json:"overlay"`
	Heard        string `json:"heard"`
	From         string `json:"from"` // the space whose agent reply is shown (and spoken) as the answer
	Answer       string `json:"answer"`
	Note         string `json:"note"`
	Turns        []Turn `json:"turns"`
	// What this turn did (tier 0, or a confirmed card), oldest first.
	Done []Done `json:"done"`
	// The open confirm card, if any.
	Card *Card `json:"card"`
	// Candidates the brain is asking about, with what vcd reads out for them (window titles, file
	// names: never given to the brain).
	Choices []Choice `json:"choices"`
	// The talk and confirm keys, as the config names them.
	TalkKey    string `json:"talk_key"`
	ConfirmKey string `json:"confirm_key"`
	// The secretary's name.
	Name string `json:"name"`
	// What waits on the owner (blocked agents, Shepherdr questions), newest reading.
	Inbox []InboxItem `json:"inbox"`
	// The reminders set (in memory only).
	Reminders []ReminderInfo `json:"reminders"`
	// The latest toast for the mod to post (Seq grows with each).
	Toast *Toast `json:"toast"`
	// The last turn's timings (never words), for `vc turn`.
	Last *turnLog `json:"last"`
	// The pomodoro timer, and what answers web searches.
	Pomo   pomo.Info  `json:"pomo"`
	Search SearchInfo `json:"search"`
	// Version grows with every state line (the mod's long poll waits on it).
	Version int64 `json:"version"`
}

// InboxItem is one thing waiting on the owner.
type InboxItem struct {
	Who      string `json:"who"`  // the agent or space, as said
	What     string `json:"what"` // what it asks ("" when it just waits)
	Terminal string `json:"terminal,omitempty"`
	Source   string `json:"source"` // herdr, or the Shepherdr's name
}

// ReminderInfo is one reminder.
type ReminderInfo struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	Due  int64  `json:"due"` // unix ms
}

// Toast is a notification for the mod to post in Herdr.
type Toast struct {
	Seq   int    `json:"seq"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Level string `json:"level"`
}

// Done is one thing a turn did. Undo is set when `vc undo` can take it back (volume).
type Done struct {
	Text string `json:"text"`
	Undo bool   `json:"undo"`
}

// Card is a confirm card: what will happen, in full, and how it may be answered.
type Card struct {
	ID      string `json:"id"`
	Intent  string `json:"intent"`
	Tier    int    `json:"tier"`
	Title   string `json:"title"`    // "Send to homelab"
	Ask     string `json:"ask"`      // the question said out loud
	Detail  string `json:"detail"`   // "Now on workspace 1"
	YouSaid string `json:"you_said"` // the transcript, when the card carries text (Text)
	Text    string `json:"text"`     // the text the action will use (a note): "send my words" swaps in YouSaid
	Confirm string `json:"confirm"`  // the confirm button's label
	Voice   bool   `json:"voice"`    // a spoken yes answers it (tier 1)
	// Untrusted names where text an app or agent wrote came from, when it raised the tier.
	Untrusted string `json:"untrusted"`
	Expires   int64  `json:"expires"` // unix ms: it cancels itself then
	Of        string `json:"of"`      // "1 of 2" when a turn has more than one card
}

// Choice is a candidate the brain asks about: its neutral label and what the owner sees.
type Choice struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

type Turn struct {
	You      string `json:"you"`
	Computer string `json:"computer"`
	At       int64  `json:"at"`
}

const (
	Off       = "off"
	Ready     = "ready"
	Listening = "listening"
	Thinking  = "thinking"
	Speaking  = "speaking"
)

// Off reasons.
const (
	OffSenses  = "senses"
	OffService = "service"
	OffKey     = "key"
	OffRefused = "refused"
	OffMic     = "mic" // macOS blocks the microphone for the app that started vcd
)

// hub fans state and level lines out to every `vc watch`.
type hub struct {
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	version atomic.Int64
}

func (h *hub) subscribe() chan []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs == nil {
		h.subs = map[chan []byte]struct{}{}
	}
	ch := make(chan []byte, 256)
	h.subs[ch] = struct{}{}
	return ch
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}

// send gives line to every watcher. A watcher too slow for a state line is dropped (it reconnects
// and gets the state afresh); a level line it can't take is just skipped.
func (h *hub) send(line []byte, droppable bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- line:
		default:
			if !droppable {
				delete(h.subs, ch)
				close(ch)
			}
		}
	}
}

func levelLine(l float64) []byte {
	b, _ := json.Marshal(map[string]any{"type": "level", "level": float64(int(l*100)) / 100})
	return b
}
