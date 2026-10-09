package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// This file is the single home of the token vocabulary an activation cost can
// ask for: Sac<1/Food>, Sac<3/Treasure>, Sac<1/Permanent.token> and
// tapXType<2/Permanent.token>. No fixture card pays such a cost, so a prelude
// casts a token-maker (the activationTokenMakers table) and the activate step
// pays with the tokens that cast made. Both the cost gate (tokenCostSupported)
// and the prelude (withTokenCostPrelude) read tokenCostNeeds, so what the gate
// admits is exactly what the prelude can make.

// tokenNeed is n tokens of one kind. kind is the token's subtype ("Food",
// "Treasure", "Clue", "Goblin"), which is how gorge names the token.
type tokenNeed struct {
	kind string
	n    int
}

// tokenMaker casts card for pool (a creature or spell making per tokens of
// the kind) and resolves the stack resolves times (an ETB-making creature
// needs its trigger resolved too).
type tokenMaker struct {
	card     string
	per      int
	resolves int
}

// activationTokenMakers maps a token kind to the card that makes it. Every
// card is in the corpus and in XMage's card database.
var activationTokenMakers = map[string]tokenMaker{
	"Food":     {"Gilded Goose", 1, 2},
	"Treasure": {"Strike It Rich", 1, 1},
	"Clue":     {"Thraben Inspector", 1, 2},
	"Goblin":   {"Dragon Fodder", 2, 1},
}

// anyTokenKind maps a token-cost base that names only a CLASS (a permanent
// token, a creature token, an artifact token) to the maker kind that pays it.
// A Food token is an artifact, so it pays an artifact-token cost; a Goblin is
// a creature, so it pays the generic token costs.
var anyTokenKind = map[string]string{
	"permanent": "Goblin",
	"card":      "Goblin",
	"creature":  "Goblin",
	"artifact":  "Food",
}

// tokenCostNeeds reads a Sac or tapXType token's token requirement. ok is
// false when the token is not a token cost this file can make.
func tokenCostNeeds(tok string) ([]tokenNeed, bool) {
	payload, ok := bracketPayload(tok)
	if !ok {
		return nil, false
	}
	parts := strings.SplitN(payload, "/", 3)
	if len(parts) < 2 {
		return nil, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || n < 1 {
		return nil, false
	}
	head := tok[:strings.IndexByte(tok, '<')]
	for _, alt := range strings.Split(strings.ToLower(parts[1]), ";") {
		if needs, ok := tokenAltNeeds(head, strings.TrimSpace(alt), n); ok {
			return needs, true
		}
	}
	return nil, false
}

// tokenAltNeeds maps one filter alternative to the tokens that pay it.
func tokenAltNeeds(head, alt string, n int) ([]tokenNeed, bool) {
	base := alt
	if i := strings.IndexAny(base, ".+"); i >= 0 {
		base = base[:i]
	}
	isToken := strings.Contains(alt, ".token")
	switch {
	case head == "Sac" && sacTokenBases[base] && !isToken:
		return []tokenNeed{{strings.ToUpper(base[:1]) + base[1:], n}}, true
	case isToken && anyTokenKind[base] != "":
		// A '+' modifier (Forge's WithDifferentNames, namedWood) narrows the
		// token the cost accepts. This build does not evaluate a Sac cost
		// spec's modifier (rules/cost keeps it on CostPart.Spec and the
		// filter matcher fails closed on it), so no maker can be shown to
		// satisfy one: fail closed rather than pay with a token the modifier
		// forbids. The scope is exactly the modifier, not the base kind.
		if strings.Contains(alt, "+") {
			return nil, false
		}
		return []tokenNeed{{anyTokenKind[base], n}}, true
	}
	return nil, false
}

// activationTokenNeeds is every token need of a whole Cost$ string.
func activationTokenNeeds(cost string) []tokenNeed {
	var out []tokenNeed
	for _, tok := range costTokens(cost) {
		if !strings.HasPrefix(tok, "Sac<") && !strings.HasPrefix(tok, "tapXType<") {
			continue
		}
		if needs, ok := tokenCostNeeds(tok); ok && !tokenCostHasFixture(tok) {
			out = append(out, needs...)
		}
	}
	return out
}

// tokenCostHasFixture reports a cost a plain catalogue fixture already pays:
// Sac<1/Creature.Other;Permanent.token+Other> is paid by a creature, so it
// needs no token.
func tokenCostHasFixture(tok string) bool {
	if strings.HasPrefix(tok, "Sac<") {
		_, ok := sacFilterFixtures(tok, 0)
		return ok
	}
	_, gap := tapXTypeResolve(tok)
	return gap == ""
}

// tokenCostSupported is activationCostIn's admission test for a token cost.
func tokenCostSupported(tok string) bool {
	_, ok := tokenCostNeeds(tok)
	return ok
}

// tokenCostPrelude builds the cast/resolve steps (and hand cards) that make
// the needs' tokens. ok is false when a maker card is missing or unpayable.
func tokenCostPrelude(reg *cards.Registry, needs []tokenNeed) (conditionPrelude, bool) {
	var pre conditionPrelude
	copies := map[string]int{}
	for _, need := range needs {
		maker, found := activationTokenMakers[need.kind]
		if !found {
			return conditionPrelude{}, false
		}
		for made := 0; made < need.n; made += maker.per {
			st, ok := castProbe(reg, maker.card)
			if !ok {
				return conditionPrelude{}, false
			}
			copies[maker.card]++
			if copies[maker.card] > 1 {
				st.Card += "#" + strconv.Itoa(copies[maker.card])
			}
			pre.hand = append(pre.hand, maker.card)
			pre.steps = append(pre.steps, st)
			for i := 0; i < maker.resolves; i++ {
				pre.steps = append(pre.steps, oraclegen.Step{Op: "resolve"})
			}
		}
	}
	return pre, true
}

// withTokenCostPrelude folds the cost's token prelude into every candidate
// prelude. A cost with no token need returns the candidates unchanged; one
// whose makers are unavailable returns none, so no scenario is built.
func withTokenCostPrelude(reg *cards.Registry, cost string, candidates []conditionPrelude) []conditionPrelude {
	needs := activationTokenNeeds(cost)
	if len(needs) == 0 {
		return candidates
	}
	tokens, ok := tokenCostPrelude(reg, needs)
	if !ok {
		return nil
	}
	out := make([]conditionPrelude, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, mergeConditionPreludes([]conditionPrelude{tokens, c}))
	}
	return out
}

