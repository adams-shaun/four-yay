// Package levelb turns a card's IR into its list of level-B requirements
// (spec docs/superpowers/specs/2026-10-05-compliance-level-b.md section 1).
//
// A requirement is one thing a level-B scenario must exercise: an activated
// ability, a trigger, a static, or an attack/block. The classifier is a pure
// function of the IR -- it runs no game and imports neither rules nor
// compliance/oraclegen -- so the requirement set, and therefore the level-B
// outstanding count, does not move when a template lands; only verdicts do.
package levelb

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// Requirement is one level-B obligation a card carries.
type Requirement struct {
	// Key is "<family>#<face>.<slot>": activate#0.2, trigger#0.1,
	// static#1.0, combat#0.attack.
	Key string
	// Family is one of "activate", "trigger", "static", "combat".
	Family string
	// Face is the index into Card.Faces.
	Face int
	// Slot is the index into the owning Face slice (Abilities for activate,
	// Triggers for trigger, Statics for static) or "attack"/"block" for
	// combat.
	Slot string
	// Sub is the level-B sub-family that names the v1 template serving the
	// requirement, or the ".face:<n>" gap for a face after 0.
	Sub string
	// CoveredByA is true only for a face-0, non-land trigger that the
	// level-A cast-resolve scenario already settles.
	CoveredByA bool
	// Gap is non-empty exactly when no v1 template serves the requirement.
	Gap string
}

// combatKeywords are the keywords whose presence on a creature face gives it
// level-B attack/block requirements (spec section 1).
var combatKeywords = []string{
	"Flying", "Reach", "First Strike", "Double Strike", "Deathtouch",
	"Trample", "Lifelink", "Menace", "Vigilance", "Indestructible",
	"Defender", "Skulk", "Fear", "Intimidate", "Shadow", "Horsemanship",
	"Flanking", "Bushido", "Afflict", "Annihilator", "Battle cry",
	"Melee", "Rampage", "Provoke", "Toxic", "Infect", "Wither",
}

// servableStaticModes maps modes whose supported parameter shape does not
// need additional classification to its v1 template family.
var servableStaticModes = []struct{ mode, sub string }{
	{"DisableTriggers", "static.disable-triggers"},
	{"CombatDamageToughness", "static.combat-damage-toughness"},
	{"Panharmonicon", "static.panharmonicon"},
}

// combatStaticModes are the combat-legality statics whose presence on a face
// gives it level-B attack/block requirements (spec section 1, section 2.3).
var combatStaticModes = []string{
	"CantAttack", "CantBlock", "CantBlockBy", "MustAttack", "MustBlock",
	"MinMaxBlocker", "CanAttackDefender", "CombatDamageToughness",
	"CombatDamageNegatePower",
}

// Requirements returns the level-B requirements for every face of c, in face
// order and then IR order, with combat last. It returns nil for a basic land,
// mirroring isBasicLand at compliance/gate/gate.go:248.
func Requirements(c *cards.Card) []Requirement {
	if c == nil || len(c.Faces) == 0 || isBasicLand(c) {
		return nil
	}
	servable := backFaceServable(c)
	var out []Requirement
	// Non-combat requirements: face order, then IR order within a face.
	for fi, f := range c.Faces {
		for ai, sa := range f.Abilities {
			if !sa.IsActivated() {
				continue
			}
			sub, gap := classifyActivate(sa)
			out = append(out, newReq("activate", fi, strconv.Itoa(ai), sub, gap, false, servable))
		}
		for ti := range f.Triggers {
			sub, gap, covered := classifyTrigger(f, &f.Triggers[ti])
			out = append(out, newReq("trigger", fi, strconv.Itoa(ti), sub, gap, covered, servable || roomDoorServable(c)))
		}
		for si := range f.Statics {
			if AIHintOnlyStatic(f, &f.Statics[si]) {
				// No rules effect to observe: the static only hands its
				// recipient a Forge AI hint (Ordeal of Nylea's
				// HasAttackEffect).
				continue
			}
			sub, gap := classifyStatic(f, &f.Statics[si])
			// A Room's second door is served by casting it (roomDoorServable's
			// trigger rule); a static on that door is served the same way when
			// the static's own sub-family already has a template.
			out = append(out, newReq("static", fi, strconv.Itoa(si), sub, gap, false,
				servable || (gap == "" && RoomStaticServable(c, sub))))
		}
	}
	// Combat requirements last, in face order.
	for fi, f := range c.Faces {
		if !faceHasCombat(f) {
			continue
		}
		out = append(out,
			newReq("combat", fi, "attack", "combat.attack", "", false, servable),
			newReq("combat", fi, "block", "combat.block", "", false, servable),
		)
	}
	return out
}

