// Fixtures for the states a continuous static's condition or count reads that
// cards in the seats cannot stand in for (ticket
// cli-20261009T031407Z-8f4b4f49, level-B class G7 "static effect
// unobservable"): creature tokens on the battlefield, an Equipment attached
// to the affected permanent, unspent mana left in the pool, and a raid count
// the turn's own attack makes true. Each builder returns one staticFixture
// candidate for staticFixtures to try; the template never evaluates the
// condition itself, so a fixture that does not satisfy the gate shows no
// change and is discarded -- a wrong stand-in can never be served.
package templates

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// staticTokenSpell is the spell a token fixture casts, and the kind of token
// it makes (gorge names the token after the kind). The spell is a plain
// sorcery with no riders: its tokens enter and stay, and nothing of its own
// moves a permanent's P/T or keywords.
const (
	staticTokenSpell = "Dragon Fodder"
	staticTokenKind  = "Goblin"
)

// staticTokenFixture is the fixture for a static whose Affected$ filter (or
// IsPresent$ condition) selects creature tokens, which the seats cannot hold
// (staticGap's "needs a token"). It casts staticTokenSpell enough times for
// the IsPresent count (one cast, two tokens, when the static names no count)
// and records the tokens' printed spec, so the observation compares the
// snapshot's tokens against what the token itself is rather than against a
// card. Affected words naming attacking tokens attack them on the following
// turn: a token a prelude cast made is summoning sick, so the fixture ends
// p0's turn and casts the card on the next one.
func staticTokenFixture(reg *cards.Registry, st cards.Static, affected string) (staticFixture, bool) {
	words := affectedWords(affected)
	present := affectedWords(st.ParamStr(cards.PKIsPresent))
	if !hasWord(words, "token") && !hasWord(present, "token") {
		return staticFixture{}, false
	}
	n := staticCountFrom(st.ParamStr(cards.PKPresentCompare))
	if n < 1 {
		n = 1
	}
	pre, ok := tokenCostPrelude(reg, []tokenNeed{{staticTokenKind, n}})
	if !ok {
		return staticFixture{}, false
	}
	fx := staticFixture{conditionPrelude: pre}
	if name, spec, ok := staticTokenSpec(reg, staticTokenSpell); ok {
		fx.tokenName = name
		fx.tokenSpec = spec
	} else {
		return staticFixture{}, false
	}
	if hasWord(words, "attacking") {
		// The prelude's maker casts round up to the maker's per-cast count,
		// so every token it made attacks.
		per := activationTokenMakers[staticTokenKind].per
		created := ((n + per - 1) / per) * per
		fx.steps = append(fx.steps,
			oraclegen.Step{Op: "pass_to", Step: "end"},
			oraclegen.Step{Op: "pass_to", Step: "main1", Active: "p0"})
		fx.tokenAttackers = staticTokenRefs(fx.tokenName, created)
	}
	return fx, true
}

// staticTokenRefs is the scenario refs of n same-named tokens in creation
// order: objRef's rank rule numbers the second and later "#2", "#3", ...
func staticTokenRefs(name string, n int) []string {
	out := make([]string, 0, n)
	for k := 1; k <= n; k++ {
		ref := "p0:token:" + name
		if k > 1 {
			ref += "#" + strconv.Itoa(k)
		}
		out = append(out, ref)
	}
	return out
}

// staticTokenSpec is the printed observable spec of the token the maker
// makes, read from the token script the maker's TokenScript$ names: its P/T
// (creature tokens only) and its keyword folds. The observation keys the
// snapshot's tokens by this name, so a token the static grants nothing stays
// printed and nothing is served.
func staticTokenSpec(reg *cards.Registry, maker string) (name string, spec staticProbeSpec, ok bool) {
	c, ok := reg.Lookup(maker)
	if !ok || len(c.Faces) == 0 {
		return "", staticProbeSpec{}, false
	}
	for _, sa := range c.Faces[0].Abilities {
		stem := strings.TrimSpace(sa.Params["TokenScript"])
		if stem == "" {
			continue
		}
		t, ok := reg.Token(stem)
		if !ok || len(t.Faces) == 0 {
			continue
		}
		f := t.Faces[0]
		return f.Name, staticProbeSpec{
			pt:            f.PT,
			keywords:      oraclediff.ComparedKeywords(f.Keywords, false),
			namedKeywords: oraclediff.ComparedKeywords(f.Keywords, true),
			creature:      f.IsCreature(),
		}, true
	}
	return "", staticProbeSpec{}, false
}

// staticEquipCard is the Equipment the equip fixture attaches. It prints no
// keyword, trigger or ability beyond its one static's constant +2/+0, so the
// fixture's own contribution to the equipped permanent is exactly the P/T
// delta the compared spec is shifted by (attachPT), the same shift the
// probe-counters fixture applies to its counters.
const staticEquipCard = "Bonesplitter"

