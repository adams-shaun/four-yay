package pay

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects/params"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// SacrificeMatchSpec normalizes Forge's NICKNAME spelling to CARDNAME before
// the source-aware filter is applied. The filter owns CARDNAME's object-ID
// semantics; costs use this helper at both offer and payment time so the two
// stages cannot disagree about whether a self-reference is payable.
func SacrificeMatchSpec(spec string) string {
	if strings.EqualFold(spec, "NICKNAME") {
		return "CARDNAME"
	}
	return spec
}

// manaAbilityLabel renders a mana ability's Produced$ value as the label the
// stage-1 "choose a mana ability" wheel shows (task fb-e079def5), with a
// recorded "Chosen" token already substituted (the combo-Chosen family), so
// the wheel shows "Add R", never the raw "Combo R Chosen" jargon. The raw
// script token ("Combo B R", "Any") is engine jargon a player cannot read,
// and it defeats the client's mana-pip styling for the whole wheel (the web
// tints an option list only when EVERY label is a single "Add <C>"), so a
// Talisman-of-Indulgence-shaped source rendered as plain grey text. The
// sibling ask sites (askManaColor, askTriggeredManaColor, the replacement
// colour ask in replacement.go) already emit resolved single colours -- this
// was the one label site that leaked the raw Produced string. Combo
// <colours> reads "Add B or R" (three or more: comma-separated, the last
// joined with "or"); Any/Combo Any read "Add any color" (the oracle's own
// wording, CR 107.4) unless they carry a literal Amount$ above one, which is
// then named ("Add three mana of any one color" for Any, "... in any
// combination of colors" for Combo Any) so a source whose abilities differ
// only in amount offers distinguishable wheel options (task
// mana-wheel-amount-labels); Produced$ Chosen reads "Add chosen color";
// anything else -- a plain single colour or C (the shape the wheel tints), a
// doubled "RR", a Special expression -- keeps the bare "Add <value>" shape.
// manaAbilityComboColours reports whether the ability's own Produced$ is an
// explicit MULTI-colour combo ("Combo B R") and returns the colour list in
// the ability's own token order -- the same order askManaColor offers, so the
// flattened stage-1 wheel and the stage-2 ask cannot disagree. A trailing
// "Chosen" token is first substituted from the recorded as-enters colour
// (the Thriving-lands/gate family, substituteChosenProduced). Both call
// sites guard on ma.API == "Mana" before calling (a ManaReflected ability's
// colours come from what other sources produce, never its own Produced$
// token), so this helper's Produced$ read is Mana-attributed
// (apiSpecificRulesSA) rather than joining the generic rules union.
func ManaAbilityComboColours(ma *cards.SA, chosen string) ([]string, bool) {
	if ma == nil || ma.API != "Mana" {
		return nil, false
	}
	produced := SubstituteChosenProduced(params.ManaOf(ma).Produced, chosen)
	cols, ok := params.ComboColours(produced)
	if !ok || len(cols) <= 1 {
		return nil, false
	}
	return cols, true
}

// manaAbilityProduced reads a mana ability's Produced$ value. Every caller
// passes a mana ability (the intrinsic-append dedup inside
// availableManaAbilitiesUsing's CR 305.6 walk), so the param census
// attributes the read to api:Mana alone (apiSpecificRulesSA) -- left in the
// generic union it would mask every other API's unread Produced$ (measured:
// api:Sacrifice/api:DealDamage).
func manaAbilityProduced(ma *cards.SA) string {
	return params.ManaOf(ma).ProducedRaw
}

// ManaAbilitiesProduce reports whether some ability of mas has Produced$
// exactly color (the granted-intrinsic dedup key).
func ManaAbilitiesProduce(mas []*cards.SA, color string) bool {
	for _, ma := range mas {
		if manaAbilityProduced(ma) == color {
			return true
		}
	}
	return false
}

// Two riders make a paid ability recognisable on the wheel (fb-877b8f8f,
// fb-bbe4fd8f: Phyrexian Tower's "{T}, Sacrifice a creature: Add {B}{B}"
// read "Add B", so the player saw no way to sacrifice): a cost beyond the
// shared {T} is named in front of the production (manaAbilityCostPrefix,
// "Sacrifice 1 creature: Add BB"), and a literal Amount$ repeats a single
// pip (manaAmountPips, "Add BB"). An ability with neither keeps the bare
// shape byte-for-byte.
func ManaAbilityLabel(ma *cards.SA, chosen string) string {
	return ManaAbilityCostPrefix(ma) + manaProducedLabel(ma, chosen)
}

