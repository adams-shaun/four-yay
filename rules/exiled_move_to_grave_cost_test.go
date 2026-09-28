package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The ExiledMoveToGrave<N/Spec> cost ticket. Forge's token moves N cards
// matching Spec out of EXILE into their OWNER's graveyard as a cost payment
// (the Eldrazi processor family and Shelob, Dread Weaver's {2}{B} ability).
// Before this ticket the token hit ParseCost's unrecognised-symbol fallback:
// a phantom {1} rode the price (a player with exactly {2}{B} could not
// activate Shelob) and the cost's governing action was silently dropped
// while the ability still resolved -- a fail-open defect. The fix parses the
// head into Cost.MoveToGrave, gates the offer on real exile-zone candidates
// (any player's -- exiled cards live in their OWNER's exile zone), asks and
// settles the move on the cast/activation flow, and wires the trigger-body
// cost window the same way.
//
// All fixtures are real compiled corpus cards; none of the 16 carriers is in
// any repo deck, so TestHeads does not depend on these cards' behaviour
// changing.

// exileGraveEngine is searchEngine's shape with a creature-bearing opponent
// deck: the ExiledMoveToGrave candidates live in the OPPONENT's exile zone
// (Shelob's trigger exiles an opponent's creature; Card.OppOwn names cards
// an opponent of the payer owns), so seat 1's deck must hold creature cards
// for the fixture to have anything to move.
func exileGraveEngine(t *testing.T, reg *cards.Registry, seed uint64, fixtures ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
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
		opp[i] = bear
	}
	cfg := Config{Seed: seed, Names: []string{"payer", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// findCardByName returns (without moving) seat 0's card named name in its
// hand or library.
func findCardByName(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				return id
			}
		}
	}
	t.Fatalf("card %q absent from seat 0's hand/library", name)
	return 0
}

// findOpponentCard returns (without moving) the first card owned by seat 1
// sitting in its hand or library.
func findOpponentCard(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 1) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil {
				return id
			}
		}
	}
	t.Fatal("opponent holds no card")
	return 0
}

// exileOpponentCard moves one card owned by seat 1 into seat 1's exile zone,
// recording the exiling source the way a real ChangeZone does (the MoveZone
// event's IDs payload -- events/apply.go's default ExiledWith branch). It
// returns the moved id.
func exileOpponentCard(t *testing.T, e *Engine, exiler state.ObjID) state.ObjID {
	t.Helper()
	id := findOpponentCard(t, e)
	z := e.G.Obj(id).Zone
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z,
		To: state.ZExile, IDs: []state.ObjID{exiler}})
	e.pending = nil
	return id
}

// inOwnerGraveyard reports whether id sits in its OWNER's graveyard.
func inOwnerGraveyard(e *Engine, id state.ObjID) bool {
	o := e.G.Obj(id)
	return o != nil && o.Zone == state.ZGraveyard && o.Owner != 0 &&
		func() bool {
			for _, zid := range e.G.Zone(state.ZGraveyard, o.Owner) {
				if zid == id {
					return true
				}
			}
			return false
		}()
}

