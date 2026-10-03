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
	Register("CopySpellAbility", effCopySpellAbility)
}

// effCopySpellAbility duplicates the spell named by Defined$ (Task 17). Two
// shapes exist in the corpus:
//
//   - Defined$ Parent: the spell currently resolving (c.Source) — Chain
//     Lightning's own copy clause.
//   - Defined$ TriggeredSpellAbility: the trigger's remembered object, i.e.
//     the spell whose cast FIRED the trigger — Storm (cards/keywords.go
//     expands kw:Storm into exactly this).
//
// Amount$ copies are placed on the stack by StackCopy events (default 1),
// each copy keeping its targets unless the creating SA declares
// MayChooseTarget$ True (CR 707.10c: that copy's controller may choose new
// targets). The permission rides the StackCopy event's Amount discriminator,
// which rules/stack.go's resolveTop reads as a one-shot target election over
// the copy's own target requirement — so an external copier (Mirari, Cloven
// Casting, a Storm or Replicate copy) grants it too. Copies keep their
// targets only when the parameter is absent or False.
//
// NonLegendary$ True (The Sixth Doctor's Time Lord's Prerogative, and the
// corpus's six-carrier CopySpellAbility family: Osgood Operation Double, Iron
// Man Bleeding Edge, Jackal Genius Geneticist, Storm of Saruman, The Clone
// Saga) drops the Legendary supertype from the copy's characteristics, so a
// copied legendary PERMANENT spell resolves into a token CR 704.5j must not
// gather against its original. The rider rides the StackCopy event's Counter
// (a field the fold otherwise leaves empty) and is folded into the mint as
// Object.CopyNonLegendary, which rules' typeCharacteristics applies as a
// layer-4 base strip -- no event field is added or reordered. An absent or
// False key adds no strip, but a copy of a previously stripped copy inherits
// that copiable characteristic (CR 707.2).
//
// UnlessCost$ (Chain Lightning, String of Disappearances) rides the ONE
// shared unless gate (effects.Resolve's unlessProceed dispatch, shared by
// every API): the payer is UnlessPayer$'s resolved target (default the
// target's controller), the pay/decline labels live in poseUnlessAsk's
// CopySpellAbility arm, and the copy loop below runs, or not, exactly once
// per the gate's orientation (rules' unless_pay resume arm charged the cost
// on an affordable "pay"). A host that cannot ask (an effects-package test
// double) keeps the deterministic decline.
func effCopySpellAbility(h Host, c *Ctx, sa *cards.SA) {
	g := h.Game()
	// Optional$ True (Sevinne's Reclamation's "if this spell was cast from a
	// graveyard, you may copy this spell", the corpus's wider may-copy
	// family, and the Optional$+UnlessCost$ carriers Wandering Archaic and
	// Chain of Silence) makes the copy itself a may effect: the copy's
	// controller is asked a yes/no before any copy is made, and a decline
	// makes none. The answered election rides Ctx.CopyOpt (the same
	// runtime-continuation class as AttachOpt), consumed and cleared here so
	// a nested CopySpellAbility poses its own ask (fx42 scoping). An absent
	// key leaves the historical unconditional copy. A host that cannot ask
	// (an effects-package double, fuzz) keeps the deterministic pre-ask
	// behaviour -- the copy is made -- the no-host stand-in the election's
	// own branch implements below (the ask's bracket is Min == Max == 1, so
	// Clamp and the bot answer option 0 = yes).
	copyOpt := c.CopyOpt
	c.CopyOpt = ""
	// Resolve which spell to copy. For a trigger the remembered entry is the
	// cast spell (the first object entry); for a direct Parent copy it is the
	// currently resolving spell itself.
	var spell state.ObjID
	// validStackSpells is the PLURAL result of a Defined$ ValidStack copy arm
	// (CR 707.10a, "copy each"): every stack object the spec admits, in
	// stack-arena order, each copied Amount$ times. It is nil for every other
	// Defined form, whose established single-spell shape is unchanged.
	var validStackSpells []state.ObjID
	switch strings.TrimSpace(sa.ParamStr(cards.PKDefined)) {
	case "TriggeredSpellAbility":
		// The activation arm (abcopy1): the trigger context's TriggerAbility
		// names the minted ability wrapper -- an AbilityPush's Obj is the
		// source permanent, so Remembered alone cannot identify it -- and the
		// wrapper is on the stack, so the zone guard below passes and the copy
		// actually resolves. Role absent (the spell arm, where Remembered IS
		// the cast spell, and hand-built contexts) keeps the remembered entry.
		if id := c.TriggerAbility; id != 0 {
			spell = id
		} else {
			for _, t := range c.Remembered {
				if !t.IsPlayer && t.Obj != 0 {
					spell = t.Obj
					break
				}
			}
		}
	case "Parent":
		// An explicit Parent copy copies the resolving spell itself.
		spell = c.Source
	default: // "Targeted" and any unset/other name
		// An SA that names targets copies its TARGET: every corpus
		// CopySpellAbility line without Defined$ (66, Flare of Duplication
		// and the whole may-choose family) carries ValidTgts$, and Forge's
		// own default define for CopySpellAbility is the targeted spell. The
		// old unset default (the resolving spell itself) made such a spell
		// copy ITSELF — Flare's copy is again a Flare whose copy is again a
		// Flare — an unbounded self-copy loop no game could finish. With no
		// object target recorded (a fizzled ask), the copy does nothing.
		for _, t := range c.Targets {
			if !t.IsPlayer && t.Obj != 0 {
				spell = t.Obj
				break
			}
		}
		if spell == 0 {
			// Defined$ ValidStack <spec> (Ulalek, Fused Atrocity's "copy all
			// spells you control" and its SubAbility$'s "copy all other
			// activated and triggered abilities you control"): the one Defined
			// form this arm cannot read off c.Targets -- a ValidStack spec
			// resolves from the STACK, not from the trigger's targets. Route
			// it through the shared ValidStack resolver every other Defined
			// consumer uses, but ONLY when some comma token actually names a
			// stack kind (state.StackKindTokenOf): the parser's no-usable-token
			// degradation to Spell-only must not leak in here -- a genuinely
			// unknown token would otherwise widen to "all spells you control"
			// and copy every spell on the stack. Fail closed instead. The
			// `Ability` base IS a stack kind now (task abcopy3: Activated+
			// Triggered, never Spell), and its `otherAbility` qualifier
			// excludes the resolving wrapper via Ctx.ResolvingObj, so the
			// sub-copy cannot copy itself; the arm is PLURAL -- every admitted
			// object is copied, so Ulalek's "copy all spells you control" and
			// "copy all other activated and triggered abilities" place one copy
			// per match instead of only the first. The family exclusion is
			// untouched by this: validStackAdmits still drops the resolving
			// wrapper AND every instance or copy sharing (Source, Ability), the
			// recorded livelock guard -- an id-only exclusion would let a copy
			// of the wrapper be copied again and ask its pay question forever.
			spec := strings.TrimSpace(sa.ParamStr(cards.PKDefined))
			if stackSpec, ok := strings.CutPrefix(spec, "ValidStack"); ok {
				for tok := range strings.SplitSeq(strings.TrimSpace(stackSpec), ",") {
					if _, known := state.StackKindTokenOf(strings.TrimSpace(tok)); known {
						if ts, knownAll := knownDefinedTargets(h, c, spec); knownAll {
							for _, t := range ts {
								if t.IsPlayer || t.Obj == 0 {
									continue
								}
								if o := g.Obj(t.Obj); o == nil || o.Zone != state.ZStack {
									continue
								}
								validStackSpells = append(validStackSpells, t.Obj)
							}
						}
						break
					}
				}
			}
		}
		if spell == 0 && len(validStackSpells) == 0 {
			return
		}
	}
	if spell != 0 {
		// The source spell must actually be on the stack; a copy of something
		// that already left it is a no-op, the same totality stance as every
		// other effect primitive. The plural ValidStack arm above already
		// applied this filter per match.
		if o := g.Obj(spell); o == nil || o.Zone != state.ZStack {
			return
		}
		if !spellCopyAllowed(h, spell) {
			return
		}
	}
	if len(validStackSpells) > 0 {
		allowed := validStackSpells[:0]
		for _, id := range validStackSpells {
			if spellCopyAllowed(h, id) {
				allowed = append(allowed, id)
			}
		}
		validStackSpells = allowed
		if len(validStackSpells) == 0 {
			return
		}
	}

	// Controller$ (Chain Lightning's "If the player does, they may copy this
	// spell" -- CR 707.10's copy-ownership half): the copy belongs to the
	// player the parameter names, not the resolving controller. The target-
	// derived family resolves against the copied spell's own targets (the
	// shared c.Targets a Parent copy inherits); an unknown selector keeps
	// the historical resolving-controller default rather than inventing a
	// binding (measured corpus: only the TargetedOrController spelling
	// reaches a registered copy on a measured path). Resolved BEFORE the
	// Optional$ election below, because on the corpus's combined carriers
	// (Wandering Archaic's Controller$ You, Chain of Silence's Controller$
	// TargetedController) the may-copy election is the addressed player's,
	// not the resolving controller's.
	controller := c.Controller
	if spec := strings.TrimSpace(sa.Params["Controller"]); spec != "" {
		if p, ok := copyControllerFor(g, c, spec); ok {
			controller = p
		}
	}

	// Optional$ True (Sevinne's Reclamation's "you may copy this spell", the
	// corpus's wider may-copy family, and the Optional$+UnlessCost$ carriers
	// Wandering Archaic and Chain of Silence): pose the may-copy election once
	// the copy is known to be possible (a resolvable, still-on-stack spell),
	// so a fizzled copy clause asks nothing. A decline ("no") returns without
	// emitting any StackCopy; an answered "yes" falls through to the ordinary
	// copy path.
	//
	// An SA that ALSO carries UnlessCost$ composes SEQUENTIALLY, not
	// exclusively: the shared unless gate (effects/unless.go's unlessProceed,
	// called by Resolve before this dispatch) resolves its pay/decline first,
	// and its orientation decides only whether this BODY runs at all --
	// Wandering Archaic's unswitched "they may pay {2}. If they don't, you may
	// copy" runs the body on the DECLINE, Chain of Silence's UnlessSwitched$
	// "may sacrifice a land. If the player does, they may copy" runs it on the
	// PAY. By the time this dispatch runs the unless question is fully
	// answered -- a body ask suspends with Host.SuspendUnless's marker, so the
	// answered re-entry consumes it and never re-poses the pay ask -- so the
	// may-copy election is the SECOND ask in Forge's own sequence: posing it
	// duplicates nothing and inverts nothing (the round-1 read that scoped
	// this arm to UnlessCost$-free SAs was wrong, findings-r2).
	if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKOptional)), "True") {
		switch copyOpt {
		case "yes":
			// Answered yes: fall through to the copy below.
		case "no":
			return
		default:
			d := &decision.Decision{Player: controller, Kind: decision.KChoose, Min: 1, Max: 1,
				Source: c.Source, ResumeKind: "copy_optional", ResumeSA: sa,
				CopyOfCopy:       copyOfCopy(g, spell),
				ResumeRemembered: copyTargets(c.Remembered),
				Prompt:           "Copy it?",
				Options: []decision.Option{
					{Index: 0, Kind: "yes", Label: "Yes — copy", Player: controller},
					{Index: 1, Kind: "no", Label: "No", Player: controller},
				}}
			if Ask(h, d) == AskAsked {
				// The election was posted and suspended the resolution; the
				// answered copy_optional re-entry (rules' resume arm) carries
				// Ctx.CopyOpt back into this same SA.
				return
			}
			// AskNoHost (an effects-package double, a fuzz run) and AskEmpty
			// (unreachable with a two-option Min-1 ask) keep the deterministic
			// pre-ask stand-in the doc comment above records: the copy is made.
			// Fall through to the copy below.
		}
	}
	// IgnoreFreeze$ True (Ulalek, Fused Atrocity's copy trigger; Forge's
	// generic copy template carries it too): Forge MagicStack's frozen flag
	// blocks adding to the stack while it holds, and a copy SA carrying this
	// key is exempt. This engine has no stack freeze -- nothing in the
	// implemented ruleset suspends stack additions -- so there is no gate to
	// relax; the recognition read documents the parameter so the census never
	// flags it unread (review sol2: the earlier empty if-block was dropped).
	_ = sa.Params["IgnoreFreeze"]
	mayChoose := strings.EqualFold(strings.TrimSpace(sa.Params["MayChooseTarget"]), "True")
	// NonLegendary$ True (The Sixth Doctor's "copy it, except the copy isn't
	// legendary", and the corpus's six-carrier CopySpellAbility family:
	// Osgood Operation Double, Iron Man Bleeding Edge, Jackal Genius
	// Geneticist, Storm of Saruman, The Clone Saga). The copy's
	// characteristics drop the Legendary supertype, so a copied legendary
	// PERMANENT spell resolves into a token CR 704.5j must not gather against
	// its original. The rider rides the StackCopy event's Counter -- the
	// field StackCopy otherwise leaves empty -- and events.Apply's fold
	// turns it into the object marker rules' typeCharacteristics strips the
	// supertype from, the same event-sourced shape ClonePermanent's Counter
	// riders take. No event field is added or reordered; an absent or False
	// key adds no strip, but a copy of a stripped copy inherits the strip.
	nonLegendary := strings.EqualFold(strings.TrimSpace(sa.Params["NonLegendary"]), "True")
	// SetLoyalty$ (the corpus's one carrier is Ob Nixilis, the Adversary's
	// Casualty:X script rider): the copy's starting loyalty, delivered
	// RESOLVED -- the creating trigger's payload already substituted the
	// casualty amount for the script's `Casualty` value, so the value here
	// is a number. The rider composes into the StackCopy event's Counter
	// payload (events.StackCopyCounter), which the entry fold
	// (events.EntryCounterGrants) places instead of the printed face value.
	// An absent key leaves the printed starting loyalty; an unparseable
	// value cannot arise from the payload path and is ignored.
	var riders events.StackCopyCounter
	riders.NonLegendary = nonLegendary
	if raw := strings.TrimSpace(sa.Params["SetLoyalty"]); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			riders.Loyalty, riders.HasLoyalty = int32(n), true
		}
	}
	payload := events.StackCopyCounterString(riders)
	// RememberCopies$ True (Shiko and Narset, Unified's "copy that spell ...
	// If you don't copy a spell this way, draw a card"; Chef's Kiss's "the
	// spell and the copy"; Tempt with Mayhem's per-copier count): Forge's
	// CopySpellAbilityEffect.resolve ends with card.addRemembered(copies) for
	// EVERY copy it actually made. Read once here and threaded into both
	// emit sites below; an absent/False key leaves the remembered set
	// untouched, exactly the historical behaviour.
	rememberCopies := strings.EqualFold(strings.TrimSpace(sa.Params["RememberCopies"]), "True")
	// DefinedTarget$ (Feather, Radiant Arbiter's DefinedTarget$ ChosenCard,
	// Ivy, Gleeful Spellthief's DefinedTarget$ Self): Forge's
	// CopySpellAbilityEffect makes ONE copy per defined target, each with
	// its target REPLACED by that entry (the StackCopy event's IDs override
	// the inherited list, events/event.go) -- "for each of those creatures,
	// copy that spell. The copy targets that creature." The copy count is
	// the defined set's size, never Amount$: no corpus carrier combines the
	// param with an Amount$ (they name none), and the param is exactly the
	// override of the copy-keeps-targets stand-in below. An unresolvable
	// value (Zevlor, Elturel Exile's OppNonTriggeredSpellAbilityTargetsOr-
	// Controller) keeps the historical single inherited-target copy under
	// one loud Note -- the override applies only where the engine can
	// resolve what it names.
	targets, defined := copyDefinedTargets(h, c, sa)
	switch {
	case defined && len(targets) > 0:
		// One copy per defined target, each carrying that entry alone in its
		// IDs. Amount$ is deliberately not consulted on this route (no corpus
		// carrier combines the two; Forge's definedTarget branch ignores it
		// too).
		for _, t := range targets {
			ev := events.Event{Kind: events.StackCopy, Obj: spell, Player: controller, IDs: []state.ObjID{t.Obj}, Counter: payload}
			emitCopy(h, c, rememberCopies, ev)
		}
	case defined:
		// A resolvable param whose set is empty (nothing chosen): no copy,
		// no Note -- the resolution's sub-abilities (the cleanup chain) still
		// run.
	default:
		if spec := strings.TrimSpace(sa.Params["DefinedTarget"]); spec != "" {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "copy: DefinedTarget$ " + spec + " not resolved; copy keeps its targets"})
		}
		n := int(Num(h, c, sa, "Amount", 1))
		copies := validStackSpells
		if len(copies) == 0 {
			copies = []state.ObjID{spell}
		}
		for _, sp := range copies {
			for i := 0; i < n; i++ {
				ev := events.Event{Kind: events.StackCopy, Obj: sp, Player: controller, Counter: payload}
				if mayChoose {
					// CR 707.10c: the copy's controller may choose new targets. The
					// permission rides the StackCopy event (Amount 1), so it is
					// recorded per copy instance and replayed; rules/stack.go's
					// resolveTop poses the election and the TargetsChosen fold
					// consumes it. No Note: the election is now real, not a
					// stand-in.
					ev.Amount = 1
				}
				emitCopy(h, c, rememberCopies, ev)
			}
		}
	}
}

