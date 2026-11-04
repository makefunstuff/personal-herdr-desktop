package daemon

// Desktop apps through agent-desktop (github.com/vercel-labs/agent-desktop), which reads windows through
// macOS accessibility. Two tools, both without a click or a keystroke: read_app reads an app's front
// window and the brain answers the owner's question from what it shows; open_app launches an app.
// Password managers are never read, and what a window shows is never logged. Herdr Voice needs
// Accessibility (System Settings > Privacy & Security) for read_app.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"herdr-voice-computer/internal/intent"
)

// neverRead: apps whose windows hold secrets.
var neverRead = regexp.MustCompile(`(?i)bitwarden|1password|keepass|keychain|lastpass|dashlane|enpass|proton pass|passwords`)

// uiNoise: the accessibility ids and states a window's tree holds beside its words.
var uiNoise = regexp.MustCompile(`^(true|false|[a-z0-9]+(?:[-_][a-z0-9]+)+|[a-z]+[A-Z][A-Za-z]+)$`)

func desktopBin() (string, error) {
	home, _ := os.UserHomeDir()
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	native := "agent-desktop-" + runtime.GOOS + "-" + arch
	var cands []string
	for _, g := range []string{
		filepath.Join(home, ".local/share/mise/installs/node/*/lib/node_modules/agent-desktop/bin", native),
		filepath.Join("/opt/homebrew/lib/node_modules/agent-desktop/bin", native),
		filepath.Join("/usr/local/lib/node_modules/agent-desktop/bin", native),
	} {
		m, _ := filepath.Glob(g)
		cands = append(cands, m...)
	}
	if p, err := exec.LookPath("agent-desktop"); err == nil {
		cands = append(cands, p)
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("agent-desktop isn't installed (npm install -g agent-desktop)")
}

// runDesktop runs an agent-desktop command; a timed-out one (the app was busy) is tried once more.
func runDesktop(ctx context.Context, args ...string) (json.RawMessage, error) {
	data, err := runDesktopOnce(ctx, args...)
	if err != nil && strings.HasPrefix(err.Error(), "TIMEOUT") {
		data, err = runDesktopOnce(ctx, args...)
	}
	return data, err
}

func runDesktopOnce(ctx context.Context, args ...string) (json.RawMessage, error) {
	bin, err := desktopBin()
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, bin, args...).Output()
	var r struct {
		OK    bool            `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(out, &r) != nil {
		return nil, errors.New("agent-desktop gave no answer")
	}
	if !r.OK {
		if r.Error != nil {
			if r.Error.Code == "PERM_DENIED" || r.Error.Code == "PERMISSION_DENIED" {
				return nil, errors.New("Herdr Voice needs Accessibility in System Settings, Privacy and Security (" + r.Error.Code + ": " + oneLine(r.Error.Message, 160) + ")")
			}
			if r.Error.Code == "TIMEOUT" {
				return nil, errors.New("TIMEOUT: " + r.Error.Message)
			}
			return nil, errors.New(r.Error.Message)
		}
		return nil, errors.New("agent-desktop failed")
	}
	return r.Data, nil
}

// windowText is the words an app's front window shows, in tree order (at most 5000 characters).
func windowText(data json.RawMessage) string {
	var tree any
	_ = json.Unmarshal(data, &tree)
	seen := map[string]bool{}
	var b strings.Builder
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			for _, k := range []string{"title", "name", "value", "text", "description"} {
				if s, ok := v[k].(string); ok {
					s = strings.Join(strings.Fields(s), " ")
					if s != "" && !seen[s] && !uiNoise.MatchString(s) && b.Len() < 5000 {
						seen[s] = true
						b.WriteString(s + "\n")
					}
				}
			}
			for k, c := range v {
				if k != "title" && k != "name" && k != "value" && k != "text" && k != "description" {
					walk(c)
				}
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(tree)
	return b.String()
}

func (d *Daemon) planReadApp(c intent.Call) *action {
	app, question := strings.TrimSpace(c.Str("app")), c.Str("question")
	return tell(func(ctx context.Context) (string, string) {
		if app == "" {
			return "no app named", "Which app should I look at?"
		}
		if neverRead.MatchString(app) {
			return "refused to read a password manager", "I don't read password managers, sorry."
		}
		data, err := runDesktop(ctx, "snapshot", "--app", app)
		if err != nil {
			return "couldn't read " + app + ": " + err.Error(), "I couldn't read " + app + ": " + err.Error() + "."
		}
		text := windowText(data)
		if text == "" {
			return "read " + app + ": no words", app + "'s window shows no text I can read."
		}
		if question == "" {
			question = "What does this window show? Say the main things."
		}
		ans := d.readSource(ctx, question, app+"'s window", app+" (an app window on this Mac)", text)
		if ans == "" {
			ans = "I read " + app + " but couldn't make sense of it."
		}
		return "read the " + app + " window", ans // what it shows stays out of the log
	})
}

func (d *Daemon) planOpenApp(c intent.Call) *action {
	app := strings.TrimSpace(c.Str("app"))
	return runner{func(ctx context.Context, _ string) (string, []Done, error) {
		if app == "" {
			return "", nil, errors.New("no app named")
		}
		if _, err := runDesktop(ctx, "launch", app, "--activate"); err != nil {
			return "", nil, err
		}
		return "Opened " + app + ".", []Done{{Text: "Opened " + app}}, nil
	}}.act("Open "+app, "", "Open")
}

// installedApp is the Mac app a spoken name means ("firefox", "activity monitor"), from the app folders,
// or "" when none is installed by that name.
func installedApp(spoken string) string {
	want := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(spoken)), " ", "")
	if want == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{"/Applications", filepath.Join(home, "Applications"), "/System/Applications", "/System/Applications/Utilities", "/Applications/Utilities"} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".app")
			if ok && strings.ReplaceAll(strings.ToLower(name), " ", "") == want {
				return name
			}
		}
	}
	return ""
}

// asApp turns "open X" / "go to X", when X is no space or agent but an installed app, into open_app.
func asApp(f intent.Fast) (intent.Fast, bool) {
	if f.Kind != "tool" || (f.Call.Name != "focus" && f.Call.Name != "go_to_space") {
		return f, false
	}
	name := f.Call.Str("name")
	if name == "" {
		name = f.Call.Str("space")
	}
	if app := installedApp(name); app != "" {
		return intent.Fast{Kind: "tool", Call: intent.Call{Name: "open_app", Args: map[string]any{"app": app}}}, true
	}
	return f, false
}
