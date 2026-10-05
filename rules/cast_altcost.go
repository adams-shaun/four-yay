package rules

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// kickerCost and surgeCost resolve a face's own parameterised keyword to a
// parsed Cost, reporting whether the keyword is printed at all.
func kickerCost(f *cards.Face) (Cost, bool) {
	if _, _, two := twoPartKickerCosts(f); two {
		// The and/or two-part Kicker is its own option family (legal.go's
		// kicked1/kicked2/kickedboth offers, one per independently payable
		// part): the single "kicked" option must not also exist for such a
		// face -- its whole-string parse would degrade the colon separator
		// into a generic pip and charge the both-parts price for a
		// single-part choice.
		return Cost{}, false
	}
	s, ok := f.KeywordParam("Kicker")
	if !ok {
		return Cost{}, false
	}
	return ParseCost(s), true
}

// twoPartKickerCosts resolves the and/or Kicker ("Kicker {G} and/or {1}{U}",
// Forge's colon-separated two-part Kicker:<a>:<b> keyword line -- 18 corpus
// files at the pin, the Volver/Battlemage/Involver cycle family, Wastescape
// Battlemage the repo-deck carrier) to its two independently payable parts:
// each may be paid alone or both together (CR 601.2b's optional additional
// costs, each declared separately). ok=false for the single-cost Kicker
// (every colon-free param) and for a colon form whose parts do not parse
// clean -- a part with an unmodelled token stays a labelled census gap
// rather than being silently charged.
func twoPartKickerCosts(f *cards.Face) (Cost, Cost, bool) {
	s, ok := f.KeywordParam("Kicker")
	if !ok {
		return Cost{}, Cost{}, false
	}
	a, b, is := strings.Cut(s, ":")
	if !is || strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return Cost{}, Cost{}, false
	}
	ca, cb := ParseCost(a), ParseCost(b)
	if len(ca.Unknown) > 0 || len(cb.Unknown) > 0 {
		return Cost{}, Cost{}, false
	}
	return ca, cb, true
}

// entwineCost resolves the Entwine keyword's additional cost (CR 702.42,
// Forge's K:Entwine:<cost>). Entwine is an OPTIONAL additional cost paid once
// as the spell is cast; if paid, every eligible mode is chosen rather than the
// normal one. The cost forms the corpus carries are plain mana (30 of the 32
// carriers at the pin) and a sacrifice (Sac<3/Land> on Betrayal of Flesh and
// Sac<2/Land> on Solar Tide), and ParseCost models both, so the cost is not
// withheld for any corpus carrier -- but ANY cost ParseCost cannot price fails
// closed here (the replicateCost direction), leaving the card's gap in the
// coverage report rather than charging a degraded generic.
func entwineCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Entwine")
	if !ok {
		return Cost{}, false
	}
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

func surgeCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Surge")
	if !ok {
		return Cost{}, false
	}
	return ParseCost(s), true
}

// isCharmSpell reports whether f's spell ability is a modal Charm (the only
// shape Entwine has a modelled meaning for: "follow the instructions of all
// its modes"). It is the single reader legal.go's entwined offer and any
// future entwine site share, so the offer and the all-modes announcement
// cannot disagree about which faces are modal.
func isCharmSpell(f *cards.Face) bool {
	sa := f.SpellAbility()
	return sa != nil && sa.API == "Charm" && effects.CharmOf(sa).HasChoices
}

