// Task 21: combat. CR 508-510 in miniature -- declare attackers, declare
// blockers, then damage -- plus the CR 514.2 cleanup that combat depends on
// and that nothing in this codebase implemented before now. The lethal-
// damage and Deathtouch state-based actions this file's own damage marking
// depends on were also added here by Task 21, as destroyLethalDamage, but
// moved to sba.go by Task 22 -- they are general state-based-action logic,
// not combat-specific, and now live alongside the rest of CR 704.
//
// M1's own simplifications, matching the brief this task was built from:
//   - Only the active player attacks. The defending player is a real, per-
//     attacker choice since Task m34: each attacking creature may be declared
//     against ANY one living opponent, independently (CR 506.2 / CR 903.14),
//     and the KAttackers decision offers one option per (attacker, defender)
//     pair -- decision.Option.Player carries the pair's defender, so widening
//     M1's single-fixed-defender simplification into the true choice was
//     additive, exactly as this comment always promised. The engine rejects
//     an intent that declares one creature against two defenders
//     (validateAttackers), and with a single opponent the pair list is one
//     option per attacker at that one defender, so a two-player game
//     observes exactly the M1 surface.
//   - Players receive priority after attackers and blockers are declared.
//     Combat damage remains an automatic step.
//   - An ordinary blocking creature may block only one attacker (CR 509.1a).
//     askBlockers still offers every individually legal (blocker, attacker)
//     pair, while validateBlockers rejects a declaration that chooses two
//     pairs for the same blocker. The build does not model any keyword or
//     capability that grants additional blocks; validateBlockers must account
//     for such a capability if one is added.
package rules

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// askAttackers builds a KAttackers decision, one option per (attacker,
// defender) pair: every creature passing combat.CanAttack, offered once against
// every living opponent of the active player (CR 506.2: each attacking
// creature's controller announces which opponent it is attacking, one
// independent choice per creature). Task m34 replaces M1's single fixed
// defender (the next living seat) with this real choice; each option's
// Player field carries its own defender, and handleAttackers groups the
// chosen options back into one DeclareAttackers event per defender. Options
// are ordered defender-major: for each living opponent in ascending seat
// order, for each legal attacker in battlefield order -- deterministic, and
// the same order the declare-blockers step asks those defenders in.
//
// A creature may be declared attacking at most one opponent (CR 506.2), but
// the pair list offers it once per defender: an intent that names the same
// creature against two defenders is REJECTED by the engine's
// validateAttackers guard, never resolved by the engine picking one defender
// for a client that could not decide.
//
// With no possible attacker, there is nothing this decision could change, so
// record the forced empty declaration without asking. The declaration still
// leaves the engine in this step for CR 508.2 priority; after that round CR
// 508.8 skips blockers and combat damage.
func (e *Engine) askAttackers() {
	p := e.G.Active
	var attackers []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if combat.CanAttack(asBoard(e), id) {
			attackers = append(attackers, id)
		}
	}
	if len(attackers) == 0 {
		// Player is the defending player on this event shape. There is no
		// actual defender for an empty declaration, but using the next living
		// seat keeps the marker valid without falsely recording that an
		// eliminated active player attacked.
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: e.G.NextAlive(p)})
		return
	}
	// Defenders are enumerated from seat 0 ascending, not from the active
	// player -- deterministic, which the replay chain requires, and the same
	// order declare-blockers asks them in.
	//
	// A POLICY MUST NOT BREAK TIES ON THIS ORDER. Because the enumeration
	// starts at seat 0 for every attacker at the table, a bot that prefers
	// the earliest option (or the lowest Option.Player) among equally-scored
	// defenders sends the whole table's attacks at the lowest-numbered living
	// seat, which biases the whole table's aggression toward low seats.
	//
	// Measured, four seats, 800 games (four deck->seat rotations x 200, so
	// deck strength is rotated out): defender totals 6170 / 5898 / 5038 /
	// 4835, a 1.28x gradient toward the low seats -- real, but mild, because
	// the tier ranking dominates and the spread survives. Seat win totals
	// over the same 800 are 115 / 183 / 247 / 255: seat 0 takes 14.4%
	// against a fair 25%, the minimum cell for all four decks (sign test
	// P ~ 0.004).
	//
	// Do NOT read a single rotation's win spread as this effect. At one
	// fixed assignment the table reads 37 / 2 / 102 / 23, and that shape is
	// deck strength, not targeting: seat 1 wins 2/200 holding
	// keen-engineering and 91/200 holding reign-of-dragons. Rotate before
	// concluding anything about a seat.
	//
	// Seat 0's win deficit measured here was partly turn order: before the
	// CR 103.1 toss (rules/engine.go New) seat 0 was ALWAYS the starting
	// player, so this tiebreak and the first-turn advantage both pointed the
	// same way and were confounded. The toss removes the confound -- the
	// starting seat is now uniform -- so the residual seat-0 deficit, if any
	// survives a re-measurement, is this tiebreak's alone. The 800-game
	// numbers above have NOT been re-measured since the toss.
	//
	// The engine's order is not the defect -- it has to be deterministic and
	// it has to match declare-blockers -- but it is what a positional
	// tiebreak turns into a bias, so a defender preference belongs on a game
	// fact (life, clock, board) rather than on seat index. attackOffers
	// (rules/attack_cost.go) preserves exactly this enumeration.
	// The CR 508.1d requirements are told to the seat on the options: an
	// attacker the declaration MUST include (a goaded creature, CR 701.38, an
	// encore token, or one under an unconditional or named MustAttack static)
	// carries Option.Required, so a rules-ignorant client can build a legal
	// declaration without re-deriving goad state from the log. attackOffers
	// has already dropped every pair that satisfies fewer named requirements
	// than the creature's best defender, so marking all of a required
	// creature's SURVIVING pairs Required cannot mislead: each one is a
	// maximal-satisfaction pair. The engine rejects an omission
	// (validateAttackDeclaration), so an unmarked list is a trap the seat
	// cannot reason its way out of.
	mustAtt := make(map[state.ObjID]bool, len(attackers))
	for _, id := range attackers {
		if e.mustAttackRequired(id) {
			mustAtt[id] = true
		}
	}
	// The option list IS the attackOffers list (rules/attack_cost.go): the
	// same enumeration and order the pre-prop list always had, plus the
	// per-pair price. A chargeable pair is admitted when its individual price
	// fits the payer's budget; the TOTAL is published to the client through
	// the same cumulative-budget wire contract a Dig's WithTotalCMC$ cap uses
	// -- Decision.MaxSum over each option's Value -- so a rules-ignorant
	// client cannot assemble an over-budget declaration (Decision.Validate
	// enforces it). mustAttackRequired and validateAttackDeclaration read the
	// same list and the same budget.
	offers := e.attackOffers()
	// A creature that is declared as attacking cannot also be tapped for
	// mana, so a declaration including a mana source gives up that source's
	// production. The published per-option budget cost therefore folds the
	// option's own production into its Value whenever the declaration carries
	// a mana tax (attackTaxed); the engine's own whole-declaration check and
	// every client repair sum the same folded Values against the same
	// MaxSum, so a declaration is admitted exactly when its remaining sources
	// can pay the tax. Without any mana tax every Value stays 0, so an
	// ordinary prop-free declaration serialises byte-identically. See
	// attackSourceUnits / attackTaxed in rules/attack_cost.go.
	// Without a mana tax every Value is its pair's zero mana price, so
	// neither the budget nor the per-source units can reach an option or the
	// MaxSum below: both are pure reads, taken only under a tax.
	taxed := e.attackTaxed(offers)
	budget, selfUnits := e.attackBudgetUnits(p, taxed)
	// The declaration-dependent tap-candidate pool (attackTapPool): published
	// only when a tapXType obligation is offered and every such obligation
	// shares one readable shape, so each option can carry the pool share it
	// would consume (Option.TapPoolCost) and Decision.Validate can reject a
	// declaration that leaves the obligation unpayable. Unpublished (0, nil)
	// for every ordinary declaration, keeping its wire payload byte-identical.
	tapPool := 0
	var tapCosts map[state.ObjID]int
	if pool, costs, ok := e.attackTapPool(p, offers); ok {
		tapPool, tapCosts = pool, costs
	}
	// Every offer becomes exactly one option: size the list once instead of
	// regrowing a 392-byte element slice through every doubling.
	opts := make([]decision.Option, 0, len(offers))
	// groupLimits carries a raised per-defender attacker cap to the wire
	// (Decision.GroupLimits): a scoped AttackRestrict ceiling above one
	// (Crawlspace's "no more than two creatures can attack you") cannot be
	// expressed by the decision's single Max nor by the default
	// at-most-one-per-Group rule, so each capped defender's Group names its
	// own ceiling. Built from the same combat.AttackRestrictLimit read the engine's
	// declaration check uses.
	groupLimits := map[string]int{}
	for _, of := range offers {
		label := "Attack with " + e.G.Obj(of.id).Face().Name + " at " + seatFacingName(e.G, of.def)
		if of.battle != 0 {
			if b := e.G.Obj(of.battle); b != nil && b.Face() != nil {
				if b.Face().IsBattle() {
					// A battle: name it instead of the protector seat (main).
					label = "Attack with " + e.G.Obj(of.id).Face().Name + " at " + b.Face().Name
				} else {
					// A planeswalker: name it alongside the controller seat.
					label += " (planeswalker: " + b.Face().Name + ")"
				}
			}
		}
		if of.charge.mana > 0 {
			label += fmt.Sprintf(" (pay {%d} per creature)", of.charge.mana)
		}
		if of.charge.life > 0 {
			label += fmt.Sprintf(", pay %d life", of.charge.life)
		}
		for _, t := range of.charge.taps {
			label += fmt.Sprintf(", tap %d", t.n)
		}
		for range of.charge.sacs {
			label += ", sacrifice a permanent"
		}
		for range of.charge.returns {
			label += ", return a permanent"
		}
		for range of.charge.phyrexian {
			label += ", pay a Phyrexian symbol"
		}
		group, cap := e.attackRestrictGroup(of.def)
		if group != "" && cap > 1 {
			groupLimits[group] = cap
		}
		opt := decision.Option{Index: len(opts), Kind: "attacker",
			Label: label, Obj: of.id, Player: of.def, Battle: of.battle, Required: mustAtt[of.id], Group: group,
			// Value is the pair's cumulative-budget cost: its mana price, plus
			// -- when the declaration carries a mana tax -- the mana units its
			// own Obj would otherwise contribute to the budget (a declared
			// attacker cannot also tap for mana). A rules-ignorant client sums
			// this against MaxSum without knowing what a mana source is.
			// omitempty keeps a prop-free list byte-identical (price 0 and no
			// tax omits), so the option enumeration order and the wire payload
			// of every ordinary declaration are unchanged.
			Value: int(e.attackOptionBudgetValue(of.charge, of.id, selfUnits, taxed))}
		// The non-mana components ride the same fields a block option uses,
		// so a rules-ignorant client can reason about the whole charge.
		opt.CostLife = int(of.charge.life)
		opt.CostPhyrexian = len(of.charge.phyrexian)
		for _, t := range of.charge.taps {
			opt.CostTaps += int(t.n)
		}
		// The pool share this attacker would consume if declared, published
		// with Decision.ChargeTapPool. Zero (omitted) unless the engine
		// published a pool, so an ordinary option is byte-identical.
		opt.TapPoolCost = tapCosts[of.id]
		opts = append(opts, opt)
	}
	// A MaxAttackers$ ceiling (CR 508.1j, Silent Arbiter's shape) bounds the
	// WHOLE declaration, so the decision's Max is the honest ceiling, not the
	// option count: a client capped at Max can never assemble a declaration
	// the engine would reject for size. Without a ceiling in force
	// combat.MaxAttackers returns the int maximum and the clamp is inert
	// (Max == len(opts), today's value).
	maxOpts := len(opts)
	if ceil := combat.MaxAttackers(asBoard(e)); ceil < maxOpts {
		maxOpts = ceil
	}
	maxSum := 0
	for _, o := range opts {
		if o.Value > 0 {
			maxSum = int(budget)
			break
		}
	}
	// Publish the payer's life as the bound on the declaration's combined
	// non-mana LIFE charge (CostLife plus each Phyrexian pip at two life), the
	// same way MaxSum publishes the mana bound. The combined charge is a
	// whole-declaration property the per-(attacker,defender) option list cannot
	// express -- a per-attacker tax (Norn's Annex) offers every pair payable on
	// its own -- so decision.RequiredQuota and FitRequired read this one field
	// to keep the required set and the declaration charge-feasible. Published
	// only when some offered pair carries a non-mana charge, so every
	// ordinary, charge-free declaration serialises byte-identically.
	payerLife := int32(0)
	for _, o := range opts {
		if o.CostLife > 0 || o.CostPhyrexian > 0 {
			payerLife = e.G.Players[p].Life
			break
		}
	}
	if len(opts) == 0 {
		// Every (attacker, defender) pair is blocked — a CantAttack static or
		// restriction covering the whole table — or priced out — a
		// CantAttackUnless prop whose charge the payer's attackBudget cannot
		// cover. No declaration anyone could answer differently exists, so the step resolves silently with the
		// empty declaration, the same no-decision path the no-attacker case
		// above takes (asking KAttackers with only the empty answer legal is
		// the forbidden wedge shape).
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: e.G.NextAlive(p)})
		return
	}
	e.ask(&decision.Decision{Player: p, Kind: decision.KAttackers, Min: 0, Max: maxOpts,
		Prompt: "turn " + strconv.Itoa(int(e.G.Turn)) + " — declare attackers", Options: opts,
		// The cumulative attack-cost budget: the sum of the chosen options'
		// Value (each pair's folded mana price: its tax plus, under a tax, the
		// mana the attacker's own source would have produced) must not exceed
		// the payer's budget. Decision.Validate enforces it as a general wire
		// contract, so the engine never sees an over-budget declaration and no
		// client has to sum prices itself. Published only when some offered
		// pair is priced: with every Value 0 the cap is vacuous, and leaving
		// it 0 (omitted) keeps every prop-free declaration's wire payload
		// byte-identical.
		MaxSum: maxSum,
		// A raised per-defender attacker cap (AttackRestrict scoped by
		// ValidDefender$). Nil when no scoped ceiling exceeds one, so every
		// ordinary declaration's wire payload is byte-identical.
		GroupLimits: groupLimits,
		// The combined non-mana charge bound (see payerLife above).
		PayerLife: payerLife,
		// The tap-candidate pool (see tapPool above): the pool size the
		// declaration's tapXType obligation draws from, published with each
		// option's TapPoolCost so a rules-ignorant client can see whether a
		// declaration still leaves it payable. 0 (omitted) when no pool is
		// readable, so every ordinary declaration is byte-identical.
		ChargeTapPool: tapPool})
}

