package voice

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
)

// Event is one session event (the realtime shape the pipeline makes up), with the fields vcd reads.
type Event struct {
	Type       string `json:"type"`
	Delta      string `json:"delta"`
	Transcript string `json:"transcript"`
	// response.function_call_arguments.done: one tool call.
	CallID    string        `json:"call_id"`
	Name      string        `json:"name"`
	Arguments string        `json:"arguments"`
	Response  *ResponseInfo `json:"response"`
	Error     *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`

	pcm []byte // the pipeline's audio, not base64'd
}

type ResponseInfo struct {
	ID      string             `json:"id"`
	Status  string             `json:"status"`
	Timings map[string]float64 `json:"x_timings"`
}

// Audio decodes an output_audio delta: PCM16 at 24 kHz.
func (e Event) Audio() ([]byte, error) {
	if e.pcm != nil {
		return e.pcm, nil
	}
	return base64.StdEncoding.DecodeString(e.Delta)
}

// SessionConfig is what vcd sets on a computer session. Extra's keys go into every chat request.
type SessionConfig struct {
	Instructions    string `json:"instructions"`
	Voice           string `json:"voice"`
	TurnDetection   any    `json:"turn_detection"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	// MaxSentences cuts a spoken answer after this many sentences (0: no cut): a small model may go on.
	MaxSentences int `json:"-"`
	// Rewrite changes each spoken sentence ("" drops it): no robotic tics.
	Rewrite func(string) string `json:"-"`
	// ContextTokens bounds the history sent each turn (the newest turns are kept).
	ContextTokens int            `json:"-"`
	Tools         any            `json:"tools,omitempty"`
	Extra         map[string]any `json:"-"`
}

// Session is one computer session: a chat history vcd keeps in memory. Events arrive on Events until
// it is closed.
type Session struct {
	pipe   *pipeline
	Events chan Event
	mu     sync.Mutex
	closed bool
}

// Dial opens a session.
func (c *Client) Dial(ctx context.Context, cfg SessionConfig) (*Session, error) {
	return c.dialPipeline(cfg)
}

// Model is the model answering.
func (s *Session) Model() string { return s.pipe.model }

func (s *Session) send(ctx context.Context, v map[string]any) error { return s.pipe.handle(v) }

// Say adds a text message (role user or assistant) to the session's history.
func (s *Session) Say(ctx context.Context, role, text string) error {
	return s.send(ctx, map[string]any{"type": "conversation.item.create", "item": map[string]any{
		"type": "message", "role": role, "content": []any{map[string]any{"type": "input_text", "text": text}},
	}})
}

// Output answers a tool call (its result goes into the brain's history).
func (s *Session) Output(ctx context.Context, callID, output string) error {
	return s.send(ctx, map[string]any{"type": "conversation.item.create", "item": map[string]any{
		"type": "function_call_output", "call_id": callID, "output": output,
	}})
}

// Respond asks the brain to answer what is in the history.
func (s *Session) Respond(ctx context.Context) error {
	return s.send(ctx, map[string]any{"type": "response.create"})
}

// Cancel stops the answer being spoken.
func (s *Session) Cancel(ctx context.Context) error {
	return s.send(ctx, map[string]any{"type": "response.cancel"})
}

func (s *Session) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.pipe.close()
}

// Messages is a copy of the session's history (for tests).
func (s *Session) Messages() []map[string]any {
	s.pipe.mu.Lock()
	defer s.pipe.mu.Unlock()
	b, _ := json.Marshal(s.pipe.msgs)
	var out []map[string]any
	json.Unmarshal(b, &out)
	return out
}
