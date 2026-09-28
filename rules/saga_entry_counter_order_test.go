package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Saga entry lore under a non-commuting AddCounter pair (cli-20260924T034908Z).
// A Saga enters with its first lore counter (CR 702.151a), and CR 614.12
// requires the CR 616.1 order choice over competing AddCounter replacements to
// be answered BEFORE the permanent is on the battlefield with that counter:
// the entry move must carry the finalized amount, never fold first and adjust.
// Two LORE-scoped replacements modelled on Hardened Scales (that many plus
// one) and Branching Evolution (twice that many) do not commute -- 1 -> 2 -> 4
// against 1 -> 2 -> 3 -- so the answer, not a fixed total, decides the entered
// lore. Chapter abilities trigger off the lore counter's placement, so a Saga
// entering with a doubled/plus-one counter queues every chapter its final lore
// reaches exactly once.
//
// The ordinary entry path stages exactly this (rules/entry_counters.go
// entryCounterOrderParks -> entryCounterStage -> foldEntryMove): the CR 616.1
// ask is pending while NO MoveZone has folded, the entry then folds with the
// answered amount in its Pairs payload, and the only CounterChange for the
// object is the notification-only EntryCounterNotice marker.

// entryCounterSagaLoreScales is a LORE-scoped "that many plus one" replacement,
// authored inline exactly like Hardened Scales' corpus line so the parser and
// the AddCounter matcher see a real card rather than a hand-shaped event.
func entryCounterSagaLoreScales(t testing.TB) *cards.Card {
	return card(t, "Name:Saga Entry Lore Scales\nTypes:Enchantment\n"+
		"R:Event$ AddCounter | ActiveZones$ Battlefield | ValidCard$ Permanent.YouCtrl+inZoneBattlefield | ValidCounterType$ LORE | ReplaceWith$ AddOneMore | Description$ If one or more lore counters would be put on a permanent you control, that many plus one lore counters are put on it instead.\n"+
		"SVar:AddOneMore:DB$ ReplaceCounter | ValidCounterType$ LORE | ChooseCounter$ True | Amount$ X\n"+
		"SVar:X:ReplaceCount$CounterNum/Plus.1\n"+
		"Oracle:If one or more lore counters would be put on a permanent you control, that many plus one lore counters are put on it instead.\n")
}

// entryCounterSagaLoreEvolution is a LORE-scoped "twice that many" replacement, the
// non-commuting partner of entryCounterSagaLoreScales.
func entryCounterSagaLoreEvolution(t testing.TB) *cards.Card {
	return card(t, "Name:Saga Entry Lore Evolution\nTypes:Enchantment\n"+
		"R:Event$ AddCounter | ActiveZones$ Battlefield | ValidCard$ Permanent.YouCtrl+inZoneBattlefield | ValidCounterType$ LORE | ReplaceWith$ DoubleIt | Description$ If one or more lore counters would be put on a permanent you control, twice that many lore counters are put on it instead.\n"+
		"SVar:DoubleIt:DB$ ReplaceCounter | ValidCounterType$ LORE | Amount$ X\n"+
		"SVar:X:ReplaceCount$CounterNum/Twice\n"+
		"Oracle:If one or more lore counters would be put on a permanent you control, twice that many lore counters are put on it instead.\n")
}

// entryCounterSagaChapters returns the chapter SVar names queued for a Saga, in the
// deterministic APNAP order the chapter list names them.
func entryCounterSagaChapters(e *Engine, id state.ObjID) []string {
	var got []string
	for _, pt := range e.pendingTriggers {
		if pt.Source == id {
			got = append(got, pt.Execute)
		}
	}
	return got
}

