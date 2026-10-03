package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Token", effToken) }

// effToken creates TokenAmount$ tokens of each TokenScript$ (a comma-
// separated list of Game.Tokens stems) for TokenOwner$ (the controller by
// default; the switch below also resolves Opponent -- deliberately only the
// FIRST opponent, not the fan-out the grammar would give -- Player ("each
// player creates ...", every ALIVE seat in seat order via AliveFrom(0)), the
// two trig:Vote vote-carrier sets (one token per voter), Imprinted/
// ImprintedController, RememberedOwner and ThisTargetedPlayer as explicit
// bespoke cases. Every OTHER TokenOwner$ spelling -- the whole
// Targeted*/Triggered*/Remembered*/Chosen* referent family and the qualified
// Player.<qualifier> forms -- is a Forge player selector resolved through
// the SAME shared grammar every other player-valued parameter in this
// package uses (tokenOwnerPlayers over definedSpec, the ordinary Defined$
// resolver): the players, or the controllers of the object targets, the
// spelling names. Only a value that grammar does not know at all falls back
// to the controller, and it says so with a Note so the gap is visible rather
// than silently papered over.
//
// Every token is its own TokenCreate event, in the order this loop visits
// them (outer: TokenScript$ stems left to right; inner: TokenAmount$ copies
// in index order) -- never a map-range order, so replay reproduces the same
// objects with the same IDs. events.Apply's TokenCreate case is what
// actually mints the object from Game.Tokens; this function only ever
// proposes the event through h.Emit, so it stays a pure proposer like every
// other primitive in this package (Ruling: effects never writes state.Game
// directly, only events.Apply does).
//
// An unknown TokenScript$ key is a Note ("unknown token script <key>") and
// creates nothing -- a Link-time diagnostic ought to have already caught a
// script that names a stem outside Game.Tokens, but Resolve's own totality
// stance (never panic on untrusted/unexpected input) applies here too.
//
// TokenTapped$ True makes every token this call creates enter tapped: the
// ordinary Tap event with the "entered tapped" text the ChangeZone paths
// emit for their own Tapped$ (an entry state, not the CR 701.21a event of
// becoming tapped) lands right after each mint, so the token is on the
// battlefield untapped for exactly one folded event and then tapped.
// TokenAttacking$ True (Mobilize, Kari Zev) makes every token this call
// creates enter tapped and attacking the combat's defending player through
// the appended events.TokenAttacks kind; see the implementation comment at
// the read below for the no-defender degrade. The other TokenAttacking$
// selector forms stay census-free gaps.
//
// TokenPower$/TokenToughness$ set the token's P/T from a dynamic value
// (Skyclave Apparition's X/X Illusion, SVar:X:Remembered$CardManaCost): the
// value resolves through the ordinary Num grammar (a signed literal, an SVar
// body, an inline Count$...), and the set is a PERMANENT layer-7b SubSet
// continuous effect on the token itself -- the same registration shape
// Amass uses for its type grant, so it is replay-rebuilt by re-execution
// and dies with the token's battlefield presence. A side the script does
// not name keeps the token script's printed value; a named side the Num
// grammar cannot resolve is a loud Note and the whole set is skipped (the
// token keeps its printed P/T, which for the corpus's */* scripts means a
// 0/0 the zero-toughness SBA sweeps -- the honest degrade, never a silent
// wrong value).
//
// RememberTokens$ True hands every object this call actually created to the
// rest of the chain via Ctx.Remembered, which is how Living Weapon's own
// keyword expansion (cards/keywords.go) attaches the Germ it just made: its
// SubAbility is `DB$ Attach | Defined$ Remembered`, and Resolve walks Sub
// with the SAME *Ctx, so appending here is what that Attach later reads.
//
// RememberOriginalTokens$ True (Forum Filibuster, Diregraf Horde, Dain
// Ironfoot and 5 more corpus carriers, all `DB$ Token` shapes) takes the SAME
// branch. Forge's own distinction is original-token-vs-post-replacement
// mint, and in this build that distinction collapses in favour of the flag:
// every mint this call proposes IS an original token, and EmitTokenCreate
// hands back every mint the event actually created, so each is remembered.
// All 8 carriers are plain `DB$ Token` lines with no `R:` replacement in
// reach (measured at the corpus pin), so the collapse is corpus-inert today.
// tokenOwnerPlayers resolves a TokenOwner$ value through the ordinary
// Defined$ player-selector grammar (definedSpec), the one resolver every
// player-valued parameter in this package reads. Player entries pass
// through; an OBJECT selection (Targeted, Remembered, TriggeredCard, ...)
// contributes its controller, exactly Forge's AbilityUtils.getDefinedPlayers
// reading of a player selector. ok is false only when the grammar does not
// know the spelling at all, which is the caller's signal to keep the
// controller and say so.
func tokenOwnerPlayers(h Host, c *Ctx, spec string) ([]state.PlayerID, bool) {
	// These two corpus spellings are player selectors whose qualifier is
	// specific to TokenOwner. Keep them here rather than widening the general
	// Defined$ grammar: they are measured residual forms, and both scan alive
	// seats explicitly so their result is deterministic.
	if spec == "Player.controlsEnchantment,controlsArtifact" {
		return tokenOwnersControllingAny(h.Game(), []string{"Enchantment", "Artifact"}), true
	}
	if spec == "Player.controlsCreature_EQX" {
		// Gor Muldrak's X is an SVar body, not a literal. Evaluate that one
		// TokenOwner-specific read with the verdict-aware count resolver: an
		// unmodelled X matches nobody rather than becoming a fake zero. Do not
		// tighten NumResolved's SVar verdict globally; other explicit numeric
		// parameters deliberately use its zero-value degradation.
		if c == nil || c.SVars == nil {
			return nil, true
		}
		body, ok := c.SVars["X"]
		if !ok {
			return nil, true
		}
		threshold, resolved := EvalCountOK(h, c, body)
		if !resolved {
			return nil, true
		}
		var owners []state.PlayerID
		for _, p := range h.Game().AliveFrom(0) {
			if tokenControlledCount(h.Game(), c, p, "Creature") == threshold {
				owners = append(owners, p)
			}
		}
		return owners, true
	}
	// TargetedController is evaluated from the target's LKI. The target may
	// have been destroyed by the parent SA before this chained Token runs;
	// events.Apply intentionally resets a departed object's live Controller to
	// Owner, so prefer the controller captured when Resolve began.
	if spec == "TargetedController" && c != nil && c.TargetControllerLKI != nil {
		owners := make([]state.PlayerID, 0, len(c.Targets))
		for _, target := range c.Targets {
			if target.IsPlayer {
				continue
			}
			if controller, ok := c.TargetControllerLKI[target.Obj]; ok {
				owners = append(owners, controller)
				continue
			}
			if object := h.Game().Obj(target.Obj); object != nil {
				owners = append(owners, object.Controller)
			}
		}
		return owners, true
	}
	ts, ok := definedSpec(h, c, spec)
	if !ok {
		return nil, false
	}
	ps := controllersOf(h.Game(), ts)
	out := make([]state.PlayerID, 0, len(ps))
	for _, t := range ps {
		if t.IsPlayer {
			out = append(out, t.Player)
		}
	}
	return out, true
}

