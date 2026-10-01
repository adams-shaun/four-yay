package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	Register("DealDamage", effDealDamage)
	Register("DamageAll", effDamageAll)
	Register("EachDamage", effEachDamage)
	Register("Fight", effFight)
	Register("DamageResolve", effDamageResolve)
}

// roundRobinSplit is DealDamage's R-9 no-host (and AskEmpty) stand-in for a
// DividedAsYouChoose$ allocation: distribute one damage at a time over the
// chosen-target list in order, exactly the deterministic split this build
// shipped before the ask existed. The returned slice is indexed by target
// position (the same order the ask's options use), so the caller reads it
// positionally whatever path filled it.
func roundRobinSplit(n int, total int32) []int32 {
	split := make([]int32, n)
	if n <= 0 {
		return split
	}
	for i := int32(0); i < total; i++ {
		split[i%int32(n)]++
	}
	return split
}

// effDealDamage implements "SP$/AB$/DB$ DealDamage" against players and
// permanents. Absorbed from Task 14's stopgap (formerly primitives.go, now
// folded in here): the negative-NumDmg clamp and its default of 0 (not 1) are
// Ruling T14-f, kept verbatim -- events.Apply's Damage case is a plain
// subtraction from Life, so an unclamped negative value would heal instead of
// doing nothing, and TestDealDamageDefaultsMissingNumDmgToZero already locks
// in the zero default.
//
// Folded in on top of that stopgap: a permanent that has already left the
// battlefield (destroyed or sacrificed in response, say) is not a legal
// recipient any more -- the effect does nothing to it rather than marking
// damage on a card sitting in a graveyard. See the Task 18 report for how
// this (and Destroy/ChangeZone/Sacrifice/Counter, which can make exactly that
// happen) interacts with CR 608.2b target rechecking.
//
// CR 609.7a provenance: the damage's source is DamageSource$ when the script
// names one, the resolving source otherwise, and SetDamageSource makes that
// object the one rules' protection check and DamageDone triggers read for
// every Damage event this loop emits (the rider and the engine override
// cannot disagree -- both come from the one resolved rider source).
func effDealDamage(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "NumDmg", 0)
	if n < 0 {
		n = 0
	}
	// ReplaceDyingDefined$ <list> (Chandra, Awakened Inferno's "If a permanent
	// dealt damage this way would die this turn, exile it instead", Wilt in
	// the Heat's Targeted form): registered once the damage batch has landed,
	// over the objects this resolution actually damaged/targeted. The deferred
	// call runs after EndDamageBatch on either path.
	var dying []state.Target
	defer func() { registerReplaceDying(h, c, sa, dying) }()
	// CR 702.15a: damage dealt by a source with lifelink causes that source's
	// controller to gain that much life. Every non-combat emit site below pays
	// the rider through one damageRider; combat has its own rider in
	// rules/combat.go's damage step (the reference shape this class was
	// modelled on).
	rider := newDamageRider(h, c, sa, n)
	prev := h.SetDamageSource(rider.source)
	defer h.SetDamageSource(prev)
	// A multi-target DealDamage is one simultaneous damage event for triggers
	// such as Ob Nixilis's LifeLostAll. The optional hook keeps effects below
	// rules in the package graph; test hosts that do not model trigger queues
	// simply do not implement it.
	if b, ok := h.(interface {
		BeginLifeLossBatch()
		EndLifeLossBatch()
	}); ok {
		b.BeginLifeLossBatch()
		defer b.EndLifeLossBatch()
	}
	// RememberDamaged$ True makes the resolution remember the objects it
	// damaged, so a SubAbility$ (Incinerate's DB$ Effect reading
	// RememberObjects$ Remembered.Creature) can act on exactly what took the
	// damage. Ctx is threaded by pointer through Resolve, so appending here is
	// visible to the sub-ability without any state write -- the remembered set
	// is per-resolution context, not game state, and replay re-derives it by
	// re-running the same resolution (Task ce1).
	remember := strings.TrimSpace(sa.Params["RememberDamaged"]) != ""
	// DividedAsYouChoose$ N (Fury's "deals 4 damage divided as you choose
	// among any number of target creatures and/or planeswalkers", Forked
	// Bolt): the NAMED TOTAL is divided among the chosen targets, not dealt
	// to each. The player's own division is a real mid-resolution ask: one
	// KChoose option per chosen target, Min == Max == the named total and
	// Repeatable, so the answer is a multiset whose per-target multiplicities
	// are the shares (a target that receives nothing is simply never picked).
	// The decision's own Min/Max/Repeatable wire rules already constrain the
	// answer to exactly the named total over exactly the chosen target list,
	// so no new Validate rule is needed. The deterministic R-9 no-host
	// stand-in (and the AskEmpty arm) distributes one damage at a time,
	// round-robin in the chosen-target order, so the batch total is exactly
	// N. Targets beyond N take nothing, as an unchosen target would.
	divided := false
	var total int32
	if raw, ok := sa.Params["DividedAsYouChoose"]; ok && strings.TrimSpace(raw) != "" {
		divided = true
		total = Num(h, c, sa, "DividedAsYouChoose", 0)
		if total < 0 {
			total = 0
		}
	}
	// The chosen-target list both the division ask and the emission walk
	// read, in Defined$ order; the option index of a target is exactly its
	// position here, so the answer's multiplicities land on the right
	// recipient.
	type divTarget struct {
		obj    state.ObjID
		player state.PlayerID
	}
	var divTargets []divTarget
	if divided {
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				divTargets = append(divTargets, divTarget{player: t.Player})
				continue
			}
			if o := h.Game().Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
				divTargets = append(divTargets, divTarget{obj: t.Obj})
			}
		}
		if !c.DamageSplitDone {
			// Only a division with something to divide is asked; an empty
			// target list or a total of 0 is the silent no-op it has always
			// been. A SINGLE legal target has exactly one legal answer (all of
			// the total), so the ask is filled directly rather than posed --
			// never a decision nobody could answer differently (the
			// strict-supersets rule putCounterChoose and bolster share).
			if len(divTargets) > 1 && total > 0 {
				opts := make([]decision.Option, 0, len(divTargets))
				for i, t := range divTargets {
					o := decision.Option{Index: i, Player: c.Controller}
					if t.obj != 0 {
						o.Kind, o.Obj = "card", t.obj
						if gobj := h.Game().Obj(t.obj); gobj != nil && gobj.Face() != nil {
							o.Label = gobj.Face().Name
						}
					} else {
						o.Kind = "player"
						o.Player = t.player
						if g := h.Game(); int(t.player) < len(g.Players) {
							o.Label = g.Players[t.player].Name
						}
					}
					opts = append(opts, o)
				}
				d := &decision.Decision{Player: c.Controller, Kind: decision.KChoose,
					Min: int(total), Max: int(total), Options: opts, Repeatable: true,
					ResumeKind: "damage_split", ResumeSA: sa, Source: c.Source,
					Prompt: "Assign " + strconv.Itoa(int(total)) + " damage"}
				if Ask(h, d) == AskAsked {
					// Suspended: rules' "damage_split" resume arm fills
					// Ctx.DamageSplit from the answered multiset and re-enters
					// this SA, which then emits with the player's shares. The
					// damage batch is not open yet, so the suspension leaves
					// nothing half-emitted.
					return
				}
				// No host (R-9): the deterministic round-robin stand-in.
				c.DamageSplit = roundRobinSplit(len(divTargets), total)
			} else if len(divTargets) == 1 && total > 0 {
				// The sole target must receive the whole total: filling the
				// split directly keeps the positional emission loop below
				// honest without posing an unanswerable decision.
				c.DamageSplit = []int32{total}
			}
			c.DamageSplitDone = true
		}
	}
	// ExcessSVar$ <name> (CR 120.10): the damage this call deals BEYOND what
	// was lethal to each permanent is published under <name> for the chained
	// SubAbility$ to read (Bottle-Cap Blast's TokenAmount$ Excess, Cramped
	// Vents' LifeAmount$ Excess, Nahiri's Warcrafting's DigNum$ X). The
	// publication is a Ctx.SVars binding -- resolution-scoped scratch, never
	// an event -- which every consumer (effects.Num, EvalCount's SVar$
	// indirection, resolveNumericRHS, CheckSVarHolds) already reads. The
	// condition gates it when present; a condition this build cannot
	// evaluate fails CLOSED (no bind), the conservative direction.
	excessName := strings.TrimSpace(sa.Params["ExcessSVar"])
	excessCond := strings.TrimSpace(sa.Params["ExcessSVarCondition"])
	// bindExcess publishes max(0, dealt - lethal) under the card's name.
	// `lethal` is captured by the caller BEFORE the damage lands -- CR 120.10
	// measures excess against the lethal amount, and marked damage counts
	// toward it, so reading toughness/remaining loyalty after the hit would
	// double-count the damage just dealt.
	bindExcess := func(o *state.Object, lethal, dealt int32) {
		if excessName == "" || o == nil {
			return
		}
		if !excessConditionHolds(h, c, excessCond, o) {
			return
		}
		excess := dealt - lethal
		if excess < 0 {
			excess = 0
		}
		c.SVars[excessName] = strconv.Itoa(int(excess))
	}
	// A DamageSource$ spec that resolves to SEVERAL objects makes each of
	// them a separate damager (emitFromEachSource below); a resolved set of
	// one keeps the single-rider path below byte-identical to what it was.
	var multiSources []state.ObjID
	if spec := strings.TrimSpace(sa.Params["DamageSource"]); spec != "" {
		if ts, ok := damageSourceSpecTargets(h, c, spec); ok {
			seen := make(map[state.ObjID]bool)
			for _, t := range ts {
				if t.IsPlayer || t.Obj == 0 {
					continue
				}
				id := resolveSourceObject(h, t.Obj)
				if !seen[id] {
					seen[id] = true
					multiSources = append(multiSources, id)
				}
			}
		}
	}
	// DamageMap$ True (Forge's AbilityFactoryDealDamage "addDamage" into the
	// resolution's damage map, flushed later by DB$ DamageResolve) MARKS this
	// call's damage instead of dealing it. The marks ride Ctx.PendingDamage --
	// the resolution-scratch class of Remembered/SVars, never event-encoded:
	// a replay re-derives them by re-running the same resolution -- and the
	// DamageResolve primitive flushes them as ONE damage batch (an unresolved
	// mark deals nothing, Forge's own semantics for a script that marks
	// without resolving). Per-CALL marking is the reading this build adopts:
	// Forge shares one map on the root ability's resolution, but which root SA
	// a given script's marks share is not verifiable from the scripts here,
	// and per-call marking reproduces every observable the corpus needs (the
	// flush is one batch; a bare flush is silent). A call this shape cannot
	// mark faithfully -- a DividedAsYouChoose distribution or a multi-source
	// DamageSource$ arm, neither of which any DamageMap line in the corpus
	// carries -- falls through to the immediate-deal path unchanged.
	if strings.EqualFold(strings.TrimSpace(sa.Params["DamageMap"]), "True") && !divided && len(multiSources) < 2 {
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				c.PendingDamage = append(c.PendingDamage, PendingDamage{rider: rider,
					target: state.Target{Player: t.Player, IsPlayer: true}})
				continue
			}
			if o := h.Game().Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
				c.PendingDamage = append(c.PendingDamage, PendingDamage{rider: rider,
					target: state.Target{Obj: t.Obj}})
			}
		}
		return
	}
	// One DealDamage call is ONE damage batch (Forge dealDamage): the events
	// this loop emits latch the DamageDealtOnce/DamageDoneOnce triggers
	// together, so a multi-target hit triggers the source's DealtOnce ability
	// once with the batch total and each target's DoneOnce ability once with
	// what that target took. The host opens the batch; emit opens a batch of
	// one for a Damage event that arrives with none open, so a call this
	// primitive never brackets (none today) still latches per event.
	h.BeginDamageBatch()
	if !divided && emitFromEachSource(h, c, sa, multiSources, n, remember, &dying, bindExcess) {
		h.EndDamageBatch()
		return
	}
	if divided {
		// The split is consumed by this one emission walk: a resolution that
		// runs DealDamage with DividedAsYouChoose$ twice (a RepeatEach body or
		// two chained divided subs) must ask again for the second call rather
		// than silently reuse the first call's shares against a different
		// target list. Reset after the walk, on every return path below.
		defer func() {
			c.DamageSplit = nil
			c.DamageSplitDone = false
		}()
		for i, t := range divTargets {
			amt := int32(0)
			if i < len(c.DamageSplit) {
				amt = c.DamageSplit[i]
			}
			if amt <= 0 {
				continue
			}
			r := rider
			r.amount = amt
			if t.obj != 0 {
				if o := h.Game().Obj(t.obj); o != nil {
					lethal, ok := excessLethal(h, o)
					dealt := emitObjectDamage(r, t.obj)
					if ok {
						bindExcess(o, lethal, dealt)
					}
				}
				if remember {
					c.Remembered = append(c.Remembered, state.Target{Obj: t.obj})
					eventRemember(h, c, t.obj)
				}
				dying = append(dying, state.Target{Obj: t.obj})
				continue
			}
			emitPlayerDamage(r, t.player)
			if remember {
				rememberPlayerBothHalves(h, c, t.player)
			}
		}
		h.EndDamageBatch()
		return
	}
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			emitPlayerDamage(rider, t.Player)
			if remember {
				rememberPlayerBothHalves(h, c, t.Player)
			}
			continue
		}
		if o := h.Game().Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
			lethal, ok := excessLethal(h, o)
			dealt := emitObjectDamage(rider, t.Obj)
			if ok {
				bindExcess(o, lethal, dealt)
			}
			if remember {
				c.Remembered = append(c.Remembered, state.Target{Obj: t.Obj})
				eventRemember(h, c, t.Obj)
			}
			dying = append(dying, state.Target{Obj: t.Obj})
		}
	}
	h.EndDamageBatch()
}