// backFaceServable reports whether a face after 0 can be set up on the
// battlefield in its active (back) face, so the face-0 templates can serve it.
// Only the two layouts whose back face is a permanent the setup can place
// directly qualify: transforming double-faced cards (DoubleFaced) and the
// Modal hero DFCs. Any other layout with a second face (Adventure, Split,
// Room, ...) keeps the face gap, because its face 1 is not a permanent a
// back-face setup can put on the battlefield.
func backFaceServable(c *cards.Card) bool {
	switch c.AlternateMode {
	case "DoubleFaced", "Modal":
		return true
	}
	return false
}

// newReq builds a Requirement, applying the face-after-0 gap: v1 templates
// serve Faces[0] only, as level A does, so every face > 0 requirement is a
// gap -- unless the card's layout lets setup place the face-1 permanent on the
// battlefield (backFaceServable), in which case the face-0 template runs
// against that face. CoveredByA is always false past face 0: the level-A
// scenario casts the front face only.
func newReq(family string, face int, slot, sub, gap string, covered, servable bool) Requirement {
	r := Requirement{
		Key:        family + "#" + strconv.Itoa(face) + "." + slot,
		Family:     family,
		Face:       face,
		Slot:       slot,
		Sub:        sub,
		CoveredByA: covered,
		Gap:        gap,
	}
	if face > 0 {
		r.CoveredByA = false
		if !servable {
			r.Sub = sub + ".face:" + strconv.Itoa(face)
			r.Gap = "face " + strconv.Itoa(face)
		}
	}
	return r
}

// classifyActivate returns the activated-ability sub-family and its gap.
func classifyActivate(sa *cards.SA) (sub, gap string) {
	zone, _ := sa.Param(cards.PKActivationZone)
	switch {
	case strings.EqualFold(zone, "Hand"):
		return "activate.hand", ""
	case strings.EqualFold(zone, "Graveyard"):
		return "activate.graveyard", ""
	case zone != "" && !strings.EqualFold(zone, "Battlefield"):
		return "activate.zone:" + zone, "activation zone " + zone
	}
	if strings.EqualFold(sa.API, "Mana") {
		return "activate.mana", ""
	}
	return "activate.battlefield", ""
}

// ClassifyTrigger is the exported form of classifyTrigger, for the
// compliance template that classifies a static's GRANTED trigger body (the
// same T:-shaped text a printed T: line has) to pick its cause sub-family.
// It is a pure function of the IR and does not move the requirement set:
// requirements are classified by classifyTrigger alone.
func ClassifyTrigger(f *cards.Face, t *cards.Trigger) (sub, gap string, covered bool) {
	return classifyTrigger(f, t)
}

