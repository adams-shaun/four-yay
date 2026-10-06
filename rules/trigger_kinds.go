package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Exact per-event-kind trigger interest.
//
// The compiled TriggerInterest classes (cards) and triggerEventMask are
// deliberately coarse: a face carrying ANY Phase$ gate is interested in every
// event (its unresolvable-spec diagnostic is event-visible), a mode the class
// table does not name (LifeGained, CounterAdded, ...) is interested in every
// event, and every kind at or past triggerMaskKindBits fails open. So the
// live walk visits, on every Priority, DecisionAsk and DamageProvenance event,
// every object whose face carries an "at the beginning of your upkeep"
// trigger, and runs its whole trigger loop to have triggerMatches' first gate
// reject each line.
//
// trigKinds is the exact version of that gate, one bit per event kind below
// 128 (events.NumKinds is far below it; a kind at or past 128 is always
// allowed). A face's mask is the union over its trigger lines of:
//
//   - every kind, for a line whose Phase$ spec does not parse: the walk's
//     Phase$ diagnostic runs for it on every event;
//   - otherwise modeTrigKinds(Mode): triggerModeEvents' kinds, which are
//     exactly the low kinds triggerMatches' leading gate admits, plus the
//     kinds at or past triggerMaskKindBits that the gate fails open on --
//     all of them, except for the modes audited in modeRejectsHighKinds,
//     whose matchers return false for every such kind.
//
// So for an event of a kind outside the mask, every line of the face is
// rejected by triggerMatches' leading gate (or earlier by the walk's own
// gates), the Phase$ diagnostic cannot fire, and the face loop of
// checkFaceTriggers is a no-op. trigZoneSkipVerify holds every skip this
// enables to that contract: it runs the skipped visit and panics if it queued
// anything.
//
// trigSig refines the zone-change kinds (MoveZone, Draw, PutOnStack) by the
// event's From/To zones: a Mode$ ChangesZone/ChangesZoneAll line can match
// only when trigmatch.ZoneChangeMatchesWithCapture's Origin$ and Destination$ tests
// admit ev.From and ev.To, so a face whose zone-change listeners are all
// "enters the battlefield" lines cannot act on a draw, a cast or a death.
type trigKinds [2]uint64

// trigSig is a face's (or an object's) exact event signature: the kind mask,
// for the zone-change kinds the union of the From and To zones their
// listening lines admit (bit z for zone z < 32; a zone at or past 32 is
// always admitted), and the union of the steps during which a line's Phase$
// gate (triggerMatches' phaseGate, which every mode passes through) can hold
// -- allSteps for a line with no Phase$.
type trigSig struct {
	kinds  trigKinds
	zcFrom uint32
	zcTo   uint32
	steps  uint16
}

// allSteps marks an ungated line: it admits every step, an invalid one
// included (phaseGate's absent-Phase$ arm). A gated line's set is a
// state.StepSet, whose bits stay below it.
const allSteps = ^uint16(0)

var allTrigSig = trigSig{kinds: allTrigKinds, zcFrom: ^uint32(0), zcTo: ^uint32(0), steps: allSteps}

func (s trigSig) or(o trigSig) trigSig {
	return trigSig{kinds: s.kinds.or(o.kinds), zcFrom: s.zcFrom | o.zcFrom, zcTo: s.zcTo | o.zcTo, steps: s.steps | o.steps}
}

func zoneMaskHas(m uint32, z state.Zone) bool { return z >= 32 || m&(1<<z) != 0 }

