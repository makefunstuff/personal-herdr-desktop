package search

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeWeb struct {
	*httptest.Server
	dsStatus []int // status for each DeepSeek search call, in order (then 200)
	calls    atomic.Int32
	body     atomic.Value
}

func newFake(t *testing.T) *fakeWeb {
	f := &fakeWeb{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/tools/search":
			n := int(f.calls.Add(1)) - 1
			if r.Header.Get("Authorization") != "Bearer test-key" {
				w.WriteHeader(401)
				w.Write([]byte(`{"error":{"message":"bad key","type":"auth","code":"invalid"}}`))
				return
			}
			b, _ := io.ReadAll(r.Body)
			f.body.Store(string(b))
			if n < len(f.dsStatus) && f.dsStatus[n] != 200 {
				w.WriteHeader(f.dsStatus[n])
				w.Write([]byte(`{"error":{"message":"nope","type":"x","param":null,"code":"x"}}`))
				return
			}
			w.Write([]byte(`{"results":[{"title":"Low","url":"https://low.example/a","snippet":"low snippet","summary":"","rerank_score":0.5,"site_name":"Low","date":null,"authority":-1},
				{"title":"Canberra","url":"https://en.wikipedia.org/wiki/Canberra","snippet":"s","summary":"Canberra is the capital city of Australia.","rerank_score":2.8,"site_name":"Wikipedia","date":"2026-01-05T00:00:00Z","authority":4}]}`))
		case r.URL.Path == "/v1/tools/fetch":
			w.Write([]byte(`{"title":"Canberra","content":"# Canberra\n\nCanberra is the capital city of Australia. It was founded in 1913."}`))
		case strings.HasPrefix(r.URL.Path, "/ddg"):
			w.Write([]byte(`{"Heading":"Canberra","AbstractText":"Canberra is the capital of Australia.","AbstractSource":"Wikipedia","AbstractURL":"https://en.wikipedia.org/wiki/Canberra","RelatedTopics":[]}`))
		case r.URL.Path == "/w/rest.php/v1/search/page":
			w.Write([]byte(`{"pages":[{"key":"Canberra","title":"Canberra","excerpt":"<span>Canberra</span> is the capital"}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/rest_v1/page/summary/"):
			w.Write([]byte(`{"extract":"Canberra is the capital city of Australia.","content_urls":{"desktop":{"page":"https://en.wikipedia.org/wiki/Canberra"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func client(t *testing.T, f *fakeWeb, key string) *Client {
	dir := t.TempDir()
	kf := filepath.Join(dir, "deepseek.key")
	if key != "" {
		os.WriteFile(kf, []byte(key+"\n"), 0o600)
	}
	return &Client{DeepSeekURL: f.URL, DeepSeekKey: kf, DDGURL: f.URL + "/ddg", WikiURL: f.URL, Timeout: 3 * time.Second, RetryWait: time.Millisecond}
}

func TestDeepSeekFirstBestFirst(t *testing.T) {
	f := newFake(t)
	c := client(t, f, "test-key")
	rs, warn, err := c.Search(context.Background(), "capital of Australia", "what's the capital of Australia")
	if err != nil || warn != "" || c.LastBackend() != "deepseek" {
		t.Fatalf("%v %q %s", err, warn, c.LastBackend())
	}
	if rs[0].Title != "Canberra" || rs[0].Snippet != "Canberra is the capital city of Australia." || rs[0].Source != "Wikipedia" || rs[0].Date != "2026-01-05" {
		t.Fatalf("results: %+v", rs)
	}
	var body map[string]any
	json.Unmarshal([]byte(f.body.Load().(string)), &body)
	if body["query"] != "capital of Australia" || body["count"] != float64(5) || body["need_content"] != false || body["ori_question"] != "what's the capital of Australia" {
		t.Fatalf("body %v", body)
	}
	title, text, err := c.Fetch(context.Background(), "https://en.wikipedia.org/wiki/Canberra", 60)
	if err != nil || title != "Canberra" || len(text) > 64 {
		t.Fatalf("fetch %q %q %v", title, text, err)
	}
}

func TestDeepSeekRetriesAndFallsBack(t *testing.T) {
	f := newFake(t)
	f.dsStatus = []int{503} // retried once, then fine
	c := client(t, f, "test-key")
	if _, _, err := c.Search(context.Background(), "q", "q"); err != nil || c.LastBackend() != "deepseek" || f.calls.Load() != 2 {
		t.Fatalf("retry: %v %s %d", err, c.LastBackend(), f.calls.Load())
	}
	// 402: said once, and the fallback answers from then on
	f2 := newFake(t)
	f2.dsStatus = []int{402}
	c2 := client(t, f2, "test-key")
	rs, warn, err := c2.Search(context.Background(), "capital", "capital")
	if err != nil || !strings.Contains(warn, "balance ran out") || c2.LastBackend() != "ddg+wikipedia" || rs[0].Source != "Wikipedia" {
		t.Fatalf("402: %v %q %s %+v", err, warn, c2.LastBackend(), rs)
	}
	if _, warn, _ = c2.Search(context.Background(), "capital", "capital"); warn != "" || f2.calls.Load() != 1 {
		t.Fatalf("402 again: %q, %d calls", warn, f2.calls.Load())
	}
	// 401 (a bad key) and no key at all: the keyless search answers
	c3 := client(t, newFake(t), "wrong-key")
	if _, _, err := c3.Search(context.Background(), "capital", "capital"); err != nil || c3.LastBackend() != "ddg+wikipedia" {
		t.Fatalf("401: %v %s", err, c3.LastBackend())
	}
	c4 := client(t, newFake(t), "")
	if rs, _, err := c4.Search(context.Background(), "capital", "capital"); err != nil || len(rs) == 0 || c4.LastBackend() != "ddg+wikipedia" {
		t.Fatalf("no key: %v %+v", err, rs)
	}
}

func TestUnreachable(t *testing.T) {
	c := &Client{DDGURL: "http://127.0.0.1:1", WikiURL: "http://127.0.0.1:1", Timeout: time.Second}
	if _, _, err := c.Search(context.Background(), "x", "x"); err == nil {
		t.Fatal("no error")
	}
}
