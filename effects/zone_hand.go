package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// handChangeNum reads the SA's ChangeNum$ as a plain integer literal
// (absent = 1, Forge's ChangeZoneEffect default for this shape). The second
// return is false for anything else -- a non-integer, negative, or value
// outside a decision count's signed 32-bit range -- and the caller routes
// that SA to the pre-existing object path
// instead: evaluating SVar/Count$ count expressions here is a scoped-out
// follow-up, not part of handmove1.
func handChangeNum(cz *ChangeZoneParams) (int32, bool) {
	v, present := cz.ChangeNum.Text, cz.ChangeNum.Present
	if !present || v == "" {
		return 1, true
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil || n < 0 {
		return 0, false
	}
	return int32(n), true
}

// effChangeZoneHand is the whole-hand shape (handmove1/rv2b r1): Origin$ Hand
// with no player selector -- the resolving controller's own hand is the one
// owner, and the controller is its own chooser (Brainstorm, Jace the Mind
// Sculptor's [0], Sawtooth Loon, Burgeoning). The mechanics -- the ask gate
// (the dig1/effDiscard strict-supersets rule), explicit markers plus real
// card/script text for markerless optionality (never an assumed "may"), the
// fx42 re-entry scoping, the R-9 stand-in, the Destination$ Library placement
// (absent LibraryPosition$ = TOP in answer order; Shuffle$ True randomises instead)
// and the unread-parameter list -- are handMoveOwnersWalk's, which this
// delegates to with the one-owner, chooser==owner, no-random configuration.
// Only a literal ChangeNum$ (or its absent default 1) reaches here: the
// routing in effChangeZone Notes a non-literal before this is ever called.
func forgetOtherRemembered(h Host, c *Ctx, sa *cards.SA) {
	forgetOther(h, c, forgetOtherRememberedParam(sa))
}

// forgetOther is forgetOtherRemembered over a compiled ForgetOtherRemembered$.
func forgetOther(h Host, c *Ctx, forget bool) {
	if forget && !c.ForgetOtherCleared {
		c.Remembered = nil
		clearEventRemembered(h, c)
		if c.ForgetOtherReady {
			c.ForgetOtherCleared = true
		}
	}
}

// A walk that re-runs its candidate filter across an ask must match every
// owner's candidates against the memory from BEFORE the first move. The
// actual remembered list is still cleared at the first move and rebuilt by
// events; this snapshot is only a filter input. It rides the owner cursor
// across asks, including the answered owner's recheck. minOwners is the
// walk's own continuation shape: 2 for a walk whose filter is only read for
// owners AFTER an answered ask, 1 for a walk whose answered owner's filter
// re-runs on re-entry (effDigUntil's re-scan). A walk whose answered
// revalidation instead reads the ask's ResumeRemembered ride never arms it.
func initForgetOtherSnapshot(h Host, c *Ctx, sa *cards.SA, owners []state.PlayerID, minOwners int) {
	initForgetOther(h, c, forgetOtherRememberedParam(sa), owners, minOwners)
}

// initForgetOther is initForgetOtherSnapshot over a compiled
// ForgetOtherRemembered$.
func initForgetOther(h Host, c *Ctx, forget bool, owners []state.PlayerID, minOwners int) {
	if len(owners) < minOwners || c.ForgetOtherReady || !forget {
		return
	}
	c.ForgetOtherReady = true
	c.ForgetOtherOwners = append([]state.PlayerID(nil), owners...)
	c.ForgetOtherSnapshot = append([]state.Target(nil), c.Remembered...)
	if src := h.Game().Obj(c.Source); src != nil {
		c.ForgetOtherSnapshot = append(c.ForgetOtherSnapshot, src.Remembered...)
	}
}

func endForgetOtherSnapshot(c *Ctx) {
	c.ForgetOtherSnapshot = nil
	c.ForgetOtherOwners = nil
	c.ForgetOtherReady, c.ForgetOtherCleared = false, false
}

func forgetOtherSpecContext(c *Ctx) SpecContext {
	sc := c.SpecContext(c.Controller)
	if c.ForgetOtherReady {
		sc.Remembered = append(append([]state.Target(nil), sc.Remembered...), c.ForgetOtherSnapshot...)
	}
	return sc
}

// forgetOtherPreClearContext is the ONE spec-context read every
// ForgetOtherRemembered$ walk's candidate filter goes through: before the
// snapshot exists the walk's own pre-clear shallow copy is authoritative
// (the set the first pass matched its options under), and once
// initForgetOtherSnapshot has armed the Ctx the snapshot is authoritative,
// riding every ask so a resumed walk re-matches the pre-clear candidates
// against memory the first move cleared. One read for every affected
// primitive so the two carriers cannot drift.
func forgetOtherPreClearContext(sel, c *Ctx) SpecContext {
	if c.ForgetOtherReady {
		return forgetOtherSpecContext(c)
	}
	return sel.SpecContext(sel.Controller)
}

func effChangeZoneHand(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, to state.Zone) {
	count, _ := handMoveCountOf(h, c, cz)
	handMoveOwnersWalk(h, c, sa, cz, to, []state.PlayerID{c.Controller}, count, false, nil, false)
}

// handMoveCount is the ChangeNum$ bound one hidden-hand walk carries: either
// a fixed bound resolved once for the whole resolution, or the per-owner
// eligible count (ChangeNum$ NumInHand / HandSize -- Forge's "all matching
// cards in that hand" texts: Eradicate, Extirpate, Kotose, Lost Legacy, The
// Great Aurora).
type handMoveCount struct {
	fixed    int32
	perOwner bool
}

// handMoveCountOf classifies a hidden-hand walk's ChangeNum$. Absent reads as
// 1 (Forge's ChangeZoneEffect default). "NumInHand"/"HandSize" are the
// per-owner spellings. A plain integer literal is that literal. An SVar-named
// count (ChangeNum$ X / Y over an SVar: body) or a bare "X" (the paid X,
// CR 107.3i) resolves through the ordinary count evaluator, bound to the
// resolving context. Anything else returns false and the caller is loud (a
// Note) rather than degrading to a silent zero-count no-op.
func handMoveCountOf(h Host, c *Ctx, cz *ChangeZoneParams) (handMoveCount, bool) {
	raw := cz.ChangeNum.Text
	if raw == "" {
		return handMoveCount{fixed: 1}, true
	}
	if strings.EqualFold(raw, "NumInHand") || strings.EqualFold(raw, "HandSize") {
		return handMoveCount{perOwner: true}, true
	}
	if n, ok := handChangeNum(cz); ok {
		return handMoveCount{fixed: n}, true
	}
	resolvable := raw == "X" || strings.HasPrefix(raw, "Count$") ||
		strings.HasPrefix(raw, "Sacrificed$") || strings.HasPrefix(raw, "TriggerCount$") ||
		strings.HasPrefix(raw, "TriggerCountMax$")
	if c != nil && c.SVars != nil {
		if _, exists := c.SVars[raw]; exists {
			resolvable = true
		}
	}
	if !resolvable {
		return handMoveCount{}, false
	}
	n := numText(h, c, cz.ChangeNum, 1)
	if n < 0 {
		n = 0
	}
	return handMoveCount{fixed: n}, true
}

// effChangeZoneHandOwners implements the owner-SELECTED hidden-hand shape
// (rv2b r2): Origin$ Hand with DefinedPlayer$-alone (Kynaios and Tiro's "each
// player may put a land card from their hand onto the battlefield", Braids,
// Conjurer Adept, Mindleech Ghoul) or ValidTgts$-alone naming the players
// whose hand moves (Karn Liberated's "[+4]: Target player exiles a card from
// their hand", Kyoki, Sanity's Eclipse). One chooser ask per hand owner,
// chained across owners through the persisted Ctx.HandMoveTarget cursor --
// the walk restarts on every answer, skips the owners already answered, and
// asks the next one -- exactly effDig's per-target continuation, but with a
// REAL ask for every later owner rather than a deterministic stand-in (the
// hand owners are few and each ask is short). Whose hand and who answers are
// the two selectors this shape carries: the owners come from
// DefinedPlayer$/ValidTgts$, the chooser from Chooser$ (Forge's default is
// the hand owner). Every shape this function cannot model emits a Note and
// moves nothing -- the finding's floor: never a silent no-op.
func effChangeZoneHandOwners(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, to state.Zone) {
	// On a resume of a multi-owner walk, the owner cursor's captured list is
	// authoritative: the first move may have cleared the remembered set the
	// owner selector reads (DefinedPlayer$ RememberedOwner with
	// ForgetOtherRemembered$ and no RememberChanged$), so recomputing here
	// would return no owners and the empty-owner guard below would return
	// before handMoveOwnersWalk can restore the list -- dropping the later
	// owner's already-answered move. handMoveOwnersWalk's own entry restores
	// the same list; this restores it early enough to survive the guards.
	owners, ok := c.ForgetOtherOwners, true
	if !c.ForgetOtherReady {
		owners, ok = handMoveOwners(h, c, sa, cz)
	}
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "cannot resolve the hand owner (DefinedPlayer$ " + cz.DefinedPlayer.Text +
				"); no hand card moves"})
		return
	}
	if len(owners) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "the hand owner selector names no player this engine can resolve; no hand card moves"})
		return
	}
	count, ok := handMoveCountOf(h, c, cz)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "cannot choose ChangeNum$ " + cz.ChangeNum.Text +
				" cards from a selected hand (a count this engine cannot evaluate)"})
		return
	}
	choosers := make([]state.PlayerID, len(owners))
	for i, owner := range owners {
		ch, ok := handMoveChooserFor(h, c, cz, owner)
		if !ok {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "Chooser$ " + cz.Chooser +
					" is not a chooser this engine can resolve; no hand card moves"})
			return
		}
		choosers[i] = ch
	}
	random := cz.AtRandom
	handMoveOwnersWalk(h, c, sa, cz, to, owners, count, random, func(_ Host, _ *Ctx, _ *cards.SA, owner state.PlayerID) (state.PlayerID, bool) {
		return choosers[ownerIndex(owners, owner)], true
	}, true)
}

