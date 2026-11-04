package latency

import (
	"bytes"
	"strings"
	"testing"
)

const log = `vcd started
{"event":"turn","kind":"brain","listening_ms":3,"mic_ms":22,"asr_ms":110,"session":"new","first_audio_ms":900,"brain_loaded":true,"game":false}
{"event":"turn","kind":"brain","listening_ms":4,"mic_ms":25,"asr_ms":120,"session":"warm","first_audio_ms":600,"brain_loaded":true,"game":false}
{"event":"turn","kind":"brain","listening_ms":5,"mic_ms":20,"asr_ms":100,"session":"warm","first_audio_ms":800,"brain_loaded":true,"game":false}
{"event":"turn","kind":"brain","listening_ms":2,"mic_ms":21,"asr_ms":130,"session":"warm","first_audio_ms":1400,"brain_loaded":false,"game":false}
{"event":"turn","kind":"brain","listening_ms":2,"mic_ms":21,"asr_ms":130,"session":"warm","first_audio_ms":2400,"brain_loaded":true,"game":true}
{"event":"turn","kind":"tap","listening_ms":2,"mic_ms":21,"held_ms":80}
{"event":"stop","stop_ms":12}`

func TestSummary(t *testing.T) {
	rows := Summary(Read(strings.NewReader(log)))
	got := map[string]Row{}
	for _, r := range rows {
		got[r.Moment] = r
	}
	check := func(moment string, n int, p50, p90 int64) {
		t.Helper()
		r := got[moment]
		if r.N != n || r.P50 != p50 || r.P90 != p90 {
			t.Errorf("%s: %+v, want n %d p50 %d p90 %d", moment, r, n, p50, p90)
		}
	}
	check("Mic open after key down", 5, 21, 25) // the tap doesn't count
	check("Transcript ready", 5, 120, 130)
	check("First audio, warm", 2, 600, 800)
	check("First audio, first turn of a new session", 1, 900, 900)
	check("First audio after the brain slept", 1, 1400, 1400)
	check("First audio, slower stand-in", 1, 2400, 2400)
	check("Stop (F7 press while speaking)", 1, 12, 12)
	var b bytes.Buffer
	Print(&b, rows)
	if !strings.Contains(b.String(), "≤ 700 / 1000 ms") {
		t.Errorf("table:\n%s", b.String())
	}
}
