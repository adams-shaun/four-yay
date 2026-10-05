package rules

import (
	"fmt"
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
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

var replZoneSkipVerifyFlag string

var replZoneSkipVerify = derivedMemoVerifyFlag != "" || replZoneSkipVerifyFlag != ""

// replZoneCount is the number of summarized zones per seat: ZLibrary ..
// ZStack, the zones forEachObject walks.
const replZoneCount = int(state.ZStack) + 1

type replZoneSummary struct {
	ids []state.ObjID
	// live is the list header last confirmed (see sameZoneList), held so its
	// array cannot be recycled while the summary names it.
	live   []state.ObjID
	hotIDs []state.ObjID
	// mask is the union of the hot objects' replacement event bits
	// (objectReplMask): a superset of every R:Event$ name a visit in this
	// zone can match.
	mask  uint32
	epoch int
	valid bool
}

// objectReplHot reports whether any face replacementFace could return for o
// carries an R: line.
func faceReplHot(f *cards.Face) bool { return f != nil && len(f.Repls) > 0 }

// replEventBit maps an R:Event$ name -- or the name replacementEvent gives
// a logged event -- to its bit; DrawCards shares Draw's (the one alias
// replacementEventNameMatches accepts). Every cards.ReplEvent kind has a
// bit; a name outside the vocabulary gets none: no event can match it.
func replEventBit(name string) uint32 { return replEventBits[cards.ReplEventOf(name)] }

// replLineBit is replEventBit for an R: line, through its load-time code.
func replLineBit(r *cards.Repl) uint32 { return replEventBits[r.EventKind()] }

// objectReplMask is the union of replEventBit over every R: line of every
// face objectReplHot reads (CopyFace and each of the card's faces), the
// face set replacementFace chooses from.
func objectReplMask(o *state.Object) uint32 {
	if o == nil {
		return 0
	}
	var m uint32
	if o.CopyFace != nil {
		for i := range o.CopyFace.Repls {
			m |= replLineBit(&o.CopyFace.Repls[i])
		}
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if f == nil {
				continue
			}
			for i := range f.Repls {
				m |= replLineBit(&f.Repls[i])
			}
		}
	}
	return m
}