// ownerIndex is the position of owner in owners (owners is small and built
// without duplicates).
func ownerIndex(owners []state.PlayerID, owner state.PlayerID) int {
	for i, p := range owners {
		if p == owner {
			return i
		}
	}
	return 0
}

// handMoveOwners resolves whose hands an owner-selected hidden-hand ChangeZone
// moves from. DefinedPlayer$ takes precedence and resolves through the same
// deterministic selector grammar the library search uses (searchPlayers); a
// ValidTgts$-alone line's chosen targets are the hand owners. It fails
// CLOSED: a player spec this build does not model returns ok=false and the
// caller emits its loud Note -- degrading an unmodelled selector to the
// resolving controller's hand would move (and reveal) cards from the WRONG
// player's hidden hand, which is worse than moving none.
func handMoveOwners(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams) ([]state.PlayerID, bool) {
	if spec := cz.DefinedPlayer.Text; spec != "" {
		if _, modelled := definedSpec(h, c, spec); !modelled {
			return nil, false
		}
		return searchPlayersFor(h, c, cz.fetch()), true
	}
	if plainRememberedSelector(cz.Defined) {
		// A remembered PLAYER is a legitimate hand owner; the plain family no
		// longer drops it just because a remembered CARD coexists in the set.
		return definedPlayers(h, c, sa), true
	}
	// ValidTgts$-alone: Defined's own rule names the chosen targets.
	owners := make([]state.PlayerID, 0, len(c.Targets))
	seen := make(map[state.PlayerID]bool, len(c.Targets))
	for _, t := range Defined(h, c, sa) {
		if !t.IsPlayer {
			// A hand-ownership selector that resolved to a card is not a shape
			// this chooser can honour.
			return nil, false
		}
		p := PlayerOf(h, c, t)
		if int(p) >= len(h.Game().Players) || seen[p] {
			continue
		}
		seen[p] = true
		owners = append(owners, p)
	}
	return owners, true
}

