package rules

// effect_frame_resume_test.go pins the brief's path 1: an api:Effect's
// ReplaceWith$ body that SUSPENDS on a mid-resolution ask and resumes. The
// frame carrying the Effect-created registration identity onto the resume
// point (rules/resolution.go's resumePoint.effectFrame, published by
// effects.Resolve via the optional effectFrameHost interface) is what lets
// the body's one-shot self-exile idiom
// (`DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$ Exile`)
// still end exactly that registration after the answer -- before this the
// resume rebuilt a Ctx with no EffectFrame and the effect lingered for its
// whole duration.
//
// Corpus measurement (GNU /usr/bin/grep + a Python SVar-chain walk over
// .cards/cardsfolder at the pin): of the 127 LIVE Effect-created
// replacements (DamageDone, or Moved with a PutCounter body -- the only two
// families rules/replacement.go registers), 79 reach the self-exile idiom
// and **0** reach any ask-shaped parameter in the body chain. So no real
// corpus carrier exercises a decision inside an Effect replacement body
// today; the fixture below carries a real corpus SHAPE (the wildgrowth1
// `Event$ Moved | ReplaceWith$ DBPutP1P1` + `SubAbility$` chain) with the
// unless-cost ask the corpus's other Effect bodies use everywhere else.

// effectSelfExileResumeSrc is the live Moved+PutCounter Effect replacement
// shape (torgal_a_fine_hound / communal_brewing / wildgrowth_archaic) whose
// body chain carries a real mid-resolution ask -- an unless-cost on the
// one-shot self-exile line itself. The ask is posed by the shared
// effects.Resolve unless gate before effChangeZone dispatches, so the
// registration must survive the suspension to be ended on resume.
const effectSelfExileResumeSrc = "Name:Fixture Self-Exile Effect\nManaCost:1 U\nTypes:Sorcery\n" +
	"A:SP$ Effect | ReplacementEffects$ ETBCreat | SpellDescription$ x\n" +
	"SVar:ETBCreat:Event$ Moved | Destination$ Battlefield | ValidCard$ Creature | ReplaceWith$ DBPutP1P1 | ReplacementResult$ Updated\n" +
	"SVar:DBPutP1P1:DB$ PutCounter | Defined$ ReplacedCard | CounterType$ P1P1 | ETB$ True | CounterNum$ 1 | SubAbility$ DBExileSelf\n" +
	"SVar:DBExileSelf:DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$ Exile | UnlessCost$ 1 | UnlessPayer$ You\n" +
	"Oracle:x\n"
