// The LeaveBattlefield$ Exile promise and the sVars$ grant on a resolving
// Animate/Pump/ChangeZone body (effects/leavebattlefield.go), pinned end to
// end on real corpus cards: Whip of Erebos is the Animate flagship (the
// DB$ Animate | Defined$ Remembered | LeaveBattlefield$ Exile | sVars$
// WhipMustAttack | Duration$ Permanent | AtEOT$ Exile chain), Dreams of the
// Dead the Pump site, From the Catacombs the ChangeZone site. The other
// participants are freely-authored or real corpus cards moved through the
// ordinary helpers; no corpus .txt is inlined.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// whipEngine seeds Whip of Erebos on seat 0's battlefield and the named
// creature card in seat 0's graveyard, then drives Whip's {2}{B}{B}, {T}
// activation targeting that creature to resolution. Returns the engine, its
// replay Config, the Whip id and the reanimated creature id. Preconditions
// the caller's own assertions rely on (creature on the battlefield under
// seat 0, haste granted) are asserted HERE so a silently failed activation
// fails loudly and names itself.
func whipEngine(t *testing.T, reg *cards.Registry, creatureName string) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	whip := lookup(t, reg, "Whip of Erebos")
	creature := lookup(t, reg, creatureName)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{whip, creature}, []*cards.Card{})
	wid := moveByName(t, e, 0, "Whip of Erebos", state.ZBattlefield)
	cid := moveByName(t, e, 0, creatureName, state.ZGraveyard)

	addMana(t, e, 0, "BBBB")
	submitChoices(t, e, abilityOption(t, e, wid, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision after activating Whip of Erebos: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == cid {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("graveyard creature %d not offered as Whip's target: %+v", cid, d.Options)
	}
	submitChoices(t, e, idx)
	settleActivation(t, e)

	o := e.G.Obj(cid)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("reanimated creature zone = %+v, want battlefield", o)
	}
	if o.Controller != 0 {
		t.Fatalf("reanimated creature controller = %d, want seat 0", o.Controller)
	}
	if !e.HasKeyword(cid, "Haste") {
		t.Fatal("reanimated creature did not gain Haste from the chained DB$ Animate")
	}
	return e, cfg, wid, cid
}

// TestWhipOfErebosLeaveBattlefieldExilesOnDeath: the flagship arm. The
// reanimated creature's death is rewritten as an exile — it must NOT reach
// the graveyard — and the promise is consumed by the departure (ExileOnMoved
// sweep), so no grant or promise lingers on the exiled card.
func TestWhipOfErebosLeaveBattlefieldExilesOnDeath(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, _, cid := whipEngine(t, reg, "Grizzly Bears")

	// The sVars$ grant is real and read back through the GrantedSVar
	// machinery: the verbatim Forge grant under its own name and the nested
	// "SVar:MustAttack:True" declaration expanded under the marker name.
	if v, ok := e.GrantedSVar(cid, "WhipMustAttack"); !ok || v != "SVar:MustAttack:True" {
		t.Fatalf("GrantedSVar(WhipMustAttack) = %q, %v; want the verbatim grant", v, ok)
	}
	if v, ok := e.GrantedSVar(cid, "MustAttack"); !ok || v != "True" {
		t.Fatalf("GrantedSVar(MustAttack) = %q, %v; want the expanded nested marker", v, ok)
	}

	// The creature dies: the leave-the-battlefield promise rewrites the
	// move as an exile.
	e.emit(events.Event{Kind: events.MoveZone, Obj: cid,
		From: state.ZBattlefield, To: state.ZGraveyard})
	o := e.G.Obj(cid)
	if o == nil {
		t.Fatal("the animated creature vanished entirely")
	}
	if o.Zone != state.ZExile {
		t.Fatalf("the animated creature's death left it in %s, want exile (pre-fix behaviour: it dies to the graveyard)", o.Zone)
	}
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		if id == cid {
			t.Fatal("the animated creature reached the graveyard despite the LeaveBattlefield$ Exile promise")
		}
	}
	// The promise is consumed: the departed object carries neither the
	// sVars grant nor the animation's haste any more.
	if _, ok := e.GrantedSVar(cid, "MustAttack"); ok {
		t.Fatal("the sVars$ grant survived the creature's battlefield departure")
	}
	if e.HasKeyword(cid, "Haste") {
		t.Fatal("the haste grant survived the creature's battlefield departure")
	}
	replayCheck(t, e, cfg)
}

