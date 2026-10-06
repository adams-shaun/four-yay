package rules

import (
	"fmt"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Trigger-walk zone skip.
//
// checkFaceTriggers visits every object in every zone on every emitted event,
// libraries included, and for the overwhelming majority of those objects the
// visit does nothing: the printed triggers cannot fire from a library, hand,
// graveyard or exile, and no granted trigger reaches them. This file lets the
// LIVE walk skip a whole hidden-ish zone (library, hand, graveyard, exile of
// one player) when no object in it can do anything for any event, without
// touching the objects themselves.
//
// Why a skipped object is a provable no-op. For an object o in zone Z that
// is NOT one of the event's own referents (ev.Obj, ev.IDs, ev.Pairs -- the
// must-visit set, always walked in place), the walk body can act only
// through:
//
//   - a printed trigger reaching triggerMatches, whose zoneGate admits the
//     source only when zoneSpecContains(TriggerZones$ or ActiveZones$ or the
//     "Battlefield" default, o.Zone) -- every other zoneGate admission
//     requires source == ev.Obj, which is must-visit;
//   - the Phase$ diagnostic, which is zone-independent: a face carrying an
//     unresolvable Phase$ counts as live in every zone;
//   - an unlocked Room's other face or a mutated pile's under-cards: any
//     Unlocked or merged object counts as live;
//   - the granted keyword walks (Ward, Dethrone, Training, Mentor, Afflict,
//     Conspire, Demonstrate, Exploit, Offspring), each gated on the object
//     being ev.Obj, in ev.IDs or in ev.Pairs -- must-visit -- and cumulative
//     upkeep, gated on o.Zone == Battlefield (never a summarized zone);
//   - a static-granted trigger (grantedStatics): the skip is off for any
//     event with at least one observing grant.
//
// objectTriggerHot is the per-object "live in its zone" test: the union over
// EVERY face of o.Card (so a face flip in place changes nothing) plus
// CopyFace. A zone is COLD when no object in it is hot.
//
// Why the per-zone summary stays true. A summary records the zone's id list
// at the moment it was classified. It is reused only while the live list
// still equals it, or (cold only) while the live list is the recorded one
// with ids removed anywhere plus new ids appended -- the new ids are then
// classified individually, and a subset of cold objects is cold. The one
// remaining input is an object's own trigger-relevant fields (Card, CopyFace,
// Unlocked, MergedCards, Zone) changing in place. Every such write is in
// events.Apply and keyed by the event's Obj, IDs or Pairs, so before each
// live walk trigZonesCatchUp scans every event logged since the previous
// walk and drops the summary of whichever summarized zone each referenced
// object now sits in. (Engine setup's direct SetZone calls are zone-list
// changes the comparison already sees.)
//
// Order: the walk still runs player by player, zone by zone, in list order;
// a cold zone passes only its must-visit ids, in their list order, so the
// sequence of walk-body effects is exactly the unskipped walk's.
//
// trigZoneSkipVerify (the rules test binary, or derivedMemoVerifyFlag at link
// time) walks every skipped object anyway and panics if the visit queued a
// trigger or a Phase$ diagnostic -- the empirical half of the argument above.

var trigZoneSkipVerify = derivedMemoVerifyFlag != ""

// trigZoneSlots is how many zones per player carry a summary. The
// battlefield joined the four hidden-ish zones so a battlefield holding
// thousands of vanilla tokens beside one trigger source walks only the
// sources (see objectTriggerHotIn): vanilla tokens are cold there too. The
// stack and the command zone joined last, on the same argument: a
// non-referent source there acts only through a printed line whose zone spec
// names that zone, so a spell or ability on the stack no longer forces every
// Priority walk to visit it. The stack list is shared (Game.Zone ignores the
// seat for it), and the walk reads it on the first living seat only, so its
// summary lives in that seat's slot.
const trigZoneSlots = 7

// trigZoneSlot maps a zone to its summary slot, or -1 for a zone the walk
// never summarizes (no walked zone today: objectWalkZones are all slotted).
func trigZoneSlot(z state.Zone) int {
	switch z {
	case state.ZLibrary:
		return 0
	case state.ZHand:
		return 1
	case state.ZBattlefield:
		return 2
	case state.ZGraveyard:
		return 3
	case state.ZExile:
		return 4
	case state.ZStack:
		return 5
	case state.ZCommand:
		return 6
	}
	return -1
}

var trigZoneSlotZones = [trigZoneSlots]state.Zone{state.ZLibrary, state.ZHand, state.ZBattlefield, state.ZGraveyard, state.ZExile, state.ZStack, state.ZCommand}

type trigZoneSummary struct {
	ids []state.ObjID
	// live is the zone's list HEADER as it was read when the summary was
	// last confirmed (not a copy): see sameZoneList. Holding it keeps that
	// backing array alive, so its address cannot be reused for another list.
	live []state.ObjID
	// hotIDs is the subset of ids whose objects can act on some event from
	// this zone, in ids order (see objectTriggerHotIn). The live walk visits
	// only these when the zone is hot and no event referent sits in it, so a
	// battlefield holding thousands of vanilla tokens beside one trigger
	// source costs the sources, not the tokens. nil while the zone is cold or
	// unclassified.
	hotIDs []state.ObjID
	// hotSigs is parallel to hotIDs: each hot object's exact event
	// signature (objectTrigSig, trigger_kinds.go). A walk with the kind
	// filter on visits only the hot objects whose signature admits the event.
	hotSigs []trigSig
	// union is the OR of hotSigs (trigger_plan.go reads it).
	union trigSig
	hot   bool
	valid bool
}

// faceTriggerZones is the bit set (by trigZoneSlot) of the summarized zones
// from which at least one of f's printed triggers can function, or every bit
// for a face carrying an unresolvable Phase$ (its diagnostic is emitted from
// any zone). Pure syntax: a configured face's answer is computed once with
// the shared compiled text (walkFaceFacts.trigZones); any other face's is
// cached per engine like triggerEventMasks.
func (e *Engine) faceTriggerZones(f *cards.Face) uint8 {
	if f == nil || len(f.Triggers) == 0 {
		return 0
	}
	if ff := e.walkFaceFactsOf(f); ff != nil && ff.triggersCurrent(f) {
		return ff.trigZones
	}
	if m, ok := e.trigFaceZones[f]; ok {
		return m
	}
	m := computeFaceTriggerZones(f, func(spec string) bool { return e.parsedPhaseSpec(spec).valid })
	if e.trigFaceZones == nil {
		e.trigFaceZones = make(map[*cards.Face]uint8)
	}
	e.trigFaceZones[f] = m
	return m
}

// computeFaceTriggerZones is faceTriggerZones' pure computation; valid
// reports whether a Phase$ spec parses (phaseSpecValid, or the engine's
// memoised parse of the same function).
func computeFaceTriggerZones(f *cards.Face, valid func(string) bool) uint8 {
	var m uint8
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if spec := t.ParamStr(cards.PKPhase); strings.TrimSpace(spec) != "" && !valid(spec) {
			m = 1<<trigZoneSlots - 1
			break
		}
		// zoneGate's spec resolution for a source that is not the event's
		// own object (the only kind a skip ever passes over).
		spec := t.ParamStr(cards.PKTriggerZones)
		if spec == "" {
			spec = t.ParamStr(cards.PKActiveZones)
		}
		if spec == "" {
			spec = "Battlefield"
		}
		for s, z := range trigZoneSlotZones {
			if zoneSpecContains(spec, z) {
				m |= 1 << s
			}
		}
	}
	return m
}

