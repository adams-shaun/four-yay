package rules

// Derived, compiled-registry corpus census for the eight "std3" parameter
// shapes (ticket agent-20261005T011820Z-734eaec3; the shapes were fixed by
// cli-20261004T233421Z-7517756b, merge ff64dcd32, whose carrier ratchets
// landed in e5cdd8350).
//
// The existing effects/ ratchets (effects/unread_param_corpus_test.go
// TestUnreadEffectParamCorpusCarriers and effects/count_pow_test.go
// TestNumberPowCorpusCarriers) scan RAW .txt text with strings.Contains(line,
// api) + strings.Contains(line, key). That is a substring match: api "Animate"
// matches a line whose token is AnimateAll, api "PutCounter" matches
// PutCounterAll/PutCounters, and a per-line key match cannot tell which SA the
// key sits on. No false positive exists at the current pin, but the shape is
// fragile: the next `AnimateAll | ... | Colors$ ChosenColor` line would be a
// false positive a compiled census cannot produce.
//
// This census is the durable, exact-keyed home for the same class. It walks
// the COMPILED registry keyed on the exact (sa.API, key) pair -- the same IR
// the engine runs -- and pins the carrier set per shape, one directory and
// one file per card under rules/testdata/, compared in BOTH directions with a
// loud precondition. A card carrying the key on a different API, or a future
// API whose name merely contains this one, cannot join the set.
//
// Deliberately NOT touched: the effects/ raw-text ratchets stay as they are
// (they remain a cheap independent text-level check); paramcensus_test.go is a
// sequenced hot file and is not edited. See the report for why both homes
// coexist.

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// std3 param census labels: one constant label per shape naming the mechanism
// (mirroring oracle_fix_census_test.go's censusLabelSacrificedPower style).
// The values are the label strings carried by every file in the matching
// rules/testdata/corpus-std3-* directory.
const (
	std3LabelPumpReplaceDying      = "param:api:Pump.ReplaceDyingDefined"
	std3LabelChangeZoneThisAndTgts = "param:api:ChangeZone.ThisDefinedAndTgts"
	std3LabelDigUntilMinTotalCMC   = "param:api:DigUntil.MinTotalCMC"
	std3LabelCopyPermanentDefName  = "param:api:CopyPermanent.DefinedName"
	std3LabelPutCounterTypes       = "param:api:PutCounter.CounterTypes"
	std3LabelAnimateColors         = "param:api:Animate.Colors"
	std3LabelChooseTypeTypes       = "param:api:ChooseType.ValidTypes+InvalidTypes"
	std3LabelNumberPow             = "count:Number.Pow"
)

// std3Shape is one measured parameter shape. extra, when non-nil, narrows a
// compiled (API, key) match further -- the Animate shape's Colors$ value must
// name the literal ChosenColor, not any colour token.
type std3Shape struct {
	label string
	api   string
	key   string
	// value contains a substring the parameter value must carry, when value
	// is non-empty.
	value string
}

// std3Shapes is the eight-shape registry. It is the ONE home of the shape
// definitions; the walk and every test below derive from it.
var std3Shapes = []std3Shape{
	{label: std3LabelPumpReplaceDying, api: "Pump", key: "ReplaceDyingDefined"},
	{label: std3LabelChangeZoneThisAndTgts, api: "ChangeZone", key: "ThisDefinedAndTgts"},
	{label: std3LabelDigUntilMinTotalCMC, api: "DigUntil", key: "MinTotalCMC"},
	{label: std3LabelCopyPermanentDefName, api: "CopyPermanent", key: "DefinedName"},
	{label: std3LabelPutCounterTypes, api: "PutCounter", key: "CounterTypes"},
	{label: std3LabelAnimateColors, api: "Animate", key: "Colors", value: "ChosenColor"},
	{label: std3LabelChooseTypeTypes, api: "ChooseType", key: "ValidTypes"},
	{label: std3LabelChooseTypeTypes, api: "ChooseType", key: "InvalidTypes"},
}

