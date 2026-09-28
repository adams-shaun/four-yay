package rules

// Task escalate1: kw:Escalate (the modal additional cost "pay this cost for
// each mode chosen beyond the first") was unread -- a Charm cast choosing
// extra modes charged only the printed mana and never tapped/discarded/paid
// the escalate cost. The fix: beginCast captures the raw keyword parameter
// (mode-blind, the plain cast is the only way in), castModeAsk clamps the
// CR 601.2b mode announcement's Max to 1 + the affordable escalations (the
// replicateAsk shape, priced through the same castable checker the payment
// faces), and the cast_modes answer handler folds N-1 escalate payments into
// pc.cost exactly once, so continueCast's re-entry asks for the extra
// non-mana resources and the payment window charges the composed total.
//
// Fixtures load the REAL compiled corpus cards (never a copied Forge script):
// Collective Effort (Escalate--tap an untapped creature), Borrowed Grace
// (Escalate {1}{W}, two targetless PumpAll modes -- the clean both-modes-
// resolve pin) and Collective Brutality (Escalate--Discard a card). None of
// the nine Escalate carriers is in any repo deck, so the heads and the
// ratchet are untouched by construction.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	fixBearSrc = "Name:Fix Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	fixCubSrc  = "Name:Fix Cub\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	fixOgreSrc = "Name:Fix Ogre\nManaCost:3 R\nTypes:Creature Ogre\nPT:4/4\nOracle:x\n"
	fixPebbleS = "Name:Fix Pebble\nTypes:Land\nOracle:x\n"
	fixScrollS = "Name:Fix Scroll\nManaCost:1 U\nTypes:Instant\nOracle:x\n"
	fixTomeS   = "Name:Fix Tome\nManaCost:2 U\nTypes:Instant\nOracle:x\n"
	fixWardSrc = "Name:Fix Ward\nTypes:Enchantment\nOracle:x\n"
	fixTowerS  = "Name:Fix Tower\nTypes:Land\nOracle:x\n"
	fixEffortN = "Collective Effort"
	fixBrutN   = "Collective Brutality"
	fixGraceN  = "Borrowed Grace"
)

// escalateFixture builds a two-seat game whose seat-0 deck holds the real
// corpus spell named by `spell` plus authored filler and Mountains, and puts
// the named authored creatures on each seat's battlefield (untapped --
// asserted by the callers as the tap assertions' precondition). Any card left
// in the decks' libraries is available to moveByName.
func escalateFixture(t *testing.T, seed uint64, spell string, deck0, field0, deck1, field1 []string) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	sp, ok := reg.Lookup(spell)
	if !ok {
		t.Fatalf("corpus fixture: %s missing", spell)
	}
	bySrc := map[string]string{
		"Fix Bear": fixBearSrc, "Fix Cub": fixCubSrc, "Fix Ogre": fixOgreSrc,
		"Fix Pebble": fixPebbleS, "Fix Scroll": fixScrollS, "Fix Tome": fixTomeS,
		"Fix Ward": fixWardSrc, "Fix Tower": fixTowerS,
	}
	deck := []*cards.Card{sp}
	for _, name := range deck0 {
		deck = append(deck, card(t, bySrc[name]))
	}
	deckA := []*cards.Card{}
	for _, name := range deck1 {
		deckA = append(deckA, card(t, bySrc[name]))
	}
	for len(deck) < 40 {
		deck = append(deck, mountainDeck(t, 1)...)
	}
	for len(deckA) < 40 {
		deckA = append(deckA, mountainDeck(t, 1)...)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck, deckA},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	moveByName(t, e, 0, spell, state.ZHand)
	for _, name := range field0 {
		moveByName(t, e, 0, name, state.ZBattlefield)
	}
	for _, name := range field1 {
		moveByName(t, e, 1, name, state.ZBattlefield)
	}
	return e, cfg
}

