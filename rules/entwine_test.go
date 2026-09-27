package rules

// Task agent-20260925T000311Z-22c4c559: kw:Entwine (CR 702.42) was unread --
// a modal spell with `K:Entwine:<cost>` had no cast option to pay the optional
// additional cost, the CR 601.2b mode announcement always capped at its
// CharmNum$ default, and the coverage walk reported `kw:Entwine` unsupported,
// leaving Spectral Shift (the api:ChangeText carrier) unplayable.
//
// The fix: legal.go offers an "entwined" cast option priced base + entwine
// cost through the shared offerCastable gate; beginCast's "entwined" case
// folds the cost into pc.cost (the Buyback shape, paid once on top of the
// printed cost); castModeAsk forces BOTH Min and Max to the filtered legal
// count when the pending cast is entwined, so Decision.Validate, the bot arm
// and Clamp all read one bound (the one-home rule). No keywords.go expansion
// is needed -- the K: line is read directly, the kw:Escalate convention.
//
// Fixtures load the REAL compiled corpus cards (never a copied Forge script).
// Spectral Shift (mana entwine {2}, two ChangeText modes) proves the all-modes
// announcement and the composed charge; Solar Tide (Sac<2/Land> entwine, two
// targetless DestroyAll modes) proves a NON-MANA additional-cost form.

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
	fixEntwineBearSrc  = "Name:Fix Entwine Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	fixEntwineElkSrc   = "Name:Fix Mountain Elk\nManaCost:1 G\nTypes:Creature Elk\nPT:2/2\nOracle:Other Mountain creatures you control get +1/+1.\n"
	fixEntwineRedSrc   = "Name:Fix Red Mage\nManaCost:1 R\nTypes:Creature Wizard\nPT:1/1\nOracle:Other red creatures you control get +1/+0.\n"
	fixEntwineForestS  = "Name:Fix Entwine Forest\nTypes:Land\nOracle:x\n"
	fixEntwineForestS2 = "Name:Fix Entwine Forest B\nTypes:Land\nOracle:x\n"
	fixEntwineForestS3 = "Name:Fix Entwine Forest C\nTypes:Land\nOracle:x\n"
	fixEntwineForestS4 = "Name:Fix Entwine Forest D\nTypes:Land\nOracle:x\n"
	fixEntwineWeenieS  = "Name:Fix Weenie\nManaCost:1 G\nTypes:Creature Elf\nPT:1/1\nOracle:x\n"
	fixEntwineGiantS   = "Name:Fix Giant\nManaCost:3 R\nTypes:Creature Giant\nPT:3/3\nOracle:x\n"
	fixEntwineShiftN   = "Spectral Shift"
	fixEntwineTideN    = "Solar Tide"
)

// entwineFixture builds a two-seat game whose seat-0 deck holds the real
// corpus spell named by `spell` plus authored filler and Mountains, then puts
// the named authored permanents on each seat's battlefield and the rest in
// hand. Any named fixture card left in a library is available to moveByName.
func entwineFixture(t *testing.T, seed uint64, spell string, field0, field1, hand0 []string) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	sp, ok := reg.Lookup(spell)
	if !ok {
		t.Fatalf("corpus fixture: %s missing", spell)
	}
	bySrc := map[string]string{
		"Fix Entwine Bear": fixEntwineBearSrc, "Fix Mountain Elk": fixEntwineElkSrc,
		"Fix Red Mage": fixEntwineRedSrc, "Fix Entwine Forest": fixEntwineForestS,
		"Fix Entwine Forest B": fixEntwineForestS2, "Fix Entwine Forest C": fixEntwineForestS3,
		"Fix Entwine Forest D": fixEntwineForestS4, "Fix Weenie": fixEntwineWeenieS,
		"Fix Giant": fixEntwineGiantS,
	}
	deck := []*cards.Card{sp}
	deckA := []*cards.Card{}
	for _, name := range append(append([]string{}, field0...), hand0...) {
		if _, ok := bySrc[name]; ok {
			deck = append(deck, card(t, bySrc[name]))
		}
	}
	for _, name := range field1 {
		if _, ok := bySrc[name]; ok {
			deckA = append(deckA, card(t, bySrc[name]))
		}
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
	for _, name := range hand0 {
		moveByName(t, e, 0, name, state.ZHand)
	}
	return e, cfg
}

