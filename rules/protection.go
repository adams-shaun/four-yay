package rules

// Protection (CR 702.16) — the third and last of the M2r ratchet's keyword
// families. Task 15 implements the classic protection cycle end to end:
// a protected permanent cannot be targeted, blocked, or dealt damage by a
// source it is protected from, and cannot be enchanted/equipped/fortified
// by one (CR 702.16b-g). The two cards that pinned the ratchet at the very
// end of the table were both protection-bearers — Goblin Piledriver
// (Protection from blue) and Knight of Infamy (Protection from white) — so
// this task retires them and empties knownUnsupported outright.
//
// The file carries the five registrations the coverage gate needs to call a
// "Protection from <colour>" bearer fully supported, the protectedFrom
// predicate the rest of the engine consults, and the emit-side hooks. The
// engine keeps the actual enforcement inside rules' emit (per config plan),
// NOT on effects.Host: Host stays tiny, the interface grows only toward
// effects' own needs (CastThisTurn and the layer host hooks), and the
// "which event does protection swallow" logic is engine rules, not an
// effect's business.

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// protectedFrom reports whether target is protected from source (CR 702.16):
// true exactly when some DERIVED keyword of target names a quality that
// source carries — "Protection from X itself has no effect on X's source
// being a spell or a permanent; it affects damage, enchantment/equipment
// and targeting". Quality is matched case-insensitively against:
//
//   - a colour word (white/blue/black/red/green) — source has it when its
//     colours (effects.ColorsOf, which applies Devoid) contain that colour;
//   - a permanent-type word (artifacts/creatures/enchantments) or a card-
//     type word (instants/sorceries) — source has it when its face carries
//     that type;
//   - everything — true against any source at all.
//
// Derived keywords include granted ones, so a permanent that gained
// "Protection from red" through a Resolution (e.g. a DB$ Protection
// resolution, or a continuous effect) protects as surely as a printed
// bearer does. (Shroud — CR 702.14 — is enforced by shroudBlocksTarget
// below, not here; protection and shroud are separate keyword families.) target or source being a zero/nonexistent object is never
// protected (players have no ObjID, so a player target is never withheld on
// protection grounds).
func (e *Engine) protectedFrom(target, source state.ObjID) bool {
	if target == 0 || source == 0 {
		return false
	}
	for _, kw := range e.Keywords(target) {
		if q, ok := protectionQuality(kw); ok && e.sourceHasQuality(source, q) {
			return true
		}
	}
	return false
}

// shroudBlocksTarget reports whether the object carries shroud (CR 702.14):
// any derived keyword whose head is "Shroud" — printed K:Shroud or granted
// (Lightning Greaves' "Haste & Shroud" static, a KW$ pump grant) — makes the
// permanent an illegal target for ANY spell or ability, its own controller's
// included (that symmetry is what separates shroud from hexproof). Derived
// keywords include granted ones, exactly like protectedFrom. The caller owns
// the zone gate: shroud functions only while the object is a permanent on
// the battlefield (CR 702.14a), the same CR 604.3 gate the protection and
// CantTarget targeting guards apply, and it must NOT reach the non-target
// attachment path (the CR 303.4f Aura entry in rules/attach.go) or the
// Overload affected census (candidatesFor's targeting=false arm).
func (e *Engine) shroudBlocksTarget(id state.ObjID) bool {
	if id == 0 {
		return false
	}
	for _, kw := range e.Keywords(id) {
		if strings.EqualFold(cards.KeywordHead(kw), "Shroud") {
			return true
		}
	}
	return false
}

