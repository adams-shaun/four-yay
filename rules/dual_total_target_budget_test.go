package rules

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestDualTotalTargetCapsBothEnforced(t *testing.T) {
	for _, dual := range []bool{true, false} {
		t.Run(map[bool]string{true: "dual", false: "cmc-only"}[dual], func(t *testing.T) {
			script := "Name:DualReclaimer\nManaCost:2\nTypes:Creature\n" +
				"A:AB$ ChangeZone | Cost$ 0 | Origin$ Graveyard | Destination$ Hand | TargetMin$ 0 | TargetMax$ 3 | ValidTgts$ Creature.YouOwn | MaxTotalTargetCMC$ 5"
			if dual {
				script += " | MaxTotalTargetPower$ 5"
			}
			script += "\nOracle:x\n"
			a := "Name:CheapStrong\nManaCost:1\nTypes:Creature\nPT:4/4\nOracle:x\n"
			b := "Name:DearWeak\nManaCost:4\nTypes:Creature\nPT:1/1\nOracle:x\n"
			c := "Name:Medium\nManaCost:2\nTypes:Creature\nPT:2/2\nOracle:x\n"
			e, _, src := newFixtureDeck(t, 9801, script, a, b, c)
			toMain1(t, e)
			ids := map[string]state.ObjID{}
			for _, name := range []string{"CheapStrong", "DearWeak", "Medium"} {
				ids[name] = moveNamedToGrave(t, e, name)
				o := e.G.Obj(ids[name])
				if o == nil || o.Zone != state.ZGraveyard {
					t.Fatalf("%s not in graveyard: %+v", name, o)
				}
			}
			if o := e.G.Obj(src); o == nil || o.Zone != state.ZBattlefield {
				e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: o.Zone, To: state.ZBattlefield})
			}
			e.pending = nil
			e.priorityRound()
			submitChoices(t, e, abilityOption(t, e, src, 0).Index)
			d := e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("pending = %+v, want KTarget", d)
			}
			if !d.Budgeted || d.MaxSum != 5 || d.Budgeted2 != dual || (dual && d.MaxSum2 != 5) || (!dual && d.MaxSum2 != 0) {
				t.Fatalf("budgets = %d/%v %d/%v, dual=%v", d.MaxSum, d.Budgeted, d.MaxSum2, d.Budgeted2, dual)
			}
			ix := map[string]int{}
			for _, o := range d.Options {
				for name, id := range ids {
					if o.Obj == id {
						ix[name] = o.Index
						power, cmc := int(e.Power(id)), int(e.G.Obj(id).Face().ManaValue())
						if power == cmc {
							if name != "Medium" {
								t.Fatalf("%s unexpectedly equal prices", name)
							}
						}
						want1, want2 := cmc, 0
						if dual {
							want1, want2 = power, cmc
						}
						if o.Value != want1 || o.Value2 != want2 {
							t.Fatalf("%s prices %d/%d want %d/%d", name, o.Value, o.Value2, want1, want2)
						}
					}
				}
			}
			if len(ix) != 3 {
				t.Fatalf("missing offered targets: %v", ix)
			}
			check := func(names []string, valid bool) {
				picks := []int{ix[names[0]], ix[names[1]]}
				err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks})
				if (err == nil) != valid {
					t.Fatalf("%v valid=%v error=%v", names, valid, err)
				}
			}
			if dual {
				check([]string{"CheapStrong", "Medium"}, false)  // power 6, CMC 3
				check([]string{"DearWeak", "Medium"}, false)     // power 3, CMC 6
				check([]string{"CheapStrong", "DearWeak"}, true) // both 5
			} else {
				check([]string{"CheapStrong", "Medium"}, true)
				raw, _ := json.Marshal(d)
				if strings.Contains(string(raw), "maxSum2") || strings.Contains(string(raw), "budgeted2") || strings.Contains(string(raw), "value2") {
					t.Fatalf("single-cap decision changed wire: %s", raw)
				}
			}
		})
	}
}
