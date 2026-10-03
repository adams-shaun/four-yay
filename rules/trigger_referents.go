package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// triggerReferents captures roles from the causing event, NOT from the trigger's
// later target answer or from Remembered (which scripts may overwrite). Nothing
// is added to the event schema: replay discovers the same bindings from the same
// events. A role not provided by a supported trigger mode remains absent.
func (e *Engine) triggerReferents(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) effects.TriggerContext {
	var c effects.TriggerContext
	player := func(p state.PlayerID) state.Target { return state.Target{Player: p, IsPlayer: true} }
	// CR 603.10a: a card that left the battlefield keeps, for this trigger,
	// the controller it had there. Recorded here so it travels with the
	// ability onto the stack (triggerContexts) and every "that card's
	// controller" read -- effects.TriggeredCardController -- sees it.
	if lki != nil && lki.ID == ev.Obj && leftBattlefield(ev) {
		c.TriggerCardController = player(lki.Controller)
	}
	switch t.Mode {
	case "RolledDie", "RolledDieOnce":
		// The canonical roll Note (effects/dice.go): the per-die DieRollNote
		// for Mode$ RolledDie and the per-resolution DieRollBatchNote for
		// Mode$ RolledDieOnce. Both name the roller (ev.Player) and the
		// reported result (ev.Amount). TriggerResult is what the
		// TriggerCount$Result head answers at resolution (Mr. House's
		// BranchConditionSVar$ reads it long after the RollDice resolution that
		// produced it has finished), TriggerResultMax is the
		// TriggerCountMax$Result head (Farideh), and TriggerPlayer is the
		// roller, so a body reading "that player" resolves the seat that
		// rolled. Each decoder rejects the other mode's Note, so the two
		// collectors can never cross-fire.
		if roller, _, _, result, ok := effects.DieRollResult(ev); ok {
			c.TriggerPlayer = player(roller)
			c.TriggerResult = result
			c.TriggerResultMax = result
		}
		if roller, _, maxResult, result, ok := effects.DieRollBatchResult(ev); ok {
			c.TriggerPlayer = player(roller)
			c.TriggerResult = result
			c.TriggerResultMax = maxResult
		}
	case "BecomesTarget", "BecomesTargetOnce":
		// This matcher fires only for its own source being targeted, even when
		// the causing spell chose several targets. ev.Obj is that spell/ability.
		// BecomesTargetOnce carries the same roles -- its batch latch is the
		// queue-time gate, not this capture -- so the plural-mode body
		// resolves TriggeredSourceSA/TriggeredSourceSAController exactly as
		// the per-target mode does (Leyline of Combustion's payout).
		c.TriggerTarget = state.Target{Obj: source}
		c.TriggerSource = e.protectionSource(ev.Obj)
		c.TriggerStack = ev.Obj
	case "DamageDone", "DamageDealtOnce", "DamageDoneOnce", "DamageAll":
		// The damage source the causing event names: the published override
		// when a DamageSource$ emitter set one (Kediss' DamageAll with
		// DamageSource$ TriggeredSource resolves its own execute through
		// exactly this role), else the resolution/combat source. The
		// defending-player read below is combat-shaped by construction: an
		// override is never published during combat's assignment loop.
		c.TriggerSource = e.inFlightDamageSource()
		c.TriggerAmount = ev.Amount
		c.TriggerTarget = state.Target{Obj: ev.Obj}
		if ev.Obj == 0 {
			c.TriggerTarget = player(ev.Player)
		}
		if o := e.G.Obj(e.inFlightDamageSource()); o != nil && o.IsAttacking {
			c.DefendingPlayer = player(o.Attacking)
		}
	case "CounterAdded", "CounterAddedOnce":
		// The batch size the body reads as TriggerCount$Amount (Simic
		// Ascendancy's "put that many growth counters" on CounterAddedOnce,
		// and the same read on a plain CounterAdded line): one CounterChange
		// event carries the whole placement batch in Amount, and ev.Obj is
		// the permanent the counters landed on. Both modes are dispatched by
		// the same matcher (trigmatch.counterAddedMatches), so both must bind the same
		// batch role -- the crossing gate is the matcher's job, the amount the
		// body reads is this one, and a plain CounterAdded trigger that reads
		// "that many" had nothing to read before this case named it.
		c.TriggerCard = ev.Obj
		c.TriggerAmount = ev.Amount
	case "CounterPlayerAddedAll":
		// The batch "whenever you put one or more counters on ..." mode's
		// roles (Generous Patron, Rikku, Kros, All Will Be One): the
		// recipient permanent is ev.Obj -- triggerRemembered already seeds
		// Remembered with it, so Defined$ TriggeredObjectLKICopy (Rikku's
		// RememberObjects$ on the DB body) resolves against it -- and the
		// batch size rides TriggerAmount for the count head TriggerCount$Amount
		// (All Will Be One's "deals that much damage" NumDmg$ X). A player
		// recipient (PlayerCounterChange, The Great Goblin's "or player")
		// carries no object: the recipient player is the TriggerTarget role
		// and the object roles stay absent rather than pointing at the
		// trigger's source.
		c.TriggerCard = ev.Obj
		if ev.Obj == 0 {
			c.TriggerTarget = player(ev.Player)
		}
		c.TriggerAmount = ev.Amount
	case "Scry":
		// The completed-scry marker carries the count of cards the player
		// actually chose to put on the bottom in Amount, which is the
		// magnitude the trigger body reads: The Temporal Anchor's
		// `SVar:X:TriggerCount$ScryBottom` resolves to this value and its
		// `DB$ Dig | DigNum$ X | FromBottom$ True` exiles that many from the
		// bottom. It is captured at fire time and carried to resolution, the
		// same contract TriggerAmount has for every other mode.
		c.TriggerAmount = ev.Amount
	case "CounterRemovedOnce":
		// The removal batch, mirrored: one CounterChange with a negative
		// Amount carries the whole removal, and the magnitude the causing
		// event carried is POSITIVE, so TriggerCount$Amount reads -ev.Amount
		// (Chandra, Fire Artisan's "deals that much damage"; B.O.B. Bevy of
		// Beebles; Regenerations Restored). ev.Obj is the permanent the
		// counters left.
		c.TriggerCard = ev.Obj
		if ev.Amount < 0 {
			c.TriggerAmount = -ev.Amount
		} else {
			c.TriggerAmount = ev.Amount
		}
	case "DamagePreventedOnce":
		// The prevention Note carries the prevented damage in Amount and the
		// damaged side in Obj/Player (rules/replacement.go's stored-prevention
		// arms). TriggerCount$DamageAmount reads TriggerAmount when the
		// trigger's DB$ PutCounter resolves (Selfless Squire's TrigPut).
		c.TriggerAmount = ev.Amount
		c.TriggerTarget = state.Target{Obj: ev.Obj}
		if ev.Obj == 0 {
			c.TriggerTarget = player(ev.Player)
		}
	case "Attacks", "AttackersDeclaredOneTarget", "AttackersDeclared":
		c.DefendingPlayer = player(ev.Player)
		c.AttackedTarget = player(ev.Player)
		if len(ev.IDs) > 0 {
			c.AttackingPlayer = player(e.controllerOf(ev.IDs[0]))
		}
		// The current engine batches attackers per defender. Preserve the
		// defending player, but do not invent a singular TriggeredCard when
		// several matching attackers caused this one queued trigger.
		matches := 0
		for _, id := range ev.IDs {
			// Attacks reads ValidCard$; the AttackersDeclared family reads
			// ValidAttackers$ -- the same per-attacker filter under its own
			// name.
			spec := t.ParamStr(cards.PKValidCard)
			if spec == "" && t.Mode == "AttackersDeclared" {
				spec = t.ParamStr(cards.PKValidAttackers)
			}
			if (spec == "" && id == source) || (spec != "" && e.matchesSpec(spec, id, e.specCtx(source, e.controllerOf(source)))) {
				matches++
				c.TriggerCard = id
			}
		}
		if matches != 1 {
			c.TriggerCard = 0
		}
		c.TriggerSource = c.TriggerCard
	case "Taps":
		c.TriggerActivator = player(trigmatch.TapActor(boardOf(e), ev))
	case "ChangesZone", "LandPlayed":
		c.TriggerCard = ev.Obj
	case "Milled", "MilledAll":
		// The milled card is ev.Obj (what ValidCard$ matched) and the player
		// whose library was milled is ev.Player (what ValidPlayer$ matched),
		// so TriggerPlayer names whose mill this was. One milled card is one
		// event, so TriggerAmount is 1 per event; the MilledAll batch close
		// overrides it with the batch's matching-card count (the
		// TriggerCount$Amount head The Wise Mothman's X and Screeching
		// Scorchbeast's "that many tokens" read).
		c.TriggerCard = ev.Obj
		c.TriggerPlayer = player(ev.Player)
		c.TriggerAmount = 1
		if ev.Amount > 0 {
			c.TriggerAmount = ev.Amount
		}
	case "DiscardedAll":
		// The discard batch's roles mirror MilledAll's: the discarded card is
		// ev.Obj (what ValidCard$ matched) and the discarding player is
		// ev.Player (what ValidPlayer$ matched), so TriggerPlayer names whose
		// discard this was. One discarded card is one event, so TriggerAmount
		// is 1 per event; the DiscardedAll batch close overrides it with the
		// batch's matching-card count (the TriggerCount$Amount head Magmakin
		// Artillerist's X and Marauding Mako's "that many counters" read) and
		// its Remembered/Captured with the discarded set (Pure Intentions'
		// Defined$ TriggeredCards).
		c.TriggerCard = ev.Obj
		c.TriggerPlayer = player(ev.Player)
		c.TriggerAmount = 1
	case "Drawn":
		c.TriggerCard = ev.Obj
		c.TriggerPlayer = player(ev.Player)
	case "LifeLost", "LifeLostAll":
		if p, amount, ok := lifeLoss(ev); ok {
			c.TriggerPlayer = player(p)
			c.TriggerAmount = amount
		}
	case "LifeGained":
		// The gaining player and the gained magnitude: TriggerCount$LifeAmount
		// (Prize Pig's CounterNum$ Y) reads both off this context.
		if ev.Kind == events.LifeChange && ev.Amount > 0 && int(ev.Player) >= 0 && int(ev.Player) < len(e.G.Players) {
			c.TriggerPlayer = player(ev.Player)
			c.TriggerAmount = ev.Amount
		}
	case "SpellCast", "AbilityCast", "SpellAbilityCast":
		c.TriggerCard = ev.Obj
		c.TriggerSource = e.protectionSource(ev.Obj)
		// TriggeredActivator is the player who cast or activated it, the
		// same ev.Player ValidActivatingPlayer$ is matched against
		// (Tangleroot: "that player adds {G}").
		c.TriggerActivator = player(ev.Player)
		// The activation arm (abcopy1): an AbilityPush event's Obj is the
		// SOURCE PERMANENT -- the ability's stack wrapper is minted inside
		// events.Apply and never travels on the event, so Remembered names the
		// battlefield permanent. Capture the minted wrapper here, at fire
		// time, when it is deterministically the topmost non-trigger ability
		// wrapper whose source is the triggering permanent (checkTriggers
		// runs synchronously inside emit immediately after the AbilityPush
		// applied, and a log-only replay folds the same AbilityPush, mints the
		// same id and re-runs this capture at the same point -- no event
		// schema change, the TriggerPaidX/TriggerConverge mechanism). Absent
		// for PutOnStack: a spell cast's ev.Obj IS the spell object.
		if ev.Kind == events.AbilityPush || ev.Kind == events.KeywordAbilityPush {
			c.TriggerAbility = e.abilityCastStackObject(ev.Obj)
		}
	case "Attached":
		// ev.Obj is the attaching Aura/Equipment, ev.IDs[0] the bearer it
		// became attached to (trigmatch.attachedMatches guarantees a bearer-bearing
		// Attach event reached this mode). The TriggerTarget role serves the
		// TriggeredTarget/TriggeredTargetController spellings (Bramble
		// Elemental's token owner); TriggerBearer -- a field ONLY this case
		// sets -- is what TriggeredTargetLKICopy (Enormous Energy Blade's
		// "tap that creature") resolves, so the bearer never masquerades as
		// another mode's TriggerTarget provenance (a BecomesTarget trigger's
		// TriggerTarget is its own source permanent; reading it as a bearer
		// would make Horobi destroy himself on every targeting).
		c.TriggerCard = ev.Obj
		if len(ev.IDs) > 0 {
			c.TriggerTarget = state.Target{Obj: ev.IDs[0]}
			c.TriggerBearer = ev.IDs[0]
		}
	case "Phase":
		c.TriggerPlayer = player(e.G.Active)
	case "BecomeMonarch":
		// The monarch designation transition's role: the NEW monarch is the
		// event's Player, so Defined$ TriggeredPlayer resolves the seat that
		// just took the crown (Knights of the Black Rose's "that player loses
		// 2 life", Custodi Lich's "target player"). The clause is read only
		// when a BecomeMonarch matcher accepted the event, so ev.Player is
		// always that seat here.
		c.TriggerPlayer = player(ev.Player)
	case "Discover":
		// Discover's completed-action marker carries the resolved discover
		// value in Amount. Curator of Sun's Creation binds it as X through
		// TriggerCount$Amount for its same-value follow-up discover.
		c.TriggerAmount = ev.Amount
	case "Explores":
		// The explore record's roles (task explore1): TriggerCard is the
		// EXPLORER (what ValidCard$ matched), the same ChangesZone read.
		// The revealed card rode the record's IDs, but every corpus body
		// reads the trigger's own source or asks its own targets, so no
		// separate referent field is minted for it.
		c.TriggerCard = ev.Obj
	case "Exploited":
		// The exploit record's roles (task exploit1): the EXPLOITING creature
		// is ev.Obj and rides TriggerSource (Colonel Autumn's team watch has
		// its own body, but a body reading TriggeredSource/TriggeredCard gets
		// a defined referent); the EXPLOITED creature is ev.IDs[0] and rides
		// TriggerCard, so TriggeredCard/TriggeredCardLKICopy resolve against
		// what the exploiter sacrificed (a "that creature" body).
		c.TriggerSource = ev.Obj
		if len(ev.IDs) > 0 {
			c.TriggerCard = ev.IDs[0]
		}
		c.TriggerPlayer = player(ev.Player)
	case "Enlisted":
		// The Enlist event names the ATTACKING creature that enlisted (ev.Obj,
		// the trigger's source for ValidCard$ Card.Self) and the creature it
		// tapped in ev.IDs[0]. TriggerCard is the attacker (so
		// TriggeredCard/TriggeredCardLKICopy resolve against it, the Exerted
		// shape) and TriggerEnlisted is the enlisted creature, what
		// ValidEnlisted$ matched and what Defined$ TriggeredEnlisted reads
		// (Goblin Morale Sergeant's conjured duplicate).
		c.TriggerCard = ev.Obj
		c.TriggerSource = ev.Obj
		c.TriggerPlayer = player(ev.Player)
		if len(ev.IDs) > 0 {
			c.TriggerEnlisted = ev.IDs[0]
		}
	case "BecomeMonstrous":
		// The monstrous mark's roles (task agent-20260919T190014Z):
		// TriggerCard is the creature that just became monstrous (ev.Obj, the
		// trigger's own source for ValidCard$ Card.Self, so
		// TriggeredCard/TriggeredCardLKICopy resolve against it) and
		// TriggerAmount is the monstrosity COUNT the mark event carried --
		// what Hydra Broodmaster's `SVar:MonstrosityX:TriggerCount$Amount` and
		// Vitality Hunter's `SVar:MaxTgts:TriggerCount$Amount` read back.
		c.TriggerCard = ev.Obj
		c.TriggerSource = ev.Obj
		c.TriggerAmount = ev.Amount
		if int(ev.Player) >= 0 && int(ev.Player) < len(e.G.Players) {
			c.TriggerPlayer = player(ev.Player)
		}
	case "Connives":
		// The connive record's roles (task connive1): TriggerCard is the
		// CONNIVER (what ValidCard$ matched), the same ChangesZone read. The
		// discarded cards rode the record's IDs, but every corpus body reads
		// the conniving creature or asks its own targets, so no separate
		// referent field is minted for them.
		c.TriggerCard = ev.Obj
	case "Exerted":
		// The Exert event names the exerted permanent (ev.Obj) and its
		// controller at exert time (ev.Player). TriggerCard is the exerted
		// permanent, so TriggeredCard/TriggeredCardLKICopy resolve against it
		// (Rohirrim Chargers' AttachedTo$ TriggeredCardLKICopy rider and the
		// general "that creature" spelling). triggerRemembered already seeds
		// Remembered with ev.Obj for any non-zero ev.Obj, so this adds the
		// dedicated role without changing the Remembered list.
		c.TriggerCard = ev.Obj
		c.TriggerPlayer = player(ev.Player)
	case "TapsForMana":
		// The ManaAdd event names the activating player, producing permanent,
		// produced type and amount without overloading Remembered. This mode's
		// matcher remains a separate primitive; retaining all four roles here
		// makes ReflectProperty$ Produced exact once that trigger is queued.
		//
		// TriggeredActivator is the player who tapped the permanent for mana --
		// the role Mana Flare's "that player adds ..." reads. The Tap payload
		// carries no Player (emitTap emits the object only), so the actor comes
		// from emitTap's synchronous provenance through trigmatch.TapActor, exactly as
		// the Taps case above binds it; a Tap emitted outside emitTap falls
		// back to the permanent's controller.
		c.TriggerPlayer = player(ev.Player)
		c.TriggerActivator = player(trigmatch.TapActor(boardOf(e), ev))
		c.TriggerCard = ev.Obj
		c.TriggerSource = ev.Obj
		c.TriggerMana = ev.Counter
		c.TriggerAmount = ev.Amount
	case "Vote":
		// The canonical vote-finished carrier (effects/vote.go): the raw
		// ballots ride the Note as player refs (each Pair is
		// [PlayerRef(voter), pick+1]) and are RE-SPLIT here against the
		// trigger SOURCE'S controller -- "a choice you voted for" means the
		// carrier permanent's controller's own ballot, never the vote
		// caster's (the two differ whenever an opponent casts the vote, the
		// ordinary multiplayer case). The resulting sets are bound so the
		// resolution-time spellings -- Defined$
		// TriggeredOpponentVotedSame/TriggeredOpponentVotedDiff and the count
		// ref TriggeredPlayersOpponentVotedDiff$Amount -- read them long after
		// the event, from this per-stack capture. List$ gates the BINDING (the
		// referent scope, never the firing -- see trigmatch.voteMatches): a spelling
		// whose List$ does not name it binds empty.
		if _, ballots, ballotExisted, ok := effects.VoteFinishedResult(ev); ok {
			if e.secretVoteBallots != nil {
				ballots = e.secretVoteBallots
			}
			same, diff := effects.VoteSplit(e.controllerOf(source), ballots, ballotExisted)
			if listAdmits(t.Params["List"], "OppVotedSame") {
				c.TriggeredOpponentsVotedSame = same
			}
			if listAdmits(t.Params["List"], "OppVotedDiff") {
				c.TriggeredOpponentsVotedDiff = diff
			}
		}
	}
	// CR 107.3m binds X when the trigger fires, not when it resolves. In
	// particular, an ETB trigger may remain on the stack after its permanent
	// dies, at which point Move has correctly cleared the object's live X.
	// Keep the event's card value with the rest of the trigger provenance.
	// CR 107.4f's converge colour count rides the same capture: a SpellCast
	// trigger's card carries the pay-time CastInfo stamp (rules/cast.go's
	// payCast) at fire time, and a spell countered before the trigger resolves
	// has had the stack->graveyard move clear it -- the snapshot is what lets
	// evalRefProperty's Converge property answer with the colours actually
	// spent, regardless of the spell's fate.
	if card := e.G.Obj(c.TriggerCard); card != nil {
		c.TriggerPaidX = card.X
		c.TriggerConverge = card.ConvergeColours
		// CR 601.2h / 106.12: the mana actually spent to cast the triggering
		// spell, snapshotted beside the converge colours for the same reason --
		// a spell that leaves the stack before the trigger resolves (countered,
		// or resolved onto the battlefield) has the live fields zeroed, so the
		// TriggeredCard$CastTotalManaSpent ref-head reads this fire-time capture.
		c.TriggerManaSpent = card.ManaSpent
		c.TriggerManaSnowSpent = card.ManaSnowSpent
		for i := range state.TypedManaTags {
			c.TriggerManaTyped[i] = card.TypedManaSpentByTag(i)
		}
	}
	// CR 107.3m: the X paid to turn a permanent face up is carried by the
	// TurnFaceUp event, because the permanent itself was not cast with that X
	// and its live Object.X remains the cast-time value (normally zero).
	if ev.Kind == events.TurnFaceUp {
		c.TriggerPaidX = ev.Amount
	}
	return c
}