// phaseSpecValid is parsedPhaseSpec's validity bit without an engine memo.
func phaseSpecValid(spec string) bool { return parsePhaseSpec(spec).valid }

// A summary classifies each listed object by the slot of its OWN Zone field,
// not the list's: zoneGate admits a non-referent source by o.Zone, so the two
// agree by construction (and agree with the list in every event-built state;
// a fixture that lists one object in two zones keeps the full walk's answer).
//
// objectTriggerHotIn is objectTriggerHot for a KNOWN summary slot: hot when
// the object can act from the zone that slot names. slot < 0 (a zone with no
// summary, or an unknown zone) is always hot, the conservative direction.
//
// The zone-specific bit is load-bearing once the battlefield carries a
// summary: a card whose only trigger functions from the battlefield is cold
// in a library (its zoneGate would reject it there), so the library stays
// skippable, while it is hot on the battlefield and the live walk visits it.
func (e *Engine) objectTriggerHotIn(o *state.Object, slot int) bool {
	if o == nil || o.Face() == nil {
		return false
	}
	if o.RoomOtherDoorUnlocked() || len(o.MergedCards) > 0 {
		return true
	}
	if slot < 0 {
		return true
	}
	bit := uint8(1) << slot
	if o.CopyFace != nil && e.faceTriggerZones(o.CopyFace)&bit != 0 {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if e.faceTriggerZones(f)&bit != 0 {
				return true
			}
		}
	}
	return false
}

