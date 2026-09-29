package rules

// api:Airbend (CR 701.65) end-to-end pins on real corpus cards (ticket
// tla-airbend-unimplemented). CR 701.65a: "Airbend [a permanent]" means exile
// it, and "for as long as it remains exiled, its owner may cast it by paying
// {2} rather than its mana cost."
//
// The exile half is pinned by rules/setaudit_tla_test.go's
// TestSetAudit_tla_AirbendingLesson_ExilesTarget on Airbending Lesson; this
// file pins the RECAST half (the owner casts the exiled card for {2}), the
// log-derived provenance the permission reads, the DB$ trigger shape
// (Aang, Airbending Master), the TargetMin$ 0 zero-target contract
// (Airbender Ascension), and the measured census of the corpus carriers the
// registration now serves.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// airbendRecastOption returns the airbend recast cast option for id in opts,
// or nil.
func airbendRecastOption(opts []decision.Option, id state.ObjID) *decision.Option {
	for i := range opts {
		o := opts[i]
		if o.Kind == "cast" && o.Mode == "airbend_cast" && o.Obj == id {
			return &opts[i]
		}
	}
	return nil
}

// airbendSettle drains the stack, answering only the airbend target ask for
// the named object (id 0 asserts NO target ask is ever posed) and passing
// every priority round, until the stack and the trigger queue are empty.
func airbendSettle(t *testing.T, e *Engine, target state.ObjID, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
				return
			}
			e.priorityRound()
			continue
		}
		switch d.Kind {
		case decision.KTarget:
			if target == 0 {
				t.Fatalf("TargetMin$ 0 airbend posed a target ask: %+v", d.Options)
			}
			picked := false
			for _, o := range d.Options {
				if o.Obj == target {
					submitChoices(t, e, o.Index)
					picked = true
					break
				}
			}
			if !picked {
				t.Fatalf("airbend target ask did not offer object %d: %+v", target, d.Options)
			}
		case decision.KPriority:
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
				return
			}
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("priority ask without a pass option: %+v", d.Options)
			}
			submitChoices(t, e, pass)
		default:
			t.Fatalf("unexpected %v decision while settling an airbend: %+v", d.Kind, d)
		}
	}
	t.Fatal("airbend settle did not drain within the limit")
}

// TestAirbendRecastCastsExiledCardForTwo pins CR 701.65a's second half: the
// airbent card's owner may cast it from exile "by paying {2} rather than its
// mana cost", for as long as it remains exiled. Grizzly Bears ({1}{G}{G}) is
// airbent by its own owner's Airbending Lesson and then cast for exactly two
// generic: the pool holds ONLY {C}{C} and not a single green mana, so the
// printed cost is unpayable and the {2} alternative is the only way the cast
// can exist. The permission dies with the exile: after the recast resolves,
// neither the predicate nor the offer exists any more.
func TestAirbendRecastCastsExiledCardForTwo(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Airbending Lesson")
	bear := onBoardCard(t, e, 0, tlaCorpusCard(t, reg, "Grizzly Bears"))
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Grizzly Bears must start on seat 0's battlefield: %+v", o)
	}
	// A battlefield card is not exiled, so the recast offer cannot exist yet.
	if airbendRecastOption(e.Pending().Options, bear) != nil {
		t.Fatal("airbend recast offered while the Bear is still on the battlefield")
	}
	if _, ok := tlaCastByName(t, e, "Airbending Lesson", "WWW"); !ok {
		t.Fatalf("Airbending Lesson not castable for {W}{W}{W}")
	}
	// The spell's pre-ask names the Bear as the airbend target.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		picked := false
		for _, o := range d.Options {
			if o.Obj == bear {
				submitChoices(t, e, o.Index)
				picked = true
				break
			}
		}
		if !picked {
			t.Fatalf("airbend target ask did not offer the Bear: %+v", d.Options)
		}
	}
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: airbent Bear zone = %+v, want ZExile (CR 701.65a)", o)
	}
	if !e.airbendCastAvailable(bear) {
		t.Fatal("airbendCastAvailable denied the freshly airbent Bear")
	}
	// Fund EXACTLY {C}{C}: no green in the pool, so the printed {1}{G}{G}
	// cannot pay and the recast must ride its {2} alternative.
	p := &e.G.Players[0]
	for c := range p.Pool {
		p.Pool[c] = 0
	}
	p.Pool[state.MC] = 2
	e.priorityRound()
	opt := airbendRecastOption(e.Pending().Options, bear)
	if opt == nil {
		t.Fatalf("no airbend recast offer for the exiled Bear: %+v", e.Pending().Options)
	}
	if opt.Label != "Cast Grizzly Bears (airbent)" {
		t.Fatalf("airbend recast label = %q, want %q", opt.Label, "Cast Grizzly Bears (airbent)")
	}
	submitChoices(t, e, opt.Index)
	finishCast(t, e, bear)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("recast Bear zone = %+v, want ZBattlefield", o)
	}
	if got := e.G.Players[0].Pool[state.MC]; got != 0 {
		t.Fatalf("recast left %d generic in the pool, want 0 (the {2} must be paid)", got)
	}
	if e.airbendCastAvailable(bear) {
		t.Fatal("airbend permission survived the recast (the card no longer remains exiled)")
	}
	if airbendRecastOption(e.Pending().Options, bear) != nil {
		t.Fatal("airbend recast still offered after the card returned")
	}
}

