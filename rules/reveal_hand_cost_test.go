// Land Grant's "reveal your hand" alternative cost (Reveal<1/Hand>) and its
// siblings. Found by the SpellBench kernel-shadow comparison: with a land-free
// hand gorge offered only pass/concede where mtg-kernel offered the free cast.
// Root cause (measured, pre-fix): the choice-cost parser read Reveal<1/Hand>
// as "reveal ONE card matching the card filter Hand" -- a filter no card ever
// matches -- so the cost was judged unpayable and the option withheld. In
// Forge (and every corpus carrier) the type slot "Hand" means the whole hand,
// the same reading Discard<1/Hand> already gets. The file pins:
//
//   - the whole-hand reveal is payable with ANY hand, including an empty one
//     (CR 701.20a: revealing a hand with no cards is a legal reveal);
//   - the reveal names the hand's cards as a public cost note and pays no
//     mana (the alternative replaces the mana cost, CR 118.9);
//   - Land Grant's CheckSVar$/SVarCompare$ gate (no land cards in hand) is
//     evaluated by the alternative-cost offer walk -- a Forest in hand
//     withholds the alternative;
//   - the whole-hand reading reaches an activated-ability carrier too
//     (Sasaya, Orochi Ascendant's "Reveal your hand: ...");
//   - the Reveal<1/CARDNAME> forecast family is payable from the card's own
//     hand: the source is still in the hand when its ability activates, so
//     the self-reveal includes it rather than matching nothing.
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// revealNote returns the first cost-reveal note event naming every one of
// names in its text, failing the test when none exists.
func revealNote(t *testing.T, e *Engine, names ...string) events.Event {
	t.Helper()
	for _, ev := range e.L.Events {
		if ev.Kind != events.Note || !strings.Contains(ev.Text, "as a cost") {
			continue
		}
		ok := true
		for _, n := range names {
			if !strings.Contains(ev.Text, n) {
				ok = false
				break
			}
		}
		if ok {
			return ev
		}
	}
	t.Fatalf("no reveal-as-cost note naming %v; cost notes seen: %v", names, costNotes(e))
	return events.Event{}
}

// costNotes returns every "as a cost" note text, for failure messages.
func costNotes(e *Engine) []string {
	var out []string
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "as a cost") {
			out = append(out, ev.Text)
		}
	}
	return out
}

// addLibraryCard puts `text` on top of p's library and returns its id.
func addLibraryCard(t *testing.T, e *Engine, p state.PlayerID, text string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, text), p)
	o.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, p, append([]state.ObjID{o.ID}, e.G.Zone(state.ZLibrary, p)...))
	return o.ID
}

// handCardNames returns the printed names of p's hand, in zone order.
func handCardNames(e *Engine, p state.PlayerID) []string {
	var out []string
	for _, id := range e.G.Zone(state.ZHand, p) {
		out = append(out, e.G.Obj(id).Face().Name)
	}
	return out
}

// altCastOptions returns the alternative-cost cast options legalActions
// offered for id (AltCostIndex > 0; a paid cast carries the zero value).
func landGrantAltOptions(opts []decision.Option, id state.ObjID) []decision.Option {
	var out []decision.Option
	for _, o := range opts {
		if o.Kind == "cast" && o.Obj == id && o.AltCostIndex > 0 {
			out = append(out, o)
		}
	}
	return out
}

// hasPaidCastOption reports whether the paid (zero-alt) cast of id is offered.
func hasPaidCastOption(opts []decision.Option, id state.ObjID) bool {
	for _, o := range opts {
		if o.Kind == "cast" && o.Obj == id && o.AltCostIndex == 0 && o.Mode == "" {
			return true
		}
	}
	return false
}

// forestInHand reports whether the given object id sits in p's hand.
func forestInHand(e *Engine, p state.PlayerID, forest state.ObjID) bool {
	for _, id := range e.G.Zone(state.ZHand, p) {
		if id == forest {
			return true
		}
	}
	return false
}

// resolveSearch answers the single-card library search the resolving spell
// poses (the pending choose); the answer resumes the resolution to completion.
func resolveSearch(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no library-search choose pending: %+v", d)
	}
	submitChoices(t, e, 0)
}

