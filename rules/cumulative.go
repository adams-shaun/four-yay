package rules

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Cumulative upkeep is expanded by cards into an ordinary beginning-of-upkeep
// Phase trigger. Consequently it is ordered and placed with every other upkeep
// trigger and can be responded to or countered before it resolves (CR 702.46a).
// This file owns that trigger's resolution-time age counter and payment window.
// triggeredEffectCost is the narrower sibling window for Mana Vault's Cost$-
// bearing triggered Untap effect. Successive merges kept renumbering these
// two above main's growing chooseFor enum (chooseManaExile, then
// chooseOpening/chooseSuspendCast, now chooseStation/chooseUnlock): chooseFor
// values are transient engine RAM — Clone copies the struct, no event or wire
// field ever carries one — so the two windows move above the current highest.
const (
	chooseCumulative         chooseFor = chooseUnlock + 1
	chooseTriggeredCost      chooseFor = chooseUnlock + 2
	chooseTriggeredMandatory chooseFor = chooseExert + 1
)

type cumulativeAction struct {
	kind string
	n    int32
	spec string
}

type cumulativeUpkeep struct {
	stackObj   state.ObjID
	source     state.ObjID
	player     state.PlayerID
	amount     Cost
	action     *cumulativeAction
	costLabel  string
	windowDone bool

	// pips carries the flexible-pip payment announcement (CR 601.2b/107.4e-f)
	// for this window, exactly as triggeredEffectCost.pips does:
	// pipAnnounceAsk asks each of the cost's announcement pips, fold folds the
	// elected faces into the cost the pay gate prices and the pay arm charges,
	// and the answer arm advances through the shared accumulator. A value
	// struct, so a clone carries it.
	pips pipAnnounce

	// actionRemaining counts repeated action payments still to make. Most
	// action costs ask once per age counter; object sacrifices/discards ask
	// for the full scaled set at once, while PutCardToLibFromSameGrave uses
	// actionOwner between its graveyard and card-selection asks.
	actionRemaining int32
	actionOwner     state.PlayerID
}

type triggeredEffectCost struct {
	resume     *resumePoint
	source     state.ObjID
	player     state.PlayerID
	amount     Cost
	costLabel  string
	windowDone bool
	// tapIdx is the next unsettled dynamic tapXType part (rules/mana.go's
	// dynTapCost heads) of amount: the tap election is this window's payment
	// for those parts, and the index keeps a multi-part cost asking in order.
	tapIdx int
	// mandatory marks a `Cost$ Mandatory <...>` body (TrigsMand1): a
	// mandatory cost has no "may" and therefore no pay/decline election, so
	// this window walks its choice-bearing non-mana components (Sac, Exile),
	// poses a real pick where one exists and a no-ask settle where it does
	// not, emits the settled events, and only then runs the parked body. A
	// component that cannot be paid skips the body (Forge: the payment gates
	// the effect). Only the Sac/Exile shapes enter this path
	// (mandatorySettleShape); everything else keeps the free-executor
	// carve-out in triggerBodyNeedsCostWindow.
	mandatory bool
	// trig is the trigger context of the ability the window parks (exg1):
	// a cost spec naming a trigger referent -- Forge's bare
	// Card.TriggeredNewCard in the `Cost$ ExileAnyGrave<1/Card.
	// TriggeredNewCard>` "you may exile it" family -- resolves against THIS
	// context when the window's candidate walk evaluates the part's spec, so
	// the cost exiles exactly the card the triggering event captured. Zero
	// (every non-trigger use, a hand-built push) leaves the filter's
	// fail-closed default: a referent-bearing spec matches nothing and the
	// cost is unpayable. A value struct, so the window clone carries it.
	trig effects.TriggerContext
	// part is the cursor into the flat choice-bearing component list
	// (triggeredMandatoryParts: Sac then Exile then Discard) a settle walks.
	part int
	// sacs, exiles and discards accumulate the settled picks (the same
	// reservation list the unless-pay and cast flows keep) so one object
	// cannot pay two parts and the settle can emit the exact events the
	// picks name.
	sacs     []state.ObjID
	exiles   []state.ObjID
	discards []state.ObjID
	// moveGraves accumulates the settled ExiledMoveToGrave picks (the same
	// reservation list the other components keep) so the settle can emit the
	// owner's-graveyard moves the picks name.
	moveGraves []state.ObjID
	// pips carries this window's flexible-pip payment announcement (CR
	// 601.2b/107.4e-f) -- Alesha's `Cost$ WB WB` and the other
	// announcement-pip trigger costs. pipAnnounceAsk walks the cost's
	// combined announcement-pip list (Cost.annPipCount, the same order and
	// alternatives the cast flow's manaAsk offers, via the one shared
	// Cost.announcePip) one decision at a time; announcedCost folds the
	// elected faces back into the cost the gates price and the pay arm
	// charges. It is a value struct, so the window clone carries it.
	pips pipAnnounce
	// xPaid is the X this window's cost carried after the X fold: the
	// payer's announced value (the choose-X ask) or the face SVar:X's fixed
	// resolved value. It rides the resume point (rp.winPaidX) into the body
	// at the pay arm, so the body's Count$xPaid / NumCards$ X / TokenPower$ X
	// reads THIS payment, never the source permanent's cast-time X (which is
	// all o.X and triggerPaidX can supply for an attack, ETB or end-step
	// trigger). Zero when the cost carries no unfolded X.
	xPaid int32
	// xDone guards the X arm's once-ness: triggeredCostPaymentAsk re-enters
	// after every mana-ability window (the "done" continuation), and the
	// choose-X ask must be posed exactly once -- after its answer (or the
	// fixed fold) the cost carries no unfolded X any more, so the
	// costCarriesUnfoldedX guard alone would suffice for the re-entries;
	// the flag also keeps a payer-chooses ask from re-posing if a future
	// call path re-enters while the announcement is still unanswered.
	xDone bool
}

// xFoldBoundCap is the defensive ceiling on an announced trigger-cost X when
// the potential pool is unbounded (an indeterminate source priced
// potentialUnbounded saturates Mana.Total): every value up to the cap is
// genuinely payable there, and no corpus X-fold carrier approaches it. The
// same hard-cap shape the multikickAsk/replicateAsk loops use.
const xFoldBoundCap int32 = 64

// costCarriesUnfoldedX reports whether the cost carries an X the payment
// cannot charge until it is folded: a printed {X} pip (Cost.X, Elenda and
// Azor's "Cost$ X W U B") or an announced PayLife<X> part (LifeX Spec X,
// Vizkopa Confessor's "pay any amount of life"). The other announced parts
// (Sac<X/Spec>, PayEnergy<X>, SubCounter<X/Kind>) are NOT this shape: the
// trigger window has no settle for them, and they keep the decline-only
// hard-decline convention (the non-mana-settle follow-up family).
func costCarriesUnfoldedX(c Cost) bool {
	if c.X > 0 {
		return true
	}
	for _, part := range c.LifeX {
		if part.Spec == "X" {
			return true
		}
	}
	for _, part := range c.Energy {
		if part.Spec == "X" {
			return true
		}
	}
	return false
}

// foldCostX folds a chosen X into the cost, once per unfolded shape: the
// printed {X} pips become that much generic mana (Cost.WithX) and each
// announced PayLife<X> part becomes that much fixed life, so the folded
// cost is Priceable and payManaConv can charge it whole.
func foldCostX(c Cost, x int32) Cost {
	out := c.WithX(x)
	if len(out.LifeX) > 0 {
		for _, part := range out.LifeX {
			if part.Spec == "X" {
				out.Life = addClampedGeneric(out.Life, int64(x))
			}
		}
		out.LifeX = nil
	}
	return out
}

// scaleCost repeats every mana/life payment component once per age counter.
func scaleCost(c Cost, n int32) Cost {
	if n <= 1 {
		return c
	}
	out := Cost{}
	for i := range c.Colored {
		out.Colored[i] = c.Colored[i] * n
	}
	out.Generic, out.Life = c.Generic*n, c.Life*n
	// CR 702.24a / CR 107.4h: the upkeep cost is paid once per age counter, so a
	// {S} pip must count once per counter too. Snow is a first-class cost
	// component (rules/mana.go's ParseCost), not a generic substitute, so it is
	// scaled like the mana it is -- without this a snow cumulative upkeep's
	// requirement vanished at the second age counter and the permanent could be
	// kept for free.
	out.Snow = c.Snow * n
	for i := int32(0); i < n; i++ {
		out.Hybrid = append(out.Hybrid, c.Hybrid...)
		out.Twobrid = append(out.Twobrid, c.Twobrid...)
		out.Phyrexian = append(out.Phyrexian, c.Phyrexian...)
	}
	out.Tap, out.X = c.Tap, c.X
	return out
}

// parseCumulativeAction recognizes the complete non-mana/action vocabulary in
// the pinned corpus. These are actions performed once per age counter, not
// malformed mana symbols; keeping the parser local prevents ordinary Cost$
// callers from acquiring cumulative-upkeep-only semantics.
func parseCumulativeAction(label string) (*cumulativeAction, bool) {
	label = strings.TrimSpace(label)
	open := strings.IndexByte(label, '<')
	if open <= 0 || !strings.HasSuffix(label, ">") {
		return nil, false
	}
	kind := label[:open]
	fields := strings.Split(label[open+1:len(label)-1], "/")
	if len(fields) == 0 {
		return nil, false
	}
	n64, err := strconv.ParseInt(fields[0], 10, 32)
	if err != nil || n64 <= 0 {
		return nil, false
	}
	a := &cumulativeAction{kind: kind, n: int32(n64)}
	switch kind {
	case "Sac", "Discard":
		if len(fields) < 2 {
			return nil, false
		}
		a.spec = fields[1]
	case "AddCounter":
		if len(fields) < 2 {
			return nil, false
		}
		a.spec = fields[1]
		if len(fields) >= 3 {
			a.spec += "/" + fields[2]
		}
	case "AddMana":
		if len(fields) < 2 || len(fields[1]) != 1 || !strings.ContainsRune("WUBRGC", rune(fields[1][0])) {
			return nil, false
		}
		a.spec = fields[1]
	case "Draw":
		if len(fields) < 2 || fields[1] != "You" {
			return nil, false
		}
	case "ExileFromTop":
		if len(fields) < 2 || fields[1] != "Card" {
			return nil, false
		}
	case "FlipCoin":
	case "GainControl":
		if len(fields) < 2 {
			return nil, false
		}
		a.spec = fields[1]
	case "GainLife":
		if len(fields) < 2 || fields[1] != "Player.Opponent" {
			return nil, false
		}
	case "PutCardToLibFromSameGrave":
		if len(fields) < 3 || fields[2] != "Card" {
			return nil, false
		}
		a.spec = fields[2]
	default:
		return nil, false
	}
	return a, true
}

