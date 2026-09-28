package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// partnerWithFaceTrigger returns the ETB trigger the kw:Partner with expansion
// synthesizes on a face, identified by its Keyword$ tag. A face that carries
// the keyword but no such trigger is the unexpanded state -- the defect this
// ticket fixes -- so the helper lets the test assert the precondition the
// whole test depends on rather than assuming it.
func partnerWithFaceTrigger(c *cards.Card) (cards.Trigger, bool) {
	for _, f := range c.Faces {
		for _, tr := range f.Triggers {
			if tr.Params["Keyword"] == "Partner with" {
				return tr, true
			}
		}
	}
	return cards.Trigger{}, false
}

// TestPartnerWithETBOffersNamedPartnerSearch is the named proof for the
// kw:Partner with expansion, over the real corpus carrier Kamber, the
// Plunderer (K:Partner with:Laurine, the Diversion). CR 702.128's partner-with
// keyword prints real rules text: "When this creature enters, target player
// may put Laurine into their hand from their library, then shuffle." No
// carrier's script prints that ability separately, so the expansion must
// synthesize it; without the expansion Kamber's face carries the keyword as a
// bare, behaviourless label.
//
// The test walks the whole ability:
//  1. PRECONDITION: the corpus Kamber face carries the synthesized
//     Partner-with trigger (fails loudly if the expansion regressed or the
//     corpus pin moved), and the named partner is actually in the library.
//  2. entering Kamber poses the trigger's target-player ask;
//  3. the search asks seat 0 and offers exactly the named partner, not some
//     other library card -- the Card.named<name> filter's whole point,
//     exercised on a name that carries a raw comma;
//  4. taking it moves Laurine to hand and shuffles exactly once (CR 701.23:
//     "then shuffle").
func TestPartnerWithETBOffersNamedPartnerSearch(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	kamberCard := searchCorpusCard(t, reg, "Kamber, the Plunderer")
	if _, ok := partnerWithFaceTrigger(kamberCard); !ok {
		t.Fatal("Kamber carries K:Partner with but no synthesized Partner-with ETB trigger (expansion missing)")
	}
	e, cfg := searchEngine(t, reg, "Kamber, the Plunderer", "Laurine, the Diversion")

	// The named partner must start in seat 0's library, or the search has
	// nothing to offer and every later assertion is vacuous.
	laurine := state.ObjID(0)
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == "Laurine, the Diversion" {
				laurine = id
			}
		}
	}
	if laurine == 0 || e.G.Obj(laurine).Zone != state.ZLibrary {
		t.Fatalf("fixture: Laurine is not in seat 0's library (%d)", laurine)
	}

	// Enter Kamber (a raw MoveZone fixture ETB): the expansion's trigger
	// queues and, at priority, asks its target player.
	kamber := searchMoveByName(t, e, "Kamber, the Plunderer", state.ZBattlefield)
	if o := e.G.Obj(kamber); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Kamber did not enter the battlefield: %+v", o)
	}
	targetAsk := passUntilNonPriority(t, e, 30)
	if targetAsk.Kind != decision.KTarget {
		t.Fatalf("Partner-with ETB did not ask its target player: %+v", targetAsk)
	}
	playerOpt := -1
	for _, o := range targetAsk.Options {
		if o.Kind == "player" && o.Player == 0 {
			playerOpt = o.Index
		}
	}
	if playerOpt < 0 {
		t.Fatalf("Partner-with target ask offers no player-0 option: %+v", targetAsk.Options)
	}
	start := len(e.L.Events)
	submitChoices(t, e, playerOpt)

	// Forge's confirmAction gate for the synthesized Optional$ True ChangeZone
	// runs before the fetch list: the targeted player first answers the
	// "perform this search?" confirmation, then the search resolves to a
	// KChoose offering the named partner only.
	confirmAsk := passUntilNonPriority(t, e, 30)
	if confirmAsk.Kind != decision.KChoose || confirmAsk.ResumeKind != "search_confirm" || confirmAsk.Player != 0 {
		t.Fatalf("Partner-with ETB did not pose its search confirmation: %+v", confirmAsk)
	}
	submitChoices(t, e, confirmAsk.Options[0].Index)

	// The search resolves to a KChoose for the targeted player, offering the
	// named partner only.
	searchAsk := passUntilNonPriority(t, e, 30)
	if searchAsk.Kind != decision.KChoose || searchAsk.ResumeKind != "search" {
		t.Fatalf("Partner-with ETB did not pose a search: %+v", searchAsk)
	}
	if searchAsk.Player != 0 {
		t.Fatalf("search decision player = %d, want the targeted seat 0", searchAsk.Player)
	}
	if searchAsk.Min != 0 {
		t.Fatalf("search Min = %d, want 0 (the CR 701.23 may-find)", searchAsk.Min)
	}
	offered := -1
	for _, o := range searchAsk.Options {
		if o.Obj == 0 {
			continue
		}
		if o.Obj != laurine {
			t.Fatalf("search offered %d (%q), want only the named partner %d",
				o.Obj, e.G.Obj(o.Obj).Face().Name, laurine)
		}
		offered = o.Index
	}
	if offered < 0 {
		t.Fatalf("search options carry no card option: %+v", searchAsk.Options)
	}
	submitChoices(t, e, offered)

	// CR 701.23: taking the card, then shuffle -- one shuffle, and the
	// partner is in hand.
	if got := e.G.Obj(laurine).Zone; got != state.ZHand {
		t.Fatalf("Laurine zone after search = %s, want hand", got)
	}
	shuffles := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if shuffles != 1 {
		t.Fatalf("seat-0 shuffles = %d, want exactly 1 (CR 701.23 \"then shuffle\")", shuffles)
	}
	replayCheck(t, e, cfg)
}

