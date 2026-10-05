package rules

// stat:ActivateAbilityAsIfHaste (CR 302.6's activation exception) — "You may
// activate abilities of creatures you control as though those creatures had
// haste." Shang-Chi, Master of Kung Fu is the Standard carrier; Dynaheir,
// Invoker Adept, Thousand-Year Elixir and Tyvar, Jubilant Brawler are the
// corpus-wide rest.
//
// This is the activation-sickness exception only: it lets the static's
// controller pay a {T} cost with a summoning-sick creature. It is read through
// activatesAsIfHaste (rules/activateasifhaste.go) and threaded into
// pay.TapFlagsSick, the ONE activation-sickness predicate.

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// activateAsIfHasteCorpusCarriers walks both delivery routes (printed S: and
// Effect-delivered bodies).
func activateAsIfHasteCorpusCarriers(reg *cards.Registry) map[string]bool {
	got := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, st := range f.Statics {
				if st.Mode == "ActivateAbilityAsIfHaste" {
					got[f.Name] = true
				}
			}
			f.EachRawEffectChild(func(ch cards.EffectChild) {
				if ch.Static != nil && ch.Static.Mode == "ActivateAbilityAsIfHaste" {
					got[f.Name] = true
				}
			})
		}
	}
	return got
}

const asIfHasteStaticSrc = "Name:Test Shang-Chi\nManaCost:2 G\nTypes:Legendary Creature Human Warrior\nPT:3/3\n" +
	"S:Mode$ ActivateAbilityAsIfHaste | ValidCard$ Creature.YouCtrl+inZoneBattlefield | Description$ You may activate abilities of creatures you control as though those creatures had haste.\n" +
	"Oracle:x\n"

const asIfHasteTapperSrc = "Name:Test Tapper\nManaCost:G\nTypes:Creature Elf\nPT:1/1\n" +
	"A:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add {G}.\n" +
	"Oracle:x\n"

// wantsActivation reports whether p's legal actions include an activation of
// obj.
func wantsActivation(e *Engine, p state.PlayerID, obj state.ObjID) bool {
	for _, o := range e.legalActions(p) {
		if o.Kind == "activate" && o.Obj == obj {
			return true
		}
	}
	return false
}

// TestActivateAbilityAsIfHasteOffersSickTapper is the behavioural leaf: a
// summoning-sick {T} mana source is withheld (asserted, so a vacuous setup
// fails), and an ActivateAbilityAsIfHaste static controlled by its controller
// makes the walk offer it again.
func TestActivateAbilityAsIfHasteOffersSickTapper(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	tapper := onBoardCard(t, e, 0, card(t, asIfHasteTapperSrc))

	// Precondition: the tapper is a summoning-sick creature and its {T}
	// activation is genuinely withheld, so the "offered" assertion below can
	// fail loudly rather than pass on a walk that always offers it.
	e.G.Obj(tapper).SummonSick = true
	if !e.G.Obj(tapper).SummonSick || !e.IsCreature(tapper) {
		t.Fatal("precondition: the tapper must be a summoning-sick creature on the battlefield")
	}
	if wantsActivation(e, 0, tapper) {
		t.Fatal("precondition: the summoning-sick tapper was offered without the static")
	}

	onBoardCard(t, e, 0, card(t, asIfHasteStaticSrc))
	if got := e.activeStatics("ActivateAbilityAsIfHaste"); len(got) != 1 {
		t.Fatalf("precondition: activeStatics(ActivateAbilityAsIfHaste) = %d entries, want 1", len(got))
	}
	if !activatesAsIfHaste(e, tapper) {
		t.Fatal("activatesAsIfHaste did not select the controller's own creature")
	}
	if !wantsActivation(e, 0, tapper) {
		t.Fatal("ActivateAbilityAsIfHaste did not offer the summoning-sick tapper's {T} ability")
	}
}

// TestActivateAbilityAsIfHasteIsScopedToTheControllersCreatures pins the
// ValidCard$ scope: an opponent's ActivateAbilityAsIfHaste (Creature.YouCtrl
// from that opponent's perspective) does NOT exempt seat 0's summoning-sick
// creature.
func TestActivateAbilityAsIfHasteIsScopedToTheControllersCreatures(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	tapper := onBoardCard(t, e, 0, card(t, asIfHasteTapperSrc))
	e.G.Obj(tapper).SummonSick = true

	onBoardCard(t, e, 1, card(t, asIfHasteStaticSrc))
	if got := e.activeStatics("ActivateAbilityAsIfHaste"); len(got) != 1 {
		t.Fatalf("precondition: activeStatics(ActivateAbilityAsIfHaste) = %d entries, want 1", len(got))
	}
	if e.staticGateHolds(e.activeStatics("ActivateAbilityAsIfHaste")[0]) != true {
		t.Fatal("precondition: the opponent's unscoped fixture static must be gate-open")
	}
	if activatesAsIfHaste(e, tapper) {
		t.Fatal("an opponent's ActivateAbilityAsIfHaste exempted seat 0's creature (ValidCard$ scope broken)")
	}
	if wantsActivation(e, 0, tapper) {
		t.Fatal("opponent's ActivateAbilityAsIfHaste wrongly offered seat 0's sick tapper")
	}
}

