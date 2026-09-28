package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kw:Squad (CR 702.66) pinned end to end on the REAL corpus carrier
// Securitron Squadron (K:Squad:3). The deck is built from compiled corpus
// cards only, so no Forge script text is committed. Securitron Squadron is
// in no legacy golden deck, so no chain head depends on this card.
//
// The helpers come from search_library_test.go (searchTestRegistry,
// searchCorpusCard, searchMoveByName), cast_test.go (addMana, submitChoices,
// replayCheck) and replacement_updated_test.go (passUntilStackEmpty) -- all
// the same package.

// squadEngine deals seat 0 a 40-card deck whose headline card is Securitron
// Squadron, then Plains and Grizzly Bears; the opponent's deck is all
// Mountains. The seed is advanced to start seat 0.
func squadEngine(t *testing.T) (*Engine, Config, *cards.Registry) {
	t.Helper()
	reg := searchTestRegistry(t)
	plains := searchCorpusCard(t, reg, "Plains")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{searchCorpusCard(t, reg, "Securitron Squadron")}
	for i := 0; i < 10; i++ {
		deck = append(deck, plains)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 7701, Names: []string{"squad", "opp"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg, reg
}

// squadOption returns the priority option casting id in the given mode.
func squadOption(t *testing.T, e *Engine, id state.ObjID, mode string) decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			return o
		}
	}
	t.Fatalf("no %q cast option for %d: %+v", mode, id, d.Options)
	return decision.Option{}
}

// chooseSquad submits the count decision's option whose Amount is want.
func chooseSquad(t *testing.T, e *Engine, want int) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected a KChoose squad-count decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "squad" && o.Amount == want {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no squad option for %d: %+v", want, d.Options)
}

// squadCastInfoAmount returns the Amount of the pay-time CastInfo naming the
// squadpaid flag for obj, and whether such an event exists.
func squadCastInfoAmount(e *Engine, obj state.ObjID) (int32, bool) {
	for _, ev := range e.L.Events {
		if ev.Kind != events.CastInfo || ev.Obj != obj {
			continue
		}
		for _, part := range splitCSV(ev.Counter) {
			if part == "squadpaid" {
				return ev.Amount, true
			}
		}
	}
	return 0, false
}

// squadCopies counts token copies of id on seat 0's battlefield: same
// printed name as the original, IsToken, and not the original itself.
func squadCopies(e *Engine, id state.ObjID) int {
	src := e.G.Obj(id)
	if src == nil || src.Face() == nil {
		return 0
	}
	name := src.Face().Name
	n := 0
	for _, cid := range e.G.Zone(state.ZBattlefield, 0) {
		if cid == id {
			continue
		}
		c := e.G.Obj(cid)
		if c != nil && c.Face() != nil && c.Face().Name == name && c.IsToken {
			n++
		}
	}
	return n
}

// TestSquadSecuritronSquadronPaidTwiceMintsTwoCopies is the brief's headline
// adapted to the corpus mechanic (CR 702.66: "you may pay {3} any number of
// times ... create that many tokens that are copies of it" -- the brief's
// tap-the-squad description does not match any corpus K:Squad line): the
// squadded cast option is offered, the count ask is answered twice, the
// pay-time CastInfo carries the squadpaid flag with Amount 2, and two token
// copies of the creature enter the battlefield.
func TestSquadSecuritronSquadronPaidTwiceMintsTwoCopies(t *testing.T) {
	t.Parallel()
	e, cfg, _ := squadEngine(t)
	hero := searchMoveByName(t, e, "Securitron Squadron", state.ZHand)
	// Base {1}{W} plus two {3} squad payments: 2 + 3 + 3 = 8 white.
	addMana(t, e, 0, "WWWWWWWW")

	// The plain and squadded cast options are both offered.
	squadOption(t, e, hero, "")
	opt := squadOption(t, e, hero, "squadded")
	submitChoices(t, e, opt.Index)

	// The count ask: ascending 0..max, here exactly 0..2.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 3 ||
		d.Options[0].Kind != "squad" || d.Options[0].Amount != 0 ||
		d.Options[2].Amount != 2 {
		t.Fatalf("squad count decision %+v", d)
	}
	chooseSquad(t, e, 2)
	passUntilStackEmpty(t, e, 40)

	o := e.G.Obj(hero)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("original in %s, want the battlefield", o.Zone)
	}
	if o.IsToken {
		t.Fatal("the original arrived as a token")
	}
	if n := squadCopies(e, hero); n != 2 {
		t.Fatalf("%d token copies on the battlefield, want 2", n)
	}
	amt, ok := squadCastInfoAmount(e, hero)
	if !ok {
		t.Fatal("the pay-time CastInfo carries no squadpaid flag")
	}
	if amt != 2 {
		t.Fatalf("squadpaid CastInfo Amount = %d, want 2", amt)
	}
	replayCheck(t, e, cfg)
}