// handleAttackers records the chosen attackers (CR 508.1c: this is what
// causes triggered abilities matching "Attacks" to fire, via checkTriggers
// running behind every emit) and taps each one unless it has Vigilance (CR
// 508.1f, 702.20b).
//
// Each chosen option carries its own defender (the KAttackers option list is
// one entry per (attacker, defender) pair, Task m34), so the declaration is
// grouped by defender into one DeclareAttackers event per defending player,
// emitted in ascending seat order -- deterministic, and the same order the
// declare-blockers step asks the defenders in. The chosen set is guaranteed
// to hold distinct attackers (validateAttackers ran before this handler), so
// each creature lands in exactly one group. Within a defender, the event's
// IDs keep the order the client submitted them (what an Attacks trigger's
// Remembered reads, in trigger_match.go).
func (e *Engine) handleAttackers(d *decision.Decision, in decision.Intent) {
	chosen := d.Chosen(in)
	// Publish the whole declaration for the declaration-wide trigger matches
	// (CR 702.70 Training's "attacks with another creature"): the events
	// below are per defender, so ev.IDs alone cannot answer it. Rebuilt by
	// replay, which re-executes this handler (see the field doc). The defer
	// clears it again so a LATER direct DeclareAttackers emit (a synthetic
	// test event, or a future emitter) can never read a stale declaration --
	// triggers fire synchronously inside the emits above, so every reader has
	// already run by the time this returns.
	if len(chosen) == 0 {
		// An empty declaration is still an event: it is the replay-derived
		// marker that the declaration turn-based action has completed. The
		// following Advance opens priority in this step; only that round's
		// completion skips blockers and damage under CR 508.8.
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: e.G.NextAlive(e.G.Active)})
		return
	}
	// CR 508.1: attack costs are paid as attackers are declared, BEFORE the
	// declaration commits (the enlist election follows the same rule -- "as
	// this creature attacks" also happens during the declaration). A
	// chargeable declaration pays from the floating pool when it already
	// covers the charge, otherwise through the tap-payment window
	// (startAttackPay, rules/attack_cost.go), whose completion resumes right
	// here with the enlist election. Both the charge and the offer list were
	// re-derived by validateAttackers moments ago from the same pure reads,
	// so the window's coverage guard cannot fail here; the Note path is the
	// loud defensive fallback.
	if charge := e.attackCharge(chosen); !charge.zero() {
		plan, ok := e.openCombatPayPlan(d.Player, charge, chosenAttackers(chosen), chosenAttackers(chosen))
		if !ok {
			e.emit(events.Event{Kind: events.Note, Player: d.Player,
				Text: "could not pay the attack cost"})
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: e.G.NextAlive(e.G.Active)})
			return
		}
		if e.combatPlanSettlesInline(plan) {
			// The floating pool already covers the whole charge (mana and any
			// Phyrexian pips on the deterministic branch); settle inline through
			// the same plan/pay path the window uses. When a CR 107.4f election
			// is owed, combatPlanSettlesInline is false and the window opens to
			// pose it even though the pool could pay the colour branch.
			e.payCombatChargeInline(plan)
		} else if e.startAttackPay(chosen, plan) {
			return
		} else {
			// The window could not complete the charge; startAttackPay emitted
			// the loud Note. ABORT: the cost is a CR 508.1 declaration cost, so
			// an unpaid charge may not silently commit -- emit the empty
			// no-attack declaration (the same event the len(chosen)==0 branch
			// emits) and advance the step. Permissive by accident would be the
			// opposite danger: committing an attack nobody paid for.
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: e.G.NextAlive(e.G.Active)})
			return
		}
	}
	// CR 702.160a (task enlist1): enlist is an "as this creature attacks"
	// action that happens DURING the declaration, before the attack triggers
	// are put on the stack. Its election is therefore posed here, BEFORE the
	// DeclareAttackers events below are emitted, so an attack trigger whose
	// intervening-if reads enlistedThisCombat (Aradesh, the Founder) is
	// matched with the answered stamp already in place -- the reverse order
	// (exert's) would match that trigger false. An attacker with no eligible
	// creature poses no ask and the declaration finishes inline.
	if e.startEnlistAsks(chosen, d.Player) {
		return
	}
	e.finishAttackers(chosen, d.Player)
}

// finishAttackers completes the declare-attackers declaration once every
// enlist election is answered: emit one DeclareAttackers event per defending
// player, tap the non-Vigilance attackers (CR 508.1f), then offer the exert
// elections (CR 702.100a, task exert1). Split out of handleAttackers so the
// enlist continuation (rules/enlist.go) and the attack-cost payment window
// (rules/attack_cost.go's attackPayAnswer) can resume exactly here.
//
// The declaration-wide scratch is published HERE, not in handleAttackers:
// the emits below fire the declaration's triggers synchronously, and the
// declare-attackers step can now suspend between the choice and the emits (a
// chargeable declaration whose pool cannot cover it opens the attack-cost
// payment window, rules/attack_cost.go). handleAttackers' own frame has
// unwound by then, so publishing there left the window path's emits with an
// empty scratch and lost CR 702.70 Training's cross-defender match. Every
// path that emits the DeclareAttackers events -- inline, the enlist
// continuation, the payment window -- goes through this function, so this is
// the one place the scratch must be built. The clear at the end is the same
// guard as before: triggers fire synchronously inside the emits, so every
// reader has run by the time this returns, and a LATER direct
// DeclareAttackers emit can never read a stale declaration.
func (e *Engine) finishAttackers(chosen []decision.Option, player state.PlayerID) {
	e.declaredAttackers = e.declaredAttackers[:0]
	e.declaredDefenders = e.declaredDefenders[:0]
	for _, opt := range chosen {
		e.declaredAttackers = append(e.declaredAttackers, opt.Obj)
		// Opt.Player is the defending seat for player, planeswalker and
		// battle attacks alike (CR 702.121b); duplicates attack one opponent.
		if opt.Player != player && !slices.Contains(e.declaredDefenders, opt.Player) {
			e.declaredDefenders = append(e.declaredDefenders, opt.Player)
		}
	}
	defer func() {
		e.declaredAttackers = e.declaredAttackers[:0]
		e.declaredDefenders = e.declaredDefenders[:0]
	}()
	type defKey struct {
		player state.PlayerID
		battle state.ObjID
	}
	byDef := make(map[defKey][]state.ObjID, len(chosen))
	var keys []defKey
	for _, opt := range chosen {
		k := defKey{player: opt.Player, battle: opt.Battle}
		if _, ok := byDef[k]; !ok {
			keys = append(keys, k)
		}
		byDef[k] = append(byDef[k], opt.Obj)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].player != keys[j].player {
			return keys[i].player < keys[j].player
		}
		return keys[i].battle < keys[j].battle
	})
	for _, k := range keys {
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: k.player, Obj: k.battle, IDs: byDef[k]})
	}
	for _, opt := range chosen {
		if !e.hasKeywordH(opt.Obj, kwhVigilance) {
			// CR 508.1f: the player declaring attackers taps them.
			e.emitTap(opt.Obj, player, false)
		}
	}
	// CR 702.100a (task exert1): each attacking creature carrying an
	// offerable stat:OptionalAttackCost static is offered its exert
	// election now, still inside the declare-attackers step, before the
	// declare-blockers step begins. The election is one KChoose per
	// offerable attacker in the declaration's own option order (chosen
	// order, deduped) -- deterministic, and the re-derivation a replay runs
	// when it answers the recorded intents again.
	e.startExertAsks(chosen)
}

// exertOfferList returns the declared attackers (in chosen-option order,
// deduped) that carry an offerable stat:OptionalAttackCost static: the
// static's source is the attacker itself (every corpus carrier's ValidCard$
// is Card.Self, verified in triage), its controller is the attacker's
// controller, and its as-long-as gate (IsPresent$/IsPresent2$/CheckSVar$,
// the shared continuousGateHolds grammar) holds. Combat Celebrant's
// `IsPresent$ Creature.Self+notExertedThisTurn` is the corpus's one gated
// carrier: it is only offerable while it has not been exerted this turn.
func (e *Engine) exertOfferList(chosen []decision.Option) []state.ObjID {
	var out []state.ObjID
	seen := make(map[state.ObjID]bool, len(chosen))
	for _, opt := range chosen {
		id := opt.Obj
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		if e.exertOfferHolds(id) {
			out = append(out, id)
		}
	}
	return out
}

// exertOfferHolds reports whether id carries a stat:OptionalAttackCost
// static whose source is id itself and whose gate holds at this instant.
func (e *Engine) exertOfferHolds(id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
		return false
	}
	for _, sv := range e.activeStatics("OptionalAttackCost") {
		if sv.Source != id || sv.Controller != o.Controller {
			continue
		}
		// The static's own ValidCard$ (uniformly Card.Self over the corpus's
		// 28 carriers, verified in triage) must still admit the attacker;
		// an unparseable spec fails closed.
		if vc := sv.ParamStr(cards.PKValidCard); vc != "" &&
			!e.matchesSpecFrom(vc, id, o.Controller, sv.Source) {
			continue
		}
		if !e.continuousGateHolds(sv) {
			continue
		}
		return true
	}
	return false
}

// startExertAsks seeds the exert election's offer list from the answered
// declaration and poses the first ask, if any attacker carries an offer.
func (e *Engine) startExertAsks(chosen []decision.Option) {
	offers := e.exertOfferList(chosen)
	if len(offers) == 0 {
		return
	}
	e.exertAskState = exertAsk{offers: offers}
	e.askNextExert()
}

// askNextExert poses the exert election for the next offerable attacker, or
// clears the election once the list is exhausted. The offer gate is
// re-evaluated per ask: the exert asks never change state between
// themselves, but the re-check keeps the cursor honest against any future
// interleaved state change and costs one statics walk per offer.
func (e *Engine) askNextExert() {
	p := e.G.Active
	for e.exertAskState.next < len(e.exertAskState.offers) {
		id := e.exertAskState.offers[e.exertAskState.next]
		if e.exertOfferHolds(id) {
			o := e.G.Obj(id)
			d := &decision.Decision{Player: p, Kind: decision.KChoose, Min: 1, Max: 1,
				Prompt: fmt.Sprintf("Exert %s as it attacks? (An exerted creature won't untap during your next untap step.)", o.Face().Name),
				Source: id}
			d.Options = append(d.Options,
				decision.Option{Index: 0, Kind: "exert", Label: "Don't exert " + o.Face().Name},
				decision.Option{Index: 1, Kind: "exert", Label: "Exert " + o.Face().Name,
					Obj: id, Amount: 1})
			e.choosing = chooseExert
			e.ask(d)
			return
		}
		e.exertAskState.next++
	}
	e.exertAskState = exertAsk{}
}

// exertAnswer applies one answered exert election: the decline (option 0)
// emits nothing, a yes emits the Exert event (whose fold stamps both
// lifetimes and whose checkExertTriggers walk queues the static's Trigger$
// rider), then the cursor advances to the next offerable attacker or the
// election ends. The Advance loop resumes the declare-attackers step's own
// flow -- the priority round -- when no ask is left.
func (e *Engine) exertAnswer(d *decision.Decision, in decision.Intent) {
	e.choosing = chooseNone
	chosen := d.Chosen(in)
	if len(chosen) == 1 && chosen[0].Amount == 1 && chosen[0].Obj != 0 {
		e.emit(events.Event{Kind: events.Exert, Obj: chosen[0].Obj, Player: d.Player})
	}
	e.exertAskState.next++
	e.askNextExert()
}

// validateAttackers is the KAttackers legality guard behind Option A's
// cross-product option list (Task m34). One creature may be declared
// attacking at most one opponent (CR 506.2, CR 903.14), but the option list
// offers every (attacker, defender) pair, so an intent naming the same
// creature twice -- against two different defenders -- passes decision.
// Decision.Validate's per-index checks (two distinct, in-range options)
// while declaring one creature attacking two players. Rejecting here, before
// the intent is recorded and the decision consumed, is the enforcement
// boundary: the legality of an attacker set is a property of the DECLARATION
// as a whole, not of any single option, so it cannot live in Validate's
// option-shape checks, and it must not be trusted to a client to avoid (a
// rules-ignorant client only knows it may pick any subset of the pairs it
// was offered). With a single opponent the pair list has exactly one option
// per creature, so this guard is inert in two-player games.
//
// Task jj-cmb (F38) adds the CR 508.1c/d requirement and restriction checks
// on top of the duplicate-defender guard: validateAttackDeclaration rejects
// a declaration that does not maximise must-attack requirements subject to
// attack restrictions (e.g. Silent Arbiter's MaxAttackers).
func (e *Engine) validateAttackers(d *decision.Decision, in decision.Intent) error {
	// The published declaration-dependent tap rule (decision.ChargeTapPoolFit),
	// shared with the wire validator and the bot's repair: a declaration that
	// consumes the last candidate its own tapXType obligation draws from is
	// rejected here by the SAME arithmetic Decision.Validate applies, so a
	// rules-ignorant client (and the bot) never submits one the engine refuses.
	// Inert (returns true) unless askAttackers published a pool.
	if !d.ChargeTapPoolFit(in.Choices) {
		return fmt.Errorf("declaration's attack cost exhausts the tap obligation's candidate pool (%d)", d.ChargeTapPool)
	}
	seen := make(map[state.ObjID]bool, len(in.Choices))
	// The offered-pair set (rules/attack_cost.go): every chosen option must
	// be a pair the offer list admitted -- the CantAttack scoping and the
	// attack-prop budget serialization are properties of the OFFER LIST, and
	// re-deriving it here (the same pure read askAttackers ran) keeps a
	// hand-built intent from naming a pair the budget ran out on.
	offers := e.attackOffersPosed(d)
	offered := make(map[attackOfferKey]blockCharge, 8)
	for _, of := range offers {
		offered[attackOfferKey{id: of.id, def: of.def, battle: of.battle}] = of.charge
	}
	// The same folded budget currency askAttackers published (attackBudget
	// Value): each attacker costs its mana price PLUS, under a mana tax, the
	// mana its own source forgoes by attacking. Summing the raw charge.mana
	// here would let this belt admit a declaration the very next check
	// (combatChargeAffordable over chosenAttackers) rejects -- the two must
	// agree. See attackOptionBudgetValue in rules/attack_cost.go. Without a
	// tax every offered cost is 0, so the budget is never compared.
	taxed := e.attackTaxed(offers)
	budget, selfUnits := e.attackBudgetUnits(d.Player, taxed)
	total := int32(0)
	// The whole declaration's composite charge, validated against the same
	// combatChargeAffordable read the offer gate used: mana within the budget,
	// life within the payer's total, Phyrexian pips reachable, and every
	// tap/sac/return obligation met by distinct permanents with the
	// declaration's own attackers set aside.
	var declaredCharge blockCharge
	// The chosen options, resolved once for every read below
	// (validateAttackDeclaration's included).
	chosen := d.Chosen(in)
	for _, o := range chosen {
		if !combat.CanAttackPair(asBoard(e), o.Obj, o.Player) {
			return fmt.Errorf("object %d cannot attack", o.Obj)
		}
		if o.Battle != 0 && !e.canAttackBattle(o.Battle, d.Player) {
			return fmt.Errorf("object %d cannot attack permanent %d", o.Obj, o.Battle)
		}
		if seen[o.Obj] {
			return fmt.Errorf("attacker %d declared against more than one defender", o.Obj)
		}
		if combat.AttackBlocked(asBoard(e), o.Obj, o.Player, o.Battle) {
			return fmt.Errorf("attacker %d cannot attack player %d", o.Obj, o.Player)
		}
		// A required creature's named duty is enforced by the offered-pair set
		// itself: attackOffers drops every pair that satisfies fewer named
		// requirements than the creature's best available defender, so a
		// sub-maximal defender is NOT offered and fails the membership check
		// below with its own message. The requirement that the creature attack
		// AT ALL is enforced by validateAttackDeclaration's RequiredQuota.
		charge, ok := offered[attackOfferKey{id: o.Obj, def: o.Player, battle: o.Battle}]
		if !ok {
			return fmt.Errorf("attacker %d cannot attack player %d (attack cost not affordable or pair not offered)", o.Obj, o.Player)
		}
		// Belt against a future membership gap: the serialized offer list
		// already bounds every subset's folded Value total, so this can only
		// fire if the two walks ever diverge. A free, source-free pair adds
		// nothing and can never be what overruns the budget, so only a priced
		// or source-bearing pair is checked.
		cost := e.attackOptionBudgetValue(charge, o.Obj, selfUnits, taxed)
		total += cost
		if cost > 0 && total > budget {
			return fmt.Errorf("declaration's attack cost {%d} exceeds the affordable {%d}", total, budget)
		}
		declaredCharge = declaredCharge.plus(charge)
		seen[o.Obj] = true
	}
	// The whole declaration's composite charge, priced together (a static's
	// per-creature charge sums over the declared attackers), against the same
	// board-aware combatChargeAffordable read the offer gate used. The message
	// names EVERY component including the mana: the priced components can all
	// be zero while the charge is still unpayable, because the declaration's
	// own attackers are withheld from the payment window's mana sources
	// (CR 508.1f taps them), so a flat {N} attack tax an offer admitted against
	// the whole window can shrink below {N} once the other chosen attackers
	// leave it. The old text printed only the non-mana parts and read as an
	// unpriceable FREE charge. Naming the mana and the fail-closed flag keeps
	// the diagnostic from hiding the real component again.
	if !declaredCharge.zero() &&
		!e.combatChargeAffordable(d.Player, declaredCharge, chosenAttackers(chosen), chosenAttackers(chosen)) {
		return fmt.Errorf("declaration's attack cost (%d mana, %d life, %d taps, %d sacrifices, %d returns, %d Phyrexian, unpriceable=%t) is not payable",
			declaredCharge.mana, declaredCharge.life, len(declaredCharge.taps), len(declaredCharge.sacs), len(declaredCharge.returns), len(declaredCharge.phyrexian), declaredCharge.unpriceable)
	}
	return e.validateAttackDeclarationChosen(d, in, chosen)
}

