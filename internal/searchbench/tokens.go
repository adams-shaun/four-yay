package searchbench

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// xmageToken is one XMage token class a StateSpec may name (upstream's
// assets/gameplay/FDN_tokens.tsv: class, printed name, P/T, colours) and the
// gorge token script it is placed as. The script is a Forge token-script
// NAME (an identifier, not script text). The characteristics are checked
// against the compiled script at resolution, so a corpus bump that changes
// a script is refused rather than silently placed.
type xmageToken struct {
	class, script string
	subtypes      []string // every one must be on the gorge face
	power, tough  int      // -1: not a creature
	colors        string   // gorge Colors, comma-joined, "" colourless
	keywords      []string // keywords the gorge face must carry
}

// fdnTokens covers every token class of upstream's FDN token table except
// the Arena "Copy" placeholder (token copies carry the copied card's id and
// are refused). The script choices are the tokens FDN cards create in the
// Forge corpus (measured: the TokenScript$ values of the 623 FDN card
// scripts), matched by name, P/T, colour and keywords.
var fdnTokens = []xmageToken{
	{"ZombieToken", "b_2_2_zombie", []string{"Zombie"}, 2, 2, "black", nil},
	{"RhinoToken", "g_4_4_rhino_trample", []string{"Rhino"}, 4, 4, "green", []string{"Trample"}},
	{"ClueArtifactToken", "c_a_clue_draw", []string{"Clue"}, -1, -1, "", nil},
	{"CatToken3", "w_1_1_cat", []string{"Cat"}, 1, 1, "white", nil},
	{"CatToken", "w_2_2_cat", []string{"Cat"}, 2, 2, "white", nil},
	{"CatToken2", "w_1_1_cat_lifelink", []string{"Cat"}, 1, 1, "white", []string{"Lifelink"}},
	{"HumanToken", "w_1_1_human", []string{"Human"}, 1, 1, "white", nil},
	{"Knight33Token", "w_3_3_knight", []string{"Knight"}, 3, 3, "white", nil},
	{"RabbitToken", "w_1_1_rabbit", []string{"Rabbit"}, 1, 1, "white", nil},
	{"SoldierToken", "w_1_1_soldier", []string{"Soldier"}, 1, 1, "white", nil},
	{"SpiritWhiteToken", "w_1_1_spirit_flying", []string{"Spirit"}, 1, 1, "white", []string{"Flying"}},
	{"DrakeToken", "u_2_2_drake_flying", []string{"Drake"}, 2, 2, "blue", []string{"Flying"}},
	{"FaerieToken", "u_1_1_faerie_flying", []string{"Faerie"}, 1, 1, "blue", []string{"Flying"}},
	{"FishNoAbilityToken", "u_1_1_fish", []string{"Fish"}, 1, 1, "blue", nil},
	{"KomasCoilToken", "komas_coil", []string{"Serpent"}, 3, 3, "blue", nil},
	{"NinjaToken2", "u_2_1_ninja", []string{"Ninja"}, 2, 1, "blue", nil},
	{"ScionOfTheDeepToken", "scion_of_the_deep", []string{"Octopus"}, 8, 8, "blue", nil},
	{"RatToken", "b_1_1_rat", []string{"Rat"}, 1, 1, "black", nil},
	{"DragonToken", "r_4_4_dragon_flying", []string{"Dragon"}, 4, 4, "red", []string{"Flying"}},
	{"DragonToken2", "r_5_5_dragon_flying", []string{"Dragon"}, 5, 5, "red", []string{"Flying"}},
	{"GoblinToken", "r_1_1_goblin", []string{"Goblin"}, 1, 1, "red", nil},
	{"ElfWarriorToken", "g_1_1_elf_warrior", []string{"Elf", "Warrior"}, 1, 1, "green", nil},
	{"RaccoonToken", "g_3_3_raccoon", []string{"Raccoon"}, 3, 3, "green", nil},
	{"InsectBlackGreenFlyingToken", "bg_1_1_insect_flying", []string{"Insect"}, 1, 1, "black,green", []string{"Flying"}},
	{"FoodToken", "c_a_food_sac", []string{"Food"}, -1, -1, "", nil},
	{"TreasureToken", "c_a_treasure_sac", []string{"Treasure"}, -1, -1, "", nil},
	{"CatBeastToken", "w_2_2_cat_beast", []string{"Cat", "Beast"}, 2, 2, "white", nil},
	{"WhiteDogToken", "w_1_1_dog", []string{"Dog"}, 1, 1, "white", nil},
	{"RatCantBlockToken", "b_1_1_rat_noblock", []string{"Rat"}, 1, 1, "black", nil},
	{"PhyrexianGoblinToken", "r_1_1_phyrexian_goblin", []string{"Phyrexian", "Goblin"}, 1, 1, "red", nil},
	{"BeastToken", "g_3_3_beast", []string{"Beast"}, 3, 3, "green", nil},
	{"BeastToken2", "g_4_4_beast", []string{"Beast"}, 4, 4, "green", nil},
	{"GolemToken", "c_3_3_a_golem", []string{"Golem"}, 3, 3, "", nil},
	{"EldraziScionToken", "c_1_1_eldrazi_scion_sac", []string{"Eldrazi", "Scion"}, 1, 1, "", nil},
	{"BirdIllusionToken", "u_1_1_bird_illusion_flying", []string{"Bird", "Illusion"}, 1, 1, "blue", []string{"Flying"}},
}

// ResolveToken maps a spec token (tokenClass, or token + set) onto a gorge
// token script key in reg.Tokens, checking the characteristics. An unknown
// class, a bare token name (the tokens-database names are ambiguous: FDN's
// "Beast" and "Cat" each name two classes), or a script whose compiled face
// disagrees is refused.
func ResolveToken(reg *cards.Registry, tokenClass, token string) (string, error) {
	if tokenClass == "" {
		return "", &Refusal{Code: "unknown token", Detail: fmt.Sprintf("token %q without tokenClass", token)}
	}
	for _, t := range fdnTokens {
		if t.class != tokenClass {
			continue
		}
		c := reg.Tokens[t.script]
		if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
			return "", &Refusal{Code: "unknown token", Detail: fmt.Sprintf("%s: script %s not in the corpus", tokenClass, t.script)}
		}
		if why := t.mismatch(c.Faces[0]); why != "" {
			return "", &Refusal{Code: "unknown token", Detail: fmt.Sprintf("%s: script %s %s", tokenClass, t.script, why)}
		}
		return t.script, nil
	}
	return "", &Refusal{Code: "unknown token", Detail: "tokenClass " + tokenClass}
}

func (t xmageToken) mismatch(f *cards.Face) string {
	have := map[string]bool{} // lookup only
	for _, ty := range f.Types {
		have[ty] = true
	}
	for _, st := range t.subtypes {
		if !have[st] {
			return "lacks type " + st
		}
	}
	if t.power >= 0 {
		if !f.IsCreature() || f.PT != strconv.Itoa(t.power)+"/"+strconv.Itoa(t.tough) {
			return "is not a " + strconv.Itoa(t.power) + "/" + strconv.Itoa(t.tough) + " creature"
		}
	} else if f.IsCreature() {
		return "is a creature"
	}
	cols := strings.Split(strings.ToLower(strings.ReplaceAll(f.Colors, " ", "")), ",")
	sort.Strings(cols)
	got := strings.Trim(strings.Join(cols, ","), ",")
	if got != t.colors {
		return "is " + got + ", not " + t.colors
	}
	for _, k := range t.keywords {
		if !f.HasKeyword(k) {
			return "lacks " + k
		}
	}
	return ""
}
