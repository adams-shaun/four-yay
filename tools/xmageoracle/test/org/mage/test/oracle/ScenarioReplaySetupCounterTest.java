package org.mage.test.oracle;

import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.g.GrizzlyBears;
import mage.constants.Rarity;
import mage.counters.CounterType;
import mage.game.FakeGame;
import mage.game.permanent.Permanent;
import mage.game.permanent.PermanentCard;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check that a setup P1P1 counter reaches the setup snapshot's
 * P/T. No H2 or game replay: the production applySetupState runs against a
 * real FakeGame and a real permanent. Run
 * scripts/xmage-oracle-test-setup-counter.sh.
 *
 * The setup snapshot serializes a creature's pt as
 * {@code perm.getPower().getValue() + "/" + perm.getToughness().getValue()},
 * so asserting the permanent's P/T after the production helper is asserting
 * exactly the value the snapshot records. */
public final class ScenarioReplaySetupCounterTest {
    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static void set(Object target, String name, Object value) throws Exception {
        for (Class<?> type = target.getClass(); type != null; type = type.getSuperclass()) {
            try {
                Field field = type.getDeclaredField(name);
                field.setAccessible(true);
                field.set(target, value);
                return;
            } catch (NoSuchFieldException ignored) {
                // Fields from the XMage base class include playerA/playerB.
            }
        }
        throw new NoSuchFieldException(name);
    }

    private static TestPlayer player(UUID id) throws Exception {
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        TestPlayer p = (TestPlayer) unsafe.allocateInstance(TestPlayer.class);
        // PlayerImpl.playerId is final, so write it through Unsafe; it is
        // declared on PlayerImpl, not TestPlayer.
        Field idField = null;
        for (Class<?> type = TestPlayer.class; type != null; type = type.getSuperclass()) {
            try {
                idField = type.getDeclaredField("playerId");
                break;
            } catch (NoSuchFieldException ignored) {
                // keep walking the hierarchy
            }
        }
        if (idField == null) {
            throw new NoSuchFieldException("playerId");
        }
        unsafe.putObject(p, unsafe.objectFieldOffset(idField), id);
        equal(id, p.getId());
        return p;
    }

    /** Runs the production helper the setup runCode uses: it adds the
     * scenario's counters and then re-applies the game's continuous effects,
     * so the permanent's P/T carries the counter's layer-7d boost. */
    private static void applySetupState(ScenarioReplay driver, FakeGame game) throws Exception {
        Method method = ScenarioReplay.class
                .getDeclaredMethod("applySetupState", mage.game.Game.class);
        method.setAccessible(true);
        method.invoke(driver, game);
    }

    public static void main(String[] args) throws Exception {
        // The superclass constructor opens H2. Bypass it and initialise only
        // the fields the setup-counter helper reads.
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        ScenarioReplay driver = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);

        UUID ctrl = UUID.randomUUID();
        TestPlayer p0 = player(ctrl);
        TestPlayer p1 = player(UUID.randomUUID());
        set(driver, "playerA", p0);
        set(driver, "playerB", p1);
        set(driver, "gorgeName", "");
        set(driver, "xmageName", "");

        // setup: p0 puts a +1/+1 counter on its Grizzly Bears (printed 2/2).
        JsonObject counters = new JsonObject();
        JsonObject kinds = new JsonObject();
        kinds.addProperty("P1P1", 1);
        counters.add("Grizzly Bears", kinds);
        JsonObject p0setup = new JsonObject();
        p0setup.add("counters", counters);
        JsonObject setup = new JsonObject();
        setup.add("p0", p0setup);
        JsonObject sc0 = new JsonObject();
        sc0.add("setup", setup);
        set(driver, "sc0", sc0);

        FakeGame game = new FakeGame();
        Card bears = new GrizzlyBears(UUID.randomUUID(),
                new CardSetInfo("Grizzly Bears", "TEST", "1", Rarity.RARE));
        PermanentCard perm = new PermanentCard(bears, ctrl, game);
        game.getBattlefield().addPermanent(perm);
        Map<UUID, String> setupNames = new HashMap<>();
        setupNames.put(perm.getId(), "Grizzly Bears");
        set(driver, "setupNames", setupNames);

        // Preconditions the assertion depends on: the permanent is a real
        // battlefield creature with no counter yet, and its printed P/T is
        // the value the counter must change.
        check(game.getBattlefield().getAllPermanents().size() == 1,
                "precondition: exactly one battlefield permanent");
        check(perm.isCreature(game), "precondition: Grizzly Bears is a creature");
        equal(0, perm.getCounters(game).getCount(CounterType.P1P1));
        equal("2/2", perm.getPower().getValue() + "/" + perm.getToughness().getValue());

        applySetupState(driver, game);

        equal(1, perm.getCounters(game).getCount(CounterType.P1P1));
        // The snapshot writes exactly this pt string. 3/3 proves the layer-7d
        // boost was re-applied; without g.applyEffects() the permanent is
        // still 2/2 beside its counter.
        String pt = perm.getPower().getValue() + "/" + perm.getToughness().getValue();
        equal("3/3", pt);
        check(!pt.equals("2/2"), "precondition: the counter must change the P/T");
        System.out.println("PASS setup P1P1 counter reaches the snapshot P/T (" + pt + ")");
    }
}