// replicateCost resolves the Replicate keyword's payment cost (CR 702.55a,
// Forge's K:Replicate:<cost>), the surgeCost shape. A cost carrying a token
// ParseCost cannot model is withheld (the fail-closed direction
// twoPartKickerCosts takes) rather than charged as degraded generic mana.
func replicateCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Replicate")
	if !ok {
		return Cost{}, false
	}
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// multikickerCost resolves the Multikicker keyword's PER-PAYMENT cost
// (CR 702.43, Forge's K:Multikicker:<cost>), the replicateCost shape: the
// same payment may be made any number of times as the spell is cast, so the
// offer gates on ONE payment being payable and the count ask
// (multikickAsk) settles how many. A cost carrying a token ParseCost cannot
// model is withheld (the replicateCost fail-closed direction).
func multikickerCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Multikicker")
	if !ok {
		return Cost{}, false
	}
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// squadCost resolves the Squad keyword's PER-PAYMENT cost (CR 702.66, Forge's
// K:Squad:<cost>), the replicateCost/multikickerCost shape: "you may pay
// [cost] any number of times" as the spell is cast, so the offer gates on ONE
// payment being payable and the count ask (squadAsk) settles how many. A cost
// carrying a token ParseCost genuinely cannot model is withheld (the same
// fail-closed direction); a modelled non-mana part (Thrill-Kill Disciple's
// "1 Discard<1/Card>", Ruthless Radrat's "ExileFromGrave<4/Card/cards>") is
// accepted here and its payability decided by the ordinary offer gate
// (nonManaCastable), exactly like any other cast cost.
func squadCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Squad")
	if !ok {
		return Cost{}, false
	}
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// keywordAltCost resolves any of the alternative-cast keyword family
// (Evoke, Dash, Overload, Warp, Madness) to a parsed Cost, reporting whether
// the keyword is printed at all. All five are "you may cast this for [cost]
// instead of its mana cost" shapes whose post-cast behaviour lives elsewhere
// (the ETB machinery for evoke, modeFlags for the rest), so they share one
// parameter read.
func keywordAltCost(f *cards.Face, head string) (Cost, bool) {
	s, ok := f.KeywordParam(head)
	if !ok {
		return Cost{}, false
	}
	// Forge's two-field K:<Head>:<N>:<cost> form (Impending, CR 702.176a)
	// leads with the count; the cost is the second field.
	if n, rest, cut := strings.Cut(s, ":"); cut && n != "" && strings.Trim(n, "0123456789") == "" {
		s = rest
	}
	return ParseCost(strings.TrimSpace(s)), true
}

// blitzCosts is the shared offer/charge reader for every printed or granted
// Blitz instance. Distinct modes preserve separate costs when a printed
// keyword and a layer-6 grant coexist; grant filters and CardManaCost are
// resolved against the proposed spell before it is offered.
func (e *Engine) blitzCosts(p state.PlayerID, id state.ObjID) []struct {
	mode string
	cost Cost
} {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	if !e.stackKeywordPossibleH(id, kwhBlitz) {
		return nil
	}
	var out []struct {
		mode string
		cost Cost
	}
	blitzIndex := 0
	for _, keyword := range e.derivedWith(id, state.ZStack).Keywords {
		if !strings.EqualFold(cardsKeywordHead(keyword), "Blitz") {
			continue
		}
		blitzIndex++
		raw := ""
		if i := strings.IndexByte(keyword, ':'); i >= 0 {
			raw = strings.TrimSpace(keyword[i+1:])
		}
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) == 2 {
			spec, admits := e.castProvenanceAdmitsWindow(parts[1], id, p, true)
			if !admits {
				continue
			}
			sc := e.specCtx(0, p)
			sc.AsStack = true
			if !e.matchesSpec(spec, id, sc) {
				continue
			}
		}
		var toks []string
		for _, tok := range strings.Fields(parts[0]) {
			if strings.EqualFold(tok, "CardManaCost") {
				toks = append(toks, strings.Fields(o.Face().ManaCost)...)
			} else {
				toks = append(toks, tok)
			}
		}
		c := ParseCost(strings.Join(toks, " "))
		if len(c.Unknown) != 0 {
			continue
		}
		mode := "blitzed"
		if blitzIndex > 1 {
			mode = fmt.Sprintf("blitzed_grant_%d", blitzIndex)
		}
		out = append(out, struct {
			mode string
			cost Cost
		}{mode, c})
	}
	return out
}

// blitzCost retains the common single-cost read for existing consumers/tests.
func (e *Engine) blitzCost(p state.PlayerID, id state.ObjID) (Cost, bool) {
	costs := e.blitzCosts(p, id)
	if len(costs) == 0 {
		return Cost{}, false
	}
	return costs[0].cost, true
}

func (e *Engine) derivedKeywordParamAt(id state.ObjID, head string, zone state.Zone) (string, bool) {
	for _, k := range e.derivedWith(id, zone).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), head) {
			if i := strings.IndexByte(k, ':'); i >= 0 {
				return strings.TrimSpace(k[i+1:]), true
			}
			return "", true
		}
	}
	return "", false
}