// tokenOwnersControllingAny returns alive players controlling at least one
// matching permanent. The outer seat walk, rather than object iteration, is
// the ordering contract for TokenCreate events.
func tokenOwnersControllingAny(g *state.Game, specs []string) []state.PlayerID {
	var owners []state.PlayerID
	for _, p := range g.AliveFrom(0) {
		if tokenControlledCount(g, nil, p, specs...) > 0 {
			owners = append(owners, p)
		}
	}
	return owners
}

// tokenControlledCount counts battlefield permanents controlled by p. The
// ordinary filter matcher is used so the read has the same type semantics as
// Count$Valid, while the player walk remains local to this TokenOwner form.
func tokenControlledCount(g *state.Game, c *Ctx, p state.PlayerID, specs ...string) int32 {
	var n int32
	var sc SpecContext
	if c != nil {
		sc = c.SpecContext(p)
	} else {
		sc.You = p
	}
	for _, id := range g.Zone(state.ZBattlefield, p) {
		o := g.Obj(id)
		if o == nil || o.Controller != p {
			continue
		}
		for _, spec := range specs {
			if matchesZoneSpecCtx(g, spec, id, sc, state.ZBattlefield) {
				n++
				break
			}
		}
	}
	return n
}

// tokenAttackingPlayers resolves a TokenAttacking$ value's PLAYER half to the
// defending seats the minted permanents attack. Forge writes the value as one
// or more ` & `-joined selectors (Adeline, Resplendent Cathar's
// `RememberedPlayer & Valid Planeswalker.ControlledBy Remembered`, the RepeatEach
// loop's per-opponent token); each player selector is read through the SHARED
// referent grammar (definedSpec, the same reader TokenOwner and UnlessPayer
// use), so a TokenAttacking$ spelling and a TokenOwner$ spelling can never
// disagree about what a seat means. `RememberedPlayer`/`RememberedPlayers` are
// the plain Remembered family and read the resolution's remembered PLAYERS,
// exactly the binding effects.effectRememberedPlayers documents for the
// RepeatEach loop's current subject -- a remembered CARD contributes no seat
// (effects.plainRememberedSelector's Forge getDefinedPlayers rule).
//
// objectArm reports an arm the grammar could not resolve to a seat: a `Valid
// <filter>` object selector (the "or a planeswalker they control" half) is
// unmodelled -- the engine's defenders are players only (the pw1 combat-damage
// gap) -- so the caller says so loudly instead of guessing a planeswalker as a
// player. seats[0] is the deterministic first resolved seat; the corpus's
// carriers all resolve exactly one.
func tokenAttackingPlayers(h Host, c *Ctx, raw string) (seats []state.PlayerID, objectArm bool) {
	g := h.Game()
	addSeat := func(p state.PlayerID) {
		if int(p) < 0 || int(p) >= len(g.Players) {
			return
		}
		for _, have := range seats {
			if have == p {
				return
			}
		}
		seats = append(seats, p)
	}
	for _, part := range strings.Split(raw, " & ") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		var ts []state.Target
		switch tokenAttackingPlayers7c51Codes.Code(string(p)) {
		case tokenAttackingPlayers7c51RememberedPlayer:
			ts = resolvedRemembered(h, c)
		default:
			// A `Valid <filter>` arm is an OBJECT selector by construction --
			// the "or a planeswalker they control" half -- and the engine's
			// defenders are players only, so it is unmodelled regardless of
			// whether any object currently matches. Reporting it from its
			// resolved (possibly empty) population would hide the arm on an
			// empty board, which is exactly Adeline's one-opponent case.
			if p == "Valid" || strings.HasPrefix(p, "Valid ") {
				objectArm = true
				continue
			}
			resolved, ok := definedSpec(h, c, p)
			if !ok {
				objectArm = true
				continue
			}
			ts = resolved
		}
		// The plain Remembered family reads remembered PLAYERS only: a
		// remembered CARD contributes no seat (Forge's getDefinedPlayers
		// rule, the same one effects.plainRememberedSelector encodes), so its
		// object entries drop out silently. Any OTHER selector that resolves
		// to an object names a planeswalker-like defender -- the
		// "or a planeswalker they control" arm -- which the engine's
		// player-only defenders cannot represent, so it is reported for a
		// loud Note, never mapped to the object's controller as a guessed
		// seat.
		plain := plainRememberedSelector(p)
		for _, t := range ts {
			if !t.IsPlayer {
				if !plain {
					objectArm = true
				}
				continue
			}
			addSeat(t.Player)
		}
	}
	return seats, objectArm
}