// std3ParamCarriers walks the compiled corpus and returns, per label, the set
// of card names carrying that shape. It follows the same traversal
// Face.Primitives uses -- direct Abilities, Trigger effects, Repl With bodies,
// every SVar-resolved ability chain (EachSVarAbility), and typed raw
// effect children -- because those are exactly the reachable SAs the engine
// runs. The Pow shape is a value body, not an ability, so it is measured from
// the face's SVar text (the compiled form of a `Number$.../Pow.` operand).
func std3ParamCarriers(reg *cards.Registry) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	add := func(label, card string) {
		if label == "" || card == "" {
			return
		}
		if out[label] == nil {
			out[label] = map[string]bool{}
		}
		out[label][card] = true
	}
	// matchOne records every shape the compiled SA carries. An exact API
	// comparison, so AnimateAll never matches Animate and PutCounterAll never
	// matches PutCounter.
	matchOne := func(card string, sa *cards.SA) {
		for _, s := range std3Shapes {
			if sa.API != s.api {
				continue
			}
			v, ok := sa.Params[s.key]
			if !ok {
				continue
			}
			if s.value != "" && !strings.Contains(v, s.value) {
				continue
			}
			add(s.label, card)
		}
	}
	// matchChain walks an SA and its SubAbility$ chain (like Primitives).
	matchChain := func(card string, head *cards.SA) {
		for sa := head; sa != nil; sa = sa.Sub {
			matchOne(card, sa)
		}
	}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, a := range f.Abilities {
				matchChain(f.Name, a)
			}
			for _, tr := range f.Triggers {
				matchChain(f.Name, tr.Effect)
			}
			for _, r := range f.Repls {
				matchChain(f.Name, r.With)
			}
			f.EachSVarAbility(func(sa *cards.SA) { matchChain(f.Name, sa) })
			f.EachRawEffectChild(func(ch cards.EffectChild) {
				if ch.Trigger != nil {
					matchChain(f.Name, ch.Trigger.Effect)
				}
			})
			// The Number$/Pow. operand is a value body, reachable through the
			// face's SVar text rather than through an ability chain.
			svar := false
			for _, body := range f.SVars {
				if strings.Contains(body, "/Pow.") {
					svar = true
				}
			}
			if svar {
				add(std3LabelNumberPow, f.Name)
			}
		}
	}
	return out
}

// checkStd3Census compares the measured carrier set for one label against the
// per-card table in both directions, with a loud precondition that the walk
// reached the class and a named card is a measured member. A registry that
// failed to load, or a walk whose Sub/SVar traversal stopped, must FAIL rather
// than pass vacuously.
func checkStd3Census(t *testing.T, label, dir, mustCarry string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	got := std3ParamCarriers(reg)[label]
	want := loadCensusLabels(t, dir)

	if len(got) == 0 {
		t.Fatalf("%s: census found no carriers at all; the corpus walk is broken", label)
	}
	if mustCarry != "" && !got[mustCarry] {
		t.Fatalf("%s: precondition: %q is not a measured carrier, got %v", label, mustCarry, sortedCensusNames(got))
	}
	var gotNames, wantNames []string
	for n := range got {
		gotNames = append(gotNames, n)
	}
	for n := range want {
		wantNames = append(wantNames, n)
	}
	sort.Strings(gotNames)
	sort.Strings(wantNames)
	if strings.Join(gotNames, "; ") != strings.Join(wantNames, "; ") {
		t.Errorf("%s carrier cards changed:\n got: %v\nwant: %v", label, gotNames, wantNames)
	}
	// Every table file must name exactly this label (a file carrying the wrong
	// label is a copy-paste defect the set comparison cannot see).
	for name, labels := range want {
		if len(labels) != 1 || labels[0] != label {
			t.Errorf("%s: %s labels = %v, want [%s]", label, name, labels, label)
		}
	}
}

// TestStd3ParamCensusFilesWellFormed holds the eight census directories to the
// per-card schema without a corpus, so a malformed table file fails even where
// .cards is absent.
func TestStd3ParamCensusFilesWellFormed(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{
		"corpus-std3-pump-replacedying",
		"corpus-std3-changezone-thisdefinedandtgts",
		"corpus-std3-diguntil-mintotalcmc",
		"corpus-std3-copypermanent-definedname",
		"corpus-std3-putcounter-countertypes",
		"corpus-std3-animate-chosencolor",
		"corpus-std3-choosetype-validinvalidtypes",
		"corpus-std3-number-pow",
	} {
		if got := loadCensusLabels(t, dir); len(got) == 0 {
			t.Errorf("%s: no per-card files; the table would compare vacuous", dir)
		}
	}
}

