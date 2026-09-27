package rules

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/state"
)

// Replacement-source walk skip.
//
// forEachReplacementSource visits every object of every living seat's
// library, hand, battlefield, graveyard, exile and the stack on every
// replaceable event, and each caller's body does exactly one thing with a
// visited id: it reads replacementFace(id, ev).Repls. replacementFace is
// o.Face() (CopyFace, else Card.Faces[FaceIdx]) or, for a FlipFace, another
// entry of o.Card.Faces -- so an object none of whose Card.Faces and whose
// CopyFace carries an R: line is a provable no-op for every caller. Such an
// object is REPLACEMENT-COLD; the walk visits only the hot ones, in the same
// list order, so every caller sees exactly the unskipped walk's sequence of
// non-trivial visits.
//
// Each summarized zone (seat x zone) records its id list at classification
// time and the hot subset of it. It is reused only while the live list
// still equals the recorded one (an integer compare) or extends it by
// appended ids (classified individually). The one remaining input is an
// object's own Card/CopyFace changing in place; every such write is in
// events.Apply and keyed by the event's Obj, IDs or Pairs (the same argument
// rules/trigger_zoneskip.go makes), so before each walk replZonesCatchUp
// drops the summary of whichever zone each object referenced by an event
// logged since the previous walk now sits in. A log shorter than the last
// catch-up (a truncation) drops every summary.
//
// replZoneSkipVerify (set by the rules test binary) walks the full list as
// well and panics if a skipped object is hot now or has a replacement face
// with R: lines.

var replZoneSkipVerify = derivedMemoVerifyFlag != ""

// replZoneCount is the number of summarized zones per seat: ZLibrary ..
// ZStack, the zones forEachObject walks.
const replZoneCount = int(state.ZStack) + 1

type replZoneSummary struct {
	ids    []state.ObjID
	hotIDs []state.ObjID
	valid  bool
}

// objectReplHot reports whether any face replacementFace could return for o
// carries an R: line.
func objectReplHot(o *state.Object) bool {
	if o == nil {
		return false
	}
	if o.CopyFace != nil && len(o.CopyFace.Repls) > 0 {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if f != nil && len(f.Repls) > 0 {
				return true
			}
		}
	}
	return false
}

func (e *Engine) replZoneTouch(id state.ObjID) {
	o := e.G.Obj(id)
	if o == nil || int(o.Zone) >= replZoneCount {
		return
	}
	for i := int(o.Zone); i < len(e.replZones); i += replZoneCount {
		e.replZones[i].valid = false
	}
}

func (e *Engine) replZonesCatchUp() {
	n := len(e.L.Events)
	if len(e.replZones) == 0 {
		// Nothing summarized yet (a fresh engine or a Clone): nothing to drop.
		e.replZonesEp = n
		return
	}
	if n < e.replZonesEp {
		for i := range e.replZones {
			e.replZones[i].valid = false
		}
		e.replZonesEp = n
		return
	}
	for _, ev := range e.L.Events[e.replZonesEp:] {
		e.replZoneTouch(ev.Obj)
		for _, id := range ev.IDs {
			e.replZoneTouch(id)
		}
		for _, pr := range ev.Pairs {
			e.replZoneTouch(pr[0])
			e.replZoneTouch(pr[1])
		}
	}
	e.replZonesEp = n
}

// replZoneHot returns the hot subset of zone (p, z), whose live list is cur,
// refreshing its summary as needed. The result is summary storage: callers
// snapshot it before running a body.
func (e *Engine) replZoneHot(p state.PlayerID, z state.Zone, cur []state.ObjID) []state.ObjID {
	i := int(p)*replZoneCount + int(z)
	if i >= len(e.replZones) {
		e.replZones = append(e.replZones, make([]replZoneSummary, i+1-len(e.replZones))...)
	}
	s := &e.replZones[i]
	if s.valid && slices.Equal(s.ids, cur) {
		return s.hotIDs
	}
	from := 0
	hot := s.hotIDs[:0]
	if s.valid && len(cur) > len(s.ids) && slices.Equal(s.ids, cur[:len(s.ids)]) {
		// Append-only: the recorded prefix and its hot subset stand.
		from = len(s.ids)
		hot = s.hotIDs
	}
	for _, id := range cur[from:] {
		if objectReplHot(e.G.Obj(id)) {
			hot = append(hot, id)
		}
	}
	s.ids = append(s.ids[:0], cur...)
	s.hotIDs, s.valid = hot, true
	return hot
}

// forEachReplacementSource extends the ordinary battlefield/game-zone scan
// with the command zone, where Plane/Vanguard replacement text explicitly
// declares ActiveZones$ Command. Trigger discovery deliberately keeps using
// forEachObject, so this cannot make unrelated command-zone triggers live.
// Command-zone objects are visited once per living seat, after all ordinary
// zones, in their zone order; replacementMatches requires an explicit Command
// ActiveZones declaration there, preventing ordinary card text from becoming
// active merely because its object happens to be parked in that zone.
//
// It visits every replacement-hot object in
// forEachObject's player/zone/list order, then every object of each living
// seat's command zone. See the file comment for why a cold object may be
// skipped; callers must only act on a visited id through
// replacementFace(id, ev).Repls.
func (e *Engine) forEachReplacementSource(fn func(id state.ObjID)) {
	e.replZonesCatchUp()
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
			hot := e.replZoneHot(p, z, cur)
			if replZoneSkipVerify {
				e.verifyReplZoneSkip(cur, hot)
			}
			buf = append(buf[:0], hot...)
			for _, id := range buf {
				fn(id)
			}
		}
	}
	if e.foreachDepth <= 1 {
		e.foreachBuf = buf
	}
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZCommand, p) {
			fn(id)
		}
	}
}

// verifyReplZoneSkip panics when hot is not exactly the hot subsequence of
// cur recomputed from scratch.
func (e *Engine) verifyReplZoneSkip(cur, hot []state.ObjID) {
	var want []state.ObjID
	for _, id := range cur {
		o := e.G.Obj(id)
		if objectReplHot(o) {
			want = append(want, id)
			continue
		}
		if o != nil {
			if f := o.Face(); f != nil && len(f.Repls) > 0 {
				panic(fmt.Sprintf("rules: replacement-cold obj %d has a face with R: lines", id))
			}
		}
	}
	if !slices.Equal(want, hot) {
		panic(fmt.Sprintf("rules: replacement zone summary %v, recomputed %v (zone %v)", hot, want, cur))
	}
}