// TestEntryCounterSagaLoreOrderIsDecidedBeforeEntry is the brief's Saga clause. It
// drives the staged CR 616.1 ask with BOTH answers and asserts the entered
// lore differs by that answer (so a fixed-total or fold-first path fails), the
// entry folds atomically, and the chapters that the final lore reaches queue
// exactly once each, independent of which replacement was chosen first.
func TestEntryCounterSagaLoreOrderIsDecidedBeforeEntry(t *testing.T) {
	t.Parallel()
	saga := tokenReplCorpusCard(t, "Urza's Saga")
	// Precondition: with no replacement the Saga enters with exactly one
	// lore counter and queues exactly its first chapter, once. If this base
	// case changed, the doubled/plus-one assertions below would be measuring
	// a different entry, not the replacement.
	{
		base, baseCfg := tokenReplGame(t, 301, saga)
		baseID := seededEntryID(t, base, saga)
		base.emit(events.Event{Kind: events.MoveZone, Obj: baseID,
			From: base.G.Obj(baseID).Zone, To: state.ZBattlefield})
		if got := base.G.Obj(baseID).Counter("LORE"); got != 1 {
			t.Fatalf("precondition: un-replaced Saga entered with %d lore, want 1", got)
		}
		if got := entryCounterSagaChapters(base, baseID); len(got) != 1 || got[0] != "Animate1" {
			t.Fatalf("precondition: un-replaced Saga queued chapters %v, want [Animate1] exactly once", got)
		}
		replayCheck(t, base, baseCfg)
	}

	for _, tc := range []struct {
		name  string
		pick  int
		want  int32
		order string
	}{
		{"scales-first", 0, 4, "plus one, then twice"},
		{"evolution-first", 1, 3, "twice, then plus one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scales := entryCounterSagaLoreScales(t)
			evo := entryCounterSagaLoreEvolution(t)
			e, cfg := tokenReplGame(t, 303, scales, evo, saga)
			sid := moveSeededCard(t, e, 0, scales, state.ZBattlefield)
			eid := moveSeededCard(t, e, 0, evo, state.ZBattlefield)
			if o := e.G.Obj(sid); o == nil || o.Zone != state.ZBattlefield {
				t.Fatal("precondition: the plus-one replacement is not on the battlefield")
			}
			if o := e.G.Obj(eid); o == nil || o.Zone != state.ZBattlefield {
				t.Fatal("precondition: the doubling replacement is not on the battlefield")
			}
			id := seededEntryID(t, e, saga)
			if e.G.Obj(id).Zone == state.ZBattlefield {
				t.Fatal("precondition: the Saga already entered")
			}
			e.SetCounterAdder(0)
			e.emit(events.Event{Kind: events.MoveZone, Obj: id,
				From: e.G.Obj(id).Zone, To: state.ZBattlefield})

			// The ask is posed and the entry has NOT folded: no battlefield
			// MoveZone may exist in the log while the order is pending.
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement {
				t.Fatalf("ask = %+v, want the CR 616.1 order choice (KReplacement) before the entry folds", d)
			}
			for _, ev := range e.L.Events {
				if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
					t.Fatalf("entry folded before the CR 616.1 answer: %+v", ev)
				}
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{tc.pick}}); err != nil {
				t.Fatalf("submit order answer: %v", err)
			}

			o := e.G.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("Saga did not enter: %+v", o)
			}
			if got := o.Counter("LORE"); got != tc.want {
				t.Fatalf("%s: entered lore = %d, want %d (1 -> 2 -> 4 as %s; the answer must decide)", tc.name, got, tc.want, tc.order)
			}
			// Atomicity: the entry MoveZone carries the final amount in its
			// Pairs payload, and the ONLY CounterChange for the object is the
			// notification-only marker (no folds-then-adjusts placement).
			moveSeen := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZBattlefield {
					moveSeen = true
					if len(ev.Pairs) == 0 {
						t.Fatalf("entry not atomic: the MoveZone carried no counter pairs: %+v", ev)
					}
				}
				if ev.Kind == events.CounterChange && ev.Obj == id && ev.Counter == "LORE" &&
					ev.Text != events.EntryCounterNotice {
					t.Fatalf("entry counter placed as a real event after the move: %+v", ev)
				}
			}
			if !moveSeen {
				t.Fatal("precondition: the entry MoveZone is absent")
			}
			// Chapters: the final lore reaches I, II and III, each exactly
			// once -- the doubling must not double-queue a chapter, and the
			// answer order must not change the set.
			want := []string{"Animate1", "Animate2", "Tutor"}
			if got := entryCounterSagaChapters(e, id); len(got) != len(want) {
				t.Fatalf("%s: chapter triggers = %v, want %v (each reached chapter exactly once)", tc.name, got, want)
			} else {
				for i := range got {
					if got[i] != want[i] {
						t.Fatalf("%s: chapter triggers = %v, want %v", tc.name, got, want)
					}
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}
