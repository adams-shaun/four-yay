// Package oraclegen is the shared core of the level-A oracle scenario
// generator: the scenario types, fixtures, gorge-side settling and the
// XMage answer scripting. Each template (play a land, cast and resolve,
// counter a spell) lives in its own file under oraclegen/templates with its
// own version; templates.Generate picks one per card (spec
// 2026-10-02-xmage-compliance-oracle-design section 6; the split is
// 2026-10-03-rules-engine-lasagna-design section 11.3 C3). Scenarios use
// the rules/testdata/oracle schema, so gorge's runner and the XMage driver
// both replay them.
//
// The generator may run gorge to choose a fixture (which target to offer,
// how many resolves the stack needs). That does not bias the verdict: the
// expectation is XMage's snapshot, never gorge's.
package oraclegen

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules"
)

// xValue is the X every generated X spell is cast with.
const xValue = 2

// Seat is one player's setup.
type Seat struct {
	Battlefield []string `json:"battlefield,omitempty"`
	Tapped      []string `json:"tapped,omitempty"`
	// BackFace names battlefield cards setup places on their back face (face
	// index 1). It is emitted as a FlipFace event, so the placement replays
	// from the log like every other setup op. A name that is not also in
	// Battlefield is an error: only a permanent already on the battlefield can
	// be flipped.
	BackFace   []string `json:"back_face,omitempty"`
	Hand       []string `json:"hand,omitempty"`
	Graveyard  []string `json:"graveyard,omitempty"`
	Exile      []string `json:"exile,omitempty"`
	Library    []string `json:"library,omitempty"`
	LibraryTop []string `json:"library_top,omitempty"`
	// Counters puts counters on this seat's battlefield cards at setup:
	// card name -> counter kind (LOYALTY, P1P1) -> how many, added to what
	// the card enters with. Like Tapped it names every placement of that
	// card. A planeswalker's loyalty headroom and a "creature with a +1/+1
	// counter" target fixture ride it.
	Counters map[string]map[string]int32 `json:"counters,omitempty"`
	// Speed is the seat's starting speed, 0..4 (the runner's setup field).
	Speed int32 `json:"speed,omitempty"`
	// Life is the seat's starting life total when set (the runner's and the
	// XMage driver's setup "life"); nil keeps the format's starting life. An
	// activation gated on "at least N life" (Ayli's CheckSVar$ X |
	// SVarCompare$ GEY over Count$YourLifeTotal) rides it.
	Life *int32 `json:"life,omitempty"`
}

// WithCounters returns s with n more counters of kind on its card name. The
// map is copied, so a fixture's seat is never mutated through a shared map.
func WithCounters(s Seat, name, kind string, n int32) Seat {
	s.Counters = cloneCounters(s.Counters)
	if s.Counters == nil {
		s.Counters = map[string]map[string]int32{}
	}
	if s.Counters[name] == nil {
		s.Counters[name] = map[string]int32{}
	}
	s.Counters[name][kind] += n
	return s
}

func cloneCounters(m map[string]map[string]int32) map[string]map[string]int32 {
	if m == nil {
		return nil
	}
	out := make(map[string]map[string]int32, len(m))
	for card, kinds := range m {
		k := make(map[string]int32, len(kinds))
		for kind, n := range kinds {
			k[kind] = n
		}
		out[card] = k
	}
	return out
}

// Step is one scenario step (a subset of the runner's op set).
type Step struct {
	Op      string   `json:"op"`
	Seat    int      `json:"seat"`
	Card    string   `json:"card,omitempty"`
	Mana    string   `json:"mana,omitempty"`
	Targets []string `json:"targets,omitempty"`
	// CastMode selects a non-default cast option of the cast step by its
	// decision.Option.Mode (the runner's cast_mode): "optionalcost" pays a
	// self-spell OptionalCost static, "bargained" elects a Bargain, a Kicker's
	// "kicked", etc. Empty selects the ordinary cast, and it is empty on every
	// level-A step, so level-A items are byte-identical.
	CastMode string `json:"cast_mode,omitempty"`
	// Attackers/Defender drive the attack op; Blocks the block op. They
	// carry a creature into combat so a "target attacking or blocking
	// creature" slot has a legal target.
	Attackers []string    `json:"attackers,omitempty"`
	Defender  string      `json:"defender,omitempty"`
	Blocks    [][2]string `json:"blocks,omitempty"`
	// Step/Decision are the pass_to op's stop conditions (a phase step name
	// or a pending decision kind).
	Step     string   `json:"step,omitempty"`
	Active   string   `json:"active,omitempty"`
	Decision string   `json:"decision,omitempty"`
	Answers  []Answer `json:"answers,omitempty"`
	// AbilityIndex selects an activated ability by its IR index (the index
	// into the source's Face().Abilities), the anchor decision.Option.Ability
	// carries and rules' `activate` step resolves. It lets a level-B activate
	// step name an ability without pasting the script's SpellDescription
	// text. Nil on every level-A step, so level-A items are byte-identical.
	AbilityIndex *int `json:"ability_index,omitempty"`
	// Ability selects an `activate` option by its label (the runner's
	// oracleStep.Ability): a special action the engine offers under its own
	// option kind, such as a Room's "Unlock <door name>", has no IR ability
	// index. Empty on every existing step, so existing items are
	// byte-identical.
	Ability string `json:"ability,omitempty"`
	// A scenario step may move a card into a zone; the move op stamps the
	// object as having entered this turn (a board-history target such as
	// ThisTurnEntered@Graveyard needs that).
	To string `json:"to,omitempty"`
	// AttachedTo is the bearer ref for an attach setup operation.
	AttachedTo string `json:"attached_to,omitempty"`
	// Expect asserts an observation at this step (the runner's oracleExpect
	// vocabulary, rules/oracle_run.go). Nil or empty on every level-A step,
	// so level-A items are byte-identical. A generated scenario uses it to
	// hold gorge to a "nothing happens" static: with no state change, the
	// frozen snapshot has no field to compare, so the claim rides an
	// explicit expectation instead.
	Expect []Expect `json:"expect,omitempty"`
}

// Expect is one assertion attached to a Step. It is the subset of the
// runner's oracleExpect vocabulary a generated level-B scenario emits.
// TriggerOnStack names a source ref whose trigger must (Want true, the
// default) or must not (Want false) be a stack entry; StackSize pins the
// stack length. Both read as rules/oracle_run.go's oracleExpect fields do.
type Offered struct {
	Seat  int    `json:"seat"`
	Kind  string `json:"kind"`
	Card  string `json:"card"`
	Label string `json:"label,omitempty"`
}

type CanBlock struct {
	Blocker  string `json:"blocker"`
	Attacker string `json:"attacker"`
	// MaxBlockers asserts the MinMaxBlocker Max$ bound the (blocker,
	// attacker) pair's option carries; nil asserts no bound.
	MaxBlockers *int `json:"max_blockers,omitempty"`
}

// AttackRequired asserts whether Attacker's option at the pending
// declare-attackers decision carries the MustAttack requirement
// (rules/oracle_run.go's oracleAttackRequired).
type AttackRequired struct {
	Attacker string `json:"attacker"`
}

// CanAttack asserts whether Attacker is among the attackers the pending
// declare-attackers decision offers (rules/oracle_can_attack.go).
type CanAttack struct {
	Attacker string `json:"attacker"`
}

type Expect struct {
	TriggerOnStack string     `json:"trigger_on_stack,omitempty"`
	StackSize      *int       `json:"stack_size,omitempty"`
	Offered        *Offered   `json:"offered,omitempty"`
	CanBlock       *CanBlock  `json:"can_block,omitempty"`
	CanAttack      *CanAttack `json:"can_attack,omitempty"`
	// AttackRequired is the runner's per-attacker MustAttack requirement
	// assertion (rules oracleExpect.AttackRequired).
	AttackRequired *AttackRequired `json:"attack_required,omitempty"`
	// LookAtLibraryTop is the runner's per-seat "may look at the top card of
	// their library" assertion (rules oracleExpect.LookAtLibraryTop).
	LookAtLibraryTop map[string]bool `json:"look_at_library_top,omitempty"`
	// MaxHandSize is the runner's per-seat effective CR 514.1 maximum hand
	// size assertion (rules oracleExpect.MaxHandSize), a Continuous
	// SetMaxHandSize$ grant no snapshot field carries.
	MaxHandSize map[string]int `json:"max_hand_size,omitempty"`
	Want        *bool          `json:"want,omitempty"`
}

// Answer is a queued answer for gorge's runner (kind = decision kind).
type Answer struct {
	Kind string   `json:"kind"`
	Pick []string `json:"pick"`
}

// Scenario is one generated scenario in the runner's schema.
type Scenario struct {
	Name  string          `json:"name"`
	Turn  int             `json:"turn,omitempty"`
	CR    []string        `json:"cr"`
	Why   string          `json:"why"`
	Setup map[string]Seat `json:"setup"`
	// SetupAnswers answer decisions posed while the runner drives from
	// genesis to turn 1 (a permanent's "may begin the game" ask).
	SetupAnswers []Answer `json:"setup_answers,omitempty"`
	Steps        []Step   `json:"steps"`
}

