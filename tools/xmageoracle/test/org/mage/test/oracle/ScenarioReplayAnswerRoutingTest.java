package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.UUID;
import mage.cards.CardSetInfo;
import mage.constants.PhaseStep;
import mage.constants.Rarity;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Answer-routing checks of the real driver, without H2 or a game. Run
 * scripts/xmage-oracle-test-split-room.sh. A host replay (scripts/driver_replay_batch.sh)
 * is still what proves XMage consumes these answers. */
public final class ScenarioReplayAnswerRoutingTest {
    /** Records every queue write as "<seat>:<queue>:<value>" and every cast. */
    public static final class RecordingDriver extends ScenarioReplay {
        List<String> queues;
        List<String> casts;

        private String seatName(TestPlayer p) throws Exception {
            return p == field(this, "playerA") ? "A" : p == field(this, "playerB") ? "B" : "?";
        }

        private void record(TestPlayer p, String queue, String value) {
            try {
                queues.add(seatName(p) + ":" + queue + ":" + value);
            } catch (Exception e) {
                throw new IllegalStateException(e);
            }
        }

        @Override
        public void addTarget(TestPlayer player, String target) {
            record(player, "target", target);
        }

        @Override
        public void setChoice(TestPlayer player, String choice) {
            record(player, "choice", choice);
        }

        @Override
        public void setChoice(TestPlayer player, boolean choice) {
            record(player, "choice", String.valueOf(choice));
        }

        @Override
        public void setModeChoice(TestPlayer player, String choice) {
            record(player, "mode", choice);
        }

        @Override
        public void setChoiceAmount(TestPlayer player, int... amounts) {
            record(player, "amount", String.valueOf(amounts[0]));
        }

        @Override
        public void castSpell(int turnNum, PhaseStep step, TestPlayer player, String cardName) {
            casts.add(cardName);
        }
    }

