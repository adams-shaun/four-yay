package rules

// Kernel-era restorations of the effects-package DigUntil ask tests the W3
// legacy removal deleted (diguntil, diguntil_aura, diguntil_opponent_aura,
// diguntil_riders): the reveal-until walk driven through a real engine,
// the mid-resolution ask answered, and the outcome asserted.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const (
	kr1Halo     = "Name:Halo\nManaCost:W\nTypes:Enchantment Aura\nK:Enchant:Creature\nOracle:x\n"
	kr1BearOne  = "Name:Bear One\nManaCost:G\nTypes:Creature\nPT:2/2\nOracle:x\n"
	kr1BearTwo  = "Name:Bear Two\nManaCost:G\nTypes:Creature\nPT:3/3\nOracle:x\n"
	kr1OppBear  = "Name:Opponent Bear\nManaCost:G\nTypes:Creature\nPT:3/3\nOracle:x\n"
	kr1Songbird = "A:SP$ DigUntil | Valid$ Aura | FoundDestination$ Battlefield | OptionalFoundMove$ True | OptionalNoDestination$ Hand | RevealedDestination$ Library | RevealedLibraryPosition$ -1"
)

// TestKr1DigUntilAuraEntryAsksForBearer (was TestDigUntilAuraEntryAsksForBearer):
// a found Aura entering the battlefield (CR 303.4f) asks for its bearer
// over both eligible creatures before it moves, and the answer — the
// SECOND creature, not an option-zero default — is what it attaches to.
func TestKr1DigUntilAuraEntryAsksForBearer(t *testing.T) {
	t.Parallel()
	e, cfg, id := kr1New(t, 140, kr1Sorcery("AuraDig", "A:SP$ DigUntil | Valid$ Aura | FoundDestination$ Battlefield"),
		[]string{kr1BearOne, kr1BearTwo, kr1Halo}, nil)
	addMana(t, e, 0, "B")
	b1 := kr1Put(t, e, 0, "Bear One", state.ZBattlefield)
	b2 := kr1Put(t, e, 0, "Bear Two", state.ZBattlefield)
	aura := kr1Top(t, e, 0, "Halo")[0]
	d := kr1Cast(t, e, id)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "diguntil_aura" {
		t.Fatalf("bearer decision = %+v, want KChoose/diguntil_aura", d)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != b1 || d.Options[1].Obj != b2 {
		t.Fatalf("bearer options = %+v, want [%d %d]", d.Options, b1, b2)
	}
	if kr1Zone(e, aura) != state.ZLibrary {
		t.Fatalf("Aura moved before its bearer was chosen: %s", kr1Zone(e, aura))
	}
	kr1Answer(t, e, decision.KChoose, 0, 1)
	if o := e.G.Obj(aura); o.Zone != state.ZBattlefield || o.AttachedTo != b2 {
		t.Fatalf("Aura zone/attachment = %s/%d, want battlefield/%d", o.Zone, o.AttachedTo, b2)
	}
	replayCheck(t, e, cfg)
}

// TestKr1DigUntilAuraCanEnchantOpponentsCreature (was
// TestDigUntilAuraCanEnchantOpponentsCreature): the bearer ask offers the
// opponent's creature too, and answering it attaches the Aura there.
func TestKr1DigUntilAuraCanEnchantOpponentsCreature(t *testing.T) {
	t.Parallel()
	e, cfg, id := kr1New(t, 141, kr1Sorcery("AuraDig", "A:SP$ DigUntil | Valid$ Aura | FoundDestination$ Battlefield"),
		[]string{kr1BearOne, kr1Halo}, []string{kr1OppBear})
	addMana(t, e, 0, "B")
	own := kr1Put(t, e, 0, "Bear One", state.ZBattlefield)
	opp := kr1Put(t, e, 1, "Opponent Bear", state.ZBattlefield)
	aura := kr1Top(t, e, 0, "Halo")[0]
	if e.G.Obj(opp).Controller != 1 {
		t.Fatal("setup: the opponent's creature is not under seat 1's control")
	}
	d := kr1Cast(t, e, id)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "diguntil_aura" {
		t.Fatalf("bearer decision = %+v, want KChoose/diguntil_aura", d)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != own || d.Options[1].Obj != opp {
		t.Fatalf("bearer options = %+v, want own %d then opponent %d", d.Options, own, opp)
	}
	kr1Answer(t, e, decision.KChoose, 0, 1)
	if o := e.G.Obj(aura); o.Zone != state.ZBattlefield || o.AttachedTo != opp {
		t.Fatalf("Aura zone/attachment = %s/%d, want battlefield/%d", o.Zone, o.AttachedTo, opp)
	}
	replayCheck(t, e, cfg)
}

// kr1PublicReveals counts public Notes carrying exactly n ids after mark.
func kr1PublicReveals(e *Engine, mark, n int) int {
	c := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Note && !ev.Secret && len(ev.IDs) == n {
			c++
		}
	}
	return c
}

// TestKr1DigUntilOptionalFoundMoveAsksAndHonoursBothBranches (was
// TestDigUntilOptionalFoundMoveAsksAndHonoursBothBranches, Songbirds'
// Blessing's shape): the found card is offered as a yes/no to the library's
// owner after the public reveal and before anything moves; "no" sends it to
// OptionalNoDestination$ Hand with the revealed rest at the bottom; "yes"
// puts it onto the battlefield attached; the reveal is recorded once.
func TestKr1DigUntilOptionalFoundMoveAsksAndHonoursBothBranches(t *testing.T) {
	t.Parallel()
	for i, answer := range []string{"no", "yes"} {
		answer := answer
		seed := uint64(142 + i)
		t.Run(answer, func(t *testing.T) {
			t.Parallel()
			e, cfg, id := kr1New(t, seed, kr1Sorcery("Songbird", kr1Songbird), []string{kr1BearOne, kr1Halo}, nil)
			addMana(t, e, 0, "B")
			bear := kr1Put(t, e, 0, "Bear One", state.ZBattlefield)
			ids := kr1Top(t, e, 0, "Mountain", "Halo", "Mountain", "Mountain")
			mark := len(e.L.Events)
			d := kr1Cast(t, e, id)
			if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.Min != 1 || d.Max != 1 || d.ResumeKind != "diguntil_move" {
				t.Fatalf("decision = %+v, want a Min==Max==1 diguntil_move KChoose for seat 0", d)
			}
			if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
				t.Fatalf("options = %+v, want yes/no", d.Options)
			}
			if kr1Zone(e, ids[1]) != state.ZLibrary {
				t.Fatalf("found card moved before the answer: %s", kr1Zone(e, ids[1]))
			}
			if kr1PublicReveals(e, mark, 2) != 1 {
				t.Fatal("the public reveal of both turned-over cards was not recorded before the ask")
			}
			idx := 1
			if answer == "yes" {
				idx = 0
			}
			kr1Answer(t, e, decision.KChoose, 0, idx)
			if answer == "no" {
				if kr1Zone(e, ids[1]) != state.ZHand {
					t.Fatalf("declined found card zone = %s, want hand", kr1Zone(e, ids[1]))
				}
				lib := e.G.Zone(state.ZLibrary, 0)
				if len(lib) == 0 || lib[len(lib)-1] != ids[0] || lib[0] != ids[2] {
					t.Fatalf("library = %v, want the revealed land %d at the bottom and %d on top", lib, ids[0], ids[2])
				}
			} else if o := e.G.Obj(ids[1]); o.Zone != state.ZBattlefield || o.AttachedTo != bear {
				t.Fatalf("accepted found card zone/attach = %s/%d, want battlefield/%d", o.Zone, o.AttachedTo, bear)
			}
			if n := kr1PublicReveals(e, mark, 2); n != 1 {
				t.Fatalf("public reveal Notes = %d, want exactly 1 across the ask", n)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestKr1DigUntilRidersEmitOnceAcrossTheOptionalAsk (was
// TestDigUntilRidersEmitOnceAcrossTheOptionalAsk): ImprintFound$ and
// Shuffle$ are recorded exactly once across the OptionalFoundMove$ ask —
// none before the answer, one each after, the imprint carrying the found
// Aura.
func TestKr1DigUntilRidersEmitOnceAcrossTheOptionalAsk(t *testing.T) {
	t.Parallel()
	body := "A:SP$ DigUntil | Valid$ Aura | FoundDestination$ Battlefield | OptionalFoundMove$ True | OptionalNoDestination$ Hand | RevealedDestination$ Library | RevealedLibraryPosition$ -1 | ImprintFound$ True | Shuffle$ True"
	e, cfg, id := kr1New(t, 144, kr1Sorcery("RiderDig", body), []string{kr1BearOne, kr1Halo}, nil)
	addMana(t, e, 0, "B")
	kr1Put(t, e, 0, "Bear One", state.ZBattlefield)
	ids := kr1Top(t, e, 0, "Mountain", "Halo", "Mountain")
	count := func(mark int) (imprints []events.Event, shuffles int) {
		for _, ev := range e.L.Events[mark:] {
			if ev.Kind == events.Imprint && ev.Obj == id {
				imprints = append(imprints, ev)
			}
			if ev.Kind == events.Shuffle && ev.Player == 0 {
				shuffles++
			}
		}
		return
	}
	mark := len(e.L.Events)
	d := kr1Cast(t, e, id)
	if d == nil || d.ResumeKind != "diguntil_move" {
		t.Fatalf("decision = %+v, want the OptionalFoundMove$ ask", d)
	}
	if imp, sh := count(mark); len(imp) != 0 || sh != 0 {
		t.Fatalf("before the answer: %d imprints, %d shuffles, want none", len(imp), sh)
	}
	kr1Answer(t, e, decision.KChoose, 0, 0)
	if kr1Zone(e, ids[1]) != state.ZBattlefield {
		t.Fatalf("answered found Aura zone = %s, want battlefield", kr1Zone(e, ids[1]))
	}
	imp, sh := count(mark)
	if len(imp) != 1 || sh != 1 {
		t.Fatalf("after the answer: %d imprints, %d shuffles, want exactly 1 each", len(imp), sh)
	}
	if len(imp[0].IDs) != 1 || imp[0].IDs[0] != ids[1] {
		t.Fatalf("imprint payload = %v, want [%d]", imp[0].IDs, ids[1])
	}
	replayCheck(t, e, cfg)
}
