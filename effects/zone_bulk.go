package effects

import (
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// changeZoneAllPlayers resolves the player scope Forge's ChangeZoneAllEffect
// applies. A card that says "exile all cards from target player's graveyard"
// (Bojuka Bog, Tormod's Crypt, Nihil Spellbomb, ...) names ONE player and must
// move only that player's cards; without this scope the effect swept every
// player's zones. Forge's rule:
//
//	if ((!sa.usesTargeting() && !sa.hasParam("Defined")) || UseAllOriginZones$ True)
//	    -> every player
//	else
//	    -> the chosen target players when the ability uses targeting, else the
//	       Defined$ players (getTargetPlayers; targeting wins when both ride)
//
// Both scope forms resolve through the shared player-target vocabulary
// (`Defined` / `definedSpec`), so `ValidTgts$ Player`, `ValidTgts$ Opponent`,
// `Defined$ You`, `Defined$ TargetedController` and the rest all work without a
// second spelling table. A selector we cannot resolve to a PLAYER (an unknown
// spelling, or an object-only one) keeps the pre-fix all-players sweep rather
// than silently moving nothing, and emits a Note saying so: the unscoped sweep
// is the previous behaviour, so an unmodelled card is never quietly inert, and
// a card we DO understand is correctly restricted. `Origin$` handling is not
// touched -- ParseZones already splits `Hand,Graveyard` into two zones and the
// Any/All wildcard is resolved by the caller before this runs.
func changeZoneAllPlayers(h Host, c *Ctx, sa *cards.SA) []state.PlayerID {
	g := h.Game()
	if strings.EqualFold(strings.TrimSpace(sa.Params["UseAllOriginZones"]), "True") {
		return g.AliveFrom(0)
	}
	_, targeting := sa.Param(cards.PKValidTgts)
	_, defined := sa.Param(cards.PKDefined)
	if !targeting && !defined {
		return g.AliveFrom(0)
	}
	var chosen []state.Target
	if targeting {
		// Forge's getTargetPlayers reads the SA's ANSWERED target players when
		// it uses targeting, never Defined$; the pre-ask answered set outranks
		// the resolution list exactly as Defined's own targeting branch does.
		if c.PickedTargets != nil {
			chosen = c.PickedTargets
		} else {
			chosen = c.Targets
		}
	} else {
		chosen = Defined(h, c, sa)
	}
	seen := make(map[state.PlayerID]bool, len(chosen))
	out := make([]state.PlayerID, 0, len(chosen))
	for _, t := range chosen {
		if !t.IsPlayer || seen[t.Player] {
			continue
		}
		seen[t.Player] = true
		out = append(out, t.Player)
	}
	if len(out) == 0 {
		sel := sa.ParamStr(cards.PKDefined)
		if targeting {
			sel = sa.ParamStr(cards.PKValidTgts)
		}
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "ChangeZoneAll could not resolve a player scope from " + sel + "; sweeping all players"})
		return g.AliveFrom(0)
	}
	return out
}