// Item is one pipeline line: the scenario plus its identity. The XMage
// driver ignores the extra fields; Raw() strips them for gorge's runner.
//
// XAnswers scripts, per step, the answers XMage's strict choose mode needs
// for the decisions the step poses (targets of a trigger, a mode, a "may").
// They are derived from the decisions gorge's deterministic runner made, so
// both engines answer alike; a decision only one engine poses still shows
// up, as an XMage harness error or a gorge leftover.
type Item struct {
	ID       string `json:"id"`
	Card     string `json:"card"`
	Template string `json:"template"`
	// XMageName is the card's spelling in XMage's card database when it
	// differs from Card (the corpus spelling): Forge prints "Dáin Ironfoot",
	// XMage stores "Dain Ironfoot". The XMage driver adds and casts the card
	// under XMageName and rewrites its snapshots back to Card, so both sides
	// name it alike. Empty means the two spellings are equal.
	XMageName string      `json:"xmage_name,omitempty"`
	XAnswers  [][]XAnswer `json:"xmage_answers,omitempty"`
	// XAbility is parallel to Scenario.Steps: entry i is the XMage rule-text
	// prefix of step i's activated ability ("{T}", "Equip {2}"), or "" for a
	// step that is not an activate. It lives on the Item, not the Step,
	// because rules' runner decodes steps with DisallowUnknownFields and
	// would reject an unknown per-step field. Empty for every level-A item.
	XAbility []string `json:"xmage_ability,omitempty"`
	// XTargetSkips is parallel to Scenario.Steps: entry i lists the cast
	// step's completely omitted optional XMage target objects, each with the
	// offset in Targets where the driver queues its "[target_skip]" (see
	// XTargetSkip). Like XAbility it is an Item field, never a Step one, so
	// Raw() and gorge's runner never see it. Nil for every item that is not
	// the supported independent-0..1 shape, which keeps those bytes unchanged
	// and the driver on its legacy trailing skip.
	XTargetSkips [][]XTargetSkip `json:"xmage_target_skips,omitempty"`
	// Ignore names snapshot fields the comparison leaves out for this
	// scenario: library_top after the card shuffles a library.
	Ignore []string `json:"ignore,omitempty"`
	// Compare opts a snapshot field into the comparison. Empty (omitempty)
	// keeps a level-A item byte-identical.
	Compare []string `json:"compare,omitempty"`
	Scenario
}