// hexproofBlocksTarget reports whether id carries hexproof (CR 702.11) that
// withholds it from a targeting spell or ability controlled by targeting.
// It mirrors shroudBlocksTarget except for the controller test: hexproof is
// ASYMMETRIC (CR 702.11b), so its controller may still target it, while
// shroud blocks every controller. Derived keywords include granted ones,
// exactly like protectedFrom and shroud. A quality-bearing form
// (K:Hexproof:Artifact, "hexproof from artifacts", CR 702.11c) withholds
// only when the targeting SOURCE carries that quality, reusing protection's
// sourceHasQuality; a quality this build cannot evaluate fails closed and
// never withholds (the permissive direction a restriction's fail-closed
// convention takes). The caller owns the battlefield zone gate (CR 604.3),
// the same gate shroud and protection use, and must NOT route the
// non-target attachment path here (hexproof never stops its controller's own
// Auras/Equipment, CR 702.11b).
func (e *Engine) hexproofBlocksTarget(id state.ObjID, targeting state.PlayerID, source state.ObjID) bool {
	if id == 0 {
		return false
	}
	o := e.G.Obj(id)
	if o == nil || o.Controller == targeting {
		return false
	}
	for _, kw := range e.Keywords(id) {
		q, ok := hexproofQuality(kw)
		if !ok {
			continue
		}
		if q == "" || e.sourceHasQuality(source, q) {
			return true
		}
	}
	return false
}

// hexproofQuality splits one derived keyword into its hexproof quality: ""
// for the plain keyword (CR 702.11b, "hexproof"), the spec after the first
// colon for a parameterised one (CR 702.11c, "Hexproof:Artifact"), and
// ok=false for any keyword that is not hexproof at all. The display-text
// field after a second colon is dropped, the same shape protectionQuality
// reads for K:Protection:<Spec>:<desc>.
func hexproofQuality(kw string) (string, bool) {
	const head = "Hexproof"
	if len(kw) < len(head) || !strings.EqualFold(kw[:len(head)], head) {
		return "", false
	}
	rest := strings.TrimSpace(kw[len(head):])
	if rest == "" {
		return "", true
	}
	if rest[0] != ':' {
		return "", false
	}
	q, _, _ := strings.Cut(strings.TrimSpace(rest[1:]), ":")
	return strings.TrimSpace(q), true
}

// protectionQuality turns one derived keyword into the single quality it
// protects against. "Protection from red and from blue" is emitted by Forge
// as TWO keywords joined with "&" — each resolves here to its own keyword
// ("Protection from red", "Protection from blue"), so parsing one quality
// per keyword after the "Protection from " prefix is exactly right and never
// faces the " and from " separator (that never survives inside a single
// keyword string; the parser separates keywords at "&" first, face.go).
// A keyword with no quality after the prefix ("Protection" alone, or a
// stray trailing token) yields nothing.
func protectionQuality(kw string) (string, bool) {
	const prefix = "Protection from "
	if len(kw) >= len(prefix) && strings.EqualFold(kw[:len(prefix)], prefix) {
		q := strings.TrimSpace(kw[len(prefix):])
		return q, q != ""
	}
	// Forge's general spelling is K:Protection:<Spec>:<display text>.
	// Keep only the spec; the following field is reminder text, not syntax.
	if len(kw) >= len("Protection:") && strings.EqualFold(kw[:len("Protection:")], "Protection:") {
		q, _, _ := strings.Cut(strings.TrimSpace(kw[len("Protection:"):]), ":")
		return q, q != ""
	}
	return "", false
}

