package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// countMostCardName implements Forge's Count$MostCardName <spec>: the
// greatest number of objects matching <spec> that share one face name, which
// is the oracle's "N or more <things> with the same name as one another"
// (Mechanized Production, Endless Atlas, Chrome Replicator, Sceptre of
// Eternal Glory -- all four corpus carriers name a battlefield spec). The
// scan is over every alive seat's battlefield, matched through the same
// zone-aware filter the Count$Valid family uses (matchesZoneSpecCtx), so
// `Artifact.YouCtrl` / `Land.YouCtrl` / `Permanent.nonLand+!token+YouCtrl`
// read exactly as they do under Count$Valid. The per-name counts live in a
// local map read only by key plus a running maximum, so no map iteration
// order ever reaches an event or a view. A spec this build cannot match
// (an unknown predicate fails closed inside the matcher) counts zero of every
// name and so returns 0, the conservative no-op every unmodelled filter
// takes. An empty spec is NOT evaluated -- the caller's fail-open convention
// for an unreadable head applies instead of a meaningless zero.
//
// The name read is the printed face's Name (o.Face().Name), matching the
// sibling distinct-name read at the zone-count site (the
// token$DifferentCardNames set) rather than the layer-3 rename table; the
// corpus shape is four same-named printed permanents, so the two agree. The
// zone is the battlefield only, which is what Count$MostCardName's Valid
// semantics mean in Forge and what all four corpus specs name.
func countMostCardName(h Host, c *Ctx, spec string) (int32, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, false
	}
	g := h.Game()
	specCtx := c.SpecContext(c.Controller)
	counts := make(map[string]int32)
	var best int32
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if !matchesZoneSpecCtx(g, spec, id, specCtx, state.ZBattlefield) {
				continue
			}
			o := g.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			name := o.Face().Name
			counts[name]++
			if counts[name] > best {
				best = counts[name]
			}
		}
	}
	return best, true
}

