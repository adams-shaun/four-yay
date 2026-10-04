package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// One test per card for the land/mana misc parameter-census work (task
// inbox-paramcensus-land-mana-misc): Lion's Eye Diamond's InstantSpeed$,
// Myriad Landscape's ShareLandType$, Mistveil Plains' IsPresent$/PresentCompare$,
// and Eldrazi Temple's RestrictValid$ pin. Each is the real corpus card (the
// inline Walloper fixture below is a synthetic stand-in for "any {3}
// non-Eldrazi creature", never a Forge script).

// wallopersDeck builds a seat-0 deck from the named corpus cards followed by
// the search fixtures' filler (8×(Forest, Mountain), then Grizzly Bears to
// 40), and an opponent deck of mountains.
func wallopersDeck(t *testing.T, reg *cards.Registry, named ...string) [][]*cards.Card {
	t.Helper()
	mountain := searchCorpusCard(t, reg, "Mountain")
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, len(named))
	for _, name := range named {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
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
	return [][]*cards.Card{deck, opp}
}

// TestEldraziTempleRestrictValidGatesTheProducedMana pins the RestrictValid$
// read the census no longer labels (the brief's premise that it was unread
// did not reproduce — restrictValidMatches genuinely prices it): the temple's
// second ability adds {C}{C} whose pool batch carries the spend restriction,
// so a colourless Eldrazi spell ({2}{C} Matter Reshaper) is payable with the
// pool alone while a plain {3} creature is not — the restricted batch stays
// out of manaAvailableFor for it.
func TestEldraziTempleRestrictValidGatesTheProducedMana(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	mountain := searchCorpusCard(t, reg, "Mountain")
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	walloper := card(t, "Name:Walloper\nManaCost:3\nTypes:Artifact Creature Golem\nPT:3/3\nOracle:x\n")
	deck := []*cards.Card{searchCorpusCard(t, reg, "Eldrazi Temple"), searchCorpusCard(t, reg, "Matter Reshaper"), walloper}
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
	cfg := seatZeroStart(Config{Seed: 9210, Names: []string{"templist", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	temple := searchMoveByName(t, e, "Eldrazi Temple", state.ZBattlefield)
	reshaper := searchMoveByName(t, e, "Matter Reshaper", state.ZHand)
	wallID := searchMoveByName(t, e, "Walloper", state.ZHand)
	e.pending = nil
	e.priorityRound()

	// The priority wheel: the "activate for mana" option, then the mana-ability
	// choose. Face order puts the plain Add {C} first, the restricted Add {C}{C}
	// second, and the wheel carries that per-ability index.
	idx := 1
	for i, ab := range e.G.Obj(temple).Face().ManaAbilities() {
		if ab.Params["RestrictValid"] != "" {
			idx = i
		}
	}
	act := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "activate" && o.Obj == temple {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatal("priority decision does not offer the temple's mana")
	}
	submitChoices(t, e, act)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "mana" {
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

	restricted := e.G.Players[0].RestrictedMana
	if len(restricted) != 1 || restricted[0].Amount != 2 || restricted[0].Color != "C" {
		t.Fatalf("restricted pool batches = %+v, want one {C}{C} batch", restricted)
	}
	if e.G.Players[0].Pool.Total() != 2 {
		t.Fatalf("pool total = %d, want 2", e.G.Players[0].Pool.Total())
	}

	// The payment-class read: the restricted batch is admitted for the
	// colourless Eldrazi spell's cast ({2}{C}) and withheld from the plain
	// {3} creature's cast. One ordinary {C} joins the pool so either cast is
	// one source of truth away from payable — the {2}{C} needs three mana and
	// the temple produces two.
	addMana(t, e, 0, "C")
	if got := pay.AvailableFor(asPayer(e), 0, paymentFor(reshaper, false, Cost{})).Pool.Total(); got != 3 {
		t.Fatalf("manaAvailableFor(Matter Reshaper) = %d, want 3 (restriction admitted)", got)
	}
	if got := pay.AvailableFor(asPayer(e), 0, paymentFor(wallID, false, Cost{})).Pool.Total(); got != 1 {
		t.Fatalf("manaAvailableFor(Walloper) = %d, want 1 (restriction withheld)", got)
	}

	// Behavioural: the priority decision offers the Eldrazi cast and not the
	// plain one.
	e.pending = nil
	e.priorityRound()
	d = e.Pending()
	offered := map[string]bool{}
	for _, o := range d.Options {
		if o.Kind == "cast" {
			offered[e.G.Obj(o.Obj).Face().Name] = true
		}
	}
	if !offered["Matter Reshaper"] {
		t.Fatalf("Eldrazi cast not offered with restricted-only pool: %+v", offered)
	}
	if offered["Walloper"] {
		t.Fatal("plain {3} cast offered on restricted-only pool — the restriction is unread")
	}

	// And the admitted cast really spends the restricted batch.
	castOpt := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == reshaper {
			castOpt = o.Index
		}
	}
	submitChoices(t, e, castOpt)
	if e.G.Obj(reshaper).Zone != state.ZStack {
		t.Fatalf("Matter Reshaper in %s, want the stack", e.G.Obj(reshaper).Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 || len(e.G.Players[0].RestrictedMana) != 0 {
		t.Fatalf("pool %d / restricted %v after payment, want both empty",
			e.G.Players[0].Pool.Total(), e.G.Players[0].RestrictedMana)
	}
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}

// TestLionEyeDiamondInstantSpeedWithholdsFromPaymentWindow pins the
// InstantSpeed$ read: "Activate only as an instant" means the controller must
// hold priority, so LED is offered at a priority window but withheld from the
// CR 601.2g cast-payment window (where the payer holds no priority) — its
// source is not even offered there, while an ordinary land is.
func TestLionEyeDiamondInstantSpeedWithholdsFromPaymentWindow(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	deck := wallopersDeck(t, reg, "Lion's Eye Diamond", "Grizzly Bears")
	cfg := seatZeroStart(Config{Seed: 9211, Names: []string{"stormer", "opponent"},
		Decks: deck, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	led := searchMoveByName(t, e, "Lion's Eye Diamond", state.ZBattlefield)
	forest := searchMoveByName(t, e, "Forest", state.ZBattlefield)
	bears := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	e.pending = nil
	e.priorityRound()

	// At priority the LED is a normal activatable mana ability.
	d := e.Pending()
	ledOffered := false
	for _, o := range d.Options {
		ledOffered = ledOffered || (o.Kind == "activate" && o.Obj == led)
	}
	if !ledOffered {
		t.Fatal("priority decision does not offer LED's mana ability")
	}

	// Cast the {1}{G} bears on a {G}-only pool: the offer gate is pool-only,
	// so the option is withheld from legalActions — drive the proposal the
	// reversal tests' way (beginCast directly) and the 601.2g payment window
	// opens, where LED's source must be withheld.
	addMana(t, e, 0, "G")
	e.pending = nil
	e.beginCast(0, decision.Option{Kind: "cast", Obj: bears})
	e.Advance()
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "activate" {
		t.Fatalf("pending = %+v, want the 601.2g mana window", d)
	}
	sources := map[state.ObjID]bool{}
	for _, o := range d.Options {
		if o.Kind == "activate" {
			sources[o.Obj] = true
		}
	}
	if sources[led] {
		t.Fatal("payment window offers LED — the InstantSpeed$ restriction is unread")
	}
	if !sources[forest] {
		t.Fatalf("payment window lost the ordinary Forest source: %v", sources)
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(bears).Zone != state.ZStack {
		t.Fatalf("Grizzly Bears in %s, want the stack after the window paid", e.G.Obj(bears).Zone)
	}
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}

// TestMyriadLandscapeSearchRequiresSharedLandType pins the ShareLandType$
// read: the search's answer may take two basics only when they share a land
// type. A disjoint pair is rejected at Submit (the decision survives for a
// legal answer); a shared pair moves both to the battlefield tapped.
func TestMyriadLandscapeSearchRequiresSharedLandType(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	deck := wallopersDeck(t, reg, "Myriad Landscape")
	cfg := seatZeroStart(Config{Seed: 9212, Names: []string{"tutor", "opponent"},
		Decks: deck, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	ml := searchMoveByName(t, e, "Myriad Landscape", state.ZBattlefield)
	// It entered tapped (its ETB replacement); the {2} activation needs it up.
	e.emit(events.Event{Kind: events.Untap, Obj: ml})
	addMana(t, e, 0, "CC")

	d := activateSearch(t, e, ml)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("pending = %+v, want a search KChoose", d)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("Min/Max = %d/%d, want 0/2", d.Min, d.Max)
	}
	forests, mountains := []int{}, []int{}
	for _, o := range d.Options {
		switch o.Label {
		case "Forest":
			forests = append(forests, o.Index)
		case "Mountain":
			mountains = append(mountains, o.Index)
		}
	}
	if len(forests) < 2 || len(mountains) == 0 {
		t.Fatalf("library lacks the fixture basics: forests %v mountains %v", forests, mountains)
	}

	// A Forest+Mountain pair does not share a land type: rejected, pending
	// decision survives.
	err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{forests[0], mountains[0]}})
	if err == nil {
		t.Fatal("disjoint-type search answer accepted")
	}
	if p := e.Pending(); p == nil || p.Seq != d.Seq {
		t.Fatalf("rejected answer did not preserve the pending decision: %+v", p)
	}
	// A single card is trivially legal; a shared pair is legal.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{mountains[0]}}); err != nil {
		t.Fatalf("single-card answer rejected: %v", err)
	}
	submitChoices(t, e, forests[0], forests[1])
	for _, i := range forests[:2] {
		id := d.Options[i].Obj
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || !o.Tapped {
			t.Fatalf("selected Forest %d = %+v, want tapped battlefield", id, o)
		}
	}
	shuffled := false
	for _, ev := range e.L.Events {
		shuffled = shuffled || ev.Kind == events.Shuffle
	}
	if !shuffled {
		t.Fatal("the shared-type search did not shuffle")
	}
	replayCheck(t, e, cfg)
}

// TestMistveilPlainsGatesOnWhitePermanents pins the IsPresent$/PresentCompare$
// activation gate: Mistveil Plains' graveyard-recall ability is offered only
// while its controller has two or more white permanents (PresentCompare$ GE2
// over IsPresent$ Permanent.White+YouCtrl), and when offered it really moves
// the targeted card to the library's bottom.
func TestMistveilPlainsGatesOnWhitePermanents(t *testing.T) {
	reg := searchTestRegistry(t)
	deck := wallopersDeck(t, reg, "Mistveil Plains")
	cfg := seatZeroStart(Config{Seed: 9213, Names: []string{"cleric", "opponent"},
		Decks: deck, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	mv := searchMoveByName(t, e, "Mistveil Plains", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Untap, Obj: mv})
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZLibrary)
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZLibrary, To: state.ZGraveyard})
	e.pending = nil
	e.priorityRound()

	// The {W} cost is pre-charged BEFORE the no-white-permanents check so
	// the cost-payable offer gate cannot withhold the ability and mask the
	// IsPresent$/PresentCompare$ gate under test; the resolution below then
	// pays that cost straight from the pool.
	addMana(t, e, 0, "W")

	idx := -1
	for i, ab := range e.G.Obj(mv).Face().Abilities {
		if ab.Kind == "AB" && ab.API == "ChangeZone" && ab.Params["IsPresent"] != "" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("Mistveil Plains has no IsPresent$ ChangeZone ability")
	}
	if _, ok := findAbilityOption(e, mv, idx); ok {
		t.Fatal("ability offered with no white permanents — the IsPresent$ gate is unread")
	}

	// Two white creatures satisfy PresentCompare$ GE2.
	for i := 0; i < 2; i++ {
		putToken(t, e, 0, "Name:Chaplain\nManaCost:W\nTypes:Creature Human Cleric\nPT:1/1\nOracle:x\n", state.ZBattlefield)
	}
	e.priorityRound() // refresh the pending ask after putToken left it nil
	opt := abilityOption(t, e, mv, idx)
	submitChoices(t, e, opt.Index)

	// The targeted graveyard card goes to the BOTTOM of the library
	// (LibraryPosition$ -1) once the ability resolves.
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the target ask", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bear {
		t.Fatalf("target options %+v, want the graveyard bear", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(bear)
	if o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("recalled card in %+v, want library", o)
	}
	if last := e.G.Zone(state.ZLibrary, 0); last[len(last)-1] != bear {
		t.Fatalf("bear at position %d of %v, want the bottom", slices.Index(last, bear), last)
	}
	replayCheck(t, e, cfg)
}
