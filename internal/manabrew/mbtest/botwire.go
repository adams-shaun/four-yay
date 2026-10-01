package mbtest

import (
	"context"
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	manabrew "github.com/adams-shaun/gorge/internal/manabrew"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// BotWireSeat (MBX-4) is a seat.Seat that answers every engine decision with
// the PRODUCTION default bot (seat.NewBot's policy), but routes that answer
// through the ManaBrew mapping: the native intent the bot chose is converted
// to a ManaBrew client message with ResponseForIntent (MBX-3's inverse
// helper), round-tripped through the real wire codec, and handed to
// Translator.TranslateResponse exactly as a real client's answer would be.
// The intent that reaches the engine is therefore the one the wire expressed
// -- if the prompt or the response grammar dropped anything the bot needed,
// ResponseForIntent errors here and the parity test fails on this seat
// instead of silently playing something else.
//
// It is the missing half of TranslatingSeat: TranslatingSeat answers from the
// prompt alone (a first-legal or prompt-shaped policy), while BotWireSeat
// answers from the full view the engine handed it -- but only its CHOICE
// travels to the engine through the wire, never its knowledge.
type BotWireSeat struct {
	// TranslatingSeat carries the Translator, the shared Census plumbing and
	// decodedIntent, the shared translate tail; only Decide is overridden.
	*TranslatingSeat

	// Bot is the production bot policy this seat answers from.
	Bot *seat.Bot
}

// NewBotWireSeat builds a BotWireSeat whose bot is seat.NewBot(botSeed) and
// whose prompt translation runs over a fresh Translator for (table, match).
// census may be nil; when set it counts every pose, rejection and unmapped
// decision exactly as the census games do.
func NewBotWireSeat(table string, match int64, botSeed uint64, census *Census) *BotWireSeat {
	return &BotWireSeat{
		TranslatingSeat: NewTranslatingSeat(table, match, nil, census),
		Bot:             seat.NewBot(botSeed),
	}
}

// Decide implements seat.Seat: ask the production bot what a native client
// would submit, then send that choice through the ManaBrew wire path.
func (s *BotWireSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	s.Census.addEnumerated(d.Kind, d.Options)
	native, err := s.Bot.Decide(ctx, v, d)
	if err != nil {
		return decision.Intent{}, fmt.Errorf("mbtest: bot failed on the %s ask (seq %d): %w", d.Kind, d.Seq, err)
	}
	msg, _, err := s.Translator.PromptFlagged(&d, &v)
	if err != nil {
		s.Census.addUnmapped(d.Kind)
		return decision.Intent{}, fmt.Errorf("mbtest: prompt for %s (seq %d): %w", d.Kind, d.Seq, err)
	}
	s.Census.addPosed(msg.Input.Value.PromptType())
	pending := &manabrew.Pending{Prompt: msg, Decision: &d, View: v}
	resp, err := ResponseForIntent(pending, native)
	if err != nil {
		return decision.Intent{}, fmt.Errorf(
			"mbtest: the bot's native answer to the %s ask (seq %d) is not expressible on the ManaBrew wire: %w",
			d.Kind, d.Seq, err)
	}
	raw, err := mb.Encode(resp)
	if err != nil {
		return decision.Intent{}, fmt.Errorf("mbtest: wire-encoding the bot's %s answer (seq %d): %w", d.Kind, d.Seq, err)
	}
	return s.decodedIntent(msg, &d, v, raw)
}
