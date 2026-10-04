package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// TriggerContext is event provenance, not the resolving ability's Targets or
// Source. Rules captures it when a trigger fires and retains it through stack
// placement and suspension. A Target's IsPlayer bit distinguishes seat zero
// from an absent referent. All fields are zero outside a trigger.
//
// TriggerCard and TriggerPlayer are separate: a zone change's card, a phase's
// active player, a damage recipient and a damage source are not interchangeable.
// Unsupported/ambiguous event roles stay absent rather than guessing.
type TriggerContext struct {
	TriggerTarget state.Target
	TriggerSource state.ObjID
	// TriggerStack is the actual spell/ability object that caused a targeting
	// event. Unlike TriggerSource it is not unwrapped to its source permanent,
	// because Ward must counter that stack object itself.
	TriggerStack    state.ObjID
	DefendingPlayer state.Target
	// DefendingBattle is the planeswalker or battle object a
	// K:Ninjutsu permanent (CR 702.49b) enters attacking, when the returned
	// creature was attacking a non-player defender. It rides beside
	// DefendingPlayer, which remains the defending player (the object's
	// controller) -- the Attacking$ True entry rider emits both, and the
	// TokenAttacks event's IDs[1] slot carries the object. Zero when the
	// defender was a player, which is every non-ninjutsu binding.
	DefendingBattle state.ObjID
	TriggerPlayer   state.Target
	TriggerCard     state.ObjID
	// DelayedObject is the causing event's object for a delayed trigger:
	// the fire-time object TriggeredObject names.
	DelayedObject state.ObjID
	// DelayedRemembered is the set a delayed-trigger REGISTRATION captured
	// (RememberObjects$ at registration time), which DelayTriggerRemembered
	// names. It is carried beside Ctx.Remembered because an event-matched
	// registration's Remembered is the firing EVENT's object -- the same
	// capture a printed trigger of that mode makes -- so the two referents
	// cannot share one slot.
	DelayedRemembered []state.Target
	// Reflexive marks a CR 603.12 reflexive triggered ability ("When you do,
	// ...") minted by effImmediateTrigger through Host.QueueReflexiveTrigger.
	// Its stack object's Remembered is the instance's OWN remembered set --
	// real memory, not a fire-time event capture -- so the resolution binds
	// Ctx.Captured from SpawnerCaptured (the spawning ability's capture, what
	// the Spawner> chain reads) instead of from the object's Remembered.
	Reflexive       bool
	SpawnerCaptured []state.Target
	// OptionalSpec is the OptionalDecider$ spec an api:Effect Triggers$
	// body registered its delayed trigger with (state.DelayedTrigger.
	// OptionalSpec). The registration carries it because a Mode$ Phase
	// body is never re-parsed at fire time; it rides this context to the
	// minted stack object so resolveTop's CR 603.5 optional gate can pose
	// the election the trigger line names, which findTriggerForAbility
	// cannot recover for a delayed Effect body (its Ability is an
	// Execute$ SVar sub-ability, not a face Triggers entry). Empty for
	// every printed trigger and every registration with no election.
	OptionalSpec     string
	AttackingPlayer  state.Target
	AttackedTarget   state.Target
	TriggerActivator state.Target
	// TriggerCardController is the controller the triggering card had as it
	// LEFT the battlefield (CR 603.10a), recorded when the trigger fires and
	// carried with the ability onto the stack. It is absent for every other
	// event: an entering or cast card's controller is its current one.
	TriggerCardController state.Target
	// TriggerMana is the fixed-order WUBRGC set of mana types produced by
	// the mana ability that caused a TapsForMana trigger. ManaReflected's
	// ReflectProperty$ Produced form consumes it; unlike TriggerAmount, it
	// preserves mixed-type production.
	TriggerMana string
	// TriggerAmount is the magnitude the causing event carried -- the Damage
	// event's dealt-damage amount for a DamageDone/DamageDealtOnce trigger,
	// etc. It is what the TriggerCount$ heads (DamageAmount, LifeAmount,
	// Amount) answer: the value has to come from the event that fired the
	// trigger, so it is captured here exactly like the other provenance roles
	// and survives to resolution through the per-stack-instance
	// triggerContexts map. Zero when the causing event carried no amount.
	TriggerAmount int32
	// TriggerResult is the die result a Mode$ RolledDie trigger fired on (the
	// canonical die-roll Note's modified result, effects.DieRollResult). It is
	// what the TriggerCount$Result head answers -- Mr. House's
	// "BranchConditionSVar$ TriggerCount$Result" reads the roll the trigger
	// matched, long after the RollDice resolution that produced it has
	// finished, so like TriggerAmount it is captured at fire time and carried
	// to resolution through the per-stack-instance triggerContexts map. Zero
	// outside a RolledDie trigger; a real roll is always >= 1, so zero is
	// unambiguous absence.
	TriggerResult int32
	// TriggerResultMax is the highest die result in the roll batch a Mode$
	// RolledDieOnce trigger fired on (the canonical batch roll Note's Pairs
	// max, effects.DieRollBatchResult). It is what the TriggerCountMax$Result
	// head answers -- Farideh, Devil's Chosen's "if any of those results was
	// 10 or higher" (ConditionCheckSVar$ DiceResult, SVar:DiceResult:TriggerCountMax$Result),
	// which must survive the roll resolution like TriggerResult. Zero outside
	// a RolledDie/RolledDieOnce trigger; a real result is always >= 1.
	TriggerResultMax int32
	// TriggerPaidX snapshots the paid X of TriggerCard when this trigger
	// matched. CR 107.3m binds that value at trigger time: it must survive if
	// the card later leaves the stack or battlefield before the ability
	// resolves. Zero is both a valid paid value and the value for a triggering
	// card with no paid X.
	TriggerPaidX int32
	// TriggerBearer is the permanent an Aura/Equipment BECAME attached to
	// (rules/triggerReferents' Attached case, over the one shared Attach
	// event: ev.Obj is the attachment, ev.IDs[0] the bearer). It is the
	// exact referent Defined$ TriggeredTargetLKICopy resolves for an
	// Attached execute (Enormous Energy Blade's "tap that creature"). Only
	// the Attached capture sets it, so the spelling's Remembered fallback
	// for every other mode is untouched -- in particular a BecomesTarget
	// trigger's Remembered entry (the targeting spell) stays exactly as it
	// always resolved, and the mode-agnostic TriggerTarget role (which for
	// BecomesTarget is the trigger's own source permanent) is never read
	// through this spelling. Zero outside an Attached trigger.
	TriggerBearer state.ObjID
	// TriggerAbility is the minted ability STACK OBJECT an AbilityCast /
	// SpellAbilityCast trigger fired on (abcopy1). An AbilityPush event's Obj
	// is the source PERMANENT -- events.Apply mints the ability's stack wrapper
	// off the event -- so Remembered alone names the battlefield permanent and
	// every Defined$ TriggeredSpellAbility consumer would resolve a non-stack
	// object (effCopySpellAbility's stack zone guard then no-ops silently).
	// Rules captures the wrapper id at fire time, when it is deterministically
	// the topmost non-trigger ability wrapper whose Source is the triggering
	// permanent (the same mechanism TriggerPaidX/TriggerConverge use: no event
	// schema change, a log-only replay folds the same AbilityPush, mints the
	// same id and re-runs the capture at the same point). Zero for a spell-cast
	// trigger (the ev.Obj spell object is TriggerCard) and for every other
	// mode. Unlike TriggerStack it is not a targeting event's object; it is
	// the activation provenance the copy / ChangeX / counter family reads.
	TriggerAbility state.ObjID
	// TriggerConverge snapshots the CR 107.4f converge colour count of
	// TriggerCard's cast when this trigger matched (rules/trigger_referents'
	// capture beside TriggerPaidX, read by evalRefProperty's Converge
	// property). The same trigger-time binding rule applies: the colours were
	// spent when the spell was cast, so a spell countered between trigger push
	// and resolution must not read 0 -- its stack->graveyard move clears the
	// live Object.ConvergeColours, while this snapshot survives to resolution.
	// Zero is both a valid count and the value for a triggering card whose
	// cast carried none.
	TriggerConverge int32
	// TriggerManaSpent snapshots the CR 601.2h / 106.12 TOTAL mana actually
	// spent to cast TriggerCard when this trigger matched (rules/
	// trigger_referents' capture beside TriggerPaidX/TriggerConverge, read by
	// evalRefProperty's CastTotalManaSpent property). TriggerManaSnowSpent and
	// TriggerManaTyped carry the per-producer breakdown the filtered
	// `TriggeredCard$CastTotalManaSpent <Type>` spelling selects (the
	// ManaSnowSpent / Treasure / Cave / Desert captures the plain head reads,
	// in state.TypedManaTags order). The same trigger-time binding rule as
	// TriggerConverge applies: the mana was spent when the spell was cast, so
	// a spell that has left the stack (countered, or resolved onto the
	// battlefield) before the trigger resolves must not read the zeroed live
	// fields -- these snapshots survive to resolution. Zero is both a valid
	// spend and the value for a triggering card whose cast carried none.
	TriggerManaSpent     int32
	TriggerManaSnowSpent int32
	TriggerManaTyped     [4]int32
	// TriggerBlocker is the BLOCKING creature of the DeclareBlockers pair a
	// Mode$ Blocks trigger fired for (rules/trigger_match.go's
	// checkBlocksTriggers). A Blocks trigger's Remembered carries the pair's
	// ATTACKER (Godsend's Blocks half reads DefinedCards$ TriggeredAttackers),
	// so the blocker role is the only exact referent for the
	// TriggeredBlockerLKICopy/TriggeredBlockerController spellings -- without
	// it they resolve the remembered attacker. The role-absent fallback (the
	// AttackerBlockedByCreature queue entries, whose Remembered IS the
	// blocker, and hand-built contexts) keeps the old Remembered read, exactly
	// like TriggerBearer's discipline. Zero outside a Blocks capture; not
	// serialized into events.Event -- the per-stack-instance capture is
	// rebuilt by the same replay re-derivation as TriggerPaidX/TriggerConverge.
	TriggerBlocker state.ObjID
	// TriggerEnlisted is the nonattacking creature an attacking creature
	// tapped for CR 702.160's enlist action (rules/enlist.go): the Enlist
	// event's Obj is the ATTACKING creature, so the enlisted creature -- what
	// Mode$ Enlisted's ValidEnlisted$ filters and what Defined$
	// TriggeredEnlisted names (Goblin Morale Sergeant's conjured duplicate) --
	// rides IDs[0] and is captured here at fire time. Zero outside an Enlisted
	// capture; not serialized into events.Event -- the per-stack-instance
	// capture is rebuilt by the same replay re-derivation as
	// TriggerPaidX/TriggerConverge.
	TriggerEnlisted state.ObjID
	// TriggeredOpponentsVotedSame / TriggeredOpponentsVotedDiff are the two
	// List$ opponent sets the canonical vote-finished carrier (effects/
	// vote.go) encodes: the players other than the TRIGGER SOURCE'S
	// CONTROLLER who voted for a choice that controller voted for / for a
	// different one, in voter order. The carrier Note carries the RAW ballots
	// (each voter's player id + pick); rules/trigger_referents' Vote case
	// re-splits them against the source's controller via the shared
	// effects.VoteSplit -- the vote CASTER is irrelevant to the referent, so
	// a vote cast by an opponent binds the sets exactly as one cast by the
	// carrier's controller does. The Defined$ spellings
	// TriggeredOpponentVotedSame/TriggeredOpponentVotedDiff and the count
	// ref TriggeredPlayersOpponentVotedDiff$Amount read them at resolution,
	// long after the event. Not serialized into events.Event -- the ballots
	// live ON the carrier Note (Pairs) and the per-stack capture is rebuilt
	// by the same replay re-derivation as TriggerPaidX/TriggerConverge. Both
	// empty outside a Vote capture.
	TriggeredOpponentsVotedSame []state.PlayerID
	TriggeredOpponentsVotedDiff []state.PlayerID
	// TriggerDamageSources / TriggerDamageTargets are the deduplicated
	// matching source and target sets of the whole damage batch a Mode$
	// DamageAll trigger fired for (trig:DamageAll): every (source, target)
	// pair the batch's Damage events carried that matched the trigger's
	// ValidSource$/ValidTarget$, in first-seen event order, captured on the
	// batch latch entry and patched onto the queued trigger at batch close.
	// They are the referents the plural corpus readers resolve -- Malcolm
	// Keen-Eyed Navigator's and Hordewing Skaab's
	// "TriggeredPlayersTargets$Amount" (the count of matching target
	// PLAYERS, the "for each opponent dealt damage" reading), Breeches'
	// "Defined$ TriggeredTargets" ("each of those opponents' libraries") and
	// Nelly Borca's "Defined$ TriggeredSourcesController" ("you and the
	// controller of those creatures"). The singleton TriggerSource /
	// TriggerTarget roles stay the FIRST matching pair's, exactly as before;
	// an absent set (hand-built context, a batch-less capture) falls back to
	// those singleton semantics. Not serialized into events.Event -- the
	// capture is rebuilt by the same replay re-derivation as TriggerAmount's
	// batch total.
	TriggerDamageSources []state.ObjID
	TriggerDamageTargets []state.Target
}

