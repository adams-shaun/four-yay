package rules

import costvocab "github.com/adams-shaun/gorge/rules/cost"

// This file is package rules' one bridge to the cost vocabulary leaf,
// rules/cost (lasagna spec W5 step E1). The types are aliases, so a
// rules.Cost IS a cost.Cost, and every function below is a one-line
// forwarder the compiler inlines: rules keeps its historical names and its
// call sites, while the definitions live in the leaf that holds no *Engine.
//
// The import is named costvocab because "cost" is one of the most common
// local variable names in this package. Add a forwarder here only for a
// name rules actually calls; payment-side code (pip expansion, resolveMana)
// stays in rules until rules/pay (E7).

type (
	// Cost is the parsed cost (cost.Cost).
	Cost = costvocab.Cost
	// CostPart is one non-mana cost component (cost.CostPart).
	CostPart = costvocab.CostPart
	// ManaPair is a two-face hybrid symbol (cost.ManaPair).
	ManaPair = costvocab.ManaPair
	// Twobrid is a monocolour hybrid symbol (cost.Twobrid).
	Twobrid = costvocab.Twobrid
	// HybridPhyrexian is a three-part hybrid-Phyrexian symbol
	// (cost.HybridPhyrexian).
	HybridPhyrexian = costvocab.HybridPhyrexian

	costTokenIter = costvocab.TokenIter
)

// pipLetters is the fixed exact-colour pip order (cost.PipLetters).
var pipLetters = costvocab.PipLetters

// ParseCost parses a Forge or oracle cost string (cost.ParseCost).
func ParseCost(s string) Cost { return costvocab.ParseCost(s) }

// ParseUnlessCost strictly parses an UnlessCost$ value
// (cost.ParseUnlessCost).
func ParseUnlessCost(s string) (Cost, bool) { return costvocab.ParseUnlessCost(s) }

func newCostTokenIter(s string) costTokenIter { return costvocab.NewTokenIter(s) }

func addClampedGeneric(v int32, n int64) int32 { return costvocab.AddClampedGeneric(v, n) }

func matchWaterbend(sym string) (string, bool) { return costvocab.MatchWaterbend(sym) }

func matchPayLife(sym string) (string, bool) { return costvocab.MatchPayLife(sym) }

func isDigitRun(s string) bool { return costvocab.IsDigitRun(s) }

func millCostTotal(parts []CostPart) (int64, bool) { return costvocab.MillCostTotal(parts) }

func dynTapParts(c Cost) []CostPart { return costvocab.DynTapParts(c) }

func costCarriesDynTap(c Cost) bool { return costvocab.CostCarriesDynTap(c) }

func withoutDynTaps(c Cost) Cost { return costvocab.WithoutDynTaps(c) }

func costAnnouncesCastX(c Cost) bool { return costvocab.CostAnnouncesCastX(c) }

func isWholeHandRevealSpec(spec string) bool { return costvocab.IsWholeHandRevealSpec(spec) }

func isSameColorRevealSpec(spec string) bool { return costvocab.IsSameColorRevealSpec(spec) }

func isWholeZoneExileSpec(spec string) bool { return costvocab.IsWholeZoneExileSpec(spec) }

func subCounterTargetsSource(target string) bool { return costvocab.SubCounterTargetsSource(target) }

func formatCost(c Cost) string { return costvocab.FormatCost(c) }

func costPhrase(c Cost) string { return costvocab.CostPhrase(c) }

func objectPhrase(part CostPart, defNoun string) string { return costvocab.ObjectPhrase(part, defNoun) }

func specNoun(spec, defNoun string) string { return costvocab.SpecNoun(spec, defNoun) }

func capitaliseFirst(s string) string { return costvocab.CapitaliseFirst(s) }

func manaCostBeyondTap(c Cost) bool { return costvocab.ManaCostBeyondTap(c) }
