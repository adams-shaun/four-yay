// target_modes.go holds the modal/charm target surface and the target-count
// bounds that depend on the chosen set: charm slots/groups and the sequential
// ask, the cross-mode and copy-target asks, the same-controller and
// different-controller normalisation, and the CMC/power total caps.
// Split out of stack.go by a pure move (no rename, no behaviour change).
package rules

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func charmTargetSlots(svars map[string]string, root *cards.SA, modes []string) []string {
	if root == nil || root.API != "Charm" || len(modes) < 2 {
		return nil
	}
	seen := make(map[string]bool, len(modes))
	var slots []string
	for _, name := range modes {
		name = strings.TrimSpace(name)
		if seen[name] {
			return nil
		}
		seen[name] = true
		if sub := cards.ResolveSVar(svars, name); sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
			slots = append(slots, name)
		}
	}
	if len(slots) < 2 {
		return nil
	}
	return slots
}

// charmBoundX is the {X} a Charm sub-ability's dynamic TargetMin$/TargetMax$
// bound resolves against: the pending cast's settled x when the ask IS the
// cast's announcement (the X announce runs at cast_begin before the mode and
// target asks, but payCast only stamps X onto the stack object after the
// targets are recorded), else the stack object's own X (a triggered modal
// ability was never paid an X and reads zero; a copy retargets through the
// copy-site reader that already passes o.X). The non-modal asks thread the
// same value at their own sites (rules/cast_targets.go's pc.x, the copy
// retarget below), so only the charm asks need this shared derivation.
func (e *Engine) charmBoundX(source state.ObjID) int32 {
	if pc := e.cast; pc != nil && pc.card == source {
		return pc.x
	}
	if o := e.G.Obj(source); o != nil {
		return o.X
	}
	return 0
}

// askCharmModeTargets is the ordinary distinct-mode target ask (CR 601.2c).
// Every target-bearing chosen mode declares its own targets, so one option
// group per mode is allocated with each mode's OWN bounds. When every mode is
// exactly 1..1 the single combined decision is kept -- Min == Max == number of
// modes, so Decision.Validate requires exactly one target from every mode's
// own candidate set, and that wire form is pinned by
// charm_distinct_targets_test.go. A mode with bounds outside 1..1 (an "up to N"
// plural declaration, a mandatory minimum above one, differing maxes) cannot
// be expressed in that one decision: a Decision has ONE global Min/Max, one
// shared GroupLimit and no per-group minimum, so a plural mode could otherwise
// satisfy the count while a mandatory single-target mode contributes nothing
// (and, with the mode chosen first, steal its target). Those shapes are asked
// SEQUENTIALLY, one mode at a time in chosen-mode order, through the same
// binding machinery (each answer lands as one group in pc.charmTargets /
// e.charmTargets, which recheckCharmTargets and charmDistinctTargetRun already
// consume positionally). infeasible means a mandatory mode's minimum cannot be
// met: callers must not fall back to the first-mode ask.
func (e *Engine) askCharmModeTargets(p state.PlayerID, source state.ObjID, svars map[string]string, root *cards.SA, modes []string) (asked, infeasible bool) {
	slots := charmTargetSlots(svars, root, modes)
	if len(slots) < 2 {
		return false, false
	}
	choices := strings.Split(root.Params["Choices"], ",")
	if status, _ := effects.CharmCrossModeShape(svars, choices); status != effects.CharmUniqueNone {
		// The already-implemented TargetUnique family has a different wire
		// contract (one target per mode AND one different player per target).
		// Leave it to its dedicated ask path rather than weakening that
		// constraint.
		return false, false
	}
	type slot struct {
		name string
		sa   *cards.SA
		cs   []targetCandidate
	}
	var all []slot
	allSimple := true
	for _, name := range slots {
		sa := cards.ResolveSVar(svars, name)
		min, max := e.resolvedTargetBounds(p, source, sa, e.charmBoundX(source))
		if min != 1 || max != 1 {
			allSimple = false
		}
		cs := e.legalTargetCandidates(p, source, source, sa)
		if len(cs) < min {
			// A mandatory minimum the board cannot meet makes the whole
			// announcement impossible (CR 601.2c): abort rather than pose an
			// unanswerable ask or silently drop the mode.
			return false, true
		}
		all = append(all, slot{name: name, sa: sa, cs: cs})
	}
	if !allSimple {
		// A shape the one combined decision cannot express: pose each mode's
		// own ask in turn. Seed the binding storage -- the cast's scratch or
		// the triggered object's map, discriminated exactly as handleTarget's
		// answer arm does -- so a skipped optional mode's empty group is
		// recorded where resolution reads it.
		var groups [][]state.Target
		groups, asked, infeasible = e.askCharmSeqSlot(p, source, svars, root, slots, groups)
		if infeasible {
			return false, true
		}
		e.storeCharmSeqGroups(source, groups)
		return asked, false
	}
	d := &decision.Decision{Player: p, Kind: decision.KTarget, Min: len(all), Max: len(all),
		Prompt: fmt.Sprintf("Choose one target for each of %d modes", len(all)), Source: source,
		ResumeKind: "charm_targets", ResumeModes: append([]string(nil), slots...)}
	for i, s := range all {
		for _, candidate := range s.cs {
			o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
				Label: e.targetOptionLabel(candidate), Obj: candidate.obj, Player: candidate.player,
				Group: fmt.Sprintf("charm-mode-%d", i), Controller: e.candidateControllerSeat(candidate)}
			d.Options = append(d.Options, o)
		}
	}
	e.ask(d)
	return true, false
}

