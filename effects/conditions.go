package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// conditions.go implements the TWO Condition* shapes this build gates a
// sub-ability on:
//
//  1. `ConditionDefined$ Remembered` + optional `ConditionPresent$ <spec>` +
//     optional `ConditionCompare$ <op><n>` — Delver of Secrets' "transform
//     only if the revealed card was an instant or sorcery" shape (task
//     fb-3f1cc033).
//  2. `ConditionPresent$ <spec>` with NO `ConditionDefined$`, whose Forge
//     default group is the whole battlefield — the conditional
//     enters-tapped land family (Blooming Marsh and the fast / check lands,
//     task fb-9d2338cc: `Land.YouCtrl` GT2 / `Plains.YouCtrl,Island.YouCtrl`
//     EQ0). See conditionMetBattlefield for the entering-object exclusion
//     that makes "two or fewer OTHER lands" mean what the oracle says.
//
// Forge spells the compare without a space ("EQ1"; 793 corpus lines) and the
// brief's "EQ 1" spelling is accepted too.
//
// Two more shapes landed with the action-level SVar-condition task (the
// paramcensus Draw/LoseLife/ChangeZone/Reveal/DealDamage cluster):
//
//  3. `ConditionCheckSVar$` + optional `ConditionSVarCompare$` — the SVar
//     gate (544 corpus SAs carry the pair): the named SVar (or inline
//     Count$-style expression) is evaluated with EvalCount and compared
//     under SVarCompare$ ("<op><threshold>", threshold optionally an SVar
//     name). No SVarCompare$ means "nonzero", Forge's default truthiness
//     read. The shared evaluator is CheckSVarHolds below — the same one the
//     statics' CheckSVar$ (rules.Engine.checkSVarHolds) and the
//     ability-offer gate (rules/legal.go sVarGateOK) delegate to. The FAIL
//     DIRECTION on an unmodelled count body is the CALLER's, not this
//     file's: CheckSVarHolds reports evaluated=false and each of the three
//     call sites documents its own choice — conditionMet and the offer gate
//     fail OPEN (run-anyway), the statics wrapper fails CLOSED.
//  4. a bare `Condition$` whose value is `Kicked` — the source was cast
//     with its Kicker paid (Into the Roil's "If this spell was kicked,
//     draw a card", the corpus's dominant bare-Condition value at 54 SAs);
//     `Foretold` — the FlagForetold cast provenance (Poison the Cup,
//     Alrund's Epiphany); and `Revolt` — CR 702.38's "a permanent you
//     controlled left the battlefield this turn" (Decommission's DB$
//     GainLife), read through Host.RevoltHolds; and `Blessing` -- CR
//     702.131's city's-blessing latch (state.Player.Blessing, granted by
//     the Ascend machinery), read straight off the folded state; and
//     `Delirium` — four or more distinct core card types in the resolving
//     controller's graveyard (Descend upon the Sinful's DB$ Token), read
//     through Host.DeliriumHolds — the same census the "Delirium —"
//     activation/continuous/replacement gates already share. The other
//     bare-Condition values (OptionalCost, Bargain, Threshold,
//     Hellbent, Surge — ~25 SAs) stay unresolved.
//  5. `ConditionDefined$ Imprinted` (34 corpus lines over 26 files) — the
//     source card's persistent imprint list (state.Object.Imprinted, the
//     events.Imprint associations: Chrome Mox's Imprint$ and now api:Play's
//     ImprintPlayed$ recording, Rashmi and Ragavan's played-exiled-card
//     marker). The group is exactly that list — never the remembered set —
//     and a source-less context (c.Source 0, a synthetic fixture) leaves
//     the gate unresolved, the fail-open run-anyway this file's convention.
//
// Deliberate scope (task fb-3f1cc033): the wider Condition vocabulary —
// Condition$ beyond Kicked, ConditionZone$ (57), ConditionManaSpent$ (34),
// the other ConditionDefined$ values (Targeted 161, ChosenCard 90, ... —
// Imprinted is IN since the ImprintPlayed task, shape 5 above), a bare
// ConditionCompare$ with no group, and ConditionNotPresent$ (8) — is NOT
// implemented. A sub carrying any of
// those is UNRESOLVED: conditionMet reports resolved=false and Resolve's
// walk runs the sub UNCONDITIONALLY, exactly as it did before this file
// existed. That is the documented (not fail-closed) choice: fail-closed
// skipping would change the behaviour of cards whose gates name specs this
// build cannot evaluate (Fatal Push's Creature.cmcLEX, Molten Rain's
// Land.Basic on a Targeted defined group) in ways no test asked for, while
// ungated preserves every observable behaviour except the shapes the
// supported set covers. Every unresolved key family is listed in the task
// report's Issues section.
//
// The sacrifice chooser additionally needs one narrower bridge for a
// post-sacrifice loop body: a `Defined$ Player.IsRemembered` effect — or its
// `Defined$ You` continuation — whose SVar body is `Remembered$Valid
// <known-spec>`. Braids uses it to distinguish an
// opponent who took the optional sacrifice from one who declined. That
// shape is subsumed by the general CheckSVarHolds gate below (the
// Remembered$Valid body evaluates through evalRememberedOK/evalRefProperty),
// so no separate bridge is kept: every Remembered$Valid gate — Braids's
// included — goes through the shared evaluator.

