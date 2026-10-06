// Event folds for combat: declarations, damage, myriads, enlist, crew, goads and phase-out.
//
// Split out of events/apply.go: code moved verbatim, no behaviour
// change. Each fold function is the body of the matching case in
// Apply (g, e) switch; see apply.go for the dispatch.
package events

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// foldDeclareAttackers folds Kind DeclareAttackers into state.
func foldDeclareAttackers(g *state.Game, e *Event) {
	// e.Player names the attacking player for every ID in this event, so
	// it is validated once, like TurnChange/Priority above, rather than
	// per object. Nothing reads Object.Attacking yet, so an unvalidated
	// value cannot panic today -- but it is the same untrusted-seat-id
	// pattern as Ruling T20-e, found in the same audit, and closing it
	// now costs nothing for a well-formed event (Player is always valid
	// there).
	if !validPlayer(g, e.Player) {
		return
	}
	for _, id := range e.IDs {
		if o := g.Obj(id); o != nil {
			o.IsAttacking = true
			o.Attacking = e.Player
			// CR 310.7/CR 508.1: a battle or planeswalker attack carries that
			// permanent in Obj, so the attacker records which permanent it is
			// attacking. A player attack leaves Obj zero and the field stays
			// zero -- the same discriminator a Numeric TargetChosen pair uses.
			o.AttackingBattle = e.Obj
			o.AttacksThisTurn++
		}
	}
}

// foldDeclareBlockers folds Kind DeclareBlockers into state.
func foldDeclareBlockers(g *state.Game, e *Event) {
	for _, pr := range e.Pairs {
		a := g.Obj(pr[0])
		if a == nil || g.Obj(pr[1]) == nil {
			continue
		}
		a.BlockedBy = append(a.BlockedBy, pr[1])
		g.NoteBlockers()
	}
}

// foldCombatRetarget folds Kind CombatRetarget into state.
func foldCombatRetarget(g *state.Game, e *Event) {
	// api:ChangeCombatants's reselect (Misleading Signpost, Portal Mage,
	// Windshaper Planetar): the attack moves, nothing else. Obj is the
	// attacker, Player the NEW defender. Deliberately narrower than
	// DeclareAttackers (which must not be reused here -- its Apply case
	// increments AttacksThisTurn and would refire every Attacks trigger on
	// a reselect, and TokenAttacks taps too): IsAttacking is already true
	// and stays true, only the defender and the block list move. The same
	// defensive shape as DeclareAttackers' own case: the defender must be
	// a valid seat, still in the game, and the attacker still on the
	// battlefield and actually attacking -- anything else (a gone
	// attacker, a fuzz event) is a no-op.
	if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZBattlefield && o.IsAttacking &&
		validPlayer(g, e.Player) && !g.Players[e.Player].Lost {
		o.Attacking = e.Player
		o.BlockedBy = nil
	}
}

