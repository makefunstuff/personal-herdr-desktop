package intent

import "testing"

func TestPoliteAndPlaceWords(t *testing.T) {
	for in, want := range map[string]string{
		"Can you open Home Lab?":               "home lab",
		"Open Home Lab Workspace":              "home lab",
		"could you please open the work space": "work",
	} {
		f, ok := Match(in, false)
		if !ok || f.Call.Name != "focus" || f.Call.Args["name"] != want {
			t.Errorf("%q: got ok=%v %+v, want focus %q", in, ok, f.Call, want)
		}
	}
	if f, ok := Match("go to the homelab project", false); !ok || f.Call.Args["space"] != "homelab" {
		t.Errorf("go to: %+v", f.Call)
	}
}

func TestAddANote(t *testing.T) {
	for _, in := range []string{"Add a note: order new fans for the server", "Can you add a note order new fans for the server", "note to self, order new fans for the server"} {
		f, ok := Match(in, false)
		if !ok || f.Call.Name != "write_note" || f.Call.Args["text"] != "order new fans for the server" {
			t.Errorf("%q: %+v", in, f.Call)
		}
	}
}

func TestBrowse(t *testing.T) {
	for in, site := range map[string]string{
		"What's on Hacker News right now?": "hacker news",
		"Open the Go blog website":         "go blog",
		"Check news.ycombinator.com":       "news.ycombinator.com",
	} {
		f, ok := Match(in, false)
		if !ok || f.Call.Name != "browse" || f.Call.Args["site"] != site {
			t.Errorf("%q: %+v", in, f.Call)
		}
	}
	if f, ok := Match("what's on my plate today", false); ok && f.Call.Name == "browse" {
		t.Error("my plate is not a website")
	}
}
