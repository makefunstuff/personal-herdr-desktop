// Package llama runs vcd's two llama-servers (the LFM3 brain, and the LFM3 audio model for speech in
// and out) as its children on loopback, and stops them with it. Every llama process gets
// GGML_METAL_BF16_DISABLE=1: Homebrew's Metal backend can't build its bf16 kernels on this macOS.
package llama

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"herdr-voice-computer/internal/config"
)

// Child is one supervised llama-server.
type Child struct {
	Name string
	Port int
	args []string
	bin  string
	log  string
	pid  string

	mu  sync.Mutex
	cmd *exec.Cmd
}

// Children are the servers cfg asks for: one per configured model (a server with no model is one the
// owner runs; vcd only uses its endpoint).
func Children(cfg config.Config) []*Child {
	var out []*Child
	add := func(name string, s config.Server, alias string) {
		if s.Model == "" || s.Port == 0 {
			return
		}
		args := []string{"-m", s.Model, "--host", "127.0.0.1", "--port", strconv.Itoa(s.Port), "--no-webui", "-a", alias}
		if s.MMProj != "" {
			args = append(args, "--mmproj", s.MMProj)
		}
		args = append(args, s.Args...)
		dir := config.StateDir()
		out = append(out, &Child{Name: name, Port: s.Port, args: args, bin: cfg.LlamaServe,
			log: filepath.Join(dir, name+".log"), pid: filepath.Join(dir, name+".pid")})
	}
	add("brain", cfg.Brain, cfg.Chat.Model)
	add("audio", cfg.Audio, cfg.STT.Model)
	add("embed", cfg.EmbedSrv, cfg.Embed.Model)
	return out
}

// Up says whether something answers /health on the child's port.
func (c *Child) Up() bool {
	cl := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := cl.Get(fmt.Sprintf("http://127.0.0.1:%d/health", c.Port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// Run keeps the child running until ctx ends (restarting it after a crash, at most once every 10 s),
// then stops it. A server already on its port (one left from before, or the owner's) is used as it is.
func (c *Child) Run(ctx context.Context, logf func(string, ...any)) {
	KillStale(c.pid)
	for ctx.Err() == nil {
		if c.Up() {
			logf(`{"event":"llama","name":%q,"what":"already up","port":%d}`, c.Name, c.Port)
		} else if err := c.start(); err != nil {
			logf(`{"event":"llama","name":%q,"what":"start failed","error":%q}`, c.Name, err.Error())
		} else {
			t0 := time.Now()
			logf(`{"event":"llama","name":%q,"what":"started","pid":%d}`, c.Name, c.cmd.Process.Pid)
			done := make(chan error, 1)
			go func() { done <- c.cmd.Wait() }()
			select {
			case <-ctx.Done():
				c.stop(done)
				return
			case err := <-done:
				os.Remove(c.pid)
				logf(`{"event":"llama","name":%q,"what":"exited","error":%q,"after_s":%d}`, c.Name, fmt.Sprint(err), int(time.Since(t0).Seconds()))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (c *Child) start() error {
	os.MkdirAll(filepath.Dir(c.log), 0o700)
	f, err := os.OpenFile(c.log, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(c.bin, c.args...)
	cmd.Env = append(os.Environ(), "GGML_METAL_BF16_DISABLE=1")
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	c.mu.Lock()
	c.cmd = cmd
	c.mu.Unlock()
	os.WriteFile(c.pid, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	return nil
}

func (c *Child) stop(done chan error) {
	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
	os.Remove(c.pid)
}

// KillStale stops a llama-server a pid file names (a vcd that died left it), if it still is one.
func KillStale(pidFile string) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	defer os.Remove(pidFile)
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || !strings.Contains(string(out), "llama-server") {
		return
	}
	syscall.Kill(-pid, syscall.SIGTERM)
	for i := 0; i < 50; i++ {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	syscall.Kill(-pid, syscall.SIGKILL)
}

// Lazy runs a child only while it's wanted: Ensure starts it (and waits for its /health), and it stops
// after idle without an Ensure. The backup brain behind a cloud one: no memory held until it's needed.
type Lazy struct {
	Child *Child
	Idle  time.Duration
	mu    sync.Mutex
	stop  context.CancelFunc
	last  time.Time
}

// Ensure has the child up (starting it under parent if it isn't), at most wait for it to answer.
func (l *Lazy) Ensure(parent context.Context, wait time.Duration, logf func(string, ...any)) error {
	l.mu.Lock()
	l.last = time.Now()
	if l.stop == nil {
		ctx, cancel := context.WithCancel(parent)
		l.stop = cancel
		go l.Child.Run(ctx, logf)
		go l.reap(ctx)
	}
	l.mu.Unlock()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if l.Child.Ready() {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("%s didn't come up in %s", l.Child.Name, wait)
}

// reap stops the child after Idle without an Ensure.
func (l *Lazy) reap(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.mu.Lock()
			if l.stop != nil && time.Since(l.last) > l.Idle {
				l.stop()
				l.stop = nil
				l.mu.Unlock()
				return
			}
			l.mu.Unlock()
		}
	}
}

// Ready says whether the child's model is loaded: /health answers 200 (it says 503 while loading).
func (c *Child) Ready() bool {
	cl := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := cl.Get(fmt.Sprintf("http://127.0.0.1:%d/health", c.Port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