// TriggeredCardController is the one resolver for "that card's controller"
// in a trigger -- Defined$, OptionalDecider$, UnlessPayer$ and the targeting
// restriction all read it here. A card that left the battlefield is referred
// to as it last existed there (CR 603.10a): a stolen creature that dies is its
// taker's, although the move has already returned it to its owner. Otherwise
// it is the triggering card's current controller, the card being TriggerCard
// or, for a mode that records none, the first object the trigger remembered.
func TriggeredCardController(g *state.Game, tc TriggerContext, remembered []state.Target) (state.PlayerID, bool) {
	if tc.TriggerCardController.IsPlayer {
		return tc.TriggerCardController.Player, true
	}
	card := tc.TriggerCard
	if card == 0 {
		for _, t := range remembered {
			if !t.IsPlayer {
				card = t.Obj
				break
			}
		}
	}
	if o := g.Obj(card); o != nil {
		return o.Controller, true
	}
	return 0, false
}

// spawnerChain strips Forge's "Spawner>" chain prefix from one ref argument,
// reporting whether the prefix was present, with the inner ref trimmed.
// Shared by the control/ownership referent grammar (controlReferent and
// controlReferentPlayers), the count-head reader (refTargets) and the
// damage-source reader (damageSourceSpecTargets) so the three cannot drift
// about what the chain spelling is.
func spawnerChain(spec string) (string, bool) {
	inner, ok := strings.CutPrefix(spec, "Spawner>")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(inner), true
}

