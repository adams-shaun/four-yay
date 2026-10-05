// cast.go is the cast-flow state machine's data model: the chooseFor values
// it adds to rules/engine.go's switch table and the pendingCast struct that
// lives from beginCast to commitCast (or an abort). Its behaviour is split
// across the cast_* files in this package: cast_begin.go starts a cast from a
// chosen "cast" priority option and runs its stages, cast_pricing.go and
// cast_payment.go resolve a chosen alternative into the cost that is paid,
// cast_asks.go/cast_subcounter.go/cast_etbchoice.go ask the stage questions,
// cast_targets.go takes targets, cast_commit.go commits the cast (payCast),
// and cast_window.go probes the CR 601.2g mana window. Kicker, Surge,
// Flashback and Delve are registered as the primitives they are
// (rules/legal.go builds the options that choose among them; these files
// resolve whichever one was picked into a Cost and drive it to the stack).
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// chooseCast and chooseMiracle extend chooseFor (rules/engine.go
// declares chooseNone = iota, the only value Task 8 needed). iota+1 here
// keeps every value distinct from chooseNone without redeclaring it --
// nothing outside this package compares chooseFor values, so the exact
// numbers only need to be pairwise different, not contiguous with the other
// file's block.
const (
	chooseCast chooseFor = iota + 1
	chooseMiracle
	// chooseETBEntry marks an entry-boundary as-enters choice.
	chooseETBEntry chooseFor = 32
	// chooseRiot is deliberately outside the independently extended
	// chooseCleanup/chooseMana ranges in combat.go and mana_activation.go.
	// It is 20 because the merged package occupies 1 through 18 (cast/etb/
	// miracle 1-3, cleanup 4, damageDivision 5, mana 6-9, opening 10,
	// suspendCast 12, station 13, unlock 14, cumulative 15, triggeredCost
	// 16, manaUnless/unlessCost 17-18): all pairwise-distinct consts in one
	// switch table, so the exact numbers do matter inside the package.
	chooseRiot chooseFor = 20
	// chooseSiege is the CR 310.10 Siege protector choice, also parked as an
	// as-enters replacement (applySiegeProtector) for every MoveZone entry
	// path. 26 is the next free value after chooseCommanderColor (25) and the
	// chooseRiot+1.. family; the numbers matter only inside this package's
	// switch table.
	chooseSiege chooseFor = 26
	// chooseAttached is the Attached-replacement name/type election
	// (rules/replacement.go). 30 is the next free value: 27-29 are
	// chooseEnlist / chooseAttackPay / chooseUnleash, each defined relative
	// to a neighbour, and 40 is chooseUntap.
	chooseAttached chooseFor = 30
	// chooseTokenReplace is the chosen-copy CreateToken replacement's
	// election (rules/replacement.go's poseChosenTokenReplacement park:
	// Esix/Moonlit/Mirrormind's `Type$ ReplaceToken | TokenScript$ Chosen`).
	// Originally 31 (next free after chooseAttached); the merged package
	// gave 31 to chooseManaSacrifice, so 43 is the next free value after
	// chooseManaConvert (42).
	chooseTokenReplace chooseFor = 43
	// chooseManaConvert is the cast-time election for an Optional$ ManaConvert
	// static. It is deliberately separate from the mana-source window: the
	// player chooses whether to use the permission before targets and payment.
	chooseManaConvert chooseFor = 42
	// chooseTurnUp is the CR 708.6 morph-family turn-face-up special action's
	// payment flow (rules/morph_turnup.go): the announced X and the non-mana
	// cost components (sacrifice/discard/reveal/return) it must choose. 46 is
	// the next free value: 44 is chooseLegend (rules/sba.go) and 45 is
	// chooseDefeatedCast (rules/turn.go). TestChooseForValuesAreDistinct
	// fails the build on a collision.
	chooseTurnUp chooseFor = 46
)

