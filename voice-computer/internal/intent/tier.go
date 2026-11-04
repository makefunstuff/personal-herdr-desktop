package intent

// Safety tiers: looking, showing, notes and reminders run at once; sending words to an agent never does.
const (
	Tier0 = 0 // just do it; the overlay says what was done
	Tier1 = 1 // a confirm: a spoken yes or the confirm key
	Tier2 = 2 // a confirm: the key only, never voice
)

var tiers = map[string]int{
	"needs_me": Tier0, "briefing": Tier0, "list_agents": Tier0, "agent_doing": Tier0, "read_latest": Tier0,
	"focus": Tier0, "go_to_space": Tier0, "agent_step": Tier0, "switch_layout": Tier0, "shepherdr_panel": Tier0,
	"write_note": Tier0, "list_notes": Tier0, "last_note": Tier0, "web_search": Tier0, "read_page": Tier0, "browse": Tier0, "read_app": Tier0, "open_app": Tier0, "pomodoro": Tier0, "about_me": Tier0, "forget_talk": Tier0, "forget_conversation": Tier1, "remind": Tier0, "list_reminders": Tier0, "cancel_reminder": Tier0,
	"volume": Tier0, "media": Tier0, "remember": Tier0, "forget": Tier0, "recall": Tier0,
	"new_tab":       Tier0, // in a space on another machine: Tier1 (it starts a program there)
	"send_to_agent": Tier1,
}

// TierOf is an intent's tier. untrusted means text an app or an agent wrote is in the session: then
// nothing runs without a confirm. An intent this table doesn't know is tier 2.
func TierOf(name string, untrusted bool) int {
	t, ok := tiers[name]
	if !ok {
		t = Tier2
	}
	if untrusted && t < Tier1 {
		t = Tier1
	}
	return t
}
