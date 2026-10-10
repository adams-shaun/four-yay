package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// staticSetupReplacementGap names why a graveyard may-play permission cannot
// be observed on a card whose own replacement effect redirects what is put
// into the graveyard (Hades, Sorcerer of Eld, Emet-Selch, Unsundered's back
// face: "If a card or token would be put into your graveyard from anywhere,
// exile it instead", beside "During your turn, you may play cards from your
// graveyard"). Setup places the source on the battlefield before the
// graveyard, so the probe card goes through the source's own replacement and
// ends in exile (measured: exile=[Shock], graveyard=[]), where the permission
// never sees it; a hand-built engine with the probe already in the graveyard
// offers the cast. "" when the face has no such replacement.
func staticSetupReplacementGap(f *cards.Face, st cards.Static) string {
	if !st.HasParam(cards.PKMayPlay) ||
		!strings.Contains(strings.ToLower(st.ParamStr(cards.PKAffectedZone)), "graveyard") {
		return ""
	}
	for i := range f.Repls {
		r := &f.Repls[i]
		if r.Event != "Moved" || !strings.EqualFold(strings.TrimSpace(r.ParamStr(cards.PKDestination)), "Graveyard") ||
			strings.TrimSpace(r.ParamStr(cards.PKReplaceWith)) == "" {
			continue
		}
		return "graveyard probe is exiled by the card's own Moved replacement when setup places it"
	}
	return ""
}
