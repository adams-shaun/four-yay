package oraclegen

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// answerRouting owns the decisions whose XMage ask is not the one their
// engine kind suggests. Each is a gorge-side step XMage either never poses
// (a chooseUse OrCost with one payable cost, an "up to one" graveyard pick's
// confirmation) or poses as a different dialog (a clash's put-back chooseUse,
// the choice-queue "choose an opponent" ask), and the opponent selection
// gorge made implicitly with one opponent. It is computed once per decision
// list so a decision's role is derived from its neighbours, not a card name.
type answerRouting struct {
	ds       []rules.OracleDecision
	soleCost map[int]bool // the cost pick that follows a one-payable OrCost ask
	opponent map[int]int  // decision index -> opponent seat the controller selects first
	// splitTarget maps a "damage_split" decision index to the divided-target
	// decision it divides; targetSplit is the inverse. The two are paired by
	// recipient set, never by step and seat alone.
	splitTarget map[int]int
	targetSplit map[int]int
	// manaBool marks a CR 603.5 optional-trigger boolean whose ability
	// resolves straight into the mana colour ask that follows it in the same
	// step: XMage's counterpart trigger is mandatory (its mana effect poses
	// only the colour dialog), so the boolean has no ask to answer and any
	// scripted one would be popped -- and thrown on -- by the colour dialog
	// (measured driver error: "Choice key [Yes] not found in [White, Blue,
	// Black, Red, Green]").
	manaBool map[int]bool
	// playBool marks a trigger-level optional boolean that gates a Play whose
	// own "may" ask follows it at once. Forge models the ONE "you may cast"
	// of the trigger's text twice (OptionalDecider$ on the trigger and
	// Optional$ on the Play), so gorge poses two asks; XMage's trigger is not
	// optional and MayCastTargetCardEffect poses a single chooseUse ("Cast
	// X?"). The play decision's answer is that one boolean, so the trigger's
	// own scripts nothing -- else its "yes" is consumed by the "Cast X?" ask
	// and the cast's target ask meets a queued answer meant for a later
	// object (Seifer Almasy: "Targets list was setup by addTarget with
	// [Shock], but not used").
	playBool map[int]bool
	// manaHoist maps a same-source trigger-order span to the colour answer
	// that replaces it and LEADS its step's stream, manaHoisted marks the
	// colour decision whose answer was thus moved. XMage's mana trigger for
	// the span's card fires while the span's inert rule texts sit at the
	// queue head, and its colour dialog pops the first one and throws
	// (measured driver error: "Choice key [Whenever this creature
	// transforms into ...] not found in [White, Blue, Black, Red, Green]").
	manaHoist   map[int]XAnswer
	manaHoisted map[int]bool
}