// controlReferent is the single classifier for the two-token ownership and
// control grammar. The Triggered* arms read event provenance; the Targeted*
// arms read only the targets of the resolving object. A "Spawner>" chain is
// recognised when its INNER ref is (Forge's adjustTriggerContext re-anchor,
// resolved against the same riding TriggerContext); every other provenance
// chain and every unknown inner ref stays unknown, failing closed.
func controlReferent(p string) (op, ref string, ok bool) {
	op, ref, ok = strings.Cut(p, " ")
	if !ok || (op != "ControlledBy" && op != "OwnedBy") {
		return "", "", false
	}
	if inner, isChain := spawnerChain(ref); isChain {
		ref = inner
	}
	switch controlReferentf9b1Codes.Code(string(ref)) {
	case controlReferentf9b1TriggeredTarget:
		return op, ref, true
	}
	return "", "", false
}

// targetReferent is the shared classifier for TargetedPlayerCtrl and the
// matching arm. It must remain separate from controlReferent because this is
// a one-token predicate, not a ControlledBy/OwnedBy argument.
func targetReferent(p string) bool { return p == "TargetedPlayerCtrl" }

// controlReferentPlayers resolves the player or players a control/ownership
// referent names. A Targeted* referent is resolution-only: SpecContext has
// ResolutionTargets set only by effects.Ctx.SpecContext (or the resolution
// legality recheck), never while an offer is being built. Every absent target,
// gone object, or unsupported reference is unbound rather than guessed.
func controlReferentPlayers(g *state.Game, sc SpecContext, op, ref string) ([]state.PlayerID, bool) {
	if g == nil {
		return nil, false
	}
	// spawnercontrol: Forge's adjustTriggerContext re-anchor. "Spawner>"
	// re-anchors the rest of the chain on the firing trigger's own event
	// roles, and the TriggerContext rides the resolving ability's stack
	// context into the body (rules binds ctx.TriggerContext from the stack
	// object) and into the offer census (e.targetSpecContext binds it again
	// for the CR 608.2b recheck), so stripping the prefix and resolving the
	// inner ref against the SAME SpecContext is the whole arm: the control
	// referents read event ROLES, not Remembered, and the roles ride
	// untouched through effImmediateTrigger's capture-excluded instance
	// copy. No Ctx.Captured substitution here -- that stand-in belongs to
	// the count/damage readers, whose refs read Remembered. An inner ref
	// this grammar does not know stays unbound; fail closed.
	if inner, isChain := spawnerChain(ref); isChain {
		return controlReferentPlayers(g, sc, op, inner)
	}
	var targets []state.Target
	switch controlReferentPlayersf9b2Codes.Code(string(ref)) {
	case controlReferentPlayersf9b2TriggeredSourceSAController:
		// The controller of the CAUSING spell/ability's source: the role a
		// BecomesTarget/BecomesTargetOnce trigger captures in TriggerSource
		// (Leyline of Combustion's payout, Ashenmoor Liege's life loss, Black
		// Bolt's "destroy target nonland permanent that player controls"
		// offer). Unlike the Targeted* arms this is not resolution-only --
		// the role is bound at fire time and rides the trigger's stack
		// context into both the offer and the CR 608.2b recheck. It names a
		// SEAT (the referent says Controller), so it resolves directly to the
		// captured source's controller instead of the object tail below,
		// which would read the source's OWNER for the OwnedBy variant. No
		// role, or a source object that is gone, stays unbound -- an absent
		// binding must never admit the positive or the negated predicate.
		if sc.TriggerSource == 0 {
			return nil, false
		}
		o := g.Obj(sc.TriggerSource)
		if o == nil {
			return nil, false
		}
		return []state.PlayerID{o.Controller}, true
	case controlReferentPlayersf9b2TriggeredTarget:
		targets = []state.Target{sc.TriggerTarget}
	case controlReferentPlayersf9b2TriggeredDefendingPlayer:
		targets = []state.Target{sc.DefendingPlayer}
	case controlReferentPlayersf9b2TriggeredPlayer:
		targets = []state.Target{sc.TriggerPlayer}
	case controlReferentPlayersf9b2TriggeredCard:
		// "Controlled by the triggering card's controller": the card's
		// last-known controller when it left the battlefield (CR 603.10a).
		if op == "ControlledBy" && sc.TriggerCardController.IsPlayer {
			targets = []state.Target{sc.TriggerCardController}
			break
		}
		targets = []state.Target{{Obj: sc.TriggerCard}}
	case controlReferentPlayersf9b2CardController:
		// The triggering card's controller as it last existed on the
		// battlefield (CR 603.10a), else its current controller. Binds only
		// through the trigger's own card role; with no card context the
		// referent stays unbound and the predicate fails closed, never
		// widening to every player.
		if sc.TriggerCardController.IsPlayer {
			targets = []state.Target{sc.TriggerCardController}
			break
		}
		targets = []state.Target{{Obj: sc.TriggerCard}}
	case controlReferentPlayersf9b2Targeted:
		bound, ok := sc.TargetBinding()
		if !ok {
			return nil, false
		}
		targets = bound
	case controlReferentPlayersf9b2Remembered:
		// Resolution-only, like Targeted*: the players this resolution
		// remembers -- a RepeatEach loop's current subject. Forge's
		// getDefinedPlayers("Remembered") adds remembered PLAYERS only; a
		// remembered CARD contributes its controller only for the
		// RememberedController/RememberedOwner spellings (handled by their
		// own referents). Mapping a remembered card to its controller here
		// would widen `ControlledBy Remembered` to the previous iteration's
		// RememberChosen$ card's controller as well as the current subject
		// (Summon: Valefor, Chaos Defiler).
		//
		// The RememberedPlayers channel is the CONSULTATION-time half:
		// rules' block consultation (combat.BlockRestricted) binds a registered
		// restriction's captured players on a static that never resolves, so
		// without it a ValidBlocker$ Creature.RememberedPlayerCtrl clause
		// (The Motherlode, Excavator) would fail closed. Every
		// resolution-time caller leaves the field zero, so their read is
		// untouched, and the fail-closed shape (no binding at all =>
		// ok=false, even under '!') is unchanged.
		for _, p := range sc.RememberedPlayers {
			targets = append(targets, state.Target{IsPlayer: true, Player: p})
		}
		if !sc.Resolving {
			if len(targets) == 0 {
				return nil, false
			}
			break
		}
		for _, t := range sc.Remembered {
			if t.IsPlayer {
				targets = append(targets, t)
			}
		}
	case controlReferentPlayersf9b2PlayerIsRemembered:
		// vow1: the same remembered set the bare "Remembered" referent
		// reads, PLAYERS ONLY -- the full player-spec spelling names the
		// remembered player (a RepeatEach loop's subject), never a
		// remembered object's controller.
		if !sc.Resolving {
			return nil, false
		}
		for _, t := range sc.Remembered {
			if t.IsPlayer {
				targets = append(targets, t)
			}
		}
	case controlReferentPlayersf9b2RememberedController:
		// definedrem3: a remembered CARD contributes its controller (ControlledBy)
		// or owner (OwnedBy) -- the positive route Forge spells
		// `ControlledBy RememberedController`. Resolution-only, exactly like the
		// bare "Remembered" arm: the tail maps each remembered target by op,
		// keeping remembered PLAYERS as their own seats.
		if !sc.Resolving {
			return nil, false
		}
		targets = sc.Remembered
	case controlReferentPlayersf9b2NextOpponentToYourLeft:
		// Barroom Brawl's "target creature the opponent to your left
		// controls": the next living seat after You in turn order (Forge's
		// getNextPlayerAfter; this build has no teams, so the next seat is
		// also the next opponent). Unbound with no other living seat.
		alive := g.AliveFrom(sc.You)
		if len(alive) < 2 {
			return nil, false
		}
		targets = []state.Target{{Player: alive[1], IsPlayer: true}}
	case controlReferentPlayersf9b2ChosenPlayer:
		// vow1: the resolution's own ChoosePlayer answer (Gluntch's
		// "ControlledBy ChosenPlayer"), the same current-resolution set the
		// Player.Chosen Defined selector reads.
		if !sc.Resolving {
			return nil, false
		}
		for _, t := range sc.Chosen {
			if t.IsPlayer {
				targets = append(targets, t)
			}
		}
	case controlReferentPlayersf9b2PlayerEnchantedBy:
		// EnchantedBy is a global player property: the Aura's own controller
		// is irrelevant. Resolve it through the shared player-spec evaluator
		// so attachment state and the property grammar have one home.
		for p := range g.Players {
			if MatchesPlayerSpecCtx(g, ref, state.PlayerID(p), sc.You, PlayerSpecCtx{}) {
				targets = append(targets, state.Target{IsPlayer: true, Player: state.PlayerID(p)})
			}
		}
	default:
		return nil, false
	}

	players := make([]state.PlayerID, 0, len(targets))
	for _, t := range targets {
		if t.IsPlayer {
			// TargetedController means the controller of an object target; a
			// player target is instead Targeted/TargetedPlayer (or the explicit
			// TargetedOrController union).
			if ref == "TargetedController" {
				continue
			}
			if int(t.Player) >= len(g.Players) {
				return nil, false
			}
			players = append(players, t.Player)
			continue
		}
		if t.Obj == 0 {
			return nil, false
		}
		obj := g.Obj(t.Obj)
		if obj == nil {
			return nil, false
		}
		// Targeted and TargetedPlayer name a player target, not an object's
		// controller. TargetedOrController explicitly admits both forms.
		if ref == "Targeted" || ref == "TargetedPlayer" || ref == "ThisTargetedPlayer" {
			continue
		}
		// TargetedController and TargetedOrController resolve the target's
		// controller to a PLAYER before ControlledBy/OwnedBy compares its
		// candidate. In particular, OwnedBy TargetedController means "owned
		// by that controller", not "owned by the targeted permanent's owner".
		if ref == "TargetedController" || ref == "TargetedOrController" {
			players = append(players, obj.Controller)
		} else if op == "OwnedBy" {
			players = append(players, obj.Owner)
		} else {
			players = append(players, obj.Controller)
		}
	}
	if len(players) == 0 {
		return nil, false
	}
	return players, true
}

