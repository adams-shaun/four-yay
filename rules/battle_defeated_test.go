package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file closes the "defeated" sub-shape of AGENTS.md's (battle1)
// approximation row: the defeat SBA (rules/sba.go's battleZeroDefense) exiles
// a battle with no defense counters instead of binning it to the graveyard,
// and the owner answers CR 310.11's "may cast it transformed without paying
// its mana cost" offer -- a yes pushes the card back to the stack as its BACK
// face (mode defeat_cast), a decline leaves it in exile.
//
// Invasion of Pyrulea is the carrier for the cast tests: its back face is
// Gargantuan Slabhorn, a plain 4/4 Beast with no targets, so the free cast
// needs no further decision. Both cards are in no repo deck, so these games
// never touch the golden heads.

// defeatedBoard seeds the named corpus Battle under seat 0 through
// battleBoard (entry grant + CR 310.10 protector answer), strips its printed
// defense counters through the ordinary counter-change event, and runs one
// state-based pass. Returns the engine and the battle's id.
func defeatedBoard(t *testing.T, name string) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, _, id := battleBoard(t, reg, name)
	return e, id
}

// defeatBattle removes every defense counter on the battle through the
// ordinary counter-change event and runs the defeat SBA over it.
func defeatBattle(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	n := e.G.Obj(id).Counter("DEFENSE")
	if n <= 0 {
		t.Fatalf("precondition: battle %d entered with %d defense counters, want > 0", id, n)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "DEFENSE", Amount: -n})
	e.checkStateBased()
}

// awaitDefeatAsk drives the engine exactly the production route drains the
// defeat queue: at the next step() before priority. battleBoard parks on a
// priority decision, so the passes get answered first and the offer drains
// from the queue the moment the engine reaches a decision-free boundary.
// Returns the posed ask.
func awaitDefeatAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	e.step()
	for i := 0; i < 12; i++ {
		d := e.Pending()
		if d == nil {
			e.step()
			d = e.Pending()
			if d == nil {
				break
			}
		}
		if d.Kind == decision.KChoose && len(d.Options) == 2 && d.Options[0].Kind == "defeat_cast_yes" {
			return d
		}
		if d.Kind == decision.KPriority {
			passPriority(t, e)
			continue
		}
		break
	}
	t.Fatal("defeated-battle cast ask was never posed")
	return nil
}