// TestAirbendPermissionIsLogDerived pins the provenance contract the offer
// and the charge both read: the permission comes from the MoveZone that
// brought the card INTO exile carrying effects.AirbendExileCounter, and it
// lasts exactly as long as that marker-carrying move is the card's most
// recent move. An ordinary exile grants nothing, and any later move out of
// exile ends the permission ("for as long as it remains exiled").
func TestAirbendPermissionIsLogDerived(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Airbending Lesson")
	lesson := searchMoveByName(t, e, "Airbending Lesson", state.ZHand)
	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZHand)
	if o := e.G.Obj(lesson); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Airbending Lesson in seat 0's hand: %+v", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Grizzly Bears in seat 0's hand: %+v", o)
	}
	// An ordinary exile carries no marker: no permission.
	e.emit(events.Event{Kind: events.MoveZone, Obj: lesson, From: state.ZHand, To: state.ZExile})
	if e.airbendCastAvailable(lesson) {
		t.Fatal("an unmarked exile granted the airbend recast permission")
	}
	// The airbend exile's marker grants it.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZExile,
		Counter: effects.AirbendExileCounter})
	if !e.airbendCastAvailable(bear) {
		t.Fatal("the marker-carrying exile did not grant the airbend recast permission")
	}
	// A later move out of exile (any effect returning the card) ends it.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZExile, To: state.ZGraveyard})
	if e.airbendCastAvailable(bear) {
		t.Fatal("the airbend permission outlived the card's exile")
	}
	// The unmarked card's permission stays absent through everything.
	if e.airbendCastAvailable(lesson) {
		t.Fatal("an unmarked exiled card gained the permission")
	}
}

// TestAirbendMasterTriggerAirbendsAnotherCreature drives the DB$ trigger
// shape on its real carrier: Aang, Airbending Master's ETB ("airbend another
// target creature") must pose the ask, exile the chosen creature with the
// recast marker, and leave the trigger's own resolution clean.
func TestAirbendMasterTriggerAirbendsAnotherCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Aang, Airbending Master")
	bear := onBoardCard(t, e, 0, tlaCorpusCard(t, reg, "Grizzly Bears"))
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Grizzly Bears on the battlefield: %+v", o)
	}
	if _, ok := tlaCastByName(t, e, "Aang, Airbending Master", "WWWWW"); !ok {
		t.Fatalf("Aang, Airbending Master not castable for {4}{W}")
	}
	airbendSettle(t, e, bear, 80)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZExile {
		t.Fatalf("airbent Bear zone = %+v, want ZExile (CR 701.65a)", o)
	}
	if !e.airbendCastAvailable(bear) {
		t.Fatal("the trigger's airbend did not stamp the recast marker")
	}
	if hasNote(e, "unimplemented API Airbend") {
		t.Fatal("Aang's airbend trigger resolved as an unimplemented API")
	}
}