// matchControlReferent returns unresolved as ok=false, including beneath '!':
// absence of a binding must never turn into a match by negation.
func matchControlReferent(g *state.Game, o *state.Object, sc SpecContext, op, ref string) (bool, bool) {
	players, ok := controlReferentPlayers(g, sc, op, ref)
	if !ok {
		return false, false
	}
	for _, p := range players {
		if (op == "OwnedBy" && o.Owner == p) || (op == "ControlledBy" && o.Controller == p) {
			return true, true
		}
	}
	return false, true
}

// matchTargetedPlayerCtrl and matchTargetedPlayerOwn are resolution-only
// one-token forms. They bind only direct PLAYER targets (not object
// controllers) and compare the corresponding candidate field.
func matchTargetedPlayerCtrl(g *state.Game, o *state.Object, sc SpecContext) (bool, bool) {
	return matchTargetedPlayerField(g, o, sc, "ControlledBy", func(o *state.Object) state.PlayerID { return o.Controller })
}

func matchTargetedPlayerOwn(g *state.Game, o *state.Object, sc SpecContext) (bool, bool) {
	return matchTargetedPlayerField(g, o, sc, "OwnedBy", func(o *state.Object) state.PlayerID { return o.Owner })
}

func matchTargetedPlayerField(g *state.Game, o *state.Object, sc SpecContext, op string, field func(*state.Object) state.PlayerID) (bool, bool) {
	players, ok := controlReferentPlayers(g, sc, op, "TargetedPlayer")
	if !ok {
		return false, false
	}
	for _, p := range players {
		if field(o) == p {
			return true, true
		}
	}
	return false, true
}

