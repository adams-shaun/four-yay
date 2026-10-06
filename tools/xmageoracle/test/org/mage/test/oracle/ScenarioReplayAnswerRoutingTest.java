package org.mage.test.oracle;

import java.lang.reflect.Field;
import java.util.List;
import sun.misc.Unsafe;

/** Regressions for answer forms consumed by the XMage scenario driver. */
public final class ScenarioReplayAnswerRoutingTest {
    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static ScenarioReplay driver(String gorgeName, String xmageName) throws Exception {
        Field unsafeField = Unsafe.class.getDeclaredField("theUnsafe");
        unsafeField.setAccessible(true);
        ScenarioReplay replay = (ScenarioReplay) ((Unsafe) unsafeField.get(null))
                .allocateInstance(ScenarioReplay.class);
        set(replay, "gorgeName", gorgeName);
        set(replay, "xmageName", xmageName);
        set(replay, "refAlias", new java.util.LinkedHashMap<String, String>());
        ((java.util.Map<String, String>) field(replay, "refAlias")).put("p1:Grizzly Bears", "@p1:Grizzly Bears");
        return replay;
    }

    private static Object field(ScenarioReplay replay, String name) throws Exception {
        Field field = ScenarioReplay.class.getDeclaredField(name);
        field.setAccessible(true);
        return field.get(replay);
    }

    private static void set(ScenarioReplay replay, String name, Object value) throws Exception {
        Field field = ScenarioReplay.class.getDeclaredField(name);
        field.setAccessible(true);
        field.set(replay, value);
    }

    public static void main(String[] args) throws Exception {
        // Twin Bolt's only selected recipient still uses the TargetAmount
        // caret-joined form, not castSpell's ordinary $target= alias form.
        ScenarioReplay twinBolt = driver("Twin Bolt", "");
        equal("Grizzly Bears", twinBolt.dividedCastTargetString(List.of("p1:Grizzly Bears")));
        equal("Grizzly Bears^Serra Angel", twinBolt.dividedCastTargetString(
                List.of("p1:Grizzly Bears", "p1:Serra Angel")));

        System.out.println("PASS divided TargetAmount cast target strings (one and multiple recipients)");
    }
}
