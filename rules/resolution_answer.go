// resume_answer.go holds the first half of resumeResolution's answer-binding switch: the draw/mana/pay/ward/discard and single-choice arms.

// Code moved verbatim out of rules/resolution.go (moving code only; the
// suspension/resumption mechanism is documented at the top of resolution.go).
package rules

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// resumeAnswerBinding binds the answer of the resume kinds whose arms live
// here into the resumed Ctx. It reports TWO outcomes, mirroring the original
// single switch's two continuations:
//
//   - handled=false: the kind belongs to resumeAnswerBindingRest (this
//     switch's default arm); the CALLER invokes that helper exactly once.
//   - handled=true, stop=true: the arm ended in a bare `return` in the
//     original switch — it resolved further engine work itself (a nested
//     ask, a parked payment) and the caller (resumeResolution) must return
//     at once, without running the re-entry machinery.
//   - handled=true, stop=false: the arm bound the answer and falls through,
//     exactly as the original switch's `break`/fall-off arms did; the
//     caller continues WITHOUT consulting resumeAnswerBindingRest.
func (e *Engine) resumeAnswerBinding(rp *resumePoint, o *state.Object, ctx *effects.Ctx, chosen []decision.Option) (handled, stop bool) {
	switch rp.kind {
	case "dredge":
		// CR 702.55 replaces exactly the one draw that asked. The enclosing
		// Draw cursor advances only after the replacement (or declined
		// ordinary draw) completes; effDraw then re-enters at that cursor
		// and performs all remaining draws before its SubAbility$.
		if len(chosen) > 0 && chosen[0].Kind == "dredge" {
			e.applyDredge(rp.player, chosen[0].Obj)
		} else {
			e.resumeOrdinaryDraw(rp.player)
		}
		ctx.DrawDone = int32(rp.target + 1)
		if rp.uptoIdx >= 0 {
			// An Upto$ Draw's answered batch parked on this Dredge ask: the
			// rider (Decision.ResumeUptoIdx/Count -> the resume point)
			// restores the in-flight target so effDraw's upto branch
			// continues it instead of re-asking its decision.
			ctx.DrawUptoIdx = int32(rp.uptoIdx)
			ctx.DrawUptoCount = rp.uptoCount
			ctx.DrawUptoAnswered = true
		}
	case "mana_color":
		// A resolution-time Mana effect asked for one colour, or an
		// allocation of Combo's produced units. The answer is carried in
		// the chosen options' structured ManaSymbol and consumed by
		// effMana on re-entry; no event kind is needed because the
		// resulting ManaAdd is the replayable state mutation.
		for _, option := range chosen {
			// The chosen colour is structured data (Option.ManaSymbol);
			// labels are presentation-only.
			colour := option.ManaSymbol
			if len(colour) != 1 || !strings.Contains("WUBRG", colour) {
				continue
			}
			if len(chosen) == 1 {
				ctx.ManaChoice = colour
			} else {
				ctx.ManaChoices = append(ctx.ManaChoices, colour)
			}
		}
	case "repeat":
		// A RepeatEach loop re-entered after one of its iterations
		// suspended: no answer, just the cursor (CR 608.2c).
		if cur := rp.repeat; cur != nil {
			ctx.Repeat = &effects.RepeatCursor{SA: rp.sa, Subjects: cur.subjects, Next: cur.next,
				Last: cur.last, HasLast: cur.hasLast}
		}
	case "repeat_choose_order":
		// A RepeatEach ChooseOrder$ loop's before-the-loop ordering ask was
		// answered. Its loop frame is rp.outer (SuspendRepeat parked it with
		// ChooseOrder set); consume it here so the loop is re-entered exactly
		// once, with the subject order the answer named, rather than a
		// second time through the outer recursion. Each option's Index is
		// the subject's position in the offered (selector/scan) order, so
		// the answer is applied by permuting the cursor's subject slice --
		// the subjects are never re-derived after the ask, and every later
		// mid-loop suspension copies the reordered slice. The loop's own
		// accumulated bindings ride the frame exactly as the
		// repeat_each_optional arm carries them.
		if lf := rp.outer; lf != nil && lf.kind == "repeat" && lf.repeat != nil && lf.repeat.chooseOrder {
			ordered := append([]state.Target(nil), lf.repeat.subjects...)
			// A well-formed answer is a permutation (Min == Max == len); a
			// malformed one (unreachable past Decision.Validate) keeps the
			// offered order rather than dropping or duplicating a subject.
			if len(chosen) == len(ordered) {
				seen := make([]bool, len(ordered))
				ok := true
				for pos, o := range chosen {
					if o.Index < 0 || o.Index >= len(ordered) || seen[o.Index] {
						ok = false
						break
					}
					seen[o.Index] = true
					ordered[pos] = lf.repeat.subjects[o.Index]
				}
				if !ok {
					ordered = append([]state.Target(nil), lf.repeat.subjects...)
				}
			}
			ctx.Repeat = &effects.RepeatCursor{SA: rp.sa, Subjects: ordered,
				Next: lf.repeat.next, Last: lf.repeat.last, HasLast: lf.repeat.hasLast}
			if lf.loopBound {
				rp.loopBound = true
				rp.loopRemembered = append([]state.Target(nil), lf.loopRemembered...)
			}
			if lf.voteCounts != nil {
				rp.voteCounts = cloneVoteCounts(lf.voteCounts)
			}
			rp.outer = lf.outer
		}
	case "repeat_each_optional":
		// A RepeatEach RepeatOptionalForEachPlayer$ election was answered.
		// Its loop frame is rp.outer (SuspendRepeat parked it with Election
		// set); consume it here so the loop is re-entered exactly once, at
		// the OFFERED subject, rather than a second time through the outer
		// recursion. The loop's own accumulated Remembered (the frame's
		// loopRemembered, i.e. the suspension's Outer) replaces the
		// iteration Body SuspendRepeat bound to this head, so the re-entered
		// loop continues from the same bindings the first pass had. The
		// answer itself rides Ctx.RepeatEachOptional: effects runs subject
		// Next's body on a yes and skips it on a no.
		if lf := rp.outer; lf != nil && lf.kind == "repeat" && lf.repeat != nil && lf.repeat.election {
			ctx.Repeat = &effects.RepeatCursor{SA: rp.sa,
				Subjects: append([]state.Target(nil), lf.repeat.subjects...),
				Next:     lf.repeat.next, Last: lf.repeat.last, HasLast: lf.repeat.hasLast}
			ctx.RepeatEachOptional = &effects.RepeatEachOptionalContinuation{
				Next:   int32(lf.repeat.next),
				Accept: len(chosen) > 0 && chosen[0].Kind == "yes",
			}
			if lf.loopBound {
				rp.loopBound = true
				rp.loopRemembered = append([]state.Target(nil), lf.loopRemembered...)
			}
			if lf.voteCounts != nil {
				rp.voteCounts = cloneVoteCounts(lf.voteCounts)
			}
			rp.outer = lf.outer
		}
	case "repeat_optional_loop":
		if cur := rp.repeat; cur != nil {
			// The body of iteration cur.next-1 completed after its own
			// suspension: the repeat election for cur.next has not been
			// posed, so AskElection re-enters the loop at the election
			// rather than running the body directly.
			ctx.RepeatOptional = &effects.RepeatOptionalContinuation{Continue: true, Next: int32(cur.next),
				AskElection: true}
		}
	case "unless_pay":
		payOption, chosePay := unlessPayChoice(chosen)
		if rp.unlessPay != "" {
			ctx.UnlessPay = rp.unlessPay
			ctx.UnlessNext = rp.target
			if len(rp.unlessDiscards) > 0 {
				ctx.UnlessDiscarded = make([]state.Target, 0, len(rp.unlessDiscards))
				for _, id := range rp.unlessDiscards {
					ctx.UnlessDiscarded = append(ctx.UnlessDiscarded, state.Target{Obj: id})
				}
			}
			break
		}
		// The payer chose the explicitly marked pay option or declined.
		// Payment happens HERE, in rules, because payMana owns the cost grammar and
		// emits the ManaAdd events — so a replay re-derives the identical
		// payment. An answer to pay from a payer that cannot cover the cost
		// is a decline: the effect's body runs (or not) per its orientation,
		// Ward has non-mana payment forms (sacrifice, discard, tap and
		// several keyword-specific costs). Its payment handler owns those
		// choices; ordinary unless-pay effects retain the shared mana path.
		if rp.sa.API == "Ward" {
			if chosePay {
				paid, asked := e.beginWardPayment(rp, ctx)
				if asked {
					return true, true
				}
				if paid {
					ctx.UnlessPay = "pay"
				} else {
					ctx.UnlessPay = "decline"
				}
			} else {
				ctx.UnlessPay = "decline"
			}
			break
		}
		// Sacrifice's damage-payment offer (Vexing Devil's UnlessCost$
		// DamageYou<4>, UnlessPayer$ Opponent, UnlessSwitched$ True):
		// "paying" is TAKING THE DAMAGE, which the mana path below cannot
		// express — ParseCost silently substitutes an unknown spelling for
		// a flat {1} and would charge one floating mana for four damage.
		// Payment happens HERE, in rules (the same split that owns
		// payMana's events): the accepting opponent's Damage event is
		// emitted from the offering permanent, and the answered
		// UnlessPay re-enters effSacrifice, which then sacrifices (the
		// switched orientation: paying CAUSES the sacrifice). The resume
		// point's target cursor travels with the answer so the effect can
		// offer the next opponent after a decline.
		if rp.sa.API == "Sacrifice" {
			if n, dmg := effects.ParseDamageUnlessCost(rp.sa.Params["UnlessCost"]); dmg {
				if chosePay {
					e.payUnlessDamageCost(ctx, payOption.Player, n)
					ctx.UnlessPay = "pay"
				} else {
					ctx.UnlessPay = "decline"
				}
				// The answering payer's cursor travels in UnlessNext (the
				// same field every other unless-pay answer uses): a decline
				// re-entry resumes the offer at payers[idx+1], and the last
				// decline ends the ask. (UnlessPayTarget was a vestigial
				// second cursor nothing read — its one write is this line —
				// so a second opponent's decline re-offered payers[1]
				// forever.)
				ctx.UnlessNext = rp.target
				break
			}
			// A plain-mana UnlessCost$ (the echo / cumulative-upkeep
			// family) falls through to the shared mana path below, exactly
			// like a Counter's: paid spares the permanent, decline
			// sacrifices it. The unimplemented non-mana shapes never
			// reach the ask, so they never reach this arm.
		}
		// deterministically.
		// UnlessCostResolved first: an UnlessCost$ naming an SVar whose
		// count body resolves folds its numeric result into a generic
		// amount (Feather, Radiant Arbiter's SVar:CopyCost:Count$ChosenSize/
		// Times.2 -- "{2} for each of those creatures"), the same string
		// unlessProceed's ask label showed, so the offer and the charge can
		// never disagree. An SVar the ctx's table lacks or whose body does
		// not resolve passes through raw and lands in the same hard
		// decline as before.
		rawUnlessCost := effects.UnlessCostResolved(e, ctx, rp.sa)
		paid, ok := ParseUnlessCost(rawUnlessCost)
		if !ok {
			// I-5: an unless-cost the payment API cannot price is a hard
			// DECLINE. ParseCost("X") is {Generic:0, X:1}; payMana never
			// charges the unfolded X, so an empty pool "pays" it for free
			// and the counterspell stays inert. An unpriceable cost must
			// decline, never resolve at zero. This is the conservative
			// correct behaviour: a cleared counter is closer to the card
			// than a no-op. The named dynamic shapes close against the
			// resolution context: the announced-X binding (CR 601.2b,
			// including an announced zero), the resolvable SVar bodies (the
			// Counter/CopySpellAbility folds and every other API), the
			// DefinedCost_/DefinedSACost_ card-anchored mana values, the
			// energy parts (PayEnergy<N>/<X>, charged from the payer's
			// counters), the Return<N/Spec> choice parts (the payer's pick,
			// the beginUnlessPayment continuation) and LifeTotalHalfUp (the
			// payer's own life, folded at the gate and the charge).
			// ParseUnlessCost is still the strict parser: ExileFromGrave<...>,
			// Behold<...>, tapXType<...>, CopyCost, and every remaining
			// dynamic or unmodelled token declines here rather than
			// ParseCost's flat {1} substitution buying it for one generic.
			// The ask is still posed to the payer (the answer is recorded by
			// ModeChosen) but cannot succeed. Declining here (rather than
			// suppressing the ask in effects, which cannot import rules'
			// cost type) keeps the decision on the wire for hosts to observe
			// while never letting an empty pool satisfy it.
			ctx.UnlessPay = "decline"
		} else if chosePay && e.unlessCostPayable(payOption.Player, rawUnlessCost, ctx, rp.obj) {
			if len(paid.Sac) > 0 || len(paid.Discard) > 0 || len(paid.Reveal) > 0 || len(paid.Behold) > 0 || len(paid.RevealChosen) > 0 || len(paid.Return) > 0 || len(paid.Exile) > 0 {
				// Sacrifice, discard, reveal, behold, return and exile are
				// choice-bearing costs.
				// Park this resume before any mutation and let the payer
				// select every component; finishUnlessPayment re-enters
				// with unlessPay set, so this arm never charges it twice.
				e.beginUnlessPayment(payOption.Player, paid, ctx, rp.obj, rp)
				return true, true
			}
			// CR 601.2g: a mana-only unless cost gives the payer the same
			// chance to activate mana abilities before the charge as a cast
			// or a Ward does, so a converted colour (stat:ManaConvert) can be
			// produced by tapping. The window only opens when the pool
			// (under the payment's conversion) cannot already pay and an
			// untapped source exists; otherwise the charge below is
			// unchanged.
			if e.unlessManaWindowNeeded(payOption.Player, paid, rp.obj) {
				e.askUnlessWardMana(payOption.Player, paid, rp)
				return true, true
			}
			if e.payUnlessCost(payOption.Player, paid, ctx, rp.obj) {
				ctx.UnlessPay = "pay"
			} else if paid.hasManaPayment() && len(e.windowManaUnits(payOption.Player)) > 0 {
				// A failed pool-only attempt is not a decline: open the
				// CR 601.2g mana-ability window and resume this exact frame
				// after the payer has assembled enough floating mana. The
				// offer gate proved the budget reachable before Pay was
				// offered, so sources remain while the charge is unmet.
				e.beginUnlessPayment(payOption.Player, paid, ctx, rp.obj, rp)
				return true, true
			} else {
				ctx.UnlessPay = "decline"
			}
		} else {
			ctx.UnlessPay = "decline"
		}
		// The payer whose answer this is (the unlessProceed gate moves a
		// decline on to the next UnlessPayer$ payer, and a pay ends the
		// ask), threaded through the decision's ResumeTarget via the
		// resume point — the same channel the "choice" and "dig" arms use.
		ctx.UnlessNext = rp.target
	case "sacrifice_optional":
		// Optional$ + StrictAmount$ is a disjoint choice (decline, or
		// exactly Amount) that KChoose cannot represent. Its first KModes
		// answer records only the election; effSacrifice then asks an exact
		// KChoose if several complete batches are available.
		ctx.SacOptional = "decline"
		if len(chosen) > 0 && chosen[0].Index == 0 {
			ctx.SacOptional = "sacrifice"
		}
		ctx.SacOptionalTarget = rp.target
	case "sacrifice":
		// A player-targeted Sacrifice's KChoose (CR 701.21a: the
		// sacrificing player chooses which of their permanents) was
		// answered. The chosen options carry the object in Obj (the same
		// shape the "discard" and "dig" arms read), so the id list goes
		// straight to Ctx.SacPicks in the player's answer order; SacDone
		// distinguishes "answered, possibly with nothing" (an Optional$
		// decline) from the first pass, and SacTarget keeps the answer
		// attached to the exact Defined$ target that asked. effSacrifice
		// consumes and clears all three at the top of its own walk, so a
		// nested sacrifice cannot inherit the outer answer.
		ctx.SacPicks = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.SacPicks = append(ctx.SacPicks, o.Obj)
			}
		}
		ctx.SacDone = true
		ctx.SacTarget = rp.target
	case "ward_mana":
		if e.answerWardMana(rp, chosen, ctx) {
			return true, true
		}
	case "unless_mana":
		if e.answerWardMana(rp, chosen, ctx) {
			return true, true
		}
	case "ward_alt":
		// The Discard<...>:<mana> Ward alternative can choose its mana
		// half even when it is not already floating; it receives the same
		// CR 702.21a activation window as an ordinary numeric Ward.
		if len(chosen) == 1 && chosen[0].Kind == "ward_mana" {
			_, manaRaw, _ := strings.Cut(rp.sa.Params["UnlessCost"], ">:")
			cost := e.parseCost(manaRaw)
			if e.payMana(chosen[0].Player, cost) {
				ctx.UnlessPay = "pay"
			} else if cost.hasManaPayment() && e.hasUntappedManaSource(chosen[0].Player) {
				e.askWardMana(rp, &wardManaPayment{payer: chosen[0].Player, cost: cost,
					resumeKind: "ward_mana", prompt: "Activate mana abilities to pay Ward"})
				return true, true
			} else {
				ctx.UnlessPay = "decline"
			}
			break
		}
		fallthrough
	case "ward_blight", "ward_evidence", "ward_waterbend", "ward_tap", "ward_sac", "ward_discard":
		if e.settleWardPayment(rp.kind, rp.sa, ctx, chosen) {
			ctx.UnlessPay = "pay"
		} else {
			ctx.UnlessPay = "decline"
		}
	case "discard":
		// A mid-resolution discard choice ("Mode$ RevealYouChoose"
		// Thoughtseize/Duress — the CASTER picks out of the target's
		// revealed hand; or "Mode$ TgtChoose" Mind Rot / Faithless
		// Looting — the DISCARDING player picks out of their own hand)
		// was answered. The chosen options carry the object in Obj (the
		// same Obj a cleanup-step discard option carries), so the id
		// list is read straight off them — the one place a
		// mid-resolution answer moves an object by identity rather than
		// an SVar name, which is why Ctx carries a Discard []ObjID
		// rather than a Modes []string. api:Recruit's one-card discard ask
		// (effects/recruit.go) shares this exact answer channel.
		// Counter's UnlessCost$ and RearrangeTopOfLibrary (Ponder) can
		// both reuse this same answer-shape and resume retrofitted onto
		// their own asking primitive — see task-dc1-brief scope.
		ids := make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ids = append(ids, o.Obj)
			}
		}
		ctx.Discard = ids
		// The per-target cursor: DiscardTarget is the index (into the
		// effect's deterministic acting-player list) of the player whose
		// hand this answer was about, so the re-entered effDiscard applies
		// it to exactly that target and poses a fresh ask for every later
		// target (the RevealPickTarget/RevealOptTarget pattern). Without
		// it, a multi-target TgtChoose answered for target 0 re-asked
		// target 1 forever — the answer was always consumed at target 0's
		// cursor (0), whose hand never held target 1's chosen cards.
		ctx.DiscardTarget = rp.target
	case "discard_hand", "discard_may":
		// A "Mode$ Hand | Optional$ True" may-discard election (the
		// whole-hand wheel's "each player may discard their hand"), or a
		// TgtChoose Optional$ True one ("you may discard a land card",
		// Mox Diamond; "discard up to two cards"), was answered: option 0
		// is yes, anything else — option 1, an empty or malformed answer
		// — is a decline, the conservative read of an ambiguous one. The re-entered effDiscard applies the answer to
		// exactly the acting player this ask was posed for (the cursor)
		// and poses a fresh election for every later player; a declined
		// election discards nothing.
		ctx.DiscardVote = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.DiscardVote = "yes"
		}
		ctx.DiscardTarget = rp.target
	case "chooseevenodd":
		if len(chosen) > 0 {
			ctx.ChosenType = chosen[0].Label
		}
	case "choosetype":
		// A mid-resolution ChooseType ask (task ct1: SP$/AB$/DB$ ChooseType
		// resolving outside the cast-time "as this enters" choice —
		// Haunting Voyage's "Choose a creature type. Return ...") was
		// answered. The chosen option is the cast-time ask's own "type"
		// wire shape, so the Label IS the creature type the chooser
		// picked. The re-entered effChooseType emits the one Choose event
		// the fallback emits, with the answered type, so events.Apply
		// records o.ChosenType exactly the way every downstream reader
		// (Card.ChosenType / IsNotChosenType filters) already reads. The
		// effect consumes and clears the field (fx42 scoping), so a nested
		// ChooseType below poses its own ask.
		if len(chosen) > 0 {
			ctx.ChosenType = chosen[0].Label
		}
	case "choosecolor":
		// A mid-resolution ChooseColor ask (task
		// cli-20260923T060000Z-choose-color: SP$/AB$/DB$ ChooseColor
		// resolving outside the cast-time "as this enters" choice -- Wash
		// Out's "Return all permanents of the color of your choice") was
		// answered. The chosen option is the cast-time ask's own "color"
		// wire shape, so the Label IS the full colour name the chooser
		// picked. The re-entered effChooseColor emits the one Choose event
		// the fallback emits, with the answered colour's WUBRG letter, so
		// events.Apply records o.ChosenColor exactly the way every
		// downstream reader (Card.ChosenColor filters, devotion) already
		// reads. The effect consumes and clears the field (fx42 scoping),
		// so a nested ChooseColor below poses its own ask.
		if len(chosen) > 0 {
			ctx.ChosenColor = chosen[0].Label
		}
	case "changetext":
		// A mid-resolution api:ChangeText word ask (the Choose/
		// ChooseCreatureType/ChooseBasicLandType halves of a
		// ChangeColorWord$/ChangeTypeWord$ substitution) was answered.
		// Each answered option's Kind says which half it is
		// ("changetext_from"/"changetext_to"), so a one-pick ask and a
		// future combined ask use the same transport. The re-entered
		// effChangeText consumes and clears whichever halves it now has
		// (fx42 scoping), so a nested ChangeText asks its own words.
		for _, o := range chosen {
			switch o.Kind {
			case "changetext_from":
				ctx.ChangeTextFrom = o.Label
			case "changetext_to":
				ctx.ChangeTextTo = o.Label
			}
		}
	case "choosenumber":
		// A mid-resolution ChooseNumber ask (task
		// cli-20260923T060000Z-choose-number: SP$/AB$/DB$ ChooseNumber
		// resolving outside the cast-time "as this enters" choice -- Void's
		// "Choose a number. Destroy all artifacts and creatures with mana
		// value equal to that number") was answered. The chosen option is
		// the number list's own "number" wire shape, so the option's Amount
		// IS the number the chooser picked. The re-entered effChooseNumber
		// emits the one Choose event the fallback emits, with the answered
		// number, so events.Apply records o.ChosenNumber exactly the way
		// every downstream reader (Card.ChosenNumber filters, Void's
		// Count$ChosenNumber X) already reads. ZERO is a legal answer, so the
		// answered marker is a separate bool. The effect consumes and clears
		// both (fx42 scoping), so a nested ChooseNumber below poses its own
		// ask.
		ctx.ChosenNumberAnswered = true
		if len(chosen) > 0 {
			ctx.ChosenNumberPick = int32(chosen[0].Amount)
		} else {
			// A malformed empty answer (the ask is Min 1/Max 1): keep the
			// deterministic fallback 0 rather than inventing a number the
			// option list never offered.
			ctx.ChosenNumberPick = 0
		}
	case "choosenumbermulti":
		// One chooser's answer in a multi-chooser secret ChooseNumber
		// election (api:ChooseNumber's MatchedAbility$/UnmatchedAbility$
		// shape, Expert-Level Safe) was recorded. The accumulated answers
		// ride the decision's ResumeNumberPicks (rp.numberPicks) and the
		// asked chooser's index its ResumeTarget (rp.target);
		// effChooseNumber's election branch consumes both, appends this
		// answer, and asks the next chooser -- or, once every chooser has
		// answered, compares the picks and runs the matched or unmatched
		// SVar body. The transport is decision-scoped (never the sibling
		// "choosenumber" single-answer fields), so a nested ChooseNumber
		// cannot inherit an outer election's picks.
		ctx.ChooseNumberPicks = append([]int32(nil), rp.numberPicks...)
		ctx.ChooseNumberIndex = rp.target
		ctx.ChooseNumberDone = true
		if len(chosen) > 0 {
			ctx.ChooseNumberAnswer = int32(chosen[0].Amount)
		} else {
			// A malformed empty answer (the ask is Min 1/Max 1): keep the
			// deterministic fallback 0 rather than inventing a number the
			// option list never offered.
			ctx.ChooseNumberAnswer = 0
		}
	case "manareflected":
		// A standalone AB$ ManaReflected colour ask (the mid-resolution
		// choice effManaReflected poses when a DB$/SP$ body reflecting
		// several colours resolves outside the mana-activation path) was
		// answered. The chosen option's structured ManaSymbol carries the
		// picked colour; the re-entered effManaReflected consumes and
		// clears it and emits
		// the answered ManaAdd, so a nested ManaReflected poses its own ask.
		// An empty answer (malformed -- the ask is Min 1/Max 1 over a set of
		// two or more) leaves the field empty, and the effect's re-entry
		// degrades to its deterministic first candidate.
		if len(chosen) > 0 {
			// The structured mana symbol travels with the chosen option;
			// the option label is presentation-only.
			ctx.ManaReflectedColor = chosen[0].ManaSymbol
		}
	case "taporuntap":
		// A TapOrUntap's tap-vs-untap election (api:TapOrUntap, Merrow
		// Reejerey / Twiddle) was answered. Each offered option carries the
		// target it elected for in Obj and its choice in Kind ("tap" or
		// "untap"), so the answer is read straight off option 0. An empty or
		// malformed answer still sets the Done marker (the effect's Min 1/
		// Max 1 ask always has a legal single-option answer, so an empty one
		// is malformed, never a decline) and degrades to "tap" with no target
		// named — the conservative read, which the re-entered effect applies
		// to its first pending target. The effect consumes and clears all
		// three fields at the point of application (fx42 scoping), so a
		// later target poses its own ask.
		ctx.TapOrUntapDone = true
		if len(chosen) > 0 {
			ctx.TapOrUntapObj = chosen[0].Obj
			ctx.TapOrUntap = chosen[0].Kind
		}
	case "explore":
		// An Explore's LCI destination election (api:Explore, CR 701.35a:
		// "put the card back or put it into your graveyard") was answered.
		// The resume point carries the pending explorer (decision.ResumeTarget
		// = the explorer's id) and the answered option carries the revealed
		// card in Obj and the choice in Kind ("graveyard"/"top"), so the
		// re-entered effExplore applies the counter and the destination move
		// together, in CR order, then emits the record. An empty answer
		// (malformed — the ask's two options are always legal, Min 1/Max 1)
		// still sets the Done marker with no card: the effect's application
		// path guards the card's absence, so the record never names a stale
		// id. The effect consumes and clears all four fields at the point of
		// application (fx42 scoping), so the pending explorer's remaining
		// explores and every later target pose their own fresh path.
		ctx.ExploreDone = true
		ctx.ExploreObj = state.ObjID(rp.target)
		ctx.ExploreCount = rp.exploreDone
		if len(chosen) > 0 {
			ctx.ExploreChoice = chosen[0].Kind
			ctx.ExploreCard = chosen[0].Obj
		}
	case "connive":
		// A Connive's discard election (api:Connive, CR 702.59: draw N,
		// then discard N — the ask fires only when the hand holds more
		// than N cards, the strict-supersets discipline) was answered.
		// The resume point carries the pending conniver
		// (decision.ResumeTarget = the conniver's id) and every chosen
		// option carries the card to discard in Obj (the same shape the
		// "discard" arm reads), so the re-entered effConnive applies the
		// discards, the per-nonland +1/+1 counters and the record, then
		// every later conniving target poses its own fresh ask. An empty
		// answer (malformed — the ask's Min is N >= 1 over a hand larger
		// than N) still sets the Done marker with no picks: the effect's
		// application path guards the absence, so the record names only
		// what actually moved. The effect consumes and clears all three
		// fields at the point of application (fx42 scoping).
		ctx.ConniveDone = true
		ctx.ConniveObj = state.ObjID(rp.target)
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.ConniveDiscard = append(ctx.ConniveDiscard, o.Obj)
			}
		}
	case "copypermanent_choice":
		// CopyPermanent's sole Choices$/Chooser$ shape has its own
		// transport so a nested ordinary Choice cannot consume the answer.
		ctx.CopyPermanentChoiceDone = true
		if len(chosen) > 0 {
			ctx.CopyPermanentChoice = chosen[0].Obj
		}
	case "clone_choice":
		// A DB$ Clone Choices$ <filter> copy-source pick was answered:
		// the chooser named one object for the copy source. The effect
		// consumes and clears the field at the top of its walk (fx42
		// scoping), so a nested Clone cannot inherit the outer answer. A
		// malformed or empty answer leaves a zero id, which the re-entered
		// effect records as one loud Note and resolves as no copy -- never
		// a silent fall-through to an object the chooser did not name.
		ctx.ClonePickDone = true
		if len(chosen) > 0 {
			ctx.ClonePick = chosen[0].Obj
		}
	case "changezone_alternative":
		// The card owner chose the primary or alternative library position.
		// Carry the offered label through the re-entered ChangeZone body.
		if len(chosen) > 0 {
			ctx.ChangeZoneAlternative = chosen[0].Label
		}
	case "choosedirection":
		// A mid-resolution ChooseDirection ask (Aminatou's [-6], Order of
		// Succession) was answered. The chosen option's Label is the
		// direction word ("left"/"right"), so it is carried verbatim:
		// the re-entered effChooseDirection consumes it and the shared
		// Ctx then lets the chain's SubAbility$ (GainControl's
		// NextPlayerInChosenDirection / ChooseNextPlayerInChosenDirection)
		// read the same direction for the rest of the walk.
		if len(chosen) > 0 {
			ctx.ChosenDirection = chosen[0].Label
		}
	case "choice":
		// ChooseCard, ChoosePlayer and ChangeTargets all use KChoose. Keep
		// the concrete target shape rather than just an ObjID because player
		// zero is a real target too.
		ctx.Choice = make([]state.Target, 0, len(chosen))
		for _, o := range chosen {
			if o.Kind == "player" {
				ctx.Choice = append(ctx.Choice, state.Target{Player: o.Player, IsPlayer: true})
			} else if o.Obj != 0 {
				ctx.Choice = append(ctx.Choice, state.Target{Obj: o.Obj})
			}
		}
		ctx.ChoiceDone = true
	case "vote":
		// api:Vote's PLAYER ballot (task votepb1): the answer to one
		// voter's "vote for a player" KChoose. The accumulated picks ride
		// the decision's ResumeChoices (rp.choices) and the answered voter
		// index its ResumeTarget (rp.target); effPlayerVote consumes both,
		// appends this answer, and asks the next voter -- or completes and
		// publishes Ctx.VoteCounts for the chained AmountFromVotes$
		// reader. The transport is decision-scoped (never Ctx.Chosen), so
		// a nested vote cannot inherit an outer ballot's picks.
		ctx.VotePicks = append([]state.Target(nil), rp.choices...)
		ctx.VoteTarget = rp.target
		ctx.VoteDone = true
		for _, o := range chosen {
			if o.Kind == "player" {
				ctx.VoteAnswer = append(ctx.VoteAnswer, state.Target{Player: o.Player, IsPlayer: true})
			} else if o.Obj != 0 {
				ctx.VoteAnswer = append(ctx.VoteAnswer, state.Target{Obj: o.Obj})
			}
		}
	case "demonstrate":
		// The demonstrate trigger's answered ask (CR 702.152): which ask
		// rides the decision's ResumeTarget (rp.target -- 0 the may-copy
		// election, 1 the opponent choice); the election's yes/no answer
		// and the opponent pick are the chosen options. effDemonstrate
		// consumes and clears all four fields at the top of its walk (the
		// fx42 scoping discipline), so a nested Demonstrate below this
		// one poses its own asks.
		ctx.DemonstrateDone = true
		ctx.DemonstrateStage = rp.target
		for _, o := range chosen {
			switch o.Kind {
			case "yes":
				ctx.DemonstrateYes = true
			case "player":
				ctx.DemonstrateOpp = append(ctx.DemonstrateOpp, state.Target{Player: o.Player, IsPlayer: true})
			}
		}
	case "cipher":
		// The api:Cipher encode ask (CR 702.99a) was answered: an empty
		// answer declines, a picked creature is the encode host.
		// effCipher consumes and clears both fields at the top of its walk
		// (the fx42 scoping discipline), so a nested Cipher poses its own
		// ask.
		ctx.CipherDone = true
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.CipherPick = append(ctx.CipherPick, state.Target{Obj: o.Obj})
			}
		}
	case "opp_pick":
		// The TargetingPlayer$ Opponent controller-selection ask
		// (agent-20260925T085158Z-c861188d): the chosen "player" option
		// names the opponent who answers the re-entered ask. Recorded
		// under the SA's line; the walk's chooser read (midChooserCore,
		// reached through Engine.ChooserFor) consumes it. The arm sets
		// no Ctx fields: the re-entry re-runs the ask construction and
		// the pin redirects it to the chosen seat.
		if len(chosen) > 0 && chosen[0].Kind == "player" && rp.sa != nil {
			if e.oppPicksMid == nil {
				e.oppPicksMid = make(map[string]state.PlayerID)
			}
			e.oppPicksMid[rp.sa.Line] = chosen[0].Player
		}
	case "tgts":
		// The generic ValidTgts$ pre-ask (task mvts1) posed inside
		// effects.Resolve's dispatch loop. Same KChoose answer shape as
		// "choice", on its own resume kind and its own Ctx transport
		// (Ctx.TargetsPick) so another KChoose primitive resolving under
		// the same SA can never consume this answer. A MoveCounter SA
		// additionally RECORDS the set: its own kind/amount asks will
		// suspend it again later, and the re-entry after THOSE must
		// re-seed this answer or the target ask re-fires forever (the
		// movecounter1 livelock; seedMoveCounterAsk).
		ctx.TargetsPick = make([]state.Target, 0, len(chosen))
		for _, o := range chosen {
			if o.Kind == "player" {
				ctx.TargetsPick = append(ctx.TargetsPick, state.Target{Player: o.Player, IsPlayer: true})
			} else if o.Obj != 0 {
				ctx.TargetsPick = append(ctx.TargetsPick, state.Target{Obj: o.Obj})
			}
		}
		ctx.TargetsPickDone = true
		if rp.sa != nil && rp.sa.API == "MoveCounter" {
			e.moveCounterEntry(rp.obj).targets = append([]state.Target(nil), ctx.TargetsPick...)
		}
		// Every OTHER API records through the general cursor: the body
		// this answer is about to run may itself suspend (Kozilek's
		// Command's Scry poses its KArrange), and the resume after THAT
		// rebuilds the Ctx from scratch. Without the record the pre-ask
		// fires again and the two asks alternate forever.
		e.recordTargetsPick(rp.obj, rp.sa, ctx.TargetsPick)
	case "damage_split":
		// DealDamage's DividedAsYouChoose$ allocation answer: the KChoose
		// offered one option per chosen target (Min == Max == the named
		// total, Repeatable), so the answer is a multiset whose per-option
		// multiplicity is the damage that option's target receives. Every
		// option index is the target's position in the resolution's
		// Defined$ order, so the re-entered effDealDamage reads the shares
		// positionally. A target the answer never picked is simply absent
		// (zero damage), which the primitive's positional read treats as
		// nothing.
		n := 0
		for _, o := range chosen {
			if o.Index+1 > n {
				n = o.Index + 1
			}
		}
		ctx.DamageSplit = make([]int32, n)
		for _, o := range chosen {
			if o.Index >= 0 && o.Index < len(ctx.DamageSplit) {
				ctx.DamageSplit[o.Index]++
			}
		}
		ctx.DamageSplitDone = true
	case "search":
		// A hidden-library KChoose answer is an ordered subset. Preserve
		// that order for ChangeZone's MoveZone sequence, and set a separate
		// marker so choosing no cards still means "answered; do not re-ask".
		ctx.Search = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.Search = append(ctx.Search, o.Obj)
			}
		}
		ctx.SearchDone = true
		ctx.LibraryTarget = rp.target
	case "search_confirm":
		// An Optional$ confirmation on a hidden-library ChangeZone
		// search was answered (Forge's confirmAction gate, which runs
		// before the fetch list is consulted): option zero accepts this
		// search player's fetch, every other answer declines it.
		// effSearchLibrary consumes and clears these at the top of its
		// walk (fx42 scoping), so a nested search poses its own
		// confirmation. ResumeTarget is the per-library cursor, the same
		// one LibraryTarget carries for the answered pick.
		ctx.SearchConfirmDone = true
		ctx.SearchConfirmTarget = rp.target
		ctx.SearchConfirm = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.SearchConfirm = "yes"
		}
	case "search_mayshuffle":
		// A ChangeZone search carrying ShuffleNonMandatory$ True (Path to
		// Exile, Stoneforge Mystic, Boggart Harbinger) asked its searcher
		// "Shuffle your library?" after the search's moves landed. The
		// answer is a bare yes/no, recorded here as a marker the
		// re-entered effect consumes and clears (fx42 scoping): "yes"
		// emits the same Secret events.Shuffle every library shuffle
		// emits, "no" -- the information-mercy Forge's flag names -- keeps
		// the library order. The moved list rides the ask
		// (Decision.ResumeMoved -> rp.moved) so the re-entry can finish
		// with the LibraryPosition$ placement after the answered shuffle.
		// A malformed or empty answer keeps the order, the conservative
		// read of an ambiguous one.
		ctx.SearchShuffle = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.SearchShuffle = "yes"
		}
		ctx.SearchShuffleMoved = append([]state.ObjID(nil), rp.moved...)
		ctx.LibraryTarget = rp.target
	case "attach_optional":
		// An Optional$ True Attach's yes/no election (Ajani's Chosen's
		// "you may attach it to the token") was answered. The answer is a
		// bare yes/no, recorded here as a marker the re-entered effect
		// consumes and clears (fx42 scoping): "yes" attaches the resolved
		// object to the first legal Defined$ target, "no" -- the decline --
		// emits no Attach and the chained SubAbility$ still runs. A
		// malformed or empty answer keeps the decline, the conservative
		// read of an ambiguous one.
		ctx.AttachOpt = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.AttachOpt = "yes"
		}
	case "investigate_optional":
		// An Optional$ True Investigate's per-player may-investigate
		// election (Will the Wise's "each opponent may investigate",
		// Nick Valentine's "you may investigate") was answered for the
		// player the ask named: Decision.ResumeTarget is the cursor index
		// into the actingPlayers walk the re-entered effInvestigate
		// re-derives (the same-Remembered ride keeps a Remembered-valued
		// Defined$ deriving the SAME list the cursor indexes). The answer
		// is a bare yes/no, recorded here as the marker the re-entered
		// effect consumes and clears at the point of application (fx42
		// scoping): "yes" mints the Num$ Clues and, with
		// RememberInvestigatingPlayers$ True, remembers the acceptor;
		// "no" — the decline — does neither, and the chained SubAbility$
		// still runs either way. A malformed or empty answer keeps the
		// decline, the conservative read draw_optional and attach_optional
		// take.
		ctx.InvestigateOptIdx = int32(rp.target)
		ctx.InvestigateOpt = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.InvestigateOpt = "yes"
		}
	case "copy_optional":
		// An Optional$ True CopySpellAbility's may-copy election
		// (Sevinne's Reclamation's "you may copy this spell", and the
		// corpus's wider may-copy family) was answered. Option 0 is "yes";
		// anything else (option 1, an empty or malformed answer) is the
		// decline -- the conservative read of an ambiguous answer. The
		// re-entered effCopySpellAbility consumes and clears Ctx.CopyOpt:
		// "yes" makes the copy through the ordinary path, "no" makes
		// none and the chained SubAbility$ still runs.
		ctx.CopyOpt = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.CopyOpt = "yes"
		}
	case "surveil_look_optional":
		// The stat:SurveilNum optional "you may look at an additional N
		// cards each time you surveil" election (Enhanced Surveillance)
		// was answered. Each Optional$ static is an independent may effect,
		// so the ask offered one option per optional static and the answer
		// is the ACCEPTED subset: the accepted ordinals ride
		// Ctx.SurveilLookOpt as a CSV done-marker the re-entered effSurveil
		// consumes and clears (fx42 scoping), each accepted ordinal adding
		// that static's Num$ to THE ASKING PLAYER's surveil count. An empty
		// answer is the real decline of every static ("no", the Min-0
		// Optional answer); a malformed one keeps the decline, the
		// conservative read attach_optional takes.
		ctx.SurveilLookOpt = "no"
		if len(chosen) > 0 {
			parts := make([]string, 0, len(chosen))
			for _, o := range chosen {
				parts = append(parts, strconv.Itoa(o.Index))
			}
			ctx.SurveilLookOpt = strings.Join(parts, ",")
		}
	case "attach_choice":
		// A Choices$ Attach's card choice was answered (Goldwardens'
		// Gambit's "for each of those tokens, you may attach an Equipment
		// you control to it", unexpected_request's "you may attach an
		// Equipment you control", Breath of Fury's "attach CARDNAME to a
		// creature you control"). The chosen card ids are recorded for the
		// re-entered effect to consume and clear (fx42 scoping): with no
		// Object$ the ids name the OBJECT to attach, with Object$ present
		// they name the DESTINATION. An empty answer on the Min-0 Optional
		// shape is a real decline, so AttachChoiceDone distinguishes it
		// from an unanswered ask (the ctx.Search/SearchDone discipline).
		// The asking pass's resolved destination list rides back in
		// rp.choices (Decision.ResumeChoices) -- a RepeatEach body's
		// Defined$ Imprinted binding does not survive the suspension, so
		// the re-entry must not re-derive it.
		ctx.AttachChoice = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.AttachChoice = append(ctx.AttachChoice, o.Obj)
			}
		}
		ctx.AttachChoiceDone = true
		ctx.AttachDests = make([]state.ObjID, 0, len(rp.choices))
		for _, t := range rp.choices {
			if !t.IsPlayer && t.Obj != 0 {
				ctx.AttachDests = append(ctx.AttachDests, t.Obj)
			}
		}
	case "attach_player_choice":
		// A PlayerChoices$ Attach's player choice was answered (Curse of
		// Leeches' `DB$ Attach | Object$ Self | PlayerChoices$ Player`,
		// Lynde's `DB$ Attach | Object$ ChosenCard | PlayerChoices$
		// Opponent`). The chosen seat is recorded for the re-entered
		// effect to consume and clear (fx42 scoping); the re-entry
		// re-checks it against the live pool the asking pass derived, so a
		// stale answer attaches nothing. AttachPlayerDone distinguishes
		// "answered" from an unanswered ask, so a Min-1 mandatory ask that
		// somehow produced an empty answer is a real no-attach rather than
		// a re-ask livelock.
		ctx.AttachPlayerDone = true
		for _, o := range chosen {
			if o.Kind == "player" {
				ctx.AttachPlayer = o.Player
				break
			}
		}
	case "planeswalk_optional":
		// An Optional$ True Planeswalk election is a KChoose yes/no. The
		// effect is a no-op without a planar deck, but its election is still
		// recorded by the effect and the normal Resolve walk continues into
		// any SubAbility$.
		ctx.PlaneswalkOpt = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.PlaneswalkOpt = "yes"
		}
	case "scry_optional":
		ctx.ScryOpt = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.ScryOpt = "yes"
		}
		ctx.Arrange = true
		ctx.LibraryTarget = rp.target - 1
	case "put_optional":
		// An Optional$ True PutCounter's yes/no election (Talus Paladin's
		// "you may put a +1/+1 counter on CARDNAME", Black Widow's "You
		// may put ... If you don't, ...") was answered. The answer is a
		// bare yes/no, recorded here as a marker the re-entered effect
		// consumes and clears (fx42 scoping): "yes" places the counters
		// through the ordinary path, "no" -- the decline -- places nothing
		// and the chained SubAbility$ still runs (the attach_optional
		// convention). A malformed or empty answer keeps the decline, the
		// conservative read of an ambiguous one.
		ctx.PutOpt = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.PutOpt = "yes"
		}
	case "endturn_optional":
		ctx.EndTurnOpt = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.EndTurnOpt = "yes"
		}
	case "venture_dungeon", "venture_room":
		// An api:Venture choice was answered (CR 701.49a/49b). The chosen
		// option's server-side Key names what the re-entered effVenture
		// acts on: the dungeon token script a first venture enters
		// ("venture_dungeon") or the room key the marker moves to
		// ("venture_room"). The ask's cursor (Decision.ResumeTarget ->
		// rp.target) rides back so a multi-player venture walk resumes
		// after the answered player; the answer fields' emptiness
		// distinguishes a fresh walk from a resumed one, so a malformed
		// empty answer keeps the walk at its start (the conservative read
		// the endturn_optional decline takes).
		if len(chosen) > 0 {
			if rp.kind == "venture_dungeon" {
				ctx.VentureEnter = chosen[0].Key
			} else {
				ctx.VentureRoom = chosen[0].Key
			}
		}
		ctx.VentureIdx = int32(rp.target)
	case "setstate_optional":
		// An Optional$ True SetState's yes/no election (Dowsing Dagger's
		// "you may transform this Equipment", High Marshal Arguel's "you
		// may transform it") was answered. The answer is a bare yes/no,
		// recorded here as a marker the re-entered effect consumes and
		// clears (fx42 scoping): "yes" runs the ordinary face change,
		// "no" -- the decline -- changes nothing and the chained
		// SubAbility$ still runs (the put_optional convention). A
		// malformed or empty answer keeps the decline, the conservative
		// read of an ambiguous one.
		ctx.SetStateOpt = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.SetStateOpt = "yes"
		}
	case "imprint":
		// An Imprint$ True public-zone choice. The effect consumes this
		// answer on re-entry and emits the persistent Imprint event.
		ctx.Imprint = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.Imprint = append(ctx.Imprint, o.Obj)
			}
		}
		ctx.ImprintDone = true
	case "untap":
		ctx.Untap = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.Untap = append(ctx.Untap, o.Obj)
			}
		}
		ctx.UntapDone = true
	case "dig":
		// A Dig look-and-take pick was answered: the library owner chose
		// which of the window's ChangeValid$-eligible cards to move to
		// DestinationZone$. The chosen options carry the object in Obj
		// (the same shape the "search" and "discard" arms read), so the
		// id list is read straight off them, in the player's answer order.
		// DigDone distinguishes "answered, possibly with no cards" (an
		// Optional$ decline) from the first pass. DigTarget keeps that
		// answer attached to the exact Defined$ target that asked, even
		// when earlier targets completed before suspension. effDig consumes
		// and clears all three at the top of its own walk, so a nested Dig
		// cannot inherit the outer answer.
		ctx.Dig = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.Dig = append(ctx.Dig, o.Obj)
			}
		}
		ctx.DigDone = true
		ctx.DigTarget = rp.target
	case "clash_placement":
		ctx.ClashContinuation = cloneClashResume(rp.clash)
		ctx.ClashTop = len(chosen) > 0 && chosen[0].Kind == "top"
	case "twopiles_split":
		// A TwoPiles pile split was answered (task twopiles1, Fact or
		// Fiction): the separator picked pile A out of the card set, in
		// answer order — the options carry the object in Obj (the same
		// shape the "dig" arm reads). An empty answer is the legal "piles
		// can be empty" answer, so TwoPilesDone is the answered marker,
		// not len(chosen). The full card set rides the decision's
		// ResumeRemembered (the ctx rebuild picks it up below, so the
		// re-entered effect re-derives pile B); pile A re-rides the pick
		// ask's ResumeChoices. effTwoPiles consumes and clears both fields
		// at the top of its own walk (fx42 scoping).
		ctx.TwoPiles = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.TwoPiles = append(ctx.TwoPiles, o.Obj)
			}
		}
		ctx.TwoPilesDone = true
	case "twopiles_pick":
		// A TwoPiles pile pick was answered: the chooser picked which pile
		// is the chosen one — option 0 Kind "pile-a" (pile A, the split
		// ask's answer, rides ResumeChoices back as Ctx.TwoPiles), option 1
		// Kind "pile-b". The ChosenPile$ body then runs on the chosen
		// pile and UnchosenPile$ on the other. A malformed or empty answer
		// keeps pile A (the deterministic clamp answer), the same
		// conservative read the malformed yes/no answers take.
		ctx.TwoPilesPick = "a"
		if len(chosen) > 0 && chosen[0].Kind == "pile-b" {
			ctx.TwoPilesPick = "b"
		}
		ctx.TwoPilesPickDone = true
		for _, t := range rp.choices {
			if !t.IsPlayer && t.Obj != 0 {
				ctx.TwoPiles = append(ctx.TwoPiles, t.Obj)
			}
		}
	case "clone":
		// A DB$ Clone Optional$ True may-copy election was answered
		// (ticket api-clone-trigger-copy; Sarkhan Soul Aflame). The answer
		// is a bare yes/no recorded as a marker the re-entered effect
		// consumes and clears (fx42 scoping): "yes" performs the copy,
		// "no" -- the decline -- leaves the permanent alone. A malformed
		// or empty answer keeps the decline, the same conservative read
		// the diguntil_move and attach_optional answers take.
		ctx.Clone = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.Clone = "yes"
		}
		ctx.CloneDone = true
	case "diguntil_move":
		// A DigUntil reveal-until's OptionalFoundMove$ yes/no election was
		// answered (task diguntil1; Songbirds' Blessing). The answer is a
		// bare yes/no recorded as a marker the re-entered effect consumes
		// and clears (fx42 scoping): "yes" moves the found card(s) to
		// FoundDestination$, "no" — the decline — to OptionalNoDestination$
		// or the revealed pile. A malformed or empty answer keeps the
		// decline, the conservative read of an ambiguous one (the same
		// attach_optional convention). DigUntilMoveDone also suppresses the
		// re-entry's reveal Note, which the first pass already recorded.
		ctx.DigUntilMove = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.DigUntilMove = "yes"
		}
		ctx.DigUntilMoveDone = true
	case "diguntil_aura":
		// CR 303.4f: an Aura entering without being cast chooses a
		// permanent to enchant. The option's object is revalidated by
		// effDigUntil against the current eligible bearer list.
		if len(chosen) > 0 {
			ctx.DigUntilAuraBearer = chosen[0].Obj
		}
		ctx.DigUntilAuraDone = true
	default:
		// Not one of this file's arms: the answer belongs to the rest of the
		// switch (resolution_answer_rest.go). The caller forwards to that
		// helper on handled=false — this arm must NOT forward itself, or a
		// rest-owned arm would run twice (measured: a doubled Shuffle on the
		// Ponder may-shuffle answer, a doubled extort pip, doubled Dismantle
		// counters).
		return false, false
	}
	// An owned arm reached its end without a bare `return`: in the original
	// single switch it fell through to resumeResolution's continuation, so
	// the caller continues here — but must not run the rest helper (this
	// kind is already bound; helper2's default would clobber Ctx.Modes).
	return true, false
}
