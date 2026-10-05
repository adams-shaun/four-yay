package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestAbigaleEloquentFirstYearPutsAKeywordCounterOfEachType(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	abigale := lookup(t, reg, "Abigale, Eloquent First-Year")
	bears := lookup(t, reg, "Grizzly Bears")
	e := corpusEngine(t, reg, []*cards.Card{abigale, bears}, []*cards.Card{})
	bearID := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	abigaleID := moveByName(t, e, 0, "Abigale, Eloquent First-Year", state.ZBattlefield)

	bear := e.G.Obj(bearID)
	if bear == nil || bear.Zone != state.ZBattlefield {
		t.Fatalf("target precondition: Grizzly Bears %d is not on the battlefield: %+v", bearID, bear)
	}
	for _, kind := range []string{"Flying", "First Strike", "Lifelink", "P1P1"} {
		if got := bear.Counter(kind); got != 0 {
			t.Fatalf("target precondition: Grizzly Bears already has %s=%d", kind, got)
		}
	}
	if source := e.G.Obj(abigaleID); source == nil || source.Zone != state.ZBattlefield {
		t.Fatalf("trigger source precondition: Abigale %d is not on the battlefield: %+v", abigaleID, source)
	}

	d := drainToTargetAsk(t, e, 40)
	if d.Kind != decision.KTarget {
		t.Fatalf("Abigale ETB animate step did not ask for its creature target: %+v", d)
	}
	targetIndex := indexOfObjOption(d, bearID)
	if targetIndex < 0 {
		t.Fatalf("Abigale's target ask does not offer Grizzly Bears %d: %+v", bearID, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{targetIndex}}); err != nil {
		t.Fatalf("choose Grizzly Bears for Abigale: %v", err)
	}
	passUntilStackEmpty(t, e, 30)

	bear = e.G.Obj(bearID)
	if bear == nil || bear.Zone != state.ZBattlefield {
		t.Fatalf("resolved target is not on the battlefield: %+v", bear)
	}
	want := map[string]int32{"Flying": 1, "First Strike": 1, "Lifelink": 1, "P1P1": 0}
	for _, kind := range []string{"Flying", "First Strike", "Lifelink", "P1P1"} {
		got := bear.Counter(kind)
		if got != want[kind] {
			t.Fatalf("Grizzly Bears %s counter = %d, want %d (all counters: Flying=%d First Strike=%d Lifelink=%d P1P1=%d)", kind, got, want[kind], bear.Counter("Flying"), bear.Counter("First Strike"), bear.Counter("Lifelink"), bear.Counter("P1P1"))
		}
	}
}
