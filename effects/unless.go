package effects

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// UnlessCost$ is Forge's "unless a player pays <cost>, <effect happens>"
// mid-resolution gate. Forge resolves it in AbilityUtils.handleUnlessCost:
// the UnlessPayer$ (default TargetedController) is offered the cost; the
// effect's main body resolves only when `alreadyPaid == isSwitched`, i.e.
//
//   - UnlessSwitched$ absent (the default): paying PREVENTS the effect —
//     "counter target spell unless its controller pays {1}" (Mana Leak),
//     "enters tapped unless you pay 2 life" (Hallowed Fountain), "target
//     player sacrifices a creature unless they pay {3}".
//   - UnlessSwitched$ True: paying CAUSES the effect — "may pay {2}. If
//     that player does, copy that spell" (Chain Lightning's copy clause,
//     whose script carries the flag explicitly).
//
// SubAbility$ chains resolve whether the cost was paid or not (Forge's
// UnlessResolveSubs$ default 'Always'), which is why this gate decides only
// whether the effect's own body runs: Resolve continues down sa.Sub either
// way.
//
// Before this gate existed only Counter and CopySpellAbility read the
// parameter; every other API (Sacrifice 155 raw corpus lines, Tap 56, Draw
// 43, LoseLife 42, ChangeZone 28, ...) ignored an UnlessCost$ entirely. The
// gate below runs in Resolve before EVERY effect dispatch, so one
// implementation now serves every API. Counter and CopySpellAbility keep
// only their label wording; their semantics are exactly these two
// orientations and always were.
//
// Payment happens in rules (rules' resumeResolution unless_pay arm): the ask
// is a KModes decision with ResumeKind "unless_pay", the answer re-enters
// this SA with Ctx.UnlessPay set ("pay" = the cost was charged to the payer
// by the engine's payment path, "decline" = it was not), and this gate
// applies the orientation. A cost the payment API cannot price is a hard
// decline there, never a free pass. The payer index rides the decision's
// ResumeTarget (Ask stores it on the resume point), so a multi-payer
// UnlessPayer$ asks each payer in turn until one pays.
//
// UnlessResolveSubs$ WhenPaid/WhenNotPaid (41 raw corpus lines) gates the
// SubAbility$ walk on the pay outcome (unlessSubsRun, applied in Resolve):
// 'Always' — the corpus default — runs the subs either way; WhenPaid runs
// them only when the cost was paid; WhenNotPaid only when it was not.

