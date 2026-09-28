package rules

// The Cultivate-family placement-leg truth fix (task fb-20260922T235033Z):
// a search's head ask picks cards BY NAME and (in the Reveal$ shapes) publicly
// reveals them, then its `ChangeType$ ...IsRemembered` placement legs pose
// follow-up asks. Those legs carry NoLooking$ True (Forge's "do not look
// again") and used to be built by effSearchLibrary as a row of identical blind
// "a card" options under a generic "Search a library: choose up to 1 card(s)"
// prompt -- hiding information the chooser already had and contradicting the
// leg's own SelectPrompt$.
//
// The fix names an option exactly when the choosing player already
// legitimately knows that card: it was publicly revealed, OR an earlier ask in
// the same chain offered it by name and this player picked it. A genuinely
// blind search stays blind (the fail-closed branch). These tests pin both
// knowledge channels, the SelectPrompt$ read, and the blind control.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// optionObjs returns each option's object id in wire order.
func optionObjs(d *decision.Decision) []state.ObjID {
	out := make([]state.ObjID, 0, len(d.Options))
	for _, o := range d.Options {
		out = append(out, o.Obj)
	}
	return out
}

// requireNamedOptions asserts no option is the blind placeholder -- the "the
// chooser already knows this card" half of the fix -- and that each label is
// the option object's real printed name.
func requireNamedOptions(t *testing.T, e *Engine, d *decision.Decision, context string) {
	t.Helper()
	for i, o := range d.Options {
		if o.Label == "a card" {
			t.Fatalf("%s: option %d is the blind placeholder: %+v", context, i, d.Options)
		}
		obj := e.G.Obj(o.Obj)
		if obj == nil || obj.Face() == nil {
			t.Fatalf("%s: option %d object %d has no face", context, i, o.Obj)
		}
		if o.Label != obj.Face().Name {
			t.Fatalf("%s: option %d label %q != printed name %q", context, i, o.Label, obj.Face().Name)
		}
	}
}

// sameObjMultiset reports whether two id slices hold the same multiset.
func sameObjMultiset(a, b []state.ObjID) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]state.ObjID(nil), a...)
	for _, x := range b {
		found := -1
		for i, y := range left {
			if y == x {
				found = i
				break
			}
		}
		if found < 0 {
			return false
		}
		left = append(left[:found], left[found+1:]...)
	}
	return true
}

