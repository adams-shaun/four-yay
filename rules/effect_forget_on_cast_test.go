package rules

// Task param:api:Effect.ForgetOnCast — the Effect primitive's ForgetOnCast$
// cast-driven lifetime and the Effect-delivered cost-modifier statics
// (Mode$ AlternativeCost / Mode$ ReduceCost) it rides. The filing carrier is
// Marshland Bloodcaster's "Rather than pay the mana cost of the NEXT spell
// you cast this turn, you may pay life equal to that spell's mana value":
// the activated AB$ Effect registers an AlternativeCost static whose Cost$
// is the dynamic PayLife<ConvertedManaCost>, and ForgetOnCast$ Card.YouCtrl
// ends the grant once one qualifying spell is cast.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func corpusNamed(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %q", name)
	}
	return c
}

// effectGrantEngine is the cascadeTestEngine pattern over the named cards:
// battlefield cards land on seat 0's battlefield and hand cards stay in its
// hand, every placement a logged MoveZone a replay reproduces.
func effectGrantEngine(t *testing.T, seed uint64, battlefield, hand []string) (*Engine, Config, []state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	var deck []*cards.Card
	for _, name := range battlefield {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for _, name := range hand {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, mountain)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"caster", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	// The protagonist cards to their zones. The named battlefield cards are
	// seated now (summoning sick until their controller's next turn); the
	// named hand cards are already in hand when the opening deal drew them
	// or are pulled there the same logged way.
	var battleIDs, handIDs []state.ObjID
	for _, name := range battlefield {
		battleIDs = append(battleIDs, searchMoveByName(t, e, name, state.ZBattlefield))
	}
	for _, name := range hand {
		handIDs = append(handIDs, searchMoveByName(t, e, name, state.ZHand))
	}
	return e, cfg, battleIDs, handIDs
}

// effectAbilityOption finds the AB$ Effect activation option of id and, as a
// precondition, asserts the creature is on the battlefield (the grant only
// exists while the source is — a setup that forgot to seat the card would
// otherwise strand the rest of the test on a vacuous pass).
func effectAbilityOption(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Effect source %d not on the battlefield (=%v)", id, o)
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending while searching for the Effect activation")
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id {
			return o
		}
	}
	t.Fatalf("no ability option for %d: %+v", id, d.Options)
	return decision.Option{}
}

// aliveCostStaticEntries counts the registry entries carrying a
// cost-modifier static of the named mode (the grant the cast sweep must end).
func aliveCostStaticEntries(e *Engine, mode string) int {
	n := 0
	for _, ce := range e.active() {
		if ce.CostStaticMode == mode {
			n++
		}
	}
	return n
}