// MatchSpec evaluates a resolution-time filter with the chain's layer-3
// rename table bound: MatchesSpecFrom's grammar (You/Source only, no
// numeric-RHS resolver) PLUS Ctx.Layers.EffectiveNames, so a resolving effect's
// filter agrees with rules' layer walk. Prefer this over a bare
// MatchesSpecFrom inside an effect body -- the bare form carries no renames
// and reads the printed face.
func (c *Ctx) MatchSpec(g *state.Game, spec string, id state.ObjID, you state.PlayerID) bool {
	return MatchesSpecCtx(g, spec, id, c.TableSpecContext(you))
}

// resolveNumericRHS is the numeric-RHS resolver the gate in (*Ctx).SpecContext
// installs on the SpecContext it builds: priority order is a published roll
// name, then "X" per the
// two-shape SVar:X reading, then any other name through the SVar table. A
// name with no resolvable source returns ok=false -- the recognised-shape-
// never-matches contract -- and the resolvingRHS guard fails closed the
// re-entrant SVar-counts-a-spec-with-the-same-RHS case.
func (c *Ctx) resolveNumericRHS(name string) (int32, bool) {
	if v, ok := runtimePublished(c, name); ok {
		return v, true
	}
	if c.resolvingRHS {
		return 0, false
	}
	if name == "X" {
		if c.Host != nil {
			if body := strings.TrimSpace(c.SVars["X"]); body != "" && !strings.EqualFold(body, "Count$xPaid") {
				c.resolvingRHS = true
				n, ok := EvalCountOK(c.Host, c, body)
				c.resolvingRHS = false
				if !ok {
					return 0, false
				}
				return n, true
			}
		}
		// No SVar:X, a Count$xPaid body, or a roll-only context without a
		// Host: the variable IS the paid X (0 when unpaid).
		return c.X, true
	}
	if c.Host == nil {
		return 0, false
	}
	body := strings.TrimSpace(c.SVars[name])
	if body == "" {
		return 0, false
	}
	c.resolvingRHS = true
	n, ok := EvalCountOK(c.Host, c, body)
	c.resolvingRHS = false
	if !ok {
		return 0, false
	}
	return n, true
}

