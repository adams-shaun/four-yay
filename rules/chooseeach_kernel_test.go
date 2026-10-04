package rules

// Restored from effects/chooseeach_test.go (W3 legacy removal): ChooseCard's
// ChooseEach$ per-group walk, answered through the resolution kernel on a
// real engine.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestChooseCardChooseEachTragicArroganceAsksPerGroupKernel: Tragic
// Arrogance's real YouChoose asks once per group (Artifact, Creature,
// Enchantment) over the remembered player's permanents, honours a
// non-first creature election, never asks for the empty Planeswalker group,
// and accumulates the picks in Chosen and (RememberChosen$) Remembered, with
// one reveal Note per answered group.
func TestChooseCardChooseEachTragicArroganceAsksPerGroupKernel(t *testing.T) {
	t.Parallel()
	ta := kr0Corpus(t, "Tragic Arrogance")
	s := kr0SVar(t, ta, "YouChoose")
	if s.API != "ChooseCard" || s.Params["ChooseEach"] != "Artifact & Creature & Enchantment & Planeswalker" {
		t.Fatalf("Tragic Arrogance's YouChoose SA changed: %+v", s)
	}
	e := kr0Engine(t, 2)
	src := kr0Place(t, e, 0, ta, state.ZBattlefield)
	art := kr0Src(t, e, 1, "Name:TA Art\nTypes:Artifact\nOracle:x\n", state.ZBattlefield)
	cre1 := kr0Src(t, e, 1, "Name:TA Cre One\nTypes:Creature\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	cre2 := kr0Src(t, e, 1, "Name:TA Cre Two\nTypes:Creature\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	ench := kr0Src(t, e, 1, "Name:TA Ench\nTypes:Enchantment\nOracle:x\n", state.ZBattlefield)
	start := len(e.L.Events)
	var last *effects.Ctx
	d := kr0Run(t, e, s, func() *effects.Ctx {
		return &effects.Ctx{Source: src, Controller: 0, SVars: ta.Faces[0].SVars,
			Remembered: []state.Target{{Player: 1, IsPlayer: true}}}
	}, &last)
	if d == nil || d.Player != 0 || d.Min != 1 || d.Max != 1 || d.ResumeKind != "choice" {
		t.Fatalf("Artifact ask = %+v, want a mandatory single pick for seat 0", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != art {
		t.Fatalf("Artifact group options = %+v, want exactly %d", d.Options, art)
	}
	d = kr0Answer(t, e, 0)
	if d == nil || len(d.Options) != 2 || d.Options[0].Obj != cre1 || d.Options[1].Obj != cre2 {
		t.Fatalf("Creature ask = %+v, want both creatures in order", d)
	}
	d = kr0Answer(t, e, kr0Opt(t, d, cre2))
	if d == nil || len(d.Options) != 1 || d.Options[0].Obj != ench {
		t.Fatalf("Enchantment ask = %+v, want the enchantment alone", d)
	}
	if next := kr0Answer(t, e, 0); next != nil {
		t.Fatalf("a fourth ask was posed (the empty Planeswalker group must be silent): %+v", next)
	}
	wantPicks := []state.ObjID{art, cre2, ench}
	if len(last.Chosen) != len(wantPicks) {
		t.Fatalf("Chosen = %+v, want %v", last.Chosen, wantPicks)
	}
	for i, id := range wantPicks {
		if last.Chosen[i].Obj != id {
			t.Fatalf("Chosen = %+v, want %v (the creature election honoured)", last.Chosen, wantPicks)
		}
	}
	if len(last.Remembered) != len(wantPicks)+1 || last.Remembered[0].Player != 1 {
		t.Fatalf("Remembered = %+v, want the remembered player plus the three picks", last.Remembered)
	}
	for i, id := range wantPicks {
		if last.Remembered[i+1].Obj != id {
			t.Fatalf("Remembered[%d] = %d, want %d", i+1, last.Remembered[i+1].Obj, id)
		}
	}
	reveals := 0
	for _, ev := range kr0Since(e, start) {
		if ev.Kind != events.Note {
			continue
		}
		if strings.Contains(ev.Text, "unimplemented") || strings.Contains(ev.Text, "no engine host") {
			t.Fatalf("unexpected degradation Note %+v", ev)
		}
		if len(ev.IDs) == 1 && ev.IDs[0] != 0 {
			reveals++
		}
	}
	if reveals != 3 {
		t.Fatalf("reveal Notes = %d, want one per answered group", reveals)
	}
}

// TestChooseCardChooseEachPartyExpandsKernel: ChooseEach$ Party reads as the
// four party groups (Cleric, Rogue, Warrior, Wizard): the Cleric and the
// Warrior groups ask, the empty groups and the non-party Goblin never show.
func TestChooseCardChooseEachPartyExpandsKernel(t *testing.T) {
	t.Parallel()
	e := kr0Engine(t, 2)
	cleric := kr0Src(t, e, 0, "Name:Party Cleric\nTypes:Creature Cleric\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	warrior := kr0Src(t, e, 0, "Name:Party Warrior\nTypes:Creature Warrior\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	goblin := kr0Src(t, e, 0, "Name:Not Party\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	var last *effects.Ctx
	d := kr0Run(t, e, kr0SA(t, "SP$ ChooseCard | Defined$ You | Choices$ Creature | ChooseEach$ Party | Mandatory$ True | Reveal$ True"),
		func() *effects.Ctx { return &effects.Ctx{Source: cleric, Controller: 0} }, &last)
	if d == nil || len(d.Options) != 1 || d.Options[0].Obj != cleric {
		t.Fatalf("first party ask = %+v, want the cleric alone", d)
	}
	first := d.ResumeTarget
	d = kr0Answer(t, e, 0)
	if d == nil || len(d.Options) != 1 || d.Options[0].Obj != warrior {
		t.Fatalf("second party ask = %+v, want the warrior alone", d)
	}
	if d.ResumeTarget <= first {
		t.Fatalf("party ResumeTarget %d did not advance past %d", d.ResumeTarget, first)
	}
	if next := kr0Answer(t, e, 0); next != nil {
		t.Fatalf("a third party ask was posed: %+v", next)
	}
	if len(last.Chosen) != 2 || last.Chosen[0].Obj != cleric || last.Chosen[1].Obj != warrior {
		t.Fatalf("Chosen = %+v, want the cleric and the warrior", last.Chosen)
	}
	for _, c := range last.Chosen {
		if c.Obj == goblin {
			t.Fatal("the non-party goblin was chosen")
		}
	}
}