// morphDownFamily reports the face-down cast mode a printed face offers:
// "morphed" for K:Morph, "megamorphed" for K:Megamorph and "disguised" for
// K:Disguise (CR 702.37a/702.168a/702.169a), "" when the face carries none
// of the family. KeywordParam is the one derived read (a layer-6
// AddKeyword$ grant would surface through it too); the {3} face-down cost
// itself is family-independent, so the offer and the charge price it
// directly, without a parameter.
func morphDownFamily(f *cards.Face) string {
	if _, ok := f.KeywordParam("Morph"); ok {
		return "morphed"
	}
	if _, ok := f.KeywordParam("Megamorph"); ok {
		return "megamorphed"
	}
	if _, ok := f.KeywordParam("Disguise"); ok {
		return "disguised"
	}
	return ""
}

func buybackCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Buyback")
	if !ok {
		return Cost{}, false
	}
	return ParseCost(s), true
}

// retraceExtra is Retrace's additional cost (CR 702.81a): discard a land
// card, in addition to the spell's other costs. Unlike the alternative-cost
// keyword family, Retrace is NOT a cost substitution -- the printed mana cost
// is still paid -- so this returns only the ADDITIONAL part, folded onto the
// printed base by both the offer gate (legal.go's graveyard walk) and
// beginCast's "retrace" mode, through the one definition so the two cannot
// disagree about what the cast costs.
func retraceExtra() Cost {
	return Cost{Discard: []CostPart{{Spec: "Land", N: 1}}}
}

// jumpstartExtra is Jump-start's additional cost (CR 702.84a): discard a card,
// in addition to the spell's other costs. Jump-start is NOT a cost
// substitution -- the oracle reads "in addition to paying its other costs" --
// so, exactly like retraceExtra, this returns only the ADDITIONAL part, folded
// onto the printed base by both the offer gate (legal.go's graveyard walk) and
// beginCast's "jumpstart" mode, through the one definition so the two cannot
// disagree. Unlike Retrace the discarded card may be any card (“a card”,
// not “a land card”), and the spell is exiled on resolution -- the flashback
// destination, charged by modeFlags' FlagJumpstart.
func jumpstartExtra() Cost {
	return Cost{Discard: []CostPart{{Spec: "Card", N: 1}}}
}

// altAddCostParts splits a face's AlternateAdditionalCost keyword into its
// alternative parts: the parameter is the parts joined by ":" (e.g. Bone
// Shards' "Sac<1/Creature>:Discard<1/Card>", Redirect Lightning's
// "PayLife<5>:2"). "As an additional cost to cast this spell, [A] or [B]"
// is a MANDATORY either-or (CR 601.2h), so the cast flow asks which one and
// folds the chosen part into the total cost. An empty or missing keyword
// yields nil; a parameter with no ":" yields nil (a single-part form would
// be an ordinary additional cost, which no corpus line uses -- the keyword's
// whole point is the either-or).
func altAddCostParts(f *cards.Face) []string {
	param, ok := f.KeywordParam("AlternateAdditionalCost")
	if !ok {
		return nil
	}
	parts := strings.Split(param, ":")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func harmonizeCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Harmonize")
	if !ok {
		return Cost{}, false
	}
	return ParseCost(s), true
}

// harmonizePayment applies the keyword's creature-power reduction in stable
// battlefield order and returns the creatures that pay it by becoming tapped.
func (e *Engine) harmonizePayment(p state.PlayerID, id state.ObjID, c Cost) (Cost, []state.ObjID) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || !o.Face().HasKeyword("Harmonize") {
		return c, nil
	}
	var tapped []state.ObjID
	for _, cid := range e.G.Zone(state.ZBattlefield, p) {
		if c.Generic == 0 {
			break
		}
		co := e.G.Obj(cid)
		if co == nil || co.Tapped || co.Face() == nil || !co.EffectiveIsCreature() || co.BestowedAttached() || co.ReconfiguredAttached() {
			continue
		}
		// The reduction is the creature's ACTUAL power (CR 702.46a: "reduce
		// that spell's generic cost by its power"): layer-7 effects and
		// P1P1/M1M1 counters apply, not the printed face. harmonizePayment
		// and convokeAsk's offer must read the same number or the offer
		// gate and the payment disagree on what the creature funds.
		reduce := e.Derived(cid).Power
		if reduce <= 0 {
			continue
		}
		if reduce > c.Generic {
			reduce = c.Generic
		}
		c.Generic -= reduce
		tapped = append(tapped, cid)
	}
	return c, tapped
}