// askCharmSeqSlot poses the next per-mode target ask of a sequential
// distinct-mode Charm. It walks slots from len(groups) forward: a mode whose
// mandatory minimum the board cannot meet reports infeasible; an optional mode
// with no legal candidates records an EMPTY group (keeping the positional
// binding aligned) and moves on; the first mode with candidates poses its own
// bounded KTarget and returns asked. It returns (groups,false,false) when
// every remaining slot was satisfied by an empty optional group, so the caller
// finishes the announcement. The accumulated groups are the same value the
// combined answer would have produced, in the same target-bearing-mode order,
// so charmTargetGroups/recheckCharmTargets/charmDistinctTargetRun bind them
// positionally without change.
func (e *Engine) askCharmSeqSlot(p state.PlayerID, source state.ObjID, svars map[string]string,
	root *cards.SA, slots []string, groups [][]state.Target) ([][]state.Target, bool, bool) {
	out := append([][]state.Target(nil), groups...)
	for idx := len(out); idx < len(slots); idx++ {
		sa := cards.ResolveSVar(svars, slots[idx])
		if sa == nil {
			out = append(out, nil)
			continue
		}
		min, max := e.resolvedTargetBounds(p, source, sa, e.charmBoundX(source))
		cs := e.legalTargetCandidates(p, source, source, sa)
		if len(cs) < min {
			return out, false, true
		}
		if max == 0 {
			// A RESOLVED zero bound -- the cast announced X = 0 against an
			// "up to X" mode -- takes no ask, exactly like resolveTop's N2
			// zero-target path: resolvedTargetBounds honours a resolved zero
			// as written (rules/target_legal.go), so the empty group records
			// the skipped slot and the announcement finishes silently.
			out = append(out, nil)
			continue
		}
		if len(cs) == 0 {
			out = append(out, nil)
			continue
		}
		if max > len(cs) {
			max = len(cs)
		}
		prompt := strings.TrimSpace(sa.Params["TgtPrompt"])
		if prompt == "" {
			prompt = "Choose a target"
		}
		d := &decision.Decision{Player: p, Kind: decision.KTarget, Min: min, Max: max,
			Prompt: prompt, Source: source, TargetEffect: e.describeTargetEffect(p, source, sa, 0),
			ResumeKind: "charm_mode_seq", ResumeSA: root,
			ResumeModes: append([]string(nil), slots...)}
		for _, candidate := range cs {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: candidate.kind, Label: e.targetOptionLabel(candidate), Obj: candidate.obj,
				Player: candidate.player, Controller: e.candidateControllerSeat(candidate)})
		}
		e.ask(d)
		return out, true, false
	}
	return out, false, false
}

// charmSeqGroups reads the sequential distinct-mode answer groups recorded so
// far, from the cast's scratch or the triggered object's map -- the same split
// storeCharmSeqGroups writes and handleTarget's combined arm discriminates.
// It is the ONE reader of that choice so the two halves cannot disagree about
// where a sequential group lives.
func (e *Engine) charmSeqGroups(source state.ObjID) [][]state.Target {
	if e.cast != nil {
		return e.cast.charmTargets
	}
	return e.charmTargets[source]
}

// storeCharmSeqGroups writes the sequential answer groups to the same place
// charmSeqGroups reads them and the combined answer's groups land: the cast's
// pc.charmTargets (committed to e.charmTargets by payCast) or the triggered
// stack object's e.charmTargets entry.
func (e *Engine) storeCharmSeqGroups(source state.ObjID, groups [][]state.Target) {
	if e.cast != nil {
		e.cast.charmTargets = groups
		return
	}
	if e.charmTargets == nil {
		e.charmTargets = make(map[state.ObjID][][]state.Target)
	}
	e.charmTargets[source] = groups
}

// charmSeqSVars resolves the SVars map and root Charm SA for a sequential ask's
// source object, so a re-asked slot resolves its mode body the same way the
// first pass did: a triggered ability reads its source permanent's face, a
// spell its own declared face.
func (e *Engine) charmSeqSVars(source state.ObjID) (map[string]string, *cards.SA, bool) {
	o := e.G.Obj(source)
	if o == nil {
		return nil, nil, false
	}
	if o.Ability != nil {
		src := e.G.Obj(o.Source)
		if src == nil || src.Face() == nil {
			return nil, nil, false
		}
		return src.Face().SVars, o.Ability, true
	}
	f := o.Face()
	if f == nil {
		return nil, nil, false
	}
	return f.SVars, f.SpellAbility(), true
}

// charmSeqAnswer records one answered per-mode group of a sequential
// distinct-mode Charm target declaration and either poses the next mode's ask
// or finalizes the announcement. It is the sequential twin of the
// charm_targets arm below: the group lands in the same positional storage
// (pc.charmTargets for a cast, e.charmTargets for a trigger), the flat target
// list and its TargetsChosen events are recorded in the same order, and the
// final group triggers the same payCast/trigger-drain tail -- so resolution's
// charmDistinctTargetRun and CR 608.2b's recheckCharmTargets need no change.
func (e *Engine) charmSeqAnswer(d *decision.Decision, in decision.Intent, chosen []decision.Option) {
	svars, root, ok := e.charmSeqSVars(d.Source)
	if !ok {
		return
	}
	thisGroup := targetOptions(chosen)
	prev := e.charmSeqGroups(d.Source)
	groups := append([][]state.Target(nil), prev...)
	groups = append(groups, thisGroup)
	appendFirst := len(prev) > 0
	if e.cast != nil {
		pc := e.cast
		pc.charmTargets = groups
		stageBase := len(pc.targets)
		pc.targets = append(pc.targets, thisGroup...)
		e.repriceForTargets(pc)
		if pc.stackObj != 0 {
			e.recordChosenTargets(pc.stackObj, chosen, stageBase > 0)
		}
	} else {
		e.storeCharmSeqGroups(d.Source, groups)
		e.recordChosenTargets(d.Source, chosen, appendFirst)
	}
	next, asked, infeasible := e.askCharmSeqSlot(d.Player, d.Source, svars, root, d.ResumeModes, groups)
	if infeasible {
		// The first pass pre-checked every slot's mandatory minimum, so this
		// cannot newly fail between asks; keep the callers' own infeasible
		// arms authoritative if it ever does.
		return
	}
	e.storeCharmSeqGroups(d.Source, next)
	if asked {
		return
	}
	if e.cast != nil {
		e.payCast()
		return
	}
	if e.drainAwaitsTarget {
		e.drainAwaitsTarget = false
		e.resumeTriggerDrain()
	} else if e.pending == nil {
		e.emit(events.Event{Kind: events.Priority, Player: in.Player, Amount: 0})
	}
}

