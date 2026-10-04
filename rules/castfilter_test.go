package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// The FILTERED Count$CastTotalManaSpent <Type> form (task castfilter1),
// pinned end to end on real corpus cards.
//
// Snow is the tractable family: the pool has always carried a PARALLEL snow
// tally (state.Player.Snow, CR 107.4h), so the payment's snow-unit delta is
// real per-unit provenance, captured at payCast and folded into
// Object.ManaSnowSpent. The six Snow carriers (Berg Strider, Blood on the
// Snow, Blessing of Frost, Tundra Fumarole, Graven Lore, Search for Glory)
// all read `Count$CastTotalManaSpent Snow`; before this fix the head returned
// the UNFILTERED total (e.g. 5), and after it returns the snow-sourced count
// (2). Task castfilter2 below gives Treasure/Cave/Desert the same per-unit
// provenance through Player.TypedMana (Marut, Bat Colony, Cataclysmic
// Prospecting).

// TestCastTotalManaSpentSnowCountsOnlySnowEndToEnd casts Tundra Fumarole
// (1 R R, "Add {C} for each {S} spent to cast this spell") from a pool of
// one snow red and two plain red, and asserts the real derived effect: the
// resolution adds exactly the number of COLOURLESS mana equal to the SNOW
// spend (1), not the total spend (3).
func TestCastTotalManaSpentSnowCountsOnlySnowEndToEnd(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Tundra Fumarole"))
	// A target for the spell's 4 damage (the resolution's mana leg runs after).
	target := e.G.AddObject(card(t, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
	target.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{target.ID})
	spell := e.G.Zone(state.ZHand, 0)[0]

	// One SNOW red unit plus two PLAIN red units: the cost {1}{R}{R} is paid
	// entirely from red, but only ONE unit is snow. A correct filtered read is
	// 1; the old unfiltered read was 3.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "SR", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 2})

	castMode(t, e, spell, "")
	// The spell targets a creature/planeswalker (CR 601.2c): answer the ask; it
	// then pays and the pay-time capture lands on the stack object.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		pick := -1
		for i, o := range d.Options {
			if o.Obj == target.ID {
				pick = i
			}
		}
		if pick < 0 {
			t.Fatalf("target decision did not offer the bear: %+v", d)
		}
		submitChoices(t, e, pick)
	}
	// The pay-time capture is live on the stack object before resolution.
	if got := e.G.Obj(spell).ManaSpent; got != 3 {
		t.Fatalf("ManaSpent = %d, want 3 (three red units paid {1}{R}{R})", got)
	}
	if got := e.G.Obj(spell).ManaSnowSpent; got != 1 {
		t.Fatalf("ManaSnowSpent = %d, want 1 (one snow unit of three spent)", got)
	}
	// Resolution adds {C} for each snow unit, not each mana spent.
	before := e.G.Players[0].Pool[state.MC]
	e.resolveTop()
	after := e.G.Players[0].Pool[state.MC]
	if got := after - before; got != 1 {
		t.Fatalf("Tundra Fumarole added %d colourless mana, want 1 (one snow unit of three spent)", got)
	}
	if got := target.Damage; got != 4 {
		t.Fatalf("bear took %d damage, want 4", got)
	}
}

