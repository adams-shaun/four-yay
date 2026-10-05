package rules

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestLCICombatTargetOracleCards pins the LCI combat/tapped-target verdicts:
// Cosmium Blast destroys an attacking Grizzly Bears, while Dreadmaw's Ire
// leaves that same combatant tapped and attacking as a 4/4.
func TestLCICombatTargetOracleCards(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := lookup(t, reg, "Grizzly Bears")
	cosmium := lookup(t, reg, "Cosmium Blast")
	ire := lookup(t, reg, "Dreadmaw's Ire")

	for _, tc := range []struct {
		name     string
		spell    *cards.Card
		mana     string
		wantZone state.Zone
		wantPT   string
	}{
		{name: "Cosmium Blast", spell: cosmium, mana: "CW", wantZone: state.ZGraveyard},
		{name: "Dreadmaw's Ire", spell: ire, mana: "R", wantZone: state.ZBattlefield, wantPT: "4/4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := corpusEngine(t, reg, []*cards.Card{bear, tc.spell}, nil)
			active := e.G.Active
			defender := state.PlayerID(1 - active)
			attacker := moveByName(t, e, active, "Grizzly Bears", state.ZBattlefield)
			ensureInHand(t, e, active, tc.name)
			// Let the creature reach its controller's next turn rather than
			// editing SummonSick directly; the turn-change event clears it.
			driveToAttackersAt(t, e, e.G.Turn+2, active)
			attackDecision := e.Pending()
			attackChoice := -1
			for _, option := range attackDecision.Options {
				if option.Obj == attacker && option.Player == defender {
					attackChoice = option.Index
					break
				}
			}
			if attackChoice < 0 {
				t.Fatalf("precondition: Grizzly Bears has no attack option against seat %d: %+v", defender, attackDecision.Options)
			}
			submitChoices(t, e, attackChoice)
			for _, symbol := range tc.mana {
				e.emit(events.Event{Kind: events.ManaAdd, Player: active, Counter: string(symbol), Amount: 1})
			}
			// ManaAdd does not itself regenerate the pending priority options.
			// Re-open the priority round so castOptions sees the funded pool.
			e.pending = nil
			e.priorityRound()

			spellInHand := false
			for _, id := range e.G.Zone(state.ZHand, active) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == tc.name {
					spellInHand = true
					break
				}
			}
			if !spellInHand {
				t.Fatalf("precondition: %s is not in player %d's hand", tc.name, active)
			}

			// The target selector depends on all three facts: the exact Bear is
			// a battlefield creature, tapped, and attacking in this combat step.
			o := e.G.Obj(attacker)
			if o == nil || o.Zone != state.ZBattlefield || !o.Tapped || !o.IsAttacking || o.Attacking != defender || e.G.Step != state.StepDeclareAttackers {
				t.Fatalf("precondition: attacker=%+v step=%s, want tapped attacking battlefield Bear during declare-attackers", o, e.G.Step)
			}
			if tc.wantPT != "" {
				before := e.Derived(attacker)
				beforePT := strconv.Itoa(int(before.Power)) + "/" + strconv.Itoa(int(before.Toughness))
				if beforePT == tc.wantPT {
					t.Fatalf("precondition: Dreadmaw's Ire starts at %s, so the expected %s pump would not distinguish a change", beforePT, tc.wantPT)
				}
			}
			cast := false
			for i := 0; i < 32 && !cast; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no pending decision during declare-attackers")
				}
				if d.Kind != decision.KPriority {
					choices := make([]int, 0, d.Min)
					for j := 0; j < d.Min && j < len(d.Options); j++ {
						choices = append(choices, d.Options[j].Index)
					}
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
						t.Fatalf("answer %s during combat: %v", d.Kind, err)
					}
					continue
				}
				canCast := false
				for _, option := range d.Options {
					if d.Player == active && option.Kind == "cast" && option.Label == "Cast "+tc.name {
						canCast = true
						break
					}
				}
				if canCast {
					castCardNow(t, e, tc.name)
					cast = true
					break
				}
				submitChoices(t, e, passIndex(t, d))
			}
			if !cast {
				t.Fatalf("never got a cast option for %s during declare-attackers", tc.name)
			}
			d := passToTargetAsk(t, e)
			target := -1
			for _, option := range d.Options {
				if option.Obj == attacker {
					target = option.Index
					break
				}
			}
			if target < 0 {
				t.Fatalf("%s did not offer the tapped attacker %d: %+v", tc.name, attacker, d.Options)
			}
			submitChoices(t, e, target)
			passUntilStackEmpty(t, e, 40)
			if e.G.Step != state.StepDeclareAttackers {
				t.Fatalf("after resolution step = %s, want declare-attackers", e.G.Step)
			}
			o = e.G.Obj(attacker)
			if tc.wantZone == state.ZGraveyard {
				if o == nil || o.Zone != tc.wantZone {
					t.Fatalf("after Cosmium Blast attacker = %+v, want Grizzly Bears in graveyard", o)
				}
				return
			}
			if o == nil || o.Zone != state.ZBattlefield || !o.Tapped || !o.IsAttacking || o.Attacking != defender {
				t.Fatalf("after Dreadmaw's Ire attacker = %+v, want tapped and attacking", o)
			}
			derived := e.Derived(attacker)
			if got := strconv.Itoa(int(derived.Power)) + "/" + strconv.Itoa(int(derived.Toughness)); got != tc.wantPT {
				t.Fatalf("after Dreadmaw's Ire attacker P/T = %s, want %s", got, tc.wantPT)
			}
		})
	}
}