// foldDamage folds Kind Damage into state.
func foldDamage(g *state.Game, e *Event) {
	// CR 702.90b: damage a source with INFECT dealt is dealt in a
	// different FORM, decided by the recipient -- to a creature, as that
	// many -1/-1 counters, not marked damage; to a player, as that many
	// poison counters, not life loss; every other object (an artifact, a
	// Battle, a printed planeswalker) takes it as ordinary damage. The
	// Damage event itself still travels the whole replacement and trigger
	// pipeline (protection, prevention, DamageDone triggers, lifelink,
	// the combat-damage ledger), exactly as the planeswalker loyalty
	// exchange below already converts the same event; the infect marker
	// rides Counter, Damage's existing characteristic carrier.
	//
	// The counters/poison themselves are NOT written here: they are
	// placed by rules' conversion (Engine.convertInfectDamage), which
	// emits a REAL CounterChange/PlayerCounterChange right after this
	// event folds, so the repl:AddCounter class (a Winding Constrictor
	// doubler, a CantPutCounter lock) and trig:CounterAdded see the
	// placement exactly like any other. This fold only withholds the
	// form the counters replace.
	infect := e.Counter == "infect"
	wither := e.Counter == "wither+creature"
	if o := g.Obj(e.Obj); o != nil {
		// CR 306.8 / 120.3c: damage dealt to a planeswalker permanent
		// removes that many loyalty counters instead of being marked as
		// damage. The conversion lives here, on the one fold every
		// Damage event goes through, so spell/ability damage (effects/
		// damage.go), future combat damage and any emitter this build
		// gains later all convert the same way and a replay derives the
		// same loyalty from the same log. A planeswalker that is ALSO a
		// creature still takes marked damage (CR 120.3e -- the exchange
		// is not exclusive), and either way a positive amount records
		// that the object was dealt damage this turn. Prevention (CR
		// 702.16d) and protection replace or note the Damage event
		// before it reaches this fold, so a prevented hit converts
		// nothing -- which is why the walker exchange no longer needs
		// the bypass it used to travel by.
		walker := false
		battle := false
		if f := o.Face(); f != nil && f.IsBattle() {
			// CR 310.8a: damage dealt to a battle removes that many defense
			// counters instead of being marked as damage. The conversion lives
			// on this one fold, exactly like the planeswalker exchange above,
			// so combat damage (rules/combat.go) and spell/ability damage
			// (effects/damage.go) all convert the same way and a log-only
			// replay re-derives the same defense counters from the same
			// events. A face-down permanent is a 2/2 creature, not a Battle
			// (CR 708.5), so it marks damage normally.
			battle = true
			if e.Amount > 0 {
				o.AddCounter("DEFENSE", -e.Amount)
			}
		}
		if f := o.Face(); f != nil && f.IsPlaneswalker() {
			walker = true
			// Cleanup represents removal of marked damage with a negative
			// Damage event. It must never restore loyalty; only positive
			// damage has the CR 120.3c loyalty conversion.
			if e.Amount > 0 {
				o.AddCounter("LOYALTY", -e.Amount)
			}
		}
		// Counter is Damage's existing, encoded characteristic carrier:
		// effects/rules set it to creature from the current layer result.
		// The printed-face fallback retains direct-event callers and normal
		// printed creature behavior.
		// The infect marker on an OBJECT event is the compound
		// "infect+creature": the emitter (rules/effects, which can read the
		// layer state this fold cannot) tags exactly the CREATURE
		// recipients, printed or layer-animated (CR 120.3e -- such a
		// planeswalker takes the loyalty exchange above AND its damage in
		// counter form). A bare "infect" object event is never emitted by
		// the engine's own emitters -- a non-creature recipient goes
		// untagged and marks normally -- so treating a bare marker as
		// ordinary damage is the safe reading for anything a future
		// emitter (or a redirect's fresh event) hands here.
		creature := e.Counter == "creature" || e.Counter == "infect+creature" ||
			(o.Face() != nil && o.Face().IsCreature())
		if e.Counter == "infect+creature" || wither {
			// Infect and Wither replace marked creature damage with a
			// separate counter placement emitted by rules after this fold.
			// damage. They arrive as the separate CounterChange event rules
			// emitted right after this one. The branch also covers a
			// rewritten negative amount (cleanup's marked-damage clearing),
			// which an infect recipient never owes -- it has no marked
			// damage to clear; its counters survive cleanup (they are not
			// marked damage).
		} else if (!walker && !battle) || creature {
			o.Damage += e.Amount
			if o.Damage < 0 {
				o.Damage = 0
			}
		}
		// A positive Damage event records that the object was dealt damage
		// this turn even if a later prevention/healing event clears its
		// marked damage. The history clears at the same TurnChange boundary
		// as the engine's other per-turn state below.
		if e.Amount > 0 {
			o.WasDealtDamageThisTurn = true
			o.DamageReceivedThisTurn += e.Amount
		}
	} else if validPlayer(g, e.Player) {
		if infect && e.Amount > 0 {
			// CR 702.90b: that many poison counters instead of life loss.
			// The placement is rules' job (Engine.convertInfectDamage emits
			// a real PlayerCounterChange right after this event, so the
			// repl:AddCounter class and trig:CounterAdded see it and
			// rules/sba.go's CR 704.5b ten-poison loss reads it exactly as
			// it reads a Ward-poison counter); this fold only withholds the
			// life loss the poison replaces.
		} else {
			g.Players[e.Player].Life -= e.Amount
		}
	}
}

// foldEndCombatReset folds Kind EndCombatReset into state.
func foldEndCombatReset(g *state.Game, e *Event) {
	// Obj zero retains the original whole-combat reset. A nonzero Obj
	// removes only that permanent (regeneration).
	resetCombat(g, e.Obj)
}

