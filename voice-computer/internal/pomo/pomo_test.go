package pomo

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFocusBreakPauseAddAndRestart(t *testing.T) {
	dir := t.TempDir()
	st, lg := filepath.Join(dir, "pomo.json"), filepath.Join(dir, "pomodoro.jsonl")
	p := New(st, lg)
	t0 := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
	in := p.Start(t0, 0, "semantic grep")
	if in.State != Focus || in.PlannedS != 1500 || *in.EndsAt != t0.Add(25*time.Minute).UnixMilli() || *in.Task != "semantic grep" {
		t.Fatalf("start %+v", in)
	}
	// paused 10 min in, for 5 minutes; then 5 more minutes added
	p.Pause(t0.Add(10 * time.Minute))
	if in := p.Info(t0.Add(13 * time.Minute)); in.State != Paused || in.LeftS != 900 || in.EndsAt != nil {
		t.Fatalf("paused %+v", in)
	}
	p.Resume(t0.Add(15 * time.Minute))
	p.Add(t0.Add(16*time.Minute), 5)
	// vcd restarts: the timer goes on
	p = New(st, lg)
	if in := p.Info(t0.Add(20 * time.Minute)); in.State != Focus || in.LeftS != 900 || in.PlannedS != 1800 {
		t.Fatalf("after a restart %+v", in)
	}
	if _, ok := p.Tick(t0.Add(34 * time.Minute)); ok {
		t.Fatal("ended early")
	}
	ev, ok := p.Tick(t0.Add(35 * time.Minute))
	if !ok || ev.Next != Break || !ev.Session.Completed || ev.Session.DoneS != 1800 || p.Info(t0).Cycle != 1 {
		t.Fatalf("end %+v", ev)
	}
	if in := p.Break(t0.Add(36*time.Minute), 0); in.PlannedS != 300 || in.State != Break {
		t.Fatalf("break %+v", in)
	}
	p.Stop(t0.Add(38 * time.Minute))
	ss := ReadLog(lg)
	if len(ss) != 2 || ss[1].Kind != Break || ss[1].Completed || ss[1].DoneS != 120 {
		t.Fatalf("log %+v", ss)
	}
	// the 4th completed focus earns the long break
	now := t0.Add(time.Hour)
	for i := 0; i < 3; i++ {
		p.Start(now, 25, "")
		now = now.Add(25 * time.Minute)
		p.Tick(now)
	}
	if in := p.Break(now, 0); in.PlannedS != 900 {
		t.Fatalf("long break %+v", in)
	}
}

func TestStats(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 11, 4, 18, 0, 0, 0, loc) // a Wednesday
	at := func(d, h int) int64 { return time.Date(2026, 11, d, h, 0, 0, 0, loc).UnixMilli() }
	task := func(s string) *string { return &s }
	ss := []Session{
		{Kind: Focus, Task: task("grep"), Start: at(4, 9), DoneS: 1500, Completed: true},
		{Kind: Focus, Task: task("grep"), Start: at(4, 10), DoneS: 600, Completed: false},
		{Kind: Break, Start: at(4, 11), DoneS: 300, Completed: true},
		{Kind: Focus, Task: task("docs"), Start: at(3, 9), DoneS: 1500, Completed: true},
		{Kind: Focus, Start: at(2, 9), DoneS: 1500, Completed: true},  // Monday
		{Kind: Focus, Start: at(1, 9), DoneS: 1500, Completed: true},  // Sunday: last week
		{Kind: Focus, Start: at(20, 9), DoneS: 1500, Completed: true}, // October 20: out of the 14 days? (Nov 20 is the future)
	}
	ss[6].Start = time.Date(2026, 10, 20, 9, 0, 0, 0, loc).UnixMilli()
	s := ComputeStats(ss, now)
	if s.Today.Count != 1 || s.Today.FocusMin != 35 || s.Week.Count != 3 || s.Week.FocusMin != 85 || s.Streak != 4 {
		t.Fatalf("stats %+v", s)
	}
	if len(s.Days) != 14 || s.Days[13].Date != "2026-11-04" || s.Days[13].Count != 1 || s.Days[0].Date != "2026-10-22" {
		t.Fatalf("days %+v", s.Days)
	}
	if len(s.Tasks) != 3 || *s.Tasks[0].Task != "grep" || s.Tasks[0].FocusMin != 35 || s.Tasks[1].Task != nil || *s.Tasks[2].Task != "docs" {
		t.Fatalf("tasks %+v", s.Tasks)
	}
	// no focus today yet: the streak still counts up to yesterday
	if s := ComputeStats(ss[3:], now); s.Streak != 3 || s.Today.Count != 0 {
		t.Fatalf("streak %+v", s)
	}
}
