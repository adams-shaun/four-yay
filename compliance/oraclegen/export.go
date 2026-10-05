package oraclegen

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
)

// The shared generator core the per-template files under
// oraclegen/templates build on. Thin exported names over the package's
// own helpers, so the helpers keep their call sites here.

// XValue is the X every generated X spell is cast with.
const XValue = xValue

// CharmMode is one charm mode: its SVar and gorge's label for it.
type CharmMode = charmMode

// Label is the mode's label as gorge's mode decision shows it.
func (m CharmMode) Label() string { return m.label }

// SVar is the SVar naming the mode's ability chain.
func (m CharmMode) SVar() string { return m.svar }

// Fixture is one target fixture: both seats' setup and the cast's targets.
type Fixture = fixture

// P0 and P1 are the fixture's seats; Targets the cast's target refs.
func (fx *Fixture) P0() *Seat        { return &fx.p0 }
func (fx *Fixture) P1() *Seat        { return &fx.p1 }
func (fx Fixture) Targets() []string { return fx.targets }

// Attacker is the p0 creature the fixture must declare attacking so a target
// filter naming an attacking or blocking creature has a legal target. Empty
// means no attack step is needed.
func (fx Fixture) Attacker() string { return fx.combat.attacker }

// Blocker is the p1 creature that blocks the fixture's attacker, so a
// "blocking creature" target filter has a legal target. Empty means p1
// declares no blocks (only the attacker step is needed).
func (fx Fixture) Blocker() string { return fx.combat.blocker }

// Fixtures is the capped cross product of every target slot's candidates.
func Fixtures(slots []string) []Fixture { return fixtures(slots) }

// PoolFor turns a Forge mana cost into the exact pool letters that pay it,
// or says why it cannot.
func PoolFor(cost string) (string, string) { return poolFor(cost) }

// TargetSlots lists the ValidTgts$ filters along the spell ability chain.
func TargetSlots(f *cards.Face) []string { return targetSlots(f) }

// OpeningHandAnswers declines a K:MayEffectFromOpeningHand ask, so a
// generated scenario casts the card from hand rather than starting it on the
// battlefield. Nil when the card has no such keyword.
func OpeningHandAnswers(f *cards.Face) []Answer { return openingHandAnswers(f) }

// CharmModes lists the spell's charm modes in Choices$ order.
func CharmModes(f *cards.Face) []CharmMode { return charmModes(f) }

// ChainSlots lists the target filters along one SVar ability chain.
func ChainSlots(f *cards.Face, svar string) []string { return chainSlots(f, svar) }

// AbilityTargetsStack reports whether an ability's target vocabulary names a
// spell or ability on the stack.
func AbilityTargetsStack(params map[string]string) bool { return abilityTargetsStack(params) }

// ModeNumbers maps each charm mode label to its 1-based Choices$ position.
func ModeNumbers(f *cards.Face) map[string]int { return modeNumbers(f) }

// XAnswers turns gorge's recorded decisions into XMage's scripted answers.
func XAnswers(ds []rules.OracleDecision, steps int, modes map[string]int) [][]XAnswer {
	return xanswers(ds, steps, modes)
}

// XAnswersForScenario also uses the observed result of a compound may/pick.
func XAnswersForScenario(res rules.OracleResult, sc Scenario, modes map[string]int) [][]XAnswer {
	return xanswersForScenario(res, sc, modes)
}

// MayYes re-scripts every declined optional pick to take the first option.
func MayYes(sc Scenario, ds []rules.OracleDecision) (Scenario, bool) { return mayYes(sc, ds) }

// PlaysThrough replays sc and reports whether gorge performed every step
// and ended with an empty stack.
func PlaysThrough(reg *cards.Registry, sc Scenario) (rules.OracleResult, bool) {
	return playsThrough(reg, sc)
}

// Settle returns how many resolve steps empty the stack after sc (at most
// 4); ok is false when gorge cannot play sc.
func Settle(reg *cards.Registry, sc Scenario) (int, rules.OracleResult, bool) { return settle(reg, sc) }

// Baseline adds the opposing creature and library top every cast scenario
// offers to "up to one" asks and library searches.
func Baseline(setup map[string]Seat, f *cards.Face) { baseline(setup, f) }

// SearchesLibrary reports whether any of the face's abilities searches a
// library.
func SearchesLibrary(f *cards.Face) bool { return searchesLibrary(f) }

// HasType reports whether the face has a type, case-insensitively.
func HasType(f *cards.Face, t string) bool { return hasType(f, t) }

// WithHand puts name first in the seat's hand.
func WithHand(s Seat, name string) Seat { return withHand(s, name) }

// Repeat is n copies of s.
func Repeat(s string, n int) []string { return repeat(s, n) }