// TestActivateAbilityAsIfHasteFalseGateDoesNotExempt pins that a false
// Condition$ gate blocks the exemption.
func TestActivateAbilityAsIfHasteFalseGateDoesNotExempt(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	tapper := onBoardCard(t, e, 0, card(t, asIfHasteTapperSrc))
	e.G.Obj(tapper).SummonSick = true
	gated := "Name:Test Gated Elixir\nManaCost:3\nTypes:Artifact\n" +
		"S:Mode$ ActivateAbilityAsIfHaste | ValidCard$ Creature.YouCtrl | Condition$ PlayerTurn | Description$ You may activate abilities of creatures you control as though those creatures had haste.\n" +
		"Oracle:x\n"
	onBoardCard(t, e, 0, card(t, gated))

	// The static's controller (seat 0) must not be the active player for the
	// gate to be false. threeSeatEngine's active seat is read, not assumed.
	svs := e.activeStatics("ActivateAbilityAsIfHaste")
	if len(svs) != 1 {
		t.Fatalf("precondition: activeStatics(ActivateAbilityAsIfHaste) = %d entries, want 1", len(svs))
	}
	e.G.Active = 1
	if e.staticGateHolds(svs[0]) {
		t.Fatal("precondition: the fixture's Condition$ PlayerTurn gate must be false with seat 1 active")
	}
	if activatesAsIfHaste(e, tapper) {
		t.Fatal("false-gated ActivateAbilityAsIfHaste exempted a summoning-sick creature")
	}
}

// TestActivateAbilityAsIfHasteDoesNotGrantCombatHaste pins the deliberate
// narrow scope: the static exempts {T} activation, it does not make the
// creature able to attack (CR 302.6's combat half is untouched).
func TestActivateAbilityAsIfHasteDoesNotGrantCombatHaste(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	tapper := onBoardCard(t, e, 0, card(t, asIfHasteTapperSrc))
	e.G.Obj(tapper).SummonSick = true
	onBoardCard(t, e, 0, card(t, asIfHasteStaticSrc))

	if e.HasKeyword(tapper, "Haste") {
		t.Fatal("ActivateAbilityAsIfHaste wrongly granted the Haste keyword (combat haste)")
	}
}

// TestActivateAbilityAsIfHasteFlowsThroughPayGate pins that the read reaches
// the shared pay gate: pay.TapFlagsSick is sick before the static and not
// sick after it.
func TestActivateAbilityAsIfHasteFlowsThroughPayGate(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	tapper := onBoardCard(t, e, 0, card(t, asIfHasteTapperSrc))
	e.G.Obj(tapper).SummonSick = true

	if !pay.TapFlagsSick(asPayer(e), tapper, true, false, activatesAsIfHaste(e, tapper)) {
		t.Fatal("precondition: a summoning-sick tapper must be tap-sick without the static")
	}

	onBoardCard(t, e, 0, card(t, asIfHasteStaticSrc))
	if pay.TapFlagsSick(asPayer(e), tapper, true, false, activatesAsIfHaste(e, tapper)) {
		t.Fatal("ActivateAbilityAsIfHaste did not clear the pay.TapFlagsSick withholding")
	}
}

// TestActivateAbilityAsIfHastePrimitiveIsRegistered pins the support
// declaration the skip gate reads.
func TestActivateAbilityAsIfHastePrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["stat:ActivateAbilityAsIfHaste"] {
		t.Fatal(`effects.Supported() is missing "stat:ActivateAbilityAsIfHaste"`)
	}
}

// activateAsIfHasteCarriers is the corpus-wide printed class. A corpus-pin
// bump that adds a carrier fails loudly.
var activateAsIfHasteCarriers = []string{
	"Dynaheir, Invoker Adept",
	"Shang-Chi, Master of Kung Fu",
	"Thousand-Year Elixir",
	"Tyvar, Jubilant Brawler",
}

// TestActivateAbilityAsIfHasteClassCensus pins the printed carrier set.
func TestActivateAbilityAsIfHasteClassCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := activateAsIfHasteCorpusCarriers(reg)
	list := make([]string, 0, len(got))
	for n := range got {
		list = append(list, n)
	}
	sort.Strings(list)
	want := append([]string(nil), activateAsIfHasteCarriers...)
	sort.Strings(want)
	if len(list) != len(want) {
		t.Fatalf("ActivateAbilityAsIfHaste carriers: got %d, want %d\ngot:  %s\nwant: %s", len(list), len(want), joinQuoted(list), joinQuoted(want))
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("ActivateAbilityAsIfHaste carrier %d = %q, want %q\ngot:  %s\nwant: %s", i, list[i], want[i], joinQuoted(list), joinQuoted(want))
		}
	}
}
