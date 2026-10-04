package rules

import (
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// pay_vocab.go is package rules' bridge to the pool solver in rules/pay
// (lasagna spec W5 step E7). The types are aliases and every function is a
// one-line forwarder the compiler inlines, so rules keeps its historical
// names while the definitions live in the package that holds no *Engine.

type (
	// manaConv is the colour-conversion set one payment resolves under.
	manaConv = pay.Conv
	// pipRider is the payer-side may-play rider set.
	pipRider = pay.PipRider
	// pipAlt is one alternative payment of a pip.
	pipAlt = pay.PipAlt
	// manaPayment is a resolved pool payment.
	manaPayment = pay.Payment
)

func resolveMana(c Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, conv *manaConv) (manaPayment, bool) {
	return pay.ResolveMana(c, pool, snow, typed, life, conv)
}

func resolveManaWith(c Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, bLifeOK bool, rider pipRider, conv *manaConv) (manaPayment, bool) {
	return pay.ResolveManaWith(c, pool, snow, typed, life, bLifeOK, rider, conv)
}

func payable(c Cost, pool, snow state.Mana, typed [7]state.Mana, life int32) bool {
	return pay.Payable(c, pool, snow, typed, life)
}

func poolCanPay(c Cost, p state.Mana) bool { return pay.PoolCanPay(c, p) }

func poolPay(c Cost, p state.Mana) (state.Mana, bool) { return pay.PoolPay(c, p) }
