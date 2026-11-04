// Package convo keeps the conversation with the owner on this Mac, so the assistant keeps the thread
// across restarts and days: conversation.jsonl (one turn a line, mode 600, kept conversation_days) and
// conversation-summary.md (plain lines the owner can read and edit, restating what older turns said).
// It is only ever read by vcd and sent to the local brain.
package convo

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"herdr-voice-computer/internal/herdr"
)

// Turn is one exchange.
type Turn struct {
	At       int64    `json:"at"` // unix seconds
	You      string   `json:"you"`
	Computer string   `json:"computer"`
	Kind     string   `json:"kind,omitempty"`  // fast, brain, …
	Tools    []string `json:"tools,omitempty"` // the tools it used
}

type Store struct {
	Path, SummaryPath string
	Days              int
	mu                sync.Mutex
}

func New(dir string, days int) *Store {
	if days <= 0 {
		days = 30
	}
	return &Store{Path: filepath.Join(dir, "conversation.jsonl"), SummaryPath: filepath.Join(dir, "conversation-summary.md"), Days: days}
}

func (s *Store) readLocked() []Turn {
	f, err := os.Open(s.Path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Turn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var t Turn
		if json.Unmarshal(sc.Bytes(), &t) == nil && (t.You != "" || t.Computer != "") {
			out = append(out, t)
		}
	}
	return out
}

// All is every kept turn, oldest first.
func (s *Store) All() []Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

// Last is the newest n turns.
func (s *Store) Last(n int) []Turn {
	ts := s.All()
	if len(ts) > n {
		ts = ts[len(ts)-n:]
	}
	return ts
}

// Append keeps a turn.
func (s *Store) Append(t Turn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	os.MkdirAll(filepath.Dir(s.Path), 0o700)
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(t)
	_, err = f.Write(append(b, '\n'))
	return err
}

func (s *Store) writeLocked(ts []Turn) error {
	var b strings.Builder
	for _, t := range ts {
		x, _ := json.Marshal(t)
		b.Write(x)
		b.WriteByte('\n')
	}
	os.MkdirAll(filepath.Dir(s.Path), 0o700)
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// Summary is the summary file's text.
func (s *Store) Summary() string {
	b, _ := os.ReadFile(s.SummaryPath)
	return strings.TrimSpace(string(b))
}

const summaryHead = "# Earlier conversations\n\n<!-- What was said, a line a turn, by day. Yours to read and edit. -->\n"

// SummaryLine restates a turn in one short line (its words only: nothing added).
func SummaryLine(t Turn) string {
	you, me := cut(t.You, 120), cut(t.Computer, 160)
	at := time.Unix(t.At, 0)
	switch {
	case you != "" && me != "":
		return fmt.Sprintf("- %s: you said \"%s\"; I said \"%s\"", at.Format("2006-01-02 15:04"), you, me)
	case you != "":
		return fmt.Sprintf("- %s: you said \"%s\"", at.Format("2006-01-02 15:04"), you)
	}
	return fmt.Sprintf("- %s: I said \"%s\"", at.Format("2006-01-02 15:04"), me)
}

func cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// Summarize adds lines for turns at or before through that aren't in the summary yet (a marker keeps
// how far it got), keeping the summary under maxLines (the oldest lines go first).
func (s *Store) Summarize(older []Turn, maxLines int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, _ := os.ReadFile(s.SummaryPath)
	lines := strings.Split(strings.TrimSpace(string(text)), "\n")
	var body []string
	var through int64
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "<!-- through "):
			fmt.Sscanf(l, "<!-- through %d -->", &through)
		case strings.HasPrefix(l, "- "):
			body = append(body, l)
		}
	}
	added := false
	for _, t := range older {
		if t.At > through {
			body = append(body, SummaryLine(t))
			through, added = t.At, true
		}
	}
	if !added {
		return nil
	}
	if len(body) > maxLines {
		body = body[len(body)-maxLines:]
	}
	out := summaryHead + fmt.Sprintf("<!-- through %d -->\n\n", through) + strings.Join(body, "\n") + "\n"
	os.MkdirAll(filepath.Dir(s.SummaryPath), 0o700)
	return os.WriteFile(s.SummaryPath, []byte(out), 0o600)
}

// Prune drops turns older than Days (after they went into the summary).
func (s *Store) Prune(now time.Time) (int, error) {
	all := s.All()
	cutAt := now.AddDate(0, 0, -s.Days).Unix()
	var old, keep []Turn
	for _, t := range all {
		if t.At < cutAt {
			old = append(old, t)
		} else {
			keep = append(keep, t)
		}
	}
	if len(old) == 0 {
		return 0, nil
	}
	if err := s.Summarize(old, 400); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(old), s.writeLocked(keep)
}

// Clear forgets the whole conversation: both files.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e1 := os.Remove(s.Path)
	e2 := os.Remove(s.SummaryPath)
	if e1 != nil && !os.IsNotExist(e1) {
		return e1
	}
	if e2 != nil && !os.IsNotExist(e2) {
		return e2
	}
	return nil
}

// about says whether a turn or line is about the spoken words (every word said, loosely).
func about(text string, q []string) bool {
	return herdr.Score(q, herdr.Words(text)) >= 0.75
}

// ForgetAbout removes the turns (and summary lines) about what the owner names; it returns how many.
func (s *Store) ForgetAbout(what string) (int, error) {
	q := herdr.Words(what)
	if len(q) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	var keep []Turn
	for _, t := range s.readLocked() {
		if about(t.You+" "+t.Computer, q) {
			n++
			continue
		}
		keep = append(keep, t)
	}
	if n > 0 {
		if err := s.writeLocked(keep); err != nil {
			return 0, err
		}
	}
	if b, err := os.ReadFile(s.SummaryPath); err == nil {
		var out []string
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "- ") && about(l, q) {
				n++
				continue
			}
			out = append(out, l)
		}
		os.WriteFile(s.SummaryPath, []byte(strings.Join(out, "\n")), 0o600)
	}
	return n, nil
}