// TestCataclysmicProspectingCastTotalManaSpentFailsClosed pins the ticket's
// named carrier against PLAIN mana: Cataclysmic Prospecting creates a tapped
// Treasure for each mana from a DESERT spent to cast it
// (SVar:Y:Count$CastTotalManaSpent Desert), and an X=1 cast paid from three
// PLAIN red units (no Desert units) reads 0 and creates NO Treasures. Before
// the fix the head returned the UNFILTERED total (3 Treasures for the
// three-mana cast); the direction -- only the named producer type counts --
// is the contract both this pin and the castfilter2 Desert pin below assert.
func TestCataclysmicProspectingCastTotalManaSpentFailsClosed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, spell := excessFixtureWithTokens(t, 7, corpusCardText(t, "c/cataclysmic_prospecting.txt"), reg.Tokens)
	// Cast for X=1: {X}{R}{R} = 3 mana from three red units (none a Desert --
	// the engine cannot yet tell a Desert from any other land).
	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castOptionFor(t, e, spell).Index)
	// The {X} ask: announce X = 1.
	if d := e.Pending(); d != nil && d.Kind == "choose" {
		submitChoices(t, e, 1)
	}
	// Drain to the resolution.
	for i := 0; i < 40 && len(e.G.Stack) > 0 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack %d)", len(e.G.Stack))
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("non-pass decision while draining: %+v", d)
		}
		submitChoices(t, e, idx)
	}
	if got := tokensNamed(e, 0, "Treasure"); got != 0 {
		t.Fatalf("Cataclysmic Prospecting created %d Treasures, want 0 (no Desert provenance: fail closed, not the unfiltered total)", got)
	}
}

// --- Task castfilter2: producer-type mana provenance ------------------------
//
// The filtered Count$CastTotalManaSpent Treasure/Cave/Desert forms now
// resolve from Player.TypedMana, the per-unit producer tally the pool
// carries beside Snow. Marut, Bat Colony and Cataclysmic Prospecting are
// the brief's named carriers; the pins below assert the REAL derived effect
// (tokens created), never the captured field alone.

// drainEtb drains the resolution's queued triggers and the stack: the
// ETB-trigger shape the Marut / Bat Colony / Cataclysmic Prospecting pins
// resolve under. pendingTriggers must be pushed explicitly -- a bare
// passUntilStackEmpty exits while the stack is empty but the trigger is
// still queued, so the ETB body would never run.
func drainEtb(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		if len(e.G.Stack) == 0 {
			return
		}
		d := e.Pending()
		if d == nil {
			e.resolveTop()
			continue
		}
		if d.Kind == decision.KPriority {
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("submit pass: %v", err)
			}
			continue
		}
		submitChoices(t, e, 0)
	}
	t.Fatalf("ETB drain did not settle: %d triggers pending, %d on the stack", len(e.pendingTriggers), len(e.G.Stack))
}

// TestCastTotalManaSpentTreasureMarutCreatesTreasureTokens: Marut ({8}) cast from a pool
// of two Treasure-typed colourless units plus six plain ones creates exactly
// 2 Treasure tokens -- one per mana from a Treasure spent to cast it, not
// the total (8) and not the old fail-closed 0.
func TestCastTotalManaSpentTreasureMarutCreatesTreasureTokens(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Marut"))
	marut := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "TreasureC", Amount: 2})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 6})
	castMode(t, e, marut, "")
	finishCast(t, e, marut)
	drainEtb(t, e)
	if got := tokensNamed(e, 0, "Treasure"); got != 2 {
		t.Fatalf("Marut created %d Treasure tokens, want 2 (two Treasure units of eight spent)", got)
	}
}

// TestCastTotalManaSpentPlainOnlyCreatesNoTokens: the same cast from eight PLAIN
// colourless units spends no Treasure mana, so the ETB trigger's
// CheckSVar$ X gate reads 0 (nonzero truthiness) and denies -- no token at
// all, not even the old fail-closed shape's silence-with-a-capture.
func TestCastTotalManaSpentPlainOnlyCreatesNoTokens(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Marut"))
	marut := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 8})
	castMode(t, e, marut, "")
	finishCast(t, e, marut)
	drainEtb(t, e)
	if got := tokensNamed(e, 0, "Treasure"); got != 0 {
		t.Fatalf("plain-only Marut cast created %d Treasure tokens, want 0 (CheckSVar$ gate on the Treasure count)", got)
	}
}

