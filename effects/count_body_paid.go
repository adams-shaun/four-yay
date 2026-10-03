package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// evalCountBodyPaid evaluates the second half of the original head switch:
// the paid-cost, chosen, remembered, life-log and commander heads
// (OptionalGenericCostPaid through ColorsColorIdentity). The dispatcher has
// already applied the space-less OptionalGenericCostPaid peel.
func evalCountBodyPaid(h Host, c *Ctx, g *state.Game, head, arg string, depth int) (int32, bool, bool) {
	switch evalCountBodyPaidc5b1Codes.Code(string(head)) {
	case evalCountBodyPaidc5b1OptionalGenericCostPaid:
		// OptionalCost's paid/unpaid branches are a boolean cast provenance.
		// The CastSA indirection has already bound c.Source to the cast object.
		// Each branch token is a numeric literal in the common case
		// (Count$OptionalGenericCostPaid.4.2), but Forge also writes another
		// SVar's value as the branch (Dragon's Fire's
		// `SVar:Y:Count$OptionalGenericCostPaid.X.3`, where the paid branch is
		// SVar X = Revealed$CardPower): a non-numeric token resolves as an
		// SVar$ indirection through the SAME runtime -> printed -> publication
		// precedence the SVar$ head uses, so the paired X/Y sizes from the
		// chosen card rather than collapsing to an unresolved zero.
		parts := strings.Split(strings.TrimSpace(arg), ".")
		if len(parts) < 2 {
			return 0, false, true
		}
		branch := func(tok string) (int32, bool) {
			tok = strings.TrimSpace(tok)
			if n, err := strconv.ParseInt(tok, 10, 32); err == nil {
				return int32(n), true
			}
			return evalCountExprOK(h, c, "SVar$"+tok, depth+1)
		}
		paid, ok1 := branch(parts[0])
		unpaid, ok2 := branch(parts[1])
		if !ok1 || !ok2 {
			return 0, false, true
		}
		if o := g.Obj(c.Source); o != nil && o.OptionalCostPaid {
			return paid, true, true
		}
		return unpaid, true, true
	case evalCountBodyPaidc5b1OffspringPaid:
		// CR 702.175a: whether the resolving spell's cast paid the optional
		// Offspring additional cost ("You may pay an additional [cost] as you
		// cast this spell. If you do, when this creature enters, create a 1/1
		// token copy of it."), carried by the pay-time CastInfo's
		// FlagOffspringPaid (rules/cast.go's payCast). The same provenance
		// read SquadPaid makes: read off the SOURCE -- the cast spell on the
		// stack, and in the keyword expansion's ETB trigger the permanent the
		// spell became (the stack->battlefield move preserves the field) -- so
		// a replay derives the same value; a copy of the spell was never cast
		// and reads 0 (so a minted 1/1 copy mints no further copies).
		if o := g.Obj(c.Source); o != nil {
			if o.OffspringPaid {
				return 1, true, true
			}
		}
		return 0, true, true
	case evalCountBodyPaidc5b1TimesKicked:
		// CR 702.43: the number of times the resolving spell's multikicker
		// cost was paid as it was cast, carried by the pay-time CastInfo's
		// FlagMultikicked Amount (rules/cast.go's multikickAsk and payCast).
		// The same provenance read ReplicatePaid makes: read off the SOURCE
		// (the cast spell on the stack; an ETB reader sees the PERMANENT it
		// became -- the stack->battlefield move preserves the field -- so a
		// replay derives the same count). A pending cast's count is seeded
		// into ctx.TimesKicked by targetBoundCtx when the spell's own
		// announcement ask reads a TimesKicked bound BEFORE payment has
		// stamped the object (Comet Storm's TargetMin/Max$ TargetsNum); a
		// COPY of the spell was never kicked and reads 0.
		if c.TimesKicked != 0 {
			return c.TimesKicked, true, true
		}
		if o := g.Obj(c.Source); o != nil {
			return o.TimesKicked, true, true
		}
		return 0, true, true
	case evalCountBodyPaidc5b1TimesMutated:
		// CR 702.140f: how many times the SOURCE permanent has mutated, folded
		// by events.Apply's Mutate case onto state.Object.TimesMutated and reset
		// when the pile leaves the battlefield. The "this creature" readers
		// (Vadrok, Apex of Thunder's "where X is the number of times this
		// creature has mutated") resolve against the mutated permanent, which
		// is c.Source at resolution.
		if o := g.Obj(c.Source); o != nil {
			return o.TimesMutated, true, true
		}
		return 0, true, true
	case evalCountBodyPaidc5b1Conspired:
		// CR 702.78a: 1 when the resolving spell's Conspire tap was actually
		// paid as it was cast, else 0. Carried by the pay-time CastInfo's
		// FlagConspired (rules/cast.go's conspireAsk/payCast). Same provenance
		// read ReplicatePaid makes -- the cast spell, the SOURCE -- so a
		// replay derives the same answer; a COPY of the spell was never cast
		// and reads 0. The keyword expansion's copy trigger uses this as its
		// Amount, so a declined Conspire (false) emits nothing.
		if o := g.Obj(c.Source); o != nil && o.Conspired {
			return 1, true, true
		}
		return 0, true, true
	case evalCountBodyPaidc5b1Converge:
		// CR 107.4f-family converge: the number of DISTINCT colours (WUBRG)
		// of mana actually spent to cast the resolving spell, carried by the
		// pay-time CastInfo's FlagConverged Amount (rules/cast.go's
		// payManaCastSpent capture and payCast's trailing CastInfo). Same
		// provenance read ReplicatePaid makes -- the cast spell, and in the
		// K:etbCounter ETB replacement the same object after the
		// stack->battlefield move preserves it -- so a replay derives the
		// same count; a copy of the spell was never cast and reads 0.
		if o := g.Obj(c.Source); o != nil {
			return o.ConvergeColours, true, true
		}
		return 0, true, true
	case evalCountBodyPaidc5b1CastTotalManaSpent:
		// CR 601.2h's payment: the TOTAL mana actually spent to cast the
		// resolving spell. The bare form (arg == "") is the spent delta's pips
		// summed over every slot, carried by the pay-time CastInfo's
		// FlagManaSpent Amount (rules/cast.go's payCast capture -- the
		// converge/replicate/multikick pattern; faceWantsCastSpend is the
		// heads-safety gate). Same provenance read Converge makes -- the cast
		// spell, and in the K:etbCounter ETB replacement the same object after
		// the stack->battlefield move preserves it -- so a replay derives the
		// same number; a copy of the spell was never cast and a cheated-in
		// permanent reads 0. The ref-property reader of OTHER casts
		// (TriggeredCard$CastTotalManaSpent, evalRefProperty) reads the
		// fire-time snapshot a trigger context carries, which payCast stamps
		// through the same gate's reader-out arm (triggeredCastSpendReaderOut)
		// when a battlefield permanent reads it.
		//
		// The FILTERED form `Count$CastTotalManaSpent <Type>` (tasks
		// castfilter1/castfilter2) counts only the mana spent whose SOURCE was
		// a permanent of <Type>. That per-unit producer provenance is carried
		// by the pool's parallel tallies and captured at payCast: <Type> ==
		// "Snow" resolves from the snow tally the pool has always carried (CR
		// 107.4h, Object.ManaSnowSpent), and <Type> == "Treasure"/"Cave"/
		// "Desert" (task castfilter2 — Marut, Bat Colony, Cataclysmic
		// Prospecting) resolves from Player.TypedMana's tagged units
		// (Object.ManaTreasureSpent / ManaCaveSpent / ManaDesertSpent). An
		// unknown <Type> — a producer type no tagging models — fails closed
		// to 0, which is strictly closer to the truth than the unfiltered
		// total the head used to return. Every resolved form is a real
		// per-unit count, not an approximation.
		if o := g.Obj(c.Source); o != nil {
			// The typed tags are the SAME table the producer-side tagging
			// reads (state.TypedManaTags), so a modelled type counts and a
			// type the pool cannot tag stays the fail-closed 0.
			return manaSpentTotalsOf(o).byTag(arg), true, true
		}
		return 0, true, true
	case evalCountBodyPaidc5b1ChosenNumber:
		// The Effect's SetChosenNumber$ binding (state.ContinuousEffect.ChosenNumber,
		// threaded into Ctx by rules' replCtx for effect-created replacement
		// bodies, task wildgrowth1: torgal_a_fine_hound / communal_brewing /
		// wildgrowth_archaic's "enters with an additional +1/+1 counter for
		// each ..." body). Bound ONCE when the Effect was created, against the
		// trigger's own context, so the body reads the frozen number wherever
		// the entry lands. The VERDICT is the bound flag (Ctx.ChosenNumberBound,
		// set only by rules' seedEffectReplCtx on effect-created matches): an
		// unbound context is UNRESOLVED, so every EvalCountOK consumer keeps
		// its pre-wildgrowth fail direction for the Choose-event population
		// whose ChosenNumber lives on state.Object.ChosenNumber and never
		// reaches here -- CheckSVarHolds fails open, a numeric filter RHS
		// (void's cmcEQX through resolveNumericRHS) never matches -- instead
		// of enforcing a meaningless zero. A bound zero is a real binding and
		// evaluates (torgal with no Dogs/Wolves on the board).
		if c.ChosenNumberBound {
			return c.ChosenNumber, true, true
		}
		// The Choose-event population (effects/choose.go's ChooseNumber,
		// the as-enters number choice): the answer lives on the SOURCE
		// object's ChosenNumber, folded from the logged choice. Aether
		// Spike's "counter unless its controller pays X, where X is the
		// chosen number", Galvanic Discharge, Die Young -- 52 supported
		// carriers whose amount read an unresolved zero (fuzz-cov3). A
		// source that never chose reads the field's zero, which is also
		// Forge's reading of an unset chosen number.
		if o := g.Obj(c.Source); o != nil {
			return o.ChosenNumber, true, true
		}
		return 0, false, true
	case evalCountBodyPaidc5b1ChosenSize:
		// Forge's Count$ChosenSize (CardUtil.getChosenCards().size()): the
		// number of CARDS the current resolution's ChooseCard chain has
		// chosen -- the same set Defined$ ChosenCard resolves (effects/
		// context.go's definedSpec case), read with the same precedence so a
		// count and a defined fetch can never disagree: the resolution's
		// bound Ctx.Chosen when it is live, else the source object's
		// event-backed Chosen list (the Choose "chosen" fold), which is what
		// a re-entry after a suspended ask reads. Player entries (a
		// ChoosePlayer's half) are not cards and do not count. A legitimate
		// zero (Feather, Radiant Arbiter's MinAmount$ 0 ask answered with
		// nothing) is exactly that -- the /Op suffix (/Times.2, the
		// UnlessCost$ CopyCost pricing) folds the zero like any other.
		// resolutionChosenCards is the shared chosen-card read (context.go's
		// Defined$ ChosenCard case, copy.go's DefinedTarget$ ChosenCard).
		chosen := resolutionChosenCards(g, c)
		n := int32(0)
		for _, t := range chosen {
			if !t.IsPlayer {
				n++
			}
		}
		return n, true, true
	case evalCountBodyPaidc5b1YourStartingLife:
		return h.StartingLife(), true, true
	case evalCountBodyPaidc5b1YourLifeTotal:
		if c.Controller < 0 || int(c.Controller) >= len(g.Players) {
			return 0, true, true
		}
		return g.Players[c.Controller].Life, true, true
	case evalCountBodyPaidc5b1PlayerCountPlayers:
		return int32(g.AliveCount()), true, true
	case evalCountBodyPaidc5b1PlayerCountOpponents:
		return int32(g.AliveCount() - 1), true, true
	case evalCountBodyPaidc5b1ThisTurnCast:
		// Task 17 (Storm): spells cast this turn by anyone, read off the
		// log via h.CastThisTurn() so a replay derives the same count. The
		// classic idiom is Count$ThisTurnCast/Minus1 (storm copies the spell
		// once per spell cast before it, i.e. everyone's casts minus itself).
		return int32(h.CastThisTurn()), true, true
	case evalCountBodyPaidc5b1TotalCommanderCastFromComman:
		// Both Forge spellings read the resolving controller's own
		// command-zone commander casts over the whole game — log-derived
		// through the Host like CastThisTurn, so replay derives the same
		// number and the same provenance read the CR 903.8 commander tax.
		return h.CommanderCastsFromCommandZone(c.Controller), true, true
	case evalCountBodyPaidc5b1RememberedNumber:
		// The chain's shared ExchangeLife rider (RememberOwnLoss$/
		// RememberDifference$) takes precedence: the pointer is re-attached to
		// every Ctx a suspension rebuilds, so the chained SubAbility$ reader
		// keeps the value the exchange transaction settled (Mister Negative's
		// draw count under a Lich suspension).
		if c.ExchangeMemory != nil && c.ExchangeMemory.Bound {
			return c.ExchangeMemory.Number, true, true
		}
		// Forge's Count$RememberedNumber is the executing ability's remembered
		// count -- the same list evalRememberedOK's Amount head reads, so it
		// applies the same capture exclusion: Forge's host remembered list is
		// never seeded with the event object the trigger fired on (a body reads
		// that through the separate Triggered* family). Use the one-home helper
		// rememberedExcludingCapture, exactly as the Amount head does, so a
		// firing trigger's ctx -- seeded Remembered == Captured == its event
		// capture -- does not overcount by that capture. A caller that already
		// passed a capture-excluded ctx (effImmediateTrigger's TriggerAmount$
		// read) is unchanged: the helper is idempotent there (its instance
		// capture is disjoint from its remembered set). Five corpus
		// ImmediateTrigger lines and 38 files elsewhere carry it.
		//
		// A DB$ FlipCoin RememberNumber$ publication takes precedence: Forge's
		// FlipCoinEffect writes the flip's rememberedNumber (Yusri's "If you
		// won five flips this way" gates Count$RememberedNumber), and the flip
		// resolves in the SAME chain the reader runs, so the remembered number
		// is the flip count, not the remembered-object count.
		if c.FlipMemory != nil && c.FlipMemory.RememberNumberKind != "" {
			return c.FlipMemory.RememberNumber, true, true
		}
		// A RememberCounteredCMC$ binding takes the same precedence: the
		// remembered number is the countered spell's mana VALUE (a counter
		// rider sizes off it), not a count of remembered entries.
		if c.RememberedCMCBound {
			return c.RememberedCMC, true, true
		}
		return int32(len(rememberedExcludingCapture(h, c))), true, true
	case evalCountBodyPaidc5b1RememberedSize:
		// Forge's RememberedSize is the HOST CARD's remembered list -- the
		// persistent list riders (RememberDiscarded$/RememberCountered$/
		// RememberChosen$/RememberControlled$/RememberSacrificed$) add to and
		// Cleanup's ClearRemembered$ clears. In this engine that list is the
		// SOURCE object's event-backed Remembered; the ctx-level list also
		// carries a trigger's captured event object, which is NOT part of
		// Forge's host list (the same exclusion iterationBase applies). A
		// resolution with no source object falls back to the ctx list.
		// RememberRevealed$ is one of the riders that fills this list: effReveal
		// writes BOTH halves (the rememberMilled discipline), so the source
		// read serves it too -- a ctx-first preference here would double-count
		// on every trigger resolution (Mind Maggots: ctx = the trigger's event
		// capture + its own RememberDiscarded$ entries).
		if o := g.Obj(c.Source); o != nil {
			return int32(len(o.Remembered)), true, true
		}
		return int32(len(c.Remembered)), true, true
	case evalCountBodyPaidc5b1LifeOppsLostThisTurn:
		// The total life the controller's OPPONENTS have lost this turn
		// (Rakdos, Lord of Riots). Each opponent's loss comes from the Host's
		// log-derived LifeLostThisTurn, so the count is replay-derivable.
		if c.Controller < 0 {
			return 0, true, true
		}
		var n int32
		for _, p := range g.AliveFrom(0) {
			if p != c.Controller {
				n += h.LifeLostThisTurn(p)
			}
		}
		return n, true, true
	case evalCountBodyPaidc5b1DamageOppsTakenThisTurn:
		// The total damage the controller's OPPONENTS were dealt this turn
		// (kw:Bloodthirst, CR 702.54, is the reader). Each opponent's take
		// comes from the Host's log-derived DamageTakenThisTurn (player-targeted
		// Damage events only, the same fold the TargetedPlayer$DamageThisTurn
		// head reads), so the count is replay-derivable. The sum answers BOTH
		// Bloodthirst shapes: a fixed N's condition ("an opponent was dealt
		// damage this turn") is the sum compared GT0 -- damage amounts are
		// positive, so a positive sum is exactly "at least one opponent was
		// dealt damage" -- and Bloodthirst X's amount ("enters with X +1/+1
		// counters, where X is the damage dealt to your opponents this turn",
		// Petrified Wood-Kin) is the sum itself.
		if c.Controller < 0 {
			return 0, true, true
		}
		var n int32
		for _, p := range g.AliveFrom(0) {
			if p != c.Controller {
				n += h.DamageTakenThisTurn(p)
			}
		}
		return n, true, true
	case evalCountBodyPaidc5b1LifeYouLostThisTurn:
		// The total life the controller LOST this turn -- Luminarch
		// Ascension's and Boarded Window's end-step CheckSVar$ gate ("if you
		// didn't lose life this turn"). The same log-derived Host fold
		// LifeOppsLostThisTurn sums over the opponents, read for the
		// controller alone, so a replay derives the same count. Unmodelled,
		// the gate failed closed and Luminarch Ascension never once gained a
		// quest counter (cardfuzz coverage audit).
		if c.Controller < 0 {
			return 0, true, true
		}
		return h.LifeLostThisTurn(c.Controller), true, true
	case evalCountBodyPaidc5b1Party:
		// CR 700.8: the controller's party -- one each of Cleric, Rogue,
		// Warrior and Wizard among the creatures they control, a creature
		// filling at most one role (partySize). 39 raw corpus carriers
		// (Archpriest of Iona, Squad Commander, Nimble Trapfinder's gates and
		// every "for each creature in your party" amount).
		if c.Controller < 0 {
			return 0, true, true
		}
		return partySize(g, c), true, true
	case evalCountBodyPaidc5b1LifeYouGainedThisTurn:
		// The total life the controller GAINED this turn — the CheckSVar$ gate
		// behind the "At the beginning of each end step, if you gained 4 or
		// more life this turn" family (Angelic Accord, Resplendent Angel,
		// Valkyrie Harbinger; 86 raw corpus Count$ lines). Folded from the log
		// through the Host's LifeGainedThisTurn like LifeOppsLostThisTurn, so
		// a replay derives the same count.
		if c.Controller < 0 {
			return 0, true, true
		}
		return h.LifeGainedThisTurn(c.Controller), true, true
	case evalCountBodyPaidc5b1YouDrewThisTurn:
		// The number of cards the controller DREW this turn — Elenda and
		// Azor's `SVar:Y:Count$YouDrewThisTurn` feeding `TokenAmount$ Y`
		// ("create a number of 1/1 black Vampire Knight creature tokens with
		// lifelink equal to the number of cards you've drawn this turn") and
		// the 29-carrier raw corpus family behind it. The same log fold the
		// PlayerCount$CardsDrawn property reads (Host.CardsDrawnThisTurn,
		// rules' bridge for the Smuggler's Share family), so the head and
		// the property can never drift apart; derived from the event log —
		// every events.Draw since the last TurnChange, the opening deal
		// naturally invisible behind turn one's own TurnChange — so a replay
		// derives the same count.
		if c.Controller < 0 {
			return 0, true, true
		}
		return h.CardsDrawnThisTurn(c.Controller), true, true
	case evalCountBodyPaidc5b1YouScryThisTurn:
		// The number of times the controller SCRIED / SURVEILLED this turn
		// (Forge's per-turn scry and surveil tallies): Desperate
		// Futurescribe, Proctor of Potential and Surveillance Phantasm's
		// "if you've scried or surveilled this turn" (Count$YouScryThisTurn
		// /Plus.Y over Count$YouSurveilThisTurn) and Darkblade Agent's
		// "as long as you've surveilled this turn". A log fold through the
		// Host (one events.Scry / events.Surveil record per completed
		// instruction since the last TurnChange), so a replay derives the
		// same count; an unbound controller is a modelled zero.
		if c.Controller < 0 {
			return 0, true, true
		}
		if head == "YouScryThisTurn" {
			return h.ScriedThisTurn(c.Controller), true, true
		}
		return h.SurveilledThisTurn(c.Controller), true, true
	case evalCountBodyPaidc5b1CountersAddedThisTurn:
		// Count$CountersAddedThisTurn <KIND> <Player> <ObjectSpec>.
		// Keep malformed or unsupported shapes unresolvable: CheckSVar
		// distinguishes that from an evaluated zero.
		parts := strings.Fields(arg)
		if len(parts) == 3 && c.Controller >= 0 && countersAddedThisTurnArgsKnown(parts[0], parts[1], parts[2]) {
			// The measured grammar needs only You and Source: Card.Self and
			// Card.EffectSource resolve from Source, while the other forms are
			// object/player predicates (countersAddedThisTurnArgsKnown's five
			// specs, none of which reads a colour, keyword or goad table).
			// Do not pass c.SpecContext here: its numeric-RHS resolver closes
			// over c, and handing that through the Host interface makes c
			// escape, allocating on the Derived hot path. TableSpecContext is
			// closure-free field copies.
			sc := c.TableSpecContext(c.Controller)
			return h.CountersAddedThisTurn(parts[0], parts[1], parts[2], sc), true, true
		}
	case evalCountBodyPaidc5b1CountersRemovedThisTurn:
		// Count$CountersRemovedThisTurn <KIND> <Player> — the number of counters
		// of KIND the named players have PAID or LOST this turn (Creative
		// Energy's cost engine: Blaster Hulk's `Amount$ Count$CountersRemovedThisTurn
		// ENERGY You` cast discount and Izzet Generatorium's `CheckSVar$ … |
		// SVarCompare$ GE4` paid-or-lost-four activation gate — 2 of the 3 corpus
		// carriers; the third, Churning Reservoir, counts OBJECT-counter removals
		// through an object spec plus a /Plus.X op, which this build does not
		// resolve: it falls through to the (0,false) tail below). A payment and a
		// loss both leave the player's pool through the ONE event shape a grant
		// uses — a negative-Amount PlayerCounterChange (rules/mana.go's PayEnergy
		// settle) — so the fold over that event since the last TurnChange, through
		// the Host like LifeLostThisTurn, is replay-derivable. KIND matches
		// case-insensitively (the same read YourCounters takes); the Player spec
		// resolves over the living seats through MatchesPlayerSpec (You/Opponent/
		// Player/Any and their qualifiers — a qualifier MatchesPlayerSpec does not
		// know fails closed to an empty set, the documented filter convention); a
		// spec whose BASE is not a player-spec base (an object spec) leaves the
		// head unresolvable — (0,false), never a fake evaluated zero.
		kind, spec, _ := strings.Cut(arg, " ")
		spec = strings.TrimSpace(spec)
		if kind != "" && spec != "" && playerSpecBaseKnown(spec) && c.Controller >= 0 {
			var n int32
			for _, p := range g.AliveFrom(0) {
				if MatchesPlayerSpec(g, spec, p, c.Controller) {
					n += h.CountersRemovedThisTurn(p, kind)
				}
			}
			return n, true, true
		}
	case evalCountBodyPaidc5b1YourTurns:
		// How many of the game's turns have begun with the controller as the
		// active player, current turn included (Serra Avenger's "your first,
		// second, or third turns of the game"). Log-derived through the Host
		// like LifeOppsLostThisTurn, so a replay derives the same number.
		return h.TurnsTaken(c.Controller), true, true
	case evalCountBodyPaidc5b1CardPower:
		if lki, ok := sacrificedSourceLKI(g, c); ok {
			return lki.Power, true, true // sacrificed by this ability: LKI
		}
		if o := g.Obj(c.Source); o != nil && o.Face() != nil {
			return refPower(h, o, false), true, true
		}
		return 0, true, true
	case evalCountBodyPaidc5b1CardBasePower:
		if o := g.Obj(c.Source); o != nil && o.Face() != nil {
			return h.Chars(c.Source).BasePower, true, true
		}
		return 0, true, true
	case evalCountBodyPaidc5b1CardToughness:
		if lki, ok := sacrificedSourceLKI(g, c); ok {
			return lki.Toughness, true, true // sacrificed by this ability: LKI
		}
		if o := g.Obj(c.Source); o != nil && o.Face() != nil {
			return refToughness(h, o, false), true, true
		}
		return 0, true, true
	case evalCountBodyPaidc5b1AttackersDeclared:
		// Count$AttackersDeclared: the attackers declared THIS turn — the Raid
		// family's "attacked this turn" read (Bloodsoaked Champion's
		// CheckSVar$ RaidTest activation gate plus 10 ConditionCheckSVar$
		// bodies). Folded from the event log through the Host (rules'
		// Engine.AttackersThisTurn) so a replay derives the identical number,
		// the same discipline CastThisTurn takes.
		return int32(h.AttackersThisTurn()), true, true
	case evalCountBodyPaidc5b1ColorsColorIdentity:
		// Count$ColorsColorIdentity: the number of colours in the resolving
		// controller's commanders' colour identity (War Room's
		// "SVar:X:Count$ColorsColorIdentity" driving "{3}, {T}, Pay life equal
		// to the number of colors in your commanders' color identity: Draw a
		// card", the corpus's only carrier). Read through the Host's
		// CommanderIdentityColourCount like the other log/state-derived heads
		// (LifeLostThisTurn, TurnsTaken), so a replay derives the identical
		// count. An empty identity (no commander, or a colourless one) is a
		// real, resolvable 0 — the gate that withholds the ability outside the
		// Commander format is ActivationGameTypes$, not this count.
		if c.Controller < 0 {
			return 0, true, true
		}
		return int32(h.CommanderIdentityColourCount(c.Controller)), true, true
	}
	return 0, false, false
}

