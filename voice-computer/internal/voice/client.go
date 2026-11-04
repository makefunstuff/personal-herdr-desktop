// Package voice talks to the backends: OpenAI-compatible transcription and chat endpoints and
// llama-server's /tts, giving vcd realtime-style session events.
package voice

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"herdr-voice-computer/internal/config"
)

type Client struct {
	Backend             string
	STT, Chat, TTS      config.Endpoint
	ChatExtra, TTSExtra map[string]any
	HTTP                *http.Client
}

// New is the client for cfg's backend.
func New(cfg config.Config) *Client {
	return &Client{Backend: cfg.Backend, STT: cfg.STT, Chat: cfg.Chat, TTS: cfg.TTS,
		ChatExtra: cfg.ChatExtra, TTSExtra: cfg.TTSExtra}
}

// ErrNoKey: a key file isn't there (or can't be read); ErrRefused: the server said 401 or 403.
var (
	ErrNoKey   = errors.New("no key")
	ErrRefused = errors.New("key refused")
)

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// ReadKey reads a key file each time, so a key made or rotated later is picked up without a restart.
// No file configured means no key ("").
func ReadKey(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNoKey, path)
	}
	k := strings.TrimSpace(string(data))
	if k == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrNoKey, path)
	}
	return k, nil
}

func (c *Client) do(ctx context.Context, ep config.Endpoint, method, path string, body io.Reader, ctype string, auth bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, ep.URL+path, body)
	if err != nil {
		return nil, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if auth {
		k, err := ReadKey(ep.KeyFile)
		if err != nil {
			return nil, err
		}
		if k != "" {
			req.Header.Set("Authorization", "Bearer "+k)
		}
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		return nil, ErrRefused
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		resp.Body.Close()
		return nil, fmt.Errorf("%s %s: %s %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	return resp, nil
}

// Ping says whether ep answers: llama-server's /health (503 while its model loads), else GET
// /v1/models with its key. Any answer but 401, 403 or 503 is up (a server without either answers 404).
func (c *Client) Ping(ctx context.Context, ep config.Endpoint) error {
	for _, path := range []string{"/health", "/v1/models"} {
		req, err := http.NewRequestWithContext(ctx, "GET", ep.URL+path, nil)
		if err != nil {
			return err
		}
		k, err := ReadKey(ep.KeyFile)
		if err != nil {
			return err
		}
		if k != "" {
			req.Header.Set("Authorization", "Bearer "+k)
		}
		resp, err := c.http().Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ErrRefused
		case http.StatusServiceUnavailable:
			return ErrLoading
		case http.StatusNotFound:
			continue
		}
		return nil
	}
	return nil
}

// ErrLoading: the server is up but its model is still loading.
var ErrLoading = errors.New("the model is loading")

// Transcribe sends 16 kHz mono PCM16 to the STT endpoint and returns what was heard.
func (c *Client) Transcribe(ctx context.Context, pcm []byte) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if c.STT.Model != "" {
		w.WriteField("model", c.STT.Model)
	}
	w.WriteField("response_format", "json")
	f, _ := w.CreateFormFile("file", "turn.wav")
	f.Write(WAV(pcm, 16000))
	w.Close()
	resp, err := c.do(ctx, c.STT, "POST", "/v1/audio/transcriptions", &body, w.FormDataContentType(), true)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Text), nil
}

// Speech streams the TTS endpoint's 24 kHz PCM16 for text; the caller closes the reader (which stops it).
// llama-server's /tts with "stream" answers a WAV whose sizes are unknown (0xffffffff): its header is
// skipped, so the reader gives bare PCM.
func (c *Client) Speech(ctx context.Context, text, voice string) (io.ReadCloser, error) {
	req := map[string]any{"input": text, "voice": voice, "stream": true}
	for k, v := range c.TTSExtra {
		req[k] = v
	}
	body, _ := json.Marshal(req)
	resp, err := c.do(ctx, c.TTS, "POST", "/tts", bytes.NewReader(body), "application/json", true)
	if err != nil {
		return nil, err
	}
	return &wavBody{rc: resp.Body}, nil
}

// wavBody reads a WAV stream's data chunk: the RIFF header and the chunks before "data" are skipped.
type wavBody struct {
	rc     io.ReadCloser
	header bool
}

func (w *wavBody) Close() error { return w.rc.Close() }

func (w *wavBody) Read(p []byte) (int, error) {
	if !w.header {
		w.header = true
		head := make([]byte, 12)
		if _, err := io.ReadFull(w.rc, head); err != nil {
			return 0, err
		}
		if string(head[:4]) != "RIFF" { // not a WAV: bare PCM (a fake, or a server that sends pcm)
			n := copy(p, head)
			return n, nil
		}
		for {
			ch := make([]byte, 8)
			if _, err := io.ReadFull(w.rc, ch); err != nil {
				return 0, err
			}
			size := binary.LittleEndian.Uint32(ch[4:])
			if string(ch[:4]) == "data" {
				break
			}
			if _, err := io.CopyN(io.Discard, w.rc, int64(size+size%2)); err != nil {
				return 0, err
			}
		}
	}
	return w.rc.Read(p)
}

// Post sends a JSON body to ep and returns the answer (a plain, non-streamed request).
func (c *Client) Post(ctx context.Context, ep config.Endpoint, path string, body io.Reader) ([]byte, error) {
	resp, err := c.do(ctx, ep, "POST", path, body, "application/json", true)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// WAV wraps mono PCM16 in a WAV header.
func WAV(pcm []byte, rate int) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+len(pcm)))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}

// Timeout is how long a plain request may take.
const Timeout = 10 * time.Second
