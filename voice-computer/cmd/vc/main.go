// vc is the voice computer for Herdr: `vc daemon` is vcd, `vc start` / `vc stop` run it in the
// background, and every other command talks to it over its socket.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"herdr-voice-computer/internal/config"
	"herdr-voice-computer/internal/daemon"
	"herdr-voice-computer/internal/desk"
	"herdr-voice-computer/internal/latency"
	"herdr-voice-computer/internal/llama"
	"herdr-voice-computer/internal/pomo"
)

const usage = `vc: the voice computer for Herdr (vcd and its two local LFM3 llama-servers)

  vc start [--wait]           start vcd in the background (it starts the models); --wait: until ready
  vc start --terminal         start it from Terminal.app, so macOS can ask for the microphone
  vc restart [--terminal]     stop, then start
  vc stop                     stop vcd and its models
  vc mic-check                record 1.5 s here and say whether macOS lets this app use the mic
  vc status                   the state as JSON (exit 1 when vcd isn't running)
  vc daemon                   run vcd in the foreground
  vc talk                     the mic button: press once to listen, again to send
  vc ptt start|stop           a held key going down / up
  vc turn --wav F | --text T | --say T
                              one turn from a WAV file, typed words, or words spoken by macOS 'say'
                              (as if heard); prints what was heard, done, said, and the timings
  vc cancel                   stop the speech and the turn
  vc confirm [<card>]         yes to the open confirm
  vc decline [<card>]         no to the open confirm
  vc quiet [on|off|toggle]    announcements off (on) or on
  vc mute [on|off|toggle]     the computer's speech off or on
  vc say <text>               speak a line
  vc watch                    the state, then every change
  vc hide                     hide the overlay
  vc undo                     take back the last volume change
  vc pomo start [min] [--task T] | break [min] | pause | resume | stop | add <min> | status
                              the pomodoro timer (prints its state as JSON)
  vc pomo stats [--json]      pomodoros today, this week, the streak, the last 14 days, top tasks
  vc note add <text>          a note in today's Desk notes file (~/.config/herdr-app/desk-notes/daily)
  vc note toggle <date> <n>   flip the checkbox on line n of that day's file
  vc latency                  p50 / p90 of each moment from vcd's log (timings only, no words)
`

func main() {
	at := time.Now()
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch args[0] {
	case "daemon":
		runDaemon()
	case "start", "restart":
		if args[0] == "restart" {
			stop()
		}
		switch arg(args, "") {
		case "--terminal":
			startInTerminal()
		case "--wait":
			start(true)
		default:
			start(false)
		}
	case "stop":
		stop()
	case "mic-check":
		micCheck()
	case "ptt":
		if len(args) != 2 || (args[1] != "start" && args[1] != "stop") {
			fail("vc ptt start|stop")
		}
		call(daemon.Request{Cmd: "ptt", Arg: args[1], At: at.UnixMilli()})
	case "talk":
		call(daemon.Request{Cmd: "talk", At: at.UnixMilli()})
	case "cancel", "hide", "words", "undo":
		call(daemon.Request{Cmd: args[0]})
	case "confirm", "decline":
		call(daemon.Request{Cmd: args[0], Arg: arg(args, "")})
	case "mute", "quiet", "replies":
		call(daemon.Request{Cmd: args[0], Arg: arg(args, "toggle")})
	case "say":
		call(daemon.Request{Cmd: "say", Arg: strings.Join(args[1:], " ")})
	case "status":
		c, err := net.Dial("unix", daemon.SocketPath())
		if err != nil {
			fmt.Println(`{"type":"state","state":"off","off":"service","why":"vcd isn't running (vc start)"}`)
			os.Exit(1)
		}
		c.Close()
		os.Stdout.Write(request(daemon.Request{Cmd: "status"}))
	case "watch":
		watch()
	case "turn":
		turn(args[1:])
	case "pomo":
		pomoCmd(args[1:])
	case "note":
		noteCmd(args[1:])
	case "latency":
		f, err := os.Open(filepath.Join(config.StateDir(), "vcd.log"))
		if err != nil {
			fail("no log yet: " + err.Error())
		}
		defer f.Close()
		latency.Print(os.Stdout, latency.Summary(latency.Read(f)))
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func arg(args []string, def string) string {
	if len(args) > 1 {
		return args[1]
	}
	return def
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "vc:", msg)
	os.Exit(1)
}

func dial() net.Conn {
	c, err := net.Dial("unix", daemon.SocketPath())
	if err != nil {
		fail("vcd isn't running (vc start)")
	}
	return c
}

// request sends one request and returns the first line back.
func request(req daemon.Request) []byte {
	c := dial()
	defer c.Close()
	b, _ := json.Marshal(req)
	c.Write(append(b, '\n'))
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		fail("no answer from vcd")
	}
	return line
}