// startCumulativeUpkeep starts resolution of the already-stacked keyword
// trigger. No age counter exists before this point: responses and another
// simultaneous upkeep trigger therefore observe the pre-resolution value.
func (e *Engine) startCumulativeUpkeep(stackObj, source state.ObjID, sa *cards.SA) {
	o := e.G.Obj(source)
	stack := e.G.Obj(stackObj)
	if o == nil || o.Zone != state.ZBattlefield || stack == nil {
		e.finishResumption(stackObj)
		return
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: source, Counter: "AGE", Amount: 1})
	label := sa.Params["Cost"]
	parsed := e.parseCost(label)
	action, actionOK := parseCumulativeAction(label)
	// The triggered ability's controller was captured when it was placed on
	// the stack. A response may change control of the cumulative permanent,
	// but it must not transfer the already-triggered payment decision.
	cu := &cumulativeUpkeep{stackObj: stackObj, source: source, player: stack.Controller,
		amount: scaleCost(parsed, o.Counter("AGE")), costLabel: cumulativeCostLabel(label, action, parsed),
		actionRemaining: o.Counter("AGE")}
	if actionOK {
		cu.action = action
		cu.amount = Cost{}
	}
	e.cumulative = cu
	e.cumulativePaymentAsk()
}

// triggerBodyNeedsCostWindow reports whether a trigger effect (or an accepted
// optional trigger's body) found through findTriggerForAbility must enter the
// triggered-cost pay/decline window (startTriggeredEffectCost) BEFORE its
// body runs, rather than executing for free. Forge's `Cost$ <cost>` on a
// trigger body is the "you may pay <cost>. If you do, ..." idiom, so ANY
// non-Mandatory Cost$ arms the window -- not just the Untap / ImmediateTrigger /
// Draw / dyn-tap shapes the original allowlist served. The window itself keeps
// the split: a Priceable cost (plain mana, fixed PayLife<N>) offers a real
// "pay"; a cost whose choice-bearing components are exactly Sac/Exile/Discard
// parts and whose draws and mana half resolve offers a real pay and settles
// the components for real (trigcost2, triggeredCostComponentsPayable);
// everything else lands decline-only (the ParseUnlessCost hard-decline
// convention -- never a free execution, never a zero-amount payment).
//
// Carve-outs, each deliberate:
//   - API Mana: mana abilities have their own activation path.
//   - API CopySpellAbility: its window arming needs the trigger context's
//     event role (TriggerAbility/TriggerCard -- a context-less synthetic push
//     keeps the free-executor semantics), so the call sites arm it separately.
//   - a `Mandatory` cost prefix: the mandatory family (Cost$ Mandatory
//     Sac<1/CARDNAME>, PayLife<X>, Exile<...>) is NOT the pay idiom -- its
//     payment is a real non-mana settle rules does not yet run -- and the
//     window could only offer decline-only, which for a mandatory payment
//     would be the WORSE regression (the body would never run AND the payment
//     would never happen). Those bodies keep the established free-executor
//     semantics (follow-up ticket), EXCEPT the shapes the existing gate
//     already served: the Untap/ImmediateTrigger APIs and the dynamic
//     tapXType election (yotia_declares_war's "Mandatory tapXType<X/Artifact>")
//     -- the tap election is the payment there, so the window stays right.
func (e *Engine) triggerBodyNeedsCostWindow(sa *cards.SA) bool {
	if sa == nil || sa.Params["Cost"] == "" || sa.API == "Mana" || sa.API == "CopySpellAbility" {
		return false
	}
	if !strings.HasPrefix(sa.Params["Cost"], "Mandatory") {
		return true
	}
	if sa.API == "Untap" || sa.API == "ImmediateTrigger" {
		return true
	}
	c := e.parseCost(sa.Params["Cost"])
	// The dynamic tapXType heads (rules/mana.go's dynTapCost): the tap
	// election is the payment, the empty election the decline.
	if costCarriesDynTap(c) {
		return true
	}
	// trigmand1: the `Mandatory Sac<...>` / `Mandatory Exile<...>` shapes are
	// now settled for real by the mandatory path in this window (a pick where
	// a choice exists, the exact Sacrifice/Exile event otherwise), so they
	// enter it. Any other mandatory component keeps the free-executor
	// carve-out: a Mandatory PayLife<X>/PayEnergy/replacement body has no
	// settle here (see the AGENTS.md approximation row), and arming it would
	// only land it decline-only -- the WORSE regression.
	return mandatorySettleShape(c)
}

// mandatorySettleShape reports whether a parsed `Mandatory` cost is one the
// mandatory window can fully settle: its non-mana components are exactly the
// choice-bearing Sac/Exile/MoveToGrave parts (settled with a real pick where
// a choice exists) and everything else it carries is chargeable by
// payManaConv (plain mana / fixed PayLife<N>). Any other component --
// PayEnergy, PayLife<X>, Discard, Reveal, Behold, SubCounter, a dynamic tap,
// ... -- returns false so the body keeps today's free execution rather than
// a decline-only ask.
func mandatorySettleShape(c Cost) bool {
	if len(c.Sac) == 0 && len(c.Exile) == 0 && len(c.MoveToGrave) == 0 {
		return false
	}
	stripped := c
	stripped.Sac = nil
	stripped.Exile = nil
	stripped.MoveToGrave = nil
	return stripped.Priceable()
}

// cumulativeCostLabel renders the player-facing phrase for a cumulative
// upkeep's Cost$ value. A mana cost goes through the shared costPhrase; a
// non-mana ACTION (Forge's keyword-action vocabulary parseCost does not
// model -- AddMana, GainLife, FlipCoin, ...) has its own bounded prose
// renderer so the label never falls back to the raw token or to the
// one-generic parseCost degradation.
func cumulativeCostLabel(raw string, action *cumulativeAction, c Cost) string {
	if action != nil {
		return cumulativeActionPhrase(action)
	}
	return costPhrase(c)
}

// cumulativeActionPhrase renders one cumulative-upkeep action as prose. The
// kinds are the closed set parseCumulativeAction accepts.
func cumulativeActionPhrase(a *cumulativeAction) string {
	n := strconv.FormatInt(int64(a.n), 10)
	plural := ""
	if a.n != 1 {
		plural = "s"
	}
	switch a.kind {
	case "Sac":
		return "sacrifice " + n + " " + specNoun(a.spec, "permanent")
	case "Discard":
		return "discard " + n + " card" + plural
	case "AddCounter":
		kind, _, _ := strings.Cut(a.spec, "/")
		return "put " + n + " " + strings.ToUpper(kind) + " counter" + plural + " on this permanent"
	case "AddMana":
		return "add " + a.spec
	case "Draw":
		return "draw " + n + " card" + plural
	case "ExileFromTop":
		return "exile the top " + n + " card" + plural + " of your library"
	case "FlipCoin":
		return "flip a coin"
	case "GainControl":
		return "gain control of " + specNoun(a.spec, "permanent")
	case "GainLife":
		return "an opponent gains " + n + " life"
	case "PutCardToLibFromSameGrave":
		return "put " + n + " card" + plural + " from your graveyard into your library"
	}
	return ""
}

// startTriggeredEffectCost parks Mana Vault's triggered Untap before it runs
// and opens the same mana-ability-only payment window used by mana cumulative
// upkeep. Callers gate this helper on triggerBodyNeedsCostWindow.
func (e *Engine) startTriggeredEffectCost(rp *resumePoint, source state.ObjID) {
	o := e.G.Obj(rp.obj)
	if o == nil || o.Zone != state.ZStack || rp.sa == nil {
		return
	}
	label := rp.sa.Params["Cost"]
	amount := e.parseCost(label)
	e.triggerCost = &triggeredEffectCost{resume: rp, source: source,
		player: o.Controller, amount: amount, costLabel: costPhrase(amount),
		mandatory: strings.HasPrefix(label, "Mandatory"), trig: e.triggerContexts[rp.obj]}
	if e.triggerCost.mandatory {
		e.advanceTriggeredMandatory(e.triggerCost)
		return
	}
	e.triggeredCostPaymentAsk()
}

// paymentWindowAsk is the continuation mana activation calls after it adds
// mana. Only one resolution-time payment can be live on the single stack.
func (e *Engine) paymentWindowAsk() {
	if e.triggerCost != nil {
		e.triggeredCostPaymentAsk()
		return
	}
	if e.echo != nil {
		// kw:Echo's mana window re-opens after each activated source (the
		// same continuation every other window uses).
		e.echoElectionAsk()
		return
	}
	e.cumulativePaymentAsk()
}

func (e *Engine) paymentManaAsk(player state.PlayerID, source state.ObjID, amount Cost, windowDone bool, prompt string, flow chooseFor) bool {
	return e.paymentManaAskClass(player, source, amount, windowDone, prompt, flow, paymentOther)
}

func (e *Engine) paymentManaAskClass(player state.PlayerID, source state.ObjID, amount Cost, windowDone bool, prompt string, flow chooseFor, class paymentClass) bool {
	rider := pipRider{anyColor: e.payerGrantsIgnoreColor(player, source), anyType: e.payerGrantsIgnoreType(player, source)}
	if windowDone || !amount.Priceable() || e.costPayableClass(player, paymentDescriptor{id: source, class: class, cost: &amount}, rider, amount) {
		return false
	}
	var sources []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, player) {
		if e.untappedManaSource(player, id) {
			sources = append(sources, id)
		}
	}
	if len(sources) == 0 {
		return false
	}
	d := &decision.Decision{Player: player, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: prompt, Source: source}
	for _, id := range sources {
		// The beyond-tap cost marker every "activate" offer carries
		// (legal.go, cast.go, ward.go), so an answerer can tell a
		// non-tapping pool converter from a tap source.
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate", Obj: id,
			Label: "Tap " + e.G.Obj(id).Face().Name + " for mana",
			Cost:  manaActivationCostMarker(e.availableManaAbilities(player, id))})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "done", Label: "Done"})
	e.choosing = flow
	e.ask(d)
	return true
}