const (
	controlReferentf9b1TriggeredTarget uint16 = 1 // "TriggeredTarget", "TriggeredDefendingPlayer", "TriggeredPlayer", "TriggeredCard", "Targeted", "TargetedPla...
)

var controlReferentf9b1Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "TriggeredTarget", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "TriggeredDefendingPlayer", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "TriggeredPlayer", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "TriggeredCard", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "Targeted", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "TargetedPlayer", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "ThisTargetedPlayer", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "TargetedController", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "TargetedOrController", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "Remembered", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "RememberedPlayer", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "Player.IsRemembered", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "ChosenPlayer", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "Player.Chosen", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "Player.EnchantedBy", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "RememberedController", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "RememberedOwner", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "CardController", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "TriggeredSourceSAController", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "NextOpponentToYourLeft", Val: controlReferentf9b1TriggeredTarget},
	state.StrEntry[uint16]{Key: "NextPlayerToYourLeft", Val: controlReferentf9b1TriggeredTarget},
)

const (
	controlReferentPlayersf9b2TriggeredSourceSAController uint16 = 1  // "TriggeredSourceSAController"
	controlReferentPlayersf9b2TriggeredTarget             uint16 = 2  // "TriggeredTarget"
	controlReferentPlayersf9b2TriggeredDefendingPlayer    uint16 = 3  // "TriggeredDefendingPlayer"
	controlReferentPlayersf9b2TriggeredPlayer             uint16 = 4  // "TriggeredPlayer"
	controlReferentPlayersf9b2TriggeredCard               uint16 = 5  // "TriggeredCard"
	controlReferentPlayersf9b2CardController              uint16 = 6  // "CardController"
	controlReferentPlayersf9b2Targeted                    uint16 = 7  // "Targeted", "TargetedPlayer", "ThisTargetedPlayer", "TargetedController", "TargetedOrController"
	controlReferentPlayersf9b2Remembered                  uint16 = 8  // "Remembered", "RememberedPlayer"
	controlReferentPlayersf9b2PlayerIsRemembered          uint16 = 9  // "Player.IsRemembered"
	controlReferentPlayersf9b2RememberedController        uint16 = 10 // "RememberedController", "RememberedOwner"
	controlReferentPlayersf9b2NextOpponentToYourLeft      uint16 = 11 // "NextOpponentToYourLeft", "NextPlayerToYourLeft"
	controlReferentPlayersf9b2ChosenPlayer                uint16 = 12 // "ChosenPlayer", "Player.Chosen"
	controlReferentPlayersf9b2PlayerEnchantedBy           uint16 = 13 // "Player.EnchantedBy"
)

