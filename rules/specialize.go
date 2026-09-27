package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Specialize (Battle for Baldur's Gate Commander / Bloomburrow Commander) is a
// no-stack special action: pay the printed `K:Specialize:<n>` cost on your own
// front-face permanent and it becomes one of the card's five specialization
// faces. This file owns the ONE home of the "specialize option offered"
// predicate -- rules/legal.go's options walk, rules/priority_guard.go's
// re-validation and the bot's policy arm all read it.

// specializeRider parses the trailing parameter segment of a K:Specialize
// line. Forge writes `K:Specialize:<cost>[:<ability name>][:<description>]
// [:Key$ value | Key2$ value2]`; the parameter segment is the last field and
// is recognised by the `$` it must contain. The name/description fields
// carry ordinary prose (and a colon is not a field separator inside them in
// the corpus), so a whole-value split would misread them as parameters.
func specializeRider(kw string) (params map[string]string, unsupported string, ok bool) {
	parts := strings.Split(kw, ":")
	if len(parts) < 2 || parts[0] != "Specialize" {
		return nil, "", false
	}
	if len(parts) == 2 {
		return nil, "", true
	}
	last := parts[len(parts)-1]
	if !strings.Contains(last, "$") {
		// Name-and/or-description only; no parameters.
		return nil, "", true
	}
	params = map[string]string{}
	for _, item := range strings.Split(last, "|") {
		k, v, cut := strings.Cut(item, "$")
		if !cut {
			continue
		}
		k = strings.TrimSpace(k)
		params[k] = strings.TrimSpace(v)
		switch k {
		case "AdditionalActivationZone", "ReduceCost":
			// Out of scope for this ticket: the option is NOT offered and the
			// unsupported rider is named instead. Both are loud so a corpus
			// count can see them rather than a silent merge.
			return nil, k, true
		case "IsPresent", "IsPresent2", "PresentCompare", "CheckSVar", "SVarCompare", "Condition":
			// Readable through the shared continuousGateHolds grammar.
		default:
			return nil, k, true
		}
	}
	return params, "", true
}

// specializeCost returns the printed activation cost of the front face's
// Specialize keyword. ok is false when the object is not an unspecialized
// front-face permanent in play or carries a rider this build does not price.
func specializeCost(o *state.Object) (Cost, bool) {
	if o == nil || o.Card == nil || o.Face() == nil ||
		o.Card.AlternateMode != "Specialize" || o.FaceIdx != 0 ||
		o.Zone != state.ZBattlefield {
		return Cost{}, false
	}
	for _, kw := range o.Face().Keywords {
		if !strings.HasPrefix(kw, "Specialize:") {
			continue
		}
		parts := strings.Split(kw, ":")
		n, err := strconv.Atoi(parts[1])
		if err != nil || n < 0 {
			return Cost{}, false
		}
		if _, unsupported, _ := specializeRider(kw); unsupported != "" {
			return Cost{}, false
		}
		return ParseCost(parts[1]), true
	}
	return Cost{}, false
}

// specializeLegal is the shared "may this permanent specialize into faceIdx
// right now" predicate: front face, controller, target face carries a
// SPECIALIZE: boundary, the readable gate riders hold, and the printed cost
// is payable by the same payment payMana will run.
func (e *Engine) specializeLegal(p state.PlayerID, id state.ObjID, faceIdx int) (Cost, bool) {
	o := e.G.Obj(id)
	cost, ok := specializeCost(o)
	if !ok || o.Controller != p || faceIdx < 1 || faceIdx >= len(o.Card.Faces) ||
		o.Card.Faces[faceIdx].SpecializeColor == "" || !e.costPayableOther(p, id, cost) {
		return Cost{}, false
	}
	for _, kw := range o.Face().Keywords {
		if !strings.HasPrefix(kw, "Specialize:") {
			continue
		}
		params, unsupported, _ := specializeRider(kw)
		if unsupported != "" {
			return Cost{}, false
		}
		if len(params) > 0 && !e.continuousGateHolds(staticView{
			Source: id, Controller: p, Params: params, SVars: o.Face().SVars,
		}) {
			return Cost{}, false
		}
	}
	return cost, true
}

// specializesMatches is Mode$ Specializes: "Whenever this permanent
// specializes". The specializing permanent is the event's Obj (events.
// Specialize), so the matcher fires on the object itself, exactly as
// TurnFaceUp fires on the turned permanent.
func (e *Engine) specializesMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	return ev.Kind == events.Specialize && ev.Obj == source
}

func init() {
	registerTrigMatcher((*Engine).specializesMatches, "Specializes")
}

// specialize resolves the special action: emit the priority marker (the
// unlock/station/turn-face-up shape), charge the printed cost through
// payMana, then record the face change as a replayable Specialize event.
func (e *Engine) specialize(p state.PlayerID, opt decision.Option) {
	i, err := strconv.Atoi(opt.Mode)
	if err != nil || i < 1 {
		return
	}
	cost, ok := e.specializeLegal(p, opt.Obj, i)
	if !ok {
		return
	}
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Priority, Amount: 0})
	if !e.payMana(p, cost) {
		return
	}
	e.emit(events.Event{Kind: events.Specialize, Obj: opt.Obj, Amount: int32(i)})
}