// tokenAttackingRider classifies a TokenAttacking$ body's attacking rider for
// the TWO mint paths that carry it (api:Token's effToken and api:CopyPermanent's
// effCopyPermanent): literal True attacks the firing Attacks trigger's captured
// defender, a selector value resolves its player half through
// tokenAttackingPlayers, and an unresolvable value degrades with ONE loud Note
// naming the limitation -- never a guessed defender, never silence. noun is
// the minted object's word ("token"/"copy") for the note text. The returned
// bool is whether every mint should be marked attacking, and state.PlayerID the
// defender it attacks.
func tokenAttackingRider(h Host, c *Ctx, attack, noun string) (bool, state.PlayerID) {
	switch {
	case strings.EqualFold(attack, "True") && c.DefendingPlayer.IsPlayer:
		return true, c.DefendingPlayer.Player
	case strings.EqualFold(attack, "True"):
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "TokenAttacking$ with no defending player in context; the " + noun + " enters but does not attack"})
		return false, 0
	}
	seats, objectArm := tokenAttackingPlayers(h, c, attack)
	if len(seats) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "TokenAttacking$ " + attack + " is not implemented; the " + noun + " enters but does not attack"})
		return false, 0
	}
	if objectArm {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: "TokenAttacking$ " + attack + ": the planeswalker arm is not implemented; the " + noun + " attacks the named player"})
	}
	return true, seats[0]
}

// tokenRememberedTargets resolves the set TokenRemembered$ (name, trimmed)
// attaches to each minted token. It is shared by Token and CopyPermanent,
// whose two mint paths must persist the same event-backed memory; each passes
// its compiler's TokenRemembered$ value.
func tokenRememberedTargets(h Host, c *Ctx, name string) []state.Target {
	if name == "" {
		return nil
	}
	if strings.EqualFold(name, "ExiledCards") {
		return append([]state.Target(nil), c.Remembered...)
	}
	return DefinedSpec(h, c, name)
}