// listAdmits reports whether a trigger's List$ scope admits one referent
// token: an absent (or empty) List$ names every set, a present one must name
// the token among its comma entries. Shared by the Vote capture only today.
func listAdmits(list, token string) bool {
	list = strings.TrimSpace(list)
	if list == "" {
		return true
	}
	for part := range strings.SplitSeq(list, ",") {
		if strings.TrimSpace(part) == token {
			return true
		}
	}
	return false
}

// abilityCastStackObject is the fire-time twin of effects.changeXAbilityObject's
// scan (effects cannot import rules, so the scan is mirrored, not shared): the
// topmost non-trigger ability wrapper on the stack whose Source is the
// activating card. Called synchronously inside the activation push emit,
// when that wrapper is exactly the stack top; 0 when no such wrapper exists
// (a stale registration) -- the role simply stays absent.
func (e *Engine) abilityCastStackObject(perm state.ObjID) state.ObjID {
	for i := len(e.G.Stack) - 1; i >= 0; i-- {
		o := e.G.Obj(e.G.Stack[i])
		if o == nil || o.Card != nil || o.Ability == nil || o.Source != perm {
			continue
		}
		if _, isTrig := state.TriggerOf(e.G, o); isTrig {
			continue
		}
		return o.ID
	}
	return 0
}

