package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kw:Unleash (CR 702.86): "You may have this creature enter the battlefield
// with a +1/+1 counter on it. While it has a +1/+1 counter on it, it can't
// block."
//
// The choice half is the Riot precedent (CR 702.108): a Forge construct
// choice with no card script, so rules reads the K:Unleash keyword directly.
// Every battlefield entry is caught by applyETBChoiceReplacement
// (rules/replacement.go), which parks the move until the answer is logged.
// Apply's Move folds the answer ("counter" -> a +1/+1 counter) on battlefield
// entry (events/apply.go), so every entry path and a log-only replay agree.
//
// The can't-block half is read in combat.CanBlock (rules/combat/block.go) beside the
// Suspected designation -- the blocker-side gate every option and validation
// path already shares.

// chooseUnleash is the chooseFor state while a parked non-cast entry waits
// on its Unleash answer. Numbered above chooseAttackPay; only pairwise
// distinctness matters (rules/cast.go's chooseFor doc).
const chooseUnleash chooseFor = chooseAttackPay + 1

// unleashOptions are the two answers of the as-enters Unleash choice, in the
// fixed order the entry replacement offers: index 0 takes the counter, index 1
// declines it. Index 0 is also the deterministic bot/host answer (option 0),
// which CR 702.86's "you may" permits.
func unleashOptions(id state.ObjID, p state.PlayerID) []decision.Option {
	return []decision.Option{
		{Index: 0, Kind: "unleash", Label: "Enter with a +1/+1 counter", Obj: id, Player: p},
		{Index: 1, Kind: "unleash", Label: "Enter without a counter", Obj: id, Player: p},
	}
}

// applyUnleashReplacement is a defensive fallback for an Unleash entry that
// bypasses applyETBChoiceReplacement. The same overwrite guard applies: never
// park on an ask while another decision is outstanding, and never re-ask an
// object whose choice was already recorded. Entries of face-down objects (a manifest or cloak) are
// vanilla 2/2 creatures (CR 708.5): the FaceDown state is folded by Apply's
// Move AFTER this replacement dispatch runs, so o.Face() still exposes the
// hidden printed face here and the keyword read below would otherwise match
// it -- the guard on the incoming event's counter is what keeps a face-down
// entry from being parked on the ask at all (the Siege precedent's exact
// shape, applySiegeProtector): the Choose event is not Secret, so emitting
// one would name the hidden card in the public transcript.
func (e *Engine) applyUnleashReplacement(ev events.Event) bool {
	if ev.To != state.ZBattlefield || e.unleashMove != nil || e.pending != nil {
		return false
	}
	// A face-down entry (manifest or cloak, CR 708.5) is a vanilla 2/2
	// creature: no as-enters choice, no counter, no public Choose event that
	// would leak the hidden card. See the Siege guard in
	// applySiegeProtector (rules/replacement.go) for the shared reasoning.
	if events.IsFaceDownEntry(ev.Counter) {
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone == state.ZBattlefield || o.Face() == nil ||
		!o.Face().HasKeyword("Unleash") || o.UnleashChoice != "" {
		return false
	}
	move := ev
	e.unleashMove = &move
	d := &decision.Decision{Player: o.Controller, Kind: decision.KChoose, Min: 1, Max: 1,
		Source: o.ID, Prompt: "Choose how this creature enters (with a +1/+1 counter or without)",
		Options: unleashOptions(o.ID, o.Controller)}
	e.choosing = chooseUnleash
	e.ask(d)
	return true
}

func init() {
	effects.RegisterNonAPI("kw:Unleash")
}
