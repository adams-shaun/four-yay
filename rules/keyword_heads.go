package rules

import "github.com/adams-shaun/gorge/cards"

// kwHead is a keyword head with its interned cards.KeywordHeadID, compiled
// once at package init so a hot keyword read is a bitset test on the face
// rather than a KeywordHead split and case-folded compare per keyword line.
type kwHead struct {
	s  string
	id cards.KeywordHeadID
}

func newKWHead(s string) kwHead { return kwHead{s: s, id: cards.InternKeywordHead(s)} }

// kwHeadOf is a run-time head (a caller-supplied string) through the
// front-cached interner.
func kwHeadOf(s string) kwHead { return kwHead{s: s, id: cards.KeywordHeadIDOf(s)} }

// The literal heads the rules hot paths read.
var (
	kwhAfflict          = newKWHead("Afflict")
	kwhAscend           = newKWHead("Ascend")
	kwhBlitz            = newKWHead("Blitz")
	kwhBloodthirst      = newKWHead("Bloodthirst")
	kwhCasualty         = newKWHead("Casualty")
	kwhConspire         = newKWHead("Conspire")
	kwhConvoke          = newKWHead("Convoke")
	kwhCycling          = newKWHead("Cycling")
	kwhDeathtouch       = newKWHead("Deathtouch")
	kwhDefender         = newKWHead("Defender")
	kwhDelve            = newKWHead("Delve")
	kwhDemonstrate      = newKWHead("Demonstrate")
	kwhDethrone         = newKWHead("Dethrone")
	kwhDoubleStrike     = newKWHead("Double Strike")
	kwhEnchant          = newKWHead("Enchant")
	kwhEnlist           = newKWHead("Enlist")
	kwhEscape           = newKWHead("Escape")
	kwhExploit          = newKWHead("Exploit")
	kwhFear             = newKWHead("Fear")
	kwhFirebending      = newKWHead("Firebending")
	kwhFirstStrike      = newKWHead("First Strike")
	kwhFlash            = newKWHead("Flash")
	kwhFlashback        = newKWHead("Flashback")
	kwhFlying           = newKWHead("Flying")
	kwhForetell         = newKWHead("Foretell")
	kwhHaste            = newKWHead("Haste")
	kwhHorsemanship     = newKWHead("Horsemanship")
	kwhImprovise        = newKWHead("Improvise")
	kwhIndestructible   = newKWHead("Indestructible")
	kwhInfect           = newKWHead("Infect")
	kwhIntimidate       = newKWHead("Intimidate")
	kwhJumpStart        = newKWHead("Jump-start")
	kwhLifelink         = newKWHead("Lifelink")
	kwhMayhem           = newKWHead("Mayhem")
	kwhMenace           = newKWHead("Menace")
	kwhMentor           = newKWHead("Mentor")
	kwhModular          = newKWHead("Modular")
	kwhOffspring        = newKWHead("Offspring")
	kwhPhasing          = newKWHead("Phasing")
	kwhReach            = newKWHead("Reach")
	kwhRetrace          = newKWHead("Retrace")
	kwhShadow           = newKWHead("Shadow")
	kwhSkulk            = newKWHead("Skulk")
	kwhSplitSecond      = newKWHead("Split second")
	kwhStartYourEngines = newKWHead("Start your engines")
	kwhStation          = newKWHead("Station")
	kwhStoried          = newKWHead("Storied")
	kwhSunburst         = newKWHead("Sunburst")
	kwhTraining         = newKWHead("Training")
	kwhTrample          = newKWHead("Trample")
	kwhTypeCycling      = newKWHead("TypeCycling")
	kwhUmbraArmor       = newKWHead("Umbra armor")
	kwhUnleash          = newKWHead("Unleash")
	kwhWebSlinging      = newKWHead("Web-slinging")
	kwhVigilance        = newKWHead("Vigilance")
	kwhWither           = newKWHead("Wither")
)
