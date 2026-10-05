package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestWalkFaceFactsScanVerifyCatchesStaleness(t *testing.T) {
	if !walkSkipVerify || !faceScanVerify {
		t.Fatal("precondition: walk and face-scan verification must both be enabled")
	}
	face := &cards.Face{Name: "Scan Probe", SVars: map[string]string{"probe": "Sunburst"}}
	table := buildWalkFaceTable([]*cards.Face{face})
	ff := table.lookup(face)
	if ff == nil || !ff.fullyCurrent(face) {
		t.Fatal("precondition: current compiled face-facts entry is required")
	}
	const bit = faceScanMentionsSunburst
	if ff.scan&bit == 0 {
		t.Fatal("precondition: compiled scan must initially find Sunburst")
	}

	e := &Engine{compiledText: &compiledText{faces: table}}
	face.SVars["probe"] = "Moonburst" // same length and map identity; only scan content changes.
	if !ff.fullyCurrent(face) {
		t.Fatal("precondition: the unchanged identity/length guards should still match")
	}
	fresh := computeFaceScan(face)
	if fresh == ff.scan || fresh&bit != 0 || ff.scan&bit == 0 {
		t.Fatalf("precondition: stale scan verdict must differ (memo=%b fresh=%b)", ff.scan, fresh)
	}

	defer func() {
		r := recover()
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "compiled face scan") {
			t.Fatalf("independent face-scan verifier did not catch stale verdict: %v", r)
		}
	}()
	if e.faceScanHas(face, bit) {
		t.Fatal("stale compiled scan unexpectedly reported Sunburst")
	}
}
