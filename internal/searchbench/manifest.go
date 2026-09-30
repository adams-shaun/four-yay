// Package searchbench owns the immutable input contract for the native Gorge
// search benchmark. It deliberately contains metadata and validation only:
// raw 17lands rows and reconstructed engine states stay in the training
// store, never in the repository.
package searchbench

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	ManifestKind          = "gorge-searchbench-manifest"
	ManifestSchemaVersion = 2
	WorldCount            = 8
)

// SBV1WorldSeeds is the deterministic nested belief-world schedule. The
// first world is PIMC-1's world, the first four are PIMC-4's, and all eight
// feed IS-MCTS; changing this order changes an experiment arm.
func SBV1WorldSeeds() []uint64 {
	const seed uint64 = 7
	out := make([]uint64, WorldCount)
	for i := range out {
		out[i] = splitMix64(seed + uint64(i))
	}
	return out
}

func splitMix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// SBV1Selection is the published Draft Zero sb-v1 population: 1,000 test
// items and 300 development items, with no game contributing more than two.
// Builders must use these counts for a claim of protocol replication.
var SBV1Selection = Selection{Test: 1000, Dev: 300, MinimumGameWinRate: .60, MinimumGames: 100, MaximumItemsPerGame: 2}

// SBV1Quotas is the exact split by decision type, in fixed report order.
var SBV1Quotas = map[Split]map[DecisionType]int{
	SplitTest: {DecisionSpell: 375, DecisionHold: 125, DecisionAttack: 300, DecisionBlock: 200},
	SplitDev:  {DecisionSpell: 110, DecisionHold: 40, DecisionAttack: 90, DecisionBlock: 60},
}

type Split string

const (
	SplitDev  Split = "dev"
	SplitTest Split = "test"
)

type DecisionType string

const (
	DecisionSpell  DecisionType = "spell"
	DecisionHold   DecisionType = "hold"
	DecisionAttack DecisionType = "attack"
	DecisionBlock  DecisionType = "block"
)

// Dataset pins the source material, including its licence, so an analysis
// never quietly changes source rows under an unchanged benchmark name.
type Dataset struct {
	Name, License, URI, SHA256 string
}

// Corpus identifies the exact Forge compilation used to reconstruct items.
// No card or token script is embedded in the manifest.
type Corpus struct {
	ForgeRef, CompilerFingerprint string
}

// Selection is the experiment-wide inclusion contract. Counts are exact: a
// partial item build is a different manifest, not a successful sb-v1 run.
type Selection struct {
	Test, Dev                         int
	MinimumGameWinRate                float64
	MinimumGames, MaximumItemsPerGame int
}

// Label is the human action re-expressed as acceptable answers in the item's
// canonical option list. Every alternative is exactly one option index, since
// a result carries exactly one canonical choice (attack and block items are
// projected onto the item's Focus creature, so a joint declaration never
// reaches a label).
//
// Alternatives is the lenient label that A_set scores against: a spell item's
// casts plus, when the human attacked that turn, Pass (index 0), because
// 17lands does not record whether a turn's casts came before or after combat
// (upstream items.py: "plus Pass when the human attacked"). Strict drops that
// Pass (upstream label_strict) and equals Alternatives for every other type.
// Act says the human acted (cast, attacked, blocked); it is exactly "Strict
// excludes the passive option 0" and drives the balanced questions.
type Label struct {
	Alternatives [][]int
	Strict       [][]int
	Act          bool
}

// Item is deliberately a provenance record, rather than an engine snapshot.
// The reconstruction cache it names is private training data; the three
// digests make a stale or changed reconstruction detectable.
//
// Options are the canonical option labels of the decision. Index 0 is always
// the passive answer: Pass for spell and hold items, "don't attack with Focus"
// for attack items and "Focus doesn't block" for block items. Row is the
// 17lands replay row index; it is the game cluster the bootstrap resamples
// (upstream analyze.py groups by item["row"]), so GameID and Row are 1:1.
type Item struct {
	ID, GameID, DraftID                                 string
	Row                                                 int
	Split                                               Split
	Type                                                DecisionType
	Seat, Turn                                          int
	Sequence                                            uint64
	Tier                                                string
	OnPlay, Mirrored                                    bool
	Options                                             []string
	Focus                                               string
	PrefixDigest, PublicStateDigest, LegalOptionsDigest string
	Label                                               Label
	WorldSeeds                                          []uint64
}

type Manifest struct {
	Kind, Digest  string
	SchemaVersion int
	Dataset       Dataset
	Corpus        Corpus
	Selection     Selection
	Items         []Item
}

// Seal records the digest of all manifest fields except Digest. Builders call
// it only after validation; consumers always call Validate, which recomputes
// the digest and rejects tampering.
func (m *Manifest) Seal() error {
	m.Digest = ""
	if err := m.validate(false); err != nil {
		return err
	}
	d, err := m.computedDigest()
	if err != nil {
		return err
	}
	m.Digest = d
	return nil
}

