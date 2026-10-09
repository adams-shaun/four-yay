// Level-B gate. checkB runs the level-A checks unchanged, then for every
// card that passed level A and is neither a basic land, hand-covered nor
// XMage-lacking, walks the card's level-B requirements (compliance/levelb)
// and applies the level-A verdict-row checks to each generable requirement's
// scenario. Every unmet requirement reports the level-A "no generated
// scenario (...)" wording (or a row problem), so adopt.Bucket classifies it
// unchanged (spec 2026-10-05-compliance-level-b.md section 4).
package gate

import (
	"fmt"
	"sort"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
)

// checkB is the level-B claim. It returns the level-A problems, plus one
// problem for every unmet requirement of a card that passed level A. A
// requirement covered by the level-A scenario (a face-0 self-ETB trigger the
// cast-resolve scenario settles) adds nothing.
func checkB(reg *cards.Registry, root, set string) ([]Problem, error) {
	aProbs, scan, okA, err := checkA(reg, root, set)
	if err != nil {
		return nil, err
	}
	out := append([]Problem(nil), aProbs...)
	bad := func(card, format string, a ...any) {
		out = append(out, Problem{card, fmt.Sprintf(format, a...)})
	}
	for _, printed := range scan.names {
		name, ok := compliance.CorpusNameFold(scan.has, scan.folded, printed)
		if !ok {
			continue // already a level-A problem
		}
		if !okA[name] {
			continue // the card has a level-A problem; level B adds nothing
		}
		c, _ := reg.Lookup(name)
		if isBasicLand(c) {
			continue
		}
		if scan.hand[name] {
			// A hand-authored scenario covers the card wholesale at B too.
			continue
		}
		bProblems(reg, name, c, scan, bad)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Card < out[j].Card })
	return out, nil
}

// bProblems reports one problem per unmet level-B requirement of c, in
// requirement order. It is the whole per-card level-B walk, so the covered-
// by-A path can be exercised with a synthetic card.
func bProblems(reg *cards.Registry, name string, c *cards.Card, scan *setScan, bad func(string, string, ...any)) {
	// The pass names a level-B scenario (and keys its verdict rows) by the
	// card's first face -- a Room or split card's verdict lives under
	// "Bottomless Pool", not "Bottomless Pool // Locker Room" -- and
	// generates the scenario from the face, the way checkA looks level-A
	// rows up and genManifest names its items. name stays the whole printed
	// spelling the XMage stamp is read from.
	face := name
	if c != nil && len(c.Faces) > 0 && c.Faces[0].Name != "" {
		face = c.Faces[0].Name
	}
	rows := scan.verdicts[name]
	if len(rows) == 0 && face != name {
		rows = scan.verdicts[face]
	}
	for _, req := range levelb.Requirements(c) {
		if req.CoveredByA {
			// The level-A cast-resolve scenario already settles it; the
			// level-A row covers the card.
			continue
		}
		it, skip := templates.GenerateB(reg, face, req)
		if skip != nil {
			bad(name, "no generated scenario (%s: %s) and no hand oracle scenario", req.Key, skip.Reason)
			continue
		}
		if xm, ok := scan.xmageSpelling[compliance.FoldName(name)]; ok && xm != it.Card {
			it.XMageName = xm
		}
		rowOK(reg, it, rows, bad)
	}
}