// emitFromEachSource is DealDamage's multi-source arm: a DamageSource$ spec
// that resolves to SEVERAL objects makes each of them a separate damager over
// the recipients (Missy's "each artifact creature you control deals 1 damage
// to that opponent", Judgment of Alexander's "each commander creature you
// control deals damage equal to its power") -- one rider per source with
// SetDamageSource published around each one's pass (the lifelink rider,
// deathtouch mark, protection and the DamageDone triggers all read THAT
// source), inside the caller's single damage batch, the shape Forge's
// DamageDealEffect per-source loop builds. RelativeTarget$ True re-resolves
// the recipients PER SOURCE through Defined$ anchored on that source (aura
// barbs: each enchantment damages its own controller, each attached Aura the
// creature it is attached to), with a per-source ctx holding only that
// source's identity -- the same shape effEachDamage's per-damager Ctx takes;
// a relative arm whose per-source recipients resolve to nothing contributes
// that source's nothing (fail closed inside, never a guessed pairing). A
// source no longer on the battlefield contributes nothing (Forge reads its
// LKI; the corpus multi-source lines all name live battlefield permanents, so
// the narrower liveness rule is unreachable there and is recorded in the
// ticket report). Returns false when the arm did not run -- fewer than two
// resolvable sources, a shared recipient arm that resolved to nothing, or a
// DividedAsYouChoose resolution (no corpus line combines the two) -- so the
// caller falls through to its single-rider path unchanged.
func emitFromEachSource(h Host, c *Ctx, sa *cards.SA, sources []state.ObjID, n int32,
	remember bool, dying *[]state.Target, bindExcess func(o *state.Object, lethal, dealt int32)) bool {
	if len(sources) < 2 {
		return false
	}
	relative := strings.TrimSpace(sa.Params["RelativeTarget"]) != ""
	var shared []state.Target
	if !relative {
		shared = Defined(h, c, sa)
		if len(shared) == 0 {
			return false
		}
	}
	// The per-source rider must not re-resolve the DamageSource$ spec (it
	// would collapse every source onto the first match again), so it reads a
	// clone of the SA's params without the key.
	saRider := cards.SA{Line: sa.Line, Params: make(map[string]string, len(sa.Params))}
	for k, v := range sa.Params {
		if k == "DamageSource" {
			continue
		}
		saRider.Params[k] = v
	}
	emittedAny := false
	for _, src := range sources {
		o := h.Game().Obj(src)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		pc := &Ctx{Source: src, Controller: o.Controller, SVars: c.SVars}
		recips := shared
		if relative {
			recips = Defined(h, pc, sa)
			if len(recips) == 0 {
				continue
			}
		}
		rider := newDamageRider(h, pc, &saRider, n)
		prev := h.SetDamageSource(rider.source)
		for _, t := range recips {
			if t.IsPlayer {
				emitPlayerDamage(rider, t.Player)
				if remember {
					rememberPlayerBothHalves(h, c, t.Player)
				}
				emittedAny = true
				continue
			}
			if to := h.Game().Obj(t.Obj); to != nil && to.Zone == state.ZBattlefield {
				lethal, ok := excessLethal(h, to)
				dealt := emitObjectDamage(rider, t.Obj)
				if ok {
					bindExcess(to, lethal, dealt)
				}
				if remember {
					c.Remembered = append(c.Remembered, state.Target{Obj: t.Obj})
					eventRemember(h, c, t.Obj)
				}
				*dying = append(*dying, state.Target{Obj: t.Obj})
				emittedAny = true
			}
		}
		h.SetDamageSource(prev)
	}
	return emittedAny
}

