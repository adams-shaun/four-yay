package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCounterAddedAllCarrierCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := map[string][]string{"CounterAddedAll": {}, "CounterTypeAddedAll": {}}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			seen := map[string]bool{}
			for _, tr := range f.Triggers {
				if _, ok := got[tr.Mode]; ok {
					seen[tr.Mode] = true
				}
			}
			// Danny Pink grants this trigger from a static's AddTrigger$ SVar,
			// rather than carrying it as a printed T: line.
			for _, body := range f.SVars {
				for mode := range got {
					if strings.Contains(body, "Mode$ "+mode) {
						seen[mode] = true
					}
				}
			}
			for mode := range seen {
				got[mode] = append(got[mode], f.Name)
			}
		}
	}
	want := map[string][]string{
		"CounterAddedAll":     {"Bioessence Hydra", "Cloaked Cadet", "Invisible Woman, Sue Storm"},
		"CounterTypeAddedAll": {"Danny Pink", "Putrid Hexhag", "Stalwart Successor"},
	}
	for mode := range got {
		sort.Strings(got[mode])
		if len(got[mode]) != len(want[mode]) {
			t.Fatalf("%s carriers = %v, want %v", mode, got[mode], want[mode])
		}
		for i := range want[mode] {
			if got[mode][i] != want[mode][i] {
				t.Fatalf("%s carriers = %v, want %v", mode, got[mode], want[mode])
			}
		}
	}
}

func TestCounterAddedAllBioessenceHydraBatchAmount(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	hydra := onBoardCard(t, e, 0, mshCorpusCard(t, "Bioessence Hydra"))
	pw1 := onBoard(t, e, 0, "Name:Walker One\nTypes:Planeswalker\nPT:4\nOracle:x\n")
	pw2 := onBoard(t, e, 0, "Name:Walker Two\nTypes:Planeswalker\nPT:4\nOracle:x\n")
	if e.G.Obj(hydra).Zone != state.ZBattlefield || e.G.Obj(pw1).Zone != state.ZBattlefield || e.G.Obj(pw2).Zone != state.ZBattlefield {
		t.Fatal("precondition: Hydra and both planeswalkers must be on the battlefield")
	}
	before := len(e.pendingTriggers)
	e.BeginActionBatch()
	e.emit(events.Event{Kind: events.CounterChange, Obj: pw1, Counter: "LOYALTY", Amount: 2})
	e.emit(events.Event{Kind: events.CounterChange, Obj: pw2, Counter: "LOYALTY", Amount: 3})
	e.EndActionBatch()
	if got := len(e.pendingTriggers) - before; got != 1 {
		t.Fatalf("one multi-recipient loyalty action queued %d Hydra triggers, want 1", got)
	}
	if got := e.pendingTriggers[before].Ctx.TriggerContext.TriggerAmount; got != 5 {
		t.Fatalf("Hydra TriggerCount$Amount = %d, want 5 from 2+3 counters", got)
	}
	// The action bracket is closed: another event is a distinct action.
	e.emit(events.Event{Kind: events.CounterChange, Obj: pw1, Counter: "LOYALTY", Amount: 1})
	if got := len(e.pendingTriggers) - before; got != 2 {
		t.Fatalf("separate loyalty action queued total %d Hydra triggers, want 2", got)
	}
}

func TestCounterTypeAddedAllDannyFirstTimePerRecipient(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	danny := onBoardCard(t, e, 0, mshCorpusCard(t, "Danny Pink"))
	if e.G.Obj(danny).Zone != state.ZBattlefield {
		t.Fatal("precondition: Danny must be on the battlefield")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: danny, Counter: "P1P1", Amount: 2})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("first counter placement queued %d Danny triggers, want 1", len(e.pendingTriggers))
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: danny, Counter: "P1P1", Amount: 1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("second placement on Danny this turn queued %d total triggers, want 1", len(e.pendingTriggers))
	}
	// The compiled mode vocabulary must resolve both names, not fall through
	// as unknown code zero.
	if cards.TriggerModeOf("CounterTypeAddedAll") == 0 {
		t.Fatal("CounterTypeAddedAll has no vocabulary code")
	}
}
