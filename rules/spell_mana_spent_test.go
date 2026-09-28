package rules

// trig:SpellCast.ValidSAonCard -- the cast spell's own mana-spent gate.
//
// Ancient Cellarspawn's T:Mode$ SpellCast carries
// `ValidSAonCard$ Spell.ManaSpent LTX` ("Whenever you cast a spell, if the
// amount of mana spent to cast it was less than its mana value, target
// opponent loses life equal to the difference"). Before this fix the
// parameter was read only on the AbilityCast arm, so the SpellCast trigger
// fired WIDE -- it targeted an opponent and drained life on every cast,
// full-price included.
//
// The real-corpus regression below rides Ancient Cellarspawn's OWN
// `S:Mode$ ReduceCost | ValidCard$ Demon,Horror,Nightmare | Type$ Spell`
// static, so the fixture differentiates mana spent from mana value with no
// synthetic mana fiddling: a Horror spell of mana value 2 costs {1} less and
// is paid with 1 mana, a non-Horror spell of the same mana value is paid with
// 2.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
)

// ancientAnswerOpponentTarget answers the pending target decision of an
// Ancient Cellarspawn trigger with seat 1 (the only opponent in a two-seat
// game), or fails.
func ancientAnswerOpponentTarget(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the trigger's opponent-target ask, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("seat 1 was not offered as the trigger's target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
}

// spellCastTriggerFires reports whether the events log holds a TriggerPush
// (a trigger put on the stack) between the last two indices -- a direct,
// matcher-independent witness that Ancient Cellarspawn's trigger fired, used
// to assert the full-price cast's silence even if some other effect were to
// move life.
func spellCastTriggerFires(e *Engine, from int) bool {
	for _, ev := range e.L.Events[from:] {
		if ev.Kind == events.TriggerPush {
			return true
		}
	}
	return false
}

func TestAncientCellarspawnSpellManaSpentGate(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Vile Manifestation", "Grizzly Bears"}, nil,
		[]string{"Ancient Cellarspawn"}, nil)
	// {B} pays the reduced {1}{B} Horror; {B}{G} pays the full-price {1}{G}
	// Bear. Both are exactly affordable.
	addMana(t, e, 0, "BBG")

	spawn := miscBoardObj(t, e, 0, "Ancient Cellarspawn")
	if o := e.G.Obj(spawn); o == nil || o.Face() == nil {
		t.Fatal("test precondition: Ancient Cellarspawn is not on the battlefield")
	}
	// The trigger under test must actually carry the gate this fix reads.
	if trg := e.G.Obj(spawn).Face().Triggers; len(trg) != 1 ||
		trg[0].Params["ValidSAonCard"] != "Spell.ManaSpent LTX" {
		t.Fatalf("test precondition: carrier's SpellCast trigger does not carry ValidSAonCard$ LTX: %+v", trg)
	}
	if e.G.Players[1].Life != 20 {
		t.Fatalf("test precondition: seat 1 life = %d, want 20", e.G.Players[1].Life)
	}

	// --- The underpaid cast: Vile Manifestation is a Horror ({1}{B}), so
	// Ancient Cellarspawn's ReduceCost makes it cost {B}. ---
	horror := miscHandObj(t, e, 0, "Vile Manifestation")
	hFace := e.G.Obj(horror).Face()
	if hFace.ManaValue() != 2 {
		t.Fatalf("test precondition: Vile Manifestation mana value = %d, want 2", hFace.ManaValue())
	}
	isHorror := false
	for _, ty := range hFace.Types {
		if ty == "Horror" {
			isHorror = true
		}
	}
	if !isHorror {
		t.Fatal("test precondition: Vile Manifestation must be a Horror for ReduceCost to apply")
	}
	submitChoices(t, e, miscCastOption(t, e, horror))
	// The cast spell is on the stack now; the pay-time capture records the
	// ACTUAL spend on the object, independently of the log-scan the gate
	// reads. This is the fixture's differentiation check: mana spent (1) must
	// be strictly less than the mana value (2).
	spellObj := e.G.Obj(horror)
	if spellObj == nil || spellObj.Face() == nil {
		t.Fatal("test precondition: the cast Horror is not on the stack")
	}
	if spellObj.ManaSpent >= spellObj.Face().ManaValue() {
		t.Fatalf("test precondition: underpaid cast spent %d, mana value %d -- not strictly less",
			spellObj.ManaSpent, spellObj.Face().ManaValue())
	}
	// The gate holds, so the trigger fires and asks for an opponent.
	ancientAnswerOpponentTarget(t, e)
	passUntilStackEmpty(t, e, 30)
	lifeAfterUnderpaid := e.G.Players[1].Life
	// The trigger's body is "loses life equal to the difference": mana value
	// (2) minus mana actually spent (1) is exactly 1. Asserting the exact
	// difference (not merely "less than 20") is the valid comparison of the
	// same trigger's observed effect against the gate's own operands.
	wantLoss := int32(spellObj.Face().ManaValue() - spellObj.ManaSpent)
	if wantLoss <= 0 {
		t.Fatalf("test precondition: underpaid difference = %d, want > 0", wantLoss)
	}
	if lifeAfterUnderpaid != 20-wantLoss {
		t.Fatalf("underpaid Horror cast drained seat 1 to %d, want %d (mana value %d minus spent %d)",
			lifeAfterUnderpaid, 20-wantLoss, spellObj.Face().ManaValue(), spellObj.ManaSpent)
	}
	replayCheck(t, e, cfg)

	// --- The full-price cast: Grizzly Bears is a Bear ({1}{G}), so no
	// ReduceCost applies and the full 2 mana is spent. ---
	bears := miscHandObj(t, e, 0, "Grizzly Bears")
	bFace := e.G.Obj(bears).Face()
	if bFace.ManaValue() != 2 {
		t.Fatalf("test precondition: Grizzly Bears mana value = %d, want 2", bFace.ManaValue())
	}
	for _, ty := range bFace.Types {
		if ty == "Demon" || ty == "Horror" || ty == "Nightmare" {
			t.Fatalf("test precondition: Grizzly Bears must not be reduced (type %q)", ty)
		}
	}
	lifeBeforeFull := e.G.Players[1].Life
	evBefore := len(e.L.Events)
	submitChoices(t, e, miscCastOption(t, e, bears))
	bearsObj := e.G.Obj(bears)
	if bearsObj == nil || bearsObj.Face() == nil {
		t.Fatal("test precondition: the cast Bear is not on the stack")
	}
	if bearsObj.ManaSpent != bearsObj.Face().ManaValue() {
		t.Fatalf("test precondition: full-price cast spent %d, mana value %d -- not equal",
			bearsObj.ManaSpent, bearsObj.Face().ManaValue())
	}
	// The gate fails, so no trigger: no opponent-target ask, no life change.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("full-price Bear cast wrongly fired the trigger's target ask: %+v", d)
	}
	if spellCastTriggerFires(e, evBefore) {
		t.Fatal("full-price Bear cast wrongly put a trigger on the stack")
	}
	// Settle the cast so the comparison reads a resolved board.
	miscPass(t, e)
	passUntilStackEmpty(t, e, 30)
	if life := e.G.Players[1].Life; life != lifeBeforeFull {
		t.Fatalf("full-price cast drained life: seat 1 %d, want unchanged %d", life, lifeBeforeFull)
	}
	replayCheck(t, e, cfg)
}
