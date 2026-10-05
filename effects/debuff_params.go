package effects

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// DebuffParams is api:Debuff's compiled keyword-removal payload.
type DebuffParams struct {
	paramBinding
	Keywords []string
	Duration string
}

var zeroDebuff DebuffParams
var debuffFront [1 << 10]atomic.Pointer[DebuffParams]

// DebuffOf returns the compiled parameters for sa.
func DebuffOf(sa *cards.SA) *DebuffParams {
	if sa == nil {
		return &zeroDebuff
	}
	slot := &debuffFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileDebuff(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// compileDebuff is the only reader of Debuff's API parameters.
func compileDebuff(sa *cards.SA) *DebuffParams {
	return &DebuffParams{
		paramBinding: bindParams(sa),
		Keywords:     slices.Clip(cards.SplitKeywordList(sa.ParamStr(cards.PKKeywords))),
		Duration:     strings.TrimSpace(sa.ParamStr(cards.PKDuration)),
	}
}