func (m Manifest) computedDigest() (string, error) {
	m.Digest = ""
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (m Manifest) Validate() error { return m.validate(true) }

// ValidateSBV1 adds the published sb-v1 population contract to ordinary
// manifest validation. It is intentionally separate so exploratory native
// manifests remain possible but cannot be confused with a reproduction.
func (m Manifest) ValidateSBV1() error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.Selection != SBV1Selection {
		return errors.New("searchbench: manifest does not use sb-v1 selection")
	}
	got := map[Split]map[DecisionType]int{SplitDev: {}, SplitTest: {}}
	for _, item := range m.Items {
		got[item.Split][item.Type]++
	}
	for split, want := range SBV1Quotas {
		for typ, n := range want {
			if got[split][typ] != n {
				return fmt.Errorf("searchbench: sb-v1 %s %s=%d, want %d", split, typ, got[split][typ], n)
			}
		}
	}
	return nil
}

func (m Manifest) validate(checkDigest bool) error {
	if m.Kind != ManifestKind || m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("searchbench: want kind %q schema %d, got kind %q schema %d", ManifestKind, ManifestSchemaVersion, m.Kind, m.SchemaVersion)
	}
	if m.Dataset.Name == "" || m.Dataset.License == "" || m.Dataset.URI == "" || !digest(m.Dataset.SHA256) {
		return errors.New("searchbench: dataset needs name, license, URI, and SHA-256")
	}
	if len(m.Corpus.ForgeRef) != 40 || !hexString(m.Corpus.ForgeRef) || m.Corpus.CompilerFingerprint == "" {
		return errors.New("searchbench: corpus needs a 40-hex Forge ref and compiler fingerprint")
	}
	if m.Selection.Test < 0 || m.Selection.Dev < 0 || m.Selection.MinimumGameWinRate < 0 || m.Selection.MinimumGameWinRate > 1 || m.Selection.MinimumGames < 1 || m.Selection.MaximumItemsPerGame < 1 {
		return errors.New("searchbench: invalid selection")
	}
	counts := [2]int{}
	lastID := ""
	itemsByGame := make(map[string]int)
	drafts := make(map[string]Split)
	gameRows := make(map[string]int)
	rowGames := make(map[int]string)
	for i := range m.Items {
		it := &m.Items[i]
		if it.ID == "" || it.ID <= lastID {
			return fmt.Errorf("searchbench: item %d id is empty or not strictly sorted", i)
		}
		lastID = it.ID
		if it.GameID == "" || it.DraftID == "" || it.Row < 0 || it.Seat < 0 || it.Seat > 1 || it.Turn < 1 || it.Sequence == 0 || (it.Split != SplitDev && it.Split != SplitTest) || !decisionType(it.Type) || (it.Tier != "T0" && it.Tier != "T1") {
			return fmt.Errorf("searchbench: item %q has invalid identity or classification", it.ID)
		}
		if !digest(it.PrefixDigest) || !digest(it.PublicStateDigest) || !digest(it.LegalOptionsDigest) {
			return fmt.Errorf("searchbench: item %q has an invalid reconstruction digest", it.ID)
		}
		if err := options(it.Options); err != nil {
			return fmt.Errorf("searchbench: item %q options: %w", it.ID, err)
		}
		if (it.Type == DecisionAttack || it.Type == DecisionBlock) != (it.Focus != "") {
			return fmt.Errorf("searchbench: item %q: attack and block items, and only they, name a focus creature", it.ID)
		}
		if err := validateLabel(it.Type, it.Label, len(it.Options)); err != nil {
			return fmt.Errorf("searchbench: item %q label: %w", it.ID, err)
		}
		if row, ok := gameRows[it.GameID]; ok && row != it.Row {
			return fmt.Errorf("searchbench: game %q spans rows %d and %d", it.GameID, row, it.Row)
		}
		if game, ok := rowGames[it.Row]; ok && game != it.GameID {
			return fmt.Errorf("searchbench: row %d names games %q and %q", it.Row, game, it.GameID)
		}
		gameRows[it.GameID], rowGames[it.Row] = it.Row, it.GameID
		if len(it.WorldSeeds) != WorldCount || !uniqueSeeds(it.WorldSeeds) {
			return fmt.Errorf("searchbench: item %q needs %d distinct non-zero world seeds", it.ID, WorldCount)
		}
		itemsByGame[it.GameID]++
		if itemsByGame[it.GameID] > m.Selection.MaximumItemsPerGame {
			return fmt.Errorf("searchbench: game %q exceeds maximum items per game", it.GameID)
		}
		if old, ok := drafts[it.DraftID]; ok && old != it.Split {
			return fmt.Errorf("searchbench: draft %q crosses dev/test split", it.DraftID)
		}
		drafts[it.DraftID] = it.Split
		if it.Split == SplitDev {
			counts[0]++
		} else {
			counts[1]++
		}
	}
	if counts != [2]int{m.Selection.Dev, m.Selection.Test} {
		return fmt.Errorf("searchbench: item counts dev=%d test=%d, want dev=%d test=%d", counts[0], counts[1], m.Selection.Dev, m.Selection.Test)
	}
	if checkDigest {
		if !digest(m.Digest) {
			return errors.New("searchbench: manifest digest is absent or invalid")
		}
		got, err := m.computedDigest()
		if err != nil {
			return err
		}
		if got != m.Digest {
			return fmt.Errorf("searchbench: manifest digest %s does not match computed %s", m.Digest, got)
		}
	}
	return nil
}

