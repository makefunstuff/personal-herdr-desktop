// Package herdr reads and drives the Herdr desktop app through its own CLIs: `herdr-app ls|status|do`
// for the window (spaces, tabs, layouts) and `hc read|send` for terminals. vcd builds every command
// here from names it looked up; nothing a model wrote is ever run.
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Runner runs a command and returns its stdout (tests swap it).
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Exec is the real Runner.
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(out, &e) == nil && e.Error != "" {
			return out, errors.New(e.Error)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("%s: %s", name, oneLine(msg, 200))
	}
	return out, nil
}

type Client struct {
	App, HC string // the herdr-app and hc commands
	Run     Runner
}

func New(app, hc string) *Client { return &Client{App: app, HC: hc, Run: Exec} }

// Term is one terminal in a tab, as `herdr-app ls` gives it.
type Term struct {
	Terminal  string   `json:"terminal"`
	Pane      string   `json:"pane"`
	Status    string   `json:"status"` // working, blocked, done, idle, or "" (not an agent)
	App       string   `json:"app"`
	Name      *string  `json:"name"`
	SubAgents []string `json:"sub_agents"`
}

type Tab struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	Terminals []Term `json:"terminals"`
}

type Space struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Machine *string `json:"machine"`
	Active  bool    `json:"active"`
	Tabs    []Tab   `json:"tabs"`
}

// Status is `herdr-app status`: what is on screen.
type Status struct {
	Layout string `json:"layout"`
	Space  string `json:"space"`
	Tab    string `json:"tab"`
	Agent  string `json:"agent"`
}

// World is Herdr now: its spaces and what is on screen.
type World struct {
	Spaces []Space
	Status Status
}

// Agent is one agent terminal with where it is.
type Agent struct {
	Terminal, Status, App, Name string
	Space, SpaceID, Machine     string
	Tab, TabID                  string
	TabIndex                    int
	Focused                     bool
}

// Label is how vcd names an agent out loud: its space, and its tab when that has a name of its own.
func (a Agent) Label() string {
	if a.Tab == "" || numeric(a.Tab) || strings.EqualFold(a.Tab, a.Space) || strings.EqualFold(a.Tab, "coordinator") {
		return Spoken(a.Space)
	}
	return Spoken(a.Space) + " " + Spoken(a.Tab)
}

// Remote says whether the agent runs on another machine.
func (a Agent) Remote() bool { return a.Machine != "" }

// Spoken is a name said out loud: dashes and underscores as spaces.
func Spoken(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(s)), " ")
}

func numeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// World reads `herdr-app ls` and `herdr-app status`.
func (c *Client) World(ctx context.Context) (World, error) {
	out, err := c.Run(ctx, c.App, "ls")
	if err != nil {
		return World{}, err
	}
	var w World
	var ls struct {
		Spaces []Space `json:"spaces"`
	}
	if err := json.Unmarshal(out, &ls); err != nil {
		return World{}, fmt.Errorf("herdr-app ls: %w", err)
	}
	w.Spaces = ls.Spaces
	if out, err := c.Run(ctx, c.App, "status"); err == nil {
		json.Unmarshal(out, &w.Status)
	}
	return w, nil
}

// Agents is every agent terminal, in Herdr's order (space, then tab).
func (w World) Agents() []Agent {
	var out []Agent
	for _, s := range w.Spaces {
		m := ""
		if s.Machine != nil {
			m = *s.Machine
		}
		for i, t := range s.Tabs {
			for _, x := range t.Terminals {
				if x.Status == "" && x.App == "" {
					continue
				}
				name := ""
				if x.Name != nil {
					name = *x.Name
				}
				out = append(out, Agent{Terminal: x.Terminal, Status: x.Status, App: x.App, Name: name,
					Space: s.Name, SpaceID: s.ID, Machine: m, Tab: t.Name, TabID: t.ID, TabIndex: i,
					Focused: x.Terminal == w.Status.Agent})
			}
		}
	}
	return out
}

// Focused is the agent on screen, if any.
func (w World) Focused() (Agent, bool) {
	for _, a := range w.Agents() {
		if a.Focused {
			return a, true
		}
	}
	return Agent{}, false
}

// ActiveSpace is the space on screen.
func (w World) ActiveSpace() (Space, bool) {
	for _, s := range w.Spaces {
		if s.ID == w.Status.Space || (w.Status.Space == "" && s.Active) {
			return s, true
		}
	}
	return Space{}, false
}

// Do runs a Herdr app command with its arguments.
func (c *Client) Do(ctx context.Context, cmd string, args map[string]any) error {
	a := []string{"do", cmd}
	if len(args) > 0 {
		b, _ := json.Marshal(args)
		a = append(a, string(b))
	}
	_, err := c.Run(ctx, c.App, a...)
	return err
}

// Read is a terminal's last lines of text (`hc read`).
func (c *Client) Read(ctx context.Context, terminal string, lines int) (string, error) {
	out, err := c.Run(ctx, c.HC, "read", terminal, "--lines", fmt.Sprint(lines))
	return string(out), err
}

// Send types text into a terminal and presses enter (`hc send`). Only ever after the owner confirmed.
func (c *Client) Send(ctx context.Context, terminal, text string) error {
	_, err := c.Run(ctx, c.HC, "send", terminal, text)
	return err
}

// urgency orders statuses: the most urgent first.
func urgency(s string) int {
	switch s {
	case "blocked":
		return 0
	case "done":
		return 1
	case "working":
		return 2
	case "idle":
		return 3
	}
	return 4
}

// SortByUrgency puts blocked agents first, then done, working and idle, keeping Herdr's order within.
func SortByUrgency(as []Agent) {
	sort.SliceStable(as, func(i, j int) bool { return urgency(as[i].Status) < urgency(as[j].Status) })
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
