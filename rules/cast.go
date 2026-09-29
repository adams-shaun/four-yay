// cast.go is the cast-flow state machine: beginCast starts it from a chosen
// "cast" priority option, continueCast runs its stages (X, Delve, each Sac
// and Discard part) in order, asking a KChoose (chooseCast) decision for any stage that
// needs one, and commitCast pays and puts the spell on the stack once every
// stage is settled. Kicker, Surge, Flashback and Delve are registered here
// as the primitives they are (rules/legal.go builds the options that choose
// among them; this file resolves whichever one was picked into a Cost and
// drives it to the stack).
package rules

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
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
	player  state.PlayerID
	card    state.ObjID
	from    state.Zone
	mode    string // "", "kicked", "surged", "flashback", "miracle", and the alternative-cost modes this file offers
	ability int    // -1 for a spell (Task 10 uses >= 0)
	// abilityMerged is the pile position of the activated ability pc.ability
	// names: 0 for the top face, i+1 for the i-th card merged beneath it
	// (CR 702.140d). It selects the SVar table a computed cost/limit resolves
	// against, so an under-card ability reads its OWN table, never the pile
	// top's. Zero for a spell and for every top-face ability.
	abilityMerged int

	// payment is a privately-owned V1 witness selected at priority.  It stays
	// on the ordinary cast continuation through target choices, then drives
	// only CR 601.2g mana activations.  All resulting state changes remain in
	// the established mana activation and payment paths.
	payment         *plannedCastPayment
	paymentNext     int
	paymentFallback *decision.PaymentFallback
	// announced marks a cast begun by Intent.Announce (announce-then-pay
	// spec, rules/announce_pay.go): its CR 601.2g window is the announced
	// one. windowTaps records the activations made from that window, for
	// Undo last tap and Cancel cast. Both are zero for every other cast.
	announced  bool
	windowTaps []windowTap

	// grantSource / grantSVar (task grantcost1) anchor a GRANTED activation
	// (rules/speed.go's beginGrantedActivation, reached from the max-speed
	// "granted" option and beginActivation's SVar branch): the body is the
	// SVar the AddAbility$ grant names, resolved off the GRANTOR's face, and
	// grantSource is the resolved grantor object (== card for a self-grant,
	// the offer's GrantSource fallback already applied). Empty SVar means a
	// printed/spell proposal; pc.ability stays -1 for a granted one. Both are
	// plain values, so Clone's shallow copy carries them like every scalar
	// above.
	grantSource state.ObjID
	grantSVar   string

	// grantKeyword anchors a KEYWORD-GRANTED activation (CR 613.1f, the
	// layer-6 AddKeyword$ Cycling/TypeCycling route): the body is synthesized
	// from the derived keyword line the option carries ("Cycling:1 U",
	// "TypeCycling:Sliver:3") -- neither a face index nor an SVar name anchors
	// it, since a granted keyword lives in no face's SVar table. pcAbility
	// re-derives the identical SA from the line at every read site (a pure
	// function of the string, so replay-safe), and payCast's ability branch
	// mints through events.KeywordAbilityPush, whose Counter carries the same
	// line. Empty means not a keyword grant. Plain data, so Clone carries it.
	grantKeyword string

	// gainedFrom / gainedIdx anchor a HAS-ALL-ABILITIES-OF activation
	// (Forge's GainsAbilitiesOf$, rules/activation's gained branch): the body
	// is a compiled SA on a FOREIGN card's face, so gainedFrom is that card's
	// object id and gainedIdx the index of the SA in its Face().Abilities.
	// Both are plain values carried through the same shallow Clone as the
	// scalars above; a zero gainedFrom means no gained activation.
	gainedFrom state.ObjID
	gainedIdx  int

	// offSorcery (kw:MayFlashSac) is the CR 702.8 rider's condition captured
	// at beginCast, before CR 601.2a pushes the spell: true when this cast was
	// made at a time a sorcery could NOT have been cast. payCast stamps
	// state.FlagMayFlashSac onto the pay-time CastInfo only when this is true
	// AND the face carries the keyword, so a sorcery-timed cast of the same
	// card registers no cleanup sacrifice. Plain data, so Clone carries it.
	offSorcery bool

	// giftDone / giftPromise / giftTo are the CR 702.168 Gift election
	// (Bloomburrow): the caster's optional promise of a gift to an opponent,
	// announced as a free cast-time choice (giftAsk) and folded onto the
	// stack object as an events.GiftPromise by pushCast so the target ask and
	// resolution read one event-backed home. giftDone marks the one ask
	// already posed (the forageDone/replicateDone shape), giftPromise is the
	// answer (false = declined, the plain-cast direction) and giftTo names
	// the promised opponent. Plain data, so Clone carries them.
	giftDone    bool
	giftPromise bool
	giftTo      state.PlayerID

	cost Cost

	// mayPlayIgnore is the may-play grant's MayPlayIgnoreColor$ rider,
	// recorded at beginCast from the offer gate that proved it (the card was
	// still in the granted zone); the mana window and the payment keep the
	// grant through it, since after the push (CR 601.2a) the card is on the
	// stack and a zone re-derivation would wrongly drop the grant.
	mayPlayIgnore bool
	// mayPlayIgnoreType is the grant's MayPlayIgnoreType$ rider (Rakdos, the
	// Muscle): the same recorded-at-beginCast discipline as mayPlayIgnore,
	// threading "mana of any type" through the same window and payment.
	mayPlayIgnoreType bool
	// mayPlayRemembered records, at beginCast, the remembered-object bindings
	// of the ManaConvert continuous effects that matched this card WHILE it
	// was still in the granted zone, keyed by effect source. A may-play
	// grant's own ForgetOnMoved$ clears that binding the instant the card is
	// put on the stack (CR 601.2a), but the paired ManaConvert's
	// ValidCard$ Card.IsRemembered must keep resolving through the cost
	// payment (CR 601.2h) -- the same recorded-at-beginCast discipline as
	// mayPlayIgnore, and for the same reason. Nil for every ordinary cast.
	mayPlayRemembered map[state.ObjID][]state.ObjID
	// mayPlayPerm is the MayPlayText$-typed permission a may-play cast
	// consumes (rules/mayplay.go's mayPlayPermKey). Empty for an untyped
	// grant. beginCast copies it off the option, the "mayplay" cost case
	// prices exactly that static's free/RaiseCost$ riders, and payCast stamps
	// it on the pay-time CastInfo (a "perm=<key>" token) so
	// mayPlayTypedLimitReached can attribute the play to its static.
	mayPlayPerm string
	// mayPlayHosts records, at beginCast, the hosts of the may-play
	// permissions that covered this "mayplay" cast's card while it still sat
	// in the granted zone (mayPlayHostsCovering). The cost chain's
	// MayPlaySource reads (castRidesMayPlayOf) consult it after CR 601.2a's
	// push, when the permission no longer covers the card on the stack.
	// mayPlayHostsSet marks a captured (possibly empty) record.
	mayPlayHosts    []state.ObjID
	mayPlayHostsSet bool
	// costRemembered records, at beginCast, the captured Remembered set of
	// every Effect-delivered cost-modifier static whose set held this card
	// (costRememberedCapture): the "a spell cast this way" raise's
	// Card.IsRemembered must keep naming the card after CR 601.2a moves it,
	// although the Effect's ForgetOnMoved$ drops it from the live set at that
	// move -- the mayPlayRemembered discipline, for cost statics.
	costRemembered []costRememberedEntry

	// replaceGraveyard is the Play SA's ReplaceGraveyard$ Exile rider
	// (task replplay1): the played spell must not rest in the graveyard —
	// payCast stamps state.FlagReplaceGraveyard onto the pay-time CastInfo
	// and spellRestZone/spellFizzleZone read it. Per-SA provenance, so it
	// rides pendingCast rather than the shared "play" mode.
	replaceGraveyard bool

	// faceDown marks the morph family's face-down cast (CR 702.37a
	// Morph, 702.168a Megamorph, 702.169a Disguise): the {3} cast puts a
	// face-down spell on the stack. It gates the cast-flow stages the
	// face-down spell must skip (CR 708.4: no targets, no printed spell
	// abilities), carries the PutOnStack's face-down entry marker
	// (pushCast), and the pay-time CastInfo stamps the family flag
	// (modeFlags) the resolution reader and a later turn-face-up action
	// read. Plain data, so Clone carries it.
	faceDown bool

	x     int32
	xDone bool
	// announceX is the alternative cost's Announce$ variable (the Shoal
	// cycle's "X"): the X this cast announces is NOT a mana X — it is bound
	// by the exile settlement's cmcEQX filter (xAsk's announce arm offers
	// exactly the mana values some exilable card matches at; exAsk binds the
	// announced value into that filter). Empty on every ordinary cast.
	announceX string

	// named / namedN / namedDone carry a NAMED announcement (Forge's
	// Announce$ other than X) that a RaiseCost additional-cost part counts
	// by (CostPart.Dyn "@<Name>"): the March cycle's "exile any number of
	// red cards from your hand" (Announce$ Exiled) and Explosive
	// Singularity's "tap any number of untapped creatures" (Announce$
	// Tapped). namedAnnounceAsk poses it before any other cost stage, the
	// part then pays exactly namedN, and the paired Relative$ ReduceCost
	// reads the value through the SVar the name spells (namedAnnounceSVars).
	named     string
	namedN    int32
	namedDone bool

	// suspendTimeX makes the chosen cast X also set the number of TIME
	// counters; suspendMinX is Forge's XMin<N> lower bound.
	suspendTimeX bool
	suspendMinX  int32

	delve     []state.ObjID
	delveDone bool

	// replicateParam is the raw Replicate keyword parameter (CR 702.55a) a
	// "replicated" cast re-parses at ask and answer time; replicateTimes is
	// the answered payment count (0 = declined: no flag, a plain cast) and
	// replicateDone marks the one ask already posed. Plain data, so Clone
	// copies it like x/delve/sacs.
	replicateParam string
	replicateSet   bool
	replicateTimes int32
	replicateDone  bool

	// squadParam is the raw Squad keyword parameter (CR 702.66) a
	// "squadded" cast re-parses at ask and answer time; squadTimes is the
	// answered payment count (0 = declined: no flag, a plain cast) and
	// squadDone marks the one ask already posed. The replicate fields' exact
	// shape; plain data, so Clone copies it.
	squadParam string
	squadSet   bool
	squadTimes int32
	squadDone  bool

	// multikickParam is the raw Multikicker keyword parameter (CR 702.43) a
	// "multikicked" cast re-parses at ask and answer time; multikickTimes is
	// the answered payment count (0 = declined: no flag, a plain cast) and
	// multikickDone marks the one ask already posed. Same shape as the
	// replicate fields above; plain data, so Clone copies it.
	multikickParam string
	multikickSet   bool
	multikickTimes int32
	multikickDone  bool

	// escalateParam is the raw Escalate keyword parameter (the modal
	// additional cost "pay this for each mode chosen beyond the first") a
	// Charm cast re-parses once the CR 601.2b mode answer is in; escalateDone
	// marks the one fold already applied. There is no separate cast option --
	// unlike Kicker, Escalate rides the mode count of the plain cast. Plain
	// data, so Clone copies it like the replicate/multikick fields above.
	escalateParam string
	escalateSet   bool
	escalateDone  bool

	// striveParam is the raw Strive keyword parameter (CR 702.52, "this
	// spell costs <cost> more for each target beyond the first"). Strive
	// rides the plain cast (no separate option): the payment count is the
	// chosen target count minus one, known only after CR 601.2c, so
	// repriceForTargets folds it into pc.cost there and striveUnits tracks
	// how many payments are already priced (the ownReduce delta shape),
	// making the fold idempotent across repriceForTargets' re-entries.
	// Plain data, so Clone copies it like the escalate fields above.
	striveParam string
	striveSet   bool
	striveUnits int32

	// Mutate (CR 702.140b): mutateTop is the answered over/under placement
	// choice and mutatePlaceDone marks the one ask already posed. Plain data,
	// so Clone copies them like the replicate/multikick fields above.
	mutateTop       bool
	mutatePlaceDone bool
	// Conspire (CR 702.78a) is a param-less keyword: the "conspired" cast
	// mode marks the intent to tap two untapped creatures that share a colour
	// with the spell, conspireDone marks the one election already posed, and
	// conspirePaid records that the election's two taps were actually
	// recorded (the payCast provenance gate: a board that changed under the
	// proposal, or a declined/plain cast, leaves it false and the cast stays
	// byte-identical). Plain data, so Clone copies it.
	conspireSet  bool
	conspireDone bool
	conspirePaid bool

	// Casualty's optional additional cost is a single power-qualified sacrifice.
	// The chosen object is settled with the other sacrifice costs at payment.
	casualtyN    int32
	casualtyDone bool
	casualtyPaid bool
	// Casualty:X (the variable form, Ob Nixilis, the Adversary): the amount
	// is the sacrificed creature's power (CR 702.249a), so no threshold
	// gates the election (casualtyN reads 0, every creature qualifies) and
	// casualtySac/casualtyX carry the chosen creature and the power read
	// live at payment. Plain data, so Clone copies it like casualtyN.
	casualtyVariable bool
	casualtySac      state.ObjID
	casualtyX        int32

	// converge (task converge1) is CR 107.4f-family's count of distinct
	// colours (WUBRG) of mana actually spent to cast this spell, captured at
	// payment from the full spent delta payManaCastSpent returns. convergeOn
	// is the heads-safety two-arm gate (faceWantsConverge OR a battlefield
	// reader naming TriggeredCard$Converge): the pay-time CastInfo is emitted
	// ONLY for a face carrying a Count$Converge SVar, or when some alive
	// player's battlefield permanent's trigger reads another spell's cast
	// colours, so no game that casts neither changes an event. Plain data, so
	// Clone copies it like replicateTimes.
	convergeOn bool
	converge   int32

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
	manaSpentOn       bool
	manaSpent         int32
	manaSpentSnow     int32
	manaSpentTreasure int32
	manaSpentCave     int32
	manaSpentDesert   int32
	manaSpentArtifact int32

	// addsCounterGrants (task opalp) captures, right after payment, the
	// AddsCounters$ rider grants the cast earned: the consumed batches'
	// producing-ability rider snapshots and spent unit counts that
	// emitRestrictedManaSpend accumulated in e.manaSpentAddsCounters. The
	// pay-time CastInfo's FlagAddsCounters Text payload carries them onto the
	// cast object, where the entry-counter plan uses them verbatim (never
	// re-reading a source face). Empty for every cast that spent no
	// rider-bearing mana, so unrelated casts stay byte-identical. Plain data,
	// so Clone carries it like converge.
	addsCounterGrants []state.ManaAddsCounterGrant

	sacs    []state.ObjID
	sacPart int
	sacPaid int

	// emerge / emergeDone mark an Emerge cast (CR 702.118a): beginCast's
	// "emerged" arm sets emerge and composes the printed K:Emerge cost with
	// the mandatory Sac<1/Creature> part; sacAsk then folds the chosen
	// creature's mana value out of pc.cost exactly once, guarded by
	// emergeDone so a resumed mana window cannot subtract twice. Plain data,
	// so Clone carries them like sacs/sacPart.
	emerge     bool
	emergeDone bool
	emergeSac  state.ObjID

	discards    []state.ObjID
	discardPart int

	// subCounterPays records the counter-removal picks of every SubCounter
	// part, each entry tagged with the part index it belongs to. A
	// fixed-kind filtered part (SubCounter<N/Kind/Target>) records ONE entry
	// carrying the object it removes from, with an empty Kind; a wildcard
	// "Any" part records ONE entry per counter unit removed, each carrying
	// the object AND the chosen counter kind. Grouping by explicit part index
	// (rather than a positional append) is what keeps a cost mixing a
	// fixed-kind part before a wildcard part from mis-indexing the selected
	// kind. subCounterPart walks the parts in cost order like sacPart. Plain
	// data, so Clone copies it like sacs/discards.
	subCounterPays []subCounterPay
	subCounterPart int

	// convoke is the announced set of creatures paying Convoke or Harmonize.
	// It is chosen after the complete mana cost exists and before the mana
	// ability window; a committed creature is therefore unavailable to make
	// mana as well as being tapped when payment is settled.
	convoke          []convokePayment
	convokeDone      bool
	suspendCastClear bool

	// payIdx / payColor / payLife / payGeneric carry the flexible-pip payment
	// announcement (CR 601.2b/107.4e-f). manaAsk walks the cost's combined
	// announcement-pip list one decision at a time; payIdx is the next
	// unsettled pip, payColor accumulates the coloured spend the announced
	// pips chose, payLife the life a Phyrexian face paid with two life costs,
	// and payGeneric the generic a monocolour hybrid pip paid with its
	// generic face. Plain data, so Clone copies it like x/delve/sacs/discards.
	payIdx     int
	payColor   state.Mana
	payLife    int32
	payGeneric int32

	// mods / taxGeneric carry the CR 601.2f cost composition: the evaluated
	// RaiseCost/ReduceCost modifiers (computed in beginCast for a spell,
	// beginActivation for an ability — Color$ reductions, MinMana$ floors and
	// the SetCost floor included) and the CR 903.8 commander tax. They are
	// applied to the mana cost only AFTER {X} is folded into Generic
	// (manaToPay), so an {X} reduction is not lost and the tax (an additional
	// cost) is never reduced -- increases before reductions, per 601.2f.
	mods       costMods
	taxGeneric int32

	// ownReduce is the amount the ability's own ReduceCost$ parameter folded
	// into pc.cost at beginActivation (nil targets there: CR 601.2c has not
	// run). repriceForTargets recomputes it target-aware and net-adjusts
	// cost.Generic by the delta, so the folded amount is never applied twice
	// and the net form is idempotent across a mana-window resume.
	ownReduce int32

	// windowDone is set when the 601.2g mana window was answered "done", so
	// payCast proceeds straight to payment instead of re-offering it.
	windowDone bool
	// manaConvertDone records the Optional$ ManaConvert election. Before the
	// election, feasibility uses the union so the cast remains offerable; after
	// it, paymentConv uses only the selected optional contribution.
	manaConvertDone bool
	manaConvertUse  bool
	// modesDone is set once a modal spell's CR 601.2b mode question has been
	// posed. modeChosen says its answer was recorded during this proposal;
	// preModes is the object's value immediately before that answer, so
	// abortCast can restore it under CR 733.1.
	modesDone  bool
	modeChosen bool
	preModes   []string
	// modeCostsDone is set once a Spree/Tiered cast has folded its chosen
	// modes' ModeCost$ into cost, so a re-entry through continueCast cannot
	// charge the per-mode additional cost twice.
	modeCostsDone bool

	// passedTarget is set once the flow has moved past the 601.2c target
	// choice into payCast, so a resume through continueCast (the mana-window
	// re-entry) does not re-ask for targets.
	passedTarget bool

	// targetStage is the CR 702.101b Fuse target stage: 0 asks the front
	// half's targets, 1 the alternate half's. Always 0 for an ordinary cast
	// (and for an ability), so their single target ask is byte-identical.
	targetStage int

	// targets are the chosen cast-time targets while this proposal is live.
	// They are copied from the target decision before payment so a ValidTarget$
	// cost modifier can be recomputed after CR 601.2c and before 601.2h, even
	// for an activated ability whose stack object is not minted until payment.
	targets []state.Target

	// stageTargets records each Fuse target stage's OWN chosen targets
	// (index 0 the front half's, index 1 the alternate half's), so
	// resolveFused hands each half exactly the targets chosen FOR it.
	// Indexed by stage, so a targetless stage the ask loop skipped never
	// misaligns the slices. A Fuse-only field; always empty for every other
	// cast. Published to Engine.fuseTargets at payment.
	stageTargets [][]state.Target
	// charmTargets records one target slice for each distinct target-bearing
	// mode selected by a modal spell. The stack object's ordinary Targets is
	// retained as the flat event-sourced view; this scratch preserves the
	// per-mode bindings for resolution and is rebuilt by the same answer path
	// during replay.
	charmTargets [][]state.Target

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
	subAsks      []*cards.SA
	subAns       [][]state.Target
	subStage     int
	subCollected bool
	rootOpts     []decision.Option

	// evidence / evidenceN / evidenceResolved / evidenceSettled carry the
	// CollectEvidence<N>/<NAME> cost component (task alltargeted1): the
	// amount is resolved once the CR 601.2c targets exist (the corpus
	// carrier's body reads AllTargeted$CardManaCost over the whole target
	// union), the ask (evidenceAsk) runs after the sub pre-asks, and the
	// settle (payCast) exiles the chosen cards. evidenceN 0 means "no
	// evidence owed" (a zero-target cast, or an unresolvable body, which
	// degrades to 0 like every count head). Plain data, so a Clone copies
	// it.
	evidence         []state.ObjID
	evidenceN        int32
	evidenceResolved bool
	evidenceSettled  bool

	// stackObj is the id of the object pushCast placed on the stack (the
	// spell card itself, or an activated ability's AbilityPush-minted
	// object). Zero until pushCast runs; handleTarget records the chosen
	// targets onto it, because a zone change clears an object's Targets.
	stackObj state.ObjID

	// pushed is true once the object has reached the stack (post-pushCast).
	// An aborted proposal reverses the push when it is set.
	pushed bool

	// provenanceRepriced is true once the post-push provenance re-price has
	// run for this proposal (castprov3: a provenance-keyed cost static is
	// unresolvable pre-push, so continueCast re-prices pc.mods right after
	// the push; the flag keeps the re-entries — a mana-window resume re-enters
	// continueCast with pushed already true — from gathering the statics
	// again). Plain data, so Clone copies it.
	provenanceRepriced bool

	// preSuppress is the suppressedCast set as it was just before pushCast's
	// PutOnStack, captured so an aborted (reversed) cast can restore it:
	// the push is a state-changing event that emit treats as progress and so
	// clears the held-out no-progress set, but an aborted cast is net no
	// progress, so that set must come back. Nil when no cast push is in
	// flight (an ability, or a spell aborted before the push).
	preSuppress map[state.ObjID]bool

	// faceBefore is non-nil only for a CR 309.4b alternate Room cast or a CR
	// 714 Adventure-face cast (adventure_alt / adventure_recast). The
	// proposal begins with an event-sourced FlipFace so all ordinary cast
	// stages read the chosen door; an aborted proposal flips it back.
	faceBefore *uint8

	// preAborts is the castAborts no-progress count map (engine.go) as it was
	// just before pushCast's PutOnStack, captured and restored for exactly the
	// reason preSuppress is: the push is a state-changing event that emit
	// treats as progress and clears the count, but an aborted cast is net no
	// progress, so the count must come back across the push (F05-2). Nil when
	// no cast push is in flight.
	preAborts map[state.ObjID]int32

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
	proposalTriggers [][2]int

	// altAddParts are the alternative parts of the card's
	// AlternateAdditionalCost keyword ("As an additional cost to cast this
	// spell, sacrifice a creature or pay {3}{B}"): one KChoose over them at
	// cast-announcement time (altAddAsk), and the chosen part's cost folded
	// into cost for the ordinary cost stages to settle. Empty for a card
	// without the keyword; altAddDone marks the one ask already posed.
	altAddParts []string
	altAddDone  bool
	// optionalCost is the selected self-spell OptionalCost additional part.
	optionalCost Cost

	// exiles / exilePart carry the Exile cost parts (ExileFromHand /
	// ExileFromGrave tokens: the evoke alternative cast's Fury/Grief shape,
	// encore's "exile this card from your graveyard") through the same ask
	// stage / commit shape sacAsk and sacs use. Nothing moves until payCast,
	// so an abort cannot leave a partially paid exile on the board.
	exiles    []state.ObjID
	exilePart int

	// returns / returnPart carry the Return cost parts (Return<N/Spec>
	// tokens: a permanent matching Spec returned to its OWNER's hand) through
	// the same ask stage / commit shape the exile parts use.
	returns    []state.ObjID
	returnPart int

	// moveGraves / moveGravePart carry the ExiledMoveToGrave cost parts
	// (cards matching Spec moved from exile to their OWNER's graveyard --
	// the Eldrazi processor family and Shelob, Dread Weaver's {2}{B}
	// ability) through the same ask stage / commit shape the exile parts
	// use. Nothing moves until payCast, so an abort cannot leave a partially
	// paid graveyard move behind.
	moveGraves    []state.ObjID
	moveGravePart int

	// putToLibs / putToLibPart carry the PutToLib cost parts
	// (PutCardToLibFrom<Zone><N/Pos/Spec> tokens: cards matching Spec moved
	// from the payer's Hand/Graveyard/Battlefield to the top or bottom of
	// their owner's library) through the same ask stage / commit shape the
	// Return parts use. Nothing moves until payCast, so an abort cannot leave
	// a partially paid library placement behind.
	putToLibs    []state.ObjID
	putToLibPart int

	reveals, beholds, taps, blights             []state.ObjID
	revealPart, beholdPart, tapPart, blightPart int
	forageDone                                  bool

	// revealOrChoosePart indexes pc.cost.RevealOrChoose through the same ask
	// stage revealCostAsk drives for plain Reveal parts. reveals carries the
	// elected object of each arm (both arms feed the `Revealed$<Property>`
	// refs, Forge's CostReveal owning both), and revealHandArm is parallel to
	// reveals: true for a hand card the REVEAL arm announced, false for a
	// permanent the CHOOSE arm elected off the battlefield. Only the true
	// entries are announced by emitChoiceCosts -- a chosen permanent is a
	// public choice, never a reveal of a hand card. Plain Reveal parts and
	// every other paid card append true (they reveal).
	//
	// revealedEmptyHand records that a whole-hand Reveal part paid with an
	// EMPTY hand (CR 701.20a: revealing a hand with no cards is legal). The
	// empty payment is still a public reveal, so emitChoiceCosts announces it
	// loudly instead of the reveal silently vanishing from the log.
	revealOrChoosePart int
	revealHandArm      []bool
	revealedEmptyHand  bool

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
	ninjutsuDefender    state.PlayerID
	ninjutsuHasDefender bool
}

// subCounterPay is one counter removed to pay a SubCounter cost part: the
// part it belongs to, the object the counter comes off, and (for a wildcard
// "Any" part) the chosen counter kind. A fixed-kind filtered part records a
// single entry with an empty Kind and settles the part's whole amount from
// part.Spec; a wildcard part records one entry per unit, each settling one
// counter of its chosen kind.
type subCounterPay struct {
	part int
	obj  state.ObjID
	kind string
}

// etbChoice is one "as this enters" choice, pre-computed: its kind
// ("name"/"/type"/"number", matching the Choose event's Counter) and the
// option list that will be offered, captured once at the start of the cast
// flow so the decision and the recorded choice always agree.
type etbChoice struct {
	kind    string
	options []decision.Option
}

// kickerCost and surgeCost resolve a face's own parameterised keyword to a
// parsed Cost, reporting whether the keyword is printed at all.
func kickerCost(f *cards.Face) (Cost, bool) {
	if _, _, two := twoPartKickerCosts(f); two {
		// The and/or two-part Kicker is its own option family (legal.go's
		// kicked1/kicked2/kickedboth offers, one per independently payable
		// part): the single "kicked" option must not also exist for such a
		// face -- its whole-string parse would degrade the colon separator
		// into a generic pip and charge the both-parts price for a
		// single-part choice.
		return Cost{}, false
	}
	s, ok := f.KeywordParam("Kicker")
	if !ok {
		return Cost{}, false
	}
	return ParseCost(s), true
}

// twoPartKickerCosts resolves the and/or Kicker ("Kicker {G} and/or {1}{U}",
// Forge's colon-separated two-part Kicker:<a>:<b> keyword line -- 18 corpus
// files at the pin, the Volver/Battlemage/Involver cycle family, Wastescape
// Battlemage the repo-deck carrier) to its two independently payable parts:
// each may be paid alone or both together (CR 601.2b's optional additional
// costs, each declared separately). ok=false for the single-cost Kicker
// (every colon-free param) and for a colon form whose parts do not parse
// clean -- a part with an unmodelled token stays a labelled census gap
// rather than being silently charged.
func twoPartKickerCosts(f *cards.Face) (Cost, Cost, bool) {
	s, ok := f.KeywordParam("Kicker")
	if !ok {
		return Cost{}, Cost{}, false
	}
	a, b, is := strings.Cut(s, ":")
	if !is || strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return Cost{}, Cost{}, false
	}
	ca, cb := ParseCost(a), ParseCost(b)
	if len(ca.Unknown) > 0 || len(cb.Unknown) > 0 {
		return Cost{}, Cost{}, false
	}
	return ca, cb, true
}

// entwineCost resolves the Entwine keyword's additional cost (CR 702.42,
// Forge's K:Entwine:<cost>). Entwine is an OPTIONAL additional cost paid once
// as the spell is cast; if paid, every eligible mode is chosen rather than the
// normal one. The cost forms the corpus carries are plain mana (30 of the 32
// carriers at the pin) and a sacrifice (Sac<3/Land> on Betrayal of Flesh and
// Sac<2/Land> on Solar Tide), and ParseCost models both, so the cost is not
// withheld for any corpus carrier -- but ANY cost ParseCost cannot price fails
// closed here (the replicateCost direction), leaving the card's gap in the
// coverage report rather than charging a degraded generic.
func entwineCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Entwine")
	if !ok {
		return Cost{}, false
	}
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

func surgeCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Surge")
	if !ok {
		return Cost{}, false
	}
	return ParseCost(s), true
}

// isCharmSpell reports whether f's spell ability is a modal Charm (the only
// shape Entwine has a modelled meaning for: "follow the instructions of all
// its modes"). It is the single reader legal.go's entwined offer and any
// future entwine site share, so the offer and the all-modes announcement
// cannot disagree about which faces are modal.
func isCharmSpell(f *cards.Face) bool {
	sa := f.SpellAbility()
	return sa != nil && sa.API == "Charm" && strings.TrimSpace(sa.Params["Choices"]) != ""
}

// replicateCost resolves the Replicate keyword's payment cost (CR 702.55a,
// Forge's K:Replicate:<cost>), the surgeCost shape. A cost carrying a token
// ParseCost cannot model is withheld (the fail-closed direction
// twoPartKickerCosts takes) rather than charged as degraded generic mana.
func replicateCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Replicate")
	if !ok {
		return Cost{}, false
	}
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// multikickerCost resolves the Multikicker keyword's PER-PAYMENT cost
// (CR 702.43, Forge's K:Multikicker:<cost>), the replicateCost shape: the
// same payment may be made any number of times as the spell is cast, so the
// offer gates on ONE payment being payable and the count ask
// (multikickAsk) settles how many. A cost carrying a token ParseCost cannot
// model is withheld (the replicateCost fail-closed direction).
func multikickerCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Multikicker")
	if !ok {
		return Cost{}, false
	}
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// squadCost resolves the Squad keyword's PER-PAYMENT cost (CR 702.66, Forge's
// K:Squad:<cost>), the replicateCost/multikickerCost shape: "you may pay
// [cost] any number of times" as the spell is cast, so the offer gates on ONE
// payment being payable and the count ask (squadAsk) settles how many. A cost
// carrying a token ParseCost genuinely cannot model is withheld (the same
// fail-closed direction); a modelled non-mana part (Thrill-Kill Disciple's
// "1 Discard<1/Card>", Ruthless Radrat's "ExileFromGrave<4/Card/cards>") is
// accepted here and its payability decided by the ordinary offer gate
// (nonManaCastable), exactly like any other cast cost.
func squadCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Squad")
	if !ok {
		return Cost{}, false
	}
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// keywordAltCost resolves any of the alternative-cast keyword family
// (Evoke, Dash, Overload, Warp, Madness) to a parsed Cost, reporting whether
// the keyword is printed at all. All five are "you may cast this for [cost]
// instead of its mana cost" shapes whose post-cast behaviour lives elsewhere
// (the ETB machinery for evoke, modeFlags for the rest), so they share one
// parameter read.
func keywordAltCost(f *cards.Face, head string) (Cost, bool) {
	s, ok := f.KeywordParam(head)
	if !ok {
		return Cost{}, false
	}
	return ParseCost(s), true
}

// blitzCosts is the shared offer/charge reader for every printed or granted
// Blitz instance. Distinct modes preserve separate costs when a printed
// keyword and a layer-6 grant coexist; grant filters and CardManaCost are
// resolved against the proposed spell before it is offered.
func (e *Engine) blitzCosts(p state.PlayerID, id state.ObjID) []struct {
	mode string
	cost Cost
} {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	if !e.stackKeywordPossibleH(id, kwhBlitz) {
		return nil
	}
	var out []struct {
		mode string
		cost Cost
	}
	blitzIndex := 0
	for _, keyword := range e.derivedWith(id, state.ZStack).Keywords {
		if !strings.EqualFold(cardsKeywordHead(keyword), "Blitz") {
			continue
		}
		blitzIndex++
		raw := ""
		if i := strings.IndexByte(keyword, ':'); i >= 0 {
			raw = strings.TrimSpace(keyword[i+1:])
		}
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) == 2 {
			spec, admits := e.castProvenanceAdmitsWindow(parts[1], id, p, true)
			if !admits {
				continue
			}
			sc := e.specCtx(0, p)
			sc.AsStack = true
			if !e.matchesSpec(spec, id, sc) {
				continue
			}
		}
		var toks []string
		for _, tok := range strings.Fields(parts[0]) {
			if strings.EqualFold(tok, "CardManaCost") {
				toks = append(toks, strings.Fields(o.Face().ManaCost)...)
			} else {
				toks = append(toks, tok)
			}
		}
		c := ParseCost(strings.Join(toks, " "))
		if len(c.Unknown) != 0 {
			continue
		}
		mode := "blitzed"
		if blitzIndex > 1 {
			mode = fmt.Sprintf("blitzed_grant_%d", blitzIndex)
		}
		out = append(out, struct {
			mode string
			cost Cost
		}{mode, c})
	}
	return out
}

// blitzCost retains the common single-cost read for existing consumers/tests.
func (e *Engine) blitzCost(p state.PlayerID, id state.ObjID) (Cost, bool) {
	costs := e.blitzCosts(p, id)
	if len(costs) == 0 {
		return Cost{}, false
	}
	return costs[0].cost, true
}

func (e *Engine) derivedKeywordParamAt(id state.ObjID, head string, zone state.Zone) (string, bool) {
	for _, k := range e.derivedWith(id, zone).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), head) {
			if i := strings.IndexByte(k, ':'); i >= 0 {
				return strings.TrimSpace(k[i+1:]), true
			}
			return "", true
		}
	}
	return "", false
}

// morphDownFamily reports the face-down cast mode a printed face offers:
// "morphed" for K:Morph, "megamorphed" for K:Megamorph and "disguised" for
// K:Disguise (CR 702.37a/702.168a/702.169a), "" when the face carries none
// of the family. KeywordParam is the one derived read (a layer-6
// AddKeyword$ grant would surface through it too); the {3} face-down cost
// itself is family-independent, so the offer and the charge price it
// directly, without a parameter.
func morphDownFamily(f *cards.Face) string {
	if _, ok := f.KeywordParam("Morph"); ok {
		return "morphed"
	}
	if _, ok := f.KeywordParam("Megamorph"); ok {
		return "megamorphed"
	}
	if _, ok := f.KeywordParam("Disguise"); ok {
		return "disguised"
	}
	return ""
}

func buybackCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Buyback")
	if !ok {
		return Cost{}, false
	}
	return ParseCost(s), true
}

// retraceExtra is Retrace's additional cost (CR 702.81a): discard a land
// card, in addition to the spell's other costs. Unlike the alternative-cost
// keyword family, Retrace is NOT a cost substitution -- the printed mana cost
// is still paid -- so this returns only the ADDITIONAL part, folded onto the
// printed base by both the offer gate (legal.go's graveyard walk) and
// beginCast's "retrace" mode, through the one definition so the two cannot
// disagree about what the cast costs.
func retraceExtra() Cost {
	return Cost{Discard: []CostPart{{Spec: "Land", N: 1}}}
}

// jumpstartExtra is Jump-start's additional cost (CR 702.84a): discard a card,
// in addition to the spell's other costs. Jump-start is NOT a cost
// substitution -- the oracle reads "in addition to paying its other costs" --
// so, exactly like retraceExtra, this returns only the ADDITIONAL part, folded
// onto the printed base by both the offer gate (legal.go's graveyard walk) and
// beginCast's "jumpstart" mode, through the one definition so the two cannot
// disagree. Unlike Retrace the discarded card may be any card (“a card”,
// not “a land card”), and the spell is exiled on resolution -- the flashback
// destination, charged by modeFlags' FlagJumpstart.
func jumpstartExtra() Cost {
	return Cost{Discard: []CostPart{{Spec: "Card", N: 1}}}
}

// altAddCostParts splits a face's AlternateAdditionalCost keyword into its
// alternative parts: the parameter is the parts joined by ":" (e.g. Bone
// Shards' "Sac<1/Creature>:Discard<1/Card>", Redirect Lightning's
// "PayLife<5>:2"). "As an additional cost to cast this spell, [A] or [B]"
// is a MANDATORY either-or (CR 601.2h), so the cast flow asks which one and
// folds the chosen part into the total cost. An empty or missing keyword
// yields nil; a parameter with no ":" yields nil (a single-part form would
// be an ordinary additional cost, which no corpus line uses -- the keyword's
// whole point is the either-or).
func altAddCostParts(f *cards.Face) []string {
	param, ok := f.KeywordParam("AlternateAdditionalCost")
	if !ok {
		return nil
	}
	parts := strings.Split(param, ":")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func harmonizeCost(f *cards.Face) (Cost, bool) {
	s, ok := f.KeywordParam("Harmonize")
	if !ok {
		return Cost{}, false
	}
	return ParseCost(s), true
}

// harmonizePayment applies the keyword's creature-power reduction in stable
// battlefield order and returns the creatures that pay it by becoming tapped.
func (e *Engine) harmonizePayment(p state.PlayerID, id state.ObjID, c Cost) (Cost, []state.ObjID) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || !o.Face().HasKeyword("Harmonize") {
		return c, nil
	}
	var tapped []state.ObjID
	for _, cid := range e.G.Zone(state.ZBattlefield, p) {
		if c.Generic == 0 {
			break
		}
		co := e.G.Obj(cid)
		if co == nil || co.Tapped || co.Face() == nil || !co.EffectiveIsCreature() || co.BestowedAttached() || co.ReconfiguredAttached() {
			continue
		}
		// The reduction is the creature's ACTUAL power (CR 702.46a: "reduce
		// that spell's generic cost by its power"): layer-7 effects and
		// P1P1/M1M1 counters apply, not the printed face. harmonizePayment
		// and convokeAsk's offer must read the same number or the offer
		// gate and the payment disagree on what the creature funds.
		reduce := e.Derived(cid).Power
		if reduce <= 0 {
			continue
		}
		if reduce > c.Generic {
			reduce = c.Generic
		}
		c.Generic -= reduce
		tapped = append(tapped, cid)
	}
	return c, tapped
}

type suspendInfo struct {
	time    int32
	timeX   bool
	minTime int32
	cost    Cost
}

// convokePayment is one announced non-mana payment. color is zero for a
// generic Convoke contribution or Harmonize; power is zero for Convoke and
// is the amount a Harmonize creature reduces the generic total by.
type convokePayment struct {
	id         state.ObjID
	color      byte
	power      int32
	countsMana bool
	// waterbend marks a tap that pays one generic of a RaiseCost
	// Waterbend<N>/<X> additional cost (convokeAsk's waterbend_generic
	// option); the count of such taps is capped at the waterbend amount.
	waterbend bool
}

// hasCastConvoke reports whether the spell being cast carries Convoke once
// it is on the stack: the printed keyword, or a layer-6 grant (Chief
// Engineer's "Artifact spells you cast have convoke") whose AffectedZone$
// scope reaches the cast spell. The announcement (CR 601.2b) runs while the
// announced spell is still in hand, so the evaluation pretends the zone is
// the stack (derivedWith's override); a wasCast Affected$ predicate already
// matches because it keys on the object being a cast spell, which it is.
func (e *Engine) hasCastConvoke(id state.ObjID) bool {
	if !e.stackKeywordPossibleH(id, kwhConvoke) {
		return false
	}
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), "Convoke") {
			return true
		}
	}
	return false
}

// hasCastImprovise reports whether the spell being cast carries Improvise
// (CR 702.66), read the same way hasCastConvoke reads Convoke: the printed
// keyword or a layer-6 grant reaching the cast spell. Inspiring Statuary
// grants Improvise to nonartifact spells, so the grant path is live.
func (e *Engine) hasCastImprovise(id state.ObjID) bool {
	if !e.stackKeywordPossibleH(id, kwhImprovise) {
		return false
	}
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), "Improvise") {
			return true
		}
	}
	return false
}

// hasCastConspire reports whether the spell being cast carries Conspire
// (CR 702.78a), read exactly the way hasCastConvoke reads Convoke: the
// printed keyword or a layer-6 grant whose AffectedZone$ scope reaches the
// cast spell. The announcement runs while the spell is still in hand, so
// derivedWith overrides the zone to the stack (which is also what lets a
// `wasCast` Affected$ grant like Wort, the Raidmother's or Raiding Schemes'
// match). The offer gate and the provenance read share this one helper so
// the two stages cannot disagree about whether the spell is conspirable.
func (e *Engine) hasCastConspire(id state.ObjID) bool {
	if !e.stackKeywordPossibleH(id, kwhConspire) {
		return false
	}
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), "Conspire") {
			return true
		}
	}
	return false
}

// hasCastCascade reports whether the spell being cast carries Cascade
// (CR 702.85), read the way hasCastConvoke reads Convoke: the printed K:
// line, or a layer-6 grant (the printed S: statics the layer walk emits and
// the DB$ Effect-delivered statics effEffect registers — TARDIS's "the next
// spell you cast this turn has cascade") whose AffectedZone$ scope reaches
// the cast spell. The queue that mints the cast trigger counts INSTANCES
// (cascadeInstances), because CR 702.85b gives a spell with two cascade
// abilities two triggers (Maelstrom Wanderer's "cascade, cascade").
func (e *Engine) hasCastCascade(id state.ObjID) bool {
	return e.cascadeInstances(id) > 0
}

// cascadeInstances counts the spell's Cascade instances: one per printed
// K:Cascade line plus one per layer-6 AddKeyword$ Cascade grant whose
// AffectedZone$ scope reaches the spell, evaluated against the stack the way
// hasCastConvoke's read is (derivedWith's zone override; the spell is on the
// stack by the time the cast is paid for, so the override and the live zone
// agree here).
func (e *Engine) cascadeInstances(id state.ObjID) int {
	n := 0
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), "Cascade") {
			n++
		}
	}
	return n
}

// conspireCandidates returns the untapped creatures the caster controls that
// share at least one colour with the spell being cast (CR 702.78a's "two
// untapped creatures you control that share a color with it"). Order is the
// deterministic battlefield zone order (costCandidates' own walk), so the
// option list and a replay's re-derivation agree. A colourless spell has no
// colour to share and returns an empty set -- the offer then never appears,
// which is the correct reading of the rule for a Conspire carrier.
func (e *Engine) conspireCandidates(p state.PlayerID, id state.ObjID) []state.ObjID {
	spell := e.G.Obj(id)
	if spell == nil || spell.Face() == nil {
		return nil
	}
	want := e.Colors(spell.ID)
	if want == "" {
		want = effects.ColorsOf(spell)
	}
	if want == "" {
		return nil
	}
	var out []state.ObjID
	for _, cid := range e.G.Zone(state.ZBattlefield, p) {
		co := e.G.Obj(cid)
		if co == nil || co.Tapped {
			continue
		}
		if !e.matchesSpecFrom("Creature.YouCtrl", cid, p, id) {
			continue
		}
		colors := e.objColors(co)
		for i := 0; i < len(colors); i++ {
			if strings.ContainsRune(want, rune(colors[i])) {
				out = append(out, cid)
				break
			}
		}
	}
	return out
}

// casualtySpec reads both printed and layer-6 granted keywords against the
// proposed stack zone. A grant scoped to AffectedZone$ Stack therefore works
// before pushCast, including CheckSVar-gated first-spell grants. The keyword
// line is `Casualty:<amount>` with optional script riders after further
// colons: the corpus's one rider carrier (Ob Nixilis, the Adversary) spells
// `K:Casualty:X:NonLegendary$ True | SetLoyalty$ Casualty:The copy isn't
// legendary and has starting loyalty X.` -- the amount token is X, the
// sacrificed creature's power (CR 702.249a's variable form), and the riders
// name the COPY's characteristics. A line whose amount parses neither as a
// nonnegative integer nor as X is skipped, the same skip casualtyValue made;
// the first parseable line wins.
type casualtyInfo struct {
	threshold    int32 // the fixed threshold, when !variable
	variable     bool  // amount token X: the amount is the sacrificed creature's power
	nonLegendary bool  // NonLegendary$ True rider: the copy isn't legendary
	setLoyalty   bool  // SetLoyalty$ Casualty rider: the copy's starting loyalty is the amount
}

func (e *Engine) casualtySpec(id state.ObjID) (casualtyInfo, bool) {
	if !e.stackKeywordPossibleH(id, kwhCasualty) {
		return casualtyInfo{}, false
	}
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if !strings.EqualFold(cardsKeywordHead(k), "Casualty") {
			continue
		}
		if info, ok := parseCasualtyLine(k); ok {
			return info, true
		}
	}
	return casualtyInfo{}, false
}

// parseCasualtyLine splits one Casualty keyword line into its amount token
// (the text up to the first further colon) and the riders/description tail
// after it, then reads the riders the corpus's one carrier spells: a literal
// `NonLegendary$ True` and a literal `SetLoyalty$ Casualty` (the copy's
// starting loyalty is the casualty amount). No other spelling is honoured --
// the tail scan is scoped to this measured shape.
func parseCasualtyLine(k string) (casualtyInfo, bool) {
	_, rest, found := strings.Cut(k, ":")
	if !found {
		return casualtyInfo{}, false
	}
	amt, riders := rest, ""
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		amt, riders = rest[:i], rest[i+1:]
	}
	var info casualtyInfo
	if strings.EqualFold(strings.TrimSpace(amt), "x") {
		info.variable = true
	} else {
		if _, err := fmt.Sscanf(strings.TrimSpace(amt), "%d", &info.threshold); err != nil || info.threshold < 0 {
			return casualtyInfo{}, false
		}
	}
	low := strings.ToLower(riders)
	info.nonLegendary = strings.Contains(low, "nonlegendary$ true")
	info.setLoyalty = strings.Contains(low, "setloyalty$ casualty")
	return info, true
}

func (e *Engine) casualtyCandidates(p state.PlayerID, spell state.ObjID, n int32) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if e.matchesSpecFrom("Creature.YouCtrl", id, p, spell) && e.Power(id) >= n {
			out = append(out, id)
		}
	}
	return out
}

// improviseCost applies CR 702.66a greedily in stable battlefield order:
// each untapped artifact controlled by p (not already committed by
// convokeCost -- the two keywords never share a carrier today, but the
// exclusion is structural) reduces the generic requirement by {1} and must
// be tapped. Improvise never pays coloured pips, so nothing else is
// consumed. It returns the reduced cost and exactly the artifacts that must
// be tapped.
func (e *Engine) improviseCost(p state.PlayerID, id state.ObjID, c Cost, committed []state.ObjID) (Cost, []state.ObjID) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || !e.hasCastImprovise(id) {
		return c, nil
	}
	skip := make(map[state.ObjID]bool, len(committed))
	for _, cid := range committed {
		skip[cid] = true
	}
	var tapped []state.ObjID
	for _, cid := range e.G.Zone(state.ZBattlefield, p) {
		if c.Generic == 0 {
			break
		}
		co := e.G.Obj(cid)
		if co == nil || co.Tapped || co.Face() == nil || !co.EffectiveIsArtifact() || co.BestowedAttached() || skip[cid] {
			continue
		}
		c.Generic--
		tapped = append(tapped, cid)
	}
	return c, tapped
}

// suspendCost parses Forge's Suspend:<time>:<cost> keyword form. X-time
// scripts put their lower bound in the leading XMin<N> cost token; the same
// announced X pays the cost and becomes the number of TIME counters.
// convokeCost commits untapped creatures in battlefield order, consuming a
// needed colour when that creature has one and otherwise one generic mana.
// It returns the reduced cost and exactly the creatures that must be tapped.
func (e *Engine) convokeCost(p state.PlayerID, id state.ObjID, c Cost) (Cost, []state.ObjID) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil || !e.hasCastConvoke(id) {
		return c, nil
	}
	var tapped []state.ObjID
	for _, cid := range e.G.Zone(state.ZBattlefield, p) {
		co := e.G.Obj(cid)
		if co == nil || co.Tapped || co.Face() == nil || !co.EffectiveIsCreature() || co.BestowedAttached() || co.ReconfiguredAttached() {
			continue
		}
		used := false
		for _, col := range []byte{'W', 'U', 'B', 'R', 'G'} {
			i := state.ManaIndex(col)
			if c.Colored[i] > 0 && strings.Contains(e.objColors(co), string(col)) {
				c.Colored[i]--
				used = true
				break
			}
		}
		if !used && c.Generic > 0 {
			c.Generic--
			used = true
		}
		if used {
			tapped = append(tapped, cid)
		}
	}
	return c, tapped
}

func suspendCost(f *cards.Face) (suspendInfo, bool) {
	raw, ok := f.KeywordParam("Suspend")
	if !ok {
		return suspendInfo{}, false
	}
	n, rest, ok := strings.Cut(raw, ":")
	if !ok {
		return suspendInfo{}, false
	}
	if strings.TrimSpace(n) == "X" {
		fields := strings.Fields(rest)
		info := suspendInfo{timeX: true}
		if len(fields) > 0 && strings.HasPrefix(fields[0], "XMin") {
			min, err := strconv.ParseInt(strings.TrimPrefix(fields[0], "XMin"), 10, 32)
			if err != nil || min < 0 {
				return suspendInfo{}, false
			}
			info.minTime = int32(min)
			fields = fields[1:]
		}
		info.cost = ParseCost(strings.Join(fields, " "))
		// The corpus's X-time Suspend form charges that same X. Without a
		// cost X there is no finite legal option range to announce, so do not
		// offer a made-up bound.
		if info.cost.X == 0 {
			return suspendInfo{}, false
		}
		return info, true
	}
	time, err := strconv.ParseInt(strings.TrimSpace(n), 10, 32)
	if err != nil || time < 0 {
		return suspendInfo{}, false
	}
	return suspendInfo{time: int32(time), cost: ParseCost(rest)}, true
}

// flashbackCost is id's Flashback cost: the printed parameter if this face
// carries one, or -- Flashback granted by a continuous effect with no
// printed parameter of its own (Snapcaster Mage's shape) -- the card's own
// mana cost (CR 702.32a's "cast for its normal cost" fallback).
func (e *Engine) flashbackCost(id state.ObjID) Cost {
	o := e.G.Obj(id)
	if o == nil {
		return Cost{}
	}
	f := o.Face()
	if f == nil {
		return Cost{}
	}
	if s, ok := f.KeywordParam("Flashback"); ok {
		return ParseCost(s)
	}
	return ParseCost(f.ManaCost)
}

// delveCredit is the most generic mana id's Delve can cover right now for a
// cost whose generic requirement is generic: the smaller of that and p's
// graveyard size. Zero for a card without Delve.
func (e *Engine) delveCredit(p state.PlayerID, id state.ObjID, generic int32) int32 {
	if generic <= 0 || !e.hasKeywordH(id, kwhDelve) {
		return 0
	}
	gy := int32(len(e.G.Zone(state.ZGraveyard, p)))
	if gy > generic {
		gy = generic
	}
	return gy
}

// castable reports whether cost is payable for id if cast by p right now:
// mana payable (Colored+Generic), crediting the generic requirement with
// delved graveyard cards when id has Delve; every Sac part has at least N
// matching permanents on p's battlefield; every Discard part is payable from
// p's hand; every SubCounter part's N does
// not exceed id's own current counters of that kind; and Tap requires id
// (an already-battlefield source -- Task 10 activates from there) to be
// untapped.
//
// The Sac check is a distinct-candidate feasibility check, not N independent
// head-counts against the same board (fix round 1, reviewer Important 1):
// the Sac parts of ONE cost are paid one after another, each consuming its
// chosen permanents, so a cost with TWO Sac parts cannot be paid by the same
// permanent twice. A `Sac<1/Creature> Sac<1/Creature>` cost must therefore
// not be offered with a single creature on the battlefield, and a
// `Sac<1/Creature.Red> Sac<1/Creature.Green> Sac<1/Creature.White>` cost
// cannot count one red-and-green creature towards both the red and the green
// part. Each part's N candidates are reserved (distinct, in zone-walk order)
// as the parts are walked, mirroring exactly what sacAsk offers; a part with
// fewer than N un-reserved candidates makes the whole cost unpayable, so the
// option is never offered (the totality rule an option that cannot be paid
// should never be offered). Reserving the first N matches in zone order is a
// sound test -- it never reports payable when no distinct assignment exists --
// and never illegal: a truly-payable cost where the FIRST N happen to collide
// with a scarcer later part is conservatively withheld (the engine's standing
// rule is that wrongly withholding a legal option is safe, while wrongly
// offering an unpayable one is an illegal game action).
// castable is the ordinary offer gate's price check. The mana half resolves
// through costPayable -- the seat's RESTRICTION-ADJUSTED floating pool
// (manaAvailableFor), exactly the pool payManaFor will charge -- because the
// engine must never offer a cast whose payment would later fail: restricted
// mana (a RestrictValid$ batch, e.g. Eldrazi Temple's "colorless mana that
// can be used only to pay Eldrazi costs") that does not match this payment
// is invisible here, the same way it is invisible to the payment. The
// potential-action walk does NOT go through castable: it prices against an
// explicitly hypothetical pool (castablePriced below), where the over-bound
// direction is deliberate. Every non-mana part -- Sac candidates, Discard
// candidates, SubCounter counts, Tap untappedness -- is checked against the
// REAL state: floating mana never satisfies a sacrifice.
func (e *Engine) castable(p state.PlayerID, id state.ObjID, cost Cost, ability bool) bool {
	mana := cost
	mana.Generic -= e.delveCredit(p, id, mana.Generic)
	if !e.costPayable(p, id, ability, mana) {
		return false
	}
	return e.nonManaCastable(p, id, cost, ability)
}

// countCandPayable reports whether a repeatable-additional-cost count walk's
// candidate -- the base composed cost plus N payments of the keyword's own
// cost -- is payable right now. The walk runs at CR 601.2b, before the CR
// 601.2f modifiers have been folded into pc.cost, so pricing the RAW
// candidate through castable skipped every RaiseCost/ReduceCost static: a
// Baral ("Instant and sorcery spells you cast cost {1} less") offered one
// fewer replicate payment than the pool could actually pay. This composes
// the candidate through manaToPay -- the SAME CR 601.2f/903.8 composition
// the payment will charge -- then makes the Convoke/Harmonize/Improvise
// reduction the later announcement can still make (convokeCountCredit),
// takes the Delve credit, and runs the ordinary mana gate; the non-mana
// parts go through nonManaCastable exactly as castable's tail does. Because
// the candidate is priced at its real charge, the bound can never offer a
// count the payment cannot settle.
func (e *Engine) countCandPayable(pc *pendingCast, cand Cost) bool {
	mana := e.countComposedCost(pc, cand)
	mana.Generic -= e.delveCredit(pc.player, pc.card, mana.Generic)
	if !e.costPayable(pc.player, pc.card, false, mana) {
		return false
	}
	return e.nonManaCastable(pc.player, pc.card, cand, false)
}

// countComposedCost is the CR 601.2f/903.8 composition of a count walk's
// candidate with the Convoke/Harmonize/Improvise reduction the caster can
// still announce folded in. It is manaToPay for the candidate cost (the same
// modifier snapshot, announced-X re-pricing and commander tax the payment
// uses), so the bound and the charge can never disagree about a ReduceCost
// static, and then convokeCountCredit for the announcement the later
// convokeAsk can make against that composed total.
func (e *Engine) countComposedCost(pc *pendingCast, cand Cost) Cost {
	tmp := *pc
	tmp.cost = cand
	m := e.manaToPay(&tmp)
	return e.convokeCountCredit(pc, m)
}

// castablePriced is castable priced against an EXPLICIT pool instead of the
// seat's restriction-adjusted floating one. Its only caller is the
// potential-action walk (rules/legal.go legalActionsPriced's affordable, and
// its own hyp==nil arm routes back to castable): pool is the hypothetical
// bound the seat would hold after floating every untapped source. The pool is
// a pure mana bound -- the walk may price against raw units a RestrictValid$
// provenance would refuse at payment time, because a wrongly WITHHELD pass
// costs one idle stop while a wrongly eaten window loses the player's action
// -- but every non-mana part (Sac candidates, Discard candidates, SubCounter
// counts, Tap untappedness) is still checked against the REAL state: floating
// or hypothetical mana never satisfies a sacrifice.
func (e *Engine) castablePriced(p state.PlayerID, id state.ObjID, cost Cost, ability bool, pool state.Mana) bool {
	mana := cost
	mana.Generic -= e.delveCredit(p, id, mana.Generic)
	if !e.costPayablePool(p, id, ability, mana, pool, e.G.Players[p].ManaUnits()) {
		return false
	}
	return e.nonManaCastable(p, id, cost, ability)
}

// chargeEnergyCost spends a cost's energy parts from the payer's pool, one
// PlayerCounterChange per part (a player counter, not an object's -- CR
// 118.2d). A fixed part spends its N; a dynamic part spends the announced x.
// This is the ONE energy-charging site, shared by the cast/activation payment
// path and the triggered-cost window, so a paid cost can never spend its
// energy in one place and skip it in another.
func (e *Engine) chargeEnergyCost(p state.PlayerID, c Cost, x int32) {
	for _, part := range c.Energy {
		amt := part.N
		if part.Spec == "X" {
			amt = x
		}
		if amt > 0 {
			e.emit(events.Event{Kind: events.PlayerCounterChange, Player: p,
				Counter: "ENERGY", Amount: -amt})
		}
	}
}

// exileFromTopCards returns the aggregate top-of-library prefix paid by a set
// of ExileFromTop parts. Parts do not each get to reuse the same prefix.
func exileFromTopCards(lib []state.ObjID, parts []CostPart) ([]state.ObjID, bool) {
	var n int64
	for _, part := range parts {
		if part.N <= 0 {
			return nil, false
		}
		n += int64(part.N)
	}
	if n > int64(len(lib)) {
		return nil, false
	}
	return append([]state.ObjID(nil), lib[:int(n)]...), true
}

// nonManaCastable is castable's payment-independent tail. Cost-modifier
// offer checks use it after their flexible-pip walk has established a payable
// resolved mana face: applying Color$ before that walk would otherwise see a
// hybrid pip as neither of its colours and withhold a cast that the eventual
// announced face can legally make free. Keeping all non-mana checks in this
// one helper means that specialized offer logic cannot bypass Sac/Discard/
// counter/tap legality.
func (e *Engine) nonManaCastable(p state.PlayerID, id state.ObjID, cost Cost, ability bool) bool {
	// A RaiseCost Cost$ part no payment stage can settle (Cost.Withheld):
	// the additional cost cannot be paid, so neither can the whole cost.
	if len(cost.Withheld) > 0 {
		return false
	}
	// untapYType<N/Spec> parts have a settle only on the mana-activation path
	// (manaUntapStage). The cast / non-mana activated-ability path has none,
	// so a cost carrying one is refused rather than offered with the untap
	// silently unpaid (the head now parses to a real part, no longer
	// Cost.Unknown).
	if len(cost.UntapPermanent) > 0 {
		return false
	}
	reserved := map[state.ObjID]bool{}
	for _, part := range cost.Sac {
		var avail []state.ObjID
		matchSpec := sacrificeMatchSpec(part.Spec)
		for _, oid := range e.G.Zone(state.ZBattlefield, p) {
			if reserved[oid] || e.sacrificeBlockedForCost(oid, costCauseForAbility(ability)) { // an earlier Sac part already claimed this one; a CantSacrifice-blocked one can never pay
				continue
			}
			if e.matchesSpecFrom(matchSpec, oid, p, id) {
				avail = append(avail, oid)
			}
		}
		if int32(len(avail)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	if !e.discardCostPayable(p, id, cost.Discard, !ability) {
		return false
	}
	// Exile cost parts (ExileFromHand/ExileFromGrave): each needs N matching
	// cards still available in the part's zone, reserved against the earlier
	// parts the same way the Sac parts above reserve against each other. For a
	// CAST (ability == false) the card being cast can never pay its own exile
	// cost: at offer time it still sits in the part's zone, so without the
	// self-skip below a Kotis with exactly Kotis+2 other cards would be
	// offered and then abort at payment (only 2 "other" cards remain once the
	// card is on the stack). An ability activation (ability == true) is
	// untouched -- encore's Cost$ ExileFromGrave<1/CARDNAME> really does exile
	// its own source. This also closes the same latent over-offer for an
	// escape cast from the graveyard.
	castObj := e.G.Obj(id)
	// Top-of-library exile parts share one ordered prefix. Check their
	// aggregate size, not each part against the same library prefix.
	if _, ok := exileFromTopCards(e.G.Zone(state.ZLibrary, p), cost.ExileFromTop); !ok {
		return false
	}
	for _, part := range cost.Exile {
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		selfInZone := !ability && castObj != nil && castObj.Zone == zone
		wholeZone := isWholeZoneExileSpec(part.Spec)
		var avail []state.ObjID
		for _, oid := range e.G.Zone(zone, p) {
			if reserved[oid] || (selfInZone && oid == id) {
				continue
			}
			// A battlefield Exile cost part (Exile<N/Spec>, Karn's Sylex,
			// Mechtitan Core) is a COST exile: a CantExile static whose
			// ForCost$ True restricts cost payments withholds the candidate
			// here, while a ForCost$ False line (The Master, Multiplied)
			// leaves it offered. exileBlockedForCost carries the pending
			// cast/activation identity so a cost-path ValidCause$ can be
			// evaluated, the same plumbing sacrificeCostCandidates uses.
			if zone == state.ZBattlefield && e.exileBlockedForCost(oid, costCauseForAbility(ability)) {
				continue
			}
			if wholeZone || e.matchesSpecFrom(part.Spec, oid, p, id) {
				avail = append(avail, oid)
			}
		}
		if int32(len(avail)) < part.N {
			return false
		}
		if wholeZone {
			// ExileFromHand<1/All> names the WHOLE zone, not a filter (the
			// same isWholeZoneExileSpec reading the triggered window's arm
			// takes): every still-available card pays, so reserve every
			// candidate, not just part.N. No cast/activation corpus carrier
			// exists today; the wiring keeps the two paths from diverging.
			for _, oid := range avail {
				reserved[oid] = true
			}
			continue
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	// ExiledMoveToGrave cost parts: each needs N matching cards still in
	// ANY player's exile zone (exiled cards live in their OWNER's exile
	// zone -- events/apply.go's zoneOwner -- so a controller-only scan
	// finds nothing on the Shelob shape, where the exiled card is in the
	// OPPONENT's exile zone), reserved against the earlier parts the same
	// way the Sac/Exile parts above reserve against each other.
	for _, part := range cost.MoveToGrave {
		avail := e.moveToGraveCandidates(p, id, part.Spec, reserved)
		if int32(len(avail)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	// Reveal cost parts: a whole-hand Reveal (Reveal<N/Hand>) is payable with
	// ANY hand -- including an empty one (CR 701.20a) -- so it never gates the
	// offer. Every other part needs N matching hand cards. The self-exclusion
	// follows the payment stage's rule: for a CAST (ability == false) the card
	// being cast is on the stack while its cost is paid, so it cannot pay its
	// own Reveal; for an ABILITY activation (ability == true) the source stays
	// in the hand and CAN pay a self-reveal (Reveal<1/CARDNAME>, the forecast
	// cycle) -- the unconditional exclusion here used to make every such
	// ability unpayable and therefore unoffered.
	for _, part := range cost.Reveal {
		if isWholeHandRevealSpec(part.Spec) {
			continue
		}
		// Reveal<N/SameColor> (Illuminated Folio) is RELATIONAL: SameColor
		// matches no card as a filter, so the candidates are every eligible
		// hand card and the part is payable iff N DISTINCT ones share one
		// colour (SetPropCapacity's largest shared class; a colourless
		// card's empty token set shares nothing). The payment ask carries
		// the same decision.SetPropShared rule, so offer and intake read
		// one definition -- sameColorRevealSets in rules/setprops.go.
		if isSameColorRevealSpec(part.Spec) {
			_, sets := e.sameColorRevealSets(p, id, !ability)
			if decision.SetPropCapacity(decision.SetPropShared, sets) < int(part.N) {
				return false
			}
			continue
		}
		if len(e.costCandidates(p, id, state.ZHand, part.Spec, !ability, false)) < int(part.N) {
			return false
		}
	}
	// RevealOrChoose<N/Spec> is the either-or cost: the cast is payable when
	// EITHER arm can pay -- N matching hand cards to reveal, OR N matching
	// permanents p controls to choose. The gate is existential, not the
	// reveal arm's alone: Monstrous Emergence must be castable with only a
	// controlled creature and no creature card in hand. Which arm is elected
	// is asked at payment (revealCostOrChooseAsk); here only existence is
	// tested, and the payability of the CHOOSE arm reads the battlefield, the
	// reveal arm the hand, exactly as revealCostOrChooseAsk offers them.
	for _, part := range cost.RevealOrChoose {
		hand, battlefield := e.revealOrChooseCandidates(p, id, part)
		if len(hand) < int(part.N) && len(battlefield) < int(part.N) {
			return false
		}
	}
	// RevealChosen<Player>/<Type> parts (Stalking Leonin, Guardian Archon,
	// Emissary of Grudges, A Killer Among Us): there is no hand choice and no
	// mana to pay, so the ONLY gate is that the ability's source still carries
	// the secretly-chosen designation. A source whose choice was cleared (or
	// never recorded) cannot activate, which is the fail-closed direction --
	// the ability is not offered rather than paying for a reveal of nothing.
	for _, part := range cost.RevealChosen {
		if !hasRevealChosenDesignation(e.G.Obj(id), part.Spec) {
			return false
		}
	}
	for _, part := range cost.Behold {
		n := len(e.costCandidates(p, id, state.ZHand, part.Spec, true, false)) +
			len(e.costCandidates(p, id, state.ZBattlefield, part.Spec, false, false))
		if n < int(part.N) {
			return false
		}
	}
	// Tap cost parts reserve their candidates against the Sac/Exile/Return
	// reservations above (one permanent cannot pay both) AND against each
	// other: a composed cost carrying the same tap part several times -- a
	// replicated cast re-pays its tapXType cost once per payment -- must not
	// count one untapped permanent for every part. Without the reservation
	// an affordability walk over the composed cost offered a bound far above
	// what the board could actually pay, and answering it aborted the cast
	// at the payment stage (CR 733's clean reversal, but a needless one).
	for _, part := range cost.TapPermanent {
		if part.Dyn != "" {
			// The dynamic tapXType heads (tapXType<X/Spec>, tapXType<Any/Spec>):
			// the tap election resolves the count at payment. An X-form part is
			// payable with zero candidates (X = 0 is a legal announcement); an
			// Any-form part must tap at least one matching permanent the earlier
			// parts have not already claimed, so a spec no unreserved candidate
			// satisfies leaves the cost unpayable rather than offering a
			// zero-tap payment of an effect that does not scale with the taps.
			if part.Dyn == "Any" {
				avail := 0
				var floorSum int32
				for _, oid := range e.costCandidates(p, id, state.ZBattlefield, part.Spec, false, true) {
					if reserved[oid] || (cost.Tap && oid == id) {
						continue
					}
					avail++
					floorSum += e.Power(oid)
				}
				if avail == 0 {
					return false
				}
				// The withTotalPowerGE<N> group predicate (Crew's "total power N
				// or greater", Mossbridge Troll's "total power 10 or greater"):
				// the paid set's TOTAL power must reach the floor, and "any
				// number" may tap every candidate, so the most the board can
				// pay is the sum of all their powers. A shortfall leaves the
				// cost unpayable rather than offering an election no legal
				// answer satisfies (Decision.Validate would reject every
				// answer and the game would wedge).
				if part.MinPower > 0 && floorSum < part.MinPower {
					return false
				}
			}
			continue
		}
		var avail []state.ObjID
		for _, oid := range e.costCandidates(p, id, state.ZBattlefield, part.Spec, false, true) {
			if reserved[oid] || (cost.Tap && oid == id) {
				continue
			}
			avail = append(avail, oid)
		}
		if len(avail) < int(part.N) {
			return false
		}
		// The literal form with a group predicate (none in the corpus today,
		// modelled for symmetry with the Any form): exactly N are tapped, so
		// the most power a legal answer can tap is the N largest candidates.
		// A shortfall withholds the cost.
		if part.MinPower > 0 && e.tapTopPowerSum(avail, int(part.N)) < part.MinPower {
			return false
		}
		for i := 0; i < int(part.N); i++ {
			reserved[avail[i]] = true
		}
	}
	for range cost.Blight {
		if len(e.costCandidates(p, id, state.ZBattlefield, "Creature.YouCtrl", false, false)) == 0 {
			return false
		}
	}
	if cost.Forage && len(e.G.Zone(state.ZGraveyard, p)) < 3 &&
		len(e.costCandidates(p, id, state.ZBattlefield, "Food.YouCtrl", false, false)) == 0 {
		return false
	}
	// Energy cost parts (PayEnergy<N>): the payer's energy counter total
	// covers the SUM of the fixed parts -- Forge CostPayEnergy.canPay reads
	// the same total, and a composed cost carrying the part several times (a
	// replicated cast re-pays its PayEnergy cost once per payment) draws the
	// pool down once per part, so the parts cannot each spend the whole
	// counter total independently. The dynamic X form is bounded by that
	// total at the X ask, so the offer gate needs no assumption about the
	// not-yet-chosen value. The read is the shared energyPayable helper, so
	// the cast path and the triggered-cost window cannot disagree about it.
	if !e.energyPayable(p, cost) {
		return false
	}
	// Return cost parts (Return<N/Spec>): the source itself (Spec CARDNAME,
	// Forge's payCostFromSource) must be in play; otherwise the payer controls
	// at least N distinct matching permanents, reserved against the Sac and
	// Exile reservations above so two parts cannot claim one permanent.
	for _, part := range cost.Return {
		spec := sacrificeMatchSpec(part.Spec)
		if strings.EqualFold(spec, "CARDNAME") {
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
				return false
			}
			continue
		}
		var avail []state.ObjID
		for _, oid := range e.G.Zone(state.ZBattlefield, p) {
			if reserved[oid] {
				continue
			}
			if e.matchesSpecFrom(spec, oid, p, id) {
				avail = append(avail, oid)
			}
		}
		if int32(len(avail)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	// PutToLib cost parts (PutCardToLibFrom<Zone><N/Pos/Spec>): the payer needs
	// N matching cards in the part's zone. A Battlefield CARDNAME part is the
	// source itself (Forge's payCostFromSource), which must still be on the
	// battlefield. Cards are reserved against the earlier Sac/Exile/Return
	// parts so two components cannot claim the same permanent. The library
	// POSITION never affects payability.
	for _, part := range cost.PutToLib {
		spec := sacrificeMatchSpec(part.Spec)
		if part.N == 1 && part.Zone == state.ZBattlefield && strings.EqualFold(spec, "CARDNAME") {
			// The singleton self-reference fast path only covers N=1; a larger
			// N needs the general candidate walk below (it would otherwise be
			// offered on the source alone and abort at payment time). The
			// controller check matches putToLibAsk's candidates branch: a
			// control-changed source is not a cost the payer can pay.
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != p {
				return false
			}
			reserved[id] = true
			continue
		}
		var avail []state.ObjID
		for _, oid := range e.G.Zone(part.Zone, p) {
			if reserved[oid] {
				continue
			}
			if e.matchesSpecFrom(spec, oid, p, id) {
				avail = append(avail, oid)
			}
		}
		if int32(len(avail)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[avail[i]] = true
		}
	}
	for _, part := range cost.Draw {
		// Draw cost parts (Draw<N/Spec>): the cast flow draws the PAYER; a
		// spec naming a trigger-only role (Player.TriggeredPlayer and friends)
		// has no binding here and is unpayable -- never offered -- rather than
		// silently drawing nobody. A dynamic part (Draw<X/Spec>) additionally
		// needs its SVar-resolved count to evaluate at payment; an
		// unresolvable body withholds the whole cost (fail closed, the
		// fixLifeXCost direction), never an unpayable offer with a zero draw.
		if _, ok := castFlowDrawPlayer(part.Spec, p); !ok {
			return false
		}
		if part.Dyn != "" {
			if _, ok := e.drawCostCount(id, p, part); !ok {
				return false
			}
		}
	}
	// GainLife<N/Player...> cost parts: the payment needs at least one alive
	// player the part's spec names relative to the payer (a player has to be
	// there to gain the life). A part whose spec matches nobody -- the only
	// payer alive, or an unevaluable spec -- leaves the cost unpayable and
	// therefore unoffered, the fail-closed direction. The same helper the
	// payment uses resolves the candidates, so gate and settlement agree.
	for _, part := range cost.GainLife {
		if len(e.gainLifeCostPlayers(p, part)) == 0 {
			return false
		}
	}
	if o := e.G.Obj(id); o != nil {
		for _, part := range cost.SubCounter {
			// An announced SubCounter<X/Kind> part's count is the cast's X,
			// bounded by the source's counter count at the X ask; the offer
			// gate makes no assumption about the not-yet-chosen value.
			if part.Announced {
				continue
			}
			// A part whose removal-target field names something other than the
			// source needs a battlefield candidate the payer controls with
			// enough counters (Ghave's "remove a +1/+1 counter from a creature
			// you control"), reserved against the earlier parts the same way.
			// A source-anchored part keeps the pre-existing source read.
			if !subCounterTargetsSource(part.Target) {
				cands := e.subCounterRemovalCandidates(p, id, part, part.N, reserved)
				if len(cands) == 0 {
					return false
				}
				// Deterministically mirror the ask stage's reservation: the
				// first candidate (candidates are in zone order) pays this
				// part when the later parts of the same cost count the same
				// pool. A board change before the ask aborts there.
				reserved[cands[0]] = true
				continue
			}
			if subCounterAvailable(o, part.Spec) < part.N {
				return false
			}
		}
		if activationTapCostUnavailable(o, cost) {
			return false
		}
	} else if len(cost.SubCounter) > 0 || cost.Tap {
		return false
	}
	return true
}

// castFlowDrawPlayer resolves a Draw cost part's spec inside the
// cast/activation flow: the payer draws (spec "", You, Player, Self), and
// Player.Activator too, because within an activation the activator IS the
// payer. A trigger-only role has no binding here; nonManaCastable blocks
// such a part so it is never offered.
func castFlowDrawPlayer(spec string, payer state.PlayerID) (state.PlayerID, bool) {
	switch spec {
	case "", "You", "Player", "Self", "Player.Activator":
		return payer, true
	}
	return 0, false
}

// drawCostCard emits the ordinary Draw event one card of a cost payment
// draws: the library's top card moves to the payer's hand, and a draw from
// an empty library is the loss the SBA checks (the same shape DrawFor's
// no-replacement draw and resumeOrdinaryDraw emit).
func (e *Engine) drawCostCard(p state.PlayerID) {
	lib := e.G.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		e.playerLoses(p, loseReasonMilled, "drew from an empty library")
		return
	}
	e.emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0],
		From: state.ZLibrary, To: state.ZHand, Secret: true})
}

// payMillCost settles every Mill<N> cost component: the payer mills the SUM
// of the parts' requirements from the top of their own library, one real
// MoveZone event per card in deterministic top-first order. The moves share a
// mill batch so MilledAll triggers once for this cost payment. No choice is
// involved, so nothing is asked. A mill instruction moves all remaining cards
// when its count exceeds the library size, so the snapshot clamps to the
// available prefix.
func (e *Engine) payMillCost(p state.PlayerID, parts []CostPart) {
	total, ok := millCostTotal(parts)
	if !ok || total <= 0 {
		return
	}
	lib := e.G.Zone(state.ZLibrary, p)
	if int64(len(lib)) < total {
		total = int64(len(lib))
	}
	// Snapshot the ids before emitting: each MoveZone mutates the library
	// the slice was read from.
	ids := append([]state.ObjID(nil), lib[:total]...)
	e.BeginMillBatch()
	for _, id := range ids {
		e.emit(events.Mill(id, p))
	}
	e.EndMillBatch()
}

func (e *Engine) payMillCostParts(pc *pendingCast) {
	e.payMillCost(pc.player, pc.cost.Mill)
}

// payDiscardCost settles every hand->graveyard discard component of a cost
// payment as ONE discard action: the payer discards the settled cards
// together, so Mode$ DiscardedAll fires once for the payment with its
// TriggerCount$Amount (and Remembered/Captured set) equal to the number of
// matching cards (CR 701.8), exactly as payMillCost batches a multi-card Mill
// cost. Every cost discard goes through this helper -- a cast, an activated
// ability, a mana-ability activation and a triggered mandatory cost all reach
// it -- so the four sites cannot diverge and a new one cannot forget the
// bracket. The bracket is opened and closed entirely inside this call and the
// emission loop cannot suspend, so successive cost actions never coalesce and
// an enclosing api:Discard batch (effects/cardflow.go's effDiscard) simply
// nests by depth. cycling names the cycling ability when the discard is paid
// for one (CR 702.29), tagging each card's event for Mode$ Cycled; an empty
// keyword emits the plain cost form. An absent object is skipped, exactly as
// the triggered-cost site's own guard did.
func (e *Engine) payDiscardCost(ids []state.ObjID, cycling string) {
	if len(ids) == 0 {
		return
	}
	e.BeginDiscardBatch()
	for _, id := range ids {
		if e.G.Obj(id) == nil {
			continue
		}
		if cycling != "" {
			e.emit(events.DiscardCostCycling(id, cycling))
		} else {
			e.emit(events.DiscardCost(id))
		}
	}
	e.EndDiscardBatch()
}

// payDrawCostParts settles every Draw cost component of a cast or activation
// payment: one ordinary draw per card of the part's count, for the drawer the
// part's spec names. The count is the literal N, or -- for the dynamic
// Draw<X/Spec> form -- the source's SVar bound by part.Dyn, resolved here at
// payment time (Champion of Wits' "draw cards equal to its power"). The
// offer gate (nonManaCastable) already proved each part's drawer and dynamic
// count resolvable, so a part that is somehow unresolvable at payment -- a
// stale stored cost -- pays nothing rather than guessing a count; the whole
// cost is never offered, so this is a belt-and-braces no-op, not a live path.
func (e *Engine) payDrawCostParts(pc *pendingCast) {
	for _, part := range pc.cost.Draw {
		drawer, ok := castFlowDrawPlayer(part.Spec, pc.player)
		if !ok {
			continue
		}
		n, ok := e.drawCostCount(pc.card, pc.player, part)
		if !ok {
			continue
		}
		for k := int32(0); k < n; k++ {
			e.drawCostCard(drawer)
		}
	}
}

// settlePutToLibCost settles every PutToLib cost component of a cast or
// activation payment (PutCardToLibFrom<Zone><N/Pos/Spec>): the chosen cards
// move to their OWNER's library. MoveZone appends to the destination zone, so
// a plain move lands at the bottom (Forge's Pos -1); a top placement (Pos 0)
// follows the move with one LibraryOrder per owner putting the moved cards
// back on top in the order they were chosen -- exactly the shape effects'
// libraryOrderPlacement emits (the same private flag), re-derived here rather
// than imported because effects must never be reached for a cost settle.
func (e *Engine) settlePutToLibCost(pc *pendingCast) {
	idx := 0
	for _, part := range pc.cost.PutToLib {
		n := int(part.N)
		end := idx + n
		if end > len(pc.putToLibs) {
			end = len(pc.putToLibs)
		}
		picks := pc.putToLibs[idx:end]
		idx = end
		for _, id := range picks {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZLibrary,
					Text: "put on the library as a cost"})
			}
		}
		if part.LibraryPos == 0 && len(picks) > 0 {
			e.putLibPicksOnTop(picks)
		}
	}
}

// putLibPicksOnTop emits the LibraryOrder that lifts the just-moved picks to
// the top of each owner's library, preserving pick order. MoveZone already
// appended them to the bottom; the order carries the complete new library and
// is Secret (a hidden zone must not leak). Grouped per owner in first-pick
// order -- deterministic because the picks slice is -- so a pick owned by
// another player lands in the right library (the zone owner Move already used).
func (e *Engine) putLibPicksOnTop(picks []state.ObjID) {
	byOwner := map[state.PlayerID][]state.ObjID{}
	var owners []state.PlayerID
	for _, id := range picks {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZLibrary {
			continue
		}
		if _, ok := byOwner[o.Owner]; !ok {
			owners = append(owners, o.Owner)
		}
		byOwner[o.Owner] = append(byOwner[o.Owner], id)
	}
	for _, owner := range owners {
		sel := byOwner[owner]
		selected := make(map[state.ObjID]bool, len(sel))
		for _, id := range sel {
			selected[id] = true
		}
		lib := e.G.Zone(state.ZLibrary, owner)
		order := make([]state.ObjID, 0, len(lib))
		order = append(order, sel...)
		for _, id := range lib {
			if !selected[id] {
				order = append(order, id)
			}
		}
		e.emit(events.Event{Kind: events.LibraryOrder, Player: owner, IDs: order, Secret: true})
	}
}

// gainLifeCostPlayers lists the seats a GainLife cost part's Spec names
// relative to the payer, in APNAP order starting at the payer (the same walk
// every multi-player effect uses). Spec is Forge's raw player word
// (Player.Opponent / Player.Other); both mean "a player other than the
// payer", and the engine evaluates them through the ONE shared player-spec
// evaluator (effects.MatchesPlayerSpec) so this can never disagree with the
// ValidPlayer$ rider arm that reads the same call. The payer is never its
// own gain-life target ("an opponent"/"each other player"), so a part that
// somehow matched the payer is skipped -- harmless for the corpus spellings,
// and it keeps the payer's own life out of a cost payment.
func (e *Engine) gainLifeCostPlayers(payer state.PlayerID, part CostPart) []state.PlayerID {
	var out []state.PlayerID
	for _, p := range e.G.AliveFrom(payer) {
		if p == payer {
			continue
		}
		if effects.MatchesPlayerSpec(e.G, part.Spec, p, payer) {
			out = append(out, p)
		}
	}
	return out
}

// payGainLifeCost settles the payer's GainLife<N/Player...> cost parts
// (Forge CostGainLife; Invigorate/Reverent Silence/Skyshroud Cutter): each
// part has every player its Spec names relative to the payer gain N life as
// one POSITIVE LifeChange per player. That is exactly the oracle for the
// /* "each other player" spelling and exact in the two-seat game for the
// bare Player.Opponent spelling; in a larger pod the bare spelling pays
// EVERY opponent rather than one chosen opponent (a documented deviation --
// the alternative-cost route has no mid-cast choose-an-opponent ask). The
// emit routes through applyReplacements/applyLifeReplacements, so a CR 616
// GainLife replacement and the CantGainLife static apply for free.
func (e *Engine) payGainLifeCost(payer state.PlayerID, parts []CostPart) {
	for _, part := range parts {
		if part.N <= 0 {
			continue
		}
		for _, p := range e.gainLifeCostPlayers(payer, part) {
			e.emit(events.Event{Kind: events.LifeChange, Player: p, Amount: part.N})
		}
	}
}

// payDamageCost makes the payer take n damage from the source -- the
// DamageYou<N> cost payment (Forge CostDamage). The event shape is the one
// payUnlessDamageCost emits: the Damage event names the payer, the engine's
// damage-source context names the source, and the same-source lifelink
// gains the controller the damage (CR 702.16d).
func (e *Engine) payDamageCost(payer state.PlayerID, n int32, source state.ObjID, sourceLKI damageKeywordLKI, sourceControllerLKI state.PlayerID) {
	if n <= 0 {
		return
	}
	liveSource := e.G.Obj(source)
	keywords := e.damageKeywordsOf(source)
	controller := payer
	if liveSource != nil && liveSource.Zone == state.ZBattlefield {
		controller = liveSource.Controller
	} else {
		keywords = sourceLKI
		controller = sourceControllerLKI
	}
	prev := e.SetDamageSource(source)
	dam := events.Event{Kind: events.Damage, Player: payer, Amount: n}
	if keywords.infect {
		// CR 702.90b: even a cost payment is damage dealt by its source, so
		// an infect source's DamageYou cost pays in counter/poison form.
		dam.Counter = "infect"
	}
	ev := e.emit(dam)
	e.SetDamageSource(prev)
	if ev.Kind != events.Damage || !keywords.lifelink {
		return
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: controller, Amount: n})
}

func (e *Engine) costCandidates(p state.PlayerID, source state.ObjID, zone state.Zone, spec string, excludeSource, untapped bool) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(zone, p) {
		o := e.G.Obj(id)
		if o == nil || (zone == state.ZBattlefield && !existsOnBattlefield(o)) || (excludeSource && id == source) || (untapped && o.Tapped) {
			continue
		}
		if e.matchesSpecFrom(spec, id, p, source) {
			out = append(out, id)
		}
	}
	return out
}

// revealOrChooseCandidates returns the objects that can pay one
// RevealOrChoose<N/Spec> part, hand cards first (the REVEAL arm) then
// battlefield permanents (the CHOOSE arm). The two arms' candidate lists are
// DISTINCT -- a card in hand is never a legal choose-arm candidate and a
// permanent is never a legal reveal-arm candidate -- so the ask can offer
// them as separate option kinds and the payment records which arm was used.
// The choose arm scans the payer's own battlefield (costCandidates' zone walk
// is controller-scoped), which is the "you control" the card text requires;
// a permanent controlled by an opponent is not offered. One helper backs both
// the offer gate (nonManaCastable) and the ask so the count that offered the
// cast and the objects the payer may elect cannot disagree.
func (e *Engine) revealOrChooseCandidates(p state.PlayerID, source state.ObjID, part CostPart) (hand, battlefield []state.ObjID) {
	hand = e.costCandidates(p, source, state.ZHand, part.Spec, true, false)
	battlefield = e.costCandidates(p, source, state.ZBattlefield, part.Spec, false, false)
	return hand, battlefield
}

// sacrificeMatchSpec normalizes Forge's NICKNAME spelling to CARDNAME before
// the source-aware filter is applied. The filter owns CARDNAME's object-ID
// semantics; costs use this helper at both offer and payment time so the two
// stages cannot disagree about whether a self-reference is payable.
func sacrificeMatchSpec(spec string) string {
	if strings.EqualFold(spec, "NICKNAME") {
		return "CARDNAME"
	}
	return spec
}

// sacrificeCostCandidates returns, in battlefield scan order, the permanents
// that can pay one Sac cost part for a cast (ability=false) or an activation
// (ability=true) of source by p. Every Sac stage derives its candidate list
// from this one helper -- the X announcement's upper bound (xAsk), the
// sacrifice settle (sacAsk) and the offer gate's announced-X affordability
// sweep (offerCastableUsing) -- so the count an offer is priced on, the count
// the payer may announce, and the count the payment can settle cannot
// disagree about whether a self-reference or a CantSacrifice block is
// payable.
func (e *Engine) sacrificeCostCandidates(p state.PlayerID, source state.ObjID, part CostPart, ability bool) []state.ObjID {
	matchSpec := sacrificeMatchSpec(part.Spec)
	cause := costCauseForAbility(ability)
	var out []state.ObjID
	if matchSpec == "CARDNAME" {
		// A bare self-reference matches exactly the source (CR 201.5; the
		// filter's CARDNAME base rejects every object whose ID is not
		// sc.Source, and a zero source matches nothing), so the scan below
		// can admit at most the source itself, at its battlefield position.
		// Test it alone instead of matching the whole battlefield: a mass of
		// Sac<1/CARDNAME> mana tokens (Eldrazi Spawn) otherwise makes every
		// payability check O(board) and the priority walk O(board^2).
		if source != 0 && slices.Contains(e.G.Zone(state.ZBattlefield, p), source) &&
			existsOnBattlefield(e.G.Obj(source)) && !e.sacrificeBlockedForCost(source, cause) &&
			e.matchesSpecFrom(matchSpec, source, p, source) {
			out = append(out, source)
		}
		if sacrificeCardnameVerify {
			if want := e.sacrificeCostScan(p, source, matchSpec, cause); !slices.Equal(out, want) {
				panic(fmt.Sprintf("rules: CARDNAME sacrifice fast path %v, full scan %v (source %d)", out, want, source))
			}
		}
		return out
	}
	return e.sacrificeCostScan(p, source, matchSpec, cause)
}

// sacrificeCardnameVerify makes the CARDNAME fast path above also run the
// full battlefield scan and panic on any difference. Set by the rules test
// binary (derivedmemo_verify_test.go), or at link time with
// derivedMemoVerifyFlag.
var sacrificeCardnameVerify = derivedMemoVerifyFlag != ""

// sacrificeCostScan is sacrificeCostCandidates' full battlefield scan.
func (e *Engine) sacrificeCostScan(p state.PlayerID, source state.ObjID, matchSpec string, cause costCause) []state.ObjID {
	var out []state.ObjID
	for _, oid := range e.G.Zone(state.ZBattlefield, p) {
		if !existsOnBattlefield(e.G.Obj(oid)) || e.sacrificeBlockedForCost(oid, cause) {
			continue
		}
		if e.matchesSpecFrom(matchSpec, oid, p, source) {
			out = append(out, oid)
		}
	}
	return out
}

// sacrificeCostAssignable tests whether all Sac parts can be paid with
// distinct permanents for an announced X. A per-part candidate count is
// insufficient: two parts can each have X candidates but share every one.
// Match each required sacrifice to an object, rerouting earlier matches when
// a later, narrower part needs one of their objects. This is an existence
// check, not a payment choice; sacAsk still lets the player choose the
// actual sacrifices in cost-part order.
func (e *Engine) sacrificeCostAssignable(p state.PlayerID, source state.ObjID, parts []CostPart, ability bool, x int32) bool {
	candidates := make([][]state.ObjID, len(parts))
	for i, part := range parts {
		candidates[i] = e.sacrificeCostCandidates(p, source, part, ability)
		need := part.N
		if part.Announced {
			need = x
		}
		if need > int32(len(candidates[i])) {
			return false
		}
	}
	assigned := make(map[state.ObjID]int)
	var claim func(int, map[state.ObjID]bool) bool
	claim = func(i int, seen map[state.ObjID]bool) bool {
		for _, oid := range candidates[i] {
			if seen[oid] {
				continue
			}
			seen[oid] = true
			prev, used := assigned[oid]
			if !used || claim(prev, seen) {
				assigned[oid] = i
				return true
			}
		}
		return false
	}
	for i, part := range parts {
		need := part.N
		if part.Announced {
			need = x
		}
		for n := int32(0); n < need; n++ {
			if !claim(i, make(map[state.ObjID]bool)) {
				return false
			}
		}
	}
	return true
}

// lastDrawnThisTurn returns the card player p most recently DREW this turn
// (the last events.Draw naming p since the most recent TurnChange), or 0 if p
// drew nothing this turn. Derived from the event log exactly like
// CardsDrawnThisTurn, so a replay derives it identically; it is NOT a filter.
// Discard<1/LastDrawn> (Jandor's Ring) is the corpus's only carrier.
func (e *Engine) lastDrawnThisTurn(p state.PlayerID) state.ObjID {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.Draw && ev.Player == p {
			return ev.Obj
		}
	}
	return 0
}

// discardCandidates returns the still-available cards that can pay one
// Discard cost part. Random names a selection method rather than a card
// characteristic, and a Hand spec is Forge's "discard your hand" shape
// (the corpus spells its ignored count as both 0 and 1). LastDrawn is the
// other history-keyed slot: "the last card you drew this turn", one specific
// card, unpayable when p drew nothing or that card has left the hand.
// A spell being announced is excluded because it will be on the stack when
// costs are paid; an activated ability's source may remain in hand and can
// therefore pay CARDNAME/NICKNAME costs such as channel and bloodrush.
func (e *Engine) discardCandidates(p state.PlayerID, source state.ObjID, part CostPart, casting bool, reserved map[state.ObjID]bool) []state.ObjID {
	all := strings.EqualFold(part.Spec, "Random") || strings.EqualFold(part.Spec, "Hand")
	matchSpec := part.Spec
	if strings.EqualFold(matchSpec, "NICKNAME") {
		// Forge uses NICKNAME as the same self-reference as CARDNAME in the
		// four discard-cost lines that carry it.
		matchSpec = "CARDNAME"
	}
	if strings.EqualFold(matchSpec, "LastDrawn") {
		// The single candidate is the last card p drew this turn, and only if
		// it is still in p's hand. If p drew nothing, or that card has left
		// the hand, the cost is unpayable -- do not skip back to an earlier
		// draw.
		last := e.lastDrawnThisTurn(p)
		if last != 0 && !reserved[last] && !(casting && last == source) {
			if o := e.G.Obj(last); o != nil && o.Zone == state.ZHand {
				return []state.ObjID{last}
			}
		}
		return nil
	}
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZHand, p) {
		if reserved[id] || (casting && id == source) {
			continue
		}
		if all || e.matchesSpecFrom(matchSpec, id, p, source) {
			out = append(out, id)
		}
	}
	return out
}

// discardCostPayable is the offer-side totality gate for Discard costs. It
// mirrors discardAsk's deterministic reservation walk without consuming RNG.
func (e *Engine) discardCostPayable(p state.PlayerID, source state.ObjID, parts []CostPart, casting bool) bool {
	reserved := map[state.ObjID]bool{}
	for _, part := range parts {
		if part.Announced {
			// An announced Discard<X/Spec> (the RaiseCost bridge's Aether
			// Tide shape): X = 0 is a legal announcement, and xAsk caps the
			// X by the matching cards, so the part never withholds an offer.
			continue
		}
		candidates := e.discardCandidates(p, source, part, casting, reserved)
		if strings.EqualFold(part.Spec, "Hand") {
			for _, id := range candidates {
				reserved[id] = true
			}
			continue
		}
		if part.N <= 0 || int32(len(candidates)) < part.N {
			return false
		}
		for i := int32(0); i < part.N; i++ {
			reserved[candidates[i]] = true
		}
	}
	return true
}

// spellsCastThisTurn counts PutOnStack events for player p since the last
// TurnChange in the log (or since the start of the log, on turn 1).
func (e *Engine) spellsCastThisTurn(p state.PlayerID) int {
	n := 0
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.TurnChange {
			break
		}
		if ev.Kind == events.PutOnStack && ev.Player == p {
			n++
		}
	}
	return n
}

// withSpellAbilityExtras folds a spell's own SpellAbility Cost$ ADDITIONAL
// (non-mana) parts into cost. It exists so the OFFER and the CHARGE cannot
// disagree about what a plain cast costs.
//
// They did disagree, and it wedged a live game. legal.go offered a plain cast
// after gating castable on adjustedCost alone -- the printed mana -- while
// beginCast folded the SpellAbility's Cost$ Sac part in afterwards. Village
// Rites ({B}, "As an additional cost, sacrifice a creature") was therefore
// offered to a player with no creature: sacAsk found zero candidates and
// aborted the cast, the abort consumed nothing, priority returned to a board
// identical to the one that produced the offer, and the same option was
// offered again -- an unbounded livelock (measured: a 5-event cycle repeating
// until the match was killed). The abort in sacAsk is correct and stays; what
// was wrong is that the option existed at all, which is the standing rule that
// an option that cannot be paid must never be offered.
//
// The alternative-cost path was given this same gate in an earlier round (see
// the ruling comment in legal.go's alternativeCosts loop). The base cast path
// has the identical hole and was missed, so both now go through this one
// definition rather than each repeating the fold.
//
// The mana part of a Cost$ is deliberately NOT folded: it RESTATES the printed
// mana cost rather than adding to it, so re-adding it would double charge.
// Every OTHER component -- Life/Sac/Discard/SubCounter/Tap, and the whole
// non-mana family including Exile, MoveToGrave, Reveal, Energy, Draw, LifeX,
// DamageYou and Mill -- is additional and is concatenated below.
func withSpellAbilityExtras(f *cards.Face, cost Cost) Cost {
	sa := f.SpellAbility()
	if sa == nil {
		return cost
	}
	sc := sa.Params["Cost"]
	if sc == "" {
		return cost
	}
	return foldAdditionalCost(cost, ParseCost(sc))
}

// foldAdditionalCost concatenates the non-mana parts of extra onto cost. It is
// THE one definition shared by two carriers that must agree: withSpellAbilityExtras
// folds a spell's own SpellAbility Cost$, and beginCast folds a RaiseCost
// static's non-mana Cost$ (Soul Immolation's `Cost$ Blight<X>`) that the cost
// composition carried in costMods.extra. The mana part of a Cost$ is
// deliberately NOT folded: it RESTATES the printed mana cost rather than
// adding to it, so re-adding it would double charge. Every OTHER component --
// Life/Sac/Discard/SubCounter/Tap, and the whole non-mana family including
// Exile, MoveToGrave, Reveal, Energy, Draw, LifeX, DamageYou, Mill and Blight --
// is additional and is concatenated below.
func foldAdditionalCost(cost, extra Cost) Cost {
	cost.Life = addClampedGeneric(cost.Life, int64(extra.Life))
	if len(extra.Sac) > 0 {
		cost.Sac = append(append([]CostPart(nil), cost.Sac...), extra.Sac...)
	}
	if len(extra.Discard) > 0 {
		cost.Discard = append(append([]CostPart(nil), cost.Discard...), extra.Discard...)
	}
	if len(extra.SubCounter) > 0 {
		cost.SubCounter = append(append([]CostPart(nil), cost.SubCounter...), extra.SubCounter...)
	}
	if len(extra.Exile) > 0 {
		cost.Exile = append(append([]CostPart(nil), cost.Exile...), extra.Exile...)
	}
	if len(extra.ExileFromTop) > 0 {
		cost.ExileFromTop = append(append([]CostPart(nil), cost.ExileFromTop...), extra.ExileFromTop...)
	}
	if len(extra.MoveToGrave) > 0 {
		cost.MoveToGrave = append(append([]CostPart(nil), cost.MoveToGrave...), extra.MoveToGrave...)
	}
	if len(extra.Reveal) > 0 {
		cost.Reveal = append(append([]CostPart(nil), cost.Reveal...), extra.Reveal...)
	}
	if len(extra.RevealOrChoose) > 0 {
		cost.RevealOrChoose = append(append([]CostPart(nil), cost.RevealOrChoose...), extra.RevealOrChoose...)
	}
	if len(extra.RevealChosen) > 0 {
		cost.RevealChosen = append(append([]CostPart(nil), cost.RevealChosen...), extra.RevealChosen...)
	}
	if len(extra.Behold) > 0 {
		cost.Behold = append(append([]CostPart(nil), cost.Behold...), extra.Behold...)
	}
	if len(extra.TapPermanent) > 0 {
		cost.TapPermanent = append(append([]CostPart(nil), cost.TapPermanent...), extra.TapPermanent...)
	}
	if len(extra.Blight) > 0 {
		cost.Blight = append(append([]CostPart(nil), cost.Blight...), extra.Blight...)
	}
	if len(extra.Energy) > 0 {
		cost.Energy = append(append([]CostPart(nil), cost.Energy...), extra.Energy...)
	}
	if len(extra.Return) > 0 {
		cost.Return = append(append([]CostPart(nil), cost.Return...), extra.Return...)
	}
	if len(extra.Draw) > 0 {
		cost.Draw = append(append([]CostPart(nil), cost.Draw...), extra.Draw...)
	}
	if len(extra.LifeX) > 0 {
		cost.LifeX = append(append([]CostPart(nil), cost.LifeX...), extra.LifeX...)
	}
	if len(extra.DamageYou) > 0 {
		cost.DamageYou = append(append([]CostPart(nil), cost.DamageYou...), extra.DamageYou...)
	}
	if len(extra.GainLife) > 0 {
		cost.GainLife = append(append([]CostPart(nil), cost.GainLife...), extra.GainLife...)
	}
	if len(extra.Mill) > 0 {
		cost.Mill = append(append([]CostPart(nil), cost.Mill...), extra.Mill...)
	}
	if len(extra.Evidence) > 0 {
		cost.Evidence = append(append([]CostPart(nil), cost.Evidence...), extra.Evidence...)
	}
	cost.Forage = cost.Forage || extra.Forage
	cost.Tap = cost.Tap || extra.Tap
	return cost
}

// beginCast starts the cast flow for opt (a "cast" priority option): resolve
// which cost opt pays (the base/alternative cost as before, or the
// kicked/surged/flashback cost opt.Mode names), build the pendingCast, and
// run its first stage.
func (e *Engine) beginCast(p state.PlayerID, opt decision.Option) {
	e.beginCastWithPayment(p, opt, nil)
}

// beginCastWithPayment is the authoritative normal-cast entry with an
// optional, already admitted payment witness.  It intentionally takes no
// client cast descriptor: the selector is resolved against the offered action
// in Submit before this point.
func (e *Engine) beginCastWithPayment(p state.PlayerID, opt decision.Option, selection *decision.PaymentSelection) {
	e.beginCastWith(p, opt, selection, false)
}

// beginCastAnnounced begins an announced cast (Intent.Announce): the ordinary
// cast transaction with no witness, whose CR 601.2g window is the announced
// "select mana" window (rules/announce_pay.go).
func (e *Engine) beginCastAnnounced(p state.PlayerID, opt decision.Option) {
	e.beginCastWith(p, opt, nil, true)
}

func (e *Engine) beginCastWith(p state.PlayerID, opt decision.Option, selection *decision.PaymentSelection, announced bool) {
	id := opt.Obj
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	from := o.Zone
	// The no-progress suppression state as it stood before this proposal.
	// The alternate-face routes below flip the card (a FlipFace is a
	// state-changing event to emit's suppression-clearing rule), but that
	// flip is part of the provisional proposal: an aborted cast flips it
	// back (CR 733.1). See emitProposalFlip.
	preSuppress, preAborts := e.suppressedCast, e.castAborts
	var faceBefore *uint8
	if opt.Mode == "modal_spell" {
		if modalSpellBack(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	if opt.Mode == "room_alt" {
		if roomAlternateCastFace(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	// CR 714: the same flip mechanism serves the Adventure faces. From the
	// hand the cast flips to the Adventure spell face (adventure_alt); from
	// the adventure zone it flips back to the main face (adventure_recast).
	// Everything downstream -- rawBaseCost, targets, timing, resolution --
	// then reads the flipped face, because o.Face() is Faces[FaceIdx]. An
	// aborted proposal restores the pre-flip face via pc.faceBefore (CR
	// 733.1), the same reversal a Room cast takes.
	if opt.Mode == "adventure_alt" {
		if adventureSpellFace(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	if opt.Mode == "adventure_recast" {
		if o.Zone != state.ZExile || adventureSpellFace(o) == nil || o.Face() != o.Card.Faces[1] {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	// CR 702.85a: the Aftermath half -- the alternate face of a Split card --
	// is cast only from its owner's graveyard. From the graveyard the cast
	// flips to the alternate face before the ordinary cast transaction;
	// rawBaseCost, targets and resolution then read the aftermath face
	// (rawBaseCost's default below already pays the FLIPPED face's printed
	// mana cost, which is what aftermath charges). An aborted proposal
	// restores the pre-flip face via pc.faceBefore (CR 733.1), the same
	// reversal a Room or Adventure cast takes.
	if opt.Mode == "aftermath" {
		if o.Zone != state.ZGraveyard || aftermathAlternateFace(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	// CR 709.4: the alternate half of a non-Room split card (mode split_alt)
	// is cast from hand exactly like a Room door or an Adventure spell face:
	// one FlipFace to the chosen half before the ordinary cast transaction,
	// after which rawBaseCost, targets and resolution all read that half. An
	// aborted proposal restores the pre-flip face via pc.faceBefore.
	if opt.Mode == "split_alt" {
		if splitAlternateCastFace(o) == nil {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	// CR 310.11: the defeated battle's owner casts it TRANSFORMED (mode
	// defeat_cast). The exiled battle is its front face; one FlipFace to the
	// back face before the ordinary cast transaction, after which targets and
	// resolution read the back face exactly like the Room/Adventure/Split
	// flips above, and an aborted proposal restores the front face via
	// pc.faceBefore (CR 733.1). The cast is free (cost switch below) and
	// bypasses the ordinary timing gate like Suspend's: it is part of the
	// defeat, which happens in a combat the owner is usually not the active
	// player of, so a creature- or sorcery-timed back face must still be
	// castable (every Siege back face is printed exactly for this cast).
	if opt.Mode == "defeat_cast" {
		if o.Zone != state.ZExile || o.Card == nil || len(o.Card.Faces) < 2 || o.FaceIdx != 0 {
			return
		}
		before := o.FaceIdx
		faceBefore = &before
		e.emitProposalFlip(id, before, preSuppress, preAborts)
	}
	f := o.Face()
	if f == nil {
		return
	}

	var optionalCost Cost
	// Which cost this pays is opt.AltCostIndex, not always adjustedCost
	// (Ruling T19b-b): legalActions gates each "cast" option on that
	// specific option's own cost being payable, so beginCast must charge
	// that same cost. An out-of-range AltCostIndex (a stale option from a
	// board state that no longer holds the granting static) falls back to
	// the base cost rather than indexing out of bounds.
	//
	// The cost stored here is the RAW selected cost, with no cost modifiers
	// (CR 601.2f): RaiseCost/ReduceCost are applied later, in manaToPay,
	// once {X} is folded into Generic. adjustedCost's modifier-into-generic
	// form must not be stored here, or a spell with an {X} in it would have
	// its reduction applied before X is known (and lost), and a
	// flashback/alternative recast would drop the modifiers entirely.
	cost := e.rawBaseCost(p, id)
	var announceAlt *altCostView
	if opt.AltCostIndex > 0 {
		if alts := e.alternativeCosts(p, id); opt.AltCostIndex-1 < len(alts) {
			cost = alts[opt.AltCostIndex-1].cost
			announceAlt = &alts[opt.AltCostIndex-1]
		}
	}
	selectedMode := opt.Mode
	if strings.HasPrefix(selectedMode, "blitzed_grant_") {
		// Keep a unique offer mode for each grant cost, but use canonical Blitz
		// semantics for the rest of the cast pipeline.
		opt.Mode = "blitzed"
	}
	if strings.HasPrefix(selectedMode, "webslinged_grant_") {
		// Web-slinging (CR 702.186a-family): the same grant-cost convention as
		// blitzed_grant_N above -- a unique offer mode per grant cost, canonical
		// web-slinging semantics for the rest of the cast pipeline (the charge
		// below and modeFlags' flag both key the canonical mode).
		opt.Mode = "web-slinging"
	}
	switch opt.Mode {
	case "kicked":
		if kc, ok := kickerCost(f); ok {
			cost = cost.Plus(kc)
		}
	// CR 702.101b: a Fuse cast pays BOTH halves' printed mana costs as one
	// combined cost; the card itself stays at its front face (no FlipFace),
	// and the pay-time FlagFused provenance makes resolution run both halves.
	case "fuse":
		if ff, fa := fusedSplitFaces(o); ff != nil {
			cost = e.fuseCost(ff, fa)
		}
	case "kicked1", "kicked2", "kickedboth":
		// The and/or Kicker's per-part modes (legal.go offers one option per
		// independently payable part): each mode adds exactly the parts its
		// name promises. An out-of-range or unparseable form (a stale option
		// or a face whose Kicker changed) falls back to the base cost --
		// the same no-crash read the AltCostIndex fallback below takes.
		if c1, c2, ok := twoPartKickerCosts(f); ok {
			switch opt.Mode {
			case "kicked1":
				cost = cost.Plus(c1)
			case "kicked2":
				cost = cost.Plus(c2)
			default:
				cost = cost.Plus(c1).Plus(c2)
			}
		}
	case "surged":
		if sc, ok := surgeCost(f); ok {
			cost = sc
		}
	case "entwined":
		// Entwine (CR 702.42a): the additional cost is paid ON TOP of the
		// printed mana cost -- the Buyback shape. The offer gate priced the
		// SAME read (legal.go's offer), so the two stages cannot disagree.
		// The mode announcement then forces every eligible mode in
		// castModeAsk; a stale option whose keyword is gone still pays the
		// base cost alone (no fake charge), and the cast_mode answer falls
		// back to the ordinary single-mode bounds.
		if ec, ok := entwineCost(f); ok {
			cost = cost.Plus(ec)
		}
	case "buyback":
		if bc, ok := buybackCost(f); ok {
			cost = cost.Plus(bc)
		}
	case "offspring":
		// Offspring (CR 702.175a): the additional cost is paid ON TOP of the
		// mana cost ("You may pay an additional [cost] as you cast this
		// spell"), never a substitution -- the Buyback shape. The cost is
		// resolved through the DERIVED keyword read (e.offspringCost), so a
		// layer-6 grant (Zinnia) charges the GRANTED parameter, and a stale
		// option whose keyword is gone pays the plain base cost rather than
		// stranding. The offer gate priced the SAME derived read
		// (rules/legal.go's hand and command-zone walks), so the two stages
		// cannot disagree.
		if oc, ok := e.offspringCost(id); ok {
			cost = cost.Plus(oc)
		}
	case "replicated":
		// Replicate (CR 702.55a): the mode marks the intent to pay the
		// optional replicate cost. The PAYMENT COUNT is a cast announcement
		// of its own (CR 601.2b), settled by replicateAsk before the
		// Convoke/X stages and folded into cost there, so a declined count
		// leaves an exactly plain cast and the offer gate's own base+1
		// composition is never silently charged for a different count. The
		// parameter itself is captured onto the pendingCast after its
		// construction (the suspend block below), keeping this switch's
		// cost-folding contract intact.
	case "multikicked":
		// Multikicker (CR 702.43): the same shape as "replicated" above --
		// the mode marks the intent to pay the optional multikicker cost;
		// the PAYMENT COUNT is settled by multikickAsk before the Convoke/X
		// stages and folded into cost there, so this case folds NOTHING. A
		// declined count leaves an exactly plain cast (modeFlags maps this
		// mode to ""), and the pay-time CastInfo rides the trailing
		// FlagMultikicked event.
	case "harmonize":
		if hc, ok := harmonizeCost(f); ok {
			cost = hc
		}
	case "suspend":
		if sc, ok := suspendCost(f); ok {
			cost = sc.cost
		}
	case "suspend_cast":
		cost = Cost{}
	case "defeat_cast":
		// CR 310.11: the defeated battle's owner casts the back face without
		// paying its mana cost.
		cost = Cost{}
	case "plot":
		// CR 701.34a: the plot ACTION pays the K:Plot colon parameter. Not a
		// cast: payCast's plot branch intercepts before the stack push, and
		// continueCast never reaches the target/push stages for this mode.
		if raw, ok := f.KeywordParam("Plot"); ok {
			cost = ParseCost(raw)
		}
	case "plot_cast":
		// CR 701.34d: the plotted card's later cast is free -- no mana cost,
		// no raises; targets and resolution run the ordinary stages.
		cost = Cost{}
	case "prepared_copy":
		// CR 722.3c: the prepared designation's exile copy is cast as a
		// copy of the prepare spell -- free, with targets and resolution
		// running the ordinary stages, exactly like a plotted card's cast.
		cost = Cost{}
	case "foretell":
		// CR 702.126a: the Foretell ACTION pays {2} and exiles the card face
		// down -- never the keyword's own colon parameter, which prices the
		// LATER cast (the foretell_cast case below).
		cost = Cost{Generic: 2}
	case "foretell_cast":
		// CR 702.126a: use the explicit keyword cost, or the printed-cost
		// reduction carried by an effect's ForetoldCost$ designation.
		if fc, ok := foretellCost(f); ok {
			cost = fc
		} else {
			// The option walk fails closed for this shape; retain a harmless
			// fallback for stale options submitted after the designation changed.
			cost = Cost{Generic: 2}
		}
	case "airbend_cast":
		// CR 701.65a: the airbent card's recast pays {2} rather than its mana
		// cost. Cost modifiers (CR 601.2f) apply later in manaToPay, exactly
		// like the other alternative-cost recasts.
		cost = Cost{Generic: 2}
	case "flashback":
		cost = e.flashbackCost(id)
	case "mayplay":
		// rules/mayplay.go granted this play from a non-hand zone. The
		// printed cost is paid (the default below) unless the granting
		// static said MayPlayWithoutManaCost$ True, in which case the mana
		// part is free while non-mana additional costs still apply
		// (CR 118.9) -- so this mode folds into the withSpellAbilityExtras
		// condition below, exactly like a plain cast. CR 118.3a: the
		// granting static's RaiseCost$ surcharge is then added on top
		// through the SAME helper the offer walk (legal.go's may-play spell
		// walk) used, so the offered cost and the charged cost structurally
		// cannot disagree. A raise the helper could not price leaves
		// mayPlayGrant withholding the card; the cast never reaches here.
		// A MayPlayText$-typed option names its permission, so a card cast
		// through ONE of several matching statics pays exactly that
		// static's riders (mayPlayPermFreeRaise); an untyped option keeps
		// the aggregate read.
		free, raise, hasRaise, priced := e.mayPlayPermFreeRaise(p, id, opt.MayPlayPerm)
		if free {
			cost = Cost{}
		}
		if hasRaise && priced {
			cost = cost.Plus(raise)
		}
	case "miracle":
		// Task 18: a Miracle cast pays the printed Miracle cost (CR 702.93d) in
		// place of the card's normal cost. KeywordParam is read off the face;
		// a missing keyword (offer routed here only from a Miracle offer, and
		// only while the card is in hand) falls back to the empty cost so a
		// stale cast cannot strand.
		if mc, ok := f.KeywordParam("Miracle"); ok {
			cost = ParseCost(mc)
		} else {
			cost = Cost{}
		}
	case "escape":
		if ec, ok := e.escapeCost(id); ok {
			cost = ec
		} else {
			cost = Cost{}
		}
	case "retrace":
		// Retrace (CR 702.81a): a graveyard cast paying the printed mana cost
		// PLUS the additional discard-a-land cost -- never a substitution, so
		// the printed base (cost's rawBaseCost seed) stays and only the
		// additional part is folded on. The offer gate priced exactly this
		// composition (legal.go's graveyard walk) and proved a land payable;
		// a stale option whose keyword is gone folds nothing, degrading to a
		// plain cast rather than charging a discard that was never offered.
		// The derived keyword (e.HasKeyword), exactly what the offer gate
		// reads: a continuous grant (Six's "nonland permanent cards in your
		// graveyard have retrace") is not on the printed face, and reading
		// f here offered the grant's cast but charged no discard -- a free
		// graveyard recast loop (fuzz batch6 line 1, Jeweled Lotus).
		if e.hasKeywordH(id, kwhRetrace) {
			cost = cost.Plus(retraceExtra())
		}
	case "jumpstart":
		// Jump-start (CR 702.84a): a graveyard cast paying the printed mana
		// cost PLUS the additional discard-a-card cost -- never a
		// substitution, exactly the retrace shape. The offer gate priced this
		// composition and proved a card payable; the same stale-option
		// degradation applies (a jumpstart mode whose keyword is gone folds
		// nothing rather than charging an unoffered discard).
		if e.hasKeywordH(id, kwhJumpStart) {
			cost = cost.Plus(jumpstartExtra())
		}
	case "evoked", "dashed", "overloaded", "warped", "madness", "bestowed", "blitzed":
		// Grant instances use distinct modes so their cost remains selectable
		// beside printed Blitz, but share Blitz's cast semantics.
		// The alternative-cost keyword family (altcosts): each mode's cost is
		// the printed keyword parameter in place of the mana cost, exactly the
		// Miracle shape. Evoke and Madness casts come from hand and exile
		// respectively via the pending-trigger/cast-offer machinery; a stale
		// option whose keyword is gone (the face cannot change, so in practice
		// only a hand-built option) falls back to the empty cost rather than
		// charging the printed mana cost.
		head := map[string]string{"evoked": "Evoke", "dashed": "Dash",
			"overloaded": "Overload", "warped": "Warp", "madness": "Madness", "blitzed": "Blitz"}[opt.Mode]
		if opt.Mode == "bestowed" {
			// Bestow goes through the ONE resolver the offer gate used
			// (rules/bestow.go's bestowCost: the colon cut and the Unknown
			// withhold), so the charge and the offer can never disagree about
			// what a bestowed cast costs -- a raw ParseCost here would price
			// hypnotic_siren's ":GainControl" suffix as a phantom generic.
			if bc, ok := bestowCost(f); ok {
				cost = bc
			} else {
				cost = Cost{}
			}
			break
		}
		if opt.Mode == "blitzed" {
			cost = Cost{}
			for _, bc := range e.blitzCosts(p, id) {
				if bc.mode == selectedMode {
					cost = bc.cost
					break
				}
			}
		} else if mc, ok := f.KeywordParam(head); ok {
			cost = ParseCost(mc)
		} else {
			cost = Cost{}
		}
	case "web-slinging":
		// Web-slinging (CR 702.186a-family, Marvel's Spider-Man): the
		// web-slinging cost replaces the mana cost AND the composed Cost
		// carries the mandatory Return<1/Creature.YouCtrl+tapped> additional
		// cost, settled by the ordinary Return machinery (returnAsk asks,
		// payCast moves the chosen permanent to its owner's hand beside the
		// other payments). webSlingingCosts is the ONE reader the offer and
		// this charge call, so the two stages cannot drift; a stale option
		// whose keyword is gone falls back to the empty cost like the keyword
		// family above rather than charging the printed mana cost.
		cost = Cost{}
		for _, wc := range e.webSlingingCosts(p, id) {
			if wc.mode == selectedMode {
				cost = wc.cost
				break
			}
		}
	case "mayhem":
		// Mayhem (the Doom Prevails keyword): a graveyard cast paying the
		// mayhem cost in place of the mana cost -- the alternative-cost
		// substitution family, the Miracle shape. The discard-this-turn
		// provenance gate is the OFFER's gate (legal.go's graveyard walk via
		// mayhemDiscardedThisTurn); the charge only re-reads the cost through
		// the same helper, so offer and charge cannot drift, and a stale
		// option whose keyword is gone falls back to the empty cost like the
		// family above. The mode's flag (state.FlagMayhem) is the whole of
		// what the cast records: Sandman's Quicksand's Card.CastSa
		// Spell.Mayhem condition reads it; there is no exile tail to gate.
		if mc, ok := e.mayhemCastCost(id); ok {
			cost = mc
		} else {
			cost = Cost{}
		}
	case "mutated":
		// Mutate (CR 702.140a): the mutate cast pays the MUTATE cost in place
		// of the mana cost -- the same substitution the offer gate priced
		// (legal.go's offerCastable(p, id, mc, spellScope("mutated"), ...)).
		// Without this case the pendingCast charges the PLAIN mana cost, which
		// only ever passes unnoticed when the two costs are payable from the
		// same pool (Everquill Phoenix's {3}{R} mutate vs {2}{R}{R} plain, both
		// payable from RRRR -- Huntmaster Liger's {2}{W} mutate vs {3}{W} plain
		// aborts the cast at the target stage instead). mutateCost applies the
		// same colon-cut and Unknown/X withhold the offer gate used; a stale
		// option whose keyword is gone falls back to the empty cost like the
		// keyword family above.
		if mc, ok := mutateCost(f); ok {
			cost = mc
		} else {
			cost = Cost{}
		}
	case "morphed", "megamorphed", "disguised":
		// Morph / Megamorph / Disguise (CR 702.37a/702.168a/702.169a): the
		// face-down cast pays {3} in place of the mana cost -- the same
		// substitution shape the alternative-cost family below charges
		// (miracle and friends). The keyword's own colon parameter is the
		// LATER turn-face-up cost, never paid now; it stays printed on the
		// face, and the pay-time CastInfo's mode flag (modeFlags) records
		// which family rode so the later turn-face-up action can validate
		// and pay against it. The offer gate (rules/legal.go's hand walk)
		// priced this same {3} through spellScope(fam)'s modifiers, so the
		// charge and the offer cannot disagree, and the fixed {3} has no
		// keyword parameter to fall back on (a stale option degrades to a
		// still-legal {3} cast rather than a free one).
		cost = Cost{Generic: 3}
	case "mayflash":
		// MayFlashCost (CR 702.8, the "as though it had flash" alternate
		// cast): the printed mana cost is paid PLUS the keyword's colon
		// parameter -- the oracle wording is "pay {2} MORE to cast it", so
		// this is base.Plus(extra), never a substitution. The offer gate
		// (legal.go's hand walk) priced exactly this composition, so a stale
		// option whose keyword is gone folds nothing rather than charging a
		// cost the gate never proved payable. The extra's non-mana parts
		// (tapXType<Tegwyll's Scouring>, Behold<Molten Exhale>) are settled
		// by the ordinary tap/choice-cost machinery after this fold.
		if mc, ok := mayflashExtraCost(f); ok {
			cost = cost.Plus(mc)
		}
	case "emerged":
		// Emerge (CR 702.118a): the emerge cast pays the printed K:Emerge cost
		// in place of the mana cost AND sacrifices a creature, whose mana
		// value reduces the cost. The reduction is NOT applied here -- the
		// creature is not chosen until sacAsk settles the Sac part -- so this
		// arm composes the emerge cost with the mandatory sacrifice and marks
		// the cast; applyEmergeReduction folds the chosen creature's mana value
		// out of pc.cost once the choice is in. A stale option whose keyword is
		// gone falls back to the empty cost like the keyword family above, and
		// without the Sac part the cast is an ordinary (over-charged) emerge;
		// the offer gate only ever routes here with the keyword present.
		if ec, ok := emergeBase(f); ok {
			cost = ec
		} else {
			cost = Cost{}
		}
	}
	// CR 601.2b/f/h: a spell's own SpellAbility may carry an explicit Cost$
	// (Forge's SP Cost) naming an additional cost -- most commonly a
	// sacrifice (Altar's Reap's "1 B Sac<1/Creature>", the CR 601.2h example).
	// The mana part of that Cost$ REPLACES the printed mana (it is the same
	// cost the card already charges), so only its non-mana parts
	// (Sac/Discard/SubCounter/Tap) are additional and fold into the total cost here; a
	// re-added mana part would double charge. Only a plain cast (and the
	// "conspired" mode, whose offer gate priced the same extras -- the
	// replicated/multikicked modes keep the older no-fold divergence) reaches
	// this (pc.ability < 0 and no alternative/flashback recast), and a spell
	// with no SP Cost$ contributes nothing.
	if opt.Mode == "optionalcost" {
		// The offer stores the selected optional part in AltCostIndex's
		// companion-independent mode; legalActions has already proved it payable.
		// The actual non-mana payment is settled by the ordinary cost stages.
		if opt.AltCostIndex <= 0 {
			return
		}
		parts := e.optionalCostViews(e.collectCostStatics(), p, id)
		if opt.AltCostIndex > len(parts) {
			return
		}
		// The offer priced withSpellAbilityExtras(f, convokeBase).Plus(extra)
		// (legal.go), so fold the same SpellAbility Cost$ extras here before
		// the optional part: the charge must match the gate, or a spell that
		// carries BOTH an OptionalCost static and a spell-ability additional
		// cost undercharges by that additional cost. Zero corpus carriers pair
		// the two today, so this is the structural agreement, not a behaviour
		// change (the fold is a no-op without a SpellAbility Cost$).
		cost = withSpellAbilityExtras(f, cost)
		cost = cost.Plus(parts[opt.AltCostIndex-1])
		optionalCost = parts[opt.AltCostIndex-1]
	}
	if opt.AltCostIndex == 0 && (opt.Mode == "" || opt.Mode == "mayplay" || opt.Mode == "modal_spell" || opt.Mode == "room_alt" ||
		opt.Mode == "adventure_alt" || opt.Mode == "aftermath" || opt.Mode == "split_alt" || opt.Mode == "conspired" || opt.Mode == "casualty" || opt.Mode == "mayflash" || opt.Mode == "retrace" || opt.Mode == "jumpstart" ||
		// CR 702.34a/601.2f: flashback replaces only the mana cost; the
		// spell's own additional cost (Eviscerator's Insight's sacrifice,
		// Electric Revelation's discard) is still paid. The offer gate
		// (legal.go's flashback walk) folds the same extras.
		opt.Mode == "flashback") {
		cost = withSpellAbilityExtras(f, cost)
	}
	// Convoke and Harmonize are announced only after X/mode/pip choices have
	// formed the total cost (convokeAsk). Do not preselect creatures here:
	// doing so let one of those creatures activate a mana ability before its
	// delayed Tap payment.
	// The either-or additional cost (AlternateAdditionalCost) is a CHOICE,
	// not a fixed component, so the parts are only captured here and the ask
	// (altAddAsk) folds the chosen part into cost before any other cost stage
	// runs. Only a plain cast carries them: a kicked/surged/etc. cast of the
	// same card pays that mode's cost without recomposing this choice (no
	// corpus card pairs both shapes). The OFFER gate already proved at least
	// one part is payable (legal.go); the ask narrows it to exactly one.
	tax := int32(0)
	if opt.Mode != "foretell" {
		tax = e.commanderTaxAmount(p, id)
	}
	scope := spellScope(opt.Mode)
	if opt.Mode == "foretell" {
		scope = foretellScope()
	}
	mods := e.costModifiers(p, id, scope)
	// The SVar-fixed PayLife<X> conversion (fixLifeXCost) -- the same helper
	// offerCastable shaped the offered cost with, so the stored cost and the
	// gated charge agree. A fixed face's value folds into Life here; the
	// withheld (unresolvable-body) shape cannot reach this line through any
	// offer gate, and a stale option that does degrades to a no-op before
	// anything is pushed or charged.
	converted, ok := e.fixLifeXCost(p, id, cost)
	if !ok {
		return
	}
	cost = converted
	// A RaiseCost static's non-mana Cost$ (Soul Immolation's `Cost$ Blight<X>`,
	// Grafted Identity's creature sacrifice, the Champion cycle's BeholdExile)
	// is carried in mods.extra so the OFFER gate (composedOfferCost, which
	// applies mods) enforces it and the CHARGE prices it. The pending cast's
	// own cost must carry it too, because every non-mana cost stage -- xAsk's
	// X announcement, sacAsk, blightCostAsk, the settle and costAnnouncesX --
	// reads pc.cost, not the composed charge. Fold it in here once and drop
	// it from mods so manaToPay's mods.apply cannot count the same part twice.
	cost, ok = e.foldRaiseExtra(p, id, cost, &mods)
	if !ok {
		return
	}
	if opt.AltCostIndex == 0 && opt.Mode == "" {
		pcAlt := altAddCostParts(f)
		e.cast = &pendingCast{player: p, card: id, from: from, mode: opt.Mode, ability: -1,
			cost: cost, faceBefore: faceBefore, mods: mods, taxGeneric: tax, altAddParts: pcAlt, optionalCost: optionalCost}
	} else {
		e.cast = &pendingCast{player: p, card: id, from: from, mode: opt.Mode, ability: -1,
			cost: cost, faceBefore: faceBefore, mods: mods, taxGeneric: tax, optionalCost: optionalCost}
	}
	// Emerge (CR 702.118a): sacAsk folds the chosen sacrifice's mana value out
	// of pc.cost once the mandatory creature sacrifice is settled. The mark is
	// set here, beside the cost the switch composed, so the charge and the
	// reduction can never disagree about what cast they belong to.
	if opt.Mode == "emerged" {
		e.cast.emerge = true
	}
	// Morph family (CR 702.37a/702.168a/702.169a): the mark is set beside
	// the cost the switch composed, so the {3} charge and the face-down
	// entry marker can never disagree about which cast they belong to.
	if opt.Mode == "morphed" || opt.Mode == "megamorphed" || opt.Mode == "disguised" {
		e.cast.faceDown = true
	}
	// Escalate (the modal additional cost "pay this for each mode chosen
	// beyond the first"): the cost is carried as its raw keyword parameter
	// and re-parsed by the cast_modes answer handler -- the same
	// string-survives-Clone convention the replicate capture below documents.
	// Escalate rides the plain cast (no separate option exists): the CR
	// 601.2b mode answer's chosen count is what prices it, so the capture is
	// mode-blind.
	if s, ok := f.KeywordParam("Escalate"); ok && strings.TrimSpace(s) != "" {
		e.cast.escalateParam, e.cast.escalateSet = s, true
	}

	// Strive (CR 702.52, "this spell costs <cost> more for each target
	// beyond the first"): the same raw-parameter capture as Escalate. Strive
	// rides the plain cast (no separate option exists): the CR 601.2c target
	// answer's chosen count is what prices it, so repriceForTargets folds it
	// into pc.cost once the targets are known.
	if s, ok := f.KeywordParam("Strive"); ok && strings.TrimSpace(s) != "" {
		e.cast.striveParam, e.cast.striveSet = s, true
	}

	// kw:MayFlashSac (CR 702.8): capture the rider's condition now, before
	// CR 601.2a puts the spell on the stack, so the empty-stack half of
	// sorcerySpeed is the board the caster announced into rather than this
	// spell's own push. An ability proposal (pc.ability >= 0) never reads it:
	// payCast's flag arm is gated on !pc.isAbility().
	e.cast.offSorcery = e.offSorceryAtCast(p)
	// The announce-bearing alternative (the Shoal cycle) rides the selected
	// cast SA into the transaction: xAsk's announce arm and exAsk's binding
	// read the captured value.
	if announceAlt != nil && announceAlt.announce != "" {
		e.cast.announceX = announceAlt.announce
	}
	// CR 903.8: the commander tax, applied to whatever cost this cast pays
	// (the base/alternative/kicked/flashback/surged/miracle cost resolved
	// above) -- the exact same commanderTaxFor the command-zone offer in
	// legal.go gated castable on, over the same board, so this charge and
	// that offer can never disagree. For a command-zone commander this is the
	// plain base + the tax; for every other card/zone it passes cost through
	// unchanged (commanderTaxFor is a no-op outside the Commander format and
	// off the command zone). It lands AFTER cost modifiers and any keyword
	// recast, so an additional cost is never reduced by them, in line with how
	// Kicker's own additional cost composes.
	//
	// The tax is captured as a separate generic amount (taxGeneric) rather
	// than folded into cost, so manaToPay adds it AFTER the 601.2f modifiers
	// and never lets those spill onto it.
	if opt.Mode == "suspend" {
		if sc, ok := suspendCost(f); ok && sc.timeX {
			e.cast.suspendTimeX, e.cast.suspendMinX = true, sc.minTime
		}
	}
	// Replicate (CR 702.55a): the cost is carried as its raw keyword
	// parameter and re-parsed by replicateAsk and the answer handler -- a
	// string survives the intent boundary's pendingCast Clone without
	// deep-copying cost slices, and ParseCost is deterministic.
	if opt.Mode == "replicated" {
		if _, ok := replicateCost(f); ok {
			e.cast.replicateParam, e.cast.replicateSet = f.KeywordParam("Replicate")
		}
	}
	// Multikicker (CR 702.43): the cost is carried as its raw keyword
	// parameter and re-parsed by multikickAsk and the answer handler -- the
	// same string-survives-Clone convention the replicate capture above
	// documents.
	if opt.Mode == "multikicked" {
		if _, ok := multikickerCost(f); ok {
			e.cast.multikickParam, e.cast.multikickSet = f.KeywordParam("Multikicker")
		}
	}
	// Squad (CR 702.66): the per-payment cost is carried as its raw keyword
	// parameter and re-parsed by squadAsk and the answer handler -- the exact
	// string-survives-Clone convention the replicate capture above documents.
	if opt.Mode == "squadded" {
		if _, ok := squadCost(f); ok {
			e.cast.squadParam, e.cast.squadSet = f.KeywordParam("Squad")
		}
	}
	// Conspire (CR 702.78a) is param-less: the mode itself marks the intent
	// and conspireSet records it for conspireAsk. The tap election is posed
	// by conspireAsk (not a cost part -- the fixed "two creatures you control
	// sharing a colour with the spell" has no Cost$ spelling), and the taps
	// settle through pc.taps exactly like every other tap cost.
	if opt.Mode == "conspired" {
		e.cast.conspireSet = true
	}
	if opt.Mode == "casualty" {
		if info, ok := e.casualtySpec(id); ok {
			if info.variable {
				e.cast.casualtyVariable = true
				e.cast.casualtyN = 0
			} else {
				e.cast.casualtyN = info.threshold
			}
		} else {
			e.cast.casualtyN = -1
		}
	}
	// CR 401.5's MayPlayIgnoreColor$ rider: "you may spend mana as though it
	// were mana of any color to cast it". Recorded from the grant the offer
	// gate consulted while the card was still in the granted zone.
	if opt.Mode == "mayplay" {
		e.cast.mayPlayIgnore = e.payerGrantsIgnoreColor(p, id)
		e.cast.mayPlayIgnoreType = e.payerGrantsIgnoreType(p, id)
		e.cast.mayPlayRemembered = e.mayPlayManaConvertRemembered(p, id)
		e.cast.mayPlayPerm = opt.MayPlayPerm
		e.cast.mayPlayHosts = e.mayPlayHostsCovering(p, id)
		e.cast.mayPlayHostsSet = true
	}
	if e.cast != nil {
		e.cast.costRemembered = e.costRememberedCapture(id)
	}
	if selection != nil && e.cast != nil {
		e.cast.payment = &plannedCastPayment{actionID: selection.ActionID, plan: decision.ClonePaymentPlan(selection.Plan)}
	}
	if announced && e.cast != nil {
		e.cast.announced = true
	}
	e.continueCast()
}

// convertedManaCostToken matches Forge's ConvertedManaCost placeholder inside
// a PlayCost$ token, case-insensitively (the corpus spells it exactly this
// way; the case fold costs nothing).
var convertedManaCostToken = regexp.MustCompile(`(?i)convertedmanacost`)

// pricePlayCost prices a Play effect's PlayCost$ token for one chosen card:
// the ConvertedManaCost placeholder is substituted with the card face's mana
// value (Amped Raptor's "an amount of {E} equal to its mana value") and the
// result is parsed with the ordinary cost grammar -- PayEnergy<N>, PayLife<N>,
// a fixed generic, and Discard<N/Spec> all land in the Cost fields the cast
// flow already asks and charges. SuspendCost is resolved from the chosen
// card's K:Suspend before that ordinary grammar. A token the grammar reports as
// unmodelled is NOT degraded the way a printed cost's malformed token would be:
// PlayCost$ is an ALTERNATIVE to the mana cost (CR 118.9 "rather than paying
// its mana cost"), so degrading it to one generic would still charge the
// player full price -- the caller hard-declines instead, ParseUnlessCost-style.
func pricePlayCost(f *cards.Face, token string) (Cost, bool) {
	// SuspendCost is the one PlayCost token whose value is another keyword's
	// cost rather than a standalone cost expression. Read the chosen card's
	// printed K:Suspend, exactly as the Face of Boe's "pay its suspend cost"
	// text requires; an absent or malformed Suspend keyword remains a hard
	// decline.
	if strings.EqualFold(strings.TrimSpace(token), "SuspendCost") {
		info, ok := suspendCost(f)
		if !ok || info.timeX {
			return Cost{}, false
		}
		return info.cost, len(info.cost.Unknown) == 0
	}
	s := convertedManaCostToken.ReplaceAllString(token, strconv.FormatInt(int64(f.ManaValue()), 10))
	c := ParseCost(s)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// beginPlay is the rules' hand-off for an answered Play effect. It starts a
// cast from the card's current zone and uses its printed cost unless that
// specific Play SA said WithoutManaCost$ True (Spinerock Knoll grants a free
// cast, while Conduit of Worlds requires payment) or carries a PlayCost$
// alternative (Amped Raptor's energy cast), which REPLACES the mana cost
// only -- additional costs and cost modifiers ride exactly as an ordinary
// cast's do (CR 118.9 / 601.2f). The card must still be on the stack of the
// suspended Play resolution when this runs; a malformed answer degrades to a
// logged no-op rather than panic. replaceGraveyard carries the Play SA's
// ReplaceGraveyard$ Exile rider (task replplay1): true stamps the played
// spell's pay-time CastInfo with state.FlagReplaceGraveyard so the resolution
// reader exiles it instead of the graveyard.
func (e *Engine) beginPlay(p state.PlayerID, id state.ObjID, withoutManaCost bool, playCost string, replaceGraveyard, copyCard bool) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		e.emit(events.Event{Kind: events.Note, Player: p, Text: "Play found no card to play"})
		return
	}
	if o.Face().IsLand() {
		// A "play" permission can play a land, but it does not grant an
		// additional land drop. Lands never become spells or enter the stack.
		if int(p) >= len(e.G.Players) || e.G.Players[p].LandsPlayed >= 1 {
			e.emit(events.Event{Kind: events.Note, Player: p, Text: "Play cannot use an additional land drop"})
			return
		}
		e.cast = &pendingCast{player: p, card: id, from: o.Zone, mode: "land", ability: -1}
		e.continueCast()
		return
	}
	// CR 601.3: a player can begin to cast a spell only if a rule or effect
	// allows it and no rule or effect prohibits it. The ordinary cast OFFER
	// runs that prohibition gate (castRestricted -- the CantBeCast family:
	// Teferi's "only any time they could cast a sorcery", Void Winnower's
	// even-mana lockout, a card's own "you can't cast this spell unless
	// ..."). A Play effect begins its cast WITHOUT passing the offer walk,
	// so the free-cast routes (cascade and Discover's election, an impulse
	// "you may play it") would otherwise cast a prohibited card for free.
	// Refuse here with a Note; the card stays in the zone the Play found it
	// in (cascade's chained tail then bottoms it). A play that cannot name
	// a castable card is not an error -- CR 601.3 simply withholds the
	// cast, and the Play's answer is consumed either way.
	if e.castRestricted(p, id) {
		e.emit(events.Event{Kind: events.Note, Player: p, Obj: id,
			Text: "the play cannot cast a restricted card"})
		return
	}
	// Cipher's CopyCard$ True Play rider: the encoded card is NOT moved --
	// a COPY of it is placed on the stack and cast, and the original stays in
	// its zone. Mint through a logged event, never by mutating Game here:
	// replay must derive the same object ID before the subsequent PutOnStack.
	// The copy starts in the temporary library holding zone, which becomes
	// the cast's From; CR 707.12 allows this copy of a card to be cast.
	if copyCard {
		copyID := e.G.NextID
		e.emit(events.Event{Kind: events.StackCopy, Obj: id, Player: p,
			Text: "copy card for play"})
		o = e.G.Obj(copyID)
		if o == nil || !o.IsCopy {
			return
		}
		id = copyID
	}
	cost := e.rawBaseCost(p, id)
	if withoutManaCost {
		cost = Cost{}
	} else if playCost != "" {
		// A PlayCost$ alternative replaces the mana cost; an unpriceable
		// token is a hard DECLINE, never a mana fallback -- the player is
		// never charged full mana for a "rather than" alternative.
		alt, ok := pricePlayCost(o.Face(), playCost)
		if !ok {
			e.emit(events.Event{Kind: events.Note, Player: p,
				Text: "Play cannot price its alternative cost (" + playCost + "); the play is declined"})
			return
		}
		// The alternative's non-mana parts must be payable the way an
		// offered cast's would be (the energy total, the discard
		// candidates): a YES answer the payment cannot settle is declined
		// with a Note, not begun and short-changed at the settle.
		if !e.nonManaCastable(p, id, alt, false) {
			e.emit(events.Event{Kind: events.Note, Player: p,
				Text: "The alternative cost cannot be paid (" + playCost + "); the play is declined"})
			return
		}
		if alt.Life > e.G.Players[p].Life {
			e.emit(events.Event{Kind: events.Note, Player: p,
				Text: "The alternative cost cannot be paid (" + playCost + "); the play is declined"})
			return
		}
		cost = alt
	}
	// A normal Play cast pays its printed mana cost; a free Play cast does
	// not; a PlayCost$ cast pays the alternative. All three still pay
	// non-mana additional costs, exactly as an ordinary cast does (CR
	// 118.9 / 601.2f). The cost is stored RAW (no cost modifiers folded):
	// RaiseCost/ReduceCost ride pc.mods and manaToPay applies them after {X}
	// is folded, the same shape beginCast stores.
	cost = withSpellAbilityExtras(o.Face(), cost)
	converted, ok := e.fixLifeXCost(p, id, cost)
	if !ok {
		// The offer gate withheld this cost; a stale Play degrades to a no-op.
		return
	}
	cost = converted
	mods := e.costModifiers(p, id, spellScope(""))
	// A RaiseCost static's non-mana Cost$ rides mods.extra; fold it into the
	// pending cost and drop it from mods so the charge cannot double it (the
	// same agreement beginCast makes).
	cost, ok = e.foldRaiseExtra(p, id, cost, &mods)
	if !ok {
		return
	}
	e.cast = &pendingCast{player: p, card: id, from: o.Zone, mode: "play", ability: -1,
		cost: cost, mods: mods, replaceGraveyard: replaceGraveyard}
	e.continueCast()
}

// applyDredge performs a dredged replacement of a draw: mill N cards (N = the
// dredge card's Dredge number) from p's library into the graveyard, then move
// the dredge card from p's graveyard to their hand. The ordinary draw was
// skipped by choosing option 0 in DrawFor's dredge ask.
func (e *Engine) applyDredge(p state.PlayerID, dredgeID state.ObjID) {
	o := e.G.Obj(dredgeID)
	if o == nil || o.Face() == nil {
		return
	}
	n := int32(0)
	if v, ok := o.Face().KeywordParam("Dredge"); ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			n = int32(parsed)
		}
	}
	lib := e.G.Zone(state.ZLibrary, p)
	// A stale or malformed answer must not turn an illegal insufficient-library
	// dredge into a partial mill: CR 702.55 requires all N cards.
	if n <= 0 || int(n) > len(lib) {
		return
	}
	for _, id := range lib[:n] {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
	}
	if dredgeID != 0 && o.Zone != state.ZHand {
		e.emit(events.Event{Kind: events.MoveZone, Obj: dredgeID,
			From: o.Zone, To: state.ZHand})
	}
}

// resumeOrdinaryDraw re-emits the ordinary draw a declined dredge skipped.
// DrawFor posed the dredge ask and suspended; a "no" (option 1) means the
// player draws as normal, which is exactly the Draw event DrawFor would have
// emitted had no dredger been in the graveyard. Only the library's top card
// moves; a decline with an empty library is a loss, checked by the SBA.
func (e *Engine) resumeOrdinaryDraw(p state.PlayerID) {
	lib := e.G.Zone(state.ZLibrary, p)
	if len(lib) == 0 {
		e.playerLoses(p, loseReasonMilled, "drew from an empty library")
		return
	}
	e.emit(events.Event{Kind: events.Draw, Player: p, Obj: lib[0],
		From: state.ZLibrary, To: state.ZHand, Secret: true})
}

// continueCast runs the cast flow's stages in order -- announced
// Convoke/Harmonize contributions, X, Delve, each Sac and Discard part --
// stopping (and returning) the instant a stage asks a KChoose;
// commitCast runs once every stage has settled. A nil e.cast (a chooseCast
// answer arriving with no flow in progress, only reachable from a
// hand-built decision) is dropped rather than panicked on, mirroring
// castAnswer's own guard.
func (e *Engine) continueCast() {
	if e.cast == nil {
		return
	}
	// Mutate (CR 702.140b): the over/under placement choice is announced
	// before payment the same way the replicate count is, so the pay-time
	// CastInfo can carry FlagMutatedTop.
	if e.mutatePlaceAsk() {
		return
	}
	if e.altAddAsk() {
		return
	}
	// CR 702.168: the Gift promise is announced as a free cast-time choice.
	// It must settle before CR 601.2c's target ask, because a TargetMin$/Max$
	// X bound of Count$PromisedGift.2.1 resolves when that ask is built -- a
	// promise settled after targeting would be invisible to it.
	if e.giftAsk() {
		return
	}
	// A RaiseCost part counted by a named announcement (the March cycle,
	// Explosive Singularity) announces its count before every stage that
	// pays or prices it: the tap election below, xAsk's discounted X bound
	// and the exile pick.
	if e.namedAnnounceAsk() {
		return
	}
	if e.forageAsk() || e.revealCostAsk() || e.revealCostOrChooseAsk() || e.beholdCostAsk() || e.tapPermanentCostAsk() || e.blightCostAsk() {
		return
	}
	// CR 601.2b: the replicate count (CR 702.55a's optional additional cost,
	// paid any number of times) is announced before Convoke/Harmonize and X,
	// whose asks must see and bound against the composed total.
	if e.replicateAsk() {
		return
	}
	// CR 601.2b: the multikicker count (CR 702.43's optional additional cost,
	// paid any number of times) is announced the same way -- no Kicker
	// carrier pairs both keywords (measured over the corpus), so the two
	// asks never coexist on one cast.
	if e.multikickAsk() {
		return
	}
	// CR 601.2b: the squad count (CR 702.66's optional additional cost, paid
	// any number of times) is announced the same way. No corpus carrier pairs
	// Squad with Replicate/Multikicker/Kicker (measured over the 15 K:Squad
	// files), so the asks never coexist on one cast.
	if e.squadAsk() {
		return
	}
	// CR 702.78a: the Conspire tap election (two untapped creatures that
	// share a colour with the spell) is posed before Convoke/X so an elected
	// creature cannot also be announced as a payment source. See conspireAsk.
	if e.conspireAsk() || e.casualtyAsk() {
		return
	}
	// CR 601.2b announces Convoke/Harmonize before X: an announced creature
	// contribution is part of the available payment for X, and cannot be used
	// as a mana source in the later mana window.
	if e.convokeAsk() {
		return
	}
	if e.xAsk() {
		return
	}
	// An announced Blight<X> part's count is the announced X, so its pick is
	// deferred past xAsk (the Sac<X/Spec> / SubCounter<X/Kind> shape) -- the
	// pre-xAsk blightCostAsk above skips it while !pc.xDone.
	if e.blightCostAsk() {
		return
	}
	if e.delveAsk() {
		return
	}
	if e.sacAsk() {
		return
	}
	// The SubCounter cost parts whose removal-target field names a filter ask
	// their payer which permanent the counters come off (Ghave's "remove a
	// +1/+1 counter from a creature you control"), after the announced X
	// exists so the candidate set can require that many counters.
	if e.subCounterAsk() {
		return
	}
	if e.discardAsk() {
		return
	}
	if e.exAsk() {
		return
	}
	if e.returnAsk() {
		return
	}
	if e.putToLibAsk() {
		return
	}
	// CR 601.2b: the ExiledMoveToGrave cost pick (Shelob's "put a creature
	// card exiled with Shelob into its owner's graveyard") runs beside the
	// other non-mana component asks, after the PutToLib ask.
	if e.moveGraveAsk() {
		return
	}
	// Suspend does not put a spell on the stack: its alternate action pays
	// the keyword cost and exiles the card with time counters. Targets are
	// chosen only when its later free cast is announced.
	if e.cast.mode == "suspend" {
		e.payCast()
		return
	}
	// Foretell (CR 702.126a) is the same shape: the {2} special action is not
	// a cast -- no stack push, no targets; the card is exiled face down and
	// its later foretell-cost cast announces its own targets.
	if e.cast.mode == "foretell" {
		e.payCast()
		return
	}
	// Plot (CR 701.34a) is the same shape again: the alternative action is
	// not a cast -- no stack push, no targets; the card is exiled with time
	// counters and its later free cast announces its own targets at sorcery
	// timing.
	if e.cast.mode == "plot" {
		e.payCast()
		return
	}
	// CR 601.2a: the object reaches the stack before the target choice
	// (601.2c) and payment (601.2h). For a spell the cast trigger (601.2i)
	// is held back until payCast; an ability's AbilityPush fires no trigger.
	if e.pushCast() {
		return
	}
	// castprov3: a provenance-keyed cost modifier (Bilbo's
	// "!wasCastFromYourHand" ReduceCost) is unresolvable before CR 601.2a's
	// push — the offer and option-selection snapshots both denied it (full
	// price, the fail-closed direction) because the priced card had no cast
	// in the log yet. Now the PutOnStack is in the log: when the selection
	// pass evaluated such a static (e.costProvenanceSeen, the
	// noCounterSpend-style transient capture), re-price the pending cast so
	// the payment takes the honest reduction. For every other cast the
	// recompute is byte-identical to the offer snapshot (both are the
	// nil-target base snapshot), so no existing price — and no chain head —
	// moves.
	if pc := e.cast; pc != nil && pc.pushed && !pc.provenanceRepriced && e.costProvenanceSeen {
		pc.provenanceRepriced = true
		pc.mods = e.costModifiers(pc.player, pc.card, spellScope(pc.mode))
	}
	// FlagSuspend is exile provenance, not cast-time state. Clear it when the
	// mandatory free cast starts so a later unrelated exile move cannot revive
	// an old suspension. Plot needs no equivalent: its designation is
	// Object.PlottedTurn, and the exile-departure clear in events.Apply's Move
	// already drops it as the card leaves exile for the stack (CR 701.34c).
	if e.cast.mode == "suspend_cast" && !e.cast.suspendCastClear {
		e.emit(events.Event{Kind: events.CastInfo, Obj: e.cast.card})
		e.cast.suspendCastClear = true
	}
	// CR 601.2b: a modal spell announces its modes after reaching the stack
	// and before targets are chosen or costs are paid. The answer is cached on
	// the proposed spell so targetAsk can inspect the selected mode and
	// resolution can execute it without asking again.
	if e.castModeAsk() {
		return
	}
	// CR 601.2b: announce how each hybrid and Phyrexian pip is paid -- which
	// half of a hybrid, whether a Phyrexian pip is paid with life -- before
	// targets (601.2c) and payment (601.2h). Runs as one decision per pip.
	if e.manaAsk() {
		return
	}
	// An Optional$ ManaConvert permission is a real CR 601.2 choice. It is
	// asked after pip announcements, before targets, and the selected arm is
	// then used consistently by target affordability and final payment.
	if e.manaConvertAsk() {
		return
	}
	// CR 601.2c: choose targets, now that the object is on the stack. An SA
	// with no target (or a zero-minimum one with no legal candidate) asks
	// nothing and the flow proceeds to the chain pre-asks and payCast.
	if e.targetAsk() {
		return
	}
	// alltargeted1: the root had no targets of its own (or none legal), but
	// the SubAbility$ chain may still declare targeting bodies Forge asks
	// before payment, and the CollectEvidence amount reads the union.
	if e.postTargetAsks(e.cast) {
		return
	}
	e.payCast()
}

// altAddAsk poses the AlternateAdditionalCost either-or choice: which ONE
// alternative additional cost this cast pays (CR 601.2h). It runs FIRST in
// continueCast -- before the {X} ask -- because the chosen part folds into
// cost and every later stage (Delve, sac, discard, the mana window and the
// final payment) must see the total it will actually charge. ALL parts are
// offered, the payable ones FIRST (deterministically: script order within
// each group), so an automated seat taking the first option always takes one
// it can pay; a seat that picks an unpayable part walks into a later stage's
// abort (CR 733.1 reversal). When NO part is payable the flow aborts (the
// offer gate already proved one was payable at offer time, so this is a
// board that changed under the flow).
func (e *Engine) altAddAsk() bool {
	pc := e.cast
	if pc == nil || pc.altAddDone || len(pc.altAddParts) == 0 {
		return false
	}
	pc.altAddDone = true
	if len(pc.altAddParts) == 1 {
		part := ParseCost(pc.altAddParts[0])
		if !e.castable(pc.player, pc.card, pc.cost.Plus(part), pc.isAbility()) {
			e.abortCast(pc, "additional cost no longer payable; cast aborted", true)
			return true
		}
		pc.cost = pc.cost.Plus(part)
		return false
	}
	payable := make([]int, 0, len(pc.altAddParts))
	unpayable := make([]int, 0, len(pc.altAddParts))
	for i, part := range pc.altAddParts {
		if e.castable(pc.player, pc.card, pc.cost.Plus(ParseCost(part)), pc.isAbility()) {
			payable = append(payable, i)
		} else {
			unpayable = append(unpayable, i)
		}
	}
	order := append(append([]int(nil), payable...), unpayable...)
	if len(payable) == 0 {
		e.abortCast(pc, "additional cost no longer payable; cast aborted", true)
		return true
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose an additional cost to cast " + e.targetName(pc.card), Source: pc.card}
	for _, i := range order {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "altaddcost",
			Label: capitaliseFirst(costPhrase(ParseCost(pc.altAddParts[i]))), Amount: i})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// giftAsk poses the CR 702.168 Gift election: "You may promise an opponent a
// gift as you cast this spell." The election is FREE -- no mana, no card --
// and is a single KChoose offering a decline plus one option per legal
// opponent (CR 702.168a: the caster chooses WHICH opponent), so a two-seat
// game offers exactly one opponent plus the decline. The answer goes nowhere
// here: it rides pc and pushCast folds it onto the stack object as an
// events.GiftPromise, the replay-derived home the target bound and the
// resolution read. Only a face printing K:Gift with a GiftAbility SVar poses
// the ask, so no unrelated cast gains a decision. The one-home answer rule
// is the generic KChoose contract (Min 1 / Max 1 over the offered options),
// which already drives Decision.Validate; the bot's own answer is proven
// against it in botpolicy's gift test, so no parallel rule can drift.
func (e *Engine) giftAsk() bool {
	pc := e.cast
	if pc == nil || pc.giftDone || pc.isAbility() {
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil || !o.Face().HasKeyword("Gift") {
		return false
	}
	if _, ok := o.Face().SVars["GiftAbility"]; !ok {
		return false
	}
	pc.giftDone = true
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Promise a gift?", Source: pc.card}
	d.Options = append(d.Options, decision.Option{Index: 0, Kind: "gift_decline",
		Label: "Don't promise a gift"})
	for _, p := range e.G.AliveFrom(pc.player) {
		if p == pc.player {
			continue
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "gift_promise",
			Player: p, Label: "Promise " + tossName(e.G, p) + " a gift"})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

func (e *Engine) forageAsk() bool {
	pc := e.cast
	if pc == nil || !pc.cost.Forage || pc.forageDone {
		return false
	}
	pc.forageDone = true
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose how to forage", Source: pc.card}
	if len(e.G.Zone(state.ZGraveyard, pc.player)) >= 3 {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "forage_exile", Label: "Exile three cards from your graveyard"})
	}
	for _, id := range e.costCandidates(pc.player, pc.card, state.ZBattlefield, "Food.YouCtrl", false, false) {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "forage_food", Obj: id,
			Label: "Sacrifice " + e.targetName(id)})
	}
	if len(d.Options) == 0 {
		e.abortCast(pc, "forage no longer payable; cast aborted", true)
		return true
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

func (e *Engine) revealCostAsk() bool {
	pc := e.cast
	for pc.revealPart < len(pc.cost.Reveal) {
		part := pc.cost.Reveal[pc.revealPart]
		// A whole-hand Reveal (Reveal<N/Hand>) settles without asking: it
		// reveals the payer's whole hand AS IT STANDS at payment. For a CAST
		// the spell being paid for has already moved to the stack (CR 601.2a)
		// and is no longer part of the hand; for an ABILITY activation the
		// source still sits in the hand and IS part of the hand it reveals.
		// Zero cards is a legal payment (CR 701.20a); emitChoiceCosts
		// announces it loudly via pc.revealedEmptyHand.
		if isWholeHandRevealSpec(part.Spec) {
			var hand []state.ObjID
			excludeSource := !pc.isAbility()
			for _, oid := range e.G.Zone(state.ZHand, pc.player) {
				if excludeSource && oid == pc.card {
					continue
				}
				hand = append(hand, oid)
			}
			pc.reveals = append(pc.reveals, hand...)
			for range hand {
				pc.revealHandArm = append(pc.revealHandArm, true)
			}
			if len(hand) == 0 {
				pc.revealedEmptyHand = true
			}
			pc.revealPart++
			continue
		}
		// Relational SameColor (Illuminated Folio): SameColor matches no card
		// as a filter, so the candidates are every eligible hand card and
		// the ask itself carries the constraint -- decision.SetPropShared
		// over each option's DERIVED colour tokens, so Decision.Validate
		// (seats) and botpolicy's Clamp (the bot) reject a non-sharing
		// answer through the same rule the offer gate measured. The exactly-N
		// auto-settle sits behind the capacity check, so it can never pay an
		// illegal reveal.
		if isSameColorRevealSpec(part.Spec) {
			cands, sets := e.sameColorRevealSets(pc.player, pc.card, !pc.isAbility())
			if decision.SetPropCapacity(decision.SetPropShared, sets) < int(part.N) {
				e.abortCast(pc, "reveal cost no longer payable; cast aborted", true)
				return true
			}
			if len(cands) == int(part.N) {
				pc.reveals = append(pc.reveals, cands...)
				for range cands {
					pc.revealHandArm = append(pc.revealHandArm, true)
				}
				pc.revealPart++
				continue
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
				Prompt: "Choose cards to reveal", Source: pc.card,
				SetPropMode: decision.SetPropShared}
			for _, id := range cands {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "revealcost", Obj: id,
					SetProps: e.setPropTokens("color", id), Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		}
		candidates := e.costCandidates(pc.player, pc.card, state.ZHand, part.Spec, !pc.isAbility(), false)
		if len(candidates) < int(part.N) {
			e.abortCast(pc, "reveal cost no longer payable; cast aborted", true)
			return true
		}
		if len(candidates) == int(part.N) {
			pc.reveals = append(pc.reveals, candidates...)
			for range candidates {
				pc.revealHandArm = append(pc.revealHandArm, true)
			}
			pc.revealPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose cards to reveal", Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "revealcost", Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// revealCostOrChooseAsk drives the either-or `RevealOrChoose<N/Spec>` parts,
// one at a time, through the same chooseCast suspend/resume shape
// revealCostAsk drives. It is explicit whenever BOTH arms can pay: the payer
// picks the arm (reveal a hand card OR choose a permanent they control) and
// the object, even when each arm has exactly one candidate -- an either-or
// additional cost is a real cast-time election and must not be silently
// resolved by the engine. When only ONE arm can pay, that arm is settled by
// the ordinary reveal rule (auto when exactly N candidates, else ask), and
// when NEITHER arm can pay the cast aborts, exactly as revealCostAsk does.
// The elected objects ride pc.reveals (the shared `Revealed$<Property>` paid
// list) with a parallel pc.revealHandArm marking which arm each came from.
func (e *Engine) revealCostOrChooseAsk() bool {
	pc := e.cast
	for pc.revealOrChoosePart < len(pc.cost.RevealOrChoose) {
		part := pc.cost.RevealOrChoose[pc.revealOrChoosePart]
		hand, battlefield := e.revealOrChooseCandidates(pc.player, pc.card, part)
		handViable := len(hand) >= int(part.N)
		bfViable := len(battlefield) >= int(part.N)
		switch {
		case !handViable && !bfViable:
			e.abortCast(pc, "reveal-or-choose cost no longer payable; cast aborted", true)
			return true
		case handViable && bfViable:
			// Both arms can pay: pose the election. Options carry their arm as
			// the option kind so the answer records which branch paid.
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
				Prompt: "Reveal a card from hand or choose a permanent you control", Source: pc.card}
			for _, id := range hand {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "revealorchoose", Obj: id, Label: e.targetName(id)})
			}
			for _, id := range battlefield {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "choosecost", Obj: id, Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		case handViable:
			if len(hand) == int(part.N) {
				pc.reveals = append(pc.reveals, hand...)
				for range hand {
					pc.revealHandArm = append(pc.revealHandArm, true)
				}
				pc.revealOrChoosePart++
				continue
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
				Prompt: "Choose cards to reveal", Source: pc.card}
			for _, id := range hand {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "revealorchoose", Obj: id, Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		default: // battlefield only
			if len(battlefield) == int(part.N) {
				pc.reveals = append(pc.reveals, battlefield...)
				for range battlefield {
					pc.revealHandArm = append(pc.revealHandArm, false)
				}
				pc.revealOrChoosePart++
				continue
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
				Prompt: "Choose a permanent you control", Source: pc.card}
			for _, id := range battlefield {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "choosecost", Obj: id, Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		}
	}
	return false
}

func (e *Engine) beholdCostAsk() bool {
	pc := e.cast
	for pc.beholdPart < len(pc.cost.Behold) {
		part := pc.cost.Behold[pc.beholdPart]
		candidates := append(e.costCandidates(pc.player, pc.card, state.ZBattlefield, part.Spec, false, false),
			e.costCandidates(pc.player, pc.card, state.ZHand, part.Spec, true, false)...)
		if len(candidates) < int(part.N) {
			e.abortCast(pc, "behold cost no longer payable; cast aborted", true)
			return true
		}
		if len(candidates) == int(part.N) {
			pc.beholds = append(pc.beholds, candidates...)
			pc.beholdPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose permanents or cards to behold", Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "beholdcost", Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

func (e *Engine) tapPermanentCostAsk() bool {
	pc := e.cast
	for pc.tapPart < len(pc.cost.TapPermanent) {
		part := pc.cost.TapPermanent[pc.tapPart]
		candidates := e.costCandidates(pc.player, pc.card, state.ZBattlefield, part.Spec, false, true)
		// One permanent can never pay two parts of the same cost, so the
		// candidate filter claims everything an earlier stage already recorded:
		// an earlier tap part's choice (taps settle together at payCast, so the
		// state does not yet show it), a Convoke/Harmonize creature announced
		// for a spell whose dyn tap part deferred to xAsk (the settle runs
		// after the announcement), and -- for every part, literal and dynamic
		// alike -- the source itself when a {T} in the same cost will tap it at
		// payCast (Forge CostTap): the {T} and the tapXType can never spend one
		// permanent twice.
		if len(pc.taps) > 0 || len(pc.convoke) > 0 || pc.cost.Tap {
			taken := make(map[state.ObjID]bool, len(pc.taps)+len(pc.convoke)+1)
			for _, id := range pc.taps {
				taken[id] = true
			}
			for _, pay := range pc.convoke {
				taken[pay.id] = true
			}
			if pc.cost.Tap {
				taken[pc.card] = true
			}
			kept := make([]state.ObjID, 0, len(candidates))
			for _, cid := range candidates {
				if !taken[cid] {
					kept = append(kept, cid)
				}
			}
			candidates = kept
		}
		if isNamedCountPart(part) {
			// A named-announcement count (Explosive Singularity's Announce$
			// Tapped): namedAnnounceAsk already fixed how many; tap exactly
			// that many. A shrunken board is the unpayable abort.
			n := int(pc.namedN)
			if n > len(candidates) {
				e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
				return true
			}
			if n == 0 {
				pc.tapPart++
				continue
			}
			if n == len(candidates) {
				pc.taps = append(pc.taps, candidates...)
				pc.tapPart++
				continue
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
				Prompt: "Choose permanents to tap", Source: pc.card}
			for _, id := range candidates {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)})
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		}
		if part.Dyn != "" {
			// The dynamic tapXType heads (dynTapCost's doc). An X-form part whose
			// cost carries another announce-bearing part defers: xAsk (later in
			// continueCast's stage order) announces the X the part settles
			// exactly, so the ask must not run before the announcement exists
			// (Necron Overlord's "{X}, tap X untapped artifacts"). xAsk's own
			// bound caps the announced value by these candidates.
			if part.Dyn == "X" && !pc.xDone && (costAnnouncesCastX(pc.cost) || pc.announceX != "") {
				return false
			}
			// An Any-form part pays only by tapping at least one: no eligible
			// permanent (the affordability gate agreed, so this is a board that
			// changed under the offer) aborts the whole cast/activation. The
			// same holds when the survivors can no longer reach a
			// withTotalPowerGE<N> group predicate's floor: posing an election
			// whose every answer Decision.Validate rejects would wedge the
			// game, so CR 733.1's clean reversal runs instead.
			if part.Dyn == "Any" && len(candidates) == 0 {
				e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
				return true
			}
			if part.Dyn == "Any" && part.MinPower > 0 && e.tapPowerSum(candidates) < part.MinPower {
				e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
				return true
			}
			// An X-form election with no eligible permanent can only announce
			// X = 0 (CR 601.2b; the affordability gate agrees, so this is a
			// board that changed under the offer). A decision nobody could
			// answer differently is never emitted -- posting the Min 0/Max 0
			// empty ask panics rules/engine.go's ask -- so resolve it silently
			// with X = 0 and no taps, mirroring triggeredTapAsk's decline.
			if part.Dyn == "X" && !pc.xDone && len(candidates) == 0 {
				pc.x = 0
				pc.tapPart++
				continue
			}
			if part.Dyn == "X" && pc.xDone {
				// The announced X settles exactly: no choice beyond which
				// permanents, so a shortfall is the same unpayable abort.
				n := int(pc.x)
				if n > len(candidates) {
					e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
					return true
				}
				if n > 0 {
					d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
						Prompt: "Choose permanents to tap", Source: pc.card}
					for _, id := range candidates {
						d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)})
					}
					e.choosing = chooseCast
					e.ask(d)
					return true
				}
				pc.tapPart++
				continue
			}
			// The election announces the count: Min 0 for the X form (X = 0 is
			// a legal announcement) and Min 1 for Any (a cost is not paid by
			// tapping nothing). The answer records the taps and, for the X form,
			// binds the cast's X to the chosen count (the "tapcost" answer arm).
			// A part carrying a withTotalPowerGE<N> group predicate publishes
			// the floor as Decision.MinSum and each candidate's power as its
			// Option.Value, so Validate -- the one legal-answer home a client
			// and the bot repair both read -- enforces the total-power clause
			// without learning what power is.
			min := int32(1)
			if part.Dyn == "X" {
				min = 0
			}
			d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(min), Max: len(candidates),
				Prompt: "Choose permanents to tap", Source: pc.card, MinSum: int(part.MinPower)}
			for _, id := range candidates {
				opt := decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)}
				if part.MinPower > 0 {
					opt.Value = int(e.Power(id))
				}
				d.Options = append(d.Options, opt)
			}
			e.choosing = chooseCast
			e.ask(d)
			return true
		}
		if len(candidates) < int(part.N) {
			e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
			return true
		}
		// A literal part with a group predicate that the shrinking board can
		// no longer satisfy aborts like the Any form above -- an auto-tap of
		// the only N candidates (next branch) or an election with no legal
		// answer would pay a floor the state no longer reaches.
		if part.MinPower > 0 && e.tapTopPowerSum(candidates, int(part.N)) < part.MinPower {
			e.abortCast(pc, "tap cost no longer payable; cast aborted", true)
			return true
		}
		if len(candidates) == int(part.N) {
			pc.taps = append(pc.taps, candidates...)
			pc.tapPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: int(part.N), Max: int(part.N),
			Prompt: "Choose permanents to tap", Source: pc.card, MinSum: int(part.MinPower)}
		for _, id := range candidates {
			opt := decision.Option{Index: len(d.Options), Kind: "tapcost", Obj: id, Label: e.targetName(id)}
			if part.MinPower > 0 {
				opt.Value = int(e.Power(id))
			}
			d.Options = append(d.Options, opt)
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// tapPowerSum sums the current power of a tap-cost candidate list -- the
// set-level read a withTotalPowerGE<N> group predicate (Crew, Mossbridge
// Troll) constrains. "Any number" may tap the whole list, so the list's
// total is the most power a payment can tap.
func (e *Engine) tapPowerSum(ids []state.ObjID) int32 {
	sum := int32(0)
	for _, id := range ids {
		sum += e.Power(id)
	}
	return sum
}

// tapTopPowerSum is tapPowerSum for a literal tapXType<N/...> part with a
// group predicate: exactly N are tapped, so the most a payment can tap is
// the power of the N largest candidates. A slice sort is fine here -- the
// result is a sum, so the order equal powers sort in cannot reach an event.
func (e *Engine) tapTopPowerSum(ids []state.ObjID, n int) int32 {
	if n <= 0 {
		return 0
	}
	if n >= len(ids) {
		return e.tapPowerSum(ids)
	}
	pw := make([]int32, 0, len(ids))
	for _, id := range ids {
		pw = append(pw, e.Power(id))
	}
	sort.Slice(pw, func(a, b int) bool { return pw[a] > pw[b] })
	sum := int32(0)
	for _, v := range pw[:n] {
		sum += v
	}
	return sum
}

func (e *Engine) blightCostAsk() bool {
	pc := e.cast
	for pc.blightPart < len(pc.cost.Blight) {
		// An announced Blight<X> part's count is the announced X, which does
		// not exist until xAsk has run; skip it here (returning false so the
		// later stages run) and let the post-xAsk call in continueCast settle
		// it. Fixed Blight<N> parts are unaffected.
		if pc.cost.Blight[pc.blightPart].Announced && !pc.xDone {
			return false
		}
		candidates := e.costCandidates(pc.player, pc.card, state.ZBattlefield, "Creature.YouCtrl", false, false)
		if len(candidates) == 0 {
			e.abortCast(pc, "blight cost no longer payable; cast aborted", true)
			return true
		}
		if len(candidates) == 1 {
			pc.blights = append(pc.blights, candidates[0])
			pc.blightPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a creature to blight", Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "blightcost", Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// exAsk offers the next unsettled Exile cost part (ExileFromHand /
// ExileFromGrave), walking pc.cost.Exile in order (pc.exilePart) the way
// sacAsk walks pc.cost.Sac. Candidates come from the part's zone (the payer's
// hand, or their graveyard for the encore self-exile shape) and are filtered
// through MatchesSpecFrom so CARDNAME self-references resolve to the source
// object exactly as they do for sacrifice costs. A part with too few
// candidates aborts the whole cast (nothing has moved yet); a part whose sole
// candidate IS the source records it without a decision, mirroring
// sacAsk's CARDNAME singleton rule.
func (e *Engine) exAsk() bool {
	pc := e.cast
	// ExileFromTop is settled after the mana window, immediately before payment:
	// a mana ability may draw or reorder the library, so locking IDs here would
	// pay a card that is no longer on top. There is no chooser for this cost.
	for pc.exilePart < len(pc.cost.Exile) {
		part := pc.cost.Exile[pc.exilePart]
		zone := part.Zone
		if zone == 0 {
			zone = state.ZHand
		}
		// The announce-bound filter (the Shoal cycle's cmcEQX): the announced
		// X binds the spec's non-literal RHS through SpecContext.Resolve — the
		// same closure mechanism a Chosen* predicate resolves through — so
		// the part's candidates are exactly the cards at the announced mana
		// value. pc.x is already settled (xAsk's announce arm ran first in
		// continueCast).
		var sc *effects.SpecContext
		if pc.announceX != "" {
			name := pc.announceX
			bound := e.withNames(effects.SpecContext{You: pc.player, Source: pc.card, Resolve: func(n string) (int32, bool) {
				if n == name {
					return pc.x, true
				}
				return 0, false
			}})
			sc = &bound
		}
		var candidates []state.ObjID
		for _, oid := range e.G.Zone(zone, pc.player) {
			// A CAST (pc.ability < 0) can never exile the card it is casting:
			// the card sits in this zone until pushCast runs (CR 601.2a pushes
			// AFTER the cost asks), so without this skip a `Card` spec would
			// offer the cast card as its own ExileFromGrave fodder -- the
			// payment side of the same self-exclusion nonManaCastable applies
			// to the affordability walk. An ability activation (pc.ability >=
			// 0) is untouched: encore's Cost$ ExileFromGrave<1/CARDNAME>
			// really does exile its own source.
			if !pc.isAbility() && oid == pc.card {
				continue
			}
			// A battlefield Exile cost candidate is withheld by a CantExile
			// static whose ForCost$ True restricts cost payments -- the
			// exAsk half of the same guard nonManaCastable's offer walk
			// applies, keeping the ask from offering an unpayable permanent
			// (which would abort the cast). ForCost$ False (The Master)
			// leaves the candidate offered.
			if zone == state.ZBattlefield && e.exileBlockedForCost(oid, costCauseForAbility(pc.isAbility())) {
				continue
			}
			wholeZone := isWholeZoneExileSpec(part.Spec)
			match := wholeZone || e.matchesSpecFrom(part.Spec, oid, pc.player, pc.card)
			if sc != nil && !wholeZone {
				match = e.matchesSpec(part.Spec, oid, *sc)
			}
			if match {
				already := false
				for _, s := range pc.exiles {
					if s == oid {
						already = true
						break
					}
				}
				if !already {
					candidates = append(candidates, oid)
				}
			}
		}
		n := int(part.N)
		if part.Announced {
			n = int(pc.x)
		}
		named := isNamedCountPart(part)
		if named {
			n = int(pc.namedN)
		}
		if n < 0 || n > len(candidates) || (!part.Announced && !named && n == 0) {
			e.abortCast(pc, "exile cost no longer payable; cast/activation aborted", true)
			return true
		}
		if n == 0 {
			pc.exilePart++
			continue
		}
		if isWholeZoneExileSpec(part.Spec) {
			// ExileFromHand<1/All> pays the WHOLE zone and never asks: every
			// candidate is the payment (the isWholeZoneExileSpec reading the
			// triggered window's arm takes). The count check above still
			// demands part.N cards. No cast/activation corpus carrier exists
			// today; the wiring keeps the paths from diverging.
			pc.exiles = append(pc.exiles, candidates...)
			pc.exilePart++
			continue
		}
		// A singleton self-reference (encore's ExileFromGrave<1/CARDNAME>, the
		// sole candidate being the resolving card itself) has no player choice.
		if !part.Announced && part.N == 1 && len(candidates) == 1 && candidates[0] == pc.card &&
			strings.EqualFold(part.Spec, "CARDNAME") {
			pc.exiles = append(pc.exiles, pc.card)
			pc.exilePart++
			continue
		}
		zoneName := "hand"
		switch zone {
		case state.ZGraveyard:
			zoneName = "graveyard"
		case state.ZBattlefield:
			zoneName = "battlefield"
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Exile " + strconv.Itoa(n) + " card(s) from your " + zoneName +
				" to cast " + e.targetName(pc.card), Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "exilecost",
				Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// returnAsk offers the next unsettled Return cost part (Return<N/Spec>:
// a permanent matching Spec returned to its OWNER's hand -- Forge
// CostReturn.doPayment's moveToHand), walking pc.cost.Return in order
// (pc.returnPart) the way exAsk walks pc.cost.Exile. A Spec of CARDNAME is
// Forge's payCostFromSource: the resolving permanent itself is the sole
// candidate (Chthonian Nightmare's "Return Chthonian Nightmare to its
// owner's hand"), so no decision is posed. Any other Spec walks the payer's
// battlefield. The settle is payCast's: the chosen objects move to their
// owner's hand beside the other cost payments, so an abort cannot leave a
// partially paid return on the board.
func (e *Engine) returnAsk() bool {
	pc := e.cast
	for pc.returnPart < len(pc.cost.Return) {
		part := pc.cost.Return[pc.returnPart]
		spec := sacrificeMatchSpec(part.Spec)
		var candidates []state.ObjID
		if strings.EqualFold(spec, "CARDNAME") {
			if o := e.G.Obj(pc.card); o != nil && o.Zone == state.ZBattlefield {
				candidates = append(candidates, pc.card)
			}
		} else {
			candidates = e.costCandidates(pc.player, pc.card, state.ZBattlefield, spec, false, false)
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.abortCast(pc, "return cost no longer payable; cast/activation aborted", true)
			return true
		}
		// A singleton self-reference has no player choice, mirroring
		// sacAsk/exAsk's CARDNAME singleton rule.
		if part.N == 1 && len(candidates) == 1 && candidates[0] == pc.card &&
			strings.EqualFold(spec, "CARDNAME") {
			pc.returns = append(pc.returns, pc.card)
			pc.returnPart++
			continue
		}
		verb := "cast " + e.targetName(pc.card)
		if pc.isAbility() {
			verb = "activate"
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Return " + strconv.Itoa(n) + " permanent(s) to their owner's hand to " + verb,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "returncost",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// putToLibAsk offers the next unsettled PutToLib cost part
// (PutCardToLibFrom<Zone><N/Pos/Spec>: cards matching Spec moved from the
// payer's Hand, Graveyard or Battlefield to the top or bottom of their OWNER's
// library -- Forge CostPutCardToLib.doPayment), walking pc.cost.PutToLib in
// order (pc.putToLibPart) the way returnAsk walks pc.cost.Return. A Spec of
// CARDNAME from the BATTLEFIELD is Forge's payCostFromSource: the resolving
// permanent itself is the sole candidate (Timestream Navigator's "{2}{U}{U},
// {T}, Put Timestream Navigator on the bottom of its owner's library"), so no
// decision is posed. Any other zone/spec walks the payer's own zone (a hand
// or graveyard cost may still name the source itself when the ability is
// activated from there -- no corpus carrier does, but costCandidates resolves
// it). The settle is payCast's: the chosen objects move to their owner's
// library beside the other cost payments, so an abort cannot leave a
// partially paid placement on the board.
func (e *Engine) putToLibAsk() bool {
	pc := e.cast
	for pc.putToLibPart < len(pc.cost.PutToLib) {
		part := pc.cost.PutToLib[pc.putToLibPart]
		spec := sacrificeMatchSpec(part.Spec)
		var candidates []state.ObjID
		if part.Zone == state.ZBattlefield && strings.EqualFold(spec, "CARDNAME") {
			// The source itself is the sole candidate (Forge's
			// payCostFromSource) -- but only while the payer still controls it:
			// a control-changed source is not a cost the payer can pay, so
			// leaving it out sends the ability to the no-candidates abort
			// below instead of paying with a permanent the payer does not own
			// the choice over (0 corpus carriers; fail-closed).
			if o := e.G.Obj(pc.card); o != nil && o.Zone == state.ZBattlefield && o.Controller == pc.player {
				candidates = append(candidates, pc.card)
			}
		} else {
			candidates = e.costCandidates(pc.player, pc.card, part.Zone, spec, false, false)
		}
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.abortCast(pc, "put-to-library cost no longer payable; cast/activation aborted", true)
			return true
		}
		// A singleton self-reference (CARDNAME from the battlefield) has no
		// player choice, mirroring sacAsk/exAsk/returnAsk's CARDNAME rule.
		if part.N == 1 && len(candidates) == 1 && candidates[0] == pc.card &&
			part.Zone == state.ZBattlefield && strings.EqualFold(spec, "CARDNAME") {
			pc.putToLibs = append(pc.putToLibs, pc.card)
			pc.putToLibPart++
			continue
		}
		verb := "cast " + e.targetName(pc.card)
		if pc.isAbility() {
			verb = "activate"
		}
		dest := "the top of their owner's library"
		if part.LibraryPos == -1 {
			dest = "the bottom of their owner's library"
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Put " + strconv.Itoa(n) + " card(s) from your " + putToLibZoneName(part.Zone) +
				" on " + dest + " to " + verb,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "puttolibcost",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// putToLibZoneName names a PutToLib part's origin zone in a human-readable
// prompt. It is the cost-flow vocabulary, deliberately separate from
// handDestPhrase/destinationPhrase in effects.
func putToLibZoneName(z state.Zone) string {
	switch z {
	case state.ZHand:
		return "hand"
	case state.ZGraveyard:
		return "graveyard"
	default:
		return "battlefield"
	}
}

// moveToGraveCandidates returns the cards matching spec (you-relative to p,
// source-relative to source) still available in ANY alive player's exile
// zone, minus everything in reserved. Exiled cards live in their OWNER's
// exile zone (events/apply.go's zoneOwner), so the scan iterates the
// players rather than p's own zone: Shelob, Dread Weaver's exiles land in
// the OPPONENT's exile zone, and a controller-only scan would find nothing.
// Zone order is deterministic (players in seat order, each zone in list
// order), so the candidate order -- and therefore every pick built from it
// -- replays.
func (e *Engine) moveToGraveCandidates(p state.PlayerID, source state.ObjID, spec string, reserved map[state.ObjID]bool) []state.ObjID {
	var out []state.ObjID
	for i := range e.G.Players {
		if e.G.Players[i].Lost {
			continue
		}
		for _, id := range e.G.Zone(state.ZExile, state.PlayerID(i)) {
			if reserved[id] {
				continue
			}
			if e.matchesSpecFrom(spec, id, p, source) {
				out = append(out, id)
			}
		}
	}
	return out
}

// moveGraveAsk offers the next unsettled ExiledMoveToGrave cost part, walking
// pc.cost.MoveToGrave in order (pc.moveGravePart) the way exAsk walks
// pc.cost.Exile. Candidates come from EVERY alive player's exile zone
// (moveToGraveCandidates above -- the origin is always exile, the owner's
// zone is where the card sits), filtered through MatchesSpecFrom so
// Card.ExiledWithSource resolves against the ability's source. A part with
// too few candidates aborts the whole cast (nothing has moved yet).
func (e *Engine) moveGraveAsk() bool {
	pc := e.cast
	for pc.moveGravePart < len(pc.cost.MoveToGrave) {
		part := pc.cost.MoveToGrave[pc.moveGravePart]
		seen := make(map[state.ObjID]bool, len(pc.moveGraves))
		for _, s := range pc.moveGraves {
			seen[s] = true
		}
		candidates := e.moveToGraveCandidates(pc.player, pc.card, part.Spec, seen)
		n := int(part.N)
		if n <= 0 || n > len(candidates) {
			e.abortCast(pc, "move-to-graveyard cost no longer payable; cast/activation aborted", true)
			return true
		}
		verb := "cast " + e.targetName(pc.card)
		if pc.isAbility() {
			verb = "activate"
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Move " + strconv.Itoa(n) + " card(s) from exile to their owner's graveyard to " + verb,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "movetogravecost",
				Obj: id, Label: e.targetName(id)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// castModeAsk poses CR 601.2b's mode announcement for a modal spell. It uses
// KModes like the placement and resolution paths, but ResumeKind distinguishes
// this cast-transaction continuation from both: handleModes records the answer
// on the proposed spell and re-enters continueCast rather than resuming an
// effect or the trigger drain. Activated abilities retain their existing
// resolution-time behaviour; CR 603.3c triggered abilities remain owned by
// askTriggerModes.
func (e *Engine) castModeAsk() bool {
	pc := e.cast
	if pc == nil || pc.isAbility() || pc.modesDone {
		return false
	}
	pc.modesDone = true
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return false
	}
	f := o.Face()
	sa := f.SpellAbility()
	if sa == nil || sa.API != "Charm" || strings.TrimSpace(sa.Params["Choices"]) == "" {
		return false
	}
	ctx := &effects.Ctx{Source: pc.card, Controller: pc.player, PendingKicked: modeIsKicked(pc.mode)}
	effects.SetSVars(ctx, f.SVars)
	if effects.CharmRandomChosen(e, ctx, sa) {
		// param:api:Charm.Random: a random Charm's mode announcement is not
		// asked (measured corpus-unreachable -- every Random$ Charm carrier,
		// 5 files, is a trigger body -- so this site is latent). Resolution's
		// effCharm picks the mode with the engine's rng (Random$ True, or
		// Random$ Compare while the comparison holds) or poses the ordinary
		// KModes ask there; modesDone is already set, so the announcement is
		// simply skipped and the per-mode legality filter above never
		// narrows the pool the rng would pick from.
		return false
	}
	choices := strings.Split(sa.Params["Choices"], ",")
	// The potential pool (a pure read) is the colour-aware upper bound the
	// per-mode cost filter below prices against: at this point in the cast no
	// mana has been floated yet (the CR 601.2g window is in payCast), so the
	// floating pool alone would wrongly withhold every payable mode.
	pot := e.PotentialMana(pc.player)
	legal := make([]string, 0, len(choices))
	for _, name := range choices {
		name = strings.TrimSpace(name)
		sub := cards.ResolveSVar(f.SVars, name)
		if sub != nil && sub.Params["ValidTgts"] != "" &&
			!e.targetSAAvailable(pc.player, pc.card, pc.card, sub, pc.x, false) {
			continue
		}
		// CR 601.2b/702.171b: a Spree/Tiered mode's own ModeCost$ is an
		// additional cost charged per chosen mode. A mode whose cost cannot be
		// paid even after floating every untapped source is not a legal
		// announcement -- the same no-progress suppression the target-legality
		// filter above applies, and what makes a mode declined for cost ABSENT
		// rather than free. It is only a per-mode necessary condition: an
		// unaffordable COMBINATION of individually affordable modes still
		// aborts at payment (CR 733.1, the ordinary reversal), so no legal cast
		// is lost here and no unpayable cast is silently allowed.
		// modeCostFeasible prices the mode through the same modifier snapshot
		// the charge applies (pc.mods, with the potential-target retry), so a
		// ReduceCost/SetCost static that makes a mode payable is seen; the
		// price is against the potential pool (no mana floated yet at 601.2b).
		//
		// A ModeCost$ this build cannot price (ParseCost leaves an unknown
		// token) is withheld outright: an unparseable mandatory cost must never
		// degrade to a free mode.
		if modeCostUnparseable(f, name) {
			continue
		}
		if mc, present, ok := modeCost(f, name); present && ok {
			if !e.modeCostFeasible(pc, mc, pot) {
				continue
			}
		}
		legal = append(legal, name)
	}
	// ChoiceRestriction$: a Charm cast (no corpus carrier today, but the
	// class) still cannot announce a mode it already chose on the same source
	// under the scope. Filtered before the bounds clamp, exactly as the
	// triggered and mid-resolution asks do.
	legal = effects.CharmEligibleModes(e, pc.card, sa, legal)
	min, max, repeat := effects.CharmModeBounds(e, ctx, sa, len(legal))
	// Entwine (CR 702.42b): "If the entwine cost was paid, follow the
	// instructions of all its modes." So an entwined cast announces EVERY
	// eligible mode -- Min and Max both move to the filtered legal count, and
	// the answer handler, Decision.Validate and the bot arm all read those
	// bounds (the one-home rule), so no other site needs an entwine branch.
	// The cost was already folded into pc.cost by beginCast, so no
	// affordability clamp applies here: a cost that could not be paid never
	// offered, and an entwined cast pays it whether one or all modes are
	// legal. A repeatable Charm forces each distinct mode exactly once --
	// "follow the instructions of all its modes" is a distinct-mode pick, not
	// a licence to repeat one -- so the same distinct count is forced there
	// too (no corpus carrier is both repeatable and Entwine, but the
	// semantics are the same either way). When NO mode is legal the ordinary
	// min>len(legal) abort below still fires and the charge reverses through
	// the CR 733.1 path -- the card plus cost never strands.
	if pc.mode == "entwined" {
		if len(legal) == 0 {
			// An entwined cast must follow ALL modes, so with no eligible
			// mode the additional cost buys nothing and the cast is not a
			// legal announcement. Abort it loudly (the CR 733.1 reversal
			// path restores the board) rather than posing an empty ask or
			// silently resolving zero modes.
			e.abortCast(pc, "cast aborted: no legal modal choice", true)
			return true
		}
		min = len(legal)
		max = len(legal)
	}
	// Escalate (the modal additional cost): a cast choosing N modes pays the
	// escalate cost N-1 times, so a mode count the board cannot pay for is
	// not a legal announcement -- clamp Max to 1 + the largest number of
	// escalate payments the SAME affordability checker the payment window's
	// composed total faces (castable) still admits, the replicateAsk shape,
	// pool-only at ask time (the CR 601.2g window afterwards may still
	// produce mana). The loop is bounded by the bounds Max itself, so it
	// terminates. An unpriceable parameter cannot clamp (it also cannot
	// charge -- the answer handler emits a loud Note instead), so the
	// CharmNum$ bounds stay honest on the wire.
	if pc.escalateSet {
		if esc := ParseCost(pc.escalateParam); len(esc.Unknown) == 0 {
			maxEscalations := 0
			cand := pc.cost
			for 1+maxEscalations < max {
				if !e.castable(pc.player, pc.card, cand.Plus(esc), false) {
					break
				}
				cand = cand.Plus(esc)
				maxEscalations++
			}
			if clamped := 1 + maxEscalations; clamped < max {
				max = clamped
			}
		}
	}
	if min > len(legal) && !repeat {
		// No legal set of modes can complete its required target choices or
		// pay its per-mode costs. This is the modal counterpart of targetAsk's
		// no-legal-target reversal; use the no-progress suppression so an
		// automated seat cannot propose the same impossible cast forever. A
		// repeatable Charm can fill its slots by repeating an eligible mode, so
		// it never aborts here.
		e.abortCast(pc, "cast aborted: no legal modal choice", true)
		return true
	}
	// The defensive floor: a clamp below MinCharmNum$ cannot occur for the
	// corpus (every Escalate carrier's MinCharmNum$ is 1 and the clamp's
	// floor is 1 + 0 = 1), but a Min-2 future carrier on an unaffordable
	// board must abort loudly rather than pose an ask no legal answer
	// satisfies -- the same no-progress suppression as the no-legal-mode
	// abort above.
	if max < min {
		e.abortCast(pc, "cast aborted: no affordable modal choice", true)
		return true
	}
	d := modeDecisionForChoices(pc.player, pc.card, sa, f.SVars, legal, min, max, repeat)
	d.ResumeKind = "cast_modes"
	if effects.OnlyEmptyAnswer(d) {
		// "Choose up to N" (MinCharmNum$ 0) with no mode that has a legal
		// target or an affordable cost: the only legal announcement is zero
		// modes (Call Damage Control with an empty graveyard). Nobody could
		// answer differently, so record it without posting the decision --
		// the same silent resolution effects.Ask gives this shape, and what
		// Engine.ask requires of every asking site.
		e.applyCastModes(d, pc.player, nil)
		return true
	}
	e.ask(d)
	return true
}

// modalTargetSA returns the first target declaration of a modal spell for
// legacy single-mode and unsupported multi-target paths. Supported distinct
// modes instead use the per-mode grouped ask in targetAsk.
func modalTargetSA(f *cards.Face, sa *cards.SA, modes []string) *cards.SA {
	if sa == nil || sa.Params["ValidTgts"] != "" || sa.API != "Charm" || f == nil {
		return sa
	}
	for _, name := range modes {
		if sub := cards.ResolveSVar(f.SVars, name); sub != nil && sub.Params["ValidTgts"] != "" {
			return sub
		}
	}
	return sa
}

// replicateAsk poses CR 702.55a's replicate count question -- "you may pay
// [the replicate cost] any number of times as you cast this spell" -- once,
// before the Convoke/Harmonize and X stages, whose asks must see and bound
// against the composed total. The max is the largest N the current board can
// still pay, checked with the SAME affordability checker the payment window's
// composed total faces (castable: the conversion-aware mana gate plus every
// non-mana part), pool-only at ask time -- the 601.2g window afterwards may
// still produce mana for the composed total, exactly like a kicked cast. The
// answered count folds that many payments into cost (castAnswer); 0 declines:
// no flag, an exactly plain cast.
func (e *Engine) replicateAsk() bool {
	pc := e.cast
	if pc.replicateDone || !pc.replicateSet {
		return false
	}
	pc.replicateDone = true
	rc := ParseCost(pc.replicateParam)
	max := int32(0)
	cand := pc.cost
	for i := int32(0); i < 64; i++ {
		// The hard cap only exists so a degenerate future cost whose every
		// part prices against a non-reserving candidate count cannot loop;
		// every real replicate resource (mana, energy, life, tap/sac
		// candidates) is finite and breaks the loop naturally.
		next := cand.Plus(rc)
		if !e.countCandPayable(pc, next) {
			break
		}
		cand = next
		max++
	}
	if max == 0 {
		// The offer gate proved one payment payable; a board that changed
		// under the proposal (or a cost modifier that priced the OFFER but
		// not this loop's bare, unmodified cost -- the bound here is
		// deliberately conservative, never over-offering) degrades the
		// explicitly chosen "(replicated)" mode to the count-0 plain cast
		// (the conservative CR 733 direction) rather than wedging or
		// aborting. The degrade is loud: a silent downgrade would leave the
		// player's choice unrecorded.
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "replicate no longer payable; casting without replicate"})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Pay the replicate cost how many times?", Source: pc.card}
	for n := int32(0); n <= max; n++ {
		label := "No replicate"
		if n == 1 {
			label = "Pay replicate once"
		} else if n > 1 {
			label = fmt.Sprintf("Pay replicate %d times", n)
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "replicate",
			Label: label, Amount: int(n)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// multikickAsk poses CR 702.43's multikicker count question -- "you may pay
// [the multikicker cost] any number of times as you cast this spell" -- once,
// the replicateAsk shape (the same cast announcement, CR 601.2b): the max is
// the largest N the current board can still pay, walked with the SAME
// affordability checker the payment window's composed total faces (castable),
// pool-only at ask time. The answered count folds that many payments into
// cost (castAnswer); 0 declines: no flag, an exactly plain cast.
func (e *Engine) multikickAsk() bool {
	pc := e.cast
	if pc.multikickDone || !pc.multikickSet {
		return false
	}
	pc.multikickDone = true
	mk := ParseCost(pc.multikickParam)
	max := int32(0)
	cand := pc.cost
	for i := int32(0); i < 64; i++ {
		// The hard cap only exists so a degenerate future cost whose every
		// part prices against a non-reserving candidate count cannot loop;
		// every real multikicker resource is finite and breaks the loop
		// naturally (the replicateAsk comment).
		next := cand.Plus(mk)
		if !e.countCandPayable(pc, next) {
			break
		}
		cand = next
		max++
	}
	if max == 0 {
		// The offer gate proved one payment payable; a board that changed
		// under the proposal degrades the explicitly chosen
		// "(multikicked)" mode to the count-0 plain cast (the replicateAsk
		// max==0 arm's conservative direction), never a wedge or abort.
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "multikicker no longer payable; casting without multikick"})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Pay the multikicker cost how many times?", Source: pc.card}
	for n := int32(0); n <= max; n++ {
		label := "No multikick"
		if n == 1 {
			label = "Pay multikicker once"
		} else if n > 1 {
			label = fmt.Sprintf("Pay multikicker %d times", n)
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "multikick",
			Label: label, Amount: int(n)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// squadAsk poses CR 702.66's squad count question -- "you may pay [the squad
// cost] any number of times as you cast this spell" -- once, the
// replicateAsk/multikickAsk shape (the same cast announcement, CR 601.2b): the
// max is the largest N the current board can still pay, walked with the SAME
// affordability checker the payment window's composed total faces (castable),
// pool-only at ask time. The answered count folds that many payments into
// cost (castAnswer); 0 declines: no flag, an exactly plain cast.
func (e *Engine) squadAsk() bool {
	pc := e.cast
	if pc.squadDone || !pc.squadSet {
		return false
	}
	pc.squadDone = true
	sc := ParseCost(pc.squadParam)
	max := int32(0)
	cand := pc.cost
	for i := int32(0); i < 64; i++ {
		// The hard cap only exists so a degenerate future cost whose every
		// part prices against a non-reserving candidate count cannot loop;
		// every real squad resource (mana, energy, life, tap/sac candidates)
		// is finite and breaks the loop naturally (the replicateAsk
		// comment).
		next := cand.Plus(sc)
		if !e.countCandPayable(pc, next) {
			break
		}
		cand = next
		max++
	}
	if max == 0 {
		// The offer gate proved one payment payable; a board that changed
		// under the proposal degrades the explicitly chosen "(squadded)"
		// mode to the count-0 plain cast (the replicateAsk max==0 arm's
		// conservative direction), never a wedge or abort. The degrade is
		// loud: a silent downgrade would leave the player's choice
		// unrecorded.
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "squad no longer payable; casting without squad"})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Pay the squad cost how many times?", Source: pc.card}
	for n := int32(0); n <= max; n++ {
		label := "No squad"
		if n == 1 {
			label = "Pay squad once"
		} else if n > 1 {
			label = fmt.Sprintf("Pay squad %d times", n)
		}
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "squad",
			Label: label, Amount: int(n)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// conspireAsk poses CR 702.78a's tap election for a "conspired" cast: tap two
// untapped creatures you control that share a colour with the spell. A
// dedicated ask (not a costCandidates-driven TapPermanent part) because the
// eligibility is a COLOUR INTERSECTION with the spell itself, which no cost
// spec in the filter vocabulary can express today (measured: no
// sharesColorWith predicate). With exactly two eligible creatures the tap is
// forced and no decision is posed (the strict-supersets convention); with
// more, a Min==Max==2 KChoose is posed over exactly the eligible set. A board
// that changed under the proposal (fewer than two eligible) degrades the
// explicitly chosen "(conspired)" mode to the plain cast with a loud Note,
// the replicateAsk/multikickAsk max==0 direction.
func (e *Engine) conspireAsk() bool {
	pc := e.cast
	if pc.conspireDone || !pc.conspireSet {
		return false
	}
	pc.conspireDone = true
	candidates := e.conspireCandidates(pc.player, pc.card)
	if len(candidates) < 2 {
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "conspire no longer payable; casting without conspire"})
		return false
	}
	if len(candidates) == 2 {
		pc.taps = append(pc.taps, candidates...)
		pc.conspirePaid = true
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 2, Max: 2,
		Prompt: "Choose two creatures to tap for conspire", Source: pc.card}
	for _, id := range candidates {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "conspire", Obj: id, Label: e.targetName(id)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// casualtyAsk announces the optional sacrifice before payment. The chosen
// creature remains on the battlefield until payCast, after target selection.
func (e *Engine) casualtyAsk() bool {
	pc := e.cast
	if pc.mode != "casualty" || pc.casualtyDone {
		return false
	}
	pc.casualtyDone = true
	candidates := e.casualtyCandidates(pc.player, pc.card, pc.casualtyN)
	if pc.casualtyN < 0 || len(candidates) == 0 {
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card, Text: "casualty no longer payable; casting without casualty"})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose a creature to sacrifice for casualty", Source: pc.card}
	for _, id := range candidates {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "casualty", Obj: id, Label: e.targetName(id)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// xMinAbilityParam is the ONE parse of an ability's XMin$ parameter (task
// cost:xmin-param): a positive integer is the SA's own announcement floor for
// the shared X; anything else (absent, non-numeric, zero, negative) binds
// nothing, exactly as the cost-embedded XMin<N> token fallback does not
// invent a floor. Both floor readers go through it -- xAsk for the option
// range of the ability actually being activated (pcAbility resolves the
// printed, granted or gained SA) and the offer gate for the cheapest legal
// announcement its feasibility composition prices (scope.ab, the same SA the
// offer walk scoped) -- so the offer and the ask can never disagree about
// what the parameter binds.
func xMinAbilityParam(ab *cards.SA) int32 {
	if ab == nil {
		return 0
	}
	n, err := strconv.ParseInt(ab.Params["XMin"], 10, 32)
	if err != nil || n <= 0 {
		return 0
	}
	return int32(n)
}

// xAsk asks a value for {X} if pc.cost carries one, offering 0..max where
// max is the largest value the mana pool (crediting the best possible
// Delve) can still pay. Runs at most once (xDone).
func (e *Engine) xAsk() bool {
	pc := e.cast
	if pc.xDone {
		return false
	}
	pc.xDone = true
	// The announce-bearing alternative cost (the Shoal cycle's Announce$ X):
	// the announced X is bound by the exile settlement, not by mana — each
	// candidate value is a distinct mana value some exilable card still
	// matches at (altCostXCandidates walks the payer's hand, binding X to
	// each card's own mana value and keeping the matches). No pool bound
	// applies (the alt cost pays no mana X), and the pool-based payable walk
	// below is meaningless for it, so the arm returns straight from the
	// candidate set. An empty set — the hand changed under the offer gate —
	// aborts the cast (CR 733.1, nothing has moved).
	if pc.announceX != "" && len(pc.cost.Exile) > 0 {
		vals := e.altCostXCandidates(pc.player, pc.card, altCostView{
			cost: pc.cost, announce: pc.announceX, src: pc.card})
		if len(vals) == 0 {
			e.abortCast(pc, "announce cost no longer payable; cast aborted", true)
			return true
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
			Prompt: "Choose a value for X", Source: pc.card}
		for _, x := range vals {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "x",
				Label: fmt.Sprintf("X = %d", x), Amount: int(x)})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	// A PayEnergy<X> part announces the same X the cast pays with (CR
	// 107.3i's ability X), so its presence triggers this ask exactly like a
	// printed {X} mana symbol does. A Sac<X/Spec> part announces the count of
	// permanents to sacrifice the same way; announced ExileFromGrave<X/Spec>,
	// PayLife<X> and SubCounter<X/Kind> parts also announce this shared X.
	energyX := false
	for _, part := range pc.cost.Energy {
		if part.Spec == "X" {
			energyX = true
		}
	}
	sacX := false
	for _, part := range pc.cost.Sac {
		if part.Announced {
			sacX = true
		}
	}
	exileX := false
	for _, part := range pc.cost.Exile {
		if part.Announced {
			exileX = true
		}
	}
	subCounterX := false
	for _, part := range pc.cost.SubCounter {
		if part.Announced {
			subCounterX = true
		}
	}
	// A Blight<X> part announces its count as the cast's X exactly the way a
	// Sac<X/Spec> part does (CR 701.60: "blight X" is X -1/-1 counters on a
	// chosen creature, and both corpus carriers spell the announcement
	// `Announce$ X | XMax$ GrTo`). It carries no {X} mana symbol.
	blightX := false
	for _, part := range pc.cost.Blight {
		if part.Announced {
			blightX = true
		}
	}
	discardX := false
	for _, part := range pc.cost.Discard {
		if part.Announced {
			discardX = true
		}
	}
	lifeXCount := len(pc.cost.LifeX)
	if pc.cost.X <= 0 && !energyX && !sacX && !exileX && !subCounterX && !blightX && !discardX && lifeXCount == 0 {
		return false
	}
	min := int32(0)
	// The cost's own announced-X lower bound (XMin<N>, "X can't be 0"):
	// Thieving Skydiver's kicked {X} must be at least 1. Suspend keeps its
	// separate time-X bound; a cost carrying both takes the higher floor.
	if pc.cost.XMin > min {
		min = pc.cost.XMin
	}
	if pc.suspendTimeX && pc.suspendMinX > min {
		min = pc.suspendMinX
	}
	// The active ability's own XMin$ parameter (task cost:xmin-param): an
	// activated ability whose parameters announce a floor for the shared X
	// (Jetfire, Rasputin, Radiant Lotus, Corpseweft all carry XMin$ 1 beside
	// an announced X cost part). It folds by MAXIMUM with the cost-embedded
	// XMin<N> token and the suspend bound above -- three independent carriers
	// of the same CR 601.2b announcement floor -- and resolves through
	// pcAbility, so a granted or has-all-abilities activation reads the
	// ability actually being activated, not a face-wide parameter. A spell
	// cast (pcAbility nil) reads nothing here; a malformed value binds
	// nothing, exactly as the token fallback does not invent a floor.
	if n := xMinAbilityParam(e.pcAbility(pc)); n > min {
		min = n
	}
	pool := e.G.Players[pc.player].Pool
	gy := int32(len(e.G.Zone(state.ZGraveyard, pc.player)))
	// Bound: past this many mana no further X is ever payable. In addition to
	// the pool and possible Delve, the already-announced Convoke/Harmonize
	// payments can cover X's generic requirement. Their exact application
	// below handles coloured costs before generic reductions; this bound need
	// only be a safe finite ceiling.
	credit := int32(0)
	for _, pay := range pc.convoke {
		if pay.power > 0 {
			credit += pay.power
		} else {
			credit++
		}
	}
	bound := pool.Total() + gy + credit + 1
	// A named-announcement discount (the March cycle's "{2} less for each
	// card exiled this way") is a generic reduction the X can spend: the
	// ceiling rises by the composed reduction at the announced count, or an
	// X affordable only through it would never be offered.
	if costHasNamedCount(pc.cost) {
		for _, red := range e.manaToPayXMods(pc, 0).reduces {
			bound = addClampedGeneric(bound, int64(red.generic))
		}
	}
	// A PayEnergy<X> cost part pays the SAME announced X in energy counters
	// (Forge CostPayEnergy.getMaxAmountX bounds a dynamic PayEnergy by the
	// payer's energy total). When the energy part is the ONLY X the cost
	// carries, the mana bound is irrelevant and the bound is exactly that
	// total; when a printed {X} also exists, the energy total still caps it
	// from above -- an X beyond it could be announced but never paid, and
	// CR 601.2b's announcement must be one the payment can settle.
	for _, part := range pc.cost.Energy {
		if part.Spec == "X" {
			energy := e.G.Players[pc.player].Counter("ENERGY")
			if pc.cost.X == 0 {
				bound = energy
			} else if energy < bound {
				bound = energy
			}
		}
	}
	// Without a printed mana X or energy X, the mana-pool ceiling is not
	// relevant. The first announced-count part supplies the ceiling; every
	// subsequent part (including tapXType) min-clamps that same X.
	announcedOnly := pc.cost.X <= 0 && !energyX
	boundSet := false
	applyCap := func(cap int32) {
		if announcedOnly && !boundSet {
			bound, boundSet = cap, true
		} else if cap < bound {
			bound = cap
		}
	}
	// A Sac<X/Spec> part's bound is the number of matching permanents the
	// payer could sacrifice -- announcing a count beyond it could never be
	// settled (CR 601.2b's announcement must be one the payment can settle).
	// When the sac count is the ONLY announced X it IS the bound; when a
	// mana/energy X also exists the candidate count caps it from above.
	for _, part := range pc.cost.Sac {
		if part.Announced {
			avail := int32(len(e.sacrificeCostCandidates(pc.player, pc.card, part, pc.isAbility())))
			applyCap(avail)
		}
	}
	// An X-form tapXType part settles exactly the announced X the same way a
	// Sac<X/Spec> part's count does, so the announcement is bounded by the
	// untapped permanents matching its spec (Necron Overlord's "{X}, tap X
	// untapped artifacts": X beyond the artifact count could be announced but
	// never settled). When the tap part is the ONLY announced X it IS the
	// bound -- but that shape never reaches xAsk at all (the tap election
	// announces it at the tap stage, before xAsk's guard sees no other reason
	// and returns); this cap governs the composed shapes.
	for _, part := range pc.cost.TapPermanent {
		if part.Dyn != "X" {
			continue
		}
		avail := int32(0)
		for _, oid := range e.costCandidates(pc.player, pc.card, state.ZBattlefield, part.Spec, false, true) {
			// The {T} in the same cost claims the source (see tapPermanentCostAsk).
			if pc.cost.Tap && oid == pc.card {
				continue
			}
			avail++
		}
		applyCap(avail)
	}
	// An announced SubCounter<X/Kind> part's bound is the number of counters
	// of that kind the SOURCE actually has (Chandra, Awakened Inferno's
	// SubCounter<X/LOYALTY>: the loyalty the walker has to remove). A filtered
	// fixed-kind part is likewise capped by its largest candidate, but a
	// filtered Any-kind part may remove individual units across candidates, so
	// its cap is their aggregate available counters (Moxite Refinery). An
	// announced PayLife<X> part's bound is the payer's life total divided
	// across the parts (the payer cannot pay more life than they have;
	// paying exactly all of it is legal -- the SBA owns the zero-life
	// consequence). When an announced part is the ONLY X the cost carries it
	// IS the bound -- the pool-based mana ceiling is meaningless without a
	// mana X -- and when another announced X also exists each cap min-clamps
	// the shared X (CR 601.2b's announcement must be one the payment can
	// settle).
	// An announced Discard<X/Spec> (Aether Tide) discards exactly X matching
	// cards, so X is capped by the matching cards in hand.
	for _, part := range pc.cost.Discard {
		if part.Announced {
			applyCap(int32(len(e.discardCandidates(pc.player, pc.card, part, !pc.isAbility(), nil))))
		}
	}
	for _, part := range pc.cost.Exile {
		if !part.Announced {
			continue
		}
		// Use the same source exclusion and zone-order filter as exAsk: a
		// spell cast from this graveyard cannot exile itself as its cost.
		candidates := e.costCandidates(pc.player, pc.card, state.ZGraveyard, part.Spec, !pc.isAbility(), false)
		applyCap(int32(len(candidates)))
	}
	for _, part := range pc.cost.SubCounter {
		if !part.Announced {
			continue
		}
		have := int32(0)
		if subCounterTargetsSource(part.Target) {
			if o := e.G.Obj(pc.card); o != nil {
				have = subCounterAvailable(o, part.Spec)
			}
		} else {
			for _, oid := range e.subCounterRemovalCandidates(pc.player, pc.card, part, 1, nil) {
				if o := e.G.Obj(oid); o != nil {
					n := subCounterAvailable(o, part.Spec)
					if strings.EqualFold(part.Spec, "Any") {
						have += n
					} else if n > have {
						have = n
					}
				}
			}
		}
		applyCap(have)
	}
	if lifeXCount > 0 {
		life := e.G.Players[pc.player].Life
		if life < 0 {
			life = 0
		}
		applyCap(life / int32(lifeXCount))
	}
	if blightX {
		// Blight<X> alone must use the toughness cap as its bound, not the
		// pool/graveyard mana ceiling: blighting does not spend mana.
		// Blight<X>'s own cap: both carriers spell it `XMax$ GrTo` -- "X
		// can't be greater than the greatest toughness among creatures you
		// control" -- so bound the announcement by the greatest toughness
		// among exactly the candidates the payment will choose from. With no
		// matching creature the cap is 0 (the offer gate withholds the cast
		// anyway), and the announcement can never exceed what the blight pick
		// can settle (CR 601.2b).
		cap := int32(0)
		for _, oid := range e.costCandidates(pc.player, pc.card, state.ZBattlefield, "Creature.YouCtrl", false, false) {
			if t := e.Toughness(oid); t > cap {
				cap = t
			}
		}
		applyCap(cap)
	}
	var legal []int32
	maxOld := int32(0)
	// A cost whose announced X feeds a ReduceCost static (Dargo's Sac<X>
	// reading Count$xPaid) does NOT price monotonically in x: the total
	// falls as the reduction grows, so the first unpayable X may be
	// followed by a payable one. The offer gate's affordability sweep
	// accepts exactly such an announcement, so xAsk must offer it too --
	// breaking at the first unpayable x would withhold the only legal
	// announcement and wedge the fetched cast. Every other announced-X
	// cost keeps the early break (generic only grows with x, so nothing
	// past the first unpayable x can be payable).
	nonMonotonic := costAnnouncesPaidX(pc.cost)
	// The target-dependent retry's statics are collected lazily, on the first
	// candidate X the ordinary nil-target price cannot settle, and reused for
	// the rest of the loop.
	var xStatics costStaticViews
	xStaticsLoaded := false
	for x := min; x <= bound; x++ {
		// The offer sweep and the announcement must agree on whether the
		// SAME X can settle every Sac part without reusing an object.
		if sacX && !e.sacrificeCostAssignable(pc.player, pc.card, pc.cost.Sac, pc.isAbility(), x) {
			continue
		}
		var potentialMods costMods
		usedPotential := false
		wx := e.paymentManaX(pc, x)
		wx.Generic -= e.delveCredit(pc.player, pc.card, wx.Generic)
		// The descriptor carries the announced-X marker: WithX folded this
		// payment's X into Generic, and a CostContainsX batch must still see
		// an X payment here or every X announcement would be unpayable.
		payable := e.costPayableClass(pc.player, paymentForCast(pc, wx),
			pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}, wx)
		if !payable {
			// A target-dependent reduction is absent from the nil-target
			// composition pc.mods carries, so an X affordable only under it
			// (Lullmage's Domination) would be withheld. Retry through the
			// same potential-target composition the offer gate uses; the
			// chosen target is repriced at CR 601.2c before payment, so this
			// only widens the menu to X some legal target can pay.
			if !xStaticsLoaded {
				xStatics = e.collectCostStatics()
				xStaticsLoaded = true
				// A target-dependent reduction does not price monotonically in
				// x: the candidate targets an X legalizes differ per X (a cmc-1
				// creature may qualify where a cmc-2 one does not, and vice
				// versa), so an unpayable X does not imply every larger X is
				// unpayable. Suppress the early break for the rest of the loop
				// exactly as costAnnouncesPaidX does for the Dargo shape.
				if xStatics.validTarget {
					nonMonotonic = true
				}
			}
			if m, ok := e.xTargetPotentialMods(pc, x, xStatics); ok {
				potentialMods = m
				wx = e.paymentManaXUsing(pc, x, m)
				wx.Generic -= e.delveCredit(pc.player, pc.card, wx.Generic)
				payable = true
				usedPotential = true
			}
		}
		if !payable {
			if !nonMonotonic {
				break
			}
			continue
		}
		maxOld = x
		// Every announced contribution must actually reduce this X's cost
		// (convokeAbsorbs against the PRE-contribution total manaToPayX --
		// paymentManaX has already applied them): an announcement
		// over-selected for the generic total cannot be made legal by
		// choosing a small X, so that X is not offered and the larger X
		// that absorbs every creature is. The payable check breaks at the
		// first unpayable X -- generic only grows with x, so everything
		// past it is unpayable too -- while a no-op X is skipped without
		// breaking: absorption improves monotonically with x. Without
		// announced contributions the absorb check is vacuously true, so
		// the offer is exactly the old payable range.
		convMana := e.manaToPayX(pc, x)
		if usedPotential {
			convMana = e.manaToPayXUsing(pc, x, potentialMods)
		}
		if !e.convokeAbsorbs(pc, convMana, pc.convoke, false) {
			continue
		}
		// Waterbend taps pay only the waterbend amount, which a Waterbend<X>
		// raise ties to this X.
		if waterbendTaps(pc.convoke) > pc.mods.waterbend+pc.mods.raiseX*x {
			continue
		}
		legal = append(legal, x)
	}
	vals := legal
	if len(vals) == 0 {
		// Either the whole range was unpayable (a proposal the offer gate
		// would have priced differently, or one made directly -- the CR 733
		// audit does exactly that), or every payable X left an announced
		// contribution a no-op. In both, the OLD offer stands and payCast's
		// own payable check aborts as it always did (CR 733.2), rather
		// than this ask wedging or moving the abort site.
		for x := min; x <= maxOld; x++ {
			vals = append(vals, x)
		}
	}
	if len(vals) == 0 {
		// A LOWER-BOUNDED announcement (Cost.XMin, from an XMin<N> leading
		// token or an X1+/... counter-removal argument) whose every cap sits
		// below the floor: the old offer is empty too (maxOld < min), and the
		// old min==0 paths always kept X=0, so only this new shape can arrive
		// here. No legal announcement exists (CR 601.2b's announcement must
		// be one the payment can settle), and posing a decision with no
		// options would wedge the seat -- so the cast aborts here, the CR
		// 733.2 fail-closed direction, the same site the other
		// no-longer-payable announcements use.
		e.abortCast(pc, "announced X has no payable value; cast aborted", true)
		return true
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose a value for X", Source: pc.card}
	for _, x := range vals {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "x",
			Label: fmt.Sprintf("X = %d", x), Amount: int(x)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// xTargetPotentialMods prices candidate X through the potential-target retry
// the offer gate uses: a ReduceCost static that reads the chosen targets
// (ValidTarget$, a target-conditional ValidSpell$, or a target-relative
// Count$Compare Amount$) is absent from pc.mods, which was priced with no
// targets. Lullmage's Domination is the corpus carrier: its {3} reduction
// hinges on the controller of the creature it targets, so with only {U}{U}{U}
// available X=1 is payable only after a qualifying target is chosen. xAsk runs
// before CR 601.2c, so without this the X menu offered only X=0 and the
// reduction was unusable for nonzero X. The candidate X is bound while the
// potential targets are enumerated so an X-dependent ValidTgts$ (cmcEQX)
// resolves. ok=false means no potential-target composition made X payable, in
// which case the caller keeps the ordinary fail-closed break. The final
// selection is still repriced before payment (repriceForTargets and
// affordableTargetCandidates), so this only widens the MENU to X values some
// legal target choice can pay.
func (e *Engine) xTargetPotentialMods(pc *pendingCast, x int32, statics costStaticViews) (costMods, bool) {
	if !statics.validTarget {
		return costMods{}, false
	}
	scope, ok := e.pendingCastScope(pc)
	if !ok {
		return costMods{}, false
	}
	// Bind the candidate X for the target census: targetSpecContext reads
	// e.cast.x, so an X-bound ValidTgts$ (Creature.cmcEQX) enumerates exactly
	// the targets X would legalize. Restored immediately; pc.x is only
	// assigned the announcement after the decision is answered.
	prevX := pc.x
	pc.x = x
	targets := e.costPotentialTargets(pc.player, pc.card, scope)
	pc.x = prevX
	if len(targets) == 0 {
		return costMods{}, false
	}
	return e.potentialCostModsUsing(statics, pc.player, pc.card, scope, targets, x, func(m costMods) bool {
		w := e.paymentManaXUsing(pc, x, m)
		w.Generic -= e.delveCredit(pc.player, pc.card, w.Generic)
		return e.costPayableClass(pc.player, paymentForCast(pc, w),
			pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}, w)
	})
}

// delveAsk offers exiling graveyard cards to pay for id's Delve, when id has
// Delve, the caster's graveyard is non-empty and the resolved cost still
// carries a generic requirement to reduce. Runs at most once (delveDone).
// Max is the SHORTFALL -- generic minus what the pool can already pay --
// not the whole generic requirement, so a caster with enough mana is not
// offered (and a bot does not take) exiles the cost does not actually need.
func (e *Engine) delveAsk() bool {
	pc := e.cast
	if pc.delveDone {
		return false
	}
	pc.delveDone = true
	if !e.hasKeywordH(pc.card, kwhDelve) {
		return false
	}
	gy := e.G.Zone(state.ZGraveyard, pc.player)
	cost := e.manaToPay(pc)
	generic := cost.Generic
	if len(gy) == 0 || generic <= 0 {
		return false
	}
	// Max is the SHORTFALL -- generic minus what the pool can already pay --
	// not the whole generic requirement, so a caster with enough mana is not
	// offered (and a bot does not take) exiles the cost does not actually
	// need. Delve only ever covers generic, so the colored requirement is
	// reserved out of the pool first; the rest of the pool can pay at most
	// its total as generic (Cost.Pay's WUBRG spending order never reduces
	// the total it can cover).
	rest := e.G.Players[pc.player].Pool
	for i, n := range cost.Colored {
		if rest[i] < n {
			// Colored unpayable: delve cannot help with it, so the whole
			// generic requirement is the shortfall (the commit stage's own
			// payMana will still fail honestly).
			rest = state.Mana{}
			break
		}
		rest[i] -= n
	}
	payable := generic
	if rest.Total() < payable {
		payable = rest.Total()
	}
	shortfall := generic - payable
	if shortfall <= 0 {
		return false
	}
	max := len(gy)
	if int32(max) > shortfall {
		max = int(shortfall)
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 0, Max: max,
		Prompt: "Delve: exile cards from your graveyard to help cast " + e.G.Obj(pc.card).Face().Name,
		Source: pc.card}
	for _, id := range gy {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "exile",
			Obj: id, Label: e.G.Obj(id).Face().Name})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// subCounterTargetsSource reports whether a SubCounter part's removal-target
// field names the paying source itself: the empty field (the original
// two-field SubCounter<N/Kind> token, which has always removed from the
// source) and Forge's payCostFromSource spellings CARDNAME/NICKNAME. Any
// other value is a filter matched against the payer's battlefield.
func subCounterTargetsSource(target string) bool {
	switch strings.ToUpper(strings.TrimSpace(target)) {
	case "", "CARDNAME", "NICKNAME":
		return true
	}
	return false
}

// subCounterAvailable reports how many counters of the part's kind the object
// could give up: the kind's own count, or the object's TOTAL counter count
// for the "Any" kind (Forge's Any removes that many counters regardless of
// kind). Used by the offer gate, the X bound and the candidate walk.
func subCounterAvailable(o *state.Object, kind string) int32 {
	if strings.EqualFold(kind, "Any") {
		total := int32(0)
		for _, c := range o.Counters {
			if c.N > 0 {
				total += c.N
			}
		}
		return total
	}
	return o.Counter(kind)
}

// subCounterRemovalCandidates lists the permanents the payer could remove
// part's counters from. A source-anchored part (subCounterTargetsSource)
// offers just the source when it still carries enough counters; a filtered
// part offers every battlefield object the PAYER controls that matches the
// target spec (MatchesSpecFrom, the sacAsk machinery's read) and still
// carries enough counters, excluding ids already reserved by an earlier part
// of the same cost (sacs and earlier counter removals).
func (e *Engine) subCounterRemovalCandidates(p state.PlayerID, source state.ObjID, part CostPart, amt int32, reserved map[state.ObjID]bool) []state.ObjID {
	if subCounterTargetsSource(part.Target) {
		if o := e.G.Obj(source); o != nil && o.Zone == state.ZBattlefield &&
			subCounterAvailable(o, part.Spec) >= amt && (reserved == nil || !reserved[source]) {
			return []state.ObjID{source}
		}
		return nil
	}
	var out []state.ObjID
	for _, oid := range e.G.Zone(state.ZBattlefield, p) {
		if reserved != nil && reserved[oid] {
			continue
		}
		o := e.G.Obj(oid)
		if o == nil || subCounterAvailable(o, part.Spec) < amt {
			continue
		}
		if e.matchesSpecFrom(part.Target, oid, p, source) {
			out = append(out, oid)
		}
	}
	return out
}

// subCounterAsk offers the next unsettled SubCounter cost part whose
// removal-target field names something other than the source (walking
// pc.cost.SubCounter in order, pc.subCounterPart). A source-anchored
// fixed-kind part and a zero-count part record nothing: their settle emits on
// the source, the pre-existing behaviour. A wildcard "Any" part -- source-
// anchored or filtered -- records one counter unit per pick (see
// wildcardCounterAsk). The chosen removals are recorded into
// pc.subCounterPays and the counters actually leave the object at payCast,
// exactly like the Sac parts' flow; a part with no remaining candidate aborts
// the whole cast/activation cleanly (sacAsk's unpayable-cost rule -- a cost
// that cannot be fully paid is never committed half paid). A sole candidate
// is recorded without an ask: a decision nobody could answer differently is
// never posed, and the settlement event records the object for replay.
func (e *Engine) subCounterAsk() bool {
	pc := e.cast
	for pc.subCounterPart < len(pc.cost.SubCounter) {
		part := pc.cost.SubCounter[pc.subCounterPart]
		amt := part.N
		if part.Announced {
			amt = pc.x
		}
		if amt <= 0 {
			pc.subCounterPart++
			continue
		}
		reserved := pc.subCounterReservations()
		if strings.EqualFold(part.Spec, "Any") {
			if e.wildcardCounterAsk(pc, part, amt) {
				return true
			}
			continue
		}
		if subCounterTargetsSource(part.Target) {
			pc.subCounterPart++
			continue
		}
		candidates := e.subCounterRemovalCandidates(pc.player, pc.card, part, amt, reserved)
		if len(candidates) == 0 {
			e.abortCast(pc, "counter-removal cost no longer payable; cast/activation aborted", true)
			return true
		}
		if len(candidates) == 1 {
			pc.subCounterPays = append(pc.subCounterPays, subCounterPay{part: pc.subCounterPart, obj: candidates[0]})
			pc.subCounterPart++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: "Choose a permanent to remove " + e.subCounterPhrase(part, amt) + " from", Source: pc.card}
		for _, oid := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "subcounter", Obj: oid, Label: e.G.Obj(oid).Face().Name})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

// subCounterReservations keeps an earlier counter-cost part from spending
// the same permanent as the current part. Wildcard units from the current
// part are deliberately absent: wildcardCounterAsk accounts for them by
// (object, kind), allowing the part itself to span objects and kinds.
func (pc *pendingCast) subCounterReservations() map[state.ObjID]bool {
	reserved := map[state.ObjID]bool{}
	for _, s := range pc.sacs {
		reserved[s] = true
	}
	for _, p := range pc.subCounterPays {
		if p.part != pc.subCounterPart {
			reserved[p.obj] = true
		}
	}
	return reserved
}

// wildcardCounterAsk advances a wildcard "Any" SubCounter part by one
// counter unit: it offers the remaining (permanent, counter-kind) units the
// payer may remove, asks when more than one is legal, and records the pick.
// It reports whether the ask loop must pause (a decision was posed); a false
// return means the part is fully paid or has advanced, and the caller's loop
// continues from the (possibly advanced) pc.subCounterPart. A wildcard part
// is settled one unit at a time so a single unit may come from a different
// kind or object than the next, exactly as "remove N counters from among ..."
// reads -- a kind's own count is NOT required to cover the whole amount.
func (e *Engine) wildcardCounterAsk(pc *pendingCast, part CostPart, amt int32) bool {
	picked := int32(0)
	used := map[state.ObjID]map[string]int32{}
	for _, p := range pc.subCounterPays {
		if p.part != pc.subCounterPart {
			continue
		}
		picked++
		if used[p.obj] == nil {
			used[p.obj] = map[string]int32{}
		}
		used[p.obj][p.kind]++
	}
	if picked >= amt {
		pc.subCounterPart++
		return false
	}
	candidates := e.subCounterRemovalCandidates(pc.player, pc.card, part, 1, pc.subCounterReservations())
	if len(candidates) == 0 {
		e.abortCast(pc, "counter-removal cost no longer payable; cast/activation aborted", true)
		return true
	}
	type choice struct {
		obj  state.ObjID
		kind string
	}
	var choices []choice
	for _, oid := range candidates {
		o := e.G.Obj(oid)
		if o == nil {
			continue
		}
		for _, c := range o.Counters {
			if c.N <= 0 {
				continue
			}
			if used[oid] != nil && used[oid][c.Kind] >= c.N {
				continue
			}
			choices = append(choices, choice{oid, c.Kind})
		}
	}
	if len(choices) == 0 {
		e.abortCast(pc, "counter-removal kind no longer payable; cast/activation aborted", true)
		return true
	}
	if len(choices) == 1 {
		pc.subCounterPays = append(pc.subCounterPays, subCounterPay{part: pc.subCounterPart, obj: choices[0].obj, kind: choices[0].kind})
		return false
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1, Prompt: "Choose a counter to remove", Source: pc.card}
	for _, c := range choices {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "subcounter", Obj: c.obj, Counter: c.kind, Label: fmt.Sprintf("%s (%s counter)", e.G.Obj(c.obj).Face().Name, c.kind)})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// subCounterPhrase renders the amount/kind half of a SubCounter part's
// removal for a decision prompt: "A +1/+1 counter", "CHARGE counters", "ANY
// counters". Display only.
func (e *Engine) subCounterPhrase(part CostPart, amt int32) string {
	unit := "counter"
	if amt != 1 && !part.Announced {
		unit = "counters"
	}
	return fmt.Sprintf("%s %s", strings.ToUpper(part.Spec), unit)
}

// settleSubCounterParts emits the SubCounter cost parts' counter removals,
// shared by the payment branches so every path settles the same shape. A
// source-anchored part (subCounterTargetsSource) removes from the paying
// source -- the pre-existing behaviour -- and a filtered part removes from
// the object its recorded subCounterPay carries. The "Any" kind removes one
// counter per recorded pick (each pick carries its chosen kind and object); a
// pending cast with no recorded pick falls back to the object's counter-list
// order, one CounterChange per kind until the amount is met.
func (e *Engine) settleSubCounterParts(pc *pendingCast) {
	for partIdx, part := range pc.cost.SubCounter {
		amt := part.N
		if part.Announced {
			amt = pc.x
		}
		if amt == 0 {
			// A zero removal emits nothing: a CounterChange of 0 would be a
			// no-op folded into state but a spurious log entry.
			continue
		}
		// The picks recorded for THIS part, by explicit part index. A
		// wildcard part has one entry per counter unit (each carrying its
		// chosen kind); a fixed-kind filtered part has one, carrying the
		// object it removes the whole amount from.
		var pays []subCounterPay
		for _, p := range pc.subCounterPays {
			if p.part == partIdx {
				pays = append(pays, p)
			}
		}
		if strings.EqualFold(part.Spec, "Any") {
			if len(pays) == 0 {
				// Legacy fallback for a hand-built/old pending cast with no
				// recorded pick: remove across kinds deterministically from the
				// source. The ordinary flow always records one entry per unit.
				pays = []subCounterPay{{part: partIdx, obj: pc.card}}
			}
			// Group the per-unit picks by (object, kind), preserving first-seen
			// order, so two units of the same kind settle as ONE CounterChange
			// of -2 (the pre-existing shape) while units of different kinds
			// settle as one event each.
			type payKey struct {
				obj  state.ObjID
				kind string
			}
			var order []payKey
			sum := map[payKey]int32{}
			for _, p := range pays {
				if p.kind == "" {
					continue
				}
				k := payKey{p.obj, p.kind}
				if _, seen := sum[k]; !seen {
					order = append(order, k)
				}
				sum[k]++
			}
			for _, k := range order {
				e.emit(events.Event{Kind: events.CounterChange, Obj: k.obj, Counter: k.kind, Amount: -sum[k]})
			}
			if len(order) > 0 {
				continue
			}
			// No kind was recorded (the legacy/fallback entry): remove across
			// kinds in counter-list order.
			o := e.G.Obj(pays[0].obj)
			if o == nil {
				return
			}
			left := amt
			for _, c := range o.Counters {
				if left <= 0 {
					break
				}
				if c.N <= 0 {
					continue
				}
				take := c.N
				if take > left {
					take = left
				}
				e.emit(events.Event{Kind: events.CounterChange, Obj: pays[0].obj, Counter: c.Kind, Amount: -take})
				left -= take
			}
			continue
		}
		target := pc.card
		if !subCounterTargetsSource(part.Target) {
			if len(pays) == 0 {
				// The ask stage guarantees an entry for every filtered part
				// this settle reaches; a missing one is a flow bug, not a
				// payment to silently skip.
				return
			}
			target = pays[0].obj
		}
		e.emit(events.Event{Kind: events.CounterChange, Obj: target, Counter: part.Spec, Amount: -amt})
	}
}

// sacAsk offers the next unsettled Sac cost part, walking pc.cost.Sac in
// order (pc.sacPart). The chosen sacrifices are excluded from each later
// part's candidates so one permanent can never pay two Sac parts of the
// same cost. castable already required a distinct-candidate assignment
// before this option was ever offered (the fix-round-1 gate), so a part
// with too few candidates here is a board that changed under the flow --
// most directly, an earlier part of the SAME cost consumed the remaining
// matching permanents (an `Sac` part earlier in the same cost, or a board
// that changed under a hand-built intent) that the later part now needs.
// The totality rule is that a cost that cannot be fully paid must not be
// committed with only part of it paid, so rather than skip the part and
// let commitCast validate mana only, such a part aborts the whole cast/
// activation cleanly, exactly as if it was never offered. No sacrifice has
// actually moved yet -- sacAsk only records the choices into pc.sacs; the
// MoveZone events are emitted by commitCast -- so clearing e.cast restores
// the pre-offer board and the Note leaves nothing behind.
func (e *Engine) sacAsk() bool {
	pc := e.cast
	for pc.sacPart < len(pc.cost.Sac) {
		part := pc.cost.Sac[pc.sacPart]
		var candidates []state.ObjID
		for _, oid := range e.sacrificeCostCandidates(pc.player, pc.card, part, pc.isAbility()) {
			already := false
			for _, s := range pc.sacs {
				if s == oid {
					already = true
					break
				}
			}
			if !already && (!pc.emerge || pc.sacPart != 0 || e.emergeSacPayable(pc, oid)) {
				candidates = append(candidates, oid)
			}
		}
		n := int(part.N)
		if part.Announced {
			// Sac<X/Spec>: the announced count (0 = sacrifice nothing -- Dargo's
			// "you MAY sacrifice any number"), already bounded by xAsk to the
			// candidates available then; no priority passes mid-flow, so the
			// board cannot shrink between announcement and this settle.
			n = int(pc.x)
			if n == 0 {
				pc.sacPart++
				pc.sacPaid = 0
				continue
			}
		}
		n -= pc.sacPaid
		if n <= 0 {
			pc.sacPart++
			pc.sacPaid = 0
			continue
		}
		// Continuation feasibility only matters when a LATER Sac part exists:
		// each unit of this part is then asked one at a time, and only the
		// candidates that leave a complete distinct assignment for this part's
		// remaining units and every later part are offered (the one home of
		// the legal-answer rule -- the offered options themselves carry it, so
		// Validate, Clamp and the bot cannot pick a stranding answer). The
		// LAST Sac part has nothing downstream to strand, so it keeps the
		// historical exact-N ask (Min == Max == n) with no feasibility walk --
		// the wire shape TestSacrificedAmountCountsSacrificedObjects,
		// announceSacX and the Sac<X> reduction offers pin (one N-of decision,
		// not N one-of asks).
		hasLaterSac := pc.sacPart+1 < len(pc.cost.Sac)
		if hasLaterSac {
			pools := make([][]state.ObjID, len(pc.cost.Sac))
			needs := make([]int, len(pc.cost.Sac))
			for i, futurePart := range pc.cost.Sac {
				needs[i] = int(futurePart.N)
				if futurePart.Announced {
					needs[i] = int(pc.x)
				}
				if i == pc.sacPart {
					needs[i] -= pc.sacPaid
				}
				pools[i] = e.sacrificeCostCandidates(pc.player, pc.card, futurePart, pc.isAbility())
			}
			candidates = feasibleSacrificeChoices(candidates, pools, needs, pc.sacs, pc.sacPart)
		}
		if n <= 0 || n > len(candidates) {
			// A cost that can no longer be fully paid must not commit half
			// paid (fix round 1, reviewer Important 1). Abort the whole
			// thing; nothing has moved yet.
			//
			// This site used to hand-roll the teardown (clear e.cast, emit the
			// Note) on the reasoning that it was "unreachable from a
			// well-formed offer after the castable gate". It was reachable,
			// and hand-rolling it is what made that reachability unbounded
			// rather than merely wasteful: abortCast is where a no-progress
			// abort holds the option out of the rest of the priority window
			// (suppress=true), and skipping it meant the identical board
			// re-offered the identical doomed cast forever. A live 4-player
			// game sat on turn 3 doing that until it was killed.
			//
			// Route through abortCast like every other unpayable-cost abort,
			// so this path gets the same liveness guarantee the Delve decline
			// has (see cast_liveness_test.go): the suppression lifts on the
			// first state-changing event, which is exactly when a retry could
			// succeed.
			e.abortCast(pc, "sacrifice cost no longer payable; cast/activation aborted", true)
			return true
		}
		// CARDNAME and NICKNAME are bare source-object references in Forge
		// sacrifice costs. When the source is their sole candidate, this exact
		// one-object payment has no player choice: record it and settle the next
		// cost part instead of posing a KChoose the player can only answer one
		// way. The candidate check above deliberately stays first, so a source
		// that has left the battlefield still takes the ordinary unpayable-cost
		// abort path.
		if part.N == 1 && len(candidates) == 1 && candidates[0] == pc.card &&
			(strings.EqualFold(part.Spec, "CARDNAME") || strings.EqualFold(part.Spec, "NICKNAME")) {
			pc.sacs = append(pc.sacs, pc.card)
			pc.sacPart++
			pc.sacPaid = 0
			continue
		}
		verb := "cast"
		if pc.isAbility() {
			verb = "activate"
		}
		// The wire shape: a part with a later Sac part is paid one unit per
		// decision (Min == Max == 1, every option individually feasibility-
		// filtered); the last Sac part is one exact-N decision, as before the
		// continuation work -- nothing downstream can be stranded by its
		// answer, so no validator rule is needed for it.
		dmin, dmax := n, n
		if hasLaterSac {
			dmin, dmax = 1, 1
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: dmin, Max: dmax,
			Prompt: "Sacrifice a permanent to " + verb + " " + e.G.Obj(pc.card).Face().Name,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "sacrifice",
				Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	// Every Sac part is settled: an Emerge cast (CR 702.118a) now folds the
	// chosen sacrifice's mana value out of pc.cost, before the mana window and
	// payment read it. Idempotent through pc.emergeDone, so the re-entries a
	// suspended mana window makes cannot subtract twice.
	e.applyEmergeReduction(pc)
	return false
}

// discardAsk settles Discard cost parts from the payer's hand. Ordinary
// specs pose the same exact-N KChoose used by sacrifice costs. Random parts
// consume the engine's seeded RNG and never ask the player; a Hand spec records
// every remaining hand card without asking. Nothing moves until commitCast,
// so an abort cannot leave a partially paid cost on the board.
func (e *Engine) discardAsk() bool {
	pc := e.cast
	for pc.discardPart < len(pc.cost.Discard) {
		part := pc.cost.Discard[pc.discardPart]
		reserved := make(map[state.ObjID]bool, len(pc.discards))
		for _, id := range pc.discards {
			reserved[id] = true
		}
		candidates := e.discardCandidates(pc.player, pc.card, part, !pc.isAbility(), reserved)

		if strings.EqualFold(part.Spec, "Hand") {
			pc.discards = append(pc.discards, candidates...)
			pc.discardPart++
			continue
		}
		n := int(part.N)
		if part.Announced {
			// The announced Discard<X/Spec>: exactly the announced X, and
			// X = 0 discards nothing.
			n = int(pc.x)
			if n == 0 {
				pc.discardPart++
				continue
			}
		}
		if n <= 0 || n > len(candidates) {
			e.abortCast(pc, "discard cost no longer payable; cast/activation aborted", true)
			return true
		}
		if strings.EqualFold(part.Spec, "Random") {
			for i := 0; i < n; i++ {
				pick := e.Rand(len(candidates))
				pc.discards = append(pc.discards, candidates[pick])
				candidates = append(candidates[:pick], candidates[pick+1:]...)
			}
			pc.discardPart++
			continue
		}

		d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: n, Max: n,
			Prompt: "Discard a card to pay the cost of " + e.G.Obj(pc.card).Face().Name,
			Source: pc.card}
		for _, id := range candidates {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "discard",
				Obj: id, Label: e.G.Obj(id).Face().Name})
		}
		e.choosing = chooseCast
		e.ask(d)
		return true
	}
	return false
}

func etbChoiceKind(api string) string {
	switch api {
	case "NameCard":
		return "name"
	case "ChooseType":
		return "type"
	case "ChooseNumber":
		return "number"
	case "ChooseColor":
		return "color"
	case "Clone":
		return "copy"
	}
	return ""
}

// etbPayLifeBound reports whether r's ReplaceWith$ body is the exact
// "as CARDNAME enters, pay any amount of life" shape -- a `Cost$
// Mandatory PayLife<X>` body whose face SVar:X is Count$xPaid (the announced
// value the body stores) -- and, if so, the largest X the payer may announce:
// the payer's life total, further capped by the body's `XMax$ <SVar>` when it
// names one that resolves (Nameless Race's Limit: the white permanents plus
// white cards in opponents' graveyards). A body that is not the exact shape
// (a fixed PayLife cost, another API, a missing Count$xPaid binding) returns
// false and keeps the ordinary replacement path.
func (e *Engine) etbPayLifeBound(o *state.Object, with *cards.SA) (int, bool) {
	if with == nil || o.Face() == nil {
		return 0, false
	}
	body, present := o.Face().SVars["X"]
	if !present || !strings.EqualFold(strings.TrimSpace(body), "Count$xPaid") {
		return 0, false
	}
	c := ParseCost(with.Params["Cost"])
	if len(c.LifeX) == 0 {
		return 0, false
	}
	bound := int(e.G.Players[o.Controller].Life)
	if bound < 0 {
		bound = 0
	}
	if raw := strings.TrimSpace(with.Params["XMax"]); raw != "" {
		ctx := &effects.Ctx{Source: o.ID, Controller: o.Controller, SVars: o.Face().SVars}
		if cap, ok := effects.NumResolved(e, ctx, with, "XMax", 0); ok {
			if cap < 0 {
				cap = 0
			}
			if int(cap) < bound {
				bound = int(cap)
			}
		}
	}
	return bound, true
}

// etbPayLifeOptions builds the ascending 0..bound option list a
// "pay any amount of life" entry offers; option 0 is the legal pay-nothing
// announcement (Oracle: "pay any amount" includes zero). The Kind is the
// shared "number" kind so the answer records through the same
// events.Choose fold a ChooseNumber uses, and resumeETBEntry reads the
// announced X off the option's Amount.
func etbPayLifeOptions(you state.PlayerID, card state.ObjID, bound int) []decision.Option {
	out := make([]decision.Option, 0, bound+1)
	for i := 0; i <= bound; i++ {
		label := strconv.Itoa(i) + " life"
		if i == 1 {
			label = "1 life"
		}
		out = append(out, decision.Option{Index: len(out), Kind: "paylife", Label: label, Amount: i, Obj: card, Player: you})
	}
	return out
}

// entryETBChoice returns the ordinal-th choice that must be made for ev's
// battlefield entry. It is deliberately derived from the same prospective
// MoveZone event the replacement matcher will later consume: an ActiveZones or
// ValidCard gate therefore cannot make the engine ask about a replacement that
// will not apply. The ordinal lets several choices on one permanent suspend
// and resume without adding transient state to the event log.
func (e *Engine) entryETBChoice(ev events.Event, ordinal int) (etbChoice, bool) {
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Face() == nil || ev.To != state.ZBattlefield {
		return etbChoice{}, false
	}
	you := o.Controller
	seen := 0
	if o.Face().HasKeyword("Riot") {
		if seen == ordinal {
			return etbChoice{kind: "riot", options: []decision.Option{
				{Index: 0, Kind: "riot", Label: "Enter with a +1/+1 counter", Obj: o.ID, Player: you},
				{Index: 1, Kind: "riot", Label: "Gain haste", Obj: o.ID, Player: you},
			}}, true
		}
		seen++
	}
	if o.Face().HasKeyword("Unleash") {
		if seen == ordinal {
			return etbChoice{kind: "unleash", options: unleashOptions(o.ID, you)}, true
		}
		seen++
	}
	for i := range o.Face().Repls {
		r := &o.Face().Repls[i]
		if r.With == nil || !e.replacementMatches(*r, o.ID, ev) {
			continue
		}
		if r.Params["Keyword"] != "ETBReplacement" {
			// A non-keyword R:Event$ Moved replacement whose ReplaceWith$ body
			// carries `Cost$ Mandatory PayLife<X>` (Minion of the Wastes,
			// Phyrexian Processor, Nameless Race: "as CARDNAME enters, pay any
			// amount of life"). The payer announces X here, at the entry
			// boundary, where a suspend-and-resume is possible -- the
			// replacement body itself runs off the Move fold with no ask
			// channel (the approximation this closes). Only the exact
			// Count$xPaid life-announcement shape is offered; anything else
			// falls through to the ordinary replacement path unchanged.
			bound, ok := e.etbPayLifeBound(o, r.With)
			if !ok {
				continue
			}
			if seen == ordinal {
				return etbChoice{kind: "paylife", options: etbPayLifeOptions(you, o.ID, bound)}, true
			}
			seen++
			continue
		}
		kind := etbChoiceKind(r.With.API)
		if kind == "" {
			continue
		}
		// The ETB Clone slice is deliberately narrow: offering a copy while
		// dropping an exception rider is worse than retaining today's loud
		// unimplemented-API fallback. A body outside the whitelist is not a
		// choice at all, so it is skipped before the ordinal is counted.
		if kind == "copy" && !etbCloneWhitelist(r.With, o.Face().SVars) {
			continue
		}
		if seen == ordinal {
			// The fifth filter slot means different things per kind: Choices$
			// is the copy-template selector the clone slice reads, while
			// ValidDescription$ is Forge prompt text for the name kinds, not
			// a second filter (effects.NameChoices reads it only as a safety
			// fallback when ValidCards$ is absent).
			selector := r.With.Params["ValidDescription"]
			if kind == "copy" {
				selector = r.With.Params["Choices"]
			}
			var opts []decision.Option
			if kind == "type" {
				// The type ask is category-aware (task ct1): a Type$ Basic Land
				// or Card or Planeswalker ranges over that category's real list,
				// exactly the list the mid-resolution ChooseType ask builds
				// (effects/type_choices.go), so the two asks cannot disagree.
				opts = e.typeChoiceOptions(you, o.ID, r.With.Params)
			} else {
				opts = e.etbOptions(you, o.ID, kind,
					r.With.Params["ValidCards"], selector,
					r.With.Params["Type"], r.With.Params["Exclude"], r.With.Params["ChooseFromList"])
			}
			if kind == "name" && len(opts) == 0 {
				// No name passes the filter: the legacy (no-universe) builder
				// only sees public objects, so "choose a nonbasic land card
				// name" with none in view (Alpine Moon, cardfuzz batch1 line
				// 14) built a Min 1 ask with zero options that no answer
				// could satisfy. Mirror effNameCard's own empty-list rule so
				// the two NameCard paths agree: without a corpus universe (or
				// with no ChooseFromList$) it names the deterministic legacy
				// stand-in; a universe-backed ChooseFromList$ with nothing
				// eligible names nothing, so there is no choice to pose and
				// the entry proceeds (the body's effNameCard then returns
				// without naming, as it does mid-resolution).
				if len(e.G.NameUniverse) > 0 && strings.TrimSpace(r.With.Params["ChooseFromList"]) != "" {
					continue
				}
				opts = []decision.Option{{Index: 0, Kind: "name",
					Label: effects.LegacyNameFallback(e.G, you)}}
			}
			if kind == "copy" {
				// ":Optional" on the keyword line is the "you MAY have it
				// enter as a copy" half; an empty template list also needs
				// the decline, or the ask would have zero options (the
				// totality rule in etbOptions' doc).
				optional := strings.Contains(strings.ToLower(r.Params["KeywordLine"]), ":optional")
				if optional || len(opts) == 0 {
					opts = append(opts, decision.Option{Index: len(opts), Kind: "clone",
						Label: "Enter as itself", Player: you})
				}
			}
			return etbChoice{kind: kind, options: opts}, true
		}
		seen++
	}
	return etbChoice{}, false
}

// etbColourLabels pairs the WUBRG letter the Choose event records with the
// option label the client shows, in fixed WUBRG order -- the same order every
// colour choice in this build offers (askManaColor, triggeredManaColourChoice,
// commanderIdentityColours). etbOptions and resumeETBEntry both read it, so
// the option offered and the letter recorded always agree.
var etbColourLabels = []struct{ letter, name string }{
	{"W", "White"}, {"U", "Blue"}, {"B", "Black"}, {"R", "Red"}, {"G", "Green"},
}

// etbColourLetter maps an option label (or already-a-letter) back to the
// WUBRG letter the event records; "" when the label is neither (the entry
// continuation only sees options etbOptions built, so the guard is defensive).
func etbColourLetter(name string) string {
	for _, cl := range etbColourLabels {
		if strings.EqualFold(name, cl.name) || strings.EqualFold(name, cl.letter) {
			return cl.letter
		}
	}
	return ""
}

// etbOptions builds the option list for one "as this enters" choice. It is a
// total list-pick -- every entryETBChoice caller is guaranteed at least one
// legal option (a name is anything on the board/hand/yard, a type falls back
// to "Human", a number is always 0..12) -- so no etb decision can ever be
// handed out with zero options, and nothing asks an empty choice (R-9's
// totality rule; see the Options here and the entry decision's Min/Max 1).
//
// Option list order is deterministic: names and types are sorted strings
// (never from a map), numbers are ascending.
// choices carries the per-kind filter text: Choices$ (the copy-template
// selector) for the clone slice, ValidDescription$ prompt text for the name
// kinds. Callers fill it per kind; see collectETBChoices.
func (e *Engine) etbOptions(you state.PlayerID, card state.ObjID, kind, validCards, choices, typeCategory, exclude string, chooseFromList ...string) []decision.Option {
	switch kind {
	case "color":
		// Exclude$ tokens (comma-separated, e.g. "black" on Black Dragon
		// Gate) remove the matching WUBRG label. Fail OPEN: a token
		// etbColourLetter cannot resolve is ignored, never emptied into an
		// ask with zero options (the totality rule in this doc comment).
		excluded := map[string]bool{}
		for tok := range strings.SplitSeq(exclude, ",") {
			if letter := etbColourLetter(strings.TrimSpace(tok)); letter != "" {
				excluded[letter] = true
			}
		}
		out := make([]decision.Option, 0, len(etbColourLabels))
		for _, cl := range etbColourLabels {
			if excluded[cl.letter] {
				continue
			}
			out = append(out, decision.Option{Index: len(out), Kind: "color", Label: cl.name})
		}
		if len(out) == 0 {
			// Totality guard: an exclusion naming every colour must never
			// empty the ask (corpus carriers exclude exactly one; this is
			// defensive against a future carrier).
			out = make([]decision.Option, 0, len(etbColourLabels))
			for _, cl := range etbColourLabels {
				out = append(out, decision.Option{Index: len(out), Kind: "color", Label: cl.name})
			}
		}
		return out
	case "copy":
		spec := strings.TrimSpace(choices)
		if spec == "" {
			spec = strings.TrimSpace(validCards)
		}
		if spec == "" {
			spec = "Creature.Other"
		}
		if !strings.Contains(spec, ".") && !strings.HasPrefix(spec, "Card") {
			spec = "Card." + spec
		}
		out := []decision.Option{}
		for _, p := range e.G.AliveFrom(0) {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				o := e.G.Obj(id)
				if o != nil && o.Face() != nil && effects.MatchesSpecFrom(e.G, spec, id, you, card) {
					out = append(out, decision.Option{Index: len(out), Kind: "clone", Obj: id, Label: o.Face().Name})
				}
			}
		}
		return out
	case "name":
		// A no-universe Config is a pre-feature match on replay. Its visible
		// object builder, including the Card.nonLand default and its full
		// MatchesSpecFrom semantics, is retained byte-for-byte below; changing
		// it would invalidate persisted ETB NameCard logs.
		if len(e.G.NameUniverse) == 0 {
			return e.legacyETBNameOptions(you, card, validCards)
		}
		// NameCard ranges over the compiled card-name universe, not public
		// objects currently visible to the chooser: Pithing Needle names any
		// card (a land included) and Revoker/Cabal Therapy name a nonland,
		// both through the SA's own ValidCards$ filter. An omitted
		// ValidCards$ is intentionally unrestricted. effects.NameChoices is
		// the ONE builder the mid-resolution NameCard ask shares, so the two
		// paths offer the same names. (choices carries ValidDescription$
		// prompt text here; see collectETBChoices.)
		list := ""
		if len(chooseFromList) > 0 {
			list = chooseFromList[0]
		}
		names := effects.NameChoicesFromList(e.G, validCards, choices, list)
		out := effects.NameOptions(names, you)
		if out == nil {
			out = []decision.Option{}
		}
		return out
	case "type":
		// The shared, category-aware enumeration; a caller that reaches here
		// with a non-creature category (a body that did not go through
		// entryETBChoice's category dispatch) still gets the real list, never a
		// creature-type list. This arm carries no ValidTypes$/InvalidTypes$
		// (the positional slots above are ValidCards$/Exclude$, different
		// params); the ETB dispatch passes the whole parameter map to
		// typeChoiceOptions instead.
		return e.typeChoiceOptions(you, card, map[string]string{"Type": typeCategory})
	default: // "number"
		// The shared 0..N list (task cli-20260923T060000Z-choose-number:
		// effects/number_choices.go is the ONE home), so the as-enters ask
		// and the mid-resolution ChooseNumber ask cannot disagree.
		return effects.NumberChoices()
	}
}

// legacyETBNameOptions is the exact pre-name-universe ETB builder. It stays
// separate from the corpus path because a sidecar without NameUniverse is an
// old log: its DecisionAsk options, including an empty ValidCards$ defaulting
// to Card.nonLand, must replay byte-for-byte.
func (e *Engine) legacyETBNameOptions(you state.PlayerID, card state.ObjID, validCards string) []decision.Option {
	if validCards == "" {
		validCards = "Card.nonLand"
	}
	seen := map[string]bool{}
	names := []string{}
	add := func(z state.Zone, players []state.PlayerID) {
		for _, p := range players {
			for _, id := range e.G.Zone(z, p) {
				o := e.G.Obj(id)
				if o == nil || o.Face() == nil {
					continue
				}
				if !effects.MatchesSpecFrom(e.G, validCards, id, you, card) {
					continue
				}
				if seen[o.Face().Name] {
					continue
				}
				seen[o.Face().Name] = true
				names = append(names, o.Face().Name)
			}
		}
	}
	add(state.ZHand, []state.PlayerID{you})
	add(state.ZBattlefield, e.G.AliveFrom(0))
	add(state.ZGraveyard, e.G.AliveFrom(0))
	sort.Strings(names)
	out := make([]decision.Option, 0, len(names))
	for _, n := range names {
		out = append(out, decision.Option{Index: len(out), Kind: "name", Label: n})
	}
	return out
}

// isCreatureFace is a local creature test (effects.hasType is unexported);
// reads the printed Types, which is all any creature-subtype enumeration
// needs.
func isCreatureFace(f *cards.Face) bool {
	for _, t := range f.Types {
		if t == "Creature" {
			return true
		}
	}
	return false
}

// creatureTypeOptions enumerates the creature-type option list the cast-time
// "as this enters" ask (etbOptions' "type" arm) and the mid-resolution
// ChooseType ask (Engine.TypeChoices, task ct1) BOTH offer, so the two asks
// and the no-ask fallback can never disagree about what a creature-type
// choice ranges over. The list is the distinct creature subtypes of every
// object you OWN (all zones, object order), sorted alphabetically; the
// "Human" tail keeps the list non-empty when you own no creature subtype,
// the same totality rule the colour list carries.
func (e *Engine) creatureTypeOptions(you state.PlayerID) []decision.Option {
	seen := map[string]bool{}
	types := []string{}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != you {
			continue
		}
		f := o.Face()
		if f == nil || !isCreatureFace(f) {
			continue
		}
		for _, t := range f.Types {
			if !effects.CreatureTypeWords(t) || seen[t] {
				continue
			}
			seen[t] = true
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		types = []string{"Human"}
	}
	sort.Strings(types)
	out := make([]decision.Option, 0, len(types))
	for _, t := range types {
		out = append(out, decision.Option{Index: len(out), Kind: "type", Label: t})
	}
	return out
}

// typeChoiceOptions builds the option list for one ChooseType Type$ category
// (task ct1), the ONE builder both the as-enters ask (entryETBChoice's "type"
// dispatch) and etbOptions' "type" arm use. A creature (or absent) category
// keeps the owner-scoped creatureTypeOptions; the context-scoped Shared
// category reads the entering object's own exiled-with set; every other
// enumerable category reads effects.TypeChoiceLabels' static list. A category
// that yields no list (an unresolvable context, or one this build still
// cannot name) falls back to the creature list so the ask is never emptied --
// the totality rule every as-enters ask lives by.
func (e *Engine) typeChoiceOptions(you state.PlayerID, source state.ObjID, params map[string]string) []decision.Option {
	cat := strings.TrimSpace(params["Type"])
	if cat == "" || strings.EqualFold(cat, "Creature") {
		return e.creatureTypeOptions(you)
	}
	var labels []string
	if strings.EqualFold(cat, "Shared") {
		labels = effects.SharedTypeLabels(e.G, source)
	} else {
		labels = effects.TypeChoiceLabels(cat, params["ValidTypes"], params["InvalidTypes"])
	}
	if len(labels) == 0 {
		return e.creatureTypeOptions(you)
	}
	out := make([]decision.Option, 0, len(labels))
	for _, label := range labels {
		out = append(out, decision.Option{Index: len(out), Kind: "type", Label: label})
	}
	return out
}

// TypeChoices implements effects.Host.TypeChoices (task ct1): the option list
// a mid-resolution ChooseType ask offers its chooser. Only the creature
// category reaches this Host method now -- the creature list is owner-scoped
// and lives here, while effects/type_choices.go builds the non-creature
// categories (and Shared/ CreatureInTargetedDeck from the resolving effect's
// own context) directly. An absent or "Creature" category returns the shared
// creatureTypeOptions; any other category yields nil, which the asking effect
// no longer reaches (it answers those categories itself).
func (e *Engine) TypeChoices(chooser state.PlayerID, category string) []decision.Option {
	if category != "" && !strings.EqualFold(category, "Creature") {
		return nil
	}
	return e.creatureTypeOptions(chooser)
}

// etbChoicePrompt names the kind of an "as this enters" choice for a client
// prompt; a cosmetic suffix on the shared "Choose" heading.
func etbChoicePrompt(kind string) string {
	switch kind {
	case "name":
		return " a card name"
	case "type":
		return " a creature type"
	case "color":
		return " a color"
	case "riot":
		return " how this creature enters (counter or haste)"
	case "unleash":
		return " how this creature enters (with a +1/+1 counter or without)"
	case "copy":
		return " a creature to copy"
	case "paylife":
		return " how much life to pay"
	}
	return " a number"
}

// etbCloneWhitelist reports whether a DB$ Clone ETB body's rider set is
// entirely inside the supported scope: Choices$ (the copy-template selector),
// AddTypes$ and AddKeywords$ (the CR 707.9e copy modifiers) and
// SpellDescription$. This is a POSITIVE whitelist over the parsed parameter
// keys -- the param census's case-whitelist range shape -- never a blacklist:
// an explicit key list cannot keep up with the corpus. The round-1 blacklist
// missed IntoPlayTapped$ (Vesuva), ChoiceTitle$ (Mirrorhall Mimic),
// Embalm$-provenance riders (Vizier of Many Faces), AddColors$, RemoveCost$,
// PumpKeywords$/PumpDuration$ and the AI-hint params, each of which offered a
// copy that silently dropped the exception. A body carrying any other
// parameter keeps today's loud unimplemented-API fallback (the etbclone1
// scope boundary); rules/etb_clone_whitelist_census_test.go pins the
// classified population bidirectionally.
func etbCloneWhitelist(sa *cards.SA, svars map[string]string) bool {
	for k := range sa.Params {
		switch k {
		case "Choices", "AddKeywords", "AddTypes", "SpellDescription", "AddStaticAbilities", "IntoPlayTapped":
			// supported: the copy-template selector, the CR 707.9e
			// copy modifiers, and (staticgoad1) the granted Goad$ static
			// effClone registers -- value-checked below.
		default:
			return false
		}
	}
	// A supported KEY is not a supported VALUE. Two value shapes inside the
	// key whitelist are withheld too, because admitting them offered a route
	// that silently did the wrong thing:
	//
	//  - A Choices$ selector carrying a predicate whose right-hand side is an
	//    SVar rather than a literal (Mockingbird's "Creature.Other+cmcLEY",
	//    Y = Count$CastTotalManaSpent). Both the option build (etbOptions)
	//    and the replacement-time revalidation (effects' cloneETBTemplateLegal)
	//    match through MatchesSpecFrom, which has no resolver, so every such
	//    predicate answers "recognised shape, never matches": the election
	//    would offer nothing but the decline at every paid X. Supporting it
	//    needs the choice deferred past payment with the cast's mana total
	//    bound as the RHS resolver -- not this task.
	//  - An AddKeywords$ member whose head is not a single word. Forge's
	//    conditional modifier grammar rides that space ("IfNew Vanishing:3",
	//    Flesh Duplicate: vanishing 3 only if the copied creature has no
	//    vanishing), and effClone installs the raw member as a layer-6
	//    AddKeywords grant, so cards.KeywordHead would read the head as
	//    "IfNew Vanishing" -- no conditional test, no vanishing, no entry
	//    time counters, silently. This is deliberately conservative: it also
	//    withholds a body whose modifier is a legitimate multi-word keyword
	//    ("First Strike"), a shape no ETB Clone carrier has today.
	if effects.SpecNeedsResolver(strings.TrimSpace(sa.Params["Choices"])) {
		return false
	}
	for _, kw := range cards.SplitKeywordList(sa.Params["AddKeywords"]) {
		if strings.ContainsAny(cards.KeywordHead(kw), " \t") {
			return false
		}
	}
	// A named static is installed on the cloned face by CloneStatic, so
	// every static reader sees it through its normal printed-S: path. An
	// unresolvable member still fails closed before posing the ETB election.
	for _, name := range strings.FieldsFunc(sa.Params["AddStaticAbilities"], func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		if !effects.CloneStaticGrantReadable(svars, name) {
			return false
		}
	}
	if raw, ok := sa.Params["IntoPlayTapped"]; ok && !strings.EqualFold(raw, "True") {
		return false
	}
	return true
}

// announcePip resolves the i-th announcement pip of a cost's hybrid →
// monocolour-hybrid → Phyrexian → hybrid-Phyrexian list into its alternative
// payments, in the order manaAsk offers them (each colour, then a generic
// face, then life). It is the single source both manaAsk (the ask's
// valid-option set) and castAnswer (recording the choice) consult, so the
// option offered and the recorded choice always agree. Snow pips are not
// announcement pips: a {S} pip has no alternative payment to announce.
func (c Cost) announcePip(i int) []pipAlt {
	if i < len(c.Hybrid) {
		p := c.Hybrid[i]
		return []pipAlt{{color: p.A}, {color: p.B}}
	}
	i -= len(c.Hybrid)
	if i < len(c.Twobrid) {
		t := c.Twobrid[i]
		alts := []pipAlt{{color: t.Col}}
		if t.Generic > 0 {
			alts = append(alts, pipAlt{generic: t.Generic})
		}
		return alts
	}
	i -= len(c.Twobrid)
	if i < len(c.Phyrexian) {
		letter := c.Phyrexian[i]
		return []pipAlt{{color: letter}, {life: 2}}
	}
	i -= len(c.Phyrexian)
	hp := c.HybridPhyrexian[i]
	return []pipAlt{{color: hp.A}, {color: hp.B}, {life: 2}}
}

// annPipCount is how many announcement pips a cost carries: the two-colour
// isAbility reports whether this proposal activates an ABILITY (a printed
// Face().Abilities index or a granted SVar anchor) rather than casting a
// spell. Every "is this an ability" test in the flow reads this, never the
// raw index, so a granted proposal -- whose ability field is -1 -- takes the
// ability arms (no spell legality recheck, no cast trigger, the ability
// payment/mint branch, no modes ask) instead of the spell ones.
func (pc *pendingCast) isAbility() bool {
	return pc.ability >= 0 || pc.grantSVar != "" || pc.gainedFrom != 0 || pc.grantKeyword != ""
}

// pcAbility resolves the proposal's ability body. A printed activation reads
// its Face().Abilities index; a granted activation (task grantcost1) resolves
// the SVar anchor off the GRANTOR's face -- the recipient's face has no such
// SVar, which is the whole reason the anchor exists. The resolve is
// deterministic (ParseSVar over a fixed table), so re-resolving at each
// read-site cannot drift; a grantor that left the battlefield (or an SVar the
// grantor's face no longer names -- a stale proposal) resolves to nil and the
// caller degrades the way a stale option always has.
func (e *Engine) pcAbility(pc *pendingCast) *cards.SA {
	if pc.grantKeyword != "" {
		// A keyword-granted body (CR 613.1f, the AddKeyword$ Cycling/TypeCycling
		// route): synthesized from the derived keyword line, a pure function of
		// the string -- no state read, so every read site and a replay re-derive
		// the identical SA. A line no synthesizer can model resolves nil and the
		// caller degrades the way a stale option always has.
		return cards.GrantedCyclingAbility(pc.grantKeyword)
	}
	if pc.gainedFrom != 0 {
		// A has-all-abilities-of body: the SA is the named foreign face's
		// own compiled ability at gainedIdx. A card that left the scoped zone
		// (or a stale index) resolves to nil and the caller degrades the way
		// a stale option always has.
		fo := e.G.Obj(pc.gainedFrom)
		if fo == nil || fo.Face() == nil {
			return nil
		}
		abilities := fo.Face().Abilities
		if pc.gainedIdx < 0 || pc.gainedIdx >= len(abilities) {
			return nil
		}
		return abilities[pc.gainedIdx]
	}
	if pc.grantSVar == "" {
		if pc.ability < 0 {
			return nil
		}
		o := e.G.Obj(pc.card)
		if o == nil || o.Face() == nil {
			return nil
		}
		// CR 702.140d: the index is a FLAT pile index (top face first, then
		// each under-card), the one enumeration the offer loop, events.Apply
		// and the activation-limit census share -- never a bare
		// Face().Abilities index, which would name a different ability on a
		// mutated pile. A plain permanent's index is unchanged.
		pa, ok := o.PileAbilityAt(pc.ability)
		if !ok {
			return nil
		}
		return pa.SA
	}
	return e.grantedSAFrom(pc.grantSource, pc.card, pc.grantSVar)
}

// cyclingKeyword returns the Forge keyword head ("Cycling" or "TypeCycling")
// when the activation pc's own ability is a cycling ability (CR 702.29), and
// "" for every other activation. It is the provenance events.DiscardCostCycling
// records on the cost discard: the discard is a cycle because THIS ability
// paid for it, whether the cycling is printed (K:Cycling) or granted (a layer's
// AddKeyword$ Cycling / K:TypeCycling, e.g. Rhet-Tomb Mystic, Tectonic
// Reformation, Homing Sliver). Resolved through pcAbility, so it names the
// ability a granted activation actually resolved rather than the card's face.
// TypeCycling is a variant of cycling (CR 702.29d's "[type]cycling"), so it
// tags a Mode$ Cycled trigger too.
func (e *Engine) cyclingKeyword(pc *pendingCast) string {
	ab := e.pcAbility(pc)
	if ab == nil {
		return ""
	}
	switch kw := strings.TrimSpace(ab.Params["Keyword"]); kw {
	case "Cycling", "TypeCycling":
		return kw
	default:
		return ""
	}
}

// hybrids, the monocolour hybrids, the Phyrexian pips and the
// hybrid-Phyrexian pips (snow pips have nothing to announce).
func (c Cost) annPipCount() int {
	return len(c.Hybrid) + len(c.Twobrid) + len(c.Phyrexian) + len(c.HybridPhyrexian)
}

// resolvedMana returns the cost the announced payment actually commits: X
// folded, every hybrid and Phyrexian pip removed (each was announced by
// manaAsk into payColor/payLife), and the announced coloured spend folded
// into Colored so payMana charges it from the pool. payLife is applied
// separately by payCast. For a cost with no hybrid or Phyrexian pip this is
// just the X-folded cost, so ordinary casting is unchanged.
func (pc *pendingCast) resolvedMana() Cost {
	m := pc.cost.WithX(pc.x)
	m.Hybrid = nil
	m.Phyrexian = nil
	m.Twobrid = nil
	m.HybridPhyrexian = nil
	for i := range pc.payColor {
		m.Colored[i] += pc.payColor[i]
	}
	m.Generic += pc.payGeneric
	return m
}

// resolvedMana is the X-folded, pip-resolved cost (see above). manaToPay is
// the full CR 601.2f composition on top of it.
func (pc *pendingCast) resolvedManaX(x int32) Cost {
	m := pc.cost.WithX(x)
	m.Hybrid = nil
	m.Phyrexian = nil
	m.Twobrid = nil
	m.HybridPhyrexian = nil
	for i := range pc.payColor {
		m.Colored[i] += pc.payColor[i]
	}
	m.Generic += pc.payGeneric
	return m
}

// repriceForTargets refreshes the modifier snapshot after CR 601.2c chooses
// targets and before CR 601.2h pays. ValidTarget$ is necessarily unavailable
// at the initial offer, but it is a cost requirement rather than a
// resolution-time condition, so this is the one point every spell and
// activation can apply it. It deliberately preserves taxGeneric: commander
// tax is independent of the chosen target and is captured at proposal start.
func (e *Engine) repriceForTargets(pc *pendingCast) {
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return
	}
	// Strive (CR 702.52a): the per-extra-target additional cost is priced
	// here, the same post-601.2c pre-601.2h seam the modifier refresh below
	// owns, because the payment count (targets minus one) exists only once
	// the target answer is in. Spell-only: an activated ability's face
	// carries no Strive and pc.targets means something else on that path.
	if !pc.isAbility() && pc.striveSet {
		e.foldStriveCost(pc, o.Face())
	}
	scope := spellScope(pc.mode)
	if ab := e.pcAbility(pc); ab != nil {
		scope = abilityScope(ab)
		// The ability's own target-dependent ReduceCost$ (Raft Security
		// Officer's AllTargeted$Valid Creature.powerLE3): beginActivation
		// folded pc.ownReduce with nil targets (full price at offer time,
		// fail closed); now the CR 601.2c answer exists, so re-evaluate and
		// net-adjust the generic by the delta. The Ctx binds BOTH the root's
		// own targets and the whole-chain union (alltargeted1): the sub-ask
		// answers are already in hand -- Forge pre-asks the chain before
		// payment -- so an AllTargeted$ body reads the union, not 0. The net
		// form is idempotent -- a second pass computes delta 0 -- which
		// matters because repriceForTargets can run again on a mana-window
		// resume, and once more per sub-ask answer as the union grows.
		if n := e.ownReduceCost(pc.player, pc.card, ab, pc.targets, pc.allTargets(), pc.abilityMerged); n != pc.ownReduce {
			pc.cost.Generic = addClampedGeneric(pc.cost.Generic, int64(pc.ownReduce-n))
			pc.ownReduce = n
		}
	}
	pc.mods = e.costModifiersForTargets(pc.player, pc.card, scope, pc.targets)
}

// foldStriveCost prices the Strive keyword's per-extra-target additional
// cost (CR 702.52, "this spell costs <cost> more for each target beyond the
// first") into pc.cost now that CR 601.2c's answer is known. The count is
// len(pc.targets)-1; striveUnits records how many payments are already
// folded, so a re-entry (a sub-ask answer re-runs repriceForTargets, the
// ownReduce delta shape) is an exact delta and never double-charges. An
// unpriceable parameter (ParseCost's degraded Unknown tokens) is a loud
// Note and no charge (the escalate convention), never a fabricated generic.
// Targets only grow across the flow's re-entries, so a shrinking want is
// unreachable; it is clamped to no-op rather than refunded.
func (e *Engine) foldStriveCost(pc *pendingCast, f *cards.Face) {
	// A stale proposal whose face no longer carries the keyword charges
	// nothing rather than reading the last captured parameter blindly.
	if _, ok := f.KeywordParam("Strive"); !ok {
		return
	}
	want := int32(len(pc.targets)) - 1
	if want < 0 {
		want = 0
	}
	if want == pc.striveUnits {
		return
	}
	sc := ParseCost(pc.striveParam)
	if len(sc.Unknown) > 0 {
		if want > pc.striveUnits {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
				Text: "strive cost unpriceable; casting without the strive charge"})
		}
		pc.striveUnits = want
		return
	}
	for i := pc.striveUnits; i < want; i++ {
		pc.cost = pc.cost.Plus(sc)
	}
	pc.striveUnits = want
}

// targetDependentCostMayPay is targetAsk's pre-payment exception: before a
// target is selected, the ordinary modifier snapshot intentionally excludes
// ValidTarget$ statics. Do not abort the proposal merely because that base
// snapshot is unaffordable when some legal target can make a reduction apply;
// repriceForTargets will replace the potential snapshot with the actual one
// as soon as the target answer arrives.
func (e *Engine) targetDependentCostMayPay(pc *pendingCast) bool {
	scope, ok := e.pendingCastScope(pc)
	if !ok {
		return false
	}
	delve := int32(0)
	if !pc.isAbility() {
		delve = int32(len(pc.delve))
	}
	_, ok = e.potentialCostModsUsing(e.collectCostStatics(), pc.player, pc.card, scope, e.costPotentialTargets(pc.player, pc.card, scope), 0, func(mods costMods) bool {
		return e.manaFeasibleDescriptor(pc.player, paymentForCast(pc, pc.resolvedMana()), pc.resolvedMana(), mods, pc.taxGeneric, delve, pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType})
	})
	return ok
}

// pendingCastScope returns the exact spell or ability scope whose modifiers
// price pc. Keeping this derivation shared by the potential-target gate and
// the final target menu makes a new ValidSpell$/Type$ rule reach both sides
// of the cast transaction rather than admitting a target the payment phase
// will price under different modifiers.
func (e *Engine) pendingCastScope(pc *pendingCast) (costScope, bool) {
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return costScope{}, false
	}
	if !pc.isAbility() {
		return spellScope(pc.mode), true
	}
	ab := e.pcAbility(pc)
	if ab == nil {
		return costScope{}, false
	}
	return abilityScope(ab), true
}

// affordableTargetCandidates filters legal CR 115 targets to the choices
// whose final target-dependent cost can complete this transaction. A target
// is tested as the sole selection: that is exact for the normal one-target
// shape and conservatively safe for multi-target declarations (where the
// decision API cannot express that one option requires another option).
//
// The probe also reprices the ability's own target-dependent ReduceCost$
// per candidate (belt_of_giant_strength's Targeted$CardPower): the offer
// gate folded the BEST legal target's reduction into pc.cost (pc.ownReduce),
// so without this fold a weaker candidate reads payable at the best-target
// offer price while repriceForTargets will actually charge its own, higher
// price at CR 601.2h -- an abort after an apparently-legal target choice.
// The delta is exactly the net shift repriceForTargets applies (same
// helper, idempotent fold); it is never negative because the offer's max
// runs over the same candidate set costPotentialTargets derives from
// legalTargetCandidates, and the clamp keeps that invariant load-bearing.
//
// The probe is a pure read (it emits nothing and writes no state; the
// convoke fold and window census are probed, never charged), so it runs in
// one Derived memo scope (derivedmemo.go): the per-candidate cost checks
// re-derive the same objects -- nonManaCastable's discard census matches
// every hand card once PER CANDIDATE -- and a mass of candidates otherwise
// makes the ask O(candidates x hand) layer walks.
func (e *Engine) affordableTargetCandidates(pc *pendingCast, candidates []targetCandidate) []targetCandidate {
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	scope, ok := e.pendingCastScope(pc)
	if !ok {
		return nil
	}
	pl := e.G.Players[pc.player]
	// Only a root-target-dependent own reduction needs a proven window
	// reachability check. Other activations retain their existing mana-window
	// offer semantics (including sources this static probe cannot price).
	targetDiscount := pc.isAbility() && pc.ownReduce > e.ownReduceCost(pc.player, pc.card, e.pcAbility(pc), nil, nil, pc.abilityMerged)
	var windowUnits []windowManaUnit
	if targetDiscount {
		windowUnits = e.castWindowUnits(pc)
	}
	out := make([]targetCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		target := state.Target{Obj: candidate.obj}
		if candidate.kind == "player" {
			target = state.Target{Player: candidate.player, IsPlayer: true}
		}
		mods := e.costModifiersForTargets(pc.player, pc.card, scope, []state.Target{target})
		cost := mods.apply(pc.resolvedMana())
		if pc.ownReduce > 0 {
			if n := e.ownReduceCost(pc.player, pc.card, e.pcAbility(pc), []state.Target{target}, nil, pc.abilityMerged); n < pc.ownReduce {
				cost.Generic = addClampedGeneric(cost.Generic, int64(pc.ownReduce-n))
			}
		}
		// An announce-bound Exile part (the Shoal cycle's cmcEQX) is priced
		// by the ANNOUNCED X, not by the candidate: nonManaCastable below
		// evaluates a non-literal cmc comparison fail-closed (it has no X
		// binding in scope), so an alt-cost cast whose only payment is such
		// an exile would drop every candidate and reverse the proposal at
		// the target ask. The offer gate (altCostXCandidates' existential
		// over X) and the X ask settled the part's payability independent of
		// any target, so the probe re-runs the exact exAsk binding for it
		// rather than dropping it silently. Parts are collected and filtered
		// AFTER the loop — mutating cost.Exile mid-range would splice wrong
		// indices once a second announce-bound part exists.
		exileSpent := make([]bool, len(cost.Exile))
		for i, part := range cost.Exile {
			if pc.announceX == "" || !strings.Contains(part.Spec, "cmcEQ"+pc.announceX) {
				continue
			}
			zone := part.Zone
			if zone == 0 {
				zone = state.ZHand
			}
			sc := e.withNames(effects.SpecContext{You: pc.player, Source: pc.card, Resolve: func(n string) (int32, bool) {
				if n == pc.announceX {
					return pc.x, true
				}
				return 0, false
			}})
			n := 0
			for _, oid := range e.G.Zone(zone, pc.player) {
				if e.matchesSpec(part.Spec, oid, sc) {
					n++
				}
			}
			if n >= int(part.N) {
				exileSpent[i] = true
			}
		}
		for _, spent := range exileSpent {
			if !spent {
				continue
			}
			kept := make([]CostPart, 0, len(cost.Exile))
			for i, part := range cost.Exile {
				if !exileSpent[i] {
					kept = append(kept, part)
				}
			}
			cost.Exile = kept
			break
		}
		cost.Generic = addClampedGeneric(cost.Generic, int64(pc.taxGeneric))
		if !pc.isAbility() {
			cost.Generic -= int32(len(pc.delve))
			if cost.Generic < 0 {
				cost.Generic = 0
			}
		}
		// Mana abilities cannot make a non-mana payment or a life shortage
		// disappear, so preserve a candidate for the mana window only after
		// those independent requirements pass.
		if !e.nonManaCastable(pc.player, pc.card, cost, pc.isAbility()) {
			continue
		}
		// A life cost with a POSITIVE component needs that much life (CR
		// 119.4: paying N>0 life requires life >= N). A cost with NO life
		// component pays 0 life, which is always legal whatever the life
		// total -- a payer dropped below 0 mid-cast (Ancient Tomb's rider)
		// may still select targets and finish paying (CR 704.3: state-based
		// actions wait for a player to receive priority). The same gate
		// lives in resolveManaWith, which payCast charges through.
		if cost.Life > 0 && cost.Life > pl.Life {
			continue
		}
		// resolvedMana carries no live pip, so manaFeasible (the shared
		// primitive) here degenerates to the composed payable check — the same
		// composition payCast will charge for this candidate's repricing. The
		// announced Convoke/Harmonize/Improvise contributions fold in exactly
		// the way paymentMana folds them into the charged total (applyConvoke
		// on the composed mods+tax+delve cost), so a cast whose pool alone
		// cannot pay but whose announced artifacts/creatures can keeps its
		// targets on the menu instead of being reversed at this ask. The
		// announcement itself was already gate-checked for absorbability
		// (convokeAbsorbs), so the fold is the payment's own arithmetic,
		// probed, never charged.
		convoked := e.applyConvoke(pc, cost)
		pay := paymentForCast(pc, convoked)
		if e.manaFeasibleDescriptor(pc.player, pay, convoked, costMods{}, 0, 0, pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}) {
			out = append(out, candidate)
			continue
		}
		if !cost.hasManaPayment() {
			continue
		}
		if targetDiscount {
			// A best-target discount can make this option affordable before
			// choosing targets while a weaker target is not. An arbitrary
			// untapped source is not proof that the 601.2g window can cover
			// the difference. Probe the window's concrete free productions,
			// one alternative per source, instead of offering a target whose
			// activation will abort at payment (CR 601.2h).
			av := e.manaAvailableFor(pc.player, pay)
			if e.castWindowReachable(pc.player, convoked, av.pool, pl.Snow, av.typed, pl.Life,
				e.paymentConv(pc.player, pay.id, pay.class == paymentActivated), windowUnits) {
				out = append(out, candidate)
			}
		} else if e.hasUntappedManaSource(pc.player) {
			out = append(out, candidate)
		}
	}
	return out
}

// manaToPay is the CR 601.2f total-cost composition for pc: resolvedMana
// ({X} folded and flexible-pip announcement recorded), then cost increases
// and reductions, then the CR 903.8 commander tax. Delve credit is the
// caller's concern (targetAsk/payCast subtract pc.delve from Generic).
func (e *Engine) manaToPay(pc *pendingCast) Cost {
	m := pc.mods.apply(pc.resolvedMana())
	if costAnnouncesPaidX(pc.cost) {
		// The announced sacrifice count re-prices the ReduceCost statics that
		// read the paid X (Dargo's {2}-less-per-sacrifice): the offer-time
		// pc.mods snapshot was bound to X=0.
		if scope, ok := e.pendingCastScope(pc); ok {
			m = e.costModifiersForTargetsX(pc.player, pc.card, scope, pc.targets, pc.x).apply(pc.resolvedMana())
		}
	}
	m.Generic += pc.taxGeneric
	return m
}

// costAnnouncesSacX reports whether the cost carries a Sac<X/Spec> part whose
// count the cast announces (the Dargo shape).
func costAnnouncesSacX(c Cost) bool {
	for _, part := range c.Sac {
		if part.Announced {
			return true
		}
	}
	return false
}

// costAnnouncesPaidX reports whether the cost carries ANY announced-count
// part whose count the cast announces as X: the Sac<X/Spec> shape
// (costAnnouncesSacX), the announced SubCounter<X/Kind> removal and the
// announced PayLife<X> or ExileFromGrave<X/Spec> payment. The offer-time
// costModifiers snapshot was bound to X=0, so a static reading the paid X
// must be re-priced once the
// announcement is known -- the same reason Dargo's Sac<X> needed it.
func costAnnouncesPaidX(c Cost) bool {
	if costAnnouncesSacX(c) {
		return true
	}
	if len(c.LifeX) > 0 {
		return true
	}
	for _, part := range c.SubCounter {
		if part.Announced {
			return true
		}
	}
	for _, part := range c.Exile {
		if part.Announced {
			return true
		}
	}
	// An X-form tapXType part binds the same X (the election announced it or
	// the tap settle paid the announced value), so a ReduceCost static
	// reading Count$xPaid is re-priced on it the same way.
	for _, part := range c.TapPermanent {
		if part.Dyn == "X" {
			return true
		}
	}
	// A named-announcement count (the March cycle's Exiled, Explosive
	// Singularity's Tapped) feeds its Relative$ ReduceCost the same way.
	return costHasNamedCount(c)
}

// manaToPayX is manaToPay with {X} folded to an explicit value.
// paymentMana applies announced Convoke/Harmonize contributions to the
// already-formed total. A stale answer can never make a requirement negative.
// faceWantsConvoked reports whether the cast's face could have a reader of
// the Convoked provenance: an SVar body or ability parameter naming the
// `Defined$ Convoked` selector (Lethal Scheme's DBConnive, Venerated
// Loxodon's and Zephyr Singer's TrigPutCounterAll) or a filter that names the
// `Convoked` referent of the sharesCardTypeWith/sharesCreatureTypeWith family
// (Everything Comes to Dust's ChangeType$ `...sharesCreatureTypeWith
// Convoked...`). The string scan is the faceWantsConverge shape; the
// shares-referent half goes through effects.SpecUsesConvokedReferent, the
// SAME classifier the matcher uses, so the provenance gate can never drift
// from who reads Convoked.
func faceWantsConvoked(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for _, v := range f.SVars {
		if strings.Contains(v, "Defined$ Convoked") || effects.SpecUsesConvokedReferent(v) || effects.SpecUsesConvokedAmount(v) {
			return true
		}
	}
	for _, a := range f.Abilities {
		if strings.EqualFold(strings.TrimSpace(a.Params["Defined"]), "Convoked") {
			return true
		}
		if abilityParamsUseConvoked(a.Params) {
			return true
		}
	}
	return false
}

// abilityParamsUseConvoked is the ability half of faceWantsConvoked: a
// whole-map VALUE scan -- it reads no specific Params key, only every value,
// the same shape the SVar loop above reads f.SVars with -- so it takes the
// map as a plain map[string]string parameter (the paramcensus scanner's
// helper-passed-map form, with no key indexed and therefore no key read the
// census would attribute). Keeping it a value scan is the point: the
// share-family referent can ride any spec-bearing parameter, and a key
// whitelist here would silently miss the next one -- exactly the silent gap
// (sharesCreatureTypeWith Convoked classifying wordUnknown) this ticket
// fixed.
func abilityParamsUseConvoked(params map[string]string) bool {
	for _, v := range params {
		if effects.SpecUsesConvokedReferent(v) || effects.SpecUsesConvokedAmount(v) {
			return true
		}
	}
	return false
}

// faceWantsConverge is the heads-safety gate for the pay-time converge
// CastInfo: it reports whether the face carries a Count$Converge SVar body
// or the printed Sunburst keyword. Without it a count>0-only gate would stamp
// a CastInfo onto EVERY multicolour cast and move the chain heads; with it,
// no game that casts no converge card changes an event (measured: no repo-deck
// card carries Count$Converge or Sunburst, so TestHeads stays put). Sunburst
// is the second consumer of this seam (CR 702.47): its whole meaning is the
// converge count put as counters on entry (rules/replacement.go's
// sunburstEntryMatch), and the count must be captured at pay time because the
// Animate-granted shape can deliver the keyword only after payment.
func faceWantsConverge(f *cards.Face) bool {
	if f == nil {
		return false
	}
	if f.HasKeyword("Sunburst") {
		return true
	}
	for _, v := range f.SVars {
		if body, ok := strings.CutPrefix(v, "Count$"); ok && strings.EqualFold(strings.TrimSpace(body), "Converge") {
			return true
		}
	}
	return false
}

// faceWantsCastSpend is the heads-safety gate for the pay-time cast-spend
// CastInfo (the converge gate's shape): it reports whether the face's SVar
// table reads the TOTAL mana actually spent to cast the spell -- a body
// naming the Count$CastTotalManaSpent head (Freestrider Commando's
// SVar:X:Count$CastTotalManaSpent feeding its etbCounter CheckSVar$ gate).
// A trigger on ANOTHER permanent that reads the cast spell's spend through
// the ref-property spelling (TriggeredCard$CastTotalManaSpent -- Aberrant
// Manawurm, Manaform Hellkite, Muse Seeker) is invisible here because the
// CAST face is an ordinary instant/sorcery; that path is gated by
// triggeredCastSpendReaderOut below instead.
func faceWantsCastSpend(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for _, body := range f.SVars {
		if strings.Contains(body, "Count$CastTotalManaSpent") {
			return true
		}
	}
	return false
}

// triggeredCastSpendReaderOut is the capture gate's second arm (the
// triggeredConvergeReaderOut shape): it reports whether any alive player's
// battlefield holds a permanent whose faces read the TRIGGER-relative spend
// spelling TriggeredCard$CastTotalManaSpent -- a trigger that reads ANOTHER
// spell's total spend, which faceWantsCastSpend cannot see because the cast
// face itself is an ordinary instant/sorcery. Only then does the pay-time
// CastInfo need stamping on a plain cast; a TriggerZones$ Battlefield
// SpellCast trigger can only exist for casts made while the reader is out, so
// this scan-at-pay-time gate stamps exactly when the value can be needed and
// no game without a reader out changes an event (heads stay put: no reader is
// in any repo deck). Pure read -- the boolean OR over the deterministic
// seat/zone walk cannot reach an event; replay re-runs payCast and derives the
// same scan.
func (e *Engine) triggeredCastSpendReaderOut() bool {
	g := e.G
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			if o := g.Obj(id); o != nil && e.objectReadsTriggeredCastSpend(o) {
				return true
			}
		}
	}
	return false
}

// objectReadsTriggeredCastSpend reports whether any face the ordinary trigger
// scan walks for o reads TriggeredCard$CastTotalManaSpent: the cast face, an
// unlocked Room's alternate face (roomTriggerFaces) and every merged
// under-card face (triggerFacesWithMerged). The shared face enumeration is
// what makes this the gate's structural twin of the trigger scan rather than
// a second, driftable list -- the next such face shape is covered without a
// new arm. Face.Mentions scans every string the face owns (SVars, keywords,
// ability params), so an inline parameter spelling is covered too, not just
// the SVar-table form the corpus uses today.
func (e *Engine) objectReadsTriggeredCastSpend(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	faces, n := roomTriggerFaces(o, f)
	walk := faces[:n]
	if len(o.MergedCards) > 0 {
		walk = triggerFacesWithMerged(o, walk)
	}
	for _, fc := range walk {
		// The per-face Mentions scan, memoised (face_scan_memo.go).
		if e.faceScanHas(fc.face, faceScanCastSpendReader) {
			return true
		}
	}
	return false
}

// faceWantsTimesKicked is the heads-safety gate for the pay-time multikick
// CastInfo on a PLAIN-Kicker cast mode (the converge gate's shape): it
// reports whether anything on the face reads the times-kicked count
// (Count$TimesKicked, op suffix included) -- an SVar value body, an ability
// parameter (DestAltSVar$), a trigger/replacement body, a static parameter
// or a keyword string. The 11 legacy plain-Kicker carriers read it from an
// SVar and get a real count stamped; The Five Doctors reads it inline in its
// ChangeZone's DestAltSVar$. A kicker card that never reads the count casts
// byte-identically to before. A multikicked-mode cast needs no gate -- its
// count>0 emission is the primitive itself.
//
// The walk is deliberately structural (cards.Face.Mentions scans every string
// the face owns) rather than an SVar-table-only scan: the earlier SVar-only
// form missed the inline parameter shape and would miss the next one (an
// ability's ChangeNum$, NumDmg$, or any future count-reading param),
// silently stamping no provenance for a script that reads it.
func faceWantsTimesKicked(f *cards.Face) bool {
	return f.Mentions("Count$TimesKicked")
}

// manaExpendReaderOut is the ManaExpend emission gate (the
// triggeredConvergeReaderOut pattern): true when the CASTING player's own
// battlefield holds a permanent whose live faces carry a Mode$ ManaExpend
// trigger. A ManaExpend trigger can only fire for its controller's own cast
// expenditure (every corpus line carries Player$ You), so scoping the scan to
// the caster's zone stamps exactly when the value can be needed and no game
// without a carrier out changes an event (heads stay put: no ManaExpend
// carrier is in any repo deck). Pure read -- the deterministic zone walk
// cannot reach an event; replay re-runs payCast and derives the same scan.
//
// The scan enumerates the SAME faces the ordinary trigger scan walks
// (roomTriggerFaces plus triggerFacesWithMerged), not just the printed top
// face: an unlocked Room's alternate face and a mutated pile's under-cards
// can each carry a ManaExpend trigger (CR 309.6, CR 702.140d), and the
// trigger scan would fire one if the wake-up event existed. Routing the gate
// through the shared helpers keeps the gate from silently under-stamping a
// shape the matcher supports -- the next such face shape is covered without
// a second list.
func (e *Engine) manaExpendReaderOut(player state.PlayerID) bool {
	g := e.G
	for _, id := range g.Zone(state.ZBattlefield, player) {
		o := g.Obj(id)
		if o == nil {
			continue
		}
		if objectHasManaExpendTrigger(o) {
			return true
		}
	}
	return false
}

// objectHasManaExpendTrigger reports whether any face the trigger scan walks
// for o carries a Mode$ ManaExpend trigger: the cast face, an unlocked Room's
// alternate face (roomTriggerFaces) and every merged under-card face
// (triggerFacesWithMerged). The shared face enumeration is what makes this
// the gate's structural twin of the scan rather than a second, driftable
// list.
func objectHasManaExpendTrigger(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	faces, n := roomTriggerFaces(o, f)
	walk := faces[:n]
	if len(o.MergedCards) > 0 {
		walk = triggerFacesWithMerged(o, walk)
	}
	for _, fc := range walk {
		if fc.face == nil {
			continue
		}
		for _, t := range fc.face.Triggers {
			if t.Mode == "ManaExpend" {
				return true
			}
		}
	}
	return false
}

// manaExpendAdd folds a paid cast's mana expenditure into the per-turn
// ManaExpend tally, resetting it first when the turn has moved on. Called
// UNCONDITIONALLY from payCast (before the gated wake-up emission), so the
// tally counts casts made while no carrier was out -- the pre-entry base the
// crossing test needs. Deterministic: e.G.Turn advances only through
// TurnChange, and replay's payCast re-execution folds the same calls in the
// same order.
func (e *Engine) manaExpendAdd(player state.PlayerID, spend int32) {
	if spend <= 0 {
		return
	}
	if e.manaExpendedTurn != e.G.Turn {
		e.manaExpendedTurn = e.G.Turn
		for i := range e.manaExpended {
			e.manaExpended[i] = 0
		}
	}
	if int(player) < len(e.manaExpended) {
		e.manaExpended[player] += spend
	}
}

// manaExpendTotal is the player's cumulative mana spent casting spells this
// turn -- the current-turn tally, or zero when the tally belongs to an
// earlier turn (no cast has stamped the new turn yet). manaExpendMatches
// reads it for the crossing test.
func (e *Engine) manaExpendTotal(player state.PlayerID) int32 {
	if e.manaExpendedTurn != e.G.Turn || int(player) >= len(e.manaExpended) {
		return 0
	}
	return e.manaExpended[player]
}

// triggeredConvergeReaderOut is the capture gate's second arm: it reports
// whether any alive player's battlefield holds a permanent whose face SVars
// name TriggeredCard$Converge -- a trigger that reads ANOTHER spell's cast
// colours (Magmablood Archaic's "for each color of mana spent to cast that
// spell"), which the Count$Converge face gate cannot see because the cast
// face itself is an ordinary non-converge instant/sorcery. Only then does the
// pay-time CastInfo need stamping on a plain cast; a TriggerZones$ Battlefield
// SpellCast trigger can only exist for casts made while the reader is out, so
// this scan-at-pay-time gate stamps exactly when the value can be needed and
// no game without a reader out changes an event (heads stay put: neither
// Archaic is in any repo deck). Pure read -- the boolean OR over the
// deterministic seat/zone walk cannot reach an event; replay re-runs payCast
// and derives the same scan.
func (e *Engine) triggeredConvergeReaderOut() bool {
	g := e.G
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			o := g.Obj(id)
			if o == nil {
				continue
			}
			// faceConvergeSVarReader per face, memoised (face_scan_memo.go).
			if e.faceScanHas(o.Face(), faceScanConvergeReader) {
				return true
			}
		}
	}
	return false
}

// sunburstGrantOut is the converge capture gate's third arm (the
// triggeredConvergeReaderOut shape): it reports whether any alive player's
// battlefield holds a permanent whose face BODY grants sunburst to a spell --
// Solar Array's and Lux Artillery's `DB$ Animate | Keywords$ Sunburst`
// (task kw:Sunburst). The Animate grant lands on the spell AFTER payment (a
// SpellCast trigger resolving while the spell is on the stack), so the
// printed-keyword arm of faceWantsConverge cannot see it at pay time; this
// board scan arms the capture so the entering permanent reads its colours.
// Pure read over the deterministic seat/zone walk, so replay re-runs payCast
// and derives the same scan.
func (e *Engine) sunburstGrantOut() bool {
	g := e.G
	for _, p := range g.AliveFrom(0) {
		for _, id := range g.Zone(state.ZBattlefield, p) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			// Mentions("Sunburst"), memoised (face_scan_memo.go).
			if e.faceScanHas(o.Face(), faceScanMentionsSunburst) {
				return true
			}
		}
	}
	return false
}

// convergeColours is CR 107.4f-family's converge count: the number of
// DISTINCT colours among W,U,B,R,G actually spent to cast the spell.
// Colourless/generic ({C}, generic pips) is not a colour and does not count;
// snow mana spent as a colour lives in the colour buckets here, so the plain
// per-colour delta is already right; mana conversion and the may-play
// ignore-colour rider changed what was actually paid, which is exactly what
// converge asks about.
func convergeColours(spent state.Mana) int32 {
	n := int32(0)
	for i := state.MW; i <= state.MG; i++ {
		if spent[i] > 0 {
			n++
		}
	}
	return n
}

// manaSpentTotal is the total mana a cast's payment actually spent: the
// spent delta's pips summed over every slot (coloured and colourless).
// Contribute the FULL delta -- a generic pip spent from a coloured unit is
// one mana spent -- so the sum is CR 601.2h's "mana spent to cast it".
func manaSpentTotal(spent state.Mana) int32 {
	var n int32
	for i := range spent {
		n += spent[i]
	}
	return n
}

func (e *Engine) paymentMana(pc *pendingCast) Cost {
	return e.applyConvoke(pc, e.manaToPay(pc))
}

// paymentManaX applies the same announced creature contributions after X is
// folded into the total. xAsk uses it so an X value funded by Convoke or
// Harmonize is actually offered, not rejected before the payment is known.
func (e *Engine) paymentManaX(pc *pendingCast, x int32) Cost {
	return e.paymentManaXUsing(pc, x, e.manaToPayXMods(pc, x))
}

// paymentManaXUsing is paymentManaX with an explicit modifier composition
// (xAsk's target-potential retry), so the announced Convoke/Harmonize
// contributions are folded onto the alternatively-priced total exactly as
// they are onto the ordinary one.
func (e *Engine) paymentManaXUsing(pc *pendingCast, x int32, mods costMods) Cost {
	return e.applyConvoke(pc, e.manaToPayXUsing(pc, x, mods))
}

func convokeManaSpent(pays []convokePayment) int32 {
	var n int32
	for _, pay := range pays {
		if pay.countsMana {
			n++
		}
	}
	return n
}

func (e *Engine) applyConvoke(pc *pendingCast, m Cost) Cost {
	for _, pay := range pc.convoke {
		if pay.color != 0 {
			i := state.ManaIndex(pay.color)
			if m.Colored[i] > 0 {
				m.Colored[i]--
			}
			continue
		}
		if pay.power > 0 {
			m.Generic -= pay.power
		} else {
			m.Generic--
		}
		if m.Generic < 0 {
			m.Generic = 0
		}
	}
	return m
}

// convokeCountCredit reduces composed cost m by the largest payment the
// caster's permanents can actually make through Convoke (CR 702.51),
// Harmonize (CR 702.46) or Improvise (CR 702.66) right now. It exists so a
// repeatable-cost count walk posed BEFORE the contributions are announced
// (replicateAsk and its multikicker/squad siblings) can see the mana a
// later Convoke announcement will free: without it the walk priced the cost
// against the pool alone and offered fewer payments than the board could
// pay. The assignment built here is the one convokeAsk then offers: a
// maximum matching covers as many coloured pips as distinct
// colour-eligible creatures allow, and every remaining eligible permanent
// takes one generic (a Harmonize permanent its power, per CR 702.46a). Both
// convokeAsk and this helper read the SAME composed total, so the payment
// the announcement is validated against (convokeAbsorbs's m) is the one
// priced here. A cast carrying none of the three keywords -- every
// pre-Convoke-replicate game -- returns m unchanged, so no game lacking one
// moves.
func (e *Engine) convokeCountCredit(pc *pendingCast, m Cost) Cost {
	isConvoke := e.hasCastConvoke(pc.card)
	isHarmonize := pc.mode == "harmonize"
	isImprovise := e.hasCastImprovise(pc.card)
	if !isConvoke && !isHarmonize && !isImprovise {
		return m
	}
	// candidate is one untapped permanent the announcement could tap, with
	// the payments it may make. A Convoke creature taps for a colour pip it
	// has or one generic; a Harmonize creature taps for its power in generic;
	// an Improvise artifact taps for one generic.
	type candidate struct {
		colors  string
		power   int32
		convoke bool
		generic bool // may pay one generic (Convoke creature or Improvise artifact)
	}
	var cands []candidate
	for _, id := range e.G.Zone(state.ZBattlefield, pc.player) {
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil || o.BestowedAttached() ||
			o.ReconfiguredAttached() || e.convokeCommitted(pc, id) {
			continue
		}
		c := candidate{}
		if o.EffectiveIsCreature() {
			if isConvoke {
				c.colors = e.objColors(o)
			}
			if isHarmonize {
				if p := e.Derived(id).Power; p > 0 {
					c.power = p
				}
			}
		}
		if isConvoke && o.EffectiveIsCreature() {
			c.convoke = true
			c.generic = true
		}
		if isImprovise && o.EffectiveIsArtifact() {
			c.generic = true
		}
		if c.convoke || c.power > 0 || c.generic {
			cands = append(cands, c)
		}
	}
	if len(cands) == 0 {
		return m
	}
	// Maximum bipartite matching of coloured pips to colour-eligible Convoke
	// creatures. The graph is tiny (at most five pip slots and the cast's
	// untapped creatures), so the classic augmenting-path walk is exact; any
	// maximal assignment here is a real announcement, and covering a pip frees
	// exactly one generic for the pool, so maximizing coloured coverage first
	// never costs generic units that could have been covered anyway.
	assign := make([]int, len(cands)) // cands index -> coloured slot, -1 unused
	for i := range assign {
		assign[i] = -1
	}
	var matchedCand func(slot int, seen []bool) bool
	matchedCand = func(slot int, seen []bool) bool {
		for i := range cands {
			if seen[i] || !cands[i].convoke || !strings.Contains(cands[i].colors, manaLetters[slot]) {
				continue
			}
			seen[i] = true
			if assign[i] == -1 || matchedCand(assign[i], seen) {
				assign[i] = slot
				return true
			}
		}
		return false
	}
	coveredPips := [6]int32{}
	// A Convoke creature taps for one mana of a colour it is (CR 702.51a),
	// so only the five coloured slots are matchable; {C} is never a
	// creature colour and the pool pays it.
	for slot := 0; slot < 5; slot++ {
		for n := int32(0); n < m.Colored[slot]; n++ {
			seen := make([]bool, len(cands))
			if matchedCand(slot, seen) {
				coveredPips[slot]++
			}
		}
	}
	for slot := range m.Colored {
		m.Colored[slot] -= coveredPips[slot]
	}
	// Every permanent not already paying a coloured pip takes one generic: a
	// Harmonize creature its power, a Convoke creature or Improvise artifact
	// one. Stop once the generic requirement is gone (each contribution must
	// reduce something, or the announcement is rejected).
	for i, c := range cands {
		if assign[i] >= 0 || m.Generic <= 0 {
			continue
		}
		reduce := int32(1)
		if c.power > 0 {
			reduce = c.power
		}
		if reduce > m.Generic {
			reduce = m.Generic
		}
		m.Generic -= reduce
	}
	return m
}

// convokeAbsorbs reports whether every announced Convoke/Harmonize payment
// actually reduces the outstanding mana requirement m when applied in
// announcement order. A colour contribution whose pip is already covered,
// or a generic/power contribution against a generic total already at {0},
// pays nothing -- CR 601.2b/702.51a let a creature be tapped only for a
// reduction the total cost still needs -- and payCast taps every announced
// creature, so an announcement containing such a no-op is an illegal
// over-payment that must be rejected, not silently tapped.
//
// With an unfixed {X} the generic requirement is not yet known when the
// announcement is answered (CR 601.2b announces Convoke before X), so a
// generic/power contribution against {0} generic is tentatively allowed
// there (xOpen) and xAsk prices the announcement against every candidate X
// with xOpen false, m already X-folded.
func (e *Engine) convokeAbsorbs(pc *pendingCast, m Cost, pays []convokePayment, xOpen bool) bool {
	for _, pay := range pays {
		if pay.color != 0 {
			i := state.ManaIndex(pay.color)
			if m.Colored[i] <= 0 {
				return false
			}
			m.Colored[i]--
			continue
		}
		if m.Generic <= 0 {
			if xOpen {
				continue
			}
			return false
		}
		reduce := pay.power
		if reduce <= 0 {
			reduce = 1
		}
		m.Generic -= reduce
		if m.Generic < 0 {
			m.Generic = 0
		}
	}
	return true
}

// validateSearch is the Submit-time gate for a hidden-library search
// decision (ResumeKind "search") whose SA carries ShareLandType$ True
// (Myriad Landscape's "up to two basic land cards that share a land type").
// The decision's static Validate sees only the offered option list -- no
// option carries the shared-type constraint -- so an answer naming two lands
// of disjoint types would pass it; this gate rejects such an answer before
// the intent is recorded and the pending decision is consumed, exactly like
// validateAttackers/validateCastContributions. Single-card answers are
// trivially legal (one card always shares with itself). Any other search --
// no ResumeSA, no ShareLandType$ -- is passed through untouched. The effect
// side (applyLibrarySearch's trim) keeps the same constraint for a host
// that bypassed the wire, through the one shared classifier
// effects.SharedLandTypes.
func (e *Engine) validateSearch(d *decision.Decision, in decision.Intent) error {
	if d.ResumeKind != "search" || d.ResumeSA == nil ||
		!strings.EqualFold(strings.TrimSpace(d.ResumeSA.Params["ShareLandType"]), "True") {
		return nil
	}
	ids := make([]state.ObjID, 0, len(in.Choices))
	for _, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			continue // Validate's own out-of-range error already fired
		}
		if o := d.Options[c]; o.Obj != 0 {
			ids = append(ids, o.Obj)
		}
	}
	if !effects.SharedLandTypes(e.G, ids) {
		return fmt.Errorf("chosen cards do not share a land type")
	}
	return nil
}

// validateCastContributions is the Submit-time gate for the cast flow's
// Convoke/Harmonize announcement decision (convokeAsk). The decision's
// static Validate sees only the offered option list -- two white creatures
// each carry a convoke_W option for a {W} spell, in distinct groups -- so
// an over-selection passes it; this gate rejects any answer containing a
// contribution that reduces nothing (convokeAbsorbs), before the intent is
// recorded and the pending decision is consumed, exactly like
// validateAttackers. The client then resubmits a legal subset. Any other
// KChoose decision, and an out-of-range choice (Validate's own error), is
// passed through untouched.
func (e *Engine) validateCastContributions(d *decision.Decision, in decision.Intent) error {
	pc := e.cast
	if pc == nil || len(in.Choices) == 0 {
		return nil
	}
	var pays []convokePayment
	for _, c := range in.Choices {
		if c < 0 || c >= len(d.Options) {
			return nil
		}
		o := d.Options[c]
		switch {
		case o.Kind == "harmonize":
			pays = append(pays, convokePayment{id: o.Obj, power: int32(o.Amount)})
		case o.Kind == "improvise_generic":
			pays = append(pays, convokePayment{id: o.Obj})
		case o.Kind == "waterbend_generic":
			pays = append(pays, convokePayment{id: o.Obj, waterbend: true})
		case strings.HasPrefix(o.Kind, "convoke_"):
			color := byte(0)
			if o.Kind != "convoke_generic" {
				color = o.Kind[len("convoke_")]
			}
			pays = append(pays, convokePayment{id: o.Obj, color: color, countsMana: true})
		default:
			return nil // a different cast-flow ask, not the convoke announcement
		}
	}
	all := append(append([]convokePayment(nil), pc.convoke...), pays...)
	if !e.convokeAbsorbs(pc, e.manaToPay(pc), all, pc.cost.X > 0) {
		return fmt.Errorf("announcement reduces nothing: the outstanding cost cannot absorb every chosen contribution")
	}
	// Waterbend taps pay only the waterbend amount. A Waterbend<X> cap is
	// not known until X is announced; xAsk offers only the X values whose
	// cap covers the announced taps.
	if pc.mods.raiseX == 0 && waterbendTaps(all) > pc.mods.waterbend {
		return fmt.Errorf("more permanents tapped than the waterbend cost allows")
	}
	return nil
}

// convokeAsk announces every creature used for Convoke or Harmonize. It is
// deliberately before manaWindowAsk: tapping is part of paying, so a chosen
// creature cannot first be used as a mana source.
func (e *Engine) convokeAsk() bool {
	pc := e.cast
	if pc == nil || pc.convokeDone || pc.isAbility() {
		return false
	}
	pc.convokeDone = true
	isConvoke := e.hasCastConvoke(pc.card)
	isHarmonize := pc.mode == "harmonize"
	isImprovise := e.hasCastImprovise(pc.card)
	// A RaiseCost Waterbend<N>/<X> additional cost (Water Whip, Crashing
	// Wave): each untapped artifact or creature tapped while paying it pays
	// for {1} of the waterbend amount.
	isWaterbend := pc.mods.waterbend > 0 || pc.mods.waterbendX
	if !isConvoke && !isHarmonize && !isImprovise && !isWaterbend {
		return false
	}
	mana := e.manaToPay(pc)
	// Before X is announced, its generic requirement is not folded into
	// mana. It nevertheless makes every creature a possible generic payment;
	// the subsequent xAsk prices the selected contributions against the real
	// X total.
	hasX := pc.cost.X > 0
	if !mana.hasManaPayment() && !hasX {
		return false
	}
	name := e.G.Obj(pc.card).Face().Name
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 0, Source: pc.card}
	sawCreature, sawArtifact := false, false
	for _, id := range e.G.Zone(state.ZBattlefield, pc.player) {
		o := e.G.Obj(id)
		if o == nil || o.Tapped || o.Face() == nil || o.BestowedAttached() || o.ReconfiguredAttached() || e.convokeCommitted(pc, id) {
			continue
		}
		group := fmt.Sprintf("payment:%d", id)
		if isHarmonize && o.EffectiveIsCreature() && (mana.Generic > 0 || hasX) {
			// The reduction offered is the creature's ACTUAL power (CR
			// 702.46a), the same number harmonizePayment credits: a printed
			// 1/1 currently boosted to 4 funds four generic, and a printed
			// 4/4 reduced to 1 funds only one.
			if p := e.Derived(id).Power; p > 0 {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "harmonize", Obj: id,
					Group: group, Amount: int(p), Label: "Tap " + o.Face().Name + " (reduce by " + strconv.Itoa(int(p)) + ")"})
				sawCreature = true
			}
		}
		if isConvoke && o.EffectiveIsCreature() {
			for _, color := range []byte{'W', 'U', 'B', 'R', 'G'} {
				if mana.Colored[state.ManaIndex(color)] > 0 && strings.Contains(e.objColors(o), string(color)) {
					d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "convoke_" + string(color), Obj: id,
						Group: group, Label: "Tap " + o.Face().Name + " for " + string(color)})
					sawCreature = true
				}
			}
			if mana.Generic > 0 || hasX {
				d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "convoke_generic", Obj: id,
					Group: group, Label: "Tap " + o.Face().Name + " for 1"})
				sawCreature = true
			}
		}
		// CR 702.66a: Improvise's artifacts -- artifact creatures included,
		// the card is an artifact independently of being a creature -- each
		// pay one generic. The shared payment group makes the object's
		// Convoke and Improvise options mutually exclusive, so one artifact
		// can never be committed to both payments.
		genericOffered := false
		if isImprovise && o.EffectiveIsArtifact() && (mana.Generic > 0 || hasX) {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "improvise_generic", Obj: id,
				Group: group, Label: "Tap " + o.Face().Name + " for 1"})
			sawArtifact = true
			genericOffered = true
		}
		if isConvoke && o.EffectiveIsCreature() && (mana.Generic > 0 || hasX) {
			genericOffered = true
		}
		// Waterbend: an artifact or creature not already offered a generic
		// payment by the spell's own Convoke/Improvise may tap for {1} of
		// the waterbend amount (the same payment group keeps one object to
		// one contribution).
		if isWaterbend && !genericOffered && (o.EffectiveIsArtifact() || o.EffectiveIsCreature()) && (mana.Generic > 0 || hasX) {
			d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "waterbend_generic", Obj: id,
				Group: group, Label: "Tap " + o.Face().Name + " to waterbend for 1"})
			if o.EffectiveIsCreature() {
				sawCreature = true
			} else {
				sawArtifact = true
			}
		}
	}
	if len(d.Options) == 0 {
		return false
	}
	// The prompt names what is actually offered: a mixed Convoke/Improvise
	// spell offers both creatures and artifacts, an Improvise-only one only
	// artifacts, and the Convoke/Harmonize shapes only creatures.
	switch {
	case sawCreature && sawArtifact:
		d.Prompt = "Choose permanents to help pay for " + name
	case sawArtifact:
		d.Prompt = "Choose artifacts to help pay for " + name
	default:
		d.Prompt = "Choose creatures to help pay for " + name
	}
	// The announcement cannot tap more creatures than the cost can absorb:
	// each chosen contribution reduces exactly one outstanding slot (a
	// colour pip or one generic), so Max is the outstanding slot count.
	// With an unfixed {X} the generic requirement is not yet known (CR
	// 601.2b announces Convoke before X), so the bound is left open and
	// xAsk prices the announcement against every candidate X instead;
	// convokeAbsorbs's answer gate plus that pricing close the rest.
	d.Max = len(d.Options)
	if !hasX {
		if slots := int(mana.Colored.Total() + mana.Generic); slots < d.Max {
			d.Max = slots
		}
	}
	if isWaterbend && !isConvoke && !isHarmonize && !isImprovise && pc.mods.raiseX == 0 && int(pc.mods.waterbend) < d.Max {
		// Only waterbend taps are offered: at most the waterbend amount.
		d.Max = int(pc.mods.waterbend)
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

func (e *Engine) manaToPayX(pc *pendingCast, x int32) Cost {
	return e.manaToPayXUsing(pc, x, e.manaToPayXMods(pc, x))
}

// manaToPayXMods is the modifier composition manaToPayX applies to a
// candidate X: pc.mods, except that a cost whose announced count feeds a
// ReduceCost static reading the paid X (Dargo's {2}-less-per-sacrifice) is
// re-priced with the X-bound targets, because the offer-time pc.mods snapshot
// was bound to X=0. xAsk reads it so the convoke-absorption check and the
// potential-target retry compose the SAME modifiers the payable check used.
func (e *Engine) manaToPayXMods(pc *pendingCast, x int32) costMods {
	if costAnnouncesPaidX(pc.cost) {
		if scope, ok := e.pendingCastScope(pc); ok {
			return e.costModifiersForTargetsX(pc.player, pc.card, scope, pc.targets, x)
		}
	}
	return pc.mods
}

// manaToPayXUsing is manaToPayX with an explicit modifier composition.
// xAsk's target-potential retry prices a candidate X under the reduction a
// potential target would give (mods is then the potential composition); every
// other caller passes manaToPayXMods. The commander tax is folded in the same
// place as manaToPayX, so an alternative composition charges the identical
// total.
func (e *Engine) manaToPayXUsing(pc *pendingCast, x int32, mods costMods) Cost {
	m := mods.apply(pc.resolvedManaX(x))
	m.Generic += pc.taxGeneric
	return m
}

// hasManaPayment reports whether a cost's mana component is non-empty, per CR
// 601.2g's "if the total cost includes a mana payment".
func (c Cost) hasManaPayment() bool {
	return c.Colored.Total() > 0 || c.Generic > 0
}

// dropAnnouncePrefix removes the first n announcement pips (in announcePip
// order: two-colour hybrids, then monocolour hybrids, then Phyrexian, then
// hybrid-Phyrexian) from the cost, leaving the rest as the cost's live
// choices. It is how feasibleAny's per-level walk consumes one announcement
// pip at a time after folding that pip's resolved face into the cost, so a
// leaf never sees a pip slot twice.
func (c Cost) dropAnnouncePrefix(n int) Cost {
	drop := n
	if drop < len(c.Hybrid) {
		c.Hybrid = c.Hybrid[drop:]
		drop = 0
	} else {
		drop -= len(c.Hybrid)
		c.Hybrid = nil
	}
	if drop > 0 {
		if drop < len(c.Twobrid) {
			c.Twobrid = c.Twobrid[drop:]
			drop = 0
		} else {
			drop -= len(c.Twobrid)
			c.Twobrid = nil
		}
	}
	if drop > 0 {
		if drop < len(c.Phyrexian) {
			c.Phyrexian = c.Phyrexian[drop:]
			drop = 0
		} else {
			drop -= len(c.Phyrexian)
			c.Phyrexian = nil
		}
	}
	if drop > 0 {
		if drop < len(c.HybridPhyrexian) {
			c.HybridPhyrexian = c.HybridPhyrexian[drop:]
		} else {
			c.HybridPhyrexian = nil
		}
	}
	return c
}

// announceCost is gone: its per-pip composition (mods applied to a cost that
// still carried the unannounced pips, so a Color$ reduction saw no W pip it
// could legally take and a floor priced an unresolved pip at its generic
// face) answered a different feasibility question than the offer gate and
// the charge, and could reject the only legal announcement (a reduction that
// is legally assigned to a LATER pip). announceFeasible below folds the
// already-announced pips into the cost as their final resolved faces and
// hands the remainder to the one shared primitive, costMods.feasibleAny.
// announceFeasible reports whether offering alternative alt for the pip the
// flow is announcing still leaves the whole cost payable: the pips already
// committed (payColor/payLife/payGeneric on pc) and the candidate alt are
// folded into the cost as their FINAL resolved faces, and the still-
// unannounced pips are enumerated by the shared primitive with the CR 601.2f
// modifiers composed onto each fully-resolved assignment, the CR 903.8
// commander tax added after and (for a spell) the Delve credit taken off the
// generic — exactly the composition manaToPay/payCast will charge once every
// pip is settled. It is the CR 601.2b legality question: an announced payment
// is offered only if SOME legal assignment of the remaining pips makes the
// total cost payable, so a player is never offered a payment that can only
// strand the cast in an unpayable remainder (and an abort at payCast).
func (e *Engine) announceFeasible(pc *pendingCast, alt pipAlt, pool, snow state.Mana, life int32) bool {
	c := pc.cost.WithX(pc.x)
	for i := range c.Colored {
		c.Colored[i] += pc.payColor[i]
	}
	c.Generic = addClampedGeneric(c.Generic, int64(pc.payGeneric))
	c.Life = addClampedGeneric(c.Life, int64(pc.payLife))
	switch {
	case alt.color != 0:
		c.Colored[state.ManaIndex(alt.color)]++
	case alt.generic > 0:
		c.Generic = addClampedGeneric(c.Generic, int64(alt.generic))
	case alt.life > 0:
		c.Life = addClampedGeneric(c.Life, int64(alt.life))
	}
	delve := int32(0)
	if !pc.isAbility() {
		delve = int32(len(pc.delve))
	}
	// The pips 0..payIdx have been announced (their faces are folded in
	// above), so their slots leave the cost; the pips after payIdx stay live
	// for the shared primitive to enumerate.
	c = c.dropAnnouncePrefix(pc.payIdx + 1)
	payment := paymentForCast(pc, c)
	rider := pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}
	if e.manaFeasibleDescriptor(pc.player, payment, c, pc.mods, pc.taxGeneric, delve, rider) {
		return true
	}
	// CR 601.2b chooses a Phyrexian or hybrid face BEFORE the 601.2g mana
	// ability window.  Pricing a face only against floating mana therefore
	// withholds a coloured face whenever its source is still untapped: a
	// Gitaxian Probe with an Island offered only "Pay 2 life".  Probe the same
	// concrete source alternatives that manaWindowAsk can subsequently offer,
	// after composing exactly the modifiers, commander tax, and Delve credit
	// that payCast will charge.  This is an offer-side read only; the chosen
	// face still reaches the ordinary mana window and is paid there.
	charged := pc.mods.apply(c)
	charged.Generic = addClampedGeneric(charged.Generic, int64(pc.taxGeneric))
	charged.Generic -= delve
	if charged.Generic < 0 {
		charged.Generic = 0
	}
	if !charged.hasManaPayment() {
		return false
	}
	av := e.manaAvailableFor(pc.player, payment)
	return e.manaReachable(pc.player, charged, av.pool, e.G.Players[pc.player].Snow,
		av.typed, e.G.Players[pc.player].Life, rider,
		e.paymentConv(pc.player, payment.id, payment.class == paymentActivated), e.castWindowUnits(pc))
}

// manaAsk offers the player's payment choice for the next unsettled hybrid or
// Phyrexian pip of the cost (CR 601.2b), one decision per pip. EVERY face is
// gated on the ONE shared feasibility primitive (announceFeasible →
// costMods.feasibleAny): with the pips already announced and the candidate
// folded in as its final resolved face, some legal assignment of the
// remaining pips must make the composed total (modifiers on final faces,
// commander tax, Delve credit) payable. Any composed modifier can make a
// locally affordable face strand the final payment — a generic Thalia raise
// (Dismember's black faces), a Color$ reduction that is only legally
// assignable to a later pip ({W/U}{W/U} under Color$ W), a SetCost floor on
// an unresolved twobrid — and only the whole-cost search sees that, so a
// player is never offered a payment a complete assignment cannot pay. The
// valid options keep their announcePip order, so the deterministic bot
// fallback (index 0) always picks a legal payment and a no-answer host never
// wedges. The offer gate (offerCastable) proved at least one full assignment
// feasible over the same primitive, so the decision is never empty for a
// state the gate measured; an empty menu is still possible after the offer
// (the {X} choice or a repricing changed the composition) and the defensive
// arm below preserves the flow's behaviour for it. It returns true once it
// has asked (and therefore suspended); payCast applies the accumulated
// payColor / payLife / payGeneric when every pip is settled.
func (e *Engine) manaAsk() bool {
	pc := e.cast
	if pc == nil || pc.payIdx >= pc.cost.annPipCount() {
		return false
	}
	alts := pc.cost.announcePip(pc.payIdx)
	// announceFeasible receives the full pool and life total because the
	// commitments already made (and this candidate face) are folded into the
	// cost it evaluates; nothing has been paid yet. Do not pre-filter a colour
	// face merely because the current pool lacks that colour: a Color$
	// reduction can make the announced face free (for example {W/U} under
	// Color$ W).
	pool, snow := e.G.Players[pc.player].Pool, e.G.Players[pc.player].Snow
	fullLife := e.G.Players[pc.player].Life
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Choose how to pay a mana symbol of " + e.G.Obj(pc.card).Face().Name,
		Source: pc.card}
	addPip := func(alt pipAlt) {
		switch {
		case alt.color != 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_" + string(alt.color), Label: "Pay " + string(alt.color), Amount: 1})
		case alt.generic > 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_generic", Label: fmt.Sprintf("Pay %d generic", alt.generic), Amount: int(alt.generic)})
		case alt.life > 0:
			d.Options = append(d.Options, decision.Option{Index: len(d.Options),
				Kind: "pay_life", Label: "Pay 2 life", Amount: 2})
		}
	}
	seen := map[byte]bool{}
	seenGeneric := false
	for _, alt := range alts {
		switch {
		case alt.color != 0:
			if seen[alt.color] {
				continue
			}
			seen[alt.color] = true
			if e.announceFeasible(pc, alt, pool, snow, fullLife) {
				addPip(alt)
			}
		case alt.generic > 0:
			if seenGeneric {
				continue
			}
			seenGeneric = true
			if e.announceFeasible(pc, alt, pool, snow, fullLife) {
				addPip(alt)
			}
		case alt.life > 0:
			if e.announceFeasible(pc, alt, pool, snow, fullLife) {
				addPip(alt)
			}
		}
	}
	if len(d.Options) == 0 {
		// Defensive: the offer gate proved at least one pip alternative
		// completes the cost, so a feasible option is always present for a
		// gated cast measured at the gate; this arm only guards the state
		// having shifted since (the {X} choice, a repricing, a shorter pool).
		// Rather than offer an infeasible payment, offer the first alternative
		// (index 0, the deterministic best) so the decision is never empty.
		addPip(alts[0])
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// manaConvertAsk poses the Optional$ ManaConvert election once for this
// proposal. A mandatory conversion remains automatic; an optional conversion
// is offered even when the ordinary pool already pays, because declining is a
// meaningful player choice and the grant may matter to a later repricing.
func (e *Engine) manaConvertAsk() bool {
	pc := e.cast
	if pc == nil || pc.manaConvertDone {
		return false
	}
	_, optional := e.manaConversionParts(pc.player, pc.card, pc.isAbility())
	if optional.empty() {
		pc.manaConvertDone = true
		return false
	}
	pc.manaConvertDone = true
	e.choosing = chooseCast
	e.ask(&decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Use optional mana conversion?", Source: pc.card,
		Options: []decision.Option{
			{Index: 0, Kind: "manaconvert", Label: "Use mana conversion", Obj: pc.card, Player: pc.player},
			{Index: 1, Kind: "manaconvert", Label: "Don't use mana conversion", Obj: pc.card, Player: pc.player},
		}})
	return true
}

// castAnswer records a chooseCast answer into the flow, keyed off which
// stage asked it (every option in one decision shares a Kind). A mana-window
// decision (CR 601.2g) is the exception: it offers both "activate" and
// "done" options, so the CHOSEN option's kind, not the stage's, identifies
// the answer.
func (e *Engine) castAnswer(d *decision.Decision, chosen []decision.Option) {
	pc := e.cast
	if pc == nil || len(d.Options) == 0 {
		return
	}
	// The decision's CHOSEN option identifies the answer, never the first
	// offered option. Most chooseCast decisions are single-kind (an {X} value,
	// a Delve exile, a sacrifice) so Options[0].Kind would coincidentally be
	// right, but a hybrid/Phyrexian/twobrid pip decision offers MIXED kinds
	// (pay_W, pay_generic, pay_life) and the mana window offers activate/done,
	// so dispatching on Options[0].Kind would mis-route a non-first choice
	// (picking a twobrid generic face from a decision whose first option is
	// pay_W fell into the pay_W branch and minted a colourless pip).
	kind := d.Options[0].Kind
	if len(chosen) > 0 {
		kind = chosen[0].Kind
	}
	switch kind {
	case "manaconvert":
		// Option 0 is the affirmative election. The decision is deliberately
		// positional rather than label-based so a translated label cannot alter
		// the payment semantics.
		pc.manaConvertUse = len(chosen) > 0 && chosen[0].Index == 0
	case "named_announce":
		if len(chosen) > 0 {
			pc.namedN = int32(chosen[0].Amount)
		}
	case "x":
		if len(chosen) > 0 {
			// The value rides on Option.Amount, not Option.Index: xAsk is the
			// first stage and appends 0..max into an empty option list, so
			// Index happens to equal the value today, but a later task that
			// prepends an option (a "cancel", Task 10's ability variants)
			// would silently corrupt an Index-derived value.
			pc.x = int32(chosen[0].Amount)
		}
	case "replicate":
		// CR 702.55a: the answered payment count folds that many replicate
		// payments into cost, so every later stage (Convoke, X, the payment
		// window, payCast) charges the composed total. The count rides on
		// Option.Amount, not Index, for the same reason xAsk's value does.
		if len(chosen) > 0 && pc.replicateSet {
			n := int32(chosen[0].Amount)
			rc := ParseCost(pc.replicateParam)
			for i := int32(0); i < n; i++ {
				pc.cost = pc.cost.Plus(rc)
			}
			pc.replicateTimes = n
		}
	case "multikick":
		// CR 702.43: the answered payment count folds that many multikicker
		// payments into cost -- the replicate arm's exact shape.
		if len(chosen) > 0 && pc.multikickSet {
			n := int32(chosen[0].Amount)
			mk := ParseCost(pc.multikickParam)
			for i := int32(0); i < n; i++ {
				pc.cost = pc.cost.Plus(mk)
			}
			pc.multikickTimes = n
		}
	case "squad":
		// CR 702.66: the answered payment count folds that many squad
		// payments into cost -- the replicate/multikicker arm's exact shape.
		if len(chosen) > 0 && pc.squadSet {
			n := int32(chosen[0].Amount)
			sc := ParseCost(pc.squadParam)
			for i := int32(0); i < n; i++ {
				pc.cost = pc.cost.Plus(sc)
			}
			pc.squadTimes = n
		}
	case "mutate_place":
		// CR 702.140b: the answered over/under placement. Option.Amount is 1
		// for "on top", 0 for "under", so the answer is read positionally.
		if len(chosen) > 0 {
			pc.mutateTop = chosen[0].Amount == 1
		}
	case "casualty":
		if len(chosen) == 1 {
			pc.sacs = append(pc.sacs, chosen[0].Obj)
			pc.casualtyPaid = true
			// Casualty:X: the chosen creature's power names the amount; it is
			// read live at payment (payCast), the rules time the sacrifice
			// settles.
			pc.casualtySac = chosen[0].Obj
		}
	case "gift_decline":
		// CR 702.168: a declined gift is the plain cast -- no promise, and
		// pushCast emits only the Amount-0 record. The byte-identical shape
		// for every non-Gift carrier, which never reaches this ask at all.
		pc.giftPromise = false
	case "gift_promise":
		// The promised opponent rides Option.Player, the field the
		// protector/player elections share. An answer naming no live seat
		// (only reachable from a hand-built decision) degrades to a decline
		// rather than silently promising seat 0.
		pc.giftPromise = false
		if len(chosen) > 0 && chosen[0].Player != pc.player &&
			int(chosen[0].Player) < len(e.G.Players) && !e.G.Players[chosen[0].Player].Lost {
			pc.giftPromise = true
			pc.giftTo = chosen[0].Player
		}
	case "conspire":
		// CR 702.78a: the two chosen creatures are the tap the conspired cast
		// pays. They settle through pc.taps (payCast taps them) and
		// conspirePaid records that the provenance flag is owed. A Min==Max==2
		// KChoose, so a well-formed answer is exactly two options.
		for _, o := range chosen {
			pc.taps = append(pc.taps, o.Obj)
		}
		if len(chosen) >= 2 {
			pc.conspirePaid = true
		}
	case "exile":
		for _, o := range chosen {
			pc.delve = append(pc.delve, o.Obj)
		}
	case "sacrifice":
		if pc.emerge && pc.sacPart == 0 && len(chosen) == 1 {
			pc.emergeSac = chosen[0].Obj
		}
		for _, o := range chosen {
			pc.sacs = append(pc.sacs, o.Obj)
			pc.sacPaid++
		}
		part := pc.cost.Sac[pc.sacPart]
		total := int(part.N)
		if part.Announced {
			total = int(pc.x)
		}
		if pc.sacPaid >= total {
			pc.sacPart++
			pc.sacPaid = 0
		}
	case "subcounter":
		// The chosen counter-removal pick of a SubCounter cost part: a
		// wildcard "Any" part records one counter unit per answer (the ask
		// loop keeps asking until the part is fully paid), a fixed-kind part
		// records its one object and advances.
		wildcard := pc.subCounterPart < len(pc.cost.SubCounter) && strings.EqualFold(pc.cost.SubCounter[pc.subCounterPart].Spec, "Any")
		for _, o := range chosen {
			pc.subCounterPays = append(pc.subCounterPays, subCounterPay{part: pc.subCounterPart, obj: o.Obj, kind: o.Counter})
		}
		if !wildcard {
			pc.subCounterPart++
		}
	case "discard":
		for _, o := range chosen {
			pc.discards = append(pc.discards, o.Obj)
		}
		pc.discardPart++
	case "altaddcost":
		// The either-or additional cost (AlternateAdditionalCost): the chosen
		// part's cost folds into pc.cost (Plus), so the ordinary stages settle
		// it and payCast charges it. The chosen part rides on Option.Amount
		// (the index into pc.altAddParts), not the option Index, for the same
		// reason xAsk's value does.
		if len(chosen) > 0 && chosen[0].Amount >= 0 && int(chosen[0].Amount) < len(pc.altAddParts) {
			pc.cost = pc.cost.Plus(ParseCost(pc.altAddParts[chosen[0].Amount]))
		}
	case "exilecost":
		for _, o := range chosen {
			pc.exiles = append(pc.exiles, o.Obj)
		}
		pc.exilePart++
	case "evidence":
		// The CollectEvidence payment's chosen graveyard cards (alltargeted1).
		// The SETTLE validation (total mana value at least the resolved
		// amount, every card still the payer's) runs in evidenceAsk on the
		// continueCast re-entry, which re-poses the ask when the answer falls
		// short -- so a malformed answer never pays a short evidence.
		for _, o := range chosen {
			pc.evidence = append(pc.evidence, o.Obj)
		}
	case "movetogravecost":
		for _, o := range chosen {
			pc.moveGraves = append(pc.moveGraves, o.Obj)
		}
		pc.moveGravePart++
	case "revealcost":
		for _, o := range chosen {
			pc.reveals = append(pc.reveals, o.Obj)
			pc.revealHandArm = append(pc.revealHandArm, true)
		}
		pc.revealPart++
	case "revealorchoose":
		// Either-or cost, REVEAL arm: the elected hand cards are a real public
		// reveal (revealHandArm true; emitChoiceCosts announces them).
		for _, o := range chosen {
			pc.reveals = append(pc.reveals, o.Obj)
			pc.revealHandArm = append(pc.revealHandArm, true)
		}
		pc.revealOrChoosePart++
	case "choosecost":
		// Either-or cost, CHOOSE arm: a permanent the payer controls, elected
		// at cast time. It rides the same paid list the `Revealed$<Property>`
		// refs read (Forge's CostReveal owns both arms) but revealHandArm is
		// false, so emitChoiceCosts announces a choice, never a reveal.
		for _, o := range chosen {
			pc.reveals = append(pc.reveals, o.Obj)
			pc.revealHandArm = append(pc.revealHandArm, false)
		}
		pc.revealOrChoosePart++
	case "beholdcost":
		for _, o := range chosen {
			pc.beholds = append(pc.beholds, o.Obj)
		}
		pc.beholdPart++
	case "tapcost":
		part := CostPart{}
		if pc.tapPart < len(pc.cost.TapPermanent) {
			part = pc.cost.TapPermanent[pc.tapPart]
		}
		for _, o := range chosen {
			pc.taps = append(pc.taps, o.Obj)
		}
		pc.tapPart++
		// A dynamic X-form part whose tap election ANNOUNCED the count (no other
		// announce-bearing part ran xAsk first -- see tapPermanentCostAsk): the
		// chosen count is the cast's {X} (CR 601.2b), which the pay-time
		// CastInfo then carries to resolution and Count$xPaid reads. A part
		// whose cost pre-announced the X (pc.xDone) settles exactly that value
		// and must not overwrite it.
		if part.Dyn == "X" && !pc.xDone {
			pc.x = int32(len(chosen))
		}
	case "blightcost":
		for _, o := range chosen {
			pc.blights = append(pc.blights, o.Obj)
		}
		pc.blightPart++
	case "returncost":
		for _, o := range chosen {
			// K:Ninjutsu (CR 702.49b): the permanent the activated ability puts
			// onto the battlefield attacks the SAME defender the returned
			// creature was attacking. Capture that defender here, while the
			// chosen attacker is still a battlefield object (payCast moves it to
			// hand at settlement, which clears its Attacking field), and carry it
			// on the AbilityPush event so resolution can bind it.
			if e.activationIsNinjutsu(pc) {
				if o := e.G.Obj(o.Obj); o != nil {
					pc.ninjutsuDefender = o.Attacking
					pc.ninjutsuHasDefender = true
				}
			}
			pc.returns = append(pc.returns, o.Obj)
		}
		pc.returnPart++
	case "puttolibcost":
		for _, o := range chosen {
			pc.putToLibs = append(pc.putToLibs, o.Obj)
		}
		pc.putToLibPart++
	case "forage_exile":
		pc.cost.Exile = append(pc.cost.Exile, CostPart{N: 3, Spec: "Card", Zone: state.ZGraveyard})
	case "forage_food":
		if len(chosen) > 0 {
			pc.sacs = append(pc.sacs, chosen[0].Obj)
		}
	case "pay_W", "pay_U", "pay_B", "pay_R", "pay_G", "pay_C":
		// A hybrid or Phyrexian pip paid with pool mana: record which colour.
		if len(chosen) > 0 {
			pc.payColor[state.ManaIndex(chosen[0].Kind[4])]++
		}
		pc.payIdx++
	case "pay_life":
		// A Phyrexian face (plain or hybrid) paid with two life.
		pc.payLife += 2
		pc.payIdx++
	case "pay_generic":
		// A monocolour hybrid pip paid with its generic face.
		if len(chosen) > 0 {
			pc.payGeneric += int32(chosen[0].Amount)
		}
		pc.payIdx++
	case "convoke_W", "convoke_U", "convoke_B", "convoke_R", "convoke_G", "convoke_generic", "improvise_generic", "waterbend_generic":
		for _, choice := range chosen {
			color := byte(0)
			if strings.HasPrefix(choice.Kind, "convoke_") && choice.Kind != "convoke_generic" {
				color = choice.Kind[len("convoke_")]
			}
			pc.convoke = append(pc.convoke, convokePayment{id: choice.Obj, color: color,
				countsMana: strings.HasPrefix(choice.Kind, "convoke_"), waterbend: choice.Kind == "waterbend_generic"})
		}
	case "harmonize":
		for _, choice := range chosen {
			pc.convoke = append(pc.convoke, convokePayment{id: choice.Obj, power: int32(choice.Amount)})
		}
	case "activate":
		// CR 601.2g: a source's mana abilities are distinct activations that
		// share its tap cost. activateManaPayment resolves a singleton
		// immediately or asks the caster to choose one before re-entering
		// this payment window -- the payment-window form, so an
		// InstantSpeed$ True mana ability (Lion's Eye Diamond, "Activate
		// only as an instant") is withheld: paying a cost is no priority
		// moment.
		if len(chosen) > 0 {
			e.activateManaPayment(pc.player, chosen[0].Obj, true)
		}
	case "done":
		// CR 601.2g: the player declines further mana abilities; pay the cost.
		pc.windowDone = true
	case "mana":
		// The announced window's per-ability option (announce_pay.go).
		if pc.announced && len(chosen) > 0 {
			e.announcedActivate(pc, chosen[0])
		}
	case decision.OptAutoFill:
		// Auto-fill: the planner's activations for what is still owed, run by
		// the ordinary plan executor on re-entry (spec §4.4). The plan is
		// re-derived at the unchanged state the ask priced.
		if pc.announced {
			if plan := e.announcedAutoFillPlan(pc, e.castPaymentMana(pc)); plan != nil {
				pc.payment = &plannedCastPayment{actionID: decision.OptAutoFill, plan: *plan}
				pc.paymentNext = 0
				pc.paymentFallback = nil
			}
		}
	case decision.OptUndoTap:
		if pc.announced {
			if _, ok := e.undoableWindowTap(pc); ok {
				e.undoWindowTap(pc)
			}
		}
	case decision.OptCancelCast:
		if pc.announced {
			e.cancelAnnouncedCast(pc)
		}
	}
}

// modeIsKicked reports whether a pendingCast.mode names a kicked cast: the
// single-cost Kicker's "kicked", the and/or Kicker's per-part "kicked1"/
// "kicked2"/"kickedboth", or a multikicked cast. It is the ONE home of that
// set, shared by modeFlags (the pay-time flag stamp), spellConstraintMatches'
// CastStatic match and targetBoundCtx's pre-payment Count$Kicked binding, so
// the three spellings cannot drift.
func modeIsKicked(mode string) bool {
	switch mode {
	case "kicked", "kicked1", "kicked2", "kickedboth", "multikicked":
		return true
	}
	return false
}

// modeFlags maps a pendingCast.mode to the CastInfo Counter string
// (events.FlagsString of the matching CastFlags bit), "" for a plain cast.
func modeFlags(mode string) string {
	switch mode {
	case "kicked":
		return events.FlagsString(state.FlagKicked)
	// The and/or Kicker's per-part modes: each index flag rides with the
	// bare FlagKicked (every part paid IS a kicked cast -- the bare
	// predicate and the Condition$ Kicked gate keep matching), so the
	// CastInfo wire carries the part's identity and the generic read.
	case "kicked1":
		return events.FlagsString(state.FlagKicked | state.FlagKicked1)
	case "kicked2":
		return events.FlagsString(state.FlagKicked | state.FlagKicked2)
	case "kickedboth":
		return events.FlagsString(state.FlagKicked | state.FlagKicked1 | state.FlagKicked2)
	case "surged":
		return events.FlagsString(state.FlagSurged)
	case "flashback":
		return events.FlagsString(state.FlagFlashback)
	// Jump-start (CR 702.84a): like flashback, the flag is what the
	// resolution reader (spellRestZone) and the fizzle reader
	// (spellFizzleZone) read to exile the card instead of the graveyard, on
	// resolution AND when countered -- the card may not be jump-started a
	// second time from the graveyard.
	case "jumpstart":
		return events.FlagsString(state.FlagJumpstart)
	// Aftermath (CR 702.85a): the flag is what the resolution reader
	// (spellRestZone) and the fizzle reader (spellFizzleZone) read to exile
	// the card instead of the graveyard -- on resolution AND when countered,
	// the same "any time it would leave the stack" convention flashback's
	// TestFlashbackedSpellCounteredGoesToExile pins.
	case "aftermath":
		return events.FlagsString(state.FlagAftermath)
	// Fuse (CR 702.101b): one spell resolving both halves. The flag is the
	// provenance rules/stack.go's resolution reader dispatches on to run both
	// faces' spell abilities instead of the single Face().SpellAbility().
	// The card stays at its front face, so no face-flip reader is involved.
	case "fuse":
		return events.FlagsString(state.FlagFused)
	case "miracle":
		return events.FlagsString(state.FlagMiracle)
	// The alternative-cost keyword family: the flag is what the ETB machinery
	// (evoke's sacrifice trigger, dash's haste + delayed return, warp's
	// delayed exile) and the warp recast offer read.
	case "escape":
		return events.FlagsString(state.FlagEscaped)
	case "evoked":
		return events.FlagsString(state.FlagEvoked)
	case "dashed":
		return events.FlagsString(state.FlagDashed)
	// Blitz (CR 702.152a): the flag is what the ETB machinery
	// (altCostEnter -> blitzEnter) reads for the haste grant, the dies-draw
	// granted trigger and the next-end-step sacrifice. It is a
	// CastProvenanceFlag (state/object.go), so a stack copy does not inherit
	// it.
	case "blitzed":
		return events.FlagsString(state.FlagBlitzed)
	case "overloaded":
		return events.FlagsString(state.FlagOverloaded)
	case "warped":
		return events.FlagsString(state.FlagWarped)
	// The Adventure spell face's cast (CR 714.3a): the flag is what the
	// resolution reader (spellRestZone) uses to exile the spell into the
	// adventure zone instead of the graveyard. adventure_recast deliberately
	// has NO case here -- casting the main face from the adventure zone is an
	// ordinary cast, exactly like warp_recast.
	case "adventure_alt":
		return events.FlagsString(state.FlagAdventure)
	case "buyback":
		return events.FlagsString(state.FlagBuyback)
	// Offspring (CR 702.175a): the mode marks the intent to pay the optional
	// ADDITIONAL offspring cost, and the offer exists only when it is payable
	// (rules/legal.go's walks), so -- unlike Squad/Multikicker/Replicate,
	// whose count asks can still answer 0 -- there is no decline case and the
	// flag is unconditional. Bare FlagOffspringPaid rides the ordinary
	// pay-time CastInfo (payCast), and the keyword expansion's ETB trigger
	// reads it through Count$OffspringPaid to mint the 1/1 token copy.
	case "offspring":
		return events.FlagsString(state.FlagOffspringPaid)
	case "optionalcost":
		return events.FlagsString(state.FlagOptionalCostPaid)
	case "mayplay":
		return events.FlagsString(state.FlagMayPlay)
	case "harmonize":
		return events.FlagsString(state.FlagHarmonize)
	case "suspend":
		return events.FlagsString(state.FlagSuspend)
	// Foretell's later cast (CR 702.126a): the flag is the provenance an ETB
	// reader (Lupine Harbingers' CheckSVar$ WasForetold) and Count$Foretold
	// read off the permanent the spell becomes -- the stack->battlefield
	// persistence the Suspend flag rides too. The {2} ACTION's CastInfo is
	// emitted directly by payCast's foretell branch (which then returns, so
	// the ordinary flags path below is never reached for that mode); the
	// action's flag has no modeFlags case for the same reason suspend's
	// branch does not share this switch.
	case "foretell_cast":
		return events.FlagsString(state.FlagForetold)
	// Mayhem (the Doom Prevails keyword): the flag is the provenance the
	// Card.CastSa Spell.Mayhem condition reads (Sandman's Quicksand's "if
	// this spell's mayhem cost was paid" split), through the CastSa
	// provenance strip (rules/cast_provenance.go's castSaAdmits and the
	// per-event walk in spellsCastThisTurnMatching, effects/conditions.go's
	// conditionMet). Mayhem has no exile tail, so the flag is the whole of
	// what the cast records.
	case "mayhem":
		return events.FlagsString(state.FlagMayhem)
	// Web-slinging (CR 702.186a-family, Marvel's Spider-Man): the flag is the
	// provenance the Card.Self+webSlinged filter predicate reads -- Spiders-Man,
	// Heroic Horde's ETB trigger and Scarlet Spider, Ben Reilly's Sensational
	// Save replacement. It is a CastProvenanceFlag (state/object.go), so a
	// stack copy does not inherit it. webslinged_grant_N modes are normalized
	// to this canonical mode in beginCastWith before this switch is reached.
	case "web-slinging":
		return events.FlagsString(state.FlagWebSlinged)
	// Bestow (CR 702.114a): the flag is the provenance the resolution
	// reader (resolveTop) uses to substitute the synthesized Aura attach
	// spell, and what keeps a bestowed cast distinguishable on the wire.
	case "bestowed":
		return events.FlagsString(state.FlagBestowed)
	// Mutate (CR 702.140a): the flag is the provenance the resolution reader
	// uses to merge the spell into its target. modeFlags maps "mutated" to
	// the bare flag; payCast ORs FlagMutatedTop in when the answered placement
	// put the mutating card on top (CR 702.140b).
	case "mutated":
		return events.FlagsString(state.FlagMutated)
	// Multikicker (CR 702.43): the mode marks the INTENT to pay the
	// optional multikicker cost, and the count ask (multikickAsk) can still
	// answer 0 -- a DECLINED multikick must stay the byte-identical plain
	// cast, no flag and no event, exactly the "replicated" contract above.
	// When a payment WAS made, payCast ORs bare FlagKicked (a multikicked
	// cast IS a kicked cast) and FlagMultikicked onto the trailing CastInfo.
	case "multikicked":
		return ""
	// Squad (CR 702.66): the mode marks the INTENT to pay the optional squad
	// cost, and the count ask (squadAsk) can still answer 0 -- a DECLINED
	// squad must stay the byte-identical plain cast, no flag and no event,
	// exactly the "replicated"/"multikicked" contract above. When a payment
	// WAS made, payCast ORs FlagSquadPaid onto a trailing CastInfo.
	case "squadded":
		return ""
	// Conspire (CR 702.78a): the mode marks the INTENT to tap two eligible
	// creatures, and the offer can be taken only when they exist, but a
	// DECLINED/plain cast must stay byte-identical -- no flag and no event,
	// exactly the "replicated" contract above. When the tap WAS paid,
	// payCast ORs FlagConspired onto a trailing CastInfo.
	case "conspired", "casualty":
		return ""
	// The morph family's face-down cast (CR 702.37a/702.168a/702.169a): the
	// flag is the provenance that names the keyword family the {3} cast
	// rode, what the resolution reader (rules/stack.go resolveTop) dispatches
	// on to resolve the spell with no printed spell abilities and no
	// targets, what rules/resolution.go's moveResolvedOffStack re-carries
	// the face-down entry marker for, and what a later turn-face-up action
	// prices its cost from. The three sibling bits shape the resolution the
	// FlagFused/FlagBestowed way, so they are deliberately NOT in
	// CastProvenanceFlags.
	case "morphed":
		return events.FlagsString(state.FlagMorphed)
	case "megamorphed":
		return events.FlagsString(state.FlagMegamorphed)
	case "disguised":
		return events.FlagsString(state.FlagDisguised)
	}
	return ""
}

// flashPermittedCandidates narrows a cast's target pool to the candidates
// that the face's target-conditional CastWithFlash grant covers. It is the
// ask-side twin of the offer's castWithFlash existential and the CR 601.2e
// recheck: an off-sorcery cast whose timing rests on
// `ValidSA$ Spell.IsTargeting Valid <spec>` may only announce a target matching
// <spec>, so handing the player a target the grant does not cover would
// guarantee the post-push reversal. Callers gate the call on
// flashGrantCoversTargets(pc.player, pc.card, f, nil), which is false exactly
// for the shape where the grant IS the timing basis -- an instant, a printed
// Flash or a MayFlashSac rider keeps its full pool unchanged.
func (e *Engine) flashPermittedCandidates(pc *pendingCast, f *cards.Face, candidates []targetCandidate) []targetCandidate {
	out := make([]targetCandidate, 0, len(candidates))
	for _, c := range candidates {
		t := state.Target{Obj: c.obj}
		if c.kind == "player" {
			t = state.Target{Player: c.player, IsPlayer: true}
		}
		if e.flashGrantCoversTargets(pc.player, pc.card, f, []state.Target{t}) {
			out = append(out, c)
		}
	}
	return out
}

// targetAsk is the last stage of continueCast before commitCast: it asks the
// spell or activated ability's target selection (CR 601.2c / 602.2b) while the
// proposal is still provisional -- BEFORE any cost is paid, any sacrificial
// permanent moves, or the object is put on the stack. That ordering is what
// makes the cast a transaction: the target answer (601.2c) precedes payment
// (601.2h), and the cast trigger (601.2i, fired by PutOnStack) waits until the
// proposal is complete. handleTarget (stack.go) completes the transaction by
// calling commitCast and then records the chosen targets onto the object that
// actually reached the stack.
//
// It returns true when it either asked a target decision or ABORTED the
// proposal. A proposal that can never complete is reversed here, before
// anything has been paid or moved (CR 733.1): the card left the zone, the
// resolved mana cost is no longer payable, or a mandatory target (min >= 1)
// has zero legal candidates. Clearing e.cast with nothing committed restores
// the pre-proposal board. An SA with no ValidTgts (or a zero-minimum target
// with no legal candidate, Requirement N2) returns false so commitCast runs
// directly.
func (e *Engine) targetAsk() bool {
	pc := e.cast
	if pc == nil || pc.passedTarget {
		// passedTarget: the flow has already moved through the 601.2c target
		// choice into payCast (a spell or ability whose target was chosen, or
		// one with no target); a mana-window resume re-enters continueCast and
		// must not re-ask for a target already settled.
		return false
	}
	if pc.faceDown {
		// Morph family (CR 708.4): a face-down spell has no targets to
		// announce -- the printed targets do not exist while the spell is
		// face down. The flow proceeds to payCast; the resolution reader
		// (resolveTop's morph dispatch) skips the printed spell abilities the
		// same way.
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil {
		return false
	}
	f := o.Face()
	sa := e.castStageSA(pc, o, f)
	// A Fuse half that declares no targets (Alive // Well's Well) is skipped:
	// advance to the next stage while one remains, so a half with a target
	// requirement is still asked. When no stage declares targets the flow
	// proceeds directly to payment, exactly as a single targetless cast does.
	for sa != nil && sa.Params["ValidTgts"] == "" && e.castHasNextTargetStage(pc, o) {
		pc.targetStage++
		sa = e.castStageSA(pc, o, f)
	}
	if sa == nil || sa.Params["ValidTgts"] == "" {
		return false
	}
	// A proposal whose resolved mana cost can no longer be paid, and with no
	// untapped mana-ability source the 601.2g window could activate to enable
	// it, can never complete. Reverse it (CR 733.1), which undoes the pushCast
	// stack move (E2: no progress was made, so hold this card's option out of
	// the window rather than re-offering the same unpayable cast). When such a
	// source exists, the window (asked later, in payCast) may still supply the
	// mana, so the proposal is not yet dead -- it proceeds to the target ask
	// and then the window. Resolved means X is fixed, the CR 601.2f modifiers
	// are applied and Delve credit is subtracted, all settled by the stages
	// above.
	mana := e.castPaymentMana(pc)
	// resolvedMana carries no live pip at this stage (the pip announcements
	// are already settled, manaAsk runs before targetAsk), so the composed
	// payable check here is the same composition manaToPay charges;
	// paymentMana additionally folds the announced Convoke/Harmonize
	// contributions in (zero when none were announced). costPayable is the
	// conversion-aware equivalent: the SAME resolveMana payManaConvFor will
	// run, including RestrictValid$ provenance. The
	// targetDependentCostMayPay arm keeps the ValidTarget$ reducer exception.
	if !e.costPayableClass(pc.player, paymentForCast(pc, mana),
		pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}, mana) &&
		!e.hasUntappedManaSource(pc.player) && !e.targetDependentCostMayPay(pc) {
		e.abortCast(pc, "cast aborted: cost no longer payable", true)
		return true
	}
	min, max := e.resolvedTargetBounds(pc.player, pc.card, sa, pc.x)
	// CR 115.5: a spell may not target itself (excludeSelf == the card); an
	// activated ability CAN target its own Source permanent (Mother of Runes
	// targeting itself). The Face-less ability stack object on the stack is
	// never offered (legalTargetCandidates drops Face()-less stack objects),
	// so the source permanent is still a legal target of its own ability.
	// An attach ability (the Equip/Reconfigure expansion mints AB$ Attach;
	// a spell SA may carry API$ Attach directly) can never target its own
	// source permanent: CR 701.3a attaches an object to ANOTHER permanent,
	// and effAttach refuses the self-attach at resolution. The pool and the
	// resolution must agree, so the source is excluded AT THE ASK for an
	// attach SA even though the Mother-of-Runes convention lets a generic
	// activated ability target its own source. For Equip the exclusion is
	// inert (an Equipment face does not match Creature specs); it is live
	// exactly for Reconfigure, whose unattached form IS a creature and was
	// offered itself here (r2 review MAJOR).
	var excludeSelf state.ObjID
	if !pc.isAbility() || sa.API == "Attach" {
		excludeSelf = pc.card
	}
	candidates := e.legalTargetCandidates(pc.player, pc.card, excludeSelf, sa)
	// CR 601.2e: a cast whose off-sorcery timing rests on a target-conditional
	// CastWithFlash grant may only announce a target the grant covers. The
	// offer admitted the cast because SOME qualifying target exists
	// (castWithFlash's potential-target census); without this the ask would
	// still offer every legal target and an uncovered pick would be reversed
	// after the push. Filter to the grant-covered pool so offer and
	// announcement judge the same set. The fuse cast judges each half against
	// its own stage targets in recheckIllegal, so it is left unfiltered here.
	//
	// The permission is EXISTENTIAL over the announced set
	// (spellMatchesValidSA judges the whole target list), so the per-candidate
	// narrowing is exact only for a single-target ask. A multi-target ask
	// whose pool has a proper subset of covered candidates keeps the FULL
	// pool: a legal completion may mix covered and uncovered targets, and
	// pruning the uncovered side would hide legal choices and -- under a
	// mandatory minimum above the covered count -- abort a castable spell
	// (offer census admitted it, the ask could not announce it). The completed
	// selection is judged by recheckIllegal (CR 601.2e) over the same grant,
	// before any cost is paid. With no covered candidate at all the pool
	// empties and the mandatory-minimum census below aborts the proposal (or,
	// at Min 0, the CR 601.2e recheck reverses an untargeted announcement).
	if !pc.isAbility() && pc.offSorcery && f != nil && pc.mode != "fuse" &&
		!e.flashGrantCoversTargets(pc.player, pc.card, f, nil) {
		covered := e.flashPermittedCandidates(pc, f, candidates)
		if len(covered) == len(candidates) || len(covered) == 0 || (min <= 1 && max <= 1) {
			candidates = covered
		}
	}
	// Overload changes the word "target" to "each". It makes no selection at
	// announcement time: the current matching set is derived at resolution,
	// so permanents entering or changing controller in response are handled.
	// No target decision/event is emitted and zero objects is legal.
	if pc.mode == "overloaded" {
		return false
	}
	// CR 601.2c: distinct modal modes each declare and choose their own
	// target. The combined decision uses one exclusive Group per mode, so
	// its exact count cannot be satisfied by choosing two targets for one
	// mode while omitting another.
	if pc.targetStage == 0 && !pc.isAbility() && f != nil {
		if root := f.SpellAbility(); root != nil {
			choices := strings.Split(root.Params["Choices"], ",")
			if status, _ := effects.CharmCrossModeShape(f.SVars, choices); status == effects.CharmUniqueSupported {
				var tbms []*cards.SA
				for _, name := range o.ChosenModes {
					if sub := cards.ResolveSVar(f.SVars, name); sub != nil && strings.TrimSpace(sub.Params["ValidTgts"]) != "" {
						tbms = append(tbms, sub)
					}
				}
				if len(tbms) >= 2 && e.askCrossModeCharmTargets(pc.player, pc.card, tbms) {
					return true
				}
			} else {
				asked, infeasible := e.askCharmModeTargets(pc.player, pc.card, f.SVars, root, o.ChosenModes)
				if infeasible {
					e.abortCast(pc, "cast aborted: no legal modal target", true)
					return true
				}
				if asked {
					return true
				}
			}
		}
	}
	// A ValidTarget$ cost modifier can make this proposal offerable only for
	// particular targets. Once mana faces are announced, do not put a target
	// on the menu unless repricing that target can still complete the cast:
	// selecting an unaffordable target and then reversing the proposal is not
	// a legal CR 601.2c choice. An untapped mana source keeps a candidate on
	// the menu because the 601.2g window may make its final cost payable.
	//
	// For a multi-target declaration this is deliberately conservative: a
	// target that needs another selected target to satisfy ValidTarget$ is
	// withheld rather than exposing a selection subset that would abort. The
	// engine's decision type cannot express cross-option dependencies, and
	// withholding is safer than offering an illegal transaction.
	candidates = e.affordableTargetCandidates(pc, candidates)
	// MaxTotalTargetPower$ (Reunion of the House): the running total-power
	// cap over the selection. Prune the candidates that can provably join
	// no legal selection (individually over the cap unless a negative-power
	// candidate could offset them -- Scourge of the Skyclaves's CDA is -1 at
	// a 21-life opponent and 11 + (-1) = 10 is legal under a cap of 10)
	// BEFORE the mandatory-minimum census so a cast whose every candidate
	// alone busts the cap aborts like a targetless one, and carry the
	// running cap as the decision's cumulative budget (Decision.MaxSum over
	// each option's Value = the candidate's power) -- the same wire contract
	// a Dig's WithTotalCMC$ budget uses, so Decision.Validate enforces the
	// cap on every submitted answer and the bot's Clamp/FitRequired repair
	// mirrors it. A candidate whose power alone fits but whose combination
	// busts the cap stays offered: the wire contract rejects the combination.
	candidates, powerCap, powerCapped := e.totalPowerCappedCandidates(candidates, pc.player, pc.card, sa, pc.x)
	candidates, cmcCap, cmcCapped := e.totalCMCCappedCandidates(candidates, pc.player, pc.card, sa, pc.x)
	// Forge's per-controller selection shapes (TargetsForEachPlayer$ one per
	// player; TargetsWithDifferentControllers$ one per controller): the same
	// bounds/group/capacity read the trigger-path askTarget uses, so a OneEach
	// CAST ask (Unexplained Absence's "up to one target nonland permanent
	// each player controls") offers the whole table's slots and the wire's
	// mutual-exclusion rule enforces one pick per controller. Before this the
	// cast-time ask ignored the shape and capped the ask at the plain Max.
	// Read AFTER affordability and the power-cap prune so `distinct` is the
	// real selectable capacity: an unaffordable or over-cap candidate cannot
	// contribute a controller to it.
	min, max, _, _ = e.oneEachTargetBounds(sa, candidates, min, max)
	min, max, _, sameController := e.sameControllerTargetBounds(sa, candidates, min, max)
	min, max, _, setMode, setKind := e.setPropTargetBounds(sa, candidates, min, max)
	// The ONE feasibility rule (rules/legal.go's targetChoiceFeasible) is the
	// same predicate the cast offer census ran (targetSAAvailable), so an
	// offered cast can always announce a legal target: the count, the
	// per-controller exclusivity/OneEach capacity, the same-controller group
	// capacity and the set-property capacity are judged identically at offer
	// and at the ask.
	if min > 0 && !e.targetChoiceFeasible(sa, candidates, min) {
		// CR 601.2c: a proposal with fewer legal targets than its mandatory
		// minimum -- or one whose per-controller constraint admits fewer
		// distinct controllers than its mandatory minimum -- cannot be
		// announced. Reverse the whole proposal (CR 733.1):
		// the pushed object returns to where it was, nothing is paid and no
		// cast trigger fires. No library was shuffled during the proposal, so
		// the 733.1 library exception does not apply.
		//
		// suppress=true engages the F05-2 (CR 733.2) no-progress discipline
		// like every other abort site: the FIRST identical abort of this card
		// in the window leaves the option offered (a player may still make a
		// play that creates a legal target -- cast a creature, then Shelter),
		// the SECOND holds it out of the window. Without it a seat whose
		// policy keeps re-picking the same castable-but-targetless spell
		// livelocks inside one priority window forever (measured: the bot
		// bench replayed "cast Shelter -> abort" 20000 times, engine note
		// "cast aborted: no legal target", zero state change). Any genuine
		// state change clears the count and the held-out set, so a target
		// created later re-offers the cast normally.
		e.abortCast(pc, "cast aborted: no legal target", true)
		return true
	}
	if min == 0 && len(candidates) == 0 {
		// Requirement N2: a subject that MAY target zero things resolves
		// untargeted when no legal target exists; proceed straight to payCast
		// with no target decision.
		return false
	}
	if max == 0 {
		// A dynamic bound RESOLVED to zero (Tear Asunder's kicked main SA:
		// TargetMin$ X | TargetMax$ X over SVar:X:Count$Kicked.0.1) declares
		// that this stage takes no targets -- the chained sub does the
		// work. A Min 0 / Max 0 ask would offer nothing selectable; skip
		// straight to payCast exactly as the N2 arm above does.
		return false
	}
	// The decision's Source is the object that must not be offered as its own
	// target (CR 115.5). For a spell that is the card (excluded via
	// excludeSelf). For an activated ability the object that may not target
	// itself is the ability stack object, which is not minted yet (the push
	// is a no-op for an ability; payCast's AbilityPush creates it), so Source
	// is 0 and the source permanent remains a legal target of its own ability
	// (Mother of Runes) via excludeSelf == 0. The prompt keeps the source
	// permanent's name for readability.
	var src state.ObjID
	if !pc.isAbility() || sa.API == "Attach" {
		src = pc.card
	}
	// CR 601.2c: TargetingPlayer$ names another player as the chooser for this
	// target declaration. The cast/activation form (Player.Opponent) has no
	// trigger context, so before this the ask silently stayed with the caster.
	// targetAskChooser is the same home askTarget uses, so both the cast flow
	// and the trigger/resolution flow route identically; target legality keeps
	// pc.player as the controller reference below.
	chooser := pc.player
	pickOwed := false
	if who, ok, pick := e.targetAskChooser(pc.player, pc.card, sa); pick {
		pickOwed = true
	} else if ok {
		chooser = who
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KTarget, Min: min, Max: max,
		Prompt: "Choose a target for " + e.targetName(pc.card),
		Source: src, TargetEffect: e.describeTargetEffect(pc.player, pc.card, sa, pc.x),
		TargetsWithSameController: sameController, SetPropMode: setMode}
	if !pc.isAbility() && pc.stackObj == pc.card {
		lim := max
		if lim < 0 || lim > len(candidates) {
			lim = len(candidates)
		}
		d.AffordableTargets = e.striveAffordableTargets(pc, lim)
	}
	for _, candidate := range candidates {
		// Shared with stack.go's askTarget so a Face-less ability object (a
		// TargetType$ Activated/Triggered census) can never nil-deref here.
		label := e.targetOptionLabel(candidate)
		o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
			Label: label, Obj: candidate.obj, Player: candidate.player}
		o.Group = e.targetControllerGroup(sa, candidate)
		o.Controller = e.candidateControllerSeat(candidate)
		o.SetProps = e.setPropTokensFor(setKind, candidate)
		// Option.Value is omitempty and read only under a budget
		// (Decision.HasBudget), so a budget-less target ask keeps its wire
		// payload byte-identical. Every present cap -- zero and negative
		// included, via Decision.Budgeted -- rides the wire, so
		// Decision.Validate enforces the total on every submitted answer.
		// The Value is the DERIVED power (Engine.Power), matching the
		// pruning read -- the printed Face().Power() read a CDA creature as
		// zero.
		if powerCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				o.Value = int(e.Power(candidate.obj))
			}
		}
		if cmcCapped && candidate.kind != "player" {
			if co := e.G.Obj(candidate.obj); co != nil && co.Face() != nil {
				o.Value = int(co.Face().ManaValue())
			}
		}
		d.Options = append(d.Options, o)
	}
	if powerCapped {
		d.MaxSum, d.Budgeted = powerCap, true
	}
	if cmcCapped {
		d.MaxSum, d.Budgeted = cmcCap, true
	}
	if pickOwed {
		// The multi-opponent Opponent form (agent-20260925T085158Z-c861188d):
		// the controller first names WHICH opponent answers. The selection
		// ask is posed here, at the cast flow's suspension boundary, and
		// the answer re-enters continueCast, whose targetAsk re-run reads
		// the pin and poses the target ask to the chosen seat.
		e.poseOpponentPick(pc.player, pc.card, sa, oppPickCastRoot)
		return true
	}
	e.ask(d)
	return true
}

// allTargets is Forge's AllTargeted$ union (task alltargeted1): the root
// cast-time targets followed by every pre-asked sub-ability chain answer in
// chain order. The cost-evaluation sites that read AllTargeted$ before
// payment (repriceForTargets, evidenceAsk) thread this through
// Ctx.AllTargets; a chain with no sub answers unions to exactly the root's
// own set.
func (pc *pendingCast) allTargets() []state.Target {
	if len(pc.subAns) == 0 {
		return pc.targets
	}
	out := append([]state.Target(nil), pc.targets...)
	for _, ts := range pc.subAns {
		out = append(out, ts...)
	}
	return out
}

// collectSubTargetPreAsks walks the root SA's SubAbility$ chain in order and
// returns the bodies whose targeting Forge would pre-ask at cast time
// (CR 601.2c): a ValidTgts$ declaration, no already-named Defined$ fetch list
// (target reuse -- with the API$ Fight carve-out, whose SA carries TWO
// independent target lists and whose ValidTgts$ IS its second ask), and not
// the ChangeZone family (effChangeZone's own mid-resolution ask owns that
// shape). The same inclusion rules effects' chosenTargetsFor applies to the
// mid-resolution path, so a sub asked at resolution today is exactly a sub
// pre-asked here, and one asked by NEITHER (a Defined$ reuse) stays silent.
// Deliberately excluded whole: modal (Charm) and CopySpellAbility roots --
// castModeAsk/AskCopyTargets own their targeting -- and trigger bodies (this
// walks the cast flow only; a trigger's placement ask timing is CR 603.3c's,
// not 601.2c's).
func (e *Engine) collectSubTargetPreAsks(root *cards.SA) []*cards.SA {
	if root == nil || root.API == "Charm" || root.API == "CopySpellAbility" {
		return nil
	}
	var out []*cards.SA
	for sa := root.Sub; sa != nil; sa = sa.Sub {
		if strings.TrimSpace(sa.Params["ValidTgts"]) == "" {
			continue
		}
		defined := strings.TrimSpace(sa.Params["Defined"])
		if defined != "" && !effects.DefinedIsTargetReuse(defined) && sa.API != "Fight" {
			continue
		}
		if sa.CompiledAPI() == cards.APIChangeZone || sa.API == "ChangeZone" {
			continue
		}
		out = append(out, sa)
	}
	return out
}

// castCostReadsAllTargeted reports whether this cast's COST depends on the
// AllTargeted$ union over the root/sub-ability chain (alltargeted1), which is
// the only reason the cast flow pre-asks the chain's targets: CR 601.2b/601.2f
// determine the cost AFTER targets are chosen, so a cost head naming
// AllTargeted$ cannot be evaluated until the sub targets exist.
//
// Two cost sites can carry such a head: the root SA's own ReduceCost$ (Wayta,
// Trainer Prodigy's `ReduceCost$ X` -> `Count$Compare Y EQ2.2.0` ->
// `Y:AllTargeted$Valid Creature.YouCtrl`) and a dynamic
// CollectEvidence<NAME> amount (Urgent Necropsy's `X:AllTargeted$CardManaCost`).
// Both are resolved through the source's SVar table, so the scan expands SVar
// references transitively (bounded, so a cyclic table cannot spin). Raft
// Security Officer carries the third corpus AllTargeted$ line but has no
// sub-ability, so the gate holds and collectSubTargetPreAsks finds nothing.
func (e *Engine) castCostReadsAllTargeted(pc *pendingCast, ab *cards.SA) bool {
	if ab == nil {
		return false
	}
	reduce := strings.TrimSpace(ab.Params["ReduceCost"])
	var dyn []string
	for _, part := range pc.cost.Evidence {
		if part.Dyn != "" {
			dyn = append(dyn, part.Dyn)
		}
	}
	if reduce == "" && len(dyn) == 0 {
		return false
	}
	svars := e.castStageSVars(pc)
	if bodyReadsAllTargeted(reduce, svars, 0) {
		return true
	}
	for _, name := range dyn {
		if bodyReadsAllTargeted(name, svars, 0) {
			return true
		}
	}
	return false
}

// castStageSVars returns the SVar table the cast's cost heads resolve
// against: an activation reads the merged pile's table, a spell its face's.
// The same split evidenceAmount and ownReduceCost apply.
func (e *Engine) castStageSVars(pc *pendingCast) map[string]string {
	o := e.G.Obj(pc.card)
	if o == nil {
		return nil
	}
	if pc.isAbility() {
		return e.pileSVars(pc.card, pc.abilityMerged)
	}
	if f := o.Face(); f != nil {
		return f.SVars
	}
	return nil
}

// bodyReadsAllTargeted reports whether v, or any SVar body it reaches, names
// the AllTargeted$ reference. v is either a literal count body or an SVar
// name; every identifier-shaped word in an expanded body is followed too
// (Wayta's Count$Compare names its Y operand as a bare word). depth bounds
// the walk so a self- or mutually-referential SVar table terminates. The scan
// walks a SLICE of words and only LOOKS UP svars, so no map iteration order
// can reach the result.
func bodyReadsAllTargeted(v string, svars map[string]string, depth int) bool {
	return bodyReadsRef(v, svars, depth, func(s string) bool {
		return strings.Contains(s, "AllTargeted")
	})
}

// bodyReadsRootTarget reports whether v, or any SVar body it reaches, reads a
// ROOT-target reference (Targeted$ / ParentTarget$ / ThisTargetedCard$ -- the
// names refTargets binds to Ctx.Targets, the ability's OWN chosen targets).
// The AllTargeted$ union is deliberately excluded: it is the sub-ability
// pre-ask's shape (alltargeted1), priced only by repriceForTargets, and this
// predicate arms the offer-time potential-target read for an equip cost
// reduction (CR 702.6), never that union. bodyReadsRef's shared walk means a
// body can never be detected by one predicate and missed by the other's
// ordering; the two only differ in which ref names they accept.
func bodyReadsRootTarget(v string, svars map[string]string, depth int) bool {
	return bodyReadsRef(v, svars, depth, func(s string) bool {
		if strings.Contains(s, "AllTargeted") {
			return false
		}
		return strings.Contains(s, "Targeted$") ||
			strings.Contains(s, "ParentTarget$") ||
			strings.Contains(s, "ThisTargetedCard$")
	})
}

// bodyReadsRef is the shared transitive SVar/word walk both ref predicates
// use. match decides whether a single expanded body names the ref; the walk
// still follows SVar references and identifier-shaped bare words so a ref
// reached only through an indirection (Count$Compare's Y operand) is found.
func bodyReadsRef(v string, svars map[string]string, depth int, match func(string) bool) bool {
	v = strings.TrimSpace(v)
	if v == "" || depth > 4 {
		return false
	}
	if match(v) {
		return true
	}
	if len(svars) == 0 {
		return false
	}
	if b, ok := svars[v]; ok && bodyReadsRef(b, svars, depth+1, match) {
		return true
	}
	for _, w := range strings.FieldsFunc(v, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_'
	}) {
		if w == v {
			continue
		}
		if b, ok := svars[w]; ok && bodyReadsRef(b, svars, depth+1, match) {
			return true
		}
	}
	return false
}

// postTargetAsks poses the next outstanding CR 601.2c announcement ask AFTER
// the root target stage: the sub-ability chain pre-ask first (alltargeted1),
// then the CollectEvidence amount ask (whose X reads the union the sub
// answers complete). It returns true when it parked the flow on a decision;
// false means every post-target stage is settled and the caller pays.
func (e *Engine) postTargetAsks(pc *pendingCast) bool {
	// The flow is now past the 601.2c ROOT target stage (answered or not
	// asked): mark it so a later continueCast re-entry -- the CollectEvidence
	// answer's own -- does not re-pose the root ask (targetAsk's
	// passedTarget guard). payCast sets the same flag again; idempotent.
	pc.passedTarget = true
	if pc.faceDown {
		// Morph family (CR 708.4): a face-down spell announces no sub targets
		// either -- its printed chain does not exist while face down.
		return false
	}
	if e.subTargetAsk(pc) {
		return true
	}
	return e.evidenceAsk()
}

// subTargetAsk poses the next un-answered chain sub's target ask, collecting
// the chain once (lazily, on the first call after the root stage). The bounds,
// the CR 115.5 self-exclusion and the option shape mirror targetAsk's; a
// TargetUnique$ sub excludes every already-chosen target (root answers and
// earlier sub answers) exactly as the mid-resolution path's accumulator does.
// A Min-0 sub with no legal candidate is recorded as an ANSWERED EMPTY set
// (Requirement N2: nobody could answer differently) and the walk advances; a
// mandatory one aborts the proposal (CR 733.1, the target stage's own rule --
// nothing has been paid yet). The answers ride the ordinary KTarget flow
// (handleTarget's cast_sub branch), so replay re-derives them like any other
// cast-flow answer.
func (e *Engine) subTargetAsk(pc *pendingCast) bool {
	if !pc.subCollected {
		pc.subCollected = true
		var root *cards.SA
		if o := e.G.Obj(pc.card); o != nil {
			if pc.isAbility() {
				root = e.pcAbility(pc)
			} else if f := o.Face(); f != nil {
				root = e.castStageSA(pc, o, f)
			}
		}
		// Fuse halves use separate target slices and resolution frames; a
		// stage-0 chain walk here cannot attribute the other half's subs.
		//
		// alltargeted1 SCOPE GATE: the chain pre-ask runs ONLY for a cast
		// whose own COST reads the AllTargeted$ union (castCostReadsAllTargeted
		// -- Wayta's ReduceCost$ and Urgent Necropsy's CollectEvidence<X>, the
		// whole corpus carrier set). General CR 601.2c sub-ability
		// pre-announcement for every chain is a separate, far wider change
		// (it moves the decision sequence of every spell with a targeting
		// sub-ability, and with it the chain heads and golden replays); it is
		// ticket agent-20260920T104356Z-c898602e's, not this row's. Outside
		// the gate a sub keeps its mid-resolution ask exactly as before.
		if pc.mode != "fuse" && e.castCostReadsAllTargeted(pc, root) {
			pc.subAsks = e.collectSubTargetPreAsks(root)
		}
		pc.subAns = make([][]state.Target, len(pc.subAsks))
	}
	for pc.subStage < len(pc.subAsks) {
		sub := pc.subAsks[pc.subStage]
		var excludeSelf state.ObjID
		if !pc.isAbility() || sub.API == "Attach" {
			excludeSelf = pc.card
		}
		candidates := e.legalTargetCandidates(pc.player, pc.card, excludeSelf, sub)
		if effects.TargetUniqueRequested(sub) {
			chosen := append([]state.Target(nil), pc.targets...)
			for i := 0; i < pc.subStage; i++ {
				if effects.TargetUniqueRequested(pc.subAsks[i]) {
					chosen = append(chosen, pc.subAns[i]...)
				}
			}
			filtered := candidates[:0]
			for _, cand := range candidates {
				t := state.Target{Obj: cand.obj, Player: cand.player, IsPlayer: cand.kind == "player"}
				if len(effects.TargetUniqueFilter(sub, []state.Target{t}, chosen)) != 0 {
					filtered = append(filtered, cand)
				}
			}
			candidates = filtered
		}
		min, max := e.resolvedTargetBounds(pc.player, pc.card, sub, pc.x)
		if min > 0 && len(candidates) < min {
			e.abortCast(pc, "cast aborted: no legal target for a chained ability", true)
			return true
		}
		if min == 0 && len(candidates) == 0 {
			pc.subAns[pc.subStage] = []state.Target{}
			pc.subStage++
			continue
		}
		if max == 0 {
			// A dynamic bound RESOLVED to zero: this stage takes no targets,
			// recorded as an ANSWERED EMPTY set exactly as the N2 arm above.
			pc.subAns[pc.subStage] = []state.Target{}
			pc.subStage++
			continue
		}
		d := &decision.Decision{Player: pc.player, Kind: decision.KTarget, Min: min, Max: max,
			Prompt: "Choose a target for " + e.targetName(pc.card) + "'s chained ability",
			Source: excludeSelf, ResumeKind: "cast_sub"}
		pickOwed := false
		if who, ok, pick := e.targetAskChooser(pc.player, pc.card, sub); pick {
			pickOwed = true
		} else if ok {
			d.Player = who
		}
		for _, candidate := range candidates {
			label := e.targetOptionLabel(candidate)
			o := decision.Option{Index: len(d.Options), Kind: candidate.kind,
				Label: label, Obj: candidate.obj, Player: candidate.player}
			o.Group = e.targetControllerGroup(sub, candidate)
			o.Controller = e.candidateControllerSeat(candidate)
			d.Options = append(d.Options, o)
		}
		if pickOwed {
			// The multi-opponent Opponent form: the controller names WHICH
			// opponent answers, then the sub's target ask is re-posed.
			e.poseOpponentPick(pc.player, pc.card, sub, oppPickCastSub)
			return true
		}
		e.ask(d)
		return true
	}
	return false
}

// answerCastSubTarget records a cast_sub KTarget answer against the stage it
// was asked for and re-prices (the union grew, so a target-dependent
// ReduceCost$ may now apply -- the net form is idempotent, which matters
// because repriceForTargets already ran on the root answer).
func (e *Engine) answerCastSubTarget(pc *pendingCast, chosen []decision.Option) {
	if pc.subStage >= len(pc.subAsks) {
		return
	}
	pc.subAns[pc.subStage] = targetOptions(chosen)
	pc.subStage++
	e.repriceForTargets(pc)
}

// installSubPreAsk publishes the answered sub-ask record onto the stack
// object that will resolve the chain (spells: pushed by pushCast, so the id
// exists at payCast entry; abilities: minted by payCast's AbilityPush, so
// the ability arm installs after it). The record is Engine.castSubTargets;
// resolution attaches it through Ctx.SubPreAsk and chosenTargetsFor consumes
// it line by line. Empty slices are real answered-zero records and must
// install too, or the resolution would re-ask the sub.
func (e *Engine) installSubPreAsk(pc *pendingCast) {
	if pc.stackObj == 0 || len(pc.subAsks) == 0 {
		return
	}
	m := make(map[string][]state.Target, len(pc.subAsks))
	for i, sa := range pc.subAsks {
		var ts []state.Target
		if i < len(pc.subAns) {
			ts = pc.subAns[i]
		}
		if ts == nil {
			ts = []state.Target{}
		}
		m[sa.Line] = ts
	}
	if e.castSubTargets == nil {
		e.castSubTargets = make(map[state.ObjID]map[string][]state.Target)
	}
	e.castSubTargets[pc.stackObj] = m
}

// evidenceAsk poses the CollectEvidence payment (alltargeted1): the payer
// exiles cards from their OWN graveyard whose total mana value reaches the
// resolved amount (CR 701.30b, the same action the Ward evidence payment
// performs). The amount is resolved once, here, against the settled target
// union -- the corpus carrier's SVar reads AllTargeted$CardManaCost, which is
// exactly why this stage runs after the sub pre-asks. The ask offers the
// graveyard ordered by mana value DESCENDING (ties by object id, so the
// order is deterministic) with Min at the greedy minimum card count, so the
// deterministic bot's first-Min answer always reaches the amount and the
// settlement validation below never rejects it; a human may pick any
// combination of at least Min cards. An answer whose total falls short is
// rejected and the ask re-posed (the same settle-validate shape the Ward
// evidence payment uses); a graveyard that cannot reach the amount at all
// aborts the proposal (CR 733.1 -- nothing has been paid yet).
func (e *Engine) evidenceAsk() bool {
	pc := e.cast
	if pc == nil || len(pc.cost.Evidence) == 0 || pc.evidenceSettled {
		return false
	}
	if !pc.evidenceResolved {
		pc.evidenceN = e.evidenceAmount(pc)
		pc.evidenceResolved = true
	}
	if pc.evidenceN <= 0 {
		// Nothing owed: a zero-target cast, or a body the count evaluator
		// cannot resolve (its degrade-to-zero convention -- the evidence is
		// never silently over-charged).
		pc.evidenceSettled = true
		return false
	}
	if len(pc.evidence) > 0 {
		valid := true
		for _, id := range pc.evidence {
			if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard || o.Owner != pc.player {
				valid = false
				break
			}
		}
		if valid && e.graveyardManaValue(pc.player, pc.evidence) >= pc.evidenceN {
			pc.evidenceSettled = true
			return false
		}
		pc.evidence = nil
		e.emit(events.Event{Kind: events.Note, Player: pc.player,
			Text: "evidence selection's total mana value is too low; choose again"})
	}
	var candidates []state.ObjID
	for _, id := range e.G.Zone(state.ZGraveyard, pc.player) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil {
			candidates = append(candidates, id)
		}
	}
	if e.graveyardManaValue(pc.player, candidates) < pc.evidenceN {
		e.abortCast(pc, "evidence cost no longer payable; cast aborted", true)
		return true
	}
	// Mana value descending, ties by object id: the option order makes the
	// greedy minimum achievable by the FIRST Min options, which is what both
	// the deterministic bot's generic KChoose arm and Clamp's top-up take.
	ids := append([]state.ObjID(nil), candidates...)
	sort.Slice(ids, func(i, j int) bool {
		mi, mj := e.G.Obj(ids[i]).Face().Cmc(), e.G.Obj(ids[j]).Face().Cmc()
		if mi != mj {
			return mi > mj
		}
		return ids[i] < ids[j]
	})
	need := pc.evidenceN
	min := 0
	for _, id := range ids {
		if need <= 0 {
			break
		}
		need -= e.G.Obj(id).Face().Cmc()
		min++
	}
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: min, Max: len(ids),
		Prompt: "Exile evidence with total mana value " + strconv.Itoa(int(pc.evidenceN)) +
			" to cast " + e.targetName(pc.card), Source: pc.card}
	for _, id := range ids {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "evidence",
			Obj: id, Label: e.G.Obj(id).Face().Name, Value: int(e.G.Obj(id).Face().Cmc())})
	}
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// evidenceAmount resolves the CollectEvidence amount against the settled
// target union. A literal part prices itself; a named part resolves through
// the source face's SVar table via effects.EvalCountOK -- the same resolver
// ownReduceCost uses -- with Ctx.AllTargets bound so an AllTargeted$ body
// reads the whole chain union. A body the evaluator cannot resolve
// contributes 0 (the degrade convention); the total is clamped at 0.
func (e *Engine) evidenceAmount(pc *pendingCast) int32 {
	o := e.G.Obj(pc.card)
	if o == nil {
		return 0
	}
	var svars map[string]string
	if pc.isAbility() {
		svars = e.pileSVars(pc.card, pc.abilityMerged)
	} else if f := o.Face(); f != nil {
		svars = f.SVars
	}
	total := int32(0)
	for _, part := range pc.cost.Evidence {
		if part.Dyn == "" {
			total = addClampedGeneric(total, int64(part.N))
			continue
		}
		body, ok := svars[part.Dyn]
		if !ok {
			continue
		}
		ctx := &effects.Ctx{Source: pc.card, Controller: pc.player, SVars: svars,
			Targets: pc.targets, AllTargets: pc.allTargets()}
		if n, ok := effects.EvalCountOK(e, ctx, body); ok && n > 0 {
			total = addClampedGeneric(total, int64(n))
		}
	}
	return total
}

// graveyardManaValue sums the mana values of the named cards (the Ward
// evidence payment's wardManaValue read, over a caller-built list).
func (e *Engine) graveyardManaValue(p state.PlayerID, ids []state.ObjID) int32 {
	var n int32
	for _, id := range ids {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Owner == p {
			n = addClampedGeneric(n, int64(o.Face().Cmc()))
		}
	}
	return n
}

// activationPushEvent names the replayable activation boundary for both
// printed and keyword-granted abilities. Use it for spend riders too: a
// synthetic AbilityPush with ability=-1 cannot describe a granted body.
//
// A K:Ninjutsu activation's captured defender (pc.ninjutsuDefender) rides the
// event's IDs: events.Apply's AbilityPush arm decodes a PlayerRef sentinel
// into the ability object's Remembered, and rules/stack.go re-binds it to the
// resolving Ctx's DefendingPlayer so the ChangeZone body's Attacking$ True
// rider places the permanent attacking the returned creature's defender.
func (pc *pendingCast) activationPushEvent(e *Engine) events.Event {
	if pc.grantKeyword != "" {
		return events.Event{Kind: events.KeywordAbilityPush, Player: pc.player,
			Obj: pc.card, Counter: pc.grantKeyword}
	}
	ev := events.Event{Kind: events.AbilityPush, Obj: pc.card,
		Player: pc.player, Amount: int32(pc.ability)}
	if e.activationIsNinjutsu(pc) && pc.ninjutsuHasDefender {
		ev.IDs = []state.ObjID{state.PlayerRef(pc.ninjutsuDefender)}
	}
	return ev
}

// activationIsNinjutsu reports whether pc is a K:Ninjutsu activation: the
// activated ability's expansion (cards/kw_ninjutsu.go) stamps Keyword$
// Ninjutsu, the same tag rules/statics.go's abilityConstraintMatches reads.
// Only a printed face-ability activation carries the tag, so pc.ability alone
// resolves the SA; a stale index or a granted body is not ninjutsu.
func (e *Engine) activationIsNinjutsu(pc *pendingCast) bool {
	if pc == nil || pc.ability < 0 || pc.grantKeyword != "" || pc.gainedFrom != 0 {
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil {
		return false
	}
	pa, ok := o.PileAbilityAt(pc.ability)
	if !ok || pa.SA == nil {
		return false
	}
	return saHasKeyword(pa.SA, "Ninjutsu")
}

// saHasKeyword reports whether ab's Keyword$ tag (a comma list set by a
// keyword expansion, cards/keywords.go) contains want. It is the same tag read
// rules/statics.go's abilityConstraintMatches uses to recognise an
// Equip/Ninjutsu/Cycling ability, factored out so the offer and resolution
// halves cannot disagree about which keyword an ability belongs to.
func saHasKeyword(ab *cards.SA, want string) bool {
	if ab == nil {
		return false
	}
	for kw := range strings.SplitSeq(ab.Params["Keyword"], ",") {
		if strings.EqualFold(strings.TrimSpace(kw), want) {
			return true
		}
	}
	return false
}

// finishTargetedCast is the completion tail every cast-flow target answer
// converges on once no post-target ask is outstanding. payCast records an
// ability's root targets once its stack object is minted (possibly after a
// mana window); a spell's targets were already recorded before the park.
// The tail carries the CR 117.3c priority discipline the root arm
// always owned: the caster keeps priority only once no announcement decision
// is outstanding, and a trigger drain parked on the target ask resumes
// through its own continuation.
func (e *Engine) finishTargetedCast(pc *pendingCast, player state.PlayerID) {
	e.payCast()
	// payCast closes the proposal after creating the stack object (and has
	// already recorded an ability's root targets, on either the immediate pay
	// path or a resumed payCast). Keep its completed target bindings available
	// while the deferred spend rider matches, then close it again before
	// control returns to the host. A proposal still parked in the 601.2g mana
	// window has minted no stack object yet, so the guard also keeps the
	// dispatch off a suspended payment.
	if pc.isAbility() && pc.stackObj != 0 {
		e.cast = pc
		e.fireManaSpentTriggers(pc.activationPushEvent(e), nil)
		e.cast = nil
	}
	if e.drainAwaitsTarget {
		e.drainAwaitsTarget = false
		e.resumeTriggerDrain()
	} else if e.pending == nil {
		e.emit(events.Event{Kind: events.Priority, Player: player, Amount: 0})
	}
}

// pushCast implements CR 601.2a: the card reaches the stack BEFORE the
// target choice (601.2c) and payment (601.2h), which is what makes the
// transaction match the CR's ordered list. The cast trigger (601.2i) is held
// back by emit (deferCastTrigger) because it fires only once the spell is
// actually cast -- after payment; payCast's fireDeferredCastTrigger re-walks
// the held PutOnStack. CastInfo (the X / mode-flag recording) is deferred to
// payCast too, so an aborted proposal leaves no cast-time trace on the card.
// An activated ability is a no-op here: CR 602.2b imports 601.2 but its
// stack object is minted by payCast's AbilityPush AFTER the target and cost
// settle, and an aborted activation must reverse with no stack object left
// behind (CR 733.1) -- pushing it first would strand a Face-less object in
// exile. A land play never goes on the stack.
//
// It returns true when the cast cannot proceed at all (the card left its
// zone before the push) and was aborted; in that case nothing was pushed, so
// no reversal is owed.
func (e *Engine) pushCast() bool {
	pc := e.cast
	if pc == nil || pc.mode == "land" || pc.mode == "suspend" || pc.mode == "plot" || pc.isAbility() {
		return false
	}
	if pc.pushed {
		// The object is already on the stack (a mana-window resume re-enters
		// continueCast); do not push it a second time.
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil || o.Zone != pc.from {
		e.cast, e.choosing = nil, chooseNone
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Text: "cast aborted: the card moved"})
		return true
	}
	// Capture the held-out suppression set before the push, so an aborted
	// (reversed) cast can restore it -- the push below is a state-changing
	// event that emit treats as progress and clears the set, yet an aborted
	// cast is net no progress.
	pc.preSuppress = e.suppressedCast
	pc.preAborts = e.castAborts
	// CR 601.2a: the spell reaches the stack. The cast trigger is held back
	// (deferCastTrigger) so it cannot fire before the spell is paid for.
	e.deferCastTrigger = true
	ev := events.Event{Kind: events.PutOnStack, Obj: pc.card, Player: pc.player, From: pc.from, To: state.ZStack, Text: o.Face().Name}
	if pc.faceDown {
		// Morph family (CR 708.4): the face-down spell's identity is hidden
		// while it sits on the stack. The face-down entry marker rides the
		// Counter so events.Apply folds Object.FaceDown onto the stack
		// object (the view then redacts the card for everyone but the
		// caster, and moveResolvedOffStack re-carries the marker onto the
		// battlefield entry), and Secret keeps the printed name out of the
		// other seats' event projections -- a non-Secret PutOnStack would
		// name the card in Text to every viewer. The cloak marker is the
		// Disguise entry's carrier (its face-down ward {2} rides the same
		// state bit the Cloak machinery reads). No ordinary cast carries a
		// Counter here, so unrelated casts are byte-identical.
		ev.Counter = events.FaceDownEntryCounter
		if pc.mode == "disguised" {
			ev.Counter = events.CloakEntryCounter
		}
		ev.Secret = true
	}
	e.emit(ev)
	e.deferCastTrigger = false
	// CR 722.3c: the prepared permanent loses its designation "at the time
	// the spell becomes cast" (CR 601.2i) -- here, as the copy reaches the
	// stack, never on resolution. Apply's fold clears Object.Prepared, so the
	// copy is no longer offered; the orphaned exile copy is not re-offered
	// because its source no longer answers Prepared.
	if pc.mode == "prepared_copy" {
		if cp := e.G.Obj(pc.card); cp != nil && cp.PreparedSource != 0 {
			e.emit(events.Event{Kind: events.AlterAttribute, Obj: cp.PreparedSource,
				Text: "Prepared", Amount: -1})
		}
	}
	// CR 601.2a: the player who cast the spell is its controller. A card
	// another seat controlled (Rashmi and Ragavan's exiled OPPONENT card,
	// Gonti's stolen card, Intellect Devourer's may-play exile) comes under
	// the caster's control the moment it is cast, and the resulting permanent
	// enters the battlefield under the caster's control; an ordinary cast's
	// card already answers to the caster, so no event rides those.
	if o := e.G.Obj(pc.card); o != nil && o.Controller != pc.player {
		e.emit(events.Event{Kind: events.ControlChange, Obj: pc.card, Player: pc.player})
	}
	pc.stackObj = pc.card
	pc.pushed = true
	// CR 702.168: the Gift election was announced before CR 601.2a, so fold
	// it onto the now-existing stack object here -- the target ask that
	// follows reads Count$PromisedGift off it, and events.Move carries the
	// promise across the stack->battlefield move for a permanent's ETB. Only
	// a cast that actually reached the Gift ask emits (Amount 1 for a
	// promise, 0 for a decline); every unrelated cast stays byte-identical.
	if pc.giftDone {
		amt := int32(0)
		if pc.giftPromise {
			amt = 1
		}
		e.emit(events.Event{Kind: events.GiftPromise, Obj: pc.card, Player: pc.giftTo, Amount: amt})
	}
	// CR 903.8: the cast counter increments the INSTANT the spell is put on
	// the stack, never when it resolves -- so a commander spell that is later
	// countered still raises the next cast's tax. Only a cast FROM the
	// command zone counts, and recordCmdCast itself carries the Commander
	// format gate.
	if pc.from == state.ZCommand {
		e.recordCmdCast(pc.player, pc.card)
	}
	return false
}

// recheckIllegal implements CR 601.2e: once every announcement choice (the
// {X} value) is made but before the cost is paid, the game rechecks that the
// proposed spell can legally be cast, considering the characteristics the
// choices changed -- most importantly the mana value with {X} counted at its
// chosen value (CR 202.3e). A CantBeCast restriction that the chosen {X} now
// makes applicable forbids the spell, so the proposal is reversed (CR 733.1)
// and nothing is paid. Only a spell is rechecked: an activated ability's
// legality was fully gated before it was offered, and 202.3e's X-count is a
// spell-mana-value rule. Returns true (and has reversed the proposal) when
// the spell has become illegal.
func (e *Engine) recheckIllegal(pc *pendingCast) bool {
	if pc.isAbility() {
		return false
	}
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return false
	}
	// CR 202.3e: the spell's mana value counts {X} at the chosen value, and
	// is a property of the card's printed mana cost -- never the alternative
	// cost (flashback) it may be paid with. restrictionOnFace evaluates the
	// same continuous-gate + ValidCard grammar the offer's
	// castRestrictedUsing runs, against one face, with that face's mana
	// value: the offer reads the front face for the ordinary cast and, for a
	// fuse cast, BOTH halves through castRestrictedAsFace (CR 709.5), so the
	// recheck must answer with the same both-halves rule or an offered cast
	// would abort here (or a prohibited one would slip through). offerAsFace
	// is the scoped read the offer probes with; for the front face it is a
	// no-op.
	restrictionOnFace := func(face *cards.Face) bool {
		if face == nil {
			return false
		}
		printed := ParseCost(face.ManaCost)
		mv := printed.CMC()
		if printed.X > 0 {
			mv = printed.WithX(pc.x).CMC()
		}
		return e.offerAsFace(pc.card, face, func() bool {
			for _, sv := range e.castRestrictionSources(e.activeStatics("CantBeCast"), pc.card) {
				if !e.actorMatches(sv, "Caster", pc.player) {
					continue
				}
				// The same shared continuous gate castRestrictedUsing runs: the
				// CR 608.2b recheck must answer with the ONE grammar the offer
				// answered with, or a cast offered under a false gate would abort
				// here (and vice versa). It subsumes the checkSVarHolds the caller
				// used to run separately.
				if !e.continuousGateHolds(sv) || !e.restrictionGateHolds(sv, pc.card) {
					continue
				}
				sc := e.specCtx(sv.Source, sv.Controller)
				sc.HasManaValue = true
				sc.ManaValue = mv
				if e.matchesSpec(sv.Params["ValidCard"], pc.card, sc) {
					return true
				}
			}
			return false
		})
	}
	restricted := restrictionOnFace(o.Face())
	if !restricted && pc.mode == "fuse" {
		if _, fa := fusedSplitFaces(o); fa != nil {
			restricted = restrictionOnFace(fa)
		}
	}
	if restricted {
		// suppress=true, not false: an illegal-proposal abort is a
		// no-progress reversal (CR 733.1) exactly like every other abort
		// site, so it rides the same F05-2 (CR 733.2) discipline -- first
		// identical abort retryable, second holds the option out of the
		// window. With false, a seat that re-picks the same X (the only
		// value it knows) re-announces the same illegal spell forever.
		e.abortCast(pc, "cast aborted: proposed spell is illegal (CR 601.2e)", true)
		return true
	}
	// Target-conditional CastWithFlash (task istargeting-flash): a spell
	// announced at a time a sorcery could not have been cast on the strength
	// of a CastWithFlash permission whose ValidSA$ requires targeting
	// something must actually have a qualifying announced target. The
	// permission is otherwise unread after the offer, so without this a cast
	// that took the flash window on a target the grant never covered would
	// still complete. offSorcery is set only by beginCast (the ordinary
	// offered cast: beginPlay's free-cast routes leave it false), and among
	// those it is true only when the face is not an instant, has no Flash and
	// no MayFlashSac rider -- so for a card with a target-conditional grant
	// the ONLY remaining way the offer passed spellTimingOK is that grant.
	// Re-running the same castWithFlashTargets the offer read, now with the
	// announced targets, keeps offer and recheck on ONE interpretation.
	//
	// The mode exclusion covers the two offers that grant their own timing
	// WITHOUT spellTimingOK (MayFlashCost's paid flash and the defeat cast).
	// Every other mode polices the face its timing rested on:
	// flashGrantCoversTargets answers false only when the face's off-sorcery
	// timing could have rested on an IsTargeting-conditional CastWithFlash
	// grant AND the announced targets do not satisfy it, so an unconditional
	// Flash/instant permission is never rejected for a non-qualifying target.
	// split_alt reads the LIVE face: beginCast already flipped to the cast
	// half, so o.Face() IS the half whose grant was judged at the offer
	// (through castWithFlashAsFace) and whose announced targets must satisfy
	// it now. A fuse cast stays at its front face and carries BOTH halves:
	// each non-instant half is judged against its OWN stage's targets through
	// the same face-scoped read (flashGrantCoversTargets scopes the alternate
	// half), never against the other half's grant or the flat target list.
	if pc.offSorcery && pc.mode != "mayflash" && pc.mode != "defeat_cast" {
		if pc.mode == "fuse" {
			if ff, fa := fusedSplitFaces(o); ff != nil {
				for i, half := range []*cards.Face{ff, fa} {
					var ts []state.Target
					if i < len(pc.stageTargets) {
						ts = pc.stageTargets[i]
					}
					if e.flashGrantCoversTargets(pc.player, pc.card, half, ts) {
						continue
					}
					e.abortCast(pc, "cast aborted: flash permission's target requirement unmet (CR 601.2e)", true)
					return true
				}
			}
		} else if f := o.Face(); !e.flashGrantCoversTargets(pc.player, pc.card, f, pc.targets) {
			e.abortCast(pc, "cast aborted: flash permission's target requirement unmet (CR 601.2e)", true)
			return true
		}
	}
	return false
}

// plannedCastPayment is deliberately private continuation state rather than
// an alternate payment engine.  Its plan is deep-copied at Submit and Clone.
type plannedCastPayment struct {
	actionID string
	plan     decision.PaymentPlan
}

// The spec §6 PaymentFallback vocabulary. It is closed: a plan that stops
// names exactly one of these on the manual window the cast returns to.
const (
	paymentFallbackCostChanged       = "cost_changed"
	paymentFallbackSourceChanged     = "source_changed"
	paymentFallbackProductionChanged = "production_changed"
	paymentFallbackChoiceRequired    = "choice_required"
)

// paymentPlanFallback stops automation for the rest of the cast: the witness
// is dropped so no later re-entry can resume it, the reason is recorded for
// the ordinary manual window, and completed activations (and the mana they
// floated) stay exactly as they are -- no rollback, no substitute source.
func (e *Engine) paymentPlanFallback(pc *pendingCast, reason string) {
	if pc == nil || pc.payment == nil {
		return
	}
	pc.paymentFallback = &decision.PaymentFallback{PlanID: pc.payment.plan.ID, Reason: reason}
	e.paymentStats.recordFallback(reason)
	pc.payment = nil
	pc.paymentNext = 0
}

// castPaymentMana is the mana a pending cast's CR 601.2h payment charges: the
// composed total less announced creature contributions (paymentMana), less a
// spell's Delve credit. The mana window prices it, payCast pays it, and a
// payment plan still describes the payment only while its witnessed Cost
// equals it.
func (e *Engine) castPaymentMana(pc *pendingCast) Cost {
	mana := e.paymentMana(pc)
	if !pc.isAbility() {
		mana.Generic -= int32(len(pc.delve))
		if mana.Generic < 0 {
			mana.Generic = 0
		}
	}
	return mana
}

// paymentPlanCheck revalidates the selected witness against the pending cast
// before each planned activation and once more after the last (spec §6): the
// resolved mana cost, the remaining steps' life + damage against the caster's
// life, the global gates, every remaining step's source and exact alternative
// (consequence included), and that the floating pool plus the remaining
// production still settles exactly as witnessed. ValidateCastPayment checked the same
// witness at Submit while the card was in hand; this reads the cast after its
// announcements (targets, sacrifices, discards, delve), so a delve exile or a
// convoked creature that changed the mana to pay is a cost change. It is a
// pure read and returns the fallback reason, or "" while the rest of the plan
// is exactly executable.
//
// The global gates are the offer's. The pool gate reads the floating pool the
// plan starts from, so it applies before the first activation only: after
// that the plan's own production (a snow land's, a rock's typed mana)
// legitimately floats. The interference gate applies before every later
// activation. Submit ran it for the first, and a change between Submit and
// this window surfaces in that first activation's actual production or its
// interruption, both of which the executor checks.
func (e *Engine) paymentPlanCheck(pc *pendingCast) string {
	reason, _ := e.paymentPlanCheckUnits(pc)
	return reason
}

// paymentPlanCheckUnits is paymentPlanCheck also returning the source census
// it validated against (nil on an early return, which never reads one), so
// the executor's immediately following step resolution reuses it rather
// than taking the identical census again at the unchanged state.
func (e *Engine) paymentPlanCheckUnits(pc *pendingCast) (string, []windowManaUnit) {
	// A pure read: one zone-entry index serves every remaining step.
	defer e.paymentPlanQueryScope()()
	plan := pc.payment.plan
	cost := e.castPaymentMana(pc)
	if plan.Version != decision.PaymentPlanV1 || plan.Cost != paymentCost(cost) {
		return paymentFallbackCostChanged, nil
	}
	next := pc.paymentNext
	// The lethal guard, re-read before every step (spec §6): the remaining
	// steps' disclosed life + damage must still leave the caster alive. A
	// life total lowered since the offer (or by an earlier step's replaced
	// damage) stops the plan before the next activation. Each remaining
	// step's consequence itself is re-derived with its alternative below
	// (paymentPlanStepReady): a changed one is production_changed.
	if pain := paymentPlanRemainingPain(plan, next); pain > 0 && pain >= int64(e.G.Players[pc.player].Life) {
		return paymentFallbackCostChanged, nil
	}
	if next == 0 && !paymentPlanPoolOK(e.G.Players[pc.player]) {
		return paymentFallbackProductionChanged, nil
	}
	if next > 0 && next < len(plan.Activations) && e.paymentPlanManaInterference() {
		return paymentFallbackProductionChanged, nil
	}
	units := e.paymentPlanManaUnits(pc.player)
	pool := e.G.Players[pc.player].Pool
	seen := make(map[state.ObjID]bool, len(plan.Activations))
	for i, pa := range plan.Activations {
		if seen[pa.Source] {
			return paymentFallbackSourceChanged, units
		}
		seen[pa.Source] = true
		if i < next {
			continue // completed: its checked production already floats.
		}
		step, reason := e.paymentPlanStepReady(pc.player, units, pa)
		if reason != "" {
			return reason, units
		}
		pool = manaAdd(pool, step.mana)
	}
	payment, ok := cost.resolveManaWith(pool, state.Mana{}, [7]state.Mana{}, e.G.Players[pc.player].Life, false, pipRider{}, nil)
	if !ok || paymentManaAmount(payment.pool) != plan.PoolAfter {
		return paymentFallbackProductionChanged, units
	}
	return "", units
}

// paymentPlanStepReady resolves one remaining witness step to the exact
// alternative it names (paymentPlanStepAlternative), or says why it no longer
// can. The source itself changed -- gone, another incarnation, tapped, phased
// out, another controller, or no V1 mana ability left under the step's
// identity -- is source_changed; the same untapped source still offering the
// step's ability identity, but not the witnessed production, is
// production_changed. It never names a substitute.
func (e *Engine) paymentPlanStepReady(p state.PlayerID, units []windowManaUnit, pa decision.PaymentActivation) (plannedManaActivation, string) {
	o := e.G.Obj(pa.Source)
	if o == nil || o.Zone != state.ZBattlefield || o.Tapped || o.PhasedOut || o.Controller != p ||
		pa.SourceZoneSeq != e.paymentSourceZoneSeq(pa.Source) {
		return plannedManaActivation{}, paymentFallbackSourceChanged
	}
	if step, ok := e.paymentPlanStepAlternative(units, pa); ok {
		return step, ""
	}
	for _, u := range units {
		if u.id != pa.Source {
			continue
		}
		for _, alt := range e.paymentPlanUnitAlternatives(u) {
			if alt.activation.Ability == pa.Ability {
				return plannedManaActivation{}, paymentFallbackProductionChanged
			}
		}
	}
	return plannedManaActivation{}, paymentFallbackSourceChanged
}

// paymentPlanProducedExactly reports whether the mana p's pool gained from
// event index from onward is exactly want, with nothing taken out: one
// planned activation's actual production against its witness step. Every
// ManaAdd counter form (plain, snow, typed) is read into its pool slot.
func (e *Engine) paymentPlanProducedExactly(p state.PlayerID, from int, want decision.ManaAmount) bool {
	var added state.Mana
	for _, ev := range e.L.Events[from:] {
		if ev.Kind != events.ManaAdd || ev.Player != p {
			continue
		}
		if ev.Amount < 0 {
			return false
		}
		added[state.ManaSlot(ev.Counter)] += ev.Amount
	}
	return paymentManaAmount(added) == want
}

// executePlannedManaActivation runs the witness's next step through the
// ordinary mana ability path, then checks what actually happened (spec §6:
// "after each activation, check actual production and outstanding
// continuation; do not continue blindly"). It reports true when the cast has
// moved on without the caller: the activation suspended on a real decision,
// or the continuation it resumed posed the next ask, settled the cast or
// reversed it. It reports false only when nothing was activated because the
// step no longer holds; the fallback is recorded and the caller continues
// into the ordinary manual window. It never substitutes a source.
func (e *Engine) executePlannedManaActivation(pc *pendingCast) bool {
	return e.executePlannedManaActivationUnits(pc, nil)
}

// executePlannedManaActivationUnits is executePlannedManaActivation over a
// source census the caller took at this exact state (paymentPlanCheckUnits,
// with nothing run in between); nil takes a fresh one.
func (e *Engine) executePlannedManaActivationUnits(pc *pendingCast, units []windowManaUnit) bool {
	pa := pc.payment.plan.Activations[pc.paymentNext]
	// Activate the exact alternative the step names -- the one whose ability
	// identity AND production equal the witness -- never the first ability
	// sharing the identity: a dual land's intrinsic {U} and {R} abilities are
	// both {intrinsic, basic_land}, and a step asking it for {R} must not
	// activate its {U} ability.
	if units == nil {
		units = e.paymentPlanManaUnits(pc.player)
	} else if walkCacheVerify && !paymentPlanSameUnits(units, e.paymentPlanManaUnits(pc.player)) {
		panic("payment plan executor: the checked source census is stale")
	}
	step, reason := e.paymentPlanStepReady(pc.player, units, pa)
	if reason != "" {
		e.paymentPlanFallback(pc, reason)
		return false
	}
	ma := step.ma
	if pc.announced {
		// An Auto-fill step of an announced window is an in-window
		// activation too: Undo last tap and Cancel cast may reverse it.
		e.beginWindowTap(pc, pa.Source, step.tier == paymentTierNormal)
	}
	pc.paymentNext++ // a synchronous continuation may re-enter payCast.
	// The planner already resolved the exact ability to run: the original for
	// fixed production, or a withProduced copy carrying the selected colour
	// for Any/Combo/Chosen/ColorIdentity. Resolve step.exec while retaining
	// step.ma as the compiled original for activation limits and replay, so no
	// colour prompt is posed at execution.
	exec := step.exec
	mark := len(e.L.Events)
	// This is a spell's CR 601.2g payment window: the call the manual
	// "activate" answer makes (activateManaPayment), never the distinct
	// cumulative/triggered-cost window, which is not open during a cast.
	e.resolveManaAbilityRefOriginal(pc.player, pa.Source, exec, ma,
		e.gainedManaRefFor(pc.player, pa.Source, ma), true, false, true)
	if e.pending != nil {
		// The activation posed a real decision (a replacement's colour
		// choice, say). Keep what completed, cancel the remaining steps, and
		// let that decision's answer resume the cast manually. Only a pending
		// decision is an interruption: e.choosing can still name the
		// cast-time ask answered before this window (a sacrifice, discard or
		// delve pick) and says nothing about this activation.
		e.paymentPlanFallback(pc, paymentFallbackChoiceRequired)
		return true
	}
	if e.cast != pc {
		return true // the activation itself settled or reversed the cast.
	}
	if !e.paymentPlanProducedExactly(pc.player, mark, pa.Produces) {
		// The source produced something other than its step: stop before
		// any further planned source; the manual window names the reason.
		e.paymentPlanFallback(pc, paymentFallbackProductionChanged)
	}
	// Ordinary manually selected mana is resumed by the answer handler.  A
	// payment-plan activation is selected internally, so resume the cast here
	// to re-validate and run the next step, settle the funded cost, or pose
	// the manual window after a fallback.
	e.continueCast()
	return true
}

// manaWindowAsk implements CR 601.2g: if the total cost includes a mana
// payment, the player gets a chance to activate mana abilities before paying
// (601.2h). The engine poses a mid-cast KChoose window -- one "activate"
// option per untapped mana-ability source the player controls, then a "done"
// option -- only when the pool alone cannot pay the resolved total cost and
// at least one such source is untapped; a caster who already has the mana, or
// has no untapped source, has nothing a window could enable, so payCast
// proceeds straight to payment. "The pool alone" is literal here: a {B} pip
// the payer's PayLifeInsteadOf:B grant could settle with 2 life is not
// counted as paid, so the window opens and the mana-or-life choice is the
// payer's (the grant is suspended only for this gate; the payment still
// spends the life when the payer answers "done"). Answering routes
// through castAnswer
// (chooseCast): "activate" taps the source and resolves its mana abilities
// (a tap consumes it, so it is not re-offered) and continueCast re-enters
// payCast to re-price the window; "done" sets windowDone so payCast pays.
//
// A selected payment plan runs here first, one step per re-entry, each step
// revalidated before it runs (paymentPlanCheck). It returns true only when a
// decision is pending or the cast has settled or reversed; a plan that stops
// falls through to this ordinary window, which then carries its
// PaymentFallback.
func (e *Engine) manaWindowAsk() bool {
	pc := e.cast
	if pc == nil || pc.windowDone {
		return false
	}
	if pc.payment != nil {
		if reason, units := e.paymentPlanCheckUnits(pc); reason != "" {
			e.paymentPlanFallback(pc, reason)
		} else if pc.paymentNext < len(pc.payment.plan.Activations) && e.executePlannedManaActivationUnits(pc, units) {
			return true
		}
	}
	mana := e.castPaymentMana(pc)
	if !mana.hasManaPayment() {
		return false
	}
	// A pool that already pays the total cost needs no window (nothing to
	// gain by activating more mana abilities here). The descriptor carries
	// the announced-X marker so a CostContainsX batch sees the X payment.
	// The check deliberately suspends the payer's PayLifeInsteadOf:B grant
	// (costPayableClassLife's lifeGrant false): a {B} pip K'rrik could settle
	// with 2 life is NOT "already paid" by the pool, so the window must open
	// and let the payer choose an untapped source over the life. Answering
	// "done" still spends the life through the ordinary payment, which keeps
	// the grant.
	if e.costPayableClassLife(pc.player, paymentForCast(pc, mana),
		pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}, mana, false) {
		return false
	}
	if pc.announced {
		// Announce then pay: the "select mana" window, posed even with no
		// untapped source left so the caster can undo or cancel. A plan
		// (Auto-fill) that ran every step yet left the pool short falls back
		// into it with its reason, as a planned cast does.
		if pc.payment != nil {
			reason := e.paymentPlanCheck(pc)
			if reason == "" {
				reason = paymentFallbackProductionChanged
			}
			e.paymentPlanFallback(pc, reason)
		}
		return e.announcedManaWindowAsk(pc, mana)
	}
	var sources []state.ObjID
	for _, id := range e.manaSourceIDs(pc.player) {
		if !e.convokeCommitted(pc, id) && e.untappedManaSource(pc.player, id) {
			sources = append(sources, id)
		}
	}
	if len(sources) == 0 {
		return false
	}
	if pc.payment != nil {
		// Every step ran and checked, yet the pool cannot pay: the manual
		// window never appears during a planned cast without saying why.
		reason := e.paymentPlanCheck(pc)
		if reason == "" {
			reason = paymentFallbackProductionChanged
		}
		e.paymentPlanFallback(pc, reason)
	}
	name := e.G.Obj(pc.card).Face().Name
	d := &decision.Decision{Player: pc.player, Kind: decision.KChoose, Min: 1, Max: 1,
		Prompt: "Activate mana abilities to pay for " + name, Source: pc.card}
	if pc.paymentFallback != nil {
		f := *pc.paymentFallback
		d.PaymentFallback = &f
	}
	for _, id := range sources {
		opt := decision.Option{Index: len(d.Options), Kind: "activate",
			Obj: id, Label: "Activate " + e.G.Obj(id).Face().Name + " for mana"}
		// The same beyond-tap cost marker legal.go's priority-window offer
		// carries, so the one "activate" option shape stays consistent across
		// both ask sites (fb-led1); this ask sits on a choose decision, which
		// every auto path refuses, so the marker changes no classification.
		if marker := e.manaActivationCostMarker(e.availableManaAbilities(pc.player, id)); marker != "" {
			opt.Cost = marker
		}
		d.Options = append(d.Options, opt)
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "done", Label: "Done"})
	e.choosing = chooseCast
	e.ask(d)
	return true
}

// convokeCommitted reports whether id is already committed to this cast's
// payment: a Convoke/Harmonize/Improvise election (pc.convoke) or a Conspire
// tap election (pc.taps -- the election records the creatures before payCast's
// emitChoiceCosts taps them, so an elected creature must be excluded from the
// mana window and from a later convoke announcement, or it could be activated
// for mana and then tapped a second time).
func (e *Engine) convokeCommitted(pc *pendingCast, id state.ObjID) bool {
	for _, pay := range pc.convoke {
		if pay.id == id {
			return true
		}
	}
	for _, tid := range pc.taps {
		if tid == id {
			return true
		}
	}
	return false
}

// untappedManaSource reports whether id is an untapped permanent under
// the player p's control with at least one unrestricted mana ability. It
// is the PAYMENT-WINDOW gate only -- every call site (the CR 601.2g
// cast window, a ward payment, a cumulative-upkeep payment) runs while
// the payer holds no priority -- so an InstantSpeed$ True mana ability
// (Lion's Eye Diamond, "Activate only as an instant") does not make its
// source eligible: the ability is activatable at priority, never inside
// a payment window (CR 605.4 defers to the ability's own timing
// restriction).
func (e *Engine) untappedManaSource(p state.PlayerID, id state.ObjID) bool {
	for _, ma := range e.availableManaAbilities(p, id) {
		if e.instantSpeedOnly(ma) {
			continue
		}
		return true
	}
	return false
}

// hasUntappedManaSource reports whether p controls ANY untapped permanent
// with a usable mana ability -- the condition under which the 601.2g window
// could supply the mana a pool alone cannot.
func (e *Engine) hasUntappedManaSource(p state.PlayerID) bool {
	for _, id := range e.manaSourceIDs(p) {
		if e.untappedManaSource(p, id) {
			return true
		}
	}
	return false
}

// installPaidCostLists publishes the cards this cast/activation's cost exiled
// (pc.exiles) and revealed (pc.reveals) onto the engine keyed by the stack
// object it just minted, in stable cost order. Resolution loads them into
// effects.Ctx.Exiled/Revealed so the `Exiled$<Property>` /
// `Revealed$<Property>` count refs and `Defined$ Exiled`/`Revealed` read the
// exact cards the cost paid (Forge's SpellAbility.getPaidList rows). It is the
// sacrificedLKI discipline: engine-only scratch (a log-only reconstruction
// rebuilds it because payCast re-executes), cloned with the engine, removed
// with the stack object by the shared MoveZone cleanup. Empty lists are not
// recorded -- an absent entry and an empty one read the same legitimate zero.
func (e *Engine) installPaidCostLists(pc *pendingCast) {
	if pc.stackObj == 0 {
		return
	}
	if len(pc.exiles) > 0 {
		if e.castExiled == nil {
			e.castExiled = make(map[state.ObjID][]state.ObjID)
		}
		e.castExiled[pc.stackObj] = append([]state.ObjID(nil), pc.exiles...)
	}
	if len(pc.reveals) > 0 {
		if e.castRevealed == nil {
			e.castRevealed = make(map[state.ObjID][]state.ObjID)
		}
		e.castRevealed[pc.stackObj] = append([]state.ObjID(nil), pc.reveals...)
	}
}

func (e *Engine) emitChoiceCosts(pc *pendingCast) {
	names := func(ids []state.ObjID) string {
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, e.targetName(id))
		}
		return strings.Join(out, ", ")
	}
	// A whole-hand Reveal part paid with an empty hand (Land Grant with no
	// other cards in hand): nothing else would appear in the log, so emit the
	// empty reveal as its own public note.
	if pc.revealedEmptyHand {
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			Text: "revealed no cards (an empty hand) as a cost"})
	}
	if len(pc.reveals) > 0 {
		// Split the paid list by arm: an announced hand reveal (a plain Reveal
		// card, the REVEAL arm of an either-or cost, or any legacy entry with no
		// arm recorded) is a public reveal; a permanent elected by the CHOOSE
		// arm is a public CHOICE, never a reveal of a hand card. Both ride the
		// same paid list the `Revealed$<Property>` refs read.
		var revealed, chosen []state.ObjID
		for i, id := range pc.reveals {
			if i < len(pc.revealHandArm) && !pc.revealHandArm[i] {
				chosen = append(chosen, id)
			} else {
				revealed = append(revealed, id)
			}
		}
		if len(revealed) > 0 {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
				IDs: append([]state.ObjID(nil), revealed...), Text: "revealed " + names(revealed) + " as a cost"})
		}
		if len(chosen) > 0 {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
				IDs: append([]state.ObjID(nil), chosen...), Text: "chose " + names(chosen) + " as a cost"})
		}
	}
	// RevealChosen<Player>/<Type> parts: the payer's secret designation is
	// made public as the cost is paid. One public Note per part, naming the
	// designation (the chosen player's chain-safe tossName, or the chosen
	// creature type). Nothing is asked -- the choice was made earlier by the
	// Secretly$ True ChoosePlayer/ChooseType.
	for _, part := range pc.cost.RevealChosen {
		if text, ok := revealChosenText(e.G, e.G.Obj(pc.card), part.Spec); ok {
			e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card, Text: text})
		}
	}
	if len(pc.beholds) > 0 {
		e.emit(events.Event{Kind: events.Note, Player: pc.player, Obj: pc.card,
			IDs: append([]state.ObjID(nil), pc.beholds...), Text: "beheld " + names(pc.beholds) + " as a cost"})
		// BeholdExile<N/Spec> parts (CostPart.ThenExile): the beheld objects
		// are exiled as the rest of the same payment. beholdCostAsk records
		// each part's N objects in part order, so the paid list is sliced by
		// part. The MoveZone carries the paying source in IDs, the event-
		// derived ExiledWith provenance the Champion cycle's "return the
		// exiled card to its owner's hand" (Defined$ ExiledWith) reads.
		at := 0
		for _, part := range pc.cost.Behold {
			n := int(part.N)
			if at+n > len(pc.beholds) {
				break
			}
			if part.ThenExile {
				for _, id := range pc.beholds[at : at+n] {
					if o := e.G.Obj(id); o != nil && o.Zone != state.ZExile {
						e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile,
							IDs: []state.ObjID{pc.card}, Text: "exiled as a cost"})
					}
				}
			}
			at += n
		}
	}
	for _, id := range pc.taps {
		e.emit(events.Event{Kind: events.Tap, Obj: id, Text: "tapped as a cost"})
	}
	// CR 702.122: the creatures that paid a Crew ability's tap cost crewed the
	// Vehicle. The crew keyword rides the minted Animate SA as `Keyword$ Crew`
	// (cards/kw_crew.go), so the tag -- not any card name -- is what marks this
	// activation: one Crew event per tapped crewer, pairing it with the source
	// Vehicle (pc.card) for the Creature.CrewedThisTurn filter. The ordinary
	// tapXType costs of other abilities (Mossbridge Troll's regeneration, the
	// {T} cost) carry no Crew tag and record nothing.
	if saHasKeyword(e.pcAbility(pc), "Crew") {
		for _, id := range pc.taps {
			e.emit(events.Event{Kind: events.Crew, Obj: id, Player: pc.player,
				IDs: []state.ObjID{pc.card}})
		}
	}
	for i, id := range pc.blights {
		if i < len(pc.cost.Blight) {
			n := pc.cost.Blight[i].N
			// An announced Blight<X> part's count is the announced X, not the
			// (unused) part.N.
			if pc.cost.Blight[i].Announced {
				n = pc.x
			}
			e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "M1M1",
				Amount: n})
		}
	}
}

// hasRevealChosenDesignation reports whether o still carries the
// secretly-chosen designation a RevealChosen<Spec> part names: a player entry
// in Object.Chosen for RevealChosen<Player> (the Secretly$ True ChoosePlayer
// answer), a non-empty Object.ChosenType for RevealChosen<Type/...> (the
// Secretly$ True ChooseType answer). A RevealChosen part has no alternative
// payment -- no hand card is picked -- so an unset designation makes the whole
// cost unpayable and the ability is not offered at all.
func hasRevealChosenDesignation(o *state.Object, spec string) bool {
	if o == nil {
		return false
	}
	if strings.EqualFold(spec, "Player") {
		for _, t := range o.Chosen {
			if t.IsPlayer {
				return true
			}
		}
		return false
	}
	return o.ChosenType != ""
}

// revealChosenText composes the public reveal line a RevealChosen<Spec> part
// prints as it is paid. The chosen player's identity is the chain-safe
// tossName -- the deck-identity Name, never the display PlayerName (the F3
// invariant every other event text keeps; view/describe.go's player label may
// prefer PlayerName, but that is a view projection, not chain text).
func revealChosenText(g *state.Game, o *state.Object, spec string) (string, bool) {
	if o == nil {
		return "", false
	}
	if strings.EqualFold(spec, "Player") {
		for _, t := range o.Chosen {
			if t.IsPlayer {
				return "revealed the chosen player: " + tossName(g, t.Player), true
			}
		}
		return "", false
	}
	if o.ChosenType == "" {
		return "", false
	}
	return "revealed the chosen creature type: " + o.ChosenType, true
}

// finishLandPlay logs a land play only after its identified object actually
// reaches the battlefield. Updated replacement effects fold their MoveZone
// directly through events.Emit, so both the ordinary emit path and those
// replacement continuations call this one finalizer.
func (e *Engine) finishLandPlay(id state.ObjID) {
	if !e.etbLandPlay || id != e.etbLandObj {
		return
	}
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		return
	}
	e.settleLandPlay(id)
}

// settleLandPlay closes the land play's accounting for id and disarms the
// continuation, WITHOUT requiring the land to be on the battlefield. Playing a
// land is a special action that is taken -- and so uses that turn's land play
// (CR 305.1, CR 505.5b) -- the moment it is announced; a replacement effect
// that fully replaces the land's battlefield entry (ReplacementResult$
// Replaced) changes where the land ends up, not whether it was played. Leaving
// the continuation armed instead would both hand the player a second land play
// that turn and let a LATER, unrelated entry of the same object consume the
// stale continuation and log a LandPlayed that belongs to nothing.
func (e *Engine) settleLandPlay(id state.ObjID) {
	if !e.etbLandPlay || id != e.etbLandObj {
		return
	}
	p := e.etbLandPlayer
	e.etbLandPlay = false
	e.etbLandObj = 0
	e.emit(events.Event{Kind: events.LandPlayed, Player: p})
}

// landEntryParked reports whether some engine machinery still holds id's
// battlefield entry and will re-emit it once an outstanding answer arrives: an
// as-enters choice parked by applyETBChoiceReplacement (or the riot/unleash/
// siege fallbacks that park the same way), or a replacement body suspended on a
// mid-resolution ask. While the entry is parked the land play is not settled --
// the entry is still in flight and its own completion reaches finishLandPlay.
func (e *Engine) landEntryParked(id state.ObjID) bool {
	for _, parked := range [...]*events.Event{e.etbMove, e.riotMove, e.unleashMove, e.siegeMove} {
		if parked != nil && parked.Obj == id {
			return true
		}
	}
	return e.resume != nil
}

// settleLandPlayIfDone is the terminal-path settle: called wherever a land
// play's battlefield entry has finished being processed, it logs the land play
// unless the entry is still parked on an outstanding answer. An entry that
// completed normally already settled through finishLandPlay, so this is a
// no-op there; it fires exactly for the fully-replaced entry, which never
// reaches the battlefield at all.
func (e *Engine) settleLandPlayIfDone(id state.ObjID) {
	if e.landEntryParked(id) {
		return
	}
	e.settleLandPlay(id)
}

// payCast implements CR 601.2h (pay all costs) and, for a spell, CR 601.2i
// (the "when you cast" trigger). It runs after the target choice (601.2c);
// the object is already on the stack (pushCast). A payment that fails here
// (a pool that changed under a hand-built intent -- castable already gated
// the option the caster chose, so this is not reachable from an ordinary,
// well-formed client) aborts and REVERSES the push (CR 733.1): the object
// returns to the zone it came from, nothing remains paid and no cast trigger
// fires. The land play is also handled here (it never goes on the stack).
func (e *Engine) payCast() {
	pc := e.cast
	if pc == nil {
		return
	}
	if pc.mode == "land" {
		// A land's as-enters choice is now asked by the MoveZone replacement
		// boundary, not during this proposal. Keep LandPlayed behind that
		// boundary so it is logged only after the answered entry completes.
		e.cast, e.choosing = nil, chooseNone
		e.etbLandPlay, e.etbLandObj, e.etbLandPlayer = true, pc.card, pc.player
		card := pc.card
		e.emit(events.Event{Kind: events.MoveZone, Obj: card, From: pc.from, To: state.ZBattlefield})
		// The entry may never have happened: a ReplacementResult$ Replaced
		// entry replacement discards the move entirely and the land stays in
		// the zone it was played from. The special action was still taken, so
		// settle the land play here unless the entry is merely parked on an
		// as-enters answer (which settles on its own completion).
		e.settleLandPlayIfDone(card)
		return
	}
	// Publish the counter adder for the whole payment. A counter a COST places
	// (a blight M1M1, a planeswalker's [+N] loyalty counter, Suspend's TIME
	// counters) is put by the paying player, and an activated ability's cost is
	// paid before its wrapper exists on the stack -- so actionCause cannot
	// attribute it and the AddCounter class's ValidSource$ would fail closed.
	// A spell is already on the stack here, but the payer is its adder too, so
	// the one publish covers every cost site in both branches.
	prevAdder := e.SetCounterAdder(pc.player)
	defer e.SetCounterAdder(prevAdder)
	// The flow is now past the 601.2c target choice (either it was asked and
	// answered, or the SA has no target), so a mana-window resume through
	// continueCast must not re-ask for one.
	pc.passedTarget = true
	// alltargeted1: for a SPELL the stack object already exists (pushCast),
	// so the chain's pre-asked sub-ability target answers install here; the
	// ability arm installs after its AbilityPush mints the object.
	e.installSubPreAsk(pc)
	// CR 601.2e: the game checks that the proposed spell can legally be cast,
	// once every announcement choice (the {X} value) is known. An illegal
	// proposal is reversed (CR 733.1) -- see recheckIllegal.
	if e.recheckIllegal(pc) {
		return
	}
	// CR 601.2g: if the total cost includes a mana payment, the player gets a
	// chance to activate mana abilities before paying. manaWindowAsk poses
	// that window (returning true to suspend) only when the pool alone cannot
	// pay and an untapped mana source exists.
	if e.manaWindowAsk() {
		return
	}
	// Mana abilities can draw/reorder the library. Resolve the aggregate
	// ExileFromTop prefix only now, before any cost is paid, so the IDs moved
	// are still the actual top cards and insufficient cards abort without
	// spending the spell/activation's mana.
	if len(pc.cost.ExileFromTop) > 0 {
		top, ok := exileFromTopCards(e.G.Zone(state.ZLibrary, pc.player), pc.cost.ExileFromTop)
		if !ok {
			e.abortCast(pc, "exile cost no longer payable; cast/activation aborted", true)
			return
		}
		pc.exiles = append(pc.exiles, top...)
	}
	// Snapshot the source before any non-mana cost can move it. DamageYou
	// shares this LKI with resolution-time damage costs when its source has
	// already left the battlefield; a live source still uses current layers.
	sourceKeywordLKI := e.damageKeywordsOf(pc.card)
	sourceControllerLKI := state.PlayerID(0)
	if sourceObj := e.G.Obj(pc.card); sourceObj != nil {
		sourceControllerLKI = sourceObj.Controller
	}
	if pc.isAbility() {
		// Task 10: an activated ability. The shared stages above (X, Delve --
		// never present on an ability --, Sac) have already run and been
		// recorded; what differs from a spell here is the cost's remaining
		// non-mana parts. Pay mana, then each Tap (a Tap event), each
		// SubCounter part (a CounterChange of -N), and every chosen sacrifice.
		// The ability object is minted after payment below; answered targets
		// are recorded onto it after AbilityPush, including a window resume.
		mana := e.manaToPay(pc)
		// The descriptor carries the announced-X marker (the ability's own
		// {X} cost was folded), so a CostContainsX batch sees this activation
		// as an X payment exactly as the offer did.
		ok, _, spentMana, _, _ := e.payManaDescriptorForSpent(pc.player, paymentForCast(pc, mana), mana,
			e.paymentConv(pc.player, pc.card, true), pipRider{})
		if !ok {
			e.abortCast(pc, "activation aborted: cost no longer payable", true)
			return
		}
		// RememberCostMana$ (Jeweled Amulet: "Note the type of mana spent to
		// pay this activation cost"): the colours the payment actually spent
		// (the same per-colour delta the negative ManaAdd events above
		// record, in WUBRG order) fold onto the source object through the
		// "noted-mana" Choose marker, where the card's mana ability (Produced$
		// Special LastNotedType) reads them back. A cost with no mana part
		// notes nothing — Forge's CostRememberSpentMana records only mana
		// costs too.
		remembered := false
		if ab := e.pcAbility(pc); ab != nil {
			remembered = strings.EqualFold(strings.TrimSpace(ab.Params["RememberCostMana"]), "True")
		}
		if remembered {
			noted := ""
			for i, letter := range manaLetters {
				if spentMana[i] > 0 {
					noted += letter
				}
			}
			e.emit(events.Event{Kind: events.Choose, Obj: pc.card, Counter: "noted-mana", Text: noted})
		}
		if pc.payLife != 0 {
			e.emit(events.Event{Kind: events.LifeChange, Player: pc.player, Amount: -pc.payLife})
		}
		for _, id := range pc.delve {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile, Text: "delved"})
		}
		e.payDiscardCost(pc.discards, e.cyclingKeyword(pc))
		// Exile cost parts (ExileFromHand/ExileFromGrave): each chosen card
		// leaves its zone (hand, or the graveyard for a self-reference) for
		// exile. Read the zone live: the settled card is still where exAsk
		// found it, but a From read from the object keeps a graveyard
		// self-exile honest about where it moved from.
		for _, id := range pc.exiles {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile, Text: "exiled as a cost"})
			}
		}
		// CollectEvidence parts (alltargeted1): the evidence chosen at the
		// evidenceAsk stage leaves the payer's graveyard for exile, the same
		// action the Ward evidence payment performs.
		for _, id := range pc.evidence {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile, Text: "collected as evidence"})
			}
		}
		// ExiledMoveToGrave cost parts: each chosen card leaves exile for
		// its OWNER's graveyard (events.Move's zoneOwner already routes a
		// non-battlefield move to the owner), and the ExiledWith provenance
		// is cleared by the same move.
		for _, id := range pc.moveGraves {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZGraveyard, Text: "moved to its owner's graveyard as a cost"})
			}
		}
		// Energy cost parts (PayEnergy<N>/<X>): the announced amount leaves
		// the payer's energy pool as one PlayerCounterChange (a player
		// counter, not an object's -- CR 118.2d). The X form spends exactly
		// the announced value (xAsk bounded it by this same total). The
		// shared chargeEnergyCost helper is the ONE energy-charging site.
		e.chargeEnergyCost(pc.player, pc.cost, pc.x)
		// Announced PayLife<X> parts (Toxic Deluge's "pay X life"): each pays
		// the announced X as one LifeChange beside the fixed life payMana
		// charged above (payLife). xAsk bounded the announcement by the payer's
		// life, so the payment cannot drive the total below zero here.
		for range pc.cost.LifeX {
			if pc.x > 0 {
				e.emit(events.Event{Kind: events.LifeChange, Player: pc.player, Amount: -pc.x})
			}
		}
		// DamageYou<N> cost parts: the payer takes N damage from the source
		// (Forge CostDamage; the same event shape payUnlessDamageCost emits).
		for _, part := range pc.cost.DamageYou {
			e.payDamageCost(pc.player, part.N, pc.card, sourceKeywordLKI, sourceControllerLKI)
		}
		// GainLife<N/Player...> cost parts (see payGainLifeCost): every player
		// the part names relative to the payer gains N life.
		e.payGainLifeCost(pc.player, pc.cost.GainLife)
		// Mill cost parts (Mill<N>): the payer mills the summed requirement
		// from the top of their library as part of the payment.
		e.payMillCostParts(pc)
		// Draw cost parts (Draw<N/Spec>): the payer draws N, as one ordinary
		// Draw event per card (an empty library's loss is the SBA's). The
		// dredge replacement is NOT posed here -- the cast-flow payment stage
		// cannot re-enter mid-payment -- and no corpus card reaches a Draw
		// cost payment with a dredger in the graveyard.
		e.payDrawCostParts(pc)
		// Return cost parts: each chosen object moves to its OWNER's hand
		// (Forge CostReturn.doPayment's moveToHand) beside the other payments.
		for _, id := range pc.returns {
			if o := e.G.Obj(id); o != nil {
				e.emit(events.ReturnCost(id, o.Zone))
			}
		}
		e.emitChoiceCosts(pc)
		if pc.cost.Untap {
			e.emit(events.Event{Kind: events.Untap, Obj: pc.card, Player: pc.player, Text: "untapped as a cost"})
		}
		if pc.cost.Tap {
			// The {T} cost's payer taps the permanent (Forge CostTap). This
			// MUST come before settlePutToLibCost: a self-placement cost that
			// also carries {T} (Timestream Navigator's
			// "{2}{U}{U}, {T}, Put Timestream Navigator on the bottom of its
			// owner's library") moves the source off the battlefield, and
			// tapping a library card is not a state that exists (CR 110.5) --
			// a library tap would also survive a direct library→battlefield
			// re-entry, whose Move entry arm does not clear Tapped. Emitted
			// in this order the tap lands on the still-battlefield permanent
			// and the move's leave-battlefield arm resets it.
			e.emitTap(pc.card, pc.player, false)
		}
		// Settled after the {T} tap for the same reason (see above).
		e.settlePutToLibCost(pc)
		e.settleSubCounterParts(pc)
		// CR 606.3: a [+N] loyalty cost adds N loyalty counters to the walker
		// as part of the activation's payment, settled beside the SubCounter
		// removals and before the AbilityPush (the ability object the effect
		// resolves through). AddCounter is a free cost component -- no mana,
		// no gate -- so this is the only thing the activation does with it.
		// A 0-count part (the [0] abilities) emits nothing: a CounterChange of
		// 0 would be a no-op folded into state but a spurious log entry.
		for _, part := range pc.cost.AddCounter {
			if part.N != 0 {
				e.emit(events.Event{Kind: events.CounterChange, Obj: pc.card, Counter: part.Spec, Amount: part.N})
			}
		}
		// Exert<1/CARDNAME> (CR 701.39a): one Exert event on the source, the
		// same fold the declare-attackers election emits, so the untap skip
		// and "whenever you exert" triggers read one path.
		for range pc.cost.Exert {
			if o := e.G.Obj(pc.card); o != nil && o.Zone == state.ZBattlefield {
				e.emit(events.Event{Kind: events.Exert, Obj: pc.card, Player: pc.player})
			}
		}
		// Capture the sacrifice LKI (Task sac1) BEFORE the MoveZone events
		// drain the permanents: each chosen object is still on the battlefield
		// here, so SacrificedInfoOf reads its live face and +1/+1 counters (the
		// layer-7d portion of its P/T, which Move will reset). The resulting
		// stack object carries these to resolution, where the ability's
		// Sacrificed$<Property> SVar heads answer "the sacrificed creature's
		// power/toughness/mana value" (CR 608.2g) against them.
		var sacrificedLKI []state.SacrificedInfo
		for _, id := range pc.sacs {
			sacrificedLKI = append(sacrificedLKI, state.SacrificedInfoOf(e.G, id))
		}
		for _, id := range pc.sacs {
			e.emit(events.Sacrifice(id))
		}
		// RollDice cost parts are free, engine-driven payment actions. Publish
		// each die through the same canonical Note as DB$ RollDice so trigger
		// matching and replay observe the exact seeded result. The final result
		// is the ability's CR 107.3i X and is stamped onto its stack object below.
		for _, part := range pc.cost.RollDice {
			sides, err := strconv.ParseInt(part.Spec, 10, 32)
			if err != nil || sides <= 0 {
				continue // ParseCost admits only positive, bounded sides.
			}
			for i := int32(0); i < part.N; i++ {
				result := int32(e.Rand(int(sides)) + 1)
				e.emit(effects.DieRollNote(pc.card, pc.player, int32(sides), result, result))
				if part.Dyn == "X" {
					pc.x = result
				}
			}
		}
		// AbilityPush mints the ability object onto the stack AFTER the cost
		// settles, so an aborted activation leaves no stack object behind
		// (CR 733.1). handleTarget records the chosen targets onto it. A
		// GRANTED activation (task grantcost1) mints through the SAME three
		// events beginGrantedActivation/beginKeywordGrantedActivation mint --
		// the delayed-shape DelayedPush for a self-grant (Counter carries the
		// SVar name; the ^uint32(0) registration id matches nothing) and
		// GrantAbilityPush for a cross-object grant (IDs[0] carries the
		// grantor; the minted ability's Source is the recipient) -- so
		// resolution reads the SVar-anchored body exactly as it always has,
		// while every cost part above (sacrifice, discard, counter, energy,
		// draw, ...) is now paid by the shared flow too. A KEYWORD-GRANTED
		// activation (the AddKeyword$ Cycling route, CR 613.1f) mints through
		// KeywordAbilityPush, whose Counter carries the derived keyword line
		// the body is synthesized from.
		if pc.gainedFrom != 0 {
			e.emit(events.Event{Kind: events.GainedAbilityPush, Player: pc.player, Obj: pc.card,
				Amount: int32(pc.gainedIdx), IDs: []state.ObjID{pc.gainedFrom}})
		} else if pc.grantSVar != "" {
			if pc.grantSource == pc.card {
				e.emit(events.Event{Kind: events.DelayedPush, Player: pc.player, Obj: pc.card,
					Amount: -1, Counter: pc.grantSVar, Text: "granted ability"})
			} else {
				e.emit(events.Event{Kind: events.GrantAbilityPush, Player: pc.player, Obj: pc.card,
					Counter: pc.grantSVar, IDs: []state.ObjID{pc.grantSource}})
			}
		} else {
			e.emit(pc.activationPushEvent(e))
		}
		if len(e.G.Stack) > 0 {
			pc.stackObj = e.G.Stack[len(e.G.Stack)-1]
		}
		// alltargeted1: the chain's pre-asked sub-ability target answers are
		// now bound to the minted ability object, so the resolution consumes
		// them instead of re-posing the asks mid-resolution.
		e.installSubPreAsk(pc)
		// CR 107.3i: record the chosen {X} on the ability stack object, the
		// same way the spell arm records it on the spell below. The shared
		// xAsk stage asked and paid it (pc.cost.WithX(pc.x)), but AbilityPush's
		// Amount is the ability index, not the X value, so without this the
		// paid X never reaches resolution and every Cost$-X parameter
		// (CounterNum$ X, NumDmg$ X, NumCards$ X, SVar:X:Count$xPaid) reads 0.
		// Obj is pc.stackObj (the minted ability object), never pc.card (the
		// source permanent), so the permanent's own X (e.g. a Walking
		// Ballista's ETB value) is not clobbered. Emitted AFTER the push so
		// the object exists for events.Apply to write it on. Zero means no
		// X was paid: no event, matching the spell arm's guard.
		if pc.x != 0 {
			e.emit(events.Event{Kind: events.CastInfo, Obj: pc.stackObj, Amount: pc.x})
		}
		if e.sacrificedLKI == nil {
			e.sacrificedLKI = make(map[state.ObjID][]state.SacrificedInfo)
		}
		e.sacrificedLKI[pc.stackObj] = sacrificedLKI
		e.installPaidCostLists(pc)
		for _, id := range pc.sacs {
			if id != pc.card {
				continue
			}
			if e.sourceLifelinkLKI == nil {
				e.sourceLifelinkLKI = make(map[state.ObjID]bool)
			}
			if e.sourceControllerLKI == nil {
				e.sourceControllerLKI = make(map[state.ObjID]state.PlayerID)
			}
			e.sourceLifelinkLKI[pc.stackObj] = sourceKeywordLKI.lifelink
			e.sourceControllerLKI[pc.stackObj] = sourceControllerLKI
			// The own-source fields above carry only lifelink and controller.
			// CR 113.7a's other damage-relevant characteristics -- infect
			// (CR 702.90b) and deathtouch (CR 702.2b) -- live in the named
			// map, which Engine.emit's departure walk cannot seed here
			// either, because AbilityPush is minted only after the cost is
			// paid. Seed it with the same pre-cost snapshot, keyed on this
			// ability and its own source, so a bearer sacrificed to pay for
			// its own ability still deals damage in the granted form.
			e.captureNamedDamageSourceLKI(pc.stackObj, pc.card, sourceKeywordLKI, sourceControllerLKI)
			break
		}
		// A target answer can suspend payment in the 601.2g mana window.
		// Record it only once the ability object actually exists, on either
		// the immediate pay path or a resumed payCast; finishTargetedCast
		// cannot do so if the window has not minted the stack object yet.
		if pc.stackObj != 0 && pc.rootOpts != nil {
			e.recordChosenTargets(pc.stackObj, pc.rootOpts, false)
		}
		if pc.rootOpts == nil {
			// No target-recording continuation: dispatch at the completed
			// AbilityPush boundary while the spent-source capture is still live.
			e.fireManaSpentTriggers(pc.activationPushEvent(e), nil)
		}
		e.cast, e.choosing = nil, chooseNone
		return
	}
	mana := e.castPaymentMana(pc)
	paid, spentMana, spentSnow, spentTyped := e.payManaCastSpent(pc, mana)
	if !paid {
		// E2 (round 2) / F05-2. This is the reachable no-progress arm: a Delve
		// exile ask (Min:0, Max the shortfall) was answered with fewer cards
		// than the shortfall needs, so the cast aborts with no state change
		// and priority re-offers it. Declining is a legal, conforming answer --
		// CR 601.2h rewinds the cast -- but the engine must not re-offer the
		// SAME unpayable cast forever. CR 733.2 lets a reversed illegal action
		// be redone legally, so the FIRST no-progress abort leaves THIS card's
		// option offered; only the SECOND identical abort in the same window
		// holds it out of the remaining priority window (the suppression clears
		// on the first state-changing event, so the option returns as soon as
		// the window ends or the mana/board changes).
		e.abortCast(pc, "cast aborted: cost no longer payable", true)
		return
	}
	if f := e.G.Obj(pc.card).Face(); faceWantsConverge(f) || e.triggeredConvergeReaderOut() || e.sunburstGrantOut() {
		pc.convergeOn = true
		pc.converge = convergeColours(spentMana)
	}
	if f := e.G.Obj(pc.card).Face(); faceWantsCastSpend(f) || e.triggeredCastSpendReaderOut() {
		pc.manaSpentOn = true
		pc.manaSpent = manaSpentTotal(spentMana)
		pc.manaSpentSnow = manaSpentTotal(spentSnow)
		pc.manaSpentTreasure = manaSpentTotal(spentTyped[state.TypedTreasure]) + manaSpentTotal(spentTyped[state.TypedArtifactTreasure])
		pc.manaSpentCave = manaSpentTotal(spentTyped[state.TypedCave]) + manaSpentTotal(spentTyped[state.TypedArtifactCave])
		pc.manaSpentDesert = manaSpentTotal(spentTyped[state.TypedDesert]) + manaSpentTotal(spentTyped[state.TypedArtifactDesert])
		pc.manaSpentArtifact = manaSpentTotal(spentTyped[state.TypedArtifact]) + manaSpentTotal(spentTyped[state.TypedArtifactTreasure]) + manaSpentTotal(spentTyped[state.TypedArtifactCave]) + manaSpentTotal(spentTyped[state.TypedArtifactDesert])
	}
	// AddsCounters$ (task opalp): capture the rider grants from the SAME
	// payment capture emitRestrictedManaSpend built. This must run before
	// fireManaSpentTriggers consumes and clears e.manaSpentSources (nothing
	// can suspend between the payment and here -- the capture site only
	// emits). The capture is already filtered to rider-bearing batches and
	// keeps one record per spent unit, so a cast that spent ordinary (or only
	// restricted) mana emits no rider event and stays byte-identical.
	pc.addsCounterGrants = append([]state.ManaAddsCounterGrant(nil), e.manaSpentAddsCounters...)
	if pc.payLife != 0 {
		e.emit(events.Event{Kind: events.LifeChange, Player: pc.player, Amount: -pc.payLife})
	}
	for _, pay := range pc.convoke {
		e.emit(events.Event{Kind: events.Tap, Obj: pay.id})
	}
	for _, id := range pc.delve {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile, Text: "delved"})
	}
	e.payDiscardCost(pc.discards, "")
	// Exile cost parts (see the ability branch above for the why).
	for _, id := range pc.exiles {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZExile, Text: "exiled as a cost"})
		}
	}
	// CollectEvidence parts (alltargeted1; see the ability branch above).
	for _, id := range pc.evidence {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile, Text: "collected as evidence"})
		}
	}
	// ExiledMoveToGrave cost parts (see the ability branch above for the why).
	for _, id := range pc.moveGraves {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZGraveyard, Text: "moved to its owner's graveyard as a cost"})
		}
	}
	// Mill cost parts (see the ability branch above for the why).
	e.payMillCostParts(pc)
	// Energy cost parts (see the ability branch above for the why).
	e.chargeEnergyCost(pc.player, pc.cost, pc.x)
	// Announced PayLife<X>, DamageYou<N> and Draw<N/Spec> cost parts (see the
	// ability branch above for the why).
	for range pc.cost.LifeX {
		if pc.x > 0 {
			e.emit(events.Event{Kind: events.LifeChange, Player: pc.player, Amount: -pc.x})
		}
	}
	for _, part := range pc.cost.DamageYou {
		e.payDamageCost(pc.player, part.N, pc.card, sourceKeywordLKI, sourceControllerLKI)
	}
	e.payGainLifeCost(pc.player, pc.cost.GainLife)
	e.payDrawCostParts(pc)
	// Return cost parts (see the ability branch above for the why).
	for _, id := range pc.returns {
		if o := e.G.Obj(id); o != nil {
			e.emit(events.ReturnCost(id, o.Zone))
		}
	}
	e.settlePutToLibCost(pc)
	e.settleSubCounterParts(pc)
	e.emitChoiceCosts(pc)
	// Capture the sacrifice LKI before the MoveZones (see the ability branch's
	// comment): the sacrificed permanents are still on the battlefield here.
	var sacrificedLKI []state.SacrificedInfo
	for _, id := range pc.sacs {
		sacrificedLKI = append(sacrificedLKI, state.SacrificedInfoOf(e.G, id))
	}
	// Casualty:X (Ob Nixilis, the Adversary): the amount is the sacrificed
	// creature's power, read live here -- the sacrifice settles with the
	// cost parts below, and the copy trigger queued later in this same
	// payment carries the resolved value (CR 702.249a).
	if pc.casualtyVariable && pc.casualtySac != 0 {
		if o := e.G.Obj(pc.casualtySac); o != nil && o.Zone == state.ZBattlefield {
			pc.casualtyX = e.Power(pc.casualtySac)
		}
	}
	for _, id := range pc.sacs {
		e.emit(events.Sacrifice(id))
	}
	if pc.mode == "suspend" {
		info, _ := suspendCost(e.G.Obj(pc.card).Face())
		time := info.time
		if info.timeX {
			time = pc.x
		}
		// CastInfo is the replayable provenance marker: only this action sets
		// FlagSuspend, so an arbitrary exiled Suspend card is never treated as
		// having been suspended. Its Amount retains X while the card is exiled:
		// counter-removal triggers on X-time Suspend cards read xPaid there.
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.x, Counter: events.FlagsString(state.FlagSuspend)})
		e.emit(events.Event{Kind: events.MoveZone, Obj: pc.card, From: pc.from, To: state.ZExile, Text: "suspended"})
		if time > 0 {
			e.emit(events.Event{Kind: events.CounterChange, Obj: pc.card, Counter: "TIME", Amount: time})
		}
		e.cast, e.choosing = nil, chooseNone
		return
	}
	if pc.mode == "plot" {
		// CR 701.34a/b: the plot ACTION is not a cast. It pays the K:Plot
		// colon parameter, exiles the card face up, and gives it the plotted
		// designation -- NO counters (that is Suspend's mechanic): the free
		// cast's only timing restriction is CR 701.34b's "on a later turn",
		// so the designation is recorded as an events.AlterAttribute grant,
		// folded into Object.PlottedTurn with the CURRENT turn (the Enlist
		// turn-stamp shape). An arbitrary exiled Plot carrier is never
		// offered the cast: it carries no PlottedTurn, and only this action
		// (and the corpus's DB$ AlterAttribute | Attributes$ Plotted family,
		// once the effect side models it) grants the designation.
		e.emit(events.Event{Kind: events.MoveZone, Obj: pc.card, From: pc.from, To: state.ZExile, Text: "plotted"})
		e.emit(events.Event{Kind: events.AlterAttribute, Obj: pc.card, Text: "Plotted", Amount: 1})
		e.cast, e.choosing = nil, chooseNone
		return
	}
	if pc.mode == "foretell" {
		// CR 702.126a: the Foretell ACTION is not a cast. CastInfo is the
		// replayable provenance marker -- only this action sets FlagForetold
		// on a hand->exile move, so an arbitrary exiled card is never treated
		// as foretold -- and the MoveZone carries the face-down exile
		// encoding (events.Apply's decode sets FaceDown and clears ExiledWith:
		// no exiling source permanent exists for Foretell, so Amount 0). Do
		// NOT route through effects' applyExileFaceDown: it is unexported and
		// binds an ability source that does not exist here -- the raw event
		// encoding is emitted directly, the suspend branch's own pattern. The
		// view redacts the face-down exile to everyone but the exiler (the
		// owner, for Foretell), and any move NOT to exile clears FaceDown, so
		// the later foretell-cost cast reveals automatically.
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Counter: events.FlagsString(state.FlagForetold)})
		e.emit(events.Event{Kind: events.MoveZone, Obj: pc.card, From: pc.from, To: state.ZExile,
			Counter: "exiled_with_face_down", Amount: 0})
		e.cast, e.choosing = nil, chooseNone
		return
	}
	if e.sacrificedLKI == nil {
		e.sacrificedLKI = make(map[state.ObjID][]state.SacrificedInfo)
	}
	e.sacrificedLKI[pc.stackObj] = sacrificedLKI
	e.installPaidCostLists(pc)
	// A Fuse cast publishes its per-stage target split for resolution
	// (review MAJOR 1): resolveFused reads it instead of re-deriving the
	// split from the flat target list. Engine-only scratch like
	// sacrificedLKI — rebuilt by replay because payCast re-executes, and
	// removed with the stack object by the shared MoveZone cleanup.
	if pc.mode == "fuse" && len(pc.stageTargets) > 0 {
		if e.fuseTargets == nil {
			e.fuseTargets = make(map[state.ObjID][][]state.Target)
		}
		e.fuseTargets[pc.stackObj] = pc.stageTargets
	}
	if len(pc.charmTargets) > 0 {
		if e.charmTargets == nil {
			e.charmTargets = make(map[state.ObjID][][]state.Target)
		}
		e.charmTargets[pc.stackObj] = pc.charmTargets
	}
	// AddsNoCounter$ mana (Cavern of Souls): if the payment just consumed a
	// batch carrying the can't-be-countered provenance FOR THIS CAST, fold
	// state.FlagNoCounter into the same pay-time CastInfo so a replay marks
	// the spell exactly like every other cast flag. The capture is read once
	// here and cleared — nothing can suspend between emitRestrictedManaSpend's
	// set and this read (it emits, never asks).
	noCounter := e.noCounterSpend == pc.stackObj
	e.noCounterSpend = 0
	// CR 601.2b: record how the spell was cast (the X value and mode flags).
	// Deferred to payment rather than the up-front push so an aborted
	// proposal leaves no cast-time trace on the card. A cast trigger that
	// reads the mode (e.g. "cast a kicked spell") sees it, because the flag
	// is applied before the trigger fires next.
	flags := modeFlags(pc.mode)
	// CR 702.168: the Gift promise rides the pay-time CastInfo too -- the
	// CastFlags word is assigned wholesale here, so the bit events.GiftPromise
	// folded at pushCast (which the CR 601.2c target ask read) must be
	// re-stated or this later event would clear it. A declined promise emits
	// no flag.
	if pc.giftPromise {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagPromisedGift)
	}
	if pc.mode == "mutated" && pc.mutateTop {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagMutatedTop)
	}
	if noCounter {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagNoCounter)
	}
	// DB$ Play's ReplaceGraveyard$ Exile rider (task replplay1): the played
	// spell's provenance — "if that spell would be put into your graveyard
	// this turn, exile it instead" — rides the same pay-time CastInfo every
	// other mode flag uses. A free Play cast today satisfies neither the X
	// gate nor a non-empty modeFlags above, so setting the bit is what makes
	// the `flags != ""` emission arm below fire at all — exactly the event
	// the resolution reader needs; a Play whose SA carries no rider keeps
	// the byte-identical no-event shape.
	if pc.replaceGraveyard {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagReplaceGraveyard)
	}
	// kw:MayFlashSac (CR 702.8): a card cast off-sorcery through the keyword's
	// own flash permission carries the flag the keyword's ETB hook reads to
	// register the cleanup-step sacrifice. A sorcery-timed cast of the same
	// card (offSorcery false) or a cast of any other card emits nothing, so
	// unrelated casts stay byte-identical. The modeFlags switch has no case
	// for this keyword because the cast is ORDINARY -- there is no cast mode
	// to read and no extra cost; the permission alone sets no flag.
	if !pc.isAbility() && pc.offSorcery {
		if o := e.G.Obj(pc.card); mayFlashSacFace(o.Face()) {
			flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagMayFlashSac)
		}
	}
	// kw:Rebound (CR 702.95a): a spell cast from its controller's HAND whose
	// face carries the keyword is exiled as it resolves and offers the free
	// recast at the next upkeep. The flag is stamped only for a hand-origin
	// cast, so the re-bound cast from exile (CR 702.95e: "doesn't rebound
	// again") carries none and resolves ordinarily. The bit is a
	// CastProvenanceFlag, so a stack copy -- put on the stack, never cast
	// (CR 707.10) -- is stripped of it at the mint and resolves without the
	// promise. The modeFlags switch has no case for this keyword because
	// the cast is ORDINARY -- the keyword grants no alternative cost and no
	// mode; only the origin zone sets the flag.
	if !pc.isAbility() && pc.from == state.ZHand {
		if o := e.G.Obj(pc.card); o != nil && o.Face() != nil {
			if _, ok := o.Face().KeywordParam("Rebound"); ok {
				flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagRebound)
			}
		}
	}
	// Replicate (CR 702.55a): the payment count rides the same pay-time
	// CastInfo. modeFlags deliberately maps "replicated" to "" -- a DECLINED
	// replicate (count 0) must stay the byte-identical plain cast, no flag
	// and no event -- so the flag is ORed here only when a payment was made.
	// The count and a paid {X} never share one Amount: measured at the corpus
	// pin, no K:Replicate carrier's mana value carries {X}, so the
	// single-event shape below is the live path; the defensive two-event
	// split keeps the two provenances distinct should one ever pair.
	// A MayPlayText$-typed permission (rules/mayplay.go): stamp the
	// permission token on the pay-time CastInfo so mayPlayTypedLimitReached
	// can attribute the play to the static that granted it. The token rides
	// the Counter as an extra comma-separated field, which FlagsFrom ignores
	// (the CastFlags word is unaffected). It is appended ONLY to the main
	// CastInfo emission below, so a face whose trailing provenance events
	// (converge, mana spend) also carry `flags` cannot count the permission
	// twice; an untyped cast (empty mayPlayPerm) is byte-identical.
	permSuffix := ""
	if pc.mayPlayPerm != "" {
		permSuffix = ",perm=" + pc.mayPlayPerm
	}
	repCount := int32(0)
	if pc.mode == "replicated" {
		repCount = pc.replicateTimes
	}
	if repCount > 0 {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagReplicated)
	}
	if repCount > 0 && pc.x != 0 {
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.x,
			Counter: events.FlagsString(events.FlagsFrom(flags)&^state.FlagReplicated) + permSuffix})
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: repCount, Counter: flags})
	} else if pc.x != 0 || flags != "" {
		amt := pc.x
		if repCount > 0 {
			amt = repCount
		}
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: amt, Counter: flags + permSuffix})
	}
	// Squad (CR 702.66): the payment count rides its own TRAILING pay-time
	// CastInfo -- the flag routes the Amount into Object.SquadPaid (events.Apply's
	// CastInfo case), so this event never clobbers the X or replicate count an
	// earlier event in this block carried (no corpus carrier pairs {X} with
	// Squad, measured over the 15 K:Squad files), and its Counter leaves
	// CastFlags carrying every earlier flag too. modeFlags deliberately maps
	// "squadded" to "" -- a DECLINED squad (count 0) must stay the
	// byte-identical plain cast, no flag and no event -- so the emission is
	// gated on a payment having actually been made. Only a "squadded" cast
	// can carry a nonzero count, and no other mode reads pc.squadTimes, so
	// the gate is exact.
	if pc.mode == "squadded" && pc.squadTimes > 0 {
		sqFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagSquadPaid)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.squadTimes, Counter: sqFlags})
	}
	// Converge (CR 107.4f-family, task converge1): the distinct-colour spend
	// count rides its own TRAILING pay-time CastInfo -- the flag routes the
	// Amount into Object.ConvergeColours (events.Apply's CastInfo case), so
	// this event never clobbers the X or replicate count an earlier event in
	// this block set, and its Counter (flags + FlagConverged) leaves
	// CastFlags carrying every earlier flag too. Emitted whenever the face
	// carries a Count$Converge SVar -- or when a battlefield permanent's
	// trigger names TriggeredCard$Converge and so reads THIS cast's colours --
	// count 0 included (a colourless-only converge cast is a real zero, not an
	// absent one); the two-arm gate is heads-safety, so no game that casts no
	// converge card with no reader out changes an event.
	if pc.convergeOn {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagConverged)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.converge, Counter: flags})
	}
	// Multikicker (CR 702.43, task multikicker1): the times-kicked count
	// rides its own TRAILING pay-time CastInfo -- the flag routes the Amount
	// into Object.TimesKicked (events.Apply's CastInfo case), so this event
	// never clobbers the X a main CastInfo carried (Comet Storm pairs {X}
	// with Multikicker; the two-event split falls out of the trailing shape
	// itself), and its Counter leaves CastFlags carrying every earlier flag
	// too. A multikicked cast IS a kicked cast, so the flag rides with the
	// bare FlagKicked (the bare predicate and the Condition$ Kicked gate
	// keep matching). The emission gate keeps unrelated casts
	// byte-identical: ALWAYS on a multikicked-mode cast with count > 0 (a
	// declined kick -- count 0, the modeFlags("replicated") contract --
	// emits nothing), and on a plain-Kicker cast mode ONLY when the face
	// carries a Count$TimesKicked SVar (faceWantsTimesKicked): the 11 legacy
	// plain-Kicker carriers' scripts still read the count, and no other
	// kicked cast gains an event.
	mkCount := int32(0)
	switch pc.mode {
	case "multikicked":
		mkCount = pc.multikickTimes
	case "kicked", "kicked1", "kicked2":
		mkCount = 1
	case "kickedboth":
		mkCount = 2
	}
	if mkCount > 0 && (pc.mode == "multikicked" || faceWantsTimesKicked(e.G.Obj(pc.card).Face())) {
		mkFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagKicked | state.FlagMultikicked)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: mkCount, Counter: mkFlags})
	}
	// Conspire (CR 702.78a): the tap provenance rides its own trailing
	// pay-time CastInfo -- FlagConspired routes the bool into
	// Object.Conspired (events.Apply folds it outside the Amount switch, so
	// no later event's routing is disturbed), and Count$Conspired reads it
	// off the source. modeFlags deliberately maps "conspired" to "" -- a
	// declined/plain cast (conspirePaid false) must stay the byte-identical
	// plain cast, no flag and no event -- so the emission is gated on the
	// tap having actually been paid.
	if pc.conspirePaid {
		cFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagConspired)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: 1, Counter: cFlags})
	}
	// Convoke (CR 702.66, task connive1): the creatures the caster tapped to
	// help pay for the cast ride their own TRAILING pay-time CastInfo's IDs
	// -- the flag (NOT ORed into the accumulating flags, the Conspired
	// pattern) routes the IDs into Object.Convoked (events.Apply folds it
	// outside the Amount switch), and Defined$ Convoked reads it. Emitted
	// only for a face whose SVar table or abilities reference the selector
	// (faceWantsConvoked), so every unrelated convoke cast stays
	// byte-identical; no accumulation means no later CastInfo carries it,
	// so its arm's position in the newest-first switch is order-independent.
	if len(pc.convoke) > 0 && faceWantsConvoked(e.G.Obj(pc.card).Face()) {
		ids := make([]state.ObjID, 0, len(pc.convoke))
		seen := make(map[state.ObjID]bool, len(pc.convoke))
		for _, pay := range pc.convoke {
			if !seen[pay.id] {
				seen[pay.id] = true
				ids = append(ids, pay.id)
			}
		}
		cvFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagConvoked)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: int32(len(ids)), Counter: cvFlags, IDs: ids})
	}
	// AddsCounters$ (task opalp): the producing abilities' rider grants this
	// cast earned ride their own TRAILING pay-time CastInfo's Text payload --
	// the flag (NOT ORed into the accumulating flags, the Conspired/Convoked
	// pattern) routes the payload into Object.ManaAddsCounterGrants
	// (events.Apply folds it outside the Amount switch), and rules'
	// entry-counter plan uses each snapshot verbatim when the spell enters as
	// a permanent. Emitted only when the payment consumed rider-bearing mana,
	// so every unrelated cast stays byte-identical; no accumulation means no
	// later CastInfo carries the flag, so its arm's position in the
	// newest-first switch is order-independent.
	if len(pc.addsCounterGrants) > 0 {
		acFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagAddsCounters)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: int32(len(pc.addsCounterGrants)), Counter: acFlags, Text: events.ManaAddsCounterGrantsText(pc.addsCounterGrants)})
	}
	// Cast-spend (task castprov1): the TOTAL mana actually spent to cast the
	// spell rides its own TRAILING pay-time CastInfo -- the flag routes the
	// Amount into Object.ManaSpent (events.Apply's CastInfo case), so this
	// event never clobbers the X an earlier event in this block carried, and
	// its Counter leaves CastFlags carrying every earlier flag too. A
	// convoke-only cast (tapped creatures, no mana) is a real zero, not an
	// absent one -- the same "count 0 included" contract the converge
	// emission keeps. The emission gate keeps unrelated casts byte-identical:
	// only a face whose SVar table reads the count (faceWantsCastSpend)
	// stamps the event.
	if pc.manaSpentOn {
		flags = events.FlagsString(events.FlagsFrom(flags) | state.FlagManaSpent)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.manaSpent, Counter: flags})
		// The SNOW-unit part of that same spend (task castfilter1) rides its
		// own trailing CastInfo: the flag routes the Amount into
		// Object.ManaSnowSpent, so it never clobbers the unfiltered total the
		// event just set (the converge/multikick two-event split, applied one
		// step further). Emitted unconditionally alongside the total -- a cast
		// that spent no snow mana is a real zero, not an absent one -- so the
		// filtered Count$CastTotalManaSpent Snow read is exact for the six
		// Snow carriers without a second gate.
		snowFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagManaSnowSpent)
		e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.manaSpentSnow, Counter: snowFlags})
		// The TYPED parts of that same spend (task castfilter2) ride their own
		// trailing CastInfos, one per tag, each accumulating the earlier
		// flags -- the same split, applied twice further. Emitted
		// unconditionally alongside the total -- a cast that spent no mana of
		// a tag is a real zero, not an absent one -- so the filtered
		// Count$CastTotalManaSpent Treasure/Cave/Desert read is exact for
		// their carriers without a second gate. The emission order is total,
		// then Snow, then Treasure, then Cave, then Desert, then Artifact;
		// since every later event carries all earlier flags,
		// events.Apply's CastInfo switch
		// checks the NEWEST flag first (Artifact, Desert, Cave, Treasure,
		// Snow, then the total) or every later event would route into the
		// first tag's field.
		typedAmounts := [4]int32{pc.manaSpentTreasure, pc.manaSpentCave, pc.manaSpentDesert, pc.manaSpentArtifact}
		typedFlags := [4]uint64{state.FlagManaTreasureSpent, state.FlagManaCaveSpent, state.FlagManaDesertSpent, state.FlagManaArtifactSpent}
		acc := events.FlagsFrom(flags)
		for t := range typedFlags {
			acc |= typedFlags[t]
			e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: typedAmounts[t],
				Counter: events.FlagsString(acc)})
		}
	}
	// ManaExpend (trig:ManaExpend): fold this cast's pool spend into the
	// per-turn engine tally UNCONDITIONALLY -- including casts made before a
	// carrier entered the battlefield, which emit no FlagManaExpendCast event
	// under the gate below. The tally is what manaExpendMatches reads for the
	// crossing test (a cast that moves it from below Amount$ N to at-or-above
	// it fires once; a cast that starts at-or-above fires nothing); a
	// gated-only tally would undercount the pre-entry base and misfire BOTH
	// ways (spurious fire after a carrier entered mid-turn, missed crossing
	// when the real total crossed with the carrier out).
	//
	// The wake-up CastInfo emission stays gated (heads safety: only a cast
	// made while the caster's battlefield already holds a ManaExpend carrier
	// can fire one, so no game without a carrier changes an event). Count pool
	// mana actually spent plus each Convoke contribution: CR 702.50 says each
	// creature tapped for Convoke pays for one mana, and CR 601.2g-h includes
	// those contributions in paying the spell's total cost. Delve, Harmonize,
	// Improvise, free casts and ability activations do not spend mana. The
	// tally update runs BEFORE the emit, so the matcher reads the post-payment
	// total.
	if spend := manaSpentTotal(spentMana) + convokeManaSpent(pc.convoke); spend > 0 {
		e.manaExpendAdd(pc.player, spend)
		if e.manaExpendReaderOut(pc.player) {
			meFlags := events.FlagsString(events.FlagsFrom(flags) | state.FlagManaExpendCast)
			e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Player: pc.player, Amount: spend, Counter: meFlags})
		}
	}
	// Compleated's life-paid amount is deliberately the FINAL CastInfo: all
	// earlier payment captures may carry accumulated flags, so this event
	// must not be followed by one that routes its Amount elsewhere.
	if pc.payLife > 0 && !pc.isAbility() {
		if o := e.G.Obj(pc.card); o != nil && o.Face() != nil {
			for _, keyword := range o.Face().Keywords {
				if strings.EqualFold(strings.TrimSpace(keyword), "Compleated") {
					cf := events.FlagsString(events.FlagsFrom(flags) | state.FlagCompleated)
					e.emit(events.Event{Kind: events.CastInfo, Obj: pc.card, Amount: pc.payLife, Counter: cf})
					break
				}
			}
		}
	}
	// CR 601.2i: the "when you cast" trigger, held back from the up-front
	// push, fires now -- only after the spell is paid for. Capture the deferred
	// PutOnStack event (and its LKI) BEFORE the call: fireDeferredCastTrigger
	// nils them. The same event feeds the mana-spent riders below, which queue
	// AFTER the deferred cast triggers (deterministic append).
	var castEv events.Event
	var castLKI *state.Object
	if e.deferredPush != nil {
		castEv = *e.deferredPush
		castLKI = e.deferredPushLKI
	}
	e.fireDeferredCastTrigger()
	// Casualty is a cast trigger only when its additional sacrifice was paid.
	// Queue a respondable ability, rather than copying at payment; the event
	// payload rebuilds its body during replay (including permanent copies).
	if pc.casualtyPaid && castEv.Kind == events.PutOnStack {
		pt := pendingTrigger{
			Source: pc.card, Controller: pc.player, Casualty: true,
			Ctx: effects.Ctx{Source: pc.card, Controller: pc.player,
				Remembered: []state.Target{{Obj: pc.card}}},
		}
		// Casualty:X's script riders (Ob Nixilis, the Adversary's
		// NonLegendary$ True | SetLoyalty$ Casualty:...): the copy's
		// characteristics ride the trigger event's payload, so the replay
		// rebuilds the identical copy body. The loyalty value is the
		// casualty amount resolved at payment: the sacrificed creature's
		// power for X, the printed threshold for a numeric carrier that
		// named the rider (no corpus carrier does).
		if spec, ok := e.casualtySpec(pc.card); ok && (spec.nonLegendary || spec.setLoyalty) {
			var cc events.StackCopyCounter
			cc.NonLegendary = spec.nonLegendary
			if spec.setLoyalty {
				cc.Loyalty, cc.HasLoyalty = spec.threshold, true
				if pc.casualtyVariable {
					cc.Loyalty = pc.casualtyX
				}
			}
			pt.CasPayload = events.StackCopyCounterString(cc)
		}
		e.pendingTriggers = append(e.pendingTriggers, pt)
	}
	if castEv.Kind == events.PutOnStack {
		e.fireManaSpentTriggers(castEv, castLKI)
	}
	// Cascade (CR 702.85, task cascade1): one cast trigger per Cascade
	// instance, queued AFTER the ordinary cast triggers (deterministic
	// append; the drain's APNAP ordering places them). The queue emits
	// nothing and asks nothing, so no game without a cascade carrier
	// changes an event.
	e.queueCascadeTriggers(pc.stackObj, pc.player)
	// The Effect grants' cast-driven lifetime (ForgetOnCast$, task
	// param:api:Effect.ForgetOnCast) ends the grants at this completed-cast
	// moment, LAST in the pay stage: the qualifying cast often has its
	// behaviour FROM the grant (Dark Apostle's granted cascade fires this
	// very cast's cascade trigger, and the cascade resolution itself
	// re-derives the spell's granted keywords), so every reader above must
	// see pre-sweep state.
	e.effectCastSweep(castEv)
	e.cast, e.choosing = nil, chooseNone
}

// abortCast reverses a cast or activation proposal that cannot complete, per
// CR 733.1. If the object was already pushed (CR 601.2a / 602.2a), the
// reversal undoes the push: a spell returns to the zone it came from, and an
// ability's stack object leaves the stack (it ceases to exist, moved to exile
// as the existing ability-fizzle resting place does). Nothing is paid and
// the held cast trigger is dropped. e.cast and e.choosing are cleared.
func (e *Engine) abortCast(pc *pendingCast, text string, suppress bool) {
	// The reversal below and the push that preceded it are state-changing
	// events to emit's suppression-clearing rule, but their NET effect is no
	// progress -- the object returns to the zone it came from -- so the
	// held-out no-progress state must survive. For a pushed spell that state
	// is pc.preSuppress / pc.preAborts (captured before the push, which
	// cleared it); for an ability (never pushed) it is the current maps. Both
	// the held-out set and the per-card no-progress count are restored, so a
	// no-progress decline of THIS object can be counted again across the
	// push (F05-2).
	var saved map[state.ObjID]bool
	if pc.pushed && pc.preSuppress != nil {
		saved = make(map[state.ObjID]bool, len(pc.preSuppress))
		for id := range pc.preSuppress {
			saved[id] = true
		}
	} else if e.suppressedCast != nil {
		saved = make(map[state.ObjID]bool, len(e.suppressedCast))
		for id := range e.suppressedCast {
			saved[id] = true
		}
	}
	var savedAborts map[state.ObjID]int32
	if pc.pushed && pc.preAborts != nil {
		savedAborts = make(map[state.ObjID]int32, len(pc.preAborts))
		for id, n := range pc.preAborts {
			savedAborts[id] = n
		}
	} else if e.castAborts != nil {
		savedAborts = make(map[state.ObjID]int32, len(e.castAborts))
		for id, n := range e.castAborts {
			savedAborts[id] = n
		}
	}
	if suppress {
		if savedAborts == nil {
			savedAborts = map[state.ObjID]int32{}
		}
		savedAborts[pc.card]++
		// F05-2 (CR 733.2): the FIRST no-progress abort of a card leaves its
		// option offered, so a merely-reversed illegal action may be redone
		// legally; the SECOND identical abort holds the option out. Undo the
		// restore-only path for a count below two by not adding to `saved`.
		if savedAborts[pc.card] >= 2 {
			if saved == nil {
				saved = map[state.ObjID]bool{}
			}
			saved[pc.card] = true
		}
	}
	if pc.pushed && pc.stackObj != 0 {
		if pc.isAbility() {
			e.emit(events.Event{Kind: events.MoveZone, Obj: pc.stackObj, From: state.ZStack, To: state.ZExile, Text: "reversed"})
		} else {
			e.emit(events.Event{Kind: events.MoveZone, Obj: pc.stackObj, From: state.ZStack, To: pc.from, Text: "reversed"})
		}
	}
	if pc.faceBefore != nil {
		if o := e.G.Obj(pc.card); o != nil && o.FaceIdx != *pc.faceBefore {
			e.emit(events.Event{Kind: events.FlipFace, Obj: pc.card, Amount: int32(*pc.faceBefore)})
		}
	}
	// CR 733.1 applies identically to a mode announced during the proposal.
	// ModeChosen is a marker event, so the cache is restored beside the reverse
	// marker just as handleModes maintains it beside the forward marker.
	if pc.modeChosen {
		if o := e.G.Obj(pc.card); o != nil {
			if sa := o.Face().SpellAbility(); sa != nil {
				e.emit(events.Event{Kind: events.ModeChosen, Obj: pc.card, Player: pc.player,
					Text: strings.Join(modeLabels(sa, o.Face().SVars, pc.preModes), ",")})
			}
			o.ChosenModes = state.CloneChosenModes(pc.preModes)
		}
	}
	e.dropProposalTriggers(pc)
	e.deferredPush = nil
	e.deferredPushLKI = nil
	e.cast, e.choosing = nil, chooseNone
	e.emit(events.Event{Kind: events.Note, Player: pc.player, Text: text})
	e.suppressedCast = saved
	e.castAborts = savedAborts
}

// fireDeferredCastTrigger re-walks the up-front PutOnStack event that
// pushCast held back (deferredPush) so the CR 601.2i "when you cast" triggers
// fire, which is only after the spell is paid for. It is called from payCast
// for a spell; a no-op when nothing was deferred (an ability, a land, or an
// aborted proposal).
func (e *Engine) fireDeferredCastTrigger() {
	if e.deferredPush == nil {
		return
	}
	ev := e.deferredPush
	e.deferredPush = nil
	lki := e.deferredPushLKI
	e.deferredPushLKI = nil
	e.sweepEffectDelayedCast(*ev)
	e.checkTriggers(*ev, lki, 0, 0, false)
}

// fireManaSpentTriggers queues the TriggersWhenSpent$ rider of every mana
// source whose provenance batch paid for the just-completed spell cast or
// activated ability. Sources are captured by emitRestrictedManaSpend; ev is
// the completed PutOnStack/AbilityPush event. It runs after payment and push,
// preserving deterministic trigger append order.
//
// A rider's SVar is a T:-shaped trigger body (Mode$ SpellCast | ValidCard$ ...
// | Execute$ ...) that the ordinary trigger scan never walks -- it lives in
// the face's SVar table, not its printed T: lines. So each is parsed by
// cards.ParseTriggerLine and queued by hand, shaped exactly like the exert
// rider (rules/trigger_match.go checkExertTriggers): Source = the mana
// permanent, Idx -1, Granted=true with Execute = the body's Execute$ name and
// SA = the resolved Execute body, so the live queue and a replayed log carry
// the identical granted-trigger push (events.Apply resolves Execute from the
// source's SVar table). A source that has left the battlefield, has no face,
// or names no longer-resolvable body fails closed -- the rider belongs to the
// permanent. SpellCast is spell-only; SpellAbilityCast dispatches on both
// spell casts and activated abilities.
// manaSpentAddsCounterSources (task opalp) is gone: the rider grant is
// captured per batch by emitRestrictedManaSpend (e.manaSpentAddsCounters) so
// the producing ability's snapshot and the spent unit count survive, instead
// of a deduplicated source list that re-read the source face at entry.

func (e *Engine) fireManaSpentTriggers(ev events.Event, lki *state.Object) {
	sources := e.manaSpentSources
	e.manaSpentSources = nil
	if len(sources) == 0 || (ev.Kind != events.PutOnStack && ev.Kind != events.AbilityPush && ev.Kind != events.KeywordAbilityPush) {
		return
	}
	for _, src := range sources {
		o := e.G.Obj(src)
		if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil {
			continue
		}
		f := o.Face()
		for _, ma := range f.ManaAbilities() {
			rider := strings.TrimSpace(ma.Params["TriggersWhenSpent"])
			if rider == "" {
				continue
			}
			body := svarBodyForObject(o, rider)
			if body == "" {
				continue
			}
			t, ok := cards.ParseTriggerLine(body)
			if !ok || (t.Mode != "SpellCast" && t.Mode != "SpellAbilityCast") {
				continue
			}
			matches := false
			switch t.Mode {
			case "SpellCast":
				matches = e.spellCastEval(t, src, ev)
			case "SpellAbilityCast":
				matches = e.spellAbilityCastMatches(t, src, ev, lki)
			}
			if !e.zoneGate(t, src, ev) || !e.phaseGate(t) || !matches {
				continue
			}
			exec := strings.TrimSpace(t.Params["Execute"])
			sa := grantedTriggerExecute(o, exec)
			if sa == nil {
				continue
			}
			key := triggerKey{Source: src, Idx: -1}
			if e.triggerFireCount == nil {
				e.triggerFireCount = map[triggerKey]int32{}
			}
			if e.triggerFireCount[key] >= maxTriggerFires {
				continue // cascade bound: see maxTriggerFires.
			}
			e.triggerFireCount[key]++
			e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
				Source:     src,
				Controller: o.Controller,
				Idx:        -1,
				SA:         sa,
				Granted:    true,
				Execute:    exec,
				Ctx: effects.Ctx{
					Source:         src,
					Controller:     o.Controller,
					TriggerContext: e.triggerReferents(t, src, ev, lki),
				},
			})
		}
	}
}

// svarBodyForObject resolves a raw SVar body by name against the object's own
// face first, then every other face of its card (the resolveSVarAcrossFaces
// walk events.Apply's granted-trigger push uses, so the queue and the replay
// agree on which body a name links). Empty when no face declares it.
func svarBodyForObject(o *state.Object, name string) string {
	if o == nil || name == "" {
		return ""
	}
	if f := o.Face(); f != nil {
		if body, ok := f.SVars[name]; ok {
			return body
		}
	}
	if o.Card != nil {
		for _, cf := range o.Card.Faces {
			if body, ok := cf.SVars[name]; ok {
				return body
			}
		}
	}
	return ""
}

// recordCmdCast increments the CmdCasts[k] bookkeeping parallel to
// Commanders[k] for a commander cast from the command zone. It is called by
// commitCast only when a command-zone cast's PutOnStack was just appended, so
// a cast from any other zone (hand, graveyard-flashback, ...) is never
// counted here.
//
// The count it maintains is DERIVED state, and the brief's "derived from
// events on replay" is the right of its two options for exactly this reason:
// CmdCasts[k] is a deterministic pure function of the already-logged
// PutOnStack events (From == ZCommand, per commander id). The generic,
// format-agnostic events.Apply handler is the wrong home for it -- this is a
// Commander-format rule, not a universally-applicable state transition -- so
// it is maintained as a projection at the exact point its authoritative event
// is appended, which introduces no new degree of freedom: a faithful replay,
// which re-runs this same beginCast -> commitCast path against the recorded
// Intents, appends the identical PutOnStack events and so lands on the
// identical count. Clone deep-copies the slice (state goes through
// Game.Clone) so the O(1) tax read (commanderTaxFor) survives a clone; the
// number itself comes from the event stream alone. No event of its own is
// needed, and events/ is outside this task's boundary.
func (e *Engine) recordCmdCast(p state.PlayerID, id state.ObjID) {
	if e.format != FormatCommander {
		return
	}
	for k, cid := range e.G.Players[p].Commanders {
		if cid == id {
			e.G.Players[p].CmdCasts[k]++
			return
		}
	}
}

// castSuppressed reports whether id's cast option is currently held out of
// p's priority offers (see suppressedCast, engine.go). The id names the one
// seat holding it, so p is not consulted beyond matching that id's zone in
// the walk that called it.
func (e *Engine) castSuppressed(p state.PlayerID, id state.ObjID) bool {
	return e.suppressedCast != nil && e.suppressedCast[id]
}

func init() {
	effects.RegisterNonAPI("kw:Kicker", "kw:Surge", "kw:Flashback", "kw:Aftermath", "kw:Delve",
		// The alternative-cost keyword family (altcosts): each is implemented
		// to its CR shape with a named proof test in altcast_test.go --
		// kw:Evoke (alternative cast + ETB unconditional sacrifice), kw:Dash
		// (alternative cast + haste + delayed return), kw:Overload
		// (alternative cast + target-reads-each), kw:Warp (alternative cast
		// from hand/graveyard/exile + delayed exile), kw:Madness (exile on
		// discard + immediate cast offer), kw:Encore (graveyard activation
		// minting attacking token copies), and kw:AlternateAdditionalCost
		// (the mandatory either-or additional cost choice).
		"kw:Evoke", "kw:Dash", "kw:Overload", "kw:Warp", "kw:Madness",
		"kw:Encore", "kw:AlternateAdditionalCost",
		// kw:Unearth (CR 702.84): the graveyard return is an ordinary
		// activated ability cards/kw_unearth.go expands from the K: line,
		// and its three riders are rules-layer (rules/unearth.go) -- the
		// haste grant, the end-step exile promise, and the exile-instead
		// replacement. Registered non-API because the keyword's whole
		// implementation lives in rules plus the one builtin SVar body.
		"kw:Unearth",
		// kw:Escalate: the modal additional cost "pay this for each mode chosen
		// beyond the first" -- read directly off the K: line by beginCast's
		// capture and the cast_modes answer handler's fold, and bounded by
		// castModeAsk's affordable-escalation clamp (no keyword expansion; the
		// plain Charm cast is the only way in). The mode ask's Max is clamped
		// to 1 + the affordable escalations so an unpayable mode count is
		// never offered. All 9 corpus carriers parse (7 plain mana,
		// tapXType<1/Creature> and Discard<1/Card>).
		"kw:Escalate",
		// kw:Entwine: CR 702.42, the OPTIONAL additional cost "pay this as you
		// cast a modal spell; if you do, follow the instructions of all its
		// modes". The offer lives in legal.go's hand/command-zone cast walk
		// (mode "entwined", composing the printed cost plus the entwine cost
		// through offerCastable), the charge in beginCast's "entwined" case,
		// and the all-modes announcement in castModeAsk (Min and Max forced to
		// the filtered legal count). The corpus forms -- plain mana (30
		// carriers) and a Sac<2/Land>/Sac<3/Land> sacrifice (Betrayal of
		// Flesh, Solar Tide) -- all price through ParseCost; an unpriceable
		// form fails closed in entwineCost and never offers. Proof:
		// rules/entwine_test.go.
		"kw:Entwine",
		// kw:Strive: CR 702.52, the mandatory additional cost "this spell
		// costs <cost> more for each target beyond the first" -- read directly
		// off the K: line by beginCast's capture (no keyword expansion; the
		// plain cast is the only way in) and priced by repriceForTargets once
		// the CR 601.2c target answer is in, folded into pc.cost so the mana
		// window and the payment charge the composed total. The per-extra
		// count is delta-tracked on pendingCast.striveUnits, so the fold is
		// idempotent across repriceForTargets' re-entries. Proof:
		// rules/strive_test.go.
		"kw:Strive",
		// kw:Escape: CR 702.135, the graveyard cast with its exile cost, read
		// off the K: line by derivedKeywordParam and gated in legal.go's
		// cast walk -- proved by TestUnderworldBreachGrantsEscapeAndTheEscape
		// CastResolves and TestKroxaEscapeCastDoesNotSacrificeOnETB. It was
		// implemented without being registered, so the coverage ratchet read
		// it as a gap.
		"kw:Escape",
		// kw:Retrace: CR 702.81a, the graveyard cast paying the printed mana
		// cost plus an additional discard-a-land cost. The offer lives in
		// legal.go's graveyard walk and the cost fold in beginCast's "retrace"
		// mode, both through retraceExtra so offer and charge cannot disagree.
		"kw:Retrace",
		// kw:Jump-start: CR 702.84a, the graveyard cast paying the printed
		// mana cost plus an additional discard-a-card cost and exiling the
		// card on resolution. The offer lives in legal.go's graveyard walk, the
		// cost fold in beginCast's "jumpstart" mode (both through
		// jumpstartExtra), and the exile destination in modeFlags' FlagJumpstart
		// read by spellRestZone/spellFizzleZone -- proved by
		// TestRadicalIdeaJumpstartCastsDiscardsAndExiles.
		"kw:Jump-start",
		"kw:Buyback", "kw:Transmute", "kw:Suspend", "kw:Convoke", "kw:Harmonize", "kw:Cycling",
		// kw:Cascade: CR 702.85, the cast trigger read directly off the K:
		// line (no keyword expansion — the printed K:Cascade and every
		// layer-6 AddKeyword$ Cascade grant reach hasCastCascade through the
		// one derived-keyword read, so the printed and granted routes cannot
		// disagree). The trigger is a real KeywordTriggerPush stack object;
		// its resolution is effects' Cascade primitive. Proof:
		// TestBloodbraidElfCascadeExilesUntilLesserAndOffersFreeCast,
		// TestCascadeDeclinedFoundCardGoesToBottom,
		// TestDarkApostleGrantedCascadeRegistersAndOffers in
		// rules/cascade_test.go.
		"kw:Cascade",
		// kw:Improvise: CR 702.66, the generic-only payment keyword -- its
		// announcement over the caster's untapped artifacts (the improvise
		// arm inside convokeAsk) and its greedy offer-gate credit
		// (improviseCost) are the proof, in cast.go.
		"kw:Improvise",
		// kw:TypeCycling: CR 702.28d (typed cycling), expanded by
		// cards/keywords.go into the Transmute-shaped library search
		// (AB$ ChangeZone | Origin$ Library | Destination$ Hand |
		// ChangeType$ <type>), whose reveal comes from the search's own
		// stated-quality default. Proof: TestTypeCyclingSearchesTheNamedType
		// and TestTypeCyclingBasicLandSearchesAnyBasic in
		// rules/alternative_costs_test.go.
		"kw:TypeCycling",
		// kw:Ninjutsu: CR 702.49, expanded by cards/kw_ninjutsu.go into an
		// ordinary hand-zone activated ability whose Cost$ carries the printed
		// ninjutsu mana cost plus Return<1/Creature.YouCtrl+attacking+unblocked>
		// (the CR 702.49a unblocked-attacker half, via the effects filter's
		// "unblocked" predicate) and whose body puts the card onto the
		// battlefield tapped and attacking via ChangeZone's Attacking$ True
		// rider, bound to the defender captured when the Return cost was paid
		// (pendingCast.ninjutsuDefender -> AbilityPush IDs -> Ctx.DefendingPlayer,
		// CR 702.49b). Proof: rules/ninjutsu_test.go.
		"kw:Ninjutsu",
		// kw:Level up: CR 702.87, expanded by cards/keywords.go into an
		// ordinary sorcery-speed PutCounter activation (CounterType$ LEVEL);
		// the level-band statics read the counter through the existing
		// counters_<CMP><n>_LEVEL predicate, so no separate path of its own.
		"kw:Level up",
		// kw:Outlast: CR 702.107, expanded by cards/kw_outlast.go into an
		// ordinary sorcery-speed PutCounter activation (CounterType$ P1P1,
		// Cost$ T <mana>); the leading T is the CR 702.107a tap cost and
		// makes the keyword repeatable only across untaps, with no
		// once-per-turn machinery of its own. Proof:
		// rules/outlast_test.go.
		"kw:Outlast",
		// kw:Class: CR 702.118, expanded by cards/keywords.go into one
		// sorcery-speed level-up activator per level (the kw:Level up shape,
		// gated on the Class's level being below that level) plus the level's
		// granted static/trigger/replacement, appended with its own ClassBand$
		// band so it is live from level N on (read as an independent AND gate
		// by rules/class_level.go's classBandGateHolds). The entry
		// counter (a Class enters at level 1) is the same etbCounter
		// PutCounter replacement shape. Proof: rules/class_test.go.
		"kw:Class",
		// kw:Replicate: CR 702.55, expanded by cards/keywords.go into the
		// Storm-shaped copy trigger whose Amount$ Count$ReplicatePaid reads
		// the pay-time CastInfo's count; the cast flow's replicateAsk poses
		// the CR 601.2b count announcement.
		"kw:Replicate",
		// kw:Multikicker: CR 702.43, the count-ask cast shape -- the cast flow
		// (rules/legal.go's multikicked offer, rules/cast.go's multikickAsk)
		// poses the CR 601.2b count announcement and the trailing
		// FlagMultikicked CastInfo carries the count into Object.TimesKicked
		// for the Count$TimesKicked head (effects/count.go). No keyword
		// expansion: the K:Multikicker line is read directly.
		"kw:Multikicker",
		// kw:Conspire: CR 702.78, expanded by cards/keywords.go into the
		// Storm-shaped copy trigger whose Amount$ Count$Conspired reads the
		// pay-time CastInfo's flag; the cast flow's "conspired" offer
		// (rules/legal.go) plus conspireAsk (rules/cast.go) pose and pay the
		// two-creature tap.
		"kw:Conspire",
		// kw:Demonstrate: CR 702.152, expanded by cards/kw_demonstrate.go
		// into the SpellCast trigger on the card's own cast whose DB$
		// Demonstrate body (effects/demonstrate.go) poses the may-copy
		// election and the opponent choice; a layer-6 AddKeyword$ Demonstrate
		// grant (Silverquill Lecturer) reaches the same body through
		// rules/trigger_granted.go's checkGrantedDemonstrateTriggers. The
		// copies are ordinary StackCopy mints, so a creature-spell copy
		// becomes a token through the standing CR 707.10g fold.
		"kw:Demonstrate",
		// kw:Squad: CR 702.66, expanded by cards/keywords.go into a
		// ChangesZone self-entry trigger whose DB$ CopyPermanent body reads
		// Count$SquadPaid (the Replicate pattern); the cast flow's "squadded"
		// offer (rules/legal.go) plus squadAsk (rules/cast.go) pose and pay
		// the CR 601.2b count announcement, and the trailing FlagSquadPaid
		// CastInfo carries the count into Object.SquadPaid.
		"kw:Squad",
		// kw:Affinity: CR 702.41, expanded by cards/keywords.go into the
		// ordinary ReduceCost cost-static machinery (rules/statics.go's
		// collectCostStatics) -- no separate cast path of its own.
		"kw:Affinity",
		// kw:Undaunted: CR 702.105, expanded by cards/kw_undaunted.go into
		// the ordinary ReduceCost cost-static machinery.
		"kw:Undaunted",
		// kw:Embalm / kw:Eternalize: CR 702.128 / 702.129, expanded by
		// cards/keywords.go into one graveyard-zone CopyPermanent activation
		// whose cost exiles the card itself (ExileFromGrave<1/CARDNAME>) and
		// whose token copy carries the keyword's modified characteristics --
		// the Encore graveyard-activation shape with a different effect.
		// kw:Gravestorm: CR 702.84, expanded by cards/keywords.go into the
		// Storm-shaped copy trigger whose Amount$
		// Count$ThisTurnEntered_Graveyard_from_Battlefield_Permanent reads the
		// zone-aware count in effects.countEntered.
		"kw:Gravestorm",
		"kw:Embalm", "kw:Eternalize",
		// kw:Plot: CR 701.34, the hand-origin alternative ACTION -- pay the
		// K:Plot colon parameter, exile the card face up with the plotted
		// designation stamped with the current turn (NO counters: the free
		// cast's only restriction is CR 701.34b's "on a later turn"), and
		// offer a free cast at sorcery timing from a later turn onward (the
		// exile-zone walk; no upkeep ask, unlike Suspend's cast-if-able). No
		// keyword expansion: the K:Plot line is read directly. Proof:
		// rules/plot_test.go.
		"kw:Plot")
}

// emitProposalFlip records the FlipFace an alternate-face cast proposal
// (room_alt, adventure_alt, adventure_recast, aftermath, split_alt,
// defeat_cast) makes before its ordinary cast transaction, WITHOUT letting
// that flip count as game progress. emit clears the held-out no-progress
// state (suppressedCast/castAborts) on every state-changing event; the flip
// is one, but it is net no progress when the proposal is then reversed
// (abortCast flips the card back). Left cleared, pushCast captured the
// already-emptied maps as the proposal's pre-push state, so abortCast's
// F05-2 count restarted at zero on every attempt and the SECOND identical
// no-progress abort never held the option out: an Adventure cast the offer
// priced as payable but the flipped face could not pay was reversed and
// re-offered forever (the botbench flip_face livelock). Restoring the maps
// the proposal began with keeps the count across attempts; a cast that goes
// on to reach the stack clears them at its PutOnStack as before.
func (e *Engine) emitProposalFlip(id state.ObjID, before uint8, preSuppress map[state.ObjID]bool, preAborts map[state.ObjID]int32) {
	e.emit(events.Event{Kind: events.FlipFace, Obj: id, Amount: int32(1 - int(before))})
	e.suppressedCast, e.castAborts = preSuppress, preAborts
}

// dropProposalTriggers removes the pending triggers a reversed spell
// proposal's own CR 601.2c target choice queued (pc.proposalTriggers). CR
// 733.1: "No abilities trigger and no effects apply as a result of an undone
// action." Only the recorded ranges go -- a trigger an unreversed mana
// ability produced during the payment window (Manabarbs) stays queued, since
// the engine never reverses those activations. Ranges are removed back to
// front so an earlier range's indices are unaffected; a range the queue no
// longer covers (it was drained, which a live proposal never does) is skipped
// rather than trusted, and an ordered prefix is never touched.
func (e *Engine) dropProposalTriggers(pc *pendingCast) {
	ranges := pc.proposalTriggers
	pc.proposalTriggers = nil
	for i := len(ranges) - 1; i >= 0; i-- {
		start, end := ranges[i][0], ranges[i][1]
		if start < e.orderedTriggers || start >= end || end > len(e.pendingTriggers) {
			continue
		}
		e.pendingTriggers = append(e.pendingTriggers[:start], e.pendingTriggers[end:]...)
	}
}

// castWindowUnits is the CR 601.2g cast-payment window's provable mana reach:
// the shared fixed-production census (windowManaUnits) plus each
// choice-shaped source (Any, Combo, Chosen) as single-colour alternatives of
// the SAME permanent, never as another tap, plus the cast-only paid/dynamic
// layer (castWindowPaidUnits) for the deterministic activation shapes the
// shared census cannot price. The shared census omits those because an
// unless-pay window cannot pose their colour or payment sub-ask; cast
// payment can. The result is filtered to the sources manaWindowAsk will
// actually offer (a permanent this cast already committed to Convoke or
// Conspire is withheld).
func (e *Engine) castWindowUnits(pc *pendingCast) []windowManaUnit {
	p := pc.player
	windowUnits := e.windowManaUnits(p)
	windowUnits = e.castWindowProbeUnits(pc, windowUnits)
	// SUPERSET GUARD (the anti-abort invariant): a source this cast already
	// committed to Convoke/Harmonize/Improvise (pc.convoke) or Conspire
	// (pc.taps) is NOT offered by manaWindowAsk (cast.go's convokeCommitted
	// filter), so the probe must not promise its tap either -- the cost fold
	// already credits its contribution, and counting the permanent a second
	// time would let the probe claim reach the window cannot complete.
	out := windowUnits[:0]
	for _, u := range windowUnits {
		if e.convokeCommitted(pc, u.id) {
			continue
		}
		out = append(out, u)
	}
	return out
}

// castWindowProbeUnits is castWindowUnits' cast-only activation-cost layer.
// It adds the shapes the shared windowManaUnits withholds from EVERY payment
// window: choice-shaped productions (a colour chosen when the source is
// tapped), free abilities whose Amount$ is not a literal, and non-free
// activation costs (a literal generic <N>, a PayLife<N>, a deterministic
// self-sacrifice, or a choice-shaped production behind any of them). The
// conservatism of windowManaUnits is load-bearing for the attack-cost and
// unless-cost windows, which cannot pose a sub-ask while tapping; the CR
// 601.2g cast window CAN: manaWindowAsk offers any currently payable
// non-InstantSpeed mana source, including a tapped source with a {Q} ability,
// and activation runs through resolveManaAbilityRef, which pays the full
// activation cost and evaluates the Amount$ body. Every source added here
// comes from the same availableManaAbilitiesForWindow walk manaWindowAsk uses,
// so the probe can never promise an activation the window will not offer.
//
// A literal generic <N> activation cost is carried on the alt as costGeneric
// rather than netted into the production: the cast-only eligibility search
// (castWindowReachable) pays it from the mana this window has already
// accumulated, so the fee can be funded by an earlier same-window activation
// and a multi-colour production stays expressible without choosing which
// colour the generic consumed. A PayLife<N> activation carries its life on
// the alt so the search debits it. The CHOICE-SHAPED production is split
// into one alt per producible colour, exactly the shared walk's
// one-alt-per-alternative shape, so the payer's colour choice is made by
// picking an alt and never needs a nested sub-ask.
//
// Deliberately EXCLUDED (fail closed), each for a named reason:
//
//   - InstantSpeed$ True abilities: already withheld by the shared walk.
//   - RestrictValid$-governed abilities: the produced batch may not pay the
//     priced cost, and the dotted matcher only admits a subset of the
//     grammar, so no restriction is priced here (AGENTS.md row 1's
//     direction).
//   - tapXType, SubCounter, Mill, UnlessCost$, Return<>, coloured activation
//     pips, multi-part or overlapping Sac costs, loyalty-ability mana
//     producers (never exposed by availableManaAbilities), and every
//     Amount$ body the count evaluator does not understand.
func (e *Engine) castWindowProbeUnits(pc *pendingCast, windowUnits []windowManaUnit) []windowManaUnit {
	p := pc.player
	pl := e.G.Players[p]
	for _, id := range e.battlefieldManaSourceIDs(p) {
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		for _, ma := range e.castWindowProbeAbilities(p, id) {
			if strings.TrimSpace(ma.Params["RestrictValid"]) != "" {
				continue
			}
			cost := e.parseCost(ma.Params["Cost"])
			if activationTapCostUnavailable(o, cost) {
				continue
			}
			lifeCost := int32(0)
			genericCost := int32(0)
			free := manaFreeCost(cost)
			switch {
			case free:
				// windowManaUnits already counted a free PLAIN literal
				// production; only a choice-shaped or dynamic amount it
				// withholds is added here (the skip test runs below, after
				// the production is parsed).
			case castWindowPayLifeCost(cost):
				lifeCost = cost.Life
				if pl.Life <= lifeCost {
					continue
				}
			case castWindowGenericCostShape(cost):
				genericCost = cost.Generic
			case e.castWindowSelfSacCost(p, id, cost):
			default:
				continue
			}
			amt, ok := e.castWindowAmount(p, id, o, ma)
			if !ok {
				continue
			}
			// Chosen is an as-enters read: substitute the recorded colour
			// BEFORE the parse so a "Combo R Chosen" land recorded as G
			// offers R/G only, never the raw parser's WUBRG superset. Without
			// a record it produces no colour and stays out of this window
			// (the same fail-closed direction the shared walk takes).
			produced := strings.TrimSpace(ma.Params["Produced"])
			if producedNeedsChosen(produced) {
				chosen := e.chosenProducedColour(id)
				if chosen == "" {
					continue
				}
				produced = substituteChosenProduced(produced, chosen)
			}
			counts, any := cards.ProducedCounts(produced)
			total := int32(0)
			for _, n := range counts {
				total += n
			}
			if total <= 0 {
				continue
			}
			if free {
				// A free plain literal production is windowManaUnits' domain
				// (already counted); a free choice-shaped one is not, and a
				// free dynamic amount is the shape this layer adds.
				if !any && availableAmount(ma) > 0 {
					continue
				}
			}
			if any {
				for colour, n := range counts {
					if n == 0 {
						continue
					}
					var single [6]int32
					single[colour] = 1
					windowUnits = appendCastWindowAlt(windowUnits, id, ma, single, amt, lifeCost, genericCost)
				}
				continue
			}
			windowUnits = appendCastWindowAlt(windowUnits, id, ma, counts, amt, lifeCost, genericCost)
		}
	}
	return windowUnits
}

// castWindowProbeAbilities is the cast-window probe's own member walk: the
// same gates as availableManaAbilitiesForWindow, EXCEPT the live-pool
// payability gate, which a CR 601.2g window can satisfy later in the same
// window (appendAvailableManaAbilitiesGate's ignorePayable). InstantSpeed$
// True abilities stay withheld exactly as the shared window walk withholds
// them, so no ability the live payment window cannot activate is ever priced.
func (e *Engine) castWindowProbeAbilities(p state.PlayerID, id state.ObjID) []*cards.SA {
	var out []*cards.SA
	for _, ma := range e.appendAvailableManaAbilitiesGate(nil, nil, p, id, true) {
		if e.instantSpeedOnly(ma) {
			continue
		}
		out = append(out, ma)
	}
	return out
}

// castWindowAmount resolves a window mana ability's Amount$ the way the
// activation's own manaEffectAmount does -- the source face's SVar table and
// effects.Num's grammar -- but with the resolvability verdict the count
// ratchet demands: effects.NumResolvedStrict rejects a named SVar whose
// Count$ body the evaluator does not model, and the inline `Amount$ Count$...`
// form (which Strict does not itself re-check) is verified with EvalCountOK.
// A body that does not resolve deterministically, or resolves to zero or
// less, is not priced.
func (e *Engine) castWindowAmount(p state.PlayerID, source state.ObjID, o *state.Object, ma *cards.SA) (int32, bool) {
	raw := strings.TrimSpace(ma.Params["Amount"])
	if raw == "" {
		return 1, true
	}
	if v, err := strconv.Atoi(raw); err == nil {
		if v <= 0 {
			return 0, false
		}
		return int32(v), true
	}
	ctx := e.manaAmountCtx(p, source)
	effects.SetSVars(ctx, o.Face().SVars)
	n, ok := effects.NumResolvedStrict(e, ctx, ma, "Amount", 1)
	if !ok || n <= 0 {
		return 0, false
	}
	ref := raw
	if len(ref) > 1 && (ref[0] == '+' || ref[0] == '-') {
		ref = ref[1:]
	}
	if strings.HasPrefix(ref, "Count$") {
		if _, evaluated := effects.EvalCountOK(e, ctx, ref); !evaluated {
			return 0, false
		}
	}
	return n, true
}

// castWindowPayLifeCost reports whether c is a PayLife<N> activation cost
// this probe can price: the fixed life part and nothing else (bar the tap).
// An announced PayLife<X> (LifeX), a life-halving token and every other cost
// component are refused.
func castWindowPayLifeCost(c Cost) bool {
	return c.Life > 0 && c.Generic == 0 && c.Colored == (state.Mana{}) &&
		len(c.Sac) == 0 && castWindowOtherPartsAbsent(c)
}

// castWindowGenericCostShape reports whether c is a literal generic <N>
// activation cost this probe can price: exactly one literal generic mana and
// nothing else (bar the tap). A coloured pip, an {X} component or any other
// part is refused.
//
// The pool is deliberately NOT consulted here: the fee may be funded by an
// earlier same-window activation, which the cast-only eligibility search
// (castWindowReachable) proves. The live window offers this source once it is
// untapped and re-checks payability at each activation
// (manaAbilityPayablePool), so promising only a source the search can actually
// fund stays sound.
func castWindowGenericCostShape(c Cost) bool {
	return c.Generic > 0 && c.Colored == (state.Mana{}) &&
		len(c.Sac) == 0 && castWindowOtherPartsAbsent(c)
}

// castWindowSelfSacCost reports whether c is a self-sacrifice activation cost
// ("Sac<1/CARDNAME>") whose batch is deterministic: the only matching
// permanent is the source itself, so the interactive continuation
// (manaDiscardActivation) sacrifices it without a further ask. Any other Sac
// shape (multi-part, overlapping, multiple candidates) is refused.
func (e *Engine) castWindowSelfSacCost(p state.PlayerID, source state.ObjID, c Cost) bool {
	if len(c.Sac) != 1 || c.Sac[0].N != 1 || !strings.EqualFold(sacrificeMatchSpec(c.Sac[0].Spec), "CARDNAME") {
		return false
	}
	if c.Generic != 0 || c.Life != 0 || c.Colored != (state.Mana{}) || !castWindowOtherPartsAbsent(c) {
		return false
	}
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if e.sacrificeBlockedForCost(id, costCauseActivated) {
			continue
		}
		if e.matchesSpecFrom(c.Sac[0].Spec, id, p, source) {
			n++
		}
	}
	return n == 1
}

// castWindowOtherPartsAbsent reports whether c carries none of the cost
// components the paid-cost layer does not price. It is deliberately broader
// than manaFreeCost (which only needs a bare tap): every part whose payment
// needs a choice, an event or a resource this probe does not model is
// refused, as is any token the parser did not understand.
func castWindowOtherPartsAbsent(c Cost) bool {
	return len(c.Discard) == 0 && len(c.SubCounter) == 0 && len(c.AddCounter) == 0 &&
		len(c.Exile) == 0 && len(c.ExileFromTop) == 0 && len(c.Reveal) == 0 && len(c.RevealOrChoose) == 0 && len(c.RevealChosen) == 0 &&
		len(c.Behold) == 0 && len(c.TapPermanent) == 0 && len(c.Blight) == 0 &&
		len(c.Exert) == 0 && !c.Forage && !c.LifeHalfUp && len(c.Draw) == 0 &&
		len(c.Energy) == 0 && len(c.LifeX) == 0 && len(c.DamageYou) == 0 &&
		len(c.GainLife) == 0 &&
		len(c.Return) == 0 && len(c.PutToLib) == 0 && len(c.MoveToGrave) == 0 &&
		len(c.Mill) == 0 && len(c.Evidence) == 0 && len(c.RollDice) == 0 &&
		len(c.Unknown) == 0 && len(c.Hybrid) == 0 && len(c.Phyrexian) == 0 &&
		len(c.Twobrid) == 0 && len(c.HybridPhyrexian) == 0 && c.Snow == 0 && c.X == 0
}

// appendCastWindowAlt merges one production alternative into the unit for id
// (creating it if absent), so the affordability search can never tap the same
// permanent twice through two separate unit entries.
func appendCastWindowAlt(units []windowManaUnit, id state.ObjID, ma *cards.SA, counts [6]int32, amt, life, costGeneric int32) []windowManaUnit {
	idx := -1
	for i := range units {
		if units[i].id == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		units = append(units, windowManaUnit{id: id})
		idx = len(units) - 1
	}
	units[idx].alts = append(units[idx].alts, windowManaAlt{ma: ma, counts: counts, amt: amt, life: life, costGeneric: costGeneric})
	return units
}

// castWindowReachable is the cast-only CR 601.2g eligibility search. It is
// unlessManaReachable plus the one transition the cast window has and the
// attack/unless windows do not: an activation may PAY a literal generic fee
// (windowManaAlt.costGeneric) and a life fee before its production is added,
// and that fee may be funded by mana an earlier same-window activation
// already produced. The live window offers one source at a time and
// re-enters after each activation, so the search explores executable source
// orders and never claims a paid source it cannot fund. Units are visited in
// stable fee/id order, but every remaining source can be selected next;
// every shared-window alt carries costGeneric 0 so the attack/unless callers
// of unlessManaReachable are untouched.
//
// spellPool is the payer's restriction-adjusted pool for the SPELL (the same
// projection unlessManaReachable receives); activation fees are funded from
// the payer's real pool minus every restricted batch a non-matching
// descriptor would hide (unrestrictedWindowPool), never from spell-restricted
// mana, so the sequence is executable under the live activation gate. Every
// produced unit is unrestricted (both probe walks exclude RestrictValid$
// abilities).
func (e *Engine) castWindowReachable(p state.PlayerID, cost Cost, spellPool, snow state.Mana,
	typed [7]state.Mana, life int32, conv *manaConv, units []windowManaUnit) bool {
	payable := func(pool, snowPool state.Mana, lifeNow int32) bool {
		_, ok := cost.resolveManaWith(pool, snowPool, typed, lifeNow,
			e.payerGrantsPayLifeInsteadOfB(p), pipRider{}, conv)
		return ok
	}
	if payable(spellPool, snow, life) {
		return true
	}
	free := e.unrestrictedWindowPool(p)
	// Stable free-first ordering: a free alt sorts before a paid one; ties
	// retain the zone walk's order, with the id as a determinism guard.
	ordered := append([]windowManaUnit(nil), units...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0; j-- {
			if !castWindowUnitLess(ordered[j], ordered[j-1]) {
				break
			}
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	nodes := 0
	used := make([]bool, len(ordered))
	var rec func(pool, spellSnow, activationPool, activationSnow state.Mana, lifeLeft int32) bool
	rec = func(pool, spellSnow, activationPool, activationSnow state.Mana, lifeLeft int32) bool {
		if payable(pool, spellSnow, lifeLeft) {
			return true
		}
		nodes++
		if nodes > 1<<18 {
			return false
		}
		for i := range ordered {
			if used[i] {
				continue
			}
			for _, a := range ordered[i].alts {
				if a.life > lifeLeft {
					continue
				}
				// Pay a generic activation fee from mana the live activation
				// gate can spend. Track the same spent colours in the spell
				// pool; fees reduce that pool, they are not extra spell pips.
				activationFee, feeOK := (Cost{Generic: a.costGeneric}).resolveMana(
					activationPool, activationSnow, [7]state.Mana{}, lifeLeft, nil)
				if !feeOK {
					continue
				}
				spent := manaSub(activationPool, activationFee.pool)
				snowSpent := manaSub(activationSnow, activationFee.snow)
				nextPool := manaSub(pool, spent)
				nextSpellSnow := manaSub(spellSnow, snowSpent)
				nextActivation := activationFee.pool
				nextActivationSnow := activationFee.snow
				produced := a.mana()
				used[i] = true
				found := rec(manaAdd(nextPool, produced), nextSpellSnow,
					manaAdd(nextActivation, produced), nextActivationSnow, lifeLeft-a.life)
				used[i] = false
				if found {
					return true
				}
			}
		}
		return false
	}
	return rec(spellPool, snow, free, snow, life)
}

func manaSub(a, b state.Mana) state.Mana {
	var m state.Mana
	for i := range m {
		m[i] = a[i] - b[i]
		if m[i] < 0 {
			m[i] = 0
		}
	}
	return m
}

// castWindowUnitLess orders a unit before another when its cheapest
// alternative is cheaper in generic activation cost, tie-broken by zone id,
// giving castWindowReachable a stable traversal order.
func castWindowUnitLess(a, b windowManaUnit) bool {
	ka, kb := int32(-1), int32(-1)
	for _, x := range a.alts {
		if ka < 0 || x.costGeneric < ka {
			ka = x.costGeneric
		}
	}
	for _, x := range b.alts {
		if kb < 0 || x.costGeneric < kb {
			kb = x.costGeneric
		}
	}
	if ka != kb {
		return ka < kb
	}
	return a.id < b.id
}

// unrestrictedWindowPool is the payer's real floating pool minus every
// restricted batch (a non-empty Valid) a non-matching descriptor would hide.
// Only these units are guaranteed spendable on a mana-ability activation
// regardless of the activation's own restriction terms, so castWindowReachable
// funds activation fees from them and never from spell-restricted mana.
// Empty-Valid batches are unrestricted (Boseiju's AddsNoCounter$ provenance)
// and stay counted, matching manaAvailableFor's own rule.
func (e *Engine) unrestrictedWindowPool(p state.PlayerID) state.Mana {
	pl := e.G.Players[p]
	free := pl.Pool
	for _, r := range pl.RestrictedMana {
		if r.Valid == "" {
			continue
		}
		idx := state.ManaSlot(r.Color)
		if free[idx] < r.Amount {
			free[idx] = 0
		} else {
			free[idx] -= r.Amount
		}
	}
	return free
}

// striveAffordableTargets is the Decision.AffordableTargets hint for a Strive
// spell's target ask: the largest n in [1, max] whose total cost -- the
// resolved cost, n-1 Strive payments (CR 702.52a), the proposal's modifier
// snapshot, commander tax, Delve credit and announced Convoke -- the caster
// can provably pay from the pool plus the window's fixed productions
// (castWindowUnits, the same probe the target-discount gate trusts). It
// returns 0 (no hint) when the proposal carries no priceable Strive or max is
// at most one; it returns at least 1 otherwise, since one target adds no
// Strive charge and the ask's own gate already admitted the base cost. A pure
// read: nothing is emitted.
func (e *Engine) striveAffordableTargets(pc *pendingCast, max int) int {
	if pc.isAbility() || !pc.striveSet || max <= 1 {
		return 0
	}
	o := e.G.Obj(pc.card)
	if o == nil || o.Face() == nil {
		return 0
	}
	if _, ok := o.Face().KeywordParam("Strive"); !ok {
		return 0
	}
	sc := ParseCost(pc.striveParam)
	if len(sc.Unknown) > 0 {
		return 0
	}
	pl := e.G.Players[pc.player]
	var units []windowManaUnit
	unitsBuilt := false
	affordable := func(n int) bool {
		cost := pc.resolvedMana()
		for i := int(pc.striveUnits); i < n-1; i++ {
			cost = cost.Plus(sc)
		}
		cost = pc.mods.apply(cost)
		cost.Generic = addClampedGeneric(cost.Generic, int64(pc.taxGeneric))
		cost.Generic -= int32(len(pc.delve))
		if cost.Generic < 0 {
			cost.Generic = 0
		}
		convoked := e.applyConvoke(pc, cost)
		pay := paymentForCast(pc, convoked)
		rider := pipRider{anyColor: pc.mayPlayIgnore, anyType: pc.mayPlayIgnoreType}
		if e.manaFeasibleDescriptor(pc.player, pay, convoked, costMods{}, 0, 0, rider) {
			return true
		}
		if !convoked.hasManaPayment() {
			return false
		}
		if !unitsBuilt {
			units, unitsBuilt = e.castWindowUnits(pc), true
		}
		av := e.manaAvailableFor(pc.player, pay)
		return e.castWindowReachable(pc.player, convoked, av.pool, pl.Snow, av.typed, pl.Life,
			e.paymentConv(pc.player, pay.id, pay.class == paymentActivated), units)
	}
	// Ascending: the price is monotone in n, so the first unaffordable count
	// ends the walk and at most one exhaustive (failing) window search runs.
	best := 1
	for n := 2; n <= max; n++ {
		if !affordable(n) {
			break
		}
		best = n
	}
	return best
}