// validateLabel enforces the v2 label contract: one option index per
// alternative, every index in range, Strict = Alternatives except for a spell
// item's lenient Pass, a hold's only answer is Pass, and Act is exactly
// "Strict excludes the passive option". A strict label may not mix the passive
// option with an active one, since whether the human acted must be decidable.
func validateLabel(t DecisionType, l Label, nOptions int) error {
	if err := alternatives(l.Alternatives); err != nil {
		return err
	}
	if err := alternatives(l.Strict); err != nil {
		return fmt.Errorf("strict: %w", err)
	}
	for _, set := range [][][]int{l.Alternatives, l.Strict} {
		for _, a := range set {
			if len(a) != 1 {
				return errors.New("every alternative must be exactly one option index")
			}
			if a[0] >= nOptions {
				return fmt.Errorf("alternative %d is outside %d options", a[0], nOptions)
			}
		}
	}
	strictPassive := containsChoice(l.Strict, 0)
	if strictPassive && len(l.Strict) != 1 {
		return errors.New("strict label mixes the passive option with active ones")
	}
	if l.Act == strictPassive {
		return errors.New("act must be exactly: the strict label excludes the passive option 0")
	}
	switch t {
	case DecisionSpell:
		if strictPassive {
			return errors.New("a spell item's strict label must name a cast")
		}
		want := l.Strict
		if containsChoice(l.Alternatives, 0) {
			want = append([][]int{{0}}, l.Strict...)
		}
		if !equalAlternatives(l.Alternatives, want) {
			return errors.New("a spell item's label must be its strict label, plus at most the lenient Pass")
		}
	case DecisionHold:
		if !equalAlternatives(l.Alternatives, [][]int{{0}}) || !equalAlternatives(l.Strict, [][]int{{0}}) {
			return errors.New("a hold item's only alternative is Pass (option 0)")
		}
	default:
		if !equalAlternatives(l.Alternatives, l.Strict) {
			return errors.New("strict label must equal the label outside spell items")
		}
	}
	return nil
}

func options(v []string) error {
	if len(v) < 2 {
		return errors.New("a decision needs at least two distinct options")
	}
	seen := make(map[string]struct{}, len(v))
	for _, o := range v {
		if o == "" {
			return errors.New("empty option label")
		}
		if _, ok := seen[o]; ok {
			return fmt.Errorf("duplicate option label %q", o)
		}
		seen[o] = struct{}{}
	}
	return nil
}

func containsChoice(v [][]int, c int) bool {
	for _, a := range v {
		if len(a) == 1 && a[0] == c {
			return true
		}
	}
	return false
}

func equalAlternatives(a, b [][]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if compareChoices(a[i], b[i]) != 0 {
			return false
		}
	}
	return true
}

func choices(v []int) error {
	for i, n := range v {
		if n < 0 || i > 0 && n <= v[i-1] {
			return errors.New("choices must be non-negative, unique, and sorted")
		}
	}
	return nil
}

func alternatives(v [][]int) error {
	if len(v) == 0 {
		return errors.New("label needs at least one candidate")
	}
	for i := range v {
		if err := choices(v[i]); err != nil {
			return err
		}
		if i > 0 && compareChoices(v[i-1], v[i]) >= 0 {
			return errors.New("candidates must be unique and lexically sorted")
		}
	}
	return nil
}

func compareChoices(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func decisionType(v DecisionType) bool {
	return v == DecisionSpell || v == DecisionHold || v == DecisionAttack || v == DecisionBlock
}

func uniqueSeeds(v []uint64) bool {
	seen := make(map[uint64]struct{}, len(v))
	for _, seed := range v {
		if seed == 0 {
			return false
		}
		if _, ok := seen[seed]; ok {
			return false
		}
		seen[seed] = struct{}{}
	}
	return true
}

func digest(v string) bool { return len(v) == 64 && hexString(v) }

func hexString(v string) bool {
	_, err := hex.DecodeString(v)
	return err == nil
}

// Read decodes one manifest, rejects unknown fields and trailing values, and
// validates its seal. This is the only entry point command-line consumers
// should use.
func Read(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, errors.New("searchbench: manifest has trailing JSON")
		}
		return Manifest{}, err
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
