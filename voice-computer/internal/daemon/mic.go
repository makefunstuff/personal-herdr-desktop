package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// LaunchedBy is the app macOS holds responsible for vcd's microphone: the first app bundle among the
// processes that started it (`vc start` puts it in VC_LAUNCHED_BY, since vcd itself runs detached),
// else the program at the top of the chain (a daemon such as herdr-core, which macOS can't ask).
func LaunchedBy() string {
	if v := os.Getenv("VC_LAUNCHED_BY"); v != "" {
		return v
	}
	return Ancestry(os.Getpid())
}

// Ancestry walks pid's parents for an app bundle ("Terminal", "Herdr"); with none, the last program
// before launchd.
func Ancestry(pid int) string {
	top := ""
	for i := 0; i < 20 && pid > 1; i++ {
		out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			break
		}
		f := strings.Fields(strings.TrimSpace(string(out)))
		if len(f) < 2 {
			break
		}
		comm := strings.Join(f[1:], " ")
		if j := strings.Index(comm, ".app/"); j >= 0 {
			name := comm[:j]
			return name[strings.LastIndex(name, "/")+1:]
		}
		top = comm[strings.LastIndex(comm, "/")+1:]
		pid, _ = strconv.Atoi(f[0])
	}
	return top
}

// MicBlockedWhy says why the mic gives silence and what to do.
func MicBlockedWhy() string {
	app := LaunchedBy()
	if app == "" {
		app = "the app that started it"
	}
	if app == "Herdr Voice" {
		return "Microphone blocked: allow Herdr Voice in System Settings → Privacy & Security → Microphone, then vc restart"
	}
	known := map[string]bool{"Terminal": true, "iTerm": true, "iTerm2": true, "Ghostty": true, "WezTerm": true, "kitty": true, "Alacritty": true}
	if known[app] {
		return fmt.Sprintf("Microphone blocked for %s: allow %s in System Settings → Privacy & Security → Microphone, then vc restart", app, app)
	}
	return fmt.Sprintf("Microphone blocked: the voice computer was started from %s, which macOS can't ask for the microphone. "+
		"Build Herdr Voice.app (install/app.sh), run vc restart and click Allow when macOS asks for Herdr Voice", app)
}