func (e *Engine) cumulativePaymentAsk() {
	cu := e.cumulative
	if cu == nil {
		return
	}
	o := e.G.Obj(cu.source)
	if o == nil || o.Zone != state.ZBattlefield {
		e.finishCumulative()
		return
	}
	announced := cu.pips.fold(cu.amount)
	if cu.action == nil && e.pipAnnounceAsk(cu.player, cu.source, cu.amount, &cu.pips,
		cu.costLabel, chooseCumulative) {
		return
	}
	if cu.action == nil && e.paymentManaAskClass(cu.player, cu.source, announced, cu.windowDone,
		"Activate mana abilities to pay cumulative upkeep", chooseCumulative, paymentCumulativeUpkeep) {
		return
	}
	age := strconv.FormatInt(int64(o.Counter("AGE")), 10)
	var opts []decision.Option
	payable := cu.action != nil && e.cumulativeActionPayable(cu)
	if cu.action == nil {
		payable = announced.Priceable() && e.costPayableClass(cu.player, paymentDescriptor{id: cu.source, class: paymentCumulativeUpkeep, cost: &announced}, pipRider{anyColor: e.payerGrantsIgnoreColor(cu.player, cu.source), anyType: e.payerGrantsIgnoreType(cu.player, cu.source)}, announced)
	}
	if payable {
		opts = append(opts, decision.Option{Index: 0, Kind: "cumulative_pay", Obj: cu.source,
			Label: capitaliseFirst(cu.costLabel) + " per age (" + age + " age counter(s))"})
	}
	opts = append(opts, decision.Option{Index: len(opts), Kind: "cumulative_sac", Obj: cu.source,
		Label: "Sacrifice " + o.Face().Name})
	e.choosing = chooseCumulative
	e.ask(&decision.Decision{Player: cu.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: o.Face().Name + " — cumulative upkeep: pay or sacrifice", Source: cu.source, Options: opts})
}

// cumulativeSacObjects is the Sac arm's candidate walk, shared by every
// cumulative-upkeep-style Sac payment: the upkeep action's payable check and
// its choice ask, and echo's pay-gate and pick ask over the same shim. The
// payment is demanded by a triggered ability (the upkeep or echo trigger,
// CR 702.25a / CR 702.35), so the CantSacrifice cost gate runs with
// costCauseTriggered: an Effect-registered CantSacrifice or a bare
// `ForCost$ True` face static blocks the permanent, a `ValidCause$
// Spell,Activated` carrier (Angel of Jubilation) scopes past it, and a
// `ValidCause$ Triggered` one admits the block (cantsac1 r2).
func (e *Engine) cumulativeSacObjects(cu *cumulativeUpkeep) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.cumulativeObjects(cu, state.ZBattlefield, cu.action.spec) {
		if !e.sacrificeBlockedForCost(id, costCauseTriggered) {
			out = append(out, id)
		}
	}
	return out
}

func (e *Engine) cumulativeObjects(cu *cumulativeUpkeep, zone state.Zone, spec string) []state.ObjID {
	var out []state.ObjID
	players := []state.PlayerID{cu.player}
	if zone == state.ZBattlefield && (strings.Contains(spec, "OppCtrl") || strings.Contains(spec, "YouDontCtrl")) {
		players = e.G.AliveFrom(0)
	}
	if zone == state.ZGraveyard {
		players = e.G.AliveFrom(0)
	}
	for _, p := range players {
		for _, id := range e.G.Zone(zone, p) {
			if e.matchesSpecFrom(spec, id, cu.player, cu.source) {
				out = append(out, id)
			}
		}
	}
	return out
}

func (e *Engine) cumulativeActionPayable(cu *cumulativeUpkeep) bool {
	a := cu.action
	if a == nil || cu.actionRemaining <= 0 {
		return false
	}
	total := int(a.n * cu.actionRemaining)
	switch a.kind {
	case "Sac":
		return len(e.cumulativeSacObjects(cu)) >= total
	case "Discard":
		return len(e.cumulativeObjects(cu, state.ZHand, a.spec)) >= total
	case "Draw", "ExileFromTop":
		return len(e.G.Zone(state.ZLibrary, cu.player)) >= total
	case "AddMana", "FlipCoin":
		return true
	case "AddCounter":
		if !strings.Contains(a.spec, "/") {
			return e.G.Obj(cu.source) != nil
		}
		_, targetSpec, _ := strings.Cut(a.spec, "/")
		return len(e.cumulativeObjects(cu, state.ZBattlefield, targetSpec)) > 0
	case "GainControl":
		return len(e.cumulativeObjects(cu, state.ZBattlefield, a.spec)) >= total
	case "GainLife":
		return len(e.G.AliveFrom(cu.player)) > 1
	case "PutCardToLibFromSameGrave":
		groups := 0
		for _, p := range e.G.AliveFrom(0) {
			groups += len(e.G.Zone(state.ZGraveyard, p)) / int(a.n)
		}
		return groups >= int(cu.actionRemaining)
	}
	return false
}

func (e *Engine) cumulativeObjectDecision(cu *cumulativeUpkeep, ids []state.ObjID, min, max int, kind, prompt string) {
	d := &decision.Decision{Player: cu.player, Kind: decision.KChoose, Min: min, Max: max,
		Prompt: prompt, Source: cu.source}
	for _, id := range ids {
		label := "card"
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			label = o.Face().Name
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: kind, Obj: id, Label: label})
	}
	e.choosing = chooseCumulative
	e.ask(d)
}

func (e *Engine) continueCumulativeAction() {
	cu := e.cumulative
	if cu == nil || cu.action == nil {
		return
	}
	if cu.actionRemaining <= 0 {
		e.finishCumulative()
		return
	}
	a := cu.action
	total := int(a.n * cu.actionRemaining)
	switch a.kind {
	case "Sac":
		e.cumulativeObjectDecision(cu, e.cumulativeSacObjects(cu), total, total,
			"cumulative_action_sac", "Choose permanents to sacrifice for cumulative upkeep")
	case "Discard":
		e.cumulativeObjectDecision(cu, e.cumulativeObjects(cu, state.ZHand, a.spec), total, total,
			"cumulative_action_discard", "Choose cards to discard for cumulative upkeep")
	case "Draw":
		for i := 0; i < total; i++ {
			effects.DrawFor(e, cu.player)
		}
		cu.actionRemaining = 0
		e.finishCumulative()
	case "ExileFromTop":
		for i := 0; i < total; i++ {
			lib := e.G.Zone(state.ZLibrary, cu.player)
			if len(lib) == 0 {
				break
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: lib[0], From: state.ZLibrary, To: state.ZExile})
		}
		cu.actionRemaining = 0
		e.finishCumulative()
	case "AddMana":
		// Producer-type provenance: the upkeep mana is produced by the
		// permanent paying the cumulative cost, so the same
		// Treasure/Cave/Desert/Snow tag effMana stamps rides this ManaAdd's
		// Counter (task ctms). Braid of Fire is an Enchantment, so its
		// corpus shape stays a plain "R"; a typed carrier is tagged.
		counter := a.spec
		if tag, snow := effects.ManaProducerTag(e, cu.source); tag != "" {
			counter = tag + counter
		} else if snow {
			counter = "S" + counter
		}
		e.emit(events.Event{Kind: events.ManaAdd, Player: cu.player, Counter: counter, Amount: int32(total)})
		cu.actionRemaining = 0
		e.finishCumulative()
	case "FlipCoin":
		// The canonical coin-flip result Note effects.FlipCoinNote emits —
		// the ONE encoding the FlippedCoin trigger matcher
		// (rules/trigger_match.go's flippedCoinMatches) reads, so a
		// cost-side flip fires "whenever you win/lose a coin flip" exactly
		// like an effect-side one (Karplusan Minotaur).
		for i := 0; i < total; i++ {
			e.emit(effects.FlipCoinNote(cu.source, cu.player, e.Rand(2) == 0))
		}
		cu.actionRemaining = 0
		e.finishCumulative()
	case "AddCounter":
		counter, targetSpec, targeted := strings.Cut(a.spec, "/")
		if !targeted {
			// A cumulative-upkeep action cost is paid by cu.player, and this
			// window has no usable stack cause for the AddCounter class, so
			// publish the payer as the adder explicitly.
			prevAdder := e.SetCounterAdder(cu.player)
			e.emit(events.Event{Kind: events.CounterChange, Obj: cu.source, Counter: counter, Amount: int32(total)})
			e.SetCounterAdder(prevAdder)
			cu.actionRemaining = 0
			e.finishCumulative()
			return
		}
		e.cumulativeObjectDecision(cu, e.cumulativeObjects(cu, state.ZBattlefield, targetSpec), 1, 1,
			"cumulative_action_counter", "Choose a permanent to receive a "+counter+" counter")
	case "GainControl":
		e.cumulativeObjectDecision(cu, e.cumulativeObjects(cu, state.ZBattlefield, a.spec), total, total,
			"cumulative_action_control", "Choose permanents to gain control of")
	case "GainLife":
		d := &decision.Decision{Player: cu.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose an opponent to gain life", Source: cu.source}
		for _, p := range e.G.AliveFrom(cu.player) {
			if p == cu.player {
				continue
			}
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "cumulative_action_life",
				Player: p, Label: seatFacingName(e.G, p)})
		}
		e.choosing = chooseCumulative
		e.ask(d)
	case "PutCardToLibFromSameGrave":
		var owners []state.PlayerID
		for _, p := range e.G.AliveFrom(0) {
			if len(e.G.Zone(state.ZGraveyard, p)) >= int(a.n) {
				owners = append(owners, p)
			}
		}
		if len(owners) == 1 {
			cu.actionOwner = owners[0]
			e.cumulativeObjectDecision(cu, e.G.Zone(state.ZGraveyard, owners[0]), int(a.n), int(a.n),
				"cumulative_action_grave_card", "Choose cards from one graveyard")
			return
		}
		d := &decision.Decision{Player: cu.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a graveyard", Source: cu.source}
		for _, p := range owners {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "cumulative_action_grave",
				Player: p, Label: seatFacingName(e.G, p) + "'s graveyard"})
		}
		e.choosing = chooseCumulative
		e.ask(d)
	}
}

