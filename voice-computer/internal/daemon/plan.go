package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"herdr-voice-computer/internal/herdr"
	"herdr-voice-computer/internal/intent"
	"herdr-voice-computer/internal/shepherdr"
)

// plan turns one tool call into an action: vcd looks every name up in Herdr (the brain never supplies
// a terminal, a command or a path), and the intent's tier says whether it needs a confirm. A name that
// matches nothing or more than one thing is asked back, never guessed.
func (d *Daemon) plan(ctx context.Context, c intent.Call, w herdr.World, werr error, untrusted string) *action {
	if !intent.Known(c.Name) {
		return notDone("there is no tool called %q", c.Name)
	}
	if c.Args == nil {
		return notDone("its arguments weren't readable")
	}
	tier := intent.TierOf(c.Name, untrusted != "")
	herdrTools := map[string]bool{"needs_me": true, "briefing": true, "list_agents": true, "agent_doing": true,
		"read_latest": true, "focus": true, "go_to_space": true, "new_tab": true, "send_to_agent": true}
	if werr != nil && herdrTools[c.Name] {
		return notDone("I can't reach Herdr (%s).", plainErr(werr))
	}
	var a *action
	switch c.Name {
	case "remember":
		a = d.planRemember(c)
	case "forget":
		a = d.planForget(c)
	case "forget_conversation":
		a = d.planForgetConversation()
	case "forget_talk":
		a = d.planForgetTalk(c)
	case "about_me":
		a = tell(func(ctx context.Context) (string, string) { return d.recall() })
	case "recall":
		a = d.planRecall(c)
	case "web_search":
		a = d.planWebSearch(c)
	case "read_page":
		a = d.planReadPage()
	case "browse":
		a = d.planBrowse(c)
	case "read_app":
		a = d.planReadApp(c)
	case "open_app":
		a = d.planOpenApp(c)
	case "computer_task":
		a = d.planComputerTask(c)
	case "pomodoro":
		a = d.planPomodoro(c)
	case "last_note":
		a = tell(func(ctx context.Context) (string, string) { return d.lastNote() })
	case "volume":
		a = d.planVolume(c)
	case "media":
		action := c.Str("action")
		a = runner{func(ctx context.Context, _ string) (string, []Done, error) {
			out, done, err := d.doMedia(ctx, action)
			return "Done: " + out + ".", done, err
		}}.act(fmt.Sprintf("Media: %s", action), "", "Do it")
	case "needs_me":
		a = tell(func(ctx context.Context) (string, string) { return d.needsMe(ctx, w) })
	case "briefing":
		a = tell(func(ctx context.Context) (string, string) { return d.briefing(ctx, w) })
	case "list_agents":
		a = tell(func(ctx context.Context) (string, string) { return "told the owner the agents' states", listAgents(w) })
	case "agent_doing":
		a = d.planDoing(c, w, false)
	case "read_latest":
		a = d.planDoing(c, w, true)
	case "focus":
		a = d.planFocus(c.Str("name"), w, false)
	case "go_to_space":
		a = d.planFocus(c.Str("space"), w, true)
	case "agent_step":
		a = d.planStep(c)
	case "new_tab":
		a = d.planTab(c, w)
	case "send_to_agent":
		a = d.planSend(c, w)
	case "switch_layout":
		a = d.planLayout(c)
	case "shepherdr_panel":
		a = d.planPanel(c)
	case "write_note":
		a = d.planNote(c)
	case "list_notes":
		a = tell(func(ctx context.Context) (string, string) { return d.listNotes() })
	case "remind":
		a = d.planRemind(c)
	case "list_reminders":
		a = tell(func(ctx context.Context) (string, string) { return d.listReminders() })
	case "cancel_reminder":
		which := c.Str("which")
		a = tell(func(ctx context.Context) (string, string) { return d.cancelReminder(which) })
	}
	if a.run != nil {
		a.tier = max(tier, a.minTier)
	}
	return a
}

// runner builds an action from its run func.
type runner struct {
	run func(ctx context.Context, text string) (string, []Done, error)
}

func (x runner) act(title, detail, confirm string) *action {
	return &action{run: x.run, card: Card{Title: title, Detail: detail, Confirm: confirm}}
}