func effChangeZoneAll(h Host, c *Ctx, sa *cards.SA) {
	if exileHostGone(h, c, sa) {
		return
	}
	from, all, valid := ParseZones(sa.ParamStr(cards.PKOrigin))
	if !valid {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
			Text: "unrecognised ChangeZoneAll Origin " + sa.ParamStr(cards.PKOrigin)})
		return
	}
	if all {
		from = []state.Zone{
			state.ZLibrary, state.ZHand, state.ZBattlefield, state.ZGraveyard,
			state.ZExile, state.ZStack, state.ZCommand, state.ZCeased,
		}
	}
	to := ParseZone(sa.ParamStr(cards.PKDestination))
	spec := sa.ParamStr(cards.PKChangeType)
	if spec == "" {
		spec = "Card"
	}
	// ChangeNum$ caps the sweep (expert_level_safe's DBOpenSafe writes "All",
	// bone_dancer's DBChangeZone writes "1"): an omitted value or "All" moves
	// every matching card -- the behaviour the primitive always had -- while a
	// numeric cap (a literal, or an SVar/X reference through Num) moves at
	// most that many, in the sweep's own scan order (zone-major, seat-minor;
	// a RandomOrder$ sweep's shuffle picks WHICH candidates sit under the
	// cap, since the shuffle only sets the move order). A value Num cannot
	// resolve degrades to 0 by Num's own documented convention -- "the card
	// did nothing", the fail-closed direction.
	changeCap := int32(-1) // -1: uncapped
	if raw := strings.TrimSpace(sa.ParamStr(cards.PKChangeNum)); raw != "" && !strings.EqualFold(raw, "All") {
		changeCap = Num(h, c, sa, "ChangeNum", 0)
		if changeCap < 0 {
			changeCap = 0
		}
	}
	g := h.Game()
	// LibraryPosition$ (Terminus' "put all creatures on the bottom of their
	// owners' libraries") and Shuffle$ (Jace, the Mind Sculptor's [-12]
	// "shuffles their hand into their library", Gomazoa's "put on top ... then
	// those players shuffle") both act on the DESTINATION libraries, which are
	// each object's OWNER's library — a battlefield creature controlled by
	// another player (the Gomazoa / Vortex Elemental blocking shapes) still
	// returns to its owner's library, because the MoveZone keeps its owner.
	// The move loop therefore records every destination-library OWNER that had
	// a card moved (read off the object, not the source-zone player), in the
	// loop's own deterministic (zone-major, AliveFrom(0)-minor) order.
	position := strings.TrimSpace(sa.ParamStr(cards.PKLibraryPosition))
	shuffle := strings.EqualFold(sa.ParamStr(cards.PKShuffle), "True")
	type ownerMoved struct {
		owner state.PlayerID
		ids   []state.ObjID
	}
	var placements []ownerMoved
	// AtEOT$'s affected set for ChangeZoneAll is the objects the sweep
	// actually moved, collected in move order.
	var moved []state.ObjID
	findOwnerMoved := func(owner state.PlayerID) *ownerMoved {
		for i := range placements {
			if placements[i].owner == owner {
				return &placements[i]
			}
		}
		placements = append(placements, ownerMoved{owner: owner})
		return &placements[len(placements)-1]
	}
	players := changeZoneAllPlayers(h, c, sa)
	// ForgetOtherRemembered$ True (The Mimeoplasm's MimeoExile, 11 corpus
	// ChangeZoneAll carriers): Forge forgets every previously remembered
	// object before this effect resolves, so a setup that remembered its own
	// candidates (the ChooseCard's RememberChosen$) plus stale memory from an
	// earlier resolution leaves exactly the moved set behind (RememberChanged$
	// re-remembers it). The ChangeType$ Card.IsRemembered selector reads the
	// very memory the clear drops, so the matched set is snapshotted BEFORE
	// the clear and the sweep below matches against the snapshot -- matching
	// after the clear would sweep nothing.
	var preMatched map[state.ObjID]bool
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKForgetOtherRemembered)), "True") {
		preMatched = make(map[state.ObjID]bool)
		for _, z := range from {
			for _, p := range players {
				for _, id := range g.Zone(z, p) {
					if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
						preMatched[id] = true
					}
				}
			}
		}
		c.Remembered = nil
		clearEventRemembered(h, c)
	}
	// RandomOrder$ True (task mordorparams1, Gríma, Saruman's Footman's
	// "Then that player puts the exiled cards that weren't cast this way on
	// the bottom of their library in a random order"): the destination
	// placement order is a real shuffle, not the engine's scan order. The
	// cards are COLLECTED first (the same zone-major/seat-minor scan, no
	// emission), Fisher-Yates'd per destination-library owner through the
	// seeded engine rng (the randomChoices/Host.Rand precedent — a replay
	// re-derives the identical order), and only then emitted, so the
	// MoveZone appends settle in the shuffled order. The shuffle only sets
	// the MOVE ORDER; the LibraryPosition$/Shuffle$ tail below still applies
	// on top of it (Triumph of Saint Katherine's `LibraryPosition$ 0` after
	// a RandomOrder$ sweep puts the shuffled pile on TOP, not the bottom).
	// The Dig/RestRandomOrder$/RevealRandomOrder$ variants are their own rows
	// and are not touched here.
	randomOrder := strings.EqualFold(strings.TrimSpace(sa.Params["RandomOrder"]), "True")
	rider := classifyAttackingEntry(c, sa, to)
	// A sweep off the battlefield is one simultaneous departure (CR 603.10a):
	// every member's leaves-the-battlefield triggers look back at the same
	// pre-sweep board. The id list is empty -- the sweep emits as it scans --
	// so only the shared board is parked; the per-object lifelink capture
	// keeps its live read.
	if to != state.ZBattlefield && slices.Contains(from, state.ZBattlefield) {
		h.BatchDepartures(nil)
		defer h.EndBatchDepartures()
	}
	emitMove := func(id state.ObjID, z state.Zone, p state.PlayerID) {
		// A CantExile restriction withholds the object from a battlefield exile
		// before the MoveZone (and the moved bookkeeping) is produced -- the
		// ChangeZoneAll half of the same guard effChangeZone's object loop and
		// settleChangeZoneMoveAs carry.
		if to == state.ZExile && h.ExileBlocked(id, false) {
			return
		}
		// Capture before MoveZone folds: battlefield departure resets control,
		// clears counters and removes battlefield-derived characteristics.
		if strings.EqualFold(sa.ParamStr(cards.PKRememberLKI), "True") {
			if o := g.Obj(id); o != nil {
				snapshot := o.CloneDeep()
				c.ChangeZoneLKI = append(c.ChangeZoneLKI, state.LKIObject{
					Obj: id, Controller: o.Controller, Owner: o.Owner, Snapshot: snapshot,
				})
			}
		}
		ev := moveZoneEvent(c, id, z, to)
		applyFaceDownMarker(h, sa, c, &ev, to)
		h.Emit(ev)
		moved = append(moved, id)
		exiledWithAssociation(h, c, id, to)
		// Tapped$ True (Splendid Reclamation's "Return all land cards
		// ... tapped"): a battlefield entry is followed by the same
		// "entered tapped" Tap event every other Tapped$ zone-change
		// path emits -- an entry state, not the CR 701.21a event of
		// becoming tapped.
		if to == state.ZBattlefield && strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKTapped)), "True") {
			h.Emit(events.Event{Kind: events.Tap, Obj: id, Player: p, Text: "entered tapped"})
		}
		rider.apply(h, c, id, p, to)
		if to == state.ZExile {
			recordExileReturn(h, c, sa, id, z, to)
		}
		// GainControl$ hands the moved object to the named player
		// (Karn Liberated's ReturnFromExile, Cold Storage, Ghost
		// Vacuum). Only a battlefield entry can carry a control
		// change (CR 701.22a controls permanents), the same rule the
		// ChangeZone path applies; the shared resolver is loud rather
		// than silent on an unresolvable selector.
		if to == state.ZBattlefield {
			applyGainControl(h, c, sa, id)
			// StaticEffect$ (ChangeZoneAll's carriers -- Ghost Vacuum, Grimoire
			// of the Dead, Storm of Souls, Shilgengar): the same per-card rider
			// registration every other ChangeZone mover applies.
			applyStaticEffect(h, c, sa, to, []state.ObjID{id})
		}
		if to == state.ZLibrary {
			owner := p
			if o := g.Obj(id); o != nil {
				owner = o.Owner
			}
			findOwnerMoved(owner).ids = append(findOwnerMoved(owner).ids, id)
		}
		// RememberChanged$ True re-remembers the moved cards in both halves
		// (the ctx list the chain's later sub-abilities read and the source's
		// event-backed persistent list a later resolution's IsRemembered /
		// Remembered$ head reads -- The Mimeoplasm's MimeoChooseCopy, Gift of
		// Immortality's return trigger). Previously this recorded the ctx
		// entries alone and only for the ExiledWithSource provenance shape
		// (Valakut Exploration); the persistent half is what the Mimeoplasm
		// chain's IsRemembered/Remembered$CardPower reads need.
		if strings.EqualFold(sa.ParamStr(cards.PKRememberLKI), "True") &&
			!strings.EqualFold(sa.ParamStr(cards.PKRememberChanged), "True") {
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
		}
		if strings.EqualFold(sa.ParamStr(cards.PKRememberChanged), "True") {
			c.Remembered = append(c.Remembered, state.Target{Obj: id})
			eventRemember(h, c, id)
		}
	}
	if randomOrder {
		type pendingMove struct {
			id state.ObjID
			z  state.Zone
			p  state.PlayerID
		}
		var owners []state.PlayerID
		byOwner := make(map[state.PlayerID][]pendingMove)
		for _, z := range from {
			for qi, p := range players {
				// The shared stack (state/game.go Zone) is snapshotted once,
				// under the first player in the resolved scope. Without this an
				// N-player sweep enqueues the same stack object N times and
				// emits N MoveZones for one card. Other origins stay per-player.
				if z == state.ZStack && qi > 0 {
					continue
				}
				// Snapshot the zone exactly like the emit loop does.
				ids := append([]state.ObjID(nil), g.Zone(z, p)...)
				for _, id := range ids {
					if preMatched != nil {
						if !preMatched[id] {
							continue
						}
					} else if !MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
						continue
					}
					owner := p
					if o := g.Obj(id); o != nil {
						owner = o.Owner
					}
					if _, seen := byOwner[owner]; !seen {
						owners = append(owners, owner)
					}
					byOwner[owner] = append(byOwner[owner], pendingMove{id: id, z: z, p: p})
				}
			}
		}
		for _, owner := range owners {
			list := byOwner[owner]
			for i := len(list) - 1; i > 0; i-- {
				j := h.Rand(i + 1)
				list[i], list[j] = list[j], list[i]
			}
			byOwner[owner] = list
		}
	emitLoop:
		for _, owner := range owners {
			for _, pm := range byOwner[owner] {
				if changeCap >= 0 && int32(len(moved)) >= changeCap {
					break emitLoop
				}
				emitMove(pm.id, pm.z, pm.p)
			}
		}
	} else {
	sweep:
		for _, z := range from {
			for qi, p := range players {
				// Same shared-stack guard as the RandomOrder$ branch: one
				// snapshot of the stack, taken under the first scoped player.
				if z == state.ZStack && qi > 0 {
					continue
				}
				// Snapshot the zone: emitting move events mutates it underneath us.
				ids := append([]state.ObjID(nil), g.Zone(z, p)...)
				for _, id := range ids {
					if changeCap >= 0 && int32(len(moved)) >= changeCap {
						break sweep
					}
					matched := false
					if preMatched != nil {
						matched = preMatched[id]
					} else {
						matched = MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller))
					}
					if matched {
						emitMove(id, z, p)
					}
				}
			}
		}
	}
	// The post-placement tail runs for BOTH branches: the shuffled order is
	// only the ORDER the moves settle in, so `LibraryPosition$ 0` (Triumph of
	// Saint Katherine's "shuffle that pile and put it back on TOP of your
	// library") and `Shuffle$` must still apply after a RandomOrder$ sweep.
	if to == state.ZLibrary && len(placements) > 0 {
		// LibraryPosition$: MoveZone already appends at the bottom of the
		// destination library in settle order, so "-1" (Terminus) is exactly the
		// move order and needs no extra event; "0" pins the moved cards on TOP
		// via the one Secret LibraryOrder placement every library placement
		// shares. The ABSENT spelling is also TOP (golgari_thug2):
		// ChangeZoneAllEffect computes libPos = 0 when LibraryPosition$ is
		// absent, the same default the hand path and the object-target path
		// apply. Any other value is loud rather than silently inert.
		switch position {
		case "-1":
		case "", "0":
			for _, pm := range placements {
				libraryOrderPlacement(h, pm.owner, pm.ids, false)
			}
		default:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "LibraryPosition$ " + position + " is not implemented; the cards sit at the BOTTOM of their owners' libraries (the MoveZone append)"})
		}
		// Shuffle$ True shuffles each destination library that received a card,
		// AFTER the placement (Gomazoa's "put on top ..., then those players
		// shuffle" order), through the same Secret events.Shuffle every other
		// library shuffle emits.
		if shuffle {
			for _, pm := range placements {
				shuffleLibraryOrder(h, pm.owner)
			}
		}
	}
	scheduleAtEOT(h, c, sa, moved)
}