// zeroInterestEvent reports whether ev's kind carries no trigger interest and
// no granted keyword walk observes it: the kind the zero-interest no-op memo
// (checkFaceTriggers) is keyed to.
func zeroInterestEvent(kind events.Kind, evAll bool, evMask cards.TriggerInterest) bool {
	return !evAll && evMask == cards.TriggerInterestAny && !grantedKeywordTriggerEvent(kind) &&
		kind != events.TargetsChosen && kind != events.StepChange
}

func (e *Engine) trigZoneInvalidateAll() {
	e.trigZoneGen++
	for i := range e.trigZones {
		e.trigZones[i].valid = false
	}
}

// trigZoneTouch re-checks one event referent against the summaries of the
// zone slot it now sits in. A summary describes its list's ids, which
// objects among them are hot (hotIDs) and each hot object's kind mask
// (hotSigs); an in-place write can change only the touched object's own
// classification. So a summary stays exact while the object's current
// classification is the one it records -- hot exactly when it is listed in
// hotIDs, with exactly the recorded signature -- and a summary whose list
// does not hold the object at all (another seat's) does not describe it (a
// list change is caught by the list comparison). Anything else drops the
// summary.
func (e *Engine) trigZoneTouch(id state.ObjID) {
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	s := trigZoneSlot(o.Zone)
	if s < 0 {
		return
	}
	hot, classified := false, false
	var sig trigSig
	for i := s; i < len(e.trigZones); i += trigZoneSlots {
		z := &e.trigZones[i]
		if !z.valid {
			continue
		}
		if !classified {
			hot = e.objectTriggerHotIn(o, s)
			if hot {
				sig = e.objectTrigSig(o)
			}
			classified = true
		}
		at := slices.Index(z.hotIDs, id)
		if hot && at < 0 && !slices.Contains(z.ids, id) {
			// Another seat's list (a hot object is listed in hotIDs exactly
			// when it is in ids): this summary does not describe the object.
			continue
		}
		if hot != (at >= 0) || (hot && z.hotSigs[at] != sig) {
			z.valid = false
			e.trigZoneGen++
		}
	}
}