// TestSquadSecuritronDeclinedIsAPlainCast: the count ask answered 0 declines
// -- the cast resolves exactly like the pre-existing plain cast: no copies,
// no squadpaid flag, no CastInfo from the squad path.
func TestSquadSecuritronDeclinedIsAPlainCast(t *testing.T) {
	t.Parallel()
	e, cfg, _ := squadEngine(t)
	hero := searchMoveByName(t, e, "Securitron Squadron", state.ZHand)
	addMana(t, e, 0, "WWWWWWWW")

	opt := squadOption(t, e, hero, "squadded")
	submitChoices(t, e, opt.Index)
	chooseSquad(t, e, 0)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(hero); o.Zone != state.ZBattlefield {
		t.Fatalf("original in %s, want the battlefield", o.Zone)
	}
	if n := squadCopies(e, hero); n != 0 {
		t.Fatalf("%d token copies on the battlefield, want 0", n)
	}
	if _, ok := squadCastInfoAmount(e, hero); ok {
		t.Fatal("a declined squad still stamped the squadpaid flag")
	}
	replayCheck(t, e, cfg)
}

// TestSquadSecuritronPlainCastUnchanged: the plain cast (never entering the
// squadded mode) mints no copies and stamps no squadpaid flag -- the
// byte-identical-plain-cast contract the replicate/multikicker modes keep.
func TestSquadSecuritronPlainCastUnchanged(t *testing.T) {
	t.Parallel()
	e, cfg, _ := squadEngine(t)
	hero := searchMoveByName(t, e, "Securitron Squadron", state.ZHand)
	addMana(t, e, 0, "WW")

	opt := squadOption(t, e, hero, "")
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(hero); o.Zone != state.ZBattlefield {
		t.Fatalf("original in %s, want the battlefield", o.Zone)
	}
	if n := squadCopies(e, hero); n != 0 {
		t.Fatalf("%d token copies on the battlefield, want 0", n)
	}
	if _, ok := squadCastInfoAmount(e, hero); ok {
		t.Fatal("a plain cast stamped the squadpaid flag")
	}
	replayCheck(t, e, cfg)
}

// TestSquadNonManaCostsParse: the two corpus K:Squad lines whose cost carries
// a non-mana component -- Thrill-Kill Disciple's "1 Discard<1/Card>" and
// Ruthless Radrat's "ExileFromGrave<4/Card/cards>" -- are modelled cost
// components (ParseCost's Discard/Exile parts), so squadCost accepts them and
// the ordinary offer gate (nonManaCastable) decides payability from the
// board: the discard needs a card in hand, the exile needs four cards in the
// graveyard. Withholding them here would drop a real squad cost the payment
// flow can settle; the fail-closed direction this guards is a cost ParseCost
// genuinely cannot model (an Unknown token), not a modelled one.
func TestSquadNonManaCostsParse(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	for _, name := range []string{"Thrill-Kill Disciple", "Ruthless Radrat", "Wasteland Raider"} {
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("missing corpus %s", name)
		}
		saw := false
		for _, f := range card.Faces {
			if _, ok := f.KeywordParam("Squad"); !ok {
				continue
			}
			saw = true
			if _, payable := squadCost(f); !payable {
				t.Fatalf("%s: squadCost withheld a modelled squad cost", name)
			}
		}
		if !saw {
			t.Fatalf("%s: no K:Squad line on any face", name)
		}
	}
}

