package botpolicy

import (
	"math/rand/v2"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// LegacyDecide preserves the pre-B2 heuristic for the botbench head-to-head:
// attackers are still all declared, and blockers are still selected by the
// historical per-option coin. Its blocker arm is the one branch that reads
// attacker facts beyond Board.IsMain: after the coin loop it drops a whole
// block declaration that leaves a team-needing attacker (a derived Menace
// keyword on Board.Creatures, or an offered option's published MinBlockers)
// with fewer than its required blockers, because the engine rejects such a
// declaration and a rejected intent aborts the bench. This is a deliberate
// reversal of the original "no Board facts at all" contract, scoped to that
// branch: the heuristic -- attack rule, coin, every other arm -- is otherwise
// the historical snapshot, and this remains a benchmark driver, not a
// production policy. The Effect-specific no-host decline remains the other
// deliberate deviation; game seats, acceptance and fuzz use Decide.
func LegacyDecide(b Board, d *decision.Decision, r *rand.Rand) decision.Intent {
	in := decision.Intent{Seq: d.Seq, Player: d.Player}
	switch d.Kind {
	case decision.KPriority:
		if b.IsMain {
			for _, o := range d.Options {
				if o.Kind == "activate" {
					in.Choices = []int{o.Index}
					return Clamp(d, in)
				}
			}
		}
		for _, want := range [...]string{"play_land", "cast"} {
			for _, o := range d.Options {
				if o.Kind == want {
					in.Choices = []int{o.Index}
					return Clamp(d, in)
				}
			}
		}
		if b.IsMain {
			for _, o := range d.Options {
				if o.Kind == "ability" {
					in.Choices = []int{o.Index}
					return Clamp(d, in)
				}
			}
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				in.Choices = []int{o.Index}
				return Clamp(d, in)
			}
		}

	case decision.KTarget:
		for _, o := range d.Options {
			if o.Kind == "player" && o.Player != d.Player {
				in.Choices = []int{o.Index}
				return Clamp(d, in)
			}
		}
		if len(d.Options) > 0 {
			in.Choices = []int{d.Options[0].Index}
			return Clamp(d, in)
		}

	case decision.KAttackers:
		// Attack with every legal attacker -- at most ONE defender each (CR
		// 506.2; Task m34 offers every (attacker, defender) pair, and the
		// engine rejects an intent naming the same creature twice, so a
		// naive "every option" would be rejected in a multiplayer game).
		// The first option per creature is already deterministic: the
		// engine offers the pairs defender-major in seat order, so the
		// first option of a creature names its lowest-numbered defender.
		used := map[state.ObjID]bool{} // membership only -- never ranged.
		ch := make([]int, 0, len(d.Options))
		for _, o := range d.Options {
			if used[o.Obj] {
				continue
			}
			used[o.Obj] = true
			ch = append(ch, o.Index)
		}
		in.Choices = ch
		return Clamp(d, in)

	case decision.KBlockers:
		var ch []int
		used := map[state.ObjID]bool{} // membership only -- never ranged.
		for _, o := range d.Options {
			if !used[o.Obj] && r.IntN(2) == 0 {
				used[o.Obj] = true
				ch = append(ch, o.Index)
			}
		}
		// Preserve every coin draw above; only then drop whole block
		// declarations that violate a team-size requirement. The shared
		// legalBlockChoices guard is the same rule the production KBlockers
		// arms and the search teacher route through, so a partial team
		// (0 < chosen < required Min) is dropped entirely rather than
		// truncated to a lone block the engine still rejects. It reads the
		// same two facts the engine validates against: the derived Menace
		// keyword on Board.Creatures and the MinMaxBlocker bound published
		// on each offered option. Clamp repairs required attackers.
		in.Choices = legalBlockChoices(b, d, ch)
		return Clamp(d, in)

	case decision.KTriggerOrder:
		if n := len(d.Options); n > 0 {
			perm := make([]int, n)
			for i, o := range d.Options {
				perm[i] = o.Index
			}
			for i := n - 1; i > 0; i-- {
				j := r.IntN(i + 1)
				perm[i], perm[j] = perm[j], perm[i]
			}
			in.Choices = perm
			return Clamp(d, in)
		}

	case decision.KTriggerOptional:
		if d.EffectOptional {
			in.Choices = declineOptional(d)
			return Clamp(d, in)
		}
		if idx := r.IntN(2); idx < len(d.Options) {
			in.Choices = []int{d.Options[idx].Index}
			return Clamp(d, in)
		}

	case decision.KChoose:
		if len(d.Options) == 0 {
			break
		}
		switch d.Options[0].Kind {
		case "x":
			in.Choices = []int{d.Options[len(d.Options)-1].Index}
		case "exile", "sacrifice", "discard":
			// "discard" joins "exile"/"sacrifice": take the first Max options.
			// Deliberately naive (discards oldest-held cards without judging
			// them), matching policy.go; bot decision quality is out of scope
			// for the cleanup-discard task (findings ck/cl).
			for i := 0; i < len(d.Options) && i < d.Max; i++ {
				in.Choices = append(in.Choices, d.Options[i].Index)
			}
		default:
			in.Choices = []int{d.Options[0].Index}
		}
		return Clamp(d, in)

	case decision.KMulligan:
		if len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			for j := 0; j < len(d.Options) && j < d.Min; j++ {
				in.Choices = append(in.Choices, d.Options[j].Index)
			}
			return Clamp(d, in)
		}
		if len(d.Options) > 1 {
			for _, o := range d.Options {
				if o.Kind == "mulligan" && r.IntN(3) == 0 {
					in.Choices = []int{o.Index}
					return Clamp(d, in)
				}
			}
		}
		if len(d.Options) > 0 {
			in.Choices = []int{d.Options[0].Index}
			return Clamp(d, in)
		}

	case decision.KModes:
		if c := aiRandomPick(d, r); c != nil {
			in.Choices = c
			return Clamp(d, in)
		}
		for j := 0; j < len(d.Options) && j < d.Min; j++ {
			in.Choices = append(in.Choices, d.Options[j].Index)
		}
		return Clamp(d, in)
	}

	if d.Min == 0 {
		for _, o := range d.Options {
			if o.Kind == "pass" {
				in.Choices = []int{o.Index}
				return Clamp(d, in)
			}
		}
	}
	return Clamp(d, in)
}
