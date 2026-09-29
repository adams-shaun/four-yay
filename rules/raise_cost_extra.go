package rules

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A RaiseCost static's Cost$ names the WHOLE additional cost it adds (Forge
// CostAdjustment's RaiseCost branch). raiseFromCost prices the plain
// mana/life shape; everything else is an ADDITIONAL non-mana cost (Brutal
// Suppression's "Sacrifice a land", Grafted Identity's creature sacrifice,
// Carth the Lion's extra [+1], the Champion cycle's BeholdExile, Tectonic
// Split's "sacrifice half your lands"), and it is carried in costMods.extra
// so the offer gate (composedOfferCost -> nonManaCastable), the cast-flow
// stages (every fold site moves it into the pending cost) and the settle
// all price the SAME parts. Before this bridge only Blight<X> was carried
// and every other non-mana Cost$ fell through to the Amount$ fallback --
// absent, so evaluated to zero -- and the additional cost was never charged.
//
// A Cost$ part the payment machinery cannot pay is never silently dropped:
// it is recorded in Cost.Withheld, which nonManaCastable refuses, so the
// spell or ability is withheld at the offer gate and a stale option that
// reaches a fold site is declined with a replay-visible Note.

// raiseNamedCount matches a cost token whose first field is a NAME rather
// than a literal count: Sac<X/Land/...>, Discard<X/Creature/...>,
// ExileFromHand<Y/Card.Red/...>, PayLife<X>, tapXType<Tapped/Creature>,
// Waterbend<X>. The name resolves through the source face's SVar table.
var raiseNamedCount = regexp.MustCompile(`^([A-Za-z]+)<([A-Za-z][A-Za-z0-9]*)((?:/[^>]*)?)>$`)

// raiseLiteralHeads are first-field words that are part of a head's own
// grammar, never an SVar name (tapXType<Any/...>'s free count).
var raiseLiteralHeads = map[string]bool{"Any": true}

// raiseAnnounce is a named count the CASTER announces (Forge's Announce$
// on the spell ability, with the SVar's Number$0 as its placeholder): the
// March cycle's "exile any number of red cards" (ExileFromHand<Y> with
// SVar:Y:SVar$Exiled and Announce$ Exiled) and Explosive Singularity's
// "tap any number of untapped creatures" (tapXType<Tapped/Creature> with
// Announce$ Tapped). The part's count is that announcement, which the
// cast flow asks as its own CR 601.2b choice (namedAnnounceAsk).
func raiseAnnounceName(name string, svars map[string]string, announces []string) (string, bool) {
	cur := name
	for depth := 0; depth < 6; depth++ {
		for _, a := range announces {
			if strings.EqualFold(a, cur) {
				return a, true
			}
		}
		body, ok := svars[cur]
		if !ok {
			return "", false
		}
		next, ok := strings.CutPrefix(strings.TrimSpace(body), "SVar$")
		if !ok {
			return "", false
		}
		cur = strings.TrimSpace(next)
	}
	return "", false
}

// resolveRaiseCostText rewrites every named count in a RaiseCost Cost$ into
// the form ParseCost prices, reading the source face's SVar table:
//
//   - a body of Count$xPaid (Aether Tide's Discard<X>, the Waterbend<X>
//     cards) is the cast's own announced X, spelled "X";
//   - a name bound to an Announce$ on the face's spell ability (the March
//     cycle, Explosive Singularity) is a named announcement, spelled
//     "@<Name>" so the caller can bind the part to it;
//   - any other body is FIXED at the composition point and evaluated
//     (Tectonic Split's Count$Valid Land.YouCtrl/HalfUp, Liesa's
//     Count$CommanderCastFromCommandZone/Times.2), spelled as its literal.
//
// A name with no resolvable reading is returned in withheld: the cost is
// unpayable as written, never silently free.
func resolveRaiseCostText(raw string, svars map[string]string, announces []string, eval func(body string) (int32, bool)) (string, []string) {
	var out []string
	var withheld []string
	for toks := (costTokenIter{s: strings.TrimSpace(raw)}); ; {
		sym, more := toks.next()
		if !more {
			break
		}
		m := raiseNamedCount.FindStringSubmatch(sym)
		if m == nil || raiseLiteralHeads[m[2]] {
			out = append(out, sym)
			continue
		}
		head, name, rest := m[1], m[2], m[3]
		if a, ok := raiseAnnounceName(name, svars, announces); ok {
			out = append(out, head+"<@"+a+rest+">")
			continue
		}
		body, ok := svars[name]
		switch {
		case !ok && name == "X":
			out = append(out, sym)
		case !ok:
			withheld = append(withheld, head+"<"+name+">")
		case strings.EqualFold(strings.TrimSpace(body), "Count$xPaid"):
			out = append(out, head+"<X"+rest+">")
		default:
			n, resolved := eval(body)
			if !resolved || n < 0 {
				withheld = append(withheld, head+"<"+name+">")
				continue
			}
			out = append(out, head+"<"+strconv.Itoa(int(n))+rest+">")
		}
	}
	return strings.Join(out, " "), withheld
}

