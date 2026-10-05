package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// chosenTargetsFor serves ANY ValidTgts$-declared SA that resolves with no
// ask having covered its targeting: the generic pre-ask inside
// effects.Resolve's dispatch loop (task mvts1). The placement ask
// (rules/trigger_queue.go pushTrigger) and the announcement ask
// (rules/stack.go resolveTop's ability/spell branches) cover only the
// depth-1 body of a trigger or cast -- a deeper sub of the Execute chain
// (the "when you do" family: Mogg Bombers' DealDamage, Aria of Flame's
// verse ping, Kor Outfitter's Attach) used to arrive here with no chosen
// targets and either moved nothing silently or inherited the OUTER SA's
// targets. This poses the sub's own ask through the same Host.LegalTargets
// census the placement ask uses, as a KChoose answered in place via
// AskTape. A host that cannot ask takes the deterministic first-max stand-in
// (R-9), which is what botpolicy's first-option KChoose default answers
// with for card/player options.
//
// The ok return is NOT "targets were found" -- it is "use the returned set
// INSTEAD of Defined's own fallthrough": ok=true with a nil set means a
// which-opponent selection was reported pending (or an empty pre-ask set was
// recorded). Every other shape returns false and the caller keeps Defined's
// own behaviour.
//
// The ask never fires when the SA also carries Defined$ (an already-named
// fetch list is Forge's no-ask shape) -- with ONE carve-out: API$ Fight,
// the one primitive whose SA carries TWO independent target lists
// (Defined$ names the fighter(s), ValidTgts$ names the creature(s) they
// fight). A Fight sub reached deeper in an Execute chain (Kraul
// Harpooner's DB$ Pump | Defined$ Self | SubAbility$ DBFight) would
// otherwise never be asked for its opponent, and its fight would stay
// silently inert even with effFight implemented -- the placement ask
// cannot reach a depth-2 sub. The execute-shaped Fight bodies (Warbriar
// Blessing) and the modal Charm-mode ones (Voracious Hydra) are still
// skipped below: their placement ask already ran and set OfferedSA, so
// the Line match skips them before any ask is re-posed. When it is an
// API$ ChangeZone body
// (effChangeZone's own mid-resolution ask, changeZoneChosenTargets, owns
// that shape -- the closed ChangeZone slice), when this is the depth-0
// entry SA of a resolution whose TargetsOffered marker is set (the
// placement ask covered exactly that SA; its targets are already in
// Ctx.Targets and a Min-0 elected-zero must not be re-posed). Bounds
// come from TargetMin$/TargetMax$ through the ordinary Num grammar,
// clamped to the eligible count; Min == Max == 0 or an empty eligible set
// is no ask and no move -- a decision nobody could answer differently is
// never emitted, and a ValidTgts$ spec that matches nobody (an unknown
// predicate fails closed) keeps today's empty-set no-op.
//
// Deliberately NOT honoured here (measured carrier population: one sub
// each, both noted in the mvts1 report): DividedAsYouChoose$ -- the ask
// offers the plain TargetMin$/TargetMax$ bounds, exactly like the placement
// ask does for the same parameters.
//
// TargetsForEachPlayer$ IS honoured (pfpe1; the mvts1 round deliberately
// skipped it): the bounds read the OneEach spellings against the distinct-
// controller count of the eligible candidates, and the pose attaches each
// option's controller Group -- the same label rules' ask sites attach -- so
// Decision.Validate's mutual-exclusion rule enforces one pick per
// controller on the wire whatever host answers. A depth-2 SubAbility$
// carrier (Kaya, Spirits' Justice's exile-each; mega_flare,
// tasha_the_witch_queen, geths_summons) reaches its ask here.
func chosenTargetsFor(h Host, c *Ctx, sa *cards.SA, atRoot bool) ([]state.Target, bool) {
	if !TargetsOf(sa).Targeted() || TargetAskReusesPrior(sa) {
		return nil, false
	}
	if TargetAskIsChangeZone(sa) {
		return nil, false
	}
	if c.SubPreAsk != nil {
		// The cast-time pre-ask's answer for exactly this sub (alltargeted1):
		// Forge chose the whole chain's targets before payment (CR 601.2c), so
		// the resolution uses the recorded set instead of re-posing the ask
		// here. A TargetUnique$ sub feeds the same later-ask exclusion
		// accumulator the answered path below does. An EMPTY recorded set is
		// a real answer (a Min-0 chain sub elected zero, or no candidate
		// existed at cast time): it must still use the empty answer, or the
		// walk would pose the mid-resolution ask after all.
		if ts, ok := c.SubPreAsk[sa.Line]; ok {
			if TargetUniqueRequested(sa) {
				c.TargetsUnique = append(c.TargetsUnique, ts...)
			}
			return ts, true
		}
	}

	if c.OfferedSA != nil && sa.Line == c.OfferedSA.Line {
		// The placement/announcement ask covered exactly THIS SA (matched by
		// Line: ResolveSVar parses fresh on every call, so pointer identity
		// never holds between two derivations of the same body -- the
		// matching convention rules' charmModeTarget established). Its
		// targets are already in Ctx.Targets and a Min-0 elected-zero must
		// not be re-posed.
		return nil, false
	}
	if atRoot && c.TargetsOffered {
		// Belt and braces for a depth-0 entry under an offered marker whose
		// OfferedSA derivation did not fire (e.g. a modal root whose own
		// ValidTgts$ was not the ask's subject).
		return nil, false
	}
	// Legality stays referenced to the ability controller (TargetingPlayer$
	// names who ANSWERS, not whose target legality this is); only the
	// decision's Player moves to the chooser, via the same resolver every
	// rules-tier target ask uses (Engine.ChooserFor -> targetChooserCore).
	candidates := subAskCandidates(h, c, sa)
	chooser := h.ChooserFor(c, sa)
	if ch, posed := opponentPick(h, c, sa, chooser); posed {
		// The host reports the which-opponent selection as still pending:
		// no target set yet (the rules host answers it in place and never
		// reports it pending).
		return nil, true
	} else if !posed {
		chooser = ch
	}
	tp := TargetsOf(sa)
	min := numText(h, c, tp.Min, 1)
	max := numText(h, c, tp.Max, 1)
	if tp.Has(TgtForEachPlayer) {
		// pfpe1: OneEach is the distinct-controller count of the eligible
		// set (Forge's TargetRestrictions.setForEachPlayer), not a literal
		// Num can read -- and a dynamic bound (TargetMax$ X with
		// SVar:X:PlayerCountOpponents$Amount) already resolved above.
		owners := map[state.PlayerID]bool{}
		for _, t := range candidates {
			owners[targetOwnerOf(h, t)] = true
		}
		if tp.Has(TgtMinOneEach) {
			min = int32(len(owners))
		}
		if tp.Has(TgtMaxOneEach) {
			max = int32(len(owners))
		}
	}
	if max > int32(len(candidates)) {
		max = int32(len(candidates))
	}
	if min > max {
		min = max
	}
	if min < 0 {
		min = 0
	}
	if max <= 0 {
		// Nothing eligible (or an explicitly zero bound): no ask, no move.
		return noSubTargets(c, sa)
	}
	ts, ok, _ := poseTargetsAsk(h, c, sa, chooser, candidates, min, max, "tgts")
	return ts, ok
}

