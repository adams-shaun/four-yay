package rules

// Kernel-era restorations of the effects-package GainControlVariant chooser
// tests the W3 legacy removal deleted (gain_control_variant_players): a
// three-seat real engine, the per-recipient ask answered with a NON-first
// option, and every recipient's control change asserted.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func kr1Perm(name, types string) string {
	return "Name:" + name + "\nManaCost:1\nTypes:" + types + "\nPT:1/1\nOracle:x\n"
}

// kr1ThreeSeats builds a three-seat game whose seat 0 holds fixtureSrc.
func kr1ThreeSeats(t *testing.T, seed uint64, fixtureSrc string, ex0, ex1, ex2 []string) (*Engine, Config, state.ObjID) {
	t.Helper()
	return kr1Build(t, seed, card(t, fixtureSrc), kr1Cards(t, ex0), kr1Cards(t, ex1), kr1Cards(t, ex2))
}

// TestKr1GainControlVariantInniazCasterChoosesForEachRecipient (was
// TestGainControlVariantInniazCasterChoosesForEachRecipient, Inniaz's
// ChooseFromPlayerToTheirRight shape): the effect's controller chooses, for
// each recipient, a nonland permanent of that recipient's right neighbour;
// only recipient 1's pool (seat 0's two permanents) is a real choice, so
// seat 0 — the caster, not the recipient — is asked, and the SECOND option
// is what changes control.
func TestKr1GainControlVariantInniazCasterChoosesForEachRecipient(t *testing.T) {
	t.Parallel()
	src := kr1Sorcery("InniazFx", "A:SP$ GainControlVariant | AllValid$ Permanent.nonLand | ChangeController$ ChooseFromPlayerToTheirRight")
	e, cfg, id := kr1ThreeSeats(t, 230, src,
		[]string{kr1Perm("P Zero A", "Creature"), kr1Perm("P Zero B", "Artifact")},
		[]string{kr1Perm("R Two", "Creature")},
		[]string{kr1Perm("R Zero", "Creature")})
	addMana(t, e, 0, "B")
	r0 := kr1Put(t, e, 2, "R Zero", state.ZBattlefield)
	p0a := kr1Put(t, e, 0, "P Zero A", state.ZBattlefield)
	p0b := kr1Put(t, e, 0, "P Zero B", state.ZBattlefield)
	r2 := kr1Put(t, e, 1, "R Two", state.ZBattlefield)
	d := kr1Cast(t, e, id)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" {
		t.Fatalf("Inniaz ask = %+v, want the per-recipient KChoose", d)
	}
	if d.Player != 0 {
		t.Fatalf("Inniaz chooser = seat %d, want the caster seat 0", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != p0a || d.Options[1].Obj != p0b {
		t.Fatalf("Inniaz options = %+v, want seat 0's two permanents [%d %d]", d.Options, p0a, p0b)
	}
	if p := kr1Pick(t, e, 1); p != nil {
		t.Fatalf("unexpected further ask %+v", p)
	}
	for _, c := range []struct {
		id   state.ObjID
		want state.PlayerID
		what string
	}{{r0, 0, "recipient 0's pick"}, {p0b, 1, "recipient 1's (answered) pick"}, {p0a, 0, "the unchosen permanent"}, {r2, 2, "recipient 2's pick"}} {
		if got := e.G.Obj(c.id).Controller; got != c.want {
			t.Fatalf("%s controller = %d, want %d", c.what, got, c.want)
		}
	}
	replayCheck(t, e, cfg)
}

// TestKr1GainControlVariantOrderEachRecipientChoosesForThemself (was
// TestGainControlVariantOrderEachRecipientChoosesForThemself, Order of
// Succession): after the caster picks "left", each recipient chooses for
// themself a creature of the next player in that direction; only seat 2's
// pool (seat 0's two creatures) is a real choice, so seat 2 is asked and
// its SECOND option is what it gains.
func TestKr1GainControlVariantOrderEachRecipientChoosesForThemself(t *testing.T) {
	t.Parallel()
	src := "Name:OrderFx\nManaCost:B\nTypes:Sorcery\nA:SP$ ChooseDirection | SubAbility$ DBGainControl\n" +
		"SVar:DBGainControl:DB$ GainControlVariant | AllValid$ Creature | ChangeController$ ChooseNextPlayerInChosenDirection\nOracle:x\n"
	e, cfg, id := kr1ThreeSeats(t, 231, src,
		[]string{kr1Perm("C Zero A", "Creature"), kr1Perm("C Zero B", "Creature")},
		[]string{kr1Perm("C One", "Creature")},
		[]string{kr1Perm("C Two", "Creature")})
	addMana(t, e, 0, "B")
	c1 := kr1Put(t, e, 1, "C One", state.ZBattlefield)
	c2 := kr1Put(t, e, 2, "C Two", state.ZBattlefield)
	c0a := kr1Put(t, e, 0, "C Zero A", state.ZBattlefield)
	c0b := kr1Put(t, e, 0, "C Zero B", state.ZBattlefield)
	d := kr1Cast(t, e, id)
	if d == nil || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("direction ask = %+v, want left/right for seat 0", d)
	}
	left := -1
	for _, o := range d.Options {
		if o.Label == "left" || o.Kind == "left" {
			left = o.Index
		}
	}
	if left < 0 {
		t.Fatalf("no left option: %+v", d.Options)
	}
	d = kr1Pick(t, e, left)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" {
		t.Fatalf("Order ask = %+v, want the per-recipient KChoose", d)
	}
	if d.Player != 2 {
		t.Fatalf("Order chooser = seat %d, want seat 2 (each recipient chooses for themself)", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != c0a || d.Options[1].Obj != c0b {
		t.Fatalf("Order options = %+v, want seat 0's two creatures [%d %d]", d.Options, c0a, c0b)
	}
	if p := kr1Pick(t, e, 1); p != nil {
		t.Fatalf("unexpected further ask %+v", p)
	}
	for _, c := range []struct {
		id   state.ObjID
		want state.PlayerID
	}{{c1, 0}, {c2, 1}, {c0b, 2}, {c0a, 0}} {
		if got := e.G.Obj(c.id).Controller; got != c.want {
			t.Fatalf("%s controller = %d, want %d", e.G.Obj(c.id).Face().Name, got, c.want)
		}
	}
	replayCheck(t, e, cfg)
}
