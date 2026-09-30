// Event folds for delayed trigger/ability registration, grants and pushes.
//
// Split out of events/apply.go: code moved verbatim, no behaviour
// change. Each fold function is the body of the matching case in
// Apply (g, e) switch; see apply.go for the dispatch.
package events

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// foldDelayedRegister folds Kind DelayedRegister into state.
func foldDelayedRegister(g *state.Game, e Event) {
	// Ruling dt1-a: the registration is game state folded here, so a
	// log-only replay rebuilds the same set a live game held. Totality
	// like every case: an invalid controller, a nonexistent source, or a
	// non-canonical Step (at the very least a zero Step is untap, which
	// every DelayedRegister this build emits carries a real phase for)
	// degrades to a no-op rather than panicking.
	if !validPlayer(g, e.Player) {
		return
	}
	if g.Obj(e.Obj) == nil || !e.Step.Valid() {
		return
	}
	src := g.Obj(e.Obj)
	// Dash and Warp refer to the exact permanent that received their
	// keyword promise, and so does the AtEOT$ end-of-turn rider family
	// (effects/atEOTBody reuses the warp body for Exile and mints its
	// own __kwAtEOTDestroy for Destroy) and the MayFlashSac cleanup
	// sacrifice (rules/mayflashsac.go's __kwMayFlashSacrifice): a copy
	// that left the battlefield and returned as a new incarnation is NOT
	// acted on by the stale promise (CR 400.7). Encore's grouped delayed
	// trigger does not: CR 603.7 leaves it independent of the card that
	// created it, and it must sacrifice its remembered token group even
	// if that card later changes zones and returns as a new incarnation.
	track := strings.HasPrefix(e.Counter, "__kwDash") ||
		strings.HasPrefix(e.Counter, "__kwWarp") ||
		strings.HasPrefix(e.Counter, "__kwUnearth") ||
		strings.HasPrefix(e.Counter, "__kwAtEOT") ||
		strings.HasPrefix(e.Counter, "__kwMayFlashSac")
	// Event-matched (non-phase) registrations encode
	// "<Mode$ value>:<trigger SVar name>" in Text. The DelayedRegister
	// event gains no field of its own (Ruling T20-a's field-reuse
	// precedent); a Mode$ Phase registration's Text is the Forge Phase$
	// string, which never contains a colon, and the decode only splits on
	// the modes rules.registerOpeningEffectTriggers emits, so every
	// already-logged registration decodes as a phase one. An inline
	// body (effDelayedTrigger's Mode$ SpellCast branch, where the
	// DelayedTrigger SA is a face Ability with no SVar name of its own)
	// stores the RAW trigger parameters instead of a name: the body
	// starts "Mode$", which no SVar name does, so fire time can tell the
	// two carriers apart.
	//
	// The tail suffixes parse off a working copy so the stored body and
	// the phase string never carry them: ValidPlayer$ rides
	// "|VP=<value>" and a ThisTurn$ True registration's expiry turn
	// rides "|TT=<turn>" (state.DelayedTrigger.MaxTurn; zero when
	// absent). The phase registrations' Texts ("Upkeep",
	// "End of Turn|VP=You") contain neither spelling inside the phase
	// name, so every already-logged registration decodes exactly as
	// before (VP ungated; MaxTurn zero).
	text := e.Text
	effectRepeat := strings.HasSuffix(text, "|EF")
	if effectRepeat {
		text = strings.TrimSuffix(text, "|EF")
	}
	// OptionalDecider$ (an api:Effect Triggers$ body's "you may"
	// election) rides "|OD=<spec>" the same way ValidPlayer$ and MaxTurn
	// do. It is stripped before the mode/Trigger split below so the
	// suffix cannot reach the stored body name; the value never contains
	// "|", so a single LastIndex is exact.
	// Phase registrations append VP after OD, while event registrations
	// have OD at the tail. Strip the phase suffix first so an optional
	// spec cannot accidentally swallow its player gate.
	// A Phase registration's IsPresent$/PresentZone$/PresentCompare$
	// condition rides "|IP=<spec>", "|PZ=<zone>", "|PC=<compare>" after
	// every other suffix effDelayedTrigger writes
	// ("<Phase>|VP=<value>|IP=<spec>|PZ=<zone>|PC=<compare>"), so they
	// are stripped in reverse append order, BEFORE the VP gate: a
	// LastIndex on an earlier marker would otherwise swallow the later
	// ones into its value (Bank Job and Grinning Totem carry ValidPlayer$
	// YOU together with IsPresent$, so VP-before-IP is the ordinary shape,
	// not an edge). The values are Forge filter/zone/compare tokens with
	// no "|", so each strip is exact; a registration logged before this
	// slot existed carries none of the spelling and decodes with all
	// three empty, exactly as before.
	presentCompare := ""
	if i := strings.LastIndex(text, "|PC="); i >= 0 {
		presentCompare = text[i+4:]
		text = text[:i]
	}
	presentZone := ""
	if i := strings.LastIndex(text, "|PZ="); i >= 0 {
		presentZone = text[i+4:]
		text = text[:i]
	}
	presentSpec := ""
	if i := strings.LastIndex(text, "|IP="); i >= 0 {
		presentSpec = text[i+4:]
		text = text[:i]
	}
	vp := ""
	if i := strings.LastIndex(text, "|VP="); i >= 0 {
		vp = text[i+4:]
		text = text[:i]
	}
	optionalSpec := ""
	if i := strings.LastIndex(text, "|OD="); i >= 0 {
		optionalSpec = text[i+4:]
		text = text[:i]
	}
	// Effect lifetime suffixes are stripped before the mode/body split.
	// Their values are Forge tokens without a pipe delimiter. |SB is a
	// bare flag appended LAST by the registering arm, so it is stripped
	// FIRST (before the value-bearing suffixes and |IH).
	sourceBattlefield := strings.HasSuffix(text, "|SB")
	if sourceBattlefield {
		text = strings.TrimSuffix(text, "|SB")
	}
	imprint := strings.HasSuffix(text, "|IH")
	if imprint {
		text = strings.TrimSuffix(text, "|IH")
	}
	strip := func(key string) string {
		if i := strings.LastIndex(text, key); i >= 0 {
			value := text[i+len(key):]
			text = text[:i]
			return value
		}
		return ""
	}
	cast := strip("|FC=")
	counter := strip("|FK=")
	exile := strip("|XM=")
	forget := strip("|FM=")
	duration := strip("|DU=")
	maxTurn := int32(0)
	if i := strings.LastIndex(text, "|TT="); i >= 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(text[i+4:])); err == nil && n > 0 {
			maxTurn = int32(n)
		}
		text = text[:i]
	}
	mode, trigger := "", ""
	if i := strings.Index(text, ":"); i > 0 &&
		(effectRepeat || text[:i] == "SpellCast" || text[:i] == "ChangesZone" ||
			text[:i] == "ChangesController" || text[:i] == "DamageDone" ||
			text[:i] == "AttackersDeclared" || text[:i] == "BecomeMonarch") {
		mode, trigger = text[:i], text[i+1:]
	}
	g.Delayed = append(g.Delayed, state.DelayedTrigger{
		ID:                g.DelayedNext,
		Phase:             e.Step,
		Source:            e.Obj,
		Controller:        e.Player,
		Execute:           e.Counter,
		Remembered:        rememberedFrom(e.IDs),
		MinTurn:           e.Amount,
		MaxTurn:           maxTurn,
		SourceIncarnation: src.Incarnation,
		TrackSource:       track,
		EventMode:         mode,
		Trigger:           trigger,
		EffectRepeat:      effectRepeat,
		ValidPlayer:       vp,
		PresentSpec:       presentSpec,
		PresentZone:       presentZone,
		PresentCompare:    presentCompare,
		OptionalSpec:      optionalSpec,
		EffectDuration:    duration,
		BirthTurn:         g.Turn,
		ForgetOnMoved:     forget,
		ExileOnMoved:      exile,
		ForgetCounter:     counter,
		ForgetOnCast:      cast,
		ImprintOnHost:     imprint,
		SourceBattlefield: sourceBattlefield,
	})
	g.DelayedNext++
}