// both is a tool result whose brain-facing output and spoken words differ: what an agent or Shepherdr
// wrote is said to the owner, never given to the brain (it gets a neutral line).
func both(brain, say string) string { return "Done: " + brain + "." + sayMark + say }

const sayMark = "\x00say:"

// tell is a tier-0 action that reads something and says it.
func tell(f func(ctx context.Context) (brain, say string)) *action {
	return runner{func(ctx context.Context, _ string) (string, []Done, error) {
		brain, say := f(ctx)
		return both(brain, say), nil, nil
	}}.act("", "", "")
}

func (d *Daemon) planVolume(c intent.Call) *action {
	act := c.Str("action")
	level, has := c.Int("level")
	if act == "" && has {
		act = "set"
	}
	switch act {
	case "set":
		if !has || level < 0 || level > 100 {
			return notDone("which level? Ask the owner for a volume from 0 to 100.")
		}
	case "up", "down", "mute", "unmute":
	default:
		return notDone("volume takes set, up, down, mute or unmute")
	}
	title := map[string]string{"set": fmt.Sprintf("Volume to %d%%", level), "up": "Volume up",
		"down": "Volume down", "mute": "Mute", "unmute": "Unmute"}[act]
	return runner{func(ctx context.Context, _ string) (string, []Done, error) {
		out, done, err := d.doVolume(ctx, act, level, has)
		return "Done: " + out + ".", done, err
	}}.act(title, "", "Do it")
}

// ---- finding agents and spaces

// findAgent is the agent a spoken name means: a label from the last ask-back ("the second one"), "it"
// (the last one talked about), the one on screen for "" or "this", else Herdr's names.
func (d *Daemon) findAgent(name string, w herdr.World) []herdr.Agent {
	if a, ok := d.pickAsked("agent", name); ok {
		for _, x := range w.Agents() {
			if x.Terminal == a.agent.Terminal {
				return []herdr.Agent{x}
			}
		}
	}
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "it", "that", "that one", "him", "her", "them", "the last one":
		d.mu.Lock()
		it := d.sec.it
		d.mu.Unlock()
		if it.kind == "agent" {
			for _, x := range w.Agents() {
				if x.Terminal == it.agent.Terminal {
					return []herdr.Agent{x}
				}
			}
		}
	case "", "this", "this one", "this agent", "the focused one", "the current one":
		if a, ok := w.Focused(); ok {
			return []herdr.Agent{a}
		}
		return nil
	}
	return w.FindAgents(name)
}

// askAgents asks back which of several agents.
func (d *Daemon) askAgents(found []herdr.Agent) *action {
	if len(found) > 4 {
		found = found[:4]
	}
	var list []asked
	var labels []string
	a := &action{}
	for _, x := range found {
		list = append(list, asked{kind: "agent", label: x.Label(), agent: x})
		labels = append(labels, x.Label())
		a.choices = append(a.choices, Choice{Label: x.Label(), Text: x.Status})
	}
	d.setAsked(list)
	a.out = fmt.Sprintf("Not done yet: %d agents match: %s. Which one?", len(found), orList(labels))
	return a
}

func (d *Daemon) setIt(a herdr.Agent) {
	d.mu.Lock()
	d.sec.it.kind, d.sec.it.agent = "agent", a
	d.mu.Unlock()
}