// mustAttackRequired reports whether id is a creature that must attack this
// combat (CR 508.1d), under the active player's control and able to attack.
//
// A creature is required when its combat.RequirementSet is non-empty (an
// encore designation, any applicable Effect-registered or face Mode$
// MustAttack static, or a live goad) AND at least one offered pair actually
// DISCHARGES one of those duties (attackDutyDischargeable) -- CR 508.1d's
// "attacks if able". The requirement set
// is the board-wide collection (which includes the creature's own face,
// source-bound through the same specCtx the face walk used), so an
// AURA-carried requirement -- Fealty to the Realm's `S:Mode$ MustAttack |
// ValidCreature$ Creature.EnchantedBy`, the Vow cycle's shape -- reaches the
// enchanted creature, not just its bearer's own face. A MustAttack static
// carrying a condition or any other parameter the requirement collector
// cannot evaluate contributes nothing (combat.AttackRequirements skips it), the safe
// direction for a requirement: erring toward requiring a creature that
// already attacks changes nothing, while falsely requiring one that cannot
// legitimately attack would make a legal declaration unanswerable.
func (e *Engine) mustAttackRequired(id state.ObjID) bool {
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != e.G.Active {
		return false
	}
	f := o.Face()
	if f == nil || !combat.CanAttack(asBoard(e), id) {
		return false
	}
	s := combat.AttackRequirements(asBoard(e), id)
	if !s.Any() {
		return false
	}
	// CR 508.1d counts a requirement only when the creature can actually
	// satisfy it ("attack ... if able"): a creature whose every (attacker,
	// defender) pair a CantAttack static/restriction, a goad restriction or
	// the attack-prop budget removes is NOT required, otherwise
	// validateAttackDeclaration would reject every legal declaration and the
	// KAttackers decision would have no legal answer. The MaxAttackers$
	// ceiling is deliberately not a pair gate: the requirement solver's
	// maxReq (validateAttackDeclaration) already clamps to it, and a nonzero
	// ceiling that merely caps the count still leaves the requirement binding.
	// attackDutyDischargeable reads the same maximal-satisfaction offer list
	// the options do, so the two can never disagree.
	return e.attackDutyDischargeable(id, s)
}

// attackDutyDischargeable reports whether creature id has at least one
// offered (attacker, defender) pair that actually DISCHARGES a requirement in
// s. It is the "if able" half of CR 508.1d read as a duty, not merely as the
// existence of some legal pair.
//
// A BROAD requirement (an unconditional Mode$ MustAttack) is discharged by
// any surviving pair. A goad requires a surviving PLAYER pair; only then is
// attacking mandatory. A NAMED
// requirement names its defender, so only a pair against that player
// discharges it: when every such pair is gone -- a CantAttack static or
// restriction scoped to that one defender, a goad restriction, or an
// individually unaffordable attack-prop price -- attacking a DIFFERENT player
// and not attacking at all both obey ZERO requirements, so CR 508.1d permits
// either and the creature is NOT required. attackOffers deliberately keeps
// every surviving pair in that case (its maximal-satisfaction filter is a
// no-op when the best satisfaction is zero), so the creature may still attack
// freely; it simply must not be MARKED Required, which would reject the
// equally maximal no-attack declaration and, with no other required creature
// to fall back on, leave the KAttackers decision no legal answer at all.
//
// The pairs ARE the attackOffers list (rules/attack_cost.go): the defender
// enumeration (AliveFrom(0), controller excluded), the goad/CantAttack
// scoping, the CR 508.1d maximal-satisfaction filter and the attack-prop
// budget serialization are all the offer list's own rules, so the requirement
// solver, the option list and validateAttackDeclaration can never disagree
// about which pairs exist or which of them discharge a duty.
func (e *Engine) attackDutyDischargeable(id state.ObjID, s combat.RequirementSet) bool {
	anyPair := false
	for _, of := range e.attackOffers() {
		if of.id != id {
			continue
		}
		if s.SatisfiedByOffer(of.def, of.battle) > 0 {
			return true
		}
		anyPair = true
	}
	return anyPair && s.Broad
}

// validateAttackDeclaration enforces CR 508.1c/d on the chosen attacker set.
// The active player must attack with as many creatures that fulfil a
// requirement (MustAttack) as possible, subject to restrictions
// (AttackRestrict's MaxAttackers), so a declaration that omits a required
// creature it was legal to include is rejected, as is one that exceeds a
// ceiling. When no requirement and no restriction is in force (the ordinary
// game), the checks are inert.
func (e *Engine) validateAttackDeclaration(d *decision.Decision, in decision.Intent) error {
	return e.validateAttackDeclarationChosen(d, in, d.Chosen(in))
}

// validateAttackDeclarationChosen is validateAttackDeclaration over
// chosen = d.Chosen(in), already resolved by the caller (validateAttackers).
func (e *Engine) validateAttackDeclarationChosen(d *decision.Decision, in decision.Intent, chosen []decision.Option) error {
	maxAllowed := combat.MaxAttackers(asBoard(e))
	// CR 508.1d: the declaration must include as many required creatures as
	// possible. The options carry the requirement (Option.Required, set from
	// mustAttackRequired in askAttackers), the attack-prop budget
	// (Decision.MaxSum over each pair's Value) and the MaxAttackers$ ceiling
	// (Decision.Max), so "as many as possible" is decision.RequiredQuota --
	// the ONE rule the client-side repair (botpolicy.Clamp via
	// Decision.FitRequired) also builds from. A required creature a
	// CantAttackUnless prop prices is "able" only while the declaration stays
	// within budget; re-deriving that bound here from the board, on its own,
	// is exactly how the bot's own answer came to be rejected on a board where
	// a legal declaration existed (attackprop1 review, the livelock pinned by
	// TestAttackPropRequiredBotAnswerNeverLivelocks).
	if quota, got := d.RequiredQuota(), d.RequiredChosen(in.Choices); got < quota {
		return fmt.Errorf("must attack with as many required creatures as possible (required %d, declared %d; max attackers %d)",
			quota, got, maxAllowed)
	}
	if len(chosen) > maxAllowed {
		return fmt.Errorf("declared %d attackers, more than the allowed %d", len(chosen), maxAllowed)
	}
	counts := make(map[state.PlayerID]int)
	for _, index := range in.Choices {
		if index >= 0 && index < len(d.Options) {
			counts[d.Options[index].Player]++
		}
	}
	for defender, count := range counts {
		limit, ok := combat.AttackRestrictLimit(asBoard(e), defender)
		if ok && count > limit {
			return fmt.Errorf("declared %d attackers at defender %d, more than the allowed %d", count, defender, limit)
		}
	}
	return nil
}

// attackRestrictGroup marks options at a defender constrained by an active
// AttackRestrict static, and reports that defender's ceiling. Decision.Validate
// and botpolicy.Clamp share the per-Group cap rule through
// Decision.GroupLimits, so the bot cannot offer an answer the engine rejects;
// a ceiling above one (Crawlspace's "no more than two creatures can attack
// you") rides GroupLimits while the ordinary at-most-one-per-Group rule covers
// the limit-one shape byte-identically.
func (e *Engine) attackRestrictGroup(defender state.PlayerID) (string, int) {
	limit, ok := combat.AttackRestrictLimit(asBoard(e), defender)
	if !ok {
		return "", 0
	}
	return fmt.Sprintf("attack-restrict:%d", defender), limit
}

// validateBlockers is the KBlockers whole-declaration legality guard. The
// cross-product option list correctly offers each blocker against every
// attacker it may block, but choosing two of those individually legal options
// for one ordinary blocker violates CR 509.1a. Decision.Validate only checks
// the shape of each selected option, so reject the combination here before the
// intent is recorded or the pending decision is consumed. Multiple blockers
// may still choose the same attacker, but CR 702.111b requires either zero or
// at least two of them when that attacker has Menace. The build has no model
// for effects that let one creature block additional attackers; this limit
// must become capability-aware when such effects are implemented.
func (e *Engine) validateBlockers(d *decision.Decision, in decision.Intent) error {
	seen := make(map[state.ObjID]bool, len(in.Choices))
	chosen := d.Chosen(in)
	// The whole-declaration charge, priced by the same blockPairCharge the
	// offer list and the payment use, checked against the same
	// blockChargeAffordable read: mana within the budget (Decision.MaxSum
	// publishes it too), life within the payer's total, and every tap
	// obligation payable by distinct permanents once the declaration's own
	// committed blockers are set aside.
	charge := e.blockChargeOf(chosen)
	if !charge.zero() && !e.blockChargeAffordable(d.Player, charge, chosenBlockers(chosen), nil) {
		need := int32(0)
		for _, t := range charge.taps {
			need += t.n
		}
		return fmt.Errorf("declaration's block charge ({%d} mana, %d life, %d taps) exceeds what the defender can pay",
			charge.mana, charge.life, need)
	}
	byAttacker := make(map[state.ObjID]int, len(chosen))
	for _, o := range chosen {
		if seen[o.Obj] {
			return fmt.Errorf("blocker %d declared against more than one attacker", o.Obj)
		}
		seen[o.Obj] = true
		byAttacker[o.Attacker]++
	}
	// CR 509.1c: the same whole-team maximum used by the client repair.
	// Min/Max bounds, group exclusivity and total block costs all constrain
	// which requirements can be satisfied together.
	want := d.RequiredQuota()
	got := d.RequiredChosen(in.Choices)
	if got < want {
		return fmt.Errorf("declaration satisfies %d of %d possible blocking requirements", got, want)
	}
	checked := make(map[state.ObjID]bool, len(byAttacker))
	for _, o := range chosen {
		if checked[o.Attacker] {
			continue
		}
		checked[o.Attacker] = true
		if byAttacker[o.Attacker] == 1 && e.hasKeywordH(o.Attacker, kwhMenace) {
			return fmt.Errorf("attacker %d with menace must be blocked by at least two creatures", o.Attacker)
		}
		if err := combat.ValidateMinMaxBlockers(asBoard(e), o.Attacker, byAttacker[o.Attacker], d.Player); err != nil {
			return err
		}
	}
	return nil
}

// blockPairScope is the per-defender pair admissibility ONE declare-blockers
// ask derives: the attacking creatures, the attackers an impossible CR 509.1a
// bound rules out, and the [min,max] hint each offered option publishes.
// The maps are membership/read only; option order stays battlefield order.
type blockPairScope struct {
	attackers     []state.ObjID
	minImpossible map[state.ObjID]bool
	bounds        map[state.ObjID][2]int
}

// blockPairScopeFor derives the scope for one defender's declare-blockers
// ask: a pure read, deterministic (blockAttackers is the battlefield scan).
func (e *Engine) blockPairScopeFor(defender state.PlayerID) blockPairScope {
	scope := blockPairScope{
		attackers:     e.blockAttackers(defender),
		minImpossible: make(map[state.ObjID]bool),
		bounds:        make(map[state.ObjID][2]int),
	}
	for _, aid := range scope.attackers {
		min, max, minOK, maxOK, all := combat.MinMaxBlockerBounds(asBoard(e), aid)
		// CR 702.111b: Menace is the same whole-declaration floor as a
		// MinMaxBlocker Min$ 2 ("can't be blocked except by two or more
		// creatures"), so it is folded into the published bound. A client
		// that drops or cannot field the second blocker (a tap-costed or
		// unaffordable pair, a Max$ trim) then sees the floor on the option
		// itself instead of having to re-derive the keyword, and a Menace
		// attacker that also carries a Max$ below two (or faces fewer than
		// two legal blockers) is never offered at all.
		if !all && e.hasKeywordH(aid, kwhMenace) && (!minOK || min < 2) {
			min, minOK = 2, true
		}
		var b [2]int
		if minOK {
			b[0] = min
		}
		if maxOK {
			b[1] = max
		}
		if all {
			// Min$ All: a blocking declaration must be EVERY creature the
			// defender controls (an unblocked declaration stays legal). If
			// any of them cannot block, no blocking declaration is possible
			// at all, so the attacker's pairs are not offered; otherwise
			// the bounds publish the required all-team and a client unable
			// to field it drops the block.
			required := combat.DefenderCreatureCount(asBoard(e), defender)
			if combat.LegalBlockerCount(asBoard(e), aid, defender) < required ||
				(required < 2 && e.hasKeywordH(aid, kwhMenace)) {
				scope.minImpossible[aid] = true
			} else {
				b = [2]int{required, required}
			}
		} else if minOK && (combat.LegalBlockerCount(asBoard(e), aid, defender) < min || (maxOK && max < min)) {
			scope.minImpossible[aid] = true
		}
		if b[0] != 0 || b[1] != 0 {
			scope.bounds[aid] = b
		}
	}
	return scope
}