// pendingCast is the cast flow's own state, live only between beginCast and
// commitCast (or an abort). ability is -1 for a spell; Task 10 (activated
// abilities) sets it to a real Face().Abilities index and reuses this same
// flow for a cost with X/Sac/Delve of its own.
type pendingCast struct {
	player  state.PlayerID `clone:"deep"`
	card    state.ObjID    `clone:"deep"`
	from    state.Zone     `clone:"deep"`
	mode    string         `clone:"deep"` // "", "kicked", "surged", "flashback", "miracle", and the alternative-cost modes this file offers
	ability int            `clone:"deep"` // -1 for a spell (Task 10 uses >= 0)
	// abilityMerged is the pile position of the activated ability pc.ability
	// names: 0 for the top face, i+1 for the i-th card merged beneath it
	// (CR 702.140d). It selects the SVar table a computed cost/limit resolves
	// against, so an under-card ability reads its OWN table, never the pile
	// top's. Zero for a spell and for every top-face ability.
	abilityMerged int `clone:"deep"`

	// CastPayment (rules/pay) is the cast's payment state beside PaidCost:
	// the planned payment it executes, the announced window and its taps,
	// the cost-part ask cursors, the Convoke announcement and the flexible
	// pip announcement. The payment layer reads and records it without the
	// rest of the pending cast (lasagna spec §9.2, E7 flow slice 4).
	pay.CastPayment `clone:"deep"`

	// grantSource / grantSVar (task grantcost1) anchor a GRANTED activation
	// (rules/speed.go's beginGrantedActivation, reached from the max-speed
	// "granted" option and beginActivation's SVar branch): the body is the
	// SVar the AddAbility$ grant names, resolved off the GRANTOR's face, and
	// grantSource is the resolved grantor object (== card for a self-grant,
	// the offer's GrantSource fallback already applied). Empty SVar means a
	// printed/spell proposal; pc.ability stays -1 for a granted one. Both are
	// plain values, so Clone's shallow copy carries them like every scalar
	// above.
	grantSource state.ObjID `clone:"deep"`
	grantSVar   string      `clone:"deep"`

	// grantKeyword anchors a KEYWORD-GRANTED activation (CR 613.1f, the
	// layer-6 AddKeyword$ Cycling/TypeCycling route): the body is synthesized
	// from the derived keyword line the option carries ("Cycling:1 U",
	// "TypeCycling:Sliver:3") -- neither a face index nor an SVar name anchors
	// it, since a granted keyword lives in no face's SVar table. pcAbility
	// re-derives the identical SA from the line at every read site (a pure
	// function of the string, so replay-safe), and payCast's ability branch
	// mints through events.KeywordAbilityPush, whose Counter carries the same
	// line. Empty means not a keyword grant. Plain data, so Clone carries it.
	grantKeyword string `clone:"deep"`

	// gainedFrom / gainedIdx anchor a HAS-ALL-ABILITIES-OF activation
	// (Forge's GainsAbilitiesOf$, rules/activation's gained branch): the body
	// is a compiled SA on a FOREIGN card's face, so gainedFrom is that card's
	// object id and gainedIdx the index of the SA in its Face().Abilities.
	// Both are plain values carried through the same shallow Clone as the
	// scalars above; a zero gainedFrom means no gained activation.
	gainedFrom state.ObjID `clone:"deep"`
	gainedIdx  int         `clone:"deep"`

	// offSorcery (kw:MayFlashSac) is the CR 702.8 rider's condition captured
	// at beginCast, before CR 601.2a pushes the spell: true when this cast was
	// made at a time a sorcery could NOT have been cast. payCast stamps
	// state.FlagMayFlashSac onto the pay-time CastInfo only when this is true
	// AND the face carries the keyword, so a sorcery-timed cast of the same
	// card registers no cleanup sacrifice. Plain data, so Clone carries it.
	offSorcery bool `clone:"deep"`

	// giftDone / giftPromise / giftTo are the CR 702.168 Gift election
	// (Bloomburrow): the caster's optional promise of a gift to an opponent,
	// announced as a free cast-time choice (giftAsk) and folded onto the
	// stack object as an events.GiftPromise by pushCast so the target ask and
	// resolution read one event-backed home. giftDone marks the one ask
	// already posed (the forageDone/replicateDone shape), giftPromise is the
	// answer (false = declined, the plain-cast direction) and giftTo names
	// the promised opponent. Plain data, so Clone carries them.
	giftDone    bool           `clone:"deep"`
	giftPromise bool           `clone:"deep"`
	giftTo      state.PlayerID `clone:"deep"`

	cost Cost `clone:"deep"`

	// mayPlayIgnore is the may-play grant's MayPlayIgnoreColor$ rider,
	// recorded at beginCast from the offer gate that proved it (the card was
	// still in the granted zone); the mana window and the payment keep the
	// grant through it, since after the push (CR 601.2a) the card is on the
	// stack and a zone re-derivation would wrongly drop the grant.
	mayPlayIgnore bool `clone:"deep"`
	// mayPlayIgnoreType is the grant's MayPlayIgnoreType$ rider (Rakdos, the
	// Muscle): the same recorded-at-beginCast discipline as mayPlayIgnore,
	// threading "mana of any type" through the same window and payment.
	mayPlayIgnoreType bool `clone:"deep"`
	// mayPlayRemembered records, at beginCast, the remembered-object bindings
	// of the ManaConvert continuous effects that matched this card WHILE it
	// was still in the granted zone, keyed by effect source. A may-play
	// grant's own ForgetOnMoved$ clears that binding the instant the card is
	// put on the stack (CR 601.2a), but the paired ManaConvert's
	// ValidCard$ Card.IsRemembered must keep resolving through the cost
	// payment (CR 601.2h) -- the same recorded-at-beginCast discipline as
	// mayPlayIgnore, and for the same reason. Nil for every ordinary cast.
	mayPlayRemembered map[state.ObjID][]state.ObjID `clone:"deep"`
	// mayPlayPerm is the MayPlayText$-typed permission a may-play cast
	// consumes (rules/mayplay.go's mayPlayPermKey). Empty for an untyped
	// grant. beginCast copies it off the option, the "mayplay" cost case
	// prices exactly that static's free/RaiseCost$ riders, and payCast stamps
	// it on the pay-time CastInfo (a "perm=<key>" token) so
	// mayPlayTypedLimitReached can attribute the play to its static.
	mayPlayPerm string `clone:"deep"`
	// mayPlayHosts records, at beginCast, the hosts of the may-play
	// permissions that covered this "mayplay" cast's card while it still sat
	// in the granted zone (mayPlayHostsCovering). The cost chain's
	// MayPlaySource reads (castRidesMayPlayOf) consult it after CR 601.2a's
	// push, when the permission no longer covers the card on the stack.
	// mayPlayHostsSet marks a captured (possibly empty) record.
	mayPlayHosts    []state.ObjID `clone:"deep"`
	mayPlayHostsSet bool          `clone:"deep"`
	// costRemembered records, at beginCast, the captured Remembered set of
	// every Effect-delivered cost-modifier static whose set held this card
	// (costRememberedCapture): the "a spell cast this way" raise's
	// Card.IsRemembered must keep naming the card after CR 601.2a moves it,
	// although the Effect's ForgetOnMoved$ drops it from the live set at that
	// move -- the mayPlayRemembered discipline, for cost statics.
	costRemembered []costRememberedEntry `clone:"deep"`

	// replaceGraveyard is the Play SA's ReplaceGraveyard$ Exile rider
	// (task replplay1): the played spell must not rest in the graveyard —
	// payCast stamps state.FlagReplaceGraveyard onto the pay-time CastInfo
	// and spellRestZone/spellFizzleZone read it. Per-SA provenance, so it
	// rides pendingCast rather than the shared "play" mode.
	replaceGraveyard bool `clone:"deep"`

	// faceDown marks the morph family's face-down cast (CR 702.37a
	// Morph, 702.168a Megamorph, 702.169a Disguise): the {3} cast puts a
	// face-down spell on the stack. It gates the cast-flow stages the
	// face-down spell must skip (CR 708.4: no targets, no printed spell
	// abilities), carries the PutOnStack's face-down entry marker
	// (pushCast), and the pay-time CastInfo stamps the family flag
	// (modeFlags) the resolution reader and a later turn-face-up action
	// read. Plain data, so Clone carries it.
	faceDown bool `clone:"deep"`

	x     int32 `clone:"deep"`
	xDone bool  `clone:"deep"`
	// announceX is the alternative cost's Announce$ variable (the Shoal
	// cycle's "X"): the X this cast announces is NOT a mana X — it is bound
	// by the exile settlement's cmcEQX filter (xAsk's announce arm offers
	// exactly the mana values some exilable card matches at; exAsk binds the
	// announced value into that filter). Empty on every ordinary cast.
	announceX string `clone:"deep"`

	// named / namedN / namedDone carry a NAMED announcement (Forge's
	// Announce$ other than X) that a RaiseCost additional-cost part counts
	// by (CostPart.Dyn "@<Name>"): the March cycle's "exile any number of
	// red cards from your hand" (Announce$ Exiled) and Explosive
	// Singularity's "tap any number of untapped creatures" (Announce$
	// Tapped). namedAnnounceAsk poses it before any other cost stage, the
	// part then pays exactly namedN, and the paired Relative$ ReduceCost
	// reads the value through the SVar the name spells (namedAnnounceSVars).
	named     string `clone:"deep"`
	namedN    int32  `clone:"deep"`
	namedDone bool   `clone:"deep"`

	// suspendTimeX makes the chosen cast X also set the number of TIME
	// counters; suspendMinX is Forge's XMin<N> lower bound.
	suspendTimeX bool  `clone:"deep"`
	suspendMinX  int32 `clone:"deep"`

	delve     []state.ObjID `clone:"deep"`
	delveDone bool          `clone:"deep"`

	// replicateParam is the raw Replicate keyword parameter (CR 702.55a) a
	// "replicated" cast re-parses at ask and answer time; replicateTimes is
	// the answered payment count (0 = declined: no flag, a plain cast) and
	// replicateDone marks the one ask already posed. Plain data, so Clone
	// copies it like x/delve/sacs.
	replicateParam string `clone:"deep"`
	replicateSet   bool   `clone:"deep"`
	replicateTimes int32  `clone:"deep"`
	replicateDone  bool   `clone:"deep"`

	// squadParam is the raw Squad keyword parameter (CR 702.66) a
	// "squadded" cast re-parses at ask and answer time; squadTimes is the
	// answered payment count (0 = declined: no flag, a plain cast) and
	// squadDone marks the one ask already posed. The replicate fields' exact
	// shape; plain data, so Clone copies it.
	squadParam string `clone:"deep"`
	squadSet   bool   `clone:"deep"`
	squadTimes int32  `clone:"deep"`
	squadDone  bool   `clone:"deep"`

	// multikickParam is the raw Multikicker keyword parameter (CR 702.43) a
	// "multikicked" cast re-parses at ask and answer time; multikickTimes is
	// the answered payment count (0 = declined: no flag, a plain cast) and
	// multikickDone marks the one ask already posed. Same shape as the
	// replicate fields above; plain data, so Clone copies it.
	multikickParam string `clone:"deep"`
	multikickSet   bool   `clone:"deep"`
	multikickTimes int32  `clone:"deep"`
	multikickDone  bool   `clone:"deep"`

	// escalateParam is the raw Escalate keyword parameter (the modal
	// additional cost "pay this for each mode chosen beyond the first") a
	// Charm cast re-parses once the CR 601.2b mode answer is in; escalateDone
	// marks the one fold already applied. There is no separate cast option --
	// unlike Kicker, Escalate rides the mode count of the plain cast. Plain
	// data, so Clone copies it like the replicate/multikick fields above.
	escalateParam string `clone:"deep"`
	escalateSet   bool   `clone:"deep"`
	escalateDone  bool   `clone:"deep"`

	// striveParam is the raw Strive keyword parameter (CR 702.52, "this
	// spell costs <cost> more for each target beyond the first"). Strive
	// rides the plain cast (no separate option): the payment count is the
	// chosen target count minus one, known only after CR 601.2c, so
	// repriceForTargets folds it into pc.cost there and striveUnits tracks
	// how many payments are already priced (the ownReduce delta shape),
	// making the fold idempotent across repriceForTargets' re-entries.
	// Plain data, so Clone copies it like the escalate fields above.
	striveParam string `clone:"deep"`
	striveSet   bool   `clone:"deep"`
	striveUnits int32  `clone:"deep"`

	// Mutate (CR 702.140b): mutateTop is the answered over/under placement
	// choice and mutatePlaceDone marks the one ask already posed. Plain data,
	// so Clone copies them like the replicate/multikick fields above.
	mutateTop       bool `clone:"deep"`
	mutatePlaceDone bool `clone:"deep"`
	// Conspire (CR 702.78a) is a param-less keyword: the "conspired" cast
	// mode marks the intent to tap two untapped creatures that share a colour
	// with the spell, conspireDone marks the one election already posed, and
	// conspirePaid records that the election's two taps were actually
	// recorded (the payCast provenance gate: a board that changed under the
	// proposal, or a declined/plain cast, leaves it false and the cast stays
	// byte-identical). Plain data, so Clone copies it.
	conspireSet  bool `clone:"deep"`
	conspireDone bool `clone:"deep"`
	conspirePaid bool `clone:"deep"`

	// Casualty's optional additional cost is a single power-qualified sacrifice.
	// The chosen object is settled with the other sacrifice costs at payment.
	// casualtyDone marks the optional-sacrifice election already posed for
	// whichever rules/optional_sacrifice.go row the cast mode elects
	// (Casualty or Bargain; a cast has one mode, so one flag suffices).
	casualtyN    int32 `clone:"deep"`
	casualtyDone bool  `clone:"deep"`
	casualtyPaid bool  `clone:"deep"`
	// Casualty:X (the variable form, Ob Nixilis, the Adversary): the amount
	// is the sacrificed creature's power (CR 702.249a), so no threshold
	// gates the election (casualtyN reads 0, every creature qualifies) and
	// casualtySac/casualtyX carry the chosen creature and the power read
	// live at payment. Plain data, so Clone copies it like casualtyN.
	casualtyVariable bool        `clone:"deep"`
	casualtySac      state.ObjID `clone:"deep"`
	casualtyX        int32       `clone:"deep"`

	// converge (task converge1) is CR 107.4f-family's count of distinct
	// colours (WUBRG) of mana actually spent to cast this spell, captured at
	// payment from the full spent delta payManaCastSpent returns. convergeOn
	// is the heads-safety two-arm gate (faceWantsConverge OR a battlefield
	// reader naming TriggeredCard$Converge): the pay-time CastInfo is emitted
	// ONLY for a face carrying a Count$Converge SVar, or when some alive
	// player's battlefield permanent's trigger reads another spell's cast
	// colours, so no game that casts neither changes an event. Plain data, so
	// Clone copies it like replicateTimes.
	convergeOn bool  `clone:"deep"`
	converge   int32 `clone:"deep"`

	// manaSpentOn/manaSpent (task castprov1) capture the TOTAL mana the
	// cast's payment actually spent, from the same full spent delta
	// payManaCastSpent returns (the pips summed over every slot).
	// manaSpentOn is the heads-safety gate (faceWantsCastSpend): the pay-time
	// CastInfo is emitted ONLY for a face whose SVar table reads the
	// Count$CastTotalManaSpent head, so no game that casts no such card
	// changes an event. manaSpentSnow (task castfilter1) is the SNOW-unit part
	// of that same payment (CR 107.4h), the filtered
	// Count$CastTotalManaSpent Snow form the six Snow carriers read;
	// manaSpentTreasure/Cave/Desert (task castfilter2) are the TYPED parts of
	// that same payment, the filtered Treasure/Cave/Desert forms Marut, Bat
	// Colony and Cataclysmic Prospecting read. They ride the same emission
	// gate, and each tag's total rides its OWN trailing CastInfo, so no two
	// totals ever share an event. Plain data, so Clone copies it like
	// converge.
	manaSpentOn       bool  `clone:"deep"`
	manaSpent         int32 `clone:"deep"`
	manaSpentSnow     int32 `clone:"deep"`
	manaSpentTreasure int32 `clone:"deep"`
	manaSpentCave     int32 `clone:"deep"`
	manaSpentDesert   int32 `clone:"deep"`
	manaSpentArtifact int32 `clone:"deep"`

	// manaColorSpentOn/manaColorSpent (Adamant, CR 702.5) capture the
	// PER-COLOUR parts of the same payment (the six state.Mana slots
	// W,U,B,R,G,C), from the same full spent delta payManaCastSpent returns.
	// manaColorSpentOn is the heads-safety gate (faceWantsManaColorSpent):
	// the pay-time CastInfo is emitted ONLY for a face whose SVar table or
	// ability text reads a Count$Adamant head, so no game that casts no such
	// card changes an event. Plain data, so Clone copies it like converge.
	manaColorSpentOn bool       `clone:"deep"`
	manaColorSpent   state.Mana `clone:"deep"`

	// PaidCost (rules/pay) is the cast's payment slice: the objects each
	// non-mana cost part was paid with (Sacs, Discards, Exiles, Reveals with
	// RevealHandArm and RevealedEmptyHand, Beholds, Taps, Blights), recorded
	// by the ask stages and settled at payCast. The payment layer reads it
	// without the rest of the pending cast (spec section 9.1).
	pay.PaidCost `clone:"deep"`

	// addsCounterGrants (task opalp) captures, right after payment, the
	// AddsCounters$ rider grants the cast earned: the consumed batches'
	// producing-ability rider snapshots and spent unit counts that
	// emitRestrictedManaSpend accumulated in e.manaSpentAddsCounters. The
	// pay-time CastInfo's FlagAddsCounters Text payload carries them onto the
	// cast object, where the entry-counter plan uses them verbatim (never
	// re-reading a source face). Empty for every cast that spent no
	// rider-bearing mana, so unrelated casts stay byte-identical. Plain data,
	// so Clone carries it like converge.
	addsCounterGrants []state.ManaAddsCounterGrant `clone:"share"`

	// emerge / emergeDone mark an Emerge cast (CR 702.118a): beginCast's
	// "emerged" arm sets emerge and composes the printed K:Emerge cost with
	// the mandatory Sac<1/Creature> part; sacAsk then folds the chosen
	// creature's mana value out of pc.cost exactly once, guarded by
	// emergeDone so a resumed mana window cannot subtract twice. Plain data,
	// so Clone carries them like sacs/sacPart.
	emerge     bool        `clone:"deep"`
	emergeDone bool        `clone:"deep"`
	emergeSac  state.ObjID `clone:"deep"`

	// suspendCastClear marks a suspend cast's time-counter clear as owed.
	suspendCastClear bool `clone:"deep"`

	// mods / taxGeneric carry the CR 601.2f cost composition: the evaluated
	// RaiseCost/ReduceCost modifiers (computed in beginCast for a spell,
	// beginActivation for an ability — Color$ reductions, MinMana$ floors and
	// the SetCost floor included) and the CR 903.8 commander tax. They are
	// applied to the mana cost only AFTER {X} is folded into Generic
	// (manaToPay), so an {X} reduction is not lost and the tax (an additional
	// cost) is never reduced -- increases before reductions, per 601.2f.
	mods       costMods `clone:"deep,copy=cloneCastMods"`
	taxGeneric int32    `clone:"deep"`

	// ownReduce is the amount the ability's own ReduceCost$ parameter folded
	// into pc.cost at beginActivation (nil targets there: CR 601.2c has not
	// run). repriceForTargets recomputes it target-aware and net-adjusts
	// cost.Generic by the delta, so the folded amount is never applied twice
	// and the net form is idempotent across a mana-window resume.
	ownReduce int32 `clone:"deep"`

	// modesDone is set once a modal spell's CR 601.2b mode question has been
	// posed. modeChosen says its answer was recorded during this proposal;
	// preModes is the object's value immediately before that answer, so
	// abortCast can restore it under CR 733.1.
	modesDone  bool     `clone:"deep"`
	modeChosen bool     `clone:"deep"`
	preModes   []string `clone:"deep"`
	// modeCostsDone is set once a Spree/Tiered cast has folded its chosen
	// modes' ModeCost$ into cost, so a re-entry through continueCast cannot
	// charge the per-mode additional cost twice.
	modeCostsDone bool `clone:"deep"`

	// passedTarget is set once the flow has moved past the 601.2c target
	// choice into payCast, so a resume through continueCast (the mana-window
	// re-entry) does not re-ask for targets.
	passedTarget bool `clone:"deep"`

	// targetStage is the CR 702.101b Fuse target stage: 0 asks the front
	// half's targets, 1 the alternate half's. Always 0 for an ordinary cast
	// (and for an ability), so their single target ask is byte-identical.
	targetStage int `clone:"deep"`

	// targets are the chosen cast-time targets while this proposal is live.
	// They are copied from the target decision before payment so a ValidTarget$
	// cost modifier can be recomputed after CR 601.2c and before 601.2h, even
	// for an activated ability whose stack object is not minted until payment.
	targets []state.Target `clone:"deep"`

	// stageTargets records each Fuse target stage's OWN chosen targets
	// (index 0 the front half's, index 1 the alternate half's), so
	// resolveFused hands each half exactly the targets chosen FOR it.
	// Indexed by stage, so a targetless stage the ask loop skipped never
	// misaligns the slices. A Fuse-only field; always empty for every other
	// cast. Published to Engine.fuseTargets at payment.
	stageTargets [][]state.Target `clone:"deep"`
	// charmTargets records one target slice for each distinct target-bearing
	// mode selected by a modal spell. The stack object's ordinary Targets is
	// retained as the flat event-sourced view; this scratch preserves the
	// per-mode bindings for resolution and is rebuilt by the same answer path
	// during replay.
	charmTargets [][]state.Target `clone:"deep"`

	// subAsks / subAns / subStage carry the CAST-TIME pre-ask of the chain's
	// targeting SubAbility$ bodies (task alltargeted1): Forge asks every
	// targeting SA in the root ability's whole sub-ability chain BEFORE
	// cost payment (CR 601.2c), and the answers must (a) feed the
	// AllTargeted$ cost reads through the union and (b) be consumed by the
	// resolution instead of being re-asked mid-resolution. subAsks holds the
	// collected targeting subs in chain order (collected once, lazily, by
	// subTargetAsk); subAns the answers, indexed like subAsks; subStage the
	// next unsettled index. rootOpts keeps the ROOT target answer's options
	// so the ability arm's post-payment recordChosenTargets can run from the
	// sub-answer tail (the ability object does not exist until payCast's
	// AbilityPush). Plain data, so a Clone copies them.
	subAsks      []*cards.SA       `clone:"deep"`
	subAns       [][]state.Target  `clone:"deep"`
	subStage     int               `clone:"deep"`
	subCollected bool              `clone:"deep"`
	rootOpts     []decision.Option `clone:"deep"`
	// targetedFinish is set for the duration of finishTargetedCast's payCast
	// call (see there): the caller, not payCast, dispatches the ability's
	// mana-spent rider.
	targetedFinish bool `clone:"deep"`

	// evidence / evidenceN / evidenceResolved / evidenceSettled carry the
	// CollectEvidence<N>/<NAME> cost component (task alltargeted1): the
	// amount is resolved once the CR 601.2c targets exist (the corpus
	// carrier's body reads AllTargeted$CardManaCost over the whole target
	// union), the ask (evidenceAsk) runs after the sub pre-asks, and the
	// settle (payCast) exiles the chosen cards. evidenceN 0 means "no
	// evidence owed" (a zero-target cast, or an unresolvable body, which
	// degrades to 0 like every count head). Plain data, so a Clone copies
	// it.
	evidence         []state.ObjID `clone:"deep"`
	evidenceN        int32         `clone:"deep"`
	evidenceResolved bool          `clone:"deep"`
	evidenceSettled  bool          `clone:"deep"`

	// stackObj is the id of the object pushCast placed on the stack (the
	// spell card itself, or an activated ability's AbilityPush-minted
	// object). Zero until pushCast runs; handleTarget records the chosen
	// targets onto it, because a zone change clears an object's Targets.
	stackObj state.ObjID `clone:"deep"`

	// pushed is true once the object has reached the stack (post-pushCast).
	// An aborted proposal reverses the push when it is set.
	pushed bool `clone:"deep"`

	// provenanceRepriced is true once the post-push provenance re-price has
	// run for this proposal (castprov3: a provenance-keyed cost static is
	// unresolvable pre-push, so continueCast re-prices pc.mods right after
	// the push; the flag keeps the re-entries — a mana-window resume re-enters
	// continueCast with pushed already true — from gathering the statics
	// again). Plain data, so Clone copies it.
	provenanceRepriced bool `clone:"deep"`

	// preSuppress is the suppressedCast set as it was just before pushCast's
	// PutOnStack, captured so an aborted (reversed) cast can restore it:
	// the push is a state-changing event that emit treats as progress and so
	// clears the held-out no-progress set, but an aborted cast is net no
	// progress, so that set must come back. Nil when no cast push is in
	// flight (an ability, or a spell aborted before the push).
	preSuppress map[state.ObjID]bool `clone:"deep"`

	// faceBefore is non-nil only for a CR 309.4b alternate Room cast or a CR
	// 714 Adventure-face cast (adventure_alt / adventure_recast). The
	// proposal begins with an event-sourced FlipFace so all ordinary cast
	// stages read the chosen door; an aborted proposal flips it back.
	faceBefore *uint8 `clone:"share"`

	// preAborts is the castAborts no-progress count map (engine.go) as it was
	// just before pushCast's PutOnStack, captured and restored for exactly the
	// reason preSuppress is: the push is a state-changing event that emit
	// treats as progress and clears the count, but an aborted cast is net no
	// progress, so the count must come back across the push (F05-2). Nil when
	// no cast push is in flight.
	preAborts map[state.ObjID]int32 `clone:"deep"`

	// proposalTriggers are the [start, end) pendingTriggers index ranges a
	// pushed SPELL proposal's own TargetsChosen events queued (Ward, "becomes
	// the target" and every other matcher of a CR 601.2c target choice). They
	// are recorded by emit and removed by abortCast: CR 733.1 "no abilities
	// trigger ... as a result of an undone action", so a reversed cast must
	// leave nothing on the queue. Without it a bot re-attempting the same
	// unpayable Strive cast queued a fresh Ward/Silverfur Partisan trigger per
	// attempt, and that trigger's push was the state change that cleared the
	// F05-2 no-progress suppression -- an endless cast/reverse cycle.
	// Engine-side scratch rebuilt by replay (the same intents reach the same
	// emits); nil outside a spell proposal with targets.
	proposalTriggers [][2]int `clone:"deep"`

	// altAddParts are the alternative parts of the card's
	// AlternateAdditionalCost keyword ("As an additional cost to cast this
	// spell, sacrifice a creature or pay {3}{B}"): one KChoose over them at
	// cast-announcement time (altAddAsk), and the chosen part's cost folded
	// into cost for the ordinary cost stages to settle. Empty for a card
	// without the keyword; altAddDone marks the one ask already posed.
	altAddParts []string `clone:"share"`
	altAddDone  bool     `clone:"deep"`
	// optionalCost is the selected self-spell OptionalCost additional part.
	optionalCost Cost `clone:"share"`

	// returns / returnPart carry the Return cost parts (Return<N/Spec>
	// tokens: a permanent matching Spec returned to its OWNER's hand) through
	// the same ask stage / commit shape the exile parts use.
	returns    []state.ObjID `clone:"deep"`
	returnPart int           `clone:"deep"`

	// moveGraves / moveGravePart carry the ExiledMoveToGrave cost parts
	// (cards matching Spec moved from exile to their OWNER's graveyard --
	// the Eldrazi processor family and Shelob, Dread Weaver's {2}{B}
	// ability) through the same ask stage / commit shape the exile parts
	// use. Nothing moves until payCast, so an abort cannot leave a partially
	// paid graveyard move behind.
	moveGraves    []state.ObjID `clone:"deep"`
	moveGravePart int           `clone:"deep"`

	// putToLibs / putToLibPart carry the PutToLib cost parts
	// (PutCardToLibFrom<Zone><N/Pos/Spec> tokens: cards matching Spec moved
	// from the payer's Hand/Graveyard/Battlefield to the top or bottom of
	// their owner's library) through the same ask stage / commit shape the
	// Return parts use. Nothing moves until payCast, so an abort cannot leave
	// a partially paid library placement behind.
	putToLibs    []state.ObjID `clone:"deep"`
	putToLibPart int           `clone:"deep"`

	forageDone bool `clone:"deep"`

	// ninjutsuDefender is the defender (CR 702.49b: the player, planeswalker
	// or battle the returned creature was attacking) captured when a
	// K:Ninjutsu activation paid its Return cost. ninjutsuHasDefender
	// discriminates the capture: seat 0 is a legal defending player, so
	// ninjutsuDefender == 0 on its own cannot mean "not captured" (the same
	// hazard documented at combat.go's mustAttackRequired). It rides the
	// AbilityPush event's IDs, which events.Apply folds into the minted
	// ability's Remembered, and rules/stack.go re-binds it to the resolving
	// Ctx's DefendingPlayer so effects/zone.go's Attacking$ True rider places
	// the permanent tapped and attacking that same defender. Plain data, so a
	// Clone copies it.
	ninjutsuDefender state.PlayerID `clone:"deep"`
	// ninjutsuDefenderObject is the planeswalker or battle the returned
	// creature was attacking (CR 702.49b's non-player defender), captured
	// beside ninjutsuDefender. Zero when the returned creature attacked a
	// player. It rides the AbilityPush event's IDs as a real object id,
	// which events.Apply's rememberedFrom decodes to a {Obj} target, and
	// rules/stack.go re-binds it to the resolving Ctx's DefendingBattle so
	// effects/zone.go's Attacking$ True rider places the permanent attacking
	// that same object. Plain data, so a Clone copies it.
	ninjutsuDefenderObject state.ObjID `clone:"deep"`
	ninjutsuHasDefender    bool        `clone:"deep"`

	// sneakDefender is the defender (CR 702.190b: the player, planeswalker or
	// battle the returned creature was attacking) captured when a K:Sneak
	// cast paid its Return cost. sneakHasDefender discriminates the capture
	// (seat 0 is a legal defending player, so sneakDefender == 0 on its own
	// cannot mean "not captured" -- the same hazard documented for
	// ninjutsuDefender). It rides a Choose "remembered" event onto the
	// spell's Remembered (events/apply.go), which the stack->battlefield move
	// preserves, so rules/altcast.go's entry hook can place the permanent
	// tapped and attacking that same defender. Plain data, so a Clone copies
	// it.
	sneakDefender       state.PlayerID `clone:"deep"`
	sneakDefenderObject state.ObjID    `clone:"deep"`
	sneakHasDefender    bool           `clone:"deep"`
}

// subCounterPay is one counter removed to pay a SubCounter cost part
// (pay.SubCounterPay).
type subCounterPay = pay.SubCounterPay

// etbChoice is one "as this enters" choice, pre-computed: its kind
// ("name"/"/type"/"number", matching the Choose event's Counter) and the
// option list that will be offered, captured once at the start of the cast
// flow so the decision and the recorded choice always agree.
type etbChoice struct {
	kind    string
	options []decision.Option
	// prompt, when set, is the client prompt's suffix in place of
	// etbChoicePrompt(kind) (the CR 303.4f Aura choice carries its own).
	prompt string
}
