package effects

import (
	"sort"
	"strings"
)

// creatureSubtypeWords is the authoritative corpus vocabulary of creature
// subtype tokens. It is deliberately a positive set rather than the
// complement of card/supertype words: the latter accidentally made a
// Changeling an Arcane, Alara, or Ajani, even though those are spell, plane,
// and planeswalker subtypes. Forge stores multiword types as separate tokens,
// so this is necessarily token-based like the rest of the filter grammar.
var creatureSubtypeWords = func() map[string]bool {
	out := make(map[string]bool)
	for word := range strings.FieldsSeq(`
Advisor Aetherborn Alien Ally Andorian Angel Antelope Ape Archer Archon Armadillo
Armored Army Artificer Assassin Assembly-Worker Astartes Atog Aurochs Avatar Azra
B.O.B. Badger Bahamut Balloon Barbarian Bard Basilisk Bat Bear Beast Beaver
Beeble Beholder Berserker Bird Bison Blinkmoth Boar Borg Brainiac Bringer Brushwagg
C'tan Caitian Camarid Camel Capybara Caribou Carrier Cat Centaur Chef Child Chimera
Citizen Clamfolk Cleric Clown Cockatrice Construct Cow Coward Coyote Crab
Crocodile Custodes Cyberman Cyborg Cyclops Dalek Dauthi Demigod Demon Deserter
Detective Devil Dinosaur Djinn Doctor Dog Dragon Drake Dreadnought Drix Drone
Druid Dryad Duck Dwarf Echidna Efreet Egg Elder Eldrazi Elemental Elephant Elf
Elk Employee Eternal Eye Faerie Ferret Fighter Fish Flagbearer Fox Fractal Frog
Fungus Gamer Gamma Gargoyle Germ Giant Giraffe Gith Glass Glimmer Gnoll Gnome Goat
Goblin God Golem Gorgon Gorilla Gorn Graveborn Gremlin Griffin Guest Hag Halfling Hamster
Harpy Hedgehog
Hellion Hero Hippo Hippogriff Homarid Homunculus Horror Horse Horsehead Human
Hydra Hyena Illusion Imp Incarnation Inhuman Inkling Inquisitor Insect Jackal
Jellyfish Juggernaut Kangaroo Kavu Kelpien Killbot Kirin Kithkin Klingon Knight
Kobold Kor Kraken Kree Llama Lamia Lammasu Lanthanite Leech Lemur Leviathan Lhurgoyf
Licid Lizard Lobster Lord Mammoth Manticore Masticore Mercenary Merfolk
Metathran Minion Minotaur Mite Mole Monger Mongoose Monk Monkey Moogle Moonfolk
Mount Mouse Mutant Myr Mystic Nautilus Necron Nephilim Nightmare Nightstalker
Ninja Noble Noggle Nomad Nymph Octopus Officer Ogre Ooze Orb Orc Orion Orgg Otter
Ouphe Ox Oyster Pangolin Peasant Pegasus Pentavite Performer Pest Phelddagrif
Phoenix Phyrexian Pilot Pincher Pirate Plant Platypus Porcupine Possum Praetor
Primarch Prism Processor Q Qu Rabbit Raccoon Ranger Rat Rebel Reflection Rhino
Rigger Robot Rogue Romulan Sable Salamander Samurai Sand Saproling Satyr Scarecrow
Scientist Scion Scout Sculpture Scorpion Seal Serf Serpent Servo Shade Shaman
Shapeshifter Shark Sheep Shi'ar Siren Skeleton Skrull Skunk Slith Sliver Sloth
Slug Snail Snake Soldier Soltari Sorcerer Specter Spellshaper Sphinx Spider Spike
Spawn Spirit Splinter Sponge Spy Squid Squirrel Stained Starfish Surrakar Survivor
Symbiote Synth Talosian Teddy Tellarite Tentacle Tetravite Thalakos Tholian Thopter
Thrull Tiefling Time Toy Treefolk Trilobite Triskelavite Troll Tosk Turtle Tyranid
Unicorn Utrom Vampire Varmint Vedalken Villain Volver Vorta Vulcan Wall Walrus
Warlock Warrior Weasel Weird Werewolf Whale Wizard Wolf Wolverine Wombat Worm
Wraith Wurm Xindi Yeti Zombie Zubera
`) {
		out[word] = true
	}
	return out
}()

// creatureTypeWordList is the creature-subtype vocabulary as one sorted
// slice, built once at package init (map range -> sort, so the order is
// deterministic). rules' layer-4 "all creature types" grant (Maskwood
// Nexus, the manland family) materialises this into the type walk's list;
// a positive vocabulary keeps the grant from ever leaking a non-creature
// word (Arcane/Alara/Ajani) onto an affected object.
var creatureTypeWordList = func() []string {
	out := make([]string, 0, len(creatureSubtypeWords))
	for w := range creatureSubtypeWords {
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}()

// CreatureTypeWordList returns every creature subtype word the filter
// grammar knows, sorted (deterministic; the caller must not mutate it).
func CreatureTypeWordList() []string { return creatureTypeWordList }

// cardTypeWords is the CR 205.1 card-type vocabulary (including the retired
// Tribal and Vanguard), the set Count$Valid...$CardTypes counts distinct
// members of (Tarmogoyf).
var cardTypeWords = map[string]bool{
	"Artifact": true, "Battle": true, "Conspiracy": true, "Creature": true,
	"Dungeon": true, "Enchantment": true, "Instant": true, "Land": true,
	"Phenomenon": true, "Plane": true, "Planeswalker": true, "Scheme": true,
	"Sorcery": true, "Tribal": true, "Vanguard": true,
}

// permanentTypeWords is the CR 205.2 permanent-type vocabulary, the set
// Count$Valid...$CardTypesPermanent counts distinct members of (Korvold,
// Gleeful Glutton's combat-damage trigger; Matzalantli, the Great Door's
// transform gate -- whose oracle text names the six). Battles stay in the
// set: no carrier can match one today (battles never enter combat here),
// but the vocabulary is the rule's, not the engine's reach.
var permanentTypeWords = map[string]bool{
	"Artifact": true, "Battle": true, "Creature": true, "Enchantment": true,
	"Land": true, "Planeswalker": true,
}

// CardTypeWord reports whether w is a CR 205.2a card type word (the
// cardTypeWords vocabulary, plus Kindred -- the current spelling of the
// Tribal type).
func CardTypeWord(w string) bool { return cardTypeWords[w] || w == "Kindred" }