// raiseExtraCost is the evaluated reading of one RaiseCost static's Cost$
// that raiseFromCost does not price as plain mana/life.
type raiseExtraCost struct {
	col       state.Mana
	gen, life int32
	// waterbend is the generic a Waterbend<N> part lets tapped artifacts and
	// creatures pay (N is also in gen); x counts Waterbend<X> parts, whose
	// announced {X} the taps may pay too.
	waterbend int32
	x         int32
	// extra carries the non-mana additional parts (and any Withheld heads).
	extra Cost
}

// waterbendCost matches the Waterbend<N> / Waterbend<X> additional cost.
var waterbendCost = regexp.MustCompile(`^Waterbend<(X|\d+)>$`)

// discardXCost matches Discard<X/Spec[/desc]> with the announced X.
var discardXCost = regexp.MustCompile(`^Discard<X/([^/>]+)(?:/([^>]*))?>$`)

// raiseAnnouncePart matches a resolved "@<Name>" named-announce count.
var raiseAnnouncePart = regexp.MustCompile(`^([A-Za-z]+)<@([A-Za-z][A-Za-z0-9]*)((?:/[^>]*)?)>$`)

// parseRaiseExtra prices a resolved RaiseCost Cost$ text. Mana and life ride
// the plain raise fields; every non-mana part ParseCost models rides extra;
// a part the payment machinery cannot pay is recorded in extra.Withheld.
func parseRaiseExtra(text string, withheld []string) raiseExtraCost {
	var r raiseExtraCost
	var plain []string
	for toks := (costTokenIter{s: text}); ; {
		sym, more := toks.next()
		if !more {
			break
		}
		// Waterbend<N> / Waterbend<X> (the keyword action "waterbend {N}":
		// pay {N}; while paying it, each untapped artifact or creature the
		// payer taps pays for {1}). The {N} is an ordinary generic raise and
		// the tap help rides the cast's contribution announcement
		// (convokeAsk's waterbend_generic options), capped at the waterbend
		// amount. This bridge is the RaiseCost reader; ParseCost models the
		// same head for the shapes this one never sees (an ability's own
		// Cost$, a self-spell OptionalCost), folding the {N} into Generic and
		// annotating it with Cost.Waterbend/WaterbendX.
		if m := waterbendCost.FindStringSubmatch(sym); m != nil {
			if m[1] == "X" {
				r.x++
				continue
			}
			n, err := strconv.ParseInt(m[1], 10, 32)
			if err != nil || n < 0 {
				withheld = append(withheld, "Waterbend")
				continue
			}
			r.gen = addClampedGeneric(r.gen, n)
			r.waterbend = addClampedGeneric(r.waterbend, n)
			continue
		}
		// Discard<X/Spec> whose X is the cast's own announced X (Aether
		// Tide's "discard X creature cards", SVar:X:Count$xPaid): an
		// Announced discard part, settled by discardAsk at the announced X
		// and capped by xAsk at the matching cards in hand. Only this bridge
		// produces the announced form; ParseCost keeps its reported fallback
		// for the Discard<X> ACTIVATION costs whose X is not the cast's.
		if m := discardXCost.FindStringSubmatch(sym); m != nil {
			r.extra = r.extra.Plus(Cost{Discard: []CostPart{{Spec: strings.ReplaceAll(m[1], ";", ","), Announced: true, Desc: m[2]}}})
			continue
		}
		if m := raiseAnnouncePart.FindStringSubmatch(sym); m != nil {
			part, ok := namedAnnouncePart(m[1], m[2], m[3])
			if !ok {
				withheld = append(withheld, m[1])
				continue
			}
			r.extra = r.extra.Plus(part)
			continue
		}
		plain = append(plain, sym)
	}
	c := ParseCost(strings.Join(plain, " "))
	withheld = append(withheld, c.Unknown...)
	if c.X != 0 || c.XMin != 0 || c.Snow != 0 || len(c.Hybrid) > 0 || len(c.Phyrexian) > 0 ||
		len(c.Twobrid) > 0 || len(c.HybridPhyrexian) > 0 || c.Tap || c.Untap {
		// No corpus RaiseCost Cost$ mixes an unannounced {X}, a flexible pip
		// or the source's own {T}/{Q} into an additional cost; none of them
		// has a meaning this bridge can price, so the static withholds.
		withheld = append(withheld, "mana")
	}
	r.col = c.Colored
	r.gen = addClampedGeneric(r.gen, int64(c.Generic))
	r.life = c.Life
	c.Colored, c.Generic, c.Life = state.Mana{}, 0, 0
	c.Unknown = nil
	r.extra = r.extra.Plus(c)
	if len(withheld) > 0 {
		return raiseExtraCost{extra: Cost{Withheld: withheld}}
	}
	return r
}