// foldCmdDamage folds Kind CmdDamage into state.
func foldCmdDamage(g *state.Game, e *Event) {
	// Commander combat damage to a player (CR 903.10, Task m33): fold
	// Amount into Player's cumulative tally at the source commander's
	// match-wide dense index, exactly the slot m30's genesis sizes and
	// New's Commanders bookkeeping names, so commander B keeps one slot
	// - and one cumulative total - for the whole match, across zone
	// changes and recasts (state.ObjID is stable across moves). A
	// log-only reconstruction reproduces the tally because the event
	// itself carries it; deriving it from the existing Damage events is
	// impossible because those carry no source. Guarded to totality
	// like every case here: an out-of-range Player, a nonexistent
	// source, or a game whose CmdDamage was never sized (a non-
	// Commander game, or a hand-built state) is a no-op, never a panic.
	if validPlayer(g, e.Player) && e.Obj != 0 {
		if idx, ok := commanderDenseIndex(g, e.Obj); ok {
			if idx >= 0 && idx < len(g.Players[e.Player].CmdDamage) {
				g.Players[e.Player].CmdDamage[idx] += e.Amount
			}
		}
	}
}

// foldDamageProvenance folds Kind DamageProvenance into state.
func foldDamageProvenance(g *state.Game, e *Event) {
	// Game-long damage-by-source provenance (the_fallen, diseased_vermin):
	// append the SOURCE to the recipient's record so the
	// wasDealtDamageThisGameBy / wasDealtDamageByThisGame filters can ask
	// "has this source dealt me damage this game". Obj is the source,
	// IDs[0] is the recipient (PlayerRef-encoded for a seat, a plain
	// ObjID for an object) -- the TriggerPush encoding, decoded with the
	// shared ObjID.PlayerRef helper. The record is NEVER cleared (it is
	// game-long) and the append DEDUPS, so the fold is idempotent and a
	// repeated source keeps one entry. Guarded to totality like every
	// case here: a missing source or recipient, or an out-of-range seat,
	// is a no-op rather than a panic.
	if e.Obj == 0 || len(e.IDs) == 0 {
		return
	}
	src := e.Obj
	if o := g.Obj(src); o != nil && e.Amount > 0 {
		oldText, sourceWords, hasSource := strings.Cut(e.Text, DamageProvenanceSourceSeparator)
		head, typeWords, hasTypes := strings.Cut(oldText, DamageProvenanceTypeSeparator)
		provenance, colours, hasColours := strings.Cut(head, DamageProvenanceColorSeparator)
		record := state.DamageDealtRecord{
			SourceControl: o.Controller, Recipient: e.IDs[0], Amount: e.Amount, Combat: provenance == DamageProvenanceCombat,
			SourceColors: colours, HasSourceColors: hasColours,
		}
		if hasTypes {
			record.RecipientTypes = strings.Split(typeWords, DamageProvenanceTypeWordSeparator)
		}
		if hasSource {
			zoneWord, sourceTypes, hasSourceTypes := strings.Cut(sourceWords, DamageProvenanceSourceTypeSeparator)
			if zone, err := strconv.ParseUint(zoneWord, 10, 8); err == nil && hasSourceTypes {
				record.SourceZone = state.Zone(zone)
				record.HasSourceZone = true
				record.SourceTypes = strings.Split(sourceTypes, DamageProvenanceTypeWordSeparator)
			}
		}
		if recipient := g.Obj(e.IDs[0]); recipient != nil {
			record.RecipientZone = recipient.Zone
			record.RecipientControl = recipient.Controller
			if face := recipient.Face(); face != nil && !hasTypes {
				record.RecipientTypes = append([]string(nil), face.Types...)
			}
		}
		o.DamageDealtThisTurn = append(o.DamageDealtThisTurn, record)
	}
	if p, isPlayer := e.IDs[0].PlayerRef(); isPlayer {
		if !validPlayer(g, p) {
			return
		}
		rec := g.Players[p].DamageTakenByGame
		if !containsObjID(rec, src) {
			g.Players[p].DamageTakenByGame = append(rec, src)
		}
		return
	}
	if o := g.Obj(e.IDs[0]); o != nil {
		if !containsObjID(o.DamageTakenByGame, src) {
			o.DamageTakenByGame = append(o.DamageTakenByGame, src)
		}
		if !containsObjID(o.DamageTakenThisTurnBy, src) {
			o.DamageTakenThisTurnBy = append(o.DamageTakenThisTurnBy, src)
		}
	}
}