// TestDefeatedBattleIsExiledNotGraveyarded is the defeat SBA's destination:
// a battle with no defense counters is DEFEATED -- exiled (CR 310.11), not
// put into its owner's graveyard.
func TestDefeatedBattleIsExiledNotGraveyarded(t *testing.T) {
	t.Parallel()
	e, id := defeatedBoard(t, "Invasion of Tolvada")
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || !o.Face().IsBattle() {
		t.Fatalf("precondition: battle %d missing from the battlefield", id)
	}
	if got := o.Counter("DEFENSE"); got != 5 {
		t.Fatalf("precondition: battle carries %d defense counters, want 5", got)
	}
	defeatBattle(t, e, id)
	if got := e.G.Obj(id).Zone; got != state.ZExile {
		t.Fatalf("battle at 0 defense went to %v, want exile", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZBattlefield {
			if ev.To == state.ZGraveyard {
				t.Fatal("the defeat move went to the graveyard, want exile")
			}
			if ev.To == state.ZExile {
				return
			}
		}
	}
	t.Fatal("no battlefield→exile defeat move in the log")
}

// TestHealthyExiledBattleGetsNoCastOffer pins the feed's discriminator: a
// battle exiled by some other route while it still HAS defense counters was
// never defeated, so no transformed-cast offer may be posed for it. Apply's
// Move clears o.Counters as the object leaves the battlefield, so the queue
// must read the pre-fold defense count (Engine.emit's defenseBefore).
func TestHealthyExiledBattleGetsNoCastOffer(t *testing.T) {
	t.Parallel()
	e, id := defeatedBoard(t, "Invasion of Pyrulea")
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: battle %d missing from the battlefield", id)
	}
	if got := o.Counter("DEFENSE"); got != 4 {
		t.Fatalf("precondition: battle carries %d defense counters, want 4", got)
	}
	// An effect's own exile of a healthy battle (a blink, a Banishing Stroke):
	// a plain logged battlefield→exile move.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZExile})
	e.checkStateBased()
	e.step()
	if len(e.defeatedCasts) != 0 {
		t.Fatalf("healthy exile queued %d cast offers, want none", len(e.defeatedCasts))
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 &&
		d.Options[0].Kind == "defeat_cast_yes" {
		t.Fatal("a transformed-cast offer was posed for a battle that was never defeated")
	}
}

// TestDefeatedBattleOffersTransformedCast is CR 310.11's cast half: the
// defeat poses the exiled battle's owner a real cast-or-decline ask, and a
// yes casts the card TRANSFORMED -- pushed to the stack as the back face
// without paying its mana cost, and resolved onto the battlefield as that
// back face.
func TestDefeatedBattleOffersTransformedCast(t *testing.T) {
	t.Parallel()
	e, id := defeatedBoard(t, "Invasion of Pyrulea")
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Card == nil || len(o.Card.Faces) != 2 {
		t.Fatalf("precondition: battle %d missing or not a transforming card", id)
	}
	if back := o.Card.Faces[1]; back == nil || back.Name != "Gargantuan Slabhorn" {
		t.Fatalf("precondition: back face %v, want Gargantuan Slabhorn", back)
	}
	if got := o.Counter("DEFENSE"); got != 4 {
		t.Fatalf("precondition: battle carries %d defense counters, want 4", got)
	}
	defeatBattle(t, e, id)
	if got := e.G.Obj(id).Zone; got != state.ZExile {
		t.Fatalf("precondition: battle went to %v, want exile before the offer", got)
	}
	// The offer drains from the queue at the next step() before priority,
	// after the priority round battleBoard parked on.
	d := awaitDefeatAsk(t, e)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
		d.Options[0].Kind != "defeat_cast_yes" || d.Options[1].Kind != "defeat_cast_no" {
		t.Fatalf("no defeated-battle cast ask posed, got %+v", d)
	}
	if d.Player != 0 {
		t.Fatalf("cast ask went to seat %d, want the battle's owner seat 0", d.Player)
	}
	if d.Options[0].Obj != id {
		t.Fatalf("cast ask names object %d, want the defeated battle %d", d.Options[0].Obj, id)
	}
	submitChoices(t, e, 0)
	o = e.G.Obj(id)
	if !hasEvent(e, events.PutOnStack, id) {
		t.Fatal("accepting the defeat cast did not put the battle on the stack")
	}
	if o.Zone != state.ZStack {
		t.Fatalf("accepted cast ended in %v, want stack", o.Zone)
	}
	if o.FaceIdx != 1 {
		t.Fatalf("accepted cast pushed face index %d, want the back face 1", o.FaceIdx)
	}
	if f := o.Face(); f == nil || f.Name != "Gargantuan Slabhorn" {
		t.Fatalf("stack face is %v, want the transformed back face", f)
	}
	// Resolution: both seats pass priority and the transformed permanent
	// enters the battlefield.
	for i := 0; i < 12 && e.G.Obj(id).Zone == state.ZStack; i++ {
		passPriority(t, e)
	}
	o = e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("transformed cast resolved into %v, want battlefield", o.Zone)
	}
	if o.FaceIdx != 1 {
		t.Fatalf("resolved permanent is face index %d, want the transformed back face 1", o.FaceIdx)
	}
}

// TestDefeatedBattleDeclineLeavesInExile is CR 310.11's decline half: a
// decline leaves the exiled battle in exile (front face, unflipped) and
// offers nothing further.
func TestDefeatedBattleDeclineLeavesInExile(t *testing.T) {
	t.Parallel()
	e, id := defeatedBoard(t, "Invasion of Pyrulea")
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || len(o.Card.Faces) != 2 {
		t.Fatalf("precondition: battle %d missing or not a transforming card", id)
	}
	if got := o.Counter("DEFENSE"); got != 4 {
		t.Fatalf("precondition: battle carries %d defense counters, want 4", got)
	}
	defeatBattle(t, e, id)
	d := awaitDefeatAsk(t, e)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("precondition: no defeated-battle cast ask posed, got %+v", d)
	}
	submitChoices(t, e, 1)
	o = e.G.Obj(id)
	if o.Zone != state.ZExile {
		t.Fatalf("declined battle ended in %v, want exile", o.Zone)
	}
	if o.FaceIdx != 0 {
		t.Fatalf("declined battle is face index %d, want the unflipped front face 0", o.FaceIdx)
	}
	if len(e.defeatedCasts) != 0 {
		t.Fatalf("decline left %d defeated battles queued, want the queue drained", len(e.defeatedCasts))
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose && len(d.Options) > 0 &&
		d.Options[0].Kind == "defeat_cast_yes" {
		t.Fatal("a second defeat cast ask was posed after the decline")
	}
}
