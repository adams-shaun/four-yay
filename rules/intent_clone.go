package rules

import "github.com/adams-shaun/gorge/decision"

// singleChoices backs the logged Choices of a one-choice intent: element v
// is v, and a logged intent answering v holds the capped one-element window
// singleChoices[v:v+1:v+1].
var singleChoices = func() (a [256]int) {
	for i := range a {
		a[i] = i
	}
	return a
}()

// cloneIntentForLog is decision.CloneIntent for the intent Submit records:
// the same values, but a plain one-choice answer -- nearly every Submit --
// shares a read-only one-element window of singleChoices instead of
// allocating its own copy. Nothing writes a recorded intent's Choices in
// place (the log is replay history); a reader that wants to modify one must
// copy it, as with any shared log data.
func cloneIntentForLog(in decision.Intent) decision.Intent {
	if len(in.Choices) == 1 && len(in.Rest) == 0 && in.Payment == nil && in.Announce == nil {
		if v := in.Choices[0]; v >= 0 && v < len(singleChoices) {
			c := in
			c.Choices = singleChoices[v : v+1 : v+1]
			c.Rest = nil
			return c
		}
	}
	return decision.CloneIntent(in)
}
