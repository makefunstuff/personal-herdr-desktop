// Package desk writes the owner's notes where they can see them: plain markdown, one file a day,
// ~/Work/desk-notes/daily/YYYY-MM-DD.md, starting "# YYYY-MM-DD". A note is "- HH:MM text", a reminder
// "- [ ] HH:MM text" (checked when it rang or was cancelled), a finished focus "- HH:MM 🍅 25m focus: task".
package desk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Desk struct {
	Dir string
	mu  sync.Mutex
}

func New(dir string) *Desk { return &Desk{Dir: dir} }

// Path is the day's file.
func (d *Desk) Path(day time.Time) string {
	return filepath.Join(d.Dir, "daily", day.Format("2006-01-02")+".md")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// add appends a line to the day's file (made with its heading when new); it returns the line.
func (d *Desk) add(at time.Time, line string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.Path(at)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	head := ""
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		head = "# " + at.Format("2006-01-02") + "\n\n"
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.WriteString(head + line + "\n")
	return line, err
}

// Note writes "- HH:MM text".
func (d *Desk) Note(at time.Time, text string) (string, error) {
	return d.add(at, "- "+at.Format("15:04")+" "+oneLine(text))
}

// Task writes "- [ ] HH:MM text" (a reminder); Check ticks it later.
func (d *Desk) Task(at time.Time, text string) (string, error) {
	return d.add(at, "- [ ] "+at.Format("15:04")+" "+oneLine(text))
}

// Focus writes a finished focus session.
func (d *Desk) Focus(at time.Time, minutes int, task string) (string, error) {
	line := fmt.Sprintf("- %s 🍅 %dm focus", at.Format("15:04"), minutes)
	if task != "" {
		line += ": " + oneLine(task)
	}
	return d.add(at, line)
}

// Check ticks the open task line written on day (by its exact text), if it is still open.
func (d *Desk) Check(day time.Time, line string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.Path(day)
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if l == line && strings.HasPrefix(l, "- [ ] ") {
			lines[i] = "- [x] " + l[len("- [ ] "):]
			return os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
		}
	}
	return nil
}

// Toggle flips the checkbox on line n (1-based) of a day's file ("2026-11-04"): for the widgets.
func (d *Desk) Toggle(date string, n int) (string, error) {
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.Path(day)
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")
	if n < 1 || n > len(lines) {
		return "", fmt.Errorf("%s has no line %d", filepath.Base(p), n)
	}
	l := lines[n-1]
	switch {
	case strings.HasPrefix(l, "- [ ] "):
		l = "- [x] " + l[6:]
	case strings.HasPrefix(l, "- [x] "), strings.HasPrefix(l, "- [X] "):
		l = "- [ ] " + l[6:]
	default:
		return "", fmt.Errorf("line %d has no checkbox", n)
	}
	lines[n-1] = l
	return l, os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o644)
}

var itemRE = regexp.MustCompile(`^- (\[[ xX]\] )?(\d\d:\d\d) (.*)$`)

// Item is one line of a day.
type Item struct {
	Time, Text string
	Box        string // "", " ", "x"
}

// Day is a day's items, in order.
func (d *Desk) Day(day time.Time) []Item {
	b, err := os.ReadFile(d.Path(day))
	if err != nil {
		return nil
	}
	var out []Item
	for _, l := range strings.Split(string(b), "\n") {
		if m := itemRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			box := ""
			if m[1] != "" {
				box = strings.ToLower(m[1][1:2])
			}
			out = append(out, Item{Time: m[2], Text: m[3], Box: box})
		}
	}
	return out
}

// Files is every markdown file under the folder.
func (d *Desk) Files() []string {
	var out []string
	filepath.WalkDir(d.Dir, func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// LastNote is the newest plain note (not a reminder or a focus), looking back up to a month.
func (d *Desk) LastNote(now time.Time) (Item, time.Time, bool) {
	for i := 0; i < 31; i++ {
		day := now.AddDate(0, 0, -i)
		items := d.Day(day)
		for j := len(items) - 1; j >= 0; j-- {
			if items[j].Box == "" && !strings.HasPrefix(items[j].Text, "🍅") {
				return items[j], day, true
			}
		}
	}
	return Item{}, time.Time{}, false
}
