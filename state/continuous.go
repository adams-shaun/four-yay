// ContinuousEffect and its Layer/Sublayer vocabulary live in state, below
// effects and rules, purely so effects primitives (Pump, PumpAll, Animate,
// Protection -- see mtgcore/effects/combatfx.go) can build one and hand it to
// rules.Engine through the effects.Host interface without effects ever
// importing rules (the dependency order is cards -> state -> decision ->
// events -> effects -> rules -> view -> seat -> replay -> cmd/*, and effects
// must never import rules). The layer computation itself -- ordering,
// expiry, Derived -- stays in rules/layers.go, which re-exports these same
// names as aliases so its own existing API and tests are unaffected by the
// move. See Task 19c.
package state

import "github.com/adams-shaun/gorge/cards"

// GainedFace is one foreign card whose abilities a has-all-abilities-of
// static grants (state.ContinuousEffect.GainedFaces): Obj is the object
// carrying Face at scan time (so the activation/trigger events can name it),
// and Face is the compiled face whose Abilities and Triggers are gained.
// Both are re-derived live on every static rescan, so a card that leaves the
// scoped zone drops its grant at the next event.
type GainedFace struct {
	Obj  ObjID
	Face *cards.Face
}

// Layer is CR 613's application order.
type Layer uint8

const (
	LCopy      Layer = 1
	LControl   Layer = 2
	LText      Layer = 3
	LType      Layer = 4
	LColor     Layer = 5
	LAbilities Layer = 6
	LPT        Layer = 7
)

// Sublayer is CR 613.4's breakdown of layer 7.
type Sublayer uint8

const (
	SubNone     Sublayer = 0
	SubCDA      Sublayer = 1 // 7a characteristic-defining
	SubSet      Sublayer = 2 // 7b setting
	SubModify   Sublayer = 3 // 7c modifying
	SubCounters Sublayer = 4 // 7d counters
	SubSwitch   Sublayer = 5 // 7e switching
)

