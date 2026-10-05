package events

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// Emit is the engine's only mutation path: append to the log, then fold into
// state. Replay calls Apply directly with logged events, so post-replay state
// equals post-play state by construction.
func Emit(g *state.Game, l *Log, e Event) Event {
	EmitPtr(g, l, &e)
	return e
}

// EmitPtr is Emit without the by-value copies: it appends *e to the log --
// assigning e.Seq and detaching e.IDs/e.Pairs exactly as Append does, so on
// return *e IS the stored event -- and folds it. e must not point into l's own
// Events (Append may reallocate it).
func EmitPtr(g *state.Game, l *Log, e *Event) {
	l.AppendPtr(e)
	ApplyPtr(g, e)
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

// maxQueuedGrants bounds the pending entries ONE ExtraTurn or AddPhase grant
// appends (one per granted turn or phase). An Amount is an arbitrary int32 --
// an X value, or TestApplyNeverPanics' MaxInt32, which queued 2^31 turns and
// allocated ~11 GB -- while a game ends long before a thousand extra turns or
// phases could be taken. Folded here, so live play and replay clamp alike; the
// ExtraTurns count takes the clamped amount too, keeping it equal to the
// queue.
const maxQueuedGrants int32 = 1 << 10

// resetCombat removes permanent(s) from combat (CR 511.3). only == 0 is the
// whole-combat reset EndCombatReset's Obj-zero form and CR 723.1c's "end the
// turn" both need; a nonzero only removes that single permanent (the
// regeneration shape) and leaves a zero tombstone in every attacker's blocker
// list -- the attacker remains blocked (CR 509.1h) while liveBlockers ignores
// the removed blocker, even if it lives. Shared so the two callers cannot
// drift apart.
func resetCombat(g *state.Game, only state.ObjID) {
	for i := range g.Objs {
		o := &g.Objs[i]
		if only == 0 || o.ID == only {
			o.IsAttacking = false
			o.AttackingBattle = 0
			o.BlockedBy = nil
		} else {
			for j, id := range o.BlockedBy {
				if id == only {
					o.BlockedBy[j] = 0
				}
			}
		}
	}
	if only == 0 {
		// Every BlockedBy list is now nil.
		g.ClearBlockers()
	}
}

func Apply(g *state.Game, e Event) { ApplyPtr(g, &e) }

// ApplyPtr is Apply on a pointer: the fold reads *e and never writes it.
func ApplyPtr(g *state.Game, e *Event) {
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
	case SetupEntered:
		// The oracle harness stages battlefield cards before the initial
		// TurnChange. Restore their first-turn entry history after that reset
		// without moving them (which would fire an artificial ETB trigger).
		if o := g.Obj(e.Obj); o != nil && o.Zone == state.ZBattlefield && g.Turn == 1 {
			o.EnteredThisTurn = true
			o.EnteredFrom = state.ZLibrary
			g.Entered = append(g.Entered, state.ZoneEntry{Obj: o.ID, To: state.ZBattlefield,
				From: state.ZLibrary, Owner: o.Owner, PermanentCard: !o.IsToken && !o.IsCopy && o.Card != nil})
		}
	case EndTurn:
		foldEndTurn(g, e)
	case Resolve:
		foldResolve(g, e)
	case Mutate:
		foldMutate(g, e)
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
		foldKeywordAbilityPush(g, e)
	case Discover, Seek, Surveil, Scry, Proliferate, ElementalBend:
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
		// cards put on the bottom. One marker per completed action. The
		// ElementalBend record (task agent-20260929T010346Z-ae55d89d) is
		// the same shape: the bend's own state changes are their own
		// events that surround it, and the record is what
		// trig:ElementalBend matches; its Text carries the verb
		// (water/earth/fire/air) the per-turn all-four ledger folds.
	case Exploit:
		// The exploit record (CR 702.58a, task exploit1) is a pure marker,
		// exactly like Explore/Investigate: the sacrifice's own state change
		// (the battlefield-to-graveyard MoveZone) is its own event that
		// preceded this one, and the record is what trig:Exploited matches.
		// Obj the exploiting creature, Player its controller, IDs[0] the
		// exploited creature. A declined optional sacrifice records nothing.
	case AlterAttribute:
		foldAlterAttribute(g, e)
	case Enlist:
		foldEnlist(g, e)
	case Crew:
		foldCrew(g, e)
	case Connive:
		// The connive record (CR 702.59, task connive1) is a pure marker,
		// exactly like Explore: the connive's own state changes (the draws,
		// the discards, the +1/+1 counters) are their own events that
		// preceded this one, and the record is what trig:Connives matches.
		// Obj the conniving permanent, Player its controller, IDs the
		// discarded cards in discard order, Amount the nonland count among
		// them. One marker per completed connive action.
	case Pair:
		foldPair(g, e)
	case MyriadCopy:
		foldMyriadCopy(g, e)
	case MyriadCleanup:
		foldMyriadCleanup(g, e)
	case TokenAttacks:
		foldTokenAttacks(g, e)
	case Shuffle:
		foldShuffle(g, e)
	case PlanarDeckShuffle:
		foldPlanarDeckShuffle(g, e)
	case PlanarReveal:
		foldPlanarReveal(g, e)
	case PlanarWalk:
		foldPlanarWalk(g, e)
	case DungeonCreate:
		foldDungeonCreate(g, e)
	case DungeonRoom:
		foldDungeonRoom(g, e)
	case DungeonComplete:
		foldDungeonComplete(g, e)
	case DungeonRemove:
		foldDungeonRemove(g, e)
	case ManaUndo:
		foldManaUndo(g, e)
	case ChaosEnsues:
		// The chaos-ensues marker (CR 901.9, task planar-verbs) is a pure
		// marker, exactly like PlanarRoll: no state folds. The current plane's
		// chaos ability is an ordinary triggered ability (Mode$ ChaosEnsues)
		// that rules' trigger walk queues when this marker is checked, so the
		// logged event is the record and replay re-derives the trigger queue.
	case MonarchChange:
		foldMonarchChange(g, e)
	case InitiativeChange:
		foldInitiativeChange(g, e)
	case BlessingChange:
		foldBlessingChange(g, e)
	case EnduringStoryChange:
		foldEnduringStoryChange(g, e)
	case StartingPlayerChange:
		foldStartingPlayerChange(g, e)
	case ControlChange:
		foldControlChange(g, e)
	case Imprint:
		foldImprint(g, e)
	case LibraryOrder:
		foldLibraryOrder(g, e)
	case SkipTurn:
		foldSkipTurn(g, e)
	case ControlPlayerChange:
		foldControlPlayerChange(g, e)
	case ExtraTurn:
		foldExtraTurn(g, e)
	case ExtraPhase:
		foldExtraPhase(g, e)
	case DoorUnlock:
		foldDoorUnlock(g, e)
	case SpeedChange:
		foldSpeedChange(g, e)
	case RingTemptsYou:
		foldRingTemptsYou(g, e)
	case RingEmblemPush:
		foldRingEmblemPush(g, e)
	case MoveZone, Draw, PutOnStack:
		foldMoveZone(g, e)
	case LifeChange:
		foldLifeChange(g, e)
	case Damage:
		foldDamage(g, e)
	case Tap:
		foldTap(g, e)
	case Untap:
		foldUntap(g, e)
	case StepChange:
		foldStepChange(g, e)
	case TurnChange:
		foldTurnChange(g, e)
	case Goad:
		foldGoad(g, e)
	case PlayerCounterChange:
		foldPlayerCounterChange(g, e)
	case Priority:
		foldPriority(g, e)
	case ManaAdd:
		foldManaAdd(g, e)
	case ManaClear:
		foldManaClear(g, e)
	case CounterChange:
		foldCounterChange(g, e)
	case DeclareAttackers:
		foldDeclareAttackers(g, e)
	case DeclareBlockers:
		foldDeclareBlockers(g, e)
	case CombatRetarget:
		foldCombatRetarget(g, e)
	case PlayerLost:
		foldPlayerLost(g, e)
	case GameOver:
		foldGameOver(g, e)
	case LandPlayed:
		foldLandPlayed(g, e)
	case TargetsChosen:
		foldTargetsChosen(g, e)
	case FlipFace, Specialize:
		foldFlipFace(g, e)
	case TurnFaceDown:
		foldTurnFaceDown(g, e)
	case TurnFaceUp:
		foldTurnFaceUp(g, e)
	case PhaseOut:
		foldPhaseOut(g, e)
	case ClockTick:
		foldClockTick(g, e)
	case TriggerPush:
		foldTriggerPush(g, e)
	case EndCombatReset:
		foldEndCombatReset(g, e)
	case CastInfo:
		foldCastInfo(g, e)
	case XChange:
		foldXChange(g, e)
	case GiftPromise:
		foldGiftPromise(g, e)
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
		// tied), already read off the live event by trigmatch.ClashMatches, so Apply
		// stores nothing.
	case NoteNumber:
		foldNoteNumber(g, e)
	case StoreSVar:
		foldStoreSVar(g, e)
	case PlayerNoted:
		foldPlayerNoted(g, e)
	case PlayerNoteCleared:
		foldPlayerNoteCleared(g, e)
	case CardNoted:
		foldCardNoted(g, e)
	case Choose:
		foldChoose(g, e)
	case TokenCreate:
		foldTokenCreate(g, e)
	case CardToken:
		foldCardToken(g, e)
	case CopyToken:
		foldCopyToken(g, e)
	case CloneStatic:
		foldCloneStatic(g, e)
	case ClonePermanent:
		foldClonePermanent(g, e)
	case Exert:
		foldExert(g, e)
	case KeywordTriggerPush:
		foldKeywordTriggerPush(g, e)
	case StackCopy:
		foldStackCopy(g, e)
	case Attach:
		foldAttach(g, e)
	case Unattached:
		foldUnattached(g, e)
	case AbilityPush:
		foldAbilityPush(g, e)
	case DelayedRegister:
		foldDelayedRegister(g, e)
	case DelayedForget:
		foldDelayedForget(g, e)
	case DelayedRemove:
		foldDelayedRemove(g, e)
	case DelayedPush:
		foldDelayedPush(g, e)
	case MergedTriggerPush:
		foldMergedTriggerPush(g, e)
	case GrantTriggerPush:
		foldGrantTriggerPush(g, e)
	case GrantAbilityPush:
		foldGrantAbilityPush(g, e)
	case GainedAbilityPush:
		foldGainedAbilityPush(g, e)
	case GainedTriggerPush:
		foldGainedTriggerPush(g, e)
	case CmdDamage:
		foldCmdDamage(g, e)
	case DamageProvenance:
		foldDamageProvenance(g, e)
	}
}
