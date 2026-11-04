package daemon

import (
	"testing"

	"herdr-voice-computer/internal/intent"
)

func TestAppTasksGoToComputerTask(t *testing.T) {
	for in, app := range map[string]string{
		"Open calculator and calculate two plus two.":               "calculator",
		"No, I mean calculate two plus two on that calculator app.": "calculator",
		"Mute the work chat in the Telegram app":                    "telegram",
	} {
		f, ok := intent.Match(in, false)
		if !ok || f.Call.Name != "computer_task" || f.Call.Str("app") != app || f.Call.Str("task") != in {
			t.Errorf("%q: %+v", in, f.Call)
		}
	}
	if f, ok := intent.Match("open homelab", false); ok && f.Call.Name == "computer_task" {
		t.Error("a space became a computer task")
	}
}

func TestLastSentences(t *testing.T) {
	if got := lastSentences("I opened it. I pressed 3, 7 and =.\nIt shows 1517.", 2); got != "I pressed 3, 7 and =. It shows 1517." {
		t.Fatalf("%q", got)
	}
}
