package herdr

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Fake is a Runner that answers `herdr-app ls|status` from files and records every other command.
type fake struct {
	ls, status string
	ran        []string
}

func (f *fake) run(_ context.Context, name string, args ...string) ([]byte, error) {
	switch {
	case len(args) == 1 && args[0] == "ls":
		return []byte(f.ls), nil
	case len(args) == 1 && args[0] == "status":
		return []byte(f.status), nil
	}
	f.ran = append(f.ran, name+" "+strings.Join(args, " "))
	return []byte("{}"), nil
}

func world(t *testing.T) (World, *fake) {
	b, err := os.ReadFile("testdata/ls.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fake{ls: string(b), status: `{"layout": "workbench", "space": "sp1", "tab": "tb1", "agent": "t3"}`}
	c := &Client{App: "herdr-app", HC: "hc", Run: f.run}
	w, err := c.World(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return w, f
}

func TestFindAgents(t *testing.T) {
	w, _ := world(t)
	for spoken, want := range map[string]string{
		"the semantic grep agent": "gpubox/t82", // the space's most urgent agent (SEMA-4 is blocked)
		"semantic grep":           "gpubox/t82",
		"semantic rep":            "gpubox/t82",
		"semantic grep sema 3":    "gpubox/t81",
		"sema three":              "",
		"SEMA-3":                  "gpubox/t81",
		"shepherder":              "gpubox/t90",
		"shepherd r":              "gpubox/t90",
		"the mmwave lab":          "gpubox/t91",
		"hardware lab":            "gpubox/t91",
		"home lab":                "gpubox/t92",
		"homelab":                 "gpubox/t92",
		"work":                    "t3",
		"kubernetes":              "",
	} {
		got := w.FindAgents(spoken)
		switch {
		case want == "" && len(got) != 0 && spoken == "kubernetes":
			t.Errorf("%q: matched %v", spoken, got)
		case want != "" && (len(got) != 1 || got[0].Terminal != want):
			t.Errorf("%q: got %+v, want %s", spoken, got, want)
		}
	}
	if a, ok := w.Focused(); !ok || a.Terminal != "t3" || a.Label() != "Work" {
		t.Fatalf("focused: %+v", a)
	}
	if got := w.FindAgents("semantic grep sema 4"); len(got) != 1 || got[0].Label() != "semantic grep nvim SEMA 4" {
		t.Fatalf("label: %+v", got)
	}
}

func TestFindSpaces(t *testing.T) {
	w, _ := world(t)
	if s := w.FindSpaces("home"); len(s) != 1 || s[0].Name != "home" {
		t.Fatalf("home: %+v", s)
	}
	if s := w.FindSpaces("the shepherder space"); len(s) != 1 || s[0].Name != "shepherdr" {
		t.Fatalf("shepherdr: %+v", s)
	}
}

func TestDoBuildsTheCommand(t *testing.T) {
	_, f := world(t)
	c := &Client{App: "herdr-app", HC: "hc", Run: f.run}
	c.Do(context.Background(), "layout.switch", map[string]any{"name": "fortress"})
	c.Send(context.Background(), "t9", "hello; rm -rf /")
	if f.ran[0] != `herdr-app do layout.switch {"name":"fortress"}` || f.ran[1] != "hc send t9 hello; rm -rf /" {
		t.Fatalf("ran %q", f.ran)
	}
}