// foldTokenAttacks folds Kind TokenAttacks into state.
func foldTokenAttacks(g *state.Game, e *Event) {
	// A permanent that entered tapped and attacking (TokenAttacking$ or a
	// move body's Attacking$ True rider). Unlike MyriadCopy -- which MINTS
	// a copy of the source card and flags IsMyriad, which MyriadCleanup
	// exiles at end of combat -- this marks an ALREADY-EXISTING battlefield
	// object: Obj is the permanent, Player its controller and IDs[0] the
	// defender it attacks. The object must
	// still be on the battlefield and both players valid; anything else
	// (a gone token, a fuzz event) is a no-op.
	if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZBattlefield &&
		validPlayer(g, e.Player) && len(e.IDs) > 0 && validPlayer(g, state.PlayerID(e.IDs[0])) {
		var battle state.ObjID
		if len(e.IDs) > 1 {
			b := g.Obj(e.IDs[1])
			if b == nil || b.Zone != state.ZBattlefield {
				return
			}
			battle = b.ID
		}
		o.Tapped = true
		o.IsAttacking = true
		o.Attacking = state.PlayerID(e.IDs[0])
		o.AttackingBattle = battle
	}
}

// foldMyriadCopy folds Kind MyriadCopy into state.
func foldMyriadCopy(g *state.Game, e *Event) {
	// CR 702.109: a Myriad attacker token. Mint a copy of the source
	// attack-creature (same face/power/toughness, marked IsToken and
	// IsCopy) tapped and attacking the opponent named by Player. If the
	// source is gone the copy still enters (from its last-known
	// characteristics: we clone the object as it is now, which is the
	// best available LKI for a transient copy).
	//
	// This event only MINTS the object (in the untracked ZLibrary state
	// AddObject leaves it in); it deliberately does NOT move it onto the
	// battlefield. The caller (effects/myriad.go) follows this event with
	// a genuine MoveZone event for the mint's ID, so the token's entry is
	// an ordinary ChangesZone-matchable event and existing
	// enters-the-battlefield triggers (the token's own ETBs, and any
	// other permanent's "creature enters" trigger) see it exactly as they
	// would a cast or reanimated creature. A direct Move() here, as
	// TokenCreate/CardToken use, would leave the entry invisible to
	// trigmatch.ZoneChangeMatches (Mode$ ChangesZone requires ev.Kind ==
	// events.MoveZone).
	src := g.Obj(e.Obj)
	if validPlayer(g, e.Player) && src != nil && src.Face() != nil {
		o := g.AddObject(src.Card, e.Player)
		o.SetFaceIdx(src.FaceIdx)
		o.IsToken = true
		o.IsCopy = true
		o.IsMyriad = true
		o.Tapped = true
		o.IsAttacking = true
		if len(e.IDs) > 0 {
			o.Attacking = state.PlayerID(e.IDs[0])
		}
	}
}

// foldMyriadCleanup folds Kind MyriadCleanup into state.
func foldMyriadCleanup(g *state.Game, e *Event) {
	// CR 702.109a: every token created by Myriad is exiled at end of
	// combat. Move is called only from this event fold, so replay performs
	// the same deterministic arena-order cleanup without synthetic events.
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.IsMyriad && o.Zone == state.ZBattlefield {
			Move(g, o.ID, state.ZBattlefield, state.ZExile)
		}
	}
}

// foldEnlist folds Kind Enlist into state.
func foldEnlist(g *state.Game, e *Event) {
	// CR 702.160's enlist action (the `K:Enlist` keyword, task enlist1):
	// Obj is the ATTACKING creature that enlisted (the Mode$ Enlisted
	// trigger's source) and IDs[0] the nonattacking creature it tapped
	// (never a state change here -- the tap is its own Tap event). The
	// fold stamps the attacker's per-combat marker: (Turn,
	// CombatsThisTurn), so the enlistedThisCombat filter predicate can
	// answer "enlisted THIS combat" and reset itself when a later combat
	// begins without an enlist (state.Object.EnlistedTurn/EnlistedCombat,
	// cleared at TurnChange). Totality: a missing object or an absent
	// enlisted id is a no-op, never a panic.
	o := g.Obj(e.Obj)
	if o == nil || len(e.IDs) == 0 {
		return
	}
	o.EnlistedTurn = g.Turn
	o.EnlistedCombat = g.CombatsThisTurn
}

