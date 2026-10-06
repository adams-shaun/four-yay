// Event folds for zone movement: MoveZone/Draw/PutOnStack, library order, shuffles, search, explore, attach.
//
// Split out of events/apply.go: code moved verbatim, no behaviour
// change. Each fold function is the body of the matching case in
// Apply (g, e) switch; see apply.go for the dispatch.
package events

import (
	"fmt"

	"github.com/adams-shaun/gorge/state"
)

// foldMoveZone folds Kinds MoveZone, Draw, PutOnStack into state.
func foldMoveZone(g *state.Game, e *Event) {
	// CR 733.1 reverses a proposed cast with a real logged stack->origin
	// move. Preserve the entry history that preceded its stack proposal in
	// transient object state: a log-only replay sees the same PutOnStack,
	// captures the same fields and consumes them on the reverse move.
	wasStack := false
	// The object's REAL pre-move zone, not Event.From: Move itself treats
	// From as advisory (a malformed caller-supplied From must not corrupt
	// state), and the Ring-bearer clear below must follow the same rule --
	// otherwise a blob return/re-entry reusing the same ObjID could keep a
	// stale designation.
	wasBattlefield := false
	// The DrawnThisTurn stamp before the move: a CR 733.1 reversal
	// ("reversed" stack->origin move) undoes the proposal, so the card is
	// still the card that was drawn; Move's clear is reverted below.
	drawnBefore := int32(0)
	if o := g.Obj(e.Obj); o != nil {
		drawnBefore = o.DrawnTurn
		wasStack = o.Zone == state.ZStack
		wasBattlefield = o.Zone == state.ZBattlefield
		if e.To == state.ZStack {
			o.PreStackEntryThisTurn = o.EnteredThisTurn
			o.PreStackEntryFrom = o.EnteredFrom
			o.PreStackEnteredLen = len(g.Entered)
			o.HasPreStackEntry = true
		}
	}
	// A manifest's or cloak's face-down entry (CR 708.5) must be visible
	// INSIDE the Move below: Move's battlefield-entry grants read it (a
	// manifested planeswalker enters as a 2/2 creature with no loyalty
	// grant, a manifested Saga with no lore counter -- while face down it
	// is neither), so the marker folds onto the object before the move and
	// is re-asserted after it. A Counter value on the existing MoveZone
	// decode: no new event kind, no Event field change. The same marker
	// carries a ChangeZone FaceDown$ True entry's folded set type and
	// power/toughness (FaceDownSetType$/FaceDownPower$/FaceDownToughness$)
	// as an optional Counter payload; the bare marker is CR 708.5's plain
	// 2/2 creature. Cloak shares the face-down entry with a second Counter
	// value; only the cloak marker sets Cloaked, the state rules/layers.go
	// and trigger_match.go read for the ward {2}.
	moveCounter, countersRemain := CountersRemainMovePayload(e.Counter)
	if !countersRemain {
		moveCounter = e.Counter
	}
	setType, fdPower, fdTough, fdHasPT, manifesting := "", int32(0), int32(0), false, false
	if e.Kind == MoveZone && e.To == state.ZBattlefield {
		if moveCounter == CloakEntryCounter {
			manifesting = true
		} else {
			setType, fdPower, fdTough, fdHasPT, manifesting = FaceDownEntryFields(moveCounter)
		}
	}
	if manifesting {
		if o := g.Obj(e.Obj); o != nil {
			o.FaceDown = true
			o.Cloaked = e.Counter == CloakEntryCounter
			o.FaceDownSetType = setType
			o.FaceDownPower = fdPower
			o.FaceDownToughness = fdTough
			o.FaceDownHasPT = fdHasPT
		}
	}
	var sacrificer state.PlayerID
	sacrificed := IsSacrifice(*e)
	if sacrificed {
		if o := g.Obj(e.Obj); o != nil {
			sacrificer = o.Controller
		}
	}
	if e.Kind == MoveZone && countersRemain {
		MoveCountersRemain(g, e.Obj, e.From, e.To)
	} else {
		Move(g, e.Obj, e.From, e.To)
	}
	if e.Kind == Draw && e.To == state.ZHand {
		// The DrawnThisTurn stamp (state.Object.DrawnTurn): the card
		// was drawn on this turn.
		if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZHand {
			o.DrawnTurn = g.Turn
		}
	} else if wasStack && e.Text == "reversed" {
		if o := g.Obj(e.Obj); o != nil {
			o.DrawnTurn = drawnBefore
		}
	}
	if sacrificed {
		// Stamp the sacrifice onto this move's own zone entry (the
		// latest one naming the object: a mutated pile's under-cards
		// append after it). The controller was read BEFORE Move reset
		// it to the owner (CR 400.7).
		for i := len(g.Entered) - 1; i >= 0; i-- {
			if g.Entered[i].Obj == e.Obj {
				g.Entered[i].Sacrificed, g.Entered[i].Sacrificer = true, sacrificer
				break
			}
		}
	}
	if o := g.Obj(e.Obj); o != nil {
		// A WithMayLook$ look permission is a property of ONE face-down
		// exile, so every later move clears it: the "exiled_with_face_down_
		// maylook" branch below re-sets it for the exile that grants it.
		// Without this reset a card exiled face down a second time (or
		// otherwise moved) would keep a stale looker and leak its face.
		o.HasMayLook = false
		o.MayLookPlayer = 0
		if e.Kind == MoveZone && e.To == state.ZBattlefield && !wasBattlefield {
			applyEntryCounterPairs(o, e.Pairs)
		}
		if e.To == state.ZStack && o.Face() != nil {
			o.StackKind, o.StackKindKnown = state.StackKindSpell, true
		}
		if e.Kind == PutOnStack {
			// Record the zone and caster of this cast on the object, so
			// rules' latestCastOrigin reads the field instead of scanning
			// the whole log backwards on every call. A later PutOnStack
			// overwrites it (latest cast wins); the reverse move below does
			// NOT clear it, because the reverse scan it replaces still finds
			// this PutOnStack in the log after a CR 733.1 reversal. The fold
			// is the one writer, so a log-only replay derives the same pair.
			o.LatestCastFrom, o.LatestCastBy, o.HasLatestCast = e.From, e.Player, true
		}

		if e.To == state.ZStack && IsFaceDownEntry(moveCounter) {
			// CR 708.4: a face-down CAST's spell sits on the stack with no
			// name, no types and no abilities. The face-down entry marker
			// rides the PutOnStack (rules/cast.go's pushCast), and this fold
			// keeps Object.FaceDown on the stack object so the view redacts
			// its printed identity from everyone but its controller, and so
			// the resolution entry (moveResolvedOffStack's re-carried marker)
			// can tell a face-down spell from an ordinary one. The cloak
			// marker is the Disguise entry's carrier (the ward {2} a
			// disguised creature has while face down rides the same state
			// bit the Cloak machinery reads). No ordinary PutOnStack or
			// stack-bound MoveZone carries an entry marker today, so every
			// unrelated cast folds exactly as before.
			o.ExiledWith = 0
			o.FaceDown = true
			o.Cloaked = moveCounter == CloakEntryCounter
			o.FaceDownSetType, o.FaceDownPower, o.FaceDownToughness, o.FaceDownHasPT = "", 0, 0, false
		} else if e.To == state.ZExile {
			switch moveCounter {
			case "exiled_with_face_down", "exiled_with_face_down_foretold":
				// Hideaway's face-down exile (CR 702.75): the exiling source
				// rides in Amount, and FaceDown is state so a later projection
				// knows not to reveal the card. The foretold variant also
				// records the designation after Move has reset a battlefield
				// object's cast flags.
				o.ExiledWith = state.ObjID(e.Amount)
				o.FaceDown = true
				if moveCounter == "exiled_with_face_down_foretold" {
					o.CastFlags |= state.FlagForetold
				}
			case "exiled_with_face_down_maylook", "exiled_with_face_down_maylook_foretold":
				// A WithMayLook$ True face-down exile (Ixhel, Scion of Atraxa):
				// same state as the Hideaway spell, plus the look permission the
				// exiling effect's controller holds. This marker's own layout
				// puts the LOOKER in Amount and the exiling source in IDs, so
				// both survive replay with no new event kind and no Event field
				// change. MayLookPlayer replaces Object.Controller as the
				// privileged viewer (view/cardViews), so the card's owner cannot
				// read a face the owner never had the right to look at.
				o.FaceDown = true
				o.MayLookPlayer = state.PlayerID(e.Amount)
				o.HasMayLook = true
				if len(e.IDs) > 0 {
					o.ExiledWith = e.IDs[0]
				} else {
					o.ExiledWith = 0
				}
				if moveCounter == "exiled_with_face_down_maylook_foretold" {
					o.CastFlags |= state.FlagForetold
				}
			case "face_down":
				// A bare ChangeZone FaceDown$ True exile (Tezzeret's
				// Reckoning): the card is put into exile face down WITHOUT
				// claiming an ExiledWith association -- the line never named
				// an exiling source, so none is invented. The default branch's
				// own ExiledWith handling below is deliberately bypassed.
				o.FaceDown = true
			case "exiled_with":
				o.ExiledWith = state.ObjID(e.Amount)
				o.FaceDown = false
			default:
				// MoveZone reserves its otherwise-unused IDs payload for the
				// source when an effect exiles a card (moveZoneEvent). This
				// provenance is event-derived, hence survives replay, and
				// clears as soon as the card leaves exile.
				if len(e.IDs) > 0 {
					o.ExiledWith = e.IDs[0]
				} else {
					o.ExiledWith = 0
				}
				o.FaceDown = false
			}
		} else if manifesting {
			// CR 708.5: the manifested or cloaked card stays state-face-down
			// while it is on the battlefield (the view redacts it to everyone
			// but its controller); leaving the battlefield clears it (CR 708.9)
			// through Move's own leave reset and the default branch.
			o.ExiledWith = 0
			o.FaceDown = true
			o.FaceDownSetType = setType
			o.FaceDownPower = fdPower
			o.FaceDownToughness = fdTough
			o.FaceDownHasPT = fdHasPT
			o.Cloaked = e.Counter == CloakEntryCounter
		} else {
			o.ExiledWith = 0
			o.FaceDown = false
		}
		if e.Text == "reversed" && o.HasPreStackEntry {
			o.EnteredThisTurn = o.PreStackEntryThisTurn
			o.EnteredFrom = o.PreStackEntryFrom
			if o.PreStackEnteredLen <= len(g.Entered) {
				g.Entered = g.Entered[:o.PreStackEnteredLen]
			}
			o.PreStackEntryThisTurn = false
			o.PreStackEntryFrom = state.ZLibrary
			o.PreStackEnteredLen = 0
			o.HasPreStackEntry = false
		} else if wasStack && e.To != state.ZStack {
			o.PreStackEntryThisTurn = false
			o.PreStackEntryFrom = state.ZLibrary
			o.PreStackEnteredLen = 0
			o.HasPreStackEntry = false
		}
	}
	// Source-dependent goads end as soon as their source leaves play.
	pruneGoads(g)
	// CR 400.7 / 701.54e: a Ring-bearer designation lives on a permanent
	// and requires the battlefield — the moment the object leaves, every
	// seat's designation naming it is gone (the next battlefield entry is
	// a new object and never inherits one).
	if wasBattlefield && e.To != state.ZBattlefield {
		clearRingBearers(g, e.Obj)
		// CR 702.157b: the suspected designation has the same shape -- it
		// ends the moment the permanent leaves the battlefield; a later
		// battlefield entry never inherits one. The Plotted designation
		// (CR 701.34) has the same end condition on a permanent (the
		// exile case is Move's own leaving-exile clear, the plot ACTION's
		// path).
		if o := g.Obj(e.Obj); o != nil {
			o.Suspected = false
			o.SaddledTurn = 0
			o.Monstrous = false
			o.Solved = false
			o.Renowned = false
			o.PlottedTurn = 0
			// CR 722.3a: the prepared designation lives on a battlefield
			// permanent; the exempted exile copy (CR 722.3c) ceases to be
			// castable because its PreparedSource no longer answers true.
			o.Prepared = false
		}
		// The prepared exile copy's cessation exemption is linked to this
		// battlefield permanent. Once it leaves, retire that provenance in
		// the same replayed zone-change fold so the orphaned copy ceases.
		if g.PreparedSourcesLive() {
			for i := range g.Objs {
				cp := &g.Objs[i]
				if cp.IsCopy && cp.PreparedSource == e.Obj {
					cp.PreparedSource = 0
					g.ClearPreparedSource()
				}
			}
		}
	}
}

