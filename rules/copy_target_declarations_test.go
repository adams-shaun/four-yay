package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCopyOfFusedSpellAsksPerDeclaration pins the per-declaration half of the
// copy-target approximation: a fused spell has TWO target declarations, the
// front half's and the alternate half's, each with its OWN ValidTgts$ and
// therefore its own legal set. AskCopyTargets must ask them in order -- one
// KNOCK for the artifact (Wear's declaration) and a SECOND for the
// enchantment (Tear's declaration) -- rather than collapsing both into one
// flat ask built from the front half alone. Before the fix the copy was asked
// only once, from Wear's artifact spec, so Tear's enchantment was never
// offered, the copy's answer dropped it, and Tear did nothing.
//
// The carriers are the REAL corpus cards: Wear // Tear (fused through its own
// K:Fuse) cast and copied through Mirrorpool's MayChooseTarget$ ability. No
// synthetic fixture card takes part.
func TestCopyOfFusedSpellAsksPerDeclaration(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	// PRECONDITION: the two halves really carry DIFFERENT declarations, so a
	// single flat ask can only ever satisfy one of them.
	wear := searchCorpusCard(t, reg, "Wear")
	if len(wear.Faces) != 2 {
		t.Fatalf("Wear is not a two-face split card: %+v", wear.Faces)
	}
	frontSA := wear.Faces[0].SpellAbility()
	altSA := wear.Faces[1].SpellAbility()
	if frontSA == nil || altSA == nil ||
		frontSA.Params["ValidTgts"] == altSA.Params["ValidTgts"] ||
		frontSA.Params["ValidTgts"] == "" || altSA.Params["ValidTgts"] == "" {
		t.Fatalf("Wear // Tear halves do not carry distinct target declarations: %+v / %+v", frontSA, altSA)
	}

	eng, cfg := miscHandsEngine(t, reg,
		[]string{"Wear"},
		nil,
		[]string{"Mirrorpool"},
		[]string{"Sol Ring", "Ghostly Prison"})
	pool := miscBoardObj(t, eng, 0, "Mirrorpool")
	// Mirrorpool enters tapped (its own ETB replacement); clear the tap so its
	// {2}{C}, {T}, Sacrifice ability is offered.
	eng.emit(events.Event{Kind: events.Untap, Obj: pool})
	eng.priorityRound()
	// Fund seat 0: the fused cast is {1}{R}{W} and the copy ability is
	// {2}{C} plus the tap. The pool persists across the cast.
	addMana(t, eng, 0, "RWCCCG")

	wearObj := miscHandObj(t, eng, 0, "Wear")
	fuse := splitOption(t, eng, wearObj, "fuse")
	if fuse == nil {
		t.Fatalf("Wear // Tear fused cast not offered: %+v", castOptions(t, eng))
	}
	submitChoices(t, eng, fuse.Index)

	// The fused cast asks each half's own target in order: Wear's artifact,
	// then Tear's enchantment.
	ring := miscBoardObj(t, eng, 1, "Sol Ring")
	prison := miscBoardObj(t, eng, 1, "Ghostly Prison")
	answerDecl := func(want, other state.ObjID, stage int) {
		t.Helper()
		d := eng.Pending()
		if d == nil || d.Kind != decision.KTarget {
			t.Fatalf("fused cast target stage %d: pending=%+v, want target", stage, d)
		}
		found := -1
		for _, o := range d.Options {
			if o.Obj == want {
				found = o.Index
			}
			if o.Obj == other {
				t.Fatalf("stage %d offered the other half's target: %+v", stage, d.Options)
			}
		}
		if found < 0 {
			t.Fatalf("stage %d: no option for %d: %+v", stage, want, d.Options)
		}
		submitChoices(t, eng, found)
	}
	answerDecl(ring, prison, 0)
	answerDecl(prison, ring, 1)

	// Wear // Tear sits on the stack; seat 0 keeps priority and activates
	// Mirrorpool's copy ability targeting it. Mirrorpool's ability indices are
	// its A: list -- the {T}: mana ability is 0 and the copy is 1.
	ab := abilityOption(t, eng, pool, 1)
	submitChoices(t, eng, ab.Index)
	d := eng.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the copy ability's target ask, got %+v", d)
	}
	spellOpt := -1
	for _, o := range d.Options {
		if o.Obj == wearObj {
			spellOpt = o.Index
		}
	}
	if spellOpt < 0 {
		t.Fatalf("copy ability did not offer the fused Wear // Tear on the stack: %+v", d.Options)
	}
	submitChoices(t, eng, spellOpt)

	// The ability resolves, putting a CR 707.10 copy of the fused spell on the
	// stack. The election asks the FIRST declaration (Wear's artifact).
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "copy_targets" {
		t.Fatalf("expected the copy's first per-declaration ask, got %+v", d)
	}
	copyID := d.Source
	co := eng.G.Obj(copyID)
	if co == nil || !co.IsCopy || co.Zone != state.ZStack || !co.CopyMayChooseTarget {
		t.Fatalf("copy ask source %d is not an electing stack copy: %+v", copyID, co)
	}
	// PRECONDITION: this is a FUSED copy, so both declarations are in play.
	if co.CastFlags&state.FlagFused == 0 {
		t.Fatalf("copy %d does not carry FlagFused: cast flags %v", copyID, co.CastFlags)
	}
	if len(co.Targets) != 2 {
		t.Fatalf("copy inherited %d targets, want 2: %+v", len(co.Targets), co.Targets)
	}
	// Stage 0 is Wear's artifact declaration: Sol Ring is offered, Ghostly
	// Prison (an enchantment) is not.
	stage0Ring, stage0Prison := -1, -1
	for _, o := range d.Options {
		switch o.Obj {
		case ring:
			stage0Ring = o.Index
		case prison:
			stage0Prison = o.Index
		}
	}
	if stage0Ring < 0 {
		t.Fatalf("stage 0 (Wear's artifact declaration) did not offer Sol Ring: %+v", d.Options)
	}
	if stage0Prison >= 0 {
		t.Fatalf("stage 0 offered the enchantment the ALTERNATE declaration demands: %+v", d.Options)
	}
	if len(d.Options) == 0 || d.Options[0].Obj != ring {
		t.Fatalf("stage 0 did not keep the inherited artifact first: %+v", d.Options)
	}
	submitChoices(t, eng, stage0Ring)

	// THE FIX: a SECOND ask follows, for Tear's enchantment declaration, and
	// it offers the enchantment -- not the artifact already spent.
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "copy_targets" {
		t.Fatalf("expected the copy's second per-declaration ask (Tear's enchantment), got %+v", d)
	}
	stage1Prison, stage1Ring := -1, -1
	for _, o := range d.Options {
		switch o.Obj {
		case prison:
			stage1Prison = o.Index
		case ring:
			stage1Ring = o.Index
		}
	}
	if stage1Prison < 0 {
		t.Fatalf("stage 1 (Tear's enchantment declaration) did not offer Ghostly Prison: %+v", d.Options)
	}
	if stage1Ring >= 0 {
		t.Fatalf("stage 1 re-offered the artifact the front declaration already owns: %+v", d.Options)
	}
	if len(d.Options) == 0 || d.Options[0].Obj != prison {
		t.Fatalf("stage 1 did not keep the inherited enchantment first: %+v", d.Options)
	}
	submitChoices(t, eng, stage1Prison)

	// Both declarations recorded: the copy resolves BOTH halves, destroying
	// the artifact and the enchantment. Before the fix Tear had no target and
	// the enchantment survived.
	if n := countTargetsChosenOn(eng, copyID); n != 2 {
		t.Fatalf("copy recorded %d TargetsChosen events, want 2 (one per declaration)", n)
	}
	drainToEnd(t, eng, 60)
	if z := eng.G.Obj(ring).Zone; z != state.ZGraveyard {
		t.Fatalf("copy's Wear half did not destroy the artifact: Sol Ring zone=%s", z)
	}
	if z := eng.G.Obj(prison).Zone; z != state.ZGraveyard {
		t.Fatalf("copy's Tear half did not destroy the enchantment: Ghostly Prison zone=%s", z)
	}
	replayCheck(t, eng, cfg)
}

