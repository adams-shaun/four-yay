// Fixtures for a continuous static whose condition or count the turn-1 static
// scenario leaves false or zero (ticket levelb-static-condition-fixtures). A
// "gets +1/+0 as long as you control an artifact", a Threshold or Delirium
// gate, a "+1/+1 for each Forest you control" or an Infusion "as long as you
// gained life this turn" changes nothing on the bare scenario, so the row was
// skipped as unobservable. This file offers candidate fixtures instead: board,
// graveyard and exile cards in setup, and cast preludes for turn history. The
// template tries them in order and serves the first that makes staticObserved
// true; it never evaluates the condition itself, so a fixture that does not
// satisfy the gate simply shows no change and is discarded.
//
// The generic board/graveyard/turn-history fixtures are the phase-trigger
// condition helper's (conditionPreludes); this file adds the static-specific
// ones (a typed IsPresent filter with its zone and count, Threshold,
// EnduringStory, scaled counts) and names the conditions no fixture reaches.
package templates

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// staticFixture is one candidate enrichment of a static scenario.
type staticFixture struct {
	conditionPrelude
	p1Battlefield []string // cards on p1's battlefield (an opponent's planeswalker)
	exile         []string // cards in p0's exile
	// probeCounters puts counters on the probe (staticProbe) in setup, keyed
	// by kind: the affected permanent's counter gate
	// (Creature.YouCtrl+counters_GE1_P1P1, Permanent.YouCtrl+HasCounters) the
	// bare scenario leaves at zero. The probe spec's compared P/T is shifted
	// by the same amount, so only a change the static itself makes is
	// observable.
	probeCounters map[string]int
	// place puts the card on the battlefield in setup instead of casting it:
	// a "you haven't cast a spell this turn" gate that the card's own cast
	// would falsify.
	place bool
	// equip is an Equipment card placed in setup and attached by staticBase
	// to what attach names (staticProbe or "self"), for a static whose
	// affected filter selects equipped permanents. attachPT is the
	// equipment's own constant P/T grant, the compared spec's shift.
	equip    string
	attach   string
	attachPT [2]int32
	// manaExtra is colourless mana the card's own cast is paid with and
	// leaves unspent, for a count of the unspent mana pool.
	manaExtra int
	// tokenName and tokenSpec are the printed name and spec of the token a
	// token fixture casts into being; tokenAttackers is its scenario refs
	// when the static selects attacking tokens.
	tokenName      string
	tokenSpec      staticProbeSpec
	tokenAttackers []string
	// afterSteps are steps staticBase appends after the card is on the
	// battlefield (the solved-Case fixtures' end-step solve sequence: the
	// Case's own "To solve" trigger is a battlefield trigger, so it fires
	// only at the end step of the turn the Case is cast, CR 719.3a).
	afterSteps []oraclegen.Step
	// afterXab is parallel to afterSteps: the XMage rule-text prefix of an
	// afterStep activate (the crew or Craft prelude's activation), "" on
	// every other afterStep.
	afterXab []string
	// reanswers marks a fixture whose afterSteps ask XMage questions the
	// cast path's XAnswers do not cover (an activate step's cost): the
	// serving loop re-derives XAnswers from the full replay and scripts the
	// activationCost's picks at the fixture's own activate step.
	reanswers bool
	// craft places the card by casting its FRONT face (the static under
	// test sits on the back face a Craft activation transforms into).
	craft bool
	// activationCost is the Forge cost of an afterStep activate step whose
	// non-mana cost carries a choice (a Crew tap, a Craft material exile):
	// the serving loop scripts XMage's cost selector from the observed
	// decisions, the same helper the standalone activate template uses.
	activationCost string
}

// setupOnly reports whether the fixture needs no steps, so it also fits the
// land and back-face scenarios, which carry no cast to hang a prelude on.
func (s staticFixture) setupOnly() bool {
	return len(s.steps) == 0 && len(s.afterSteps) == 0 && !s.place
}

