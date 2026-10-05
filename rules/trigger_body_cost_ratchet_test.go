package rules

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Full sorted carrier snapshot: identifiers/raw costs only, never Forge text.
const triggerBodyCostInventory = `
a-ancestral_katana.txt|T|TrigImmediateTrig|1
a-ardent_dustspeaker.txt|T|ABImpulse|PutCardToLibFromGrave<1/-1/Sorcery;Instant>
a-civil_servant.txt|T|TrigPump|tapXType<1/Citizen.Other>
a-dokuchi_silencer.txt|T|TrigImmediateTrig|Discard<1/Card>
a-elven_bow.txt|T|TrigToken|1
a-guide_of_souls.txt|T|TrigImmediateTrig|PayEnergy<4>
a-leyline_of_resonance.txt|T|TrigCopy|1
a-shessra_deaths_whisper.txt|T|TrigDraw|PayLife<2>
a_golden_opportunity.txt|K:Chapter|DBConjureEgg|tapXType<1/Bird> Sac<1/Artifact>
abomination_of_gudul.txt|T|TrigLoot|Draw<1/You>
academy_raider.txt|T|TrigDiscard|Discard<1/Card>
academy_rector.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>
academy_wall.txt|T|TrigLoot|Draw<1/You>
ace_fearless_rebel.txt|T|TrigImmediateTrig|Sac<1/Artifact>
adrianas_valor.txt|T|TrigPump|W
aegis_sculptor.txt|T|TrigPutCounter|ExileFromGrave<2/Card>
aerie_worshippers.txt|T|TrigToken|2 U
aether_chaser.txt|T|TrigToken|PayEnergy<2>
aether_herder.txt|T|TrigToken|PayEnergy<2>
aether_inspector.txt|T|TrigToken|PayEnergy<2>
aether_poisoner.txt|T|TrigToken|PayEnergy<2>
aether_swooper.txt|T|TrigToken|PayEnergy<2>
aetherplasm.txt|T|TrigBounce|Return<1/CARDNAME>
aetherstorm_roc.txt|T|TrigPutCounter|PayEnergy<2>
aetherstream_leopard.txt|T|TrigPump|PayEnergy<1>
agrus_kos_eternal_soldier.txt|T|TrigCopy|1 RW
ajanis_last_stand.txt|T|TrigDiesToken|Sac<1/CARDNAME>
akki_ronin.txt|T|TrigDraw|Discard<1/Card>
akoum_firebird.txt|T|TrigChange|4 R R
akoum_stonewaker.txt|T|TrigToken|2 R
akuta_born_of_ash.txt|T|TrigReturn|Sac<1/Swamp>
alesha_who_smiles_at_death.txt|T|TrigChange|WB WB
alistair_the_brigadier.txt|T|TrigPumpAll|8
altar_of_the_wretched_wretched_bonemass.txt|T|TrigDraw|Sac<1/Creature.!token/nontoken creature>
amber_gristle_omaul.txt|T|TrigDraw|Discard<1/Hand>
ambergris_citadel_agent.txt|T|TrigDamage|Discard<1/Hand> Draw<2/You>
ambulatory_edifice.txt|T|DBTrigger|PayLife<2>
ancestral_katana.txt|T|TrigImmediateTrig|1
angelic_renewal.txt|T|TrigReturn|Sac<1/CARDNAME>
animation_module.txt|T|TrigToken|1
anointer_of_valor.txt|T|TrigImmediateTrig|3
ant_man_colony_commander.txt|T|TrigImmediateTrig|1
aphelia_viper_whisperer.txt|T|TrigToken|1 BG
aphemia_the_cacophony.txt|T|DBToken|ExileFromGrave<1/Enchantment>
apothecary_initiate.txt|T|TrigGainLife|1
arahbo_roar_of_the_world.txt|T|TrigPump2|1 G W
ardent_dustspeaker.txt|T|ABImpulse|PutCardToLibFromGrave<1/-1/Sorcery;Instant>
arena_rector.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>
arni_metalbrow.txt|T|TrigChangeZone|1 R
arni_metalbrow.txt|T|TrigChangeZoneBis|1 R
arrogant_poet.txt|T|TrigPump|PayLife<2>
artillery_enthusiast.txt|T|TrigSeek|Discard<1/Card>
artists_talent.txt|T|TrigDiscard|Discard<1/Card>
asgardian_inspiration.txt|T|TrigChangeZone|2
ashcoat_of_the_shadow_swarm.txt|T|TrigChange|Mill<4>
ashling_rekindled_ashling_rimebound.txt|T|TrigDraw|Discard<1/Card>
ashling_rekindled_ashling_rimebound.txt|T|TrigTransform|R
ashnod_flesh_mechanist.txt|T|TrigToken|Sac<1/Creature.Other/another creature>
aspiring_champion.txt|T|TrigDig|Mandatory Sac<1/CARDNAME>
assaultron_dominator.txt|T|TrigPutCounter|PayEnergy<1>
astonishing_spider_man.txt|T|TrigDraw|Discard<1/Hand>
atraxas_skitterfang.txt|T|DBTrigger|SubCounter<1/OIL>
aurora_shifter.txt|T|TrigImmediateTrig|PayEnergy<2>
auspicious_ancestor.txt|T|TrigGainLife|1
automated_warfare_system.txt|T|TrigDraw|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>
awaken_the_sky_tyrant.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>
aziza_mage_tower_captain.txt|T|TrigCopy|tapXType<3/Creature>
azor_the_lawbringer.txt|T|TrigDraw|X W U U
azorius_aethermage.txt|T|TrigDraw|1
azra_oddsmaker.txt|T|ChooseCreature|Discard<1/Card>
balthier_and_fran.txt|T|TrigAddCombat|1 R G
banon_the_returners_leader.txt|T|TrigDraw|1 Discard<1/Card>
baral_chief_of_compliance.txt|T|TrigLoot|Draw<1/You>
battlefield_scavenger.txt|T|TrigDiscard|Discard<1/Card>
battlemages_bracers.txt|T|TrigCopyAbility|1
bearer_of_silence.txt|T|TrigSacrifice|1 C
bebop_skull_crossbones.txt|T|TrigLoseLife|Draw<X/You>
beetle_headed_merchants.txt|T|TrigDraw|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>
benalish_partisan.txt|T|TrigReturn|1 W
benthic_criminologists.txt|T|TrigDraw|Sac<1/Artifact>
biblioplex_kraken.txt|T|TrigUnblockable|Return<1/Creature.Other>
big_wheel.txt|T|TrigDiscard|Discard<1/Card>
biting_palm_ninja.txt|T|TrigImmediateTrig|SubCounter<1/Menace>
bitter_chill.txt|T|TrigDraw|1
bitter_reunion.txt|T|TrigDiscard|Discard<1/Card>
blight_herder.txt|T|TrigToken|ExiledMoveToGrave<2/Card.OppOwn/cards your opponents own>
blighted_blackthorn.txt|T|TrigDraw|Blight<2>
blind_zealot.txt|T|TrigDestroy|Sac<1/CARDNAME>
blood_operative.txt|T|TrigReturn|PayLife<3>
blood_speaker.txt|T|TrigSearch|Sac<1/CARDNAME>
bloodcrazed_socialite.txt|T|TrigPump|Sac<1/Blood.token/Blood token>
bloodfeather_phoenix.txt|T|TrigReturn|R
bloodmist_infiltrator.txt|T|TrigUnblockable|Sac<1/Creature.Other/another creature>
bloodthirsty_adversary.txt|T|TrigPay|Mana<2 R\NumTimes>
bog_strider_ash.txt|T|TrigGainLife|G
boggart_mischief.txt|T|TrigToken|Blight<1>
boilerbilges_ripper.txt|T|TrigSac|Sac<1/Creature.Other;Enchantment.Other/another creature or enchantment>
bolg_of_the_north.txt|T|TrigSac|Sac<1/Creature.Other/another creature>
boneyard_scourge.txt|T|TrigReturn|1 B
booby_trap.txt|T|TrapTriggered|Mandatory Sac<1/CARDNAME>
book_devourer.txt|T|TrigDiscard|Discard<1/Hand>
boundary_lands_ranger.txt|T|TrigDraw|Discard<1/Card>
braidss_frightful_return.txt|K:Chapter|ABDiscard|Sac<1/Creature>
bramble_sovereign.txt|T|TrigCopy|1 G
brass_gnat.txt|T|TrigUntap|1
brass_man.txt|T|TrigUntap|1
brawl_bash_ogre.txt|T|TrigPump|Sac<1/Creature.Other/another creature>
breeches_the_blastmaker.txt|T|TrigFlip|Sac<1/Artifact>
brigid_clachans_heart_brigid_douns_mind.txt|T|TrigTransform|W
brilliant_wings.txt|T|TrigAttach|1
bringer_of_the_black_dawn.txt|T|TrigChange|PayLife<2>
bristlebud_farmer.txt|T|TrigMill|Sac<1/Food>
brood_astronomer.txt|T|TrigDraft|Sac<1/Land>
burning_tree_vandal.txt|T|TrigDraw|Discard<1/Card>
bushy_bodyguard.txt|T|TrigPutCounter|Forage
buzzard_wasp_colony.txt|T|TrigDraw|Sac<1/Artifact;Creature/artifact or creature>
byway_barterer.txt|T|TrigDraw|Discard<1/Hand>
cabal_therapist.txt|T|DBImmediateTrigger|Sac<1/Creature>
cacophony_scamp.txt|T|TrigProliferate|Sac<1/CARDNAME>
cadric_soul_kindler.txt|T|TrigCopy|1
caesar_legions_emperor.txt|T|TrigImmediateTrig|Sac<1/Creature.Other/another creature>
call_of_the_ring.txt|T|TrigDraw|PayLife<2>
campsite_cuisine.txt|T|TrigImmediateTrig|Sac<X/Food>
canoptek_wraith.txt|T|TrigSearch|3 Sac<1/CARDNAME>
caparocti_sunborn.txt|T|TrigDiscover|tapXType<2/Artifact;Creature/artifacts and/or creatures>
carefree_swinemaster.txt|T|TrigToken|1 G
carrion_thrash.txt|T|TrigChange|2
cavalier_of_night.txt|T|TrigSac|Sac<1/Creature.Other/another creature>
cavalier_of_thorns.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>
celestine_cave_witch.txt|T|TrigCurse|Sac<1/Insect>
cemetery_puca.txt|T|CemeteryPucaCopy|1
centaur_vinecrasher.txt|T|TrigReturn|G G
champion_of_wits.txt|T|TrigDraw|Draw<X/You>
chandras_regulator.txt|T|TrigCopyAbility|1
chipper_chopper.txt|T|TrigPutCounter|Sac<1/Artifact.Other/another artifact>
chitterspitter.txt|T|TrigPutCounter|Sac<1/Permanent.token/token>
circle_of_affliction.txt|T|TrigDrain|1
civil_servant.txt|T|TrigPump|tapXType<1/Citizen.Other>
cloudpiercer.txt|T|TrigDiscard|Discard<1/Card>
cloven_casting.txt|T|TrigCopy|1
cogwork_progenitor.txt|T|TrigSeek|ExileCtrlOrGrave<1/Artifact.Other>
colfenors_urn.txt|T|TrigReturnAll|Mandatory Sac<1/CARDNAME>
comet_crawler.txt|T|TrigPump|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>
common_iguana.txt|T|TrigDiscard|Discard<1/Card>
conduit_goblin.txt|T|TrigPump|PayEnergy<1>
conspiracy_theorist.txt|T|TrigDraw|1 Discard<1/Card>
conspiracy_theorist.txt|T|TrigEffect|ExileFromGrave<1/Card.TriggeredCards>
consuls_shieldguard.txt|T|TrigPump|PayEnergy<1>
cool_but_rude.txt|T|TrigDraw|Discard<1/Card>
copy_catchers.txt|T|TrigCopy|1 U
cornered_crook.txt|T|TrigImmediateTrig|Sac<1/Artifact>
corpseberry_cultivator.txt|T|TrigPump|Forage
corpses_of_the_lost.txt|T|TrigReturn|PayLife<1>
councils_deliberation.txt|T|DBDraw|ExileFromGrave<1/CARDNAME>
cradle_of_vitality.txt|T|TrigPutCounter|1 W
creeping_chill.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredCard>
crosis_the_purger.txt|T|TrigChoose|2 B
crossway_troublemakers.txt|T|TrigDraw|PayLife<2>
crucias_titan_of_the_waves.txt|T|TrigToken|Discard<1/Card>
crystal_rod.txt|T|TrigGainLife|1
curious_forager.txt|T|TrigImmediateTrig|Forage
curse_of_silence.txt|T|TrigDraw|Sac<1/CARDNAME>
customs_depot.txt|T|TrigLoot|1
cyclops_superconductor.txt|T|DBTrigger|PayEnergy<3>
dalek_intensive_care.txt|T|TrigExile|Mandatory Exile<1/Creature.nonDalek/non-Dalek creature>
dance_of_the_dead.txt|T|TrigUntap|1 B
daretti_rocketeer_engineer.txt|T|TrigReturn|Sac<1/Artifact>
darigaaz_the_igniter.txt|T|TrigChooseColor|2 R
daring_saboteur.txt|T|TrigLoot|Draw<1/You>
dark_depths.txt|T|TrigToken|Mandatory Sac<1/CARDNAME>
dauthi_mindripper.txt|T|TrigDiscard|Sac<1/CARDNAME>
dawn_of_hope.txt|T|TrigDraw|2
deadly_designs.txt|T|TrigDestroy|Mandatory Sac<1/CARDNAME>
death_priest_of_myrkul.txt|T|TrigToken|1
death_spark.txt|T|TrigReturn|1
decree_of_justice.txt|T|TrigToken|X
depala_pilot_exemplar.txt|T|TrigDig|X
descendant_of_storms.txt|T|TrigEndure|1 W
descendants_fury.txt|T|TrigDigUntil|Sac<1/Card.TriggeredSources>
digsite_conservator.txt|T|TrigDiscover|4
digsite_engineer.txt|T|TrigToken|2
dire_fleet_warmonger.txt|T|TrigPump|Sac<1/Creature.Other/another creature>
discerning_peddler.txt|T|TrigDiscard|Discard<1/Card>
disciple_of_deceit.txt|T|TrigSearch|Discard<1/Card.nonLand/nonland card>
disciple_of_freyalise_garden_of_freyalise.txt|T|TrigGainLife|Sac<1/Creature.Other/another creature>
disturbing_mirth.txt|T|TrigDraw|Sac<1/Enchantment.Other;Creature.Other/another enchantment or creature>
dokuchi_silencer.txt|T|TrigImmediateTrig|Discard<1/Creature>
doombot_harbinger.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>
doric_natures_warden_doric_owlbear_avenger.txt|T|TrigTransform|1 G
drainpipe_vermin.txt|T|TrigDiscard|B
drake_haven.txt|T|TrigToken|1
draugrs_helm.txt|T|TrigToken|2 B
dream_seizer.txt|T|TrigDiscard|Blight<1>
dreamcatcher.txt|T|TrigDraw|Sac<1/CARDNAME>
dreamshaper_shaman.txt|T|TrigDig|2 R Sac<1/Permanent.nonLand/nonland permanent>
dromar_the_banisher.txt|T|TrigChoose|2 U
drowner_initiate.txt|T|TrigMill|1
duelist_of_the_mind.txt|T|TrigLoot|Draw<1/You>
durable_handicraft.txt|T|TrigPutCounter|1
dutiful_replicator.txt|T|TrigImmediate|1
dwarven_hammer.txt|T|TrigToken|2
ecstatic_electromancer.txt|T|TrigDraw|Discard<1/Card>
eddie_brock_venom_lethal_protector.txt|T|TrigDraw|Sac<1/Creature.Other/another creature>
eddytrail_hawk.txt|T|TrigPump|PayEnergy<1>
edgars_awakening.txt|T|TrigImmediateTrig|B
eirdu_carrier_of_dawn_isilu_carrier_of_twilight.txt|T|TrigTransform|W
elaborate_firecannon.txt|T|TrigUntap|Discard<1/Card>
eldrazi_obligator.txt|T|TrigChange|1 C
electro_assaulting_battery.txt|T|TrigImmediateTrig|X
electropotence.txt|T|TrigDamage|2 R
elenda_and_azor.txt|T|TrigDraw|X W U B
elenda_and_azor.txt|T|TrigToken|PayLife<4>
elusive_tormentor_insidious_mist.txt|T|TrigTransform|2 B
elven_bow.txt|T|TrigToken|2
embereth_skyblazer.txt|T|TrigPumpAll|2 R
embersmith.txt|T|TrigDamage|1
emeritus_of_ideation_ancestrall_recall.txt|T|TrigPrepare|ExileFromGrave<8/Card>
emiel_the_blessed.txt|T|TrigPutCounter|GW
emperor_mihail_ii.txt|T|TrigToken|1
endless_ranks_of_hydra.txt|T|TrigReturn|1 B
enigmatic_incarnation.txt|T|TrigSearch|Sac<1/Enchantment.Other/another enchantment>
equilibrium.txt|T|TrigBounce|1
era_of_innovation.txt|T|TrigEnergy|1
erebos_bleak_hearted.txt|T|ABDraw|PayLife<2>
ereboss_titan.txt|T|TrigReturn|Discard<1/Card>
escape_protocol.txt|T|TrigImmediateTrig|1
esoteric_duplicator.txt|T|TrigDelayedTrig|2
estwald_shieldbasher.txt|T|TrigPump|1
eternal_taskmaster.txt|T|TrigChange|2 B
euru_acorn_scrounger.txt|T|TrigImmediateTrig|Forage
euru_acorn_scrounger.txt|T|TrigPutCounterAll|Sac<1/Permanent.token/token>
evereth_viceroy_of_plunder.txt|T|TrigImmediateTrig|1 BR
every_last_vestige_shall_rot.txt|T|MoveToBottom|X
evidence_examiner.txt|T|TrigEvidence|CollectEvidence<4>
excavating_anurid.txt|T|TrigDraw|Sac<1/Land>
extricator_of_sin_extricator_of_flesh.txt|T|TrigToken|Sac<1/Permanent.Other/another permanent>
eye_of_vecna.txt|T|TrigDrawUpkeep|2
eyes_of_the_watcher.txt|T|TrigScry|1
ezuri_stalker_of_spheres.txt|T|TrigProliferate|3
faith_of_the_devoted.txt|T|TrigDrain|1
fathom_fleet_captain.txt|T|TrigToken|2
feed_the_pack.txt|T|TrigToken|Sac<1/Creature.!token/nontoken creature>
felhide_spiritbinder.txt|T|TrigCopy|1 R
felothar_dawn_of_the_abzan.txt|T|TrigImmediateTrig|Sac<1/Permanent.nonLand/nonland permanent>
fetid_gargantua.txt|T|TrigLoseLife|Draw<2/You>
filigree_racer.txt|T|TrigTrigger|PayEnergy<2>
fire_lord_ozai.txt|T|TrigMana|Sac<1/Creature.Other/another creature>
fissure_wizard.txt|T|TrigDiscard|Discard<1/Card>
flame_kin_war_scout.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>
flameblast_dragon.txt|T|TrigDamage|X R
flameshadow_conjuring.txt|T|TrigCopy|R
flamespeakers_will.txt|T|TrigDestroy|Sac<1/CARDNAME>
flamewake_phoenix.txt|T|TrigReturn|R
flaring_cinder.txt|T|TrigDraw|Discard<1/Card>
flaxen_intruder_welcome_home.txt|T|TrigTrig|Sac<1/CARDNAME>
flight_spellbomb.txt|T|TrigDraw|U
foot_chopper.txt|T|TrigDraw|Sac<1/Card.TriggeredSource/that creature>
foreboding_steamboat.txt|T|TrigInvestigate|ExiledMoveToGrave<1/Card.ExiledWithSource/card exiled with CARDNAME>
forgehammer_centurion.txt|T|TrigImmediateTrig|SubCounter<2/OIL>
forgotten_creation.txt|T|TrigDiscard|Discard<1/Hand>
forgotten_harvest.txt|T|TrigPutCounter|ExileFromGrave<1/Land>
forlorn_pseudamma.txt|T|GFGToken|2 B
formidable_speaker.txt|T|TrigSearch|Discard<1/Card>
forsaken_city.txt|T|TrigUntap|ExileFromHand<1/Card>
forsaken_miner.txt|T|TrigChange|B
foster.txt|T|TrigDig|1
founding_of_omashu.txt|K:Chapter|DBDraw|Discard<1/Card>
frenzied_geistblaster.txt|T|TrigSeek|Discard<1/Card>
frenzied_goblin.txt|T|TrigPump|R
freyalises_charm.txt|T|TrigDraw|G G
furious_forebear.txt|T|TrigChangeZone|1 W
furnace_celebration.txt|T|TrigDealDamage|2
furnace_scamp.txt|T|TrigDamage|Sac<1/CARDNAME>
furyblade_vampire.txt|T|TrigPump|Discard<1/Card>
gamekeeper.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>
gastal_blockbuster.txt|T|TrigSac|Sac<1/Creature;Vehicle/a creature or Vehicle>
general_traag_heart_of_stone.txt|T|TrigImmediateTrig|Sac<1/Artifact.Other/another artifact>
genesis.txt|T|TrigChange|2 G
gert_and_old_lace_runaways.txt|T|TrigSearch|Discard<1/Card>
ghastly_remains.txt|T|TrigReturn|B B B
ghitu_embercoiler.txt|T|TrigSeek|Discard<1/Card>
ghostly_pilferer.txt|T|TrigDraw1|2
giant_albatross.txt|T|TrigEach|1 U
giants_amulet.txt|T|TrigToken|3 U
gigapede.txt|T|TrigChange|Discard<1/Card>
gilded_ambusher.txt|T|TrigImmediateTrig|Sac<1/Permanent.Other+nonLand/another nonland permanent>
giott_king_of_the_dwarves.txt|T|TrigDraw|Discard<1/Card>
gitaxian_anatomist.txt|T|TrigProliferate|tapXType<1/Card.Self/CARDNAME>
glint_sleeve_siphoner.txt|T|TrigDraw|PayEnergy<2>
glorifier_of_suffering.txt|T|TrigSac|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>
go_shintai_of_ancient_wars.txt|T|TrigImmediateTrig|1
go_shintai_of_boundless_vigor.txt|T|TrigImmediateTrig|1
go_shintai_of_hidden_cruelty.txt|T|TrigImmediateTrig|1
go_shintai_of_lost_wisdom.txt|T|TrigImmediateTrig|1
go_shintai_of_shared_purpose.txt|T|TrigToken|1
goblin_dirigible.txt|T|TrigUntap|4
goblin_grenadiers.txt|T|TrigDestroyCreature|Sac<1/CARDNAME>
goblin_vandal.txt|T|TrigDestroy|R
goblin_war_wagon.txt|T|TrigUntap|2
goblinslide.txt|T|TrigToken|1
god_favored_general.txt|T|GFGToken|2 W
gorbag_of_minas_morgul.txt|T|TrigImmediateTrig|Sac<1/Card.TriggeredSource/that creature>
graha_tia_scion_reborn.txt|T|TrigToken|PayLife<X>
grave_peril.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>
gravelgill_scoundrel.txt|T|TrigUnblockable|tapXType<1/Creature.Other>
gravity_negator.txt|T|TrigPump|C
great_desert_hellion.txt|T|TrigDraw|Discard<1/Hand>
green_goblin_back_for_more.txt|T|TrigDiscard|Discard<1/Card>
greenwarden_of_murasa.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>
greven_predator_captain.txt|T|TrigDraw|Sac<1/Creature.Other/another creature>
grim_reaper_lethal_legionnaire.txt|T|TrigTrigger|3 B
grist_voracious_larva_grist_the_plague_swarm.txt|T|TrigTransform|G
grovetender_druids.txt|T|TrigToken|1
grub_storied_matriarch_grub_notorious_auntie.txt|T|TrigCopy|Blight<1>
grub_storied_matriarch_grub_notorious_auntie.txt|T|TrigTransform|B
gryffwing_cavalry.txt|T|TrigPump|1 W
guide_of_souls.txt|T|TrigImmediateTrig|PayEnergy<3>
guiding_hydra.txt|T|TrigPump|SubCounter<1/P1P1>
gut_true_soul_zealot.txt|T|TrigToken|Sac<1/Creature.Other;Artifact/another creature or an artifact>
haazda_snare_squad.txt|T|TrigTap|W
halana_kessig_ranger.txt|T|TrigPayCost|2
halo_forager.txt|T|TrigImmediateTrig|X
hangar_scrounger.txt|T|TrigDraw|Discard<1/Card>
harvester_troll.txt|T|TrigPutCounter|Sac<1/Creature;Land/creature or land>
hashaton_scarabs_fist.txt|T|TrigCopy|2 U
haunted_cadaver.txt|T|TrigDiscard|Sac<1/CARDNAME>
haunted_library.txt|T|TrigToken|1
hawkeye_master_marksman.txt|T|TrigImmediateTrigger|Mana<1\NumTimes>
hazorets_monument.txt|T|TrigDiscard|Discard<1/Card>
heart_piercer_manticore.txt|T|DBTrigger|Sac<1/Creature.Other/another creature>
hei_bai_spirit_of_balance.txt|T|TrigPutCounter1|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>
hellkite_charger.txt|T|TrigUntap|5 R R
hematite_talisman.txt|T|TrigUntap|3
herigast_erupting_nullkite.txt|T|TrigDraw|ExileFromHand<1/All>
hero_of_leina_tower.txt|T|TrigPutCounter|X
hexgold_slith.txt|T|TrigPump|PayEnergy<2>
high_society_hunter.txt|T|TrigPutCounter|Sac<1/Creature.Other/another creature>
hired_heist.txt|T|TrigDraw|U
hollow_specter.txt|T|TrigDiscard|X
hordewing_skaab.txt|T|TrigDraw|Draw<X/You>
horizon_spellbomb.txt|T|TrigDraw|G
hormagaunt_horde.txt|T|TrigReturn|2 G
horrid_shadowspinner.txt|T|TrigDraw|Draw<X/You>
hostile_hostel_creeping_inn.txt|T|TrigDrain|ExileFromGrave<1/Creature/creature card>
human_torch.txt|T|DBEffect|R G W U
hurkyls_prodigy.txt|T|TrigUnblockable|2
hurska_sweet_tooth.txt|T|TrigImmediateTrig|GW
hylda_of_the_icy_crown.txt|T|TrigImmediateTrig|1
iceman_and_firestar.txt|T|TrigDraw|Discard<1/Card>
icewrought_sentry.txt|T|TrigTrigger|1 U
ichorid.txt|T|TrigReturn|ExileFromGrave<1/Creature.Black+Other>
immersturm_raider.txt|T|TrigDiscard|Discard<1/Card>
imoen_trickster_friend.txt|T|TrigCounter|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>
imoen_trickster_friend.txt|T|TrigDamage|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>
imoen_trickster_friend.txt|T|TrigDraw|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>
imoen_trickster_friend.txt|T|TrigImmTrigger|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>
imoen_trickster_friend.txt|T|TrigToken|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>
impaler_shrike.txt|T|TrigDraw|Sac<1/CARDNAME>
in_the_pale_moonlight.txt|K:Chapter|ABToken|Sac<1/Artifact;Creature/artifact or creature>
inalla_archmage_ritualist.txt|T|TrigCopyPermanent|1
iname_as_one.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>
incinerator_of_the_guilty.txt|T|TrigImmediateTrig|CollectEvidence<X>
indoctrination_attendant.txt|T|TrigToken|Return<1/Permanent.Other/other permanent>
industrial_advancement.txt|T|TrigDig|Sac<1/Creature>
ingenious_prodigy.txt|T|TrigDraw|SubCounter<1/P1P1>
inheritance.txt|T|TrigDraw|3
insidious_bookworms.txt|T|TrigDiscard|1 B
interceptor_shadows_hound.txt|T|TrigChangeZone|2 B
intet_the_dreamer.txt|T|TrigExile|2 U
inti_seneschal_of_the_sun.txt|T|TrigImmediateTrig|Discard<1/Card/card>
intimidator_initiate.txt|T|TrigPumpCurse|1
intrepid_adversary.txt|T|TrigPay|Mana<1 W\NumTimes>
invasion_of_ergamon_truga_cliffcharger.txt|T|TrigChangeZone|Discard<1/Card>
invasion_of_mercadia_kyren_flamewright.txt|T|TrigDiscard|Discard<1/Card>
invasion_of_new_capenna_holy_frazzle_cannon.txt|T|TrigImmediateTrig|Sac<1/Artifact;Creature/artifact or creature>
invisible_woman.txt|T|TrigImmediateTrig|R G W U
iron_star.txt|T|TrigGainLife|1
ironclad_revolutionary.txt|T|TrigPutCounter|Sac<1/Artifact>
irreverent_gremlin.txt|T|TrigDraw|Discard<1/Card>
isareth_the_awakener.txt|T|TrigImmediateTrig|X
island_fish_jasconius.txt|T|TrigUntap|U U U
itzquinth_firstborn_of_gishath.txt|T|TrigImmediate|2
ivory_cup.txt|T|TrigGainLife|1
izoni_center_of_the_web.txt|T|TrigToken|CollectEvidence<4>
izzet_keyrune.txt|T|TrigLoot|Draw<1/You>
jackdaw.txt|T|TrigDiscard|Discard<0/Hand>
jasconian_isle.txt|T|TrigUntap|U U
jedit_ojanen_mercenary.txt|T|TrigToken|G
jerren_corrupted_bishop_ormendahl_the_corruptor.txt|T|TrigTransform|4 B B
jeskai_ascendancy.txt|T|TrigLoot|Draw<1/You>
jeskai_elder.txt|T|TrigLoot|Draw<1/You>
jeweled_torque.txt|T|TrigGainLife|2
jubilant_mascot.txt|T|TrigPutCounter|3 W
jugan_defends_the_temple_remnant_of_the_rising_star.txt|T|TrigImmediateTrig|X
kaito_dancing_shadow.txt|T|TrigLoyalty|Return<1/Card.TriggeredSources>
kalastria_highborn.txt|T|TrigLoseLife|B
kappa_tech_wrecker.txt|T|TrigImmediateTrig|SubCounter<1/Deathtouch>
karlach_raging_tiefling.txt|T|TrigDraw|Sac<1/Creature>
katara_waterbending_master.txt|T|TrigDraw|Draw<X/You>
kate_stewart.txt|T|TrigPumpAll|8
kavaron_harrier.txt|T|TrigTokenAttacking|2
keldon_raider.txt|T|TrigDiscard|Discard<1/Card>
kels_fight_fixer.txt|T|TrigDraw|UB
kethek_crucible_goliath.txt|T|TrigSac|Sac<1/Creature.StrictlyOther/another creature>
key_to_the_city.txt|T|TrigDraw|2
kheru_lich_lord.txt|T|TrigChangZone|2 B
kickoff_celebrations.txt|T|TrigDraw|Discard<1/Card>
kill_zone_acrobat.txt|T|TrigPump|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>
killer_service.txt|T|TrigToken|2 Sac<1/Permanent.token/token>
killians_confidence.txt|T|TrigChangeZone|WB
killmonger_scourge_of_wakanda.txt|T|TrigSac|Sac<1/Creature.Other/another creature>
kilnspire_district.txt|T|RolledChaos|X
kinzu_of_the_bleak_coven.txt|T|TrigExile|PayLife<2> ExileAnyGrave<1/Card.TriggeredNewCard>
kishla_trawlers.txt|T|TrigImmediateTrig|ExileFromGrave<1/Creature/creature card>
kitt_kanto_mayhem_diva.txt|T|TrigImmediateTrig|tapXType<2/Creature>
knowledge_and_power.txt|T|TrigDmg|2
kozileks_return.txt|T|DBDamageAll|ExileFromGrave<1/CARDNAME>
krenko_baron_of_tin_street.txt|T|TrigToken|R
krovod_haunch.txt|T|TrigToken|1 W
kroxa_and_kunoros.txt|T|TrigImmediateTrig|ExileFromGrave<5/Card>
krydle_of_baldurs_gate.txt|T|TrigUnblockable|2
kuldotha_flamefiend.txt|T|TrigDealDamage|Sac<1/Artifact>
kurkesh_onakke_ancient.txt|T|TrigCopyAbility|R
labyrinth_adversary.txt|T|TrigImmediateTrig|1 R
lamplight_phoenix.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard> CollectEvidence<4>
lamplighter_of_selhoff.txt|T|TrigDraw|Draw<1/You>
lapis_lazuli_talisman.txt|T|TrigUntap|3
larval_scoutlander.txt|T|TrigChangeZone|Sac<1/Land;Lander/land or Lander>
lattice_blade_mantis.txt|T|TrigUntap|SubCounter<1/OIL>
lazotep_chancellor.txt|T|TrigAmass|1
leaf_crowned_visionary.txt|T|TrigDraw|G
leatherhead_swamp_stalker.txt|T|TrigImmediateTrig|RemoveAnyCounter<1/Any/NICKNAME>
leshracs_sigil.txt|T|TrigDiscard|B B
leviathan.txt|T|TrigUntap|Sac<2/Island>
leyline_of_lightning.txt|T|TrigDealDamage|1
leyline_tyrant.txt|T|TrigImmediateTrig|X
lichs_relic.txt|T|TrigImmediateTrig|2
lifecrafters_bestiary.txt|T|TrigDraw|G
lifesmith.txt|T|TrigGainLife|1
lightning_cloud.txt|T|TrigDealDamage|R
lightning_phoenix.txt|T|TrigReturn|R
lightning_rift.txt|T|TrigDamage|1
lilianas_devotee.txt|T|TrigToken|1 B
lim_dul_the_necromancer.txt|T|TrigReturn|1 B
lingering_phantom.txt|T|TrigReturn|B
living_artifact.txt|T|TrigGainLife|SubCounter<1/VITALITY>
living_lore.txt|T|TrigSacLore|Sac<1/CARDNAME>
llanowar_sentinel.txt|T|TrigChange|1 G
loch_dragon.txt|T|TrigDraw|Discard<1/Card>
lorcan_warlock_collector.txt|T|TrigReanimate|PayLife<X>
lorehold_the_historian.txt|T|TrigDraw|Discard<1/Card>
lorthos_the_tidemaker.txt|T|TrigTap|8
lunar_mystic.txt|T|TrigDraw|1
madame_null_power_broker.txt|T|TrigPutCounter|PayLife<X>
magnanimous_magistrate.txt|T|TrigChangeZone|SubCounter<X/REPR>
malachite_talisman.txt|T|TrigUntap|3
mana_vault.txt|T|TrigUntap|4
maralen_of_the_mornsong_avatar.txt|T|TrigPayLife|PayLife<X>
marchesa_dealer_of_death.txt|T|TrigDig|1
marit_lages_slumber.txt|T|TrigToken|Mandatory Sac<1/CARDNAME>
markov_purifier.txt|T|TrigDraw|2
marooned.txt|T|TrigTap|3
mask_of_griselbrand.txt|T|TrigDraw|PayLife<X>
mask_of_memory.txt|T|TrigLoot|Draw<2/You>
masked_admirers.txt|T|TrigReturn|G G
masked_vandal.txt|T|TrigExile|ExileFromGrave<1/Creature>
master_of_death.txt|T|TrigReturn|PayLife<1>
master_skald.txt|T|DBFetch|ExileFromGrave<1/Creature>
matt_murdock_justice_seeker.txt|T|TrigImmediateTrig|1
maulfist_doorbuster.txt|T|TrigPump|PayEnergy<1>
meanders_guide.txt|T|TrigImmediateTrig|tapXType<1/Merfolk.Other>
meathook_massacre_ii.txt|T|TrigReturn1|PayLife<3>
megatron_tyrant_megatron_destructive_force.txt|T|TrigImmediate|Sac<1/Artifact.Other/another artifact>
melded_moxite.txt|T|TrigDiscard|Discard<1/Card>
mentor_of_the_meek.txt|T|TrigDraw|1
mercurial_spelldancer.txt|T|TrigDelayTrig|SubCounter<2/OIL>
merfolk_seer.txt|T|TrigDraw|1 U
merry_bards.txt|T|TrigImmediate|1
miara_thorn_of_the_glade.txt|T|TrigDraw|1 PayLife<1>
mica_reader_of_ruins.txt|T|TrigCopy|Sac<1/Artifact>
militias_pride.txt|T|TrigToken|W
mind_raker.txt|T|TrigChangeZone|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>
minds_eye.txt|T|TrigDraw|1
mindstab_thrull.txt|T|TrigDiscard|Sac<1/CARDNAME>
minion_reflector.txt|T|TrigCopy|2
mirari.txt|T|TrigCopy|3
mirrorworks.txt|T|TrigCopy|2
mishras_self_replicator.txt|T|TrigCopy|1
mizzix_replica_rider.txt|T|TrigCopy|1 UR
moku_meandering_drummer.txt|T|TrigPump|1
monstrosity_of_the_lake.txt|T|TrigTapAll|5
moon_circuit_hacker.txt|T|TrigDraw|Draw<1/You>
moonveil_regent.txt|T|TrigDraw|Discard<0/Hand>
mortal_obstinacy.txt|T|TrigDestroy|Sac<1/CARDNAME>
mortarion_daemon_primarch.txt|T|TrigToken|X
mukotai_soulripper.txt|T|TrigPutCounter|Sac<1/Creature.Other;Artifact.Other/another artifact or creature>
murasa_ranger.txt|T|TrigPutCounters|3 G
murder_of_crows.txt|T|TrigLoot|Draw<1/You>
murk_strider.txt|T|TrigChangeZone|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>
my_genius_knows_no_bounds.txt|T|GeniusLife|X
myr_battlesphere.txt|T|TrigPump|tapXType<X/Myr>
myrsmith.txt|T|TrigToken|1
mystery_key.txt|T|TrigSacrifice|Mandatory Sac<1/CARDNAME>
nacre_talisman.txt|T|TrigUntap|3
nadir_kraken.txt|T|TrigPutCounter|1
najal_the_storm_runner.txt|T|TrigDelayedTrigger|2
naktamun.txt|T|RolledChaos|Discard<1/Card>
namazu_trader.txt|T|TrigSurveil|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>
narset_jeskai_waymaster.txt|T|TrigDraw|Discard<1/Hand>
nazar_the_velvet_fang.txt|T|TrigDraw|SubCounter<3/FEEDING>
necrite.txt|T|TrigDestroy|Sac<1/CARDNAME>
necrodominance.txt|T|TrigDraw|PayLife<X>
necromaster_dragon.txt|T|TrigToken|2
nether_traitor.txt|T|TrigReturn|B
nevinyrral_urborg_tyrant.txt|T|TrigPayCost|1
neyith_of_the_dire_hunt.txt|T|TrigPump|2 RG
nihil_spellbomb.txt|T|TrigDraw|B
nim_deathmantle.txt|T|TrigReturn|4
niv_mizzet_ghost_counsel.txt|T|TrigDraw|PayLife<X>
null_group_biological_assets.txt|T|TrigDraw|Discard<1/Card>
numa_joraga_chieftain.txt|T|TrigPayCost|X X
numot_the_devastator.txt|T|TrigDestroy|2 R
nurturer_initiate.txt|T|TrigPump|1
nyssa_of_traken.txt|T|TrigImmediateTrig|Sac<X/Artifact>
ogre_head_helm.txt|T|TrigDiscard|Sac<1/Card.TriggeredSource>
ohabi_caleria.txt|T|DBDraw|2
oko_lorwyn_liege_oko_shadowmoor_scion.txt|T|TrigTransform|U
old_man_willow.txt|T|TrigImmediate|Sac<1/Creature.Other;Card.token/another creature or token>
old_one_eye.txt|T|TrigReturn|Discard<2/Card>
olivia_mobilized_for_war.txt|T|TrigDiscard|Discard<1/Card>
oloro_ageless_ascetic.txt|T|TrigDraw|1
ominous_lockbox.txt|T|TrigCopy|Mandatory Sac<1/CARDNAME>
onyx_talisman.txt|T|TrigUntap|3
order_of_the_golden_cricket.txt|T|TrigPump|W
oreplate_pangolin.txt|T|TrigPutCounter|1
origin_of_thor.txt|K:Chapter|DBDraw|Discard<1/Card>
origin_spellbomb.txt|T|TrigDraw|W
oros_the_avenger.txt|T|TrigDamageAll|2 W
overclocked_electromancer.txt|T|TrigPutCounter|PayEnergy<3>
overseer_of_vault_76.txt|T|TrigImmediateTrig|RemoveAnyCounter<3/QUEST/Permanent/permanents>
pack_guardian.txt|T|TrigToken|Discard<1/Land>
panic_spellbomb.txt|T|TrigDraw|R
papercraft_decoy.txt|T|TrigDraw1|2
paramecia_coloniex.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>
party_thrasher.txt|T|TrigExile|Discard<1/Card>
passenger_ferry.txt|T|TrigImmediateTrig|U
pedantic_learning.txt|T|TrigDraw|1
persistent_marshstalker.txt|T|TrigChangeZone|2 B
pheres_band_raiders.txt|T|GFGToken|2 G
phoenix_chick.txt|T|TrigReturn|R R
phyrexian_dragon_engine.txt|T|TrigDraw|Discard<1/Hand>
pia_aether_ascetic.txt|T|TrigChange|Discard<1/Card>
pitchstone_wall.txt|T|TrigChange|Sac<1/CARDNAME>
plague_boiler.txt|T|TrigDestroyAll|Mandatory Sac<1/CARDNAME>
plundering_predator.txt|T|TrigDiscard|Discard<1/Card>
preponderant_pearl.txt|T|TrigConjure2|Mandatory Sac<1/CARDNAME/this artifact>
primal_adversary.txt|T|TrigPay|Mana<1 G\NumTimes>
proft_consulting_detective.txt|T|TrigPutCounter|2
projektor_inspector.txt|T|TrigDraw|Draw<1/You>
promise_of_aclazotz_foul_rebirth.txt|T|TrigPopulate|Sac<1/Creature.nonDemon/non-Demon creature>
promise_of_bunrei.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>
provisions_merchant.txt|T|TrigPumpAll|Sac<1/Food>
punishing_fire.txt|T|TrigChange|R
purestrain_genestealer.txt|T|TrigRamp|SubCounter<1/P1P1>
purgatory.txt|T|TrigReturn|4 PayLife<2>
purraj_of_urborg.txt|T|TrigPutCounter|B
pyre_zombie.txt|T|TrigReturn|1 B B
pyrewild_shaman.txt|T|TrigChange|3
quantum_entanglement.txt|T|TrigImmediateTrig|1 W
quicksmith_genius.txt|T|TrigDraw|Discard<1/Card>
quiet_contemplation.txt|T|TrigTap|1
raff_weatherlight_stalwart.txt|T|TrigDraw|tapXType<2/Creature>
ragefire_hellkite.txt|T|TrigPump|Sac<1/Creature.Other/another creature>
ragged_short_spear.txt|T|TrigDiscard|Discard<1/Card>
raiders_spoils.txt|T|TrigDraw|PayLife<1>
ral_and_the_implicit_maze.txt|K:Chapter|DBImpulseDraw|Discard<1/Card>
rank_officer.txt|T|DBToken|Discard<1/Card>
rankle_pitiless_trickster.txt|T|DBTrigger|PayLife<1>
rebellion_of_the_flamekin.txt|T|TrigTokenL|1
rebellion_of_the_flamekin.txt|T|TrigTokenW|1
reckless_racer.txt|T|TrigDiscard|Discard<1/Card>
redcap_gutter_dweller.txt|T|TrigPutCounter|Sac<1/Creature.Other/another creature>
redcap_raiders.txt|T|TrigPump|tapXType<1/Creature.nonHuman/non-Human creature>
redtooth_vanguard.txt|T|TrigReturn|2
reflective_golem.txt|T|TrigCopy|2
relentless_dead.txt|T|TrigChange|X
relentless_dead.txt|T|TrigReturn|B
replication_specialist.txt|T|TrigCopy|1 U
rescue_leopard.txt|T|TrigDraw|Discard<1/Card>
resonance_technician.txt|T|TrigInvestigate|Discard<1/Card>
restless_vents.txt|T|TrigLoot|Discard<1/Card>
rhovanion_rampager.txt|T|TrigPutCounter|Sac<1/Creature.Other/another creature>
richlau_headmaster.txt|T|TrigImmediateTrig|1
riddle_gate_gargoyle.txt|T|TrigImmediateTrig|PayEnergy<2>
riddlesmith.txt|T|TrigLoot|Draw<1/You>
riku_of_two_reflections.txt|T|TrigCopy|G U
riku_of_two_reflections.txt|T|TrigCopySpell|U R
rings_of_brighthearth.txt|T|TrigCopySpell|2
riparian_tiger.txt|T|TrigPump|PayEnergy<2>
riptide_entrancer.txt|T|TrigGainControl|Sac<1/CARDNAME>
rise_of_the_hobgoblins.txt|T|TrigToken|X
rith_the_awakener.txt|T|TrigChoose|2 G
rith_the_awakener_avatar.txt|T|TrigToken|5
riveting_rigger.txt|T|TrigPutCounter|Sac<1/Artifact.Other/another artifact>
roalesk_prime_specimen.txt|T|TrigRandom|X
roar_of_resistance.txt|T|TrigPumpAll|1 R
robobrain_war_mind.txt|T|TrigDraw|PayEnergy<3>
rodolf_duskbringer.txt|T|TrigImmediateTrig|1 WB
rohgahh_kher_keep_overlord.txt|T|TrigTokenDragon|2
rook_turret.txt|T|TrigLoot|Draw<1/You>
rooting_kavu.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>
rootwater_thief.txt|T|TrigChangeZone|2
rubble_rouser.txt|T|TrigDiscard|Discard<1/Card>
ruin_processor.txt|T|TrigHerd|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>
ruthless_lawbringer.txt|T|TrigSac|Sac<1/Creature.Other/another creature>
ruthless_sniper.txt|T|TrigPutCounter|1
ruthless_technomancer.txt|T|TrigToken|Sac<1/Creature.Other/another creature you control>
rydia_summoner_of_mist.txt|T|TrigDraw|Discard<1/Card>
safe_haven.txt|T|TrigReturn|Sac<1/CARDNAME>
sage_of_the_falls.txt|T|TrigLoot|Draw<1/You>
saheeli_radiant_creator.txt|T|TrigImmediateTrig|PayEnergy<3>
saheelis_lattice_mastercraft_raptor.txt|T|TrigDiscard|Discard<1/Card>
salvage_drone.txt|T|TrigLoot|Draw<1/You>
sample_collector.txt|T|TrigImmediateTrig|CollectEvidence<3>
sanctuary_warden.txt|T|TrigChange|RemoveAnyCounter<1/Any/Creature;Planeswalker>
sanctum_of_calm_waters.txt|T|TrigDraw|Draw<X/You>
sanctum_of_ugin.txt|T|TrigSearch|Sac<1/CARDNAME>
sandbender_scavengers.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>
sanguine_spy.txt|T|TrigDraw|PayLife<2>
sarkhan_dragon_ascendant.txt|T|TrigToken|Behold<1/Dragon>
satyr_nyx_smith.txt|T|GFGToken|2 R
sauron_the_dark_lord.txt|T|TrigDraw|Discard<1/Hand>
savai_thundermane.txt|T|TrigImmediateTrig|2
savra_queen_of_the_golgari.txt|T|TrigSacrifice|PayLife<2>
saw.txt|T|TrigDraw|Sac<1/Permanent.Other+NotDefinedTriggeredAttacker/permanent other than the triggered attacker or CARDNAME>
scarlet_spider_kaine.txt|T|TrigPutCounter|Discard<1/Card>
scrapper_champion.txt|T|TrigPutCounter|PayEnergy<2>
scrapwork_mutt.txt|T|TrigDraw|Discard<1/Card>
screeching_bat_stalking_vampire.txt|T|TrigTransform|2 B B
scuzzback_scrounger.txt|T|TrigToken|Blight<1>
searing_meditation.txt|T|TrigDamage|2
seers_sundial.txt|T|TrigDraw|2
selfcraft_mechan.txt|T|TrigSac|Sac<1/Artifact/artifact>
sentry_bot.txt|T|PutCounterAll|PayEnergy<3>
sephiroth_fabled_soldier_sephiroth_one_winged_angel.txt|T|TrigDraw1|Sac<1/Creature.Other/another creature>
sephiroth_fabled_soldier_sephiroth_one_winged_angel.txt|T|TrigDraw2|Sac<X/Creature.Other/another creature>
seraph_of_new_capenna_seraph_of_new_phyrexia.txt|T|TrigPump|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>
serene_steward.txt|T|TrigPutCounter|W
servant_of_the_stinger.txt|T|TrigSearch|Sac<1/CARDNAME>
seymour_flux.txt|T|TrigDraw|PayLife<1>
shadow_mysterious_assassin.txt|T|TrigDraw|Sac<1/Permanent.Other+nonLand/another nonland permanent>
shambling_cieth.txt|T|TrigReturn|B
shanna_purifying_blade.txt|T|TrigDraw|X
shessra_deaths_whisper.txt|T|TrigDraw|PayLife<2>
shipwreck_looter.txt|T|TrigDraw|Draw<1/You>
shire_shirriff.txt|T|TrigSac|Sac<1/Card.token/token>
shoal_kraken.txt|T|TrigDraw|Draw<1/You>
shrapnel_slinger.txt|T|TrigSac|Sac<1/Creature>
shriek_treblemaker.txt|T|DBImmediateTrig|Discard<1/Card>
shu_yun_the_silent_tempest.txt|T|TrigPump|RW RW
sigil_of_the_new_dawn.txt|T|TrigReturn|1 W
silvan_reveler.txt|T|TrigReturn|1 G U
skeleton_key.txt|T|TrigDraw|Draw<1/You>
skirk_drill_sergeant.txt|T|TrigDig|2 R
skyfisher_spider.txt|T|TrigSac|Sac<1/Creature.Other/another creature>
skyline_scout.txt|T|TrigPump|1 W
skyrider_patrol.txt|T|TrigPayCost|G U
skyswimmer_koi.txt|T|TrigLoot|Draw<1/You>
skywarp_skaab.txt|T|DBDraw|ExileFromGrave<2/Creature/creature card>
skywise_teachings.txt|T|TrigToken|1 U
slab_hammer.txt|T|TrigPump|Return<1/Land>
slinza_the_spiked_stampede.txt|T|TrigImmediateTrig|1 RG
sludge_strider.txt|T|TrigLoseLife|1
slumbering_walker.txt|T|TrigImmediateTrig|RemoveAnyCounter<1/Any/CARDNAME/this creature>
smellerbee_rebel_fighter.txt|T|TrigDraw|Discard<1/Hand>
smelted_chargebug.txt|T|TrigPump|PayEnergy<1>
smolder_initiate.txt|T|TrigLoseLife|1
smugglers_copter.txt|T|TrigLoot|Draw<1/You>
snaremaster_sprite.txt|T|TrigImmediateTrig|2
sokenzan_smelter.txt|T|TrigToken|1 Sac<1/Artifact/an artifact>
sorcerers_broom.txt|T|TrigCopy|3
soul_net.txt|T|TrigGainLife|1
sourbread_auntie.txt|T|TrigToken|Blight<2>
sparktongue_dragon.txt|T|TrigPayCost|2 R
spectral_adversary.txt|T|TrigPay|Mana<1 U\NumTimes>
speed_young_avenger.txt|T|TrigImmediateTrig|1
spellbook_vendor.txt|T|TrigImmediate|1
spider_gwen_free_spirit.txt|T|TrigDraw|Discard<1/Card>
spider_man_to_the_rescue.txt|T|TrigImmediateTrig|tapXType<1/Card.Self/CARDNAME>
spiked_ripsaw.txt|T|TrigPump|Sac<1/Forest>
spined_tyrranax.txt|T|TrigImmediateTrig|2 G
spirit_bonds.txt|T|TrigToken|W
spirit_cairn.txt|T|TrigToken|W
spit_flame.txt|T|TrigABChangeZone|R
springbloom_druid.txt|T|TrigRamp|Sac<1/Land>
squealing_devil.txt|T|TrigPump|X
squirrel_sanctuary.txt|T|TrigReturn|1
stadium_tidalmage.txt|T|TrigDiscard|Draw<1/You>
stalking_tiger_avatar.txt|T|TrigDraw|1
standstill.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>
starks_ingenuity.txt|T|TrigDraw|X
steamflogger_service_rep.txt|T|TrigAssemble|1
stockpiling_celebrant.txt|T|TrigScry|Return<1/Permanent.nonLand+Other/another nonland permanent>
strefan_maurer_progenitor.txt|T|TrigChangeZone|Sac<2/Blood.token/Blood token>
subway_train.txt|T|TrigChangeZone|G
summon_g_f_ifrit.txt|K:Chapter|DBDiscard|Discard<1/Card>
sun_droplet.txt|T|TrigGainLife|SubCounter<1/CHARGE>
sunstreak_phoenix.txt|T|TrigReturn|1 R
surge_mare.txt|T|TrigDraw|Draw<1/You>
surgespanner.txt|T|TrigBounce|1 U
surtland_flinger.txt|T|TrigImmediate|Sac<1/Creature.Other/another creature>
surveillance_monitor.txt|T|TrigEvidence|CollectEvidence<4>
sutina_speaker_of_the_tajuru.txt|T|TrigImmediateTrig|Return<1/Land>
swarm_culler.txt|T|TrigDraw|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>
sygg_wanderwine_wisdom_sygg_wanderbrine_shield.txt|T|TrigTransform|U
sylvan_library.txt|T|TrigDraw|Draw<2/You>
symmetry_matrix.txt|T|TrigDraw|1
t45_power_armor.txt|T|TrigUntap|PayEnergy<1>
tablet_of_epityr.txt|T|TrigGainLife|1
taeko_the_patient_avalanche.txt|T|TrigImmediateTrig|UB
tainted_adversary.txt|T|TrigPay|Mana<2 B\NumTimes>
tainted_observer.txt|T|TrigProliferate|2
taj_nar_swordsmith.txt|T|TrigChange|X
telekinetic_bonds.txt|T|TrigTapOrUntap|1 U
tenacious_dead.txt|T|TrigReturn|1 B
teneb_the_harvester.txt|T|TrigChange|2 B
tenured_tethermage.txt|T|TrigToken|Sac<1/Land>
terra_herald_of_hope.txt|T|TrigPayCost|2
terror_ballista.txt|T|TrigSac|Sac<1/Creature.Other/another creature>
terror_of_towashi.txt|T|TrigImmediateTrig|3 B
tester_of_the_tangential.txt|T|TrigImmediateTrig|X
tether_technician.txt|T|TrigImmediateTrig|Discard<1/Card>
tetravus.txt|T|TrigPutCounters|Exile<X/Creature.IsRemembered/Tetravite>
tetravus.txt|T|TrigToken|SubCounter<X/P1P1>
the_balrog_of_moria.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>
the_disciple_of_nissa.txt|T|TrigImmediateTrig|X X X G G
the_falcon_airship_restored.txt|T|TrigImmediateTrigger|Sac<1/CARDNAME>
the_fantasticar.txt|T|TrigToken|Sac<1/CARDNAME>
the_fire_nation_drill.txt|T|TrigImmediateTrigger|tapXType<1/Card.Self/CARDNAME>
the_fugitive_doctor.txt|T|TrigImmediateTrig|Sac<1/Clue>
the_gitrog_ravenous_ride.txt|T|TrigDraw|Sac<1/Creature.SaddledThisTurn/creature that saddled it this turn>
the_goose_mother.txt|T|TrigDraw|Sac<1/Food>
the_huntsmans_redemption.txt|K:Chapter|ABSearch|Sac<1/Creature>
the_kingpin_of_crime.txt|T|TrigEffect|PayLife<2>
the_meep.txt|T|TrigAnimateAll|Sac<1/Creature.Other/another creature>
the_motherlode_excavator.txt|T|TrigImmediateTrig|PayEnergy<4>
the_rise_of_sozin_fire_lord_sozin.txt|T|TrigImmediateTrig|X
the_sackville_bagginses.txt|T|TrigDraw|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>
the_thing.txt|T|TrigImmediateTrig|R G W U
thousand_moons_crackshot.txt|T|TrigImmediateTrig|2 W
thousand_moons_smithy_barracks_of_the_thousand.txt|T|TrigTransform|tapXType<5/Artifact;Creature/artifact or creature>
three_dog_galaxy_news_dj.txt|T|TrigImmediateTrig|2 Sac<1/Aura.Attached/Aura attached to CARDNAME>
thriving_grubs.txt|T|TrigPutCounter|PayEnergy<2>
thriving_ibex.txt|T|TrigPutCounter|PayEnergy<2>
thriving_rats.txt|T|TrigPutCounter|PayEnergy<2>
thriving_rhino.txt|T|TrigPutCounter|PayEnergy<2>
thriving_skyclaw.txt|T|TrigPutCounter|PayEnergy<3>
thriving_turtle.txt|T|TrigPutCounter|PayEnergy<2>
throne_of_bone.txt|T|TrigGainLife|1
throwing_knife.txt|T|TrigDamage|Sac<1/CARDNAME>
thunderblade_charge.txt|T|TrigPlay|2 R R R
tidal_terror.txt|T|TrigUnblockable|tapXType<2/Creature>
tiger_tribe_hunter.txt|T|TrigImmediate|Sac<1/Creature.Other/another creature>
tilonallis_summoner.txt|T|TrigToken|X R
timothar_baron_of_bats.txt|T|TrigToken|1 ExileAnyGrave<1/Card.TriggeredNewCard>
titan_of_littjara.txt|T|TrigDraw|Draw<X/You>
tivash_gloom_summoner.txt|T|TrigToken|PayLife<X>
tolarian_kraken.txt|T|TrigImmediateTrig|1
tomakul_phoenix.txt|T|TrigReturn|X R
toph_hardheaded_teacher.txt|T|TrigChangeZone|Discard<1/Card>
trail_of_crumbs.txt|T|TrigDig|1
tranquil_frillback.txt|T|TrigPay|Mana<G\NumTimes>
transplant_theorist.txt|T|TrigLoot|Draw<1/You>
treetop_sentries.txt|T|TrigDraw|Forage
treva_the_renewer.txt|T|TrigChoose|2 W
tribute_to_horobi_echo_of_deaths_wail.txt|T|TrigDraw|Sac<1/Creature.Other/another creature>
trudge_garden.txt|T|TrigToken|2
trystan_callous_cultivator_trystan_penitent_culler.txt|T|TrigTransform|G
ty_lee_artful_acrobat.txt|T|TrigImmediateTrig|1
tymna_the_weaver.txt|T|TrigDraw|PayLife<X>
ugins_binding.txt|T|TrigImmediateTrig|ExileFromGrave<1/CARDNAME>
ulalek_fused_atrocity.txt|T|TrigCopySpell|C C
ulamogs_despoiler.txt|T|TrigPutCounters|ExiledMoveToGrave<2/Card.OppOwn/card an opponent owns>
ulamogs_nullifier.txt|T|TrigProcess|ExiledMoveToGrave<2/Card.OppOwn/card an opponent owns>
ulamogs_reclaimer.txt|T|TrigChangeZone|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>
ultimecia_time_sorceress_ultimecia_omnipotent.txt|T|TrigTransform|4 U U B B ExileFromGrave<8/Card>
ultron_artificial_malevolence.txt|T|TrigCopy|2
ultron_unlimited.txt|T|TrigToken|1
unassuming_sage.txt|T|TrigToken|2
unconventional_tactics.txt|T|TrigChange|W
uncover_the_moon_letters.txt|T|TrigDiscard|Draw<X/You>
undead_butler.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>
undercity_eliminator.txt|T|TrigImmediateTrig|Sac<1/Artifact;Creature/artifact or creature>
undercity_scavenger.txt|T|TrigPutCounter|Sac<1/Creature.Other/another creature>
underhanded_designs.txt|T|TrigDrain|1
unscrupulous_contractor.txt|T|TrigSac|Sac<1/Creature>
urzas_chalice.txt|T|TrigGainLife|1
urzas_miter.txt|T|TrigDraw|3
urzas_sylex.txt|T|TrigSearch|2
valentin_dean_of_the_vein_lisette_dean_of_the_root.txt|T|TrigPutCounterAll|1
valkyries_sword.txt|T|TrigToken|4 W
vampire_gourmand.txt|T|TrigDraw|Sac<1/Creature.Other/another creature>
vanguard_of_the_restless.txt|T|TrigReturn|2 W
vanille_cheerful_lcie_ragnarok_divine_deliverance.txt|T|Meld|3 B G
vaultbreaker.txt|T|TrigDiscard|Discard<1/Card>
vaultguard_trooper.txt|T|TrigDraw|Discard<0/Hand>
veinwitch_coven.txt|T|TrigReturn|B
venomous_brutalizer.txt|T|TrigProliferate|1 G
venus_torn_between_worlds.txt|T|TrigDraw|U
verazol_the_split_current.txt|T|TrigCopy|SubCounter<2/P1P1>
veronica_dissident_scribe.txt|T|TrigDraw|Discard<1/Card>
verrak_warped_sengir.txt|T|TrigCopySpell|PayLife<X>
viashino_racketeer.txt|T|TrigDiscard|Discard<1/Card>
vigil_for_the_lost.txt|T|TrigGainLife|X
vile_redeemer.txt|T|TrigToken|C
vivis_persistence.txt|T|TrigChangeZone|2
vizkopa_confessor.txt|T|OppRevealX|PayLife<X>
volatile_wanderglyph.txt|T|TrigDraw|Discard<1/Card>
voltaic_brawler.txt|T|TrigPump|PayEnergy<1>
voltstorm_angel.txt|T|TrigImmediateTrig|PayEnergy<2>
voracious_tome_skimmer.txt|T|TrigDraw|PayLife<1>
vorosh_the_hunter.txt|T|TrigPutCounter|2 G
vraska_the_silencer.txt|T|TrigChangeZone|1
wandering_champion.txt|T|TrigDiscard|Discard<1/Card>
warcry_phoenix.txt|T|TrigReturn|2 R
warren_torchmaster.txt|T|TrigImmediate|Blight<1>
wasp_of_the_bitter_end.txt|T|TrigDestroy|Sac<1/CARDNAME>
wasteland_strangler.txt|T|TrigChangeZone|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>
weakstones_subjugation.txt|T|TrigTap|3
weapons_vendor.txt|T|TrigImmediateTrigger|1
wedding_security.txt|T|TrigPutCounter|Sac<1/Blood.token/Blood token>
west_wind_avatar.txt|T|TrigGainLife|Sac<1/Permanent.token;Land/token or a land>
wharf_infiltrator.txt|T|TrigDraw|Draw<1/You>
wharf_infiltrator.txt|T|TrigToken|2
which_of_you_burns_brightest.txt|T|DarkEffect|X
whiskerquill_scribe.txt|T|TrigDraw|Discard<1/Card>
whispering_specter.txt|T|TrigDiscard|Sac<1/CARDNAME>
wildborn_preserver.txt|T|TrigImmediateTrig|X
wilhelt_the_rotcleaver.txt|T|TrigDraw|Sac<1/Zombie>
windrider_wizard.txt|T|TrigLoot|Draw<1/You>
winter_tormented_loner.txt|T|TrigImmediateTrig|Sac<1/Creature;Planeswalker/creature or planeswalker>
wolf_of_devils_breach.txt|T|TrigDamage|1 R Discard<1/Card>
wolfbat.txt|T|TrigChangeZone|B
wooden_sphere.txt|T|TrigGainLife|1
wyll_pact_bound_duelist.txt|T|TrigEffect|Sac<1/Creature.Other;Artifact/another creature or an artifact>
wyll_pact_bound_duelist.txt|T|TrigImmediateTrig|Sac<1/Creature.Other;Artifact/another creature or an artifact>
wyll_pact_bound_duelist.txt|T|TrigUntap|Sac<1/Creature.Other;Artifact/another creature or an artifact>
yasova_dragonclaw.txt|T|TrigChange|1 UR UR
yotia_declares_war.txt|K:Chapter|TrigTap|Mandatory tapXType<X/Artifact>
young_necromancer.txt|T|TrigImmediateTrig|ExileFromGrave<2/card>
yuma_proud_protector.txt|T|TrigDraw|Sac<1/Land>
yuyan_archers.txt|T|TrigDraw|Discard<1/Card>
zhentarim_bandit.txt|T|TrigToken|PayLife<1>
ziatora_the_incinerator.txt|T|DBTrigger|Sac<1/Creature.StrictlyOther/another creature>
zoraline_cosmos_caller.txt|T|TrigImmediateTrig|W B PayLife<2>
`