// UnlessCostResolved renders an SA's UnlessCost$ value as the string the
// mid-resolution pay path prices. A literal value (a mana symbol list, a
// Sac<...>/Discard<...>/... component, or an unpriceable spelling like X or
// CopyCost with no matching SVar) passes through unchanged -- rules'
// ParseUnlessCost stays the strict parser and hard-declines what it cannot
// price. A value naming an SVar on the resolving face whose body is a
// RESOLVABLE count expression folds its numeric result into one generic amount
// "{N}": Feather, Radiant Arbiter's SVar:CopyCost:Count$ChosenSize/Times.2
// becomes "{4}" for two chosen creatures. The fold applies to every
// UnlessCost$ API because the shared gate owns the payment; leaving a
// resolvable SVar opaque on Sacrifice, Tap, or another effect would make the
// ask and its payment disagree. An SVar present but unresolvable also passes
// through: the ask is still posed and recorded, but it cannot be answered
// "pay", exactly as before. Three more shapes fold here rather than at the
// parser, because only the resolution context can bind them: an announced X
// (including zero — Ctx.XAnnounced, CR 601.2b), a DefinedCost_ token's
// card-anchored mana value (Tariff, Flash, Disruption Aura) and an amount
// token (PayEnergy<N>, PayLife<X>) whose SVar body the face's table
// resolves — the token keeps its shape, only the amount folds, so an energy
// cost never turns into generic mana. The same string must reach the ask's
// label (unlessProceed) and the payment (rules' unless_pay arm calls this
// with the resumed ctx), so the offer and the charge can never disagree.
func UnlessCostResolved(h Host, c *Ctx, sa *cards.SA) string {
	if sa == nil {
		return ""
	}
	raw := strings.TrimSpace(sa.ParamStr(cards.PKUnlessCost))
	if raw == "" || c == nil {
		return raw
	}
	// CR 601.2b: an X in an UnlessCost$ is the value announced by the
	// resolving spell or ability. The rules package carries that announcement
	// through Ctx.X when it rebuilds a suspended resolution; XAnnounced marks
	// that an X cost was genuinely paid, so an announced ZERO resolves too
	// (Power Sink cast for X=0) instead of staying an unpriceable token. XX is
	// Forge's spelling for twice the same announced value (Thassa's
	// Intervention). A face whose SVar:X body is NOT Count$xPaid FIXES the
	// value instead (fixLifeXCost's reading: Mausoleum Wanderer's
	// Sacrificed$CardPower, Cephalid Shrine's graveyard count) — that binding
	// wins over any announcement, so the token falls through to the SVar fold
	// below.
	if c.X > 0 || c.XAnnounced {
		fixedX := false
		if c.SVars != nil {
			if body, ok := c.SVars["X"]; ok && !strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
				fixedX = true
			}
		}
		if !fixedX {
			switch raw {
			case "X":
				return "{" + strconv.FormatInt(int64(c.X), 10) + "}"
			case "XX":
				return "{" + strconv.FormatInt(int64(c.X)*2, 10) + "}"
			}
		}
	}
	if c.SVars != nil {
		body, ok := c.SVars[raw]
		if ok {
			n, resolved := EvalCountOK(h, c, body)
			if resolved {
				if n < 0 {
					n = 0
				}
				return "{" + strconv.Itoa(int(n)) + "}"
			}
		}
	}
	// Forge's DefinedCost_<Defined>[_<Modifier>] spelling (AbilityUtils's
	// handleDefinedCost family): the amount is a CARD's mana value — the
	// source's (Disruption Aura's "pay its mana cost"), the chosen card's
	// (Tariff's greatest-CMC creature), the remembered card's (Flash, minus
	// 2) — and DefinedSACost_TriggeredSpellAbility (Ice Cave) names the
	// triggering spell's whole mana COST, colours included.
	if m := definedCostToken.FindStringSubmatch(raw); m != nil {
		if out, ok := unlessDefinedCost(h, c, m); ok {
			return out
		}
		// An unresolvable suffix passes through: the ask is still posed and
		// recorded, but it cannot be answered "pay" — the strict parser
		// rejects the token and the payer declines.
		return raw
	}
	// Token-level folds for components whose AMOUNT is an SVar: the token
	// keeps its shape (an energy part stays an energy part; a life part stays
	// a life part) and only the amount folds. Aether Spike's
	// "UnlessCost$ Mandatory PayEnergy<N>" binds the energy to the payer's
	// chosen amount, and Wand of Ith's "UnlessCost$ PayLife<X>" binds the
	// life to the remembered card's mana value — both SVar bodies on the
	// resolving face. "PayEnergy<X>" and unresolvable amounts pass through:
	// the strict parser takes the X channel (the announced-X binding at the
	// pay sites) or the token hard-declines, never a silent zero.
	if c.SVars != nil && strings.ContainsAny(raw, "<>") {
		fields := strings.Fields(raw)
		out := make([]string, 0, len(fields))
		changed := false
		for _, f := range fields {
			if strings.EqualFold(f, "Mandatory") {
				// Forge's mandatory-payment marker (Cost$ Mandatory ...): no
				// cost, so it does not reach the price.
				changed = true
				continue
			}
			if m := payEnergyToken.FindStringSubmatch(f); m != nil {
				name := m[1]
				if name == "X" {
					// The announced-X channel, unless the face's SVar:X body
					// FIXES the value (Behemoth of Vault 0's
					// Targeted$CardManaCost): a non-Count$xPaid body is a fixed
					// amount the resolution evaluates, exactly fixLifeXCost's
					// reading of the same shape on the life side.
					body, ok := c.SVars["X"]
					if ok && !strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
						if n, resolved := EvalCountOK(h, c, body); resolved {
							if n < 0 {
								n = 0
							}
							f = "PayEnergy<" + strconv.Itoa(int(n)) + ">"
							changed = true
						}
					}
					out = append(out, f)
					continue
				}
				if !isPlainNumber(name) {
					if n, resolved := EvalCountOK(h, c, c.SVars[name]); resolved {
						if n < 0 {
							n = 0
						}
						f = "PayEnergy<" + strconv.Itoa(int(n)) + ">"
						changed = true
					}
				}
				out = append(out, f)
				continue
			}
			if m := payLifeXToken.FindStringSubmatch(f); m != nil {
				if n, resolved := EvalCountOK(h, c, c.SVars["X"]); resolved && n >= 0 {
					f = "PayLife<" + strconv.Itoa(int(n)) + ">"
					changed = true
				}
			}
			out = append(out, f)
		}
		if changed {
			return strings.Join(out, " ")
		}
	}
	return raw
}

// payEnergyToken matches the PayEnergy<...> cost token whose amount may name
// an SVar (Aether Spike's "PayEnergy<N>"); X and numerics are parsed
// elsewhere (the announced-X channel, the literal).
var payEnergyToken = regexp.MustCompile(`^PayEnergy<([^>]*)>$`)

// payLifeXToken matches the announced PayLife<X> life token (Wand of Ith,
// Essence Vortex): the amount is the SVar:X body, folded above when the
// resolution's table resolves it and at the pay sites' face route otherwise.
var payLifeXToken = regexp.MustCompile(`^PayLife<X>$`)

// definedCostToken matches Forge's DefinedCost_<Defined>[_<Modifier>] and
// DefinedSACost_<Defined> tokens; Modifier is Minus<N>/Plus<N>.
var definedCostToken = regexp.MustCompile(`^(DefinedSACost|DefinedCost)_([A-Za-z]+)(?:_(Minus|Plus)([0-9]+))?$`)