// TestPartnerWithTargetedOpponentAnswersTheSearch pins the CR 702.128
// chooser: the TARGET player answers the private search over THEIR OWN
// library, not the trigger controller. The expansion's SA carries both
// DefinedPlayer$ Targeted (whose library) and Chooser$ Targeted (whose seat
// the ask goes to); dropping the second made the controller read and answer
// the opponent's hidden library. Kamber's controller targets seat 1, whose
// library holds the named partner Laurine -- a library seat 0 has no legal
// look at, so the ask must arrive with Player 1 and offer only Laurine.
func TestPartnerWithTargetedOpponentAnswersTheSearch(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	kamberCard := searchCorpusCard(t, reg, "Kamber, the Plunderer")
	if _, ok := partnerWithFaceTrigger(kamberCard); !ok {
		t.Fatal("Kamber carries K:Partner with but no synthesized Partner-with ETB trigger (expansion missing)")
	}
	// Deck 0 is Kamber's controller; deck 1 opens with the named partner so
	// the searched library is the OPPONENT's, never seat 0's.
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck0 := []*cards.Card{kamberCard}
	for i := 0; i < 8; i++ {
		deck0 = append(deck0, forest, mountain)
	}
	for len(deck0) < 40 {
		deck0 = append(deck0, bear)
	}
	deck1 := []*cards.Card{searchCorpusCard(t, reg, "Laurine, the Diversion")}
	for len(deck1) < 40 {
		deck1 = append(deck1, mountain)
	}
	cfg := Config{Seed: 9203, Names: []string{"searcher", "opponent"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens, NameUniverse: reg.Cards}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	// PRECONDITION: the named partner is in seat 1's LIBRARY. The opening
	// draw may have pulled it to seat 1's hand; put it back first, or every
	// later assertion is vacuous (a search of an empty-named library would
	// offer nothing and "the opponent answered" could not be exercised).
	laurine := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZHand, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Laurine, the Diversion" {
			laurine = id
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
	}
	if laurine == 0 {
		for _, id := range e.G.Zone(state.ZLibrary, 1) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Laurine, the Diversion" {
				laurine = id
			}
		}
	}
	if lo := e.G.Obj(laurine); laurine == 0 || lo == nil || lo.Zone != state.ZLibrary || lo.Controller != 1 {
		t.Fatalf("fixture: Laurine is not in seat 1's library (%d)", laurine)
	}

	kamber := searchMoveByName(t, e, "Kamber, the Plunderer", state.ZBattlefield)
	if o := e.G.Obj(kamber); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Kamber did not enter the battlefield: %+v", o)
	}
	targetAsk := passUntilNonPriority(t, e, 30)
	if targetAsk.Kind != decision.KTarget {
		t.Fatalf("Partner-with ETB did not ask its target player: %+v", targetAsk)
	}
	// Kamber's controller targets the OPPONENT (seat 1).
	oppOpt := -1
	for _, o := range targetAsk.Options {
		if o.Kind == "player" && o.Player == 1 {
			oppOpt = o.Index
		}
	}
	if oppOpt < 0 {
		t.Fatalf("Partner-with target ask offers no seat-1 option: %+v", targetAsk.Options)
	}
	start := len(e.L.Events)
	submitChoices(t, e, oppOpt)

	// Forge's confirmAction gate for the synthesized Optional$ True ChangeZone
	// runs before the fetch list, and it is the TARGETED seat that answers it,
	// exactly as that seat answers the search below.
	confirmAsk := passUntilNonPriority(t, e, 30)
	if confirmAsk.Kind != decision.KChoose || confirmAsk.ResumeKind != "search_confirm" || confirmAsk.Player != 1 {
		t.Fatalf("Partner-with ETB did not pose its seat-1 search confirmation: %+v", confirmAsk)
	}
	submitChoices(t, e, confirmAsk.Options[0].Index)

	// The ask goes to the TARGETED seat (Player 1), and the offered card is
	// Laurine OUT OF SEAT 1's library -- not seat 0 reading seat 1's cards.
	searchAsk := passUntilNonPriority(t, e, 30)
	if searchAsk.Kind != decision.KChoose || searchAsk.ResumeKind != "search" {
		t.Fatalf("Partner-with ETB did not pose a search: %+v", searchAsk)
	}
	if searchAsk.Player != 1 {
		t.Fatalf("search decision player = %d, want the targeted seat 1 (CR 702.128's target player answers)", searchAsk.Player)
	}
	if searchAsk.Min != 0 {
		t.Fatalf("search Min = %d, want 0 (the may-find)", searchAsk.Min)
	}
	offered := -1
	for _, o := range searchAsk.Options {
		if o.Obj == 0 {
			continue
		}
		oo := e.G.Obj(o.Obj)
		if oo == nil || oo.Face() == nil || oo.Face().Name != "Laurine, the Diversion" {
			t.Fatalf("search offered a card other than the named partner: %+v", o)
		}
		if oo.Controller != 1 {
			t.Fatalf("search offered %d owned by seat %d, want a seat-1 library card", o.Obj, oo.Controller)
		}
		offered = o.Index
	}
	if offered < 0 {
		t.Fatalf("search options carry no card option: %+v", searchAsk.Options)
	}
	submitChoices(t, e, offered)

	// The partner moved to the TARGETED seat's hand, and the shuffle is the
	// searched library's: exactly one seat-1 shuffle, none for seat 0.
	if got := e.G.Obj(laurine); got.Zone != state.ZHand || got.Controller != 1 {
		t.Fatalf("Laurine after search = zone %s controller %d, want seat 1's hand", got.Zone, got.Controller)
	}
	shuffle1, shuffle0 := 0, 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind != events.Shuffle {
			continue
		}
		switch ev.Player {
		case 1:
			shuffle1++
		case 0:
			shuffle0++
		}
	}
	if shuffle1 != 1 || shuffle0 != 0 {
		t.Fatalf("shuffles = seat0 %d seat1 %d, want seat0 0 seat1 1 (the searched library shuffles)", shuffle0, shuffle1)
	}
	replayCheck(t, e, cfg)
}

