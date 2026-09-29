package host

import (
	"github.com/adams-shaun/gorge/bots"
	_ "github.com/adams-shaun/gorge/bots/bot"
	"github.com/adams-shaun/gorge/seat"
)

const (
	BotPolicy            = bots.BotPolicy
	LethalPressurePolicy = bots.LethalPressurePolicy
	CastProfilePolicy    = bots.CastProfilePolicy
)

// NormalizeBotPolicy returns a hosted policy's stable name.
func NormalizeBotPolicy(name string) (string, error) { return bots.Normalize(name) }

// NewBotPolicySeat builds a fresh deterministic seat for a supported hosted policy.
func NewBotPolicySeat(name string, seed uint64) (seat.Seat, error) {
	return NewBotPolicySeatWithAutoPayMana(name, seed, false)
}

// newCaretakerSeat builds the timeout caretaker for a human seat.
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

// NewBotPolicySeatWithAutoPayMana builds a named hosted bot.
func NewBotPolicySeatWithAutoPayMana(name string, seed uint64, autoPayMana bool) (seat.Seat, error) {
	return bots.New(name, bots.Options{Seed: seed, AutoPayMana: autoPayMana})
}
