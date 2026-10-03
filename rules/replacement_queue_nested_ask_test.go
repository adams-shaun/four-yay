package rules

// A resolving ability that returns several lands at once, each of which meets
// a non-commuting entry competition (Horizon Explorer's enters-untapped body
// against the land's own enters-tapped body), parks one CR 616.1 order ask per
// land on the replChoices queue and suspends its resolution on e.resume. When
// the body chosen for one land ASKS (a shock land's "pay 2 life or it enters
// tapped" UnlessCost) while later lands' competitions are still queued, that
// nested Engine.Ask overwrote e.resume with its own frame; once the nested
// answer completed, the suspended resolution's frame was gone, and the next
// order answer called resumeResolution(nil) -- the botbench panic
// (cavalry-charge vs pro-shaper, seed 7149: Lumra, Bellow of the Woods
// returning Overgrown Tomb, Mirrorpool, Spire Garden, ... under Horizon
// Explorer). The suspended resolution must survive the nested ask and resume
// exactly once, after the last queued competition is answered.

// returnLandsSrc is a Lumra-shaped test-local trigger host: whenever its
// controller draws, return every land card from their graveyard to the
// battlefield tapped, then gain 1 life -- the sub-ability that proves the
// suspended chain's continuation runs exactly once.
const returnLandsSrc = "Name:Land Returner\nTypes:Creature\nPT:1/1\n" +
	"T:Mode$ Drawn | ValidCard$ Card.YouCtrl | TriggerZones$ Battlefield | Execute$ TrigReturn | TriggerDescription$ Whenever you draw a card, return all land cards from your graveyard to the battlefield tapped.\n" +
	"SVar:TrigReturn:DB$ ChangeZoneAll | ChangeType$ Land.YouCtrl | Origin$ Graveyard | Destination$ Battlefield | Tapped$ True | SubAbility$ DBGain\n" +
	"SVar:DBGain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

// shockLandSrc is the shock-land entry shape: an Updated tap body gated on an
// UnlessCost the entering land's controller is asked about.
const shockLandSrc = "Name:Test Shock\nTypes:Land\n" +
	"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ DBTap | ReplacementResult$ Updated | Description$ As CARDNAME enters, you may pay 2 life. If you don't, it enters tapped.\n" +
	"SVar:DBTap:DB$ Tap | ETB$ True | Defined$ Self | UnlessCost$ PayLife<2> | UnlessPayer$ You\nOracle:x\n"
