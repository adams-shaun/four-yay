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
	// hotIDs is the subset of ids whose objects can act on some event from
	// this zone, in ids order (see objectTriggerHotIn). The live walk visits
	// only these when the zone is hot and no event referent sits in it, so a
	// battlefield holding thousands of vanilla tokens beside one trigger
	// source costs the sources, not the tokens. nil while the zone is cold or
	// unclassified.
	hotIDs []state.ObjID
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

func (e *Engine) trigZoneInvalidateAll() {
	for i := range e.trigZones {
		e.trigZones[i].valid = false
	}
}

func (e *Engine) trigZoneTouch(id state.ObjID) {
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	s := trigZoneSlot(o.Zone)
	if s < 0 {
		return
	}
	for i := s; i < len(e.trigZones); i += trigZoneSlots {
		e.trigZones[i].valid = false
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
	for _, ev := range e.L.Events[e.trigZonesEp:] {
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

// trigZoneCold reports whether zone (p, z) -- whose live list is cur -- holds
// no hot object, refreshing its summary as needed.
func (e *Engine) trigZoneCold(p state.PlayerID, slot int, cur []state.ObjID) bool {
	i := int(p)*trigZoneSlots + slot
	if i >= len(e.trigZones) {
		e.trigZones = append(e.trigZones, make([]trigZoneSummary, i+1-len(e.trigZones))...)
	}
	s := &e.trigZones[i]
	if s.valid && slices.Equal(s.ids, cur) {
		return !s.hot
	}
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
			if e.objectTriggerHotIn(e.G.Obj(id), slot) {
				s.hot = true
				s.hotIDs = append(s.hotIDs, id)
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
	hot := false
	for _, id := range cur[from:] {
		if e.objectTriggerHotIn(e.G.Obj(id), slot) {
			hot = true
			hotIDs = append(hotIDs, id)
		}
	}
	s.ids = append(s.ids[:0], cur...)
	s.hotIDs, s.hot, s.valid = hotIDs, hot, true
	return !hot
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
func (e *Engine) forEachTriggerObject(ev events.Event, skip bool, fn func(id state.ObjID), verify func(id state.ObjID)) {
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
		for z := state.ZLibrary; z <= state.ZStack; z++ {
			if z == state.ZStack && si != 0 {
				continue
			}
			cur := e.G.Zone(z, p)
			if slot := trigZoneSlot(z); slot >= 0 && e.trigZoneCold(p, slot, cur) {
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
			} else if slot := trigZoneSlot(z); slot >= 0 && refSlots&(1<<slot) == 0 {
				// Hot summarized zone with no event referent in it. Only the
				// classified hot objects can act, so visit those and skip the
				// rest; order is ids order because hotIDs is a subsequence of
				// the live list. In verify mode the skipped (cold) objects are
				// run through the verifier instead, so the hot-subset skip is
				// held to the same empirical contract the cold-zone skip is. A
				// referent would have to be walked in place, so its slot falls
				// through to the full list below.
				hotIDs := e.trigZoneHotIDs(p, slot)
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
