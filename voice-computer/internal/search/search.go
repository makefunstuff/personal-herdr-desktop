// Package search looks things up on the web for the voice computer: DeepSeek's search API when its
// key is there (the key file is read at each request and never logged), else keyless DuckDuckGo
// Instant Answers and Wikipedia; SearXNG or Brave when configured. Results are short: a title, a
// snippet, where it came from and its URL.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Result is one thing found.
type Result struct {
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Source  string  `json:"source"` // "Wikipedia", a site's name
	URL     string  `json:"url"`
	Date    string  `json:"date,omitempty"`
	Score   float64 `json:"score,omitempty"`
}

type Client struct {
	HTTP          *http.Client
	DeepSeekURL   string // https://api.deepseek.com
	DeepSeekKey   string // the key file
	DDGURL        string // https://api.duckduckgo.com
	WikiURL       string // https://en.wikipedia.org
	SearxURL      string
	BraveKey      string // the key file
	Timeout       time.Duration
	RetryWait     time.Duration
	mu            sync.Mutex
	outOfBalance  bool // DeepSeek said 402 once: the owner was told, and the fallback answers
	balanceWarned bool
	Backend       string // what answered last: deepseek, ddg+wikipedia, searxng, brave
}

func New() *Client {
	return &Client{DeepSeekURL: "https://api.deepseek.com", DeepSeekKey: "",
		DDGURL: "https://api.duckduckgo.com", WikiURL: "https://en.wikipedia.org", Timeout: 8 * time.Second,
		RetryWait: 700 * time.Millisecond}
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: c.Timeout}
}

func readKey(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ErrBalance: DeepSeek's balance ran out (said once; the fallback answers from then on).
var ErrBalance = errors.New("the DeepSeek search balance ran out")

// apiError is DeepSeek's error answer.
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message) }

// deepseek POSTs to one of DeepSeek's tools, retrying once on 408, 429 and 5xx.
func (c *Client) deepseek(ctx context.Context, path string, body any, out any) error {
	key := readKey(c.DeepSeekKey)
	if key == "" {
		return errors.New("no DeepSeek key")
	}
	data, _ := json.Marshal(body)
	for try := 0; ; try++ {
		req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.DeepSeekURL, "/")+path, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http().Do(req)
		if err != nil {
			return err
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if resp.StatusCode == 200 {
			return json.Unmarshal(b, out)
		}
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal(b, &e)
		retry := resp.StatusCode == 408 || resp.StatusCode == 429 || resp.StatusCode >= 500
		if retry && try == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.RetryWait):
			}
			continue
		}
		if resp.StatusCode == 402 {
			return ErrBalance
		}
		return &apiError{resp.StatusCode, e.Error.Message}
	}
}

func cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	if i := strings.LastIndexByte(s[:n], ' '); i > n/2 {
		return s[:i] + "…"
	}
	return s[:n] + "…"
}

// DeepSeek searches with DeepSeek's search tool; asked is the owner's own question.
func (c *Client) DeepSeek(ctx context.Context, query, asked string) ([]Result, error) {
	var out struct {
		Results []struct {
			Title     string   `json:"title"`
			URL       string   `json:"url"`
			Snippet   string   `json:"snippet"`
			Summary   string   `json:"summary"`
			Rerank    float64  `json:"rerank_score"`
			SiteName  string   `json:"site_name"`
			Date      *string  `json:"date"`
			Authority *float64 `json:"authority"`
		} `json:"results"`
	}
	err := c.deepseek(ctx, "/v1/tools/search", map[string]any{"query": cut(query, 128), "count": 5,
		"ori_question": cut(asked, 128), "need_content": false}, &out)
	if err != nil {
		return nil, err
	}
	var rs []Result
	for _, r := range out.Results {
		text := r.Summary
		if text == "" {
			text = r.Snippet
		}
		score := r.Rerank
		if r.Authority != nil && *r.Authority > 0 {
			score += *r.Authority / 4
		}
		if strings.Contains(strings.ToLower(r.SiteName+" "+r.URL), "wikipedia") { // plain, sourced prose: said best
			score += 1
		}
		res := Result{Title: r.Title, Snippet: cut(text, 600), Source: r.SiteName, URL: r.URL, Score: score}
		if res.Source == "" {
			res.Source = host(r.URL)
		}
		if r.Date != nil && len(*r.Date) >= 10 {
			res.Date = (*r.Date)[:10]
		}
		rs = append(rs, res)
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Score > rs[j].Score })
	return rs, nil
}

// Fetch reads a page through DeepSeek's fetch tool: its title and up to max characters of its text.
func (c *Client) Fetch(ctx context.Context, pageURL string, max int) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	var out struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := c.deepseek(ctx, "/v1/tools/fetch", map[string]any{"url": pageURL}, &out); err != nil {
		return "", "", err
	}
	return out.Title, cut(out.Content, max), nil
}

func host(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(p.Host, "www.")
}

func (c *Client) getJSON(ctx context.Context, u string, hdr map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "herdr-voice-computer/0.1 (local voice assistant)")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

var tagRE = regexp.MustCompile(`<[^>]+>`)