// foldDelayedForget folds Kind DelayedForget into state.
func foldDelayedForget(g *state.Game, e Event) {
	for i := range g.Delayed {
		if g.Delayed[i].ID != uint32(e.Amount) {
			continue
		}
		for j, target := range g.Delayed[i].Remembered {
			if target.Obj == e.Obj {
				g.Delayed[i].Remembered = append(g.Delayed[i].Remembered[:j:j], g.Delayed[i].Remembered[j+1:]...)
				break
			}
		}
		break
	}
}

// foldDelayedRemove folds Kind DelayedRemove into state.
func foldDelayedRemove(g *state.Game, e Event) {
	for i := range g.Delayed {
		if g.Delayed[i].ID == uint32(e.Amount) {
			g.Delayed = append(g.Delayed[:i], g.Delayed[i+1:]...)
			break
		}
	}
}

// foldDelayedPush folds Kind DelayedPush into state.
func foldDelayedPush(g *state.Game, e Event) {
	// Ruling dt1-a: the ability object is minted here, inside Apply, so a
	// log-only replay creates the same object a live game did (the
	// Ruling T20-a precedent TriggerPush and AbilityPush already set).
	// Unlike those two, the Ability is not a face Triggers index -- a
	// delayed trigger's Effect is an SVar-named sub-ability on the
	// source's face -- so it is resolved here from e.Counter (the
	// Execute$ name) via cards.ResolveSVar. Firing is one-shot: the
	// matching registration is removed, which is what keeps a delayed
	// trigger from firing every turn.
	if !validPlayer(g, e.Player) {
		return
	}
	// CR 724.2a's monarch draw is the engine's OWN trigger, minted from
	// a synthetic body with no card registration at all: it must never
	// consume one. Its event carries Amount zero (no DelayedRegister
	// ever set it), which would otherwise match registration ID 0 and
	// delete a bystander's pending delayed trigger.
	monarchDraw := e.Counter == "__monarch_draw"
	radiationDrain := e.Counter == "__radiation_drain"
	speedIncrease := e.Counter == "__speed_increase"
	initiativeVenture := e.Counter == "__initiative_venture"
	// Consume the registration first, even when its tracked permanent has
	// changed incarnation. A stale dash/warp promise expires once; it must
	// neither act on the returned object nor be retried forever. Ordinary
	// delayed triggers, including Encore's group cleanup, are independent
	// of their source and still resolve.
	var registration *state.DelayedTrigger
	if !monarchDraw && !radiationDrain && !speedIncrease && !initiativeVenture {
		for i := range g.Delayed {
			if g.Delayed[i].ID == uint32(e.Amount) {
				dt := g.Delayed[i]
				registration = &dt
				if !dt.EffectRepeat {
					g.Delayed = append(g.Delayed[:i], g.Delayed[i+1:]...)
				}
				break
			}
		}
	}
	src := g.Obj(e.Obj)
	if !radiationDrain && !speedIncrease && !initiativeVenture {
		if src == nil {
			return
		}
		if registration != nil && registration.TrackSource &&
			src.Incarnation != registration.SourceIncarnation {
			return
		}
		if src.Face() == nil {
			return
		}
	}
	var sa *cards.SA
	if monarchDraw {
		sa = &cards.SA{Kind: "DB", API: "Draw", Params: map[string]string{"Defined": "You", "NumCards": "1"}}
	} else if radiationDrain {
		sa = &cards.SA{Kind: "DB", API: "RadiationDrain", Params: map[string]string{"Defined": "You"}}
	} else if speedIncrease {
		sa = &cards.SA{Kind: "DB", API: "SpeedIncrease"}
	} else if initiativeVenture {
		// CR 726.2: the inherent "whenever a player takes the initiative,
		// that player ventures into Undercity" ability (the combat-damage
		// path queues this). The body is the ordinary Venture primitive
		// narrowed to the Undercity quality (CR 726.2 / 701.49d), resolved
		// by effects/venture.go's effVenture exactly as a printed DB$
		// Venture | Dungeon$ Undercity would be.
		sa = &cards.SA{Kind: "DB", API: "Venture", Params: map[string]string{"Dungeon": "Undercity"}}
	} else {
		sa = ResolveSVarAcrossFaces(src, e.Counter)
	}
	if sa == nil {
		return
	}
	if e.Text == "static" {
		// Forge's static delayed trigger (TriggerHandler's isStatic arm):
		// the Execute body resolved IMMEDIATELY at fire time — rules ran
		// it inline — so the one-shot registration is consumed and no
		// ability object is minted. Every earlier DelayedPush carries no
		// Text, so already-logged firings mint exactly as before.
		return
	}
	if e.Text == "granted ability" {
		// A SELF-granted activation (rules' shared activation flow mints
		// it through this delayed shape): count it on the per-source
		// activation census exactly as GrantAbilityPush counts a
		// cross-object grant.
		countActivation(g, e.Obj)
	}
	// StackCopy's discipline: snapshot every src field the post-mint
	// code reads (Incarnation here) before AddObject may reallocate
	// g.Objs and orphan the src pointer.
	var incarnation uint32
	if src != nil {
		incarnation = src.Incarnation
	}
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = sa
	o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
	if !radiationDrain && !speedIncrease && !initiativeVenture {
		o.Source = e.Obj
	}
	if registration != nil && registration.TrackSource {
		o.SourceIncarnation = incarnation
	}
	o.Remembered = rememberedFrom(e.IDs)
}

