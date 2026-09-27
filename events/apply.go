package events

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Emit is the engine's only mutation path: append to the log, then fold into
// state. Replay calls Apply directly with logged events, so post-replay state
// equals post-play state by construction.
func Emit(g *state.Game, l *Log, e Event) Event {
	stored := l.Append(e)
	Apply(g, stored)
	return stored
}

// Apply folds one event into state. It must stay a pure function of (g, e):
// no randomness, no clock, no reads outside g.
// ResolveSVarAcrossFaces resolves an Execute$ SVar name against the source
// object's card, trying the ACTIVE face's table first and then every face in
// index order. A one-face card behaves exactly as before (the active face
// IS the first hit). The multi-face case is why this helper exists: an
// Enchantment Room's alternate-face trigger (an unlocked room's "When you
// unlock this door", CR 309.5) names an SVar that lives on Face[1]'s table,
// which src.Face() -- the active face -- does not carry. First face whose
// table defines the name wins: deterministic, and a name defined on several
// faces resolves to the lowest index consistently on live play and replay.
func ResolveSVarAcrossFaces(src *state.Object, name string) *cards.SA {
	if name == "" {
		return nil
	}
	if f := src.Face(); f != nil {
		if sa := cards.ResolveSVar(f.SVars, name); sa != nil {
			return sa
		}
	}
	if src.Card != nil {
		for _, cf := range src.Card.Faces {
			if sa := cards.ResolveSVar(cf.SVars, name); sa != nil {
				return sa
			}
		}
	}
	// CR 702.140d: a mutated permanent has all abilities of the cards beneath
	// its top card, so an under-card's Execute$ SVar resolves here too, as a
	// LAST-RESORT fall-through for the by-name siblings (DelayedPush,
	// GrantTriggerPush) when the top card's own table lacks the name. The
	// pile's under-card TRIGGERS do not use this walk any more -- they push
	// MergedTriggerPush, which resolves the name against the exact under-card
	// face, because this top-first walk would steal the body whenever the top
	// face defines the same name (Cubwarden under Everquill Phoenix).
	for i := range src.MergedCards {
		if cf := src.MergedFaceAt(i); cf != nil {
			if sa := cards.ResolveSVar(cf.SVars, name); sa != nil {
				return sa
			}
		}
	}
	return nil
}

// SVarAcrossFaces returns the RAW SVar body named on the source's card, in
// the same face order ResolveSVarAcrossFaces uses (active face first, then
// every face in index order, then recovered merged under-card faces). It is
// the raw-text sibling of ResolveSVarAcrossFaces for callers that need the
// trigger LINE (e.g. a delayed registration's Mode$ body) rather than a
// parsed sub-ability. A name defined on several faces resolves to the lowest
// index, so live play and replay agree.
func SVarAcrossFaces(src *state.Object, name string) string {
	if name == "" {
		return ""
	}
	if f := src.Face(); f != nil {
		if raw := f.SVars[name]; raw != "" {
			return raw
		}
	}
	if src.Card != nil {
		for _, cf := range src.Card.Faces {
			if raw := cf.SVars[name]; raw != "" {
				return raw
			}
		}
	}
	for i := range src.MergedCards {
		if cf := src.MergedFaceAt(i); cf != nil {
			if raw := cf.SVars[name]; raw != "" {
				return raw
			}
		}
	}
	return ""
}