// registerReplaceDying implements DealDamage's ReplaceDyingDefined$ <list>
// ("If a permanent dealt damage this way would die this turn, exile it
// instead"): every PERMANENT this resolution damaged joins one continuous
// Moved replacement (battlefield-to-graveyard becomes exile) that lasts
// until end of turn (CR 614.9's "this turn" half — the replacement must
// outlive the resolving spell, which is why it rides the continuous registry
// rather than the resolution's own Ctx). The list name is Forge's
// Defined-list selector: "Remembered" (Chandra's RememberDamaged$ shape)
// reads the resolution's remembered set, "Targeted" (Wilt in the Heat's) its
// chosen targets; an optional ".<filter>" qualifier (Burn from Within's
// Remembered.Creature) narrows the list through the ordinary filter grammar.
// The replacement's ValidCard$ Card.IsRemembered is matched against the
// registered ContinuousEffect's Remembered snapshot (rules carries the list
// into the match), so a Cleanup ClearRemembered$ rider (Chandra's DBCleanup)
// cannot cut the replacement's legs out from under it, and the With body is
// the same Defined$ ReplacedCard exile the Kumano-family R: lines carry. A
// list name this grammar does not know is loud rather than silently inert.
func registerReplaceDying(h Host, c *Ctx, sa *cards.SA, damaged []state.Target) {
	die := strings.TrimSpace(sa.Params["ReplaceDyingDefined"])
	if die == "" || len(damaged) == 0 {
		return
	}
	base, qualifier, _ := strings.Cut(die, ".")
	if base != "Remembered" && base != "Targeted" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "unrecognised ReplaceDyingDefined$ " + die})
		return
	}
	var ids []state.ObjID
	for _, t := range damaged {
		if t.IsPlayer || t.Obj == 0 {
			continue
		}
		if qualifier != "" && !MatchesSpecCtx(h.Game(), qualifier, t.Obj, c.SpecContext(c.Controller)) {
			continue
		}
		ids = append(ids, t.Obj)
	}
	if len(ids) == 0 {
		return
	}
	h.AddContinuous(state.ContinuousEffect{
		Source: c.Source, Controller: c.Controller,
		UntilEOT: true, Duration: "UntilEOT",
		ReplacementEvent: "Moved",
		ReplacementParams: map[string]string{
			"Origin":      "Battlefield",
			"Destination": "Graveyard",
			"ValidCard":   "Card.IsRemembered",
		},
		ReplacementBody: "DB$ ChangeZone | Defined$ ReplacedCard | Origin$ Battlefield | Destination$ Exile",
		Remembered:      ids,
	})
}

// damageRider bundles the facts every non-combat damage emit site shares: the
// host, the dealing source, that source's controller, and the amount. Passing
// it through one struct is what makes the lifelink rider impossible to forget
// at a new site -- a new emitter takes a rider, and payLifelinkRider runs for
// it unless the emitter can prove the damage did not land.
// damageRider carries the damage-source facts one resolution's emits share.
// The three keyword bits are resolved ONCE, in the constructor, so every emit
// below reads the same answer -- and so a source that left while the
// resolution waited reads CR 113.7a's last known characteristics rather than
// a live board that has already stripped a granted keyword.
type damageRider struct {
	h             Host
	source        state.ObjID
	controller    state.PlayerID
	amount        int32
	hasLifelink   bool
	hasInfect     bool
	hasWither     bool
	hasDeathtouch bool
}

// resolveSourceObject unwraps one object id to the permanent a damage rider
// gates on: an ability stack object (minted by AbilityPush/TriggerPush, Card
// == nil, no Face) reads nothing off itself, so the keywords lifelink and
// deathtouch belong to the SOURCE permanent that pushed it (CR 607.2). A
// spell's own stack object IS the card and is read as-is. Same rule
// rules.Engine.protectionSource applies (Task 15 fix round 1, Critical C2).
func resolveSourceObject(h Host, id state.ObjID) state.ObjID {
	if o := h.Game().Obj(id); o != nil && o.Ability != nil && o.Source != 0 {
		return o.Source
	}
	return id
}

// damageSourceSpecTargets resolves one DamageSource$ spec to the objects the
// damage is dealt FROM (CR 609.7a provenance). definedSpec first: the
// spellings every other object reference uses (TriggeredCard, Targeted,
// ChosenCard, the bare Valid battlefield walk, Imprinted's RepeatEach
// subject) resolve exactly there, so a damage source cannot disagree with
// the same spelling's own Defined$ read. Two damage-local extensions follow,
// both reading the way Forge's getDefinedCards serves this parameter:
//
//   - Imprinted whose definedSpec set came back empty re-reads the source's
//     imprint associations RAW (rawImprintTargets). definedSpec's Defined$
//     Imprinted keeps its CR 607.2a exile gate for its own callers; Forge's
//     getImprintedCards has no zone gate, and the corpus's non-repeat
//     DamageSource$ Imprinted line (Enchanter's Bane) imprints a BATTLEFIELD
//     permanent, so the gated read would drop the one source the script names.
//   - Spawner> <inner> is Forge's adjustTriggerContext re-anchor: the rest of
//     the chain resolves against the resolving ability's TRIGGER's spawning
//     ability. This build's stand-in for that context is the firing trigger's
//     own event capture (Ctx.Captured), which a chained ImmediateTrigger's
//     Execute context no longer carries as Remembered (effImmediateTrigger
//     hands its instances the capture-excluded parent set). Halana, Kessig
//     Ranger is the one DamageSource$ user. An inner spec definedSpec does
//     not know still fails closed.
//
// The bool keeps definedSpec's contract: false means this spec names nothing
// this build models, and the caller fails closed (the damage keeps the
// resolving source, loudly). true with an EMPTY set is a recognised spelling
// whose referent is absent -- the caller keeps the resolving source silently,
// exactly as it does for every recognised-but-absent referent.
func damageSourceSpecTargets(h Host, c *Ctx, spec string) ([]state.Target, bool) {
	if ts, ok := definedSpec(h, c, spec); ok {
		if spec != "Imprinted" || hasObjectTarget(ts) {
			return ts, true
		}
		if raw := rawImprintTargets(h.Game(), c); len(raw) > 0 {
			return raw, true
		}
		return ts, true
	}
	if inner, ok := spawnerChain(spec); ok { // shared Spawner> strip (spawnercontrol)
		sc := *c
		sc.Remembered = copyTargets(c.Captured)
		return definedSpec(h, &sc, inner)
	}
	return nil, false
}

// hasObjectTarget reports whether the set carries at least one non-player
// object entry -- the only entries a damage rider can deal from.
func hasObjectTarget(ts []state.Target) bool {
	for _, t := range ts {
		if !t.IsPlayer && t.Obj != 0 {
			return true
		}
	}
	return false
}

// rawImprintTargets is the ungated imprint-pile read Forge's
// getImprintedCards hands its damage-source and count consumers: every object
// the resolving source's Imprinted/ImprintTokens/SeekFound associations name
// that still exists, in association order, whatever zone it sits in.
// definedSpec's Defined$ Imprinted read keeps its CR 607.2a exile gate for
// its own callers; these readers do not inherit it.
func rawImprintTargets(g *state.Game, c *Ctx) []state.Target {
	o := g.Obj(c.Source)
	if o == nil {
		return nil
	}
	var out []state.Target
	for _, id := range o.Imprinted {
		if g.Obj(id) != nil {
			out = append(out, state.Target{Obj: id})
		}
	}
	for _, id := range o.ImprintTokens {
		if g.Obj(id) != nil {
			out = append(out, state.Target{Obj: id})
		}
	}
	for _, id := range o.SeekFound {
		if g.Obj(id) != nil {
			out = append(out, state.Target{Obj: id})
		}
	}
	return out
}