// triggeredCostDrawCounts resolves every Draw component of a trigger's Cost$
// at the window's source, so the ask can decide whether "pay" is answerable
// and the pay arm can settle the draws without re-deriving them. ok=false
// means a component is unpayable: a drawer spec with no binding in the cast
// flow, or a dynamic Draw<X/Spec> whose source SVar is absent or
// unresolvable (fail closed, the ParseUnlessCost hard-decline convention).
func (e *Engine) triggeredCostDrawCounts(tc *triggeredEffectCost) ([]int32, bool) {
	out := make([]int32, len(tc.amount.Draw))
	// The window parks a TRIGGER body, so a dynamic Draw<X/Spec> part's SVar
	// can name a trigger referent (Hordewing Skaab's
	// TriggeredPlayersTargets$Amount): seed the fire-time capture exactly
	// like evalTriggerCostFixedX does, else the count fails closed and the
	// window offers decline only.
	var tcx *effects.TriggerContext
	if t, ok := e.triggerContexts[tc.resume.obj]; ok {
		tcx = &t
	}
	for i, part := range tc.amount.Draw {
		if _, ok := castFlowDrawPlayer(part.Spec, tc.player); !ok {
			return nil, false
		}
		n, ok := e.drawCostCountTrig(tc.source, tc.player, part, tcx)
		if !ok {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// evalTriggerCostFixedX evaluates a FIXED X body (the source face's SVar:X
// names a resolvable expression OTHER than Count$xPaid -- Tomakul Phoenix's
// Count$CardPower, Tivash's Count$LifeYouGainedThisTurn), with the context
// the body will see -- Source and Controller bound the way fixLifeXCost/
// drawCostCount seed their evaluations, plus the trigger context the body's
// own resolution will carry (the TriggeredCard$/Targeted$ referent family).
// resolvable=false means the body is unresolvable (fail closed, the
// decline-only hard-decline convention, never a silent zero).
func (e *Engine) evalTriggerCostFixedX(tc *triggeredEffectCost, o *state.Object, body string) (int32, bool) {
	ctx := &effects.Ctx{Source: tc.source, Controller: tc.player, SVars: o.Face().SVars}
	if tcx, ok := e.triggerContexts[tc.resume.obj]; ok {
		ctx.TriggerContext = tcx
	}
	n, resolvable := effects.EvalCountOK(e, ctx, body)
	if !resolvable || n < 0 {
		return 0, false
	}
	return n, true
}

// triggeredCostXAsk is the X arm of the payment ask: a cost carrying an
// unfolded {X} or PayLife<X> part needs its X announced BEFORE the mana
// activations that cover it (CR 601.2b's announce-then-activate ordering;
// the ordinary pay/decline election follows the fold). Two shapes:
//
//   - FIXED (SVar:X names a resolvable expression other than Count$xPaid):
//     the value is folded into the cost once, no ask is posed, and the
//     ordinary flow proceeds with the now-Priceable cost -- the corpus's
//     "where X is CARDNAME's power" family. The value binds the body at the
//     pay arm (rp.winPaidX).
//   - PAYER-CHOOSES (SVar:X is Count$xPaid): a real KChoose over
//     the payable values 0..bound in ASCENDING order, so option 0 -- what
//     botpolicy's default arm and a headless host take -- stays the
//     decline-equivalent (X = 0 pays only the cost's own pips). The bound is
//     finite and every offered value is genuinely payable against the seat's
//     potential pool (the floating pool plus every untapped source's
//     production -- the window then opens paymentManaAsk, so the activations
//     that cover X + pips are real): the payer's current life caps a
//     PayLife<X> part, PotentialMana caps a mana {X}.
//
// A face with NO SVar:X, and a face whose SVar:X body is unresolvable (Tymna
// the Weaver's head), both fail closed: no announcement ask, the amount stays
// unfolded, and the ordinary unpriceable path below poses decline only -- the
// no-SVar case is what keeps the synthetic copy-cost shapes (Verrak's
// PayLife<X> copy execute) hard-decline as abcopy1 pinned them. Returns true
// when the announcement ask was posed; the answer re-enters through
// triggeredCostAnswer's trigger_cost_x arm.
func (e *Engine) triggeredCostXAsk(tc *triggeredEffectCost) bool {
	if tc.xDone || costCarriesDynTap(tc.amount) || !costCarriesUnfoldedX(tc.amount) {
		return false
	}
	// Never fold an X into a payment for a body this build cannot run: an
	// unregistered body API (Maralen of the Mornsong Avatar's AB$ StoreSVar,
	// the corpus's one X-fold carrier of the kind) would charge the payer
	// real mana or life and then emit only the unimplemented-API Note -- the
	// decline-only hard-decline below is the honest shape for it.
	if tc.resume == nil || tc.resume.sa == nil || !effects.Supported()["api:"+tc.resume.sa.API] {
		return false
	}
	tc.xDone = true
	o := e.G.Obj(tc.source)
	if o == nil || o.Face() == nil {
		return false
	}
	body, present := o.Face().SVars["X"]
	if !present {
		// No SVar:X at all: nothing fixes the value AND nothing says the payer
		// announces one -- the corpus's every X-fold carrier names an SVar:X
		// (the census), and a synthetic no-SVar shape (Verrak's PayLife<X>
		// copy execute) keeps today's decline-only hard-decline. Fail closed.
		return false
	}
	if strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
		// Payer-chooses by construction: fall through to the ask.
	} else if n, resolvable := e.evalTriggerCostFixedX(tc, o, body); !resolvable {
		// A PRESENT but unresolvable SVar:X body (Tymna the Weaver's
		// head) fails closed: no announcement ask, the amount stays
		// unfolded, and the ordinary unpriceable path below poses the
		// decline-only ask -- the same hard-decline convention the
		// unpayable-cost arm takes, never a silent zero.
		return false
	} else {
		tc.amount = foldCostX(tc.amount, n)
		tc.xPaid = n
		return false
	}
	// Payer-chooses (SVar:X is Count$xPaid): "you may pay {X}" is the
	// payer's announcement (CR 601.2b's announce-then-activate ordering).
	pot := e.PotentialMana(tc.player)
	bound := int32(-1)
	if tc.amount.X > 0 {
		bound = pot.Total()
		if bound > xFoldBoundCap {
			bound = xFoldBoundCap
		}
	}
	for _, part := range tc.amount.LifeX {
		if part.Spec != "X" {
			continue
		}
		if life := e.G.Players[tc.player].Life; bound < 0 || life < bound {
			bound = life
		}
	}
	// A PayEnergy<X> part is bounded by the payer's energy counter total (the
	// same cap the cast path's xAsk applies to an energy X -- Forge
	// CostPayEnergy.getMaxAmountX). A fixed PayEnergy<N> part is charged as
	// its N regardless of the announced X, but it must still be affordable
	// for any offered value, so the total caps the bound too.
	if tc.amount.energyCostX() {
		if energy := e.G.Players[tc.player].Counter("ENERGY"); bound < 0 || energy < bound {
			bound = energy
		}
	}
	if bound < 0 {
		return false
	}
	var opts []decision.Option
	for x := int32(0); x <= bound; x++ {
		v := foldCostX(tc.amount, x)
		if !e.energyPayable(tc.player, v) || !e.costPayablePool(tc.player, tc.source, false, v, pot, e.G.Players[tc.player].ManaUnits()) {
			continue
		}
		opts = append(opts, decision.Option{Index: len(opts), Kind: "trigger_cost_x",
			Label: "X = " + strconv.FormatInt(int64(x), 10), Amount: int(x), Obj: tc.source})
	}
	if len(opts) == 0 {
		// No X value is payable at all: the cost can never be paid. The amount
		// stays unfolded, so the ordinary unpriceable path below poses the
		// decline-only ask -- the same shape an unresolvable fixed body takes.
		return false
	}
	name := "triggered ability"
	if o := e.G.Obj(tc.source); o != nil && o.Face() != nil {
		name = o.Face().Name
	}
	e.choosing = chooseTriggeredCost
	e.ask(&decision.Decision{Player: tc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: name + " — choose a value for X", Source: tc.source, Options: opts})
	return true
}

// pipAnnounce carries the flexible-pip payment announcement (CR 601.2b /
// 107.4e-f) every announcement-pip window shares. idx is the next unsettled
// pip; color accumulates the coloured faces the announced pips chose, life
// the life a Phyrexian face paid with two life, and generic the generic a
// monocolour hybrid pip paid with its generic face. Plain data
// (a value array and scalars), so a window clone copies it without a
// deep-copy rule.
//
// It is embedded by every window that carries a cost with announcement pips
// -- triggeredEffectCost, echoFlow and cumulativeUpkeep -- so the fold, the
// ask and the answer-advance exist once. announcedCost's contract is the
// reason a shared home is required rather than three copies: the payability
// gate, the mana window and the pay arm must all price and charge the SAME
// resolved cost, so all three windows fold through this one method.
type pipAnnounce struct {
	idx     int
	color   state.Mana
	life    int32
	generic int32
}

// fold returns c with every announcement pip -- a two-colour hybrid, a
// monocolour hybrid, a Phyrexian pip or a hybrid-Phyrexian pip, in
// Cost.announcePip order -- replaced by the face the payer elected: the
// chosen colour is folded into Colored, a monocolour hybrid's generic face
// into Generic, a Phyrexian face's two life into Life. It is the window
// counterpart of pendingCast.resolvedMana (the cast flow's one folded-cost
// home), so the payability gate, the mana window and the pay arm all price
// and charge the SAME resolved cost and can never disagree. Before any pip is
// announced the accumulator lists are empty and the four pip lists are still
// present, so a cost with no announcement pip is returned byte-identical to
// c -- every non-hybrid window is unchanged.
func (pa *pipAnnounce) fold(c Cost) Cost {
	m := c
	m.Hybrid = nil
	m.Phyrexian = nil
	m.Twobrid = nil
	m.HybridPhyrexian = nil
	for i := range pa.color {
		m.Colored[i] += pa.color[i]
	}
	m.Generic = addClampedGeneric(m.Generic, int64(pa.generic))
	m.Life = addClampedGeneric(m.Life, int64(pa.life))
	return m
}

// accept consumes one pay_*/pay_life/pay_generic answer, recording the face
// the payer elected and advancing to the next pip. It reports whether the
// option Kind was an announcement answer (false leaves the caller's own arm
// to handle it). The option Kinds are the cast flow's own (castAnswer's
// pay_W family), so every window and the cast flow answer the same way and a
// replay records the same choice shape.
func (pa *pipAnnounce) accept(kind string, amount int) bool {
	switch kind {
	case "pay_W", "pay_U", "pay_B", "pay_R", "pay_G", "pay_C":
		pa.color[state.ManaIndex(kind[len("pay_"):][0])]++
	case "pay_life":
		pa.life += 2
	case "pay_generic":
		pa.generic += int32(amount)
	default:
		return false
	}
	pa.idx++
	return true
}

// pipAnnounceAsk poses the CR 601.2b announcement for the next unsettled
// announcement pip of amount (Cost.annPipCount / Cost.announcePip -- the ONE
// shared alternative source the cast flow's manaAsk also reads, so the faces
// offered here and the faces the cast flow offers can never drift). It runs
// BEFORE the window's mana ask, exactly as manaAsk runs before a cast's
// payment, so the payer fixes each pip's face and the window then prices and
// charges that resolved cost (fold). Every face is offered without a pool
// pre-filter: an announcement is a CR 601.2b choice, not a solvency claim --
// an announced face the pool and untapped sources cannot actually cover ends
// in the decline-only pay ask, never a wedge. It returns true once it has
// asked (and therefore suspended).
func (e *Engine) pipAnnounceAsk(player state.PlayerID, source state.ObjID, amount Cost, pa *pipAnnounce, costLabel string, flow chooseFor) bool {
	if pa.idx >= amount.annPipCount() {
		return false
	}
	alts := amount.announcePip(pa.idx)
	name := "triggered ability"
	if o := e.G.Obj(source); o != nil && o.Face() != nil {
		name = o.Face().Name
	}
	d := &decision.Decision{Player: player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose how to pay a mana symbol of " + name + " — " + costLabel,
		Source: source}
	seen := map[byte]bool{}
	seenGeneric := false
	for _, alt := range alts {
		switch {
		case alt.color != 0:
			if seen[alt.color] {
				continue
			}
			seen[alt.color] = true
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_" + string(alt.color), Label: "Pay " + string(alt.color), Amount: 1})
		case alt.generic > 0:
			if seenGeneric {
				continue
			}
			seenGeneric = true
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_generic", Label: fmt.Sprintf("Pay %d generic", alt.generic), Amount: int(alt.generic)})
		case alt.life > 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_life", Label: "Pay 2 life", Amount: 2})
		}
	}
	if len(d.Options) == 0 {
		// announcePip never returns an empty alternative list (every pip shape
		// has at least one face), so this is unreachable for a real cost; the
		// guard keeps a malformed one from posing an empty decision.
		return false
	}
	e.choosing = flow
	e.ask(d)
	return true
}

// announcedCost is the window's cost folded through its shared pip
// announcement accumulator (see pipAnnounce.fold).
func (tc *triggeredEffectCost) announcedCost() Cost {
	return tc.pips.fold(tc.amount)
}

// triggeredCostPayable reports whether the window's "pay" election is
// answerable: the payer's energy pool covers the cost's energy parts, no
// other unmodelled component rides the cost, and the remaining mana/life (and
// any Draw/Sac/Exile/Discard components) half is chargeable. It is the ONE
// home for the ask gate's payable decision, so the offered answers and the
// pay arm's charge can never disagree (the two energy sites are
// energyPayable here and chargeEnergyCost there). It prices the ANNOUNCED
// cost, so a hybrid pip is chargeable exactly when the face the payer elected
// is (CR 107.4e).
func (e *Engine) triggeredCostPayable(tc *triggeredEffectCost) bool {
	amt := tc.announcedCost()
	if !e.energyPayable(tc.player, amt) {
		return false
	}
	rest := amt.withoutEnergy()
	if len(rest.Sac)+len(rest.Discard)+len(rest.Exile)+len(rest.MoveToGrave) > 0 {
		// A component-bearing cost is payable ONLY through the settle gate
		// (trigcost2): the Draw arm alone would offer "pay" while silently
		// skipping the components.
		return e.triggeredCostComponentsPayable(tc)
	}
	if rest.Priceable() {
		return e.triggeredCostManaHalfPayable(tc, rest)
	}
	if len(rest.Draw) > 0 {
		_, ok := e.triggeredCostDrawCounts(tc)
		return ok && e.triggeredCostManaHalfPayable(tc, rest)
	}
	return false
}

// triggeredCostManaHalfPayable reports whether the pool can actually charge
// the cost's mana/life half RIGHT NOW -- the same costPayableOther pricing
// the pay arm's payManaConv (and the component-settle arm's stripped charge)
// runs, so an offered "pay" and the charge that follows can never disagree.
// A cost half with no mana payment at all is trivially payable. A half the
// pool does not cover keeps the decline-only ask: by the time the pay/decline
// ask is reached, paymentManaAskClass has already offered every untapped
// source and either the pool covers, the payer pressed Done (windowDone --
// the window is closed, no more mana can enter), or no untapped source
// exists -- so pool coverability here is exactly coverability, and an
// unpayable window never offers a "pay" that can only fail at the charge.
func (e *Engine) triggeredCostManaHalfPayable(tc *triggeredEffectCost, rest Cost) bool {
	mana := rest
	mana.Sac, mana.Discard, mana.Exile, mana.Draw, mana.MoveToGrave = nil, nil, nil, nil, nil
	if !mana.hasManaPayment() && mana.Life == 0 && mana.Snow == 0 &&
		len(mana.Hybrid) == 0 && len(mana.Phyrexian) == 0 &&
		len(mana.Twobrid) == 0 && len(mana.HybridPhyrexian) == 0 {
		return true
	}
	return e.costPayableOther(tc.player, tc.source, mana)
}

// triggeredCostComponentsPayable reports whether the window can settle a
// non-Priceable cost's choice-bearing components for real (trigcost2): every
// component is a Sac/Exile/Discard part (no Announced Sac -- the announced
// count has no channel here), no OTHER non-mana component rides the cost,
// no {X}/tap demand needs an announcement the window cannot make, every
// Draw count resolves, every component is payable at ask time, and the
// mana/life half is chargeable from the pool (pool-only: the window never
// opens a mana-activation ask for a component cost, the same shape the
// unless-pay arm keeps). A cost failing any of these keeps the decline-only
// ask -- a half-paid commitment is unreachable, and so is the silent skip
// this gate replaces.
func (e *Engine) triggeredCostComponentsPayable(tc *triggeredEffectCost) bool {
	amt := tc.announcedCost()
	if len(amt.Sac)+len(amt.Discard)+len(amt.Exile)+len(amt.MoveToGrave) == 0 {
		return false
	}
	if amt.Tap || amt.X != 0 {
		return false
	}
	for _, part := range amt.Sac {
		if part.Announced {
			return false
		}
	}
	// Every component outside the settleable set keeps the decline-only ask
	// (PayEnergy, SubCounter, AddCounter, Return, PutToLib, Behold, Reveal,
	// Forage, the dyn-tap election, a dynamic life/counter value) -- never a
	// half-paid commitment (the ParseUnlessCost hard-decline convention).
	// AddCounter is parsed into the Cost (rules/mana.go) but priced FREE for
	// planeswalker activations; no settle reads it here, so it is rejected
	// structurally rather than by population -- the corpus carries no trigger
	// body with one today, and if one lands it must not be silently skipped.
	if len(amt.SubCounter) > 0 || len(amt.Reveal) > 0 || len(amt.RevealChosen) > 0 || len(amt.Behold) > 0 ||
		len(amt.TapPermanent) > 0 || len(amt.Blight) > 0 ||
		len(amt.AddCounter) > 0 || len(amt.Return) > 0 || len(amt.PutToLib) > 0 ||
		len(amt.LifeX) > 0 || len(amt.DamageYou) > 0 || len(amt.Exert) > 0 || amt.Forage {
		return false
	}
	if _, ok := e.triggeredCostDrawCounts(tc); !ok {
		return false
	}
	parts := triggeredMandatoryParts(amt)
	// Reserve candidates ACROSS parts while gating: parts sharing a pool must
	// be payable TOGETHER, not each in isolation -- two Discard parts of one
	// card over a one-card hand passed independently before and offered "pay"
	// that the walk then declined at the second part (an offer that cannot be
	// honoured). discardCostPayable's reservation walk is the model; the
	// settle walk itself reserves the same way through tc's component lists.
	reserved := map[state.ObjID]bool{}
	for i, part := range parts {
		if triggeredPartIsDiscard(amt, i) && strings.EqualFold(part.Spec, "Hand") {
			// The Hand shape pays the whole hand: every card the hand still
			// holds is reserved and the token's count is satisfied whenever
			// the hand holds at least that many -- a Discard<1/Hand> token
			// needs a card; a Discard<0/Hand> one is payable with an empty
			// hand (a free pay-nothing election, disclosed in the round-2
			// report; no corpus carrier is measured for it in a window cost).
			cands := e.triggeredMandatoryCandidatesWith(tc, i, part, reserved)
			if part.N > 0 && int32(len(cands)) < part.N {
				return false
			}
			for _, id := range cands {
				reserved[id] = true
			}
			continue
		}
		if part.N <= 0 {
			// A zero-count ordinary component has no payment to settle; the
			// window declines rather than walking a degenerate ask.
			return false
		}
		eligible := e.triggeredMandatoryCandidatesWith(tc, i, part, reserved)
		if int32(len(eligible)) < part.N {
			return false
		}
		for j := int32(0); j < part.N; j++ {
			reserved[eligible[j]] = true
		}
	}
	mana := amt
	mana.Sac, mana.Discard, mana.Exile, mana.Draw = nil, nil, nil, nil
	mana.MoveToGrave = nil
	if mana.hasManaPayment() || mana.Life > 0 || mana.Snow > 0 ||
		len(mana.Hybrid) > 0 || len(mana.Phyrexian) > 0 ||
		len(mana.Twobrid) > 0 || len(mana.HybridPhyrexian) > 0 {
		if !e.costPayableOther(tc.player, tc.source, mana) {
			return false
		}
	}
	return true
}

// triggeredCostPipAsk poses the CR 601.2b announcement for the next unsettled
// hybrid, monocolour-hybrid, Phyrexian or hybrid-Phyrexian pip of the window's
// cost (Cost.annPipCount / Cost.announcePip -- the ONE shared alternative
// source the cast flow's manaAsk also reads, so the faces offered here and the
// faces the cast flow offers can never drift). It runs BEFORE the mana window,
// exactly as manaAsk runs before a cast's payment, so the payer fixes each
// triggeredCostPipAsk poses the CR 601.2b announcement for the next unsettled
// announcement pip of this window's cost, through the shared pipAnnounceAsk
// (every window that carries an announcement-pip cost uses it, so the faces
// offered here and at the cast flow can never drift). It returns true once it
// has asked (and therefore suspended).
func (e *Engine) triggeredCostPipAsk(tc *triggeredEffectCost) bool {
	return e.pipAnnounceAsk(tc.player, tc.source, tc.amount, &tc.pips, tc.costLabel, chooseTriggeredCost)
}

func (e *Engine) triggeredCostPaymentAsk() {
	tc := e.triggerCost
	if tc == nil {
		return
	}
	// The dynamic tapXType heads (tapXType<X/Spec>, tapXType<Any/Spec>): the
	// tap election IS their payment, so the window poses it directly instead
	// of the pay/decline ask. It opens only when the non-tap rest is an EMPTY
	// payment -- the corpus's dyn-tap trigger costs are all tap-only, and a
	// cost that also charges mana or life alongside the election would
	// otherwise risk a partially paid commitment -- anything else keeps the
	// decline-only ask below (the ParseUnlessCost hard-decline convention).
	// An empty election answer is the decline: tapping nothing does not pay
	// an Any-form cost, and an X-form cost paid with zero taps runs a body
	// scaled only by X at 0 -- the outcome a decline gives without emitting
	// the no-op body's events.
	if costCarriesDynTap(tc.amount) {
		// The X fold is deliberately NOT reached from this branch: a cost
		// carrying BOTH a dynamic tap election and an unfolded X keeps the
		// decline-only ask below (the corpus's dyn-tap trigger costs are all
		// tap-only), exactly as before the X fold existed.
		rest := withoutDynTaps(tc.amount)
		if rest.Priceable() && !rest.hasManaPayment() && rest.Life == 0 && rest.Snow == 0 {
			if e.paymentManaAsk(tc.player, tc.source, rest, tc.windowDone,
				"Activate mana abilities to "+tc.costLabel, chooseTriggeredCost) {
				return
			}
			if part, ok := e.nextDynTapPart(tc); ok {
				e.triggeredTapAsk(tc, part)
			}
			return
		}
		// fall through: the unpayable-cost decline-only ask below
	}
	if e.triggeredCostXAsk(tc) {
		return
	}
	if e.triggeredCostPipAsk(tc) {
		return
	}
	if e.paymentManaAsk(tc.player, tc.source, tc.announcedCost(), tc.windowDone,
		"Activate mana abilities to "+tc.costLabel, chooseTriggeredCost) {
		return
	}
	name := "triggered ability"
	if o := e.G.Obj(tc.source); o != nil && o.Face() != nil {
		name = o.Face().Name
	}
	opts := []decision.Option{{Index: 0, Kind: "trigger_cost_pay", Obj: tc.source, Label: capitaliseFirst(tc.costLabel)},
		{Index: 1, Kind: "trigger_cost_decline", Obj: tc.source, Label: "Do not pay"}}
	// A Draw-bearing cost is Priceable()==false by construction (payMana
	// cannot charge the draw), but it IS payable when every Draw component's
	// count resolves and the mana half -- if any -- is covered. The dynamic
	// Draw<X/Spec> form (Champion of Wits' Cost$ Draw<X/You>) resolves its
	// count from the source's SVar table here, at the window, so an
	// unresolvable body offers decline only. An energy-bearing cost
	// (PayEnergy<N>) is likewise unPriceable but payable when the payer's
	// energy pool covers the parts and the mana/life half is chargeable --
	// the same split the cast path's nonManaCastable makes. The ONE shared
	// gate keeps the ask and the pay arm in lockstep.
	payable := e.triggeredCostPayable(tc)
	if !payable {
		// An unpriceable cost (PayLife<X>, Verrak, Warped Sengir's copy
		// trigger) is a hard decline per the ParseUnlessCost convention, and
		// since the pool-coverability gate (triggeredCostManaHalfPayable) the
		// same decline-only shape covers a Priceable cost the pool cannot
		// charge either (an empty pool over Alesha's WB WB, an X announced
		// larger than the mana that can be produced): the ask is still posed
		// and the decision recorded, but "pay" is not an answerable option --
		// never a free copy through a zero-amount read, never an offered pay
		// that can only fail at the charge. Options are renumbered: an ask's
		// option Index must equal its position.
		opts = []decision.Option{{Index: 0, Kind: "trigger_cost_decline", Obj: tc.source, Label: "Do not pay"}}
	}
	e.choosing = chooseTriggeredCost
	e.ask(&decision.Decision{Player: tc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: name + " — " + tc.costLabel + "?", Source: tc.source, Options: opts})
}

func (e *Engine) cumulativeAnswer(chosen []decision.Option) {
	cu := e.cumulative
	if cu == nil || len(chosen) == 0 {
		return
	}
	e.choosing = chooseNone
	if cu.pips.accept(chosen[0].Kind, chosen[0].Amount) {
		e.cumulativePaymentAsk()
		return
	}
	switch chosen[0].Kind {
	case "activate":
		e.activatePaymentMana(cu.player, chosen[0].Obj)
		return
	case "done":
		cu.windowDone = true
		e.cumulativePaymentAsk()
		return
	case "cumulative_pay":
		if cu.action != nil {
			e.continueCumulativeAction()
			return
		}
		announced := cu.pips.fold(cu.amount)
		if announced.Priceable() &&
			e.payManaCumulative(cu.player, cu.source, announced, e.paymentConv(cu.player, cu.source, false)) {
			e.finishCumulative()
			return
		}
	case "cumulative_action_sac":
		for _, option := range chosen {
			if o := e.G.Obj(option.Obj); o != nil && o.Zone == state.ZBattlefield {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZBattlefield,
					To: state.ZGraveyard, Text: "sacrificed for cumulative upkeep"})
			}
		}
		cu.actionRemaining = 0
		e.finishCumulative()
		return
	case "cumulative_action_discard":
		for _, option := range chosen {
			if o := e.G.Obj(option.Obj); o != nil && o.Zone == state.ZHand && o.Owner == cu.player {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand,
					To: state.ZGraveyard, Text: "discarded for cumulative upkeep"})
			}
		}
		cu.actionRemaining = 0
		e.finishCumulative()
		return
	case "cumulative_action_counter":
		counter, _, _ := strings.Cut(cu.action.spec, "/")
		prevAdder := e.SetCounterAdder(cu.player)
		e.emit(events.Event{Kind: events.CounterChange, Obj: chosen[0].Obj, Counter: counter, Amount: cu.action.n})
		e.SetCounterAdder(prevAdder)
		cu.actionRemaining--
		e.continueCumulativeAction()
		return
	case "cumulative_action_control":
		for _, option := range chosen {
			e.emit(events.Event{Kind: events.ControlChange, Obj: option.Obj, Player: cu.player})
		}
		cu.actionRemaining = 0
		e.finishCumulative()
		return
	case "cumulative_action_life":
		e.emit(events.Event{Kind: events.LifeChange, Player: chosen[0].Player, Amount: cu.action.n})
		cu.actionRemaining--
		e.continueCumulativeAction()
		return
	case "cumulative_action_grave":
		cu.actionOwner = chosen[0].Player
		e.cumulativeObjectDecision(cu, e.G.Zone(state.ZGraveyard, cu.actionOwner), int(cu.action.n), int(cu.action.n),
			"cumulative_action_grave_card", "Choose cards from one graveyard")
		return
	case "cumulative_action_grave_card":
		for _, option := range chosen {
			if o := e.G.Obj(option.Obj); o != nil && o.Zone == state.ZGraveyard && o.Owner == cu.actionOwner {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZGraveyard, To: state.ZLibrary})
			}
		}
		cu.actionRemaining--
		e.continueCumulativeAction()
		return
	}
	if o := e.G.Obj(cu.source); o != nil && o.Zone == state.ZBattlefield {
		e.emit(events.Event{Kind: events.MoveZone, Obj: cu.source, From: state.ZBattlefield,
			To: state.ZGraveyard, Text: "sacrificed for cumulative upkeep"})
	}
	e.finishCumulative()
}

