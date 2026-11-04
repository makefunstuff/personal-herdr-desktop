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