var controlReferentPlayersf9b2Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "TriggeredSourceSAController", Val: controlReferentPlayersf9b2TriggeredSourceSAController},
	state.StrEntry[uint16]{Key: "TriggeredTarget", Val: controlReferentPlayersf9b2TriggeredTarget},
	state.StrEntry[uint16]{Key: "TriggeredDefendingPlayer", Val: controlReferentPlayersf9b2TriggeredDefendingPlayer},
	state.StrEntry[uint16]{Key: "TriggeredPlayer", Val: controlReferentPlayersf9b2TriggeredPlayer},
	state.StrEntry[uint16]{Key: "TriggeredCard", Val: controlReferentPlayersf9b2TriggeredCard},
	state.StrEntry[uint16]{Key: "CardController", Val: controlReferentPlayersf9b2CardController},
	state.StrEntry[uint16]{Key: "Targeted", Val: controlReferentPlayersf9b2Targeted},
	state.StrEntry[uint16]{Key: "TargetedPlayer", Val: controlReferentPlayersf9b2Targeted},
	state.StrEntry[uint16]{Key: "ThisTargetedPlayer", Val: controlReferentPlayersf9b2Targeted},
	state.StrEntry[uint16]{Key: "TargetedController", Val: controlReferentPlayersf9b2Targeted},
	state.StrEntry[uint16]{Key: "TargetedOrController", Val: controlReferentPlayersf9b2Targeted},
	state.StrEntry[uint16]{Key: "Remembered", Val: controlReferentPlayersf9b2Remembered},
	state.StrEntry[uint16]{Key: "RememberedPlayer", Val: controlReferentPlayersf9b2Remembered},
	state.StrEntry[uint16]{Key: "Player.IsRemembered", Val: controlReferentPlayersf9b2PlayerIsRemembered},
	state.StrEntry[uint16]{Key: "RememberedController", Val: controlReferentPlayersf9b2RememberedController},
	state.StrEntry[uint16]{Key: "RememberedOwner", Val: controlReferentPlayersf9b2RememberedController},
	state.StrEntry[uint16]{Key: "NextOpponentToYourLeft", Val: controlReferentPlayersf9b2NextOpponentToYourLeft},
	state.StrEntry[uint16]{Key: "NextPlayerToYourLeft", Val: controlReferentPlayersf9b2NextOpponentToYourLeft},
	state.StrEntry[uint16]{Key: "ChosenPlayer", Val: controlReferentPlayersf9b2ChosenPlayer},
	state.StrEntry[uint16]{Key: "Player.Chosen", Val: controlReferentPlayersf9b2ChosenPlayer},
	state.StrEntry[uint16]{Key: "Player.EnchantedBy", Val: controlReferentPlayersf9b2PlayerEnchantedBy},
)