// TestLandGrantRevealHandAltCostCastsForFreeWithALandFreeHand: the brief's
// recorded shape (Generous Ent, Balustrade Spy, no land cards). The
// alternative is offered, pays no mana, reveals the whole hand as a public
// cost, and the spell resolves (the Forest is searched into hand).
func TestLandGrantRevealHandAltCostCastsForFreeWithALandFreeHand(t *testing.T) {
	t.Parallel()
	lg := corpusAlternativeCard(t, "Land Grant")
	ent := corpusAlternativeCard(t, "Generous Ent")
	spy := corpusAlternativeCard(t, "Balustrade Spy")
	e := handEngine(t, lg, ent, spy)
	grantID := e.G.Zone(state.ZHand, 0)[0]
	forest := addLibraryCard(t, e, 0, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")

	// Precondition: the hand really is land-free, so Land Grant's
	// CheckSVar$/SVarCompare$ gate holds.
	for _, n := range handCardNames(e, 0) {
		l := strings.ToLower(n)
		if strings.Contains(l, "forest") || strings.Contains(l, "mountain") || strings.Contains(l, "island") ||
			strings.Contains(l, "swamp") || strings.Contains(l, "plains") {
			t.Fatalf("precondition: hand holds a land card: %v", handCardNames(e, 0))
		}
	}
	// Precondition: the paid {1}{G} cast is not on the table from an empty
	// pool, so the option under test can only be the alternative.
	if hasPaidCastOption(e.legalActions(0), grantID) {
		t.Fatal("precondition: the paid {1}{G} cast is offered from an empty pool")
	}
	alts := landGrantAltOptions(e.legalActions(0), grantID)
	if len(alts) != 1 {
		t.Fatalf("Land Grant reveal-hand alternative not offered with a land-free hand: options=%+v", e.legalActions(0))
	}
	e.beginCast(0, alts[0])

	// The reveal paid the cost: the OTHER hand cards are named publicly, no
	// mana left the (empty) pool, and the spell sits on the stack. The spell
	// itself is on the stack, not the hand, so it is not in the reveal.
	revealNote(t, e, "Generous Ent", "Balustrade Spy")
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("the reveal-hand alternative paid mana: pool total %d", got)
	}
	if got := e.G.Obj(grantID).Zone; got != state.ZStack {
		t.Fatalf("Land Grant sits in %s after the reveal-hand cast, want stack", got)
	}
	// The spell resolves and searches the Forest into its caster's hand.
	e.resolveTop()
	resolveSearch(t, e)
	if e.G.Obj(grantID).Zone != state.ZGraveyard {
		t.Fatalf("Land Grant resolved into %s, want graveyard", e.G.Obj(grantID).Zone)
	}
	if !forestInHand(e, 0, forest) {
		t.Fatalf("the searched Forest never reached the hand; hand=%v", handCardNames(e, 0))
	}
}