// spellCopyAllowed asks the rules host whether the candidate spell's own
// static permits copying. Effects test doubles without this optional rules
// seam retain their established behavior.
func spellCopyAllowed(h Host, id state.ObjID) bool {
	checker, ok := h.(interface{ SpellCopyAllowed(state.ObjID) bool })
	return !ok || checker.SpellCopyAllowed(id)
}

// emitCopy emits one StackCopy event. When remember is set it routes through
// the mint-returning Host.EmitStackCopy and appends every minted copy object
// to the resolution's remembered set -- both halves, exactly as
// effects/token.go's RememberTokens$ rider does for a token mint: the chain
// local Ctx.Remembered (read by Defined$ Remembered and by the ConditionDefined$
// Remembered gate) and the source object's persistent event-backed list via
// eventRemember (Forge's card.addRemembered). A copy that the fold did not
// actually mint (an early break -- the source left the stack) returns no id
// and is not remembered, mirroring Forge's own early returns. An absent or
// False RememberCopies$ leaves remember false, so emission is the historical
// plain Emit.
func emitCopy(h Host, c *Ctx, remember bool, ev events.Event) {
	if !remember {
		h.Emit(ev)
		return
	}
	for _, id := range h.EmitStackCopy(ev) {
		c.Remembered = append(c.Remembered, state.Target{Obj: id})
		eventRemember(h, c, id)
	}
}

