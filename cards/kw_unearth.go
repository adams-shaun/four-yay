// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import "strings"

func kwUnearth(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	if has("A", k) {
		return
	}
	// CR 702.84a (Unearth): "'Unearth [cost]' means '[Cost]: Return this
	// card from your graveyard to the battlefield. It gains haste. Exile
	// it at the beginning of the next end step or if it would leave the
	// battlefield. Unearth only as a sorcery.'" The ability itself is the
	// ordinary graveyard activation the Encore/Embalm expansions already
	// produce: one AB$ ChangeZone from the graveyard onto the battlefield,
	// gated to sorcery timing. The three riders the return implies (haste,
	// the end-step exile, and the exile-instead replacement) are NOT
	// expressible as a single Forge body, so the Unearth$ True marker is
	// read by rules (rules/unearth.go) at each stage: effects/zone.go
	// stamps the entry MoveZone with it (the entered_unearthed counter),
	// rules' entry hook grants haste and registers the end-step exile, and
	// rules' replacement dispatch redirects any leave to exile while the
	// promise is live.
	//
	// param is "<cost>" ("3 W B"), occasionally followed by further
	// colon-separated fields no corpus line carries; the first field is the
	// cost.
	cost, _, _ := strings.Cut(param, ":")
	sa, _ := parseSA("", "AB$ ChangeZone | Cost$ "+cost+" | Origin$ Graveyard | Destination$ Battlefield | Defined$ Self | ActivationZone$ Graveyard | SorcerySpeed$ True | Unearth$ True | Keyword$ Unearth | SpellDescription$ Unearth "+cost)
	if sa != nil {
		sa.Params["KeywordLine"] = k
		f.Abilities = append(f.Abilities, sa)
	}
}

func init() { registerKeyword(kwUnearth, "Unearth") }