// charmTargetGroups partitions a combined Charm target answer by its
// per-mode Option.Group labels. The returned order is target-bearing mode
// order, not the order in which a client happened to submit the options.
func charmTargetGroups(d *decision.Decision, chosen []decision.Option) ([][]state.Target, []decision.Option) {
	groups := make([][]state.Target, len(d.ResumeModes))
	bySlot := make([][]decision.Option, len(groups))
	for _, opt := range chosen {
		const prefix = "charm-mode-"
		if !strings.HasPrefix(opt.Group, prefix) {
			continue
		}
		i, err := strconv.Atoi(strings.TrimPrefix(opt.Group, prefix))
		if err != nil || i < 0 || i >= len(groups) {
			continue
		}
		if opt.Kind == "player" {
			groups[i] = append(groups[i], state.Target{Player: opt.Player, IsPlayer: true})
		} else {
			groups[i] = append(groups[i], state.Target{Obj: opt.Obj})
		}
		bySlot[i] = append(bySlot[i], opt)
	}
	var ordered []decision.Option
	for i := range bySlot {
		ordered = append(ordered, bySlot[i]...)
	}
	return groups, ordered
}

// askCrossModeCharmTargets poses the cross-mode TargetUnique family's ONE
// combined target ask: Min == Max == the number of chosen target-bearing
// modes, over the shared candidate pool of the modes' common ValidTgts$ spec,
// with every option's Group naming its player — Decision.Validate's
// mutual-exclusion rule (and botpolicy clamp's group discipline) enforce
// "each mode must target a different player" on the wire, so an intent that
// reuses a player is not merely wrong but impossible to submit. The answer
// records onto the stack object in choice order, which is the chosen-mode
// order, so the per-mode attribution is positional and replay-safe without
// any new event kind or field. Returns false (nothing asked) when the legal
// candidates are fewer than the modes that need them — the caller keeps the
// historical first-mode narrowing, whose own insufficiency handling governs.
func (e *Engine) askCrossModeCharmTargets(p state.PlayerID, source state.ObjID, tbms []*cards.SA) bool {
	k := len(tbms)
	sub := tbms[0]
	candidates := e.legalTargetCandidates(p, source, source, sub)
	// MaxTotalTargetPower$ over the cross-mode pool: the same shared cap read
	// every other target ask uses, so a Charm whose first target-bearing mode
	// carries the parameter prunes the census that can provably join no legal
	// selection and rides the running budget on the decision exactly as
	// askTarget and cast.go's targetAsk do. The family's modes target players
	// (CharmCrossModeShape requires a player-kind spec), whose Value is 0 and
	// which are never pruned, so a non-negative cap never rejects the required
	// one-per-mode answer; the read exists so the ask cannot silently ignore a
	// cap a caller put on it.
	candidates, powerCap, powerCapped := e.totalPowerCappedCandidates(candidates, p, source, sub, 0)
	candidates, cmcCap, cmcCapped := e.totalCMCCappedCandidates(candidates, p, source, sub, 0)
	if len(candidates) < k {
		return false
	}
	d := &decision.Decision{Player: p, Kind: decision.KTarget, Min: k, Max: k,
		Prompt: fmt.Sprintf("Choose %d targets: one for each mode, each a different player", k),
		Source: source, TargetEffect: e.describeTargetEffect(p, source, sub, 0)}
	for _, candidate := range candidates {
		o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
			Label: e.targetOptionLabel(candidate), Obj: candidate.obj, Player: candidate.player}
		if candidate.kind == "player" {
			o.Group = "charm-mode-player-" + strconv.Itoa(int(candidate.player))
		}
		if powerCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				o.Value = int(e.Power(candidate.obj))
			}
		}
		if cmcCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				if powerCapped {
					o.Value2 = int(co.Face().ManaValue())
				} else {
					o.Value = int(co.Face().ManaValue())
				}
			}
		}
		d.Options = append(d.Options, o)
	}
	if powerCapped {
		d.MaxSum, d.Budgeted = powerCap, true
	}
	if cmcCapped {
		if powerCapped {
			d.MaxSum2, d.Budgeted2 = cmcCap, true
		} else {
			d.MaxSum, d.Budgeted = cmcCap, true
		}
	}
	e.ask(d)
	return true
}

// candidateControllerSeat is the controller seat a TARGET CANDIDATE keys a
// per-controller selection constraint on: a player candidate is its own seat
// (player candidates carry an explicit seat), an object candidate is its
// controller (its owner for a card in a graveyard/hand/exile, which Move sets
// Controller to). Both oneEachTargetBounds' distinct-controller count and
// targetControllerGroup's group label read this one helper, so the bound and
// the wire exclusivity can never disagree about which candidates share a
// controller.
func (e *Engine) candidateControllerSeat(candidate targetCandidate) state.PlayerID {
	if candidate.kind == "player" {
		return candidate.player
	}
	if o := e.G.Obj(candidate.obj); o != nil {
		return o.Controller
	}
	return candidate.player
}

