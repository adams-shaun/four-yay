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
	"Beluna Grandsquall":           "ValidCard$ dead: Permanent base on a spell",
	"Captain Eberhart":             "ValidCard$ dead",
	"Cemetery Prowler":             "Amount$ unevaluable by EvalCountOK",
	"Chandra's Incinerator":        "Amount$ unevaluable by EvalCountOK",
	"Close Encounter":              "RaiseCost Cost$ unmodelled",
	"Cunning Nightbonder":          "ValidCard$ dead",
	"Damping Sphere":               "Relative$ True on RaiseCost (only ReduceCost has an exception)",
	"Doc Aurlock, Grizzled Genius": "ValidSpell$ unsupported: Static.Plotting",
	"Dream Chisel":                 "ValidSpell$ unsupported: Spell.isCastFaceDown",
	"Elite Spellbinder":            "ValidCard$ dead",
	"Exiled Doomsayer":             "ValidSpell$ unsupported: Static.MorphUp",
	"Fast Forward":                 "Amount$ unevaluable by EvalCountOK",
	"Fireball":                     "Relative$ True on RaiseCost (only ReduceCost has an exception)",
	"Flying Drone":                 "SA ReduceAmount$ unread (ReduceCost$ is a mana cost); SA ReduceCost$ unevaluable",
	"Glamdring, Foe-hammer":        "Amount$ unevaluable by EvalCountOK",
	"Gonti, Canny Acquisitor":      "ValidCard$ dead",
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
	"Lightstall Inquisitor":        "ValidCard$ dead",
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
	"Tezzeret, Betrayer of Flesh":  "CheckSVar$ unevaluable",
	"Urianger Augurelt":            "ValidSpell$ unsupported: Spell.MayPlaySource",
	"Warbringer":                   "ValidSpell$ unsupported: Spell.Dash",
	"Zimone, Infinite Analyst":     "ValidCard$ dead",
}

// WRONG: the engine applies the static but ignores or misreads a parameter.
// OFFER-BLIND findings (applied only at the payment-time reprice, never at
// the offer gate) ride this table with an "OFFER-BLIND: " prefix.
var knownWrongCostStatics = map[string]string{
	"Closing Statement":           "param unread: Phases$; param unread: PlayerTurn$",
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
