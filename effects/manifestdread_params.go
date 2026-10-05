package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// manifestDreadParams compiles the ManifestDread-specific count and memory
// flag once before its repeated two-card operation.
type manifestDreadParams struct {
	Choices  bool
	Amount   ParamText
	Remember bool
}

func compileManifestDread(sa *cards.SA) manifestDreadParams {
	raw, present := sa.Param(cards.PKAmount)
	return manifestDreadParams{
		Choices:  sa.ParamStr(cards.PKChoices) != "",
		Amount:   ParamText{Text: raw, Present: present && strings.TrimSpace(raw) != ""},
		Remember: isTrue(sa.ParamStr(cards.PKRememberManifested)),
	}
}