// touchFreeKinds are the event kinds whose events.Apply fold never writes an
// EXISTING object's Card, CopyFace, FaceIdx, Unlocked, MergedCards or Zone
// (and never moves one between zone lists): the fields the trigger and
// replacement zone summaries classify by. Their referents need no catch-up
// touch (trigZonesCatchUp, replZonesCatchUp); a fold that mints a NEW object
// changes a zone list, which the summaries' list comparison sees. Audited
// against the folds (events/apply*.go): every write to those fields is in
// the zone-move, copy/clone/mutate/token, door-unlock, myriad, end-turn and
// control-change folds, none of which is listed. Note is deliberately NOT
// listed although it has no fold: naming an object in a Note is the
// documented way to announce an in-place write made outside Apply
// (TestTrigZoneSkipSeesInPlaceChangeThroughReferent). trigZoneSkipVerify
// touches the listed kinds anyway and panics if a summary changes.
var touchFreeKinds = func() trigKinds {
	var m trigKinds
	for _, k := range []events.Kind{events.Tap, events.Untap, events.Damage, events.CounterChange,
		events.LifeChange, events.Priority, events.DecisionAsk, events.DecisionMade,
		events.StepChange, events.TurnChange, events.ManaAdd, events.ManaClear,
		events.ClockTick, events.TargetsChosen, events.DeclareAttackers, events.DeclareBlockers,
		events.DamageProvenance, events.EndCombatReset, events.Imprint, events.StoreSVar,
		events.Choose, events.ModeChosen, events.CastInfo, events.Goad, events.Exert,
		events.PlayerCounterChange, events.NoteNumber, events.CardNoted} {
		m[k>>6] |= 1 << (k & 63)
	}
	return m
}()

// zoneChangeKind: the kinds trigmatch.ZoneChangeMatchesWithCapture accepts.
func zoneChangeKind(k events.Kind) bool {
	return k == events.MoveZone || k == events.Draw || k == events.PutOnStack
}

// admits reports whether some line behind s can pass triggerMatches' kind,
// Phase$, Origin$ and Destination$ gates for ev during step (the matching
// engine's G.Step, which phaseGate reads) -- an over-approximation of the
// lines' conjunction.
func (s trigSig) admits(ev *events.Event, step state.Step) bool {
	if !s.kinds.has(ev.Kind) {
		return false
	}
	if s.steps != allSteps && !state.StepSet(s.steps).Has(step) {
		return false
	}
	if zoneChangeKind(ev.Kind) {
		return zoneMaskHas(s.zcFrom, ev.From) && zoneMaskHas(s.zcTo, ev.To)
	}
	return true
}

var zoneChangeKindsMask = func() trigKinds {
	var m trigKinds
	for _, k := range []events.Kind{events.MoveZone, events.Draw, events.PutOnStack} {
		m[k>>6] |= 1 << (k & 63)
	}
	return m
}()

// lineTrigSig is one trigger line's signature (valid: see computeFaceTrigSig).
func lineTrigSig(t *cards.Trigger, valid func(string) bool) trigSig {
	sig := trigSig{steps: allSteps}
	if spec := t.ParamStr(cards.PKPhase); strings.TrimSpace(spec) != "" {
		if !valid(spec) {
			return allTrigSig
		}
		// phaseGate: a valid spec holds only while G.Step is in its set
		// (narrowed further by PhaseCount$ and the first-strike mapping).
		sig.steps = uint16(parsePhaseSpec(spec).set)
	}
	sig.kinds = modeTrigKinds(t.ModeKind())
	if sig.kinds[0]&zoneChangeKindsMask[0] == 0 && sig.kinds[1]&zoneChangeKindsMask[1] == 0 {
		return sig
	}
	sig.zcFrom, sig.zcTo = ^uint32(0), ^uint32(0)
	if k := t.ModeKind(); k != cards.TriggerChangesZone && k != cards.TriggerChangesZoneAll {
		return sig
	}
	// trigmatch.ZoneChangeMatchesWithCapture's own reads, in its order.
	if o, ok := t.ParamCode(cards.PKOrigin); ok {
		switch zl := effects.ZoneList(o); {
		case !zl.OK():
			sig.zcFrom = 0
		case !zl.All():
			sig.zcFrom = zl.Zones()
		}
	}
	if d, ok := t.ParamCode(cards.PKDestination); ok && !effects.Destination(d).IsAny() {
		sig.zcTo = effects.Destination(d).Zones()
	}
	return sig
}

var allTrigKinds = trigKinds{^uint64(0), ^uint64(0)}

func (m trigKinds) has(k events.Kind) bool {
	if k >= 128 {
		return true
	}
	return m[k>>6]&(1<<(k&63)) != 0
}

func (m trigKinds) or(o trigKinds) trigKinds { return trigKinds{m[0] | o[0], m[1] | o[1]} }

// modeTrigKinds is the exact kind set triggerMatches can admit for mode.
func modeTrigKinds(mode cards.TriggerMode) trigKinds {
	low := triggerModeEventMasks[mode]
	if low == allTriggerEvents {
		return allTrigKinds
	}
	if modeRejectsHighKinds(mode) {
		return trigKinds{uint64(low), 0}
	}
	return trigKinds{uint64(low), ^uint64(0)}
}

