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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/cards/oracletext"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// oracleKnownDivergent is the audit's ratchet (the knownUnsupported pattern
// of acceptance_test.go): "<card>/<scenario>" -> the observed divergence. A
// listed scenario must still FAIL (a fixed one is stale and fails the build
// until its row is deleted); an unlisted failure fails the build. Rows are
// only ever added by the triage step, never by the scenario author.
var oracleKnownDivergent = map[string]string{
	// Engine gap: stat:Continuous RemoveType$ is never read (layers.go reads
	// only AddType$/RemoveCardTypes$/RemoveCreatureTypes$); paramcensus
	// already lists it for Purphoros and Mogis. 29 corpus scripts carry it,
	// the Theros god cycle among them, so every god is always a creature.
	"Purphoros, God of the Forge/low-devotion-not-a-creature": "a devotion-1 Purphoros is a 6/5 creature (RemoveType$ unread)",
	// Engine bug (CR 603.10a): a granted "whenever a creature you control
	// dies" trigger is checked AFTER a non-SBA departure, so the departure
	// that ends the grant's IsPresent$ condition loses its own trigger. The
	// SBA path (only-cleric-dies-to-damage-looks-back) uses the pre-batch
	// snapshot and passes.
	"Relic Vial/only-cleric-dies-looks-back":              "destroying the only Cleric drains nobody (no look-back for an effect destroy)",
	"Relic Vial/sacrifice-only-cleric-as-cost-looks-back": "sacrificing the only Cleric as a cost drains nobody (no look-back for a cost sacrifice)",
	// Engine bug, ticket fb-20260927T160557Z-b958ef31: the trigger walk
	// (Engine.forEachObject) visits ZLibrary..ZStack only, never ZCommand,
	// so an Eminence trigger never fires from the command zone.
	"Sidar Jabari of Zhalfir/eminence-from-command-zone-knight-attacks": "no Eminence trigger while the commander is in the command zone",
	// Engine/script gap: max-speed-gated AddAbility is not offered after the
	// three turn-specific speed increases (CR 702.179).
	"Amonkhet Raceway/max-speed-after-opponent-loses-life-on-three-turns": "max-speed haste activation is not offered after reaching speed four",
	// Script translation: Brotherhood Scribe's CounterAddedOnce trigger does
	// not produce the printed team-wide +1/+1 bonus after its energy ability.
	"Brotherhood Scribe/metalcraft-three-artifacts-gives-energy": "observed Scribe 1/3 and Lions 2/1, expected 2/4 and 3/2 after energy",
	// Script translation: Urza's Workshop's conditional Urza-land count is
	// not reflected in its mana ability; the three-land board produces one C.
	"Urza's Workshop/metalcraft-three-artifacts-three-urza-lands": "observed C, expected CCC for three Urza's lands",
}

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
	Life       *int32   `json:"life,omitempty"`
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
	Expect    []oracleExpect `json:"expect,omitempty"`
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
	cfg     Config
	reg     *cards.Registry
	e       *Engine
	refs    map[string]state.ObjID
	targets []string
	answers []oracleAnswer
	log     []string
}

func (r *oracleRun) logf(format string, a ...any) {
	r.log = append(r.log, fmt.Sprintf(format, a...))
}

