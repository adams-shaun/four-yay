package rules

// This file pins the DOTTED `AttachedTo <referent>` Defined selector on the
// four real corpus carriers the ticket names -- Murderous Spoils, Fumble,
// Rhuk, Hexgold Nabber and Cass, Hand of Vengeance -- plus the real
// events.Apply fold that backs the "were attached" half.
//
// Each test drives the card's OWN SVar body (resolveSourceFaceSA) with the
// binding the real resolution would carry (Ctx.Targets for the mid-chain
// sub-ability, Ctx.Remembered for a trigger's captured referent), asserts its
// own preconditions, and replays byte-identically at the end. The
// destination-picking carriers (Fumble, Cass) answer the ask the engine
// poses rather than substituting a resolvable selector.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// attachedRulesBoard builds the shared board: a seat-1 creature wearing a
// seat-0 Aura and a seat-0 Equipment, a second seat-0 creature as a
// destination, and a decoy graveyard Aura that was never attached to anyone.
// Returns the ids by key.
func attachedRulesBoard(t *testing.T, e *Engine) map[string]state.ObjID {
	t.Helper()
	bear := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	dest := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	// The attachments are SEAT 1's, so a control gain to seat 0 is observable
	// (an already-seat-0 permanent would make the gain assertion vacuous).
	aura := moveByName(t, e, 1, "Unholy Strength", state.ZBattlefield)
	equip := moveByName(t, e, 1, "Bonesplitter", state.ZBattlefield)
	decoy := moveByName(t, e, 0, "Unholy Strength", state.ZGraveyard)
	e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{bear}})
	e.emit(events.Event{Kind: events.Attach, Obj: equip, IDs: []state.ObjID{bear}})
	return map[string]state.ObjID{"bear": bear, "dest": dest, "aura": aura, "equip": equip, "decoy": decoy}
}

// TestMurderousSpoilsStealsEquipmentAttachedToDestroyedCreature drives
// Murderous Spoils' own `StealEquip` body. Its `Defined$ AttachedTo
// Targeted.Equipment` used to be unknown, so Defined fell back to the source
// (the spoils card) and effGainControl's battlefield-only loop skipped it:
// control of the Equipment was never gained.
func TestMurderousSpoilsStealsEquipmentAttachedToDestroyedCreature(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Murderous Spoils"), lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Unholy Strength")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Bonesplitter"), lookup(t, reg, "Unholy Strength")})
	spoils := moveByName(t, e, 0, "Murderous Spoils", state.ZGraveyard)
	ids := attachedRulesBoard(t, e)

	// Preconditions: the Equipment is attached to the seat-1 bear and is
	// controlled by seat 0 (its owner); the bear is a live object.
	if got := e.G.Obj(ids["equip"]).AttachedTo; got != ids["bear"] {
		t.Fatalf("precondition failed: Equipment AttachedTo = %d, want bear %d", got, ids["bear"])
	}
	if e.G.Obj(ids["equip"]).Controller != 1 {
		t.Fatalf("precondition failed: Equipment controller = %d, want seat 1 so a gain to seat 0 is observable", e.G.Obj(ids["equip"]).Controller)
	}
	if e.G.Obj(ids["bear"]) == nil {
		t.Fatalf("precondition failed: the bear object is not live")
	}

	// The Destroy leg moves the bear off the battlefield; the StealEquip
	// sub-ability then resolves BEFORE any SBA checkpoint, so the Equipment's
	// live AttachedTo still names the (now graveyard) bear.
	e.emit(events.Event{Kind: events.MoveZone, Obj: ids["bear"], From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(ids["equip"]).AttachedTo; got != ids["bear"] {
		t.Fatalf("precondition failed: mid-chain Equipment AttachedTo = %d, want the stale bear %d", got, ids["bear"])
	}

	sa := resolveSourceFaceSA(t, e, spoils, "StealEquip")
	if defined := sa.Params["Defined"]; defined != "AttachedTo Targeted.Equipment" {
		t.Fatalf("precondition failed: StealEquip Defined = %q, want the real selector", defined)
	}
	ctx := &effects.Ctx{Source: spoils, Controller: 0, Targets: []state.Target{{Obj: ids["bear"]}}}
	effects.Resolve(e, ctx, sa)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("StealEquip posed an unexpected ask: %+v", d)
	}

	// Control of the Equipment was gained by seat 0 (Unearther's real
	// `NewController$` default is the resolving controller), and the decoy
	// graveyard Aura was untouched.
	if e.G.Obj(ids["equip"]).Controller != 0 {
		t.Fatalf("Equipment controller = %d, want 0 (control gained)", e.G.Obj(ids["equip"]).Controller)
	}
	if got := e.G.Obj(ids["aura"]).Controller; got != 1 {
		t.Fatalf("the Aura controller = %d, want 1 (StealEquip names only Equipment)", got)
	}
	replayCheck(t, e, cfg)
}