// castEntwinedOption submits the "(entwined)" cast option for name, failing if
// the offer is absent (the precondition that the offer itself was made).
func castEntwinedOption(t *testing.T, e *Engine, name string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Mode == "entwined" && o.Obj != 0 &&
			e.G.Obj(o.Obj) != nil && e.G.Obj(o.Obj).Face() != nil &&
			e.G.Obj(o.Obj).Face().Name == name {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no (entwined) cast option for %s: %+v", name, d.Options)
}

// submitChangeText picks the from-word and to-word options by their labelled
// kind (changetext_from / changetext_to) and submits them, failing if either
// is absent -- so the substitution under test is the one actually applied.
func submitChangeText(t *testing.T, e *Engine, from, to string) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose the text word(s)" {
		t.Fatalf("pending = %+v, want the ChangeText word ask", d)
	}
	fromIdx, toIdx := -1, -1
	for _, o := range d.Options {
		if o.Kind == "changetext_from" && strings.EqualFold(o.Label, from) {
			fromIdx = o.Index
		}
		if o.Kind == "changetext_to" && strings.EqualFold(o.Label, to) {
			toIdx = o.Index
		}
	}
	if fromIdx < 0 || toIdx < 0 {
		t.Fatalf("ChangeText ask lacks %q->%q: %+v", from, to, d.Options)
	}
	submitChoices(t, e, fromIdx, toIdx)
}

// TestEntwinePrimitiveIsRegistered pins the support declaration the coverage
// walk and deck validation read: without it make report keeps all 32 Entwine
// carriers gated on kw:Entwine.
func TestEntwinePrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["kw:Entwine"] {
		t.Fatal(`effects.Supported() is missing "kw:Entwine"`)
	}
}

// TestEntwineCorpusCostFormsAreAllPriced pins the measured disposition of the
// 32 corpus Entwine carriers: 30 carry a plain mana cost and 2 carry a
// sacrifice (Betrayal of Flesh Sac<3/Land>, Solar Tide Sac<2/Land>). Every
// form must price through entwineCost, so no carrier is declared supported on
// a cost the engine cannot charge, and every carrier must now read as fully
// playable (its kw:Entwine gap closed and no other primitive missing). A
// carrier whose keyword disappears, whose cost stops parsing, or which still
// reports an unsupported primitive makes this fail.
func TestEntwineCorpusCostFormsAreAllPriced(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	mana, sac := 0, 0
	for _, c := range reg.Cards {
		if c == nil {
			continue
		}
		for _, f := range c.Faces {
			if f.Name == "" {
				continue
			}
			if _, has := f.KeywordParam("Entwine"); !has {
				continue
			}
			cost, ok := entwineCost(f)
			if !ok {
				t.Fatalf("%s: K:Entwine form does not price (still a coverage gap)", f.Name)
			}
			if len(cost.Sac) > 0 {
				sac++
			} else {
				mana++
			}
			if miss := reg.Unsupported(c, supported); len(miss) > 0 {
				t.Fatalf("%s still unsupported after the Entwine fix: %v", f.Name, miss)
			}
		}
	}
	if mana != 30 || sac != 2 {
		t.Fatalf("Entwine carriers priced mana=%d sac=%d, want mana=30 sac=2 (re-measure at FORGE_REF)", mana, sac)
	}
}

