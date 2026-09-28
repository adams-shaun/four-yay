package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Hellcat, Undying Vigilante's death trigger pins the review defect that
// registerStaticEffectGrant used to split one static body's layer-6 removal,
// keyword grant and ability grant into SEPARATE AddContinuous calls. Each call
// stamps its own ClockTick, so the removal (registered after the keyword
// grant) ran at a later timestamp and the layer walk's `kw = kw[:0]` wiped the
// keyword the grant had just appended. The card's own body is
//   Mode$ Continuous | Affected$ Card.IsRemembered | RemoveAllAbilities$ True | AddKeyword$ Haste
//
// so pre-fix the returned Hellcat derived keywords = [] (printed Haste removed
// and the re-grant wiped); post-fix the single combined effect applies the
// removal and the grant in the walk's in-effect order and Haste survives.
// Bronzehide/hellcat/harold are 3 of the 55 StaticEffect$ carriers with this
// shape; no repo/legacy deck carries any of them, so the golden heads cannot
// move.

// hellcatEngine deals seat 0 a hand holding Hellcat, Undying Vigilante; seat
// 1's deck is bears so nothing else interacts. All corpus cards.
func hellcatEngine(t *testing.T, reg *cards.Registry) (*Engine, Config) { //nolint:revive
	t.Helper()
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{searchCorpusCard(t, reg, "Hellcat, Undying Vigilante")}
	for len(deck) < 40 {
		deck = append(deck, searchCorpusCard(t, reg, "Forest"))
	}
	opp := make([]*cards.Card, 0, 40)
	for i := 0; i < 10; i++ {
		opp = append(opp, bear)
	}
	for len(opp) < 40 {
		opp = append(opp, searchCorpusCard(t, reg, "Forest"))
	}
	cfg := seatZeroStart(Config{Seed: 4407, Names: []string{"hellcat", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// hellcatOnBattlefield moves the Hellcat to seat 0's battlefield and returns
// its id.
func hellcatOnBattlefield(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == "Hellcat, Undying Vigilante" {
				if z != state.ZBattlefield {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatal("Hellcat, Undying Vigilante absent from hand/library")
	return 0
}

func TestHellcatReturnedKeepsTheGrantedHaste(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := hellcatEngine(t, reg)
	id := hellcatOnBattlefield(t, e)

	if !e.HasKeyword(id, "Haste") {
		t.Fatalf("pre-death printed keywords = %v, want Haste", e.Derived(id).Keywords)
	}

	// Lethal damage: the death trigger returns the card with a +1/+1 counter,
	// loses all abilities and gains haste.
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 5, Player: 0})
	e.checkStateBased()
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 20)

	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Hellcat zone = %+v, want battlefield", o)
	}
	if got := o.Counter("P1P1"); got != 1 {
		t.Fatalf("+1/+1 counters = %d, want 1", got)
	}
	// The combined layer-6 effect must leave the granted Haste in place. The
	// pre-fix split stamped the removal later than the grant, so the walk's
	// clear wiped it and this read [].
	if !e.HasKeyword(id, "Haste") {
		t.Fatalf("returned keywords = %v, want the granted Haste to survive the removal", e.Derived(id).Keywords)
	}
	replayCheck(t, e, cfg)
}

// Lim-Dûl, the Necromancer (see the effects-level
// TestStaticEffectAffectedIsRememberedSuffix) is the one StaticEffect$ carrier
// scoped with Affected$ Creature.IsRemembered. Its return rides a trigger with
// an optional {1}{B} payment and a target, so the rewrite is pinned at the
// registration boundary (the Affected$ value the layer walk evaluates) plus
// the effects-level matcher probe in staticeffect_test.go rather than through
// a full game here.
