// The activate preludes a static fixture's afterSteps run (ticket
// levelb-static-count-attachments). A static whose amount is a computed count
// the bare turn cannot make observable has two shapes the cast path cannot
// reach: a signed "-X where X is your life total" on a Vehicle whose P/T the
// snapshot prints only once the card is a creature (The Last Ride needs the
// crew the driver's activate op drives), and a back-face characteristic-
// defining P/T that counts the cards exiled with the source (Sunbird Effigy's
// ExiledWith$Colors needs the Craft activation that populates the exile set).
// Both preludes mirror classLevelPrelude's shape: an activate step labelled
// with the ability's XMage rule-text prefix, then a resolve.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// crewAbilityIndex is the face's crew activation (the kw:Crew expansion
// stamps KeywordLine$ Crew), -1 when the face has none.
func crewAbilityIndex(f *cards.Face) int {
	for i, sa := range f.Abilities {
		if strings.HasPrefix(strings.TrimSpace(sa.ParamStr(cards.PKKeywordLine)), "Crew") {
			return i
		}
	}
	return -1
}

// craftAbilityIndex is the front face's Craft activation (the kw:Craft
// expansion stamps Keyword$ Craft), -1 when the face has none.
func craftAbilityIndex(f *cards.Face) int {
	for i, sa := range f.Abilities {
		if sa.ParamStr(cards.PKKeyword) != "Craft" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(sa.ParamStr(cards.PKKeywordLine)), "Craft") {
			return i
		}
	}
	return -1
}

// crewPrelude is the afterSteps block that crews the card: the crew
// activation (its tapXType cost's catalogue fixtures are placed by the
// caller, so the tap election has candidates), then a resolve that animates
// the Vehicle. cost is the activation's Cost$, for the serving loop's cost
// answers; xab is parallel to the returned steps.
func crewPrelude(f *cards.Face, name string) (steps []oraclegen.Step, xab []string, taps []string, cost string, ok bool) {
	idx := crewAbilityIndex(f)
	if idx < 0 {
		return nil, nil, nil, "", false
	}
	prefixes, why := oraclegen.XMageAbility(f)
	if why != "" {
		return nil, nil, nil, "", false
	}
	prefix, okp := prefixes[idx]
	if !okp {
		return nil, nil, nil, "", false
	}
	sa := f.Abilities[idx]
	cost = sa.ParamStr(cards.PKCost)
	for _, tok := range costTokens(cost) {
		if !strings.HasPrefix(tok, "tapXType") {
			continue
		}
		picks, supported := tapXTypeFixtures(tok, activationX(cost))
		if !supported {
			return nil, nil, nil, "", false
		}
		taps = append(taps, picks...)
	}
	if len(taps) == 0 {
		return nil, nil, nil, "", false
	}
	i := idx
	steps = append(steps, oraclegen.Step{
		Op: "activate", Seat: 0, Card: "p0:" + name, AbilityIndex: &i,
	})
	steps = append(steps, oraclegen.Step{Op: "resolve"})
	xab = append(xab, prefix, "")
	return steps, xab, taps, cost, true
}

// craftPrelude is the afterSteps block that crafts the card: the Craft
// activation (its {N} mana and material exile cost), then a resolve that
// returns the card transformed. materials are the catalogue cards the cost
// part's fixture exiles (the caller places them in the seat the cost part
// reads). xab is parallel to the returned steps.
func craftPrelude(front *cards.Face, name string) (steps []oraclegen.Step, xab []string, materials []string, cost string, ok bool) {
	idx := craftAbilityIndex(front)
	if idx < 0 {
		return nil, nil, nil, "", false
	}
	prefixes, why := oraclegen.XMageAbility(front)
	if why != "" {
		return nil, nil, nil, "", false
	}
	prefix, okp := prefixes[idx]
	if !okp {
		return nil, nil, nil, "", false
	}
	sa := front.Abilities[idx]
	cost = sa.ParamStr(cards.PKCost)
	pool, gap := activationCostIn(cost, "battlefield", "")
	if gap != "" {
		return nil, nil, nil, "", false
	}
	x := activationX(cost)
	for _, tok := range costTokens(cost) {
		if !strings.HasPrefix(tok, "ExileCtrlOrGrave") {
			continue
		}
		picks := activationCostFixturesX(tok, x)
		if len(picks) == 0 {
			return nil, nil, nil, "", false
		}
		materials = append(materials, picks...)
	}
	if len(materials) == 0 {
		return nil, nil, nil, "", false
	}
	i := idx
	steps = append(steps, oraclegen.Step{
		Op: "activate", Seat: 0, Card: "p0:" + name, Mana: pool, AbilityIndex: &i,
	})
	steps = append(steps, oraclegen.Step{Op: "resolve"})
	xab = append(xab, prefix, "")
	return steps, xab, materials, cost, true
}