// TestEntwinePaidChoosesEveryModeAndCharges pins the fails-before-the-fix core
// on the real Spectral Shift: paying the entwine {2} forces BOTH ChangeText
// modes onto the CR 601.2b announcement (Min and Max both 2, the filtered
// legal count), the composed {3}{U} drains the pool exactly, and BOTH
// substitutions land on the two battlefield permanents.
func TestEntwinePaidChoosesEveryModeAndCharges(t *testing.T) {
	e, cfg := entwineFixture(t, 901, fixEntwineShiftN,
		[]string{"Fix Mountain Elk", "Fix Red Mage"}, []string{"Fix Entwine Bear"}, nil)
	elk := findByName(e, "Fix Mountain Elk", 0)
	red := findByName(e, "Fix Red Mage", 0)
	if elk == 0 || red == 0 {
		t.Fatalf("precondition: fixture targets on the battlefield (elk=%d red=%d)", elk, red)
	}
	// Preconditions the substitutions depend on: each target's text actually
	// names the source word and not the replacement.
	elkText := e.Text(elk)
	if !strings.Contains(elkText, "Mountain") || strings.Contains(elkText, "Island") {
		t.Fatalf("precondition: elk text %q must contain Mountain and not Island", elkText)
	}
	redText := e.Text(red)
	if !strings.Contains(redText, "red") || strings.Contains(redText, "blue") {
		t.Fatalf("precondition: red-mage text %q must contain red and not blue", redText)
	}
	addMana(t, e, 0, "111U") // printed {1}{U} + entwine {2}
	castEntwinedOption(t, e, fixEntwineShiftN)
	// The forced all-modes announcement: BOTH modes, no choice left.
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "cast_modes" {
		t.Fatalf("pending = %+v, want the cast_modes KModes ask", d)
	}
	if d.Min != 2 || d.Max != 2 || len(d.Options) != 2 {
		t.Fatalf("entwined mode ask Min=%d Max=%d options=%d, want 2/2/2", d.Min, d.Max, len(d.Options))
	}
	// The legal-answer rule has one home: the wire bound must reject an
	// answer that does not name every mode.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err == nil {
		t.Fatal("a one-mode answer validated on a forced-all-modes entwine ask")
	}
	// The engine records the forced answers without asking: submit both.
	submitChoices(t, e, 0, 1)
	// CR 601.2c: each chosen mode has its own target slot, both a Card here.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("pending = %+v, want two per-mode target slots", d)
	}
	slot := [2]int{-1, -1}
	for _, o := range d.Options {
		switch o.Group {
		case "charm-mode-0":
			if o.Obj == elk {
				slot[0] = o.Index
			}
		case "charm-mode-1":
			if o.Obj == red {
				slot[1] = o.Index
			}
		}
	}
	if slot[0] < 0 || slot[1] < 0 {
		t.Fatalf("fixture lacks an independent target per mode: %+v", d.Options)
	}
	submitChoices(t, e, slot[0], slot[1])
	// Resolve, answering each mode's ChangeText word ask in mode order:
	// DBBasicLand (Mountain -> Island) then DBColor (red -> blue).
	tasks := 0
	for i := 0; i < 60; i++ {
		d = e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KChoose:
			tasks++
			if tasks == 1 {
				submitChangeText(t, e, "Mountain", "Island")
			} else {
				submitChangeText(t, e, "red", "blue")
			}
		case decision.KPriority:
			if tasks >= 2 && len(e.G.Stack) == 0 {
				i = 60 // both substitutions landed; stop before the next turn
				break
			}
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected pending decision %v (%s)", d.Kind, d.Prompt)
		}
	}
	if tasks != 2 {
		t.Fatalf("ChangeText asks answered = %d, want 2 (one per mode)", tasks)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the entwined cast = %d, want 0 (printed {1}{U} + entwine {2})", got)
	}
	if got := e.Text(elk); !strings.Contains(got, "Island") || strings.Contains(got, "Mountain") {
		t.Fatalf("basic-land mode did not resolve: elk text %q", got)
	}
	if got := e.Text(red); !strings.Contains(got, "blue") || strings.Contains(got, "red") {
		t.Fatalf("color mode did not resolve: red-mage text %q", got)
	}
	replayCheck(t, e, cfg)
}

// TestEntwineDeclinedIsTheOrdinaryOneModeCast is the negative control: the
// plain cast of Spectral Shift is unchanged by the fix -- the mode ask is the
// CharmNum$ 1/1 bound, only the printed {1}{U} is charged, and only the chosen
// mode resolves.
func TestEntwineDeclinedIsTheOrdinaryOneModeCast(t *testing.T) {
	e, cfg := entwineFixture(t, 903, fixEntwineShiftN,
		[]string{"Fix Mountain Elk", "Fix Red Mage"}, []string{"Fix Entwine Bear"}, nil)
	elk := findByName(e, "Fix Mountain Elk", 0)
	if elk == 0 {
		t.Fatal("precondition: elk not on the battlefield")
	}
	addMana(t, e, 0, "1U") // exactly the printed cost
	castSpellOption(t, e, fixEntwineShiftN)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "cast_modes" {
		t.Fatalf("pending = %+v, want the cast_modes KModes ask", d)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("plain mode ask Min=%d Max=%d, want 1/1 (not every mode)", d.Min, d.Max)
	}
	submitModes(t, e, 0) // only the basic-land mode
	sawAsk := false
	for i := 0; i < 60; i++ {
		d = e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KTarget:
			if idx := indexOfObjOption(d, elk); idx >= 0 {
				submitChoices(t, e, idx)
			} else {
				t.Fatalf("target ask does not offer the elk: %+v", d.Options)
			}
		case decision.KChoose:
			sawAsk = true
			submitChangeText(t, e, "Mountain", "Island")
		case decision.KPriority:
			if sawAsk && len(e.G.Stack) == 0 {
				i = 60
				break
			}
			passOnceP(t, e)
		default:
			t.Fatalf("unexpected pending decision %v (%s)", d.Kind, d.Prompt)
		}
	}
	if !sawAsk {
		t.Fatal("the chosen ChangeText mode never posed its word ask")
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the plain cast = %d, want 0 (printed {1}{U} only)", got)
	}
	if got := e.Text(elk); !strings.Contains(got, "Island") {
		t.Fatalf("the chosen mode did not resolve: elk text %q", got)
	}
	replayCheck(t, e, cfg)
}