func manaProducedLabel(ma *cards.SA, chosen string) string {
	prod := manaProductionSA(ma)
	mp := params.ManaOf(prod)
	produced := SubstituteChosenProduced(mp.Produced, chosen)
	switch manaProducedLabelCodes.Code(string(produced)) {
	case manaProducedLabelAny:
		// A literal amount above one is named so a source whose abilities
		// differ only in amount -- Sceptre of Eternal Glory's one-mana and
		// three-mana "any color" abilities -- offers two distinguishable
		// wheel options. Without it both read "Add any color", and a manual
		// payer could not choose the larger ability on purpose (task
		// mana-wheel-amount-labels). The two shapes keep the distinct
		// wording their stage-2 prompt uses (manaColourPrompt).
		if n, ok := literalManaAmount(mp); ok && n > 1 {
			if produced == "Combo Any" {
				return "Add " + manaNumberWord(n) + " mana in any combination of colors"
			}
			return "Add " + manaNumberWord(n) + " mana of any one color"
		}
		return "Add any color"
	case manaProducedLabelChosen:
		return "Add chosen color"
	}
	if cols, ok := params.ComboColours(produced); ok {
		if len(cols) == 1 {
			return "Add " + ManaAmountPips(prod, cols[0])
		}
		last := len(cols) - 1
		return "Add " + strings.Join(cols[:last], ", ") + " or " + cols[last]
	}
	return "Add " + ManaAmountPips(prod, produced)
}

// manaProductionSA is the SA whose Produced$/Amount$ a wheel label names: the
// ability itself, or the DB$ Mana production a chain head delegates to (an
// "AB$ ChooseColor | SubAbility$ DBMana" head carries no Produced$ of its
// own, so labelling the head directly rendered a bare "Add ").
func manaProductionSA(ma *cards.SA) *cards.SA {
	if ma == nil {
		return nil
	}
	if params.ManaOf(ma).Produced == "" {
		if sub := cards.ManaChainProduction(ma); sub != nil {
			return sub
		}
	}
	return ma
}

// ManaAmountPips repeats a single-pip production by the ability's literal
// Amount$ ("B" with Amount$ 2 is "BB"). Only a strconv-literal positive
// amount counts -- the manaColourPrompt rule: an absent, non-literal (X, an
// SVar, Count$) or non-positive Amount$ keeps the single pip, because a
// wrong number on the wheel is worse than none -- and only a one-letter
// WUBRGC pip is repeated, so a "RR" token or a Special expression is left as
// written. An amount above manaPipRepeatLimit keeps the bare pip too: the
// repeated spelling is the only one that expands, so its length is bounded
// here rather than in the shared literal rule.
func ManaAmountPips(ma *cards.SA, pip string) string {
	if len(pip) != 1 || !strings.Contains("WUBRGC", pip) {
		return pip
	}
	mp := params.ManaOf(ma)
	if !mp.Amount.Present {
		return pip
	}
	n, ok := literalManaAmount(mp)
	if !ok || n <= 1 {
		return pip
	}
	if n > manaPipRepeatLimit {
		// A single-pip production repeated by an absurd Amount$ would make an
		// unreadable label (and a needlessly long string). The label spelling
		// is the only consumer that expands, so the safety limit lives here;
		// the any-colour wording below never repeats and has no such bound.
		return pip
	}
	return strings.Repeat(pip, n)
}

// manaPipRepeatLimit bounds how many times manaAmountPips expands a single
// pip. It is a label-size safety valve, not a correctness gate: a literal
// amount above it is a corpus oddity, and the bare pip is the fail-safe
// spelling. (The any-colour wording names an amount once, so it is not
// bounded here.)
const manaPipRepeatLimit = 20

// literalManaAmount parses a mana ability's Amount$ parameter as a positive
// integer literal, shared by manaAmountPips (which repeats a single pip) and
// the any-colour label (which names the amount). Only an absent, non-literal
// (X, an SVar, Count$) or non-positive amount fails: the rule is exactly the
// manaColourPrompt rule, which also accepts every positive literal -- a wheel
// label for "Add 21 mana of any one color" must be as faithful as its
// stage-2 prompt, not silently fall back to "Add any color". A wrong number
// on the wheel is worse than none, but a refused large number is worse still.
func literalManaAmount(mp *params.ManaParams) (int, bool) {
	if !mp.AmountIsLit || mp.AmountLit <= 0 {
		return 0, false
	}
	return mp.AmountLit, true
}

