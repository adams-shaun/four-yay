package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// rescue_pepper_potts_test.go pins the ConditionDefined$ Targeted gate on the
// real repo-deck carrier (avengers-assemble's Rescue, Pepper Potts): "return up
// to one other target artifact or creature you control to its owner's hand. If
// it was an artifact, put a +1/+1 counter on NICKNAME." Before the gate read
// the Targeted group it failed OPEN, so Rescue took the counter whatever it
// bounced -- a plain creature, or nothing at all. That fix is what moved the
// botbench constructed-default golden (ticket agent-20260918T210307Z-25a7b039).

const rescueTrinketSrc = "Name:Trinket\nManaCost:1\nTypes:Artifact\nOracle:x\n"

// rescueCast casts the real Rescue from seat 0's hand with a Trinket (plain
// artifact) and a Bear (plain creature) on seat 0's battlefield, resolves the
// spell, and answers the ETB trigger's up-to-one target ask with want (0 = no
// target). It returns the engine and Rescue's id once the stack is quiet.
func rescueCast(t *testing.T, want string) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Rescue, Pepper Potts"), card(t, rescueTrinketSrc), card(t, bearSrc)},
		[]*cards.Card{})
	ids := map[string]state.ObjID{
		"Trinket": moveByName(t, e, 0, "Trinket", state.ZBattlefield),
		"Bear":    moveByName(t, e, 0, "Bear", state.ZBattlefield),
	}
	rescue := moveByName(t, e, 0, "Rescue, Pepper Potts", state.ZHand)
	addMana(t, e, 0, "UU")
	castNamed(t, e, "Rescue, Pepper Potts")
	answered := false
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KPriority:
			if len(e.G.Zone(state.ZStack, 0)) == 0 && answered {
				i = 40
				continue
			}
			castFirst(t, e, "pass")
		case decision.KTarget:
			answered = true
			if want == "" {
				submitChoices(t, e)
				continue
			}
			submitTargetFor(t, e, ids[want])
		default:
			submitChoices(t, e, d.Options[0].Index)
		}
		if answered && len(e.G.Zone(state.ZStack, 0)) == 0 {
			break
		}
	}
	if !answered {
		t.Fatalf("Rescue's ETB target ask was never posed")
	}
	if want != "" {
		if o := e.G.Obj(ids[want]); o == nil || o.Zone != state.ZHand {
			t.Fatalf("%s was not returned to hand: %+v", want, o)
		}
	}
	return e, rescue
}

func TestRescuePepperPottsArtifactBounceAddsCounter(t *testing.T) {
	t.Parallel()
	e, rescue := rescueCast(t, "Trinket")
	if n := e.G.Obj(rescue).Counter("P1P1"); n != 1 {
		t.Fatalf("Rescue bounced an artifact: P1P1 = %d, want 1", n)
	}
}

func TestRescuePepperPottsCreatureBounceAddsNoCounter(t *testing.T) {
	t.Parallel()
	e, rescue := rescueCast(t, "Bear")
	if n := e.G.Obj(rescue).Counter("P1P1"); n != 0 {
		t.Fatalf("Rescue bounced a non-artifact creature: P1P1 = %d, want 0 (the gate failed open)", n)
	}
}

func TestRescuePepperPottsNoTargetAddsNoCounter(t *testing.T) {
	t.Parallel()
	e, rescue := rescueCast(t, "")
	if n := e.G.Obj(rescue).Counter("P1P1"); n != 0 {
		t.Fatalf("Rescue returned nothing: P1P1 = %d, want 0 (the gate failed open)", n)
	}
}
