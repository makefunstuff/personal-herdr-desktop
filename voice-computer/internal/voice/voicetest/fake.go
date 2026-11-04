// Package voicetest is a fake backend for tests: the transcription, chat and speech routes vcd uses,
// on one server.
package voicetest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

type Fake struct {
	*httptest.Server
	Key string

	mu sync.Mutex
	// What the next transcriptions return, in order (then Transcript).
	Transcripts []string
	Transcript  string
	// The answer's sentences; each gets AudioPerSentence bytes of PCM.
	Answer           []string
	AudioPerSentence int
	// Delay before each sentence, so a test can press during an answer.
	SentenceDelay time.Duration
	SensesUp      []string // /health's senses_up
	Model         string   // brain_effective's model
	STTModel      string   // the model a transcription must name ("": any)
	// ModelList is /v1/models' data (nil: just Model).
	ModelList     func() []any
	ErrorOnCreate bool          // answer response.create with only an error, as a refused brain does
	SpeechDelay   time.Duration // before /v1/audio/speech answers
	// Tool calls for the next responses, in order: each response.create takes the first list (an empty
	// one answers with Answer), then Answer once they run out.
	ToolCalls [][]ToolCall
	Speeches  []Speech // what /v1/audio/speech was asked to say

	// What it saw.
	Transcribed int
	Sessions    []*Call
	Chats       []map[string]any // each /v1/chat/completions request
	Spoken      []string         // each /v1/audio/speech input
}

// ToolCall is one function call a response makes.
type ToolCall struct {
	Name, Args string
}

// Speech is one /v1/audio/speech request.
type Speech struct{ Input, Voice string }

// Call is one realtime session as the fake saw it.
type Call struct {
	Session map[string]any
	Items   []map[string]any // conversation.item.create items
	Cancels int
	Creates int
}

func New() *Fake {
	f := &Fake{Key: "test-key", Transcript: "what is twelve times seven", Answer: []string{"Eighty-four."},
		AudioPerSentence: 4800, Model: "test-brain"}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

func (f *Fake) Lock()   { f.mu.Lock() }
func (f *Fake) Unlock() { f.mu.Unlock() }

func (f *Fake) authed(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+f.Key {
		http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health":
		f.mu.Lock()
		up := f.SensesUp
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "senses_up": up, "realtime_sessions": 0})
	case "/v1/models":
		if !f.authed(w, r) {
			return
		}
		f.mu.Lock()
		list, model := f.ModelList, f.Model
		f.mu.Unlock()
		data := []any{map[string]any{"id": model, "object": "model"}}
		if list != nil {
			data = list()
		}
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	case "/v1/audio/transcriptions":
		if !f.authed(w, r) {
			return
		}
		f.mu.Lock()
		wantModel := f.STTModel
		f.mu.Unlock()
		if err := r.ParseMultipartForm(32 << 20); err != nil || (wantModel != "" && r.FormValue("model") != wantModel) {
			http.Error(w, "bad form", 400)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file", 400)
			return
		}
		head := make([]byte, 4)
		io.ReadFull(file, head)
		if string(head) != "RIFF" {
			http.Error(w, "not a wav", 400)
			return
		}
		f.mu.Lock()
		f.Transcribed++
		text := f.Transcript
		if len(f.Transcripts) > 0 {
			text, f.Transcripts = f.Transcripts[0], f.Transcripts[1:]
		}
		f.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"text": text})
	case "/tts":
		if !f.authed(w, r) {
			return
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		sp := Speech{}
		sp.Input, _ = req["input"].(string)
		sp.Voice, _ = req["voice"].(string)
		f.mu.Lock()
		delay := f.SpeechDelay
		f.Speeches = append(f.Speeches, sp)
		f.Spoken = append(f.Spoken, sp.Input)
		f.mu.Unlock()
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		w.Write(voiceWAV(4800))
	case "/v1/chat/completions":
		if !f.authed(w, r) {
			return
		}
		f.chat(w, r)
	default:
		http.NotFound(w, r)
	}
}

// chat answers a streamed chat completion as an OpenAI-compatible server does: a reasoning block, then
// Answer's sentences (or the next ToolCalls) as deltas.
func (f *Fake) chat(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	json.NewDecoder(r.Body).Decode(&req)
	f.mu.Lock()
	f.Chats = append(f.Chats, req)
	refuse := f.ErrorOnCreate
	answer, delay := f.Answer, f.SentenceDelay
	var calls []ToolCall
	stream, _ := req["stream"].(bool)
	if len(f.ToolCalls) > 0 && stream {
		calls, f.ToolCalls = f.ToolCalls[0], f.ToolCalls[1:]
	}
	if len(calls) > 0 {
		answer = nil
	}
	f.mu.Unlock()
	if refuse {
		http.Error(w, `{"error":{"message":"brain refused"}}`, http.StatusBadRequest)
		return
	}
	if stream, _ := req["stream"].(bool); !stream { // a plain request (a warm-up, a summary): one JSON answer
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"index": 0,
			"message": map[string]any{"role": "assistant", "content": strings.Join(answer, " ")}}}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	delta := func(d map[string]any) {
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": d}}})
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	delta(map[string]any{"role": "assistant", "content": "<think>The owner asks"})
	delta(map[string]any{"content": " something.</think>"})
	for _, s := range answer {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		delta(map[string]any{"content": s + " "})
	}
	for i, tc := range calls {
		delta(map[string]any{"tool_calls": []any{map[string]any{"index": i, "id": fmt.Sprintf("call_%d", i),
			"type": "function", "function": map[string]any{"name": tc.Name, "arguments": ""}}}})
		delta(map[string]any{"tool_calls": []any{map[string]any{"index": i, "function": map[string]any{"arguments": tc.Args}}}})
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

// Texts is the text of each item a call got, as "role: text".
func (c *Call) Texts() []string {
	var out []string
	for _, it := range c.Items {
		content, _ := it["content"].([]any)
		for _, x := range content {
			m, _ := x.(map[string]any)
			out = append(out, fmt.Sprint(it["role"], ": ", m["text"]))
		}
	}
	return out
}

// Join is Texts on one line, for test messages.
func (c *Call) Join() string { return strings.Join(c.Texts(), " | ") }

// Outputs is each function_call_output a call got, as "call_id: output".
func (c *Call) Outputs() []string {
	var out []string
	for _, it := range c.Items {
		if it["type"] == "function_call_output" {
			out = append(out, fmt.Sprint(it["call_id"], ": ", it["output"]))
		}
	}
	return out
}

// voiceWAV is a streamed WAV of n bytes of silence, as llama-server's /tts sends (sizes unknown).
func voiceWAV(n int) []byte {
	h := []byte("RIFF\xff\xff\xff\xffWAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\xc0\x5d\x00\x00\x80\xbb\x00\x00\x02\x00\x10\x00data\xff\xff\xff\xff")
	return append(h, make([]byte, n)...)
}
