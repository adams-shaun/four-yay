package rules

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Off-battlefield static-source walk skip.
//
// The whole-board static collectors -- staticEffects (active()'s static half,
// rebuilt after nearly every emitted event), staticsMayChangeTypes
// (anyLayer4Active's precheck, likewise per event) and the fused
// scanBoardStatics / scanActionStatics / scanCostStatics / scanManaConvSources
// walks -- visit every object of every seat's library, hand, graveyard and
// exile, and for almost all of them do nothing: off the battlefield a static
// is admitted only through an EffectZone$ / ExcludeZone$ key (or the
// stack-self shape, which needs the stack, never summarized here), and each
// collector already skips an object whose current face's derived probe says
// no such static exists (cards.Face.ContinuousStaticsMayFunctionOffBattlefield,
// StaticsMayNameEffectZone, StaticsMayChangeTypes(false)). A library of sixty
// vanilla cards costs sixty object visits per collector per event.
//
// This file lets those walks visit only the STATIC-HOT subsequence of a
// summarized zone, in list order. An object is static-hot when any face
// o.Face() could resolve to -- every entry of o.Card.Faces (so a face flip or
// faceprobe's temporary FaceIdx swap changes nothing) and o.CopyFace --
// answers yes to any of the three probes, or when it carries merged cards (a
// pile is never skipped by the collectors). A static-cold object is therefore
// skipped by every collector's own per-object gate, so leaving it out of the
// walk changes no collector's output or order.
//
// Summary freshness is the replacement-walk argument (rules/repl_zoneskip.go):
// each (seat, zone) summary records its id list and hot subset, and is reused
// only while the live list equals the recorded one; otherwise the longest
// prefix of the live list that is a subsequence of the recorded one (removals
// anywhere) keeps its recorded classification and only the rest is
// classified.
// The one remaining input, an object's own Card/CopyFace/MergedCards changing
// in place, is written only by events.Apply and keyed by the event's Obj, IDs
// or Pairs, so staticZonesCatchUp drops the summary of whichever summarized
// zone every object referenced by an event logged since the last catch-up now
// sits in. A log shorter than the last catch-up drops every summary.
//
// staticZoneSkipVerify (set by the rules test binary) recomputes every
// summary from scratch on use and panics on a difference or on a cold object
// whose current face answers yes to a probe, and staticEffects additionally
// re-runs the unskipped walk and compares the whole emission.

var staticZoneSkipVerify = derivedMemoVerifyFlag != ""

// staticZoneSlots is the number of summarized zones per seat.
const staticZoneSlots = 4

// staticZoneSlot maps a zone to its summary slot, or -1 for a zone every
// static walk visits in full (battlefield, stack, command).
func staticZoneSlot(z state.Zone) int {
	switch z {
	case state.ZLibrary:
		return 0
	case state.ZHand:
		return 1
	case state.ZGraveyard:
		return 2
	case state.ZExile:
		return 3
	}
	return -1
}

type staticZoneSummary struct {
	ids    []state.ObjID
	hotIDs []state.ObjID
	valid  bool
}

// faceStaticHotOff reports whether f carries a static some off-battlefield
// static collector could admit. Each probe is conservative (an unbound or
// stale probe answers true).
func faceStaticHotOff(f *cards.Face) bool {
	return f != nil && (f.ContinuousStaticsMayFunctionOffBattlefield() || f.StaticsMayNameEffectZone() ||
		f.StaticsMayChangeTypes(false))
}

// objectStaticHotOff reports whether any face o.Face() could resolve to is
// static-hot off the battlefield, or o is a merged pile.
func objectStaticHotOff(o *state.Object) bool {
	if o == nil {
		return false
	}
	if len(o.MergedCards) > 0 {
		return true
	}
	if o.CopyFace != nil && faceStaticHotOff(o.CopyFace) {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if faceStaticHotOff(f) {
				return true
			}
		}
	}
	return false
}

func (e *Engine) staticZoneTouch(id state.ObjID) {
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	s := staticZoneSlot(o.Zone)
	if s < 0 {
		return
	}
	for i := s; i < len(e.staticZones); i += staticZoneSlots {
		e.staticZones[i].valid = false
	}
}