// untappedOn reports whether seat p's battlefield object named name exists
// untapped; the tap assertions' precondition.
func untappedOn(t *testing.T, e *Engine, p state.PlayerID, name string) bool {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return !o.Tapped
		}
	}
	t.Fatalf("precondition: %q not on seat %d's battlefield", name, p)
	return false
}

// castSpellOption submits the pending priority decision's cast option for the
// named spell.
func castSpellOption(t *testing.T, e *Engine, name string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj != 0 && e.G.Obj(o.Obj) != nil &&
			e.G.Obj(o.Obj).Face() != nil && e.G.Obj(o.Obj).Face().Name == name {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no cast option for %s: %+v", name, d.Options)
}

// submitModes answers the pending cast_modes KModes decision with the given
// option indices and returns the names chosen (choice order).
func submitModes(t *testing.T, e *Engine, idx ...int) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "cast_modes" {
		t.Fatalf("pending = %+v, want the cast_modes KModes ask", d)
	}
	submitChoices(t, e, idx...)
}

// tappedCountOn counts seat p's battlefield objects named name that are
// tapped.
func tappedCountOn(t *testing.T, e *Engine, p state.PlayerID, name string) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name && o.Tapped {
			n++
		}
	}
	return n
}

// zoneHasName reports whether seat p's zone holds a card named name.
func zoneHasName(e *Engine, p state.PlayerID, z state.Zone, name string) bool {
	for _, id := range e.G.Zone(z, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return true
		}
	}
	return false
}

// TestEscalatePrimitiveIsRegistered pins the support declaration the coverage
// walk and deck validation read: without it make report keeps all nine
// Escalate carriers gated on kw:Escalate.
func TestEscalatePrimitiveIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["kw:Escalate"] {
		t.Fatal(`effects.Supported() is missing "kw:Escalate"`)
	}
}