// CheckSVarHolds evaluates Forge's CheckSVar$/SVarCompare$ intervening-if
// gate — the ONE SVar-compare evaluator this build ships, shared by three
// call sites: conditionMet's ConditionCheckSVar$ branch (this file),
// rules.Engine.checkSVarHolds (the statics' CheckSVar$), and rules/legal.go's
// ability-offer gate (an AB's CheckSVar$, Bloodsoaked Champion's Raid). It
// reports (holds, evaluated). holds is the compare's answer;
// evaluated is false when the gate's count body is not one the evaluator
// models (EvalCountOK's verdict) or the compare operator/threshold is
// unreadable — the caller picks its own fail direction for that case, and
// each of the three call sites documents its own:
//
//   - conditionMet (ConditionCheckSVar$, this file): fail OPEN — the gated
//     ability runs, the pre-gate behaviour;
//   - rules/legal.go sVarGateOK (an AB's CheckSVar$ at offer time): fail
//     OPEN — the ability is offered (a gate you cannot read must not
//     silently remove a card's only activation);
//   - rules/statics.go checkSVarHolds (a static's CheckSVar$): fail CLOSED
//     — the shipped statics convention (an unreadable "as long as" gate
//     must not silently always-apply a continuous effect).
//
// The named SVar (c.SVars first, then the source face's own table) or the
// raw inline Count$-style expression is evaluated with EvalCountOK and
// compared under cmp ("<op><threshold>", e.g. GE11 — the threshold may also
// be an SVar name or an inline expression, resolved the same way). No cmp
// means "nonzero" (Forge's default truthiness read).
func CheckSVarHolds(h Host, c *Ctx, check, cmp string) (holds, evaluated bool) {
	check = strings.TrimSpace(check)
	if check == "" {
		return true, false
	}
	if c == nil {
		c = &Ctx{}
	}
	g := h.Game()
	body := check
	if c.SVars != nil {
		if b, ok := c.SVars[check]; ok {
			body = b
		}
	}
	if body == check && c.Source != 0 {
		// The ctx carried no table (or no entry): fall back to the source
		// face's own SVar table, the same lookup rules.Engine.checkSVarHolds
		// builds its ctx around.
		if o := g.Obj(c.Source); o != nil && o.Face() != nil {
			if b, ok := o.Face().SVars[check]; ok {
				body = b
			}
		}
	}
	var val int32
	if v, ok := sourceRuntimeSVar(g, c, check); ok {
		// A runtime write (api:StoreSVar) shadows the printed body of the
		// same name, exactly as NumResolved and the SVar$ head read it.
		// Without this a repeat-while gate over a StoreSVar flag (Sword of
		// Dungeons & Dragons: RepeatCheck starts at Number$ 1 and the d20
		// body stores 0 on a miss) read the printed 1 forever and looped
		// to the 1000-iteration cap.
		val = v
	} else {
		v, ok := EvalCountOK(h, c, body)
		if !ok {
			return false, false
		}
		val = v
	}
	cmp = strings.TrimSpace(cmp)
	if cmp == "" {
		return val != 0, true
	}
	if len(cmp) < 3 {
		return false, false
	}
	op, rhs := strings.ToUpper(cmp[:2]), cmp[2:]
	var threshold int32
	switch n, err := strconv.ParseInt(rhs, 10, 64); {
	case err == nil:
		threshold = int32(n)
	case c.SVars != nil:
		if b, found := c.SVars[rhs]; found {
			threshold = EvalCount(h, c, b)
		} else if t, ok2 := EvalCountOK(h, c, rhs); ok2 {
			threshold = t
		} else {
			// A threshold that resolves nowhere: unreadable compare.
			return false, false
		}
	default:
		return false, false
	}
	switch op {
	case "EQ":
		return val == threshold, true
	case "GE":
		return val >= threshold, true
	case "GT":
		return val > threshold, true
	case "LE":
		return val <= threshold, true
	case "LT":
		return val < threshold, true
	case "NE":
		// Forge's CompareOperator NE. Four corpus carriers (Spark Fiend's
		// upkeep roll gate `CheckSVar$ Safe | SVarCompare$ NE0`); unread, the
		// compare failed closed and the trigger never fired.
		return val != threshold, true
	}
	return false, false
}