// TestAirbenderAscensionUpToOneTargetNeverAsks pins the TargetMin$ 0
// contract: Airbender Ascension's ETB airbends "up to one target creature",
// and with no creature on any battlefield the ability resolves without
// posing an ask and without a Note -- the registration is what ran.
func TestAirbenderAscensionUpToOneTargetNeverAsks(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Airbender Ascension")
	for _, z := range []state.Zone{state.ZBattlefield} {
		for _, p := range []state.PlayerID{0, 1} {
			for _, id := range e.G.Zone(z, p) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().IsCreature() {
					t.Fatalf("precondition: battlefield must have no creatures, found %q", o.Face().Name)
				}
			}
		}
	}
	if _, ok := tlaCastByName(t, e, "Airbender Ascension", "WW"); !ok {
		t.Fatalf("Airbender Ascension not castable for {1}{W}")
	}
	airbendSettle(t, e, 0, 80)
	if hasNote(e, "unimplemented API Airbend") {
		t.Fatal("Airbender Ascension's airbend resolved as an unimplemented API")
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("TargetMin$ 0 airbend posed a target ask with no valid targets: %+v", d.Options)
	}
}

// TestAirbendRecastFiresAppaCastFromExileTrigger checks the brief-named
// interaction: Appa, Steadfast Guardian's "Whenever you cast a spell from
// exile, create a 1/1 white Ally creature token" must see the airbend recast
// as a cast from exile (Card.wasCastFromExile reads the logged cast's origin
// zone, and the recast pushes the card from ZExile onto the stack).
func TestAirbendRecastFiresAppaCastFromExileTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := searchEngine(t, reg, "Appa, Steadfast Guardian", "Airbending Lesson")
	bear := onBoardCard(t, e, 0, tlaCorpusCard(t, reg, "Grizzly Bears"))
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Grizzly Bears on the battlefield: %+v", o)
	}
	if _, ok := tlaCastByName(t, e, "Airbending Lesson", "WWW"); !ok {
		t.Fatalf("Airbending Lesson not castable for {W}{W}{W}")
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		picked := false
		for _, o := range d.Options {
			if o.Obj == bear {
				submitChoices(t, e, o.Index)
				picked = true
				break
			}
		}
		if !picked {
			t.Fatalf("airbend target ask did not offer the Bear: %+v", d.Options)
		}
	}
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: airbent Bear zone = %+v, want ZExile", o)
	}
	// Appa enters; its own ETB airbend has no other nonland permanent left to
	// name (TargetMin$ 0), so the settle must be quiet.
	if _, ok := tlaCastByName(t, e, "Appa, Steadfast Guardian", "WWWW"); !ok {
		t.Fatalf("Appa, Steadfast Guardian not castable for {2}{W}{W}")
	}
	passUntilStackEmpty(t, e, 60)
	appa := -1
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Appa, Steadfast Guardian" {
			appa = 1
		}
	}
	if appa < 0 {
		t.Fatal("precondition: Appa must be on the battlefield before the recast")
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken {
			t.Fatal("precondition: no Ally token may exist before the recast")
		}
	}
	// The recast pays {2} from exile; Appa's SpellCast trigger answers it.
	p := &e.G.Players[0]
	for c := range p.Pool {
		p.Pool[c] = 0
	}
	p.Pool[state.MC] = 2
	e.priorityRound()
	opt := airbendRecastOption(e.Pending().Options, bear)
	if opt == nil {
		t.Fatalf("no airbend recast offer for the exiled Bear: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	finishCast(t, e, bear)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("recast Bear zone = %+v, want ZBattlefield", o)
	}
	tokens := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Ally Token" {
			tokens++
		}
	}
	if tokens != 1 {
		t.Fatalf("Appa's cast-from-exile trigger created %d 1/1 Ally tokens, want 1 (CR 701.65a recast is a cast from exile)", tokens)
	}
}

// TestAirbendCensusCarriersAreServed is the census: every card in the live
// corpus whose faces compile an api:Airbend must no longer report it
// unsupported. The measured count is pinned at 13 so a corpus pin move that
// adds a carrier fails here and forces a re-measure (the failure prints the
// fresh list).
func TestAirbendCensusCarriersAreServed(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	sup := effects.Supported()
	var carriers []string
	for _, c := range reg.Cards {
		has := false
		for _, p := range c.Primitives() {
			if p == "api:Airbend" {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		carriers = append(carriers, c.Faces[0].Name)
		for _, m := range reg.Unsupported(c, sup) {
			if m == "api:Airbend" {
				t.Errorf("%s still reports api:Airbend unsupported", c.Faces[0].Name)
			}
		}
	}
	if len(carriers) != 13 {
		t.Errorf("measured api:Airbend carrier count = %d, want 13; carriers = %v", len(carriers), carriers)
	}
}
