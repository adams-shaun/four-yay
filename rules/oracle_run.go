package rules

// Oracle-text card audit (docs/superpowers/specs/2026-09-27-oracle-text-card-audit.md).
//
// Every scenario under testdata/oracle/<family>/<card>.json states what a
// card does according to its PRINTED (Oracle) text and the Comprehensive
// Rules, written by an author who never read the card's Forge script. The
// runner below builds a real two-seat game around real corpus cards, drives
// it only through the actions a player has (cast, activate, attack, block,
// pass priority, answer a decision), and asserts observable state: zones,
// life, P/T, keywords, counters, the stack, and what the engine offers. A
// failure therefore means the engine+script disagrees with the printed card,
// whichever side is wrong.
//
// The runner's only non-player actions are SETUP (placing named cards into
// zones and setting life totals before turn 1, as logged MoveZone and
// LifeChange events) and the "mana"/"move"/"life" ops, which stand in for
// an unspecified outside effect. All of them go through e.emit, so every
// scenario still replays from its log.
//
// Nothing here embeds card script or Oracle text: scenarios name cards and
// the corpus is read at run time (the GPL boundary, AGENTS.md).

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

type oracleFile struct {
	Card      string           `json:"card"`
	Family    string           `json:"family"`
	Author    string           `json:"author"`
	OracleSHA string           `json:"oracle_sha,omitempty"`
	Scenarios []oracleScenario `json:"scenarios"`
}

type oracleScenario struct {
	Name   string                `json:"name"`
	CR     []string              `json:"cr"`
	Why    string                `json:"why"`
	Format string                `json:"format,omitempty"`
	Setup  map[string]oracleSeat `json:"setup"`
	// SetupAnswers answer decisions posed while the runner drives from
	// genesis to turn 1's first main phase (a setup permanent's upkeep
	// trigger), in place of the fallback.
	SetupAnswers []oracleAnswer `json:"setup_answers,omitempty"`
	Steps        []oracleStep   `json:"steps"`
	Expect       []oracleExpect `json:"expect"`
}

type oracleSeat struct {
	Hand        []string `json:"hand,omitempty"`
	Battlefield []string `json:"battlefield,omitempty"`
	Graveyard   []string `json:"graveyard,omitempty"`
	Library     []string `json:"library,omitempty"`
	Exile       []string `json:"exile,omitempty"`
	Command     []string `json:"command,omitempty"`
	// LibraryTop puts these cards on top of the library, first = top.
	LibraryTop []string `json:"library_top,omitempty"`
	// Sideboard is the seat's cards outside the game (Config.Sideboards),
	// for "from outside the game" effects. They are genesis configuration,
	// not dealt from the deck, and are bound as "pN:Name" refs like the rest.
	Sideboard []string `json:"sideboard,omitempty"`
	Life      *int32   `json:"life,omitempty"`
}

type oracleStep struct {
	Op        string         `json:"op"`
	Seat      int            `json:"seat"`
	Card      string         `json:"card,omitempty"`
	Mana      string         `json:"mana,omitempty"`
	Targets   []string       `json:"targets,omitempty"`
	Kicked    bool           `json:"kicked,omitempty"`
	CastMode  string         `json:"cast_mode,omitempty"`
	Ability   string         `json:"ability,omitempty"`
	Attackers []string       `json:"attackers,omitempty"`
	Defender  string         `json:"defender,omitempty"`
	Blocks    [][2]string    `json:"blocks,omitempty"`
	Step      string         `json:"step,omitempty"`
	Active    string         `json:"active,omitempty"`
	Decision  string         `json:"decision,omitempty"`
	To        string         `json:"to,omitempty"`
	Amount    int32          `json:"amount,omitempty"`
	Answers   []oracleAnswer `json:"answers,omitempty"`
	Observe   *oracleObserve `json:"observe,omitempty"`
	Expect    []oracleExpect `json:"expect,omitempty"`
}

type oracleObserve struct {
	Kind   string   `json:"kind"`
	Source string   `json:"source,omitempty"`
	Has    []string `json:"has,omitempty"`
	Not    []string `json:"not,omitempty"`
}

type oracleAnswer struct {
	Kind string   `json:"kind"`
	Pick []string `json:"pick"`
}

type oracleExpect struct {
	Card           string            `json:"card,omitempty"`
	Zone           string            `json:"zone,omitempty"`
	PT             string            `json:"pt,omitempty"`
	Keywords       []string          `json:"keywords,omitempty"`
	NoKeywords     []string          `json:"no_keywords,omitempty"`
	Types          []string          `json:"types,omitempty"`
	NoTypes        []string          `json:"no_types,omitempty"`
	Tapped         *bool             `json:"tapped,omitempty"`
	Damage         *int32            `json:"damage,omitempty"`
	Counters       map[string]int32  `json:"counters,omitempty"`
	Life           map[string]int32  `json:"life,omitempty"`
	HandSize       map[string]int    `json:"hand_size,omitempty"`
	GraveyardSize  map[string]int    `json:"graveyard_size,omitempty"`
	Pool           map[string]string `json:"pool,omitempty"`
	TriggerOnStack string            `json:"trigger_on_stack,omitempty"`
	StackSize      *int              `json:"stack_size,omitempty"`
	Offered        *oracleOffered    `json:"offered,omitempty"`
	CanBlock       *oracleCanBlock   `json:"can_block,omitempty"`
	Count          *oracleCount      `json:"count,omitempty"`
	Eq             *int              `json:"eq,omitempty"`
	Want           *bool             `json:"want,omitempty"`
}

type oracleOffered struct {
	Seat  int    `json:"seat"`
	Kind  string `json:"kind"`
	Card  string `json:"card"`
	Label string `json:"label,omitempty"`
}

type oracleCanBlock struct {
	Blocker  string `json:"blocker"`
	Attacker string `json:"attacker"`
}

type oracleCount struct {
	Seat  int    `json:"seat"`
	Zone  string `json:"zone"`
	Name  string `json:"name"`
	Token *bool  `json:"token,omitempty"`
}

// oracleHarnessError marks a failure to PERFORM a scenario (unknown card, a
// ref that names nothing, a decision the runner cannot answer) as opposed
// to an expectation mismatch. Triage reads the two differently: an action the
// Oracle text says is legal but the engine does not offer is still reported
// here, and may itself be the bug.
type oracleHarnessError struct{ msg string }

