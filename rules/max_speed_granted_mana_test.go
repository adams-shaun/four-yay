package rules

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// maxSpeedManaBoard places one real MaxSpeed mana-grant card on seat 0's
// battlefield at the given speed and asserts every precondition the gate
// assertions depend on: the card is an untapped, non-summoning-sick
// battlefield permanent and the seat's ACTUAL speed is the requested one.
func maxSpeedManaBoard(t *testing.T, name string, speed int32, hand ...string) (*oracleRun, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	fails, _, r := runOracleScenario(reg, oracleScenario{Setup: map[string]oracleSeat{
		"p0": {Battlefield: []string{name, "Island"}, Hand: hand, Speed: speed},
	}})
	if len(fails) != 0 {
		t.Fatalf("scenario failed: %v", fails)
	}
	id, err := r.resolve("p0:" + name)
	if err != nil {
		t.Fatal(err)
	}
	o := r.e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Tapped || o.SummonSick {
		t.Fatalf("precondition: %s is not an untapped, non-sick battlefield permanent: %+v", name, o)
	}
	if got := r.e.G.Players[0].Speed; got != speed {
		t.Fatalf("precondition: speed = %d, want %d", got, speed)
	}
	return r, id
}

// maxSpeedManaMembers returns the ONE mana membership of id after asserting
// the pass-scoped snapshot walk, the fresh walk and the ignorePayable walk
// (the cast-window probe's, which skips payability and never a grant's own
// existence condition) agree on it, order included.
func maxSpeedManaMembers(t *testing.T, e *Engine, id state.ObjID) []*cards.SA {
	t.Helper()
	snapshot := e.availableManaAbilitiesUsing(&actionStaticSource{e: e}, 0, id)
	fresh := e.availableManaAbilities(0, id)
	probe := e.appendAvailableManaAbilitiesGate(nil, nil, 0, id, true)
	lines := func(l []*cards.SA) string {
		var b strings.Builder
		for _, sa := range l {
			b.WriteString(sa.Line + "\n")
		}
		return b.String()
	}
	if lines(snapshot) != lines(fresh) || lines(fresh) != lines(probe) {
		t.Fatalf("membership disagrees at speed %d:\nsnapshot:\n%sfresh:\n%sprobe:\n%s",
			e.G.Players[0].Speed, lines(snapshot), lines(fresh), lines(probe))
	}
	return fresh
}

func grantedCCCount(labels []string, want string) int {
	n := 0
	for _, l := range labels {
		if strings.EqualFold(l, want) {
			n++
		}
	}
	return n
}

// TestMaxSpeedGrantedManaGate pins CR 702.179e on the three real corpus
// cards whose "Max speed -- {T}: Add ..." is a Continuous AddAbility$ mana
// grant: below max speed the grant does not exist -- not as a collector
// member, not as a label, not through the generic priority "activate" option
// -- and at speed 4 it is exactly one member.
func TestMaxSpeedGrantedManaGate(t *testing.T) {
	cases := []struct {
		card string
		// base is the count of ordinary (unconditional) mana abilities.
		base int
	}{
		{"Muraganda Raceway", 1},
		{"Endrider Catalyzer", 0},
		{"Howlsquad Heavy", 0},
	}
	for _, tc := range cases {
		t.Run(tc.card, func(t *testing.T) {
			var below, at int
			for _, speed := range []int32{0, 3, 4} {
				r, id := maxSpeedManaBoard(t, tc.card, speed)
				members := maxSpeedManaMembers(t, r.e, id)
				if speed < maxSpeed {
					below = len(members)
				} else {
					at = len(members)
				}
			}
			if below != tc.base {
				t.Errorf("%s below max speed has %d mana members, want %d (the ordinary ability only)", tc.card, below, tc.base)
			}
			if at != tc.base+1 {
				t.Errorf("%s at max speed has %d mana members, want %d (ordinary + the one grant)", tc.card, at, tc.base+1)
			}
			if at == below {
				t.Fatalf("vacuous: membership did not differ across speed (%d vs %d)", below, at)
			}
		})
	}

	t.Run("Muraganda Raceway activation", func(t *testing.T) {
		for _, speed := range []int32{0, 3} {
			r, id := maxSpeedManaBoard(t, "Muraganda Raceway", speed)
			labels := r.manaAbilityLabels(0, id)
			if grantedCCCount(labels, "Add C") != 1 {
				t.Fatalf("precondition: unconditional Add C missing at speed %d: %v", speed, labels)
			}
			if grantedCCCount(labels, "Add CC") != 0 {
				t.Fatalf("speed %d offers the max-speed Add CC: %v", speed, labels)
			}
			// The generic option is a singleton: it adds exactly C.
			submitChoices(t, r.e, activateOption(t, r.e, id))
			if d := r.e.Pending(); d == nil || d.Kind != decision.KPriority {
				t.Fatalf("speed %d: Raceway activation asked an extra decision: %+v", speed, d)
			}
			if pool := poolString(r.e.G.Players[0].Pool); pool != "C" {
				t.Fatalf("speed %d: pool %q, want C", speed, pool)
			}
			if !r.e.G.Obj(id).Tapped {
				t.Fatalf("speed %d: Raceway not tapped", speed)
			}
			// Naming the gated-out ability fails loudly through the runner.
			r2, _ := maxSpeedManaBoard(t, "Muraganda Raceway", speed)
			reg := testutil.CorpusRegistry(t)
			fails, _, _ := runOracleScenario(reg, oracleScenario{
				Setup: map[string]oracleSeat{"p0": {Battlefield: []string{"Muraganda Raceway"}, Speed: speed}},
				Steps: []oracleStep{{Op: "activate", Seat: 0, Card: "p0:Muraganda Raceway", Ability: "Add {C}{C}"}},
			})
			if len(fails) == 0 {
				t.Fatalf("speed %d: named Add CC activation succeeded below max speed (pool %q)", speed, poolString(r2.e.G.Players[0].Pool))
			}
		}

		r, id := maxSpeedManaBoard(t, "Muraganda Raceway", 4)
		labels := r.manaAbilityLabels(0, id)
		if grantedCCCount(labels, "Add C") != 1 || grantedCCCount(labels, "Add CC") != 1 {
			t.Fatalf("speed 4 labels = %v, want exactly one Add C and one Add CC", labels)
		}
		submitChoices(t, r.e, activateOption(t, r.e, id))
		d := r.e.Pending()
		cc := manaOption(t, d, "CC")
		if r.e.G.Obj(id).Tapped {
			t.Fatal("Raceway tapped before the ability choice")
		}
		submitChoices(t, r.e, cc)
		if pool := poolString(r.e.G.Players[0].Pool); pool != "CC" {
			t.Fatalf("speed 4: pool %q, want CC", pool)
		}
		if got := manaEventsFor(r.e, events.Tap, id); got != 1 || !r.e.G.Obj(id).Tapped {
			t.Fatalf("speed 4: Tap events = %d tapped=%t, want one tap", got, r.e.G.Obj(id).Tapped)
		}
	})

	// A speed change is an event: the snapshot, fresh and ignorePayable walks
	// must follow it together, in both directions, with no state poke.
	t.Run("event-backed speed change", func(t *testing.T) {
		r, id := maxSpeedManaBoard(t, "Muraganda Raceway", 3)
		if n := len(maxSpeedManaMembers(t, r.e, id)); n != 1 {
			t.Fatalf("speed 3 members = %d, want 1", n)
		}
		r.e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: 1})
		if got := r.e.G.Players[0].Speed; got != maxSpeed {
			t.Fatalf("precondition: speed after SpeedChange = %d, want %d", got, maxSpeed)
		}
		if n := len(maxSpeedManaMembers(t, r.e, id)); n != 2 {
			t.Fatalf("speed 4 members = %d, want 2", n)
		}
	})
}

