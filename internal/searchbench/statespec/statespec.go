// Package statespec is a Go mirror of Draft Zero's StateSpec v1
// (draftzero/gameplay/statespec.py at the pinned upstream commit): one JSON
// description of a two-player Magic position. Seats are "A" and "B"; turn is
// the global 1-based turn; card names are XMage names (FDN names equal
// Forge's); tokens come as an XMage tokenClass or a token name.
//
// Decoding is strict: an unknown field anywhere is an error (upstream
// ignores them for forward compatibility; a replication must not silently
// drop a field it does not understand). The one exception is Labels, which
// upstream defines as an open pass-through dict: it is kept raw, and
// ParseLabels types the keys the benchmark reads. Defaults are upstream's
// dataclass defaults (count 1, life 20, version 1, ...), because upstream's
// to_dict prunes empty and None values before writing.
package statespec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// Version is the schema version this mirror reads.
const Version = 1

var (
	Seats         = []string{"A", "B"}
	EnterModes    = []string{"PRIORITY_FRESH", "BEGIN_STEP", "PRIORITY_HELD"}
	DeckSources   = []string{"exact", "belief", "placeholder"}
	Sources       = []string{"17lands", "arena", "synthetic"}
	Tiers         = []string{"T0", "T1", "T2", "T3"}
	PhaseSteps    = map[string][]string{"BEGINNING": {"UNTAP", "UPKEEP", "DRAW"}, "PRECOMBAT_MAIN": {"PRECOMBAT_MAIN"}, "COMBAT": {"BEGIN_COMBAT", "DECLARE_ATTACKERS", "DECLARE_BLOCKERS", "FIRST_COMBAT_DAMAGE", "COMBAT_DAMAGE", "END_COMBAT"}, "POSTCOMBAT_MAIN": {"POSTCOMBAT_MAIN"}, "END": {"END_TURN", "CLEANUP"}}
	phaseOrder    = []string{"BEGINNING", "PRECOMBAT_MAIN", "COMBAT", "POSTCOMBAT_MAIN", "END"}
	manaPoolRegex = regexp.MustCompile(`^[WUBRGC]*$`)
)

// Steps is every step in turn order.
func Steps() []string {
	var out []string
	for _, ph := range phaseOrder {
		out = append(out, PhaseSteps[ph]...)
	}
	return out
}

// Perm is one battlefield entry (a card or a token), listed under its
// controller's seat.
type Perm struct {
	Name       string          `json:"name,omitempty"`
	Set        string          `json:"set,omitempty"`
	Number     string          `json:"number,omitempty"`
	Token      string          `json:"token,omitempty"`
	TokenClass string          `json:"tokenClass,omitempty"`
	ID         string          `json:"id,omitempty"`
	Count      int             `json:"count"`
	Tapped     bool            `json:"tapped"`
	Sick       bool            `json:"sick"`
	Damage     int             `json:"damage"`
	Counters   map[string]*int `json:"counters,omitempty"`
	AttachTo   string          `json:"attachTo,omitempty"`
	FaceDown   bool            `json:"faceDown"`
	Owner      string          `json:"owner,omitempty"`
}

// IsToken reports whether the entry is a token.
func (p Perm) IsToken() bool { return p.Token != "" || p.TokenClass != "" }

// What names the entry for messages.
func (p Perm) What() string {
	switch {
	case p.Name != "":
		return p.Name
	case p.TokenClass != "":
		return p.TokenClass
	}
	return p.Token
}

func (p *Perm) UnmarshalJSON(b []byte) error {
	type raw Perm
	r := raw{Count: 1}
	if err := strict(b, &r); err != nil {
		return err
	}
	*p = Perm(r)
	return nil
}

