// Focused tests for the opponent-turn spell-cast causes this ticket adds: a
// SpellCast trigger with ValidActivatingPlayer$ You and OpponentTurn$ True
// fires only on p0's own cast during p1's turn, so the cause passes to p1's
// main phase, hands priority over, and casts an instant there.
package templates

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
)

func TestTriggerOpponentTurnCast(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, key string }{
		{"Nightmare Sower", "trigger#0.0"},
		{"Unwelcome Sprite", "trigger#0.0"},
		{"Voracious Tome-Skimmer", "trigger#0.0"},
		{"Dream Spoilers", "trigger#0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s not in the corpus", tc.name)
			}
			f := card.Faces[0]
			var req levelb.Requirement
			found := false
			for _, r := range levelb.Requirements(card) {
				if r.Key == tc.key {
					req = r
					found = true
				}
			}
			if !found {
				t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
			}
			// Precondition: the gate really is "you cast during an opponent's
			// turn", or the cause under test would serve nothing.
			idx, err := strconv.Atoi(req.Slot)
			if err != nil || idx < 0 || idx >= len(f.Triggers) {
				t.Fatalf("precondition: slot %q does not name a trigger", req.Slot)
			}
			trig := &f.Triggers[idx]
			if trig.ParamStr(cards.PKOpponentTurn) != "True" {
				t.Fatalf("precondition: %s trigger OpponentTurn$ = %q, want True", tc.name, trig.ParamStr(cards.PKOpponentTurn))
			}
			it := triggerItem(t, reg, tc.name, tc.key)
			// The cause ran on p1's turn: a pass_to to p1's main phase and a
			// p0 cast after it.
			toP1, cast := false, false
			for _, st := range it.Scenario.Steps {
				if st.Op == "pass_to" && st.Active == "p1" {
					toP1 = true
				}
				if st.Op == "cast" && st.Seat == 0 && st.Card != "p0:"+tc.name {
					cast = true
				}
			}
			if !toP1 || !cast {
				t.Fatalf("%s: steps %v, want a pass_to p1 and a p0 cast", tc.name, it.Scenario.Steps)
			}
		})
	}
}