// oneEachTargetBounds applies Forge's per-controller target selection shapes:
// TargetsForEachPlayer$ (TargetRestrictions.setForEachPlayer) and
// TargetsWithDifferentControllers$ ("targets controlled by different
// players"). Both are the SAME set constraint -- no two chosen targets may
// share a controller -- and both are labelled on the wire by
// targetControllerGroup's Option.Group. askTarget (this file) and cast.go's
// targetAsk share it so the two ask sites cannot drift.
//
// It returns the (possibly rewritten) bounds, whether the constraint applies
// at all, and the DISTINCT-CONTROLLER count. The count is the real capacity of
// the constraint: a TargetMin$/TargetMax$ spelled OneEach asks for exactly
// that many, and BOTH shapes cap the effective maximum at it, because no legal
// answer can ever select more targets than there are distinct controllers.
// Without the cap (the pre-fix state) a TargetsWithDifferentControllers$ SA
// with a literal TargetMin$ 2 | TargetMax$ 2 asked for two picks while
// offering only one selectable group -- an unsatisfiable decision that no
// intent could answer (Run Away Together, Kitsune, Dragon's Daughter).
// Callers compare min against distinct (not the raw option count) to detect
// that no legal set exists.
// sameControllerTargetBounds applies the TargetsWithSameController$ set
// constraint. Unlike the one-per-controller family, this permits multiple
// picks from one group; its capacity is therefore the largest controller
// group, not the number of groups. The capacity is returned separately so
// callers can reject a mandatory ask without exposing an unsatisfiable
// decision.
func (e *Engine) sameControllerTargetBounds(sa *cards.SA, candidates []targetCandidate, min, max int) (int, int, int, bool) {
	if !strings.EqualFold(sa.Params["TargetsWithSameController"], "True") {
		return min, max, 0, false
	}
	counts := map[state.PlayerID]int{}
	for _, candidate := range candidates {
		counts[e.candidateControllerSeat(candidate)]++
	}
	capacity := 0
	for _, count := range counts {
		if count > capacity {
			capacity = count
		}
	}
	if capacity > 0 && max > capacity {
		max = capacity
	}
	return min, max, capacity, true
}

func (e *Engine) oneEachTargetBounds(sa *cards.SA, candidates []targetCandidate, min, max int) (int, int, bool, int) {
	if !targetControllerExclusive(sa) {
		return min, max, false, 0
	}
	// Option.Group makes the one-per-controller restriction part of the
	// generic decision contract, so every target API consumes the same
	// enforcement rather than each effect maintaining a picker.
	groups := map[state.PlayerID]bool{}
	for _, candidate := range candidates {
		groups[e.candidateControllerSeat(candidate)] = true
	}
	distinct := len(groups)
	// OneEach respells a bound as the distinct-controller count. It is the
	// TargetsForEachPlayer$ spelling in Forge's grammar, but the corpus also
	// writes it on a TargetsWithDifferentControllers$ SA (Mysterious
	// Stranger's "for each player" graveyard pick), where it means the same
	// thing -- so the respell is driven by the VALUE, not by which of the two
	// equivalent flags is present. Before this only the
	// TargetsForEachPlayer$ spelling was read and Mysterious Stranger asked
	// for ONE target (Min 1 / Max 1) instead of one per represented player.
	if strings.EqualFold(sa.Params["TargetMin"], "OneEach") {
		min = distinct
	}
	if strings.EqualFold(sa.Params["TargetMax"], "OneEach") {
		max = distinct
	}
	// Cap the maximum at the distinct-controller count for BOTH shapes: a
	// literal or dynamic TargetMax$ larger than the number of controllers
	// present could only invite an answer the exclusivity rule rejects, so
	// the offer must advertise the true capacity (Havoc Eater's TargetMax$ X,
	// Protector of the Wastes' TargetMax$ 2). distinct is 0 only when there
	// are no candidates at all, and the no-option paths handle that before a
	// decision is built, so the max >= 1 clamp contract is preserved.
	if distinct > 0 && max > distinct {
		max = distinct
	}
	return min, max, true, distinct
}

// targetControllerExclusive reports whether this targeting SA carries Forge's
// per-controller selection shape -- TargetsForEachPlayer$ True ("up to one
// target each player controls", the OneEach family) or
// TargetsWithDifferentControllers$ True ("targets controlled by different
// players", Protector of the Wastes and 7 more corpus carriers). Both are the
// SAME set constraint -- no two chosen targets may share a controller -- and
// both are expressed on the wire by Option.Group, so one predicate backs both
// and the resolution recheck reads it through the same helper.
func (e *Engine) narrowSameController(targets []state.Target) []state.Target {
	if len(targets) < 2 {
		return targets
	}
	controller := targets[0]
	seat, ok := e.targetControllerSeat(controller)
	if !ok {
		return targets
	}
	out := targets[:0]
	for _, target := range targets {
		if got, valid := e.targetControllerSeat(target); valid && got == seat {
			out = append(out, target)
		}
	}
	return out
}

func targetControllerExclusive(sa *cards.SA) bool {
	return strings.EqualFold(sa.Params["TargetsForEachPlayer"], "True") ||
		strings.EqualFold(sa.Params["TargetsWithDifferentControllers"], "True")
}

// targetControllerGroup is the Option.Group label binding one selection slot
// to its controller -- the same label both ask sites attach, so
// Decision.Validate's mutual-exclusion rule enforces one pick per controller
// on the wire. It applies whenever targetControllerExclusive holds, so a
// TargetsWithDifferentControllers$ ask restricts the SAME way a OneEach ask
// does: the exclusivity is the generic wire contract's job, not each effect's.
func (e *Engine) targetControllerGroup(sa *cards.SA, candidate targetCandidate) string {
	if !targetControllerExclusive(sa) {
		return ""
	}
	return "target-controller-" + strconv.Itoa(int(e.candidateControllerSeat(candidate)))
}

// targetControllerSeat is the controller seat a chosen target keys the
// per-controller constraint on, read at the resolution recheck: a player
// target is its own seat; an object target is its controller (its owner for a
// card in a graveyard/hand/exile, which apply.go's Move fold keeps equal to
// Controller). It is the recheck's twin of targetControllerGroup, which reads
// the same seat off a candidate at the offer (candidatesFor labels a
// non-battlefield candidate with the zone slice's owner q, and Move sets
// Controller = Owner there), so offer and recheck cannot disagree. ok is false
// for a target whose seat is unknown (a vanished object), which the caller
// keeps rather than duplicating a phantom controller.
func (e *Engine) targetControllerSeat(t state.Target) (state.PlayerID, bool) {
	if t.IsPlayer {
		if int(t.Player) < len(e.G.Players) && !e.G.Players[t.Player].Lost {
			return t.Player, true
		}
		return 0, false
	}
	if o := e.G.Obj(t.Obj); o != nil {
		return o.Controller, true
	}
	return 0, false
}