// planFocus shows an agent (agent.focus) or a space (space.select): spaceFirst for "go to <space>".
func (d *Daemon) planFocus(name string, w herdr.World, spaceFirst bool) *action {
	if strings.TrimSpace(name) == "" {
		return notDone("which agent or space? Ask the owner.")
	}
	agents := d.findAgent(name, w)
	spaces := w.FindSpaces(name)
	if spaceFirst && len(spaces) == 1 || len(agents) == 0 && len(spaces) == 1 {
		sp := spaces[0]
		return runner{func(ctx context.Context, _ string) (string, []Done, error) {
			if err := d.herdr.Do(ctx, "space.select", map[string]any{"id": sp.ID}); err != nil {
				return "", nil, err
			}
			return both("on "+herdr.Spoken(sp.Name), d.phrase("focus.space", map[string]string{"space": herdr.Spoken(sp.Name)})),
				[]Done{{Text: "Space " + sp.Name}}, nil
		}}.act("Go to "+sp.Name, "", "Go")
	}
	switch {
	case len(agents) == 0:
		return &action{out: both("no agent or space is called "+name, d.phrase("agent.unknown", map[string]string{"x": name}))}
	case len(agents) > 1:
		return d.askAgents(agents)
	}
	ag := agents[0]
	return runner{func(ctx context.Context, _ string) (string, []Done, error) {
		if err := d.herdr.Do(ctx, "agent.focus", map[string]any{"terminal": ag.Terminal}); err != nil {
			return "", nil, err
		}
		d.setIt(ag)
		return both("showing "+ag.Label()+", "+statusWords(ag.Status),
				d.phrase("focus.agent", map[string]string{"label": ag.Label(), "status": statusWords(ag.Status)})),
			[]Done{{Text: "Showing " + ag.Label()}}, nil
	}}.act("Show "+ag.Label(), "", "Show")
}

func statusWords(s string) string {
	switch s {
	case "blocked":
		return "waiting on you"
	case "done":
		return "finished"
	case "working":
		return "working"
	case "idle":
		return "idle"
	}
	return "no status"
}

func (d *Daemon) planStep(c intent.Call) *action {
	cmd, word := "agent.next", "Next"
	if c.Str("direction") == "previous" {
		cmd, word = "agent.prev", "Previous"
	}
	return runner{func(ctx context.Context, _ string) (string, []Done, error) {
		if err := d.herdr.Do(ctx, cmd, nil); err != nil {
			return "", nil, err
		}
		w, err := d.herdr.World(ctx)
		if a, ok := w.Focused(); err == nil && ok {
			d.setIt(a)
			return both(a.Label()+", "+statusWords(a.Status), d.phrase("step", map[string]string{"label": a.Label(), "status": statusWords(a.Status)})),
				[]Done{{Text: word + " agent"}}, nil
		}
		return "Done: " + word + " agent.", []Done{{Text: word + " agent"}}, nil
	}}.act(word+" agent", "", "Go")
}

var agentArgv = map[string][]string{"claude": {"claude"}, "codex": {"codex"}}

func (d *Daemon) planTab(c intent.Call, w herdr.World) *action {
	name := c.Str("space")
	var sp herdr.Space
	if name == "" || name == "this" || name == "here" {
		s, ok := w.ActiveSpace()
		if !ok {
			return notDone("no space is shown")
		}
		sp = s
	} else {
		found := w.FindSpaces(name)
		switch {
		case len(found) == 0:
			return notDone("I can't find a space called %s.", name)
		case len(found) > 1:
			var labels []string
			for _, s := range found {
				labels = append(labels, herdr.Spoken(s.Name))
			}
			return &action{out: "Not done yet: " + strconv.Itoa(len(found)) + " spaces match: " + orList(labels) + ". Which one?"}
		}
		sp = found[0]
	}
	agent := strings.ToLower(c.Str("agent"))
	argv := agentArgv[agent]
	args := map[string]any{"space": sp.ID}
	what, with := "a new tab", ""
	if argv != nil {
		args["argv"] = argv
		what, with = "a new tab with "+agent, " with "+capFirst(agent)
	}
	a := runner{func(ctx context.Context, _ string) (string, []Done, error) {
		if err := d.herdr.Do(ctx, "tab.new", args); err != nil {
			return "", nil, err
		}
		return both("opened "+what+" in "+herdr.Spoken(sp.Name), d.phrase("tab.new", map[string]string{"space": herdr.Spoken(sp.Name), "with": with})),
			[]Done{{Text: "New tab in " + sp.Name}}, nil
	}}.act("Open "+what+" in "+sp.Name, "", "Open")
	if sp.Machine != nil && *sp.Machine != "" {
		a.minTier = intent.Tier1 // it starts a program on another machine: asked first
		a.card.Detail = "On " + *sp.Machine
		a.card.Ask = "Open " + what + " in " + herdr.Spoken(sp.Name) + " on " + *sp.Machine + "?"
	}
	return a
}

