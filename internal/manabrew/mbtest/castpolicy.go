package mbtest

// MBX-7's vote fixtures need a seat that can BUILD the mana for a spell and
// cast it. The wire-only mock client cannot express that policy: the
// priority prompt carries no phase, so from the prompt alone a client cannot
// tell a (pool-wasting) upkeep activation from a main-phase one -- and the
// first-legal client's greedy activations at every priority leave every
// turn's mana spent before the main phase reaches it, so a spell with a
// multi-mana cost is never castable. The PickPolicy sees the view (the
// phase is a view field), which is exactly the information a real
// ManaBrew client has of its own game; the fixture policy below uses only
// that.

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/view"
)

// CastSeekerPolicy returns a PickPolicy that plays one spell-driven game
// plan: cast any of spells as soon as the priority offers it (the option
// only appears once the pool can pay its cost), otherwise -- on the seat's
// own main phases -- play a land, then activate a mana source, accumulating
// until the cast is affordable; pass everywhere else (the wire pass output,
// so pool is never spent outside the phase that will use it). Each seat of
// the vote fixtures uses its own CastSeekerPolicy (the caster casts the vote
// spell, each voter casts its ballot fodder), so all of them drive mana the
// way a real client would: never outside a main phase of its own turn,
// never wasting pool across a step boundary.
//
// Every pick is the identical wire shape a real client's response carries
// (an act on "opt-<i>", or the pass output), so everything between the pick
// and the engine (wire codec, translate, validate, census) is unchanged.
func CastSeekerPolicy(spells ...string) PickPolicy {
	return func(v view.View, d *decision.Decision) (mb.PromptOutputValue, bool) {
		if d.Kind != decision.KPriority {
			return nil, false
		}
		cast, land, act := -1, -1, -1
		for _, o := range d.Options {
			switch o.Kind {
			case "cast":
				if cast < 0 {
					for _, spell := range spells {
						if o.Label == "Cast "+spell {
							cast = o.Index
							break
						}
					}
				}
			case "play_land":
				if land < 0 {
					land = o.Index
				}
			case "activate":
				if act < 0 {
					act = o.Index
				}
			}
		}
		if cast >= 0 {
			return mb.ActOutput{ActionID: fmt.Sprintf("opt-%d", cast)}, true
		}
		if ownMain(v) && land >= 0 {
			return mb.ActOutput{ActionID: fmt.Sprintf("opt-%d", land)}, true
		}
		if ownMain(v) && act >= 0 {
			return mb.ActOutput{ActionID: fmt.Sprintf("opt-%d", act)}, true
		}
		// Everything else: the pass output (never an act on the pass option,
		// which the translator rejects -- §6.3).
		return mb.PassOutput{}, true
	}
}

// ownMain reports whether the seat deciding v is the active player in one of
// its own main phases (the only window a land drop or a sorcery-speed cast
// is legal in, and the only place this policy is willing to spend
// activations).
func ownMain(v view.View) bool {
	return v.Active == v.Viewer && (v.Step == "main1" || v.Step == "main2")
}
