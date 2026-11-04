package daemon

import (
	"strings"
	"testing"

	"herdr-voice-computer/internal/intent"
)

func TestWindowTextKeepsWordsDropsIDs(t *testing.T) {
	got := windowText([]byte(`{"tree":{"name":"Inbox","children":[{"name":"tabs-newtab-button"},{"value":"Lunch at noon?"},{"name":"true"},{"name":"Inbox"}]}}`))
	if got != "Inbox\nLunch at noon?\n" {
		t.Fatalf("%q", got)
	}
}

func TestPasswordManagersAreNeverRead(t *testing.T) {
	for _, app := range []string{"Bitwarden", "1Password 8", "Keychain Access", "Passwords"} {
		if !neverRead.MatchString(app) {
			t.Errorf("%s readable", app)
		}
	}
}

func TestAppPhrases(t *testing.T) {
	for in, want := range map[string]string{"Open the Telegram app": "open_app telegram", "Launch Firefox": "open_app firefox",
		"What's in my Telegram app?": "read_app telegram", "What's on my Spotify?": "read_app spotify"} {
		f, ok := intent.Match(in, false)
		got := f.Call.Name + " "
		for _, k := range []string{"app"} {
			got += f.Call.Str(k)
		}
		if !ok || strings.TrimSpace(got) != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestOpenAnInstalledApp(t *testing.T) {
	if installedApp("calculator") != "Calculator" { // every Mac has it
		t.Skip("no Calculator.app")
	}
	f, _ := intent.Match("Open Calculator.", false)
	got, ok := asApp(f)
	if !ok || got.Call.Name != "open_app" || got.Call.Str("app") != "Calculator" {
		t.Fatalf("%+v", got.Call)
	}
	if _, ok := asApp(intent.Fast{Kind: "tool", Call: intent.Call{Name: "focus", Args: map[string]any{"name": "no such app here"}}}); ok {
		t.Fatal("an unknown name became an app")
	}
}
