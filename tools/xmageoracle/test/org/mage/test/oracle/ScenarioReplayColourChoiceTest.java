package org.mage.test.oracle;

import mage.choices.ChoiceColor;
import mage.constants.MultiplayerAttackOption;
import mage.constants.Outcome;
import mage.constants.RangeOfInfluence;
import mage.game.Game;
import mage.game.TwoPlayerDuel;
import mage.game.mulligan.MulliganType;
import org.mage.test.player.TestComputerPlayer;
import org.mage.test.player.TestPlayer;

/** Executable check of how the driver queues a scripted colour, without H2 or a
 * full game. Run scripts/xmage-oracle-test-colour-choice.sh. A level-B
 * "add one mana of any color" activation and a setup-placed "choose a color"
 * permanent both reach TestPlayer.choose(Outcome, ChoiceColor, Game); with an
 * empty queue TestPlayer picks at random, which flaked six replayed rows. */
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

        // Model build()'s setup placement: queue before addCard can ask the
        // ETB replacement, then prove that exact ChoiceColor dialog consumes
        // the scripted value rather than TestPlayer's random fallback.
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
    }
}