// isPlainNumber reports whether s is a non-negative decimal integer.
func isPlainNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// unlessDefinedCost renders a DefinedCost token's amount. m is
// definedCostToken's match: m[1] the head (DefinedCost or DefinedSACost),
// m[2] the defined role, m[3]/m[4] an optional Minus/Plus modifier. The
// amount of a DefinedCost_ is the named card's mana VALUE as one generic
// amount; a DefinedSACost_ renders the named spell's whole mana COST string
// (colours included — Ice Cave's "pay that spell's mana cost"). A role the
// resolution cannot bind reports ok=false and the token passes through raw.
func unlessDefinedCost(h Host, c *Ctx, m []string) (string, bool) {
	g := h.Game()
	if g == nil {
		return "", false
	}
	mv := func(o *state.Object) (string, bool) {
		if o == nil || o.Face() == nil {
			return "", false
		}
		return "{" + strconv.FormatInt(int64(o.Face().ManaValue()), 10) + "}", true
	}
	obj := func(ts []state.Target) *state.Object {
		for _, t := range ts {
			if !t.IsPlayer && t.Obj != 0 {
				if o := g.Obj(t.Obj); o != nil {
					return o
				}
			}
		}
		return nil
	}
	if m[1] == "DefinedSACost" {
		// The triggering SPELL ABILITY's mana cost (Ice Cave): the trigger's
		// own role binding is the stack-legal wrapper, the Remembered fallback
		// the cast spell — the same two bindings Defined$ TriggeredSpellAbility
		// resolves through.
		var o *state.Object
		if c.TriggerAbility != 0 {
			o = g.Obj(c.TriggerAbility)
		} else {
			o = obj(c.Remembered)
		}
		if o == nil || o.Face() == nil || strings.TrimSpace(o.Face().ManaCost) == "" {
			return "", false
		}
		return strings.Join(strings.Fields(o.Face().ManaCost), " "), true
	}
	switch m[2] {
	case "Self":
		s, ok := mv(g.Obj(c.Source))
		if !ok {
			return "", false
		}
		return applyUnlessCostModifier(s, m[3], m[4]), true
	case "ChosenCard":
		s, ok := mv(obj(resolutionChosenCards(g, c)))
		if !ok {
			return "", false
		}
		return applyUnlessCostModifier(s, m[3], m[4]), true
	case "Remembered":
		s, ok := mv(obj(c.Remembered))
		if !ok {
			return "", false
		}
		return applyUnlessCostModifier(s, m[3], m[4]), true
	}
	return "", false
}

// applyUnlessCostModifier applies a DefinedCost_ suffix's Minus<N>/Plus<N>
// modifier (Flash's DefinedCost_Remembered_Minus2, "pay its mana value minus
// 2") to a resolved {N} generic amount. A negative result clamps to zero.
func applyUnlessCostModifier(shown, op, arg string) string {
	if op == "" {
		return shown
	}
	n, err := strconv.Atoi(strings.Trim(shown, "{}"))
	if err != nil {
		return shown
	}
	d, err2 := strconv.Atoi(arg)
	if err2 != nil {
		return shown
	}
	if op == "Minus" {
		n -= d
	} else {
		n += d
	}
	if n < 0 {
		n = 0
	}
	return "{" + strconv.Itoa(n) + "}"
}

