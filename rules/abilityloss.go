package rules

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// "Loses all abilities" (CR 613.1f), beyond keywords.
//
// A layer-6 RemoveAbilities effect (Forge's RemoveAllAbilities$ True: the
// Witness Protection / Frogify / Darksteel Mutation Aura statics, Humility,
// the one-shot Animate deliveries of Turn to Frog and its family) removes
// EVERY ability of the permanents it applies to -- triggered, activated, mana
// and static abilities as well as the keywords the layer-6 keyword walk
// already clears. Two halves implement that, and both read the same effects:
//
//   - the affected permanent's PRINTED abilities do not exist
//     (printedAbilitiesGone, the gate every printed-face scan shares with CR
//     708.8's face-down rule), and an ability another effect GRANTED it
//     survives only when that grant is not older than the removal
//     (grantLostTo: layer 6 applies in timestamp order, so a later grant is
//     applied on top of the removal and an earlier one is removed with the
//     rest);
//   - the continuous effects the permanent's own static abilities generate
//     stop applying. That half lives inside active() (pruneLostSourceAbilities):
//     every reader of the list -- the layer walks, the granted-ability and
//     granted-trigger collectors, the may-play and land-drop rules
//     modifications -- then sees the list without them, with no per-reader
//     gate to forget.
//
// CR 613.6 bounds the second half: an effect that started to apply in an
// earlier layer keeps applying even though the ability generating it is
// removed in layer 6. The static scan records each static ability's first
// layer on the effects it emits (state.ContinuousEffect.SourceAbilityLayer):
// one beginning in layers 1-5 is never dropped; one beginning in layer 6 is
// dropped only by a removal with an EARLIER timestamp (the removal applied
// first, so the effect never began -- which is also what orders two removers
// against each other); one beginning in layer 7, or a rules modification
// that is no layer effect at all, is dropped by any removal.

// ruleModAbilityLayer is the SourceAbilityLayer of a static-generated rules
// modification that is not a layer effect (Layer zero: MayPlay,
// AdjustLandPlays, a trigger or SVar grant): past every real layer, so any
// removal of the source's abilities ends it.
const ruleModAbilityLayer = LPT + 1

// markSourceAbilityLayer stamps the effects one static ability emitted with
// the first layer that ability applies in (see the file comment).
func markSourceAbilityLayer(emitted []ContinuousEffect) {
	first := ruleModAbilityLayer
	for i := range emitted {
		if l := emitted[i].Layer; l != 0 && l < first {
			first = l
		}
	}
	for i := range emitted {
		if emitted[i].Layer == 0 {
			emitted[i].SourceAbilityLayer = ruleModAbilityLayer
		} else {
			emitted[i].SourceAbilityLayer = first
		}
	}
}

// removalApplies reports whether the ability-removing effect r applies to the
// battlefield permanent o, whose finished layer-4 type list is types. It is
// the layer-6 walk's own applicability test (derivedCompute): the shared
// matcher, then the AffectedZone$ qualifier.
func (e *Engine) removalApplies(r *ContinuousEffect, o *state.Object, types []string) bool {
	if o.Zone != state.ZBattlefield {
		return false
	}
	if types == nil {
		// Bound even when empty, as derivedCompute binds it: a nil list is
		// SpecContext's "unbound, read the printed face".
		types = []string{}
	}
	if !e.matchesWithChars(r, o.ID, types, nil, 0) {
		return false
	}
	if r.AffectedZone != "" && !r.MayPlay {
		if zones, all, ok := effects.ParseZones(r.AffectedZone); !ok || (!all && !slices.Contains(zones, state.ZBattlefield)) {
			return false
		}
	}
	return true
}

// sourceLoss is one static source's verdict in pruneLostSourceAbilities:
// whether some live removal applies to it, and the earliest such timestamp.
type sourceLoss struct {
	id    state.ObjID
	lost  bool
	first uint32
}