// PlayerState is one seat.
type PlayerState struct {
	Name           string   `json:"name"`
	Life           int      `json:"life"`
	Decklist       []string `json:"decklist"`
	DecklistSource string   `json:"decklistSource"`
	LandsPlayed    int      `json:"landsPlayed"`
	Hand           []string `json:"hand,omitempty"`
	HandUnknown    int      `json:"handUnknown"`
	Graveyard      []string `json:"graveyard,omitempty"`
	Exile          []string `json:"exile,omitempty"`
	LibraryTop     []string `json:"libraryTop,omitempty"`
	LibrarySize    *int     `json:"librarySize,omitempty"`
	ManaPool       *string  `json:"manaPool,omitempty"`
	Battlefield    []Perm   `json:"battlefield"`
}

func (p *PlayerState) UnmarshalJSON(b []byte) error {
	type raw PlayerState
	r := raw{Life: 20, DecklistSource: "exact"}
	if err := strict(b, &r); err != nil {
		return err
	}
	*p = PlayerState(r)
	return nil
}

// StackItem is one stack object, listed bottom to top.
type StackItem struct {
	Controller string   `json:"controller"`
	Card       string   `json:"card"`
	Targets    []string `json:"targets,omitempty"`
}

func (s *StackItem) UnmarshalJSON(b []byte) error {
	type raw StackItem
	var r raw
	if err := strict(b, &r); err != nil {
		return err
	}
	*s = StackItem(r)
	return nil
}

// Attack is one declared attacker.
type Attack struct {
	Attacker string `json:"attacker"`
	Defender string `json:"defender"`
}

func (a *Attack) UnmarshalJSON(b []byte) error {
	type raw Attack
	var r raw
	if err := strict(b, &r); err != nil {
		return err
	}
	*a = Attack(r)
	return nil
}

// Block is one declared blocker.
type Block struct {
	Blocker  string `json:"blocker"`
	Attacker string `json:"attacker"`
}

func (a *Block) UnmarshalJSON(b []byte) error {
	type raw Block
	var r raw
	if err := strict(b, &r); err != nil {
		return err
	}
	*a = Block(r)
	return nil
}

// Provenance records where a spec came from.
type Provenance struct {
	Source  string   `json:"source"`
	Ref     string   `json:"ref,omitempty"`
	Tier    *string  `json:"tier,omitempty"`
	Flags   []string `json:"flags,omitempty"`
	Sampled []string `json:"sampled,omitempty"`
}

func (p *Provenance) UnmarshalJSON(b []byte) error {
	type raw Provenance
	r := raw{Source: "synthetic"}
	if err := strict(b, &r); err != nil {
		return err
	}
	*p = Provenance(r)
	return nil
}

// Spec is a StateSpec v1.
type Spec struct {
	Version        int                    `json:"version"`
	Comment        string                 `json:"comment,omitempty"`
	Turn           int                    `json:"turn"`
	ActivePlayer   string                 `json:"activePlayer"`
	Phase          string                 `json:"phase"`
	Step           string                 `json:"step"`
	EnterMode      string                 `json:"enterMode"`
	PriorityPlayer *string                `json:"priorityPlayer,omitempty"`
	PassedPlayers  []string               `json:"passedPlayers,omitempty"`
	StartingPlayer *string                `json:"startingPlayer,omitempty"`
	Players        map[string]PlayerState `json:"players"`
	Stack          []StackItem            `json:"stack,omitempty"`
	Attackers      []Attack               `json:"attackers,omitempty"`
	Blockers       []Block                `json:"blockers,omitempty"`
	Provenance     Provenance             `json:"provenance"`
	// Labels is upstream's open pass-through dict; see ParseLabels.
	Labels map[string]json.RawMessage `json:"labels,omitempty"`
}

func (s *Spec) UnmarshalJSON(b []byte) error {
	type raw Spec
	r := raw{Version: Version, Turn: 1, ActivePlayer: "A", Phase: "PRECOMBAT_MAIN",
		Step: "PRECOMBAT_MAIN", EnterMode: "PRIORITY_FRESH", Provenance: Provenance{Source: "synthetic"}}
	if err := strict(b, &r); err != nil {
		return err
	}
	*s = Spec(r)
	return nil
}

