package org.mage.test.oracle;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonPrimitive;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.LinkedHashMap;
import java.util.UUID;
import mage.cards.CardSetInfo;
import mage.cards.t.TheEaglesAreComing;
import mage.constants.PhaseStep;
import mage.constants.Rarity;
import mage.constants.Zone;
import mage.filter.FilterPermanent;
import mage.target.TargetPermanent;
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
        // Unsafe allocation skips initialisers; driver() fills these.
        java.util.List<String> queued;
        java.util.List<String> casts;

        @Override
        public void addTarget(TestPlayer player, String target) {
            queued.add(target);
        }

        @Override
        public void castSpell(int turnNum, PhaseStep step, TestPlayer player, String cardName) {
            casts.add(cardName);
        }

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

    private static final Class<?>[] ROUTE_TYPES = {int.class, PhaseStep.class, TestPlayer.class, String.class,
            java.util.List.class, mage.abilities.Ability.class};

    private static Object field(ScenarioReplay driver, String name) throws Exception {
        Field f = ScenarioReplay.class.getDeclaredField(name);
        f.setAccessible(true);
        return f.get(driver);
    }

    private static RecordingDriver routeDriver() throws Exception {
        RecordingDriver d = driver("", "");
        set(d, "cast", new java.util.ArrayList<String>());
        set(d, "adjustedCasts", new java.util.HashSet<String>());
        return d;
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
        d.queued = new java.util.ArrayList<>();
        d.casts = new java.util.ArrayList<>();
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
        // Crew/Saddle abilities include HTML reminder text in getRule(). The
        // driver must issue the ability-class prefix accepted by TestPlayer.
        String crewRule = "Crew 1 <i>(Tap any number of untapped creatures you control with total power 1 or more: This Vehicle becomes an artifact creature until end of turn.)</i>";
        equal("Crew 1", ScenarioReplay.activationCommandText(crewRule));
        RecordingDriver activation = driver("", "");
        JsonArray abilityTexts = new JsonArray();
        abilityTexts.add(crewRule);
        set(activation, "xabilities", abilityTexts);
        equal("Crew 1", call(activation, "xabilityAt", new Class<?>[]{int.class}, 0));
        equal("Crew 2", ScenarioReplay.activationCommandText("Crew 2"));
        equal("Saddle 3", ScenarioReplay.activationCommandText(
                "Saddle 3 <i>(Tap any number of other creatures you control with total power 3 or more: This Mount becomes saddled until end of turn. Saddle only as a sorcery.)</i>"));
        equal("Equip {2}", ScenarioReplay.activationCommandText("Equip {2}"));
        equal(true, ScenarioReplay.queueAdjustedCastTargets(true, false, 1));
        equal(false, ScenarioReplay.queueAdjustedCastTargets(false, false, 1));
        equal(false, ScenarioReplay.queueAdjustedCastTargets(true, true, 1));
        equal(false, ScenarioReplay.queueAdjustedCastTargets(true, false, 0));
        TheEaglesAreComing eagles = new TheEaglesAreComing(UUID.randomUUID(),
                new CardSetInfo("The Eagles Are Coming!", "HOB", "1", Rarity.RARE));
        // Precondition the production predicate depends on: the motivating card's
        // spell ability is targetless until its adjuster runs. If a future edit
        // adds a base target, this fails loudly instead of silently rerouting.
        equal(true, ScenarioReplay.hasTargetAdjuster(eagles.getSpellAbility()));
        equal(true, eagles.getSpellAbility().getAllSelectedTargets().isEmpty());
        equal(true, ScenarioReplay.needsQueuedCastTargets(eagles.getSpellAbility()));
        // An adjuster card that already declares a base target (Dominate,
        // Distorting Wake) is NOT targetless: it keeps the inline $target path and
        // must not be rerouted through the queue+skip branch.
        mage.cards.d.Dominate withBaseTarget = new mage.cards.d.Dominate(UUID.randomUUID(),
                new CardSetInfo("Dominate", "DTK", "1", Rarity.UNCOMMON));
        equal(true, ScenarioReplay.hasTargetAdjuster(withBaseTarget.getSpellAbility()));
        equal(false, withBaseTarget.getSpellAbility().getAllSelectedTargets().isEmpty());
        equal(false, ScenarioReplay.needsQueuedCastTargets(withBaseTarget.getSpellAbility()));
        // A modal spell whose only target is in a later mode (Cosmium Confluence:
        // modes 1 and 2 targetless, mode 3 destroys target enchantment) fails the
        // inline $target= check against the first mode, so it is queued instead.
        mage.cards.c.CosmiumConfluence confluence = new mage.cards.c.CosmiumConfluence(UUID.randomUUID(),
                new CardSetInfo("Cosmium Confluence", "LCI", "1", Rarity.RARE));
        equal(true, confluence.getSpellAbility().getModes().getMode().getTargets().isEmpty());
        equal(true, confluence.getSpellAbility().getModes().size() > 1);
        equal(true, ScenarioReplay.firstTargetInLaterMode(confluence.getSpellAbility()));
        // Unaffected shapes: a modal spell whose first mode already has a target
        // (Cryptic Command's first mode, Abrade) keeps the inline path, as does a
        // single-mode targeted spell and a single-mode targetless one.
        mage.cards.a.Abrade abrade = new mage.cards.a.Abrade(UUID.randomUUID(),
                new CardSetInfo("Abrade", "LCI", "1", Rarity.COMMON));
        equal(true, abrade.getSpellAbility().getModes().size() > 1);
        equal(false, abrade.getSpellAbility().getModes().getMode().getTargets().isEmpty());
        equal(false, ScenarioReplay.firstTargetInLaterMode(abrade.getSpellAbility()));
        equal(false, ScenarioReplay.firstTargetInLaterMode(eagles.getSpellAbility()));
        equal(false, ScenarioReplay.firstTargetInLaterMode(null));
        // The production cast routing (castQueuedTargets, the cast step's first
        // branch), driven through the SAME method the cast step calls, with the
        // real spell ability each card produces. The routing decision derives
        // every flag from that ability inside castQueuedTargets, so dropping the
        // modal term anywhere in it fails this test instead of passing silently.
        // Cosmium: the target is queued and the cast carries no inline target.
        java.util.List<String> tgt = java.util.List.of("p1:Glorious Anthem");
        RecordingDriver route = routeDriver();
        equal(true, call(route, "castQueuedTargets", ROUTE_TYPES, 1, PhaseStep.PRECOMBAT_MAIN, null,
                "Cosmium Confluence", tgt, confluence.getSpellAbility()));
        equal(java.util.List.of("Glorious Anthem"), route.queued);
        equal(java.util.List.of("Cosmium Confluence"), route.casts);
        equal(false, ((java.util.Set<?>) field(route, "adjustedCasts")).contains("Cosmium Confluence"));
        // An adjuster card is queued AND registered as adjusted.
        RecordingDriver adj = routeDriver();
        equal(true, call(adj, "castQueuedTargets", ROUTE_TYPES, 1, PhaseStep.PRECOMBAT_MAIN, null,
                "The Eagles Are Coming!", tgt, eagles.getSpellAbility()));
        equal(java.util.List.of("Glorious Anthem"), adj.queued);
        equal(true, ((java.util.Set<?>) field(adj, "adjustedCasts")).contains("The Eagles Are Coming!"));
        // Ordinary (Abrade), divided (Biogenic Upgrade's TargetAmount), and
        // targetless casts keep their own routes: nothing queued, nothing cast.
        mage.cards.b.BiogenicUpgrade biogenic = new mage.cards.b.BiogenicUpgrade(UUID.randomUUID(),
                new CardSetInfo("Biogenic Upgrade", "RNA", "1", Rarity.UNCOMMON));
        equal(true, ScenarioReplay.targetsDivided(biogenic.getSpellAbility()));
        for (Object[] c : new Object[][]{
                {abrade.getSpellAbility(), tgt}, {biogenic.getSpellAbility(), tgt},
                {confluence.getSpellAbility(), java.util.List.<String>of()}}) {
            RecordingDriver other = routeDriver();
            equal(false, call(other, "castQueuedTargets", ROUTE_TYPES, 1, PhaseStep.PRECOMBAT_MAIN, null,
                    "Abrade", c[1], c[0]));
            equal(java.util.List.of(), other.queued);
            equal(java.util.List.of(), other.casts);
        }
        equal(true, ScenarioReplay.targetSlotNeedsSkip(
                java.util.List.of(new TargetPermanent(0, Integer.MAX_VALUE, new FilterPermanent())), 1));
        equal(false, ScenarioReplay.targetSlotNeedsSkip(java.util.List.of(new TargetPermanent()), 1));
        // The skip is added only when XMage asks again with no answer left, so
        // a pool the supplied target exhausted never asks, so nothing calls
        // this and no skip is left behind; an ask with candidates left gets it.
        java.util.List<String> askedAgain = new java.util.ArrayList<>();
        ScenarioReplay.closeAskedAgain(askedAgain, true);
        equal(java.util.List.of(TestPlayer.TARGET_SKIP), askedAgain);
        java.util.List<String> answered = new java.util.ArrayList<>(java.util.List.of("Grizzly Bears"));
        ScenarioReplay.closeAskedAgain(answered, true);
        equal(java.util.List.of("Grizzly Bears"), answered);
        java.util.List<String> otherSpell = new java.util.ArrayList<>();
        ScenarioReplay.closeAskedAgain(otherSpell, false);
        equal(java.util.List.of(), otherSpell);
        System.out.println("PASS target-adjuster detection and closing an unfilled adjusted target slot");
        System.out.println("PASS adjusted spell targets are queued without changing divided, ordinary, or targetless casts");
        System.out.println("PASS ordinary alias and unchanged spelling");
    }
}
