package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kr4ResolveChooseSource resolves a spell's SP$ ChooseSource chain under a
// kernel probe, answers the KChoose with want, and asserts the answer landed
// on the spell's Chosen list.
func kr4ResolveChooseSource(t *testing.T, e *Engine, spell state.ObjID, want state.ObjID) {
	t.Helper()
	o := e.G.Obj(spell)
	face, ctl := o.Face(), o.Controller
	kr4Resolve(e, func() *effects.Ctx { return &effects.Ctx{Source: spell, Controller: ctl, SVars: face.SVars} }, face.SpellAbility())
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("ChooseSource did not pose a KChoose, got %+v", d)
	}
	submitChoices(t, e, kr4Option(t, d, want))
	if ch := e.G.Obj(spell).Chosen; len(ch) != 1 || ch[0].Obj != want {
		t.Fatalf("chosen = %+v, want [%d]", ch, want)
	}
}

// kr4DamageFrom logs amount damage from src to player p.
func kr4DamageFrom(e *Engine, src state.ObjID, p state.PlayerID, amount int32) {
	e.damaging = src
	e.emit(events.Event{Kind: events.Damage, Player: p, Amount: amount})
	e.damaging = 0
}

// TestDeflectingPalmPreventsChosenSourceAndReflectsKernel: Deflecting Palm's
// answered source is prevented (to its controller only) and reflected to that
// source's controller; an unchosen source and damage to another player are
// untouched.
func TestDeflectingPalmPreventsChosenSourceAndReflectsKernel(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	chosen := onBoard(t, e, 1, "Name:Aggressor\nManaCost:R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	other := onBoard(t, e, 1, "Name:Bystander\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	palm := e.G.AddObject(mustCorpusCard(t, reg, "Deflecting Palm"), 0)
	palm.Zone = state.ZStack
	kr4ResolveChooseSource(t, e, palm.ID, chosen)

	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	kr4DamageFrom(e, chosen, 0, 3)
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 life = %d, want %d: the chosen source's damage was not prevented", got, life0)
	}
	if got := e.G.Players[1].Life; got != life1-3 {
		t.Fatalf("seat 1 life = %d, want %d: the prevented amount was not reflected", got, life1-3)
	}
	kr4DamageFrom(e, other, 0, 2)
	if got := e.G.Players[0].Life; got != life0-2 {
		t.Fatalf("seat 0 life = %d, want %d: an unchosen source's damage must not be prevented", got, life0-2)
	}
	life1 = e.G.Players[1].Life
	kr4DamageFrom(e, chosen, 1, 4)
	if got := e.G.Players[1].Life; got != life1-4 {
		t.Fatalf("seat 1 life = %d, want %d: ValidTarget$ You must not prevent damage to another player", got, life1-4)
	}
}

// TestDeflectingPalmPreventsOnlyTheNextDamageKernel: the CR 615 one-shot --
// the first damage event from the chosen source is prevented and reflected,
// the registration ends, and a second event lands in full.
func TestDeflectingPalmPreventsOnlyTheNextDamageKernel(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := newSeats(t, 2)
	e.pending = nil
	chosen := onBoard(t, e, 1, "Name:Aggressor\nManaCost:R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	palm := e.G.AddObject(mustCorpusCard(t, reg, "Deflecting Palm"), 0)
	palm.Zone = state.ZStack
	kr4ResolveChooseSource(t, e, palm.ID, chosen)

	frames := func() int {
		n := 0
		for _, ce := range e.continuous {
			if ce.Source == palm.ID && ce.ReplacementEvent == "DamageDone" {
				n++
			}
		}
		return n
	}
	if n := frames(); n != 1 {
		t.Fatalf("precondition: %d DamageDone registrations from the spell, want 1", n)
	}
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	kr4DamageFrom(e, chosen, 0, 3)
	if e.G.Players[0].Life != life0 || e.G.Players[1].Life != life1-3 {
		t.Fatalf("first damage: lives %d/%d, want %d/%d", e.G.Players[0].Life, e.G.Players[1].Life, life0, life1-3)
	}
	if n := frames(); n != 0 {
		t.Fatalf("after one application %d registrations remain, want the one-shot ended", n)
	}
	life0, life1 = e.G.Players[0].Life, e.G.Players[1].Life
	kr4DamageFrom(e, chosen, 0, 4)
	if e.G.Players[0].Life != life0-4 || e.G.Players[1].Life != life1 {
		t.Fatalf("second damage: lives %d/%d, want %d/%d", e.G.Players[0].Life, e.G.Players[1].Life, life0-4, life1)
	}
}
