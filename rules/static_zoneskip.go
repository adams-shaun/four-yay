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
// answers yes to any of the probes the zone's collectors share, or when it
// carries merged cards (a pile is never skipped by the collectors).
//
// The battlefield joined the four hidden-ish zones (task
// agent-20260927T121204Z-25ddc587): staticEffects re-scans once per emitted
// event, and on a mass-token board almost every permanent is a vanilla token
// with no statics at all, so the scan's cost is O(board) per event for a
// result no object changes. A face with NO statics is a provable no-op for
// every collector that shares staticSourceIDs, so the battlefield summary
// treats a face with any statics at all as hot. That probe is deliberately
// coarser than the off-battlefield trio below: the collectors sharing this
// walk differ in which Mode$ and which parameters they read (staticEffects
// reads Continuous; staticsMayChangeTypes reads the layer-4 keys; the
// walkcache cost/mana collectors read others), so "has any static" is the
// one test conservative for all of them, and it still skips the vanilla
// token. A static-cold object is therefore skipped by every collector's own
// per-object gate, so leaving it out of the walk changes no collector's
// output or order.
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

var staticZoneSkipVerifyFlag string

var staticZoneSkipVerify = derivedMemoVerifyFlag != "" || staticZoneSkipVerifyFlag != ""

// staticZoneSlots is the number of summarized zones per seat.
const staticZoneSlots = 5

// staticZoneSlot maps a zone to its summary slot, or -1 for a zone every
// static walk visits in full (stack, command).
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
	case state.ZBattlefield:
		return 4
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

// faceStaticHotOn reports whether f carries ANY static, so some battlefield
// static collector could read it. The collectors sharing staticSourceIDs read
// different Mode$ values and parameters, so this is the conservative shared
// test: a face with no Statics at all is a provable no-op for every one of
// them (each collector iterates the face's Statics and acts inside), and a
// face with any static stays hot.
func faceStaticHotOn(f *cards.Face) bool {
	return f != nil && len(f.Statics) > 0
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

// objectStaticHotOn reports whether any face o.Face() could resolve to carries
// any static on the battlefield, or o is a merged pile.
func objectStaticHotOn(o *state.Object) bool {
	if o == nil {
		return false
	}
	if len(o.MergedCards) > 0 {
		return true
	}
	if o.CopyFace != nil && faceStaticHotOn(o.CopyFace) {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if faceStaticHotOn(f) {
				return true
			}
		}
	}
	return false
}

// objectStaticHot is the per-object static-hot test for a zone: the
// battlefield uses the coarser has-any-static probe (faceStaticHotOn), every
// other summarized zone the off-battlefield trio (faceStaticHotOff).
func objectStaticHot(o *state.Object, z state.Zone) bool {
	if z == state.ZBattlefield {
		return objectStaticHotOn(o)
	}
	return objectStaticHotOff(o)
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
			e.staticZoneSkipVerifyOnce(i, z, cur, s.hotIDs)
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
		if objectStaticHot(e.G.Obj(id), z) {
			hot = append(hot, id)
		}
	}
	s.ids = append(s.ids[:0], cur...)
	s.hotIDs, s.valid = hot, true
	if staticZoneSkipVerify {
		e.staticZoneSkipVerifyOnce(i, z, cur, hot)
	}
	return hot
}

// staticZoneVerifiedAt records the exact (cur, hot) slices one summary slot
// was verified against: same backing arrays, same lengths.
type staticZoneVerifiedAt struct {
	cur, hot *state.ObjID
	nc, nh   int
	ok       bool
}

func sliceHead(s []state.ObjID) *state.ObjID {
	if cap(s) == 0 {
		return nil
	}
	return &s[:1][0]
}

// staticZoneSkipVerifyOnce is verifyStaticZoneSkip, deduplicated inside ONE
// verifyBoardStatics call. That call recomputes the cost, action and
// ManaConvert scans back to back, a pure read with no event and no state
// write between them, and each scan asks staticSourceIDs for the same seats
// and zones -- so without the dedupe the identical (cur, hot) check ran three
// times over identical objects. A slot is skipped only when the very same
// cur and hot slices (backing array and length) were verified earlier in the
// same call; every first sighting, and every call outside a verify scope,
// runs the full check.
func (e *Engine) staticZoneSkipVerifyOnce(i int, z state.Zone, cur, hot []state.ObjID) {
	if !e.staticZoneVerifyScope {
		e.verifyStaticZoneSkip(z, cur, hot)
		return
	}
	for i >= len(e.staticZoneVerified) {
		e.staticZoneVerified = append(e.staticZoneVerified, staticZoneVerifiedAt{})
	}
	at := staticZoneVerifiedAt{cur: sliceHead(cur), hot: sliceHead(hot), nc: len(cur), nh: len(hot), ok: true}
	if e.staticZoneVerified[i] == at {
		return
	}
	e.verifyStaticZoneSkip(z, cur, hot)
	e.staticZoneVerified[i] = at
}

// verifyStaticZoneSkip panics when hot is not exactly the static-hot
// subsequence of cur recomputed from scratch, or when a cold object's current
// face answers yes to a probe.
func (e *Engine) verifyStaticZoneSkip(z state.Zone, cur, hot []state.ObjID) {
	var want []state.ObjID
	for _, id := range cur {
		o := e.G.Obj(id)
		if objectStaticHot(o, z) {
			want = append(want, id)
			continue
		}
		if o != nil {
			var coldLive bool
			if z == state.ZBattlefield {
				coldLive = faceStaticHotOn(o.Face())
			} else {
				coldLive = faceStaticHotOff(o.Face())
			}
			if coldLive {
				panic(fmt.Sprintf("rules: static-cold obj %d has a live face in zone %v", id, z))
			}
		}
	}
	if !slices.Equal(want, hot) {
		panic(fmt.Sprintf("rules: static zone summary %v, recomputed %v (zone %v)", hot, want, z))
	}
}
