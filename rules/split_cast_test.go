package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// split_cast_test.go pins the brief's named deck carriers: Coward // Killer
// (its Killer alternate half deals 3 and sweeps shared types) and Gallifrey
// Falls // No More (its No More alternate half is the mass phase-out and its
// Fuse asks the combined cost). split_card_test.go already pins the same CR
// 709.4/702.101b shape on Wear // Tear and Alive // Well; these two pin the
// carriers the split-casting-fuse brief named, on corpus cards only -- no
// Forge script text is committed here -- and end in replayCheck.

// killerSplitEngine deals seat 0 a 40-card deck led by the Coward // Killer
// split card and puts one Grizzly Bears (2/2) on seat 1's battlefield as the
// Killer half's target -- the only creature on the battlefield, so the
// shares-creature-type sweep has nothing else to reach.
func killerSplitEngine(t *testing.T, reg *cards.Registry, seed uint64) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	coward := searchCorpusCard(t, reg, "Coward")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	island := searchCorpusCard(t, reg, "Island")
	deck := []*cards.Card{coward}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := []*cards.Card{bear}
	for len(opp) < 40 {
		opp = append(opp, island)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"split", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	bearID := splitMoveFromLibrary(t, e, 1, "Grizzly Bears")
	id := searchMoveByName(t, e, "Coward", state.ZHand)
	return e, cfg, id, bearID
}

// TestKillerHalfIsCastable pins the brief's first carrier: the split card's
// SECOND face is reachable from hand. Coward // Killer's Killer half ({2}{R}
// {R} sorcery) is offered as mode split_alt alongside the front Coward half,
// casting it asks Killer's own creature target, and its 3 damage kills the
// 2/2. Without the split-face offer the alternate half is dead half the card.
func TestKillerHalfIsCastable(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, id, bearID := killerSplitEngine(t, reg, 8425)
	// Precondition: the bear is where the damage lands and the card is in
	// hand -- a vacuous setup would pass anything.
	if z := e.G.Obj(bearID).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: bear zone=%s, want battlefield", z)
	}
	if z := e.G.Obj(id).Zone; z != state.ZHand {
		t.Fatalf("precondition: split card zone=%s, want hand", z)
	}
	addMana(t, e, 0, "RRRR")
	plain := splitOption(t, e, id, "")
	alt := splitOption(t, e, id, "split_alt")
	if plain == nil || plain.Label != "Cast Coward" {
		t.Fatalf("front-half offer missing/renamed: %+v", castOptions(t, e))
	}
	if alt == nil || alt.Label != "Cast Killer" {
		t.Fatalf("alternate-half offer missing/renamed (the brief's dead-half bug): %+v", castOptions(t, e))
	}
	submitChoices(t, e, alt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Killer's creature-target ask: %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("Killer offered no option for the bear: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(bearID).Zone; z != state.ZGraveyard {
		t.Fatalf("Killer's 3 damage left the 2/2 at zone=%s, want graveyard", z)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved split half zone=%s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestGallifreyFallsFuseCastsBothHalves pins the brief's second carrier: the
// fused cast of Gallifrey Falls // No More is offered only at the SUMMED cost
// ({4}{R}{R} + {2}{W} = {6}{R}{R}{W}) and dispatches BOTH halves: Falls
// damages every creature, then No More's real Phases half (effects/phases.go)
// phases out seat 0's own 9/9, which survives Falls' 4. The phased-out status
// is the proof the second half was visited after the first, rather than
// silently skipped.
func TestGallifreyFallsFuseCastsBothHalves(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	falls := searchCorpusCard(t, reg, "Gallifrey Falls")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	brontodon := searchCorpusCard(t, reg, "Ancient Brontodon")
	island := searchCorpusCard(t, reg, "Island")
	deck := []*cards.Card{falls, brontodon}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := []*cards.Card{bear}
	for len(opp) < 40 {
		opp = append(opp, island)
	}
	cfg := seatZeroStart(Config{Seed: 8426, Names: []string{"split", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	bearID := splitMoveFromLibrary(t, e, 1, "Grizzly Bears")
	// No More's target: seat 0's own creature, a 9/9 that survives Falls' 4.
	ownID := moveByName(t, e, 0, "Ancient Brontodon", state.ZBattlefield)
	id := searchMoveByName(t, e, "Gallifrey Falls", state.ZHand)
	if z := e.G.Obj(bearID).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: bear zone=%s, want battlefield", z)
	}
	if o := e.G.Obj(ownID); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: seat 0's Brontodon not a phased-in battlefield permanent: %+v", o)
	}

	// Cost exactness: fund exactly {4}{R}{R} = 7 red (4 generic + R + R). The
	// Falls half alone is payable, but the fused cast needs the No More half's
	// {2}{W} on top and must NOT be offered. (No More alone is not payable
	// either, so only the front half shows.)
	addMana(t, e, 0, "RRRRRRR")
	if splitOption(t, e, id, "fuse") != nil {
		t.Fatalf("fused cast offered for {4}{R}{R} alone, want the summed {6}{R}{R}{W}: %+v", castOptions(t, e))
	}
	if splitOption(t, e, id, "") == nil {
		t.Fatalf("front half not offered at its own {4}{R}{R}: %+v", castOptions(t, e))
	}

	// Add the {2}{W}: now the fused cast is offered.
	addMana(t, e, 0, "WW")
	fuse := splitOption(t, e, id, "fuse")
	if fuse == nil {
		t.Fatalf("fused offer missing at the summed cost: %+v", castOptions(t, e))
	}
	if n := len(e.G.Zone(state.ZBattlefield, 0)); n != 1 {
		t.Fatalf("precondition: No More's controller has %d battlefield objects, want only the Brontodon", n)
	}
	submitChoices(t, e, fuse.Index)
	// The Falls half targets nothing; the No More half asks any number of
	// seat 0's creatures (TargetMin 0). Answer with the Brontodon so its
	// phase-out is the observable second-half effect.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		tgt := -1
		for _, o := range d.Options {
			if o.Obj == ownID {
				tgt = o.Index
			}
		}
		if tgt < 0 {
			t.Fatalf("No More offered no option for seat 0's Brontodon: %+v", d.Options)
		}
		submitChoices(t, e, tgt)
	}
	passUntilStackEmpty(t, e, 20)

	// Falls dealt its 4 to every creature. No More's real Phases half then
	// phased out seat 0's 9/9 (it survives Falls' 4). The phased-out status
	// proves the second half was visited after the first.
	if o := e.G.Obj(ownID); o == nil || !o.PhasedOut || o.Zone != state.ZBattlefield {
		t.Fatalf("fused No More half did not phase out seat 0's Brontodon: %+v", o)
	}
	if phasesHasNote(e, "unimplemented API Phases") {
		t.Fatal("api:Phases is unregistered (fallback Note present)")
	}
	// The Falls half's 4 damage on the 2/2 opposing bear is the first half's
	// observable effect. Falls also carries ReplaceDyingDefined$ Remembered,
	// so the bear that would die to the damage is exiled instead of buried
	// ("If a creature dealt damage this way would die this turn, exile it
	// instead.").
	if z := e.G.Obj(bearID).Zone; z != state.ZExile {
		t.Fatalf("fused Falls left the opposing bear at zone=%s, want exile (4 damage on a 2/2, ReplaceDyingDefined$ Remembered)", z)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved fused spell zone=%s, want graveyard", z)
	}
	if pool := e.G.Players[0].Pool; pool != (state.Mana{}) {
		t.Fatalf("pool after the fused {6}{R}{R}{W} payment = %+v, want empty", pool)
	}
	replayCheck(t, e, cfg)
}
