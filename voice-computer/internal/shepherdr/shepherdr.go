// Package shepherdr reads a Shepherdr's API (http://127.0.0.1:7787/api on its own machine): its
// snapshot (places, tasks, what needs the owner) and coordinator chats. This Mac's is fetched on loopback;
// another machine's the way Herdr's shepherdr mod does, with curl run there through herdr-core
// (`hc exec --on <machine>`). It writes only what the owner confirmed (a send).
package shepherdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
)

// Client is one Shepherdr.
type Client struct {
	Name    string // how vcd says it ("this Mac")
	URL     string // its base URL on its own machine, without /api
	Machine string // the herdr-core machine to read it through ("": here)
	HC      string // the hc command
	HTTP    *http.Client
	// Exec runs a command (tests swap it).
	Exec func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Need is something waiting on the owner.
type Need struct {
	Chat     string    `json:"chat"`
	Task     string    `json:"task"`
	Text     string    `json:"text"`
	Question *Question `json:"question"`
	Secret   *struct {
		Name string `json:"name"`
	} `json:"secret"`
}

// What is what the need asks, in a few words.
func (n Need) What() string {
	switch {
	case n.Question != nil && n.Question.Title != "":
		return n.Question.Title
	case n.Secret != nil:
		return "a secret, " + n.Secret.Name
	}
	return n.Text
}

type Place struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // idle, work, need, …
	Chat   string `json:"chat"`
}

type Task struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Place  string `json:"place"`
	Status string `json:"status"`
	Agent  *struct {
		Chat string `json:"chat"`
	} `json:"agent"`
	Archived any `json:"archived"` // false, or when (a number)
	// Report is what the task's agent last said of its progress.
	Report *struct {
		Progress any    `json:"progress"` // a number, or null
		ETA      any    `json:"eta"`
		Msg      string `json:"msg"`
		Paused   any    `json:"paused"`
	} `json:"report"`
}

// Snapshot is the part of /api/snapshot vcd reads.
type Snapshot struct {
	Places []Place `json:"places"`
	Tasks  []Task  `json:"tasks"`
	Needs  []Need  `json:"needs"`
	// Asks are the open questions agents put to the owner: chat key → question id.
	Asks map[string]string `json:"asks"`
}

// PlaceOf is the place a chat belongs to (a coordinator's, or a task agent's), and the task.
func (s Snapshot) PlaceOf(chat string) (Place, *Task) {
	for _, p := range s.Places {
		if p.Chat == chat {
			return p, nil
		}
	}
	for i, t := range s.Tasks {
		if t.Agent != nil && t.Agent.Chat == chat {
			for _, p := range s.Places {
				if p.ID == t.Place {
					return p, &s.Tasks[i]
				}
			}
		}
	}
	return Place{}, nil
}

// Space is one coordinator (a place) as agent replies watch it.
type Space struct {
	Space  string `json:"space"`
	ID     string `json:"id"` // the chat key
	Status string `json:"status"`
}

// Message is one chat message.
type Message struct {
	ID       string          `json:"id"`
	Who      string          `json:"who"` // you or agent
	At       float64         `json:"at"`  // unix ms
	Text     string          `json:"text"`
	Card     json.RawMessage `json:"card"` // Shepherdr's own update card (news)
	Question *Question       `json:"question"`
}

// IsCard says whether the message is one of Shepherdr's cards, not words.
func (m Message) IsCard() bool {
	return len(m.Card) > 0 && string(m.Card) != "null" && string(m.Card) != `""`
}

type Question struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
}

func (c *Client) exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.Exec != nil {
		return c.Exec(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).Output()
}

// call asks the API: GET when body is nil, else POST it as JSON.
func (c *Client) call(ctx context.Context, path string, body any, out any) error {
	u := strings.TrimRight(c.URL, "/") + "/api/" + path
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	if c.Machine == "" {
		method := "GET"
		if body != nil {
			method = "POST"
		}
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(data))
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("content-type", "application/json")
		}
		hc := c.HTTP
		if hc == nil {
			hc = http.DefaultClient
		}
		resp, err := hc.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		return decode(resp.StatusCode, b, out)
	}
	hc := c.HC
	if hc == "" {
		hc = "hc"
	}
	args := []string{"exec", "--on", c.Machine, "curl", "-sS", "-m", "20", "-w", "\n%{http_code}"}
	if body != nil { // only a send the owner confirmed, as Herdr's shepherdr mod sends
		args = append(args, "-X", "POST", "-H", "content-type: application/json", "--data-binary", string(data))
	}
	b, err := c.exec(ctx, hc, append(args, u)...)
	if err != nil {
		return fmt.Errorf("%s: %v", c.Name, err)
	}
	s := string(b)
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		return fmt.Errorf("%s: no answer", c.Name)
	}
	code, _ := strconv.Atoi(strings.TrimSpace(s[i+1:]))
	return decode(code, []byte(s[:i]), out)
}

func decode(code int, b []byte, out any) error {
	var e struct {
		Error string `json:"error"`
	}
	json.Unmarshal(b, &e)
	if code >= 400 || code == 0 || e.Error != "" {
		if e.Error == "" {
			e.Error = fmt.Sprintf("HTTP %d", code)
		}
		return errors.New(e.Error)
	}
	return json.Unmarshal(b, out)
}

// Snapshot reads /api/snapshot.
func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var s Snapshot
	err := c.call(ctx, "snapshot", nil, &s)
	return s, err
}

// Spaces lists the coordinators (places) and their status, for agent replies.
func (c *Client) Spaces(ctx context.Context) ([]Space, error) {
	s, err := c.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	var out []Space
	for _, p := range s.Places {
		out = append(out, Space{Space: p.Name, ID: p.Chat, Status: p.Status})
	}
	return out, nil
}

// Chat is a chat's latest limit messages, oldest first.
func (c *Client) Chat(ctx context.Context, chat string, limit int) ([]Message, error) {
	var out struct {
		Chat struct {
			Messages []Message `json:"messages"`
			Working  bool      `json:"working"`
		} `json:"chat"`
	}
	if err := c.call(ctx, "chat?chat="+url.QueryEscape(chat), nil, &out); err != nil {
		return nil, err
	}
	m := out.Chat.Messages
	if len(m) > limit {
		m = m[len(m)-limit:]
	}
	return m, nil
}

// Send writes the owner's words into a chat (a coordinator's or a task agent's): the coordinator wakes
// on it. vcd sends only what the owner confirmed.
func (c *Client) Send(ctx context.Context, chat, text, cid string) error {
	var out map[string]any
	return c.call(ctx, "action", map[string]any{"type": "send", "chat": chat, "text": text, "cid": cid, "human": true}, &out)
}

// Num is a JSON value as a number, when it is one.
func Num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	}
	return 0, false
}