// narrowDifferentControllers is CR 608.2b's "does as much as possible" read of
// TargetsWithDifferentControllers$ at resolution: walk the still-legal targets
// in recorded order and keep the first of each controller, dropping a later
// target whose controller already appears. A controller change in response can
// make two targets share a controller, and a resolution must never act on a
// set the targeting requirement forbids; keeping the earliest chosen target is
// the deterministic, order-stable reading. It is applied only to the flag-bearing
// SA, after the ordinary per-target legality recheck, so it only ever REMOVES a
// target -- it can never widen a set the per-target filter already narrowed.
func (e *Engine) narrowDifferentControllers(targets []state.Target) []state.Target {
	seen := map[state.PlayerID]bool{}
	out := targets[:0:0]
	for _, t := range targets {
		seat, ok := e.targetControllerSeat(t)
		if ok && seen[seat] {
			continue
		}
		if ok {
			seen[seat] = true
		}
		out = append(out, t)
	}
	return out
}

// maxTotalTargetPower resolves a targeting subject's MaxTotalTargetPower$
// cap -- the running total-power bound over a multi-target selection
// ("Return any number of target creature cards with total power 10 or less",
// Reunion of the House and Nethroi, Apex of Death; 2 corpus files). A literal
// token reads directly (including a literal 0 or negative, both enforceable:
// the ask sites attach every present cap as the decision's budget with
// Decision.Budgeted set, because a MaxSum of 0 alone reads as NO budget on
// the wire); a dynamic token resolves through the
// effects numeric grammar, the same reader resolvedTargetBounds applies to a
// TargetMax$ X token, bound to the asking player and the source anchor. A
// token the grammar cannot resolve returns ok=false -- the cap is then
// simply not enforced (today's behaviour; measured, no corpus carrier
// reaches this arm unresolvable, both carriers are the literal 10).

func (e *Engine) maxTotalTargetCMC(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) (int, bool) {
	v, ok := sa.Params["MaxTotalTargetCMC"]
	if !ok {
		return 0, false
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return n, true
	}
	ctx, okc := e.targetBoundCtx(p, source)
	if !okc {
		return 0, false
	}
	ctx.X = x
	if n, resolved := effects.NumResolved(e, ctx, sa, "MaxTotalTargetCMC", 0); resolved {
		return int(n), true
	}
	return 0, false
}

// totalCMCCappedCandidates is the target-walk half of MaxTotalTargetCMC$.
// Mana values are nonnegative, so an object over the cap can never join a
// legal subset. The decision's Value/MaxSum carries the subset sum constraint.
func (e *Engine) totalCMCCappedCandidates(candidates []targetCandidate, p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) ([]targetCandidate, int, bool) {
	capCMC, ok := e.maxTotalTargetCMC(p, source, sa, x)
	if !ok {
		return candidates, 0, false
	}
	out := make([]targetCandidate, 0, len(candidates))
	for _, c := range candidates {
		if c.kind == "player" {
			out = append(out, c)
			continue
		}
		o := e.G.Obj(c.obj)
		if o == nil || o.Face() == nil || int(o.Face().ManaValue()) > capCMC {
			continue
		}
		out = append(out, c)
	}
	return out, capCMC, true
}

func (e *Engine) maxTotalTargetPower(p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) (int, bool) {
	v, ok := sa.Params["MaxTotalTargetPower"]
	if !ok {
		return 0, false
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return n, true
	}
	ctx, okc := e.targetBoundCtx(p, source)
	if !okc {
		return 0, false
	}
	ctx.X = x
	if n, resolved := effects.NumResolved(e, ctx, sa, "MaxTotalTargetPower", 0); resolved {
		return int(n), true
	}
	return 0, false
}