// handMoveChooserFor resolves who answers one owner's hidden-hand ask.
// Forge's default for the shape is the hand owner (Kynaios and Tiro's "each
// player may put", Mindleech Ghoul's "defending player exiles a card from
// their hand"); Chooser$ You is the caster picking out of another player's
// hand (Kitesail Freebooter, Witness the End), Chooser$ Targeted the chosen
// target (Karn Liberated), and the TriggeredTarget/TriggeredPlayer spellings
// the causing event's bound player (Kheru Mind Eater, Widespread Panic). An
// unmodelled value fails closed (ok=false) so the caller is loud rather than
// handing the ask to a guessed seat.
func handMoveChooserFor(h Host, c *Ctx, cz *ChangeZoneParams, owner state.PlayerID) (state.PlayerID, bool) {
	switch cz.handChooser {
	case handChooserOwner:
		return owner, true
	case handChooserYou:
		return c.Controller, true
	case handChooserTargeted:
		if len(c.Targets) > 0 {
			return PlayerOf(h, c, c.Targets[0]), true
		}
		return owner, true
	case handChooserTriggeredTarget:
		if c.TriggerTarget.IsPlayer {
			return c.TriggerTarget.Player, true
		}
		if c.TriggerTarget.Obj != 0 {
			if o := h.Game().Obj(c.TriggerTarget.Obj); o != nil {
				return o.Controller, true
			}
		}
		return owner, true
	case handChooserTriggeredPlayer:
		if c.TriggerPlayer.IsPlayer {
			return c.TriggerPlayer.Player, true
		}
		return owner, true
	case handChooserChosenPlayer:
		// The chosen player, resolved through the SAME shared read
		// searchChooser/hiddenPickChooser use. With none bound or the seat
		// gone, fail closed (never fall to the hand owner: a hidden-hand
		// move from the wrong seat is worse than moving none).
		if p, ok := chooserChosenPlayer(h, c); ok {
			return p, true
		}
		return owner, false
	}
	return owner, false
}