// unlessProceed reports whether the effect's body should run for this pass,
// whether the UnlessCost$ was paid, and whether its election was served
// from the resolution kernel's tape (then nothing suspended: the answer was
// settled in line and the gate re-read it, exactly as the legacy re-entry
// does, so Resolve must not read the served ask as a suspension). Called
// from Resolve immediately before the dispatch, for every SA; a zero-cost SA returns (true, false)
// with no work. On the first pass (no recorded answer) it poses the pay
// decision and reports (false, false) for the suspended pass; the answered
// re-entry applies the orientation. The paid half feeds UnlessResolveSubs$
// (Resolve gates the SubAbility$ walk on it); on every path where no cost
// was charged — a decline, an unresolvable payer, a no-host fallback — it is
// false.
func unlessProceed(h Host, c *Ctx, sa *cards.SA) (run, paid, served bool) {
	cost := UnlessCostResolved(h, c, sa)
	if strings.TrimSpace(sa.ParamStr(cards.PKUnlessCost)) == "" {
		return true, false, false
	}
	if sa.API == "Ward" {
		// effWard owns Ward's ask end to end: its payer is the CONTROLLING
		// object of the targeting spell/ability held in TriggerStack (not a
		// UnlessPayer$ selector and not the warding permanent's controller),
		// and its payment forms — the CR 702.21a mana window and the
		// non-mana ward costs — are handled by rules' unless_pay arm
		// (beginWardPayment) before the answer re-enters effWard. Gate it
		// here and the generic ask would go to the wrong player and bypass
		// those windows, so leave the shape to its own handler.
		return true, false, false
	}
	switched := strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKUnlessSwitched)), "True")
	// The answer and payer cursor are consumed (and cleared) at the top of
	// every pass — the fx42 scoping discipline: an unless SA reached below a
	// consuming SA in the same walk poses its own ask instead of inheriting
	// the outer answer. ctx.UnlessNext is the index of the payer whose
	// answer this pass applies (0 on a first pass; rules' resume arm copies
	// it off the resume point).
	ans := c.UnlessPay
	c.UnlessPay = ""
	idx := c.UnlessNext
	c.UnlessNext = 0
	// The body's own re-entry: this gate already resolved on the suspended
	// pass and recorded its outcome through Host.SuspendUnless (the asking
	// body-under-UnlessCost$ livelock fix) — consume the marker, never ask
	// again. The recorded pay outcome feeds UnlessResolveSubs$ exactly as
	// the original pass computed it.
	if ans == "resolved-pay" {
		return true, true, false
	}
	if ans == "resolved-decline" {
		return true, false, false
	}
	// A paid answer is authoritative even if a re-entry fixture or nested
	// continuation did not retain every transient payer binding from the ask.
	if ans == "pay" {
		return switched, true, false
	}
	// A DefinedTarget$ ChosenCard copy ask (Feather, Radiant Arbiter) with an
	// EMPTY chosen set is not a decision anybody could answer differently:
	// Forge's "you may choose any number of other creatures ... and pay {2}
	// for each of those creatures. If you do, for each of those creatures,
	// copy that spell" has no payment offer when the choice answered empty --
	// neither the pay election nor a zero-cost "pay" that would copy nothing.
	// Apply the not-paid orientation (switched: no copies) and let the
	// SubAbility$ chain run, exactly as a decline would.
	if sa.API == "CopySpellAbility" && strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKDefinedTarget)), "ChosenCard") {
		n := 0
		for _, t := range resolutionChosenCards(h.Game(), c) {
			if !t.IsPlayer {
				n++
			}
		}
		if n == 0 {
			return !switched, false, false
		}
	}
	payers, payerKnown := unlessPayerTargets(h, c, sa)
	// A named selector whose binding is unavailable must not silently charge
	// an unrelated target or the resolving controller. Treat it as a decline:
	// the ordinary "unless" body runs, while a switched "if they pay" body
	// does not. The unqualified default remains known even when it has no
	// target, and retains its historical controller fallback below.
	if !payerKnown {
		return !switched, false, false
	}
	switch ans {
	case "decline":
		// A decline moves on to the next payer; only when every payer has
		// declined does the orientation decide the body. idx is the payer
		// whose answer this is. A host that cannot pose the NEXT ask is also
		// a decline, so it must use that same orientation rather than running
		// every switched effect (the old `!poseUnlessAsk` inverted this case).
		if idx+1 < len(payers) {
			switch poseUnlessAsk(h, c, sa, cost, payers, idx+1) {
			case unlessAsked:
				return false, false, false
			case unlessServed:
				return unlessReread(h, c, sa)
			}
		}
		return !switched, false, false
	}
	// First pass: pose the pay decision to the first payer. With no
	// resolvable payer the resolving controller is asked — the same fallback
	// the pre-gate Counter primitive used, so the untargeted unpriceable-X
	// counter shapes keep asking today's player.
	if len(payers) == 0 {
		payers = []state.Target{{Player: c.Controller, IsPlayer: true}}
	}
	switch poseUnlessAsk(h, c, sa, cost, payers, 0) {
	case unlessAsked:
		return false, false, false
	case unlessServed:
		return unlessReread(h, c, sa)
	}
	// No engine host means poseUnlessAsk deterministically declined. Apply
	// exactly the same orientation as an answered decline.
	return !switched, false, false
}

// unlessReread is the gate's pass over an election the resolution kernel's
// tape served and the host settled (Ctx.UnlessPay and Ctx.UnlessNext set):
// the same read the legacy "unless_pay" re-entry makes when it re-enters
// this SA -- a pay applies the orientation, a decline moves on to the next
// payer -- marked served.
func unlessReread(h Host, c *Ctx, sa *cards.SA) (bool, bool, bool) {
	run, paid, _ := unlessProceed(h, c, sa)
	return run, paid, true
}

// unlessAsk is what poseUnlessAsk did with the election.
type unlessAsk uint8

const (
	// unlessNoHost: the host could not ask; the deterministic decline (R-9)
	// applies.
	unlessNoHost unlessAsk = iota
	// unlessAsked: the ask was posed (or deferred) and the resolution
	// suspended; the answer re-enters this SA.
	unlessAsked
	// unlessServed: the resolution kernel's tape served the answer, and the
	// host's answer record settled it in line into the asking walk's Ctx
	// (Ctx.UnlessPay, Ctx.UnlessNext) exactly as the legacy re-entry's
	// resume arm does.
	unlessServed
)

