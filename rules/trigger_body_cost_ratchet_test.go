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
a-ancestral_katana.txt|T|TrigImmediateTrig|1|window-settled
a-ardent_dustspeaker.txt|T|ABImpulse|PutCardToLibFromGrave<1/-1/Sorcery;Instant>|decline-only
a-civil_servant.txt|T|TrigPump|tapXType<1/Citizen.Other>|window-settled
a-dokuchi_silencer.txt|T|TrigImmediateTrig|Discard<1/Card>|window-settled
a-elven_bow.txt|T|TrigToken|1|window-settled
a-guide_of_souls.txt|T|TrigImmediateTrig|PayEnergy<4>|window-settled
a-leyline_of_resonance.txt|T|TrigCopy|1|window-settled
a-shessra_deaths_whisper.txt|T|TrigDraw|PayLife<2>|window-settled
a_golden_opportunity.txt|K:Chapter|DBConjureEgg|tapXType<1/Bird> Sac<1/Artifact>|decline-only
abomination_of_gudul.txt|T|TrigLoot|Draw<1/You>|window-settled
academy_raider.txt|T|TrigDiscard|Discard<1/Card>|window-settled
academy_rector.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
academy_wall.txt|T|TrigLoot|Draw<1/You>|window-settled
ace_fearless_rebel.txt|T|TrigImmediateTrig|Sac<1/Artifact>|window-settled
adrianas_valor.txt|T|TrigPump|W|window-settled
aegis_sculptor.txt|T|TrigPutCounter|ExileFromGrave<2/Card>|window-settled
aerie_worshippers.txt|T|TrigToken|2 U|window-settled
aether_chaser.txt|T|TrigToken|PayEnergy<2>|window-settled
aether_herder.txt|T|TrigToken|PayEnergy<2>|window-settled
aether_inspector.txt|T|TrigToken|PayEnergy<2>|window-settled
aether_poisoner.txt|T|TrigToken|PayEnergy<2>|window-settled
aether_swooper.txt|T|TrigToken|PayEnergy<2>|window-settled
aetherplasm.txt|T|TrigBounce|Return<1/CARDNAME>|decline-only
aetherstorm_roc.txt|T|TrigPutCounter|PayEnergy<2>|window-settled
aetherstream_leopard.txt|T|TrigPump|PayEnergy<1>|window-settled
agrus_kos_eternal_soldier.txt|T|TrigCopy|1 RW|window-settled
ajanis_last_stand.txt|T|TrigDiesToken|Sac<1/CARDNAME>|window-settled
akki_ronin.txt|T|TrigDraw|Discard<1/Card>|window-settled
akoum_firebird.txt|T|TrigChange|4 R R|window-settled
akoum_stonewaker.txt|T|TrigToken|2 R|window-settled
akuta_born_of_ash.txt|T|TrigReturn|Sac<1/Swamp>|window-settled
alesha_who_smiles_at_death.txt|T|TrigChange|WB WB|window-settled
alistair_the_brigadier.txt|T|TrigPumpAll|8|window-settled
altar_of_the_wretched_wretched_bonemass.txt|T|TrigDraw|Sac<1/Creature.!token/nontoken creature>|window-settled
amber_gristle_omaul.txt|T|TrigDraw|Discard<1/Hand>|window-settled
ambergris_citadel_agent.txt|T|TrigDamage|Discard<1/Hand> Draw<2/You>|window-settled
ambulatory_edifice.txt|T|DBTrigger|PayLife<2>|window-settled
ancestral_katana.txt|T|TrigImmediateTrig|1|window-settled
angelic_renewal.txt|T|TrigReturn|Sac<1/CARDNAME>|window-settled
animation_module.txt|T|TrigToken|1|window-settled
anointer_of_valor.txt|T|TrigImmediateTrig|3|window-settled
ant_man_colony_commander.txt|T|TrigImmediateTrig|1|window-settled
aphelia_viper_whisperer.txt|T|TrigToken|1 BG|window-settled
aphemia_the_cacophony.txt|T|DBToken|ExileFromGrave<1/Enchantment>|window-settled
apothecary_initiate.txt|T|TrigGainLife|1|window-settled
arahbo_roar_of_the_world.txt|T|TrigPump2|1 G W|window-settled
ardent_dustspeaker.txt|T|ABImpulse|PutCardToLibFromGrave<1/-1/Sorcery;Instant>|decline-only
arena_rector.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
arni_metalbrow.txt|T|TrigChangeZone|1 R|window-settled
arni_metalbrow.txt|T|TrigChangeZoneBis|1 R|window-settled
arrogant_poet.txt|T|TrigPump|PayLife<2>|window-settled
artillery_enthusiast.txt|T|TrigSeek|Discard<1/Card>|window-settled
artists_talent.txt|T|TrigDiscard|Discard<1/Card>|window-settled
asgardian_inspiration.txt|T|TrigChangeZone|2|window-settled
ashcoat_of_the_shadow_swarm.txt|T|TrigChange|Mill<4>|decline-only
ashling_rekindled_ashling_rimebound.txt|T|TrigDraw|Discard<1/Card>|window-settled
ashling_rekindled_ashling_rimebound.txt|T|TrigTransform|R|window-settled
ashnod_flesh_mechanist.txt|T|TrigToken|Sac<1/Creature.Other/another creature>|window-settled
aspiring_champion.txt|T|TrigDig|Mandatory Sac<1/CARDNAME>|window-settled
assaultron_dominator.txt|T|TrigPutCounter|PayEnergy<1>|window-settled
astonishing_spider_man.txt|T|TrigDraw|Discard<1/Hand>|window-settled
atraxas_skitterfang.txt|T|DBTrigger|SubCounter<1/OIL>|decline-only
aurora_shifter.txt|T|TrigImmediateTrig|PayEnergy<2>|window-settled
auspicious_ancestor.txt|T|TrigGainLife|1|window-settled
automated_warfare_system.txt|T|TrigDraw|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>|window-settled
awaken_the_sky_tyrant.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>|window-settled
aziza_mage_tower_captain.txt|T|TrigCopy|tapXType<3/Creature>|window-settled
azor_the_lawbringer.txt|T|TrigDraw|X W U U|window-settled
azorius_aethermage.txt|T|TrigDraw|1|window-settled
azra_oddsmaker.txt|T|ChooseCreature|Discard<1/Card>|window-settled
balthier_and_fran.txt|T|TrigAddCombat|1 R G|window-settled
banon_the_returners_leader.txt|T|TrigDraw|1 Discard<1/Card>|window-settled
baral_chief_of_compliance.txt|T|TrigLoot|Draw<1/You>|window-settled
battlefield_scavenger.txt|T|TrigDiscard|Discard<1/Card>|window-settled
battlemages_bracers.txt|T|TrigCopyAbility|1|window-settled
bearer_of_silence.txt|T|TrigSacrifice|1 C|window-settled
bebop_skull_crossbones.txt|T|TrigLoseLife|Draw<X/You>|window-settled
beetle_headed_merchants.txt|T|TrigDraw|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>|window-settled
benalish_partisan.txt|T|TrigReturn|1 W|window-settled
benthic_criminologists.txt|T|TrigDraw|Sac<1/Artifact>|window-settled
biblioplex_kraken.txt|T|TrigUnblockable|Return<1/Creature.Other>|decline-only
big_wheel.txt|T|TrigDiscard|Discard<1/Card>|window-settled
biting_palm_ninja.txt|T|TrigImmediateTrig|SubCounter<1/Menace>|decline-only
bitter_chill.txt|T|TrigDraw|1|window-settled
bitter_reunion.txt|T|TrigDiscard|Discard<1/Card>|window-settled
blight_herder.txt|T|TrigToken|ExiledMoveToGrave<2/Card.OppOwn/cards your opponents own>|window-settled
blighted_blackthorn.txt|T|TrigDraw|Blight<2>|window-settled
blind_zealot.txt|T|TrigDestroy|Sac<1/CARDNAME>|window-settled
blood_operative.txt|T|TrigReturn|PayLife<3>|window-settled
blood_speaker.txt|T|TrigSearch|Sac<1/CARDNAME>|window-settled
bloodcrazed_socialite.txt|T|TrigPump|Sac<1/Blood.token/Blood token>|window-settled
bloodfeather_phoenix.txt|T|TrigReturn|R|window-settled
bloodmist_infiltrator.txt|T|TrigUnblockable|Sac<1/Creature.Other/another creature>|window-settled
bloodthirsty_adversary.txt|T|TrigPay|Mana<2 R\NumTimes>|window-settled
bog_strider_ash.txt|T|TrigGainLife|G|window-settled
boggart_mischief.txt|T|TrigToken|Blight<1>|window-settled
boilerbilges_ripper.txt|T|TrigSac|Sac<1/Creature.Other;Enchantment.Other/another creature or enchantment>|window-settled
bolg_of_the_north.txt|T|TrigSac|Sac<1/Creature.Other/another creature>|window-settled
boneyard_scourge.txt|T|TrigReturn|1 B|window-settled
booby_trap.txt|T|TrapTriggered|Mandatory Sac<1/CARDNAME>|window-settled
book_devourer.txt|T|TrigDiscard|Discard<1/Hand>|window-settled
boundary_lands_ranger.txt|T|TrigDraw|Discard<1/Card>|window-settled
braidss_frightful_return.txt|K:Chapter|ABDiscard|Sac<1/Creature>|window-settled
bramble_sovereign.txt|T|TrigCopy|1 G|window-settled
brass_gnat.txt|T|TrigUntap|1|window-settled
brass_man.txt|T|TrigUntap|1|window-settled
brawl_bash_ogre.txt|T|TrigPump|Sac<1/Creature.Other/another creature>|window-settled
breeches_the_blastmaker.txt|T|TrigFlip|Sac<1/Artifact>|window-settled
brigid_clachans_heart_brigid_douns_mind.txt|T|TrigTransform|W|window-settled
brilliant_wings.txt|T|TrigAttach|1|window-settled
bringer_of_the_black_dawn.txt|T|TrigChange|PayLife<2>|window-settled
bristlebud_farmer.txt|T|TrigMill|Sac<1/Food>|window-settled
brood_astronomer.txt|T|TrigDraft|Sac<1/Land>|window-settled
burning_tree_vandal.txt|T|TrigDraw|Discard<1/Card>|window-settled
bushy_bodyguard.txt|T|TrigPutCounter|Forage|decline-only
buzzard_wasp_colony.txt|T|TrigDraw|Sac<1/Artifact;Creature/artifact or creature>|window-settled
byway_barterer.txt|T|TrigDraw|Discard<1/Hand>|window-settled
cabal_therapist.txt|T|DBImmediateTrigger|Sac<1/Creature>|window-settled
cacophony_scamp.txt|T|TrigProliferate|Sac<1/CARDNAME>|window-settled
cadric_soul_kindler.txt|T|TrigCopy|1|window-settled
caesar_legions_emperor.txt|T|TrigImmediateTrig|Sac<1/Creature.Other/another creature>|window-settled
call_of_the_ring.txt|T|TrigDraw|PayLife<2>|window-settled
campsite_cuisine.txt|T|TrigImmediateTrig|Sac<X/Food>|window-settled
canoptek_wraith.txt|T|TrigSearch|3 Sac<1/CARDNAME>|window-settled
caparocti_sunborn.txt|T|TrigDiscover|tapXType<2/Artifact;Creature/artifacts and/or creatures>|window-settled
carefree_swinemaster.txt|T|TrigToken|1 G|window-settled
carrion_thrash.txt|T|TrigChange|2|window-settled
cavalier_of_night.txt|T|TrigSac|Sac<1/Creature.Other/another creature>|window-settled
cavalier_of_thorns.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
celestine_cave_witch.txt|T|TrigCurse|Sac<1/Insect>|window-settled
cemetery_puca.txt|T|CemeteryPucaCopy|1|window-settled
centaur_vinecrasher.txt|T|TrigReturn|G G|window-settled
champion_of_wits.txt|T|TrigDraw|Draw<X/You>|window-settled
chandras_regulator.txt|T|TrigCopyAbility|1|window-settled
chipper_chopper.txt|T|TrigPutCounter|Sac<1/Artifact.Other/another artifact>|window-settled
chitterspitter.txt|T|TrigPutCounter|Sac<1/Permanent.token/token>|window-settled
circle_of_affliction.txt|T|TrigDrain|1|window-settled
civil_servant.txt|T|TrigPump|tapXType<1/Citizen.Other>|window-settled
cloudpiercer.txt|T|TrigDiscard|Discard<1/Card>|window-settled
cloven_casting.txt|T|TrigCopy|1|window-settled
cogwork_progenitor.txt|T|TrigSeek|ExileCtrlOrGrave<1/Artifact.Other>|decline-only
colfenors_urn.txt|T|TrigReturnAll|Mandatory Sac<1/CARDNAME>|window-settled
comet_crawler.txt|T|TrigPump|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>|window-settled
common_iguana.txt|T|TrigDiscard|Discard<1/Card>|window-settled
conduit_goblin.txt|T|TrigPump|PayEnergy<1>|window-settled
conspiracy_theorist.txt|T|TrigDraw|1 Discard<1/Card>|window-settled
conspiracy_theorist.txt|T|TrigEffect|ExileFromGrave<1/Card.TriggeredCards>|window-settled
consuls_shieldguard.txt|T|TrigPump|PayEnergy<1>|window-settled
cool_but_rude.txt|T|TrigDraw|Discard<1/Card>|window-settled
copy_catchers.txt|T|TrigCopy|1 U|window-settled
cornered_crook.txt|T|TrigImmediateTrig|Sac<1/Artifact>|window-settled
corpseberry_cultivator.txt|T|TrigPump|Forage|decline-only
corpses_of_the_lost.txt|T|TrigReturn|PayLife<1>|window-settled
councils_deliberation.txt|T|DBDraw|ExileFromGrave<1/CARDNAME>|window-settled
cradle_of_vitality.txt|T|TrigPutCounter|1 W|window-settled
creeping_chill.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredCard>|window-settled
crosis_the_purger.txt|T|TrigChoose|2 B|window-settled
crossway_troublemakers.txt|T|TrigDraw|PayLife<2>|window-settled
crucias_titan_of_the_waves.txt|T|TrigToken|Discard<1/Card>|window-settled
crystal_rod.txt|T|TrigGainLife|1|window-settled
curious_forager.txt|T|TrigImmediateTrig|Forage|decline-only
curse_of_silence.txt|T|TrigDraw|Sac<1/CARDNAME>|window-settled
customs_depot.txt|T|TrigLoot|1|window-settled
cyclops_superconductor.txt|T|DBTrigger|PayEnergy<3>|window-settled
dalek_intensive_care.txt|T|TrigExile|Mandatory Exile<1/Creature.nonDalek/non-Dalek creature>|window-settled
dance_of_the_dead.txt|T|TrigUntap|1 B|window-settled
daretti_rocketeer_engineer.txt|T|TrigReturn|Sac<1/Artifact>|window-settled
darigaaz_the_igniter.txt|T|TrigChooseColor|2 R|window-settled
daring_saboteur.txt|T|TrigLoot|Draw<1/You>|window-settled
dark_depths.txt|T|TrigToken|Mandatory Sac<1/CARDNAME>|window-settled
dauthi_mindripper.txt|T|TrigDiscard|Sac<1/CARDNAME>|window-settled
dawn_of_hope.txt|T|TrigDraw|2|window-settled
deadly_designs.txt|T|TrigDestroy|Mandatory Sac<1/CARDNAME>|window-settled
death_priest_of_myrkul.txt|T|TrigToken|1|window-settled
death_spark.txt|T|TrigReturn|1|window-settled
decree_of_justice.txt|T|TrigToken|X|window-settled
depala_pilot_exemplar.txt|T|TrigDig|X|window-settled
descendant_of_storms.txt|T|TrigEndure|1 W|window-settled
descendants_fury.txt|T|TrigDigUntil|Sac<1/Card.TriggeredSources>|window-settled
digsite_conservator.txt|T|TrigDiscover|4|window-settled
digsite_engineer.txt|T|TrigToken|2|window-settled
dire_fleet_warmonger.txt|T|TrigPump|Sac<1/Creature.Other/another creature>|window-settled
discerning_peddler.txt|T|TrigDiscard|Discard<1/Card>|window-settled
disciple_of_deceit.txt|T|TrigSearch|Discard<1/Card.nonLand/nonland card>|window-settled
disciple_of_freyalise_garden_of_freyalise.txt|T|TrigGainLife|Sac<1/Creature.Other/another creature>|window-settled
disturbing_mirth.txt|T|TrigDraw|Sac<1/Enchantment.Other;Creature.Other/another enchantment or creature>|window-settled
dokuchi_silencer.txt|T|TrigImmediateTrig|Discard<1/Creature>|window-settled
doombot_harbinger.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
doric_natures_warden_doric_owlbear_avenger.txt|T|TrigTransform|1 G|window-settled
drainpipe_vermin.txt|T|TrigDiscard|B|window-settled
drake_haven.txt|T|TrigToken|1|window-settled
draugrs_helm.txt|T|TrigToken|2 B|window-settled
dream_seizer.txt|T|TrigDiscard|Blight<1>|window-settled
dreamcatcher.txt|T|TrigDraw|Sac<1/CARDNAME>|window-settled
dreamshaper_shaman.txt|T|TrigDig|2 R Sac<1/Permanent.nonLand/nonland permanent>|window-settled
dromar_the_banisher.txt|T|TrigChoose|2 U|window-settled
drowner_initiate.txt|T|TrigMill|1|window-settled
duelist_of_the_mind.txt|T|TrigLoot|Draw<1/You>|window-settled
durable_handicraft.txt|T|TrigPutCounter|1|window-settled
dutiful_replicator.txt|T|TrigImmediate|1|window-settled
dwarven_hammer.txt|T|TrigToken|2|window-settled
ecstatic_electromancer.txt|T|TrigDraw|Discard<1/Card>|window-settled
eddie_brock_venom_lethal_protector.txt|T|TrigDraw|Sac<1/Creature.Other/another creature>|window-settled
eddytrail_hawk.txt|T|TrigPump|PayEnergy<1>|window-settled
edgars_awakening.txt|T|TrigImmediateTrig|B|window-settled
eirdu_carrier_of_dawn_isilu_carrier_of_twilight.txt|T|TrigTransform|W|window-settled
elaborate_firecannon.txt|T|TrigUntap|Discard<1/Card>|window-settled
eldrazi_obligator.txt|T|TrigChange|1 C|window-settled
electro_assaulting_battery.txt|T|TrigImmediateTrig|X|window-settled
electropotence.txt|T|TrigDamage|2 R|window-settled
elenda_and_azor.txt|T|TrigDraw|X W U B|window-settled
elenda_and_azor.txt|T|TrigToken|PayLife<4>|window-settled
elusive_tormentor_insidious_mist.txt|T|TrigTransform|2 B|window-settled
elven_bow.txt|T|TrigToken|2|window-settled
embereth_skyblazer.txt|T|TrigPumpAll|2 R|window-settled
embersmith.txt|T|TrigDamage|1|window-settled
emeritus_of_ideation_ancestrall_recall.txt|T|TrigPrepare|ExileFromGrave<8/Card>|window-settled
emiel_the_blessed.txt|T|TrigPutCounter|GW|window-settled
emperor_mihail_ii.txt|T|TrigToken|1|window-settled
endless_ranks_of_hydra.txt|T|TrigReturn|1 B|window-settled
enigmatic_incarnation.txt|T|TrigSearch|Sac<1/Enchantment.Other/another enchantment>|window-settled
equilibrium.txt|T|TrigBounce|1|window-settled
era_of_innovation.txt|T|TrigEnergy|1|window-settled
erebos_bleak_hearted.txt|T|ABDraw|PayLife<2>|window-settled
ereboss_titan.txt|T|TrigReturn|Discard<1/Card>|window-settled
escape_protocol.txt|T|TrigImmediateTrig|1|window-settled
esoteric_duplicator.txt|T|TrigDelayedTrig|2|window-settled
estwald_shieldbasher.txt|T|TrigPump|1|window-settled
eternal_taskmaster.txt|T|TrigChange|2 B|window-settled
euru_acorn_scrounger.txt|T|TrigImmediateTrig|Forage|decline-only
euru_acorn_scrounger.txt|T|TrigPutCounterAll|Sac<1/Permanent.token/token>|window-settled
evereth_viceroy_of_plunder.txt|T|TrigImmediateTrig|1 BR|window-settled
every_last_vestige_shall_rot.txt|T|MoveToBottom|X|window-settled
evidence_examiner.txt|T|TrigEvidence|CollectEvidence<4>|window-settled
excavating_anurid.txt|T|TrigDraw|Sac<1/Land>|window-settled
extricator_of_sin_extricator_of_flesh.txt|T|TrigToken|Sac<1/Permanent.Other/another permanent>|window-settled
eye_of_vecna.txt|T|TrigDrawUpkeep|2|window-settled
eyes_of_the_watcher.txt|T|TrigScry|1|window-settled
ezuri_stalker_of_spheres.txt|T|TrigProliferate|3|window-settled
faith_of_the_devoted.txt|T|TrigDrain|1|window-settled
fathom_fleet_captain.txt|T|TrigToken|2|window-settled
feed_the_pack.txt|T|TrigToken|Sac<1/Creature.!token/nontoken creature>|window-settled
felhide_spiritbinder.txt|T|TrigCopy|1 R|window-settled
felothar_dawn_of_the_abzan.txt|T|TrigImmediateTrig|Sac<1/Permanent.nonLand/nonland permanent>|window-settled
fetid_gargantua.txt|T|TrigLoseLife|Draw<2/You>|window-settled
filigree_racer.txt|T|TrigTrigger|PayEnergy<2>|window-settled
fire_lord_ozai.txt|T|TrigMana|Sac<1/Creature.Other/another creature>|window-settled
fissure_wizard.txt|T|TrigDiscard|Discard<1/Card>|window-settled
flame_kin_war_scout.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>|window-settled
flameblast_dragon.txt|T|TrigDamage|X R|window-settled
flameshadow_conjuring.txt|T|TrigCopy|R|window-settled
flamespeakers_will.txt|T|TrigDestroy|Sac<1/CARDNAME>|window-settled
flamewake_phoenix.txt|T|TrigReturn|R|window-settled
flaring_cinder.txt|T|TrigDraw|Discard<1/Card>|window-settled
flaxen_intruder_welcome_home.txt|T|TrigTrig|Sac<1/CARDNAME>|window-settled
flight_spellbomb.txt|T|TrigDraw|U|window-settled
foot_chopper.txt|T|TrigDraw|Sac<1/Card.TriggeredSource/that creature>|window-settled
foreboding_steamboat.txt|T|TrigInvestigate|ExiledMoveToGrave<1/Card.ExiledWithSource/card exiled with CARDNAME>|window-settled
forgehammer_centurion.txt|T|TrigImmediateTrig|SubCounter<2/OIL>|decline-only
forgotten_creation.txt|T|TrigDiscard|Discard<1/Hand>|window-settled
forgotten_harvest.txt|T|TrigPutCounter|ExileFromGrave<1/Land>|window-settled
forlorn_pseudamma.txt|T|GFGToken|2 B|window-settled
formidable_speaker.txt|T|TrigSearch|Discard<1/Card>|window-settled
forsaken_city.txt|T|TrigUntap|ExileFromHand<1/Card>|decline-only
forsaken_miner.txt|T|TrigChange|B|window-settled
foster.txt|T|TrigDig|1|window-settled
founding_of_omashu.txt|K:Chapter|DBDraw|Discard<1/Card>|window-settled
frenzied_geistblaster.txt|T|TrigSeek|Discard<1/Card>|window-settled
frenzied_goblin.txt|T|TrigPump|R|window-settled
freyalises_charm.txt|T|TrigDraw|G G|window-settled
furious_forebear.txt|T|TrigChangeZone|1 W|window-settled
furnace_celebration.txt|T|TrigDealDamage|2|window-settled
furnace_scamp.txt|T|TrigDamage|Sac<1/CARDNAME>|window-settled
furyblade_vampire.txt|T|TrigPump|Discard<1/Card>|window-settled
gamekeeper.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
gastal_blockbuster.txt|T|TrigSac|Sac<1/Creature;Vehicle/a creature or Vehicle>|window-settled
general_traag_heart_of_stone.txt|T|TrigImmediateTrig|Sac<1/Artifact.Other/another artifact>|window-settled
genesis.txt|T|TrigChange|2 G|window-settled
gert_and_old_lace_runaways.txt|T|TrigSearch|Discard<1/Card>|window-settled
ghastly_remains.txt|T|TrigReturn|B B B|window-settled
ghitu_embercoiler.txt|T|TrigSeek|Discard<1/Card>|window-settled
ghostly_pilferer.txt|T|TrigDraw1|2|window-settled
giant_albatross.txt|T|TrigEach|1 U|window-settled
giants_amulet.txt|T|TrigToken|3 U|window-settled
gigapede.txt|T|TrigChange|Discard<1/Card>|window-settled
gilded_ambusher.txt|T|TrigImmediateTrig|Sac<1/Permanent.Other+nonLand/another nonland permanent>|window-settled
giott_king_of_the_dwarves.txt|T|TrigDraw|Discard<1/Card>|window-settled
gitaxian_anatomist.txt|T|TrigProliferate|tapXType<1/Card.Self/CARDNAME>|window-settled
glint_sleeve_siphoner.txt|T|TrigDraw|PayEnergy<2>|window-settled
glorifier_of_suffering.txt|T|TrigSac|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>|window-settled
go_shintai_of_ancient_wars.txt|T|TrigImmediateTrig|1|window-settled
go_shintai_of_boundless_vigor.txt|T|TrigImmediateTrig|1|window-settled
go_shintai_of_hidden_cruelty.txt|T|TrigImmediateTrig|1|window-settled
go_shintai_of_lost_wisdom.txt|T|TrigImmediateTrig|1|window-settled
go_shintai_of_shared_purpose.txt|T|TrigToken|1|window-settled
goblin_dirigible.txt|T|TrigUntap|4|window-settled
goblin_grenadiers.txt|T|TrigDestroyCreature|Sac<1/CARDNAME>|window-settled
goblin_vandal.txt|T|TrigDestroy|R|window-settled
goblin_war_wagon.txt|T|TrigUntap|2|window-settled
goblinslide.txt|T|TrigToken|1|window-settled
god_favored_general.txt|T|GFGToken|2 W|window-settled
gorbag_of_minas_morgul.txt|T|TrigImmediateTrig|Sac<1/Card.TriggeredSource/that creature>|window-settled
graha_tia_scion_reborn.txt|T|TrigToken|PayLife<X>|window-settled
grave_peril.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>|window-settled
gravelgill_scoundrel.txt|T|TrigUnblockable|tapXType<1/Creature.Other>|window-settled
gravity_negator.txt|T|TrigPump|C|window-settled
great_desert_hellion.txt|T|TrigDraw|Discard<1/Hand>|window-settled
green_goblin_back_for_more.txt|T|TrigDiscard|Discard<1/Card>|window-settled
greenwarden_of_murasa.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
greven_predator_captain.txt|T|TrigDraw|Sac<1/Creature.Other/another creature>|window-settled
grim_reaper_lethal_legionnaire.txt|T|TrigTrigger|3 B|window-settled
grist_voracious_larva_grist_the_plague_swarm.txt|T|TrigTransform|G|window-settled
grovetender_druids.txt|T|TrigToken|1|window-settled
grub_storied_matriarch_grub_notorious_auntie.txt|T|TrigCopy|Blight<1>|window-settled
grub_storied_matriarch_grub_notorious_auntie.txt|T|TrigTransform|B|window-settled
gryffwing_cavalry.txt|T|TrigPump|1 W|window-settled
guide_of_souls.txt|T|TrigImmediateTrig|PayEnergy<3>|window-settled
guiding_hydra.txt|T|TrigPump|SubCounter<1/P1P1>|decline-only
gut_true_soul_zealot.txt|T|TrigToken|Sac<1/Creature.Other;Artifact/another creature or an artifact>|window-settled
haazda_snare_squad.txt|T|TrigTap|W|window-settled
halana_kessig_ranger.txt|T|TrigPayCost|2|window-settled
halo_forager.txt|T|TrigImmediateTrig|X|window-settled
hangar_scrounger.txt|T|TrigDraw|Discard<1/Card>|window-settled
harvester_troll.txt|T|TrigPutCounter|Sac<1/Creature;Land/creature or land>|window-settled
hashaton_scarabs_fist.txt|T|TrigCopy|2 U|window-settled
haunted_cadaver.txt|T|TrigDiscard|Sac<1/CARDNAME>|window-settled
haunted_library.txt|T|TrigToken|1|window-settled
hawkeye_master_marksman.txt|T|TrigImmediateTrigger|Mana<1\NumTimes>|window-settled
hazorets_monument.txt|T|TrigDiscard|Discard<1/Card>|window-settled
heart_piercer_manticore.txt|T|DBTrigger|Sac<1/Creature.Other/another creature>|window-settled
hei_bai_spirit_of_balance.txt|T|TrigPutCounter1|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>|window-settled
hellkite_charger.txt|T|TrigUntap|5 R R|window-settled
hematite_talisman.txt|T|TrigUntap|3|window-settled
herigast_erupting_nullkite.txt|T|TrigDraw|ExileFromHand<1/All>|decline-only
hero_of_leina_tower.txt|T|TrigPutCounter|X|window-settled
hexgold_slith.txt|T|TrigPump|PayEnergy<2>|window-settled
high_society_hunter.txt|T|TrigPutCounter|Sac<1/Creature.Other/another creature>|window-settled
hired_heist.txt|T|TrigDraw|U|window-settled
hollow_specter.txt|T|TrigDiscard|X|window-settled
hordewing_skaab.txt|T|TrigDraw|Draw<X/You>|window-settled
horizon_spellbomb.txt|T|TrigDraw|G|window-settled
hormagaunt_horde.txt|T|TrigReturn|2 G|window-settled
horrid_shadowspinner.txt|T|TrigDraw|Draw<X/You>|window-settled
hostile_hostel_creeping_inn.txt|T|TrigDrain|ExileFromGrave<1/Creature/creature card>|window-settled
human_torch.txt|T|DBEffect|R G W U|window-settled
hurkyls_prodigy.txt|T|TrigUnblockable|2|window-settled
hurska_sweet_tooth.txt|T|TrigImmediateTrig|GW|window-settled
hylda_of_the_icy_crown.txt|T|TrigImmediateTrig|1|window-settled
iceman_and_firestar.txt|T|TrigDraw|Discard<1/Card>|window-settled
icewrought_sentry.txt|T|TrigTrigger|1 U|window-settled
ichorid.txt|T|TrigReturn|ExileFromGrave<1/Creature.Black+Other>|window-settled
immersturm_raider.txt|T|TrigDiscard|Discard<1/Card>|window-settled
imoen_trickster_friend.txt|T|TrigCounter|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>|window-settled
imoen_trickster_friend.txt|T|TrigDamage|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>|window-settled
imoen_trickster_friend.txt|T|TrigDraw|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>|window-settled
imoen_trickster_friend.txt|T|TrigImmTrigger|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>|window-settled
imoen_trickster_friend.txt|T|TrigToken|ExileFromGrave<1/Instant;Sorcery/instant or sorcery>|window-settled
impaler_shrike.txt|T|TrigDraw|Sac<1/CARDNAME>|window-settled
in_the_pale_moonlight.txt|K:Chapter|ABToken|Sac<1/Artifact;Creature/artifact or creature>|window-settled
inalla_archmage_ritualist.txt|T|TrigCopyPermanent|1|window-settled
iname_as_one.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
incinerator_of_the_guilty.txt|T|TrigImmediateTrig|CollectEvidence<X>|window-settled
indoctrination_attendant.txt|T|TrigToken|Return<1/Permanent.Other/other permanent>|decline-only
industrial_advancement.txt|T|TrigDig|Sac<1/Creature>|window-settled
ingenious_prodigy.txt|T|TrigDraw|SubCounter<1/P1P1>|decline-only
inheritance.txt|T|TrigDraw|3|window-settled
insidious_bookworms.txt|T|TrigDiscard|1 B|window-settled
interceptor_shadows_hound.txt|T|TrigChangeZone|2 B|window-settled
intet_the_dreamer.txt|T|TrigExile|2 U|window-settled
inti_seneschal_of_the_sun.txt|T|TrigImmediateTrig|Discard<1/Card/card>|window-settled
intimidator_initiate.txt|T|TrigPumpCurse|1|window-settled
intrepid_adversary.txt|T|TrigPay|Mana<1 W\NumTimes>|window-settled
invasion_of_ergamon_truga_cliffcharger.txt|T|TrigChangeZone|Discard<1/Card>|window-settled
invasion_of_mercadia_kyren_flamewright.txt|T|TrigDiscard|Discard<1/Card>|window-settled
invasion_of_new_capenna_holy_frazzle_cannon.txt|T|TrigImmediateTrig|Sac<1/Artifact;Creature/artifact or creature>|window-settled
invisible_woman.txt|T|TrigImmediateTrig|R G W U|window-settled
iron_star.txt|T|TrigGainLife|1|window-settled
ironclad_revolutionary.txt|T|TrigPutCounter|Sac<1/Artifact>|window-settled
irreverent_gremlin.txt|T|TrigDraw|Discard<1/Card>|window-settled
isareth_the_awakener.txt|T|TrigImmediateTrig|X|window-settled
island_fish_jasconius.txt|T|TrigUntap|U U U|window-settled
itzquinth_firstborn_of_gishath.txt|T|TrigImmediate|2|window-settled
ivory_cup.txt|T|TrigGainLife|1|window-settled
izoni_center_of_the_web.txt|T|TrigToken|CollectEvidence<4>|window-settled
izzet_keyrune.txt|T|TrigLoot|Draw<1/You>|window-settled
jackdaw.txt|T|TrigDiscard|Discard<0/Hand>|window-settled
jasconian_isle.txt|T|TrigUntap|U U|window-settled
jedit_ojanen_mercenary.txt|T|TrigToken|G|window-settled
jerren_corrupted_bishop_ormendahl_the_corruptor.txt|T|TrigTransform|4 B B|window-settled
jeskai_ascendancy.txt|T|TrigLoot|Draw<1/You>|window-settled
jeskai_elder.txt|T|TrigLoot|Draw<1/You>|window-settled
jeweled_torque.txt|T|TrigGainLife|2|window-settled
jubilant_mascot.txt|T|TrigPutCounter|3 W|window-settled
jugan_defends_the_temple_remnant_of_the_rising_star.txt|T|TrigImmediateTrig|X|window-settled
kaito_dancing_shadow.txt|T|TrigLoyalty|Return<1/Card.TriggeredSources>|decline-only
kalastria_highborn.txt|T|TrigLoseLife|B|window-settled
kappa_tech_wrecker.txt|T|TrigImmediateTrig|SubCounter<1/Deathtouch>|decline-only
karlach_raging_tiefling.txt|T|TrigDraw|Sac<1/Creature>|window-settled
katara_waterbending_master.txt|T|TrigDraw|Draw<X/You>|window-settled
kate_stewart.txt|T|TrigPumpAll|8|window-settled
kavaron_harrier.txt|T|TrigTokenAttacking|2|window-settled
keldon_raider.txt|T|TrigDiscard|Discard<1/Card>|window-settled
kels_fight_fixer.txt|T|TrigDraw|UB|window-settled
kethek_crucible_goliath.txt|T|TrigSac|Sac<1/Creature.StrictlyOther/another creature>|window-settled
key_to_the_city.txt|T|TrigDraw|2|window-settled
kheru_lich_lord.txt|T|TrigChangZone|2 B|window-settled
kickoff_celebrations.txt|T|TrigDraw|Discard<1/Card>|window-settled
kill_zone_acrobat.txt|T|TrigPump|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>|window-settled
killer_service.txt|T|TrigToken|2 Sac<1/Permanent.token/token>|window-settled
killians_confidence.txt|T|TrigChangeZone|WB|window-settled
killmonger_scourge_of_wakanda.txt|T|TrigSac|Sac<1/Creature.Other/another creature>|window-settled
kilnspire_district.txt|T|RolledChaos|X|window-settled
kinzu_of_the_bleak_coven.txt|T|TrigExile|PayLife<2> ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
kishla_trawlers.txt|T|TrigImmediateTrig|ExileFromGrave<1/Creature/creature card>|window-settled
kitt_kanto_mayhem_diva.txt|T|TrigImmediateTrig|tapXType<2/Creature>|window-settled
knowledge_and_power.txt|T|TrigDmg|2|window-settled
kozileks_return.txt|T|DBDamageAll|ExileFromGrave<1/CARDNAME>|window-settled
krenko_baron_of_tin_street.txt|T|TrigToken|R|window-settled
krovod_haunch.txt|T|TrigToken|1 W|window-settled
kroxa_and_kunoros.txt|T|TrigImmediateTrig|ExileFromGrave<5/Card>|window-settled
krydle_of_baldurs_gate.txt|T|TrigUnblockable|2|window-settled
kuldotha_flamefiend.txt|T|TrigDealDamage|Sac<1/Artifact>|window-settled
kurkesh_onakke_ancient.txt|T|TrigCopyAbility|R|window-settled
labyrinth_adversary.txt|T|TrigImmediateTrig|1 R|window-settled
lamplight_phoenix.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard> CollectEvidence<4>|window-settled
lamplighter_of_selhoff.txt|T|TrigDraw|Draw<1/You>|window-settled
lapis_lazuli_talisman.txt|T|TrigUntap|3|window-settled
larval_scoutlander.txt|T|TrigChangeZone|Sac<1/Land;Lander/land or Lander>|window-settled
lattice_blade_mantis.txt|T|TrigUntap|SubCounter<1/OIL>|decline-only
lazotep_chancellor.txt|T|TrigAmass|1|window-settled
leaf_crowned_visionary.txt|T|TrigDraw|G|window-settled
leatherhead_swamp_stalker.txt|T|TrigImmediateTrig|RemoveAnyCounter<1/Any/NICKNAME>|decline-only
leshracs_sigil.txt|T|TrigDiscard|B B|window-settled
leviathan.txt|T|TrigUntap|Sac<2/Island>|window-settled
leyline_of_lightning.txt|T|TrigDealDamage|1|window-settled
leyline_tyrant.txt|T|TrigImmediateTrig|X|window-settled
lichs_relic.txt|T|TrigImmediateTrig|2|window-settled
lifecrafters_bestiary.txt|T|TrigDraw|G|window-settled
lifesmith.txt|T|TrigGainLife|1|window-settled
lightning_cloud.txt|T|TrigDealDamage|R|window-settled
lightning_phoenix.txt|T|TrigReturn|R|window-settled
lightning_rift.txt|T|TrigDamage|1|window-settled
lilianas_devotee.txt|T|TrigToken|1 B|window-settled
lim_dul_the_necromancer.txt|T|TrigReturn|1 B|window-settled
lingering_phantom.txt|T|TrigReturn|B|window-settled
living_artifact.txt|T|TrigGainLife|SubCounter<1/VITALITY>|decline-only
living_lore.txt|T|TrigSacLore|Sac<1/CARDNAME>|window-settled
llanowar_sentinel.txt|T|TrigChange|1 G|window-settled
loch_dragon.txt|T|TrigDraw|Discard<1/Card>|window-settled
lorcan_warlock_collector.txt|T|TrigReanimate|PayLife<X>|window-settled
lorehold_the_historian.txt|T|TrigDraw|Discard<1/Card>|window-settled
lorthos_the_tidemaker.txt|T|TrigTap|8|window-settled
lunar_mystic.txt|T|TrigDraw|1|window-settled
madame_null_power_broker.txt|T|TrigPutCounter|PayLife<X>|window-settled
magnanimous_magistrate.txt|T|TrigChangeZone|SubCounter<X/REPR>|decline-only
malachite_talisman.txt|T|TrigUntap|3|window-settled
mana_vault.txt|T|TrigUntap|4|window-settled
maralen_of_the_mornsong_avatar.txt|T|TrigPayLife|PayLife<X>|window-settled
marchesa_dealer_of_death.txt|T|TrigDig|1|window-settled
marit_lages_slumber.txt|T|TrigToken|Mandatory Sac<1/CARDNAME>|window-settled
markov_purifier.txt|T|TrigDraw|2|window-settled
marooned.txt|T|TrigTap|3|window-settled
mask_of_griselbrand.txt|T|TrigDraw|PayLife<X>|window-settled
mask_of_memory.txt|T|TrigLoot|Draw<2/You>|window-settled
masked_admirers.txt|T|TrigReturn|G G|window-settled
masked_vandal.txt|T|TrigExile|ExileFromGrave<1/Creature>|window-settled
master_of_death.txt|T|TrigReturn|PayLife<1>|window-settled
master_skald.txt|T|DBFetch|ExileFromGrave<1/Creature>|window-settled
matt_murdock_justice_seeker.txt|T|TrigImmediateTrig|1|window-settled
maulfist_doorbuster.txt|T|TrigPump|PayEnergy<1>|window-settled
meanders_guide.txt|T|TrigImmediateTrig|tapXType<1/Merfolk.Other>|window-settled
meathook_massacre_ii.txt|T|TrigReturn1|PayLife<3>|window-settled
megatron_tyrant_megatron_destructive_force.txt|T|TrigImmediate|Sac<1/Artifact.Other/another artifact>|window-settled
melded_moxite.txt|T|TrigDiscard|Discard<1/Card>|window-settled
mentor_of_the_meek.txt|T|TrigDraw|1|window-settled
mercurial_spelldancer.txt|T|TrigDelayTrig|SubCounter<2/OIL>|decline-only
merfolk_seer.txt|T|TrigDraw|1 U|window-settled
merry_bards.txt|T|TrigImmediate|1|window-settled
miara_thorn_of_the_glade.txt|T|TrigDraw|1 PayLife<1>|window-settled
mica_reader_of_ruins.txt|T|TrigCopy|Sac<1/Artifact>|window-settled
militias_pride.txt|T|TrigToken|W|window-settled
mind_raker.txt|T|TrigChangeZone|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>|window-settled
minds_eye.txt|T|TrigDraw|1|window-settled
mindstab_thrull.txt|T|TrigDiscard|Sac<1/CARDNAME>|window-settled
minion_reflector.txt|T|TrigCopy|2|window-settled
mirari.txt|T|TrigCopy|3|window-settled
mirrorworks.txt|T|TrigCopy|2|window-settled
mishras_self_replicator.txt|T|TrigCopy|1|window-settled
mizzix_replica_rider.txt|T|TrigCopy|1 UR|window-settled
moku_meandering_drummer.txt|T|TrigPump|1|window-settled
monstrosity_of_the_lake.txt|T|TrigTapAll|5|window-settled
moon_circuit_hacker.txt|T|TrigDraw|Draw<1/You>|window-settled
moonveil_regent.txt|T|TrigDraw|Discard<0/Hand>|window-settled
mortal_obstinacy.txt|T|TrigDestroy|Sac<1/CARDNAME>|window-settled
mortarion_daemon_primarch.txt|T|TrigToken|X|window-settled
mukotai_soulripper.txt|T|TrigPutCounter|Sac<1/Creature.Other;Artifact.Other/another artifact or creature>|window-settled
murasa_ranger.txt|T|TrigPutCounters|3 G|window-settled
murder_of_crows.txt|T|TrigLoot|Draw<1/You>|window-settled
murk_strider.txt|T|TrigChangeZone|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>|window-settled
my_genius_knows_no_bounds.txt|T|GeniusLife|X|window-settled
myr_battlesphere.txt|T|TrigPump|tapXType<X/Myr>|window-settled
myrsmith.txt|T|TrigToken|1|window-settled
mystery_key.txt|T|TrigSacrifice|Mandatory Sac<1/CARDNAME>|window-settled
nacre_talisman.txt|T|TrigUntap|3|window-settled
nadir_kraken.txt|T|TrigPutCounter|1|window-settled
najal_the_storm_runner.txt|T|TrigDelayedTrigger|2|window-settled
naktamun.txt|T|RolledChaos|Discard<1/Card>|window-settled
namazu_trader.txt|T|TrigSurveil|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>|window-settled
narset_jeskai_waymaster.txt|T|TrigDraw|Discard<1/Hand>|window-settled
nazar_the_velvet_fang.txt|T|TrigDraw|SubCounter<3/FEEDING>|decline-only
necrite.txt|T|TrigDestroy|Sac<1/CARDNAME>|window-settled
necrodominance.txt|T|TrigDraw|PayLife<X>|window-settled
necromaster_dragon.txt|T|TrigToken|2|window-settled
nether_traitor.txt|T|TrigReturn|B|window-settled
nevinyrral_urborg_tyrant.txt|T|TrigPayCost|1|window-settled
neyith_of_the_dire_hunt.txt|T|TrigPump|2 RG|window-settled
nihil_spellbomb.txt|T|TrigDraw|B|window-settled
nim_deathmantle.txt|T|TrigReturn|4|window-settled
niv_mizzet_ghost_counsel.txt|T|TrigDraw|PayLife<X>|window-settled
null_group_biological_assets.txt|T|TrigDraw|Discard<1/Card>|window-settled
numa_joraga_chieftain.txt|T|TrigPayCost|X X|window-settled
numot_the_devastator.txt|T|TrigDestroy|2 R|window-settled
nurturer_initiate.txt|T|TrigPump|1|window-settled
nyssa_of_traken.txt|T|TrigImmediateTrig|Sac<X/Artifact>|window-settled
ogre_head_helm.txt|T|TrigDiscard|Sac<1/Card.TriggeredSource>|window-settled
ohabi_caleria.txt|T|DBDraw|2|window-settled
oko_lorwyn_liege_oko_shadowmoor_scion.txt|T|TrigTransform|U|window-settled
old_man_willow.txt|T|TrigImmediate|Sac<1/Creature.Other;Card.token/another creature or token>|window-settled
old_one_eye.txt|T|TrigReturn|Discard<2/Card>|window-settled
olivia_mobilized_for_war.txt|T|TrigDiscard|Discard<1/Card>|window-settled
oloro_ageless_ascetic.txt|T|TrigDraw|1|window-settled
ominous_lockbox.txt|T|TrigCopy|Mandatory Sac<1/CARDNAME>|window-settled
onyx_talisman.txt|T|TrigUntap|3|window-settled
order_of_the_golden_cricket.txt|T|TrigPump|W|window-settled
oreplate_pangolin.txt|T|TrigPutCounter|1|window-settled
origin_of_thor.txt|K:Chapter|DBDraw|Discard<1/Card>|window-settled
origin_spellbomb.txt|T|TrigDraw|W|window-settled
oros_the_avenger.txt|T|TrigDamageAll|2 W|window-settled
overclocked_electromancer.txt|T|TrigPutCounter|PayEnergy<3>|window-settled
overseer_of_vault_76.txt|T|TrigImmediateTrig|RemoveAnyCounter<3/QUEST/Permanent/permanents>|decline-only
pack_guardian.txt|T|TrigToken|Discard<1/Land>|window-settled
panic_spellbomb.txt|T|TrigDraw|R|window-settled
papercraft_decoy.txt|T|TrigDraw1|2|window-settled
paramecia_coloniex.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
party_thrasher.txt|T|TrigExile|Discard<1/Card>|window-settled
passenger_ferry.txt|T|TrigImmediateTrig|U|window-settled
pedantic_learning.txt|T|TrigDraw|1|window-settled
persistent_marshstalker.txt|T|TrigChangeZone|2 B|window-settled
pheres_band_raiders.txt|T|GFGToken|2 G|window-settled
phoenix_chick.txt|T|TrigReturn|R R|window-settled
phyrexian_dragon_engine.txt|T|TrigDraw|Discard<1/Hand>|window-settled
pia_aether_ascetic.txt|T|TrigChange|Discard<1/Card>|window-settled
pitchstone_wall.txt|T|TrigChange|Sac<1/CARDNAME>|window-settled
plague_boiler.txt|T|TrigDestroyAll|Mandatory Sac<1/CARDNAME>|window-settled
plundering_predator.txt|T|TrigDiscard|Discard<1/Card>|window-settled
preponderant_pearl.txt|T|TrigConjure2|Mandatory Sac<1/CARDNAME/this artifact>|window-settled
primal_adversary.txt|T|TrigPay|Mana<1 G\NumTimes>|window-settled
proft_consulting_detective.txt|T|TrigPutCounter|2|window-settled
projektor_inspector.txt|T|TrigDraw|Draw<1/You>|window-settled
promise_of_aclazotz_foul_rebirth.txt|T|TrigPopulate|Sac<1/Creature.nonDemon/non-Demon creature>|window-settled
promise_of_bunrei.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>|window-settled
provisions_merchant.txt|T|TrigPumpAll|Sac<1/Food>|window-settled
punishing_fire.txt|T|TrigChange|R|window-settled
purestrain_genestealer.txt|T|TrigRamp|SubCounter<1/P1P1>|decline-only
purgatory.txt|T|TrigReturn|4 PayLife<2>|window-settled
purraj_of_urborg.txt|T|TrigPutCounter|B|window-settled
pyre_zombie.txt|T|TrigReturn|1 B B|window-settled
pyrewild_shaman.txt|T|TrigChange|3|window-settled
quantum_entanglement.txt|T|TrigImmediateTrig|1 W|window-settled
quicksmith_genius.txt|T|TrigDraw|Discard<1/Card>|window-settled
quiet_contemplation.txt|T|TrigTap|1|window-settled
raff_weatherlight_stalwart.txt|T|TrigDraw|tapXType<2/Creature>|window-settled
ragefire_hellkite.txt|T|TrigPump|Sac<1/Creature.Other/another creature>|window-settled
ragged_short_spear.txt|T|TrigDiscard|Discard<1/Card>|window-settled
raiders_spoils.txt|T|TrigDraw|PayLife<1>|window-settled
ral_and_the_implicit_maze.txt|K:Chapter|DBImpulseDraw|Discard<1/Card>|window-settled
rank_officer.txt|T|DBToken|Discard<1/Card>|window-settled
rankle_pitiless_trickster.txt|T|DBTrigger|PayLife<1>|window-settled
rebellion_of_the_flamekin.txt|T|TrigTokenL|1|window-settled
rebellion_of_the_flamekin.txt|T|TrigTokenW|1|window-settled
reckless_racer.txt|T|TrigDiscard|Discard<1/Card>|window-settled
redcap_gutter_dweller.txt|T|TrigPutCounter|Sac<1/Creature.Other/another creature>|window-settled
redcap_raiders.txt|T|TrigPump|tapXType<1/Creature.nonHuman/non-Human creature>|window-settled
redtooth_vanguard.txt|T|TrigReturn|2|window-settled
reflective_golem.txt|T|TrigCopy|2|window-settled
relentless_dead.txt|T|TrigChange|X|window-settled
relentless_dead.txt|T|TrigReturn|B|window-settled
replication_specialist.txt|T|TrigCopy|1 U|window-settled
rescue_leopard.txt|T|TrigDraw|Discard<1/Card>|window-settled
resonance_technician.txt|T|TrigInvestigate|Discard<1/Card>|window-settled
restless_vents.txt|T|TrigLoot|Discard<1/Card>|window-settled
rhovanion_rampager.txt|T|TrigPutCounter|Sac<1/Creature.Other/another creature>|window-settled
richlau_headmaster.txt|T|TrigImmediateTrig|1|window-settled
riddle_gate_gargoyle.txt|T|TrigImmediateTrig|PayEnergy<2>|window-settled
riddlesmith.txt|T|TrigLoot|Draw<1/You>|window-settled
riku_of_two_reflections.txt|T|TrigCopy|G U|window-settled
riku_of_two_reflections.txt|T|TrigCopySpell|U R|window-settled
rings_of_brighthearth.txt|T|TrigCopySpell|2|window-settled
riparian_tiger.txt|T|TrigPump|PayEnergy<2>|window-settled
riptide_entrancer.txt|T|TrigGainControl|Sac<1/CARDNAME>|window-settled
rise_of_the_hobgoblins.txt|T|TrigToken|X|window-settled
rith_the_awakener.txt|T|TrigChoose|2 G|window-settled
rith_the_awakener_avatar.txt|T|TrigToken|5|window-settled
riveting_rigger.txt|T|TrigPutCounter|Sac<1/Artifact.Other/another artifact>|window-settled
roalesk_prime_specimen.txt|T|TrigRandom|X|window-settled
roar_of_resistance.txt|T|TrigPumpAll|1 R|window-settled
robobrain_war_mind.txt|T|TrigDraw|PayEnergy<3>|window-settled
rodolf_duskbringer.txt|T|TrigImmediateTrig|1 WB|window-settled
rohgahh_kher_keep_overlord.txt|T|TrigTokenDragon|2|window-settled
rook_turret.txt|T|TrigLoot|Draw<1/You>|window-settled
rooting_kavu.txt|T|TrigExile|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
rootwater_thief.txt|T|TrigChangeZone|2|window-settled
rubble_rouser.txt|T|TrigDiscard|Discard<1/Card>|window-settled
ruin_processor.txt|T|TrigHerd|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>|window-settled
ruthless_lawbringer.txt|T|TrigSac|Sac<1/Creature.Other/another creature>|window-settled
ruthless_sniper.txt|T|TrigPutCounter|1|window-settled
ruthless_technomancer.txt|T|TrigToken|Sac<1/Creature.Other/another creature you control>|window-settled
rydia_summoner_of_mist.txt|T|TrigDraw|Discard<1/Card>|window-settled
safe_haven.txt|T|TrigReturn|Sac<1/CARDNAME>|window-settled
sage_of_the_falls.txt|T|TrigLoot|Draw<1/You>|window-settled
saheeli_radiant_creator.txt|T|TrigImmediateTrig|PayEnergy<3>|window-settled
saheelis_lattice_mastercraft_raptor.txt|T|TrigDiscard|Discard<1/Card>|window-settled
salvage_drone.txt|T|TrigLoot|Draw<1/You>|window-settled
sample_collector.txt|T|TrigImmediateTrig|CollectEvidence<3>|window-settled
sanctuary_warden.txt|T|TrigChange|RemoveAnyCounter<1/Any/Creature;Planeswalker>|decline-only
sanctum_of_calm_waters.txt|T|TrigDraw|Draw<X/You>|window-settled
sanctum_of_ugin.txt|T|TrigSearch|Sac<1/CARDNAME>|window-settled
sandbender_scavengers.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
sanguine_spy.txt|T|TrigDraw|PayLife<2>|window-settled
sarkhan_dragon_ascendant.txt|T|TrigToken|Behold<1/Dragon>|decline-only
satyr_nyx_smith.txt|T|GFGToken|2 R|window-settled
sauron_the_dark_lord.txt|T|TrigDraw|Discard<1/Hand>|window-settled
savai_thundermane.txt|T|TrigImmediateTrig|2|window-settled
savra_queen_of_the_golgari.txt|T|TrigSacrifice|PayLife<2>|window-settled
saw.txt|T|TrigDraw|Sac<1/Permanent.Other+NotDefinedTriggeredAttacker/permanent other than the triggered attacker or CARDNAME>|window-settled
scarlet_spider_kaine.txt|T|TrigPutCounter|Discard<1/Card>|window-settled
scrapper_champion.txt|T|TrigPutCounter|PayEnergy<2>|window-settled
scrapwork_mutt.txt|T|TrigDraw|Discard<1/Card>|window-settled
screeching_bat_stalking_vampire.txt|T|TrigTransform|2 B B|window-settled
scuzzback_scrounger.txt|T|TrigToken|Blight<1>|window-settled
searing_meditation.txt|T|TrigDamage|2|window-settled
seers_sundial.txt|T|TrigDraw|2|window-settled
selfcraft_mechan.txt|T|TrigSac|Sac<1/Artifact/artifact>|window-settled
sentry_bot.txt|T|PutCounterAll|PayEnergy<3>|window-settled
sephiroth_fabled_soldier_sephiroth_one_winged_angel.txt|T|TrigDraw1|Sac<1/Creature.Other/another creature>|window-settled
sephiroth_fabled_soldier_sephiroth_one_winged_angel.txt|T|TrigDraw2|Sac<X/Creature.Other/another creature>|window-settled
seraph_of_new_capenna_seraph_of_new_phyrexia.txt|T|TrigPump|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>|window-settled
serene_steward.txt|T|TrigPutCounter|W|window-settled
servant_of_the_stinger.txt|T|TrigSearch|Sac<1/CARDNAME>|window-settled
seymour_flux.txt|T|TrigDraw|PayLife<1>|window-settled
shadow_mysterious_assassin.txt|T|TrigDraw|Sac<1/Permanent.Other+nonLand/another nonland permanent>|window-settled
shambling_cieth.txt|T|TrigReturn|B|window-settled
shanna_purifying_blade.txt|T|TrigDraw|X|window-settled
shessra_deaths_whisper.txt|T|TrigDraw|PayLife<2>|window-settled
shipwreck_looter.txt|T|TrigDraw|Draw<1/You>|window-settled
shire_shirriff.txt|T|TrigSac|Sac<1/Card.token/token>|window-settled
shoal_kraken.txt|T|TrigDraw|Draw<1/You>|window-settled
shrapnel_slinger.txt|T|TrigSac|Sac<1/Creature>|window-settled
shriek_treblemaker.txt|T|DBImmediateTrig|Discard<1/Card>|window-settled
shu_yun_the_silent_tempest.txt|T|TrigPump|RW RW|window-settled
sigil_of_the_new_dawn.txt|T|TrigReturn|1 W|window-settled
silvan_reveler.txt|T|TrigReturn|1 G U|window-settled
skeleton_key.txt|T|TrigDraw|Draw<1/You>|window-settled
skirk_drill_sergeant.txt|T|TrigDig|2 R|window-settled
skyfisher_spider.txt|T|TrigSac|Sac<1/Creature.Other/another creature>|window-settled
skyline_scout.txt|T|TrigPump|1 W|window-settled
skyrider_patrol.txt|T|TrigPayCost|G U|window-settled
skyswimmer_koi.txt|T|TrigLoot|Draw<1/You>|window-settled
skywarp_skaab.txt|T|DBDraw|ExileFromGrave<2/Creature/creature card>|window-settled
skywise_teachings.txt|T|TrigToken|1 U|window-settled
slab_hammer.txt|T|TrigPump|Return<1/Land>|decline-only
slinza_the_spiked_stampede.txt|T|TrigImmediateTrig|1 RG|window-settled
sludge_strider.txt|T|TrigLoseLife|1|window-settled
slumbering_walker.txt|T|TrigImmediateTrig|RemoveAnyCounter<1/Any/CARDNAME/this creature>|decline-only
smellerbee_rebel_fighter.txt|T|TrigDraw|Discard<1/Hand>|window-settled
smelted_chargebug.txt|T|TrigPump|PayEnergy<1>|window-settled
smolder_initiate.txt|T|TrigLoseLife|1|window-settled
smugglers_copter.txt|T|TrigLoot|Draw<1/You>|window-settled
snaremaster_sprite.txt|T|TrigImmediateTrig|2|window-settled
sokenzan_smelter.txt|T|TrigToken|1 Sac<1/Artifact/an artifact>|window-settled
sorcerers_broom.txt|T|TrigCopy|3|window-settled
soul_net.txt|T|TrigGainLife|1|window-settled
sourbread_auntie.txt|T|TrigToken|Blight<2>|window-settled
sparktongue_dragon.txt|T|TrigPayCost|2 R|window-settled
spectral_adversary.txt|T|TrigPay|Mana<1 U\NumTimes>|window-settled
speed_young_avenger.txt|T|TrigImmediateTrig|1|window-settled
spellbook_vendor.txt|T|TrigImmediate|1|window-settled
spider_gwen_free_spirit.txt|T|TrigDraw|Discard<1/Card>|window-settled
spider_man_to_the_rescue.txt|T|TrigImmediateTrig|tapXType<1/Card.Self/CARDNAME>|window-settled
spiked_ripsaw.txt|T|TrigPump|Sac<1/Forest>|window-settled
spined_tyrranax.txt|T|TrigImmediateTrig|2 G|window-settled
spirit_bonds.txt|T|TrigToken|W|window-settled
spirit_cairn.txt|T|TrigToken|W|window-settled
spit_flame.txt|T|TrigABChangeZone|R|window-settled
springbloom_druid.txt|T|TrigRamp|Sac<1/Land>|window-settled
squealing_devil.txt|T|TrigPump|X|window-settled
squirrel_sanctuary.txt|T|TrigReturn|1|window-settled
stadium_tidalmage.txt|T|TrigDiscard|Draw<1/You>|window-settled
stalking_tiger_avatar.txt|T|TrigDraw|1|window-settled
standstill.txt|T|TrigSac|Mandatory Sac<1/CARDNAME>|window-settled
starks_ingenuity.txt|T|TrigDraw|X|window-settled
steamflogger_service_rep.txt|T|TrigAssemble|1|window-settled
stockpiling_celebrant.txt|T|TrigScry|Return<1/Permanent.nonLand+Other/another nonland permanent>|decline-only
strefan_maurer_progenitor.txt|T|TrigChangeZone|Sac<2/Blood.token/Blood token>|window-settled
subway_train.txt|T|TrigChangeZone|G|window-settled
summon_g_f_ifrit.txt|K:Chapter|DBDiscard|Discard<1/Card>|window-settled
sun_droplet.txt|T|TrigGainLife|SubCounter<1/CHARGE>|decline-only
sunstreak_phoenix.txt|T|TrigReturn|1 R|window-settled
surge_mare.txt|T|TrigDraw|Draw<1/You>|window-settled
surgespanner.txt|T|TrigBounce|1 U|window-settled
surtland_flinger.txt|T|TrigImmediate|Sac<1/Creature.Other/another creature>|window-settled
surveillance_monitor.txt|T|TrigEvidence|CollectEvidence<4>|window-settled
sutina_speaker_of_the_tajuru.txt|T|TrigImmediateTrig|Return<1/Land>|decline-only
swarm_culler.txt|T|TrigDraw|Sac<1/Creature.Other;Artifact.Other/another creature or artifact>|window-settled
sygg_wanderwine_wisdom_sygg_wanderbrine_shield.txt|T|TrigTransform|U|window-settled
sylvan_library.txt|T|TrigDraw|Draw<2/You>|window-settled
symmetry_matrix.txt|T|TrigDraw|1|window-settled
t45_power_armor.txt|T|TrigUntap|PayEnergy<1>|window-settled
tablet_of_epityr.txt|T|TrigGainLife|1|window-settled
taeko_the_patient_avalanche.txt|T|TrigImmediateTrig|UB|window-settled
tainted_adversary.txt|T|TrigPay|Mana<2 B\NumTimes>|window-settled
tainted_observer.txt|T|TrigProliferate|2|window-settled
taj_nar_swordsmith.txt|T|TrigChange|X|window-settled
telekinetic_bonds.txt|T|TrigTapOrUntap|1 U|window-settled
tenacious_dead.txt|T|TrigReturn|1 B|window-settled
teneb_the_harvester.txt|T|TrigChange|2 B|window-settled
tenured_tethermage.txt|T|TrigToken|Sac<1/Land>|window-settled
terra_herald_of_hope.txt|T|TrigPayCost|2|window-settled
terror_ballista.txt|T|TrigSac|Sac<1/Creature.Other/another creature>|window-settled
terror_of_towashi.txt|T|TrigImmediateTrig|3 B|window-settled
tester_of_the_tangential.txt|T|TrigImmediateTrig|X|window-settled
tether_technician.txt|T|TrigImmediateTrig|Discard<1/Card>|window-settled
tetravus.txt|T|TrigPutCounters|Exile<X/Creature.IsRemembered/Tetravite>|window-settled
tetravus.txt|T|TrigToken|SubCounter<X/P1P1>|decline-only
the_balrog_of_moria.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
the_disciple_of_nissa.txt|T|TrigImmediateTrig|X X X G G|window-settled
the_falcon_airship_restored.txt|T|TrigImmediateTrigger|Sac<1/CARDNAME>|window-settled
the_fantasticar.txt|T|TrigToken|Sac<1/CARDNAME>|window-settled
the_fire_nation_drill.txt|T|TrigImmediateTrigger|tapXType<1/Card.Self/CARDNAME>|window-settled
the_fugitive_doctor.txt|T|TrigImmediateTrig|Sac<1/Clue>|window-settled
the_gitrog_ravenous_ride.txt|T|TrigDraw|Sac<1/Creature.SaddledThisTurn/creature that saddled it this turn>|window-settled
the_goose_mother.txt|T|TrigDraw|Sac<1/Food>|window-settled
the_huntsmans_redemption.txt|K:Chapter|ABSearch|Sac<1/Creature>|window-settled
the_kingpin_of_crime.txt|T|TrigEffect|PayLife<2>|window-settled
the_meep.txt|T|TrigAnimateAll|Sac<1/Creature.Other/another creature>|window-settled
the_motherlode_excavator.txt|T|TrigImmediateTrig|PayEnergy<4>|window-settled
the_rise_of_sozin_fire_lord_sozin.txt|T|TrigImmediateTrig|X|window-settled
the_sackville_bagginses.txt|T|TrigDraw|Sac<1/Artifact.Other;Creature.Other/another creature or artifact>|window-settled
the_thing.txt|T|TrigImmediateTrig|R G W U|window-settled
thousand_moons_crackshot.txt|T|TrigImmediateTrig|2 W|window-settled
thousand_moons_smithy_barracks_of_the_thousand.txt|T|TrigTransform|tapXType<5/Artifact;Creature/artifact or creature>|window-settled
three_dog_galaxy_news_dj.txt|T|TrigImmediateTrig|2 Sac<1/Aura.Attached/Aura attached to CARDNAME>|window-settled
thriving_grubs.txt|T|TrigPutCounter|PayEnergy<2>|window-settled
thriving_ibex.txt|T|TrigPutCounter|PayEnergy<2>|window-settled
thriving_rats.txt|T|TrigPutCounter|PayEnergy<2>|window-settled
thriving_rhino.txt|T|TrigPutCounter|PayEnergy<2>|window-settled
thriving_skyclaw.txt|T|TrigPutCounter|PayEnergy<3>|window-settled
thriving_turtle.txt|T|TrigPutCounter|PayEnergy<2>|window-settled
throne_of_bone.txt|T|TrigGainLife|1|window-settled
throwing_knife.txt|T|TrigDamage|Sac<1/CARDNAME>|window-settled
thunderblade_charge.txt|T|TrigPlay|2 R R R|window-settled
tidal_terror.txt|T|TrigUnblockable|tapXType<2/Creature>|window-settled
tiger_tribe_hunter.txt|T|TrigImmediate|Sac<1/Creature.Other/another creature>|window-settled
tilonallis_summoner.txt|T|TrigToken|X R|window-settled
timothar_baron_of_bats.txt|T|TrigToken|1 ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
titan_of_littjara.txt|T|TrigDraw|Draw<X/You>|window-settled
tivash_gloom_summoner.txt|T|TrigToken|PayLife<X>|window-settled
tolarian_kraken.txt|T|TrigImmediateTrig|1|window-settled
tomakul_phoenix.txt|T|TrigReturn|X R|window-settled
toph_hardheaded_teacher.txt|T|TrigChangeZone|Discard<1/Card>|window-settled
trail_of_crumbs.txt|T|TrigDig|1|window-settled
tranquil_frillback.txt|T|TrigPay|Mana<G\NumTimes>|window-settled
transplant_theorist.txt|T|TrigLoot|Draw<1/You>|window-settled
treetop_sentries.txt|T|TrigDraw|Forage|decline-only
treva_the_renewer.txt|T|TrigChoose|2 W|window-settled
tribute_to_horobi_echo_of_deaths_wail.txt|T|TrigDraw|Sac<1/Creature.Other/another creature>|window-settled
trudge_garden.txt|T|TrigToken|2|window-settled
trystan_callous_cultivator_trystan_penitent_culler.txt|T|TrigTransform|G|window-settled
ty_lee_artful_acrobat.txt|T|TrigImmediateTrig|1|window-settled
tymna_the_weaver.txt|T|TrigDraw|PayLife<X>|window-settled
ugins_binding.txt|T|TrigImmediateTrig|ExileFromGrave<1/CARDNAME>|window-settled
ulalek_fused_atrocity.txt|T|TrigCopySpell|C C|window-settled
ulamogs_despoiler.txt|T|TrigPutCounters|ExiledMoveToGrave<2/Card.OppOwn/card an opponent owns>|window-settled
ulamogs_nullifier.txt|T|TrigProcess|ExiledMoveToGrave<2/Card.OppOwn/card an opponent owns>|window-settled
ulamogs_reclaimer.txt|T|TrigChangeZone|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>|window-settled
ultimecia_time_sorceress_ultimecia_omnipotent.txt|T|TrigTransform|4 U U B B ExileFromGrave<8/Card>|window-settled
ultron_artificial_malevolence.txt|T|TrigCopy|2|window-settled
ultron_unlimited.txt|T|TrigToken|1|window-settled
unassuming_sage.txt|T|TrigToken|2|window-settled
unconventional_tactics.txt|T|TrigChange|W|window-settled
uncover_the_moon_letters.txt|T|TrigDiscard|Draw<X/You>|window-settled
undead_butler.txt|T|TrigImmediateTrig|ExileAnyGrave<1/Card.TriggeredNewCard>|window-settled
undercity_eliminator.txt|T|TrigImmediateTrig|Sac<1/Artifact;Creature/artifact or creature>|window-settled
undercity_scavenger.txt|T|TrigPutCounter|Sac<1/Creature.Other/another creature>|window-settled
underhanded_designs.txt|T|TrigDrain|1|window-settled
unscrupulous_contractor.txt|T|TrigSac|Sac<1/Creature>|window-settled
urzas_chalice.txt|T|TrigGainLife|1|window-settled
urzas_miter.txt|T|TrigDraw|3|window-settled
urzas_sylex.txt|T|TrigSearch|2|window-settled
valentin_dean_of_the_vein_lisette_dean_of_the_root.txt|T|TrigPutCounterAll|1|window-settled
valkyries_sword.txt|T|TrigToken|4 W|window-settled
vampire_gourmand.txt|T|TrigDraw|Sac<1/Creature.Other/another creature>|window-settled
vanguard_of_the_restless.txt|T|TrigReturn|2 W|window-settled
vanille_cheerful_lcie_ragnarok_divine_deliverance.txt|T|Meld|3 B G|window-settled
vaultbreaker.txt|T|TrigDiscard|Discard<1/Card>|window-settled
vaultguard_trooper.txt|T|TrigDraw|Discard<0/Hand>|window-settled
veinwitch_coven.txt|T|TrigReturn|B|window-settled
venomous_brutalizer.txt|T|TrigProliferate|1 G|window-settled
venus_torn_between_worlds.txt|T|TrigDraw|U|window-settled
verazol_the_split_current.txt|T|TrigCopy|SubCounter<2/P1P1>|decline-only
veronica_dissident_scribe.txt|T|TrigDraw|Discard<1/Card>|window-settled
verrak_warped_sengir.txt|T|TrigCopySpell|PayLife<X>|window-settled
viashino_racketeer.txt|T|TrigDiscard|Discard<1/Card>|window-settled
vigil_for_the_lost.txt|T|TrigGainLife|X|window-settled
vile_redeemer.txt|T|TrigToken|C|window-settled
vivis_persistence.txt|T|TrigChangeZone|2|window-settled
vizkopa_confessor.txt|T|OppRevealX|PayLife<X>|window-settled
volatile_wanderglyph.txt|T|TrigDraw|Discard<1/Card>|window-settled
voltaic_brawler.txt|T|TrigPump|PayEnergy<1>|window-settled
voltstorm_angel.txt|T|TrigImmediateTrig|PayEnergy<2>|window-settled
voracious_tome_skimmer.txt|T|TrigDraw|PayLife<1>|window-settled
vorosh_the_hunter.txt|T|TrigPutCounter|2 G|window-settled
vraska_the_silencer.txt|T|TrigChangeZone|1|window-settled
wandering_champion.txt|T|TrigDiscard|Discard<1/Card>|window-settled
warcry_phoenix.txt|T|TrigReturn|2 R|window-settled
warren_torchmaster.txt|T|TrigImmediate|Blight<1>|window-settled
wasp_of_the_bitter_end.txt|T|TrigDestroy|Sac<1/CARDNAME>|window-settled
wasteland_strangler.txt|T|TrigChangeZone|ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>|window-settled
weakstones_subjugation.txt|T|TrigTap|3|window-settled
weapons_vendor.txt|T|TrigImmediateTrigger|1|window-settled
wedding_security.txt|T|TrigPutCounter|Sac<1/Blood.token/Blood token>|window-settled
west_wind_avatar.txt|T|TrigGainLife|Sac<1/Permanent.token;Land/token or a land>|window-settled
wharf_infiltrator.txt|T|TrigDraw|Draw<1/You>|window-settled
wharf_infiltrator.txt|T|TrigToken|2|window-settled
which_of_you_burns_brightest.txt|T|DarkEffect|X|window-settled
whiskerquill_scribe.txt|T|TrigDraw|Discard<1/Card>|window-settled
whispering_specter.txt|T|TrigDiscard|Sac<1/CARDNAME>|window-settled
wildborn_preserver.txt|T|TrigImmediateTrig|X|window-settled
wilhelt_the_rotcleaver.txt|T|TrigDraw|Sac<1/Zombie>|window-settled
windrider_wizard.txt|T|TrigLoot|Draw<1/You>|window-settled
winter_tormented_loner.txt|T|TrigImmediateTrig|Sac<1/Creature;Planeswalker/creature or planeswalker>|window-settled
wolf_of_devils_breach.txt|T|TrigDamage|1 R Discard<1/Card>|window-settled
wolfbat.txt|T|TrigChangeZone|B|window-settled
wooden_sphere.txt|T|TrigGainLife|1|window-settled
wyll_pact_bound_duelist.txt|T|TrigEffect|Sac<1/Creature.Other;Artifact/another creature or an artifact>|window-settled
wyll_pact_bound_duelist.txt|T|TrigImmediateTrig|Sac<1/Creature.Other;Artifact/another creature or an artifact>|window-settled
wyll_pact_bound_duelist.txt|T|TrigUntap|Sac<1/Creature.Other;Artifact/another creature or an artifact>|window-settled
yasova_dragonclaw.txt|T|TrigChange|1 UR UR|window-settled
yotia_declares_war.txt|K:Chapter|TrigTap|Mandatory tapXType<X/Artifact>|window-settled
young_necromancer.txt|T|TrigImmediateTrig|ExileFromGrave<2/card>|window-settled
yuma_proud_protector.txt|T|TrigDraw|Sac<1/Land>|window-settled
yuyan_archers.txt|T|TrigDraw|Discard<1/Card>|window-settled
zhentarim_bandit.txt|T|TrigToken|PayLife<1>|window-settled
ziatora_the_incinerator.txt|T|DBTrigger|Sac<1/Creature.StrictlyOther/another creature>|window-settled
zoraline_cosmos_caller.txt|T|TrigImmediateTrig|W B PayLife<2>|window-settled
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
		if families := costFamiliesInRaw(strings.TrimPrefix(raw, "Mandatory ")); len(families) == 1 && families["tapXType"] {
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
	// A pure dynamic-tap cost is settled by the window's tap election. Mixed
	// tap costs remain decline-only: production offers that election only when
	// the non-tap remainder is empty.
	if len(families) == 1 && families["tapXType"] {
		return "window-settled"
	}
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
		if !inAngle && token != "" && (token == "X" || token[0] >= '0' && token[0] <= '9' || strings.Trim(token, "WUBRGC") == "") || token == "Mana" {
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
	wantRoutesByCarrier := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(triggerBodyCostInventory), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 5)
		if len(parts) != 5 {
			t.Fatalf("invalid pinned trigger Cost$ inventory row %q", line)
		}
		carrier := strings.Join(parts[:4], "|")
		want[carrier] = true
		wantRoutesByCarrier[carrier] = parts[4]
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
	var routeDrift []string
	for row := range got {
		raw := strings.SplitN(row, "|", 4)[3]
		seen := costFamiliesInRaw(raw)
		route := triggerCostRoute(row)
		if route == "invalid" {
			t.Fatalf("unclassified trigger cost carrier %s", row)
		}
		if want := wantRoutesByCarrier[row]; route != want {
			routeDrift = append(routeDrift, fmt.Sprintf("%s => %s (want %s)", row, route, want))
		}
		routes[route]++
		for fam := range seen {
			families[fam]++
		}
	}
	wantFamilies := map[string]int{"Mana": 384, "Sac": 163, "Discard": 97, "Draw": 38, "PayEnergy": 37, "PayLife": 36, "ExileFromGrave": 22, "tapXType": 17, "ExileAnyGrave": 16, "SubCounter": 15, "ExiledMoveToGrave": 9, "Blight": 7, "Return": 7, "CollectEvidence": 6, "Forage": 5, "RemoveAnyCounter": 4, "Exile": 2, "ExileFromHand": 2, "PutCardToLibFromGrave": 2, "Behold": 1, "ExileCtrlOrGrave": 1, "Mill": 1}
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
	wantRoutes := map[string]int{"window-settled": 816, "decline-only": 39}
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
