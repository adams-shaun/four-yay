package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// uniqueTargetSlot is one target declaration of a TargetUnique$ chain as the
// offer census sees it: its legal candidate pool and how many of them the
// announcement must name.
type uniqueTargetSlot struct {
	cands []targetCandidate
	need  int
}

// distinctTargetsFeasible reports whether every slot can name `need`
// candidates of its own pool with no candidate named by two slots: the
// assignment subTargetAsk's TargetUnique$ exclusion demands of a root and its
// unique chain links (each unique link excludes the root's answers and every
// earlier unique link's). It is a bipartite b-matching, answered by Kuhn's
// augmenting paths over need-many copies of each slot; the needs are a
// handful (one per "another target"), so the copies are too.
func distinctTargetsFeasible(slots []uniqueTargetSlot) bool {
	total := 0
	for _, s := range slots {
		if s.need > len(s.cands) {
			return false
		}
		total += s.need
	}
	if total == 0 {
		return true
	}
	// Every constrained pool holds at least the whole demand: whatever the
	// other slots took (fewer than total), enough of each pool is left, so a
	// greedy pass succeeds and no matching need be built.
	roomy := true
	for _, s := range slots {
		if s.need > 0 && len(s.cands) < total {
			roomy = false
			break
		}
	}
	if roomy {
		return true
	}
	// Index the distinct candidates (a player and an object at the same
	// numeric id are different targets). Linear dedupe: only reached when
	// some pool is smaller than the demand, i.e. on a sparse board.
	var keys []targetCandidate
	keyIndex := func(c targetCandidate) int {
		for i, k := range keys {
			if sameTargetCandidate(k, c) {
				return i
			}
		}
		keys = append(keys, c)
		return len(keys) - 1
	}
	pools := make([][]int, len(slots))
	for i, s := range slots {
		if s.need == 0 {
			continue
		}
		pools[i] = make([]int, len(s.cands))
		for j, c := range s.cands {
			pools[i][j] = keyIndex(c)
		}
	}
	copies := make([]int, 0, total) // the slot each copy belongs to
	for i, s := range slots {
		for k := 0; k < s.need; k++ {
			copies = append(copies, i)
		}
	}
	owner := make([]int, len(keys)) // the copy holding each candidate, -1 free
	for i := range owner {
		owner[i] = -1
	}
	seen := make([]bool, len(keys))
	var augment func(c int) bool
	augment = func(c int) bool {
		for _, k := range pools[copies[c]] {
			if seen[k] {
				continue
			}
			seen[k] = true
			if owner[k] < 0 || augment(owner[k]) {
				owner[k] = c
				return true
			}
		}
		return false
	}
	for c := range copies {
		clear(seen)
		if !augment(c) {
			return false
		}
	}
	return true
}

// uniqueChainTargetsFeasible is the joint half of chainTargetsAvailable: the
// root declaration and the chain's TargetUnique$ links must be announceable
// together, pairwise distinct. Each slot's mandatory minimum is the offer
// census's own (targetOfferMin); a minimum that waits for an announcement
// (a pending X) is judged by the post-push ask instead, so its slot demands
// nothing here. A root this census cannot judge before the announcement (an
// Announce$ root whose declaration reads the announced value,
// announceShapesTargets, which rootTargetsAvailable also leaves offerable) likewise
// demands nothing. gift is the Gift election being judged (nil: either).
func uniqueChainTargetsFeasible(e *Engine, p state.PlayerID, id, excludeSelf state.ObjID, root *cards.SA, uniq []*cards.SA, x int32, xPending bool, gift *bool) bool {
	slots := make([]uniqueTargetSlot, 0, len(uniq)+1)
	if effects.TargetsOf(root).Targeted() && !announceShapesTargets(e, id, root) {
		if need, dynamic := targetOfferMin(e, p, id, root, x, xPending, gift); !dynamic && need > 0 {
			slots = append(slots, uniqueTargetSlot{cands: e.legalTargetCandidates(p, id, excludeSelf, root), need: need})
		}
	}
	for _, sub := range uniq {
		need, dynamic := targetOfferMin(e, p, id, sub, x, xPending, gift)
		if dynamic || need <= 0 {
			continue
		}
		subExclude := excludeSelf
		if sub.API == "Attach" {
			subExclude = id
		}
		slots = append(slots, uniqueTargetSlot{cands: e.legalTargetCandidates(p, id, subExclude, sub), need: need})
	}
	if len(slots) < 2 {
		// One declaration alone was already judged by targetSAAvailable.
		return true
	}
	return distinctTargetsFeasible(slots)
}