// newDamageRider builds the rider for one resolving DealDamage/DamageAll.
// The source is DamageSource$ when the script names one -- resolved through
// damageSourceSpecTargets, whose definedSpec arm is the same resolver every
// other object reference uses (Scourge of Valkas' TriggeredCard, Kiku's
// Shadow's Targeted), never guessed at the chosen targets -- and the
// resolving source otherwise. A DamageSource$ the resolver genuinely cannot
// model fails closed LOUDLY: the damage keeps the resolving source (the
// conservative direction -- damage from the resolving spell/ability's
// source, never damage silently attributed to a target) and the Note says
// so in the log. Resolving here, once per resolution, keeps the next rider
// honest: every keyword read below reads r.source, already resolved, and
// SetDamageSource publishes the same object.
func newDamageRider(h Host, c *Ctx, sa *cards.SA, amount int32) damageRider {
	own := resolveSourceObject(h, c.Source)
	source := own
	if spec := strings.TrimSpace(sa.Params["DamageSource"]); spec != "" {
		if ts, ok := damageSourceSpecTargets(h, c, spec); ok {
			for _, t := range ts {
				if t.IsPlayer || t.Obj == 0 {
					continue
				}
				source = resolveSourceObject(h, t.Obj)
				break
			}
		} else {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "unresolvable DamageSource$ " + spec + "; damage keeps the resolving source"})
		}
	}
	hasLifelink := h.HasKeyword(source, "Lifelink")
	hasInfect := h.HasKeyword(source, "Infect")
	hasWither := h.HasKeyword(source, "Wither")
	hasDeathtouch := h.HasKeyword(source, "Deathtouch")
	// CR 608.2h: a source that left while this resolution waited uses LKI.
	// The own-source fields cover the independently resolving ability's own
	// permanent; DamageSourceLKI covers a distinct named source such as
	// TriggeredCard or Remembered. Live battlefield state always wins, so a
	// detached lifelink grant is not retained after a source stays in play.
	live := false
	if o := h.Game().Obj(source); o != nil && o.Zone == state.ZBattlefield {
		live = true
	}
	controller := c.Controller
	if !live {
		lki, named := c.DamageSourceLKI[source]
		if named {
			// CR 113.7a covers the whole damage rider, not just lifelink: the
			// same departure walk seeds this map for the resolution's OWN
			// source as well as a named DamageSource$ object, so it is the one
			// home for infect and deathtouch. Lifelink and controller keep the
			// own-source fields' older precedence below.
			hasInfect, hasWither, hasDeathtouch = lki.Infect, lki.Wither, lki.Deathtouch
		}
		switch {
		case source == own && c.SourceLifelinkLKIValid:
			hasLifelink = c.SourceLifelinkLKI
			controller = c.SourceControllerLKI
			if !c.SourceControllerLKIValid {
				controller = c.Controller
			}
		case named:
			hasLifelink = lki.Lifelink
			controller = lki.Controller
		default:
			if o := h.Game().Obj(source); o != nil {
				// A non-permanent source (for example a spell on the stack)
				// still has a live controller even though it is not a
				// battlefield object.
				controller = o.Controller
			}
		}
	} else if o := h.Game().Obj(source); o != nil {
		controller = o.Controller
	}
	return damageRider{h: h, source: source, controller: controller,
		amount: amount, hasLifelink: hasLifelink, hasInfect: hasInfect,
		hasWither: hasWither, hasDeathtouch: hasDeathtouch}
}

// payLifelinkRider is CR 702.15a's life gain for NON-COMBAT damage: when
// damage from a source with lifelink actually landed, the source's controller
// gains `r.amount` life. Lifelink is not a trigger (CR 702.15a) -- the
// LifeChange rides the damage emit itself, so the gain is visible at the same
// instant the damage is, with no stack, no trigger queue and no ask.
//
// dealt is the amount on the Damage event that actually landed after
// replacement effects. It is zero for prevention, and may differ from the
// proposed amount for a multiplier or other amount-changing replacement.
func payLifelinkRider(r damageRider, dealt int32) {
	if dealt <= 0 || !r.hasLifelink {
		return
	}
	r.h.Emit(events.Event{Kind: events.LifeChange, Player: r.controller,
		Amount: dealt})
}

// emitObjectDamage marks the shared SBA witness only when EmitDamage returns
// positive applied damage. The same returned result gates and prices lifelink,
// so prevention pays neither rider and amount replacement prices both from the
// event that actually landed.
//
// CR 306.8's planeswalker loyalty exchange happens in rules.Engine.emit,
// after this proposed Damage has traversed the same prevention/replacement
// pipeline as every other recipient. EmitDamage still returns the final
// Damage shape so lifelink and deathtouch consume the replaced amount. A
// creature-planeswalker still needs its Counter "creature" tag set on the
// proposed event (events.Apply cannot import rules' layer engine, so it
// cannot discover the animation itself) so it gets marked damage as well as
// loyalty loss; printed creatures and plain planeswalkers are unaffected by
// the tag.
func emitObjectDamage(r damageRider, target state.ObjID) int32 {
	h := r.h
	o := h.Game().Obj(target)
	if o == nil {
		return 0
	}
	ev := events.Event{Kind: events.Damage, Obj: target, Amount: r.amount}
	creature := h.IsCreature(target)
	if creature && r.hasInfect {
		// CR 702.90b: a CREATURE recipient takes infect damage as -1/-1
		// counters. The compound marker rides Damage's Counter carrier and
		// carries BOTH facts its consumers need -- the source's infect (rules'
		// conversion emits the real CounterChange right after this event
		// folds, through the same replacement/trigger pipeline) and the
		// recipient's creature classification by layer state, which the fold
		// cannot evaluate (its printed-face fallback covers only printed
		// creatures; this tag covers an animated planeswalker too). Only a
		// creature recipient is tagged: an artifact, a Battle or a printed
		// planeswalker takes the hit as ordinary damage, untagged. A granted
		// infect (e.g. a Grafted Exoskeleton bearer) reads the same, because
		// Host.HasKeyword reads the derived keyword list.
		ev.Counter = "infect+creature"
	} else if r.hasWither {
		// Keep the source fact on every recipient; Engine.emit recomputes the
		// recipient half after DamageDone redirects. This is required when a
		// player hit is redirected onto a creature (and when a creature hit is
		// redirected away from one).
		ev.Counter = "wither"
	} else if creature && o.Face() != nil && o.Face().IsPlaneswalker() && !o.Face().IsCreature() {
		ev.Counter = "creature"
	}
	applied := h.EmitDamage(ev)
	dealt := int32(0)
	if applied.Kind == events.Damage {
		dealt = applied.Amount
	}
	if dealt > 0 && applied.Obj != 0 && r.hasDeathtouch {
		h.Emit(events.Event{Kind: events.CounterChange, Obj: applied.Obj,
			Counter: "Deathtouched", Amount: 1})
	}
	payLifelinkRider(r, dealt)
	return dealt
}

// emitPlayerDamage lands one non-combat Damage event on a player and pays the
// lifelink rider from the amount that survived replacement effects.
func emitPlayerDamage(r damageRider, target state.PlayerID) {
	ev := events.Event{Kind: events.Damage, Player: target, Amount: r.amount}
	if r.hasInfect {
		// CR 702.90b: damage from an infect source is dealt to a player in
		// the form of that many poison counters; the fold converts it.
		ev.Counter = "infect"
	} else if r.hasWither {
		// Keep the source fact even though Wither damage to a player is
		// ordinary. A DamageDone replacement can redirect this event to a
		// creature, where Engine.emit recomputes it into counter form.
		ev.Counter = "wither"
	}
	applied := r.h.EmitDamage(ev)
	dealt := int32(0)
	if applied.Kind == events.Damage {
		dealt = applied.Amount
	}
	payLifelinkRider(r, dealt)
}

// excessLethal is the amount of damage that is LETHAL to one permanent, read
// BEFORE the damage lands (CR 120.10 measures excess against the damage that
// was lethal, and already-marked damage counts toward it): a creature's
// derived toughness minus its marked damage, a planeswalker's remaining
// loyalty. Anything else -- an artifact, land or enchantment has no lethal
// threshold; a Battle's defence-counter threshold is not modelled here -- has
// no excess computation, so ok is false and NO value is bound (the
// conservative direction: the sub reads its own default rather than an
// invented number). Damage to a PLAYER never reaches here: excess is not
// defined against a player.
func excessLethal(h Host, o *state.Object) (int32, bool) {
	switch {
	case h.IsCreature(o.ID):
		lethal := h.Toughness(o.ID) - o.Damage
		if lethal < 0 {
			lethal = 0
		}
		return lethal, true
	case o.Face() != nil && o.Face().IsPlaneswalker():
		loyalty := o.Counter("LOYALTY")
		if loyalty < 0 {
			loyalty = 0
		}
		return loyalty, true
	default:
		return 0, false
	}
}