func (e *Engine) finishCumulative() {
	cu := e.cumulative
	e.cumulative = nil
	e.choosing = chooseNone
	if cu == nil {
		return
	}
	e.finishResumption(cu.stackObj)
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
}

func (e *Engine) triggeredCostAnswer(chosen []decision.Option) {
	tc := e.triggerCost
	if tc == nil {
		return
	}
	e.choosing = chooseNone
	if len(chosen) == 0 {
		// The empty answer of the Min-0 tap election (the dynamic tapXType
		// window) is the decline: tapping nothing pays nothing. A Min-0 KChoose
		// elsewhere (Dig's take-none) answers the same way, and the guard this
		// branch replaces would have dropped the answer and wedged the window.
		e.triggeredCostDecline(tc)
		return
	}
	switch chosen[0].Kind {
	case "activate":
		e.activatePaymentMana(tc.player, chosen[0].Obj)
		return
	case "done":
		tc.windowDone = true
		e.triggeredCostPaymentAsk()
		return
	case "trigger_cost_tap":
		e.triggeredTapAnswer(tc, chosen)
		return
	case "trigger_cost_x":
		// The X announcement's answer (the payer-chooses fold): the chosen
		// value folds into the cost and the payment ask re-opens, now pricing
		// the folded cost -- CR 601.2b's announce-then-activate ordering. The
		// answer cannot name an unoffered value (Decision.Validate clamps to
		// the offered indices); a chosen value the settle then cannot actually
		// pay ends in the decline at the pay arm, never a wedge.
		x := int32(0)
		if len(chosen) > 0 {
			x = int32(chosen[0].Amount)
		}
		tc.amount = foldCostX(tc.amount, x)
		tc.xPaid = x
		e.triggeredCostPaymentAsk()
		return
	case "pay_W", "pay_U", "pay_B", "pay_R", "pay_G", "pay_C", "pay_life", "pay_generic":
		// A flexible-pip announcement answer (the cast flow's own pay_W
		// family): record the face the payer elected and advance to the next
		// pip, then re-open the payment ask. A hybrid, twobrid or Phyrexian
		// pip paid with pool mana, a Phyrexian face paid with two life, and a
		// monocolour hybrid's generic face all share the one accumulator
		// (pipAnnounce.accept), so this window, Echo and cumulative upkeep
		// answer identically and a replay records the same choice shape.
		var amount int
		if len(chosen) > 0 {
			amount = chosen[0].Amount
		}
		tc.pips.accept(chosen[0].Kind, amount)
		e.triggeredCostPaymentAsk()
		return
	}
	paid := false
	if chosen[0].Kind == "trigger_cost_pay" {
		// The announced cost: every hybrid/Phyrexian pip is folded to the face
		// the payer elected, so the mana half the gates priced and the pay arm
		// charges is exactly the cost triggeredCostPayable proved. A cost with
		// no announcement pip returns amount unchanged.
		announced := tc.announcedCost()
		if len(announced.Sac)+len(announced.Discard)+len(announced.Exile)+len(announced.MoveToGrave) > 0 {
			// The settleable-component cost (trigcost2): the election is
			// "pay"; walk the components (the mandatory walk's picks -- a
			// real KChoose where a choice exists, the Hand/Random discard
			// specials without one) and settle. The walk ends in
			// settleTriggeredMandatory, which charges the stripped mana half,
			// emits the component events, executes the draws and resumes the
			// body. The gate offered "pay" only when every part was payable,
			// and the window is synchronous -- no priority pass runs between
			// its asks -- so a part the walk finds unpayable is the decline,
			// with nothing yet moved to un-pay.
			e.advanceTriggeredMandatory(tc)
			return
		}
		if len(announced.Draw) > 0 {
			// The Draw-bearing cost: resolve every count (source SVar for the
			// dynamic form), charge the mana half, then draw. The window's ask
			// offered "pay" only when this resolution succeeds, so the pay
			// answer is never a partial payment. Any energy part is charged by
			// the shared helper beside the mana (payMana ignores Energy).
			draws, ok := e.triggeredCostDrawCounts(tc)
			if ok && e.payManaConv(tc.player, announced.withoutEnergy(), e.paymentConv(tc.player, tc.source, false)) {
				paid = true
				e.chargeEnergyCost(tc.player, tc.amount, tc.xPaid)
				for i, part := range announced.Draw {
					if drawer, ok := castFlowDrawPlayer(part.Spec, tc.player); ok {
						for n := int32(0); n < draws[i]; n++ {
							e.drawCostCard(drawer)
						}
					}
				}
			}
		} else {
			// The mana/life half is charged by payManaConv; any energy part is
			// charged by the shared chargeEnergyCost helper (payMana ignores
			// Energy). The gate offered "pay" only when both halves were
			// covered, so a paid answer is never a partial payment.
			lower := announced.withoutEnergy()
			paid = lower.Priceable() &&
				e.payManaConv(tc.player, lower, e.paymentConv(tc.player, tc.source, false))
			if paid {
				e.chargeEnergyCost(tc.player, tc.amount, tc.xPaid)
			}
		}
	}
	rp := tc.resume
	e.triggerCost = nil
	if paid {
		// The X fold's binding: the announced (or fixed) value rides the
		// frame into the body, the same channel the dyn-tap election's count
		// takes (rp.tapPaidX). The body's Count$xPaid then reads THIS
		// payment, not the source permanent's cast-time X.
		if tc.xPaid != 0 {
			rp.winPaidX = tc.xPaid
		}
		rp.kind = "effect_paid"
		e.resumeResolution(rp, nil)
		return
	}
	e.triggeredCostDecline(tc)
}