// seats adds the fixture's cards to the two seats. Battlefield cards are
// appended as is, not de-duplicated: "seven or more lands" needs seven. The
// probe counters land on BOTH seats' probe: the compared probe spec is keyed
// by card name, so a probe countered on p0 only would make p1's untouched
// probe read as changed against the shifted spec -- a false observation no
// static's grant can explain.
func (s staticFixture) seats(p0, p1 *oraclegen.Seat) {
	p0.Hand = append(p0.Hand, s.hand...)
	p0.Battlefield = append(p0.Battlefield, s.battlefield...)
	if s.equip != "" {
		p0.Battlefield = append(p0.Battlefield, s.equip)
	}
	p0.Graveyard = append(p0.Graveyard, s.graveyard...)
	p0.Exile = append(p0.Exile, s.exile...)
	p1.Battlefield = append(p1.Battlefield, s.p1Battlefield...)
	if s.life > 0 {
		// The fixture's own life total (a signed "-X where X is your life
		// total" static must leave the crewed card a positive, non-printed
		// P/T): p0's starting life, the same setup field the
		// applyActivationPrelude prelude sets.
		life := s.life
		p0.Life = &life
	}
	kinds := make([]string, 0, len(s.probeCounters))
	for kind := range s.probeCounters {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		*p0 = oraclegen.WithCounters(*p0, staticProbe, kind, int32(s.probeCounters[kind]))
		*p1 = oraclegen.WithCounters(*p1, staticProbe, kind, int32(s.probeCounters[kind]))
	}
}

// apply adds the fixture to a cast fixture: cards in the seats, turn-history
// steps before the card's cast.
func (s staticFixture) apply(fx *oraclegen.Fixture) {
	s.seats(fx.P0(), fx.P1())
	fx.AddPrelude(s.steps...)
}

// merge is the fixture holding everything a and b hold.
func (s staticFixture) merge(o staticFixture) staticFixture {
	s.hand = append(append([]string(nil), s.hand...), o.hand...)
	s.battlefield = append(append([]string(nil), s.battlefield...), o.battlefield...)
	s.graveyard = append(append([]string(nil), s.graveyard...), o.graveyard...)
	s.steps = append(append([]oraclegen.Step(nil), s.steps...), o.steps...)
	s.p1Battlefield = append(append([]string(nil), s.p1Battlefield...), o.p1Battlefield...)
	s.exile = append(append([]string(nil), s.exile...), o.exile...)
	s.place = s.place || o.place
	if s.equip == "" {
		s.equip, s.attach, s.attachPT = o.equip, o.attach, o.attachPT
	}
	if s.manaExtra == 0 {
		s.manaExtra = o.manaExtra
	}
	if s.life == 0 {
		s.life = o.life
	}
	if s.tokenName == "" {
		s.tokenName, s.tokenSpec, s.tokenAttackers = o.tokenName, o.tokenSpec, o.tokenAttackers
	}
	if len(s.afterSteps) == 0 {
		s.afterSteps = append(s.afterSteps, o.afterSteps...)
	}
	if len(s.afterXab) == 0 {
		s.afterXab = append(s.afterXab, o.afterXab...)
	}
	if o.reanswers {
		s.reanswers = true
	}
	if o.craft {
		s.craft = true
	}
	if s.activationCost == "" {
		s.activationCost = o.activationCost
	}
	for kind, n := range o.probeCounters {
		if s.probeCounters == nil {
			s.probeCounters = map[string]int{}
		}
		s.probeCounters[kind] += n
	}
	return s
}

// staticProbeCounterFixture is the fixture for a static whose affected
// permanent carries a counter gate the bare scenario leaves at zero:
// Creature.YouCtrl+counters_GE1_P1P1 (Training Regimen's trample lord), or
// HasCounters for a gate on any counter (Innkeeper's Talent level 2's ward).
// The probe holds the counters the gate names, so the static's grant lands on
// it; the compared probe spec is shifted by the same counters, so a static
// that grants nothing still observes nothing. The card's own counter gate is
// counterGatedBase's, not this fixture's, and a gate on the opponent's
// permanents has no p1-side fixture here.
func staticProbeCounterFixture(st *cards.Static, affected string) (staticFixture, bool) {
	if _, _, ok := staticCounterGate(st); ok {
		return staticFixture{}, false
	}
	words := affectedWords(affected)
	if hasWord(words, "OppCtrl") {
		return staticFixture{}, false
	}
	for _, w := range words {
		if !strings.HasPrefix(w, "counters_") && w != "HasCounters" {
			continue
		}
		kind, n := "P1P1", 1
		if m := probeCounterGate.FindStringSubmatch(w); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v > 0 {
				n = v
			}
			kind = strings.ToUpper(m[2])
		}
		return staticFixture{probeCounters: map[string]int{kind: n}}, true
	}
	return staticFixture{}, false
}