// XAnswer is one scripted XMage answer: Kind target (Value a card name, a
// seat "pN", or "[target_skip]"), mode (Value the 1-based mode number), or
// choice (Value "yes"/"no" or an option label).
type XAnswer struct {
	Seat  int    `json:"seat"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Raw is the scenario alone, as gorge's runner decodes it.
func (it Item) Raw() []byte {
	b, _ := json.Marshal(it.Scenario)
	return b
}

// Skip explains why a card got no scenario.
type Skip struct {
	Card   string `json:"card"`
	Reason string `json:"reason"`
}

// mayYes queues, on the step that posed it, an answer taking option 0 for
// every decision gorge's fallback left empty although it offered options.
func mayYes(sc Scenario, ds []rules.OracleDecision) (Scenario, bool) {
	out := sc
	out.Steps = append([]Step(nil), sc.Steps...)
	changed := false
	for _, d := range ds {
		if d.Step < 0 || d.Step >= len(out.Steps) || d.Options == 0 || d.First == "" ||
			d.Via == "target" || d.Via == "answer" || (d.GorgeKind != "choose" && d.GorgeKind != "target") {
			continue
		}
		st := out.Steps[d.Step]
		st.Answers = append(append([]Answer(nil), st.Answers...), Answer{Kind: d.GorgeKind, Pick: []string{d.First}})
		out.Steps[d.Step] = st
		changed = true
	}
	return out, changed
}

// searchPicks queues, on the hidden-library-search step that posed it, an
// answer taking the search's first eligible card. Gorge's deterministic
// fallback declines an "up to N" hidden search (CR 701.19b fail-to-find),
// and XMage poses a mandatory library search (Terramorphic Expanse's
// TargetCardInLibrary 1..1) that rejects the resulting [target_skip] with
// "Wrong skip command found". A declined search whose option list is
// non-empty has a legal card, so re-running with the first eligible card
// makes both engines find the same card. An optional "you may search"
// shape poses a search_confirm yes/no first; when that confirm was declined
// no search pick is posed, so there is nothing to queue, and when it was
// accepted the search was actually made, so forcing its pick agrees with
// XMage too. The answer is only kept when the replay consumes it (see the
// caller's PlaysThrough guard), so a search XMage never reaches falls back
// to the decline pair.
func searchPicks(sc Scenario, ds []rules.OracleDecision) (Scenario, bool) {
	out := sc
	out.Steps = append([]Step(nil), sc.Steps...)
	changed := false
	for _, d := range ds {
		if d.Step < 0 || d.Step >= len(out.Steps) || d.Options == 0 || d.First == "" ||
			d.Kind != "choose_n" || d.Resume != "search" || len(d.Picks) != 0 {
			continue
		}
		st := out.Steps[d.Step]
		st.Answers = append(append([]Answer(nil), st.Answers...), Answer{Kind: d.GorgeKind, Pick: []string{d.First}})
		out.Steps[d.Step] = st
		changed = true
	}
	return out, changed
}

// playsThrough replays sc exactly and reports whether gorge performed
// every step and ended with an empty stack. Both leftover targets and
// answers are failures here: callers must rewrite targets to gorge's actual
// picks before replaying the generated scenario.
func playsThrough(reg *cards.Registry, sc Scenario) (rules.OracleResult, bool) {
	res, ok := probeTargets(reg, sc)
	return res, ok && len(res.Fails) == 0
}

// probeTargets runs a preliminary fixture that may over-offer targets. Only
// unused-target failures are excused here, before chooseTargets replaces the
// fixture slots; a reversed cast, or any other failure, is never a success.
func probeTargets(reg *cards.Registry, sc Scenario) (rules.OracleResult, bool) {
	b, _ := json.Marshal(sc)
	res, err := rules.RunOracleScenarioJSON(reg, b)
	if err != nil || len(res.Snapshots) != len(sc.Steps)+1 || castAborted(sc, res.Snapshots, res.Transcript) {
		return res, false
	}
	for _, f := range res.Fails {
		if !strings.Contains(f, rules.OracleUnusedTargetMarker) {
			return res, false
		}
	}
	return res, len(res.Snapshots[len(res.Snapshots)-1].Stack) == 0
}

type charmMode struct{ svar, label string }

// CharmCombination is one legal, ordered set of mode picks and their target
// chains. Combinations follow Choices$ order; repeated picks appear only when
// CanRepeatModes$ is true.
type CharmCombination struct {
	Modes []CharmMode
	Slots []Slot
}

// CharmCombinations enumerates legal combinations from the minimum pick count
// upward, in Choices$ order within each size. Ordinary charms default to one
// pick, preserving their historical ordering.
func CharmCombinations(f *cards.Face) []CharmCombination {
	modes := charmModes(f)
	if len(modes) == 0 {
		return nil
	}
	pickCount, minCount, repeat := 1, 1, false
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" || sa.API != "Charm" {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(sa.Params["CharmNum"])); err == nil && n > 0 {
			pickCount = n
		}
		if n, err := strconv.Atoi(strings.TrimSpace(sa.Params["MinCharmNum"])); err == nil && n > 0 {
			minCount = n
		} else if strings.TrimSpace(sa.Params["MinCharmNum"]) == "" {
			minCount = pickCount
		}
		repeat = strings.EqualFold(strings.TrimSpace(sa.Params["CanRepeatModes"]), "True")
		break
	}
	var out []CharmCombination
	var selected []CharmMode
	// emit appends the current selection as one combination.
	emit := func() {
		combo := CharmCombination{Modes: append([]CharmMode(nil), selected...)}
		for _, mode := range selected {
			combo.Slots = append(combo.Slots, ChainSlotSpecs(f, mode.svar)...)
		}
		out = append(out, combo)
	}
	// allDistinct reports whether the current selection repeats no mode.
	allDistinct := func() bool {
		seen := make(map[string]bool, len(selected))
		for _, mode := range selected {
			if seen[mode.svar] {
				return false
			}
			seen[mode.svar] = true
		}
		return true
	}
	// visit enumerates combinations of count modes from start. distinct
	// constrains the walk to strictly increasing positions (repeat-free
	// combinations only); otherwise positions may repeat, and the leaf
	// skips any all-distinct selection so the two walks do not overlap.
	var visit func(start, count int, distinct bool)
	visit = func(start, count int, distinct bool) {
		if len(selected) == count {
			if !distinct && allDistinct() {
				return
			}
			emit()
			return
		}
		for i := start; i < len(modes); i++ {
			selected = append(selected, modes[i])
			next := i
			if distinct {
				next++
			}
			visit(next, count, distinct)
			selected = selected[:len(selected)-1]
		}
	}
	// Within each pick count the repeat-free combinations come first, in
	// Choices$ order; only a CanRepeatModes$ charm then adds the repeating
	// ones. A non-repeat charm takes the single distinct walk, so its output
	// is byte-identical to the historical one.
	for count := minCount; count <= pickCount; count++ {
		visit(0, count, true)
		if repeat {
			visit(0, count, false)
		}
	}
	return out
}

// charmModes lists the spell's charm modes in Choices$ order.
func charmModes(f *cards.Face) []charmMode {
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		if sa.API != "Charm" {
			return nil
		}
		var out []charmMode
		for _, name := range strings.Split(sa.Params["Choices"], ",") {
			if name = strings.TrimSpace(name); name != "" {
				out = append(out, charmMode{name, effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)})
			}
		}
		return out
	}
	return nil
}

// targetZone is the zone a target filter draws from: an explicit TgtZone$,
// else a non-battlefield Origin$, else Stack for a slot that names a spell
// or ability on the stack (TargetType$ Spell/SpellAbility/Activated/
// Triggered, or ValidTgts$ with inZoneStack).
//
// charmMode selects the narrower stack rule a charm's SVar mode needs: a
// mode whose ValidTgts$ also names a battlefield type (Icy Reception's
// "creature or legendary spell", Theorix Charm's "noncreature card") keeps
// its battlefield fixture, because the historical generator read it that
// way and its verdict is pinned to the battlefield scenario. A top-level
// Counter (Precise Redaction, Countersculpt) is routed before this by
// targetsSpell, so its broader reading is unchanged.
func targetZone(params map[string]string, charmMode bool) string {
	if z := params["TgtZone"]; z != "" {
		return z
	}
	if o := params["Origin"]; o != "" {
		// An Origin$ that names the battlefield alone is the default zone
		// and stays unsuffixed (no slot change for the common case); an
		// Origin$ that adds Stack ("Battlefield,Stack") is kept so the
		// slot records both alternatives.
		if !strings.Contains(o, "Battlefield") || strings.Contains(o, "Stack") {
			return o
		}
	}
	if charmMode {
		if charmModeTargetsStack(params) {
			return "Stack"
		}
		return ""
	}
	if abilityTargetsStack(params) {
		return "Stack"
	}
	return ""
}

// zoneNamesStack reports whether a TgtZone$ list names Stack (it may be a
// comma-separated combo such as "Stack,Battlefield").
func zoneNamesStack(z string) bool {
	for _, part := range strings.Split(z, ",") {
		if strings.EqualFold(strings.TrimSpace(part), "Stack") {
			return true
		}
	}
	return false
}

// abilityTargetsStack reports whether an ability's target is a spell or
// ability on the stack, judged from its target vocabulary: a "Spell"-
// family TargetType$, or a ValidTgts$ naming the inZoneStack zone. An
// explicit non-stack zone (TgtZone$ / Origin$) wins, so a card targeting an
// instant card in a graveyard is not mistaken for a stack target.
func abilityTargetsStack(params map[string]string) bool {
	if strings.Contains(params["ValidTgts"], "inZoneStack") {
		return true
	}
	if params["TargetType"] == "" {
		return false
	}
	for _, part := range strings.Split(params["TargetType"], ",") {
		base := strings.SplitN(strings.TrimSpace(part), ".", 2)[0]
		switch base {
		case "Spell", "SpellAbility", "Activated", "Triggered", "Instant", "Sorcery":
			return true
		}
	}
	return false
}

// charmModeTargetsStack is abilityTargetsStack narrowed for a charm SVar
// mode: an ability-type TargetType$ (SpellAbility/Activated/Triggered) is
// always stack-only, but a Spell/Instant/Sorcery target is routed to the
// stack only when its ValidTgts$ cannot be read as a battlefield permanent.
// A mode whose ValidTgts$ names a battlefield base type (Creature, Card, ...)
// keeps the battlefield fixture its committed verdict was generated for.
func charmModeTargetsStack(params map[string]string) bool {
	if strings.Contains(params["ValidTgts"], "inZoneStack") {
		return true
	}
	if params["TargetType"] == "" {
		return false
	}
	for _, part := range strings.Split(params["TargetType"], ",") {
		base := strings.SplitN(strings.TrimSpace(part), ".", 2)[0]
		switch base {
		case "SpellAbility", "Activated", "Triggered":
			return true
		}
	}
	for _, part := range strings.Split(params["TargetType"], ",") {
		base := strings.SplitN(strings.TrimSpace(part), ".", 2)[0]
		switch base {
		case "Spell", "Instant", "Sorcery":
			return !validTgtsNamesBattlefield(params["ValidTgts"])
		}
	}
	return false
}

// validTgtsBattlefieldBases are the ValidTgts$ base types that the
// battlefield candidate table can serve. A filter that names any of them is
// satisfiable on the battlefield, so a charm mode's Spell-family
// TargetType$ on such a filter is not forced onto the stack.
var validTgtsBattlefieldBases = map[string]bool{
	"any": true, "creature": true, "player": true, "opponent": true,
	"permanent": true, "card": true, "artifact": true, "enchantment": true,
	"land": true, "planeswalker": true,
}

// validTgtsNamesBattlefield reports whether a ValidTgts$ filter names a
// battlefield-card base type. An inZoneStack qualifier wins (the card is on
// the stack), and only the first alternative is consulted -- the fixture
// builder serves a filter from its first alternative's shape. An empty
// filter names no type and is never battlefield-satisfiable here.
func validTgtsNamesBattlefield(validTgts string) bool {
	if strings.Contains(validTgts, "inZoneStack") {
		return false
	}
	first := strings.SplitN(validTgts, ",", 2)[0]
	base := strings.ToLower(strings.SplitN(strings.TrimSpace(first), ".", 2)[0])
	return validTgtsBattlefieldBases[base]
}

// SlotIsStack reports whether a target filter (as TargetSlots/ChainSlots
// encode it) draws only from the stack. A slot that also names a non-stack
// zone ("Stack,Battlefield") is served by the ordinary fixture -- the
// battlefield candidate -- so it is not a stack slot.
func SlotIsStack(filter string) bool {
	i := strings.LastIndexByte(filter, '@')
	if i < 0 {
		return false
	}
	z := filter[i+1:]
	if !zoneNamesStack(z) {
		return false
	}
	for _, part := range strings.Split(z, ",") {
		if p := strings.TrimSpace(part); p != "" && !strings.EqualFold(p, "Stack") {
			return false
		}
	}
	return true
}

// firstThreeTurnScenarioTurn recognizes the compiled first-three-turns cast
// lockout. Fixtures cast as seat 0 in a two-seat game, so global turn 2*4-1
// is that seat's fourth turn (TurnsTaken >= 4), when the lockout ends.
func firstThreeTurnScenarioTurn(f *cards.Face) (int, bool) {
	if f == nil {
		return 0, false
	}
	for _, st := range f.Statics {
		if st.Mode != "CantBeCast" || st.Params["SVarCompare"] != "LE3" {
			continue
		}
		v := st.Params["CheckSVar"]
		if body, ok := f.SVars[v]; ok {
			v = body
		}
		if v == "Count$YourTurns" {
			return 7, true
		}
	}
	return 0, false
}

// NewItem names a template's scenario. The template's version is part of
// the id and the scenario name, so bumping one template's version stales
// only that template's verdicts (compliance/oraclegen/templates).
func NewItem(f *cards.Face, card, template string, version int, sc Scenario) Item {
	if turn, ok := firstThreeTurnScenarioTurn(f); ok {
		sc.Turn = turn
	}
	sc.Name = fmt.Sprintf("gen%d-%s", version, template)
	sc.CR = []string{"601.2"}
	sc.Why = "generated level-A scenario"
	it := Item{ID: fmt.Sprintf("%s/%s/v%d", card, template, version), Card: card, Template: template, Scenario: sc}
	if CanShuffleLibrary(f) {
		it.Compare = []string{CompareNoLibraryOrder}
	}
	return it
}

// baseline gives every cast scenario something for "up to one target"
// triggers and library searches to find: XMage poses those decisions even
// with no legal choice (and strict mode then needs a scripted skip), while
// gorge skips them. An opposing creature and a varied library top make
// both engines ask.
func baseline(setup map[string]Seat, f *cards.Face) {
	p1 := setup["p1"]
	has := false
	for _, n := range p1.Battlefield {
		if n == "Grizzly Bears" {
			has = true
		}
	}
	if !has {
		p1.Battlefield = append(p1.Battlefield, "Grizzly Bears")
	}
	setup["p1"] = p1
	if searchesLibrary(f) {
		p0 := setup["p0"]
		p0.LibraryTop = []string{"Jace Beleren", "Grizzly Bears", "Forest", "Glorious Anthem", "Shock", "Plains", "Ornithopter"}
		setup["p0"] = p0
	}
}

func searchesLibrary(f *cards.Face) bool {
	for _, sa := range f.Abilities {
		if sa.Kind == "SP" && sa.API == "Discover" {
			// Discover (CR 701.57) exiles from the top of the library until a
			// nonland card is found, so the scenario must seed a discoverable
			// card; otherwise gorge finds none and no Treasures are made while
			// XMage, whose library is not empty, creates them.
			return true
		}
		if strings.Contains(sa.Line, "Origin$ Library") {
			return true
		}
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "Origin$ Library") {
			return true
		}
	}
	for _, t := range f.Triggers {
		if t.Effect != nil && strings.Contains(t.Effect.Line, "Origin$ Library") {
			return true
		}
	}
	return false
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func withHand(s Seat, name string) Seat {
	s.Hand = append([]string{name}, s.Hand...)
	return s
}

// settle runs the cast in gorge and returns how many resolve steps empty
// the stack (at most 4); ok is false when gorge cannot cast with this
// fixture.
func settle(reg *cards.Registry, sc Scenario) (int, rules.OracleResult, bool) {
	for n := 1; n <= 4; n++ {
		try := sc
		try.Steps = append(append([]Step(nil), sc.Steps...), make([]Step, n)...)
		for i := len(sc.Steps); i < len(try.Steps); i++ {
			try.Steps[i] = Step{Op: "resolve"}
		}
		b, _ := json.Marshal(try)
		res, err := rules.RunOracleScenarioJSON(reg, b)
		if err != nil || len(res.Snapshots) == 0 {
			return 0, res, false
		}
		for _, f := range res.Fails {
			// The static fixture over-offers targets on purpose; the generator
			// rewrites each cast step to gorge's actual picks afterwards, and
			// verifies the rewrite with PlaysThrough. So the runner's
			// unused-target self-check is expected here and is not a reason to
			// reject the fixture; every other step/harness fail is.
			if strings.Contains(f, rules.OracleUnusedTargetMarker) {
				continue
			}
			if strings.HasPrefix(f, "step ") || strings.Contains(f, "harness:") {
				return 0, res, false
			}
		}
		if castAborted(try, res.Snapshots, res.Transcript) {
			continue
		}
		last := res.Snapshots[len(res.Snapshots)-1]
		if len(res.Snapshots) == len(try.Steps)+1 && len(last.Stack) == 0 {
			return n, res, true
		}
	}
	return 0, rules.OracleResult{}, false
}

// castAborted rejects a scenario where the runner reports a cast abort or a
// cast step leaves its card in its origin hand zone. CR 601.2c/733.1 reverses
// an illegal cast; treating the resulting empty stack as a successful settle
// would publish a scenario that never cast the named spell.
func castAborted(sc Scenario, snaps []rules.OracleSnapshot, transcript []string) bool {
	for _, line := range transcript {
		if strings.Contains(strings.ToLower(line), "cast aborted") {
			return true
		}
	}
	if len(snaps) != len(sc.Steps)+1 {
		return false
	}
	for i, st := range sc.Steps {
		if st.Op != "cast" {
			continue
		}
		seatRef, name, ok := strings.Cut(st.Card, ":")
		if !ok || !strings.HasPrefix(seatRef, "p") {
			continue
		}
		var seat int
		if _, err := fmt.Sscanf(seatRef, "p%d", &seat); err != nil || seat < 0 || seat >= len(snaps[i].Players) {
			continue
		}
		before := countName(snaps[i].Players[seat].Hand, name)
		after := countName(snaps[i+1].Players[seat].Hand, name)
		if before > 0 && after >= before {
			return true
		}
	}
	return false
}

func countName(names []string, want string) int {
	n := 0
	for _, name := range names {
		if name == want {
			n++
		}
	}
	return n
}

// chooseTargets rewrites the target-carrying step's Targets from the target
// decisions gorge's deterministic runner actually made, in order. A
// target-carrying step is a cast (XMage's castSpell) or an activated
// ability's activate step (activateAbility): both take the step's Targets
// directly. The static fixture only promises a legal candidate per slot; it
// over-offers -- an "up to N" slot gorge declines, a token slot with no token
// on the board, a slot in a mixed chain -- and XMage rejects a target list
// whose count does not match the ability's, so the scenario must carry
// exactly gorge's picks (the target decision's PickRefs, in order).
//
// castSteps is the set of step indices whose targets were taken from such a
// step (the steps held on the Item): xanswers sends those through castSpell
// or activateAbility, not through a scripted target answer, while a target
// decision posed during a resolve step still needs an XMage answer. A step
// that posed no target decision (every slot skipped, or an ability with no
// targets) has its fixture targets cleared, so the surplus never reaches
// XMage. Level-A scenarios hold only cast steps, so their bytes and
// castSteps are unchanged.
func chooseTargets(sc Scenario, ds []rules.OracleDecision) (Scenario, map[int]bool) {
	out := sc
	out.Steps = append([]Step(nil), sc.Steps...)
	// carriesTargets is the op set whose own targets XMage consumes: cast
	// through castSpell, activate through activateAbility.
	carriesTargets := func(op string) bool { return op == "cast" || op == "activate" }
	chosen := map[int][]string{}
	castSteps := map[int]bool{}
	for _, d := range ds {
		if d.Via != "target" || d.Step < 0 || d.Step >= len(out.Steps) || !carriesTargets(out.Steps[d.Step].Op) {
			continue
		}
		chosen[d.Step] = append(chosen[d.Step], d.PickRefs...)
		castSteps[d.Step] = true
	}
	for i := range out.Steps {
		if !carriesTargets(out.Steps[i].Op) {
			continue
		}
		if _, ok := castSteps[i]; ok {
			out.Steps[i].Targets = chosen[i]
			continue
		}
		// A cast that posed no target decision: drop the fixture's surplus.
		out.Steps[i].Targets = nil
	}
	return out, castSteps
}

// xanswers turns gorge's recorded decisions into XMage's scripted answers,
// grouped by the step that posed them.
func xanswers(ds []rules.OracleDecision, steps int, modes map[string]int, castSteps map[int]bool) [][]XAnswer {
	out := make([][]XAnswer, steps)
	any := false
	// A step whose card name is then searched for (Ancient Vendetta's "choose
	// a card name. Search ... for cards with that name"): XMage poses the name
	// dialog however narrowly gorge offered it, and the search must find it.
	namedSearch := map[int]bool{}
	for _, d := range ds {
		if pickKind(d, 0) == "search" {
			namedSearch[d.Step] = true
		}
	}
	routing := newAnswerRouting(ds)
	// Setup ETB replacement choices (colour or creature type) are answered
	// before XMage places the seeded permanents, so they must LEAD step zero's
	// answer stream: the driver queues them before build() and XMage's as-enters
	// dialog consumes the head of the choice queue. Collect them apart rather
	// than appending in decision order, which would put a step-0 gameplay
	// answer that happens to precede the setup decision (Lifecraft Engine's
	// crew pick) in front of the as-enters dialog. Prepended after the loop.
	var setupAnswers []XAnswer
	for i, d := range ds {
		if d.Step < 0 {
			if IsSetupChoice(d) {
				setupAnswers = append(setupAnswers, XAnswer{d.Seat, "setup_choice", d.Picks[0]})
				any = true
			}
			continue
		}
		if d.Step >= steps || (d.Via == "target" && castSteps[d.Step]) {
			// A cast step's own targets reach XMage through castSpell; a
			// target decision posed at a resolve step is scripted below.
			continue
		}
		if as, owned := routing.route(i); owned {
			if len(as) > 0 {
				out[d.Step] = append(out[d.Step], as...)
				any = true
			}
			continue
		}
		if pickKind(d, 0) == "name" && namedSearch[d.Step] && len(d.Picks) == 1 {
			out[d.Step] = append(out[d.Step], XAnswer{d.Seat, "choice", d.Picks[0]})
			any = true
			continue
		}
		if forcedSingleOption(d) && !forcedChoicePosed(ds, i) {
			// A forced one-option ask: XMage does not pose it. A forced
			// target-kind pick is the exception -- one legal opponent is still
			// a chooseTarget XMage asks for -- and so is a mode ask, which
			// XMage keeps posing even when only one mode is affordable.
			continue
		}
		var as []XAnswer
		switch d.Kind {
		case "target":
			if d.Resume == "trig_sub" && d.Options == 1 && d.Min == 1 && d.Max == 1 {
				// A CR 603.3d chain link's forced single target (Mechanical
				// Mobster's "target creature you control" with only itself):
				// XMage picks it without asking, so a scripted answer is left
				// unused (measured on the std pass).
				continue
			}
			as = targetDecisionAnswers(routing, i)
		case "mode":
			if d.Resume == "play" {
				// Optional Play effects (Discover, Cascade, and other carriers)
				// are XMage chooseUse asks, not numeric mode choices. Gorge's
				// recorded pick means cast; no pick means decline to hand.
				yes := len(d.Picks) > 0
				as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[yes]})
				break
			}
			if (d.Resume == "unless_pay" || d.Resume == "unless_decline") && unlessPolarity(d) == "no" {
				// An unless-pay (UnlessCost$) is XMage's boolean chooseUse,
				// not a mode ask; the engine models it as a one-option mode
				// decision whose label is the decline ("Don't pay"). The
				// driver routes only the literals "yes"/"no" to setChoice
				// (boolean), so the raw label reaches setChoice(String) and
				// never answers the ask (std3: Dispelling Exhale, Spectral
				// Denial). Only the DECLINE is answered this way: a paid unless
				// cost (Lithobraking, Rottenmouth Viper, Meathook Massacre II)
				// agrees with XMage only as the ordinary mode pick below.
				as = append(as, XAnswer{d.Seat, "choice", "no"})
				break
			}
			if pickKind(d, 0) == "discard" || d.Resume == "discard" {
				// Gorge's discard card picker is KModes; XMage uses
				// TargetDiscard.choose -> makeChoose (not chooseMode). A
				// discard among same-named cards needs the exact-object
				// discriminator, exactly as the ordinary choice path does.
				for k, ref := range d.PickRefs {
					// discardLabel is the card name XMage's TargetDiscard matches;
					// the pick LABEL is often "Discard <name>", which would not
					// match. Only the exact-object discriminator is added.
					as = append(as, XAnswer{d.Seat, "choice", disambiguatedObjectChoice(d, k, oraclediffRefName(ref))})
				}
				if d.Max > len(d.PickRefs) {
					as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
				}
				break
			}
			// gorge offers only the modes with legal targets, so an option
			// index is not always the mode number; the label is when it names
			// a charm mode. A label outside the charm map is still a mode ask
			// for a non-charm modal (a plain modal), so fall back to the
			// option's 1-based position; the discard-style picker that shares
			// the kind is routed above.
			for k, i := range d.PickIdx {
				if m, ok := modeNumberFor(d, k, modes); ok && m == ModeChoiceQueue {
					// A DB$ GenericChoice | SetChosenMode$ True body (a
					// Theros-style Siege): XMage reads the pick through its
					// ChooseModeEffect -> controller.choose(Outcome.Neutral,
					// Choice, game), the CHOICE queue, showing the option
					// LABEL ("Abzan", "Khans") -- never a numeric mode. The
					// label is what gorge's mode decision already carries.
					as = append(as, XAnswer{d.Seat, "choice", d.Picks[k]})
					continue
				}
				if m, ok := modeNumberFor(d, k, modes); ok && (m == ModeYesQueue || m == ModeNoQueue) {
					// A Timetwister-style per-player "may shuffle" GenericChoice
					// (Turtles in Time): XMage asks chooseUse, so the answer is
					// yes/no for whichever seat chose.
					as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[m == ModeYesQueue]})
					continue
				}
				n := i + 1
				if m, ok := modeNumberFor(d, k, modes); ok {
					n = m
				}
				as = append(as, XAnswer{d.Seat, "mode", fmt.Sprint(n)})
			}
			if d.Max > len(d.Picks) {
				// XMage keeps choosing modes up to Max; stop it with the mode
				// queue's skip token (including when gorge picked none).
				as = append(as, XAnswer{d.Seat, "mode", "[mode_skip]"})
			}
		case "yesno":
			yes := len(d.PickIdx) > 0 && d.PickIdx[0] == 0
			if len(d.Picks) > 0 {
				l := strings.ToLower(d.Picks[0])
				yes = strings.HasPrefix(l, "yes") || strings.HasPrefix(l, "accept") || strings.HasPrefix(l, "pay") || l == "true"
			}
			as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[yes]})
		case "choose_n":
			if d.Resume == "damage_split" {
				if routing.ownsSplit(i) {
					// The shares were emitted at the target ask's own queue
					// position (before that ask's chain skips); emitting them
					// here as well would duplicate every "^X=" answer.
					continue
				}
				// Divided damage: the engine's split ask repeats one option index
				// per damage assigned (Fury, Forked Bolt, Twin Bolt). XMage's
				// chooseTargetAmount consumes one "<ref>^X=<share>" per chosen
				// target on the target queue, not makeChoose choices.
				as = damageSplitAnswers(d)
				break
			}
			if d.Resume == "mana_color" && d.Min == d.Max && d.Max > 1 {
				// A multi-amount allocation (Combo Any, Desolation of Smaug):
				// one unit per picked option, options laid out unit*5+colour
				// over WUBRG. XMage poses one multi-amount message per colour
				// and needs every one filled, zeroes included.
				as = manaAllocationAnswers(d)
				break
			}
			if payment(d.Picks) {
				// Hybrid/phyrexian halves are payment UI; XMage pays from
				// the pool without asking.
				continue
			}
			if n := colourPickCount(d); n > 1 && n == len(d.Picks) {
				// A multi-unit "any combination of colors" mana ability
				// (Baxter Building's "Add four mana in any combination of
				// colors", Amount$ 4) is not one XMage colour dialog. XMage
				// asks a multi-amount distribution among colours, so a joined
				// "^" colour selection throws "Missing choice in multi
				// amount". Emit nothing and let XMage distribute it itself,
				// as it did before this routing existed.
				continue
			}
			if len(d.Picks) == 1 && strings.HasPrefix(d.Picks[0], "X = ") {
				as = append(as, XAnswer{d.Seat, "choice", "X=" + strings.TrimPrefix(d.Picks[0], "X = ")})
				break
			}
			if len(d.Picks) == 1 && pickKind(d, 0) == "number" {
				if _, err := strconv.Atoi(d.Picks[0]); err == nil {
					// "Choose a number": XMage's getAmount reads an "X=<n>"
					// choice, exactly like announceX (TestPlayer.getAmount).
					as = append(as, XAnswer{d.Seat, "choice", "X=" + d.Picks[0]})
					break
				}
			}
			if yn, ok := yesNo(d); ok {
				as = append(as, XAnswer{d.Seat, "choice", yn})
				break
			}
			if d.Resume == "sacrifice" && d.Min == 0 && len(d.Picks) > 0 {
				// An accepted optional sacrifice ("any opponent may sacrifice
				// a creature"): XMage asks chooseUse before the pick
				// (DesecrationDemon.java:77, DoIfCostPaid), so the pick needs
				// a "yes" ahead of it. The decline already scripts "no".
				as = append(as, XAnswer{d.Seat, "choice", "yes"})
			}
			if len(d.Picks) > 1 && allChoiceQueue(d) {
				// One makeChoose dialog consumes ONE definition, whose own
				// parser splits on '^' into the multi-card selection (Dig's
				// "put two of them into your hand", a discard-two). Emitting
				// one answer per pick would let each pick answer a separate
				// dialog and leave the rest as an unused leftover.
				as = append(as, XAnswer{d.Seat, "choice", JoinedChoice(d)})
			} else {
				for k, label := range d.Picks {
					switch pickQueue(d, k, label) {
					case "skip":
						// XMage resolves this pick inside its computer player (a
						// library search) or pays it from the pool (a mana-tapping
						// cost); a scripted answer would only be an unused leftover.
						continue
					case "target":
						v := label
						if k < len(d.PickRefs) {
							v = d.PickRefs[k]
						}
						if isSeat(v) {
							as = append(as, XAnswer{d.Seat, "target", v})
							continue
						}
						// XMage's chooseTarget parses the same copy marker as
						// makeChoose, so a target among same-named objects needs
						// the discriminator too. An alias pick keeps its full ref:
						// the driver's targetName maps a bound ref to its @alias.
						if p := ClassifySameName(d, k); p.Alias != "" {
							as = append(as, XAnswer{d.Seat, "target", d.PickRefs[k]})
							continue
						}
						v = oraclediffRefName(v)
						_, marker := SameNameAmbiguity(d, k)
						as = append(as, XAnswer{d.Seat, "target", v + marker})
						continue
					}
					// The choice queue: makeChoose shows the option's label, which
					// for an unlabelled object pick is the object's name.
					if label == "" && k < len(d.PickRefs) {
						label = oraclediffRefName(d.PickRefs[k])
					}
					label = disambiguatedObjectChoice(d, k, label)
					if colour, ok := manaColourLabel(label); ok {
						label = colour
					}
					as = append(as, XAnswer{d.Seat, "choice", label})
				}
			}
			switch {
			case len(d.Picks) == 0:
				// Declined: XMage may pose it as a yes/no or as an "up to"
				// pick; script both (measured: Zimone's Experiment agrees
				// only with this pair).
				as = append(as, XAnswer{d.Seat, "choice", "no"}, XAnswer{d.Seat, "target", "[target_skip]"})
			case d.Max > len(d.Picks) && len(as) > 0 && as[len(as)-1].Kind == "target":
				// Fewer than "up to N" on the target queue: stop XMage
				// picking more.
				as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
			case d.Max > len(d.Picks) && len(as) > 0:
				// A short makeChoose; its queue has its own skip token.
				as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
			}
		case "order":
			if d.GorgeKind == "trigger_order" {
				// chooseTriggeredAbility compares the choice against the ability's
				// rule text (getRule) or its source's name, not gorge's
				// "<Source>: <text>" label, so drop the source prefix here.
				for _, label := range d.Picks {
					as = append(as, XAnswer{d.Seat, "choice", triggerRule(label)})
				}
				break
			}
			// Arrange first asks which cards move; its follow-up ordering is
			// also on XMage's choice queue. Keeping all cards is a choice skip.
			if d.GorgeKind == "arrange" {
				forcedOrder := d.Min == d.Max && d.Max == d.Options
				if !forcedOrder && len(d.PickIdx) == d.Options {
					// Keeping every card where it is: XMage's surveil/scry
					// selection is a TargetCard on the target queue, and a
					// skip dismisses it. Measured against XMage on the std
					// pass (Refute Destiny, Proctor of Potential, ... -- 25
					// cards that agree only with this answer); no ORDER
					// answer follows, XMage keeps the cards in place.
					as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
					break
				}
				if !forcedOrder {
					// A proper subset is selected on the choice queue,
					// then choice_skip terminates that dialog.
					for _, label := range d.Picks {
						as = append(as, XAnswer{d.Seat, "choice", label})
					}
					as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
				}
				// The subsequent ORDER prompt consumes one choice for each
				// kept card, in the order gorge selected them.
				for _, label := range d.Picks {
					as = append(as, XAnswer{d.Seat, "choice", label})
				}
			} else {
				continue
			}
		default:
			continue
		}
		if len(as) > 0 {
			out[d.Step] = append(out[d.Step], as...)
			any = true
		}
	}
	if !any {
		return nil
	}
	if len(setupAnswers) > 0 {
		// Setup answers are read from xmage_answers[0] before build(), even
		// when the scenario has no gameplay steps.
		if len(out) == 0 {
			out = append(out, nil)
		}
		out[0] = append(setupAnswers, out[0]...)
	}
	return out
}

// forcedSingleOption reports a one-option ask XMage never poses: exactly one
// legal answer (Min==Max==Options==1) that is not a target, order or mode ask.
// A target pick is still a chooseTarget XMage asks for even with one legal
// option, a mode ask is posed up to Max picks even when only one mode is
// affordable, and a min-0 single option is a real decline XMage still asks
// (Break Out, Destined Confrontation).
func forcedSingleOption(d rules.OracleDecision) bool {
	if d.Kind == "mode" && d.Resume == "generic_players" && d.Options == 1 && d.Min == 1 && d.Max == 1 {
		// A per-player punisher's forced one-mode ask (Rottenmouth Viper's
		// "you lose 4 life unless ..."): XMage does not pose it; the unless
		// choice that follows is the real decision.
		return true
	}
	if d.Kind == "target" || d.Kind == "order" || d.Kind == "mode" {
		return false
	}
	if hasTargetPick(d) {
		return false
	}
	return d.Options == 1 && d.Min == 1 && d.Max == 1
}

// forcedChoicePosed reports whether a forced one-option choose_n ask is still
// posed by XMage's makeChoose (Unstable Glyphbridge's "choose a creature").
// A ChangeTargets redirect shares the same decision shape (Resume "choice")
// but is not a choice ask: XMage retargets through chooseTarget, which
// auto-selects a sole candidate in non-strict mode and reads the target queue
// otherwise, so a scripted choice would never be consumed. The redirect is
// told apart by the earlier cast that targeted a stack object (Bolt Bend,
// Redirect Lightning).
func forcedChoicePosed(ds []rules.OracleDecision, i int) bool {
	d := ds[i]
	if d.Kind != "choose_n" || d.Resume != "choice" {
		return false
	}
	for _, e := range ds[:i] {
		if e.Kind != "target" || e.Step >= d.Step {
			continue
		}
		for k := range e.Picks {
			switch pickKind(e, k) {
			case "spell", "ability", "trigger":
				return false
			}
		}
	}
	return true
}

// unlessPolarity is the yes/no answer to an unless-pay boolean ask: the
// picked option's label says whether the cost was paid. A decline wording
// ("Don't pay", "Decline") is "no"; a payment wording is "yes".
func unlessPolarity(d rules.OracleDecision) string {
	if len(d.Picks) == 0 {
		return "no"
	}
	l := strings.ToLower(strings.TrimSpace(d.Picks[0]))
	for _, neg := range []string{"don't", "do not", "decline", "no", "refuse"} {
		if strings.HasPrefix(l, neg) {
			return "no"
		}
	}
	// Any other label names the cost being paid ("Pay {2}", "Sacrifice an
	// artifact").
	return "yes"
}

// modeNumberFor resolves the k-th pick's 1-based XMage mode number from its
// label. ok is false when the pick's label names no charm mode -- a
// non-charm modal or a discard-style picker sharing the "mode" kind; the
// caller then falls back to the option's own 1-based position. A label that
// names a SetChosenMode$ True GenericChoice returns ModeChoiceQueue, telling
// the caller to answer on XMage's choice queue instead of the mode queue.
func modeNumberFor(d rules.OracleDecision, k int, modes map[string]int) (int, bool) {
	if k < 0 || k >= len(d.Picks) {
		return 0, false
	}
	m, ok := modes[d.Picks[k]]
	return m, ok
}

// allChoiceQueue reports whether every pick in a decision reaches XMage's
// choice queue (makeChoose). A decision with a target or skipped pick is not
// one makeChoose dialog and must not be joined with '^'.
func allChoiceQueue(d rules.OracleDecision) bool {
	for k, label := range d.Picks {
		if xmQueue(pickKind(d, k), label) != "choice" {
			return false
		}
	}
	return true
}

// perPlayerTargetAnswers answers a TargetsForEachPlayer$ ask: one target for
// each seat in the match, in seat order starting at 0, using the pick that
// seat's controller made or a target skip when the seat chose none. XMage's
// ForEachPlayerTargetsAdjuster poses every seat's ask, even a seat with no
// legal candidate.
func perPlayerTargetAnswers(d rules.OracleDecision) []XAnswer {
	seatPick := map[int]string{}
	for _, ref := range d.PickRefs {
		s, ok := refSeat(ref)
		if !ok {
			continue
		}
		if _, dup := seatPick[s]; dup {
			continue
		}
		v := ref
		if !isSeat(ref) {
			v = oraclediffRefName(ref)
		}
		seatPick[s] = v
	}
	n := d.SeatCount
	if n < 1 {
		n = 1
	}
	out := make([]XAnswer, 0, n)
	for s := 0; s < n; s++ {
		if v, ok := seatPick[s]; ok {
			out = append(out, XAnswer{d.Seat, "target", v})
		} else if s == d.Seat && d.PerOpponent {
			// The controller's own seat without a pick: the corpus's
			// per-player targets are "for each opponent" (Celebrate the
			// Mountain-king, Omega, Riptide Gearhulk), which XMage never
			// asks the controller for -- a skip here would be consumed as
			// the opponent's answer (measured on the std pass).
			continue
		} else {
			out = append(out, XAnswer{d.Seat, "target", "[target_skip]"})
		}
	}
	return out
}

// refSeat returns the seat a scenario ref belongs to ("p1:Grizzly Bears" or
// "p1" -> 1). It is how a per-player target answer finds the seat whose ask
// the pick answers.
func refSeat(ref string) (int, bool) {
	if !strings.HasPrefix(ref, "p") {
		return 0, false
	}
	r := ref[1:]
	if i := strings.IndexByte(r, ':'); i >= 0 {
		r = r[:i]
	}
	n, err := strconv.Atoi(r)
	if err != nil {
		return 0, false
	}
	return n, true
}

// targetDecisionAnswers scripts one target decision. A TargetsForEachPlayer$
// ask is answered per seat, a DividedAsYouChoose$ ask in XMage's divided
// "<ref>^X=<share>" form, anything else one answer per pick. Every form is
// followed by a target skip for each "up to N" chain slot the engine settled
// without posing it (XMage still asks those), and preceded by one for each
// such slot settled BEFORE this ask (leadingUnposedSkips).
func targetDecisionAnswers(r *answerRouting, i int) []XAnswer {
	d := r.ds[i]
	as := leadingUnposedSkips(d)
	switch {
	case d.PerPlayer:
		// TargetsForEachPlayer$ (CR 601.2c): XMage asks one target
		// per player, in seat order starting at seat 0. Answer each
		// seat with the pick its controller made, or a target skip
		// when the seat chose none.
		as = append(as, perPlayerTargetAnswers(d)...)
	case d.Divided > 0 && len(d.PickRefs) > 0:
		// A TargetAmount slot (distribute counters, divided damage): XMage's
		// chooseTargetAmount takes one "<ref>^X=<share>" per target and
		// completes when the allocation is answered.
		as = append(as, dividedTargetAnswers(r, i)...)
	default:
		for k, ref := range d.PickRefs {
			if isSeat(ref) {
				as = append(as, XAnswer{d.Seat, "target", ref})
				continue
			}
			// XMage's chooseTarget parses the same copy marker as
			// makeChoose and filters on isCopy(), so a target that shares its
			// name with another offered object needs the discriminator here
			// too (a token copy of a card, Extravagant Replication). An alias
			// pick keeps its full ref, which the driver's targetName maps to
			// the object's @alias.
			if p := ClassifySameName(d, k); p.Alias != "" {
				as = append(as, XAnswer{d.Seat, "target", ref})
				continue
			}
			_, marker := SameNameAmbiguity(d, k)
			as = append(as, XAnswer{d.Seat, "target", oraclediffRefName(ref) + marker})
		}
		if len(d.PickRefs) < d.Max {
			// Fewer picks than the "up to N" ask allows: XMage keeps
			// asking, so stop it with the target queue's skip token. This
			// also covers a slot gorge never posed at all (zero picks).
			as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
		}
	}
	for n := 0; n < d.UnposedSlots; n++ {
		as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
	}
	return as
}

// dividedTargetAnswers answers a divided target ask with each pick's share of
// the total. A divided target ask with several recipients is paired with the
// engine's own damage_split decision (that split's recipients are exactly the
// ask's), whose shares are then emitted here -- at the ask's own queue
// position, ahead of the chain skips its caller appends. Counters have no such
// decision: the engine deals them round-robin over the picks.
func dividedTargetAnswers(r *answerRouting, i int) []XAnswer {
	d := r.ds[i]
	n := len(d.PickRefs)
	if j, ok := r.targetSplit[i]; ok {
		return damageSplitAnswers(r.ds[j])
	}
	as := make([]XAnswer, 0, n)
	for k, ref := range d.PickRefs {
		share := d.Divided / n
		if k < d.Divided%n {
			share++
		}
		if share == 0 {
			continue
		}
		// Keep the scenario ref (including #N), as the damage split does.
		as = append(as, XAnswer{d.Seat, "target", ref + "^X=" + strconv.Itoa(share)})
	}
	return as
}

// damageSplitAnswers turns a "damage_split" KChoose into one target answer
// per chosen target, its Value the ref plus "^X=<share>". The engine's split
// answer is a multiset over option indexes: a target receiving k damage has
// its index repeated k times, and PickIdx/Picks/PickRefs are parallel. Targets
// are emitted in first-appearance order of the option index, exactly as the
// engine assigns them.
func damageSplitAnswers(d rules.OracleDecision) []XAnswer {
	share := map[int]int{}
	var order []int
	for _, i := range d.PickIdx {
		if _, seen := share[i]; !seen {
			order = append(order, i)
		}
		share[i]++
	}
	ref := map[int]string{}
	for k, i := range d.PickIdx {
		if k < len(d.PickRefs) {
			ref[i] = d.PickRefs[k]
		}
	}
	as := make([]XAnswer, 0, len(order))
	for _, i := range order {
		name := ref[i]
		if name == "" {
			// A snapshot without refs: fall back to the option label, which
			// for a damage-split permanent is the card name.
			if i < len(d.Picks) {
				name = d.Picks[i]
			}
		}
		// Keep the scenario ref (including #N): XMage's targetName uses it
		// to resolve two same-name permanents to distinct setup aliases.
		as = append(as, XAnswer{d.Seat, "target", name + "^X=" + strconv.Itoa(share[i])})
	}
	return as
}

// manaAllocationAnswers turns a "mana_color" allocation (Min==Max>1) into one
// amount answer per WUBRG colour, zeroes included: XMage's
// getMultiAmountWithIndividualConstraints iterates the effect's manaSymbols in
// ColoredManaSymbol order and requires an "X=<n>" for each. A pick's colour is
// its option label ("Add W", the authority), falling back to its index mod 5
// (the effects/mana_effect.go layout: unit*5 + colourIndex).
func manaAllocationAnswers(d rules.OracleDecision) []XAnswer {
	counts := map[byte]int{}
	for k := range d.Picks {
		if code, ok := allocationColour(d, k); ok {
			counts[code]++
		}
	}
	as := make([]XAnswer, 0, 5)
	for _, code := range "WUBRG" {
		as = append(as, XAnswer{d.Seat, "amount", strconv.Itoa(counts[byte(code)])})
	}
	return as
}

// allocationColour names the WUBRG colour one picked option allocates.
func allocationColour(d rules.OracleDecision, k int) (byte, bool) {
	if k < len(d.Picks) {
		if label := d.Picks[k]; strings.HasPrefix(label, "Add ") && len(label) == 5 {
			if strings.IndexByte("WUBRG", label[4]) >= 0 {
				return label[4], true
			}
		}
	}
	if k < len(d.PickIdx) {
		return "WUBRG"[d.PickIdx[k]%5], true
	}
	return 0, false
}

// yesNo recognises a bare two-way boolean choice. The engine's option kind
// and exact label/ref identity must both agree; composed choices such as

// ModeChoiceQueue is the sentinel position modeNumbers stores for a mode
// label that reaches XMage through its CHOICE queue (controller.choose)
// rather than the numeric mode queue: the Choices$ label of a
// DB$ GenericChoice | SetChosenMode$ True body (the Theros-style Sieges).
// XMage's ChooseModeEffect does controller.choose(Outcome.Neutral, Choice,
// game), which shows the option LABEL; chooseMode/setModeChoice (the numeric
// queue) is never posed for it. A real charm position is 1-based, so 0 is
// never a valid mode number and is unambiguous.
const ModeChoiceQueue = 0

// ModeYesQueue and ModeNoQueue are the sentinels for the two labels of a
// DB$ GenericChoice | AILogic$ Timetwister body (Turtles in Time): a per-player
// "may shuffle your hand and graveyard" ask that XMage poses as
// player.chooseUse (the yes/no choice queue), never as a mode. Choices$ lists
// the yes label first, the no label second. Negative, so neither collides with
// a real 1-based charm position nor with ModeChoiceQueue.
const (
	ModeYesQueue = -1
	ModeNoQueue  = -2
)

// modeNumbers maps each charm mode's label (as gorge's mode decision
// shows it) to its 1-based position in its Choices$ list, for every Charm
// on the face -- the spell's own and any modal trigger's. It ALSO carries
// every DB$ GenericChoice | SetChosenMode$ True label, mapped to the
// ModeChoiceQueue sentinel: those picks are the same "mode" decision kind
// to gorge but a different XMage queue, and no in-band discriminator on the
// decision separates them (a mid-resolution Charm shares Resume "modes"),
// so the face shape is the authority.
func modeNumbers(f *cards.Face) map[string]int {
	out := map[string]int{}
	addCharm := func(choices string) {
		for i, name := range strings.Split(choices, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out[effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)] = i + 1
		}
	}
	for _, body := range f.SVars {
		p := svarParams(body)
		if p["DB"] != "GenericChoice" || !strings.EqualFold(p["AILogic"], "Timetwister") {
			continue
		}
		for i, name := range strings.Split(p["Choices"], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			sentinel := ModeNoQueue
			if i == 0 {
				sentinel = ModeYesQueue
			}
			out[effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)] = sentinel
		}
	}
	// The SetChosenMode labels first, so a real Charm mode of the same
	// label (a corpus impossibility) would keep its numeric position below.
	for _, body := range f.SVars {
		p := svarParams(body)
		if p["DB"] != "GenericChoice" || !strings.EqualFold(p["SetChosenMode"], "True") {
			continue
		}
		for _, name := range strings.Split(p["Choices"], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out[effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)] = ModeChoiceQueue
		}
	}
	for _, sa := range f.Abilities {
		if sa.API == "Charm" {
			addCharm(sa.Params["Choices"])
		}
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "Charm") {
			if c := svarParams(body)["Choices"]; c != "" {
				addCharm(c)
			}
		}
	}
	return out
}

// yesNo recognises a bare two-way boolean choice. The engine's option kind
// and exact label/ref identity must both agree; composed choices such as
// "Yes — discard" are ordinary makeChoose picks, not boolean answers. Older
// snapshots without PickKinds use the same exact-label/ref rule.
func yesNo(d rules.OracleDecision) (string, bool) {
	if d.Options != 2 || len(d.Picks) != 1 || len(d.PickRefs) != 1 {
		return "", false
	}
	switch kind := pickKind(d, 0); kind {
	case "yes", "no":
		// The engine's own boolean option ("Yes — shuffle", a may
		// trigger): XMage's chooseUse.
		return kind, true
	case "altaddcost", "gift_decline", "gift_promise", "primary":
		// An optional additional cost, a gift promise and a two-way
		// primary/secondary pick are chooseUse asks in XMage too
		// (measured: Silence the Echo, Kitnap, Lost in Space agree only
		// with a yes/no answer). Option 0 is the "do it" side.
		l := strings.ToLower(d.Picks[0])
		for _, neg := range []string{"do not", "don't", "no", "decline", "skip"} {
			if strings.HasPrefix(l, neg) {
				return "no", true
			}
		}
		if len(d.PickIdx) == 1 && d.PickIdx[0] == 0 {
			return "yes", true
		}
		return "no", true
	case "":
		// A snapshot without PickKinds: only an exact Yes/No label.
		label := strings.ToLower(strings.TrimSpace(d.Picks[0]))
		if d.Picks[0] == d.PickRefs[0] && (label == "yes" || label == "no") {
			return label, true
		}
	}
	return "", false
}

// hasTargetPick reports whether any picked option reaches XMage's target
// queue (a real TargetXxx), which XMage poses even when the engine offered
// exactly one legal option.
func hasTargetPick(d rules.OracleDecision) bool {
	for k := range d.Picks {
		switch pickKind(d, k) {
		case "permanent", "player", "opponent_choice":
			return true
		}
	}
	return false
}

// pickKind is the engine option kind of the k-th pick. A snapshot written
// before PickKinds existed returns "", which every caller treats as the
// choice queue.
func pickKind(d rules.OracleDecision, k int) string {
	if k >= 0 && k < len(d.PickKinds) {
		if (d.Resume == "opp_pick" || d.Resume == "choice") && d.PickKinds[k] == "player" {
			// The TargetingPlayer$ Opponent flow's controller-facing
			// which-opponent ask: XMage's ChoicePlayer, the choice queue.
			return "opponent_choice"
		}
		return d.PickKinds[k]
	}
	return ""
}

// xmQueue says which TestPlayer queue an engine option kind reaches. The
// choice queue is makeChoose/setChoice -- XMage's choose(Cards, TargetCard),
// choose(Choice) and choose(ChoicePlayer) all land there. The target queue is
// addTarget/chooseTarget, reached by a real TargetXxx. "skip" is for a pick
// XMage never asks TestPlayer about: a library search (TestPlayer.searchLibrary
// delegates to the computer player, exactly as doSurveil does) and a
// mana-tapping cost (paid from the pool). The engine option kind, not the
// label text, is the authority -- the mechanism the census test pins.
func xmQueue(kind, label string) string {
	switch kind {
	case "search", "exilecost":
		// A library search (TargetCardInLibrary) and an exile-from-graveyard
		// cost (TargetCardInYourGraveyard) are answered from the target
		// queue: measured on the std pass, 47 search carriers (Shared Roots,
		// Nature's Rhythm, Solemn Simulacrum, ...) and Feed the Cycle /
		// Soaring Stoneglider agree only with a target answer.
		return "target"
	case "trigger_cost_pay":
		return "skip"
	case "mana":
		if _, ok := manaColourLabel(label); ok {
			// A mana ability's "add one mana of any colour" pick is a real
			// choice dialog; only a mana-tapping cost is paid silently.
			return "choice"
		}
		return "skip"
	case "permanent", "player":
		return "target"
	case "opponent_choice":
		return "choice"
	}
	return "choice"
}

// triggerRule drops gorge's "<SourceName>: " prefix from a trigger-order
// label: XMage's chooseTriggeredAbility compares its choice against the
// ability's rule text (getRule), which carries no source prefix.
func triggerRule(label string) string {
	if i := strings.Index(label, ": "); i >= 0 {
		return label[i+2:]
	}
	return label
}

// manaColourLabel maps gorge's "Add W" mana option to the colour name
// XMage's colour chooser shows (its Choice key is "White", not "Add W"). A
// colour option of a costed mana ability names the cost first ("Pay 1: Add W",
// "Pay 2 life: Add W": pay.ManaAbilityCostPrefix), and XMage asks the same
// colour dialog for it, so the cost prefix is dropped before matching.
func manaColourLabel(label string) (string, bool) {
	if i := strings.LastIndex(label, ": "); i >= 0 {
		label = label[i+2:]
	}
	if strings.HasPrefix(label, "Add ") && len(label) == 5 {
		return manaColour(label[4])
	}
	return "", false
}

// colourPickCount counts the picks of a decision that name a single mana
// colour (a mana ability's colour option, with or without a cost prefix).
// When every pick of a multi-pick decision is such an option, XMage models
// the ask as a multi-amount distribution among colours, not one colour
// dialog, so the decision must not emit a joined "^" colour selection.
func colourPickCount(d rules.OracleDecision) int {
	n := 0
	for k, label := range d.Picks {
		if label == "" && k < len(d.PickRefs) {
			label = oraclediffRefName(d.PickRefs[k])
		}
		if _, ok := manaColourLabel(label); ok {
			n++
		}
	}
	return n
}

func manaColour(code byte) (string, bool) {
	switch code {
	case 'W':
		return "White", true
	case 'U':
		return "Blue", true
	case 'B':
		return "Black", true
	case 'R':
		return "Red", true
	case 'G':
		return "Green", true
	default:
		return "", false
	}
}

func payment(picks []string) bool {
	if len(picks) == 0 {
		return false
	}
	for _, p := range picks {
		if !strings.HasPrefix(p, "Pay ") {
			return false
		}
		// "Pay 1: Add W" is a costed mana ability's colour pick, a real
		// XMage colour dialog, not a hybrid/phyrexian payment half.
		if _, colour := manaColourLabel(p); colour {
			return false
		}
	}
	return true
}

func isSeat(s string) bool {
	return len(s) >= 2 && s[0] == 'p' && strings.Trim(s[1:], "0123456789") == ""
}

// SameNamePick describes one object pick that shares its name with another
// offered object, and the XMage answer that selects it exactly.
//
// Ambiguous is set only when the pick and a sibling are DISTINCT objects
// (their scenario refs differ). Two options that resolve to the same ref are
// the same object -- a repeated trigger source, a dual-mode index -- and
// XMage's name match already selects it, so they are not ambiguous and need
// no discriminator.
//
// At most one of CopyMarker and Alias is set:
//
//   - CopyMarker ("[only copy]"/"[no copy]") is XMage's isCopy() filter. It
//     is used only when it leaves EXACTLY ONE candidate: a token among
//     same-named cards, or a card among same-named tokens. Its spelling
//     carries no space before the bracket; TestPlayer.makeChoose strips
//     exactly 9/11 characters.
//   - Alias ("@<ref>") is the driver's exact-object form
//     (TestPlayer.hasObjectTargetNameOrAlias): a bound alias names one
//     object and no other. It is used when the copy filter is not unique --
//     two cards, two tokens, or a card and a token that share a name -- and
//     the scenario ref is therefore the only thing that separates them.
//     registerAliases binds a setup ref to its object; the driver also binds
//     the ref of a mid-game permanent (a token copy) before the ask.
//
// A pick with neither (a bare name) is NOT uniquely identified and is the
// defect the same-name census counts as unresolved.
type SameNamePick struct {
	Ambiguous  bool
	Distinct   int    // distinct same-name offered refs, pick included
	CopyMarker string // "[only copy]"/"[no copy]" when the copy filter is unique
	Alias      string // "@<ref>" when only the exact ref separates the candidates
	// Unanswerable is set when the copy filter is not unique AND the pick's
	// ref is not an identity XMage can bind (a rank-derived library/hand
	// card). There is no sound answer; the census counts the item as
	// unproven rather than resolved.
	Unanswerable bool
}

// pickRefExact reports whether pick k's ref is an identity XMage's alias
// binding reconstructs exactly: a token, or a setup battlefield permanent.
// A rank-derived ref (an anonymous library/hand card) is not. A hand-built
// decision that omits the provenance list is treated as exact (its refs are
// synthesized identity fixtures, not live ranks).
func pickRefExact(d rules.OracleDecision, k int) bool {
	if k >= 0 && k < len(d.PickRefsInexact) {
		return !d.PickRefsInexact[k]
	}
	return true
}

// ClassifySameName reports how pick k of d is disambiguated.
func ClassifySameName(d rules.OracleDecision, k int) SameNamePick {
	if k >= len(d.PickRefs) || !strings.Contains(d.PickRefs[k], ":") {
		return SameNamePick{}
	}
	ref := d.PickRefs[k]
	name := oraclediffRefName(ref)
	pickToken := strings.Contains(ref, ":token:")
	distinct := map[string]bool{}
	sameKind := 0
	for _, candidate := range d.OptionRefs {
		if !strings.EqualFold(oraclediffRefName(candidate), name) {
			continue
		}
		distinct[candidate] = true
		if strings.Contains(candidate, ":token:") == pickToken {
			sameKind++
		}
	}
	if len(distinct) < 2 {
		// The pick's name is unambiguous, or every same-named option is the
		// same object (a repeated trigger source), which a bare name still
		// selects exactly.
		return SameNamePick{}
	}
	p := SameNamePick{Ambiguous: true, Distinct: len(distinct)}
	if sameKind == 1 {
		// Exactly one candidate passes the copy filter: the token-or-card
		// distinction is unique, so the bare name plus the marker selects it.
		if pickToken {
			p.CopyMarker = "[only copy]"
		} else {
			p.CopyMarker = "[no copy]"
		}
		return p
	}
	// Two or more candidates of the same kind (or a card and a token plus a
	// same-kind sibling): the copy filter leaves more than one. Only the
	// pick's own scenario ref identifies it, and only when XMage can bind
	// that ref by identity. A rank-derived ref would bind the WRONG object
	// (or nothing), so it is not emitted: the item is unanswerable.
	if !pickRefExact(d, k) {
		p.Unanswerable = true
		return p
	}
	p.Alias = "@" + ref
	return p
}

// InNameMatchMechanism reports whether pick k of d reaches an XMage name /
// scenario-ref match at all. Excluded are the mechanisms that do NOT:
//
//   - an arrange order (Surveil/Scry/Dig "put them back"): XMage shows the
//     looked-at cards as an ordering list; two identical basics are
//     interchangeable and the compared snapshot cannot tell which one moved,
//     so disambiguating changes nothing.
//   - a library search (pickKind search, or a dig/hideaway pick): XMage's
//     computer player resolves it and the card's identity among identical
//     same-named basics does not change the compared state.
//
// It is the outer scope of the same-name census. IsNameSelection additionally
// drops a pick no exact answer can identify (an inexact ref whose copy filter
// leaves several same-kind candidates).
func InNameMatchMechanism(d rules.OracleDecision, k int) bool {
	if d.Kind == "order" {
		return false
	}
	if pickKind(d, k) == "search" {
		return false
	}
	switch d.Resume {
	case "dig", "search", "dig_arrange", "hideaway_arrange":
		return false
	}
	return true
}

// IsNameSelection reports whether pick k of d is answered by an object name
// or scenario ref that XMage's own name matching consumes (makeChoose,
// chooseTarget, TargetDiscard) AND an exact answer exists. It is the scope
// of the same-name census: every in-scope ambiguous pick must resolve, and a
// pick with no exact answer (ClassifySameName.Unanswerable) is counted as
// unproven, never as resolved.
func IsNameSelection(d rules.OracleDecision, k int) bool {
	return InNameMatchMechanism(d, k) && !ClassifySameName(d, k).Unanswerable
}

// SameNameAmbiguity is ClassifySameName's boolean summary: whether the pick
// shares its name with a distinct sibling, and the copy marker when that
// filter is unique (empty for an alias pick). It is the compatibility form
// the census used before Alias existed; new callers use ClassifySameName.
func SameNameAmbiguity(d rules.OracleDecision, k int) (ambiguous bool, marker string) {
	p := ClassifySameName(d, k)
	return p.Ambiguous, p.CopyMarker
}

// namesObject reports whether a choice LABEL is an object-name answer (or
// empty, meaning "use the object's name"). An activated ability or mana
// choice carries an action label ("Activate Llanowar Elves for mana") that
// XMage's makeChoose matches literally, exactly like an endure choice
// (TestYesNoRequiresAnEngineBoolean); replacing it with the object's scenario
// ref would answer a different question. The exact-ref alias is an
// object-NAME answer, so it is only correct when the label names the object.
func namesObject(label, ref string) bool {
	if label == "" || label == ref || strings.HasPrefix(label, "@") {
		return true
	}
	return strings.EqualFold(label, oraclediffRefName(ref))
}

// ExactRefAlias is the "@ref" answer that names decision d's pick k when its
// name alone matches more than one offered object, or "" when the name is
// unambiguous (ClassifySameName). Cost paths that script a batch as one answer
// per pick use it so each single binds the exact object, as JoinedChoice does
// for the joined answer.
func ExactRefAlias(d rules.OracleDecision, k int) string {
	if k >= len(d.Picks) || k >= len(d.PickRefs) {
		return ""
	}
	p := ClassifySameName(d, k)
	if p.Alias != "" && namesObject(d.Picks[k], d.PickRefs[k]) {
		return p.Alias
	}
	return ""
}

// JoinedChoice is the one "^"-joined answer XAnswersForScenario scripts for a
// multi-pick decision that one makeChoose dialog consumes: each pick spelled as
// XMage's dialog lists it (a same-name object by its exact ref, a mana colour
// by its colour name). Callers that drop or match that answer use it, so they
// see the same spelling the scenario carries.
func JoinedChoice(d rules.OracleDecision) string {
	labels := make([]string, 0, len(d.Picks))
	for k, label := range d.Picks {
		if label == "" && k < len(d.PickRefs) {
			label = oraclediffRefName(d.PickRefs[k])
		}
		label = disambiguatedObjectChoice(d, k, label)
		if colour, ok := manaColourLabel(label); ok {
			label = colour
		}
		labels = append(labels, label)
	}
	return strings.Join(labels, "^")
}

// disambiguatedObjectChoice returns the choice-queue answer for pick k: the
// exact-ref alias when the copy marker cannot separate the candidates AND the
// label is an object-name answer, else the label with the copy marker
// appended (no space before the bracket).
func disambiguatedObjectChoice(d rules.OracleDecision, k int, label string) string {
	p := ClassifySameName(d, k)
	if p.Alias != "" && k < len(d.PickRefs) && namesObject(label, d.PickRefs[k]) {
		return p.Alias
	}
	return label + p.CopyMarker
}

// oraclediffRefName strips a scenario ref to the object name.
func oraclediffRefName(ref string) string {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 0 && strings.HasPrefix(n, "p") {
		n = n[i+1:]
	}
	n = strings.TrimPrefix(n, "token:")
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}

func hasType(f *cards.Face, t string) bool {
	for _, x := range f.Types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

// poolFor turns a Forge mana cost ("2 R R", "W/U", "X G") into the exact
// pool letters that pay it: generic as colorless, hybrid as its first half.
func poolFor(cost string) (string, string) {
	cost = strings.TrimSpace(cost)
	if cost == "" || strings.EqualFold(cost, "no cost") {
		return "", "no mana cost"
	}
	var b strings.Builder
	for _, sym := range strings.Fields(cost) {
		switch {
		case sym == "0":
		case strings.Trim(sym, "0123456789") == "":
			var n int
			fmt.Sscanf(sym, "%d", &n)
			b.WriteString(strings.Repeat("C", n))
		case len(sym) == 1 && strings.Contains("WUBRGC", sym):
			b.WriteString(sym)
		case len(sym) == 2 && strings.Contains("WUBRG", sym[:1]) && strings.Contains("WUBRG", sym[1:]):
			// Forge spells hybrid {W/B} as "WB": pay the first half.
			b.WriteString(sym[:1])
		case len(sym) == 2 && sym[0] == '2' && strings.Contains("WUBRG", sym[1:]):
			// {2/W}: pay the coloured half.
			b.WriteString(sym[1:])
		case strings.Contains(sym, "/"):
			h := strings.Split(sym, "/")
			switch {
			case strings.EqualFold(h[1], "P"):
				b.WriteString(h[0])
			case len(h[0]) == 1 && strings.Contains("WUBRGC", h[0]):
				b.WriteString(h[0])
			case strings.Trim(h[0], "0123456789") == "":
				// {2/W}: pay the coloured half.
				b.WriteString(h[1])
			default:
				return "", "mana symbol " + sym
			}
		case sym == "X":
			// X = xValue, scripted for both engines (Generate).
			b.WriteString(strings.Repeat("C", xValue))
		default:
			return "", "mana symbol " + sym
		}
	}
	return b.String(), ""
}

// playerTargetHead reports whether a ValidTgts$ filter names a player rather
// than a card. A zone qualifier (Origin$/TgtZone$) describes where an effect
// finds its cards, so it must never be appended to a player target: doing so
// turned "Opponent" into "Opponent@Hand" (Cruelclaw's Heist, Ruthless
// Negotiation, Soul Search, Aggressive Negotiations), which no fixture can
// satisfy. The head is the first comma-separated alternative before its first
// '.' predicate.
func playerTargetHead(filter string) bool {
	head := strings.ToLower(strings.SplitN(strings.Split(filter, ",")[0], ".", 2)[0])
	return head == "player" || head == "opponent"
}

// openingHandAnswers declines the "you may begin the game with this card" ask
// for a K:MayEffectFromOpeningHand card. The runner's setup fallback otherwise
// takes option 0 ("Yes"), which starts the card on the battlefield and makes
// the later cast step "not offered"; the scenario wants the card in hand.
func openingHandAnswers(f *cards.Face) []Answer {
	if _, ok := f.KeywordParam("MayEffectFromOpeningHand"); ok {
		return []Answer{{Kind: "choose", Pick: []string{"no"}}}
	}
	return nil
}

// pickQueue is xmQueue for the k-th pick, except that a search of ANOTHER
// player's library (Ancient Vendetta's "search target opponent's ... library")
// reaches XMage's choice queue: only a search of your own library is the
// TargetCardInLibrary target ask (measured on the std pass).
func pickQueue(d rules.OracleDecision, k int, label string) string {
	q := xmQueue(pickKind(d, k), label)
	if q == "target" && pickKind(d, k) == "search" && k < len(d.PickRefs) {
		if s, ok := refSeat(d.PickRefs[k]); ok && s != d.Seat {
			return "choice"
		}
	}
	return q
}
