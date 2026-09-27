package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// faceScan is a set of text-scan verdicts about one compiled face. Each bit
// is a pure function of the face's own content (its SVars, keywords and
// ability/trigger/replacement/static text), which is fixed once the face is
// built: the pay-time converge/cast-spend capture gates and the auto-pay
// shape gate re-ran these substring scans over every battlefield face on
// every cast and every planner query scope. faceScanHas memoises them per
// face pointer for the engine's lifetime (a face held as a key stays alive,
// so its address cannot be reused by another face). Clone leaves the memo
// nil. In the rules test binary faceScanVerify recomputes every hit and
// panics on a difference.
type faceScan uint16

const (
	faceScanComputed faceScan = 1 << iota
	// faceScanConvergeReader: an SVar reads TriggeredCard$Converge (any
	// case) -- triggeredConvergeReaderOut's per-face test.
	faceScanConvergeReader
	// faceScanCastSpendReader: the face mentions
	// TriggeredCard$CastTotalManaSpent (objectReadsTriggeredCastSpend).
	faceScanCastSpendReader
	// faceScanMentionsSunburst: Mentions("Sunburst") (sunburstGrantOut).
	faceScanMentionsSunburst
	// faceScanSunburstGrantPlan: faceGrantsSunburstForPlan.
	faceScanSunburstGrantPlan
	// faceScanReadsManaSpent: faceReadsManaSpent.
	faceScanReadsManaSpent
	// faceScanModalCost: some SVar names a mode printing a ModeCost$
	// (faceHasModeCost).
	faceScanModalCost
)

// faceScanVerify: see derivedMemoVerify. Set by the rules test binary.
var faceScanVerify = derivedMemoVerifyFlag != ""

func computeFaceScan(f *cards.Face) faceScan {
	s := faceScanComputed
	if faceConvergeSVarReader(f) {
		s |= faceScanConvergeReader
	}
	if f.Mentions("TriggeredCard$CastTotalManaSpent") {
		s |= faceScanCastSpendReader
	}
	if f.Mentions("Sunburst") {
		s |= faceScanMentionsSunburst
	}
	if faceGrantsSunburstForPlan(f) {
		s |= faceScanSunburstGrantPlan
	}
	if faceReadsManaSpent(f) {
		s |= faceScanReadsManaSpent
	}
	if faceHasModeCost(f) {
		s |= faceScanModalCost
	}
	return s
}

// faceScanHas reports whether f's scan carries bit. A nil face carries none.
func (e *Engine) faceScanHas(f *cards.Face, bit faceScan) bool {
	if f == nil {
		return false
	}
	s, ok := e.faceScans[f]
	if !ok {
		s = computeFaceScan(f)
		if e.faceScans == nil {
			e.faceScans = make(map[*cards.Face]faceScan)
		}
		e.faceScans[f] = s
	} else if faceScanVerify {
		if fresh := computeFaceScan(f); fresh != s {
			panic(fmt.Sprintf("rules: face scan memo for %q is stale (%b vs %b)", f.Name, s, fresh))
		}
	}
	return s&bit != 0
}

// faceConvergeSVarReader is triggeredConvergeReaderOut's per-face test: some
// SVar reads TriggeredCard$Converge in any case (an any-SVar OR, so the map
// order cannot matter).
func faceConvergeSVarReader(f *cards.Face) bool {
	for _, v := range f.SVars {
		if strings.Contains(strings.ToLower(v), "triggeredcard$converge") {
			return true
		}
	}
	return false
}

// faceHasModeCost is the auto-pay shape gate's modal-cost test: some SVar of
// f resolves to a mode with a ModeCost$, priceable or not (modeCost's
// present). An any-SVar OR, so the map order cannot matter.
func faceHasModeCost(f *cards.Face) bool {
	for name := range f.SVars {
		if _, present, _ := modeCost(f, name); present || modeCostUnparseable(f, name) {
			return true
		}
	}
	return false
}
