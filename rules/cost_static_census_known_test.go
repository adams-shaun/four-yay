package rules

// knownBrokenCostStatics / knownWrongCostStatics are TestCostStaticCensus'
// committed baseline (Ruling R-20's two-direction contract, the
// knownUnsupported shape): card name -> its sorted reason classes, measured
// over the whole corpus. The census fails on a card that newly measures
// BROKEN/WRONG (naming the finding and the engine file:line) AND on an entry
// the build no longer measures (stale). Both tables only ever shrink: a fix
// deletes its rows; a regression is fixed, never added here. On a mismatch
// the test logs the measured table as a Go literal.
//
// BROKEN: the engine can never apply the static (a fail-closed shape).
var knownBrokenCostStatics = map[string]string{
	"Aether Tide":                  "RaiseCost Cost$ unmodelled",
	"Beluna Grandsquall":           "ValidCard$ dead: Permanent base on a spell",
	"Benevolent River Spirit":      "RaiseCost Cost$ unmodelled",
	"Brutal Suppression":           "RaiseCost Cost$ unmodelled",
	"Captain Eberhart":             "ValidCard$ dead",
	"Carth the Lion":               "RaiseCost Cost$ unmodelled",
	"Cemetery Prowler":             "Amount$ unevaluable by EvalCountOK",
	"Champion of the Clachan":      "RaiseCost Cost$ unmodelled",
	"Champion of the Path":         "RaiseCost Cost$ unmodelled",
	"Champion of the Weird":        "RaiseCost Cost$ unmodelled",
	"Champions of the Perfect":     "RaiseCost Cost$ unmodelled",
	"Champions of the Shoal":       "RaiseCost Cost$ unmodelled",
	"Chandra's Incinerator":        "Amount$ unevaluable by EvalCountOK",
	"Close Encounter":              "RaiseCost Cost$ unmodelled",
	"Crashing Wave":                "RaiseCost Cost$ unmodelled",
	"Cunning Nightbonder":          "ValidCard$ dead",
	"Damping Sphere":               "Relative$ True on RaiseCost (only ReduceCost has an exception)",
	"Doc Aurlock, Grizzled Genius": "ValidSpell$ unsupported: Static.Plotting",
	"Dream Chisel":                 "ValidSpell$ unsupported: Spell.isCastFaceDown",
	"Drought":                      "RaiseCost Cost$ unmodelled",
	"Elite Spellbinder":            "ValidCard$ dead",
	"Exiled Doomsayer":             "ValidSpell$ unsupported: Static.MorphUp",
	"Explosive Singularity":        "RaiseCost Cost$ unmodelled",
	"Fast Forward":                 "Amount$ unevaluable by EvalCountOK",
	"Fireball":                     "Relative$ True on RaiseCost (only ReduceCost has an exception)",
	"Flying Drone":                 "SA ReduceAmount$ unread (ReduceCost$ is a mana cost); SA ReduceCost$ unevaluable",
	"Foggy Swamp Visions":          "RaiseCost Cost$ unmodelled",
	"Glamdring, Foe-hammer":        "Amount$ unevaluable by EvalCountOK",
	"Gonti, Canny Acquisitor":      "ValidCard$ dead",
	"Grafted Identity":             "RaiseCost Cost$ unmodelled",
	"Hamlet Glutton":               "ValidSpell$ unsupported: Spell.Bargain",
	"Harrowing Swarm":              "ValidSpell$ unsupported: Static.isTurnFaceUp",
	"Heliod, the Radiant Dawn":     "Amount$ unevaluable by EvalCountOK",
	"Hinata, Dawn-Crowned":         "Relative$ True on RaiseCost (only ReduceCost has an exception)",
	"Hum of the Radix":             "Relative$ True on RaiseCost (only ReduceCost has an exception)",
	"Ice Out":                      "ValidSpell$ unsupported: Spell.Bargain",
	"Inquisitive Glimmer":          "ValidSpell$ unsupported: Static.Unlock",
	"Johann's Stopgap":             "ValidSpell$ unsupported: Spell.Bargain",
	"Kami of Jealous Thirst":       "SA ReduceAmount$ unread (ReduceCost$ is a mana cost); SA ReduceCost$ unevaluable",
	"Korvold, Gleeful Glutton":     "Amount$ unevaluable by EvalCountOK",
	"Liesa, Shroud of Dusk":        "RaiseCost Cost$ unmodelled",
	"Lightstall Inquisitor":        "ValidCard$ dead",
	"March of Burgeoning Life":     "RaiseCost Cost$ unmodelled",
	"March of Otherworldly Light":  "RaiseCost Cost$ unmodelled",
	"March of Reckless Joy":        "RaiseCost Cost$ unmodelled",
	"March of Swirling Mist":       "RaiseCost Cost$ unmodelled",
	"March of Wretched Sorrow":     "RaiseCost Cost$ unmodelled",
	"Mavinda, Students' Advocate":  "Effect-delivered: param outside whitelist; ValidCard$ dead",
	"Memory Crystal":               "ValidSpell$ unsupported: Spell.Buyback",
	"Moonrager's Slash":            "Condition$ Night unsupported",
	"Obscuring Aether":             "ValidSpell$ unsupported: Spell.isCastFaceDown",
	"Officious Interrogation":      "Relative$ True on RaiseCost (only ReduceCost has an exception)",
	"Pollywog Symbiote":            "ValidCard$ dead",
	"Professor Hojo":               "CheckSVar$ unevaluable",
	"Samut, the Driving Force":     "Amount$ unevaluable by EvalCountOK",
	"Seal of the Guildpact":        "Amount$ unevaluable by EvalCountOK; Relative$ ReduceCost amount unresolvable, no {X}",
	"Seize the Secrets":            "CheckSVar$ unevaluable",
	"Semblance Anvil":              "ValidCard$ dead",
	"Soul Partition":               "ValidCard$ dead",
	"Synchronized Eviction":        "CheckSVar$ unevaluable",
	"Tectonic Split":               "RaiseCost Cost$ unmodelled",
	"Tezzeret, Betrayer of Flesh":  "CheckSVar$ unevaluable",
	"Urianger Augurelt":            "ValidSpell$ unsupported: Spell.MayPlaySource",
	"Warbringer":                   "ValidSpell$ unsupported: Spell.Dash",
	"Water Whip":                   "RaiseCost Cost$ unmodelled",
	"Waterbender's Restoration":    "RaiseCost Cost$ unmodelled",
	"Zimone, Infinite Analyst":     "ValidCard$ dead",
}

