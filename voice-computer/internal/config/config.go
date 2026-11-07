// Package config holds vcd's settings: built-in defaults, overridden by
// ~/.config/herdr-voice-computer/config.json when it exists.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Endpoint is one OpenAI-compatible server: its base URL (without /v1), the model to ask for, and
// the file holding its API key (mode 600, never in git; empty for a server that needs no key).
type Endpoint struct {
	URL     string `json:"url"`
	Model   string `json:"model"`
	KeyFile string `json:"key_file"`
}

// SearchConfig is how vcd looks things up: DeepSeek's search when its key file is there, else
// DuckDuckGo and Wikipedia (SearXNG or Brave when set). Keys are only ever read from their files.
type SearchConfig struct {
	Enabled         bool   `json:"enabled"`
	DeepSeekKeyFile string `json:"deepseek_key_file"`
	SearxngURL      string `json:"searxng_url"`
	BraveKeyFile    string `json:"brave_key_file"`
}

// Server is one llama-server vcd starts and supervises (an empty Model: vcd starts none and only
// uses the endpoint, a server you run yourself).
type Server struct {
	Model  string   `json:"model"`  // the GGUF file
	MMProj string   `json:"mmproj"` // the multimodal projector, for the audio model
	Port   int      `json:"port"`
	Args   []string `json:"args"` // more llama-server arguments
}

// Shepherdr is one Shepherdr whose snapshot vcd reads: this Mac's own (Machine empty: fetched on
// loopback), or another machine's, read with `hc exec --on <machine> curl …` as the Herdr shepherdr mod
// does (read-only).
type Shepherdr struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Machine string `json:"machine"`
	// HerdrMachine is the Herdr machine that shows its agents' terminals (for "focus" and "tell").
	HerdrMachine string `json:"herdr_machine"`
}