// modeTargetsAvailable is the ONE per-mode target predicate of a Charm cast:
// the mode's own declaration AND its SubAbility$ chain (CR 601.2c: a chosen
// mode's every target is announced on cast -- modalTargetSA makes the mode
// the cast's target root, whose chain subTargetAsk announces). The offer
// census (charmTargetsAvailable) and the post-push mode ask (castModeAsk)
// both filter modes through it, so a mode the ask offers can always be
// announced in full.
func modeTargetsAvailable(e *Engine, p state.PlayerID, id state.ObjID, sub *cards.SA, x int32, xPending bool) bool {
	if sub == nil {
		return true
	}
	return (!effects.TargetsOf(sub).Targeted() || e.targetSAAvailable(p, id, id, sub, x, xPending)) &&
		e.chainTargetsAvailable(p, id, id, sub, x, xPending, nil)
}

// castSubAskLinks is the chain links subTargetAsk announces for this cast,
// in order (CR 601.2c): every pre-askable link of the cast's target root,
// none for a Fuse or overloaded cast (see subTargetAsk). The root target ask
// reads the same list before subTargetAsk collects it, so the pruning below
// and the asks it protects see one chain.
func castSubAskLinks(e *Engine, pc *pendingCast, root *cards.SA) []*cards.SA {
	if pc.mode == "fuse" || altCastIs(pc.mode, altOverload) {
		return nil
	}
	// For a Charm cast, announce every selected mode's SubAbility$ chain;
	// the Charm itself is only the mode-election root. This handles a single
	// selected mode as well as multi-mode casts while leaving non-cast Charm
	// callers on their existing resolution-time path.
	var roots []*cards.SA
	var charm *cards.SA
	if !pc.isAbility() {
		if o := e.G.Obj(pc.card); o != nil && o.Face() != nil {
			charm = o.Face().SpellAbility()
			if effects.CharmOf(charm).HasChoices && len(o.ChosenModes) > 0 {
				for _, name := range o.ChosenModes {
					if mode := cards.ResolveSVar(o.Face().SVars, name); mode != nil {
						roots = append(roots, mode)
					}
				}
			}
		}
	}
	if !effects.CharmOf(charm).HasChoices && root != nil {
		roots = append(roots, root)
	}
	var out []*cards.SA
	for _, mode := range roots {
		for _, sub := range e.collectSubTargetPreAsks(mode, pc.card) {
			if e.castSubPreAskable(pc, sub) {
				out = append(out, sub)
			}
		}
	}
	return out
}

