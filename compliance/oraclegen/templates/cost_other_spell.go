package templates

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// costCandidateLimit bounds how many probe spells (or abilities) are replayed
// through gorge for one static. The candidates are ranked simplest first, so a
// handful is enough; a static none of them satisfies is a named skip.
const costCandidateLimit = 12

// costActivation turns a costProbe into an activate step: the probe is a
// permanent on the battlefield and the reduced price pays one of its abilities.
type costActivation struct {
	index   int
	prefix  string
	targets []string
}

// otherCostProbes derives probe candidates for a static that makes OTHER
// spells, or activated abilities, cheaper. The candidates are ranked simplest
// first and the caller keeps the first one gorge can pay for at the reduced
// price. A non-empty reason is a narrower named skip: the shape needs a
// fixture this generator cannot establish.
func otherCostProbes(reg *cards.Registry, source *cards.Face, name string, idx int) ([]costProbe, string) {
	st := source.Statics[idx]
	if st.ModeKind() != cards.StaticReduceCost || strings.EqualFold(st.Params["ValidCard"], "Card.Self") {
		return nil, ""
	}
	ability := strings.EqualFold(st.Params["Type"], "Ability") || strings.HasPrefix(st.Params["ValidSpell"], "Activated.") || strings.HasPrefix(st.Params["ValidSpell"], "Static.")
	if !ability && !strings.EqualFold(st.Params["Type"], "Spell") {
		return nil, ""
	}
	filter := st.Params["ValidCard"]
	if filter == "" && !ability {
		filter = strings.ReplaceAll(st.Params["ValidSpell"], "Spell.", "")
	}
	if filter == "" {
		filter = "Card"
	}
	if strings.Contains(filter, "token") {
		return nil, "token fixture unavailable"
	}
	base := costProbe{battlefield: []string{name}}
	if band := classStaticBand(&st); band >= 2 {
		// The cost static is live only from the Class's level on, so the
		// probe first raises the Class through its own level-up activators.
		// A face that cannot build the prelude keeps the named skip.
		steps, xab, ok := classLevelPrelude(source, name, band)
		if !ok {
			return nil, "class-level prelude unsupported"
		}
		base.pre = append(base.pre, steps...)
		base.preXAbility = append(base.preXAbility, xab...)
	}
	if strings.Contains(filter, "wasCast") || strings.Contains(filter, "AdventureCard") {
		return nil, "cast-provenance probe unsupported"
	}
	if !spellFilterSupported(filter) {
		return nil, "ValidCard filter unsupported"
	}
	base.mustReplay = true
	// Melek's characteristic-defining ability sets its toughness to twice
	// the number of instant and sorcery cards in its controller's graveyard.
	// Keep the reducer alive during setup and the probe cast; the cost static
	// itself remains the only mechanic being measured.
	if name == "Melek, Reforged Researcher" {
		base.graveyard = appendUnique(base.graveyard, "Opt")
	}
	if reason := costStaticConditions(reg, st, &base); reason != "" {
		return nil, reason
	}
	reduction, reason := costStaticReduction(source, st, &base)
	if reason != "" {
		return nil, reason
	}
	if ability {
		return abilityCostProbes(reg, name, st, filter, reduction, base)
	}
	return spellCostProbes(reg, name, st, filter, reduction, base), ""
}

// costStaticConditions establishes the prerequisites a static's own condition
// needs from scenario state, or names the shape it cannot establish.
func costStaticConditions(reg *cards.Registry, st cards.Static, p *costProbe) string {
	switch strings.ToLower(st.Params["Condition"]) {
	case "", "playerturn":
	case "notplayerturn":
		return "NotPlayerTurn needs an opponent-turn probe"
	default:
		return "condition unsupported"
	}
	switch check := st.Params["CheckSVar"]; {
	case check == "":
	case check == "YouCastThisTurn" && st.Params["SVarCompare"] == "EQ1":
		// "The second spell you cast each turn": a cheap first spell.
		first := oraclegen.Step{Op: "cast", Seat: 0, Card: "p0:Shock", Mana: "R", Targets: []string{"p1"}}
		p.first = &first
	case check == "X" && st.Params["SVarCompare"] == "EQ0":
		// "The first <kind> spell you cast each turn": nothing was cast yet.
	default:
		return "CheckSVar condition unsupported"
	}
	present := st.Params["IsPresent"]
	if present == "" {
		return ""
	}
	need := 1
	if n, ok := strings.CutPrefix(st.Params["PresentCompare"], "GE"); ok {
		var err error
		if need, err = strconv.Atoi(n); err != nil || need < 1 {
			return "IsPresent count unsupported"
		}
	}
	names := presentFixtures(reg, strings.SplitN(present, ".", 2)[0], need)
	if len(names) < need {
		return "IsPresent fixture unavailable"
	}
	if strings.EqualFold(st.Params["PresentZone"], "Graveyard") {
		p.graveyard = appendUnique(p.graveyard, names...)
	} else {
		p.battlefield = appendUnique(p.battlefield, names...)
	}
	return ""
}

