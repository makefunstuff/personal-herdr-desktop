// Package memory is what the secretary keeps between conversations, in a markdown file the owner can
// read and edit: facts the owner told it, how the owner likes it to talk, and its corrections. Only
// what the owner said is kept, and never a secret. The conversation itself stays in RAM.
package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"herdr-voice-computer/internal/herdr"
)

// Kinds, in the order the prompt gives them.
const (
	Style      = "style"
	Correction = "correction"
	Fact       = "fact"
)

var kinds = []string{Style, Correction, Fact}

// Memory is the file and what it holds (oldest first within each kind).
type Memory struct {
	Path  string
	Owner string
	mu    sync.Mutex
	items map[string][]string
}

func New(path, owner string) *Memory {
	m := &Memory{Path: path, Owner: owner, items: map[string][]string{}}
	m.load()
	return m
}

func (m *Memory) heading(kind string) string {
	switch kind {
	case Style:
		return "How " + m.Owner + " likes me to talk"
	case Correction:
		return "Corrections"
	}
	return "About " + m.Owner
}

// load reads the file (the owner may have edited it): "## " headings, "- " items.
func (m *Memory) load() {
	b, err := os.ReadFile(m.Path)
	if err != nil {
		return
	}
	cur := ""
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "## "):
			h := strings.ToLower(t[3:])
			switch {
			case strings.Contains(h, "talk") || strings.Contains(h, "style"):
				cur = Style
			case strings.Contains(h, "correct"):
				cur = Correction
			case strings.HasPrefix(h, "about"):
				cur = Fact
			default:
				cur = ""
			}
		case cur != "" && (strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ")):
			if x := strings.TrimSpace(t[2:]); x != "" {
				m.items[cur] = append(m.items[cur], x)
			}
		}
	}
}

// Reload reads the file again (the owner edited it).
func (m *Memory) Reload() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = map[string][]string{}
	m.load()
}

func (m *Memory) save() error {
	var b strings.Builder
	fmt.Fprintf(&b, "# What the voice computer remembers\n\n<!-- Yours to read and edit: one \"- \" line each. It reads this file on every start. -->\n")
	for _, k := range []string{Fact, Style, Correction} {
		fmt.Fprintf(&b, "\n## %s\n\n", m.heading(k))
		for _, x := range m.items[k] {
			fmt.Fprintf(&b, "- %s\n", x)
		}
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
		return err
	}
	tmp := m.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.Path)
}

// ErrSecret: the owner asked to keep a password, a key or the like; it is never written down.
var ErrSecret = errors.New("that looks like a secret")

var secretRE = regexp.MustCompile(`(?i)\b(password|passphrase|passcode|pin( code)?|api[ -]?key|token|secret|private key|ssh key|credit card|card number|cvv|iban|social security|ssn|seed phrase|recovery (code|phrase)|2fa|otp)\b`)
var randomRE = regexp.MustCompile(`[A-Za-z0-9+/_\-]{20,}`)

// IsSecret says whether text looks like something never to keep.
func IsSecret(text string) bool {
	return secretRE.MatchString(text) || randomRE.MatchString(text)
}

func clean(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	text = strings.TrimLeft(text, "-*# ")
	if len(text) > 240 {
		text = text[:240]
	}
	if text != "" && !strings.ContainsAny(text[len(text)-1:], ".!?") {
		text += "."
	}
	if text != "" {
		text = strings.ToUpper(text[:1]) + text[1:]
	}
	return text
}

// similar: two notes say nearly the same thing (the newer one replaces the older).
func similar(a, b string) bool {
	wa, wb := herdr.Words(a), herdr.Words(b)
	if strings.Join(wa, " ") == strings.Join(wb, " ") {
		return true
	}
	return herdr.Score(wa, wb) >= 0.85 && herdr.Score(wb, wa) >= 0.85
}

// Add keeps a note of kind; a near duplicate goes (the newest wins). It returns the note as kept.
func (m *Memory) Add(kind, text string) (string, error) {
	if kind != Style && kind != Correction {
		kind = Fact
	}
	if IsSecret(text) {
		return "", ErrSecret
	}
	text = clean(text)
	if text == "" {
		return "", errors.New("nothing to remember")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := m.items[kind][:0]
	for _, x := range m.items[kind] {
		if !similar(x, text) {
			keep = append(keep, x)
		}
	}
	m.items[kind] = append(keep, text)
	return text, m.save()
}

// Forget removes the note that best matches text, in any kind; "" when none does.
func (m *Memory) Forget(text string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := herdr.Words(text)
	bestK, bestI, best := "", -1, 0.0
	for _, k := range kinds {
		for i, x := range m.items[k] {
			// every word said matches one of the note's (or the other way round, for a short note)
			s := max(herdr.Score(q, herdr.Words(x)), herdr.Score(herdr.Words(x), q))
			if s > best || (s == best && s > 0) {
				best, bestK, bestI = s, k, i
			}
		}
	}
	if best < 0.6 {
		return "", nil
	}
	gone := m.items[bestK][bestI]
	m.items[bestK] = append(m.items[bestK][:bestI:bestI], m.items[bestK][bestI+1:]...)
	return gone, m.save()
}

// Items is a copy of one kind's notes, oldest first.
func (m *Memory) Items(kind string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.items[kind]...)
}

// Has says whether a style note matches (e.g. "don't use the owner's name").
func (m *Memory) HasStyle(re *regexp.Regexp) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.items[Style]) - 1; i >= 0; i-- {
		if x := m.items[Style][i]; re.MatchString(x) {
			return x, true
		}
	}
	return "", false
}

// Prompt is the memory for the brain's instructions: style first, then corrections, then facts, the
// newest of each first, within about limit characters. "" when there is nothing.
func (m *Memory) Prompt(limit int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b strings.Builder
	used := 0
	for _, k := range kinds {
		xs := m.items[k]
		var lines []string
		for i := len(xs) - 1; i >= 0; i-- {
			if used+len(xs[i])+3 > limit {
				break
			}
			used += len(xs[i]) + 3
			lines = append(lines, "- "+xs[i])
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s:\n%s\n", m.heading(k), strings.Join(lines, "\n"))
	}
	if b.Len() == 0 {
		return ""
	}
	return fmt.Sprintf("What you know from earlier conversations with %s (%s told you these; you may use them):\n%s",
		m.Owner, m.Owner, strings.TrimSpace(b.String()))
}
