package rules

// Restores effects/putcounter_optional_test.go,
// effects/putcounter_exile_test.go and effects/putcounter_rememberput_test.go
// on the kernel: an Optional$ PutCounter poses a put_optional yes/no to the
// controller (option 0 = yes) and places nothing until it is answered; "no"
// places nothing, "yes" places exactly the stated counters -- on an object
// (Talus Paladin's real DBCounter), on an EXILED remembered object, and on a
// player recipient, whose RememberPut$ makes the player visible to the
// chained sub.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestTalusPaladinOptionalCounterAskAndAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		accept bool
	}{{"decline", false}, {"accept", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := kr2Engine(t, 2)
			paladin := kr2Put(t, e, 0, kr2Corpus(t, "Talus Paladin"), state.ZHand, false)
			d := kr2Cast(t, e, 0, paladin)
			// The lifelink grant's own Optional$ comes first: decline it.
			for i := 0; d != nil && d.ResumeKind != "put_optional" && i < 4; i++ {
				idx := d.Options[len(d.Options)-1].Index
				for _, o := range d.Options {
					if o.Kind == "no" {
						idx = o.Index
					}
				}
				if d.Min == 0 && d.Kind == decision.KModes {
					d = kr2Answer(t, e, d)
					continue
				}
				d = kr2Answer(t, e, d, idx)
			}
			d = kr2Want(t, d, "put_optional")
			if d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || d.Player != 0 {
				t.Fatalf("election = %+v, want a 1-of KChoose for the controller", d)
			}
			if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
				t.Fatalf("options = %+v, want yes then no", d.Options)
			}
			if got := e.G.Obj(paladin).Counter("P1P1"); got != 0 {
				t.Fatalf("placed %d counter(s) before the election was answered", got)
			}
			from := len(e.L.Events)
			choice := 1
			if tc.accept {
				choice = 0
			}
			if d = kr2Answer(t, e, d, choice); d != nil {
				t.Fatalf("unexpected ask after the election: %+v", d)
			}
			want := int32(0)
			if tc.accept {
				want = 1
			}
			if got := e.G.Obj(paladin).Counter("P1P1"); got != want {
				t.Fatalf("Talus Paladin P1P1 = %d, want %d", got, want)
			}
			n := 0
			for _, ev := range kr2Events(e, from, events.CounterChange) {
				if ev.Obj == paladin {
					n++
					if ev.Counter != "P1P1" || ev.Amount != 1 {
						t.Fatalf("counter event = %+v, want one P1P1 of amount 1", ev)
					}
				}
			}
			if n != int(want) {
				t.Fatalf("%d CounterChange events on the paladin, want %d", n, want)
			}
		})
	}
}

func TestPutCounterOptionalAsksOverExiledObject(t *testing.T) {
	t.Parallel()
	e := kr2Engine(t, 2)
	victim := kr2Put(t, e, 1, kr2Src(t, "Name:Victim\n"+kr2CreatureSrc), state.ZBattlefield, false)
	spell := kr2Put(t, e, 0, kr2Sorcery(t, "Suspender",
		"A:SP$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Exile | RememberChanged$ True | SubAbility$ DBPut",
		"SVar:DBPut:DB$ PutCounter | Defined$ Remembered | CounterType$ TIME | CounterNum$ 3 | Optional$ True"), state.ZHand, false)
	d := kr2Cast(t, e, 0, spell)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("cast = %+v, want the creature target ask", d)
	}
	d = kr2Want(t, kr2Answer(t, e, d, kr2ObjIdx(t, d, victim)), "put_optional")
	if z := e.G.Obj(victim).Zone; z != state.ZExile {
		t.Fatalf("victim is on %s at the election, want exile", z)
	}
	if got := e.G.Obj(victim).Counter("TIME"); got != 0 {
		t.Fatalf("placed %d TIME counters before the election", got)
	}
	if d = kr2Answer(t, e, d, kr2Kind(t, d, "yes")); d != nil {
		t.Fatalf("unexpected ask: %+v", d)
	}
	if got := e.G.Obj(victim).Counter("TIME"); got != 3 {
		t.Fatalf("exiled object TIME = %d, want 3", got)
	}
}

func TestPutCounterOptionalPlayerRecipientRemembersThePlayer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		accept bool
	}{{"decline", false}, {"accept", true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := kr2Engine(t, 2)
			spell := kr2Put(t, e, 0, kr2Sorcery(t, "Charger",
				"A:SP$ PutCounter | Defined$ You | CounterType$ ENERGY | CounterNum$ 2 | RememberPut$ True | Optional$ True | SubAbility$ DBGain",
				"SVar:DBGain:DB$ GainLife | Defined$ Remembered | LifeAmount$ 3"), state.ZHand, false)
			life := e.G.Players[0].Life
			from := len(e.L.Events)
			d := kr2Want(t, kr2Cast(t, e, 0, spell), "put_optional")
			choice := kr2Kind(t, d, "no")
			if tc.accept {
				choice = kr2Kind(t, d, "yes")
			}
			if d = kr2Answer(t, e, d, choice); d != nil {
				t.Fatalf("unexpected ask: %+v", d)
			}
			pcs := kr2Events(e, from, events.PlayerCounterChange)
			if !tc.accept {
				if len(pcs) != 0 || e.G.Players[0].Life != life {
					t.Fatalf("decline placed %+v / life %d->%d, want nothing", pcs, life, e.G.Players[0].Life)
				}
				return
			}
			if len(pcs) != 1 || pcs[0].Player != 0 || pcs[0].Counter != "ENERGY" || pcs[0].Amount != 2 {
				t.Fatalf("player counter events = %+v, want one ENERGY x2 on seat 0", pcs)
			}
			if got := e.G.Players[0].Counter("ENERGY"); got != 2 {
				t.Fatalf("energy = %d, want 2", got)
			}
			if got := e.G.Players[0].Life; got != life+3 {
				t.Fatalf("life = %d, want %d (RememberPut$ remembered the recipient player)", got, life+3)
			}
		})
	}
}