// foldLibraryOrder folds Kind LibraryOrder into state.
func foldLibraryOrder(g *state.Game, e *Event) {
	// A library-arranging effect (Ponder, later Scry/Surveil) set a
	// complete new order on a player's library. Mechanically identical to
	// Shuffle's SetZone (see the same defensive copy below -- never alias
	// the event's slice into game state), but a separate Kind on purpose
	// (Ruling J1): Shuffle means "randomised, the old order is gone" to
	// a decoder, while LibraryOrder means "the player chose a new order"
	// -- a scry is not a shuffle, and a log reader must be able to tell
	// them apart. Same totality stance as every case here: an invalid
	// Player is a no-op, never a panic.
	if validPlayer(g, e.Player) {
		g.SetZone(state.ZLibrary, e.Player, append([]state.ObjID(nil), e.IDs...))
	}
}

// foldShuffle folds Kind Shuffle into state.
func foldShuffle(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		g.SetZone(state.ZLibrary, e.Player, append([]state.ObjID(nil), e.IDs...))
	}
}

// foldAttach folds Kind Attach into state.
func foldAttach(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil {
		switch {
		case e.Text == "attach to player" && validPlayer(g, e.Player):
			o.AttachedTo = 0
			o.AttachedPlayer, o.HasAttachedPlayer = e.Player, true
		case len(e.IDs) == 0:
			o.AttachedTo, o.HasAttachedPlayer = 0, false
		case g.Obj(e.IDs[0]) != nil:
			o.AttachedTo, o.HasAttachedPlayer = e.IDs[0], false
			// A re-attach supersedes any earlier bearer: the object is
			// now "attached to" the new one, so a later "was attached
			// to X" read must not still name the old X (state.Object.
			// LastBearer's contract).
			o.LastBearer = 0
		}
	}
}