// effToken creates the requested token scripts and applies their token riders.
func effToken(h Host, c *Ctx, sa *cards.SA) {
	tp := TokenOf(sa)
	if rest := resumingMint(c, sa); rest != nil {
		// A re-entry after a parked mint's answer (rules' "token_rest"
		// frame): everything before the loop already ran on the first pass
		// and its values are frozen in the job, so only the owed units run.
		job := rest.Job
		runTokenMints(h, c, sa, tp, &job, rest.Next, rest.Parked, rest.Minted)
		return
	}
	// Once per call: a re-entry already noted on its first pass.
	noteUnreadParams(h, c, "Token", tp.Unread)
	g := h.Game()
	n := numText(h, c, tp.Amount, 1)
	// owners is the per-mint owner list: one entry for every shape the
	// switch resolves, so the mint loop below can give EACH owner its own
	// tokens when a spelling names several (trig:Vote's
	// TokenOwner$ TriggeredOpponentVotedSame -- "each opponent who voted
	// ... creates a Treasure"). Every pre-existing shape resolves exactly
	// one owner, so the loop is byte-identical for them.
	owners := []state.PlayerID{c.Controller}
	switch v := tp.Owner; v {
	case "", "You":
		// The default: the controller, already set above.
	case "Opponent":
		for _, p := range g.AliveFrom(c.Controller) {
			if p != c.Controller {
				owners = []state.PlayerID{p}
				break
			}
		}
	case "Player":
		// "Each player creates ..." (Rendmaw, Creaking Nest, Marching
		// Duodrone, Grismold the Dreadsower and 10 more corpus carriers of
		// the bare spelling): EVERY alive seat creates TokenAmount$ tokens,
		// including the resolving controller. The order is AliveFrom(0) --
		// seat order from seat 0, NOT AliveFrom(c.Controller) -- so the
		// mint sequence and the token ids are deterministic and replay-stable
		// regardless of who is resolving, and a dead seat creates nothing
		// (a player who has lost no longer creates; the alive set is the
		// same one every other per-player walk uses). The mint loop below
		// gives each owner its own TokenAmount$ copies, so a TokenAmount$ X
		// carrier (Edge Rover's "each player creates X ...") reads X per
		// player. The qualified Player.<qualifier> spellings stay in the
		// default arm above.
		owners = g.AliveFrom(0)
	case "TriggeredOpponentVotedSame", "TriggeredOpponentVotedDiff":
		// The vote-carrier referent (trig:Vote): each player in the List$
		// set the firing trigger captured creates its own token. An EMPTY
		// set creates nothing -- "each opponent who voted ..." is vacuous
		// when nobody did, never a token for the controller (the old
		// unrecognised-owner fallback would have minted a wrong one and
		// noted).
		ps := c.TriggeredOpponentsVotedSame
		if v == "TriggeredOpponentVotedDiff" {
			ps = c.TriggeredOpponentsVotedDiff
		}
		owners = make([]state.PlayerID, 0, len(ps))
		owners = append(owners, ps...)
	case "Imprinted", "ImprintedController":
		// Forge's TokenOwner$ ImprintedController: the controller of the
		// RepeatEach iteration's current imprinted subject, and only that
		// (UseImprinted$ binds the subject). The ordinary Defined resolver owns
		// the selector, including the last-known controller a ChangeZone's
		// RememberLKI$ captured -- Curse of the Swine's Boar per exiled
		// creature. A subject whose controller cannot be resolved leaves the
		// controller default, the same silent degrade the other miss cases take.
		for _, t := range DefinedSpec(h, c, v) {
			if t.IsPlayer {
				owners = []state.PlayerID{t.Player}
				break
			}
		}
	case "RememberedOwner":
		// The owner of the first remembered OBJECT (Skyclave Apparition's
		// "the exiled card's owner creates the token"). The same group the
		// Remembered$ SVar head reads -- the source card's shared list first,
		// the firing trigger's own referent capture excluded -- so the
		// exiled card, not the leaving host, is the first candidate. No
		// remembered object (the ability's own ConditionPresent$ gate should
		// have kept this call from running at all) keeps the controller.
		for _, t := range rememberedWithSource(h, c) {
			if !t.IsPlayer {
				if o := g.Obj(t.Obj); o != nil {
					owners = []state.PlayerID{o.Owner}
					break
				}
			}
		}
	case "ThisTargetedPlayer":
		// The player one of the charm's modes targeted (Shadrix Silverquill,
		// the duo cycle, verdant/ashlings/prismari command -- 7 corpus files
		// carry the spelling on a Token): the first player-kind entry of this
		// resolution's own target list. With the cross-mode TargetUnique split
		// (effCharm's charmCrossModeRun) that list is exactly the running
		// mode's own target, so the token is created BY the player the mode
		// targeted, not by the ability's controller. A resolution with no
		// player target keeps the controller, the same silent degrade the
		// other miss cases here take.
		//
		// The list is read through Defined, not raw Ctx.Targets: an SA whose
		// own ValidTgts$ was answered by the mid-resolution pre-ask carries
		// that answer in Ctx.PickedTargets while its body dispatches, and
		// Ctx.Targets still holds the PARENT's target (Cybernetica Datasmith's
		// root Draw targets player A, its Token SubAbility's TargetUnique$
		// ask answers player B -- reading Ctx.Targets here created the token
		// under A). For a charm mode PickedTargets is nil and Defined returns
		// Ctx.Targets, exactly the historical read.
		for _, t := range Defined(h, c, sa) {
			if t.IsPlayer {
				owners = []state.PlayerID{t.Player}
				break
			}
		}
	default:
		// Every remaining player-selector spelling -- TargetedController,
		// TargetedPlayer, TriggeredCardController, TriggeredPlayer,
		// RememberedController, ChosenPlayer, Player, ImprintedController, ...
		// -- resolves through the SAME shared Defined$ grammar every other
		// player-valued parameter uses, so a new spelling Forge adds is covered
		// by definedSpec without a second list here. Object selectors (Targeted,
		// Remembered) contribute their controllers, which is Forge's
		// getDefinedPlayers reading of a player selector. Only a value the
		// grammar does not know at all keeps the controller fallback, under the
		// loud Note. A recognised selector that resolves to NOBODY (a targeted
		// permanent that left play before the chained Token, an empty referent
		// set) creates no token and emits no Note -- the fail-closed direction,
		// the same convention the vote referents above take.
		if ps, ok := tokenOwnerPlayers(h, c, v); ok {
			owners = ps
		} else {
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source,
				Text: "unrecognized TokenOwner " + v + ", defaulting to the controller"})
		}
	}
	// RememberOriginalTokens$ True mirrors RememberTokens$ exactly (see the
	// doc above for the original-vs-replaced-mint note). The 8 carriers all
	// chain a `DB$ ImmediateTrigger` "when you do" sub that reads this set.
	remember := tp.Remember
	// AttachedTo$ names the permanent the token enters attached to (the Wicked
	// Role of Charming Scoundrel's ETB, 50+ corpus lines): the value is a
	// Defined$-grammar selector, resolved with the ordinary resolver against
	// a shallow SA that carries it in Defined$, so every corpus spelling
	// (Targeted, Self, Remembered, ChosenCard, ...) works without a second
	// resolver. Each minted token is attached to the FIRST resolved object
	// target; an object that has left play by resolution time attaches
	// nothing (the token simply enters unattached, the Aura's unattached
	// state).
	attachedTo := tp.AttachedTo
	var attachTo state.ObjID
	if attachedTo != "" {
		for _, t := range DefinedSpec(h, c, attachedTo) {
			if !t.IsPlayer {
				attachTo = t.Obj
				break
			}
		}
	}
	// TokenPower$/TokenToughness$: resolve both dynamic sides once, before
	// the mint loop. Absent side = the token script's printed value, read off
	// each minted object's face below. A named-but-unresolvable side is loud
	// (one Note for the whole call) and skips the set entirely.
	var setPow, setTgh int32
	var hasPow, hasTgh bool
	dynBad := ""
	if tp.Power.Present {
		if v, resolved := numResolvedText(h, c, tp.Power, 0); resolved {
			setPow, hasPow = v, true
		} else {
			dynBad = "TokenPower$ " + tp.Power.Text
		}
	}
	if tp.Toughness.Present {
		if v, resolved := numResolvedText(h, c, tp.Toughness, 0); resolved {
			setTgh, hasTgh = v, true
		} else {
			if dynBad != "" {
				dynBad += ", "
			}
			dynBad += "TokenToughness$ " + tp.Toughness.Text
		}
	}
	if dynBad != "" {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
			Text: dynBad + " is not implemented; the token keeps its script's printed P/T"})
	}
	// WithCountersType$/WithCountersAmount$ (Printlifter Ooze's "create a
	// 0/0 ... token ... The token enters with X +1/+1 counters on it"): every
	// token this call creates enters with that many of the named counter
	// kind, emitted as ONE CounterChange per mint right after the mint -- the
	// ChangeZone entry counters' exact shape (zone.go's WithCounters read),
	// so AddCounter replacements (Doubling Season) and every CounterAdded
	// trigger see an entry counter the way they see a ChangeZone one. The
	// amount resolves through the ordinary Num grammar: a signed literal
	// (incubob's WithCountersAmount$ 1), an SVar name on the resolving face
	// (Printlifter Ooze's WithCountersAmount$ X over SVar:X:Count$Valid
	// Creature.YouCtrl), or an inline Count$... -- the same resolution the
	// TokenPower$/TokenToughness$ read above uses. A WithCountersType$ with
	// no WithCountersAmount$ defaults to 1; an amount the grammar cannot
	// resolve is loud (one Note for the whole call, never per mint) and the
	// set is skipped -- the token enters WITHOUT the counters, the honest
	// degrade that for a 0/0 script means the zero-toughness SBA sweeps it
	// visibly rather than a silent wrong count. The riders below run once per
	// mint EmitTokenCreate returned, so a CreateToken replacement's extras
	// each enter with the counters too -- not just the first mint.
	withKind := tp.WithCountersType
	var withAmt int32
	var withOK bool
	if withKind != "" {
		if tp.WithCountersAmount.Present {
			if v, ok := numResolvedText(h, c, tp.WithCountersAmount, 1); ok {
				withAmt, withOK = v, true
			} else {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
					Text: "WithCountersAmount$ " + strings.TrimSpace(tp.WithCountersAmount.Text) +
						" is not implemented; the token enters with no " + withKind + " counters"})
			}
		} else {
			withAmt, withOK = 1, true
		}
	}
	// PumpKeywords$/PumpDuration$: the keyword rider a token enters with or
	// is granted for a bounded time (Loyal Apprentice's Lieutenant Thopter
	// "gains haste until end of turn"; 25 corpus `DB$ Token` lines carry the
	// pair). The keywords resolve through cards.SplitKeywordList -- the same
	// ampersand-joined list reader CopyPermanent's PumpKeywords$ uses, so
	// "Menace & Haste" is two -- and each mint gets ONE LAbilities effect
	// sourced to the token itself, the exact shape the CopyPermanent path
	// registers, so the layer system re-derives it on replay.
	//
	// api:Token's default differs from CopyPermanent's: a bare
	// `PumpKeywords$ Haste` Token line means "until end of turn" (measured:
	// all 3 Token carriers without a PumpDuration$ read "gains haste until
	// end of turn" in Oracle), because the token is the lasting object and
	// the grant is the temporary part. CopyPermanent's bare form instead
	// keeps the keyword for as long as the copy exists (Mirage Phalanx). The
	// duration goes through the SAME parser the Effect primitive uses
	// (effectUntilEOT), so the turn-spanning forms get the identical
	// AddContinuous UntilTurn boundary and Unknown spellings are one loud
	// Note rather than a silent wrong lifetime.
	pumpKeywords := tp.PumpKeywords
	pumpDuration := tp.PumpDuration
	pumpPermanent := false
	pumpUntilEOT := false
	if len(pumpKeywords) > 0 {
		switch {
		case IsNextTurnDuration(pumpDuration):
			// AddContinuous derives the UntilTurn boundary from the live rotation.
		case pumpDuration == "":
			pumpUntilEOT = true
		case strings.EqualFold(pumpDuration, "untilendofcombat"):
			// The Duration value rides the registration; EndOfTurnCleanup
			// reclaims it (CR 511.2).
		case effectUntilEOT(h, 0, pumpDuration):
			// Source 0 deliberately disables effectUntilEOT's one-shot-spell
			// shortcut: a token's pump rider is not the spell's own effect, so
			// only the Duration$ vocabulary (this-turn spellings) is reused
			// here, never the "an instant/sorcery effect defaults to EOT" rule.
			pumpUntilEOT = true
		default:
			h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: c.Controller,
				Text: "PumpDuration$ " + pumpDuration + " is not implemented; the token keeps the keyword"})
			pumpPermanent = true
		}
	}
	tapped := tp.Tapped
	// TokenRemembered$ binds the newly-created token's persistent memory to
	// the named Defined$ group.  ExiledCards is Forge's name for the cards
	// exiled by the payment immediately before this Token effect; that set is
	// already the resolution's Remembered set in this engine.  Other selector
	// forms use the ordinary Defined resolver, so this remains extensible as
	// Defined gains readers rather than special-casing individual cards.
	tokenMemory := tokenRememberedTargets(h, c, tp.Remembered)
	// All selectors (owner, bearer and memory) have read the old set.
	forgetOtherRemembered(h, c, sa)

	// TokenAttacking$ True (Mobilize, Kari Zev's "tapped and attacking"
	// rider): every token this call creates enters attacking the combat's
	// DEFENDING player, read from the firing Attacks trigger's own referent
	// capture (rules/trigger_referents.go binds c.DefendingPlayer from the
	// DeclareAttackers event; the defending player is ev.Player there --
	// the engine batches attackers per defender). The corpus's OTHER selector
	// values (Remembered x5, RememberedPlayer x3, TriggeredAttackedTarget x4,
	// TriggeredDefender x1) resolve their PLAYER half through the shared
	// referent grammar in tokenAttackingRider above -- Adeline, Resplendent
	// Cathar's `RememberedPlayer & Valid Planeswalker.ControlledBy Remembered`
	// now attacks the RepeatEach loop's current opponent instead of minting a
	// sitting duck. A True with NO defender in context (an ACTIVATED AB$
	// Token rider like kavaron_harrier or militias_pride -- no trigger
	// context exists), or a selector whose player half resolves to nobody,
	// still enters (tapped, when TokenTapped$ says so) but NOT attacking,
	// under one deterministic Note: never a guessed defender. The mark itself
	// rides the appended events.TokenAttacks kind (events/apply.go), so
	// replay rebuilds it.
	// The AtEOT$ rider's affected set is every mint the loop actually mints,
	// collected here and scheduled by ONE scheduleAtEOT call after the loop:
	// the call (and, for an out-of-scope value, its one loud Note) is per
	// resolution, never per mint -- a multi-token body with an out-of-scope
	// value must not emit one Note per token.
	attackCtx := false
	var attackDefender state.PlayerID
	if attack := tp.Attacking; attack != "" {
		attackCtx, attackDefender = tokenAttackingRider(h, c, attack, "token")
	}

	job := TokenJob{
		N: n, Owners: owners, Remember: remember, AttachTo: attachTo, AttachNamed: attachedTo != "",
		SetPow: setPow, SetTgh: setTgh, HasPow: hasPow, HasTgh: hasTgh,
		WithKind: withKind, WithAmt: withAmt, WithOK: withOK,
		PumpKeywords: pumpKeywords, PumpDuration: pumpDuration,
		PumpPermanent: pumpPermanent, PumpUntilEOT: pumpUntilEOT,
		Tapped: tapped, TokenMemory: tokenMemory,
		AttackCtx: attackCtx, AttackDefender: attackDefender,
	}
	runTokenMints(h, c, sa, tp, &job, -1, nil, nil)
}

