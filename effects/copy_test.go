package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// spellOnStack places a fresh object of fixture card on the stack and returns
// it, so the copy effect has a genuine spell-in-standby to duplicate -- the
// Without-Storm companion to rules/storm_test.go, which drives the whole
// triggered path through the engine.
func spellOnStack(t *testing.T, h *fakeHost, src string, ctlr state.PlayerID) *state.Object {
	t.Helper()
	c := mkCard(t, "Name:Blast\nManaCost:R\nTypes:Sorcery\n"+src+"\nOracle:x\n")
	o := h.g.AddObject(c, ctlr)
	events.Move(h.g, o.ID, state.ZLibrary, state.ZStack)
	return h.g.Obj(o.ID)
}

func copyEvents(h *fakeHost) int {
	n := 0
	for _, ev := range h.log {
		if ev.Kind == events.StackCopy {
			n++
		}
	}
	return n
}

func TestCopySpellAbilityDuplicatesTheSourceNamedByParent(t *testing.T) {
	h := newHost(t, 2)
	o := spellOnStack(t, h, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 0)
	Resolve(h, &Ctx{Source: o.ID, Controller: 0},
		sa(t, "SP$ CopySpellAbility | Defined$ Parent | Amount$ 2 | MayChooseTarget$ True"))

	if got, want := copyEvents(h), 2; got != want {
		t.Fatalf("%d StackCopy events, want %d", got, want)
	}
	// The source spell never leaves the stack (a copy is placed above it).
	if o.Zone != state.ZStack {
		t.Fatalf("source left the stack: %s", o.Zone)
	}
	// A copy object actually exists, tagged IsCopy, on the stack.
	copies := 0
	for _, ob := range h.g.Objs {
		if ob.IsCopy {
			copies++
		}
	}
	if copies != 2 {
		t.Fatalf("%d copy objects, want 2", copies)
	}
	// CR 707.10c: MayChooseTarget$ True now rides each StackCopy event as the
	// Amount discriminator (1 = the copy's controller may choose new targets),
	// recorded per copy instance and consumed when targets are recorded. There
	// is no longer a "keeps its targets" Note: the election is real.
	flagged := 0
	for _, ev := range h.log {
		if ev.Kind == events.StackCopy && ev.Amount == 1 {
			flagged++
		}
	}
	if flagged != 2 {
		t.Fatalf("%d StackCopy events carry the MayChooseTarget$ flag, want 2", flagged)
	}
	notes := 0
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "keeps its targets") {
			notes++
		}
	}
	if notes != 0 {
		t.Fatalf("%d 'keeps its targets' Notes, want none (the election replaced the stand-in)", notes)
	}
}

// corpusCopySA returns the REAL compiled CopySpellAbility sub-ability of a
// named corpus card, walking every face's abilities, triggers and their
// SubAbility chains. The copy shapes hide in different places: Chain
// Lightning's lives in a spell's SubAbility (DBCopy1), Wandering Archaic's
// in a trigger's Effect (TrigCopySpell). The brief insists on real compiled
// SAs rather than a synthetic map fixture -- a hand-built bag is exactly
// what let the inverted unswitched orientation look right (B2).
func corpusCopySA(t *testing.T, name string) *cards.SA {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("corpus has no %q", name)
	}
	var found *cards.SA
	var walk func(*cards.SA)
	walk = func(sa *cards.SA) {
		if sa == nil || found != nil {
			return
		}
		if sa.API == "CopySpellAbility" {
			found = sa
			return
		}
		walk(sa.Sub)
	}
	for _, f := range c.Faces {
		for _, a := range f.Abilities {
			walk(a)
		}
		for _, t := range f.Triggers {
			walk(t.Effect)
		}
	}
	if found == nil {
		t.Fatalf("corpus card %q has no CopySpellAbility", name)
	}
	return found
}

// TestCopySpellAbilityUnswitchedNoAskHostCopiesOnTheDecline covers the
// no-ask determination for the UNSWITCHED shape (an UnlessCost$ with NO
// UnlessSwitched$): its deterministic decline IS the copy path ("if they
// don't pay, you may copy"), so a host that cannot ask (an effects-package
// test double) still copies and records the suppressed-ask Note -- the
// opposite of the SWITCHED shape, where the deterministic decline copies
// nothing.
func TestCopySpellAbilityUnswitchedNoAskHostCopiesOnTheDecline(t *testing.T) {
	h := newHost(t, 2)
	o := spellOnStack(t, h, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 0)
	Resolve(h, &Ctx{Source: o.ID, Controller: 0},
		sa(t, "SP$ CopySpellAbility | Defined$ Parent | UnlessCost$ R R | UnlessPayer$ TargetedOrController"))

	if got := copyEvents(h); got != 1 {
		t.Fatalf("%d StackCopy events, want 1 (the unswitched decline IS the copy path)", got)
	}
	found := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "declined") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no declined-may-pay Note recorded")
	}
}