// presentFixtures names n distinct cards of the IsPresent type.
func presentFixtures(reg *cards.Registry, typ string, n int) []string {
	known := map[string]string{"Lesson": "Introduction to Annihilation", "Creature": "Grizzly Bears", "Artifact": "Silver Myr", "Kithkin": "Kithkin Greatheart", "land": "Forest", "Land": "Forest"}
	if card, ok := known[typ]; ok && n == 1 {
		return []string{card}
	}
	return affinityFixtures(reg, typ, n)
}

// costStaticReduction is how many generic mana the static removes in the
// probe, together with the board that makes a counted amount at least that
// large.
func costStaticReduction(source *cards.Face, st cards.Static, p *costProbe) (int, string) {
	amount := st.Params["Amount"]
	if strings.Contains(amount, "Speed") {
		return costSpeedFixture(p), ""
	}
	if n, err := strconv.Atoi(amount); err == nil {
		if n < 1 {
			return 0, "reduction below one"
		}
		return n, ""
	}
	desc := strings.ToLower(st.Params["Description"])
	switch {
	case amount != "X" && amount != "Y":
		return 0, "reduction amount unsupported"
	case strings.Contains(desc, "your speed"):
		return costSpeedFixture(p), ""
	case strings.Contains(desc, "equipped creature's power"):
		// Grizzly Bears is a 2/2: the equipped creature's power is 2.
		p.battlefield = appendUnique(p.battlefield, "Grizzly Bears")
		p.pre = append(p.pre, oraclegen.Step{Op: "attach", Seat: 0, Card: "p0:" + source.Name, AttachedTo: "p0:Grizzly Bears"})
		return 2, ""
	case strings.Contains(desc, "'s power"):
		if source.Power() < 1 {
			return 0, "source power below one"
		}
		return source.Power(), ""
	case strings.Contains(desc, "creature you control with power 4 or greater"):
		p.battlefield = appendUnique(p.battlefield, "Serra Angel")
		return 1, ""
	case strings.Contains(desc, "land you control with a flood counter"):
		p.battlefield = appendUnique(p.battlefield, "Island")
		p.seat = func(s *oraclegen.Seat) { *s = oraclegen.WithCounters(*s, "Island", "FLOOD", 1) }
		return 1, ""
	}
	return 0, "count-scaled reduction unsupported"
}

// costSpeedFixture starts p0 at speed 2 and returns the generic mana a "where X
// is your speed" reduction then removes. Speed 1 would not do: a seat that
// controls a permanent with Start your engines! starts at 1 anyway (CR
// 702.179a), so a setup speed of 1 could not tell the count from the keyword.
func costSpeedFixture(p *costProbe) int {
	const speed = 2
	prev := p.seat
	p.seat = func(s *oraclegen.Seat) {
		if prev != nil {
			prev(s)
		}
		s.Speed = speed
	}
	return speed
}

// preferredCostProbes are mainstream cards ranked ahead of the complexity
// order: they are certain to exist in XMage's card database, which a vanilla
// silver-border or Alchemy card is not. They only reorder candidates that
// already satisfy the static's filter and price.
var preferredCostProbes = map[string]bool{
	"Divination": true, "Concentrate": true, "Harmonize": true, "Wrath of God": true,
	"Day of Judgment": true, "Damnation": true, "Millstone": true, "Grizzly Bears": true,
	"Hill Giant": true, "Air Elemental": true, "Serra Angel": true, "Craw Wurm": true,
	"Colossal Dreadmaw": true, "Shivan Dragon": true, "Gray Ogre": true, "Mind Stone": true,
}

// costCandidate is one probe face with the ordering key that ranks it.
type costCandidate struct {
	face *cards.Face
	key  [4]int
}

// rankCostCandidates orders candidates simplest first: the fewest abilities,
// triggers and keywords (so the probe's own behaviour cannot interfere), then
// the shortest cost, then by name. The order never depends on corpus order.
func rankCostCandidates(cands []costCandidate) []*cards.Face {
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.key != b.key {
			for k := range a.key {
				if a.key[k] != b.key[k] {
					return a.key[k] < b.key[k]
				}
			}
		}
		return a.face.Name < b.face.Name
	})
	out := make([]*cards.Face, 0, costCandidateLimit)
	for _, c := range cands {
		if len(out) == costCandidateLimit {
			break
		}
		out = append(out, c.face)
	}
	return out
}