func newAnswerRouting(ds []rules.OracleDecision) *answerRouting {
	r := &answerRouting{ds: ds, soleCost: map[int]bool{}, opponent: map[int]int{},
		splitTarget: map[int]int{}, targetSplit: map[int]int{},
		manaBool: map[int]bool{}, playBool: map[int]bool{},
		manaHoist: map[int]XAnswer{}, manaHoisted: map[int]bool{}}
	for i := range ds {
		if soleAltCost(ds[i]) && i+1 < len(ds) && sameAsker(ds[i], ds[i+1]) && len(ds[i+1].ObjectPicks) == 1 {
			r.soleCost[i+1] = true
		}
	}
	for i := 0; i+1 < len(ds); i++ {
		a, b := ds[i], ds[i+1]
		if a.Step < 0 || !sameAsker(a, b) {
			continue
		}
		if a.Kind == "yesno" && a.GorgeKind == "trigger_optional" && a.Resume == "optional" &&
			pickKind(a, 0) == "yes" && manaHoistAnswer(b) != "" {
			r.manaBool[i] = true
		}
		if a.Kind == "yesno" && a.GorgeKind == "trigger_optional" && a.Resume == "optional" &&
			pickKind(a, 0) == "yes" && len(a.Picks) == 1 && b.Kind == "mode" && b.Resume == "play" &&
			maysIn(a) == 1 {
			r.playBool[i] = true
		}
	}
	for i := range ds {
		d := ds[i]
		if d.Step < 0 || d.Kind != "order" || d.GorgeKind != "trigger_order" || triggerOrderNamesDistinct(d) {
			continue
		}
		for j := i + 1; j < len(ds); j++ {
			n := ds[j]
			if n.Step != d.Step || n.Seat != d.Seat {
				continue
			}
			if n.Kind == "order" && n.GorgeKind == "trigger_order" {
				break
			}
			if ans := manaHoistAnswer(n); ans != "" {
				r.manaHoist[i] = XAnswer{d.Seat, "choice", ans}
				r.manaHoisted[j] = true
				break
			}
		}
	}
	// A clash is the controller's decision then its opponent's; XMage's
	// ClashEffect asks the controller for the opponent (TargetOpponent) first.
	for i := 0; i+1 < len(ds); i++ {
		a, b := ds[i], ds[i+1]
		if a.Resume == "clash_placement" && b.Resume == "clash_placement" && a.Step == b.Step && a.Seat != b.Seat {
			r.opponent[i] = b.Seat
			i++
		}
	}
	// Memories Returning style: the controller picks from revealed cards, then
	// "chooses an opponent" who puts one on the bottom. Gorge picks the lone
	// opponent without a decision; the first dig pick made by another seat
	// marks where XMage's TargetOpponent ask sits.
	first := map[int]int{}
	for i, d := range ds {
		if d.Resume != "dig" {
			continue
		}
		s, ok := first[d.Step]
		if !ok {
			first[d.Step] = d.Seat
		} else if s >= 0 && d.Seat != s && len(d.Picks) == 1 && digBottomName(d.Picks[0]) != "" {
			r.opponent[i] = d.Seat
			first[d.Step] = -1
		}
	}
	// A divided-damage target ask announces its recipients; the shares arrive
	// later as the engine's own "damage_split" KChoose, whose options are
	// exactly those recipients (effects/damage_deal.go builds them from the
	// chosen Defined$ list). Pair the two by that recipient set so the split
	// answers land at the target ask's own queue position -- searching by step
	// and seat alone lets an unrelated same-step split (a second trigger
	// resolving in the same step) steal the answers, and a split's allocation
	// must precede the chain skips the target ask carries. Each split is
	// claimed at most once, and the nearest preceding unclaimed target wins.
	claimed := map[int]bool{}
	for i := range ds {
		d := ds[i]
		if d.Kind != "target" || d.Divided <= 0 || len(d.PickRefs) == 0 {
			continue
		}
		for j := i + 1; j < len(ds); j++ {
			n := ds[j]
			if n.Resume != "damage_split" || claimed[j] || !sameAsker(d, n) {
				continue
			}
			if !sameRecipients(d.PickRefs, n.PickRefs) {
				continue
			}
			r.splitTarget[j] = i
			r.targetSplit[i] = j
			claimed[j] = true
			break
		}
	}
	return r
}

// sameRecipients reports whether two pick-ref lists name the same set of
// targets. A damage_split repeats one ref per point of damage assigned, so
// duplicates are expected and only the distinct refs matter.
func sameRecipients(a, b []string) bool {
	distinct := func(refs []string) map[string]bool {
		m := map[string]bool{}
		for _, r := range refs {
			m[r] = true
		}
		return m
	}
	x, y := distinct(a), distinct(b)
	if len(x) != len(y) {
		return false
	}
	for k := range x {
		if !y[k] {
			return false
		}
	}
	return true
}

// ownsSplit reports a "damage_split" decision whose shares the paired divided
// target ask already emitted at its own queue position. Emitting them again
// would duplicate every "^X=" answer.
func (r *answerRouting) ownsSplit(i int) bool {
	_, ok := r.splitTarget[i]
	return ok
}

