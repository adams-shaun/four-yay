package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// TestRoomProbeScenariosShowTheirEffect pins the fully-unlock probe
// scenarios of the DSK Eerie rows (ticket cli-20261009T031407Z-31a90cb3):
// each item generates and its gorge replay shows, in the compared state,
// the effect the scenario exists to check. The probe (roomUnlockProbe, cast
// then paid-unlocked) is the trigger's cause, so every case first asserts
// the probe Room really is on the battlefield unlocked -- the trigger's
// cause is where the rule looks -- before asserting the effect.
func TestRoomProbeScenariosShowTheirEffect(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	probeName := frontHalfOf(roomUnlockProbe)
	cases := []struct {
		card, key string
		check     func(t *testing.T, reg *cards.Registry, res rules.OracleResult, item oraclegen.Item)
	}{
		{"Balemurk Leech", "trigger#0.1", balemurkLosesLife},
		{"Gremlin Tamer", "trigger#0.1", gremlinTamerTokens},
		{"Victor, Valgavoth's Seneschal", "trigger#0.1", nil}, // trigger on the stack, below
	}
	for _, tc := range cases {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			req := requirement(t, tc.card, tc.key)
			if req.Sub != levelb.FullyUnlockSub {
				t.Fatalf("precondition: %s %s is sub %q, want the fully-unlock probe shape", tc.card, tc.key, req.Sub)
			}
			item, skip := GenerateB(reg, tc.card, req)
			if skip != nil {
				t.Fatalf("precondition: %s does not generate: %s", tc.card, skip.Reason)
			}
			unlocked := false
			for _, st := range item.Steps {
				unlocked = unlocked || (st.Op == "activate" && st.Ability != "" && st.Card == "p0:"+roomUnlockProbe)
			}
			if !unlocked {
				t.Fatalf("precondition: no paid unlock of the probe in %+v", item.Steps)
			}
			res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
			if err != nil || len(res.Fails) != 0 {
				t.Fatalf("replay err=%v fails=%v", err, res.Fails)
			}
			// The probe Room is on p0's battlefield by the last checkpoint:
			// the cause the Eerie trigger watches really happened.
			final := res.Snapshots[len(res.Snapshots)-1]
			onField := false
			for _, p := range final.Permanents {
				if p.Controller == 0 && !p.Token && p.Name == probeName {
					onField = true
				}
			}
			if !onField {
				t.Fatalf("precondition: the unlocked probe Room %q is not on p0's battlefield at the end", probeName)
			}
			if tc.check != nil {
				tc.check(t, reg, res, item)
				return
			}
			// Victor's surveil default (a target skip) leaves no state to
			// read, so show the trigger itself: strip the trailing resolves,
			// pass once, and find the requirement's trigger on the stack.
			probe := item.Scenario
			probe.Steps = append([]oraclegen.Step(nil), item.Steps...)
			for n := len(probe.Steps); n > 0 && probe.Steps[n-1].Op == "resolve"; n-- {
				probe.Steps = probe.Steps[:n-1]
			}
			probe.Steps = append(probe.Steps, oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1})
			_, probed, ok := oraclegen.Settle(reg, probe)
			if !ok {
				t.Fatalf("the probe scenario does not play through")
			}
			card, _ := reg.Lookup(tc.card)
			if !abilityOnStack(probed.Snapshots, stackSourceWants(reg, tc.card, card.Faces[req.Face]), stackSlot(req)) {
				t.Fatalf("trigger %s never reached the stack", req.Key)
			}
		})
	}
}

func balemurkLosesLife(t *testing.T, reg *cards.Registry, res rules.OracleResult, item oraclegen.Item) {
	t.Helper()
	setup := res.Snapshots[0]
	if setup.Players[1].Life != 20 {
		t.Fatalf("precondition: p1 starts at %d life, not 20", setup.Players[1].Life)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if got := final.Players[1].Life; got != 18 {
		t.Fatalf("p1 life at the end = %d, want 18 (the probe's ETB and its full unlock each lose 1)", got)
	}
}

func gremlinTamerTokens(t *testing.T, reg *cards.Registry, res rules.OracleResult, item oraclegen.Item) {
	t.Helper()
	final := res.Snapshots[len(res.Snapshots)-1]
	tokens := 0
	for _, p := range final.Permanents {
		if p.Token && p.Controller == 0 && strings.Contains(p.Name, "Gremlin") {
			tokens++
		}
	}
	if tokens != 2 {
		t.Fatalf("Gremlin tokens at the end = %d, want 2 (the probe's ETB and its full unlock each make one); permanents %+v", tokens, final.Permanents)
	}
}

func frontHalfOf(n string) string {
	if i := strings.Index(n, " // "); i > 0 {
		return n[:i]
	}
	return n
}