// TestCopySpellAbilitySwitchedShapePayingMakesTheCopies drives Chain
// Lightning's REAL compiled copy SA -- the SWITCHED shape (UnlessCost$ R R
// with UnlessSwitched$ True). Paying CAUSES the copy, so "decline" makes
// nothing and "pay" makes exactly one; the copy keeps the target's
// controller as payer (UnlessPayer$ TargetedOrController, resolved from the
// first target). This is main's already-correct behaviour and must stay put:
// the guard reads UnlessSwitched$ True, so the switched shapes are untouched
// by the unswitched inversion.
func TestCopySpellAbilitySwitchedShapePayingMakesTheCopies(t *testing.T) {
	saCL := corpusCopySA(t, "Chain Lightning")
	if !strings.EqualFold(strings.TrimSpace(saCL.Params["UnlessSwitched"]), "True") {
		t.Fatalf("Chain Lightning copy SA unexpectedly lacks UnlessSwitched$ True: %+v", saCL.Params)
	}
	h := &askHost{}
	h.g = state.NewGame(names(2))
	o := spellOnStack(t, &h.fakeHost, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 0)
	Resolve(h, &Ctx{Source: o.ID, Controller: 0,
		Targets: []state.Target{{Player: 1, IsPlayer: true}}}, saCL)
	if h.asked == nil {
		t.Fatal("no pay decision was posed")
	}
	d := h.asked
	if d.Kind != decision.KModes || d.Player != 1 || d.Min != 1 || d.Max != 1 {
		t.Fatalf("decision = %+v, want a Min==Max==1 KModes posed to the TARGET's controller (seat 1)", d)
	}
	if len(d.Options) != 2 || d.Options[0].Label != "Pay R R — make a copy" {
		t.Fatalf("options = %+v, want the switched pay/decline pair headed by the copy label", d.Options)
	}
	if got := copyEvents(&h.fakeHost); got != 0 {
		t.Fatalf("%d copies made before the pay decision was answered", got)
	}
	// Re-entry, decline: the switched shape copies nothing.
	Resolve(h, &Ctx{Source: o.ID, Controller: 0, UnlessPay: "decline"}, saCL)
	if got := copyEvents(&h.fakeHost); got != 0 {
		t.Fatalf("%d copies made on a switched decline, want 0", got)
	}
	// Re-entry, pay: the switched copy loop runs exactly once.
	Resolve(h, &Ctx{Source: o.ID, Controller: 0, UnlessPay: "pay"}, saCL)
	if got := copyEvents(&h.fakeHost); got != 1 {
		t.Fatalf("%d copies made on a switched pay, want 1", got)
	}
}

