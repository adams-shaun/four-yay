package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// servedTriggerEffect returns the Execute chain of the trigger the
// requirement served (stackSlot is the face's trigger index), or nil when it
// names no slot.
func servedTriggerEffect(f *cards.Face, req levelb.Requirement) *cards.SA {
	slot := stackSlot(req)
	if slot == "" {
		return nil
	}
	idx, err := strconv.Atoi(slot)
	if err != nil || idx < 0 || idx >= len(f.Triggers) {
		return nil
	}
	return f.Triggers[idx].Effect
}

// hasExileLibraryLook reports whether the effect chain looks at an
// exile/library zone and asks a ChooseCard over the looked-at cards
// (Fireglass Mentor's Dig "Choose one of them" over the exiled cards): the
// one card-side proof that a declined two-option look pick is XMage's
// mandatory TargetCardInExile and not an "up to" makeChoose.
func hasExileLibraryLook(sa *cards.SA) bool {
	for ; sa != nil; sa = sa.Sub {
		if sa.API == "ChooseCard" {
			switch sa.ParamStr(cards.PKChoiceZone) {
			case "Exile", "Library":
				return true
			}
		}
	}
	return false
}

// scriptLookedPickDecline re-scripts the declined one-card pick after a
// hidden-zone look (Whiskervale Forerunner's "you may reveal a creature card
// ... You may put it onto the battlefield"): XMage poses the ask without a
// chooseUse, so the declined pair's "no" boolean has nothing to answer
// (measured driver error: "Found wrong choice command") and the lone target
// skip declines it. The gate is card-side — the served chain's ChooseCard
// over the looked-at cards — because the same decision shape on a cast item
// (Break Out's cast-resolve row) agrees with the measured pair, so the pose
// follows the card, not the decision. An unproven shape keeps the pair.
func scriptLookedPickDecline(xa [][]oraclegen.XAnswer, sa *cards.SA, decisions []rules.OracleDecision) [][]oraclegen.XAnswer {
	if !hasExileLibraryLook(sa) {
		return xa
	}
	for _, d := range decisions {
		if d.Kind != "choose_n" || d.Resume != "choice" || d.Options != 1 ||
			d.Min != 0 || d.Max != 1 || len(d.Picks) != 0 || d.Step < 0 || d.Step >= len(xa) {
			continue
		}
		looked := false
		for _, e := range decisions {
			if e.Step == d.Step && e.Seat == d.Seat && e.Resume == "look_ack" {
				looked = true
			}
		}
		if !looked {
			continue
		}
		as := xa[d.Step]
		at := -1
		for i, a := range as {
			if a.Seat != d.Seat || a.Kind != "choice" || a.Value != "no" {
				continue
			}
			if at >= 0 {
				at = -2
				break
			}
			at = i
		}
		if at < 0 {
			// No lone "no" to drop: a second boolean in the step would make
			// the swap ambiguous, so the pair stays as scripted.
			continue
		}
		// Drop the pair's "no" boolean; the [target_skip] alone ends
		// XMage's ask on the target queue.
		xa[d.Step] = append(append([]oraclegen.XAnswer{}, as[:at]...), as[at+1:]...)
	}
	return xa
}

// scriptSacrificeUnlessDiscardDecline re-scripts the unless-pay decline of a
// "sacrifice a permanent unless you discard a card" trigger (Bebop &
// Rocksteady's DB$ Sacrifice | UnlessCost$ Discard<1/Card> | UnlessPayer$
// You): XMage poses the cost's own ask on the target queue and never a
// chooseUse, so the scripted "no" boolean has nothing to answer (measured
// driver error: "Found wrong choice command"). Only the measured shape is
// re-scripted: the pay branch unreachable (one option offered), the decline
// picked, and exactly one "no" in the step so the swap cannot hit another
// ask's answer. Every other unpayable unless shape keeps the measured
// chooseUse "no" (measured agreeing rows: Painful Quandary, Public
// Thoroughfare, Gutsplitter Gang, ...), pending the host driver-replay
// batch's verdict on those poses.
func scriptSacrificeUnlessDiscardDecline(xa [][]oraclegen.XAnswer, sa *cards.SA, decisions []rules.OracleDecision) [][]oraclegen.XAnswer {
	if sa == nil || sa.API != "Sacrifice" || sa.ParamStr(cards.PKUnlessPayer) != "You" ||
		!strings.HasPrefix(sa.ParamStr(cards.PKUnlessCost), "Discard<") {
		return xa
	}
	for _, d := range decisions {
		if d.Kind != "mode" || d.Resume != "unless_pay" || d.Seat != 0 ||
			d.Options != 1 || len(d.Picks) != 1 || d.Step < 0 || d.Step >= len(xa) {
			continue
		}
		at := -1
		for i, a := range xa[d.Step] {
			if a.Seat == d.Seat && a.Kind == "choice" && a.Value == "no" {
				if at >= 0 {
					at = -2
					break
				}
				at = i
			}
		}
		if at < 0 {
			continue
		}
		xa[d.Step][at] = oraclegen.XAnswer{Seat: d.Seat, Kind: "target", Value: "[target_skip]"}
	}
	return xa
}