    private static void equal(Object want, Object got) {
        if (!java.util.Objects.equals(want, got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static Field fieldOf(Class<?> type, String name) throws Exception {
        for (Class<?> c = type; c != null; c = c.getSuperclass()) {
            try {
                Field f = c.getDeclaredField(name);
                f.setAccessible(true);
                return f;
            } catch (NoSuchFieldException e) {
                // keep climbing: playerA/playerB live in MageTestPlayerBase
            }
        }
        throw new NoSuchFieldException(name);
    }

    private static Object field(Object o, String name) throws Exception {
        return fieldOf(o.getClass(), name).get(o);
    }

    private static void set(Object o, String name, Object value) throws Exception {
        fieldOf(o.getClass(), name).set(o, value);
    }

    private static final Unsafe UNSAFE = unsafe();

    private static Unsafe unsafe() {
        try {
            Field f = Unsafe.class.getDeclaredField("theUnsafe");
            f.setAccessible(true);
            return (Unsafe) f.get(null);
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException(e);
        }
    }

    /** A driver that skips the H2-backed constructor; seats are distinct stubs. */
    private static RecordingDriver driver() throws Exception {
        RecordingDriver d = (RecordingDriver) UNSAFE.allocateInstance(RecordingDriver.class);
        d.queues = new ArrayList<>();
        d.casts = new ArrayList<>();
        TestPlayer a = (TestPlayer) UNSAFE.allocateInstance(TestPlayer.class);
        TestPlayer b = (TestPlayer) UNSAFE.allocateInstance(TestPlayer.class);
        set(a, "choices", new ArrayList<String>());
        set(b, "choices", new ArrayList<String>());
        set(d, "playerA", a);
        set(d, "playerB", b);
        set(d, "gorgeName", "");
        set(d, "xmageName", "");
        set(d, "cast", new ArrayList<String>());
        set(d, "adjustedCasts", new java.util.HashSet<String>());
        LinkedHashMap<String, String> alias = new LinkedHashMap<>();
        alias.put("p1:Grizzly Bears", "@p1:Grizzly Bears");
        set(d, "refAlias", alias);
        return d;
    }

    private static final Class<?>[] ROUTE_TYPES = {int.class, PhaseStep.class, TestPlayer.class, String.class,
            List.class, mage.abilities.Ability.class};

    private static boolean route(RecordingDriver d, String card, List<String> targets, mage.abilities.Ability a)
            throws Exception {
        Method m = ScenarioReplay.class.getDeclaredMethod("castQueuedTargets", ROUTE_TYPES);
        m.setAccessible(true);
        return (Boolean) m.invoke(d, 1, PhaseStep.PRECOMBAT_MAIN, field(d, "playerA"), card, targets, a);
    }

    private static JsonArray answers(String... kindValue) {
        JsonArray as = new JsonArray();
        for (int i = 0; i < kindValue.length; i += 2) {
            JsonObject a = new JsonObject();
            a.addProperty("seat", 0);
            a.addProperty("kind", kindValue[i]);
            a.addProperty("value", kindValue[i + 1]);
            as.add(a);
        }
        return as;
    }

    private static void scripted(RecordingDriver d, JsonArray as) throws Exception {
        Method m = ScenarioReplay.class.getDeclaredMethod("scripted", JsonArray.class);
        m.setAccessible(true);
        m.invoke(d, as);
    }

    /** Calls the driver's setup-answer queueing on a scenario whose first
     * xmage_answers entry is the given array (the pre-build setup queue). */
    private static void queueSetup(RecordingDriver d, JsonArray first) throws Exception {
        JsonObject sc = new JsonObject();
        JsonArray xans = new JsonArray();
        xans.add(first);
        sc.add("xmage_answers", xans);
        Method m = ScenarioReplay.class.getDeclaredMethod("queueSetupChoices", JsonObject.class);
        m.setAccessible(true);
        m.invoke(d, sc);
    }

    public static void main(String[] args) throws Exception {
        // Twin Bolt: a lone recipient of TargetAnyTargetAmount(2). The inline
        // $target= form leaves "selected 1 of 2", so the cast is queued with
        // the whole amount, through the same castQueuedTargets the cast step's
        // first branch calls.
        mage.cards.t.TwinBolt twinBolt = new mage.cards.t.TwinBolt(UUID.randomUUID(),
                new CardSetInfo("Twin Bolt", "TDM", "1", Rarity.COMMON));
        // Preconditions: the ability really is a divided, non-creature TargetAmount
        // with a fixed total of 2, or this would pass on any card.
        equal(true, ScenarioReplay.targetsDivided(twinBolt.getSpellAbility()));
        equal(true, twinBolt.getSpellAbility().getTargets().get(0) instanceof mage.target.common.TargetAnyTargetAmount);
        equal(2, ScenarioReplay.soleRecipientAmount(twinBolt.getSpellAbility()));
        RecordingDriver bolt = driver();
        equal(true, route(bolt, "Twin Bolt", List.of("p1:Grizzly Bears"), twinBolt.getSpellAbility()));
        equal(List.of("A:target:@p1:Grizzly Bears^X=2"), bolt.queues);
        equal(List.of("Twin Bolt"), bolt.casts);
        // A short target list is not a lone recipient: two refs keep the
        // generator's own split answers and are not queued here.
        RecordingDriver two = driver();
        equal(false, route(two, "Twin Bolt", List.of("p1:Grizzly Bears", "p1:Serra Angel"), twinBolt.getSpellAbility()));
        equal(List.of(), two.queues);
        equal(List.of(), two.casts);
        // Biogenic Upgrade's TargetCreaturePermanentAmount takes the whole amount
        // inline and keeps its old route.
        mage.cards.b.BiogenicUpgrade biogenic = new mage.cards.b.BiogenicUpgrade(UUID.randomUUID(),
                new CardSetInfo("Biogenic Upgrade", "RNA", "1", Rarity.UNCOMMON));
        equal(true, ScenarioReplay.targetsDivided(biogenic.getSpellAbility()));
        equal(null, ScenarioReplay.soleRecipientAmount(biogenic.getSpellAbility()));
        RecordingDriver bio = driver();
        equal(false, route(bio, "Biogenic Upgrade", List.of("p1:Grizzly Bears"), biogenic.getSpellAbility()));
        equal(List.of(), bio.queues);
        // An ordinary single-target spell is untouched.
        mage.cards.a.Abrade abrade = new mage.cards.a.Abrade(UUID.randomUUID(),
                new CardSetInfo("Abrade", "LCI", "1", Rarity.COMMON));
        equal(null, ScenarioReplay.soleRecipientAmount(abrade.getSpellAbility()));
        equal(null, ScenarioReplay.soleRecipientAmount(null));
        System.out.println("PASS lone divided recipient is queued with its whole amount (Twin Bolt)");

        // Unstable Glyphbridge: "for each player, choose a creature". XMage's
        // controller.choose(...) reads the CHOICE queue of the casting seat, so
        // the scripted answer must land there by bare card name - not in the
        // target, mode or amount queues.
        RecordingDriver glyph = driver();
        scripted(glyph, answers("choice", "Grizzly Bears"));
        equal(List.of("Grizzly Bears"), field(field(glyph, "playerA"), "choices"));
        System.out.println("PASS per-player creature choice is queued on the controller's choice queue (Glyphbridge)");

        // Threats Around Every Corner: manifest dread's pick from the top two
        // is a CHOICE answer, and the Forest fetched by the face-down trigger
        // is a TARGET answer, both for the controller, in answer order.
        RecordingDriver threats = driver();
        scripted(threats, answers("choice", "Jace Beleren", "target", "Forest"));
        equal(List.of("Jace Beleren"), field(field(threats, "playerA"), "choices"));
        equal(List.of("A:target:Forest"), threats.queues);
        System.out.println("PASS manifest dread pick and basic land fetch route to the choice and target queues (Threats)");

        // The sacrifice selector calls TestPlayer.choose and consumes the
        // choice queue. The discriminator is interpreted there by TestPlayer;
        // routing it to targets leaves the sacrifice answer unconsumed.
        RecordingDriver copy = driver();
        scripted(copy, answers("choice", "Joo Dee, One of Many[only copy]"));
        equal(List.of("Joo Dee, One of Many[only copy]"), field(field(copy, "playerA"), "choices"));
        equal(List.of(), copy.queues);
        System.out.println("PASS same-name token copy choice uses XMage's choice queue");

        // A same-kind same-name pick (two cards, two tokens, or an opponent's
        // object) is answered by the pick's exact scenario ref, which XMage
        // matches as its "@ref" alias. The choice queue must receive it
        // unchanged; targetName is only applied to a bare ref, never to an
        // already-aliased one.
        RecordingDriver alias = driver();
        scripted(alias, answers("choice", "@p0:Forest#2"));
        equal(List.of("@p0:Forest#2"), field(field(alias, "playerA"), "choices"));
        equal(List.of(), alias.queues);
        System.out.println("PASS same-kind same-name choice uses the exact-ref alias");

        // isScenarioRef is the gate that decides whether aliasChoiceValue
        // rewrites a value. A seat ref and a skip token are not object refs.
        equal(true, ScenarioReplay.isScenarioRef("p0:Forest#2"));
        equal(true, ScenarioReplay.isScenarioRef("p1:token:Goblin Token#2"));
        equal(false, ScenarioReplay.isScenarioRef("p1"));
        equal(false, ScenarioReplay.isScenarioRef("[target_skip]"));
        equal(false, ScenarioReplay.isScenarioRef("Forest"));
        System.out.println("PASS scenario-ref predicate excludes seats and skip tokens");

        // bindAnswerAlias parses a ref's seat ("p0:..." -> 0, not "p0") and
        // binds each "^"-joined pick of a multi-pick answer separately; the
        // joined string is not itself a ref.
        equal(0, ScenarioReplay.refSeat("p0:Forest#2"));
        equal(1, ScenarioReplay.refSeat("p1:token:Goblin Token#2"));
        equal(List.of("p0:Wastes#27", "p0:Wastes#39"), ScenarioReplay.answerRefs("@p0:Wastes#27^@p0:Wastes#39"));
        equal(List.of("p1:Grizzly Bears"), ScenarioReplay.answerRefs("@p1:Grizzly Bears"));
        equal(List.of("p1:Grizzly Bears"), ScenarioReplay.answerRefs("p1:Grizzly Bears^X=2"));
        equal(List.of(), ScenarioReplay.answerRefs("[target_skip]"));
        System.out.println("PASS answer refs parse their seat and split a joined multi-pick");

        // The corpus emits "@p0:Wastes#27^@p0:Wastes#39" on the choice queue.
        // With a live game the driver must bind each segment (here both are
        // already bound by setup, so nothing touches the board) and queue the
        // value unchanged, never throw on the joined string.
        RecordingDriver joined = driver();
        @SuppressWarnings("unchecked")
        java.util.Map<String, String> bound = (java.util.Map<String, String>) field(joined, "refAlias");
        bound.put("p0:Wastes#27", "@p0:Wastes#27");
        bound.put("p0:Wastes#39", "@p0:Wastes#39");
        Object game = UNSAFE.allocateInstance(mage.game.TwoPlayerDuel.class);
        set(joined, "currentGame", game);
        equal(true, field(joined, "currentGame") != null);
        scripted(joined, answers("choice", "@p0:Wastes#27^@p0:Wastes#39"));
        equal(List.of("@p0:Wastes#27^@p0:Wastes#39"), field(field(joined, "playerA"), "choices"));
        System.out.println("PASS joined same-name multi-pick choice binds per segment with a live game (Wastes)");

        // The setup-drive answers (xmage_answers[0]) are queued before build():
        // a numeric mode reaches the mode queue, a yes/no setup_choice is a
        // boolean chooseUse, a label setup_choice is a labelled choice, and a
        // setup_target is a target skip. The step-time scripted() must not
        // re-enqueue any setup answer at step 0 (it skips the setup_* kinds).
        RecordingDriver setup = driver();
        queueSetup(setup, answers("setup_choice", "yes", "setup_mode", "2",
                "setup_choice", "Bear", "setup_target", "[target_skip]"));
        equal(List.of("A:choice:true", "A:mode:2", "A:target:[target_skip]"), setup.queues);
        equal(List.of("Bear"), field(field(setup, "playerA"), "choices"));
        scripted(setup, answers("setup_mode", "3", "setup_choice", "yes", "setup_target", "[target_skip]"));
        equal(List.of("A:choice:true", "A:mode:2", "A:target:[target_skip]"), setup.queues);
        equal(List.of("Bear"), field(field(setup, "playerA"), "choices"));
        System.out.println("PASS setup-drive mode/boolean/label/target answers queue before build and are not re-enqueued");
    }
}
