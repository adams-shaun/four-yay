package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The monstrosity suite (task agent-20260919T190014Z): the Monstrosity$ read
// on AB$ PutCounter, the monstrous mark behind the events.AlterAttribute
// fold, the CR 701.31b once-only offer gate, the IsMonstrous filter
// predicate and the trig:BecomeMonstrous listener. Every end-to-end pin is a
// real corpus carrier.

// monstrosityAbilityIndex finds the monstrosity PutCounter ability on the
// face and returns its (pile) index, fatal when absent.
func monstrosityAbilityIndex(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		t.Fatalf("source %d has no face", id)
	}
	for i, sa := range o.Face().Abilities {
		if sa.Kind == "AB" && sa.API == "PutCounter" && sa.Params["Monstrosity"] != "" {
			return i
		}
	}
	t.Fatalf("%s has no Monstrosity$ PutCounter activated ability", o.Face().Name)
	return -1
}

// monstrousMarkEvents returns every AlterAttribute mark event in the log.
func monstrousMarkEvents(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.AlterAttribute && ev.Text == "Monstrous" {
			out = append(out, ev)
		}
	}
	return out
}

// monstrosityEngine places a corpus carrier on seat 0's battlefield, drives
// to Main1 and adds mana symbols to seat 0's pool. Seat 1's deck is all
// Mountains EXCEPT one Grizzly Bears at the library bottom, so a test that
// needs an opposing creature moves it with a logged MoveZone (crAbortMove)
// and the state replays byte-identically -- no eventless placement anywhere
// in this suite.
func monstrosityEngine(t *testing.T, name, mana string) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	carrier := searchCorpusCard(t, reg, name)
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	mountain := searchCorpusCard(t, reg, "Mountain")
	forest := searchCorpusCard(t, reg, "Forest")
	deck := []*cards.Card{carrier}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 0, 40)
	for i := 0; i < 39; i++ {
		opp = append(opp, mountain)
	}
	opp = append(opp, bear)
	cfg := Config{Seed: 9207, Names: []string{"monstrous", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	// The toss winner is seeded; whatever it is, get to a SEAT 0 priority
	// window in seat 0's Main1 (pass the other seat's priority and let the
	// turn advance), so the tests below always read seat 0's offer list.
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority || d.Player == 0 {
			break
		}
		passIdx := -1
		for _, op := range d.Options {
			if op.Kind == "pass" {
				passIdx = op.Index
			}
		}
		if passIdx < 0 {
			t.Fatalf("non-starting seat has no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{passIdx}}); err != nil {
			t.Fatalf("pass non-starting seat: %v", err)
		}
	}
	toMain1(t, e)
	if d := e.Pending(); d == nil || d.Player != 0 {
		t.Fatalf("precondition: no seat 0 priority window (pending %+v)", d)
	}
	id := searchMoveByName(t, e, name, state.ZBattlefield)
	if mana != "" {
		addMana(t, e, 0, mana)
	}
	return e, cfg, id
}

// TestGigglingSkitterspikeMonstrosityMarksOnceAndGateWithholds: the named
// card of the report. Activating {5}: Monstrosity 5 puts FIVE +1/+1 counters
// (not the Paramless-PutCounter default 1), emits one Monstrous mark whose
// Amount carries the count, and the CR 701.31b once-only gate withholds the
// ability on the now-monstrous creature even with the pool re-funded.
func TestGigglingSkitterspikeMonstrosityMarksOnceAndGateWithholds(t *testing.T) {
	t.Parallel()
	e, cfg, id := monstrosityEngine(t, "Giggling Skitterspike", "CCCCC")
	o := e.G.Obj(id)
	idx := monstrosityAbilityIndex(t, e, id)
	// Preconditions the assertions below depend on.
	if o.Monstrous || o.Counter("P1P1") != 0 {
		t.Fatalf("precondition: monstrous=%v counters=%d", o.Monstrous, o.Counter("P1P1"))
	}
	if _, ok := findAbilityOption(e, id, idx); !ok {
		t.Fatal("precondition: the monstrosity ability is not offered")
	}
	opt := abilityOption(t, e, id, idx)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	o = e.G.Obj(id)
	if o.Monstrous != true {
		t.Fatal("the creature is not monstrous after the ability resolved")
	}
	if got := o.Counter("P1P1"); got != 5 {
		t.Fatalf("want 5 +1/+1 counters, got %d", got)
	}
	marks := monstrousMarkEvents(e)
	if len(marks) != 1 || marks[0].Obj != id || marks[0].Amount != 5 {
		t.Fatalf("want exactly one Monstrous mark on %d with Amount 5, got %+v", id, marks)
	}
	// The once-only gate (CR 701.31b): re-fund the pool so the only thing
	// that can withhold the offer is the mark itself.
	addMana(t, e, 0, "CCCCC")
	if _, ok := findAbilityOption(e, id, idx); ok {
		t.Fatalf("monstrosity ability offered again on an already-monstrous creature: %+v", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}

// TestWildfireCerberusBecomeMonstrousTriggerFiresOnce: the BecomeMonstrous
// listener fires exactly once on the mark (the opponent loses exactly 2
// life -- a second firing would be 4) and its DamageAll body runs.
func TestWildfireCerberusBecomeMonstrousTriggerFiresOnce(t *testing.T) {
	t.Parallel()
	e, cfg, id := monstrosityEngine(t, "Wildfire Cerberus", "RRCCCCC")
	bear := crAbortMove(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if e.G.Players[1].Life != 20 {
		t.Fatalf("precondition: opponent life %d, want 20", e.G.Players[1].Life)
	}
	if dmg := bearDamage(e, bear); dmg != 0 {
		t.Fatal("precondition: bear already carries a Damage event")
	}
	idx := monstrosityAbilityIndex(t, e, id)
	if _, ok := findAbilityOption(e, id, idx); !ok {
		t.Fatal("precondition: the monstrosity ability is not offered")
	}
	opt := abilityOption(t, e, id, idx)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
		t.Fatalf("Monstrosity$ 1 put %d +1/+1 counters, want 1", got)
	}
	marks := monstrousMarkEvents(e)
	if len(marks) != 1 || marks[0].Amount != 1 {
		t.Fatalf("want exactly one Monstrous mark with Amount 1, got %+v", marks)
	}
	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("opponent life %d, want 18 (exactly one trigger firing of 2 damage)", life)
	}
	// The bear's marked damage clears at the turn's cleanup step, so the
	// hit is asserted from the log -- the event every consumer reads.
	if dmg := bearDamage(e, bear); dmg != 2 {
		t.Fatalf("log Damage events on the bear sum to %d, want 2", dmg)
	}
	replayCheck(t, e, cfg)
}

// bearDamage sums the log's Damage events naming the object.
func bearDamage(e *Engine, id state.ObjID) int32 {
	var n int32
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == id {
			n += ev.Amount
		}
	}
	return n
}

// TestFleecemaneLionIsMonstrousGrantsKeywords: the IsMonstrous filter
// predicate reads the mark in the layer walk -- the hexproof/indestructible
// static grants only after the mark, never before.
// TestFleecemaneLionIsMonstrousGrantsKeywords
func TestFleecemaneLionIsMonstrousGrantsKeywords(t *testing.T) {
	t.Parallel()
	e, cfg, id := monstrosityEngine(t, "Fleecemane Lion", "GGGGW")
	idx := monstrosityAbilityIndex(t, e, id)
	kws := e.Keywords(id)
	if slices.Contains(kws, "Hexproof") || slices.Contains(kws, "Indestructible") {
		t.Fatalf("precondition: keywords granted before the mark: %v", kws)
	}
	if e.G.Obj(id).Monstrous {
		t.Fatal("precondition: already monstrous")
	}
	if _, ok := findAbilityOption(e, id, idx); !ok {
		t.Fatal("precondition: the monstrosity ability is not offered")
	}
	opt := abilityOption(t, e, id, idx)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(id).Monstrous {
		t.Fatal("the lion is not monstrous after the ability resolved")
	}
	kws = e.Keywords(id)
	if !slices.Contains(kws, "Hexproof") || !slices.Contains(kws, "Indestructible") {
		t.Fatalf("monstrous lion lacks hexproof/indestructible (derived: %v)", kws)
	}
	// The gate reads the same mark the static does.
	addMana(t, e, 0, "GGGW")
	if _, ok := findAbilityOption(e, id, idx); ok {
		t.Fatalf("monstrosity ability offered again on the monstrous lion: %+v", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}

// TestVitalityHunterAnnouncedXMonstrousMarksWithX: the announced X binds the
// counter count AND the mark's Amount -- the BecomeMonstrous trigger's
// `SVar:MaxTgts:TriggerCount$Amount` reads the mark Amount back as its
// target bound (up to X creatures).
func TestVitalityHunterAnnouncedXMonstrousMarksWithX(t *testing.T) {
	t.Parallel()
	e, cfg, id := monstrosityEngine(t, "Vitality Hunter", "WWWW")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	o := e.G.Obj(id)
	if o.Monstrous || o.Counter("P1P1") != 0 {
		t.Fatalf("precondition: monstrous=%v counters=%d", o.Monstrous, o.Counter("P1P1"))
	}
	idx := monstrosityAbilityIndex(t, e, id)
	if _, ok := findAbilityOption(e, id, idx); !ok {
		t.Fatal("precondition: the monstrosity ability is not offered")
	}
	opt := abilityOption(t, e, id, idx)
	submitChoices(t, e, opt.Index)
	// The announced X (CR 601.2b): choose X = 2 out of the payable range.
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "" || d.Prompt != "Choose a value for X" {
		t.Fatalf("want the X announcement ask, got %+v", d)
	}
	xIdx := -1
	for _, op := range d.Options {
		if op.Kind == "x" && op.Amount == 2 {
			xIdx = op.Index
		}
	}
	if xIdx < 0 {
		t.Fatalf("no X = 2 option in %+v", d.Options)
	}
	submitChoices(t, e, xIdx)
	// The ability resolves; the BecomeMonstrous trigger then asks up to
	// X = 2 targets. Answer it with exactly one creature (Min 0 allows
	// fewer than the max).
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the trigger's target ask, got %+v", d)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("target ask bounds Min %d Max %d, want 0..2 (TriggerCount$Amount = the mark's X)", d.Min, d.Max)
	}
	tIdx := -1
	for _, op := range d.Options {
		if op.Obj == bear {
			tIdx = op.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("the bear is not targetable: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 20)
	o = e.G.Obj(id)
	if got := o.Counter("P1P1"); got != 2 {
		t.Fatalf("Monstrosity$ X with X = 2 put %d counters, want 2", got)
	}
	marks := monstrousMarkEvents(e)
	if len(marks) != 1 || marks[0].Amount != 2 {
		t.Fatalf("want exactly one Monstrous mark with Amount 2, got %+v", marks)
	}
	if got := e.G.Obj(bear).Counter("Lifelink"); got != 1 {
		t.Fatalf("targeted bear carries %d lifelink counters, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestMonstrosityClearsWhenItLeavesBattlefield: CR 701.31 has no
// controller-change end, but the returning permanent is a new permanent --
// the Move fold's leaving-battlefield block clears the mark (the Suspected
// designation's own end shape; ControlChange deliberately does NOT clear).
func TestMonstrosityClearsWhenItLeavesBattlefield(t *testing.T) {
	t.Parallel()
	e, _, id := monstrosityEngine(t, "Giggling Skitterspike", "CCCCC")
	idx := monstrosityAbilityIndex(t, e, id)
	opt := abilityOption(t, e, id, idx)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(id).Monstrous {
		t.Fatal("precondition: creature did not become monstrous")
	}
	// A change of controller does NOT end the designation (CR 701.31 gives
	// monstrous no controller-change end -- unlike Suspected).
	e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 1})
	if !e.G.Obj(id).Monstrous {
		t.Fatal("a control change wrongly cleared the monstrous designation")
	}
	// Leaving the battlefield does: the next battlefield entry is a new
	// permanent.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZHand})
	if e.G.Obj(id).Monstrous {
		t.Fatal("the mark survived the permanent leaving the battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	if e.G.Obj(id).Monstrous {
		t.Fatal("a fresh battlefield entry inherited the monstrous designation")
	}
}

// TestMonstrosityCorpusCarriersExistAndAreNotInRepoDecks guards the safety
// claim the brief made: every Monstrosity$ carrier is real in the corpus and
// none of the suite's carriers is in any repo deck (so the goldens are safe
// by construction).
func TestMonstrosityCorpusCarriersExistAndAreNotInRepoDecks(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	carriers := map[string]bool{}
	for _, name := range []string{
		"Giggling Skitterspike", "Wildfire Cerberus", "Fleecemane Lion",
		"Vitality Hunter", "Domesticated Hydra", "Grim Giganotosaurus",
	} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("corpus card %q not found", name)
		}
		carriers[name] = true
	}
	for _, deckName := range testutil.RepoDeckNames() {
		for _, c := range testutil.RepoDeck(t, reg, deckName) {
			name := ""
			if len(c.Faces) > 0 {
				name = c.Faces[0].Name
			}
			if carriers[name] {
				t.Fatalf("carrier %q is in repo deck %q -- goldens are NOT safe by construction", name, deckName)
			}
		}
	}
}

// TestStormbreathDragonMonstrosityEndToEnd pins the whole feature on the
// real card: activating Monstrosity 3 puts three +1/+1 counters, marks the
// permanent monstrous, refuses a second activation (the CR 701.31b one-shot
// rule), and the BecomeMonstrous trigger deals damage to each opponent equal
// to the number of cards in that player's hand.
// (Ported from the parallel kw-monstrosity branch at the merge resolution.)
func TestStormbreathDragonMonstrosityEndToEnd(t *testing.T) {
	t.Parallel()
	e, cfg, dragon := monstrosityEngine(t, "Stormbreath Dragon", "CCCCCRR")

	// Preconditions the assertions below depend on.
	if e.G.Obj(dragon).Monstrous {
		t.Fatal("test precondition: dragon already monstrous before the activation")
	}
	if e.G.Obj(dragon).Counter("P1P1") != 0 {
		t.Fatal("test precondition: dragon already carries +1/+1 counters")
	}
	opponentHand := len(e.G.Zone(state.ZHand, 1))
	if opponentHand <= 0 {
		t.Fatal("test precondition: opponent hand is empty, damage assertion would be vacuous")
	}
	// A logged draw widens the hand so the "equal to the cards in that
	// player's hand" dependence is measured, not assumed from the opening
	// deal.
	extra := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: extra, From: state.ZLibrary, To: state.ZHand, Secret: true})
	opponentHand++

	opt, ok := findAbilityOption(e, dragon, 0)
	if !ok || opt.Kind != "ability" {
		t.Fatalf("Monstrosity activation not offered on a non-monstrous dragon: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying {5}{R}{R} = %d, want 0", got)
	}
	passUntilStackEmpty(t, e, 50)

	d := e.G.Obj(dragon)
	if got := d.Counter("P1P1"); got != 3 {
		t.Fatalf("+1/+1 counters after Monstrosity 3 = %d, want 3", got)
	}
	if !d.Monstrous {
		t.Fatal("dragon not marked monstrous after the activation")
	}

	// The drain resolved both the ability and the BecomeMonstrous trigger
	// it queued (the trigger is placed when the empty stack re-grants
	// priority); the log carries the push.
	pushed := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == dragon {
			pushed = true
		}
	}
	if !pushed {
		t.Fatal("BecomeMonstrous trigger never pushed")
	}
	if got := e.G.Players[1].Life; got != int32(20-opponentHand) {
		t.Fatalf("opponent life after the trigger = %d, want %d (20 minus %d hand cards)",
			got, 20-opponentHand, opponentHand)
	}

	// The one-shot rule: a monstrous dragon's activation is no longer offered.
	e.pending = nil
	e.priorityRound()
	if _, ok := findAbilityOption(e, dragon, 0); ok {
		t.Fatalf("Monstrosity activation re-offered on a monstrous dragon: %+v", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}

// TestDomesticatedHydraMonstrosityXAndStatic pins the X-cost form and the
// IsMonstrous filter predicate on the real card: Monstrosity X with X = 2
// puts two counters, and the "as long as CARDNAME is monstrous, it has
// trample" static turns on exactly when the designation lands.
func TestDomesticatedHydraMonstrosityXAndStatic(t *testing.T) {
	t.Parallel()
	e, cfg, hydra := monstrosityEngine(t, "Domesticated Hydra", "CCGGG")

	if e.HasKeyword(hydra, "Trample") {
		t.Fatal("test precondition: hydra already has trample before it is monstrous")
	}

	opt, ok := findAbilityOption(e, hydra, 0)
	if !ok || opt.Kind != "ability" {
		t.Fatalf("Monstrosity X activation not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)

	// The X announcement the {X}{G}{G}{G} cost poses at activation.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("want the X announce decision for Monstrosity X, got %+v", d)
	}
	two := -1
	for _, o := range d.Options {
		if o.Label == "X = 2" {
			two = o.Index
		}
	}
	if two < 0 {
		t.Fatalf("no X = 2 option: %+v", d)
	}
	submitChoices(t, e, two)
	passUntilStackEmpty(t, e, 50)

	if got := e.G.Obj(hydra).Counter("P1P1"); got != 2 {
		t.Fatalf("+1/+1 counters after Monstrosity X=2 = %d, want 2", got)
	}
	if !e.G.Obj(hydra).Monstrous {
		t.Fatal("hydra not marked monstrous after the activation")
	}
	if !e.HasKeyword(hydra, "Trample") {
		t.Fatal("IsMonstrous static did not turn on after the designation")
	}
	replayCheck(t, e, cfg)
}

// TestMonstrousDesignationClearsOnBattlefieldExit pins the Move departure
// fold: the designation ends the moment the permanent leaves the battlefield
// (a later entry is a new permanent and never inherits one), which is also
// what un-offers a returned activation and un-lights the IsMonstrous
// statics.
func TestMonstrousDesignationClearsOnBattlefieldExit(t *testing.T) {
	t.Parallel()
	e, cfg, dragon := monstrosityEngine(t, "Stormbreath Dragon", "CCCCCRR")

	opt, ok := findAbilityOption(e, dragon, 0)
	if !ok {
		t.Fatalf("Monstrosity activation not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 50)
	if !e.G.Obj(dragon).Monstrous {
		t.Fatal("test precondition: dragon should be monstrous after the activation")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: dragon, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(dragon); o.Monstrous {
		t.Fatal("monstrous designation survived a battlefield exit")
	}
	replayCheck(t, e, cfg)
}
