package pay

import "github.com/adams-shaun/gorge/decision"

// RecordManaCostAnswer records the seat's answer to the mana-cost election
// ask of flow (ManaCostTapStage, ManaCostChoiceStages) on md and advances md
// past the answered part; the engine then resumes the election.
func RecordManaCostAnswer(md *ManaCostActivation, flow AskFlow, chosen []decision.Option) {
	switch flow {
	case AskManaSacrifice:
		for _, opt := range chosen {
			md.Sacs = append(md.Sacs, opt.Obj)
			md.SacPaid++
		}
		part := md.Cost.Sac[md.SacPart]
		if md.SacPaid >= int(part.N) {
			md.SacPart++
			md.SacPaid = 0
		}
	case AskManaDiscard:
		for _, opt := range chosen {
			md.Discards = append(md.Discards, opt.Obj)
		}
		md.Part++
	case AskManaExile:
		for _, opt := range chosen {
			md.Exiles = append(md.Exiles, opt.Obj)
		}
		md.ExilePart++
	case AskManaTap:
		part := md.Cost.TapPermanent[md.TapPart]
		for _, opt := range chosen {
			md.Taps = append(md.Taps, opt.Obj)
		}
		if part.Dyn == "X" {
			// Hazel's Amount$ X is the elected count. Rewrite only this activation
			// copy so it survives the later colour decision without reading an
			// enclosing spell's X or mutating the compiled card ability.
			md.Ability = ManaAbilityWithPaidX(md.Ability, int32(len(chosen)))
		}
		md.TapPart++
	case AskManaForage:
		md.ForageDone = true
		if len(chosen) > 0 && chosen[0].Kind == "forage_food" {
			md.ForagePay = true
			md.ForageFood = chosen[0].Obj
		} else {
			// Exile three: only legal when the graveyard holds three cards (the
			// offer gate read it), so the arm is recorded and the settle takes the
			// top three.
			md.ForagePay = true
		}
	case AskManaUntap:
		for _, opt := range chosen {
			md.Untaps = append(md.Untaps, opt.Obj)
		}
		md.UntapPart++
	}
}
