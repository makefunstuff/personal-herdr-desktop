package voice

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

// pipeline is the openai backend's session: vcd keeps the chat history, each response.create is one
// streamed POST /v1/chat/completions with the intents as tools, and each sentence of the answer is
// spoken through the TTS endpoint as it comes. It makes the same events a realtime call sends.
type pipeline struct {
	c      *Client
	cfg    SessionConfig
	model  string
	tools  []any
	events chan Event

	mu     sync.Mutex
	msgs   []map[string]any
	cancel context.CancelFunc
	n      int
	closed bool
	done   chan struct{}
	wg     sync.WaitGroup
}

func (c *Client) dialPipeline(cfg SessionConfig) (*Session, error) {
	p := &pipeline{c: c, cfg: cfg, model: c.Chat.Model, done: make(chan struct{})}
	// Realtime-style tools ({type, name, description, parameters}) in chat's shape.
	var tools []map[string]any
	b, _ := json.Marshal(cfg.Tools)
	json.Unmarshal(b, &tools)
	for _, t := range tools {
		p.tools = append(p.tools, map[string]any{"type": "function", "function": map[string]any{
			"name": t["name"], "description": t["description"], "parameters": t["parameters"]}})
	}
	s := &Session{pipe: p, Events: make(chan Event, 256)}
	p.events = s.Events
	return s, nil
}

func (p *pipeline) emit(ev Event) bool {
	select {
	case <-p.done:
		return false
	default:
	}
	select {
	case p.events <- ev:
		return true
	case <-p.done:
		return false
	}
}

func (p *pipeline) close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	if p.cancel != nil {
		p.cancel()
	}
	close(p.done)
	p.mu.Unlock()
	go func() { p.wg.Wait(); close(p.events) }()
}

