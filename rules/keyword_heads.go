package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/chars"
)

// kwHead is a compiled keyword head (chars.KW).
type kwHead = chars.KW

func newKWHead(s string) kwHead { return chars.NewKW(s) }

// kwHeadOf is a run-time head (a caller-supplied string) through the
// front-cached interner.
func kwHeadOf(s string) kwHead { return kwHead{S: s, ID: cards.KeywordHeadIDOf(s)} }

// The literal heads the rules hot paths read.
var (
	kwhAfflict          = newKWHead("Afflict")
	kwhAscend           = newKWHead("Ascend")
	kwhBlitz            = newKWHead("Blitz")
	kwhBloodthirst      = newKWHead("Bloodthirst")
	kwhCasualty         = newKWHead("Casualty")
	kwhConspire         = newKWHead("Conspire")
	kwhConvoke          = newKWHead("Convoke")
	kwhCrew             = newKWHead("Crew")
	kwhCumulativeUpkeep = newKWHead("Cumulative upkeep")
	kwhCycling          = newKWHead("Cycling")
	kwhDeathtouch       = newKWHead("Deathtouch")
	kwhDecayed          = newKWHead("Decayed")
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
	kwhHaste            = chars.KWHaste
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
	kwhProwess          = newKWHead("Prowess")
	kwhReach            = newKWHead("Reach")
	kwhRetrace          = newKWHead("Retrace")
	kwhSaddle           = newKWHead("Saddle")
	kwhShadow           = newKWHead("Shadow")
	kwhSkulk            = newKWHead("Skulk")
	kwhSneak            = newKWHead("Sneak")
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