// TestCastTotalManaSpentTreasureExactnessOneOfEight: exactness, not the total --
// one Treasure unit among eight creates exactly ONE token.
func TestCastTotalManaSpentTreasureExactnessOneOfEight(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Marut"))
	marut := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "TreasureC", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 7})
	castMode(t, e, marut, "")
	finishCast(t, e, marut)
	drainEtb(t, e)
	if got := tokensNamed(e, 0, "Treasure"); got != 1 {
		t.Fatalf("Marut created %d Treasure tokens, want 1 (one Treasure unit of eight spent)", got)
	}
}

// TestCastTotalManaSpentMixedPoolCaptureSplitsEveryTag: one snow red, one Treasure
// colourless and six plain colourless pay the {8}; the pay-time capture
// splits the spend across ALL FOUR totals (8 / 1 / 1 / 0 / 0). The typed
// consumption order (plain -> typed -> snow) is what puts the snow unit last,
// so every split is exact and the CastInfo flag routing (each later event
// carries all earlier flags, so Apply checks the NEWEST flag first: Desert,
// Cave, Treasure, Snow, then the total) can never route one tag's Amount into
// another's field.
func TestCastTotalManaSpentMixedPoolCaptureSplitsEveryTag(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Marut"))
	marut := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "SR", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "TreasureC", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 6})
	castMode(t, e, marut, "")
	finishCast(t, e, marut)
	drainEtb(t, e)
	o := e.G.Obj(marut)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Marut not on the battlefield: %+v", o)
	}
	if got := o.ManaSpent; got != 8 {
		t.Fatalf("ManaSpent = %d, want 8", got)
	}
	if got := o.ManaSnowSpent; got != 1 {
		t.Fatalf("ManaSnowSpent = %d, want 1 (the snow unit is consumed last)", got)
	}
	if got := o.ManaTreasureSpent; got != 1 {
		t.Fatalf("ManaTreasureSpent = %d, want 1", got)
	}
	if got := o.ManaCaveSpent; got != 0 {
		t.Fatalf("ManaCaveSpent = %d, want 0", got)
	}
	if got := o.ManaDesertSpent; got != 0 {
		t.Fatalf("ManaDesertSpent = %d, want 0", got)
	}
	if got := tokensNamed(e, 0, "Treasure"); got != 1 {
		t.Fatalf("Marut created %d Treasure tokens, want 1 (the ETB read the Treasure split)", got)
	}
}

// TestCastTotalManaSpentDesertCataclysmicProspectingCreatesTappedTreasures: the sorcery's
// DBTreasure creates a TAPPED Treasure for each mana from a Desert spent
// (SVar:Y:Count$CastTotalManaSpent Desert). X=2 announced over a pool of two
// Desert-typed colourless units and two plain red: the {R}{R} pips take the
// plain red units, X's {2} generic takes the typed units, so exactly 2
// tapped Treasures -- the exact per-tag count, not the total (4).
func TestCastTotalManaSpentDesertCataclysmicProspectingCreatesTappedTreasures(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Cataclysmic Prospecting"))
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "DesertC", Amount: 2})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 2})
	castMode(t, e, spell, "")
	// The {X} ask: announce X = 2.
	if d := e.Pending(); d != nil && d.Kind == "choose" {
		submitChoices(t, e, 2)
	}
	finishCast(t, e, spell)
	drainEtb(t, e)
	if got := tokensNamed(e, 0, "Treasure"); got != 2 {
		t.Fatalf("Cataclysmic Prospecting created %d Treasures, want 2 (two Desert units of four spent)", got)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && strings.Contains(o.Face().Name, "Treasure") && !o.Tapped {
			t.Fatalf("a created Treasure token entered untapped; TokenTapped$ True must tap it")
		}
	}
}