// trigZonesCatchUp drops the summary of every zone an object referenced by
// an event logged since the previous call now sits in (the in-place half of
// the argument above).
func (e *Engine) trigZonesCatchUp() {
	n := len(e.L.Events)
	if n < e.trigZonesEp {
		e.trigZoneInvalidateAll()
		e.trigZonesEp = n
		return
	}
	if len(e.trigZones) == 0 {
		// Nothing summarized yet (a fresh engine, or a clone of one that
		// had no summaries): nothing to drop, so skip the whole history.
		e.trigZonesEp = n
		return
	}
	for i := e.trigZonesEp; i < n; i++ {
		ev := &e.L.Events[i]
		free := touchFreeKinds.has(ev.Kind)
		if free && !trigZoneSkipVerify {
			continue
		}
		gen := e.trigZoneGen
		e.trigZoneTouch(ev.Obj)
		for _, id := range ev.IDs {
			e.trigZoneTouch(id)
		}
		for _, pr := range ev.Pairs {
			e.trigZoneTouch(pr[0])
			e.trigZoneTouch(pr[1])
		}
		if free && e.trigZoneGen != gen {
			panic(fmt.Sprintf("rules: touch-free %v event changed a trigger zone summary", ev.Kind))
		}
	}
	e.trigZonesEp = n
}

// sameZoneList reports whether cur is the very list header a summary
// recorded: same length over the same backing array. A zone list is written
// only by state.Game.SetZone, and every writer installs either a FRESH array
// (events' remove, a shuffle, Clone, genesis) or an append onto the current
// header, which writes past its length and so changes the length; no writer
// stores into a live list below its length. So an identical header holds
// identical ids, and the O(len) compare is needed only when the header moved.
// The recorded header pins its array (trigZoneSummary.live), so the address
// cannot be recycled for a different list while the summary holds it.
func sameZoneList(rec, cur []state.ObjID) bool {
	if len(rec) != len(cur) {
		return false
	}
	return len(cur) == 0 || &rec[0] == &cur[0]
}

// trigZoneCold reports whether zone (p, z) -- whose live list is cur -- holds
// no hot object, refreshing its summary as needed.
func (e *Engine) trigZoneCold(p state.PlayerID, slot int, cur []state.ObjID) bool {
	i := int(p)*trigZoneSlots + slot
	if i >= len(e.trigZones) {
		e.trigZones = growZoneSummaries(e.trigZones, max(i+1, len(e.G.Players)*trigZoneSlots))
		e.trigZoneGen++
	}
	s := &e.trigZones[i]
	if s.valid && sameZoneList(s.live, cur) {
		if trigZoneSkipVerify && !slices.Equal(s.ids, cur) {
			panic(fmt.Sprintf("rules: trigger zone summary (seat %d, slot %d) kept its header but the list changed in place", p, slot))
		}
		return !s.hot
	}
	// Every path below writes the summary (at least its live header).
	e.trigZoneGen++
	if s.valid && slices.Equal(s.ids, cur) {
		s.live = cur
		return !s.hot
	}
	s.live = cur
	// Append-only fast path: the live list is the recorded one with ids
	// appended at the end. This is the mass-token-creation shape -- the token
	// is appended to the battlefield list and no recorded index moves -- and
	// the prefix compare is a cheap integer scan, far below re-testing every
	// object's face. A removal anywhere breaks the prefix and falls through to
	// the general rebuild below, so a stale or shifted list is never trusted.
	if s.valid && len(cur) > len(s.ids) && len(s.ids) > 0 && slices.Equal(s.ids, cur[:len(s.ids)]) {
		// The recorded prefix (and therefore its hot subset) is unchanged;
		// classify only the appended tail.
		for _, id := range cur[len(s.ids):] {
			if o := e.G.Obj(id); o != nil && e.objectTriggerHotIn(o, trigZoneSlot(o.Zone)) {
				s.hot = true
				sig := e.objectTrigSig(o)
				s.hotIDs = append(s.hotIDs, id)
				s.hotSigs = append(s.hotSigs, sig)
				s.union = s.union.or(sig)
			}
		}
		s.ids = append(s.ids[:0], cur...)
		return !s.hot
	}
	from := 0
	if s.valid && !s.hot {
		// cur[:k] a subsequence of the recorded cold list (removals
		// anywhere) is cold; classify only the unmatched tail.
		j, k := 0, len(cur)
		for idx, id := range cur {
			for j < len(s.ids) && s.ids[j] != id {
				j++
			}
			if j == len(s.ids) {
				k = idx
				break
			}
			j++
		}
		from = k
	}
	// A cold recorded list has no hot ids, so the prefix contributes none and
	// the hot subset is the tail's. A previously-hot list must be rebuilt
	// whole (from == 0).
	hotIDs := s.hotIDs[:0]
	hotSigs := s.hotSigs[:0]
	var union trigSig
	hot := false
	for _, id := range cur[from:] {
		if o := e.G.Obj(id); o != nil && e.objectTriggerHotIn(o, trigZoneSlot(o.Zone)) {
			sig := e.objectTrigSig(o)
			hot = true
			hotIDs = append(hotIDs, id)
			hotSigs = append(hotSigs, sig)
			union = union.or(sig)
		}
	}
	s.ids = append(s.ids[:0], cur...)
	s.hotIDs, s.hotSigs, s.union, s.hot, s.valid = hotIDs, hotSigs, union, hot, true
	return !hot
}

