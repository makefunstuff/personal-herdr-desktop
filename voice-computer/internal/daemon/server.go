package daemon

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"herdr-voice-computer/internal/audio"
	"herdr-voice-computer/internal/config"
)

// Request is one line from `vc` (docs/vc-socket.md), or one POST /cmd from the Herdr mod.
type Request struct {
	Cmd string `json:"cmd"`
	Arg string `json:"arg"`
	At  int64  `json:"at"` // unix ms when `vc` ran (ptt)
}

// SocketPath is vcd's socket in its state folder.
func SocketPath() string { return filepath.Join(config.StateDir(), "vc.sock") }

// Serve answers requests on path until ctx ends.
func (d *Daemon) Serve(ctx context.Context, path string) error {
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return errors.New("vcd already runs on " + path)
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.Remove(path)
	old := umask(0o077)
	l, err := net.Listen("unix", path)
	umask(old)
	if err != nil {
		return err
	}
	os.Chmod(path, 0o600)
	go func() { <-ctx.Done(); l.Close() }()
	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go d.handle(c)
	}
}

func (d *Daemon) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	c.SetReadDeadline(time.Time{})
	var req Request
	if json.Unmarshal(line, &req) != nil {
		reply(c, map[string]any{"ok": false, "error": "not JSON"})
		return
	}
	if req.Cmd == "watch" {
		ch, stop := d.Watch()
		defer stop()
		go func() { // the client hanging up ends the watch
			r.ReadByte()
			stop()
		}()
		for line := range ch {
			c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := c.Write(append(line, '\n')); err != nil {
				return
			}
		}
		return
	}
	reply(c, d.Dispatch(req))
}

// Dispatch answers one request (every command but watch).
func (d *Daemon) Dispatch(req Request) any {
	at := time.Now()
	if req.At > 0 {
		at = time.UnixMilli(req.At)
	}
	if d.cfg.DictationOnly {
		switch req.Cmd {
		case "ptt", "talk", "turn", "text", "say", "confirm", "decline":
			return map[string]any{"ok": false, "error": "the voice computer runs for dictation only (dictation_only in its config)"}
		}
	}
	switch req.Cmd {
	case "ptt":
		switch req.Arg {
		case "start":
			d.PTTStart(at)
		case "stop":
			d.PTTStop(at)
		default:
			return map[string]any{"ok": false, "error": "ptt takes start or stop"}
		}
		return map[string]any{"ok": true}
	case "dictate": // written down for a text box: start, stop (returns the text) or cancel
		switch req.Arg {
		case "start":
			return okErr(d.DictateStart())
		case "stop":
			text, err := d.DictateStop(context.Background())
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}
			}
			return map[string]any{"ok": true, "text": text}
		case "cancel":
			d.DictateCancel()
			return map[string]any{"ok": true}
		}
		return map[string]any{"ok": false, "error": "dictate takes start, stop or cancel"}
	case "talk": // the mod's button and key: press once to listen, again to send
		listening := d.Talk(at)
		return map[string]any{"ok": true, "listening": listening}
	case "turn":
		return okErr(d.TurnWAV(req.Arg))
	case "text": // a typed turn: as if heard (tests and the mod's input)
		return okErr(d.TurnText(req.Arg))
	case "cancel":
		d.Cancel()
		return map[string]any{"ok": true}
	case "hide":
		d.Hide()
		return map[string]any{"ok": true}
	case "mute":
		muted, err := d.SetMute(req.Arg)
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		return map[string]any{"ok": true, "muted": muted}
	case "quiet", "replies":
		arg := req.Arg
		if req.Cmd == "quiet" { // quiet on = replies off
			arg = map[string]string{"on": "off", "off": "on"}[arg]
			if arg == "" {
				arg = "toggle"
			}
		}
		muted, err := d.SetReplies(arg)
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		d.mu.Lock()
		d.savePrefsLocked()
		d.mu.Unlock()
		return map[string]any{"ok": true, "quiet": muted}
	case "say":
		return okErr(d.Say(req.Arg))
	case "confirm":
		return okErr(d.ConfirmKey(req.Arg))
	case "decline":
		return okErr(d.AnswerCard(req.Arg, "decline"))
	case "words":
		_, err := d.MyWords()
		return okErr(err)
	case "undo":
		return okErr(d.Undo())
	case "status":
		return d.Snapshot()
	case "pomo": // the widgets: {"action", "minutes", "task"}; nothing is said
		var p struct {
			Action  string `json:"action"`
			Minutes int    `json:"minutes"`
			Task    string `json:"task"`
		}
		if json.Unmarshal([]byte(req.Arg), &p) != nil {
			return map[string]any{"ok": false, "error": "pomo takes {\"action\", \"minutes\", \"task\"}"}
		}
		say, in, err := d.Pomo(p.Action, p.Minutes, p.Task)
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error(), "pomo": in}
		}
		return map[string]any{"ok": true, "pomo": in, "text": say}
	case "note":
		line, err := d.AddNote(req.Arg)
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}
		}
		return map[string]any{"ok": true, "line": line}
	}
	return map[string]any{"ok": false, "error": "unknown command " + req.Cmd}
}