// conditionMet evaluates sa's Condition* gate against the resolving
// context. It returns (met, resolved):
//
//   - (…, true)  the gate is one of the supported shapes and met says
//     whether the sub should run;
//   - (…, false) the gate carries something unsupported — the caller runs
//     the sub anyway (the pre-condition-engine behaviour, documented in
//     the task report). A ConditionNotPresent$, a non-Remembered
//     ConditionDefined$, a bare ConditionCompare$ with no group, a bare
//     Condition$ whose value is not Kicked, and the mixed shapes are all in
//     this class.
//   - a sub with no Condition* key at all is not gated: (true, false).
//
// Two more keys landed with the Unbreakable Formation task
// (agent-20260918T200326Z-10b49320): `ConditionPlayerTurn$ True|False` —
// the resolving controller's turn vs. not — and `ConditionPhases$ <list>`
// (Main1,Main2 — the Addendum family), both read through the ONE shared
// phase-name parser state.ParsePhases and AND-ed with whatever group gate
// the SA also carries.
func conditionMet(h Host, c *Ctx, sa *cards.SA) (met bool, resolved bool) {
	defined := strings.TrimSpace(sa.Params["ConditionDefined"])
	present := strings.TrimSpace(sa.Params["ConditionPresent"])
	notPresent := strings.TrimSpace(sa.Params["ConditionNotPresent"])
	compare := strings.TrimSpace(sa.Params["ConditionCompare"])
	// PresentDefined$/IsPresent$/PresentCompare$ are the DB-body spellings of
	// the same defined-group presence gate ConditionDefined$/
	// ConditionPresent$/ConditionCompare$ express. Normalize here so every
	// effect body uses the same evaluator and group support as
	// ConditionDefined. The filter key is spelled IsPresent$ (the corpus's
	// DB-body spelling: Experimental Lab // Staff Room's DBPutCounter and
	// DBTurnFaceUp are the only two `DB$ ... PresentDefined$` lines in the
	// corpus, and both carry IsPresent$); a bare Present$ key does not exist
	// in the corpus, so it is deliberately NOT read here.
	presentDefined := strings.TrimSpace(sa.Params["PresentDefined"])
	presentCompare := strings.TrimSpace(sa.Params["PresentCompare"])
	if presentDefined != "" {
		if defined != "" || present != "" || compare != "" {
			return false, false
		}
		defined = presentDefined
		present = strings.TrimSpace(sa.Params["IsPresent"])
		compare = presentCompare
	}
	check := strings.TrimSpace(sa.Params["ConditionCheckSVar"])
	svarCmp := strings.TrimSpace(sa.Params["ConditionSVarCompare"])
	bare := strings.TrimSpace(sa.Params["Condition"])
	playerTurn := strings.TrimSpace(sa.Params["ConditionPlayerTurn"])
	phases := strings.TrimSpace(sa.Params["ConditionPhases"])
	firstCombat := strings.TrimSpace(sa.Params["ConditionFirstCombat"])
	activationLimit := strings.TrimSpace(sa.Params["ConditionActivationLimit"])
	if defined == "" && present == "" && notPresent == "" && compare == "" && check == "" && bare == "" &&
		playerTurn == "" && phases == "" && firstCombat == "" && activationLimit == "" {
		return true, false // not gated (a lone ConditionSVarCompare$ compares nothing)
	}
	// ConditionActivationLimit$ <op><n> (4 corpus lines: Farrelite Priest,
	// Initiates of the Ebon Hand, Dragon Whelp, Nalathni Dragon -- "if this
	// ability has been activated four or more times this turn"): compares
	// the resolving activated ability's activation count this turn,
	// including the current activation, which rules binds as
	// Ctx.ActivationsThisTurn. An unbound count (0: no activated ability is
	// resolving, or a synthetic Ctx) or an unreadable compare stays
	// unresolved -- the fail-open run-anyway this file's convention. It is
	// only a corpus shape alone, so any other Condition key beside it is
	// unsupported too.
	if activationLimit != "" {
		if defined != "" || present != "" || notPresent != "" || compare != "" || check != "" || bare != "" ||
			playerTurn != "" || phases != "" || firstCombat != "" || c.ActivationsThisTurn <= 0 {
			return false, false
		}
		return activationCountHolds(c.ActivationsThisTurn, activationLimit)
	}
	// Any other Condition* key (Zone, ManaSpent, ...) beside the supported
	// nine makes the shape unsupported. ConditionDescription$ is
	// display text, not part of the evaluation, and is ignored.
	// (The ConditionCheckSVar$ shape below covers the sacrifice-continuation
	// bridge the pre-merge build carried as rememberedSacrificeCondition:
	// Braids's `Defined$ Player.IsRemembered` legs with a `Remembered$Valid`
	// SVar body evaluate through the same shared gate.)
	for k := range sa.Params {
		if !strings.HasPrefix(k, "Condition") || k == "ConditionDescription" {
			continue
		}
		switch k {
		case "ConditionDefined", "ConditionPresent", "ConditionNotPresent", "ConditionCompare",
			"ConditionCheckSVar", "ConditionSVarCompare", "Condition",
			"ConditionPlayerTurn", "ConditionPhases", "ConditionFirstCombat", "ConditionActivationLimit":
		default:
			return false, false
		}
	}
	// The player-turn / phase preconditions (ConditionPlayerTurn$ True|False,
	// ConditionPhases$ <phase-list>): the Unbreakable Formation Addendum
	// family and the conditional enters-tapped lands. ConditionPlayerTurn$
	// compares g.Active with the resolving controller, case-insensitively
	// True/False — Eddymurk Crab's `False` ("enters tapped if it's not your
	// turn") is a real shape, not a negation-by-absence. ConditionPhases$
	// parses through the ONE shared phase-name parser (state.ParsePhases —
	// the same parser Mode$ Phase triggers use) and requires the game's
	// current step to be in the named set; unknown names or an empty
	// resolved set are unsupported, fail-open per this file's convention.
	// Both are preconditions AND-ed with whatever group gate the SA also
	// carries (combine below); a value this gate cannot read leaves the
	// whole shape unsupported so the sub runs unconditionally, exactly as
	// before these keys existed.
	g := h.Game()
	extraMet := true
	if playerTurn != "" {
		switch strings.ToLower(playerTurn) {
		case "true":
			extraMet = g.Active == c.Controller
		case "false":
			extraMet = g.Active != c.Controller
		default:
			return false, false
		}
	}
	if phases != "" {
		set, unknown := state.ParsePhases(phases)
		if len(unknown) > 0 || set == 0 {
			return false, false
		}
		if !set.Has(g.Step) {
			extraMet = false
		}
	}
	// ConditionFirstCombat$ (the DB$ AddPhase gate: Raiyuu, Storm's Edge and
	// A-Raiyuu's "if it's the first combat phase of the turn" -- 3 corpus
	// lines, the gate that keeps an extra combat from granting another one):
	// met when the current combat is the turn's FIRST, read off the folded
	// per-turn combat count (state.Game.CombatsThisTurn, one increment per
	// BeginCombat entry). Only "True" is a corpus shape; anything else stays
	// unsupported (the fail-open run-anyway this file's convention).
	if firstCombat != "" {
		if !strings.EqualFold(firstCombat, "True") {
			return false, false
		}
		extraMet = extraMet && g.CombatsThisTurn == 1
	}
	// combine AND-s the group gate's answer with the player-turn/phase
	// preconditions above: a resolved group gate that says run still stays
	// skipped when a phase/turn precondition says no, and an unresolved
	// group gate keeps the whole shape fail-open.
	combine := func(met, resolved bool) (bool, bool) {
		if resolved && !extraMet {
			return false, true
		}
		return met, resolved
	}
	// The SVar gate (ConditionCheckSVar$ + optional ConditionSVarCompare$,
	// Forge's dominant pair at 544 corpus SAs): Vampire Lacerator's upkeep
	// trigger, Braids's per-opponent lose-life/draw, Electrostatic Bolt's
	// artifact-creature modal split, Victimize's "If you do", Infernal
	// Tutor's hellbent legs. Enforced only when the SVar gate is the SA's
	// ONLY condition — a CheckSVar beside a Present/Defined/Compare group
	// (~18 corpus SAs) has no single evaluator and stays unsupported, the
	// same fail-open run-anyway the other unsupported shapes take.
	if check != "" {
		if defined != "" || present != "" || notPresent != "" || compare != "" || bare != "" ||
			playerTurn != "" || phases != "" || firstCombat != "" {
			return false, false
		}
		holds, evaluated := CheckSVarHolds(h, c, check, svarCmp)
		if !evaluated {
			// The gate's count body is not one the evaluator models: fail
			// OPEN, the same run-anyway the other unsupported Condition*
			// shapes take (the doc above). Enforcing a zero would silence
			// cards whose gates name counts this build cannot compute
			// (Sephiroth's Count$ResolvedThisTurn transform, 52 corpus
			// sites' worth of families).
			return false, false
		}
		return holds, true
	}
	// A bare Condition$ is the cast-option family: Kicked and Foretold are
	// evaluated over the source's CastFlags (the bit the Kicker payment / the
	// Foretell action's CastInfo recorded -- the same provenance Count$
	// Foretold reads); Revolt is CR 702.38's leave-the-battlefield state
	// through Host.RevoltHolds; Blessing is CR 702.131's city's-blessing
	// latch (state.Player.Blessing); Delirium is the controller's graveyard
	// holding four or more distinct core card types, through
	// Host.DeliriumHolds (the same census the "Delirium —" activation,
	// continuous and replacement gates read, so the spellings cannot drift);
	// OptionalCost/Bargain/Threshold/Hellbent/Surge stay
	// unresolved and run unconditionally. A bare Condition beside a group key or beside
	// ConditionSVarCompare$ is a mixed shape no single evaluator covers (~11
	// corpus SAs).
	if bare != "" {
		if defined != "" || present != "" || notPresent != "" || compare != "" || svarCmp != "" ||
			playerTurn != "" || phases != "" || firstCombat != "" {
			return false, false
		}
		switch {
		case strings.EqualFold(bare, "Kicked"):
			o := h.Game().Obj(c.Source)
			return o != nil && o.CastFlags&state.FlagKicked != 0, true
		case strings.EqualFold(bare, "Foretold"):
			// CR 702.126: the "if this spell was foretold" gate (Poison the
			// Cup's conditional scry, Alrund's Epiphany's conditional tokens) --
			// the same FlagForetold provenance Count$Foretold reads, the two
			// bare-Condition corpus carriers are exactly this shape.
			o := h.Game().Obj(c.Source)
			return o != nil && o.CastFlags&state.FlagForetold != 0, true
		case strings.EqualFold(bare, "Revolt"):
			// CR 702.38's ability-word gate (Decommission's DB$ GainLife |
			// Condition$ Revolt -- the corpus's only bare-Condition$ Revolt
			// line): a permanent the resolving controller controlled left
			// the battlefield this turn, through the Host predicate the
			// rules-side Revolt$ clauses and the Count$Revolt branch head
			// share, so the spellings cannot drift apart.
			return h.RevoltHolds(c.Controller), true
		case strings.EqualFold(bare, "Delirium"):
			// The Delirium ability word: four or more distinct core card types
			// among cards in the resolving controller's graveyard
			// (Descend upon the Sinful's DB$ Token is the deck card that
			// needed it; drag_to_the_roots-style Continuous statics and the
			// activation gates read the same census rules-side). An
			// out-of-range controller denies -- a graveyard this build cannot
			// name cannot hold four types.
			return h.DeliriumHolds(c.Controller), true
		case strings.EqualFold(bare, "Metalcraft"):
			// Share the rules-side artifact census used by static and activation gates.
			return h.MetalcraftHolds(c.Controller), true
		case strings.EqualFold(bare, "Blessing"):
			// CR 702.131: the city's blessing (Ascend), read off the one-way
			// latch state.Player.Blessing that events.Apply's BlessingChange
			// fold writes (rules/ascend.go grants it). This is what makes
			// ocelot_pride's DB$ CopyPermanent and the_golden_city_of_orazca's
			// DB$ Draw condition-gated instead of run-anyway. An out-of-range
			// controller denies -- the fail-closed direction a blessing gate
			// that cannot name its seat must take.
			if int(c.Controller) >= len(g.Players) {
				return false, true
			}
			return g.Players[c.Controller].Blessing, true
		}
		return false, false
	}
	if notPresent != "" {
		// ConditionNotPresent$ (8 corpus lines, two shapes): met when NO object
		// matching the spec is present in the gate's group. With a
		// ConditionDefined$ group (Ajani Sleeper Agent, Ravenous Gigamole,
		// Fallaji Archeologist: ConditionDefined$ Remembered | NotPresent$ Card)
		// the group is that remembered list; without one the group is the
		// battlefield, the escape shape (Kroxa/Uro/Phlage's TrigSac spec
		// Card.Self+escaped: the entered permanent matches exactly when it
		// did NOT escape, so "sacrifice it unless it escaped" runs only for a
		// non-escape entry). A second present/compare key beside NotPresent is
		// not a corpus shape; a defined group this build cannot enumerate
		// (Targeted, TriggeredCardLKICopy) or an unknown predicate in the spec
		// stays unresolved and runs the sub unconditionally, the documented
		// pre-condition-engine behaviour.
		if present != "" || compare != "" {
			return false, false
		}
		return combine(conditionNotPresentMet(h, c, defined, notPresent))
	}
	if defined == "" {
		// ConditionPresent$ with NO ConditionDefined$: Forge's default group
		// is the battlefield (the conditional enters-tapped land family —
		// fast lands' Land.YouCtrl GT2, check lands' Plains.YouCtrl,Island.
		// YouCtrl EQ0). This is the shape task fb-9d2338cc resolves; it was
		// previously removed (round 2) for changing unrelated cards through
		// the global Resolve hook, and re-authorizing it is this task's
		// scope. The battlefield scan, including the entering-object
		// exclusion, lives in conditionMetBattlefield.
		//
		// A BARE ConditionCompare$ (no Present, no Defined) names no count
		// group here, so it stays unresolved.
		if present == "" {
			if compare == "" {
				// No group key named anything and the player-turn/phase gates
				// above resolved: met is exactly their conjunction (the
				// Eddymurk Crab shape — a lone ConditionPlayerTurn$ gate).
				return extraMet, true
			}
			return false, false
		}
		return combine(conditionMetBattlefield(h, c, present, compare))
	}
	if defined != "Remembered" && defined != "Self" && defined != "TriggeredCard" &&
		defined != "Imprinted" && defined != "Discarded" && defined != "Targeted" &&
		defined != "Returned" && defined != "ChosenCard" && defined != "TriggeredSourceLKICopy" &&
		defined != "RememberedLKI" &&
		defined != "TriggeredSpellAbility" {
		// Only the Remembered, Self, TriggeredCard, Imprinted, Targeted,
		// Discarded, Returned and ChosenCard families are in scope among DEFINED groups:
		// the objects a walk
		// carries in Ctx.Remembered, the resolving source object alone (the
		// Addendum shape: ConditionDefined$ Self | ConditionPresent$
		// Card.wasCast holds only when the sub is reached through a cast of
		// the source, which effects.Resolve's walk evaluates while the spell
		// is still on the stack), the card the triggering event moved (task
		// castprov2, Amped Raptor's `ConditionDefined$ TriggeredCard |
		// ConditionPresent$ Card.wasCastFromYourHandByYou`: the exile-until
		// runs only when the entering permanent was cast from its
		// controller's hand), the source card's persistent imprint list
		// (shape 5 above — Rashmi and Ragavan's `ConditionDefined$ Imprinted
		// | ConditionPresent$ Card | ConditionCompare$ EQ0`: the MayPlay
		// static registers only when the Play did NOT cast the card, the "if
		// you don't cast it this way" branch), and the resolving ability's
		// own chosen targets (Stalking Leonin's `ConditionDefined$ Targeted |
		// ConditionPresent$ Card.ChosenCtrl`: the exile runs only when the
		// targeted attacker is controlled by the secretly chosen player).
		// The LKI-copy variants and the rest need Ctx state this gate does not
		// model (and whose fail-closed skip would change unrelated cards).
		return false, false
	}
	sc := c.SpecContext(c.Controller)
	count := 0
	group := rememberedWithSource(h, c)
	if defined == "RememberedLKI" {
		// ConditionDefined$ RememberedLKI (Nurturing Pixie's
		// `ConditionDefined$ RememberedLKI | ConditionPresent$ Card.Permanent`
		// gate on the put-counter sub): the LKI spelling of the Remembered
		// group. It must enumerate the SAME capture-excluding set the
		// Defined$ RememberedLKI resolver and the count path's refTargets
		// use (rememberedLKIGroup, effects/count.go) -- a default
		// rememberedWithSource group here would read the trigger's fire-time
		// capture (the triggering permanent itself) as memory and satisfy the
		// gate when the resolution remembered nothing. This value was
		// previously absent from the supported set, so the whole gate was
		// UNRESOLVED and every such sub ran unconditionally; routing it
		// through the one shared helper is what fixes the reported card.
		group = rememberedLKIGroup(h, c)
	}
	if defined == "ChosenCard" {
		// Use the same in-flight or event-backed binding as Defined$ ChosenCard.
		// An absent binding/source stays unresolved (and therefore fail-open);
		// an explicitly answered empty choice is a known zero.
		group = ChosenTargets(g, c)
		chosenBound := c.ChosenValid || len(c.Chosen) > 0
		if !chosenBound {
			if o := g.Obj(c.Source); o != nil && (o.Chosen != nil || len(o.Chosen) > 0) {
				chosenBound = true
			}
		}
		if !chosenBound {
			return false, false
		}
	}
	if defined == "Targeted" {
		// ConditionDefined$ Targeted is the resolving ability's OWN answered
		// targets: Forge's `Targeted` defined group. It reads the same two
		// channels Defined$ Targeted does (effects/context.go — the generic
		// pre-ask's Ctx.PickedTargets while a pre-asked body dispatches, else
		// the resolution-level Ctx.Targets), so the gate and the effects' own
		// Defined$ Targeted can never disagree about which targets the
		// ability chose. Both empty is a resolved zero (the ability really
		// chose nothing), not the fail-open an absent binding gets elsewhere:
		// a Target-bearing ability always has a definite target list, and a
		// gate over an empty list genuinely denies.
		group = targetedGroup(c)
	}
	if defined == "Discarded" {
		// ConditionDefined$ Discarded (task mordorparams1: Moria Scavenger's
		// "If the discarded card was a creature card, amass Orcs 1",
		// Argentum Masticore's "When you discard a card this way, destroy
		// ..."): Forge's group is the cards the resolving chain discarded.
		// Two provenance channels enumerate it: the unless-payment's settled
		// Discard<...> component (Ctx.UnlessDiscarded, set by rules'
		// unless_pay resume arm — the mid-resolution channel), and the
		// resolving object's own activation cost discards read off the log
		// (Host.DiscardedInWindow — Moria's channel; the cost discard is
		// emitted at activation, the sub runs at resolution). Both channels
		// empty leaves the gate UNRESOLVED — the sub runs unconditionally,
		// this file's documented fail-open — never a resolved-false that
		// would silently stop subs that ran before this read existed.
		var grpOK bool
		group, grpOK = discardedGroup(h, c)
		if !grpOK {
			return false, false
		}
	}
	if defined == "Returned" {
		// ConditionDefined$ Returned (Wonderscape Sage's `ConditionDefined$
		// Returned | ConditionPresent$ Land.hasANonBasicLandType |
		// ConditionCompare$ EQ0`, the corpus's one carrier): Forge's group is
		// the permanents THIS activation's own Return<N/Spec> cost returned
		// to their owner's hand. It is enumerated off the event log through
		// the same activation window DiscardedInWindow scans (effects.Host's
		// ReturnedInWindow), so the cost payment and the gate cannot disagree.
		// An empty window is a resolved zero -- the ability really returned
		// nothing -- never the fail-open a missing channel gets elsewhere, so
		// the EQ0 comparison binds against a definite count.
		group = returnedGroup(h, c)
	}
	if defined == "Self" {
		// Self is the source object ALONE — not rememberedWithSource's
		// Source-union with the walk's remembered set.
		group = []state.Target{{Obj: c.Source}}
	}
	if defined == "Imprinted" {
		// The source card's persistent imprint list (state.Object.Imprinted,
		// the events.Imprint associations): the ONLY group for this family —
		// never the remembered set. A source-less context is the one
		// unresolved shape; a real source with an empty list is a resolved
		// zero (Rashmi's EQ0 arm), never fail-open.
		if c.Source == 0 {
			return false, false
		}
		group = nil
		if o := g.Obj(c.Source); o != nil {
			for _, id := range o.Imprinted {
				group = append(group, state.Target{Obj: id})
			}
		}
	}
	if defined == "TriggeredSourceLKICopy" {
		if c.TriggerSource == 0 || g.Obj(c.TriggerSource) == nil {
			return false, false
		}
		group = []state.Target{{Obj: c.TriggerSource}}
	}
	if defined == "TriggeredCard" {
		// The card the triggering event moved — the TriggerContext.TriggerCard
		// role rules' triggerReferents captures for every mode that names one
		// (ChangesZone, SpellCast, Drawn, ...). An ABSENT binding (a synthetic
		// fixture, a hand-built context, a mode with no card role) leaves the
		// gate UNSUPPORTED — the sub runs unconditionally, this file's
		// documented convention — never a resolved-false, which would silently
		// stop subs that ran before the group was enumerable. Measured corpus
		// population of `ConditionDefined$ TriggeredCard` gates: 33 raw lines
		// over 36 files, every one of which ran its sub unconditionally before.
		if c.TriggerCard == 0 {
			return false, false
		}
		group = []state.Target{{Obj: c.TriggerCard}}
	}
	if defined == "TriggeredSpellAbility" {
		// The trigger's spell/ability referent (Shiko and Narset, Unified and
		// Orvar, the All-Form; 2 corpus carriers) -- the SAME binding the
		// Defined$ TriggeredSpellAbility resolver reads (effects/context.go):
		// the fire-time TriggerAbility role for an AbilityCast trigger (an
		// AbilityPush's Obj is the source permanent, so Remembered alone names
		// a non-stack object), else the first object entry in Remembered (the
		// SpellCast arm, where Remembered IS the cast spell). An ABSENT binding
		// (a synthetic fixture, a hand-built context, a non-trigger context)
		// leaves the gate UNSUPPORTED -- the sub runs unconditionally, this
		// file's documented fail-open convention -- never a resolved false that
		// would stop subs that ran before this group was enumerable.
		ref := c.TriggerAbility
		if ref == 0 {
			for _, t := range c.Remembered {
				if !t.IsPlayer && t.Obj != 0 {
					ref = t.Obj
					break
				}
			}
		}
		if ref == 0 {
			return false, false
		}
		group = []state.Target{{Obj: ref}}
	}
	// The wasCastFromYourHandByYou / !wasCastFromYourHandByYou qualifier
	// (task castprov2, Amped Raptor's gate) and its bare wasCastFromYourHand
	// sibling (task castprov3, Otterball Antics' `Card.wasCast+!
	// wasCastFromYourHand`) are not filter predicates: they are evaluated
	// per member against the Host's log reads (castFromHandAdmitsFilter /
	// castFromHandAnyAdmitsFilter), the same split rules' castProvenanceAdmits
	// applies at the rules-side match sites. The UnknownPredicates guard
	// below reads the token-STRIPPED spec — the tokens themselves are unknown
	// to the filter (that is the whole reason for the split), and an
	// unreadable remainder must still be unresolved.
	hasHandToken := strings.Contains(present, "wasCastFromYourHandByYou")
	// The bare spelling is a SUBSTRING of the ByYou token, so a ByYou spec
	// must not route to the bare helper — the ByYou branch owns it.
	hasBareHand := !hasHandToken && strings.Contains(present, "wasCastFromYourHand")
	// The card-level CastSa flag tokens (task mayhem: Sandman's
	// Quicksand's "if this spell's mayhem cost was paid" split; task
	// mayplay-warp: Full Bore's Card.CastSa Spell.Warp) are evaluated per
	// member off the cast's CastFlags provenance, the same split the hand
	// families take. The spend spellings of the CastSa family stay
	// fail-closed here (no strip; wordMatches never matches them) — the
	// documented convention above.
	hasCastSaFlagTok := hasCastSaFlag(present)
	// The `Spell.IsTargeting <target-spec>` present form (Shiko and Narset's
	// and Orvar, the All-Form's `ConditionDefined$ TriggeredSpellAbility`
	// guards) is a property of the triggering spell's own TARGET LIST, not of
	// its printed face. The shared matcher now knows the predicate at the
	// alternative level (spellIsTargetingAlt, evaluated by matchesObjectText /
	// matchesZoneSpecText / compileSpec through spellIsTargetingMatches), but
	// the ordinary path would still apply the Spell BASE first -- and the
	// base reads the candidate's zone (state.ZStack), which a triggering spell
	// that has already resolved no longer satisfies. So the shape is still
	// routed here, bypassing the base, when -- and only when -- every
	// alternative is exactly this shape and the shared grammar answers its
	// argument whole; every other group and shape keeps the ordinary matcher.
	spellTargeting := defined == "TriggeredSpellAbility" && isSpellTargetingPresent(present)
	if present != "" {
		check := present
		if hasHandToken || hasBareHand {
			check = stripWasCastFromHandToken(present)
		}
		if !spellTargeting && len(UnknownPredicates(check)) > 0 {
			return false, false
		}
	}
	for _, t := range group {
		if t.IsPlayer {
			// A player entry is counted against the PLAYER-side grammar, never
			// the object matcher. Synth Eradicator's DBPlay gate
			// (`ConditionDefined$ Remembered | ConditionPresent$ Player |
			// ConditionCompare$ EQ0`) asks "did the optional put NOT happen",
			// and Play with Fire / Sonic Shrieker's GE1 ask the opposite, so a
			// remembered player must be visible to the count. An object-typed
			// base (Card, Creature, ...) never matches a player entry: skip
			// rather than hand MatchesObjectCtx an object-less target. The
			// player read goes through MatchesPlayerSpecCtx -- the ONE player
			// matcher this build ships -- with the source bound so
			// IsRemembered/Chosen clauses resolve, failing closed on an
			// unreadable player qualifier per member. An object-spec member
			// with an unknown predicate is already unresolved above.
			if present != "" && MatchesPlayerSpecCtx(g, present, t.Player, c.Controller,
				PlayerSpecCtx{Source: c.Source}) {
				count++
			}
			continue
		}
		o := g.Obj(t.Obj)
		if o == nil {
			continue
		}
		// CR 608.2b/h look-back: a target that has left the battlefield since
		// targeting is read with the counters it had there, so Dismantle's
		// `ConditionDefined$ Targeted | ConditionPresent$ Card.HasCounters`
		// still holds after the chained Destroy moved the target to the
		// graveyard. Only the counter field is substituted; every other
		// characteristic is read live (or via Ctx.LKI for a trigger).
		if defined == "Targeted" {
			if cs, ok := targetCountersLKI(c, t.Obj, o); ok {
				oc := *o
				oc.Counters = cs
				o = &oc
			}
		}
		if present == "" {
			count++
			continue
		}
		memberSpec := present
		if hasHandToken {
			s, ok := castFromHandAdmitsFilter(h, present, t.Obj, c.Controller)
			if !ok {
				// This member fails its own provenance requirement.
				continue
			}
			memberSpec = s
		} else if hasBareHand {
			s, ok := castFromHandAnyAdmitsFilter(h, present, t.Obj)
			if !ok {
				continue
			}
			memberSpec = s
		}
		if hasCastSaFlagTok {
			s, ok := castSaAdmitsFilter(h, memberSpec, t.Obj)
			if !ok {
				continue
			}
			memberSpec = s
		}
		if spellTargeting {
			// The present spec is a target filter over the member spell's
			// TARGETS, not a filter over the member object's own face.
			if spellTargetingMatches(g, memberSpec, o, sc) {
				count++
			}
			continue
		}
		if MatchesObjectCtx(g, memberSpec, o, sc) {
			count++
		}
	}
	return combine(evalConditionCount(count, compare))
}