// Keyless: DuckDuckGo's instant answer, then Wikipedia's best pages with their summaries.
func (c *Client) Keyless(ctx context.Context, query string) ([]Result, error) {
	var rs []Result
	var ddg struct {
		Heading        string `json:"Heading"`
		AbstractText   string `json:"AbstractText"`
		AbstractSource string `json:"AbstractSource"`
		AbstractURL    string `json:"AbstractURL"`
		Answer         string `json:"Answer"`
		Related        []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
	}
	errD := c.getJSON(ctx, c.DDGURL+"/?format=json&no_html=1&skip_disambig=1&q="+url.QueryEscape(query), nil, &ddg)
	if errD == nil {
		if ddg.AbstractText != "" {
			rs = append(rs, Result{Title: ddg.Heading, Snippet: cut(ddg.AbstractText, 600), Source: ddg.AbstractSource, URL: ddg.AbstractURL, Score: 3})
		}
		if ddg.Answer != "" {
			rs = append(rs, Result{Title: ddg.Heading, Snippet: cut(ddg.Answer, 300), Source: "DuckDuckGo", Score: 2.5})
		}
		for i, r := range ddg.Related {
			if i == 2 || r.Text == "" {
				break
			}
			rs = append(rs, Result{Title: cut(r.Text, 60), Snippet: cut(r.Text, 300), Source: "DuckDuckGo", URL: r.FirstURL, Score: 1})
		}
	}
	var wiki struct {
		Pages []struct {
			Key     string `json:"key"`
			Title   string `json:"title"`
			Excerpt string `json:"excerpt"`
		} `json:"pages"`
	}
	errW := c.getJSON(ctx, c.WikiURL+"/w/rest.php/v1/search/page?limit=3&q="+url.QueryEscape(query), nil, &wiki)
	for i, p := range wiki.Pages {
		var sum struct {
			Extract string `json:"extract"`
			URLs    struct {
				Desktop struct {
					Page string `json:"page"`
				} `json:"desktop"`
			} `json:"content_urls"`
		}
		res := Result{Title: p.Title, Snippet: cut(tagRE.ReplaceAllString(p.Excerpt, ""), 300), Source: "Wikipedia",
			URL: c.WikiURL + "/wiki/" + url.PathEscape(p.Key), Score: 2 - float64(i)*0.5}
		if i == 0 && c.getJSON(ctx, c.WikiURL+"/api/rest_v1/page/summary/"+url.PathEscape(p.Key), nil, &sum) == nil && sum.Extract != "" {
			res.Snippet = cut(sum.Extract, 600)
			if sum.URLs.Desktop.Page != "" {
				res.URL = sum.URLs.Desktop.Page
			}
		}
		rs = append(rs, res)
	}
	if len(rs) == 0 && errD != nil && errW != nil {
		return nil, errD
	}
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Score > rs[j].Score })
	return rs, nil
}

func (c *Client) searx(ctx context.Context, query string) ([]Result, error) {
	var out struct {
		Results []struct {
			Title, Content, URL string
		} `json:"results"`
	}
	if err := c.getJSON(ctx, strings.TrimRight(c.SearxURL, "/")+"/search?format=json&q="+url.QueryEscape(query), nil, &out); err != nil {
		return nil, err
	}
	var rs []Result
	for _, r := range out.Results {
		rs = append(rs, Result{Title: r.Title, Snippet: cut(r.Content, 400), Source: host(r.URL), URL: r.URL})
	}
	return rs, nil
}

func (c *Client) brave(ctx context.Context, query string) ([]Result, error) {
	var out struct {
		Web struct {
			Results []struct {
				Title, Description, URL string
			} `json:"results"`
		} `json:"web"`
	}
	err := c.getJSON(ctx, "https://api.search.brave.com/res/v1/web/search?q="+url.QueryEscape(query),
		map[string]string{"X-Subscription-Token": readKey(c.BraveKey), "Accept": "application/json"}, &out)
	if err != nil {
		return nil, err
	}
	var rs []Result
	for _, r := range out.Web.Results {
		rs = append(rs, Result{Title: r.Title, Snippet: cut(tagRE.ReplaceAllString(r.Description, ""), 400), Source: host(r.URL), URL: r.URL})
	}
	return rs, nil
}

// Search finds at most 5 results: DeepSeek first (when its key is there and it has balance), then
// SearXNG or Brave when configured, then DuckDuckGo and Wikipedia. warn is said once to the owner (the
// DeepSeek balance ran out).
func (c *Client) Search(ctx context.Context, query, asked string) (rs []Result, warn string, err error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	c.mu.Lock()
	broke := c.outOfBalance
	c.mu.Unlock()
	if !broke && readKey(c.DeepSeekKey) != "" {
		rs, err = c.DeepSeek(ctx, query, asked)
		if err == nil && len(rs) > 0 {
			c.setBackend("deepseek")
			return top(rs), "", nil
		}
		if errors.Is(err, ErrBalance) {
			c.mu.Lock()
			c.outOfBalance = true
			if !c.balanceWarned {
				c.balanceWarned, warn = true, "Your DeepSeek search balance ran out, so I used the free search."
			}
			c.mu.Unlock()
		}
	}
	for _, alt := range []struct {
		name string
		on   bool
		f    func(context.Context, string) ([]Result, error)
	}{{"searxng", c.SearxURL != "", c.searx}, {"brave", c.BraveKey != "" && readKey(c.BraveKey) != "", c.brave}} {
		if alt.on {
			if rs, err = alt.f(ctx, query); err == nil && len(rs) > 0 {
				c.setBackend(alt.name)
				return top(rs), warn, nil
			}
		}
	}
	rs, err = c.Keyless(ctx, query)
	c.setBackend("ddg+wikipedia")
	return top(rs), warn, err
}

func (c *Client) setBackend(b string) {
	c.mu.Lock()
	c.Backend = b
	c.mu.Unlock()
}

// LastBackend is what answered the last search.
func (c *Client) LastBackend() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Backend
}

func top(rs []Result) []Result {
	if len(rs) > 5 {
		rs = rs[:5]
	}
	return rs
}