// The DefinedTarget$ route (Feather, Radiant Arbiter's "for each of those
// creatures, copy that spell. The copy targets that creature"): one copy per
// defined entry, each copy's target REPLACED by its own entry (the StackCopy
// event's IDs), and a chosen-PLAYER entry never becomes a target.
func TestCopySpellAbilityDefinedTargetChosenCardReplacesEachCopyTarget(t *testing.T) {
	h := newHost(t, 2)
	o := spellOnStack(t, h, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 0)
	bear := h.g.AddObject(mkCard(t, "Name:Grizzly Bears\nManaCost:1G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	events.Move(h.g, bear.ID, state.ZLibrary, state.ZBattlefield)
	bear2 := h.g.AddObject(mkCard(t, "Name:Grizzly Bears\nManaCost:1G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	events.Move(h.g, bear2.ID, state.ZLibrary, state.ZBattlefield)

	Resolve(h, &Ctx{Source: o.ID, Controller: 0, ChosenValid: true,
		Chosen: []state.Target{{Obj: bear.ID}, {Obj: bear2.ID}}},
		sa(t, "DB$ CopySpellAbility | Defined$ Parent | DefinedTarget$ ChosenCard"))

	if got := copyEvents(h); got != 2 {
		t.Fatalf("%d StackCopy events, want one per chosen creature", got)
	}
	byTarget := map[state.ObjID][]state.Target{}
	for _, ev := range h.log {
		if ev.Kind != events.StackCopy {
			continue
		}
		if len(ev.IDs) != 1 || (ev.IDs[0] != bear.ID && ev.IDs[0] != bear2.ID) {
			t.Fatalf("StackCopy IDs = %v, want one chosen creature per copy", ev.IDs)
		}
		byTarget[ev.IDs[0]] = h.g.Objs[len(h.g.Objs)-1].Targets
	}
	// The minted copies (the last two objects) each carry their OWN creature
	// as their target list, and the two lists differ.
	if len(byTarget) != 2 {
		t.Fatalf("copies named %d distinct creatures, want both", len(byTarget))
	}
	copies := 0
	for _, ob := range h.g.Objs {
		if !ob.IsCopy {
			continue
		}
		copies++
		if len(ob.Targets) != 1 || (ob.Targets[0].Obj != bear.ID && ob.Targets[0].Obj != bear2.ID) {
			t.Fatalf("copy targets = %+v, want its own bear", ob.Targets)
		}
	}
	if copies != 2 {
		t.Fatalf("%d copy objects, want 2", copies)
	}
}

// A chosen-PLAYER entry is not "those creatures": with only a player chosen,
// the DefinedTarget$ ChosenCard route resolves to an empty object set and
// copies nothing.
func TestCopySpellAbilityDefinedTargetChosenCardSkipsPlayerEntries(t *testing.T) {
	h := newHost(t, 2)
	o := spellOnStack(t, h, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 0)
	_ = o
	Resolve(h, &Ctx{Source: o.ID, Controller: 0, ChosenValid: true,
		Chosen: []state.Target{{Player: 1, IsPlayer: true}}},
		sa(t, "DB$ CopySpellAbility | Defined$ Parent | DefinedTarget$ ChosenCard"))
	if got := copyEvents(h); got != 0 {
		t.Fatalf("%d StackCopy events, want none from a player-only chosen set", got)
	}
}

// DefinedTarget$ Self (Ivy, Gleeful Spellthief: "The copy targets NICKNAME")
// targets the resolving source; an unresolvable value keeps the historical
// single inherited-target copy under the loud Note.
func TestCopySpellAbilityDefinedTargetSelfAndUnresolved(t *testing.T) {
	h := newHost(t, 2)
	o := spellOnStack(t, h, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 0)
	_ = o
	src := h.g.AddObject(mkCard(t, "Name:Ivy\nManaCost:GU\nTypes:Creature Faerie Rogue\nPT:2/1\nOracle:x\n"), 0)
	events.Move(h.g, src.ID, state.ZLibrary, state.ZBattlefield)

	// Ivy's real shape: the copy SA is the trigger's sub, so the spell comes
	// from the trigger's Remembered and the SELF target is the trigger's
	// source (Ivy herself, on the battlefield).
	Resolve(h, &Ctx{Source: src.ID, Controller: 0, Remembered: []state.Target{{Obj: o.ID}}},
		sa(t, "DB$ CopySpellAbility | Defined$ TriggeredSpellAbility | DefinedTarget$ Self"))
	if got := copyEvents(h); got != 1 {
		t.Fatalf("%d StackCopy events, want the one Self-targeted copy", got)
	}
	if co := h.g.Objs[len(h.g.Objs)-1]; len(co.Targets) != 1 || co.Targets[0].Obj != src.ID {
		t.Fatalf("copy targets = %+v, want Ivy herself", co.Targets)
	}

	h2 := newHost(t, 2)
	o2 := spellOnStack(t, h2, "A:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3", 0)
	Resolve(h2, &Ctx{Source: o2.ID, Controller: 0, Remembered: []state.Target{{Obj: o2.ID}}},
		sa(t, "DB$ CopySpellAbility | Defined$ TriggeredSpellAbility | DefinedTarget$ OppNonTriggeredSpellAbilityTargetsOrController"))
	if got := copyEvents(h2); got != 1 {
		t.Fatalf("%d StackCopy events, want the one inherited-target copy", got)
	}
	notes := 0
	for _, ev := range h2.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "DefinedTarget$") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("%d unresolved-DefinedTarget$ Notes, want 1", notes)
	}
}