func costFaceRank(f *cards.Face) int {
	if preferredCostProbes[f.Name] {
		return 0
	}
	return 1
}

func costFaceComplexity(f *cards.Face) int {
	return len(f.Abilities) + len(f.Triggers) + len(f.Statics) + len(f.Repls) + len(f.Keywords)
}

// probeNameUsable keeps the probe to names a deck list and XMage's card
// database both hold (oraclegen.XMageKnown): Alchemy rebalances, quoted names
// and the Un-set, Alchemy-only and Commander-only cards the manifests lack.
func probeNameUsable(name string) bool {
	return oraclegen.XMageKnown(name)
}

// spellCostProbes picks single-faced spells satisfying the static's filter
// whose printed cost has enough generic mana for the reduction to remove and
// something left over, so the reduced price is never an empty payment.
func spellCostProbes(reg *cards.Registry, name string, st cards.Static, filter string, reduction int, base costProbe) []costProbe {
	var cands []costCandidate
	pay, full := map[*cards.Face]string{}, map[*cards.Face]string{}
	for _, card := range reg.Cards {
		if len(card.Faces) != 1 || card.Faces[0] == nil {
			continue
		}
		face := card.Faces[0]
		if face.Name == name || !probeNameUsable(face.Name) || face.ManaCost == "" || face.ManaCost == "no cost" || !spellFilterMatches(face, filter) || costFaceTargets(face) {
			continue
		}
		pool, why := oraclegen.PoolFor(face.ManaCost)
		if why != "" || (st.Params["Color"] != "" && strings.Contains(pool, st.Params["Color"])) {
			continue
		}
		mana, ok := removeGenericMana(pool, reduction)
		if !ok || mana == "" {
			continue
		}
		pay[face], full[face] = mana, pool
		cands = append(cands, costCandidate{face: face, key: [4]int{costFaceRank(face), costFaceComplexity(face), len(face.ManaCost), 0}})
	}
	var out []costProbe
	for _, face := range rankCostCandidates(cands) {
		p := base
		p.spell, p.mana, p.full = face.Name, pay[face], full[face]
		out = append(out, p)
	}
	return out
}

// costFaceTargets reports a spell that asks for a target: the probe cast
// carries none, so such a spell would stall on a target decision.
func costFaceTargets(f *cards.Face) bool {
	for _, sa := range f.Abilities {
		if sa.ParamStr(cards.PKValidTgts) != "" {
			return true
		}
	}
	return costFaceHasType(f, "Aura")
}

// abilityCostProbes picks a permanent on the battlefield whose mana-only
// activated ability the static makes cheaper. An Equip static targets its own
// source, so the probe equips the source.
func abilityCostProbes(reg *cards.Registry, name string, st cards.Static, filter string, reduction int, base costProbe) ([]costProbe, string) {
	wantKind := strings.TrimPrefix(st.Params["ValidSpell"], "Activated.")
	if strings.HasPrefix(st.Params["ValidSpell"], "Static.") {
		return nil, "static-ability cost probe unsupported"
	}
	if st.Params["ValidSpell"] == "" {
		wantKind = ""
	}
	targetsSource := strings.EqualFold(st.Params["ValidTarget"], "Card.Self")
	var cands []costCandidate
	found := map[*cards.Face]costProbe{}
	for _, card := range reg.Cards {
		if len(card.Faces) == 0 || card.Faces[0] == nil {
			continue
		}
		face := card.Faces[0]
		if face.Name == name || !probeNameUsable(face.Name) || !spellFilterMatches(face, filter) {
			continue
		}
		for i, sa := range face.Abilities {
			if !sa.IsActivated() || !abilityIsKind(sa, wantKind) {
				continue
			}
			slots := oraclegen.AbilitySlotSpecs(face, sa)
			if len(slots) > 1 || len(slots) == 1 && !targetsSource {
				continue
			}
			pool, ok := manaOnlyCost(sa.ParamStr(cards.PKCost))
			if !ok {
				continue
			}
			mana, ok := removeGenericMana(pool, reduction)
			if !ok || mana == "" {
				continue
			}
			prefixes, why := oraclegen.XMageAbility(face)
			if why != "" || prefixes[i] == "" {
				continue
			}
			p := base
			p.spell, p.mana, p.full = face.Name, mana, pool
			p.battlefield = appendUnique(p.battlefield, face.Name)
			p.activate = &costActivation{index: i, prefix: prefixes[i]}
			if targetsSource {
				p.activate.targets = []string{"p0:" + name}
			}
			found[face] = p
			cands = append(cands, costCandidate{face: face, key: [4]int{costFaceRank(face), costFaceComplexity(face), len(pool), i}})
			break
		}
	}
	var out []costProbe
	for _, face := range rankCostCandidates(cands) {
		out = append(out, found[face])
	}
	return out, ""
}

