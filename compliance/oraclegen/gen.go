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
	Hand        []string `json:"hand,omitempty"`
	Graveyard   []string `json:"graveyard,omitempty"`
	Exile       []string `json:"exile,omitempty"`
	Library     []string `json:"library,omitempty"`
	LibraryTop  []string `json:"library_top,omitempty"`
}

// Step is one scenario step (a subset of the runner's op set).
type Step struct {
	Op      string   `json:"op"`
	Seat    int      `json:"seat"`
	Card    string   `json:"card,omitempty"`
	Mana    string   `json:"mana,omitempty"`
	Targets []string `json:"targets,omitempty"`
	Answers []Answer `json:"answers,omitempty"`
}

// Answer is a queued answer for gorge's runner (kind = decision kind).
type Answer struct {
	Kind string   `json:"kind"`
	Pick []string `json:"pick"`
}

// Scenario is one generated scenario in the runner's schema.
type Scenario struct {
	Name  string          `json:"name"`
	CR    []string        `json:"cr"`
	Why   string          `json:"why"`
	Setup map[string]Seat `json:"setup"`
	Steps []Step          `json:"steps"`
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
	// Ignore names snapshot fields the comparison leaves out for this
	// scenario: library_top after the card shuffles a library.
	Ignore []string `json:"ignore,omitempty"`
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

// playsThrough replays sc exactly and reports whether gorge performed
// every step and ended with an empty stack.
func playsThrough(reg *cards.Registry, sc Scenario) (rules.OracleResult, bool) {
	b, _ := json.Marshal(sc)
	res, err := rules.RunOracleScenarioJSON(reg, b)
	if err != nil || len(res.Fails) > 0 || len(res.Snapshots) != len(sc.Steps)+1 {
		return res, false
	}
	return res, len(res.Snapshots[len(res.Snapshots)-1].Stack) == 0
}

type charmMode struct{ svar, label string }

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

// chainSlots lists the target filters along one SVar ability chain.
func chainSlots(f *cards.Face, svar string) []string {
	var out []string
	for name := svar; name != ""; {
		params := svarParams(f.SVars[name])
		if v := params["ValidTgts"]; v != "" {
			if z := targetZone(params, true); z != "" {
				v += "@" + z
			}
			out = append(out, v)
		}
		name = params["SubAbility"]
	}
	return out
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

// NewItem names a template's scenario. The template's version is part of
// the id and the scenario name, so bumping one template's version stales
// only that template's verdicts (compliance/oraclegen/templates).
func NewItem(card, template string, version int, sc Scenario) Item {
	sc.Name = fmt.Sprintf("gen%d-%s", version, template)
	sc.CR = []string{"601.2"}
	sc.Why = "generated level-A scenario"
	return Item{ID: fmt.Sprintf("%s/%s/v%d", card, template, version), Card: card, Template: template, Scenario: sc}
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
			if strings.HasPrefix(f, "step ") || strings.Contains(f, "harness:") {
				return 0, res, false
			}
		}
		last := res.Snapshots[len(res.Snapshots)-1]
		if len(res.Snapshots) == len(try.Steps)+1 && len(last.Stack) == 0 {
			return n, res, true
		}
	}
	return 0, rules.OracleResult{}, false
}

