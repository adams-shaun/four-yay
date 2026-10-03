package rules

// The resolution-time target set survives a mid-resolution suspension
// (spike S3, legacy defect 2).
//
// resolveTop does not resolve against the stack object's recorded Targets
// as-is: an overloaded spell's "each" census (CR 702.96b) is taken as it
// resolves, and CR 608.2b drops the targets that became illegal. A chain that
// then suspends re-enters through resumeResolution, which rebuilt its Ctx
// from o.Targets -- empty for an overloaded spell, unfiltered for a partly
// illegal one. Overloaded Mind Rake's per-player TgtChoose discard therefore
// resumed over NO acting players: the first player's answered discard was
// dropped and the second player was never asked (5 of the 10k dual-run fuzz
// divergences).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestOverloadedDiscardAsksEachPlayerAcrossResume casts the real Mind Rake
// (corpus) for its overload cost: each player discards two cards, each
// choosing from their own hand.
func TestOverloadedDiscardAsksEachPlayerAcrossResume(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 7311, []string{"Mind Rake"}, nil, nil)
	id := findCardObj(t, e, 0, "Mind Rake", state.ZHand)
	addMana(t, e, 0, "BB")
	submitChoices(t, e, castModeOption(t, e, id, "overloaded"))
	d := passUntilNonPriority(t, e, 20)
	var picked []state.ObjID
	for seat := state.PlayerID(0); seat < 2; seat++ {
		if d == nil || d.Kind != decision.KModes || d.ResumeKind != "discard" || d.Player != seat {
			t.Fatalf("pending = %+v, want seat %d's two-card TgtChoose discard pick", d, seat)
		}
		if d.Min != 2 || d.Max != 2 {
			t.Fatalf("seat %d pick bounds %d..%d, want 2..2", seat, d.Min, d.Max)
		}
		n := len(d.Options)
		picked = append(picked, d.Options[n-1].Obj, d.Options[n-2].Obj)
		submitChoices(t, e, d.Options[n-1].Index, d.Options[n-2].Index)
		d = e.Pending()
	}
	passUntilStackEmpty(t, e, 20)
	for _, c := range picked {
		if z := e.G.Obj(c).Zone; z != state.ZGraveyard {
			t.Fatalf("chosen card %d zone = %s, want graveyard (an answered discard was dropped)", c, z)
		}
	}
	if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
		t.Fatalf("Mind Rake zone = %s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestIllegalTargetDroppedBeforeAskStaysDroppedOnResume is the CR 608.2b
// member of the same class: a two-player TgtChoose discard whose first target
// gained shroud while the spell was on the stack resolves against the second
// target alone. Its answered pick must be applied to that target, and the
// resume must not walk the dropped target back in (pre-fix: the answer was
// consumed at the shrouded seat's cursor, which held none of the chosen
// cards, and the legal target was asked a second time).
func TestIllegalTargetDroppedBeforeAskStaysDroppedOnResume(t *testing.T) {
	t.Parallel()
	rot := "Name:Test Twin Rot\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Discard | ValidTgts$ Player | TargetMin$ 2 | TargetMax$ 2 | TargetUnique$ True | NumCards$ 1 | Mode$ TgtChoose\nOracle:x\n"
	mask := "Name:Test Shroud Mask\nManaCost:2\nTypes:Enchantment\n" +
		"S:Mode$ Continuous | Affected$ You | AddKeyword$ Shroud\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 7312, rot, mask)
	addMana(t, e, 0, "B")
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("no cast option for the fixture: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the two-player target ask", d)
	}
	var picks []int
	for _, o := range d.Options {
		if o.Kind == "player" {
			picks = append(picks, o.Index)
		}
	}
	if len(picks) != 2 {
		t.Fatalf("target options = %+v, want both seats", d.Options)
	}
	submitChoices(t, e, picks...)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack || len(o.Targets) != 2 {
		t.Fatalf("precondition: the spell must be on the stack targeting both seats: %+v", o)
	}
	// Seat 0 gains shroud in response: CR 608.2b drops it at resolution.
	maskID := moveSeeded(t, e, 0, mask, state.ZBattlefield)
	e.priorityRound()
	wantPlayerKeyword(t, e, 0, "Shroud")
	if e.G.Obj(maskID).Zone != state.ZBattlefield {
		t.Fatal("precondition: the shroud mask did not reach the battlefield")
	}
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "discard" || d.Player != 1 {
		t.Fatalf("pending = %+v, want the legal target seat 1's discard pick", d)
	}
	pick := d.Options[len(d.Options)-1].Obj
	submitChoices(t, e, d.Options[len(d.Options)-1].Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KModes {
		t.Fatalf("a second discard pick was posed after the legal target answered: %+v", d)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(pick).Zone; z != state.ZGraveyard {
		t.Fatalf("seat 1's chosen card zone = %s, want graveyard", z)
	}
	replayCheck(t, e, cfg)
}

// TestCharmModeTargetDroppedBeforeAskStaysDroppedOnResume is the per-mode
// member: CR 608.2b rechecks each chosen mode's targets independently, and a
// mode whose only target became illegal does nothing. When an EARLIER mode
// suspends on a discard pick, the later mode runs from a charm_rest frame
// after the resume, and it must still see its rechecked (empty) group rather
// than the raw announced target.
func TestCharmModeTargetDroppedBeforeAskStaysDroppedOnResume(t *testing.T) {
	t.Parallel()
	charm := "Name:Test Rot Charm\nManaCost:B\nTypes:Sorcery\n" +
		"A:SP$ Charm | CharmNum$ 2 | Choices$ Rot,Drain\n" +
		"SVar:Rot:DB$ Discard | ValidTgts$ Player | NumCards$ 1 | Mode$ TgtChoose | SpellDescription$ Target player discards a card.\n" +
		"SVar:Drain:DB$ LoseLife | ValidTgts$ Player | LifeAmount$ 2 | SpellDescription$ Target player loses 2 life.\n" +
		"Oracle:x\n"
	mask := "Name:Test Shroud Mask\nManaCost:2\nTypes:Enchantment\n" +
		"S:Mode$ Continuous | Affected$ You | AddKeyword$ Shroud\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 7313, charm, mask)
	addMana(t, e, 0, "B")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the mode announcement", d)
	}
	submitChoices(t, e, 0, 1)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the per-mode target ask", d)
	}
	rot, drain := -1, -1
	for _, opt := range d.Options {
		if opt.Group == "charm-mode-0" && opt.Kind == "player" && opt.Player == 1 {
			rot = opt.Index
		}
		if opt.Group == "charm-mode-1" && opt.Kind == "player" && opt.Player == 0 {
			drain = opt.Index
		}
	}
	if rot < 0 || drain < 0 {
		t.Fatalf("per-mode player targets not offered: %+v", d.Options)
	}
	submitChoices(t, e, rot, drain)
	moveSeeded(t, e, 0, mask, state.ZBattlefield)
	e.priorityRound()
	wantPlayerKeyword(t, e, 0, "Shroud")
	life0 := e.G.Players[0].Life
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "discard" || d.Player != 1 {
		t.Fatalf("pending = %+v, want seat 1's discard pick from the Rot mode", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != life0 {
		t.Fatalf("seat 0 life %d -> %d: the Drain mode hit its CR 608.2b-dropped target after the resume", life0, got)
	}
	replayCheck(t, e, cfg)
}
