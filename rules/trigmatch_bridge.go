package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/trigmatch"
	"github.com/adams-shaun/gorge/state"
)

// The rules -> rules/trigmatch bridge (W5 E3), on the rules/cost_vocab.go
// precedent: the trigger-condition comparison grammar and the two event
// classifiers moved into rules/trigmatch with the matchers that own them, and
// these forwarders keep their historical rules names, so the static,
// replacement, activation and turn-ledger readers that share them stay
// untouched. Every other rules caller of a moved matcher calls trigmatch
// directly with boardOf(e).

// lifeLoss is trigmatch.LifeLoss: the player and amount of a life-losing
// event (CR 120.3 damage to a player, a negative LifeChange).
func lifeLoss(ev events.Event) (state.PlayerID, int32, bool) { return trigmatch.LifeLoss(ev) }

// leftBattlefield is trigmatch.LeftBattlefield: ev moves an object off the
// battlefield.
func leftBattlefield(ev events.Event) bool { return trigmatch.LeftBattlefield(ev) }

// compareLife, comparePresent, splitCompare and applyCompare are the Forge
// <OP><N> comparison grammar (trigmatch/compare.go).
func compareLife(have int32, cmp string) bool             { return trigmatch.CompareLife(have, cmp) }
func comparePresent(have int, cmp string) bool            { return trigmatch.ComparePresent(have, cmp) }
func splitCompare(cmp string) (op string, n int, ok bool) { return trigmatch.SplitCompare(cmp) }
func applyCompare(have int, op string, n int) bool        { return trigmatch.ApplyCompare(have, op, n) }
