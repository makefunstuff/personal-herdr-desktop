// Package pomo is the voice computer's pomodoro timer: focus and break sessions, kept across vcd
// restarts (the running one in a state file), every finished or stopped session logged as one JSON line,
// and the stats the Herdr widgets show. Time comes in as arguments, so tests drive their own clock.
package pomo

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	Idle   = "idle"
	Focus  = "focus"
	Break  = "break"
	Paused = "paused"

	FocusMin      = 25
	ShortBreakMin = 5
	LongBreakMin  = 15
	LongEvery     = 4 // every 4th completed focus earns the long break
)

// Session is one line of pomodoro.jsonl.
type Session struct {
	Kind      string  `json:"kind"` // focus or break
	Task      *string `json:"task"`
	Start     int64   `json:"start"` // unix ms
	End       int64   `json:"end"`
	PlannedS  int     `json:"planned_s"`
	DoneS     int     `json:"done_s"`
	Completed bool    `json:"completed"`
}

// running is the timer now, as kept in the state file.
type running struct {
	Kind      string `json:"kind"` // focus, break ("" when idle)
	Task      string `json:"task"`
	StartedAt int64  `json:"started_at"` // unix ms, when the session began
	EndsAt    int64  `json:"ends_at"`    // unix ms; 0 while paused
	PlannedS  int    `json:"planned_s"`
	LeftS     int    `json:"left_s"` // while paused: what was left
	Paused    bool   `json:"paused"`
	Cycle     int    `json:"cycle"`  // completed focus sessions since the last long break
	DoneS     int    `json:"done_s"` // time spent before the last pause
	LastStart int64  `json:"last_start"`
}

// Info is the pomo field of vcd's state.
type Info struct {
	State     string  `json:"state"` // idle, focus, break, paused
	Kind      string  `json:"kind"`  // focus or break, also while paused ("" idle)
	Task      *string `json:"task"`
	EndsAt    *int64  `json:"ends_at"`
	StartedAt *int64  `json:"started_at"`
	PlannedS  int     `json:"planned_s"`
	LeftS     int     `json:"left_s"`
	Cycle     int     `json:"cycle"`
}

// Event is what a Tick finished.
type Event struct {
	Session   Session
	Next      string // what comes next: "break" after a focus, "focus" after a break
	LongBreak bool
}

type Timer struct {
	StatePath, LogPath string
	mu                 sync.Mutex
	r                  running
}

// New loads the running timer, if vcd stopped while one ran.
func New(statePath, logPath string) *Timer {
	t := &Timer{StatePath: statePath, LogPath: logPath}
	if b, err := os.ReadFile(statePath); err == nil {
		json.Unmarshal(b, &t.r)
	}
	return t
}

func (t *Timer) save() {
	b, _ := json.Marshal(t.r)
	os.MkdirAll(filepath.Dir(t.StatePath), 0o700)
	tmp := t.StatePath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, t.StatePath)
	}
}