// triggeredCostDecline completes a declined window: the body never runs, and
// the continuation is the outer frame when one is parked or the plain
// resolution completion (the tail the decline option's answer has always
// taken).
func (e *Engine) triggeredCostDecline(tc *triggeredEffectCost) {
	rp := tc.resume
	e.triggerCost = nil
	if rp.outer != nil {
		e.resumeResolution(rp.outer, nil)
		return
	}
	e.finishResumption(rp.obj)
	e.emit(events.Event{Kind: events.Priority, Player: e.G.Active})
}

// triggeredMandatoryParts is the flat, ordered list of choice-bearing
// components a settle walks: every Sac part, then every Exile part, then
// every ExiledMoveToGrave part, then every Discard part. The order is
// deterministic (the cost grammar's own order, extended with Discard last)
// so a replay settles the same picks in the same sequence. The mandatory
// walk only ever carries Sac/Exile (mandatorySettleShape); the Discard and
// MoveToGrave ranges serve the optional window's pay arm, which walks the
// same list after the pay election (trigcost2).
func triggeredMandatoryParts(c Cost) []CostPart {
	parts := make([]CostPart, 0, len(c.Sac)+len(c.Exile)+len(c.MoveToGrave)+len(c.Discard))
	parts = append(parts, c.Sac...)
	parts = append(parts, c.Exile...)
	parts = append(parts, c.MoveToGrave...)
	parts = append(parts, c.Discard...)
	return parts
}

