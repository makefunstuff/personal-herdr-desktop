package daemon

import "testing"

func TestKeepsFigures(t *testing.T) {
	if !keepsFigures("HOME-16 is done, 3 files changed.", "Plot twist: HOME-16 actually finished, all 3 files changed.") {
		t.Error("a faithful rewrite refused")
	}
	if keepsFigures("HOME-16 is done, 3 files changed.", "Your homelab task is done!") {
		t.Error("a rewrite that lost the id and the number kept")
	}
}