// totalPowerCappedCandidates applies the MaxTotalTargetPower$ cap to a
// target census. The per-option half is a real filter, but ONLY where a
// candidate provably cannot join any legal selection: with every power
// non-negative, a candidate whose power ALONE exceeds the cap busts every
// set containing it and is pruned. A NEGATIVE-power candidate breaks that
// argument -- a CDA can be negative in a zone (Scourge of the Skyclaves's
// 20-minus-highest-life CDA is -1 at a 21-life opponent, and it applies in
// EVERY zone per CR 208.2), and 11 + (-1) = 10 is a legal compensated
// selection under a cap of 10 -- so when any candidate reads negative the
// over-cap candidate is pruned only when even the maximal offset cannot
// save it: no selection containing it can score under the cap unless it
// takes EVERY other negative candidate on offer, so the prune test is
// p + otherNeg > cap (otherNeg = the sum of the OTHER candidates' negative
// powers). TargetMax$ bounds the offset: a selection containing the
// candidate under test can take at most maxTargets-1 OTHER things (CR
// 601.2c), so the offset is the sum of the most negative maxTargets-1
// candidates, never the whole negative sum. Both corpus carriers' TargetMax$
// is X (every candidate), where the ceiling is unreachable and the offset is
// the whole negative sum exactly as before; a lone-target ask (the default
// TargetMax$ 1) has no offset at all. A survivor the TargetMax$-aware prune
// keeps is always selectable alongside the negatives it counted, and
// Decision.Validate still rejects any over-cap answer.
// The running half -- any combination whose summed power stays within the
// bound -- is NOT expressible as a per-candidate property, so it is not a
// filter here: the two ask sites (askTarget below and cast.go's targetAsk)
// attach it to the decision as the cumulative-budget wire contract --
// Decision.MaxSum over each object option's Value (the candidate's power,
// negatives included) -- which Decision.Validate enforces on every
// submitted answer and Clamp/decision.FitRequired mirror for the
// deterministic bot. That is the same mechanism a Dig's WithTotalCMC$
// budget uses, so what a client may submit and what the engine offered can
// never disagree. Player candidates carry Value 0 and are never pruned (a
// MaxTotalTargetPower$ ask names cards; a player's presence is free).
// Returns the pruned census, the cap and whether the parameter is present
// at all.
//
// The power read is the DERIVED power (Engine.Power), not the printed
// face: a characteristic-defining P/T applies in EVERY zone (CR 208.2 --
// Lord of Extinction counts the graveyards from its own graveyard, which
// derivedScalarFrom's CDA read covers), and the printed Face().Power()
// returns 0 for such a face -- the first cut of this read undercounted a
// CDA creature as a free target.
func (e *Engine) totalPowerCappedCandidates(candidates []targetCandidate, p state.PlayerID, source state.ObjID, sa *cards.SA, x int32) ([]targetCandidate, int, bool) {
	capPower, ok := e.maxTotalTargetPower(p, source, sa, x)
	if !ok {
		return candidates, 0, false
	}
	// First pass: every card candidate's DERIVED power, plus the census's
	// negative powers sorted most-negative first (the pool the offset draws
	// from). Player candidates carry no power.
	power := make([]int32, len(candidates))
	var negs []int32
	for i, c := range candidates {
		if c.kind == "player" {
			continue
		}
		o := e.G.Obj(c.obj)
		if o == nil || o.Face() == nil {
			continue
		}
		power[i] = e.Power(c.obj)
		if power[i] < 0 {
			negs = append(negs, power[i])
		}
	}
	sort.Slice(negs, func(a, b int) bool { return negs[a] < negs[b] })
	// A selection containing the candidate under test has at most this many
	// OTHER slots (CR 601.2c), so its offset may draw on at most that many
	// negative candidates. resolvedTargetBounds clamps max >= 1, so a
	// lone-target ask (the default) has otherSlots 0 and no offset at all.
	_, maxTargets := e.resolvedTargetBounds(p, source, sa, x)
	otherSlots := maxTargets - 1
	if otherSlots < 0 {
		otherSlots = 0
	}
	out := make([]targetCandidate, 0, len(candidates))
	for i, c := range candidates {
		if c.kind == "player" {
			out = append(out, c)
			continue
		}
		o := e.G.Obj(c.obj)
		if o == nil || o.Face() == nil {
			continue
		}
		// Prune only a candidate no legal selection can contain: its own
		// power plus the maximal offset the OTHER selectable candidates can
		// supply (the most negative maxTargets-1 of them) still busts the
		// cap. With no negatives that is the plain p > cap prune; a candidate
		// at or under the cap is never pruned by it when cap >= 0. The same
		// rule holds for a cap of zero or less (powers 2,-1,-1 under a cap of
		// 0 with maxTargets >= 3 total 0, so the 2 stays; under maxTargets 2
		// only one -1 fits, 2-1 = 1 > 0, so the 2 is pruned), and it can
		// prune the whole census. Whatever survives, taking the negatives the
		// offset counted alongside it fits within both the cap and Max, which
		// is what decision.FitRequired's negative top-up relies on to always
		// reach a valid answer.
		p := power[i]
		others := maxNegativeOffset(negs, otherSlots, p < 0, p)
		if p+others > int32(capPower) {
			continue
		}
		out = append(out, c)
	}
	return out, capPower, true
}

// maxNegativeOffset is the most a selection of at most `slots` candidates can
// claw back from `negs` (negative powers sorted most-negative first), skipping
// one occurrence of skipVal when the candidate under test is itself negative
// and so already contributes its own power to the sum. A missing skipVal or a
// non-positive slots yields the plain top-slots sum (0 when slots <= 0). The
// caller's census is small, so the linear scan per candidate is kept simple
// rather than prefix-summed.
func maxNegativeOffset(negs []int32, slots int, skip bool, skipVal int32) int32 {
	if slots <= 0 {
		return 0
	}
	sum := int32(0)
	taken := 0
	skipped := !skip
	for _, v := range negs {
		if !skipped && v == skipVal {
			skipped = true
			continue
		}
		if taken >= slots {
			break
		}
		sum += v
		taken++
	}
	return sum
}