// TestCastTotalManaSpentCaveBatColonyCreatesBatTokens: Bat Colony ({2}{W}) cast from a
// pool of two Cave-typed white units plus one plain white creates exactly 2
// 1/1 black Bat tokens -- one per mana from a Cave spent to cast it.
func TestCastTotalManaSpentCaveBatColonyCreatesBatTokens(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Bat Colony"))
	bat := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "CaveW", Amount: 2})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 1})
	castMode(t, e, bat, "")
	finishCast(t, e, bat)
	drainEtb(t, e)
	if got := tokensNamed(e, 0, "Bat"); got != 2 {
		t.Fatalf("Bat Colony created %d Bat tokens, want 2 (two Cave units of three spent)", got)
	}
}

// TestCastTotalManaSpentConsumesPlainBeforeTyped is the deterministic
// consumption-order pin: a {1}{C} cast whose ETB draws
// Count$CastTotalManaSpent Treasure cards. A pool of two PLAIN {C} and one
// Treasure {C} spends both {C} units on the pip and the generic -- whichever
// order the pool was seeded in -- so the Treasure count reads 0 (the
// deterministic contract); two Treasure units alone must read 2.
func TestCastTotalManaSpentConsumesPlainBeforeTyped(t *testing.T) {
	t.Parallel()
	fixture := "Name:Treasure Counter\nManaCost:1 C\nTypes:Creature\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$x\n" +
		"SVar:TrigDraw:DB$ Draw | NumCards$ X\n" +
		"SVar:X:Count$CastTotalManaSpent Treasure\nOracle:x\n"
	for _, tc := range []struct {
		name      string
		counters  []string // the Counter forms emitted, in order
		wantDraws int
		wantTyped int32
	}{
		{"plain first", []string{"C", "TreasureC", "C"}, 0, 0},
		{"treasure first", []string{"TreasureC", "C", "C"}, 0, 0},
		{"two treasures", []string{"TreasureC", "TreasureC"}, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := handEngineTokens(t, card(t, fixture))
			spell := e.G.Zone(state.ZHand, 0)[0]
			for _, counter := range tc.counters {
				e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: counter, Amount: 1})
			}
			castMode(t, e, spell, "")
			finishCast(t, e, spell)
			drainEtb(t, e)
			if got := e.G.Obj(spell).ManaTreasureSpent; got != tc.wantTyped {
				t.Fatalf("ManaTreasureSpent = %d, want %d", got, tc.wantTyped)
			}
			if got := len(e.G.Zone(state.ZHand, 0)); got != tc.wantDraws {
				t.Fatalf("drew %d cards, want %d (Treasure count %d)", got, tc.wantDraws, tc.wantTyped)
			}
		})
	}
}

// --- Restricted tagged mana: the restriction must survive the tag ----------
//
// A Treasure/Cave/Desert-typed producer can also carry RestrictValid$ (task
// castfilter2's review fix). Registering the restriction batch only for the
// PLAIN counter form silently dropped it for a tagged unit -- the ManaAdd
// case's typed branch broke out before the registration block -- so Echoing
// Cavern, Sunken Citadel (both Cave) and Bucolic Ranch (Desert) produced
// unrestricted mana on main. The pins below cover both halves: the batch is
// registered, and a TYPED unit under an unusable restriction is hidden from
// the typed tally as well as the pool (takeUnit partitions a slot by the
// tally, so a visible typed unit could otherwise be consumed through that
// path).

