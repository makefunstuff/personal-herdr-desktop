package audio

import (
	"os/exec"
	"testing"
	"time"
)

func TestRecordReadsTheMic(t *testing.T) {
	old := RecordCmd
	defer func() { RecordCmd = old }()
	RecordCmd = func() *exec.Cmd {
		return exec.Command("sh", "-c", `while :; do head -c 3200 /dev/zero | tr "\\0" "@"; sleep 0.1; done`)
	}
	r, err := Record(10*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		r.mu.Lock()
		n := len(r.pcm)
		r.mu.Unlock()
		if n >= 6400 {
			break
		}
	}
	if pcm := r.Stop(); len(pcm) < 6400 || r.OpenedAt().IsZero() {
		t.Fatalf("%d bytes", len(pcm))
	}
}

func TestBlockedIsDigitalSilence(t *testing.T) {
	zero := make([]byte, InRate) // 0.5 s of zeros
	if !Blocked(zero) {
		t.Fatal("zeros aren't blocked")
	}
	quiet := make([]byte, InRate)
	quiet[1000] = 9 // a room's faint noise
	if Blocked(quiet) || Blocked(make([]byte, 100)) {
		t.Fatal("noise or a short clip taken for a blocked mic")
	}
}