// admissiblePair reports whether the declare-blockers offer actually offers
// the (blocker, attacker) pair: combat.CanBlock, the attacker not ruled out by an
// impossible block-count bound, and a per-pair price the defender can pay.
// This is the ONE oracle of declarability: askBlockers' option build and
// its MustBlock team solver read it, so a requirement cannot be counted on a
// pair no client could declare.
func (e *Engine) admissiblePair(scope *blockPairScope, defender state.PlayerID, blocker, attacker state.ObjID) bool {
	if scope.minImpossible[attacker] {
		return false
	}
	if !combat.CanBlock(asBoard(e), blocker, attacker) {
		return false
	}
	charge := e.blockPairCharge(blocker, attacker)
	if !charge.zero() && !e.blockChargeAffordable(defender, charge, map[state.ObjID]bool{blocker: true}, nil) {
		return false
	}
	return true
}

// blockerRound is the declare-blockers step's plain-value cursor, the same
// one-decision-at-a-time pattern as the London mulligan round (rules/
// mulligan.go): Task m34 lets one attack split across several defending
// players (CR 506.2), and each defender declares its own blocks (CR
// 509.1c). order lists the defenders that have at least one attacking
// creature, in APNAP turn order starting after the active player; cursor is
// the next defender to ask. askBlockers builds the list on the step's first
// entry, asks one
// defender per call, and hands the step to its post-declaration priority
// round once every defender has declared. Plain data (a slice plus an index), never a
// closure, so Engine.Clone copies it like the mulligan round.
//
// zero value: order == nil means "not yet built", the step's first-entry
// state; an empty-but-built round (order with len 0) means "nothing to
// block", which askBlockers records as an empty declaration before priority.
type blockerRound struct {
	order  []state.PlayerID
	cursor int
}

// askBlockers runs the declare-blockers step one defending player at a
// time. The first entry to the step builds the defender list; each call
// then asks the next defender with at least one legal block option, and the
// last one exhausted opens the step's priority round. A defender with
// attackers but zero legal options for them is skipped without a decision:
// its only legal answer would be "block with nothing", and asking would
// change nothing (the same skip askAttackers applies to its own no-option
// case). Between two defenders' answers the Advance loop stays on
// StepDeclareBlockers (handleBlockers only advances the round cursor), so a
// split attack on two opponents produces two KBlockers decisions, one per
// defender, in APNAP turn order.
//
// With no attackers this combat (declared 0, or all already gone), there is
// nothing to block: the round is empty and the step skips straight to
// combat damage. With attackers but zero legal blockers for them (every
// candidate fails combat.CanBlock, e.g. a lone ground creature against a flier),
// the same reasoning skips each such defender.
func (e *Engine) askBlockers() {
	if e.blockerRound.order == nil {
		order := make([]state.PlayerID, 0, len(e.G.Players))
		// CR 802.4: the active player's opponents declare in APNAP turn order.
		for _, q := range e.G.AliveFrom(e.G.Active) {
			if q == e.G.Active || len(e.blockAttackers(q)) == 0 {
				continue
			}
			order = append(order, q)
		}
		e.blockerRound = blockerRound{order: order}
	}
	br := &e.blockerRound
	for br.cursor < len(br.order) {
		defender := br.order[br.cursor]
		// The per-defender pair scope, derived once: minImpossible records a
		// Min$ the defender cannot meet (every non-empty declaration is
		// illegal, so only the forced empty declaration can include the
		// attacker and its pairs are not offered); bounds carries the
		// [min,max] hint every offered option publishes on the wire. The
		// requirement matcher runs over the SAME scope, so its maximum is
		// counted on exactly the pairs the offer below makes declarable.
		scope := e.blockPairScopeFor(defender)
		// The options are collected in a stack buffer and copied once into an
		// exact-size list below, instead of regrowing a 392-byte element
		// slice through every doubling.
		var optBuf [16]decision.Option
		built := optBuf[:0]
		requiredBlockers := combat.MustBlockCandidates(asBoard(e), defender)
		// The attacker-oriented CR 509.1c requirements: every attacker the
		// defender is being asked about that carries "CARDNAME must be blocked
		// if able.". The requirement is satisfied by ANY one legal blocker
		// pair, so the flag is per option and the whole-declaration solver
		// (decision.blockRequiredCore) counts it once per attacker; an attacker
		// with no offered pair (no legal blocker, no affordable pair) has no
		// option carrying the flag, so it contributes no requirement -- the
		// "if able" half is decided by the offer, never asserted here.
		mustBeBlocked := make(map[state.ObjID]bool, len(scope.attackers))
		for _, aid := range scope.attackers {
			if combat.HasMustBeBlockedKeyword(asBoard(e), aid) {
				mustBeBlocked[aid] = true
			}
		}
		for _, bid := range e.G.Zone(state.ZBattlefield, defender) {
			for _, aid := range scope.attackers {
				if !e.admissiblePair(&scope, defender, bid, aid) {
					continue
				}
				charge := e.blockPairCharge(bid, aid)
				// The offer gate: a pair is offered only when the defender can
				// pay its full composite charge individually (mana, life, taps
				// -- with the blocker itself set aside, since declaring it
				// commits it to blocking). The same read validates the whole
				// declaration and gates the payment, so the three cannot
				// disagree about what is payable.
				if !charge.zero() && !e.blockChargeAffordable(defender, charge, map[state.ObjID]bool{bid: true}, nil) {
					continue
				}
				// Group is the exclusivity marker on the wire: every option
				// naming this same blocker shares one Group, so the two
				// (blocker, attacker) pairs for that blocker are mutually
				// exclusive and a rules-ignorant client can enforce CR 509.1a
				// (one creature blocks one attacker) without knowing what a
				// blocker is. The value is internal only -- a blocker:<id>
				// prefix plus the object id -- never a display string.
				opt := decision.Option{Index: len(built), Kind: "block",
					Label: e.G.Obj(bid).Face().Name + " blocks " + e.G.Obj(aid).Face().Name,
					Obj:   bid, Attacker: aid, Player: defender,
					Group: "blocker:" + strconv.FormatUint(uint64(bid), 10), Required: requiredBlockers[bid], BlockMust: requiredBlockers[bid],
					AttackMust: mustBeBlocked[aid]}
				if b, ok := scope.bounds[aid]; ok {
					opt.MinBlockers, opt.MaxBlockers = b[0], b[1]
				}
				// The charge components ride the option for rules-ignorant
				// clients: Value the mana (the MaxSum budget's currency),
				// CostLife the life component, CostTaps the total tap
				// obligation.
				if charge.mana > 0 {
					opt.Label += fmt.Sprintf(" (pay {%d})", charge.mana)
				}
				if charge.life > 0 {
					opt.Label += fmt.Sprintf(", pay %d life", charge.life)
				}
				for _, t := range charge.taps {
					opt.Label += fmt.Sprintf(", tap %d", t.n)
				}
				opt.Value = int(charge.mana)
				opt.CostLife = int(charge.life)
				opt.CostPhyrexian = len(charge.phyrexian)
				for _, t := range charge.taps {
					opt.CostTaps += int(t.n)
				}
				// Menace is another whole-team minimum. Publish it for a
				// required block -- and for an attacker the CR 509.1c
				// requirement forces a block -- so the shared solver cannot
				// count a lone required blocker whose declaration would be
				// rejected.
				if (opt.Required || opt.AttackMust) && e.hasKeywordH(aid, kwhMenace) && opt.MinBlockers < 2 {
					opt.MinBlockers = 2
				}
				built = append(built, opt)
			}
		}
		if len(built) == 0 {
			br.cursor++
			continue
		}
		opts := append(make([]decision.Option, 0, len(built)), built...)
		maxSum := 0
		for _, opt := range opts {
			if opt.Value > 0 {
				maxSum = int(e.blockManaBudget(defender))
				break
			}
		}
		// The combined non-mana LIFE charge bound, as askAttackers publishes
		// it: requiredCore/RequiredQuota/FitRequired read it to keep a block
		// team whose per-pair charges sum past the payer's life out of the
		// required quota. 0 (omitted) when no offered pair carries one.
		payerLife := int32(0)
		for _, opt := range opts {
			if opt.CostLife > 0 || opt.CostPhyrexian > 0 {
				payerLife = e.G.Players[defender].Life
				break
			}
		}
		d := &decision.Decision{Player: defender, Kind: decision.KBlockers, Min: 0, Max: len(opts),
			Prompt: "turn " + strconv.Itoa(int(e.G.Turn)) + " — declare blockers", Options: opts, MaxSum: maxSum, PayerLife: payerLife}
		// First find the maximum legal declaration with every candidate
		// duty flagged. Publish only the required pairs in that team; the
		// other members remain optional helpers needed to meet a Min$ bound.
		team := d.BlockRequiredTeam()
		selected := make(map[state.ObjID]bool, len(team))
		for _, ci := range team {
			if d.Options[ci].BlockMust {
				selected[d.Options[ci].Obj] = true
			}
		}
		for i := range d.Options {
			d.Options[i].Required = selected[d.Options[i].Obj]
		}
		e.ask(d)
		return
	}
	// If every defender was skipped, no answer emitted a declaration. Record
	// the forced empty declaration so the log still marks this turn-based
	// action complete and the next Advance opens the priority window.
	if !e.declarationMadeThisStep(events.DeclareBlockers) {
		e.emit(events.Event{Kind: events.DeclareBlockers})
	}
}

// blockAttackers lists the creatures currently declared attacking defender
// (CR 509.1): every battlefield object under the active player's control
// marked IsAttacking with Attacking == defender and a Face().
//
// Ruling T21-c (Task 21 fix round 1): the census additionally requires
// Face() != nil. DeclareAttackers's own events.Apply case sets IsAttacking
// on any existing object with no such check (Player is validated, but
// nothing about the object it names), so a malformed or tampered event -- or
// a nil-Card object such as an ability's own stack object (Ruling F3) --
// reaching IsAttacking used to make it as far as the label build in the
// single-defender askBlockers, which read e.G.Obj(aid).Face().Name
// unconditionally: a nil-pointer panic, and therefore a remote kill of the
// whole match (one goroutine runs it). combat.CanAttack already requires this for a
// real attacker, so no legitimate attacker is excluded by requiring it here
// too.
func (e *Engine) blockAttackers(defender state.PlayerID) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, e.G.Active) {
		o := e.G.Obj(id)
		if o == nil || !o.IsAttacking || o.Face() == nil || o.Attacking != defender {
			continue
		}
		out = append(out, id)
	}
	return out
}

// handleBlockers records the chosen (attacker, blocker) pairs in one
// DeclareBlockers event, in the order the client submitted them -- that order
// is what BlockedBy preserves (events.Apply's DeclareBlockers case is a plain
// append per pair) and so what dealCombatDamage's damage-assignment loop
// below reads as "blocker order" for CR 510.1c's ordered damage assignment.
//
// Task m34: this advances only the blockers-round cursor, never the step.
// Each defending player declares its own blocks (CR 509.1c -- a split attack
// can involve several), and the Advance loop re-enters askBlockers for the
// next defender, which is what decides when the step moves to combat damage.
func (e *Engine) handleBlockers(d *decision.Decision, in decision.Intent) {
	chosen := d.Chosen(in)
	charge := e.blockChargeOf(chosen)
	if !charge.zero() {
		plan, ok := e.openCombatPayPlan(d.Player, charge, chosenBlockers(chosen), nil)
		if !ok {
			// An obligation cannot be met: decline rather than committing an
			// unpaid declaration.
			e.declineBlockDeclaration(d.Player)
			return
		}
		if e.combatPlanSettlesInline(plan) {
			e.payCombatChargeInline(plan)
		} else if e.startBlockPay(chosen, plan) {
			return
		} else {
			// The payment window could not complete the charge: decline the
			// declaration. Committing here would be the review's unpaid-block
			// defect.
			e.declineBlockDeclaration(d.Player)
			return
		}
	}
	pairs := make([][2]state.ObjID, 0, len(chosen))
	for _, opt := range chosen {
		pairs = append(pairs, [2]state.ObjID{opt.Attacker, opt.Obj})
	}
	// Empty is a real declaration and is also the replay-derived marker that
	// this defender answered; the cursor determines whether every defender
	// in a multiplayer declaration round has answered.
	e.emit(events.Event{Kind: events.DeclareBlockers, Player: d.Player, Pairs: pairs})
	e.blockerRound.cursor++
}