// handMoveOwnersWalk is the ONE hidden-hand mover both shapes share: for each
// owner in order the eligible pool is that owner's hand filtered through
// ChangeType$ (absent: the whole hand), the bound is the count (perOwner:
// that pool's own size), and the pick is one of three shapes -- the chained
// ask (STRICTLY more eligible cards than the bound: a real KChoose to the
// chooser, Min 0 when the take is optional else the bound, Max the bound, the
// answer re-entering through ResumeKind "hand_move" with ResumeTarget
// binding it to this owner), the no-choice deterministic take (a REQUIRED
// move with eligible <= bound: every eligible card moves, in hand order, no
// ask), or AtRandom$'s engine-random pick (no ask: randomness, not a player
// choice, picks). An OPTIONAL move takes the choice path whenever there is
// at least one eligible card, including eligible <= bound: declining remains
// a meaningful answer even when taking every card is the only nonempty pick
// (an empty-only ChangeNum$ 0 still resolves through AskEmpty). The re-entry
// contract (fx42 scoping): Ctx.HandMove/HandMoveDone/HandMoveTarget are captured and
// cleared at the top of the walk; owners before the cursor completed before a
// later owner suspended and are skipped, the cursor's owner consumes the
// answer (moved exactly as answered, revalidated against the CURRENT hand
// and filter), and owners after it continue the chain.
//
// The whole-hand shape (handmove1/rv2b r1) is the one-owner case of this
// walk, with the chooser == the owner -- the r1 contracts (ask shape, card
// text/explicit-marker optionality, R-9 stand-in text, zero-eligible silence,
// library tail) are this walk's contracts, unchanged. Still unread here, each
// a scoped-out follow-up:
// Destination$ Hand/Sideboard oddities (2 lines), and any ConditionPresent$/
// ConditionDefined$ gate (the engine-wide Condition* gap).
func handMoveOwnersWalk(h Host, c *Ctx, sa *cards.SA, cz *ChangeZoneParams, to state.Zone, owners []state.PlayerID,
	count handMoveCount, random bool, chooserFor func(Host, *Ctx, *cards.SA, state.PlayerID) (state.PlayerID, bool),
	eventPlayer bool) {
	spec := cz.ChangeType // "Card" when absent: the whole hand (Brainstorm, Jace's [0], Sawtooth Loon)
	// A leading `Permanent` base in a HAND-origin move must read Forge's
	// "permanent CARD" (nta1): every candidate here is a card in a hand, so
	// the shared matcher's on-the-battlefield base reading (matchesBase)
	// can never be what the script meant -- `ChangeType$ Permanent...` from
	// hand matched NOTHING and the whole walk was a silent no-op (Kodama of
	// the East Tree's ETB rider, Kona Rescue Beastie, Mind into Matter; 29
	// raw corpus lines on an exact `Origin$ Hand`). The rewrite is the same
	// zone-aware normalizer the Dig windows (permanentCardSpec) and
	// rules/stack.go's targetSpecForZone already apply -- one leading token,
	// every qualifier riding along -- and it is safe for ALL of this
	// function's callers (whole-hand, owner-selected, random) because a
	// hand move has no on-battlefield candidates to mis-read.
	spec = permanentCardSpec(spec)
	// The per-type groups an EACH ChangeType asks for, computed once: the
	// sub-specs are a property of the SA, not of the hand owner.
	eachSubs, isEach := eachAlternatives(spec)
	if c.ForgetOtherReady {
		owners = c.ForgetOtherOwners
	}
	g := h.Game()
	// fx42 scoping: capture and clear the answered pick (and the cursor that
	// binds it to the owner that asked) BEFORE anything else, so a nested
	// hand-move ask below cannot inherit them.
	ans := c.HandMove
	done := c.HandMoveDone
	cursor := c.HandMoveTarget
	c.HandMove, c.HandMoveDone, c.HandMoveTarget = nil, false, 0
	// fx42 scoping for the Optional$ confirmation answer: consumed and cleared
	// before anything else so a nested hand move poses its own confirmation.
	confirmDone := c.HandMoveConfirmDone
	confirmYes := strings.EqualFold(c.HandMoveConfirm, "yes")
	confirmTarget := c.HandMoveConfirmTarget
	c.HandMoveConfirm, c.HandMoveConfirmDone, c.HandMoveConfirmTarget = "", false, 0
	withKind := cz.WithCountersType
	var withAmt int32
	if withKind != "" && counterDestination(to) {
		withAmt = withCounterAmount(h, c, cz)
	}
	// settleHandMove settles one chosen card: exactly the shared ChangeZone
	// mover; on the owner-SELECTED shapes the Move event also carries the
	// hand's OWNER as its Player (a hidden-zone move of another player's
	// card -- the same attribution the library search's move carries),
	// while the whole-hand shape keeps its historical event shape
	// (eventPlayer false, the r1 golden contract). settleChangeZoneMoveAs's
	// tail is also the one Tapped$ True entry-state emitter for every
	// hand-origin mover, so concrete Defined$ objects and future hand-owner
	// selectors cannot silently miss the tapped entry.
	rider := classifyAttackingEntryText(c, cz.Riders.Attacking, to)
	// The pre-clear remembered snapshot rides the walk's asks: owner B's
	// candidates must still match the set the walk started with after
	// owner A's settle cleared both halves of the remembered state. It is
	// armed for a single owner too now that the walk clears BEFORE its first
	// ask (an accepted Optional$ confirmation or a mandatory entry), so the
	// answered re-entry's eligibility filter still sees the pre-clear set.
	initForgetOther(h, c, cz.Riders.ForgetOtherRemembered, owners, 1)
	// Snapshot eligibility before forgetting: an IsRemembered filter must
	// still admit an answered card after the old set has been cleared.
	eligibleByOwner := make([][]state.ObjID, len(owners))
	for i, owner := range owners {
		for _, id := range zoneOf(g, state.ZHand, owner) {
			if MatchesSpecCtx(g, spec, id, forgetOtherSpecContext(c)) {
				eligibleByOwner[i] = append(eligibleByOwner[i], id)
			}
		}
	}
	forgot := false
	settleHandMove := func(id state.ObjID, owner state.PlayerID) {
		if !forgot {
			forgetOther(h, c, cz.Riders.ForgetOtherRemembered)
			forgot = true
		}
		settleChangeZoneMoveAs(h, c, sa, cz, id, state.ZHand, to, withKind, withAmt, owner, eventPlayer, &rider)
		if cz.RememberChanged {
			eventRemember(h, c, id)
		}
	}
	// applyAnswered moves one owner's answered pick: the answer re-entry's
	// branch, and the resolution kernel's served answer alike.
	applyAnswered := func(owner state.PlayerID, hand, eligible, ans []state.ObjID) {
		// Move exactly the answered cards that still sit in THIS owner's hand
		// and still match the filter (a stray answer must not move an object
		// that left the hand meanwhile), in the player's answer order.
		// Reaching here at all means the fetch was ENTERED -- a mandatory
		// move, or an accepted Optional$ confirmation -- so the event-backed
		// memory is cleared exactly once here, before the answered cards are
		// settled, even when the answer picked nothing (Forge clears at
		// ChangeZoneEffect.changeHiddenOriginResolve 1103 before the choose,
		// so an accepted search that finds nothing still clears).
		forgetOther(h, c, cz.Riders.ForgetOtherRemembered)
		var moved []state.ObjID
		for _, id := range ans {
			if !containsID(hand, id) {
				continue
			}
			o := g.Obj(id)
			if o == nil || o.Zone != state.ZHand {
				continue
			}
			if !containsID(eligible, id) {
				continue
			}
			settleHandMove(id, owner)
			moved = append(moved, id)
		}
		handLibraryTail(h, g, cz, c.Source, owner, moved, to)
		// AtEOT$ on the hand walk: the owner's answered batch is the
		// affected set, scheduled per owner BEFORE the walk can suspend on a
		// later owner's ask (a suspension must not lose this batch's
		// registrations -- the re-entry skips already-answered owners and
		// never re-schedules them).
		scheduleAtEOT(h, c, sa, moved)
	}
	for i, owner := range owners {
		hand := zoneOf(g, state.ZHand, owner)
		eligible := eligibleByOwner[i]
		if done && i < cursor {
			// This owner answered on an earlier pass, before a later owner
			// suspended the walk. Re-running it could move a second batch, so
			// skip it (effDig's per-target continuation contract).
			continue
		}
		if done && i == cursor {
			applyAnswered(owner, hand, eligible, ans)
			continue
		}
		n := count.fixed
		if count.perOwner {
			n = int32(len(eligible))
		}
		var moved []state.ObjID
		// The chooser is a property of the SA and the owner, not of the
		// eligible pool, so it is resolved once here for both the Optional$
		// confirmation and the card pick below.
		chooser := owner
		if chooserFor != nil {
			chooser, _ = chooserFor(h, c, sa, owner)
		}
		if random {
			// AtRandom$ True: the engine picks, not a player -- ChangeNum$
			// random distinct eligible cards (corpus: always 1, mandatory),
			// through the seeded generator, so the pick replays. No player can
			// decline an engine pick, so the fetch is entered unconditionally
			// here and the event-backed memory is cleared even when no card can
			// be drawn.
			forgetOther(h, c, cz.Riders.ForgetOtherRemembered)
			if len(eligible) == 0 || n == 0 {
				continue
			}
			pool := append([]state.ObjID(nil), eligible...)
			for k := int32(0); k < n && len(pool) > 0; k++ {
				j := h.Rand(len(pool))
				settleHandMove(pool[j], owner)
				moved = append(moved, pool[j])
				pool = append(pool[:j], pool[j+1:]...)
			}
			handLibraryTail(h, g, cz, c.Source, owner, moved, to)
			scheduleAtEOT(h, c, sa, moved)
			continue
		}
		// Forge's Optional$ confirmation (ChangeZoneEffect's confirmAction gate,
		// which runs BEFORE the card pick): a script that carries the marker asks
		// the decider whether to proceed. A decline skips this owner with the
		// remembered set intact; only an accepted confirmation enters the fetch.
		// The markerless may-shapes handTakeOptional recognises stay
		// confirmation-free: Forge expresses their may as a null pick, not as a
		// confirmation, so their empty answer is an accepted fetch that clears.
		if handMoveConfirms(cz) {
			if confirmDone && i < confirmTarget {
				// This owner's confirmation was already answered on an earlier
				// pass (declined, or accepted with its pick completed); do not
				// ask again and do not clear for it.
				continue
			}
			if !confirmDone {
				prompt := cz.OptionalPrompt
				if prompt == "" {
					prompt = "Proceed with moving a card from hand?"
				}
				cd := &decision.Decision{Player: chooser, Kind: decision.KChoose,
					Min: 1, Max: 1, Source: c.Source,
					ResumeKind: "hand_move_confirm", ResumeSA: sa, ResumeTarget: i,
					ResumeRemembered:          copyTargets(c.Remembered),
					ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
					ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
					ResumeForgetOtherReady:    c.ForgetOtherReady,
					ResumeForgetOtherCleared:  c.ForgetOtherCleared,
					Prompt:                    prompt,
					Options: []decision.Option{
						{Index: 0, Kind: "yes", Label: "Yes", Player: chooser},
						{Index: 1, Kind: "no", Label: "No", Player: chooser},
					}}
				if ans, ok := AskTape(h, cd); ok {
					// The resolution kernel's answer in hand: the
					// "hand_move_confirm" re-entry's own events, then a
					// decline skips this owner and an acceptance enters the
					// fetch.
					handMoveReentryEcho(h, c, cz, to)
					if !tapeAnswerYes(ans) {
						continue
					}
				} else if Ask(h, cd) == AskAsked {
					return
				}
				// R-9: no host to ask -- play "may" as "do" deterministically,
				// the same fallback moveDefinedLibraryObjects applies.
			} else if i == confirmTarget && !confirmYes {
				confirmDone = false // this owner's decline is consumed; later owners still confirm
				continue            // declined: keep the remembered set
			} else if i == confirmTarget {
				confirmDone = false // this owner's acceptance is consumed
			}
		}
		if len(eligible) == 0 || n == 0 {
			// No eligible card, or an empty-only ChangeNum$ 0 choice. An
			// ordinary (non-ForgetOther) empty hand move completes silently
			// before optionality is read -- AskEmpty's contract. A
			// ForgetOtherRemembered$ fetch that is entered (mandatory, a
			// markerless may-shape, or an accepted Optional$ confirmation)
			// clears its remembered set: Forge clears before the choose and
			// does not require a nonempty fetchList.
			if cz.Riders.ForgetOtherRemembered {
				forgetOther(h, c, true)
			}
			continue
		}
		// NumInHand/HandSize means "all matching cards in that hand", an
		// intrinsically required all-cards move (Eradicate, Extirpate, The
		// Great Aurora). Its count semantics settle optionality even when the
		// script has neither marker nor explanatory text; preserve an explicit
		// Optional$ marker should a future script carry one.
		intrinsicAll := count.perOwner && !cz.OptionalPresent && !cz.MandatoryPresent
		optional, optionalKnown := handTakeOptional(h, c, cz, to)
		if intrinsicAll {
			optional, optionalKnown = false, true
		}
		if !optionalKnown {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "cannot determine whether the markerless hand move is optional; no hand card moves"})
			return
		}
		// The per-type pick structure (each1's object-path fix): an EACH hand
		// move is one move of EACH listed type, never a flat count over the
		// union -- Michelangelo Improvisers' "EACH Creature & Land" must be
		// able to move a creature AND a land, not one card from the union. The
		// candidates join the FIRST sub-spec that matches them
		// (EachTypeGroups), so a card matching two listed qualities is offered
		// once, in one Group, and picking it cannot block the other type's
		// pick; per-type ChangeNum$ rides Decision.GroupLimit when it is above
		// 1. AtRandom$ and the all-matching perOwner shapes keep their flat
		// walks (measured-absent with EACH; the count semantics are their own).
		structured := isEach && !random && !count.perOwner
		var eachGroups [][]state.ObjID
		var eachPerType int32
		var ceiling int
		if structured {
			eachPerType = n
			eachGroups = EachTypeGroups(g, eachSubs, eligible, c.SpecContext(c.Controller))
			for _, ids := range eachGroups {
				k := int32(len(ids))
				if k > eachPerType {
					k = eachPerType
				}
				ceiling += int(k)
			}
			if !optional && ceiling == len(eligible) {
				// A required structured move with no selection alternative: every
				// group's pool fits its per-type count, so the only legal answer
				// takes all of eligible -- the same deterministic take-all the
				// flat shape takes below, in the same (hand) order.
				for _, id := range eligible {
					settleHandMove(id, owner)
					moved = append(moved, id)
				}
				handLibraryTail(h, g, cz, c.Source, owner, moved, to)
				scheduleAtEOT(h, c, sa, moved)
				continue
			}
		}
		if !structured && int32(len(eligible)) <= n && !optional {
			// A required move with no possible nonempty selection alternative
			// takes every eligible card deterministically. An OPTIONAL move
			// must still ask here: declining is a distinct, legal answer even
			// when every nonempty answer takes all eligible cards.
			for _, id := range eligible {
				settleHandMove(id, owner)
				moved = append(moved, id)
			}
			handLibraryTail(h, g, cz, c.Source, owner, moved, to)
			scheduleAtEOT(h, c, sa, moved)
			continue
		}
		min := int(n)
		if optional {
			min = 0 // "you may put": none is a legal answer
		}
		d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
			Min: min, Max: int(n), Source: c.Source,
			ResumeKind: "hand_move", ResumeSA: sa, ResumeTarget: i,
			// The re-entered walk revalidates the answered cards against the
			// SAME filter it offered them under (Card.IsRemembered in Vizkopa
			// Confessor's PickOne, whose remembered population is ctx-level
			// only -- RememberRevealed$), so the ask must RIDE that set the way
			// every other mid-resolution ask boundary does (attach.go,
			// counters.go, play.go): without it the rebuild loses the ctx-level
			// Remembered and the revalidation re-eligible-matches nothing.
			// The ForgetOtherRemembered$ pre-clear snapshot rides with it: a
			// LATER owner's pool (and any answered revalidation after an
			// earlier owner's settle cleared the live set) still reads the
			// candidates the walk started with.
			ResumeRemembered:          copyTargets(c.Remembered),
			ResumeForgetOtherSnapshot: copyTargets(c.ForgetOtherSnapshot),
			ResumeForgetOtherOwners:   append([]state.PlayerID(nil), c.ForgetOtherOwners...),
			ResumeForgetOtherReady:    c.ForgetOtherReady,
			ResumeForgetOtherCleared:  c.ForgetOtherCleared,
			Prompt:                    handMovePromptFor(cz, to, int(n), chooser == owner)}
		for _, id := range eligible {
			name := "a card"
			if o := g.Obj(id); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "hand_move", Label: name, Obj: id, Player: owner})
		}
		if structured {
			// Replace the flat range and option list with the per-type one: the
			// ceiling (each group's min(perType, size) summed) bounds the ask,
			// a required move demands it whole, and one option per candidate
			// carries its group's ordinal. The flat loop above has already
			// appended the union options -- rebuild from scratch.
			d.Min = 0
			d.Max = 0
			d.Options = nil
			if !optional {
				d.Min = ceiling
			}
			d.Max = ceiling
			if eachPerType > 1 {
				d.GroupLimit = int(eachPerType)
			}
			eachStructuredOptions(g, d, eachGroups, eachPerType, false, owner, "hand_move")
		}
		// The iteration is entered (a mandatory move, a markerless may-shape, or
		// an accepted Optional$ confirmation): Forge clears the source's
		// remembered cards here, before the choose, so this applies even when
		// the answer picks nothing. The decision above already captured the
		// pre-clear Remembered, so the resumed revalidation keeps matching.
		forgetOther(h, c, cz.Riders.ForgetOtherRemembered)
		// The shared ask boundary (effects.Ask): a ChangeNum$ 0 pick over a
		// nonempty eligible hand is Min == Max == 0 -- the empty-answer-only
		// shape -- so it is never posted; AskEmpty resolves silently through
		// the stand-in below, which moves zero cards.
		if ans, ok := AskTape(h, d); ok {
			// The resolution kernel's answer in hand: the "hand_move"
			// re-entry's own events, then its answered branch over the hand
			// as it stands.
			handMoveReentryEcho(h, c, cz, to)
			applyAnswered(owner, zoneOf(g, state.ZHand, owner), eligible, tapeAnswerObjs(ans))
			continue
		}
		oc := Ask(h, d)
		if oc == AskAsked {
			return // resolution suspended; the answer re-enters with Ctx.HandMove set.
		}
		// R-9: a host without a decision channel cannot ask a player, so it
		// supplies the deterministic answer in the player's place -- the first
		// ChangeNum eligible cards in the same ordered eligible list the
		// decision's options were built from.
		if oc == AskNoHost {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: chooser,
				Text: "moves the first matching card(s) from hand (no engine host to ask)"})
		}
		if structured {
			// The structured stand-in: each group's first perType candidates, in
			// group order -- the exact take the bot's group-aware fill
			// re-derives, and the per-type mirror of the flat first-n take.
			for _, ids := range eachGroups {
				k := eachPerType
				if int32(len(ids)) < k {
					k = int32(len(ids))
				}
				for _, id := range ids[:k] {
					settleHandMove(id, owner)
					moved = append(moved, id)
				}
			}
		} else {
			for k := int32(0); k < n && int(k) < len(eligible); k++ {
				settleHandMove(eligible[k], owner)
				moved = append(moved, eligible[k])
			}
		}
		handLibraryTail(h, g, cz, c.Source, owner, moved, to)
		scheduleAtEOT(h, c, sa, moved)
	}
	// The walk completed: release the ride. A later ability in the same
	// chain must not inherit this walk's snapshot (the same boundary the
	// search and hidden walks end at).
	endForgetOtherSnapshot(c)
}