func (h oracleHarnessError) Error() string { return "harness: " + h.msg }

func harnessf(format string, a ...any) error {
	return oracleHarnessError{fmt.Sprintf(format, a...)}
}

type oracleRun struct {
	cfg        Config
	reg        *cards.Registry
	e          *Engine
	refs       map[string]state.ObjID
	targets    []string
	answers    []oracleAnswer
	log        []string
	extraFails []string
	snaps      []OracleSnapshot
	decisions  []OracleDecision
	noSnapshot bool // runOracleScenarioWith's switch
	step       int  // the scenario step being played; -1 during setup
}

func (r *oracleRun) logf(format string, a ...any) {
	r.log = append(r.log, fmt.Sprintf(format, a...))
}

var oracleZones = map[string]state.Zone{
	"library": state.ZLibrary, "hand": state.ZHand, "battlefield": state.ZBattlefield,
	"graveyard": state.ZGraveyard, "exile": state.ZExile, "stack": state.ZStack,
	"command": state.ZCommand, "sideboard": state.ZSideboard,
}

// oracleFiller pads every library to 40 cards. Wastes has no colour, no
// subtype and no ability beyond {T}: Add {C}, so it influences no Oracle
// condition a scenario is likely to test.
const oracleFiller = "Wastes"

func parseSeatRef(s string) (state.PlayerID, bool) {
	if len(s) < 2 || s[0] != 'p' {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n < 0 {
		return 0, false
	}
	return state.PlayerID(n), true
}

// splitRef parses "p1:Name", "p1:Name#2" and "p1:token:Name#2".
func splitRef(ref string) (seat state.PlayerID, name string, token bool, nth int, err error) {
	i := strings.IndexByte(ref, ':')
	if i < 0 {
		return 0, "", false, 0, harnessf("bad card ref %q (want pN:Name)", ref)
	}
	p, ok := parseSeatRef(ref[:i])
	if !ok {
		return 0, "", false, 0, harnessf("bad seat in ref %q", ref)
	}
	name = ref[i+1:]
	if strings.HasPrefix(name, "token:") {
		token, name = true, strings.TrimPrefix(name, "token:")
	}
	nth = 1
	if j := strings.LastIndexByte(name, '#'); j >= 0 {
		n, perr := strconv.Atoi(name[j+1:])
		if perr != nil || n < 1 {
			return 0, "", false, 0, harnessf("bad ordinal in ref %q", ref)
		}
		name, nth = name[:j], n
	}
	return p, name, token, nth, nil
}

func (r *oracleRun) objName(o *state.Object) string {
	if o == nil || o.Face() == nil {
		return ""
	}
	return o.Face().Name
}

// resolve maps a card ref to an object id: setup-bound refs first (a card
// keeps its ObjID across zones), then the k-th matching object in id order.
func (r *oracleRun) resolve(ref string) (state.ObjID, error) {
	if id, ok := r.refs[ref]; ok {
		return id, nil
	}
	seat, name, token, nth, err := splitRef(ref)
	if err != nil {
		return 0, err
	}
	seen := 0
	for i := range r.e.G.Objs {
		o := &r.e.G.Objs[i]
		if o.Zone == state.ZCeased || o.Ability != nil || o.Face() == nil {
			continue
		}
		if token {
			if !o.IsToken || o.Controller != seat || o.Zone != state.ZBattlefield ||
				!strings.Contains(strings.ToLower(o.Face().Name), strings.ToLower(name)) {
				continue
			}
		} else if o.Owner != seat || o.Face().Name != name {
			continue
		}
		seen++
		if seen == nth {
			return o.ID, nil
		}
	}
	return 0, harnessf("ref %q names no object", ref)
}

// build constructs the game: named cards seeded into each seat's deck (so
// genesis mints them as real cards), commanders in the command zone, then --
// BEFORE turn 1 begins -- every opening hand returned to the library and each
// named card moved to its setup zone. Placing permanents before the first
// TurnChange means seat 0's battlefield is not summoning sick on turn 1.
func (r *oracleRun) build(sc oracleScenario) error {
	const seats = 2
	lookup := func(name string) (*cards.Card, error) {
		c, ok := r.reg.Lookup(name)
		if !ok {
			return nil, harnessf("card %q not in the corpus", name)
		}
		return c, nil
	}
	filler, err := lookup(oracleFiller)
	if err != nil {
		return err
	}
	type placement struct {
		name string
		zone state.Zone
		top  bool
	}
	decks := make([][]*cards.Card, seats)
	sideboards := make([][]*cards.Card, seats)
	commanders := make([][]int, seats)
	places := make([][]placement, seats)
	for key := range sc.Setup {
		if p, ok := parseSeatRef(key); !ok || int(p) >= seats {
			return harnessf("setup key %q (want p0 or p1)", key)
		}
	}
	for p := 0; p < seats; p++ {
		s := sc.Setup[fmt.Sprintf("p%d", p)]
		for _, z := range []struct {
			names []string
			zone  state.Zone
			top   bool
		}{{s.Battlefield, state.ZBattlefield, false}, {s.Hand, state.ZHand, false},
			{s.Graveyard, state.ZGraveyard, false}, {s.Library, state.ZLibrary, false},
			{s.Exile, state.ZExile, false}, {s.Command, state.ZCommand, false},
			{s.LibraryTop, state.ZLibrary, true}} {
			top := z.top
			for _, n := range z.names {
				c, err := lookup(n)
				if err != nil {
					return err
				}
				if z.zone == state.ZCommand {
					commanders[p] = append(commanders[p], len(decks[p]))
				}
				decks[p] = append(decks[p], c)
				places[p] = append(places[p], placement{n, z.zone, top})
			}
		}
		for len(decks[p]) < 40 {
			decks[p] = append(decks[p], filler)
		}
		for _, n := range s.Sideboard {
			c, err := lookup(n)
			if err != nil {
				return err
			}
			sideboards[p] = append(sideboards[p], c)
		}
	}
	cfg := Config{Seed: 42, Names: []string{"a", "b"}, Decks: decks, Tokens: r.reg.Tokens}
	for p := range sideboards {
		if len(sideboards[p]) > 0 {
			// Only a scenario that names a sideboard sets the field, so every
			// other scenario's Config (and its replay) is unchanged.
			cfg.Sideboards = sideboards
			break
		}
	}
	switch sc.Format {
	case "", "constructed":
	case "commander":
		cfg.Format = FormatCommander
		cfg.StartingLife = 40 // CR 903.7
		cfg.Commanders = commanders
	default:
		return harnessf("unknown format %q", sc.Format)
	}
	cfg = seatZeroStart(cfg)
	r.cfg = cfg
	// NewStartingPlayerChoice defers turn 1 to the first Advance (the toss
	// is identical to New's, so seatZeroStart's seed still starts seat 0).
	e := NewStartingPlayerChoice(cfg)
	r.e = e
	if e.G.StartingPlayer != 0 {
		return harnessf("seat 0 does not start (seed %d)", cfg.Seed)
	}
	for p := 0; p < seats; p++ {
		pid := state.PlayerID(p)
		for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, pid)...) {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
		bound := map[state.ObjID]bool{}
		counts := map[string]int{}
		var tops []state.ObjID
		for _, pl := range places[p] {
			var id state.ObjID
			for _, z := range []state.Zone{state.ZCommand, state.ZLibrary} {
				if pl.zone != state.ZCommand && z == state.ZCommand {
					continue
				}
				for _, cand := range e.G.Zone(z, pid) {
					if o := e.G.Obj(cand); !bound[cand] && r.objName(o) == pl.name {
						id = cand
						break
					}
				}
				if id != 0 {
					break
				}
			}
			if id == 0 {
				return harnessf("setup: p%d's %q was not dealt", p, pl.name)
			}
			bound[id] = true
			counts[pl.name]++
			ref := fmt.Sprintf("p%d:%s", p, pl.name)
			if counts[pl.name] > 1 {
				ref = fmt.Sprintf("%s#%d", ref, counts[pl.name])
			}
			r.refs[ref] = id
			if from := e.G.Obj(id).Zone; from != pl.zone {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: pl.zone})
			}
			if pl.top {
				tops = append(tops, id)
			}
		}
		// Sideboard cards were minted straight into the sideboard at genesis;
		// bind them in setup order, continuing the per-name ordinals.
		for _, n := range sc.Setup[fmt.Sprintf("p%d", p)].Sideboard {
			var id state.ObjID
			for _, cand := range e.G.Zone(state.ZSideboard, pid) {
				if o := e.G.Obj(cand); !bound[cand] && r.objName(o) == n {
					id = cand
					break
				}
			}
			if id == 0 {
				return harnessf("setup: p%d's sideboard %q was not minted", p, n)
			}
			bound[id] = true
			counts[n]++
			ref := fmt.Sprintf("p%d:%s", p, n)
			if counts[n] > 1 {
				ref = fmt.Sprintf("%s#%d", ref, counts[n])
			}
			r.refs[ref] = id
		}
		if len(tops) > 0 {
			// LibraryOrder's IDs are the complete new order, top first.
			order := append([]state.ObjID(nil), tops...)
			isTop := map[state.ObjID]bool{}
			for _, id := range tops {
				isTop[id] = true
			}
			for _, id := range e.G.Zone(state.ZLibrary, pid) {
				if !isTop[id] {
					order = append(order, id)
				}
			}
			e.emit(events.Event{Kind: events.LibraryOrder, Player: pid, IDs: order, Secret: true})
		}
		if life := sc.Setup[fmt.Sprintf("p%d", p)].Life; life != nil {
			if d := *life - e.G.Players[p].Life; d != 0 {
				e.emit(events.Event{Kind: events.LifeChange, Player: pid, Amount: d})
			}
		}
	}
	e.Advance()
	r.answers = append([]oracleAnswer(nil), sc.SetupAnswers...)
	// Drive to seat 0's first main phase. Triggers that setup placements
	// caused resolve here under the fallback answers; the transcript names
	// every one.
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return harnessf("game stopped during setup")
		}
		if d.Kind == decision.KPriority && e.G.Step == state.StepMain1 && e.G.Turn == 1 && len(e.G.Stack) == 0 {
			r.logf("setup done: turn %d %s, stack empty", e.G.Turn, e.G.Step)
			return nil
		}
		if err := r.answer(d, "setup"); err != nil {
			return err
		}
	}
	return harnessf("setup never reached turn 1 main1")
}

