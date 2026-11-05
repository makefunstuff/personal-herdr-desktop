package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"herdr-voice-computer/internal/config"
	"herdr-voice-computer/internal/desk"
	"herdr-voice-computer/internal/index"
	"herdr-voice-computer/internal/intent"
	"herdr-voice-computer/internal/memory"
	"herdr-voice-computer/internal/pomo"
	"herdr-voice-computer/internal/search"
	"herdr-voice-computer/internal/speech"
)

// SearchInfo is the search field of vcd's state: what answers searches and how many paid calls today.
type SearchInfo struct {
	Backend    string `json:"backend"`
	CallsToday int    `json:"calls_today"`
}

// extras sets up web search, the desk notes, the pomodoro and the semantic index.
func (d *Daemon) extras() {
	cfg := d.cfg
	d.desk = desk.New(cfg.NotesDir)
	d.pomo = pomo.New(filepath.Join(config.StateDir(), "pomo.json"), filepath.Join(config.DataDir(), "pomodoro.jsonl"))
	if cfg.Search.Enabled {
		d.search = search.New()
		d.search.DeepSeekKey = cfg.Search.DeepSeekKeyFile
		d.search.SearxURL = cfg.Search.SearxngURL
		d.search.BraveKey = cfg.Search.BraveKeyFile
	}
	if cfg.Embed.URL != "" {
		d.index = index.Open(filepath.Join(config.DataDir(), "index.json"), &index.Embedder{URL: cfg.Embed.URL, Model: cfg.Embed.Model})
	}
	d.searchCalls = filepath.Join(config.StateDir(), "search-calls.json")
}

// ---- web search

type calls struct {
	Date  string `json:"date"`
	Calls int    `json:"calls"`
}

func (d *Daemon) callsToday() int {
	var c calls
	if b, err := os.ReadFile(d.searchCalls); err == nil {
		json.Unmarshal(b, &c)
	}
	if c.Date != time.Now().Format("2006-01-02") {
		return 0
	}
	return c.Calls
}

// countCall counts one paid DeepSeek call (no query is kept).
func (d *Daemon) countCall() {
	n := d.callsToday() + 1
	b, _ := json.Marshal(calls{time.Now().Format("2006-01-02"), n})
	os.MkdirAll(filepath.Dir(d.searchCalls), 0o700)
	os.WriteFile(d.searchCalls, b, 0o600)
}

func (d *Daemon) searchInfo() SearchInfo {
	if d.search == nil {
		return SearchInfo{Backend: "off"}
	}
	b := d.search.LastBackend()
	if b == "" {
		b = "ddg+wikipedia"
		if k, err := os.ReadFile(d.cfg.Search.DeepSeekKeyFile); err == nil && len(strings.TrimSpace(string(k))) > 0 {
			b = "deepseek"
		}
	}
	return SearchInfo{Backend: b, CallsToday: d.callsToday()}
}

func results(rs []search.Result) []map[string]any {
	var out []map[string]any
	for _, r := range rs {
		out = append(out, map[string]any{"title": r.Title, "snippet": r.Snippet, "source": r.Source, "url": r.URL, "date": r.Date})
	}
	return out
}

var sentenceRE = regexp.MustCompile(`[^.!?]+[.!?]+(\s|$)`)

// firstSentences is up to n sentences of text.
func firstSentences(text string, n int) string {
	text = strings.TrimSpace(text)
	ss := sentenceRE.FindAllString(text, -1)
	if len(ss) == 0 {
		return oneLine(text, 300)
	}
	if len(ss) > n {
		ss = ss[:n]
	}
	return strings.TrimSpace(strings.Join(ss, ""))
}

// short says whether the owner asked for short answers (a style note).
func (d *Daemon) short() bool {
	_, ok := d.mem.HasStyle(regexp.MustCompile(`(?i)short|brief|less|one sentence`))
	return ok
}

// webSay is what vcd says of web results: the best one, in its source's words, named.
func (d *Daemon) webSay(rs []map[string]any) string {
	if len(rs) == 0 {
		return d.phrase("web.none", nil)
	}
	n := 3
	if d.short() {
		n = 1
	}
	r := rs[0]
	src, _ := r["source"].(string)
	if src == "" {
		src = "The web"
	}
	text := firstSentences(strings.NewReplacer("**", "", "`", "", "#", "").Replace(fmt.Sprint(r["snippet"])), n)
	return d.fill(d.phrase("web.says", map[string]string{"source": src}), nil) + " " + text
}

