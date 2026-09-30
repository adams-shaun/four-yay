package rules

import (
	"math"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// hiddenKeywordFlags is the parsed meaning of one derived keyword line that
// carries an English combat-restriction/requirement sentence. Forge delivers
// these three the same way -- as keyword TEXT, sometimes under a
// HiddenKeywords$ Animate parameter, sometimes under a Pump/PumpAll KW$ -- so
// ONE reader every rules consumer calls is what keeps the spellings from
// drifting apart (a new equivalent spelling is then one switch arm).
type hiddenKeywordFlags struct {
	cantAttack bool
	cantBlock  bool
	mustBlock  bool
}

// parseHiddenKeyword reads one derived keyword line (already the head, via
// cardsKeywordHead) into its combat meaning, normalising Forge's optional
// "HIDDEN " marker away first because the corpus spells the SAME restriction
// both ways. Only the three measured phrase shapes are recognised; any other
// sentence contributes nothing (the deliberate fail-closed direction for a
// per-keyword switch). The comparison is case-insensitive and the phrase is
// matched whole -- a longer sentence that merely contains one of these (a
// conditional rider) is a different grant and must not borrow the
// unconditional meaning.
func parseHiddenKeyword(k string) hiddenKeywordFlags {
	head := strings.TrimSpace(strings.TrimPrefix(cardsKeywordHead(k), "HIDDEN "))
	switch {
	case strings.EqualFold(head, "CARDNAME can't attack or block."),
		strings.EqualFold(head, "CantAttackOrBlock"):
		// The compound spelling imparts BOTH restrictions (Opportunistic
		// Dragon, Extraction Specialist); it must satisfy the cant-block
		// reader as well as the cant-attack one, so it is matched before
		// either simple spelling. Printed K: lines are canonicalised to the
		// head; runtime KW$ grants retain the sentence spelling.
		return hiddenKeywordFlags{cantAttack: true, cantBlock: true}
	case strings.EqualFold(head, "CARDNAME can't attack."):
		return hiddenKeywordFlags{cantAttack: true}
	case strings.EqualFold(head, "CARDNAME can't block."):
		return hiddenKeywordFlags{cantBlock: true}
	case strings.EqualFold(head, "CARDNAME must be blocked if able."):
		return hiddenKeywordFlags{mustBlock: true}
	case strings.EqualFold(head, "MustBlock"):
		// The canonical head cards/parse.go rewrites the sentence form to
		// (cards/hiddenkeyword.go CanonicalKeywordLine). Both spellings must
		// reach the reader: a printed K: line arrives here as "MustBlock",
		// while a runtime Pump/PumpAll `KW$ HIDDEN CARDNAME must be blocked
		// if able.` grant never passes through the parser and keeps the
		// sentence form above. The head is distinct from the Mode$ MustBlock
		// static (a BLOCKER's duty, mustBlockCandidates): a keyword head and a
		// static mode are separate namespaces, so this arm cannot borrow the
		// blocker-oriented meaning.
		return hiddenKeywordFlags{mustBlock: true}
	}
	return hiddenKeywordFlags{}
}

// derivedHiddenFlags folds parseHiddenKeyword over the object's CURRENT
// derived keyword list (printed plus layer-granted), so both an Animate
// HiddenKeywords$ grant and a Pump/PumpAll KW$ grant reach the combat oracle
// alike.
func (e *Engine) derivedHiddenFlags(id state.ObjID) hiddenKeywordFlags {
	var f hiddenKeywordFlags
	for _, k := range e.Derived(id).Keywords {
		g := parseHiddenKeyword(k)
		f.cantAttack = f.cantAttack || g.cantAttack
		f.cantBlock = f.cantBlock || g.cantBlock
		f.mustBlock = f.mustBlock || g.mustBlock
	}
	return f
}

// hasCantBlockKeyword reports whether the object's CURRENT derived keyword
// list carries Forge's textual can't-block grant ("CARDNAME can't block." or
// the compound "CARDNAME can't attack or block."), with or without the
// HIDDEN marker Forge prepends. Unlike HasKeyword, which compares heads
// exactly, parseHiddenKeyword normalises the optional "HIDDEN " prefix away
// first, because the corpus spells the SAME restriction both ways: many files
// carry `KW$ HIDDEN CARDNAME can't block.` (Pump/PumpAll templates, Concussive
// Bolt) and Incite Hysteria, Unearthly Blizzard and Siegebreaker Giant carry
// the bare `KW$ CARDNAME can't block.`. Both are one derived layer-6 grant and
// must reach the block oracle alike; a hardcoded pair of literals would miss
// the next spelling. The grant is a rules-side casting/blocking option, so it
// is read from the derived list (printed plus layer-granted), never the
// printed face.
func (e *Engine) hasCantBlockKeyword(id state.ObjID) bool {
	return e.derivedHiddenFlags(id).cantBlock
}

// hasCantAttackKeyword is hasCantBlockKeyword's attacker-side counterpart:
// the derived keyword grant "CARDNAME can't attack." or the compound
// "CARDNAME can't attack or block." (Opportunistic Dragon, Extraction
// Specialist). Read by attackBlocked, which feeds both the attacker offer
// list and validateAttackers, so a rules-ignorant client can never declare
// the attack and the validator recomputes it with the same oracle.
func (e *Engine) hasCantAttackKeyword(id state.ObjID) bool {
	return e.derivedHiddenFlags(id).cantAttack
}

// hasMustBeBlockedKeyword reports whether the object's derived keyword list
// carries the attacker-oriented requirement "CARDNAME must be blocked if
// able." (Elemental Uprising, Vengeant Earth, Disturbed Slumber, and the
// Pump/PumpAll KW$ carriers). This is the ATTACKER's CR 509.1c requirement to
// receive at least one legal blocker -- not the blocker-oriented Mode$
// MustBlock (mustBlockCandidates), which requires a particular BLOCKER to
// block. The requirement's feasibility is decided by askBlockers over the
// offered pairs, never here.
func (e *Engine) hasMustBeBlockedKeyword(id state.ObjID) bool {
	return e.derivedHiddenFlags(id).mustBlock
}

// blockRestricted reports whether blocker is forbidden from blocking
// attacker (CantBlock, CantBlockBy, or a granted can't-block keyword).
// Called from rules/combat.go's canBlock, which askBlockers and handleBlockers
// both use for real declare-blockers option generation and validation.
func (e *Engine) blockRestricted(blocker, attacker state.ObjID) bool {
	// Forge's Pump/PumpAll KW$ (HIDDEN) CARDNAME can't block. is a derived
	// layer-6 grant, not a static. Read it here so the same restriction
	// governs offered blocks and validation, including Concussive Bolt.
	if e.hasCantBlockKeyword(blocker) {
		return true
	}
	// The Effect-registered CantBlockBy grants walk FIRST, beside the
	// CantTarget precedent (restrictionBlocksTarget): the registered
	// restriction's ValidAttacker$ is matched against the ATTACKER with the
	// registration's Remembered set bound (Rikku Resourceful Guardian's
	// "that creature can't be blocked by creatures your opponents control",
	// RememberObjects$ TriggeredObjectLKICopy -- the gaining creature), and
	// ValidBlocker$ against the would-be blocker with the effect's own
	// controller as the spec's "you". The registration path (effEffect's
	// CantBlockByRestrictionParamsReadable whitelist) already excluded gated
	// bodies, so
	// no per-static condition gate runs here.
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantBlockBy" {
			continue
		}
		atkSpec := ce.RestrictParams["ValidAttacker"]
		if atkSpec == "" {
			atkSpec = ce.RestrictParams["ValidCard"]
		}
		sc := e.specCtx(ce.Source, ce.Controller)
		for _, r := range ce.Remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		// The registration's captured PLAYERS ride the consultation-time
		// channel: a static consultation never has Resolving set, so the
		// RememberedPlayer referent resolves them only through this field
		// (The Motherlode, Excavator's RememberObjects$ TargetedController
		// -- the defender of the destroyed land).
		sc.RememberedPlayers = ce.RememberedPlayers
		if !e.matchesSpec(atkSpec, attacker, sc) {
			continue
		}
		blkSpec, ok := ce.RestrictParams["ValidBlocker"]
		if !ok {
			return true
		}
		if e.matchesSpec(blkSpec, blocker, sc) {
			return true
		}
	}
	for _, sv := range e.activeStatics("CantBlock") {
		// The shared continuous gate evaluates Condition$, IsPresent$ and
		// CheckSVar$ families with the same fail-closed semantics used by
		// continuous effects.
		if !e.continuousGateHolds(sv) {
			continue
		}
		if e.matchesSpec(sv.Params["ValidCard"], blocker, e.staticSpecCtx(sv)) {
			return true
		}
	}
	for _, sv := range e.activeStatics("CantBlockBy") {
		// Apply the same shared per-static gate as the CantBlock loop above.
		if !e.continuousGateHolds(sv) {
			continue
		}
		// ValidAttacker$ is Forge's own spelling for the attacker side of a
		// CantBlockBy static (Steel Leaf Champion's "Creature.Self", the
		// Unblockable pump templates' "Card.IsRemembered", the blocker-side
		// "CARDNAME can block only creatures with flying" shape) — 594 corpus
		// files carry it and NONE of them spell the attacker with ValidCard$;
		// that spelling is the hand-authored test fixture's. The historical
		// ValidCard$ read stays as the fallback so both grammars work, and an
		// SA carrying neither fails closed exactly as before (the empty spec
		// matches nothing).
		attackerSpec := sv.Params["ValidAttacker"]
		if attackerSpec == "" {
			attackerSpec = sv.Params["ValidCard"]
		}
		if !e.matchesSpec(attackerSpec, attacker, e.staticSpecCtx(sv)) {
			continue
		}
		spec, ok := sv.Params["ValidBlocker"]
		if !ok {
			return true
		}
		if e.matchesSpec(spec, blocker, e.staticSpecCtx(sv)) {
			return true
		}
	}
	// The Effect-registered CantBlockBy restrictions (task cbb1): an
	// `AB$ Effect | StaticAbilities$ Unblockable` whose SVar body is
	// `Mode$ CantBlockBy | ValidAttacker$ Card.IsRemembered` -- Suspicious
	// Bookcase's "{3},{T}: Target creature can't be blocked this turn", the
	// dominant unblockable template (measured 246 corpus files carry the
	// Effect-delivered shape) -- registers through effEffect's restriction
	// case and is consulted here beside the face statics, so the remembered
	// creature really is unblockable for the effect's lifetime. The
	// registration gate (effects.CantBlockByRestrictionParamsReadable) has
	// already refused every body whose scoping this loop cannot evaluate
	// (ValidBlockerRelative$, IsPresent$/PresentCompare$ gates), so the
	// loop reads ValidAttacker$/ValidBlocker$ unconditionally and the only
	// fail-closed direction is the ordinary matcher's empty-set read.
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantBlockBy" {
			continue
		}
		attackerSpec := ce.RestrictParams["ValidAttacker"]
		if attackerSpec == "" {
			attackerSpec = ce.RestrictParams["ValidCard"]
		}
		sc := e.specCtx(ce.Source, ce.Controller)
		for _, r := range ce.Remembered {
			sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
		}
		// Same consultation-time player channel as the first walk above.
		sc.RememberedPlayers = ce.RememberedPlayers
		if attackerSpec == "" {
			// A spec-less restriction names exactly its remembered set (the
			// restrictionApplies convention, evaluated per-pair here because
			// this read consults one (blocker, attacker) candidate at a
			// time): an attacker outside the set is not restricted.
			remembered := false
			for _, r := range ce.Remembered {
				if r == attacker {
					remembered = true
				}
			}
			if !remembered {
				continue
			}
		} else if !e.matchesSpec(attackerSpec, attacker, sc) {
			continue
		}
		if spec, ok := ce.RestrictParams["ValidBlocker"]; ok {
			if !e.matchesSpec(spec, blocker, sc) {
				continue
			}
		}
		return true
	}
	return false
}

