// Package effects registration marker for the rules-side Saddle implementation.
package effects

// CR 702.171's keyword ability is expanded by cards/kw_saddle.go; its
// tap-power payment is shared rules machinery, and AlterAttribute applies the
// event-backed Saddled designation.
func init() { RegisterNonAPI("kw:Saddle") }
