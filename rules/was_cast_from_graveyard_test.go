package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the graveyard-origin cast provenance end to end, both
// halves of the same read:
//
//   - the Count$wasCastFromGraveyard.<yes>.<no> branch head, whose YES/NO
//     values count Increasing Devotion's five hand-cast vs ten
//     flashback-cast Human tokens (the whole Increasing cycle shares the
//     shape), and
//   - the Card.wasCastFromGraveyard filter predicate, Ash Zealot's
//     "whenever a player casts a spell from a graveyard" ValidCard$.

// devotionSrc is Increasing Devotion's real script shape (cost, flashback
// cost, Token SA, X SVar) with a hand-authored TokenScript$ stem -- never
// the GPL-3.0 corpus's own .cards/tokenscripts.
const devotionSrc = "Name:Increasing Devotion\nManaCost:3 W W\nTypes:Sorcery\n" +
	"A:SP$ Token | TokenAmount$ X | TokenScript$ w_1_1_human | TokenOwner$ You | SpellDescription$ Create five 1/1 white Human creature tokens. If this spell was cast from a graveyard, create ten of those tokens instead.\n" +
	"SVar:X:Count$wasCastFromGraveyard.10.5\n" +
	"K:Flashback:7 W W\n" +
	"Oracle:Create five 1/1 white Human creature tokens. If this spell was cast from a graveyard, create ten of those tokens instead.\n"

// devotionTokenSrc backs TokenScript$ w_1_1_human.
const devotionTokenSrc = "Name:Human Token\nTypes:Token Creature Human\nPT:1/1\nOracle:x\n"

// devotionEngine is newFixtureDeck (replacement_updated_test.go) with
// Config.Tokens carrying the Human token fixture, authored here so a live
// Engine and the returned cfg share one map for replayCheck -- the
// newFixtureDeckWithTokens shape (sba_test.go), local to this file.
func devotionEngine(t *testing.T, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	fixture := card(t, devotionSrc)
	name := fixture.Faces[0].Name
	var cfg Config
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append([]*cards.Card{fixture}, mountainDeck(t, 39)...),
				mountainDeck(t, 40),
			},
			Tokens: map[string]*cards.Card{
				"w_1_1_human": card(t, devotionTokenSrc),
			},
		}
	}
	cfg = seatZeroStart(build(seed))
	e := New(cfg)
	e.Advance()

	var id state.ObjID
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == name {
			id = cand
		}
	}
	if id == 0 {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == name {
				id = cand
			}
		}
		if id == 0 {
			t.Fatalf("fixture %q not found in seat 0's hand or library", name)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
		e.pending = nil
		e.Advance()
	}
	return e, cfg, id
}

// tokenCount counts seat p's battlefield tokens.
func tokenCount(e *Engine, p state.PlayerID) int {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if e.G.Obj(id).IsToken {
			n++
		}
	}
	return n
}