// combatRound is the combat damage step's continuation state (Task jj-cmb):
// which damage passes remain, and any controller damage-division choices
// being collected or awaiting an answer. It is the same plain-value state
// class as blockerRound -- scalars plus slices, never a closure -- so Clone
// copies it and a log-driven replay re-derives the identical branch from the
// recorded division and priority intents.
//
// Zero value means the combat damage step is not in progress. The step is
// reset to zero (combatRound{}) once both damage passes have dealt and the
// step has moved to end combat.
type combatRound struct {
	hasFirst    bool `clone:"deep"` // this combat damage step runs a first-strike pass (CR 510.3)
	firstDone   bool `clone:"deep"` // first-strike pass's damage dealt and priority granted
	regularDone bool `clone:"deep"` // regular pass's damage dealt

	// priorityPending marks the between-passes priority round (CR 510.3/4) as
	// owed: the first-strike pass's damage is dealt and its SBA tail posed an
	// SBA decision (the CR 704.5j legend rule), so completeCombatPass deferred
	// the round rather than displacing the ask. combatStep runs it once the
	// answer lands. Zero in every no-decision game.
	priorityPending bool `clone:"deep"`

	// pass is true while a combat damage pass is being processed (true = the
	// first-strike pass, false = the regular pass).
	pass bool `clone:"deep"`
	// active is true while a pass has begun (divisions collected or asked) and
	// has not yet finished dealing.
	active bool `clone:"deep"`

	// queue lists the attackers in this pass that still need a damage-division
	// answer, in battlefield order. When empty, an ask is pending for
	// askAttacker, or there were no divisions to ask at all.
	queue []state.ObjID `clone:"deep"`
	// done holds the divisions answered so far this pass, in answer order.
	done []divChoice `clone:"deep"`
	// askAttacker names the attacker whose damage-division decision is
	// currently pending (0 when none).
	askAttacker state.ObjID `clone:"deep"`
	// askOptions is parallel to the pending division Decision's Options:
	// askOptions[i] is the per-blocker damage split the i-th option selects.
	askOptions [][]int32 `clone:"deep"`

	// electQueue lists this pass's attackers whose controller may elect to
	// assign their combat damage as though they weren't blocked
	// (stat:AssignCombatDamageAsUnblocked, CR 509's optional assignment
	// election), in battlefield order. Elections are collected BEFORE the
	// division queue: an accepted election routes the whole power to the
	// defending player, so the attacker needs no division at all and is
	// dropped from the queue when its election is accepted (a declined
	// election leaves it in place for the ordinary division ask).
	electQueue []state.ObjID `clone:"deep"`
	// doneElect holds the attackers of this pass whose as-unblocked election
	// was ACCEPTED (or whose matching static is mandatory, auto-accepted
	// without an ask -- all printed corpus carriers are Optional$ True, so
	// the mandatory reading is comment-only today). damageStep consults it
	// through chosenElection before the ordinary assignment switch.
	doneElect []state.ObjID `clone:"deep"`
	// askElection marks the pending askAttacker ask as an election rather
	// than a division, so the answer routes to the right handler.
	askElection bool `clone:"deep"`
	// assignments and damageNext preserve a combat pass when a replacement
	// order decision parks one assignment. The remaining simultaneous pass
	// cannot run (nor can its SBA/regular pass) until that event settles.
	assignments []assignment `clone:"deep"`
	damageNext  int          `clone:"deep"`
	// initHad/initHolder/initCtrls carry the CR 726.2 simultaneous-pass
	// adjudication across a replacement-order suspension the same way:
	// initHolder is the initiative-holder snapshot taken when the pass began
	// dealing (the designation must be matched against WHO HELD IT when the
	// simultaneous pass began, not the live field, because this very block
	// moves it), and initCtrls accumulates the distinct controllers of
	// creatures whose hits LANDED on that snapshot holder. The one
	// adjudication runs after the loop, once per completed pass.
	initHad    bool             `clone:"deep"`
	initHolder state.PlayerID   `clone:"deep"`
	initCtrls  []state.PlayerID `clone:"deep"`
	// dealing marks a pass whose damage has started being dealt (damageStep's
	// fresh path stored its assignments) and whose completeCombatPass tail
	// has not run yet. A decision that is NOT a replacement-order ask can be
	// posed mid-pass -- a DamageDone replacement body's own ask (Phyrexian
	// Vindicator's "deals that much damage to any other target" target
	// choice) -- and runCombatAssignments deals the remaining assignments
	// under it, but completeCombatPass must wait for the answer. When the
	// Advance loop re-enters combatStep after that answer, dealing routes it
	// to the pass's completion instead of beginning the SAME pass again,
	// which re-dealt the already-dealt combat damage forever (cardfuzz batch5
	// line 10: choose -> damage -> replacement ask -> ... repeated).
	dealing bool `clone:"deep"`
}

// divChoice records one answered damage division: which attacker divided its
// combat damage, and the amount per live blocker in declaration order.
type divChoice struct {
	attacker state.ObjID
	amounts  []int32
}

// combatStep is the StepCombatDamage turn-based action (rules/turn.go's step()
// switch): run whichever damage pass is due, suspending on a controller
// damage-division decision or a between-passes priority round as required.
// It is re-entered through the Advance loop after a division answer resumes a
// pass, and through advanceStep after the between-passes priority round
// completes the first-strike pass and the regular pass must run.
func (e *Engine) combatStep() {
	if e.combatRound.dealing {
		// A pass already dealt (some or all of) its damage and was suspended
		// by a decision posed mid-pass (see combatRound.dealing): resume the
		// parked remainder, then run the pass's completion exactly once.
		pass := e.combatRound.pass
		if e.combatRound.assignments != nil {
			e.damageStep(pass)
		}
		if e.combatRound.assignments != nil || e.pending != nil {
			return
		}
		e.completeCombatPass(pass)
		return
	}
	if !e.combatRound.firstDone {
		e.combatRound.hasFirst = e.anyFirstStrike()
		if e.combatRound.hasFirst {
			// The first-strike damage step runs, then a priority round before
			// the regular step (CR 510.3/4).
			e.beginCombatPass(true)
			return
		}
		// No first striker: the regular pass is the whole of the step's
		// damage (CR 510.1).
		e.combatRound.firstDone = true
	}
	if !e.combatRound.regularDone {
		if e.combatRound.priorityPending {
			// The first-strike pass's between-passes priority round was owed
			// when its SBA tail posed a decision (completeCombatPass). The
			// ask is now answered -- Submit's SBA tail already re-ran -- so
			// run the deferred round. Its own pass once the round ends routes
			// through advanceStep's StepCombatDamage arm, which starts the
			// regular pass.
			e.combatRound.priorityPending = false
			e.priorityRound()
			return
		}
		e.beginCombatPass(false)
		return
	}
	// The regular pass's damage is dealt but its SBA tail was deferred for an
	// SBA decision: completeCombatPass returned before the transition. Finish
	// it now, exactly as the undeferred path would have.
	e.combatRound = combatRound{}
	e.setStep(state.StepEndCombat)
}

// beginCombatPass starts a combat damage pass: it collects the attackers that
// need a controller damage-division decision (asking them one at a time, so
// each ask suspends through the Advance loop) and, once every division is
// answered or none is owed, deals the pass via finishCombatPass.
func (e *Engine) beginCombatPass(pass bool) {
	e.combatRound.pass = pass
	e.combatRound.active = true
	e.combatRound.electQueue = e.asUnblockedNeeding(pass)
	e.combatRound.doneElect = nil
	e.combatRound.queue = e.divisionNeeding(pass)
	e.combatRound.done = nil
	e.combatRound.askAttacker = 0
	e.combatRound.askOptions = nil
	e.combatRound.askElection = false
	if e.askNextCombatAsk() {
		return // a combat decision is pending; Advance pauses on it
	}
	e.finishCombatPass()
}

// divisionNeeding returns the attackers of this pass whose combat damage must
// be divided by their controller (CR 510.1c): a non-trample attacker with
// power above zero and more than one live blocker, whose legal divisions are
// too numerous to enumerate is excluded and falls back to the deterministic
// assignment. Battlefield order, deterministic.
func (e *Engine) divisionNeeding(pass bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, e.G.Active) {
		a := e.G.Obj(id)
		if a == nil || !a.IsAttacking || a.Zone != state.ZBattlefield {
			continue
		}
		if !e.actsThisDamageStep(id, pass) {
			continue
		}
		if e.hasKeywordH(id, kwhTrample) || e.combatDamageAmount(id) <= 0 || len(e.liveBlockers(a)) < 2 {
			continue
		}
		if e.divisionCount(e.liveBlockers(a), e.combatDamageAmount(id)) > maxDivisionOptions {
			continue
		}
		out = append(out, id)
	}
	return out
}

// maxDivisionOptions bounds the number of damage-division options offered for
// one attacker, so a large power across many blockers does not flood the wire
// with thousands of choice. An attacker whose legal divisions exceed it is
// not asked; its damage is assigned deterministically (a Note is recorded).
const maxDivisionOptions = 128

// divisionCount returns the number of nonnegative compositions of power into
// n bins, i.e. C(power+n-1, n-1), saturating at maxDivisionOptions+1.
func (e *Engine) divisionCount(blockers []state.ObjID, power int32) int {
	n := len(blockers)
	if n <= 1 || power < 0 {
		return 1
	}
	// C(power+n-1, n-1), computed iteratively to stay within int.
	k := n - 1
	total := int(power) + k
	if k > total-k {
		k = total - k
	}
	res := int64(1)
	for i := 0; i < k; i++ {
		res = res * int64(total-i) / int64(i+1)
		if res > int64(maxDivisionOptions) {
			return maxDivisionOptions + 1
		}
	}
	return int(res)
}

// askNextCombatAsk poses the combat damage pass's next pending controller
// decision, or returns false when none remains (so the pass can be dealt).
// Elections (as-unblocked, stat:AssignCombatDamageAsUnblocked) are asked
// first, one at a time, then the damage-division decisions: an accepted
// election removes its attacker from the division queue entirely (the whole
// power goes to the defending player), so the two queues are drained in that
// fixed order. Building the ask and asking in one go keeps the pending-ask
// bookkeeping (askAttacker, askElection, askOptions) in lockstep with the
// pending Decision.
func (e *Engine) askNextCombatAsk() bool {
	if len(e.combatRound.electQueue) > 0 {
		a := e.combatRound.electQueue[0]
		e.combatRound.askAttacker = a
		e.combatRound.askElection = true
		e.choosing = chooseAsUnblockedElection
		e.ask(&decision.Decision{Player: e.G.Obj(a).Controller, Kind: decision.KChoose,
			Min: 1, Max: 1,
			Prompt: fmt.Sprintf("turn %d — have %s assign its combat damage as though it weren't blocked?",
				e.G.Turn, e.G.Obj(a).Face().Name),
			Options: []decision.Option{
				{Index: 0, Kind: "asunblocked", Label: "assign normally (blocked)", Obj: a,
					Player: e.G.Obj(a).Controller},
				{Index: 1, Kind: "asunblocked", Label: "assign as though not blocked", Obj: a,
					Player: e.G.Obj(a).Controller},
			}, Source: a})
		return true
	}
	e.combatRound.askElection = false
	return e.askNextDivision()
}

// asUnblockedNeeding returns this pass's attacking creatures whose controller
// is offered the stat:AssignCombatDamageAsUnblocked election (CR 509's
// optional "assign as though it weren't blocked"): a creature that WAS
// blocked (a genuinely unblocked creature's election is a no-op), with power
// above zero (an election over zero damage is a decision nobody could answer
// differently), not in the one shape where the outcome is already identical
// (Trample with no live blocker left routes the whole power to the player
// either way, Ruling T21-d), and whose static match is OPTIONAL (Optional$
// True -- every printed corpus carrier). A mandatory match (no Optional$) is
// auto-accepted into doneElect without an ask: the election is the
// controller's only when the card says "may", and a mandatory reading
// assigns as-unblocked unconditionally. No printed corpus static omits
// Optional$, so the mandatory arm is dead code kept for the shape's
// correctness (documented, deliberately untested -- out of scope per brief).
// Battlefield order, deterministic.
func (e *Engine) asUnblockedNeeding(pass bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, e.G.Active) {
		a := e.G.Obj(id)
		if a == nil || !a.IsAttacking || a.Zone != state.ZBattlefield {
			continue
		}
		if !e.actsThisDamageStep(id, pass) {
			continue
		}
		if len(a.BlockedBy) == 0 || e.combatDamageAmount(id) <= 0 {
			continue
		}
		if e.hasKeywordH(id, kwhTrample) && len(e.liveBlockers(a)) == 0 {
			continue
		}
		matched, mandatory := e.asUnblockedStaticMatches(id)
		if !matched {
			continue
		}
		if mandatory {
			e.combatRound.doneElect = append(e.combatRound.doneElect, id)
			continue
		}
		out = append(out, id)
	}
	return out
}

// chosenElection reports whether attacker a's as-unblocked election was
// accepted (or auto-accepted, mandatory) in the CURRENT pass, so damageStep
// routes its whole power to the defending player.
func (e *Engine) chosenElection(a state.ObjID) bool {
	for _, id := range e.combatRound.doneElect {
		if id == a {
			return true
		}
	}
	return false
}

// handleAsUnblockedElection applies an answered as-unblocked election: an
// accepted election records the attacker in doneElect (so damageStep routes
// its whole power to the defending player) and drops it from the division
// queue; a declined election leaves the ordinary assignment path untouched.
// It is the chooseAsUnblockedElection branch of handleChoose.
func (e *Engine) handleAsUnblockedElection(chosen []decision.Option) {
	// This flow consumed the pending choose: clear the marker so a later,
	// unrelated KChoose answer is not routed back into the election path
	// (the same reset handleDamageDivision performs).
	e.choosing = chooseNone
	if len(e.combatRound.electQueue) == 0 {
		// No pending election to consume: fall through to the pass rather
		// than stranding it (the same empty-answer fallback the division
		// handler keeps).
		e.finishCombatPass()
		return
	}
	a := e.combatRound.electQueue[0]
	e.combatRound.electQueue = e.combatRound.electQueue[1:]
	e.combatRound.askAttacker = 0
	e.combatRound.askElection = false
	if len(chosen) > 0 && chosen[0].Index == 1 {
		e.combatRound.doneElect = append(e.combatRound.doneElect, a)
		for i, id := range e.combatRound.queue {
			if id == a {
				e.combatRound.queue = append(e.combatRound.queue[:i], e.combatRound.queue[i+1:]...)
				break
			}
		}
	}
	if e.askNextCombatAsk() {
		return
	}
	e.finishCombatPass()
}

// askNextDivision asks the controller for the next unanswered damage division
// in this pass, or returns false when none remains (so the pass can be dealt).
// Building the option list and asking in one go keeps the option-to-split
// table (askOptions) in lockstep with the pending Decision.
func (e *Engine) askNextDivision() bool {
	if len(e.combatRound.queue) == 0 {
		return false
	}
	a := e.combatRound.queue[0]
	opts, table := e.divisionOptions(a)
	e.combatRound.askAttacker = a
	e.combatRound.askOptions = table
	e.choosing = chooseDamageDivision
	e.ask(&decision.Decision{Player: e.G.Active, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: fmt.Sprintf("turn %d — divide %s's combat damage among its blockers", e.G.Turn,
			e.G.Obj(a).Face().Name), Options: opts, Source: a})
	return true
}

// divisionOptions builds the KChoose option list for dividing attacker a's
// combat damage among its live blockers, plus the parallel per-option split
// table. Every nonnegative composition of the attacker's power into the
// blocker count is legal under the no-order CR 510.1c (this revision removed
// the declaration-order assignment rule), so the options enumerate them all;
// the split table lets the answer handler recover the chosen amounts.
func (e *Engine) divisionOptions(a state.ObjID) ([]decision.Option, [][]int32) {
	blockers := e.liveBlockers(e.G.Obj(a))
	pw := e.combatDamageAmount(a)
	n := len(blockers)
	var splits [][]int32
	var cur []int32
	var rec func(remaining int32, idx int)
	rec = func(remaining int32, idx int) {
		if idx == n-1 {
			cur = append(cur, remaining)
			splits = append(splits, append([]int32(nil), cur...))
			cur = cur[:len(cur)-1]
			return
		}
		for v := int32(0); v <= remaining; v++ {
			cur = append(cur, v)
			rec(remaining-v, idx+1)
			cur = cur[:len(cur)-1]
		}
	}
	rec(pw, 0)
	opts := make([]decision.Option, 0, len(splits))
	for i, sp := range splits {
		label := make([]byte, 0, 64)
		for j, bid := range blockers {
			if j > 0 {
				label = append(label, ',')
			}
			label = append(label, fmt.Sprintf("%d to %s", sp[j], e.G.Obj(bid).Face().Name)...)
		}
		opts = append(opts, decision.Option{Index: i, Kind: "division",
			Label: string(label), Obj: a, Player: e.G.Active, Amount: int(sp[0])})
	}
	return opts, splits
}

