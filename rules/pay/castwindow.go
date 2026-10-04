package pay

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The announced CR 601.2g window's activation records (announce-then-pay spec
// §5; lasagna spec §9.2, E7 flow slice 4): one WindowTap per activation made
// from the window, judged reversible or not when it closes, and reversed by
// Undo last tap / Cancel cast. trig is always the engine's pending-trigger
// queue length at the call (the queue is the engine's flow state).

// BeginWindowTap closes the previous record and opens one for the activation
// about to run from source.
func BeginWindowTap(e Engine, cp *CastPayment, payer state.PlayerID, source state.ObjID, normal bool, trig int) {
	CloseWindowTap(e, cp, payer, trig)
	cp.WindowTaps = append(cp.WindowTaps, WindowTap{Source: source, Mark: len(e.Log().Events),
		Trig: trig, Normal: normal})
}

// CloseWindowTap judges the open record's span (spec §5 conditions 1-3): a
// normal-tier ability whose activation logged exactly one Tap of its source
// and positive plain/snow ManaAdds to the payer, and queued no trigger.
func CloseWindowTap(e Engine, cp *CastPayment, payer state.PlayerID, trig int) {
	n := len(cp.WindowTaps)
	if n == 0 || cp.WindowTaps[n-1].Closed {
		return
	}
	t := &cp.WindowTaps[n-1]
	t.Closed = true
	log := e.Log().Events
	if !t.Normal || trig != t.Trig || t.Mark > len(log) {
		return
	}
	taps := 0
	var adds []WindowTapAdd
	for _, ev := range log[t.Mark:] {
		switch ev.Kind {
		case events.Tap:
			if ev.Obj != t.Source {
				return
			}
			taps++
		case events.ManaAdd:
			if ev.Player != payer || ev.Amount <= 0 || ev.Text != "" || !PlainOrSnowManaCounter(ev.Counter) {
				return
			}
			adds = append(adds, WindowTapAdd{Counter: ev.Counter, Amount: ev.Amount})
		default:
			return
		}
	}
	if taps != 1 || len(adds) == 0 {
		return
	}
	t.Adds, t.Reversible = adds, true
}

// UndoableWindowTap reports the last record when it can be reversed now
// (spec §5 conditions 4-5): the source is still on the battlefield, tapped,
// the payer's and free of stun counters, and the pool still holds every unit
// it added.
func UndoableWindowTap(e Engine, cp *CastPayment, payer state.PlayerID) (WindowTap, bool) {
	n := len(cp.WindowTaps)
	if n == 0 {
		return WindowTap{}, false
	}
	t := cp.WindowTaps[n-1]
	if !t.Closed || !t.Reversible {
		return WindowTap{}, false
	}
	g := e.Game()
	o := g.Obj(t.Source)
	if o == nil || o.Zone != state.ZBattlefield || !o.Tapped || o.Controller != payer || o.Counter("STUN") > 0 {
		return WindowTap{}, false
	}
	pl := g.Players[payer]
	var need, snow state.Mana
	for _, a := range t.Adds {
		idx := ManaCounterSlot(a.Counter)
		need[idx] += a.Amount
		if len(a.Counter) == 2 {
			snow[idx] += a.Amount
		}
	}
	for i := range need {
		if need[i] > pl.Pool[i] || snow[i] > pl.Snow[i] {
			return WindowTap{}, false
		}
	}
	return t, true
}

// UndoWindowTap reverses the last record (spec §5): one ManaUndo per
// recorded ManaAdd, the first also untapping the source. The caller has
// checked UndoableWindowTap, and drops any trigger the reversal queued
// (CR 733.1: the queue is its flow state).
func UndoWindowTap(e Engine, cp *CastPayment, payer state.PlayerID) {
	n := len(cp.WindowTaps)
	t := cp.WindowTaps[n-1]
	cp.WindowTaps = cp.WindowTaps[:n-1]
	for i, a := range t.Adds {
		ev := events.Event{Kind: events.ManaUndo, Player: payer, Counter: a.Counter, Amount: a.Amount}
		if i == 0 {
			ev.Obj = t.Source
		}
		e.Emit(ev)
	}
}
