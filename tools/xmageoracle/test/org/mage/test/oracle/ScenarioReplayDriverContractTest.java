package org.mage.test.oracle;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.UUID;
import java.util.function.BiPredicate;
import mage.abilities.keyword.SpreeAbility;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.l.LightningBolt;
import mage.cards.o.OneLastJob;
import mage.constants.Rarity;
import mage.constants.Zone;
import org.mage.test.player.TestPlayer;

/** No database or AI: executable contract checks on the driver helpers that
 * answer an XMage attach ask, map a move step's zone and detect Spree. Actual
 * chooser/strict replay still belongs to the host replay gate. */
public final class ScenarioReplayDriverContractTest {
    private static final String SKIP = TestPlayer.TARGET_SKIP;

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static UUID id(int n) {
        return new UUID(0, n);
    }

    /** The driver's attach-ask decision, reached the same way
     * OptionalTargetSkipsTest reaches hideAfterNextSkip: reflection into the
     * private ScriptedChoicePlayer so the production method is what runs. */
    @SuppressWarnings("unchecked")
    private static UUID attachmentChoice(List<String> queue, List<UUID> candidates,
            BiPredicate<UUID, String> matches) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("attachmentChoice", List.class, List.class, BiPredicate.class);
        method.setAccessible(true);
        return (UUID) method.invoke(null, queue, candidates, matches);
    }

    private static void attachments() throws Exception {
        UUID bears = id(1), elves = id(2);
        BiPredicate<UUID, String> byId = (candidate, answer) -> answer.equals(candidate.toString());
        List<UUID> both = Arrays.asList(bears, elves);

        // A scripted answer that names the non-first candidate is honored and
        // consumed, not overridden by an alphabetical or first-candidate pick.
        List<String> queue = new ArrayList<>(Arrays.asList(elves.toString()));
        check(elves.equals(attachmentChoice(queue, both, byId)),
                "scripted second candidate was not honored");
        check(queue.isEmpty(), "scripted attachment answer was not consumed");
        // Precondition: the two candidates really are distinct answers.
        check(!bears.equals(elves), "candidates must differ");

        // Successive asks: the first consumes the scripted answer, the second
        // (now unscripted and uniquely determined) auto-selects without a
        // stale answer resurfacing.
        List<String> successive = new ArrayList<>(Arrays.asList(elves.toString()));
        check(elves.equals(attachmentChoice(successive, both, byId)), "first of successive asks failed");
        check(bears.equals(attachmentChoice(successive, Arrays.asList(bears), byId)),
                "second successive ask did not auto-select its unique candidate");

        // Unscripted, uniquely determined: automatic selection.
        check(bears.equals(attachmentChoice(new ArrayList<>(), Arrays.asList(bears), byId)),
                "unscripted unique candidate was not auto-selected");

        // Unscripted, ambiguous: the driver must not guess; the base player
        // answers (and its strict unused-command check still applies).
        check(attachmentChoice(new ArrayList<>(), both, byId) == null,
                "ambiguous unscripted attach ask was auto-selected");

        // A scripted answer that names no candidate is left queued so the
        // strict replay reports the scenario error rather than hiding it.
        List<String> unmatched = new ArrayList<>(Arrays.asList("@not-a-candidate"));
        check(attachmentChoice(unmatched, both, byId) == null, "unmatched answer accepted");
        check(unmatched.equals(Arrays.asList("@not-a-candidate")),
                "unmatched scripted answer was consumed or altered");

        // A leading skip is the base's optional-skip semantics; fall through.
        List<String> skipped = new ArrayList<>(Arrays.asList(SKIP, bears.toString()));
        check(attachmentChoice(skipped, Arrays.asList(bears), byId) == null,
                "a leading skip must fall through, not auto-select");
        check(skipped.equals(Arrays.asList(SKIP, bears.toString())),
                "a leading skip was consumed by the attach branch");
        System.out.println("PASS attach-ask choice (scripted first/non-first, consume, successive, "
                + "unique auto-select, ambiguous, unmatched, skip)");
    }

    private static void zones() {
        check(ScenarioReplay.moveDestination("exile") == Zone.EXILED,
                "gorge's lowercase exile did not map to XMage's EXILED");
        check(ScenarioReplay.moveDestination("graveyard") == Zone.GRAVEYARD, "graveyard mapping");
        check(ScenarioReplay.moveDestination("hand") == Zone.HAND, "hand mapping");
        check(ScenarioReplay.moveDestination("battlefield") == Zone.BATTLEFIELD, "battlefield mapping");
        // Precondition: EXILED is not EXILE (the enum member whose absence the
        // mapping exists for); a valueOf("exile") would in fact throw.
        boolean threw = false;
        try {
            Zone.valueOf("exile");
        } catch (IllegalArgumentException expected) {
            threw = true;
        }
        check(threw, "precondition: Zone.valueOf(\"exile\") should not resolve");
        System.out.println("PASS move destination mapping (exile->EXILED and other zones)");
    }

    private static CardSetInfo info(String name) {
        return new CardSetInfo(name, "TEST", "1", Rarity.RARE);
    }

    private static void spree() {
        // The structural check the cast step uses must be exact on the actual
        // XMage card models, not a name list.
        Card spree = new OneLastJob(UUID.randomUUID(), info("One Last Job"));
        Card plain = new LightningBolt(UUID.randomUUID(), info("Lightning Bolt"));
        check(spree.getAbilities().containsClass(SpreeAbility.class),
                "precondition: OneLastJob does not carry a SpreeAbility");
        check(!plain.getAbilities().containsClass(SpreeAbility.class),
                "precondition: LightningBolt carries a SpreeAbility");
        check(ScenarioReplay.isSpreeCard(spree), "a Spree card was not detected structurally");
        check(!ScenarioReplay.isSpreeCard(plain), "a non-Spree card was detected as Spree");
        check(!ScenarioReplay.isSpreeCard(null), "a null card was detected as Spree");
        System.out.println("PASS Spree detection (SpreeAbility on the card, not a name list)");
    }

    public static void main(String[] args) throws Exception {
        attachments();
        zones();
        spree();
    }
}