// trigZoneHotIDs returns the classified hot subset of zone (p, slot)'s list
// and its parallel kind masks. It must be called after trigZoneCold has
// classified the zone; a cold zone (or one outside the summary range) has
// none.
func (e *Engine) trigZoneHotIDs(p state.PlayerID, slot int) ([]state.ObjID, []trigSig) {
	i := int(p)*trigZoneSlots + slot
	if i < 0 || i >= len(e.trigZones) {
		return nil, nil
	}
	return e.trigZones[i].hotIDs, e.trigZones[i].hotSigs
}

// trigMustVisit reports whether id is one of the event's referents (Obj, IDs,
// Pairs), which are walked in place even in a cold zone.
func trigMustVisit(ev events.Event, id state.ObjID) bool {
	if id == ev.Obj {
		return true
	}
	if slices.Contains(ev.IDs, id) {
		return true
	}
	for _, pr := range ev.Pairs {
		if pr[0] == id || pr[1] == id {
			return true
		}
	}
	return false
}

// forEachTriggerObject is forEachObject for the live trigger walk: the same
// players, zones, order and snapshot/re-entry discipline, with a cold
// summarized zone reduced to its must-visit ids. skip false walks everything
// (forEachObject's exact behaviour). verify, when non-nil, is called for
// every skipped id in its list position.
//
// kindOnly narrows a hot zone further, to the hot objects whose exact
// signature (hotSigs, trigger_kinds.go) admits ev, plus the referents: a hot
// non-referent object outside that subset has no printed line triggerMatches
// can admit for ev, so its visit reaches nothing a cold object's would not --
// the granted walks it can still reach are the referent-gated ones the
// cold-zone skip already relies on.
func (e *Engine) forEachTriggerObject(ev *events.Event, skip, kindOnly bool, fn func(id state.ObjID), verify func(id state.ObjID)) {
	e.trigWalkUnionOK = false
	if !skip {
		e.forEachObject(fn)
		return
	}
	e.trigZonesCatchUp()
	planHeld := kindOnly && e.trigPlanHolds()
	if planHeld {
		e.trigWalkUnion, e.trigWalkUnionOK = e.trigPlan.union, true
		if trigNoReferent(ev) && !e.trigPlan.union.admits(ev, e.G.Step) &&
			!(ev.Kind == events.StepChange && e.stepWalksBattlefield()) {
			if verify == nil {
				return
			}
			// Verify mode: walk anyway; nothing may be visited.
			fn = func(id state.ObjID) {
				panic(fmt.Sprintf("rules: trigger walk plan skipped a %v walk that visits obj %d", ev.Kind, id))
			}
		}
	}
	// refSlots: the summarized zones some referent sits in now. A cold zone
	// outside it has no must-visit id and is skipped without a scan
	// (events.Apply moves an object's Zone field and its list membership
	// together).
	var refSlots uint8
	ref := func(id state.ObjID) {
		if o := e.G.Obj(id); o != nil {
			if s := trigZoneSlot(o.Zone); s >= 0 {
				refSlots |= 1 << s
			}
		}
	}
	ref(ev.Obj)
	for _, id := range ev.IDs {
		ref(id)
	}
	for _, pr := range ev.Pairs {
		ref(pr[0])
		ref(pr[1])
	}
	e.foreachDepth++
	defer func() { e.foreachDepth-- }()
	buf := e.foreachBuf
	if e.foreachDepth > 1 {
		buf = nil
	}
	// A walk the plan did not answer records the next plan as it validates
	// each slot (trigger_plan.go): the slot's confirmed header and its
	// summary's signature union.
	pl := &e.trigPlan
	rec := kindOnly && !planHeld && e.foreachDepth == 1 // a nested walk never records
	recN := 0
	var recUnion trigSig
	if rec {
		pl.ok = false
	}
	for si, p := range e.G.AliveFrom(0) {
		for _, z := range objectWalkZones {
			if z == state.ZStack && si != 0 {
				continue
			}
			cur := e.G.Zone(z, p)
			slot := trigZoneSlot(z)
			cold := slot >= 0 && e.trigZoneCold(p, slot, cur)
			if rec {
				if slot < 0 || recN >= trigPlanSlots {
					rec = false
				} else {
					recUnion = recUnion.or(e.trigZones[int(p)*trigZoneSlots+slot].union)
					pl.heads[recN] = trigHeadOf(p, cur)
					recN++
				}
			}
			// A StepChange into an upkeep or end step cannot use the plain
			// battlefield skip: checkGrantedCumulativeUpkeepTriggers and
			// checkGrantedAtEOTTriggers synthesize a battlefield trigger from
			// state the face hot test cannot see (a DERIVED keyword, a copied
			// AtEOTTrig$ body), so the battlefield also visits every object
			// stepGrantMay admits. Every other granted trigger the battlefield
			// summary could hide is gated on the object being an event
			// referent. Hidden-ish zones keep their skip -- both synthesized
			// triggers function only from the battlefield.
			stepFull := slot == trigZoneSlot(state.ZBattlefield) && ev.Kind == events.StepChange && e.stepWalksBattlefield()
			if stepFull && kindOnly {
				// The step's full battlefield walk, narrowed exactly: besides
				// the selected hot objects only an object one of the two
				// synthesized step triggers can reach is visited -- each of
				// checkGrantedCumulativeUpkeepTriggers and
				// checkGrantedAtEOTTriggers returns before any work for an
				// object stepGrantMay rules out (StepChange has no referent).
				hotIDs, hotSigs := e.trigZoneHotIDs(p, slot)
				buf = e.trigHotMerge(buf, ev, cur, hotIDs, hotSigs, true, true, p, slot, fn, verify)
				if verify != nil {
					continue
				}
			} else if slot >= 0 && !stepFull && cold {
				if verify != nil {
					buf = append(buf[:0], cur...)
					for _, id := range buf {
						if trigMustVisit(*ev, id) {
							fn(id)
						} else {
							verify(id)
						}
					}
					continue
				}
				buf = buf[:0]
				if refSlots&(1<<slot) == 0 {
					continue
				}
				for _, id := range cur {
					if trigMustVisit(*ev, id) {
						buf = append(buf, id)
					}
				}
			} else if slot >= 0 && !stepFull && refSlots&(1<<slot) == 0 {
				// Hot summarized zone with no event referent in it. Only the
				// classified hot objects can act (and, under kindOnly, only
				// those whose kind mask has ev's kind), so visit those and skip
				// the rest; order is ids order because hotIDs is a subsequence
				// of the live list. In verify mode the skipped objects are run
				// through the verifier instead, so the hot-subset skip is held
				// to the same empirical contract the cold-zone skip is. A
				// referent would have to be walked in place, so its slot falls
				// through to the merge below.
				hotIDs, hotSigs := e.trigZoneHotIDs(p, slot)
				if verify == nil {
					for i, id := range hotIDs {
						if !kindOnly || hotSigs[i].admits(ev, e.G.Step) {
							fn(id)
						}
					}
					continue
				}
				buf = e.trigHotMerge(buf, ev, cur, hotIDs, hotSigs, kindOnly, false, p, slot, fn, verify)
				continue
			} else if slot >= 0 && !stepFull {
				// Hot summarized zone WITH an event referent in it: the
				// referents are walked in place and, of everything else, only
				// the selected hot objects can act -- the two skips above
				// combined. The visit set is snapshotted first, in list order
				// (hotIDs is a subsequence of cur, so one merge pass keeps the
				// order), exactly as the full walk snapshots the list.
				hotIDs, hotSigs := e.trigZoneHotIDs(p, slot)
				buf = e.trigHotMerge(buf, ev, cur, hotIDs, hotSigs, kindOnly, false, p, slot, fn, verify)
				if verify != nil {
					continue
				}
			} else {
				buf = append(buf[:0], cur...)
			}
			for _, id := range buf {
				fn(id)
			}
		}
	}
	if e.foreachDepth <= 1 {
		e.foreachBuf = buf
	}
	if rec {
		// Every living seat's walked zones went through trigZoneCold above,
		// so the recorded headers and unions describe the whole board.
		pl.n, pl.union, pl.gen, pl.ok = recN, recUnion, e.trigZoneGen, true
	}
	if kindOnly && pl.ok && pl.gen == e.trigZoneGen {
		e.trigWalkUnion, e.trigWalkUnionOK = pl.union, true
	}
}