// sourceHasQuality reports whether the object source carries the given
// protection quality. "everything" is short for "a source that has any
// characteristics"; everything else is matched against the source's colours
// or its face's types (never a player: source==0 already returned at the
// protectedFrom gate).
func (e *Engine) sourceHasQuality(source state.ObjID, q string) bool {
	if strings.EqualFold(q, "everything") {
		return true
	}
	o := e.G.Obj(source)
	if o == nil {
		return false
	}
	// A card object on the stack is a spell. An ability source is represented
	// by its source permanent for protection checks, so it is not a spell.
	if strings.EqualFold(q, "Spell") {
		return o.Zone == state.ZStack && o.Face() != nil
	}
	if strings.EqualFold(q, "Permanent.ThisTurnCast") {
		return o.Zone == state.ZBattlefield && e.permanentCastThisTurn(o.ID)
	}
	// Forge's remaining printed parameterised K:Protection qualities include
	// mana-value bounds, counters and "coloured spells". They cannot be sent
	// through MatchesSpec unchanged: cmc/counters are value predicates and
	// nonColorless is a colour-identity predicate, none of which the generic
	// object filter can safely guess.
	if m := protectionCMC.FindStringSubmatch(q); m != nil {
		n, _ := strconv.Atoi(m[2])
		cmc := o.Face().Cmc()
		return (m[1] == "GE" && cmc >= int32(n)) || (m[1] == "LE" && cmc <= int32(n))
	}
	if m := protectionCounter.FindStringSubmatch(q); m != nil {
		n, _ := strconv.Atoi(m[1])
		return o.Zone == state.ZBattlefield && o.Counter(m[2]) >= int32(n)
	}
	if strings.EqualFold(q, "Spell.nonColorless") {
		return o.Zone == state.ZStack && o.Face() != nil && effects.ColorsOf(o) != ""
	}
	// "Each color" means any source with at least one colour. It is not
	// protection from everything: colourless sources, including artifacts,
	// remain valid sources under CR 702.16c.
	if strings.EqualFold(q, "each color") {
		return e.objColors(o) != ""
	}
	// MonoColor and EnemyColor are Forge's colour-class predicates, rather
	// than type predicates. They occur on Guardian/Frenemy of the Guildpact;
	// keep them here with the other source-quality tests so generic
	// kw:Protection registration covers every live K:Protection form.
	switch strings.ToLower(q) {
	case "card.monocolor":
		return isMonoColor(e.objColors(o))
	case "card.enemycolor":
		return hasEnemyColorPair(e.objColors(o))
	}
	// Parameterised protection qualities are Forge object specs (Artifact,
	// Creature.God, Card.MultiColor, and so on). Reuse the filter grammar so
	// every supported type/colour predicate has identical meaning here.
	if e.matchesSpec(q, source, e.specCtx(0, e.controllerOf(source))) {
		return true
	}
	if c := protecColourLetter(q); c != 0 {
		col := e.objColors(e.G.Obj(source))
		return col != "" && strings.ContainsRune(col, c)
	}
	if o.Face() == nil {
		return false
	}
	f := o.Face()
	switch strings.ToLower(q) {
	case "artifacts":
		return f.IsArtifact()
	case "creatures":
		// CR 702.114e: a bestowed-attached card is an Aura, not a creature.
		// CR 702.150c: an attached Reconfigure card is not a creature either.
		return o.EffectiveIsCreature() && !o.BestowedAttached() && !o.ReconfiguredAttached()
	case "enchantments":
		return f.IsEnchantment()
	case "instants":
		return f.IsInstant()
	case "sorceries":
		return f.IsSorcery()
	}
	return false
}

// isMonoColor reports the CR colour-class meaning: exactly one colour, not
// colourless. ColorsOf has already applied Devoid before this point.
var (
	protectionCMC     = regexp.MustCompile(`^Card\.cmc(GE|LE)([0-9]+)$`)
	protectionCounter = regexp.MustCompile(`^Permanent\.counters_GE([0-9]+)_([^_]+)$`)
)

func isMonoColor(colors string) bool { return len(colors) == 1 }

// hasEnemyColorPair reports whether a multicoloured object includes an enemy
// pair. The five enemy pairs are the non-adjacent pairs on the WUBRG colour
// wheel; a three- or five-colour object matches when it contains any one of
// them, as "enemy-colored multicolored" requires.
func hasEnemyColorPair(colors string) bool {
	for _, pair := range [...]string{"WB", "WR", "UR", "UG", "BG"} {
		if strings.ContainsRune(colors, rune(pair[0])) && strings.ContainsRune(colors, rune(pair[1])) {
			return true
		}
	}
	return false
}

// protecColourLetter maps a colour quality word to its WUBRG letter, 0
// when q is not a colour word.
func protecColourLetter(q string) rune {
	if v, ok := protecColourLetterTab1.Get(strings.ToLower(q)); ok {
		return v
	}
	return 0
}