// modeRejectsHighKinds names the modes whose registered matcher was audited
// to return false for every event kind at or past triggerMaskKindBits (the
// kinds triggerModeEvents cannot express and triggerMatches' leading gate
// therefore admits). Each matcher's first statement rejects every kind but
// the listed low ones:
//
//	ChangesZone, ChangesZoneAll   trigmatch.ZoneChangeMatchesWithCapture: MoveZone/Draw/PutOnStack
//	SpellCast                     trigmatch.spellCastMatches: PutOnStack
//	SpellCastOrCopy, SpellCopy    trigmatch.spellCopyMatches: StackCopy, else trigmatch.spellCastMatches
//	Attacks                       trigmatch.AttacksMatches: DeclareAttackers
//	AttackersDeclared(OneTarget)  trigmatch.AttackersDeclaredOneTargetMatches: DeclareAttackers
//	Untaps                        trigmatch.untapsMatches: Untap
//	Taps, TapsForMana             trigmatch.tapsMatches: Tap
//	Sacrificed                    events.IsSacrifice: MoveZone
//	Discarded, DiscardedAll       events.IsDiscard: MoveZone
//	Milled, MilledAll             events.IsMill: MoveZone
//	LandPlayed                    trigmatch.landPlayedMatches: MoveZone
//	Explores                      trigmatch.exploresMatches: Explore
//	BecomeMonarch                 trigmatch.BecomeMonarchMatches: MonarchChange
//	CommitCrime                   trigmatch.commitCrimeMatches: TargetsChosen
//	BecomesTarget(Once)           becomesTarget(Once)Matches: TargetsChosen
//	Attached                      trigmatch.attachedMatches: Attach
//	Exerted                       trigmatch.ExertedMatches: Exert
//	DamageDone/DealtOnce/DoneOnce/All  trigmatch.DamageMatchesWithCapture: Damage
//	CounterAdded(Once)            CounterChange
//	ClassLevelGained             ClassLevelChange (high kind; not rejected)
//	Transformed                   trigmatch.TransformedMatches: FlipFace
//	TokenCreated(Once)            trigmatch.tokenCreatedMatches: TokenCreate
//	Drawn                         trigmatch.drawnMatches: Draw
//	LifeLost                      lifeLoss: Damage/LifeChange (LifeLostAll is not listed)
//	LifeGained                    trigmatch.lifeGainedMatches: LifeChange
//	Phase                         trigmatch.PhaseMatches: StepChange
//
// A mode not listed keeps every high kind (the fail-open reading).
func modeRejectsHighKinds(mode cards.TriggerMode) bool {
	return modeRejectsHighKindsTab[mode]
}

// computeFaceTrigSigs is a face's exact signature, the union of its lines'
// (all), and the same union over only the lines that can act for a source
// that is not one of the event's referents (other): a referent-only line
// (lineReferentOnly) contributes to all alone. valid reports whether a Phase$
// spec parses (phaseSpecValid, or the engine's memo of it). The Phase$ read is
// the walk's own (ParamStr(PKPhase)).
func computeFaceTrigSigs(f *cards.Face, valid func(string) bool) (all, other trigSig) {
	for i := range f.Triggers {
		t := &f.Triggers[i]
		sig := lineTrigSig(t, valid)
		all = all.or(sig)
		if sig != allTrigSig && lineReferentOnly(t) {
			continue
		}
		other = other.or(sig)
	}
	return all, other
}

// lineReferentOnly reports whether t can match only when its source is the
// event's own object, so a walk may leave it to the referent's in-place
// visit: a Mode$ ChangesZone/ChangesZoneAll line whose card filter
// (ValidCards$, else ValidCard$ -- trigmatch.ZoneChangeMatchesWithCapture's read) is
// "<Type>.Self", optionally with further "+"-joined properties. The filter is
// matched against ev.Obj (or its LKI, the same id) under a spec context whose
// Source is the trigger's source, and the Self predicate is o.ID == Source.
func lineReferentOnly(t *cards.Trigger) bool {
	if k := t.ModeKind(); k != cards.TriggerChangesZone && k != cards.TriggerChangesZoneAll {
		return false
	}
	v, ok := t.Param(cards.PKValidCards)
	if !ok {
		v, ok = t.Param(cards.PKValidCard)
	}
	return ok && selfOnlySpec(v)
}