// optionalConfirmMarker is the ONE reader of Forge's explicit Optional$
// confirmation marker (the confirmAction gate in
// ChangeZoneEffect.changeHiddenOriginResolve, which runs before any card is
// picked and whose decline does not clear the source's remembered cards).
// Only a positive marker asks the decider: Optional$ True, or the older
// Optional$ You spelling Cauldron Dance carries. An explicit Optional$ False
// and an absent marker pose no confirmation. The markerless may-shapes
// handTakeOptional recognises from card text stay confirmation-free -- Forge
// expresses their may as a null pick, not as a confirmation -- so an empty
// answer there is an accepted fetch that clears. ChoiceOptional$ is
// deliberately NOT this marker: it names the pick's own cardinality (the
// Min-0 may-pick default), not a yes/no gate.
func optionalConfirmMarker(cz *ChangeZoneParams) bool {
	return cz.OptionalYes
}

// handMoveConfirms reports whether a hidden-hand ChangeZone poses Forge's
// Optional$ confirmation before its card pick. The ordinary (non-ForgetOther)
// Optional$ fetch confirms exactly like the ForgetOtherRemembered$ one: the
// decline-vs-accept split is observable in the ask sequence itself (a decline
// poses no card pick at all, where an accepted Min-0 pick would), which is
// Forge's confirm-before-pick order in changeHiddenOriginResolve.
func handMoveConfirms(cz *ChangeZoneParams) bool {
	return optionalConfirmMarker(cz)
}