func (t *Timer) log(s Session) error {
	os.MkdirAll(filepath.Dir(t.LogPath), 0o700)
	f, err := os.OpenFile(t.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(s)
	_, err = f.Write(append(b, '\n'))
	return err
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func (t *Timer) leftLocked(now time.Time) int {
	switch {
	case t.r.Kind == "":
		return 0
	case t.r.Paused:
		return t.r.LeftS
	}
	return max(0, int((t.r.EndsAt-ms(now)+999)/1000))
}

func (t *Timer) doneLocked(now time.Time) int {
	if t.r.Paused || t.r.LastStart == 0 {
		return t.r.DoneS
	}
	return t.r.DoneS + int((ms(now)-t.r.LastStart)/1000)
}

func taskPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// endLocked logs the session that ran and clears it.
func (t *Timer) endLocked(now time.Time, completed bool) Session {
	done := t.doneLocked(now)
	end := now
	if completed {
		done = t.r.PlannedS
		end = time.UnixMilli(t.r.EndsAt)
	}
	s := Session{Kind: t.r.Kind, Task: taskPtr(t.r.Task), Start: t.r.StartedAt, End: ms(end), PlannedS: t.r.PlannedS,
		DoneS: min(done, t.r.PlannedS), Completed: completed}
	t.log(s)
	cycle := t.r.Cycle
	t.r = running{Cycle: cycle}
	return s
}

func (t *Timer) startLocked(now time.Time, kind string, minutes int, task string) {
	t.r = running{Kind: kind, Task: task, StartedAt: ms(now), PlannedS: minutes * 60, EndsAt: ms(now.Add(time.Duration(minutes) * time.Minute)),
		Cycle: t.r.Cycle, LastStart: ms(now)}
}

// Start begins a focus session (a running one is stopped and logged first).
func (t *Timer) Start(now time.Time, minutes int, task string) Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	if minutes <= 0 {
		minutes = FocusMin
	}
	if t.r.Kind != "" {
		t.endLocked(now, false)
	}
	t.startLocked(now, Focus, minutes, task)
	t.save()
	return t.infoLocked(now)
}

// Break begins a break: minutes, or 5 (15 after every 4th completed focus). Its length is returned.
func (t *Timer) Break(now time.Time, minutes int) Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.r.Kind != "" {
		t.endLocked(now, false)
	}
	if minutes <= 0 {
		minutes = ShortBreakMin
		if t.r.Cycle >= LongEvery {
			minutes = LongBreakMin
		}
	}
	if minutes >= LongBreakMin {
		t.r.Cycle = 0
	}
	t.startLocked(now, Break, minutes, "")
	t.save()
	return t.infoLocked(now)
}

var ErrIdle = errors.New("no pomodoro is running")

func (t *Timer) Pause(now time.Time) (Info, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.r.Kind == "" || t.r.Paused {
		return t.infoLocked(now), ErrIdle
	}
	t.r.LeftS = t.leftLocked(now)
	t.r.DoneS = t.doneLocked(now)
	t.r.Paused, t.r.EndsAt, t.r.LastStart = true, 0, 0
	t.save()
	return t.infoLocked(now), nil
}

func (t *Timer) Resume(now time.Time) (Info, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.r.Kind == "" || !t.r.Paused {
		return t.infoLocked(now), ErrIdle
	}
	t.r.Paused, t.r.EndsAt, t.r.LastStart = false, ms(now.Add(time.Duration(t.r.LeftS)*time.Second)), ms(now)
	t.save()
	return t.infoLocked(now), nil
}

// Stop ends the session now (logged as not completed).
func (t *Timer) Stop(now time.Time) (Session, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.r.Kind == "" {
		return Session{}, ErrIdle
	}
	s := t.endLocked(now, false)
	t.save()
	return s, nil
}

// Add lengthens the running session by minutes.
func (t *Timer) Add(now time.Time, minutes int) (Info, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.r.Kind == "" {
		return t.infoLocked(now), ErrIdle
	}
	t.r.PlannedS += minutes * 60
	if t.r.Paused {
		t.r.LeftS += minutes * 60
	} else {
		t.r.EndsAt += int64(minutes) * 60000
	}
	t.save()
	return t.infoLocked(now), nil
}

// Tick finishes the session whose time is up (also one that ended while vcd was down).
func (t *Timer) Tick(now time.Time) (Event, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.r.Kind == "" || t.r.Paused || ms(now) < t.r.EndsAt {
		return Event{}, false
	}
	kind := t.r.Kind
	s := t.endLocked(now, true)
	ev := Event{Session: s, Next: Focus}
	if kind == Focus {
		t.r.Cycle++
		ev.Next = Break
		ev.LongBreak = t.r.Cycle >= LongEvery
	}
	t.save()
	return ev, true
}

func (t *Timer) Info(now time.Time) Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.infoLocked(now)
}

func (t *Timer) infoLocked(now time.Time) Info {
	in := Info{State: Idle, Cycle: t.r.Cycle}
	if t.r.Kind == "" {
		return in
	}
	in.State, in.Kind, in.Task, in.PlannedS, in.LeftS = t.r.Kind, t.r.Kind, taskPtr(t.r.Task), t.r.PlannedS, t.leftLocked(now)
	started := t.r.StartedAt
	in.StartedAt = &started
	if t.r.Paused {
		in.State = Paused
	} else {
		ends := t.r.EndsAt
		in.EndsAt = &ends
	}
	return in
}