// excessConditionHolds evaluates DealDamage's ExcessSVarCondition$ gate. The
// corpus writes exactly three spellings; `targetedBy` is not part of the
// general filter grammar, so the three are matched structurally and an
// unrecognised condition fails CLOSED with one loud Note (no bind), rather
// than binding an excess the card's clause never asked for.
func excessConditionHolds(h Host, c *Ctx, cond string, o *state.Object) bool {
	if cond == "" {
		return true
	}
	switch cond {
	case "Card.targetedBy", "Creature.targetedBy", "Permanent.targetedBy":
		// The damaged object is necessarily a chosen target of this
		// resolution (Defined resolves the targets effDealDamage damages),
		// so the targetedBy half is structural truth here.
		return true
	case "Creature":
		return h.IsCreature(o.ID)
	case "Creature.targetedBy+OppCtrl":
		return h.IsCreature(o.ID) && o.Controller != c.Controller
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "unread DealDamage ExcessSVarCondition " + cond})
		return false
	}
}

// effFight implements "SP$/AB$/DB$ Fight" (CR 701.12): each fighter deals
// damage equal to its power to the creature(s) it fights, and each fought
// creature deals its power back, SIMULTANEOUSLY -- one Damage emission per
// direction, both inside ONE BeginDamageBatch/EndDamageBatch bracket so
// DamageDealtOnce/DamageDoneOnce triggers latch the whole exchange the way
// effDealDamage's multi-target batch does. The damage is NOT combat damage
// (CR 701.12a): the ordinary effects-path EmitDamage below is already
// non-combat, and Fight is never routed through rules' combat step.
//
// Fight is the one primitive whose SA carries TWO independent target lists:
// Defined$ names the FIGHTER(S) (resolved through the ordinary Defined
// resolver -- every corpus Fight Defined$ selector is already supported),
// ValidTgts$ names the creatures they fight. The generic machinery conflates
// them, so the opponents come from the CHOSEN targets: the pre-ask's answered
// set (c.PickedTargets, non-nil only while the pre-asked body dispatches)
// outranks the placement ask's Ctx.Targets, the same precedence Defined's own
// ValidTgts fallthrough uses. An SA with NO Defined$ (Blood Feud's "Target
// creature fights another target creature", the four SP$ Fight lines) is the
// degenerate case where both lists are the chosen targets: the pairwise walk
// below pairs them against each other, which is exactly that card text. The
// pre-ask for those already ran in effects.Resolve's dispatch (no Defined$
// means the ordinary ValidTgts$ ask), and the Defined$-carrying sub-shaped
// Fight (Kraul Harpooner) gets its own ask from chosenTargetsFor's Fight
// carve-out; the execute-shaped ones (Warbriar Blessing) and modal Charm-mode
// ones (Voracious Hydra) had their placement ask cover them.
//
// Per-side damage source: each hit's source is the FIGHTING CREATURE, not the
// resolving spell/ability -- a lifelinked fighter's controller gains its
// power, a deathtouched wound tags the victim, and DamageDone triggers see
// the creature. One damageRider per fighter (built off the fighter's LIVE
// controller -- both sides are battlefield objects by the guard below, so no
// LKI path is reachable), with SetDamageSource toggled around each emission
// and restored, since one batch here spans two sources (a shape one
// effDealDamage call never reaches).
//
// Power is the DERIVED power (h.Power) per the Host contract; zero power is a
// legal deal (the effDealDamage n<=0 shape -- the event emits with Amount 0,
// prevention and both riders naturally no-op). A fighter or opponent no
// longer on the battlefield at resolution is skipped; an empty opponent set
// (the optional-target decline, or no eligible creature at all) is a silent
// no-op -- no note, no event. ReplaceDyingDefined$ (faunsbane_troll's "if
// that creature would die this turn, exile it instead") reuses the shared
// registerReplaceDying helper over everything this exchange damaged.
func effFight(h Host, c *Ctx, sa *cards.SA) {
	// Unread flags stay loud, never silent: TargetsAtRandom$ (Scab-Clan
	// Giant's "chosen at random" -- the targeting ask above is the
	// deterministic stand-in, never math/rand), TargetsWithoutSameCreatureType$
	// (Rivals' Duel -- the pairwise share-no-types legality is not expressible
	// in the per-candidate census, so the plain 2-target ask stands in), and
	// ExcessSVar$/ExcessSVarCondition$ (rhinos_rampage, the_last_agni_kai --
	// "excess damage becomes X").
	fightUnreadNote(h, c, sa, "TargetsAtRandom")
	fightUnreadNote(h, c, sa, "TargetsWithoutSameCreatureType")
	fightUnreadNote(h, c, sa, "ExcessSVar")
	fightUnreadNote(h, c, sa, "ExcessSVarCondition")
	fighters := Defined(h, c, sa)
	opponents := c.PickedTargets
	if opponents == nil {
		opponents = c.Targets
	}
	// One fight is ONE simultaneous damage batch.
	h.BeginDamageBatch()
	var dying []state.Target
	defer func() { registerReplaceDying(h, c, sa, dying) }()
	seen := make(map[[2]state.ObjID]bool)
	for _, f := range fighters {
		if f.IsPlayer {
			continue
		}
		fo := h.Game().Obj(f.Obj)
		if fo == nil || fo.Zone != state.ZBattlefield {
			continue
		}
		for _, t := range opponents {
			if t.IsPlayer || t.Obj == f.Obj {
				continue
			}
			to := h.Game().Obj(t.Obj)
			if to == nil || to.Zone != state.ZBattlefield {
				continue
			}
			// Each (fighter, opponent) pair is one mutual fight: each deals its
			// power to the other, one emission per direction. A creature in BOTH
			// lists (the no-Defined$ pair shape) meets its partner from both
			// walks, so the ordered-pair set dedups the exchange to one mutual
			// pair instead of double-hitting.
			pair := [2]state.ObjID{f.Obj, t.Obj}
			if pair[0] > pair[1] {
				pair[0], pair[1] = pair[1], pair[0]
			}
			if seen[pair] {
				continue
			}
			seen[pair] = true
			emitFightHit(h, f.Obj, t.Obj)
			emitFightHit(h, t.Obj, f.Obj)
			dying = append(dying, state.Target{Obj: f.Obj}, state.Target{Obj: t.Obj})
		}
	}
	h.EndDamageBatch()
}

// fightUnreadNote is the loud unread-Fight-parameter note (the key is a real
// parameter so the paramcensus attributes each literal call site).
func fightUnreadNote(h Host, c *Ctx, sa *cards.SA, key string) {
	if strings.TrimSpace(sa.Params[key]) != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "unread Fight param " + key})
	}
}

// emitFightHit lands one direction of a fight: src deals its DERIVED power
// to dst, through a per-fighter rider whose source is the fighter itself and
// with SetDamageSource toggled around the emission so rules' protection check
// and DamageDone triggers read the creature, then restored (one batch spans
// two sources).
func emitFightHit(h Host, src, dst state.ObjID) {
	amount := h.Power(src)
	rider := fightRider(h, src, amount)
	prev := h.SetDamageSource(rider.source)
	emitObjectDamage(rider, dst)
	h.SetDamageSource(prev)
}

// fightRider builds the per-fighter damage rider: source is the fighter
// (unwrapped through resolveSourceObject, the effDealDamage convention), the
// controller its LIVE controller, lifelink/infect/deathtouch read DERIVED off
// the fighter. effFight guards both sides onto the battlefield before
// emitting, so the live branch of newDamageRider's LKI ladder is the only
// reachable one and no DamageSourceLKI capture exists for a fight hit.
func fightRider(h Host, fighter state.ObjID, amount int32) damageRider {
	source := resolveSourceObject(h, fighter)
	controller := state.PlayerID(0)
	if o := h.Game().Obj(source); o != nil {
		controller = o.Controller
	}
	return damageRider{h: h, source: source, controller: controller,
		amount: amount, hasLifelink: h.HasKeyword(source, "Lifelink"),
		hasInfect:     h.HasKeyword(source, "Infect"),
		hasDeathtouch: h.HasKeyword(source, "Deathtouch")}
}