// manaNumberWord is a literal mana amount in words, for the any-colour
// stage-1 label ("Add three mana of any one color"). Amounts past twenty
// fall back to digits ("Add 21 mana of any one color"): the wording stays
// faithful rather than inventing a bound the stage-2 prompt does not apply.
func manaNumberWord(n int) string {
	words := [...]string{"zero", "one", "two", "three", "four", "five", "six",
		"seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen",
		"fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen", "twenty"}
	if n >= 0 && n < len(words) {
		return words[n]
	}
	return strconv.Itoa(n)
}

// manaAbilityCostPrefix names a mana ability's activation cost BEYOND the
// {T} every wheel option shares, as "<Cost phrase>: " -- empty when the cost
// is the bare tap (or blank), so the common "Add C" label is unchanged. It
// phrases the parsed cost with manaAbilityCostPhrase: costPhrase with the tap
// clause cleared (every option on the wheel is tapping this source, so the
// tap is not what the player is choosing between), plus the article and
// origin-zone wording a wheel label wants.
func ManaAbilityCostPhrase(c costvocab.Cost) string {
	phrase := costvocab.CostPhrase(c)
	// costPhrase keeps the synthesized count as digits (its objectPhrase),
	// which reads as engine jargon on a mana wheel: "Sacrifice 1 creature".
	// Replace each object clause with its article form for a single object
	// ("a creature"), keyed on the parsed spec's own noun -- so an artifact
	// or enchantment sacrifice reads the same as a creature one, and the
	// next object noun the grammar learns needs no new string here. A count
	// other than one, an announced X count and an embedded description keep
	// objectPhrase's exact text, so N > 1 keeps its number.
	for _, part := range c.Sac {
		phrase = replaceObjectClause(phrase, part, "permanent")
	}
	for _, part := range c.Discard {
		if strings.EqualFold(part.Spec, "Hand") {
			// Forge's Discard<N/Hand> pays the WHOLE hand (N is the
			// discard-all marker, 0 on the corpus's Lion's Eye Diamond),
			// so objectPhrase's "0 card"/"1 card" is not the payment.
			phrase = strings.Replace(phrase, "discard "+costvocab.ObjectPhrase(part, "card"), "discard your hand", 1)
			continue
		}
		phrase = replaceObjectClause(phrase, part, "card")
	}
	for _, part := range c.Exile {
		clause := costvocab.ObjectPhrase(part, "card")
		repl := articleObjectClause(part, "card") + exileOriginPhrase(part.Zone)
		phrase = strings.Replace(phrase, "exile "+clause, "exile "+repl, 1)
	}
	return phrase
}

// replaceObjectClause swaps one cost part's rendered object clause
// (":sacrifice 1 creature") for its article form within an already-rendered
// phrase. It is a no-op when the two are identical.
func replaceObjectClause(phrase string, part costvocab.CostPart, defNoun string) string {
	clause := costvocab.ObjectPhrase(part, defNoun)
	repl := articleObjectClause(part, defNoun)
	if clause == repl {
		return phrase
	}
	return strings.Replace(phrase, clause, repl, 1)
}

// articleObjectClause renders one cost part's object the way the wheel
// reads: "a creature" for a single ordinary object, objectPhrase's own text
// for every other shape (a count, an announced X, or a corpus description
// that already names the object). specNoun supplies the noun, so the next
// object type the grammar learns is covered without a new string here.
func articleObjectClause(part costvocab.CostPart, defNoun string) string {
	if !part.Announced && part.N == 1 && part.Desc == "" {
		return articleNoun(costvocab.SpecNoun(part.Spec, defNoun))
	}
	return costvocab.ObjectPhrase(part, defNoun)
}

// articleNoun returns the indefinite article plus noun ("a creature", "an
// artifact"), so a single object cost reads naturally.
func articleNoun(noun string) string {
	if noun == "" {
		return noun
	}
	switch noun[0] {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return "an " + noun
	}
	return "a " + noun
}

// exileOriginPhrase names the origin zone an exile cost pays from, so
// "Exile a card" reads as the oracle's "Exile a card from your hand". The
// zero zone is the hand default the mana continuation itself uses
// (manaExiles); a graveyard exile names its zone too.
func exileOriginPhrase(z state.Zone) string {
	switch z {
	case 0, state.ZHand:
		return " from your hand"
	case state.ZGraveyard:
		return " from your graveyard"
	default:
		return ""
	}
}