// TestShelobExiledMoveToGravePaysExactlyAndMovesTheCard pins the Shelob,
// Dread Weaver end-to-end shape on the real compiled face: with a pool of
// EXACTLY {2}{B}'s worth of mana (three pips -- one fewer than the phantom
// {3}{B} the pre-fix parse priced) the ability is offered, the cost pick is
// a real choice over the exile zone, and paying moves the exiled card to its
// OWNER's graveyard while the counters land and the SubAbility$ draw runs.
func TestShelobExiledMoveToGravePaysExactlyAndMovesTheCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := exileGraveEngine(t, reg, 4223, "Shelob, Dread Weaver")
	shelob := searchMoveByName(t, e, "Shelob, Dread Weaver", state.ZBattlefield)
	card := exileOpponentCard(t, e, shelob)
	addMana(t, e, 0, "BBB") // exactly {2}{B}; {3}{B} would be one pip short

	if e.G.Obj(card).Zone != state.ZExile {
		t.Fatalf("exiled card zone = %s, want Exile before the activation", e.G.Obj(card).Zone)
	}
	if e.G.Obj(card).ExiledWith != shelob {
		t.Fatalf("exiled card ExiledWith = %d, want %d (the Shelob shape)", e.G.Obj(card).ExiledWith, shelob)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))

	// The ability (index 0, PutCounter) must be OFFERED at exactly {2}{B} --
	// the offer itself pins the missing phantom {1}.
	opt, ok := findAbilityOption(e, shelob, 0)
	if !ok {
		t.Fatalf("Shelob's counter ability not offered on a {2}{B} pool: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)

	// The cost pick is a real KChoose over the exile zone's candidates.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the move-to-graveyard cost ask, got %+v", d)
	}
	found := -1
	for _, o := range d.Options {
		if o.Kind == "movetogravecost" && o.Obj == card {
			found = o.Index
		}
	}
	if found < 0 {
		t.Fatalf("cost ask does not offer the exiled card: %+v", d.Options)
	}
	submitChoices(t, e, found)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(card).Zone; got != state.ZGraveyard {
		t.Fatalf("paid card zone = %s, want Graveyard (the cost's governing action)", got)
	}
	if !inOwnerGraveyard(e, card) {
		t.Fatalf("paid card is not in its OWNER's (seat 1's) graveyard")
	}
	if got := e.G.Obj(shelob).Counter("P1P1"); got != 2 {
		t.Fatalf("Shelob P1P1 = %d, want 2", got)
	}
	if got := e.G.Players[0].Pool[state.MB]; got != 0 {
		t.Fatalf("pool {B} = %d after paying, want 0", got)
	}
	if got := e.G.Obj(shelob).Zone; got != state.ZBattlefield {
		t.Fatalf("Shelob zone = %s, want Battlefield", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand = %d cards, want %d (the SubAbility$ TrigDraw draw)", got, handBefore+1)
	}
	replayCheck(t, e, cfg)
}

// TestShelobExiledMoveToGraveWithheldOnAShortPool pins the fail-closed half
// of the gate: with NO qualifying card in any exile zone the ability is not
// offered (the totality rule -- an option that cannot be paid is never
// offered).
func TestShelobExiledMoveToGraveWithheldOnAShortPool(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := exileGraveEngine(t, reg, 4224, "Shelob, Dread Weaver")
	shelob := searchMoveByName(t, e, "Shelob, Dread Weaver", state.ZBattlefield)
	addMana(t, e, 0, "BBB")
	// No card exiled with Shelob anywhere: the ability must be withheld.
	if _, ok := findAbilityOption(e, shelob, 0); ok {
		t.Fatalf("Shelob's counter ability offered with nothing exiled: %+v", e.Pending())
	}
}

// TestWastelandStranglerTriggerCostMovesTheCard wires the trigger-body
// window: Wasteland Strangler's ETB carries
// `Cost$ ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>`; answering
// PAY moves a card the opponent owns from exile into that player's graveyard
// and the body resolves (the -3/-3 pump on the chosen creature).
func TestWastelandStranglerTriggerCostMovesTheCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := exileGraveEngine(t, reg, 4225, "Wasteland Strangler", "Grizzly Bears")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	// The victim must already be in exile when the strangler enters: its ETB
	// trigger asks its target as soon as it is put on the stack, and the
	// pending ask must be the one the test answers.
	stranglerID := findCardByName(t, e, "Wasteland Strangler")
	victim := exileOpponentCard(t, e, stranglerID)
	// searchMoveByName leaves the trigger's target ask pending -- do not
	// touch e.pending past this point.
	searchMoveByName(t, e, "Wasteland Strangler", state.ZBattlefield)

	// The ETB trigger's own target ask (the body's Pump targets a creature
	// when the trigger hits the stack), then "you may ..." -- answer yes.
	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KTarget {
		t.Fatalf("expected the pump target ask, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("pump target ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTriggerOptional || len(d.Options) == 0 || d.Options[0].Kind != "yes" {
		t.Fatalf("expected the trigger_optional yes ask, got %+v", d)
	}
	submitChoices(t, e, 0)

	pay, _ := triggerCostWindowAsk(t, e)
	if pay < 0 {
		t.Fatalf("the ExiledMoveToGrave body was not offered as payable: %+v", e.Pending())
	}
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(victim).Zone; got != state.ZGraveyard {
		t.Fatalf("paid card zone = %s, want Graveyard (the paid cost's move)", got)
	}
	if !inOwnerGraveyard(e, victim) {
		t.Fatalf("paid card is not in its OWNER's (seat 1's) graveyard")
	}
	if got := e.Derived(bear).Power; got != -1 {
		t.Fatalf("bear power = %d, want -1 (the paid body's -3/-3)", got)
	}
	replayCheck(t, e, cfg)
}

// TestWastelandStranglerTriggerCostDeclineChangesNothing pins the decline
// arm: a declined ExiledMoveToGrave body leaves the exiled card alone and
// never pumps.
func TestWastelandStranglerTriggerCostDeclineChangesNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := exileGraveEngine(t, reg, 4226, "Wasteland Strangler", "Grizzly Bears")
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	stranglerID := findCardByName(t, e, "Wasteland Strangler")
	victim := exileOpponentCard(t, e, stranglerID)
	// searchMoveByName leaves the trigger's target ask pending -- do not
	// touch e.pending past this point.
	searchMoveByName(t, e, "Wasteland Strangler", state.ZBattlefield)

	// The ETB trigger's target ask, then the optional ask.
	d := passUntilNonPriority(t, e, 40)
	if d.Kind != decision.KTarget {
		t.Fatalf("expected the pump target ask, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("pump target ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTriggerOptional {
		t.Fatalf("expected the trigger_optional ask, got %+v", d)
	}
	submitChoices(t, e, 0)

	_, decline := triggerCostWindowAsk(t, e)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(victim).Zone; got != state.ZExile {
		t.Fatalf("declined card zone = %s, want Exile (a decline pays nothing)", got)
	}
	if got := e.Derived(bear).Power; got != 2 {
		t.Fatalf("bear power = %d, want 2 (a decline never pumps)", got)
	}
}

// TestProcessorAssaultSpellAdditionalCostMovesTheCard is the spell half of
// the same token: Processor Assault's `A:SP$ DealDamage | Cost$ 1 R
// ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>` carries the cost on
// the SPELL's own Cost$ (an additional cost), folded in by
// withSpellAbilityExtras. Before that fold the parsed Cost.MoveToGrave was
// dropped on the floor for a spell, so the spell was offered, resolved, dealt
// its 5 damage and left the opponent-owned exiled card untouched -- the same
// fail-open defect the AB half closed, in its spell spelling.
//
// The offer itself is gated on a real candidate (an opponent-owned card in
// exile); with one present the cast is offered at exactly {1}{R}, the cost
// pick is a real KChoose over the exile zone, and paying moves the card to its
// OWNER's graveyard before the damage resolves.
func TestProcessorAssaultSpellAdditionalCostMovesTheCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := exileGraveEngine(t, reg, 4227, "Processor Assault", "Grizzly Bears")
	spell := findCardByName(t, e, "Processor Assault")
	if got := e.G.Obj(spell).Zone; got == state.ZLibrary {
		e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
	}
	if got := e.G.Obj(spell).Zone; got != state.ZHand {
		t.Fatalf("Processor Assault zone = %s, want Hand", got)
	}
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	victim := exileOpponentCard(t, e, spell)
	addMana(t, e, 0, "CR") // exactly {1}{R}: the printed cost, no phantom {1}

	// Offered only because an opponent-owned card sits in exile.
	if !plainCastOffered(e, 0, spell) {
		t.Fatalf("Processor Assault not offered at {1}{R} with an exile candidate: %+v", e.Pending())
	}
	opt := castOptionFor(t, e, spell)
	submitChoices(t, e, opt.Index)

	// The additional-cost pick: a real KChoose over the exile zone.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the move-to-graveyard cost ask, got %+v", d)
	}
	found := -1
	for _, o := range d.Options {
		if o.Kind == "movetogravecost" && o.Obj == victim {
			found = o.Index
		}
	}
	if found < 0 {
		t.Fatalf("cost ask does not offer the exiled card: %+v", d.Options)
	}
	submitChoices(t, e, found)

	// The spell's target ask, then resolution.
	d = passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTarget {
		t.Fatalf("expected the damage target ask, got %+v", d)
	}
	tidx := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			tidx = o.Index
		}
	}
	if tidx < 0 {
		t.Fatalf("damage target ask does not offer the bear: %+v", d.Options)
	}
	submitChoices(t, e, tidx)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(victim).Zone; got != state.ZGraveyard {
		t.Fatalf("paid card zone = %s, want Graveyard (the spell's additional cost)", got)
	}
	if !inOwnerGraveyard(e, victim) {
		t.Fatalf("paid card is not in its OWNER's (seat 1's) graveyard")
	}
	if got := e.G.Obj(bear).Zone; got != state.ZGraveyard {
		t.Fatalf("bear zone = %s, want Graveyard (died to Processor Assault's NumDmg$ 5 on a 2/2)", got)
	}
	replayCheck(t, e, cfg)
}

// TestProcessorAssaultWithheldWithoutAnExiledCandidate pins the spell offer
// gate: no opponent-owned card in exile, no cast (the additional cost cannot
// be paid).
func TestProcessorAssaultWithheldWithoutAnExiledCandidate(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := exileGraveEngine(t, reg, 4228, "Processor Assault")
	spell := findCardByName(t, e, "Processor Assault")
	addMana(t, e, 0, "CR")
	if plainCastOffered(e, 0, spell) {
		t.Fatalf("Processor Assault offered with nothing in exile: %+v", e.Pending())
	}
}