// hiddenPickConfirms reports whether a Hidden$ True public-origin ChangeZone
// pick poses Forge's Optional$ confirmation before its pick: the same
// optionalConfirmMarker the hidden-hand walk reads, for the same
// confirmAction gate. effHiddenPick poses it per fetch player, including when
// that player's eligible pool turns out empty -- a decline skips the player
// with the remembered set intact, and only an accepted confirmation reaches
// the pick or the empty-pool continuation that clears.
func hiddenPickConfirms(cz *ChangeZoneParams) bool {
	return optionalConfirmMarker(cz)
}

// handTakeOptional reads Forge's optional-vs-mandatory markers for a
// hidden-origin hand move. A missing marker is NOT an optional default:
// Volrath's Dungeon is markerless but requires its target to put a card back.
// Forge does not encode that distinction in ChangeZone's parameters, so the
// markerless may-shapes are recognised from their card/script text (Burgeoning,
// Oviya, Volcanic Spite); text we cannot classify fails closed and loudly at
// the caller rather than granting an invented decline.
func handTakeOptional(h Host, c *Ctx, cz *ChangeZoneParams, to state.Zone) (optional, known bool) {
	if cz.Mandatory {
		return false, true
	}
	if cz.OptionalPresent {
		return cz.OptionalYes, true
	}
	text := cz.SpellDescription
	if o := h.Game().Obj(c.Source); o != nil && o.Face() != nil {
		text += "\n" + o.Face().Oracle
	}
	if strings.TrimSpace(text) == "" {
		return false, false
	}
	return handMoveTextOptional(text, to)
}

// handMoveTextOptional recognises the actual English may-forms for the one
// ChangeZone move being resolved. "Put any number" is optional even without
// the word may. It requires the destination's action verb and a hand
// reference IN THE SAME sentence: an unrelated "may put" elsewhere on a
// multi-ability card is not evidence that this move may be declined.
// Conversely, text that lacks a matching action is unknown, so the caller
// emits its fail-closed Note.
func handMoveTextOptional(text string, to state.Zone) (optional, known bool) {
	text = strings.ToLower(text)
	var action string
	switch to {
	case state.ZBattlefield, state.ZLibrary:
		action = "put"
	case state.ZExile:
		action = "exile"
	case state.ZGraveyard:
		action = "discard"
	case state.ZHand:
		action = "return"
	default:
		return false, false
	}
	if handMovePhraseMentionsHand(text, "may "+action) ||
		handMovePhraseFollowsHandReveal(text, "may "+action) ||
		(to == state.ZLibrary && handMovePhraseMentionsHand(text, "may shuffle")) ||
		(handMovePhraseMentionsHand(text, "any number") && handMovePhraseMentionsHand(text, action)) {
		return true, true
	}
	if handMovePhraseSupportsRequiredMove(text, action) ||
		handMovePhraseFollowsHandChoice(text, action) ||
		(to == state.ZLibrary && handMovePhraseSupportsRequiredMove(text, "shuffle")) {
		return false, true
	}
	return false, false
}