// effDestroy is a single-target removal effect: exactly the shape CR 608.2b
// target rechecking exists for. Today the only recheck is "does the target
// still exist, and is it still on the battlefield" -- a target that stayed on
// the battlefield but became newly ineligible some other way (e.g. it gained
// Indestructible in response, or protection from the source) between
// targeting and resolution is not rechecked. See the Task 18 report.
func effDestroy(h Host, c *Ctx, sa *cards.SA) {
	// Forge's ForgetOtherTargets$ replaces the prior remembered set before
	// this Destroy, while RememberTargets$ records only objects that actually
	// leave the battlefield (not targets spared by regeneration or
	// indestructibility).  Keep both the resolution-local and event-backed
	// halves in sync, as the chained sub-ability may read either one.
	if strings.EqualFold(strings.TrimSpace(sa.Params["ForgetOtherTargets"]), "True") {
		c.Remembered = nil
		clearEventRemembered(h, c)
	}
	// RememberTargets$ records only objects that actually leave the
	// battlefield. RememberDestroyed$ True (Transforming Flourish) is Forge's
	// spelling of the same "this permanent was destroyed this way" record.
	// RememberLKI$ True (Noxious Gearhulk) does BOTH -- it records the object
	// and captures its last-known-information snapshot (CR 603.10 look-back),
	// because the chained read (RememberedLKI$CardToughness) needs the
	// battlefield P/T that events.Apply's Move clears. The snapshot rides
	// Ctx.LKI/LKIPower/LKIToughness, the same fields rules' triggerLKI publishes
	// and evalRefProperty reads for a zone-change trigger; evalRefProperty
	// applies it only when the snapshot names the referenced object, so no
	// other remembered read is affected.
	rememberTargets := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberTargets)), "True")
	rememberDestroyed := strings.EqualFold(strings.TrimSpace(sa.Params["RememberDestroyed"]), "True")
	rememberLKI := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberLKI)), "True")
	// Same pre-batch discipline as effDestroyAll: the targets Defined
	// resolves are destroyed as one simultaneous batch (a multi-target
	// Destroy over a lifelink Equipment and its bearer must not make the
	// bearer's LKI depend on battlefield order), so the snapshot covers all
	// of them before the first move.
	var victims []state.ObjID
	for _, t := range Defined(h, c, sa) {
		o := h.Game().Obj(t.Obj)
		if t.IsPlayer || o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		if h.HasKeyword(o.ID, "Indestructible") {
			continue
		}
		victims = append(victims, o.ID)
	}
	if len(victims) > 0 {
		h.BatchDepartures(victims)
		defer h.EndBatchDepartures()
	}
	for _, id := range victims {
		o := h.Game().Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		// NoRegen$ is compared against "True", not against empty: an explicit
		// NoRegen$ False PERMITS regeneration, and reading it as "set, so
		// suppress" would invert the card. The corpus splits 144 True / 1
		// False (creepy_doll.txt), and that one is unreachable today because
		// cards/link.go auto-links only SubAbility$, not the WinSubAbility$ it
		// hangs off -- so this is correctness insurance for when that changes,
		// not a live fix.
		if sa.Params["NoRegen"] != "True" && ReplaceDestruction(h, id) {
			continue
		}
		// Umbra armor (CR 702.90) applies even when NoRegen$ suppresses
		// regeneration — it is its own replacement, not a shield. Consuming
		// the Aura leaves it in the graveyard; when the loop reaches the Aura
		// itself (a DestroyAll that named it too) the zone guard above skips it.
		if ReplaceUmbraArmor(h, id) {
			continue
		}
		// Capture the last-known information BEFORE the Move folds: the
		// snapshot must see the battlefield permanent (its counters, pump
		// layers and printed toughness), not the graveyard card the move
		// leaves behind. The zone guard above proved o is on the battlefield.
		var lki *state.Object
		var lkiPower, lkiToughness int32
		var lkiValid bool
		if rememberLKI {
			cp := o.CloneDeep()
			lki = &cp
			lkiPower, lkiToughness = h.Power(id), h.Toughness(id)
			lkiValid = true
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
			From: state.ZBattlefield, To: state.ZGraveyard, Text: "destroyed"})
		// Host.Emit applies move replacements before folding the move. Only
		// remember a permanent that actually ended up in the graveyard; a
		// replacement such as exile must not feed a later IsRemembered search.
		if rememberTargets || rememberDestroyed || rememberLKI {
			if moved := h.Game().Obj(id); moved != nil && moved.Zone == state.ZGraveyard {
				if rememberLKI {
					c.LKI = lki
					c.LKIPower, c.LKIToughness, c.LKIPTValid = lkiPower, lkiToughness, lkiValid
				}
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
	}
}

