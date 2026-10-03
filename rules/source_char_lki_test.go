package rules

// CR 113.7a / 608.2b / 608.2h: a target filter bounded by the ability's own
// source's characteristic ("mana value less than or equal to this creature's
// power") is re-judged at resolution against the source's LAST KNOWN
// information when the source has left the battlefield -- Alesha, Who Laughs
// at Fate destroyed in response to her raid ability. The snapshot is captured
// at the departure boundary (source_char_lki.go) for a waiting ability, and
// before the cost is paid for an ability that sacrifices its own source.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const lkiThreeDropSrc = "Name:ThreeDrop\nManaCost:2 G\nTypes:Creature Beast\nPT:3/3\nOracle:x\n"

func lkiRaiserSrc(cost string) string {
	return "Name:LKIRaiser\nManaCost:0\nTypes:Creature Human\nPT:1/1\n" +
		"A:AB$ ChangeZone | Cost$ " + cost + " | Origin$ Graveyard | Destination$ Battlefield | " +
		"ValidTgts$ Creature.YouOwn+cmcLEX | TgtPrompt$ Select target creature card with mana value less than or equal to CARDNAME's power | " +
		"SpellDescription$ Return target creature card with mana value less than or equal to CARDNAME's power.\n" +
		"SVar:X:Count$CardPower\nOracle:x\n"
}

func TestSourceRelativeTargetFilterReadsDepartedSourceLKI(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, cost string
		destroy    bool // destroy the source with the ability on the stack
	}{
		{"destroyed-in-response", "0", true},
		{"sacrificed-as-cost", "Sac<1/CARDNAME>", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, cfg, id := newFixtureDeck(t, 23, lkiRaiserSrc(tc.cost), lkiThreeDropSrc)
			moveByName(t, e, 0, "LKIRaiser", state.ZBattlefield)
			drop := moveByName(t, e, 0, "ThreeDrop", state.ZGraveyard)
			// Power 3 only through counters, which leave with the permanent.
			e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 2})
			addMana(t, e, 0, "")
			if got := e.Power(id); got != 3 {
				t.Fatalf("precondition: LKIRaiser power %d, want 3", got)
			}

			submitChoices(t, e, abilityOption(t, e, id, 0).Index)
			answerTargetAsk(t, e, []state.ObjID{drop})
			if len(e.G.Stack) != 1 {
				t.Fatalf("activation did not reach the stack: %v", e.G.Stack)
			}
			if tc.destroy {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
			}
			if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
				t.Fatalf("precondition: source zone %v, want graveyard", z)
			}
			passUntilStackEmpty(t, e, 400)
			if z := e.G.Obj(drop).Zone; z != state.ZBattlefield {
				t.Fatalf("the target is legal by the source's last known power 3 (CR 608.2h); it stayed in zone %v", z)
			}
			if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
				t.Fatalf("log-only replay differs:\n%s", diff)
			}
		})
	}
}

// TestSourceRelativeTargetFilterLiveSourceUnchanged is the control: with the
// source alive at power 1 the mana-value-3 card is never offered.
func TestSourceRelativeTargetFilterLiveSourceUnchanged(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 23, lkiRaiserSrc("0"), lkiThreeDropSrc)
	moveByName(t, e, 0, "LKIRaiser", state.ZBattlefield)
	drop := moveByName(t, e, 0, "ThreeDrop", state.ZGraveyard)
	addMana(t, e, 0, "")
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		// No legal target at all withholds the activation (CR 601.2c).
		return
	}
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d != nil && targetOptionContains(d.Options, drop) {
		t.Fatalf("mana value 3 offered against a power-1 source: %+v", d.Options)
	}
}
