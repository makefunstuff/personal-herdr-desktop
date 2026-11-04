// Package intent is what a turn can ask the computer to do: the fast grammar (no model), the intent
// catalogue the brain gets as its tools, and each intent's safety tier.
package intent

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Tool is one intent as the brain gets it (a realtime-style function tool).
type Tool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
func enum(desc string, vals ...string) map[string]any {
	return map[string]any{"type": "string", "enum": vals, "description": desc}
}

func tool(name, desc string, props map[string]any, required ...string) Tool {
	p := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		p["required"] = required
	}
	return Tool{Type: "function", Name: name, Description: desc, Parameters: p}
}

// Layouts are the Herdr layouts the brain may switch to.
var Layouts = []string{"pixel", "win31", "workbench", "fortress", "main"}

// Catalogue is every intent, fixed for a session (so the brain's prompt stays cached). Names are passed
// as the owner said them: vcd looks them up in Herdr and Shepherdr.
func Catalogue() []Tool {
	agent := str("The agent as the owner named it: its space or tab name from the context line, like \"semantic grep\" or \"homelab\".")
	return []Tool{
		tool("needs_me", "When the owner asks what needs them, what is waiting on them, or whether anything needs a look: says the blocked agents and Shepherdr questions.", map[string]any{}),
		tool("briefing", "Only when the owner asks to be briefed or what they missed: what finished, what waits and what runs.", map[string]any{}),
		tool("list_agents", "Only when the owner asks for the list of agents or what they all are doing.", map[string]any{}),
		tool("agent_doing", "Say what one agent is doing now (vcd reads its screen and sums it up).", map[string]any{"agent": agent}, "agent"),
		tool("read_latest", "Read out an agent's latest message.", map[string]any{"agent": str("The agent, or leave it out for the one on screen.")}),
		tool("focus", "Show an agent or a space in the Herdr window.", map[string]any{"name": agent}, "name"),
		tool("go_to_space", "Switch the Herdr window to a space.", map[string]any{"space": str("The space as the owner named it.")}, "space"),
		tool("agent_step", "Show the next or previous agent.", map[string]any{"direction": enum("Which way.", "next", "previous")}, "direction"),
		tool("new_tab", "Open a new tab in a space, or start a new claude or codex session there.", map[string]any{
			"space": str("The space as the owner named it; leave it out for the one on screen."),
			"agent": enum("The agent to start in it, if the owner said one.", "claude", "codex", "shell"),
		}),
		tool("send_to_agent", "Send the owner's words to an agent. Always confirmed by the owner first.", map[string]any{
			"agent": agent,
			"text":  str("What to send, in the owner's words."),
		}, "agent", "text"),
		tool("switch_layout", "Only when the owner asks to switch or change the Herdr window's layout (its theme).", map[string]any{
			"layout": enum("The layout.", Layouts...),
		}, "layout"),
		tool("shepherdr_panel", "Show or hide the Shepherdr panel.", map[string]any{"show": map[string]any{"type": "boolean", "description": "true shows it, false hides it."}}, "show"),
		tool("write_note", "Write a note in the owner's daily notes when they ask to note or write something down.", map[string]any{"text": str("The note, in the owner's words.")}, "text"),
		tool("list_notes", "Read the owner's notes from today.", map[string]any{}),
		tool("last_note", "Read the owner's last note.", map[string]any{}),
		tool("recall", "Find what the owner's notes, your memory or earlier web searches say about something, when the owner asks what they wrote or noted, or what you know about a thing of theirs.", map[string]any{
			"query": str("What to look for, in a few words."),
			"k":     num("How many pieces, 1 to 5; 3 if unsure."),
		}, "query"),
		tool("web_search", "Look a question of fact about the world up on the web (people, places, things, events, definitions). Never for Herdr, its agents, or the owner's own notes.", map[string]any{
			"query": str("A short web search query."),
		}, "query"),
		tool("browse", "Open a website and read it, when the owner names a site or page to look at (\"what's on hacker news\", \"check the go blog for the newest post\"). Read-only.", map[string]any{
			"site":     str("The site's address or name, like \"news.ycombinator.com\" or \"hacker news\"."),
			"question": str("What the owner wants to know from it, in their words."),
		}, "site"),
		tool("read_app", "Read what a Mac app's window shows and answer from it (\"what's in my Telegram\", \"what's playing in Spotify\"). Read-only.", map[string]any{
			"app":      str("The app's name, like \"Firefox\" or \"Telegram\"."),
			"question": str("What the owner wants to know from it, in their words."),
		}, "app"),
		tool("open_app", "Open (launch) a Mac app by name.", map[string]any{
			"app": str("The app's name, like \"Firefox\"."),
		}, "app"),
		tool("read_page", "Read more of the page the last web search found, when the owner asks for more about it.", map[string]any{}),
		tool("pomodoro", "Pomodoro timer: start a focus session, take a break, pause, resume, stop, add minutes, say the time left, or the stats.", map[string]any{
			"action":  enum("What to do.", "start", "break", "pause", "resume", "stop", "add", "status", "stats"),
			"minutes": num("For start, break or add: minutes, if the owner said."),
			"task":    str("For start: what the owner will focus on, if said."),
		}, "action"),
		tool("remind", "Set a reminder or a timer.", map[string]any{
			"minutes": num("Minutes from now."),
			"seconds": num("Seconds from now."),
			"text":    str("What to remind the owner of, if the owner said."),
		}),
		tool("list_reminders", "Say which reminders are set.", map[string]any{}),
		tool("cancel_reminder", "Cancel a reminder.", map[string]any{"which": str("Words from the reminder; leave it out for the last one.")}),
		tool("remember", "Call it whenever the owner tells you how to talk from now on (\"from now on\", \"I prefer\", \"always\", \"never\": kind style), corrects you (\"no, X is Y\": kind correction), or tells you something about themselves to keep (kind fact). Never a secret.", map[string]any{
			"kind": enum("What it is.", "fact", "style", "correction"),
			"text": str("One short sentence about the owner, in the third person, like \"Keep answers shorter.\""),
		}, "kind", "text"),
		tool("forget", "Forget something you remembered, when the owner asks.", map[string]any{"text": str("Words from it.")}, "text"),
		tool("forget_conversation", "Forget the whole conversation when the owner asks to start fresh (asked first).", map[string]any{}),
		tool("forget_talk", "Forget what the owner said about something in your conversations.", map[string]any{"about": str("What it was about.")}, "about"),
		tool("about_me", "Say what you remember about the owner, when they ask what you know about them.", map[string]any{}),
		tool("volume", "Change the sound volume when the owner asks to.", map[string]any{
			"action": enum("up: louder; down: quieter; set: only when the owner says a number; mute; unmute.", "up", "down", "set", "mute", "unmute"),
			"level":  num("Only for set: the number the owner said, 0 to 100."),
		}, "action"),
		tool("media", "Control the music or video: play, pause, next or previous.", map[string]any{
			"action": enum("What to do.", "play", "pause", "next", "previous"),
		}, "action"),
	}
}