// AskCopyTargets offers CR 707.10c's new-target choice for the copy on top
// of the stack. It is driven entirely by the copy's own
// CopyMayChooseTarget flag -- set by the StackCopy fold from the CREATING
// CopySpellAbility's MayChooseTarget$ parameter, so an external copier
// (Mirari, Cloven Casting, a Storm or Replicate copy) grants the election
// even though its SA is not part of the copied spell's text.
//
// The ask preserves the copied spell's WHOLE target requirement, not just
// one slot: MayChooseTarget$ True is not restricted to one-target spells, so
// the decision's bounds come from the copy's own declaration through the
// SAME resolvedTargetBounds / oneEachTargetBounds pair askTarget and cast.go's
// targetAsk use (a two-target spell therefore accepts two picks, and a
// per-controller declaration keeps its Option.Group exclusivity). Every
// inherited target is offered first as a keep-current option -- ALWAYS, even
// when it is no longer legal, because choosing new targets is optional and a
// player who keeps an illegal target simply lets the copy fizzle per CR
// 608.2b (forcing a new target here would retarget a copy the player declined
// to change) -- so selecting the leading keep-current options reproduces
// "choose nothing new". The remaining options use the same legal-target
// census as casting. The election is one-shot: the answer records targets
// through recordChosenTargets, whose TargetsChosen fold clears the flag, so
// the resolveTop re-entry does not ask again.
//
// A copy whose spell has MORE THAN ONE target DECLARATION -- the two halves
// of a Fuse cast, or each target-bearing mode of a modal spell -- is asked
// ONE DECISION PER DECLARATION, in the same
// order the cast asked them (castStageSA / castHasNextTargetStage), so each
// declaration is offered its own legal set, its own bounds and its own
// per-controller Option.Group. The earlier single-list shape flattened every
// declaration into one pool, so a half's own ValidTgts$ was never applied to
// its half's choices (a fused Wear // Tear offered only artifacts and never
// the enchantment the alternate half demands). A copy inherits the cast's
// announcement and target provenance; an illegal inherited target remains
// in its original declaration rather than migrating to a later stage. The
// in-progress stage is
// tracked in Engine.copyTargetStage keyed on the copy's stack object, so the
// second-stage ask survives the TargetsChosen fold that clears
// CopyMayChooseTarget after the first declaration.
func (e *Engine) AskCopyTargets() bool {
	n := len(e.G.Stack)
	if n == 0 {
		return false
	}
	o := e.G.Obj(e.G.Stack[n-1])
	if o == nil || !o.IsCopy {
		return false
	}
	stage := 0
	if e.copyTargetStage != nil {
		stage = e.copyTargetStage[o.ID]
	}
	// The one-shot election is pending while the flag is set OR while a
	// multi-declaration ask is mid-flight (the first answer's TargetsChosen
	// fold clears the flag, but later declarations still owe an ask).
	if !o.CopyMayChooseTarget && stage == 0 {
		return false
	}
	controller := o.Controller
	decls := e.copyTargetDeclarations(o)
	if stage >= len(decls) {
		return false
	}
	sa := decls[stage]
	if sa == nil || strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
		return false
	}
	candidates := e.legalTargetCandidates(controller, o.ID, o.ID, sa)
	// MaxTotalTargetPower$ (Reunion of the House): re-run the cast ask's
	// per-candidate prune here, so a copy of a power-capped multi-target spell
	// offers the same census the cast did. The running budget rides the
	// decision as Decision.MaxSum over Option.Value (below), exactly as the
	// cast ask attaches it.
	candidates, powerCap, powerCapped := e.totalPowerCappedCandidates(candidates, controller, o.ID, sa, o.X)
	candidates, cmcCap, cmcCapped := e.totalCMCCappedCandidates(candidates, controller, o.ID, sa, o.X)
	// Keep the original declaration's targets, including ones that have since
	// become illegal. The StackCopy emission inherits the cast's stage split;
	// when no split exists, assign flat targets by declaration position, never
	// by current legality.
	inherited := e.copyInheritedForDeclaration(o, decls, stage)
	ordered := make([]targetCandidate, 0, len(candidates)+len(inherited))
	for _, old := range inherited {
		matched := -1
		for i, candidate := range candidates {
			if targetCandidateEqual(old, candidate) {
				matched = i
				break
			}
		}
		if matched >= 0 {
			ordered = append(ordered, candidates[matched])
			candidates = append(candidates[:matched], candidates[matched+1:]...)
		} else {
			// Keep-current even though the target is no longer legal: the
			// player may decline new targets (CR 707.10c), and the copy
			// then fizzles at CR 608.2b.
			ordered = append(ordered, stateTargetCandidate(old))
		}
	}
	ordered = append(ordered, candidates...)
	if len(ordered) == 0 {
		return false
	}
	// The copy's OWN declaration supplies the required count (CR 707.10c
	// retargets a copy per the spell's target rules), through the same shared
	// readers the cast ask uses so the two sites cannot drift. oneEachTargetBounds
	// is fed the full selectable list -- keep-current slots included -- so the
	// per-controller capacity counts a kept target too.
	min, max := e.resolvedTargetBounds(controller, o.ID, sa, o.X)
	min, max, _, _ = e.oneEachTargetBounds(sa, ordered, min, max)
	// A mandatory minimum above the offered list would be an unanswerable
	// decision no seat could satisfy (a livelock). The inherited keep-current
	// entries are always offered even when illegal, so the list is non-empty;
	// clamping Min to it keeps totality whenever a dynamic bound outruns the
	// copy's inherited set (the copy then fizzles at CR 608.2b like any other
	// under-target resolution).
	if min > len(ordered) {
		min = len(ordered)
	}
	if max < min {
		max = min
	}
	// A declaration whose resolved bound admits NO target (Min 0 Max 0 -- a
	// dynamic TargetMax$ that evaluated to zero) leaves nothing to choose:
	// the only legal answer is the empty one, which Engine.ask refuses to
	// post. Resolve it silently -- the copy keeps its inherited (empty)
	// set for this declaration -- and move on to the next declaration, if
	// any, exactly as an answered ask would.
	if max == 0 {
		if e.copyTargetStage == nil {
			e.copyTargetStage = make(map[state.ObjID]int)
		}
		e.copyTargetStage[o.ID] = stage + 1
		return e.AskCopyTargets()
	}
	d := &decision.Decision{Player: controller, Kind: decision.KTarget, Min: min, Max: max,
		Prompt: "Choose a new target for the copy", Source: o.ID,
		ResumeKind: "copy_targets", ResumeSA: sa, TargetEffect: e.describeTargetEffect(controller, o.ID, sa, o.X)}
	for _, candidate := range ordered {
		opt := decision.Option{Index: len(d.Options), Kind: candidate.kind,
			Label: e.targetOptionLabel(candidate), Obj: candidate.obj, Player: candidate.player,
			Group: e.targetControllerGroup(sa, candidate)}
		opt.Controller = e.candidateControllerSeat(candidate)
		// Option.Value is read only under a budget (Decision.HasBudget), so a
		// budget-less copy ask keeps its wire payload byte-identical. The
		// value is the DERIVED power (Engine.Power), matching the prune read.
		if powerCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				opt.Value = int(e.Power(candidate.obj))
			}
		}
		if cmcCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				if powerCapped {
					opt.Value2 = int(co.Face().ManaValue())
				} else {
					opt.Value = int(co.Face().ManaValue())
				}
			}
		}
		d.Options = append(d.Options, opt)
	}
	if powerCapped {
		d.MaxSum, d.Budgeted = powerCap, true
	}
	if cmcCapped {
		if powerCapped {
			d.MaxSum2, d.Budgeted2 = cmcCap, true
		} else {
			d.MaxSum, d.Budgeted = cmcCap, true
		}
	}
	// Record the stage BEFORE the ask is answered: the answer's TargetsChosen
	// clears the one-shot flag, so the resolveTop re-entry learns from this
	// scratch that the next declaration (if any) still owes an ask.
	if e.copyTargetStage == nil {
		e.copyTargetStage = make(map[state.ObjID]int)
	}
	e.copyTargetStage[o.ID] = stage + 1
	e.Ask(d)
	return true
}

