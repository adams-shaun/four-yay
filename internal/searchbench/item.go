package searchbench

// The gorge half of an sb-v1 item (upstream items.py make_item, after its
// bridge.request): a prep payload (scripts/searchbench/prep.py, the Python
// half) is materialised, advanced to the item decision, checked, and
// canonicalised; the stored item is what `searchbench run` rebuilds its
// engines from.

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/searchbench/statespec"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// ItemRequest is a payload's "opts": upstream's bridge request options
// (labels.bridge plus make_item's decideFrom/preLand edits). Other keys are
// kept raw in Payload.Opts.
type ItemRequest struct {
	DecisionPlayer string                `json:"decisionPlayer,omitempty"`
	DecideFrom     *statespec.DecideFrom `json:"decideFrom,omitempty"`
	PreLand        string                `json:"preLand,omitempty"`
}

// Payload is one prep.py candidate response: a Python-side rejection
// (Reject), a Python exception (Error), or the item's Python half.
type Payload struct {
	Row  int    `json:"row"`
	Turn int    `json:"turn"`
	Kind string `json:"kind"`

	Reject  string `json:"reject,omitempty"`
	Error   string `json:"error,omitempty"`
	Ledger  string `json:"ledger,omitempty"`
	Message string `json:"message,omitempty"`

	Split         string            `json:"split,omitempty"`
	Spec          json.RawMessage   `json:"spec,omitempty"`
	Fidelity      json.RawMessage   `json:"fidelity,omitempty"`
	Real          json.RawMessage   `json:"real,omitempty"`
	Worlds        []json.RawMessage `json:"worlds,omitempty"`
	Opts          json.RawMessage   `json:"opts,omitempty"`
	BuildSeed     uint64            `json:"build_seed,omitempty"`
	Human         json.RawMessage   `json:"human,omitempty"`
	Attacked      bool              `json:"attacked,omitempty"`
	WinRateBucket *float64          `json:"win_rate_bucket,omitempty"`
	Rank          *string           `json:"rank,omitempty"`
	OnPlay        bool              `json:"on_play,omitempty"`
	Mirrored      bool              `json:"mirrored,omitempty"`
	Tier          string            `json:"tier,omitempty"`
	Belief        json.RawMessage   `json:"belief,omitempty"`
}

// StoreItem is one item of the item store (items.jsonl.gz): everything
// `searchbench run` needs to rebuild the item's engines, and the canonical
// options and label the manifest seals. Engines are never stored; they are
// rebuilt deterministically from Real/Worlds, BuildSeed and WorldSeeds.
type StoreItem struct {
	ID    string       `json:"id"`
	Row   int          `json:"row"`
	Turn  int          `json:"turn"`
	Type  DecisionType `json:"type"`
	Split Split        `json:"split"`
	Tier  string       `json:"tier"`
	// BuildSeed materialises Real and seeds Reach's bot on every engine
	// (upstream's REAL_SEED + row); WorldSeeds[i] materialises Worlds[i].
	BuildSeed  uint64          `json:"build_seed"`
	WorldSeeds []uint64        `json:"world_seeds"`
	Opts       json.RawMessage `json:"opts"`
	// Options are the canonical option labels (Canon.Labels); Focus is the
	// attack/block creature's spec alias.
	Options     []string `json:"options"`
	Focus       string   `json:"focus,omitempty"`
	Label       []int    `json:"label"`
	LabelStrict []int    `json:"label_strict"`
	LabelNote   string   `json:"label_note,omitempty"`
	Act         bool     `json:"act"`
	Sequence    uint64   `json:"sequence"`

	OnPlay        bool     `json:"on_play"`
	Mirrored      bool     `json:"mirrored"`
	WinRateBucket *float64 `json:"win_rate_bucket,omitempty"`
	Rank          *string  `json:"rank,omitempty"`
	Attacked      bool     `json:"attacked"`

	Spec     json.RawMessage   `json:"spec"`
	Real     json.RawMessage   `json:"real"`
	Worlds   []json.RawMessage `json:"worlds"`
	Fidelity json.RawMessage   `json:"fidelity,omitempty"`
	Human    json.RawMessage   `json:"human,omitempty"`
	Belief   json.RawMessage   `json:"belief,omitempty"`

	PrefixDigest, PublicStateDigest, LegalOptionsDigest string
}