// namedAnnouncePart builds the cost part whose count is a named announcement
// (see raiseAnnounceName). Only the heads the cast flow can settle against
// an announced count are modelled: ExileFromHand (exAsk) and tapXType
// (tapPermanentCostAsk). The part carries the announcement's name in Dyn.
func namedAnnouncePart(head, name, rest string) (Cost, bool) {
	fields := strings.SplitN(strings.TrimPrefix(rest, "/"), "/", 2)
	spec := strings.ReplaceAll(strings.TrimSpace(fields[0]), ";", ",")
	if spec == "" {
		return Cost{}, false
	}
	desc := ""
	if len(fields) > 1 {
		desc = fields[1]
	}
	switch head {
	case "ExileFromHand":
		return Cost{Exile: []CostPart{{Spec: spec, Dyn: "@" + name, Desc: desc}}}, true
	case "tapXType":
		return Cost{TapPermanent: []CostPart{{Spec: spec, Dyn: "@" + name, Desc: desc}}}, true
	}
	return Cost{}, false
}

// forEachShardCount reads ForEachShard$ <Colour> (Drought: "an additional
// 'Sacrifice a Swamp' ... for each black mana symbol in their mana costs"):
// the number of mana symbols of that colour in the priced object's mana cost
// (a spell's printed mana cost, an ability's activation cost). A hybrid or
// Phyrexian symbol containing the colour counts once, as Forge's
// ManaCostShard colour test does. ok=false is an unrecognised colour word.
func (e *Engine) forEachShardCount(word string, id state.ObjID, scope costScope) (int, bool) {
	var letter byte
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "white":
		letter = 'W'
	case "blue":
		letter = 'U'
	case "black":
		letter = 'B'
	case "red":
		letter = 'R'
	case "green":
		letter = 'G'
	default:
		return 0, false
	}
	var c Cost
	if scope.kind == "Ability" {
		ab := scope.ab
		if ab == nil {
			return 0, true
		}
		c = ParseCost(ab.Params["Cost"])
	} else {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			return 0, true
		}
		c = ParseCost(o.Face().ManaCost)
	}
	return costShardsOf(c, letter), true
}

// costShardsOf counts the mana symbols of c that contain the colour letter.
func costShardsOf(c Cost, letter byte) int {
	n := int(c.Colored[state.ManaIndex(letter)])
	for _, h := range c.Hybrid {
		if h.A == letter || h.B == letter {
			n++
		}
	}
	for _, p := range c.Phyrexian {
		if p == letter {
			n++
		}
	}
	for _, t := range c.Twobrid {
		if t.Col == letter {
			n++
		}
	}
	for _, hp := range c.HybridPhyrexian {
		if hp.A == letter || hp.B == letter {
			n++
		}
	}
	return n
}