// trigHotMerge snapshots the visit set of a hot summarized zone into buf, in
// list order: every referent, every hot object selected by kindOnly, and
// (step) every object stepGrantMay admits. In verify mode it instead walks
// the list itself, visiting the selected ids and verifying the rest.
func (e *Engine) trigHotMerge(buf []state.ObjID, ev *events.Event, cur, hotIDs []state.ObjID, hotSigs []trigSig,
	kindOnly, step bool, p state.PlayerID, slot int, fn, verify func(id state.ObjID)) []state.ObjID {
	if !step {
		if verify == nil && !trigHotMergeVerify {
			if out, ok := trigHotMergeRefs(e.G, buf, ev, cur, hotIDs, hotSigs, kindOnly, slot); ok {
				return out
			}
		} else {
			verifyTrigHotMergeRefs(e.G, ev, cur, hotIDs, hotSigs, kindOnly, p, slot)
		}
	}
	if verify != nil {
		buf = append(buf[:0], cur...)
		j := 0
		for _, id := range buf {
			sel := false
			if j < len(hotIDs) && hotIDs[j] == id {
				sel = !kindOnly || hotSigs[j].admits(ev, e.G.Step)
				j++
			}
			if sel || trigMustVisit(*ev, id) || (step && e.stepGrantMay(id)) {
				fn(id)
			} else {
				verify(id)
			}
		}
		return buf
	}
	buf = buf[:0]
	j := 0
	for _, id := range cur {
		if j < len(hotIDs) && hotIDs[j] == id {
			if !kindOnly || hotSigs[j].admits(ev, e.G.Step) || trigMustVisit(*ev, id) || (step && e.stepGrantMay(id)) {
				buf = append(buf, id)
			}
			j++
		} else if trigMustVisit(*ev, id) || (step && e.stepGrantMay(id)) {
			buf = append(buf, id)
		}
	}
	if j != len(hotIDs) {
		panic(fmt.Sprintf("rules: trigger zone summary (seat %d, slot %d) hot list is not a subsequence of the zone list", p, slot))
	}
	return buf
}