// xmageTokenName is the name XMage's permanent picker matches a token by:
// "<Subtype> Token" (FoodToken, TreasureToken, ClueArtifactToken, GoblinToken).
// A pick whose ref is not a token keeps its name.
func xmageTokenName(pick, ref string) string {
	if strings.Contains(ref, ":token:") && !strings.HasSuffix(pick, " Token") {
		return pick + " Token"
	}
	return pick
}

// observedCostPick is the XMage answer for decision d's observed cost pick i:
// the exact-ref alias when the name matches several offered objects (three
// identical Goblin Tokens), else the name in XMage's token spelling.
func observedCostPick(d rules.OracleDecision, i int) string {
	if alias := oraclegen.ExactRefAlias(d, i); alias != "" {
		return alias
	}
	if i < len(d.ObjectPicks) {
		return xmageTokenName(d.Picks[i], d.ObjectPicks[i])
	}
	return d.Picks[i]
}

// tokenCostAnswerNames is the XMage answer for each token a cost needs when
// gorge posed no pick to read (one legal token is not asked). Names repeat:
// XMage's picker is asked once per token.
func tokenCostAnswerNames(needs []tokenNeed) []string {
	var out []string
	for _, need := range needs {
		for i := 0; i < need.n; i++ {
			out = append(out, need.kind+" Token")
		}
	}
	return out
}

// appendTokenAnswers makes the step carry one choice answer per named token,
// repeats kept: XMage's picker is asked once per token, so N identical tokens
// are N identical answers (the name dedupe the catalogue picks use would leave
// one). A single answer the step already holds (XAnswersForScenario scripts a
// one-pick sacrifice itself) counts toward the total, so a pick is never
// queued twice. The rest follow the step's other answers exactly as a
// catalogue pick's singles do (Kithkeeper, Supportive Parents: agreed in
// XMage).
func appendTokenAnswers(answers []oraclegen.XAnswer, seat int, names ...string) []oraclegen.XAnswer {
	have := map[string]int{}
	for _, a := range answers {
		if a.Seat == seat && a.Kind == "choice" {
			have[strings.ToLower(a.Value)]++
		}
	}
	for _, name := range names {
		if key := strings.ToLower(name); have[key] > 0 {
			have[key]--
			continue
		}
		answers = append(answers, oraclegen.XAnswer{Seat: seat, Kind: "choice", Value: name})
	}
	return answers
}

// isTokenPick reports a pick name that is a token's ("Goblin Token").
func isTokenPick(name string) bool { return strings.HasSuffix(name, " Token") }