// TestBucolicRanchRestrictedDesertManaKeepsItsRestriction activates the real
// corpus card's restricted mana ability and asserts the produced unit is BOTH
// Desert-tagged AND restricted: the batch is registered, and its provenance
// is admitted for a Mount spell and withheld -- in pool and in the typed
// tally -- from a non-Mount.
func TestBucolicRanchRestrictedDesertManaKeepsItsRestriction(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	mountain := searchCorpusCard(t, reg, "Mountain")
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	walloper := card(t, "Name:Walloper\nManaCost:1\nTypes:Artifact Creature Golem\nPT:3/3\nOracle:x\n")
	deck := []*cards.Card{searchCorpusCard(t, reg, "Bucolic Ranch"), searchCorpusCard(t, reg, "Bulwark Ox"), walloper}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9211, Names: []string{"rancher", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	ranch := searchMoveByName(t, e, "Bucolic Ranch", state.ZBattlefield)
	mountID := searchMoveByName(t, e, "Bulwark Ox", state.ZHand)
	wallID := searchMoveByName(t, e, "Walloper", state.ZHand)
	e.pending = nil
	e.priorityRound()

	// The restricted ability: {T}: Add one mana of any color. Spend this mana
	// only to cast a Mount spell.
	idx := -1
	for i, ab := range e.G.Obj(ranch).Face().ManaAbilities() {
		if ab.Params["RestrictValid"] != "" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("Bucolic Ranch has no restricted mana ability")
	}
	act := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "activate" && o.Obj == ranch {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatalf("priority decision does not offer Bucolic Ranch's mana: %+v", e.Pending())
	}
	submitChoices(t, e, act)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pending = %+v, want the mana-ability wheel", d)
	}
	wheel := -1
	for _, o := range d.Options {
		if o.Ability == idx {
			wheel = o.Index
		}
	}
	if wheel < 0 {
		t.Fatalf("no wheel option for the restricted ability: %+v", d.Options)
	}
	submitChoices(t, e, wheel)
	// The restricted "any color" ability asks which colour (the restriction
	// needs a concrete pool slot); take white so a Mount spell can use it.
	if cd := e.Pending(); cd != nil && cd.Kind == decision.KChoose {
		submitChoices(t, e, 0)
	}

	// The batch exists AND carries the Desert tag (the regression: it was
	// dropped, so this slice was empty).
	restricted := e.G.Players[0].RestrictedMana
	if len(restricted) != 1 || restricted[0].Amount != 1 ||
		restricted[0].Color != "DesertW" || restricted[0].Valid != "Spell.Mount" {
		t.Fatalf("restricted batches = %+v, want one DesertW batch valid for Spell.Mount", restricted)
	}
	if e.G.Players[0].TypedMana[state.TypedDesert][state.MW] != 1 {
		t.Fatalf("Desert typed tally W = %d, want 1", e.G.Players[0].TypedMana[state.TypedDesert][state.MW])
	}

	// Admitted for the Mount spell, withheld from the non-Mount in BOTH the
	// pool and the typed tally.
	if av := pay.AvailableFor(asPayer(e), 0, paymentFor(mountID, false, Cost{})); av.Pool.Total() != 1 ||
		av.Typed[state.TypedDesert][state.MW] != 1 {
		t.Fatalf("manaAvailableFor(Mount) = pool %d typed %d, want 1/1 (restriction admitted)",
			av.Pool.Total(), av.Typed[state.TypedDesert][state.MW])
	}
	if av := pay.AvailableFor(asPayer(e), 0, paymentFor(wallID, false, Cost{})); av.Pool.Total() != 0 ||
		av.Typed[state.TypedDesert][state.MW] != 0 {
		t.Fatalf("manaAvailableFor(non-Mount) = pool %d typed %d, want 0/0 (restriction withheld)",
			av.Pool.Total(), av.Typed[state.TypedDesert][state.MW])
	}

	// Behavioural: the non-Mount {1} cast is not offered on the restricted-only
	// pool; without the typed filter the search would reach the hidden unit
	// through the typed tally and wrongly offer it.
	e.pending = nil
	e.priorityRound()
	offered := map[string]bool{}
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" {
			offered[e.G.Obj(o.Obj).Face().Name] = true
		}
	}
	if offered["Walloper"] {
		t.Fatal("non-Mount cast offered on restricted-only pool -- the restriction is unread for a tagged unit")
	}
}

