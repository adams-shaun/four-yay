package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonPrimitive;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.LinkedHashMap;
import mage.constants.Zone;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable regression check of the real driver helpers, without H2 or a game.
 * Run scripts/xmage-oracle-test-split-room.sh. Full XMage replay is still needed
 * to validate resolution and compare verdicts on the host. */
public final class ScenarioReplaySplitRoomTest {
    public static final class RecordingDriver extends ScenarioReplay {
        String dealt;
        Zone dealtZone;
        int dealtCount;

        @Override
        public void addCard(Zone zone, TestPlayer player, String name, int count, boolean tapped) {
            dealt = name;
            dealtZone = zone;
            dealtCount += count;
        }
    }

    private static void set(ScenarioReplay driver, String name, Object value) throws Exception {
        Field f = ScenarioReplay.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(driver, value);
    }

    private static Object call(ScenarioReplay driver, String name, Class<?>[] types, Object... args) throws Exception {
        Method m = ScenarioReplay.class.getDeclaredMethod(name, types);
        m.setAccessible(true);
        return m.invoke(driver, args);
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static RecordingDriver driver(String half, String whole) throws Exception {
        // The superclass constructor scans CardRepository (H2), even when only
        // testing names. Bypass it for this test double, then initialise only
        // the fields used by add/name conversion. Never execute a game here.
        Field f = Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        RecordingDriver d = (RecordingDriver) ((Unsafe) f.get(null)).allocateInstance(RecordingDriver.class);
        set(d, "gorgeName", half);
        set(d, "xmageName", whole);
        set(d, "buildCounts", new LinkedHashMap<String, Integer>());
        set(d, "refAlias", new LinkedHashMap<String, String>());
        return d;
    }

    private static void checkHalf(String half, String whole, String zoneName, String observed) throws Exception {
        if (half.equals(whole) || !whole.startsWith(half + " // ")) {
            throw new AssertionError("fixture must have distinct half and whole names");
        }
        RecordingDriver d = driver(half, whole);
        JsonObject setup = new JsonObject();
        JsonArray hand = new JsonArray();
        hand.add(half);
        setup.add("hand", hand);
        // Exercise the setup path itself, recording what add sends to XMage.
        equal(1, call(d, "add", new Class<?>[]{JsonObject.class, String.class, Zone.class, TestPlayer.class},
                setup, "hand", Zone.HAND, null));
        equal(1, d.dealtCount);
        equal(Zone.HAND, d.dealtZone);
        equal(whole, d.dealt);
        String ref = "p0:" + half;
        String stripped = (String) call(d, "refName", new Class<?>[]{String.class}, ref);
        // Execute the production cast conversion, not a copy or a source-text assertion.
        equal(half, call(d, "castSpelling", new Class<?>[]{String.class}, stripped));
        equal("Grizzly Bears", call(d, "castSpelling", new Class<?>[]{String.class}, "Grizzly Bears"));
        equal(whole, call(d, "xmageSpelling", new Class<?>[]{String.class}, half));
        // XMage reports split cards whole in the graveyard, Rooms by half on
        // the battlefield. Both must match gorge's scenario face spelling.
        JsonObject snapshot = new JsonObject();
        JsonArray zone = new JsonArray();
        zone.add(observed);
        snapshot.add(zoneName, zone);
        JsonObject normalised = (JsonObject) call(d, "gorgeSpellings", new Class<?>[]{JsonElement.class}, snapshot);
        equal(half, normalised.getAsJsonArray(zoneName).get(0).getAsString());
        equal(new JsonPrimitive(half), call(d, "gorgeSpellings", new Class<?>[]{JsonElement.class}, new JsonPrimitive(whole)));
        System.out.println("PASS " + ref + ": hand=" + d.dealt + ", cast=" + half + ", " + zoneName + "=" + observed);
    }

    public static void main(String[] args) throws Exception {
        checkHalf("Cease", "Cease // Desist", "graveyard", "Cease // Desist");
        checkHalf("Walk-In Closet", "Walk-In Closet // Forgotten Cellar", "battlefield", "Walk-In Closet");
        RecordingDriver alias = driver("Dáin Ironfoot", "Dain Ironfoot");
        equal("Dain Ironfoot", call(alias, "castSpelling", new Class<?>[]{String.class}, "Dáin Ironfoot"));
        RecordingDriver plain = driver("Shock", "");
        equal("Shock", call(plain, "castSpelling", new Class<?>[]{String.class}, "Shock"));
        equal(true, ScenarioReplay.queueAdjustedCastTargets(true, false, 1));
        equal(false, ScenarioReplay.queueAdjustedCastTargets(false, false, 1));
        equal(false, ScenarioReplay.queueAdjustedCastTargets(true, true, 1));
        equal(false, ScenarioReplay.queueAdjustedCastTargets(true, false, 0));
        System.out.println("PASS adjusted spell targets are queued without changing divided, ordinary, or targetless casts");
        System.out.println("PASS ordinary alias and unchanged spelling");
    }
}
