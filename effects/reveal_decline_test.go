package effects

// The round-2 review finding (fx45): a DECLINED Optional$ hand reveal
// resumed into a MANDATORY reveal_pick. On the reveal_optional resume with
// answer "no", deferToOptionalAsk was false (it only covers the first pass)
// and the pick block ran before the decline `continue` at the bottom of the
// walk, so a player who declined "You may reveal a card from your hand"
// (Dragons Disciple, Vault 21: House Gambit, Temple of the Dragon Queen)
// was then forced to pick a card and reveal it anyway. This file pins the
// decline arm of the pickable shape; the accept arm's pick is pinned in
// effects/infernal_tutor_test.go.