// composeRaiseCost folds one RaiseCost static's Cost$ into mods, once per
// unit of amount when the static pairs it with an Amount$ (the caller's
// evaluated read). It reports false when the static carries no Cost$,
// leaving the caller's Amount$-as-generic raise in charge.
func (e *Engine) composeRaiseCost(mods *costMods, sv staticView, id state.ObjID, scope costScope, x int32, targets []state.Target, amount int32) bool {
	raw, hasCost := sv.Params["Cost"]
	if !hasCost {
		return false
	}
	times := 1
	if _, hasAmt := sv.Params["Amount"]; hasAmt {
		// Cost$ with Amount$ is Forge's "that cost, Amount$ times"
		// (CostAdjustment's RaiseCost branch adds the Cost$ once per unit):
		// Officious Interrogation's {W}{U} per target beyond the first, the
		// "pay {1}{G} any number of times" family (Taste of Paradise,
		// Primitive Justice) at its announced count. The amount is the
		// caller's modAmountX read, never priced as generic mana.
		times = int(amount)
	}
	if word, ok := sv.Params["ForEachShard"]; ok {
		n, known := e.forEachShardCount(word, id, scope)
		if !known {
			mods.extra = mods.extra.Plus(Cost{Withheld: []string{"ForEachShard"}})
			mods.hasExtra = true
			return true
		}
		times *= n
	}
	if times <= 0 {
		return true
	}
	if rc, rg, rl, ok := raiseFromCost(raw); ok {
		addPlainRaise(mods, rc, rg, rl, times)
		return true
	}
	r := e.raiseExtraFor(sv, raw, x, targets)
	addPlainRaise(mods, r.col, r.gen, r.life, times)
	for range times {
		mods.waterbend = addClampedGeneric(mods.waterbend, int64(r.waterbend))
		mods.raiseX = addClampedGeneric(mods.raiseX, int64(r.x))
	}
	if r.x > 0 {
		mods.waterbendX = true
	}
	if r.extra.isZeroExtra() {
		return true
	}
	for range times {
		mods.extra = mods.extra.Plus(r.extra)
	}
	mods.hasExtra = true
	return true
}

func addPlainRaise(mods *costMods, col state.Mana, gen, life int32, times int) {
	for range times {
		for i := range col {
			mods.raiseCol[i] = addClampedGeneric(mods.raiseCol[i], int64(col[i]))
		}
		mods.raiseGen = addClampedGeneric(mods.raiseGen, int64(gen))
		mods.raiseLife = addClampedGeneric(mods.raiseLife, int64(life))
	}
}

// raiseExtraFor resolves and prices a non-plain RaiseCost Cost$ against the
// static's source: its SVar table (the static's own, else the source face's)
// and its spell ability's Announce$ names.
func (e *Engine) raiseExtraFor(sv staticView, raw string, x int32, targets []state.Target) raiseExtraCost {
	svars := sv.SVars
	var announces []string
	o := e.G.Obj(sv.Source)
	if o != nil && o.Face() != nil {
		if svars == nil {
			svars = o.Face().SVars
		}
		announces = faceAnnounces(o.Face().SpellAbility())
	}
	eval := func(body string) (int32, bool) {
		if o == nil || o.Face() == nil {
			return 0, false
		}
		ctx := &effects.Ctx{Source: sv.Source, Controller: sv.Controller, SVars: svars, X: x,
			ChosenNumber: sv.ChosenNumber, ChosenNumberBound: sv.chosenNumberBound, Targets: targets}
		return effects.EvalCountOK(e, ctx, body)
	}
	return raiseExtraResolve(raw, svars, announces, eval)
}

// raiseExtraResolve is raiseExtraFor's pure core: resolve the named counts
// against svars/announces, then price the resolved text. The cost-static
// census classifies a face's RaiseCost statics through this same function.
func raiseExtraResolve(raw string, svars map[string]string, announces []string, eval func(body string) (int32, bool)) raiseExtraCost {
	text, withheld := resolveRaiseCostText(raw, svars, announces, eval)
	return parseRaiseExtra(text, withheld)
}

// isZeroExtra reports whether c carries nothing a fold would add.
func (c Cost) isZeroExtra() bool {
	return paymentPlanCostDetail(c) == "" && len(c.Withheld) == 0
}

