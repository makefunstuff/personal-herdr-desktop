// Package index is the voice computer's local semantic memory: the owner's desk notes, memory.md and
// the web results it looked up, cut into short chunks, embedded with EmbeddingGemma on this Mac and
// kept in one JSON file. Searches embed the question and rank chunks by cosine similarity. Nothing
// leaves the machine.
package index

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Embedder asks an OpenAI-compatible /v1/embeddings (llama-server --embedding).
type Embedder struct {
	URL, Model string
	HTTP       *http.Client
}

// EmbeddingGemma's prompts for a question and for a document.
func Query(s string) string { return "task: search result | query: " + s }
func Doc(s string) string   { return "title: none | text: " + s }

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": e.Model, "input": texts})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(e.URL, "/")+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := e.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, fmt.Errorf("embeddings: HTTP %d %s", resp.StatusCode, b)
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < len(vecs) {
			vecs[d.Index] = normalize(d.Embedding)
		}
	}
	for i, v := range vecs {
		if v == nil {
			return nil, fmt.Errorf("embeddings: none for input %d", i)
		}
	}
	return vecs, nil
}

func normalize(v []float32) []float32 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	n := float32(math.Sqrt(s))
	if n == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / n
	}
	return out
}

// Chunk is one piece of text with where it came from.
type Chunk struct {
	Text   string `json:"text"`
	Source string `json:"source"` // a file path, "memory", or a URL
	Date   string `json:"date"`   // YYYY-MM-DD, when known
	Vec    string `json:"vec"`    // base64 float32 little-endian, normalized
	vec    []float32
}

// WebEntry is one cached search: its results, so asking again needs no network.
type WebEntry struct {
	At      int64            `json:"at"`
	Results []map[string]any `json:"results"`
}

// Hit is a search hit.
type Hit struct {
	Text   string  `json:"text"`
	Source string  `json:"source"`
	Date   string  `json:"date"`
	Score  float64 `json:"score"`
}

type Index struct {
	Path string
	E    *Embedder
	mu   sync.Mutex
	data struct {
		Files  map[string]int64    `json:"files"` // path → mtime (unix ns) when indexed
		Chunks []Chunk             `json:"chunks"`
		Web    map[string]WebEntry `json:"web"`
	}
}

func Open(path string, e *Embedder) *Index {
	ix := &Index{Path: path, E: e}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &ix.data)
	}
	if ix.data.Files == nil {
		ix.data.Files = map[string]int64{}
	}
	if ix.data.Web == nil {
		ix.data.Web = map[string]WebEntry{}
	}
	for i := range ix.data.Chunks {
		ix.data.Chunks[i].vec = decode(ix.data.Chunks[i].Vec)
	}
	return ix
}

func encode(v []float32) string {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return base64.StdEncoding.EncodeToString(b)
}

func decode(s string) []float32 {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func (ix *Index) saveLocked() error {
	b, err := json.Marshal(ix.data)
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(ix.Path), 0o700)
	tmp := ix.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, ix.Path)
}

var (
	dateRE   = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)
	bulletRE = regexp.MustCompile(`^\s*[-*] (\[[ xX]\] )?`)
)

// chunks cuts a markdown file: a bullet each, or a paragraph; headings are skipped. A conversation
// (.jsonl) is a chunk a turn.
func chunksOf(path, text, memoryPath string) []Chunk {
	if strings.HasSuffix(path, ".jsonl") {
		var out []Chunk
		for _, l := range strings.Split(text, "\n") {
			var t struct {
				At       int64  `json:"at"`
				You      string `json:"you"`
				Computer string `json:"computer"`
				Kind     string `json:"kind"`
			}
			// Only what was talked about: commands and lookups ("theme fortress", "find my note…") aren't.
			if json.Unmarshal([]byte(l), &t) != nil || t.You == "" || (t.Kind != "" && t.Kind != "brain") {
				continue
			}
			c := "you said: " + t.You
			if t.Computer != "" {
				c += " / I said: " + t.Computer
			}
			if len(c) > 500 {
				c = c[:500]
			}
			out = append(out, Chunk{Text: c, Source: "conversation", Date: time.Unix(t.At, 0).Format("2006-01-02")})
		}
		return out
	}
	date := ""
	if m := dateRE.FindStringSubmatch(filepath.Base(path)); m != nil {
		date = m[1]
	}
	source := path
	if path == memoryPath {
		source = "memory"
	}
	var out []Chunk
	var para []string
	flush := func() {
		if t := strings.TrimSpace(strings.Join(para, " ")); len(t) > 3 {
			out = append(out, Chunk{Text: t, Source: source, Date: date})
		}
		para = nil
	}
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "<!--"):
			flush()
		case bulletRE.MatchString(t):
			flush()
			para = []string{bulletRE.ReplaceAllString(t, "")}
			flush()
		default:
			para = append(para, t)
		}
	}
	flush()
	for i := range out {
		if len(out[i].Text) > 500 {
			out[i].Text = out[i].Text[:500]
		}
	}
	return out
}