func ManaAbilityCostPrefix(ma *cards.SA) string {
	raw := strings.TrimSpace(ma.ParamStr(cards.PKCost))
	if raw == "" {
		return ""
	}
	c := costvocab.ParseCost(raw)
	c.Tap = false
	phrase := ManaAbilityCostPhrase(c)
	if phrase == "" {
		return ""
	}
	return strings.ToUpper(phrase[:1]) + phrase[1:] + ": "
}

func ActivationTapCostUnavailable(o *state.Object, cost *costvocab.Cost) bool {
	return o == nil || (cost.Tap && o.Tapped) || (cost.Untap && !o.Tapped)
}

// manaTapsPayable reports whether a mana ability's literal tapXType<N/Spec>
// parts have enough untapped matching permanents, reserving the given set
// (the sacrifice/discard/exile picks already claimed), the source when the
// cost's own {T} taps it, and each earlier tap part's picked permanents. The
// X form (Forge's tapXType<X/Spec> head) is settled by the payment election in
// continueManaDiscard -- the count announced there is the ability's X -- so an X
// part with no eligible permanent is affordable at X=0 (the cast path's
// tapPermanentCostAsk does the same), while a non-X dynamic head still fails
// closed HERE: no "any number" election exists beside a mana ability yet.
func CostHasDynamicXTap(cost *costvocab.Cost) bool {
	for _, part := range cost.TapPermanent {
		if part.Dyn == "X" {
			return true
		}
	}
	return false
}

// ManaAbilityWithPaidX binds the dynamic tap election to this activation's
// production amount. The compiled SA is immutable, and an explicit literal
// Amount$ survives the mana-colour continuation without inheriting another
// object's X: Hazel's `Amount$ X`/`SVar:X:Count$xPaid` is the elected tap
// count, which has no home on the off-stack mana path's Ctx (xPaid resolves
// the CAST's paid X, so reading it here would leak an enclosing spell's X).
// Copy-on-write keeps every other activation of the same card untouched.
func ManaAbilityWithPaidX(ma *cards.SA, x int32) *cards.SA {
	cp := *ma
	cp.Params = make(map[string]string, len(ma.Params))
	for k, v := range ma.Params {
		cp.Params[k] = v
	}
	cp.SetParam(cards.PKAmount, fmt.Sprint(x))
	return &cp
}

// TriggeredManaColourChoice finds the first Mana sub-ability in a triggered
// ability's chain whose Produced$ is a colour choice, with the colours it
// offers. nil when the chain adds only fixed mana (or none).
func TriggeredManaColourChoice(sa *cards.SA) (*cards.SA, []string) {
	for d := 0; sa != nil && d < 32; d, sa = d+1, sa.Sub {
		if sa.API != "Mana" {
			continue
		}
		produced := params.ManaOf(sa).Produced
		if produced == "Any" || produced == "Combo Any" {
			return sa, []string{"W", "U", "B", "R", "G"}
		}
		if colours, ok := params.ComboColours(produced); ok {
			return sa, colours
		}
	}
	return nil, nil
}

// WithProduced copies the chain from head down to target, with target's
// Produced$ rewritten to the chosen colour. Corpus SAs are shared immutable
// data, so the rewrite never touches them.
func WithProduced(head, target *cards.SA, produced string) *cards.SA {
	return withManaProduction(head, target, produced, "")
}

// WithManaAllocation records a resolved Combo allocation as one concrete
// symbol per selected unit and prevents effMana from multiplying that string
// by the original Amount$ again.
func WithManaAllocation(head, target *cards.SA, produced string) *cards.SA {
	return withManaProduction(head, target, produced, "1")
}

func withManaProduction(head, target *cards.SA, produced, amount string) *cards.SA {
	if head == nil {
		return nil
	}
	cp := *head
	if head == target {
		cp.Params = make(map[string]string, len(head.Params)+1)
		for k, v := range head.Params {
			cp.Params[k] = v
		}
		cp.SetParam(cards.PKProduced, produced)
		if amount != "" {
			cp.SetParam(cards.PKAmount, amount)
		}
		return &cp
	}
	cp.Sub = withManaProduction(head.Sub, target, produced, amount)
	return &cp
}