// faceAnnounces lists a spell ability's Announce$ names other than X (the
// ordinary announced X is spelled "X" in every cost token that reads it).
func faceAnnounces(sa *cards.SA) []string {
	if sa == nil {
		return nil
	}
	var out []string
	for name := range strings.SplitSeq(sa.Params["Announce"], ",") {
		if name = strings.TrimSpace(name); name != "" && name != "X" {
			out = append(out, name)
		}
	}
	return out
}

// plusRaiseExtra composes a cost with a RaiseCost static's additional parts
// (costMods.extra). It is Cost.Plus -- the extra carries no mana by
// construction (parseRaiseExtra moves mana and life to the plain raise
// fields) -- followed by the loyalty netting Carth the Lion needs: "loyalty
// abilities cost an additional [+1]" makes a [+1] cost [+2] and a [-2] cost
// [-1] (the card's own ruling), so the source-anchored literal LOYALTY
// add/remove parts merge into ONE net part. The payability gate (a [-N] needs
// N counters) and the settle then read the net cost, exactly as Forge's
// Cost.add merges a CostPutCounter into a CostRemoveCounter of the same type.
func plusRaiseExtra(c, extra Cost) Cost {
	c = c.Plus(extra)
	for _, part := range extra.AddCounter {
		if strings.EqualFold(part.Spec, "LOYALTY") {
			return netLoyaltyParts(c)
		}
	}
	return c
}

// netLoyaltyParts merges every literal, source-anchored LOYALTY AddCounter
// and SubCounter part of c into one net part. An announced or
// target-bearing part (a [-X] ability) is left as written: its count is
// not known until the announcement.
func netLoyaltyParts(c Cost) Cost {
	literal := func(p CostPart) bool {
		return strings.EqualFold(p.Spec, "LOYALTY") && !p.Announced && p.Dyn == "" && p.Target == ""
	}
	var net int64
	var add, sub []CostPart
	merged := 0
	for _, p := range c.AddCounter {
		if literal(p) {
			net += int64(p.N)
			merged++
			continue
		}
		add = append(add, p)
	}
	for _, p := range c.SubCounter {
		if literal(p) {
			net -= int64(p.N)
			merged++
			continue
		}
		sub = append(sub, p)
	}
	if merged < 2 {
		return c
	}
	if net >= 0 {
		add = append(add, CostPart{N: int32(net), Spec: "LOYALTY"})
	} else {
		sub = append(sub, CostPart{N: int32(-net), Spec: "LOYALTY"})
	}
	c.AddCounter, c.SubCounter = add, sub
	return c
}

// foldRaiseExtra moves a RaiseCost static's additional parts (mods.extra)
// into the pending cast's own cost, which every non-mana cast-flow stage
// reads (sacAsk, exAsk, beholdCostAsk, the settle). It reports false when
// the extra carries a Withheld part: the offer gate refused that cost, so a
// stale option reaching a fold site is declined with a replay-visible Note
// rather than begun and short-changed.
func (e *Engine) foldRaiseExtra(p state.PlayerID, id state.ObjID, cost Cost, mods *costMods) (Cost, bool) {
	// A Waterbend<N>/<X> part carried by the cost itself (an ABILITY's own
	// Cost$ like Giant Koi's, or a spell's optional-cost part) is the same
	// credit a RaiseCost Waterbend<...> contributes: mods.waterbend caps how
	// much of the cost's generic the taps may cover (CR 701.67a), and mods.
	// waterbendX marks the announced-X amount open. Move it into mods and
	// clear the annotation once, so convokeAsk, xAsk and
	// validateCastContributions all read the one cap. The {N} itself is
	// already in cost.Generic (ParseCost folded it there), so nothing is
	// re-charged here.
	mods.waterbend = addClampedGeneric(mods.waterbend, int64(cost.Waterbend))
	if cost.WaterbendX {
		mods.waterbendX = true
	}
	cost.Waterbend, cost.WaterbendX = 0, false
	// A Waterbend<X> raise adds an {X} the cast announces (xAsk reads
	// pc.cost.X); costMods.apply never prices raiseX, so it is folded here
	// exactly once.
	cost.X += int(mods.raiseX)
	if !mods.hasExtra {
		return cost, true
	}
	if len(mods.extra.Withheld) > 0 {
		e.emit(events.Event{Kind: events.Note, Player: p, Obj: id,
			Text: "additional cost cannot be paid (" + strings.Join(mods.extra.Withheld, ", ") + "); declined"})
		return cost, false
	}
	cost = plusRaiseExtra(cost, mods.extra)
	mods.extra = Cost{}
	return cost, true
}