// TestFumbleGainsBothAndAttachesThemToAnotherCreature drives Fumble's own
// GainControl + DBAttach bodies. The GainControl leg was a no-op (unknown
// selector -> source fallback), and the attach-back leg attached the Fumble
// card instead of the attachments.
func TestFumbleGainsBothAndAttachesThemToAnotherCreature(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Fumble"), lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Unholy Strength")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Bonesplitter"), lookup(t, reg, "Unholy Strength")})
	fumble := moveByName(t, e, 0, "Fumble", state.ZGraveyard)
	ids := attachedRulesBoard(t, e)

	// Preconditions: both attachments point at the bear, the bear and the
	// destination are on the battlefield, and the destination differs from
	// the bear.
	if e.G.Obj(ids["aura"]).AttachedTo != ids["bear"] || e.G.Obj(ids["equip"]).AttachedTo != ids["bear"] {
		t.Fatalf("precondition failed: aura.AttachedTo=%d equip.AttachedTo=%d, want bear %d",
			e.G.Obj(ids["aura"]).AttachedTo, e.G.Obj(ids["equip"]).AttachedTo, ids["bear"])
	}
	if e.G.Obj(ids["dest"]).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: destination zone=%s, want battlefield", e.G.Obj(ids["dest"]).Zone)
	}
	if ids["dest"] == ids["bear"] {
		t.Fatalf("precondition failed: destination and bear share id %d", ids["dest"])
	}

	// The ChangeZone leg returns the bear to hand; the sub-abilities resolve
	// before any SBA, so the attachments' live links still name the bear.
	e.emit(events.Event{Kind: events.MoveZone, Obj: ids["bear"], From: state.ZBattlefield, To: state.ZHand})

	gain := resolveSourceFaceSA(t, e, fumble, "GainControl")
	ctx := &effects.Ctx{Source: fumble, Controller: 0, Targets: []state.Target{{Obj: ids["bear"]}}}
	effects.Resolve(e, ctx, gain)
	if e.G.Obj(ids["aura"]).Controller != 0 || e.G.Obj(ids["equip"]).Controller != 0 {
		t.Fatalf("GainControl left aura=%d equip=%d, want both controlled by 0",
			e.G.Obj(ids["aura"]).Controller, e.G.Obj(ids["equip"]).Controller)
	}

	// The attach-back leg: `Object$ AttachedTo Targeted.Aura,Equipment |
	// Choices$ Creature` must attach BOTH resolved objects to the destination.
	// The engine already CHAINS GainControl -> DBAttach (Fumble's
	// `SubAbility$ DBAttach`), so resolving GainControl above has already run
	// the attach-back leg: there is exactly ONE legal destination creature, so
	// the `Choices$` pool auto-takes it and `attachAll` runs inline with no
	// ask. Do NOT re-resolve DBAttach here: a second pass would re-resolve the
	// selector, find only the still-attached object, and attach it -- masking
	// a first-object-only bug. The both-attached assertion below is the pin.
	dba := resolveSourceFaceSA(t, e, fumble, "DBAttach")
	if dba.Params["Object"] != "AttachedTo Targeted.Aura,Equipment" {
		t.Fatalf("precondition failed: DBAttach Object = %q, want the real plural selector", dba.Params["Object"])
	}
	// Answer any chained ask (on this single-destination board there should be
	// none -- the pool auto-takes -- but keep the drain so a future board that
	// poses one still drives it).
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		idx := -1
		for _, o := range d.Options {
			if o.Obj == ids["dest"] {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("destination ask %+v has no option naming the destination %d", d, ids["dest"])
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit destination: %v", err)
		}
	}
	// Both attachments now sit on the destination.
	if got := e.G.Obj(ids["aura"]).AttachedTo; got != ids["dest"] {
		t.Fatalf("Aura AttachedTo = %d, want the destination %d", got, ids["dest"])
	}
	if got := e.G.Obj(ids["equip"]).AttachedTo; got != ids["dest"] {
		t.Fatalf("Equipment AttachedTo = %d, want the destination %d", got, ids["dest"])
	}
	// And not on the Fumble card itself (the old source fallback).
	if e.G.Obj(ids["aura"]).AttachedTo == fumble || e.G.Obj(ids["equip"]).AttachedTo == fumble {
		t.Fatalf("an attachment self-attached to the Fumble source %d", fumble)
	}
	replayCheck(t, e, cfg)
}