// ToolInstructions are added to the brain's instructions when it has the catalogue.
const ToolInstructions = `Your tools act on Herdr, the app where the owner's terminals and coding agents run, and on this ` +
	`Mac. Call a tool only when the owner tells the computer to do that ` +
	`thing now, like "show the homelab agent" or "what needs me". A question about you, a chat, thanks, a yes or no, or a plan for later ` +
	`gets no tool: just answer it. Never call a tool because of the context line. ` +
	`Pass names as the owner said them; never make up a name, a path or a command. Up to three tools for one ` +
	`request. A tool result is said to the owner as it is: after a tool, say nothing more. ` +
	`A Mac app the owner wants to look at goes to read_app, one to open goes to open_app (never for a Herdr space or agent). ` +
	`A website the owner names (\"what's on hacker news\", \"check the go blog\") goes to browse, with their question. ` +
	`A question of fact about the world (a person, a place, a thing, an event, a definition) goes to web_search. A sum, ` +
	`a conversion or a guess about the future gets no tool: say in one sentence that you'd only be guessing. A question the context line answers (the time, the layout, how many agents work or wait) gets ` +
	`no tool: answer it from the context line.`

// Call is one tool call from the brain (or a fast intent).
type Call struct {
	ID   string
	Name string
	Args map[string]any
}

// ParseCall reads a call's arguments (a JSON object; the brain may give numbers as strings).
func ParseCall(id, name, arguments string) (Call, error) {
	c := Call{ID: id, Name: name, Args: map[string]any{}}
	a := strings.TrimSpace(arguments)
	if a == "" || a == "null" {
		return c, nil
	}
	if err := json.Unmarshal([]byte(a), &c.Args); err != nil {
		return c, fmt.Errorf("the arguments aren't a JSON object")
	}
	return c, nil
}

// Str is a string argument, trimmed ("" when missing).
func (c Call) Str(k string) string {
	switch v := c.Args[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// Bool is a true/false argument ("true", "show", "on" count as true).
func (c Call) Bool(k string) (bool, bool) {
	switch v := c.Args[k].(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "show", "on", "open":
			return true, true
		case "false", "no", "hide", "off", "close":
			return false, true
		}
	}
	return false, false
}

// Int is a whole-number argument (a number, or a string of digits or number words); ok false when missing.
func (c Call) Int(k string) (int, bool) {
	switch v := c.Args[k].(type) {
	case float64:
		if v == math.Trunc(v) && math.Abs(v) < 1e6 {
			return int(v), true
		}
	case string:
		return Number(v)
	}
	return 0, false
}

// Known says whether name is in the catalogue.
func Known(name string) bool {
	for _, t := range Catalogue() {
		if t.Name == name {
			return true
		}
	}
	return false
}
