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
import mage.target.common.TargetPermanentOrPlayer;
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

    private static void accepted(String plan, List<Target> objects, int targetCount) {
        JsonArray skips = array(plan);
        List<Integer> got = ScenarioReplay.validateTargetSkips(skips, objects, targetCount);
        check(got.size() == skips.size(), "lost a skip in " + plan);
        for (int i = 0; i < got.size(); i++) {
            check(got.get(i) == skips.get(i).getAsJsonObject().get("at").getAsInt(), "offset changed in " + plan);
        }
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
            accepted(plan, objects, 4 - array(plan).size());
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
        // The widened shape: the 0..2 object takes one pick and the skip
        // closes it. A skip on a slot that already reached its maximum is
        // rejected (XMage closes it itself and the skip would be unused).
        accepted("[{at:2,slot:1}]", objects, 4);
        rejected(() -> ScenarioReplay.validateTargetSkips(array("[{at:1,slot:0}]"), objects, 4));

        JsonArray steps = array("[{op:'cast'},{op:'resolve'}]");
        JsonObject item = JsonParser.parseString("{xmage_target_skips:[[{at:1,slot:1}],null]}").getAsJsonObject();
        check(ScenarioReplay.readTargetSkips(item, steps).size() == 2, "valid step-parallel plan rejected");
        for (String bad : Arrays.asList("null", "{}", "[]", "[{},null]", "[null,[{at:0,slot:0}]]")) {
            item.add("xmage_target_skips", JsonParser.parseString(bad));
            rejected(() -> ScenarioReplay.readTargetSkips(item, steps));
        }
        System.out.println("PASS optional target plan validation (0..1 objects, multi-pick closing skips and malformed plans)");
    }

    /** An optional ask with no legal candidate is unscriptable: gorge's
     * engine never poses an ask without a legal option, so the only legal
     * answer is none, and the queue (a later ask's answers) must stay
     * untouched. A queued skip of its own is honoured by the ordinary path. */
    private static void unscriptableAsk() throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("unscriptableEmptyAsk", int.class, int.class, boolean.class);
        method.setAccessible(true);
        check((boolean) method.invoke(null, 0, 0, false), "an empty optional ask was not declined");
        check(!(boolean) method.invoke(null, 1, 0, false), "a required ask was declined");
        check(!(boolean) method.invoke(null, 0, 2, false), "an optional ask with candidates was declined");
        check(!(boolean) method.invoke(null, 0, 0, true), "a skipped optional ask was declined");
        System.out.println("PASS unscriptable empty ask (min-0 no-candidate declined, required/candidates/skip not)");
    }

    @SuppressWarnings("unchecked")
    private static List<String> hidePlayers(Method method, Target target, List<String> queue) throws Exception {
        return (List<String>) method.invoke(null, target, queue);
    }

    /** The any-target ask of a chain whose later object targets a player must
     * not consume the later object's player answer: TestPlayer's player
     * branch scans the whole queue for a "targetPlayer=" entry before its
     * permanent branch runs. */
    private static void playerQueue() throws Exception {
        Class<?> adapter = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        Method method = adapter.getDeclaredMethod("hideLaterPlayerTargets", Target.class, List.class);
        method.setAccessible(true);
        Target anyTarget = new TargetPermanentOrPlayer();
        List<String> queue = new ArrayList<>(Arrays.asList("@p1:Grizzly Bears", "targetPlayer=PlayerB"));
        check(queue.indexOf("targetPlayer=PlayerB") == 1, "precondition: the player answer belongs to a later object");
        List<String> hidden = hidePlayers(method, anyTarget, queue);
        check(queue.equals(Arrays.asList("@p1:Grizzly Bears")), "later player answer visible to the any-target ask");
        check(hidden.equals(Arrays.asList("targetPlayer=PlayerB")), "hidden player answer reordered");
        queue.remove(0); // the ask consumes its own answer
        queue.addAll(hidden);
        check(queue.equals(Arrays.asList("targetPlayer=PlayerB")), "player answer not restored for the next ask");

        // A player answer at the front is this ask's own and stays.
        queue = new ArrayList<>(Arrays.asList("targetPlayer=PlayerB", "@p1:Grizzly Bears"));
        hidden = hidePlayers(method, anyTarget, queue);
        check(hidden.isEmpty() && queue.equals(Arrays.asList("targetPlayer=PlayerB", "@p1:Grizzly Bears")),
                "a front player answer was withheld from its own ask");
        // A TargetPlayer ask (not the any-target class) is untouched.
        hidden = hidePlayers(method, new mage.target.TargetPlayer(), queue);
        check(hidden.isEmpty(), "a TargetPlayer ask had player answers withheld");
        // No player entries: nothing hidden.
        queue = new ArrayList<>(Arrays.asList("@A", "@B"));
        check(hidePlayers(method, anyTarget, queue).isEmpty() && queue.equals(Arrays.asList("@A", "@B")),
                "a player-free queue changed");
        System.out.println("PASS later-player hiding (any-target ask, own front answer, TargetPlayer untouched)");
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
        playerQueue();
        unscriptableAsk();
        boundaries();
    }
}