// Request decodes the item's request options.
func (s *StoreItem) Request() (ItemRequest, error) { return parseRequest(s.Opts) }

func parseRequest(raw json.RawMessage) (ItemRequest, error) {
	var r ItemRequest
	if len(raw) == 0 {
		return r, refuse("opts", "no request options")
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, refuse("opts", "%v", err)
	}
	if r.DecideFrom == nil || r.DecideFrom.Turn < 1 {
		return r, refuse("opts", "no decideFrom turn")
	}
	return r, nil
}

// RequestReachOptions is ReachOptions from an item's request options
// (upstream's decisionPlayer, decideFrom.turn and preLand), with the bot
// seeded by seed.
func RequestReachOptions(r ItemRequest, seed uint64) ReachOptions {
	o := ReachOptions{DecisionPlayer: SeatID("A"), Turn: int32(r.DecideFrom.Turn), PreLand: r.PreLand, BotSeed: seed}
	if r.DecisionPlayer != "" {
		o.DecisionPlayer = SeatID(r.DecisionPlayer)
	}
	return o
}

// Position is one engine of an item at its root decision.
type Position struct {
	M       *Materialized
	Reached *Reached
}

// Positioned is an item's real position, its canon and its worlds.
type Positioned struct {
	Real   Position
	Canon  *Canon
	Worlds []Position

	pin   []string // the decision player's library cards the walk to the root took, top first
	seat  string
	specs []*statespec.Spec
}

// WorldEngines lists the worlds' engines.
func (p *Positioned) WorldEngines() []*rules.Engine {
	out := make([]*rules.Engine, len(p.Worlds))
	for i, w := range p.Worlds {
		out[i] = w.M.Engine
	}
	return out
}

// Stage names the part of the gorge half a refusal came from, the prefix
// of its ledger key.
const (
	StageBuild  = "build"  // materialising the real world
	StageReach  = "reach"  // advancing the real world to the item decision
	StageCanon  = "canon"  // canonical options and labels on the real world
	StageWorlds = "worlds" // the eight belief worlds
)

// ItemRefusal is a gorge-side rejection: the stage and the refusal.
type ItemRefusal struct {
	Stage string
	Err   error
}

func (r *ItemRefusal) Error() string { return r.Stage + ": " + r.Err.Error() }

// Key is the rejection's summary text, upstream's Rejected text where one
// exists: "reached <KIND> at <STEP>", "wrong turn", "trivial",
// "label: <why>", "defender closed", "hold label", ...; a materialisation
// failure is "build: <code>: <detail>" (upstream's bridge "build: <error>");
// a world failure "worlds: <code>" (gorge only: upstream's bridge builds the
// worlds only at run time).
func (r *ItemRefusal) Key() string {
	var rf *Refusal
	if !errors.As(r.Err, &rf) {
		return r.Stage + ": error " + r.Err.Error()
	}
	switch r.Stage {
	case StageBuild:
		return "build: " + rf.Error()
	case StageWorlds:
		return "worlds: " + rf.Code
	}
	switch rf.Code {
	case "reached":
		return "reached " + rf.Detail
	case "label", "spec invalid", "stage", "unknown card":
		return rf.Error()
	}
	return rf.Code
}

// PositionItem rebuilds an item's engines at its root decision: the real
// world (Real, BuildSeed), its canon, and the first nWorlds belief worlds
// (Worlds[i], WorldSeeds[i]), every engine advanced with the item's request
// options and a bot seeded by BuildSeed.
func PositionItem(reg *cards.Registry, s *StoreItem, nWorlds int) (*Positioned, error) {
	req, err := s.Request()
	if err != nil {
		return nil, &ItemRefusal{StageBuild, err}
	}
	return positionRaw(reg, s.Type, req, s.Real, s.BuildSeed, s.Worlds, s.WorldSeeds, nWorlds)
}