// finishCombatPass deals the current pass's damage and advances the combat
// damage step: for the first-strike pass it grants the between-passes priority
// round (CR 510.3/4); for the regular pass it moves to the end-combat step.
func (e *Engine) finishCombatPass() {
	pass := e.combatRound.pass
	e.dealDamagePass(pass)
	if e.combatRound.assignments != nil {
		return // a replacement-order decision parked this pass
	}
	e.completeCombatPass(pass)
}

// completeCombatPass performs the post-damage SBA and phase progression only
// after every assignment in the pass has landed. It is also called by the
// replacement-order resumption path.
func (e *Engine) completeCombatPass(pass bool) {
	e.combatRound.dealing = false
	e.combatRound.queue = nil
	e.combatRound.done = nil
	e.combatRound.askAttacker = 0
	e.combatRound.askOptions = nil
	e.combatRound.electQueue = nil
	e.combatRound.doneElect = nil
	e.combatRound.askElection = false
	if pass {
		e.combatRound.firstDone = true
		e.combatRound.active = false
		e.checkStateBased()
		if e.G.Over {
			return
		}
		// An SBA decision posed by checkStateBased (the CR 704.5j legend rule)
		// must not be displaced by the between-passes priority round: defer the
		// round (combatStep runs it after the answer) instead of overwriting
		// the ask, the same return-shape step() takes at its own head.
		if e.pending != nil {
			e.combatRound.priorityPending = true
			return
		}
		// CR 510.4: players gain priority after the first-strike damage step,
		// before the regular damage step runs.
		e.priorityRound()
		return
	}
	e.combatRound.regularDone = true
	e.combatRound.active = false
	e.checkStateBased()
	if e.G.Over {
		return
	}
	// Same displacement guard as the first-strike branch: an SBA decision
	// outstanding from the tail defers the end-of-combat transition to
	// combatStep rather than letting it run under the ask.
	if e.pending != nil {
		return
	}
	e.combatRound = combatRound{}
	e.setStep(state.StepEndCombat)
}

// handleDamageDivision applies an answered damage-division decision (CR
// 510.1c): it records the chosen split, then asks the next undone division or
// deals the pass. It is the chooseDamageDivision branch of handleChoose.
func (e *Engine) handleDamageDivision(chosen []decision.Option) {
	// This flow consumed the pending choose: clear the marker so a later,
	// unrelated KChoose answer is not routed back into the damage-division
	// path (the same reset discardCleanup performs).
	e.choosing = chooseNone
	if len(chosen) == 0 {
		// A no-option answer should not occur (Min == Max == 1); fall back to
		// the deterministic assignment rather than stranding the pass.
		e.finishCombatPass()
		return
	}
	idx := chosen[0].Index
	amounts := e.combatRound.askOptions[idx]
	e.combatRound.done = append(e.combatRound.done, divChoice{attacker: e.combatRound.askAttacker, amounts: amounts})
	e.combatRound.queue = e.combatRound.queue[1:]
	e.combatRound.askAttacker = 0
	e.combatRound.askOptions = nil
	if e.askNextCombatAsk() {
		return
	}
	e.finishCombatPass()
}

// chosenDivision returns the answered division for attacker a in the current
// pass, or nil when none was answered (so a deterministic assignment applies).
func (e *Engine) chosenDivision(a state.ObjID) []int32 {
	for _, dc := range e.combatRound.done {
		if dc.attacker == a {
			return dc.amounts
		}
	}
	return nil
}

// dealCombatDamage runs first-strike damage and then regular damage. Damage
// within a step is simultaneous: every amount is computed against pre-step
// state before any event is emitted, so two creatures that would kill each
// other both die.
func (e *Engine) dealCombatDamage() {
	if e.anyFirstStrike() {
		e.damageStep(true)
		e.checkStateBased()
		// The SBA tail can pose a CR 704.5j legend choice; the regular pass
		// must not deal under it (the same displacement guard completeCombatPass
		// takes). A direct-call fixture answers through the normal Submit path,
		// which resumes the step machinery.
		if e.pending != nil {
			return
		}
	}
	e.damageStep(false)
}

// dealDamagePass is the internal wrapper a combat damage pass uses: it deals
// one pass's damage, optionally consulting the controller-collected divisions
// in combatRound.done (CR 510.1c). dealCombatDamage (the whole two-pass
// helper used by direct-call fixtures and older tests) keeps damageStep.
func (e *Engine) dealDamagePass(pass bool) {
	e.damageStep(pass)
}

// combatDefenderTarget returns the recipient of an attacking creature's
// forward combat damage: the battle object when the creature is attacking a
// battle (CR 310.7), else the defending player. Object.AttackingBattle is set
// by events.Apply from the DeclareAttackers event and cleared with IsAttacking.
// A battle that has left the battlefield since the declaration returns
// ok=false, which the caller drops -- the recipient no longer exists, the same
// no-assignment shape a departed defender leaves. The ok flag, not a zero
// value, is the discriminator: seat 0 is a legal defending player, so
// (player 0, object 0) is a real assignment and cannot mean "no recipient".
func (e *Engine) combatDefenderTarget(a *state.Object) (state.PlayerID, state.ObjID, bool) {
	if a.AttackingBattle != 0 {
		if b := e.G.Obj(a.AttackingBattle); b != nil && b.Zone == state.ZBattlefield {
			return 0, a.AttackingBattle, true
		}
		return 0, 0, false
	}
	return a.Attacking, 0, true
}

// liveBlockers filters a's BlockedBy to blockers still actually on the
// battlefield: one may have left play (destroyed by a trick, sacrificed) in
// the gap between blocks being declared and damage being dealt.
func (e *Engine) liveBlockers(a *state.Object) []state.ObjID {
	var out []state.ObjID
	for _, bid := range a.BlockedBy {
		if b := e.G.Obj(bid); b != nil && b.Zone == state.ZBattlefield {
			out = append(out, bid)
		}
	}
	return out
}

// anyFirstStrike reports whether any attacker or its live blockers has First
// Strike or Double Strike, which is what decides whether dealCombatDamage
// runs a separate first-strike step at all (CR 510.5): with none, only the
// single regular damage step happens. Double Strike is not among the eight
// keywords this task registers as implemented (see the init below) -- a card
// that actually has it is not routed into real decks by the coverage gate --
// but the check costs nothing to leave in exactly as the brief specified it,
// so a Double-Strike creature that reaches combat some other way (a test, a
// future task) still behaves correctly rather than merely "not being asked
// about".
func (e *Engine) anyFirstStrike() bool {
	for _, id := range e.G.Zone(state.ZBattlefield, e.G.Active) {
		a := e.G.Obj(id)
		if a == nil || !a.IsAttacking {
			continue
		}
		if e.hasKeywordH(id, kwhFirstStrike) || e.hasKeywordH(id, kwhDoubleStrike) {
			return true
		}
		for _, bid := range e.liveBlockers(a) {
			if e.hasKeywordH(bid, kwhFirstStrike) || e.hasKeywordH(bid, kwhDoubleStrike) {
				return true
			}
		}
	}
	return false
}

// assignment is one pending damage event, computed against pre-step state so
// a whole damage step applies simultaneously (CR 510.2/510.4).
type assignment struct {
	toPlayer   state.PlayerID
	toObj      state.ObjID
	amount     int32
	lifelink   state.PlayerID
	hasLink    bool
	deathtouch bool
	// infect records that the dealing creature has infect (CR 702.90b): the
	// damage event carries the infect marker and rules' emit conversion
	// (Engine.convertInfectDamage) deals it as -1/-1 counters (a creature
	// recipient) or poison counters (the defending player) instead of marked
	// damage / life loss. A source granted infect by a static (Grafted
	// Exoskeleton) reads the same way, because HasKeyword reads the derived
	// keyword list.
	infect bool
	wither bool
	// from is the creature dealing this assignment (the attacker for its own
	// assignments, each blocker for its hit-back), kept so the damage emit
	// loop can set e.damaging (engine.go) and let protection prevent damage
	// from a protected source (CR 702.16d).
	from state.ObjID
}

// actsThisDamageStep reports whether id deals damage during this pass of
// dealCombatDamage (CR 510.5): Double Strike acts in both the first-strike
// and the regular step; First Strike (without Double Strike) acts only in
// the first-strike step; everything else acts only in the regular step.
func (e *Engine) actsThisDamageStep(id state.ObjID, firstStrike bool) bool {
	if e.hasKeywordH(id, kwhDoubleStrike) {
		return true
	}
	if e.hasKeywordH(id, kwhFirstStrike) {
		return firstStrike
	}
	return !firstStrike
}

// tallyCmdDamage records that the commander object from dealt amount combat
// damage to the player p (CR 903.10), by emitting the CmdDamage event that
// events.Apply folds into that player's cumulative commander-damage tally
// (state.Player.CmdDamage). It must only be called from the combat damage
// step, for a Player-targeted assignment whose damage actually landed (not
// prevented or replaced), in a Commander-format game -- the caller in
// damageStep arms all three, and the Apply case derives the commander's
// match-wide dense index (the same slot m30's genesis sizes and New's
// Commanders bookkeeping names) from g.Players[].Commanders, which is what
// keeps a commander keyed to the same slot for the whole match.
//
// The tally is carried in an event of its own rather than written directly:
// the existing Damage event does not record which commander the source was
// (events.Event's fields are append-only, so its field set is frozen), so a
// reconstruction starting from the log alone cannot re-derive the per-
// commander tally from the Damage events it already has. Recording the tally
// in a CmdDamage event -- appended after every earlier Kind, so no ordinal,
// hash chain or golden replay is affected -- makes that same log-only replay
// fold the tally back exactly, which is the deciding question the brief poses
// (and answers "carried in an event of its own"). It also keeps every state
// mutation on the events.Apply path, the build's standing invariant.
func (e *Engine) tallyCmdDamage(p state.PlayerID, from state.ObjID, amount int32) {
	e.emit(events.Event{Kind: events.CmdDamage, Player: p, Obj: from, Amount: amount})
}

// damageStep computes and then applies one round of combat damage --
// first-strike creatures only, or everyone else, per firstStrike. Every
// Power/Toughness/HasKeyword read above the emit loop happens before any
// Damage event this step produces is applied, which is what makes two
// creatures that would each kill the other both actually die instead of the
// first one's death sparing the second.
//
// Ruling T21-a (Task 21 fix round 1): a first-strike attacker's own forward
// damage (to its blockers or the defending player) is gated on
// actsThisDamageStep, exactly as before, but the "blockers hit back" section
// below is not -- it used to sit inside the same gate, so a first-strike
// attacker skipped its own regular-step turn (correctly) but that same
// `continue` also skipped ever collecting a surviving, non-first-strike
// blocker's regular-step damage back at it. Each blocker's own hit-back
// entry is now independently gated on that blocker's own
// actsThisDamageStep, which is the only thing CR 510.4 actually conditions
// it on.
func (e *Engine) damageStep(firstStrike bool) {
	// A KReplacement answer resumes the already-computed simultaneous pass;
	// never rebuild assignments from post-replacement state.
	if e.combatRound.assignments != nil {
		e.runCombatAssignments()
		return
	}
	// api:Fog (CR 701.14a, effects/fog.go): a Fog-registered continuous
	// effect prevents ALL combat damage this turn. The check sits here, at
	// the top of each damage pass, rather than per-assignment: "combat
	// damage that would be dealt this turn" is a whole-turn fact, and the
	// continuous registry is event-derived state a replay rebuilds
	// identically (the registrations happen during effect resolution, which
	// the replay re-executes). The Note marks the pass in the transcript;
	// the step's own structure (priority rounds either side) is untouched,
	// exactly as the CR's prevent-damage reading requires.
	if e.fogActive() {
		e.emit(events.Event{Kind: events.Note, Obj: 0,
			Text: "all combat damage this turn is prevented"})
		return
	}
	var as []assignment
	for _, aid := range e.G.Zone(state.ZBattlefield, e.G.Active) {
		a := e.G.Obj(aid)
		if !a.IsAttacking || a.Zone != state.ZBattlefield {
			continue
		}
		blockers := e.liveBlockers(a)

		if e.actsThisDamageStep(aid, firstStrike) {
			if pw := e.combatDamageAmount(aid); pw > 0 {
				link := e.hasKeywordH(aid, kwhLifelink)
				dt := e.hasKeywordH(aid, kwhDeathtouch)
				trample := e.hasKeywordH(aid, kwhTrample)
				inf := e.hasKeywordH(aid, kwhInfect)
				wit := e.hasKeywordH(aid, kwhWither)
				damageToDefender := func(amount int32) {
					tp, to, ok := e.combatDefenderTarget(a)
					if !ok {
						return
					}
					as = append(as, assignment{toPlayer: tp, toObj: to, amount: amount,
						lifelink: a.Controller, hasLink: link, from: aid, infect: inf, wither: wit})
				}
				switch {
				case e.chosenElection(aid):
					// stat:AssignCombatDamageAsUnblocked (CR 509's optional
					// "assign as though it weren't blocked"): the controller's
					// accepted election routes the WHOLE power to the defending
					// player and nothing to any blocker -- the same shape an
					// unblocked attacker takes. This case sits first so it also
					// covers Ruling T21-d's blocked-but-blockers-all-left shape
					// (an accepted election deals to the player even without
					// Trample) and the ordinary blocked shape. The blockers
					// still hit back below; only the ATTACKER's assignment is
					// rerouted.
					damageToDefender(pw)

				case len(a.BlockedBy) == 0:
					// Genuinely unblocked: full damage to the defender.
					damageToDefender(pw)

				case len(blockers) == 0:
					// Ruling T21-d (CR 509.1h): a creature that was blocked
					// stays blocked for the rest of combat even if every
					// creature blocking it has since left -- it deals no
					// combat damage at all, unless Trample lets the whole
					// amount push through to the player instead (there is no
					// blocker left to owe any of it to).
					if trample {
						damageToDefender(pw)
					}

				default:
					// Ruling T21-b (CR 510.1c): lethal damage to each
					// blocker, in declaration order, before any spills to
					// the next. Which blocker(s) receive more than lethal
					// when Trample is absent and power exceeds every
					// blocker's combined toughness is really the attacking
					// player's choice (CR 510.1a); turning that into a real
					// decision is new scope this fix does not take on, so
					// the deterministic approximation is: every blocker
					// except the last is capped at its own need, and the
					// last absorbs whatever remains (Trample instead caps
					// every blocker, spilling any true excess to the
					// defending player below).
					//
					// Task jj-cmb (F40): a NON-trample attacker with more than
					// one blocker and few enough legal divisions has already
					// had its division chosen by its controller (a CR 510.1c
					// KChoose, collected in combatRound.done by the combat
					// damage step machinery before this pass is dealt) -- so
					// that controller-chosen split is used here instead of the
					// greedy approximation. Trample's excess-to-player
					// division is still the deterministic assignment (see
					// divisionNeeding), and a direct-call fixture that never
					// asked has no division recorded and keeps the greedy
					// behaviour.
					if div := e.chosenDivision(aid); div != nil {
						for i, bid := range blockers {
							if i < len(div) && div[i] > 0 {
								as = append(as, assignment{toObj: bid, amount: div[i],
									lifelink: a.Controller, hasLink: link, deathtouch: dt, from: aid, infect: inf, wither: wit})
							}
						}
						break
					}
					remaining := pw
					for i, bid := range blockers {
						need := e.Toughness(bid)
						if dt {
							need = 1
						}
						give := remaining
						if (trample || i < len(blockers)-1) && give > need {
							give = need
						}
						as = append(as, assignment{toObj: bid, amount: give,
							lifelink: a.Controller, hasLink: link, deathtouch: dt, from: aid, infect: inf, wither: wit})
						remaining -= give
						if remaining <= 0 {
							break
						}
					}
					if remaining > 0 && trample {
						damageToDefender(remaining)
					}
				}
			}
		}

		// Blockers hit back -- independent of whether the attacker itself
		// acted this step above (Ruling T21-a).
		for _, bid := range blockers {
			if !e.actsThisDamageStep(bid, firstStrike) {
				continue
			}
			if bp := e.combatDamageAmount(bid); bp > 0 {
				as = append(as, assignment{toObj: aid, amount: bp,
					lifelink: e.G.Obj(bid).Controller, hasLink: e.hasKeywordH(bid, kwhLifelink),
					deathtouch: e.hasKeywordH(bid, kwhDeathtouch), from: bid,
					infect: e.hasKeywordH(bid, kwhInfect), wither: e.hasKeywordH(bid, kwhWither)})
			}
		}
	}
	// One damage pass is ONE damage batch (CR 510.4): every Damage event the
	// emit loop below produces latches the DamageDealtOnce/DamageDoneOnce
	// triggers together and accumulates their referent amounts, closed (and
	// the referent totals patched) when the pass finishes dealing. The
	// first-strike pass and the regular pass are separate calls of this
	// function, hence separate batches -- a double striker triggers a bearer's
	// Jitte once per step, twice for the attack.
	e.openDamageBatch()
	// CR 510.2 makes every assignment in this pass one simultaneous damage
	// event. Begun here (the fresh, not-yet-parked path) rather than with a
	// defer inside runCombatAssignments, because a parked replacement-order
	// choice returns out of that function early and re-enters it later
	// (damageStep's e.combatRound.assignments != nil branch): the batch must
	// stay open across that suspension and close only once the whole
	// simultaneous pass has actually finished, in runCombatAssignments below.
	e.BeginLifeLossBatch()
	e.combatRound.assignments = as
	e.combatRound.damageNext = 0
	e.combatRound.dealing = true
	e.runCombatAssignments()
}