// unlessSubsRun reports whether the SA's SubAbility$ chain resolves for the
// pay outcome. Forge's AbilityUtils.handleUnlessCost: the value is absent
// (the corpus default, 'Always') or WhenPaid — subs run when the cost was
// paid — or WhenNotPaid — subs run when it was not. The 41 raw corpus lines
// carrying the parameter were unread before this gate existed; every other
// UnlessCost$ line keeps the default either way.
func unlessSubsRun(sa *cards.SA, paid bool) bool {
	switch strings.TrimSpace(sa.Params["UnlessResolveSubs"]) {
	case "", "Always":
		return true
	case "WhenPaid":
		return paid
	case "WhenNotPaid":
		return !paid
	}
	// An unknown value is the corpus default: every corpus occurrence spells
	// one of the three above, and guessing 'always' keeps an unseen future
	// spelling harmless rather than dropping chains.
	return true
}

// poseUnlessAsk offers payer payers[i] the unless cost. It reports whether
// the resolution suspended, the tape served and settled the answer, or the
// host could not ask (an effects-package test double, R-9) and the
// deterministic decline applies — with the Note the no-host path has always
// carried.
func poseUnlessAsk(h Host, c *Ctx, sa *cards.SA, cost string, payers []state.Target, i int) unlessAsk {
	payer := c.Controller
	if int(i) < len(payers) && payers[i].IsPlayer {
		payer = payers[i].Player
	}
	pay := unlessPayPhrase(cost)
	prompt, payLabel, declineLabel := pay+", or decline", pay, "Don't pay"
	switch sa.API {
	case "Counter":
		prompt = pay + " to save the spell, or decline"
		payLabel = pay + " — don't counter"
	case "CopySpellAbility":
		if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKUnlessSwitched)), "True") {
			prompt = pay + " to copy the spell, or decline"
			payLabel = pay + " — make a copy"
		} else {
			prompt = pay + " to stop the copy, or decline to copy"
			payLabel = pay + " — no copy"
			declineLabel = "Don't pay — make a copy"
		}
	default:
		if n, dmg := ParseDamageUnlessCost(cost); dmg {
			// The damage-payment offer (Vexing Devil, Longhorn Firebeast —
			// the Sacrifice UnlessSwitched$ True population): "paying" is
			// taking the damage, so the labels must say so, never "Pay the
			// cost". The switched offer is the fb-20260916T070855Z wording;
			// the unswitched shape (no corpus carrier today) mirrors the
			// plain unless orientation.
			name := "The permanent"
			if o := h.Game().Obj(c.Source); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			payLabel = "Take " + strconv.Itoa(n) + " damage"
			if strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKUnlessSwitched)), "True") {
				prompt = name + " deals " + strconv.Itoa(n) + " damage to you — accept?"
				declineLabel = "Refuse — it stays"
			} else {
				prompt = name + " — take " + strconv.Itoa(n) + " damage to spare it, or sacrifice it"
				declineLabel = "Sacrifice it"
			}
		}
	}
	// Rules hosts prove whether the pay branch is reachable (floating mana
	// plus the sources the payment window can tap, and the choice-bearing
	// Sac/Discard/Reveal/Draw/RevealChosen/SubCounter components).
	// UnlessCostPayableFromCtx hands the host this very resolution context,
	// so a Draw<.../Player.targetedBy> or RevealChosen cost is evaluated
	// against the same targets/roles the pay path will use. A host that
	// implements only the two-argument form is still consulted. The effects
	// test host (and other embedders) keep the two options -- R-9 still
	// declines when it cannot ask.
	// DamageYou<N> is the one strict-parser exception: the Sacrifice arm has
	// its own rules-side payment path (payUnlessDamageCost), so its recognised
	// damage offer is payable even though generic ParseUnlessCost deliberately
	// rejects DamageYou. Every other strict-unpriceable cost reaches the shared
	// gate below and exposes only decline.
	_, damagePayment := ParseDamageUnlessCost(cost)
	damagePayment = sa.API == "Sacrifice" && damagePayment
	// A non-rules host cannot price a cost and keeps the historic two-option
	// R-9 ask; its no-host fallback deterministically declines. A rules host
	// supplies the actual fail-closed offer gate for every non-damage cost.
	payable := true
	if !damagePayment {
		if checker, ok := h.(interface {
			UnlessCostPayableFromCtx(state.PlayerID, string, *Ctx) bool
		}); ok {
			payable = checker.UnlessCostPayableFromCtx(payer, cost, c)
		} else if checker, ok := h.(interface {
			UnlessCostPayable(state.PlayerID, string) bool
		}); ok {
			payable = checker.UnlessCostPayable(payer, cost)
		}
	}
	d := &decision.Decision{Player: payer, Kind: decision.KModes,
		Min: 1, Max: 1, Source: c.Source, ResumeKind: "unless_pay",
		ResumeSA: sa, ResumeTarget: i, Prompt: prompt,
		// The asking SA's Remembered rides the decision (the same channel the
		// choice asks use): a replacement body's unless ask — Breathstealer's
		// Crypt's discard — must re-enter with the Remembered it had at ask
		// time (the RememberDrawn$ cards), or the replacement-arm reseed
		// loses them and the body's condition gates read an empty set.
		ResumeRemembered: append([]state.Target(nil), c.Remembered...),
		// The TargetUnique$ accumulator rides too (the same ride every ask
		// poseTargetsAsk makes): an unless ask parked between two TargetUnique$
		// riders (Withdraw's UnlessCost$ ChangeZone sub) must not drop the
		// earlier riders' picks at the resumed Ctx's rebuild.
		ResumeTargetsUnique: copyTargets(c.TargetsUnique),
		Options: []decision.Option{
			{Index: 0, Kind: "mode", Label: declineLabel, Obj: c.Source, Player: payer, Mode: decision.ModeUnlessDecline},
		}}
	if payable {
		d.Options = []decision.Option{
			{Index: 0, Kind: "mode", Label: payLabel, Obj: c.Source, Player: payer, Mode: decision.ModeUnlessPay},
			{Index: 1, Kind: "mode", Label: declineLabel, Obj: c.Source, Player: payer, Mode: decision.ModeUnlessDecline},
		}
	}
	if _, ok := AskTape(h, d); ok {
		return unlessServed
	}
	if Ask(h, d) == AskAsked {
		return unlessAsked // resolution suspended; the answer re-enters this SA.
	}
	// Fuzz/no-engine host: the deterministic decline (R-9). The pay was
	// never posed, so resolve as if the player declined.
	h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
		Text: "may pay declined (UnlessCost not asked on this host)"})
	return unlessNoHost
}