// printedPT is f's printed "power/toughness", ok=false for a value the
// negative-life fixture cannot size (a * the CDA defines, a missing one).
func printedFacePT(f *cards.Face) (power, toughness int32, ok bool) {
	slash := strings.IndexByte(f.PT, '/')
	if slash < 0 {
		return 0, 0, false
	}
	p, err1 := parsePTInt(f.PT[:slash])
	t, err2 := parsePTInt(f.PT[slash+1:])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return p, t, true
}

// parsePTInt is one printed P/T half.
func parsePTInt(s string) (int32, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return int32(n), err
}

// staticCrewFixture is the fixture for a self static whose signed -X amount
// is your life total (The Last Ride's "gets -X/-X, where X is your life
// total"): p0's starting life is set just below the printed power, so the
// count the static subtracts leaves a positive, non-printed P/T, and the
// card is crewed after the cast -- a snapshot prints P/T for creatures only,
// so an uncrewed Vehicle shows nothing. The crew cost's catalogue taps are
// placed on the battlefield beside the probe, and the serving loop scripts
// XMage's crew dialog from the runner's observed pick (reanswers).
func staticCrewFixture(reg *cards.Registry, f *cards.Face, name string, st cards.Static) (staticFixture, bool) {
	if !negativeLifeCount(f, st) {
		return staticFixture{}, false
	}
	power, toughness, ok := printedFacePT(f)
	if !ok {
		return staticFixture{}, false
	}
	// The crewed P/T is printed minus life; life must leave BOTH values
	// positive (a non-positive toughness is an SBA sweep that removes the
	// card) and below the format's starting total (20), so the result is
	// neither printed nor dead. power-1 is the largest life that keeps the
	// power readable.
	life := min(power, toughness) - 1
	if life < 1 || life > 19 {
		return staticFixture{}, false
	}
	steps, xab, taps, cost, ok := crewPrelude(f, name)
	if !ok {
		return staticFixture{}, false
	}
	fx := staticFixture{
		conditionPrelude: conditionPrelude{
			battlefield: taps,
			life:        life,
		},
		afterSteps:     steps,
		afterXab:       xab,
		reanswers:      true,
		activationCost: cost,
	}
	return fx, true
}

// staticCraftFixture is the fixture for a back-face characteristic-defining
// P/T that counts the cards exiled with the source (Sunbird Standard's
// Sunbird Effigy face, "power and toughness are each equal to the number of
// colors among the exiled cards used to craft it"): the front face is cast,
// then its Craft activation exiles the card and one coloured catalogue
// material, so the back face enters transformed with the exile set populated
// and the count prints. The material cards go to the graveyard, the zone the
// ExileCtrlOrGrave cost part also reads (the standalone activate template's
// craft fixtures land there), and the serving loop scripts XMage's material
// selector from the runner's observed pick (reanswers).
func staticCraftFixture(reg *cards.Registry, c *cards.Card, f *cards.Face, name string, st cards.Static) (staticFixture, bool) {
	if !exiledWithColorsCount(f, st) {
		return staticFixture{}, false
	}
	if len(c.Faces) < 2 || c.Faces[0] == f {
		return staticFixture{}, false
	}
	steps, xab, materials, cost, ok := craftPrelude(c.Faces[0], name)
	if !ok {
		return staticFixture{}, false
	}
	return staticFixture{
		conditionPrelude: conditionPrelude{graveyard: materials},
		afterSteps:       steps,
		afterXab:         xab,
		reanswers:        true,
		craft:            true,
		activationCost:   cost,
	}, true
}

// negativeLifeCount reports whether st's pump amount is a signed "-X" whose
// SVar body is your life total (the one count a lower starting life makes
// observable against a higher one).
func negativeLifeCount(f *cards.Face, st cards.Static) bool {
	if !strings.HasPrefix(st.ParamStr(cards.PKAddPower), "-") &&
		!strings.HasPrefix(st.ParamStr(cards.PKAddToughness), "-") {
		return false
	}
	for _, body := range staticSVarBodies(f, st) {
		if strings.EqualFold(strings.TrimSpace(body), "Count$YourLifeTotal") {
			return true
		}
	}
	return false
}

// exiledWithColorsCount reports whether st's set amount is an SVar body
// reading the colours among the cards exiled with the source. The SVar is
// resolved off the set param's own face table: staticSVarBodies visits only
// the AddPower/AddToughness/CheckSVar keys, and a CDA's value rides SetPower$/
// SetToughness$.
func exiledWithColorsCount(f *cards.Face, st cards.Static) bool {
	pumps := []cards.ParamKey{cards.PKSetPower, cards.PKSetToughness, cards.PKAddPower, cards.PKAddToughness}
	reads := false
	for _, k := range pumps {
		if !st.HasParam(k) {
			continue
		}
		if _, err := parsePTInt(st.ParamStr(k)); err != nil {
			reads = true
		}
	}
	if !reads {
		return false
	}
	for _, k := range pumps {
		v := strings.TrimSpace(st.ParamStr(k))
		if v == "" {
			continue
		}
		body, ok := f.SVars[strings.TrimLeft(v, "+-")]
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(body), "ExiledWith$Colors") {
			return true
		}
	}
	return false
}