func pickPass(d *decision.Decision) int {
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o.Index
		}
	}
	return -1
}

func (r *oracleRun) submit(d *decision.Decision, choices []int, why string) error {
	labels := make([]string, 0, len(choices))
	for _, c := range choices {
		if c >= 0 && c < len(d.Options) {
			labels = append(labels, d.Options[c].Label)
		}
	}
	r.logf("  [%s] p%d %s -> %q", why, d.Player, d.Kind, labels)
	if kind, ok := oracleDecisionKind(d.Kind); ok {
		od := OracleDecision{Step: r.step, Seat: int(d.Player), Kind: kind, Options: len(d.Options), Picks: labels,
			PickIdx: append([]int{}, choices...), PickRefs: []string{}, Via: why, GorgeKind: string(d.Kind), Min: d.Min, Max: d.Max}
		if len(d.Options) > 0 {
			od.First = d.Options[0].Label
		}
		for _, c := range choices {
			if c < 0 || c >= len(d.Options) {
				continue
			}
			switch o := d.Options[c]; {
			case o.Obj != 0:
				od.PickRefs = append(od.PickRefs, r.objRef(r.e.G.Obj(o.Obj)))
			case strings.Contains(o.Kind, "player"):
				od.PickRefs = append(od.PickRefs, fmt.Sprintf("p%d", o.Player))
			default:
				od.PickRefs = append(od.PickRefs, o.Label)
			}
		}
		r.decisions = append(r.decisions, od)
	}
	if err := r.e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		return harnessf("submit %s %v: %v (options %s)", d.Kind, choices, err, optionDump(d))
	}
	return nil
}

// oracleActivateKind reports whether an option's Kind names an action the
// `activate` scenario op may select. Besides the ordinary "ability" and the
// engine's generic "activate" composite, the engine poses three special
// actions under their own option kinds -- Station (rules/legal.go
// "station"), Room unlock ("unlock") and morph-family turn face up
// ("turn_face_up") -- each already offered and performed rules-side. A
// scenario drives them through `activate` plus the option's label, exactly
// as a named ability is driven; they are not a separate op.
func oracleActivateKind(kind string) bool {
	switch kind {
	case "ability", "activate", "station", "unlock", "turn_face_up":
		return true
	}
	return false
}