func okErr(err error) map[string]any {
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true}
}

func reply(c net.Conn, v any) {
	b, _ := json.Marshal(v)
	c.Write(append(b, '\n'))
}

// ---- the Herdr mod's way in: HTTP on loopback, with a token only the owner can read

// TokenPath is the HTTP token's file (mode 600).
func TokenPath() string { return filepath.Join(config.StateDir(), "token") }

// ServeHTTP answers the mod on 127.0.0.1:port until ctx ends: GET /state?after=<version> (a long poll
// up to 25 s) and POST /cmd {"cmd", "arg"}. Every request carries X-VC-Token: a web page can't read the
// token, and a custom header makes a browser ask first, so nothing but the owner's programs gets in.
func (d *Daemon) ServeHTTP(ctx context.Context, port int) error {
	tok := make([]byte, 16)
	rand.Read(tok)
	token := hex.EncodeToString(tok)
	os.MkdirAll(config.StateDir(), 0o700)
	if err := os.WriteFile(TokenPath(), []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	mux := http.NewServeMux()
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		got := r.Header.Get("X-VC-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, `{"ok":false,"error":"no token"}`, http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("GET /state", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if d.hub.version.Load() <= after {
			ch, stop := d.Watch() // its first line is the state now
			deadline := time.After(25 * time.Second)
		wait:
			for {
				select {
				case <-ch:
					if d.hub.version.Load() > after {
						break wait
					}
				case <-deadline:
					break wait
				case <-r.Context().Done():
					stop()
					return
				}
			}
			stop()
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(d.Snapshot())
	})
	mux.HandleFunc("POST /cmd", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		var req Request
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if json.Unmarshal(b, &req) != nil || req.Cmd == "watch" {
			http.Error(w, `{"ok":false,"error":"not a command"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(d.Dispatch(req))
	})
	srv := &http.Server{Addr: "127.0.0.1:" + strconv.Itoa(port), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); srv.Close() }()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ---- turns without the key

// Talk is the mod's button: the first press opens the mic, the next sends what was heard.
func (d *Daemon) Talk(at time.Time) bool {
	d.mu.Lock()
	held := d.rec != nil
	d.mu.Unlock()
	if held {
		d.PTTStop(at)
		return false
	}
	d.PTTStart(at)
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rec != nil
}

// TurnWAV sends a WAV file (16 kHz mono PCM16) as one turn, as if heard: `vc turn --wav`.
func (d *Daemon) TurnWAV(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pcm, rate, err := wavPCM(b)
	if err != nil {
		return err
	}
	if rate != audio.InRate {
		return errors.New("the WAV must be 16 kHz mono 16-bit (vc turn converts it with sox)")
	}
	return d.turnPCM(pcm, "")
}

// TurnText sends words as one turn, as if heard (no speech recognition).
func (d *Daemon) TurnText(text string) error {
	if text == "" {
		return errors.New("nothing to say")
	}
	return d.turnPCM(nil, text)
}

func (d *Daemon) turnPCM(pcm []byte, text string) error {
	d.mu.Lock()
	if d.health.off != "" {
		why := d.health.why
		d.mu.Unlock()
		return errors.New(why)
	}
	if d.rec != nil {
		d.mu.Unlock()
		return errors.New("the mic is open")
	}
	d.gen++
	gen := d.gen
	d.pauseReplyLocked()
	d.stopOutputLocked()
	d.st.State, d.st.Overlay, d.st.Heard, d.st.From, d.st.Answer, d.st.Note = Thinking, true, "", "", "", ""
	d.st.Done = nil
	d.hideGen++
	d.publishLocked()
	d.mu.Unlock()
	now := time.Now()
	tl := turnLog{Event: "turn"}
	if text != "" {
		go d.sendTurnText(gen, text, now, tl)
	} else {
		go d.sendTurn(gen, pcm, now, tl)
	}
	return nil
}

// wavPCM is a WAV file's mono PCM16 data and rate.
func wavPCM(b []byte) ([]byte, int, error) {
	if len(b) < 44 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a WAV file")
	}
	rate, channels, bits := 0, 0, 0
	for i := 12; i+8 <= len(b); {
		id := string(b[i : i+4])
		size := int(uint32(b[i+4]) | uint32(b[i+5])<<8 | uint32(b[i+6])<<16 | uint32(b[i+7])<<24)
		body := i + 8
		switch id {
		case "fmt ":
			if body+16 > len(b) {
				return nil, 0, errors.New("short fmt")
			}
			channels = int(b[body+2]) | int(b[body+3])<<8
			rate = int(uint32(b[body+4]) | uint32(b[body+5])<<8 | uint32(b[body+6])<<16 | uint32(b[body+7])<<24)
			bits = int(b[body+14]) | int(b[body+15])<<8
		case "data":
			if channels != 1 || bits != 16 {
				return nil, 0, errors.New("the WAV must be mono 16-bit")
			}
			end := min(len(b), body+size)
			if size < 0 || size == 0xffffffff {
				end = len(b)
			}
			return b[body:end], rate, nil
		}
		i = body + size + size%2
	}
	return nil, 0, errors.New("no data in the WAV")
}