// returnedGroup enumerates the ConditionDefined$ Returned group: the
// permanents the resolving object's OWN activation returned to their owner's
// hand as a Return<N/Spec> cost, over the activation window
// (effects.Host.ReturnedInWindow) the Discarded group's cost channel uses.
// An empty window is a resolved empty list, not an unresolved gate: an
// activation that returned nothing genuinely has no returned permanents, so
// a count comparison over it is definite.
func returnedGroup(h Host, c *Ctx) []state.Target {
	var out []state.Target
	for _, id := range h.ReturnedInWindow(c.ResolvingObj) {
		out = append(out, state.Target{Obj: id})
	}
	return out
}

// discardedGroup enumerates the ConditionDefined$ Discarded group: the
// cards the resolving chain discarded, over the two provenance channels
// conditionMet's Discarded branch documents. The log-scan channel is scoped
// to c.ResolvingObj — the wrapper whose resolution is walking — so another
// activation's cost discard cannot bleed in; a wrapper-less context (a
// hand-built one) has no window to scan. Both channels empty is UNRESOLVED
// (false), never a resolved-empty: the fail-open convention must not turn
// into a resolved gate just because no channel carried evidence.
func discardedGroup(h Host, c *Ctx) ([]state.Target, bool) {
	var out []state.Target
	seen := make(map[state.ObjID]bool, len(c.UnlessDiscarded)+4)
	add := func(id state.ObjID) {
		if id != 0 && !seen[id] {
			seen[id] = true
			out = append(out, state.Target{Obj: id})
		}
	}
	for _, t := range c.UnlessDiscarded {
		add(t.Obj)
	}
	if c.ResolvingObj != 0 {
		for _, id := range h.DiscardedInWindow(c.ResolvingObj) {
			add(id)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// conditionMetBattlefield resolves a ConditionPresent$ (with an optional
// ConditionCompare$ but NO ConditionDefined$) against Forge's default group
// for that shape: every permanent on the battlefield. Task fb-9d2338cc (the
// conditional enters-tapped land family — Blooming Marsh and the fast / check
// lands) is what needs it.
//
// The count EXCLUDES the entering object (c.Replaced). For the
// ReplacementResult$ Updated shape rules/replacement.go emits the original
// MoveZone (the land enters the battlefield) BEFORE resolving the With, so
// when conditionMet runs here the entering land is already a battlefield
// permanent; Forge evaluates the gate before entry, so Land.YouCtrl there
// means "other lands". Without the exclusion a naive count would include the
// land itself and fire the fast lands' GT2 one land early (tapped at 2 other
// lands instead of 3). c.Replaced names exactly that moved object (and is 0
// outside a replacement, where nothing is excluded). c.Source is deliberately
// NOT excluded: it is the permanent owning the replacement, which for the
// shared "other permanents enter tapped" shapes (Blind Obedience) is a DIFFERENT
// object that the count must still see.
//
// An unreadable spec (UnknownPredicates) is unresolved, counted once before
// the object loop, mirroring the Remembered path: an empty battlefield with
// an unknown spec must be unresolved, not a resolved "count 0" that silently
// stops subs that used to run.
func conditionMetBattlefield(h Host, c *Ctx, present, compare string) (met, resolved bool) {
	if len(UnknownPredicates(present)) > 0 {
		return false, false
	}
	g := h.Game()
	sc := c.SpecContext(c.Controller)
	count := 0
	for i := range g.Objs {
		o := &g.Objs[i]
		if o.Zone != state.ZBattlefield || o.Face() == nil {
			continue
		}
		if c.Replaced != 0 && o.ID == c.Replaced {
			// The entering object is already a battlefield permanent (see the
			// doc above): it is not an "other" permanent and must not count.
			continue
		}
		if MatchesObjectCtx(g, present, o, sc) {
			count++
		}
	}
	return evalConditionCount(count, compare)
}

// isSpellTargetingPresent reports whether spec is exactly Forge's
// `Spell.IsTargeting <target-spec>` present filter (or the SpellAbility
// spelling), possibly an OR list of those forms. It is deliberately strict:
// a `+`-joined predicate, an unknown base, an empty argument or an argument
// the shared grammar cannot answer whole (a malformed ValidX, an unknown
// inner predicate -- spellIsTargetingArgRecognised) leaves the spec to the
// ordinary matcher and its UnknownPredicates census, so this gate can never
// claim a shape it does not fully answer. The evaluation itself is the
// shared matcher's (spellIsTargetingMatches via spellTargetingMatches); this
// function only decides whether the ordinary object matcher's BASE check
// (Spell reads the candidate's zone) must be bypassed in favour of the
// spell's recorded targets.
func isSpellTargetingPresent(spec string) bool {
	if spec == "" {
		return false
	}
	for alt := range filterAlternatives(spec) {
		base, rest, ok := strings.Cut(strings.TrimSpace(alt), ".")
		if !ok || (base != "Spell" && base != "SpellAbility") {
			return false
		}
		arg, ok := spellIsTargetingArg(rest)
		if !ok || !spellIsTargetingArgRecognised(arg) {
			return false
		}
	}
	return true
}

// spellTargetingMatches evaluates Forge's `Spell.IsTargeting <target-spec>`
// against a member spell: met when ANY of the spell's recorded targets
// matches the spec, OR over the spec's comma alternatives. The argument
// conventions (`Valid ` strip, `~Other` rewrite) and the target walk are the
// shared matcher's own (spellIsTargetingInner / spellIsTargetingMatches in
// filter.go) -- this wrapper only splits the alternatives and cuts the
// Spell./SpellAbility. base the alternative grammar carries, so the condition
// gate and every other filter caller cannot drift apart. The strictness of
// isSpellTargetingPresent means the argument always normalises here; the
// continues are unreachable for a spec the gate admitted and stay as the
// fail-closed backstop.
func spellTargetingMatches(g *state.Game, spec string, o *state.Object, sc SpecContext) bool {
	for alt := range filterAlternatives(spec) {
		_, rest, ok := strings.Cut(strings.TrimSpace(alt), ".")
		if !ok {
			continue
		}
		arg, ok := spellIsTargetingArg(rest)
		if !ok {
			continue
		}
		inner, ok := spellIsTargetingInner(arg)
		if !ok {
			continue
		}
		if spellIsTargetingMatches(g, inner, o, sc) {
			return true
		}
	}
	return false
}

// conditionNotPresentMet is the ConditionNotPresent$ evaluator the two
// supported group shapes share (see conditionMet's notPresent branch for the
// shape census). The spec is matched with MatchesObjectCtx -- the same object
// grammar ValidCard$ uses -- so the escape family's Card.Self+escaped reads
// the CastFlags FlagEscaped provenance the escape cast recorded.
func conditionNotPresentMet(h Host, c *Ctx, defined, spec string) (met, resolved bool) {
	if len(UnknownPredicates(spec)) > 0 {
		return false, false
	}
	g := h.Game()
	sc := c.SpecContext(c.Controller)
	count := 0
	switch defined {
	case "":
		// Forge's default group for a group-less ConditionPresent$/NotPresent$
		// gate is the battlefield (conditionMetBattlefield's group).
		for i := range g.Objs {
			o := &g.Objs[i]
			if o.Zone != state.ZBattlefield || o.Face() == nil {
				continue
			}
			if MatchesObjectCtx(g, spec, o, sc) {
				count++
			}
		}
	case "Remembered":
		for _, t := range rememberedWithSource(h, c) {
			if t.IsPlayer {
				continue
			}
			o := g.Obj(t.Obj)
			if o == nil {
				continue
			}
			if MatchesObjectCtx(g, spec, o, sc) {
				count++
			}
		}
	case "Targeted":
		// The resolving ability's own answered targets — the same group
		// conditionMet's Targeted branch enumerates.
		for _, t := range targetedGroup(c) {
			if t.IsPlayer {
				continue
			}
			o := g.Obj(t.Obj)
			if o == nil {
				continue
			}
			if MatchesObjectCtx(g, spec, o, sc) {
				count++
			}
		}
	default:
		// A defined group this build cannot enumerate (TriggeredCardLKICopy):
		// unresolved, the sub runs unconditionally.
		return false, false
	}
	return count == 0, true
}

// targetedGroup is the ConditionDefined$ Targeted group: the resolving
// ability's own answered targets, over the same two channels Defined$
// Targeted reads (effects/context.go) — Ctx.PickedTargets while a pre-asked
// body dispatches, else the resolution-level Ctx.Targets.
func targetedGroup(c *Ctx) []state.Target {
	if c.PickedTargets != nil {
		return c.PickedTargets
	}
	return c.Targets
}

// evalConditionCount turns a counted group into (met, resolved) from the
// ConditionCompare$ operator, sharing the comparison logic between the
// Remembered and battlefield groups. No Compare key means presence (>= 1),
// Forge's default; a non-literal right-hand side (GTX/EQY — 5 corpus lines)
// needs the SVar resolver this gate does not carry and stays unresolved.
func evalConditionCount(count int, compare string) (met, resolved bool) {
	if compare == "" {
		return count >= 1, true
	}
	op, n, ok := parseConditionCompare(compare)
	if !ok {
		return false, false
	}
	switch op {
	case "EQ":
		return count == n, true
	case "NE":
		return count != n, true
	case "LT":
		return count < n, true
	case "LE":
		return count <= n, true
	case "GT":
		return count > n, true
	case "GE":
		return count >= n, true
	}
	return false, false
}

// parseConditionCompare parses Forge's ConditionCompare$ value: a
// two-letter operator immediately followed by an integer, with or without
// an intervening space ("EQ1" — the corpus spelling, 793 lines — or
// "EQ 1"). Unknown operators or non-literal right-hand sides fail.
func parseConditionCompare(v string) (op string, n int, ok bool) {
	if len(v) < 3 {
		return "", 0, false
	}
	op = strings.ToUpper(v[:2])
	switch op {
	case "EQ", "NE", "LT", "LE", "GT", "GE":
	default:
		return "", 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(v[2:]))
	if err != nil {
		return "", 0, false
	}
	return op, n, true
}

// admitProvenanceAlternativesFilter is the effects-side rejoin loop shared
// by the two cast-provenance filter helpers (rules' twin,
// admitProvenanceAlternatives, lives in rules/cast_provenance.go): the spec
// is split into its comma alternatives, every alternative CARRYING the
// qualifier but failing the provenance test is dropped, and the surviving
// alternatives are rejoined for the ordinary filter. ok is false when no
// alternative survives: the spec matches nothing. A spec without the token
// is returned unchanged, so every unrelated gate is byte-identical.
func admitProvenanceAlternativesFilter(spec, pred string, holds bool) (string, bool) {
	if !strings.Contains(spec, pred) {
		return spec, true
	}
	var b strings.Builder
	first, alive := true, false
	for alt := range FilterAlternatives(spec) {
		s1, hadPos := StripPredicateToken(alt, pred)
		s2, hadNeg := StripPredicateToken(s1, "!"+pred)
		// The positive spelling requires the provenance to HOLD; the negated
		// spelling requires it to FAIL.
		if (hadPos && !holds) || (hadNeg && holds) {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		b.WriteString(s2)
		first = false
		alive = true
	}
	if !alive {
		return "", false
	}
	return b.String(), true
}

// castFromHandAdmitsFilter evaluates the bare wasCastFromYourHandByYou /
// !wasCastFromYourHandByYou qualifier of a Forge filter spec against ONE
// object through the Host's log read (task castprov2, Amped Raptor's
// `ConditionPresent$ Card.wasCastFromYourHandByYou` gate — the effects-side
// twin of rules' castFromHandAdmits, which runs the same split at the
// rules-side match sites where the Engine and its log are in scope). The
// object was NOT cast from you's hand by you, or the object is a copy (never
// cast, the same IsCopy guard the Count$wasCastFromYourHandByYou head
// takes) — every alternative carrying the qualifier but failing the
// provenance test is dropped. ok is false when no alternative survives: the
// spec matches nothing (this member fails its own provenance requirement).
func castFromHandAdmitsFilter(h Host, spec string, objID state.ObjID, you state.PlayerID) (string, bool) {
	if !strings.Contains(spec, "wasCastFromYourHandByYou") {
		return spec, true
	}
	holds := false
	if o := h.Game().Obj(objID); o != nil && !o.IsCopy {
		holds = h.WasCastFromHandByYou(objID, you)
	}
	return admitProvenanceAlternativesFilter(spec, "wasCastFromYourHandByYou", holds)
}

// castFromHandAnyAdmitsFilter evaluates the BARE wasCastFromYourHand /
// !wasCastFromYourHand qualifier (task castprov3 — the player-less hand
// provenance: the "from anywhere other than your hand" carriers whose
// scripts omit the ByYou suffix, Otterball Antics' `ConditionPresent$
// Card.wasCast+!wasCastFromYourHand`) against ONE object through the Host's
// log read — the effects-side twin of rules' castFromHandAnyAdmits. The
// provenance is any caster's hand: every carrier that needs player scoping
// supplies it elsewhere in the spec. Copies were never cast; a card never
// put on the stack reads false. A spec carrying the ByYou spelling is NOT
// this helper's family (ByYou is a SUPERSTRING of the bare token; its own
// helper runs first wherever both could appear) and is returned unchanged.
func castFromHandAnyAdmitsFilter(h Host, spec string, objID state.ObjID) (string, bool) {
	if strings.Contains(spec, "wasCastFromYourHandByYou") || !strings.Contains(spec, "wasCastFromYourHand") {
		return spec, true
	}
	holds := false
	if o := h.Game().Obj(objID); o != nil && !o.IsCopy {
		holds = h.WasCastFromHand(objID)
	}
	return admitProvenanceAlternativesFilter(spec, "wasCastFromYourHand", holds)
}

// castSaFlagTokens is the effects-side list of the CastSa flag spellings
// whose truth rides the cast's pay-time CastInfo CastFlags (state.FlagMayhem,
// state.FlagWarped). It mirrors the flag entries of rules' castSaTokens
// (effects cannot import rules); a new flag spelling must be added to BOTH
// tables, and the census recognition in filter.go's wordKind switch is the
// third site. Spell.MayPlaySource is deliberately NOT here: it is the
// sibling task mayplay-src's predicate, recorded and read rules-side only.
// The loop in castSaAdmitsFilter and the hasCastSaFlag gate in the
// ConditionPresent walk both read this one table, so the next flag spelling
// cannot be handled by one read and missed by the other.
var castSaFlagTokens = []struct {
	token string
	flag  uint64
}{
	{token: "CastSa Spell.Mayhem", flag: state.FlagMayhem},
	{token: "CastSa Spell.Warp", flag: state.FlagWarped},
}

// hasCastSaFlag reports whether spec carries ANY flag spelling, the cheap
// gate the ConditionPresent walk uses before calling castSaAdmitsFilter.
func hasCastSaFlag(spec string) bool {
	for _, t := range castSaFlagTokens {
		if strings.Contains(spec, t.token) {
			return true
		}
	}
	return false
}

// castSaAdmitsFilter evaluates the card-level CastSa flag tokens (task
// mayhem: Sandman's Quicksand's `ConditionPresent$ Card.!
// CastSa Spell.Mayhem` / `Card.CastSa Spell.Mayhem` split; task
// mayplay-warp: Full Bore's `ConditionPresent$ Card.CastSa Spell.Warp`)
// against ONE object through the Host's game read — the effects-side twin
// of rules' castSaAdmits (which runs the same strip at the rules-side match
// sites where the Engine is in scope). holds reads the object's LATEST
// cast's CastFlags: the pay-time CastInfo REPLACES the set (events.Apply's
// CastInfo case), so a re-cast object cannot inherit an older cast's flags.
// A copy was never cast (CR 707.10) and carries no provenance bit for the
// flags state.CastProvenanceFlags strips at the copy; FlagWarped is
// deliberately NOT in that set (a warp cast's alternative cost is a choice
// the copy rules carry), so a warp copy reads true — the same ruling the
// FlagWarped entry hook takes. Both spellings of each token (positive and
// !-negated) are handled; the spend spellings of the CastSa family and the
// sibling task mayplay-src's Spell.MayPlaySource stay fail-closed here, the
// documented convention.
func castSaAdmitsFilter(h Host, spec string, objID state.ObjID) (string, bool) {
	var flags uint64
	flagsRead := false
	for _, t := range castSaFlagTokens {
		if !strings.Contains(spec, t.token) {
			continue
		}
		if !flagsRead {
			if o := h.Game().Obj(objID); o != nil && !o.IsCopy {
				flags = o.CastFlags
			}
			flagsRead = true
		}
		var ok bool
		if spec, ok = admitProvenanceAlternativesFilter(spec, t.token, flags&t.flag != 0); !ok {
			return "", false
		}
	}
	return spec, true
}

// stripWasCastFromHandToken removes both spellings of the
// wasCastFromYourHandByYou qualifier AND the bare wasCastFromYourHand
// spelling (task castprov3) from every comma alternative of a spec,
// text-only and polarity-agnostic: the UnknownPredicates guard must read the
// token-STRIPPED spec (the tokens themselves are unknown to the filter —
// that is the whole reason the split exists), while the per-member polarity
// lives in castFromHandAdmitsFilter / castFromHandAnyAdmitsFilter. A spec
// without either token is returned unchanged.
func stripWasCastFromHandToken(spec string) string {
	if !strings.Contains(spec, "wasCastFromYourHand") {
		return spec
	}
	var b strings.Builder
	first := true
	for alt := range FilterAlternatives(spec) {
		s1, _ := StripPredicateToken(alt, "wasCastFromYourHandByYou")
		s2, _ := StripPredicateToken(s1, "!wasCastFromYourHandByYou")
		s3, _ := StripPredicateToken(s2, "wasCastFromYourHand")
		s4, _ := StripPredicateToken(s3, "!wasCastFromYourHand")
		if !first {
			b.WriteByte(',')
		}
		b.WriteString(s4)
		first = false
	}
	return b.String()
}

// activationCountHolds evaluates a literal "<op><n>" compare (GE4, EQ0, ...)
// against n through the shared compareCount. It reports (holds, evaluated);
// a compare it cannot read is unevaluated.
func activationCountHolds(n int32, cmp string) (bool, bool) {
	cmp = strings.TrimSpace(cmp)
	if len(cmp) < 3 {
		return false, false
	}
	t, err := strconv.Atoi(strings.TrimSpace(cmp[2:]))
	if err != nil {
		return false, false
	}
	switch op := strings.ToUpper(cmp[:2]); op {
	case "EQ", "NE", "LT", "LE", "GT", "GE":
		return compareCount(op, int(n), t), true
	}
	return false, false
}

// sourceRuntimeSVar is runtimeSVar keyed off an explicit game rather than
// c.Host, for the gate evaluators that are handed the host directly: the
// resolving source object's api:StoreSVar store entry under name.
func sourceRuntimeSVar(g *state.Game, c *Ctx, name string) (int32, bool) {
	if c == nil || c.Source == 0 || g == nil {
		return 0, false
	}
	o := g.Obj(c.Source)
	if o == nil {
		return 0, false
	}
	v, ok := o.RuntimeSVars[name]
	return v, ok
}