// waterbendTaps counts the announced contributions that pay a waterbend cost.
func waterbendTaps(pays []convokePayment) int32 {
	var n int32
	for _, pay := range pays {
		if pay.waterbend {
			n++
		}
	}
	return n
}

// withWaterbendOfferCredit is the offer gate's credit for a RaiseCost
// Waterbend<N> additional cost: each untapped artifact or creature the
// payer controls may pay {1} of the N (convokeAsk's waterbend_generic
// announcement), so a cast affordable only with that help is offered. The
// credit is CONSERVATIVE -- a permanent that is itself an untapped mana
// source is not counted, since the offer's mana walk may already be
// spending it -- so the gate can only withhold a legal cast, never offer
// one the payment cannot settle. A Waterbend<X> part prices at X = 0 here
// and needs no credit.
func (e *Engine) withWaterbendOfferCredit(p state.PlayerID, id state.ObjID, mods costMods) costMods {
	if mods.waterbend <= 0 {
		return mods
	}
	var credit int32
	for _, oid := range e.G.Zone(state.ZBattlefield, p) {
		if credit >= mods.waterbend {
			break
		}
		o := e.G.Obj(oid)
		if oid == id || o == nil || o.Tapped || o.Face() == nil || o.BestowedAttached() || o.ReconfiguredAttached() ||
			!(o.EffectiveIsArtifact() || o.EffectiveIsCreature()) || e.untappedManaSource(p, oid) {
			continue
		}
		credit++
	}
	if credit > 0 {
		mods.reduces = append(append([]costMod(nil), mods.reduces...), costMod{generic: credit})
	}
	return mods
}

// isNamedCountPart reports whether a cost part's count is a named
// announcement (namedAnnouncePart's Dyn "@<Name>").
func isNamedCountPart(part CostPart) bool {
	return strings.HasPrefix(part.Dyn, "@")
}

// costHasNamedCount reports whether c carries any named-announcement part.
func costHasNamedCount(c Cost) bool {
	for _, part := range c.Exile {
		if isNamedCountPart(part) {
			return true
		}
	}
	for _, part := range c.TapPermanent {
		if isNamedCountPart(part) {
			return true
		}
	}
	return false
}

// namedAnnounceAsk poses the named announcement a RaiseCost part counts by
// (CR 601.2b: the caster announces how many before paying): 0 up to the
// number of objects the part could pay with. It runs once, before every
// stage that pays or prices the part. With nothing to pay with, the count
// is 0 and nothing is asked.
func (e *Engine) namedAnnounceAsk() bool {
	pc := e.cast
	if pc == nil || pc.namedDone {
		return false
	}
	pc.namedDone = true
	name := ""
	max := -1
	for _, part := range pc.cost.Exile {
		if !isNamedCountPart(part) {
			continue
		}
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		n := len(e.costCandidates(pc.player, pc.card, zone, part.Spec, !pc.isAbility(), false))
		if max < 0 || n < max {
			max = n
		}
		name = part.Dyn[1:]
	}
	for _, part := range pc.cost.TapPermanent {
		if !isNamedCountPart(part) {
			continue
		}
		n := 0
		for _, oid := range e.costCandidates(pc.player, pc.card, state.ZBattlefield, part.Spec, false, true) {
			if pc.cost.Tap && oid == pc.card {
				continue
			}
			n++
		}
		if max < 0 || n < max {
			max = n
		}
		name = part.Dyn[1:]
	}
	if name == "" {
		return false
	}
	pc.named = name
	pc.namedN = 0
	if max <= 0 {
		return false
	}
	title := "Announce " + name
	if sa := e.pcSpellAbility(pc); sa != nil {
		if t := strings.TrimSpace(sa.Params["AnnounceTitle"]); t != "" {
			title = "Choose " + t
		}
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: title, Source: pc.card}
	for n := 0; n <= max; n++ {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "named_announce",
			Label: name + " = " + strconv.Itoa(n), Amount: n})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// pcSpellAbility is the spell ability of the card a pending cast casts (nil
