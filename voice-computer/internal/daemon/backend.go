package daemon

import (
	"context"
	"errors"

	"herdr-voice-computer/internal/config"
	"herdr-voice-computer/internal/voice"
)

// ext is a backend's own checks on top of the generic ones (a realtime server that can pause the
// voice computer, or answer with a slower stand-in). Nil for the openai and realtime backends.
type ext interface {
	// health is the whole check: full on the 30 s tick (and before a turn), else the 5 s one. keep:
	// the pause wasn't read this time (unit: but a quick sign says it is on).
	health(ctx context.Context, full bool) (h healthInfo, keep, unit bool)
	// preTurn runs before a turn is transcribed: whether the brain is loaded, and whether to pause.
	preTurn(ctx context.Context) (loaded *bool, pause bool, err error)
	// resumed: a press while paused checks again, so a press right after the pause ends works.
	resumed(ctx context.Context) bool
	// allowed: the session's brain may answer (else vcd pauses); slow: it is a slower stand-in.
	allowed(model string) bool
	slow(model string) bool
}

// exts makes a backend's ext, by its config name.
var exts = map[string]func(cfg config.Config, c *voice.Client) ext{}

// The notes for a paused or slowed backend (a backend's ext sets its own wording).
var (
	pausedNote = "The backend is busy: the voice computer waits"
	pausedWhy  = "The backend is busy: the voice computer waits"
	slowNote   = "My answers are slower for now"
	slowWhy    = "The backend answers more slowly for now"
	keyHint    = ""
)

func (d *Daemon) allowed(model string) bool { return d.ext == nil || d.ext.allowed(model) }
func (d *Daemon) slow(model string) bool    { return d.ext != nil && d.ext.slow(model) }

// genericHealth checks the key files and that each endpoint answers. Once all is well, only the 30 s
// tick pings them again (skip: nothing read now).
func (d *Daemon) genericHealth(ctx context.Context, full bool) (h healthInfo, skip bool) {
	d.mu.Lock()
	ok := d.health.off == "" && d.health.dict == ""
	d.mu.Unlock()
	h.dict, h.dictWhy = d.sttHealth(ctx, full || !ok)
	for _, k := range d.cfg.KeyFiles() {
		if _, err := voice.ReadKey(k); err != nil {
			h.off, h.why = OffKey, "No API key: "+k+keyHint
			return h, false
		}
	}
	if ok && !full {
		return h, true
	}
	type named struct {
		name string
		ep   config.Endpoint
	}
	eps := []named{{"speech to text", d.cfg.STT}, {"text to speech", d.cfg.TTS}, {"chat", d.cfg.Chat}}
	for _, e := range eps {
		if e.name == "speech to text" && h.dict != "" {
			h.off, h.why = h.dict, h.dictWhy
			return h, false
		}
		if e.name == "speech to text" {
			continue // just checked
		}
		err := d.voice.Ping(ctx, e.ep)
		if err != nil && e.name == "chat" && d.cfg.ChatFallback.URL != "" && d.cfg.ChatFallback.URL != d.cfg.Chat.URL {
			continue // a backup brain answers (started on demand when it's the local one)
		}
		if errors.Is(err, voice.ErrRefused) {
			h.off, h.why = OffRefused, "The "+e.name+" endpoint refused the key"
			return h, false
		} else if err != nil {
			h.off, h.why = OffService, "The "+e.name+" model isn't up yet at "+e.ep.URL
			return h, false
		}
	}
	return h, false
}

// sttHealth is the STT endpoint's state alone (dictation uses it even while another endpoint is down):
// its key, then, when ping, whether it answers. Without ping it keeps what the last check found.
func (d *Daemon) sttHealth(ctx context.Context, ping bool) (string, string) {
	if _, err := voice.ReadKey(d.cfg.STT.KeyFile); err != nil {
		return OffKey, "No API key: " + d.cfg.STT.KeyFile + keyHint
	}
	if !ping {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.health.dict, d.health.dictWhy
	}
	if err := d.voice.Ping(ctx, d.cfg.STT); errors.Is(err, voice.ErrRefused) {
		return OffRefused, "The speech to text endpoint refused the key"
	} else if err != nil {
		return OffService, "The speech to text endpoint can't be reached at " + d.cfg.STT.URL
	}
	return "", ""
}