// handle takes what Session would send a realtime server.
func (p *pipeline) handle(v map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("session closed")
	}
	switch v["type"] {
	case "conversation.item.create":
		item, _ := v["item"].(map[string]any)
		switch item["type"] {
		case "message":
			role, _ := item["role"].(string)
			content, _ := item["content"].([]any)
			var parts []string
			for _, x := range content {
				if m, ok := x.(map[string]any); ok {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
			p.add(role, strings.Join(parts, "\n"))
		case "function_call_output":
			p.msgs = append(p.msgs, map[string]any{"role": "tool", "tool_call_id": item["call_id"], "content": item["output"]})
		}
	case "response.create":
		if p.cancel != nil {
			p.cancel()
		}
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		p.n++
		p.trim()
		// A copy of each message: add() changes the last one in place while this one is sent.
		msgs := []map[string]any{{"role": "system", "content": p.cfg.Instructions}}
		for _, m := range p.msgs {
			c := make(map[string]any, len(m))
			for k, v := range m {
				c[k] = v
			}
			msgs = append(msgs, c)
		}
		p.wg.Add(1)
		go p.respond(ctx, fmt.Sprintf("resp_%d", p.n), msgs)
	case "response.cancel":
		if p.cancel != nil {
			p.cancel()
		}
	}
	return nil
}

// maxMessages bounds a session's history (the context line, words, answers, tool calls and
// outputs: about 25 turns), so a long session never outgrows the server's context.
const maxMessages = 100

// trim drops the oldest messages beyond maxMessages, from a user message on, so a tool call is never
// kept without its call or its output.
func (p *pipeline) trim() {
	if len(p.msgs) > maxMessages {
		cut := len(p.msgs) - maxMessages
		for cut < len(p.msgs) && p.msgs[cut]["role"] != "user" {
			cut++
		}
		p.msgs = append([]map[string]any{}, p.msgs[cut:]...)
	}
	// Within the token budget (about 4 characters a token): the oldest whole turns go first, the newest
	// is always kept.
	for p.cfg.ContextTokens > 0 && msgTokens(p.msgs) > p.cfg.ContextTokens {
		cut := 1
		for cut < len(p.msgs) && p.msgs[cut]["role"] != "user" {
			cut++
		}
		if cut >= len(p.msgs) {
			break
		}
		p.msgs = append([]map[string]any{}, p.msgs[cut:]...)
	}
}

// msgTokens estimates the messages' tokens: about four characters each.
func msgTokens(msgs []map[string]any) int {
	n := 0
	for _, m := range msgs {
		b, _ := json.Marshal(m)
		n += len(b) / 4
	}
	return n
}

// add appends a message; two user messages in a row become one (some chat templates refuse them).
func (p *pipeline) add(role, text string) {
	if n := len(p.msgs); n > 0 && role == "user" && p.msgs[n-1]["role"] == "user" {
		if prev, ok := p.msgs[n-1]["content"].(string); ok {
			p.msgs[n-1]["content"] = prev + "\n" + text
			return
		}
	}
	p.msgs = append(p.msgs, map[string]any{"role": role, "content": text})
}

type chunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

type call struct {
	id, name, args string
	index          int
}

func (p *pipeline) respond(ctx context.Context, id string, msgs []map[string]any) {
	defer p.wg.Done()
	body := map[string]any{"model": p.model, "messages": msgs, "stream": true}
	if p.cfg.MaxOutputTokens > 0 {
		body["max_tokens"] = p.cfg.MaxOutputTokens
	}
	if len(p.tools) > 0 {
		body["tools"] = p.tools
	}
	for k, v := range p.cfg.Extra {
		body[k] = v
	}
	data, _ := json.Marshal(body)
	resp, err := p.c.ChatDo(ctx, "/v1/chat/completions", data, "application/json")
	if err != nil {
		if ctx.Err() == nil {
			p.fail(err)
		}
		return
	}
	defer resp.Body.Close()
	if !p.emit(Event{Type: "response.created", Response: &ResponseInfo{ID: id}}) {
		return
	}

	// Sentences are spoken in order while the rest of the answer streams in.
	sentences := make(chan string, 64)
	spoken := make(chan struct{})
	go func() {
		defer close(spoken)
		for s := range sentences {
			if ctx.Err() == nil {
				p.speak(ctx, s)
			}
		}
	}()
	var raw, said strings.Builder
	sent := 0 // bytes of the visible text already sent as sentences
	nSaid := 0
	calls := map[int]*call{}
	last, next := 0, 0 // the slot written last; how far ids without their own index moved on
	streamErr := error(nil)
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	flush := func(final bool) {
		vis := visible(raw.String(), final)
		for sent < len(vis) {
			rest := vis[sent:]
			n := sentenceEnd(rest, final)
			if n == 0 {
				return
			}
			s := strings.TrimSpace(rest[:n])
			sent += n
			if p.cfg.Rewrite != nil {
				s = p.cfg.Rewrite(s)
			}
			if s == "" {
				continue
			}
			if said.Len() > 0 {
				said.WriteString(" ")
			}
			said.WriteString(s)
			p.emit(Event{Type: "response.output_audio_transcript.delta", Delta: s})
			sentences <- s
			nSaid++
		}
	}
	var alive time.Time
	for sc.Scan() && ctx.Err() == nil {
		// While the model reasons (nothing to say yet), a progress event a second keeps vcd's watchdog
		// from taking the quiet for a stuck server.
		if time.Since(alive) > time.Second {
			alive = time.Now()
			p.emit(Event{Type: "response.progress"})
		}
		line, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "[DONE]" {
			break
		}
		var ch chunk
		if json.Unmarshal([]byte(line), &ch) != nil || len(ch.Choices) == 0 {
			continue
		}
		dl := ch.Choices[0].Delta
		for _, tc := range dl.ToolCalls {
			// By index; a new id at a used index (a server that leaves index out) is a new call.
			if c := calls[last]; c != nil && tc.ID != "" && c.id != "" && tc.ID != c.id && tc.Index == c.index {
				next++
			}
			slot := tc.Index + next
			last = slot
			c := calls[slot]
			if c == nil {
				c = &call{index: tc.Index}
				calls[slot] = c
			}
			if tc.ID != "" {
				c.id = tc.ID
			}
			c.name += tc.Function.Name
			c.args += tc.Function.Arguments
		}
		if dl.Content != "" {
			raw.WriteString(dl.Content)
			flush(false)
			if p.cfg.MaxSentences > 0 && nSaid >= p.cfg.MaxSentences && len(calls) == 0 {
				raw.Reset() // enough said: the rest is neither spoken nor kept
				sent = 0
				break
			}
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		streamErr = err
	}
	if ctx.Err() == nil {
		flush(true)
	}
	var idx []int
	for i := range calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	var toolCalls []any
	for n, i := range idx {
		c := calls[i]
		if c.id == "" {
			c.id = fmt.Sprintf("call_%s_%d", id, n)
		}
		if c.args == "" {
			c.args = "{}"
		}
		if ctx.Err() == nil {
			p.emit(Event{Type: "response.function_call_arguments.done", CallID: c.id, Name: c.name, Arguments: c.args})
			toolCalls = append(toolCalls, map[string]any{"id": c.id, "type": "function",
				"function": map[string]any{"name": c.name, "arguments": c.args}})
		}
	}
	close(sentences)
	<-spoken
	if ctx.Err() != nil {
		// Stopped while its sentences were still being spoken: vcd answers none of its calls, so
		// they don't go in the history (a call with no output is refused next time).
		toolCalls = nil
	}

	status := "completed"
	switch {
	case ctx.Err() != nil:
		status = "cancelled" // its tool calls are dropped: nothing would answer them
	case streamErr != nil:
		status = "failed"
		p.fail(streamErr)
	}
	// What was said (and called) goes into the history before response.done, so the tool outputs that
	// answer it come after it.
	p.mu.Lock()
	if said.Len() > 0 || len(toolCalls) > 0 {
		m := map[string]any{"role": "assistant", "content": said.String()}
		if len(toolCalls) > 0 {
			m["tool_calls"] = toolCalls
		}
		p.msgs = append(p.msgs, m)
	}
	p.mu.Unlock()
	p.emit(Event{Type: "response.done", Response: &ResponseInfo{ID: id, Status: status}})
}

func (p *pipeline) fail(err error) {
	ev := Event{Type: "error"}
	ev.Error = &struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}{Type: "server_error", Message: "The brain failed: " + err.Error()}
	if errors.Is(err, ErrRefused) {
		ev.Error.Message = "The chat endpoint refused the key"
	}
	p.emit(ev)
}

// speak sends one sentence to the TTS endpoint and its audio on as output_audio deltas.
func (p *pipeline) speak(ctx context.Context, text string) {
	body, err := p.c.Speech(ctx, text, p.cfg.Voice)
	if err != nil {
		if ctx.Err() == nil {
			ev := Event{Type: "error"}
			ev.Error = &struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}{Type: "server_error", Message: "Speech failed: " + err.Error()}
			p.emit(ev)
		}
		return
	}
	defer body.Close()
	defer func() { // a breath between sentences
		if ctx.Err() == nil {
			p.emit(Event{Type: "response.output_audio.delta", pcm: make([]byte, 24000*2*180/1000)})
		}
	}()
	buf := make([]byte, 9600)
	var carry []byte
	for ctx.Err() == nil {
		n, err := body.Read(buf)
		if n > 0 {
			data := append(carry, buf[:n]...)
			cut := len(data) &^ 1
			if cut > 0 && !p.emit(Event{Type: "response.output_audio.delta", pcm: append([]byte{}, data[:cut]...)}) {
				return
			}
			carry = append([]byte{}, data[cut:]...)
		}
		if err != nil {
			if err != io.EOF && ctx.Err() == nil {
				return
			}
			return
		}
	}
}