// TestSquadThrillKillDisciplePaysTheDiscardEndToEnd: the non-mana squad
// carrier (K:Squad:1 Discard<1/Card>) is offered, the count ask is answered
// once, the discard cost part is then posed as a real choice and its card
// lands in the graveyard, and one token copy enters. This proves the squad
// cost composes with the ordinary Discard cost machinery (squadAsk folds the
// part into pc.cost, discardAsk settles it), not just plain mana.
func TestSquadThrillKillDisciplePaysTheDiscardEndToEnd(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{searchCorpusCard(t, reg, "Thrill-Kill Disciple")}
	for i := 0; i < 12; i++ {
		deck = append(deck, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 7712, Names: []string{"tkr", "opp"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	hero := searchMoveByName(t, e, "Thrill-Kill Disciple", state.ZHand)
	// Base {2}{R} plus one squad payment {1} plus the discarded card.
	addMana(t, e, 0, "RRRR")
	if len(e.G.Zone(state.ZHand, 0)) < 2 {
		t.Fatalf("need a second card in hand to discard, hand has %d", len(e.G.Zone(state.ZHand, 0)))
	}

	opt := squadOption(t, e, hero, "squadded")
	submitChoices(t, e, opt.Index)
	// The count ask: max is bounded by BOTH the pool and the number of
	// discardable cards, so answer 1 explicitly.
	chooseSquad(t, e, 1)
	// The discard cost part, now a real choice over the rest of the hand.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("expected the discard-cost ask, got %+v", d)
	}
	discarded := state.ObjID(0)
	for _, o := range d.Options {
		if o.Obj != hero {
			discarded = o.Obj
			submitChoices(t, e, o.Index)
			break
		}
	}
	if discarded == 0 {
		t.Fatalf("no discard option other than the card being cast: %+v", d.Options)
	}
	passUntilStackEmpty(t, e, 40)

	if o := e.G.Obj(hero); o.Zone != state.ZBattlefield {
		t.Fatalf("original in %s, want the battlefield", o.Zone)
	}
	if n := squadCopies(e, hero); n != 1 {
		t.Fatalf("%d token copies on the battlefield, want 1", n)
	}
	if o := e.G.Obj(discarded); o.Zone != state.ZGraveyard {
		t.Fatalf("discarded card ended in %s, want the graveyard", o.Zone)
	}
	if amt, ok := squadCastInfoAmount(e, hero); !ok || amt != 1 {
		t.Fatalf("squadpaid CastInfo = (%d,%v), want (1,true)", amt, ok)
	}
	replayCheck(t, e, cfg)
}

// TestSquadKeywordExpansionAddsTheETBTrigger: the compiled Securitron
// Squadron face carries the keyword expansion's ChangesZone self-entry
// trigger whose body copies the entering creature Count$SquadPaid times --
// the property that makes kw:Squad real rather than an inert keyword line.
func TestSquadKeywordExpansionAddsTheETBTrigger(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	card, ok := reg.Lookup("Securitron Squadron")
	if !ok {
		t.Fatal("missing corpus Securitron Squadron")
	}
	found := false
	for _, f := range card.Faces {
		for _, tr := range f.Triggers {
			if tr.Mode != "ChangesZone" || tr.Effect == nil {
				continue
			}
			if tr.Effect.API != "CopyPermanent" {
				continue
			}
			if tr.Effect.Params["NumCopies"] == "Count$SquadPaid" &&
				tr.Effect.Params["Defined"] == "Self" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("Securitron Squadron has no Count$SquadPaid CopyPermanent entry trigger")
	}
	prims := map[string]bool{}
	for _, p := range card.Primitives() {
		prims[p] = true
	}
	if !prims["kw:Squad"] {
		t.Fatal("Securitron Squadron's primitive set omits kw:Squad")
	}
}