// TestCultivatePlacementLegs drives Cultivate end to end: the head search picks
// two basic lands by name, and BOTH placement legs (battlefield, then hand)
// must label their options with the real basic-land names -- never "a card" --
// even though each leg carries NoLooking$ True, because the head's public
// reveal already taught the chooser which cards they are.
func TestCultivatePlacementLegs(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Cultivate")
	_, d0 := castSearchSpell(t, e, "Cultivate")
	if d0.Kind != decision.KChoose || len(d0.Options) < 2 {
		t.Fatalf("head = %v with %d options, want a KChoose with >=2", d0.Kind, len(d0.Options))
	}
	requireNamedOptions(t, e, d0, "head")
	picked := []decision.Option{d0.Options[0], d0.Options[1]}
	want := []state.ObjID{picked[0].Obj, picked[1].Obj}
	submitChoices(t, e, picked[0].Index, picked[1].Index)

	// Leg 0: put one onto the battlefield tapped.
	d1 := e.Pending()
	if d1 == nil || d1.Kind != decision.KChoose || d1.ResumeKind != "search" {
		t.Fatalf("battlefield leg pending = %+v, want a search KChoose", d1)
	}
	if got := d1.Prompt; got != "Select a card to put onto the battlefield" {
		t.Fatalf("battlefield leg prompt = %q, want the card's SelectPrompt$", got)
	}
	requireNamedOptions(t, e, d1, "battlefield leg")
	if !sameObjMultiset(optionObjs(d1), want) {
		t.Fatalf("battlefield leg offered %v, want the picked %v", optionObjs(d1), want)
	}
	placed := d1.Options[0]
	submitChoices(t, e, placed.Index)

	// Leg 1: put the other into your hand. This runs after the FIRST leg's
	// own suspension rebuilt the Ctx, so it also proves the known set survived
	// on the ask ride.
	d2 := e.Pending()
	if d2 == nil || d2.Kind != decision.KChoose || d2.ResumeKind != "search" {
		t.Fatalf("hand leg pending = %+v, want a search KChoose", d2)
	}
	if got := d2.Prompt; got != "Select a card to put into your hand" {
		t.Fatalf("hand leg prompt = %q, want the card's SelectPrompt$", got)
	}
	remaining := picked[0].Obj
	if placed.Obj == picked[0].Obj {
		remaining = picked[1].Obj
	}
	if len(d2.Options) != 1 || d2.Options[0].Obj != remaining {
		t.Fatalf("hand leg offered %v, want the one card not yet placed (%d)", optionObjs(d2), remaining)
	}
	requireNamedOptions(t, e, d2, "hand leg")
	submitChoices(t, e, d2.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}

// TestSearchLegSelectPrompt pins the SelectPrompt$ read on the search-ask path
// and the prompt-before-Mandatory-clamp bug together: every leg here is
// Min == Max == 1, so the old code advertised "choose up to 1 card(s)" while
// the card's own SelectPrompt$ asks for exactly one. Both legs must carry the
// script's prompt and no option may be blind.
func TestSearchLegSelectPrompt(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Kodama's Reach")
	_, d0 := castSearchSpell(t, e, "Kodama's Reach")
	if d0.Kind != decision.KChoose || len(d0.Options) < 2 {
		t.Fatalf("head = %v with %d options, want a KChoose with >=2", d0.Kind, len(d0.Options))
	}
	submitChoices(t, e, d0.Options[0].Index, d0.Options[1].Index)

	d1 := e.Pending()
	if d1 == nil || d1.Kind != decision.KChoose || d1.ResumeKind != "search" {
		t.Fatalf("battlefield leg pending = %+v, want a search KChoose", d1)
	}
	if d1.Min != 1 || d1.Max != 1 {
		t.Fatalf("battlefield leg = %d..%d, want the Mandatory$ 1..1", d1.Min, d1.Max)
	}
	if got := d1.Prompt; got != "Select a card to put onto the battlefield" {
		t.Fatalf("battlefield leg prompt = %q", got)
	}
	requireNamedOptions(t, e, d1, "battlefield leg")
	submitChoices(t, e, d1.Options[0].Index)

	d2 := e.Pending()
	if d2 == nil || d2.Kind != decision.KChoose || d2.ResumeKind != "search" {
		t.Fatalf("hand leg pending = %+v, want a search KChoose", d2)
	}
	if got := d2.Prompt; got != "Select a card to put into your hand" {
		t.Fatalf("hand leg prompt = %q", got)
	}
	requireNamedOptions(t, e, d2, "hand leg")
	submitChoices(t, e, d2.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}

// TestSearchLegNoRevealSameChooser pins knowledge channel (b): Final Parting's
// head carries NO Reveal$, so nothing is public -- but the head offered its
// cards BY NAME and the SAME player picks the placement leg, so the leg's
// options must still carry real names. This is the half a reveal-only fix
// would miss.
func TestSearchLegNoRevealSameChooser(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	if !headHasNoReveal(t, reg, "Final Parting") {
		t.Fatal("precondition: Final Parting's head must carry no Reveal$")
	}
	e, cfg := searchEngine(t, reg, "Final Parting")
	id := searchMoveByName(t, e, "Final Parting", state.ZHand)
	addMana(t, e, 0, "WUBRGBBCCCCCCCC")
	d0 := castFixture(t, e, id, -1)
	if d0 == nil || d0.Kind != decision.KChoose || len(d0.Options) < 2 {
		t.Fatalf("head pending = %+v, want a KChoose with >=2", d0)
	}
	requireNamedOptions(t, e, d0, "head")
	want := []state.ObjID{d0.Options[0].Obj, d0.Options[1].Obj}
	submitChoices(t, e, d0.Options[0].Index, d0.Options[1].Index)

	d1 := e.Pending()
	if d1 == nil || d1.Kind != decision.KChoose || d1.ResumeKind != "search" {
		t.Fatalf("hand leg pending = %+v, want a search KChoose", d1)
	}
	if d1.Player != 0 {
		t.Fatalf("hand leg chooser = seat %d, want the caster (0) who picked by name", d1.Player)
	}
	requireNamedOptions(t, e, d1, "no-reveal hand leg")
	if !sameObjMultiset(optionObjs(d1), want) {
		t.Fatalf("hand leg offered %v, want the picked %v", optionObjs(d1), want)
	}
	submitChoices(t, e, d1.Options[0].Index)
	if d2 := e.Pending(); d2 != nil && d2.Kind == decision.KChoose {
		submitChoices(t, e, d2.Options[0].Index)
	}
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}

// TestSearchLegOpponentChooser pins knowledge channel (a): Elemental
// Teachings' head publicly reveals its finds, and the leg is answered by the
// OPPONENT (Chooser$ Opponent), who never picked by name -- so the reveal
// alone must name the leg's options. A picked-by-name-only fix would leave
// this chooser blind.
func TestSearchLegOpponentChooser(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Elemental Teachings")
	_, d0 := castSearchSpell(t, e, "Elemental Teachings")
	if d0 == nil || d0.Kind != decision.KChoose || len(d0.Options) < 2 {
		t.Fatalf("head pending = %+v, want a KChoose with >=2", d0)
	}
	requireNamedOptions(t, e, d0, "head")
	// Pick two options with distinct names (DifferentNames$ True).
	seen := map[string]bool{}
	var picks []int
	var want []state.ObjID
	for _, o := range d0.Options {
		if seen[o.Label] {
			continue
		}
		seen[o.Label] = true
		picks = append(picks, o.Index)
		want = append(want, o.Obj)
		if len(picks) == 2 {
			break
		}
	}
	if len(picks) != 2 {
		t.Fatalf("head offered only %d distinct-name options, need 2", len(picks))
	}
	submitChoices(t, e, picks...)

	d1 := e.Pending()
	if d1 == nil || d1.Kind != decision.KChoose || d1.ResumeKind != "search" {
		t.Fatalf("graveyard leg pending = %+v, want a search KChoose", d1)
	}
	if d1.Player != 1 {
		t.Fatalf("graveyard leg chooser = seat %d, want the opponent (1)", d1.Player)
	}
	if got := d1.Prompt; got != "Select two cards to be put into the graveyard of CARDNAME's controller" {
		t.Fatalf("graveyard leg prompt = %q, want the card's SelectPrompt$", got)
	}
	requireNamedOptions(t, e, d1, "opponent graveyard leg")
	if !sameObjMultiset(optionObjs(d1), want) {
		t.Fatalf("graveyard leg offered %v, want the 2 revealed cards %v", optionObjs(d1), want)
	}
	submitChoices(t, e, d1.Options[0].Index, d1.Options[1].Index)
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}

// TestSearchLegStaysBlindWhenUnknown is the fail-closed control: an inline
// search whose head never revealed and never offered names (NoReveal$ True,
// NoLooking$ True) remembers its picks, and a placement leg asks the same
// player to move one -- but that player genuinely never saw the cards, so the
// leg's options MUST stay the blind "a card" placeholder. The SelectPrompt$
// is still honoured, which also proves the fixed builder ran.
func TestSearchLegStaysBlindWhenUnknown(t *testing.T) {
	t.Parallel()
	src := "Name:BlindSearch\nManaCost:1 G\nTypes:Sorcery\n" +
		"A:SP$ ChangeZone | Origin$ Library | Destination$ Library | ChangeType$ Land.Basic | " +
		"ChangeNum$ 2 | RememberChanged$ True | NoReveal$ True | NoLooking$ True | SubAbility$ Leg\n" +
		"SVar:Leg:DB$ ChangeZone | Origin$ Library | Destination$ Hand | ChangeType$ Land.IsRemembered | " +
		"ChangeNum$ 1 | Mandatory$ True | NoLooking$ True | SelectPrompt$ Select a hidden card\n" +
		"Oracle:x\n"
	e, cfg, id := newFixtureDeck(t, 9101, src)
	addMana(t, e, 0, "GC")
	d0 := castFixture(t, e, id, -1)
	if d0 == nil || d0.Kind != decision.KChoose || d0.ResumeKind != "search" {
		t.Fatalf("head pending = %+v, want a search KChoose", d0)
	}
	if len(d0.Options) < 2 {
		t.Fatalf("head offered %d options, need >=2", len(d0.Options))
	}
	for i, o := range d0.Options {
		if o.Label != "a card" {
			t.Fatalf("head option %d = %q, want blind (NoLooking$ True)", i, o.Label)
		}
		obj := e.G.Obj(o.Obj)
		if obj == nil || obj.Face() == nil || obj.Face().Name == "" {
			t.Fatalf("head option %d has no named card: %+v", i, o)
		}
	}
	submitChoices(t, e, d0.Options[0].Index, d0.Options[1].Index)

	d1 := e.Pending()
	if d1 == nil || d1.Kind != decision.KChoose || d1.ResumeKind != "search" {
		t.Fatalf("leg pending = %+v, want a search KChoose", d1)
	}
	if got := d1.Prompt; got != "Select a hidden card" {
		t.Fatalf("leg prompt = %q, want the SelectPrompt$ (proves the builder ran)", got)
	}
	for i, o := range d1.Options {
		if o.Label != "a card" {
			t.Fatalf("leg option %d = %q, must stay blind: the chooser never saw it", i, o.Label)
		}
	}
	submitChoices(t, e, d1.Options[0].Index)
	passUntilStackEmpty(t, e, 30)
	replayCheck(t, e, cfg)
}

// TestCultivateLegsEmitNoRevealNote guards the reveal surface: the fixed legs
// are a display-only change, so a placement leg must not start emitting extra
// public Note reveals of its own (the head already revealed).
func TestCultivateLegsEmitNoRevealNote(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Cultivate")
	_, d0 := castSearchSpell(t, e, "Cultivate")
	// Count the head's own reveal Note, then every Note the legs add. The
	// hand leg legitimately reveals what it moved to the hand (Cultivate's
	// DBChangeZone2 carries no NoReveal$), so the assertion is that the fix
	// adds exactly ZERO new Notes beyond that pre-existing one -- never a
	// per-option identity Note.
	submitChoices(t, e, d0.Options[0].Index, d0.Options[1].Index)
	start := len(e.L.Events)
	legs := 0
	for {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
			break
		}
		legs++
		submitChoices(t, e, d.Options[0].Index)
	}
	if legs != 2 {
		t.Fatalf("answered %d placement legs, want 2", legs)
	}
	passUntilStackEmpty(t, e, 30)
	notes := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Note && len(ev.IDs) > 0 {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("placement legs emitted %d identity Notes, want the 1 pre-existing hand-leg reveal", notes)
	}
	replayCheck(t, e, cfg)
}

// headHasNoReveal reports whether the named corpus card's library search head
// lacks Reveal$ True -- the precondition TestSearchLegNoRevealSameChooser
// depends on (an unrevealed head is what makes it channel (b) rather than (a)).
func headHasNoReveal(t *testing.T, reg *cards.Registry, name string) bool {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("missing corpus card %q", name)
	}
	for _, f := range c.Faces {
		for _, sa := range f.Abilities {
			if sa.API == "ChangeZone" && strings.EqualFold(sa.Params["Origin"], "Library") {
				return !strings.EqualFold(strings.TrimSpace(sa.Params["Reveal"]), "True")
			}
		}
	}
	return false
}