func oracleLabelMatches(label, want string) bool {
	normalize := func(s string) string {
		s = strings.ToLower(s)
		s = strings.NewReplacer("{", "", "}", "").Replace(s)
		return s
	}
	return strings.Contains(normalize(label), normalize(want))
}

// oracleManaColourAliases maps a scenario's colour word or letter to the
// mana symbol. Package-level, not a local: rules' param census scans every
// non-test file in the package and classifies local map[string]string reads
// as card-param reads.
var oracleManaColourAliases = map[string]string{
	"w": "W", "white": "W", "u": "U", "blue": "U", "b": "B", "black": "B",
	"r": "R", "red": "R", "g": "G", "green": "G",
}

func oracleManaColourMatches(symbol, want string) bool {
	wantSymbol, ok := oracleManaColourAliases[strings.ToLower(strings.TrimSpace(want))]
	return ok && strings.EqualFold(symbol, wantSymbol)
}

func oracleOptionMatches(o decision.Option, want string) bool {
	return oracleManaColourMatches(o.ManaSymbol, want) || oracleLabelMatches(o.Label, want)
}

func oracleObserveMismatches(d *decision.Decision, observe oracleObserve) []string {
	var mismatches []string
	for _, want := range observe.Has {
		found := false
		for _, o := range d.Options {
			if oracleOptionMatches(o, want) {
				found = true
				break
			}
		}
		if !found {
			mismatches = append(mismatches, fmt.Sprintf("observed %s options missing %q", d.Kind, want))
		}
	}
	for _, want := range observe.Not {
		for _, o := range d.Options {
			if oracleOptionMatches(o, want) {
				mismatches = append(mismatches, fmt.Sprintf("observed %s options unexpectedly contain %q", d.Kind, want))
				break
			}
		}
	}
	return mismatches
}

func hasOracleAnswer(answers []oracleAnswer, kind decision.Kind) bool {
	for _, answer := range answers {
		if strings.EqualFold(answer.Kind, string(kind)) {
			return true
		}
	}
	return false
}

// manaAbilityLabels returns the engine's currently available mana abilities,
// which are the authoritative named candidates behind the generic priority option.
func (r *oracleRun) manaAbilityLabels(seat state.PlayerID, id state.ObjID) []string {
	abilities := r.e.availableManaAbilities(seat, id)
	labels := make([]string, 0, len(abilities))
	for _, ma := range abilities {
		labels = append(labels, manaAbilityLabel(ma, r.e.chosenProducedColour(id)))
	}
	return labels
}

func optionDump(d *decision.Decision) string {
	var b strings.Builder
	for _, o := range d.Options {
		fmt.Fprintf(&b, "[%d %s %q obj=%d pl=%d att=%d mode=%s] ", o.Index, o.Kind, o.Label, o.Obj, o.Player, o.Attacker, o.Mode)
	}
	return b.String()
}

// matchPick maps one pick string onto an option index: "pN" is a player,
// "pN:..." a card ref, anything else a case-insensitive label substring or
// an exact option kind.
func (r *oracleRun) matchPick(d *decision.Decision, pick string, used map[int]bool) (int, error) {
	if p, ok := parseSeatRef(pick); ok {
		for _, o := range d.Options {
			if !used[o.Index] && o.Obj == 0 && o.Player == p && o.Kind != "pass" {
				return o.Index, nil
			}
		}
		for _, o := range d.Options {
			if !used[o.Index] && o.Obj == state.PlayerRef(p) {
				return o.Index, nil
			}
		}
		return -1, nil
	}
	if strings.HasPrefix(pick, "p") && strings.Contains(pick, ":") {
		id, err := r.resolve(pick)
		if err != nil {
			return -1, err
		}
		for _, o := range d.Options {
			if !used[o.Index] && o.Obj == id {
				return o.Index, nil
			}
		}
		return -1, nil
	}
	lp := strings.ToLower(pick)
	// "yes"/"no" are the author's abstract accept/decline: they match the
	// engine's yes/no/decline option kinds or a label that starts that way,
	// so a scenario never has to know the engine's prompt wording.
	if lp == "yes" || lp == "no" {
		for _, o := range d.Options {
			k, l := strings.ToLower(o.Kind), strings.ToLower(o.Label)
			if used[o.Index] {
				continue
			}
			if lp == "yes" && (k == "yes" || k == "accept" || o.Mode == "unless_pay" || strings.HasPrefix(l, "yes")) {
				return o.Index, nil
			}
			if lp == "no" && (k == "no" || k == "decline" || o.Mode == "unless_decline" || strings.HasPrefix(l, "no") ||
				strings.HasPrefix(l, "decline") || strings.HasPrefix(l, "refuse") || strings.HasPrefix(l, "don't")) {
				return o.Index, nil
			}
		}
	}
	for _, o := range d.Options {
		if !used[o.Index] && oracleManaColourMatches(o.ManaSymbol, pick) {
			return o.Index, nil
		}
	}
	for _, o := range d.Options {
		if !used[o.Index] && (strings.EqualFold(o.Kind, pick) || oracleLabelMatches(o.Label, pick)) {
			return o.Index, nil
		}
	}
	return -1, nil
}