func effDestroyAll(h Host, c *Ctx, sa *cards.SA) {
	spec := sa.ParamStr(cards.PKValidCards)
	if spec == "" {
		spec = "Permanent"
	}
	zone := state.ZBattlefield
	if raw := strings.TrimSpace(sa.ParamStr(cards.PKZone)); raw != "" {
		var ok bool
		zone, ok = ParseZoneWord(raw)
		if !ok {
			return
		}
	}
	g := h.Game()
	remember := strings.EqualFold(strings.TrimSpace(sa.Params["RememberDestroyed"]), "True")
	// One pre-batch victim list across every player, then ONE departure
	// snapshot, then the emit loop (CR 704.3 simultaneity, as far as the
	// sequential emit model can express it): the CR 603.10a lifelink LKI a
	// later victim's departure capture reads must be the state from
	// immediately before the FIRST move -- a destroy-all over a
	// lifelink-granting Equipment and its bearer must not make the bearer's
	// own lifelink LKI depend on battlefield order.
	var victims []state.ObjID
	sc := c.SpecContext(c.Controller)
	sc.CombatDamageHits = h.CombatDamageToPlayersThisTurn()
	for _, p := range g.AliveFrom(0) {
		ids := append([]state.ObjID(nil), g.Zone(zone, p)...)
		for _, id := range ids {
			if zone == state.ZBattlefield && h.HasKeyword(id, "Indestructible") {
				continue
			}
			if MatchesSpecCtx(g, spec, id, sc) {
				victims = append(victims, id)
			}
		}
	}
	if len(victims) > 0 && zone == state.ZBattlefield {
		h.BatchDepartures(victims)
		defer h.EndBatchDepartures()
	}
	for _, id := range victims {
		if g.Obj(id) == nil || g.Obj(id).Zone != zone {
			continue
		}
		if zone == state.ZBattlefield {
			// NoRegen$ != "True", not == "": see effDestroy's note above.
			if sa.Params["NoRegen"] != "True" && ReplaceDestruction(h, id) {
				continue
			}
			// Umbra armor after the shield: see effDestroy's note.
			if ReplaceUmbraArmor(h, id) {
				continue
			}
		}
		h.Emit(events.Event{Kind: events.MoveZone, Obj: id,
			From: zone, To: state.ZGraveyard, Text: "destroyed"})
		if remember {
			// Forge's RememberDestroyed$ adds only cards that actually
			// reached the graveyard; a move replacement may redirect it.
			if moved := h.Game().Obj(id); moved != nil && moved.Zone == state.ZGraveyard {
				c.Remembered = append(c.Remembered, state.Target{Obj: id})
				eventRemember(h, c, id)
			}
		}
	}
}

// sacTargetCardReferent resolves a SacValid$ spec that names the resolution's
// card TARGET rather than the resolving source. Forge writes this referent as
// the base `TargetedCard` with the `.Self` property (“the card this ability
// targeted”), e.g. Enchanter's Bane's `SacValid$ TargetedCard.Self`: the
// sacrifice is aimed at the targeted enchantment's controller, and the only
// eligible permanent is that enchanted enchantment itself. The engine's filter
// grammar reads a bare `Self` relative to the resolving SOURCE, so this base
// is unrecognised there and fails closed; resolving it here, against the
// resolution's object targets, is what admits the actual targeted card. Any
// other base (including `Self`/`Card.Self`, whose subject IS the source) and any
// qualifier other than `.Self` are not this referent and return ok=false,
// leaving MatchesSpecCtx's own reading in place -- fail closed, never widened.
// The caller checks the returned id against the pool object, so a referent that
// is not in the asked player's battlefield never becomes eligible.
func sacTargetCardReferent(spec string, c *Ctx) (state.ObjID, bool) {
	base, qual, _ := strings.Cut(strings.TrimSpace(spec), ".")
	if base != "TargetedCard" || qual != "Self" {
		return 0, false
	}
	for _, t := range c.Targets {
		if !t.IsPlayer && t.Obj != 0 {
			return t.Obj, true
		}
	}
	return 0, false
}

