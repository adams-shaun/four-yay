package templates

import (
	"encoding/json"
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
// shares its name with a DIFFERENT offered object (same-name options that
// resolve to one ref are the same object, not an ambiguity).
//
// It reads the EMITTED xmage_answers and re-runs the driver's own matching
// over the offered refs, so a bare name that could match more than one object
// counts as unresolved even if some other helper would call it resolved. The
// three buckets are:
//
//   - Alias: the emitted value is the pick's exact scenario ref ("p0:Forest#2",
//     carried to XMage as its "@ref" alias), which names that one object.
//   - Copy: the emitted value is "name[only copy]"/"name[no copy]", and the
//     isCopy() filter leaves exactly one same-name candidate.
//   - Unresolved: the emitted value matches zero or more than one distinct
//     same-name candidate. This is the Joo Dee defect and must stay 0.
//
// Buckets count items, not individual picks. If an item uses both forms,
// Alias wins; any unresolved pick makes the whole item Unresolved. Their sum
// is the number of items ambiguous before discrimination; Unresolved is the
// number remaining afterward. Pins fail on increases AND decreases.
type sameNameCensus struct {
	Alias      int      `json:"alias"`
	Copy       int      `json:"copy"`
	Unresolved int      `json:"unresolved"`
	Items      []string `json:"items"`
}

// wantSameNameCensus pins the per-set census as JSON. Measured 2026-10-06 on
// the fix that emits an exact ref for same-kind and cross-seat duplicates and
// a copy marker for a unique token-or-card pick. Re-measured as ITEMS rather
// than picks (multi-pick decisions previously inflated these counts).
var wantSameNameCensus = map[string]string{
	"BIG": `{"alias":1,"copy":0,"unresolved":0,"items":null}`,
	"BLB": `{"alias":4,"copy":0,"unresolved":0,"items":null}`,
	"DFT": `{"alias":3,"copy":0,"unresolved":0,"items":null}`,
	"DSK": `{"alias":27,"copy":0,"unresolved":0,"items":null}`,
	"ECL": `{"alias":5,"copy":0,"unresolved":0,"items":null}`,
	"EOE": `{"alias":2,"copy":0,"unresolved":0,"items":null}`,
	"FDN": `{"alias":2,"copy":1,"unresolved":0,"items":null}`,
	"FIN": `{"alias":4,"copy":0,"unresolved":0,"items":null}`,
	"FRA": `{"alias":2,"copy":0,"unresolved":0,"items":null}`,
	"HOB": `{"alias":2,"copy":0,"unresolved":0,"items":null}`,
	"LCI": `{"alias":5,"copy":0,"unresolved":0,"items":null}`,
	"MKM": `{"alias":1,"copy":0,"unresolved":0,"items":null}`,
	"MSH": `{"alias":4,"copy":0,"unresolved":0,"items":null}`,
	"OTJ": `{"alias":2,"copy":1,"unresolved":0,"items":null}`,
	"SOS": `{"alias":5,"copy":0,"unresolved":0,"items":null}`,
	"SPM": `{"alias":3,"copy":0,"unresolved":0,"items":null}`,
	"TDM": `{"alias":5,"copy":0,"unresolved":0,"items":null}`,
	"TLA": `{"alias":3,"copy":1,"unresolved":0,"items":null}`,
	"TMT": `{"alias":4,"copy":0,"unresolved":0,"items":null}`,
	"WOE": `{"alias":3,"copy":0,"unresolved":0,"items":null}`,
}

// refName strips a scenario ref to the object name.
func refName(ref string) string {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 0 && strings.HasPrefix(n, "p") {
		n = n[i+1:]
	}
	n = strings.TrimPrefix(n, "token:")
	if j := strings.LastIndexByte(n, '#'); j >= 0 && j+1 < len(n) && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}

func isTokenRef(ref string) bool { return strings.Contains(ref, ":token:") }

// isRefSpelling reports whether s is a full scenario ref ("p0:Name#2",
// "p1:token:Name"). The generator emits one for an alias pick on the target
// queue, which the driver's targetName maps to the object's "@" alias.
func isRefSpelling(s string) bool {
	if len(s) < 3 || s[0] != 'p' || s[1] < '0' || s[1] > '9' {
		return false
	}
	return strings.Contains(s, ":")
}

// answerMatchesRef mirrors the XMage driver when it checks one emitted answer
// against one offered object: an exact-ref alias equals the ref; a bare or
// copy-marked name matches by name, with "[only copy]"/"[no copy]" filtering
// on the token-vs-card distinction (TestPlayer.hasObjectTargetNameOrAlias +
// the isCopy() filter).
func answerMatchesRef(answer, ref string) bool {
	if strings.HasPrefix(answer, "@") {
		return answer == "@"+ref
	}
	if isRefSpelling(answer) {
		return answer == ref
	}
	base := answer
	copyFilter := ""
	switch {
	case strings.HasSuffix(base, "[no copy]"):
		copyFilter = "origin"
		base = strings.TrimSuffix(base, "[no copy]")
	case strings.HasSuffix(base, "[only copy]"):
		copyFilter = "copy"
		base = strings.TrimSuffix(base, "[only copy]")
	}
	if !strings.EqualFold(base, refName(ref)) {
		return false
	}
	switch copyFilter {
	case "origin":
		return !isTokenRef(ref)
	case "copy":
		return isTokenRef(ref)
	}
	return true
}

// classifyAnswer returns the census bucket for one emitted answer over the
// offered refs, and the number of DISTINCT same-name candidates it matches.
// Zero or more than one match is unresolved.
func classifyAnswer(answer string, ref string, options []string) (bucket string, matches int) {
	name := refName(ref)
	seen := map[string]bool{}
	for _, o := range options {
		if !strings.EqualFold(refName(o), name) {
			continue
		}
		if answerMatchesRef(answer, o) {
			seen[o] = true
		}
	}
	// The answer must select the pick itself, not merely one object: an
	// alias naming a same-name sibling is a wrong answer, and a pick whose
	// own option is missing from options cannot be proven resolved.
	if len(seen) == 1 && seen[ref] {
		if strings.HasPrefix(answer, "@") {
			return "alias", 1
		}
		if isRefSpelling(answer) {
			return "alias", 1
		}
		return "copy", 1
	}
	return "unresolved", len(seen)
}

// segmentFor finds the emitted answer segment (answers may be '^'-joined)
// that NAMES the pick's ref, across every answer value of the step: first an
// exact "@ref"/ref alias, then a bare/copy-marked name equal to the pick's
// name, then an alias of a same-name SIBLING (a wrong answer, which
// classifyAnswer then counts as unresolved rather than letting it go
// uncounted). Finally a different-name alias is retained as a wrong answer,
// not silently skipped. Returns "" when no segment names any object, the
// ordinary case for a pick XMage answers with a number, a yes/no, or a skip
// token rather than a name selection.
func segmentFor(values []string, ref string) string {
	var segs []string
	for _, v := range values {
		segs = append(segs, strings.Split(v, "^")...)
	}
	for _, seg := range segs {
		if seg == "@"+ref || seg == ref {
			return seg
		}
	}
	for _, seg := range segs {
		if strings.HasPrefix(seg, "@") || isRefSpelling(seg) {
			continue
		}
		base := strings.TrimSuffix(strings.TrimSuffix(seg, "[no copy]"), "[only copy]")
		if strings.EqualFold(base, refName(ref)) {
			return seg
		}
	}
	for _, seg := range segs {
		alias := strings.TrimPrefix(seg, "@")
		if isRefSpelling(alias) && strings.EqualFold(refName(alias), refName(ref)) {
			return seg
		}
	}
	for _, seg := range segs {
		if isRefSpelling(strings.TrimPrefix(seg, "@")) {
			return seg
		}
	}
	return ""
}

// Count siblings independently of the production discriminator, so a bug in
// ClassifySameName cannot make both the generator and its census go green.
func sameNameCandidates(ref string, options []string) int {
	seen := map[string]bool{}
	for _, candidate := range options {
		if strings.EqualFold(refName(candidate), refName(ref)) {
			seen[candidate] = true
		}
	}
	return len(seen)
}

func runSameNameAnswerCensus(t *testing.T, start, end int) {
	reg := testutil.CorpusRegistry(t)
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)

	declared, err := compliance.EmbeddedDeclared()
	if err != nil {
		t.Fatal(err)
	}
	var sets []string
	for set := range declared {
		sets = append(sets, set)
	}
	sort.Strings(sets)
	if len(sets) != 20 || len(wantSameNameCensus) != len(sets) {
		t.Fatalf("census chunks/pins must cover every declared set: sets=%v pins=%d", sets, len(wantSameNameCensus))
	}
	sets = sets[start:end]

	got := map[string]string{}
	for _, set := range sets {
		printed, err := compliance.EmbeddedPrinted(set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		var c sameNameCensus
		scan := func(it oraclegen.Item, id string) {
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil {
				return
			}
			itemBucket := ""
			for _, d := range res.Decisions {
				for k := range d.Picks {
					if !oraclegen.IsNameSelection(d, k) {
						continue
					}
					if k >= len(d.PickRefs) || sameNameCandidates(d.PickRefs[k], d.OptionRefs) < 2 {
						continue
					}
					ref := d.PickRefs[k]
					// Find the emitted answer that NAMES this pick. A step can carry
					// several answers; a multi-pick definition joins them with
					// '^'. A pick XMage answers with a number, a yes/no or a
					// skip token is not a name selection and is skipped (its
					// object identity never reaches XMage's name match).
					answer := ""
					if d.Step >= 0 && d.Step < len(it.XAnswers) {
						var values []string
						for _, a := range it.XAnswers[d.Step] {
							values = append(values, a.Value)
						}
						answer = segmentFor(values, ref)
					}
					if answer == "" {
						continue
					}
					bucket, matches := classifyAnswer(answer, ref, d.OptionRefs)
					if bucket == "unresolved" {
						// The answer named something, but not exactly the
						// pick: report both the defect and the pick.
						t.Errorf("%s: ambiguous pick %q emitted %q, which matches %d distinct offered objects, not exactly the pick (kind=%s resume=%s via=%s pk=%v)", id, ref, answer, matches, d.Kind, d.Resume, d.Via, d.PickKinds)
					}
					// Count ITEMS, not picks: multi-pick dialogs or several
					// decisions in one item must not inflate the pre-fix census.
					if itemBucket == "" || bucket == "unresolved" || bucket == "alias" && itemBucket == "copy" {
						itemBucket = bucket
					}
				}
			}
			switch itemBucket {
			case "alias":
				c.Alias++
			case "copy":
				c.Copy++
			case "unresolved":
				c.Unresolved++
				c.Items = append(c.Items, id)
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
		t.Logf("%s before=%d after=%d %s", set, c.Alias+c.Copy+c.Unresolved, c.Unresolved, got[set])
	}
	for _, set := range sets {
		if got[set] != wantSameNameCensus[set] {
			t.Errorf("%s same-name census: got %s want %s", set, got[set], wantSameNameCensus[set])
		}
	}
}