// TestFumbleWithNoAttachmentsDoesNotAttachItself pins the bound-but-empty
// direction: when the referent creature bears NO Aura or Equipment, the
// `Object$ AttachedTo Targeted.Aura,Equipment` selector resolves to ZERO
// objects. The source fallback must NOT fire -- the Fumble instant must not
// attach itself to the destination, and the attach-back leg must do nothing.
func TestFumbleWithNoAttachmentsDoesNotAttachItself(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Fumble"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears")})
	fumble := moveByName(t, e, 0, "Fumble", state.ZGraveyard)
	bear := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	dest := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)

	// Precondition: the bear really bears nothing.
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.AttachedTo == bear {
			t.Fatalf("precondition failed: object %d is attached to the bear", o.ID)
		}
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZHand})
	gain := resolveSourceFaceSA(t, e, fumble, "GainControl")
	ctx := &effects.Ctx{Source: fumble, Controller: 0, Targets: []state.Target{{Obj: bear}}}
	effects.Resolve(e, ctx, gain)
	// Answer any chained ask (there should be none: no attachments to attach).
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	// The Fumble card is where it belongs (graveyard, its source) and is NOT
	// attached to the destination or the bear.
	if got := e.G.Obj(fumble).AttachedTo; got != 0 {
		t.Fatalf("the Fumble source self-attached: AttachedTo = %d, want 0", got)
	}
	if e.G.Obj(fumble).Zone == state.ZBattlefield {
		t.Fatalf("the Fumble source moved to the battlefield")
	}
	// The destination got nothing attached to it.
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.AttachedTo == dest {
			t.Fatalf("object %d was attached to the destination %d with zero resolved attachments", o.ID, dest)
		}
	}
	replayCheck(t, e, cfg)
}

// TestRhukAttachesWereAttachedEquipment drives BOTH of Rhuk's own trigger
// bodies: the Attacks half (`TriggeredAttackerLKICopy`) and the death half
// (`TriggeredCardLKICopy`), whose Equipment has already been detached by the
// CR 704.5n sweep -- the LastBearer fold.
func TestRhukAttachesWereAttachedEquipment(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Rhuk, Hexgold Nabber"), lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Bonesplitter"), lookup(t, reg, "Bonesplitter")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Bonesplitter")})
	rhuk := moveByName(t, e, 0, "Rhuk, Hexgold Nabber", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	equipA := moveByName(t, e, 0, "Bonesplitter", state.ZBattlefield)
	equipB := moveByName(t, e, 0, "Bonesplitter", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: equipA, IDs: []state.ObjID{bear}})

	// Preconditions: Rhuk and the bear are on the battlefield, the Equipment
	// really is attached to the bear, and Rhuk is not.
	if e.G.Obj(rhuk).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: rhuk zone=%s bear zone=%s, want both battlefield", e.G.Obj(rhuk).Zone, e.G.Obj(bear).Zone)
	}
	if e.G.Obj(equipA).AttachedTo != bear {
		t.Fatalf("precondition failed: Equipment AttachedTo = %d, want bear %d", e.G.Obj(equipA).AttachedTo, bear)
	}
	if e.G.Obj(equipA).AttachedTo == rhuk {
		t.Fatalf("precondition failed: Equipment is already on Rhuk")
	}

	// Attacks half: bind the attacking creature the way an Attacks trigger
	// does (Remembered).
	sa := resolveSourceFaceSA(t, e, rhuk, "TrigAttackAttach")
	if obj := sa.Params["Object"]; obj != "AttachedTo TriggeredAttackerLKICopy.Equipment" {
		t.Fatalf("precondition failed: TrigAttackAttach Object = %q", obj)
	}
	effects.Resolve(e, &effects.Ctx{Source: rhuk, Controller: 0, Remembered: []state.Target{{Obj: bear}}}, sa)
	if got := e.G.Obj(equipA).AttachedTo; got != rhuk {
		t.Fatalf("Attacks half: Equipment AttachedTo = %d, want Rhuk %d", got, rhuk)
	}

	// Death half: the bear dies; the CR 704.5n sweep detaches both Equipment
	// (the live link clears, LastBearer records the bear). The trigger then
	// resolves AFTER the sweep -- so only the were-attached read can find the
	// Equipment.
	e.emit(events.Event{Kind: events.Attach, Obj: equipA, IDs: []state.ObjID{bear}})
	e.emit(events.Event{Kind: events.Attach, Obj: equipB, IDs: []state.ObjID{bear}})
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.checkStateBased()
	if got := e.G.Obj(equipA).AttachedTo; got != 0 {
		t.Fatalf("precondition failed: post-sweep Equipment AttachedTo = %d, want 0", got)
	}
	if got := e.G.Obj(equipA).LastBearer; got != bear {
		t.Fatalf("precondition failed: post-sweep LastBearer = %d, want the dead bear %d", got, bear)
	}

	sa2 := resolveSourceFaceSA(t, e, rhuk, "TrigAttach")
	if obj := sa2.Params["Object"]; obj != "AttachedTo TriggeredCardLKICopy.Equipment" {
		t.Fatalf("precondition failed: TrigAttach Object = %q", obj)
	}
	effects.Resolve(e, &effects.Ctx{Source: rhuk, Controller: 0, Remembered: []state.Target{{Obj: bear}}}, sa2)
	if got := e.G.Obj(equipA).AttachedTo; got != rhuk {
		t.Fatalf("death half: first Equipment AttachedTo = %d, want Rhuk %d", got, rhuk)
	}
	if got := e.G.Obj(equipB).AttachedTo; got != rhuk {
		t.Fatalf("death half: second Equipment AttachedTo = %d, want Rhuk %d (all were-attached Equipment must attach)", got, rhuk)
	}
	replayCheck(t, e, cfg)
}