// soleAltCost reports an AlternateAdditionalCost either-or ask that only one
// option of could be paid: XMage's OrCost poses no chooseUse for it.
func soleAltCost(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && len(d.Picks) == 1 && pickKind(d, 0) == "altaddcost" && d.AltPayable == 1
}

// manaHoistAnswer is the choice-queue colour answer a mana colour pick
// scripts, or "" when the decision is not exactly one colour option (the
// generic choose_n path maps an "Add W" label through manaColourLabel).
func manaHoistAnswer(d rules.OracleDecision) string {
	if d.Kind != "choose_n" || d.Resume != "mana_color" || len(d.Picks) != 1 {
		return ""
	}
	label := d.Picks[0]
	if label == "" && len(d.PickRefs) > 0 {
		label = oraclediffRefName(d.PickRefs[0])
	}
	if c, ok := manaColourLabel(label); ok {
		return c
	}
	return ""
}

// manaHoistSpan is the colour answer that LEADS a same-source trigger-order
// span's step, replacing the span's inert rule texts entirely.
func (r *answerRouting) manaHoistSpan(i int) (XAnswer, bool) {
	a, ok := r.manaHoist[i]
	return a, ok
}

// manaHoistedPick reports a mana colour decision whose answer was hoisted to
// the span's queue position; its own decision scripts nothing.
func (r *answerRouting) manaHoistedPick(i int) bool { return r.manaHoisted[i] }

func sameAsker(a, b rules.OracleDecision) bool { return a.Step == b.Step && a.Seat == b.Seat }

var mayWord = regexp.MustCompile(`(?i)\bmay\b`)

// maysIn counts the "may" words of an optional trigger decision's label,
// "Yes -- <Source>: <the trigger's rules text>", past the source's name (a
// card name may itself contain the word). XMage poses one chooseUse per
// "may" of the Oracle text, so a trigger text with exactly one has one real
// ask however many Forge models it with; two or more ("you may put it on
// the bottom ... You may cast that card", Neera, Wild Mage) are distinct
// asks and stay as recorded.
func maysIn(d rules.OracleDecision) int {
	text := d.Picks[0]
	if len(d.PickRefs) == 1 {
		if _, rest, ok := strings.Cut(text, oraclediffRefName(d.PickRefs[0])+": "); ok {
			text = rest
		}
	}
	return len(mayWord.FindAllStringIndex(text, -1))
}

// digBottomName is the card name of a Dig "Put X on bottom" label, else "".
func digBottomName(label string) string {
	if strings.HasPrefix(label, "Put ") && strings.HasSuffix(label, " on bottom") {
		return strings.TrimSuffix(strings.TrimPrefix(label, "Put "), " on bottom")
	}
	return ""
}

// digExileComplement is the card name a Dig take must script for the
// second-destination-is-exile family (Ashiok, Wicked Manipulator; Karn, Scion
// of Urza), else "". XMage's Dig dialog for that shape selects the card to
// EXILE, the opposite of gorge's pick (the card to hand), so the scripted
// answer must be the OTHER offered option. The family is the Dig take whose
// pick is a plain card name over exactly two offered objects with a one-card
// take (DigNum$ 2 / ChangeNum$ 1); the complement is computed from the
// decision alone, since the offered option list holds both names and gorge's
// pick is the one to hand. The decision carries no destination, so the family
// is recognised by this shape -- the only shape a Standard-set Dig take
// produces for it (measured over the 20 level-B sets, Ashiok is the sole
// real-name two-option one-card take). The bottom-label form (digBottomName)
// is a different XMage dialog and measures AGREE, so it is excluded here and
// owned by its own case.
func digExileComplement(d rules.OracleDecision) string {
	if d.Resume != "dig" || d.Options != 2 || d.Min != 1 || d.Max != 1 ||
		len(d.Picks) != 1 || len(d.OptionRefs) != 2 || digBottomName(d.Picks[0]) != "" {
		return ""
	}
	pick := d.Picks[0]
	complement := ""
	matched := false
	for _, ref := range d.OptionRefs {
		name := oraclediffRefName(ref)
		if name == pick {
			matched = true
			continue
		}
		if complement != "" && complement != name {
			// Two distinct non-pick names: the offer is ambiguous, so the
			// complement cannot be computed. Leave the decision to the generic
			// choice path rather than guess.
			return ""
		}
		complement = name
	}
	if !matched || complement == "" {
		return ""
	}
	return complement
}