// runCombatAssignments applies the preserved pass from its first unfinished
// assignment. A replacement-order ask returns immediately, keeping the next
// index and every later assignment parked until handleReplacement resumes it.
func (e *Engine) runCombatAssignments() {
	// CR 726.2 snapshot, taken when the pass STARTS dealing (damageNext == 0;
	// a replacement-order re-entry always resumes at damageNext >= 1, so this
	// runs once per pass): the whole simultaneous pass is ONE adjudication, so
	// who held the initiative is fixed when dealing begins, not re-read per
	// hit -- this very pass moves it. Cleared here, accumulated in the loop,
	// and judged once in this function's tail.
	if e.combatRound.damageNext == 0 {
		e.combatRound.initHad = e.G.HasInitiative
		e.combatRound.initHolder = e.G.Initiative
		e.combatRound.initCtrls = e.combatRound.initCtrls[:0]
	}
	for i := e.combatRound.damageNext; i < len(e.combatRound.assignments); i++ {
		x := e.combatRound.assignments[i]
		// e.damaging names the dealing creature for the whole of this
		// assignment so emit's protection check (Task 15) can prevent the
		// damage when the recipient is protected from it (CR 702.16d); reset
		// before the next assignment.
		e.damaging = x.from
		// combatDamaging marks THIS assignment's Damage event as combat damage
		// (see engine.go): trigmatch.DamageMatches reads it inside the emit's
		// synchronous checkTriggers, and a prevented hit (the protection Note
		// substituted for the Damage event) never reaches it.
		e.combatDamaging = true
		var prevented bool
		dealt := x.amount
		if x.toObj != 0 {
			// Task 15 fix round 1 (Critical C1): the return value of the
			// Damage emit is read here. emit swallows a protected permanent's
			// damage and returns a Note instead of a Damage event, but the
			// FOLLOW-ON emits that ride the damage -- the deathtouch marker and
			// the lifelink life gain -- used to run regardless, so a 2/2 blue
			// Merfolk with Lifelink + Deathtouch blocked by a creature with
			// protection from blue would both slap a Deathtouched counter on
			// it (lethal under CR 704.5g) and gain its controller life (CR
			// 702.15a: lifelink triggers only on damage actually dealt) even
			// though the damage itself was prevented. Checking the emitted
			// event's Kind -- the recipient-armed bet here is that a Note (or
			// any replacement-substituted non-Damage kind) means the damage
			// did NOT land -- skips both riders for a prevented assignment.
			dam := events.Event{Kind: events.Damage, Obj: x.toObj, Amount: x.amount}
			if x.infect && e.IsCreature(x.toObj) {
				// CR 702.90b: a CREATURE recipient takes infect damage as
				// -1/-1 counters; the compound marker rides Damage's Counter
				// carrier (both facts -- the source's infect and the recipient's
				// layer-accurate creature classification -- which the fold
				// reuses) and Engine.convertInfectDamage places them as a real
				// CounterChange right after this event folds, through the same
				// replacement/trigger pipeline every other placement uses.
				// Classifying rather than assuming keeps the marker honest for
				// any future shape that lands a blocker-shaped hit on a
				// non-creature (a Battle, a redirected hit): that recipient
				// goes untagged and takes ordinary marked damage. Preceding
				// replacements (protection is handled before them, in emit)
				// still act on the event, so a rewritten amount converts as the
				// rewritten amount.
				dam.Counter = "infect+creature"
			} else if x.wither {
				// Engine.emit recomputes the recipient half after redirects.
				dam.Counter = "wither"
			}
			ev := e.emit(dam)
			prevented = ev.Kind != events.Damage
			if !prevented {
				dealt = ev.Amount
			}
			if x.deathtouch && !prevented && ev.Obj != 0 {
				e.emit(events.Event{Kind: events.CounterChange, Obj: ev.Obj,
					Counter: "Deathtouched", Amount: 1})
			}
		} else {
			// Combat damage to a player. The existing (Task 15) protection
			// prevention path only armed the Obj branch; the player branch
			// never read the emit's return value. In a Commander-format game
			// (CR 903.10, Task m33) this branch must know whether the damage
			// actually landed before it tallies commander damage -- so it
			// reads the returned kind exactly the way the Obj branch already
			// does, and only tallies when it is still a Damage event (a
			// replacement-substituted Note means prevented/replaced damage,
			// which must not add to the tally). Reusing `prevented` here also
			// makes the shared lifelink rider below skip a prevented
			// player-hit, consistent with the Obj branch. Non-Commander games
			// take the original single-emit path untouched, so this task
			// changes nothing about them.
			dam := events.Event{Kind: events.Damage, Player: x.toPlayer, Amount: x.amount}
			if x.infect {
				// CR 702.90b: that many poison counters instead of life loss;
				// the marker rides Damage's Counter carrier and
				// Engine.convertInfectDamage emits a real PlayerCounterChange
				// right after this event folds, so the placement goes through
				// the same replacement/trigger pipeline every other counter
				// placement does.
				dam.Counter = "infect"
			} else if x.wither {
				// A player normally takes ordinary Wither damage, but preserve
				// the source fact through the replacement pass: Palisade Giant
				// and similar DamageDone replacements may redirect the hit onto
				// a creature, where Engine.emit selects the counter form.
				dam.Counter = "wither"
			}
			ev := e.emit(dam)
			prevented = ev.Kind != events.Damage
			if !prevented {
				dealt = ev.Amount
				// A damage-redirection replacement can rewrite this
				// player-targeted event into a PERMANENT-targeted one
				// (Protector of the Crown, Palisade Giant's `Affected$
				// Self`/`Enchanted`/`Equipped` bodies): ev.Obj becomes the
				// receiving permanent and ev.Player is zeroed. That is still
				// a Damage event (so `prevented` is false), but NO player
				// was dealt damage -- both the commander tally and the
				// combat-hit ledger must skip it. Guarding on ev.Obj == 0
				// also keeps recording a redirect that retargets TO a
				// player (ev.Obj == 0, ev.Player = the new recipient).
				if ev.Obj == 0 {
					if e.format == FormatCommander {
						e.tallyCmdDamage(ev.Player, x.from, dealt)
					}
					// The PlayerCountDefinedRegistered$HasPropertywasDealtCombatDam
					// ageThisTurnBy ledger (effects.Host's
					// CombatDamageToPlayersThisTurn): capture the LANDED hit with
					// the dealing creature's stable *cards.Card face pointer, so a
					// token that dies before the read point is still matchable.
					// Engine-side and NO-EVENT -- a new event kind would move every
					// chain head and diverge every stored log. Only the player
					// branch records (the object branch above is untouched): the
					// property is only ever read about players.
					e.combatHitsThisTurn = append(e.combatHitsThisTurn, e.combatHit(ev.Player, x.from, dealt))
					// CR 724.2b: combat damage to the monarch makes the
					// damage-dealing player become the monarch. Emit this
					// transition only after confirming the damage landed.
					if e.G.HasMonarch && ev.Player == e.G.Monarch &&
						x.from != 0 && e.G.Obj(x.from) != nil {
						e.emit(events.Event{Kind: events.MonarchChange,
							Player: e.G.Obj(x.from).Controller})
					}
					// CR 726.2: "Whenever one or more creatures a player controls
					// deal combat damage to the player who has the initiative, the
					// controller of those creatures takes the initiative." The
					// whole simultaneous pass is ONE adjudication: this block only
					// records the controllers of creatures whose hits LANDED on the
					// holder SNAPSHOT (taken when dealing began -- the live field
					// must not be re-read, because this very rule moves it), and
					// runCombatAssignments' tail judges once: the candidate first in
					// turn order takes the initiative, exactly one InitiativeChange
					// folds and exactly one source-less "whenever a player takes
					// the initiative" venture trigger (CR 726.2) is queued with the
					// rest of the combat-damage triggers. Judging by turn order
					// rather than event order is the rule's own reading (CR 726.2
					// fires once for the pass, and "the controller of those
					// creatures" is a single designation), and it matches 726.5:
					// a controller who already holds the designation re-taking it
					// folds an idempotent InitiativeChange and still ventures.
					if e.combatRound.initHad && ev.Player == e.combatRound.initHolder &&
						x.from != 0 && e.G.Obj(x.from) != nil {
						ctrl := e.G.Obj(x.from).Controller
						known := false
						for _, c := range e.combatRound.initCtrls {
							if c == ctrl {
								known = true
								break
							}
						}
						if !known {
							e.combatRound.initCtrls = append(e.combatRound.initCtrls, ctrl)
						}
					}
					// CR 702.164 (toxic): a player dealt combat damage by a source
					// with toxic N ALSO gets N poison counters. Toxic modifies the
					// damage only by adding a second instruction, so it must not
					// change the damage itself (unlike infect, which replaces it) --
					// this is why the read lives here, on the player branch, and
					// not in the object branch above: toxic is player-only. The
					// readable N comes off the source's DERIVED keywords
					// (ToxicValue), so a granted toxic counts too. ev.Obj == 0 is
					// the load-bearing guard: a redirect that rewrote this hit to
					// a permanent means no player was dealt damage, so no poison
					// is placed (CR 702.164b triggers on damage to a player).
					if n := e.ToxicValue(x.from); n > 0 {
						e.emit(events.Event{Kind: events.PlayerCounterChange,
							Player: ev.Player, Counter: "POISON", Amount: int32(n)})
					}
				}
			}
		}
		if x.hasLink && !prevented {
			e.emit(events.Event{Kind: events.LifeChange, Player: x.lifelink, Amount: dealt})
		}
		e.damaging = 0
		e.combatDamaging = false
		if e.pending != nil && e.pending.Kind == decision.KReplacement {
			e.combatRound.damageNext = i + 1
			return
		}
	}
	// CR 726.2, the one adjudication of this simultaneous pass (see the
	// collection block above): among the distinct controllers whose creatures
	// landed combat damage on the holder snapshot, the one FIRST IN TURN ORDER
	// takes the initiative -- a turn-based action, not an event-order race.
	// AliveFrom(e.G.Active) walks the surviving seats in APNAP order from the
	// active player (the same "first in turn order" convention askBlockers
	// uses for CR 802.4); the first walk entry that is a candidate wins. One
	// InitiativeChange folds and one venture trigger is queued, no matter how
	// many hits (or how many distinct controllers) the pass carried. A
	// controller who already holds the designation wins cleanly: the fold is
	// idempotent (CR 726.3) and the venture still fires (CR 726.5).
	if e.combatRound.initHad && len(e.combatRound.initCtrls) > 0 {
		order := e.G.AliveFrom(e.G.Active)
		winner, found := state.PlayerID(0), false
		for _, p := range order {
			for _, c := range e.combatRound.initCtrls {
				if c == p {
					winner, found = p, true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			e.emit(events.Event{Kind: events.InitiativeChange, Player: winner})
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
				Controller: winner, InitiativeVenture: true})
		}
		e.combatRound.initCtrls = e.combatRound.initCtrls[:0]
	}
	e.closeDamageBatch()
	e.tape.ResolutionDone()
	e.combatRound.assignments = nil
	e.combatRound.damageNext = 0
	e.EndLifeLossBatch()
}

// maxHandSize is CR 514.1's DEFAULT maximum: at the beginning of a player's
// cleanup step, if their hand contains more cards than their effective
// maximum, they discard down to it. Task D1 verified no corpus effect
// modified it then; Reliquary Tower and Thought Vessel (SetMaxHandSize$
// Unlimited) do now, so cleanupStep reads the effective maximum through
// maxHandSizeFor below and this constant is only that read's default.
const maxHandSize = 7

// unlimitedHandSize is the stand-in value SetMaxHandSize$ Unlimited maps to:
// far above any hand a game can assemble, so the CR 514.1 discard never
// triggers for a player under a no-maximum effect.
const unlimitedHandSize = 1 << 20