// ContinuousEffect is one active modification. Affects is a Forge filter spec
// evaluated with effects.MatchesSpecFrom against each object on the
// battlefield, so continuous effects reuse the same filter language as
// everything else rather than reimplementing predicate matching. (That
// evaluation itself lives in rules.Engine.Derived, which is free to import
// effects; this type only needs to be nameable from both packages.)
type ContinuousEffect struct {
	Source ObjID
	// SourceIncarnation anchors a self-scoped grant to the battlefield
	// incarnation that registered it, so a stable ObjID returning after a
	// zone change cannot inherit the old grant. Engine-runtime only, rebuilt
	// by re-execution on replay.
	SourceIncarnation uint32
	// DurationSource is the permanent whose continued presence controls a
	// source-scoped one-shot effect. Source remains the object used by Affects
	// and layer derivation; this separate anchor handles effects such as
	// Exchange of Words that modify other objects while its enchantment stays.
	DurationSource ObjID
	Timestamp      uint32
	Layer          Layer
	Sub            Sublayer
	Affects        string
	Controller     PlayerID
	UntilEOT       bool

	// FromEffect marks a registration created by the api:Effect primitive (the
	// Ctx-side stand-in for Forge's implicit Command-zone effect object). The
	// one-shot self-exile idiom (`DB$ ChangeZone | Defined$ Self | Origin$
	// Command | Destination$ Exile`) ends exactly the source's Effect-created
	// registrations (rules.Engine.EndEffectSource), so a printed static of the
	// SAME source -- registered by the layer-5/6 static scan, not by an
	// Effect -- is never a casualty. Rebuilt by re-execution on replay like
	// every other continuous-effect field.
	FromEffect bool

	// SVars is the SVar table of the face that carries this static, so a
	// deferred expression (AddPowerExpr$ naming an SVar) resolves against the
	// face that wrote it. nil means the source object's top face -- every
	// numeric-API construction keeps today's behaviour. A card merged beneath
	// a mutated pile's top (CR 702.140d) sets it to the under-card's table.
	SVars map[string]string

	AddPower, AddToughness int32
	// DoublePower/DoubleToughness implement Pump's Double stat op in layer 7c:
	// the current value at this effect's timestamp is added again.
	DoublePower, DoubleToughness bool
	SetPower, SetToughness       int32
	HasSet                       bool
	AddKeywords                  []string
	AddTypes                     []string

	// SetName is a layer-3 name overwrite (SetName$), resolved by rules' layer walk.
	SetName string

	// TextFrom/TextTo carry a layer-3 TEXT-substitution effect (CR 613.1d /
	// CR 612, Forge's api:ChangeText): while the effect applies, every
	// whole-word, case-insensitive instance of TextFrom in the affected
	// object's printed Oracle text is replaced by TextTo, applied in
	// timestamp order by rules' layer walk. An empty TextFrom substitutes
	// nothing (a substitution whose source word is empty is never
	// meaningful), so a zero value leaves the printed text untouched.
	// TextSet replaces the printed text outright (the sibling
	// api:ExchangeTextBox swaps two objects' text boxes). TextSetSet
	// distinguishes an intentionally empty text box from no TextSet effect;
	// the walk starts from TextSet and then applies any TextFrom/TextTo
	// substitutions.
	TextFrom, TextTo string
	TextSet          string
	TextSetSet       bool

	// AddPowerExpr preserves a static P/T parameter that must be evaluated
	// against its source each time characteristics are derived (for example
	// +X or -X). An empty expression retains the already-resolved numeric
	// field. Written only by rules' static scanner; the numeric fields above
	// stay the API for resolution-created effects.
	AddPowerExpr, AddToughnessExpr string
	// AddPowerAffected and AddToughnessAffected mark the Forge AffectedX
	// convention: only those expressions re-anchor their count source on the
	// object receiving the pump. Other named expressions keep their grantor
	// source (for example, Mace of the Valiant's counter count).
	AddPowerAffected, AddToughnessAffected bool
	SetPowerExpr, SetToughnessExpr         string
	// SetPowerPresent and SetToughnessPresent distinguish an omitted setter
	// from an explicit zero on a static that sets only one characteristic.
	SetPowerPresent, SetToughnessPresent bool
	// StaticSet marks a setter read from a card's S:Mode$ Continuous line.
	// Such a static may set exactly one characteristic, so Derived must gate
	// each assignment on its corresponding *Present bit. Effects created by
	// the older numeric ContinuousEffect API leave this false and retain
	// their historical paired-setter behaviour for compatibility.
	StaticSet bool

	// AddColors is a layer-5 colour change (CR 613.1e): the WUBRG letters of
	// the colours the affected object GAINS. Written by the continuous-effect
	// primitives (effects' Animate Colors$ without OverwriteColors$) AND by
	// rules' static scan for a Mode$ Continuous static's AddColor$ ("...in
	// addition to its other colors", Blade of the Oni / Angelic Armaments),
	// composed by rules' Derived in timestamp order. Empty on every effect
	// that grants no colour. "All"/"Colorless" never reach this field: the
	// registering path normalises them to "WUBRG" and the empty set
	// respectively.
	AddColors []string
	// OverwriteColors marks a layer-5 colour SET (Forge's Animate
	// OverwriteColors$ True with Colors$, and a Mode$ Continuous static's
	// SetColor$ -- Imprisoned in the Moon's "is a colorless land",
	// Kenrith's Transformation's green Elk, Leyline of the Guildpact's
	// "is all colors"): while this effect applies, the affected object's
	// colours are exactly AddColors -- replacing, never extending, the
	// printed colours and every earlier layer-5 grant (CR 613.1e sets by
	// timestamp order). With it false AddColors extends.
	OverwriteColors bool
	// RemoveCreatureTypes is Forge's Animate RemoveCreatureTypes$ True: while
	// this effect applies, the affected object loses every creature-type
	// SUBTYPE (its face's types that are neither card types nor supertypes)
	// BEFORE this same effect's AddTypes apply -- so an animated creature's
	// own new creature type lands on a stripped base, and the printed types
	// return the moment the animation expires because Derived recomputes from
	// the face. A printed planeswalker's name-subtype ("Sarkhan") is stripped
	// with the rest while the walker is animated as a creature.
	RemoveCreatureTypes bool
	// RemoveSubTypes strips every subtype (creature, land and other kinds)
	// before this effect's AddTypes are applied.
	RemoveSubTypes bool
	// RemoveTypes names the exact type words removed by Animate's
	// RemoveTypes$ at layer 4, before this effect's AddTypes. Unlike
	// RemoveSubTypes, this can remove a card type or supertype too.
	RemoveTypes []string
	// SetCreatureTypes strips only creature subtypes, preserving land and
	// other subtype words; its replacement types live in AddTypes.
	SetCreatureTypes bool
	// AddAllCreatureTypes is Forge's AddAllCreatureTypes$ True (Maskwood
	// Nexus's "creatures you control are every creature type", the manland
	// family): while this effect applies, the affected object is EVERY
	// creature subtype alongside whatever AddTypes grants (CR 613.1c's "all
	// creature types" grant). The subtype vocabulary is deliberately NEVER
	// materialised into AddTypes at registration: rules' typeCharacteristics
	// appends effects.CreatureTypeWordList() into the walk's type list when
	// the flag is set, so the effects filter's type predicates answer it
	// through ExtraTypes exactly the way Changeling's intrinsic CDA answers
	// through hasType. Set by the S:Mode$ Continuous scanner (rules'
	// staticEffects) and the Animate primitive (AddAllCreatureTypes$ on
	// Mutavault's animation line).
	AddAllCreatureTypes bool
	// RemoveCardTypes is the S:Mode$ Continuous RemoveCardTypes$ True strip
	// (Darksteel Mutation, Kenrith's Transformation, Witness Protection):
	// while this effect applies, the affected object loses every card type
	// AND every subtype -- subtypes are tied to their card types (CR
	// 205.2-family), so the object keeps only its supertypes -- BEFORE this
	// same effect's AddTypes apply (strip-before-add, like
	// RemoveCreatureTypes). Set only by the static scanner today; the Animate
	// primitive does not read RemoveCardTypes$ yet.
	RemoveCardTypes bool
	// RemoveLegendary is Forge's CopyPermanent NonLegendary$ True (the
	// "except it isn't legendary" clause, e.g. Multiversal Recruitment): while
	// this effect applies, the affected object loses the Legendary supertype
	// from its layer-4 type list, leaving every other supertype in place.
	// Honoured in rules' typeCharacteristics beside the RemoveCardTypes /
	// RemoveCreatureTypes strips, and applied BEFORE this same effect's
	// AddTypes (strip-before-add), so a copy that both strips Legendary and
	// gains types keeps the base stripped first.
	RemoveLegendary bool
	// AddAbilities is a layer-6 ability GRANT (CR 613.1f): the SVar names --
	// on the SOURCE object's own face -- of the AB$ activated abilities the
	// affected object gains for the effect's lifetime. Written only by the
	// continuous-effect primitives (effects' Animate Abilities$), read only
	// by rules' offer/activation paths (legal.go's grantedAbilities), never
	// by the CR 613 layer sorter itself: the grant contributes no
	// characteristic, it contributes an activation surface. Empty on every
	// effect that grants none.
	AddAbilities []string

	// GainedFaces carries a has-all-abilities-of static's foreign faces for
	// its ACTIVATED half (Forge's GainsAbilitiesOf$ on a Mode$ Continuous
	// static, the Idris, Soul of the TARDIS shape): each entry pairs the face
	// of the named card with the object that face belongs to, so the affected
	// object gains every ACTIVATED ability of Face the grant's
	// GainsValidAbilities$ filter admits (the layer-6 grant rules'
	// grantedAbilities offers). GainsAbilitiesOf$ alone NEVER grants
	// triggered abilities: Forge's parameter means activated only, so the
	// triggered half lives on GainedTriggerFaces and the trigger walk reads
	// only that. Unlike AddAbilities the abilities are compiled SAs on the
	// FOREIGN card -- not SVar names on this effect's source -- so the
	// consumers read the face directly and the events they emit carry the
	// foreign object id and the face-ability index, which a replay
	// re-resolves identically. The rule 613 layer sorter ignores this field:
	// like AddAbilities it contributes an activation surface, never a
	// characteristic. Written only by rules' static scan (rules/layers.go).
	// Empty on every effect that gains no activated ability.
	GainedFaces []GainedFace

	// GainedTriggerFaces is the TRIGGERED half of the same grant (Forge's
	// GainsTriggerAbsOf$, the second parameter of Idris's static): the same
	// GainedFace pairing, consumed by the granted-trigger walk (which queues
	// every trigger Face.Triggers carries) and by the owning-face recovery.
	// GainsTriggerAbsOf$ alone grants triggered abilities and NOTHING
	// activated. Written only by rules' static scan (rules/layers.go). Empty
	// on every effect that gains no triggered ability.
	GainedTriggerFaces []GainedFace

	// GainsValidAbilities is the activated-ability filter a GainsAbilitiesOf$
	// grant carries (Forge's GainsValidAbilities$, e.g. Sharkey's
	// `Activated.!ManaAbility`, Nicol Bolas Dragon-God's `Activated.Loyalty`):
	// comma alternatives, each `Activated` with optional dot qualifiers, read
	// by grantedAbilities so a foreign ability outside the filter is never
	// offered, never a mana candidate. An unmodelled qualifier fails closed
	// (that alternative admits nothing). Engine-runtime only, rebuilt by
	// re-execution on replay like every other continuous-effect field.
	GainsValidAbilities string

	// GainsLimitPerTurn is the per-foreign-ability activation cap a
	// GainsAbilitiesOf$ grant carries (Forge's GainsAbilitiesLimitPerTurn$,
	// Mairsil the Pretender's "You may activate each of those abilities only
	// once each turn"): each gained activated ability may be activated at
	// most this many times per turn, counted per (foreign card, face-ability
	// index) identity from the replayable log. 0 = no limit (the parameter is
	// absent or unparseable). Engine-runtime only, rebuilt by re-execution on
	// replay like every other continuous-effect field.
	GainsLimitPerTurn int

	// GainedZones is the zone scoping a GainedFaces/GainedTriggerFaces grant
	// was built under (Forge's GainsAbilitiesOfZones$, default Battlefield):
	// the zones the named card must sit in for its abilities to be gained.
	// Engine-runtime only, rebuilt by re-execution on replay like every other
	// continuous-effect field.
	GainedZones string
	// Restriction carries an Effect-created S: mode (CantTarget,
	// CantRegenerate) rather than a layer change. When non-empty the effect is
	// a rules-mod, consulted by the decision point the mode names (rules'
	// askTarget for CantTarget, effects.ReplaceDestruction via the Host for
	// CantRegenerate); the layer fields above are ignored for it, and it is
	// never handed to the CR 613 layer sorter as a characteristic change.
	// A zero value means the effect is an ordinary layer effect.
	Restriction string
	// RestrictParams carries the restriction's S: parameters (ValidCard$,
	// ValidTarget$, Activator$...) so the consultation point can resolve the
	// same logic the registered static carries. Map-only, never read by the
	// layer sorter.
	RestrictParams map[string]string
	// RestrictSVars is the SVar table a restriction static's own parameters
	// resolve against (a CantBlockUnless body's Cost$ naming an SVar on the
	// granting face: Whipgrass Entangler's WhipgrassClericNum, War Cadence's
	// XChosen). The printed static route reads the carrying face's table
	// (staticView.SVars); this is the delivered route's equivalent, captured
	// from the granting ability's Ctx at registration. Map-only, never read
	// by the layer sorter. Nil on every restriction that names no SVar.
	RestrictSVars map[string]string
	// Name is an Effect's Name$ (Wrenn and Six's "Emblem — Wrenn and Six",
	// Sephiroth's emblem): the effect's own display name, carried so an Effect
	// whose Stackable$ is False can ask the registry (rules'
	// ContinuousNamed) whether a same-name effect from the same controller is
	// already active and decline to stack a second one. Empty on every effect
	// that carries no Name$.
	Name string
	// Remembered is the objects the Effect captured for its restriction
	// (Vines of Vastwood's targeted creature, Incinerate's damaged creature),
	// so a restriction whose ValidCard$/ValidTarget$ says "Card.IsRemembered"
	// can be resolved against the set the effect actually held, not against
	// nothing. Objects only; a player-only remembered target yields an empty
	// slice.
	Remembered []ObjID
	// RememberedPlayers is the PLAYERS an Effect captured for its restriction
	// (Call for Aid's RememberObjects$ TargetedPlayer: the targeted opponent
	// whose creatures were stolen, so the registered CantAttack's Target$
	// Player.IsRemembered — "you can't attack that player" — has a remembered
	// player to resolve). effectRemembered deliberately records objects only;
	// a player-only remember yields an empty slice there, so this field is the
	// explicit player half of the same capture. Empty on every effect that
	// captured no players. Engine-runtime only, rebuilt by re-execution on
	// replay like every other continuous-effect field.
	RememberedPlayers []PlayerID
	// Chosen is the resolution's chosen-card set (the ChooseCard answer)
	// snapshotted when an Effect-delivered may-play grant registered, and
	// ChosenBound says a snapshot was taken. Forge's EffectEffect copies the
	// host's chosen cards onto the effect card it creates, so the grant's
	// Affected$ Card.ChosenCard (Strongbox Raider, Chandra, Flameshaper,
	// Feldon, Party Thrasher) names the card chosen at creation even after
	// the chain's ClearChosenCard$ cleanup wipes the source's list. A
	// chosen card that leaves the grant's AffectedZone$ drops out (CR
	// 400.7). Engine-runtime only, rebuilt by re-execution on replay.
	Chosen      []ObjID
	ChosenBound bool
	// Duration is the original Duration$ value ("" means Permanent, the
	// effEffect default) preserved for reporting and for the expiry decision
	// in rules/layers.go. Cosmetic for a layer effect.
	Duration string
	// Permanent marks a resolution-created effect with no stated duration
	// (CR 611.2a): it lasts until the end of the game, outliving its source
	// (a one-shot spell is already in the graveyard by the time it registers)
	// and surviving every EndOfTurnCleanup. Distinct from Duration "Permanent"
	// on a permanent's OWN static, which CR 611.3b scopes to the source's
	// battlefield presence -- that case leaves Permanent false and keeps the
	// source-leaves rule. Set only by the continuous-effect primitives that
	// build a lasting one-shot (effects/combatfx.go).
	Permanent bool
	// ReplacementEvent/ReplacementParams/ReplacementBody describe an Effect-created
	// replacement (for example Blood of the Martyr). They are deliberately plain
	// data rather than cards types: state sits below cards' parsed SA graph.
	// Rules reconstructs the body at application time under the original source's
	// SVar context. An empty ReplacementEvent is not a replacement effect.
	ReplacementEvent  string
	ReplacementParams map[string]string
	ReplacementBody   string
	// ChosenNumber is the Effect's SetChosenNumber$ binding: the number the
	// Effect resolved when it was created (Torgal's Dog/Wolf count at trigger
	// time, Wildgrowth Archaic's TriggeredCard$Converge snapshot, Communal
	// Brewing's ingredient-counter count), read later by the registered
	// replacement's body through the Count$ChosenNumber head (rules' replCtx
	// threads it into the body Ctx). Binding ONCE at creation against the
	// trigger's own context is the point: a live re-read after the entry would
	// answer a different question. Engine-runtime only, rebuilt by
	// re-execution on replay like every other continuous-effect field. Zero
	// means nothing bound (and reads as zero).
	ChosenNumber int32
	// RemoveAbilities is a layer-6 ability-removing effect (CR 613.1f/613.4b,
	// e.g. Humility's RemoveAllAbilities$ True): when an applicable effect
	// carries it, Derived clears the object's printed (and any earlier-granted)
	// keywords/abilities before later layer-6 grants re-add any.
	RemoveAbilities bool
	// SourceAbilityLayer marks an effect generated by a STATIC ABILITY of
	// Source (rules' static scan sets it; a registered effect from a resolved
	// spell or ability leaves it zero): the earliest CR 613 layer in which
	// that one static ability applies, or LPT+1 for a rules modification that
	// is no layer effect at all (a may-play grant, an extra land drop). It is
	// what lets rules stop applying the effect when Source itself loses all
	// abilities (CR 613.1f): an effect that began applying in an earlier
	// layer keeps applying (CR 613.6), one that begins in layer 6 or later
	// does not exist once its ability is gone (rules/abilityloss.go).
	SourceAbilityLayer Layer
	// RemoveKeywords names keywords (matched by cards.KeywordHead, so a
	// parameterised grant is removed by its head) that the affected object
	// loses at layer 6 (CR 613.1f). Honoured in rules' LAbilities walk BEFORE
	// AddKeywords on the SAME effect, so a modification that removes one
	// keyword and grants another (CopyPermanent's RemoveKeywords$ Soulbond |
	// AddKeywords$ Haste) applies in the order the card text reads whichever
	// way the timestamps order independent effects. Empty on every effect
	// that removes none.
	RemoveKeywords []string
	// CantHaveKeywords names keywords the affected object can't have or gain
	// (CR 613.1f): after the layer-6 walk has applied every grant, Derived
	// removes each prohibited keyword head, suppressing printed, intrinsic and
	// granted keywords alike. Empty on effects that prohibit none.
	CantHaveKeywords []string
	// MayPlay marks a may-play-from-zone grant (CR 401.5: "you may play
	// cards of a certain kind from a zone other than the one they would
	// normally be played from", e.g. Conduit of Worlds' "You may play lands
	// from your graveyard."). Registered from an S:Mode$ Continuous static
	// that carries MayPlay$ True, alongside the Affects filter (the Affected$
	// spec, the effect's ordinary filter field) and AffectedZone. When set
	// the effect is a rules-mod consulted by the land-play offer (rules'
	// mayplay.go), never a CR 613 layer change -- no layer fields are
	// read for it -- and it expires with its source permanent (CR 611.3b)
	// through the ordinary source-on-battlefield check in active().
	MayPlay bool
	// AffectedZone is the may-play grant's AffectedZone$ value (a single
	// zone, a comma-separated list, or "All"), interpreted with
	// effects.ParseZones. Meaningful only when MayPlay is set.
	AffectedZone string

	// SetMaxHandSize is an Effect-delivered S:Mode$ Continuous | Affected$
	// <players> | SetMaxHandSize$ <value> static's raw value ("Unlimited" or a
	// numeric string), the CR 514.1 maximum this effect sets for the affected
	// players. Like MayPlay it is a rules-mod consulted by one decision point
	// (rules' maxHandSizeFor, from cleanupStep), never a CR 613 layer change;
	// the Affects spec and this field are what the consultation reads. Empty on
	// every effect that does not set a maximum hand size. Registered by
	// effects' effEffect (the Effect-delivered route: Finale of Revelation's
	// STHandSize, Wrenn and Seven's UnlimitedHand) and read beside the printed
	// S:-static scan so the two routes cannot disagree about the value's
	// grammar (rules' handSizeValue).
	SetMaxHandSize string

	// MayPlayIgnoreColor marks the grant's MayPlayIgnoreColor$ True rider:
	// "you may spend mana as though it were mana of any color to cast it"
	// (Opposition Agent, Kotose, ...). While it holds, every coloured pip of
	// the may-play cast's cost is payable by any colour of mana in the pool;
	// the {C} pip stays colourless-only (CR 107.4c: "any color" never
	// includes colourless). Set only together with MayPlay.
	MayPlayIgnoreColor bool

	// MayPlayLimit is the grant's MayPlayLimit$ once-per-turn cap (Kotose's
	// and Evelyn's "once each turn"): the number of cards p may play through
	// THIS KIND of grant in one turn. Zero means unlimited. The count is a
	// log scan (rules' mayPlaysThisTurn), never a mutable field.
	MayPlayLimit int32

	// ShieldTargets/ShieldTargetPlayers carry a prevention shield's
	// ShieldEffectTarget$ ParentTarget binding (Acolyte's Reward, Vengeful
	// Archon): the PARENT SA's chosen targets, which the registered
	// PreventionSubAbility$ rider's DB$ DealDamage hits with the amount each
	// application prevented (rules' applyReplaceDamageTail). Objects only in
	// the first, players only in the second. Engine-runtime only, rebuilt by
	// re-execution on replay like every other continuous-effect field.
	ShieldTargets       []ObjID
	ShieldTargetPlayers []PlayerID

	// MayPlayPlayerTurn marks the grant's Condition$ PlayerTurn rider (the
	// Kess/Karador "during each of your turns" family): the grant is live
	// only while its controller is the ACTIVE player. The walks check the
	// live turn, never a mutable field.
	MayPlayPlayerTurn bool

	// MayPlayFree marks the grant's MayPlayWithoutManaCost$ True rider (the
	// free-cast may-play family: Dauthi Voidwalker's "you may play it this
	// turn without paying its mana cost", Idol of Endurance, Nicol Bolas,
	// God-Pharaoh): the mana part of the granted play is free while
	// non-mana additional costs still apply (CR 118.9). Registered only
	// from the Effect-delivery path (a DB$ Effect's StaticAbilities$ SVar
	// static); the printed-S: battlefield route keeps MayPlayStaticParams'
	// free refusal because its entries carry no free flag and the printed
	// route's free read lives in mayPlayStatic. Set only together with
	// MayPlay.
	MayPlayFree bool

	// MayPlayValidAfterStack is an Effect-delivered may-play grant's
	// ValidAfterStack$ qualifier (Nahiri, Forged in Fury's "You may cast
	// Equipment spells this way without paying their mana costs":
	// ValidAfterStack$ Spell.Equipment). It is a DERIVED stack-view spell
	// characteristic filter, not a state fact: the card is still in its
	// origin zone while the permission is offered, so rules evaluates it
	// through the ordinary spec matcher with SpecContext.AsStack set, the
	// same derived override mayPlayStatic uses for a printed S: grant's
	// ValidAfterStack$. An absent value means no characteristic gate; an
	// unsupported value fails closed in the matcher (no grant). Registered
	// only from the Effect-delivery path (a DB$ Effect's StaticAbilities$
	// SVar static); the printed-S: route keeps MayPlayStaticParams' refusal
	// because rules/layers.go's printed grant carries no such field and
	// evaluates the qualifier inline in mayPlayStatic. Set only together
	// with MayPlay.
	MayPlayValidAfterStack string

	// AddTrigger is a static-grant's triggered ability (AddTrigger$ on a
	// Mode$ Continuous static, e.g. Hearthhull's "STATION 8+ Whenever you
	// sacrifice a land"): the SVar-parsed trigger (cards.ParseTriggerLine
	// off the granting face's own SVar table, its Execute$ linked there) the
	// objects the static's Affected$ matches gain while the static is live.
	// Written only by rules/layers.go's static scan, matched and queued by
	// rules/trigger_match.go's granted-trigger walk (the granted-Ward/
	// granted-Dethrone precedent); it contributes no CR 613 characteristic
	// and the layer sorter ignores it. Nil on every effect that grants none.
	AddTrigger *cards.Trigger
	// TriggerGrantor is the object whose SVar table resolves an AddTrigger
	// grant's Execute$ body when that is NOT the effect's own Source: the
	// Animate route (effects/combatfx.go) registers the grant with Source =
	// the ANIMATED object (the trigger fires as that object's trigger and
	// Affects Card.Self must name it), while the T:-shaped body lives on the
	// ANIMATING face's table (Dragon Cursed Halls animating a target
	// creature). 0 = the statics route, where Source itself carries the body
	// (an Aura granting its enchanted creature a trigger). Engine-runtime
	// only, rebuilt by re-execution on replay like every other
	// continuous-effect field.
	TriggerGrantor ObjID
	// AbilityGrantor is the object whose SVar table resolves an AddAbilities
	// grant's body when that is not the effect's Source. 0 means Source.
	// Like TriggerGrantor, this is engine-runtime state rebuilt by resolution;
	// it is not part of the event schema.
	AbilityGrantor ObjID
	// AddSVars is a static-grant's named variables (AddSVar$): the SVar the
	// affected object GAINS, parsed from Forge's "SVar:<Name>:<Value>" value
	// shape. The corpus's granted SVars are AI-evaluation hints (AE, AITap,
	// MustBeBlocked) no rules consumer reads yet; the engine records the
	// grant and resolves it through Engine.GrantedSVar, the same lookup a
	// later CheckSVar$-style consumer of the affected object's variables
	// reads. Nil on every effect that grants none.
	AddSVars map[string]string
	// GainControl is a control-change static (Mind Control's "You control
	// enchanted creature", Fealty to the Realm's "The monarch controls
	// enchanted creature"): the value is the GainControl$ parameter of an
	// S:Mode$ Continuous static, and while the static is live every object
	// its Affects spec matches is controlled by the player the value names.
	// Like MayPlay it changes no characteristic and is not a CR 613 layer
	// change -- it is a rules-mod realized by rules' static-control reconcile
	// (rules/control_static.go), which registers a real tracked control grant
	// (rules/control.go) and emits events.ControlChange, so triggers, the
	// controller-reset semantics and the view all see the transfer through
	// the ordinary path. The value resolves through the shared player-spec
	// grammar: "You" is the static's controller, any other qualified player
	// spec ("Player.isMonarch") resolves to the single seat the spec matches,
	// and anything that names nobody (or several) fails closed -- no grant.
	// The static's own "as long as" gate (IsPresent$/CheckSVar$) is the
	// ordinary continuousGateHolds the scan runs for every static; the grant
	// ends when the static stops being live (source left the battlefield,
	// gate flipped, Aura moved bearers) or the resolved controller changes.
	// Empty on every effect that grants no control.
	GainControl string

	// MayLookAt is a look-permission grant (MayLookAt$ on a Mode$
	// Continuous static, e.g. Oracle of Mul Daya): while the static is live,
	// the affected player may look at the object its Affected$ spec matches
	// -- in the corpus always the top card of the controller's own library
	// (Affected$ Card.TopLibrary+YouCtrl, AffectedZone$ Library). Consumed
	// by Engine.MayLookAtLibraryTop (the view's reveal of that top card);
	// it contributes no CR 613 characteristic. False on every effect that
	// grants none.
	MayLookAt bool

	// MayPlayIgnoreType marks the grant's MayPlayIgnoreType$ True rider
	// (Rakdos, the Muscle's "mana of any type can be spent to cast those
	// spells"): like MayPlayIgnoreColor it lets any colour pay a coloured
	// pip, and it additionally lets any colour pay the {C} pips, which
	// CR 107.4c's colour-only "any color" never reaches. Set only together
	// with MayPlay.
	MayPlayIgnoreType bool

	// ForgetOnMoved carries the Effect's ForgetOnMoved$ zone: a remembered
	// card that moves FROM that zone leaves the effect's Remembered set (the
	// may-play grant's Affected$ Card.IsRemembered list; Abbot of Keral
	// Keep's "for as long as it remains exiled"). Empty means the
	// effect never forgets. Engine-runtime only, rebuilt by re-execution on
	// replay like every other continuous-effect field.
	ForgetOnMoved string
	// ExileOnMoved carries the Effect's ExileOnMoved$ zone: a remembered
	// card that moves FROM that zone ENDS the whole effect (Vines of
	// Vastwood's blinked target, Abbot's exiled card being cast). Empty
	// means the effect never ends on a move. Engine-runtime only, like
	// ForgetOnMoved.
	ExileOnMoved string
	// ExileOnMovedAlso is an additional move-from zone that ends the whole
	// effect. Animate's host-scoped grants can target an object in a different
	// zone from their host, so the target's zone and the host's battlefield
	// departure must both be irreversible lifetime boundaries.
	ExileOnMovedAlso string
	// ImprintOnHost marks a DB$ Effect registration whose SA carried
	// ImprintOnHost$ True: Forge's EffectEffect imprints the CREATED EFFECT
	// TOKEN on the host card and moves the token to the Command zone -- the
	// imprint is the link "this effect belongs to this card", never the
	// remembered card itself. The corpus's dig-and-play family (Superior
	// Foes of Spider-Man, Furious Rise, Unstable Amulet) ends the previous
	// effect through its trigger's `DB$ ChangeZone | Defined$ Imprinted |
	// Origin$ Command | Destination$ Exile` -- exiling the imprinted token
	// from the Command zone is exiling the effect, the "until you exile
	// another card" lifetime -- and Word of Command / Semester's End use the
	// same idiom inside one chain. This build has no effect-token object, so
	// the marker rides every registration the resolving effEffect call
	// creates and the idiom ends exactly those through rules'
	// EndImprintedEffect. Engine-runtime only, rebuilt by re-execution on
	// replay like every other continuous-effect field.
	ImprintOnHost bool
	// ForgetCounter carries the Effect's ForgetCounter$ counter kind (task
	// vow1; Promise of Loyalty's VOW, Quicksilver Fountain's FLOOD,
	// Obsidian Fireheart's BLAZE -- 18 corpus carriers): a remembered card
	// whose count of that kind reaches zero after a counter-removal leaves
	// the effect's Remembered set -- "for as long as it has a vow counter
	// on it". The count DROPPING without reaching zero keeps the card (the
	// measured semantics this build pins: a multi-countered card loses the
	// restriction only when its LAST such counter goes). Engine-runtime
	// only, rebuilt by re-execution on replay like every other
	// continuous-effect field.
	ForgetCounter string

	// ForgetOnCast carries the Effect's ForgetOnCast$ spec (task
	// param:api:Effect.ForgetOnCast; Marshland Bloodcaster's "Rather than
	// pay the mana cost of the NEXT spell you cast this turn", Dark
	// Apostle's / Bigger on the Inside's one-cast cascade grant): the first
	// qualifying spell cast ENDS the whole effect. The spec is a card spec
	// over the cast spell, You-relative to the effect's controller, matched
	// by rules' effectCastSweep at the deferred re-walk of the cast's
	// PutOnStack (payCast, after payment) -- so an ABORTED proposal (one
	// reversed before payment, CR 733.1) never consumes the grant while a
	// completed cast, even one later countered, does. Empty means the
	// effect never forgets (Forge's explicit False degrades to this at
	// registration). Engine-runtime only, rebuilt by re-execution on
	// replay like every other continuous-effect field.
	ForgetOnCast string

	// AssignmentStaticMode carries an Effect-delivered combat-assignment
	// static's mode (currently CombatDamageToughness). Its consumer joins this
	// registration with printed assignment statics in the shared deterministic
	// collector; unrelated StaticAbilities modes are not made live by this
	// field. Empty when the Effect delivers no assignment static.
	AssignmentStaticMode string
	// AssignmentStaticParams are the parsed parameters of the delivered static.
	AssignmentStaticParams map[string]string
	// AssignmentStaticSVars is the SVar table that owns the delivered body.
	AssignmentStaticSVars map[string]string

	// CostStaticMode carries an Effect-delivered cost-modifier static's
	// mode ("ReduceCost"/"RaiseCost"/"SetCost"/"AlternativeCost" -- the
	// parseStaticLine Mode$ of the SVar body the Effect SA's
	// StaticAbilities$ entry named). The cost path reads it through the
	// SAME readers the printed S: static route feeds -- rules'
	// collectCostStatics for the Raise/Reduce/Set modes, rules'
	// alternativeCosts for AlternativeCost -- so the two registration
	// paths cannot disagree about what applies. Empty on every effect
	// that delivers no cost static. Engine-runtime only, rebuilt by
	// re-execution on replay like every other continuous-effect field.
	CostStaticMode string

	// CostStaticSVars is the SVar table that owned an Effect-delivered static.
	// Nil means the source object's current face supplies the table.
	CostStaticSVars map[string]string

	// CostStaticParams carries the static line's own parameter map (the
	// parseStaticLine output effEffect whitelisted through
	// effects.CostStaticParamsReadable before registering). The cost
	// chain's gates (ValidCard$, Activator$/Caster$, ValidSA$, ValidPlayer$,
	// ...) evaluate it exactly as they evaluate a printed static's map.
	// Engine-runtime only, rebuilt by re-execution on replay like every
	// other continuous-effect field.
	CostStaticParams map[string]string

	// CostStaticGranted marks a cost-modifier static the Affects objects
	// HAVE -- an ability granted to them (Animate/AnimateAll staticAbilities$,
	// a Mode$ Continuous AddStaticAbility$, CopyPermanent
	// AddStaticAbilities$) -- rather than a static of the Effect itself.
	// rules' cost collector binds each such static to its HOST exactly as a
	// printed static is bound to the object printing it: the host is the
	// static's source (so Card.Self names the host) and its current
	// controller is the static's controller, EffectZone$ gates on the host's
	// zone, and CostStaticSVars (the GRANTING face's table) resolves its
	// SVars. Affects "Card.Self" names Source as the one host; any other
	// spec is matched from Source against the objects in the grant's zone.
	CostStaticGranted bool

	// AdjustLandPlays marks an additional-land-drops grant (Azusa, Lost but
	// Seeking's "You may play two additional lands on each of your turns",
	// Oracle of Mul Daya, Exploration): the number of EXTRA land drops the
	// affected player gets each turn, ON TOP of the one ordinary drop
	// (CR 305.2a reads the printed sentence as a modifier on the one-drop
	// normal, so grants from multiple permanents SUM -- Azusa plus
	// Exploration is three drops, never the max). Registered from an
	// S:Mode$ Continuous static carrying a plain integer AdjustLandPlays$,
	// alongside the Affects player spec (Affected$ You/Player); the effect
	// is a rules-mod consulted by rules' land-play offer gates, never a
	// CR 613 layer change -- no layer fields are read for it -- and it
	// expires with its source permanent (CR 611.3b) through the ordinary
	// source-on-battlefield check in active(). Zero means the effect grants
	// no additional drop.
	AdjustLandPlays int32

	// CloneTarget marks a continuous effect created by an api:Clone copy
	// (DB$ Clone): the permanent that BECAME the copy. Every effect a single
	// clone registered carries the same value -- the layer-1 LCopy marker
	// (which owns the lifetime) and its layer-4/5/6/7 modifier effects (which
	// share it), so rules' clone sweep can expire a copy as one unit: it
	// drops the marker on the marker's own duration and removes every
	// effect whose CloneTarget is the same object, emitting the ClonePermanent
	// clear that drops the object's CopyFace basis. Zero on every other
	// effect. Engine-runtime, rebuilt by re-execution on replay like the
	// other resolution-created fields.
	CloneTarget ObjID
	// CloneDurationTarget is the tapped object whose actual untap ends a
	// Duration$ UntilTargetedUntaps copy (not necessarily CloneTarget).
	CloneDurationTarget ObjID

	// CloneSource, CloneName and CloneGainThisAbility describe the copy the
	// layer-1 LCopy MARKER of a clone unit owns: the object whose printed
	// face the copy is taken from, the NewName$ rider (empty means the
	// source's own name) and the GainThisAbility$ True rider. They exist so a
	// unit's expiry can RE-BASE the become object onto whatever OTHER clone
	// unit is still live on it (CR 613.1a applies copy effects in timestamp
	// order, so the highest-timestamp survivor wins) instead of clearing the
	// shared CopyFace basis outright. Set only on the marker (Layer LCopy)
	// by effects' api:Clone; zero on its sibling modifier effects and on
	// every non-clone effect. Engine-runtime, rebuilt by re-execution on
	// replay like CloneTarget itself.
	//
	// The re-base re-snapshots the surviving source's face at expiry time
	// rather than replaying the original snapshot, so a survivor whose own
	// source has since changed re-bases onto the source's CURRENT printed
	// face. Measured corpus-unreachable (no carrier stacks two clone units
	// on one permanent and then mutates the older one's source), recorded in
	// AGENTS.md's clone row.
	CloneSource          ObjID
	CloneName            string
	CloneChosenName      string
	CloneStaticBodies    []string
	CloneGainThisAbility bool
	CloneAbilityIndex    int32
	// CloneTriggerIndex is the gain-this-trigger counterpart of
	// CloneAbilityIndex: a one-based index into the become object's
	// top-face TRIGGERS, for a GainThisAbility$ True DB$/SVar body whose
	// root is a TRIGGER (Cryptoplasm, Lazav, ...). Zero when the resolving
	// root is an activated/spell ability (CloneAbilityIndex then carries
	// it) or when no root was recovered. Carried by the same layer-1 LCopy
	// marker so a unit's expiry can re-base the become object onto a
	// surviving clone unit and re-emit exactly the trigger it owns.
	CloneTriggerIndex int32

	// UntilTurn is the turn number at whose END (its cleanup step) this
	// effect expires, for a Duration$ that spans the controller's NEXT turn
	// (UntilYourNextTurn, UntilTheEndOfYourNextTurn). Computed at
	// registration from the live turn/active-player rotation
	// (rules.Engine.AddContinuous), so an effect on a one-shot spell outlives
	// its source and is dropped only by EndOfTurnCleanup when e.G.Turn
	// reaches UntilTurn -- a real turn-boundary lifetime, not the
	// source-leaves branch nor a premature end-of-this-turn expiry. Zero
	// means "no turn boundary": the effect is UntilEOT or expires with its
	// source, per the fields above. Reproduced byte-identically by a replay,
	// because AddContinuous recomputes it from the same deterministic
	// rotation when the intents re-execute.
	UntilTurn int32
}