// stepGrantMay reports whether one of the two synthesized step triggers
// could act for id on the current step: the same leading gates
// checkGrantedCumulativeUpkeepTriggers (battlefield, upkeep, the derived
// keyword precheck) and checkGrantedAtEOTTriggers (battlefield, end step, a
// copied AtEOTTrig$ body) return on.
func (e *Engine) stepGrantMay(id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		return false
	}
	if atEOTTrigSteps.Has(e.G.Step) && o.AtEOTTrigBody != "" {
		return true
	}
	return cumulativeUpkeepSteps.Has(e.G.Step) && e.mayHaveDerivedKeywordH(id, kwhCumulativeUpkeep)
}

// stepWalksBattlefield reports whether a StepChange into the current step
// must consider every battlefield object (stepGrantMay): only the two
// synthesized step triggers the
// face hot test cannot see read a step change from a battlefield object, and
// each is gated on its own Phase$ before anything else (the cumulative-upkeep
// grant on Upkeep, checkGrantedCumulativeUpkeepTriggers; the AtEOT body on its
// end-of-turn phase, checkGrantedAtEOTTriggers). On every other step both
// return before any work, so the battlefield keeps its ordinary skip.
func (e *Engine) stepWalksBattlefield() bool {
	return cumulativeUpkeepSteps.Has(e.G.Step) || atEOTTrigSteps.Has(e.G.Step)
}