const (
	evalCountBodyPaidc5b1OptionalGenericCostPaid      uint16 = 1  // "OptionalGenericCostPaid"
	evalCountBodyPaidc5b1OffspringPaid                uint16 = 2  // "OffspringPaid"
	evalCountBodyPaidc5b1TimesKicked                  uint16 = 3  // "TimesKicked"
	evalCountBodyPaidc5b1TimesMutated                 uint16 = 4  // "TimesMutated"
	evalCountBodyPaidc5b1Conspired                    uint16 = 5  // "Conspired"
	evalCountBodyPaidc5b1Converge                     uint16 = 6  // "Converge"
	evalCountBodyPaidc5b1CastTotalManaSpent           uint16 = 7  // "CastTotalManaSpent"
	evalCountBodyPaidc5b1ChosenNumber                 uint16 = 8  // "ChosenNumber"
	evalCountBodyPaidc5b1ChosenSize                   uint16 = 9  // "ChosenSize"
	evalCountBodyPaidc5b1YourStartingLife             uint16 = 10 // "YourStartingLife"
	evalCountBodyPaidc5b1YourLifeTotal                uint16 = 11 // "YourLifeTotal"
	evalCountBodyPaidc5b1PlayerCountPlayers           uint16 = 12 // "PlayerCountPlayers"
	evalCountBodyPaidc5b1PlayerCountOpponents         uint16 = 13 // "PlayerCountOpponents"
	evalCountBodyPaidc5b1ThisTurnCast                 uint16 = 14 // "ThisTurnCast"
	evalCountBodyPaidc5b1TotalCommanderCastFromComman uint16 = 15 // "TotalCommanderCastFromCommandZone", "CommanderCastFromCommandZone"
	evalCountBodyPaidc5b1RememberedNumber             uint16 = 16 // "RememberedNumber"
	evalCountBodyPaidc5b1RememberedSize               uint16 = 17 // "RememberedSize"
	evalCountBodyPaidc5b1LifeOppsLostThisTurn         uint16 = 18 // "LifeOppsLostThisTurn"
	evalCountBodyPaidc5b1DamageOppsTakenThisTurn      uint16 = 19 // "DamageOppsTakenThisTurn"
	evalCountBodyPaidc5b1LifeYouLostThisTurn          uint16 = 20 // "LifeYouLostThisTurn"
	evalCountBodyPaidc5b1Party                        uint16 = 21 // "Party"
	evalCountBodyPaidc5b1LifeYouGainedThisTurn        uint16 = 22 // "LifeYouGainedThisTurn"
	evalCountBodyPaidc5b1YouDrewThisTurn              uint16 = 23 // "YouDrewThisTurn"
	evalCountBodyPaidc5b1YouScryThisTurn              uint16 = 24 // "YouScryThisTurn", "YouSurveilThisTurn"
	evalCountBodyPaidc5b1CountersAddedThisTurn        uint16 = 25 // "CountersAddedThisTurn"
	evalCountBodyPaidc5b1CountersRemovedThisTurn      uint16 = 26 // "CountersRemovedThisTurn"
	evalCountBodyPaidc5b1YourTurns                    uint16 = 27 // "YourTurns"
	evalCountBodyPaidc5b1CardPower                    uint16 = 28 // "CardPower"
	evalCountBodyPaidc5b1CardBasePower                uint16 = 29 // "CardBasePower"
	evalCountBodyPaidc5b1CardToughness                uint16 = 30 // "CardToughness"
	evalCountBodyPaidc5b1AttackersDeclared            uint16 = 31 // "AttackersDeclared"
	evalCountBodyPaidc5b1ColorsColorIdentity          uint16 = 32 // "ColorsColorIdentity"
)