// classifyTrigger returns the trigger's sub-family, its gap, and whether the
// level-A scenario already covers it.
func classifyTrigger(f *cards.Face, t *cards.Trigger) (sub, gap string, covered bool) {
	gapMode := func() (string, string, bool) {
		return "trigger.gap:" + t.Mode, "trigger mode " + t.Mode, false
	}
	if sub, ok := classifyEventTrigger(t); ok {
		return sub, "", false
	}
	if sub, ok := classifyPhaseOther(t); ok {
		return sub, "", false
	}
	if sub, ok := classifyTapCombatTrigger(f, t); ok {
		return sub, "", false
	}
	if sub, ok := classifyCastTrigger(f, t); ok {
		return sub, "", false
	}
	if sub, ok := classifyStateTrigger(t); ok {
		return sub, "", false
	}
	if sub, ok := classifyManaTrigger(t); ok {
		return sub, "", false
	}
	switch t.ModeKind() {
	case cards.TriggerChangesZone:
		dest := t.ParamStr(cards.PKDestination)
		origin := t.ParamStr(cards.PKOrigin)
		self := namesSelf(t.ParamStr(cards.PKValidCard))
		if strings.EqualFold(dest, "Battlefield") {
			if !self {
				return "trigger.etb-other", "", false
			}
			// A self-ETB trigger on a non-land is the one shape the level-A
			// cast-resolve scenario settles. A land is played, never cast, so
			// its self-ETB gets its own sub-family with a play-land cause
			// (the level-A scenario never puts a spell on the stack for it).
			if f.IsLand() {
				return "trigger.etb-land", "", false
			}
			return "trigger.etb-other", "", true
		}
		if strings.EqualFold(origin, "Battlefield") && strings.EqualFold(dest, "Graveyard") && self {
			return "trigger.dies", "", false
		}
		if selfLeavesBattlefield(t) {
			// classifyZoneChangeTrigger served the stack-using shape; a
			// Static$ cleanup never uses the stack, so it is a named gap.
			return "trigger.gap:ChangesZone", "static trigger", false
		}
		return gapMode()

	case cards.TriggerAttacks:
		// "naming the card or its controller": the card itself, or a
		// creature its controller controls.
		if namesSelf(t.ParamStr(cards.PKValidCard)) || namesYouCtrl(t.ParamStr(cards.PKValidCard)) {
			return "trigger.attacks", "", false
		}
		return gapMode()

	case cards.TriggerAttackersDeclared:
		// "Whenever a player attacks with N or more creatures" is p0's attack
		// too (AttackingPlayer$ Player, no attacked-target filter).
		anyPlayer := strings.EqualFold(t.ParamStr(cards.PKAttackingPlayer), "Player") && !t.HasParam(cards.PKAttackedTarget)
		if strings.EqualFold(t.ParamStr(cards.PKAttackingPlayer), "You") || anyPlayer || namesYouCtrl(t.ParamStr(cards.PKValidAttackers)) {
			return "trigger.attacks", "", false
		}
		return gapMode()

	case cards.TriggerDamageDone:
		if namesSelf(t.ParamStr(cards.PKValidSource)) && strings.EqualFold(t.ParamStr(cards.PKCombatDamage), "True") {
			return "trigger.combat-damage", "", false
		}
		return gapMode()

	case cards.TriggerSpellCast:
		if selfCastTrigger(f, t) {
			return "trigger.spell-cast-self", "", true
		}
		if strings.EqualFold(t.ParamStr(cards.PKValidActivatingPlayer), "You") {
			return "trigger.spell-cast", "", false
		}
		return gapMode()

	case cards.TriggerBecomesTarget:
		if namesSelf(t.ParamStr(cards.PKValidTarget)) || strings.EqualFold(t.ParamStr(cards.PKValidSource), "SpellAbility.OppCtrl") {
			return "trigger.becomes-target", "", false
		}
		// Loki, God of Mischief's "a player or permanent becomes the target of
		// an ability you control" stays a gap: the engine's
		// becomesTargetMatches reads ValidSource$ through the ordinary filter
		// grammar, which has no Ability base (an ability stack object carries
		// no card types), so Ability.YouCtrl fails closed and the trigger can
		// never fire. A recipe cause exists (a probe's targeted {T} ability at
		// p1); serving it first needs the engine-side grammar. Filed as a
		// follow-up ticket.
		return gapMode()

	case cards.TriggerLifeGained:
		if strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "You") {
			return "trigger.life-gained", "", false
		}
		return gapMode()

	case cards.TriggerDrawn:
		vp := strings.ToLower(strings.TrimSpace(t.ParamStr(cards.PKValidPlayer)))
		vc := t.ParamStr(cards.PKValidCard)
		if strings.EqualFold(vp, "you") || namesYouCtrl(vc) {
			return "trigger.drawn", "", false
		}
		// An opponent-draws filter (ValidCard$ Card.OppOwn) or an
		// opponent/player ValidPlayer is served by the other-player cause.
		if vp == "opponent" || vp == "player" || filterHasToken(vc, "OppOwn") || filterHasToken(vc, "OppCtrl") {
			return "trigger.drawn-other", "", false
		}
		if vp == "" && strings.TrimSpace(vc) == "" {
			return "trigger.drawn", "", false
		}
		return gapMode()

	case cards.TriggerPhase:
		// Phase recipes cover p0's own steps and the first opponent step for
		// player-wide triggers, p1's upkeep and draw for opponent-only
		// triggers, and p0's next first main phase. Other explicit player
		// filters remain gaps.
		vp := t.ParamStr(cards.PKValidPlayer)
		switch t.ParamStr(cards.PKPhase) {
		case "Main1":
			if strings.EqualFold(vp, "You") {
				return "trigger.phase", "", false
			}
		case "Upkeep", "Draw":
			if vp == "" || strings.EqualFold(vp, "You") || strings.EqualFold(vp, "Player") || strings.EqualFold(vp, "Opponent") {
				return "trigger.phase", "", false
			}
		case "End of Turn", "BeginCombat":
			if vp == "" || strings.EqualFold(vp, "You") || strings.EqualFold(vp, "Player") {
				return "trigger.phase", "", false
			}
		}
		return gapMode()

	default:
		return gapMode()
	}
}

