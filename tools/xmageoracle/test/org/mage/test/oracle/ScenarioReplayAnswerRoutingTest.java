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

        // A same-name token copy uses XMage's supported copy discriminator.
        // It must leave through the target queue; a bare choice label would
        // resolve by name and could select the original instead.
        RecordingDriver copy = driver();
        scripted(copy, answers("choice", "Joo Dee, One of Many [only copy]"));
        equal(List.of("A:target:Joo Dee, One of Many [only copy]"), copy.queues);
        System.out.println("PASS same-name token copy choice uses XMage's [only copy] selector");
    }
}