// TokenJob is one DB$ Token resolution's per-mint work, resolved ONCE before
// the mint loop: the owners and amount, and every value the per-mint riders
// read. Freezing it is what lets a mint that parks behind a CR 616.1 order ask
// resume its riders with exactly the values the first pass resolved -- a
// TokenAmount$ Count$ or a Defined$ selector re-read after the earlier mints
// landed would name a different set. Plain data (Clone copies the slices).
type TokenJob struct {
	N        int32
	Owners   []state.PlayerID
	Remember bool
	AttachTo state.ObjID
	// AttachNamed is AttachedTo$'s presence: the effect names the bearer, so
	// an Aura token whose named bearer resolved to no battlefield permanent
	// is not created (CR 303.4g, auraTokenWithheld).
	AttachNamed    bool
	SetPow, SetTgh int32
	HasPow, HasTgh bool
	WithKind       string
	WithAmt        int32
	WithOK         bool
	PumpKeywords   []string
	PumpDuration   string
	PumpPermanent  bool
	PumpUntilEOT   bool
	Tapped         bool
	TokenMemory    []state.Target
	AttackCtx      bool
	AttackDefender state.PlayerID
}

// auraTokenWithheld reports whether a mint of token script key must not
// happen because it is an Aura whose AttachedTo$-named bearer resolved to no
// battlefield permanent (a Role "attached to CARDNAME" whose creature left
// before the ability resolved). CR 303.4g: an Aura token with nothing it can
// legally enchant isn't created; one that would enter unattached would only
// die to the CR 704.5m SBA after its entry had already been seen. An Aura
// token minted with NO AttachedTo$ (no corpus carrier) is left to the
// engine's own TokenCreate gate (rules/aura_entry.go).
func auraTokenWithheld(g *state.Game, job *TokenJob, key string) bool {
	if !job.AttachNamed {
		return false
	}
	if b := g.Obj(job.AttachTo); job.AttachTo != 0 && b != nil && b.Zone == state.ZBattlefield {
		return false
	}
	def := g.Tokens[key]
	if def == nil || len(def.Faces) == 0 {
		return false
	}
	for _, t := range def.Faces[0].Types {
		if t == "Aura" {
			return true
		}
	}
	return false
}

