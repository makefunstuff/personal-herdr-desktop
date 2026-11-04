package daemon

import (
	"context"
	"regexp"
	"strings"
	"time"

	"herdr-voice-computer/internal/config"
	"herdr-voice-computer/internal/convo"
	"herdr-voice-computer/internal/intent"
)

// The conversation, kept on this Mac (conversation.jsonl and its summary): reloaded at start, the newest
// turns replayed within the token budget, older ones restated in the summary.

// loadConversation reads the last turns back (the voice window shows them) and prunes old ones.
func (d *Daemon) loadConversation() {
	d.convo = convo.New(config.DataDir(), d.cfg.ConversationDays)
	d.convo.Prune(time.Now())
	for _, t := range d.convo.Last(d.cfg.History) {
		d.turns = append(d.turns, Turn{You: t.You, Computer: t.Computer, At: t.At})
	}
}

func (d *Daemon) lastKind() string {
	if d.lastLog != nil && time.Since(d.lastLogAt) < time.Minute {
		return d.lastLog.Kind
	}
	return ""
}

// recentTurnsLocked are the turns to replay into a new session: the newest ones within the token budget
// (about four characters a token); d.mu is held.
func (d *Daemon) recentTurnsLocked() []Turn {
	budget := d.cfg.ContextTokens * 4
	var out []Turn
	for i := len(d.turns) - 1; i >= 0; i-- {
		n := len(d.turns[i].You) + len(d.turns[i].Computer) + 40
		if d.cfg.ContextTokens > 0 && n > budget && len(out) > 0 {
			break
		}
		budget -= n
		out = append(out, d.turns[i])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// summarizeOlder restates the turns that no longer fit the replay in the summary (after the answer
// was spoken: it costs the owner no time).
func (d *Daemon) summarizeOlder() {
	d.mu.Lock()
	recent := len(d.recentTurnsLocked())
	d.mu.Unlock()
	all := d.convo.All()
	if older := len(all) - recent; older > 0 {
		d.convo.Summarize(all[:older], 400)
	}
}

var commentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

func stripComments(s string) string {
	s = commentRE.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "# Earlier conversations", "")
	return strings.TrimSpace(s)
}

func (d *Daemon) planForgetConversation() *action {
	n := len(d.convo.All())
	a := runner{func(ctx context.Context, _ string) (string, []Done, error) {
		if err := d.convo.Clear(); err != nil {
			return "", nil, err
		}
		d.mu.Lock()
		d.turns = nil
		d.memStale = true
		d.mu.Unlock()
		return both("cleared the conversation", d.phrase("convo.cleared", nil)), []Done{{Text: "Conversation cleared"}}, nil
	}}.act("Forget our conversation", "", "Forget it")
	a.card.Ask = d.phrase("convo.clear.ask", map[string]string{"n": plainCount(n)})
	a.declined = d.phrase("convo.kept", nil)
	a.minTier = intent.Tier1
	return a
}

func plainCount(n int) string {
	switch {
	case n == 0:
		return "nothing yet"
	case n < 10:
		return "a few turns"
	case n < 60:
		return "a few dozen turns"
	}
	return "a lot of turns"
}

func (d *Daemon) planForgetTalk(c intent.Call) *action {
	about := c.Str("about")
	return tell(func(ctx context.Context) (string, string) {
		n, _ := d.convo.ForgetAbout(about)
		q := strings.ToLower(about)
		d.mu.Lock()
		keep := d.turns[:0]
		for _, t := range d.turns {
			if !strings.Contains(strings.ToLower(t.You+" "+t.Computer), q) {
				keep = append(keep, t)
			}
		}
		d.turns = keep
		d.memStale = true // the next session starts without them
		d.mu.Unlock()
		if n == 0 {
			return "found nothing like it", d.phrase("forget.none", nil)
		}
		return "forgot what was said about it", d.phrase("convo.forgot", nil)
	})
}

func init() {
	phrases["convo.clear.ask"] = []string{"Forget our whole conversation, {n} and the summary of earlier days?", "Start fresh and clear everything we've talked about, {n}?"}
	phrases["convo.cleared"] = []string{"Done, clean slate.", "Okay, starting fresh."}
	phrases["convo.kept"] = []string{"Okay, I'll keep it.", "Got it, nothing forgotten."}
	phrases["convo.forgot"] = []string{"Done, I've forgotten what you said about that.", "Okay, that's gone from our conversation."}
}