// TargetAskReusesPrior identifies a link that consumes an earlier target
// rather than asking again. Fight declares its own distinct targets.
func TargetAskReusesPrior(sa *cards.SA) bool {
	return DefinedIsTargetReuse(DefinedRefOf(sa).Text) && sa.API != "Fight"
}

// TargetAskIsChangeZone identifies the link whose target chooser lives in
// changeZoneChosenTargetsFor rather than chosenTargetsFor.
func TargetAskIsChangeZone(sa *cards.SA) bool {
	return sa.CompiledAPI() == cards.APIChangeZone || sa.API == "ChangeZone"
}

// DefinedIsTargetReuse reports whether a Defined$ value names one of the
// parent-target-reuse referents -- the reason chosenTargetsFor suppresses a
// duplicate target ask (a sub that names its PARENT's target; task tgtplayer1
// narrowed the guard to exactly that shape). Any other
// Defined$ value -- `You`, `Self`, a battlefield `Valid` sweep, a fire-time
// `Triggered*` referent -- is the beneficiary/actor half of the SA, not its
// targeting, so the SA's own ValidTgts$ is a REAL targeting this build must
// ask (Knollspine Dragon's `DB$ Draw | Defined$ You | ValidTgts$ Opponent`:
// the opponent is the magnitude's source, You only names the drawer --
// suppressing the ask left the TargetedPlayer$ head over an empty target
// list and the draw silently at zero). Dot-qualified variants of the same
// referents (`Targeted.Creature`, `ThisTargetedCard.Creature`) reuse the
// parent target just the same, so the classifier reads each comma token's
// head before its first `.`; `TargetedController` and friends are NOT in
// the set (they are derived referents this engine resolves through its own
// machinery, measured corpus-unreachable at the reachable dispatch sites).
func DefinedIsTargetReuse(defined string) bool {
	for tok := range strings.SplitSeq(defined, ",") {
		tok = strings.TrimSpace(tok)
		if i := strings.IndexByte(tok, '.'); i >= 0 {
			tok = tok[:i]
		}
		if definedIsTargetReuseSet.Has(tok) {
			return true
		}
	}
	return false
}