// specResolveEnv is targetSpecContext's Resolve closure state.
type specResolveEnv struct {
	e                      *Engine
	source                 state.ObjID
	you                    state.PlayerID
	tcx                    effects.TriggerContext
	remembered             []state.Target
	svars                  map[string]string
	lki                    *state.Object
	lkiPower, lkiToughness int32
	lkiPTValid             bool
	x                      int32
	ctx                    *effects.Ctx
	// srcLKI is the source's last-known characteristics when the source has
	// left the battlefield since the ability was created (CR 608.2h,
	// source_char_lki.go); srcLKIValid gates it.
	srcLKI      sourceCharSnapshot
	srcLKIValid bool
	// resolveFn is resolve bound to this env, made once when the env is
	// allocated: a method value taken per call would heap-allocate its
	// closure every time.
	resolveFn func(string) (int32, bool)
}

// acquireSpecEnv hands targetSpecContext its env from the engine's stack of
// reusable envs (e.specEnvs[e.specEnvDepth]), allocating only when the stack
// is deeper than it has ever been. Every caller releases it (releaseSpecEnv,
// deferred) when its SpecContext goes out of use, and the SpecContext --
// with its Resolve -- is only ever passed down into the matching walk, never
// stored, so the env is free again by then. Nested target walks (a Resolve
// that evaluates a count, which walks targets again) take deeper slots:
// defers pop in LIFO order, including when a panic unwinds. The lazily built
// count Ctx is dropped on reuse, so a retained one is never rewritten.
func (e *Engine) acquireSpecEnv() *specResolveEnv {
	if e.specEnvDepth == len(e.specEnvs) {
		env := &specResolveEnv{}
		env.resolveFn = env.resolve
		e.specEnvs = append(e.specEnvs, env)
	}
	env := e.specEnvs[e.specEnvDepth]
	e.specEnvDepth++
	return env
}