type suspendInfo struct {
	time    int32
	timeX   bool
	minTime int32
	cost    Cost
}

// convokePayment is one announced non-mana payment (pay.ConvokePayment).
type convokePayment = pay.ConvokePayment

// hasCastConvoke reports whether the spell being cast carries Convoke once
// it is on the stack: the printed keyword, or a layer-6 grant (Chief
// Engineer's "Artifact spells you cast have convoke") whose AffectedZone$
// scope reaches the cast spell. The announcement (CR 601.2b) runs while the
// announced spell is still in hand, so the evaluation pretends the zone is
// the stack (derivedWith's override); a wasCast Affected$ predicate already
// matches because it keys on the object being a cast spell, which it is.
func (e *Engine) hasCastConvoke(id state.ObjID) bool {
	if !e.stackKeywordPossibleH(id, kwhConvoke) {
		return false
	}
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), "Convoke") {
			return true
		}
	}
	return false
}

// hasCastImprovise reports whether the spell being cast carries Improvise
// (CR 702.66), read the same way hasCastConvoke reads Convoke: the printed
// keyword or a layer-6 grant reaching the cast spell. Inspiring Statuary
// grants Improvise to nonartifact spells, so the grant path is live.
func (e *Engine) hasCastImprovise(id state.ObjID) bool {
	if !e.stackKeywordPossibleH(id, kwhImprovise) {
		return false
	}
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), "Improvise") {
			return true
		}
	}
	return false
}

// hasCastConspire reports whether the spell being cast carries Conspire
// (CR 702.78a), read exactly the way hasCastConvoke reads Convoke: the
// printed keyword or a layer-6 grant whose AffectedZone$ scope reaches the
// cast spell. The announcement runs while the spell is still in hand, so
// derivedWith overrides the zone to the stack (which is also what lets a
// `wasCast` Affected$ grant like Wort, the Raidmother's or Raiding Schemes'
// match). The offer gate and the provenance read share this one helper so
// the two stages cannot disagree about whether the spell is conspirable.
func (e *Engine) hasCastConspire(id state.ObjID) bool {
	if !e.stackKeywordPossibleH(id, kwhConspire) {
		return false
	}
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), "Conspire") {
			return true
		}
	}
	return false
}

// cascadeInstances counts the spell's Cascade instances: one per printed
// K:Cascade line plus one per layer-6 AddKeyword$ Cascade grant whose
// AffectedZone$ scope reaches the spell, evaluated against the stack the way
// hasCastConvoke's read is (derivedWith's zone override; the spell is on the
// stack by the time the cast is paid for, so the override and the live zone
// agree here).
func (e *Engine) cascadeInstances(id state.ObjID) int {
	n := 0
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), "Cascade") {
			n++
		}
	}
	return n
}

// conspireCandidates returns the untapped creatures the caster controls that
// share at least one colour with the spell being cast (CR 702.78a's "two
// untapped creatures you control that share a color with it"). Order is the
// deterministic battlefield zone order (costCandidates' own walk), so the
// option list and a replay's re-derivation agree. A colourless spell has no
// colour to share and returns an empty set -- the offer then never appears,
// which is the correct reading of the rule for a Conspire carrier.
func (e *Engine) conspireCandidates(p state.PlayerID, id state.ObjID) []state.ObjID {
	spell := e.G.Obj(id)
	if spell == nil || spell.Face() == nil {
		return nil
	}
	want := e.Colors(spell.ID)
	if want == "" {
		want = effects.ColorsOf(spell)
	}
	if want == "" {
		return nil
	}
	var out []state.ObjID
	for _, cid := range e.G.Zone(state.ZBattlefield, p) {
		co := e.G.Obj(cid)
		if co == nil || co.Tapped {
			continue
		}
		if !e.matchesSpecFrom("Creature.YouCtrl", cid, p, id) {
			continue
		}
		colors := e.objColors(co)
		for i := 0; i < len(colors); i++ {
			if strings.ContainsRune(want, rune(colors[i])) {
				out = append(out, cid)
				break
			}
		}
	}
	return out
}

