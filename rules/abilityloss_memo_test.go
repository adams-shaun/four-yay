package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// lossMemoHas reports whether abilityLoss's memo holds a live entry for id.
func lossMemoHas(e *Engine, id state.ObjID) bool {
	m := &e.lossMemo
	return int(id) < len(m.ents) && m.gen != 0 && m.ents[id].gen == m.gen && m.seq == e.activeBuildSeq
}

// The per-build ability-loss memo tracks the board: Witness Protection
// moved from one creature to the other (the remover list's content is the
// same, only the attachment changed), then off the battlefield, flips each
// creature's answer, and every answer after each step is both served from
// the memo and equal to a fresh computation.
func TestAbilityLossMemoTracksTheBoard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Elvish Archdruid"), lookup(t, reg, "Llanowar Elves"),
		lookup(t, reg, "Witness Protection")}, nil)
	lord := moveCorpusCard(t, e, "Elvish Archdruid", 0, state.ZBattlefield)
	elf := moveCorpusCard(t, e, "Llanowar Elves", 0, state.ZBattlefield)
	lossNextTurn(e)

	check := func(step string, wantLord, wantElf bool) {
		t.Helper()
		for _, c := range []struct {
			id   state.ObjID
			want bool
		}{{lord, wantLord}, {elf, wantElf}} {
			o := e.G.Obj(c.id)
			_, first := e.abilityLoss(o)
			ts, lost := e.abilityLoss(o)
			if first != lost || lost != c.want {
				t.Errorf("%s: %s lost=%v then %v, want %v", step, o.Face().Name, first, lost, c.want)
			}
			if !c.want {
				// No remover at all (or one that misses) still answers;
				// only a board with a remover reaches the memo.
				if e.activeSummaryOf(e.active()).hasRemoveAbilities && !lossMemoHas(e, c.id) {
					t.Errorf("%s: %s's answer was not memoized", step, o.Face().Name)
				}
				continue
			}
			if !lossMemoHas(e, c.id) {
				t.Errorf("%s: %s's answer was not memoized", step, o.Face().Name)
			}
			e.lossMemo.gen++ // retire every entry: the next read recomputes
			fts, flost := e.abilityLoss(o)
			if fts != ts || flost != lost {
				t.Errorf("%s: %s memo (%d, %v) != fresh (%d, %v)", step, o.Face().Name, ts, lost, fts, flost)
			}
			if lossOffers(t, e, c.id) {
				t.Errorf("%s: %s offers a mana ability while it has lost all abilities", step, o.Face().Name)
			}
		}
	}

	check("before the Aura", false, false)
	aura := lossAttach(t, e, "Witness Protection", 0, lord)
	check("on the Archdruid", true, false)
	// Re-attach: the same remover entry, a different recipient.
	e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{elf}})
	e.pending = nil
	e.Advance()
	if got := e.G.Obj(aura).AttachedTo; got != elf {
		t.Fatalf("Witness Protection is attached to %d, want the Elves %d", got, elf)
	}
	check("moved to the Elves", false, true)
	if !lossOffers(t, e, lord) {
		t.Error("the Archdruid did not get its mana ability back when the Aura moved")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: aura, From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.Advance()
	check("Aura gone", false, false)
	if !lossOffers(t, e, lord) || !lossOffers(t, e, elf) {
		t.Error("an Elf did not get its mana ability back with the Aura gone")
	}
	replayCheck(t, e, cfg)
}

// A layer-inert run (priority bookkeeping) keeps the memo's generation: the
// answer is served, not recomputed, across it.
func TestAbilityLossMemoSurvivesLayerInertEvents(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Elvish Archdruid"), lookup(t, reg, "Witness Protection")}, nil)
	lord := moveCorpusCard(t, e, "Elvish Archdruid", 0, state.ZBattlefield)
	lossNextTurn(e)
	lossAttach(t, e, "Witness Protection", 0, lord)
	if _, lost := e.abilityLoss(e.G.Obj(lord)); !lost {
		t.Fatal("precondition: the enchanted Archdruid has lost all abilities")
	}
	gen := e.lossMemo.gen
	e.emit(events.Event{Kind: events.Priority, Player: 0})
	if _, lost := e.abilityLoss(e.G.Obj(lord)); !lost {
		t.Fatal("the Archdruid regained its abilities across a priority event")
	}
	if e.lossMemo.gen != gen || !lossMemoHas(e, lord) {
		t.Errorf("a layer-inert event moved the memo generation (%d -> %d)", gen, e.lossMemo.gen)
	}
}