// foldUnattached folds Kind Unattached into state.
func foldUnattached(g *state.Game, e *Event) {
	// CR 701.3b: Obj became unattached from the bearer on a path where Obj
	// itself stays on the battlefield (the attachmentSBAs detach arms and
	// the bestowed type switch). The fold is the same AttachedTo clear an
	// empty-IDs Attach makes; the Kind is distinct so Mode$ Attached keeps
	// ignoring a detach while Mode$ Unattached fires. IDs[0] is the former
	// bearer, which only the trigger matcher reads -- nothing about the
	// state fold depends on it.
	if o := g.Obj(e.Obj); o != nil {
		o.AttachedTo, o.HasAttachedPlayer = 0, false
		// IDs[0] is the former bearer: the attachmentSBAs detach arms
		// carry it so a later trigger can still resolve "attached to
		// that creature" after the sweep cleared AttachedTo
		// (state.Object.LastBearer's contract). A zero carrier leaves
		// any earlier LastBearer standing -- the object was not
		// attached to a named permanent.
		if len(e.IDs) > 0 && e.IDs[0] != 0 {
			o.LastBearer = e.IDs[0]
		}
	}
}

// foldLandPlayed folds Kind LandPlayed into state.
func foldLandPlayed(g *state.Game, e *Event) {
	if validPlayer(g, e.Player) {
		g.Players[e.Player].LandsPlayed++
	}
}

// zone's order. That is deterministic and matches every other move.
func Move(g *state.Game, id state.ObjID, from, to state.Zone) {
	move(g, id, from, to, false)
}

// MoveCountersRemain folds a move whose departing permanent has the
// CountersRemain static. The marker is carried by the logged MoveZone event.
func MoveCountersRemain(g *state.Game, id state.ObjID, from, to state.Zone) {
	move(g, id, from, to, true)
}