// classifyStatic returns the static's sub-family and its gap.
func classifyStatic(f *cards.Face, st *cards.Static) (sub, gap string) {
	switch st.ModeKind() {
	case cards.StaticContinuous:
		return "static.continuous", ""
	case cards.StaticReduceCost:
		return "static.cost", ""
	case cards.StaticRaiseCost:
		// Opponent-side taxes use the opponent-cast probe; own additional
		// costs remain served by the source's own cast probe.
		activator := st.ParamStr(cards.PKActivator)
		if strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.Self") && (activator == "" || strings.EqualFold(activator, "You")) {
			return "static.cost", ""
		}
		if strings.EqualFold(activator, "Opponent") || strings.EqualFold(activator, "Player.Opponent") {
			if strings.EqualFold(f.Name, "Aven Interrupter") {
				return "static.cost", "opponent-cast cost static: graveyard/exile cast zone unsupported"
			}
			return "static.cost", ""
		}
		if vc := st.ParamStr(cards.PKValidCard); strings.Contains(vc, "NamedCard") || strings.Contains(vc, "ChosenType") {
			return "static.cost", "opponent-cast cost static: chosen-name/chosen-type recipient unsupported"
		}
		return "static.cost", "opponent-cast cost static"
	}
	for _, s := range servableStaticModes {
		if strings.EqualFold(st.Mode, s.mode) {
			return s.sub, ""
		}
	}
	if TapPowerValueShape(st) {
		return "static.tap-power-value", ""
	}
	if CastWithFlashShape(st) {
		return "static.cast-with-flash", ""
	}
	if UntapOtherPlayerShape(st) {
		return "static.untap-other-player", ""
	}
	if CantDrawShape(st) {
		return "static.cant-draw", ""
	}
	if sub, ok := supportedLegalityStatic(f, st); ok {
		return sub, ""
	}
	if sub, gap := serveStaticMode(f, st); sub != "" || gap != "" {
		return sub, gap
	}
	if sub, ok := gatedLegalityStatic(f, st); ok {
		return sub, ""
	}
	if sub, ok := cantBeShape(f, st); ok {
		return sub, ""
	}
	if optionalCostSelfShape(st) {
		return "static.optional-cost", ""
	}
	if gap := cantBeNamedGap(st); gap != "" {
		return "static.gap:" + st.Mode, gap
	}
	if isCombatStaticMode(st.Mode) {
		return "static.combat", "legality static"
	}
	return "static.gap:" + st.Mode, "static mode " + st.Mode
}

