package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-voice-computer/internal/config"
)

func TestVisible(t *testing.T) {
	for _, c := range []struct {
		in    string
		final bool
		want  string
	}{
		{"<think>hmm</think>Hello.", false, "Hello."},
		{"Hi <think>open", false, "Hi "},
		{"Hi <thi", false, "Hi "},
		{"Hi <thi", true, "Hi <thi"},
		{"stray</think> end", true, "stray  end"},
	} {
		if got := visible(c.in, c.final); got != c.want {
			t.Errorf("visible(%q, %v) = %q, want %q", c.in, c.final, got, c.want)
		}
	}
}

func TestSentenceEnd(t *testing.T) {
	for _, c := range []struct {
		in    string
		final bool
		want  int
	}{
		{"Dr. Smith is here. And", false, 18},
		{"Short. Next", false, 0},
		{"no end yet", false, 0},
		{"no end yet", true, 10},
		{"line one\nline two", false, 9},
	} {
		if got := sentenceEnd(c.in, c.final); got != c.want {
			t.Errorf("sentenceEnd(%q, %v) = %d, want %d", c.in, c.final, got, c.want)
		}
	}
}

// A stop while the answer is still being spoken drops its tool calls from the history: vcd answers
// none of them, and a call with no output is refused next time.
func TestCancelWhileSpeakingDropsToolCalls(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			mu.Lock()
			bodies = append(bodies, b)
			mu.Unlock()
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"A sentence that is long enough. "}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"volume","arguments":"{}"}}]}}]}`+"\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
		case "/v1/audio/speech":
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
			}
		}
	}))
	defer srv.Close()
	ep := config.Endpoint{URL: srv.URL}
	c := &Client{Backend: "openai", STT: ep, Chat: ep, TTS: ep}
	s, _ := c.Dial(context.Background(), SessionConfig{Instructions: "x"})
	defer s.Close()
	ctx := context.Background()
	s.Say(ctx, "user", "louder")
	s.Respond(ctx)
	wait := func(typ string) {
		for ev := range s.Events {
			if ev.Type == typ {
				return
			}
		}
		t.Fatalf("no %s", typ)
	}
	wait("response.function_call_arguments.done")
	s.Cancel(ctx)
	wait("response.done")
	s.Say(ctx, "user", "never mind")
	s.Respond(ctx)
	wait("response.created")
	mu.Lock()
	b, _ := json.Marshal(bodies[1]["messages"])
	mu.Unlock()
	if strings.Contains(string(b), "tool_calls") {
		t.Fatalf("a cancelled call stayed in the history: %s", b)
	}
}

func TestTrimKeepsWholeTurns(t *testing.T) {
	p := &pipeline{}
	for i := 0; i < 60; i++ {
		p.msgs = append(p.msgs, map[string]any{"role": "user"}, map[string]any{"role": "assistant", "tool_calls": 1},
			map[string]any{"role": "tool"})
	}
	p.trim()
	if len(p.msgs) > maxMessages || p.msgs[0]["role"] != "user" {
		t.Fatalf("%d messages, first %v", len(p.msgs), p.msgs[0])
	}
}

// Two calls from a server that leaves index out stay two calls; indexed ones stream into their own.
func TestToolCallsWithAndWithoutIndex(t *testing.T) {
	for name, stream := range map[string][]string{
		"no index": {
			`{"choices":[{"delta":{"tool_calls":[{"id":"a","function":{"name":"volume","arguments":"{\"action\":\"up\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"id":"b","function":{"name":"media","arguments":"{\"action\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"\"pause\"}"}}]}}]}`,
		},
		"indexed": {
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","function":{"name":"volume","arguments":""}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"media","arguments":"{\"action\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"action\":\"up\"}"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"pause\"}"}}]}}]}`,
		},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, l := range stream {
				fmt.Fprintf(w, "data: %s\n\n", l)
			}
			fmt.Fprint(w, "data: [DONE]\n\n")
		}))
		ep := config.Endpoint{URL: srv.URL}
		c := &Client{Backend: "openai", STT: ep, Chat: ep, TTS: ep}
		s, _ := c.Dial(context.Background(), SessionConfig{})
		s.Say(context.Background(), "user", "louder and pause")
		s.Respond(context.Background())
		var got []string
		for ev := range s.Events {
			if ev.Type == "response.function_call_arguments.done" {
				got = append(got, ev.CallID+" "+ev.Name+" "+ev.Arguments)
			}
			if ev.Type == "response.done" {
				break
			}
		}
		s.Close()
		srv.Close()
		if len(got) != 2 || got[0] != `a volume {"action":"up"}` || got[1] != `b media {"action":"pause"}` {
			t.Errorf("%s: %q", name, got)
		}
	}
}