// maxHandSizeFor is p's effective CR 514.1 maximum: the SetMaxHandSize$
// Continuous statics affecting p (Reliquary Tower's Affected$ You,
// "Unlimited"; a numeric value sets the maximum outright, Forge's
// StaticAbilityContinuous RULES layer reads both shapes), plus the
// Effect-delivered route (an Effect whose StaticAbilities$ SVar carries the
// same S: line -- Finale of Revelation's STHandSize, Wrenn and Seven's
// UnlimitedHand), else the default.
//
// Both routes are consulted through the ONE value grammar
// effects.HandSizeValueOK, so they cannot disagree about what a value means.
// The scan walks the printed statics first (in their deterministic
// activeStatics order), then the registered continuous effects (in active()
// order); the FIRST affecting static wins. Applying two at once has no rules
// meaning for a set -- CR 613 orders them by timestamp, and "no maximum" can
// only be overridden by another set, which the first-match reading
// approximates; the printed-before-Effect tie-break is that same
// approximation's deterministic choice.
func (e *Engine) maxHandSizeFor(p state.PlayerID) int {
	for _, sv := range e.activeStatics("Continuous") {
		raw := strings.TrimSpace(sv.ParamStr(cards.PKSetMaxHandSize))
		if raw == "" {
			continue
		}
		if !effects.MatchesPlayerSpecFrom(e.G, sv.ParamStr(cards.PKAffected), p, sv.Controller, sv.Source) {
			continue
		}
		if n, ok := effects.HandSizeValueOK(raw); ok {
			return n
		}
	}
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.SetMaxHandSize == "" {
			continue
		}
		if !effects.MatchesPlayerSpecFrom(e.G, ce.Affects, p, ce.Controller, ce.Source) {
			continue
		}
		if n, ok := effects.HandSizeValueOK(ce.SetMaxHandSize); ok {
			return n
		}
	}
	return maxHandSize
}

// cleanupStep is the CR 514 cleanup step's turn-based actions, Task D1
// adding CR 514.1 on top of the CR 514.2 body Task 21 owns. Ordering:
// CR 514.1 (discard down to maxHandSize) runs first, then the 514.2 body --
// the two are simultaneous under the rules (nothing about the discard
// affects what the 514.2 body clears and vice versa), so the engine picks
// this order and the discard is offered before any permanent cleanup is
// emitted, keeping the transcript's discard lines ahead of the damage-/
// effect-clear lines it is a turn-based action of the same step. Called from
// turn.go's priorityRound.
//
// When the active player's hand exceeds maxHandSize, this ASKS a KChoose
// "discard" decision -- exactly one option per card in hand, in hand-zone
// order (never built from a map), with Min == Max == the number to discard --
// and returns with the decision pending, suspending the cleanup step until
// the answer arrives (discardCleanup). With a hand of maxHandSize or fewer
// there is nothing to discard and no decision is asked at all -- a zero-option
// or zero-count decision must never reach a seat (brief decision 5). Only the
// ACTIVE player discards (CR 514.1); nobody else is asked during their turn's
// cleanup.
func (e *Engine) cleanupStep() {
	hand := e.G.Zone(state.ZHand, e.G.Active)
	limit := e.maxHandSizeFor(e.G.Active)
	if len(hand) > limit {
		n := len(hand) - limit
		opts := make([]decision.Option, 0, len(hand))
		for _, id := range hand {
			name := "a card"
			if o := e.G.Obj(id); o != nil && o.Face() != nil {
				name = o.Face().Name
			}
			opts = append(opts, decision.Option{Index: len(opts), Kind: "discard",
				Label: "Discard " + name, Obj: id, Player: e.G.Active})
		}
		e.choosing = chooseCleanup
		e.ask(&decision.Decision{Player: e.G.Active, Kind: decision.KChoose, Min: n, Max: n,
			Prompt:  fmt.Sprintf("turn %d — discard %d card(s) down to the hand-size limit", e.G.Turn, n),
			Options: opts})
		return
	}
	e.cleanupBody()
}

// cleanupBody is the CR 514.2 portion of the cleanup step, run exactly once
// per cleanup step. It removes damage marked on every permanent (combat or
// otherwise) EXCEPT one a stat:NoCleanupDamage static selects (CR 514.2's
// "Damage isn't removed from this creature during cleanup steps.", Ancient
// Adamantoise; Engine.noCleanupDamageKeeps), clears this turn's Deathtouched markers (a deathtouch mark lasts
// only as long as the damage it accompanied, CR 702.2c), and drops every
// "until end of turn" continuous effect the layer system is holding
// (Engine.EndOfTurnCleanup, layers.go -- built and tested since Task 19c, but
// nothing ever called it, so a resolved pump effect such as Giant Growth used
// to survive forever instead of expiring at the end of the turn it was cast
// in). Called either directly from cleanupStep when no discard is owed, or
// from discardCleanup after the discard answer is recorded -- never both, so
// the 514.2 actions are never doubled.
func (e *Engine) cleanupBody() {
	for _, p := range e.G.AliveFrom(0) {
		for _, id := range e.G.Zone(state.ZBattlefield, p) {
			o := e.G.Obj(id)
			if o == nil {
				continue
			}
			// A stat:NoCleanupDamage static (CR 514.2: "Damage isn't removed
			// from this creature during cleanup steps.", Ancient Adamantoise)
			// keeps the marked damage. Read here, at the one removal site, so a
			// static the cleanup step cannot see never drops damage.
			keepDamage := false
			for _, sv := range e.activeStatics("NoCleanupDamage") {
				// A Condition$/IsPresent$ gate is a read of arbitrary board
				// state, so a gated static whose gate is false must NOT keep
				// damage. staticGateHolds evaluates the same gate the layer
				// walk does (and records it for the memo re-check).
				if !e.staticGateHolds(sv) {
					continue
				}
				spec := strings.TrimSpace(sv.ParamStr(cards.PKValidCard))
				if spec == "" {
					continue
				}
				if e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {
					keepDamage = true
					break
				}
			}
			if o.Damage > 0 && !keepDamage {
				ev := events.Event{Kind: events.Damage, Obj: id, Amount: -o.Damage}
				if f := o.Face(); e.IsCreature(id) && f != nil && f.IsPlaneswalker() && !f.IsCreature() {
					ev.Counter = "creature"
				}
				e.emit(ev)
			}
			if n := o.Counter("Shield"); n > 0 {
				e.emit(events.Event{Kind: events.CounterChange, Obj: id,
					Counter: "Shield", Amount: -n})
			}
			if n := o.Counter("Deathtouched"); n > 0 {
				e.emit(events.Event{Kind: events.CounterChange, Obj: id,
					Counter: "Deathtouched", Amount: -n})
			}
		}
	}
	e.EndOfTurnCleanup()
}

// chooseCleanup is the chooseFor the pending KChoose discard decision belongs
// to (engine.go): it lets handleChoose route the answer to discardCleanup
// rather than to a cast/miracle flow or the no-flow Note fallback. It
// extends the chooseFor enum in its own file; iota+4 is pairwise distinct from
// the shared package set chooseCast=1 / chooseMiracle=2 (cast.go). The exact
// numbers only need to differ, never to be adjacent.
const chooseCleanup chooseFor = iota + 4

// chooseDamageDivision is the chooseFor for the combat damage step's
// controller damage-division decision (CR 510.1c, Task jj-cmb F40): it lets
// handleChoose route the KChoose answer to handleDamageDivision (combat.go)
// rather than to a cast/miracle flow or the no-flow Note fallback. Like
// chooseCleanup, it extends the chooseFor enum in combat.go; iota+5 is
// pairwise distinct from the shared package set (cast=1 / miracle=2 /
// cleanup=4), and the exact numbers only need to differ.
const chooseDamageDivision chooseFor = iota + 5

// chooseAsUnblockedElection is the chooseFor for the combat damage step's
// assign-as-unblocked election (stat:AssignCombatDamageAsUnblocked, CR
// 509's optional "assign as though it weren't blocked"): it lets handleChoose
// route the KChoose answer to handleAsUnblockedElection (combat.go). Like
// its siblings it extends the chooseFor enum in combat.go; chooseEcho+1 is
// pairwise distinct from the shared package set (cast=1 / etb=2 / miracle=3 /
// cleanup=4 / division=5 / mana=6.. / opening=10 / suspend=12 / station=13 /
// unlock=14 / cumulative=15 / triggeredcost=16 / manaunless=17 / riot=20 /
// echo=21).
const chooseAsUnblockedElection chooseFor = chooseEcho + 1

// chooseExert is the chooseFor for the declare-attackers step's exert
// election (CR 702.100a, task exert1): one KChoose per attacking creature
// carrying an offerable stat:OptionalAttackCost static, posed by askNextExert
// after the KAttackers declaration is recorded, still inside the
// declare-attackers step (the CR 702.100a "as it attacks" ask is a follow-up
// election inside the same step -- a disclosed approximation: nothing can
// respond between the declaration and the election). Option 0 is always the
// decline ("Don't exert"), the replicate/multikicker shape: botpolicy's
// KChoose default arm takes the first offer, so a bot never exerts.
const chooseExert chooseFor = chooseAsUnblockedElection + 1

// chooseEnlist is the chooseFor for the declare-attackers step's enlist
// election (CR 702.160a, task enlist1): one KChoose per attacking creature
// carrying `K:Enlist` that has at least one eligible creature to tap, posed
// by askNextEnlist (rules/enlist.go) BEFORE the declaration's DeclareAttackers
// events are emitted so an intervening-if reading enlistedThisCombat sees the
// answer. Option 0 is always the decline (the may), the replicate/exert shape:
// botpolicy's KChoose default arm takes the first offer, so a bot never
// enlists. chooseExert+1 was already taken (chooseTriggeredMandatory in
// rules/cumulative.go extends the same enum), so the free value is
// chooseSiege+1 (27), pairwise distinct from the shared package set
// (cast=1 .. exert=23 / triggeredMandatory=24 / commanderColor=25 /
// siege=26).
const chooseEnlist chooseFor = chooseSiege + 1

// exertAsk is the declare-attackers exert election's resumable state (the
// blockerRound plain-value precedent): the deterministic offer list, in the
// answered KAttackers declaration's option order, and the cursor of the ask
// currently outstanding. A nil/empty offer list means no election is owed;
// it is cleared when the cursor exhausts the list.
type exertAsk struct {
	offers []state.ObjID
	next   int
}

// discardCleanup applies an answered CR 514.1 discard decision: each chosen
// card moves from the active player's hand to their graveyard (a canonical
// discard MoveZone event per card, in the order the client selected them),
// then the CR 514.2 body runs (cleanupBody), then the turn hands to the next player's
// turn (advanceStep). The move events ride the ordinary emit path, so
// state-based actions and triggered abilities matched by the discard are
// queued exactly as for any other zone change and handled by the same
// machinery the next priority round already drives (see the 514.3 follow-up
// note in the task report: a cleanup-created trigger is placed at the next
// player's first priority rather than in a repeated cleanup step, because
// implementing the CR 514.3 repeated-step control flow -- the no-priority
// turn loop re-entering itself to grant priority and then redoing cleanup --
// was judged not a small, clearly-correct addition at this point of
// priorityRound; an honest recorded gap beats a speculative turn-loop
// change, and the brief directs exactly that).
//
// mayflashsac2 implemented exactly that tail (finishCleanupStep, turn.go):
// the discard and the 514.2 body are followed by the CR 514.3 tail the
// no-discard path shares -- a trigger the discard or the body queued is
// placed during the cleanup step and the players get priority while it
// resolves, instead of the old gap where a cleanup-created trigger waited
// until the next turn's first priority. advanceStep is reached only after
// EVERY part of the cleanup has run and nothing is waiting, so the step is
// never advanced mid-cleanup.
func (e *Engine) discardCleanup(chosen []decision.Option) {
	// Matching the cast flows (cast.go), clear the choosing marker this flow
	// itself set in cleanupStep: once the discard answer is recorded there is
	// no pending choose anymore, and leaving chooseCleanup behind would let a
	// later KChoose answered with no flow waiting route into the destructive
	// discard path (handleChoose's default arm is the no-flow fallback and
	// expects chooseNone here).
	e.choosing = chooseNone
	for _, opt := range chosen {
		e.emit(events.Discard(opt.Obj, e.G.Active))
	}
	e.cleanupBody()
	e.finishCleanupStep()
}

// Registered here: exactly the eight keywords this task actually implements
// (Flying/Reach gate blocking in combat.CanBlock; Haste and Vigilance gate/modify
// attacking in combat.CanAttack/handleAttackers; Deathtouch, Trample, Lifelink and
// First Strike are all read directly in damageStep above and
// destroyLethalDamage, sba.go), plus three the M2r ratchet adds, each with a
// named proof test in keyword_registration_test.go: Flash (legal.go's
// instant-speed gate), Indestructible (destroyLethalDamage and, via
// Host.HasKeyword, Destroy/DestroyAll) and Devoid (effects.ColorsOf). Double
// Strike is still deliberately NOT registered: it is read by
// anyFirstStrike/damageStep but has no proof test of its own, and registering
// a keyword the build only partially or incidentally handles would tell the
// coverage report -- and so the deck-builder gate downstream of it -- that a
// card carrying it is safe to play, which is worse than leaving it reported
// as unsupported.
func init() {
	effects.RegisterNonAPI("kw:Flying", "kw:Reach", "kw:Haste", "kw:Vigilance",
		"kw:Deathtouch", "kw:Trample", "kw:Lifelink", "kw:First Strike", "kw:Double Strike",
		// kw:Infect (CR 702.90): the conversion lives in events.Apply's Damage
		// fold, driven by the infect marker rules/combat.go and effects/damage.go
		// set on the event; rules need no keyword machinery of its own beyond
		// the HasKeyword read the combat path already makes.
		"kw:Infect", "kw:Wither", "kw:Backup",
		"kw:Flash", "kw:Indestructible", "kw:Devoid", "kw:Defender", "kw:Menace",
		"kw:Fear", "kw:Shadow", "kw:Horsemanship", "kw:Skulk",
		// kw:Intimidate (CR 702.13a): an Intimidate attacker is blocked only
		// by artifact creatures and/or creatures sharing a colour with it --
		// read directly in combat.CanBlock as a colour-intersection generalisation
		// of Fear. Proof test: TestIntimidateBlocksOnlyArtifactsAndSharedColors.
		"kw:Intimidate",
		// kw:Landwalk (CR 702.14): a walker can't be blocked while the
		// defending player controls a land of the named type. The family is
		// read directly in combat.CanBlock/combat.LandwalkEvades against the defender's
		// controlled lands, including the nonbasic/legendary/snow qualifier
		// forms the corpus spells. Proof test: TestLandwalkEvadesDefenderLands.
		"kw:Landwalk",
		// kw:Toxic (CR 702.164) is a static ability rules reads directly, the
		// way it reads Deathtouch/Lifelink: the poison instruction rides the
		// player branch of runCombatAssignments, reading the N off the
		// source's derived keywords (ToxicValue). Proof test:
		// TestToxicIxhelAddsPoisonOnCombatDamage.
		"kw:Toxic",
		// kw:Boast (CR 702.142) has no K: keyword line: Forge marks a Boast
		// ability with a `Boast$ True` parameter on the activated ability
		// itself, so Face.Primitives never surfaces it and this explicit
		// registration is what puts it on the coverage report. The gate
		// itself is the offer-time read in rules/legal.go's ability loop.
		"kw:Boast", "stat:AttackRestrict")
}