// releaseSpecEnv returns the innermost env acquireSpecEnv handed out.
func (e *Engine) releaseSpecEnv() { e.specEnvDepth-- }

func (r *specResolveEnv) countCtx() *effects.Ctx {
	if r.ctx == nil {
		r.ctx = effects.NewCtxPtr(r.source, r.you, effects.CtxInit{TriggerContext: r.tcx,
			Remembered: r.remembered, SVars: r.svars, LKI: r.lki, LKIPower: r.lkiPower,
			LKIToughness: r.lkiToughness, LKIPTValid: r.lkiPTValid, X: r.x})
		r.ctx.Host = r.e
	}
	return r.ctx
}

// sourceLKICount answers a source-characteristic body (Count$CardPower,
// Count$CardToughness, Count$CardCounters.<KIND>) from the departed source's
// last-known snapshot; ok is false when no snapshot applies or the body is
// another head.
func (r *specResolveEnv) sourceLKICount(body string) (int32, bool) {
	if !r.srcLKIValid {
		return 0, false
	}
	return r.srcLKI.count(body)
}

func (r *specResolveEnv) resolve(name string) (int32, bool) {
	body, hasBody := r.svars[name]
	if strings.EqualFold(name, "X") {
		// Forge's two-shape SVar:X contract: Count$xPaid means the
		// causing cast's announced X; every other body is a fixed count
		// evaluated against the trigger's captured referents. Without an
		// authored SVar:X, retain the paid/stack X binding.
		if hasBody && strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
			return r.x, true
		}
		if hasBody {
			if n, ok := r.sourceLKICount(body); ok {
				return n, true
			}
			return effects.EvalCountOK(r.e, r.countCtx(), body)
		}
		return r.x, true
	}
	if !hasBody || strings.TrimSpace(body) == "" {
		return 0, false
	}
	if n, ok := r.sourceLKICount(body); ok {
		return n, true
	}
	return effects.EvalCountOK(r.e, r.countCtx(), body)
}

