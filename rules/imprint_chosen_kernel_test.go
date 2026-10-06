package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func TestUnstableGlyphbridgeSparesChosenCreatures(t *testing.T) {
	t.Parallel()
	glyph := kr0Corpus(t, "Unstable Glyphbridge")
	e := kr0Engine(t, 2)
	source := kr0Place(t, e, 0, glyph, state.ZBattlefield)
	creatures := [2][2]state.ObjID{}
	for p, specs := range [2][2][2]string{{{"Glyph Small Zero", "2/2"}, {"Glyph Large Zero", "4/4"}}, {{"Glyph Small One", "2/2"}, {"Glyph Large One", "5/5"}}} {
		for i, spec := range specs {
			creatures[p][i] = kr0Src(t, e, state.PlayerID(p), "Name:"+spec[0]+"\nTypes:Creature\nPT:"+spec[1]+"\nOracle:x\n", state.ZBattlefield)
		}
	}
	for p := range creatures {
		for _, id := range creatures[p] {
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition creature %d not on battlefield: %+v", id, o)
			}
		}
		if e.Power(creatures[p][0]) > 2 || e.Power(creatures[p][1]) <= 2 {
			t.Fatalf("precondition seat %d powers small/large=%d/%d, want <=2/>2", p, e.Power(creatures[p][0]), e.Power(creatures[p][1]))
		}
	}
	if e.G.Obj(source).Zone != state.ZBattlefield {
		t.Fatalf("precondition Glyphbridge source zone=%s", e.G.Obj(source).Zone)
	}
	sa := kr0SVar(t, glyph, "TrigRepeat")
	mk := func() *effects.Ctx { return &effects.Ctx{Source: source, Controller: 0, SVars: glyph.Faces[0].SVars} }
	var last *effects.Ctx
	d := kr0Run(t, e, sa, mk, &last)
	for p := range creatures {
		if d == nil || d.Player != 0 || d.Min != 1 || d.Max != 1 {
			t.Fatalf("choice for player %d = %+v, want controller's mandatory single choice", p, d)
		}
		if len(d.Options) != 1 || d.Options[0].Obj != creatures[p][0] {
			t.Fatalf("choice for player %d offered %+v, want only qualifying creature %d", p, d.Options, creatures[p][0])
		}
		d = kr0Answer(t, e, kr0Opt(t, d, creatures[p][0]))
	}
	if d != nil {
		t.Fatalf("resolution left decision pending: %+v", d)
	}
	for p := range creatures {
		if got := e.G.Obj(creatures[p][0]).Zone; got != state.ZBattlefield {
			t.Errorf("chosen seat %d creature zone=%s, want battlefield", p, got)
		}
		if got := e.G.Obj(creatures[p][1]).Zone; got != state.ZGraveyard {
			t.Errorf("unchosen seat %d creature zone=%s, want graveyard", p, got)
		}
	}
	if src := e.G.Obj(source); src == nil || len(src.SeekFound) != 0 {
		t.Fatalf("DBCleanup left SeekFound association: %+v", src)
	}
	if last == nil {
		t.Fatal("resolution produced no final context")
	}
}

func TestImprintChosenDefinedImprintedReadsBattlefieldPick(t *testing.T) {
	t.Parallel()
	e := kr0Engine(t, 2)
	source := kr0Src(t, e, 0, "Name:Imprint Source\nTypes:Artifact\nOracle:x\n", state.ZBattlefield)
	creature := kr0Src(t, e, 0, "Name:Imprint Target\nTypes:Creature\nPT:2/2\nOracle:x\n", state.ZBattlefield)
	sa := kr0SA(t, "DB$ ChooseCard | Choices$ Creature | Amount$ 1 | Mandatory$ True | ImprintChosen$ True | SubAbility$ X",
		"SVar:X:DB$ Tap | Defined$ Imprinted")
	var last *effects.Ctx
	d := kr0Run(t, e, sa, func() *effects.Ctx { return &effects.Ctx{Source: source, Controller: 0} }, &last)
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition chosen card zone=%+v, want battlefield", o)
	}
	if d == nil || len(d.Options) != 1 || d.Options[0].Obj != creature {
		t.Fatalf("ChooseCard options=%+v, want battlefield creature %d", d, creature)
	}
	if next := kr0Answer(t, e, kr0Opt(t, d, creature)); next != nil {
		t.Fatalf("resolution left decision pending: %+v", next)
	}
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield || !o.Tapped {
		t.Fatalf("Defined$ Imprinted did not tap the battlefield pick: %+v", o)
	}
	if last == nil {
		t.Fatal("resolution produced no final context")
	}
}

func TestThreatsAroundEveryCornerManifestDreadPin(t *testing.T) {
	t.Parallel()
	threats := kr0Corpus(t, "Threats Around Every Corner")
	e := kr0Engine(t, 2)
	source := kr0Place(t, e, 0, threats, state.ZBattlefield)
	jace := kr0Library(t, e, 0, "Name:Jace Beleren\nTypes:Planeswalker\nPT:3\nOracle:x\n", 1)[0]
	bear := kr0Src(t, e, 0, "Name:Grizzly Bears\nTypes:Creature\nPT:2/2\nOracle:x\n", state.ZLibrary)
	kr0Src(t, e, 0, "Name:Forest\nTypes:Land Basic Forest\nOracle:x\n", state.ZLibrary)
	if got := e.G.Zone(state.ZLibrary, 0); len(got) != 3 || got[0] != jace || got[1] != bear {
		t.Fatalf("precondition library top order=%v, want Jace, Bears, Forest", got)
	}
	var last *effects.Ctx
	d := kr0Run(t, e, kr0SVar(t, threats, "TrigDread"), func() *effects.Ctx {
		return &effects.Ctx{Source: source, Controller: 0, SVars: threats.Faces[0].SVars}
	}, &last)
	if d == nil || len(d.Options) != 2 {
		t.Fatalf("manifest-dread ask=%+v, want Jace and Bears", d)
	}
	if d.Options[0].Obj != jace || d.Options[1].Obj != bear {
		t.Fatalf("manifest-dread options=%+v, want Jace then Bears", d.Options)
	}
	if next := kr0Answer(t, e, kr0Opt(t, d, jace)); next != nil {
		t.Fatalf("resolution left decision pending: %+v", next)
	}
	if o := e.G.Obj(jace); o == nil || o.Zone != state.ZBattlefield || !o.FaceDown {
		t.Fatalf("Jace zone/face-down=%+v, want face-down battlefield", o)
	}
	if got := e.G.Zone(state.ZGraveyard, 0); len(got) != 1 || got[0] != bear {
		t.Fatalf("p0 graveyard=%v, want only Grizzly Bears (the stale expectation is compliance/verdicts/t.jsonl:558)", got)
	}
	if e.G.Obj(jace).Zone == state.ZGraveyard {
		t.Fatal("chosen Jace unexpectedly entered the graveyard")
	}
	if last == nil {
		t.Fatal("resolution produced no final context")
	}
}
