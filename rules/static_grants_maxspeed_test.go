package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// setSpeed brings player 0's speed to n through the event fold (additive).
func setSpeed(t *testing.T, e *Engine, n int32) {
	t.Helper()
	e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: n - e.G.Players[0].Speed})
	if got := e.G.Players[0].Speed; got != n {
		t.Fatalf("precondition: speed = %d, want %d", got, n)
	}
}

// TestMaxSpeedNestedAddStaticAbilityGrantsInner pins the nested
// `Condition$ MaxSpeed | AddStaticAbility$ <inner>` shape (Burnout
// Bashtronaut, Gastal Raider, Streaking Oilgorger): the shared continuous
// gate must hold at the controller's max speed (CR 702.179e) so the layer
// walk reaches the nested Mode$ Continuous body, and must not at speed 3.
func TestMaxSpeedNestedAddStaticAbilityGrantsInner(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cases := []struct {
		name    string
		keyword string
	}{
		{name: "Burnout Bashtronaut", keyword: "Double Strike"},
		{name: "Gastal Raider", keyword: "Menace"},
		{name: "Streaking Oilgorger", keyword: "Lifelink"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, c.name)}, nil)
			id := moveByName(t, e, 0, c.name, state.ZBattlefield)
			if e.G.Obj(id).Zone != state.ZBattlefield {
				t.Fatalf("precondition: %s is not on the battlefield", c.name)
			}
			setSpeed(t, e, 3)
			p3, t3 := e.Power(id), e.Toughness(id)
			if e.HasKeyword(id, c.keyword) {
				t.Fatalf("%s has %s at speed 3, want it absent", c.name, c.keyword)
			}
			setSpeed(t, e, maxSpeed)
			if !e.HasKeyword(id, c.keyword) {
				t.Fatalf("%s lacks %s at speed 4: the nested AddStaticAbility$ was never applied", c.name, c.keyword)
			}
			if c.name == "Gastal Raider" {
				if e.Power(id) != p3+1 || e.Toughness(id) != t3+1 {
					t.Fatalf("Gastal Raider speed 4 = %d/%d, want +1/+1 over speed 3's %d/%d", e.Power(id), e.Toughness(id), p3, t3)
				}
			}
			// The grant follows the speed back down (the gate is re-read, not latched).
			setSpeed(t, e, 3)
			if e.HasKeyword(id, c.keyword) {
				t.Fatalf("%s keeps %s after speed drops below 4", c.name, c.keyword)
			}
		})
	}
}

// TestMaxSpeedAddAbilityOfferedExactlyOnce: with the continuous gate now
// holding for MaxSpeed, a "Max speed --" AddAbility$ must still be offered
// only by speed.go's maxSpeedAbilities (kind granted), never also as a layer
// grant (kind ability). Amonkhet Raceway's body is a non-mana pump.
func TestMaxSpeedAddAbilityOfferedExactlyOnce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := card(t, "Name:BigBear\nManaCost:2 G\nTypes:Creature Bear\nPT:3/3\nOracle:x\n")
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "Amonkhet Raceway"), bear}, nil)
	rw := moveByName(t, e, 0, "Amonkhet Raceway", state.ZBattlefield)
	bearID := moveByName(t, e, 0, "BigBear", state.ZBattlefield)
	if e.G.Obj(rw).Zone != state.ZBattlefield || e.G.Obj(bearID).Zone != state.ZBattlefield {
		t.Fatal("precondition: Raceway and a creature must be on the battlefield")
	}
	count := func() (granted, ability int) {
		e.priorityRound()
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("no priority decision (got %+v)", d)
		}
		for _, o := range d.Options {
			if o.Obj != rw || o.SVar != "ABPump" {
				continue
			}
			switch o.Kind {
			case "granted":
				granted++
			case "ability":
				ability++
			}
		}
		return
	}
	setSpeed(t, e, 3)
	if g, a := count(); g != 0 || a != 0 {
		t.Fatalf("speed 3: ABPump offered granted=%d ability=%d, want none", g, a)
	}
	setSpeed(t, e, maxSpeed)
	if g, a := count(); g != 1 || a != 0 {
		t.Fatalf("speed 4: ABPump offered granted=%d ability=%d, want exactly one granted", g, a)
	}
}