// supportedLegalityStatic recognizes only the legality shapes for which a
// v1 template has a validated control. Other instances remain visible gaps.
func supportedLegalityStatic(f *cards.Face, st *cards.Static) (string, bool) {
	switch strings.ToLower(st.Mode) {
	case "canattackdefender":
		valid := st.ParamStr(cards.PKValidCard)
		if strings.EqualFold(valid, "Creature.YouCtrl") && !st.HasParam(cards.PKCheckSVar) && !st.HasParam(cards.PKIsPresent) && !st.HasParam(cards.PKValidAttacked) && !st.HasParam(cards.PKPhases) && !st.HasParam(cards.PKCondition) {
			return "static.can-attack-defender", true
		}
		if strings.EqualFold(valid, "Card.Self") && strings.EqualFold(st.ParamStr(cards.PKCheckSVar), "X") && !st.HasParam(cards.PKIsPresent) && !st.HasParam(cards.PKPhases) &&
			strings.EqualFold(f.SVars["X"], "Count$YouScryThisTurn/Plus.Y") &&
			strings.EqualFold(f.SVars["Y"], "Count$YouSurveilThisTurn") {
			return "static.can-attack-defender-svar", true
		}
	case "manaconvert":
		if strings.EqualFold(st.ParamStr(cards.PKValidPlayer), "You") &&
			strings.EqualFold(st.ParamStr(cards.PKValidCard), "Creature.YouCtrl") &&
			strings.EqualFold(st.ParamStr(cards.PKValidSA), "Spell") &&
			(strings.EqualFold(st.ParamStr(cards.PKManaConversion), "AnyType->AnyType") || strings.EqualFold(st.ParamStr(cards.PKManaConversion), "AnyType->AnyColor")) &&
			!st.HasParam(cards.PKEffectZone) && !st.HasParam(cards.PKOptional) && !st.HasParam(cards.PKAffectedZone) {
			return "static.mana-convert-creature-spells", true
		}
	case "cantgainlife":
		switch strings.ToLower(st.ParamStr(cards.PKValidPlayer)) {
		case "player", "you", "":
			// Unconditional: only ValidPlayer$ plus the Mode and display
			// parameters. "" is Forge's implicit "every player" (Mornsong
			// Aria, which carries no ValidPlayer$ at all); Secondary$ True (a
			// static that is one rider of a wider Oracle sentence) stays
			// display-only.
			allowed := 0
			if st.HasParam(cards.PKValidPlayer) {
				allowed++
			}
			for _, k := range []cards.ParamKey{cards.PKMode, cards.PKDescription, cards.PKSecondary} {
				if st.HasParam(k) {
					allowed++
				}
			}
			if len(st.Params) == allowed {
				return "static.cant-gain-life", true
			}
		}
	case "cantattack":
		if EnchantedHostLegalityStatic(f, st) {
			return "static.cant-attack-enchanted", true
		}
	case "cantblockby":
		if f.IsCreature() && SelfLegalityStatic(st, cards.PKValidAttacker, cards.PKValidBlocker) && blockerFilterServable(st.ParamStr(cards.PKValidBlocker)) {
			return "static.cant-block-by-blocker-filter", true
		}
		if strings.EqualFold(st.ParamStr(cards.PKValidAttacker), "Creature.YouCtrl+powerLE1,Creature.YouCtrl+toughnessLE1") && !st.HasParam(cards.PKValidBlocker) && !st.HasParam(cards.PKIsPresent) && !st.HasParam(cards.PKCondition) {
			return "static.cant-block-by", true
		}
		// The unfiltered self form ("can't be blocked"); the Tetsuko filter
		// shape above keeps its own sub-family.
		if f.IsCreature() && SelfLegalityStatic(st, cards.PKValidAttacker) {
			return "static.cant-block-by-self", true
		}
	case "cantblock":
		if f.IsCreature() && SelfLegalityStatic(st, cards.PKValidCard) {
			return "static.cant-block-self", true
		}
		if EnchantedHostLegalityStatic(f, st) {
			return "static.cant-block-enchanted", true
		}
	case "minmaxblocker":
		if f.IsCreature() && SelfLegalityStatic(st, cards.PKValidCard, cards.PKMin) && MinBlockers(st) > 0 {
			return "static.min-blockers", true
		}
	case "cantbecast":
		if strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.Self") && strings.EqualFold(st.ParamStr(cards.PKCheckSVar), "X") && strings.EqualFold(st.ParamStr(cards.PKSVarCompare), "LT7") && strings.EqualFold(st.ParamStr(cards.PKEffectZone), "All") && !st.HasParam(cards.PKPhases) && !st.HasParam(cards.PKCondition) && !st.HasParam(cards.PKCaster) {
			return "static.cant-be-cast-threshold", true
		}
		if strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card") && strings.EqualFold(st.ParamStr(cards.PKPhases), "BeginCombat->EndCombat") && !st.HasParam(cards.PKCaster) && !st.HasParam(cards.PKCondition) && !st.HasParam(cards.PKIsPresent) {
			return "static.cant-be-cast-combat", true
		}
	case "cantbeactivated":
		if NamedCardActivationStatic(f, st) {
			return "static.cant-be-activated-named", true
		}
		if NamedCardActivationEntersStatic(f, st) {
			return "static.cant-be-activated-named-enters", true
		}
		if strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card") && strings.EqualFold(st.ParamStr(cards.PKValidSA), "Activated.!ManaAbility") && strings.EqualFold(st.ParamStr(cards.PKPhases), "BeginCombat->EndCombat") && !st.HasParam(cards.PKActivator) && !st.HasParam(cards.PKAffectedZone) && !st.HasParam(cards.PKCondition) {
			return "static.cant-be-activated-combat", true
		}
	}
	return "", false
}