// TestCopyOfReunionKeepsThePowerBudget pins the second half of the row: the
// copy-target ask must re-run the cast ask's MaxTotalTargetPower$ prune and
// ride the same cumulative-budget wire contract (Decision.MaxSum over
// Option.Value). Reunion of the House carries MaxTotalTargetPower$ 10; a
// Mirrorpool copy of it must not offer the 11-power Polar Kraken and must
// carry Budgeted=true with MaxSum 10 on the copy ask, exactly as the cast
// ask did.
func TestCopyOfReunionKeepsThePowerBudget(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	reunion := searchCorpusCard(t, reg, "Reunion of the House")
	// PRECONDITION: the copied card really carries the cap, so a budget-less
	// copy ask would be the defect under test.
	if sa := reunion.Faces[0].SpellAbility(); sa == nil || sa.Params["MaxTotalTargetPower"] != "10" {
		t.Fatalf("Reunion of the House carries no MaxTotalTargetPower$ 10: %+v", sa)
	}

	eng, cfg := miscHandsEngine(t, reg,
		[]string{"Reunion of the House", "Polar Kraken", "Craw Wurm", "Serra Angel", "Hill Giant", "Grizzly Bears"},
		nil,
		[]string{"Mirrorpool"},
		nil)
	pool := miscBoardObj(t, eng, 0, "Mirrorpool")
	eng.emit(events.Event{Kind: events.Untap, Obj: pool})
	eng.priorityRound()
	// The target creatures move from seat 0's hand to its graveyard (Reunion's
	// Origin$ Graveyard).
	grave := map[string]state.ObjID{}
	for _, name := range []string{"Polar Kraken", "Craw Wurm", "Serra Angel", "Hill Giant", "Grizzly Bears"} {
		grave[name] = miscMoveByName(t, eng, 0, name, state.ZGraveyard)
	}
	addMana(t, eng, 0, "WWCCCCCCCCCC")

	reunionObj := miscHandObj(t, eng, 0, "Reunion of the House")
	submitChoices(t, eng, reunionCastOption(t, eng, reunionObj).Index)
	d := eng.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected Reunion's cast target ask, got %+v", d)
	}
	// PRECONDITION: the cast ask itself carries the budget, so the copy ask is
	// compared against a real reference rather than a zero value.
	if !d.Budgeted || d.MaxSum != 10 {
		t.Fatalf("cast ask budget = %v/%d, want Budgeted true / MaxSum 10", d.Budgeted, d.MaxSum)
	}
	idxOf := func(id state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id {
				return o.Index
			}
		}
		t.Fatalf("cast ask did not offer %d: %+v", id, d.Options)
		return -1
	}
	submitChoices(t, eng, idxOf(grave["Craw Wurm"]), idxOf(grave["Serra Angel"]))

	// Copy the Reunion spell on the stack through Mirrorpool.
	ab := abilityOption(t, eng, pool, 1)
	submitChoices(t, eng, ab.Index)
	d = eng.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the copy ability's target ask, got %+v", d)
	}
	spellOpt := -1
	for _, o := range d.Options {
		if o.Obj == reunionObj {
			spellOpt = o.Index
		}
	}
	if spellOpt < 0 {
		t.Fatalf("copy ability did not offer Reunion on the stack: %+v", d.Options)
	}
	submitChoices(t, eng, spellOpt)

	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "copy_targets" {
		t.Fatalf("expected the copy's target ask, got %+v", d)
	}
	// THE FIX: the copy ask re-runs the capped census and the budget contract.
	if !d.Budgeted || d.MaxSum != 10 {
		t.Fatalf("copy ask budget = %v/%d, want Budgeted true / MaxSum 10", d.Budgeted, d.MaxSum)
	}
	offerSet := map[state.ObjID]decision.Option{}
	for _, o := range d.Options {
		offerSet[o.Obj] = o
	}
	if _, ok := offerSet[grave["Polar Kraken"]]; ok {
		t.Fatalf("copy ask offered the 11-power Polar Kraken under the cap of 10: %+v", d.Options)
	}
	if o, ok := offerSet[grave["Craw Wurm"]]; !ok || o.Value != 6 {
		t.Fatalf("copy ask's Craw Wurm option = %+v (present %v), want Value 6", o, ok)
	}
	// The copy inherits the cast's two targets; keeping them resolves both.
	submitChoices(t, eng, d.Options[0].Index, d.Options[1].Index)
	drainToEnd(t, eng, 60)
	if z := eng.G.Obj(grave["Craw Wurm"]).Zone; z != state.ZBattlefield {
		t.Fatalf("copy's Reunion did not return Craw Wurm: zone=%s", z)
	}
	if z := eng.G.Obj(grave["Serra Angel"]).Zone; z != state.ZBattlefield {
		t.Fatalf("copy's Reunion did not return Serra Angel: zone=%s", z)
	}
	replayCheck(t, eng, cfg)
}