// triggeredMandatoryZone is the zone a mandatory component's candidates
// come from. A Sac part is always the battlefield; an Exile part carries its
// own Zone (ZBattlefield for the bare Exile<N/Spec> token -- Dalek
// Intensive Care's shape -- ZHand/ZGraveyard for the ExileFrom* heads), with
// the zero value defaulting to hand, exactly as exAsk reads it.
func triggeredMandatoryZone(part CostPart, isSac bool) state.Zone {
	if isSac {
		return state.ZBattlefield
	}
	if part.Zone == 0 {
		return state.ZHand
	}
	return part.Zone
}

// triggeredMandatoryCandidates returns the still-available objects that can
// pay one component: the battlefield permanents a Sac part names (never one
// a CantSacrifice restriction blocks, and never a source that has left the
// battlefield), the zone's cards an Exile part names, or the hand cards a
// Discard part names (through the cast flow's discardCandidates, so the
// Hand all-candidates and Random seeded-selection specials read identically
// there), deduped against every component already settled so one object
// cannot pay twice.
func (e *Engine) triggeredMandatoryCandidates(tc *triggeredEffectCost, idx int, part CostPart) []state.ObjID {
	return e.triggeredMandatoryCandidatesWith(tc, idx, part, nil)
}

// triggeredMandatoryCandidatesWith is triggeredMandatoryCandidates with an
// EXTRA reservation set merged in beside the window's own component lists --
// the offer-side gate uses it to reserve candidates ACROSS parts (two parts
// over one shared pool must be payable together, not each in isolation),
// while the settle walk passes nil and reserves through tc's lists alone.
func (e *Engine) triggeredMandatoryCandidatesWith(tc *triggeredEffectCost, idx int, part CostPart, extra map[state.ObjID]bool) []state.ObjID {
	isSac := idx < len(tc.amount.Sac)
	used := make(map[state.ObjID]bool, len(tc.sacs)+len(tc.exiles)+len(tc.discards))
	for _, id := range tc.sacs {
		used[id] = true
	}
	for _, id := range tc.exiles {
		used[id] = true
	}
	for _, id := range tc.discards {
		used[id] = true
	}
	for _, id := range tc.moveGraves {
		used[id] = true
	}
	for id := range extra {
		used[id] = true
	}
	if triggeredPartIsDiscard(tc.amount, idx) {
		return e.discardCandidates(tc.player, tc.source, part, false, used)
	}
	if triggeredPartIsMoveToGrave(tc.amount, idx) {
		// An ExiledMoveToGrave part's candidates come from EVERY player's
		// exile zone (exiled cards live in their OWNER's exile zone --
		// events/apply.go's zoneOwner), not tc.player's own.
		return e.moveToGraveCandidates(tc.player, tc.source, part.Spec, used)
	}
	zone := triggeredMandatoryZone(part, isSac)
	spec := part.Spec
	if isSac {
		// NICKNAME is the same bare self-reference as CARDNAME.
		spec = sacrificeMatchSpec(spec)
	}
	// exg1: the window's trigger context rides the spec evaluation, so a
	// cost spec naming a trigger referent (Card.TriggeredNewCard -- the
	// "you may exile it" family) resolves the card the triggering event
	// captured. A zero context is the filter's fail-closed default.
	sc := e.withNames(effects.SpecContext{You: tc.player, Source: tc.source, TriggerContext: tc.trig})
	var out []state.ObjID
	for _, id := range e.G.Zone(zone, tc.player) {
		if used[id] {
			continue
		}
		if isSac && e.sacrificeBlockedForCost(id, costCauseTriggered) {
			// The window's cost components are demanded by the RESOLVING
			// triggered ability (a `Cost$ Mandatory Sac<...>` body, TrigsMand1),
			// so the cause is costCauseTriggered (cantsac1 r2).
			continue
		}
		if e.matchesSpec(spec, id, sc) {
			out = append(out, id)
		}
	}
	return out
}

// triggeredPartIsDiscard reports whether the component at flat index idx of
// c's triggeredMandatoryParts list is a Discard part (the list's third
// range, after Sac and Exile).
func triggeredPartIsDiscard(c Cost, idx int) bool {
	return idx >= len(c.Sac)+len(c.Exile)+len(c.MoveToGrave)
}

// triggeredPartIsMoveToGrave reports whether the component at flat index idx
// of c's triggeredMandatoryParts list is an ExiledMoveToGrave part (the
// list's third range, after Sac and Exile).
func triggeredPartIsMoveToGrave(c Cost, idx int) bool {
	return idx >= len(c.Sac)+len(c.Exile) && idx < len(c.Sac)+len(c.Exile)+len(c.MoveToGrave)
}

// recordTriggeredMandatoryPick appends one settled component's picks to the
// window's reservation lists.
func (tc *triggeredEffectCost) recordMandatoryPick(idx int, ids []state.ObjID) {
	switch {
	case idx < len(tc.amount.Sac):
		tc.sacs = append(tc.sacs, ids...)
	case idx < len(tc.amount.Sac)+len(tc.amount.Exile):
		tc.exiles = append(tc.exiles, ids...)
	case idx < len(tc.amount.Sac)+len(tc.amount.Exile)+len(tc.amount.MoveToGrave):
		tc.moveGraves = append(tc.moveGraves, ids...)
	default:
		tc.discards = append(tc.discards, ids...)
	}
}

