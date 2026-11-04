package desk

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestDailyFileAndToggle(t *testing.T) {
	d := New(t.TempDir())
	at := time.Date(2026, 11, 4, 9, 5, 0, 0, time.Local)
	d.Note(at, "buy  milk")
	task, _ := d.Task(at.Add(time.Minute), "call Anna")
	d.Focus(at.Add(30*time.Minute), 25, "semantic grep")
	d.Check(at, task)
	b, _ := os.ReadFile(d.Path(at))
	want := "# 2026-11-04\n\n- 09:05 buy milk\n- [x] 09:06 call Anna\n- 09:35 🍅 25m focus: semantic grep\n"
	if string(b) != want {
		t.Fatalf("file:\n%s", b)
	}
	if l, err := d.Toggle("2026-11-04", 4); err != nil || l != "- [ ] 09:06 call Anna" {
		t.Fatalf("toggle: %q %v", l, err)
	}
	if _, err := d.Toggle("2026-11-04", 3); err == nil || !strings.Contains(err.Error(), "no checkbox") {
		t.Fatalf("toggle a note: %v", err)
	}
	items := d.Day(at)
	if len(items) != 3 || items[1].Box != " " || items[0].Text != "buy milk" {
		t.Fatalf("items %+v", items)
	}
	if n, _, ok := d.LastNote(at.Add(24 * time.Hour)); !ok || n.Text != "buy milk" {
		t.Fatalf("last note %+v", n)
	}
	if f := d.Files(); len(f) != 1 {
		t.Fatalf("files %v", f)
	}
}
