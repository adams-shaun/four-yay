package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// split_card_test.go pins non-Room Split cards (CR 709.4) and Fuse (CR
// 702.101b) end to end on the REAL corpus card Wear // Tear (the deck carrier
// the pip census named). Every test runs on corpus cards only -- no Forge
// script text is committed here -- and ends in replayCheck.

// TestSplitFuseKeywordIsRegistered pins the coverage support declaration for
// kw:Fuse (rules/split.go's init): cards' census derives it from the printed
// K: line, and omitting the registration would leave the 17 corpus fuse
// carriers unplayable in every coverage/deck-validation consumer even though
// the engine now implements the cast.
func TestSplitFuseKeywordIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["kw:Fuse"] {
		t.Fatal(`effects.Supported() is missing "kw:Fuse"`)
	}
}

// TestSplitFuseWithATargetlessHalfRunsBothHalves pins the fused target-stage
// skip: Alive // Well's front half declares no targets, so the cast must not
// stall on an empty first stage -- it goes straight to Well's resolution.
// Both halves run (a Centaur token AND Well's "2 life per creature you
// control", read off the just-created token), so each half's own SVar table
// survived the shared resolution.
func TestSplitFuseWithATargetlessHalfRunsBothHalves(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	alive := searchCorpusCard(t, reg, "Alive")
	island := searchCorpusCard(t, reg, "Island")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{alive}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 8415, Names: []string{"split", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := searchMoveByName(t, e, "Alive", state.ZHand)
	addMana(t, e, 0, "WWGGG")
	fuse := splitOption(t, e, id, "fuse")
	if fuse == nil {
		t.Fatalf("fused offer missing for a targetless-front-half card: %+v", castOptions(t, e))
	}
	submitChoices(t, e, fuse.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("a targetless fused half asked for a target: %+v", d)
	}
	passUntilStackEmpty(t, e, 20)
	tokens := 0
	for _, bid := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(bid); o != nil && o.IsToken {
			tokens++
		}
	}
	if tokens != 1 {
		t.Fatalf("fused Alive created %d tokens, want 1", tokens)
	}
	if life := e.G.Players[0].Life; life != 22 {
		t.Fatalf("fused Well life = %d, want 22 (2 per the token Alive made)", life)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved fused spell zone=%s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestSplitResolvedHalfDoesNotPersistOnTheCard pins the CR 709.4 face
// normalization events.Apply's Move fold applies to a non-Room split card
// leaving the stack: after casting Tear, returning the card to hand offers
// BOTH halves again (face 0 is canonical off the stack), not just the half it
// was last cast as. Before the normalization the returned card sat at face 1
// and, with Tear's own target gone, offered nothing at all.
func TestSplitResolvedHalfDoesNotPersistOnTheCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, id, _, prisonID := splitCorpusEngine(t, reg, 8421)
	// A second enchantment so Tear still has a target after the first one is
	// destroyed (both halves must be offered again after the return).
	splitMoveFromLibrary(t, e, 1, "Ghostly Prison")
	addMana(t, e, 0, "RW")
	alt := splitOption(t, e, id, "split_alt")
	submitChoices(t, e, alt.Index)
	d := e.Pending()
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == prisonID {
			tgt = o.Index
		}
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)
	if fi := e.G.Obj(id).FaceIdx; fi != 0 {
		t.Fatalf("split card left the stack at FaceIdx %d, want 0", fi)
	}
	// Return it to hand: both halves are castable again.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZHand})
	e.pending = nil
	e.priorityRound()
	addMana(t, e, 0, "RWG")
	if got := splitOption(t, e, id, ""); got == nil {
		t.Fatalf("front half not offered after the card returned to hand: %+v", castOptions(t, e))
	}
	if got := splitOption(t, e, id, "split_alt"); got == nil {
		t.Fatalf("alternate half not offered after the card returned to hand: %+v", castOptions(t, e))
	}
	if got := splitOption(t, e, id, "fuse"); got == nil {
		t.Fatalf("fused cast not offered after the card returned to hand: %+v", castOptions(t, e))
	}
	replayCheck(t, e, cfg)
}

// splitOption returns the cast option for id whose Mode is exactly mode, or
// nil when none is offered.
func splitOption(t *testing.T, e *Engine, id state.ObjID, mode string) *decision.Option {
	t.Helper()
	opts := castOptions(t, e)
	for i := range opts {
		o := &opts[i]
		if o.Obj == id && o.Mode == mode {
			return o
		}
	}
	return nil
}

// splitMoveFromLibrary event-sources a card from player p's library onto p's
// battlefield, so a replayCheck can rebuild the same board (onBoardCard is
// eventless and a log-only replay cannot see it).
func splitMoveFromLibrary(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		o := e.G.Obj(id)
		if o != nil && o.Face() != nil && o.Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
			e.pending = nil
			e.priorityRound()
			return id
		}
	}
	t.Fatalf("corpus card %q absent from seat %d's library", name, p)
	return 0
}

