package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestNihilSpellbombExilesOnlyTargetedGraveyard pins the real corpus card's
// "exile all cards from target player's graveyard" on an ACTIVATED
// ChangeZoneAll: both seats start with a card in their graveyard, the ability
// targets seat 1, and only seat 1's graveyard may be exiled. Before the
// ChangeZoneAll player-scope fix, effChangeZoneAll walked g.AliveFrom(0) and
// exiled seat 0's graveyard too.
func TestNihilSpellbombExilesOnlyTargetedGraveyard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bomb := corpusCard(t, "Nihil Spellbomb")
	mtn := corpusCard(t, "Mountain")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{bomb}, nil)

	id := moveSeededCard(t, e, 0, bomb, state.ZBattlefield)
	mine := moveSeededCard(t, e, 0, mtn, state.ZGraveyard)
	theirs := moveSeededCard(t, e, 1, mtn, state.ZGraveyard)
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 1 {
		t.Fatalf("seat 0 graveyard setup = %d cards, want 1", got)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 1)); got != 1 {
		t.Fatalf("seat 1 graveyard setup = %d cards, want 1", got)
	}
	// moveSeededCard clears the pending priority decision; re-ask.
	e.Advance()

	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d != nil && (d.Kind == decision.KTarget || d.Kind == decision.KChoose) {
		idx := indexOfPlayerOption(d, 1)
		if idx < 0 {
			t.Fatalf("no option targeting seat 1: %+v", d.Options)
		}
		submitChoices(t, e, idx)
	}
	// Nihil Spellbomb sacrifices itself as the activation cost, so its "you
	// may pay {B} to draw" dies trigger fires above the ability; decline it
	// while draining (passUntilStackEmpty fatals on any non-priority ask).
	for n := 0; n < 20 && !e.G.Over && len(e.G.Stack) > 0; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack depth %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KTriggerOptional {
			submitChoices(t, e, 1) // decline the {B} draw
			continue
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected %s decision while draining: %+v", d.Kind, d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		submitChoices(t, e, idx)
	}

	if got := len(e.G.Zone(state.ZGraveyard, 1)); got != 0 {
		t.Fatalf("targeted seat 1 graveyard not exiled: %d cards left", got)
	}
	if o := e.G.Obj(theirs); o == nil || o.Zone != state.ZExile {
		t.Fatalf("seat 1's graveyard card not exiled: %+v", o)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 2 {
		// seat 0's graveyard holds `mine` plus the Spellbomb sacrificed as
		// the activation cost; anything less means `mine` was swept.
		t.Fatalf("seat 0 graveyard = %d cards, want 2 (mine + sacrificed Spellbomb)", got)
	}
	if o := e.G.Obj(mine); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("seat 0's graveyard card moved: %+v", o)
	}
	replayCheck(t, e, cfg)
}
