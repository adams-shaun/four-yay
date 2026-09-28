package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A Mode$ Continuous static's EffectZone$ scopes the zone its SOURCE sits in
// (Forge's StaticAbilityContinuous EffectZone$, default the battlefield).
// staticEffects (the layer walk) and collectActionStatics (the AddAbility$
// mana-grant membership) never consulted it: a graveyard-scoped static was
// collected only while its source was on the battlefield -- the one zone it
// must NOT apply in -- and never from the zone it named. These tests pin the
// read on the real corpus card Anger (S:Mode$ Continuous | Affected$
// Creature.YouCtrl | EffectZone$ Graveyard | AddKeyword$ Haste |
// IsPresent$ Mountain.YouCtrl), which is in no repo deck, so no chain head
// depends on it.
//
// The helpers come from search_library_test.go: the deck is built from
// compiled corpus cards only, so no Forge script text is committed here.

func effectZoneEngine(t *testing.T, reg *cards.Registry, withMountain bool) (*Engine, state.ObjID) {
	t.Helper()
	bearCard := searchCorpusCard(t, reg, "Grizzly Bears")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{searchCorpusCard(t, reg, "Anger"), mountain}
	for len(deck) < 40 {
		deck = append(deck, bearCard)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = bearCard
	}
	cfg := seatZeroStart(Config{Seed: 9204, Names: []string{"zone", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	// Seat 0's battlefield: a Grizzly Bears (the Affected$ Creature.YouCtrl
	// subject) and, when asked, a Mountain (Anger's IsPresent$ gate). The
	// opening deal leaves everything in hand; raw MoveZone emits place them.
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if withMountain {
		searchMoveByName(t, e, "Mountain", state.ZBattlefield)
	}
	return e, bear
}

// The corpus Anger in the graveyard with a Mountain on the battlefield: its
// graveyard-scoped haste grant reaches the bear through the layer walk.
func TestAngerGraveyardStaticGrantsHasteFromTheGraveyard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, bear := effectZoneEngine(t, reg, true)
	anger := searchMoveByName(t, e, "Anger", state.ZGraveyard)
	if o := e.G.Obj(anger); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Anger zone = %v, want graveyard", o)
	}
	d := e.Derived(bear)
	if !slices.Contains(d.Keywords, "Haste") {
		t.Fatalf("bear keywords = %v, want Haste from Anger's graveyard static", d.Keywords)
	}
}

// The static's IsPresent$ Mountain.YouCtrl gate: with no Mountain on the
// battlefield the same graveyard grant withholds.
func TestAngerGraveyardStaticWithholdsWithoutMountain(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, bear := effectZoneEngine(t, reg, false)
	anger := searchMoveByName(t, e, "Anger", state.ZGraveyard)
	if o := e.G.Obj(anger); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Anger zone = %v, want graveyard", o)
	}
	d := e.Derived(bear)
	if slices.Contains(d.Keywords, "Haste") {
		t.Fatalf("bear keywords = %v, want no Haste without Anger's IsPresent$ Mountain gate", d.Keywords)
	}
}

// The zone gate's deny side: while Anger itself is ON the battlefield, its
// EffectZone$ Graveyard static must NOT apply (the static belongs to the
// graveyard alone) -- a bear keeps no Haste grant, while Anger keeps only its
// own printed K:Haste.
func TestAngerBattlefieldPresenceDoesNotApplyTheGraveyardStatic(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, bear := effectZoneEngine(t, reg, true)
	anger := searchMoveByName(t, e, "Anger", state.ZBattlefield)
	if o := e.G.Obj(anger); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Anger zone = %v, want battlefield", o)
	}
	if d := e.Derived(bear); slices.Contains(d.Keywords, "Haste") {
		t.Fatalf("bear keywords = %v, want no Haste: a graveyard-scoped static does not apply from the battlefield", d.Keywords)
	}
	if !slices.Contains(e.Derived(anger).Keywords, "Haste") {
		t.Fatal("Anger lost its own printed K:Haste")
	}
}

// collectActionStatics' Continuous membership mirrors the same gate: the
// graveyard static is collected from the graveyard (the AddAbility$ mana-grant
// membership walk reaches non-battlefield sources) and denied from the
// battlefield.
func TestActionStaticsCollectContinuousFromTheGraveyardOnly(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := effectZoneEngine(t, reg, true)
	anger := searchMoveByName(t, e, "Anger", state.ZGraveyard)

	inGraveyard := false
	for _, sv := range e.collectActionStatics().continuous {
		if sv.Source == anger {
			if !effectZoneOK(sv.Params["EffectZone"], state.ZGraveyard) {
				t.Fatalf("collected graveyard static's EffectZone$ = %q, want Graveyard", sv.Params["EffectZone"])
			}
			inGraveyard = true
		}
	}
	if !inGraveyard {
		t.Fatal("Anger's graveyard-scoped Continuous static was not collected from the graveyard")
	}

	// Deny side: on the battlefield the static leaves the membership.
	e.emit(events.Event{Kind: events.MoveZone, Obj: anger, From: state.ZGraveyard, To: state.ZBattlefield})
	for _, sv := range e.collectActionStatics().continuous {
		if sv.Source == anger {
			t.Fatal("Anger's EffectZone$ Graveyard static was collected while Anger is on the battlefield")
		}
	}
}

// staticEffects' own membership: the layer memo carries the AddKeyword effect
// with the graveyard source, and drops it the moment the source leaves the
// named zone (the memo re-runs once per emitted event).
func TestStaticEffectsCollectGraveyardContinuousAndDropItOnLeave(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := effectZoneEngine(t, reg, true)
	anger := searchMoveByName(t, e, "Anger", state.ZGraveyard)

	found := false
	for _, ce := range e.staticEffects(e.staticContinuous) {
		if ce.Source == anger && len(ce.AddKeywords) > 0 && slices.Contains(ce.AddKeywords, "Haste") {
			found = true
		}
	}
	if !found {
		t.Fatal("staticEffects did not emit Anger's graveyard AddKeyword$ Haste")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: anger, From: state.ZGraveyard, To: state.ZExile})
	for _, ce := range e.staticEffects(e.staticContinuous) {
		if ce.Source == anger {
			t.Fatalf("staticEffects still emits the static from exile: %+v", ce)
		}
	}
}
