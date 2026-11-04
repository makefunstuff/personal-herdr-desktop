package daemon

import "testing"

func TestGroundedKeepsOnlyWhatTheOwnerSaid(t *testing.T) {
	said := "my main project right now is the homelab and I hate long answers in the morning"
	if !grounded("Alex's main project is the homelab", said, "Alex") {
		t.Error("a note from their words was dropped")
	}
	if grounded("Alex works at Google as a manager", said, "Alex") {
		t.Error("an invented note was kept")
	}
}

func TestParseLearned(t *testing.T) {
	l := parseLearned("```json\n{\"facts\": [\"A\"], \"style\": []}\n```")
	if len(l.Facts) != 1 || l.Facts[0] != "A" {
		t.Fatalf("%+v", l)
	}
}

func TestThoughtOf(t *testing.T) {
	got := thoughtOf("<think>\nLet me think. Alex wants to see homelab, so go_to_space with homelab. Then I", "Alex")
	if got != "Alex wants to see homelab, so go_to_space with homelab." {
		t.Fatalf("%q", got)
	}
	if got := thoughtOf(" to rest. Small talk: answer warmly.", "Alex"); got != "Alex wants to rest. Small talk: answer warmly." {
		t.Fatalf("%q", got)
	}
}

func TestPassingNotesAreNotKept(t *testing.T) {
	for _, n := range []string{"Alex said I'm pretty tired today.", "Alex is fixing the homelab tonight."} {
		if !passing(n, "Alex") {
			t.Errorf("%q kept", n)
		}
	}
	if passing("Alex's main project this month is the homelab.", "Alex") {
		t.Error("a lasting note dropped")
	}
}