var oracleZones = map[string]state.Zone{
	"library": state.ZLibrary, "hand": state.ZHand, "battlefield": state.ZBattlefield,
	"graveyard": state.ZGraveyard, "exile": state.ZExile, "stack": state.ZStack,
	"command": state.ZCommand,
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
	}
	cfg := Config{Seed: 42, Names: []string{"a", "b"}, Decks: decks, Tokens: r.reg.Tokens}
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
	if err := r.e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		return harnessf("submit %s %v: %v (options %s)", d.Kind, choices, err, optionDump(d))
	}
	return nil
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
		if !used[o.Index] && (strings.EqualFold(o.Kind, pick) || strings.Contains(strings.ToLower(o.Label), lp)) {
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
		idx, fallback := -1, -1
		wantMode := st.CastMode
		if st.Kicked {
			wantMode = "kicked"
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
			if st.Op == "activate" && (o.Kind == "ability" || o.Kind == "activate") &&
				strings.Contains(strings.ToLower(o.Label), strings.ToLower(st.Ability)) {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			idx = fallback
		}
		if idx < 0 {
			return harnessf("%s %s not offered: %s", st.Op, st.Card, optionDump(d))
		}
		if err := r.submit(d, []int{idx}, st.Op); err != nil {
			return err
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
				if o.Obj == id && kindOK && strings.Contains(strings.ToLower(o.Label), strings.ToLower(x.Offered.Label)) {
					found = true
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

// runOracleScenario plays one scenario and returns its mismatches (nil =
// pass), a transcript, and the run (for the replay check; its engine is nil
// when setup failed before genesis).
func runOracleScenario(reg *cards.Registry, sc oracleScenario) (fails []string, transcript []string, run *oracleRun) {
	r := &oracleRun{reg: reg, refs: map[string]state.ObjID{}}
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
	for i, st := range sc.Steps {
		if err := r.do(st); err != nil {
			return append(fails, fmt.Sprintf("step %d (%s): %v", i, st.Op, err)), r.log, r
		}
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

func loadOracleFiles(t *testing.T) map[string]oracleFile {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "oracle", "*", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no oracle scenario files: %v", err)
	}
	out := map[string]oracleFile{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		var f oracleFile
		if err := dec.Decode(&f); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if f.Card == "" || f.Family == "" || len(f.Scenarios) == 0 {
			t.Fatalf("%s: card, family and at least one scenario are required", p)
		}
		if fam := filepath.Base(filepath.Dir(p)); fam != f.Family {
			t.Fatalf("%s: family %q does not match its directory %q", p, f.Family, fam)
		}
		out[p] = f
	}
	return out
}

// oracleOps is the closed step vocabulary; TestOracleScenarioFilesWellFormed
// holds every scenario file to it.
var oracleOps = map[string]bool{
	"mana": true, "cast": true, "activate": true, "play": true, "resolve": true,
	"attack": true, "block": true, "pass_to": true, "move": true, "life": true,
}

// TestOracleScenarioFilesWellFormed needs no corpus, so it runs where the
// scenario author works -- a worktree deliberately WITHOUT .cards, so the
// author cannot read a script or fit expectations to engine output. It checks
// the schema (unknown fields are rejected by the decoder), the family
// directory, unique scenario names, the step vocabulary, and ref syntax.
func TestOracleScenarioFilesWellFormed(t *testing.T) {
	files := loadOracleFiles(t)
	checkRef := func(where, ref string) {
		if ref == "" {
			return
		}
		if _, ok := parseSeatRef(ref); ok {
			return
		}
		if _, _, _, _, err := splitRef(ref); err != nil {
			t.Errorf("%s: %v", where, err)
		}
	}
	for p, f := range files {
		names := map[string]bool{}
		for _, sc := range f.Scenarios {
			where := p + ": " + sc.Name
			if sc.Name == "" || names[sc.Name] {
				t.Errorf("%s: scenario name empty or duplicated", where)
			}
			names[sc.Name] = true
			if sc.Why == "" || len(sc.CR) == 0 {
				t.Errorf("%s: every scenario states its Oracle/CR reasoning (why, cr)", where)
			}
			if len(sc.Expect) == 0 {
				hasStepExpect := false
				for _, st := range sc.Steps {
					hasStepExpect = hasStepExpect || len(st.Expect) > 0
				}
				if !hasStepExpect {
					t.Errorf("%s: asserts nothing", where)
				}
			}
			exps := append([]oracleExpect(nil), sc.Expect...)
			for i, st := range sc.Steps {
				if !oracleOps[st.Op] {
					t.Errorf("%s: step %d: unknown op %q", where, i, st.Op)
				}
				for _, ref := range append(append([]string{st.Card, st.Defender}, st.Targets...), st.Attackers...) {
					checkRef(fmt.Sprintf("%s: step %d", where, i), ref)
				}
				exps = append(exps, st.Expect...)
			}
			for _, x := range exps {
				checkRef(where, x.Card)
				checkRef(where, x.TriggerOnStack)
			}
		}
	}
}

// TestOracleAudit runs every Oracle-text scenario against the real corpus.
// Filter with -run 'TestOracleAudit/<Card>/<scenario>'.
func TestOracleAudit(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	files := loadOracleFiles(t)
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	seen := map[string]bool{}
	for _, p := range paths {
		f := files[p]
		c, ok := reg.Lookup(f.Card)
		if !ok {
			t.Errorf("%s: %q not in the corpus", p, f.Card)
			continue
		}
		if f.OracleSHA != "" && f.OracleSHA != oracletext.Digest(c) {
			t.Errorf("%s: Oracle text changed since the scenarios were written (oracle_sha %s, corpus %s): re-derive them", p, f.OracleSHA, oracletext.Digest(c))
		}
		for _, sc := range f.Scenarios {
			key := f.Card + "/" + sc.Name
			seen[key] = true
			t.Run(key, func(t *testing.T) {
				fails, transcript, run := runOracleScenario(reg, sc)
				// Every scenario must also replay from its log alone: the setup
				// and the stand-in ops are logged events, so a divergence here
				// is an engine replay bug (or a runner write outside emit),
				// independent of the Oracle verdict and never ratcheted.
				if run.e != nil {
					if diff := diffGames(run.e.G, replayFromLog(t, run.cfg, run.e.L.Events)); diff != "" {
						t.Errorf("log-only replay differs:\n%s", diff)
					}
				}
				known, isKnown := oracleKnownDivergent[key]
				if os.Getenv("ORACLE_AUDIT_TRACE") != "" {
					t.Logf("transcript:\n    %s", strings.Join(transcript, "\n    "))
				}
				switch {
				case len(fails) == 0 && isKnown:
					t.Errorf("stale known divergence (the scenario now passes; delete its oracleKnownDivergent row): %s", known)
				case len(fails) > 0 && isKnown:
					t.Logf("known divergence: %s\n  observed: %s", known, strings.Join(fails, "\n  observed: "))
				case len(fails) > 0:
					t.Errorf("%s [%s] CR %v\n  Oracle-derived: %s\n  FAIL: %s\n  transcript:\n    %s",
						f.Card, f.Family, sc.CR, sc.Why, strings.Join(fails, "\n  FAIL: "), strings.Join(transcript, "\n    "))
				}
			})
		}
	}
	for key := range oracleKnownDivergent {
		if !seen[key] {
			t.Errorf("oracleKnownDivergent row %q names no scenario", key)
		}
	}
}
