package host

import (
	"fmt"

	"github.com/adams-shaun/gorge/seat"
)

const (
	// BotPolicy is the stable deterministic hosted bot policy.
	BotPolicy = "bot"
	// LethalPressurePolicy is the measured opt-in hosted experiment.
	LethalPressurePolicy = "lethal-pressure"
	// CastProfilePolicy plays the production bot with a learned cast profile
	// (botpolicy.CastWeights) selected by name from the embedded profile set.
	// With the embedded default profile it is intent-identical to BotPolicy;
	// a tuned profile is how an experiment reaches a live table without a
	// rebuild. cmd/botbench's -profile flag overrides the weights per run.
	CastProfilePolicy = "cast-profile"
)

// NormalizeBotPolicy returns a hosted policy's stable name. An omitted name
// deliberately selects the production bot so tables saved before policy
// selection existed retain their behavior. The hosted vocabulary is closed:
// diagnostic policies such as legacy cannot silently become opponents.
func NormalizeBotPolicy(name string) (string, error) {
	if name == "" {
		return BotPolicy, nil
	}
	switch name {
	case BotPolicy, LethalPressurePolicy, CastProfilePolicy:
		return name, nil
	default:
		return "", fmt.Errorf("host: unknown bot policy %q (known: bot, lethal-pressure, cast-profile)", name)
	}
}

// NewBotPolicySeat builds a fresh deterministic seat for a supported hosted
// policy. The caller owns the per-seat seed derivation; the factory never
// reaches ambient randomness or substitutes a policy on an unknown name.
func NewBotPolicySeat(name string, seed uint64) (seat.Seat, error) {
	return NewBotPolicySeatWithAutoPayMana(name, seed, false)
}

// newCaretakerSeat builds the timeout caretaker for a human seat: the hosted
// policy's bot for that slot, except that with auto-pay it never selects a
// payment plan that pays life (spec §6: submitting a selector is consent, and
// a caretaker cannot consent for the human it stands in for). Every hosted
// policy constructor returns a *seat.Bot; anything else is returned as is.
func newCaretakerSeat(name string, seed uint64, autoPayMana bool) (seat.Seat, error) {
	s, err := NewBotPolicySeatWithAutoPayMana(name, seed, autoPayMana)
	if err != nil {
		return nil, err
	}
	if b, ok := s.(*seat.Bot); ok && autoPayMana {
		b.SkipLifePlans()
	}
	return s, nil
}

// NewBotPolicySeatWithAutoPayMana builds a named hosted bot. When autoPayMana
// is set, the bot selects offered payment-plan witnesses instead of manually
// tapping mana sources; normal decision policy remains unchanged.
func NewBotPolicySeatWithAutoPayMana(name string, seed uint64, autoPayMana bool) (seat.Seat, error) {
	name, err := NormalizeBotPolicy(name)
	if err != nil {
		return nil, err
	}
	if name == LethalPressurePolicy {
		b := seat.NewLethalPressureBot(seed)
		if autoPayMana {
			b.EnableAutoPayMana()
		}
		return b, nil
	}
	if name == CastProfilePolicy {
		b, err := seat.NewCastProfileBot(seed)
		if err != nil {
			return nil, err
		}
		if autoPayMana {
			b.EnableAutoPayMana()
		}
		return b, nil
	}
	b := seat.NewBot(seed)
	if autoPayMana {
		b.EnableAutoPayMana()
	}
	return b, nil
}