// TestPartnerWithExpansionSearchesTheFullPartnerName is the class-level guard
// over all 52 K:Partner with carriers at the pin. The param is either
// "<full name>" or "<full name>:<short alias>" (Khorvath Brightflame:Khorvath,
// Bebop, Skull & Crossbones:Bebop), and the search must name the FULL printed
// card: the short form is only a deck-hint alias. The filter must also match
// names a naive comma/space split would tear -- a raw comma (Kamber, the
// Plunderer) and an ampersand (Bebop, Skull & Crossbones). A regression that
// searched the short alias or mis-split the name would fail every carrier row
// here, not just Kamber's proof leaf.
func TestPartnerWithExpansionSearchesTheFullPartnerName(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	g := state.NewGame([]string{"you", "them"})
	checked := 0
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			line := ""
			for _, k := range f.Keywords {
				if cards.KeywordHead(k) == "Partner with" {
					line = k
				}
			}
			if line == "" {
				continue
			}
			// The full name is the param up to the first colon (the short
			// alias, when present, follows it).
			full := strings.TrimSpace(strings.SplitN(line, ":", 3)[1])
			tr, ok := partnerWithFaceTrigger(c)
			if !ok {
				t.Errorf("%s carries %q but has no synthesized Partner-with trigger", c.Path, line)
				continue
			}
			if tr.Effect == nil {
				t.Errorf("%s: Partner-with trigger Execute$ did not resolve to an effect", c.Path)
				continue
			}
			want := "Card.named" + full
			if got := tr.Effect.Params["ChangeType"]; got != want {
				t.Errorf("%s: ChangeType$ = %q, want %q (the short alias must not be searched)", c.Path, got, want)
				continue
			}
			partner, ok := reg.Lookup(full)
			if !ok {
				t.Errorf("%s names partner %q absent from the corpus", c.Path, full)
				continue
			}
			id := g.AddObject(partner, 0).ID
			if !effects.MatchesSpecFrom(g, want, id, 0, 0) {
				t.Errorf("%s: filter %q does not match the real corpus card %q", c.Path, want, full)
				continue
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no K:Partner with carrier produced a verifiable search; the corpus pin may have moved")
	}
	t.Logf("verified %d K:Partner with carriers search their full named partner", checked)
}
