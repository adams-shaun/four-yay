package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestMustBlockCorpusWatchdogRequirement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	watchdog, ok := reg.Lookup("Watchdog")
	if !ok {
		t.Fatal("corpus missing Watchdog")
	}
	bear := card(t, staticBearFixture)
	e, _ := restrictionGame(t, 90931,
		[][]*cards.Card{nil, nil},
		[][]*cards.Card{{bear}, {watchdog}})
	attacker := bearOnBoard(t, e, 0, bear)
	blocker := bearOnBoard(t, e, 1, watchdog)
	if e.G.Obj(blocker).Zone != state.ZBattlefield || e.G.Obj(attacker).Zone != state.ZBattlefield {
		t.Fatal("fixture creatures must be on the battlefield")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
	if !e.canBlock(blocker, attacker) {
		t.Fatal("Watchdog must be able to block the fixture attacker")
	}
	e.G.Step = state.StepDeclareBlockers
	e.askBlockers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KBlockers {
		t.Fatalf("no blocker declaration decision: %+v", d)
	}
	var required bool
	for _, opt := range d.Options {
		if opt.Obj == blocker && opt.Attacker == attacker {
			required = opt.Required
		}
	}
	if !required {
		t.Fatalf("Watchdog's block option is not Required: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err == nil {
		t.Fatal("empty declaration ignored Watchdog's MustBlock requirement")
	}
}
