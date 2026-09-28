package rules

// knownBrokenCostStatics / knownWrongCostStatics are TestCostStaticCensus'
// committed baseline (Ruling R-20's two-direction contract, the
// knownUnsupported shape): card name -> its sorted reason classes, measured
// over the whole corpus. The census fails on a card that newly measures
// BROKEN/WRONG (naming the finding and the engine file:line) AND on an entry
// the build no longer measures (stale). Both tables only ever shrink: a fix
// deletes its rows; a regression is fixed, never added here. On a mismatch
// the test logs the measured table as a Go literal.
//
// BROKEN: the engine can never apply the static (a fail-closed shape).
var knownBrokenCostStatics = map[string]string{
	"Close Encounter":       "RaiseCost Cost$ unmodelled",
	"Hamlet Glutton":        "ValidSpell$ unsupported: Spell.Bargain",
	"Ice Out":               "ValidSpell$ unsupported: Spell.Bargain",
	"Johann's Stopgap":      "ValidSpell$ unsupported: Spell.Bargain",
	"Moonrager's Slash":     "Condition$ Night unsupported",
	"Seal of the Guildpact": "Amount$ unevaluable by EvalCountOK; Relative$ ReduceCost amount unresolvable, no {X}",
}

// WRONG: the engine applies the static but ignores or misreads a parameter.
// OFFER-BLIND findings (applied only at the payment-time reprice, never at
// the offer gate) ride this table with an "OFFER-BLIND: " prefix.
var knownWrongCostStatics = map[string]string{
	"Mana Matrix":   "param unread: UpTo$",
	"Planar Gate":   "param unread: UpTo$",
	"Urza's Filter": "param unread: UpTo$",
}
