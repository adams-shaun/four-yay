package rules

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// trigHotMergeRefs is trigHotMerge's non-step snapshot without the full
// zone-list scan. The visit set of a hot summarized zone that holds an event
// referent is the hot objects kindOnly selects plus the zone's referents, in
// list order. A referent already in hotIDs needs no position lookup (hotIDs
// is a subsequence of cur). Only a referent outside hotIDs must be placed
// among the hot ids, and it is found by scanning cur BACKWARD: a referent is
// almost always an object that just moved, so it sits at the tail of its new
// list and the scan stops after a few ids instead of walking the whole
// battlefield or graveyard.
//
// ok is false (the caller falls back to the full scan) when the event names
// more referents than the fixed buffers hold. Zero alloc: everything is on
// the stack except buf, which is the caller's reused snapshot.
//
// trigHotMergeVerify (the rules test binary) checks every call against the
// full scan; trigger_hotmerge_verify_test.go turns it on.
func (e *Engine) trigHotMergeRefs(buf []state.ObjID, ev *events.Event, cur, hotIDs []state.ObjID, hotSigs []trigSig, kindOnly bool, slot int) ([]state.ObjID, bool) {
	const maxRefs = 8
	var refs [maxRefs]state.ObjID
	n := 0
	add := func(id state.ObjID) bool {
		if id == 0 {
			return true
		}
		for _, r := range refs[:n] {
			if r == id {
				return true
			}
		}
		if n == maxRefs {
			return false
		}
		refs[n] = id
		n++
		return true
	}
	if !add(ev.Obj) {
		return buf, false
	}
	for _, id := range ev.IDs {
		if !add(id) {
			return buf, false
		}
	}
	for _, pr := range ev.Pairs {
		if !add(pr[0]) || !add(pr[1]) {
			return buf, false
		}
	}
	// The referents that are not hot ids but may sit in this list: an object
	// whose Zone field names another zone is not in cur (events.Apply moves
	// the field and the list membership together).
	var cold [maxRefs]state.ObjID
	nc := 0
	for _, r := range refs[:n] {
		if slices.Contains(hotIDs, r) {
			continue
		}
		o := e.G.Obj(r)
		if o == nil || trigZoneSlot(o.Zone) != slot {
			continue
		}
		cold[nc] = r
		nc++
	}
	// Place each cold referent: anchor = the number of hot ids before it.
	var pos [maxRefs]state.ObjID
	var anchor [maxRefs]int
	np := 0
	if nc > 0 {
		jh := len(hotIDs) - 1
		for i := len(cur) - 1; i >= 0 && np < nc; i-- {
			id := cur[i]
			if jh >= 0 && hotIDs[jh] == id {
				jh--
				continue
			}
			for _, r := range cold[:nc] {
				if r == id {
					pos[np], anchor[np] = id, jh+1
					np++
					break
				}
			}
		}
		// Found back to front: reverse into list order.
		for a, b := 0, np-1; a < b; a, b = a+1, b-1 {
			pos[a], pos[b] = pos[b], pos[a]
			anchor[a], anchor[b] = anchor[b], anchor[a]
		}
	}
	buf = buf[:0]
	k := 0
	for i, id := range hotIDs {
		for k < np && anchor[k] == i {
			buf = append(buf, pos[k])
			k++
		}
		if !kindOnly || hotSigs[i].admits(ev, e.G.Step) || slices.Contains(refs[:n], id) {
			buf = append(buf, id)
		}
	}
	for ; k < np; k++ {
		buf = append(buf, pos[k])
	}
	return buf, true
}

// trigHotMergeVerify checks trigHotMergeRefs against the full scan on every
// non-step merge (and makes trigHotMerge use the full scan's result).
var trigHotMergeVerify = derivedMemoVerifyFlag != ""

// verifyTrigHotMergeRefs panics when trigHotMergeRefs' snapshot differs from
// the full scan's.
func (e *Engine) verifyTrigHotMergeRefs(ev *events.Event, cur, hotIDs []state.ObjID, hotSigs []trigSig, kindOnly bool, p state.PlayerID, slot int) {
	fast, ok := e.trigHotMergeRefs(nil, ev, cur, hotIDs, hotSigs, kindOnly, slot)
	if !ok {
		return
	}
	full := e.trigHotMergeScan(nil, ev, cur, hotIDs, hotSigs, kindOnly, false, p, slot, nil, nil)
	if !slices.Equal(fast, full) {
		panic(fmt.Sprintf("rules: trigger hot merge (seat %d, slot %d) on %v: referent merge %v, full scan %v", p, slot, ev.Kind, fast, full))
	}
}
