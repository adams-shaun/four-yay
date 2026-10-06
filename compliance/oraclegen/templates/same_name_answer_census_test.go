package templates

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// sameNameCensus is, per declared set, the generated items (level A plus
// every non-gap level-B requirement) whose gorge run picks an object that
// shares its name with another offered object. It reads the EMITTED
// xmage_answers, not the classification helper: the previous census re-derived
// the ambiguity from SameNameAmbiguity and so stayed green when the answer
// derivation was stubbed out. The Joo Dee flake was a pick among a card and a
// same-named token copy that reached XMage as a bare name; XMage then chose
// whichever object its own iteration order reached first.
//
// Distinct counts ambiguous picks whose same-named siblings include an object
// of a different kind (a token among cards, or the reverse): XMage's
// TestPlayer.makeChoose parses a "[only copy]"/"[no copy]" suffix and filters
// on isCopy(), so these are resolvable and MUST carry the discriminator.
// Unresolved is the subset of Distinct whose emitted answer carries no
// discriminator: it is the ticket's defect and must stay 0. SameKind counts
// ambiguous picks among objects of one kind (two cards, two tokens), which
// XMage has no discriminator for; they keep the bare name and are pinned for
// drift only. Fails in both directions: any change that adds or removes an
// ambiguous pick, or drops a discriminator from an emitted answer, moves a
// pin.
type sameNameCensus struct {
	Distinct   int      `json:"distinct"`
	SameKind   int      `json:"same_kind"`
	Unresolved int      `json:"unresolved"`
	Items      []string `json:"items"`
}

// wantSameNameCensus pins the per-set census as JSON. Measured 2026-10-06.
var wantSameNameCensus = map[string]string{
	"BIG": `{"distinct":0,"same_kind":18,"unresolved":0,"items":null}`,
	"BLB": `{"distinct":0,"same_kind":98,"unresolved":0,"items":null}`,
	"DFT": `{"distinct":0,"same_kind":56,"unresolved":0,"items":null}`,
	"DSK": `{"distinct":0,"same_kind":101,"unresolved":0,"items":null}`,
	"ECL": `{"distinct":0,"same_kind":84,"unresolved":0,"items":null}`,
	"EOE": `{"distinct":0,"same_kind":49,"unresolved":0,"items":null}`,
	"FDN": `{"distinct":1,"same_kind":102,"unresolved":0,"items":["Extravagant Replication/trigger#0.0/v1"]}`,
	"FIN": `{"distinct":0,"same_kind":80,"unresolved":0,"items":null}`,
	"FRA": `{"distinct":0,"same_kind":53,"unresolved":0,"items":null}`,
	"HOB": `{"distinct":0,"same_kind":33,"unresolved":0,"items":null}`,
	"LCI": `{"distinct":0,"same_kind":64,"unresolved":0,"items":null}`,
	"MKM": `{"distinct":0,"same_kind":63,"unresolved":0,"items":null}`,
	"MSH": `{"distinct":0,"same_kind":85,"unresolved":0,"items":null}`,
	"OTJ": `{"distinct":1,"same_kind":63,"unresolved":0,"items":["Double Down/trigger#0.0/v1"]}`,
	"SOS": `{"distinct":0,"same_kind":66,"unresolved":0,"items":null}`,
	"SPM": `{"distinct":0,"same_kind":55,"unresolved":0,"items":null}`,
	"TDM": `{"distinct":0,"same_kind":91,"unresolved":0,"items":null}`,
	"TLA": `{"distinct":1,"same_kind":84,"unresolved":0,"items":["Joo Dee, One of Many/activate#0.0/v1"]}`,
	"TMT": `{"distinct":0,"same_kind":48,"unresolved":0,"items":null}`,
	"WOE": `{"distinct":0,"same_kind":49,"unresolved":0,"items":null}`,
}

// markedAnswers counts the emitted answers for a step that carry an XMage
// copy discriminator. Both the choice queue (TestPlayer.makeChoose) and the
// target queue (chooseTarget / chooseTargetAmount) parse the marker and
// filter on isCopy(). A definition may join several picks with '^', so the
// marker is tested per segment.
func markedAnswers(as []oraclegen.XAnswer) int {
	n := 0
	for _, a := range as {
		if a.Kind != "choice" && a.Kind != "target" {
			continue
		}
		for _, seg := range strings.Split(a.Value, "^") {
			if strings.HasSuffix(seg, "[no copy]") || strings.HasSuffix(seg, "[only copy]") {
				n++
			}
		}
	}
	return n
}

func TestSameNameAnswerCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..")
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)

	var sets []string
	declared, err := compliance.LoadDeclared(filepath.Join(root, "compliance", "declared.json"))
	if err != nil {
		t.Fatal(err)
	}
	for set := range declared {
		sets = append(sets, set)
	}
	sort.Strings(sets)

	got := map[string]string{}
	for _, set := range sets {
		printed, err := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		var c sameNameCensus
		scan := func(it oraclegen.Item, id string) {
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil {
				return
			}
			// Ambiguous picks and the emitted answers that resolve them are
			// counted per step, because one step's XAnswers may answer several
			// decisions at that step.
			type stepCensus struct{ distinct, marked int }
			byStep := map[int]*stepCensus{}
			stepOf := func(s int) *stepCensus {
				if byStep[s] == nil {
					byStep[s] = &stepCensus{}
				}
				return byStep[s]
			}
			for _, d := range res.Decisions {
				for k := range d.Picks {
					amb, marker := oraclegen.SameNameAmbiguity(d, k)
					if !amb {
						continue
					}
					if marker == "" {
						c.SameKind++
						continue
					}
					stepOf(d.Step).distinct++
					c.Distinct++
					c.Items = append(c.Items, id)
				}
			}
			for step, sc := range byStep {
				if step < 0 || step >= len(it.XAnswers) {
					// No emitted answers for a step that needs one: every
					// distinct pick there is unresolved.
					sc.marked = 0
				} else {
					sc.marked = markedAnswers(it.XAnswers[step])
				}
				if sc.marked < sc.distinct {
					c.Unresolved += sc.distinct - sc.marked
				}
			}
		}
		for _, name := range printed.Cards {
			card, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				continue
			}
			if it, skip := Generate(reg, card); skip == nil {
				scan(it, it.ID)
			}
			cd, _ := reg.Lookup(card)
			for _, r := range levelb.Requirements(cd) {
				if r.Gap != "" {
					continue
				}
				if it, skip := GenerateB(reg, card, r); skip == nil {
					scan(it, it.ID)
				}
			}
		}
		sort.Strings(c.Items)
		b, _ := json.Marshal(c)
		got[set] = string(b)
	}
	for _, set := range sets {
		if got[set] != wantSameNameCensus[set] {
			t.Errorf("%s same-name census: got %s want %s", set, got[set], wantSameNameCensus[set])
		}
	}
}