// xanswers turns gorge's recorded decisions into XMage's scripted answers,
// grouped by the step that posed them.
func xanswers(ds []rules.OracleDecision, steps int, modes map[string]int) [][]XAnswer {
	out := make([][]XAnswer, steps)
	any := false
	for _, d := range ds {
		if d.Step < 0 || d.Step >= steps || d.Via == "target" {
			// A step's own targets reach XMage through castSpell.
			continue
		}
		if d.Options <= 1 && d.Kind != "target" && d.Kind != "order" && !hasTargetPick(d) && !(d.Kind == "mode" && pickKind(d, 0) == "discard") {
			// A forced one-option ask: XMage does not pose it. A forced
			// target-kind pick is the exception -- one legal opponent is still
			// a chooseTarget XMage asks for.
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
			for _, ref := range d.PickRefs {
				v := ref
				if !isSeat(ref) {
					v = oraclediffRefName(ref)
				}
				as = append(as, XAnswer{d.Seat, "target", v})
			}
			if len(d.PickRefs) == 0 {
				as = append(as, XAnswer{d.Seat, "target", "[target_skip]"})
			}
		case "mode":
			if pickKind(d, 0) == "discard" {
				// Gorge's discard card picker is KModes; XMage uses
				// TargetDiscard.choose -> makeChoose (not chooseMode).
				for _, ref := range d.PickRefs {
					as = append(as, XAnswer{d.Seat, "choice", oraclediffRefName(ref)})
				}
				if d.Max > len(d.PickRefs) {
					as = append(as, XAnswer{d.Seat, "choice", "[choice_skip]"})
				}
				break
			}
			// gorge offers only the modes with legal targets, so an option
			// index is not the mode number; the label is.
			for k, i := range d.PickIdx {
				n := i + 1
				if k < len(d.Picks) {
					if m, ok := modes[d.Picks[k]]; ok {
						n = m
					}
				}
				as = append(as, XAnswer{d.Seat, "mode", fmt.Sprint(n)})
			}
		case "yesno":
			yes := len(d.PickIdx) > 0 && d.PickIdx[0] == 0
			if len(d.Picks) > 0 {
				l := strings.ToLower(d.Picks[0])
				yes = strings.HasPrefix(l, "yes") || strings.HasPrefix(l, "accept") || strings.HasPrefix(l, "pay") || l == "true"
			}
			as = append(as, XAnswer{d.Seat, "choice", map[bool]string{true: "yes", false: "no"}[yes]})
		case "choose_n":
			if payment(d.Picks) {
				// Hybrid/phyrexian halves are payment UI; XMage pays from
				// the pool without asking.
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
			for k, label := range d.Picks {
				switch xmQueue(pickKind(d, k), label) {
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
					if !isSeat(v) {
						v = oraclediffRefName(v)
					}
					as = append(as, XAnswer{d.Seat, "target", v})
					continue
				}
				// The choice queue: makeChoose shows the option's label, which
				// for an unlabelled object pick is the object's name.
				if label == "" && k < len(d.PickRefs) {
					label = oraclediffRefName(d.PickRefs[k])
				}
				if colour, ok := manaColourLabel(label); ok {
					label = colour
				}
				as = append(as, XAnswer{d.Seat, "choice", label})
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
	return out
}

// modeNumbers maps each charm mode's label (as gorge's mode decision
// shows it) to its 1-based position in its Choices$ list, for every Charm
// on the face -- the spell's own and any modal trigger's.
func modeNumbers(f *cards.Face) map[string]int {
	out := map[string]int{}
	add := func(choices string) {
		for i, name := range strings.Split(choices, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out[effects.CharmModeLabel(cards.ResolveSVar(f.SVars, name), name)] = i + 1
		}
	}
	for _, sa := range f.Abilities {
		if sa.API == "Charm" {
			add(sa.Params["Choices"])
		}
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "Charm") {
			if c := svarParams(body)["Choices"]; c != "" {
				add(c)
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
		case "permanent", "player":
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
		if d.Resume == "opp_pick" && d.PickKinds[k] == "player" {
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
// XMage's colour chooser shows (its Choice key is "White", not "Add W").
func manaColourLabel(label string) (string, bool) {
	if strings.HasPrefix(label, "Add ") && len(label) == 5 {
		return manaColour(label[4])
	}
	return "", false
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
	}
	return true
}

func isSeat(s string) bool {
	return len(s) >= 2 && s[0] == 'p' && strings.Trim(s[1:], "0123456789") == ""
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

// targetSlots lists the ValidTgts$ filters along the card's spell ability
// chain (permanent spells have none), in the order the cast asks for them.
func targetSlots(f *cards.Face) []string {
	var out []string
	add := func(params map[string]string) {
		v := params["ValidTgts"]
		if v == "" {
			return
		}
		if z := targetZone(params, false); z != "" {
			v += "@" + z
		}
		out = append(out, v)
	}
	for _, sa := range f.Abilities {
		if sa.Kind != "SP" {
			continue
		}
		if sa.API == "Charm" {
			// The runner and XMage both take the first mode; its chain
			// carries the targets.
			if first := strings.TrimSpace(strings.Split(sa.Params["Choices"], ",")[0]); first != "" {
				for name := first; name != ""; {
					params := svarParams(f.SVars[name])
					add(params)
					name = params["SubAbility"]
				}
			}
			break
		}
		for s := sa; s != nil; s = s.Sub {
			add(s.Params)
		}
		break
	}
	return out
}

// FaceHasFixture reports whether the static fixture builder can satisfy every
// target the card's cast demands: each slot has at least one candidate (a
// stack-only slot is coverable by a precast spell). The second return names
// the first unsatisfiable slot, for the census. This is a static scan -- it
// runs no game -- so the census ratchet can scan the whole corpus.
func FaceHasFixture(f *cards.Face) (bool, string) {
	plans := [][]string{targetSlots(f)}
	if modes := charmModes(f); len(modes) > 0 {
		plans = nil
		for _, m := range modes {
			plans = append(plans, chainSlots(f, m.svar))
		}
	}
	for _, slots := range plans {
		for _, s := range slots {
			if SlotIsStack(s) {
				continue
			}
			if len(candidatesFor(s)) == 0 {
				return false, s
			}
		}
	}
	return true, ""
}

// svarParams splits an SVar ability body ("DB$ Pump | ValidTgts$ ...")
// into its params.
func svarParams(body string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(body, "|") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "$")
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

type fixture struct {
	p0, p1  Seat
	targets []string
}

// candidates for one target filter, most generic first. Each puts the
// target on the board and names it.
type cand struct {
	seat, zone, card string // seat "p0"/"p1", zone, card; card "" = the player
}

func candidatesFor(filter string) []cand {
	zone := ""
	if i := strings.LastIndexByte(filter, '@'); i >= 0 {
		filter, zone = filter[:i], strings.ToLower(filter[i+1:])
	}
	if zone != "" && zone != "battlefield" {
		if strings.Contains(zone, "battlefield") {
			// A mixed zone (Stack,Battlefield, or Origin$ Battlefield,Stack)
			// is served on the battlefield.
			zone = ""
		} else {
			return zoneCandidates(filter, zone)
		}
	}
	alt := strings.Split(filter, ",")
	base := strings.ToLower(strings.SplitN(alt[0], ".", 2)[0])
	mine := strings.Contains(filter, "YouCtrl") || strings.Contains(filter, "YouOwn")
	opp := "p1"
	if mine {
		opp = "p0"
	}
	creatures := []cand{{opp, "battlefield", "Grizzly Bears"}, {opp, "battlefield", "Serra Angel"}, {opp, "battlefield", "Ornithopter"}, {opp, "battlefield", "Llanowar Elves"}, {opp, "battlefield", "Hill Giant"}}
	switch base {
	case "any":
		return append(creatures, cand{"p1", "", ""})
	case "creature":
		return creatures
	case "player", "opponent":
		if strings.Contains(filter, "You") && !strings.Contains(filter, "Opp") {
			return []cand{{"p0", "", ""}}
		}
		return []cand{{"p1", "", ""}, {"p0", "", ""}}
	case "permanent", "card":
		if strings.Contains(filter, "Graveyard") {
			break
		}
		return append(creatures, cand{opp, "battlefield", "Glorious Anthem"}, cand{opp, "battlefield", "Forest"})
	case "artifact":
		return []cand{{opp, "battlefield", "Ornithopter"}, {opp, "battlefield", "Sol Ring"}}
	case "enchantment":
		return []cand{{opp, "battlefield", "Glorious Anthem"}}
	case "land":
		return []cand{{opp, "battlefield", "Forest"}}
	case "planeswalker":
		return []cand{{opp, "battlefield", "Jace Beleren"}}
	case "instant", "sorcery":
		return []cand{{opp, "graveyard", "Shock"}, {"p0", "graveyard", "Shock"}}
	}
	return nil
}

// zoneCandidates offers cards in a non-battlefield zone (TgtZone$): the
// owner from YouOwn/OppOwn, else both seats.
func zoneCandidates(filter, zone string) []cand {
	if strings.Contains(zone, ",") {
		zone = strings.Split(zone, ",")[0]
	}
	switch zone {
	case "graveyard", "exile", "hand":
	default:
		return nil
	}
	seats := []string{"p0", "p1"}
	switch {
	case strings.Contains(filter, "YouOwn") || strings.Contains(filter, "YouCtrl"):
		seats = []string{"p0"}
	case strings.Contains(filter, "OppOwn") || strings.Contains(filter, "OppCtrl"):
		seats = []string{"p1"}
	}
	var out []cand
	for _, c := range []string{"Grizzly Bears", "Serra Angel", "Shock", "Llanowar Elves", "Glorious Anthem", "Ornithopter", "Forest", "Duress"} {
		for _, st := range seats {
			out = append(out, cand{st, zone, c})
		}
	}
	return out
}

// fixtures is the cross product of every slot's candidates, capped.
func fixtures(slots []string) []fixture {
	out := []fixture{{}}
	for _, s := range slots {
		cs := candidatesFor(s)
		if len(cs) == 0 {
			return nil
		}
		var next []fixture
		for _, fx := range out {
			for _, c := range cs {
				n := fixture{p0: clone(fx.p0), p1: clone(fx.p1), targets: append([]string(nil), fx.targets...)}
				if c.card == "" {
					n.targets = append(n.targets, c.seat)
				} else {
					s := &n.p1
					if c.seat == "p0" {
						s = &n.p0
					}
					count := 0
					for _, x := range append(append(append(append([]string(nil), s.Battlefield...), s.Graveyard...), s.Exile...), s.Hand...) {
						if x == c.card {
							count++
						}
					}
					switch c.zone {
					case "battlefield":
						s.Battlefield = append(s.Battlefield, c.card)
					case "graveyard":
						s.Graveyard = append(s.Graveyard, c.card)
					case "exile":
						s.Exile = append(s.Exile, c.card)
					case "hand":
						s.Hand = append(s.Hand, c.card)
					}
					ref := c.seat + ":" + c.card
					if count > 0 {
						ref = fmt.Sprintf("%s#%d", ref, count+1)
					}
					n.targets = append(n.targets, ref)
				}
				next = append(next, n)
				if len(next) >= 24 {
					break
				}
			}
		}
		out = next
	}
	return out
}

func clone(s Seat) Seat {
	return Seat{
		Battlefield: append([]string(nil), s.Battlefield...), Hand: append([]string(nil), s.Hand...),
		Graveyard: append([]string(nil), s.Graveyard...), Exile: append([]string(nil), s.Exile...),
		Library: append([]string(nil), s.Library...), LibraryTop: append([]string(nil), s.LibraryTop...),
	}
}
