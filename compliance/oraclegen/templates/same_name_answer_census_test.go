package templates

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// sameNameCensus is, per declared set, the generated items (level A plus
// every non-gap level-B requirement) whose gorge run picks an object that
// shares its name with another offered object. "distinguished" counts the
// picks the answer derivation tags with an XMage copy discriminator
// ([only copy] / [no copy]: a token among same-named cards or the reverse);
// "interchangeable" the picks among same-named objects of one kind, which
// XMage cannot tell apart either and which keep the bare name. The Joo Dee
// flake was a distinguished pick that carried no tag. Fails in both
// directions: a template change that adds an ambiguous pick, or removes a
// pinned one, moves the pin.
type sameNameCensus struct {
	Distinguished, Interchangeable int
	DistinguishedItems             []string
}

// wantSameNameCensus pins the per-set census as JSON. Measured 2026-10-06.
var wantSameNameCensus = map[string]string{
	"BIG": `{"Distinguished":0,"Interchangeable":18,"DistinguishedItems":null}`,
	"BLB": `{"Distinguished":0,"Interchangeable":98,"DistinguishedItems":null}`,
	"DFT": `{"Distinguished":0,"Interchangeable":56,"DistinguishedItems":null}`,
	"DSK": `{"Distinguished":0,"Interchangeable":101,"DistinguishedItems":null}`,
	"ECL": `{"Distinguished":0,"Interchangeable":84,"DistinguishedItems":null}`,
	"EOE": `{"Distinguished":0,"Interchangeable":49,"DistinguishedItems":null}`,
	"FDN": `{"Distinguished":1,"Interchangeable":102,"DistinguishedItems":["Extravagant Replication/trigger#0.0/v1"]}`,
	"FIN": `{"Distinguished":0,"Interchangeable":80,"DistinguishedItems":null}`,
	"FRA": `{"Distinguished":0,"Interchangeable":53,"DistinguishedItems":null}`,
	"HOB": `{"Distinguished":0,"Interchangeable":33,"DistinguishedItems":null}`,
	"LCI": `{"Distinguished":0,"Interchangeable":64,"DistinguishedItems":null}`,
	"MKM": `{"Distinguished":0,"Interchangeable":63,"DistinguishedItems":null}`,
	"MSH": `{"Distinguished":0,"Interchangeable":85,"DistinguishedItems":null}`,
	"OTJ": `{"Distinguished":1,"Interchangeable":63,"DistinguishedItems":["Double Down/trigger#0.0/v1"]}`,
	"SOS": `{"Distinguished":0,"Interchangeable":66,"DistinguishedItems":null}`,
	"SPM": `{"Distinguished":0,"Interchangeable":55,"DistinguishedItems":null}`,
	"TDM": `{"Distinguished":0,"Interchangeable":91,"DistinguishedItems":null}`,
	"TLA": `{"Distinguished":1,"Interchangeable":84,"DistinguishedItems":["Joo Dee, One of Many/activate#0.0/v1"]}`,
	"TMT": `{"Distinguished":0,"Interchangeable":48,"DistinguishedItems":null}`,
	"WOE": `{"Distinguished":0,"Interchangeable":49,"DistinguishedItems":null}`,
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
			for _, d := range res.Decisions {
				for k := range d.Picks {
					amb, marker := oraclegen.SameNameAmbiguity(d, k)
					if !amb {
						continue
					}
					if marker == "" {
						c.Interchangeable++
						continue
					}
					c.Distinguished++
					c.DistinguishedItems = append(c.DistinguishedItems, id)
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
		sort.Strings(c.DistinguishedItems)
		b, _ := json.Marshal(c)
		got[set] = string(b)
	}
	for _, set := range sets {
		if got[set] != wantSameNameCensus[set] {
			t.Errorf("%s same-name census: got %s want %s", set, got[set], wantSameNameCensus[set])
		}
	}
}
