package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 603.3d: a triggered ability's targets -- ALL of them, its SubAbility$
// links' included -- are chosen as the controller puts it on the stack, not
// as each link resolves. The engine has always asked a trigger's ROOT
// ValidTgts$ at placement (pushTrigger -> askTarget); a targeting SubAbility$
// link was asked mid-resolution instead (effects.chosenTargetsFor), and its
// answer was visible only to that link's own dispatch. A later link that
// acts on "the targets" (Defined$ Targeted, Forge's union of every target
// choice down the chain) then read the ROOT's list alone, so Uldaros
// Theorix's "exile up to one target nonland card of each card type from
// your graveyard" -- eight Min-0 targeting Pump links and an untargeted
// ChangeZone | Defined$ Targeted -- exiled nothing.
//
// This file announces such a chain's link targets at placement, through the
// same record the cast flow's chain pre-ask installs (Engine.castSubTargets,
// read at resolution as Ctx.SubPreAsk and rechecked by recheckCastSubTargets),
// and the resolution binds the root+chain union as Ctx.AllTargets, which the
// Defined$ Targeted referent reads.
//
// SCOPE (triggerChainPreAsks): only the chain shape whose late ask loses the
// target outright -- a link reading Defined$ Targeted AFTER a targeting link.
// Every other trigger chain keeps its mid-resolution link asks unchanged (a
// CR 603.3d timing divergence, not a lost target); widening this to every
// trigger chain moves the decision sequence of ~140 corpus triggers and is
// the general CR 603.3d stage the cast-side chain announcement (CR 601.2c)
// will be followed by.

// trigSubAsk is one triggered ability's in-flight chain announcement.
type trigSubAsk struct {
	obj        state.ObjID
	controller state.PlayerID
	subs       []*cards.SA
	ans        [][]state.Target
	stage      int
}

func (t *trigSubAsk) clone() *trigSubAsk {
	if t == nil {
		return nil
	}
	c := *t
	c.subs = append([]*cards.SA(nil), t.subs...)
	c.ans = make([][]state.Target, len(t.ans))
	for i, ts := range t.ans {
		if ts != nil {
			c.ans[i] = append([]state.Target{}, ts...)
		}
	}
	return &c
}

// triggerChainPreAsks returns the SubAbility$ links of a triggered ability
// whose targets are announced at placement, or nil when the chain is out of
// scope. In scope: a non-modal root whose chain has at least one targeting
// link (the cast flow's own collectSubTargetPreAsks walk) followed later by
// an untargeted link reading Defined$ Targeted. Out of scope, whole: a link
// that names its chooser (TargetingPlayer$ -- the opponent-pick round trip is
// cast/placement-root machinery) or carries a Defined$ beside its ValidTgts$
// (a target-reuse body the mid-resolution walk never consumes a pre-ask for).
func (e *Engine) triggerChainPreAsks(root *cards.SA) []*cards.SA {
	if root == nil || root.Sub == nil || strings.TrimSpace(root.ParamStr(cards.PKChoices)) != "" {
		return nil
	}
	subs := e.collectSubTargetPreAsks(root)
	if len(subs) == 0 {
		return nil
	}
	for _, sa := range subs {
		if effects.TargetsOf(sa).TargetingPlayer != "" || strings.TrimSpace(sa.ParamStr(cards.PKDefined)) != "" {
			return nil
		}
	}
	first := subs[0]
	seen := false
	for sa := root.Sub; sa != nil; sa = sa.Sub {
		if sa == first {
			seen = true
			continue
		}
		if seen && !effects.TargetsOf(sa).Targeted() &&
			strings.TrimSpace(sa.ParamStr(cards.PKDefined)) == "Targeted" {
			return subs
		}
	}
	return nil
}

