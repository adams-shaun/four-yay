package rules

// The attack/block mana windows pin a choice-shaped Produced$ to one concrete
// colour so their one tap ask cannot displace a nested colour decision. That
// immutable copy must still retain the original ability identity: a Combo R
// Chosen source recorded as G can only pin R or G (never raw-parser W), and
// an ActivationLimit$ marker must be emitted from either shared window.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const limitedComboChosenLand = "Name:Thriving Test Land\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ Combo R Chosen | Amount$ 2 | ActivationLimit$ 1 | Oracle:x\n"

func recordChosenGreen(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: source %d is not an untapped battlefield permanent: %+v", id, o)
	}
	e.emit(events.Event{Kind: events.Choose, Obj: id, Counter: "color", Text: "G"})
	if got := e.chosenProducedColour(id); got != "G" {
		t.Fatalf("precondition: recorded chosen colour = %q, want G", got)
	}
}

func assertLimitedWindowSource(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) {
	t.Helper()
	sources := e.attackManaSources(p)
	if len(sources) != 1 || sources[0].id != id {
		t.Fatalf("precondition: window sources = %+v, want only %d", sources, id)
	}
	if got, want := sources[0].ma.Params["Produced"], "R"; got != want {
		t.Fatalf("source pinned production = %q, want %q (recorded G permits R/G, never W)", got, want)
	}
	if got, want := sources[0].original.Params["Produced"], "Combo R Chosen"; got != want {
		t.Fatalf("source original production = %q, want %q", got, want)
	}
}

func findAttackPair(d *decision.Decision, attacker state.ObjID, defender state.PlayerID) *decision.Option {
	if d == nil {
		return nil
	}
	for i := range d.Options {
		if d.Options[i].Obj == attacker && d.Options[i].Player == defender {
			return &d.Options[i]
		}
	}
	return nil
}

func assertWindowActivationLimit(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) {
	t.Helper()
	markers := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.ManaActivate && ev.Obj == id {
			markers++
			if ev.Player != p || ev.Amount != 0 || len(ev.IDs) != 0 {
				t.Fatalf("ManaActivate marker = %+v, want source %d's printed ability index 0", ev, id)
			}
		}
	}
	if markers != 1 {
		t.Fatalf("ManaActivate markers for window source = %d, want 1", markers)
	}
	e.emit(events.Event{Kind: events.Untap, Obj: id})
	if got := e.availableManaAbilities(p, id); len(got) != 0 {
		t.Fatalf("ActivationLimit$ 1 source re-offered after its window activation: %+v", got)
	}
}

// TestAttackManaWindowRetainsChosenAndLimitIdentity drives the attack shared
// payment window. Its one source must pay Ghostly Prison's {2}, be pinned to
// a colour it really can produce, and record the printed limit marker.
func TestAttackManaWindowRetainsChosenAndLimitIdentity(t *testing.T) {
	t.Parallel()
	e, bear := attackPropSeat(t, "Ghostly Prison", 0)
	land := onBoardCard(t, e, 1, card(t, limitedComboChosenLand))
	recordChosenGreen(t, e, land)
	assertLimitedWindowSource(t, e, 1, land)

	e.askAttackers()
	d := e.Pending()
	opt := findAttackPair(d, bear, 0)
	if opt == nil {
		t.Fatalf("precondition: Ghostly Prison attack is not offered despite its {2} source: %+v", d)
	}
	submitChoices(t, e, opt.Index)
	pay := e.Pending()
	if pay == nil || pay.Kind != decision.KChoose || len(pay.Options) != 1 || pay.Options[0].Kind != "attack_mana" || pay.Options[0].Obj != land {
		t.Fatalf("attack payment ask = %+v, want the limited chosen-colour source", pay)
	}
	submitChoices(t, e, pay.Options[0].Index)
	if o := e.G.Obj(bear); o == nil || !o.IsAttacking {
		t.Fatalf("attack did not commit after payment: %+v", o)
	}
	assertWindowActivationLimit(t, e, 1, land)
}

// TestBlockManaWindowRetainsChosenAndLimitIdentity drives the same source
// through the block payment path, so a future fix cannot preserve the attack
// marker while dropping the blocker half.
func TestBlockManaWindowRetainsChosenAndLimitIdentity(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	blocker := onBoardCard(t, e, 0, mshCorpusCard(t, "Qal Sisma Behemoth"))
	land := onBoardCard(t, e, 0, card(t, limitedComboChosenLand))
	attacker := onBoardReady(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:0/4\nOracle:x\n")
	recordChosenGreen(t, e, land)
	assertLimitedWindowSource(t, e, 0, land)
	attackSeat0(t, e, attacker)

	d := askBlockersFresh(t, e)
	opt := findBlockOption(d, blocker, attacker)
	if opt == nil {
		t.Fatalf("precondition: Qal block is not offered despite its {2} source: %+v", d)
	}
	submitChoices(t, e, opt.Index)
	pay := e.Pending()
	if pay == nil || pay.Kind != decision.KChoose || len(pay.Options) != 1 || pay.Options[0].Kind != "block_mana" || pay.Options[0].Obj != land {
		t.Fatalf("block payment ask = %+v, want the limited chosen-colour source", pay)
	}
	submitChoices(t, e, pay.Options[0].Index)
	if got := e.G.Obj(attacker).BlockedBy; len(got) != 1 || got[0] != blocker {
		t.Fatalf("block did not commit after payment: BlockedBy = %v, want [%d]", got, blocker)
	}
	assertWindowActivationLimit(t, e, 0, land)
}
