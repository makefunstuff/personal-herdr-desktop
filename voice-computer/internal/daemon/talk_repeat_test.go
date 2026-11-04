package daemon

import "testing"

func TestRecentTurnsReplayARepeatedReplyOnce(t *testing.T) {
	d := &Daemon{}
	d.turns = []Turn{{You: "a", Computer: "I'm still here."}, {You: "b", Computer: "Sure."}, {You: "c", Computer: "I'm  still here."}}
	got := d.recentTurnsLocked()
	if len(got) != 2 || got[0].You != "b" || got[1].You != "c" {
		t.Fatalf("got %+v", got)
	}
}