// effDamageAll is the sweep pattern: when ValidCards$ is present, iterate the
// battlefield in seat order, filter and emit; then run the independent
// ValidPlayers$ arm (Pestilence, Valakut Exploration, Earthquake). An absent
// ValidCards$ means no object half at all -- Valakut's player-only sweep must
// not inherit a synthetic Creature default. Seat order keeps the event
// sequence deterministic. DamageSource$ applies to the player arm too (the
// lifelink rider pays for player damage from a named source), through the same
// rider.
func effDamageAll(h Host, c *Ctx, sa *cards.SA) {
	n := Num(h, c, sa, "NumDmg", 1)
	if n < 0 {
		n = 0
	}
	// DamageAll is one simultaneous damage event even though its individual
	// hits are serialized in the log. Keep its complete permanent-and-player
	// pass inside the boundary so LifeLostAll observes the affected group once.
	if b, ok := h.(interface {
		BeginLifeLossBatch()
		EndLifeLossBatch()
	}); ok {
		b.BeginLifeLossBatch()
		defer b.EndLifeLossBatch()
	}
	spec := strings.TrimSpace(sa.Params["ValidCards"])
	remember := strings.TrimSpace(sa.Params["RememberDamaged"]) != ""
	// A player-kind ValidTgts$ scopes the object sweep to that target
	// player's permanents ("each creature target player controls"): the
	// restriction is carried by ValidTgts$, never by ValidCards$, so the
	// plain spellings (Aggravate, Simoon, Chandra, Bold Pyromancer) would
	// otherwise sweep every seat. Resolve it through the same referent the
	// TargetedPlayerCtrl filter predicate uses so the two cannot drift. A
	// non-nil scope that resolves to no player fails CLOSED, matching the
	// Defined/TargetedPlayerCtrl direction. ValidTgts$ Creature (a
	// non-player spec) leaves scope nil, so the filter-only sweep stands.
	var scope map[state.PlayerID]bool
	if tg := strings.TrimSpace(sa.Params["ValidTgts"]); tg != "" && playerSpecBaseKnown(tg) {
		sc := c.SpecContext(c.Controller)
		sc.ResolutionTargets = targetedGroup(c)
		players, _ := controlReferentPlayers(h.Game(), sc, "ControlledBy", "TargetedPlayer")
		scope = make(map[state.PlayerID]bool, len(players))
		for _, p := range players {
			scope[p] = true
		}
	}
	g := h.Game()
	rider := newDamageRider(h, c, sa, n)
	prev := h.SetDamageSource(rider.source)
	defer h.SetDamageSource(prev)
	// One DamageAll call is ONE damage batch, exactly like DealDamage's
	// (see effDealDamage): every creature and player it hits latches
	// together.
	var damaged []state.Target
	h.BeginDamageBatch()
	if spec != "" {
		for _, p := range g.AliveFrom(0) {
			for _, id := range g.Zone(state.ZBattlefield, p) {
				if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
					if scope != nil {
						if o := g.Obj(id); o == nil || !scope[o.Controller] {
							continue
						}
					}
					emitObjectDamage(rider, id)
					damaged = append(damaged, state.Target{Obj: id})
					if remember {
						c.Remembered = append(c.Remembered, state.Target{Obj: id})
						eventRemember(h, c, id)
					}
				}
			}
		}
	}
	for _, p := range validPlayers(h, c, sa.Params["ValidPlayers"]) {
		emitPlayerDamage(rider, p)
		damaged = append(damaged, state.Target{Player: p, IsPlayer: true})
		if remember {
			rememberPlayerBothHalves(h, c, p)
		}
	}
	h.EndDamageBatch()
	registerReplaceDying(h, c, sa, damaged)
}

// validPlayers resolves a DamageAll ValidPlayers$ spec to the players the
// sweep damages, in a deterministic order (selector order first, then seat
// order). A spec Defined recognises as an object reference resolves through
// the same resolver every other reference uses -- ValidPlayers$ Targeted
// (players among the chosen targets), Remembered -- while anything else is
// a PREDICATE over every living player through MatchesPlayerSpec (Player,
// Player.Opponent, Opponent, You). The dotted `.IsRemembered` spelling
// (Snort) narrows the two-tier remember set to its base constraint, and the
// OppNonTriggeredTarget singleton (Kediss, Parapet Thrasher) resolves
// through its trigger binding. The Fallen's game-long compound
// (Player.Opponent+wasDealtDamageThisGameBy Self) resolves through the
// shared player filter now that events.Apply folds a game-long (recipient,
// source) damage record (state.Player.DamageTakenByGame); the sweep binds
// c.Source so the qualifier's Self referent resolves. A spec the grammar
// still does not model fails closed AND loud: it damages no player and
// emits a Note naming the selector, never a silent no-op and never a guess
// about who takes the sweep.
func validPlayers(h Host, c *Ctx, spec string) []state.PlayerID {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	g := h.Game()
	appendPlayer := func(out []state.PlayerID, seen map[state.PlayerID]bool,
		p state.PlayerID) []state.PlayerID {
		if p < 0 || int(p) >= len(g.Players) || g.Players[p].Lost || seen[p] {
			return out
		}
		seen[p] = true
		return append(out, p)
	}
	if spec == "TargetedController" {
		// The controller of the chosen object target (1 corpus line); the
		// object-target analogue of ValidPlayers$ Targeted.
		if len(c.Targets) == 0 {
			return nil
		}
		return []state.PlayerID{PlayerOf(h, c, c.Targets[0])}
	}
	// The dotted `.IsRemembered` spelling (Snort's ValidPlayers$
	// Opponent.IsRemembered), BEFORE definedSpec: its trailing player-base
	// sweep claims any base.Opponent/Player/You/Other spec and evaluates it
	// through MatchesPlayerSpecFrom, whose IsRemembered qualifier reads only
	// the source object's PERSISTENT list -- which RememberDiscardingPlayers$
	// never writes (it records the discarding players in the resolution's
	// Remembered only) -- so the sweep would answer an empty set and the
	// whole player half would silently deal no damage. Resolved here instead:
	// the remember set through definedSpec's Player.IsRemembered read reused
	// verbatim (the source object's persistent player-remember list wins,
	// the resolution's Remembered is the fallback), narrowed to the spec's
	// base constraint (Opponent/You/Other) through the shared player filter.
	// The Player/Any spellings stay on their existing exact-case path.
	if base, qual, ok := strings.Cut(spec, "."); ok && qual == "IsRemembered" &&
		base != "Player" && base != "Any" {
		ts, _ := definedSpec(h, c, "Player.IsRemembered")
		var out []state.PlayerID
		seen := make(map[state.PlayerID]bool)
		for _, t := range ts {
			if t.IsPlayer && MatchesPlayerSpec(g, base, t.Player, c.Controller) {
				out = appendPlayer(out, seen, t.Player)
			}
		}
		return out
	}
	// The dotted-claim census, BEFORE definedSpec: its trailing player-base
	// sweep claims any dotted Player/Any/Opponent/Other/You spec and
	// evaluates it inside the shared filter, whose unknown clauses match
	// nobody SILENTLY (its own fail-closed direction). The sweep wants an
	// unmodelled clause loud instead, so a claim-bound spec with a clause
	// whose base the player grammar does not name is rejected here with a
	// Note naming the selector. The clause vocabulary consulted is the
	// shared filter's own bare-clause list (isBarePlayerProperty), now
	// including the game-long wasDealtDamageThisGameBy <ref> clause The
	// Fallen's compound carries, so the two cannot drift. Known-base
	// unknown-QUALIFIER spellings stay on definedSpec's silent empty-set
	// path: the qualifier vocabulary is the shared filter's to own, and a
	// parallel
	// census of it here could only drift from the matcher it mirrors.
	if base, _, ok := strings.Cut(spec, "."); ok &&
		(base == "Player" || base == "Any" || base == "Opponent" || base == "Other" || base == "You") {
		if validPlayersSelectorUnknown(spec) {
			eachDamageNote(h, c, "unresolved DamageAll ValidPlayers$ "+spec)
			return nil
		}
	}
	if ts, ok := definedSpec(h, c, spec); ok {
		var out []state.PlayerID
		seen := make(map[state.PlayerID]bool)
		for _, t := range ts {
			if t.IsPlayer {
				out = appendPlayer(out, seen, t.Player)
			}
		}
		return out
	}
	// OppNonTriggeredTarget (Kediss, Emberclaw Familiar, 9 corpus lines):
	// every opponent except the player the triggering event targeted --
	// "deals that much damage to each OTHER opponent". Absent a player
	// TriggerTarget the whole opponent set matches, the same reading the
	// spec's name gives a trigger with no player referent.
	if spec == "OppNonTriggeredTarget" {
		exclude, has := state.PlayerID(0), false
		if c.TriggerTarget.IsPlayer {
			exclude, has = c.TriggerTarget.Player, true
		}
		var out []state.PlayerID
		for _, p := range g.AliveFrom(0) {
			if p != c.Controller && (!has || p != exclude) {
				out = append(out, p)
			}
		}
		return out
	}
	// The fall-through census: the same check for a spec no earlier arm
	// claimed -- a bare unknown selector would silently match nobody; make
	// it loud instead. Same qualifier exemption as the dotted gate above.
	if validPlayersSelectorUnknown(spec) {
		eachDamageNote(h, c, "unresolved DamageAll ValidPlayers$ "+spec)
		return nil
	}
	var out []state.PlayerID
	for _, p := range g.AliveFrom(0) {
		// MatchesPlayerSpecFrom (not MatchesPlayerSpec) so the source-bound
		// clauses this sweep now owns -- the bare/qualified
		// wasDealtDamageThisGameBy <ref> player qualifier The Fallen's
		// compound carries -- read c.Source. A source-less spec (Player,
		// Player.Opponent, ...) is unaffected: the extra binding is only
		// consulted by a clause that names <ref>.
		if MatchesPlayerSpecFrom(g, spec, p, c.Controller, c.Source) {
			out = append(out, p)
		}
	}
	return out
}