// copyDefinedTargets resolves a CopySpellAbility DefinedTarget$ parameter to
// the per-copy target list. true (defined) means the param is present and
// every value this build recognises resolved: the copies follow the defined
// entries one-for-one and the caller never falls back to the inherited
// targets. false covers both the absent param and an unresolvable value, so
// the caller can keep the historical copy-keeps-targets shape (under the
// loud Note) instead of silently dropping the param.
func copyDefinedTargets(h Host, c *Ctx, sa *cards.SA) ([]state.Target, bool) {
	spec := strings.TrimSpace(sa.Params["DefinedTarget"])
	if spec == "" {
		return nil, false
	}
	g := h.Game()
	switch spec {
	case "ChosenCard":
		// The ChooseCard chain's answered set (resolutionChosenCards is the
		// shared read Count$ChosenSize and Defined$ ChosenCard take, so the
		// copies can never disagree with either). Only object entries copy:
		// a ChooseCard's chosen PLAYERS are not "those creatures".
		var out []state.Target
		for _, t := range resolutionChosenCards(g, c) {
			if !t.IsPlayer && t.Obj != 0 {
				out = append(out, t)
			}
		}
		return out, true
	case "Self":
		// Ivy, Gleeful Spellthief: the copy targets the trigger's source
		// permanent ("The copy targets NICKNAME"). An absent source fails
		// the set empty -- defined, but nothing to target.
		if c.Source == 0 || g.Obj(c.Source) == nil {
			return nil, true
		}
		return []state.Target{{Obj: c.Source}}, true
	}
	return nil, false
}