// effSacrifice moves permanents to the graveyard. Sacrifice ignores
// Indestructible: sacrificing is not destruction (CR 701.16), so no
// HasKeyword/Indestructible gate and no ReplaceDestruction/regeneration
// consultation -- a regenerated creature does not survive being sacrificed.
// Same CR 608.2b caveat as effDestroy: only existence-and-zone is rechecked.
//
// A sacrifice aimed at a PLAYER now asks that player (CR 701.21a: "its
// controller chooses one") through a real KChoose over their matching
// permanents: Amount$ (default 1) sizes the ask, Optional$ True makes it
// "may sacrifice" (Min 0), and a hand of fewer eligible permanents than
// Amount$ sacrifices everything it has without asking (there is no choice
// to record, the effDiscard TgtChoose strict-supersets rule). The answer
// re-enters this effect through ResumeKind "sacrifice" with Ctx.SacPicks
// set, one suspension per Defined$ target (the cursor mirrors effDig's
// per-library asks). An Optional$ ask whose no-host fallback runs takes the
// first Amount$ eligible permanents — the same pick the pre-ask engine made
// — so games that never reach a real player answer replay byte-identically
// up to the pick the answer names.
func effSacrifice(h Host, c *Ctx, sa *cards.SA) {
	// UnlessCost$ is handled by the shared unlessProceed gate in Resolve,
	// exactly as it is for every other API — including the Vexing Devil
	// damage-payment offer (UnlessCost$ DamageYou<N>, UnlessPayer$ Opponent,
	// UnlessSwitched$ True), whose "pay" is taking the damage. When the gate
	// consumed the resolution — an ask was posed (suspended), the answered
	// choice spared the permanent, or every opponent declined the offer —
	// this body does not run at all.
	g := h.Game()
	// SacValid$ narrows WHAT may be sacrificed ("Creature.nonToken",
	// "Artifact"). With no SacValid$ at all the default is "Permanent" (any
	// permanent). The older justification -- that the self-sacrifice and
	// at-end-of-step lines need "Permanent" because the object they sacrifice
	// may be an artifact, a land or a creature -- is empirically false: those
	// lines carry no Defined$ and no ValidTgts$, so Defined() resolves them
	// to the SOURCE object (effects/context.go) and they take the object-target
	// path below.
	//
	// ValidCard$ is the corpus's second narrowing spelling: three Sacrifice
	// SAs carry `ValidCard$ Card.Self` and no SacValid$. Exactly one of them is
	// player-targeted -- Expert-Level Safe's
	// `DB$ Sacrifice | Defined$ You | ValidCard$ Card.Self` -- so before this
	// read its controller handed over whichever permanent sat first in zone
	// order (an artifact, a land, anything) rather than "this artifact". The
	// other two, Departed Deckhand and Dream Strix, carry no Defined$ and no
	// ValidTgts$, so Defined() resolves them to their own source object and
	// they take the object-target path below (where that object already IS the
	// self the spec names). The two spellings never co-occur in the corpus
	// (measured: 3 ValidCard$ lines, 437 SacValid$ lines, 0 carrying both), so
	// applying both as a conjunction reads every line exactly once. Card.Self
	// resolves through SpecContext.Source, so the player-targeted line can only
	// hand over the source itself.
	spec := sa.Params["SacValid"]
	if spec == "" {
		spec = "Permanent"
	}
	validCard := strings.TrimSpace(sa.ParamStr(cards.PKValidCard))
	// RememberSacrificed$ True drives the task's effect-driven sacrifice
	// capture: it makes effSacrifice record the LKI snapshot (power,
	// toughness, mana value) of each object it sacrifices, so a SubAbility$
	// chained after it can resolve Sacrificed$CardPower/CardManaCost/Amount
	// against what THIS ability just sacrificed. Without the flag nothing is
	// remembered -- and nothing is, because the flag is read nowhere else in
	// this package (the sacrifice_audit test only counts its occurrence), so
	// the absence is the conservative same-as-before no-op, not a regression.
	remember := sa.Params["RememberSacrificed"] != ""
	// rememberLKICapture captures the sacrificed object's LKI (before the
	// MoveZone resets its counters) into c.Sacrificed, when the flag asks it
	// to. Idempotent per call site; called exactly once per sacrificed object.
	rememberLKICapture := func(id state.ObjID) {
		if remember {
			c.Sacrificed = append(c.Sacrificed, SacrificedLKI(h, id))
			// Forge's RememberSacrificed$ also remembers the card, which is
			// what a following ConditionDefined$ Remembered, Remembered$Amount
			// or RememberedCard reads (Braids, Scapeshift, Victimize).
			c.Remembered = append(copyTargets(c.Remembered), state.Target{Obj: id})
			eventRemember(h, c, id)
		}
	}
	// fx42 scoping: capture and clear the answered per-player pick BEFORE the
	// target loop, so a nested sacrifice below this walk poses its own ask.
	// SacTarget identifies the exact target that asked: earlier targets
	// completed before suspension and must be skipped, that target consumes
	// the answer, and later targets pose their own asks (Dig's per-library
	// ask shape).
	sacAns := c.SacPicks
	sacDone := c.SacDone
	sacTarget := c.SacTarget
	c.SacPicks, c.SacDone, c.SacTarget = nil, false, 0
	amount := sacrificeAmount(h, c, sa)
	// An Amount$ of zero has no legal sacrifice and, crucially, no meaningful
	// answer. Do not produce a 0..0 KChoose merely because eligible cards
	// happen to exist (an Optional$ Amount$ X trigger with X=0 has this shape).
	if amount <= 0 {
		return
	}
	optional := sa.ParamStr(cards.PKOptional) == "True"
	strict := optional && sa.Params["StrictAmount"] == "True"
	// Optional + StrictAmount is not a 0..Amount range: it is specifically
	// "none, or exactly Amount". The KModes answer is consumed below before a
	// possible exact-batch KChoose; keeping it separate prevents a partial
	// sacrifice from taking the card's "if you do" continuation.
	sacOptional, sacOptionalTarget := c.SacOptional, c.SacOptionalTarget
	c.SacOptional, c.SacOptionalTarget = "", 0
	who := Defined(h, c, sa)
	// ShowSacrificedCards$ True (Demonic Covenant's own sacrifice line): the
	// sacrificed cards are REVEALED publicly — one ids-Note naming everything
	// this call sacrificed, the same payload shape effMill's ShowMilledCards$
	// arm emits. Collected across every path below (the answered batch, the
	// re-entry batch and the plain object path) so one Note covers the call.
	show := strings.EqualFold(strings.TrimSpace(sa.Params["ShowSacrificedCards"]), "True")
	var sacrificed []state.ObjID
	// A Sacrifice that names neither Defined$ nor ValidTgts$ but a SacValid$
	// other than itself is Forge's default Defined$ You: its controller
	// sacrifices a matching permanent (Braids's "you may sacrifice an
	// artifact, creature, ..."). Only a SacValid$ Self/Card.Self line (or no
	// SacValid$ at all) sacrifices the source object itself. Corpus: 66 such
	// lines, which previously sacrificed the source whatever its type.
	if _, targeted := sa.Param(cards.PKValidTgts); !targeted && strings.TrimSpace(sa.ParamStr(cards.PKDefined)) == "" {
		if v := strings.TrimSpace(sa.Params["SacValid"]); v != "" && v != "Self" && v != "Card.Self" {
			who = []state.Target{{Player: c.Controller, IsPlayer: true}}
		}
	}
	for targetIndex, t := range who {
		if sacOptional != "" {
			if targetIndex < sacOptionalTarget {
				continue
			}
			if targetIndex == sacOptionalTarget && sacOptional == "decline" {
				continue
			}
		}
		if sacDone {
			// Re-entry after some target's ask suspended: earlier targets
			// completed on the first pass and must be skipped (re-running
			// them would sacrifice a second batch); the asking target
			// applies its answer; later targets fall through to the normal
			// paths below and pose their own asks (Dig's per-library shape).
			if targetIndex < sacTarget {
				continue
			}
			if targetIndex == sacTarget {
				if t.IsPlayer {
					// Sacrifice exactly the answered cards that still sit on
					// this player's battlefield (a zone check keeps a stray
					// answer from moving an object that left meanwhile), in
					// the player's answer order. One departure snapshot for
					// the whole answered batch (BatchDepartures).
					if len(sacAns) > 0 {
						h.BatchDepartures(sacAns)
						defer h.EndBatchDepartures()
					}
					for _, id := range sacAns {
						if o := g.Obj(id); o == nil || o.Zone != state.ZBattlefield {
							continue
						}
						rememberLKICapture(id)
						sacrificed = append(sacrificed, id)
						h.Emit(events.Sacrifice(id))
					}
				} else if len(sacAns) > 0 {
					// The object-optional ask's sole option was answered
					// "sacrifice it": the object was already zone-checked on
					// the first pass, but re-check here in case it moved.
					if o := g.Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
						rememberLKICapture(o.ID)
						sacrificed = append(sacrificed, o.ID)
						h.Emit(events.Sacrifice(o.ID))
					}
				}
				continue
			}
		}
		if t.IsPlayer {
			// Bounds guard: g.Zone indexes g.zones[zoneIndex(z, p)] and
			// zoneIndex has no bounds check, so an out-of-range target-supplied
			// player id would panic with "index out of range" and halt the
			// table. Player targets normally come from askTarget or AliveFrom
			// and are bounded, but the package's idiom (see cardflow.go and
			// count.go) is not to trust a target blindly.
			if int(t.Player) >= len(g.Players) {
				continue
			}
			// The multi-permanent count defaults to `amount` -- 1 for an
			// ordinary Sacrifice line, or the Amount$ the primitive carries.
			// The Annihilator expansion's generated SA carries its own count
			// in its Annihilator$ marker (cards/keywords.go) and overrides it;
			// the two contexts never coincide in the corpus.
			n := amount
			if ann := sa.Params["Annihilator"]; ann != "" {
				if v, err := strconv.Atoi(ann); err == nil && v >= 0 {
					n = int32(v)
				}
			}
			ids := append([]state.ObjID(nil), g.Zone(state.ZBattlefield, t.Player)...)
			eligible := make([]state.ObjID, 0, len(ids))
			sc := c.SpecContext(t.Player)
			// SacValid$/ValidCard$ can carry a greatestPower comparison
			// (Consume, Consumed by Greed), which must size the whole pool
			// with layer-derived power, exactly as the Choices$ matcher does;
			// the shared builder returns nil for every other spec. The two
			// spellings never co-occur, so appending both cannot double-bind.
			sc.DerivedPTs = append(sc.DerivedPTs, GreatestPowerDerivedPTs(g, spec, h)...)
			if validCard != "" {
				sc.DerivedPTs = append(sc.DerivedPTs, GreatestPowerDerivedPTs(g, validCard, h)...)
			}
			for _, id := range ids {
				if h.SacrificeBlocked(id, false) {
					// A CantSacrifice restriction (Call for Aid) or face static:
					// the permanent is not a sacrifice candidate at all — not
					// offered, never taken (Annihilator rides this same pool).
					continue
				}
				// ValidCard$, when present, narrows the same pool: a permanent
				// must match BOTH spellings (they never co-occur, so this is
				// just SacValid$ and ValidCard$ in turn).
				matchesSacValid := MatchesSpecCtx(g, spec, id, sc)
				if ref, ok := sacTargetCardReferent(spec, c); ok {
					// A SacValid$ that names the resolution's card TARGET
					// (TargetedCard.Self) must admit exactly that object, not the
					// resolving source the filter grammar's bare Self reads. The
					// player-targeted pool is already this player's battlefield,
					// so id == ref is also the "belongs to the player asked"
					// check. Unknown referent forms stay with MatchesSpecCtx,
					// which fails closed.
					matchesSacValid = id == ref
				}
				if matchesSacValid &&
					(validCard == "" || MatchesSpecCtx(g, validCard, id, sc)) {
					eligible = append(eligible, id)
				}
			}
			minv, maxv := int32(0), int32(0)
			ask := false
			// n is the deterministic/no-host batch. An optional strict batch
			// with too few eligible permanents cannot be paid partially, so it
			// starts at zero rather than falling through to the old first-N path.
			if optional {
				if strict {
					switch {
					case sacOptional == "sacrifice" && targetIndex == sacOptionalTarget:
						// The player accepted the first yes/no step. If there is a
						// genuine identity choice, ask for EXACTLY Amount; when every
						// eligible permanent is required, there is nothing left to ask.
						if int32(len(eligible)) > amount {
							ask = true
							minv, maxv = amount, amount
						}
					case int32(len(eligible)) >= amount:
						// KChoose can express a range but not the disjoint set
						// {0, Amount}, so ask yes/no first and only then (above)
						// choose the exact batch.
						d := &decision.Decision{Player: t.Player, Kind: decision.KModes,
							Min: 1, Max: 1, Source: c.Source, ResumeKind: "sacrifice_optional",
							ResumeSA: sa, ResumeTarget: targetIndex,
							Prompt: "Sacrifice " + strconv.Itoa(int(amount)) + " permanent(s)?",
							Options: []decision.Option{
								{Index: 0, Kind: "mode", Label: "Sacrifice " + strconv.Itoa(int(amount)) + " permanent(s)", Obj: c.Source, Player: t.Player},
								{Index: 1, Kind: "mode", Label: "Don't sacrifice", Obj: c.Source, Player: t.Player},
							}}
						if Ask(h, d) == AskAsked {
							return
						}
						// R-9 no-host fallback: preserve the old deterministic pick,
						// but only as a complete strict batch.
						h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: t.Player,
							Text: "sacrifices the first matching permanent(s) (no engine host to ask)", Secret: true})
					case int32(len(eligible)) < amount:
						n = 0
					}
				} else if len(eligible) > 0 {
					// A non-strict optional sacrifice permits any number through
					// Amount$, including none.
					ask = true
					maxv = amount
					if maxv > int32(len(eligible)) {
						maxv = int32(len(eligible))
					}
				}
			} else if int32(len(eligible)) > n {
				// Mandatory with a choice: exactly the batch count of the
				// eligible — Amount$, or the Annihilator$ marker's count when
				// the generated expansion carries one.
				ask = true
				minv, maxv = n, n
			}
			// (the remaining mandatory shape — eligible <= amount — sacrifices
			// everything eligible without asking: no choice to record, the
			// effDiscard TgtChoose strict-supersets rule.)
			if ask {
				d := &decision.Decision{Player: t.Player, Kind: decision.KChoose,
					Min:          int(minv),
					Max:          int(maxv),
					Source:       c.Source,
					ResumeKind:   "sacrifice",
					ResumeSA:     sa,
					ResumeTarget: targetIndex,
					Prompt:       sacrificePrompt(optional && !strict, maxv)}
				for _, id := range eligible {
					name := "a permanent"
					if o := g.Obj(id); o != nil && o.Face() != nil {
						name = o.Face().Name
					}
					d.Options = append(d.Options, decision.Option{Index: len(d.Options),
						Kind: "sacrifice", Label: name, Obj: id, Player: t.Player})
				}
				if Ask(h, d) == AskAsked {
					return // resolution suspended; the answer re-enters with Ctx.SacPicks set.
				}
				// Fuzz/no-engine host: the deterministic stand-in (R-9) keeps
				// the pre-ask behaviour — the first Amount$ eligible permanents
				// in zone order, so an Optional$ "may sacrifice" plays "do".
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: t.Player,
					Text: "sacrifices the first matching permanent(s) (no engine host to ask)", Secret: true})
				n = maxv
			}
			// One departure snapshot per emitted batch (BatchDepartures): a
			// sacrifice sweep over a lifelink-granting Equipment and its bearer
			// must not make the bearer's CR 603.10a lifelink LKI depend on
			// battlefield order. Asks suspend before any emission, so every
			// suspend-then-resume path still re-collects its batch here.
			batch := make([]state.ObjID, 0, n)
			for i := int32(0); i < n && int(i) < len(eligible); i++ {
				batch = append(batch, eligible[i])
			}
			if len(batch) > 0 {
				h.BatchDepartures(batch)
				defer h.EndBatchDepartures()
			}
			for _, id := range batch {
				rememberLKICapture(id)
				sacrificed = append(sacrificed, id)
				h.Emit(events.Sacrifice(id))
			}
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		if h.SacrificeBlocked(o.ID, false) {
			// A CantSacrifice restriction (or face static): this specific
			// object cannot be sacrificed at all — neither offered to its
			// Optional$ ask nor emitted. The targeting already picked it; the
			// restriction is what stops the pick.
			continue
		}
		// A specific object target is sacrificed as-is: the choice of which
		// object was already made by the effect's targeting, so SacValid$'
		// "which one may be sacrificed" step does not re-filter a concrete
		// object (and would misfire on the corpus's SacValid$ Self lines,
		// where "Self" is not a type the filter grammar knows).
		//
		// Optional$ True on an object target is a real yes/no ("you may
		// sacrifice this artifact"): a 0..1 ask over the object, answered
		// through the same "sacrifice" resume. A host that cannot ask keeps
		// the mandatory sacrifice (the pre-ask behaviour).
		if optional {
			// A concrete target cannot satisfy a strict batch greater than one:
			// it may decline, but it must not sacrifice this one object as a
			// partial payment.
			if strict && amount != 1 {
				continue
			}
			d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose,
				Min: 0, Max: 1, Source: c.Source,
				ResumeKind: "sacrifice", ResumeSA: sa, ResumeTarget: targetIndex,
				Prompt: sacrificePrompt(true, 1)}
			name := "a permanent"
			if o.Face() != nil {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: 0,
				Kind: "sacrifice", Label: name, Obj: o.ID, Player: o.Controller})
			if Ask(h, d) == AskAsked {
				return
			}
			// No-host stand-in: the mandatory sacrifice the pre-ask engine
			// made, with the Note that records why the richer path did not run.
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: o.Controller,
				Text: "sacrifices the first matching permanent(s) (no engine host to ask)", Secret: true})
		}
		rememberLKICapture(o.ID)
		sacrificed = append(sacrificed, o.ID)
		h.Emit(events.Sacrifice(o.ID))
	}
	if show && len(sacrificed) > 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, IDs: sacrificed})
	}
}

