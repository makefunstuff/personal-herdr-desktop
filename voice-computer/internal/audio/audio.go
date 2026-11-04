// Package audio records the mic and plays speech through sox's rec and play (CoreAudio on macOS), so
// vcd links no audio library. The mic is open only while a Recorder runs.
package audio

import (
	"encoding/binary"
	"io"
	"math"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const (
	InRate  = 16000 // what the STT endpoint hears
	OutRate = 24000 // what the TTS endpoint speaks (OpenAI's pcm)
)

// Level is the loudness of a PCM16 chunk, 0 to 1 (RMS, scaled so speech sits mid-range).
func Level(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) / 32768
		sum += v * v
	}
	l := math.Sqrt(sum/float64(n)) * 4
	return math.Min(1, l)
}

// silentRMS is -62 dBFS: 6 dB over the desk's empty input (its loudest 20 ms: -68.4 dBFS RMS), and far
// under a voice's syllables (-50 to -20 dBFS on a working mic). No real voice has been measured on it yet.
const silentRMS = 32768 * 0.000794

// Blocked says whether a recording is digital silence: no sample above 2 of 32768. macOS gives exactly
// that when the microphone is blocked for the app that started the recorder (a quiet room still
// has noise well above it).
func Blocked(pcm []byte) bool {
	n := len(pcm) / 2
	if n < InRate/4 {
		return false
	}
	for i := 0; i < n; i++ {
		v := int16(binary.LittleEndian.Uint16(pcm[2*i:]))
		if v > 2 || v < -2 {
			return false
		}
	}
	return true
}

// Silent says whether no 20 ms of a 16 kHz PCM16 recording reaches -62 dBFS RMS. A speech model may hear
// such silence as words ("Yeah."), so it is an empty turn without asking it.
func Silent(pcm []byte) bool {
	const window = InRate / 50
	n := len(pcm) / 2
	for start := 0; start < n; start += window {
		end := min(start+window, n)
		var sum float64
		for i := start; i < end; i++ {
			v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:])))
			sum += v * v
		}
		if math.Sqrt(sum/float64(end-start)) >= silentRMS {
			return false
		}
	}
	return true
}

// RecordCmd is the mic command: sox's rec from the default input, 16 kHz mono PCM16 on stdout. The first
// recording asks macOS for the microphone permission of the app that started vcd. Tests swap it.
var RecordCmd = func() *exec.Cmd {
	return exec.Command("rec", "-q", "--buffer", "1024", "-t", "raw", "-r", "16000", "-e", "signed-integer",
		"-b", "16", "-c", "1", "-")
}

// PlayCmd is the speaker command for 24 kHz PCM16 on stdin (sox's play to the default output).
var PlayCmd = func() *exec.Cmd {
	return exec.Command("play", "-q", "--buffer", "2048", "-t", "raw", "-r", "24000", "-e", "signed-integer",
		"-b", "16", "-c", "1", "-")
}

// Recorder holds one turn's audio in memory.
type Recorder struct {
	cmd    *exec.Cmd
	mu     sync.Mutex
	pcm    []byte
	max    int
	Opened time.Time // when the first audio arrived: the mic is really open
	done   chan struct{}
	full   chan struct{} // closed when max is reached
}