// foldGrantTriggerPush folds Kind GrantTriggerPush into state.
func foldGrantTriggerPush(g *state.Game, e Event) {
	// A static-grant's trigger (AddTrigger$ on a Mode$ Continuous static):
	// the Ruling T20-a/DelayedPush precedent -- the ability object is
	// minted here, inside Apply, so a log-only replay creates the same
	// object a live game did. Like DelayedPush the Ability is not a face
	// Triggers index: it is the granted trigger's Execute$ SVar-named
	// body. The body lives on the GRANTOR's face (Forge defines the
	// AddTrigger$-named SVar on the card carrying the static), while Obj
	// is the AFFECTED recipient -- the two differ for a cross-object
	// grant (an Aura granting its enchanted creature a trigger). The
	// grantor rides Amount (0 = the historical self-grant shape, where
	// grantor == recipient and the AFFECTED table is the right one):
	// when set, the name resolves from the grantor's table -- the exact
	// table rules' queue gate linked the body from -- else from the
	// affected object's own table (the self-grant path, byte-identical
	// for every already-logged event). A grantor that has left the
	// battlefield, or whose face no longer resolves the name, mints
	// nothing (the totality stance every SVar resolution takes).
	// o.Source stays e.Obj: a granted body's `Defined$ Self`/`CARDNAME`
	// names the recipient. No registration is consumed: a granted
	// trigger is fired by nothing and lives exactly as long as its
	// granting static.
	if !validPlayer(g, e.Player) {
		return
	}
	src := g.Obj(e.Obj)
	if src == nil || src.Face() == nil {
		return
	}
	resolver := src
	if e.Amount > 0 {
		if grantor := g.Obj(state.ObjID(e.Amount)); grantor != nil && grantor.Face() != nil {
			resolver = grantor
		} else {
			return
		}
	}
	sa := ResolveSVarAcrossFaces(resolver, e.Counter)
	if sa == nil {
		return
	}
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = sa
	o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
	o.Source = e.Obj
	o.Remembered = rememberedFrom(e.IDs)
}