// cantPreventDamage implements stat:CantPreventDamage. It intentionally
// scans both ordinary battlefield statics and an EffectZone$ Stack source,
// since Banefire's self-static exists while its spell is resolving; the
// source/Combat gates are evaluated against the damage currently in flight.
func (e *Engine) cantPreventDamage(damageSource, target state.ObjID) bool {
	// Effect-created restrictions (Skullcrack, Call In a Professional, and
	// the rest of the StaticAbilities$ family) live in the same registry as
	// CantTarget/CantRegenerate. A restriction with no Affected$ is global;
	// an IsRemembered restriction applies only to the captured target.
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantPreventDamage" {
			continue
		}
		spec := ce.RestrictParam(cards.PKAffected)
		if spec == "" {
			return true
		}
		if strings.Contains(spec, "IsRemembered") {
			for _, id := range ce.Remembered {
				if id == target {
					return true
				}
			}
		}
	}

	forbidden := false
	e.forEachObject(func(id state.ObjID) {
		if forbidden {
			return
		}
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			return
		}
		for _, st := range o.Face().Statics {
			if st.Mode != "CantPreventDamage" ||
				(o.Zone != state.ZBattlefield && !(st.ParamStr(cards.PKEffectZone) == "Stack" && o.Zone == state.ZStack)) {
				continue
			}
			if v := st.ParamStr(cards.PKValidSource); v != "" &&
				(damageSource == 0 || !e.matchesSpecFrom(v, damageSource, o.Controller, id)) {
				continue
			}
			if combat := st.ParamStr(cards.PKIsCombat); (strings.EqualFold(combat, "True") && !e.combatDamaging) ||
				(strings.EqualFold(combat, "False") && e.combatDamaging) {
				continue
			}
			if !e.replacementConditionHolds(cards.Repl{Params: st.Params}, id, o.Controller) {
				continue
			}
			forbidden = true
			return
		}
	})
	return forbidden
}

// kw:Protection covers Forge's parameterised K:Protection:<Spec> spelling,
// including every parameterised quality printed on a K:Protection line in
// the corpus. The older colour-specific registrations remain for Forge's
// separate natural-language "Protection from <colour>" keyword spelling.
func init() {
	effects.RegisterNonAPI("kw:Protection", "kw:Protection from white", "kw:Protection from blue",
		"kw:Protection from black", "kw:Protection from red", "kw:Protection from green",
		// Progenitus's printed "Protection from everything": sourceHasQuality
		// already answers the "everything" quality true for any source.
		"kw:Protection from everything",
		"stat:CantPreventDamage")
	// kw:Hexproof is the plain keyword and every parameterised K:Hexproof:<Spec>
	// form (KeywordHead collapses them onto the same head); enforcement lives
	// in hexproofBlocksTarget, which reads the quality the same way
	// protectedFrom does.
	effects.RegisterNonAPI("kw:Hexproof")
}

// permanentCastThisTurn reports whether the permanent id became a permanent by
// resolving as a spell cast this turn (Forge's Permanent.ThisTurnCast, CR
// 601.2). EnteredThisTurn is not enough: a token, a reanimated card or a
// flickered permanent also entered this turn without being cast. The answer
// is derived from the event log, like spellsCastThisTurn, so a replay agrees:
// walking back to the last TurnChange, the object's most recent battlefield
// entry must be its stack->battlefield resolution, and the most recent zone
// event before that must be the PutOnStack a cast emits. A copy of a spell
// (StackCopy) or an ability never emits a PutOnStack for its object, so it
// does not qualify.
func (e *Engine) permanentCastThisTurn(id state.ObjID) bool {
	entered := false
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			return false
		}
		if ev.Obj != id {
			continue
		}
		switch ev.Kind {
		case events.PutOnStack:
			return entered
		case events.MoveZone, events.Draw:
			if entered {
				// Some other zone move sits between the resolution and the
				// cast: the object reached the stack without being cast.
				return false
			}
			if ev.To != state.ZBattlefield {
				continue
			}
			if ev.From != state.ZStack {
				return false
			}
			entered = true
		}
	}
	return false
}

var protecColourLetterTab1 = cards.NewStrTable[rune](
	cards.StrEntry[rune]{Key: "white", Val: 'W'},
	cards.StrEntry[rune]{Key: "blue", Val: 'U'},
	cards.StrEntry[rune]{Key: "black", Val: 'B'},
	cards.StrEntry[rune]{Key: "red", Val: 'R'},
	cards.StrEntry[rune]{Key: "green", Val: 'G'},
)