// startTriggerSubTargets opens the chain announcement for the triggered
// ability just pushed as id (pushTrigger, after the root's own ask). When the
// root posed no decision the links are asked at once; otherwise the root's
// answer (handleTarget) continues into them.
func (e *Engine) startTriggerSubTargets(id state.ObjID, controller state.PlayerID, root *cards.SA) {
	subs := e.triggerChainPreAsks(root)
	if len(subs) == 0 {
		return
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZStack {
		return
	}
	e.trigSub = &trigSubAsk{obj: id, controller: controller, subs: subs, ans: make([][]state.Target, len(subs))}
	if e.Pending() == nil {
		e.askTriggerSubTargets()
	}
}

// askTriggerSubTargets poses the next unanswered link's target ask, the
// placement twin of the cast flow's subTargetAsk: the same candidate census
// the root ask uses (the trigger object as source), the TargetUnique$
// exclusion over the root's and earlier links' answers, an answered-empty
// record for a Min-0 link with no candidate, and -- a mandatory link with too
// few legal targets -- the root ask's own exit: the ability is removed from
// the stack (CR 603.3d). It reports whether a decision was posed; when every
// link is settled the answers are installed for the resolution.
func (e *Engine) askTriggerSubTargets() bool {
	ts := e.trigSub
	if ts == nil {
		return false
	}
	o := e.G.Obj(ts.obj)
	if o == nil || o.Zone != state.ZStack {
		e.trigSub = nil
		return false
	}
	for ts.stage < len(ts.subs) {
		sub := ts.subs[ts.stage]
		candidates := e.legalTargetCandidates(ts.controller, ts.obj, ts.obj, sub)
		if effects.TargetUniqueRequested(sub) {
			chosen := append([]state.Target(nil), o.Targets...)
			for i := 0; i < ts.stage; i++ {
				if effects.TargetUniqueRequested(ts.subs[i]) {
					chosen = append(chosen, ts.ans[i]...)
				}
			}
			filtered := candidates[:0]
			for _, cand := range candidates {
				t := state.Target{Obj: cand.obj, Player: cand.player, IsPlayer: cand.kind == "player"}
				if len(effects.TargetUniqueFilter(sub, []state.Target{t}, chosen)) != 0 {
					filtered = append(filtered, cand)
				}
			}
			candidates = filtered
		}
		min, max := e.resolvedTargetBounds(ts.controller, ts.obj, sub, 0)
		if min > 0 && len(candidates) < min {
			e.trigSub = nil
			e.emit(events.Event{Kind: events.MoveZone, Obj: ts.obj,
				From: state.ZStack, To: state.ZExile, Text: "countered: no legal targets"})
			e.ensureLeftTheStack(ts.obj, state.ZExile, "a replacement fully discarded this "+
				"ability's 'countered: no legal targets' move without relocating it anywhere; sent "+
				"to its resting zone instead of re-resolving forever")
			return false
		}
		if max == 0 || len(candidates) == 0 {
			ts.ans[ts.stage] = []state.Target{}
			ts.stage++
			continue
		}
		d := &decision.Decision{Player: ts.controller, Kind: decision.KTarget, Min: min, Max: max,
			Prompt: "Choose a target for " + e.targetName(ts.obj) + "'s chained ability",
			Source: ts.obj, ResumeKind: "trig_sub", ResumeSA: sub,
			TargetEffect: e.describeTargetEffect(ts.controller, ts.obj, sub, 0)}
		for _, candidate := range candidates {
			opt := decision.Option{Index: len(d.Options), Kind: candidate.kind,
				Label: e.targetOptionLabel(candidate), Obj: candidate.obj, Player: candidate.player}
			opt.Group = e.targetControllerGroup(sub, candidate)
			opt.Controller = e.candidateControllerSeat(candidate)
			d.Options = append(d.Options, opt)
		}
		e.ask(d)
		return true
	}
	m := make(map[string][]state.Target, len(ts.subs))
	for i, sa := range ts.subs {
		m[sa.Line] = ts.ans[i]
	}
	if e.castSubTargets == nil {
		e.castSubTargets = make(map[state.ObjID]map[string][]state.Target)
	}
	e.castSubTargets[ts.obj] = m
	e.trigSub = nil
	return false
}

// answerTriggerSubTarget records a "trig_sub" answer, poses the next link's
// ask, and once the chain is settled hands control back exactly as the
// root's own placement answer does: the trigger drain resumes, or (outside a
// drain) the answering player receives priority.
func (e *Engine) answerTriggerSubTarget(in decision.Intent, chosen []decision.Option) {
	if ts := e.trigSub; ts != nil && ts.stage < len(ts.subs) {
		ts.ans[ts.stage] = targetOptions(chosen)
		ts.stage++
	}
	if e.askTriggerSubTargets() {
		return
	}
	if e.drainAwaitsTarget {
		e.drainAwaitsTarget = false
		e.resumeTriggerDrain()
		return
	}
	e.emit(events.Event{Kind: events.Priority, Player: in.Player, Amount: 0})
}

// chainTargetUnion is the resolution's Ctx.AllTargets for a stack object
// whose chain targets were announced (Engine.castSubTargets): the root's
// targets followed by each announced link's (rechecked) answers in chain
// order -- Forge's union of every target choice down the chain, which the
// Defined$ Targeted referent and the AllTargeted$ count read. nil (unbound)
// for every object without an announced chain, so those keep reading their
// own Ctx.Targets.
func (e *Engine) chainTargetUnion(id state.ObjID, root *cards.SA, rootTargets []state.Target) []state.Target {
	answers := e.castSubTargets[id]
	if len(answers) == 0 {
		return nil
	}
	out := append(make([]state.Target, 0, len(rootTargets)+len(answers)), rootTargets...)
	for _, sa := range e.collectSubTargetPreAsks(root) {
		out = append(out, answers[sa.Line]...)
	}
	return out
}
