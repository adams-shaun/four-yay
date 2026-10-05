package effects

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This census pins every corpus file carrying player-kind ValidTgts$ on a
// candidate *All API. It is deliberately API-specific: implementations
// with player-zone selection (ChangeZoneAll, TapAll family) are audited
// separately from battlefield object-controller sweeps.
func TestPlayerKindValidTgtsAPICensus(t *testing.T) {
	dir := "../.cards/cardsfolder"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("corpus not fetched; run `make fetch-cards`")
	}
	got := map[string]map[string]bool{}
	for _, api := range []string{"ChangeZoneAll", "PumpAll", "PutCounterAll", "TapAll", "DestroyAll", "TapOrUntapAll", "UntapAll", "AnimateAll", "DamageAll"} {
		got[api] = map[string]bool{}
	}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			tg := animateAllValidTgts(line)
			if tg == "" || !playerSpecBaseKnown(tg) {
				continue
			}
			_, rest, ok := strings.Cut(line, "$")
			if !ok {
				continue
			}
			api, _, _ := strings.Cut(strings.TrimSpace(rest), " | ")
			if _, ok := got[strings.TrimSpace(api)]; ok {
				got[strings.TrimSpace(api)][filepath.Base(path)] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk corpus: %v", err)
	}
	want := map[string][]string{
		"ChangeZoneAll": {
			"agent_of_erebos.txt", "angel_of_finality.txt", "apple_of_eden_isu_relic.txt", "blessed_respite.txt",
			"boggart_trawler_boggart_bog.txt", "bojuka_bog.txt", "callous_bloodmage.txt", "canoptek_scarab_swarm.txt",
			"clear_the_mind.txt", "cranial_archive.txt", "cranial_extraction.txt", "crypt_incursion.txt",
			"disciple_of_perdition.txt", "elspeths_nightmare.txt", "endurance.txt", "every_last_vestige_shall_rot.txt",
			"ghastly_conscription.txt", "grey_host_reinforcements.txt", "guiding_spirit.txt", "haunting_echoes.txt",
			"head_games.txt", "hedonists_trove.txt", "hurkyls_recall.txt", "hypnox.txt",
			"identity_crisis.txt", "induced_amnesia.txt", "into_the_earthen_maw.txt", "jace_the_mind_sculptor.txt",
			"jesters_mask.txt", "jirina_dauntless_general.txt", "jund_charm.txt", "kutzils_flanker.txt",
			"leadership_vacuum.txt", "learn_from_the_past.txt", "malanthrope.txt", "mudhole.txt",
			"nautiloid_ship.txt", "necromancers_covenant.txt", "nihil_spellbomb.txt", "phyrexian_furnace.txt",
			"primal_command.txt", "quest_for_ancient_secrets.txt", "rakdos_charm.txt", "ravenous_trap.txt",
			"release_to_memory.txt", "reminisce.txt", "remorseful_cleric.txt", "repopulate.txt",
			"rivers_rebuke.txt", "riveteers_charm.txt", "sentinel_of_lost_lore.txt", "settle_the_wreckage.txt",
			"shadow_of_the_enemy.txt", "stone_of_erech.txt", "stonespeaker_crystal.txt", "sudden_disappearance.txt",
			"suppress.txt", "the_death_of_gwen_stacy.txt", "the_heron_moon.txt", "thraben_charm.txt",
			"thran_foundry.txt", "titanias_command.txt", "tombfire.txt", "tormods_crypt.txt",
			"tormods_cryptkeeper.txt", "tranquil_frillback.txt", "unite_the_coalition.txt", "urza_academy_headmaster.txt",
			"waste_management.txt",
		},
		"PumpAll": {
			"arms_of_hadar.txt", "cabaretti_confluence.txt", "ego_erasure.txt", "gnashing_of_teeth.txt",
			"great_oak_guardian.txt", "grubs_command.txt", "how_to_start_a_riot.txt", "jamming_device.txt",
			"marsh_casualties.txt", "neutralize_the_guards.txt", "night_day.txt", "primaris_eliminator.txt",
			"savage_alliance.txt", "shields_of_velis_vel.txt", "syggs_command.txt", "torrent_of_souls.txt",
			"trystans_command.txt", "wail_of_war.txt",
		},
		"PutCounterAll": {
			"collective_effort.txt", "contagion_engine.txt", "corrosion.txt", "meadowboon.txt",
			"practiced_offense.txt", "requisition_raid.txt", "shadrix_silverquill.txt", "splinter_leo_father_son.txt",
			"twisted_fates.txt",
		},
		"TapAll": {
			"assassin_gauntlet.txt", "dawnglare_invoker.txt", "dovin_architect_of_law.txt", "gulf_squid.txt",
			"kiora_bests_the_sea_god.txt", "mana_short.txt", "mistbind_clique.txt", "naya_charm.txt",
			"sleep.txt", "suppression_ray_orderly_plaza.txt", "tempest_caller.txt",
		},
		"DestroyAll": {
			"ajani_vengeant.txt", "angrath_minotaur_pirate.txt", "behold_the_power_of_destruction.txt", "mogg_infestation.txt",
			"overwhelming_forces.txt", "primeval_light.txt", "rain_of_daggers.txt", "urza_academy_headmaster.txt",
		},
		"TapOrUntapAll": {
			"turnabout.txt",
		},
		"UntapAll": {
			"early_harvest.txt", "reins_of_power.txt", "rustler_rampage.txt",
		},
		"AnimateAll": {
			"curious_colossus.txt", "jolrael_empress_of_beasts.txt", "mass_diminish.txt", "polymorphists_jest.txt",
			"quick_draw.txt", "sudden_spoiling.txt",
		},
		"DamageAll": {
			"aggravate.txt", "ashlings_command.txt", "brigid_hero_of_kinsbaile.txt", "chandra_bold_pyromancer.txt",
			"chandra_flames_fury.txt", "chandras_flame_wave.txt", "chaos_balor.txt", "dwarven_catapult.txt",
			"rowan_kenrith.txt", "savage_alliance.txt", "seismic_wave.txt", "simoon.txt",
			"urabrask_the_great_work.txt",
		},
	}
	for api, wantNames := range want {
		gotNames := make([]string, 0, len(got[api]))
		for n := range got[api] {
			gotNames = append(gotNames, n)
		}
		sort.Strings(gotNames)
		sort.Strings(wantNames)
		if strings.Join(gotNames, ",") != strings.Join(wantNames, ",") {
			t.Errorf("%s carriers = %v, want %v", api, gotNames, wantNames)
		}
	}
}