// declinedChoice is a generic effect ask ("choose up to N") gorge answered
// with nothing.
func declinedChoice(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && d.Resume == "choice" && d.Min == 0 && d.Max > 0 && len(d.Picks) == 0
}

// waterbendHelperDeclined is a declined waterbend tap-helpers ask
// (rules/pay/castasks.go's "Tap <name> to waterbend for 1" option, option
// kind waterbend_generic). Only a payment ask carries the phrase: option 0's
// label names the helper and the contribution ({1} of the waterbend amount).
func waterbendHelperDeclined(d rules.OracleDecision) bool {
	return d.Kind == "choose_n" && len(d.Picks) == 0 &&
		strings.HasSuffix(d.First, " to waterbend for 1")
}

// route returns the answers for decision i when this file owns it.
func (r *answerRouting) route(i int) (as []XAnswer, owned bool) {
	d := r.ds[i]
	if seat, ok := r.opponent[i]; ok {
		// The controller selects the opponent on the choice queue; the seat
		// is the asker of the clash's first decision, or the dig's first pick.
		asker := d.Seat
		if d.Resume == "dig" {
			asker = r.firstDigSeat(i)
		}
		as = append(as, XAnswer{asker, "choice", seatRef(seat)})
	}
	switch {
	case r.playBool[i]:
		// The redundant trigger-level half of a single "may cast": the Play
		// decision that follows carries the one boolean XMage asks.
		return nil, true
	case r.manaBool[i]:
		// The mandatory-in-XMage mana trigger's own resolution: XMage poses
		// only the colour dialog that follows, so the boolean scripts
		// nothing and the colour pick leads the queue.
		return nil, true
	case soleAltCost(d):
		return as, true
	case waterbendHelperDeclined(d):
		// The waterbend tap-helpers ask: XMage pays the ability's waterbend
		// generic from the prefilled mana pool and never poses the ask, so
		// neither of the declined pair's halves has an ask to answer (Giant
		// Koi, Aang, Swift Savior, ...). The ability's own target or effect
		// asks are re-scripted by the serving template when it knows they
		// follow (activate_waterbend.go).
		return nil, true
	case r.soleCost[i]:
		// XMage still asks the cost's own pick (TargetCardInHand "discard
		// cost", TargetSacrifice) through makeChoose, by name.
		return append(as, XAnswer{d.Seat, "choice", oraclediffRefName(d.ObjectPicks[0])}), true
	case d.Resume == "hidden_pick_confirm" && len(d.Picks) == 1 && pickKind(d, 0) == "yes" && r.upToPickFollows(i):
		// Forge's Optional$ confirmation before an "up to one" pick: XMage's
		// ReturnCardChosenFromGraveyardEffect has the up-to-one target only.
		return nil, true
	case d.Resume == "clash_placement" && len(d.PickKinds) == 1:
		// ClashTargetEffect's chooseUse "put back on top?": true is top.
		answer := "no"
		if d.PickKinds[0] == "top" {
			answer = "yes"
		}
		return append(as, XAnswer{d.Seat, "choice", answer}), true
	case d.Resume == "dig" && len(d.Picks) == 1 && digBottomName(d.Picks[0]) != "":
		// XMage's bottom pick is a card selection by name, not gorge's label.
		return append(as, XAnswer{d.Seat, "choice", digBottomName(d.Picks[0])}), true
	case digExileComplement(d) != "":
		// XMage's Dig dialog for the exile-second-destination family selects
		// the card to EXILE, the opposite of gorge's pick (the card to hand),
		// so the scripted answer is the other offered option.
		return append(as, XAnswer{d.Seat, "choice", digExileComplement(d)}), true
	case d.Resume == "taporuntap" && len(d.Picks) == 1:
		// TapOrUntap's election is XMage's chooseUse, not a labelled choice:
		// the pick's option index maps to the boolean (option 0, the
		// state-changing choice, is yes). The label would be left unused and
		// the AI would answer instead.
		return append(as, XAnswer{d.Seat, "choice", tapOrUntapChoice(d)}), true
	case perPlayerTargetAsk(d) && len(d.Picks) == 0:
		// A declined mid-resolution per-player target ask: XMage asks one
		// real target per opponent, so close each with a target skip on the
		// TARGET queue. The generic declinedChoice arm would emit a
		// [choice_skip] on the choice queue instead.
		return perControllerTargetAnswers(d), true
	case d.Resume == "hidden_pick" && len(d.Picks) == 0 && d.Min == 0 && d.Max > 0:
		// A declined "you may put it into your hand" hidden pick (Sparring
		// Dummy's milled land): XMage poses the optional ask on its target
		// queue only, so the skip token declines it there. The declined
		// pair's "no" boolean has no chooseUse to answer (measured driver
		// error: "Found wrong choice command").
		return append(as, XAnswer{d.Seat, "target", "[target_skip]"}), true
	case d.Kind == "mode" && d.Resume == "play" && len(d.Picks) == 0 && r.lookedFirst(i):
		// A declined optional Play after a hidden-zone look (Cosmic Cube's
		// "you may play one of these"): XMage poses the "cast from among
		// them" ask on the target queue only, so the skip token declines
		// it. The plain Play carriers (Discover, Cascade) have no look and
		// stay with the measured chooseUse "no" below.
		return append(as, XAnswer{d.Seat, "target", "[target_skip]"}), true
	case declinedChoice(d) && !r.lookedFirst(i):
		// One makeChoose "up to" ask however many type slots gorge posed; its
		// own skip token ends it. (A boolean "no" is not an answer to it.)
		if i > 0 && declinedChoice(r.ds[i-1]) && sameAsker(r.ds[i-1], d) {
			return nil, true
		}
		return []XAnswer{{d.Seat, "choice", "[choice_skip]"}}, true
	}
	return nil, false
}

// lookedFirst reports a look_ack by the same seat earlier in decision i's
// step: a look-at-the-top "up to" pick (Zimone's Experiment, Break Out) whose
// declined answer is the measured "no" + target_skip pair, not this routing.
func (r *answerRouting) lookedFirst(i int) bool {
	for _, d := range r.ds[:i] {
		if d.Resume == "look_ack" && sameAsker(d, r.ds[i]) {
			return true
		}
	}
	return false
}

// firstDigSeat is the seat of the first dig decision of decision i's step.
func (r *answerRouting) firstDigSeat(i int) int {
	for _, d := range r.ds {
		if d.Step == r.ds[i].Step && d.Resume == "dig" {
			return d.Seat
		}
	}
	return r.ds[i].Seat
}

// upToPickFollows reports an optional (Min 0) hidden pick by the same seat
// right after decision i.
func (r *answerRouting) upToPickFollows(i int) bool {
	if i+1 >= len(r.ds) {
		return false
	}
	n := r.ds[i+1]
	return sameAsker(r.ds[i], n) && n.Resume == "hidden_pick" && n.Min == 0
}

func seatRef(seat int) string { return "p" + strconv.Itoa(seat) }
