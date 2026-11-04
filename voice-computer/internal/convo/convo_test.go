package convo

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestPersistSummarizePruneForget(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 30)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.Local)
	s.Append(Turn{At: now.AddDate(0, 0, -40).Unix(), You: "the router keeps dropping", Computer: "I'll note it."})
	s.Append(Turn{At: now.AddDate(0, 0, -2).Unix(), You: "what about the demo", Computer: "It's on Friday, per your note."})
	s.Append(Turn{At: now.Unix(), You: "my password is hunter2 lol", Computer: "I won't keep that."})
	// a restart: a new store reads them back
	s2 := New(dir, 30)
	if ts := s2.Last(60); len(ts) != 3 || ts[1].You != "what about the demo" {
		t.Fatalf("reload: %+v", ts)
	}
	if fi, _ := os.Stat(s2.Path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	// retention: the 40-day-old turn goes, and lives on in the summary
	if n, err := s2.Prune(now); n != 1 || err != nil {
		t.Fatalf("prune %d %v", n, err)
	}
	if ts := s2.All(); len(ts) != 2 {
		t.Fatalf("after prune: %+v", ts)
	}
	sum := s2.Summary()
	if !strings.Contains(sum, `you said "the router keeps dropping"; I said "I'll note it."`) {
		t.Fatalf("summary:\n%s", sum)
	}
	// summarizing again adds nothing twice
	s2.Summarize(s2.All()[:1], 400)
	s2.Summarize(s2.All()[:1], 400)
	if strings.Count(s2.Summary(), "what about the demo") != 1 {
		t.Fatalf("twice:\n%s", s2.Summary())
	}
	// forget what I said about X: turns and summary lines
	if n, _ := s2.ForgetAbout("the router"); n != 1 || strings.Contains(s2.Summary(), "router") {
		t.Fatalf("forget about: %d\n%s", n, s2.Summary())
	}
	s2.Clear()
	if len(s2.All()) != 0 || s2.Summary() != "" {
		t.Fatal("not cleared")
	}
}