// declaration regression guard for the per-declaration rewrite: a copy of a
// non-fused spell with one declaration that demands two targets still gets
// ONE ask whose leading keep-current slots reproduce the inherited targets in
// order and whose Min is the declaration's own requirement. It is the
// byte-identical single-declaration path the fused case must not disturb.
func TestCopyOfTwoTargetSpellAsksForBothTargetsKeepsFlatList(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	eng, _ := miscHandsEngine(t, reg,
		[]string{"Reckless Spite"},
		nil,
		[]string{"Mirrorpool"},
		[]string{"Grizzly Bears", "Grizzly Bears", "Elvish Mystic"})
	pool := miscBoardObj(t, eng, 0, "Mirrorpool")
	eng.emit(events.Event{Kind: events.Untap, Obj: pool})
	eng.priorityRound()
	addMana(t, eng, 0, "BBCCCC")

	spiteObj := miscHandObj(t, eng, 0, "Reckless Spite")
	submitChoices(t, eng, miscCastOption(t, eng, spiteObj))
	d := eng.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 {
		t.Fatalf("expected Reckless Spite's two-target cast ask, got %+v", d)
	}
	bearOpts := make([]int, 0, 2)
	for _, o := range d.Options {
		if o.Kind == "permanent" {
			if co := eng.G.Obj(o.Obj); co != nil && co.Face() != nil && co.Face().Name == "Grizzly Bears" {
				bearOpts = append(bearOpts, o.Index)
			}
		}
	}
	if len(bearOpts) != 2 {
		t.Fatalf("Reckless Spite did not offer both Grizzly Bears: %+v", d.Options)
	}
	submitChoices(t, eng, bearOpts...)

	ab := abilityOption(t, eng, pool, 1)
	submitChoices(t, eng, ab.Index)
	d = eng.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the copy ability's target ask, got %+v", d)
	}
	spellOpt := -1
	for _, o := range d.Options {
		if o.Obj == spiteObj {
			spellOpt = o.Index
		}
	}
	if spellOpt < 0 {
		t.Fatalf("copy ability did not offer Reckless Spite on the stack: %+v", d.Options)
	}
	submitChoices(t, eng, spellOpt)

	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "copy_targets" {
		t.Fatalf("expected the copy's new-target ask, got %+v", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("single-declaration copy ask bounds = %d..%d, want 2..2", d.Min, d.Max)
	}
	if len(d.Options) < 2 || d.Options[0].Kind != "permanent" || d.Options[1].Kind != "permanent" {
		t.Fatalf("single-declaration copy ask did not keep both inherited targets first: %+v", d.Options)
	}
	submitChoices(t, eng, d.Options[0].Index, d.Options[1].Index)
	if n := countTargetsChosenOn(eng, d.Source); n != 2 {
		t.Fatalf("copy recorded %d TargetsChosen events, want 2", n)
	}
}
