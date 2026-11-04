// Package macos is what the voice computer does on the Mac itself: the volume and the media player,
// through osascript (no other program, no permissions beyond what macOS asks once for Automation).
package macos

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Script runs AppleScript (tests swap it).
var Script = func(ctx context.Context, src string) (string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", src).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("osascript: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Volume is the output volume, 0 to 100, and whether it is muted.
func Volume(ctx context.Context) (int, bool, error) {
	out, err := Script(ctx, "get volume settings")
	if err != nil {
		return 0, false, err
	}
	// output volume:50, input volume:75, alert volume:100, output muted:false
	level, muted := -1, false
	for _, part := range strings.Split(out, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), ":")
		switch k {
		case "output volume":
			level, _ = strconv.Atoi(v)
		case "output muted":
			muted = v == "true"
		}
	}
	if level < 0 {
		return 0, false, errors.New("no output volume (" + out + ")")
	}
	return level, muted, nil
}

// SetVolume sets the output volume and returns the one before.
func SetVolume(ctx context.Context, level int) (int, error) {
	before, _, err := Volume(ctx)
	if err != nil {
		return 0, err
	}
	_, err = Script(ctx, fmt.Sprintf("set volume output volume %d", max(0, min(100, level))))
	return before, err
}

// StepVolume changes the volume by step and returns before and after.
func StepVolume(ctx context.Context, step int) (int, int, error) {
	before, _, err := Volume(ctx)
	if err != nil {
		return 0, 0, err
	}
	after := max(0, min(100, before+step))
	_, err = Script(ctx, fmt.Sprintf("set volume output volume %d", after))
	return before, after, err
}

// SetMute mutes or unmutes; changed is false when it already was.
func SetMute(ctx context.Context, mute bool) (bool, error) {
	_, muted, err := Volume(ctx)
	if err != nil {
		return false, err
	}
	if muted == mute {
		return false, nil
	}
	_, err = Script(ctx, fmt.Sprintf("set volume output muted %t", mute))
	return err == nil, err
}

// players are the media apps vcd controls, the first running one.
var players = []string{"Spotify", "Music"}

// Player is the running media app and whether it plays.
func Player(ctx context.Context) (string, bool, bool) {
	for _, p := range players {
		out, err := Script(ctx, fmt.Sprintf(`if application %q is running then tell application %q to get player state as string`, p, p))
		if err == nil && out != "" {
			return p, out == "playing", true
		}
	}
	return "", false, false
}

// MediaAction plays, pauses or skips in the running media app.
func MediaAction(ctx context.Context, action string) error {
	verb := map[string]string{"play": "play", "pause": "pause", "next": "next track", "previous": "previous track"}[action]
	if verb == "" {
		return errors.New("media takes play, pause, next or previous")
	}
	app, _, ok := Player(ctx)
	if !ok {
		return errors.New("no media app is running (Spotify or Music)")
	}
	_, err := Script(ctx, fmt.Sprintf("tell application %q to %s", app, verb))
	return err
}