type Config struct {
	// Which backend answers: "openai" (STT, chat and TTS as OpenAI-compatible endpoints; the only one here).
	Backend string `json:"backend"`
	// Speech to text: POST {url}/v1/audio/transcriptions (llama-server with the LFM3 audio model).
	STT Endpoint `json:"stt"`
	// The brain: POST {url}/v1/chat/completions with tools (llama-server with LFM3).
	Chat Endpoint `json:"chat"`
	// ChatFallback: where the brain's requests go when Chat can't be reached or errs (the local brain).
	ChatFallback Endpoint `json:"chat_fallback"`
	// Text to speech: POST {url}/tts with {"input", "voice", "stream": true}: a 24 kHz mono PCM16 WAV stream
	// (llama-server with the LFM3 audio model).
	TTS Endpoint `json:"tts"`
	// The llama-servers vcd runs: the brain, the audio model (ears and mouth) and the embedding model
	// (EmbeddingGemma, for recall over notes, memory and web results).
	Brain    Server `json:"brain_server"`
	Audio    Server `json:"audio_server"`
	EmbedSrv Server `json:"embed_server"`
	// POST {url}/v1/embeddings.
	Embed Endpoint `json:"embed"`
	// Web search.
	Search SearchConfig `json:"search"`
	// The owner's visible notes: daily/YYYY-MM-DD.md under it.
	NotesDir   string `json:"notes_dir"`
	LlamaServe string `json:"llama_server"` // the llama-server command
	// The secretary's name, said in its instructions ("Herdr").
	Name string `json:"name"`
	// The owner's name, used now and then (default: the first word of git's user.name).
	Owner string `json:"owner"`
	// What the secretary remembers between conversations (markdown the owner can edit).
	MemoryFile string `json:"memory_file"`
	// The TTS voice (female or male for LFM3 audio).
	Voice string `json:"voice"`
	// Extra fields for every chat request, e.g. {"temperature": 0.2}.
	ChatExtra map[string]any `json:"chat_extra"`
	// Extra fields for every TTS request.
	TTSExtra map[string]any `json:"tts_extra"`
	// The keys the prompts and the mod name.
	TalkKey    string `json:"talk_key"`
	ConfirmKey string `json:"confirm_key"`
	// A press released sooner than this is a tap (stop).
	TapMS int `json:"tap_ms"`
	// The mic closes after this long, and what was heard is sent as the turn.
	MaxTurnS int `json:"max_turn_s"`
	// The computer's session closes after this long without a turn.
	IdleCloseS int `json:"idle_close_s"`
	// The overlay hides this long after an answer ends.
	OverlayHideS int `json:"overlay_hide_s"`
	// Turns kept (in conversation.jsonl too) for the overlay and replayed into a new session.
	History int `json:"history"`
	// The recent turns the brain sees each time, in tokens (older ones live on in the summary).
	ContextTokens int `json:"context_tokens"`
	// How long whole turns are kept on disk; older ones only in the summary.
	ConversationDays int `json:"conversation_days"`
	// The computer brain's standing instructions (fixed for a session, so they stay cached).
	Instructions    string `json:"instructions"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	// A spoken answer stops after this many sentences (0: never); the instructions ask for one.
	MaxSentences int `json:"max_sentences"`
	// Think: the brain thinks privately before a turn (a short request of its own, about a second).
	Think bool `json:"think"`
	// Learn: after a turn, what the owner said about themselves or how you should talk goes to memory.
	Learn bool `json:"learn"`
	// DictationOnly: only `vc dictate` (LFM3-Audio writes speech down for a text box); no talking, brain,
	// announcements, memory or recall, and only the audio model is started.
	DictationOnly bool `json:"dictation_only"`
	// Flavor: what vcd says on its own is said the secretary's way, varied (a cloud brain only).
	Flavor bool `json:"flavor"`
	// FlavorModel: the model for it ("": DeepSeek's chat with DeepSeek, else the brain's).
	FlavorModel string `json:"flavor_model"`
	// A card cancels itself after this long.
	CardTimeoutS int `json:"card_timeout_s"`
	// "note: …" appends here, timestamped.
	NotesFile string `json:"notes_file"`
	// The Shepherdrs read for "what needs me", the briefing, announcements and agent replies.
	Shepherdrs []Shepherdr `json:"shepherdrs"`
	// Agent replies: the Shepherdr spaces whose coordinators' replies are spoken (none: off).
	ReplySpaces []string `json:"reply_spaces"`
	// The TTS voice for agents ("": the computer's voice).
	ReplyVoice string `json:"reply_voice"`
	// How often vcd reads Herdr's agents and Shepherdr.
	ReplyPollMS int `json:"reply_poll_ms"`
	// A reply to Shepherdr's news (not to the owner) waits until the owner has been idle this long.
	ReplyIdleS int `json:"reply_idle_s"`
	// Announcements: one line at most this often; events that come together are said together.
	AnnounceGapS int `json:"announce_gap_s"`
	// The commands vcd drives Herdr with.
	HerdrApp string `json:"herdr_app"`
	HC       string `json:"hc"`
	// The layouts "theme …" may switch to.
	Layouts []string `json:"layouts"`
	// HTTP on loopback for the Herdr mod (0: none).
	HTTPPort int `json:"http_port"`

	// The file's own keys.
	Raw map[string]json.RawMessage `json:"-"`
}

// DefaultInstructions is the brain's standing instructions, the original's honesty rules in a warm
// executive assistant's voice: %[1]s is the secretary's name, %[2]s the owner's, %[3]s this Mac's
// hostname, %[4]s the "unknown" hint. The examples hold no facts of their own.
const DefaultInstructions = `You are %[1]s, %[2]s's executive assistant inside Herdr, the app on this Mac (a computer called %[3]s) ` +
	`where %[2]s's terminals and coding agents run. %[2]s talks to you by pressing a key, and you answer out loud. ` +
	`You're warm, sharp, witty and a little unpredictable, like a friendly, capable assistant with a cheeky streak: ` +
	`you joke, tease %[2]s lightly and drop fun remarks now and then, never the same way twice, and never at the cost ` +
	`of getting things right. Natural spoken ` +
	`English with contractions, never markdown, lists or code. You're a personal helper having a conversation, not a ` +
	`voice command box: when %[2]s talks to you, answer in two to four natural sentences, react to what they said, and ` +
	`connect to what came up earlier in this conversation, in your memory or in their notes when it's relevant. An ` +
	`answer from a web search or the owner's notes can take up to five sentences and says where it came from ` +
	`("Wikipedia says …", "Your note from Tuesday says …"). Small talk is welcome: keep it short and human, then offer ` +
	`something useful when it fits. When you act on a command, say what you did in one lively sentence. When it fits, offer one ` +
	`useful next step as a short question, at most one per answer, and only one grounded in what the context, your ` +
	`tools or your memory actually show (an agent waiting, a reminder coming up, something from earlier today); never ` +
	`invent one. If %[2]s's style notes ask for shorter answers, they win. Use %[2]s's name now and then, not in every line.` + "\n\n" +
	`Honesty comes first, but be useful. Facts about %[2]s, their agents, spaces, notes and day come only from ` +
	`the context line before their words (the time, the date, the shown layout, the focused space, tab and agent, ` +
	`how many agents work or wait, the reminders), your tools' results, your memory and your earlier conversations ` +
	`with %[2]s (refer back to them naturally, "like you mentioned yesterday"): never invent any of those. For ` +
	`current facts about the world (prices, news, today's anything) call web_search, since your own knowledge may ` +
	`be out of date; never for Herdr, its agents or the owner's own things. You may think things through, do sums, ` +
	`give your opinion and advice, and explain what you know in general, saying when you're unsure. When the owner ` +
	`asks the computer to do something, use a tool, and never say you did something (wrote a note, sent a message, ` +
	`set a timer, opened a space) unless you called the tool for it in this turn. If you didn't understand, ask ` +
	`back briefly.` + "\n\n" +
	`When %[2]s tells you how to talk from now on ("from now on", "I prefer", "always", "never", "stop …"), or ` +
	`corrects you ("no, X is Y"), call remember first: kind style for how you talk, kind correction for a mistake, ` +
	`kind fact for something about %[2]s to keep. Never remember a password, a key or any other secret.` + "\n\n" +
	`How you sound:` + "\n" +
	`If %[2]s says "thanks", you say "Anytime."` + "\n" +
	`If %[2]s asks "what's twelve times seven", you say "Eighty-four."` + "\n" +
	`If %[2]s asks "who are you", you say "I'm %[1]s, your assistant here in Herdr. I keep an eye on your agents, take notes, look things up and run your timers. Want a quick briefing?"` + "\n" +
	`If %[2]s says "good morning", you say "Morning, %[2]s! Hope you slept well. Want me to tell you what your agents did overnight?"` + "\n" +
	`If %[2]s says "from now on keep it shorter", you call remember with kind style and text "Keep answers shorter.", then say "Got it, shorter from now on."` + "\n" +
	`If %[2]s says "I'm tired", you say "Sounds like a long one. I can keep things quiet for a while, or start a short break timer. Which would help?"`

// Instructions fills DefaultInstructions: unknown follows "you don't know".
func Instructions(name, owner, unknown string) string {
	host, _ := os.Hostname()
	if host == "" {
		host = "this machine"
	}
	host = strings.TrimSuffix(host, ".local")
	if owner == "" {
		owner = "the owner"
	}
	return fmt.Sprintf(DefaultInstructions, name, owner, host, unknown)
}

// DefaultOwner is the first word of git's user.name ("" when there is none).
func DefaultOwner() string {
	out, err := exec.Command("git", "config", "--global", "user.name").Output()
	if err != nil {
		return ""
	}
	f := strings.Fields(string(out))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// hfModel finds a file in the Hugging Face cache (the newest snapshot that has it).
// firstModel is the first of files that's downloaded (the card's choice first).
func firstModel(repo string, files ...string) string {
	for _, f := range files {
		if p := hfModel(repo, f); p != "" {
			return p
		}
	}
	return ""
}

func hfModel(repo, file string) string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".cache/huggingface/hub", "models--"+strings.ReplaceAll(repo, "/", "--"), "snapshots")
	m, _ := filepath.Glob(filepath.Join(dir, "*", file))
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1]
}