// uniqueChainViableCandidates withholds, from one announcement ask of a cast
// whose chain carries later TargetUnique$ links, every candidate whose choice
// leaves those links no distinct legal assignment: naming it can only end in
// the CR 733.1 reversal ("no legal target for a chained ability"), exactly
// the unaffordable-target case targetAsk already withholds
// (affordableTargetCandidates). Band Together with one creature on the table:
// naming it among "up to two target creatures you control" leaves "another
// target creature" nothing, while naming none leaves it the creature.
//
// links are the cast's chain asks (castSubAskLinks) and from the index of
// the first link still to be announced; chosen is what the current and every
// later unique link must avoid (the root's answers and earlier unique
// links'); need is the current ask's mandatory minimum. Only the root ask and
// a TargetUnique$ link's own ask call it -- a plain link's answer is not
// excluded by any later link. The verdict per candidate is a joint
// assignment (distinctTargetsFeasible) of the current ask's remaining
// minimum and each later unique link's minimum, so it is exact for a
// single-target ask and a sound necessary condition for a multi-target one.
func uniqueChainViableCandidates(e *Engine, pc *pendingCast, links []*cards.SA, from int, chosen []state.Target, need int, candidates []targetCandidate) []targetCandidate {
	if len(candidates) == 0 || from >= len(links) {
		return candidates
	}
	var later []uniqueTargetSlot
	for _, sub := range links[from:] {
		if !effects.TargetUniqueRequested(sub) || !castSubTargetsOwed(pc, sub) {
			continue
		}
		min, _ := e.resolvedTargetBounds(pc.player, pc.card, sub, pc.x)
		if min <= 0 {
			continue
		}
		var excludeSelf state.ObjID
		if !pc.isAbility() || sub.API == "Attach" {
			excludeSelf = pc.card
		}
		pool := e.legalTargetCandidates(pc.player, pc.card, excludeSelf, sub)
		kept := pool[:0]
		for _, c := range pool {
			if !targetCandidateIn(c, chosen) {
				kept = append(kept, c)
			}
		}
		later = append(later, uniqueTargetSlot{cands: kept, need: min})
	}
	if len(later) == 0 {
		return candidates
	}
	rest := need - 1
	if rest < 0 {
		rest = 0
	}
	slots := make([]uniqueTargetSlot, len(later)+1)
	out := candidates[:0:0]
	for _, c := range candidates {
		slots[0] = uniqueTargetSlot{cands: targetCandidatesWithout(candidates, c), need: rest}
		for i, s := range later {
			slots[i+1] = uniqueTargetSlot{cands: targetCandidatesWithout(s.cands, c), need: s.need}
		}
		if distinctTargetsFeasible(slots) {
			out = append(out, c)
		}
	}
	if len(out) == len(candidates) {
		return candidates
	}
	return out
}

// sameTargetCandidate reports whether two candidates name the same player or
// the same object (a player and an object are never the same target).
func sameTargetCandidate(a, b targetCandidate) bool {
	if (a.kind == "player") != (b.kind == "player") {
		return false
	}
	if a.kind == "player" {
		return a.player == b.player
	}
	return a.obj == b.obj
}

// targetCandidateIn reports whether c names one of the chosen targets.
func targetCandidateIn(c targetCandidate, chosen []state.Target) bool {
	for _, t := range chosen {
		if t.IsPlayer == (c.kind == "player") && ((t.IsPlayer && t.Player == c.player) || (!t.IsPlayer && t.Obj == c.obj)) {
			return true
		}
	}
	return false
}

// targetCandidatesWithout returns pool less every candidate naming c's target.
func targetCandidatesWithout(pool []targetCandidate, c targetCandidate) []targetCandidate {
	out := make([]targetCandidate, 0, len(pool))
	for _, p := range pool {
		if !sameTargetCandidate(p, c) {
			out = append(out, p)
		}
	}
	return out
}

// announceShapesTargets reports whether a declaration's Announce$ value can
// change the declaration itself: one announced name is read by its
// ValidTgts$, TargetMin$ or TargetMax$, directly or through the face's SVar
// table (bodyReadsRef's transitive walk). Such a root cannot be judged before
// the cast reaches its announcement stage; any other Announce$ root (the
// announced value sizes a chain link or a cost) is judged like a plain one.
// The name match is a substring test, which only errs toward "shapes it" --
// the old, always-unjudged reading.
func announceShapesTargets(e *Engine, id state.ObjID, sa *cards.SA) bool {
	ann := strings.TrimSpace(sa.ParamStr(cards.PKAnnounce))
	if ann == "" {
		return false
	}
	var svars map[string]string
	if o := e.G.Obj(id); o != nil && o.Face() != nil {
		svars = o.Face().SVars
	}
	tp := effects.TargetsOf(sa)
	for name := range strings.SplitSeq(ann, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		match := func(s string) bool { return strings.Contains(s, name) }
		if bodyReadsRef(tp.ValidTgts, svars, 0, match) ||
			bodyReadsRef(tp.Min.Text, svars, 0, match) ||
			bodyReadsRef(tp.Max.Text, svars, 0, match) {
			return true
		}
	}
	return false
}