func positionRaw(reg *cards.Registry, kind DecisionType, req ItemRequest, real json.RawMessage, seed uint64, worlds []json.RawMessage, worldSeeds []uint64, nWorlds int) (*Positioned, error) {
	o := RequestReachOptions(req, seed)
	spec, err := statespec.Parse(real)
	if err != nil {
		return nil, &ItemRefusal{StageBuild, refuse("spec decode", "%v", err)}
	}
	// Every world is parsed, whatever nWorlds, so the library sizes (and so
	// the real engine) do not depend on how many worlds an arm reads.
	wspecs := make([]*statespec.Spec, len(worlds))
	for i, raw := range worlds {
		if wspecs[i], err = statespec.Parse(raw); err != nil {
			return nil, &ItemRefusal{StageWorlds, refuse("spec decode", "world %d: %v", i, err)}
		}
	}
	equalLibraries(append([]*statespec.Spec{spec}, wspecs...))
	m, err := Materialize(reg, spec, seed)
	if err != nil {
		return nil, &ItemRefusal{StageBuild, err}
	}
	dp := o.DecisionPlayer
	var staged []string
	for _, id := range m.Engine.G.Zone(state.ZLibrary, dp) {
		staged = append(staged, objName(m.Engine, id))
	}
	r, err := Reach(m.Engine, kind, o)
	if err != nil {
		return nil, &ItemRefusal{StageReach, err}
	}
	p := &Positioned{Real: Position{M: m, Reached: r}}
	if p.Canon, err = BuildCanon(m, r); err != nil {
		return nil, &ItemRefusal{StageCanon, err}
	}
	// The cards that left the decision player's library on the way to the
	// root (the turn's draw, a surveil, a mill) are pinned on top of every
	// world's library in the real order: that seat saw them go, so a world
	// that dealt it different ones would not share its observation. The
	// rest of its library is the world's own shuffle.
	p.pin = staged[:len(staged)-len(m.Engine.G.Zone(state.ZLibrary, dp))]
	p.seat = SeatName(dp)
	p.specs = wspecs
	if err := p.addWorlds(reg, kind, o, worldSeeds, nWorlds); err != nil {
		return nil, err
	}
	return p, nil
}

// equalLibraries trims every spec's library to the smallest library among
// them, seat by seat (StateSpec librarySize: the bottom cards of the
// ordered library go). A library's size is public, and the belief model's
// opponent decklists differ in length (40, 41, 42 cards), so the real world
// and its belief worlds would otherwise not share the searching seat's
// observation. The trimmed cards are the bottom of a shuffled library.
func equalLibraries(specs []*statespec.Spec) {
	for _, seat := range statespec.Seats {
		low := -1
		for _, s := range specs {
			if n := libraryCount(s, seat); low < 0 || n < low {
				low = n
			}
		}
		for _, s := range specs {
			if libraryCount(s, seat) > low {
				p := s.Players[seat]
				n := low
				p.LibrarySize = &n
				s.Players[seat] = p
			}
		}
	}
}

// libraryCount is the size of the library Materialize stages for seat.
func libraryCount(s *statespec.Spec, seat string) int {
	p := s.Players[seat]
	left := map[string]int{} // lookup only
	for _, n := range s.Owned()[seat] {
		left[n]++
	}
	rest := 0
	for _, n := range p.Decklist {
		if left[n] > 0 {
			left[n]--
			continue
		}
		rest++
	}
	lib := rest - p.HandUnknown + len(p.LibraryTop)
	if p.LibrarySize != nil && *p.LibrarySize < lib {
		lib = *p.LibrarySize
	}
	return lib
}

// addWorlds materialises and reaches the first n belief worlds, the
// decision player's pinned library top in place.
func (p *Positioned) addWorlds(reg *cards.Registry, kind DecisionType, o ReachOptions, worldSeeds []uint64, n int) error {
	if n > len(p.specs) || n > len(worldSeeds) {
		return &ItemRefusal{StageWorlds, refuse("count", "%d worlds, %d seeds, want %d", len(p.specs), len(worldSeeds), n)}
	}
	for i := 0; i < n; i++ {
		spec := p.specs[i]
		ps := spec.Players[p.seat]
		if len(p.pin) > len(ps.LibraryTop) {
			if !slices.Equal(ps.LibraryTop, p.pin[:len(ps.LibraryTop)]) {
				return &ItemRefusal{StageWorlds, refuse("library top", "world %d: %s libraryTop %q, real %q", i, p.seat, ps.LibraryTop, p.pin)}
			}
			ps.LibraryTop = append([]string(nil), p.pin...)
			spec.Players[p.seat] = ps
		}
		w, err := func() (Position, error) {
			m, err := Materialize(reg, spec, worldSeeds[i])
			if err != nil {
				return Position{}, err
			}
			r, err := Reach(m.Engine, kind, o)
			return Position{M: m, Reached: r}, err
		}()
		if err != nil {
			var rf *Refusal
			if errors.As(err, &rf) {
				return &ItemRefusal{StageWorlds, &Refusal{Code: rf.Code, Detail: fmt.Sprintf("world %d: %s", i, rf.Detail)}}
			}
			return &ItemRefusal{StageWorlds, err}
		}
		p.Worlds = append(p.Worlds, w)
	}
	return nil
}

