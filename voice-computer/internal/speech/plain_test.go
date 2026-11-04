package speech

import "testing"

func TestPlain(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		// Coordinator replies as they come.
		{"ABC-33 is merged, and an app build with the new settings page builds next.",
			"The task is merged, and an app build with the new settings page builds next."},
		{"The new app build is running (ABC-34). The \"needs you\" card came from a brief pause, and the build is running again. Nothing needed from you.",
			"The new app build is running. The \"needs you\" card came from a brief pause, and the build is running again."},
		{"The app with the new settings page is out: build-1a2b3c4, marked Latest.",
			"The app with the new settings page is out: marked Latest."},
		{"Sure, I've started on it as ABC-16. The settings page gets a dark theme, and the old toggle goes away.\n\n- **Scope:** only the settings page.\n- **Tests:** the snapshot ones change.",
			"Sure, I've started on it as the task. The settings page gets a dark theme, and the old toggle goes away."},
		// Code, paths, links, logs and tags are left out.
		{"Fixed it in `internal/daemon/daemon.go`, see [the PR](https://github.com/x/y/pull/3).\n\n```go\nfunc main() {}\n```\nTests pass.",
			"Fixed it, see the PR. Tests pass."},
		{"<reply to=\"Can you check?\"/>\nDone: commit 1a2b3c4 on ~/Work/project.\n2026-11-01 23:10:01 INFO started\nAll good 👍",
			"Done: commit. All good."},
		{"Say `be quiet` to mute it.", "Say be quiet to mute it."},
		{"PR #52 is ready to merge.", "The PR is ready to merge."},
		{"## Summary\n| a | b |\n|---|---|\n| 1 | 2 |\nIt works.", "Summary. It works."},
		{"```\nonly code\n```", ""},
		{"Set `brain_extra` in config.json and run --force.", "Set in and run."},
		{"Use e.g. the home space. Then the next one. And a third.", "Use e.g. the home space. Then the next one."},
	} {
		if got := Plain(c.in); got != c.want {
			t.Errorf("Plain(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestPlainCutsLongSentences(t *testing.T) {
	in := "This sentence goes on and on with many words, more than anyone wants to hear read out loud by a voice at the desk while working on something else entirely and it still does not stop here."
	got := Plain(in)
	if n := len([]rune(got)); n > 200 || got[len(got)-1] != '.' {
		t.Fatalf("not cut: %q", got)
	}
}

func TestPlainKeepsRealWords(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Ready: the fix is in.", "Ready: the fix is in."},
		{"Node.js is slow here.", "Node.js is slow here."},
		{"Use UTF-8 everywhere. GPT-4 is fine.", "Use UTF-8 everywhere. GPT-4 is fine."},
		{"The split is 50/50 now.", "The split is 50 50 now."},
		{"It's on `main` in ~/Work/x.", "It's on main."},
	} {
		if got := Plain(c.in); got != c.want {
			t.Errorf("Plain(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestPlainDropsMoreCode(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Merged as 03da397 into main.", "Merged into main."},
		{"Pushed to origin/work-16. Done.", "Pushed. Done."},
		{"It failed.\nerror: exit status 1\nRetrying.", "It failed. Retrying."},
		{"The key is sk-abc123def456ghi789jkl now.", "The key is now."},
	} {
		if got := Plain(c.in); got != c.want {
			t.Errorf("Plain(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}