func (d *Daemon) planWebSearch(c intent.Call) *action {
	q := strings.TrimSpace(c.Str("query"))
	if q == "" {
		return notDone("what should I look up? Ask the owner.")
	}
	if d.search == nil {
		return &action{out: both("web search is off", d.phrase("web.off", nil))}
	}
	d.mu.Lock()
	asked := d.st.Heard
	d.mu.Unlock()
	return tell(func(ctx context.Context) (string, string) {
		t0 := time.Now()
		var rs []map[string]any
		cached := false
		if d.index != nil && !timeSensitive.MatchString(q+" "+asked) { // a price or today's news is asked afresh
			rs, cached = d.index.Cached(q, 7*24*time.Hour)
			rs = skipCJK(rs) // kept before Chinese pages were skipped
			cached = cached && len(rs) > 0
		}
		warn, backend := "", "cache"
		if !cached {
			found, w, err := d.search.Search(ctx, q, asked)
			warn, backend = w, d.search.LastBackend()
			if err != nil || len(found) == 0 {
				d.logSearch(backend, t0, 0, err)
				if err != nil {
					return "couldn't reach the web", d.phrase("web.down", nil)
				}
				return "found nothing", d.phrase("web.none", nil)
			}
			if backend == "deepseek" {
				d.countCall()
			}
			rs = results(found)
			if d.index != nil {
				go d.index.AddWeb(context.Background(), q, rs, time.Now())
			}
		}
		d.logSearch(backend, t0, len(rs), nil)
		d.mu.Lock()
		d.sec.lastWeb = rs
		d.mu.Unlock()
		say := joinText(warn, d.webSay(rs))
		// A price or a score is rarely in a snippet: the pages are read in turn (the one said first), and the
		// brain answers from the first that gives a figure.
		if timeSensitive.MatchString(q+" "+asked) && len(rs) > 0 && !hasDigit(say) && d.search != nil {
			order := append([]map[string]any{}, rs...)
			for i, r := range order {
				if src, _ := r["source"].(string); src != "" && strings.Contains(say, src) {
					order[0], order[i] = order[i], order[0]
					break
				}
			}
			for i, top := range order {
				if i == 3 {
					break
				}
				u, _ := top["url"].(string)
				src, _ := top["source"].(string)
				if src == "" {
					src = "a page"
				}
				_, text, err := d.search.Fetch(ctx, u, 5000)
				if err != nil || len(strings.TrimSpace(text)) < 200 { // a page drawn by script: a real browser reads it
					if t, _, berr := d.browsePage(ctx, u); berr == nil {
						text, err = t, nil
					}
				}
				ok := err == nil && strings.TrimSpace(text) != ""
				ans := ""
				if ok {
					ans = d.readSource(ctx, asked, src, u, text)
				}
				lb, _ := json.Marshal(map[string]any{"event": "read_top", "n": i, "ok": ok, "figure": hasDigit(ans)})
				d.log.Println(string(lb))
				if hasDigit(ans) {
					say = joinText(warn, ans)
					break
				}
				if i == len(order)-1 || i == 2 {
					say = joinText(warn, "I couldn't find a live figure for that: the pages I checked didn't show one.")
				}
			}
		}
		var brain []string
		for i, r := range rs {
			if i == 3 {
				break
			}
			brain = append(brain, fmt.Sprintf("%v (%v): %v", r["source"], r["title"], oneLine(fmt.Sprint(r["snippet"]), 300)))
		}
		return "found on the web, and said the first: " + strings.Join(brain, " | "), say
	})
}

// logSearch logs a search's timing and backend, never its words.
func (d *Daemon) logSearch(backend string, t0 time.Time, n int, err error) {
	m := map[string]any{"event": "search", "backend": backend, "ms": time.Since(t0).Milliseconds(), "results": n}
	if err != nil {
		m["error"] = true
	}
	b, _ := json.Marshal(m)
	d.log.Println(string(b))
}