func objectReplHot(o *state.Object) bool {
	if o == nil {
		return false
	}
	if faceReplHot(o.CopyFace) {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if faceReplHot(f) {
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
	// An in-place write can change only the touched object's own class, so a
	// summary that already records that class (hot exactly when listed in
	// hotIDs) stays exact; a list change is caught by the list comparison.
	hot, classified := false, false
	var mask uint32
	for i := int(o.Zone); i < len(e.replZones); i += replZoneCount {
		z := &e.replZones[i]
		if !z.valid {
			continue
		}
		if !classified {
			hot, classified = objectReplHot(o), true
			if hot {
				mask = objectReplMask(o)
			}
		}
		inHot := slices.Contains(z.hotIDs, id)
		if !inHot && !hot {
			// A cold object the summary does not list as hot is already
			// classified correctly wherever it sits (mask is 0): nothing to
			// drop, and no need to search the full id list for it.
			continue
		}
		if !inHot && !slices.Contains(z.ids, id) {
			// Another seat's list: this summary does not describe the
			// object (a list change is caught by the list comparison).
			continue
		}
		if hot != inHot || mask&^z.mask != 0 {
			z.valid = false
		}
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
	for i := e.replZonesEp; i < n; i++ {
		ev := &e.L.Events[i]
		if touchFreeKinds.has(ev.Kind) {
			// No in-place write to a summarized field (touchFreeKinds); verify
			// mode recomputes every summary on use (verifyReplZoneSkip).
			continue
		}
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
		e.replZones = growZoneSummaries(e.replZones, max(i+1, len(e.G.Players)*replZoneCount))
	}
	s := &e.replZones[i]
	n := len(e.L.Events)
	if s.valid && sameZoneList(s.live, cur) {
		if replZoneSkipVerify && !slices.Equal(s.ids, cur) {
			panic(fmt.Sprintf("rules: replacement zone summary (seat %d, zone %v) kept its header but the list changed in place", p, z))
		}
		s.epoch = n
		return s.hotIDs
	}
	s.live = cur
	if s.valid && len(cur) > len(s.ids) && s.epoch > 0 && s.epoch <= n && z == state.ZBattlefield {
		// TokenCreate appends exactly one object to the event player's
		// battlefield and names no Obj referent. The event suffix therefore
		// proves the old list is an unchanged prefix without comparing its
		// thousands of IDs; classify only the newly appended tail.
		creates := 0
		appendOnly := true
		for j := s.epoch; j < n; j++ {
			if ev := &e.L.Events[j]; ev.Kind != events.TokenCreate || ev.Player != p {
				appendOnly = false
				break
			}
			creates++
		}
		if appendOnly && creates == len(cur)-len(s.ids) {
			for _, id := range cur[len(s.ids):] {
				if o := e.G.Obj(id); objectReplHot(o) {
					s.hotIDs = append(s.hotIDs, id)
					s.mask |= objectReplMask(o)
				}
			}
			s.ids = append(s.ids, cur[len(s.ids):]...)
			s.epoch, s.valid = n, true
			return s.hotIDs
		}
	}
	if s.valid && slices.Equal(s.ids, cur) {
		s.epoch = n
		return s.hotIDs
	}
	from := 0
	hot := s.hotIDs[:0]
	var mask uint32
	if s.valid && len(cur) > len(s.ids) && slices.Equal(s.ids, cur[:len(s.ids)]) {
		// Append-only: the recorded prefix and its hot subset stand.
		from = len(s.ids)
		hot, mask = s.hotIDs, s.mask
	}
	for _, id := range cur[from:] {
		if o := e.G.Obj(id); objectReplHot(o) {
			hot = append(hot, id)
			mask |= objectReplMask(o)
		}
	}
	s.ids = append(s.ids[:0], cur...)
	s.hotIDs, s.mask, s.epoch, s.valid = hot, mask, len(e.L.Events), true
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
	e.forEachReplacementSourceFor(0, fn)
}

// forEachReplacementSourceFor is forEachReplacementSource for a caller that
// acts on a visited object only through R: lines whose event name maps to
// bit (replEventBit; 0 visits everything): a summarized zone whose hot
// objects carry no such line (summary mask) is not visited. The summaries
// are still brought up to date, in the same order. The command zone is
// always visited.
func (e *Engine) forEachReplacementSourceFor(bit uint32, fn func(id state.ObjID)) {
	if bit != 0 && !e.replArenaMaskFor(bit) {
		// No arena object carries a line for bit (repl_arena_mask.go): only
		// the command zone can be visited.
		if replZoneSkipVerify {
			e.verifyReplArenaSkip(bit)
		}
		for _, p := range e.G.AliveFrom(0) {
			for _, id := range e.G.Zone(state.ZCommand, p) {
				fn(id)
			}
		}
		return
	}
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
			if bit != 0 && e.replZones[int(p)*replZoneCount+int(z)].mask&bit == 0 {
				if replZoneSkipVerify {
					for _, id := range hot {
						if objectReplMask(e.G.Obj(id))&bit != 0 {
							panic(fmt.Sprintf("rules: replacement zone mask skipped obj %d carrying event bit %#x", id, bit))
						}
					}
				}
				continue
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

func (s *replZoneSummary) resetSummary() {
	*s = replZoneSummary{ids: s.ids[:0], hotIDs: s.hotIDs[:0]}
}

// copyReplZones is copyTrigZones for the replacement-source summaries; the
// TokenCreate append path's epoch is the parent's, over the same log.
func copyReplZones(dst, src []replZoneSummary) []replZoneSummary {
	dst = growZoneSummaries(dst[:0], len(src))
	for i := range src {
		d, s := &dst[i], &src[i]
		d.ids = append(d.ids[:0], s.ids...)
		d.hotIDs = append(d.hotIDs[:0], s.hotIDs...)
		d.live, d.mask, d.epoch, d.valid = d.ids, s.mask, s.epoch, s.valid
	}
	return dst
}

var replEventBits = [cards.ReplEventCount]uint32{
	cards.ReplAttached:       1 << 0,
	cards.ReplMoved:          1 << 1,
	cards.ReplUntap:          1 << 2,
	cards.ReplBeginPhase:     1 << 3,
	cards.ReplTransform:      1 << 4,
	cards.ReplProduceMana:    1 << 5,
	cards.ReplDamageDone:     1 << 6,
	cards.ReplDraw:           1 << 7,
	cards.ReplDrawCards:      1 << 7,
	cards.ReplCreateToken:    1 << 8,
	cards.ReplExplore:        1 << 9,
	cards.ReplCascade:        1 << 10,
	cards.ReplScry:           1 << 11,
	cards.ReplRollDice:       1 << 12,
	cards.ReplRollPlanarDice: 1 << 13,
	cards.ReplAddCounter:     1 << 14,
	cards.ReplTurnFaceUp:     1 << 15,
	// Every remaining kind has its own bit too: a line with no bit sits in
	// a zone whose summary mask cannot see it, so an every-event body scan
	// (tapeAnyReplBodyMayAsk's ^0) would miss its asking body.
	// TestReplEventBitCensus holds every kind reachable.
	cards.ReplGainLife:    1 << 16,
	cards.ReplLifeReduced: 1 << 17,
	cards.ReplCounter:     1 << 18,
	cards.ReplBeginTurn:   1 << 19,
	cards.ReplGameLoss:    1 << 20,
	cards.ReplGameWin:     1 << 21,
	cards.ReplPayLife:     1 << 22,
}
