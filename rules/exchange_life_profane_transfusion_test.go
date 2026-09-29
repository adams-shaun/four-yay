package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestProfaneTransfusionDifferenceRiderSurvivesDredgeSuspension pins the
// RememberDifference half of the ExchangeLife numeric rider
// (agent-20260928T235256Z-127d875e). Profane Transfusion is a two-target
// exchange; its DBToken reads X:Count$RememberedNumber/Abs for the created
// Horror's power and toughness. When Lich replaces one side's life gain with
// draws whose dredge asks suspend the exchange mid-resolution, the created
// token must still be X/X where X is the life difference -- the rider value
// written on the resolving Ctx must reach the SubAbility$ continuation that a
// resume rebuilds on a FRESH Ctx (the shared-pointer defect).
//
// The value is set synchronously by effExchangeLife right after the host
// ExchangeLife call returns (RememberDifference$), so this test isolates the
// Ctx-identity defect from the settle-ordering defect the Mister Negative test
// covers.
func TestProfaneTransfusionDifferenceRiderSurvivesDredgeSuspension(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	lich := mustCorpusCard(t, reg, "Lich")
	thug := mustCorpusCard(t, reg, "Golgari Thug")
	e, _ := searchEngine(t, reg, "Profane Transfusion")
	// Seat 0 is the gaining side after the exchange (10 -> 20), so Lich must
	// be on seat 0's battlefield and the dredger in seat 0's graveyard.
	lichID := onBoardCard(t, e, 0, lich)
	if o := e.G.Obj(lichID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Lich not on seat 0's battlefield: %+v", o)
	}
	thugObj := e.G.AddObject(thug, 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: thugObj.ID, From: thugObj.Zone, To: state.ZGraveyard})
	if o := e.G.Obj(thugObj.ID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("precondition: dredger not in graveyard: %+v", o)
	}
	// Make the exchange have a real difference: seat 0 at 10, seat 1 at 20.
	e.emit(events.Event{Kind: events.LifeChange, Player: 0, Amount: -10})
	if e.G.Players[0].Life != 10 || e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: lives = %d/%d, want distinct 10/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	ensureInHand(t, e, 0, "Profane Transfusion")
	addMana(t, e, 0, "CCCCCCBBB")
	castCardNow(t, e, "Profane Transfusion")
	d := passToTargetAsk(t, e)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending after cast = %+v, want Profane Transfusion's target ask", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("precondition: target bounds = %d..%d, want 2..2", d.Min, d.Max)
	}
	i0, i1 := indexOfPlayerOption(d, 0), indexOfPlayerOption(d, 1)
	if i0 < 0 || i1 < 0 {
		t.Fatalf("precondition: both players must be offered: %+v", d.Options)
	}
	submitChoices(t, e, i0, i1)
	// The exchange's gaining side (seat 0) is replaced by Lich draws that park
	// on dredge asks; decline each one until the stack drains.
	dredges := 0
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 && dredges > 0 {
				break
			}
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KModes:
			if d.ResumeKind != "dredge" {
				t.Fatalf("unexpected modal ask: %+v", d)
			}
			dredges++
			submitChoices(t, e, len(d.Options)-1)
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				t.Fatal("no pass")
			}
			submitChoices(t, e, idx)
			if len(e.G.Stack) == 0 && dredges > 0 {
				i = 400
			}
		default:
			t.Fatalf("unexpected ask: %s %+v", d.Kind, d)
		}
	}
	if dredges != 10 {
		t.Fatalf("Lich posed %d dredge asks for the replaced 10-point gain, want 10", dredges)
	}
	// Seat 0's 10-point gain (10 -> 20) was consumed by Lich into draws, so it
	// stays at 10; seat 1 lost 10 (20 -> 10).
	if got0, got1 := e.G.Players[0].Life, e.G.Players[1].Life; got0 != 10 || got1 != 10 {
		t.Fatalf("lives after the suspended exchange = %d/%d, want 10/10 (seat 0's gain replaced by Lich)", got0, got1)
	}
	// The difference was 10, so DBToken must create a 10/10 Horror.
	tok := tokenNamedOnBattlefield(t, e, 0, "Horror Token")
	dv := e.Derived(tok)
	if dv.Power != 10 || dv.Toughness != 10 {
		t.Fatalf("rider: token P/T = %d/%d, want 10/10 = the life difference", dv.Power, dv.Toughness)
	}
}

// tokenNamedOnBattlefield returns the first battlefield permanent seat p
// controls whose face name is name, failing loudly when absent.
func tokenNamedOnBattlefield(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("no %q permanent on seat %d's battlefield", name, p)
	return 0
}