// handMovePhraseSupportsRequiredMove also accepts "choose" in the action's
// sentence: a preceding RevealHand can make the later "You choose ... and
// exile that card" sentence omit the word hand (Thought-Knot Seer), but it is
// still an unambiguously required chooser action.
func handMovePhraseSupportsRequiredMove(text, phrase string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		begin := 0
		if j := strings.LastIndexAny(text[:i], ".;"); j >= 0 {
			begin = j + 1
		}
		end := len(text)
		if j := strings.IndexAny(text[i:], ".;"); j >= 0 {
			end = i + j
		}
		if strings.Contains(text[begin:end], "hand") || strings.Contains(text[begin:end], "choose") {
			return true
		}
		start = i + len(phrase)
	}
}

// handMovePhraseFollowsHandChoice recognises the same hidden-hand sequence
// when Forge split its selection and movement into sentences: "reveal their
// hand. You choose a card from it. Exile that card" (Kitesail Freebooter).
// It scans only the action sentence and its three predecessors, all of which
// must establish the hand -> choice -> pronoun chain.
func handMovePhraseFollowsHandChoice(text, phrase string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		begin := 0
		if j := strings.LastIndexAny(text[:i], ".;"); j >= 0 {
			begin = j + 1
		}
		end := len(text)
		if j := strings.IndexAny(text[i:], ".;"); j >= 0 {
			end = i + j
		}
		contextStart := begin
		for n := 0; n < 3 && contextStart > 0; n++ {
			prior := strings.TrimRight(text[:contextStart], ".; ")
			if j := strings.LastIndexAny(prior, ".;"); j >= 0 {
				contextStart = j + 1
			} else {
				contextStart = 0
			}
		}
		context := text[contextStart:end]
		if (strings.Contains(text[begin:end], "that card") || strings.Contains(text[begin:end], " it")) &&
			strings.Contains(context, "hand") && strings.Contains(context, "choose") && strings.Contains(context, "it") {
			return true
		}
		start = i + len(phrase)
	}
}

// handMovePhraseFollowsHandReveal recognises the usual two-sentence hidden
// hand wording: "Target player reveals their hand. You may put ... from it."
// The pronoun is enough only immediately after a hand-reveal sentence, so an
// unrelated optional action elsewhere cannot make this move optional.
func handMovePhraseFollowsHandReveal(text, phrase string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		begin := 0
		if j := strings.LastIndexAny(text[:i], ".;"); j >= 0 {
			begin = j + 1
		}
		end := len(text)
		if j := strings.IndexAny(text[i:], ".;"); j >= 0 {
			end = i + j
		}
		prior := strings.TrimRight(text[:begin], ".; ")
		if j := strings.LastIndexAny(prior, ".;"); j >= 0 {
			prior = prior[j+1:]
		}
		if strings.Contains(prior, "hand") && strings.Contains(text[begin:end], "it") {
			return true
		}
		start = i + len(phrase)
	}
}

// handMovePhraseMentionsHand keeps text classification local to the sentence
// carrying a candidate action. Forge's Oracle text uses periods for sentence
// boundaries; semicolons also separate instructions often enough to be a safe
// boundary here.
func handMovePhraseMentionsHand(text, phrase string) bool {
	for start := 0; ; {
		i := strings.Index(text[start:], phrase)
		if i < 0 {
			return false
		}
		i += start
		end := len(text)
		if j := strings.IndexAny(text[i:], ".;"); j >= 0 {
			end = i + j
		}
		if strings.Contains(text[i:end], "hand") {
			return true
		}
		start = i + len(phrase)
	}
}

// handMovePrompt builds the human-readable ask text, naming the top/bottom
// placement when the destination is the library (the one destination where
// WHERE matters to the chooser), and whose hand it is when the chooser is
// not the hand's owner (Chooser$ You: the caster picks out of another
// player's hand).
func handMovePromptFor(cz *ChangeZoneParams, to state.Zone, n int, own bool) string {
	dest := handDestPhrase(to)
	if to == state.ZLibrary {
		if cz.LibraryPositionText == "-1" {
			dest = "the bottom of your library"
		} else {
			dest = "the top of your library"
		}
	}
	whose := "your hand"
	if !own {
		whose = "that player's hand"
		if to == state.ZLibrary {
			if cz.LibraryPositionText == "-1" {
				dest = "the bottom of that player's library"
			} else {
				dest = "the top of that player's library"
			}
		}
	}
	return "Choose " + strconv.Itoa(n) + " card(s) from " + whose + ": they move to " + dest
}

// handLibraryTail is the post-move library placement both hidden-origin
// movers end with. A hand put-back only shuffles when its own script says
// so (Shuffle$ True -- Slowtrip), while a library search shuffles by
// default; the placement itself is the shared libraryOrderPlacement helper,
// with Forge's absent-LibraryPosition$ default (TOP) applied for the hand
// path -- Brainstorm and Jace's [0] name no LibraryPosition$ and their
// oracle puts the cards on top.
func handLibraryTail(h Host, _ *state.Game, cz *ChangeZoneParams, source state.ObjID, owner state.PlayerID, moved []state.ObjID, to state.Zone) {
	if to != state.ZLibrary || len(moved) == 0 {
		return
	}
	// DestinationAlternative$/LibraryPositionAlternative$ (Dream Cache's "both
	// on top of your library or both on the bottom", 1 raw line) is a modal
	// destination choice this engine cannot yet ask: the alternative is named
	// in a Note and the primary destination/position is taken
	// deterministically, so the unsupported shape is never silent.
	if alt := cz.DestinationAltText; alt != "" || cz.LibraryPositionAltText != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: source, Player: owner,
			Text: "DestinationAlternative$ " + alt + " is not a choice this engine can ask; the cards take the primary destination"})
	}
	if cz.ShuffleTrue && !cz.NoShuffle {
		shuffleLibraryExplicit(h, cz, owner)
		return // a shuffled library has no meaningful LibraryPosition$
	}
	position := cz.LibraryPositionText
	if position != "" && position != "0" && position != "-1" {
		h.Emit(events.Event{Kind: events.Note, Obj: source, Player: owner,
			Text: "LibraryPosition$ " + position + " is not implemented; the cards go on top"})
	}
	libraryOrderPlacement(h, owner, moved, position == "-1")
}

