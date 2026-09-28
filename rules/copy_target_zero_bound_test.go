package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestAvacynsJudgmentTargetBoundCountsPlayersAndPermanents pins the real
// Avacyn's Judgment's "any number of targets" bound. Its TargetMax$ names
// SVar:MaxTgts:PlayerCountPlayers$Amount/Plus.MaxPermanents -- a Count$-less
// PlayerCount body with an /Op suffix. The bare-body branch of the count
// evaluator never cut that suffix, so the body read as an unmodelled zero,
// NumResolved reported the zero as resolved, and the spell could never
// target anything; a copy of it (Increasing Vengeance, fuzz batch4 lines 3
// and 8) then posed a Min 0 Max 0 target ask and panicked the engine.
func TestAvacynsJudgmentTargetBoundCountsPlayersAndPermanents(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	eng, cfg := miscHandsEngine(t, reg,
		[]string{"Avacyn's Judgment"}, nil, nil, []string{"Grizzly Bears"})
	addMana(t, eng, 0, "RR")
	bear := miscBoardObj(t, eng, 1, "Grizzly Bears")
	card := miscHandObj(t, eng, 0, "Avacyn's Judgment")
	submitChoices(t, eng, miscCastOption(t, eng, card))
	d := eng.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Avacyn's Judgment's target ask, got %+v", d)
	}
	// Two living players plus at least the bear: the bound must admit every
	// player and permanent, never the zero the unsplit /Plus suffix read.
	if d.Min != 0 || d.Max < 3 {
		t.Fatalf("Avacyn's Judgment target bounds = [%d,%d], want [0, >=3]", d.Min, d.Max)
	}
	bearOpt := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			bearOpt = o.Index
		}
	}
	if bearOpt < 0 {
		t.Fatalf("Grizzly Bears not offered: %+v", d.Options)
	}
	submitChoices(t, eng, bearOpt)
	if got := eng.G.Obj(card).Targets; len(got) != 1 || got[0].Obj != bear {
		t.Fatalf("Avacyn's Judgment targets = %+v, want the Grizzly Bears", got)
	}
	// (The damage amount, SVar:Y:Count$Madness.X.2, reads an unmodelled
	// Count$Madness head -- a separate gap -- so this pins the bound and a
	// clean resolution, not the bear's death.)
	drainToEnd(t, eng, 40)
	if len(eng.G.Stack) != 0 {
		t.Fatalf("stack did not drain: %v", eng.G.Stack)
	}
	replayCheck(t, eng, cfg)
}

// TestCopyWithZeroTargetBoundResolvesWithoutAsking pins the class behind the
// same panic: a copy whose own declaration resolves to Min 0 Max 0 (Crackle
// with Power cast for X=0 -- "up to X targets" -- copied by the real
// Increasing Vengeance, MayChooseTarget$ True) has nothing to choose.
// AskCopyTargets used to post that decision anyway, which Engine.ask refuses
// with a panic; it must resolve the empty election silently and let the
// copy resolve.
func TestCopyWithZeroTargetBoundResolvesWithoutAsking(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	eng, cfg := miscHandsEngine(t, reg,
		[]string{"Crackle with Power", "Increasing Vengeance"}, nil, nil, []string{"Grizzly Bears"})
	addMana(t, eng, 0, "RRRR")
	crackle := miscHandObj(t, eng, 0, "Crackle with Power")
	submitChoices(t, eng, miscCastOption(t, eng, crackle))
	// Announce X = 0 and answer whatever the cast still asks with the empty
	// or first option until priority returns.
	for i := 0; i < 5; i++ {
		d := eng.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		switch {
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "x":
			submitChoices(t, eng, 0) // X = 0
		case d.Kind == decision.KTarget && d.Min == 0:
			submitChoices(t, eng)
		default:
			t.Fatalf("unexpected cast decision %+v", d)
		}
	}
	if o := eng.G.Obj(crackle); o.Zone != state.ZStack || o.X != 0 {
		t.Fatalf("Crackle with Power zone %s X %d, want on the stack with X=0", o.Zone, o.X)
	}
	vengeance := miscHandObj(t, eng, 0, "Increasing Vengeance")
	submitChoices(t, eng, miscCastOption(t, eng, vengeance))
	if d := eng.Pending(); d != nil && d.Kind == decision.KTarget {
		idx := -1
		for _, o := range d.Options {
			if o.Obj == crackle {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("Increasing Vengeance did not offer Crackle with Power: %+v", d.Options)
		}
		submitChoices(t, eng, idx)
	}
	for i := 0; i < 60 && !eng.G.Over && len(eng.G.Stack) > 0; i++ {
		d := eng.Pending()
		if d == nil {
			t.Fatal("no decision pending while the stack is non-empty")
		}
		if d.ResumeKind == "copy_targets" {
			t.Fatalf("a Max 0 copy-target ask was posted: %+v", d)
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision while resolving: %+v", d)
		}
		miscPass(t, eng)
	}
	if len(eng.G.Stack) != 0 {
		t.Fatalf("stack did not drain: %v", eng.G.Stack)
	}
	if z := eng.G.Obj(crackle).Zone; z != state.ZGraveyard {
		t.Fatalf("Crackle with Power in %s, want graveyard", z)
	}
	replayCheck(t, eng, cfg)
}