// copyControllerFor resolves a CopySpellAbility Controller$ selector to the
// player who receives the copy. Only the target-derived family is modelled:
// TargetedOrController is the Forge spelling for "the target if it is a
// player, else the target's controller" (Chain Lightning's cycle), and the
// plain Targeted/TargetedController/TargetedPlayer forms read the same
// binding. A missing target (a fizzled ask) fails to ok=false and the caller
// keeps its controller.
// copyOfCopy reports whether the spell a may-copy election would copy is
// itself a stack copy (decision.Decision.CopyOfCopy: the chain-continuation
// marker the unattended bot declines).
func copyOfCopy(g *state.Game, spell state.ObjID) bool {
	o := g.Obj(spell)
	return o != nil && o.IsCopy
}

func copyControllerFor(g *state.Game, c *Ctx, spec string) (state.PlayerID, bool) {
	switch spec {
	case "TargetedOrController", "Targeted", "TargetedController", "TargetedPlayer",
		"ThisTargetedController", "ThisTargetedPlayer":
		for _, t := range c.Targets {
			if t.IsPlayer {
				return t.Player, true
			}
			if o := g.Obj(t.Obj); o != nil {
				return o.Controller, true
			}
		}
		return 0, false
	case "ChosenPlayer", "Player.Chosen":
		for _, t := range c.Chosen {
			if t.IsPlayer {
				return t.Player, true
			}
		}
		return 0, false
	case "Remembered", "RememberedController":
		// Tempt with Mayhem's per-opponent copy: the RepeatEach loop binds its
		// current subject (a player) as Remembered, and prior iterations' copy
		// objects are remembered too. A remembered PLAYER named directly wins
		// over an object's controller -- the same precedence Defined$'s
		// single-player read takes -- so the copy belongs to the current
		// subject, else to the first remembered object's controller. An absent
		// binding returns false and the caller keeps the resolving controller.
		for _, t := range c.Remembered {
			if t.IsPlayer {
				return t.Player, true
			}
		}
		for _, t := range c.Remembered {
			if o := g.Obj(t.Obj); o != nil {
				return o.Controller, true
			}
		}
		return 0, false
	case "You":
		return c.Controller, true
	case "NextOpponentToYourLeft", "NextPlayerToYourLeft":
		// Barroom Brawl's "Then that player [the opponent to your left] may
		// copy this spell": the next living seat after the resolving
		// controller in turn order (this build has no teams). Before this arm
		// the unknown selector fell back to the resolving controller, so the
		// CASTER was offered its own copy, and the copy's copy, forever
		// (cardfuzz batch1 line 1).
		alive := g.AliveFrom(c.Controller)
		if len(alive) < 2 {
			return 0, false
		}
		return alive[1], true
	}
	return 0, false
}
