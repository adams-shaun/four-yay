package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The DealDamage ExcessSVar$ publication (CR 120.10) pinned on the real
// corpus carriers: the damage beyond what was lethal to a permanent is bound
// into the resolution's Ctx.SVars under the card's chosen name, so the
// chained SubAbility$ reads it (Bottle-Cap Blast's TokenAmount$ Excess,
// Lacerate Flesh's TokenAmount$ X).

// noPlayerTarget is the sentinel for "the caller wants an object target, not
// a player"; it is a PlayerID past any real seat.
const noPlayerTarget = state.PlayerID(255)

// excessFixture builds a seat-0-protagonist game from a real corpus card and
// the corpus token registry, so a token-creating SubAbility$ resolves its
// TokenScript$ (the searchEngine shape). `rel` is the corpus-relative script
// path; the fixture card is returned.
func excessFixture(t *testing.T, seed uint64, rel string) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	return excessFixtureWithTokens(t, seed, corpusCardText(t, rel), reg.Tokens)
}

// excessFixtureWithTokens is newFixtureDeck's twin that seeds a real token
// registry (its hardcoded empty Tokens map cannot resolve a corpus
// TokenScript$). Same seatZeroStart treatment and same replayable Config.
func excessFixtureWithTokens(t *testing.T, seed uint64, fixtureSrc string, tokens map[string]*cards.Card) (*Engine, Config, state.ObjID) {
	t.Helper()
	fixture := card(t, fixtureSrc)
	name := fixture.Faces[0].Name
	var e *Engine
	var cfg Config
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append([]*cards.Card{fixture}, mountainDeck(t, 39)...),
				mountainDeck(t, 40),
			},
			Tokens: tokens,
		}
	}
	cfg = seatZeroStart(build(seed))
	e = New(cfg)
	e.Advance()

	var id state.ObjID
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == name {
			id = cand
		}
	}
	if id == 0 {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == name {
				id = cand
			}
		}
		if id == 0 {
			t.Fatalf("fixture %q not found in seat 0's hand or library", name)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	return e, cfg, id
}

// excessCastAndDrain casts the fixture (already offered) at a target, then
// passes priority to empty. `target` is the object id to choose (0 = no
// object); `targetPlayer` is the seat to choose (noPlayerTarget = no seat).
func excessCastAndDrain(t *testing.T, e *Engine, id state.ObjID, target state.ObjID, targetPlayer state.PlayerID) {
	t.Helper()
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target ask after casting: %+v", d)
	}
	pick := -1
	for _, o := range d.Options {
		if target != 0 && o.Obj == target {
			pick = o.Index
		}
		if targetPlayer != noPlayerTarget && o.Kind == "player" && o.Player == targetPlayer {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("target not offered: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	for i := 0; i < 40 && len(e.G.Stack) > 0 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack %d)", len(e.G.Stack))
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("non-pass decision while draining: %+v", d)
		}
		submitChoices(t, e, idx)
	}
}

// TestBottleCapBlastExcessCreatesTreasures pins the full oracle clause on the
// real carrier: 5 damage to a 2-toughness creature leaves 3 excess, so the
// chained token sub makes exactly 3 tapped Treasures.
func TestBottleCapBlastExcessCreatesTreasures(t *testing.T) {
	e, cfg, blast := excessFixture(t, 11, "b/bottle_cap_blast.txt")
	bear := putToken(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "RRRRR")
	excessCastAndDrain(t, e, blast, bear, noPlayerTarget)
	if got := tokensNamed(e, 0, "Treasure"); got != 3 {
		t.Fatalf("Treasures = %d, want 3 (5 damage to a 2-toughness creature)", got)
	}
	replayCheck(t, e, cfg)
}

// TestBottleCapBlastNoExcessCreatesNoTreasures pins the boundary: 5 damage to
// a 5-toughness creature is exactly lethal, leaving no excess, so the sub
// makes no Treasures.
func TestBottleCapBlastNoExcessCreatesNoTreasures(t *testing.T) {
	e, cfg, blast := excessFixture(t, 12, "b/bottle_cap_blast.txt")
	wurm := putToken(t, e, 1, "Name:Wurm\nManaCost:3 G\nTypes:Creature Wurm\nPT:5/5\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "RRRRR")
	excessCastAndDrain(t, e, blast, wurm, noPlayerTarget)
	if got := tokensNamed(e, 0, "Treasure"); got != 0 {
		t.Fatalf("Treasures = %d, want 0 (5 damage to a 5-toughness creature)", got)
	}
	replayCheck(t, e, cfg)
}

// TestBottleCapBlastPlayerTargetCreatesNoTreasures pins that damage to a
// PLAYER never produces excess (CR 120.10 defines it only for a permanent):
// 5 to the opponent's face makes no Treasures.
func TestBottleCapBlastPlayerTargetCreatesNoTreasures(t *testing.T) {
	t.Parallel()
	e, cfg, blast := excessFixture(t, 13, "b/bottle_cap_blast.txt")
	addMana(t, e, 0, "RRRRR")
	excessCastAndDrain(t, e, blast, 0, 1)
	if got := tokensNamed(e, 0, "Treasure"); got != 0 {
		t.Fatalf("Treasures = %d, want 0 (player target has no lethal/excess)", got)
	}
	if e.G.Players[1].Life != 15 {
		t.Fatalf("opponent life = %d, want 15 (20-5)", e.G.Players[1].Life)
	}
	replayCheck(t, e, cfg)
}

// TestLacerateFleshExcessXVariantCreatesBloodTokens pins the ExcessSVar$ X
// spelling (the SVar name IS "X", which resolveNumericRHS and effects.Num
// both read before the paid c.X): 4 damage to a 2-toughness creature leaves 2
// excess, so 2 Blood tokens appear.
func TestLacerateFleshExcessXVariantCreatesBloodTokens(t *testing.T) {
	e, cfg, flesh := excessFixture(t, 14, "l/lacerate_flesh.txt")
	bear := putToken(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "RRRRR")
	excessCastAndDrain(t, e, flesh, bear, noPlayerTarget)
	if got := tokensNamed(e, 0, "Blood"); got != 2 {
		t.Fatalf("Blood tokens = %d, want 2 (4 damage to a 2-toughness creature)", got)
	}
	replayCheck(t, e, cfg)
}
