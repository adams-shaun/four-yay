package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestStandaloneManaReflectedAsksForTheColour pins the half of the
// api:ManaReflected approximation that is NOT the mana-activation path: a DB$
// ManaReflected body reached on its own (here the SubAbility$ of an ordinary
// activated ability) resolves a reflected set that offers several colours, so
// its controller must be ASKED which one to add. Before the fix the effect
// always emitted a Note and silently took the fixed first candidate.
func TestStandaloneManaReflectedAsksForTheColour(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t,
		"Name:Reflect Test\nTypes:Artifact\n"+
			"A:AB$ PutCounter | Cost$ 0 | CounterType$ P1P1 | Defined$ Self | SubAbility$ DBMana\n"+
			"SVar:DBMana:DB$ ManaReflected | ReflectProperty$ Is | ColorOrType$ Color | Amount$ 1 | Valid$ Creature\n"+
			"Oracle:x\n"))
	reflector := e.G.Obj(e.G.Zone(state.ZHand, 0)[0])
	reflector.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{reflector.ID})
	// Two differently coloured creatures: the reflected set is {W, U}, a
	// genuine two-way choice. (Asserted below: the two options must differ.)
	w := e.G.AddObject(card(t, "Name:White Helper\nManaCost:W\nTypes:Creature Soldier\nPT:1/1\nOracle:x\n"), 0)
	u := e.G.AddObject(card(t, "Name:Blue Helper\nManaCost:U\nTypes:Creature Wizard\nPT:1/1\nOracle:x\n"), 0)
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{reflector.ID, w.ID, u.ID})
	addMana(t, e, 0, "")

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision to activate the reflector from: %+v", d)
	}
	opt := -1
	for _, o := range d.Options {
		if o.Obj == reflector.ID && (o.Kind == "ability" || o.Kind == "activate") {
			opt = o.Index
		}
	}
	if opt < 0 {
		t.Fatalf("the activated ability carrying the DB$ ManaReflected was not offered: %+v", d.Options)
	}
	submitChoices(t, e, opt)

	// The ability is on the stack: pass priority until the mid-resolution
	// colour ask appears (it must NOT be auto-answered).
	ask := e.Pending()
	for i := 0; i < 20 && ask != nil && ask.Kind == decision.KPriority; i++ {
		pass := -1
		for _, o := range ask.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision with no pass option: %+v", ask.Options)
		}
		submitChoices(t, e, pass)
		ask = e.Pending()
	}
	if ask == nil || ask.Kind != decision.KChoose || ask.ResumeKind != "manareflected" {
		t.Fatalf("standalone ManaReflected posed no colour ask: %+v", ask)
	}
	if len(ask.Options) != 2 {
		t.Fatalf("colour ask options = %d, want the 2 reflected colours: %+v", len(ask.Options), ask.Options)
	}
	if ask.Options[0].Label == ask.Options[1].Label {
		t.Fatalf("precondition failed: the two reflected colours are identical: %+v", ask.Options)
	}
	pick := -1
	for _, o := range ask.Options {
		if o.Label == "Add U" {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("no blue option among the reflected colours: %+v", ask.Options)
	}
	submitChoices(t, e, pick)
	passUntilStackEmpty(t, e, 50)

	if got := e.G.Players[0].Pool[state.MU]; got != 1 {
		t.Fatalf("pool U=%d, want the ASKED-FOR colour to be added (1)", got)
	}
	if e.G.Players[0].Pool[state.MW] != 0 {
		t.Fatalf("pool W=%d, want 0 — the answer must pick, never take the first candidate", e.G.Players[0].Pool[state.MW])
	}
}