// TestCassHandOfVengeanceReturnsOnlyTheWereAttachedAuras drives Cass's own
// DBChangeZone body: `ChooseFromDefined$ AttachedTo TriggeredCardLKICopy.Aura`
// must narrow the graveyard offer to the Aura that WAS attached to the dead
// creature, and DBAttach must re-attach the were-attached Equipment.
func TestCassHandOfVengeanceReturnsOnlyTheWereAttachedAuras(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	// setup builds a fresh Cass board: a seat-1 creature wearing a seat-1 Aura
	// and Equipment, a second creature as the destination, and a decoy
	// graveyard Aura that was never attached to anyone. The creature then dies
	// and the CR 704.5 sweep detaches the Equipment (recording LastBearer) and
	// sweeps the attached Aura to the graveyard (recording LastBearer). Each
	// phase needs its own engine: the offer-filter phase leaves the engine's
	// resolution suspended, which would DEFER a later chained ask.
	setup := func() (*Engine, Config, state.ObjID, map[string]state.ObjID) {
		t.Helper()
		e, cfg := corpusEngineCfg(t, reg,
			[]*cards.Card{lookup(t, reg, "Cass, Hand of Vengeance"), lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Unholy Strength")},
			[]*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Bonesplitter"), lookup(t, reg, "Unholy Strength")})
		cass := moveByName(t, e, 0, "Cass, Hand of Vengeance", state.ZBattlefield)
		ids := attachedRulesBoard(t, e)
		if ids["decoy"] == ids["aura"] {
			t.Fatalf("precondition failed: decoy and attached Aura share id %d", ids["decoy"])
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: ids["bear"], From: state.ZBattlefield, To: state.ZGraveyard})
		e.checkStateBased()
		if e.G.Obj(ids["aura"]).Zone != state.ZGraveyard {
			t.Fatalf("precondition failed: the swept Aura zone = %s, want graveyard", e.G.Obj(ids["aura"]).Zone)
		}
		if got := e.G.Obj(ids["aura"]).LastBearer; got != ids["bear"] {
			t.Fatalf("precondition failed: swept Aura LastBearer = %d, want the dead creature %d", got, ids["bear"])
		}
		if got := e.G.Obj(ids["decoy"]).LastBearer; got != 0 {
			t.Fatalf("precondition failed: decoy Aura LastBearer = %d, want 0", got)
		}
		if e.G.Obj(ids["dest"]).Zone != state.ZBattlefield {
			t.Fatalf("precondition failed: destination zone = %s, want battlefield", e.G.Obj(ids["dest"]).Zone)
		}
		return e, cfg, cass, ids
	}

	// Phase A -- Cass's DBAttach leg, resolved as its own body with the
	// captured trigger referent. `Object$ AttachedTo
	// TriggeredCardLKICopy.Equipment | Optional$ True | Defined$ Targeted`
	// must attach the were-attached Equipment to the target the trigger
	// named -- not to Cass and not to the Object$-unknown source fallback.
	// This phase needs no hidden pick, so it isolates the Equipment bearer.
	eA, cfgA, cassA, idsA := setup()
	dba := resolveSourceFaceSA(t, eA, cassA, "DBAttach")
	if obj := dba.Params["Object"]; obj != "AttachedTo TriggeredCardLKICopy.Equipment" {
		t.Fatalf("precondition failed: DBAttach Object = %q, want the real Equipment selector", obj)
	}
	if dba.Params["Defined"] != "Targeted" {
		t.Fatalf("precondition failed: DBAttach Defined = %q, want Targeted", dba.Params["Defined"])
	}
	if dba.Params["Optional"] != "True" {
		t.Fatalf("precondition failed: DBAttach Optional = %q, want True", dba.Params["Optional"])
	}
	if got := eA.G.Obj(idsA["equip"]).AttachedTo; got != 0 {
		t.Fatalf("precondition failed: the were-attached Equipment AttachedTo = %d, want 0 (detached by the sweep)", got)
	}
	if got := eA.G.Obj(idsA["equip"]).LastBearer; got != idsA["bear"] {
		t.Fatalf("precondition failed: the were-attached Equipment LastBearer = %d, want the dead creature %d", got, idsA["bear"])
	}
	if eA.G.Obj(idsA["equip"]).AttachedTo == cassA {
		t.Fatalf("precondition failed: the Equipment is already on Cass")
	}
	effects.Resolve(eA, &effects.Ctx{Source: cassA, Controller: 0, Remembered: []state.Target{{Obj: idsA["bear"]}},
		Targets: []state.Target{{Obj: idsA["dest"]}}, AttachOpt: "yes"}, dba)
	if got := eA.G.Obj(idsA["equip"]).AttachedTo; got != idsA["dest"] {
		t.Fatalf("DBAttach: the were-attached Equipment AttachedTo = %d, want the target %d", got, idsA["dest"])
	}
	if eA.G.Obj(idsA["equip"]).AttachedTo == cassA {
		t.Fatalf("DBAttach attached the Equipment to Cass (%d) instead of the target %d", cassA, idsA["dest"])
	}
	if got := eA.G.Obj(idsA["equip"]).LastBearer; got != 0 {
		t.Fatalf("DBAttach left LastBearer = %d, want 0 after the re-attach", got)
	}
	replayCheck(t, eA, cfgA)

	// Phase B -- the offer filter. `ChooseFromDefined$ AttachedTo
	// TriggeredCardLKICopy.Aura` must offer the were-attached Aura and NOT the
	// decoy that was never attached to the dead creature.
	e, _, cass, ids := setup()
	sa := resolveSourceFaceSA(t, e, cass, "DBChangeZone")
	if cfd := sa.Params["ChooseFromDefined"]; cfd != "AttachedTo TriggeredCardLKICopy.Aura" {
		t.Fatalf("precondition failed: ChooseFromDefined = %q, want the real selector", cfd)
	}
	effects.Resolve(e, &effects.Ctx{Source: cass, Controller: 0, Remembered: []state.Target{{Obj: ids["bear"]}},
		Targets: []state.Target{{Obj: ids["dest"]}}}, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("the ChooseFromDefined pick was not posed: %+v", d)
	}
	auraOffered, decoyOffered := false, false
	for _, o := range d.Options {
		if o.Obj == ids["decoy"] {
			decoyOffered = true
		}
		if o.Obj == ids["aura"] {
			auraOffered = true
		}
	}
	if decoyOffered {
		t.Fatalf("the decoy Aura %d was offered by ChooseFromDefined", ids["decoy"])
	}
	if !auraOffered {
		t.Fatalf("the were-attached Aura %d was never offered by the hidden pick: %+v", ids["aura"], d.Options)
	}

	// Phase C -- the answered re-entry. The Aura returns attached to the
	// target, the decoy stays put, and the DBChangeZone chain walks on to the
	// real DBAttach body, which poses its Optional$ True yes/no election.
	e2, cfg2, cass2, ids2 := setup()
	sa2 := resolveSourceFaceSA(t, e2, cass2, "DBChangeZone")
	effects.Resolve(e2, &effects.Ctx{Source: cass2, Controller: 0, Remembered: []state.Target{{Obj: ids2["bear"]}},
		Targets:    []state.Target{{Obj: ids2["dest"]}},
		HiddenPick: []state.ObjID{ids2["aura"]}, HiddenPickDone: true}, sa2)
	if e2.G.Obj(ids2["aura"]).Zone != state.ZBattlefield {
		t.Fatalf("the returned Aura zone = %s, want battlefield", e2.G.Obj(ids2["aura"]).Zone)
	}
	if got := e2.G.Obj(ids2["aura"]).AttachedTo; got != ids2["dest"] {
		t.Fatalf("returned Aura AttachedTo = %d, want the target %d", got, ids2["dest"])
	}
	if e2.G.Obj(ids2["decoy"]).Zone != state.ZGraveyard {
		t.Fatalf("the decoy Aura left the graveyard (zone %s) -- only were-attached Auras may return", e2.G.Obj(ids2["decoy"]).Zone)
	}
	da := e2.Pending()
	if da == nil || da.Kind != decision.KChoose {
		t.Fatalf("DBAttach's Optional$ True election was not posed: %+v", da)
	}
	if len(da.Options) < 2 || da.Options[0].Kind != "yes" {
		t.Fatalf("DBAttach's election options are not the yes/no pair: %+v", da.Options)
	}
	replayCheck(t, e2, cfg2)
}

// TestAttachedToSelectorChooseFromDefinedFailsClosed pins item 2's fail-closed
// direction: an unresolvable ChooseFromDefined$ value must offer nothing and
// emit the loud Note, never fall back to the whole origin zone.
func TestAttachedToSelectorChooseFromDefinedFailsClosed(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Cass, Hand of Vengeance"), lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Unholy Strength")},
		[]*cards.Card{lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Bonesplitter"), lookup(t, reg, "Unholy Strength")})
	cass := moveByName(t, e, 0, "Cass, Hand of Vengeance", state.ZBattlefield)
	ids := attachedRulesBoard(t, e)
	real := resolveSourceFaceSA(t, e, cass, "DBChangeZone")
	bogus := *real
	bogus.Params = map[string]string{}
	for k, v := range real.Params {
		bogus.Params[k] = v
	}
	bogus.Params["ChooseFromDefined"] = "Bogus.Selector"

	before := len(e.L.Events)
	effects.Resolve(e, &effects.Ctx{Source: cass, Controller: 0, Remembered: []state.Target{{Obj: ids["bear"]}},
		Targets: []state.Target{{Obj: ids["dest"]}}}, &bogus)
	if o := e.G.Obj(ids["decoy"]); o.Zone != state.ZGraveyard {
		t.Fatalf("the decoy Aura moved (zone %s): an unresolvable ChooseFromDefined$ must offer nothing", o.Zone)
	}
	noted := false
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "ChooseFromDefined$") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("no ChooseFromDefined$ fail-closed Note was emitted")
	}
	replayCheck(t, e, cfg)
}