// UnlessPayers resolves the UnlessPayer$ selector to the players who get the
// pay offer, in deterministic AliveFrom order, deduplicated. It returns no
// targets for an unresolved named selector; callers that need to distinguish
// that from Forge's unqualified default use unlessPayerTargets below.
func UnlessPayers(h Host, c *Ctx, sa *cards.SA) []state.Target {
	out, _ := unlessPayerTargets(h, c, sa)
	return out
}

// unlessPayerTargets is the binding-aware half of UnlessPayers. known is
// false only when a *named* payer cannot be resolved from state the engine
// actually carries. That distinction prevents an Aura's EnchantedController
// (or an unmodelled ImprintedController) from falling through to c.Targets
// and asking the wrong player. A missing target for the empty/default
// selector remains known: Forge defaults it to the resolving controller.
func unlessPayerTargets(h Host, c *Ctx, sa *cards.SA) ([]state.Target, bool) {
	g := h.Game()
	if g == nil {
		return nil, false
	}
	spec := strings.TrimSpace(sa.Params["UnlessPayer"])
	var out []state.Target
	seen := map[state.PlayerID]bool{}
	add := func(p state.PlayerID) {
		if int(p) < 0 || int(p) >= len(g.Players) || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, state.Target{Player: p, IsPlayer: true})
	}
	addTargets := func(ts []state.Target) {
		for _, t := range ts {
			if t.IsPlayer {
				add(t.Player)
			} else if o := g.Obj(t.Obj); o != nil {
				add(o.Controller)
			}
		}
	}
	switch spec {
	case "":
		addTargets(c.Targets)
	case "TargetedController", "TargetedPlayer", "ThisTargetedController", "TargetedOrController":
		// A missing target uses the historical resolving-controller fallback
		// below. This is a known target selector, unlike an unknown role.
		addTargets(c.Targets)
	case "You":
		add(c.Controller)
	case "EnchantedController":
		// Aura sources retain their attachment in state.Object.AttachedTo.
		// The attached permanent's controller is the named payer, not the
		// Aura's controller (Power Taint and Paralyze).
		source := g.Obj(c.Source)
		if source == nil || source.AttachedTo == 0 {
			return nil, false
		}
		enchanted := g.Obj(source.AttachedTo)
		if enchanted == nil {
			return nil, false
		}
		add(enchanted.Controller)
	case "EnchantedPlayer":
		// The seat an Aura/Curse source enchants. The link is the source's
		// own AttachedPlayer/HasAttachedPlayer pair (written only by
		// events.Attach's player branch); a source that is not attached to a
		// player has no honest binding and fails closed, exactly as before.
		source := g.Obj(c.Source)
		if source == nil || !source.HasAttachedPlayer {
			return nil, false
		}
		add(source.AttachedPlayer)
	case "ReplacedPlayer", "NonReplacedPlayer":
		// The draw-er of a replaced Draw event, and its complement (Zur's
		// Weirding's "any other player may pay 2 life"). Set only on a Draw
		// replacement's own context — fail closed outside one.
		if !c.ReplacedPlayer.IsPlayer {
			return nil, false
		}
		if spec == "ReplacedPlayer" {
			add(c.ReplacedPlayer.Player)
			break
		}
		for _, p := range g.AliveFrom(0) {
			if p != c.ReplacedPlayer.Player {
				add(p)
			}
		}
	case "Imprinted", "ImprintedController":
		// Forge's UseImprinted$ binds the RepeatEach iteration's current
		// subject as "Imprinted" (Heroism's attacking red creature, Stench
		// of Evil's destroyed Plains). The engine binds it on the iteration
		// context and carries it through a resumed ask; a zero subject means
		// this SA is outside such a loop (or the subject left the game
		// entirely) — fail closed rather than guess from Remembered, whose
		// last entry can be anything the body remembered.
		if c.RepeatSubject.IsPlayer {
			add(c.RepeatSubject.Player)
		} else if o := g.Obj(c.RepeatSubject.Obj); o != nil {
			add(o.Controller)
		} else {
			return nil, false
		}
	case "Targeted", "Player.targetedBy":
		addTargets(c.Targets)
	case "ParentTarget":
		addTargets(parentLinkTargets(c))
	case "TriggeredTarget":
		if !c.TriggerTarget.IsPlayer && c.TriggerTarget.Obj == 0 {
			return nil, false
		}
		addTargets([]state.Target{c.TriggerTarget})
	case "Remembered", "RememberedController", "Player.IsRemembered":
		if len(c.Remembered) == 0 {
			return nil, false
		}
		addTargets(c.Remembered)
	case "Player":
		for _, p := range g.AliveFrom(0) {
			add(p)
		}
	case "Opponent", "Player.Opponent":
		for _, p := range g.AliveFrom(c.Controller) {
			if p != c.Controller {
				add(p)
			}
		}
	case "ChosenPlayer":
		if len(c.Chosen) == 0 {
			return nil, false
		}
		addTargets(c.Chosen)
	case "TriggeredPlayer":
		if !c.TriggerPlayer.IsPlayer {
			return nil, false
		}
		add(c.TriggerPlayer.Player)
	case "TriggeredCardController", "TriggeredCardLKIController":
		if p, ok := TriggeredCardController(g, c.TriggerContext, c.Remembered); ok {
			add(p)
		} else {
			return nil, false
		}
	case "TriggeredSourceSAController", "TriggeredSourceController", "TriggeredSpellAbilityController":
		// These forms name the controller of the source the triggering EVENT
		// captured -- the targeting spell a BecomesTarget trigger holds in
		// TriggerSource (Reality Smasher, Kira, the glasskite family: "unless
		// its controller discards"), or the dealing source a DamageDone
		// trigger captured -- not the resolving trigger's controller. The
		// role is preferred when the firing trigger captured one, the same
		// precedence effects/context.go's TriggeredSourceController defined-
		// arm uses; Ctx.Controller (the trigger source's own controller,
		// bound at pushTrigger) stays the fallback for contexts without the
		// role.
		if c.TriggerSource != 0 {
			if o := g.Obj(c.TriggerSource); o != nil {
				add(o.Controller)
				return out, true
			}
		}
		add(c.Controller)
	case "NonTriggeredCardController":
		// The controller of the (non-triggered) resolving card -- the caster
		// the SpellCast trigger watched. Ctx.Controller is bound from that
		// source when the ability is put on the stack.
		add(c.Controller)
	case "TriggeredTargetController":
		if !c.TriggerTarget.IsPlayer && c.TriggerTarget.Obj == 0 {
			return nil, false
		}
		addTargets([]state.Target{c.TriggerTarget})
	case "TriggeredActivator":
		if c.TriggerActivator.IsPlayer {
			add(c.TriggerActivator.Player)
		} else if o := g.Obj(c.TriggerActivator.Obj); o != nil {
			add(o.Controller)
		} else {
			// Old trigger contexts did not retain activators. Keep their
			// documented source-controller fallback until every such context
			// is populated; real contexts above use the actual activator.
			add(c.Controller)
		}
	case "TriggeredDefendingPlayer", "DefendingPlayer":
		if !c.DefendingPlayer.IsPlayer {
			return nil, false
		}
		add(c.DefendingPlayer.Player)
	case "TriggeredAttackingPlayer", "TriggeredAttackerController":
		if !c.AttackingPlayer.IsPlayer {
			return nil, false
		}
		add(c.AttackingPlayer.Player)
	default:
		// Do not silently substitute c.Targets for a selector the context does
		// not carry (ReplacedPlayer, NonReplacedPlayer, ImprintedController and
		// their kind). That used to charge an unrelated target; fail closed
		// instead.
		return nil, false
	}
	// Deterministic AliveFrom order, whoever named them.
	alive := g.AliveFrom(0)
	rank := map[state.PlayerID]int{}
	for i, p := range alive {
		rank[p] = i
	}
	if len(out) == 0 && !unlessPayerControllerFallback(spec) {
		// A named binding that yielded no live player is unavailable, not the
		// default-selector case where c.Controller is intentionally used.
		return nil, false
	}
	sortTargets(out, rank)
	return out, true
}