var triggerCostField = regexp.MustCompile(`(?:^|\s|\|)Cost\$\s*([^|]+)`)
var triggerExecuteField = regexp.MustCompile(`(?:^|\s|\|)Execute\$\s*([^ |]+)`)
var triggerCostFamilies = []string{"Sac", "Discard", "Draw", "PayEnergy", "PayLife", "ExileFromGrave", "tapXType", "ExileAnyGrave", "SubCounter", "ExiledMoveToGrave", "Blight", "Return", "CollectEvidence", "Forage", "RemoveAnyCounter", "ExileFromHand", "PutCardToLibFromGrave", "Behold", "ExileCtrlOrGrave", "Mill", "Exile"}
var triggerFamilyHead = regexp.MustCompile(`^([A-Za-z]+)(?:<|$)`)

// TestTriggerBodyCostCorpusRatchet fails closed on any new, removed, or
// changed trigger/chapter carrier and names the card, root SVar, and raw cost.
// triggerCostRoute classifies each carrier by the route its current cost shape
// can take, independent of game-state availability. free-executor is the
// intentional Mandatory carve-out, distinct from a decline-only window.
func triggerCostRoute(row string) string {
	parts := strings.SplitN(row, "|", 4)
	if len(parts) != 4 {
		return "invalid"
	}
	raw := parts[3]
	mandatory := strings.HasPrefix(raw, "Mandatory ")
	if mandatory {
		if strings.Contains(raw, "tapXType<") {
			return "window-settled"
		}
		families := costFamiliesInRaw(strings.TrimPrefix(raw, "Mandatory "))
		settleable := map[string]bool{"Sac": true, "Exile": true, "ExiledMoveToGrave": true, "Mana": true, "PayLife": true}
		for family := range families {
			if !settleable[family] {
				return "free-executor"
			}
		}
		if families["Sac"] || families["Exile"] || families["ExiledMoveToGrave"] {
			return "window-settled"
		}
		return "free-executor"
	}
	families := costFamiliesInRaw(raw)
	settleable := map[string]bool{"Sac": true, "Discard": true, "Draw": true, "PayEnergy": true, "PayLife": true, "ExileFromGrave": true, "Exile": true, "ExileAnyGrave": true, "ExiledMoveToGrave": true, "Blight": true, "CollectEvidence": true, "Mana": true}
	for family := range families {
		if !settleable[family] {
			return "decline-only"
		}
	}
	return "window-settled"
}

