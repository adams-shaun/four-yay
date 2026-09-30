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
	ManifestSchemaVersion = 1
	WorldCount            = 8
)

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

// Label is the human action re-expressed as one or more acceptable candidates
// in the decision's canonical option list. A spell item can have several
// acceptable casts (and, under the documented timing convention, Pass), while
// attacker/blocker candidates are subsets. Each candidate's choices and the
// candidate list itself are canonical. Act drives the balanced act-or-wait
// metric and is deliberately separate from the acceptable-match convention.
type Label struct {
	Alternatives [][]int
	Act          bool
}

// Item is deliberately a provenance record, rather than an engine snapshot.
// The reconstruction cache it names is private training data; the three
// digests make a stale or changed reconstruction detectable.
type Item struct {
	ID, GameID, DraftID                                 string
	Split                                               Split
	Type                                                DecisionType
	Seat, Turn                                          int
	Sequence                                            uint64
	Fidelity                                            string
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
	for i := range m.Items {
		it := &m.Items[i]
		if it.ID == "" || it.ID <= lastID {
			return fmt.Errorf("searchbench: item %d id is empty or not strictly sorted", i)
		}
		lastID = it.ID
		if it.GameID == "" || it.DraftID == "" || it.Seat < 0 || it.Seat > 1 || it.Turn < 1 || it.Sequence == 0 || (it.Split != SplitDev && it.Split != SplitTest) || !decisionType(it.Type) || (it.Fidelity != "T0" && it.Fidelity != "T1") {
			return fmt.Errorf("searchbench: item %q has invalid identity or classification", it.ID)
		}
		if !digest(it.PrefixDigest) || !digest(it.PublicStateDigest) || !digest(it.LegalOptionsDigest) {
			return fmt.Errorf("searchbench: item %q has an invalid reconstruction digest", it.ID)
		}
		if it.Type == DecisionHold && it.Label.Act {
			return fmt.Errorf("searchbench: hold item %q acts", it.ID)
		}
		if err := alternatives(it.Label.Alternatives); err != nil {
			return fmt.Errorf("searchbench: item %q label: %w", it.ID, err)
		}
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