func Apply(g *state.Game, e Event) {
	switch e.Kind {
	// RollDice is the proposal-only roll-action Kind (task rolldice-repl): it
	// is held out to replacement matching, never emitted, so it folds nothing
	// -- the marker shape PlanarRoll keeps.
	case GameStart, DecisionAsk, DecisionMade, Note, ModeChosen, ManaActivate, RollDice:
		// Markers. ModeChosen is a marker too: rules carries
		// its answer in a cast/trigger cache or suspended-resolution context, so
		// Apply writes nothing; the log lets replay re-derive the same branch.
		// ManaActivate is the ActivationLimit$ scan marker (see the Kind's own
		// comment): the mana itself lands through the nearby ManaAdd events.

	case Resolve:
		// The resolving object leaves the stack through its own MoveZone event,
		// so popping here would drop a second object; what the case DOES fold
		// is the per-ability resolution tally Forge's Count$ResolvedThisTurn
		// reads. e.Obj is the ability stack-object wrapper, whose Source (the
		// permanent) and Ability (the root Ability$ body, re-derived from the
		// TriggerPush/AbilityPush event) together identify "this ability". A
		// SPELL resolution carries no Ability and no tally target: every corpus
		// carrier of the head is a triggered or activated ability. Incremented
		// here, from the existing Resolve event, so a log-only replay rebuilds
		// the identical tally with no new Kind or field; TurnChange zeroes it.
		// The increment happens BEFORE the rules side builds the resolving Ctx,
		// so the count the card reads already includes its own resolution --
		// Forge's "if this is the FOURTH time" counts the current one.
		if o := g.Obj(e.Obj); o != nil && o.Ability != nil {
			if g.ResolvedThisTurn == nil {
				g.ResolvedThisTurn = make(map[string]int32)
			}
			g.ResolvedThisTurn[ResolvedAbilityKey(o.Source, o.Ability)]++
		}

	case Mutate:
		// CR 702.140d: a mutate-spell resolution merges the mutating card's
		// card into the target permanent. Obj is the surviving target, IDs[0]
		// the mutating card's object (the resolving spell), Text "top"/"under"
		// the placement choice and Amount the mutated count to add. The
		// survivor's Card/FaceIdx always describe the TOP card; every
		// under-card lands in MergedCards, top-of-pile first, its object
		// parked in ZCeased (no membership list, so no battlefield scan sees
		// it as a second permanent). Nothing here reads a map or the clock, so
		// a log-only replay rebuilds the identical pile.
		survivor := g.Obj(e.Obj)
		if len(e.IDs) == 0 || survivor == nil || survivor.Card == nil ||
			survivor.Zone != state.ZBattlefield || e.Text == "" {
			break
		}
		src := g.Obj(e.IDs[0])
		if src == nil || src.Card == nil {
			break
		}
		srcCard, srcFace := src.Card, src.FaceIdx
		if e.Text == "top" {
			// The survivor's current top card becomes an under-card. Its own
			// card needs a parked object to move to a graveyard when the pile
			// dies, and the survivor's ID must keep naming the pile, so mint
			// one for the demoted card. Snapshot before AddObject (it may
			// reallocate g.Objs).
			oldCard, oldFace, owner := survivor.Card, survivor.FaceIdx, survivor.Owner
			parked := g.AddObject(oldCard, owner)
			parked.Zone = state.ZCeased
			parkedID := parked.ID
			survivor = g.Obj(e.Obj)
			if survivor == nil {
				break
			}
			survivor.Card, survivor.FaceIdx = srcCard, srcFace
			under := state.MergedCard{Obj: parkedID, Card: oldCard, FaceIdx: oldFace}
			survivor.MergedCards = append([]state.MergedCard{under}, survivor.MergedCards...)
			// The mutating spell's object is now redundant (its card data was
			// copied onto the survivor), so park it in ZCeased without adding
			// it to the pile: moving it to a graveyard later would duplicate
			// the top card.
			Move(g, src.ID, src.Zone, state.ZCeased)
		} else {
			// The mutating card goes under the target: park its object and
			// stack it beneath the survivor's existing cards (top-of-pile
			// first, so the newest under-card goes last).
			Move(g, src.ID, src.Zone, state.ZCeased)
			survivor = g.Obj(e.Obj)
			if survivor == nil {
				break
			}
			survivor.MergedCards = append(survivor.MergedCards, state.MergedCard{Obj: e.IDs[0], Card: srcCard, FaceIdx: srcFace})
		}
		survivor.TimesMutated += e.Amount

	case PlanarRoll:
		// The planar-dice roll (CR 901.3, task rollplanar1) is a pure marker:
		// no plane deck exists in this build, so a roll folds no state — the
		// logged event is the record (Amount the post-replacement count, IDs
		// the kept results, Counter the ignored count the replacement wrote)
		// and replay re-derives the same rolls from the seeded rng.

	case Explore:
		// The explore record (CR 701.35a, task explore1) is a pure marker,
		// exactly like PlanarRoll: the explore's own state changes (the
		// revealed card's move, the +1/+1 counter on the explorer) are their
		// own MoveZone/CounterChange events that preceded this one, and the
		// record is what trig:Explores matches. Obj the explorer, Player its
		// controller, IDs[0] the revealed card, Amount 1 = land (went to
		// hand) / 0 = nonland (counter put; card back on top or graveyard).

	case Investigate:
		// The investigate record (CR 701.36a, task investtrig1) is a pure
		// marker, exactly like Explore: the investigate's own state change
		// (the Clue token mint) is its own TokenCreate event that preceded
		// this one, and the record is what trig:Investigated matches. Player
		// is the investigating seat, Obj the resolving source permanent.

	case SearchedLibrary:
		// Pure marker for one completed library search; all resulting card
		// moves and the shuffle have their own events.

	case KeywordAbilityPush:
		// A keyword-GRANTED activated ability (CR 613.1f): the mint mirrors
		// AbilityPush (Ruling T20-a) so a log-only replay creates the same
		// object a live game did, but the body is not a face index -- it is
		// SYNTHESIZED from the derived keyword line Counter carries
		// ("Cycling:1 U", "TypeCycling:Sliver:3"), exactly the synthesis the
		// offer loop and pcAbility re-derive, so live game and replay mint the
		// identical ability. Obj is the activating card and the minted
		// object's Source (`Defined$ Self`/`CARDNAME` names it); no
		// registration is consumed, a grant lives exactly as long as its
		// granting static. A line no synthesizer can model, an invalid
		// controller or a missing source mints nothing (the totality stance
		// every case here takes).
		if !validPlayer(g, e.Player) {
			break
		}
		src := g.Obj(e.Obj)
		if src == nil {
			break
		}
		sa := cards.GrantedCyclingAbility(e.Counter)
		if sa == nil {
			break
		}
		// AbilityPush's per-source activation census, same condition: only a
		// battlefield activation counts (a cycling activation is from the
		// hand, so this is the mirror rather than a live increment).
		if src.Zone == state.ZBattlefield {
			src.ActivatedThisTurn++
		}
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = sa
		o.StackKind, o.StackKindKnown = state.StackKindActivated, true
		o.Source = e.Obj
		o.Remembered = rememberedFrom(e.IDs)

	case Discover, Seek, Surveil, Scry, Proliferate:
		// The discover (CR 701.57), seek (task trigdisc1), surveil
		// (CR 701.42, task trig-surveil), scry (CR 701.18, task
		// scrybottom) and proliferate (CR 701.27, task trig-proliferate)
		// records are pure markers, exactly like Explore/Investigate: the
		// action's own state changes (the exiles/reveals, the sought card's
		// move, the KArrange answer's LibraryOrder, the counter batch per
		// chosen recipient) are their own events that surround this one, and
		// the record is what trig:Discover / trig:SeekAll / trig:Surveil /
		// trig:Scry / trig:Proliferate match. Player is the acting seat, Obj
		// the resolving source permanent; Scry's Amount is the number of
		// cards put on the bottom. One marker per completed action.

	case Exploit:
		// The exploit record (CR 702.58a, task exploit1) is a pure marker,
		// exactly like Explore/Investigate: the sacrifice's own state change
		// (the battlefield-to-graveyard MoveZone) is its own event that
		// preceded this one, and the record is what trig:Exploited matches.
		// Obj the exploiting creature, Player its controller, IDs[0] the
		// exploited creature. A declined optional sacrifice records nothing.

	case AlterAttribute:
		// The AlterAttribute fold (task alterattr1): the engine models the
		// "Suspected" (CR 702.157) and "Plotted" (CR 701.34, task kw-plot)
		// attributes. Text names the attribute so a future modelled one
		// extends this switch without an event-schema change; an unmodelled
		// name never reaches Apply (the effect emits its loud
		// unsupported-attribute Note instead of an event), so the fall
		// through to no fold is replay-safe. Amount 1 grants, -1 removes.
		if o := g.Obj(e.Obj); o != nil {
			switch e.Text {
			case "Suspected":
				o.Suspected = e.Amount >= 1
			case "Monstrous":
				// CR 701.31b's monstrous designation (Giggling Skitterspike's
				// `{5}: Monstrosity 5`, task agent-20260919T190014Z): Amount is
				// the monstrosity COUNT the resolving ability carried (the
				// BecomeMonstrous triggers' `SVar:MonstrosityX:TriggerCount$Amount`
				// reads it back), and Amount >= 1 sets the mark. CR 701.31 gives
				// the designation no controller-change end -- the only clear is
				// the Move fold's leaving-battlefield block below.
				o.Monstrous = e.Amount >= 1
			case "Renowned":
				// CR 702.112b: renowned persists only for this battlefield
				// permanent; Amount carries the Renown count for listeners.
				o.Renowned = e.Amount >= 1
			case "Suspend":
				o.SuspendGranted = e.Amount >= 1
			case "Plotted":
				// CR 701.34c: the plotted designation on an exiled card. The
				// grant stamps PlottedTurn with the CURRENT turn so the free
				// cast's "on a later turn" gate (rules/legal.go's exile walk)
				// can compare e.G.Turn against it; a removal (-1) clears it.
				// The turn is read from the game, never carried on the event,
				// exactly like Enlist's EnlistedTurn stamp -- a log-only
				// replay re-derives the same value.
				if e.Amount >= 1 {
					o.PlottedTurn = g.Turn
				} else {
					o.PlottedTurn = 0
				}
			}
		}

	case Enlist:
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
			break
		}
		o.EnlistedTurn = g.Turn
		o.EnlistedCombat = g.CombatsThisTurn

	case Connive:
		// The connive record (CR 702.59, task connive1) is a pure marker,
		// exactly like Explore: the connive's own state changes (the draws,
		// the discards, the +1/+1 counters) are their own events that
		// preceded this one, and the record is what trig:Connives matches.
		// Obj the conniving permanent, Player its controller, IDs the
		// discarded cards in discard order, Amount the nonland count among
		// them. One marker per completed connive action.

	case Pair:
		// CR 702.103: a Soulbond pairing. Obj is the pairing permanent and
		// IDs[0] its chosen partner; both fields are set reciprocally when
		// both are battlefield permanents. Neither half is written when a
		// pairing ends (the paired field is reset by each object's own
		// Move when one leaves the battlefield).
		if len(e.IDs) > 0 {
			applyPair(g, e.Obj, e.IDs[0])
		}

	case MyriadCopy:
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
		// zoneChangeMatches (Mode$ ChangesZone requires ev.Kind ==
		// events.MoveZone).
		src := g.Obj(e.Obj)
		if validPlayer(g, e.Player) && src != nil && src.Face() != nil {
			o := g.AddObject(src.Card, e.Player)
			o.FaceIdx = src.FaceIdx
			o.IsToken = true
			o.IsCopy = true
			o.IsMyriad = true
			o.Tapped = true
			o.IsAttacking = true
			if len(e.IDs) > 0 {
				o.Attacking = state.PlayerID(e.IDs[0])
			}
		}

	case MyriadCleanup:
		// CR 702.109a: every token created by Myriad is exiled at end of
		// combat. Move is called only from this event fold, so replay performs
		// the same deterministic arena-order cleanup without synthetic events.
		for i := range g.Objs {
			o := &g.Objs[i]
			if o.IsMyriad && o.Zone == state.ZBattlefield {
				Move(g, o.ID, state.ZBattlefield, state.ZExile)
			}
		}

	case TokenAttacks:
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
			o.Tapped = true
			o.IsAttacking = true
			o.Attacking = state.PlayerID(e.IDs[0])
		}

	case Shuffle:
		if validPlayer(g, e.Player) {
			g.SetZone(state.ZLibrary, e.Player, append([]state.ObjID(nil), e.IDs...))
		}

	case PlanarDeckShuffle:
		if validPlayer(g, e.Player) {
			ids := append([]state.ObjID(nil), e.IDs...)
			for _, id := range ids {
				if o := g.Obj(id); o != nil {
					o.Zone, o.FaceDown = state.ZPlanarDeck, true
				}
			}
			g.SetZone(state.ZPlanarDeck, e.Player, ids)
		}

	case PlanarReveal:
		if validPlayer(g, e.Player) {
			ids := g.Zone(state.ZPlanarDeck, e.Player)
			if len(ids) > 0 && ids[0] == e.Obj {
				if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZPlanarDeck {
					o.FaceDown = false
				}
			}
		}

	case PlanarWalk:
		if validPlayer(g, e.Player) {
			ids := g.Zone(state.ZPlanarDeck, e.Player)
			if len(ids) > 0 {
				if len(e.IDs) > 0 {
					// A Defined$ planeswalk names its destination(s): move each
					// named plane to the front (in the order named), keeping
					// every other plane's relative order. The previously-current
					// plane stays in the zone where it was, which is what
					// DontPlaneswalkAway$'s "don't planeswalk away" leaves
					// behind (Norn's Seedcore); the away trigger itself is
					// suppressed separately by the event's Amount flag.
					ids = planarWalkToOrder(ids, e.IDs)
				} else if len(ids) > 1 {
					ids = append(append([]state.ObjID(nil), ids[1:]...), ids[0])
				}
				for _, id := range ids {
					if o := g.Obj(id); o != nil && o.Zone == state.ZPlanarDeck {
						o.FaceDown = true
					}
				}
				if o := g.Obj(ids[0]); o != nil && o.Zone == state.ZPlanarDeck {
					o.FaceDown = false
				}
				g.SetZone(state.ZPlanarDeck, e.Player, ids)
			}
		}

	case ChaosEnsues:
		// The chaos-ensues marker (CR 901.9, task planar-verbs) is a pure
		// marker, exactly like PlanarRoll: no state folds. The current plane's
		// chaos ability is an ordinary triggered ability (Mode$ ChaosEnsues)
		// that rules' trigger walk queues when this marker is checked, so the
		// logged event is the record and replay re-derives the trigger queue.

	case MonarchChange:
		if validPlayer(g, e.Player) {
			g.Monarch, g.HasMonarch = e.Player, true
		}

	case BlessingChange:
		// CR 702.131: one-way designation latch.
		if validPlayer(g, e.Player) {
			g.Players[e.Player].Blessing = true
		}

	case EnduringStoryChange:
		if validPlayer(g, e.Player) {
			g.Players[e.Player].EnduringStory = true
		}

	case StartingPlayerChange:
		if validPlayer(g, e.Player) {
			g.StartingPlayer, g.HasStartingPlayer = e.Player, true
		}

	case ControlChange:
		if validPlayer(g, e.Player) {
			if o := g.Obj(e.Obj); o != nil {
				// CR 701.54b: a Ring-bearer designation ends "until another
				// player gains control of it" — the old controller is still
				// on o.Controller here, so the seat losing the designation is
				// the one naming the object that is not the new controller.
				// (The new controller gaining control of their OWN bearer is
				// not a change of controller for the designation and keeps it.)
				for i := range g.Players {
					if g.Players[i].RingBearer == o.ID && state.PlayerID(i) != e.Player {
						g.Players[i].RingBearer = 0
					}
				}
				// CR 702.157b: the suspected designation ends the moment
				// ANOTHER player gains control of the permanent -- the same
				// condition the Ring-bearer designation keys on, evaluated on
				// the still-old controller (o.Controller is still the old seat
				// here). A same-controller ControlChange (no real change of
				// controller) keeps the designation.
				if o.Controller != e.Player {
					o.Suspected = false
				}
				changeControl(g, o, e.Player)
				// An AsLongAsControl goad ends the moment its controller
				// condition fails; pruning here keeps a later return of
				// control from reviving it.
				pruneGoads(g)
			}
		}

	case Imprint:
		if o := g.Obj(e.Obj); o != nil {
			if e.Text == "clear" {
				o.Imprinted = nil
				o.ImprintTokens = nil
				o.SeekFound = nil
				o.EncodedCards = nil
			} else if e.Text == "forget" {
				// ForgetImprinted$ (Pump's Chrome Mox body): remove exactly the
				// named ids from the persistent Imprinted list, keeping the
				// rest -- a bad or partial payload degrades to a smaller
				// forget, never a wider one.
				drop := make(map[state.ObjID]bool, len(e.IDs))
				for _, id := range e.IDs {
					drop[id] = true
				}
				kept := make([]state.ObjID, 0, len(o.Imprinted))
				for _, id := range o.Imprinted {
					if !drop[id] {
						kept = append(kept, id)
					}
				}
				o.Imprinted = kept
				keptTokens := make([]state.ObjID, 0, len(o.ImprintTokens))
				for _, id := range o.ImprintTokens {
					if !drop[id] {
						keptTokens = append(keptTokens, id)
					}
				}
				o.ImprintTokens = keptTokens
				keptFound := make([]state.ObjID, 0, len(o.SeekFound))
				for _, id := range o.SeekFound {
					if !drop[id] {
						keptFound = append(keptFound, id)
					}
				}
				o.SeekFound = keptFound
				keptEncoded := make([]state.ObjID, 0, len(o.EncodedCards))
				for _, id := range o.EncodedCards {
					if !drop[id] {
						keptEncoded = append(keptEncoded, id)
					}
				}
				o.EncodedCards = keptEncoded
			} else {
				// Text is an in-kind discriminator, not a new Event field:
				// ImprintCards$ records Forge's imprintedCards list while a
				// ChangeZone-to-exile records the separate exiledCards list.
				// Both associations survive replay, but only the latter is
				// pruned when its card leaves exile (in Move below).
				// Text "until-host-leaves" records ChangeZone's Duration$
				// UntilHostLeavesPlay association on the SOURCE object: the
				// exiled cards (IDs) come back to the zone carried in Amount
				// when the source leaves the battlefield (rules sweepExileReturn).
				if e.Text == "until-host-leaves" {
					from := state.Zone(e.Amount)
					if from.Valid() {
						for _, id := range e.IDs {
							if g.Obj(id) == nil {
								continue
							}
							dupe := false
							for _, en := range o.ExileReturn {
								if en.Obj == id {
									dupe = true
									break
								}
							}
							if !dupe {
								o.ExileReturn = append(o.ExileReturn, state.ExileReturnEntry{Obj: id, From: from})
							}
						}
					}
				} else {
					list := &o.Imprinted
					if e.Text == "exiled-with" {
						list = &o.ExiledCards
					} else if e.Text == "imprint-tokens" {
						// ImprintTokens$ records the created TOKENS here, the
						// association `Defined$ Imprinted` resolves while they sit
						// on the battlefield (state.Object.ImprintTokens).
						list = &o.ImprintTokens
					} else if e.Text == "seek-found" {
						// Seek's ImprintFound$ records the cards it moved to a
						// hand here; `Defined$ Imprinted` resolves them wherever
						// they currently sit (state.Object.SeekFound), so the
						// ordinary Imprinted list's exile-only reader keeps its
						// CR 607.2a contract.
						list = &o.SeekFound
					} else if e.Text == "encoded" {
						// Cipher (CR 702.99a): the resolving spell card is exiled
						// ENCODED on this creature. state.Object.EncodedCards is
						// the creature-side link the combat-damage trigger reads;
						// it is pruned when the card leaves exile and cleared when
						// this object leaves the battlefield (both in Move), so
						// only text without that cleanup names a different list.
						list = &o.EncodedCards
					}
					for _, id := range e.IDs {
						if g.Obj(id) != nil {
							*list = append(*list, id)
						}
					}
				}
			}
		}

	case LibraryOrder:
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

	case ExtraTurn:
		// One grant or consumption of an extra turn (CR 500.7). The count and
		// the ordered pending queue are game state folded here so a log-only
		// reconstruction holds the same pending extras the live game did; the
		// turn structure's own consumption is the -1 form, emitted by rules'
		// advanceStep at the exact boundary it repeats the seat instead of
		// moving on. The queue is the ORDER the rule takes them in: grants
		// append in creation order, the -1 consumption removes the seat's LAST
		// entry (most recently created first, CR 500.7), so the total counts and
		// the queue agree by construction.
		if validPlayer(g, e.Player) && e.Amount != 0 {
			if g.ExtraTurns == nil {
				g.ExtraTurns = map[state.PlayerID]int{}
			}
			g.ExtraTurns[e.Player] += int(e.Amount)
			if g.ExtraTurns[e.Player] < 0 {
				g.ExtraTurns[e.Player] = 0
			}
			if e.Amount > 0 {
				// Amount is a number of distinct grants, not merely the
				// aggregate counter. Keep one queue entry per granted turn so
				// NumTurns$ 2 (Time Stretch) is consumed twice. Text is an
				// already encoded Event field; this canonical marker carries the
				// Forge SkipUntap$ rider without changing Event's hash-chain
				// schema.
				skipUntap := e.Text == ExtraTurnSkipUntapText
				for n := int32(0); n < e.Amount; n++ {
					g.ExtraTurnQueue = append(g.ExtraTurnQueue, state.ExtraTurn{Player: e.Player, SkipUntap: skipUntap})
				}
			} else {
				for i := len(g.ExtraTurnQueue) - 1; i >= 0; i-- {
					if g.ExtraTurnQueue[i].Player == e.Player {
						g.ExtraTurnQueue = append(g.ExtraTurnQueue[:i], g.ExtraTurnQueue[i+1:]...)
						break
					}
				}
			}
		}
		// Forge's ExtraTurnDelayedTrigger$ (Final Fortune: "At the beginning
		// of that turn's end step, you lose the game") registers the delayed
		// trigger HERE, at CONSUMPTION time, with the consumed turn's number
		// as its MinTurn -- so the ordinary Mode$ Phase delayed firing skips
		// the granting turn's own end step and fires exactly once, in the
		// granted turn. Registration must ride the consumption, not the grant:
		// with several extra turns pending (CR 500.7 takes them most recently
		// created first) the turn a grant PRODUCES is not known at grant time
		// -- it is exactly the turn about to begin when the -1 fires. Only the
		// -1 form registers; the +grant carries no Counter at consumption. A
		// consumption with no source object, no Execute$ name, or a source
		// whose face lacks the SVar degrades to no registration rather than
		// panicking (the same totality stance DelayedRegister applies).
		if e.Amount < 0 && e.Counter != "" && e.Obj != 0 && g.Obj(e.Obj) != nil {
			f := g.Obj(e.Obj).Face()
			if f != nil && cards.ResolveSVar(f.SVars, e.Counter) != nil {
				// The registered trigger's phase: IDs[0] carries the state.Step
				// the granting DelayedTrigger SA's Phase$ named (effAddTurn
				// parsed it through the shared parser and the consumption
				// forwarded it). An old log's consumption — or a forwarder
				// that degraded — carries no IDs, and the end step Final
				// Fortune's body names is the fallback every earlier
				// registration used. An out-of-range ordinal degrades too.
				phase := state.StepEnd
				if len(e.IDs) > 0 {
					if p := state.Step(e.IDs[0]); p.Valid() {
						phase = p
					}
				}
				g.Delayed = append(g.Delayed, state.DelayedTrigger{
					ID:         g.DelayedNext,
					Phase:      phase,
					Source:     e.Obj,
					Controller: e.Player,
					Execute:    e.Counter,
					MinTurn:    g.Turn + 1,
				})
				g.DelayedNext++
			}
		}

	case ExtraPhase:
		// One Forge AddPhaseEffect message (DB$ AddPhase). Three forms split
		// on Amount (the ExtraTurn precedent one level up): +1 appends one
		// queue entry per granted phase (NumPhases$ is the emitter's count),
		// -1 marks a grant consumed (the turn structure entered its extra
		// phase), -2 removes a consumed grant (the extra phase completed).
		// A malformed grant -- no IDs, an invalid step ordinal, an invalid
		// player -- is a no-op, the same totality stance as the ExtraTurn
		// case. Totality for consume/complete: an identity that matches no
		// queue entry is a no-op, never a panic.
		switch {
		case e.Amount > 0:
			if !validPlayer(g, e.Player) || len(e.IDs) == 0 {
				break
			}
			entry := state.Step(e.IDs[0])
			if !entry.Valid() {
				break
			}
			riders := DecodeExtraPhaseRiders(e.Text)
			// The extra phase's range: Entry's own by default, overridden by a
			// multi-step ExtraPhase$ grant's RANGEEND rider (a whole named
			// phase; only taken when it does not walk BACKWARD past the entry).
			rangeEnd := state.ExtraPhaseRangeEnd(entry)
			if riders.HasRangeEnd && riders.RangeEnd >= entry {
				rangeEnd = riders.RangeEnd
			}
			ep := state.ExtraPhase{
				Player:    e.Player,
				AfterStep: e.Step,
				Entry:     entry,
				RangeEnd:  rangeEnd,
				Execute:   e.Counter,
				Source:    e.Obj,
			}
			if len(e.IDs) > 1 {
				if fb := state.Step(e.IDs[1]); fb.Valid() {
					ep.HasFollowedBy, ep.FollowedBy = true, fb
				}
			}
			ep.HasDelayedPhase, ep.DelayedPhase = riders.HasDelayedPhase, riders.DelayedPhase
			ep.ValidPlayer = riders.ValidPlayer
			for n := int32(0); n < e.Amount; n++ {
				g.ExtraPhases = append(g.ExtraPhases, ep)
			}
		case e.Amount == -1:
			if i, ok := matchExtraPhase(g, e, false); ok {
				g.ExtraPhases[i].Consumed = true
			}
			// The delayed-trigger rider (Moraug's "At the beginning of that
			// combat, untap all creatures you control") registers HERE, at
			// consume time -- the ExtraTurn Final-Fortune precedent: the
			// registration must ride the consumption, because the extra phase
			// begins exactly now (the very next StepChange is its entry step,
			// which the ordinary delayed-trigger drain fires on). MinTurn is
			// the CURRENT turn -- the extra phase begins in the granting turn,
			// unlike an extra turn's Turn+1. A consume with no source object,
			// no Execute$ name, or a source whose face lacks the SVar degrades
			// to no registration rather than panicking.
			if e.Counter != "" && e.Obj != 0 && g.Obj(e.Obj) != nil {
				f := g.Obj(e.Obj).Face()
				if f != nil && cards.ResolveSVar(f.SVars, e.Counter) != nil {
					// The registered phase: the rider's DELAY= step when the
					// grant forwarded one, else the extra phase's own entry
					// step (IDs[0] -- the only step a consume always carries).
					riders := DecodeExtraPhaseRiders(e.Text)
					phase := state.Step(0)
					okPhase := false
					if riders.HasDelayedPhase {
						phase, okPhase = riders.DelayedPhase, true
					} else if len(e.IDs) > 0 {
						phase, okPhase = state.Step(e.IDs[0]), state.Step(e.IDs[0]).Valid()
					}
					if okPhase {
						dt := state.DelayedTrigger{
							ID:         g.DelayedNext,
							Phase:      phase,
							Source:     e.Obj,
							Controller: e.Player,
							Execute:    e.Counter,
							MinTurn:    g.Turn,
						}
						dt.ValidPlayer = riders.ValidPlayer
						g.Delayed = append(g.Delayed, dt)
						g.DelayedNext++
					}
				}
			}
		case e.Amount == -2:
			if i, ok := matchExtraPhase(g, e, true); ok {
				g.ExtraPhases = append(g.ExtraPhases[:i], g.ExtraPhases[i+1:]...)
			}
		}

	case DoorUnlock:
		// CR 309.5: the unlock activation paid the locked half's mana cost as
		// a sorcery. The flag is what makes the alternate face's rules text
		// live (rules' trigger/static/ability scans) and what a Mode$
		// UnlockDoor trigger matches against. Totality: an unknown object, or
		// one already unlocked, is a no-op.
		if o := g.Obj(e.Obj); o != nil && !o.Unlocked {
			o.Unlocked = true
		}

	case SpeedChange:
		// One speed increment (CR 702.163). The cap and the once-per-turn
		// gate are the EMITTER's (rules' emit-side speed check) responsibility,
		// so Apply folds the delta plainly; a negative or oversized delta is
		// still clamped to [0, 4] defensively.
		if validPlayer(g, e.Player) {
			g.Players[e.Player].Speed += e.Amount
			if g.Players[e.Player].Speed < 0 {
				g.Players[e.Player].Speed = 0
			}
			if g.Players[e.Player].Speed > 4 {
				g.Players[e.Player].Speed = 4
			}
		}

	case RingTemptsYou:
		// One "the Ring tempts you" action (CR 701.54a): the count rises by
		// one and the designated permanent becomes (or stays) this seat's
		// Ring-bearer. An impossible bearer choice (no creature controlled)
		// carries Obj 0 and still counts — CR 701.54d: the "Whenever the Ring
		// tempts you" trigger fires when the actions complete even if some
		// were impossible.
		if validPlayer(g, e.Player) {
			g.Players[e.Player].RingTempted++
			g.Players[e.Player].RingBearer = e.Obj
		}

	case RingEmblemPush:
		// One of the Ring emblem's four level abilities (CR 701.54c) being
		// put on the stack. The ability is minted HERE, inside Apply, so a
		// log-only replay creates the exact same object a live game did
		// (Ruling T20-a, the KeywordTriggerPush precedent): the emblem has
		// no object in any zone, so TriggerPush's face-index derivation
		// cannot carry it and the "__ring:<level>" payload rebuilds the
		// ability structurally from the event text alone.
		if !validPlayer(g, e.Player) {
			break
		}
		level := int(e.Amount)
		if rest, ok := strings.CutPrefix(e.Counter, "__ring:"); ok {
			if n, err := strconv.Atoi(rest); err == nil {
				level = n
			}
		}
		sa := ringEmblemAbility(level)
		if sa == nil {
			break
		}
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = sa
		o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
		// The emblem is not an object, so there is no Source to carry: the
		// hand-built bodies read only their controller (Defined$ You /
		// Opponent) and the live Ring-bearer designation
		// (Card.IsRingbearer+YouCtrl). A zero Source makes
		// findTriggerForAbility false, so no intervening-if recheck and no
		// OptionalDecider read runs on it -- exactly the mandatory shape
		// CR 701.54c's "whenever" abilities are.
		o.Source = 0
		o.Remembered = rememberedFrom(e.IDs)

	case MoveZone, Draw, PutOnStack:
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
		if o := g.Obj(e.Obj); o != nil {
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
		sacrificed := IsSacrifice(e)
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
				o.Monstrous = false
				o.Renowned = false
				o.PlottedTurn = 0
			}
		}

	case LifeChange:
		if validPlayer(g, e.Player) {
			g.Players[e.Player].Life += e.Amount
		}

	case Damage:
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

	case Tap:
		if o := g.Obj(e.Obj); o != nil {
			o.Tapped = true
		}
	case Untap:
		if o := g.Obj(e.Obj); o != nil {
			o.Tapped = false
		}

	case StepChange:
		g.Step = e.Step
		// The per-turn combat-phase count (CR 500.6: a turn has exactly one
		// combat phase -- except the additional ones an api:AddPhase grant
		// splices in): one increment per BeginCombat ENTRY, so an extra combat
		// counts a second time and ConditionFirstCombat$ (Raiyuu's "if it's the
		// first combat phase of the turn" gate, effects/conditions.go) reads
		// the real ordinal. TurnChange resets it below.
		if e.Step.Valid() && e.Step == state.StepBeginCombat {
			g.CombatsThisTurn++
			// ChoiceRestriction$ YourLastCombat: preserve only picks from
			// each controlled permanent's immediately preceding combat, then
			// rotate its combat identity. The log is the forbidden set during
			// the combat just begun; an intervening pickless combat therefore
			// releases an older pick on the following combat.
			for i := range g.Objs {
				o := &g.Objs[i]
				if o.Zone != state.ZBattlefield || o.Controller != g.Active {
					continue
				}
				kept := o.ModeChoices[:0]
				for _, pick := range o.ModeChoices {
					if pick.Scope != state.ModeScopeYourLastCombat ||
						(pick.Turn == o.CurCombatTurn && pick.Combat == o.CurCombatCombat) {
						kept = append(kept, pick)
					}
				}
				o.ModeChoices = kept
				o.CurCombatTurn, o.CurCombatCombat = g.Turn, g.CombatsThisTurn
			}
		}
		// kw:Echo's provenance (CR 702.35a): the Draw step's beginning means
		// this turn's upkeep just ended, so the turn's upkeep is now the
		// controller's "most recent upkeep". Recording here (not at the
		// Upkeep StepChange) is what makes the gate's comparison read the
		// PREVIOUS upkeep at the next upkeep's beginning: the echo trigger
		// fires on the same StepChange that would otherwise have just
		// overwritten the record, and an acquisition made during that upkeep
		// itself still counts via its AcqStep >= StepUpkeep half. Zero stays
		// zero until a seat's first draw step (their first upkeep then reads
		// as absent — gate vacuously true).
		if e.Step == state.StepDraw && validPlayer(g, g.Active) {
			g.Players[g.Active].LastUpkeepTurn = g.Turn
		}

	case TurnChange:
		if validPlayer(g, e.Player) {
			g.Turn = e.Amount
			g.Active = e.Player
			g.Players[e.Player].LandsPlayed = 0
			// g.Zone(ZBattlefield, e.Player) can only ever hold IDs that Move
			// already confirmed are real objects, so this nil check is
			// currently unreachable in practice -- but it is one line, it
			// matches every other zone-walk in this switch (DeclareAttackers,
			// DeclareBlockers) that guards g.Obj before dereferencing, and it
			// stops that invariant from becoming a silent, easy-to-reopen
			// panic if a future Kind ever populates a zone list some other
			// way. Found in the same audit as Ruling T20-e.
			for _, id := range g.Zone(state.ZBattlefield, e.Player) {
				if o := g.Obj(id); o != nil {
					o.SummonSick = false
				}
			}
			// TurnChange is the existing per-turn reset boundary. Zone-entry
			// provenance and damage history are object facts rather than facts
			// of the incoming active player, so reset every arena object here.
			for i := range g.Objs {
				g.Objs[i].EnteredThisTurn = false
				g.Objs[i].WasDealtDamageThisTurn = false
				g.Objs[i].ActivatedThisTurn = 0
				g.Objs[i].AttacksThisTurn = 0
				// CR 702.100a: exerted is a per-turn fact. ExertSkipUntap is
				// deliberately NOT reset here -- its window spans the turn
				// boundary and is consumed at the next untap step instead.
				g.Objs[i].ExertedThisTurn = false
				// An untap election belongs to one controller's untap
				// step; the next turn gets a fresh election.
				g.Objs[i].UntapChoice = ""
				// CR 702.160: enlist is a per-combat fact; the stamp is cleared at
				// the turn boundary (a same-turn second combat compares its own
				// CombatsThisTurn against the stamp, so it needs no separate
				// reset).
				g.Objs[i].EnlistedTurn = 0
				g.Objs[i].EnlistedCombat = 0
				// Only default-duration goads expire at the goader's next turn.
				g.Objs[i].Goads = expireTurnGoads(g.Objs[i].Goads, e.Player)
				// ChoiceRestriction$ ThisTurn is the only per-turn scope. Keep
				// ThisGame for the battlefield stint and YourLastCombat across
				// turns; its combat-start rotation expires it at the right boundary.
				keptModes := g.Objs[i].ModeChoices[:0]
				for _, pick := range g.Objs[i].ModeChoices {
					if pick.Scope != state.ModeScopeThisTurn {
						keptModes = append(keptModes, pick)
					}
				}
				g.Objs[i].ModeChoices = keptModes
			}
			// The per-add entry list is per-turn state too.
			g.Entered = nil
			// Extra phases never survive into the next turn: whatever is still
			// queued (or mid-extra-phase) at the turn boundary is dropped here,
			// so a grant whose splice point this turn has already passed is
			// silently spent at the boundary rather than firing next turn. The
			// per-turn combat-phase count resets with them.
			g.ExtraPhases = nil
			g.CombatsThisTurn = 0
			// The per-ability resolution tally is a per-turn fact (CR 608.2m
			// counts resolutions in the turn), so it is dropped at the turn
			// boundary exactly as CombatsThisTurn is. Clearing (rather than
			// zeroing entries) keeps the map empty for the overwhelmingly
			// common game that never resolves a Count$ResolvedThisTurn carrier.
			g.ResolvedThisTurn = nil
			// Snapshot the monarch designation as the NEW turn begins, for the
			// trig:BecomeMonarch BeginTurn$ intervening-if ("if you were the
			// monarch as the turn began"). Folding it here, from state
			// MonarchChange already established, keeps the read replay-exact
			// without a new event or event field; a game with no monarch ever
			// set carries the false presence bit and the condition fails
			// closed.
			g.TurnStartMonarch, g.HasTurnStartMonarch = g.Monarch, g.HasMonarch
		}
		// PersistentMana$ True mana expires at the end of the turn it was
		// produced in (the carriers' "until end of turn" bound), whichever
		// seat holds it — so every seat's tally drops here and the units
		// become ordinary pool mana again, emptied by the next boundary's
		// ManaClear exactly like mana that was never persistent. The demotion
		// takes the persistent RESTRICTION batches with it: ManaClear keeps a
		// persistent batch unconditionally, so a batch left here after the
		// tally's zero would outlive its units as a phantom whose Amount
		// manaAvailableFor subtracts from every non-matching payment — hiding
		// the seat's real mana behind units that no longer exist. The printed
		// restriction (Klauth's "Spend this mana only to cast spells") is not
		// time-bounded — only the don't-lose clause is — so the batch is
		// DEMOTED to ordinary, not dropped: its units stay restricted until
		// the next boundary's ManaClear empties them with the batch.
		for i := range g.Players {
			g.Players[i].PersistentMana = state.Mana{}
			for j := range g.Players[i].RestrictedMana {
				g.Players[i].RestrictedMana[j].Persistent = false
			}
		}

	case Goad:
		if o := g.Obj(e.Obj); o != nil {
			if e.Amount == -1 {
				o.Goads = nil
				break
			}
			if !validPlayer(g, e.Player) {
				break
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
			pruneGoads(g)
		}

	case PlayerCounterChange:
		if validPlayer(g, e.Player) {
			g.Players[e.Player].AddCounter(e.Counter, e.Amount)
		}

	case Priority:
		if validPlayer(g, e.Player) {
			g.Priority = e.Player
			passes := e.Amount
			if passes < 0 {
				passes = 0
			}
			g.Passes = passes
		}

	case ManaAdd:
		if validPlayer(g, e.Player) {
			player := &g.Players[e.Player]
			// PersistentMana$ True (task persistentmana): the suffix rides Text
			// after every other encoding, so cut it before the restriction
			// parse and mark the tally/batch below.
			rest, persistent := cutManaPersistent(e.Text)
			// One event moves the pool and its parallel producer tally, so a
			// tally can never drift from the pool it partitions. Three counter
			// forms exist, and all three land in the colour's pool slot:
			//
			//   "S<colour>"       -- a SNOW mana unit (CR 107.4h), tallied in
			//                        Player.Snow so a {S} pip can be paid only
			//                        from it. The historical two-char form stays
			//                        first and exact: recorded games carry it.
			//   "<Tag><colour>"   -- a TYPED mana unit (task castfilter2),
			//                        tallied in Player.TypedMana[tag] so the
			//                        filtered Count$CastTotalManaSpent
			//                        Treasure/Cave/Desert heads can read how much
			//                        of a cast's spend came from a producer of
			//                        that type.
			//   a bare WUBRGC letter (or the empty default) -- plain pool mana.
			idx := state.MC
			if len(e.Counter) == 2 && e.Counter[0] == 'S' {
				idx = state.ManaIndex(e.Counter[1])
			} else if _, slot, ok := state.TypedManaCounter(e.Counter); ok {
				idx = slot
			} else if e.Counter != "" {
				idx = state.ManaIndex(e.Counter[0])
			}
			player.Pool[idx] += e.Amount
			if e.Amount > 0 && persistent {
				player.PersistentMana[idx] += e.Amount
			}
			if len(e.Counter) == 2 && e.Counter[0] == 'S' {
				player.Snow[idx] += e.Amount
			} else if tag, slot, ok := state.TypedManaCounter(e.Counter); ok {
				base, artifact := state.ManaUnitTypes(tag)
				player.TypedMana[base][slot] += e.Amount
				if artifact && base != state.TypedArtifact {
					player.ArtifactTyped[base][slot] += e.Amount
				}
			}
			// The RestrictValid$/AddsNoCounter$ provenance is registered for
			// EVERY counter form, never only a plain one: a tagged restricted
			// unit (Echoing Cavern's Cave mana, Sunken Citadel's, Bucolic
			// Ranch's) keeps its restriction exactly like a plain one, and the
			// matching spend event (which carries r.Color verbatim) consumes
			// it here. Registering before the pool write would be equivalent
			// for the ADD path; the consume path needs the batch list, which
			// this block owns.
			if valid, srcID, cond, addsCounters, restricted := ManaRestrictionFromText(rest); restricted {
				if e.Amount > 0 {
					player.RestrictedMana = append(player.RestrictedMana, state.ManaRestriction{
						Color: e.Counter, Amount: e.Amount, Valid: valid, Source: srcID,
						NoCounter: cond, Persistent: persistent, AddsCounters: addsCounters,
					})
				} else if e.Amount < 0 {
					// A restricted spend event names exactly the restriction batch it
					// consumes. Walk insertion order so two matching additions replay
					// identically, and tolerate a malformed historical event that
					// over-spends its batch without making Pool negative here.
					//
					// The consumed batches' own Persistent flags move the persistent
					// tally: the payment path carved a MATCHING batch, so the units
					// this spend took are the batch's, and the flag — not raw slot
					// arithmetic — is what keeps the tally on the units that
					// actually survived. The " pm" marker is meaningless on a
					// restricted spend; the batch flags are authoritative.
					need := -e.Amount
					perUsed := int32(0)
					for i := 0; i < len(player.RestrictedMana) && need > 0; {
						r := &player.RestrictedMana[i]
						if r.Color != e.Counter || r.Valid != valid {
							i++
							continue
						}
						used := r.Amount
						if used > need {
							used = need
						}
						r.Amount -= used
						need -= used
						if r.Persistent {
							perUsed += used
						}
						if r.Amount == 0 {
							player.RestrictedMana = append(player.RestrictedMana[:i], player.RestrictedMana[i+1:]...)
							continue
						}
						i++
					}
					if perUsed > 0 {
						d := perUsed
						if player.PersistentMana[idx] < d {
							d = player.PersistentMana[idx]
						}
						player.PersistentMana[idx] -= d
					}
				}
			} else if e.Amount < 0 && persistent {
				// A marked PLAIN spend event names the persistent share the
				// payment consumed: the payment path (payManaForSpent) attributes
				// the slot's units ordinary-first over the VISIBLE pool and marks
				// the persistent remainder with this suffix, so the tally follows
				// the units that actually survived instead of raw slot arithmetic.
				// (The old fresh-rule — decrement past Pool minus PersistentMana —
				// misattributed whenever a payment's visible pool differed from
				// the raw slot: a hidden restricted batch, or a carve that consumed
				// the persistent batch first, made ordinary mana wrongly survive a
				// step boundary. An unmarked negative event consumes ordinary
				// units only, by the same attribution convention.)
				d := -e.Amount
				if player.PersistentMana[idx] < d {
					d = player.PersistentMana[idx]
				}
				player.PersistentMana[idx] -= d
			}
		}

	case ManaClear:
		if validPlayer(g, e.Player) {
			// PersistentMana$ True units survive the boundary (CR 500.4 with the
			// producing card's exception) — only the slot's ordinary share
			// empties, and the tag tallies are drained alongside it so they
			// never exceed the shrunken pool. Persistent RESTRICTION batches
			// survive too; the ordinary ones empty with the pool.
			//
			// The Text keep letters (stat:UnspentMana, rules/turn.go's
			// unspentManaKeep) protect a slot whole: the static's "don't lose
			// unspent mana as steps and phases end" keeps the slot's ordinary
			// share AND its restriction batches of that colour (the restriction
			// provenance is not time-bounded; only the emptying is). "" keeps
			// nothing — the historical shape every game without a live carrier
			// emits — so old logs replay byte-identically.
			keep := manaClearKeepSlots(e.Text)
			player := &g.Players[e.Player]
			for i := range player.Pool {
				if keep[i] {
					continue
				}
				if clear := player.Pool[i] - player.PersistentMana[i]; clear > 0 {
					clearNonPersistent(player, i, clear)
				}
			}
			kept := player.RestrictedMana[:0]
			for _, r := range player.RestrictedMana {
				// state.ManaSlot is the ONE full-counter decoder (the payment
				// paths in rules/stack.go use it): a restricted batch stores its
				// producing ManaAdd.Counter verbatim, so a tagged red batch
				// ("SR" snow red, "TreasureR") read through ManaIndex(c[0])
				// would decode the tag letter as colourless and silently drop
				// the protected colour's spend restriction at the very boundary
				// the keep exists for. An empty Color batch (the unrestricted
				// AddsNoCounter provenance shape) decodes to the C slot, so a
				// keep that protects the C slot keeps it, slot-whole, like the
				// ordinary share above.
				if r.Persistent || keep[state.ManaSlot(r.Color)] {
					kept = append(kept, r)
				}
			}
			player.RestrictedMana = kept
		}

	case CounterChange:
		if e.Text != EntryCounterNotice {
			if o := g.Obj(e.Obj); o != nil {
				o.AddCounter(e.Counter, e.Amount)
			}
		}

	case DeclareAttackers:
		// e.Player names the attacking player for every ID in this event, so
		// it is validated once, like TurnChange/Priority above, rather than
		// per object. Nothing reads Object.Attacking yet, so an unvalidated
		// value cannot panic today -- but it is the same untrusted-seat-id
		// pattern as Ruling T20-e, found in the same audit, and closing it
		// now costs nothing for a well-formed event (Player is always valid
		// there).
		if !validPlayer(g, e.Player) {
			break
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

	case DeclareBlockers:
		for _, pr := range e.Pairs {
			a := g.Obj(pr[0])
			if a == nil || g.Obj(pr[1]) == nil {
				continue
			}
			a.BlockedBy = append(a.BlockedBy, pr[1])
		}

	case CombatRetarget:
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

	case PlayerLost:
		if validPlayer(g, e.Player) {
			g.Players[e.Player].Lost = true
		}

	case GameOver:
		// Ruling T22-g (fix round 1): the first GameOver wins; a later one
		// on an already-finished game is a no-op. Without this guard, a log
		// carrying two GameOver events (a duplicate, a replay quirk, a
		// tampered log) produced a game simultaneously won (Winner set by
		// the first) and drawn (Draw set by the second) -- Over alone
		// cannot express "this event doesn't apply", so the guard has to
		// live here, ahead of everything else in this case.
		if g.Over {
			break
		}
		// Ruling T22-a (Amount is the draw/winner discriminator, the same
		// trick TargetsChosen already uses to tell its two target shapes
		// apart), pinned down fully by T22-g and, for Amount == 0's own
		// Player validity, T22-l: exactly two shapes are defined, and
		// everything else is not a GameOver this build recognizes at all.
		//   Amount == 0: a win, but ONLY when Player validates. Ruling
		//     T22-l (fix round 2): a fix-round-1 build of this let an
		//     invalid Player under Amount 0 still set Over, with Winner
		//     left at its untouched zero value -- an invalid seat silently
		//     reading as seat 0 winning, the exact defect class this whole
		//     discriminator exists to remove. For an untrusted log,
		//     refusing to end the game on a malformed win claim is the safe
		//     response: it is detectable (Over stays false) rather than
		//     manufacturing a draw the log never actually carried.
		//   Amount == 1: CR 104.4a's draw. Player is irrelevant; Winner is
		//     never touched and Draw says so explicitly, since PlayerID(0)
		//     is both Winner's zero value and a real seat and so cannot
		//     mean "nobody" on its own.
		//   anything else (including Amount == 0 with an invalid Player):
		//     not a shape this Kind defines. Previously any other Amount
		//     still set Over unconditionally and, since Winner's own guard
		//     only ever checked validPlayer, a tampered event naming an
		//     out-of-range Player under some third Amount read as "seat 0
		//     won" regardless. Changing nothing at all is the safe response
		//     to a shape this build cannot interpret.
		switch {
		case e.Amount == 0 && validPlayer(g, e.Player):
			g.Over = true
			g.Winner = e.Player
		case e.Amount == 1:
			g.Over = true
			g.Draw = true
		}

	case LandPlayed:
		if validPlayer(g, e.Player) {
			g.Players[e.Player].LandsPlayed++
		}

	case TargetsChosen:
		// Amount discriminates the target shape (Ruling T14-b's own
		// discriminator, extended by Task 4 with two more shapes that
		// APPEND rather than replace -- a spell can gain a second target
		// from a later effect without losing the first):
		//   0 (default): replace with object targets, read from IDs.
		//   1: replace with a single player target, read from Player.
		//   2: append one object target per entry in IDs.
		//   3: append a single player target, read from Player.
		if o := g.Obj(e.Obj); o != nil {
			// CR 707.10c: recording chosen targets on a COPY consumes its
			// one-shot MayChooseTarget$ election. The only TargetsChosen a copy
			// can receive is the copy-target ask's own answer (a copy is minted
			// after its original was cast, so no cast-flow target records onto
			// it), so this clear cannot swallow an unrelated choice; replay
			// re-runs the same fold.
			if o.IsCopy {
				o.CopyMayChooseTarget = false
			}
			switch e.Amount {
			case 1:
				if validPlayer(g, e.Player) {
					o.Targets = []state.Target{{Player: e.Player, IsPlayer: true}}
				}
			case 2:
				for _, id := range e.IDs {
					o.Targets = append(o.Targets, state.Target{Obj: id})
				}
			case 3:
				if validPlayer(g, e.Player) {
					o.Targets = append(o.Targets, state.Target{Player: e.Player, IsPlayer: true})
				}
			default:
				var targets []state.Target
				for _, id := range e.IDs {
					targets = append(targets, state.Target{Obj: id})
				}
				o.Targets = targets
			}
		}

	case FlipFace, Specialize:
		if o := g.Obj(e.Obj); o != nil && o.Card != nil &&
			e.Amount >= 0 && int(e.Amount) < len(o.Card.Faces) {
			o.FaceIdx = uint8(e.Amount)
		}

	case TurnFaceDown:
		if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZBattlefield && !o.FaceDown {
			setType, power, toughness, hasPT, _ := FaceDownEntryFields(e.Counter)
			o.FaceDown = true
			o.FaceDownSetType = setType
			o.FaceDownPower, o.FaceDownToughness = power, toughness
			o.FaceDownHasPT = hasPT
		}

	case TurnFaceUp:
		// CR 708.6: turning a face-down permanent face up reveals the face it
		// already had -- no FaceIdx change -- and retires the CR 708.5
		// face-down characteristic set (the folded FaceDownSetType/Power/
		// Toughness payload). Gated on the battlefield, the same way
		// faceDownEffective reads it: a marker stranded on a card that left
		// the battlefield is not a turn-up.
		if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZBattlefield && o.FaceDown {
			o.FaceDown = false
			o.FaceDownSetType = ""
			o.FaceDownPower = 0
			o.FaceDownToughness = 0
			o.FaceDownHasPT = false
		}

	case PhaseOut:
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

	case ClockTick:
		g.Clock++

	case TriggerPush:
		// Ruling T20-a: the ability object is minted here, inside Apply, so
		// a log-only replay creates the exact same object a live game did --
		// not via a direct, unlogged AddObject call from rules.Engine. e.Obj
		// names the permanent whose trigger fired; a permanent that no
		// longer exists, or an out-of-range trigger index (stale data, a
		// tampered log), degrades to a no-op rather than panicking.
		//
		// Ruling T20-e: Player must be checked too, same as every sibling
		// case in this switch that indexes a Player-keyed slice (LifeChange,
		// TurnChange, Priority, ManaAdd, ManaClear). This case skipped it,
		// and an out-of-range Player flows straight into g.AddObject below,
		// then into Move's zoneOwner/SetZone/zoneIndex path, which indexes
		// g.zones (sized numZones*len(g.Players) at NewGame) with no bounds
		// check of its own -- panic: index out of range. Unreachable via
		// ordinary self-play (Player is always sourced from a real object's
		// controller) but directly reachable replaying an external,
		// corrupted, or tampered log -- exactly the case this event exists
		// to support.
		if !validPlayer(g, e.Player) {
			break
		}
		src := g.Obj(e.Obj)
		if src == nil {
			break
		}
		f := src.Face()
		if f == nil || e.Amount < -1 || int(e.Amount) >= len(f.Triggers) {
			break
		}
		o := g.AddObject(nil, e.Player)
		// Move first (it resets Remembered, among other stack-only fields,
		// for anything other than a battlefield destination -- see Move's
		// own default case below), then set what the ability actually
		// carries. Setting these before Move would have them wiped by that
		// same reset; ordering them after is what makes Remembered actually
		// survive onto the stack (Ruling T20-c).
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		if e.Amount == -1 {
			// A layer-granted Dethrone has no printed Trigger index. The
			// matcher already established its condition; this logged sentinel
			// carries the fixed keyword body through replay.
			o.Ability = &cards.SA{Kind: "DB", API: "PutCounter", Params: map[string]string{
				"Defined": "Self", "CounterType": "P1P1", "CounterNum": "1",
			}}
		} else {
			o.Ability = f.Triggers[e.Amount].Effect
		}
		o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
		o.Source = e.Obj
		// FL-41: an id in IDs is either a real object (the ordinary case)
		// or a player reference (state.PlayerRef, rules.pushTrigger) --
		// triggerRemembered's DeclareAttackers case appends the defending
		// player to Remembered, and IDs has no field of its own for a bare
		// PlayerID, so that entry travels here encoded rather than as a bare
		// ObjID that would decode as "object 0": playersOf (effects/context.go)
		// filters that entry out, so Defined$ TriggeredDefendingPlayer would
		// resolve to nothing and the effect silently no-op.
		o.Remembered = rememberedFrom(e.IDs)

	case EndCombatReset:
		// Obj zero retains the original whole-combat reset. A nonzero Obj
		// removes only that permanent (regeneration). Keep a zero tombstone
		// in attackers' blocker lists: they remain blocked (CR 509.1h),
		// while liveBlockers ignores the removed blocker, even if it lives.
		for i := range g.Objs {
			o := &g.Objs[i]
			if e.Obj == 0 || o.ID == e.Obj {
				o.IsAttacking = false
				o.AttackingBattle = 0
				o.BlockedBy = nil
			} else {
				for j, id := range o.BlockedBy {
					if id == e.Obj {
						o.BlockedBy[j] = 0
					}
				}
			}
		}

	case CastInfo:
		if o := g.Obj(e.Obj); o != nil {
			o.CastFlags = FlagsFrom(e.Counter)
			// FlagConverged's Amount is the distinct-colour spend count (CR
			// 107.4f converge), never an X value: converge faces carrying their
			// own {X} pip (Skyrider Elf) keep the two on separate pay-time
			// CastInfo events, and the flag routes this Amount into the count
			// field instead of overwriting X.
			// FlagReplicated's Amount is the replicate payment count, never an
			// X value (measured: no K:Replicate carrier's mana value carries
			// {X}), so the flag routes the Amount into the count field instead
			// of overwriting X.
			// FlagMultikicked's Amount is CR 702.43's times-kicked count, never
			// an X value: the count rides its own TRAILING pay-time CastInfo
			// (rules/cast.go's payCast), so a multikicker carrier that pairs
			// {X} with Multikicker (Comet Storm) keeps the two on separate
			// events.
			// FlagManaSpent's Amount is the TOTAL mana actually spent to cast
			// the spell (CR 601.2h), never an X value: the count rides its own
			// TRAILING pay-time CastInfo (rules/cast.go's payCast), so a
			// carrier that pairs {X} with the read (none measured) keeps the
			// two on separate events.
			// Conspire (CR 702.78a) is a BOOL fold, not an amount: it is set
			// whenever the resolved cast's pay-time CastInfo carries
			// FlagConspired, whatever other tags ride the same event. Folded
			// OUTSIDE the exclusive switch below so a later event carrying the
			// flag (each later event accumulates all earlier flags) cannot
			// steal that event's Amount from its own routing case.
			if FlagsFrom(e.Counter)&state.FlagConspired != 0 {
				o.Conspired = true
			}
			// Offspring (CR 702.175a) is a BOOL fold as well: paid at most
			// once, so it is set whenever the pay-time CastInfo carries
			// FlagOffspringPaid, whatever other tags ride the same event
			// (the Conspired pattern).
			if FlagsFrom(e.Counter)&state.FlagOffspringPaid != 0 {
				o.OffspringPaid = true
			}
			if FlagsFrom(e.Counter)&state.FlagOptionalCostPaid != 0 {
				o.OptionalCostPaid = true
			}
			// Convoke (CR 702.66, task connive1) is an ID-LIST fold, not an
			// amount: the convoked creatures ride the pay-time CastInfo's IDs
			// whenever the flag is present, whatever other tags ride the same
			// event. Folded OUTSIDE the exclusive switch below (the Conspired
			// pattern) so a later event carrying the flag cannot steal that
			// event's Amount from its own routing case.
			if FlagsFrom(e.Counter)&state.FlagConvoked != 0 {
				o.Convoked = append([]state.ObjID(nil), e.IDs...)
			}
			// AddsCounters$ (Opal Palace and siblings) is a structured-payload
			// fold: the producing ABILITIES whose riders applied to this cast --
			// snapshotted at production, with how many of each one's mana units
			// the payment spent -- ride the pay-time CastInfo's Text payload into
			// Object.ManaAddsCounterGrants, alongside the flag that records the
			// spend. Folded OUTSIDE the exclusive switch below (the Convoked
			// pattern) so the Amount stays for its own consume arm; the
			// entry-counter plan uses the stored rider verbatim, never re-reading
			// the source's face.
			if FlagsFrom(e.Counter)&state.FlagAddsCounters != 0 {
				o.ManaAddsCounterGrants = ManaAddsCounterGrantsFromText(e.Text)
			}
			switch {
			// Conspire's Amount is a marker, never data: the bool was folded
			// above, and the flag rides a LOCAL counter at the emission site
			// (rules/cast.go's payCast never ORs FlagConspired into the
			// accumulating flags), so no later CastInfo carries it and this
			// arm's position in the newest-flag-first ordering is
			// order-independent. The arm exists to CONSUME the Amount: without
			// it the event fell through to default and wrote o.X = 1 onto every
			// conspired cast (and StackCopy propagated that onto its copies).
			case FlagsFrom(e.Counter)&state.FlagConspired != 0:
				// bool folded above; the Amount is deliberately unused
			case FlagsFrom(e.Counter)&state.FlagOffspringPaid != 0:
				// bool folded above; the Amount is deliberately unused
			case FlagsFrom(e.Counter)&state.FlagOptionalCostPaid != 0:
				// bool folded above; the Amount is deliberately unused
			case FlagsFrom(e.Counter)&state.FlagConvoked != 0:
				// the convoked id list was folded above; the Amount is
				// deliberately unused (the Conspired arm's consume shape)
			case FlagsFrom(e.Counter)&state.FlagAddsCounters != 0:
				// the rider-source id list was folded above; the Amount is
				// deliberately unused (the Conspired arm's consume shape)
			case FlagsFrom(e.Counter)&state.FlagCompleated != 0:
				o.CompleatedLifePaid = e.Amount
			case FlagsFrom(e.Counter)&state.FlagConverged != 0:
				o.ConvergeColours = e.Amount
			case FlagsFrom(e.Counter)&state.FlagReplicated != 0:
				o.ReplicateTimes = e.Amount
			case FlagsFrom(e.Counter)&state.FlagSquadPaid != 0:
				o.SquadPaid = e.Amount
			case FlagsFrom(e.Counter)&state.FlagMultikicked != 0:
				o.TimesKicked = e.Amount
			// One CastInfo per captured total, each LATER event carrying ALL
			// earlier flags (payCast's flags |= accumulation), so this switch
			// checks the NEWEST flag first -- the reverse of the emission
			// order -- or every later event would route into the first tag's
			// field: Artifact, Desert, Cave, Treasure, then Snow, then the
			// total.
			case FlagsFrom(e.Counter)&state.FlagManaArtifactSpent != 0:
				o.ManaArtifactSpent = e.Amount
			case FlagsFrom(e.Counter)&state.FlagManaDesertSpent != 0:
				o.ManaDesertSpent = e.Amount
			case FlagsFrom(e.Counter)&state.FlagManaCaveSpent != 0:
				o.ManaCaveSpent = e.Amount
			case FlagsFrom(e.Counter)&state.FlagManaTreasureSpent != 0:
				o.ManaTreasureSpent = e.Amount
			case FlagsFrom(e.Counter)&state.FlagManaSnowSpent != 0:
				o.ManaSnowSpent = e.Amount
			case FlagsFrom(e.Counter)&state.FlagManaSpent != 0:
				o.ManaSpent = e.Amount
			default:
				o.X = e.Amount
			}
		}

	case XChange:
		// A mid-resolution effect rewrote the {X} a stack object was cast or
		// activated with (DB$ ChangeX: Unbound Flourishing's doubling, Glava's
		// "the value of X becomes 5"). Amount is the new value, Obj the stack
		// object -- downstream readers (resolution's ctx.X, the ETB
		// replacement ctx, Count$xPaid) pick it up fresh, so the rewrite is
		// the only write needed.
		if o := g.Obj(e.Obj); o != nil {
			o.X = e.Amount
		}

	case GiftPromise:
		// CR 702.168: the cast-time gift election. Obj is the spell on the
		// stack, Player the promised opponent (valid only when Amount != 0),
		// Amount 1 for a promise and 0 for a decline. Folded onto the object
		// so the PromisedGift predicate, the Count$PromisedGift head and
		// Defined$ Promised read one home, and preserved across the
		// stack->battlefield move by events.Move (the X/CastFlags window).
		if o := g.Obj(e.Obj); o != nil {
			o.CastFlags &^= state.FlagPromisedGift
			o.GiftPromisedTo = 0
			if e.Amount != 0 {
				o.CastFlags |= state.FlagPromisedGift
				o.GiftPromisedTo = e.Player
			}
		}

	case GiveGift:
		// A completed gift action (CR 702.168b), matched by trig:GiveGift.
		// Like Investigate it is a pure Apply no-op marker: the gift's own
		// state change is its own preceding event, and the record exists only
		// so "whenever you give a gift" fires on a promise actually kept
		// rather than on any draw or token creation.

	case Evolved:
		// One completed evolve keyword action (CR 702.99b), matched by
		// trig:Evolved. Like GiveGift/Investigate it is a pure Apply no-op
		// marker: the +1/+1 counter placement is its own CounterChange event
		// rules emitted during the keyword ability's resolution, and this
		// record exists only so "whenever this creature evolves" fires on the
		// evolve action rather than on any unrelated counter.

	case Clash:
		// One clashing player's win/lose outcome from a completed CR 701.31
		// clash action, matched by trig:Clashed. Like GiveGift it is a pure
		// Apply no-op marker: the reveal Note and the top/bottom placements
		// are their own preceding events, and this record exists only so
		// "whenever you win/lose a clash" fires on a clash rather than on any
		// reveal. Amount carries the Won$ orientation (1 = won, 0 = lost or
		// tied), already read off the live event by clashMatches, so Apply
		// stores nothing.

	case NoteNumber:
		// A trigger's Execute$ body noted a number onto the CARD (DB$ Pump
		// NoteNumber$ <expr> -- Lupine Harbingers' exile trigger noting
		// Count$YourTurns). Amount is the value, Obj the card; Count$
		// NotedNumber reads it at the later ETB, and events.Move's
		// leave-the-battlefield reset clears it with the X/CastFlags window.
		if o := g.Obj(e.Obj); o != nil {
			o.NotedNumber = e.Amount
		}

	case StoreSVar:
		// api:StoreSVar wrote one named runtime SVar onto its source (Forge's
		// sa.setSVar: Minion of the Wastes / Phyrexian Processor's
		// `Cost$ Mandatory PayLife<X>` body storing the paid life under
		// LifePaidOnETB). Obj is the object, Text the SVar name and Amount
		// the resolved value; Object.RuntimeSVars overlays the printed face
		// table for the CDA and token reads that consume it. An empty name
		// writes nothing rather than a ghost entry, and events.Move's
		// leave-the-battlefield reset clears the table with the cast-time
		// window. The write is a keyed map insert, so map order never
		// reaches an event.
		if o := g.Obj(e.Obj); o != nil && e.Text != "" {
			if o.RuntimeSVars == nil {
				o.RuntimeSVars = make(map[string]int32)
			}
			o.RuntimeSVars[e.Text] = e.Amount
		}

	case PlayerNoted:
		// A DB$ Pump body noted a label onto a player (NoteCards$ <defined>
		// | NoteCardsFor$ <label> -- Seize the Spotlight, Master of
		// Ceremonies). Player is the seat and Text the label; the note is
		// read back by the shared player filter's `Player.NotedFor<label>`
		// qualifier. Appending is idempotent (a re-note of the same label
		// does not duplicate it) and preserves first-note order, so a
		// log-only replay rebuilds the exact slice. An empty label or an
		// out-of-range seat writes nothing rather than a ghost note.
		if e.Text == "" || int(e.Player) >= len(g.Players) {
			break
		}
		p := &g.Players[e.Player]
		seen := false
		for _, n := range p.Notes {
			if n == e.Text {
				seen = true
				break
			}
		}
		if !seen {
			p.Notes = append(p.Notes, e.Text)
		}

	case PlayerNoteCleared:
		// ClearNotedCardsFor$ removes exactly one label. Retaining the remaining
		// order makes the event fold deterministic and replay-equivalent.
		if e.Text == "" || int(e.Player) >= len(g.Players) {
			break
		}
		p := &g.Players[e.Player]
		out := p.Notes[:0]
		for _, label := range p.Notes {
			if label != e.Text {
				out = append(out, label)
			}
		}
		p.Notes = out

	case CardNoted:
		// A DB$ Pump body noted a label onto a CARD (NoteCards$ Remembered |
		// NoteCardsFor$ <label> -- Volatile Chimera, Arcane Savant, Caller of
		// the Untamed; NoteCards$ TriggeredSource -- Maelstrom Archangel
		// Avatar). Obj is the noted object and Text the label; the note is
		// read back by the shared card filter's `Card.NotedFor<label>`
		// qualifier. Appending is idempotent (a re-note of the same label does
		// not duplicate it) and preserves first-note order, so a log-only
		// replay rebuilds the exact slice. An empty label or a vanished object
		// writes nothing rather than a ghost note. A note is card-identity
		// provenance, not zone-local state -- the setup-path carriers note
		// cards sitting in exile -- so the fold never clears on a zone move.
		if e.Text == "" {
			break
		}
		noted := g.Obj(e.Obj)
		if noted == nil {
			break
		}
		seen := false
		for _, n := range noted.Notes {
			if n == e.Text {
				seen = true
				break
			}
		}
		if !seen {
			noted.Notes = append(noted.Notes, e.Text)
		}

	case Choose:
		if o := g.Obj(e.Obj); o != nil {
			switch e.Counter {
			case "name":
				o.ChosenName = e.Text
			case "type":
				o.ChosenType = e.Text
			case "color":
				o.ChosenColor = e.Text
			case "mode":
				// SetChosenMode$ on an as-enters GenericChoice: the mode
				// persists on the permanent, unlike a stack-only modal answer.
				o.ChosenModes = []string{e.Text}
			case "number":
				o.ChosenNumber = e.Amount
			case "riot":
				o.RiotChoice = e.Text
			case "untap":
				o.UntapChoice = e.Text
			case "unleash":
				o.UnleashChoice = e.Text
			case "clone":
				o.ETBCloneChoiceValid = true
				o.ETBCloneChoice = 0
				if len(e.IDs) > 0 {
					o.ETBCloneChoice = e.IDs[0]
				}
			case state.ModeChoiceCounterPrefix + state.ModeScopeThisTurn,
				state.ModeChoiceCounterPrefix + state.ModeScopeThisGame,
				state.ModeChoiceCounterPrefix + state.ModeScopeYourLastCombat:
				scope := strings.TrimPrefix(e.Counter, state.ModeChoiceCounterPrefix)
				o.ModeChoices = append(o.ModeChoices, state.ModeChoice{
					Mode: e.Text, Scope: scope,
					Turn: g.Turn, Combat: g.CombatsThisTurn,
				})
				if scope == state.ModeScopeYourLastCombat {
					// Resynchronise the object's combat identity to the clock the
					// pick was just stamped from. An object that entered the
					// battlefield (or changed controller) after this combat's
					// BeginCombat rotation was not in that loop, so its
					// CurCombat* is stale or zero; without this, a second ask in
					// the SAME combat would wrongly treat the current combat's
					// pick as an earlier combat's and withhold its mode. Setting
					// it here keeps the identity derived from the same game clock
					// the pick carries, so rotation at the next combat start still
					// retains exactly this pick.
					o.CurCombatTurn, o.CurCombatCombat = g.Turn, g.CombatsThisTurn
				}
			case "protector":
				// CR 310.10: the Siege protector chosen as this Battle
				// entered. Player carries the chosen opponent's seat.
				o.Protector = e.Player
				o.ProtectorValid = true
			case "chosen":
				o.Chosen = rememberedFrom(e.IDs)
			case "remembered":
				o.Remembered = append(o.Remembered, rememberedFrom(e.IDs)...)
			case "forget-remembered":
				// ForgetChanged$ True (Forge ChangeZoneEffect's
				// host.removeRemembered on the moved card): the named cards leave
				// the source object's persistent Remembered list. Player entries
				// and ids not named are kept, so a bad or partial payload degrades
				// to a smaller forget, never a wider one.
				drop := make(map[state.ObjID]bool, len(e.IDs))
				for _, id := range e.IDs {
					drop[id] = true
				}
				kept := make([]state.Target, 0, len(o.Remembered))
				for _, t := range o.Remembered {
					if !t.IsPlayer && drop[t.Obj] {
						continue
					}
					kept = append(kept, t)
				}
				o.Remembered = kept
			case "clear-remembered":
				o.Remembered = nil
			case "clear-chosen-card":
				// Forge's Cleanup ClearChosenCard$: the chosen-card half of the
				// object's chosen list goes; chosen players stay.
				kept := o.Chosen[:0]
				for _, t := range o.Chosen {
					if t.IsPlayer {
						kept = append(kept, t)
					}
				}
				o.Chosen = kept
			case "clear-chosen-player":
				// The chosen-PLAYER half goes; chosen cards stay.
				keptCards := o.Chosen[:0]
				for _, t := range o.Chosen {
					if !t.IsPlayer {
						keptCards = append(keptCards, t)
					}
				}
				o.Chosen = keptCards
			case "noted-mana":
				// RememberCostMana$ (Jeweled Amulet: "note the type of mana
				// spent to pay this activation cost"): the payment path's
				// negative ManaAdd events carry the spend, and this marker
				// folds the SAME colours onto the source object so the card's
				// mana ability (Produced$ Special LastNotedType) can read
				// them later. Text is the WUBRG-ordered colour letters the
				// payment spent; an empty Text degrades to a cleared note.
				o.LastNotedMana = e.Text
			}
		}

	case TokenCreate:
		if !validPlayer(g, e.Player) {
			break
		}
		def, ok := g.Tokens[e.Text]
		if !ok || def == nil {
			break
		}
		o := g.AddObject(def, e.Player)
		o.IsToken = true
		Move(g, o.ID, state.ZLibrary, state.ZBattlefield)
		applyEntryCounterPairs(o, e.Pairs)

	case CardToken:
		// A battlefield token that is a copy of the CARD object Obj names
		// (encore's "create a token copy" per opponent). Mirrors StackCopy's
		// snapshot discipline: every read from src is taken into a local
		// BEFORE AddObject, because AddObject may reallocate g.Objs and a
		// src pointer read after it would read the old backing array.
		// Totality like every case: a missing source (already ceased to
		// exist) or an invalid player mints nothing.
		if !validPlayer(g, e.Player) {
			break
		}
		src := g.Obj(e.Obj)
		if src == nil || src.Card == nil {
			break
		}
		card, faceIdx := src.Card, src.FaceIdx
		o := g.AddObject(card, e.Player)
		o.IsToken = true
		o.FaceIdx = faceIdx
		Move(g, o.ID, state.ZLibrary, state.ZBattlefield)
		applyEntryCounterPairs(o, e.Pairs)
		// Encore encodes its required defender as seat+1; zero remains the
		// ordinary CardToken shape. The current turn is folded here so replay
		// reconstructs the same one-turn attack requirement.
		if e.Amount > 0 {
			defender := state.PlayerID(e.Amount - 1)
			if validPlayer(g, defender) {
				o.EncoreAttackTurn = g.Turn
				o.EncoreAttackDefender = defender
			}
		}

	case CopyToken:
		// DB$ CopyPermanent's mint (task copyp1: Flamerush Rider, Molten
		// Echoes, the populate family). Mirrors MyriadCopy's discipline: the
		// copy is the SOURCE CARD + face snapshot taken BEFORE AddObject
		// (which may reallocate g.Objs), the token's printed characteristics
		// are the copied card's, and the entry-state riders ride the Amount
		// bitmask so a replay derives the identical object. Like MyriadCopy
		// this only MINTS (in the untracked ZLibrary state AddObject leaves
		// it in); the caller follows with a genuine MoveZone so the entry is
		// an ordinary ChangesZone-matchable event. AtEOT$ ExileCombat flags
		// IsMyriad so the existing end-of-combat cleanup -- the same fold and
		// the same rules-side emit gate Myriad tokens already use -- exiles
		// the copy with identical semantics (end of combat, battlefield
		// only). Totality like every case: a missing source or an invalid
		// player mints nothing.
		if !validPlayer(g, e.Player) {
			break
		}
		src := g.Obj(e.Obj)
		if src == nil || src.Card == nil {
			break
		}
		card, faceIdx := src.Card, src.FaceIdx
		o := g.AddObject(card, e.Player)
		o.IsToken = true
		o.IsCopy = true
		o.FaceIdx = faceIdx
		if e.Amount&CopyTokenTapped != 0 {
			o.Tapped = true
		}
		if e.Amount&CopyTokenAttacking != 0 {
			o.IsAttacking = true
			if len(e.IDs) > 0 {
				o.Attacking = state.PlayerID(e.IDs[0])
			}
		}
		if e.Amount&CopyTokenExileCombat != 0 {
			o.IsMyriad = true
		}

	case CloneStatic:
		if o := g.Obj(e.Obj); o != nil && o.CopyFace != nil {
			if statics, ok := cards.ParseStaticLines(e.Text); ok {
				face := *o.CopyFace
				face.Statics = append(append([]cards.Static(nil), face.Statics...), statics...)
				o.CopyFace = &face
			}
		}

	case ClonePermanent:
		// CR 613.1a's layer-1 copy basis (DB$ Clone, api:Clone). Obj is the
		// object that becomes the copy and IDs[0] the object copied from; an
		// empty or zero id CLEARS the basis. The synthetic face is a value
		// copy of the source's PRINTED face taken here, inside Apply, so a
		// replay derives the identical characteristics from the same event.
		// Modifier parameters (AddTypes$/SetColor$/AddKeywords$/SetPower$/
		// SetToughness$/RemoveCardTypes$/RemoveCreatureTypes$) are separate
		// layer-4/5/6/7 continuous effects the primitive registered; they are
		// deliberately NOT folded into this face, so the CR 613 layer walk
		// stays the one place exceptions settle. NewName$ rides Text and the
		// GainThisAbility$ rider rides Counter.
		o := g.Obj(e.Obj)
		if o == nil {
			break
		}
		if e.Counter != "chosen-name" && (len(e.IDs) == 0 || e.IDs[0] == 0) {
			o.CopyFace = nil
			o.CopyGainThisAbility = false
			break
		}
		var face *cards.Face
		if e.Counter == "chosen-name" {
			for _, card := range g.NameUniverse {
				if len(card.Faces) > 0 && card.Faces[0].Name == e.Text {
					face = card.Faces[0]
					break
				}
			}
		} else if src := g.Obj(e.IDs[0]); src != nil {
			face = src.Face()
		}
		if face == nil {
			break
		}
		sf := *face
		if e.Counter != "chosen-name" && e.Text != "" {
			sf.Name = e.Text
		}
		// GainThisAbility$ True: "...except it has this ability". New
		// events carry a one-based index of the resolving ability, or -- for
		// a DB$/SVar-under-trigger body whose root is a TRIGGER -- of the
		// resolving trigger (Counter "gain-this-trigger"); old events without
		// one retain their original whole-list replay semantics. The
		// original face's SVar table is still merged to retain references used
		// by the granted ability.
		if e.Counter == "gain-this-ability" || e.Counter == "gain-this-trigger" {
			if of := o.Face(); of != nil {
				if e.Counter == "gain-this-trigger" {
					// Amount is a one-based index into the become object's face
					// TRIGGERS: Forge appends exactly root.getTrigger().copy(...),
					// so the copy keeps the recurring trigger that makes a
					// recurring Copy carrier recur.
					if e.Amount > 0 && int(e.Amount) <= len(of.Triggers) {
						sf.Triggers = append(append([]cards.Trigger(nil), sf.Triggers...), of.Triggers[e.Amount-1])
					}
				} else if e.Amount > 0 && int(e.Amount) <= len(of.Abilities) {
					// Amount is a one-based index into the become object's face
					// abilities. Zero retains the legacy whole-list form for old
					// logs; new Clone effects identify the resolving ability.
					sf.Abilities = append(append([]*cards.SA(nil), sf.Abilities...), of.Abilities[e.Amount-1])
				} else if e.Amount == 0 && len(of.Abilities) > 0 {
					sf.Abilities = append(append([]*cards.SA(nil), sf.Abilities...), of.Abilities...)
				}
				if len(of.SVars) > 0 {
					merged := make(map[string]string, len(sf.SVars)+len(of.SVars))
					for k, v := range sf.SVars {
						merged[k] = v
					}
					for k, v := range of.SVars {
						merged[k] = v
					}
					sf.SVars = merged
				}
			}
			o.CopyGainThisAbility = true
		} else {
			o.CopyGainThisAbility = false
		}
		o.CopyFace = &sf

	case Exert:
		// CR 702.100's fold (task exert1). Amount >= 0 is the exert itself:
		// both lifetimes stamp here -- ExertedThisTurn (the per-turn fact the
		// notExertedThisTurn offer gate and the "as it attacks" walkers
		// read) and ExertSkipUntap (the consumed-at-use no-untap window,
		// cleared by the Amount -1 consume marker the untap-step scan emits
		// when it passes the permanent). Totality: a missing object is a
		// no-op, never a panic.
		o := g.Obj(e.Obj)
		if o == nil {
			break
		}
		if e.Amount < 0 {
			o.ExertSkipUntap = false
			break
		}
		o.ExertedThisTurn = true
		o.ExertSkipUntap = true

	case KeywordTriggerPush:
		if !validPlayer(g, e.Player) {
			break
		}
		src := g.Obj(e.Obj)
		if src == nil || src.Face() == nil {
			break
		}
		sa := cards.ResolveSVar(src.Face().SVars, e.Counter)
		conspire := false
		cipher := false
		casualty := false
		demonstrate := false
		flanking := false
		gift := e.Counter == "GiftAbility"
		melee := e.Counter == "__kwMeleeGranted"
		if sa == nil {
			// A granted Melee instance has no printed SVar. Rebuild its
			// pump from the logged marker; IDs holds one player ref per
			// opponent attacked in the triggering declaration.
			if e.Counter == "__kwMeleeGranted" {
				sa = &cards.SA{Kind: "DB", API: "Pump", Params: map[string]string{
					"Defined": "Self", "NumAtt": cards.MeleePumpCount, "NumDef": cards.MeleePumpCount}}
			}
			// A granted ward (rules.pushTrigger's __kwWard: payload) has no
			// SVar to resolve: the ability is rebuilt structurally from the
			// payload -- the same DB$ Ward | UnlessCost$ <cost> a printed
			// K:Ward's compiled trigger carries -- so the live game and the
			// replay mint identical objects from the event text alone.
			if rest, ok := strings.CutPrefix(e.Counter, "__kwWard:"); ok {
				sa = &cards.SA{Kind: "DB", API: "Ward",
					Params: map[string]string{"UnlessCost": rest, "TriggerDescription": "Ward"}}
			}
			// A granted afflict (rules.pushTrigger's __kwAfflict: payload) has
			// no SVar either: rebuilt structurally into the same
			// DB$ LoseLife | Defined$ TriggeredDefendingPlayer body the printed
			// K:Afflict expansion carries, so live game and replay mint
			// identical objects from the event text alone.
			if rest, ok := strings.CutPrefix(e.Counter, "__kwAfflict:"); ok {
				sa = &cards.SA{Kind: "DB", API: "LoseLife",
					Params: map[string]string{"Defined": "TriggeredDefendingPlayer", "LifeAmount": rest,
						"TriggerDescription": "Afflict"}}
			}
			// A granted Conspire (rules.pushTrigger's __kwConspire: payload)
			// has no SVar either: rebuilt structurally into the same
			// DB$ CopySpellAbility body the printed K:Conspire expansion
			// carries, so the live game and the replay mint identical
			// objects from the event text alone. The triggering spell rides
			// Remembered (IDs) -- Defined$ TriggeredSpellAbility reads it
			// there, exactly as the printed expansion's own TriggerPush
			// entries carry it. The trailing colon (the Ward/Afflict shape)
			// keeps the payload distinct from the "__kwConspire" SVar a
			// printed bare K:Conspire line mints.
			if _, ok := strings.CutPrefix(e.Counter, "__kwConspire:"); ok {
				sa = &cards.SA{Kind: "DB", API: "CopySpellAbility",
					Params: map[string]string{"Defined": "TriggeredSpellAbility", "Amount": "Count$Conspired",
						"MayChooseTarget": "True"}}
				conspire = ok
			}
			// A CASUALTY cast trigger (rules.pushTrigger's __kwCasualty:
			// payload) has no SVar either: rebuilt structurally into the same
			// DB$ CopySpellAbility | Defined$ TriggeredSpellAbility body the
			// printed K:Casualty expansion cannot carry (the keyword is read
			// directly by the cast flow, not expanded). The triggering spell
			// rides IDs as Remembered. The payload after the colon is the
			// StackCopyCounter grammar: the Casualty:X carrier's script riders
			// (Ob Nixilis, the Adversary) -- the copy isn't legendary and its
			// starting loyalty is the casualty amount -- which the live game
			// and the replay mint identically from the event text alone. The
			// trailing colon keeps the payload from aliasing the "__kwCasualty"
			// SVar a printed bare K:Casualty line mints.
			if rest, ok := strings.CutPrefix(e.Counter, "__kwCasualty:"); ok {
				sa = &cards.SA{Kind: "DB", API: "CopySpellAbility",
					Params: map[string]string{"Defined": "TriggeredSpellAbility", "MayChooseTarget": "True"}}
				cc := ParseStackCopyCounter(rest)
				if cc.NonLegendary {
					sa.Params["NonLegendary"] = "True"
				}
				if cc.HasLoyalty {
					sa.Params["SetLoyalty"] = strconv.Itoa(int(cc.Loyalty))
				}
				casualty = ok
			}
			// A CIPHER combat-damage trigger (rules.pushTrigger's __kwCipher:
			// payload) has no SVar either -- the association that grants it is
			// state.Object.EncodedCards -- so it is rebuilt structurally into
			// the same body the printed expansion cannot carry: a free cast of
			// a COPY of the encoded card (CR 702.99b). The encoded card rides
			// IDs as Remembered, which Defined$ Remembered resolves; CopyCard$
			// True makes that a copy on the stack while the exiled original
			// stays put, and Optional$ True makes the cast the "may". The
			// trailing colon (the Ward/Afflict/Conspire shape) keeps the
			// payload distinct from the "__kwCipher" SVar a printed bare
			// K:Cipher line mints.
			if _, ok := strings.CutPrefix(e.Counter, "__kwCipher:"); ok {
				sa = &cards.SA{Kind: "DB", API: "Play",
					Params: map[string]string{"Defined": "Remembered", "ValidZone": "Exile",
						"ValidSA": "Spell", "CopyCard": "True", "CipherCopy": "True", "WithoutManaCost": "True",
						"Optional": "True", "TriggerDescription": "Cipher"}}
				cipher = true
			}
			// A granted Demonstrate (rules.pushTrigger's __kwDemonstrate:
			// payload) has no SVar either: rebuilt structurally into the same
			// DB$ Demonstrate body the printed K:Demonstrate expansion
			// carries (cards/kw_demonstrate.go), so the live game and the
			// replay mint identical objects from the event text alone. The
			// may-copy election and the opponent choice are the body's own
			// asks (effects/demonstrate.go); the triggering spell rides
			// Remembered (IDs) -- Defined$ TriggeredSpellAbility reads it
			// there, exactly as the printed expansion's own TriggerPush
			// entries carry it. The trailing colon (the Conspire shape)
			// keeps the payload distinct from the "__kwDemonstrate" SVar a
			// printed bare K:Demonstrate line mints.
			if _, ok := strings.CutPrefix(e.Counter, "__kwDemonstrate:"); ok {
				sa = &cards.SA{Kind: "DB", API: "Demonstrate",
					Params: map[string]string{"Defined": "TriggeredSpellAbility"}}
				demonstrate = ok
			}
			// A cascade trigger (rules.pushTrigger's __kwCascade: payload) has
			// no SVar either: rebuilt structurally into the DB$ Cascade body
			// both a printed K:Cascade line and every layer-6 AddKeyword$
			// Cascade grant share, so the live game and the replay mint
			// identical objects from the event text alone. The trigger's
			// Source (the cast spell) is what the effect reads its mana value
			// off at resolution (CR 702.85a's "costs less" comparison). The
			// trailing colon keeps the payload from aliasing a printed bare
			// K:Cascade line's "__kwCascade" SVar.
			if _, ok := strings.CutPrefix(e.Counter, "__kwCascade:"); ok {
				sa = &cards.SA{Kind: "DB", API: "Cascade",
					Params: map[string]string{"TriggerDescription": "Cascade"}}
			}
			// A granted flanking (rules.pushTrigger's __kwFlanking: payload) has
			// no SVar either: rebuilt structurally into the same
			// DB$ Pump | Defined$ TriggeredBlockerLKICopy | NumAtt$ -1 | NumDef$
			// -1 body the printed K:Flanking expansion carries
			// (cards/kw_flanking.go), so the live game and the replay mint
			// identical objects from the event text alone. The blocked creature
			// rides Remembered (IDs), exactly as the printed expansion's own
			// TriggerPush entries carry it. The trailing colon keeps the payload
			// from aliasing the "__kwFlanking" SVar a printed bare K:Flanking
			// line mints.
			if _, ok := strings.CutPrefix(e.Counter, "__kwFlanking:"); ok {
				sa = &cards.SA{Kind: "DB", API: "Pump",
					Params: map[string]string{"Defined": "TriggeredBlockerLKICopy", "NumAtt": "-1", "NumDef": "-1"}}
				flanking = ok
			}
			// A granted cumulative upkeep (rules.pushTrigger's
			// __kwCumulativeUpkeepGranted:<cost> payload) has no SVar either:
			// rebuilt structurally into the same DB$ CumulativeUpkeep |
			// Cost$ <cost> ability the printed K:Cumulative upkeep expansion
			// carries (cards/kw_cumulativeupkeep.go), so the live game and the
			// replay mint identical objects from the event text alone. The
			// "Granted" suffix and the trailing colon keep the payload from
			// aliasing the "__kw<keyword-line>" SVar a printed bare
			// K:Cumulative upkeep line mints (the Exploit/Offspring rule).
			// The cost rides Params["Cost"], which rules' startCumulativeUpkeep
			// reads at resolution.
			if rest, ok := strings.CutPrefix(e.Counter, "__kwCumulativeUpkeepGranted:"); ok {
				sa = &cards.SA{Kind: "DB", API: "CumulativeUpkeep",
					Params: map[string]string{"Cost": rest, "TriggerDescription": "Cumulative upkeep"}}
			}
			// A granted Exploit (rules.pushTrigger's __kwExploitGranted
			// payload) has no SVar either: rebuilt structurally into the same
			// DB$ Sacrifice | Optional$ True | SacValid$ Creature |
			// RememberSacrificed$ True -> DB$ Exploit chain the printed
			// K:Exploit expansion carries (cards/kw_exploit.go), so the live
			// game and the replay mint identical objects from the event text
			// alone. The Exploit body reads the sacrificed creature off
			// Ctx.Sacrificed, exactly as the printed chain does. The payload
			// is deliberately NOT the bare "__kwExploit": addKeywordTrigger
			// mints a printed bare K:Exploit line's SVar as "__kw"+line =
			// "__kwExploit", so the old spelling aliased a real printed-face
			// SVar. The SVar lookup above wins today, but a future caller
			// that pushed the bare payload for a face defining that SVar
			// would silently take the SVar path; "Granted" cannot collide
			// with any "__kw"+<keyword-line> mint.
			if _, ok := strings.CutPrefix(e.Counter, "__kwExploitGranted"); ok {
				sac := &cards.SA{Kind: "DB", API: "Sacrifice",
					Params: map[string]string{"Defined": "You", "Optional": "True", "SacValid": "Creature",
						"RememberSacrificed": "True"}}
				sac.Sub = &cards.SA{Kind: "DB", API: "Exploit",
					Params: map[string]string{"TriggerDescription": "Exploit"}}
				sa = sac
			}
			// A granted Offspring (rules.pushTrigger's __kwOffspringGranted
			// payload) has no SVar either: rebuilt structurally into the same
			// DB$ CopyPermanent | Defined$ Self | NumCopies$ Count$OffspringPaid
			// | SetPower$ 1 | SetToughness$ 1 body the printed K:Offspring
			// expansion carries (cards/kw_offspring.go), so the live game and
			// the replay mint identical objects from the event text alone. The
			// "Granted" suffix keeps the payload from aliasing a printed bare
			// K:Offspring line's "__kwOffspring" SVar (the Exploit comment's
			// rule).
			if _, ok := strings.CutPrefix(e.Counter, "__kwOffspringGranted"); ok {
				sa = &cards.SA{Kind: "DB", API: "CopyPermanent",
					Params: map[string]string{"Defined": "Self", "NumCopies": "Count$OffspringPaid",
						"SetPower": "1", "SetToughness": "1"}}
			}
			// A granted Mentor (rules.pushTrigger's __kwMentorGranted payload)
			// has no SVar either: rebuilt structurally into the same
			// DB$ PutCounter | ValidTgts$ Creature.attacking | Mentor$ True
			// targeted body the printed K:Mentor expansion carries
			// (cards/kw_mentor.go), so the live game and the replay mint
			// identical objects from the event text alone. The "Granted"
			// suffix keeps the payload from aliasing the "__kwMentor" SVar a
			// printed bare K:Mentor line mints (the Exploit/Offspring rule).
			// The Mentor$ marker rides the params, so rules' mentorAdmits reads
			// it at both the target offer and the CR 608.2b recheck either way.
			if _, ok := strings.CutPrefix(e.Counter, "__kwMentorGranted"); ok {
				sa = &cards.SA{Kind: "DB", API: "PutCounter",
					Params: map[string]string{"ValidTgts": "Creature.attacking",
						"TgtPrompt": "Select target attacking creature with lesser power",
						"Mentor":    "True", "CounterType": "P1P1", "CounterNum": "1"}}
			}
		}
		// The Gift trigger's payload (rules.pushTrigger's GiftAbility) DOES
		// resolve a real card SVar, but its promise referent must not read
		// the live source: the ability resolves independently of its source
		// (CR 112.7a), and events.Move clears the source's CastFlags bit and
		// GiftPromisedTo the moment the permanent leaves the battlefield --
		// the response window the trigger's own respondability creates. The
		// promised receiver rides the payload as Remembered (IDs), and the
		// body's Defined$/TokenOwner$ Promised referents are rewritten to
		// PromisedSnapshot -- effects' read of that snapshot -- so the live
		// game and a replay mint identical objects from the event text alone.
		// The rewrite CLONES the resolved SA: the SVar table is the card's
		// shared compiled data and Apply must never mutate it.
		if gift && sa != nil {
			clone := *sa
			clone.Params = make(map[string]string, len(sa.Params))
			for k, v := range sa.Params {
				if (k == "Defined" || k == "TokenOwner") && v == "Promised" {
					v = "PromisedSnapshot"
				}
				clone.Params[k] = v
			}
			sa = &clone
		}
		if sa == nil {
			break
		}
		incarnation := src.Incarnation
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = sa
		o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
		o.Source = e.Obj
		// The incarnation stamp arms resolveTop's source-incarnation gate,
		// which drops a keyword trigger whose effect names the SOURCE
		// PERMANENT (Evoke's "sacrifice it") once that permanent leaves and
		// returns as a new object (CR 400.7). It is armed ONLY for a trigger
		// that demonstrably acts on its source (keywordTriggerBindsSource): an
		// ability resolves independently of its source (CR 112.7a), so the
		// default is NO stamp. A granted Ward or Afflict, for example, acts on
		// the targeting spell / captured defender and must still resolve when
		// the granting permanent is removed in response to its own trigger
		// -- the response window that trigger's respondability creates. A
		// promised permanent's gift (CR 702.168c) is likewise independent: its
		// body acts on the promised player (snapshotted into Remembered), so
		// it must still deliver after the source is removed. Leaving the stamp
		// at its zero value makes the gate skip such a trigger exactly as it
		// does for an ordinary matched ETB trigger pushed by TriggerPush.
		if !gift && keywordTriggerBindsSource(e.Counter, sa) {
			o.SourceIncarnation = incarnation
		}
		if conspire || casualty || demonstrate || flanking || melee || cipher || gift {
			o.Remembered = rememberedFrom(e.IDs)
		}

	case StackCopy:
		if !validPlayer(g, e.Player) {
			break
		}
		src := g.Obj(e.Obj)
		if e.Text == "copy card for play" {
			// Cipher's CopyCard$ True on Play: mint a copy of the encoded
			// card in the temporary library holding zone. The cast flow
			// then moves this copy onto the stack. The event, rather than
			// the rules caller, owns the mutation so replay derives its ID.
			if src == nil || src.Face() == nil {
				break
			}
			card, faceIdx := src.Card, src.FaceIdx
			o := g.AddObject(card, e.Player)
			o.FaceIdx, o.IsCopy = faceIdx, true
			break
		}
		if src == nil || src.Zone != state.ZStack {
			break
		}
		// Snapshot everything read from src into locals before AddObject:
		// AddObject appends to g.Objs and may reallocate its backing array,
		// and src is a pointer into that array (g.Obj returns &g.Objs[id-1])
		// -- so a *src field read after AddObject would come from whatever
		// the old backing array still holds, not necessarily kept in sync
		// with the live object src names. Correct today only because
		// nothing between AddObject and the reads below mutates src; this
		// is the engine's one mutation path, so it does not get to rely on
		// that happening to remain true.
		card, faceIdx, ability, source := src.Card, src.FaceIdx, src.Ability, src.Source
		stackKind, stackKindKnown := src.StackKind, src.StackKindKnown
		// A copy of a HAS-ALL-ABILITIES-OF wrapper keeps the minted foreign-face
		// provenance (r3): the copy resolves the same compiled SA, so it reads
		// the same owning face.
		gainedFace, gainedFrom := src.GainedFace, src.GainedFrom
		copyNonLegendary := src.CopyNonLegendary
		copyLoyalty, copyLoyaltySet := src.CopyLoyalty, src.CopyLoyaltySet
		// The copy inherits the original's CastFlags -- a copy of a fused,
		// bestowed or kicked spell resolves as one -- EXCEPT the cast
		// provenance a later reader turns into an "if you cast it"
		// obligation. A copy is put on the stack, not cast (CR 707.10), so
		// state.CastProvenanceFlags is stripped here, at the mint: the copy
		// resolves, Move turns it into a token and clears IsCopy, and
		// rules/altcast.go's battlefield-entry hook has no IsCopy left to
		// tell a never-cast token from the real cast.
		x, castFlags := src.X, src.CastFlags&^state.CastProvenanceFlags
		// CR 708.4: a stack COPY of a face-down (morph-family) spell stays
		// face down. The original's stack marker was folded by the MoveZone
		// branch above from the face-down entry Counter, but a copy is minted
		// by AddObject and never passes through that fold -- so derive the
		// face-down status from the morph-family cast flags it inherits (the
		// same family rules/resolution.go's entry hook re-carries). The flags
		// are read here, before AddObject may reallocate g.Objs. An ordinary
		// (face-up) spell carries none of these bits, so its copy stays
		// unmarked and the view's redaction (view/view.go stackViews) is
		// unchanged for it. Cloaked mirrors the Disguise entry's marker so a
		// copied disguised spell keeps the same state bit the cloak machinery
		// reads.
		morphFlags := castFlags & (state.FlagMorphed | state.FlagMegamorphed | state.FlagDisguised)
		// Deep-copy, never alias: the copy's Targets/Remembered must be
		// able to change independently of the original's once both sit on
		// the stack.
		targets := append([]state.Target(nil), src.Targets...)
		remembered := append([]state.Target(nil), src.Remembered...)
		// Mode announcements are copiable characteristics (CR 707.10): a
		// modal copy must resolve the same chosen modes, not ask for new ones.
		chosenModes := state.CloneChosenModes(src.ChosenModes)
		// A DefinedTarget$ copy names its own targets (the StackCopy doc): the
		// event's IDs replace the inherited list with object targets. The
		// ids are not re-validated here beyond existence -- the copy's own CR
		// 608.2b resolution recheck judges legality, exactly as it does for
		// every other stack object's targets.
		if len(e.IDs) > 0 {
			targets = targets[:0]
			for _, id := range e.IDs {
				if g.Obj(id) != nil {
					targets = append(targets, state.Target{Obj: id})
				}
			}
		}

		o := g.AddObject(card, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.FaceIdx, o.Ability, o.Source = faceIdx, ability, source
		o.StackKind, o.StackKindKnown = stackKind, stackKindKnown
		o.GainedFace, o.GainedFrom = gainedFace, gainedFrom
		o.Targets = targets
		o.Remembered = remembered
		o.ChosenModes = chosenModes
		o.X, o.CastFlags, o.IsCopy = x, castFlags, true
		// A copy was never cast (CR 707.10), so it carries no rider grants:
		// the Spell.MayPlaySource/AddsCounters provenance is a statement about
		// the original's cast, and FlagAddsCounters is stripped from castFlags
		// by CastProvenanceFlags above.
		o.ManaAddsCounterGrants = nil
		if morphFlags != 0 {
			o.FaceDown = true
			o.Cloaked = morphFlags&state.FlagDisguised != 0
		}
		// CR 707.10c: Amount is the creating CopySpellAbility's
		// MayChooseTarget$ discriminator (1 = true). It rides the event so the
		// permission travels with the COPY instance -- an external copier
		// (Mirari, Cloven Casting, Storm, Replicate) whose SA is not part of
		// the copied spell's own text still grants the election on replay,
		// and effects/copy.go never has to reach into rules to ask.
		o.CopyMayChooseTarget = e.Amount == 1
		// The creating CopySpellAbility's rider payload (Counter, the
		// StackCopyCounter grammar): NonLegendary$ True strips the Legendary
		// supertype and SetLoyalty$ overrides the entry's starting loyalty
		// (Ob Nixilis, the Adversary's Casualty:X script). Both changed
		// characteristics are copiable: a later copy of this copy inherits
		// them even without its own payload (CR 707.2), the event's own
		// payload winning when present. Snapshot before AddObject, which may
		// reallocate g.Objs.
		if cc := ParseStackCopyCounter(e.Counter); cc.NonLegendary || cc.HasLoyalty {
			if cc.NonLegendary {
				o.CopyNonLegendary = true
			}
			if cc.HasLoyalty {
				o.CopyLoyalty, o.CopyLoyaltySet = cc.Loyalty, true
			}
		} else {
			o.CopyNonLegendary = copyNonLegendary
			if copyLoyaltySet {
				o.CopyLoyalty, o.CopyLoyaltySet = copyLoyalty, true
			}
		}

	case Attach:
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

	case Unattached:
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

	case AbilityPush:
		// Mirrors TriggerPush above (Ruling T20-a): the ability object is
		// minted here, inside Apply, so a log-only replay creates the same
		// object a live game did.
		if !validPlayer(g, e.Player) {
			break
		}
		src := g.Obj(e.Obj)
		if src == nil {
			break
		}
		// Amount is a FLAT pile-ability index: the top face's Abilities in
		// order, then each card merged beneath it (CR 702.140d). A plain
		// permanent's flat index is exactly its old top-face index, so no
		// existing event changes meaning; a mutated pile's under-card ability
		// decodes against the same folded MergedCards a replay rebuilt before
		// this push. Resolving through the pile is what makes an under-card
		// activation mint the under-card's SA rather than the top face's.
		pa, ok := src.PileAbilityAt(int(e.Amount))
		if !ok {
			break
		}
		// The per-source activation census (state/object.go's
		// ActivatedThisTurn): one AbilityPush per non-mana activation, folded
		// onto the source the same way the other per-turn object facts are.
		// Mutated BEFORE AddObject, per StackCopy's discipline below:
		// AddObject appends to g.Objs and may reallocate its backing array,
		// and src is a pointer into it (g.Obj returns &g.Objs[id-1]) -- a
		// src pointer mutated after the mint would write into the orphaned
		// old array whenever the append reallocated, silently dropping the
		// increment. A cloned engine's Objs slice is exactly full (Game.Clone
		// copies into len-sized storage), so its very next mint always
		// reallocates: the snapshot-derived view path lost every activation
		// the mint coincided with, while the from-genesis replay kept them
		// (the parked-overshoot view test's Shepherd of Rot divergence).
		if src.Zone == state.ZBattlefield {
			src.ActivatedThisTurn++
		}
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = pa.SA
		o.StackKind, o.StackKindKnown = state.StackKindActivated, true
		o.Source = e.Obj
		// Same PlayerRef decode as TriggerPush above (FL-41): an activated
		// ability can remember a player the same way a trigger can, so the
		// two mint paths stay symmetric through rememberedFrom.
		o.Remembered = rememberedFrom(e.IDs)

	case DelayedRegister:
		// Ruling dt1-a: the registration is game state folded here, so a
		// log-only replay rebuilds the same set a live game held. Totality
		// like every case: an invalid controller, a nonexistent source, or a
		// non-canonical Step (at the very least a zero Step is untap, which
		// every DelayedRegister this build emits carries a real phase for)
		// degrades to a no-op rather than panicking.
		if !validPlayer(g, e.Player) {
			break
		}
		if g.Obj(e.Obj) == nil || !e.Step.Valid() {
			break
		}
		src := g.Obj(e.Obj)
		// Dash and Warp refer to the exact permanent that received their
		// keyword promise, and so does the AtEOT$ end-of-turn rider family
		// (effects/atEOTBody reuses the warp body for Exile and mints its
		// own __kwAtEOTDestroy for Destroy) and the MayFlashSac cleanup
		// sacrifice (rules/mayflashsac.go's __kwMayFlashSacrifice): a copy
		// that left the battlefield and returned as a new incarnation is NOT
		// acted on by the stale promise (CR 400.7). Encore's grouped delayed
		// trigger does not: CR 603.7 leaves it independent of the card that
		// created it, and it must sacrifice its remembered token group even
		// if that card later changes zones and returns as a new incarnation.
		track := strings.HasPrefix(e.Counter, "__kwDash") ||
			strings.HasPrefix(e.Counter, "__kwWarp") ||
			strings.HasPrefix(e.Counter, "__kwUnearth") ||
			strings.HasPrefix(e.Counter, "__kwAtEOT") ||
			strings.HasPrefix(e.Counter, "__kwMayFlashSac")
		// Event-matched (non-phase) registrations encode
		// "<Mode$ value>:<trigger SVar name>" in Text. The DelayedRegister
		// event gains no field of its own (Ruling T20-a's field-reuse
		// precedent); a Mode$ Phase registration's Text is the Forge Phase$
		// string, which never contains a colon, and the decode only splits on
		// the modes rules.registerOpeningEffectTriggers emits, so every
		// already-logged registration decodes as a phase one. An inline
		// body (effDelayedTrigger's Mode$ SpellCast branch, where the
		// DelayedTrigger SA is a face Ability with no SVar name of its own)
		// stores the RAW trigger parameters instead of a name: the body
		// starts "Mode$", which no SVar name does, so fire time can tell the
		// two carriers apart.
		//
		// The tail suffixes parse off a working copy so the stored body and
		// the phase string never carry them: ValidPlayer$ rides
		// "|VP=<value>" and a ThisTurn$ True registration's expiry turn
		// rides "|TT=<turn>" (state.DelayedTrigger.MaxTurn; zero when
		// absent). The phase registrations' Texts ("Upkeep",
		// "End of Turn|VP=You") contain neither spelling inside the phase
		// name, so every already-logged registration decodes exactly as
		// before (VP ungated; MaxTurn zero).
		text := e.Text
		effectRepeat := strings.HasSuffix(text, "|EF")
		if effectRepeat {
			text = strings.TrimSuffix(text, "|EF")
		}
		// OptionalDecider$ (an api:Effect Triggers$ body's "you may"
		// election) rides "|OD=<spec>" the same way ValidPlayer$ and MaxTurn
		// do. It is stripped before the mode/Trigger split below so the
		// suffix cannot reach the stored body name; the value never contains
		// "|", so a single LastIndex is exact.
		// Phase registrations append VP after OD, while event registrations
		// have OD at the tail. Strip the phase suffix first so an optional
		// spec cannot accidentally swallow its player gate.
		// A Phase registration's IsPresent$/PresentZone$/PresentCompare$
		// condition rides "|IP=<spec>", "|PZ=<zone>", "|PC=<compare>" after
		// every other suffix effDelayedTrigger writes
		// ("<Phase>|VP=<value>|IP=<spec>|PZ=<zone>|PC=<compare>"), so they
		// are stripped in reverse append order, BEFORE the VP gate: a
		// LastIndex on an earlier marker would otherwise swallow the later
		// ones into its value (Bank Job and Grinning Totem carry ValidPlayer$
		// YOU together with IsPresent$, so VP-before-IP is the ordinary shape,
		// not an edge). The values are Forge filter/zone/compare tokens with
		// no "|", so each strip is exact; a registration logged before this
		// slot existed carries none of the spelling and decodes with all
		// three empty, exactly as before.
		presentCompare := ""
		if i := strings.LastIndex(text, "|PC="); i >= 0 {
			presentCompare = text[i+4:]
			text = text[:i]
		}
		presentZone := ""
		if i := strings.LastIndex(text, "|PZ="); i >= 0 {
			presentZone = text[i+4:]
			text = text[:i]
		}
		presentSpec := ""
		if i := strings.LastIndex(text, "|IP="); i >= 0 {
			presentSpec = text[i+4:]
			text = text[:i]
		}
		vp := ""
		if i := strings.LastIndex(text, "|VP="); i >= 0 {
			vp = text[i+4:]
			text = text[:i]
		}
		optionalSpec := ""
		if i := strings.LastIndex(text, "|OD="); i >= 0 {
			optionalSpec = text[i+4:]
			text = text[:i]
		}
		// Effect lifetime suffixes are stripped before the mode/body split.
		// Their values are Forge tokens without a pipe delimiter. |SB is a
		// bare flag appended LAST by the registering arm, so it is stripped
		// FIRST (before the value-bearing suffixes and |IH).
		sourceBattlefield := strings.HasSuffix(text, "|SB")
		if sourceBattlefield {
			text = strings.TrimSuffix(text, "|SB")
		}
		imprint := strings.HasSuffix(text, "|IH")
		if imprint {
			text = strings.TrimSuffix(text, "|IH")
		}
		strip := func(key string) string {
			if i := strings.LastIndex(text, key); i >= 0 {
				value := text[i+len(key):]
				text = text[:i]
				return value
			}
			return ""
		}
		cast := strip("|FC=")
		counter := strip("|FK=")
		exile := strip("|XM=")
		forget := strip("|FM=")
		duration := strip("|DU=")
		maxTurn := int32(0)
		if i := strings.LastIndex(text, "|TT="); i >= 0 {
			if n, err := strconv.Atoi(strings.TrimSpace(text[i+4:])); err == nil && n > 0 {
				maxTurn = int32(n)
			}
			text = text[:i]
		}
		mode, trigger := "", ""
		if i := strings.Index(text, ":"); i > 0 &&
			(effectRepeat || text[:i] == "SpellCast" || text[:i] == "ChangesZone" ||
				text[:i] == "ChangesController" || text[:i] == "DamageDone" ||
				text[:i] == "AttackersDeclared" || text[:i] == "BecomeMonarch") {
			mode, trigger = text[:i], text[i+1:]
		}
		g.Delayed = append(g.Delayed, state.DelayedTrigger{
			ID:                g.DelayedNext,
			Phase:             e.Step,
			Source:            e.Obj,
			Controller:        e.Player,
			Execute:           e.Counter,
			Remembered:        rememberedFrom(e.IDs),
			MinTurn:           e.Amount,
			MaxTurn:           maxTurn,
			SourceIncarnation: src.Incarnation,
			TrackSource:       track,
			EventMode:         mode,
			Trigger:           trigger,
			EffectRepeat:      effectRepeat,
			ValidPlayer:       vp,
			PresentSpec:       presentSpec,
			PresentZone:       presentZone,
			PresentCompare:    presentCompare,
			OptionalSpec:      optionalSpec,
			EffectDuration:    duration,
			BirthTurn:         g.Turn,
			ForgetOnMoved:     forget,
			ExileOnMoved:      exile,
			ForgetCounter:     counter,
			ForgetOnCast:      cast,
			ImprintOnHost:     imprint,
			SourceBattlefield: sourceBattlefield,
		})
		g.DelayedNext++

	case DelayedForget:
		for i := range g.Delayed {
			if g.Delayed[i].ID != uint32(e.Amount) {
				continue
			}
			for j, target := range g.Delayed[i].Remembered {
				if target.Obj == e.Obj {
					g.Delayed[i].Remembered = append(g.Delayed[i].Remembered[:j:j], g.Delayed[i].Remembered[j+1:]...)
					break
				}
			}
			break
		}

	case DelayedRemove:
		for i := range g.Delayed {
			if g.Delayed[i].ID == uint32(e.Amount) {
				g.Delayed = append(g.Delayed[:i], g.Delayed[i+1:]...)
				break
			}
		}

	case DelayedPush:
		// Ruling dt1-a: the ability object is minted here, inside Apply, so a
		// log-only replay creates the same object a live game did (the
		// Ruling T20-a precedent TriggerPush and AbilityPush already set).
		// Unlike those two, the Ability is not a face Triggers index -- a
		// delayed trigger's Effect is an SVar-named sub-ability on the
		// source's face -- so it is resolved here from e.Counter (the
		// Execute$ name) via cards.ResolveSVar. Firing is one-shot: the
		// matching registration is removed, which is what keeps a delayed
		// trigger from firing every turn.
		if !validPlayer(g, e.Player) {
			break
		}
		// CR 724.2a's monarch draw is the engine's OWN trigger, minted from
		// a synthetic body with no card registration at all: it must never
		// consume one. Its event carries Amount zero (no DelayedRegister
		// ever set it), which would otherwise match registration ID 0 and
		// delete a bystander's pending delayed trigger.
		monarchDraw := e.Counter == "__monarch_draw"
		radiationDrain := e.Counter == "__radiation_drain"
		// Consume the registration first, even when its tracked permanent has
		// changed incarnation. A stale dash/warp promise expires once; it must
		// neither act on the returned object nor be retried forever. Ordinary
		// delayed triggers, including Encore's group cleanup, are independent
		// of their source and still resolve.
		var registration *state.DelayedTrigger
		if !monarchDraw && !radiationDrain {
			for i := range g.Delayed {
				if g.Delayed[i].ID == uint32(e.Amount) {
					dt := g.Delayed[i]
					registration = &dt
					if !dt.EffectRepeat {
						g.Delayed = append(g.Delayed[:i], g.Delayed[i+1:]...)
					}
					break
				}
			}
		}
		src := g.Obj(e.Obj)
		if !radiationDrain {
			if src == nil {
				break
			}
			if registration != nil && registration.TrackSource &&
				src.Incarnation != registration.SourceIncarnation {
				break
			}
			if src.Face() == nil {
				break
			}
		}
		var sa *cards.SA
		if monarchDraw {
			sa = &cards.SA{Kind: "DB", API: "Draw", Params: map[string]string{"Defined": "You", "NumCards": "1"}}
		} else if radiationDrain {
			sa = &cards.SA{Kind: "DB", API: "RadiationDrain", Params: map[string]string{"Defined": "You"}}
		} else {
			sa = ResolveSVarAcrossFaces(src, e.Counter)
		}
		if sa == nil {
			break
		}
		if e.Text == "static" {
			// Forge's static delayed trigger (TriggerHandler's isStatic arm):
			// the Execute body resolved IMMEDIATELY at fire time — rules ran
			// it inline — so the one-shot registration is consumed and no
			// ability object is minted. Every earlier DelayedPush carries no
			// Text, so already-logged firings mint exactly as before.
			break
		}
		if e.Text == "granted ability" {
			// A SELF-granted activation (rules' shared activation flow mints
			// it through this delayed shape): count it on the per-source
			// activation census exactly as GrantAbilityPush counts a
			// cross-object grant.
			countActivation(g, e.Obj)
		}
		// StackCopy's discipline: snapshot every src field the post-mint
		// code reads (Incarnation here) before AddObject may reallocate
		// g.Objs and orphan the src pointer.
		var incarnation uint32
		if src != nil {
			incarnation = src.Incarnation
		}
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = sa
		o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
		if !radiationDrain {
			o.Source = e.Obj
		}
		if registration != nil && registration.TrackSource {
			o.SourceIncarnation = incarnation
		}
		o.Remembered = rememberedFrom(e.IDs)

	case MergedTriggerPush:
		// CR 702.140d: a mutated pile's under-card trigger fired and its
		// ability object is minted here, inside Apply, so a log-only replay
		// creates the same object a live game did (the Ruling T20-a/DelayedPush
		// precedent). Unlike DelayedPush no registration is consumed -- the
		// grant-trigger shape -- and unlike both by-name siblings the ability
		// is minted from the UNDER-CARD's own COMPILED trigger, named by
		// e.Amount (the packed pair: its pile index in MergedCards, and that
		// face's Triggers index), exactly as TriggerPush mints from the top
		// face's. Not a by-name SVar walk, for two reasons: the pile's top
		// face may define the same Execute$ name with a different body
		// (Cubwarden's two Cats must not become Everquill Phoenix's Feather),
		// and cards.ResolveSVar parses a fresh *SA whose pointer matches no
		// compiled cards.Trigger -- which is how every consumer that recovers
		// a resolving ability's owning trigger (findTriggerForAbilityFace:
		// the OptionalDecider$ gate, the intervening-if recheck, the
		// ResolvedLimit$ count, the label, the merged-face SVar table)
		// identifies it. e.Counter carries the Execute$ name as the log's
		// readable provenance and is checked against the trigger line here,
		// so a truncated or tampered log mints nothing rather than the wrong
		// ability.
		if !validPlayer(g, e.Player) {
			break
		}
		src := g.Obj(e.Obj)
		if src == nil {
			break
		}
		mergedIdx, trigIdx, ok := MergedTriggerIndexes(e.Amount)
		if !ok {
			break
		}
		f := src.MergedFaceAt(mergedIdx)
		if f == nil || trigIdx >= len(f.Triggers) {
			break
		}
		tr := f.Triggers[trigIdx]
		if tr.Effect == nil || tr.Params["Execute"] != e.Counter {
			break
		}
		sa := tr.Effect
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = sa
		o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
		o.Source = e.Obj
		o.Remembered = rememberedFrom(e.IDs)

	case GrantTriggerPush:
		// A static-grant's trigger (AddTrigger$ on a Mode$ Continuous static):
		// the Ruling T20-a/DelayedPush precedent -- the ability object is
		// minted here, inside Apply, so a log-only replay creates the same
		// object a live game did. Like DelayedPush the Ability is not a face
		// Triggers index: it is the granted trigger's Execute$ SVar-named
		// body. The body lives on the GRANTOR's face (Forge defines the
		// AddTrigger$-named SVar on the card carrying the static), while Obj
		// is the AFFECTED recipient -- the two differ for a cross-object
		// grant (an Aura granting its enchanted creature a trigger). The
		// grantor rides Amount (0 = the historical self-grant shape, where
		// grantor == recipient and the AFFECTED table is the right one):
		// when set, the name resolves from the grantor's table -- the exact
		// table rules' queue gate linked the body from -- else from the
		// affected object's own table (the self-grant path, byte-identical
		// for every already-logged event). A grantor that has left the
		// battlefield, or whose face no longer resolves the name, mints
		// nothing (the totality stance every SVar resolution takes).
		// o.Source stays e.Obj: a granted body's `Defined$ Self`/`CARDNAME`
		// names the recipient. No registration is consumed: a granted
		// trigger is fired by nothing and lives exactly as long as its
		// granting static.
		if !validPlayer(g, e.Player) {
			break
		}
		src := g.Obj(e.Obj)
		if src == nil || src.Face() == nil {
			break
		}
		resolver := src
		if e.Amount > 0 {
			if grantor := g.Obj(state.ObjID(e.Amount)); grantor != nil && grantor.Face() != nil {
				resolver = grantor
			} else {
				break
			}
		}
		sa := ResolveSVarAcrossFaces(resolver, e.Counter)
		if sa == nil {
			break
		}
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = sa
		o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
		o.Source = e.Obj
		o.Remembered = rememberedFrom(e.IDs)

	case GrantAbilityPush:
		// A cross-object ability grant (CR 613.1f): the granting static's
		// SOURCE resolves the SVar body (Counter), while the minted ability
		// object's Source is the RECIPIENT (Obj). The DelayedPush/
		// GrantTriggerPush precedent -- mint inside Apply so a log-only
		// replay creates the identical object. IDs[0] is the granting
		// object, carried here rather than on Obj because Obj must stay the
		// recipient; it is NOT decoded into Remembered (the ability's
		// Remembered set is unrelated to who granted it). A grantor that
		// has left the battlefield, or whose face no longer resolves the
		// name, mints nothing (the totality stance every SVar resolution
		// takes). No registration is consumed: unlike a delayed trigger a
		// grant lives exactly as long as its granting static, and rules
		// re-derives the offer each priority window.
		if !validPlayer(g, e.Player) {
			break
		}
		if len(e.IDs) == 0 {
			break
		}
		grantor := g.Obj(e.IDs[0])
		if grantor == nil || grantor.Face() == nil {
			break
		}
		if g.Obj(e.Obj) == nil {
			break
		}
		sa := ResolveSVarAcrossFaces(grantor, e.Counter)
		if sa == nil {
			break
		}
		// AbilityPush's per-source activation census (ActivatedThisTurn),
		// same battlefield condition and the same before-AddObject
		// discipline: a GRANTED activation is an activation of the
		// recipient, and leaving it uncounted let a free granted ability
		// escape the bot's repeatability budget forever.
		countActivation(g, e.Obj)
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = sa
		o.StackKind, o.StackKindKnown = state.StackKindActivated, true
		o.Source = e.Obj

	case GainedAbilityPush:
		// A has-all-abilities-of activated ability (Forge's GainsAbilitiesOf$,
		// task gains1): like AbilityPush the object is minted inside Apply so a
		// log-only replay creates the same object a live game did, but the
		// ability is a compiled SA on a FOREIGN card's face rather than an
		// index of the recipient's own. Obj is the recipient (o.Source, so
		// `Defined$ Self`/`CARDNAME` in the body names it), IDs[0] the foreign
		// card, Amount the index of the ability in that face's Abilities. The
		// compiled pointer is what the activation-limit census and the
		// owning-face SVar reads recover, so it must be the face's own SA --
		// never a fresh parse. A missing IDs[0], a foreign card that left the
		// scoped zone, or a stale index mints nothing (the totality stance
		// every case here takes).
		if !validPlayer(g, e.Player) {
			break
		}
		if len(e.IDs) == 0 {
			break
		}
		if g.Obj(e.Obj) == nil {
			break
		}
		foreign := g.Obj(e.IDs[0])
		if foreign == nil || foreign.Face() == nil {
			break
		}
		abilities := foreign.Face().Abilities
		if e.Amount < 0 || int(e.Amount) >= len(abilities) {
			break
		}
		sa := abilities[int(e.Amount)]
		if sa == nil {
			break
		}
		// The per-source activation census, as AbilityPush and
		// GrantAbilityPush count it: Myr Welder activating an imprinted
		// Knowledge Vault's "{0}: Sacrifice" was never counted, so the bot's
		// repeatability budget never closed and it re-activated forever.
		countActivation(g, e.Obj)
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = sa
		o.StackKind, o.StackKindKnown = state.StackKindActivated, true
		o.Source = e.Obj
		// The wrapper carries its foreign-face provenance on itself (r3):
		// the granting static can END between this push and the resolution --
		// the foreign card leaves the scoped zone, the static's named set
		// re-derives -- and the live-grant recovery scans then find no owner.
		// Setting it here (never in rules/) is what makes a log-only replay
		// reproduce the exact face the live mint resolved.
		o.GainedFace = foreign.Face()
		o.GainedFrom = foreign.ID

	case GainedTriggerPush:
		// A has-all-abilities-of triggered ability (Forge's GainsTriggerAbsOf$,
		// task gains1): the GainedAbilityPush shape one level over, the
		// MergedTriggerPush precedent's Apply-time mint. Obj is the recipient
		// (o.Source), IDs[0] the foreign card, Amount the index of the
		// trigger in that face's Triggers, and Counter the Execute$ name
		// carried as readable provenance and checked against the trigger line
		// here -- so a truncated or tampered log mints nothing rather than the
		// wrong ability. The face's own compiled Trigger.Effect pointer is
		// minted, never a by-name parse, for the same reason MergedTriggerPush
		// mints the compiled pointer: every consumer that recovers a resolving
		// ability's owning trigger (the OptionalDecider$ gate, the
		// intervening-if recheck, the label, the SVar table) does so by
		// pointer identity.
		if !validPlayer(g, e.Player) {
			break
		}
		if len(e.IDs) == 0 {
			break
		}
		if g.Obj(e.Obj) == nil {
			break
		}
		foreign := g.Obj(e.IDs[0])
		if foreign == nil || foreign.Face() == nil {
			break
		}
		triggers := foreign.Face().Triggers
		if e.Amount < 0 || int(e.Amount) >= len(triggers) {
			break
		}
		// The Trigger is a value type on the face; take a stable pointer to
		// the element rather than copying (the compiled pointer identity the
		// consumers rely on). A face's Triggers slice never resizes after
		// parse, so the pointer stays valid for the match.
		tr := &triggers[int(e.Amount)]
		if tr.Effect == nil || tr.Params["Execute"] != e.Counter {
			break
		}
		sa := tr.Effect
		o := g.AddObject(nil, e.Player)
		Move(g, o.ID, state.ZLibrary, state.ZStack)
		o.Ability = sa
		o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
		o.Source = e.Obj
		// The same foreign-face provenance the GainedAbilityPush mint stamps
		// (r3): the foreign face's own SVar table, OptionalDecider$ gate,
		// intervening-if and label all stay resolvable after the grant ends.
		o.GainedFace = foreign.Face()
		o.GainedFrom = foreign.ID
		// pushTrigger serializes the queue-time ctx Remembered AFTER the
		// provenance slot (IDs[0] is the foreign card): the same decode
		// TriggerPush/AbilityPush run through rememberedFrom, so a gained
		// trigger's remembered-derived body (Defined$ Remembered,
		// Remembered$Amount, Card.IsRemembered) resolves the same referents a
		// live queue walked with. Without this the wrapper resolves with an
		// empty remembered set and the body acts on nothing.
		o.Remembered = rememberedFrom(e.IDs[1:])

	case CmdDamage:
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

	case DamageProvenance:
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
			break
		}
		src := e.Obj
		if p, isPlayer := e.IDs[0].PlayerRef(); isPlayer {
			if !validPlayer(g, p) {
				break
			}
			rec := g.Players[p].DamageTakenByGame
			if !containsObjID(rec, src) {
				g.Players[p].DamageTakenByGame = append(rec, src)
			}
			break
		}
		if o := g.Obj(e.IDs[0]); o != nil {
			if !containsObjID(o.DamageTakenByGame, src) {
				o.DamageTakenByGame = append(o.DamageTakenByGame, src)
			}
		}
	}
}

