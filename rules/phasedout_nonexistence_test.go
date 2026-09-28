package rules

// CR 702.25b/d — the "treated as though it does not exist" half of phasing,
// beyond the targeting and layer gates: a phased-out permanent's ACTIVATED
// abilities are not offered, and a phased-out permanent is removed from
// combat (CR 702.25c). These are engine-boundary probes on inline fixtures,
// so the only variable is the phased-out flag.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// phasedManaArtifact is an artifact with a real tap-activated mana ability:
// the offer walk (legalActionsPriced) must surface it while phased in and
// withhold it while phased out.
const phasedManaArtifact = "Name:Mana Rock\nManaCost:2\nTypes:Artifact\n" +
	"A:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add {G}.\n" +
	"Oracle:{T}: Add {G}.\n"

// phaseOutActivateOffered reports whether p's legal-action offer includes an
// "activate" (tap-for-mana) option for obj.
func phaseOutActivateOffered(e *Engine, p state.PlayerID, obj state.ObjID) bool {
	for _, o := range e.legalActions(p) {
		if o.Kind == "activate" && o.Obj == obj {
			return true
		}
	}
	return false
}

// TestCR702PhasedOutPermanentActivatedAbilityNotOffered pins CR 702.25b: a
// phased-out permanent is treated as though it does not exist, so none of
// its activated abilities is offered. The phased-in control on the SAME
// object proves the offer walk is the thing under test, not an empty menu.
func TestCR702PhasedOutPermanentActivatedAbilityNotOffered(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 501, phasedManaArtifact)
	// Move the artifact itself onto the battlefield (newFixtureDeck returns
	// the card in hand, and the offer walk only reads battlefield/permanent
	// zones for activated abilities).
	id := moveSeededCard(t, e, 0, card(t, phasedManaArtifact), state.ZBattlefield)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: Mana Rock not a phased-in battlefield permanent: %+v", o)
	}
	// Precondition: the activated ability IS offered while phased in.
	if !phaseOutActivateOffered(e, 0, id) {
		t.Fatal("precondition: the phased-in artifact's mana ability was not offered")
	}
	e.emit(events.Event{Kind: events.PhaseOut, Obj: id, Amount: 1})
	if o := e.G.Obj(id); o == nil || !o.PhasedOut {
		t.Fatal("precondition: the artifact was not phased out")
	}
	if phaseOutActivateOffered(e, 0, id) {
		t.Fatal("CR 702.25b: a phased-out permanent's activated ability was offered")
	}
}

// TestCR702PhasedOutAttackerIsRemovedFromCombat pins CR 702.25c: "A permanent
// that phases out is removed from combat." The state is set up directly (the
// combat-declaration path is covered by combat_test.go); the assertion is on
// the PhaseOut fold, which must clear the attacker even though phasing is not
// a zone change and no Move fold runs.
func TestCR702PhasedOutAttackerIsRemovedFromCombat(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 502, "Grizzly Bears")
	bears := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	o := e.G.Obj(bears)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Bears not on the battlefield")
	}
	// Precondition: the object really is an attacking creature before the
	// phase-out (a vacuous setup must fail loudly).
	o.IsAttacking = true
	if !e.G.Obj(bears).IsAttacking {
		t.Fatal("precondition: Bears are not attacking after direct setup")
	}
	e.emit(events.Event{Kind: events.PhaseOut, Obj: bears, Amount: 1})
	got := e.G.Obj(bears)
	if got == nil || !got.PhasedOut {
		t.Fatal("precondition: Bears were not phased out")
	}
	if got.IsAttacking {
		t.Fatal("CR 702.25c: a phased-out attacker was not removed from combat")
	}
	if got.BlockedBy != nil {
		t.Fatalf("CR 702.25c: phased-out attacker kept BlockedBy %v, want nil", got.BlockedBy)
	}
	replayCheck(t, e, cfg)
}

// TestCR702PhasedOutBlockerLeavesZeroTombstone pins the CR 509.1h half of the
// combat removal: a phased-out BLOCKER no longer absorbs damage, but the
// attacker it blocked stays blocked (a zero tombstone, not a shortened list).
func TestCR702PhasedOutBlockerLeavesZeroTombstone(t *testing.T) {
	t.Parallel()
	e, _ := phasesGame(t, 503, "Grizzly Bears", "Hill Giant")
	attacker := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	blocker := moveSeededCard(t, e, 0, tokenReplCorpusCard(t, "Hill Giant"), state.ZBattlefield)
	a := e.G.Obj(attacker)
	if a == nil || e.G.Obj(blocker) == nil {
		t.Fatal("precondition: attacker/blocker not on the battlefield")
	}
	a.IsAttacking = true
	a.BlockedBy = []state.ObjID{blocker}
	e.emit(events.Event{Kind: events.PhaseOut, Obj: blocker, Amount: 1})
	if b := e.G.Obj(blocker); b == nil || !b.PhasedOut {
		t.Fatal("precondition: blocker was not phased out")
	}
	got := e.G.Obj(attacker).BlockedBy
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("CR 509.1h: attacker BlockedBy = %v, want [0] (a zero tombstone keeping it blocked)", got)
	}
}

// guard against the fixture constant drifting out of the parser: an artifact
// with no activated ability would make the offer test vacuous.
func TestPhasedManaArtifactFixtureHasAnActivatedAbility(t *testing.T) {
	t.Parallel()
	c := card(t, phasedManaArtifact)
	if len(c.Faces) == 0 || len(c.Faces[0].Abilities) == 0 {
		t.Fatal("fixture Mana Rock parsed with no activated ability")
	}
}