// TestLandGrantRevealHandAltCostWithheldByItsCheckSVarGate: the same
// alternative is gated on "no land cards in hand" (CheckSVar$ X /
// SVarCompare$ EQ0, X = Count$ValidHand Land.YouOwn). A Forest in hand
// withholds the alternative.
func TestLandGrantRevealHandAltCostWithheldByItsCheckSVarGate(t *testing.T) {
	t.Parallel()
	lg := corpusAlternativeCard(t, "Land Grant")
	forestCard := card(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")
	e := handEngine(t, lg, forestCard)
	grantID := e.G.Zone(state.ZHand, 0)[0]

	// Precondition: the hand really holds a land card.
	held := false
	for _, n := range handCardNames(e, 0) {
		if strings.EqualFold(n, "Forest") {
			held = true
		}
	}
	if !held {
		t.Fatalf("precondition: hand holds no Forest: %v", handCardNames(e, 0))
	}
	if alts := landGrantAltOptions(e.legalActions(0), grantID); len(alts) != 0 {
		t.Fatalf("reveal-hand alternative offered although the hand holds a land: %+v", alts)
	}
}

// TestLandGrantRevealHandAltCostWithAnEmptyHand: revealing an empty hand is a
// legal payment (CR 701.20a). Land Grant alone in hand: once cast, the spell
// is on the stack and the hand is empty; the alternative must still be
// offered and paid, with a loud public note that nothing was revealed.
func TestLandGrantRevealHandAltCostWithAnEmptyHand(t *testing.T) {
	t.Parallel()
	lg := corpusAlternativeCard(t, "Land Grant")
	e := handEngine(t, lg)
	grantID := e.G.Zone(state.ZHand, 0)[0]
	forest := addLibraryCard(t, e, 0, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")

	alts := landGrantAltOptions(e.legalActions(0), grantID)
	if len(alts) != 1 {
		t.Fatalf("reveal-hand alternative not offered for the card's own cast: options=%+v", e.legalActions(0))
	}
	e.beginCast(0, alts[0])
	// Precondition for the empty-hand assertion: the spell left the hand and
	// the hand is now empty, so the whole-hand reveal is the zero-card case.
	if ids := e.G.Zone(state.ZHand, 0); len(ids) != 0 {
		t.Fatalf("precondition: hand not empty after the cast: %v", ids)
	}
	revealNote(t, e, "empty hand")
	if got := e.G.Obj(grantID).Zone; got != state.ZStack {
		t.Fatalf("Land Grant sits in %s, want stack", got)
	}
	e.resolveTop()
	resolveSearch(t, e)
	if !forestInHand(e, 0, forest) {
		t.Fatalf("the searched Forest never reached the hand; hand=%v", handCardNames(e, 0))
	}
}

// TestSasayaRevealHandAbilityIsOfferedWithSevenLands: the ability carrier of
// Reveal<1/Hand>. Sasaya's flip ability is an activated ability whose ONLY
// cost is the whole-hand reveal; pre-fix the reveal was read as an unmatched
// card filter, so the cost was unpayable and the ability was never offered.
// The "seven or more land cards in hand" rider is ConditionCheckSVar$/
// ConditionSVarCompare$, a resolution-side gate -- the OFFER assertion here
// is about the cost being payable.
func TestSasayaRevealHandAbilityIsOfferedWithSevenLands(t *testing.T) {
	t.Parallel()
	sasaya := corpusAlternativeCard(t, "Sasaya, Orochi Ascendant")
	land := card(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n")
	e := handEngine(t)
	src := e.G.AddObject(sasaya, 0)
	src.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})
	var lands []state.ObjID
	for i := 0; i < 7; i++ {
		lo := e.G.AddObject(land, 0)
		lo.Zone = state.ZHand
		lands = append(lands, lo.ID)
	}
	e.G.SetZone(state.ZHand, 0, lands)

	// Precondition: the source really is on the battlefield and the hand
	// really holds seven land cards.
	if e.G.Obj(src.ID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: Sasaya is in %s, want battlefield", e.G.Obj(src.ID).Zone)
	}
	if n := len(e.G.Zone(state.ZHand, 0)); n != 7 {
		t.Fatalf("precondition: hand holds %d cards, want 7", n)
	}
	found := false
	for _, o := range e.legalActions(0) {
		if o.Obj == src.ID && (o.Kind == "ability" || o.Kind == "activate") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Sasaya's whole-hand-reveal ability not offered with a payable cost: %+v", e.legalActions(0))
	}
}

// TestRevealHandSelfAbilityPaysByRevealingItself: the Reveal<1/CARDNAME> cost
// shape (13 corpus carriers, e.g. the forecast cycle) is paid by revealing a
// card named CARDNAME -- for an ability activated from the hand, the source
// itself is that card: it still sits in the hand while its ability
// activates, so the self-reveal must include it. Pre-fix the candidate walk
// excluded the source unconditionally, so the only matching card could never
// pay and such abilities were never offered. (The corpus's forecast cards
// additionally need Forecast$ zone/timing support to be offered from the
// hand; that mechanic is outside this ticket -- the synthetic card carries
// ActivationZone$ Hand so the COST semantics are what is under test.)
func TestRevealHandSelfAbilityPaysByRevealingItself(t *testing.T) {
	t.Parallel()
	selfText := "Name:Hand Reveal Self\nManaCost:0\nTypes:Instant\n" +
		"A:AB$ Draw | Cost$ 1 Reveal<1/CARDNAME> | ActivationZone$ Hand | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n"
	self := card(t, selfText)
	e := handEngine(t, self)
	selfID := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 1
	nLibrary := len(e.G.Zone(state.ZLibrary, 0))

	// Precondition: the source really is a hand card whose printed name the
	// CARDNAME spec resolves to, so the self-reveal is the only candidate.
	if e.G.Obj(selfID).Zone != state.ZHand {
		t.Fatalf("precondition: source is in %s, want hand", e.G.Obj(selfID).Zone)
	}
	if e.G.Obj(selfID).Face().Name != "Hand Reveal Self" {
		t.Fatalf("precondition: source name %q", e.G.Obj(selfID).Face().Name)
	}

	// Drive priority and answer with the activation option.
	e.priorityRound()
	d2 := e.Pending()
	if d2 == nil || d2.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d2)
	}
	found := -1
	for _, o := range d2.Options {
		if o.Kind == "ability" && o.Obj == selfID {
			found = o.Index
		}
	}
	if found < 0 {
		t.Fatalf("self-reveal ability not offered at the priority decision: %+v", d2.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d2.Seq, Player: d2.Player, Choices: []int{found}}); err != nil {
		t.Fatalf("activate self-reveal ability: %v", err)
	}
	e.resolveTop()

	revealNote(t, e, "Hand Reveal Self")
	if got := e.G.Obj(selfID).Zone; got != state.ZHand {
		t.Fatalf("the ability's source left the hand into %s, want hand", got)
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != nLibrary-1 {
		t.Fatalf("the drawn card did not resolve: library has %d cards, want %d", got, nLibrary-1)
	}
}
