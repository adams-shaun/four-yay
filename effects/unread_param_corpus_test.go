package effects

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// These corpus ratchets pin every file carrying one of the effect parameter
// shapes repaired by the std3 unread-parameter ticket. Matching the API token
// rather than a record prefix covers both A: abilities and SVar subabilities.
func TestUnreadEffectParamCorpusCarriers(t *testing.T) {
	testutil.CorpusRegistry(t) // absent .cards must skip rather than pass vacuously
	root := filepath.Join("..", ".cards", "cardsfolder")
	checks := []struct {
		name string
		api  string
		keys []string
		want []string
	}{
		{name: "Pump.ReplaceDyingDefined", api: "Pump", keys: []string{"ReplaceDyingDefined$"}, want: []string{
			"b/bleed_dry.txt",
			"g/gnashing_of_teeth.txt",
			"n/necrotic_wound.txt",
			"o/ob_nixiliss_cruelty.txt",
		}},
		{name: "ChangeZone.ThisDefinedAndTgts", api: "ChangeZone", keys: []string{"ThisDefinedAndTgts$"}, want: []string{
			"c/carrionette.txt",
			"c/coastal_wizard.txt",
			"d/don_leo_problem_solvers.txt",
			"f/floodpits_drowner.txt",
			"g/giant_trap_door_spider.txt",
			"h/hunting_kavu.txt",
			"l/lady_sun.txt",
			"m/mangara_of_corondor.txt",
			"s/sandman_shifting_scoundrel.txt",
			"s/slimefoot_and_squee.txt",
			"s/snow_hound.txt",
			"s/suspend_aggression.txt",
			"v/void_stalker.txt",
			"w/wizard_mentor.txt",
		}},
		{name: "DigUntil.MinTotalCMC", api: "DigUntil", keys: []string{"MinTotalCMC$"}, want: []string{
			"d/dream_harvest.txt",
			"i/improvisation_capstone.txt",
			"t/tashas_hideous_laughter.txt",
		}},
		{name: "CopyPermanent.DefinedName", api: "CopyPermanent", keys: []string{"DefinedName$"}, want: []string{
			"a/arzakon.txt",
			"b/balefang_the_unslayable.txt",
			"b/black_lotus_lounge.txt",
			"d/disa_the_restless.txt",
			"d/domesticated_mammoth.txt",
			"d/dwarven_confluencer.txt",
			"e/elvish_impersonation_contest.txt",
			"f/feroz_ulgrothas_warden.txt",
			"g/generated_horizons.txt",
			"g/ginger_queen_of_sweets.txt",
			"g/greensleeves.txt",
			"m/mutable_explorer.txt",
			"n/nivea_beloved_battlemage.txt",
			"p/peacekeeper_avatar.txt",
			"r/ral_and_the_implicit_maze.txt",
			"t/tarmogoyf_nest.txt",
			"t/thomil_the_destroyer.txt",
			"w/waste_land.txt",
			"w/worzel_the_protector.txt",
		}},
		{name: "PutCounter.CounterTypes", api: "PutCounter", keys: []string{"CounterTypes$"}, want: []string{
			"a/abigale_eloquent_first_year.txt",
			"a/arcane_archery.txt",
			"a/arwen_mortal_queen.txt",
			"b/bribe_taker.txt",
			"c/captain_marvel_earths_protector.txt",
			"c/champion_of_dusan.txt",
			"e/elspeth_resplendent.txt",
			"e/exotic_pets.txt",
			"f/frankensteins_monster.txt",
			"g/gift_of_the_viper.txt",
			"k/klement_novice_acolyte.txt",
			"m/march_toward_perfection.txt",
			"p/placid_rottentail.txt",
			"q/qarsi_revenant.txt",
			"q/quicksilver_brash_blur.txt",
			"rebalanced/a-nezumi_prowler.txt",
			"s/scavenged_brawler.txt",
			"t/tenacious_pup.txt",
			"u/unexpected_fangs.txt",
		}},
		{name: "Animate.ChosenColor", api: "Animate", keys: []string{"Colors$ ChosenColor"}, want: []string{
			"a/alchors_tomb.txt",
			"b/blind_seer.txt",
			"c/caldera_kavu.txt",
			"d/distorting_lens.txt",
			"d/dream_coat.txt",
			"f/foraging_wickermaw.txt",
			"g/govern_the_guildless.txt",
			"i/illusion_reality.txt",
			"k/kavu_chameleon.txt",
			"m/mondo_gecko.txt",
			"p/prismatic_dragon.txt",
			"p/prismatic_lace.txt",
			"p/prismwake_merrow.txt",
			"p/pucas_eye.txt",
			"q/quickchange.txt",
			"r/rainbow_crow.txt",
			"s/scuttlemutt.txt",
			"s/shyft.txt",
			"s/sisays_ingenuity.txt",
			"s/spiritmonger.txt",
			"s/sway_of_illusion.txt",
			"s/swirling_spriggan.txt",
			"t/tidal_visionary.txt",
			"v/vodalian_mystic.txt",
			"w/wild_mongrel.txt",
		}},
		{name: "ChooseType.ValidInvalidTypes", api: "ChooseType", keys: []string{"ValidTypes$", "InvalidTypes$"}, want: []string{
			"a/a_killer_among_us.txt",
			"a/arachne_psionic_weaver.txt",
			"a/archon_of_valors_reach.txt",
			"b/____________rocketship.txt",
			"c/cloud_key.txt",
			"c/creeping_renaissance.txt",
			"d/dawn_blessed_pennant.txt",
			"e/eclipsed_realms.txt",
			"g/gollum_scheming_guide.txt",
			"i/imagecrafter.txt",
			"j/jukai_liberator.txt",
			"m/mistform_mutant.txt",
			"r/roots_of_life.txt",
			"s/standardize.txt",
			"s/stenn_paranoid_partisan.txt",
			"t/turnabout.txt",
			"u/unnatural_selection.txt",
			"w/winding_way.txt",
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			var got []string
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || filepath.Ext(path) != ".txt" {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				for _, line := range strings.Split(string(data), "\n") {
					if !strings.Contains(line, check.api) {
						continue
					}
					for _, key := range check.keys {
						if strings.Contains(line, key) {
							rel, _ := filepath.Rel(root, path)
							got = append(got, filepath.ToSlash(rel))
							break
						}
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(got)
			got = slices.Compact(got)
			if !slices.Equal(got, check.want) {
				t.Errorf("carrier files = %v, want %v", got, check.want)
			}
		})
	}
}