// TestStd3ParamCensusPump pins Pump's ReplaceDyingDefined$ carriers.
func TestStd3ParamCensusPump(t *testing.T) {
	checkStd3Census(t, std3LabelPumpReplaceDying, "corpus-std3-pump-replacedying", "Gnashing of Teeth")
}

// TestStd3ParamCensusChangeZone pins ChangeZone's ThisDefinedAndTgts$ carriers.
func TestStd3ParamCensusChangeZone(t *testing.T) {
	checkStd3Census(t, std3LabelChangeZoneThisAndTgts, "corpus-std3-changezone-thisdefinedandtgts", "Mangara of Corondor")
}

// TestStd3ParamCensusDigUntil pins DigUntil's MinTotalCMC$ carriers.
func TestStd3ParamCensusDigUntil(t *testing.T) {
	checkStd3Census(t, std3LabelDigUntilMinTotalCMC, "corpus-std3-diguntil-mintotalcmc", "Dream Harvest")
}

// TestStd3ParamCensusCopyPermanent pins CopyPermanent's DefinedName$ carriers.
func TestStd3ParamCensusCopyPermanent(t *testing.T) {
	checkStd3Census(t, std3LabelCopyPermanentDefName, "corpus-std3-copypermanent-definedname", "Mutable Explorer")
}

// TestStd3ParamCensusPutCounter pins PutCounter's CounterTypes$ carriers.
func TestStd3ParamCensusPutCounter(t *testing.T) {
	checkStd3Census(t, std3LabelPutCounterTypes, "corpus-std3-putcounter-countertypes", "Abigale, Eloquent First-Year")
}

// TestStd3ParamCensusAnimate pins the Animate Colors$ ChosenColor carriers.
func TestStd3ParamCensusAnimate(t *testing.T) {
	checkStd3Census(t, std3LabelAnimateColors, "corpus-std3-animate-chosencolor", "Puca's Eye")
}

// TestStd3ParamCensusChooseType pins the ChooseType ValidTypes$/InvalidTypes$
// carriers (both spellings share one table and one label).
func TestStd3ParamCensusChooseType(t *testing.T) {
	checkStd3Census(t, std3LabelChooseTypeTypes, "corpus-std3-choosetype-validinvalidtypes", "Dawn-Blessed Pennant")
}

// TestStd3ParamCensusNumberPow pins the Number$/Pow. carriers.
func TestStd3ParamCensusNumberPow(t *testing.T) {
	checkStd3Census(t, std3LabelNumberPow, "corpus-std3-number-pow", "Mathemagics")
}

// std3CensusDirForLabel maps a label back to its table directory so
// TestStd3ParamCensusAllShapesMeasured can hold the shape list and the
// directories in sync.
var std3CensusDirForLabel = map[string]string{
	std3LabelPumpReplaceDying:      "corpus-std3-pump-replacedying",
	std3LabelChangeZoneThisAndTgts: "corpus-std3-changezone-thisdefinedandtgts",
	std3LabelDigUntilMinTotalCMC:   "corpus-std3-diguntil-mintotalcmc",
	std3LabelCopyPermanentDefName:  "corpus-std3-copypermanent-definedname",
	std3LabelPutCounterTypes:       "corpus-std3-putcounter-countertypes",
	std3LabelAnimateColors:         "corpus-std3-animate-chosencolor",
	std3LabelChooseTypeTypes:       "corpus-std3-choosetype-validinvalidtypes",
	std3LabelNumberPow:             "corpus-std3-number-pow",
}

// TestStd3ParamCensusAllShapesMeasured is the self-check that every registered
// shape has a table directory and a measured member: a shape added to
// std3Shapes with no directory, or one whose walk silently measured nothing,
// fails here rather than being quietly untested.
func TestStd3ParamCensusAllShapesMeasured(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := std3ParamCarriers(reg)
	for _, s := range std3Shapes {
		dir, ok := std3CensusDirForLabel[s.label]
		if !ok {
			t.Errorf("shape %s has no census directory mapping", s.label)
			continue
		}
		if filepath.Base(dir) == "" {
			t.Errorf("shape %s maps to an empty directory", s.label)
		}
		if len(got[s.label]) == 0 {
			t.Errorf("shape %s (%s.%s) measured no carriers", s.label, s.api, s.key)
		}
	}
}