// trigSkipVerifier returns the verify callback checkFaceTriggers hands
// forEachTriggerObject in verify mode: it runs the real visit and panics if
// it queued anything.
func (e *Engine) trigSkipVerifier(ev events.Event, visit func(id state.ObjID), notes func() int) func(id state.ObjID) {
	return func(id state.ObjID) {
		np, nn := len(e.pendingTriggers), notes()
		visit(id)
		if len(e.pendingTriggers) != np || notes() != nn {
			panic(fmt.Sprintf("rules: trigger zone skip passed over obj %d (zone %v) that acts on %v event", id, e.G.Obj(id).Zone, ev.Kind))
		}
	}
}

// growZoneSummaries extends a zone-summary table to n entries: within its
// capacity (a recycled table, rules.Spare) the new entries are reset to an
// invalid summary that keeps its id lists' arrays, otherwise it allocates
// the whole range at once. A new entry is always invalid.
func growZoneSummaries[S any, PS interface {
	*S
	resetSummary()
}](t []S, n int) []S {
	if n <= cap(t) {
		old := len(t)
		t = t[:n]
		for i := old; i < n; i++ {
			PS(&t[i]).resetSummary()
		}
		return t
	}
	out := make([]S, n)
	copy(out, t)
	return out
}

func (s *trigZoneSummary) resetSummary() {
	*s = trigZoneSummary{ids: s.ids[:0], hotIDs: s.hotIDs[:0], hotSigs: s.hotSigs[:0]}
}

// copyTrigZones copies a parent engine's summaries into a clone's
// (recycled) table: the clone's board and log are the parent's at the clone
// boundary, so each summary describes the clone's same-content list. Its
// recorded header is the copy's own id array, which no zone list shares, so
// the clone's first look compares contents (an empty list matches an empty
// summary, correctly) and then records its own header.
func copyTrigZones(dst, src []trigZoneSummary) []trigZoneSummary {
	dst = growZoneSummaries(dst[:0], len(src))
	for i := range src {
		d, s := &dst[i], &src[i]
		d.ids = append(d.ids[:0], s.ids...)
		d.hotIDs = append(d.hotIDs[:0], s.hotIDs...)
		d.hotSigs = append(d.hotSigs[:0], s.hotSigs...)
		d.union = s.union
		d.live, d.hot, d.valid = d.ids, s.hot, s.valid
	}
	return dst
}