// SelfLegalityStatic reports whether st is a combat-legality static that
// applies to its own host only and carries no condition: key (the param that
// scopes the static) names the host ("Card.Self" or "Creature.Self"), every
// other parameter in extra is a value the caller reads, and the only other
// parameters present are the Mode and display ones. A static with an IsPresent,
// Condition, CheckSVar, UnlessDefender, ValidBlocker, Phases ... parameter is
// conditional or filtered and is not this shape.
func SelfLegalityStatic(st *cards.Static, key cards.ParamKey, extra ...cards.ParamKey) bool {
	scope := st.ParamStr(key)
	if !strings.EqualFold(scope, "Card.Self") && !strings.EqualFold(scope, "Creature.Self") {
		return false
	}
	allowed := 1
	for _, k := range extra {
		if !st.HasParam(k) {
			return false
		}
		allowed++
	}
	for _, k := range []cards.ParamKey{cards.PKMode, cards.PKDescription, cards.PKSecondary} {
		if st.HasParam(k) {
			allowed++
		}
	}
	return len(st.Params) == allowed
}

// EnchantedHostLegalityStatic reports whether st is an Aura's unconditional
// CantAttack/CantBlock on the creature it enchants (Pacifism's "Enchanted
// creature can't attack or block"): ValidCard$ names the enchanted host and
// no other parameter beyond the Mode and display ones scopes it.
func EnchantedHostLegalityStatic(f *cards.Face, st *cards.Static) bool {
	if f.IsCreature() || !faceHasSubtype(f, "Aura") {
		return false
	}
	switch strings.ToLower(st.ParamStr(cards.PKValidCard)) {
	case "creature.enchantedby", "creature.attachedby", "card.enchantedby", "card.attachedby":
	default:
		return false
	}
	allowed := 1
	for _, k := range []cards.ParamKey{cards.PKMode, cards.PKDescription, cards.PKSecondary} {
		if st.HasParam(k) {
			allowed++
		}
	}
	return len(st.Params) == allowed
}

