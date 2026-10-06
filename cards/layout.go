package cards

// The AlternateMode values whose back face is a battlefield-only state: a
// transforming double-faced card (CR 712) and a modal double-faced card
// (CR 712.4d) are front face up in every zone but the battlefield. Named so a
// reader compares against an identifier, not a repeated literal.
const (
	AlternateDoubleFaced = "DoubleFaced"
	AlternateModal       = "Modal"
)

// BackFaceIsBattlefieldOnly reports whether c is a two-faced card of either
// layout above, i.e. one whose back face exists only on the battlefield.
func (c *Card) BackFaceIsBattlefieldOnly() bool {
	return c != nil && len(c.Faces) == 2 &&
		(c.AlternateMode == AlternateDoubleFaced || c.AlternateMode == AlternateModal)
}

// IsTransformingDFC reports whether c is a transforming double-faced card
// (AlternateMode:DoubleFaced with both faces), CR 712.
func (c *Card) IsTransformingDFC() bool {
	return c != nil && len(c.Faces) == 2 && c.AlternateMode == AlternateDoubleFaced
}