// targetSpecContext accepts the actual stack id, so simultaneous triggers of
// the same permanent cannot inherit one another's bindings. A prospective cast
// or an ordinary static has no entry and therefore no trigger context.
//
// Resolve reads numeric bounds from the source face's SVar table. In
// particular, SVar:X follows the same two-shape contract as resolving
// effects: Count$xPaid reads the announced/captured X, while another body is
// evaluated as a fixed count with the trigger's captured Remembered, LKI and
// TriggerContext. Missing non-X names and unresolvable bodies fail closed;
// no authored SVar:X retains the ordinary paid/stack X binding.
func (e *Engine) targetSpecContext(source, stack state.ObjID, you state.PlayerID) effects.SpecContext {
	tcx := e.triggerContexts[stack]
	var remembered []state.Target
	if o := e.G.Obj(stack); o != nil {
		remembered = append(remembered, o.Remembered...)
	}
	var lki *state.Object
	var lkiPower, lkiToughness int32
	var lkiPTValid bool
	if snapshot, ok := e.triggerLKI[stack]; ok && snapshot.object != nil {
		lki = snapshot.object
		lkiPower, lkiToughness, lkiPTValid = snapshot.power, snapshot.toughness, snapshot.ptValid
	}
	var svars map[string]string
	if o := e.G.Obj(source); o != nil && o.Face() != nil {
		svars = o.Face().SVars
	}
	var stackObj *state.Object
	if o := e.G.Obj(stack); o != nil {
		stackObj = o
	}
	x := int32(0)
	if stackObj != nil {
		x = stackObj.X
		if stackObj.Ability != nil && x == 0 {
			x = e.triggerPaidX(stack, stackObj)
		}
	}
	// At the placement ask an activation may not yet have a stack object.
	// Its announced X must take precedence over the stack/trigger value.
	if e.cast != nil && (e.cast.card == source || e.cast.stackObj == source) {
		x = e.cast.x
	}
	// Resolve's captured referents live in ONE heap record (specResolveEnv)
	// rather than as separately heap-moved closure variables. The count Ctx
	// is only needed when Resolve evaluates an SVar body, which most target
	// offers never do; it is built on first use (once, and then reused
	// exactly as the eager one was). Every field it reads is final by now, so
	// a late build is identical to an eager one.
	//
	// The record itself is one of the engine's reusable envs (acquireSpecEnv):
	// the caller must defer e.releaseSpecEnv() once it is done with the
	// returned SpecContext.
	srcLKI, srcLKIValid := e.sourceCharLKIFor(stack, source, lki, lkiPower, lkiToughness, lkiPTValid)
	env := e.acquireSpecEnv()
	*env = specResolveEnv{e: e, source: source, you: you, tcx: tcx, remembered: remembered,
		svars: svars, lki: lki, lkiPower: lkiPower, lkiToughness: lkiToughness, lkiPTValid: lkiPTValid, x: x,
		srcLKI: srcLKI, srcLKIValid: srcLKIValid, resolveFn: env.resolveFn}
	sc := effects.NewSpecContext(you, source)
	sc.TriggerContext, sc.Remembered, sc.Resolve = tcx, remembered, env.resolveFn
	// The stack object's Remembered and fire-time LKI are bound above, beside
	// TriggerContext, so placement-time numeric SVars read the same captured
	// referents the resolving Ctx receives later.
	// withNames binds the layer-3 rename table and the layer-4 derived-type
	// table (layer4types.go) so the target OFFER sees a granted/removed type,
	// exactly as the layer walk's own Affected$ match does; without it a
	// Goblin-killer would not offer a creature a static made a Goblin (the
	// AGENTS.md row this closes).
	return e.withNames(sc)
}
