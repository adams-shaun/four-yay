package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCopyOfTwoTargetSpellAsksForBothTargets pins the multi-target half of CR
// 707.10c: AskCopyTargets must preserve the COPIED spell's whole target
// requirement, not just one slot. MayChooseTarget$ True is not restricted to
// one-target spells, so a copy of a spell that demands two targets must pose a
// decision that accepts two -- otherwise the answer replaces the copy's
// complete target list with a single target and the copy resolves short.
//
// The carriers are the REAL corpus cards: Reckless Spite (an instant with
// TargetMin$ 2 | TargetMax$ 2) cast by seat 0 and copied through Mirrorpool's
// MayChooseTarget$ copy ability. No synthetic fixture card takes part.
//
// The keep-current answer must reproduce the ORIGINAL two targets, so the
// assertion is on the copy's target list after answering, not merely on d.Min.
func TestCopyOfTwoTargetSpellAsksForBothTargets(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	eng, cfg := miscHandsEngine(t, reg,
		[]string{"Reckless Spite"},
		nil,
		[]string{"Mirrorpool"},
		[]string{"Grizzly Bears", "Grizzly Bears", "Elvish Mystic"})
	pool := miscBoardObj(t, eng, 0, "Mirrorpool")
	// Mirrorpool enters tapped (its own ETB replacement); clear the tap so its
	// {2}{C}, {T}, Sacrifice ability is offered.
	eng.emit(events.Event{Kind: events.Untap, Obj: pool})
	eng.priorityRound()
	// Fund seat 0: Reckless Spite costs {1}{B}{B}; the copy ability costs
	// {2}{C} plus the tap. The pool persists across the cast.
	addMana(t, eng, 0, "BBCCCC")

	// PRECONDITION: the copied spell really demands two targets, so a
	// one-target decision would be the defect under test.
	spite := searchCorpusCard(t, reg, "Reckless Spite")
	sa := spite.Faces[0].SpellAbility()
	if sa == nil || sa.Params["TargetMin"] != "2" || sa.Params["TargetMax"] != "2" {
		t.Fatalf("Reckless Spite is not a two-target spell: %+v", sa)
	}

	// Cast Reckless Spite, choosing both seat-1 creatures.
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

	// Reckless Spite sits on the stack; seat 0 keeps priority and activates
	// Mirrorpool's copy ability targeting it. Mirrorpool's ability indices are
	// its A: list -- the {T}: mana ability is 0 and the copy is 1.
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
		t.Fatalf("copy ability did not offer the Reckless Spite on the stack: %+v", d.Options)
	}
	submitChoices(t, eng, spellOpt)

	// The ability resolves, putting a CR 707.10 copy of Reckless Spite on the
	// stack; AskCopyTargets must now demand TWO new targets.
	d = passUntilNonPriority(t, eng, 30)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "copy_targets" {
		t.Fatalf("expected the copy's new-target ask, got %+v", d)
	}
	copyID := d.Source
	co := eng.G.Obj(copyID)
	if co == nil || !co.IsCopy || co.Zone != state.ZStack || !co.CopyMayChooseTarget {
		t.Fatalf("copy ask source %d is not an electing stack copy: %+v", copyID, co)
	}
	if len(co.Targets) != 2 {
		t.Fatalf("copy inherited %d targets, want 2: %+v", len(co.Targets), co.Targets)
	}
	// THE FIX: the ask accepts the copy's required count, not one.
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("copy-target ask bounds = %d..%d, want 2..2 (the copied spell's requirement)", d.Min, d.Max)
	}
	// Keep-current bookkeeping: the first two options reproduce the copy's
	// inherited targets in order.
	if len(d.Options) < 2 {
		t.Fatalf("copy-target ask offered %d options, want the two keep-current slots at least", len(d.Options))
	}
	for i, want := range co.Targets {
		got := d.Options[i]
		if got.Kind != "permanent" || got.Obj != want.Obj {
			t.Fatalf("keep-current option %d = %+v, want inherited target %+v", i, got, want)
		}
	}

	// The one-home rule: the real bot's answer to this ask must pass
	// Decision.Validate (a bot that submitted one target against Min 2 would
	// livelock), and it must pick exactly the required two.
	bot := newTestBot(4242)
	intent := bot.answer(eng, d)
	if err := d.Validate(intent); err != nil {
		t.Fatalf("the bot's answer %+v failed Validate: %v", intent.Choices, err)
	}
	if len(intent.Choices) != 2 {
		t.Fatalf("the bot picked %d targets, want the required 2", len(intent.Choices))
	}

	// Answer with both keep-current options. The answer drives the copy's
	// resolution synchronously, so assert on the recorded targets through the
	// log rather than the stack object's live list (which a zone change
	// clears): a two-target answer records a replace plus an append, while the
	// one-target defect records exactly one event.
	submitChoices(t, eng, d.Options[0].Index, d.Options[1].Index)
	if n := countTargetsChosenOn(eng, copyID); n != 2 {
		t.Fatalf("copy recorded %d TargetsChosen events, want 2 (both inherited targets)", n)
	}

	// The copy resolves first, destroying both bears; the original then
	// fizzles (its targets are gone), so only the copy does the work. A
	// livelock or a short answer would leave a bear alive.
	drainToEnd(t, eng, 60)
	for _, id := range eng.G.Zone(state.ZBattlefield, 1) {
		if o := eng.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
			t.Fatalf("Grizzly Bears %d survived the copy's two-target resolution", id)
		}
	}
	replayCheck(t, eng, cfg)
}