// SubstituteChosenProduced replaces the literal "Chosen" token in a Mana
// ability's Produced$ value with the recorded as-enters chosen colour
// (state.Object.ChosenColor): the bare "Chosen" (Quirion Elves) and the
// "Combo <letter> Chosen" family (Thriving Bluff, Citadel Gate, the five
// thriving lands and five gates: "Add {R} or one mana of the chosen
// color"). It is a read, not a choice; with nothing valid recorded the
// value is returned unchanged and the caller keeps its loud fail-closed
// handling (never invent a colour). The classifier for the substituted
// value stays effects.ComboColours -- a pure string classifier shared with
// the tests and the mana projection, deliberately not taught about state --
// so the substitution happens here in rules, where ChosenColor is readable,
// and every caller (resolveManaEffect, manaAbilityComboColours,
// manaAbilityLabel) sees one consistent value. Measured on the corpus,
// every Chosen token is the value's LAST token ("Combo Chosen" has no fixed
// letter), so the rewrite only ever touches the tail.
func SubstituteChosenProduced(produced, chosen string) string {
	if chosen == "" {
		return produced
	}
	trimmed := strings.TrimSpace(produced)
	if trimmed == "Chosen" || trimmed == "ChosenColor" || trimmed == "ComboChosen" {
		return chosen
	}
	toks := strings.Fields(trimmed)
	if len(toks) >= 2 && toks[0] == "Combo" && (toks[len(toks)-1] == "Chosen" || toks[len(toks)-1] == "ChosenColor") {
		toks[len(toks)-1] = chosen
		// The recorded colour can equal a fixed letter ("Combo R Chosen" with
		// R recorded): dedup so the value stays "Combo R" -- a decision nobody
		// could answer differently resolves directly, and the duplicate
		// "Combo R R" would otherwise pose a two-option ask over one colour.
		out := toks[:1]
		for _, tok := range toks[1:] {
			seen := false
			for _, prev := range out {
				if prev == tok {
					seen = true
					break
				}
			}
			if !seen {
				out = append(out, tok)
			}
		}
		return "Combo " + strings.Join(out[1:], " ")
	}
	return produced
}

// ManaReturnCostSupported reports whether a mana ability's Return<N/Spec>
// parts are the shape the off-stack mana path pays: none at all, or exactly
// the self-return Return<1/CARDNAME> with no sacrifice/discard/exile part
// beside it (those route through the manaDiscardActivation continuation,
// which does not settle a return).
func ManaReturnCostSupported(cost costvocab.Cost) bool {
	if len(cost.Return) == 0 {
		return true
	}
	if len(cost.Return) != 1 || len(cost.Sac) > 0 || len(cost.Discard) > 0 || len(cost.Exile) > 0 {
		return false
	}
	part := cost.Return[0]
	return part.N == 1 && strings.EqualFold(SacrificeMatchSpec(part.Spec), "CARDNAME")
}

// ManaColourPrompt names the amount of mana the ability adds when the script
// carries an EXPLICIT, positive literal Amount$, so a player choosing the
// colour of "Add three mana of any one color" (Lion's Eye Diamond), or every
// unit of a Combo allocation, sees the whole deal instead of a bare "Choose a
// colour of mana" that reads like the card only offered one colour of mana.
// Every other shape keeps the generic prompt: an absent Amount$ (the prompt
// must not invent "1" for the pool that effMana will actually resolve), a
// non-literal amount (X, Y, an SVar or inline Count$ expression) the ask site
// cannot price, and a non-positive literal. The wording distinguishes
// Produced$ Any (one colour covers every unit) from Combo Any (the selected
// units may be split); a restricted "Combo <colours>" shape is not "any"
// colour, so its prompt only names the amount; a ColorIdentity shape names
// the commander-identity restriction.
func ManaColourPrompt(ma *cards.SA) string {
	generic := "Choose a colour of mana"
	mp := params.ManaOf(ma)
	shape := mp.Produced
	identity := IsColourIdentityProduced(shape)
	if identity {
		generic = "Choose a colour in your commander's color identity"
	}
	if !mp.Amount.Present {
		// No Amount$ param: stay generic — the prompt must not invent an
		// amount the script never stated.
		return generic
	}
	n := mp.AmountLit
	if !mp.AmountIsLit || n <= 0 {
		// A non-literal amount (X, Y, an SVar or inline Count$ expression)
		// or a non-positive literal: the ask site cannot price it, and a
		// wrong number in the prompt is worse than no number.
		return generic
	}
	switch {
	case identity:
		return fmt.Sprintf("Add %d mana of any color in your commander's color identity — choose the colour", n)
	case shape == "Any":
		return fmt.Sprintf("Add %d mana of any one color — choose the colour", n)
	case shape == "Combo Any":
		return fmt.Sprintf("Add %d mana in any combination of colors — choose the colours", n)
	case strings.HasPrefix(shape, "Combo "):
		return fmt.Sprintf("Add %d mana — choose the colours", n)
	default:
		return fmt.Sprintf("Add %d mana — choose the colour", n)
	}
}