// sortTargets orders player targets by the seat order rank (AliveFrom
// position), keeping the slice's own order for ties (no ties arise — add
// dedupes — but the comparator is total regardless).
// unlessPayerControllerFallback identifies the legacy target-based selector
// family whose absent target is deliberately charged to c.Controller. Every
// other selector must bind a real role or fail closed.
func unlessPayerControllerFallback(spec string) bool {
	switch spec {
	case "", "TargetedController", "TargetedPlayer", "ThisTargetedController", "TargetedOrController", "Targeted", "ParentTarget":
		return true
	}
	return false
}

func sortTargets(ts []state.Target, rank map[state.PlayerID]int) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && rank[ts[j].Player] < rank[ts[j-1].Player]; j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

// unlessPayPhrase is the imperative a player-facing unless ask leads with:
// "Pay <unlessCostLabel>", except for a single-object sacrifice, which reads
// as the action itself ("Sacrifice a land", Chain of Silence's switched copy
// gate; "Sacrifice another creature" from Forge's own /description) rather
// than degrading to "Pay the cost". Display only, like unlessCostLabel: raw
// cost syntax (Sac<1/Land>) must never reach a prompt or label.
func unlessPayPhrase(cost string) string {
	if what, ok := sacrificeOneLabel(cost); ok {
		return "Sacrifice " + what
	}
	return "Pay " + unlessCostLabel(cost)
}

