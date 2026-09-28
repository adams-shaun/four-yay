package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestGainedAnimateGrantResolvesOffTheForeignCard pins cardfuzz batch7 line
// 2 on the real corpus pair: Manascape Refractor has all activated abilities
// of all lands, so it gains Spawning Pool's "{1}{B}: becomes a 1/1 Skeleton
// with '{B}: Regenerate this creature'". The Animate resolves as a gained
// wrapper, so its Abilities$ ABRegen names an SVar on SPAWNING POOL's face,
// never the Refractor's. The grant was registered with the Refractor as its
// grantor: the offer loop read the body off the effect's captured table and
// offered "Regenerate", while the activation resolved the name off the
// Refractor's own face, found nothing and silently returned -- the bot
// re-chose the no-op option 100 times in one main phase (a livelock).
//
// The granted ability must be offered AND activate: it goes on the stack
// through GrantAbilityPush naming Spawning Pool as the table owner, and its
// resolution gives the REFRACTOR (the recipient) a regeneration shield.
func TestGainedAnimateGrantResolvesOffTheForeignCard(t *testing.T) {
	t.Parallel()
	refractor := tokenReplCorpusCard(t, "Manascape Refractor")
	pool := tokenReplCorpusCard(t, "Spawning Pool")
	e, cfg := tokenReplGame(t, 9171, refractor, pool)
	refID := moveSeededCard(t, e, 0, refractor, state.ZBattlefield)
	poolID := moveSeededCard(t, e, 0, pool, state.ZBattlefield)
	e.priorityRound()
	gainsDriveToStep(t, e, 3, 0, state.StepMain1)

	pick := func(what string, match func(decision.Option) bool) {
		t.Helper()
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
			t.Fatalf("pending = %+v, want seat 0 priority", d)
		}
		for _, o := range d.Options {
			if match(o) {
				submitChoices(t, e, o.Index)
				return
			}
		}
		t.Fatalf("no %s option offered: %+v", what, d.Options)
	}

	floatMana(t, e, 0, "BB")
	e.pending = nil
	e.askPriority(0)
	pick("gained Animate", func(o decision.Option) bool {
		return o.Kind == "ability" && o.Obj == refID && o.GainedSource == poolID &&
			strings.Contains(o.Label, "Skeleton")
	})
	passUntilStackEmpty(t, e, 20)
	isCreature := false
	for _, ty := range e.Derived(refID).Types {
		if ty == "Creature" {
			isCreature = true
		}
	}
	if !isCreature {
		t.Fatalf("Manascape Refractor types = %v after the gained Animate, want a Creature", e.Derived(refID).Types)
	}

	floatMana(t, e, 0, "B")
	e.pending = nil
	e.askPriority(0)
	before := len(e.L.Events)
	pick("granted Regenerate", func(o decision.Option) bool {
		return o.Kind == "ability" && o.Obj == refID && o.SVar == "ABRegen"
	})
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack depth after activating the granted Regenerate = %d, want 1 (the option was a no-op)", len(e.G.Stack))
	}
	pushed := false
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.GrantAbilityPush && ev.Obj == refID && len(ev.IDs) > 0 && ev.IDs[0] == poolID {
			pushed = true
		}
	}
	if !pushed {
		t.Fatalf("no GrantAbilityPush naming Spawning Pool as the grantor")
	}
	passUntilStackEmpty(t, e, 20)
	if got := countersOf(e.G.Obj(refID), "Shield"); got != 1 {
		t.Fatalf("Refractor regeneration shields = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}
