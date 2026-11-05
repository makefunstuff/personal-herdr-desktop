package daemon

// computer_task: anything to do in a Mac app or on a website that takes clicking or typing ("open the
// calculator and work out 37 times 41", "in Telegram, mute the work chat"). Nothing app-specific lives
// here: the owner's words go, as said, to Claude Code run headless, allowed only agent-desktop (apps,
// through accessibility) and agent-browser (websites). It reads agent-desktop's own guide and works the
// app's real controls. It always asks first, runs in the background, and its answer is said when done.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"herdr-voice-computer/internal/intent"
	"herdr-voice-computer/internal/speech"
)

const computerPrompt = `You act on this Mac for its owner, who asked by voice. Operate apps only through the ` +
	`agent-desktop CLI (run "agent-desktop skills get desktop" first and follow it: snapshot, act on refs, verify) ` +
	`and websites only through agent-browser. Never use any other tool, never touch password managers, and never ` +
	`send, buy, delete or post anything the owner didn't ask for in so many words. When done, end with one or two ` +
	`short plain sentences for the owner to hear: what you did and what the app or page shows. No markdown.`

// claudeBin finds Claude Code (the app's PATH is short).
func claudeBin() (string, error) {
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(home, ".local/bin/claude"), "/opt/homebrew/bin/claude", "/usr/local/bin/claude"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("Claude Code isn't installed")
}

// toolPath is a PATH where agent-desktop and agent-browser are found.
func toolPath() string {
	var dirs []string
	for _, b := range []func() (string, error){desktopBin, browserBin} {
		if p, err := b(); err == nil {
			dirs = append(dirs, filepath.Dir(p))
		}
	}
	home, _ := os.UserHomeDir()
	dirs = append(dirs, filepath.Join(home, ".local/bin"), "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin")
	return strings.Join(dirs, ":")
}

// computerEnv is this process's environment without a parent Claude Code's marks, with toolPath.
func computerEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CLAUDECODE=") || strings.HasPrefix(kv, "CLAUDE_CODE_ENTRYPOINT=") || strings.HasPrefix(kv, "PATH=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "PATH="+toolPath())
}

// agent-desktop names its command line tools "agent-desktop"; the native binary has the platform in its
// name, so Claude Code gets a folder where the plain names work.
func toolShims() (string, error) {
	dir := filepath.Join(os.TempDir(), "herdr-voice-tools")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	for name, find := range map[string]func() (string, error){"agent-desktop": desktopBin, "agent-browser": browserBin} {
		p, err := find()
		if err != nil {
			continue
		}
		link := filepath.Join(dir, name)
		_ = os.Remove(link)
		if err := os.Symlink(p, link); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func (d *Daemon) planComputerTask(c intent.Call) *action {
	task := strings.TrimSpace(c.Str("task"))
	if task == "" {
		d.mu.Lock()
		task = d.st.Heard
		d.mu.Unlock()
	}
	run := func(ctx context.Context, _ string) (string, []Done, error) {
		if task == "" {
			return "", nil, errors.New("nothing to do")
		}
		if neverRead.MatchString(task) {
			return "", nil, errors.New("password managers are off limits")
		}
		bin, err := claudeBin()
		if err != nil {
			return "", nil, err
		}
		go d.runComputerTask(bin, task)
		return "On it. I'll tell you when it's done.", []Done{{Text: "Working on: " + oneLine(task, 80)}}, nil
	}
	a := runner{run}.act("Do on your Mac: "+oneLine(task, 90), task, "Do it")
	a.declined = "Okay, I'll leave it."
	return a
}

// runComputerTask runs Claude Code on the task (at most five minutes) and says its answer.
func (d *Daemon) runComputerTask(bin, task string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	t0 := time.Now()
	shims, _ := toolShims()
	cmd := exec.CommandContext(ctx, bin, "-p", task, "--model", "sonnet", "--output-format", "text",
		"--allowedTools", "Bash(agent-desktop:*)", "Bash(agent-browser:*)", "--append-system-prompt", computerPrompt)
	cmd.Env = computerEnv()
	if shims != "" {
		for i, kv := range cmd.Env {
			if strings.HasPrefix(kv, "PATH=") {
				cmd.Env[i] = "PATH=" + shims + ":" + strings.TrimPrefix(kv, "PATH=")
			}
		}
	}
	home, _ := os.UserHomeDir()
	cmd.Dir = home
	cmd.Stdin = nil
	out, err := cmd.Output()
	answer := speech.Plain(strings.TrimSpace(string(out)))
	lb, _ := json.Marshal(map[string]any{"event": "computer_task", "ok": err == nil && answer != "", "s": int(time.Since(t0).Seconds())})
	d.log.Println(string(lb))
	switch {
	case ctx.Err() != nil:
		answer = "That task took too long, so I stopped it."
	case err != nil || answer == "":
		answer = "That task didn't work out, sorry."
	default:
		answer = lastSentences(answer, 2)
	}
	d.notify(answer)
	_ = d.Say(d.flavor(answer, 6*time.Second))
}

// lastSentences keeps the end of an agent's report: its conclusion.
func lastSentences(s string, n int) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' })
	s = strings.Join(parts, " ")
	var ends []int
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' {
			ends = append(ends, i)
		}
	}
	if len(ends) <= n {
		return s
	}
	return strings.TrimSpace(s[ends[len(ends)-n-1]+1:])
}
