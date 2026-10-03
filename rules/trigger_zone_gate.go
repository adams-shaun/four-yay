package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// zoneGate implements TriggerZones$: a trigger only fires while its source is
// in one of the listed zones. The default is the battlefield, which is why an
// enchantment's upkeep trigger stops when it is destroyed.
//
// checkTriggers calls this after the event has already been folded into
// state (emit logs before it checks triggers), so o.Zone alone only ever
// reflects the zone the object is in *now*. That is correct for an
// entering-the-zone trigger (Snapcaster's ETB: o.Zone is already
// Battlefield by the time this runs) but wrong for a leaving-the-zone one --
// a plain "dies" trigger (Origin$ Battlefield, Destination$ Graveyard,
// ValidCard$ Card.Self, default TriggerZones$ Battlefield) would never see
// its own source "in" the battlefield, because by the time checkTriggers
// runs the move has already happened and o.Zone reads Graveyard. CR 603.10's
// full "look back in time" is not modeled, but the one case Task 20's own
// ChangesZone mode needs it for is narrow and self-contained: when the event
// under test is itself the zone change of this trigger's own source (source
// == ev.Obj), the zone it was in immediately before (ev.From) counts as well
// as the zone it is in now, so both an ETB and a dies trigger with the
// ordinary default work from the same rule.
func (e *Engine) zoneGate(t cards.Trigger, source state.ObjID, ev events.Event) bool {
	o := e.G.Obj(source)
	if o == nil {
		return false
	}
	spec := t.ParamStr(cards.PKTriggerZones)
	if spec == "" {
		// Forge's ActiveZones$ is the trigger-side spelling of the same gate
		// (the replacement side already reads the key:
		// rules/replacement.go's zone gate). Sower of Discord's two
		// DamageDoneOnce halves declare ActiveZones$ Battlefield; an explicit
		// ActiveZones$ is authoritative exactly like an explicit
		// TriggerZones$, so the two special cases below keep treating it as
		// declared.
		spec = t.ParamStr(cards.PKActiveZones)
	}
	if spec == "" && ev.Kind == events.PutOnStack && source == ev.Obj && t.Mode == "SpellCast" {
		// CR 601.2i: the spell's OWN cast trigger fires while the source is
		// the spell sitting on the stack -- exactly the event being walked.
		// The battlefield default would gate it out (the source is in ZStack,
		// and the PutOnStack look-back zone below is the zone it came FROM,
		// the hand), so every bare "When you cast this spell" script --
		// Hydroid Krasis, Genesis Hydra, Ulamog, World Breaker -- would
		// never fire at all. An EXPLICIT TriggerZones$ stays authoritative:
		// a script naming one knows where its trigger lives.
		return true
	}
	if spec == "" {
		// "When you discard this card" (Orvar, Bartered Cow, Titanbones: 14
		// of the corpus's Mode$ Discarded lines) declares no TriggerZones$,
		// and the only zone a card is discarded from is its owner's hand (CR
		// 701.9a). Forge applies no zone restriction to a trigger without
		// TriggerZones$; the battlefield default below would leave such a
		// trigger unable to fire at all, so the card's own discard admits it
		// wherever the discard (or a replacement redirecting it) put it.
		if t.Mode == "Discarded" && source == ev.Obj && events.IsDiscard(ev) {
			return true
		}
		// The cycled card itself is the moved card (ValidCard$ Card.Self):
		// its own cycle-trigger must fire from wherever the cost discard (or a
		// replacement redirecting it) put it, the same courtesy the Discarded
		// case above extends.
		if t.Mode == "Cycled" && source == ev.Obj && events.IsDiscard(ev) {
			return true
		}
		// A card's own hand->exile move (Lupine Harbingers' "note the number
		// of turns you've begun since it was foretold" exile trigger -- the
		// corpus's ONE ChangesZone self-trigger with no TriggerZones$ whose
		// destination is not the battlefield, measured over the corpus pin):
		// the only zone the trigger can observe the move from is the hand the
		// card sits in. Forge applies no zone restriction to a trigger
		// without TriggerZones$; the battlefield default would leave such a
		// trigger unable to fire at all, so the card's own hand-origin move
		// admits it, the same courtesy the Discarded and Cycled cases above
		// extend. (Self-moves whose DESTINATION is the battlefield need no
		// admission: the post-move zone check below already sees them.)
		if t.Mode == "ChangesZone" && source == ev.Obj && ev.Kind == events.MoveZone &&
			ev.From == state.ZHand {
			return true
		}
		spec = "Battlefield"
	}
	zones := [2]state.Zone{o.Zone, o.Zone}
	n := 1
	if source == ev.Obj && ev.Obj != 0 &&
		(ev.Kind == events.MoveZone || ev.Kind == events.Draw || ev.Kind == events.PutOnStack) {
		zones[1] = ev.From
		n = 2
	}
	for _, zone := range zones[:n] {
		if zoneSpecContains(spec, zone) {
			return true
		}
	}
	return false
}

// zoneSpecContains reports whether spec (a Forge TriggerZones value -- a
// comma-separated list of zone names, constant for the life of the card)
// lists want. It scans the string by slicing comma-separated parts apart with
// strings.Cut, which shares the backing string and allocates nothing, instead
// of strings.Split (whose []string is a fresh allocation per call). zoneGate
// runs from the per-event trigger walk -- the same hot path Task A2 fixed the
// zone copy in -- so this avoids churning an allocation for every trigger on
// every event. Parts are handed to effects.ParseZone unchanged (it trims each
// name itself), so the result is byte-identical to the old
// strings.Split+TrimSpace+ParseZone loop, including its handling of empty
// leading/trailing/double-separator segments: an empty zone name parses to
// the graveyard, exactly as it always did.
func zoneSpecContains(spec string, want state.Zone) bool {
	// Walk separator-delimited segments by index so that, like strings.Split,
	// a spec ending in a separator still yields a final empty segment (which
	// effects.ParseZone resolves to the graveyard). strings.Cut would drop
	// that trailing Phantom Graveyard part and change behaviour on a malformed
	// spec; slicing keeps every segment while allocating nothing.
	i := 0
	for {
		j := strings.IndexByte(spec[i:], ',')
		var part string
		if j < 0 {
			part = spec[i:]
		} else {
			part = spec[i : i+j]
		}
		if effects.ParseZone(part) == want {
			return true
		}
		if j < 0 {
			return false
		}
		i += j + 1
	}
}

// zoneDelayedDestinationAdmits is the delayed-registration Destination$
// reader: a comma-separated zone list admits a move into any listed zone
// (Earthbend's "when it dies or is exiled" promise names Graveyard,Exile in
// one registration). It is deliberately separate from zoneChangeMatches,
// which reads Destination$ through the single-word effects.ParseZone: this
// only ever runs when the delayed-trigger arm sees a comma in the clause, so
// a face trigger -- and every single-zone delayed registration -- keeps the
// existing single-word reading unchanged (the engine-wide comma-Destination$
// defect is ledgered separately and is not fixed here).
func zoneDelayedDestinationAdmits(spec string, to state.Zone) bool {
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" || part == "Any" {
			continue
		}
		if z, ok := effects.ParseZoneWord(part); ok && z == to {
			return true
		}
	}
	return false
}