// TokenRest is a DB$ Token's continuation once one of its mints parked
// behind a CR 616.1 replacement-order ask (an entry-counter order, a token
// replacement order): the ask suspends the resolution INSIDE the mint, so the
// minted objects exist only after the answer. The host re-enters the Token SA
// with this cursor once the answer has minted: Parked (host-filled) are the
// objects the parked mint produced, which take the riders; the loop then
// continues at the mint after Next. SinkID is the host's handle on the
// collector the answer mints into; effects never reads it. Plain data, so the
// host carries it on its own continuation frame and replay re-derives it.
//
// The same continuation serves every token-minting primitive whose post-mint
// work reads the minted ids (effToken's riders, Encore's haste and sacrifice
// group, Incubate's counters, Amass's Army): Next is the primitive's own loop
// cursor, and Players/Objs/Script/Amount/Count are the frozen values a
// primitive other than effToken re-enters with (effToken freezes Job
// instead; CopyPermanent freezes its controllers, copy sources and count).
type TokenRest struct {
	SA     *cards.SA
	SinkID uint64
	Next   int
	Parked []state.ObjID
	Minted []state.ObjID
	Job    TokenJob

	Players []state.PlayerID
	Objs    []state.ObjID
	Script  string
	Amount  int32
	Count   int32
	// Counts is a copy effect's per-destination copy count after the
	// CreateToken replacements (ProposeCopyTokens), frozen at the first pass
	// so a resumed pass mints the same units without re-proposing.
	Counts []int32
}

