package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effExchangeControl implements the two-permanent ExchangeControl form. The
// exchange is all-or-nothing: both battlefield objects and their controllers
// are captured before either control-change event is emitted.
func effExchangeControl(h Host, c *Ctx, sa *cards.SA) {
	// TargetsAtRandom$ (Power Struggle's DB body) is honoured at the target
	// ask (RandomTargetsAsk), so the pair arrives already drawn at random.
	// TargetingPlayer$ is deliberately NOT judged
	// here: it is a target-time CHOOSER redirect that rules/stack.go's
	// targetChooserCore already resolves at the ask site (trigger placement,
	// cast, activation and the mid-resolution pre-ask alike), so the effect
	// receives the chosen side through the ordinary target transport and must
	// not second-guess it. Confusion in the Ranks is exactly that shape
	// (`Defined$ TriggeredCard | TargetingPlayer$ TriggeredCardController`).

	g := h.Game()
	var targets []state.Target
	if DefinedRefOf(sa).Set() {
		// Defined and the body's target list identify opposite sides in
		// ParentTarget-plus-ValidTgts DB abilities. Resolve Defined without
		// Defined's ordinary ValidTgts shortcut, which intentionally prefers
		// the body's picked targets.
		definedSA := *sa
		definedSA.Params = make(map[string]string, len(sa.Params))
		for key, value := range sa.Params {
			definedSA.Params[key] = value
		}
		delete(definedSA.Params, "ValidTgts")
		targets = append(targets, Defined(h, c, &definedSA)...)
		// The sub's OWN side of the pair. A `Defined$ ParentTarget` sub with
		// its own ValidTgts$ is a target-REUSE shape: chosenTargetsFor's
		// definedIsTargetReuse guard suppresses the generic mid-resolution
		// pre-ask, so its answer never reaches Ctx.PickedTargets and lives
		// only in the cast/activation-time chain record Ctx.SubPreAsk
		// (collectSubTargetPreAsks keeps reuse subs; the answer is keyed by
		// the sub's Line). A `Defined$ Self`/`Defined$ TriggeredCard` sub is
		// NOT a reuse shape, so the generic pre-ask DOES fire and delivers
		// the same answer as Ctx.PickedTargets -- and leaves SubPreAsk
		// populated too, which is why PickedTargets (the consumed-once
		// transport) must win to avoid double-counting the side.
		if c.PickedTargets != nil {
			targets = append(targets, c.PickedTargets...)
		} else if ts, ok := c.SubPreAsk[sa.Line]; ok {
			targets = append(targets, ts...)
		} else if TargetsOf(sa).Has(TgtValidPresent) {
			targets = append(targets, c.Targets...)
		}
	} else {
		// Root spells' full target set (including a target set delivered by
		// the generic resolution pre-ask) is the exchange pair.
		if c.PickedTargets != nil {
			targets = append(targets, c.PickedTargets...)
		} else {
			targets = append(targets, c.Targets...)
		}
	}
	if len(targets) != 2 || targets[0].IsPlayer || targets[1].IsPlayer || targets[0].Obj == targets[1].Obj {
		return
	}
	first, second := g.Obj(targets[0].Obj), g.Obj(targets[1].Obj)
	if first == nil || second == nil || first.Zone != state.ZBattlefield || second.Zone != state.ZBattlefield {
		return
	}
	// Exchanging with the same controller cannot change control, and does
	// not create two artificial control-layer effects.
	firstController, secondController := first.Controller, second.Controller
	if firstController == secondController {
		return
	}

	firstGrant := ControlGrant{Obj: first.ID, ObjStamp: first.Timestamp, Previous: firstController,
		Controller: secondController, You: c.Controller, Source: c.Source, SVars: c.SVars}
	secondGrant := ControlGrant{Obj: second.ID, ObjStamp: second.Timestamp, Previous: secondController,
		Controller: firstController, You: c.Controller, Source: c.Source, SVars: c.SVars}
	if source := g.Obj(c.Source); source != nil && source.Zone == state.ZBattlefield {
		firstGrant.SourceStamp, secondGrant.SourceStamp = source.Timestamp, source.Timestamp
	}

	h.Emit(events.Event{Kind: events.ControlChange, Obj: first.ID, Player: secondController})
	h.Emit(events.Event{Kind: events.ControlChange, Obj: second.ID, Player: firstController})
	h.RegisterControl(firstGrant)
	h.RegisterControl(secondGrant)
	if strings.EqualFold(sa.ParamStr(cards.PKRememberExchanged), "True") {
		for _, id := range []state.ObjID{first.ID, second.ID} {
			target := state.Target{Obj: id}
			if !targetIn(c.Remembered, target) {
				c.Remembered = append(c.Remembered, target)
			}
			if source := h.Game().Obj(c.Source); source == nil || !targetIn(source.Remembered, target) {
				eventRemember(h, c, id)
			}
		}
	}
}
