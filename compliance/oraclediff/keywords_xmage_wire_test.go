package oraclediff

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// xmageKeywordLine is a hand-written XMage result line in the driver's shape:
// the permanent carries the `keywords` array ScenarioReplay.keywordNames
// emits (getRule() spellings, sorted), with one non-evergreen keyword.
const xmageKeywordLine = `{"name":"Garruk's Uprising","strict":true,"ms":1,"snapshots":[{
 "checkpoint":"setup","turn":1,"step":"PRECOMBAT_MAIN","active":0,"priority":0,
 "players":[
  {"seat":0,"life":20,"hand":[],"graveyard":[],"library_count":39,"library_top":["Wastes"]},
  {"seat":1,"life":20,"hand":[],"graveyard":[],"library_count":38,"library_top":["Shock"]}],
 "permanents":[{"ref":"p1:Grizzly Bears","name":"Grizzly Bears","controller":1,"owner":1,
  "pt":"2/2","types":["Bear","Creature"],"colors":"G",
  "keywords":["First strike","Flying","Ward {2}"]}]}]}`

// TestKeywordXMageWireLineDecodesAndCompares decodes the line through
// XResult and compares it with a gorge snapshot under the opt-in. It can
// fail: it asserts the decoded keywords are non-empty, then that dropping
// them on the XMage side diverges.
func TestKeywordXMageWireLineDecodesAndCompares(t *testing.T) {
	var x XResult
	if err := json.Unmarshal([]byte(xmageKeywordLine), &x); err != nil {
		t.Fatal(err)
	}
	got := x.Snapshots[0].Permanents[0].Keywords
	if len(got) != 3 || got[0] != "First strike" {
		t.Fatalf("keywords did not decode from the wire line: %v", got)
	}

	g := rules.OracleResult{Snapshots: []rules.OracleSnapshot{kwSnap("setup", "Flying", "First Strike")}}
	g.Snapshots[0].Checkpoint = x.Snapshots[0].Checkpoint
	if v := CompareOpts(g, nil, x, []string{CompareKeywords}); v.Status != Agree {
		t.Fatalf("gorge Flying/First Strike vs XMage wire keywords: %+v, want AGREE", v)
	}

	x.Snapshots[0].Permanents[0].Keywords = nil
	if v := CompareOpts(g, nil, x, []string{CompareKeywords}); v.Status != Diverge {
		t.Fatalf("XMage side without keywords = %+v, want DIVERGE", v)
	}
	if v := Compare(g, nil, x); v.Status != Agree {
		t.Fatalf("level-A (no opt-in) compare moved: %+v", v)
	}
}