// staticFixtureTable maps one word of an IsPresent / Count$Valid filter to the
// cards that stand in for it, in the order a count of n takes them (the list
// cycles when n exceeds it). Subtype words the static probe table already
// names (staticProbeTable) fall through to it. A card is chosen for being inert
// when placed: it must not move the P/T or keywords of the probe or of the card
// under test on its own.
var staticFixtureTable = []struct {
	word  string
	cards []string
}{
	{"Land", []string{"Forest", "Island", "Mountain", "Plains", "Swamp"}},
	{"Forest", []string{"Forest"}},
	{"Island", []string{"Island"}},
	{"Mountain", []string{"Mountain"}},
	{"Desert", []string{"Desert"}},
	{"Artifact", []string{"Sol Ring", "Arcane Signet"}},
	{"Equipment", []string{"Bonesplitter"}},
	{"Permanent", []string{"Llanowar Elves", "Island", "Sol Ring", "Grizzly Bears", "Plains", "Swamp", "Mountain", "Forest"}},
	{"Creature", []string{"Llanowar Elves", "Grizzly Bears"}},
	{"Instant", []string{"Shock"}},
	{"Sorcery", []string{"Divination"}},
	{"Card", []string{"Wastes"}},
	{"Lesson", []string{"Accumulate Wisdom"}},
	{"AdventureCard", []string{"Bonecrusher Giant"}},
	{"Black", []string{"Muck Rats"}},
	{"White", []string{"Savannah Lions"}},
	{"Rat", []string{"Muck Rats"}},
	{"Dwarf", []string{"Dwarven Trader"}},
	{"Faerie", []string{"Cloudseeder"}},
	{"Planeswalker", []string{"Ajani Goldmane"}},
	{"Jace", []string{"Jace Beleren"}},
}

// staticFixtureFor is the cards one filter group asks for: its card kind's
// stand-in, taken i-th. "" when no stand-in is known or the filter names a
// state setup cannot give (a token, a counter, a solved Case).
func staticFixtureFor(group string, i int) string {
	words := affectedWords(group)
	for _, w := range words {
		switch {
		case w == "token", w == "HasCounters", w == "IsSolved", w == "Attached", strings.HasPrefix(w, "named"), strings.HasPrefix(w, "counters_"):
			return ""
		}
	}
	if hasWord(words, "Creature") && hasWord(words, "Land") {
		return "Dryad Arbor"
	}
	if hasWord(words, "nonCreature") && hasWord(words, "nonLand") {
		return []string{"Shock", "Sol Ring", "Divination"}[i%3]
	}
	// The last word naming a stand-in wins: "Planeswalker.Jace" is a Jace,
	// "Permanent.Black" a black permanent.
	pick := ""
	for _, w := range words {
		if card := staticFixtureWord(w, i); card != "" {
			pick = card
		}
	}
	return pick
}

// staticFixtureWord is the i-th stand-in for one filter word: the fixture
// table's, else the static probe table's subtype probe.
func staticFixtureWord(w string, i int) string {
	for _, row := range staticFixtureTable {
		if strings.EqualFold(row.word, w) {
			return row.cards[i%len(row.cards)]
		}
	}
	for _, row := range staticProbeTable {
		if row.word == w {
			return row.card
		}
	}
	return ""
}

func hasWord(words []string, w string) bool {
	for _, x := range words {
		if x == w {
			return true
		}
	}
	return false
}

// staticOpposing reports whether a filter group asks for a permanent an
// opponent controls.
func staticOpposing(group string) bool { return hasWord(affectedWords(group), "OppCtrl") }