// casualtySpec reads both printed and layer-6 granted keywords against the
// proposed stack zone. A grant scoped to AffectedZone$ Stack therefore works
// before pushCast, including CheckSVar-gated first-spell grants. The keyword
// line is `Casualty:<amount>` with optional script riders after further
// colons: the corpus's one rider carrier (Ob Nixilis, the Adversary) spells
// `K:Casualty:X:NonLegendary$ True | SetLoyalty$ Casualty:The copy isn't
// legendary and has starting loyalty X.` -- the amount token is X, the
// sacrificed creature's power (CR 702.249a's variable form), and the riders
// name the COPY's characteristics. A line whose amount parses neither as a
// nonnegative integer nor as X is skipped, the same skip casualtyValue made;
// the first parseable line wins.
type casualtyInfo struct {
	threshold    int32 // the fixed threshold, when !variable
	variable     bool  // amount token X: the amount is the sacrificed creature's power
	nonLegendary bool  // NonLegendary$ True rider: the copy isn't legendary
	setLoyalty   bool  // SetLoyalty$ Casualty rider: the copy's starting loyalty is the amount
}

func (e *Engine) casualtySpec(id state.ObjID) (casualtyInfo, bool) {
	if !e.stackKeywordPossibleH(id, kwhCasualty) {
		return casualtyInfo{}, false
	}
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if !strings.EqualFold(cardsKeywordHead(k), "Casualty") {
			continue
		}
		if info, ok := parseCasualtyLine(k); ok {
			return info, true
		}
	}
	return casualtyInfo{}, false
}

// parseCasualtyLine splits one Casualty keyword line into its amount token
// (the text up to the first further colon) and the riders/description tail
// after it, then reads the riders the corpus's one carrier spells: a literal
// `NonLegendary$ True` and a literal `SetLoyalty$ Casualty` (the copy's
// starting loyalty is the casualty amount). No other spelling is honoured --
// the tail scan is scoped to this measured shape.
func parseCasualtyLine(k string) (casualtyInfo, bool) {
	_, rest, found := strings.Cut(k, ":")
	if !found {
		return casualtyInfo{}, false
	}
	amt, riders := rest, ""
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		amt, riders = rest[:i], rest[i+1:]
	}
	var info casualtyInfo
	if strings.EqualFold(strings.TrimSpace(amt), "x") {
		info.variable = true
	} else {
		if _, err := fmt.Sscanf(strings.TrimSpace(amt), "%d", &info.threshold); err != nil || info.threshold < 0 {
			return casualtyInfo{}, false
		}
	}
	low := strings.ToLower(riders)
	info.nonLegendary = strings.Contains(low, "nonlegendary$ true")
	info.setLoyalty = strings.Contains(low, "setloyalty$ casualty")
	return info, true
}

func (e *Engine) casualtyCandidates(p state.PlayerID, spell state.ObjID, n int32) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if e.matchesSpecFrom("Creature.YouCtrl", id, p, spell) && e.Power(id) >= n {
			out = append(out, id)
		}
	}
	return out
}

// improviseCost applies CR 702.66a greedily in stable battlefield order:
// each untapped artifact controlled by p (not already committed by
// convokeCost -- the two keywords never share a carrier today, but the
// exclusion is structural) reduces the generic requirement by {1} and must
// be tapped. Improvise never pays coloured pips, so nothing else is
// consumed. It returns the reduced cost and exactly the artifacts that must
// be tapped.
func (e *Engine) improviseCost(p state.PlayerID, id state.ObjID, c Cost, committed []state.ObjID) (Cost, []state.ObjID) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || !e.hasCastImprovise(id) {
		return c, nil
	}
	skip := make(map[state.ObjID]bool, len(committed))
	for _, cid := range committed {
		skip[cid] = true
	}
	var tapped []state.ObjID
	for _, cid := range e.G.Zone(state.ZBattlefield, p) {
		if c.Generic == 0 {
			break
		}
		co := e.G.Obj(cid)
		if co == nil || co.Tapped || co.Face() == nil || !co.EffectiveIsArtifact() || co.BestowedAttached() || skip[cid] {
			continue
		}
		c.Generic--
		tapped = append(tapped, cid)
	}
	return c, tapped
}

