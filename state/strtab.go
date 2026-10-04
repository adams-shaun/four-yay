package state

import "github.com/adams-shaun/gorge/cards"

// The string tables live in cards (L0, so the IR binder can resolve a
// node's vocabulary codes at load); state re-exports them for the packages
// that may not import cards directly (rules/cost).

// StrEntry is one row of a StrTable.
type StrEntry[T any] = cards.StrEntry[T]

// StrTable is cards.StrTable.
type StrTable[T any] = cards.StrTable[T]

// NameSet is cards.NameSet.
type NameSet = cards.NameSet

// StrCodes is cards.StrCodes.
type StrCodes[T ~uint16] = cards.StrCodes[T]

// NewStrTable is cards.NewStrTable.
func NewStrTable[T any](entries ...StrEntry[T]) StrTable[T] { return cards.NewStrTable(entries...) }

// NewNameSet is cards.NewNameSet.
func NewNameSet(names ...string) NameSet { return cards.NewNameSet(names...) }

// NewStrCodes is cards.NewStrCodes.
func NewStrCodes[T ~uint16](entries ...StrEntry[T]) StrCodes[T] { return cards.NewStrCodes(entries...) }
