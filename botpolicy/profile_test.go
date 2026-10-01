package botpolicy

// The L2 cast-profile pins: the embedded default.json parses to exactly
// DefaultCastWeights (that equality is what makes the cast-profile policy
// intent-identical to bot), the loader is strict (unknown keys, wrong or
// missing version, missing cast object, trailing data all rejected), and a
// parsed profile's weights reach the cast scorer like any Board.Cast.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestEmbeddedDefaultProfileEqualsDefaultCastWeights pins the whole point of
// the default profile: the embedded JSON file parses to the same weights the
// L1 default bot plays, field for field. A drift between the file and the
// struct breaks the "cast-profile with the default profile is intent-identical
// to bot" guarantee the seat/host tests pin, so it fails here first.
func TestEmbeddedDefaultProfileEqualsDefaultCastWeights(t *testing.T) {
	w, err := LoadCastProfile(DefaultCastProfileName)
	if err != nil {
		t.Fatalf("LoadCastProfile(default): %v", err)
	}
	if w != DefaultCastWeights {
		t.Fatalf("embedded default profile = %+v, want DefaultCastWeights %+v", w, DefaultCastWeights)
	}
}

// TestLoadCastProfileUnknownNameErrors: the embedded profile set is a closed
// vocabulary like every other named table -- an unknown name is an error
// naming what exists, never a silent fallback to the default.
func TestLoadCastProfileUnknownNameErrors(t *testing.T) {
	_, err := LoadCastProfile("aggressive")
	if err == nil || !strings.Contains(err.Error(), `unknown cast profile "aggressive"`) {
		t.Fatalf("LoadCastProfile(aggressive) err = %v, want an unknown-profile error", err)
	}
}

// TestParseCastProfileRejectsUnknownKey: DisallowUnknownFields applies
// everywhere -- a mis-spelled weight key inside "cast" and an unknown key at
// the top level are both refused, so a stale or typo'd candidate file fails
// at parse instead of silently playing a half-empty profile.
func TestParseCastProfileRejectsUnknownKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
	}{
		{"unknown weight key", `{"version":1,"cast":{"CreatureBase":30,"CreaturePowerd":4}}`},
		{"unknown top-level key", `{"version":1,"cast":{"NonCreatureCMC":1},"combat":{"AttackScale":3}}`},
	} {
		if _, err := ParseCastProfile([]byte(tc.doc)); err == nil {
			t.Fatalf("%s: ParseCastProfile succeeded, want an unknown-key error", tc.name)
		}
	}
}

// TestParseCastProfileRejectsWrongVersion: a version the loader does not know
// -- including an absent one (0) -- is refused, so a future schema change
// cannot silently re-read old files with new meanings.
func TestParseCastProfileRejectsWrongVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
	}{
		{"version 2", `{"version":2,"cast":{"CreatureBase":30}}`},
		{"missing version", `{"cast":{"CreatureBase":30}}`},
	} {
		if _, err := ParseCastProfile([]byte(tc.doc)); err == nil || !strings.Contains(err.Error(), "version") {
			t.Fatalf("%s: err = %v, want a version error", tc.name, err)
		}
	}
}

// TestParseCastProfileRequiresCast: a file without the cast object is an
// error, not a silent all-zero profile -- which Board.castWeights would fold
// back into DefaultCastWeights, the one shape the zero-value trade cannot
// express (see profile.go's doc comment).
func TestParseCastProfileRequiresCast(t *testing.T) {
	if _, err := ParseCastProfile([]byte(`{"version":1}`)); err == nil || !strings.Contains(err.Error(), `missing "cast"`) {
		t.Fatalf("err = %v, want a missing-cast error", err)
	}
}

// TestParseCastProfileRejectsTrailingData: a concatenated or truncated file
// never half-loads -- the decoder must see exactly one JSON value.
func TestParseCastProfileRejectsTrailingData(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
	}{
		{"second value", `{"version":1,"cast":{}} {"version":1,"cast":{}}`},
		{"garbage", `{"version":1,"cast":{}} oops`},
	} {
		if _, err := ParseCastProfile([]byte(tc.doc)); err == nil || !strings.Contains(err.Error(), "trailing") {
			t.Fatalf("%s: err = %v, want a trailing-data error", tc.name, err)
		}
	}
}

// TestParsedProfileWeightsReachTheScorer proves the parse output is live: a
// tuned profile read out of JSON changes a pick the same way the equivalent
// hand-built Board.Cast does (the TestCastWeightsZeroValueIsDefault tuned
// case), and the embedded default's parsed weights do NOT change it.
func TestParsedProfileWeightsReachTheScorer(t *testing.T) {
	b := Board{IsMain: true, Cards: TableOf(map[state.ObjID]Card{
		1: {Creature: true, Power: 1},
		2: {CMC: 3},
	})}
	d := &decision.Decision{Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{castSpell(0, 2), castCreature(1, 1)}}

	if got := b.chooseCast(d); got != 1 {
		t.Fatalf("default pick = option %d, want 1 (the creature outranks by C1)", got)
	}

	tuned := `{"version":1,"cast":{"NonCreatureCMC":10,"CastThreshold":-1073741824}}`
	w, err := ParseCastProfile([]byte(tuned))
	if err != nil {
		t.Fatalf("ParseCastProfile(tuned): %v", err)
	}
	tunedBoard := b
	tunedBoard.Cast = w
	if got := tunedBoard.chooseCast(d); got != 0 {
		t.Fatalf("parsed tuned profile pick = option %d, want 0 — the priced-up spell outranks once the creature terms are gone", got)
	}

	def, err := LoadCastProfile(DefaultCastProfileName)
	if err != nil {
		t.Fatalf("LoadCastProfile(default): %v", err)
	}
	defBoard := b
	defBoard.Cast = def
	if got := defBoard.chooseCast(d); got != 1 {
		t.Fatalf("parsed default profile pick = option %d, want 1 — the embedded profile must play like the default bot", got)
	}
}
