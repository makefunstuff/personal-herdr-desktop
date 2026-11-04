package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(filepath.Join(dir, "none.json"))
	if err != nil || c.Backend != "openai" || c.Chat.URL != "http://127.0.0.1:8790" || c.History != 60 || c.TapMS != 300 ||
		c.Name != "Herdr" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	p := filepath.Join(dir, "config.json")
	os.WriteFile(p, []byte(`{"name": "Ada", "voice": "male", "stt": {"url": "https://stt.example/", "model": "x", "key_file": "~/k"},
		"chat": {"url": "http://127.0.0.1:9000", "model": "m"}, "history": 5}`), 0o600)
	c, err = Load(p)
	home, _ := os.UserHomeDir()
	if err != nil || c.Voice != "male" || c.STT.URL != "https://stt.example" || c.STT.KeyFile != filepath.Join(home, "k") ||
		c.Chat.Model != "m" || c.TTS.URL != "http://127.0.0.1:8791" || c.IdleCloseS != 120 || c.History != 5 {
		t.Fatalf("override: %+v %v", c, err)
	}
	if !strings.HasPrefix(c.Instructions, "You are Ada, ") {
		t.Fatalf("the name isn't in the instructions: %q", c.Instructions)
	}
	if k := c.KeyFiles(); len(k) != 1 || k[0] != filepath.Join(home, "k") {
		t.Fatalf("key files: %v", k)
	}
	os.WriteFile(p, []byte(`{"chat": {"url": "localhost:8080"}}`), 0o600)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "chat.url") {
		t.Fatalf("bad url accepted: %v", err)
	}
	os.WriteFile(p, []byte(`{"backend": "realtime"}`), 0o600)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "backend") {
		t.Fatalf("bad backend accepted: %v", err)
	}
}

// The personality: a warm executive assistant, with the original's honesty rules word for word.
func TestInstructionsKeepThePersonality(t *testing.T) {
	s := Instructions("Herdr", "Alex", ", and that an agent can find out")
	for _, want := range []string{"You are Herdr, Alex's executive assistant inside Herdr", "warm, sharp and attentive",
		"personal helper", "two to four natural sentences", "up to five sentences", "at most one per answer", "never invent one", "style notes ask for shorter answers", "Wikipedia says", "call web_search", "never markdown", "one useful next step as a short question", "Use Alex's name now and then",
		"small local model", "never state a fact, a number, a name or a date from your own knowledge", "never work out a sum",
		"never guess", "context line", "what you remember from earlier conversations with Alex",
		"say plainly in one sentence that you don't know, and that an agent can find out", "ask back briefly", "Think briefly",
		"call remember", "Never remember a password", "I'd only be guessing there, sorry."} {
		if !strings.Contains(s, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
	if strings.Contains(s, "%!") {
		t.Fatalf("a bad verb: %s", s)
	}
}

func TestOwnerDefaultsToGitsFirstName(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if want := DefaultOwner(); c.Owner != want || (want != "" && !strings.Contains(c.Instructions, want+"'s executive assistant")) {
		t.Fatalf("owner %q, git's %q", c.Owner, want)
	}
	p := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(p, []byte(`{"owner": "Ada"}`), 0o600)
	if c, _ := Load(p); c.Owner != "Ada" || !strings.Contains(c.Instructions, "Ada's executive assistant") {
		t.Fatalf("owner: %q", c.Owner)
	}
}