// resumingMint consumes and returns c's TokenRest when this pass re-enters sa
// after a parked mint's answer, else nil.
func resumingMint(c *Ctx, sa *cards.SA) *TokenRest {
	rest := (*TokenRest)(nil)

	if rest == nil || rest.SA != sa {
		return nil
	}

	return rest
}

// suspendMint hands a parked mint's continuation to the host (the caller's
// last EmitTokenCreate parked the resolution). It reports whether the host
// recorded it; the caller then stops, and Resolve defers the SA's
// Imprint/ClearImprinted tail to the re-entry.
func suspendMint(h Host, c *Ctx, rest TokenRest) bool {
	th, ok := h.(tokenRestHost)
	if !ok || !th.SuspendTokenRest(rest.SA, rest) {
		return false
	}
	c.tokensSuspended = true
	return true
}

// Clone returns a copy that shares no slice with r.
func (r TokenRest) Clone() TokenRest {
	r.Parked = append([]state.ObjID(nil), r.Parked...)
	r.Minted = append([]state.ObjID(nil), r.Minted...)
	r.Job.Owners = append([]state.PlayerID(nil), r.Job.Owners...)
	r.Job.PumpKeywords = append([]string(nil), r.Job.PumpKeywords...)
	r.Job.TokenMemory = append([]state.Target(nil), r.Job.TokenMemory...)
	r.Players = append([]state.PlayerID(nil), r.Players...)
	r.Objs = append([]state.ObjID(nil), r.Objs...)
	r.Counts = append([]int32(nil), r.Counts...)
	return r
}

// tokenRestHost is implemented by the rules engine: SuspendTokenRest records
// the continuation of a Token SA whose last EmitTokenCreate parked the
// resolution behind a replacement-order ask. It reports false when that emit
// did not park (or nothing is suspended to continue), and the caller keeps
// its synchronous behaviour.
type tokenRestHost interface {
	SuspendTokenRest(sa *cards.SA, rest TokenRest) bool
}

// runTokenMints is effToken's mint loop. resumeAt < 0 is a first pass; a
// TokenRest re-entry passes the parked mint's unit index, the objects the
// answer minted for it and the mints already made. A unit is one mint or one
// unknown-script Note, in the loop's own order, so a re-entry skips exactly
// the units the first pass already performed.
func runTokenMints(h Host, c *Ctx, sa *cards.SA, tp *TokenParams, job *TokenJob, resumeAt int, parked, minted []state.ObjID) {
	g := h.Game()
	unit := -1
	for _, key := range tp.Scripts {
		if _, ok := g.Tokens[key]; !ok {
			unit++
			if unit > resumeAt {
				h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "unknown token script " + key})
			}
			continue
		}
		for _, owner := range job.Owners {
			for i := int32(0); i < job.N; i++ {
				unit++
				if unit < resumeAt {
					continue
				}
				if unit == resumeAt {
					// The parked mint: the answer minted it (or its whole
					// rewritten plan); only its riders are owed.
					for _, want := range parked {
						minted = applyTokenMintRiders(h, c, job, owner, want, minted)
					}
					continue
				}
				// want is the ID the new object gets if TokenCreate's own Apply
				// case actually mints one (state.Game.AddObject assigns NextID,
				// then increments it) -- a direct, positive identity check,
				// rather than inferring a mint happened from g.Objs having grown
				// by watching its length before and after. EmitTokenCreate
				// returns EVERY object the emit created: an ordinary emit the
				// single mint, a CreateToken replacement the whole rewritten
				// plan (Divine Visitation's angel, Doubling Season's pair, Xorn's
				// original-plus-one), so each rider below lands on every mint the
				// resolution actually produced -- not just the first.
				if auraTokenWithheld(g, job, key) {
					// CR 303.4g: "If the Aura is a token, it isn't created."
					h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Player: owner,
						Text: "Aura token " + key + " has no legal permanent to enchant; it isn't created (CR 303.4g)"})
					continue
				}
				want := g.NextID
				wasSuspended := h.Suspended()
				mints := h.EmitTokenCreate(events.Event{Kind: events.TokenCreate, Player: owner, Text: key})
				if !wasSuspended && h.Suspended() {
					// The mint parked the resolution behind a replacement-order
					// ask: what landed so far takes its riders now, and the rest
					// of this resolution -- the parked mint's riders, the mints
					// after it, and the post-loop work -- resumes with the
					// answer instead of running against objects that do not
					// exist yet (and instead of emitting the next mint into the
					// outstanding ask, which would swallow it).
					if _, ok := h.(tokenRestHost); ok {
						landed := minted
						for _, id := range mints {
							landed = applyTokenMintRiders(h, c, job, owner, id, landed)
						}
						if suspendMint(h, c, TokenRest{SA: sa, Next: unit, Minted: landed, Job: *job}) {
							return
						}
						minted = landed
						continue
					}
				}
				if len(mints) == 0 {
					// Nothing landed (an unknown key or a plan rounded down to
					// zero): keep the single predicted id so the g.Obj guards
					// below no-op exactly as before and no rider fires.
					mints = []state.ObjID{want}
				}
				for _, want := range mints {
					minted = applyTokenMintRiders(h, c, job, owner, want, minted)
				}
			}
		}
	}
	// ImprintTokens$ True: the SOURCE is imprinted with the created tokens, so
	// a following SubAbility$ resolving `Defined$ Imprinted` (Timothar's
	// DBAnimate grant, Intrude on the Mind's DBPutCounters, Ugin's DBEffect)
	// names the newly-created token -- the reverse association (token imprinted
	// with the resolution's remembered cards) names the exiled cards instead
	// and leaves every such sub-ability acting on nothing.
	if tp.ImprintTokens && c.Source != 0 {
		ids := make([]state.ObjID, 0, len(minted))
		for _, id := range minted {
			if g.Obj(id) != nil {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			h.Emit(events.Event{Kind: events.Imprint, Obj: c.Source, IDs: ids, Text: "imprint-tokens"})
		}
	}
	scheduleAtEOT(h, c, sa, minted)
}