// planReadPage reads more of the last result's page (DeepSeek's fetch), or says the other results.
func (d *Daemon) planReadPage() *action {
	return tell(func(ctx context.Context) (string, string) {
		d.mu.Lock()
		rs := d.sec.lastWeb
		d.mu.Unlock()
		if len(rs) == 0 {
			return "there was no search to say more of", d.phrase("web.nopage", nil)
		}
		u, _ := rs[0]["url"].(string)
		src, _ := rs[0]["source"].(string)
		if d.search != nil && u != "" {
			if _, text, err := d.search.Fetch(ctx, u, 4000); err == nil && text != "" {
				d.countCall()
				body := speech.Plain(text)
				if s := firstSentences(body, 4); s != "" {
					return "read more of " + u + ": " + oneLine(body, 600), "The page says: " + s
				}
			}
		}
		if len(rs) > 1 { // no fetch: the next result instead
			return "said the next result", d.webSay(rs[1:])
		}
		return "had nothing more", fmt.Sprintf("That's all %s had.", src)
	})
}

// ---- recall over notes, memory and web results

// EmbeddingGemma's similarities sit high (unrelated short texts score about 0.65): a hit must clear
// recallMin and be close to the best one.
const (
	recallMin    = 0.72
	recallMargin = 0.06
	autoMin      = 0.76
)

// close keeps the hits near the best one.
func closeHits(hits []index.Hit) []index.Hit {
	if len(hits) == 0 {
		return hits
	}
	var out []index.Hit
	for _, h := range hits {
		if h.Score >= hits[0].Score-recallMargin {
			out = append(out, h)
		}
	}
	return out
}

func (d *Daemon) planRecall(c intent.Call) *action {
	q := c.Str("query")
	k, ok := c.Int("k")
	if !ok || k < 1 || k > 5 {
		k = 3
	}
	if q == "" {
		return notDone("recall what? Ask the owner.")
	}
	if d.index == nil {
		return &action{out: both("recall is off", d.phrase("recall.off", nil))}
	}
	return tell(func(ctx context.Context) (string, string) {
		d.syncIndex()
		t0 := time.Now()
		hits, err := d.index.Search(ctx, q, k, recallMin)
		hits = closeHits(hits)
		b, _ := json.Marshal(map[string]any{"event": "recall", "ms": time.Since(t0).Milliseconds(), "hits": len(hits)})
		d.log.Println(string(b))
		if err != nil {
			return "couldn't search the notes", d.phrase("recall.down", nil)
		}
		if len(hits) == 0 {
			return "found nothing in the notes", d.phrase("recall.nothing", map[string]string{"x": q})
		}
		var brain []string
		for _, h := range hits {
			brain = append(brain, fmt.Sprintf("[%s, %s, %.2f] %s", whereFrom(h), h.Date, h.Score, h.Text))
		}
		return "found in the notes: " + strings.Join(brain, " | "), d.hitSay(hits[0])
	})
}

// whereFrom names where a hit came from: "your note", "my memory of you", a site.
func whereFrom(h index.Hit) string {
	switch {
	case h.Source == "memory":
		return "memory"
	case h.Source == "conversation":
		return "our conversation"
	case strings.HasPrefix(h.Source, "http"):
		return "web " + h.Source
	}
	return "note " + filepath.Base(h.Source)
}

var timeLead = regexp.MustCompile(`^(\[[ xX]\] )?\d\d:\d\d `)

func (d *Daemon) hitSay(h index.Hit) string {
	text := strings.TrimSpace(timeLead.ReplaceAllString(h.Text, ""))
	switch {
	case h.Source == "memory":
		return d.phrase("recall.memory", map[string]string{"text": strings.TrimRight(text, ".")})
	case h.Source == "conversation":
		return d.phrase("recall.talk", map[string]string{"text": strings.TrimRight(text, "."), "date": dayWords(h.Date)})
	case strings.HasPrefix(h.Source, "http"):
		return d.phrase("recall.web", map[string]string{"text": strings.TrimRight(text, "."), "date": dayWords(h.Date)})
	}
	return d.phrase("recall.note", map[string]string{"text": strings.TrimRight(text, "."), "date": dayWords(h.Date)})
}

// dayWords says a date the way people do: "today", "yesterday", "Tuesday", "3 November".
func dayWords(date string) string {
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return "earlier"
	}
	now := time.Now()
	days := int(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).Sub(t).Hours() / 24)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 7:
		return t.Weekday().String()
	}
	return t.Format("2 January")
}