// selfOnlySpec reports whether spec is a single filter alternative whose
// property list starts with Self: "<Type>.Self" or "<Type>.Self+...", with no
// "," alternative and no white space.
func selfOnlySpec(spec string) bool {
	dot := strings.IndexByte(spec, '.')
	if dot <= 0 || strings.ContainsAny(spec, ", \t\n\r!") {
		return false
	}
	for i := 0; i < dot; i++ {
		c := spec[i]
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			return false
		}
	}
	rest := spec[dot+1:]
	return rest == "Self" || strings.HasPrefix(rest, "Self+")
}

// computeFaceLookBackZones is the bit set (by trigZoneSlot) of the zones from
// which some trigger line of f survives the leaves-the-battlefield look-back
// walk's split gate -- Mode$ ChangesZone with Origin$ exactly Battlefield
// (checkFaceTriggers' leaving test) -- for a source that is not the event's
// own object: zoneGate's spec for it (TriggerZones$, then ActiveZones$, then
// Battlefield) must contain the source's zone. Phase$ diagnostics do not run
// in the look-back walk, so they add nothing.
func computeFaceLookBackZones(f *cards.Face) uint8 {
	var m uint8
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if t.Mode != "ChangesZone" || t.ParamStr(cards.PKOrigin) != "Battlefield" {
			continue
		}
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

// faceTrigSig is f's exact signature: from the shared compiled face facts
// when current, else computed and cached per engine (fixture and synthetic
// faces), like faceTriggerZones.
func (e *Engine) faceTrigSig(f *cards.Face) trigSig {
	if f == nil || len(f.Triggers) == 0 {
		return trigSig{}
	}
	if ff := e.walkFaceFactsOf(f); ff != nil && ff.triggersCurrent(f) {
		return ff.trigSig
	}
	return e.faceTrigCached(f).sig
}

// faceTrigSigOther is faceTrigSig's non-referent half (computeFaceTrigSigs).
func (e *Engine) faceTrigSigOther(f *cards.Face) trigSig {
	if f == nil || len(f.Triggers) == 0 {
		return trigSig{}
	}
	if ff := e.walkFaceFactsOf(f); ff != nil && ff.triggersCurrent(f) {
		return ff.trigSigOther
	}
	return e.faceTrigCached(f).other
}

// faceTrigCached is the per-engine cache entry for a face outside the
// compiled face table.
func (e *Engine) faceTrigCached(f *cards.Face) faceTrigCache {
	if c, ok := e.trigFaceKinds[f]; ok {
		return c
	}
	var c faceTrigCache
	c.sig, c.other = computeFaceTrigSigs(f, func(spec string) bool { return e.parsedPhaseSpec(spec).valid })
	c.lookBack = computeFaceLookBackZones(f)
	if e.trigFaceKinds == nil {
		e.trigFaceKinds = make(map[*cards.Face]faceTrigCache)
	}
	e.trigFaceKinds[f] = c
	return c
}

// faceLookBackZones is computeFaceLookBackZones served like faceTrigSig.
func (e *Engine) faceLookBackZones(f *cards.Face) uint8 {
	if f == nil || len(f.Triggers) == 0 {
		return 0
	}
	if ff := e.walkFaceFactsOf(f); ff != nil && ff.triggersCurrent(f) {
		return ff.trigLookBack
	}
	return e.faceTrigCached(f).lookBack
}

type faceTrigCache struct {
	sig, other trigSig
	lookBack   uint8
}

// objectTrigSig is the union of the non-referent signatures
// (faceTrigSigOther) of every face o could walk: each face of its card and
// its CopyFace (so a face flip in place changes nothing), or everything for
// an unlocked Room or a merged pile, whose walked faces are not only the
// current one. It answers for o as a NON-referent: a referent is always
// visited in place.
func (e *Engine) objectTrigSig(o *state.Object) trigSig {
	if o.RoomOtherDoorUnlocked() || len(o.MergedCards) > 0 {
		return allTrigSig
	}
	var m trigSig
	if o.CopyFace != nil {
		m = e.faceTrigSigOther(o.CopyFace)
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			m = m.or(e.faceTrigSigOther(f))
		}
	}
	return m
}