// minMaxBlockerParamsReadable is the parameter whitelist a MinMaxBlocker
// static must pass before its blocker-count bound is enforced. A static
// carrying a semantic parameter this build cannot evaluate is SKIPPED (the
// restriction simply does not apply), the permissive direction for a
// restriction and the same convention CantRestrictionParamsReadable uses for
// CantAttack/CantSacrifice. The gate parameters are whitelisted because
// continuousGateHolds evaluates them (fail-closed).
func minMaxBlockerParamsReadable(params map[string]string) bool {
	for k := range params {
		switch k {
		case "Mode", "ValidCard", "Min", "Max", "Description", "Secondary",
			"Condition", "IsPresent", "IsPresent2", "PresentCompare", "PresentZone",
			"CheckSVar", "SVarCompare", "AffectedZone":
		default:
			return false
		}
	}
	return true
}

// minMaxBlockerBounds reports the blocker-count bounds an attacking creature
// is subject to from every applicable S:Mode$ MinMaxBlocker static (CR 509.1a's
// block-restriction family: "can't be blocked by more than one creature" and
// "can't be blocked except by N or more creatures"). min is the STRICTEST
// Min$ among the matching statics (the largest), max the strictest Max$ (the
// smallest); minOK/maxOK say whether a bound was present at all. all is set by
// Min$ All (Tromokratis: "can't be blocked unless all creatures defending
// player controls block it"), which the caller resolves against the defending
// player's own board.
//
// ValidCard$ is resolved against the ATTACKER (the creature the restriction
// applies to) with the static's host as the spec source, so a non-self scope
// (Vorrac Battlehorns' Creature.EquippedBy, the YouCtrl team statics) reaches
// the right creature. A static whose parameters or gates this build cannot
// read is skipped -- a restriction that cannot be proven must not silently
// apply. The SVar-delivered form (SVar:MinMaxBlocked:Mode$ MinMaxBlocker,
// reached through DB$ Effect | StaticAbilities$) is NOT seen here: activeStatics
// reads printed statics only, the same Effect-delivered gap AGENTS.md records
// for other modes.
func (e *Engine) minMaxBlockerBounds(attacker state.ObjID) (min, max int, minOK, maxOK, all bool) {
	max = math.MaxInt32
	for _, sv := range e.activeStatics("MinMaxBlocker") {
		if !minMaxBlockerParamsReadable(sv.Params) {
			continue
		}
		if !e.continuousGateHolds(sv) {
			continue
		}
		if !e.matchesSpec(sv.Params["ValidCard"], attacker, e.staticSpecCtx(sv)) {
			continue
		}
		if raw, ok := sv.Params["Min"]; ok {
			if strings.EqualFold(strings.TrimSpace(raw), "All") {
				all = true
				continue
			}
			if n, ok := literalBlockCount(raw); ok && (!minOK || n > min) {
				min, minOK = n, true
			}
		}
		if raw, ok := sv.Params["Max"]; ok {
			if n, ok := literalBlockCount(raw); ok && (!maxOK || n < max) {
				max, maxOK = n, true
			}
		}
	}
	return min, max, minOK, maxOK, all
}

// literalBlockCount parses a Min$/Max$ bound: a non-negative integer, the
// only shape the corpus prints. A non-literal value (an SVar name, an
// expression) reports ok=false, so the bound is not enforced rather than
// mis-enforced -- the same permissive direction the whitelist takes.
func literalBlockCount(raw string) (int, bool) {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v < 0 || v > int64(math.MaxInt32) {
		return 0, false
	}
	return int(v), true
}