var geCount = regexp.MustCompile(`(?i)ge(\d+)|divideevenlydown\.(\d+)`)

// staticCountFrom is the count a compare ("GE4") or divisor ("DivideEvenlyDown.7")
// in text asks for, 1 when it names none.
func staticCountFrom(text string) int {
	m := geCount.FindStringSubmatch(text)
	for i := 1; i < len(m); i++ {
		if n, err := strconv.Atoi(m[i]); err == nil && n > 0 {
			return n
		}
	}
	return 1
}

// staticPresence is the fixture for "n cards matching filter in zone".
func staticPresence(filter, zone string, n int) (staticFixture, bool) {
	var out staticFixture
	groups := strings.Split(filter, ",")
	for i := 0; i < n; i++ {
		g := groups[i%len(groups)]
		card := staticFixtureFor(g, i/len(groups))
		if card == "" {
			return out, false
		}
		switch {
		case strings.EqualFold(zone, "Graveyard"):
			out.graveyard = append(out.graveyard, card)
		case strings.EqualFold(zone, "Exile"):
			out.exile = append(out.exile, card)
		case staticOpposing(g):
			out.p1Battlefield = append(out.p1Battlefield, card)
		default:
			out.battlefield = append(out.battlefield, card)
		}
	}
	return out, true
}

// staticSVarBodies is the lowercased body of every SVar the static reaches:
// each param value naming an SVar, and every SVar those bodies refer to with
// "SVar$Name".
func staticSVarBodies(f *cards.Face, st cards.Static) []string {
	var out []string
	seen := map[string]bool{}
	var visit func(name string)
	visit = func(name string) {
		body, ok := f.SVars[name]
		if !ok || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, body)
		for _, part := range strings.FieldsFunc(body, func(r rune) bool { return r == '/' || r == '$' || r == ' ' }) {
			visit(part)
		}
	}
	for _, key := range []cards.ParamKey{cards.PKAddPower, cards.PKAddToughness, cards.PKCheckSVar} {
		if !st.HasParam(key) {
			continue
		}
		// CheckSVar$ may carry the Count$ expression inline instead of naming
		// an SVar (Omenport Vigilante).
		if v := st.ParamStr(key); strings.HasPrefix(v, "Count$") {
			out = append(out, v)
		} else {
			// A signed pump ("AddPower$ -X", Stingerback Terror, The Last
			// Ride) names its SVar after the sign, which is not part of the
			// name.
			visit(strings.TrimLeft(v, "+-"))
		}
	}
	return out
}