// suspendCost parses Forge's Suspend:<time>:<cost> keyword form. X-time
// scripts put their lower bound in the leading XMin<N> cost token; the same
// announced X pays the cost and becomes the number of TIME counters.
// convokeCost commits untapped creatures in battlefield order, consuming a
// needed colour when that creature has one and otherwise one generic mana.
// It returns the reduced cost and exactly the creatures that must be tapped.
func (e *Engine) convokeCost(p state.PlayerID, id state.ObjID, c Cost) (Cost, []state.ObjID) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || !e.hasCastConvoke(id) {
		return c, nil
	}
	var tapped []state.ObjID
	for _, cid := range e.G.Zone(state.ZBattlefield, p) {
		co := e.G.Obj(cid)
		if co == nil || co.Tapped || co.Face() == nil || !co.EffectiveIsCreature() || co.BestowedAttached() || co.ReconfiguredAttached() {
			continue
		}
		used := false
		for _, col := range []byte{'W', 'U', 'B', 'R', 'G'} {
			i := state.ManaIndex(col)
			if c.Colored[i] > 0 && strings.Contains(e.objColors(co), string(col)) {
				c.Colored[i]--
				used = true
				break
			}
		}
		if !used && c.Generic > 0 {
			c.Generic--
			used = true
		}
		if used {
			tapped = append(tapped, cid)
		}
	}
	return c, tapped
}

func suspendCost(f *cards.Face) (suspendInfo, bool) {
	raw, ok := f.KeywordParam("Suspend")
	if !ok {
		return suspendInfo{}, false
	}
	n, rest, ok := strings.Cut(raw, ":")
	if !ok {
		return suspendInfo{}, false
	}
	if strings.TrimSpace(n) == "X" {
		fields := strings.Fields(rest)
		info := suspendInfo{timeX: true}
		if len(fields) > 0 && strings.HasPrefix(fields[0], "XMin") {
			min, err := strconv.ParseInt(strings.TrimPrefix(fields[0], "XMin"), 10, 32)
			if err != nil || min < 0 {
				return suspendInfo{}, false
			}
			info.minTime = int32(min)
			fields = fields[1:]
		}
		info.cost = ParseCost(strings.Join(fields, " "))
		// The corpus's X-time Suspend form charges that same X. Without a
		// cost X there is no finite legal option range to announce, so do not
		// offer a made-up bound.
		if info.cost.X == 0 {
			return suspendInfo{}, false
		}
		return info, true
	}
	time, err := strconv.ParseInt(strings.TrimSpace(n), 10, 32)
	if err != nil || time < 0 {
		return suspendInfo{}, false
	}
	return suspendInfo{time: int32(time), cost: ParseCost(rest)}, true
}

// flashbackCost is id's Flashback cost: the printed parameter if this face
// carries one, or -- Flashback granted by a continuous effect with no
// printed parameter of its own (Snapcaster Mage's shape) -- the card's own
// mana cost (CR 702.32a's "cast for its normal cost" fallback).
func (e *Engine) flashbackCost(id state.ObjID) Cost {
	o := e.G.Obj(id)
	if o == nil {
		return Cost{}
	}
	f := o.Face()
	if f == nil {
		return Cost{}
	}
	if s, ok := f.KeywordParam("Flashback"); ok {
		return ParseCost(s)
	}
	return ParseCost(f.ManaCost)
}

// extraFlashbackCosts lists the raw costs of id's flashback instances
// beyond the one flashbackCost prices, in derived keyword order and
// deduplicated against it and each other (CR 702.34a: each instance is a
// separate permission; two instances with the same cost are one choice). A
// parameterless instance -- a grant with no cost of its own (Sphinx of
// Forgotten Lore, Snapcaster Mage) -- costs the card's mana cost; a card with
// no mana cost has no payable parameterless instance and contributes none.
// Nil for the ordinary single-instance card.
func (e *Engine) extraFlashbackCosts(id state.ObjID) []string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || !e.mayHaveDerivedKeywordH(id, kwhFlashback) {
		return nil
	}
	f := o.Face()
	// Costs compare by their whitespace-separated fields (the normalised
	// form out holds), without building the normalised string for the
	// common answer: the printed instance only.
	first := e.flashbackCostString(f)
	var out []string
	for _, k := range e.Derived(id).Keywords {
		if !strings.EqualFold(cardsKeywordHead(k), kwhFlashback.S) {
			continue
		}
		raw := f.ManaCost
		if i := strings.IndexByte(k, ':'); i >= 0 {
			raw = k[i+1:]
		}
		if costFieldsEqual(raw, "") || costFieldsEqual(raw, first) ||
			slices.ContainsFunc(out, func(s string) bool { return costFieldsEqual(raw, s) }) {
			continue
		}
		out = append(out, strings.Join(strings.Fields(raw), " "))
	}
	return out
}