// poseTargetsAsk is the shared tail of both ValidTgts$ mid-resolution asks
// (this file's chosenTargetsFor and zone.go's changeZoneChosenTargets): a
// KChoose over the eligible candidates -- one option per target, players
// labelled from the player table, cards from the face name -- answered in
// place via AskTape under the resume kind the caller names, with the R-9
// no-host stand-in (the first max candidates in offered order) when the host
// cannot ask. ok=true returns the set to use: the stand-in, or -- with served
// set -- the answer, already fed to the TargetUnique$ accumulator.
// targetOwnerOf is the controlling player of one target candidate: the
// player itself, else the object's controller. A vanished object fails to
// seat 0 -- it only merges a dead candidate's group with seat 0's, the
// over-restrictive direction.
func targetOwnerOf(h Host, t state.Target) state.PlayerID {
	if t.IsPlayer {
		return t.Player
	}
	if o := h.Game().Obj(t.Obj); o != nil {
		return o.Controller
	}
	return 0
}

func poseTargetsAsk(h Host, c *Ctx, sa *cards.SA, chooser state.PlayerID,
	candidates []state.Target, min, max int32, resumeKind string,
) (ts []state.Target, ok bool, served bool) {
	tp := TargetsOf(sa)
	prompt := tp.Prompt
	if prompt == "" {
		prompt = "Choose target"
	}
	// TargetUnique$ True: no candidate already chosen in this resolution may
	// be offered again (a root's target to an "another target" sub, or an
	// earlier TargetUnique pick in the same chain). The bounds clamp AFTER
	// the filter, so the no-host stand-in's candidates[:max] can never slice
	// past the filtered length. A decision every candidate of which was
	// excluded is never posed -- but unlike the empty-eligible-set case it
	// does NOT keep the caller's Defined fallthrough: that fallthrough reads
	// Ctx.Targets (the resolution's parent target), so the "other target"
	// body would act on the excluded target itself (Venom Blast's pumped
	// creature dealing its damage to ITSELF). A TargetUnique$ filter that
	// leaves no candidate therefore returns a handled, non-nil EMPTY target
	// set, which the caller dispatches the body over (Ctx.PickedTargets
	// non-nil outranks Ctx.Targets in Defined, and effChangeZone assigns the
	// empty set to its move list -- both a no-op).
	if TargetUniqueRequested(sa) {
		filtered := TargetUniqueFilter(sa, candidates, TargetsAlreadyChosen(c))
		if len(filtered) == 0 {
			return []state.Target{}, true, false
		}
		candidates = filtered
	}
	if max > int32(len(candidates)) {
		max = int32(len(candidates))
	}
	if min > max {
		min = max
	}
	if max <= 0 {
		return nil, false, false
	}
	resumeSA := sa
	if c.TargetAskResume != nil {
		resumeSA = c.TargetAskResume
	}
	d := &decision.Decision{Player: chooser, Kind: decision.KChoose,
		Min: int(min), Max: int(max), Source: c.Source,
		ResumeKind: resumeKind, ResumeSA: resumeSA,
		Prompt: prompt}
	// pfpe1: the TargetsForEachPlayer$ shape binds each option to its
	// controller's Group -- the same label rules' ask sites attach (askTarget
	// / cast.go targetAsk) -- so Decision.Validate's mutual-exclusion rule
	// enforces one pick per controller whatever host answers. The bot's
	// KChoose default arm plus Clamp's group-aware top-up answers it
	// validly (first offer, topped up one per new group).
	forEach := tp.Has(TgtForEachPlayer)
	for _, t := range candidates {
		o := decision.Option{Index: len(d.Options)}
		owner := state.PlayerID(0)
		label := ""
		if t.IsPlayer {
			o.Kind, o.Player = "player", t.Player
			if p := h.Game(); int(t.Player) < len(p.Players) {
				label = p.Players[t.Player].Name
			}
			owner = t.Player
		} else {
			o.Kind, o.Obj = "card", t.Obj
			if g := h.Game().Obj(t.Obj); g != nil {
				if g.Face() != nil {
					label = g.Face().Name
				}
				owner = g.Controller
			}
		}
		if forEach {
			o.Group = "target-controller-" + strconv.Itoa(int(owner))
		}
		o.Label = label
		d.Options = append(d.Options, o)
	}
	if RandomTargetsAsk(h, d, sa) {
		// TargetsAtRandom$: the ask now offers exactly the rng's draw, which
		// is also what a host that cannot ask takes below.
		candidates = tapeAnswerTargets(d.Options)
		max = int32(len(candidates))
	}
	if ans, ok := AskTape(h, d); ok {
		// Answered in place: the chosen target set.
		ts := tapeAnswerTargets(ans)
		if TargetUniqueRequested(sa) {
			c.TargetsUnique = append(c.TargetsUnique, ts...)
		}
		return ts, true, true
	}
	return candidates[:max], true, false
}

var definedIsTargetReuseSet = state.NewNameSet(
	"Targeted",
	"ParentTarget",
	"ParentTargeted",
	"ThisTargetedCard",
	"AllTargeted",
)