// syncIndex brings the index up to date with the notes and memory.md (only what changed is embedded).
func (d *Daemon) syncIndex() {
	if d.index == nil {
		return
	}
	d.indexMu.Lock()
	defer d.indexMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	files := append(d.desk.Files(), d.cfg.MemoryFile)
	if d.convo != nil {
		files = append(files, d.convo.Path)
	}
	t0 := time.Now()
	n, err := d.index.Sync(ctx, files, d.cfg.MemoryFile)
	if n > 0 || err != nil {
		b, _ := json.Marshal(map[string]any{"event": "index", "embedded": n, "chunks": d.index.Len(), "ms": time.Since(t0).Milliseconds(), "ok": err == nil})
		d.log.Println(string(b))
	}
}

// retrieve is the auto-recall for a brain turn: up to 3 relevant pieces of the owner's notes, memory
// and earlier searches, in about 600 characters ("" when nothing is close enough).
func (d *Daemon) retrieve(ctx context.Context, text string) string {
	if d.index == nil || d.index.Len() == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 600*time.Millisecond)
	defer cancel()
	hits, err := d.index.Search(ctx, text, 3, autoMin)
	hits = closeHits(hits)
	if err != nil || len(hits) == 0 {
		return ""
	}
	var parts []string
	used := 0
	for _, h := range hits {
		line := fmt.Sprintf("%s (%s): %s", whereFrom(h), dayWords(h.Date), oneLine(h.Text, 220))
		if used+len(line) > 600 {
			break
		}
		used += len(line)
		parts = append(parts, line)
	}
	if len(parts) == 0 {
		return ""
	}
	return "(From the owner's notes, your memory and earlier searches, which you may use and should name: " + strings.Join(parts, " | ") + ")"
}

// ---- desk notes

func (d *Daemon) planNote(c intent.Call) *action {
	text := oneLine(c.Str("text"), 500)
	if text == "" {
		return notDone("what should the note say? Ask the owner.")
	}
	return runner{func(ctx context.Context, _ string) (string, []Done, error) {
		if _, err := d.desk.Note(time.Now(), text); err != nil {
			return "", nil, err
		}
		go d.syncIndex()
		return both("noted in today's notes", d.phrase("note", nil)), []Done{{Text: "Noted: " + text}}, nil
	}}.act("Add a note", d.cfg.NotesDir, "Add it")
}

// AddNote is `vc note add`.
func (d *Daemon) AddNote(text string) (string, error) {
	text = oneLine(text, 500)
	if text == "" {
		return "", fmt.Errorf("nothing to note")
	}
	line, err := d.desk.Note(time.Now(), text)
	go d.syncIndex()
	return line, err
}

func (d *Daemon) listNotes() (string, string) {
	var notes []string
	for _, it := range d.desk.Day(time.Now()) {
		t := it.Text
		if it.Box != "" {
			continue
		}
		notes = append(notes, strings.TrimRight(t, ".!? "))
	}
	if len(notes) == 0 {
		return "the owner has no notes today", d.phrase("notes.none", nil)
	}
	n := len(notes)
	if n > 4 {
		notes = notes[n-4:]
	}
	head := fmt.Sprintf("%d note%s today. ", n, plural(n))
	if n > 4 {
		head += "The latest: "
	}
	return "read today's notes", head + strings.Join(notes, ". ") + "."
}

func (d *Daemon) lastNote() (string, string) {
	it, day, ok := d.desk.LastNote(time.Now())
	if !ok {
		return "the owner has no notes", d.phrase("notes.none", nil)
	}
	return "read the last note", d.phrase("notes.last", map[string]string{"date": dayWords(day.Format("2006-01-02")),
		"time": it.Time, "text": strings.TrimRight(it.Text, ".!? ")})
}

func (d *Daemon) checkReminderLine(r *reminder) {
	if d.desk != nil && r.line != "" {
		d.desk.Check(r.due, r.line)
	}
}

// ---- pomodoro

func pomoMinutes(c intent.Call) int {
	if n, ok := c.Int("minutes"); ok && n > 0 && n <= 240 {
		return n
	}
	return 0
}

