package daemon

// Dictation: the owner's words written down for a text box (Herdr's chat), no turn, no brain. The mic
// opens through Herdr Voice (its permission), LFM3-Audio writes it down on this Mac, and the text goes
// back to whoever asked (vc dictate stop prints it). At most five minutes a take.

import (
	"context"
	"errors"
	"strings"
	"time"

	"herdr-voice-computer/internal/audio"
)

const dictateMax = 5 * time.Minute

// DictateStart opens the mic for a dictation.
func (d *Daemon) DictateStart() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.dict != nil {
		return nil // already listening
	}
	if d.rec != nil {
		return errors.New("the mic is in a voice turn")
	}
	rec, err := audio.Record(dictateMax, d.level)
	if err != nil {
		return errors.New("the mic didn't open: " + err.Error())
	}
	d.dict = rec
	return nil
}

// DictateStop closes the mic and returns what was said, written down.
func (d *Daemon) DictateStop(ctx context.Context) (string, error) {
	d.mu.Lock()
	rec := d.dict
	d.dict = nil
	d.mu.Unlock()
	if rec == nil {
		return "", errors.New("not dictating")
	}
	pcm := rec.Stop()
	d.level(0)
	if len(pcm) < 16000*2/4 { // under a quarter second: nothing said
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	text, err := d.voice.Transcribe(ctx, pcm)
	if err != nil {
		return "", errors.New("couldn't write it down: " + plainErr(err))
	}
	return strings.TrimSpace(text), nil
}

// DictateCancel closes the mic and drops the take.
func (d *Daemon) DictateCancel() {
	d.mu.Lock()
	rec := d.dict
	d.dict = nil
	d.mu.Unlock()
	if rec != nil {
		rec.Stop()
		d.level(0)
	}
}