// staticEquipFixture is the fixture for a static whose Affected$ filter
// selects equipped permanents ("needs an equipped permanent"): it places an
// Equipment in setup and attaches it to the probe, or to the card itself when
// the filter is defined by Self. The equipment's own constant P/T grant is
// carried on the fixture so the compared spec can be shifted by it.
func staticEquipFixture(reg *cards.Registry, st cards.Static) (staticFixture, bool) {
	affected := st.ParamStr(cards.PKAffected)
	words := affectedWords(affected)
	if !hasWord(words, "equipped") {
		return staticFixture{}, false
	}
	// A static defined by the permanent the card itself is attached to (an
	// Aura's EnchantedBy, an Equipment's EquippedBy) lands on that host; the
	// attachment prelude and the attach-host fallback serve those rows.
	for _, w := range words {
		switch w {
		case "EnchantedBy", "EquippedBy", "AttachedBy", "FortifiedBy":
			return staticFixture{}, false
		}
	}
	if hasWord(words, "OppCtrl") {
		return staticFixture{}, false
	}
	pt, ok := staticEquipPTDelta(reg, staticEquipCard)
	if !ok {
		return staticFixture{}, false
	}
	fx := staticFixture{equip: staticEquipCard, attachPT: pt}
	if hasWord(words, "Self") {
		fx.attach = "self"
	} else {
		fx.attach = staticProbe
	}
	return fx, true
}

// staticEquipPTDelta is the constant P/T grant the Equipment's printed
// statics make on the equipped permanent. A static with any other param (a
// keyword, a counter, a computed or characteristic-defining value) makes the
// equipment's own contribution unobservable, so the fixture is refused: the
// observation must never be able to fire on the equipment alone.
func staticEquipPTDelta(reg *cards.Registry, name string) ([2]int32, bool) {
	var out [2]int32
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return out, false
	}
	for _, st := range c.Faces[0].Statics {
		keys := make([]string, 0, len(st.Params))
		for k := range st.Params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := st.Params[k]
			switch k {
			case "Mode", "Affected", "AffectedZone", "EffectZone", "Description", "KeywordLine":
			case "AddPower", "AddToughness":
				n, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil {
					return [2]int32{}, false
				}
				if k == "AddPower" {
					out[0] += int32(n)
				} else {
					out[1] += int32(n)
				}
			default:
				return [2]int32{}, false
			}
		}
	}
	return out, true
}

// staticStateFixtures is the state fixtures staticFixtures appends after the
// condition-derived ones, most specific first.
func staticStateFixtures(reg *cards.Registry, f *cards.Face, st cards.Static) []staticFixture {
	affected := st.ParamStr(cards.PKAffected)
	var out []staticFixture
	if fx, ok := staticTokenFixture(reg, st, affected); ok {
		out = append(out, fx)
	}
	if fx, ok := staticEquipFixture(reg, st); ok {
		out = append(out, fx)
	}
	if fx, ok := staticAttackCountFixture(reg, f, st, affected); ok {
		out = append(out, fx)
	}
	return out
}

// staticAttackCountFixture is the fixture for a count of the attackers
// declared this turn (Count$AttackersDeclared, the Raid family): the turn's
// own combat makes the count true. It adds two more of the affected filter's
// creature probe and attacks with all three before the card is cast, so the
// count the static's SVar reads is three at the final checkpoint. The attack
// step is a prelude: the probes are setup-placed, so they are not summoning
// sick. ok is false unless an SVar body of the static reads the attacker
// count and the affected filter names a creature the probe table stands in
// for.
func staticAttackCountFixture(reg *cards.Registry, f *cards.Face, st cards.Static, affected string) (staticFixture, bool) {
	counts := false
	for _, body := range staticSVarBodies(f, st) {
		if strings.Contains(strings.ToLower(body), "attackersdeclared") {
			counts = true
		}
	}
	if !counts {
		return staticFixture{}, false
	}
	card := staticAffectedCreatureProbe(reg, affected)
	if card == "" {
		return staticFixture{}, false
	}
	attackers := []string{"p0:" + card, "p0:" + card + "#2", "p0:" + card + "#3"}
	return staticFixture{
		conditionPrelude: conditionPrelude{
			battlefield: []string{card, card},
			steps: []oraclegen.Step{
				{Op: "attack", Seat: 0, Defender: "p1", Attackers: attackers},
				{Op: "pass_to", Step: "main2"},
			},
		},
	}, true
}

// staticAffectedCreatureProbe is the probe the affected filter's first word
// selects (staticPlanFor's own table), so the fixture's extra copies stand
// beside the probe the plan already places. Only a creature probe counts: an
// attack needs creatures, and the affected grant must land on them.
func staticAffectedCreatureProbe(reg *cards.Registry, affected string) string {
	for _, w := range affectedWords(affected) {
		for _, row := range staticProbeTable {
			if row.word != w {
				continue
			}
			if c, ok := reg.Lookup(row.card); ok && len(c.Faces) > 0 && c.Faces[0].IsCreature() {
				return row.card
			}
			return ""
		}
	}
	return ""
}