// Pomo runs a pomodoro action (the brain's tool, the fast intents, `vc pomo`): what to say, the state.
func (d *Daemon) Pomo(action string, minutes int, task string) (string, pomo.Info, error) {
	now := time.Now()
	var in pomo.Info
	var err error
	say := ""
	switch action {
	case "start":
		in = d.pomo.Start(now, minutes, task)
		args := map[string]string{"min": fmt.Sprint(in.PlannedS / 60), "task": task}
		if task != "" {
			say = d.phrase("pomo.start.task", args)
		} else {
			say = d.phrase("pomo.start", args)
		}
	case "break":
		in = d.pomo.Break(now, minutes)
		say = d.phrase("pomo.break", map[string]string{"min": fmt.Sprint(in.PlannedS / 60)})
	case "pause":
		if in, err = d.pomo.Pause(now); err == nil {
			say = d.phrase("pomo.pause", map[string]string{"left": minutesWords(in.LeftS)})
		}
	case "resume":
		if in, err = d.pomo.Resume(now); err == nil {
			say = d.phrase("pomo.resume", map[string]string{"left": minutesWords(in.LeftS)})
		}
	case "stop":
		var s pomo.Session
		if s, err = d.pomo.Stop(now); err == nil {
			say = d.phrase("pomo.stop", map[string]string{"done": minutesWords(s.DoneS)})
		}
		in = d.pomo.Info(now)
	case "add":
		if minutes <= 0 {
			minutes = 5
		}
		if in, err = d.pomo.Add(now, minutes); err == nil {
			say = d.phrase("pomo.add", map[string]string{"min": fmt.Sprint(minutes), "left": minutesWords(in.LeftS)})
		}
	case "status":
		in = d.pomo.Info(now)
		switch in.State {
		case pomo.Idle:
			say = d.phrase("pomo.idle", nil)
		case pomo.Paused:
			say = d.phrase("pomo.paused", map[string]string{"left": minutesWords(in.LeftS)})
		default:
			say = d.phrase("pomo.left", map[string]string{"left": minutesWords(in.LeftS), "kind": in.State})
		}
	case "stats":
		st := pomo.ComputeStats(pomo.ReadLog(d.pomo.LogPath), now)
		in = d.pomo.Info(now)
		say = fmt.Sprintf("%d pomodoro%s today, %d this week", st.Today.Count, plural(st.Today.Count), st.Week.Count)
		if st.Streak > 1 {
			say += fmt.Sprintf(", and a %d-day streak", st.Streak)
		}
		say += "."
	default:
		return "", in, fmt.Errorf("pomodoro takes start, break, pause, resume, stop, add, status or stats")
	}
	if err == pomo.ErrIdle {
		return d.phrase("pomo.none", nil), d.pomo.Info(now), err
	}
	d.mu.Lock()
	d.publishLocked()
	d.mu.Unlock()
	return say, in, err
}

func minutesWords(secs int) string {
	m := (secs + 30) / 60
	switch {
	case secs < 60:
		return "less than a minute"
	case m == 1:
		return "a minute"
	}
	return fmt.Sprintf("%d minutes", m)
}

func (d *Daemon) planPomodoro(c intent.Call) *action {
	action, task, minutes := c.Str("action"), c.Str("task"), pomoMinutes(c)
	return tell(func(ctx context.Context) (string, string) {
		say, in, err := d.Pomo(action, minutes, task)
		if err != nil {
			return "pomodoro: " + err.Error(), say
		}
		return fmt.Sprintf("pomodoro %s: %s, %d seconds left", action, in.State, in.LeftS), say
	})
}

// watchPomo ends sessions whose time is up: a line said (after any turn), a toast, and a finished
// focus in the desk notes.
func (d *Daemon) watchPomo(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		ev, ok := d.pomo.Tick(time.Now())
		if !ok {
			continue
		}
		d.pomoEnded(ev)
	}
}