// advanceTriggeredMandatory settles every component of a cost in order: a
// component with an exact-candidate count records its picks without a
// decision (a decision nobody could answer differently is never emitted),
// a component with a genuine choice poses a real KChoose, and a component
// that cannot be paid at all skips the parked body. No pay/decline election
// is ever posed HERE -- for a mandatory cost there is no "may" (the window's
// pay election, where one exists, was answered before this walk started),
// and a Hand-spec Discard pays the whole hand and a Random-spec Discard pays
// seeded picks without ever asking (the cast flow's discardAsk specials).
func (e *Engine) advanceTriggeredMandatory(tc *triggeredEffectCost) {
	parts := triggeredMandatoryParts(tc.amount)
	for tc.part < len(parts) {
		part := parts[tc.part]
		isSac := tc.part < len(tc.amount.Sac)
		isDiscard := triggeredPartIsDiscard(tc.amount, tc.part)
		eligible := e.triggeredMandatoryCandidates(tc, tc.part, part)
		if isDiscard && strings.EqualFold(part.Spec, "Hand") {
			// Forge's "discard your hand" shape: every hand card pays,
			// whatever count the token spells (the corpus writes the ignored
			// count as both 0 and 1).
			tc.recordMandatoryPick(tc.part, eligible)
			tc.part++
			continue
		}
		if isDiscard && strings.EqualFold(part.Spec, "Random") {
			// A Random discard is a selection method, never a choice: the
			// seeded picks the cast flow's discardAsk takes.
			picks := make([]state.ObjID, 0, part.N)
			for i := int32(0); i < part.N && len(eligible) > 0; i++ {
				pick := e.Rand(len(eligible))
				picks = append(picks, eligible[pick])
				eligible = append(eligible[:pick], eligible[pick+1:]...)
			}
			tc.recordMandatoryPick(tc.part, picks)
			tc.part++
			continue
		}
		if int32(len(eligible)) < part.N {
			// A component that cannot be fully paid skips the body; nothing
			// has moved yet (the settle emits every event only at the end),
			// so the skip leaves the board untouched.
			e.triggeredCostDecline(tc)
			return
		}
		if int32(len(eligible)) == part.N {
			tc.recordMandatoryPick(tc.part, eligible)
			tc.part++
			continue
		}
		name := "triggered ability"
		if o := e.G.Obj(tc.source); o != nil && o.Face() != nil {
			name = o.Face().Name
		}
		kind, noun := "sacrifice", "permanent(s)"
		switch {
		case isDiscard:
			kind, noun = "discard", "card(s)"
		case triggeredPartIsMoveToGrave(tc.amount, tc.part):
			kind, noun = "graveyard_cost", "card(s)"
		case !isSac:
			kind, noun = "exile_cost", "card(s)"
		}
		d := &decision.Decision{Player: tc.player, Kind: decision.KChoose,
			Min: int(part.N), Max: int(part.N), Source: tc.source,
			Prompt: name + " — choose " + strconv.FormatInt(int64(part.N), 10) + " " + noun + " to " + kind}
		for _, id := range eligible {
			label := e.targetName(id)
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: kind, Obj: id, Label: label})
		}
		e.choosing = chooseTriggeredMandatory
		e.ask(d)
		return
	}
	e.settleTriggeredMandatory(tc)
}

// triggeredMandatoryAnswer validates one answered pick against the current
// board and advances the walk (the unless-pay re-validation discipline).
func (e *Engine) triggeredMandatoryAnswer(chosen []decision.Option) {
	tc := e.triggerCost
	if tc == nil {
		return
	}
	e.choosing = chooseNone
	parts := triggeredMandatoryParts(tc.amount)
	if tc.part >= len(parts) {
		return
	}
	part := parts[tc.part]
	ids := make([]state.ObjID, 0, len(chosen))
	for _, o := range chosen {
		if o.Obj != 0 {
			ids = append(ids, o.Obj)
		}
	}
	eligible := e.triggeredMandatoryCandidates(tc, tc.part, part)
	allowed := make(map[state.ObjID]bool, len(eligible))
	for _, id := range eligible {
		allowed[id] = true
	}
	if int32(len(ids)) != part.N {
		e.triggeredCostDecline(tc)
		return
	}
	for _, id := range ids {
		if !allowed[id] {
			e.triggeredCostDecline(tc)
			return
		}
	}
	tc.recordMandatoryPick(tc.part, ids)
	tc.part++
	e.advanceTriggeredMandatory(tc)
}

// settleTriggeredMandatory finishes a fully settled cost: the mana half (if
// any) is charged, each picked object leaves its zone by the correct event
// (events.Sacrifice for a Sac part, a MoveZone to exile for an Exile part,
// events.DiscardCost for a Discard part -- the cast flow's provenance
// marker, so discard triggers and the CardsDiscardedThisTurn count see cost
// discards), the cost's own Draw components draw (the pay arm's draw half),
// and the parked body resumes. The mana charge is a totality guard -- the
// gates armed this window only when the mana half was chargeable, but a
// board that changed under the walk could in principle leave it uncovered,
// in which case the body is skipped rather than half paid.
func (e *Engine) settleTriggeredMandatory(tc *triggeredEffectCost) {
	stripped := tc.announcedCost()
	stripped.Sac, stripped.Discard, stripped.Exile, stripped.Draw = nil, nil, nil, nil
	stripped.MoveToGrave = nil
	if (stripped.hasManaPayment() || stripped.Life > 0) &&
		!e.payManaConv(tc.player, stripped.withoutEnergy(), e.paymentConv(tc.player, tc.source, false)) {
		e.triggeredCostDecline(tc)
		return
	}
	// The energy half of the component cost (payMana ignores Energy parts):
	// the gate already confirmed the payer covers it (triggeredCostPayable).
	e.chargeEnergyCost(tc.player, tc.amount, tc.xPaid)
	rp := tc.resume
	e.triggerCost = nil
	e.choosing = chooseNone
	for _, id := range tc.sacs {
		if o := e.G.Obj(id); o != nil && o.Zone == state.ZBattlefield {
			e.emit(events.Sacrifice(id))
		}
	}
	for _, id := range tc.exiles {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile, Text: "exiled as a cost"})
		}
	}
	for _, id := range tc.moveGraves {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZGraveyard, Text: "moved to its owner's graveyard as a cost"})
		}
	}
	e.payDiscardCost(tc.discards, "")
	// The cost's own Draw components (the pay arm's draw half -- Ambergris'
	// "discard your hand. If you do, draw two cards"). The pay gate resolved
	// every count before offering "pay", so the belt-and-braces re-resolution
	// here failing is the decline: nothing to un-pay, the component events
	// above are the payment and the body never runs.
	if len(tc.amount.Draw) > 0 {
		draws, ok := e.triggeredCostDrawCounts(tc)
		if ok {
			for i, part := range tc.amount.Draw {
				if drawer, hasDrawer := castFlowDrawPlayer(part.Spec, tc.player); hasDrawer {
					for n := int32(0); n < draws[i]; n++ {
						e.drawCostCard(drawer)
					}
				}
			}
		}
		if !ok {
			e.triggeredCostDecline(tc)
			return
		}
	}
	rp.kind = "effect_paid"
	e.resumeResolution(rp, nil)
}

// nextDynTapPart returns the next unsettled dynamic tapXType part of the
// window's cost.
func (e *Engine) nextDynTapPart(tc *triggeredEffectCost) (CostPart, bool) {
	dyn := dynTapParts(tc.amount)
	if tc.tapIdx >= len(dyn) {
		return CostPart{}, false
	}
	return dyn[tc.tapIdx], true
}

// triggeredTapAsk poses one dynamic tapXType part's tap election: a KChoose
// over the untapped permanents matching the part's spec, Min 0 (the empty
// answer is the decline), Max the candidate count. An X-form part's answered
// count is the cost's announced {X} (rules/mana.go's dynTapCost doc); the
// body the window then runs reads it through the resume point (rp.tapPaidX).
func (e *Engine) triggeredTapAsk(tc *triggeredEffectCost, part CostPart) {
	candidates := e.costCandidates(tc.player, tc.source, state.ZBattlefield, part.Spec, false, true)
	if len(candidates) == 0 {
		// No eligible permanent: an X-form election could only announce 0 and
		// an Any-form part is not payable at all -- both are the decline, and
		// a decision nobody could answer differently is never emitted (the
		// strict-supersets convention).
		e.triggeredCostDecline(tc)
		return
	}
	name := "triggered ability"
	if o := e.G.Obj(tc.source); o != nil && o.Face() != nil {
		name = o.Face().Name
	}
	d := &decision.Decision{Player: tc.player, Kind: decision.KChoose, Min: 0, Max: len(candidates),
		Prompt: name + " — " + tc.costLabel, Source: tc.source}
	for _, id := range candidates {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "trigger_cost_tap", Obj: id, Label: e.targetName(id)})
	}
	e.choosing = chooseTriggeredCost
	e.ask(d)
}

// triggeredTapAnswer settles one tap election: the chosen permanents tap
// (the Tap events every other cost tap takes), and when every dynamic part
// of the cost has settled the window's payment is complete and the parked
// body resumes with the X-form count bound. The window is synchronous -- no
// priority pass runs between its asks -- so the chosen objects cannot have
// moved; the zone guard is the same read the cumulative actions take.
func (e *Engine) triggeredTapAnswer(tc *triggeredEffectCost, chosen []decision.Option) {
	dyn := dynTapParts(tc.amount)
	idx := tc.tapIdx
	if idx >= len(dyn) {
		return
	}
	part := dyn[idx]
	for _, o := range chosen {
		if ob := e.G.Obj(o.Obj); ob != nil && ob.Zone == state.ZBattlefield {
			e.emit(events.Event{Kind: events.Tap, Obj: o.Obj, Text: "tapped as a cost"})
		}
	}
	rp := tc.resume
	paidX := int32(0)
	if part.Dyn == "X" {
		paidX = int32(len(chosen))
	}
	tc.tapIdx++
	if tc.tapIdx < len(dyn) {
		e.triggeredCostPaymentAsk()
		return
	}
	e.triggerCost = nil
	if paidX != 0 {
		rp.tapPaidX = paidX
	}
	rp.kind = "effect_paid"
	e.resumeResolution(rp, nil)
}

func init() {
	effects.RegisterNonAPI("kw:Cumulative upkeep", "stat:UntapOtherPlayer")
}