// TestEscalateOneModeChargesNoExtraCost: a one-mode Collective Effort cast is
// exactly the plain cast -- the printed {1}{W}{W} drains the pool, no creature
// is tapped, and the chosen mode resolved (the PutCounterAll sweep put its
// +1/+1 counter on the target player's creature). Byte-identical before and
// after the fix by design (the negative control).
func TestEscalateOneModeChargesNoExtraCost(t *testing.T) {
	t.Parallel()
	e, cfg := escalateFixture(t, 771, fixEffortN, []string{"Fix Bear", "Fix Cub"}, []string{"Fix Bear", "Fix Cub"}, []string{"Fix Ogre"}, []string{"Fix Ogre"})
	if !untappedOn(t, e, 0, "Fix Bear") || !untappedOn(t, e, 0, "Fix Cub") {
		t.Fatal("precondition: fixture creatures already tapped")
	}
	ogre := findByName(e, "Fix Ogre", 1)
	if ogre == 0 || e.G.Obj(ogre).Counter("P1P1") != 0 {
		t.Fatalf("precondition: Fix Ogre id %d counters %d", ogre, e.G.Obj(ogre).Counter("P1P1"))
	}
	addMana(t, e, 0, "1WW") // exactly the printed cost
	castSpellOption(t, e, fixEffortN)
	// Legal modes with this board: DBDestroyCreature (Ogre is power 4) and
	// DBPutCounterAll; DBDestroyEnchantment has no legal target and is
	// filtered. Choose only the counter mode (option index 1 in Choices$
	// order, the second legal entry).
	submitModes(t, e, 1)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the target ask", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat 1 not offered as a target: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the one-mode cast = %d, want 0 (printed {1}{W}{W} exactly)", got)
	}
	if tappedCountOn(t, e, 0, "Fix Bear") != 0 || tappedCountOn(t, e, 0, "Fix Cub") != 0 {
		t.Fatal("a one-mode escalate cast tapped a creature")
	}
	if got := e.G.Obj(ogre).Counter("P1P1"); got != 1 {
		t.Fatalf("chosen mode did not resolve: Ogre carries %d +1/+1 counters, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestEscalateExtraModeTapsAnAdditionalCreature is the fails-before-the-fix
// core: choosing BOTH legal modes of Collective Effort poses the escalate tap
// ask (Escalate--tap an untapped creature you control, once per mode beyond
// the first) and payCast taps exactly one of the two fixture creatures. The
// destroy mode resolves on the Ogre; the counter mode independently targets
// the opponent (who has no remaining creature to receive its counter).
func TestEscalateExtraModeTapsAnAdditionalCreature(t *testing.T) {
	t.Parallel()
	e, cfg := escalateFixture(t, 773, fixEffortN, []string{"Fix Bear", "Fix Cub"}, []string{"Fix Bear", "Fix Cub"}, []string{"Fix Ogre"}, []string{"Fix Ogre"})
	if !untappedOn(t, e, 0, "Fix Bear") || !untappedOn(t, e, 0, "Fix Cub") {
		t.Fatal("precondition: fixture creatures already tapped")
	}
	ogre := findByName(e, "Fix Ogre", 1)
	if ogre == 0 || e.G.Obj(ogre).Face().Power() != 4 {
		t.Fatalf("precondition: Fix Ogre id %d power %d", ogre, e.G.Obj(ogre).Face().Power())
	}
	addMana(t, e, 0, "1WW") // the printed cost; the escalate cost is a tap
	castSpellOption(t, e, fixEffortN)
	submitModes(t, e, 0, 1) // both legal modes
	// The escalate tap ask: one untapped creature you control.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "tapcost" {
		t.Fatalf("pending = %+v, want the escalate tap ask (Escalate--tap an untapped creature)", d)
	}
	bearIdx := -1
	for _, o := range d.Options {
		if o.Obj != 0 && e.G.Obj(o.Obj) != nil && e.G.Obj(o.Obj).Face() != nil &&
			e.G.Obj(o.Obj).Face().Name == "Fix Bear" {
			bearIdx = o.Index
		}
	}
	if bearIdx < 0 {
		t.Fatalf("Fix Bear not offered as the escalate tap: %+v", d.Options)
	}
	submitChoices(t, e, bearIdx)
	// CR 601.2c: each chosen mode has its own target slot.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want two per-mode targets", d)
	}
	ogreIdx, playerIdx := -1, -1
	for _, o := range d.Options {
		if o.Group == "charm-mode-0" && o.Obj == ogre {
			ogreIdx = o.Index
		}
		if o.Group == "charm-mode-1" && o.Kind == "player" && o.Player == 1 {
			playerIdx = o.Index
		}
	}
	if ogreIdx < 0 || playerIdx < 0 || ogreIdx == playerIdx {
		t.Fatalf("fixture lacks independent Ogre/player targets: %+v", d.Options)
	}
	submitChoices(t, e, ogreIdx, playerIdx)
	passUntilStackEmpty(t, e, 20)
	if got := tappedCountOn(t, e, 0, "Fix Bear"); got != 1 {
		t.Fatalf("Fix Bear tapped %d times, want exactly 1 (the escalate charge)", got)
	}
	if tappedCountOn(t, e, 0, "Fix Cub") != 0 {
		t.Fatal("Fix Cub tapped too -- more than one escalate payment")
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the cast = %d, want 0", got)
	}
	if o := e.G.Obj(ogre); o.Zone != state.ZGraveyard {
		t.Fatalf("destroy mode did not resolve: Fix Ogre in %s", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// TestEscalateNotOfferedWithoutEnoughCreatures: the mode announcement's Max
// is clamped to 1 + the affordable escalations. With NO untapped creature the
// second mode is not offerable (Max 1, the printed-cast bound); with one
// creature exactly one escalation is affordable (Max 2); with two creatures
// the CharmNum$ 3 bound stands. Before the fix every board read Max 3.
func TestEscalateNotOfferedWithoutEnoughCreatures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		bears   []string
		wantMax int
	}{
		{"no creature", nil, 1},
		{"one creature", []string{"Fix Bear"}, 2},
		{"two creatures", []string{"Fix Bear", "Fix Cub"}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg := escalateFixture(t, 775, fixEffortN, tc.bears, tc.bears, []string{"Fix Ogre", "Fix Ward"}, []string{"Fix Ogre", "Fix Ward"})
			addMana(t, e, 0, "1WW")
			castSpellOption(t, e, fixEffortN)
			d := e.Pending()
			if d == nil || d.Kind != decision.KModes || d.ResumeKind != "cast_modes" {
				t.Fatalf("pending = %+v, want the cast_modes KModes ask", d)
			}
			if d.Min != 1 || d.Max != tc.wantMax {
				t.Fatalf("mode bounds Min %d Max %d, want Min 1 Max %d", d.Min, d.Max, tc.wantMax)
			}
			// A decision's legal-answer rule has one home: the wire's own Max.
			// An intent claiming a mode beyond the clamp cannot validate.
			if tc.wantMax < 3 {
				if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player,
					Choices: []int{0, 1, 2}}); err == nil {
					t.Fatal("an over-clamp mode answer validated -- the bound is not on the wire")
				}
			}
			// Complete the cast with the counter mode (index 2 in Choices$
			// order; legal on every board) so the engine ends in a
			// replayable state.
			submitModes(t, e, 2)
			d = e.Pending()
			if d == nil || d.Kind != decision.KTarget {
				t.Fatalf("pending = %+v, want the target ask", d)
			}
			for _, o := range d.Options {
				if o.Kind == "player" && o.Player == 1 {
					submitChoices(t, e, o.Index)
					break
				}
			}
			passUntilStackEmpty(t, e, 20)
			replayCheck(t, e, cfg)
		})
	}
}

