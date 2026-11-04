package index

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEmbed embeds by keywords: one dimension per word of a small vocabulary.
func fakeEmbed(t *testing.T) (*Embedder, *int) {
	vocab := []string{"milk", "demo", "friday", "anna", "canberra", "australia", "grep", "homelab"}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Input []string `json:"input"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var data []map[string]any
		for i, in := range req.Input {
			v := make([]float32, len(vocab)+1)
			v[len(vocab)] = 0.05
			for j, w := range vocab {
				if strings.Contains(strings.ToLower(in), w) {
					v[j] = 1
				}
			}
			data = append(data, map[string]any{"index": i, "embedding": v})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return &Embedder{URL: srv.URL, Model: "embed"}, &calls
}

func TestSyncSearchAndWebCache(t *testing.T) {
	e, calls := fakeEmbed(t)
	dir := t.TempDir()
	day := filepath.Join(dir, "daily", "2026-11-03.md")
	os.MkdirAll(filepath.Dir(day), 0o755)
	os.WriteFile(day, []byte("# 2026-11-03\n\n- 09:00 buy milk\n- [ ] 10:00 call Anna\n- 11:00 the demo is on Friday\n"), 0o644)
	mem := filepath.Join(dir, "memory.md")
	os.WriteFile(mem, []byte("# What the voice computer remembers\n\n## About Alex\n\n- Alex's main project is homelab.\n"), 0o600)
	ix := Open(filepath.Join(dir, "index.json"), e)
	n, err := ix.Sync(context.Background(), []string{day, mem}, mem)
	if err != nil || n != 4 || ix.Len() != 4 {
		t.Fatalf("sync %d %v (%d)", n, err, ix.Len())
	}
	before := *calls
	if n, _ := ix.Sync(context.Background(), []string{day, mem}, mem); n != 0 || *calls != before {
		t.Fatalf("an unchanged file was embedded again")
	}
	hits, err := ix.Search(context.Background(), "when is the demo", 3, 0.5)
	if err != nil || len(hits) != 1 || hits[0].Text != "11:00 the demo is on Friday" || hits[0].Date != "2026-11-03" || hits[0].Source != day {
		t.Fatalf("hits %+v %v", hits, err)
	}
	if hits, _ := ix.Search(context.Background(), "what's my main project, homelab?", 1, 0.5); len(hits) != 1 || hits[0].Source != "memory" {
		t.Fatalf("memory hit %+v", hits)
	}
	// An edit re-indexes that file only; a deleted file goes.
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(day, []byte("# 2026-11-03\n\n- 09:00 buy oat milk\n"), 0o644)
	os.Chtimes(day, time.Now(), time.Now().Add(time.Second))
	if n, _ := ix.Sync(context.Background(), []string{day, mem}, mem); n != 1 || ix.Len() != 2 {
		t.Fatalf("edit: %d, %d chunks", n, ix.Len())
	}
	ix.Sync(context.Background(), []string{mem}, mem)
	if ix.Len() != 1 {
		t.Fatalf("delete: %d chunks", ix.Len())
	}
	// Web results: cached, embedded, and kept across a reopen.
	res := []map[string]any{{"title": "Canberra", "snippet": "Canberra is the capital of Australia.", "source": "Wikipedia", "url": "https://en.wikipedia.org/wiki/Canberra"}}
	ix.AddWeb(context.Background(), "Capital of  Australia", res, time.Now())
	ix2 := Open(filepath.Join(dir, "index.json"), e)
	if r, ok := ix2.Cached("capital of australia", time.Hour); !ok || r[0]["title"] != "Canberra" {
		t.Fatalf("cache %v %v", r, ok)
	}
	if hits, _ := ix2.Search(context.Background(), "australia", 1, 0.5); len(hits) != 1 || hits[0].Source != "https://en.wikipedia.org/wiki/Canberra" {
		t.Fatalf("web hit %+v", hits)
	}
}