func Default() Config {
	owner := DefaultOwner()
	return Config{
		Backend:    "openai",
		STT:        Endpoint{URL: "http://127.0.0.1:8791", Model: "lfm3-audio"},
		Chat:       Endpoint{URL: "http://127.0.0.1:8790", Model: "lfm3"},
		TTS:        Endpoint{URL: "http://127.0.0.1:8791", Model: "lfm3-audio"},
		LlamaServe: "llama-server",
		Brain: Server{Model: hfModel("LiquidAI/LFM3-Omni-2B-GGUF", "LFM3-Omni-2B-Q4_K_M.gguf"), Port: 8790,
			Args: []string{"-c", "16384", "--reasoning", "off", "--jinja", "-np", "1"}}, // LFM3-Omni is a non-reasoning model (its card)
		Audio: Server{Model: hfModel("LiquidAI/LFM3-Audio-2B-GGUF", "LFM3-Audio-2B-Q4_K_M.gguf"),
			MMProj: firstModel("LiquidAI/LFM3-Audio-2B-GGUF", "mmproj-LFM3-Audio-2B-Q8_0.gguf", "mmproj-LFM3-Audio-2B-Q4_0.gguf"), Port: 8791,
			Args: []string{"-c", "8192", "-np", "2"}}, // two slots: an answer's speech and a filler or announcement at once
		EmbedSrv: Server{Model: filepath.Join(home(), ".local/share/nvim/semantic-grep/models/embeddinggemma-2-Q8_0.gguf"), Port: 8793,
			Args: []string{"--embedding", "-c", "2048", "-np", "1"}},
		Embed:            Endpoint{URL: "http://127.0.0.1:8793", Model: "embeddinggemma"},
		Search:           SearchConfig{Enabled: true, DeepSeekKeyFile: ""},
		NotesDir:         filepath.Join(home(), ".config/herdr-app/desk-notes"), // the Desk panel's (mods/desk)
		Name:             "Herdr",
		Voice:            "female",
		ChatExtra:        map[string]any{"temperature": 0.2},
		TalkKey:          "cmd-shift-m",
		ConfirmKey:       "cmd-shift-y",
		TapMS:            300,
		MaxTurnS:         30,
		IdleCloseS:       120,
		OverlayHideS:     8,
		History:          200, // kept and shown; the brain gets what fits ContextTokens, the rest condensed in the summary
		ContextTokens:    6000,
		ConversationDays: 30,
		Owner:            owner,
		Instructions:     Instructions("Herdr", owner, ""),
		MaxOutputTokens:  450,
		Think:            false, // LFM3-Omni-2B invents tool results in a forced thought: off until a brain reasons
		Learn:            true,
		Flavor:           true,
		MaxSentences:     5,
		CardTimeoutS:     30,
		NotesFile:        filepath.Join(DataDir(), "notes.md"),
		MemoryFile:       filepath.Join(DataDir(), "memory.md"),
		Shepherdrs:       []Shepherdr{{Name: "this Mac", URL: "http://127.0.0.1:7787", HerdrMachine: "mac-shepherdr"}},
		ReplyPollMS:      2000,
		ReplyIdleS:       30,
		AnnounceGapS:     20,
		HerdrApp:         "herdr-app",
		HC:               "hc",
		Layouts:          []string{"pixel", "win31", "workbench", "fortress", "main"},
		HTTPPort:         8792,
	}
}

