package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestExchangeControlRoleReversalCorpus(t *testing.T) {
	_, ability := corpusSA(t, "Role Reversal", "")
	if ability.API != "ExchangeControl" || ability.Params["TargetsWithSameCardType"] != "True" {
		t.Fatalf("Role Reversal fixture changed: %+v", ability)
	}
	h := newHost(t, 2)
	first := h.g.AddObject(mkCard(t, "Name:First\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	second := h.g.AddObject(mkCard(t, "Name:Second\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	for _, o := range []*state.Object{first, second} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	if h.g.Obj(first.ID).Zone != state.ZBattlefield || h.g.Obj(second.ID).Zone != state.ZBattlefield || h.g.Obj(first.ID).Controller == h.g.Obj(second.ID).Controller {
		t.Fatal("fixture requires two battlefield permanents with different controllers")
	}
	ctx := &Ctx{Controller: 0, Targets: []state.Target{{Obj: first.ID}, {Obj: second.ID}}}
	Resolve(h, ctx, ability)
	if h.g.Obj(first.ID).Controller != 1 || h.g.Obj(second.ID).Controller != 0 {
		t.Fatalf("Role Reversal controllers = %d,%d, want 1,0; events=%+v", h.g.Obj(first.ID).Controller, h.g.Obj(second.ID).Controller, h.log)
	}
	var changes []events.Event
	for _, ev := range h.log {
		if ev.Kind == events.ControlChange {
			changes = append(changes, ev)
		}
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ExchangeControl") {
			t.Fatalf("ExchangeControl handler was not dispatched: %+v", h.log)
		}
	}
	if len(changes) != 2 || changes[0].Obj != first.ID || changes[0].Player != 1 || changes[1].Obj != second.ID || changes[1].Player != 0 {
		t.Fatalf("control event order = %+v, want first->1 then second->0", changes)
	}
	if len(h.controls) != 2 || h.controls[0].Previous != 0 || h.controls[0].Controller != 1 || h.controls[1].Previous != 1 || h.controls[1].Controller != 0 {
		t.Fatalf("control grants did not use pre-exchange controller snapshot: %+v", h.controls)
	}
	// Replaying the emitted event stream from the identical pre-resolution
	// board must reproduce the same result.
	replay := newHost(t, 2)
	rFirst := replay.g.AddObject(mkCard(t, "Name:First\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	rSecond := replay.g.AddObject(mkCard(t, "Name:Second\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	for _, o := range []*state.Object{rFirst, rSecond} {
		replay.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	for _, ev := range h.log[2:] { // omit the two setup moves
		replay.Emit(ev)
	}
	if replay.g.Obj(rFirst.ID).Controller != 1 || replay.g.Obj(rSecond.ID).Controller != 0 {
		t.Fatalf("replayed controllers = %d,%d", replay.g.Obj(rFirst.ID).Controller, replay.g.Obj(rSecond.ID).Controller)
	}
}

func TestExchangeControlDefinedAndPickedTargetRemembered(t *testing.T) {
	h := newHost(t, 2)
	parent := h.g.AddObject(mkCard(t, "Name:Parent\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	picked := h.g.AddObject(mkCard(t, "Name:Picked\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	for _, o := range []*state.Object{parent, picked} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	sourceCard := mkCard(t, "Name:Source\nTypes:Creature\nPT:1/1\nOracle:x\n")
	sourceObj := h.g.AddObject(sourceCard, 0)
	h.Emit(events.Event{Kind: events.MoveZone, Obj: sourceObj.ID, From: state.ZLibrary, To: state.ZBattlefield})
	body := sa(t, "DB$ ExchangeControl | Defined$ ParentTarget | ValidTgts$ Creature | RememberExchanged$ True")
	// A `Defined$ ParentTarget` sub is a target-reuse shape, so the generic
	// mid-resolution pre-ask is suppressed and the sub's own side arrives in
	// the CAST-time chain record Ctx.SubPreAsk -- the real transport the
	// resolution attaches (rules/resolution.go). Ctx.PickedTargets stays nil
	// for this shape; feeding it instead would test a transport that never
	// carries the answer.
	ctx := &Ctx{Source: sourceObj.ID, Controller: 0, Targets: []state.Target{{Obj: parent.ID}},
		SubPreAsk: map[string][]state.Target{body.Line: {{Obj: picked.ID}}}}
	effExchangeControl(h, ctx, body)
	if h.g.Obj(parent.ID).Controller != 1 || h.g.Obj(picked.ID).Controller != 0 {
		t.Fatalf("ParentTarget/sub target controllers = %d,%d events=%+v", h.g.Obj(parent.ID).Controller, h.g.Obj(picked.ID).Controller, h.log)
	}
	if ctx.PickedTargets != nil {
		t.Fatalf("precondition: a targeted sub must leave PickedTargets to the resolution wrapper: %v", ctx.PickedTargets)
	}
	if len(ctx.Remembered) != 2 || ctx.Remembered[0].Obj != parent.ID || ctx.Remembered[1].Obj != picked.ID {
		t.Fatalf("SubAbility remembered input = %+v, want exchanged pair", ctx.Remembered)
	}
	if got := h.g.Obj(sourceObj.ID).Remembered; len(got) != 2 || got[0].Obj != parent.ID || got[1].Obj != picked.ID {
		t.Fatalf("persistent remembered = %v, want exchanged pair", got)
	}
}

// A `Defined$ Self` sub is NOT a target-reuse shape, so the generic
// mid-resolution pre-ask fires and delivers the sub's own side as
// Ctx.PickedTargets -- while Ctx.SubPreAsk keeps the same cast-time answer.
// The handler must consume the picked transport once, not append both: a
// double-count yields four targets and a silent no-op exchange.
func TestExchangeControlSelfSideConsumesPickedOnce(t *testing.T) {
	h := newHost(t, 2)
	sourceCard := mkCard(t, "Name:Source\nTypes:Artifact\nOracle:x\n")
	sourceObj := h.g.AddObject(sourceCard, 0)
	theirs := h.g.AddObject(mkCard(t, "Name:Theirs\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	for _, o := range []*state.Object{sourceObj, theirs} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	if h.g.Obj(sourceObj.ID).Controller == h.g.Obj(theirs.ID).Controller {
		t.Fatal("fixture requires source and target controlled by different players")
	}
	body := sa(t, "DB$ ExchangeControl | Defined$ Self | ValidTgts$ Creature.OppCtrl")
	pair := []state.Target{{Obj: theirs.ID}}
	ctx := &Ctx{Source: sourceObj.ID, Controller: 0, Targets: []state.Target{{Obj: theirs.ID}},
		PickedTargets: pair,
		SubPreAsk:     map[string][]state.Target{body.Line: pair}}
	effExchangeControl(h, ctx, body)
	if h.g.Obj(sourceObj.ID).Controller != 1 || h.g.Obj(theirs.ID).Controller != 0 {
		t.Fatalf("Defined$ Self exchange controllers = %d,%d, want 1,0; events=%+v",
			h.g.Obj(sourceObj.ID).Controller, h.g.Obj(theirs.ID).Controller, h.log)
	}
	changes := 0
	for _, ev := range h.log {
		if ev.Kind == events.ControlChange {
			changes++
		}
	}
	if changes != 2 {
		t.Fatalf("ControlChange count = %d, want 2 (a double-counted side no-ops)", changes)
	}
}

func TestExchangeControlGauntletsRememberedFollowUp(t *testing.T) {
	card, exchange := corpusSA(t, "Gauntlets of Chaos", "DBExchange")
	if exchange.API != "ExchangeControl" || exchange.Params["RememberExchanged"] != "True" ||
		exchange.Sub == nil || exchange.Sub.API != "DestroyAll" {
		t.Fatalf("Gauntlets of Chaos fixture changed: %+v", exchange)
	}
	h := newHost(t, 2)
	src := h.g.AddObject(card, 0)
	mine := h.g.AddObject(mkCard(t, "Name:Mine\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	aura := h.g.AddObject(mkCard(t, "Name:MyAura\nTypes:Enchantment\nKeywords:Aura\nOracle:x\n"), 0)
	theirs := h.g.AddObject(mkCard(t, "Name:Theirs\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	for _, o := range []*state.Object{src, mine, aura, theirs} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	h.Emit(events.Event{Kind: events.Attach, Obj: aura.ID, IDs: []state.ObjID{mine.ID}})
	if h.g.Obj(aura.ID).AttachedTo != mine.ID || h.g.Obj(mine.ID).Controller != 0 || h.g.Obj(theirs.ID).Controller != 1 {
		t.Fatal("fixture requires an Aura attached to my creature and an opponent-controlled permanent")
	}
	ctx := &Ctx{Source: src.ID, Controller: 0,
		Targets:   []state.Target{{Obj: mine.ID}},
		SubPreAsk: map[string][]state.Target{exchange.Line: {{Obj: theirs.ID}}}}
	effExchangeControl(h, ctx, exchange)
	if h.g.Obj(mine.ID).Controller != 1 || h.g.Obj(theirs.ID).Controller != 0 {
		t.Fatalf("Gauntlets controllers = %d,%d, want 1,0", h.g.Obj(mine.ID).Controller, h.g.Obj(theirs.ID).Controller)
	}
	// RememberExchanged$ is the chained SA's input: both exchanged permanents
	// in the walk set (DBDestroyAll's IsRemembered read) and on the source
	// (the persistent list an IsRemembered sub-filter matches). The
	// no-op paths above never write either.
	if len(ctx.Remembered) != 2 || ctx.Remembered[0].Obj != mine.ID || ctx.Remembered[1].Obj != theirs.ID {
		t.Fatalf("chain Remembered = %+v, want the exchanged pair", ctx.Remembered)
	}
	if got := h.g.Obj(src.ID).Remembered; len(got) != 2 || got[0].Obj != mine.ID || got[1].Obj != theirs.ID {
		t.Fatalf("persistent remembered = %v, want the exchanged pair", got)
	}
	// The chain walks the SubAbility with the SAME ctx: DBDestroyAll runs and
	// its DBCleanup clears the walk set it consumed. (DBDestroyAll's own
	// ValidCards$ shape `Aura.AttachedTo Card.IsRemembered` is an unrecognised
	// filter term -- attachedToArg allow-lists only the YouCtrl qualifier --
	// so the destroy itself matches nothing today; that filter gap is
	// reported separately and is not this primitive's contract.)
	Resolve(h, ctx, exchange.Sub)
	cleared := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API Cleanup") {
			t.Fatalf("chained sub was not dispatched: %+v", h.log)
		}
		if ev.Kind == events.Choose && ev.Counter == "clear-remembered" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("chained DBCleanup never cleared the remembered pair: %+v", h.log)
	}
}

func TestExchangeControlUnsupportedTargetingIsLoud(t *testing.T) {
	// Power Struggle's DB body needs TargetsAtRandom$ (Confusion in the
	// Ranks' needs TargetingPlayer$): the handler must refuse the exchange
	// loudly rather than swap an arbitrary pair silently.
	_, ability := corpusSA(t, "Power Struggle", "DBExchangeControl")
	if ability.Params["TargetsAtRandom"] != "True" {
		t.Fatalf("Power Struggle fixture changed: %+v", ability)
	}
	h := newHost(t, 2)
	src := h.g.AddObject(mkCard(t, "Name:Src\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	a := h.g.AddObject(mkCard(t, "Name:A\nTypes:Artifact\nOracle:x\n"), 0)
	b := h.g.AddObject(mkCard(t, "Name:B\nTypes:Creature\nPT:1/1\nOracle:x\n"), 1)
	for _, o := range []*state.Object{src, a, b} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	ctx := &Ctx{Source: src.ID, Controller: 0,
		Targets: []state.Target{{Obj: a.ID}}, PickedTargets: []state.Target{{Obj: b.ID}}}
	Resolve(h, ctx, ability)
	for _, ev := range h.log {
		if ev.Kind == events.ControlChange {
			t.Fatalf("random-target exchange silently changed control: %+v", h.log)
		}
		if ev.Kind == events.Note && strings.Contains(ev.Text, "TargetsAtRandom") {
			return
		}
	}
	t.Fatalf("no loud unimplemented-TargetsAtRandom note: %+v", h.log)
}

func TestExchangeControlNoOpBoundaries(t *testing.T) {
	_, ability := corpusSA(t, "Role Reversal", "")
	for _, tc := range []struct {
		name                     string
		controllerA, controllerB state.PlayerID
		missing                  bool
	}{
		{name: "same controller", controllerA: 0, controllerB: 0},
		{name: "departed side", controllerA: 0, controllerB: 1, missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t, 2)
			a := h.g.AddObject(mkCard(t, "Name:A\nTypes:Creature\nPT:1/1\nOracle:x\n"), tc.controllerA)
			b := h.g.AddObject(mkCard(t, "Name:B\nTypes:Creature\nPT:1/1\nOracle:x\n"), tc.controllerB)
			h.Emit(events.Event{Kind: events.MoveZone, Obj: a.ID, From: state.ZLibrary, To: state.ZBattlefield})
			if !tc.missing {
				h.Emit(events.Event{Kind: events.MoveZone, Obj: b.ID, From: state.ZLibrary, To: state.ZBattlefield})
			}
			beforeA, beforeB := h.g.Obj(a.ID).Controller, h.g.Obj(b.ID).Controller
			if !tc.missing && beforeA != beforeB {
				t.Fatal("same-controller no-op fixture has differing controllers")
			}
			Resolve(h, &Ctx{Controller: 0, Targets: []state.Target{{Obj: a.ID}, {Obj: b.ID}}}, ability)
			if h.g.Obj(a.ID).Controller != beforeA || h.g.Obj(b.ID).Controller != beforeB || len(h.controls) != 0 {
				t.Fatalf("invalid exchange partially changed control: %+v", h.log)
			}
			for _, ev := range h.log {
				if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ExchangeControl") {
					t.Fatalf("handler not dispatched: %+v", h.log)
				}
			}
		})
	}
}
