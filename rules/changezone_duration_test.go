package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// ChangeZone's Duration$ UntilHostLeavesPlay is the Oblivion Ring /
// Banisher Priest pattern: the ETB trigger exiles a permanent and the exiled
// card comes back when the exiling permanent leaves the battlefield. The
// engine encodes it as an event-backed ExileReturn association on the exiling
// permanent (recorded by effects.recordExileReturn) and a rules sweep
// (Engine.sweepExileReturn) that fires on the exiler's battlefield departure.
// Fixtures are inline (the licensing rule); the scripts are the corpus cards'
// own SVar shapes.
// passToTargetAsk passes priority (either seat, always the pass option)
// until a target decision appears -- the shape an ETB trigger's placement
// target ask takes after the cast resolves.
func passToTargetAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 16; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision")
		}
		if d.Kind == decision.KTarget {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("expected priority or a target ask, got %+v", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	t.Fatal("no target ask appeared within the pass budget")
	return nil
}

func TestChangeZoneDurationUntilHostLeavesPlay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		orer    string
		targets string
	}{
		{"banishing_light", "Banishing Light", "Permanent.nonLand+OppCtrl"},
		{"banisher_priest", "Banisher Priest", "Creature.OppCtrl"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orering := card(t, "Name:"+tc.orer+"\nManaCost:2 W\nTypes:Enchantment\n"+
				"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigExile | TriggerDescription$ When CARDNAME enters, exile target nonland permanent an opponent controls until CARDNAME leaves the battlefield.\n"+
				"SVar:TrigExile:DB$ ChangeZone | Origin$ Battlefield | Destination$ Exile | ValidTgts$ "+tc.targets+" | TgtPrompt$ Select target | Duration$ UntilHostLeavesPlay\n"+
				"Oracle:x\n")
			removal := card(t, "Name:Vindicate\nManaCost:1 B W\nTypes:Instant\n"+
				"A:SP$ Destroy | ValidTgts$ Permanent | TgtPrompt$ Select target permanent\nOracle:x\n")
			e := handEngine(t, orering, removal)
			bear := onBoard(t, e, 1, "Name:Runeclaw Bear\nManaCost:1 G U\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

			// Cast the exiler; its ETB trigger poses the exile target ask.
			e.G.Players[0].Pool[state.MW] = 1
			e.G.Players[0].Pool[state.MC] = 2
			e.askPriority(0)
			castFirst(t, e, "cast")
			d := passToTargetAsk(t, e)
			idx := -1
			for _, o := range d.Options {
				if o.Obj == bear {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("opponent's bear not offered as exile target: %+v", d.Options)
			}
			submitChoices(t, e, idx)
			passUntilStackEmpty(t, e, 8)
			if got := e.G.Obj(bear).Zone; got != state.ZExile {
				t.Fatalf("bear should be exiled, zone=%v", got)
			}
			light := -1
			for _, o := range e.G.Zone(state.ZBattlefield, 0) {
				light = int(o)
			}
			if light < 0 || len(e.G.Obj(state.ObjID(light)).ExileReturn) != 1 ||
				e.G.Obj(state.ObjID(light)).ExileReturn[0].Obj != bear ||
				e.G.Obj(state.ObjID(light)).ExileReturn[0].From != state.ZBattlefield {
				t.Fatalf("expected one ExileReturn entry naming the bear from the battlefield, got %+v",
					e.G.Obj(state.ObjID(light)).ExileReturn)
			}

			// Destroy the exiler: the sweep returns the bear to the
			// battlefield under its owner's control.
			e.G.Players[0].Pool[state.MB] = 1
			e.G.Players[0].Pool[state.MW] = 1
			e.G.Players[0].Pool[state.MC] = 1
			e.askPriority(0)
			castFirst(t, e, "cast")
			d = e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("expected Vindicate's target ask, got %+v", d)
			}
			idx = -1
			for _, o := range d.Options {
				if o.Obj == state.ObjID(light) {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("exiler not offered as Vindicate target: %+v", d.Options)
			}
			submitChoices(t, e, idx)
			passUntilStackEmpty(t, e, 8)
			if got := e.G.Obj(state.ObjID(light)).Zone; got != state.ZGraveyard {
				t.Fatalf("exiler should be destroyed, zone=%v", got)
			}
			if got := e.G.Obj(bear); got.Zone != state.ZBattlefield {
				t.Fatalf("bear should have returned to the battlefield when the exiler left, zone=%v", got.Zone)
			} else if got.Controller != 1 {
				t.Fatalf("returned bear must be under its owner's control, controller=%v", got.Controller)
			}
			if n := len(e.G.Obj(state.ObjID(light)).ExileReturn); n != 0 {
				t.Fatalf("sweep must prune the entries it returned, %d left", n)
			}
		})
	}
}

// A token exiled under Duration$ has ceased to exist (CR 111.7) and must not
// come back: the record step skips it, so the sweep has nothing to return.
func TestChangeZoneDurationDoesNotReturnAToken(t *testing.T) {
	t.Parallel()
	light := card(t, "Name:Banishing Light\nManaCost:2 W\nTypes:Enchantment\n"+
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigExile | TriggerDescription$ When CARDNAME enters, exile target nonland permanent an opponent controls until CARDNAME leaves the battlefield.\n"+
		"SVar:TrigExile:DB$ ChangeZone | Origin$ Battlefield | Destination$ Exile | ValidTgts$ Permanent.nonLand+OppCtrl | Duration$ UntilHostLeavesPlay\n"+
		"Oracle:x\n")
	removal := card(t, "Name:Vindicate\nManaCost:1 B W\nTypes:Instant\n"+
		"A:SP$ Destroy | ValidTgts$ Permanent | TgtPrompt$ Select target permanent\nOracle:x\n")
	e := handEngine(t, light, removal)
	token := onBoard(t, e, 1, "Name:Goblin\nManaCost:R\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n")
	e.G.Obj(token).IsToken = true

	e.G.Players[0].Pool[state.MW] = 1
	e.G.Players[0].Pool[state.MC] = 2
	e.askPriority(0)
	castFirst(t, e, "cast")
	d := passToTargetAsk(t, e)
	idx := -1
	for _, o := range d.Options {
		if o.Obj == token {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("token not offered as exile target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)
	exiler := e.G.Zone(state.ZBattlefield, 0)[0]
	if n := len(e.G.Obj(exiler).ExileReturn); n != 0 {
		t.Fatalf("a ceased token must not be recorded for return, got %+v", e.G.Obj(exiler).ExileReturn)
	}

	e.G.Players[0].Pool[state.MB] = 1
	e.G.Players[0].Pool[state.MW] = 1
	e.G.Players[0].Pool[state.MC] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Vindicate's target ask, got %+v", d)
	}
	idx = -1
	for _, o := range d.Options {
		if o.Obj == exiler {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("exiler not offered as Vindicate target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 8)
	// CR 111.7: a token that left the battlefield has ceased to exist. This
	// build parks such objects in exile, so either tombstone state is fine;
	// what must never happen is a battlefield return.
	if got := e.G.Obj(token).Zone; got != state.ZExile && got != state.ZCeased {
		t.Fatalf("a ceased token must stay dead, zone=%v", got)
	}
}