// objectLookBackHot reports whether the leaves-the-battlefield look-back walk
// might do anything for o other than as an event referent: o's visit can
// reach a match only through a look-back-shaped printed line of a face it
// could walk whose zone spec holds o's zone (computeFaceLookBackZones;
// zoneGate admits a non-referent source by exactly that test), or as an
// unlocked Room or a merged pile. Granted walks are referent-gated, and the
// caller disables the filter whenever a static-granted trigger observes the
// event.
func (e *Engine) objectLookBackHot(o *state.Object) bool {
	if o == nil || o.PhasedOut || o.Face() == nil {
		return false
	}
	if o.RoomOtherDoorUnlocked() || len(o.MergedCards) > 0 {
		return true
	}
	slot := trigZoneSlot(o.Zone)
	if slot < 0 {
		return true
	}
	bit := uint8(1) << slot
	if o.CopyFace != nil && e.faceLookBackZones(o.CopyFace)&bit != 0 {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if e.faceLookBackZones(f)&bit != 0 {
				return true
			}
		}
	}
	return false
}

// lookBackNoopBoard reports whether no leaves-the-battlefield look-back walk
// over the current board can act: no trigger grant is possible
// (trigGrantsPossible), and no object the walk visits is look-back-hot
// (objectLookBackHot, which for a departing referent is exactly its own
// look-back lines from the battlefield it sits on in the snapshot). A
// referent's visit can then reach only the granted keyword walks, and the
// only ones a MoveZone event reaches (Exploit, Offspring) return for any
// destination but the battlefield. The board is the one a window opened now
// would snapshot, so the answer holds for every departure of that window.
func (e *Engine) lookBackNoopBoard() bool {
	if e.trigGrantsPossible() {
		return false
	}
	for si, p := range e.G.AliveFrom(0) {
		// The battlefield first: it holds nearly every look-back source.
		for _, z := range [...]state.Zone{state.ZBattlefield, state.ZLibrary, state.ZHand,
			state.ZGraveyard, state.ZExile, state.ZStack, state.ZCommand} {
			if z == state.ZStack && si != 0 {
				continue
			}
			for _, id := range e.G.Zone(z, p) {
				if e.objectLookBackHot(e.G.Obj(id)) {
					return false
				}
			}
		}
	}
	return true
}

var modeRejectsHighKindsTab = [cards.TriggerModeCount]bool{
	cards.TriggerChangesZone:                true,
	cards.TriggerChangesZoneAll:             true,
	cards.TriggerSpellCast:                  true,
	cards.TriggerSpellCastOrCopy:            true,
	cards.TriggerSpellCopy:                  true,
	cards.TriggerAttacks:                    true,
	cards.TriggerAttackersDeclared:          true,
	cards.TriggerAttackersDeclaredOneTarget: true,
	cards.TriggerUntaps:                     true,
	cards.TriggerTaps:                       true,
	cards.TriggerTapsForMana:                true,
	cards.TriggerSacrificed:                 true,
	cards.TriggerDiscarded:                  true,
	cards.TriggerDiscardedAll:               true,
	cards.TriggerMilled:                     true,
	cards.TriggerMilledAll:                  true,
	cards.TriggerLandPlayed:                 true,
	cards.TriggerExplores:                   true,
	cards.TriggerBecomeMonarch:              true,
	cards.TriggerCommitCrime:                true,
	cards.TriggerBecomesTarget:              true,
	cards.TriggerBecomesTargetOnce:          true,
	cards.TriggerAttached:                   true,
	cards.TriggerExerted:                    true,
	cards.TriggerDamageDone:                 true,
	cards.TriggerDamageDealtOnce:            true,
	cards.TriggerDamageDoneOnce:             true,
	cards.TriggerDamageAll:                  true,
	cards.TriggerExcessDamageAll:            true,
	cards.TriggerCounterAdded:               true,
	cards.TriggerCounterAddedOnce:           true,
	cards.TriggerCounterAddedAll:            true,
	cards.TriggerCounterTypeAddedAll:        true,
	cards.TriggerTransformed:                true,
	cards.TriggerTokenCreated:               true,
	cards.TriggerTokenCreatedOnce:           true,
	cards.TriggerDrawn:                      true,
	cards.TriggerLifeLost:                   true,
	cards.TriggerLifeGained:                 true,
	cards.TriggerPhase:                      true,
}
