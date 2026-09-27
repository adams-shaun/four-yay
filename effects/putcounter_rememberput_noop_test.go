package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestPutCounterRememberPutOnlyRemembersPositivePlacement pins
// api:PutCounter's RememberPut$ recipient capture at the API body itself
// (Synth Eradicator's DBEnergy is the corpus carrier). RememberPut$ names the
// recipient a pass actually CounterChanged, never the attempt: a body whose
// count resolves to zero emits a zero CounterChange and must remember
// nothing, so a chained EQ0 gate over the set cannot run its "you did put"
// follow-up on a paid no-op. The positive half of each axis is asserted in
// the same test so a broken capture cannot pass by remembering nothing
// everywhere.
func TestPutCounterRememberPutOnlyRemembersPositivePlacement(t *testing.T) {
	// positiveCount runs the API body over one recipient with the given count
	// and returns the remembered targets and the CounterChange events emitted.
	run := func(counterNum string, player bool) ([]state.Target, []events.Event) {
		h := &askHost{}
		h.g = state.NewGame(names(2))
		src := putCounterObject(t, &h.fakeHost)
		if src == 0 || h.g.Obj(src) == nil || h.g.Obj(src).Zone != state.ZBattlefield {
			t.Fatalf("precondition: recipient object %d is not on the battlefield", src)
		}
		sa := &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
			"Defined":     "Targeted",
			"CounterType": "P1P1",
			"CounterNum":  counterNum,
			"RememberPut": "True",
		}}
		c := &Ctx{Source: src, Controller: 0, SVars: map[string]string{}}
		if player {
			c.Targets = []state.Target{{Player: 0, IsPlayer: true}}
		} else {
			c.Targets = []state.Target{{Obj: src}}
		}
		Resolve(h, c, sa)
		for _, ev := range h.log {
			if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API") {
				t.Fatalf("PutCounter handler did not run: %q", ev.Text)
			}
		}
		var changes []events.Event
		for _, ev := range h.log {
			if ev.Kind == events.CounterChange || ev.Kind == events.PlayerCounterChange {
				changes = append(changes, ev)
			}
		}
		return append([]state.Target(nil), c.Remembered...), changes
	}

	t.Run("positive object recipient is remembered", func(t *testing.T) {
		remembered, changes := run("2", false)
		if len(remembered) != 1 || remembered[0].IsPlayer || remembered[0].Obj == 0 {
			t.Fatalf("Remembered = %+v, want exactly the countered object", remembered)
		}
		if len(changes) != 1 {
			t.Fatalf("CounterChange events = %+v, want exactly one", changes)
		}
		if changes[0].Amount != 2 {
			t.Fatalf("placed amount = %d, want 2", changes[0].Amount)
		}
	})

	t.Run("zero-count object recipient is not remembered", func(t *testing.T) {
		remembered, changes := run("0", false)
		if len(remembered) != 0 {
			t.Fatalf("Remembered = %+v, want empty: a zero-count put remembers nothing", remembered)
		}
		// The zero CounterChange is emitted (the engine keeps the event), but
		// no positive counter landed, so the recipient must not be captured.
		for _, ev := range changes {
			if ev.Amount > 0 {
				t.Fatalf("a zero-count put emitted a positive CounterChange: %+v", ev)
			}
		}
	})
}

// TestPutCounterRememberPutZeroPlayerRecipientIsNotRemembered is the player-
// axis companion: a zero-count player placement (an {E}{E} body whose count
// resolves to 0) must not leave the player in the remembered set, or the
// chained EQ0 gate reads "you got energy" after a no-op.
func TestPutCounterRememberPutZeroPlayerRecipientIsNotRemembered(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	src := putCounterObject(t, &h.fakeHost)
	sa := &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
		"Defined":     "You",
		"CounterType": "ENERGY",
		"CounterNum":  "0",
		"RememberPut": "True",
	}}
	c := &Ctx{Source: src, Controller: 0, SVars: map[string]string{}}
	Resolve(h, c, sa)
	if got := h.g.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("precondition: energy = %d, want 0", got)
	}
	if len(c.Remembered) != 0 {
		t.Fatalf("Remembered = %+v, want empty: a zero-count energy put remembers nothing", c.Remembered)
	}
}

// TestPutCounterPickApplyRemembersOnlyPositivePlacement covers the Choices$
// mid-resolution pick sibling (effects/counters.go putCounterPickApply): it
// appends its chosen recipients to the same placed list rememberPlaced folds
// under RememberPut$/RememberCards$, so it must keep the same positivity
// contract -- a CounterNum$ resolving to 0 emits a zero CounterChange and
// must not leave the recipient remembered.
func TestPutCounterPickApplyRemembersOnlyPositivePlacement(t *testing.T) {
	h := &askHost{}
	h.g = state.NewGame(names(2))
	target := putCounterObject(t, &h.fakeHost)
	sa := &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{"RememberPut": "True"}}

	zero := &Ctx{Source: target, Controller: 0}
	putCounterPickApply(h, zero, sa, 0, "P1P1", []state.ObjID{target})
	if len(zero.Remembered) != 0 {
		t.Fatalf("zero-count pick Remembered = %+v, want empty", zero.Remembered)
	}

	pos := &Ctx{Source: target, Controller: 0}
	putCounterPickApply(h, pos, sa, 2, "P1P1", []state.ObjID{target})
	if len(pos.Remembered) != 1 || pos.Remembered[0].Obj != target {
		t.Fatalf("positive pick Remembered = %+v, want exactly the countered object %d", pos.Remembered, target)
	}
}

// TestPutCounterEachFromSourceRemembersOnlyPositivePlacement covers the
// EachFromSource$ copy sibling (effects/counters.go putCounterEachFromSource):
// a source whose counters are all gone (or a CounterNum$ multiplier resolving
// to 0) emits no CounterChange, so the destination must not be remembered by
// RememberPut$.
func TestPutCounterEachFromSourceRemembersOnlyPositivePlacement(t *testing.T) {
	run := func(srcCounters []state.Counter) []state.Target {
		h := &askHost{}
		h.g = state.NewGame(names(2))
		srcCard := mkCard(t, "Name:EachSrc\nTypes:Creature\nPT:1/1\nOracle:x\n")
		src := h.g.AddObject(srcCard, 0)
		src.Zone = state.ZBattlefield
		src.Counters = srcCounters
		dstCard := mkCard(t, "Name:EachDst\nTypes:Creature\nPT:1/1\nOracle:x\n")
		dst := h.g.AddObject(dstCard, 0)
		dst.Zone = state.ZBattlefield
		sa := &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
			"Defined": "Targeted", "CounterType": "EachFromSource",
			"EachFromSource": "Remembered", "CounterNum": "1", "RememberPut": "True",
		}}
		c := &Ctx{Source: src.ID, Controller: 0,
			Targets:    []state.Target{{Obj: dst.ID}},
			Remembered: []state.Target{{Obj: src.ID}}}
		base := len(c.Remembered)
		putCounterEachFromSource(h, c, sa, false, "Remembered")
		// Return only what the pass APPENDED: c.Remembered is pre-seeded with
		// the EachFromSource$ referent, so the whole slice is never empty.
		return c.Remembered[base:]
	}

	if got := run(nil); len(got) != 0 {
		t.Fatalf("counter-less source Remembered = %+v, want empty", got)
	}
	if got := run([]state.Counter{{Kind: "P1P1", N: 1}}); len(got) != 1 {
		t.Fatalf("counter-carrying source Remembered = %+v, want exactly one destination", got)
	}
}