// TestRestrictedTaggedManaSpendIsNotDoubleCounted pins the emission split for
// a restricted TAGGED unit beside a plain one in the same slot. The carve
// emits the tagged negative directly, so the split loop must not emit it
// again: a double emit would drive TypedMana (and the pool) negative. A {2}
// cost over one restricted Cave {C} and one plain {C} admits the restriction
// for a Mount spell and spends both units.
func TestRestrictedTaggedManaSpendIsNotDoubleCounted(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Bulwark Ox"))
	mount := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "CaveC", Amount: 1,
		Text: events.ManaRestrictionText("Spell.Mount", 0)})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
	if len(e.G.Players[0].RestrictedMana) != 1 {
		t.Fatalf("restricted batch not registered: %+v", e.G.Players[0].RestrictedMana)
	}
	if ok, _, _, _, _ := pay.PayManaForSpent(asPayer(e), 0, mount, false, ParseCost("2"), nil, pipRider{}); !ok {
		t.Fatal("payment refused: the restricted Cave unit is admitted for a Mount spell")
	}
	if got := e.G.Players[0].Pool[state.MC]; got != 0 {
		t.Fatalf("pool MC = %d, want 0 (two units spent, each emitted once)", got)
	}
	if got := e.G.Players[0].TypedMana[state.TypedCave][state.MC]; got != 0 {
		t.Fatalf("Cave typed tally MC = %d, want 0 (the carved tagged unit must not be re-emitted)", got)
	}
	if len(e.G.Players[0].RestrictedMana) != 0 {
		t.Fatalf("restricted batches = %+v, want consumed", e.G.Players[0].RestrictedMana)
	}
}

// TestRestrictedTaggedManaSpendNeverOverSpends pins the shape the
// double-count pin above cannot reach: a restricted TAGGED batch beside
// PLAIN mana of the same colour and a pool STRICTLY LARGER than the cost.
// The payment search (takeUnit) consumes a plain unit before a typed one,
// so a {1}{W} cast over one restricted DesertW unit plus two plain W units
// spends the two plain units and never touches the Desert unit. The carve
// must therefore attribute the restricted batch zero units: attributing it
// min(spent, batch.Amount) would drive the Desert emission tally negative,
// and the split loop's `plain := spent - snow - typed` subtraction would be
// inflated by the negative term -- the pool would lose three units for a
// two-mana cost. The emitted spend events must sum to exactly the cost, and
// the pay-time capture must agree with them (no Desert spend either way).
func TestRestrictedTaggedManaSpendNeverOverSpends(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		counters []string // the Counter forms emitted, in order
	}{
		{"restricted first", []string{"DesertW", "W", "W"}},
		{"plain first", []string{"W", "W", "DesertW"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := handEngineTokens(t, corpusAlternativeCard(t, "Bulwark Ox"))
			mount := e.G.Zone(state.ZHand, 0)[0]
			for _, counter := range tc.counters {
				if counter == "DesertW" {
					e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: counter, Amount: 1,
						Text: events.ManaRestrictionText("Spell.Mount", 0)})
				} else {
					e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: counter, Amount: 1})
				}
			}
			if len(e.G.Players[0].RestrictedMana) != 1 {
				t.Fatalf("restricted batch not registered: %+v", e.G.Players[0].RestrictedMana)
			}
			before := e.G.Players[0].Pool.Total()
			ok, _, _, _, typed := pay.PayManaForSpent(asPayer(e), 0, mount, false, ParseCost("1 W"), nil, pipRider{})
			if !ok {
				t.Fatal("payment refused: the restricted Desert unit is admitted for a Mount spell")
			}
			if got := before - e.G.Players[0].Pool.Total(); got != 2 {
				t.Fatalf("pool lost %d mana paying {1}{W}, want exactly 2 (the search spends plain units, so the restricted tagged unit stays)", got)
			}
			if typed[state.TypedDesert][state.MW] != 0 {
				t.Fatalf("Desert typed spend = %d, want 0 (plain units were consumed first)", typed[state.TypedDesert][state.MW])
			}
			if got := e.G.Players[0].TypedMana[state.TypedDesert][state.MW]; got != 1 {
				t.Fatalf("Desert tally = %d, want 1 (an unspent restricted tagged unit keeps its provenance)", got)
			}
			if rest := e.G.Players[0].RestrictedMana; len(rest) != 1 || rest[0].Amount != 1 {
				t.Fatalf("restricted batches = %+v, want one unspent DesertW batch", rest)
			}
		})
	}
}

