package effects

import (
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// SurveilParams contains the parameters shared by the Surveil ask and its
// answer record. RememberKept is compiled here so both answered and no-host
// resolution use the same interpretation.
type SurveilParams struct {
	paramBinding
	RememberKept bool
}

func compileSurveil(sa *cards.SA) *SurveilParams {
	return &SurveilParams{
		paramBinding: bindParams(sa),
		RememberKept: strings.EqualFold(strings.TrimSpace(sa.ParamStr(cards.PKRememberKept)), "True"),
	}
}

// SurveilOf returns the compiled parameter record for this ability.
func SurveilOf(sa *cards.SA) *SurveilParams {
	if p := surveilFront[paramMapSlot(sa.Params)].Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileSurveil(sa)
	if sa.Params != nil {
		surveilFront[paramMapSlot(sa.Params)].Store(p)
	}
	return p
}

var surveilFront [1 << 10]atomic.Pointer[SurveilParams]
