package daemon

import "testing"

func TestSiteURL(t *testing.T) {
	for in, want := range map[string]string{
		"news.ycombinator.com":      "https://news.ycombinator.com",
		"https://go.dev/blog":       "https://go.dev/blog",
		"hacker news":               "https://duckduckgo.com/?q=%21ducky+hacker+news",
		"the verge dot com website": "https://duckduckgo.com/?q=%21ducky+verge.com",
	} {
		if got := siteURL(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