// TestEscalateManaEscalateChargesPerExtraMode pins the plain-mana escalate
// end to end on Borrowed Grace (Escalate {1}{W}): both targetless PumpAll
// modes resolve, and the composed total {3}{W}{W} drains the pool exactly --
// a cast that charges only the printed {2}{W} leaves 2 mana in the pool and
// fails this test.
func TestEscalateManaEscalateChargesPerExtraMode(t *testing.T) {
	t.Parallel()
	e, cfg := escalateFixture(t, 777, fixGraceN, []string{"Fix Bear", "Fix Cub"}, []string{"Fix Bear", "Fix Cub"}, nil, nil)
	if !untappedOn(t, e, 0, "Fix Bear") || !untappedOn(t, e, 0, "Fix Cub") {
		t.Fatal("precondition: fixture creatures already tapped")
	}
	addMana(t, e, 0, "111WW") // printed {2}{W} + one escalate {1}{W}
	castSpellOption(t, e, fixGraceN)
	submitModes(t, e, 0, 1) // both modes; no target ask (PumpAll is targetless)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the two-mode cast = %d, want 0 ({2}{W} printed + {1}{W} escalate)", got)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o.Face() == nil || !strings.HasPrefix(o.Face().Name, "Fix ") {
			continue
		}
		if got := e.Power(id); got != 4 || e.Toughness(id) != 4 {
			t.Fatalf("%s is %d/%d, want 4/4 (+2/+0 and +0/+2 both resolved)",
				o.Face().Name, e.Power(id), e.Toughness(id))
		}
	}
	replayCheck(t, e, cfg)
}

