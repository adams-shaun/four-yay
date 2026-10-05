// The set-mechanic / keyword-action trigger modes (task triage-478c51d1).
//
// Crewed, Saddled, BecomesSaddled, BecomesPlotted and SacrificedOnce are
// implemented rules-side (rules/trigmatch/*.go), so their coverage keys are
// declared here from the effects side -- the gift.go / evolved.go /
// elemental-bend precedent. Without this, cards.Registry.Coverage would keep
// reporting each `trig:<Mode>` as an unimplemented primitive and the
// compliance gate would stay red even though a matcher is registered.
package effects

func init() {
	RegisterNonAPI(
		"trig:Crewed", "trig:Saddled", "trig:BecomesSaddled", "trig:BecomesPlotted", "trig:SacrificedOnce",
	)
}