// WRONG: the engine applies the static but ignores or misreads a parameter.
// OFFER-BLIND findings (applied only at the payment-time reprice, never at
// the offer gate) ride this table with an "OFFER-BLIND: " prefix.
var knownWrongCostStatics = map[string]string{
	"Closing Statement":           "param unread: Phases$; param unread: PlayerTurn$",
	"Drought":                     "param unread: ForEachShard$",
	"Elite Spellbinder":           "AffectedZone$ ignored for spells",
	"Invasion of Gobakhan":        "AffectedZone$ ignored for spells",
	"Lightstall Inquisitor":       "AffectedZone$ ignored for spells",
	"Mana Matrix":                 "param unread: UpTo$",
	"Mavinda, Students' Advocate": "UnlessValidTarget$ unread: the ValidTarget$ test is applied un-inverted",
	"Mental Modulation":           "param unread: PlayerTurn$",
	"Officious Interrogation":     "RaiseCost Cost$+Amount$: Cost$ ignored",
	"Planar Gate":                 "param unread: UpTo$",
	"Primitive Justice":           "RaiseCost Cost$+Amount$: Cost$ ignored",
	"Soul Partition":              "AffectedZone$ ignored for spells",
	"Taste of Paradise":           "RaiseCost Cost$+Amount$: Cost$ ignored",
	"Urza's Filter":               "param unread: UpTo$",
}
