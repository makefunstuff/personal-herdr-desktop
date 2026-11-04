package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

func tone(n int, amp float64) []byte {
	b := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[2*i:], uint16(int16(amp*math.Sin(float64(i)*0.3))))
	}
	return b
}

func TestSilent(t *testing.T) {
	quiet := tone(InRate, 35)    // just under the gate (-62.4 dBFS RMS); the desk's empty input is -68.4
	voice := tone(InRate/50, 38) // one 20 ms syllable just over it (-61.7 dBFS RMS), after a second of quiet
	if !Silent(nil) || !Silent(make([]byte, 2*InRate)) || !Silent(quiet) {
		t.Fatal("silence or the empty input's noise is not silent")
	}
	if Silent(append(quiet, voice...)) {
		t.Fatal("a quiet syllable is taken for silence")
	}
}
