package v2engine

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/state"
)

// fullName is a card's Oracle full name (spec 4.4): "A // B" for a
// multi-face card (transform, modal DFC, split, adventure, flip), else its
// one face's name. Decklists and full_name use it.
func fullName(c *cards.Card) string {
	if c == nil || len(c.Faces) == 0 {
		return ""
	}
	if len(c.Faces) >= 2 && c.AlternateMode != "" && c.Faces[1] != nil && c.Faces[1].Name != "" {
		return c.Faces[0].Name + " // " + c.Faces[1].Name
	}
	return c.Faces[0].Name
}

// isMultiFace reports whether c has an "A // B" full name.
func isMultiFace(c *cards.Card) bool { return c != nil && strings.Contains(fullName(c), " // ") }

// Vocabulary normalization (spec 6.10): lowercase, apostrophes removed,
// spaces and hyphens become "_".

var seats = [2]string{"p0", "p1"}

func seatOf(p state.PlayerID) string {
	if p == 1 {
		return "p1"
	}
	return "p0"
}

// snake normalizes a word to spec 6.10's form; "" when nothing of it is
// usable snake_case.
func snake(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\'' || r == '’':
			continue
		case r == ' ' || r == '-':
			b.WriteByte('_')
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			// any other character (accents, punctuation) is dropped
		}
	}
	out := strings.Trim(b.String(), "_")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if out == "" || out[0] < 'a' || out[0] > 'z' {
		return ""
	}
	return out
}

// splitTypes splits gorge's type words into supertypes, card types and
// subtypes, each normalized and deduplicated in first-seen order.
func splitTypes(words []string) (supers, types, subs []string) {
	supers, types, subs = []string{}, []string{}, []string{}
	seen := map[string]bool{} // lookup only
	add := func(dst *[]string, w string) {
		if w == "" || seen[w] {
			return
		}
		seen[w] = true
		*dst = append(*dst, w)
	}
	for _, raw := range words {
		for _, w := range strings.Fields(raw) {
			if w == "-" || w == "—" {
				continue
			}
			if s, ok := policynet.V2SupertypeWord(w); ok {
				add(&supers, s)
			} else if t, ok := policynet.V2CardTypeWord(w); ok {
				add(&types, t)
			} else {
				add(&subs, snake(w))
			}
		}
	}
	return supers, types, subs
}

// colorWords maps gorge's WUBRG colour letters to spec 6.10 words, in order.
func colorWords(letters string) []string {
	out := []string{}
	for _, c := range []struct {
		l byte
		w string
	}{{'W', "white"}, {'U', "blue"}, {'B', "black"}, {'R', "red"}, {'G', "green"}} {
		if strings.IndexByte(strings.ToUpper(letters), c.l) >= 0 {
			out = append(out, c.w)
		}
	}
	return out
}

// keywordName normalizes a gorge keyword line ("Ninjutsu:1 U", "Protection
// from red", "Flying") to a spec 6.10 keyword name without parameters, or ""
// when the line is not a plain keyword.
func keywordName(k string) string {
	head, _, _ := strings.Cut(k, ":")
	head = strings.TrimSpace(head)
	low := strings.ToLower(head)
	switch {
	case strings.HasPrefix(low, "protection"):
		return "protection"
	case strings.HasPrefix(low, "hexproof"):
		return "hexproof"
	case strings.HasSuffix(low, "walk") && !strings.Contains(low, " "):
		return "landwalk"
	}
	if strings.Count(head, " ") > 2 || strings.ContainsAny(head, ".,$<>{}()") || strings.Contains(low, "cardname") {
		return ""
	}
	return snake(head)
}

// counterName normalizes a gorge counter kind (Forge's "P1P1", "LOYALTY").
func counterName(k string) string {
	return snake(k)
}

// phaseStep maps a gorge step to spec 6.2's phase_step.
func phaseStep(s state.Step) string {
	switch s {
	case state.StepUntap:
		return "untap"
	case state.StepUpkeep:
		return "upkeep"
	case state.StepDraw:
		return "draw"
	case state.StepMain1:
		return "precombat_main"
	case state.StepBeginCombat:
		return "beginning_of_combat"
	case state.StepDeclareAttackers:
		return "declare_attackers"
	case state.StepDeclareBlockers:
		return "declare_blockers"
	case state.StepCombatDamage:
		return "combat_damage"
	case state.StepEndCombat:
		return "end_of_combat"
	case state.StepMain2:
		return "postcombat_main"
	case state.StepEnd:
		return "end_step"
	case state.StepCleanup:
		return "cleanup"
	}
	return "precombat_main"
}

func zoneName(z state.Zone) string {
	switch z {
	case state.ZLibrary:
		return "library"
	case state.ZHand:
		return "hand"
	case state.ZBattlefield:
		return "battlefield"
	case state.ZGraveyard:
		return "graveyard"
	case state.ZExile:
		return "exile"
	case state.ZStack:
		return "stack"
	case state.ZCommand:
		return "command"
	}
	return ""
}