func costFamiliesInRaw(raw string) map[string]bool {
	seen := map[string]bool{}
	for _, token := range strings.Fields(raw) {
		inAngle := strings.Contains(token, "<")
		if inAngle {
			token = token[:strings.IndexByte(token, '<')]
		}
		if !inAngle && token != "" && (token[0] >= '0' && token[0] <= '9' || strings.Trim(token, "WUBRGC") == "") || token == "Mana" {
			seen["Mana"] = true
		}
		for _, family := range triggerCostFamilies {
			if token == family {
				seen[family] = true
			}
		}
	}
	return seen
}

func TestTriggerBodyCostCorpusRatchet(t *testing.T) {
	root := filepath.Join("..", ".cards", "cardsfolder")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("corpus missing at %s: %v", root, err)
	}
	got := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".txt") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		var lines []string
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 4096), 4*1024*1024)
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if err = sc.Err(); err != nil {
			return err
		}
		svars := map[string]string{}
		type ref struct{ kind, root string }
		var refs []ref
		for _, line := range lines {
			if strings.HasPrefix(line, "SVar:") {
				p := strings.SplitN(line, ":", 3)
				if len(p) == 3 {
					svars[p[1]] = p[2]
				}
			}
			if strings.HasPrefix(line, "T:") {
				if m := triggerExecuteField.FindStringSubmatch(line); len(m) > 1 {
					refs = append(refs, ref{"T", m[1]})
				}
			}
			if strings.HasPrefix(line, "K:Chapter:") {
				p := strings.SplitN(line, ":", 4)
				if len(p) == 4 {
					for _, r := range strings.Split(p[3], ",") {
						refs = append(refs, ref{"K:Chapter", strings.TrimSpace(r)})
					}
				}
			}
		}
		for _, r := range refs {
			if m := triggerCostField.FindStringSubmatch(svars[r.root]); len(m) > 1 {
				raw := strings.TrimSpace(m[1])
				got[filepath.Base(path)+"|"+r.kind+"|"+r.root+"|"+raw] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(triggerBodyCostInventory), "\n") {
		if line != "" {
			want[line] = true
		}
	}
	var added, removed []string
	for k := range got {
		if !want[k] {
			added = append(added, k)
		}
	}
	for k := range want {
		if !got[k] {
			removed = append(removed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("trigger Cost$ inventory drift; unexpected card | record | root SVar | raw cost: %v; missing: %v", added, removed)
	}
	if len(got) != 855 {
		t.Fatalf("unique trigger Cost$ carriers=%d, want 855", len(got))
	}
	families := map[string]int{}
	routes := map[string]int{}
	for row := range got {
		raw := strings.SplitN(row, "|", 4)[3]
		seen := costFamiliesInRaw(raw)
		route := triggerCostRoute(row)
		if route == "invalid" {
			t.Fatalf("unclassified trigger cost carrier %s", row)
		}
		routes[route]++
		for fam := range seen {
			families[fam]++
		}
	}
	wantFamilies := map[string]int{"Mana": 358, "Sac": 163, "Discard": 97, "Draw": 38, "PayEnergy": 37, "PayLife": 36, "ExileFromGrave": 22, "tapXType": 17, "ExileAnyGrave": 16, "SubCounter": 15, "ExiledMoveToGrave": 9, "Blight": 7, "Return": 7, "CollectEvidence": 6, "Forage": 5, "RemoveAnyCounter": 4, "Exile": 2, "ExileFromHand": 2, "PutCardToLibFromGrave": 2, "Behold": 1, "ExileCtrlOrGrave": 1, "Mill": 1}
	var familyDrift []string
	for family, want := range wantFamilies {
		if got := families[family]; got != want {
			familyDrift = append(familyDrift, fmt.Sprintf("%s=%d (want %d)", family, got, want))
		}
	}
	for family, got := range families {
		if _, ok := wantFamilies[family]; !ok {
			familyDrift = append(familyDrift, fmt.Sprintf("%s=%d (unexpected)", family, got))
		}
	}
	sort.Strings(familyDrift)
	if len(familyDrift) > 0 {
		t.Fatalf("Cost$ family occurrence drift: %s", strings.Join(familyDrift, ", "))
	}
	wantRoutes := map[string]int{"window-settled": 801, "decline-only": 54}
	var routeDrift []string
	for route, want := range wantRoutes {
		if got := routes[route]; got != want {
			routeDrift = append(routeDrift, fmt.Sprintf("%s=%d (want %d)", route, got, want))
		}
	}
	for route, got := range routes {
		if _, ok := wantRoutes[route]; !ok {
			routeDrift = append(routeDrift, fmt.Sprintf("%s=%d (unexpected)", route, got))
		}
	}
	sort.Strings(routeDrift)
	if len(routeDrift) != 0 {
		t.Fatalf("trigger Cost$ route classification drift: %s", strings.Join(routeDrift, ", "))
	}
	if len(got) != 855 {
		t.Fatalf("Cost$ carrier aggregate changed during family census: %d", len(got))
	}
}