func home() string { h, _ := os.UserHomeDir(); return h }

// Dir is where the config lives: $XDG_CONFIG_HOME/herdr-voice-computer.
func Dir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home(), ".config")
	}
	return filepath.Join(dir, "herdr-voice-computer")
}

// Path is the config file.
func Path() string { return filepath.Join(Dir(), "config.json") }

// StateDir holds the socket, the pid files, the log (timings only) and the preferences.
func StateDir() string {
	if d := os.Getenv("VC_STATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".local/state/herdr-voice-computer")
}

// DataDir holds what the owner asked to keep: the notes.
func DataDir() string {
	if d := os.Getenv("VC_DATA_DIR"); d != "" {
		return d
	}
	return filepath.Join(home(), ".local/share/herdr-voice-computer")
}

// Load reads path over the defaults; a missing file means the defaults.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, c.Check()
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c.Raw); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	if _, ok := c.Raw["instructions"]; !ok {
		c.Instructions = Instructions(c.Name, c.Owner, "")
	}
	return c, c.Check()
}

func checkURL(name, u string) error {
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return fmt.Errorf("%s.url must be an http(s) URL, got %q", name, u)
	}
	return nil
}

func expand(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home(), path[2:])
	}
	return path
}

func (c *Config) Check() error {
	if c.Backend != "openai" {
		return fmt.Errorf("backend must be openai, got %q", c.Backend)
	}
	for name, e := range map[string]*Endpoint{"stt": &c.STT, "tts": &c.TTS, "chat": &c.Chat} {
		if err := checkURL(name, e.URL); err != nil {
			return err
		}
	}
	for _, e := range []*Endpoint{&c.STT, &c.Chat, &c.ChatFallback, &c.TTS} {
		e.URL = strings.TrimRight(e.URL, "/")
		e.KeyFile = expand(e.KeyFile)
	}
	for _, s := range []*Server{&c.Brain, &c.Audio} {
		s.Model, s.MMProj = expand(s.Model), expand(s.MMProj)
	}
	c.NotesFile = expand(c.NotesFile)
	c.MemoryFile = expand(c.MemoryFile)
	c.NotesDir = expand(c.NotesDir)
	c.Search.DeepSeekKeyFile = expand(c.Search.DeepSeekKeyFile)
	c.Search.BraveKeyFile = expand(c.Search.BraveKeyFile)
	c.EmbedSrv.Model = expand(c.EmbedSrv.Model)
	c.Embed.URL = strings.TrimRight(c.Embed.URL, "/")
	if c.Name == "" {
		c.Name = "Herdr"
	}
	if c.CardTimeoutS <= 0 {
		return errors.New("card_timeout_s must be above 0")
	}
	if c.ReplyVoice == "" {
		c.ReplyVoice = c.Voice
	}
	if c.ReplyPollMS < 250 || c.ReplyIdleS < 0 {
		return errors.New("reply_poll_ms must be 250 or more, reply_idle_s 0 or more")
	}
	if c.TapMS <= 0 || c.MaxTurnS <= 0 || c.IdleCloseS <= 0 || c.OverlayHideS <= 0 || c.History < 0 {
		return errors.New("tap_ms, max_turn_s, idle_close_s and overlay_hide_s must be above 0, history 0 or more")
	}
	for _, s := range c.Shepherdrs {
		if err := checkURL("shepherdrs[].", s.URL); err != nil {
			return err
		}
	}
	return nil
}

// KeyFiles is every key file the backend uses, once each.
func (c Config) KeyFiles() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range []Endpoint{c.STT, c.TTS, c.Chat} {
		if e.KeyFile != "" && !seen[e.KeyFile] {
			seen[e.KeyFile] = true
			out = append(out, e.KeyFile)
		}
	}
	return out
}

func (c Config) Tap() time.Duration       { return time.Duration(c.TapMS) * time.Millisecond }
func (c Config) MaxTurn() time.Duration   { return time.Duration(c.MaxTurnS) * time.Second }
func (c Config) IdleClose() time.Duration { return time.Duration(c.IdleCloseS) * time.Second }
func (c Config) OverlayHide() time.Duration {
	return time.Duration(c.OverlayHideS) * time.Second
}
func (c Config) CardTimeout() time.Duration { return time.Duration(c.CardTimeoutS) * time.Second }
func (c Config) ReplyPoll() time.Duration   { return time.Duration(c.ReplyPollMS) * time.Millisecond }
func (c Config) ReplyIdle() time.Duration   { return time.Duration(c.ReplyIdleS) * time.Second }
func (c Config) AnnounceGap() time.Duration { return time.Duration(c.AnnounceGapS) * time.Second }