// Sync indexes the files that changed since the last time (and drops gone ones); memoryPath's chunks
// are tagged "memory". It returns how many chunks it embedded.
func (ix *Index) Sync(ctx context.Context, files []string, memoryPath string) (int, error) {
	ix.mu.Lock()
	seen := map[string]bool{}
	var todo []string
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		seen[f] = true
		if ix.data.Files[f] != st.ModTime().UnixNano() {
			todo = append(todo, f)
		}
	}
	var gone []string
	for f := range ix.data.Files {
		if !seen[f] {
			gone = append(gone, f)
		}
	}
	ix.mu.Unlock()
	if len(todo) == 0 && len(gone) == 0 {
		return 0, nil
	}
	type file struct {
		path   string
		mtime  int64
		chunks []Chunk
	}
	var done []file
	n := 0
	for _, f := range todo {
		st, err := os.Stat(f)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		cs := chunksOf(f, string(b), memoryPath)
		for i := 0; i < len(cs); i += 32 {
			end := min(len(cs), i+32)
			var texts []string
			for _, c := range cs[i:end] {
				texts = append(texts, Doc(c.Text))
			}
			vecs, err := ix.E.Embed(ctx, texts)
			if err != nil {
				return n, err
			}
			for j, v := range vecs {
				cs[i+j].vec, cs[i+j].Vec = v, encode(v)
			}
		}
		n += len(cs)
		done = append(done, file{f, st.ModTime().UnixNano(), cs})
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	drop := map[string]bool{}
	for _, f := range gone {
		drop[f] = true
		delete(ix.data.Files, f)
	}
	for _, f := range done {
		drop[f.path] = true
	}
	keep := ix.data.Chunks[:0]
	for _, c := range ix.data.Chunks {
		src := c.Source
		switch src {
		case "memory":
			src = memoryPath
		case "conversation":
			src = convoPath(files)
		}
		if !drop[src] {
			keep = append(keep, c)
		}
	}
	ix.data.Chunks = keep
	for _, f := range done {
		ix.data.Chunks = append(ix.data.Chunks, f.chunks...)
		ix.data.Files[f.path] = f.mtime
	}
	return n, ix.saveLocked()
}

func norm(q string) string { return strings.Join(strings.Fields(strings.ToLower(q)), " ") }

// Cached is a search done before (within maxAge), so it needs no network.
func (ix *Index) Cached(query string, maxAge time.Duration) ([]map[string]any, bool) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	e, ok := ix.data.Web[norm(query)]
	if !ok || time.Since(time.UnixMilli(e.At)) > maxAge {
		return nil, false
	}
	return e.Results, true
}

// AddWeb keeps a search's results (cached, and embedded so recall finds them later).
func (ix *Index) AddWeb(ctx context.Context, query string, results []map[string]any, at time.Time) error {
	var cs []Chunk
	var texts []string
	for _, r := range results {
		text := strings.TrimSpace(fmt.Sprint(r["title"]) + ": " + fmt.Sprint(r["snippet"]))
		src, _ := r["url"].(string)
		if src == "" {
			src, _ = r["source"].(string)
		}
		cs = append(cs, Chunk{Text: text, Source: src, Date: at.Format("2006-01-02")})
		texts = append(texts, Doc(text))
	}
	var vecs [][]float32
	if len(texts) > 0 && ix.E != nil {
		v, err := ix.E.Embed(ctx, texts)
		if err == nil {
			vecs = v
		}
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.data.Web[norm(query)] = WebEntry{At: at.UnixMilli(), Results: results}
	if vecs != nil {
		urls := map[string]bool{}
		for _, c := range cs {
			urls[c.Source] = true
		}
		keep := ix.data.Chunks[:0]
		for _, c := range ix.data.Chunks { // a page found again replaces its old chunk
			if !urls[c.Source] || !strings.HasPrefix(c.Source, "http") {
				keep = append(keep, c)
			}
		}
		ix.data.Chunks = keep
		for i := range cs {
			cs[i].vec, cs[i].Vec = vecs[i], encode(vecs[i])
			ix.data.Chunks = append(ix.data.Chunks, cs[i])
		}
	}
	return ix.saveLocked()
}

// Search ranks chunks by similarity to query: the best k at or above min.
func (ix *Index) Search(ctx context.Context, query string, k int, minScore float64) ([]Hit, error) {
	vecs, err := ix.E.Embed(ctx, []string{Query(query)})
	if err != nil {
		return nil, err
	}
	q := vecs[0]
	ix.mu.Lock()
	defer ix.mu.Unlock()
	var hits []Hit
	for _, c := range ix.data.Chunks {
		if len(c.vec) != len(q) {
			continue
		}
		var s float64
		for i := range q {
			s += float64(q[i]) * float64(c.vec[i])
		}
		if s >= minScore {
			hits = append(hits, Hit{Text: c.Text, Source: c.Source, Date: c.Date, Score: math.Round(s*1000) / 1000})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

// Len is how many chunks it holds.
func (ix *Index) Len() int {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return len(ix.data.Chunks)
}

// convoPath is the conversation file among files ("" when none).
func convoPath(files []string) string {
	for _, f := range files {
		if strings.HasSuffix(f, ".jsonl") {
			return f
		}
	}
	return ""
}
