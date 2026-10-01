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
// only when zoneChangeMatchesWithCapture's Origin$ and Destination$ tests
// admit ev.From and ev.To, so a face whose zone-change listeners are all
// "enters the battlefield" lines cannot act on a draw, a cast or a death.
type trigKinds [2]uint64

// trigSig is a face's (or an object's) exact event signature: the kind mask,
// and for the zone-change kinds the union of the From and To zones their
// listening lines admit (bit z for zone z < 32; a zone at or past 32 is
// always admitted).
type trigSig struct {
	kinds  trigKinds
	zcFrom uint32
	zcTo   uint32
}

var allTrigSig = trigSig{kinds: allTrigKinds, zcFrom: ^uint32(0), zcTo: ^uint32(0)}

func (s trigSig) or(o trigSig) trigSig {
	return trigSig{kinds: s.kinds.or(o.kinds), zcFrom: s.zcFrom | o.zcFrom, zcTo: s.zcTo | o.zcTo}
}

func zoneMaskHas(m uint32, z state.Zone) bool { return z >= 32 || m&(1<<z) != 0 }

// zoneChangeKind: the kinds zoneChangeMatchesWithCapture accepts.
func zoneChangeKind(k events.Kind) bool {
	return k == events.MoveZone || k == events.Draw || k == events.PutOnStack
}

