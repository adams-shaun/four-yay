package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestNestedCharmDoesNotInheritOuterModes pins the fx41 defect: a Charm whose
// chosen mode's sub-chain reaches a SECOND Charm must pose its own modal ask.
//
// THE SHAPE IS CORPUS-UNREACHABLE TODAY. A walker over the compiled corpus
// (cards.OpenCorpus on .cards/ir.gob.gz at the branch under test) counted
// 797 distinct compiled Charm SAs; resolving every Charm's Choices$ name
// through cards.ResolveSVar and walking each returned sub-chain, ZERO Charm
// SAs reach a second Charm. So this is a latent landmine, not a live defect:
// the engine authors already knew it (the depth-3 test in
// resumption_after_ask_test.go deliberately used a Repeat, not a second Charm,
// as its two-level carrier, because "a Charm invoked AS a mode inherits that
// Modes value and re-resolves itself endlessly"). The fixture below
// reproduces the shape so a future card or script change cannot re-open it
// silently.
//
// The card: an outer Charm whose own SubAbility$ is a second Charm.
//
//	A:SP$ Charm | Choices$ DoGain,DoLose | SubAbility$ InnerCharm
//
// On the outer Charm's resume pass, Ctx.Modes is set to the chosen outer
// mode's SVar name. The bug is that effCharm hands the SAME Ctx (with Modes
// still set) to every mode it runs AND to the Charm's own SubAbility$, so
// the inner Charm below reads c.Modes != nil, takes the re-entry branch, and
// "runs" the OUTER mode names against the INNER Charm's SVar table instead of
// posing its own choice. Here DoGain resolves (inside the inner Charm's
// table) to the same GainLife 5 SA, so the caster gains 5 a second time and
// the inner modal ask never fires. Correct resolution: the outer mode runs
// once (+5), then the inner Charm poses its own KModes decision.
func TestNestedCharmDoesNotInheritOuterModes(t *testing.T) {
	t.Parallel()
	charm := "Name:PiNest\nManaCost:R\nTypes:Instant\n" +
		"A:SP$ Charm | Choices$ DoGain,DoLose | SubAbility$ InnerCharm\n" +
		"SVar:DoGain:DB$ GainLife | Defined$ You | LifeAmount$ 5 | SpellDescription$ Gain 5 life\n" +
		"SVar:DoLose:DB$ LoseLife | Defined$ You | LifeAmount$ 1 | SpellDescription$ Lose 1 life\n" +
		"SVar:InnerCharm:DB$ Charm | Choices$ LoseOne,LoseTwo\n" +
		"SVar:LoseOne:DB$ LoseLife | Defined$ You | LifeAmount$ 1 | SpellDescription$ Lose 1 life\n" +
		"SVar:LoseTwo:DB$ LoseLife | Defined$ You | LifeAmount$ 2 | SpellDescription$ Lose 2 life\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 101, charm)
	addMana(t, e, 0, "R")
	life0 := e.G.Players[0].Life

	// Pass 1: the outer Charm asks for a mode.
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the OUTER Charm KModes ask, got %+v", d)
	}
	if len(d.Options) != 2 || d.Options[0].Label != "Gain 5 life" || d.Options[1].Label != "Lose 1 life" {
		t.Fatalf("outer mode options: %+v", d.Options)
	}

	// Pick DoGain (index 0). The chosen mode runs (+5) and then, because the
	// outer Charm's SubAbility$ is the inner Charm, the inner Charm must
	// pose its OWN modal choice. With the defect, no such ask appears: the
	// inner Charm inherits the outer's answered modes and silently re-runs
	// DoGain instead.
	submitChoices(t, e, 0)
	// The outer choice is now a cast-time announcement. Pass priority so the
	// spell starts resolving and reaches the genuinely mid-resolution inner
	// Charm ask.
	d = passUntilNonPriority(t, e, 20)

	// The inner Charm must now be asking, mid-resolution, suspended with the
	// spell still on the stack, its own options presented.
	if got := e.G.Players[0].Life; got != life0+5 {
		t.Fatalf("life after the chosen outer mode = %d, want %d (the mode must run exactly once)", got, life0+5)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected the INNER Charm's own KModes ask after the outer mode, got %+v", d)
	}
	if len(d.Options) != 2 || d.Options[0].Label != "Lose 1 life" || d.Options[1].Label != "Lose 2 life" {
		t.Fatalf("inner mode options: %+v (the inner Charm must present ITS OWN options, not inherit the outer's)", d.Options)
	}
	if o := e.G.Obj(id); o.Zone != state.ZStack {
		t.Fatalf("the inner Charm ask must suspend with the spell on the stack, zone %s", o.Zone)
	}

	// Answer the inner Charm and drain the stack.
	submitChoices(t, e, 0) // Lose 1 life
	passUntilStackEmpty(t, e, 20)
	if e.G.Players[0].Life != life0+5-1 {
		t.Fatalf("life = %d, want %d (outer DoGain +5 once, inner LoseOne -1 once)", e.G.Players[0].Life, life0+5-1)
	}
	if !hasEventKind(e, events.ModeChosen) {
		t.Fatal("no ModeChosen recorded the inner modal pick")
	}
	replayCheck(t, e, cfg)
}