// evalTriggerCount resolves a "TriggerCount$<Head>[/Op]" body against the
// triggering event's magnitude (Ctx.TriggerAmount). The heads this build
// models -- DamageAmount (damage the event dealt), LifeAmount (life it
// gained/lost) and Amount (the generic event magnitude) -- all answer the
// same number, because each is the single amount the causing event carried;
// the distinction between them is only in which trigger mode populates it
// (and, for the corpus, that the LifeGained trigger mode is not yet
// registered, so a LifeAmount head is unreachable today). Result is the
// RolledDie head: the die result the trigger fired on (Ctx.TriggerResult,
// captured at fire time -- Mr. House's BranchConditionSVar$ reads it after
// the RollDice resolution that produced it has finished). The /Op suffix is
// applied exactly as applyCountOp does for Count$ and Sacrificed$. An
// unmodelled head (ScryNum) degrades to zero. max selects the
// TriggerCountMax$ prefix's reading: the same heads, but Result answers the
// highest result in the roll batch (Ctx.TriggerResultMax -- Farideh's "if any
// of those results was 10 or higher") rather than the batch's reported result.
//
// ScryBottom is the completed Scry's own magnitude: events.Scry.Amount, the
// count the player actually chose to put on the bottom (rules captures it
// into TriggerAmount, the same fire-time binding every other head reads).
// The Temporal Anchor's `SVar:X:TriggerCount$ScryBottom` sizes its bottom
// exile through it. ScryNum -- the number LOOKED at -- remains unmodelled:
// this build raises no scry marker carrying that number.
//
// Result is an EVALUATED head (verdict true) on every trigger, not only a
// roll trigger: before RolledDie was registered it reported (0, false), so a
// CheckSVar$ gate over it failed open; now a non-roll trigger reads 0 and the
// gate is enforced. Measured: all 8 corpus files carrying TriggerCount$Result
// (`/usr/bin/grep -rlE 'TriggerCount\$Result' .cards/cardsfolder`) sit on
// Mode$ RolledDie/RolledDieOnce triggers, where Ctx.TriggerResult is set, so
// no corpus gate changes direction.
func evalTriggerCountOK(c *Ctx, body string, max bool) (int32, bool) {
	body, op, hasOp := strings.Cut(body, "/")
	var n int32
	switch evalTriggerCountOKCodes.Code(string(strings.TrimSpace(body))) {
	case evalTriggerCountOKAmount:
		// ScryBottom is the events.Scry marker's Amount (the number put on
		// the bottom), captured into TriggerAmount by rules for Mode$ Scry.
		n = c.TriggerAmount
	case evalTriggerCountOKResult:
		if max {
			n = c.TriggerResultMax
		} else {
			n = c.TriggerResult
		}
	default:
		// ScryNum (the number LOOKED at) is a head whose triggering number
		// this build does not carry, so it stays zero -- the same
		// conservative no-op as before the prefix was recognised. NOT
		// evaluated: a gate over it fails open.
		return 0, false
	}
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// evalSacrificed resolves a "Sacrificed$<Property>[/Op]" body against the
// LKI snapshots this resolving spell/ability captured when it sacrificed
// each object (Ctx.Sacrificed). Property is the corpus head after the
// Sacrificed$ prefix: CardPower, CardToughness, CardManaCost, or Amount (the
// count of objects sacrificed). For a numeric property over more than one
// sacrificed object the values are summed -- corpus Sac costs are almost
// always exactly one candidate, so summing agrees with the single-value
// answer and is the least surprising reading of "the sacrificed creature's
// power" when a shape somehow names several. An empty Sacrificed list
// (nothing captured) degrades to zero rather than panicking, preserving the
// "the card did nothing" totality convention of every other head here. The
// /Op suffix (Plus/Minus/Times./Twice/HalfDown/HalfUp/Negative) is applied
// after the base value, exactly as applyCountOp does for Count$.
func evalSacrificedOK(c *Ctx, body string) (int32, bool) {
	body, op, hasOp := strings.Cut(body, "/")
	var n int32
	switch evalSacrificedOKCodes.Code(string(strings.TrimSpace(body))) {
	case evalSacrificedOKCardPower:
		n = sacrificedNumeric(c, func(s state.SacrificedInfo) int32 { return s.Power })
	case evalSacrificedOKCardToughness:
		n = sacrificedNumeric(c, func(s state.SacrificedInfo) int32 { return s.Toughness })
	case evalSacrificedOKCardManaCost:
		n = sacrificedNumeric(c, func(s state.SacrificedInfo) int32 { return s.ManaValue })
	case evalSacrificedOKAmount:
		n = int32(len(c.Sacrificed))
	default:
		// An out-of-scope head (Valid, CardTypes, ChromaSource, CardNumColors,
		// CardCounters) degrades to zero, exactly as before the fix -- the
		// conservative same-as-before no-op the brief scopes out. NOT
		// evaluated: a gate over one of these fails open.
		return 0, false
	}
	if hasOp {
		n = applyCountOp(n, op)
	}
	return n, true
}

// sacrificedNumeric folds a numeric property across every object this
// spell/ability sacrificed, summing (see evalSacrificed's doc for why sum and
// not first).
func sacrificedNumeric(c *Ctx, f func(state.SacrificedInfo) int32) int32 {
	var n int32
	for _, s := range c.Sacrificed {
		n += f(s)
	}
	return n
}

func evalRememberedOK(h Host, c *Ctx, body string) (int32, bool) {
	body, op, hasOp := strings.Cut(body, "/")
	if strings.TrimSpace(body) == "Amount" {
		n := int32(len(rememberedExcludingCapture(h, c)))
		if hasOp {
			n = applyCountOp(n, op)
		}
		return n, true
	}
	// A Remembered$CardPower / CardToughness / CardManaCost / CardCounters.
	// / Valid body is the shared <Ref>$<Property> family (evalRefProperty);
	// evalCountExpr routes it here first only because the Remembered$
	// prefix cut wins. An unmodelled property still degrades to zero, the
	// same conservative no-op evalSacrificed's default takes, and the
	// property verdict rides through: an unmodelled one is NOT evaluated.
	if n, ok := evalRefProperty(h, c, "Remembered$"+body); ok {
		if hasOp {
			n = applyCountOp(n, op)
		}
		return n, true
	}
	return 0, false
}

// rememberedExcludingCapture is the ctx's Remembered list minus its
// fire-time event capture (Ctx.Captured) -- Forge's host remembered list,
// which never contains the event object the trigger fired on. The exclusion
// lives in ONE helper every plain-Remembered reader goes through:
// effImmediateTrigger (which builds each "when you do" instance's ctx from
// it, so its exclusion and the TriggerRemembered count head's cannot drift),
// refTargets' TriggerRemembered case (Loamcrafter Faun's SVar:X:
// TriggerRemembered$Amount), evalRememberedOK's Amount head,
// evalCountExprOK's RememberedNumber head, rememberedWithSource (the plain
// Remembered$ group every Valid/condition reader resolves through),
// rememberedLKIGroup (the RememberedLKI ref group) and evalRefProperty's
// Remembered$<Property> heads.
// Contract: docs/superpowers/specs/2026-09-22-engine-contracts.md, “Trigger remembered readers intentionally differ.”
// A no-capture ctx (captured empty) returns the list unchanged; the helper
// is idempotent -- the instance ctx effImmediateTrigger builds has Captured
// and Remembered disjoint, so applying it a second time there answers the
// same set.
func rememberedExcludingCapture(h Host, c *Ctx) []state.Target {
	if len(c.Captured) == 0 {
		return c.Remembered
	}
	// Remove only the seeded occurrence(s), not every equal target. A body
	// can explicitly remember the captured object again (e.g. RememberSacrificed
	// on a death trigger); that later occurrence is real memory even though its
	// identity equals the capture.
	remaining := make(map[state.Target]int, len(c.Captured))
	for _, t := range c.Captured {
		remaining[t]++
	}
	var out []state.Target
	for _, t := range c.Remembered {
		if remaining[t] > 0 {
			remaining[t]--
			continue
		}
		out = append(out, t)
	}
	return out
}

// rememberedLKIGroup resolves the RememberedLKI ref group: the SAME
// remembered set the plain-Remembered resolver (rememberedWithSource)
// answers -- Forge's host remembered list is the source card's persistent
// event-backed list UNIONED with the resolution's own ctx memory -- with ONE
// deviation: an object that IS the resolving source is kept when the
// resolution explicitly remembered it. rememberedWithSource drops
// c.Source unconditionally, which was this build's stand-in for keeping the
// fire-time capture out of a plain Remembered read; for RememberedLKI the
// capture-excluding helper above already removes exactly the seeded capture
// occurrences, so the blanket exclusion only ever deleted a REAL memory of
// the source -- Cosima's DBReturn (Defined$ Self RememberLKI$ True: the
// exiled god returns to the battlefield and its own X reads
// RememberedLKI$CardCounters.VOYAGE off itself) and Riders of the Mark
// (RememberChanged$ True on a Defined$ Self move, its toughness read
// RememberedLKI$CardToughness) both went to zero under it. The union is
// deduped by id (the same object may sit in both halves -- an explicit
// RememberChanged$/RememberLKI$ append after a capture seed, or a rider that
// eventRemembered into the persistent list while the ctx list already carried
// it), and the helper is idempotent: a ctx whose capture is already excluded
// answers the same set again.
func rememberedLKIGroup(h Host, c *Ctx) []state.Target {
	out := make([]state.Target, 0, len(c.Remembered))
	seen := make(map[state.ObjID]bool, len(c.Remembered))
	if o := h.Game().Obj(c.Source); o != nil {
		for _, t := range o.Remembered {
			if !t.IsPlayer && !seen[t.Obj] {
				seen[t.Obj] = true
				out = append(out, t)
			}
		}
	}
	for _, t := range rememberedExcludingCapture(h, c) {
		if t.IsPlayer {
			out = append(out, t)
			continue
		}
		if !seen[t.Obj] {
			seen[t.Obj] = true
			out = append(out, t)
		}
	}
	return out
}

// refTargets resolves one ref name of the <Ref>$<Property> family into the
// targets it names. Shared by evalRefProperty and the <Ref>>Count$...>
// indirection branch in evalCountExprOK, so the two cannot disagree about
// which refs exist. An unknown ref returns false -- the caller fails closed,
// exactly as evalRefProperty's default always did.
func refTargets(h Host, c *Ctx, ref string) ([]state.Target, bool) {
	if inner, ok := spawnerChain(ref); ok { // shared Spawner> strip (spawnercontrol)
		// Forge's adjustTriggerContext (AbilityUtils): "Spawner>" re-anchors
		// the rest of the chain on the resolving ability's TRIGGER's spawning
		// ability. This build's stand-in for that context is the firing
		// trigger's own event capture (Ctx.Captured), which a chained
		// ImmediateTrigger's Execute context no longer carries as Remembered
		// (effImmediateTrigger hands its instances the capture-excluded parent
		// set). Halana, Kessig Ranger's immediate-trigger chain is the corpus
		// user (X:Spawner>TriggeredCard$CardPower sizes its DamageSource$
		// Spawner>... hit); an inner ref this grammar does not know still
		// fails closed here exactly as it did before the arm existed.
		sc := *c
		sc.Remembered = copyTargets(c.Captured)
		// The substitution consumes the capture into Remembered, so a nested
		// ref that excludes Ctx.Captured (TriggerRemembered) must not be
		// handed the old capture again and subtract it away. No corpus line
		// writes Spawner>TriggerRemembered; this keeps the composition correct
		// structurally (the count head's one-home helper).
		sc.Captured = nil
		return refTargets(h, &sc, inner)
	}
	switch refTargetsCodes.Code(string(ref)) {
	case refTargetsTargeted:
		return c.Targets, true
	case refTargetsParentTarget:
		// The NEAREST targeting parent link's targets (parent_targets.go):
		// Flourishing Grapple's X = ParentTargeted$CardPower is DBPump's
		// creature, not the root's.
		return parentLinkTargets(c), true
	case refTargetsAllTargeted:
		// AllTargeted (task alltargeted1) is Forge's UNION of every targeting
		// SA's targets down the root ability's sub-ability chain. The cast
		// flow pre-asks the whole chain's targets before payment (CR 601.2c)
		// and threads the union through Ctx.AllTargets at the cost-evaluation
		// sites that read it (ownReduceCost's reprice, the CollectEvidence
		// amount resolution); a caller that did not bind one -- a resolution-
		// time read, where no corpus line carries the ref -- keeps the
		// faithful-as-available Ctx.Targets binding, which for a chain with
		// no sub targets IS the union.
		if c.AllTargets != nil {
			return c.AllTargets, true
		}
		return c.Targets, true
	case refTargetsTriggeredCard:
		return c.Remembered, true
	case refTargetsRememberedLKI:
		// The LKI spelling of the Remembered$ group names the SAME objects
		// (Forge's remembered list, which never contains the event object the
		// trigger fired on); only the characteristic read differs, through
		// Ctx.LKI's pre-move snapshot. Route it through the one RememberedLKI
		// resolver so the two cannot disagree about WHICH objects are
		// remembered -- a raw Ctx.Remembered read summed a triggered
		// Destroy/ChangeZone's fire-time capture (the trigger's own source)
		// into a chained RememberedLKI$CardToughness (Noxious Gearhulk,
		// Rotfeaster Maggot), inflating the read by the source's own
		// characteristic, while the plain resolver's blanket source exclusion
		// dropped an object that IS the source and was genuinely remembered
		// (Cosima's self-return RememberLKI$ read, Riders of the Mark's
		// RememberChanged$ one). The capture-occurrence exclusion is precise;
		// the source itself is kept.
		return rememberedLKIGroup(h, c), true
	case refTargetsTriggerRemembered:
		// TriggerRemembered (task triggerremembered1) is Forge's name for the
		// trigger's own RememberObjects$ capture -- the set the resolving
		// body introspects ("return up to that many"). It is NOT the whole
		// ctx list: rules seeds a firing trigger's ctx with Remembered =
		// Captured = the trigger's own event capture, and effImmediateTrigger
		// hands each "when you do" instance the capture-EXCLUDED parent set.
		// Bind the exclusion here, at the one place the ref resolves, so a
		// read that happens at either level -- the ImmediateTrigger's own ctx
		// (Loamcrafter Faun's TriggerAmount$/a direct Execute read) or an
		// instance ctx -- answers the chain's remembered set and never the
		// fire-time capture. A plain Ctx.Remembered read (what the sibling
		// ticket first landed) overcounts by that capture in the outer ctx;
		// the helper is idempotent with effImmediateTrigger's own exclusion
		// (the instance capture is disjoint from its remembered set), so the
		// instance read is unchanged. The members the corpus writes (Amount/
		// CardPower/CardToughness/CardManaCost/CardManaCostLKI/
		// CardCounters.*) read through the one property switch below.
		return rememberedExcludingCapture(h, c), true
	case refTargetsTriggeredExploited:
		// The exploited creature (CR 702.58c's "that creature"): the Exploit
		// marker's triggerReferents case binds ev.IDs[0] to TriggerCard at
		// fire time, so Henry Wu's TriggeredExploited$CardPower and Profaner
		// of the Dead's TriggeredExploited$CardToughness read exactly the
		// sacrificed creature. evalRefProperty then reads its LKI P/T from
		// Ctx.LKIPower/LKIToughness, which rules' attachExploitedLKI sets from
		// the as-sacrificed snapshot effects/exploit.go publishes (CR 608.2g):
		// the bare graveyard card would carry only its printed face, losing a
		// +1/+1 counter or a pump the creature had when it was sacrificed. The
		// role-absent fallback keeps the old Remembered read for a hand-built
		// context (the TriggeredBlocker precedent).
		if c.TriggerCard != 0 {
			return []state.Target{{Obj: c.TriggerCard}}, true
		}
		return c.Remembered, true
	case refTargetsTriggeredBlocker:
		// The pair's BLOCKER (trig:Blocks): prefer the fire-time TriggerBlocker
		// role when the Blocks capture set it (Remembered names the attacker
		// there); the role-absent fallback keeps the old Remembered read --
		// the AttackerBlockedByCreature queue entries and hand-built contexts,
		// whose Remembered IS the blocker. This mirrors the shared case in
		// effects/context.go's knownDefinedTargets so the two resolvers cannot
		// disagree about one spelling.
		if c.TriggerBlocker != 0 {
			return []state.Target{{Obj: c.TriggerBlocker}}, true
		}
		return c.Remembered, true
	case refTargetsTriggeredSpellAbility:
		// The activation arm (abcopy1): the fire-time TriggerAbility role is
		// the exact referent (Remembered names the source permanent); the
		// spell-cast arm and hand-built contexts keep the Remembered entry.
		if c.TriggerAbility != 0 {
			return []state.Target{{Obj: c.TriggerAbility}}, true
		}
		return c.Remembered, true
	case refTargetsCastSA:
		// The cast spell ability (Graven Archfiend's ETB gate
		// "CastSA>Count$OptionalGenericCostPaid.1.0"): the cast spell's own
		// object. For the corpus shape -- an ETB trigger of the permanent the
		// cast spell became -- the ctx source IS that object (the
		// stack->battlefield move preserves the id, and the pay-time CastInfo
		// folded the paid provenance onto it), so binding the ctx source is
		// exactly the binding the indirection needs; a copy of the spell is a
		// distinct object and reads its own (unpaid) provenance.
		//
		// CastSA names THIS source's own cast. When the trigger context names
		// a DIFFERENT cast spell (a SpellCast trigger firing on another card's
		// cast), that referent is TriggeredSpellAbility, not CastSA -- this
		// source was not the card being cast, so the ref is unbound. Every
		// corpus CastSA carrier is self-referential (SpellCast ValidCard$
		// Card.Self, a self ChangesZone ETB, or a bare CheckSVar$/replacement
		// ctx with no trigger referent), so no real shape regresses; the
		// alternative reading silently bound an unrelated cast spell's X.
		if c.TriggerCard != 0 && c.TriggerCard != c.Source {
			return nil, false
		}
		if c.Source != 0 {
			return []state.Target{{Obj: c.Source}}, true
		}
		return nil, false
	case refTargetsRemembered:
		// Forge's plain Remembered$ form reads the executing ability's shared
		// host-card remembered list: the ctx walk's set UNIONED with the
		// source's persistent event-backed list (rememberedWithSource).
		return rememberedWithSource(h, c), true
	case refTargetsImprinted:
		// The imprint reference the <Ref>$<Property> family reads RAW: the
		// RepeatEach subject first (the same precedence definedSpec's Imprinted
		// case takes -- the loop's CURRENT subject, Master of the Wild Hunt's
		// X:Imprinted$CardPower), else the source's imprint associations raw
		// (Forge's getImprintedCards has no zone gate; the damage-source and
		// count consumers are not definedSpec's CR 607.2a exile-gated Defined$
		// readers -- rawImprintTargets carries the rationale). An empty pile is
		// a legitimate zero, not an unresolvable body.
		if c.RepeatSubject.Obj != 0 && !c.RepeatSubject.IsPlayer {
			return []state.Target{{Obj: c.RepeatSubject.Obj}}, true
		}
		return rawImprintTargets(h.Game(), c), true
	case refTargetsChosenCard:
		// The chosen-card read resolutionChosenCards already serves definedSpec's
		// ChosenCard case -- the same shared read here keeps a count body from
		// disagreeing with a Defined$ ChosenCard (Crush Underfoot's
		// X:ChosenCard$CardPower sizes its DamageSource$ ChosenCard hit).
		return resolutionChosenCards(h.Game(), c), true
	case refTargetsExiledWith:
		// The same defined-targets resolver case effects/context.go's
		// knownDefinedTargets carries, so a count body over the ref (the
		// CheckSVar gate SVar:X:ExiledWith$Amount of Colfenor's Urn,
		// Veteran Survivor and River Song's Diary) evaluates against the
		// same set the body's own Defined$ ExiledWith resolves. Without it
		// the ref fails closed and the gate is never evaluated, so those
		// triggers stay silent even when the exile count meets the gate.
		var out []state.Target
		g := h.Game()
		for _, id := range g.Zone(state.ZExile, c.Controller) {
			if o := g.Obj(id); o != nil && o.ExiledWith == c.Source {
				out = append(out, state.Target{Obj: id})
			}
		}
		return out, true
	case refTargetsExiled:
		// Forge's cast-cost PAID lists (AbilityUtils.getPaidCards ->
		// SpellAbility.getPaidList): the cards this cast's own cost removed --
		// CostExile's row is keyed "Exiled" (HashLKIListKey),
		// CostReveal.doPayment's is "Revealed". The cards ride Ctx.Exiled /
		// Ctx.Revealed, bound from the engine's per-stack-object capture at
		// payment, so a body such as Draconic Intervention's
		// `SVar:X:Exiled$CardManaCost` reads the card the ExileFromGrave cost
		// actually exiled rather than the source or a remembered set. An
		// absent binding is a legitimate EMPTY list (ok=true, zero), never a
		// fallback -- exactly the ExiledWith association's empty case above.
		// This is deliberately NOT Object.ExiledWith: an ExileFromGrave cost
		// emits a plain MoveZone with no ExiledWith marker, so aliasing the
		// association would read zero for the real carriers. paidCostTargets
		// is the shared home with definedSpec's own case.
		return paidCostTargets(c, ref), true
	case refTargetsExiledCards:
		// Forge's `ExiledCards` count referent (Corpseweft's
		// `SVar:Y:ExiledCards$Amount/Twice` -- the only corpus carrier at this
		// pin): the cards THIS cast or activation exiled as a cost, i.e. the
		// SAME paid list the `Exiled` spelling immediately above reads. It is
		// claimed here explicitly because the default fallback below cannot
		// model it -- definedSpec carries no `ExiledCards` selector -- so the
		// body would fail closed and Corpseweft's Zombie Horror would be minted
		// at the dynamic side's zero and swept by state-based actions. It is
		// deliberately NOT Object.ExiledCards (a ChangeZone zone association on
		// the exiling object) nor Ctx.Remembered (the memory/captured trigger
		// objects `TokenRemembered$ ExiledCards` reads): neither holds the paid
		// list an ExileFromGrave cost fills. Paired with evalRefProperty's
		// `Amount` property, this sizes the token; the corpus's `/Twice` op
		// rides the ordinary applyCountOp suffix.
		return paidCostTargets(c, "Exiled"), true
	case refTargetsEquipped:
		// The object the source is attached to (Glamdring's "where X is
		// equipped creature's power", Equipped$CardPower). Claimed here rather
		// than through the defined-targets fallback below, which refuses an
		// empty set: an unattached Equipment names NO creature, and "its
		// power" is then a legitimate zero, not an unreadable body.
		g := h.Game()
		if o := g.Obj(c.Source); o != nil && o.AttachedTo != 0 && g.Obj(o.AttachedTo) != nil {
			return []state.Target{{Obj: o.AttachedTo}}, true
		}
		return nil, true
	case refTargetsTargetedObjects:
		// Forge's TargetedObjects referent (AbilityUtils.calcX's
		// `calcX[0].startsWith("TargetedObjects")` arm): the UNION of every
		// targeting SA's chosen targets down the root ability's sub-ability
		// chain. Forge's Amount includes players; object properties below skip
		// player entries. The union
		// is the same set Ctx.AllTargets carries when the cast pre-ask bound
		// it (the AllTargeted ref's own source), else the resolution's own
		// Ctx.Targets -- which for a chain with no sub targets IS the union.
		// Builders' Bane's "Destroy X target artifacts", Fireball's and
		// Firestorm's TargetMin$/Max$ and Choking Vines' X are the corpus
		// users. "Distinct" (TargetedObjectsDistinct, Officious
		// Interrogation's IncreaseCost$) de-duplicates by full target identity
		// in first-seen order -- Forge's `new ArrayList<>(new HashSet<>(objects))`.
		var out []state.Target
		seen := map[state.Target]bool{}
		for _, t := range refTargetUnion(c) {
			if t.Obj == 0 && !t.IsPlayer {
				continue
			}
			if ref == "TargetedObjectsDistinct" {
				if seen[t] {
					continue
				}
				seen[t] = true
			}
			out = append(out, t)
		}
		return out, true
	case refTargetsSpellTargeted:
		// Forge's SpellTargeted names the targeted SPELL -- the target that is
		// a spell on the stack (AbilityUtils.calcX's
		// `calcX[0].equals("SpellTargeted")` arm, which reads the FIRST of
		// `getDefinedSpellAbilities(card, "SpellTargeted", sa)` and counts its
		// host card). This build's target list already holds that spell's
		// object id (the cast pre-ask stored the target), so bind the first
		// OBJECT target; a chain with no object target yields the empty set
		// (ok=true, a legitimate zero). Reject Imperfection's, Press the
		// Enemy's, Gale's Redirection's and Sound the Trumpets'
		// SpellTargeted$CardManaCostLKI are the four corpus users. The mana
		// value is a printed face characteristic, so it survives the
		// counter/exile that follows the read and the LKI spelling reads the
		// same face value the plain spelling does (the shared CardManaCost/
		// CardManaCostLKI property arm below).
		//
		// The spell identity is the RESOLUTION-START snapshot (Ctx.TargetSpellLKI,
		// captured by effects.Resolve before any effect can move a target), not
		// the live zone: a Counter or ChangeZone earlier in the same chain has
		// already moved the spell off the stack when the later sub-ability reads
		// this ref (Reject Imperfection's DBProliferate gate, Gale's
		// Redirection's DBRoll modifier, Press the Enemy's DBMayPlay filter), so
		// a live ZStack test would lose exactly the spell the ref names. A
		// hand-built Ctx with no snapshot falls back to the live zone, which is
		// correct for a directly-evaluated body and never admits a battlefield
		// permanent (its object was never on the stack at entry).
		for _, t := range refTargetUnion(c) {
			if t.IsPlayer || t.Obj == 0 {
				continue
			}
			if c.Snap.TargetSpell[t.Obj] {
				return []state.Target{t}, true
			}
			if o := h.Game().Obj(t.Obj); o == nil || o.Zone != state.ZStack {
				continue
			}
			return []state.Target{t}, true
		}
		return nil, true
	default:
		// A ref this build's explicit cases do not name is delegated to the
		// SAME defined-targets resolver a body's own Defined$ spelling uses
		// (effects/context.go knownDefinedTargets), so the count vocabulary can
		// no longer lag the defined-targets vocabulary and the next sibling
		// spelling is covered without a new hand-written case. The explicit
		// cases ABOVE keep the deliberate count-only distinctions (the RAW
		// Imprinted associations, ChosenCard, the ExiledWith scan) ahead of this
		// fallback.
		//
		// The delegation claims the ref only when the resolved set carries at
		// least one OBJECT: a player-valued ref (TargetedPlayer, TriggeredTarget,
		// Player.IsRemembered, ...) belongs to the player arm
		// (evalPlayerRefProperty), and claiming it here with zero objects would
		// short-circuit that arm and read every player property as 0. An empty
		// or player-only resolution therefore still fails closed, exactly as
		// before the fallback existed -- an unknown ref too, since
		// knownDefinedTargets refuses a selector it does not model.
		if ts, ok := knownDefinedTargets(h, c, ref); ok {
			for _, t := range ts {
				if !t.IsPlayer && t.Obj != 0 {
					return ts, true
				}
			}
		}
		return nil, false
	}
}

// refTargetUnion is the target set Forge's chain-union refs read: every
// targeting SA's chosen targets down the root ability's sub-ability chain.
// Ctx.AllTargets carries that union when the cast pre-ask bound it (the
// AllTargeted ref's own binding); a resolution that did not bind one -- where
// no corpus line carries a chain union -- keeps the resolution's own
// Ctx.Targets, which for a chain with no sub targets IS the union. Shared by
// the AllTargeted case and the TargetedObjects/SpellTargeted refs so the
// three cannot disagree about what "the targets" means.
func refTargetUnion(c *Ctx) []state.Target {
	if c.AllTargets != nil {
		return c.AllTargets
	}
	return c.Targets
}

// castManaSpentTotals is one cast's recorded CR 601.2h / 106.12 spend
// breakdown: the unfiltered total, the snow-unit part, and the per-producer
// typed parts in state.TypedManaTags order. The plain Count$CastTotalManaSpent
// head reads it live off the source Object; the TriggeredCard$CastTotalManaSpent
// ref-head reads the fire-time snapshot a trigger context carries, so the two
// forms can never disagree about what one argument selects.
type castManaSpentTotals struct {
	total, snow int32
	typed       [4]int32
}

// manaSpentTotalsOf reads a cast object's recorded spend breakdown. The typed
// parts come from Object.TypedManaSpentByTag so adding a tag to
// state.TypedManaTags extends the head automatically.
func manaSpentTotalsOf(o *state.Object) castManaSpentTotals {
	t := castManaSpentTotals{total: o.ManaSpent, snow: o.ManaSnowSpent}
	for i := range state.TypedManaTags {
		t.typed[i] = o.TypedManaSpentByTag(i)
	}
	return t
}

// byTag selects the spend a Count$CastTotalManaSpent <Type> argument names:
// the bare total for an empty arg, the snow part for "Snow", a modelled typed
// tag for its word, and the fail-closed 0 for anything the pool cannot tag.
func (t castManaSpentTotals) byTag(arg string) int32 {
	switch countRefTagCodes.Code(string(arg)) {
	case countRefTagEmpty:
		return t.total
	case countRefTagSnow:
		return t.snow
	default:
		for i, tagWord := range state.TypedManaTags {
			if arg == tagWord {
				return t.typed[i]
			}
		}
		return 0
	}
}

type evalTriggerCountOKCode uint16

const (
	evalTriggerCountOKAmount evalTriggerCountOKCode = iota + 1
	evalTriggerCountOKResult
)

var evalTriggerCountOKCodes = state.NewStrCodes(
	state.StrEntry[evalTriggerCountOKCode]{Key: "DamageAmount", Val: evalTriggerCountOKAmount},
	state.StrEntry[evalTriggerCountOKCode]{Key: "LifeAmount", Val: evalTriggerCountOKAmount},
	state.StrEntry[evalTriggerCountOKCode]{Key: "Amount", Val: evalTriggerCountOKAmount},
	state.StrEntry[evalTriggerCountOKCode]{Key: "ScryBottom", Val: evalTriggerCountOKAmount},
	state.StrEntry[evalTriggerCountOKCode]{Key: "Result", Val: evalTriggerCountOKResult},
)

type evalSacrificedOKCode uint16

const (
	evalSacrificedOKCardPower evalSacrificedOKCode = iota + 1
	evalSacrificedOKCardToughness
	evalSacrificedOKCardManaCost
	evalSacrificedOKAmount
)

var evalSacrificedOKCodes = state.NewStrCodes(
	state.StrEntry[evalSacrificedOKCode]{Key: "CardPower", Val: evalSacrificedOKCardPower},
	state.StrEntry[evalSacrificedOKCode]{Key: "CardToughness", Val: evalSacrificedOKCardToughness},
	state.StrEntry[evalSacrificedOKCode]{Key: "CardManaCost", Val: evalSacrificedOKCardManaCost},
	state.StrEntry[evalSacrificedOKCode]{Key: "Amount", Val: evalSacrificedOKAmount},
)

type refTargetsCode uint16

const (
	refTargetsTargeted refTargetsCode = iota + 1
	refTargetsParentTarget
	refTargetsAllTargeted
	refTargetsTriggeredCard
	refTargetsRememberedLKI
	refTargetsTriggerRemembered
	refTargetsTriggeredExploited
	refTargetsTriggeredBlocker
	refTargetsTriggeredSpellAbility
	refTargetsCastSA
	refTargetsRemembered
	refTargetsImprinted
	refTargetsChosenCard
	refTargetsExiledWith
	refTargetsExiled
	refTargetsExiledCards
	refTargetsEquipped
	refTargetsTargetedObjects
	refTargetsSpellTargeted
)

var refTargetsCodes = state.NewStrCodes(
	state.StrEntry[refTargetsCode]{Key: "Targeted", Val: refTargetsTargeted},
	state.StrEntry[refTargetsCode]{Key: "ThisTargetedCard", Val: refTargetsTargeted},
	state.StrEntry[refTargetsCode]{Key: "ParentTarget", Val: refTargetsParentTarget},
	state.StrEntry[refTargetsCode]{Key: "ParentTargeted", Val: refTargetsParentTarget},
	state.StrEntry[refTargetsCode]{Key: "AllTargeted", Val: refTargetsAllTargeted},
	state.StrEntry[refTargetsCode]{Key: "TriggeredCard", Val: refTargetsTriggeredCard},
	state.StrEntry[refTargetsCode]{Key: "TriggeredCardLKICopy", Val: refTargetsTriggeredCard},
	state.StrEntry[refTargetsCode]{Key: "TriggeredNewCard", Val: refTargetsTriggeredCard},
	state.StrEntry[refTargetsCode]{Key: "TriggeredNewCardLKICopy", Val: refTargetsTriggeredCard},
	state.StrEntry[refTargetsCode]{Key: "TriggeredAttacker", Val: refTargetsTriggeredCard},
	state.StrEntry[refTargetsCode]{Key: "TriggeredAttackerLKICopy", Val: refTargetsTriggeredCard},
	state.StrEntry[refTargetsCode]{Key: "TriggeredTargetLKICopy", Val: refTargetsTriggeredCard},
	state.StrEntry[refTargetsCode]{Key: "DelayTriggerRemembered", Val: refTargetsTriggeredCard},
	state.StrEntry[refTargetsCode]{Key: "DelayTriggerRememberedLKI", Val: refTargetsTriggeredCard},
	state.StrEntry[refTargetsCode]{Key: "RememberedLKI", Val: refTargetsRememberedLKI},
	state.StrEntry[refTargetsCode]{Key: "TriggerRemembered", Val: refTargetsTriggerRemembered},
	state.StrEntry[refTargetsCode]{Key: "TriggeredExploited", Val: refTargetsTriggeredExploited},
	state.StrEntry[refTargetsCode]{Key: "TriggeredBlocker", Val: refTargetsTriggeredBlocker},
	state.StrEntry[refTargetsCode]{Key: "TriggeredBlockerLKICopy", Val: refTargetsTriggeredBlocker},
	state.StrEntry[refTargetsCode]{Key: "TriggeredSpellAbility", Val: refTargetsTriggeredSpellAbility},
	state.StrEntry[refTargetsCode]{Key: "CastSA", Val: refTargetsCastSA},
	state.StrEntry[refTargetsCode]{Key: "Remembered", Val: refTargetsRemembered},
	state.StrEntry[refTargetsCode]{Key: "Imprinted", Val: refTargetsImprinted},
	state.StrEntry[refTargetsCode]{Key: "ChosenCard", Val: refTargetsChosenCard},
	state.StrEntry[refTargetsCode]{Key: "ExiledWith", Val: refTargetsExiledWith},
	state.StrEntry[refTargetsCode]{Key: "Exiled", Val: refTargetsExiled},
	state.StrEntry[refTargetsCode]{Key: "Revealed", Val: refTargetsExiled},
	state.StrEntry[refTargetsCode]{Key: "ExiledCards", Val: refTargetsExiledCards},
	state.StrEntry[refTargetsCode]{Key: "Equipped", Val: refTargetsEquipped},
	state.StrEntry[refTargetsCode]{Key: "Enchanted", Val: refTargetsEquipped},
	state.StrEntry[refTargetsCode]{Key: "AttachedTo", Val: refTargetsEquipped},
	state.StrEntry[refTargetsCode]{Key: "TargetedObjects", Val: refTargetsTargetedObjects},
	state.StrEntry[refTargetsCode]{Key: "TargetedObjectsDistinct", Val: refTargetsTargetedObjects},
	state.StrEntry[refTargetsCode]{Key: "SpellTargeted", Val: refTargetsSpellTargeted},
)

type countRefTagCode uint16

const (
	countRefTagEmpty countRefTagCode = iota + 1
	countRefTagSnow
)

var countRefTagCodes = state.NewStrCodes(
	state.StrEntry[countRefTagCode]{Key: "", Val: countRefTagEmpty},
	state.StrEntry[countRefTagCode]{Key: "Snow", Val: countRefTagSnow},
)