// ---- stats

type Day struct {
	Date     string `json:"date"`
	Count    int    `json:"count"`
	FocusMin int    `json:"focus_min"`
}

type Period struct {
	Count    int `json:"count"`
	FocusMin int `json:"focus_min"`
}

type TaskMin struct {
	Task     *string `json:"task"`
	FocusMin int     `json:"focus_min"`
}

// Stats is `vc pomo stats --json`: count is completed focus sessions; focus_min every focus minute
// worked, completed or not. The week starts on Monday; the streak counts days with a completed focus,
// up to today (or yesterday, while today has none yet).
type Stats struct {
	Today  Period    `json:"today"`
	Week   Period    `json:"week"`
	Streak int       `json:"streak_days"`
	Days   []Day     `json:"days"`
	Tasks  []TaskMin `json:"tasks"`
}

// ReadLog reads pomodoro.jsonl (a missing file is no sessions).
func ReadLog(path string) []Session {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Session
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s Session
		if json.Unmarshal(sc.Bytes(), &s) == nil && s.Kind != "" {
			out = append(out, s)
		}
	}
	return out
}

func dayOf(ms int64, loc *time.Location) string {
	return time.UnixMilli(ms).In(loc).Format("2006-01-02")
}

func ComputeStats(sessions []Session, now time.Time) Stats {
	loc := now.Location()
	today := now.Format("2006-01-02")
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	weekStart := midnight.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
	byDay := map[string]*Day{}
	taskMin := map[string]int{}
	st := Stats{Days: []Day{}, Tasks: []TaskMin{}}
	for _, s := range sessions {
		if s.Kind != Focus {
			continue
		}
		d := dayOf(s.Start, loc)
		x := byDay[d]
		if x == nil {
			x = &Day{Date: d}
			byDay[d] = x
		}
		secs := s.DoneS
		if s.Completed {
			x.Count++
		}
		x.FocusMin += 0 // minutes are summed in seconds below
		if s.Start >= weekStart.UnixMilli() {
			if s.Completed {
				st.Week.Count++
			}
			st.Week.FocusMin += secs
			name := ""
			if s.Task != nil {
				name = *s.Task
			}
			taskMin[name] += secs
		}
		if d == today {
			if s.Completed {
				st.Today.Count++
			}
			st.Today.FocusMin += secs
		}
		byDay[d].FocusMin += secs
	}
	st.Today.FocusMin = (st.Today.FocusMin + 30) / 60
	st.Week.FocusMin = (st.Week.FocusMin + 30) / 60
	for i := 13; i >= 0; i-- {
		d := midnight.AddDate(0, 0, -i).Format("2006-01-02")
		day := Day{Date: d}
		if x := byDay[d]; x != nil {
			day.Count, day.FocusMin = x.Count, (x.FocusMin+30)/60
		}
		st.Days = append(st.Days, day)
	}
	for day := midnight; ; day = day.AddDate(0, 0, -1) {
		x := byDay[day.Format("2006-01-02")]
		if x == nil || x.Count == 0 {
			if day.Equal(midnight) {
				continue // today has none yet: the streak may still run to yesterday
			}
			break
		}
		st.Streak++
	}
	for name, secs := range taskMin {
		st.Tasks = append(st.Tasks, TaskMin{Task: taskPtr(name), FocusMin: (secs + 30) / 60})
	}
	sort.Slice(st.Tasks, func(i, j int) bool {
		if st.Tasks[i].FocusMin != st.Tasks[j].FocusMin {
			return st.Tasks[i].FocusMin > st.Tasks[j].FocusMin
		}
		return deref(st.Tasks[i].Task) < deref(st.Tasks[j].Task)
	})
	if len(st.Tasks) > 5 {
		st.Tasks = st.Tasks[:5]
	}
	return st
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