// costFieldsEqual reports whether a and b have the same whitespace-separated
// fields: strings.Join(strings.Fields(a), " ") == the same of b, with no
// allocation.
func costFieldsEqual(a, b string) bool {
	for {
		a = strings.TrimLeftFunc(a, unicode.IsSpace)
		b = strings.TrimLeftFunc(b, unicode.IsSpace)
		if a == "" || b == "" {
			return a == b
		}
		i := strings.IndexFunc(a, unicode.IsSpace)
		if i < 0 {
			i = len(a)
		}
		j := strings.IndexFunc(b, unicode.IsSpace)
		if j < 0 {
			j = len(b)
		}
		if a[:i] != b[:j] {
			return false
		}
		a, b = a[i:], b[j:]
	}
}

// flashbackCostString is the raw cost string flashbackCost parses.
func (e *Engine) flashbackCostString(f *cards.Face) string {
	if s, ok := f.KeywordParam("Flashback"); ok {
		return s
	}
	return f.ManaCost
}

// flashbackCostFor is the flashback cost a cast option charges: the cost it
// carries (Option.Cost, one of extraFlashbackCosts) when it names one of the
// card's further flashback instances, else flashbackCost. A stale Cost no
// longer among the card's instances falls back to flashbackCost.
func (e *Engine) flashbackCostFor(id state.ObjID, opt decision.Option) Cost {
	if opt.Cost != "" && slices.Contains(e.extraFlashbackCosts(id), opt.Cost) {
		return ParseCost(opt.Cost)
	}
	return e.flashbackCost(id)
}

// delveCredit is the most generic mana id's Delve can cover right now for a
// cost whose generic requirement is generic: the smaller of that and p's
// graveyard size. Zero for a card without Delve.
func (e *Engine) delveCredit(p state.PlayerID, id state.ObjID, generic int32) int32 {
	if generic <= 0 || !e.hasKeywordH(id, kwhDelve) {
		return 0
	}
	gy := int32(len(e.G.Zone(state.ZGraveyard, p)))
	if gy > generic {
		gy = generic
	}
	return gy
}

// castable reports whether cost is payable for id if cast by p right now:
// mana payable (Colored+Generic), crediting the generic requirement with
// delved graveyard cards when id has Delve; every Sac part has at least N
// matching permanents on p's battlefield; every Discard part is payable from
// p's hand; every SubCounter part's N does
// not exceed id's own current counters of that kind; and Tap requires id
// (an already-battlefield source -- Task 10 activates from there) to be
// untapped.
//
// The Sac check is a distinct-candidate feasibility check, not N independent
// head-counts against the same board (fix round 1, reviewer Important 1):
// the Sac parts of ONE cost are paid one after another, each consuming its
// chosen permanents, so a cost with TWO Sac parts cannot be paid by the same
// permanent twice. A `Sac<1/Creature> Sac<1/Creature>` cost must therefore
// not be offered with a single creature on the battlefield, and a
// `Sac<1/Creature.Red> Sac<1/Creature.Green> Sac<1/Creature.White>` cost
// cannot count one red-and-green creature towards both the red and the green
// part. Each part's N candidates are reserved (distinct, in zone-walk order)
// as the parts are walked, mirroring exactly what sacAsk offers; a part with
// fewer than N un-reserved candidates makes the whole cost unpayable, so the
// option is never offered (the totality rule an option that cannot be paid
// should never be offered). Reserving the first N matches in zone order is a
// sound test -- it never reports payable when no distinct assignment exists --
// and never illegal: a truly-payable cost where the FIRST N happen to collide
// with a scarcer later part is conservatively withheld (the engine's standing
// rule is that wrongly withholding a legal option is safe, while wrongly
// offering an unpayable one is an illegal game action).
// castable is the ordinary offer gate's price check. The mana half resolves
// through costPayable -- the seat's RESTRICTION-ADJUSTED floating pool
// (manaAvailableFor), exactly the pool payManaFor will charge -- because the
// engine must never offer a cast whose payment would later fail: restricted
// mana (a RestrictValid$ batch, e.g. Eldrazi Temple's "colorless mana that
// can be used only to pay Eldrazi costs") that does not match this payment
// is invisible here, the same way it is invisible to the payment. The
// potential-action walk does NOT go through castable: it prices against an
// explicitly hypothetical pool (castablePriced below), where the over-bound
// direction is deliberate. Every non-mana part -- Sac candidates, Discard
// candidates, SubCounter counts, Tap untappedness -- is checked against the
// REAL state: floating mana never satisfies a sacrifice.
func (e *Engine) castable(p state.PlayerID, id state.ObjID, cost Cost, ability bool) bool {
	mana := cost
	mana.Generic -= e.delveCredit(p, id, mana.Generic)
	if !pay.CostPayable(asPayer(e), p, id, ability, mana) {
		return false
	}
	return pay.NonManaCastable(asPayer(e), p, id, cost, ability, "")
}