// containsObjID reports whether ids already holds want. Walked by index so
// the result never depends on map iteration order.
func containsObjID(ids []state.ObjID, want state.ObjID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// commanderDenseIndex returns id's match-wide dense commander index - (valid
// commanders in every seat before its owner) plus (its position within its
// owner's Commanders list) - and whether id is a commander at all, mirroring
// the exact indexing rules.New assigns at genesis (see rules/engine.go):
// seat B's CmdDamage holds a slot for seat A's commander at A's commander's
// dense index, so the k-th commander of seat p is index
// sum(len(Commanders[s]) for s<p) + k. Walked deterministically by index over
// the seat slice and each seat's Commanders slice - never a map - so the
// order cannot and does not matter to the result.
func commanderDenseIndex(g *state.Game, id state.ObjID) (int, bool) {
	if id == 0 {
		return 0, false
	}
	for p := range g.Players {
		for k, c := range g.Players[p].Commanders {
			if c == id {
				idx := 0
				for s := 0; s < p; s++ {
					idx += len(g.Players[s].Commanders)
				}
				return idx + k, true
			}
		}
	}
	return 0, false
}

// rememberedFrom decodes an event's IDs into the Remembered list an ability
// object carries: a real object id becomes {Obj: id}, and a PlayerRef
// sentinel (state.PlayerRef, rules.pushTrigger) becomes {Player: p,
// IsPlayer: true}. TriggerPush and AbilityPush both use it so the two mint
// paths stay symmetric.
func rememberedFrom(ids []state.ObjID) []state.Target {
	var out []state.Target
	for _, id := range ids {
		if p, ok := id.PlayerRef(); ok {
			out = append(out, state.Target{Player: p, IsPlayer: true})
			continue
		}
		out = append(out, state.Target{Obj: id})
	}
	return out
}

// ringEmblemAbility rebuilds one of the Ring emblem's four level abilities
// (CR 701.54c) from its level alone. The emblem has no corpus script text
// and no object in any zone, so these bodies are hand-built here in events,
// exactly as the granted ward/afflict payloads (KeywordTriggerPush) are:
// Apply rebuilds from the "__ring:<level>" payload so a log-only replay
// mints the identical ability object a live game did. Level N is active iff
// the tempted seat's RingTempted >= N; lower levels stay active as the count
// rises (the emitter gates, this function only builds).
//
//  1. "Whenever your Ring-bearer attacks, draw a card."
//  2. "Whenever your Ring-bearer becomes blocked, discard a card. If you
//     can't, sacrifice it." The discard is TgtChoose (the discarding
//     player's own choice); its RememberDiscarded$ records what (if
//     anything) went, and the chained Sacrifice is gated on that set being
//     EMPTY (ConditionDefined$ Remembered | ConditionPresent$ Card |
//     ConditionCompare$ EQ0) -- the corpus's exact "if you can't" shape
//     (Davriel, Soul Broker). effDiscard's strict-supersets rule means an
//     empty or too-small hand discards nothing and asks nothing, which IS
//     the "can't" arm.
//  3. "Whenever your Ring-bearer deals combat damage to a player,
//     sacrifice it." The SacValid$ reads the LIVE designation, so a bearer
//     already dead from the combat damage leaves nothing eligible: no ask,
//     no-op ("sacrifice it" of something that no longer exists).
//  4. "Whenever the Ring tempts you, each opponent loses 1 life."
func ringEmblemAbility(level int) *cards.SA {
	switch level {
	case 1:
		return &cards.SA{Kind: "DB", API: "Draw", Params: map[string]string{
			"Defined": "You",
		}}
	case 2:
		sac := &cards.SA{Kind: "DB", API: "Sacrifice", Params: map[string]string{
			"Defined": "You", "SacValid": "Card.IsRingbearer+YouCtrl", "Amount": "1",
			"ConditionDefined": "Remembered", "ConditionPresent": "Card", "ConditionCompare": "EQ0",
		}}
		return &cards.SA{Kind: "DB", API: "Discard", Params: map[string]string{
			"Defined": "You", "NumCards": "1", "Mode": "TgtChoose", "RememberDiscarded": "True",
		}, Sub: sac}
	case 3:
		return &cards.SA{Kind: "DB", API: "Sacrifice", Params: map[string]string{
			"Defined": "You", "SacValid": "Card.IsRingbearer+YouCtrl", "Amount": "1",
		}}
	case 4:
		return &cards.SA{Kind: "DB", API: "LoseLife", Params: map[string]string{
			"Defined": "Opponent", "LifeAmount": "1",
		}}
	}
	return nil
}

// Move relocates an object between zones, preserving zone order and the
// one-object-one-zone invariant.
//
// The zone an object is removed from is always o.Zone — the object's own
// recorded location — never the caller-supplied from. from (and Event.From
// in the log) exist for the client and for replay to read, but a caller that
// gets it wrong must not be able to leave the object in its real zone while
// also adding it to to: that would put it in two zones at once, and a
// repeat of the same wrong move would duplicate it within one zone.
//
// Moving an object to the zone it is already in is not special-cased: it is
// removed from that zone and appended again, so it ends up at the end of the
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
	if wasBattlefield && to != state.ZBattlefield {
		// Leaving combat removes this permanent as a blocker, but does not
		// make creatures it blocked unblocked (CR 506.4, 509.1h). Preserve
		// each attacker's blocker-list length with the same zero tombstone
		// EndCombatReset uses for regeneration. Dense arena order keeps this
		// deterministic, and the departing object's own state is cleared by
		// the zone reset below.
		for i := range g.Objs {
			other := &g.Objs[i]
			if other.ID == id {
				continue
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
			o.FaceIdx = 0
		}
	}
	// CR 712.4d: a Modal DFC is front-face up in every non-battlefield
	// zone. Its back face remains active while it is a permanent, but leaving
	// the battlefield creates a new object whose characteristics are the
	// front face. Keep this in the event fold so replay and live play agree.
	if wasBattlefield && to != state.ZBattlefield && o.Card != nil &&
		o.Card.AlternateMode == "Modal" && len(o.Card.Faces) == 2 &&
		o.Card.Faces[0] != nil && o.Card.Faces[1] != nil {
		o.FaceIdx = 0
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
		// Leaving the battlefield or the stack resets everything that only
		// exists while a permanent or spell is in play.
		o.Tapped = false
		o.Damage = 0
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
		o.CopyFace = nil
		o.CopyGainThisAbility = false
		o.Paired = 0
		o.Targets = nil
		// o.Remembered is deliberately NOT reset here: a card's remembered
		// list is CARD memory, not permanent state -- Forge preserves it
		// across zone changes, which is the whole O-Ring premise (the return
		// trigger on a card in the graveyard reads the exile its
		// battlefield-stint remembered) and the reason its ForgetOtherTargets$
		// exists at all (the deliberate clear on a re-exile). The list stays
		// event-backed (Choose "remembered"/"clear-remembered"), so live play
		// and replay derive it identically either way.
		if wasBattlefield {
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
			o.Convoked = nil
			o.ManaAddsCounterGrants = nil
			o.ManaSpent = 0
			o.ManaSnowSpent = 0
			o.ManaTreasureSpent = 0
			o.ManaCaveSpent = 0
			o.ManaDesertSpent = 0
			o.ManaArtifactSpent = 0
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
			// this turn and carries no untap-skip window.
			o.ExertedThisTurn, o.ExertSkipUntap = false, false
			// CR 702.160: enlist is the old permanent's fact, not the new
			// object's -- a re-entering creature carries no enlist stamp.
			o.EnlistedTurn, o.EnlistedCombat = 0, 0
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
			o.Convoked = nil
			o.ManaAddsCounterGrants = nil
			o.ManaSpent = 0
			o.ManaSnowSpent = 0
			o.ManaTreasureSpent = 0
			o.ManaCaveSpent = 0
			o.ManaDesertSpent = 0
			o.ManaArtifactSpent = 0
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

func changeControl(g *state.Game, o *state.Object, p state.PlayerID) {
	if o.Controller == p {
		return
	}
	if o.Zone == state.ZBattlefield {
		remove(g, o.ID, state.ZBattlefield, o.Controller)
		g.SetZone(state.ZBattlefield, p, append(g.Zone(state.ZBattlefield, p), o.ID))
		for i := range g.Objs {
			other := &g.Objs[i]
			if other.ID == o.ID {
				continue
			}
			for j, blocker := range other.BlockedBy {
				if blocker == o.ID {
					other.BlockedBy[j] = 0
				}
			}
		}
		o.IsAttacking = false
		o.AttackingBattle = 0
		o.BlockedBy = nil
		o.SummonSick = true
		// kw:Echo's gate stamp (CR 702.35a): a battlefield control change is
		// a fresh "came under your control" moment for the new controller,
		// so the acquisition tuple re-stamps here. The echo trigger's own
		// ValidPlayer$ You keeps it firing only during the controller's
		// upkeep, so this record is read against the new controller's
		// Player.LastUpkeepTurn.
		o.AcqTurn = g.Turn
		o.AcqStep = g.Step
	}
	o.Controller = p
}

// validPlayer reports whether p indexes an existing seat.
func validPlayer(g *state.Game, p state.PlayerID) bool {
	return int(p) < len(g.Players)
}

// manaClearKeepSlots parses the keep-mask Text the stat:UnspentMana emitter
// rides on a ManaClear event: one WUBRGC letter per pool slot whose unspent
// mana the boundary must not empty (rules/turn.go's unspentManaKeep). An
// empty Text (every historical event, and every game without a live
// carrier) keeps nothing. The answer is a fixed-size mask over the pool
// slot order, so the fold is a slice test, not a map lookup.
func manaClearKeepSlots(text string) [6]bool {
	var keep [6]bool
	for i := 0; i < len(text); i++ {
		if s := state.ManaIndex(text[i]); s >= 0 && s < len(keep) {
			keep[s] = true
		}
	}
	return keep
}

// cutManaPersistent splits a ManaAdd event's Text encoding into the
// restriction encoding it may also carry and whether the mana is persistent
// (PersistentMana$ True — the trailing " pm" suffix ManaPersistentText
// appends). Ordinary historical events never carry the suffix.
func cutManaPersistent(text string) (string, bool) {
	rest, ok := strings.CutSuffix(text, manaPersistentSuffix)
	return rest, ok
}

// clearNonPersistent drains n ordinary (non-persistent) units from slot i of
// the pool ManaClear is emptying. The tagged tallies drain first (the typed
// units in their fixed Treasure > Cave > Desert order, then snow), then the
// plain remainder absorbs the rest with no tally to drain — so every
// SURVIVING unit's tag is attributed to the persistent share, which is where
// it belongs: every measured PersistentMana carrier produces plain mana, so
// the ordinary share drained at a boundary is the tagged one when one
// exists. The persistent tally is left alone: the surviving units are
// exactly the persistent ones (the caller cleared exactly Pool minus
// PersistentMana), so PersistentMana[i] keeps naming them. A persistent unit
// that is ALSO tagged (snow/typed) is the one attribution this drain cannot
// see — the final clamp keeps every tally within the pool in that
// unmeasured corner instead of letting the Snow/Typed <= Pool invariant
// break.
func clearNonPersistent(p *state.Player, i int, n int32) {
	p.Pool[i] -= n
	for t := range p.TypedMana {
		if n == 0 {
			break
		}
		d := min(n, p.TypedMana[t][i])
		p.TypedMana[t][i] -= d
		if t < 3 {
			// Drain the artifact subset with its parent type tally.
			p.ArtifactTyped[t][i] -= min(d, p.ArtifactTyped[t][i])
		}
		n -= d
	}
	if n > 0 {
		d := min(n, p.Snow[i])
		p.Snow[i] -= d
		n -= d
	}
	// n's remainder (if any) was plain ordinary units — no tally carries
	// them. Clamp the corner: a persistent unit that is also snow or typed
	// may have had its tag drained by the order above; restore the invariant
	// every reader (takeUnit, the typed count heads) relies on.
	if p.Snow[i] > p.Pool[i] {
		p.Snow[i] = p.Pool[i]
	}
	total := p.Snow[i]
	for t := range p.TypedMana {
		if p.TypedMana[t][i] > p.Pool[i] {
			p.TypedMana[t][i] = p.Pool[i]
		}
		if t < 3 && p.ArtifactTyped[t][i] > p.TypedMana[t][i] {
			p.ArtifactTyped[t][i] = p.TypedMana[t][i]
		}
		total += p.TypedMana[t][i]
	}
	if over := total - p.Pool[i]; over > 0 {
		if d := min(over, p.Snow[i]); d > 0 {
			p.Snow[i] -= d
			over -= d
		}
		for t := range p.TypedMana {
			if over == 0 {
				break
			}
			d := min(over, p.TypedMana[t][i])
			p.TypedMana[t][i] -= d
			if t < 3 {
				p.ArtifactTyped[t][i] -= min(d, p.ArtifactTyped[t][i])
			}
			over -= d
		}
	}
}

// clearRingBearers drops every seat's Ring-bearer designation naming id
// (CR 701.54b/701.54e: the designation ends when the permanent leaves the
// battlefield or another player gains control of it — the derived clear
// events.Apply's ControlChange and battlefield-leave paths share).
func clearRingBearers(g *state.Game, id state.ObjID) {
	if id == 0 {
		return
	}
	for i := range g.Players {
		if g.Players[i].RingBearer == id {
			g.Players[i].RingBearer = 0
		}
	}
}

// expireTurnGoads drops only default-duration relationships made by p.
func expireTurnGoads(in []state.GoadEffect, p state.PlayerID) []state.GoadEffect {
	out := in[:0]
	for _, ge := range in {
		if ge.Player == p && (ge.Duration == "" || ge.Duration == "UntilYourNextTurn") {
			continue
		}
		out = append(out, ge)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// pruneGoads enforces source/control conditions from replayable state.
func pruneGoads(g *state.Game) {
	for i := range g.Objs {
		o := &g.Objs[i]
		out := o.Goads[:0]
		for _, ge := range o.Goads {
			active := o.Zone == state.ZBattlefield
			switch ge.Duration {
			case "AsLongAsInPlay":
				src := g.Obj(ge.Source)
				active = active && src != nil && src.Zone == state.ZBattlefield
			case "AsLongAsControl":
				active = o.Zone == state.ZBattlefield && o.Controller == ge.Controller
			}
			if active {
				out = append(out, ge)
			}
		}
		if len(out) == 0 {
			o.Goads = nil
		} else {
			o.Goads = out
		}
	}
}

// zoneOwner picks whose zone list an object belongs to: the battlefield and the
// stack are keyed by controller, every private zone by owner.
func zoneOwner(o *state.Object, z state.Zone) state.PlayerID {
	if z == state.ZBattlefield || z == state.ZStack {
		return o.Controller
	}
	return o.Owner
}

func remove(g *state.Game, id state.ObjID, z state.Zone, p state.PlayerID) {
	src := g.Zone(z, p)
	for i, x := range src {
		if x == id {
			out := make([]state.ObjID, 0, len(src)-1)
			out = append(out, src[:i]...)
			out = append(out, src[i+1:]...)
			g.SetZone(z, p, out)
			return
		}
	}
}

// applyPair writes a Soulbond pairing (CR 702.103): both permanents' Paired
// fields are set to each other's id when both are on the battlefield. Neither
// half is written for an object that has left the battlefield (its own Move
// already reset it).
func applyPair(g *state.Game, srcID, partnerID state.ObjID) {
	src := g.Obj(srcID)
	partner := g.Obj(partnerID)
	if src != nil && src.Zone == state.ZBattlefield && partner != nil && partner.Zone == state.ZBattlefield {
		src.Paired = partnerID
		partner.Paired = srcID
	}
}

// matchExtraPhase finds the queue entry a consume (-1) or complete (-2)
// event names: the FIRST entry (creation order, deterministic) whose
// identity matches the event's carried fields. consumed=false matches only
// un-consumed grants (a consume marks), consumed=true only consumed ones (a
// complete removes). ok=false when no entry matches -- a malformed or
// stale message, a no-op by the case's totality stance.
func matchExtraPhase(g *state.Game, e Event, consumed bool) (int, bool) {
	wantEntry := state.Step(0)
	wantEntryOK := false
	if len(e.IDs) > 0 {
		wantEntry = state.Step(e.IDs[0])
		wantEntryOK = wantEntry.Valid()
	}
	for i := range g.ExtraPhases {
		ep := &g.ExtraPhases[i]
		if ep.Consumed != consumed || ep.Player != e.Player || ep.AfterStep != e.Step ||
			ep.Source != e.Obj || ep.Execute != e.Counter || ep.Entry != wantEntry {
			continue
		}
		if !wantEntryOK {
			continue
		}
		return i, true
	}
	return 0, false
}

// countActivation folds one non-mana activation onto its source's
// per-turn census (state.Object.ActivatedThisTurn), under AbilityPush's
// condition: only a battlefield source counts. It must run BEFORE the
// caller's AddObject, which may reallocate g.Objs under the src pointer.
func countActivation(g *state.Game, id state.ObjID) {
	if src := g.Obj(id); src != nil && src.Zone == state.ZBattlefield {
		src.ActivatedThisTurn++
	}
}
