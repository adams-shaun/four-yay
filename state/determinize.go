package state

import (
	"math/rand/v2"
)

// SearchingSeatKnowsOpponentComposition is the strategic assumption a
// determinization carries about the searching seat's knowledge of what is in
// the opponents' hidden zones (their libraries and hands).
//
// A determinization re-deals each opponent's TRUE remaining cards (the pool is
// that opponent's actual hand + library), so it randomises LOCATION, never
// COMPOSITION: the determinized hidden-zone multiset is exactly the cards that
// opponent really has left, merely permuted between hand and library. That is
// the strictly stronger claim than "knows the decklist": it assumes the seat
// knows each opponent's list AND has perfect recall of every card that has
// already left it, so it can reconstruct precisely which cards remain hidden.
//
// That is a real strategic assumption, defensible for gorge -- decks come from
// published repo decklists and play is self-play, so the cards an opponent has
// hidden are in principle knowable. It is NOT defensible as a general claim
// about a search agent: a genuine opponent who never showed their list would
// have to be sampled from a plausible pool consistent with public knowledge --
// a much larger space, and the honest alternative. gorge has no card-pool
// model with which to build that pool, so that sampling is deferred, not
// solved; this constant records that composition is assumed stable because the
// engine only knows how to preserve the true multiset.
//
// This flag is therefore NOT a NO-OP on Determinize's output: the output here
// embodies composition-is-preserved (true). The flag is carried here as the
// single documented place the assumption lives, so C2 reads it from this one
// spot rather than re-deriving it in two places and letting the two drift.
const SearchingSeatKnowsOpponentComposition = true

// Determinize returns a NEW game (built on Clone; the receiver is never
// mutated) in which every fact fromSeat cannot legally see has been
// re-randomised and every fact it can see is byte-identical -- the honest
// information set a search agent (C2) is allowed to plan with. Ruling C0: a
// search agent searches determinizations, never the true state.
//
// What stays put:
//
//   - fromSeat's own hand is untouched, element for element.
//   - every public fact -- battlefield, graveyard, exile, the stack, and every
//     scalar field on Player and Game -- is byte-identical.
//   - the Objs arena is preserved: Objs[i] still has ID i+1, and no ObjID is
//     invented, dropped or duplicated.
//
// What moves:
//
//   - fromSeat's own library is order-randomised, composition preserved. A
//     player knows what is in their deck; they do not know its order. This is
//     intentionally NOT skipped because the searching seat "owns" those cards.
//   - every other seat's hidden zones (library + hand) are pooled per owner and
//     re-dealt to the same per-zone counts. An opponent's hand size is public;
//     its contents are not. Cards never change owner.
//
// Known unsoundness: gorge records a reveal (and any card known from play) as
// a one-shot Note event in the event log, never as persistent per-object
// state, and a determinization has no access to the log, so it cannot tell
// which opponent cards became known during play. A card an opponent revealed,
// or drew from a public zone, is re-randomised like any other unknown card. The
// concrete case it gets wrong is documented in the D1 report; tracking
// revealed-ness well enough to exempt such cards is deliberately deferred --
// no mechanism is invented this round.
//
// Face-down permanents (morph/manifest) do not exist in gorge's corpus
// (state/object.go has no such flag and cards/keywords.go registers none), so
// there is no hidden-identity public object to handle.
//
// rng is consumed, never constructed here. Same seed, same determinization,
// every time -- the engine is event-sourced, hash-chained and replayable, and
// a search that cannot be replayed cannot be debugged.
func (g *Game) Determinize(fromSeat PlayerID, rng *rand.Rand) *Game {
	c := g.Clone()

	// fromSeat's own library: order-randomised, composition preserved.
	shuffleZone(c, ZLibrary, fromSeat, rng)

	// Every other seat: pool hidden zones and re-deal to the same counts.
	for p := PlayerID(0); p < PlayerID(len(c.Players)); p++ {
		if p != fromSeat {
			redistributeHidden(c, p, rng)
		}
	}
	return c
}

// shuffleZone randomises the order of one zone's ids, preserving its multiset.
// For fromSeat's own library this expresses "a player knows their deck, not
// its order".
func shuffleZone(g *Game, z Zone, p PlayerID, rng *rand.Rand) {
	ids := g.Zone(z, p)
	if len(ids) < 2 {
		return
	}
	rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
}

// WARNING -- the assumption behind pooling (see the package doc and
// SearchingSeatKnowsOpponentComposition): this pools each opponent's ACTUAL
// remaining cards and re-deals them, so it preserves COMPOSITION exactly and
// randomises only LOCATION. That models a seat that knows the opponent's
// decklist AND has perfect recall of every card that has left it -- not a seat
// that genuinely did not know the list (which would have to sample from a
// plausible pool consistent with public knowledge). gorge has no card-pool
// model to do that sampling with, so it is deferred, not solved. If you change
// what is pooled here, you change the assumption this determinization carries.
//
// Cards never cross owners: the pool is p's alone. Each object's own Zone
// field is kept consistent with its new zone membership, because events.Move
// reads o.Zone as the authoritative source zone and the zones arrays must
// agree with it. Both ends of the re-deal are explicitly set (not appended) so
// no stale ids leak from a prior shuffle.
func redistributeHidden(g *Game, p PlayerID, rng *rand.Rand) {
	lib := g.Zone(ZLibrary, p)
	hand := g.Zone(ZHand, p)
	nLib, nHand := len(lib), len(hand)
	total := nLib + nHand
	if total < 2 {
		return
	}
	order := make([]ObjID, 0, total)
	order = append(order, lib...)
	order = append(order, hand...)
	rng.Shuffle(total, func(i, j int) { order[i], order[j] = order[j], order[i] })

	// Cap the library at nLib: its spare capacity would otherwise run into
	// the hand window, so an append to the library would overwrite a hand id.
	g.SetZone(ZLibrary, p, order[:nLib:nLib])
	g.SetZone(ZHand, p, order[nLib:])
	for _, id := range order[:nLib] {
		if o := g.Obj(id); o != nil {
			o.Zone = ZLibrary
		}
	}
	for _, id := range order[nLib:] {
		if o := g.Obj(id); o != nil {
			o.Zone = ZHand
		}
	}
}