// TestAttachedToSelectorLastBearerFold pins the events.Apply fold itself on the real
// SBA path: a dying creature's Equipment detaches with LastBearer set, and an
// object that re-attaches clears it.
func TestAttachedToSelectorLastBearerFold(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{lookup(t, reg, "Bonesplitter"), lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Grizzly Bears")},
		[]*cards.Card{})
	equip := moveByName(t, e, 0, "Bonesplitter", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	other := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: equip, IDs: []state.ObjID{bear}})
	if e.G.Obj(equip).AttachedTo != bear || e.G.Obj(equip).LastBearer != 0 {
		t.Fatalf("precondition failed: attached Equipment AttachedTo=%d LastBearer=%d, want %d/0",
			e.G.Obj(equip).AttachedTo, e.G.Obj(equip).LastBearer, bear)
	}
	// The bearer dies: the SBA detaches the Equipment through the Unattached
	// fold, which records the former bearer.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.checkStateBased()
	if got := e.G.Obj(equip).AttachedTo; got != 0 {
		t.Fatalf("post-sweep AttachedTo = %d, want 0", got)
	}
	if got := e.G.Obj(equip).LastBearer; got != bear {
		t.Fatalf("post-sweep LastBearer = %d, want the dead bearer %d", got, bear)
	}
	// Re-attaching clears it.
	e.emit(events.Event{Kind: events.Attach, Obj: equip, IDs: []state.ObjID{other}})
	if got := e.G.Obj(equip).LastBearer; got != 0 {
		t.Fatalf("after re-attach LastBearer = %d, want 0", got)
	}
	if got := e.G.Obj(equip).AttachedTo; got != other {
		t.Fatalf("after re-attach AttachedTo = %d, want other %d", got, other)
	}
	replayCheck(t, e, cfg)
}
