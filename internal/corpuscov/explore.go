package corpuscov

import (
	"context"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// ExploreSeat is coverage-directed exploration: a wrapper over any seat that,
// at a priority decision, with probability P forces the least-chosen offered
// (card, ability) slot instead of asking the wrapped policy. "Least chosen"
// reads the shared Census, so the corpus is steered toward the slots it has
// shown least. Only priority decisions are forced; every follow-up decision
// a forced action poses (targets, payment, modes) goes to the wrapped seat.
//
// Determinism: the coin and the tie-break draw from a PCG seeded by the
// caller (no ambient randomness), and the census is a pure function of the
// game history, so a run replays exactly. The wrapped seat's RNG is not
// advanced on a forced decision.
//
// Labelling: Explored reports whether the LAST answer was forced; the driver
// copies it into the decision record ("explore": true) so an imitation
// learner drops those rows -- they are not the teacher's choices.
type ExploreSeat struct {
	Inner  seat.Seat
	P      float64
	Census *Census
	Game   func() *state.Game

	r        *rand.Rand
	explored bool
}

// NewExploreSeat wraps inner. p<=0 never explores (the wrapper is then
// transparent).
func NewExploreSeat(inner seat.Seat, p float64, seed uint64, c *Census, g func() *state.Game) *ExploreSeat {
	return &ExploreSeat{Inner: inner, P: p, Census: c, Game: g, r: rand.New(rand.NewPCG(seed, seed^0xc0ffee5eed))}
}

// Explored reports whether the last Decide was a forced exploration.
func (s *ExploreSeat) Explored() bool { return s.explored }

// Decide implements seat.Seat.
func (s *ExploreSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	s.explored = false
	if s.P > 0 && d.Kind == decision.KPriority {
		if pick, ok := s.pick(&d); ok {
			s.explored = true
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}, nil
		}
	}
	return s.Inner.Decide(ctx, v, d)
}

// pick draws the coin, then the least-chosen card-bearing option (uniform
// among ties). Mana activations are excluded: tapping for mana is not an
// action the census wants more of, and a forced float strands nothing.
func (s *ExploreSeat) pick(d *decision.Decision) (int, bool) {
	if s.r.Float64() >= s.P {
		return 0, false
	}
	g := s.Game()
	best, n := -1, 0
	var cands []int
	for i := range d.Options {
		o := &d.Options[i]
		if o.Kind == "mana" {
			continue
		}
		card, key, ok := OptionKey(g, d, o)
		if !ok {
			continue
		}
		c := s.Census.Chosen(card, key)
		switch {
		case best < 0 || c < n:
			best, n = i, c
			cands = append(cands[:0], i)
		case c == n:
			cands = append(cands, i)
		}
	}
	if best < 0 {
		return 0, false
	}
	return cands[s.r.IntN(len(cands))], true
}

// WantsPaymentActions forwards the wrapped seat's payment-plan opt-in, so a
// host or driver builds the extension exactly when the policy reads it.
func (s *ExploreSeat) WantsPaymentActions() bool {
	pc, ok := s.Inner.(seat.PaymentPlanConsumer)
	return ok && pc.WantsPaymentActions()
}