// Decode reads exactly one spec from r, strictly.
func Decode(r io.Reader) (*Spec, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var s Spec
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("statespec: trailing data after the spec")
	}
	return &s, nil
}

// Parse reads one spec from b, strictly.
func Parse(b []byte) (*Spec, error) { return Decode(bytes.NewReader(b)) }

func strict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("statespec: trailing data")
	}
	return nil
}

// IsPartial reports whether some hidden card is still unfilled.
func (s *Spec) IsPartial() bool {
	for _, seat := range Seats {
		if s.Players[seat].HandUnknown > 0 {
			return true
		}
	}
	return false
}

// Other is the other seat.
func Other(seat string) string {
	if seat == "A" {
		return "B"
	}
	return "A"
}

// Alias is one resolved battlefield alias: "<seat>:<id>" or
// "<seat>:<id>#k" names copy K (1-based) of entry Index in seat's
// battlefield. The bare "<seat>:<id>" of a count>1 entry is copy 1.
type Alias struct {
	Seat  string
	Index int
	Copy  int
}

// Aliases maps every alias the battlefield defines, in upstream's scheme.
func (s *Spec) Aliases() map[string]Alias {
	out := map[string]Alias{}
	for _, seat := range Seats {
		for i, p := range s.Players[seat].Battlefield {
			if p.ID == "" {
				continue
			}
			out[seat+":"+p.ID] = Alias{Seat: seat, Index: i, Copy: 1}
			if p.Count > 1 {
				for k := 1; k <= p.Count; k++ {
					out[fmt.Sprintf("%s:%s#%d", seat, p.ID, k)] = Alias{Seat: seat, Index: i, Copy: k}
				}
			}
		}
	}
	return out
}

// Resolve returns the alias a "<seat>:<id>[#k]" reference names.
func (s *Spec) Resolve(ref string) (Alias, bool) {
	a, ok := s.Aliases()[ref]
	return a, ok
}

func in(v string, set []string) bool {
	for _, x := range set {
		if x == v {
			return true
		}
	}
	return false
}

func multiset(xs []string) map[string]int {
	out := map[string]int{}
	for _, x := range xs {
		out[x]++
	}
	return out
}

// Owned lists, per seat, the cards the seat owns outside its library:
// its hand, graveyard, exile and known library top, the non-token
// permanents it owns on either battlefield, and the stack items it
// controls (upstream's accounting).
func (s *Spec) Owned() map[string][]string {
	owned := map[string][]string{"A": nil, "B": nil}
	for _, seat := range Seats {
		p := s.Players[seat]
		owned[seat] = append(owned[seat], p.Hand...)
		owned[seat] = append(owned[seat], p.Graveyard...)
		owned[seat] = append(owned[seat], p.Exile...)
		owned[seat] = append(owned[seat], p.LibraryTop...)
	}
	for _, seat := range Seats {
		for _, perm := range s.Players[seat].Battlefield {
			if perm.Name == "" || perm.IsToken() {
				continue
			}
			o := seat
			if in(perm.Owner, Seats) {
				o = perm.Owner
			}
			for k := 0; k < perm.Count; k++ {
				owned[o] = append(owned[o], perm.Name)
			}
		}
	}
	for _, it := range s.Stack {
		if in(it.Controller, Seats) && it.Card != "" {
			owned[it.Controller] = append(owned[it.Controller], it.Card)
		}
	}
	return owned
}

