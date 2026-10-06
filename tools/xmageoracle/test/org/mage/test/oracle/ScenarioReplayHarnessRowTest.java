package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.io.BufferedReader;
import java.io.PrintWriter;
import java.io.StringReader;
import java.io.StringWriter;
import java.lang.reflect.Field;
import sun.misc.Unsafe;

/** Executable regression check that one malformed scenario fails its own row
 * and the batch goes on. Run scripts/xmage-oracle-test-harness-row.sh.
 * Exercises the real steps() helper and the real replayLines() loop with a
 * stub replayOnce, so no H2 database or game is needed. */
public final class ScenarioReplayHarnessRowTest {
    /** replayOnce throws for the malformed scenario (the shape a "steps":
     * null line used to produce) and returns an ordinary row for the next, so
     * the loop's catch is what has to keep the batch alive. */
    static final class StubDriver extends ScenarioReplay {
        int calls;

        @Override
        JsonObject replayOnce(JsonObject sc, boolean strict) {
            calls++;
            if (sc.has("id") && "bad".equals(sc.get("id").getAsString())) {
                throw new ClassCastException("JsonNull cannot be cast to JsonArray");
            }
            JsonObject res = new JsonObject();
            res.addProperty("strict", strict);
            res.addProperty("name", sc.has("name") ? sc.get("name").getAsString() : "");
            if (sc.has("id")) {
                res.add("id", sc.get("id"));
            }
            res.add("snapshots", new JsonArray());
            return res;
        }
    }

    private static StubDriver driver() throws Exception {
        // The superclass constructor scans CardRepository (H2), even when only
        // the loop is under test. Bypass it for this test double.
        Field f = Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        return (StubDriver) ((Unsafe) f.get(null)).allocateInstance(StubDriver.class);
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static JsonObject parse(String s) {
        return JsonParser.parseString(s).getAsJsonObject();
    }

    public static void main(String[] args) throws Exception {
        // Precondition the loop's survival depends on: the helper must turn a
        // JSON null into an empty array, not throw. On the unfixed driver the
        // first line throws ClassCastException and this test fails loudly.
        equal(0, ScenarioReplay.steps(parse("{\"id\":\"bad\",\"steps\":null}")).size());
        equal(0, ScenarioReplay.steps(parse("{\"id\":\"bad\"}")).size());
        JsonArray two = ScenarioReplay.steps(parse("{\"steps\":[{},{}]}"));
        equal(2, two.size());
        System.out.println("PASS steps: null=" + ScenarioReplay.steps(parse("{\"steps\":null}")).size()
                + ", missing=" + ScenarioReplay.steps(parse("{}")).size()
                + ", present=" + two.size());

        String in = "{\"id\":\"bad\",\"name\":\"malformed\",\"steps\":null}\n"
                + "{\"id\":\"good\",\"name\":\"ordinary\",\"steps\":[]}\n";
        StubDriver d = driver();
        StringWriter out = new StringWriter();
        int n = ScenarioReplay.replayLines(d, new BufferedReader(new StringReader(in)),
                new PrintWriter(out, true));
        // The malformed scenario must get a harness row for ITS id, and the
        // next scenario must still get its own row: exactly 2 rows, in order.
        equal(2, n);
        equal(2, d.calls);
        String[] lines = out.toString().strip().split("\\R");
        equal(2, lines.length);
        JsonObject bad = parse(lines[0]);
        equal("bad", bad.get("id").getAsString());
        equal(true, bad.has("harness"));
        equal(true, bad.get("harness").getAsString().contains("ClassCastException"));
        JsonObject good = parse(lines[1]);
        equal("good", good.get("id").getAsString());
        equal(false, good.has("harness"));
        System.out.println("PASS harness row: " + lines[0]);
        System.out.println("PASS next scenario row: " + lines[1]);
    }
}