func faceHasSubtype(f *cards.Face, t string) bool {
	for _, x := range f.Types {
		if strings.EqualFold(x, t) {
			return true
		}
	}
	return false
}

// NamedCardActivationStatic reports whether st is Sorcerous Spyglass's
// shape: "Activated abilities of sources with the chosen name can't be
// activated unless they're mana abilities" on a castable nonland permanent
// whose as-enters replacement names the card (NameCard).
func NamedCardActivationStatic(f *cards.Face, st *cards.Static) bool {
	if f.IsLand() || f.ManaCost == "" || !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.NamedCard") ||
		!strings.EqualFold(st.ParamStr(cards.PKValidSA), "Activated.!ManaAbility") {
		return false
	}
	allowed := 2
	for _, k := range []cards.ParamKey{cards.PKMode, cards.PKDescription, cards.PKSecondary} {
		if st.HasParam(k) {
			allowed++
		}
	}
	if len(st.Params) != allowed {
		return false
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "DB$ NameCard") {
			return true
		}
	}
	return false
}

// NamedCardActivationEntersStatic reports whether st is Petrified Hamlet's
// shape: the same chosen-name lock (CantBeActivated ValidCard$ Card.NamedCard
// on non-mana activations) on a land whose ETB trigger poses the NameCard ask
// while the trigger resolves ("When this land enters, choose a land card
// name"), not through an as-enters replacement. The name is chosen
// mid-trigger-resolution, so the observation plays the land and answers the
// ask with the library top (static_named_enters.go).
func NamedCardActivationEntersStatic(f *cards.Face, st *cards.Static) bool {
	if !f.IsLand() || !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.NamedCard") ||
		!strings.EqualFold(st.ParamStr(cards.PKValidSA), "Activated.!ManaAbility") {
		return false
	}
	allowed := 2
	for _, k := range []cards.ParamKey{cards.PKMode, cards.PKDescription, cards.PKSecondary} {
		if st.HasParam(k) {
			allowed++
		}
	}
	if len(st.Params) != allowed {
		return false
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "DB$ NameCard") {
			return true
		}
	}
	return false
}

// aiHintSVars are Forge AI-only SVar names: hints the AI reads to choose
// attacks, equips, sacrifices and targets. They have no rules meaning.
var aiHintSVars = map[string]bool{
	"hasattackeffect": true, "hasblockeffect": true, "aitapdown": true,
	"sacme": true, "equipme": true, "enchantme": true, "destroywhendamaged": true,
}

// AIHintOnlyStatic reports whether st is a Continuous static whose only
// effect is AddSVar$/AddSVars$ of Forge AI hints ("SVar:AE:SVar:
// HasAttackEffect:TRUE"): it changes nothing either rules engine models, so
// it gives the card no level-B requirement. Any other effect parameter, or
// an added SVar that is not a known AI hint, keeps the requirement.
func AIHintOnlyStatic(f *cards.Face, st *cards.Static) bool {
	if st.ModeKind() != cards.StaticContinuous {
		return false
	}
	var names []string
	for k, v := range st.Params {
		switch k {
		case "Mode", "Description", "Secondary", "Affected", "AffectedDefined", "AffectedZone", "EffectZone":
		case "AddSVar", "AddSVars":
			for _, n := range strings.FieldsFunc(v, func(r rune) bool { return r == '&' || r == ',' }) {
				names = append(names, strings.TrimSpace(n))
			}
		default:
			return false
		}
	}
	if len(names) == 0 {
		return false
	}
	for _, n := range names {
		body := strings.TrimSpace(f.SVars[n])
		rest, ok := strings.CutPrefix(body, "SVar:")
		if !ok {
			return false
		}
		hint, _, _ := strings.Cut(rest, ":")
		if !aiHintSVars[strings.ToLower(hint)] {
			return false
		}
	}
	return true
}

