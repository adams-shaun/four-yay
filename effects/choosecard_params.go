package effects

import (
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// ChooseCardParams contains parameters consumed specifically by api:ChooseCard.
type ChooseCardParams struct {
	paramBinding
	ImprintChosen bool
}

var chooseCardFront [1 << 10]atomic.Pointer[ChooseCardParams]

// ChooseCardOf returns the parsed ChooseCard-specific operands for sa.
func ChooseCardOf(sa *cards.SA) *ChooseCardParams {
	if sa == nil {
		return &ChooseCardParams{}
	}
	slot := &chooseCardFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := &ChooseCardParams{paramBinding: bindParams(sa)}
	p.ImprintChosen = isTrue(sa.ParamStr(cards.PKImprintChosen))
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}