// Validate mirrors upstream StateSpec.validate(): the problems that would
// make the spec unbuildable. Empty is fine. Messages follow upstream's.
func (s *Spec) Validate() []string {
	var errs []string
	add := func(f string, a ...any) { errs = append(errs, fmt.Sprintf(f, a...)) }
	if s.Version != Version {
		add("version %d != %d", s.Version, Version)
	}
	if len(s.Players) != 2 || s.Players == nil {
		keys := make([]string, 0, len(s.Players))
		for k := range s.Players {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		add("players must be exactly [A B], got %v", keys)
		return errs
	}
	for _, seat := range Seats {
		if _, ok := s.Players[seat]; !ok {
			add("players must be exactly [A B]")
			return errs
		}
	}
	if !in(s.ActivePlayer, Seats) {
		add("activePlayer %q", s.ActivePlayer)
	}
	if s.StartingPlayer != nil && !in(*s.StartingPlayer, Seats) {
		add("startingPlayer %q", *s.StartingPlayer)
	}
	if steps, ok := PhaseSteps[s.Phase]; !ok {
		add("phase %q", s.Phase)
	} else if !in(s.Step, steps) {
		add("step %q is not in phase %s", s.Step, s.Phase)
	}
	if !in(s.EnterMode, EnterModes) {
		add("enterMode %q", s.EnterMode)
	}
	if s.EnterMode == "PRIORITY_HELD" && (s.PriorityPlayer == nil || !in(*s.PriorityPlayer, Seats)) {
		add("PRIORITY_HELD needs priorityPlayer")
	}
	if s.PriorityPlayer != nil && !in(*s.PriorityPlayer, Seats) {
		add("priorityPlayer %q", *s.PriorityPlayer)
	}
	for _, p := range s.PassedPlayers {
		if !in(p, Seats) {
			add("passedPlayers entry %q", p)
		}
	}
	if s.Turn < 1 {
		add("turn %d", s.Turn)
	}
	if s.Turn == 1 && s.StartingPlayer != nil && *s.StartingPlayer != s.ActivePlayer {
		add("turn 1 belongs to the starting player: startingPlayer %s != activePlayer %s", *s.StartingPlayer, s.ActivePlayer)
	}
	if s.Step == "UNTAP" {
		add("UNTAP cannot be entered: use the previous turn's END_TURN with PRIORITY_FRESH")
	}
	if s.Step == "CLEANUP" && s.EnterMode != "BEGIN_STEP" {
		add("CLEANUP has no priority window: use BEGIN_STEP at CLEANUP, or END_TURN")
	}
	if s.EnterMode == "BEGIN_STEP" && len(s.Stack) > 0 {
		add("BEGIN_STEP with a non-empty stack: a step only begins once the stack is empty")
	}
	if !in(s.Provenance.Source, Sources) {
		add("provenance.source %q", s.Provenance.Source)
	}
	if s.Provenance.Tier != nil && !in(*s.Provenance.Tier, Tiers) {
		add("provenance.tier %q", *s.Provenance.Tier)
	}
	aliases := s.Aliases()
	owned := s.Owned()
	for _, seat := range Seats {
		p := s.Players[seat]
		seen := map[string]bool{}
		if !in(p.DecklistSource, DeckSources) {
			add("%s.decklistSource %q", seat, p.DecklistSource)
		}
		if len(p.Decklist) < 40 {
			add("%s.decklist has %d cards; XMage needs at least 40", seat, len(p.Decklist))
		}
		if p.HandUnknown < 0 {
			add("%s.handUnknown < 0", seat)
		}
		if p.LandsPlayed < 0 {
			add("%s.landsPlayed < 0", seat)
		}
		if p.LibrarySize != nil && *p.LibrarySize < len(p.LibraryTop) {
			add("%s.librarySize %d < libraryTop count %d", seat, *p.LibrarySize, len(p.LibraryTop))
		}
		if p.ManaPool != nil && !manaPoolRegex.MatchString(*p.ManaPool) {
			add("%s.manaPool %q (use W U B R G C)", seat, *p.ManaPool)
		}
		pool := multiset(p.Decklist)
		short := map[string]int{}
		for c, n := range multiset(owned[seat]) {
			if n > pool[c] {
				short[c] = n - pool[c]
			}
		}
		if len(short) > 0 {
			names := make([]string, 0, len(short))
			for c := range short {
				names = append(names, fmt.Sprintf("%s:%d", c, short[c]))
			}
			sort.Strings(names)
			add("%s: cards named in zones but missing from decklist: {%s}", seat, strings.Join(names, ", "))
		}
		remaining := len(p.Decklist) - len(owned[seat])
		if p.HandUnknown > max(0, remaining) {
			add("%s.handUnknown %d > %d cards left in the library", seat, p.HandUnknown, remaining)
		}
		for _, perm := range p.Battlefield {
			what := perm.What()
			if perm.Name == "" && !perm.IsToken() {
				add("%s: battlefield entry with no name/token/tokenClass", seat)
			}
			if perm.AttachTo != "" {
				if _, ok := aliases[perm.AttachTo]; !ok {
					add("%s: attachTo %q names no permanent alias", seat, perm.AttachTo)
				}
			}
			if perm.Count < 1 {
				add("%s: battlefield count %d", seat, perm.Count)
			}
			if perm.Damage < 0 {
				add("%s: negative damage on %s", seat, what)
			}
			if perm.ID != "" && seen[seat+":"+perm.ID] {
				add("%s: duplicate alias %q", seat, perm.ID)
			}
			if perm.ID != "" {
				seen[seat+":"+perm.ID] = true
			}
			if perm.AttachTo != "" && perm.Count > 1 {
				add("%s: attachTo with count > 1 on %s", seat, what)
			}
			for _, v := range perm.Counters {
				if v == nil || *v < 0 {
					add("%s: counters on %s must be >= 0", seat, what)
					break
				}
			}
			if perm.Owner != "" && !in(perm.Owner, Seats) {
				add("%s: owner %q on %s", seat, perm.Owner, what)
			}
			if perm.Owner != "" && perm.IsToken() {
				add("%s: owner on a token (%s) is not supported", seat, what)
			}
		}
	}
	other := Other(s.ActivePlayer)
	for _, a := range s.Attackers {
		if _, ok := aliases[a.Attacker]; !ok {
			add("attacker %q names no permanent alias", a.Attacker)
		}
		if !strings.HasPrefix(a.Attacker, s.ActivePlayer+":") {
			add("attacker %q is not controlled by the active player", a.Attacker)
		}
		if _, ok := aliases[a.Defender]; a.Defender != "player:"+other && !ok {
			add("defender %q must be player:%s or a permanent alias", a.Defender, other)
		}
	}
	for _, b := range s.Blockers {
		_, okB := aliases[b.Blocker]
		_, okA := aliases[b.Attacker]
		if !okB || !okA {
			add("block %q->%q names no permanent alias", b.Blocker, b.Attacker)
		}
	}
	steps := Steps()
	at := index(steps, s.Step)
	if len(s.Attackers) > 0 && (s.Phase != "COMBAT" || at < index(steps, "DECLARE_ATTACKERS") ||
		(s.Step == "DECLARE_ATTACKERS" && s.EnterMode == "BEGIN_STEP")) {
		add("attackers need a position after attackers were declared (got %s/%s)", s.Step, s.EnterMode)
	}
	if len(s.Blockers) > 0 {
		if len(s.Attackers) == 0 {
			add("blockers without attackers")
		}
		if s.Phase != "COMBAT" || at < index(steps, "DECLARE_BLOCKERS") ||
			(s.Step == "DECLARE_BLOCKERS" && s.EnterMode == "BEGIN_STEP") {
			add("blockers need a position after blockers were declared (got %s/%s)", s.Step, s.EnterMode)
		}
	}
	for _, it := range s.Stack {
		if !in(it.Controller, Seats) {
			add("stack controller %q", it.Controller)
		}
		if it.Card == "" {
			add("stack item with no card")
		}
		for _, t := range it.Targets {
			if _, ok := aliases[t]; t != "player:A" && t != "player:B" && !ok {
				add("stack target %q names no permanent alias or player", t)
			}
		}
	}
	return errs
}

func index(xs []string, v string) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

// StartingSeat is startingPlayer, or the seat turn parity gives (the
// player on the play takes the odd turns).
func (s *Spec) StartingSeat() string {
	if s.StartingPlayer != nil {
		return *s.StartingPlayer
	}
	if s.Turn%2 == 1 {
		return s.ActivePlayer
	}
	return Other(s.ActivePlayer)
}