// blockerFilterServable reports whether a CantBlockBy ValidBlocker$ filter is
// one creature filter the blocker-filter template can field a matching and a
// non-matching probe for: a single "Creature.<qualifiers>" alternative with
// no controller or zone qualifier.
func blockerFilterServable(filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" || strings.ContainsAny(filter, ",$ ") || !strings.HasPrefix(filter, "Creature.") {
		return false
	}
	for _, q := range []string{"YouCtrl", "OppCtrl", "Other", "Self", "Remembered", "Chosen", "Named"} {
		if strings.Contains(filter, q) {
			return false
		}
	}
	return true
}

// MinBlockers is the Min$ of a MinMaxBlocker static ("can't be blocked except
// by N or more creatures"), or 0 when it is not a plain integer bound the
// level-B template can observe (2 to 6; "All" and larger bounds are not).
func MinBlockers(st *cards.Static) int {
	n, err := strconv.Atoi(strings.TrimSpace(st.ParamStr(cards.PKMin)))
	if err != nil || n < 2 || n > 6 {
		return 0
	}
	return n
}

// isCombatStaticMode reports whether mode names a combat-legality static.
func isCombatStaticMode(mode string) bool {
	for _, m := range combatStaticModes {
		if strings.EqualFold(mode, m) {
			return true
		}
	}
	return false
}

// faceHasCombat reports whether f is a creature face carrying a combat
// keyword or a combat-legality static.
func faceHasCombat(f *cards.Face) bool {
	if !f.IsCreature() {
		return false
	}
	for _, k := range combatKeywords {
		if f.HasKeyword(k) {
			return true
		}
	}
	for i := range f.Statics {
		if isCombatStaticMode(f.Statics[i].Mode) {
			return true
		}
	}
	return false
}

// isBasicLand mirrors compliance/gate/gate.go:248.
func isBasicLand(c *cards.Card) bool {
	if len(c.Faces) == 0 {
		return false
	}
	basic, land := false, false
	for _, t := range c.Faces[0].Types {
		basic = basic || strings.EqualFold(t, "Basic")
		land = land || strings.EqualFold(t, "Land")
	}
	return basic && land
}

// filterHasToken reports whether a card filter carries want as a token in any
// comma-separated alternative. A Forge filter is "<class>.<qualifier>[+<qualifier>]"
// per alternative, so a token is split on both "." and "+" (the qualifier
// separator): `Card.Self+kicked` carries tokens Card, Self and kicked.
func filterHasToken(filter, want string) bool {
	for _, alt := range strings.Split(filter, ",") {
		for _, tok := range strings.FieldsFunc(alt, func(r rune) bool { return r == '.' || r == '+' }) {
			if strings.EqualFold(strings.TrimSpace(tok), want) {
				return true
			}
		}
	}
	return false
}

// namesSelf reports whether a card filter selects the source card itself, e.g.
// `Card.Self` or `Card.Self+wasCastByYou`.
func namesSelf(filter string) bool { return filterHasToken(filter, "Self") }

// namesYouCtrl reports whether a card filter selects a permanent you control,
// e.g. `Creature.YouCtrl` or `Creature.Other+YouCtrl`.
func namesYouCtrl(filter string) bool { return filterHasToken(filter, "YouCtrl") }