// validPlayersSelectorUnknown reports whether a ValidPlayers$ spec carries
// any clause whose base the shared player filter does not recognise: one of
// Player/Any/You/Opponent/Other, or one of the bare property spellings the
// compound grammar reads (matchesPlayerCompoundCtx's own vocabulary). A
// leading '!' does not change what the base is. Comma alternatives are the
// filter's own first split, so each is checked separately.
func validPlayersSelectorUnknown(spec string) bool {
	knownBase := func(clause string) bool {
		clause = strings.TrimSpace(strings.TrimPrefix(clause, "!"))
		if isBarePlayerProperty(clause) {
			return true
		}
		base, _, _ := strings.Cut(clause, ".")
		switch base {
		case "Player", "Any", "You", "Opponent", "Other":
			return true
		}
		return false
	}
	for alt := range strings.SplitSeq(spec, ",") {
		for clause := range strings.SplitSeq(alt, "+") {
			if !knownBase(clause) {
				return true
			}
		}
	}
	return false
}

// effEachDamage implements "SP$/AB$/DB$ EachDamage" -- the "each creature
// deals damage" primitive (Wave of Reckoning's "Each creature deals damage
// to itself equal to its power", the fight family's two-sided shapes, the
// Sarkhan the Masterless attack trigger).
//
// The grammar below is measured over the corpus's 29 EachDamage carriers:
//
// Damagers (exactly one of):
//   - ToEachOther$ <ref>: the named set is BOTH damagers and recipients --
//     each member deals to every OTHER member (Grim Contest, The Great
//     Aerie).
//   - DefinedDamagers$ <spec>: one Defined resolver spec (ParentTarget,
//     Targeted, Targeted.YouCtrl, Remembered, or the "Valid <filter>"
//     battlefield sweep).
//   - ValidCards$ <spec>: the DamageAll battlefield sweep, seat order then
//     zone order.
//
// An unresolvable or empty damager set is a fail-closed no-op -- the
// standing filter convention (an unknown predicate matches nobody), never
// a guess at the resolving source.
//
// Recipients, in precedence order:
//   - EachToItself$ True: each damager damages itself (Wave of Reckoning,
//     Solar Blaze, The Akroan War's tapped sweep).
//   - ToEachOther$: the OTHER members of the damager set.
//   - Defined$ <spec>: the ordinary Defined resolver (Self, ParentTarget,
//     Opponent, Remembered, TriggeredAttackerLKICopy, ...).
//   - the SA's own ValidTgts$ targets: the mid-resolution target ask's
//     answer (effects.Resolve's generic pre-ask, the Kamahl's Will /
//     coordinated_clobbering family).
//
// Amount: NumDmg$ is PER DAMAGER -- Count$CardPower/CardToughness evaluate
// bound to each damager (a per-damager Ctx whose Source is the damager,
// keeping the resolution's own SVar table so Nissa's Judgment's NumDmg$ X
// still finds the spell face's SVar), which is what makes EachDamage
// different from DamageAll, whose NumDmg resolves once against the
// resolution's source. Default 1 (DamageAll's default; every corpus line
// carries NumDmg$ so the default is defensive only); negatives clamp to 0
// like both siblings.
//
// Every hit is its own damage source: one rider per damager, built through
// the same newDamageRider the siblings use (so a DamageSource$ override, if
// one ever appears on a corpus line, resolves through its existing spec
// arm), and SetDamageSource published around each damager's pass -- the
// lifelink rider, the deathtouch mark, protection and the DamageDone
// triggers all read the DAMAGER, and the pg2 referent machinery binds it.
// The whole pass is one simultaneous damage batch (CR: the creatures deal
// their damage at the same time even though the log serializes the hits),
// so DamageDealtOnce/DamageDoneOnce latch per pass and LifeLostAll-style
// triggers observe the group once.
func effEachDamage(h Host, c *Ctx, sa *cards.SA) {
	eachToItself := strings.TrimSpace(sa.Params["EachToItself"]) != ""
	eachOtherRef := strings.TrimSpace(sa.Params["ToEachOther"])
	hasDefined := strings.TrimSpace(sa.Params["Defined"]) != ""
	_, hasTgts := sa.Params["ValidTgts"]

	// LifeLostAll observes the affected group once, exactly as effDamageAll
	// brackets its sweep.
	if b, ok := h.(interface {
		BeginLifeLossBatch()
		EndLifeLossBatch()
	}); ok {
		b.BeginLifeLossBatch()
		defer b.EndLifeLossBatch()
	}

	var eachOtherSet []state.ObjID
	damagers := func() []state.ObjID {
		g := h.Game()
		battlefield := func(ts []state.Target) []state.ObjID {
			var out []state.ObjID
			for _, t := range ts {
				if t.IsPlayer || t.Obj == 0 {
					continue
				}
				if o := g.Obj(t.Obj); o != nil && o.Zone == state.ZBattlefield {
					out = append(out, t.Obj)
				}
			}
			return out
		}
		if eachOtherRef != "" {
			// The set named by ToEachOther$ is the parent's targets (in
			// Ctx.Targets) joined with this SA's own answered ask (in
			// Ctx.PickedTargets while it dispatches). Dedup keeps a shared
			// member one member.
			seen := make(map[state.ObjID]bool)
			var out []state.ObjID
			for _, id := range append(battlefield(c.Targets), battlefield(c.PickedTargets)...) {
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
			eachOtherSet = out
			return out
		}
		if spec := strings.TrimSpace(sa.Params["DefinedDamagers"]); spec != "" {
			if unknown := eachDamagerUnknownPredicates(spec); len(unknown) > 0 {
				eachDamageNote(h, c, "unresolved EachDamage damager predicate in "+spec)
				return nil
			}
			ts, recognized := eachDamagerTargets(h, c, spec)
			if !recognized {
				eachDamageNote(h, c, "unresolved EachDamage damager selector "+spec)
				return nil
			}
			return battlefield(ts)
		}
		if spec := strings.TrimSpace(sa.Params["ValidCards"]); spec != "" {
			if unknown := UnknownPredicates(spec); len(unknown) > 0 {
				eachDamageNote(h, c, "unresolved EachDamage damager predicate in "+spec)
				return nil
			}
			var out []state.ObjID
			for _, p := range g.AliveFrom(0) {
				for _, id := range g.Zone(state.ZBattlefield, p) {
					if MatchesSpecCtx(g, spec, id, c.SpecContext(c.Controller)) {
						out = append(out, id)
					}
				}
			}
			return out
		}
		return nil
	}()
	if len(damagers) == 0 {
		return
	}

	// Recipients for the Defined$/ValidTgts$ arms, resolved once against the
	// resolution's own context. The ValidTgts$ arm must not fall through to
	// the source-default: a sub whose own ask found no eligible candidates
	// (chosenTargetsFor's max<=0 arm returns ok=false) has no recipients,
	// never the parent's targets.
	var recipients []state.Target
	if !eachToItself && eachOtherRef == "" {
		if hasDefined {
			recipients = Defined(h, c, sa)
		} else if hasTgts {
			if c.PickedTargets != nil {
				recipients = copyTargets(c.PickedTargets)
			} else if c.OfferedSA != nil && sa.Line == c.OfferedSA.Line {
				recipients = copyTargets(c.Targets)
			} else {
				eachDamageNote(h, c, "EachDamage ValidTgts$ has no answered recipient targets")
				return
			}
		} else {
			eachDamageNote(h, c, "EachDamage has no supported recipient selector")
			return
		}
		if len(recipients) == 0 {
			eachDamageNote(h, c, "EachDamage resolved no recipient targets")
			return
		}
	}

	h.BeginDamageBatch()
	unresolvedAmountNoted := false
	for _, d := range damagers {
		o := h.Game().Obj(d)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		// Per-damager amount: Count$CardPower/CardToughness read
		// g.Obj(c.Source), so the per-damager Ctx anchors Source on the
		// damager; the resolution's SVar table is kept so an SVar-named
		// amount (Nissa's Judgment's NumDmg$ X) still resolves.
		pc := &Ctx{Source: d, Controller: o.Controller, SVars: c.SVars}
		n, amountResolved := NumResolved(h, pc, sa, "NumDmg", 1)
		if !amountResolved {
			if _, present := sa.Params["NumDmg"]; !present {
				n = 1
			} else {
				n = 0
				if !unresolvedAmountNoted {
					eachDamageNote(h, c, "unresolved EachDamage NumDmg$")
					unresolvedAmountNoted = true
				}
			}
		}
		if n < 0 {
			n = 0
		}
		rider := newDamageRider(h, pc, sa, n)
		prev := h.SetDamageSource(rider.source)
		switch {
		case eachToItself:
			emitObjectDamage(rider, d)
		case eachOtherRef != "":
			for _, r := range eachOtherSet {
				if r == d {
					continue
				}
				emitObjectDamage(rider, r)
			}
		default:
			for _, t := range recipients {
				if t.IsPlayer {
					emitPlayerDamage(rider, t.Player)
					continue
				}
				if ro := h.Game().Obj(t.Obj); ro != nil && ro.Zone == state.ZBattlefield {
					emitObjectDamage(rider, t.Obj)
				}
			}
		}
		h.SetDamageSource(prev)
	}
	h.EndDamageBatch()
}

func eachDamageNote(h Host, c *Ctx, text string) {
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller, Text: text})
}

