package view

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// RedactEvents strips what a viewer must not see out of a batch of events,
// building a copy of each -- the log is shared across every seat's
// projection, and a live rules.Engine's own log must never be touched by a
// read path.
//
// Call with the game state as of the LAST event in evs: rules (2) and (3)
// below ask "is this object CURRENTLY sitting in a hidden zone", and
// "currently" means at that point in the log, not at the moment the event
// was originally emitted. A live stream redacts each newly-appended batch
// against the game that batch just produced; a playback viewer (Task 24)
// passes the state reconstructed as of that point in the log, not the
// game's eventual final state.
//
// Three rules, applied to a COPY of each event -- the input slice, and
// every event in it, is never mutated:
//
//  1. Secret: rules (2)/(3) below never apply to a Secret event, for
//     EITHER branch of this rule -- a Secret event is wholly its emitter's
//     to redact-or-not, not something additionally filtered id-by-id.
//     - Player != viewer: keep ONLY the event's shape (Seq, Kind,
//     Player, From, To, Step, Secret) and zero every payload field
//     (Obj, Amount, Counter, Text, IDs, Pairs). This is an allowlist
//     of what SURVIVES redaction, not an enumerated list of what to
//     strip from it (Ruling T23-s, closing review finding I-2): a
//     future Secret emitter that starts carrying Pairs or Amount is
//     covered automatically, which an enumerated strip list is not.
//     - Player == viewer: pass through untouched -- it is their own
//     secret, EXCEPT PlanarDeckShuffle, whose ordered IDs must remain
//     private even from the deck's owner.
//  2. Not Secret, a zone-move kind (MoveZone, Draw, PutOnStack): its Obj is
//     stripped when the move is entirely between hidden zones
//     (From.Hidden() && To.Hidden()) and the moved object is not visible
//     to viewer (visibleTo, below). A move FROM a public zone stays public
//     -- everyone already saw the permanent bounce to hand.
//  3. Not Secret, every other kind EXCEPT Note: any id referenced by Obj,
//     IDs or Pairs is stripped when it is not visible to viewer. Note is
//     exempt (Ruling T23-w): a Note is the engine's explicit "tell
//     everyone" channel -- effReveal/effRevealHand/effPeekAndReveal
//     (effects/cardflow.go) exist specifically to show hidden cards to
//     EVERY seat via a public Note, so a Note is public unless its own
//     emitter says otherwise by setting Secret, which rule (1) above
//     already handles in full. effRearrangeTopOfLibrary's Note (the one
//     Note that must NOT be public -- a private look at the library) is
//     therefore Secret itself, not filtered by this rule.
//
// Rule (3) is what closes review finding C-1's TriggerPush half:
// rules/trigger.go's TriggerPush.IDs (Ctx.Remembered; for a "whenever you
// draw a card" trigger, the ChangesZone-mode T: line this engine uses to
// express that, this is the card now sitting in its owner's hand) names a
// hidden-zone object without ever setting Secret, and cannot be fixed by
// widening the Secret+Player contract at the emitter: TriggerPush's Player
// is the trigger's CONTROLLER (Apply needs it to mint the ability object),
// which is not the same seat as the remembered card's OWNER, so
// Secret+Player cannot express "this payload belongs to someone other than
// the event's own Player" for that case at all. Redaction has to be
// state-aware instead, checking each referenced id's actual owner against
// the game rather than trusting the event's own Player field to name it.
// C-1's other half, effRearrangeTopOfLibrary's Note, is closed by making it
// Secret (see effects/cardflow.go) rather than by rule (3), per Ruling
// T23-w above.
//
// visibleTo's "an unresolvable id is not visible" is the safe default for
// both rule (2) and rule (3): an id nothing can currently resolve (stale
// data, a tampered log) cannot be proven safe to show, so it is treated the
// same as one this function CAN prove is hidden from viewer.
//
// A stripped id inside IDs leaves a SHORTER slice, not a zero hole in
// place; a Pair with either side stripped is dropped as a whole pair.
//
// g == nil applies rule (1) only -- there is no game state to check rules
// (2)/(3) against -- and never panics.
//
// RedactEventsFor (visibility.go) is this function plus one more mode:
// RedactEventsFor(..., Seat) and RedactEventsFor(..., Public) are exactly
// RedactEvents (Public always passes NoSeat, so every owner-only branch
// above stays closed); Omniscient is the one case RedactEvents itself never
// implements -- every event visible except library order.
func RedactEvents(g *state.Game, evs []events.Event, viewer state.PlayerID) []events.Event {
	out := make([]events.Event, 0, len(evs))
	for _, e := range evs {
		out = append(out, RedactEvent(g, e, viewer))
	}
	return out
}

