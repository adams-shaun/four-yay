package org.mage.test.oracle;

import mage.choices.ChoiceColor;
import mage.choices.ChoiceCreatureType;
import mage.constants.MultiplayerAttackOption;
import mage.constants.Outcome;
import mage.constants.RangeOfInfluence;
import mage.game.Game;
import mage.game.TwoPlayerDuel;
import mage.game.mulligan.MulliganType;
import org.mage.test.player.TestComputerPlayer;
import org.mage.test.player.TestPlayer;

/** Executable check of how the driver queues a scripted colour, without H2 or a
 * full game. Run scripts/xmage-oracle-test-colour-choice.sh. This exercises
 * the exact ChoiceColor answer path with a queue populated as build() does
 * before placing setup permanents; the actual addCard/build integration is
 * covered only by the XMage batch replay, which cannot run in the H2-less seat.
 * With an empty queue TestPlayer picks at random, which flaked replayed rows. */
public final class ScenarioReplayColourChoiceTest {
    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static Game game(TestPlayer p) {
        Game g = new TwoPlayerDuel(MultiplayerAttackOption.LEFT, RangeOfInfluence.ONE,
                MulliganType.GAME_DEFAULT.getMulligan(0), 60, 20, 7);
        g.addPlayer(p, new mage.cards.decks.Deck());
        return g;
    }

    private static String ask(TestPlayer p, Game g) {
        ChoiceColor c = new ChoiceColor(true);
        equal(true, p.choose(Outcome.Benefit, c, g));
        return c.getChoice();
    }

    /** The as-enters "choose a creature type" dialog (ChooseCreatureTypeEffect).
     * A key choice over SubType descriptions; a source is not needed for the
     * choice itself. */
    private static String askCreatureType(TestPlayer p, Game g) {
        ChoiceCreatureType c = new ChoiceCreatureType(g, null, true, "Choose a creature type");
        equal(true, p.choose(Outcome.Benefit, c, g));
        return c.getChoiceKey();
    }

    public static void main(String[] args) {
        // The colour name the generator emits is the key XMage's chooser uses.
        for (String colour : new String[] {"White", "Blue", "Black", "Red", "Green"}) {
            TestPlayer p = new TestPlayer(new TestComputerPlayer("A", RangeOfInfluence.ONE));
            Game g = game(p);
            ScenarioReplay.queueChoice(p, colour);
            equal(1, p.getChoices().size());
            equal(colour, ask(p, g));
            equal(0, p.getChoices().size());
        }
        System.out.println("PASS each colour name answers ChoiceColor and is consumed");

        // Isolate the setup-placement answer path: build() queues this before
        // addCard. This deliberately tests the ChoiceColor consumer, not the
        // addCard integration (the batch replay is the integration check).
        TestPlayer setupPlayer = new TestPlayer(new TestComputerPlayer("Setup", RangeOfInfluence.ONE));
        Game setupGame = game(setupPlayer);
        ScenarioReplay.queueSetupChoices(setupPlayer, "Green");
        equal("Green", ask(setupPlayer, setupGame));
        equal(0, setupPlayer.getChoices().size());
        System.out.println("PASS setup-placement ETB ChoiceColor consumes its scripted answer");

        // Setup ETB answer precedes later step colours in the FIFO queue.
        TestPlayer p = new TestPlayer(new TestComputerPlayer("A", RangeOfInfluence.ONE));
        Game g = game(p);
        ScenarioReplay.queueChoice(p, "Green");
        ScenarioReplay.queueChoice(p, "Red");
        equal("Green", ask(p, g));
        equal("Red", ask(p, g));
        System.out.println("PASS two scripted colours answer two dialogs in order");

        // The skip token maps to TestPlayer's own.
        TestPlayer q = new TestPlayer(new TestComputerPlayer("B", RangeOfInfluence.ONE));
        ScenarioReplay.queueChoice(q, "[choice_skip]");
        equal(TestPlayer.CHOICE_SKIP, q.getChoices().get(0));
        System.out.println("PASS [choice_skip] maps to TestPlayer.CHOICE_SKIP");

        // Patchwork Banner's setup dialog: the emitted subtype name answers
        // the key choice and is consumed, exactly like the colour case. These
        // are the four values gorge's generator emitted on 2026-10-06.
        for (String type : new String[] {"Human", "Elemental", "Dinosaur", "Bear"}) {
            TestPlayer tp = new TestPlayer(new TestComputerPlayer("T", RangeOfInfluence.ONE));
            Game tg = game(tp);
            ScenarioReplay.queueSetupChoices(tp, type);
            equal(type, askCreatureType(tp, tg));
            equal(0, tp.getChoices().size());
        }
        System.out.println("PASS setup-placement ETB ChoiceCreatureType consumes its scripted subtype");

        // A mana colour offered to the creature-type dialog is rejected with
        // the SAME message the Level B rows reported, which is what identifies
        // that dialog as the consumer of the first gameplay answer.
        TestPlayer bad = new TestPlayer(new TestComputerPlayer("Bad", RangeOfInfluence.ONE));
        Game badGame = game(bad);
        ScenarioReplay.queueSetupChoices(bad, "White");
        String badMessage = null;
        try {
            askCreatureType(bad, badGame);
        } catch (IllegalArgumentException e) {
            badMessage = e.getMessage();
        }
        if (badMessage == null || !badMessage.equals("Choice key [White] not found in []")) {
            throw new AssertionError("expected the reported Level B message, got " + badMessage);
        }
        System.out.println("PASS a mana colour is rejected by ChoiceCreatureType with the reported [] message");
    }
}