// staticZonesCatchUp drops the summary of every summarized zone an object
// referenced by an event logged since the previous catch-up now sits in.
func (e *Engine) staticZonesCatchUp() {
	n := len(e.L.Events)
	if n == e.staticZonesEp {
		return
	}
	if len(e.staticZones) == 0 {
		e.staticZonesEp = n
		return
	}
	if n < e.staticZonesEp {
		for i := range e.staticZones {
			e.staticZones[i].valid = false
		}
		e.staticZonesEp = n
		return
	}
	for _, ev := range e.L.Events[e.staticZonesEp:] {
		e.staticZoneTouch(ev.Obj)
		for _, id := range ev.IDs {
			e.staticZoneTouch(id)
		}
		for _, pr := range ev.Pairs {
			e.staticZoneTouch(pr[0])
			e.staticZoneTouch(pr[1])
		}
	}
	e.staticZonesEp = n
}

// staticSourceIDs returns the ids of zone (p, z) a whole-board static
// collector must visit: the full list for an unsummarized zone, else the
// static-hot subsequence. The result is read-only; a rebuilt summary never
// rewrites a hot list a caller may still be ranging.
func (e *Engine) staticSourceIDs(p state.PlayerID, z state.Zone) []state.ObjID {
	cur := e.G.Zone(z, p)
	slot := staticZoneSlot(z)
	if slot < 0 || len(cur) == 0 {
		return cur
	}
	e.staticZonesCatchUp()
	i := int(p)*staticZoneSlots + slot
	if i >= len(e.staticZones) {
		e.staticZones = append(e.staticZones, make([]staticZoneSummary, i+1-len(e.staticZones))...)
	}
	s := &e.staticZones[i]
	if s.valid && slices.Equal(s.ids, cur) {
		if staticZoneSkipVerify {
			e.verifyStaticZoneSkip(cur, s.hotIDs)
		}
		return s.hotIDs
	}
	from := 0
	var hot []state.ObjID
	if s.valid && len(cur) > len(s.ids) && slices.Equal(s.ids, cur[:len(s.ids)]) {
		// Append-only: the recorded prefix and its hot subset stand. Growing
		// the hot list by append never rewrites an element a caller ranges.
		from = len(s.ids)
		hot = s.hotIDs
	} else if s.valid {
		// Removals anywhere plus an appended tail (a draw, a card played
		// from hand, a shuffle's reorder degrading to a full tail): the
		// longest prefix of cur that is a subsequence of the recorded list
		// keeps each object's recorded classification -- ids are unique in
		// a zone list and an object whose own fields moved was touched, which
		// invalidated this summary -- and only the unmatched tail is
		// classified. The kept hot ids go to fresh storage.
		j, h, k := 0, 0, len(cur)
		for idx, id := range cur {
			for j < len(s.ids) && s.ids[j] != id {
				if h < len(s.hotIDs) && s.hotIDs[h] == s.ids[j] {
					h++
				}
				j++
			}
			if j == len(s.ids) {
				k = idx
				break
			}
			if h < len(s.hotIDs) && s.hotIDs[h] == id {
				hot = append(hot, id)
				h++
			}
			j++
		}
		from = k
	}
	for _, id := range cur[from:] {
		if objectStaticHotOff(e.G.Obj(id)) {
			hot = append(hot, id)
		}
	}
	s.ids = append(s.ids[:0], cur...)
	s.hotIDs, s.valid = hot, true
	if staticZoneSkipVerify {
		e.verifyStaticZoneSkip(cur, hot)
	}
	return hot
}

// verifyStaticZoneSkip panics when hot is not exactly the static-hot
// subsequence of cur recomputed from scratch, or when a cold object's current
// face answers yes to an off-battlefield probe.
func (e *Engine) verifyStaticZoneSkip(cur, hot []state.ObjID) {
	var want []state.ObjID
	for _, id := range cur {
		o := e.G.Obj(id)
		if objectStaticHotOff(o) {
			want = append(want, id)
			continue
		}
		if o != nil && faceStaticHotOff(o.Face()) {
			panic(fmt.Sprintf("rules: static-cold obj %d has an off-battlefield-live face", id))
		}
	}
	if !slices.Equal(want, hot) {
		panic(fmt.Sprintf("rules: static zone summary %v, recomputed %v (zone %v)", hot, want, cur))
	}
}