// foldCrew folds Kind Crew into state. Kind Saddle (CR 702.171) is folded here
// too: its payload is the same (Obj the paying creature, IDs[0] the permanent
// it paid for), and the Creature.SaddledThisTurn filter reads the same
// per-turn pairing, so one set serves both. The set is keyed by the target
// permanent's id, so a Vehicle's crewers and a Mount's saddlers never collide.
func foldCrew(g *state.Game, e *Event) {
	// CR 702.122's crew action (the `K:Crew` keyword): Obj is the CREWING
	// creature and IDs[0] the Vehicle it crewed. The fold records the
	// source-relative pairing the Creature.CrewedThisTurn filter reads:
	// the turn and the crewed Vehicle. A creature that crews the same
	// Vehicle twice in one turn (it untapped in between) records one
	// entry, and one that crews a second Vehicle appends it, so the list
	// is a set with last-write-wins duplicates dropped. Totality: a
	// missing object or an absent Vehicle id is a no-op, never a panic.
	o := g.Obj(e.Obj)
	if o == nil || len(e.IDs) == 0 || e.IDs[0] == 0 {
		return
	}
	if o.CrewedTurn != g.Turn {
		o.CrewedTurn = g.Turn
		if len(o.CrewedVehicles) != 0 {
			g.ClearCrewedObject()
		}
		o.CrewedVehicles = o.CrewedVehicles[:0]
	}
	found := false
	for _, v := range o.CrewedVehicles {
		if v == e.IDs[0] {
			found = true
			break
		}
	}
	if !found {
		wasEmpty := len(o.CrewedVehicles) == 0
		o.CrewedVehicles = append(o.CrewedVehicles, e.IDs[0])
		if wasEmpty {
			g.NoteCrewedObject()
		}
	}
}

// foldExert folds Kind Exert into state.
func foldExert(g *state.Game, e *Event) {
	// CR 702.100's fold (task exert1). Amount >= 0 is the exert itself:
	// both lifetimes stamp here -- ExertedThisTurn (the per-turn fact the
	// notExertedThisTurn offer gate and the "as it attacks" walkers
	// read) and ExertSkipUntap (the consumed-at-use no-untap window,
	// cleared by the Amount -1 consume marker the untap-step scan emits
	// when it passes the permanent). Totality: a missing object is a
	// no-op, never a panic.
	o := g.Obj(e.Obj)
	if o == nil {
		return
	}
	if e.Amount < 0 {
		o.ExertSkipUntap = false
		return
	}
	o.ExertedThisTurn = true
	o.ExertSkipUntap = true
}

// foldGoad folds Kind Goad into state.
func foldGoad(g *state.Game, e *Event) {
	if o := g.Obj(e.Obj); o != nil {
		if e.Amount == -1 {
			o.Goads = nil
			return
		}
		if !validPlayer(g, e.Player) {
			return
		}
		duration := e.Text
		if duration == "" {
			duration = "UntilYourNextTurn"
		}
		var source state.ObjID
		if len(e.IDs) > 0 {
			source = e.IDs[0]
		}
		controller := o.Controller
		if e.Amount > 0 && int(e.Amount-1) < len(g.Players) {
			controller = state.PlayerID(e.Amount - 1)
		}
		ge := state.GoadEffect{Player: e.Player, Source: source, Controller: controller, Duration: duration}
		for _, existing := range o.Goads {
			if existing == ge {
				return
			}
		}
		o.Goads = append(o.Goads, ge)
		g.NoteGoad()
		pruneGoads(g)
	}
}

// foldPhaseOut folds Kind PhaseOut into state.
func foldPhaseOut(g *state.Game, e *Event) {
	// CR 702.25's phased-out status (api:Phases): Amount 1 phases the
	// permanent OUT, -1 phases it IN. Gated on the battlefield, the same
	// way TurnFaceUp's face-down clear is -- a marker stranded on a card
	// that left the battlefield is not a phase. The Move fold clears
	// PhasedOut on every real battlefield departure, so a later entry
	// (CR 400.7) starts phased in.
	if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZBattlefield {
		o.PhasedOut = e.Amount >= 1
		if e.Amount >= 1 {
			o.WontPhaseInNormal = e.Text == "wont-phase-in-normal"
		} else {
			o.WontPhaseInNormal = false
		}
		// CR 702.25c: "A permanent that phases out is removed from
		// combat." Phasing is deliberately NOT a zone change, so no Move
		// fold runs to clear the combat members the way a departure does;
		// clear them here with the exact EndCombatReset{Obj} shape, so a
		// phased-out ATTACKER stops assigning and receiving combat damage
		// and a phased-out BLOCKER stops absorbing it. A zero tombstone is
		// left in each attacking creature's BlockedBy (CR 509.1h: the
		// attacker stays blocked even though its blocker is gone), which
		// is exactly what liveBlockers and damageStep already read.
		if o.PhasedOut {
			for i := range g.Objs {
				other := &g.Objs[i]
				if other.ID == e.Obj {
					other.IsAttacking = false
					other.AttackingBattle = 0
					other.BlockedBy = nil
					continue
				}
				for j, id := range other.BlockedBy {
					if id == e.Obj {
						other.BlockedBy[j] = 0
					}
				}
			}
		}
	}
}