// foldGrantAbilityPush folds Kind GrantAbilityPush into state.
func foldGrantAbilityPush(g *state.Game, e Event) {
	// A cross-object ability grant (CR 613.1f): the granting static's
	// SOURCE resolves the SVar body (Counter), while the minted ability
	// object's Source is the RECIPIENT (Obj). The DelayedPush/
	// GrantTriggerPush precedent -- mint inside Apply so a log-only
	// replay creates the identical object. IDs[0] is the granting
	// object, carried here rather than on Obj because Obj must stay the
	// recipient; it is NOT decoded into Remembered (the ability's
	// Remembered set is unrelated to who granted it). A grantor that
	// has left the battlefield, or whose face no longer resolves the
	// name, mints nothing (the totality stance every SVar resolution
	// takes). No registration is consumed: unlike a delayed trigger a
	// grant lives exactly as long as its granting static, and rules
	// re-derives the offer each priority window.
	if !validPlayer(g, e.Player) {
		return
	}
	if len(e.IDs) == 0 {
		return
	}
	grantor := g.Obj(e.IDs[0])
	if grantor == nil || grantor.Face() == nil {
		return
	}
	if g.Obj(e.Obj) == nil {
		return
	}
	sa := ResolveSVarAcrossFaces(grantor, e.Counter)
	if sa == nil {
		return
	}
	// AbilityPush's per-source activation census (ActivatedThisTurn),
	// same battlefield condition and the same before-AddObject
	// discipline: a GRANTED activation is an activation of the
	// recipient, and leaving it uncounted let a free granted ability
	// escape the bot's repeatability budget forever.
	countActivation(g, e.Obj)
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = sa
	o.StackKind, o.StackKindKnown = state.StackKindActivated, true
	o.Source = e.Obj
}