// eachDamagerUnknownPredicates reports unsupported filter predicates embedded
// in a DefinedDamagers$ filter, without treating an unsupported selector name
// as an ordinary filter predicate.
func eachDamagerUnknownPredicates(spec string) []string {
	if strings.HasPrefix(strings.TrimSpace(spec), "Valid ") {
		return UnknownPredicates(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(spec), "Valid ")))
	}
	return nil
}

// eachDamagerTargets resolves a DefinedDamagers$ value to concrete targets,
// failing closed (nil) on a spec this grammar does not model -- the same
// contract damage.go's DamageSource$ and ValidPlayers$ resolutions keep.
// knownDefinedTargets answers the exact referent forms (ParentTarget,
// Targeted, Remembered, ... and their " & " joins); the "Valid <filter>"
// form is Defined's own battlefield sweep, repeated here so the resolver
// never falls through to a source default; and the one qualified referent
// the corpus writes on an EachDamage (friendly_rivalry's
// "Targeted.YouCtrl") narrows the named set through the one qualifier this
// grammar reads -- YouCtrl, the resolving controller.
func eachDamagerTargets(h Host, c *Ctx, spec string) ([]state.Target, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, false
	}
	if ts, ok := knownDefinedTargets(h, c, spec); ok {
		return ts, true
	}
	if base, filt, ok := strings.Cut(spec, " "); ok && base == "Valid" {
		filt = strings.TrimSpace(filt)
		g := h.Game()
		var out []state.Target
		for _, p := range g.AliveFrom(0) {
			for _, id := range g.Zone(state.ZBattlefield, p) {
				if MatchesSpecCtx(g, filt, id, c.SpecContext(c.Controller)) {
					out = append(out, state.Target{Obj: id})
				}
			}
		}
		return out, true
	}
	if base, qual, ok := strings.Cut(spec, "."); ok && qual == "YouCtrl" {
		if ts, known := knownDefinedTargets(h, c, base); known {
			var out []state.Target
			for _, t := range ts {
				if t.IsPlayer {
					if t.Player == c.Controller {
						out = append(out, t)
					}
					continue
				}
				if o := h.Game().Obj(t.Obj); o != nil && o.Controller == c.Controller {
					out = append(out, t)
				}
			}
			return out, true
		}
	}
	return nil, false
}

// PendingDamage is one damage a DealDamage with DamageMap$ True MARKED for the
// resolving chain's later DB$ DamageResolve flush, instead of dealing it. The
// rider carries the dealing source, controller, amount and keyword provenance
// computed at mark time (so the flush pays the lifelink rider exactly once,
// from the same facts the immediate path would); the target is the recipient
// recorded at mark time. It is resolution-scratch like Ctx.Remembered -- never
// event-encoded, re-derived by a replay re-running the same resolution -- and
// is carried across a mid-chain ask (rules stamps it on the pending frame).
// The fields are unexported so the rules package can hold the marks opaquely
// (it only ever clones them) without reading a rider.
type PendingDamage struct {
	rider  damageRider
	target state.Target
}

// ClonePendingDamage returns a copy of a chain's pending-damage marks, the
// same defensive-copy shape the other cross-suspension riders use. A nil or
// empty input yields nil, so a chain with no marks is indistinguishable from
// one that never marked.
func ClonePendingDamage(m []PendingDamage) []PendingDamage {
	if len(m) == 0 {
		return nil
	}
	return append([]PendingDamage(nil), m...)
}

// effDamageResolve implements "DB$ DamageResolve" (Forge's
// AbilityFactoryDamageResolve): it flushes the marks a DamageMap$ True
// DealDamage left in the resolving chain as ONE simultaneous damage batch --
// the observable that motivates the primitive, so a DamageDealtOnce
// ("deals damage one or more times") trigger latches once with the batch
// total instead of once per marked call, and CR 616 replacement effects see
// one damage event per recipient, not one per marking call. The marks are
// flushed in recorded order (a slice, never a map, so the order is
// deterministic). A flush with nothing marked is a SILENT no-op: no Note, no
// batch, exactly the shape a bare DamageResolve tail leaves when the script's
// marking half dealt nothing. It runs the DamageResolve SA's own
// RememberDamaged$ True / ReplaceDyingDefined$ riders over the objects the
// flush actually damaged, the same way a real DealDamage would (the one
// corpus carrier, Serpentine Spike, puts both on the DamageResolve line).
func effDamageResolve(h Host, c *Ctx, sa *cards.SA) {
	marks := c.PendingDamage
	if len(marks) == 0 {
		return
	}
	// Consume the marks before emitting: a Damage event's own replacement or
	// trigger machinery must not see them as still pending, and a re-entry
	// must never re-flush a batch already dealt.
	c.PendingDamage = nil
	h.BeginDamageBatch()
	var damaged []state.Target
	for _, m := range marks {
		prev := h.SetDamageSource(m.rider.source)
		if m.target.IsPlayer {
			emitPlayerDamage(m.rider, m.target.Player)
			damaged = append(damaged, state.Target{Player: m.target.Player, IsPlayer: true})
		} else if o := h.Game().Obj(m.target.Obj); o != nil && o.Zone == state.ZBattlefield {
			emitObjectDamage(m.rider, m.target.Obj)
			damaged = append(damaged, state.Target{Obj: m.target.Obj})
		}
		h.SetDamageSource(prev)
	}
	h.EndDamageBatch()
	if len(damaged) == 0 {
		return
	}
	if strings.TrimSpace(sa.Params["RememberDamaged"]) != "" {
		for _, t := range damaged {
			c.Remembered = append(c.Remembered, t)
			// A player entry's Obj is zero and would emit a garbage Choose
			// event; encode the player through PlayerRef, the same encoding
			// rememberPlayerBothHalves' persistent half uses.
			id := t.Obj
			if t.IsPlayer {
				id = state.PlayerRef(t.Player)
			}
			eventRemember(h, c, id)
		}
	}
	registerReplaceDying(h, c, sa, damaged)
}
