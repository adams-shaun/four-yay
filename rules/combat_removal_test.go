package rules

// CR 506.4: an attacking or blocking creature that stops being a creature is
// removed from combat. Imprisoned in the Moon makes the enchanted permanent a
// noncreature land; on an attacker it stops attacking (and so deals no combat
// damage), on a blocker it stops blocking while the attacker it blocked stays
// blocked (CR 509.1h).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestNoncreatureAttackerAndBlockerAreRemovedFromCombat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	t.Run("attacker", func(t *testing.T) {
		t.Parallel()
		e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Craw Wurm")},
			[]*cards.Card{lookup(t, reg, "Imprisoned in the Moon")})
		wurm := moveCorpusCard(t, e, "Craw Wurm", 0, state.ZBattlefield)
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{wurm}})
		if !e.G.Obj(wurm).IsAttacking {
			t.Fatal("precondition: Craw Wurm is not attacking")
		}
		lossAttach(t, e, "Imprisoned in the Moon", 1, wurm)
		e.checkStateBased()
		if e.IsCreature(wurm) {
			t.Fatal("precondition: the enchanted Wurm is still a creature")
		}
		if e.G.Obj(wurm).IsAttacking {
			t.Fatal("an attacking creature that stopped being a creature must be removed from combat (CR 506.4)")
		}
		if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
			t.Fatalf("log-only replay differs:\n%s", diff)
		}
	})
	t.Run("blocker", func(t *testing.T) {
		t.Parallel()
		e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Craw Wurm"), lookup(t, reg, "Imprisoned in the Moon")},
			[]*cards.Card{lookup(t, reg, "Grizzly Bears")})
		wurm := moveCorpusCard(t, e, "Craw Wurm", 0, state.ZBattlefield)
		bears := moveCorpusCard(t, e, "Grizzly Bears", 1, state.ZBattlefield)
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{wurm}})
		e.emit(events.Event{Kind: events.DeclareBlockers, Player: 1, Pairs: [][2]state.ObjID{{wurm, bears}}})
		lossAttach(t, e, "Imprisoned in the Moon", 0, bears)
		e.checkStateBased()
		for _, b := range e.G.Obj(wurm).BlockedBy {
			if b == bears {
				t.Fatal("a blocking creature that stopped being a creature must be removed from combat (CR 506.4)")
			}
		}
		if !e.G.Obj(wurm).IsAttacking || len(e.G.Obj(wurm).BlockedBy) == 0 {
			t.Fatalf("the attacker stays attacking and blocked (CR 509.1h): %+v", e.G.Obj(wurm).BlockedBy)
		}
		if diff := diffGames(e.G, replayFromLog(t, cfg, e.L.Events)); diff != "" {
			t.Fatalf("log-only replay differs:\n%s", diff)
		}
	})
}