// RedactEvent redacts ONE event for viewer; the per-event half of
// RedactEvents, split out so a hot caller that wants the redacted form of
// each input can redact into its own output without first building an
// intermediate []events.Event (the whole-slice version has to allocate a
// len(evs) backing array just to have something to append into). Rules are
// identical to RedactEvents'; see its doc for rules (1)/(2)/(3) and the
// state-as-of-last-event convention.
//
// Deep-copying (final review finding I2): e is a struct copy of the caller's
// event, but e.IDs and e.Pairs are slice HEADERS -- copying the header does
// not copy the backing array. Every branch below that returns e unchanged
// would otherwise still point at the caller's own arrays. filterVisible/
// filterVisiblePairs already return fresh slices, so the rule-3 default
// branch is never the problem; the owner's-own-secret pass-through, the
// g==nil degrade, and the zone-move/Note paths that fall through to the
// final return all are. Deep-copying both, unconditionally, before any
// branch runs, is what makes this function's "the input event is never
// mutated" promise true: a caller can now freely mutate the returned
// event's IDs/Pairs without ever touching the engine's own log. Measured
// before this fix: redacting a real game's log for one seat returned 50
// events whose IDs[0] aliased the engine's own logged event (that seat's
// own Shuffle, i.e. its entire library order); mutating one permanently
// desynced Log.Head() from Log.HeadAt(len(Log.Events)), breaking replay of
// that match for good.
func RedactEvent(g *state.Game, e events.Event, viewer state.PlayerID) events.Event {
	e.IDs = append([]state.ObjID(nil), e.IDs...)
	e.Pairs = append([][2]state.ObjID(nil), e.Pairs...)
	if e.Secret {
		// Unlike a library shuffle, a planar-deck shuffle's order is hidden
		// from every seat, including its owner (CR 901.5).
		if e.Player != viewer || e.Kind == events.PlanarDeckShuffle {
			return events.Event{
				Seq: e.Seq, Kind: e.Kind, Player: e.Player,
				From: e.From, To: e.To, Step: e.Step, Secret: e.Secret,
			}
		}
		// The owner's own secret: rule 1 does not apply to them at all,
		// and neither do rules (2)/(3) -- a Secret event is wholly its
		// emitter's to redact-or-not, not something this function
		// additionally filters id-by-id even for its owner.
		return e
	}
	if g == nil {
		return e
	}
	switch e.Kind {
	case events.MoveZone, events.Draw, events.PutOnStack:
		if e.From.Hidden() && e.To.Hidden() && !visibleTo(g, e.Obj, viewer) {
			e.Obj = 0
		}
		// T23-z: rule 2 above only ever narrowed Obj; IDs/Pairs on a
		// zone-move kind got no filtering at all, unlike every other kind
		// (rule 3's default branch below). Measured behaviour-neutral
		// today (0 non-test emitters of MoveZone/Draw/PutOnStack carry IDs
		// or Pairs), so no chain head can move -- this closes the allowlist
		// gap for whenever one starts to.
		e.IDs = filterVisible(g, e.IDs, viewer)
		e.Pairs = filterVisiblePairs(g, e.Pairs, viewer)
	case events.Note:
		// Ruling T23-w: rule 3 does not apply to a non-Secret Note at all
		// -- it passes through unchanged. A Note is the engine's explicit
		// "tell everyone" channel (Reveal/RevealHand/PeekAndReveal exist
		// to show hidden cards to every seat via one), so it is public
		// unless the emitter opted it into Secret instead, which the
		// branch above already handles. effRearrangeTopOfLibrary's private
		// look is the counterexample that must NOT be public, and it is
		// Secret for exactly that reason -- not filtered here.
	default:
		if !visibleTo(g, e.Obj, viewer) {
			e.Obj = 0
		}
		e.IDs = filterVisible(g, e.IDs, viewer)
		e.Pairs = filterVisiblePairs(g, e.Pairs, viewer)
	}
	return e
}

// visibleTo reports whether id is safe to show viewer: the object it names
// currently exists, and either its zone is not hidden or viewer is its
// owner. An id nothing can currently resolve (including ObjID 0, "no
// object") is not visible -- the safe default (see RedactEvents' doc).
func visibleTo(g *state.Game, id state.ObjID, viewer state.PlayerID) bool {
	o := g.Obj(id)
	if o == nil {
		return false
	}
	if o.Zone == state.ZPlanarDeck && !o.FaceDown {
		return true
	}
	return !o.Zone.Hidden() || o.Owner == viewer
}

// filterVisible drops every id not visible to viewer, shortening the slice
// rather than leaving a zero hole. Always a fresh slice: never the input's
// own backing array, so the caller's copy of ids is never touched.
func filterVisible(g *state.Game, ids []state.ObjID, viewer state.PlayerID) []state.ObjID {
	out := make([]state.ObjID, 0, len(ids))
	for _, id := range ids {
		if visibleTo(g, id, viewer) {
			out = append(out, id)
		}
	}
	return out
}

// filterVisiblePairs drops a whole pair when either side is not visible to
// viewer -- a pair with only one id redacted would still leak the other
// half of the relationship.
func filterVisiblePairs(g *state.Game, pairs [][2]state.ObjID, viewer state.PlayerID) [][2]state.ObjID {
	out := make([][2]state.ObjID, 0, len(pairs))
	for _, p := range pairs {
		if visibleTo(g, p[0], viewer) && visibleTo(g, p[1], viewer) {
			out = append(out, p)
		}
	}
	return out
}
