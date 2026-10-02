// Package mzplay is the self-play engine behind DraftZero's own training
// loop when gorge stands in for the XMage JVM: it reads the loop's game.yml,
// plays whole games in which both seats search with internal/azmcts on the
// real engine state, records every searched decision in MageZero's training
// format, and labels the records with upstream's TD-lambda value target.
// cmd/mzselfplay is the command; scripts/mzrepro/mzjava is the MZ_JAVA shim
// the loop launches.
//
// It is the counterpart of three upstream classes, pinned at the XMage fork
// 48e4918413: ParallelDataGenerator.java (the game loop, the deck pools, the
// GAME_SUMMARY line, the value labels), ComputerPlayerMCTS2.java (what a
// record is) and GameStateEvaluator3.java (the offline leaf). The package
// reads no clock and imports no network client: the command injects both.
package mzplay