// PrepareItem is the gorge half of make_item for one Python payload:
//
//   - materialise the real world (build seed), reach the item decision
//     (upstream's decision checks: turn, kind, step), build the canonical
//     options and match the labels (trivial, label, hold label, nothing
//     castable, defender closed, human unmatched);
//   - materialise and reach all eight belief worlds (world seeds);
//   - check every world's root is observation-identical to the real root
//     (RunArm's precondition) and re-dealable (IS-MCTS's).
//
// A refusal is an *ItemRefusal. The returned item has no ID yet.
func PrepareItem(reg *cards.Registry, p *Payload, worldSeeds []uint64) (*StoreItem, error) {
	kind := DecisionType(p.Kind)
	req, err := parseRequest(p.Opts)
	if err != nil {
		return nil, &ItemRefusal{StageBuild, err}
	}
	if len(p.Worlds) != WorldCount {
		return nil, &ItemRefusal{StageWorlds, refuse("count", "%d worlds, want %d", len(p.Worlds), WorldCount)}
	}
	pos, err := positionRaw(reg, kind, req, p.Real, p.BuildSeed, p.Worlds, worldSeeds, 0)
	if err != nil {
		return nil, err
	}
	lab, err := LabelItem(pos.Real.M, pos.Canon)
	if err != nil {
		return nil, &ItemRefusal{StageCanon, err}
	}
	if err := pos.addWorlds(reg, kind, RequestReachOptions(req, p.BuildSeed), worldSeeds, WorldCount); err != nil {
		return nil, err
	}
	if err := CheckRoots(pos.Real.M.Engine, pos.WorldEngines()); err != nil {
		return nil, &ItemRefusal{StageWorlds, refuse("observation", "%v", err)}
	}
	actor := pos.Real.Reached.Decision.Player
	for i, w := range pos.Worlds {
		in, _, err := OneFrameRedealInput(w.M.Engine, actor, WorldDecks(w.M.Engine))
		if err != nil {
			return nil, &ItemRefusal{StageWorlds, refuse("redeal", "world %d: %v", i, err)}
		}
		if _, why := searchprobe.NewRedealer(in.Setup, in.History, in.Known, in.Base); why != "" {
			return nil, &ItemRefusal{StageWorlds, refuse("redeal", "world %d: %s", i, why)}
		}
	}
	frame, err := searchprobe.NewCollector(actor).Capture(pos.Real.M.Engine, nil)
	if err != nil {
		return nil, &ItemRefusal{StageWorlds, refuse("observation", "capture: %v", err)}
	}
	public, err := json.Marshal(struct {
		Board      json.RawMessage
		Identities []searchprobe.Identity
		Decision   *searchprobe.ObservedDecision
	}{frame.Board, frame.Identities, frame.Decision})
	if err != nil {
		return nil, err
	}
	labels := pos.Canon.Labels()
	seq := pos.Real.Reached.Decision.Seq
	if seq == 0 {
		seq = 1 // the manifest's Sequence is non-zero; a staged engine's first decision is Seq 0
	}
	it := &StoreItem{
		Row: p.Row, Turn: p.Turn, Type: kind, Split: Split(p.Split), Tier: p.Tier,
		BuildSeed: p.BuildSeed, WorldSeeds: append([]uint64(nil), worldSeeds...), Opts: p.Opts,
		Options: labels, Focus: pos.Canon.FocusAlias,
		Label: lab.Members, LabelStrict: lab.Strict, LabelNote: lab.Note, Act: lab.Act, Sequence: uint64(seq),
		OnPlay: p.OnPlay, Mirrored: p.Mirrored, WinRateBucket: p.WinRateBucket, Rank: p.Rank, Attacked: p.Attacked,
		Spec: p.Spec, Real: p.Real, Worlds: p.Worlds, Fidelity: p.Fidelity, Human: p.Human, Belief: p.Belief,
		PrefixDigest:       sha256Hex(p.Real),
		PublicStateDigest:  sha256Hex(public),
		LegalOptionsDigest: sha256Hex([]byte(strings.Join(labels, "\n"))),
	}
	return it, nil
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// ManifestItem is the item's sealed manifest record. gameID and draftID
// name the 17lands game (1:1 with Row) and its draft (a digest, never the
// raw draft id).
func (s *StoreItem) ManifestItem(gameID, draftID string) Item {
	alts := func(v []int) [][]int {
		out := make([][]int, len(v))
		for i, c := range v {
			out[i] = []int{c}
		}
		return out
	}
	return Item{
		ID: s.ID, GameID: gameID, DraftID: draftID, Row: s.Row, Split: s.Split, Type: s.Type,
		Seat: 0, Turn: s.Turn, Sequence: s.Sequence, Tier: s.Tier, OnPlay: s.OnPlay, Mirrored: s.Mirrored,
		Options: append([]string(nil), s.Options...), Focus: s.Focus,
		PrefixDigest: s.PrefixDigest, PublicStateDigest: s.PublicStateDigest, LegalOptionsDigest: s.LegalOptionsDigest,
		Label:      Label{Alternatives: alts(s.Label), Strict: alts(s.LabelStrict), Act: s.Act},
		WorldSeeds: append([]uint64(nil), s.WorldSeeds...),
	}
}

// WriteStore writes items as one gzipped JSON line each (no name, mtime 0,
// so identical items give identical bytes).
func WriteStore(path string, items []*StoreItem) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	w := bufio.NewWriterSize(zw, 1<<20)
	enc := json.NewEncoder(w)
	for _, it := range items {
		if err := enc.Encode(it); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ReadStore reads an item store, keyed by item ID.
func ReadStore(path string) (map[string]*StoreItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bufio.NewReaderSize(zr, 1<<20))
	dec.DisallowUnknownFields()
	out := map[string]*StoreItem{}
	for {
		var it StoreItem
		if err := dec.Decode(&it); errors.Is(err, io.EOF) {
			return out, nil
		} else if err != nil {
			return nil, fmt.Errorf("searchbench: item store %s: %w", path, err)
		}
		if _, dup := out[it.ID]; dup || it.ID == "" {
			return nil, fmt.Errorf("searchbench: item store %s: empty or duplicate id %q", path, it.ID)
		}
		out[it.ID] = &it
	}
}

// CardNames lists, sorted and distinct, every card name the items' real
// and belief specs seat (each seat's decklist and every named card in a
// zone, plus a split/DFC name's front face, which LookupCard falls back
// to): the pool a subset registry (cards.OpenCorpusFor) must hold to
// rebuild them. Tokens are not listed; a subset registry keeps them all.
func CardNames(items []*StoreItem) ([]string, error) {
	seen := map[string]bool{} // membership only; the output is sorted
	add := func(n string) {
		if n == "" {
			return
		}
		seen[n] = true
		if i := strings.Index(n, " // "); i > 0 {
			seen[n[:i]] = true
		}
	}
	for _, it := range items {
		for _, raw := range append([]json.RawMessage{it.Real}, it.Worlds...) {
			spec, err := statespec.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("searchbench: %s: %w", it.ID, err)
			}
			for _, p := range spec.Players {
				for _, list := range [][]string{p.Decklist, p.Hand, p.Graveyard, p.Exile, p.LibraryTop} {
					for _, n := range list {
						add(n)
					}
				}
				for _, perm := range p.Battlefield {
					add(perm.Name)
				}
			}
			for _, st := range spec.Stack {
				add(st.Card)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	slices.Sort(out)
	return out, nil
}

// CheckRoots checks every world is at the real engine's root decision and
// observes exactly what the real engine does: the precondition RunArm
// checks for its worlds.
func CheckRoots(real *rules.Engine, worlds []*rules.Engine) error {
	_, err := checkRoots(real, real, worlds)
	return err
}