// foldGainedAbilityPush folds Kind GainedAbilityPush into state.
func foldGainedAbilityPush(g *state.Game, e Event) {
	// A has-all-abilities-of activated ability (Forge's GainsAbilitiesOf$,
	// task gains1): like AbilityPush the object is minted inside Apply so a
	// log-only replay creates the same object a live game did, but the
	// ability is a compiled SA on a FOREIGN card's face rather than an
	// index of the recipient's own. Obj is the recipient (o.Source, so
	// `Defined$ Self`/`CARDNAME` in the body names it), IDs[0] the foreign
	// card, Amount the index of the ability in that face's Abilities. The
	// compiled pointer is what the activation-limit census and the
	// owning-face SVar reads recover, so it must be the face's own SA --
	// never a fresh parse. A missing IDs[0], a foreign card that left the
	// scoped zone, or a stale index mints nothing (the totality stance
	// every case here takes).
	if !validPlayer(g, e.Player) {
		return
	}
	if len(e.IDs) == 0 {
		return
	}
	if g.Obj(e.Obj) == nil {
		return
	}
	foreign := g.Obj(e.IDs[0])
	if foreign == nil || foreign.Face() == nil {
		return
	}
	abilities := foreign.Face().Abilities
	if e.Amount < 0 || int(e.Amount) >= len(abilities) {
		return
	}
	sa := abilities[int(e.Amount)]
	if sa == nil {
		return
	}
	// The per-source activation census, as AbilityPush and
	// GrantAbilityPush count it: Myr Welder activating an imprinted
	// Knowledge Vault's "{0}: Sacrifice" was never counted, so the bot's
	// repeatability budget never closed and it re-activated forever.
	countActivation(g, e.Obj)
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = sa
	o.StackKind, o.StackKindKnown = state.StackKindActivated, true
	o.Source = e.Obj
	// The wrapper carries its foreign-face provenance on itself (r3):
	// the granting static can END between this push and the resolution --
	// the foreign card leaves the scoped zone, the static's named set
	// re-derives -- and the live-grant recovery scans then find no owner.
	// Setting it here (never in rules/) is what makes a log-only replay
	// reproduce the exact face the live mint resolved.
	o.GainedFace = foreign.Face()
	o.GainedFrom = foreign.ID
}

// foldGainedTriggerPush folds Kind GainedTriggerPush into state.
func foldGainedTriggerPush(g *state.Game, e Event) {
	// A has-all-abilities-of triggered ability (Forge's GainsTriggerAbsOf$,
	// task gains1): the GainedAbilityPush shape one level over, the
	// MergedTriggerPush precedent's Apply-time mint. Obj is the recipient
	// (o.Source), IDs[0] the foreign card, Amount the index of the
	// trigger in that face's Triggers, and Counter the Execute$ name
	// carried as readable provenance and checked against the trigger line
	// here -- so a truncated or tampered log mints nothing rather than the
	// wrong ability. The face's own compiled Trigger.Effect pointer is
	// minted, never a by-name parse, for the same reason MergedTriggerPush
	// mints the compiled pointer: every consumer that recovers a resolving
	// ability's owning trigger (the OptionalDecider$ gate, the
	// intervening-if recheck, the label, the SVar table) does so by
	// pointer identity.
	if !validPlayer(g, e.Player) {
		return
	}
	if len(e.IDs) == 0 {
		return
	}
	if g.Obj(e.Obj) == nil {
		return
	}
	foreign := g.Obj(e.IDs[0])
	if foreign == nil || foreign.Face() == nil {
		return
	}
	triggers := foreign.Face().Triggers
	if e.Amount < 0 || int(e.Amount) >= len(triggers) {
		return
	}
	// The Trigger is a value type on the face; take a stable pointer to
	// the element rather than copying (the compiled pointer identity the
	// consumers rely on). A face's Triggers slice never resizes after
	// parse, so the pointer stays valid for the match.
	tr := &triggers[int(e.Amount)]
	if tr.Effect == nil || tr.Params["Execute"] != e.Counter {
		return
	}
	sa := tr.Effect
	o := g.AddObject(nil, e.Player)
	Move(g, o.ID, state.ZLibrary, state.ZStack)
	o.Ability = sa
	o.StackKind, o.StackKindKnown = state.StackKindTriggered, true
	o.Source = e.Obj
	// The same foreign-face provenance the GainedAbilityPush mint stamps
	// (r3): the foreign face's own SVar table, OptionalDecider$ gate,
	// intervening-if and label all stay resolvable after the grant ends.
	o.GainedFace = foreign.Face()
	o.GainedFrom = foreign.ID
	// pushTrigger serializes the queue-time ctx Remembered AFTER the
	// provenance slot (IDs[0] is the foreign card): the same decode
	// TriggerPush/AbilityPush run through rememberedFrom, so a gained
	// trigger's remembered-derived body (Defined$ Remembered,
	// Remembered$Amount, Card.IsRemembered) resolves the same referents a
	// live queue walked with. Without this the wrapper resolves with an
	// empty remembered set and the body acts on nothing.
	o.Remembered = rememberedFrom(e.IDs[1:])
}