// sacrificeOneLabel renders a lone Sac<1/Type> or Sac<1/Filter/description>
// token: the description verbatim when Forge supplies one, otherwise an
// article plus a plain one-word type ("a land", "an artifact"). Any other
// shape (a count above one, a dotted filter with no description, a mixed
// cost) reports false.
func sacrificeOneLabel(cost string) (string, bool) {
	inner, ok := strings.CutPrefix(strings.TrimSpace(cost), "Sac<1/")
	if !ok || !strings.HasSuffix(inner, ">") {
		return "", false
	}
	inner = strings.TrimSuffix(inner, ">")
	if strings.ContainsAny(inner, "<>") {
		return "", false
	}
	if _, desc, found := strings.Cut(inner, "/"); found {
		desc = strings.TrimSpace(desc)
		return desc, desc != ""
	}
	if inner == "" || strings.Trim(inner, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") != "" || inner == "CARDNAME" {
		return "", false
	}
	article := "a "
	if strings.ContainsRune("AEIOUaeiou", rune(inner[0])) {
		article = "an "
	}
	return article + strings.ToLower(inner), true
}

// unlessCostLabel renders an UnlessCost$ value for the humans a
// decision.Decision can reach. A plain mana cost ("1", "3", "2 U", "R R") is
// already readable and comes back verbatim -- that is every repo-deck Counter
// with an UnlessCost$ except Mausoleum Wanderer and Reality Smasher. A fixed
// life payment PayLife<N> (the shock-land election family -- Steam Vents'
// "As Steam Vents enters, you may pay 2 life" -- and the rest of the corpus's
// UnlessCost$ PayLife population) is equally readable: rules' ParseUnlessCost
// prices exactly that token and charges exactly N life, so it renders "N
// life" (fb-20260917T233137Z: the election used to say "Pay the cost, or
// decline" without naming the cost). A cost mixing mana with PayLife<N> (2
// raw corpus lines, "1 PayLife<3>") renders both parts; PayLife<X>/Y keep the
// degradation, their value being unresolved here.
// Everything else is raw Forge script: a bare SVar name (X, Y, Z, whose value
// this engine does not read at all) or a bracket form (Discard<1/Hand>,
// ExileFromGrave<1/All>). Those must not reach a player's screen, so they
// render as "the cost". Display only: the amount actually charged is decided
// by rules' unless-payment path.
func unlessCostLabel(cost string) string {
	fields := strings.Fields(cost)
	if len(fields) == 0 {
		return "the cost"
	}
	var life, mana []string
	for _, f := range fields {
		if n, ok := payLifeAmount(f); ok {
			life = append(life, strconv.Itoa(n)+" life")
			continue
		}
		if _, err := strconv.Atoi(f); err == nil {
			mana = append(mana, f) // generic amount
			continue
		}
		if len(f) >= 3 && f[0] == '{' && f[len(f)-1] == '}' {
			inner := f[1 : len(f)-1]
			if _, err := strconv.Atoi(inner); err == nil {
				mana = append(mana, f) // resolved generic amount
				continue
			}
		}
		if strings.Trim(f, "WUBRGC") == "" {
			mana = append(mana, f) // colour/colourless symbols
			continue
		}
		return "the cost"
	}
	switch {
	case len(life) == 0:
		return cost // plain mana cost, verbatim as before
	case len(mana) == 0:
		return strings.Join(life, ", ")
	default:
		return strings.Join(mana, " ") + " and " + strings.Join(life, ", ")
	}
}

// payLifeAmount reports whether f is Forge's FIXED life-payment token
// PayLife<N> and extracts N. rules' lifeCost (rules/mana.go) prices exactly
// this shape — bare ASCII digits only — and charges N life, so the label can
// name it. The inner text is accepted only when every rune is a digit: a
// sign-prefixed value like PayLife<-2> or PayLife<+2> would Atoi cleanly but
// lifeCost rejects it (hard decline), so the label must not promise a
// payable cost the payment path refuses. PayLife<X>, PayLife<Y> and any
// other bracket form stay false: their value is not resolved here and the
// "the cost" degradation applies.
func payLifeAmount(f string) (int, bool) {
	rest, ok := strings.CutPrefix(f, "PayLife<")
	if !ok || !strings.HasSuffix(rest, ">") {
		return 0, false
	}
	inner := strings.TrimSuffix(rest, ">")
	if inner == "" {
		return 0, false
	}
	for _, r := range inner {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(inner)
	return n, err == nil
}
