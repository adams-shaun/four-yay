package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import mage.cards.Card;
import mage.cards.CardSetInfo;
import mage.cards.DoubleFacedCard;
import mage.cards.b.BruceBanner;
import mage.cards.p.PeterParker;
import mage.cards.v.VincentValentine;
import mage.constants.Zone;
import mage.game.PutToBattlefieldInfo;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Dry setup/helper regression: no H2, reset(), execute(), or game replay.
 * Full snapshots and verdict agreement still require the host pass. */
public final class ScenarioReplayBackFaceTest {
    public static final class RecordingDriver extends ScenarioReplay {
        Map<TestPlayer, List<PutToBattlefieldInfo>> placements;
        DoubleFacedCard dealt;

        @Override
        protected List<PutToBattlefieldInfo> getBattlefieldCards(TestPlayer player) {
            return placements.computeIfAbsent(player, key -> new ArrayList<>());
        }

        @Override
        public void addCard(Zone zone, TestPlayer player, String name, int count, boolean tapped) {
            CardSetInfo info = new CardSetInfo(name, "TEST", "1", mage.constants.Rarity.RARE);
            if (name.equals("Vincent Valentine")) {
                dealt = new VincentValentine(null, info);
            } else if (name.equals("Peter Parker")) {
                dealt = new PeterParker(null, info);
            } else if (name.equals("Bruce Banner")) {
                dealt = new BruceBanner(null, info);
            } else {
                throw new AssertionError("unexpected fixture " + name);
            }
            if (zone == Zone.BATTLEFIELD) {
                getBattlefieldCards(player).add(new PutToBattlefieldInfo(dealt, tapped));
            }
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

    private static Object call(ScenarioReplay driver, String name, Class<?>[] types, Object... args) throws Exception {
        Method method = ScenarioReplay.class.getDeclaredMethod(name, types);
        method.setAccessible(true);
        return method.invoke(driver, args);
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    public static void main(String[] args) throws Exception {
        // The superclass constructor opens H2. As in SplitRoomTest, bypass
        // it and initialise only the fields these production helpers use.
        Field field = Unsafe.class.getDeclaredField("theUnsafe");
        field.setAccessible(true);
        Unsafe unsafe = (Unsafe) field.get(null);
        RecordingDriver driver = (RecordingDriver) unsafe.allocateInstance(RecordingDriver.class);
        TestPlayer p0 = (TestPlayer) unsafe.allocateInstance(TestPlayer.class);
        TestPlayer p1 = (TestPlayer) unsafe.allocateInstance(TestPlayer.class);
        set(driver, "playerA", p0);
        set(driver, "playerB", p1);
        driver.placements = new HashMap<>();
        Map<UUID, String> setupNames = new HashMap<>();
        set(driver, "setupNames", setupNames);
        set(driver, "backFaceNames", new HashMap<String, String>());
        set(driver, "buildCounts", new HashMap<String, Integer>());
        set(driver, "refAlias", new HashMap<String, String>());
        set(driver, "gorgeName", "");
        set(driver, "xmageName", "");

        for (String[] fixture : new String[][]{
                {"Vincent Valentine", "Galian Beast"},
                {"Peter Parker", "Amazing Spider-Man"},
                {"Bruce Banner", "The Incredible Hulk"}
        }) {
            String front = fixture[0], back = fixture[1];
            JsonObject setup = new JsonObject();
            JsonArray cards = new JsonArray();
            cards.add(front);
            cards.add(front);
            setup.add("battlefield", cards);
            JsonArray marked = new JsonArray();
            marked.add(front);
            setup.add("back_face", marked);
            setup.add("tapped", marked);
            equal(2, call(driver, "add", new Class<?>[]{JsonObject.class, String.class, Zone.class, TestPlayer.class},
                    setup, "battlefield", Zone.BATTLEFIELD, p0));
            // Preconditions: a genuine two-permanent-face fixture with distinct
            // names, and two actual placements (not a missing setup or no-op).
            equal(back, driver.dealt.getRightHalfCard().getName());
            if (front.equals(back) || !driver.dealt.getRightHalfCard().isPermanent()) {
                throw new AssertionError("fixture must have a distinct permanent back face");
            }
            List<PutToBattlefieldInfo> placed = driver.getBattlefieldCards(p0);
            for (int i = placed.size() - 2; i < placed.size(); i++) {
                Card card = placed.get(i).getCard();
                equal(back, card.getName());
                equal(front, setupNames.get(card.getId()));
                equal(true, placed.get(i).isTapped());
            }
            equal(back + ":1", call(driver, "combatName", new Class<?>[]{String.class}, "p0:" + front + "#2"));
            equal(front, call(driver, "combatName", new Class<?>[]{String.class}, "p1:" + front));
            // Neither another seat's unmarked battlefield nor a hand card is flipped.
            JsonObject unmarked = new JsonObject();
            unmarked.add("battlefield", marked);
            call(driver, "add", new Class<?>[]{JsonObject.class, String.class, Zone.class, TestPlayer.class},
                    unmarked, "battlefield", Zone.BATTLEFIELD, p1);
            equal(front, driver.getBattlefieldCards(p1).get(driver.getBattlefieldCards(p1).size() - 1).getCard().getName());
            setup.add("hand", marked);
            call(driver, "add", new Class<?>[]{JsonObject.class, String.class, Zone.class, TestPlayer.class},
                    setup, "hand", Zone.HAND, p0);
            equal(front, driver.dealt.getName());
            System.out.println("PASS " + front + " -> " + back + ": duplicate/tapped setup, front refs, seat and hand isolation");
        }
    }
}
