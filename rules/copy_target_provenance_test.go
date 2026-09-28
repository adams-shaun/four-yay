package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A target that has left the battlefield must retain its CAST declaration,
// rather than being attributed to the last half by a fresh legality check.
func TestCopyFusedIllegalInheritedTargetKeepsDeclaration(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	eng, _ := miscHandsEngine(t, reg, []string{"Wear"}, nil,
		[]string{"Mirrorpool"}, []string{"Sol Ring", "Ghostly Prison"})
	pool := miscBoardObj(t, eng, 0, "Mirrorpool")
	eng.emit(events.Event{Kind: events.Untap, Obj: pool})
	eng.priorityRound()
	addMana(t, eng, 0, "RWCCCG")
	wear := miscHandObj(t, eng, 0, "Wear")
	submitChoices(t, eng, splitOption(t, eng, wear, "fuse").Index)
	ring := miscBoardObj(t, eng, 1, "Sol Ring")
	prison := miscBoardObj(t, eng, 1, "Ghostly Prison")
	choose := func(id state.ObjID) {
		t.Helper()
		d := eng.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("target declaration missing: %+v", d)
		}
		for _, opt := range d.Options {
			if opt.Obj == id {
				submitChoices(t, eng, opt.Index)
				return
			}
		}
		t.Fatalf("target %d missing from %+v", id, d.Options)
	}
	choose(ring)
	choose(prison)
	if z := eng.G.Obj(wear).Zone; z != state.ZStack {
		t.Fatalf("fused original zone %s, want stack", z)
	}
	if len(eng.fuseTargets[wear]) != 2 || len(eng.fuseTargets[wear][0]) != 1 || len(eng.fuseTargets[wear][1]) != 1 {
		t.Fatalf("setup lacks two original declarations: %+v", eng.fuseTargets[wear])
	}
	eng.emit(events.Event{Kind: events.MoveZone, Obj: ring, From: state.ZBattlefield, To: state.ZGraveyard})
	if eng.G.Obj(ring).Zone != state.ZGraveyard || eng.G.Obj(prison).Zone != state.ZBattlefield {
		t.Fatalf("setup target zones wrong: ring=%s prison=%s", eng.G.Obj(ring).Zone, eng.G.Obj(prison).Zone)
	}
	eng.priorityRound()
	submitChoices(t, eng, abilityOption(t, eng, pool, 1).Index)
	choose(wear)
	d := passUntilNonPriority(t, eng, 30)
	if d == nil || d.ResumeKind != "copy_targets" || d.Source == wear || len(d.Options) == 0 || d.Options[0].Obj != ring {
		t.Fatalf("first copy declaration did not keep illegal inherited artifact: %+v", d)
	}
	copyID := d.Source
	if co := eng.G.Obj(copyID); co == nil || !co.IsCopy || co.Zone != state.ZStack || len(co.Targets) != 2 {
		t.Fatalf("copy precondition failed: %+v", co)
	}
	submitChoices(t, eng, 0)
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.ResumeKind != "copy_targets" || len(d.Options) == 0 || d.Options[0].Obj != prison {
		t.Fatalf("second copy declaration did not keep enchantment: %+v", d)
	}
	for _, opt := range d.Options {
		if opt.Obj == ring {
			t.Fatalf("illegal artifact assigned to Tear: %+v", d.Options)
		}
	}
	// The inherited split is present while both choices are still pending;
	// it is discarded when the copy leaves the stack after resolution.
	stages := eng.fuseTargets[copyID]
	if len(stages) != 2 || len(stages[0]) != 1 || stages[0][0].Obj != ring || len(stages[1]) != 1 || stages[1][0].Obj != prison {
		t.Fatalf("inherited declaration attribution = %+v, want artifact then enchantment", stages)
	}
	submitChoices(t, eng, 0)
}

func TestCopyCharmAsksEveryChosenTargetMode(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	card := searchCorpusCard(t, reg, "Winterflame")
	sa := card.Faces[0].SpellAbility()
	if sa == nil || sa.API != "Charm" || sa.Params["CharmNum"] != "2" ||
		len(copyCharmModes(card.Faces[0], sa, []string{"DBTap", "DBDmg"})) != 2 {
		t.Fatalf("Winterflame does not declare two distinct target modes: %+v", sa)
	}
	eng, _ := miscHandsEngine(t, reg, []string{"Winterflame"}, nil, []string{"Mirrorpool"}, []string{"Grizzly Bears", "Elvish Mystic"})
	pool := miscBoardObj(t, eng, 0, "Mirrorpool")
	eng.emit(events.Event{Kind: events.Untap, Obj: pool})
	eng.priorityRound()
	addMana(t, eng, 0, "URCCCG")
	spell := miscHandObj(t, eng, 0, "Winterflame")
	submitChoices(t, eng, miscCastOption(t, eng, spell))
	d := eng.Pending()
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("expected modal cast decision, got %+v", d)
	}
	var picks []int
	for _, opt := range d.Options {
		if opt.Label == "Tap target creature." || opt.Label == "CARDNAME deals 2 damage to target creature." {
			picks = append(picks, opt.Index)
		}
	}
	if len(picks) != 2 {
		t.Fatalf("expected both chosen modes, got %+v", d.Options)
	}
	submitChoices(t, eng, picks...)
	bear := miscBoardObj(t, eng, 1, "Grizzly Bears")
	mystic := miscBoardObj(t, eng, 1, "Elvish Mystic")
	if bear == mystic || eng.G.Obj(bear).Zone != state.ZBattlefield || eng.G.Obj(mystic).Zone != state.ZBattlefield {
		t.Fatalf("precondition: distinct creature targets must be on battlefield: bear=%d mystic=%d", bear, mystic)
	}
	d = eng.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("cast per-mode target ask missing: %+v", d)
	}
	first, second := -1, -1
	for _, opt := range d.Options {
		if opt.Group == "charm-mode-0" && opt.Obj == bear {
			first = opt.Index
		}
		if opt.Group == "charm-mode-1" && opt.Obj == mystic {
			second = opt.Index
		}
	}
	if first < 0 || second < 0 || first == second {
		t.Fatalf("cast target pair absent: %+v", d.Options)
	}
	submitChoices(t, eng, first, second)
	if o := eng.G.Obj(spell); o == nil || o.Zone != state.ZStack || len(o.ChosenModes) != 2 {
		t.Fatalf("modal cast precondition failed: %+v", o)
	}
	submitChoices(t, eng, abilityOption(t, eng, pool, 1).Index)
	d = eng.Pending()
	found := -1
	if d != nil {
		for _, opt := range d.Options {
			if opt.Obj == spell {
				found = opt.Index
			}
		}
	}
	if found < 0 {
		t.Fatalf("copy ability did not offer Winterflame: %+v", d)
	}
	submitChoices(t, eng, found)
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.ResumeKind != "copy_targets" || d.Options[0].Obj != bear {
		t.Fatalf("first modal copy target missing: %+v", d)
	}
	submitChoices(t, eng, 0)
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.ResumeKind != "copy_targets" || d.Source == spell || d.ResumeSA == nil || d.ResumeSA.Line != copyCharmModes(card.Faces[0], sa, []string{"DBTap", "DBDmg"})[1].Line || d.Options[0].Obj != mystic {
		t.Fatalf("second modal copy target declaration missing: %+v", d)
	}
}
