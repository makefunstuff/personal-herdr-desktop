// herdr-voice is the executable of "Herdr Voice.app", the small app bundle macOS asks for the
// microphone. It starts `vc daemon` as its child (so vcd, rec and the models belong to this app for
// macOS's privacy checks) and lives as long as vcd does. It changes rarely, so a permission granted
// to it survives rebuilding vc.
package main

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	home, _ := os.UserHomeDir()
	path := []string{filepath.Join(home, ".local/bin"), "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	os.Setenv("PATH", strings.Join(path, ":")+":"+os.Getenv("PATH"))
	os.Setenv("VC_LAUNCHED_BY", "Herdr Voice")
	vc := filepath.Join(home, ".local/bin/vc")
	if v := os.Getenv("VC_BIN"); v != "" {
		vc = v
	}
	stateDir := filepath.Join(home, ".local/state/herdr-voice-computer")
	os.MkdirAll(stateDir, 0o700)
	logf, err := os.OpenFile(filepath.Join(stateDir, "vcd.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		os.Exit(1)
	}
	cmd := exec.Command(vc, "daemon")
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		logf.WriteString("herdr-voice: " + err.Error() + "\n")
		os.Exit(1)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-sig
		cmd.Process.Signal(s)
	}()
	cmd.Wait()
}