// answer handles one pending decision. Priority is passed. Anything else is
// answered from the step's queues (targets, then answers of the matching
// kind), else by the documented fallback: accept an optional trigger, keep
// orders as offered, and otherwise take the first Min options. Every
// fallback is logged so a failure shows exactly what the runner chose.
func (r *oracleRun) answer(d *decision.Decision, why string) error {
	if d.Kind == decision.KPriority {
		idx := pickPass(d)
		if idx < 0 {
			return harnessf("priority with no pass option")
		}
		return r.e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}})
	}
	if d.Kind == decision.KTarget && len(r.targets) > 0 {
		used := map[int]bool{}
		var choices []int
		for len(r.targets) > 0 && len(choices) < max(d.Max, 1) {
			idx, err := r.matchPick(d, r.targets[0], used)
			if err != nil {
				return err
			}
			if idx < 0 {
				if len(choices) == 0 {
					return harnessf("target %q not offered: %s", r.targets[0], optionDump(d))
				}
				break
			}
			r.targets = r.targets[1:]
			used[idx] = true
			choices = append(choices, idx)
		}
		return r.submit(d, choices, "target")
	}
	for i, a := range r.answers {
		if a.Kind != string(d.Kind) && a.Kind != "any" {
			continue
		}
		r.answers = append(r.answers[:i:i], r.answers[i+1:]...)
		used := map[int]bool{}
		var choices []int
		for _, p := range a.Pick {
			idx, err := r.matchPick(d, p, used)
			if err != nil {
				return err
			}
			if idx < 0 {
				return harnessf("answer %q not offered for %s: %s", p, d.Kind, optionDump(d))
			}
			used[idx] = true
			choices = append(choices, idx)
		}
		return r.submit(d, choices, "answer")
	}
	var choices []int
	switch d.Kind {
	case decision.KTriggerOptional:
		for _, o := range d.Options {
			if o.Kind == "yes" {
				choices = []int{o.Index}
			}
		}
	case decision.KTriggerOrder, decision.KArrange:
		for _, o := range d.Options {
			choices = append(choices, o.Index)
		}
	case decision.KMulligan:
		for _, o := range d.Options {
			if o.Kind == "keep" {
				choices = []int{o.Index}
			}
		}
	}
	if choices == nil {
		for i := 0; i < d.Min && i < len(d.Options); i++ {
			choices = append(choices, d.Options[i].Index)
		}
		if choices == nil {
			choices = []int{}
		}
	}
	return r.submit(d, choices, why+" fallback")
}

// untilPriority answers non-priority decisions until a priority decision
// (or the end of the game) is pending.
func (r *oracleRun) untilPriority(why string) error {
	for i := 0; i < 200; i++ {
		d := r.e.Pending()
		if d == nil || r.e.G.Over || d.Kind == decision.KPriority {
			return nil
		}
		if err := r.answer(d, why); err != nil {
			return err
		}
	}
	return harnessf("%s: decisions never returned to priority", why)
}

func (r *oracleRun) addMana(seat state.PlayerID, mana string) error {
	d := r.e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != seat {
		return harnessf("mana: p%d does not hold priority", seat)
	}
	for _, c := range mana {
		if !strings.ContainsRune("WUBRGC", c) {
			return harnessf("mana: bad symbol %q", c)
		}
		r.e.emit(events.Event{Kind: events.ManaAdd, Player: seat, Counter: string(c), Amount: 1})
	}
	r.e.priorityRound()
	r.logf("  [mana] p%d +%s", seat, mana)
	return nil
}

func (r *oracleRun) priorityFor(seat state.PlayerID, op string) (*decision.Decision, error) {
	d := r.e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != seat {
		got := "none"
		if d != nil {
			got = fmt.Sprintf("%s for p%d", d.Kind, d.Player)
		}
		return nil, harnessf("%s: p%d does not hold priority (pending %s)", op, seat, got)
	}
	return d, nil
}