// applyTokenMintRiders applies one minted token's riders and returns minted
// with the token appended (the AtEOT$/ImprintTokens$ set). A want that did
// not become an object takes nothing.
func applyTokenMintRiders(h Host, c *Ctx, job *TokenJob, owner state.PlayerID, want state.ObjID, minted []state.ObjID) []state.ObjID {
	g := h.Game()
	if g.Obj(want) == nil {
		return minted
	}
	// A CreateToken replacement may have put this mint under a
	// controller other than the original creator (Crafty Cutpurse);
	// the Tap/TokenAttacks provenance reads the object's real
	// controller, which for the ordinary path is owner.
	mintOwner := owner
	if o := g.Obj(want); o != nil {
		mintOwner = o.Controller
	}
	if len(job.TokenMemory) > 0 {
		ids := make([]state.ObjID, 0, len(job.TokenMemory))
		for _, t := range job.TokenMemory {
			if t.IsPlayer {
				ids = append(ids, state.PlayerRef(t.Player))
			} else if t.Obj != 0 {
				ids = append(ids, t.Obj)
			}
		}
		if len(ids) > 0 {
			h.Emit(events.Event{Kind: events.Choose, Obj: want, Counter: "remembered", IDs: ids})
		}
	}
	if job.Remember {
		c.Remembered = append(c.Remembered, state.Target{Obj: want})
		eventRemember(h, c, want)
	}
	if job.WithOK {
		h.Emit(events.Event{Kind: events.CounterChange, Obj: want, Counter: job.WithKind, Amount: job.WithAmt})
	}
	if len(job.PumpKeywords) > 0 {
		h.AddContinuous(state.ContinuousEffect{
			Source: want, Controller: mintOwner, Affects: "Card.Self",
			Layer: state.LAbilities, AddKeywords: job.PumpKeywords,
			Duration: job.PumpDuration, Permanent: job.PumpPermanent, UntilEOT: job.PumpUntilEOT,
		})
	}
	if job.Tapped {
		h.Emit(events.Event{Kind: events.Tap, Obj: want, Player: mintOwner, Text: "entered tapped"})
	}
	if job.AttackCtx {
		h.Emit(events.Event{Kind: events.TokenAttacks, Obj: want, Player: mintOwner,
			IDs: []state.ObjID{state.ObjID(job.AttackDefender)}, Text: "entered attacking"})
	}
	if (job.HasPow || job.HasTgh) && g.Obj(want).Face() != nil {
		// The absent side keeps the token script's printed value. Every
		// corpus script a dynamic side rides (u_x_x_illusion, ...) is a
		// characteristic-defining */* whose printed read is 0, so both
		// sides are effectively always named together.
		pow, tgh := int32(g.Obj(want).Face().Power()), int32(g.Obj(want).Face().Toughness())
		if job.HasPow {
			pow = job.SetPow
		}
		if job.HasTgh {
			tgh = job.SetTgh
		}
		h.AddContinuous(state.ContinuousEffect{
			Source:       want,
			Controller:   mintOwner,
			Affects:      "Card.Self",
			Layer:        state.LPT,
			Sub:          state.SubSet,
			SetPower:     pow,
			SetToughness: tgh,
			HasSet:       true,
			Permanent:    true,
		})
	}
	if job.AttachTo != 0 && g.Obj(job.AttachTo) != nil {
		emitAttach(h, want, job.AttachTo)
	}
	// AtEOT$ (Valduk, Zektar Shrine Expedition: "exile those tokens at
	// the beginning of the next end step"): remember the predicted mint
	// id (the CopyPermanent pattern); the shared reader schedules the
	// whole minted set in one call after the loop.
	minted = append(minted, want)
	return minted
}

// copyTokenProposer is implemented by the rules engine: ProposeCopyTokens
// answers how many token copies of src an effect creating n of them under
// player creates once the CreateToken replacement effects apply (Doubling
// Season, Parallel Lives, ...), and emits any scripted mints those
// replacements add. A copy effect asks it once per creation, before minting.
type copyTokenProposer interface {
	ProposeCopyTokens(player state.PlayerID, src state.ObjID, n int32) int32
}

// proposeCopyTokens is the copy effects' read of copyTokenProposer; a host
// without it creates exactly the n copies asked for.
func proposeCopyTokens(h Host, player state.PlayerID, src state.ObjID, n int32) int32 {
	if p, ok := h.(copyTokenProposer); ok {
		return p.ProposeCopyTokens(player, src, n)
	}
	return n
}

const (
	tokenAttackingPlayers7c51RememberedPlayer uint16 = 1 // "RememberedPlayer", "RememberedPlayers"
)

var tokenAttackingPlayers7c51Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "RememberedPlayer", Val: tokenAttackingPlayers7c51RememberedPlayer},
	state.StrEntry[uint16]{Key: "RememberedPlayers", Val: tokenAttackingPlayers7c51RememberedPlayer},
)
