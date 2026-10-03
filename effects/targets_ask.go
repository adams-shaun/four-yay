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
// census the placement ask uses, as a KChoose over the "tgts" resume arm;
// the answer lands in Ctx.TargetsPick and the re-entered pass consumes it
// (the ask's ResumeSA is exactly this SA, so the pending frame re-enters
// it). A host that cannot ask takes the deterministic first-max stand-in
// (R-9), which is what botpolicy's first-option KChoose default answers
// with for card/player options.
//
// The ok return is NOT "targets were found" -- it is "use the returned set
// INSTEAD of Defined's own fallthrough": ok=true with a nil set means the
// ask was posed and SUSPENDED the resolution (effects.Resolve stops before
// dispatching the body), and the answered re-entry consumes
// Ctx.TargetsPick here. Every other shape returns false and the caller
// keeps Defined's own behaviour.
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
// Ctx.Targets and a Min-0 elected-zero must not be re-posed), or when an
// answered pre-ask for this very SA is waiting in Ctx.TargetsPick. Bounds
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
	defined := DefinedRefOf(sa).Text
	if !TargetsOf(sa).Targeted() ||
		(defined != "" && definedIsTargetReuse(defined) && sa.API != "Fight") {
		return nil, false
	}
	if sa.CompiledAPI() == cards.APIChangeZone || sa.API == "ChangeZone" {
		return nil, false
	}
	if c.SubPreAsk != nil {
		// The cast-time pre-ask's answer for exactly this sub (alltargeted1):
		// Forge chose the whole chain's targets before payment (CR 601.2c), so
		// the resolution uses the recorded set instead of re-posing the ask
		// here. Keep the answer until the stack object leaves: a suspended body
		// re-enters with a fresh Ctx and must see the same chosen targets (e.g.
		// MoveCounter's counter-kind pick). A TargetUnique$ sub
		// feeds the same later-ask exclusion accumulator the answered path
		// below does. An EMPTY recorded set is a real answer (a Min-0 chain
		// sub elected zero, or no candidate existed at cast time): it must
		// still use the empty answer, or the re-entered walk would pose the
		// mid-resolution ask after all.
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
		// The controller's which-opponent selection ask was posted: the walk
		// is suspended and re-enters this very SA, where the answered
		// selection makes ChooserFor return the chosen seat.
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

// definedIsTargetReuse reports whether a Defined$ value names one of the
// parent-target-reuse referents -- the documented reason the blanket
// Defined$ suppression above exists (a sub that names its PARENT's target;
// task tgtplayer1 narrowed the guard to exactly that shape). Any other
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
// DefinedIsTargetReuse is the exported form of definedIsTargetReuse, for
// rules' cast-time sub-ask collector (alltargeted1) to apply the same
// inclusion rule the mid-resolution path applies.
func DefinedIsTargetReuse(defined string) bool {
	return definedIsTargetReuse(defined)
}

func definedIsTargetReuse(defined string) bool {
	for tok := range strings.SplitSeq(defined, ",") {
		tok = strings.TrimSpace(tok)
		if i := strings.IndexByte(tok, '.'); i >= 0 {
			tok = tok[:i]
		}
		switch tok {
		case "Targeted", "ParentTarget", "ParentTargeted", "ThisTargetedCard", "AllTargeted":
			return true
		}
	}
	return false
}

// poseTargetsAsk is the shared tail of both ValidTgts$ mid-resolution asks
// (this file's chosenTargetsFor and zone.go's changeZoneChosenTargets): a
// KChoose over the eligible candidates -- one option per target, players
// labelled from the player table, cards from the face name -- posted through
// the shared Ask boundary under the "choice"-shaped resume transport the
// caller names, with the R-9 no-host stand-in (the first max candidates in
// offered order) when the host cannot ask. ok=true with a nil set is the
// SUSPENDED outcome; ok=true with a non-nil set is the stand-in -- or, with
// served set, the resolution kernel's answer, already consumed exactly as
// both callers' answered re-entries consume theirs (the TargetUnique$
// accumulator), so the caller continues with it.
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
		ResumeRemembered: copyTargets(c.Remembered), Prompt: prompt}
	// The TargetUnique accumulator rides EVERY ask through the ask boundary
	// (Engine.Ask stamps the live chain Ctx's accumulator onto any decision
	// that did not already carry one, which is what makes it survive a
	// suspension of ANY kind -- a Charm mode election, a ward pay window, a
	// dig/scry/arrange ask). The explicit stamp here is belt and braces for a
	// host that does not publish the chain Ctx (an effects-package test
	// double). Copied as a fresh slice -- the walk may append to it after this
	// ask is parked.
	if len(c.TargetsUnique) > 0 {
		d.ResumeTargetsUnique = copyTargets(c.TargetsUnique)
	}
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
		// The resolution kernel's answer in hand: the target set the
		// "tgts"/"choice" arm binds, consumed as the answered re-entry
		// consumes it (chosenTargetsFor's TargetsPickDone branch,
		// changeZoneChosenTargetsFor's ChoiceDone branch).
		ts := tapeAnswerTargets(ans)
		if TargetUniqueRequested(sa) {
			c.TargetsUnique = append(c.TargetsUnique, ts...)
		}
		return ts, true, true
	}
	if Ask(h, d) == AskAsked {
		return nil, true, false
	}
	return candidates[:max], true, false
}
