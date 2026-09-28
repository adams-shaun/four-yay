package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// seededEntryID keeps the pending trigger queue intact (moveSeededCard clears it).
func seededEntryID(t *testing.T, e *Engine, c *cards.Card) state.ObjID {
	t.Helper()
	toMain1(t, e)
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == c.Faces[0].Name {
				return id
			}
		}
	}
	t.Fatalf("precondition: %s not seeded", c.Faces[0].Name)
	return 0
}

func TestEntryUnleashCountersAreAtomicUnderScalesAndSolemnity(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int32
	}{{"Hardened Scales", 2}, {"Solemnity", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			rider := tokenReplCorpusCard(t, tc.name)
			cre := unleashCard(t, "Rakdos Cackler")
			e, cfg := tokenReplGame(t, 165, rider, cre)
			rid := moveSeededCard(t, e, 0, rider, state.ZBattlefield)
			if o := e.G.Obj(rid); o == nil || o.Zone != state.ZBattlefield {
				t.Fatal("precondition: replacement source not on battlefield")
			}
			cid := seededEntryID(t, e, cre)
			if e.G.Obj(cid).Zone == state.ZBattlefield {
				t.Fatal("precondition: creature already entered")
			}
			e.SetCounterAdder(0)
			enterWithUnleashChoice(t, e, cid, e.G.Obj(cid).Zone, true)
			o := e.G.Obj(cid)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("creature did not enter: %+v", o)
			}
			if got := o.Counter("P1P1"); got != tc.want {
				t.Fatalf("entry counter = %d, want %d", got, tc.want)
			}
			// The move itself carries the final counter, before the notification event.
			for i, ev := range e.L.Events {
				if ev.Kind != events.MoveZone || ev.Obj != cid || ev.To != state.ZBattlefield {
					continue
				}
				if i+1 >= len(e.L.Events) || (tc.want > 0 && len(ev.Pairs) == 0) {
					t.Fatalf("entry not atomic: move=%+v", ev)
				}
				break
			}
			replayCheck(t, e, cfg)
		})
	}
}

func TestRedirectedEntryLeavesNoEntryCounters(t *testing.T) {
	t.Parallel()
	priest := tokenReplCorpusCard(t, "Containment Priest")
	cre := card(t, "Name:Entry Creature Walker\nTypes:Creature Planeswalker Entry\nPT:2/2\nLoyalty:4\nOracle:x\n")
	control, controlCfg := tokenReplGame(t, 174, cre)
	base := seededEntryID(t, control, cre)
	control.emit(events.Event{Kind: events.MoveZone, Obj: base,
		From: control.G.Obj(base).Zone, To: state.ZBattlefield})
	if got := control.G.Obj(base).Counter("LOYALTY"); got != 4 {
		t.Fatalf("precondition: unredirected walker enters with %d loyalty, want 4", got)
	}
	foundGrant := false
	for _, ev := range control.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == base && ev.To == state.ZBattlefield {
			foundGrant = len(ev.Pairs) != 0
		}
	}
	if !foundGrant {
		t.Fatal("unredirected entry lacked its atomic loyalty grant")
	}
	replayCheck(t, control, controlCfg)

	e, cfg := tokenReplGame(t, 173, priest, cre)
	pid := seededEntryID(t, e, priest)
	// Seed the Priest as an already-established permanent: its own
	// not-cast entry replacement would exile a synthetic direct entry.
	events.Emit(e.G, e.L, events.Event{Kind: events.MoveZone, Obj: pid,
		From: e.G.Obj(pid).Zone, To: state.ZBattlefield})
	if o := e.G.Obj(pid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Containment Priest absent")
	}
	id := seededEntryID(t, e, cre)
	from := e.G.Obj(id).Zone
	if from == state.ZBattlefield {
		t.Fatal("precondition: creature already entered")
	}
	e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZBattlefield})
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZExile || o.Counter("LOYALTY") != 0 {
		t.Fatalf("redirected creature = %+v, want exile with no entry counter", o)
	}
	for _, ev := range e.L.Events {
		if ev.Obj == id && ev.Kind == events.MoveZone && ev.To == state.ZBattlefield {
			t.Fatalf("prevented battlefield entry was logged: %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}

func TestSagaEntryLoreReplacementChaptersExactlyOnce(t *testing.T) {
	t.Parallel()
	saga := tokenReplCorpusCard(t, "Urza's Saga")
	for _, tc := range []struct {
		name     string
		want     int32
		chapters []string
	}{
		{"Vorinclex, Monstrous Raider", 2, []string{"Animate1", "Animate2"}},
		{"Solemnity", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rider := tokenReplCorpusCard(t, tc.name)
			e, cfg := tokenReplGame(t, 169, rider, saga)
			rid := moveSeededCard(t, e, 0, rider, state.ZBattlefield)
			if o := e.G.Obj(rid); o == nil || o.Zone != state.ZBattlefield {
				t.Fatal("precondition: replacement source absent")
			}
			id := seededEntryID(t, e, saga)
			if e.G.Obj(id).Zone == state.ZBattlefield {
				t.Fatal("precondition: saga already on battlefield")
			}
			e.SetCounterAdder(0)
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("saga did not enter: %+v", o)
			}
			if got := o.Counter("LORE"); got != tc.want {
				t.Fatalf("entry lore = %d, want %d", got, tc.want)
			}
			// The finalized lore is visible as part of the Move, before the
			// notification and before ETB triggers inspect this permanent.
			if tc.want > 0 {
				found := false
				for _, ev := range e.L.Events {
					if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
						found = true
						if len(ev.Pairs) == 0 {
							t.Fatal("entry lore not folded into MoveZone")
						}
					}
				}
				if !found {
					t.Fatal("precondition: saga has no entry move")
				}
			}
			var got []string
			for _, pt := range e.pendingTriggers {
				if pt.Source == id {
					got = append(got, pt.Execute)
				}
			}
			if len(got) != len(tc.chapters) {
				t.Fatalf("chapter triggers = %v, want %v", got, tc.chapters)
			}
			for i := range got {
				if got[i] != tc.chapters[i] {
					t.Fatalf("chapter triggers = %v, want %v", got, tc.chapters)
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}