func TestMarshlandBloodcasterAlternativeCostPaysLifeOnceForTheNextSpell(t *testing.T) {
	t.Parallel()
	// Marshland Bloodcaster: the two cast spells have different mana values
	// (2 then 3), so the dynamic PayLife<ConvertedManaCost> pricing is
	// observable spell by spell.
	e, cfg, battle, handIDs := effectGrantEngine(t, 9301,
		[]string{"Marshland Bloodcaster"},
		[]string{"Night's Whisper", "Divination"})
	bloodcaster := battle[0]
	whisper, divination := handIDs[0], handIDs[1]
	// Precondition: the payer has enough life for both reads (2 and 3) and
	// NO mana that could pay the plain costs — the alternative is the only
	// affordable way to cast the first spell.
	if e.G.Players[0].Life < 5 {
		t.Fatalf("life %d too low for the test's pricing assertions", e.G.Players[0].Life)
	}
	e.G.Players[0].Pool = state.Mana{}
	// The Bloodcaster is summoning sick on turn 1; drive to its first
	// untapped, non-sick turn (turn 3, seat 0's Main1 — turn 2 is seat 1's).
	driveToStep(t, e, 3, 0, state.StepMain1)
	if got := e.G.Players[0].Life; got < 5 {
		t.Fatalf("life after driving = %d, want >= 5", got)
	}
	if e.G.Obj(bloodcaster).Tapped {
		t.Fatal("precondition: Bloodcaster must be untapped")
	}
	if mv := e.G.Obj(whisper).Face().ManaValue(); mv != 2 {
		t.Fatalf("precondition: Night's Whisper mana value = %d, want 2", mv)
	}
	if mv := e.G.Obj(divination).Face().ManaValue(); mv != 3 {
		t.Fatalf("precondition: Divination mana value = %d, want 3", mv)
	}

	// Activate the ability ({1}{B}, {T}).
	addMana(t, e, 0, "CB")
	opt := effectAbilityOption(t, e, bloodcaster)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if n := aliveCostStaticEntries(e, "AlternativeCost"); n != 1 {
		t.Fatalf("after the activation %d AlternativeCost entries are registered, want 1", n)
	}

	// The next spell (Divination, mana value 3, a body that touches no
	// life) offers the alternative: pay 3 life instead of {2}{U}.
	d := e.Pending()
	alts := altCastOptions(d.Options, divination)
	if len(alts) != 1 {
		t.Fatalf("want exactly one alternative-cost option for Divination, got %d (%+v)", len(alts), d.Options)
	}
	lifeBefore := e.G.Players[0].Life
	submitChoices(t, e, alts[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != lifeBefore-3 {
		t.Fatalf("life after the alternative cast = %d, want %d (mana value 3 paid as life)", got, lifeBefore-3)
	}
	if total := poolTotal(e.G.Players[0].Pool); total != 0 {
		t.Fatalf("the alternative cost must not touch the mana pool, got %d units", total)
	}
	if e.G.Obj(divination).Zone != state.ZGraveyard {
		t.Fatalf("Divination in %s, want the graveyard", e.G.Obj(divination).Zone)
	}
	// ForgetOnCast$ Card.YouCtrl: the qualifying cast consumed the grant.
	if n := aliveCostStaticEntries(e, "AlternativeCost"); n != 0 {
		t.Fatalf("after the qualifying cast %d AlternativeCost entries remain, want 0 (ForgetOnCast$ ended the grant)", n)
	}

	// A SECOND spell that turn pays mana again: no alternative option, and
	// the plain cast charges the printed cost ({1}{B} -- Night's Whisper's
	// own body later loses 2 life, which the assertion below accounts for).
	addMana(t, e, 0, "CB")
	d = e.Pending()
	if alts := altCastOptions(d.Options, whisper); len(alts) != 0 {
		t.Fatalf("second spell still offered %d alternative-cost option(s), want none", len(alts))
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == whisper {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no plain cast option for Night's Whisper: %+v", d.Options)
	}
	lifeBefore = e.G.Players[0].Life
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != lifeBefore-2 {
		t.Fatalf("second spell = %d life, want the printed mana charged plus its own lose-2 body (%d)", got, lifeBefore-2)
	}
	if pool := e.G.Players[0].Pool; pool[state.MB] != 0 || pool[state.MC] != 0 {
		t.Fatalf("Night's Whisper's {1}{B} left %+v in the pool, want it spent", e.G.Players[0].Pool)
	}
	if e.G.Obj(whisper).Zone != state.ZGraveyard {
		t.Fatalf("Night's Whisper in %s, want the graveyard", e.G.Obj(whisper).Zone)
	}
	replayCheck(t, e, cfg)
}

func TestKazaRoilChaserGrantedReductionIsOneShotAndKindScoped(t *testing.T) {
	t.Parallel()
	// Kaza's activation (Cost$ T) registers a Mode$ ReduceCost static with
	// Amount$ Count$ChosenNumber (SetChosenNumber$ X at resolution) and
	// ValidCard$ Instant,Sorcery; ForgetOnCast$ Instant.YouCtrl,Sorcery.
	// YouCtrl ends it after the first INSTANT/SORCERY cast -- a creature
	// spell cast in between must NOT consume it.
	e, cfg, battle, handIDs := effectGrantEngine(t, 9317,
		[]string{"Kaza, Roil Chaser", "Fugitive Wizard"},
		[]string{"Fugitive Wizard", "Night's Whisper", "Divination"})
	creature, whisper, divination := handIDs[0], handIDs[1], handIDs[2]
	kaza := battle[0]
	// Precondition the wizard count is 2 (Kaza itself is a Wizard): it is
	// the amount the reduction must price, and a setup that lost a wizard
	// would silently halve it.
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			for _, ty := range o.Face().Types {
				if ty == "Wizard" {
					n++
				}
			}
		}
	}
	if n != 2 {
		t.Fatalf("precondition: %d Wizards on the battlefield, want 2 (Kaza itself counts)", n)
	}
	if mv := e.G.Obj(creature).Face().ManaValue(); mv != 1 {
		t.Fatalf("precondition: Fugitive Wizard mana value = %d, want 1", mv)
	}
	e.G.Players[0].Pool = state.Mana{}
	driveToStepAny(t, e, 3, 0, state.StepMain1)
	// Kaza's activation is {T}-only: no mana is owed or added.
	opt := effectAbilityOption(t, e, kaza)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if n := aliveCostStaticEntries(e, "ReduceCost"); n != 1 {
		t.Fatalf("after the activation %d ReduceCost entries are registered, want 1", n)
	}

	// A CREATURE spell first (Fugitive Wizard, {U}): the ValidCard$ scope
	// keeps the grant alive -- the creature cast must pay full price and
	// must not consume the instant/sorcery-only reduction.
	addMana(t, e, 0, "U")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == creature {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Fugitive Wizard: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if pool := e.G.Players[0].Pool; pool[state.MU] != 0 {
		t.Fatalf("creature spell left %d blue in the pool, want the full {U} spent (no reduction)", pool[state.MU])
	}
	if n := aliveCostStaticEntries(e, "ReduceCost"); n != 1 {
		t.Fatalf("the creature cast left %d ReduceCost entries, want 1 (out of the grant's ValidCard$ scope)", n)
	}

	// The next sorcery (Night's Whisper, {1}{B}) is reduced by X=2: the
	// generic pip is gone, the black pip stays. The offer gate priced the
	// REDUCED cost -- the plain cast is offered with only the black pip in
	// the pool (a full-price gate would have withheld the option entirely).
	addMana(t, e, 0, "B")
	d = e.Pending()
	idx = -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == whisper {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Night's Whisper under the granted reduction: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if pool := e.G.Players[0].Pool; pool[state.MB] != 0 {
		t.Fatalf("reduced Night's Whisper left %d black in the pool, want {1}{B} reduced to {B} spent", pool[state.MB])
	}
	// The instant/sorcery cast consumed the grant.
	if n := aliveCostStaticEntries(e, "ReduceCost"); n != 0 {
		t.Fatalf("after the qualifying cast %d ReduceCost entries remain, want 0 (ForgetOnCast$ ended the grant)", n)
	}

	// The second sorcery (Divination, {2}{U}) pays full price again.
	addMana(t, e, 0, "CUU")
	d = e.Pending()
	idx = -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == divination {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no plain cast option for Divination: %+v", d.Options)
	}
	poolBefore := e.G.Players[0].Pool
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if pool := e.G.Players[0].Pool; pool[state.MU] != 0 || pool[state.MC] != 0 {
		t.Fatalf("second sorcery left %+v in the pool (before: %+v), want the full {2}{U} spent",
			pool, poolBefore)
	}
	replayCheck(t, e, cfg)
}

// driveToStepAny is driveToStep for a battlefield that can attack: besides
// priority passes it answers the declare-attackers decision with the empty
// declaration (decline every offered attacker) -- Kaza, Roil Chaser has
// haste, so seat 0's first combat poses an attackers ask the plain helper
// cannot answer.
func driveToStepAny(t *testing.T, e *Engine, turn int32, active state.PlayerID, step state.Step) {
	t.Helper()
	for i := 0; i < 4000; i++ {
		if e.G.Turn == turn && e.G.Active == active && e.G.Step == step {
			return
		}
		if e.G.Over {
			t.Fatalf("game ended before reaching turn %d seat %d step %s", turn, active, step)
		}
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("nil decision while driving")
		}
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
		case decision.KAttackers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err != nil {
				t.Fatalf("submit empty attackers: %v", err)
			}
		default:
			t.Fatalf("unexpected decision kind %v while driving: %+v", d.Kind, d)
		}
	}
	t.Fatalf("did not reach turn %d seat %d step %s within the pass budget", turn, active, step)
}
