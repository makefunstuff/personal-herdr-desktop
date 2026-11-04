// Package latency sums up vcd's turn log (one JSON line per turn, no words) against its
// latency targets.
package latency

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// Turn is the part of a log line the summary reads.
type Turn struct {
	Event       string `json:"event"`
	Kind        string `json:"kind"`
	ListeningMS int64  `json:"listening_ms"`
	MicMS       int64  `json:"mic_ms"`
	ASRMS       int64  `json:"asr_ms"`
	Session     string `json:"session"`
	FirstAudio  int64  `json:"first_audio_ms"`
	Status      string `json:"status"`
	BrainLoaded *bool  `json:"brain_loaded"`
	Game        bool   `json:"game"`
	Muted       bool   `json:"muted"`
	Capped      bool   `json:"capped"`
	StopMS      int64  `json:"stop_ms"`
}

// Case is which first-audio row a computer answer counts for.
func (t Turn) Case() string {
	switch {
	case t.Game:
		return "slower stand-in (logged, no target)"
	case t.BrainLoaded != nil && !*t.BrainLoaded:
		return "after the brain slept"
	case t.Session == "new":
		return "first turn of a new session"
	default:
		return "warm"
	}
}

// Read takes the turn and stop lines out of a log (other lines are skipped).
func Read(r io.Reader) []Turn {
	var out []Turn
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.IndexByte(line, '{'); i >= 0 {
			line = line[i:]
		}
		var t Turn
		if json.Unmarshal([]byte(line), &t) == nil && (t.Event == "turn" || t.Event == "stop") {
			out = append(out, t)
		}
	}
	return out
}

// Row is one line of the summary.
type Row struct {
	Moment   string
	N        int
	P50, P90 int64
	Target   string
}

// Pct is the nearest-rank percentile of ms (sorted in place).
func Pct(ms []int64, p float64) int64 {
	if len(ms) == 0 {
		return 0
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
	i := int(math.Ceil(p/100*float64(len(ms)))) - 1
	return ms[max(0, i)]
}

func row(moment, target string, ms []int64) Row {
	return Row{Moment: moment, N: len(ms), P50: Pct(ms, 50), P90: Pct(ms, 90), Target: target}
}

// Summary works out each moment's p50 and p90.
func Summary(turns []Turn) []Row {
	var listening, mic, asr, stop []int64
	audio := map[string][]int64{}
	for _, t := range turns {
		if t.Event == "stop" {
			stop = append(stop, t.StopMS)
			continue
		}
		if t.Kind == "tap" {
			continue
		}
		if t.ListeningMS > 0 || t.MicMS > 0 {
			listening = append(listening, t.ListeningMS)
			mic = append(mic, t.MicMS)
		}
		if t.ASRMS > 0 {
			asr = append(asr, t.ASRMS)
		}
		if t.Kind == "brain" && t.FirstAudio > 0 && !t.Capped {
			audio[t.Case()] = append(audio[t.Case()], t.FirstAudio)
		}
	}
	rows := []Row{
		row("Listening sent to the bar after vc ptt start", "≤ 50 / 100 ms", listening),
		row("Mic open after key down", "≤ 100 ms", mic),
		row("Transcript ready", "≤ 200 / 300 ms", asr),
		row("First audio, warm", "≤ 700 / 1000 ms", audio["warm"]),
		row("First audio, first turn of a new session", "≤ 1000 / 1500 ms", audio["first turn of a new session"]),
		row("First audio after the brain slept", "≤ 1500 / 2000 ms", audio["after the brain slept"]),
		row("First audio, slower stand-in", "logged only", audio["slower stand-in (logged, no target)"]),
		row("Stop (F7 press while speaking)", "≤ 150 ms", stop),
	}
	return rows
}

// Print writes the summary as a table.
func Print(w io.Writer, rows []Row) {
	fmt.Fprintf(w, "%-44s %4s %8s %8s  %s\n", "Moment", "n", "p50 ms", "p90 ms", "Target (p50 / p90)")
	for _, r := range rows {
		if r.N == 0 {
			fmt.Fprintf(w, "%-44s %4d %8s %8s  %s\n", r.Moment, 0, "-", "-", r.Target)
			continue
		}
		fmt.Fprintf(w, "%-44s %4d %8d %8d  %s\n", r.Moment, r.N, r.P50, r.P90, r.Target)
	}
}