// TestEntwineNotOfferedWhenCostUnpayable: with no mana the entwined offer is
// absent from the priority options entirely (the shared offerCastable gate),
// so an unpayable entwine can never be proposed and then abort.
func TestEntwineNotOfferedWhenCostUnpayable(t *testing.T) {
	e, _ := entwineFixture(t, 905, fixEntwineShiftN,
		[]string{"Fix Mountain Elk"}, nil, nil)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Mode == "entwined" {
			t.Fatalf("entwined offered with an empty pool: %+v", o)
		}
	}
}

// TestEntwineNonManaCostFormSacrificesLands pins a non-mana additional-cost
// form on the real Solar Tide (K:Entwine:Sac<2/Land>): the entwined offer
// composes the printed {4}{W}{W} with the two-land sacrifice, paying it taps
// the Sac<2/Land> part (two lands leave the battlefield) and forces BOTH
// targetless DestroyAll modes, so the 1/1 and the 3/3 both die.
func TestEntwineNonManaCostFormSacrificesLands(t *testing.T) {
	e, cfg := entwineFixture(t, 907, fixEntwineTideN,
		[]string{"Fix Entwine Forest", "Fix Entwine Forest B", "Fix Entwine Forest C", "Fix Entwine Forest D"},
		[]string{"Fix Weenie", "Fix Giant"}, nil)
	weenie := findByName(e, "Fix Weenie", 1)
	giant := findByName(e, "Fix Giant", 1)
	if weenie == 0 || giant == 0 {
		t.Fatalf("precondition: opponent creatures on the battlefield (weenie=%d giant=%d)", weenie, giant)
	}
	if e.G.Obj(weenie).Face().Power() != 1 || e.G.Obj(giant).Face().Power() != 3 {
		t.Fatalf("precondition: weenie power %d, giant power %d",
			e.G.Obj(weenie).Face().Power(), e.G.Obj(giant).Face().Power())
	}
	landsBefore := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && strings.HasPrefix(o.Face().Name, "Fix Entwine Forest") {
			landsBefore++
		}
	}
	if landsBefore != 4 {
		t.Fatalf("precondition: caster controls %d fixture lands, want 4", landsBefore)
	}
	addMana(t, e, 0, "1111WW") // printed {4}{W}{W}; the entwine cost is the sacrifice
	castEntwinedOption(t, e, fixEntwineTideN)
	// The Sac<2/Land> part surfaces FIRST (continueCast's non-mana part pass
	// runs ahead of the mode announcement) as ONE sacrifice choose ask
	// (Min == Max == 2, the last-Sac-part shape) whose options name the
	// candidate lands.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 ||
		len(d.Options) < 2 || d.Options[0].Kind != "sacrifice" {
		t.Fatalf("pending = %+v, want the Sac<2/Land> sacrifice ask (Min=Max=2)", d)
	}
	submitChoices(t, e, 0, 1)
	// The forced all-modes announcement (both DestroyAll modes, targetless).
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "cast_modes" {
		t.Fatalf("pending = %+v, want the cast_modes KModes ask", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("entwined mode ask Min=%d Max=%d, want 2/2", d.Min, d.Max)
	}
	submitChoices(t, e, 0, 1)
	passUntilStackEmpty(t, e, 30)
	landsAfter := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && strings.HasPrefix(o.Face().Name, "Fix Entwine Forest") {
			landsAfter++
		}
	}
	if landsAfter != 2 {
		t.Fatalf("caster lands after the entwined cast = %d, want 2 (the Sac<2/Land> charge)", landsAfter)
	}
	if e.G.Obj(weenie).Zone != state.ZGraveyard || e.G.Obj(giant).Zone != state.ZGraveyard {
		t.Fatalf("both DestroyAll modes must resolve: weenie in %s, giant in %s",
			e.G.Obj(weenie).Zone, e.G.Obj(giant).Zone)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the cast = %d, want 0 (printed {4}{W}{W})", got)
	}
	replayCheck(t, e, cfg)
}