func move(g *state.Game, id state.ObjID, from, to state.Zone, countersRemain bool) {
	o := g.Obj(id)
	if o == nil || !o.Zone.Valid() || !to.Valid() {
		return
	}
	// The object's real (pre-move) zone, captured before o.Zone is
	// overwritten below -- Task 4's X/CastFlags/Chosen* reset needs to know
	// whether this object is actually LEAVING the battlefield, not merely
	// where the caller-supplied from claims it came from (the same
	// real-zone-over-claimed-zone rule this function already applies to the
	// removal itself, a few lines below).
	enteredFrom := o.Zone
	wasBattlefield := enteredFrom == state.ZBattlefield
	wasStack := enteredFrom == state.ZStack
	// Forge's drawnThisTurn survives only the move onto the stack (a cast of
	// the drawn card); any other move makes the card a new object that was
	// not drawn. The Draw fold re-stamps it after this Move.
	if to != state.ZStack {
		o.DrawnTurn = 0
		// The as-cast battlefield snapshot belongs to the spell on the stack:
		// any move off it (resolution, counter, fizzle, an aborted cast's
		// reversal) ends it, so a stable ObjID never reuses a stale freeze.
		o.CastBattlefield = nil
	}
	if enteredFrom == state.ZExile && to != state.ZExile {
		// Forge's exiledCards association is a zone relationship, not an
		// imprint. Once this object leaves exile it is a new object for that
		// association, even if a later effect exiles the same engine ObjID.
		// Do this inside Apply's Move fold so live play and log replay prune
		// every source's list identically. The ExileReturn list (ChangeZone's
		// Duration$ UntilHostLeavesPlay) prunes identically: a card that left
		// exile by any other path is no longer its exiler's business to return.
		for i := range g.Objs {
			g.Objs[i].ExiledCards = withoutObjID(g.Objs[i].ExiledCards, id)
			g.Objs[i].ExileReturn = withoutExileReturnObj(g.Objs[i].ExileReturn, id)
			// Cipher (CR 702.99): the encoded link is a zone relationship too.
			// Once the encoded card leaves exile -- cast, blinked, moved by any
			// effect -- it is no longer that creature's encoded card, so prune
			// it exactly as ExiledCards prunes, inside the event fold so live
			// play and log replay agree.
			g.Objs[i].EncodedCards = withoutObjID(g.Objs[i].EncodedCards, id)
		}
		// CR 701.34c: a plotted card is no longer plotted once it leaves
		// exile (cast from exile to the stack, or moved on by any effect), so
		// the free-cast permission cannot revive on a later return to exile.
		// The exile-departure clear is the one home for this: every leaving
		// path (MoveZone, Draw and PutOnStack all call Move) runs it.
		if o := g.Obj(id); o != nil {
			o.PlottedTurn = 0
			// A granted suspend keyword is scoped to the exiled object; once it
			// leaves exile it is a new object for the grant's purposes.
			o.SuspendGranted = false
		}
	}
	if wasBattlefield && to != state.ZBattlefield && (g.BlockersLive() || ArenaSkipVerify) {
		// Leaving combat removes this permanent as a blocker, but does not
		// make creatures it blocked unblocked (CR 506.4, 509.1h). Preserve
		// each attacker's blocker-list length with the same zero tombstone
		// EndCombatReset uses for regeneration. Dense arena order keeps this
		// deterministic, and the departing object's own state is cleared by
		// the zone reset below. With no blocker recorded since the last
		// whole-combat reset (state.Game.BlockersLive) every list is empty
		// and the arena walk is skipped: without it a mass departure of N
		// permanents walked the arena N times.
		live := g.BlockersLive()
		for i := range g.Objs {
			other := &g.Objs[i]
			// The empty-list test first: it is the common case, and it reads
			// only the BlockedBy header instead of also touching the ID at
			// the other end of the ~800-byte object on every arena entry.
			if len(other.BlockedBy) == 0 || other.ID == id {
				continue
			}
			if !live {
				panic(fmt.Sprintf("events: obj %d has BlockedBy %v while no blocker is live (a BlockedBy write skipped state.Game.NoteBlockers)", other.ID, other.BlockedBy))
			}
			for j, blocker := range other.BlockedBy {
				if blocker == id {
					other.BlockedBy[j] = 0
				}
			}
		}
	}
	remove(g, id, o.Zone, zoneOwner(o, o.Zone))
	// CR 702.140e: when a mutated permanent leaves the battlefield, each card
	// merged beneath its top card moves to the same zone -- the pile is one
	// permanent, not a top plus orphans. The under-cards are parked in ZCeased
	// (no membership list) each with its own object, and this is the one site
	// that relocates them; a log-only replay runs the same Move. The pile
	// marker clears with the departure so a permanent that returns later
	// (CR 400.7) is a fresh, unmutated object. Recurse only into this
	// battlefield-boundary arm: a parked object's own Move has
	// wasBattlefield == false, so the walk cannot nest.
	if wasBattlefield && to != state.ZBattlefield && len(o.MergedCards) > 0 {
		merged := o.MergedCards
		o.MergedCards = nil
		o.TimesMutated = 0
		for _, mc := range merged {
			if mc.Obj != 0 {
				Move(g, mc.Obj, state.ZCeased, to)
			}
		}
	}
	// CR 712.4: a melded permanent that goes anywhere but the battlefield
	// splits back into its two cards (events/apply_meld.go).
	if to != state.ZBattlefield && o.MeldedWith != 0 {
		unmeld(g, o, enteredFrom, to)
	}
	// CR 400.7: leaving the battlefield makes the object a new object in
	// its next zone, so control-changing effects do not follow it. Reset
	// before choosing the destination's zone owner: a later graveyard/hand
	// re-entry must be placed under its owner, not its former controller.
	// CR 702.25e: a phased-out permanent that leaves the battlefield phases
	// in as it does so -- the phased-out status is a property of that
	// battlefield object, and a later entry is a fresh, phased-in permanent.
	if wasBattlefield && to != state.ZBattlefield {
		o.Controller = o.Owner
		o.PhasedOut = false
	}
	if wasBattlefield && to != state.ZBattlefield {
		// CR 702.99: a creature's encoded cards are battlefield-stint state.
		// A creature that leaves the battlefield is a new object (CR 400.7),
		// and its encoded cards stay in exile un-encoded; the association is
		// dropped here so no later damage trigger reads a stale link.
		o.EncodedCards = nil
	}
	// CR 113.7a: an ability on the stack is not a card, and once it leaves
	// the stack it ceases to exist. The resolved/countered ability's move is
	// logged as stack->exile and o.Zone says exile (a historical shape every
	// golden replay and many pins carry: "the CR 608.2m exile parking"), but
	// the Face-less object never joins a zone's MEMBERSHIP list: an exile
	// walk (Oracle of Dust's "put a card an opponent owns from exile into
	// that player's graveyard" cost) moved such an object into a graveyard,
	// where Delve and an ExileFromGrave cost offered it as a card and
	// dereferenced its nil Face.
	ceasedAbility := to != state.ZStack && o.Card == nil && o.Ability != nil
	if to != state.ZCeased && !ceasedAbility {
		dst := zoneOwner(o, to)
		g.SetZone(to, dst, append(g.Zone(to, dst), id))
	}

	o.Zone = to
	// CR 709.4: a non-Room split card has no persistent "current half" once it
	// leaves the stack -- both halves are printed on the same physical card and
	// either is castable again from whatever zone it lands in. A split_alt (or
	// aftermath) cast flips the object to face 1 for the cast transaction; this
	// normalization resets it, or a card returned to hand would offer only the
	// half it was last cast as. Rooms are excluded (their face IS persistent
	// battlefield state) and so is a battlefield destination. Done inside
	// Apply's Move fold so live play and log replay normalize identically.
	if wasStack && to != state.ZBattlefield && o.Card != nil &&
		o.Card.AlternateMode == "Split" && len(o.Card.Faces) == 2 && int(o.FaceIdx) != 0 {
		room := false
		for _, f := range o.Card.Faces {
			if f != nil && f.IsRoom() {
				room = true
				break
			}
		}
		if !room {
			o.SetFaceIdx(0)
		}
	}
	// CR 715.4: off the stack an adventurer card has only its normal (main
	// face) characteristics, so an Adventure spell leaving the stack --
	// resolved into the adventure zone, countered, or reversed to hand -- is
	// its main face again. The adventure-zone recast reads the log, not the face.
	if wasStack && to != state.ZBattlefield && o.Card != nil &&
		o.Card.AlternateMode == "Adventure" && o.FaceIdx != 0 {
		o.SetFaceIdx(0)
	}
	// CR 712.4d: a Modal DFC is front-face up in every non-battlefield
	// zone. Its back face remains active while it is a permanent, but leaving
	// the battlefield creates a new object whose characteristics are the
	// front face. Keep this in the event fold so replay and live play agree.
	if wasBattlefield && to != state.ZBattlefield && o.Card != nil &&
		o.Card.AlternateMode == "Modal" && len(o.Card.Faces) == 2 &&
		o.Card.Faces[0] != nil && o.Card.Faces[1] != nil {
		o.SetFaceIdx(0)
	}
	// The incarnation stamp is used by promises tied to a particular
	// permanent (evoke/dash/warp), so only crossing the battlefield
	// boundary advances it. A provisional hand->stack->hand CR 733 reversal
	// must restore byte-identical state and is not a permanent incarnation.
	if enteredFrom != to && (enteredFrom == state.ZBattlefield || to == state.ZBattlefield) {
		o.Incarnation++
	}
	// A new object in a new zone has its owner's default control. The old
	// controller is needed above to remove it from the battlefield/stack, so
	// reset only after removal and placement have used that zone ownership.
	if to != state.ZBattlefield && to != state.ZStack {
		o.Controller = o.Owner
	}
	// The stamped stack kind (state.StackKindKnown) is a property of STACK
	// MEMBERSHIP, not of the card: every mint stamps it when its event mints
	// the stack object (MoveZone's Spell entry, TriggerPush/AbilityPush and
	// their siblings), so an ability object keeps its kind after its source
	// has left (CR 113.7a) -- and leaving the stack (a CR 733.1 reversal's
	// MoveZone, a resolution, a counter) un-stamps it, so the restored object
	// is byte-identical with its pre-push state and the legacy re-derivation
	// in state.StackKindOf applies again. Inside the fold for the same reason
	// the Controller reset above is: no rules/ or effects/ caller can mint a
	// leaving-the-stack move that skips it.
	if wasStack && to != state.ZStack {
		o.StackKind, o.StackKindKnown = state.StackKindSpell, false
	}
	// A zone change is the single source of zone-entry provenance. Capture
	// the actual old zone (not Event.From, which Move deliberately treats as
	// advisory) so replay and a live game derive identical ThisTurnEntered*
	// state even from a malformed caller-supplied From.
	o.EnteredThisTurn = true
	o.EnteredFrom = enteredFrom
	if to == state.ZBattlefield && enteredFrom != state.ZBattlefield && o.Card != nil && len(o.Card.Faces) > 0 && o.Card.Faces[0].IsRoom() {
		// A stack copy is put onto the stack, not cast (CR 707.10). It
		// resolves from ZStack too, but does not designate a cast door.
		o.CastDoor = enteredFrom == state.ZStack && !o.IsCopy
	}
	// Record the per-add entry the Count$ThisTurnEntered_* heads and the
	// ThisTurnEntered* filter predicates read (Forge's per-zone
	// getCardsAddedThisTurn lists, one append per add). Every Move routes
	// through events.Apply, so this stays inside the Apply-only mutation
	// discipline; the TurnChange case clears the list with the rest of the
	// per-turn state. A reversed CR 733.1 cast proposal keeps its entries:
	// the proposal's PutOnStack entry and the reverse move's entry both
	// land, and no corpus head reads the zones that pair touches
	// (Hand_from_Stack does, and the double entry it sees is the honest
	// record of the two moves).
	permanentCard := !o.IsToken && !o.IsCopy && o.Card != nil && o.Face() != nil && o.Face().IsPermanent()
	if !ceasedAbility {
		// A ceased ability is no zone entry: a Count$ThisTurnEntered_Exile
		// head counts cards put into exile, never retired abilities.
		g.Entered = append(g.Entered, state.ZoneEntry{Obj: id, To: to, From: enteredFrom,
			Owner: o.Owner, PermanentCard: permanentCard})
	}
	switch to {
	case state.ZBattlefield:
		o.SummonSick = true
		o.Damage = 0
		g.Clock++
		o.Timestamp = g.Clock
		// CR 707.10g: a copy of a permanent spell becomes a token. The pair
		// enteredFrom == ZStack + o.IsCopy uniquely identifies a StackCopy
		// mint resolving onto the battlefield (every other token mint routes
		// through a token script from a non-stack zone), and folding the flag
		// here inside Apply keeps a log-only replay byte-identical with no
		// new event. IsCopy is cleared at the same point: a resolved
		// permanent copy is a token, not a CR 707.10h "copy that left the
		// stack" (the clear also keeps the cast-provenance readers, which
		// gate on !IsCopy, reading a battlefield copy as never-cast). Note
		// this clear is NOT what makes the resolved copy visible any more:
		// state.Object.Ephemeral's IsCopy half is zone-aware and would show
		// a battlefield copy regardless, and effects/filter.go's zone-aware
		// CR 707.10h guard already matched it as a real permanent. A copy of
		// an instant/sorcery never enters the battlefield, so its IsCopy and
		// its exile rest zone are untouched.
		if enteredFrom == state.ZStack && o.IsCopy {
			o.IsToken = true
			o.IsCopy = false
		}

		// CR 400.7: a battlefield entry from another zone is a new object and
		// a new control acquisition — kw:Echo's gate stamp (the entry already
		// carries the entering controller). A battlefield→battlefield stay is
		// not a new acquisition and must not re-stamp.
		//
		// The entry-characteristic COUNTERS (CR 306.5b starting loyalty, Riot's
		// and Unleash's +1/+1 election, a Saga's lore counter, a Battle's
		// defense counters) are deliberately NOT folded here. events.Move is a
		// pure state fold with no way to emit, so a counter folded here is
		// invisible to the CR 614 replacement pipeline and to the CantPutCounter
		// prohibition. rules snapshots events.EntryCounterGrants just before
		// this move folds and places each grant through a real CounterChange
		// event, so replacements and prohibitions see an entry counter exactly
		// like any other placement and a log-only replay re-derives it from
		// those logged events (task addcounter1/2). What stays here is only the
		// Riot "haste" election (a keyword grant, not a counter) and the
		// one-shot consumption of both elections.
		if !wasBattlefield {
			o.AcqTurn = g.Turn
			o.AcqStep = g.Step
			// Riot's choice is made before this entry. The "haste" half grants
			// a keyword (the "counter" half is placed by the engine's
			// EntryCounterGrants path); consuming the election here makes every
			// entry path obey the same logged choice.
			switch o.RiotChoice {
			case "haste":
				o.IntrinsicKeywords = append(o.IntrinsicKeywords, "Haste")
			}
			o.RiotChoice = ""
			// kw:Unleash's choice rides the same logged-then-consumed shape;
			// the "counter" half is placed through the engine's CounterChange
			// path, so only the consumption is left here.
			o.UnleashChoice = ""
		}
		// A face-down entry (a manifest) is a 2/2 creature with no abilities
		// (CR 708.5), so it grants none of the entry counters above; the
		// engine's EntryCounterGrants gate reads the same MoveZone face-down
		// marker this fold applies.
	default:
		// CR 702.103: a Soulbond pair ends when either member leaves the
		// battlefield. Move itself is the complete logged state transition, so
		// clear the remaining member here too; replay derives the same break
		// without a second event.
		if wasBattlefield && o.Paired != 0 {
			if partner := g.Obj(o.Paired); partner != nil && partner.Paired == o.ID {
				partner.Paired = 0
			}
		}
		// CR 400.7 / CR 702.122: the crew pairing the Creature.CrewedThisTurn
		// filter reads is battlefield-stint state on BOTH ends, and the crewing
		// creature's list is keyed by the Vehicle's stable ObjID. A Vehicle that
		// leaves the battlefield and returns in the same turn (blink, bounce) is
		// a NEW object (CR 400.7), but the old pairing would still match its id:
		// a trigger would target or affect a creature that never crewed THIS
		// object. Clear the departing permanent's id from every battlefield
		// object's CrewedVehicles -- the same derive-without-a-second-event sweep
		// the Soulbond break above does for its pairing. (The departing object's
		// OWN list is cleared below with the rest of its leaving-the-battlefield
		// state.) Totality: the id can appear at most once (the Crew case folds a
		// set), so the first hit is removed and the loop stops.
		if wasBattlefield && g.CrewedObjectsLive() {
			for i := range g.Objs {
				cr := &g.Objs[i] // a read: never copy the ~1 KB Object per arena slot
				if cr.ID == id || cr.Zone != state.ZBattlefield || len(cr.CrewedVehicles) == 0 {
					continue
				}
				for j, v := range cr.CrewedVehicles {
					if v == id {
						cr.CrewedVehicles = append(cr.CrewedVehicles[:j], cr.CrewedVehicles[j+1:]...)
						if len(cr.CrewedVehicles) == 0 {
							g.ClearCrewedObject()
						}
						break
					}
				}
			}
		}
		// Leaving the battlefield or the stack resets everything that only
		// exists while a permanent or spell is in play.
		o.Tapped = false
		o.Damage = 0
		o.ClassLevelValue = 0
		o.IsAttacking = false
		o.AttackingBattle = 0
		o.BlockedBy = nil
		if !countersRemain || to == state.ZHand || to == state.ZLibrary {
			o.Counters = nil
		}
		o.IntrinsicKeywords = nil
		o.ExiledWith = 0
		o.FaceDown = false
		o.FaceDownSetType = ""
		o.FaceDownPower = 0
		o.FaceDownToughness = 0
		o.FaceDownHasPT = false
		o.Cloaked = false
		o.RiotChoice = ""
		o.UntapChoice = ""
		o.UnleashChoice = ""
		o.IsMyriad = false
		// CR 400.7: leaving the battlefield makes the object a new object, so
		// a layer-1 copy effect does not follow it. The ClonePermanent basis
		// is battlefield-only state and is cleared here (its continuous-effect
		// bookkeeping is dropped by active()/cleanup, since the effect's
		// source -- this same object -- is no longer on the battlefield).
		o.SetCopyFace(nil)
		o.CopyGainThisAbility = false
		o.Paired = 0
		o.Targets = nil
		o.SubTargets = nil
		// o.Remembered is deliberately NOT reset here: a card's remembered
		// list is CARD memory, not permanent state -- Forge preserves it
		// across zone changes, which is the whole O-Ring premise (the return
		// trigger on a card in the graveyard reads the exile its
		// battlefield-stint remembered) and the reason its ForgetOtherTargets$
		// exists at all (the deliberate clear on a re-exile). The list stays
		// event-backed (Choose "remembered"/"clear-remembered"), so live play
		// and replay derive it identically either way.
		if wasBattlefield {
			// Room door designations belong to this battlefield incarnation.
			o.Unlocked = false
			o.CastDoor = false
			o.LockedDoors = 0
			o.Imprinted = nil
			o.ImprintTokens = nil
			o.SeekFound = nil
		}
		// X/CastFlags/Chosen* carry cast-time and choose-time information
		// forward from the stack onto the permanent it resolves into (an
		// ETB "if it was kicked" trigger needs to read X/CastFlags off the
		// permanent, not just the spell) -- so hand/stack -> battlefield
		// must NOT reset them, and they only reset once the permanent
		// genuinely leaves the battlefield again. AttachedTo has no legal
		// life off the battlefield at all (an Aura/Equipment that isn't a
		// permanent cannot be "attached"), so it always resets here
		// regardless of where the object came from.
		if wasBattlefield {
			o.X, o.CastFlags = 0, 0
			o.ReplicateTimes = 0
			o.SquadPaid = 0
			o.OffspringPaid = false
			o.OptionalCostPaid = false
			o.ConvergeColours = 0
			o.TimesKicked = 0
			o.Conspired = false
			o.TeamworkPaid = false
			o.Convoked = nil
			o.ManaAddsCounterGrants = nil
			o.ManaSpent = 0
			o.ManaSnowSpent = 0
			o.ManaTreasureSpent = 0
			o.ManaCaveSpent = 0
			o.ManaDesertSpent = 0
			o.ManaArtifactSpent = 0
			o.ManaColorSpent = state.Mana{}
			o.CompleatedLifePaid = 0
			o.NotedNumber = 0
			// CR 400.7: the runtime SVar store is the old permanent's, not the
			// new object's -- a blunk/reanimated StoreSVar carrier starts with
			// no stored value (the printed default stands).
			o.RuntimeSVars = nil
			// CR 702.168: the gift promise's receiver is cast-time
			// provenance, not a battlefield characteristic -- a re-entering
			// permanent carries no promise from its old cast. The
			// FlagPromisedGift bit is cleared with CastFlags just above.
			o.GiftPromisedTo = 0
			o.ChosenName, o.ChosenType, o.ChosenNumber, o.ChosenColor = "", "", 0, ""
			o.ETBCloneChoice, o.ETBCloneChoiceValid = 0, false
			o.Protector, o.ProtectorValid = 0, false
			o.LastNotedMana = ""
			o.Chosen = nil
			// CR 400.7: leaving the battlefield makes the object a new object,
			// so a Charm's ChoiceRestriction$ ThisTurn picks -- battlefield-
			// stint state the source's own Charm reads -- do not follow it. A
			// permanent that leaves and returns (blink, reanimation) starts
			// with an empty log, even in the same turn.
			o.ModeChoices = nil
			o.CurCombatTurn, o.CurCombatCombat = 0, 0
			// Exert state is the old permanent's, not the new object's
			// (CR 400.7): a re-entering Combat Celebrant may exert again
			// this turn and carries no untap-skip window. The CR 611.2b
			// next-untap-step restriction (Frost Lynx's runtime keyword
			// grant) is the same kind of battlefield-stint state and is
			// cleared with it.
			o.ExertedThisTurn, o.ExertSkipUntap = false, false
			o.CantUntapNextStep = false
			// CR 702.160: enlist is the old permanent's fact, not the new
			// object's -- a re-entering creature carries no enlist stamp.
			o.EnlistedTurn, o.EnlistedCombat = 0, 0
			// CR 400.7 / 702.122: crew status is the old permanent's, not the
			// new object's -- a re-entering creature carries no crew stamp.
			if len(o.CrewedVehicles) != 0 {
				g.ClearCrewedObject()
			}
			o.CrewedVehicles, o.CrewedTurn = nil, 0
		}
		// CR 107.3m: the paid X belongs to the spell on the stack and to the
		// permanent the spell becomes, and to nothing else. An object leaving
		// the stack for a zone OTHER than the battlefield -- a countered or
		// fizzled spell into the graveyard, a resolving instant/sorcery -- is
		// a card in a non-battlefield zone, where X in its text is 0. Without
		// this the stale paid X rides along: a countered Genesis Hydra
		// reanimated later would resolve its ETB trigger with the dead cast's
		// X instead of 0. (A stack->battlefield move keeps X/CastFlags -- the
		// battlefield case above deliberately does not reset them, which is
		// what lets an ETB trigger read them off the permanent.)
		if wasStack {
			o.X, o.CastFlags = 0, 0
			o.ReplicateTimes = 0
			o.SquadPaid = 0
			o.OffspringPaid = false
			o.OptionalCostPaid = false
			o.ConvergeColours = 0
			o.TimesKicked = 0
			o.Conspired = false
			o.TeamworkPaid = false
			o.Convoked = nil
			o.ManaAddsCounterGrants = nil
			o.ManaSpent = 0
			o.ManaSnowSpent = 0
			o.ManaTreasureSpent = 0
			o.ManaCaveSpent = 0
			o.ManaDesertSpent = 0
			o.ManaArtifactSpent = 0
			o.ManaColorSpent = state.Mana{}
			o.CompleatedLifePaid = 0
			o.NotedNumber = 0
			// CR 702.168: a spell leaving the stack for a non-battlefield zone
			// (a resolving instant/sorcery, a countered spell) names no gift
			// receiver further. The FlagPromisedGift bit is cleared with
			// CastFlags just above.
			o.GiftPromisedTo = 0
		}
		// ChosenModes is needed while a modal spell/ability resolves or for
		// the lifetime of a mode-chosen permanent (SetChosenMode$). A fresh
		// battlefield stint must choose again. An aborted cast restores its
		// captured prior value after the reverse stack move.
		if wasStack || wasBattlefield {
			o.ChosenModes = nil
		}
		// AttachedTo has no legal life off the battlefield at all (an Aura/
		// Equipment that isn't a permanent cannot be "attached"), so it always
		// resets here. The pre-clear bearer is preserved as LastBearer so a
		// trigger that resolves after the sweep can still resolve "objects
		// that were attached to it" (state.Object.LastBearer's contract).
		// When AttachedTo is already 0 (an earlier Unattached set it) the
		// existing LastBearer stands.
		if o.AttachedTo != 0 {
			o.LastBearer = o.AttachedTo
		}
		o.AttachedTo, o.HasAttachedPlayer = 0, false
	}
}

