// resolution_answer_rest.go holds the second half of resumeResolution's answer-binding switch: the counter, hidden-pick, arrange, token/rest and optional arms, plus the "modes"/"" default arm.

// Code moved verbatim out of rules/resolution.go (moving code only; the
// suspension/resumption mechanism is documented at the top of resolution.go).
package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// resumeAnswerBindingRest binds the answer of the remaining resume kinds
// into the resumed Ctx. It never stops the resolution itself; the caller
// continues with its completion tail either way. It is invoked ONLY for a
// kind resumeAnswerBinding does not own (its handled=false) — an owned kind
// must never be re-bound here, or its arm would run twice and the default
// arm below would clobber Ctx.Modes to [""] for SAs without Choices$.
func (e *Engine) resumeAnswerBindingRest(rp *resumePoint, o *state.Object, ctx *effects.Ctx, chosen []decision.Option) {
	switch rp.kind {
	case "counter_dist":
		// A DividedAsYouChoose$ PutCounter distribution pick was answered
		// (Vastwood Hydra): the chooser picked which of the Choices$
		// eligible battlefield creatures receive the CounterNum$ total, in
		// answer order. CounterDistDone distinguishes "answered, possibly
		// with no creatures" (a MinChoiceAmount$ 0 decline) from the first
		// pass. effPutCounter consumes and clears both at the top of its
		// own walk (the fx42 scoping discipline), so a nested PutCounter
		// cannot inherit the outer answer.
		ctx.CounterDist = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.CounterDist = append(ctx.CounterDist, o.Obj)
			}
		}
		ctx.CounterDistDone = true
	case "counter_kind":
		ctx.CounterKind = ""
		if len(chosen) > 0 {
			ctx.CounterKind = chosen[0].Label
		}
		ctx.CounterKindDone = true
		// A bare Choices$ recipient pick precedes its comma-list kind
		// question. Carry that completed pick only through THIS re-entry;
		// effPutCounter consumes it at entry before a nested SA can see it.
		if len(rp.choices) > 0 {
			for _, t := range rp.choices {
				if !t.IsPlayer && t.Obj != 0 {
					ctx.CounterPick = append(ctx.CounterPick, t.Obj)
				}
			}
			ctx.CounterPickDone = true
		}
		if rp.sa != nil && strings.EqualFold(strings.TrimSpace(rp.sa.Params["CounterTypePerDefined"]), "True") {
			if e.counterTypeAsk == nil {
				e.counterTypeAsk = make(map[state.ObjID]*counterTypePending)
			}
			p := e.counterTypeAsk[rp.obj]
			if p == nil || p.sa != rp.sa {
				p = &counterTypePending{sa: rp.sa}
				e.counterTypeAsk[rp.obj] = p
			}
			for len(p.answers) <= rp.target {
				p.answers = append(p.answers, "")
			}
			p.answers[rp.target] = ctx.CounterKind
			ctx.CounterKindAnswerIndex, ctx.CounterKindAnswerSet = rp.target, true
		}
	case "counter_kinds":
		ctx.CounterKinds = nil
		for _, o := range chosen {
			ctx.CounterKinds = append(ctx.CounterKinds, o.Label)
		}
		ctx.CounterKindsDone = true
	case "counter_pick":
		// A bare-Choices$ PutCounter pick was answered (task vow1;
		// Promise of Loyalty's vow): the chooser picked the creature(s)
		// that take the full CounterNum$, in answer order.
		// CounterPickDone distinguishes "answered" from the first pass.
		// effPutCounter consumes and clears both at the top of its own
		// walk (the fx42 scoping discipline), so a nested PutCounter
		// cannot inherit the outer answer.
		ctx.CounterPick = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.CounterPick = append(ctx.CounterPick, o.Obj)
			}
		}
		ctx.CounterPickDone = true
	case "proliferate":
		// A Proliferate any-number recipient pick was answered (CR 701.27):
		// the resolving controller chose which permanents and/or players
		// take another counter of each kind already there. Unlike the
		// "counter_pick" arm, the option list is MIXED -- an object
		// recipient carries Obj, a player recipient carries Player with
		// Obj 0 -- so both halves are decoded here into the state.Target
		// shape Ctx.Proliferate carries. ProliferateDone distinguishes
		// "answered, possibly with nothing" (a Min-0 decline) from the
		// first pass, so a decline is not re-asked. effProliferate consumes
		// and clears both at the top of its own walk (the fx42 scoping
		// discipline), so a nested Proliferate cannot inherit the outer
		// answer.
		ctx.Proliferate = make([]state.Target, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.Proliferate = append(ctx.Proliferate, state.Target{Obj: o.Obj})
				continue
			}
			ctx.Proliferate = append(ctx.Proliferate,
				state.Target{Player: o.Player, IsPlayer: true})
		}
		ctx.ProliferateDone = true
	case "move_counter_kind":
		// A MoveCounter CounterType$ Any kind pick was answered: the
		// chooser picked which counter kind to move, out of the distinct
		// kinds the origin holds. The option's Label (the kind string) is
		// the answer; an empty or malformed answer keeps the deterministic
		// first-kind stand-in, the conservative read of an ambiguous one.
		// effMoveCounter consumes and clears both fields at the top of its
		// own walk (fx42 scoping), so a nested MoveCounter cannot inherit
		// the answer. The answer is ALSO recorded on the pending state:
		// this round's re-entry may suspend again on the amount ask, and
		// that later re-entry must still see the kind (seedMoveCounterAsk).
		ctx.MoveCounterKind = ""
		if len(chosen) > 0 {
			ctx.MoveCounterKind = chosen[0].Label
		}
		ctx.MoveCounterKindDone = true
		if rp.sa != nil && rp.sa.API == "MoveCounter" {
			p := e.moveCounterEntry(rp.obj)
			p.kind, p.kindSet = ctx.MoveCounterKind, true
		}
	case "time_travel":
		// Time Travel asks one optional add/remove/skip election per
		// affected object. ResumeTarget is the object's index into the
		// repetition's snapshot and ResumeRound the repetition itself,
		// so the re-entered effect continues at the exact object.
		ctx.TimeTravelChoice = "time_travel_skip"
		if len(chosen) > 0 {
			ctx.TimeTravelChoice = chosen[0].Kind
		}
		ctx.TimeTravelDone = true
		ctx.TimeTravelRound = rp.timeTravelRound
		ctx.TimeTravelIndex = rp.target
		ctx.TimeTravelObjects = append([]state.ObjID(nil), rp.timeTravelObjects...)
	case "move_counter":
		// A MoveCounter CounterNum$ Any amount pick was answered: how many
		// counters of the chosen kind to move. The option's Amount carries
		// the number (0 is a legitimate decline); a malformed answer moves
		// nothing. effMoveCounter consumes and clears both fields at the top
		// of its own walk (fx42 scoping). Recorded on the pending state for
		// the same later-resume reason as the kind above.
		ctx.MoveCounterN = 0
		if len(chosen) > 0 {
			ctx.MoveCounterN = int32(chosen[0].Amount)
		}
		ctx.MoveCounterNDone = true
		if rp.sa != nil && rp.sa.API == "MoveCounter" {
			p := e.moveCounterEntry(rp.obj)
			p.n, p.nSet = ctx.MoveCounterN, true
		}
	case "aor_elect":
		// An AddOrRemoveCounter add/remove election was answered
		// (counterchoice1). The election's kind is parsed out of the
		// answer's own option encoding ("aor_remove:<kind>"/
		// "aor_put:<kind>"), so the fresh Ctx carries everything the
		// re-entered walk needs without a second transport. A malformed
		// answer elects the deterministic first option (remove), the same
		// conservative read every KChoose arm takes. The answered kind is
		// ALSO recorded on the pending state (the moveCounterAsk
		// discipline): this round's re-entry may suspend again on the next
		// kind's election, and that later re-entry must not re-ask an
		// already-answered PUT kind (its counter count is still positive,
		// so the kinds enumeration still lists it).
		ctx.AorDone = true
		ctx.AorElect, ctx.AorKind = "remove", ""
		if len(chosen) > 0 {
			if chosen[0].Kind == "aor_skip" {
				// The combined absent-kind election has one skip option,
				// unlike the per-kind form's aor_skip:<kind>.
				ctx.AorElect = "skip"
			} else {
				if strings.HasPrefix(chosen[0].Kind, "aor_put:") {
					ctx.AorElect = "put"
				} else if strings.HasPrefix(chosen[0].Kind, "aor_skip:") {
					ctx.AorElect = "skip"
				}
				if _, k, found := strings.Cut(chosen[0].Kind, ":"); found {
					ctx.AorKind = k
				}
			}
		}
		if rp.sa != nil && rp.sa.API == "AddOrRemoveCounter" && ctx.AorKind != "" {
			e.aorEntry(rp.obj)[ctx.AorKind] = true
		}
	case "manifest_dread":
		if len(chosen) > 0 {
			ctx.ManifestDreadPick = chosen[0].Obj
		}
		ctx.ManifestDreadPlayer = rp.player
		ctx.ManifestDreadDone = true
	case "ring_bearer":
		// A Ring tempts you Ring-bearer choice (CR 701.54a: the tempted
		// player chooses a creature they control) was answered. The chosen
		// option carries the object in Obj (the same shape the "sacrifice"
		// and "blight" arms read). RingBearerDone distinguishes "answered"
		// from the first pass, so the re-entered effRingTemptsYou skips the
		// ask and emits the single RingTemptsYou event once -- a suspension
		// can never increment the count twice. effRingTemptsYou consumes and
		// clears both at the top of its own walk, so a nested Ring tempts
		// cannot inherit the outer answer.
		if len(chosen) > 0 {
			ctx.RingBearerPick = chosen[0].Obj
		}
		ctx.RingBearerDone = true
	case "blight":
		// A Blight's per-player KChoose (CR 701.60: the blighting player
		// chooses which of their own creatures takes the −1/−1 counters)
		// was answered. The chosen options carry the object in Obj (the
		// same shape the "sacrifice" and "counter_pick" arms read), so the
		// id list goes straight to Ctx.BlightPicks in the player's answer
		// order; BlightDone distinguishes "answered" from the first pass
		// and BlightTarget keeps the answer attached to the exact Defined$
		// target that asked. effBlight consumes and clears all three at the
		// top of its own walk, so a nested blight cannot inherit the outer
		// answer.
		ctx.BlightPicks = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.BlightPicks = append(ctx.BlightPicks, o.Obj)
			}
		}
		ctx.BlightDone = true
		ctx.BlightTarget = rp.target
	case "roll":
		// A RollDice choose-one-result answer (effects/dice.go's
		// ChosenSVar$/OtherSVar$ shape, the Endeavor cycle): the chosen
		// options' Index values name the dice (into the ask's own per-die
		// results, rp.rolls) the player picked. The re-entered effRollDice
		// publishes ChosenSVar$ = the sum of the picked dice's results,
		// OtherSVar$ = the sum of the rest, and consumes and clears all
		// three Ctx fields at its top (the fx42 scoping discipline).
		ctx.RollResults = rp.rolls
		pick := make([]int, 0, len(chosen))
		for _, o := range chosen {
			pick = append(pick, o.Index)
		}
		ctx.RollPick = pick
		ctx.RollDone = true
	case "hand_move":
		// A "choose N cards matching ChangeType$ from Origin$ Hand" pick was
		// answered (handmove1): the hand's owner chose which of the
		// ChangeType$-eligible cards to move to Destination$. The chosen
		// options carry the object in Obj (the same shape the "search",
		// "discard" and "dig" arms read), so the id list is read straight
		// off them, in the player's answer order. HandMoveDone distinguishes
		// "answered, possibly with no cards" from the first pass.
		// effChangeZoneHand consumes and clears both at the top of its own
		// walk (the fx42 scoping discipline), so a nested hand move cannot
		// inherit the outer answer.
		ctx.HandMove = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.HandMove = append(ctx.HandMove, o.Obj)
			}
		}
		ctx.HandMoveDone = true
		// The owner-selected shape (rv2b r2) chains one ask per hand owner:
		// the answer belongs to the exact owner that asked, and the
		// re-entered walk skips owners before the cursor and continues with
		// the owners after it (the same continuation DigTarget carries).
		ctx.HandMoveTarget = rp.target
	case "hand_move_confirm":
		// An Optional$ confirmation on a hidden-hand ChangeZone was
		// answered (Forge's confirmAction gate, which runs before the card
		// pick): option zero accepts the fetch, every other answer declines
		// it. effChangeZone...handMoveOwnersWalk consumes and clears these
		// at the top of its walk (fx42 scoping), so a nested hand move
		// poses its own confirmation.
		ctx.HandMoveConfirmDone = true
		ctx.HandMoveConfirmTarget = rp.target
		ctx.HandMoveConfirm = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.HandMoveConfirm = "yes"
		}
	case "hidden_pick":
		// A Hidden$ True public-origin pick was answered (hiddenpick1): the
		// chooser picked which of the ChangeType$-eligible cards in the
		// origin zone(s) move to Destination$. The chosen options carry the
		// object in Obj (the same shape the "search", "dig" and
		// "hand_move" arms read), so the id list is read straight off them,
		// in the player's answer order. HiddenPickDone distinguishes
		// "answered, possibly with no cards" from the first pass, and the
		// cursor keeps the answer attached to the exact fetch player that
		// asked -- the same continuation HandMoveTarget carries.
		// effHiddenPick consumes and clears all three at its top (the fx42
		// scoping discipline), so a nested pick cannot inherit the outer
		// answer.
		ctx.HiddenPick = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.HiddenPick = append(ctx.HiddenPick, o.Obj)
			}
		}
		ctx.HiddenPickDone = true
		ctx.HiddenPickTarget = rp.target
	case "hidden_pick_confirm":
		// An Optional$ confirmation on a Hidden$ True public-origin
		// ChangeZone pick was answered (the same confirmAction gate the
		// "hand_move_confirm" arm decodes): option zero accepts the fetch,
		// every other answer declines it. effHiddenPick consumes and clears
		// these at the top of its walk (fx42 scoping), so a nested pick
		// poses its own confirmation.
		ctx.HiddenPickConfirmDone = true
		ctx.HiddenPickConfirmTarget = rp.target
		ctx.HiddenPickConfirm = "no"
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.HiddenPickConfirm = "yes"
		}
	case "scry_replacement":
		ctx.ScryReplacement = true
		ctx.ScryCount, ctx.ScryProceed = rp.scryCount, rp.scryProceed
		ctx.LibraryTarget = rp.target
	case "arrange", "dig_arrange":
		// Ruling J0: rules' handleArrange already applied the answered
		// arrangement and emitted the LibraryOrder event before calling
		// resumeResolution, so the re-entered effect needs only to know
		// not to re-ask -- the arrangement lives on the LibraryOrder
		// event, not on Ctx, so this is a done-marker rather than an
		// answer the effect re-reads. A Dig additionally carries the
		// asking target's index (ArrangeTarget, only on its own
		// "dig_arrange" continuation), because its walk must keep the
		// deterministic processing of the LATER Defined$ targets on the
		// arrange re-entry instead of dropping them.
		ctx.Arrange = true
		if rp.kind == "dig_arrange" {
			ctx.ArrangeTarget = rp.target
		}
		// The shared per-player cursor every multi-library walk (search,
		// arrange, scry/surveil) reads to resume at the NEXT library; a
		// Dig's own arrange continuation additionally carries
		// ArrangeTarget so effDig's walk skips through it.
		ctx.LibraryTarget = rp.target
	case "arrange_mayshuffle":
		// A RearrangeTopOfLibrary carrying MayShuffle$ True (Ponder) asked
		// "you may shuffle?" on its arrange re-entry pass. The effect
		// re-enters here third: the arrange itself was applied two passes
		// ago, so ctx.Arrange stays the done-marker; ctx.MayShuffle carries
		// the answer as the ask-again marker the effect consumes and
		// clears. The shuffle itself is emitted HERE, before the
		// re-entered walk continues to the chained SubAbility$ (Ponder's
		// draw comes after the shuffle) -- the same Fisher-Yates over the
		// engine rng and the same Secret events.Shuffle the genesis deal
		// and every effects shuffle emit, with rp.player the library's
		// owner (the arranging player the ask was posed to).
		ctx.Arrange = true
		ctx.MayShuffle = "no"
		ctx.LibraryTarget = rp.target
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.MayShuffle = "yes"
			order := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, rp.player)...)
			e.rng.Shuffle(order)
			e.emit(events.Event{Kind: events.Shuffle, Player: rp.player, IDs: order, Secret: true})
		}
	case "discard_unless":
		// A Discard carrying UnlessType$ (Thirst for Knowledge) asked its
		// election: "discard one card of the type instead" vs "discard
		// NumCards$". The re-entered effDiscard reads the election off
		// ctx.UnlessElected (consumed and cleared there, fx42 scoping);
		// an empty or malformed answer elects the ordinary discard, the
		// conservative read of an ambiguous one.
		ctx.UnlessElected = "ordinary"
		if len(chosen) > 0 && chosen[0].Kind == "unless" {
			ctx.UnlessElected = "unless"
		}
	case "hideaway_pick":
		// Hideaway's first ask chooses exactly one of the looked-at cards.
		// The effect validates it remains in the library before moving it,
		// then asks the separate ordered-bottom question for the remainder.
		ctx.HideawayPicked = true
		if len(chosen) == 1 {
			ctx.Hideaway = chosen[0].Obj
		}
	case "hideaway_arrange":
		ctx.HideawayArranged = true
	case "soulbond":
		// Soulbond is a may choice: no option is a legitimate decline.
		ctx.SoulbondDone = true
		if len(chosen) == 1 {
			ctx.SoulbondPartner = chosen[0].Obj
		}
	case "myriad":
		// CR 702.109 makes a separate may choice for each eligible opponent.
		// ResumeTarget is that opponent's stable index in effMyriad's
		// deterministic list; only its explicit yes option creates the copy.
		ctx.MyriadDone = true
		ctx.MyriadTarget = rp.target
		ctx.MyriadCreate = len(chosen) == 1 && chosen[0].Kind == "yes"
	case "defined_library_optional":
		// An Optional$ object-valued Defined$ library fetch list (Kenessos's
		// DBBottom): option zero accepts the whole direct move; every other
		// answer declines it. The effect consumes this marker before any
		// nested optional fetch can see it.
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.DefinedLibraryMove = "yes"
		} else {
			ctx.DefinedLibraryMove = "no"
		}
	case "reveal_optional":
		// Task fb-3f1cc033 (Delver of Secrets): the peeking player's
		// RevealOptional$ yes/no was answered. Option 0 is "yes"; anything
		// else (option 1, an empty or malformed answer) is a decline — the
		// conservative read of an ambiguous answer is "no reveal". The
		// re-entered effReveal applies the answer: "yes" emits the Note
		// (which names the cards) and fires RememberRevealed$; "no" does
		// neither, so the chained ConditionDefined$ Remembered gate does
		// not fire either.
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.RevealOpt = "yes"
		} else {
			ctx.RevealOpt = "no"
		}
		// The per-target cursor: RevealOptTarget is the index of the
		// Defined$ target whose yes/no this answer was, so the re-entered
		// effReveal applies it to exactly that target and poses a fresh ask
		// for every later target (the LookAckTarget/RevealPickTarget
		// pattern). Without it, a multi-target optional reveal answered
		// for target 0 and then applied that same answer to every later
		// target — a yes silently revealed the rest, a no silently
		// declined them.
		ctx.RevealOptTarget = rp.target
	case "reveal_pick":
		// Task infernaltutor1: a mid-resolution hand-reveal pick (Infernal
		// Tutor's "Reveal a card from your hand", an AnyNumber$/Optional$
		// reveal) was answered. Every chosen option carries the revealed
		// card in Obj (the same shape the "discard" arm reads), so the id
		// list is read straight off them; the re-entered effReveal filters
		// it against the rebuilt pool and emits the reveal plus the
		// RememberRevealed$ capture for exactly those cards, which is what
		// the chained ChangeType$ Remembered.sameName sub then reads. The
		// slice is built non-nil (make, not nil) so a legitimate
		// "reveal none" answer is distinguishable from a first pass -- the
		// Ctx.Discard convention. effReveal consumes and clears it at the
		// top of its walk (fx42 scoping).
		ctx.RevealPick = make([]state.ObjID, 0, len(chosen))
		for _, o := range chosen {
			if o.Obj != 0 {
				ctx.RevealPick = append(ctx.RevealPick, o.Obj)
			}
		}
		// The per-target cursor: RevealPickTarget is the index of the
		// Defined$ target whose pick this answer was, so the re-entered
		// effReveal applies it to exactly that target's pool and poses a
		// fresh ask for every later target (the LookAckTarget pattern).
		ctx.RevealPickTarget = rp.target
	case "look_ack":
		// The bare private look's pacing ack (lookack, task
		// fb-20260917T232325Z-35cfca4b, Mishra's Bauble / Gitaxian Probe):
		// the looker clicked Continue on the "You look at ..." modal. There
		// is NO decline — the ask gates only the pacing, and CR 701.20e
		// requires the look itself to happen — so ANY answer (the single
		// Continue option; a malformed empty one included) acknowledges.
		// The answer is addressed by the per-target cursor (the DigTarget
		// pattern): LookAckTarget carries rp.target, the index of the
		// Defined$ target whose ack was answered, so the re-entered
		// effReveal emits exactly that target's note, skips the targets
		// already processed on the pass that suspended, and every later
		// bare look in the walk poses its own ack — without the cursor a
		// multi-target bare look (Case the Joint's Defined$ Player)
		// re-posed the last target's ack forever.
		ctx.LookAck = true
		ctx.LookAckTarget = rp.target
	case "draw_upto":
		// Upto$ Draw (Arcane Denial, Truce): the per-target count ask was
		// answered — the count is the number of chosen card options (the
		// options are the top cards of the target's own library; an empty
		// answer, legal at Min 0, is a draw-nothing decline, the point of
		// Upto$). The re-entered effDraw consumes the answer for exactly
		// the target the ask named (ResumeTarget, fx42 scoping), draws it,
		// then poses the next target's own ask.
		ctx.DrawUptoIdx = int32(rp.target)
		ctx.DrawUptoCount = int32(len(chosen))
		ctx.DrawUptoAnswered = true
	case "draw_optional":
		// OptionalDecider$ Draw (Mystic Remora, Rhystic Study): the
		// decider's yes/no was answered. Option 0 is "yes" (draw the
		// NumCards the unless arm did not price); anything else — option
		// 1, an empty or malformed answer — is a decline, the same
		// conservative read the reveal_optional arm takes. The re-entered
		// effDraw consumes the answer before its draw loop (fx42), so a
		// chained sub-Draw poses its own ask.
		if len(chosen) > 0 && chosen[0].Kind == "yes" {
			ctx.DrawOpt = "yes"
		} else {
			ctx.DrawOpt = "no"
		}
	case "play":
		// A Play effect (Conduit of Worlds, Spinerock Knoll) was answered:
		// each chosen option's Obj is a card to play from its current zone,
		// in answer order. An empty answer is a DECLINE of an Optional$
		// Play (effPlay now offers Min 0) -- the answer is consumed with
		// nothing begun. Only the effect's own WithoutManaCost$ grants a
		// free cast: Conduit has no such parameter, while Spinerock Knoll
		// does. A PlayCost$ alternative (task playcost1: Amped Raptor's
		// PayEnergy<ConvertedManaCost>, Anrakyr's PayLife<ConvertedManaCost>,
		// Blue Mage's Cane's fixed {3}, Cruelclaw's Discard<1/Card>) is
		// priced per chosen card -- ConvertedManaCost substitutes the
		// card's own mana value -- inside beginPlay, which hard-declines an
		// unpriceable or unpayable alternative with a loud Note instead of
		// charging full mana. An Amount$ All / N answer may name several
		// cards; each is begun in turn, and the loop stops at the first cast
		// that cannot commit synchronously (an ask inside the cast
		// transaction -- an ETB choice, a target, a mana window -- parks the
		// resolution on that cast's question, and the not-yet-begun cards
		// are dropped with a Note rather than wedging; the corpus Amount$ All
		// shapes are without-mana-cost creature/spell plays, which commit
		// synchronously). ctx.Play/PlayDone are set so the re-entered
		// effPlay sees the answer as consumed either way.
		free := strings.EqualFold(rp.sa.Params["WithoutManaCost"], "True")
		playCost := strings.TrimSpace(rp.sa.Params["PlayCost"])
		// ReplaceGraveyard$ Exile (task replplay1): the Play SA's own
		// rider — "if that spell would be put into your graveyard this
		// turn, exile it instead" — stamps the played spell's pay-time
		// CastInfo with state.FlagReplaceGraveyard so spellRestZone (and
		// spellFizzleZone for a fizzled/countered play) exiles it. The
		// conditional sibling ReplaceGraveyardValid$ (2 corpus files:
		// Bilbo, Thief in the Night; Scholar of the Lost Trove) restricts
		// the exile to named types and is unread — fail closed, keep the
		// graveyard resting place for those.
		replaceGraveyard := strings.EqualFold(strings.TrimSpace(rp.sa.Params["ReplaceGraveyard"]), "Exile") &&
			strings.TrimSpace(rp.sa.Params["ReplaceGraveyardValid"]) == ""
		// CopyCard$ True casts an event-minted copy of the selected card,
		// leaving the original in its source zone. False or absent keeps
		// the ordinary Play path.
		copyCard := strings.EqualFold(strings.TrimSpace(rp.sa.Params["CopyCard"]), "True")
		// ImprintPlayed$ True (task imprintplayed: Rashmi and Ragavan,
		// Kefka, Beseech the Mirror, Soundwave, Smuggler's Buggy — 5 corpus
		// files): every card the Play actually BEGINS to play is recorded
		// as imprinted on the resolution's source (events.Imprint, the
		// same association Chrome Mox's Imprint$ writes), so the chained
		// ConditionDefined$ Imprinted gate (DBEffect's "did you cast it
		// this way?" arm) reads a real answer. "Actually begins" is read
		// from the card's zone: a begun cast pushes the card onto the
		// stack (CR 601.2a) or moves it onward, while a declined Play — an
		// unpayable alternative, a stale answer, a reversed cast — leaves
		// it in its zone, and an aborted cast reverses it back to exactly
		// the zone it started in. The emission sits before the suspension
		// break so a cast suspended mid-transaction (a target ask inside
		// the free cast) is still recorded as played.
		imprintPlayed := strings.EqualFold(rp.sa.Params["ImprintPlayed"], "True")
		// The play's own CONTROLLER rides the answer's options (effPlay set
		// each option's Player to the Controller$-resolved seat): a
		// Controller$ Play (Word of Command's TargetedPlayer, Wild
		// Evocation's TriggeredPlayer, Spell Queller's RememberedOwner) is
		// begun BY that seat, not by the resolving ability's controller.
		// An option carrying no seat (a hand-built context, or every
		// historical Controller$-absent game whose options named the
		// resolving controller anyway) keeps ctx.Controller, the pre-
		// Controller$ read.
		player := ctx.Controller
		if len(chosen) > 0 && int(chosen[0].Player) < len(e.G.Players) {
			player = chosen[0].Player
		}
		// ShowCards$ (Sunbird's Invocation -- the corpus's one carrier):
		// the play's public reveal rider. The population it names is a
		// card filter over the walk's remembered set ("Card.IsRemembered"
		// = the window the PeekAndReveal revealed and remembered); the
		// reveal is ONE public Note (the ids payload view.Describe
		// renders) emitted BEFORE the first cast begins, while the cards
		// still sit in their hidden zones. A decline plays nothing and
		// reveals nothing. The R-9 no-host path (effPlay's deterministic
		// first candidate) never reaches this arm and stays untouched --
		// the same boundary ForgetPlayed$ keeps.
		var toPlay []state.ObjID
		for _, ch := range chosen {
			if ch.Obj != 0 {
				toPlay = append(toPlay, ch.Obj)
			}
		}
		if show := strings.TrimSpace(rp.sa.Params["ShowCards"]); show != "" && len(toPlay) > 0 {
			sc := ctx.SpecContext(player)
			var ids []state.ObjID
			seenShow := map[state.ObjID]bool{}
			for _, t := range ctx.Remembered {
				if t.IsPlayer || t.Obj == 0 || seenShow[t.Obj] {
					continue
				}
				if o := e.G.Obj(t.Obj); o != nil && e.matchesSpec(show, t.Obj, sc) {
					seenShow[t.Obj] = true
					ids = append(ids, t.Obj)
				}
			}
			if len(ids) > 0 {
				e.emit(events.Event{Kind: events.Note, Player: player, IDs: ids})
			}
		}
		ctx.PlayDone = true
		if len(toPlay) > 0 {
			ctx.Play = toPlay[0]
		}
		q := &queuedPlays{player: player, ids: toPlay, free: free, playCost: playCost,
			replaceGraveyard: replaceGraveyard, copyCard: copyCard}
		if imprintPlayed {
			q.imprintOn = ctx.Source
		}
		e.runPlays(q)
	case "play_resume":
		// The continuation an Amount$ Play parked while its chosen cards were
		// cast one at a time (rules/play_queue.go): every cast is complete,
		// so the re-entered effPlay sees the answer as consumed and the walk
		// continues at its SubAbility$.
		ctx.PlayDone = true
		ctx.Play = rp.playFirst
	case "extort":
		// Extort's optional {W/B} payment was answered. Option 0 is "pay";
		// anything else is a decline. The hybrid pip is charged from the
		// caster's pool as one W or B when available; a pool lacking both
		// colours deterministically declines (the drain never runs without
		// the mana being genuinely paid). The re-entered effExtort reads
		// Ctx.Extort and runs the drain only on "pay".
		if len(chosen) > 0 && chosen[0].Index == 0 {
			if e.payExtortPip(chosen[0].Player) {
				ctx.Extort = "pay"
			} else {
				ctx.Extort = "decline"
			}
		} else {
			ctx.Extort = "decline"
		}
	case "effect_paid":
		// The trigger's Cost$ was paid by triggeredCostAnswer; run the
		// parked effect without opening the payment window a second time.
	case "charm_rest":
		// A cross-mode TargetUnique Charm's mode loop suspended mid-mode;
		// the remaining chosen modes re-enter effCharm exactly as the
		// placement answer did: Ctx.Modes names them (overriding the
		// o.Ability branch's full ChosenModes seed), effCharm's split
		// assigns each target-bearing one the last targets of the original
		// positional assignment, and nothing re-asks.
		// Non-nil even when the rest is empty (the last chosen mode
		// suspended): effCharm's re-entry branch is keyed on Ctx.Modes !=
		// nil, so an empty but present list runs no mode and resumes the
		// Charm's own Sub rather than re-posing the mode ask.
		ctx.Modes = append([]string{}, rp.charmRest...)
		// CanRepeatModes$ (CR 601.2b): the rest is a suffix of the object's
		// full ChosenModes (the walk only ever truncates a suffix), so the
		// names the earlier passes consumed are derivable exactly. Seed
		// them so effCharm's first-occurrence covered-marking knows which
		// modes already ran -- a repeated target-bearing mode's later
		// instance keeps its own ValidTgts$ pre-ask instead of inheriting
		// the shared list a second time.
		if o := e.G.Obj(rp.obj); o != nil && len(o.ChosenModes) > len(rp.charmRest) {
			ctx.ModesSeen = append(ctx.ModesSeen,
				o.ChosenModes[:len(o.ChosenModes)-len(rp.charmRest)]...)
		}
	case "villainous_rest":
		// A VillainousChoice's chosen body suspended on its own nested ask
		// (DBSac's sacrifice picker) and that ask's chain has completed.
		// Re-enter the primitive with the victim cursor restored so the
		// remaining Defined$ victims are still asked. The modes seed the
		// ability branch applied must be cleared: this frame carries no
		// answered mode (the first victim's was consumed long ago) and a
		// stale ChosenModes must not make effVillainousChoice re-run a
		// previously chosen body.
		ctx.Modes = nil
		ctx.VillainousVictims = append([]state.Target(nil), rp.villainousVictims...)
		ctx.VillainousIndex = rp.villainousIndex
	case "generic_players":
		// A multi-player api:GenericChoice's per-chooser KModes answer: the
		// chosen SVar name is scoped to the chooser whose body has not run
		// yet. Ctx.Modes names it (overriding any stale ChosenModes seed)
		// and the cursor advances PAST that chooser, so effCharm runs the
		// body once and then asks the next Defined$ chooser. Ctx.Remembered
		// is the chooser (resumeResolution bound rp.remembered), which is
		// what the body's Defined$ Remembered reads.
		ctx.Modes = []string{rp.genericChoice}
		ctx.GenericChoosers = append([]state.Target(nil), rp.genericChoosers...)
		ctx.GenericChooserIndex = rp.genericChooserIndex + 1
	case "generic_players_rest":
		// Re-enter after the chosen body's nested ask, restoring both the
		// remaining chooser cursor and the enclosing remembered set.
		ctx.Modes = nil
		ctx.GenericChoosers = append([]state.Target(nil), rp.genericChoosers...)
		ctx.GenericChooserIndex = rp.genericChooserIndex
		ctx.Remembered = append([]state.Target(nil), rp.genericRemembered...)
	case "token_rest":
		// A DB$ Token's mint parked behind a replacement-order ask and
		// the answer has minted it: re-enter the Token with its frozen
		// job, the parked mint's objects (the collector the answer
		// minted into) and the cursor of the mints still owed.
		ctx.Modes = nil
		rest := rp.tokenRest.Clone()
		rest.Parked = e.takeMintSink(rest.SinkID)
		ctx.TokenRest = &rest
	case "flip_rest":
		// A DB$ FlipCoin loop's per-flip sub-ability suspended on its own
		// mid-resolution ask (Mirror March's copy choice, say) and that
		// ask's chain has completed. Re-enter effFlipCoin with the flip
		// cursor restored so the remaining flips run. Ctx.Modes is
		// cleared for the same reason as villainous_rest: this frame
		// carries no answered mode.
		ctx.Modes = nil
		cursor := rp.flipCursor
		ctx.FlipRest = &cursor
	case "optional":
		// CR 603.5: the decider answered yes to applying this optional
		// triggered ability's effect. The answer is a yes/no, not a mode
		// choice, so nothing is written to Ctx.Modes -- it was already
		// seeded from the stack object's ChosenModes by the o.Ability
		// branch above, exactly as resolveTop's own first pass would
		// have. The re-entry below just runs the ability's effect.
		//
		// ResolvedLimit$: an ACCEPTED optional trigger is one the effect
		// runs for, so it consumes the per-turn resolution count here. A
		// decline (handleTriggerOptional's finishResumption branch) never
		// reaches resumeResolution and so never increments, exactly as the
		// oracle's "you may ... do this only once" requires. The eligibility
		// check is on the RESOLVED line's own param (the trigger
		// findTriggerForAbility matches for the resumed ability), never on
		// the source's other lines -- accepting a sibling optional trigger
		// (Tidus, Yuna's Guardian's non-RL BeginCombat line) must not spend
		// the ResolvedLimit$ line's count.
		if o != nil {
			if t, ok := e.triggerForAbilityObject(rp.obj, o); ok {
				if _, limited := resolvedLimitValue(t); limited {
					e.noteTriggerResolved(o.Source)
				}
			}
		}
	default: // "modes", and "" (a pure outer continuation with no answer)
		// A KWChoice$ pump's modes are keyword labels, not SVar names:
		// when the asking SA carries no Choices$ but a KWChoice$, the
		// chosen indexes map against THAT list (effects' effPump re-entry
		// consumes them as the granted keywords).
		eligible := []string(nil)
		if strings.TrimSpace(rp.sa.Params["Choices"]) == "" {
			if kw := strings.TrimSpace(rp.sa.Params["KWChoice"]); kw != "" {
				eligible = strings.Split(kw, ",")
				for i := range eligible {
					eligible[i] = strings.TrimSpace(eligible[i])
				}
			}
		}
		ctx.Modes = modeChoiceNames(rp.sa, chosen, eligible)
	}
}