// staticBodyFixture is the fixture a count-bearing SVar body asks for: the
// board or graveyard a "Count$Valid ..." tallies, a life total, a crime.
func staticBodyFixture(reg *cards.Registry, body string, n int) (staticFixture, bool) {
	lower := strings.ToLower(body)
	// filter is the card filter after the body's prefix, up to a "/" or "$"
	// modifier (Times.2, GreatestCardManaCost).
	filter := func(prefix string) string {
		rest := body[len(prefix):]
		if i := strings.IndexAny(rest, "/$"); i >= 0 {
			rest = rest[:i]
		}
		return strings.TrimSpace(rest)
	}
	switch {
	case strings.HasPrefix(lower, "count$validgraveyard "):
		return staticPresence(filter("Count$ValidGraveyard "), "Graveyard", staticCountFrom(body))
	case strings.HasPrefix(lower, "count$validhand "):
		// A count of the cards in hand ("gets -1/-1 for each card in your
		// hand", Stingerback Terror): the fixture holds that many spare cards
		// so the count is nonzero.
		return staticFixture{conditionPrelude: conditionPrelude{hand: oraclegen.Repeat("Wastes", staticCountFrom(body))}}, true
	case strings.HasPrefix(lower, "count$valid "):
		flt := filter("Count$Valid ")
		if fx, ok := staticAttachedCountFixture(reg, flt); ok {
			return fx, true
		}
		return staticPresence(flt, "Battlefield", n)
	case strings.Contains(lower, "hascardsingraveyard"):
		return staticFixture{conditionPrelude: conditionPrelude{graveyard: oraclegen.Repeat("Wastes", staticCountFrom(body))}}, true
	case strings.HasPrefix(lower, "count$yourlifetotal"), strings.HasPrefix(lower, "count$lifeyougainedthisturn"):
		if steps := resolvedCastSteps(reg, "Angel's Mercy"); len(steps) > 0 {
			return staticFixture{conditionPrelude: conditionPrelude{hand: []string{"Angel's Mercy"}, steps: steps}}, true
		}
	case strings.HasPrefix(lower, "playercountpropertyyou$lifelostthisturn"):
		if step, ok := castProbe(reg, "Shock", "p0"); ok {
			return staticFixture{conditionPrelude: conditionPrelude{hand: []string{"Shock"}, steps: []oraclegen.Step{step, {Op: "resolve"}}}}, true
		}
	case strings.HasPrefix(lower, "playercountopponents$lowestlifetotal"):
		// 5 + 3 + 3 damage takes the opponent from 20 to 9. Distinct spell
		// names: a second cast of one name is ambiguous with the first,
		// already in the graveyard.
		var steps []oraclegen.Step
		var hand []string
		for _, spell := range []string{"Lava Axe", "Lightning Strike", "Searing Spear"} {
			step, ok := castProbe(reg, spell, "p1")
			if !ok {
				return staticFixture{}, false
			}
			hand = append(hand, spell)
			steps = append(steps, step, oraclegen.Step{Op: "resolve"})
		}
		return staticFixture{conditionPrelude: conditionPrelude{hand: hand, steps: steps}}, true
	case strings.HasPrefix(lower, "count$thisturncast"):
		// The card's own cast is one of the spells, so n-1 earlier ones. A
		// spell that is not the probe: a hand card named like a battlefield
		// probe would be ambiguous in the cast step's card ref.
		var steps []oraclegen.Step
		var hand []string
		for _, spell := range []string{"Llanowar Elves", "Elvish Mystic", "Fyndhorn Elves"}[:max(1, min(n-1, 3))] {
			step, ok := castProbe(reg, spell)
			if !ok {
				return staticFixture{}, false
			}
			hand = append(hand, spell)
			steps = append(steps, step, oraclegen.Step{Op: "resolve"})
		}
		return staticFixture{conditionPrelude: conditionPrelude{hand: hand, steps: steps}}, true
	case strings.HasPrefix(lower, "count$committedcrimethisturn"):
		if step, ok := castProbe(reg, "Shock", "p1"); ok {
			return staticFixture{conditionPrelude: conditionPrelude{hand: []string{"Shock"}, steps: []oraclegen.Step{step, {Op: "resolve"}}}}, true
		}
	case strings.HasPrefix(lower, "count$manapool"):
		// A count of the unspent mana pool (Ozai, the Phoenix King's "as
		// long as you have six or more unspent mana"): the card's own cast
		// is paid with n colourless mana more than it needs and the pool
		// keeps the rest.
		return staticFixture{manaExtra: n}, true
	}
	return staticFixture{}, false
}

// resolvedCastSteps is castProbe followed by a resolve, nil when the probe
// cannot be cast.
func resolvedCastSteps(reg *cards.Registry, card string) []oraclegen.Step {
	st, ok := castProbe(reg, card)
	if !ok {
		return nil
	}
	return []oraclegen.Step{st, {Op: "resolve"}}
}

