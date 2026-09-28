package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// auditRelicSrc is an inline vanilla artifact with a known mana value, used as
// the sacrifice fodder for a creature mana ability's Sac<1/Artifact> cost.
// Authored inline (never a Forge .txt: the GPL licensing rule).
const auditRelicSrc = "Name:Audit Relic\nManaCost:3\nTypes:Artifact\nOracle:x\n"

// TestSacrificeValueManaAbilityProducesManaValue is the reported defect: a
// creature mana ability whose Amount$ is an SVar over the object it
// sacrifices (Priest of Yawgmoth's `SVar:X:Sacrificed$CardManaCost`,
// Produced$ B) paid the sacrifice but added ZERO mana, because the plain
// (non-choice) production path resolved Amount$ in a Ctx that never carried
// the sacrificed LKI. Sacrificing a mana-value-3 artifact must add {B}{B}{B}.
//
// The census (rules/autopay_census_test.go) measured this family as
// `no_mana`: Priest of Yawgmoth, Red Priest of Yawgmoth, Soldevi Adnate,
// Slobad, Illuminor Szeras and Furgul Quag Nurturer all carry a
// sacrifice-value Amount$ and all produced zero on a board with fodder.
func TestSacrificeValueManaAbilityProducesManaValue(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	priest := corpusCommander(t, reg, "Priest of Yawgmoth")
	relic := card(t, auditRelicSrc)
	e, cfg := eachColorGame(t, 401, []*cards.Card{priest, relic})
	pid := moveToBattlefieldByName(t, e, 0, "Priest of Yawgmoth")
	rid := moveToBattlefieldByName(t, e, 0, "Audit Relic")
	// PRECONDITION: the source is an untapped battlefield creature and the
	// fodder is a distinct untapped artifact, so the {T} tap and the
	// Sac<1/Artifact> cost can both be paid.
	if o := e.G.Obj(pid); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("precondition: Priest = %+v, want an untapped battlefield creature", o)
	}
	if o := e.G.Obj(rid); o == nil || o.Zone != state.ZBattlefield || o.Tapped || rid == pid {
		t.Fatalf("precondition: relic = %+v, want a distinct untapped battlefield artifact", o)
	}
	// CR 302.6: the Priest entered this turn, so clear summoning sickness with
	// a logged TurnChange (the tapForManaAfterTurnClears discipline) rather
	// than a raw field write a replay could not reconstruct.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.priorityRound()

	activateMana(t, e, pid)
	// The sacrifice cost may pose its object choice before the effect runs.
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		submitChoices(t, e, sacrificeOption(t, d, rid))
	}
	if o := e.G.Obj(rid); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("sacrificed relic zone = %v, want graveyard (cost not paid)", o.Zone)
	}
	// The sacrificed artifact's mana value was 3, so the ability adds three
	// black mana. Pre-fix this is 0 (the amount SVar resolved without the
	// sacrificed LKI).
	if got := e.G.Players[0].Pool[state.MB]; got != 3 {
		t.Fatalf("Priest produced B=%d, want 3 (mana value of the sacrificed artifact)", got)
	}
	replayCheck(t, e, cfg)
}
