package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/rules"
)

// prependCombatDamageAnswers scripts XMage's combat-damage assignment dialog
// for an attack+block pair whose damage the engine assigns deterministically
// (agent 20261009T041408Z, cluster C2, combat half). The engine poses no
// decision for a trample attacker's assignment (rules/combat.go caps every
// blocker at lethal in declaration order and tramples the remainder), but
// XMage poses getMultiAmountWithIndividualConstraints whenever the attacker
// has trample and the blockers do not absorb all of it
// (mage/game/combat/CombatGroup.java: remainingDamage > 0 or more than one
// blocker). Unscripted, that ask dangles and the replay throws "Missing
// choice in multi amount".
//
// The dialog's messages are the BLOCKERS only, in block-declaration order --
// the unassigned damage tramples to the defender by itself, and the defender
// is never an option (HYPOTHESIS, labelled for the host replay batch; read
// off CombatGroup's damageDivision build, not measured against a live
// XMage). Each message consumes one "X=<n>" amount answer in order, so the
// answers are one per blocker carrying the engine's own assignment. They are
// PREPENDED at the pass_to step that follows the block -- combat damage
// resolves while that step advances the game, so its ask leads every other
// answer the step already carries (the combat-damage triggers' asks).
//
// A scenario whose attacker lacks trample, whose blockers absorb all the
// damage with a single blocker, or whose combat participants strike at a
// different damage step (first/double strike) poses no dialog in XMage, so
// no answers are synthesized and the item's bytes are unchanged.
func prependCombatDamageAnswers(res rules.OracleResult, sc Scenario, out [][]XAnswer) {
	if len(res.Snapshots) == 0 {
		return
	}
	for bi, st := range sc.Steps {
		if st.Op != "block" || len(st.Blocks) == 0 {
			continue
		}
		if bi+1 >= len(res.Snapshots) {
			return
		}
		snap := res.Snapshots[bi+1]
		byRef := map[string]rules.OracleSnapPerm{}
		for _, p := range snap.Permanents {
			byRef[p.Ref] = p
		}
		// Group the block step's pairs by the attacker they name, in block
		// order; a block step blocks exactly the previous attack step's
		// attackers.
		order := []string{}
		blockers := map[string][]string{}
		for _, pair := range st.Blocks {
			blocker, attacker := pair[0], pair[1]
			if _, seen := blockers[attacker]; !seen {
				order = append(order, attacker)
			}
			blockers[attacker] = append(blockers[attacker], blocker)
		}
		answers := []XAnswer{}
		for _, attacker := range order {
			a, ok := byRef[attacker]
			if !ok {
				return
			}
			seat, ok := seatOfRef(attacker)
			if !ok {
				return
			}
			power, _, ok := parsePT(a.PT)
			if !ok || power <= 0 || !hasKeyword(a.Keywords, "trample") {
				continue
			}
			// First/double strike changes which pass assigns and which
			// blockers survive to absorb; leave those scenarios to their own
			// measured class.
			if hasKeyword(a.Keywords, "first strike") || hasKeyword(a.Keywords, "double strike") {
				continue
			}
			dt := hasKeyword(a.Keywords, "deathtouch")
			remaining := power
			absorbed := 0
			assign := make([]int, 0, len(blockers[attacker]))
			for _, ref := range blockers[attacker] {
				b, ok := byRef[ref]
				if !ok {
					return
				}
				if hasKeyword(b.Keywords, "first strike") || hasKeyword(b.Keywords, "double strike") {
					return
				}
				_, toughness, ok := parsePT(b.PT)
				if !ok {
					return
				}
				need := toughness
				if dt && need > 1 {
					need = 1
				}
				give := remaining
				if give > need {
					give = need
				}
				assign = append(assign, give)
				remaining -= give
				absorbed += give
			}
			// XMage poses the dialog only when the blockers leave damage
			// unassigned or there is more than one message; a single blocker
			// that absorbs everything is auto-assigned on both sides.
			if len(assign) <= 1 && absorbed >= power {
				continue
			}
			for _, n := range assign {
				answers = append(answers, XAnswer{seat, "amount", strconv.Itoa(n)})
			}
		}
		if len(answers) == 0 {
			continue
		}
		// The answers lead the pass_to step that follows the block: the
		// combat damage step runs inside that step's advance, ahead of the
		// step's own asks.
		for j := bi + 1; j < len(sc.Steps); j++ {
			if sc.Steps[j].Op == "pass_to" {
				for len(out) < len(sc.Steps) {
					out = append(out, nil)
				}
				out[j] = append(append([]XAnswer(nil), answers...), out[j]...)
				break
			}
		}
	}
}

// seatOfRef reads the "pN" prefix of a scenario ref.
func seatOfRef(ref string) (int, bool) {
	if !strings.HasPrefix(ref, "p") {
		return 0, false
	}
	rest := ref[1:]
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:i])
	if err != nil {
		return 0, false
	}
	return n, true
}

// parsePT splits a snapshot's "P/T" field.
func parsePT(pt string) (power, toughness int, ok bool) {
	p, t, found := strings.Cut(pt, "/")
	if !found {
		return 0, 0, false
	}
	power, err := strconv.Atoi(strings.TrimSpace(p))
	if err != nil {
		return 0, 0, false
	}
	toughness, err = strconv.Atoi(strings.TrimSpace(t))
	if err != nil {
		return 0, 0, false
	}
	return power, toughness, true
}

// hasKeyword reports whether the snapshot keyword list names kw
// (case-insensitive; the snapshot spells keywords as their rule names).
func hasKeyword(keywords []string, kw string) bool {
	for _, k := range keywords {
		if strings.EqualFold(k, kw) {
			return true
		}
	}
	return false
}