// admits reports whether some line behind s can pass triggerMatches' kind,
// Origin$ and Destination$ gates for ev (an over-approximation of the
// lines' conjunction).
func (s trigSig) admits(ev *events.Event) bool {
	if !s.kinds.has(ev.Kind) {
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
	if spec := t.ParamStr(cards.PKPhase); strings.TrimSpace(spec) != "" && !valid(spec) {
		return allTrigSig
	}
	sig := trigSig{kinds: modeTrigKinds(t.Mode)}
	if sig.kinds[0]&zoneChangeKindsMask[0] == 0 && sig.kinds[1]&zoneChangeKindsMask[1] == 0 {
		return sig
	}
	sig.zcFrom, sig.zcTo = ^uint32(0), ^uint32(0)
	if t.Mode != "ChangesZone" && t.Mode != "ChangesZoneAll" {
		return sig
	}
	// zoneChangeMatchesWithCapture's own reads, in its order.
	if o, ok := t.Params["Origin"]; ok {
		zones, all, listOK := effects.ParseZones(o)
		switch {
		case !listOK:
			sig.zcFrom = 0
		case !all:
			sig.zcFrom = 0
			for _, z := range zones {
				if z >= 32 {
					sig.zcFrom = ^uint32(0)
					break
				}
				sig.zcFrom |= 1 << z
			}
		}
	}
	if d, ok := t.Params["Destination"]; ok && d != "Any" {
		if z := effects.ParseZone(d); z < 32 {
			sig.zcTo = 1 << z
		}
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
func modeTrigKinds(mode string) trigKinds {
	low := triggerModeEvents(mode)
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
//	ChangesZone, ChangesZoneAll   zoneChangeMatchesWithCapture: MoveZone/Draw/PutOnStack
//	SpellCast                     spellCastMatches: PutOnStack
//	SpellCastOrCopy, SpellCopy    spellCopyMatches: StackCopy, else spellCastMatches
//	Attacks                       attacksMatches: DeclareAttackers
//	AttackersDeclared(OneTarget)  attackersDeclaredOneTargetMatches: DeclareAttackers
//	Untaps                        untapsMatches: Untap
//	Taps, TapsForMana             tapsMatches: Tap
//	Sacrificed                    events.IsSacrifice: MoveZone
//	Discarded, DiscardedAll       events.IsDiscard: MoveZone
//	Milled, MilledAll             events.IsMill: MoveZone
//	LandPlayed                    landPlayedMatches: MoveZone
//	Explores                      exploresMatches: Explore
//	BecomeMonarch                 becomeMonarchMatches: MonarchChange
//	CommitCrime                   commitCrimeMatches: TargetsChosen
//	BecomesTarget(Once)           becomesTarget(Once)Matches: TargetsChosen
//	Attached                      attachedMatches: Attach
//	Exerted                       exertedMatches: Exert
//	DamageDone/DealtOnce/DoneOnce/All  damageMatchesWithCapture: Damage
//	CounterAdded(Once), ClassLevelGained  CounterChange
//	Transformed                   transformedMatches: FlipFace
//	TokenCreated(Once)            tokenCreatedMatches: TokenCreate
//	Drawn                         drawnMatches: Draw
//	LifeLost                      lifeLoss: Damage/LifeChange (LifeLostAll is not listed)
//	LifeGained                    lifeGainedMatches: LifeChange
//	Phase                         phaseMatches: StepChange
//
// A mode not listed keeps every high kind (the fail-open reading).
func modeRejectsHighKinds(mode string) bool {
	switch mode {
	case "ChangesZone", "ChangesZoneAll", "SpellCast", "SpellCastOrCopy", "SpellCopy",
		"Attacks", "AttackersDeclared", "AttackersDeclaredOneTarget", "Untaps", "Taps", "TapsForMana",
		"Sacrificed", "Discarded", "DiscardedAll", "Milled", "MilledAll", "LandPlayed", "Explores",
		"BecomeMonarch", "CommitCrime", "BecomesTarget", "BecomesTargetOnce", "Attached", "Exerted",
		"DamageDone", "DamageDealtOnce", "DamageDoneOnce", "DamageAll",
		"CounterAdded", "CounterAddedOnce", "ClassLevelGained", "Transformed",
		"TokenCreated", "TokenCreatedOnce", "Drawn", "LifeLost", "LifeGained", "Phase":
		return true
	}
	return false
}

// computeFaceTrigSig is a face's exact signature, the union of its lines';
// valid reports whether a Phase$ spec parses (phaseSpecValid, or the
// engine's memo of it). The Phase$ read is the walk's own (ParamStr(PKPhase)).
func computeFaceTrigSig(f *cards.Face, valid func(string) bool) trigSig {
	var m trigSig
	for i := range f.Triggers {
		m = m.or(lineTrigSig(&f.Triggers[i], valid))
	}
	return m
}

// computeFaceLookBack reports whether some trigger line of f survives the
// leaves-the-battlefield look-back walk's split gate: Mode$ ChangesZone with
// Origin$ exactly Battlefield (checkFaceTriggers' leaving test).
func computeFaceLookBack(f *cards.Face) bool {
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if t.Mode == "ChangesZone" && t.ParamStr(cards.PKOrigin) == "Battlefield" {
			return true
		}
	}
	return false
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
	if c, ok := e.trigFaceKinds[f]; ok {
		return c.sig
	}
	c := faceTrigCache{
		sig:      computeFaceTrigSig(f, func(spec string) bool { return e.parsedPhaseSpec(spec).valid }),
		lookBack: computeFaceLookBack(f),
	}
	if e.trigFaceKinds == nil {
		e.trigFaceKinds = make(map[*cards.Face]faceTrigCache)
	}
	e.trigFaceKinds[f] = c
	return c.sig
}

// faceLookBack is computeFaceLookBack served like faceTrigKinds.
func (e *Engine) faceLookBack(f *cards.Face) bool {
	if f == nil || len(f.Triggers) == 0 {
		return false
	}
	if ff := e.walkFaceFactsOf(f); ff != nil && ff.triggersCurrent(f) {
		return ff.trigLookBack
	}
	e.faceTrigSig(f)
	return e.trigFaceKinds[f].lookBack
}

type faceTrigCache struct {
	sig      trigSig
	lookBack bool
}

// objectTrigSig is the union of the signatures of every face o could walk:
// each face of its card and its CopyFace (so a face flip in place changes
// nothing), or everything for an unlocked Room or a merged pile, whose
// walked faces are not only the current one.
func (e *Engine) objectTrigSig(o *state.Object) trigSig {
	if o.Unlocked || len(o.MergedCards) > 0 {
		return allTrigSig
	}
	var m trigSig
	if o.CopyFace != nil {
		m = e.faceTrigSig(o.CopyFace)
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			m = m.or(e.faceTrigSig(f))
		}
	}
	return m
}

// objectLookBackHot reports whether the leaves-the-battlefield look-back walk
// might do anything for o other than as an event referent: o's visit can
// reach triggerMatches only through a look-back-shaped printed line (see
// computeFaceLookBack) of a face it could walk, or as an unlocked Room or a
// merged pile. Granted walks are referent-gated, and the caller disables the
// filter whenever a static-granted trigger observes the event.
func (e *Engine) objectLookBackHot(o *state.Object) bool {
	if o == nil || o.PhasedOut || o.Face() == nil {
		return false
	}
	if o.Unlocked || len(o.MergedCards) > 0 {
		return true
	}
	if o.CopyFace != nil && e.faceLookBack(o.CopyFace) {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if e.faceLookBack(f) {
				return true
			}
		}
	}
	return false
}