func (r *oracleRun) do(st oracleStep) error {
	e := r.e
	r.targets = append([]string(nil), st.Targets...)
	r.answers = append([]oracleAnswer(nil), st.Answers...)
	seat := state.PlayerID(st.Seat)
	r.logf("step %s %s", st.Op, st.Card)
	switch st.Op {
	case "mana":
		return r.addMana(seat, st.Mana)
	case "cast", "activate":
		if st.Mana != "" {
			if err := r.addMana(seat, st.Mana); err != nil {
				return err
			}
		}
		id, err := r.resolve(st.Card)
		if err != nil {
			return err
		}
		d, err := r.priorityFor(seat, st.Op)
		if err != nil {
			return err
		}
		// A cast picks the option whose Mode matches: "kicked" for a kicked
		// cast, an explicit cast_mode (flashback, evoke, ...) when given, and
		// otherwise the plain cast -- or, when the card offers only a
		// permission-mode cast (a graveyard "mayplay"), that one.
		idx, fallback, manaFallback := -1, -1, -1
		wantMode := st.CastMode
		if st.Kicked {
			wantMode = "kicked"
		}
		// A named mana ability lives behind the generic "Activate <card> for
		// mana" priority option: the engine asks a second-stage KChoose over
		// the source's available mana abilities (or, when exactly one is
		// available, resolves it with no ask at all). The generic option may
		// stand in for a requested label only when that label names an ability
		// the source can currently produce -- the same authoritative set the
		// offer and the second stage are built from -- otherwise the step must
		// fail loudly below instead of silently activating a different ability
		// (or a mana ability when a non-mana label was requested).
		manaLabels := []string(nil)
		requestedManaAbility := false
		if st.Op == "activate" && st.Ability != "" {
			manaLabels = r.manaAbilityLabels(seat, id)
			for _, label := range manaLabels {
				if oracleLabelMatches(label, st.Ability) {
					requestedManaAbility = true
					break
				}
			}
		}
		for _, o := range d.Options {
			if o.Obj != id {
				continue
			}
			if st.Op == "cast" && o.Kind == "cast" {
				if o.Mode == wantMode {
					idx = o.Index
					break
				}
				if wantMode == "" && fallback < 0 && !strings.HasPrefix(o.Mode, "kicked") {
					fallback = o.Index
				}
			}
			if st.Op == "activate" && oracleActivateKind(o.Kind) {
				if st.Ability == "" || oracleLabelMatches(o.Label, st.Ability) {
					idx = o.Index
					break
				}
				if o.Kind == "activate" && requestedManaAbility && strings.Contains(strings.ToLower(o.Label), "for mana") && manaFallback < 0 {
					manaFallback = o.Index
				}
			}
		}
		if idx < 0 {
			idx = manaFallback
		}
		if idx < 0 {
			idx = fallback
		}
		if idx < 0 {
			want := ""
			if st.Ability != "" {
				want = fmt.Sprintf(" (ability: %q)", st.Ability)
			}
			return harnessf("%s %s not offered%s: %s", st.Op, st.Card, want, optionDump(d))
		}
		if err := r.submit(d, []int{idx}, st.Op); err != nil {
			return err
		}
		if st.Observe != nil {
			pending := e.Pending()
			if pending == nil || pending.Kind != decision.Kind(st.Observe.Kind) {
				got := "none"
				if pending != nil {
					got = string(pending.Kind)
				}
				return harnessf("observe expected pending %s decision, got %s", st.Observe.Kind, got)
			}
			if st.Observe.Source != "" {
				source, err := r.resolve(st.Observe.Source)
				if err != nil {
					return err
				}
				if pending.Source != source {
					return harnessf("observe expected source %s (object %d), got %d", st.Observe.Source, source, pending.Source)
				}
			}
			r.extraFails = append(r.extraFails, oracleObserveMismatches(pending, *st.Observe)...)
		}
		if st.Op == "activate" && st.Ability != "" && d.Options[idx].Kind == "activate" &&
			!oracleLabelMatches(d.Options[idx].Label, st.Ability) {
			// The generic mana option was submitted for a named ability. When
			// the engine poses the second-stage wheel, the requested label must
			// be among its options. When it does NOT -- exactly one ability was
			// available and the engine resolved it with no ask -- the requested
			// label must have been that single ability (checked above against
			// the same set); a multi-ability source that never asks, or a single
			// ability that does not match, is an error, never a silent
			// different-ability activation.
			choice := e.Pending()
			if choice != nil && choice.Kind == decision.KChoose && choice.Source == id {
				manaOptions := false
				for _, o := range choice.Options {
					if o.Kind == "mana" {
						manaOptions = true
						break
					}
				}
				// A queued answer owns the pending decision: fall back to label
				// matching only when the scenario did not queue one, so a costed
				// any-colour wheel behaves like every other colour wheel.
				if manaOptions && !hasOracleAnswer(r.answers, choice.Kind) {
					for _, o := range choice.Options {
						if o.Kind == "mana" && oracleLabelMatches(o.Label, st.Ability) {
							if err := r.submit(choice, []int{o.Index}, "mana ability"); err != nil {
								return err
							}
							break
						}
					}
					matched := false
					for _, o := range choice.Options {
						if o.Kind == "mana" && oracleLabelMatches(o.Label, st.Ability) {
							matched = true
							break
						}
					}
					if !matched {
						return harnessf("mana ability %q not offered: %s", st.Ability, optionDump(choice))
					}
				}
			} else if len(manaLabels) != 1 || !oracleLabelMatches(manaLabels[0], st.Ability) {
				return harnessf("mana ability %q not offered (available: %v)", st.Ability, manaLabels)
			}
		}
		return r.untilPriority(st.Op)
	case "play":
		id, err := r.resolve(st.Card)
		if err != nil {
			return err
		}
		d, err := r.priorityFor(seat, st.Op)
		if err != nil {
			return err
		}
		for _, o := range d.Options {
			if o.Obj == id && o.Kind == "play_land" {
				if err := r.submit(d, []int{o.Index}, "play"); err != nil {
					return err
				}
				return r.untilPriority("play")
			}
		}
		return harnessf("play %s not offered: %s", st.Card, optionDump(d))
	case "resolve":
		for i := 0; i < 300; i++ {
			d := e.Pending()
			if d == nil || e.G.Over {
				return nil
			}
			if d.Kind == decision.KPriority && len(e.G.Stack) == 0 {
				return nil
			}
			if err := r.answer(d, "resolve"); err != nil {
				return err
			}
		}
		return harnessf("resolve: stack never emptied")
	case "attack":
		def, ok := parseSeatRef(st.Defender)
		if !ok {
			return harnessf("attack: bad defender %q", st.Defender)
		}
		for i := 0; ; i++ {
			d := e.Pending()
			if d == nil || e.G.Over || i > 200 {
				return harnessf("attack: never reached the declare-attackers decision")
			}
			if d.Kind == decision.KAttackers && d.Player == seat {
				used := map[int]bool{}
				var choices []int
				for _, a := range st.Attackers {
					id, err := r.resolve(a)
					if err != nil {
						return err
					}
					idx := -1
					for _, o := range d.Options {
						if !used[o.Index] && o.Obj == id && o.Player == def {
							idx = o.Index
							break
						}
					}
					if idx < 0 {
						return harnessf("attack: %s at p%d not offered: %s", a, def, optionDump(d))
					}
					used[idx] = true
					choices = append(choices, idx)
				}
				if err := r.submit(d, choices, "attack"); err != nil {
					return err
				}
				return r.untilPriority("attack")
			}
			if d.Kind == decision.KPriority && len(e.G.Stack) == 0 && e.G.Step > state.StepDeclareAttackers {
				return harnessf("attack: passed declare-attackers without being asked")
			}
			if err := r.answer(d, "to-attack"); err != nil {
				return err
			}
		}
	case "block":
		d := e.Pending()
		if d == nil || d.Kind != decision.KBlockers {
			return harnessf("block: no blockers decision pending")
		}
		used := map[int]bool{}
		var choices []int
		for _, pair := range st.Blocks {
			b, err := r.resolve(pair[0])
			if err != nil {
				return err
			}
			a, err := r.resolve(pair[1])
			if err != nil {
				return err
			}
			idx := -1
			for _, o := range d.Options {
				if !used[o.Index] && o.Obj == b && o.Attacker == a {
					idx = o.Index
				}
			}
			if idx < 0 {
				return harnessf("block: %s on %s not offered: %s", pair[0], pair[1], optionDump(d))
			}
			used[idx] = true
			choices = append(choices, idx)
		}
		if err := r.submit(d, choices, "block"); err != nil {
			return err
		}
		return r.untilPriority("block")
	case "pass":
		// `pass` answers exactly one priority decision: the named seat must
		// hold priority right now, or this fails loudly rather than silently
		// passing someone else's priority (which `pass_to` does not check).
		d, err := r.priorityFor(seat, st.Op)
		if err != nil {
			return err
		}
		return r.submit(d, []int{pickPass(d)}, "pass")
	case "pass_to":
		var want state.Step
		if st.Step != "" {
			s, ok := state.ParseStep(st.Step)
			if !ok {
				return harnessf("pass_to: unknown step %q", st.Step)
			}
			want = s
		}
		active, hasActive := parseSeatRef(st.Active)
		for i := 0; i < 2000; i++ {
			d := e.Pending()
			if d == nil || e.G.Over {
				return harnessf("pass_to: game stopped")
			}
			if st.Decision != "" && string(d.Kind) == st.Decision {
				return nil
			}
			if st.Step != "" && i > 0 && e.G.Step == want && (!hasActive || e.G.Active == active) {
				return nil
			}
			if err := r.answer(d, "pass_to"); err != nil {
				return err
			}
		}
		return harnessf("pass_to: target never reached")
	case "move":
		id, err := r.resolve(st.Card)
		if err != nil {
			return err
		}
		to, ok := oracleZones[st.To]
		if !ok {
			return harnessf("move: unknown zone %q", st.To)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: to})
		e.priorityRound()
		return r.untilPriority("move")
	case "life":
		e.emit(events.Event{Kind: events.LifeChange, Player: seat, Amount: st.Amount})
		e.priorityRound()
		return r.untilPriority("life")
	}
	return harnessf("unknown op %q", st.Op)
}