// TestWhipOfErebosLeaveBattlefieldExilesOnBounce: "instead of putting it
// anywhere else" — a bounce to hand is exiled too.
func TestWhipOfErebosLeaveBattlefieldExilesOnBounce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, _, cid := whipEngine(t, reg, "Grizzly Bears")

	e.emit(events.Event{Kind: events.MoveZone, Obj: cid,
		From: state.ZBattlefield, To: state.ZHand})
	if o := e.G.Obj(cid); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the animated creature bounced to hand ended in %v, want exile", o)
	}
	replayCheck(t, e, cfg)
}

// TestWhipOfErebosAtEOTExilesTheAnimatedCreature: the AtEOT$ Exile arm —
// "Exile it at the beginning of the next end step" — through the same chain,
// and the DBCleanup sub clears Whip's remembered set.
func TestWhipOfErebosAtEOTExilesTheAnimatedCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, wid, cid := whipEngine(t, reg, "Grizzly Bears")

	ateotDriveToStep(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 20)
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(cid); o == nil || o.Zone == state.ZBattlefield {
		t.Fatalf("the animated creature survived the end step (zone %+v), want it exiled", o)
	}
	if o := e.G.Obj(wid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Whip itself moved (zone %v), want it kept", o)
	}
	replayCheck(t, e, cfg)
}

// TestWhipOfErebosGrantsDoNotSurviveReentry is the same CR 400.7 regression
// on the Animate/sVars path: after the animated creature is exiled by the
// promise and returns to the battlefield, neither the animation's haste nor
// the granted sVars may re-apply.
func TestWhipOfErebosGrantsDoNotSurviveReentry(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, _, cid := whipEngine(t, reg, "Grizzly Bears")

	// PRECONDITION: haste and the sVars grant are live on the battlefield.
	if !e.HasKeyword(cid, "Haste") {
		t.Fatal("animated creature never gained Haste (precondition)")
	}
	if _, ok := e.GrantedSVar(cid, "MustAttack"); !ok {
		t.Fatal("animated creature never gained the sVars grant (precondition)")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: cid,
		From: state.ZBattlefield, To: state.ZHand})
	if o := e.G.Obj(cid); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the bounced creature ended in %+v, want exile (precondition)", o)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: cid,
		From: state.ZExile, To: state.ZBattlefield})
	if o := e.G.Obj(cid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("returned creature zone = %+v, want battlefield (precondition)", o)
	}
	if e.HasKeyword(cid, "Haste") {
		t.Fatal("the consumed Animate haste grant re-applied to the returned card (CR 400.7)")
	}
	if _, ok := e.GrantedSVar(cid, "MustAttack"); ok {
		t.Fatal("the consumed sVars grant re-applied to the returned card (CR 400.7)")
	}
	replayCheck(t, e, cfg)
}

// TestDreamsOfTheDeadLeaveBattlefieldExilesOnBounce: the Pump site — the
// rider rides the sub's `DB$ Pump | ... | LeaveBattlefield$ Exile | Defined$
// Targeted | Duration$ Permanent`, so the returned creature is exiled when
// it would leave the battlefield.
func TestDreamsOfTheDeadLeaveBattlefieldExilesOnBounce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	dreams := lookup(t, reg, "Dreams of the Dead")
	specter := lookup(t, reg, "Hypnotic Specter")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{dreams, specter}, []*cards.Card{})
	did := moveByName(t, e, 0, "Dreams of the Dead", state.ZBattlefield)
	sid := moveByName(t, e, 0, "Hypnotic Specter", state.ZGraveyard)

	addMana(t, e, 0, "UU")
	submitChoices(t, e, abilityOption(t, e, did, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision after activating Dreams of the Dead: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == sid {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("graveyard specter %d not offered: %+v", sid, d.Options)
	}
	submitChoices(t, e, idx)
	settleActivation(t, e)

	// PRECONDITION: the specter really returned under seat 0 and really
	// gained the Pump's Cumulative upkeep grant -- without the grant the
	// departure below would not involve the Pump rider at all.
	if o := e.G.Obj(sid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("returned specter zone = %+v, want battlefield (precondition)", o)
	}
	if !e.HasKeyword(sid, "Cumulative upkeep") {
		t.Fatal("returned specter did not gain Cumulative upkeep from the chained DB$ Pump (precondition)")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: sid,
		From: state.ZBattlefield, To: state.ZHand})
	if o := e.G.Obj(sid); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the pumped specter's bounce ended in %v, want exile", o)
	}
	replayCheck(t, e, cfg)
}