// TestRestrictedTaggedManaPartialConsumptionIsExact pins a PARTIAL carve: a
// restricted DesertW batch of TWO units alone in its slot, paying a single
// {W} pip. The search has no plain unit to prefer and consumes one typed
// unit, so the carve takes exactly one of the batch's two units. The pool
// loses exactly one, the Desert tally drops by one, one unit of the batch
// remains, and the pay-time capture (ManaDesertSpent) agrees with the emitted
// tally delta -- the carve's restricted-first attribution and the search's
// consumption can never disagree.
func TestRestrictedTaggedManaPartialConsumptionIsExact(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Bulwark Ox"))
	mount := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "DesertW", Amount: 2,
		Text: events.ManaRestrictionText("Spell.Mount", 0)})
	if rest := e.G.Players[0].RestrictedMana; len(rest) != 1 || rest[0].Amount != 2 {
		t.Fatalf("restricted batches = %+v, want one DesertW batch of 2", rest)
	}
	before := e.G.Players[0].Pool.Total()
	ok, _, _, _, typed := pay.PayManaForSpent(asPayer(e), 0, mount, false, ParseCost("W"), nil, pipRider{})
	if !ok {
		t.Fatal("payment refused")
	}
	if got := before - e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool lost %d mana paying {W}, want exactly 1", got)
	}
	if got := typed[state.TypedDesert][state.MW]; got != 1 {
		t.Fatalf("Desert typed spend = %d, want 1 (one of the batch's two units)", got)
	}
	if got := e.G.Players[0].TypedMana[state.TypedDesert][state.MW]; got != 1 {
		t.Fatalf("Desert tally = %d, want 1 (one of two units spent)", got)
	}
	if rest := e.G.Players[0].RestrictedMana; len(rest) != 1 || rest[0].Amount != 1 {
		t.Fatalf("restricted batches = %+v, want one DesertW batch of 1 left", rest)
	}
}

// TestEmitManaTagsTreasureProducerType pins the PRODUCER side of the typed
// provenance end to end: every other leaf in this file seeds the pool with a
// hand-written "TreasureC"/"CaveW"/"DesertR" Counter, so the effect-side
// tagging (effMana reading the producing permanent's Face().Types) could
// break without failing a single test. Here a real c_a_treasure_sac token is
// created, its printed mana ability activated, and the produced unit
// asserted to be BOTH in the pool AND in the Treasure tally.
func TestEmitManaTagsTreasureProducerType(t *testing.T) {
	t.Parallel()
	e := handEngineTokens(t, corpusAlternativeCard(t, "Marut"))
	e.emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "c_a_treasure_sac"})
	var tok state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken {
			tok = id
		}
	}
	if tok == 0 {
		t.Fatal("the Treasure token was not minted")
	}
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision")
	}
	act := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == tok {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatalf("the Treasure token's mana ability is not offered: %+v", d.Options)
	}
	submitChoices(t, e, act)
	// "Add one mana of any color" asks which colour; take white.
	if cd := e.Pending(); cd != nil && cd.Kind == decision.KChoose {
		submitChoices(t, e, 0)
	}
	if got := e.G.Players[0].Pool[state.MW]; got != 1 {
		t.Fatalf("pool W = %d, want 1 (the token's ability produced one white unit)", got)
	}
	if got := e.G.Players[0].TypedMana[state.TypedTreasure][state.MW]; got != 1 {
		t.Fatalf("Treasure tally W = %d, want 1 (effMana must tag the unit from the producer's Types)", got)
	}
	if got := e.G.Players[0].Snow[state.MW]; got != 0 {
		t.Fatalf("snow tally W = %d, want 0 (a Treasure is not snow)", got)
	}
}
