package rules

// The Count$IfCastInOwnMainPhase / Count$InOwnMainPhase branch heads (task
// ifcastmain1): "if you cast this spell during your main phase" gate. The
// reading is LIVE (Forge's game.getPhaseHandler(): current step is a main
// phase and the active player is the resolving controller), plus -- for the
// IfCast spelling -- the source must have been cast. This file pins the
// target-bound path (Return to Dust's TargetMax$ X) and the effect-amount
// path (Sulfurous Blast's NumDmg$ X) end to end on the real corpus cards.
// The eval-level grammar is pinned in effects/ifcastmain_count_test.go; no
// Forge script text is committed (searchTestRegistry loads the corpus).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const ifcastRelicSrc = "Name:Probe Relic\nManaCost:1\nTypes:Artifact\nOracle:x\n"

// ifcastDeck builds a seat-zero-start engine whose seat 0 leads with the named
// corpus cards plus two seeded Probe Relic artifact cards; both seats' decks
// are padded with corpus basics. The relics are seeded as REAL cards (never
// tokens): a token ceases to exist when exiled (CR 111.7), so it could not
// witness Return to Dust's exile.
func ifcastDeck(t *testing.T, seed uint64, fixtures ...string) (*Engine, Config) {
	t.Helper()
	reg := searchTestRegistry(t)
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	deck = append(deck, card(t, ifcastRelicSrc), card(t, ifcastRelicSrc))
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"caster", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// ifcastRelics moves the two seeded Probe Relics onto seat 0's battlefield
// and returns their ids.
func ifcastRelics(t *testing.T, e *Engine) [2]state.ObjID {
	t.Helper()
	toMain1(t, e)
	var out [2]state.ObjID
	for i := range out {
		out[i] = moveByName(t, e, 0, "Probe Relic", state.ZBattlefield)
	}
	return out
}

// ifcastCastByName casts the named corpus card from seat 0's hand at the
// current step (funding the pool first) and returns the pending decision --
// the target ask for Return to Dust, nil for a resolution that needs none.
func ifcastCastByName(t *testing.T, e *Engine, name, mana string) *decision.Decision {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	fundPool(t, e, mana)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision before casting %s (got %+v)", name, d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %s: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
	return e.Pending()
}

// TestReturnToDustMainPhaseExilesTwoTargets pins the target-bound path in the
// caster's own Main1: TargetMax$ X reads Count$IfCastInOwnMainPhase.2.1, so
// the CR 601.2c announcement ask is Min=1 Max=2 with both eligible artifacts
// offered, and picking both exiles both. Before the head was implemented the
// bound degraded to a resolved 0, clamped to Max 1 -- the second permanent
// could never be chosen.
func TestReturnToDustMainPhaseExilesTwoTargets(t *testing.T) {
	t.Parallel()
	e, cfg := ifcastDeck(t, 9301, "Return to Dust")
	relics := ifcastRelics(t, e)
	relicA, relicB := relics[0], relics[1]
	toMain1(t, e)
	d := ifcastCastByName(t, e, "Return to Dust", "WWCC")
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the target ask, got %+v", d)
	}
	if d.Min != 1 || d.Max != 2 {
		t.Fatalf("target ask Min=%d Max=%d, want 1/2 in own Main1", d.Min, d.Max)
	}
	if len(d.Options) != 2 {
		t.Fatalf("target ask offered %d options, want both artifacts: %+v", len(d.Options), d.Options)
	}
	submitChoices(t, e, d.Options[1].Index, d.Options[0].Index)
	passUntilStackEmpty(t, e, 40)
	for _, id := range []state.ObjID{relicA, relicB} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZExile {
			t.Fatalf("Probe Relic %d on %s, want exile", id, o.Zone)
		}
	}
	replayCheck(t, e, cfg)
}

// TestReturnToDustOutsideMainPhaseCapsOne pins the same card outside the
// caster's main phase: seat 0 casts in its OWN end step, so the live step is
// not a main phase and the head takes the not-main branch (1) -- Max=1, and
// only one permanent can be chosen.
func TestReturnToDustOutsideMainPhaseCapsOne(t *testing.T) {
	t.Parallel()
	e, cfg := ifcastDeck(t, 9301, "Return to Dust")
	relics := ifcastRelics(t, e)
	relicA, relicB := relics[0], relics[1]
	// Seat 0's own end step (active == 0, step == End): not a main phase.
	driveToStepAll(t, e, 1, 0, state.StepEnd)
	d := ifcastCastByName(t, e, "Return to Dust", "WWCC")
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the target ask, got %+v", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("target ask Min=%d Max=%d, want 1/1 outside a main phase", d.Min, d.Max)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 40)
	exiled := 0
	for _, id := range []state.ObjID{relicA, relicB} {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZExile {
			exiled++
		}
	}
	if exiled != 1 {
		t.Fatalf("exiled %d artifacts, want exactly 1 outside a main phase", exiled)
	}
	replayCheck(t, e, cfg)
}

// TestSulfurousBlastMainPhaseDealsThree pins the effect-amount path: NumDmg$ X
// reads Count$IfCastInOwnMainPhase.3.2, so in seat 0's own Main1 the spell
// deals 3 to each creature and each player (both life totals fall by 3).
func TestSulfurousBlastMainPhaseDealsThree(t *testing.T) {
	t.Parallel()
	e, cfg := ifcastDeck(t, 9302, "Sulfurous Blast")
	toMain1(t, e)
	before := [2]int32{e.G.Players[0].Life, e.G.Players[1].Life}
	ifcastCastByName(t, e, "Sulfurous Blast", "RRCC")
	passUntilStackEmpty(t, e, 40)
	for i := 0; i < 2; i++ {
		if got, want := e.G.Players[i].Life, before[i]-3; got != want {
			t.Fatalf("seat %d life = %d, want %d (3 damage in own Main1)", i, got, want)
		}
	}
	replayCheck(t, e, cfg)
}

// TestSulfurousBlastOutsideMainPhaseDealsTwo is the not-main twin: same card,
// cast in seat 0's own end step -- the head selects 2, so each player loses 2.
func TestSulfurousBlastOutsideMainPhaseDealsTwo(t *testing.T) {
	t.Parallel()
	e, cfg := ifcastDeck(t, 9302, "Sulfurous Blast")
	driveToStepAll(t, e, 1, 0, state.StepEnd)
	before := [2]int32{e.G.Players[0].Life, e.G.Players[1].Life}
	ifcastCastByName(t, e, "Sulfurous Blast", "RRCC")
	passUntilStackEmpty(t, e, 40)
	for i := 0; i < 2; i++ {
		if got, want := e.G.Players[i].Life, before[i]-2; got != want {
			t.Fatalf("seat %d life = %d, want %d (2 damage outside a main phase)", i, got, want)
		}
	}
	replayCheck(t, e, cfg)
}