// Record opens the mic. onLevel gets each chunk's level; it runs until Stop or max.
func Record(max time.Duration, onLevel func(float64)) (*Recorder, error) {
	r := &Recorder{cmd: RecordCmd(), max: int(max.Seconds() * InRate * 2), done: make(chan struct{}),
		full: make(chan struct{})}
	out, err := r.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := r.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		defer close(r.done)
		buf := make([]byte, 3200) // 100 ms
		for {
			n, err := out.Read(buf)
			if n > 0 {
				r.mu.Lock()
				if r.Opened.IsZero() {
					r.Opened = time.Now()
				}
				room := r.max - len(r.pcm)
				if n > room {
					n = room
				}
				r.pcm = append(r.pcm, buf[:n]...)
				isFull := len(r.pcm) >= r.max
				r.mu.Unlock()
				if onLevel != nil {
					onLevel(Level(buf[:n]))
				}
				if isFull {
					close(r.full)
					r.kill()
					io.Copy(io.Discard, out)
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return r, nil
}

// Full is closed when the turn reached its cap.
func (r *Recorder) Full() <-chan struct{} { return r.full }

func (r *Recorder) kill() {
	if r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
}

// Stop closes the mic and returns the audio.
func (r *Recorder) Stop() []byte {
	r.kill()
	<-r.done
	r.cmd.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pcm
}

// OpenedAt is when the first audio came in (zero if none yet).
func (r *Recorder) OpenedAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Opened
}

// Player plays one answer. Write queues and returns at once; its own goroutine feeds pw-play, so a
// full pipe never holds up the caller (or a stop).
type Player struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	mu      sync.Mutex
	cond    *sync.Cond
	queue   [][]byte
	closing bool // Drain: no more writes, exit once the queue is out
	stopped atomic.Bool
	fed     chan struct{} // the feeder is done
	done    chan struct{} // pw-play has exited
	end     time.Time     // when what was queued so far will have played
}

func Play() (*Player, error) {
	p := &Player{cmd: PlayCmd(), done: make(chan struct{}), fed: make(chan struct{})}
	p.cond = sync.NewCond(&p.mu)
	in, err := p.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	p.in = in
	if err := p.cmd.Start(); err != nil {
		return nil, err
	}
	go func() { p.cmd.Wait(); close(p.done) }()
	go p.feed()
	return p, nil
}

func (p *Player) feed() {
	defer close(p.fed)
	defer p.in.Close()
	for {
		p.mu.Lock()
		for len(p.queue) == 0 && !p.closing && !p.stopped.Load() {
			p.cond.Wait()
		}
		if p.stopped.Load() || len(p.queue) == 0 {
			p.mu.Unlock()
			return
		}
		chunk := p.queue[0]
		p.queue = p.queue[1:]
		p.mu.Unlock()
		if _, err := p.in.Write(chunk); err != nil {
			return
		}
	}
}

func (p *Player) Write(pcm []byte) error {
	if p.stopped.Load() {
		return io.ErrClosedPipe
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if now := time.Now(); p.end.Before(now) {
		p.end = now
	}
	p.end = p.end.Add(time.Duration(len(pcm)/2) * time.Second / OutRate)
	p.queue = append(p.queue, append([]byte{}, pcm...))
	p.cond.Signal()
	return nil
}

// Remaining is roughly how much queued audio is still to be heard.
func (p *Player) Remaining() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return max(0, time.Until(p.end))
}

// Drain waits until everything written has played (or Stop is called).
func (p *Player) Drain() {
	p.mu.Lock()
	p.closing = true
	p.cond.Signal()
	p.mu.Unlock()
	<-p.done
}

// Stop drops whatever is still queued or buffered, at once.
func (p *Player) Stop() {
	p.stopped.Store(true)
	p.mu.Lock()
	p.queue = nil
	p.cond.Signal()
	p.mu.Unlock()
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	<-p.done
}

// Done is closed when the player has exited (drained or stopped).
func (p *Player) Done() <-chan struct{} { return p.done }

// Done is closed once the recorder has stopped reading (released, cancelled or full).
func (r *Recorder) Done() <-chan struct{} { return r.done }

// To16k takes 24 kHz PCM16 to 16 kHz (linear; enough for speech recognition).
func To16k(in []byte) []byte {
	n := len(in) / 2
	at := func(i int) float64 { return float64(int16(binary.LittleEndian.Uint16(in[2*i:]))) }
	out := make([]byte, 0, n*2*2/3+2)
	for j := 0; ; j++ {
		x := float64(j) * 1.5
		i := int(x)
		if i+1 >= n {
			break
		}
		v := at(i) + (at(i+1)-at(i))*(x-float64(i))
		out = binary.LittleEndian.AppendUint16(out, uint16(int16(v)))
	}
	return out
}