// call sends a request and fails on {"ok": false}.
func call(req daemon.Request) {
	var r struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	json.Unmarshal(request(req), &r)
	if !r.OK {
		fail(r.Error)
	}
}

func watch() {
	c := dial()
	defer c.Close()
	c.Write([]byte(`{"cmd":"watch"}` + "\n"))
	io.Copy(os.Stdout, c)
}

func pidPath() string { return filepath.Join(config.StateDir(), "vcd.pid") }

func running() bool {
	c, err := net.Dial("unix", daemon.SocketPath())
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// start runs `vc daemon` detached (its own session, its output in vcd.log), and waits for its socket.
func start(wait bool) {
	if !running() && os.Getenv("VC_NO_APP") == "" {
		if app := appPath(); app != "" {
			// Through the app: macOS holds Herdr Voice responsible for the mic (and asks for it once).
			if out, err := exec.Command("open", "-g", "-j", "-a", app).CombinedOutput(); err != nil {
				fail("open " + app + ": " + string(out))
			}
			for i := 0; i < 100 && !running(); i++ {
				time.Sleep(100 * time.Millisecond)
			}
			if !running() {
				fail("vcd didn't start through " + app + ": see " + filepath.Join(config.StateDir(), "vcd.log"))
			}
			fmt.Println("vcd started through Herdr Voice.app; the models load in the background (vc status)")
		}
	}
	if !running() {
		dir := config.StateDir()
		os.MkdirAll(dir, 0o700)
		logf, err := os.OpenFile(filepath.Join(dir, "vcd.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fail(err.Error())
		}
		self, _ := os.Executable()
		cmd := exec.Command(self, "daemon")
		cmd.Stdout, cmd.Stderr = logf, logf
		cmd.Stdin = nil
		cmd.Env = append(daemonEnv(), "VC_LAUNCHED_BY="+daemon.Ancestry(os.Getpid()))
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			fail(err.Error())
		}
		os.WriteFile(pidPath(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
		cmd.Process.Release()
		for i := 0; i < 50 && !running(); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if !running() {
			fail("vcd didn't start: see " + filepath.Join(dir, "vcd.log"))
		}
		fmt.Println("vcd started; the models load in the background (vc status)")
	} else {
		fmt.Println("vcd is running")
	}
	if wait {
		for i := 0; i < 240; i++ {
			var s daemon.State
			json.Unmarshal(request(daemon.Request{Cmd: "status"}), &s)
			if s.State != daemon.Off {
				fmt.Println("ready")
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		fail("the models didn't come up in 2 minutes: see " + config.StateDir())
	}
}

// stop ends vcd (it stops its llama-servers), then any llama-server a dead vcd left behind.
func stop() {
	b, _ := os.ReadFile(pidPath())
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid > 1 {
		out, _ := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
		if strings.Contains(string(out), " daemon") {
			syscall.Kill(pid, syscall.SIGTERM)
			for i := 0; i < 100 && syscall.Kill(pid, 0) == nil; i++ {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
	os.Remove(pidPath())
	// Any other vcd (one whose pid file was overwritten): every `vc daemon` of this vc.
	self, _ := os.Executable()
	if out, err := exec.Command("pgrep", "-f", "-x", self+" daemon").Output(); err == nil {
		for _, f := range strings.Fields(string(out)) {
			if p, _ := strconv.Atoi(f); p > 1 && p != os.Getpid() {
				syscall.Kill(p, syscall.SIGTERM)
				for i := 0; i < 100 && syscall.Kill(p, 0) == nil; i++ {
					time.Sleep(100 * time.Millisecond)
				}
			}
		}
	}
	for _, n := range []string{"brain", "audio", "embed"} {
		llama.KillStale(filepath.Join(config.StateDir(), n+".pid"))
	}
	fmt.Println("vcd stopped")
}

// daemonEnv is vcd's environment: the caller's, without the terminal it ran in (hc would take vcd for
// that terminal, and an agent would see the owner's words as that terminal's).
func daemonEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "HC_") {
			env = append(env, kv)
		}
	}
	return env
}

func runDaemon() {
	for _, kv := range os.Environ() { // run in a terminal by hand: the same
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "HC_") {
			os.Unsetenv(k)
		}
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)
	cfg, err := config.Load(config.Path())
	if err != nil {
		logger.Fatalf("config: %v", err)
	}
	os.MkdirAll(config.StateDir(), 0o700)
	// One vcd at a time: the lock is held for its whole life (the kernel lets go when it exits).
	lock, err := os.OpenFile(filepath.Join(config.StateDir(), "vcd.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		logger.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		logger.Printf(`{"event":"start","error":"another vcd holds the lock"}`)
		return
	}
	defer lock.Close()
	if running() {
		logger.Printf("vcd already runs on %s", daemon.SocketPath())
		return
	}
	ctx, stopSig := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSig()
	os.MkdirAll(config.StateDir(), 0o700)
	os.WriteFile(pidPath(), []byte(strconv.Itoa(os.Getpid())), 0o600)
	children := llama.Children(cfg)
	done := make(chan struct{}, len(children))
	logf := func(f string, a ...any) { logger.Printf(f, a...) }
	for _, c := range children {
		go func() { c.Run(ctx, logf); done <- struct{}{} }()
	}
	d := daemon.New(cfg, logger)
	d.LoadPrefs(filepath.Join(config.StateDir(), "prefs.json"))
	go d.Run(ctx)
	if cfg.HTTPPort > 0 {
		go func() {
			if err := d.ServeHTTP(ctx, cfg.HTTPPort); err != nil {
				logger.Printf(`{"event":"http","error":%q}`, err.Error())
			}
		}()
	}
	if err := d.Serve(ctx, daemon.SocketPath()); err != nil {
		logger.Print(err)
		stopSig() // its models stop with it
	}
	for range children {
		<-done
	}
	os.Remove(daemon.SocketPath())
	os.Remove(pidPath())
}

// turn sends one turn and follows the state until it settles, then prints a summary.
func turn(args []string) {
	if len(args) < 2 {
		fail("vc turn --wav FILE | --text WORDS | --say WORDS")
	}
	what := strings.Join(args[1:], " ")
	var req daemon.Request
	switch args[0] {
	case "--text":
		req = daemon.Request{Cmd: "text", Arg: what}
	case "--wav", "--say":
		dir, _ := os.MkdirTemp("", "vc-turn")
		defer os.RemoveAll(dir)
		src := what
		if args[0] == "--say" {
			src = filepath.Join(dir, "say.aiff")
			if out, err := exec.Command("say", "-o", src, what).CombinedOutput(); err != nil {
				fail("say: " + string(out))
			}
		}
		wav := filepath.Join(dir, "turn.wav")
		if out, err := exec.Command("sox", src, "-r", "16000", "-c", "1", "-b", "16", "-e", "signed-integer", wav).CombinedOutput(); err != nil {
			fail("sox: " + string(out))
		}
		req = daemon.Request{Cmd: "turn", Arg: wav}
	default:
		fail("vc turn --wav FILE | --text WORDS | --say WORDS")
	}
	c := dial()
	defer c.Close()
	c.Write([]byte(`{"cmd":"watch"}` + "\n"))
	rd := bufio.NewReader(c)
	var s daemon.State
	readState := func() bool {
		for {
			line, err := rd.ReadBytes('\n')
			if err != nil {
				return false
			}
			var probe struct {
				Type string `json:"type"`
			}
			json.Unmarshal(line, &probe)
			if probe.Type == "state" {
				s = daemon.State{}
				json.Unmarshal(line, &s)
				return true
			}
		}
	}
	readState()
	startVersion := s.Version
	call(req)
	t0 := time.Now()
	lines := make(chan daemon.State, 64)
	go func() {
		for readState() {
			lines <- s
		}
		close(lines)
	}()
	busy := true
	quietSince := time.Now()
	var last daemon.State
	for time.Since(t0) < 90*time.Second {
		select {
		case st, ok := <-lines:
			if !ok {
				fail("vcd went away")
			}
			if st.Version <= startVersion {
				continue
			}
			last = st
			busy = st.State == daemon.Thinking || st.State == daemon.Speaking || st.State == daemon.Listening
			quietSince = time.Now()
		case <-time.After(200 * time.Millisecond):
		}
		if last.Version > 0 && !busy && time.Since(quietSince) > 1500*time.Millisecond {
			break
		}
	}
	out := map[string]any{"heard": last.Heard, "answer": last.Answer, "note": last.Note, "done": last.Done,
		"card": last.Card, "state": last.State, "timings": last.Last, "wall_ms": time.Since(t0).Milliseconds()}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}

// appPath is Herdr Voice.app, when install/app.sh made it.
func appPath() string {
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, "Applications", "Herdr Voice.app")
	if _, err := os.Stat(filepath.Join(p, "Contents", "MacOS", "herdr-voice")); err != nil {
		return ""
	}
	return p
}

// startInTerminal runs `vc mic-check` and `vc start` in a Terminal window: vcd then belongs to Terminal,
// which macOS asks for the microphone (once), where Herdr's own terminals can't be asked.
func startInTerminal() {
	self, _ := os.Executable()
	dir := config.StateDir()
	os.MkdirAll(dir, 0o700)
	script := filepath.Join(dir, "start-in-terminal.command")
	body := fmt.Sprintf("#!/bin/sh\n# Written by vc start --terminal.\nunset HC_TERMINAL_ID HC_SCOPE HC_TERMINAL_SOCKET\n%q mic-check\n%q start --wait\necho\necho 'You can close this window: the voice computer keeps running.'\n", self, self)
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		fail(err.Error())
	}
	if out, err := exec.Command("open", "-a", "Terminal", script).CombinedOutput(); err != nil {
		fail("open Terminal: " + string(out))
	}
	fmt.Println("starting in a Terminal window: allow the microphone there if macOS asks")
}

// micCheck records 1.5 s from the default input with rec and says whether it heard anything at all.
func micCheck() {
	app := daemon.Ancestry(os.Getpid())
	fmt.Printf("recording 1.5 s as %s…\n", app)
	out, err := exec.Command("rec", "-q", "-t", "raw", "-r", "16000", "-e", "signed-integer", "-b", "16", "-c", "1", "-", "trim", "0", "1.5").Output()
	if err != nil {
		fail("rec: " + err.Error())
	}
	peak := 0
	for i := 0; i+1 < len(out); i += 2 {
		v := int(int16(uint16(out[i]) | uint16(out[i+1])<<8))
		if v < 0 {
			v = -v
		}
		peak = max(peak, v)
	}
	if peak <= 2 {
		fmt.Printf("the mic gives digital silence (peak %d of 32768): macOS blocks it for %s\n", peak, app)
		fmt.Println("allow it in System Settings → Privacy & Security → Microphone, or run: vc restart --terminal")
		os.Exit(1)
	}
	fmt.Printf("the mic works (peak %d of 32768)\n", peak)
}

func pomoCmd(args []string) {
	if len(args) == 0 {
		fail("vc pomo start|break|pause|resume|stop|add|status|stats")
	}
	if args[0] == "stats" {
		st := pomo.ComputeStats(pomo.ReadLog(filepath.Join(config.DataDir(), "pomodoro.jsonl")), time.Now())
		if len(args) > 1 && args[1] == "--json" {
			b, _ := json.Marshal(st)
			fmt.Println(string(b))
			return
		}
		fmt.Printf("today %d (%d min), this week %d (%d min), streak %d days\n", st.Today.Count, st.Today.FocusMin,
			st.Week.Count, st.Week.FocusMin, st.Streak)
		return
	}
	p := map[string]any{"action": args[0]}
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--task" && i+1 < len(args):
			p["task"] = strings.Join(args[i+1:], " ")
			i = len(args)
		default:
			if n, err := strconv.Atoi(args[i]); err == nil {
				p["minutes"] = n
			}
		}
	}
	b, _ := json.Marshal(p)
	line := request(daemon.Request{Cmd: "pomo", Arg: string(b)})
	var r struct {
		OK    bool            `json:"ok"`
		Error string          `json:"error"`
		Pomo  json.RawMessage `json:"pomo"`
	}
	json.Unmarshal(line, &r)
	fmt.Println(string(r.Pomo))
	if !r.OK {
		fail(r.Error)
	}
}

func noteCmd(args []string) {
	switch {
	case len(args) >= 2 && args[0] == "add":
		text := strings.Join(args[1:], " ")
		if running() {
			var r struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
				Line  string `json:"line"`
			}
			json.Unmarshal(request(daemon.Request{Cmd: "note", Arg: text}), &r)
			if !r.OK {
				fail(r.Error)
			}
			fmt.Println(r.Line)
			return
		}
		cfg, _ := config.Load(config.Path())
		line, err := desk.New(cfg.NotesDir).Note(time.Now(), text)
		if err != nil {
			fail(err.Error())
		}
		fmt.Println(line)
	case len(args) == 3 && args[0] == "toggle":
		n, err := strconv.Atoi(args[2])
		if err != nil {
			fail("vc note toggle <YYYY-MM-DD> <line number>")
		}
		cfg, _ := config.Load(config.Path())
		line, err := desk.New(cfg.NotesDir).Toggle(args[1], n)
		if err != nil {
			fail(err.Error())
		}
		fmt.Println(line)
	default:
		fail("vc note add <text> | vc note toggle <YYYY-MM-DD> <line number>")
	}
}