var evalCountBodyPaidc5b1Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "OptionalGenericCostPaid", Val: evalCountBodyPaidc5b1OptionalGenericCostPaid},
	state.StrEntry[uint16]{Key: "OffspringPaid", Val: evalCountBodyPaidc5b1OffspringPaid},
	state.StrEntry[uint16]{Key: "TimesKicked", Val: evalCountBodyPaidc5b1TimesKicked},
	state.StrEntry[uint16]{Key: "TimesMutated", Val: evalCountBodyPaidc5b1TimesMutated},
	state.StrEntry[uint16]{Key: "Conspired", Val: evalCountBodyPaidc5b1Conspired},
	state.StrEntry[uint16]{Key: "Converge", Val: evalCountBodyPaidc5b1Converge},
	state.StrEntry[uint16]{Key: "CastTotalManaSpent", Val: evalCountBodyPaidc5b1CastTotalManaSpent},
	state.StrEntry[uint16]{Key: "ChosenNumber", Val: evalCountBodyPaidc5b1ChosenNumber},
	state.StrEntry[uint16]{Key: "ChosenSize", Val: evalCountBodyPaidc5b1ChosenSize},
	state.StrEntry[uint16]{Key: "YourStartingLife", Val: evalCountBodyPaidc5b1YourStartingLife},
	state.StrEntry[uint16]{Key: "YourLifeTotal", Val: evalCountBodyPaidc5b1YourLifeTotal},
	state.StrEntry[uint16]{Key: "PlayerCountPlayers", Val: evalCountBodyPaidc5b1PlayerCountPlayers},
	state.StrEntry[uint16]{Key: "PlayerCountOpponents", Val: evalCountBodyPaidc5b1PlayerCountOpponents},
	state.StrEntry[uint16]{Key: "ThisTurnCast", Val: evalCountBodyPaidc5b1ThisTurnCast},
	state.StrEntry[uint16]{Key: "TotalCommanderCastFromCommandZone", Val: evalCountBodyPaidc5b1TotalCommanderCastFromComman},
	state.StrEntry[uint16]{Key: "CommanderCastFromCommandZone", Val: evalCountBodyPaidc5b1TotalCommanderCastFromComman},
	state.StrEntry[uint16]{Key: "RememberedNumber", Val: evalCountBodyPaidc5b1RememberedNumber},
	state.StrEntry[uint16]{Key: "RememberedSize", Val: evalCountBodyPaidc5b1RememberedSize},
	state.StrEntry[uint16]{Key: "LifeOppsLostThisTurn", Val: evalCountBodyPaidc5b1LifeOppsLostThisTurn},
	state.StrEntry[uint16]{Key: "DamageOppsTakenThisTurn", Val: evalCountBodyPaidc5b1DamageOppsTakenThisTurn},
	state.StrEntry[uint16]{Key: "LifeYouLostThisTurn", Val: evalCountBodyPaidc5b1LifeYouLostThisTurn},
	state.StrEntry[uint16]{Key: "Party", Val: evalCountBodyPaidc5b1Party},
	state.StrEntry[uint16]{Key: "LifeYouGainedThisTurn", Val: evalCountBodyPaidc5b1LifeYouGainedThisTurn},
	state.StrEntry[uint16]{Key: "YouDrewThisTurn", Val: evalCountBodyPaidc5b1YouDrewThisTurn},
	state.StrEntry[uint16]{Key: "YouScryThisTurn", Val: evalCountBodyPaidc5b1YouScryThisTurn},
	state.StrEntry[uint16]{Key: "YouSurveilThisTurn", Val: evalCountBodyPaidc5b1YouScryThisTurn},
	state.StrEntry[uint16]{Key: "CountersAddedThisTurn", Val: evalCountBodyPaidc5b1CountersAddedThisTurn},
	state.StrEntry[uint16]{Key: "CountersRemovedThisTurn", Val: evalCountBodyPaidc5b1CountersRemovedThisTurn},
	state.StrEntry[uint16]{Key: "YourTurns", Val: evalCountBodyPaidc5b1YourTurns},
	state.StrEntry[uint16]{Key: "CardPower", Val: evalCountBodyPaidc5b1CardPower},
	state.StrEntry[uint16]{Key: "CardBasePower", Val: evalCountBodyPaidc5b1CardBasePower},
	state.StrEntry[uint16]{Key: "CardToughness", Val: evalCountBodyPaidc5b1CardToughness},
	state.StrEntry[uint16]{Key: "AttackersDeclared", Val: evalCountBodyPaidc5b1AttackersDeclared},
	state.StrEntry[uint16]{Key: "ColorsColorIdentity", Val: evalCountBodyPaidc5b1ColorsColorIdentity},
)