// abilityIsKind is true for an activated ability of the named kind: Equip is
// the keyword's attach ability, Exhaust and PowerUp are flags on the ability,
// and an empty kind accepts any activated ability.
func abilityIsKind(sa *cards.SA, kind string) bool {
	switch kind {
	case "":
		return true
	case "Equip":
		return sa.ParamStr(cards.PKKeyword) == "Equip"
	}
	return strings.EqualFold(sa.Params[kind], "True")
}

// manaOnlyCost is the pool paying an ability cost made only of mana: a probe
// with a tap or sacrifice component would need a prelude this generator does
// not script.
func manaOnlyCost(cost string) (string, bool) {
	if cost == "" {
		return "", false
	}
	pool, why := oraclegen.PoolFor(cost)
	if why != "" || pool == "" {
		return "", false
	}
	return pool, true
}

// spellFilterSupported is false when a filter names a term this matcher does
// not model, so the row is a named gap rather than a probe chosen by a filter
// that silently matched nothing.
func spellFilterSupported(filter string) bool {
	for _, term := range filterTerms(filter) {
		if _, known := costTermMatches(&cards.Face{}, term); !known {
			return false
		}
	}
	return true
}

func filterTerms(filter string) []string {
	var out []string
	for _, term := range strings.FieldsFunc(filter, func(r rune) bool { return r == ',' || r == '+' || r == '.' }) {
		out = append(out, strings.TrimSpace(term))
	}
	return out
}

// spellFilterMatches handles the card/type/colour filter grammar used by
// cost-reduction statics. Commas separate alternatives; '+' and '.' join terms.
func spellFilterMatches(face *cards.Face, filter string) bool {
	for _, alternative := range strings.Split(filter, ",") {
		matched := true
		for _, term := range filterTerms(alternative) {
			if ok, _ := costTermMatches(face, term); !ok {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// costTermMatches evaluates one filter term against face. known is false for
// a term this matcher does not model.
func costTermMatches(face *cards.Face, term string) (matches, known bool) {
	lower := strings.ToLower(term)
	switch lower {
	case "":
		return true, true
	case "card", "spell", "you", "youctrl", "youown", "other":
		return true, true
	case "permanent":
		return !costFaceHasType(face, "Instant") && !costFaceHasType(face, "Sorcery"), true
	case "red", "blue", "white", "black", "green":
		letter := strings.ToUpper(lower[:1])
		if lower == "blue" {
			letter = "U"
		}
		return strings.Contains(strings.ToLower(face.Colors), lower) || strings.Contains(face.ManaCost, letter), true
	}
	if strings.HasPrefix(lower, "cmc") && len(lower) > 5 {
		n, err := strconv.Atoi(lower[5:])
		if err != nil {
			return false, false
		}
		cmc := int(face.Cmc())
		switch lower[3:5] {
		case "ge":
			return cmc >= n, true
		case "le":
			return cmc <= n, true
		case "eq":
			return cmc == n, true
		}
		return false, false
	}
	if lower == "powerlttoughness" {
		return face.Power() < face.Toughness(), true
	}
	if rest := strings.TrimPrefix(term, "with"); rest != term && rest != "" {
		for _, kw := range face.Keywords {
			if strings.EqualFold(kw, rest) || strings.HasPrefix(strings.ToLower(kw), lower[4:]+":") {
				return true, true
			}
		}
		return false, true
	}
	if rest := strings.TrimPrefix(term, "non"); rest != term && rest != "" {
		if ok, known := costTermMatches(face, rest); known && !costTypeWord(rest) {
			return !ok, true
		}
		return !costFaceHasType(face, rest), costTypeWord(strings.ToUpper(rest[:1]) + rest[1:])
	}
	if !costTypeWord(term) {
		return false, false
	}
	return costFaceHasType(face, term), true
}

// costTypeWord is true for a card type or subtype word, which are the only
// capitalised terms a cost-reduction filter holds beyond the ones above.
func costTypeWord(term string) bool {
	return term != "" && term[0] >= 'A' && term[0] <= 'Z' && !strings.ContainsAny(term, "!$<>=")
}

func costFaceHasType(face *cards.Face, want string) bool {
	for _, typ := range face.Types {
		if strings.EqualFold(typ, want) {
			return true
		}
	}
	return false
}