// staticFixtures offers the candidate fixtures for one static, most specific
// first: the ones derived from the static's own condition, then the phase
// helper's generic board / graveyard / turn-history ones.
func staticFixtures(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, st cards.Static) []staticFixture {
	var parts []staticFixture
	add := func(s staticFixture, ok bool) {
		if ok {
			parts = append(parts, s)
		}
	}
	switch strings.ToLower(st.ParamStr(cards.PKCondition)) {
	case "threshold":
		add(staticFixture{conditionPrelude: conditionPrelude{graveyard: oraclegen.Repeat("Wastes", 7)}}, true)
	case "enduringstory":
		// CR 702.175a: three artifacts, legendaries and/or Sagas, one of them
		// Storied. The card is the Storied legend; two artifacts make three.
		add(staticFixture{conditionPrelude: conditionPrelude{battlefield: []string{"Sol Ring", "Arcane Signet"}}}, true)
	}
	if present := st.ParamStr(cards.PKIsPresent); present != "" {
		zone := st.ParamStr(cards.PKPresentZone)
		if zone == "" {
			zone = "Battlefield"
		}
		add(staticPresence(present, zone, staticCountFrom(st.ParamStr(cards.PKPresentCompare))))
	}
	n := staticCountFrom(st.ParamStr(cards.PKSVarCompare))
	for _, body := range staticSVarBodies(f, st) {
		add(staticBodyFixture(reg, body, n))
	}
	if strings.Contains(strings.ToLower(strings.Join(staticSVarBodies(f, st), " ")), "thisturncast") && strings.HasPrefix(strings.ToUpper(st.ParamStr(cards.PKSVarCompare)), "LT") {
		add(staticFixture{place: true}, true)
	}
	var out []staticFixture
	if len(parts) > 1 {
		all := parts[0]
		for _, p := range parts[1:] {
			all = all.merge(p)
		}
		out = append(out, all)
	}
	out = append(out, parts...)
	for _, c := range conditionPreludes(reg, st.Params, f.SVars) {
		if len(c.tapped) > 0 || len(c.counters) > 0 {
			continue
		}
		out = append(out, staticFixture{conditionPrelude: c})
	}
	// The solved-Case fixtures (a static gated on its own source Case being
	// solved) run the solve condition's own events as prelude steps and the
	// end-step solve sequence after the cast; they come after the
	// condition-derived ones, so a row an existing candidate already serves
	// keeps its scenario bytes.
	out = append(out, staticSolvedCaseFixtures(reg, f, st)...)
	// The state fixtures (a token prelude, an attached Equipment, unspent
	// pool mana, a raid-count attack) run after every existing candidate, so
	// a row an existing candidate already serves keeps its scenario bytes.
	out = append(out, staticStateFixtures(reg, f, st)...)
	// The activate preludes (a crew that makes a Vehicle's P/T print, a
	// Craft that populates the exile set a back-face CDA counts) run last,
	// after every existing candidate: they are the only fixtures whose
	// afterSteps ask XMage questions, so a row an existing candidate serves
	// keeps its scenario bytes.
	if fx, ok := staticCrewFixture(reg, f, name, st); ok {
		out = append(out, fx)
	}
	if fx, ok := staticCraftFixture(reg, c, f, name, st); ok {
		out = append(out, fx)
	}
	return staticAvoidProbeCollision(reg, out)
}

// staticConditionGap names why a static no fixture made observable, when the
// cause is a known shape rather than the generic "nothing changed". "" is the
// generic reason.
func staticConditionGap(f *cards.Face, st cards.Static) string {
	params := make(map[string]string, len(st.Params))
	for k, v := range st.Params {
		if k != "Description" && k != "KeywordLine" {
			params[k] = v
		}
	}
	text := strings.ToLower(strings.Join(append(conditionText(params, nil), staticSVarBodies(f, st)...), " "))
	has := func(xs ...string) bool {
		for _, x := range xs {
			if strings.Contains(text, strings.ToLower(x)) {
				return true
			}
		}
		return false
	}
	switch {
	case has("setmaxhandsize"):
		return "hand size is not observable in the permanent snapshot"
	case has("adjustlandplays"):
		return "changes a player rule (hand size, land plays), not a permanent"
	case has("token"):
		return "needs a token (setup places none)"
	case has("exiledwith", "validexile"):
		return "counts cards exiled with the source"
	case has("unlockeddoors"):
		return "counts unlocked Room doors"
	case has("manapool"):
		return "needs unspent mana in the pool"
	case has("issolved"):
		return "needs a solved Case"
	case has("equipment.attached"):
		return "needs Equipment attached to the card"
	case has("attackersdeclared"):
		return "counts attackers declared this turn"
	case has("maxspeed"):
		return "needs max speed (setup has no speed knob)"
	}
	return ""
}

// staticClassReason is the skip for a static a Class level gates: the
// level-up is an activated ability the scenario driver has no op for.
const staticClassReason = "needs a class level (the driver has no level-up op)"