// handDestPhrase names the hand-move destination in the human-readable
// prompt; its own vocabulary so it cannot drift into digDestPhrase's or
// destinationPhrase's.
func handDestPhrase(to state.Zone) string {
	switch to {
	case state.ZBattlefield:
		return "the battlefield"
	case state.ZGraveyard:
		return "the graveyard"
	case state.ZExile:
		return "exile"
	case state.ZLibrary:
		return "the library"
	case state.ZHand:
		return "the hand"
	default:
		return "its destination"
	}
}

// counterDestination reports whether a ChangeZone destination can carry the
// WithCountersType$/WithCountersAmount$ entry counters. They land on a
// permanent entering the battlefield (the Undying expansion) or on a card
// exiled with them (suspend's TIME counters); a counter on a moved card in any
// other zone is never read by anything, so such a destination must not parse
// the amount (which would emit a malformed-amount Note for a dynamic value)
// and must not emit a CounterChange. Measured at the corpus pin: every
// ChangeZone-family `WithCountersType$` line names exactly these two
// destinations -- Battlefield 99, Exile 38 (137 total) -- so the gate admits
// the whole measured population and nothing else. This is the one gate every
// ChangeZone mover shares (the other APIs that carry the parameter,
// CopyPermanent and Token, read it in their own primitives).
func counterDestination(to state.Zone) bool {
	return to == state.ZBattlefield || to == state.ZExile
}

// withCounterAmount reads WithCountersAmount$ (default 1): a literal, or a
// value the ordinary Num grammar resolves -- an SVar name on the resolving
// face (Nine-Lives Familiar's X over Spawner>TriggeredCard$CardCounters.
// REVIVAL/Minus.1, Ochre Jelly's Y), the same resolution the token path's
// entry counters use. An amount the grammar cannot evaluate must be loud, not
// silently default to 1 (the reviewer's item): a wrong counter count on a
// Returning permanent is a hard-to-spot board-shape bug. A Note event (the way
// Resolve surfaces an unimplemented API) keeps this deterministic and
// replay-log-visible rather than dropping to a log line the event log cannot
// account for. The movement still proceeds with the safe default 1.
//
// The amount is read BEFORE the move, so a body that measures the card this
// very move remembers (Alaundo the Seer's X:Remembered$CardManaCost under
// RememberChanged$ True) has nothing to measure yet and keeps the loud
// default rather than a silent zero.
func withCounterAmount(h Host, c *Ctx, cz *ChangeZoneParams) int32 {
	v := cz.WithCountersAmount.Text
	if v == "" {
		return 1
	}
	if n, err := strconv.Atoi(v); err == nil {
		return int32(n)
	}
	if withCounterAmountDefined(c, v) && !withCounterAmountReadsMoved(c, cz, v) {
		if n, ok := numResolvedStrictText(h, c, cz.WithCountersAmount, 1); ok {
			return n
		}
	}
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "malformed WithCountersAmount " + v})
	return 1
}

// withCounterAmountDefined reports whether v names something the resolving
// face defines: an SVar in the context's table, or an inline expression. A
// bare name with no definition (an X nothing chose) stays loud rather than
// reading the context's zero.
func withCounterAmountDefined(c *Ctx, v string) bool {
	if strings.Contains(v, "$") {
		return true
	}
	if c == nil || c.SVars == nil {
		return false
	}
	_, ok := c.SVars[v]
	return ok
}

// withCounterAmountReadsMoved reports whether the amount v measures the card
// the move itself is about to remember: a Remembered$ body (direct, or behind
// an SVar name) on a RememberChanged$ True line.
func withCounterAmountReadsMoved(c *Ctx, cz *ChangeZoneParams, v string) bool {
	if !cz.RememberChanged {
		return false
	}
	if c != nil && c.SVars != nil {
		if body, ok := c.SVars[v]; ok {
			v = body
		}
	}
	return strings.HasPrefix(strings.TrimSpace(v), "Remembered$")
}

// effSearchLibrary implements the hidden-origin ChangeZone shape. The option
// list is rebuilt deterministically from library order and ChangeType$, while
// the answer is carried only as option indices and object ids through the
// ordinary KChoose/resume mechanism.
//
// zones is the full origin set (Origin$ merged with OriginAlternative$):
// Library is the hidden half, and any public zones in the set (Graveyard,
// Exile, Hand) contribute their owner's matching cards to the SAME option
// list, exactly Forge's choose-a-card-from-any-of-these-zones step. The
// library is searched first in candidate order so a pure-library search's
// option list -- and therefore its chain heads -- is unchanged.
//
// The per-library answer cursor rides the same resume point as every other
// mid-resolution choice. That makes a multi-player search continue after the
// owner whose answer suspended the effect, rather than rebuilding from the
// first owner on every re-entry.
// chooseFromDefinedPool is the ONE resolver for the ChangeZone
// ChooseFromDefined$ selector: the value is a full Defined selector, resolved
// through the shared Defined machinery (knownDefinedTargets) into the
// eligible-object pool every dispatch path bounds its offered set with --
// effHiddenPick's public-origin pick, effSearchLibrary's option list and
// applyLibrarySearch's answer recheck. An unknown, unresolvable or
// player-only result fails CLOSED: ok=false means the caller offers and
// moves NOTHING (plus its loud Note), never the whole origin zone.
func chooseFromDefinedPool(h Host, c *Ctx, raw string) (map[state.ObjID]bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	ts, ok := knownDefinedTargets(h, c, raw)
	if !ok {
		return nil, false
	}
	pool := make(map[state.ObjID]bool, len(ts))
	for _, t := range ts {
		if !t.IsPlayer && t.Obj != 0 {
			pool[t.Obj] = true
		}
	}
	return pool, true
}