// ParseDamageUnlessCost reports whether an UnlessCost$ value is the
// damage-payment offer form "DamageYou<N>" and returns N. Recognised: the
// exact spelling (case-insensitive) with a non-negative integer N; anything
// else is not the offer (and falls to the shared gate's pricing).
func ParseDamageUnlessCost(cost string) (int, bool) {
	_, n, ok := strings.Cut(strings.TrimSpace(cost), "DamageYou<")
	if !ok || !strings.HasSuffix(n, ">") {
		return 0, false
	}
	n = strings.TrimSuffix(n, ">")
	v, err := strconv.Atoi(n)
	// N must be a POSITIVE literal: DamageYou<0> (no damage) is not an offer
	// anyone could answer differently, so it fails closed to the ordinary
	// pricing path like every other non-offer spelling.
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// sacrificePrompt renders the player-targeted sacrifice ask's prompt.
func sacrificePrompt(optional bool, n int32) string {
	if optional {
		return "Choose up to " + strconv.Itoa(int(n)) + " permanent(s) to sacrifice, or none"
	}
	return "Choose " + strconv.Itoa(int(n)) + " permanent(s) to sacrifice"
}

// sacrificeAmount resolves Amount$ (default 1). Literals pass through; a
// non-literal resolves through the count evaluator (an SVar name, an inline
// Count$ expression, or {X}). The "X" shape deserves its own arm: on a
// triggered ability Ctx.X is the ability object's own X -- zero, a trigger
// was never paid an X -- so an Amount$ X on a permanent's trigger (Meathook
// Massacre II's "each player sacrifices X creatures") must read the paid X
// off the SOURCE permanent, which CastInfo carried out of the cast onto the
// battlefield object. When that cast X is also zero but an SVar named X
// exists, the evaluator resolves it: Dralnu, Lich Lord's replacement body
// carries SVar:X:ReplaceCount$DamageAmount, naming the replaced event's own
// amount rather than any paid X. An Amount$ that is none of literal, SVar,
// Count$, Sacrificed$ or X is an unknown shape and keeps the
// pre-Amount$-reading behaviour (1) rather than degrading to zero.
func sacrificeAmount(h Host, c *Ctx, sa *cards.SA) int32 {
	raw, ok := sa.Param(cards.PKAmount)
	if !ok {
		return 1
	}
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil {
		return int32(n)
	}
	if raw == "X" {
		if c.X != 0 {
			return c.X
		}
		if o := h.Game().Obj(c.Source); o != nil && o.X != 0 {
			return o.X
		}
		if c.SVars != nil {
			// Dralnu, Lich Lord: Amount$ X with SVar:X:ReplaceCount$DamageAmount
			// — the damage-replacement body's count is the amount of the event
			// being replaced, which main's replacement machinery carries in
			// Ctx.ReplacementAmount and EvalCount's ReplaceCount$ head reads.
			if body, has := c.SVars["X"]; has {
				if v := EvalCount(h, c, body); v != 0 {
					return v
				}
			}
		}
		return 0
	}
	known := strings.HasPrefix(raw, "Count$") || strings.HasPrefix(raw, "Sacrificed$") ||
		strings.HasPrefix(raw, "TriggerCount$") || strings.HasPrefix(raw, "TriggerCountMax$")
	if !known && c.SVars != nil {
		_, known = c.SVars[raw]
	}
	v := Num(h, c, sa, "Amount", 0)
	if !known && v == 0 {
		return 1 // unknown shape: today's fixed-one behaviour, not a silent zero
	}
	return v
}

// changeZoneChosenTargets serves effChangeZone's object path the targets of a
// ValidTgts$-declared targeting when no ask has offered them yet. The ok
// return is NOT "targets were found" -- it is "use the returned set INSTEAD of
// Defined's own fallthrough": ok=true with a nil set means the ask was posed
// and SUSPENDED the resolution (the caller must return before moving
// anything), and the answered re-entry consumes Ctx.Choice here. Every other
// shape returns false and the caller keeps Defined's own behaviour
// (placement-chosen targets, Defined$-named objects, the source default).
//
// The ask never fires when the resolution already carries targets (the
// placement ask's answered set) or when the SA also carries Defined$ (an
// already-named fetch list is Forge's no-ask shape). Bounds come from
// TargetMin$/TargetMax$ through the ordinary Num grammar, clamped to the
// eligible count; Min == Max == 0 or an empty eligible set is no ask and no
// move. A host that cannot ask takes the deterministic first-max stand-in
// (R-9), which is exactly what botpolicy's clamp fallback answers with.
func changeZoneChosenTargets(h Host, c *Ctx, sa *cards.SA) ([]state.Target, bool) {
	if _, targeted := sa.Param(cards.PKValidTgts); !targeted ||
		strings.TrimSpace(sa.ParamStr(cards.PKDefined)) != "" {
		return nil, false
	}
	if c.SubPreAsk != nil {
		if ts, ok := c.SubPreAsk[sa.Line]; ok {
			return ts, true
		}
	}
	if c.TargetsOffered && (c.OfferedSA == nil || sa.Line == c.OfferedSA.Line) {
		// The announcement/placement ask offered THIS SA's targeting (rules
		// sets the marker exactly for the SA the ask covered, and OfferedSA
		// names it); the chosen-zero election must not be re-asked here. A
		// deeper sub's own targeting was never offered -- the same
		// mvts1 boundary chosenTargetsFor's OfferedSA check draws -- so it
		// falls through to its own ask below.
		return nil, false
	}
	if c.ChoiceDone {
		ans := c.Choice
		c.ChoiceDone, c.Choice = false, nil
		if TargetUniqueRequested(sa) {
			c.TargetsUnique = append(c.TargetsUnique, ans...)
		}
		return ans, true
	}
	if len(c.Targets) > 0 {
		// Inherit ONLY when the targets genuinely belong to THIS SA -- the
		// OfferedSA marker names exactly the SA the placement/announcement ask
		// covered (task spcz1; previously every sub that did not declare
		// TargetUnique$ True inherited, so a targeted root's SubAbility$
		// ChangeZone read the PARENT's targets through Defined's ValidTgts$
		// fallthrough and its own Origin$ filter rejected them into a silent
		// no-op: Cathartic Parting's and Put Away's graveyard "may shuffle"
		// clause never asked). A sub that DOES mean to reuse the parent's
		// target says so with TargetUnique$ True (Withdraw): the shared ask's
		// filter excludes the inherited parent target via TargetsAlreadyChosen,
		// so it asks for ANOTHER target instead of inheriting blindly.
		if c.OfferedSA != nil && sa.Line == c.OfferedSA.Line {
			return nil, false
		}
	}
	// Legality stays referenced to the ability controller; only the
	// decision's Player moves to the TargetingPlayer$ chooser (the same
	// resolver every rules-tier target ask uses).
	candidates := subAskCandidates(h, c, sa)
	chooser := h.ChooserFor(c, sa)
	if ch, posed := opponentPick(h, c, sa, chooser); posed {
		// The controller's which-opponent selection ask was posted: the walk
		// is suspended and re-enters this very SA, where the answered
		// selection makes ChooserFor return the chosen seat.
		return nil, true
	} else if !posed {
		chooser = ch
	}
	min := Num(h, c, sa, "TargetMin", 1)
	max := Num(h, c, sa, "TargetMax", 1)
	if max > int32(len(candidates)) {
		max = int32(len(candidates))
	}
	if min > max {
		min = max
	}
	if min < 0 {
		min = 0
	}
	if max <= 0 {
		// Nothing eligible (or an explicitly zero bound): no ask, no move.
		return noSubTargets(c, sa)
	}
	return poseTargetsAsk(h, c, sa, chooser, candidates, min, max, "choice")
}