// TestMaxSpeedGrantedManaPaymentWindow pins the CR 601.2g window: paying
// {2} with a Raceway below max speed offers no Add CC choice, and at max
// speed the one Raceway tap pays the whole cost by choosing Add CC.
func TestMaxSpeedGrantedManaPaymentWindow(t *testing.T) {
	castMindStone := func(t *testing.T, speed int32) (*oracleRun, state.ObjID, *decision.Decision) {
		t.Helper()
		r, raceway := maxSpeedManaBoard(t, "Muraganda Raceway", speed, "Mind Stone")
		stone, err := r.resolve("p0:Mind Stone")
		if err != nil {
			t.Fatal(err)
		}
		if o := r.e.G.Obj(stone); o == nil || o.Zone != state.ZHand {
			t.Fatalf("precondition: Mind Stone not in hand: %+v", o)
		}
		// The payment window is entered directly (as TestCastManaWindowChoosesOneAbility
		// does): the priority offer does not list a cast the pool cannot yet fund.
		r.e.pending = nil
		r.e.beginCast(0, decision.Option{Kind: "cast", Obj: stone})
		r.e.Advance()
		d := r.e.Pending()
		if d == nil {
			t.Fatal("no payment window decision")
		}
		return r, raceway, d
	}
	windowActivate := func(t *testing.T, d *decision.Decision, id state.ObjID) int {
		t.Helper()
		for _, o := range d.Options {
			if o.Kind == "activate" && o.Obj == id {
				return o.Index
			}
		}
		t.Fatalf("no activate option for the Raceway in the window: %s", optionDump(d))
		return -1
	}

	t.Run("below max speed", func(t *testing.T) {
		for _, speed := range []int32{0, 3} {
			r, raceway, d := castMindStone(t, speed)
			submitChoices(t, r.e, windowActivate(t, d, raceway))
			// The Raceway is a singleton here: no ability choice, one C.
			if nd := r.e.Pending(); nd != nil && nd.Kind == decision.KChoose {
				for _, o := range nd.Options {
					if strings.Contains(o.Label, "CC") {
						t.Fatalf("speed %d: window offers Add CC: %s", speed, optionDump(nd))
					}
				}
			}
			if got := manaEventsFor(r.e, events.Tap, raceway); got != 1 {
				t.Fatalf("speed %d: Raceway Tap events = %d, want 1", speed, got)
			}
			if pool := poolString(r.e.G.Players[0].Pool); pool != "C" {
				t.Fatalf("speed %d: pool %q, want exactly C from the Raceway", speed, pool)
			}
		}
	})

	t.Run("at max speed", func(t *testing.T) {
		r, raceway, d := castMindStone(t, 4)
		submitChoices(t, r.e, windowActivate(t, d, raceway))
		nd := r.e.Pending()
		cc := manaOption(t, nd, "CC")
		submitChoices(t, r.e, cc)
		if got := manaEventsFor(r.e, events.Tap, raceway); got != 1 {
			t.Fatalf("Raceway Tap events = %d, want 1", got)
		}
		if len(r.e.G.Stack) != 1 || r.e.G.Players[0].Pool.Total() != 0 {
			t.Fatalf("stack=%v pool=%s, want Mind Stone paid by one CC tap", r.e.G.Stack, fmt.Sprint(poolString(r.e.G.Players[0].Pool)))
		}
	})
}