// TestEscalateDiscardCarrier pins the non-mana Discard escalate on the real
// Collective Brutality: choosing two modes poses the escalate discard ask
// (Escalate--Discard a card) before the target ask, the answer leaves the
// hand, and each mode separately targets the opponent (the RevealYouChoose
// pick discards the instant from their hand; the drain moves 2 life).
func TestEscalateDiscardCarrier(t *testing.T) {
	t.Parallel()
	e, cfg := escalateFixture(t, 779, fixBrutN, []string{"Fix Bear", "Fix Pebble"}, []string{"Fix Bear"}, []string{"Fix Ogre", "Fix Scroll", "Fix Tome"}, []string{"Fix Ogre"})
	// Guarantee the discardable card in the caster's hand and the eligible
	// instant/sorcery cards in the opponent's hand (the deck deal is
	// seed-dependent).
	moveByName(t, e, 0, "Fix Pebble", state.ZHand)
	moveByName(t, e, 1, "Fix Scroll", state.ZHand)
	moveByName(t, e, 1, "Fix Tome", state.ZHand)
	if !zoneHasName(e, 1, state.ZHand, "Fix Scroll") || !zoneHasName(e, 1, state.ZHand, "Fix Tome") {
		t.Fatal("precondition: opponent hand lacks the instant cards")
	}
	if !zoneHasName(e, 0, state.ZHand, "Fix Pebble") {
		t.Fatal("precondition: caster hand lacks a discardable card")
	}
	hand0 := len(e.G.Zone(state.ZHand, 0))
	life1 := e.G.Players[1].Life
	addMana(t, e, 0, "1B") // the printed cost; the escalate cost is a discard
	castSpellOption(t, e, fixBrutN)
	// Two modes: DBDiscard and DBDrain (both target the opponent).
	submitModes(t, e, 0, 2)
	// The escalate discard ask comes before the target ask (continueCast's
	// part-ask pass runs ahead of it).
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "discard" {
		t.Fatalf("pending = %+v, want the escalate discard ask", d)
	}
	pebbleIdx := -1
	for _, o := range d.Options {
		if o.Obj != 0 && e.G.Obj(o.Obj) != nil && e.G.Obj(o.Obj).Face() != nil &&
			e.G.Obj(o.Obj).Face().Name == "Fix Pebble" {
			pebbleIdx = o.Index
		}
	}
	if pebbleIdx < 0 {
		t.Fatalf("Fix Pebble not offered as the escalate discard: %+v", d.Options)
	}
	submitChoices(t, e, pebbleIdx)
	// The two modes both choose seat 1, but through separate target slots.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want two per-mode target slots", d)
	}
	modeTargets := [2]int{-1, -1}
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			switch o.Group {
			case "charm-mode-0":
				modeTargets[0] = o.Index
			case "charm-mode-1":
				modeTargets[1] = o.Index
			}
		}
	}
	if modeTargets[0] < 0 || modeTargets[1] < 0 || modeTargets[0] == modeTargets[1] {
		t.Fatalf("seat 1 not offered for both modes: %+v", d.Options)
	}
	submitChoices(t, e, modeTargets[0], modeTargets[1])
	// Both seats pass priority so the spell resolves; DBDiscard's
	// RevealYouChoose then asks the caster to pick an instant/sorcery from
	// the opponent's hand (the ask surfaces mid-resolution, after the passes).
	for i := 0; i < 10; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	if d == nil || d.Kind != decision.KModes {
		t.Fatalf("pending = %+v, want the RevealYouChoose discard ask", d)
	}
	scrollIdx := -1
	for _, o := range d.Options {
		if strings.Contains(o.Label, "Fix Scroll") {
			scrollIdx = o.Index
		}
	}
	if scrollIdx < 0 {
		t.Fatalf("Fix Scroll not offered by the reveal-choose ask: %+v", d.Options)
	}
	submitChoices(t, e, scrollIdx)
	passUntilStackEmpty(t, e, 30)
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0-2 {
		t.Fatalf("caster hand holds %d cards, want %d (the spell left + one escalate discard)", got, hand0-2)
	}
	if zoneHasName(e, 0, state.ZHand, "Fix Pebble") {
		t.Fatal("the escalate discard was never charged")
	}
	if zoneHasName(e, 1, state.ZHand, "Fix Scroll") {
		t.Fatal("the chosen mode's RevealYouChoose discard never landed")
	}
	if got := e.G.Players[1].Life; got != life1-2 {
		t.Fatalf("opponent at %d life, want %d (the drain mode resolved)", got, life1-2)
	}
	if got := e.G.Players[0].Life; got <= e.G.Players[1].Life-2 {
		t.Fatalf("caster did not gain the 2 life: %d", got)
	}
	replayCheck(t, e, cfg)
}
