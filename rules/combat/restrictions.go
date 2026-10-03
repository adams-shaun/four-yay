package combat

import (
	"math"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// HiddenKeywordFlags is the parsed meaning of one derived keyword line that
// carries an English combat-restriction/requirement sentence. Forge delivers
// these three the same way -- as keyword TEXT, sometimes under a
// HiddenKeywords$ Animate parameter, sometimes under a Pump/PumpAll KW$ -- so
// ONE reader every rules consumer calls is what keeps the spellings from
// drifting apart (a new equivalent spelling is then one switch arm).
type HiddenKeywordFlags struct {
	CantAttack    bool
	CantBlock     bool
	MustBlock     bool
	UntapNextStep bool
}

// ParseHiddenKeyword reads one derived keyword line (already the head, via
// cards.KeywordHead) into its combat meaning, normalising Forge's optional
// "HIDDEN " marker away first because the corpus spells the SAME restriction
// both ways. Only the three measured phrase shapes are recognised; any other
// sentence contributes nothing (the deliberate fail-closed direction for a
// per-keyword switch). The comparison is case-insensitive and the phrase is
// matched whole -- a longer sentence that merely contains one of these (a
// conditional rider) is a different grant and must not borrow the
// unconditional meaning.
func ParseHiddenKeyword(k string) HiddenKeywordFlags {
	if cards.IsHiddenUntapNextStepKeyword(k) {
		return HiddenKeywordFlags{UntapNextStep: true}
	}
	head := strings.TrimSpace(strings.TrimPrefix(cards.KeywordHead(k), "HIDDEN "))
	switch {
	case strings.EqualFold(head, "CARDNAME can't attack or block."),
		strings.EqualFold(head, "CantAttackOrBlock"):
		// The compound spelling imparts BOTH restrictions (Opportunistic
		// Dragon, Extraction Specialist); it must satisfy the cant-block
		// reader as well as the cant-attack one, so it is matched before
		// either simple spelling. Printed K: lines are canonicalised to the
		// head; runtime KW$ grants retain the sentence spelling.
		return HiddenKeywordFlags{CantAttack: true, CantBlock: true}
	case strings.EqualFold(head, "CARDNAME can't attack."):
		return HiddenKeywordFlags{CantAttack: true}
	case strings.EqualFold(head, "CARDNAME can't block."):
		return HiddenKeywordFlags{CantBlock: true}
	case strings.EqualFold(head, "CARDNAME must be blocked if able."):
		return HiddenKeywordFlags{MustBlock: true}
	case strings.EqualFold(head, "MustBlock"):
		// The canonical head cards/parse.go rewrites the sentence form to
		// (cards/hiddenkeyword.go CanonicalKeywordLine). Both spellings must
		// reach the reader: a printed K: line arrives here as "MustBlock",
		// while a runtime Pump/PumpAll `KW$ HIDDEN CARDNAME must be blocked
		// if able.` grant never passes through the parser and keeps the
		// sentence form above. The head is distinct from the Mode$ MustBlock
		// static (a BLOCKER's duty, MustBlockCandidates): a keyword head and a
		// static mode are separate namespaces, so this arm cannot borrow the
		// blocker-oriented meaning.
		return HiddenKeywordFlags{MustBlock: true}
	}
	return HiddenKeywordFlags{}
}

// DerivedHiddenFlags folds ParseHiddenKeyword over the object's CURRENT
// derived keyword list (printed plus layer-granted), so both an Animate
// HiddenKeywords$ grant and a Pump/PumpAll KW$ grant reach the combat oracle
// alike.
func DerivedHiddenFlags(b Board, id state.ObjID) HiddenKeywordFlags {
	var f HiddenKeywordFlags
	for _, k := range b.Keywords(id) {
		g := ParseHiddenKeyword(k)
		f.CantAttack = f.CantAttack || g.CantAttack
		f.CantBlock = f.CantBlock || g.CantBlock
		f.MustBlock = f.MustBlock || g.MustBlock
		f.UntapNextStep = f.UntapNextStep || g.UntapNextStep
	}
	return f
}

// HasCantBlockKeyword reports whether the object's CURRENT derived keyword
// list carries Forge's textual can't-block grant ("CARDNAME can't block." or
// the compound "CARDNAME can't attack or block."), with or without the
// HIDDEN marker Forge prepends. Unlike HasKeyword, which compares heads
// exactly, ParseHiddenKeyword normalises the optional "HIDDEN " prefix away
// first, because the corpus spells the SAME restriction both ways: many files
// carry `KW$ HIDDEN CARDNAME can't block.` (Pump/PumpAll templates, Concussive
// Bolt) and Incite Hysteria, Unearthly Blizzard and Siegebreaker Giant carry
// the bare `KW$ CARDNAME can't block.`. Both are one derived layer-6 grant and
// must reach the block oracle alike; a hardcoded pair of literals would miss
// the next spelling. The grant is a rules-side casting/blocking option, so it
// is read from the derived list (printed plus layer-granted), never the
// printed face.
func HasCantBlockKeyword(b Board, id state.ObjID) bool {
	return DerivedHiddenFlags(b, id).CantBlock
}

// HasCantAttackKeyword is HasCantBlockKeyword's attacker-side counterpart:
// the derived keyword grant "CARDNAME can't attack." or the compound
// "CARDNAME can't attack or block." (Opportunistic Dragon, Extraction
// Specialist). Read by AttackBlocked, which feeds both the attacker offer
// list and validateAttackers, so a rules-ignorant client can never declare
// the attack and the validator recomputes it with the same oracle.
func HasCantAttackKeyword(b Board, id state.ObjID) bool {
	return DerivedHiddenFlags(b, id).CantAttack
}

// HasMustBeBlockedKeyword reports whether the object's derived keyword list
// carries the attacker-oriented requirement "CARDNAME must be blocked if
// able." (Elemental Uprising, Vengeant Earth, Disturbed Slumber, and the
// Pump/PumpAll KW$ carriers). This is the ATTACKER's CR 509.1c requirement to
// receive at least one legal blocker -- not the blocker-oriented Mode$
// MustBlock (MustBlockCandidates), which requires a particular BLOCKER to
// block. The requirement's feasibility is decided by askBlockers over the
// offered pairs, never here.
func HasMustBeBlockedKeyword(b Board, id state.ObjID) bool {
	return DerivedHiddenFlags(b, id).MustBlock
}

// BlockRestricted reports whether blocker is forbidden from blocking
// attacker (CantBlock, CantBlockBy, or a granted can't-block keyword).
// Called from CanBlock, which rules' askBlockers and handleBlockers
// both use for real declare-blockers option generation and validation.
func BlockRestricted(b Board, blocker, attacker state.ObjID) bool {
	// Forge's Pump/PumpAll KW$ (HIDDEN) CARDNAME can't block. is a derived
	// layer-6 grant, not a static. Read it here so the same restriction
	// governs offered blocks and validation, including Concussive Bolt.
	if HasCantBlockKeyword(b, blocker) {
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
	for ceI, ceL := 0, b.Active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantBlockBy" {
			continue
		}
		atkSpec := ce.RestrictParams["ValidAttacker"]
		if atkSpec == "" {
			atkSpec = ce.RestrictParams["ValidCard"]
		}
		// The registration's captured PLAYERS ride the consultation-time
		// channel: a static consultation never has Resolving set, so the
		// RememberedPlayer referent resolves them only through the
		// context's RememberedPlayers (The Motherlode, Excavator's
		// RememberObjects$ TargetedController -- the defender of the
		// destroyed land), bound with the remembered objects.
		if !b.MatchesSpec(atkSpec, attacker, ce.Source, ce.Controller, ce.Remembered, ce.RememberedPlayers) {
			continue
		}
		blkSpec, ok := ce.RestrictParams["ValidBlocker"]
		if !ok {
			return true
		}
		if b.MatchesSpec(blkSpec, blocker, ce.Source, ce.Controller, ce.Remembered, ce.RememberedPlayers) {
			return true
		}
	}
	for _, sv := range b.Statics("CantBlock") {
		// The shared continuous gate evaluates Condition$, IsPresent$ and
		// CheckSVar$ families with the same fail-closed semantics used by
		// continuous effects.
		if !b.StaticGateHolds(sv) {
			continue
		}
		if b.MatchesStaticSpec(sv.Params["ValidCard"], blocker, sv) {
			return true
		}
	}
	for _, sv := range b.Statics("CantBlockBy") {
		// Apply the same shared per-static gate as the CantBlock loop above.
		if !b.StaticGateHolds(sv) {
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
		if !b.MatchesStaticSpec(attackerSpec, attacker, sv) {
			continue
		}
		spec, ok := sv.Params["ValidBlocker"]
		if !ok {
			return true
		}
		if b.MatchesStaticSpec(spec, blocker, sv) {
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
	for ceI, ceL := 0, b.Active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantBlockBy" {
			continue
		}
		attackerSpec := ce.RestrictParams["ValidAttacker"]
		if attackerSpec == "" {
			attackerSpec = ce.RestrictParams["ValidCard"]
		}
		// The matches bind the same consultation-time player channel as the
		// first walk above.
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
		} else if !b.MatchesSpec(attackerSpec, attacker, ce.Source, ce.Controller, ce.Remembered, ce.RememberedPlayers) {
			continue
		}
		if spec, ok := ce.RestrictParams["ValidBlocker"]; ok {
			if !b.MatchesSpec(spec, blocker, ce.Source, ce.Controller, ce.Remembered, ce.RememberedPlayers) {
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

// MinMaxBlockerBounds reports the blocker-count bounds an attacking creature
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
// reached through DB$ Effect | StaticAbilities$) is NOT seen here: Board.Statics
// reads printed statics only, the same Effect-delivered gap AGENTS.md records
// for other modes.
func MinMaxBlockerBounds(b Board, attacker state.ObjID) (min, max int, minOK, maxOK, all bool) {
	max = math.MaxInt32
	for _, sv := range b.Statics("MinMaxBlocker") {
		if !minMaxBlockerParamsReadable(sv.Params) {
			continue
		}
		if !b.StaticGateHolds(sv) {
			continue
		}
		if !b.MatchesStaticSpec(sv.Params["ValidCard"], attacker, sv) {
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

// AttackBlocked reports whether creature id is forbidden from being declared
// attacking defender this combat — an Effect-registered CantAttack restriction
// (Call for Aid's "You can't attack that player this turn") or a face
// CantAttack static (the Vow cycle's "can't attack you"). A restriction with
// no Target$ (the "Creatures can't attack." shapes) blocks every defender.
// Consulted at the two (attacker, defender) enforcement points — askAttackers'
// option filter and validateAttackers — and by mustAttackRequired's
// attackDutyDischargeable gate (CR 508.1d's "if able").
//
// A face static's conditional parameter family is read here (task
// combatres-cantattack, extended by combatres-cantattack-present; the present
// family became reachable on this path with compound-statics1):
// Board.StaticGateHolds evaluates ClassBand$, the IsPresent$/IsPresent2$ +
// PresentCompare$ count family (PresentZone$ Battlefield/Graveyard/Exile/Hand/
// Stack; see countStaticPresent), and CheckSVar$/SVarCompare$/Condition$,
// and UnlessDefenderHolds evaluates UnlessDefender$ against the defender (the
// creature may attack exactly when the defended player satisfies the
// predicate), so a line carrying them is ENFORCED, not skipped. Commit
// f81f996e split a compound `S:Mode$ CantAttack,CantBlock` line into one
// static per mode sharing one Params map, so Bast, Panther Goddess's CantAttack
// half now carries the shared IsPresent$ Creature.YouCtrl | PresentCompare$
// LE2 gate. A static carrying any OTHER parameter still fails
// CantAttackParamsReadableForRules and is skipped whole -- the deliberate
// permissive direction, so a gate this build cannot evaluate never becomes an
// unconditional restriction.
func AttackBlocked(b Board, id state.ObjID, defender state.PlayerID, attacked state.ObjID) bool {
	// CR 508.1a: a derived keyword grant (Animate HiddenKeywords$ or a
	// Pump/PumpAll KW$) can forbid the attack outright -- "CARDNAME can't
	// attack." or the compound "CARDNAME can't attack or block."
	// (Opportunistic Dragon's stolen permanent, Extraction Specialist's
	// returned creature). The restriction is defender-independent, so it is
	// checked once here, where attackOffers' pair filter and validateAttackers
	// both read it: the offer list drops every pair and the validator
	// recomputes the same answer. HasCantAttackKeyword reads the DERIVED list
	// (printed plus layer-granted), so a face static or an Animate grant is
	// honoured alike; the registered/static CantAttack walk below is unchanged.
	if HasCantAttackKeyword(b, id) {
		return true
	}
	for ceI, ceL := 0, b.Active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantAttack" {
			continue
		}
		if !b.RestrictionApplies(ce, id) {
			continue
		}
		if !RestrictionTargetMatches(b.Game(), ce.RestrictParams["Target"], defender, ce.Controller, ce.Source, ce.RememberedPlayers, attacked) {
			continue
		}
		return true
	}
	for _, sv := range b.Statics("CantAttack") {
		if !effects.CantAttackParamsReadableForRules(sv.Params) || !b.StaticGateHolds(sv) {
			continue
		}
		if spec := strings.TrimSpace(sv.Params["UnlessDefender"]); spec != "" &&
			effects.UnlessDefenderHolds(b.Game(), spec, defender, sv.Controller, sv.Source) {
			continue
		}
		spec := sv.Params["ValidCard"]
		if spec == "" || !b.MatchesSpec(spec, id, sv.Source, sv.Controller, nil, nil) {
			continue
		}
		if !RestrictionTargetMatches(b.Game(), sv.Params["Target"], defender, sv.Controller, sv.Source, nil, attacked) {
			continue
		}
		return true
	}
	return false
}

// RestrictionTargetMatches resolves a CantAttack restriction's Target$
// list against the defender. Player specs match the defending player; a
// Planeswalker.<player-spec> clause matches a qualifying planeswalker that
// defender controls. An absent Target$ applies to every defender. rules'
// CantAttackUnless charge reads the same grammar.
func RestrictionTargetMatches(g *state.Game, spec string, defender, controller state.PlayerID, source state.ObjID, rememberedPlayers []state.PlayerID, attacked state.ObjID) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return true
	}
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if (attacked == 0 && effects.RestrictionPlayerSpecMatches(g, part, defender, controller, source, rememberedPlayers)) ||
			restrictionPlaneswalkerTargetMatches(g, part, defender, controller, source, rememberedPlayers, attacked) {
			return true
		}
	}
	return false
}

// restrictionPlaneswalkerTargetMatches reads one Planeswalker.<player-spec>
// entry in a restriction's Target$ list, scoped to a planeswalker controlled
// by the defender.
func restrictionPlaneswalkerTargetMatches(g *state.Game, spec string, defender, controller state.PlayerID, source state.ObjID, rememberedPlayers []state.PlayerID, attacked state.ObjID) bool {
	parts := strings.SplitN(strings.TrimSpace(spec), ".", 2)
	if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "Planeswalker") {
		return false
	}
	selector := strings.TrimSpace(parts[1])
	if attacked == 0 {
		return false
	}
	o := g.Obj(attacked)
	if o == nil || o.Zone != state.ZBattlefield || o.FaceDown || !printedHasType(o, "Planeswalker") || o.Controller != defender {
		return false
	}
	// Forge's common Target$ form is Planeswalker.YouCtrl. Other
	// controller selectors are evaluated against the restriction source.
	matches := false
	switch strings.ToLower(selector) {
	case "youctrl":
		matches = defender == controller
	case "oppctrl":
		matches = defender != controller
	case "controlledby player.cardowner":
		// Xantcha's owner, not its current controller (which may be an opponent).
		if src := g.Obj(source); src != nil {
			matches = defender == src.Owner
		}
	case "rememberedplayerctrl", "controlledby remembered":
		// Effect registrations capture the named players at resolution time.
		for _, p := range rememberedPlayers {
			if p == defender {
				matches = true
				break
			}
		}
	default:
		matches = effects.RestrictionPlayerSpecMatches(g, selector, defender, controller, source, rememberedPlayers)
	}
	if matches {
		return true
	}
	return false
}

// printedHasType reports whether o's printed face carries the exact type
// word typ (rules' faceHasType): the Planeswalker gate on a Target$ clause
// reads the printed face, as it always has.
func printedHasType(o *state.Object, typ string) bool {
	if o == nil || o.Face() == nil {
		return false
	}
	for _, got := range o.Face().Types {
		if got == typ {
			return true
		}
	}
	return false
}