// TestFromTheCatacombsLeaveBattlefieldExilesOnBounce: the ChangeZone site —
// the rider rides the casting spell's own `SP$ ChangeZone | ...
// LeaveBattlefield$ Exile`, so the creature it returns is exiled when it
// would leave the battlefield.
func TestFromTheCatacombsLeaveBattlefieldExilesOnBounce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	catacombs := lookup(t, reg, "From the Catacombs")
	specter := lookup(t, reg, "Hypnotic Specter")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{catacombs, specter}, []*cards.Card{})
	sid := moveByName(t, e, 0, "Hypnotic Specter", state.ZGraveyard)
	moveByName(t, e, 0, "From the Catacombs", state.ZHand)

	addMana(t, e, 0, "BBBBB")
	cast := castByName(t, e, 0, "From the Catacombs")
	if cast == nil {
		t.Fatal("From the Catacombs cast option not offered")
	}
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision after casting From the Catacombs: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == sid {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("graveyard specter %d not offered: %+v", sid, d.Options)
	}
	submitChoices(t, e, idx)
	// From the Catacombs also takes the initiative. Its venture into
	// Undercity poses Secret Entrance's basic-land search while the room
	// ability resolves; answer it before asserting the ChangeZone rider.
	search := passUntilNonPriority(t, e, 40)
	if search == nil || search.Kind != decision.KChoose || search.ResumeKind != "search" || search.Player != 0 || len(search.Options) == 0 {
		t.Fatalf("expected Secret Entrance search for the caster, got %+v", search)
	}
	if !e.G.IsInitiative(0) {
		t.Fatal("From the Catacombs did not take the initiative before its room search")
	}
	submitChoices(t, e, search.Options[0].Index)
	resolveCast(t, e)

	// PRECONDITION: the specter really returned to the battlefield under
	// seat 0's control (GainControl$ True) -- otherwise the departure below
	// would not involve the ChangeZone rider at all.
	if o := e.G.Obj(sid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("returned specter zone = %+v, want battlefield (precondition)", o)
	}
	if o := e.G.Obj(sid); o.Controller != 0 {
		t.Fatalf("returned specter controller = %d, want seat 0 (GainControl$ True)", o.Controller)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: sid,
		From: state.ZBattlefield, To: state.ZHand})
	if o := e.G.Obj(sid); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the returned specter's bounce ended in %v, want exile", o)
	}
	replayCheck(t, e, cfg)
}

// TestDreamsOfTheDeadPumpGrantDoesNotSurviveReentry is the CR 400.7
// regression: a Duration$ Permanent Pump grant that rides a
// LeaveBattlefield$ Exile promise must end when the pumped object leaves the
// battlefield, or it re-arms on a later re-entry. Drives the full sequence --
// reanimate (grant), bounce (rewritten to exile), then return the card from
// exile to the battlefield -- and asserts the grant is gone.
func TestDreamsOfTheDeadPumpGrantDoesNotSurviveReentry(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	dreams := lookup(t, reg, "Dreams of the Dead")
	specter := lookup(t, reg, "Hypnotic Specter")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{dreams, specter}, []*cards.Card{})
	did := moveByName(t, e, 0, "Dreams of the Dead", state.ZBattlefield)
	sid := moveByName(t, e, 0, "Hypnotic Specter", state.ZGraveyard)

	addMana(t, e, 0, "UU")
	submitChoices(t, e, abilityOption(t, e, did, 0).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision after activating Dreams of the Dead: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == sid {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("graveyard specter %d not offered: %+v", sid, d.Options)
	}
	submitChoices(t, e, idx)
	settleActivation(t, e)

	// PRECONDITION: the specter is on the battlefield under seat 0 and the
	// Pump grant is live. Without this the re-entry assertion could pass
	// vacuously (a grant that never existed is trivially absent).
	if o := e.G.Obj(sid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("reanimated specter zone = %+v, want battlefield (precondition)", o)
	}
	if !e.HasKeyword(sid, "Cumulative upkeep") {
		t.Fatal("reanimated specter never gained Cumulative upkeep (precondition)")
	}

	// Bounce: the leave-exile promise rewrites the departure to exile.
	e.emit(events.Event{Kind: events.MoveZone, Obj: sid,
		From: state.ZBattlefield, To: state.ZHand})
	if o := e.G.Obj(sid); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the bounced specter ended in %+v, want exile (precondition)", o)
	}

	// Return the SAME card from exile to the battlefield. It is a new object
	// (CR 400.7): the consumed Pump grant must not re-apply.
	e.emit(events.Event{Kind: events.MoveZone, Obj: sid,
		From: state.ZExile, To: state.ZBattlefield})
	if o := e.G.Obj(sid); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("returned specter zone = %+v, want battlefield (precondition)", o)
	}
	if e.HasKeyword(sid, "Cumulative upkeep") {
		t.Fatal("the consumed LeaveBattlefield$ Exile Pump grant re-applied to the returned card (CR 400.7)")
	}
	replayCheck(t, e, cfg)
}