// countCandPayable reports whether a repeatable-additional-cost count walk's
// candidate -- the base composed cost plus N payments of the keyword's own
// cost -- is payable right now. The walk runs at CR 601.2b, before the CR
// 601.2f modifiers have been folded into pc.cost, so pricing the RAW
// candidate through castable skipped every RaiseCost/ReduceCost static: a
// Baral ("Instant and sorcery spells you cast cost {1} less") offered one
// fewer replicate payment than the pool could actually pay. This composes
// the candidate through manaToPay -- the SAME CR 601.2f/903.8 composition
// the payment will charge -- then makes the Convoke/Harmonize/Improvise
// reduction the later announcement can still make (convokeCountCredit),
// takes the Delve credit, and runs the ordinary mana gate; the non-mana
// parts go through nonManaCastable exactly as castable's tail does. Because
// the candidate is priced at its real charge, the bound can never offer a
// count the payment cannot settle.
func (e *Engine) countCandPayable(pc *pendingCast, cand Cost) bool {
	mana := e.countComposedCost(pc, cand)
	mana.Generic -= e.delveCredit(pc.player, pc.card, mana.Generic)
	if !pay.CostPayable(asPayer(e), pc.player, pc.card, false, mana) {
		return false
	}
	return pay.NonManaCastable(asPayer(e), pc.player, pc.card, cand, false, tapCostSAKind(e.pcAbility(pc)))
}

// countComposedCost is the CR 601.2f/903.8 composition of a count walk's
// candidate with the Convoke/Harmonize/Improvise reduction the caster can
// still announce folded in. It is manaToPay for the candidate cost (the same
// modifier snapshot, announced-X re-pricing and commander tax the payment
// uses), so the bound and the charge can never disagree about a ReduceCost
// static, and then convokeCountCredit for the announcement the later
// convokeAsk can make against that composed total.
func (e *Engine) countComposedCost(pc *pendingCast, cand Cost) Cost {
	tmp := *pc
	tmp.cost = cand
	m := e.manaToPay(&tmp)
	return e.convokeCountCredit(pc, m)
}

// castablePriced is castable priced against an EXPLICIT pool instead of the
// seat's restriction-adjusted floating one. Its only caller is the
// potential-action walk (rules/legal.go legalActionsPriced's affordable, and
// its own hyp==nil arm routes back to castable): pool is the hypothetical
// bound the seat would hold after floating every untapped source. The pool is
// a pure mana bound -- the walk may price against raw units a RestrictValid$
// provenance would refuse at payment time, because a wrongly WITHHELD pass
// costs one idle stop while a wrongly eaten window loses the player's action
// -- but every non-mana part (Sac candidates, Discard candidates, SubCounter
// counts, Tap untappedness) is still checked against the REAL state: floating
// or hypothetical mana never satisfies a sacrifice.
func (e *Engine) castablePriced(p state.PlayerID, id state.ObjID, cost Cost, ability bool, pool state.Mana) bool {
	mana := cost
	mana.Generic -= e.delveCredit(p, id, mana.Generic)
	if !pay.CostPayablePool(asPayer(e), p, id, ability, mana, pool, e.G.Players[p].ManaUnits()) {
		return false
	}
	return pay.NonManaCastable(asPayer(e), p, id, cost, ability, "")
}