func (d *Daemon) pomoEnded(ev pomo.Event) {
	s := ev.Session
	task := ""
	if s.Task != nil {
		task = *s.Task
	}
	min := s.PlannedS / 60
	var say, toast string
	if s.Kind == pomo.Focus {
		d.desk.Focus(time.UnixMilli(s.End), min, task)
		go d.syncIndex()
		next := pomo.ShortBreakMin
		if ev.LongBreak {
			next = pomo.LongBreakMin
		}
		say = d.phrase("pomo.focus.done", map[string]string{"min": fmt.Sprint(min), "next": fmt.Sprint(next)})
		toast = fmt.Sprintf("Focus done: %d minutes%s. Time for a %d-minute break.", min, map[bool]string{true: " on " + task}[task != ""], next)
	} else {
		say = d.phrase("pomo.break.done", nil)
		toast = "Break's over."
	}
	d.notify(toast)
	d.mu.Lock()
	d.publishLocked()
	d.mu.Unlock()
	for i := 0; i < 120; i++ { // never over the owner: after their turn
		d.mu.Lock()
		busy := d.rec != nil || d.turn != nil || d.batch != nil || d.player != nil || d.lead != nil ||
			d.st.State == Listening || d.st.State == Thinking || d.st.State == Speaking
		d.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	d.Say(d.flavor(say, 6*time.Second))
}

func init() {
	for k, v := range map[string][]string{
		"web.says":        {"{source} says:", "According to {source}:", "Here's what {source} says:"},
		"web.none":        {"I couldn't find anything on that.", "The web came up empty on that one."},
		"web.down":        {"I couldn't reach the web.", "The web isn't answering right now."},
		"web.off":         {"Web search is off.", "I'm not set up to search the web."},
		"web.nopage":      {"There's nothing to read more of yet. What should I look up?"},
		"recall.off":      {"I can't search your notes right now."},
		"recall.down":     {"I couldn't search your notes just now."},
		"recall.nothing":  {"I don't have anything on {x} in your notes.", "Nothing about {x} in your notes. Want me to look it up?"},
		"recall.note":     {"Your note from {date} says: {text}.", "{Date}, you wrote: {text}."},
		"recall.memory":   {"You told me: {text}.", "From what you've told me: {text}."},
		"recall.talk":     {"We talked about it {date}: {text}.", "{Date}, {text}."},
		"recall.web":      {"When I looked it up {date}: {text}.", "From an earlier search: {text}."},
		"notes.none":      {"No notes yet today.", "Nothing written down today, {name}."},
		"notes.last":      {"Your last note, {date} at {time}: {text}.", "{Date} at {time} you wrote: {text}."},
		"pomo.start":      {"{min} minutes, starting now.", "Focus time: {min} minutes. I'll call the break.", "Clock's running, {min} minutes."},
		"pomo.start.task": {"{min} minutes on {task}, starting now.", "Focus on {task} for {min} minutes. I'll tell you when to break.", "Okay, {task}: {min} minutes."},
		"pomo.break":      {"{min}-minute break. Stretch a little.", "Break time, {min} minutes.", "Take {min}, {name}. You've earned it."},
		"pomo.pause":      {"Paused, {left} left.", "Holding the clock at {left}."},
		"pomo.resume":     {"Back on, {left} to go.", "Resumed, {left} left."},
		"pomo.stop":       {"Stopped after {done}.", "Okay, timer's off. That was {done}."},
		"pomo.add":        {"Added {min} minutes, {left} left.", "{Left} now."},
		"pomo.idle":       {"No timer's running.", "Nothing on the clock right now. Want to start a pomodoro?"},
		"pomo.paused":     {"It's paused with {left} left."},
		"pomo.left":       {"{Left} left.", "{Left} to go on this {kind}."},
		"pomo.none":       {"No pomodoro's running.", "There's no timer on right now."},
		"pomo.focus.done": {"That's {min} minutes. Time for a {next}-minute break?", "Focus done. Take {next}, {name}?", "Nice work, {min} minutes in. Break for {next}?"},
		"pomo.break.done": {"Break's over. Ready for another round?", "Back to it? Your break's done.", "Break's up, {name}. Start another pomodoro?"},
	} {
		phrases[k] = v
	}
}

var _ = memory.Fact

// extraStateLocked fills the state's pomodoro and search fields; d.mu is held.
func (d *Daemon) extraStateLocked() {
	if d.pomo != nil {
		d.st.Pomo = d.pomo.Info(time.Now())
	}
	d.st.Search = d.searchInfo()
}

// timeSensitive: questions whose answer changes by the hour, never answered from the search cache.
var timeSensitive = regexp.MustCompile(`(?i)\b(price|prices|cost|rate|today|tonight|now|current|currently|latest|news|weather|score|scores|stock|stocks|live|this week|exchange)\b`)

// skipCJK drops cached results in Chinese, Japanese or Korean (the owner hears English).
func skipCJK(rs []map[string]any) []map[string]any {
	out := rs[:0:0]
	for _, r := range rs {
		t, _ := r["title"].(string)
		x, _ := r["snippet"].(string)
		if !search.MostlyCJK(t + " " + x) {
			out = append(out, r)
		}
	}
	return out
}

// hasFigure: a snippet that carries an amount (78,412, $3.20, 4.5%), not just "last 7 days".
var figureRE = regexp.MustCompile(`\d[\d,.]{2,}|[$€£¥]\s?\d|\d\s?(%|usd|eur|dollars)`)

func hasDigit(s string) bool { return figureRE.MatchString(strings.ToLower(s)) }