// IsColourIdentityProduced reports whether a Produced$ value is the
// commander-identity choice shape — the literal "ColorIdentity" or the combo
// form "Combo ColorIdentity" (Command Tower, Arcane Signet, Commander's
// Sphere, Hidden Hideout, Opal Palace, Path of Ancestry). Shared by the
// activation-path branch in resolveManaEffect and manaColourPrompt's
// restricted wording so the two cannot disagree.
func IsColourIdentityProduced(produced string) bool {
	return produced == "ColorIdentity" || produced == "Combo ColorIdentity"
}

// ChainGatesOnActivationCount reports whether sa's SubAbility$ chain carries
// a ConditionActivationLimit$ gate ("if this ability has been activated four
// or more times this turn" -- Farrelite Priest, Initiates of the Ebon Hand).
// A mana ability has no AbilityPush, so such a chain needs the ManaActivate
// census marker for Ctx.ActivationsThisTurn to count it; the marker is
// emitted only for these carriers so no other game's log changes.
func ChainGatesOnActivationCount(sa *cards.SA) bool {
	for sub, n := sa, 0; sub != nil && n < 32; sub, n = sub.Sub, n+1 {
		if strings.TrimSpace(sub.ParamStr(cards.PKConditionActivationLimit)) != "" {
			return true
		}
	}
	return false
}

// ManaCostPartsSettleable is the mana path's fail-closed whitelist: a mana
// ability is activated off the stack by resolveManaAbilityRefOriginal and
// the manaDiscardActivation continuation, which settle exactly mana/life
// (payManaConvFor), {T}, Mill, SubCounter on the source, PayEnergy<N>,
// AddCounter on the source, Exert<1/CARDNAME>, Sac, Discard, Exile, the
// literal tapXType<N/Spec> tap (its election rides the same continuation)
// the self-Return, and a literal CollectEvidence<N> (Cryptex: the evidence
// election rides the same continuation). Every other part -- an unmodelled
// token (Cost.Unknown: Pili-Pala's {Q}, Benthic Explorers' untapYType, both
// of which used to be priced as one phantom generic), a CollectEvidence whose
// amount is a name or an announced X (resolved only by the cast flow), a
// Draw/DamageYou/PutToLib/MoveToGrave/RollDice part, a dynamic PayEnergy<X>
// or a SubCounter anchored to another permanent (Jetfire's
// RemoveAnyCounter) -- has no settle here, so the ability is refused rather
// than activated with that part silently free. The remaining refusals (X,
// Reveal, Behold, a DYNAMIC tapXType<X/...>/<Any/...> part, Blight, Forage,
// LifeX, an unsupported Return) live beside the call site.
func ManaCostPartsSettleable(cost costvocab.Cost) bool {
	if len(cost.Unknown) > 0 || ManaEvidenceNeed(cost) < 0 || len(cost.Draw) > 0 ||
		len(cost.DamageYou) > 0 || len(cost.GainLife) > 0 || len(cost.PutToLib) > 0 || len(cost.MoveToGrave) > 0 ||
		len(cost.RollDice) > 0 || cost.LifeHalfUp {
		return false
	}
	for _, part := range cost.Energy {
		if part.Spec == "X" {
			return false
		}
	}
	return true
}

type manaProducedLabelCode uint16

const (
	manaProducedLabelAny manaProducedLabelCode = iota + 1
	manaProducedLabelChosen
)

var manaProducedLabelCodes = state.NewStrCodes(
	state.StrEntry[manaProducedLabelCode]{Key: "Any", Val: manaProducedLabelAny},
	state.StrEntry[manaProducedLabelCode]{Key: "Combo Any", Val: manaProducedLabelAny},
	state.StrEntry[manaProducedLabelCode]{Key: "Chosen", Val: manaProducedLabelChosen},
)