// copyTargetDeclarations returns the target DECLARATIONS a copy must ask, in
// cast order. A Fuse copy has two: the front half's spell ability and the
// alternate half's (split.go's fusedSplitFaces), each with its own ValidTgts$
// and therefore its own legal set. For a chosen-mode Charm, each selected
// target-bearing mode is a separate declaration in chosen order. A declaration
// with no ValidTgts$ is dropped.
func (e *Engine) copyTargetDeclarations(o *state.Object) []*cards.SA {
	if o == nil {
		return nil
	}
	if o.Ability != nil {
		if src := e.G.Obj(o.Source); src != nil && src.Face() != nil {
			if modes := copyCharmModes(src.Face(), o.Ability, o.ChosenModes); len(modes) > 0 {
				return modes
			}
		}
		return []*cards.SA{o.Ability}
	}
	f := o.Face()
	if f == nil {
		return nil
	}
	if ff, fa := fusedSplitFaces(o); ff != nil && fa != nil {
		out := make([]*cards.SA, 0, 2)
		for _, hf := range []*cards.Face{ff, fa} {
			if modes := copyCharmModes(hf, hf.SpellAbility(), o.ChosenModes); len(modes) > 0 {
				out = append(out, modes...)
			} else if sa := hf.SpellAbility(); sa != nil && strings.TrimSpace(sa.Params["ValidTgts"]) != "" {
				out = append(out, sa)
			}
		}
		return out
	}
	if modes := copyCharmModes(f, f.SpellAbility(), o.ChosenModes); len(modes) > 0 {
		return modes
	}
	sa := modalTargetSA(f, f.SpellAbility(), o.ChosenModes)
	if sa == nil {
		return nil
	}
	return []*cards.SA{sa}
}

func copyCharmModes(f *cards.Face, sa *cards.SA, names []string) []*cards.SA {
	if f == nil || sa == nil || sa.API != "Charm" || len(names) == 0 || sa.Params["ValidTgts"] != "" {
		return nil
	}
	var out []*cards.SA
	for _, name := range names {
		if sub := cards.ResolveSVar(f.SVars, name); sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
			out = append(out, sub)
		}
	}
	return out
}

// copyInheritedForDeclaration returns the targets owned by this declaration.
// Cast-time provenance survives StackCopy in fuseTargets. For copies without
// provenance, flat targets are divided by declaration bounds in cast order;
// legality must never be used to infer ownership after the board changes.
func (e *Engine) copyInheritedForDeclaration(o *state.Object, decls []*cards.SA, stage int) []state.Target {
	if o == nil || len(o.Targets) == 0 {
		return nil
	}
	if len(decls) <= 1 {
		return o.Targets
	}
	if stages, ok := e.fuseTargets[o.ID]; ok {
		index := stage
		if ff, _ := fusedSplitFaces(o); ff != nil {
			if sa := ff.SpellAbility(); sa == nil || strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
				index++ // the first fused half has no target declaration
			}
		}
		if index < len(stages) {
			return stages[index]
		}
		return nil
	}
	start := 0
	for i, sa := range decls {
		end := len(o.Targets)
		if i < len(decls)-1 {
			_, max := e.resolvedTargetBounds(o.Controller, o.ID, sa, o.X)
			if max < 0 {
				max = 0
			}
			if end > start+max {
				end = start + max
			}
		}
		if i == stage {
			return o.Targets[start:end]
		}
		start = end
	}
	return nil
}

// stateTargetCandidate converts a recorded state.Target into the option shape
// the target census uses. The kind is only the wire label; a player target
// keeps "player" and every object target is offered as "permanent" (the
// label reads the object's own name, so a target that has left the
// battlefield still renders correctly).
func stateTargetCandidate(t state.Target) targetCandidate {
	if t.IsPlayer {
		return targetCandidate{kind: "player", player: t.Player}
	}
	return targetCandidate{kind: "permanent", obj: t.Obj}
}

func targetCandidateEqual(t state.Target, c targetCandidate) bool {
	if t.IsPlayer {
		return c.kind == "player" && c.player == t.Player
	}
	return c.kind != "player" && c.obj == t.Obj
}

// targetChooserCore is the ONE home for the TargetingPlayer$ redirect at the
// RULES-TIER ask sites: it reads the parameter off sa and resolves the
// referent against the stored trigger context (trigger-relative referents)
// or the living-seat table (the non-triggered Opponent form). Both the
// rules-tier ask sites (targetAskChooser) and the effects-tier mid-resolution
// asks (Engine.ChooserFor and OpponentPickAsk, which effects.Host calls)
// reach it, so the cast, activation, trigger, resolution-sub and
// mid-resolution ValidTgts$ (mvts1 "tgts" and ChangeZone "choice") paths
// cannot drift.
//
// Target LEGALITY is unaffected by every outcome: the caller keeps the
// ability controller as the reference for legalTargetCandidates /
// targetSpecContext, and only the decision's Player moves to the chooser.
//
// The third return (pick) is the multi-opponent Opponent form's selection
// signal: with two or more living opponents the CONTROLLER must first choose
// which of them answers, through the controller-facing "opp_pick" ask
// (poseOpponentPick for the rules tier, OpponentPickAsk for the effects
// tier). ok is then false and the caller poses that selection ask instead of
// the target ask; the answer re-poses the target ask to the chosen seat via
// the pin this function reads. A sole living opponent answers directly with
// no ask; a controller with no living opponent fails closed to the
// controller.
//
// Returns (controller, false, false) when sa names no chooser, or the spec
// is unknown, unbound or dead, so the ask stays with the controller.
