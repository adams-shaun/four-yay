package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import mage.target.Target;
import mage.target.common.TargetCardInYourGraveyard;
import org.mage.test.player.TestPlayer;

/** No database or AI: executable contract checks on the pinned XMage classes.
 * Actual chooser/strict replay still belongs to the host replay gate. */
public final class OptionalTargetSkipsTest {
    private static final String SKIP = TestPlayer.TARGET_SKIP;

    private static void check(boolean condition, String message) {
        if (!condition) {
            throw new AssertionError(message);
        }
    }

    private static JsonArray array(String json) {
        return JsonParser.parseString(json).getAsJsonArray();
    }

    private static void rejected(Runnable action) {
        try {
            action.run();
        } catch (IllegalArgumentException expected) {
            return;
        }
        throw new AssertionError("malformed plan was accepted");
    }

    private static void plans() {
        List<Target> objects = new ArrayList<>();
        for (int i = 0; i < 4; i++) {
            objects.add(new TargetCardInYourGraveyard(0, 1));
        }
        check(objects.size() == 4 && objects.get(1).getMinNumberOfTargets() == 0,
                "precondition: four optional XMage objects");
        for (String plan : Arrays.asList(
                "[{at:0,slot:0}]", "[{at:1,slot:1}]", "[{at:3,slot:3}]",
                "[{at:1,slot:1},{at:1,slot:2}]",
                "[{at:0,slot:0},{at:0,slot:1},{at:1,slot:3}]")) {
            JsonArray skips = array(plan);
            List<Integer> got = ScenarioReplay.validateTargetSkips(skips, objects, 4 - skips.size());
            check(got.size() == skips.size(), "lost a skip in " + plan);
            for (int i = 0; i < got.size(); i++) {
                check(got.get(i) == skips.get(i).getAsJsonObject().get("at").getAsInt(), "offset changed");
            }
        }
        for (String bad : Arrays.asList(
                "[{at:-1,slot:0}]", "[{at:4,slot:4}]", "[{at:1,slot:2}]",
                "[{at:1,slot:1},{at:0,slot:1}]", "[{at:1,slot:1},{at:1,slot:1}]",
                "[{at:1.5,slot:1}]", "[{at:'1',slot:1}]", "[{at:1}]", "[null]")) {
            JsonArray skips = array(bad);
            rejected(() -> ScenarioReplay.validateTargetSkips(skips, objects, 4 - skips.size()));
        }
        JsonArray middle = array("[{at:1,slot:1}]");
        rejected(() -> ScenarioReplay.validateTargetSkips(middle, objects, 2));
        objects.set(1, new TargetCardInYourGraveyard(1, 1));
        check(objects.get(1).getMinNumberOfTargets() == 1, "precondition: required slot");
        rejected(() -> ScenarioReplay.validateTargetSkips(middle, objects, 3));
        objects.set(1, new TargetCardInYourGraveyard(0, 2));
        check(objects.get(1).getMaxNumberOfTargets() == 2, "precondition: ranged slot");
        rejected(() -> ScenarioReplay.validateTargetSkips(middle, objects, 3));

        JsonArray steps = array("[{op:'cast'},{op:'resolve'}]");
        JsonObject item = JsonParser.parseString("{xmage_target_skips:[[{at:1,slot:1}],null]}").getAsJsonObject();
        check(ScenarioReplay.readTargetSkips(item, steps).size() == 2, "valid step-parallel plan rejected");
        for (String bad : Arrays.asList("null", "{}", "[]", "[{},null]", "[null,[{at:0,slot:0}]]")) {
            item.add("xmage_target_skips", JsonParser.parseString(bad));
            rejected(() -> ScenarioReplay.readTargetSkips(item, steps));
        }
        System.out.println("PASS optional target plan validation (leading/middle/trailing/consecutive and malformed)");
    }

    @SuppressWarnings("unchecked")
    private static List<String> hide(Method method, List<String> queue) throws Exception {
        return (List<String>) method.invoke(null, queue);
    }

    private static void boundaries() throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("hideAfterNextSkip", List.class);
        method.setAccessible(true);
        List<String> queue = new ArrayList<>(Arrays.asList("@A", SKIP, SKIP, "@B", "@C"));
        check(queue.indexOf(SKIP) == 1, "precondition: skip belongs to a future object");
        List<String> suffix = hide(method, queue);
        check(queue.equals(Arrays.asList("@A")), "future skip exposed to matcher");
        check(suffix.equals(Arrays.asList(SKIP, SKIP, "@B", "@C")), "suffix reordered");
        try {
            queue.remove(0); // delegate consumes the current segment
        } finally {
            queue.addAll(suffix);
        }
        check(queue.equals(Arrays.asList(SKIP, SKIP, "@B", "@C")), "suffix not restored after consumption");
        for (int i = 0; i < 2; i++) {
            check(hide(method, queue).isEmpty() && SKIP.equals(queue.get(0)), "front skip semantics changed");
            queue.remove(0); // TestPlayer's front skip consumption
        }
        check(queue.equals(Arrays.asList("@B", "@C")), "consecutive omissions lost next picks");
        check(hide(method, queue).isEmpty(), "legacy skip-free queue changed");

        queue = new ArrayList<>(Arrays.asList("@unused", SKIP));
        suffix = hide(method, queue);
        try {
            throw new IllegalArgumentException("delegate fails strictly");
        } catch (IllegalArgumentException expected) {
            // No consumption: the unused answer must remain for strict checks.
        } finally {
            queue.addAll(suffix);
        }
        check(queue.equals(Arrays.asList("@unused", SKIP)), "exception lost unused answer or trailing skip");
        System.out.println("PASS queue boundaries (front/future/consecutive/trailing, consumption and unused restoration)");
    }

    public static void main(String[] args) throws Exception {
        plans();
        boundaries();
    }
}
