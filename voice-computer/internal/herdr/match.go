package herdr

import (
	"sort"
	"strings"
)

// asrFixes are how speech recognition spells Herdr's words.
var asrFixes = map[string]string{
	"shepherder": "shepherdr", "shepherd": "shepherdr", "shepard": "shepherdr", "shepherds": "shepherdr",
	"sheppard": "shepherdr", "shepherdrs": "shepherdr", "shepardr": "shepherdr", "sheperd": "shepherdr",
	"herder": "herdr", "herd": "herdr", "hurdur": "herdr", "herta": "herdr",
	"neovim": "nvim", "vim": "nvim", "codecs": "codex", "codec": "codex", "clod": "claude",
	"claud": "claude",
}

// fillers are words a spoken name carries that no name has.
var fillers = map[string]bool{"the": true, "a": true, "an": true, "my": true, "agent": true, "agents": true,
	"space": true, "tab": true, "terminal": true, "please": true, "one": true, "session": true, "window": true,
	"to": true, "on": true, "in": true, "of": true, "s": true, "thing": true}

// Words is a name as plain lowercase words: dashes, dots and case changes split it, ASR spellings fixed.
func Words(s string) []string {
	var b strings.Builder
	prev := rune(0)
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			if prev >= 'a' && prev <= 'z' {
				b.WriteRune(' ')
			}
			b.WriteRune(r + 32)
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '\'' || r == '’':
		default:
			b.WriteRune(' ')
		}
		prev = r
	}
	var out []string
	for _, w := range strings.Fields(b.String()) {
		if f, ok := asrFixes[w]; ok {
			w = f
		}
		out = append(out, w)
	}
	// "shepherd r" / "herd r" said as two words
	var joined []string
	for i := 0; i < len(out); i++ {
		if i+1 < len(out) && out[i+1] == "r" && (out[i] == "shepherdr" || out[i] == "herdr") {
			joined = append(joined, out[i])
			i++
			continue
		}
		joined = append(joined, out[i])
	}
	return joined
}

// queryWords is a spoken name without its fillers.
func queryWords(q string) []string {
	var out []string
	for _, w := range Words(q) {
		if !fillers[w] {
			out = append(out, w)
		}
	}
	return out
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// wordScore is how well a spoken word matches a name's word.
func wordScore(q, c string) float64 {
	switch {
	case q == c:
		return 1
	case len(q) >= 3 && strings.HasPrefix(c, q):
		return 0.85
	case len(q) >= 4 && editDistance(q, c) <= 1:
		return 0.8
	case len(q) >= 3 && len(c) == len(q)+1 && strings.HasSuffix(c, q): // "rep" heard for "grep"
		return 0.7
	case len(c) >= 4 && strings.HasPrefix(q, c): // "stretching" for "stretch"
		return 0.8
	case len(q) >= 7 && editDistance(q, c) <= 2:
		return 0.65
	}
	return 0
}

// Score is how well spoken words name something with these words, 0 to 1: every spoken word must
// match one of its words; a name said as one word ("semanticgrep") matches too.
func Score(spoken []string, name []string) float64 {
	if len(spoken) == 0 || len(name) == 0 {
		return 0
	}
	if strings.Join(spoken, "") == strings.Join(name, "") {
		return 1
	}
	total := 0.0
	for _, q := range spoken {
		best := 0.0
		for _, c := range name {
			best = max(best, wordScore(q, c))
		}
		if best == 0 {
			// several spoken words may make one of the name's ("semantic grep" for "semanticgrep")
			return joinedScore(spoken, name)
		}
		total += best
	}
	s := total / float64(len(spoken))
	// A name with words nobody said is a little less likely.
	cover := float64(len(spoken)) / float64(max(len(spoken), len(name)))
	return s * (0.9 + 0.1*cover)
}

func joinedScore(spoken, name []string) float64 {
	q, c := strings.Join(spoken, ""), strings.Join(name, "")
	if len(q) >= 5 && strings.Contains(c, q) {
		return 0.8
	}
	return 0
}

// Match is a candidate and how well it matched.
type Match[T any] struct {
	Item  T
	Score float64
}

// Threshold is the lowest score taken as a match.
const Threshold = 0.6

// Best ranks items by the best score of their names, and keeps those that match.
func Best[T any](spoken string, items []T, names func(T) []string) []Match[T] {
	q := queryWords(spoken)
	var out []Match[T]
	for _, it := range items {
		best := 0.0
		for _, n := range names(it) {
			best = max(best, Score(q, Words(n)))
		}
		if best >= Threshold {
			out = append(out, Match[T]{it, best})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Top is the matches as good as the best (within a small margin).
func Top[T any](ms []Match[T]) []T {
	var out []T
	for _, m := range ms {
		if m.Score >= ms[0].Score-0.04 {
			out = append(out, m.Item)
		}
	}
	return out
}

// agentNames are the names an agent answers to: its space, its tab, both, its app.
func agentNames(a Agent) []string {
	n := []string{a.Space, a.Space + " " + a.Tab, a.Tab + " " + a.Space}
	if !numeric(a.Tab) {
		n = append(n, a.Tab)
	}
	if a.App != "" {
		n = append(n, a.App, strings.TrimSuffix(a.App, "-code")+" "+a.Space)
	}
	return n
}

// FindAgents is the agents spoken names: one when it is clear, several when it isn't (to ask back),
// none when nothing matches. Several agents of one space (its coordinator and its tasks) named by the
// space alone: the most urgent of them, then its first tab.
func (w World) FindAgents(spoken string) []Agent {
	ms := Best(spoken, w.Agents(), agentNames)
	if len(ms) == 0 {
		return nil
	}
	top := Top(ms)
	if len(top) > 1 {
		same := true
		for _, a := range top[1:] {
			if a.SpaceID != top[0].SpaceID {
				same = false
			}
		}
		if same {
			SortByUrgency(top)
			return top[:1]
		}
	}
	return top
}

// FindSpaces is the spaces spoken names (one when clear).
func (w World) FindSpaces(spoken string) []Space {
	ms := Best(spoken, w.Spaces, func(s Space) []string { return []string{s.Name} })
	if len(ms) == 0 {
		return nil
	}
	return Top(ms)
}