// pruneLostSourceAbilities drops from active()'s freshly assembled list the
// static-generated effects whose source has lost all abilities (see the file
// comment). list is in CR 613 order, so its RemoveAbilities effects are met in
// timestamp order; a remover that an earlier one silenced is not live and
// removes nothing. The survivors keep their order. Called only when the list
// holds a remover at all, so a board with none pays nothing.
//
// It runs inside activeBuild, before the list is published: applicability is
// read through typeCharacteristicsActive over this same list (layer 4 is
// settled before layer 6, and no layer-4 effect is ever dropped here), never
// through Derived.
func (e *Engine) pruneLostSourceAbilities(list []ContinuousEffect) []ContinuousEffect {
	var removerBuf [8]int
	removers := removerBuf[:0]
	for i := range list {
		if list[i].Layer == LAbilities && list[i].RemoveAbilities {
			removers = append(removers, i)
		}
	}
	if len(removers) == 0 {
		return list
	}
	var deadBuf [8]bool
	dead := deadBuf[:0]
	if len(removers) > len(deadBuf) {
		dead = make([]bool, 0, len(removers))
	}
	dead = dead[:len(removers)]
	// earliest returns the earliest timestamp among the live removers
	// list[removers[:upto]] that apply to source.
	earliest := func(source state.ObjID, upto int) (uint32, bool) {
		o := e.G.Obj(source)
		if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
			return 0, false
		}
		var types []string
		typed := false
		for k := 0; k < upto; k++ {
			if dead[k] {
				continue
			}
			r := &list[removers[k]]
			if !typed {
				types, typed = e.typeCharacteristicsActive(list, source, 0), true
			}
			if e.removalApplies(r, o, types) {
				// Timestamp order: the first hit is the earliest.
				return r.Timestamp, true
			}
		}
		return 0, false
	}
	// A remover that is itself a static ability beginning in layer 6 is
	// silenced by an EARLIER live remover applying to its source; it is
	// never silenced by itself or a later one (CR 613.6).
	for k, ri := range removers {
		r := &list[ri]
		if r.SourceAbilityLayer != LAbilities {
			continue
		}
		if ts, ok := earliest(r.Source, k); ok && ts < r.Timestamp {
			dead[k] = true
		}
	}
	var lossBuf [8]sourceLoss
	losses := lossBuf[:0]
	lossOf := func(source state.ObjID) (uint32, bool) {
		for i := range losses {
			if losses[i].id == source {
				return losses[i].first, losses[i].lost
			}
		}
		ts, ok := earliest(source, len(removers))
		losses = append(losses, sourceLoss{id: source, lost: ok, first: ts})
		return ts, ok
	}
	kept := list[:0]
	next := 0
	for i := range list {
		ce := &list[i]
		drop := false
		isRemover := next < len(removers) && removers[next] == i
		switch {
		case isRemover:
			drop = dead[next]
		case ce.SourceAbilityLayer < LAbilities:
			// A registered effect (zero), or a static ability that began
			// applying before layer 6.
		case ce.SourceAbilityLayer == LAbilities:
			ts, ok := lossOf(ce.Source)
			drop = ok && ts < ce.Timestamp
		default:
			_, drop = lossOf(ce.Source)
		}
		if isRemover {
			next++
		}
		if !drop {
			if len(kept) != i {
				kept = append(kept, *ce)
			} else {
				kept = kept[:i+1]
			}
		}
	}
	clear(list[len(kept):])
	return kept
}

// abilityLoss reports whether a live "loses all abilities" effect applies to
// the battlefield permanent o, and the latest such effect's timestamp. It
// reads active()'s published list, in which every remover still present is
// live (pruneLostSourceAbilities dropped the silenced ones). A board with no
// remover answers from the per-build summary alone.
//
// During a layer scan (active() mid-build) it stands down and reports no
// loss, the same stand-down matchesSpec's Derived bind takes.
func (e *Engine) abilityLoss(o *state.Object) (uint32, bool) {
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || e.activeDepth != 0 || !e.abilityLossPossible() {
		return 0, false
	}
	act := e.active()
	if !e.activeSummaryOf(act).hasRemoveAbilities {
		return 0, false
	}
	var types []string
	typed := false
	var latest uint32
	lost := false
	for i := range act {
		r := &act[i]
		if r.Layer != LAbilities || !r.RemoveAbilities {
			continue
		}
		if !typed {
			types, typed = e.typeCharacteristicsActive(act, o.ID, 0), true
		}
		if e.removalApplies(r, o, types) {
			lost = true
			if r.Timestamp > latest {
				latest = r.Timestamp
			}
		}
	}
	return latest, lost
}