// for an ability activation).
func (e *Engine) pcSpellAbility(pc *pendingCast) *cards.SA {
	if pc.isAbility() {
		return nil
	}
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return nil
	}
	return o.Face().SpellAbility()
}

// namedAnnounceSVars binds a pending cast's named announcement into the
// SVar table a cost static on that same card evaluates its Amount$ with:
// the announced name reads "Number$<n>" (Forge writes the announced value
// into the SVar the same way), so the March cycle's Relative$ ReduceCost
// Amount$ Z (SVar$Y/Times.2 over SVar$Exiled) and Explosive Singularity's
// Amount$ Tapped read the count the caster announced. Any other source, or
// no announcement yet, returns svars unchanged (the offer gate's 0).
func (e *Engine) namedAnnounceSVars(source state.ObjID, svars map[string]string) map[string]string {
	pc := e.cast
	if pc == nil || pc.card != source || pc.named == "" || !pc.namedDone {
		return svars
	}
	out := make(map[string]string, len(svars)+1)
	for k, v := range svars {
		out[k] = v
	}
	out[pc.named] = "Number$" + strconv.Itoa(int(pc.namedN))
	return out
}

// namedAnnounceBound reports whether a pending cast of id has announced a
// named count, which binds a Relative$ ReduceCost's amount exactly as an
// announced X does.
func (e *Engine) namedAnnounceBound(id state.ObjID) bool {
	pc := e.cast
	return pc != nil && pc.card == id && pc.named != "" && pc.namedDone
}

// offerNamedMods is the offer gate's sweep over a named announcement's legal
// counts (1..the objects the part could pay with): the paired Relative$
// ReduceCost reads the announced count, so the pre-announcement snapshot
// (count 0) prices the cast too high. Each candidate count re-composes the
// modifiers with the count bound into the source's SVar table (the same
// binding namedAnnounceSVars gives the payment) and accepts the first that
// is payable. A tap part counts only permanents that are not themselves
// untapped mana sources, so the sweep never spends one permanent twice.
func (e *Engine) offerNamedMods(p state.PlayerID, id state.ObjID, ability bool, base Cost, mods costMods, statics costStaticViews, scope costScope, tax, delve int32, hyp *state.Mana) (costMods, bool) {
	if !costHasNamedCount(mods.extra) {
		return costMods{}, false
	}
	name := ""
	max := -1
	for _, part := range mods.extra.Exile {
		if !isNamedCountPart(part) {
			continue
		}
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		n := len(e.costCandidates(p, id, zone, part.Spec, !ability, false))
		if max < 0 || n < max {
			max = n
		}
		name = part.Dyn[1:]
	}
	for _, part := range mods.extra.TapPermanent {
		if !isNamedCountPart(part) {
			continue
		}
		n := 0
		for _, oid := range e.costCandidates(p, id, state.ZBattlefield, part.Spec, false, true) {
			if !e.untappedManaSource(p, oid) {
				n++
			}
		}
		if max < 0 || n < max {
			max = n
		}
		name = part.Dyn[1:]
	}
	for n := 1; n <= max; n++ {
		bound := statics
		bound.reduce = make([]staticView, len(statics.reduce))
		for i, sv := range statics.reduce {
			if sv.Source == id {
				svars := sv.SVars
				if svars == nil {
					if o := e.G.Obj(id); o != nil && o.Face() != nil {
						svars = o.Face().SVars
					}
				}
				out := make(map[string]string, len(svars)+1)
				for k, v := range svars {
					out[k] = v
				}
				out[name] = "Number$" + strconv.Itoa(n)
				sv.SVars = out
			}
			bound.reduce[i] = sv
		}
		m := e.costModifiersWithTargetsXUsing(bound, p, id, scope, nil, false, 0)
		if e.manaFeasiblePriced(p, id, ability, base, m, tax, delve, hyp) {
			return m, true
		}
	}
	return costMods{}, false
}