// TestIncreasingDevotionResolvesCastOriginCounts pins the Count branch end
// to end: a hand cast resolves FIVE Human tokens (the NO branch), a
// flashback cast ({7}{W}{W}) resolves TEN (the YES branch, read off the
// spell's FlagFlashback CastInfo stamp), and both event streams replay
// byte-identically from the log alone.
func TestIncreasingDevotionResolvesCastOriginCounts(t *testing.T) {
	t.Parallel()
	// Hand cast: {3}{W}{W} -> the NO branch (5).
	e, cfg, dev := devotionEngine(t, 31)
	addMana(t, e, 0, "WWWWW")
	opts := castOptions(t, e)
	if len(opts) != 1 || opts[0].Obj != dev {
		t.Fatalf("hand cast not offered: %+v", opts)
	}
	submitChoices(t, e, opts[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := tokenCount(e, 0); got != 5 {
		t.Fatalf("hand cast created %d tokens, want 5", got)
	}
	if e.G.Obj(dev).Zone != state.ZGraveyard {
		t.Fatalf("sorcery went to %s, want graveyard", e.G.Obj(dev).Zone)
	}
	replayCheck(t, e, cfg)

	// Flashback cast: {7}{W}{W} from the graveyard -> the YES branch (10),
	// then exile (CR 702.32a).
	e2, cfg2, _ := devotionEngine(t, 32)
	dev2 := addToGraveyard(t, e2, 0, devotionSrc)
	addMana(t, e2, 0, "WWWWWWWWW")
	var fb *decision.Option
	for _, o := range castOptions(t, e2) {
		if o.Mode == "flashback" && o.Obj == dev2 {
			fb = &o
		}
	}
	if fb == nil {
		t.Fatalf("flashback not offered from the graveyard: %+v", castOptions(t, e2))
	}
	submitChoices(t, e2, fb.Index)
	passUntilStackEmpty(t, e2, 20)
	if got := tokenCount(e2, 0); got != 10 {
		t.Fatalf("flashback cast created %d tokens, want 10", got)
	}
	if e2.G.Obj(dev2).Zone != state.ZExile {
		t.Fatalf("flashback spell went to %s, want exile", e2.G.Obj(dev2).Zone)
	}
	replayCheck(t, e2, cfg2)
}

// zealotSrc is Ash Zealot's real script shape (cost, the SpellCast trigger
// and its DealDamage SVar verbatim), hand-authored -- never the GPL corpus
// .txt.
const zealotSrc = "Name:Ash Zealot\nManaCost:R R\nTypes:Creature Human Warrior\nPT:2/2\n" +
	"K:First Strike\nK:Haste\n" +
	"T:Mode$ SpellCast | ValidCard$ Card.wasCastFromGraveyard | Execute$ TrigDamage | TriggerZones$ Battlefield | TriggerDescription$ Whenever a player casts a spell from a graveyard, CARDNAME deals 3 damage to that player.\n" +
	"SVar:TrigDamage:DB$ DealDamage | Defined$ TriggeredCardController | NumDmg$ 3\n" +
	"Oracle:First strike, haste\n"

// riteFixtureSrc is a plain flashback sorcery: a hand cast and a flashback cast
// both gain 1 life, so the two casts' life deltas differ ONLY by the zealot
// trigger's 3 damage -- the clean discriminator for the predicate test.
const riteFixtureSrc = "Name:Rite\nManaCost:1 B\nTypes:Sorcery\n" +
	"A:SP$ GainLife | Defined$ You | LifeAmount$ 1\n" +
	"K:Flashback:2 B\nOracle:x\n"

// TestWasCastFromGraveyardFiresOnFlashbackOnly pins the filter predicate
// through the real SpellCast trigger path: a flashback-cast spell fires
// Ash Zealot's "whenever a player casts a spell from a graveyard" (3 damage
// to the caster), the same spell cast from the hand does not.
func TestWasCastFromGraveyardFiresOnFlashbackOnly(t *testing.T) {
	t.Parallel()
	e, cfg, rite := newFixtureDeck(t, 33, riteFixtureSrc, riteFixtureSrc, zealotSrc)
	putCreature(t, e, 0, zealotSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: rite, From: state.ZHand, To: state.ZGraveyard})
	e.Advance()
	addMana(t, e, 0, "BBB")
	lifeBefore := e.G.Players[0].Life

	// Flashback cast: the trigger fires for seat 0's own cast (3 damage),
	// and the rite itself gains 1 life.
	var fb *decision.Option
	for _, o := range castOptions(t, e) {
		if o.Mode == "flashback" && o.Obj == rite {
			fb = &o
		}
	}
	if fb == nil {
		t.Fatalf("flashback not offered from the graveyard: %+v", castOptions(t, e))
	}
	submitChoices(t, e, fb.Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != lifeBefore-3+1 {
		t.Fatalf("after flashback: life %d, want %d (one trigger hit, one gain)",
			got, lifeBefore-3+1)
	}

	// The same spell cast from the hand: no graveyard-origin bit, no trigger
	// -- only the rite's own life gain.
	rite2 := addToHand(t, e, 0, riteFixtureSrc)
	addMana(t, e, 0, "BBB")
	castObj(t, e, rite2)
	if got := e.G.Players[0].Life; got != lifeBefore-3+1+1 {
		t.Fatalf("after hand cast: life %d, want %d (the trigger must not fire for a hand cast)",
			got, lifeBefore-3+1+1)
	}
	replayCheck(t, e, cfg)
}