// printedAbilitiesLost reports whether o's printed abilities are removed by a
// "loses all abilities" effect.
func (e *Engine) printedAbilitiesLost(o *state.Object) bool {
	_, lost := e.abilityLoss(o)
	return lost
}

// printedAbilitiesGone is the gate every printed-face scan shares: the
// object's printed triggered, activated, mana and static abilities do not
// exist while it is face down (CR 708.8, faceDownPrintedHides) or while a
// "loses all abilities" effect applies to it (CR 613.1f).
func (e *Engine) printedAbilitiesGone(o *state.Object) bool {
	return e.faceDownPrintedHides(o) || e.printedAbilitiesLost(o)
}

// grantLostTo reports whether an ability the continuous effect ce grants to o
// is removed by a "loses all abilities" effect applying to o: layer 6 applies
// in timestamp order, so the grant survives exactly when no such removal is
// newer than it (a tie applies the removal first, compareContinuousPtr).
func (e *Engine) grantLostTo(ce *ContinuousEffect, o *state.Object) bool {
	ts, lost := e.abilityLoss(o)
	return lost && ts > ce.Timestamp
}

// abilityLossProof is the sticky proof that active() can hold no "loses all
// abilities" effect, so the gates above answer without building active() --
// the trigger walk and the replacement dispatch run once per emitted event,
// and almost no match carries such an effect (trigger_grantfree.go's
// reasoning and shape). A remover reaches active() only from a registered
// effect (AddContinuous, or a direct registry write a fixture makes) or from
// the static scan, every face of which is a face of some object: its card's,
// its CopyFace (built from another object's face) or a merged card's. So
// while no registered effect removes abilities and no object's faces name
// RemoveAllAbilities$ on a static or in an SVar body (a static granted from
// one), there is none. Objects are checked as they are appended; any hit ends
// the proof for the rest of the match. Clone and the look-back observer copy
// it.
type abilityLossProof struct {
	seen    bool
	objs    int // e.G.Objs[:objs] checked
	contPtr *ContinuousEffect
	contLen int // e.continuous header last checked
}

// abilityLossPossible reports whether some active() entry may be a "loses
// all abilities" effect (false only while the proof holds).
func (e *Engine) abilityLossPossible() bool {
	pr := &e.lossProof
	if pr.seen {
		return true
	}
	if n := len(e.continuous); n != pr.contLen || (n > 0 && &e.continuous[0] != pr.contPtr) {
		for i := range e.continuous {
			if e.continuous[i].RemoveAbilities {
				pr.seen = true
				return true
			}
		}
		pr.contLen, pr.contPtr = n, nil
		if n > 0 {
			pr.contPtr = &e.continuous[0]
		}
	}
	for ; pr.objs < len(e.G.Objs); pr.objs++ {
		o := &e.G.Objs[pr.objs]
		if faceMayRemoveAbilities(o.CopyFace) {
			pr.seen = true
			return true
		}
		if o.Card != nil {
			for _, f := range o.Card.Faces {
				if faceMayRemoveAbilities(f) {
					pr.seen = true
					return true
				}
			}
		}
	}
	return false
}

func faceMayRemoveAbilities(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		if st.HasParam(cards.PKRemoveAllAbilities) {
			return true
		}
	}
	for _, v := range f.SVars {
		if strings.Contains(v, "RemoveAllAbilities") {
			return true
		}
	}
	return false
}