func normCounter(k string) string {
	switch strings.ReplaceAll(strings.ToLower(k), " ", "") {
	case "+1/+1", "p1p1":
		return "P1P1"
	case "-1/-1", "m1m1":
		return "M1M1"
	}
	return strings.ToUpper(k)
}

func poolString(m state.Mana) string {
	var b strings.Builder
	for i, c := range "WUBRGC" {
		b.WriteString(strings.Repeat(string(c), int(m[i])))
	}
	return b.String()
}

func normPool(s string) string {
	var m state.Mana
	for i, c := range "WUBRGC" {
		m[i] = int32(strings.Count(strings.ToUpper(s), string(c)))
	}
	return poolString(m)
}

func oracleHasFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func (r *oracleRun) wantBool(x oracleExpect) bool { return x.Want == nil || *x.Want }

// check evaluates one expectation and returns its mismatches.
func (r *oracleRun) check(x oracleExpect) []string {
	e := r.e
	var bad []string
	failf := func(format string, a ...any) { bad = append(bad, fmt.Sprintf(format, a...)) }
	seatOf := func(k string) (state.PlayerID, bool) {
		p, ok := parseSeatRef(k)
		if !ok || int(p) >= len(e.G.Players) {
			failf("bad seat key %q", k)
			return 0, false
		}
		return p, true
	}
	if x.Card != "" {
		id, err := r.resolve(x.Card)
		if err != nil {
			return []string{err.Error()}
		}
		o := e.G.Obj(id)
		if x.Zone != "" && o.Zone.String() != x.Zone {
			failf("%s: zone %s, want %s", x.Card, o.Zone, x.Zone)
		}
		if x.PT != "" {
			if got := fmt.Sprintf("%d/%d", e.Power(id), e.Toughness(id)); got != x.PT {
				failf("%s: P/T %s, want %s", x.Card, got, x.PT)
			}
		}
		kws := e.Keywords(id)
		for _, k := range x.Keywords {
			if !oracleHasFold(kws, k) {
				failf("%s: lacks keyword %q (has %v)", x.Card, k, kws)
			}
		}
		for _, k := range x.NoKeywords {
			if oracleHasFold(kws, k) {
				failf("%s: has keyword %q, want none", x.Card, k)
			}
		}
		types := e.Derived(id).Types
		for _, ty := range x.Types {
			if !oracleHasFold(types, ty) {
				failf("%s: lacks type %q (types %v)", x.Card, ty, types)
			}
		}
		for _, ty := range x.NoTypes {
			if oracleHasFold(types, ty) {
				failf("%s: has type %q, want not (types %v)", x.Card, ty, types)
			}
		}
		if x.Tapped != nil && o.Tapped != *x.Tapped {
			failf("%s: tapped=%v, want %v", x.Card, o.Tapped, *x.Tapped)
		}
		if x.Damage != nil && o.Damage != *x.Damage {
			failf("%s: damage %d, want %d", x.Card, o.Damage, *x.Damage)
		}
		for k, n := range x.Counters {
			var got int32
			for _, c := range o.Counters {
				if strings.EqualFold(c.Kind, normCounter(k)) {
					got += c.N
				}
			}
			if got != n {
				failf("%s: %s counters %d, want %d (counters %v)", x.Card, k, got, n, o.Counters)
			}
		}
	}
	for k, want := range x.Life {
		if p, ok := seatOf(k); ok && e.G.Players[p].Life != want {
			failf("%s life %d, want %d", k, e.G.Players[p].Life, want)
		}
	}
	for k, want := range x.HandSize {
		if p, ok := seatOf(k); ok && len(e.G.Zone(state.ZHand, p)) != want {
			failf("%s hand size %d, want %d", k, len(e.G.Zone(state.ZHand, p)), want)
		}
	}
	for k, want := range x.GraveyardSize {
		if p, ok := seatOf(k); ok && len(e.G.Zone(state.ZGraveyard, p)) != want {
			failf("%s graveyard size %d, want %d", k, len(e.G.Zone(state.ZGraveyard, p)), want)
		}
	}
	for k, want := range x.Pool {
		if p, ok := seatOf(k); ok {
			if got := poolString(e.G.Players[p].Pool); got != normPool(want) {
				failf("%s pool %q, want %q", k, got, normPool(want))
			}
		}
	}
	if x.TriggerOnStack != "" {
		id, err := r.resolve(x.TriggerOnStack)
		if err != nil {
			return []string{err.Error()}
		}
		found := false
		for _, sid := range e.G.Stack {
			if o := e.G.Obj(sid); o != nil && o.Source == id && o.Ability != nil {
				found = true
			}
		}
		if found != r.wantBool(x) {
			failf("trigger from %s on stack=%v, want %v (stack %s)", x.TriggerOnStack, found, r.wantBool(x), r.stackDump())
		}
	}
	if x.StackSize != nil && len(e.G.Stack) != *x.StackSize {
		failf("stack size %d, want %d (%s)", len(e.G.Stack), *x.StackSize, r.stackDump())
	}
	if x.Offered != nil {
		id, err := r.resolve(x.Offered.Card)
		if err != nil {
			return []string{err.Error()}
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority || int(d.Player) != x.Offered.Seat {
			failf("offered: p%d does not hold priority", x.Offered.Seat)
		} else {
			found := false
			for _, o := range d.Options {
				kindOK := o.Kind == x.Offered.Kind || (x.Offered.Kind == "activate" && o.Kind == "ability")
				if o.Obj == id && kindOK && oracleLabelMatches(o.Label, x.Offered.Label) {
					found = true
				}
			}
			if x.Offered.Kind == "activate" && x.Offered.Label != "" {
				for _, label := range r.manaAbilityLabels(state.PlayerID(x.Offered.Seat), id) {
					if oracleLabelMatches(label, x.Offered.Label) {
						found = true
					}
				}
			}
			if found != r.wantBool(x) {
				failf("%s %s offered=%v, want %v", x.Offered.Kind, x.Offered.Card, found, r.wantBool(x))
			}
		}
	}
	if x.CanBlock != nil {
		b, err1 := r.resolve(x.CanBlock.Blocker)
		a, err2 := r.resolve(x.CanBlock.Attacker)
		if err1 != nil || err2 != nil {
			return []string{fmt.Sprint(err1, err2)}
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KBlockers {
			failf("can_block: no blockers decision pending")
		} else {
			found := false
			for _, o := range d.Options {
				if o.Obj == b && o.Attacker == a {
					found = true
				}
			}
			if found != r.wantBool(x) {
				failf("%s can block %s = %v, want %v", x.CanBlock.Blocker, x.CanBlock.Attacker, found, r.wantBool(x))
			}
		}
	}
	if x.Count != nil {
		z, ok := oracleZones[x.Count.Zone]
		if !ok {
			return []string{fmt.Sprintf("count: unknown zone %q", x.Count.Zone)}
		}
		n := 0
		for _, id := range e.G.Zone(z, state.PlayerID(x.Count.Seat)) {
			o := e.G.Obj(id)
			if o == nil || !strings.Contains(strings.ToLower(r.objName(o)), strings.ToLower(x.Count.Name)) {
				continue
			}
			if x.Count.Token != nil && o.IsToken != *x.Count.Token {
				continue
			}
			n++
		}
		if x.Eq == nil {
			failf("count without eq")
		} else if n != *x.Eq {
			failf("count p%d %s %q = %d, want %d", x.Count.Seat, x.Count.Zone, x.Count.Name, n, *x.Eq)
		}
	}
	return bad
}

func (r *oracleRun) stackDump() string {
	var parts []string
	for _, id := range r.e.G.Stack {
		o := r.e.G.Obj(id)
		src := ""
		if o != nil && o.Source != 0 {
			src = "<-" + r.objName(r.e.G.Obj(o.Source))
		}
		parts = append(parts, r.objName(o)+src)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// oracleUnconsumedMarker tags the fail a step raises when its declared
// `answers` queue still holds entries after the step finished: the fixture
// declared a decision this step never posed (or posed on a later step), so
// nothing matched it and the engine fell back. It is distinct from a known
// engine divergence so the audit can excuse exactly these rows via the
// shrinking known-unconsumed ratchet without hiding a real failure.
const oracleUnconsumedMarker = "unconsumed answer(s) for this step:"

// oracleUnconsumedFail formats the one fail a step gets for leftovers: the
// step index, the op, and every leftover answer verbatim (kind + pick).
func oracleUnconsumedFail(step int, op string, answers []oracleAnswer) string {
	j, _ := json.Marshal(answers)
	return fmt.Sprintf("step %d (%s): %s %s", step, op, oracleUnconsumedMarker, j)
}

// runOracleScenario plays one scenario and returns its mismatches (nil =
// pass), a transcript, and the run (for the replay check; its engine is nil
// when setup failed before genesis).
func runOracleScenario(reg *cards.Registry, sc oracleScenario) (fails []string, transcript []string, run *oracleRun) {
	return runOracleScenarioWith(reg, sc, false)
}

// runOracleScenarioWith is runOracleScenario with snapshots switched off
// when noSnapshot is set (the A/B check that snapshotting is read-only).
func runOracleScenarioWith(reg *cards.Registry, sc oracleScenario, noSnapshot bool) (fails []string, transcript []string, run *oracleRun) {
	r := &oracleRun{reg: reg, refs: map[string]state.ObjID{}, noSnapshot: noSnapshot, step: -1}
	run = r
	defer func() {
		if p := recover(); p != nil {
			fails = append(fails, fmt.Sprintf("harness: panic: %v", p))
			transcript = r.log
		}
	}()
	if err := r.build(sc); err != nil {
		return []string{err.Error()}, r.log, r
	}
	if !r.noSnapshot {
		r.snaps = append(r.snaps, r.snapshot("setup"))
	}
	for i, st := range sc.Steps {
		r.step = i
		if err := r.do(st); err != nil {
			return append(fails, fmt.Sprintf("step %d (%s): %v", i, st.Op, err)), r.log, r
		}
		if !r.noSnapshot {
			r.snaps = append(r.snaps, r.snapshot(fmt.Sprintf("step %d (%s)", i, st.Op)))
		}
		// A step's answers are consumed lazily by r.answer as decisions are
		// posed. A leftover means no decision on THIS step matched it: either
		// the fixture declares a decision the engine never asks, or the answer
		// was written on the wrong step. Fail loudly -- the engine's fallback
		// otherwise masks the stale fixture.
		if len(r.answers) > 0 {
			fails = append(fails, oracleUnconsumedFail(i, st.Op, r.answers))
		}
		for _, msg := range r.extraFails {
			fails = append(fails, fmt.Sprintf("after step %d (%s): %s", i, st.Op, msg))
		}
		r.extraFails = nil
		for _, x := range st.Expect {
			for _, b := range r.check(x) {
				fails = append(fails, fmt.Sprintf("after step %d (%s): %s", i, st.Op, b))
			}
		}
	}
	for _, x := range sc.Expect {
		fails = append(fails, r.check(x)...)
	}
	return fails, r.log, r
}

// seatZeroStart lives here, not in a test file, because runOracleScenario
// (non-test since the compliance pipeline calls it) needs it; every test in
// the package still uses it.
//
// seatZeroStart returns cfg with the smallest seed >= cfg.Seed whose CR 103.1
// toss starts seat 0. The scenario fixtures predate the toss: their
// protagonist is seat 0 -- before the toss seat 0 was always the starting
// player, so every fixture addresses seats and turns by index -- and the
// winner-chooses arm that would let a fixture name its starter is
// deliberately unbuilt (the "Known approximations" row in AGENTS.md). The
// toss draw sits BEFORE any shuffle, so it is deck-independent and the first
// acceptable seed is a pure function of the requested one; the effective seed
// travels in the returned Config, which is what a replay must be handed.
func seatZeroStart(cfg Config) Config {
	for {
		e := New(cfg)
		if e.G.Active == 0 {
			return cfg
		}
		cfg.Seed++
	}
}