// splitTell finds the agent at the start of "semantic grep run the tests": the longest run of words that
// names one agent, the rest is the text.
func splitTell(said string, w herdr.World) (herdr.Agent, string, bool) {
	words := strings.Fields(said)
	for k := min(len(words)-1, 6); k >= 1; k-- {
		name := strings.Join(words[:k], " ")
		found := w.FindAgents(name)
		if len(found) == 1 {
			rest := strings.TrimSpace(strings.Join(words[k:], " "))
			rest = strings.TrimLeft(rest, ":,.- ")
			low := strings.ToLower(rest)
			for _, p := range []string{"to ", "that ", "agent to ", "agent that ", "agent "} {
				if strings.HasPrefix(low, p) {
					rest, low = rest[len(p):], low[len(p):]
				}
			}
			if rest == "" {
				return herdr.Agent{}, "", false
			}
			return found[0], strings.TrimSpace(rest), true
		}
	}
	return herdr.Agent{}, "", false
}

func (d *Daemon) planSend(c intent.Call, w herdr.World) *action {
	var ag herdr.Agent
	text := c.Str("text")
	if said := c.Str("say"); said != "" { // the fast grammar's "tell …": the name and the words together
		a, t, ok := splitTell(said, w)
		if !ok {
			return notDone("I can't tell which agent: say \"tell\", the agent's name, then what to send.")
		}
		ag, text = a, t
	} else {
		found := d.findAgent(c.Str("agent"), w)
		switch {
		case len(found) == 0:
			return &action{out: both("no agent is called "+c.Str("agent"), d.phrase("agent.unknown", map[string]string{"x": c.Str("agent")}))}
		case len(found) > 1:
			return d.askAgents(found)
		}
		ag = found[0]
	}
	if text == "" {
		return notDone("what should I send? Ask the owner.")
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 2000 {
		text = text[:2000]
	}
	d.setIt(ag)
	{ // a Shepherdr coordinator or task: a brief through its chat, with context from the notes
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		chat, src := d.shepherdrChat(ctx, ag)
		cancel()
		if src != nil {
			return d.planDelegate(ag, chat, src, text)
		}
	}
	label := ag.Label()
	where := ""
	if ag.Remote() {
		where = " on " + ag.Machine
	}
	a := runner{func(ctx context.Context, words string) (string, []Done, error) {
		if words == "" {
			words = text
		}
		if err := d.sendTo(ctx, ag, words); err != nil {
			return "", nil, err
		}
		return both("sent to "+label, d.phrase("send.done", map[string]string{"label": label})), []Done{{Text: "Sent to " + label}}, nil
	}}.act("Send to "+label+where, ag.Terminal, "Send")
	a.card.Text = text
	a.card.Ask = d.phrase("send.ask", map[string]string{"label": label, "text": strings.TrimRight(oneLine(text, 160), ".!?")})
	a.declined = d.phrase("send.declined", nil)
	a.minTier = intent.Tier1
	return a
}

// sendTo sends the owner's words to an agent: a coordinator of this Mac's Shepherdr through its chat,
// anything else typed into its terminal (hc send). Only after the owner confirmed.
func (d *Daemon) sendTo(ctx context.Context, ag herdr.Agent, text string) error {
	if chat, src := d.shepherdrChat(ctx, ag); src != nil {
		return src.Send(ctx, chat, text, fmt.Sprintf("vc%d", time.Now().UnixMilli()))
	}
	return d.herdr.Send(ctx, ag.Terminal, text)
}

// shepherdrChat is the Shepherdr chat an agent is, when one of the configured Shepherdrs runs it: its
// place (the space's name) on the Herdr machine that Shepherdr shows its agents under.
func (d *Daemon) shepherdrChat(ctx context.Context, ag herdr.Agent) (string, *shepherdr.Client) {
	for i, s := range d.cfg.Shepherdrs {
		if s.HerdrMachine == "" || ag.Machine != s.HerdrMachine {
			continue
		}
		snap, err := d.sheps[i].Snapshot(ctx)
		if err != nil {
			return "", nil
		}
		for _, p := range snap.Places {
			if strings.EqualFold(p.Name, ag.Space) {
				if ag.TabIndex == 0 {
					return p.Chat, d.sheps[i]
				}
				for _, t := range snap.Tasks {
					if t.Place == p.ID && t.Agent != nil && strings.Contains(ag.Tab, t.ID) {
						return t.Agent.Chat, d.sheps[i]
					}
				}
			}
		}
	}
	return "", nil
}

func (d *Daemon) planLayout(c intent.Call) *action {
	name := strings.ToLower(strings.ReplaceAll(c.Str("layout"), " ", ""))
	ok := false
	for _, l := range d.cfg.Layouts {
		if l == name {
			ok = true
		}
	}
	if !ok {
		return notDone("there is no %s layout; there are %s.", name, orList(d.cfg.Layouts))
	}
	return runner{func(ctx context.Context, _ string) (string, []Done, error) {
		if err := d.herdr.Do(ctx, "layout.switch", map[string]any{"name": name}); err != nil {
			return "", nil, err
		}
		return both("switched to the "+name+" layout", d.phrase("layout", map[string]string{"layout": name})), []Done{{Text: "Layout " + name}}, nil
	}}.act("Switch to the "+name+" layout", "", "Switch")
}

func (d *Daemon) planPanel(c intent.Call) *action {
	show, has := c.Bool("show")
	toggle := c.Str("toggle") == "true"
	if !has && !toggle {
		return notDone("show or hide the Shepherdr panel? Ask the owner.")
	}
	cmd, word, key := "layout.hide", "Hiding", "panel.hide"
	switch {
	case toggle:
		cmd, word, key = "layout.toggle", "Toggled", "panel.toggle"
	case show:
		cmd, word, key = "layout.show", "Showing", "panel.show"
	}
	return runner{func(ctx context.Context, _ string) (string, []Done, error) {
		d.mu.Lock()
		d.sec.it.kind = "panel"
		d.mu.Unlock()
		if err := d.herdr.Do(ctx, cmd, map[string]any{"id": "shepherdr"}); err != nil {
			if strings.Contains(err.Error(), "no node") {
				return both("this layout has no Shepherdr panel", d.phrase("panel.none", nil)), nil, nil
			}
			return "", nil, err
		}
		return both(strings.ToLower(word)+" the Shepherdr panel", d.phrase(key, nil)), []Done{{Text: word + " the Shepherdr panel"}}, nil
	}}.act(word+" the Shepherdr panel", "", "Do it")
}

// ---- asked-back candidates

func (d *Daemon) setAsked(list []asked) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = list
}

