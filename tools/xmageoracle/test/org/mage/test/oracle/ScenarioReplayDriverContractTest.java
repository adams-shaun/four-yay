package org.mage.test.oracle;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.UUID;
import java.util.function.BiPredicate;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import mage.abilities.keyword.SpreeAbility;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.a.AresGodOfWar;
import mage.cards.g.GrizzlyBears;
import mage.cards.l.LightningBolt;
import mage.cards.o.OneLastJob;
import mage.cards.r.RedHerring;
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

    private static boolean isAttachmentChoice(mage.target.Target target) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("isAttachmentChoice", mage.target.Target.class);
        method.setAccessible(true);
        return (Boolean) method.invoke(null, target);
    }

    private static void attachments() throws Exception {
        mage.target.TargetPermanent ordinary = new mage.target.TargetPermanent();
        mage.target.TargetPermanent unrelatedChoice = new mage.target.TargetPermanent();
        unrelatedChoice.withNotTarget(true);
        mage.target.TargetPermanent attachAsk = new mage.target.TargetPermanent(
                new mage.filter.FilterPermanent("a creature you control that Abduction can be attached to"));
        attachAsk.withNotTarget(true);
        check(!isAttachmentChoice(ordinary),
                "ordinary targeted permanent ask was classified as attachment choice");
        check(!isAttachmentChoice(unrelatedChoice),
                "unrelated non-targeting permanent ask was classified as attachment choice");
        check(isAttachmentChoice(attachAsk),
                "unhinted non-targeting permanent attach ask was not recognized");

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

        // The `move` op must find its source card in any Card-bearing zone, not
        // only the hand: the next generator shape that moves a card already in
        // a graveyard/library/exile must not throw. Hand stays first so today's
        // generated hand -> zone move is unchanged, and a battlefield permanent
        // (a Permanent, not a Card) is deliberately absent.
        List<Zone> sources = ScenarioReplay.moveSourceZones();
        check(sources.get(0) == Zone.HAND, "move must search the hand first; got " + sources);
        for (Zone expected : Arrays.asList(Zone.HAND, Zone.GRAVEYARD, Zone.LIBRARY, Zone.EXILED)) {
            check(sources.contains(expected), "move source zones omit " + expected + ": " + sources);
        }
        check(!sources.contains(Zone.BATTLEFIELD),
                "a battlefield permanent is not a Card move source; got " + sources);
        System.out.println("PASS move destination mapping (exile->EXILED) and multi-zone source search");
    }

    private static JsonArray steps(String json) {
        return JsonParser.parseString(json).getAsJsonArray();
    }

    private static boolean rejects(JsonArray steps, int index) {
        try {
            ScenarioReplay.passAction(steps, index);
            return false;
        } catch (IllegalArgumentException expected) {
            return true;
        }
    }

    private static void setField(Object target, String name, Object value) throws Exception {
        for (Class<?> c = target.getClass(); c != null; c = c.getSuperclass()) {
            try {
                java.lang.reflect.Field f = c.getDeclaredField(name);
                f.setAccessible(true);
                f.set(target, value);
                return;
            } catch (NoSuchFieldException next) {
                // keep climbing
            }
        }
        throw new AssertionError("no field " + name);
    }

    /** A driver with two bare players: no game, so only queueing runs. */
    private static ScenarioReplay queueingDriver(JsonArray steps, TestPlayer a, TestPlayer b) throws Exception {
        ScenarioReplay r = new ScenarioReplay();
        JsonObject sc = new JsonObject();
        sc.add("steps", steps);
        setField(r, "playerA", a);
        setField(r, "playerB", b);
        setField(r, "sc0", sc);
        return r;
    }

    private static TestPlayer bare(String name) {
        return new TestPlayer(new org.mage.test.player.TestComputerPlayer(name, mage.constants.RangeOfInfluence.ALL));
    }

    private static List<String> names(TestPlayer p) {
        List<String> out = new ArrayList<>();
        for (org.mage.test.player.PlayerAction a : p.getActions()) {
            out.add(a.getAction().startsWith("waitStackResolved") ? a.getAction() : a.getActionName());
        }
        return out;
    }

    /** What the replay loop does for one step: its pass commands, the step
     * itself, then its checkpoint (the loop's order is pinned by
     * driver_levelb_pass_test.go). */
    private static void loopStep(ScenarioReplay r, TestPlayer a, JsonArray steps, int i) throws Exception {
        String op = steps.get(i).getAsJsonObject().get("op").getAsString();
        queue(r, "queuePassCommands", i);
        if (op.equals("pass")) {
            queue(r, "step", steps.get(i).getAsJsonObject(), op, i);
        }
        r.runCode("step " + i + " (" + op + ")", 1, mage.constants.PhaseStep.PRECOMBAT_MAIN, a, (info, p, g) -> { });
    }

    private static void queue(ScenarioReplay r, String method, Object... args) throws Exception {
        for (Method m : ScenarioReplay.class.getDeclaredMethods()) {
            if (m.getName().equals(method) && m.getParameterCount() == args.length) {
                m.setAccessible(true);
                m.invoke(r, args);
                return;
            }
        }
        throw new AssertionError("no method " + method);
    }

    private static void passes() throws Exception {
        JsonArray pair = steps("[{op:'pass',seat:0},{op:'pass',seat:1}]");
        check(ScenarioReplay.passAction(pair, 0) == ScenarioReplay.PASS_PAIR_FIRST, "first pass of a pair");
        check(ScenarioReplay.passAction(pair, 1) == ScenarioReplay.PASS_PAIR_SECOND, "second pass of a pair");
        JsonArray handoff = steps("[{op:'pass',seat:0},{op:'cast',seat:1,card:'p1:Lightning Bolt'}]");
        check(ScenarioReplay.passAction(handoff, 0) == ScenarioReplay.PASS_HANDOFF, "lone p0 pass before a p1 cast");
        // Every other shape fails loudly rather than being guessed at.
        check(rejects(steps("[{op:'pass',seat:0}]"), 0), "a lone trailing pass must be rejected");
        check(rejects(steps("[{op:'pass',seat:0},{op:'resolve'}]"), 0), "a pass before a non-cast must be rejected");
        check(rejects(steps("[{op:'pass',seat:1},{op:'pass',seat:0}]"), 0), "a p1-first pair must be rejected");
        check(rejects(steps("[{op:'pass',seat:0},{op:'pass',seat:0}]"), 0), "a same-seat pair must be rejected");
        check(rejects(steps("[{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass',seat:0}]"), 1),
                "a three-pass run must be rejected");

        // Queue order on the active player (the checkpoint owner). Cast and
        // pass pair: the spell is on the stack at the first-pass checkpoint,
        // and the one-object resolution sits after it, before the second.
        JsonArray cast = steps("[{op:'cast',seat:0},{op:'pass',seat:0},{op:'pass',seat:1}]");
        TestPlayer a = bare("A");
        TestPlayer b = bare("B");
        ScenarioReplay r = queueingDriver(cast, a, b);
        queue(r, "queuePassCommands", 0);
        check(names(a).isEmpty(), "a cast step queues no pass command; got " + names(a));
        loopStep(r, a, cast, 1);
        check(names(a).equals(Arrays.asList("step 1 (pass)")),
                "first pass must queue only its checkpoint (no early resolution); got " + names(a));
        loopStep(r, a, cast, 2);
        check(names(a).equals(Arrays.asList("step 1 (pass)", ScenarioReplay.CMD_REQUIRE_STACK,
                        ScenarioReplay.CMD_WAIT_RESOLVE_ONE, "step 2 (pass)")),
                "second pass must queue the one-object resolution before its checkpoint; got " + names(a));
        check(names(b).isEmpty(), "the pair queues nothing for the opponent; got " + names(b));
        check(a.getActions().get(2).getAction().equals("waitStackResolved:1"),
                "the resolution must wait for exactly one stack object");

        // The draw and event recipes append the pair before a pass_to (the
        // scry/surveil answer), so the pair is not the scenario's tail. Its
        // classification and queue order must not depend on what follows it.
        JsonArray pairTail = steps("[{op:'cast',seat:0},{op:'pass',seat:0},{op:'pass',seat:1},{op:'pass_to',decision:'priority'}]");
        check(ScenarioReplay.passAction(pairTail, 1) == ScenarioReplay.PASS_PAIR_FIRST,
                "the first pass of a pass_to-suffixed pair");
        check(ScenarioReplay.passAction(pairTail, 2) == ScenarioReplay.PASS_PAIR_SECOND,
                "the second pass of a pass_to-suffixed pair");
        TestPlayer a3 = bare("A");
        ScenarioReplay r3 = queueingDriver(pairTail, a3, bare("B"));
        loopStep(r3, a3, pairTail, 1);
        check(names(a3).equals(Arrays.asList("step 1 (pass)")),
                "a pass_to-suffixed pair's first pass must queue only its checkpoint; got " + names(a3));
        loopStep(r3, a3, pairTail, 2);
        check(names(a3).equals(Arrays.asList("step 1 (pass)", ScenarioReplay.CMD_REQUIRE_STACK,
                        ScenarioReplay.CMD_WAIT_RESOLVE_ONE, "step 2 (pass)")),
                "a pass_to-suffixed pair's second pass must queue the one-object resolution before its checkpoint; got " + names(a3));

        // Lone p0 pass then p1 cast: p0 yields priority between the pass
        // checkpoint and the opponent's cast checkpoint.
        TestPlayer a2 = bare("A");
        ScenarioReplay r2 = queueingDriver(handoff, a2, bare("B"));
        loopStep(r2, a2, handoff, 0);
        check(names(a2).equals(Arrays.asList("step 0 (pass)")), "the handoff pass queues only its checkpoint; got " + names(a2));
        queue(r2, "queuePassCommands", 1); // the cast step's own pre-step hook
        check(names(a2).equals(Arrays.asList("step 0 (pass)", ScenarioReplay.CMD_YIELD_PRIORITY)),
                "priority must be yielded before the opponent's cast checkpoint; got " + names(a2));

        // Passes need seat 0 to be the active player.
        JsonArray turn2 = steps("[{op:'pass',seat:0},{op:'pass',seat:1}]");
        boolean threw = false;
        try {
            ScenarioReplay.passCommands(turn2, 1, 1);
        } catch (IllegalArgumentException expected) {
            threw = true;
        }
        check(threw, "a pass on seat 1's turn must be rejected");
        System.out.println("PASS pass mapping and queue order (pair resolves after its second pass; "
                + "lone p0 pass yields before the p1 cast checkpoint; other shapes rejected)");
    }

    private static void mustAttack() {
        // The structural check the attack step uses must be exact on the
        // actual XMage card models, not a name list: a card that carries
        // AttacksEachCombatStaticAbility is declared and tapped by XMage's
        // own checkAttackRequirements, so its attack() command is never
        // consumed. Ares, God of War and Red Herring carry it; a plain
        // creature does not.
        Card ares = new AresGodOfWar(UUID.randomUUID(), info("Ares, God of War"));
        Card herring = new RedHerring(UUID.randomUUID(), info("Red Herring"));
        Card bears = new GrizzlyBears(UUID.randomUUID(), info("Grizzly Bears"));
        check(ScenarioReplay.isMustAttackCard(ares), "Ares, God of War was not detected as a must-attack card");
        check(ScenarioReplay.isMustAttackCard(herring), "Red Herring was not detected as a must-attack card");
        check(!ScenarioReplay.isMustAttackCard(bears), "Grizzly Bears was detected as a must-attack card");
        check(!ScenarioReplay.isMustAttackCard(null), "a null card was detected as must-attack");
        System.out.println("PASS must-attack detection (AttacksEachCombatStaticAbility on the card, not a name list)");
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

    private static String castModeSupported(String mode) throws Exception {
        Class<?> replay = ScenarioReplay.class;
        java.lang.reflect.Method m = replay.getDeclaredMethod("castModeSupported", String.class);
        m.setAccessible(true);
        return String.valueOf(m.invoke(null, mode));
    }

    @SuppressWarnings("unchecked")
    private static boolean isSacrificeChoice(mage.target.Target target) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("isSacrificeChoice", mage.target.Target.class);
        method.setAccessible(true);
        return (Boolean) method.invoke(null, target);
    }

    @SuppressWarnings("unchecked")
    private static Object optionalAdditionalCostAnswer(boolean bargained, List<String> choices) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("optionalAdditionalCostAnswer", boolean.class, List.class);
        method.setAccessible(true);
        return method.invoke(null, bargained, choices);
    }

    @SuppressWarnings("unchecked")
    private static UUID costPickChoice(List<String> picks, List<UUID> candidates,
            BiPredicate<UUID, String> matches) throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("costPickChoice", List.class, List.class, BiPredicate.class);
        method.setAccessible(true);
        return (UUID) method.invoke(null, picks, candidates, matches);
    }

    private static void bargain() throws Exception {
        // cast_mode acceptance: the driver elects Bargain and main's
        // "optionalcost" cast, an absent mode is the ordinary cast, and
        // anything else is rejected loudly (previously EVERY cast_mode was
        // rejected, before its cost was paid). The "optionalcost" check is
        // the regression guard: the reference commit's first cut dropped it.
        check(castModeSupported("").equals("true"), "an absent cast_mode must be the ordinary cast");
        check(castModeSupported("bargained").equals("true"), "bargained cast_mode was not accepted");
        check(castModeSupported("optionalcost").equals("true"),
                "optionalcost cast_mode was not accepted");
        check(castModeSupported("kicked").equals("false"), "kicked cast_mode must stay unsupported");
        check(castModeSupported("adventure_alt").equals("false"),
                "an unwired cast_mode must be rejected, not cast at face value");

        // The sacrifice ask is recognized structurally by its "to sacrifice"
        // hint (what TargetSacrifice carries), not by a card name.
        mage.target.TargetPermanent sacrifice = new mage.target.common.TargetSacrifice(
                new mage.filter.common.FilterControlledPermanent("an artifact, enchantment, or token"));
        mage.target.TargetPermanent ordinary = new mage.target.TargetPermanent();
        mage.target.TargetPermanent attach = new mage.target.TargetPermanent();
        attach.withChooseHint("to attach to a creature you control");
        check(isSacrificeChoice(sacrifice), "a TargetSacrifice ask was not recognized");
        check(!isSacrificeChoice(ordinary), "an ordinary permanent ask was classified as a sacrifice");
        check(!isSacrificeChoice(attach), "an attach ask was classified as a sacrifice");
        check(!isSacrificeChoice(null), "a null ask was classified as a sacrifice");

        // The optional-additional-cost yes/no: a bargained cast pays (the ask
        // is exactly what kicker/offspring/waterbend pose), a scripted "No" is
        // consumed so strict mode sees it used, anything else declines.
        Object pay = optionalAdditionalCostAnswer(true, new ArrayList<>());
        Object consume = optionalAdditionalCostAnswer(false, new ArrayList<>(Arrays.asList("No")));
        Object decline = optionalAdditionalCostAnswer(false, new ArrayList<>());
        check(pay.toString().equals("PAY"), "a bargained cast did not PAY the optional additional cost");
        check(consume.toString().equals("CONSUME"), "a scripted No was not consumed");
        check(decline.toString().equals("DECLINE"), "an unscripted ask was not declined");
        // Preconditions: the three outcomes really differ, and the elected
        // mode wins over a stale scripted No.
        check(!pay.toString().equals(consume.toString()) && !consume.toString().equals(decline.toString()),
                "the three optional-cost outcomes must differ");
        check(optionalAdditionalCostAnswer(true, new ArrayList<>(Arrays.asList("No"))).toString().equals("PAY"),
                "the elected mode must win over a scripted No");

        // The sacrifice object comes from the cast's recorded choose picks.
        UUID opter = id(11), relic = id(12);
        BiPredicate<UUID, String> byId = (candidate, answer) -> answer.equals(candidate.toString());
        List<UUID> both = Arrays.asList(opter, relic);
        List<String> picks = new ArrayList<>(Arrays.asList(relic.toString()));
        check(relic.equals(costPickChoice(picks, both, byId)), "the recorded sacrifice pick was not honored");
        check(costPickChoice(new ArrayList<>(), both, byId) == null,
                "a missing pick must not be guessed at");
        List<String> unmatched = new ArrayList<>(Arrays.asList("@nope"));
        check(costPickChoice(unmatched, both, byId) == null, "an unmatched pick was accepted");
        System.out.println("PASS Bargain election (cast_mode acceptance, sacrifice recognition, "
                + "pay/consume/decline, recorded sacrifice pick)");
    }

    /** The split/Room spellings the contract pins, both engines' sides of
     * one DSK table: gorge spells every card object by its front face and
     * casts a face by its half's ability name; XMage stores the whole
     * "A // B" card object and its back-half spell for a face-1 cast. */
    private static void spellings() {
        String front = "Dazzling Theater", whole = "Dazzling Theater // Prop Room";
        check(ScenarioReplay.frontHalf(whole).equals(front), "front half of a split name");
        check(ScenarioReplay.backHalf(whole).equals("Prop Room"), "back half of a split name");
        check(ScenarioReplay.frontHalf(front).equals(front) && ScenarioReplay.backHalf(front).equals(front),
                "a non-split name is its own halves (precondition)");

        // A whole-name cast of a probe Room (not the card under test, so
        // xmageName is empty) must reach XMage's front-half "Cast <half>"
        // ability, not the whole name that found no ability.
        check(ScenarioReplay.castSpellingRule(front, "", whole).equals(front),
                "probe whole-name cast did not map to the front half");
        // The card under test keeps its two existing spellings.
        check(ScenarioReplay.castSpellingRule(front, whole, front).equals(front),
                "under-test front-name cast lost its spelling");
        check(ScenarioReplay.castSpellingRule(front, whole, whole).equals(front),
                "under-test whole-name cast did not map to the front half");
        // An ordinary name falls through to xmageSpelling (null).
        check(ScenarioReplay.castSpellingRule("", "", "Grizzly Bears") == null,
                "an ordinary cast name was consumed by the split rule");

        // Snapshot values: XMage's whole object name for a probe (xmageName
        // empty) and for the under-test card both read as the front; the
        // under-test card's back half -- the face-1 cast's spell name --
        // reads as the front too; ordinary names pass through.
        check(ScenarioReplay.gorgeSpellingRule(front, "", whole).equals(front),
                "probe whole object name did not rewrite to the front half");
        check(ScenarioReplay.gorgeSpellingRule(front, whole, whole).equals(front),
                "under-test whole object name did not rewrite to the front");
        check(ScenarioReplay.gorgeSpellingRule(front, whole, "Prop Room").equals(front),
                "face-1 cast spell name (back half) did not rewrite to the front");
        check(ScenarioReplay.gorgeSpellingRule(front, whole, "Grizzly Bears").equals("Grizzly Bears"),
                "an ordinary snapshot name was rewritten");
        // Precondition: the back half and the front really are distinct names.
        check(!front.equals("Prop Room"), "halves must differ for the alias to mean anything");
        System.out.println("PASS split/Room spellings (probe whole-name cast, back-half spell alias, "
                + "whole object name rewrite, pass-through)");
    }

    public static void main(String[] args) throws Exception {
        attachments();
        zones();
        passes();
        spree();
        mustAttack();
        bargain();
        spellings();
    }
}
