// encore.go implements the Encore keyword's resolution (CR 702: "Encore
// <cost> — Exile this card from your graveyard: For each opponent, create a
// token copy that attacks that opponent this turn if able. They gain haste.
// Sacrifice them at the beginning of the next end step. Activate only as a
// sorcery.").
//
// The activated ABILITY itself is not written here: cards/keywords.go
// expands K:Encore:<cost> into an AB$ Encore ability in the graveyard whose
// Cost$ carries the printed cost plus ExileFromGrave<1/CARDNAME> (the card
// exiles itself as the cost's payment, settled by the ordinary cast-flow
// exile stage), ActivationZone$ Graveyard and SorcerySpeed$ True -- so the
// offer gate, the payment machinery and the CR 602.2b flow are the ordinary
// ones and this primitive only resolves the effect.
//
// CardToken carries each copy's required defender into state; the combat
// declaration solver enforces "attacks that opponent this turn if able".
// The rest is one CardToken copy per opponent (a copy of the card object itself,
// whatever zone it resolved from -- the cost exiled it, so exile), haste
// granted until end of turn through the layer system, and ONE end-step delayed
// trigger remembering the whole token group. Countering that single trigger
// therefore saves every surviving token, matching one Encore activation.
package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Encore", effEncore) }

func effEncore(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	// "For each opponent": the opponents of the ACTIVATOR (c.Controller),
	// in AliveFrom's fixed seat order -- never a map, so the token order is
	// replay-stable. The card is in exile (its own cost exiled it), but the
	// copy is created from the object wherever it sits.
	var opponents, tokens, parked = []state.PlayerID(nil), []state.ObjID(nil), []state.ObjID(nil)
	start := 0
	rest := resumingMint(c, sa)
	if rest != nil {
		// A copy parked behind a CR 616.1 order ask (a staged entry-counter
		// order on the copied card, a CreateToken order) and the answer has
		// minted it: take its haste and group membership, then continue
		// with the opponents after it (tokenRest's continuation).
		opponents, tokens, parked, start = rest.Players, rest.Minted, rest.Parked, rest.Next
	} else {
		src := g.Obj(c.Source)
		if src == nil || src.Card == nil {
			return
		}
		for _, p := range g.AliveFrom(c.Controller) {
			if p != c.Controller {
				opponents = append(opponents, p)
			}
		}
	}
	// The CreateToken replacements size each opponent's creation (Doubling
	// Season's "twice that many"), proposed once on the first pass and frozen
	// in the TokenRest so a resumed pass mints the same copies (Count is the
	// parked copy's index within its opponent's creation).
	var counts []int32
	inner := 0
	if rest != nil {
		counts, inner = rest.Counts, int(rest.Count)
	}
	if len(counts) != len(opponents) {
		counts = make([]int32, len(opponents))
		for i := range counts {
			counts[i] = 1
			if rest == nil {
				counts[i] = proposeCopyTokens(h, c.Controller, c.Source, 1)
			}
		}
	}
	for i := start; i < len(opponents); i++ {
		p := opponents[i]
		for k := 0; k < int(counts[i]); k++ {
			var minted []state.ObjID
			resumed := rest != nil && i == start
			if resumed && k < inner {
				continue
			}
			if resumed && k == inner {
				minted = parked
			} else {
				want := g.NextID
				wasSuspended := h.Suspended()
				// Amount encodes defender+1 for events.Apply: the token's required
				// opponent is replay-derived state, not an effects-side mutation.
				minted = h.EmitTokenCreate(events.Event{Kind: events.CardToken, Obj: c.Source, Player: c.Controller,
					Amount: int32(p) + 1})
				if !wasSuspended && h.Suspended() {
					// The copy parked the resolution: what landed takes its
					// haste now, and the parked copy, the copies and opponents
					// after it and the group registration resume with the answer.
					tokens = encoreGrantHaste(h, c, minted, tokens)
					if suspendMint(h, c, TokenRest{SA: sa, Next: i, Count: int32(k), Counts: counts,
						Minted: tokens, Players: opponents}) {
						return
					}
					continue
				}
				if len(minted) == 0 {
					minted = []state.ObjID{want}
				}
			}
			tokens = encoreGrantHaste(h, c, minted, tokens)
		}
	}
	// One activation creates one delayed triggered ability, remembering all
	// token identities. DelayedPush carries the group into the builtin
	// sacrifice SA's DelayTriggerRememberedLKI definition.
	if len(tokens) > 0 {
		h.Emit(events.Event{Kind: events.DelayedRegister, Obj: c.Source,
			Player: c.Controller, Step: state.StepEnd,
			Counter: "__kwEncoreSacrificeGroup", IDs: tokens})
	}
}

// encoreGrantHaste grants each minted copy "They gain haste" -- a layer-6
// UntilEOT grant scoped to the token itself (the effPump shape); the token's
// sacrifice at the next end step lands after cleanup would drop the grant
// anyway, and the turn boundary handles the pathological survivor -- and
// returns tokens with the copies that exist appended.
func encoreGrantHaste(h Host, c *Ctx, minted, tokens []state.ObjID) []state.ObjID {
	for _, id := range minted {
		if h.Game().Obj(id) == nil {
			continue
		}
		h.AddContinuous(state.ContinuousEffect{
			Source: id, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LAbilities, AddKeywords: []string{"Haste"}, UntilEOT: true,
		})
		tokens = append(tokens, id)
	}
	return tokens
}
