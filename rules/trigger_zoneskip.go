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
// sources (see objectTriggerHotIn): vanilla tokens are cold there too.
const trigZoneSlots = 5

// trigZoneSlot maps a zone to its summary slot, or -1 for a zone that is
// always walked in full (stack, command, ...).
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
	}
	return -1
}

var trigZoneSlotZones = [trigZoneSlots]state.Zone{state.ZLibrary, state.ZHand, state.ZBattlefield, state.ZGraveyard, state.ZExile}

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
	// anyIDs is the subset of hotIDs that can act on an event with no
	// trigger interest at all (Priority, DecisionAsk, DecisionMade): see
	// objectTriggerAnyHot. In hotIDs order.
	anyIDs []state.ObjID
	hot    bool
	valid  bool
}

// faceTriggerZones is the bit set (by trigZoneSlot) of the summarized zones
// from which at least one of f's printed triggers can function, or every bit
// for a face carrying an unresolvable Phase$ (its diagnostic is emitted from
// any zone). Pure syntax, cached per engine like triggerEventMasks.
func (e *Engine) faceTriggerZones(f *cards.Face) uint8 {
	if f == nil || len(f.Triggers) == 0 {
		return 0
	}
	if m, ok := e.trigFaceZones[f]; ok {
		return m
	}
	var m uint8
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if spec := t.Params["Phase"]; strings.TrimSpace(spec) != "" && !e.parsedPhaseSpec(spec).valid {
			m = 1<<trigZoneSlots - 1
			break
		}
		// zoneGate's spec resolution for a source that is not the event's
		// own object (the only kind a skip ever passes over).
		spec := t.Params["TriggerZones"]
		if spec == "" {
			spec = t.Params["ActiveZones"]
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
	if e.trigFaceZones == nil {
		e.trigFaceZones = make(map[*cards.Face]uint8)
	}
	e.trigFaceZones[f] = m
	return m
}

// objectTriggerHot reports whether the trigger walk might do anything for o
// (other than as an event referent) where it sits now. Conservative: true
// for any object outside a summarized zone.
func (e *Engine) objectTriggerHot(o *state.Object) bool {
	return e.objectTriggerHotIn(o, trigZoneSlot(o.Zone))
}

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
	if o.Unlocked || len(o.MergedCards) > 0 {
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

// objectTriggerAnyHot reports whether a hot object may act on an event whose
// kind carries no trigger interest bit (eventTriggerInterest == 0, e.g.
// Priority): such an event reaches the per-face walk only through a face whose
// compiled interests include TriggerInterestAny (an Always, CounterAdded,
// LifeLostAll, unknown-mode or Phase$-bearing trigger), a face with no
// compiled row (its own mask may allow anything), or an unlocked or merged
// object (checkFaceTriggers' visit). Every face of the card and CopyFace
// count, like objectTriggerHotIn, so a face flip in place changes nothing.
func objectTriggerAnyHot(o *state.Object) bool {
	if o == nil {
		return false
	}
	if o.Unlocked || len(o.MergedCards) > 0 {
		return true
	}
	if faceAnyInterest(o.CopyFace) {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if faceAnyInterest(f) {
				return true
			}
		}
	}
	return false
}

func faceAnyInterest(f *cards.Face) bool {
	if f == nil || len(f.Triggers) == 0 {
		return false
	}
	interests, ok := f.CompiledTriggerInterests()
	return !ok || interests&cards.TriggerInterestAny != 0
}

// zeroInterestEvent reports whether a trigger walk over ev can use the
// summaries' anyIDs (forEachTriggerObject's anyOnly): ev's kind carries no
// trigger interest, and no granted keyword walk observes it.
func zeroInterestEvent(kind events.Kind, evAll bool, evMask cards.TriggerInterest) bool {
	return !evAll && evMask == cards.TriggerInterestAny && !grantedKeywordTriggerEvent(kind) &&
		kind != events.TargetsChosen && kind != events.StepChange
}

func (e *Engine) trigZoneInvalidateAll() {
	for i := range e.trigZones {
		e.trigZones[i].valid = false
	}
}

// trigZoneTouch re-checks one event referent against the summaries of the
// zone slot it now sits in. A summary describes its list's ids, which
// objects among them are hot (hotIDs) and which of those are any-hot
// (anyIDs); an in-place write can change only the touched object's own
// classification. So a summary stays exact while the object's current
// classification is the one it records -- hot exactly when it is listed in
// hotIDs, any-hot exactly when listed in anyIDs -- whether or not the
// object is in that seat's list at all (a non-hot object outside hotIDs is
// recorded correctly either way, and a list change is caught by the list
// comparison). Anything else drops the summary, as every touch used to.
func (e *Engine) trigZoneTouch(id state.ObjID) {
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	s := trigZoneSlot(o.Zone)
	if s < 0 {
		return
	}
	hot, anyHot, classified := false, false, false
	for i := s; i < len(e.trigZones); i += trigZoneSlots {
		z := &e.trigZones[i]
		if !z.valid {
			continue
		}
		if !classified {
			hot = e.objectTriggerHotIn(o, s)
			anyHot = hot && objectTriggerAnyHot(o)
			classified = true
		}
		if hot != slices.Contains(z.hotIDs, id) || (hot && anyHot != slices.Contains(z.anyIDs, id)) {
			z.valid = false
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
	for i := e.trigZonesEp; i < n; i++ {
		ev := &e.L.Events[i]
		e.trigZoneTouch(ev.Obj)
		for _, id := range ev.IDs {
			e.trigZoneTouch(id)
		}
		for _, pr := range ev.Pairs {
			e.trigZoneTouch(pr[0])
			e.trigZoneTouch(pr[1])
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
	}
	s := &e.trigZones[i]
	if s.valid && sameZoneList(s.live, cur) {
		if trigZoneSkipVerify && !slices.Equal(s.ids, cur) {
			panic(fmt.Sprintf("rules: trigger zone summary (seat %d, slot %d) kept its header but the list changed in place", p, slot))
		}
		return !s.hot
	}
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
			if o := e.G.Obj(id); e.objectTriggerHotIn(o, slot) {
				s.hot = true
				s.hotIDs = append(s.hotIDs, id)
				if objectTriggerAnyHot(o) {
					s.anyIDs = append(s.anyIDs, id)
				}
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
	anyIDs := s.anyIDs[:0]
	hot := false
	for _, id := range cur[from:] {
		if o := e.G.Obj(id); e.objectTriggerHotIn(o, slot) {
			hot = true
			hotIDs = append(hotIDs, id)
			if objectTriggerAnyHot(o) {
				anyIDs = append(anyIDs, id)
			}
		}
	}
	s.ids = append(s.ids[:0], cur...)
	s.hotIDs, s.anyIDs, s.hot, s.valid = hotIDs, anyIDs, hot, true
	return !hot
}

// trigZoneAnyIDs is trigZoneHotIDs' zero-interest subset (anyIDs).
func (e *Engine) trigZoneAnyIDs(p state.PlayerID, slot int) []state.ObjID {
	i := int(p)*trigZoneSlots + slot
	if i < 0 || i >= len(e.trigZones) {
		return nil
	}
	return e.trigZones[i].anyIDs
}

// trigZoneHotIDs returns the classified hot subset of zone (p, slot)'s list.
// It must be called after trigZoneCold has classified the zone; a cold zone
// (or one outside the summary range) has none.
func (e *Engine) trigZoneHotIDs(p state.PlayerID, slot int) []state.ObjID {
	i := int(p)*trigZoneSlots + slot
	if i < 0 || i >= len(e.trigZones) {
		return nil
	}
	return e.trigZones[i].hotIDs
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
// anyOnly (zeroInterestEvent) narrows a hot zone with no referent further, to
// its anyIDs: for an event no printed trigger interest names, a hot object
// outside that subset reaches only checkFaceTriggers' early return.
func (e *Engine) forEachTriggerObject(ev events.Event, skip, anyOnly bool, fn func(id state.ObjID), verify func(id state.ObjID)) {
	if !skip {
		e.forEachObject(fn)
		return
	}
	e.trigZonesCatchUp()
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
	for si, p := range e.G.AliveFrom(0) {
		for _, z := range objectWalkZones {
			if z == state.ZStack && si != 0 {
				continue
			}
			cur := e.G.Zone(z, p)
			slot := trigZoneSlot(z)
			// A StepChange forces the battlefield to the full walk below:
			// checkGrantedCumulativeUpkeepTriggers synthesizes a battlefield
			// trigger from a DERIVED keyword the face hot test cannot see, so
			// neither the cold summary nor the hot subset may prune it. Every
			// other granted trigger the battlefield summary could hide is
			// gated on the object being an event referent. Hidden-ish zones
			// keep their skip -- cumulative upkeep functions only from the
			// battlefield, so no grant can reach them.
			stepFull := slot == trigZoneSlot(state.ZBattlefield) && ev.Kind == events.StepChange && e.stepWalksBattlefield()
			if slot >= 0 && !stepFull && e.trigZoneCold(p, slot, cur) {
				if verify != nil {
					buf = append(buf[:0], cur...)
					for _, id := range buf {
						if trigMustVisit(ev, id) {
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
					if trigMustVisit(ev, id) {
						buf = append(buf, id)
					}
				}
			} else if slot >= 0 && !stepFull && refSlots&(1<<slot) == 0 {
				// Hot summarized zone with no event referent in it. Only the
				// classified hot objects can act, so visit those and skip the
				// rest; order is ids order because hotIDs is a subsequence of
				// the live list. In verify mode the skipped (cold) objects are
				// run through the verifier instead, so the hot-subset skip is
				// held to the same empirical contract the cold-zone skip is. A
				// referent would have to be walked in place, so its slot falls
				// through to the full list below.
				hotIDs := e.trigZoneHotIDs(p, slot)
				if anyOnly {
					hotIDs = e.trigZoneAnyIDs(p, slot)
				}
				if verify == nil {
					for _, id := range hotIDs {
						fn(id)
					}
					continue
				}
				buf = append(buf[:0], cur...)
				for _, id := range buf {
					if trigMustVisit(ev, id) || slices.Contains(hotIDs, id) {
						fn(id)
					} else {
						verify(id)
					}
				}
				continue
			} else if slot >= 0 && !stepFull {
				// Hot summarized zone WITH an event referent in it: the
				// referents are walked in place and, of everything else, only
				// the classified hot objects can act -- the two skips above
				// combined. The visit set is snapshotted first, in list order
				// (hotIDs is a subsequence of cur, so one merge pass keeps the
				// order), exactly as the full walk snapshots the list.
				hotIDs := e.trigZoneHotIDs(p, slot)
				if anyOnly {
					hotIDs = e.trigZoneAnyIDs(p, slot)
				}
				if verify != nil {
					buf = append(buf[:0], cur...)
					for _, id := range buf {
						if trigMustVisit(ev, id) || slices.Contains(hotIDs, id) {
							fn(id)
						} else {
							verify(id)
						}
					}
					continue
				}
				buf = buf[:0]
				j := 0
				for _, id := range cur {
					if j < len(hotIDs) && hotIDs[j] == id {
						j++
						buf = append(buf, id)
					} else if trigMustVisit(ev, id) {
						buf = append(buf, id)
					}
				}
				if j != len(hotIDs) {
					panic(fmt.Sprintf("rules: trigger zone summary (seat %d, slot %d) hot list is not a subsequence of the zone list", p, slot))
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
}

// stepWalksBattlefield reports whether a StepChange into the current step
// must walk the whole battlefield: only the two synthesized step triggers the
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
	*s = trigZoneSummary{ids: s.ids[:0], hotIDs: s.hotIDs[:0], anyIDs: s.anyIDs[:0]}
}