// changeControl gives o to controller p. The battlefield is keyed by
// controller (zoneOwner), so a permanent moves from its old controller's list
// to the new one's, the way Forge's controllerChangeZoneCorrection does;
// every per-controller reader (untap step, attackers, blockers, mana and
// activation offers, statics, projections) then sees it under its controller.
// The stack is one shared list, so a spell only changes its Controller.
//
// On the battlefield a control change also:
//   - removes the permanent from combat (CR 506.4): it stops attacking, loses
//     its blockers, and attackers it blocked keep a zero tombstone so they stay
//     blocked (CR 509.1h), exactly as a departing blocker does in Move;
//   - makes it summoning sick (CR 302.6): its new controller has not controlled
//     it continuously since their most recent turn began. TurnChange clears it
//     from the active player's list, i.e. at its new controller's next turn.
//
// planarWalkToOrder is the Defined$ planeswalk fold's destination move: each
// named destination plane is moved to the front of ids in the order named,
// and every plane not named keeps its relative order after them. A
// destination not present in the zone is skipped (a stale remembered card),
// and duplicates are placed once. The result is a permutation of the input,
// so the fold can never lose or duplicate a plane.
func planarWalkToOrder(ids, dests []state.ObjID) []state.ObjID {
	inZone := make(map[state.ObjID]bool, len(ids))
	for _, id := range ids {
		inZone[id] = true
	}
	out := make([]state.ObjID, 0, len(ids))
	placed := make(map[state.ObjID]bool, len(dests))
	for _, d := range dests {
		if !inZone[d] || placed[d] {
			continue
		}
		placed[d] = true
		out = append(out, d)
	}
	for _, id := range ids {
		if !placed[id] {
			out = append(out, id)
		}
	}
	return out
}

// withoutObjID returns ids without id, retaining its order and avoiding an
// allocation when no entry matches. ExiledCards is a short insertion-ordered
// relation, so an ordered slice preserves deterministic selector results.
func withoutObjID(ids []state.ObjID, id state.ObjID) []state.ObjID {
	for i, got := range ids {
		if got != id {
			continue
		}
		out := append([]state.ObjID(nil), ids[:i]...)
		for _, got := range ids[i:] {
			if got != id {
				out = append(out, got)
			}
		}
		return out
	}
	return ids
}

// withoutExileReturnObj drops every ExileReturn entry naming id, preserving
// the order of the survivors (the same withoutObjID contract for the
// entry-valued list).
func withoutExileReturnObj(entries []state.ExileReturnEntry, id state.ObjID) []state.ExileReturnEntry {
	for i, got := range entries {
		if got.Obj != id {
			continue
		}
		out := append([]state.ExileReturnEntry(nil), entries[:i]...)
		for _, got := range entries[i:] {
			if got.Obj != id {
				out = append(out, got)
			}
		}
		return out
	}
	return entries
}