// splitCorpusEngine deals seat 0 a 40-card deck led by the Wear // Tear split
// card and seats a Sol Ring (artifact) and a Ghostly Prison (enchantment) on
// seat 1's battlefield, one legal target for each half. The returned ids are
// seat 0's Wear // Tear in hand and the two opposing permanents.
func splitCorpusEngine(t *testing.T, reg *cards.Registry, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	wear := searchCorpusCard(t, reg, "Wear")
	ring := searchCorpusCard(t, reg, "Sol Ring")
	prison := searchCorpusCard(t, reg, "Ghostly Prison")
	island := searchCorpusCard(t, reg, "Island")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{wear}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := []*cards.Card{ring, prison, prison}
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"split", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	ringID := splitMoveFromLibrary(t, e, 1, "Sol Ring")
	prisonID := splitMoveFromLibrary(t, e, 1, "Ghostly Prison")
	id := searchMoveByName(t, e, "Wear", state.ZHand)
	return e, cfg, id, ringID, prisonID
}

// TestSplitCardEachHalfIsSeparatelyCastable is the brief's first half: on the
// real corpus card Wear // Tear both the front half (Wear, mode "") and the
// alternate half (Tear, mode "split_alt") are offered from hand, and casting
// Tear resolves Tear's own body -- it destroys the enchantment, not the
// artifact.
func TestSplitCardEachHalfIsSeparatelyCastable(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, id, ringID, prisonID := splitCorpusEngine(t, reg, 8411)
	addMana(t, e, 0, "RW")
	plain := splitOption(t, e, id, "")
	alt := splitOption(t, e, id, "split_alt")
	if plain == nil || plain.Label != "Cast Wear" {
		t.Fatalf("front-half offer missing/renamed: %+v", castOptions(t, e))
	}
	if alt == nil || alt.Label != "Cast Tear" {
		t.Fatalf("alternate-half offer missing/renamed: %+v", castOptions(t, e))
	}

	// Cast Tear: its own Destroy Enchantment target ask is posed, and only
	// the enchantment is a legal target.
	submitChoices(t, e, alt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Tear target ask: %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == prisonID {
			tgt = o.Index
		}
		if o.Obj == ringID {
			t.Fatalf("Tear offered the artifact as a legal target: %+v", d.Options)
		}
	}
	if tgt < 0 {
		t.Fatalf("no option targeting the enchantment: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(prisonID).Zone; z != state.ZGraveyard {
		t.Fatalf("Tear's target zone=%s, want graveyard", z)
	}
	if z := e.G.Obj(ringID).Zone; z != state.ZBattlefield {
		t.Fatalf("Tear destroyed the artifact too: zone=%s", z)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved split half zone=%s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestSplitFusePaysCombinedCostAndResolvesBothHalves is the brief's second
// half: a fused cast costs BOTH halves' printed mana (Wear {1}{R} + Tear {W}
// = {1}{R}{W}), asks each half's own targets in turn, and resolves both
// destroys in one spell.
func TestSplitFusePaysCombinedCostAndResolvesBothHalves(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, id, ringID, prisonID := splitCorpusEngine(t, reg, 8412)

	// Cost exactness: fund exactly {R}{W}. Each half alone is payable (Wear's
	// generic pip takes the W; Tear needs only the W), so both are offered --
	// but the fused cast needs the THIRD mana and must not be.
	addMana(t, e, 0, "RW")
	if splitOption(t, e, id, "fuse") != nil {
		t.Fatalf("fused cast offered for 2 mana, want the combined {1}{R}{W}: %+v", castOptions(t, e))
	}
	if splitOption(t, e, id, "") == nil || splitOption(t, e, id, "split_alt") == nil {
		t.Fatalf("a single half was not offered at 2 mana: %+v", castOptions(t, e))
	}

	// Add the generic pip: now the fused cast is offered.
	addMana(t, e, 0, "G")
	fuse := splitOption(t, e, id, "fuse")
	if fuse == nil || fuse.Label != "Cast Wear // Tear (fused)" {
		t.Fatalf("fused offer missing/renamed: %+v", castOptions(t, e))
	}
	submitChoices(t, e, fuse.Index)

	// The fused cast asks each half's targets in order: Wear's artifact
	// first, then Tear's enchantment.
	wantOrder := []state.ObjID{ringID, prisonID}
	for i, want := range wantOrder {
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("fused target stage %d: pending=%+v, want target", i, d)
		}
		found := -1
		for _, o := range d.Options {
			if o.Obj == want {
				found = o.Index
			}
			if o.Obj == wantOrder[1-i] {
				t.Fatalf("stage %d offered the other half's target: %+v", i, d.Options)
			}
		}
		if found < 0 {
			t.Fatalf("stage %d: no option for %d: %+v", i, want, d.Options)
		}
		submitChoices(t, e, found)
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("fused cast asked a third target: %+v", d)
	}
	passUntilStackEmpty(t, e, 20)

	// Both halves resolved: both permanents are gone, and the whole combined
	// cost was spent (the pool is empty).
	if z := e.G.Obj(ringID).Zone; z != state.ZGraveyard {
		t.Fatalf("fused Wear did not destroy the artifact: zone=%s", z)
	}
	if z := e.G.Obj(prisonID).Zone; z != state.ZGraveyard {
		t.Fatalf("fused Tear did not destroy the enchantment: zone=%s", z)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved fused spell zone=%s, want graveyard", z)
	}
	if pool := e.G.Players[0].Pool; pool != (state.Mana{}) {
		t.Fatalf("pool after the fused {1}{R}{W} payment = %+v, want empty", pool)
	}
	replayCheck(t, e, cfg)
}

// TestSplitHalfWithNoLegalTargetIsWithheld pins the per-half target gate: an
// artifact alone leaves Wear castable but withholds Tear -- and therefore the
// fused cast, which needs both halves' targets.
func TestSplitHalfWithNoLegalTargetIsWithheld(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	wear := searchCorpusCard(t, reg, "Wear")
	ring := searchCorpusCard(t, reg, "Sol Ring")
	island := searchCorpusCard(t, reg, "Island")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{wear}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := []*cards.Card{ring}
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 8413, Names: []string{"split", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	splitMoveFromLibrary(t, e, 1, "Sol Ring")
	id := searchMoveByName(t, e, "Wear", state.ZHand)
	addMana(t, e, 0, "RWG")
	if splitOption(t, e, id, "") == nil {
		t.Fatalf("Wear not offered with an artifact present: %+v", castOptions(t, e))
	}
	if got := splitOption(t, e, id, "split_alt"); got != nil {
		t.Fatalf("Tear offered with no enchantment to target: %+v", got)
	}
	if got := splitOption(t, e, id, "fuse"); got != nil {
		t.Fatalf("fused cast offered with no enchantment target: %+v", got)
	}
	replayCheck(t, e, cfg)
}

// TestNonFuseSplitOffersBothHalvesButNoFusedCast pins the keyword boundary:
// the real corpus card Bound // Determined is a Split card with NO K:Fuse and
// no Aftermath, so each half is offered separately but never a fused cast.
func TestNonFuseSplitOffersBothHalvesButNoFusedCast(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	bound := searchCorpusCard(t, reg, "Bound")
	island := searchCorpusCard(t, reg, "Island")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{bound}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 8414, Names: []string{"split", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := searchMoveByName(t, e, "Bound", state.ZHand)
	// Bound is {3}{B}{G}, Determined is {G}{U}; fund both plus the combined.
	addMana(t, e, 0, "BBGGU")
	if got := splitOption(t, e, id, ""); got == nil {
		t.Fatalf("front half of a non-fuse split not offered: %+v", castOptions(t, e))
	}
	if got := splitOption(t, e, id, "split_alt"); got == nil {
		t.Fatalf("alternate half of a non-fuse split not offered: %+v", castOptions(t, e))
	}
	if got := splitOption(t, e, id, "fuse"); got != nil {
		t.Fatalf("fused cast offered without K:Fuse: %+v", got)
	}
	replayCheck(t, e, cfg)
}

// TestSplitFuseOverlappingHalfSpecsResolveTheirOwnTargets is review round 2's
// MAJOR 1 pin: the two halves' target stages record their own slices at
// payment (Engine.fuseTargets), so a fused cast whose halves' ValidTgts
// specs OVERLAP resolves each half against exactly the target chosen for it.
// On the real corpus card Turn // Burn (Turn `ValidTgts$ Creature`, Burn
// `ValidTgts$ Any`) a fused cast targeting the opponent's bear with Turn and
// the caster's own bear with Burn must not apply Turn to Burn's target (both
// bears turned into 0/1 Weirds and both killed by Burn's 2 damage is the
// pre-fix mis-assignment). Turn's Animate then leaves its own target a 0/1
// red Weird that took no damage; Burn kills its own target.
func TestSplitFuseOverlappingHalfSpecsResolveTheirOwnTargets(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	turn := searchCorpusCard(t, reg, "Turn")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	island := searchCorpusCard(t, reg, "Island")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{turn, bear, bear}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := []*cards.Card{bear}
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 8416, Names: []string{"split", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	myBear := splitMoveFromLibrary(t, e, 0, "Grizzly Bears")
	oppBear := splitMoveFromLibrary(t, e, 1, "Grizzly Bears")
	id := searchMoveByName(t, e, "Turn", state.ZHand)
	// Fused cost {3}{U}{R}: the pool U,U,U,R,R pays the U and R pips and
	// three generic.
	addMana(t, e, 0, "UUURR")
	fuse := splitOption(t, e, id, "fuse")
	if fuse == nil || fuse.Label != "Cast Turn // Burn (fused)" {
		t.Fatalf("fused offer missing/renamed: %+v", castOptions(t, e))
	}
	submitChoices(t, e, fuse.Index)

	// Stage 0 (Turn): both bears are legal (spec Creature) -- choose the
	// opponent's. Stage 1 (Burn): both are legal too (spec Any) -- choose
	// my own. Every bear appears in BOTH stages' option lists; the
	// assignment is what the slices must record.
	wantOrder := []state.ObjID{oppBear, myBear}
	for i, want := range wantOrder {
		d := e.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("fused target stage %d: pending=%+v, want target", i, d)
		}
		found := -1
		for _, o := range d.Options {
			if o.Obj == want {
				found = o.Index
			}
		}
		if found < 0 {
			t.Fatalf("stage %d: no option for bear %d: %+v", i, want, d.Options)
		}
		submitChoices(t, e, found)
	}
	passUntilStackEmpty(t, e, 20)

	// Turn applied to ITS target only: the opponent's bear is a 0/1 red
	// Weird that took no damage and lives. Pre-fix both bears became 0/1
	// Weirds AND both took Burn's 2 damage, so the opponent's bear died.
	oppObj := e.G.Obj(oppBear)
	if oppObj == nil || oppObj.Zone != state.ZBattlefield {
		t.Fatalf("Turn's target zone=%v, want battlefield (pre-fix both halves applied to both targets)", oppObj)
	}
	if oppObj.Damage != 0 {
		t.Fatalf("Turn's target took damage: %+v", oppObj)
	}
	der := e.Derived(oppBear)
	if der.Power != 0 || der.Toughness != 1 {
		t.Fatalf("Turn's target derived P/T = %d/%d, want 0/1 (the Weird)", der.Power, der.Toughness)
	}
	weird := false
	for _, ty := range der.Types {
		if ty == "Weird" {
			weird = true
		}
	}
	if !weird {
		t.Fatalf("Turn's target types = %v, want Weird among them", der.Types)
	}
	if der.Colors != "R" {
		t.Fatalf("Turn's target colors = %q, want R", der.Colors)
	}
	// Burn applied to ITS target only: my own bear took the 2 damage and is
	// gone; pre-fix Turn's target also died.
	if mine := e.G.Obj(myBear); mine != nil && mine.Zone == state.ZBattlefield {
		t.Fatalf("Burn's target still on the battlefield: %+v", mine)
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved fused spell zone=%s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestSplitInstantHalfOfferedAtInstantTiming is review round 2's MAJOR 2
// pin: the split offers stand above the front-face spellTimingOK gate, so a
// front-Sorcery / alternate-Instant split card offers its instant half at
// instant timing (CR 709.4 -- each half's own timing; carriers
// incubation_incongruity, discovery_dispersal, said_done, spring_mind). On
// the real corpus card Said // Done, during seat 0's own BEGIN COMBAT step
// (sorcery timing is OFF) Said is withheld but Done is offered against a
// creature of mine to tap. Pre-fix the front-face gate suppressed the whole
// card's offers.
func TestSplitInstantHalfOfferedAtInstantTiming(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	said := searchCorpusCard(t, reg, "Said")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	island := searchCorpusCard(t, reg, "Island")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{said, bear}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 8417, Names: []string{"split", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	myBear := splitMoveFromLibrary(t, e, 0, "Grizzly Bears")
	id := searchMoveByName(t, e, "Said", state.ZHand)
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepBeginCombat)
	e.pending = nil
	e.priorityRound()
	// Fund {3}{U} for Done AT the combat step: pools empty as each step ends
	// (CR 500.4, finishStepBoundary's ManaClear), so Main1 mana cannot be
	// carried here, and addMana's own drive back to Main1 cannot go backward.
	for _, r := range "UUUU" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.priorityRound()
	if got := splitOption(t, e, id, ""); got != nil {
		t.Fatalf("sorcery front half offered at instant timing: %+v", got)
	}
	alt := splitOption(t, e, id, "split_alt")
	if alt == nil || alt.Label != "Cast Done" {
		t.Fatalf("instant alternate half not offered at instant timing: %+v", castOptions(t, e))
	}
	// And it actually casts and resolves: Done taps my own bear.
	submitChoices(t, e, alt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Done target ask: %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == myBear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no option targeting my bear: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(myBear).Tapped {
		t.Fatalf("Done did not tap its target: %+v", e.G.Obj(myBear))
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("resolved instant half zone=%s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}
