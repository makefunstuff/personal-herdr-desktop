package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddForgetDedupeAndOrder(t *testing.T) {
	p := filepath.Join(t.TempDir(), "memory.md")
	m := New(p, "Alex")
	m.Add(Fact, "my main project is homelab")
	m.Add(Style, "keep answers short")
	m.Add(Correction, "SEMA is semantic grep, not a person")
	m.Add(Style, "Keep answers short!") // a near duplicate: replaces the old one
	if s := m.Items(Style); len(s) != 1 || s[0] != "Keep answers short!" {
		t.Fatalf("style: %q", s)
	}
	b, _ := os.ReadFile(p)
	for _, want := range []string{"## About Alex\n\n- My main project is homelab.", "## How Alex likes me to talk\n\n- Keep answers short!", "## Corrections"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("file lacks %q:\n%s", want, b)
		}
	}
	pr := m.Prompt(1500)
	if !strings.HasPrefix(pr, "What you know from earlier conversations with Alex") ||
		strings.Index(pr, "Keep answers") > strings.Index(pr, "SEMA is") || strings.Index(pr, "SEMA is") > strings.Index(pr, "homelab") {
		t.Fatalf("prompt order:\n%s", pr)
	}
	// The owner's edits are read back.
	m2 := New(p, "Alex")
	if f := m2.Items(Fact); len(f) != 1 {
		t.Fatalf("reload: %q", f)
	}
	if gone, _ := m2.Forget("my main project"); gone != "My main project is homelab." {
		t.Fatalf("forgot %q", gone)
	}
	if gone, _ := m2.Forget("kubernetes clusters"); gone != "" {
		t.Fatalf("forgot %q", gone)
	}
}

func TestPromptCapKeepsTheNewest(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "m.md"), "Alex")
	for i := 0; i < 40; i++ {
		m.Add(Fact, strings.Repeat("x", 10)+" fact number "+string(rune('a'+i%26))+string(rune('a'+i/26))+" about some long project detail")
	}
	pr := m.Prompt(400)
	if len(pr) > 600 || !strings.Contains(pr, "fact number nb") || strings.Contains(pr, "fact number aa") {
		t.Fatalf("cap: %d\n%s", len(pr), pr)
	}
}

func TestSecretsAreRefused(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "m.md"), "Alex")
	for _, s := range []string{"my password is hunter2", "the API key is sk-abcdefghijklmnopqrstuvwxyz123", "my bank PIN is 1234"} {
		if _, err := m.Add(Fact, s); err != ErrSecret {
			t.Errorf("%q kept: %v", s, err)
		}
	}
	if len(m.Items(Fact)) != 0 {
		t.Fatal("a secret was written")
	}
}