// visible is the answer without its reasoning: <think>…</think> blocks go, and so does an open one or a
// tag still coming in at the end (unless final).
func visible(s string, final bool) string {
	var out strings.Builder
	for {
		i := strings.Index(s, "<think>")
		if i < 0 {
			break
		}
		out.WriteString(s[:i])
		j := strings.Index(s[i:], "</think>")
		if j < 0 {
			return strings.ReplaceAll(out.String(), "</think>", " ")
		}
		s = s[i+j+len("</think>"):]
	}
	if !final {
		for k := min(len(s), len("<think>")-1); k > 0; k-- {
			if strings.HasPrefix("<think>", s[len(s)-k:]) {
				s = s[:len(s)-k]
				break
			}
		}
	}
	out.WriteString(s)
	return strings.ReplaceAll(out.String(), "</think>", " ")
}

// sentenceEnd is how much of s makes a sentence to speak: up to a . ! ? or line break with space after
// it, at least 16 bytes in (so "Dr. " or "1. " doesn't make a sentence on its own); all of s when final.
func sentenceEnd(s string, final bool) int {
	for i := 0; i < len(s)-1; i++ {
		c := s[i]
		if c == '\n' && strings.TrimSpace(s[:i]) != "" {
			return i + 1
		}
		if (c == '.' || c == '!' || c == '?') && (s[i+1] == ' ' || s[i+1] == '\n') && i >= 15 {
			return i + 1
		}
	}
	if final {
		return len(s)
	}
	return 0
}

// Warm sends the session's instructions and tools once, so the server's prompt cache holds them and
// the first real turn doesn't wait for them (one token back, thrown away).
func (c *Client) Warm(ctx context.Context, cfg SessionConfig) error {
	s, _ := c.dialPipeline(cfg)
	p := s.pipe
	body := map[string]any{"model": p.model, "stream": false, "max_tokens": 1, "messages": []map[string]any{
		{"role": "system", "content": cfg.Instructions}, {"role": "user", "content": "(warming up)"}}}
	if len(p.tools) > 0 {
		body["tools"] = p.tools
	}
	for k, v := range cfg.Extra {
		body[k] = v
	}
	data, _ := json.Marshal(body)
	_, err := c.Post(ctx, c.Chat, "/v1/chat/completions", bytes.NewReader(data))
	return err
}