var ordinals = map[string]int{"first": 1, "one": 1, "1": 1, "second": 2, "two": 2, "2": 2, "third": 3,
	"three": 3, "3": 3, "fourth": 4, "four": 4, "4": 4, "fifth": 5, "five": 5, "5": 5}

// pickAsked finds name among the last asked-back candidates of kind: their label, or "the second one",
// "number 2", "the last one".
func (d *Daemon) pickAsked(kind, name string) (asked, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var list []asked
	for _, a := range d.asked {
		if a.kind == kind {
			list = append(list, a)
		}
	}
	if len(list) == 0 || strings.TrimSpace(name) == "" {
		return asked{}, false
	}
	n := strings.ToLower(strings.TrimSpace(name))
	for _, a := range list {
		if strings.EqualFold(a.label, n) {
			return a, true
		}
	}
	var rest []string
	for _, w := range strings.FieldsFunc(n, func(r rune) bool { return r == ' ' || r == ',' || r == '.' }) {
		switch w {
		case "the", "one", "number", "agent", "please":
			continue
		}
		rest = append(rest, w)
	}
	if len(rest) == 1 {
		if rest[0] == "last" {
			return list[len(list)-1], true
		}
		if i, ok := ordinals[rest[0]]; ok && i <= len(list) {
			return list[i-1], true
		}
	}
	return asked{}, false
}

func orList(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " or " + xs[len(xs)-1]
}

func andList(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		cut := strings.LastIndexByte(s[:n], ' ')
		if cut < n/2 {
			cut = n
		}
		return s[:cut] + "…"
	}
	return s
}

var errNoAgent = errors.New("no such agent")
