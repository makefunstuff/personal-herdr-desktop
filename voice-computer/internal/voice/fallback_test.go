package voice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"herdr-voice-computer/internal/config"
)

func TestChatFallsBackWithItsModel(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "busy", 503) }))
	defer down.Close()
	var got string
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &m)
		got, _ = m["model"].(string)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer local.Close()
	c := &Client{Chat: config.Endpoint{URL: down.URL, Model: "cloud"}, ChatFallback: config.Endpoint{URL: local.URL, Model: "lfm3"}}
	out, err := c.Post(context.Background(), c.Chat, "/v1/chat/completions", strings.NewReader(`{"model":"cloud","messages":[]}`))
	if err != nil || string(out) != `{"ok":true}` || got != "lfm3" {
		t.Fatalf("out %q err %v model %q", out, err, got)
	}
}
