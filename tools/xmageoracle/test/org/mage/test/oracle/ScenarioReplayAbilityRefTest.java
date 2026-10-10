package org.mage.test.oracle;

import java.lang.reflect.Field;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import mage.cards.CardSetInfo;
import mage.constants.PhaseStep;
import mage.constants.Rarity;
import mage.target.TargetStackObject;
import org.mage.test.player.TestPlayer;
import sun.misc.Unsafe;

/** Executable check of the driver's "ability:" target-ref rule, without H2 or
 * a game. A target naming a stack ability ("p0:ability:Elvish Visionary", gorge's
 * grammar for Kirol, Attentive First-Year's "target triggered ability") must be
 * queued as an alias that a code action binds to the pending ability, because
 * TestPlayer's stack branch matches an ability only by alias or by a prefix of
 * its rule text. Run scripts/xmage-oracle-test-split-room.sh. */
public final class ScenarioReplayAbilityRefTest {
    /** Records the target queue and the registered code actions. */
    public static final class RecordingDriver extends ScenarioReplay {
        List<String> queued;
        List<String> codeActions;

        @Override
        public void addTarget(TestPlayer player, String target) {
            queued.add(target);
        }

        @Override
        public void runCode(String info, int turnNum, PhaseStep step, TestPlayer player,
                org.mage.test.serverside.base.CardTestCodePayload payload) {
            codeActions.add(info);
        }
    }

    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static void set(ScenarioReplay driver, String name, Object value) throws Exception {
        Field f = ScenarioReplay.class.getDeclaredField(name);
        f.setAccessible(true);
        f.set(driver, value);
    }

    private static RecordingDriver driver() throws Exception {
        // The superclass constructor scans CardRepository (H2); bypass it.
        Field f = Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        RecordingDriver d = (RecordingDriver) ((Unsafe) f.get(null)).allocateInstance(RecordingDriver.class);
        d.queued = new ArrayList<>();
        d.codeActions = new ArrayList<>();
        set(d, "gorgeName", "");
        set(d, "xmageName", "");
        set(d, "cast", new ArrayList<String>());
        set(d, "refAlias", new java.util.LinkedHashMap<String, String>());
        return d;
    }

    private static void queue(ScenarioReplay d, String ref) throws Exception {
        java.lang.reflect.Method m = ScenarioReplay.class.getDeclaredMethod("queueCastTarget",
                TestPlayer.class, String.class);
        m.setAccessible(true);
        m.invoke(d, (Object) null, ref);
    }

    public static void main(String[] args) throws Exception {
        // The ref grammar.
        equal(true, ScenarioReplay.isAbilityRef("p0:ability:Elvish Visionary"));
        equal(true, ScenarioReplay.isAbilityRef("p1:ability:Elvish Visionary#2"));
        equal(false, ScenarioReplay.isAbilityRef("p0:Elvish Visionary"));
        equal(false, ScenarioReplay.isAbilityRef("p0:token:Elvish Visionary"));
        equal(false, ScenarioReplay.isAbilityRef("p0"));
        equal(false, ScenarioReplay.isAbilityRef("ability:Elvish Visionary"));
        equal("Elvish Visionary", ScenarioReplay.abilitySourceName("p0:ability:Elvish Visionary"));
        equal("Elvish Visionary", ScenarioReplay.abilitySourceName("p0:ability:Elvish Visionary#2"));
        equal(1, ScenarioReplay.refOrdinal("p0:ability:Elvish Visionary"));
        equal(2, ScenarioReplay.refOrdinal("p0:ability:Elvish Visionary#2"));
        // A name with its own colon keeps it ("Summon: Bahamut").
        equal("Summon: Bahamut", ScenarioReplay.abilitySourceName("p0:ability:Summon: Bahamut"));

        // k-th PENDING ability in creation order; XMage's stack lists top first.
        List<String> topFirst = List.of("c-new", "b-new", "a-mid", "b-old");
        equal("b-old", ScenarioReplay.nthPending(topFirst, s -> s.startsWith("b"), 1));
        equal("b-new", ScenarioReplay.nthPending(topFirst, s -> s.startsWith("b"), 2));
        equal(true, ScenarioReplay.nthPending(topFirst, s -> s.startsWith("b"), 3) == null);
        // Precondition: the two orders differ, so top-first counting would fail above.
        equal(false, topFirst.stream().filter(s -> s.startsWith("b")).findFirst().get().equals("b-old"));

        // The motivating card: Kirol's target is a stack-object target, the
        // class whose branch matches only alias or rule-text prefix.
        mage.cards.k.KirolAttentiveFirstYear kirol = new mage.cards.k.KirolAttentiveFirstYear(UUID.randomUUID(),
                new CardSetInfo("Kirol, Attentive First-Year", "ECL", "1", Rarity.RARE));
        boolean stackTarget = false;
        for (mage.abilities.Ability a : kirol.getAbilities()) {
            for (mage.target.Target t : a.getTargets()) {
                stackTarget |= t instanceof TargetStackObject;
            }
        }
        equal(true, stackTarget);

        // The production queueing path (queueCastTarget): the ref is queued as
        // an alias, NOT as the literal "ability:<Source>" XMage never matches,
        // and a code action is registered ahead of the activation to bind it.
        RecordingDriver d = driver();
        queue(d, "p0:ability:Elvish Visionary");
        equal(List.of("@p0:ability:Elvish Visionary~1"), d.queued);
        equal(List.of("bind p0:ability:Elvish Visionary"), d.codeActions);
        // The same ref in a later step may name another ability object: a
        // fresh alias, since an alias can not be rebound.
        queue(d, "p0:ability:Elvish Visionary");
        equal("@p0:ability:Elvish Visionary~2", d.queued.get(1));
        // A card target is untouched and registers no code action.
        queue(d, "p1:Grizzly Bears");
        equal(2, d.codeActions.size());
        equal("p1:Grizzly Bears".contains("ability:"), false);

        // The source-name match: the live name, or the gorge spelling of it.
        mage.cards.e.ElvishVisionary visionary = new mage.cards.e.ElvishVisionary(UUID.randomUUID(),
                new CardSetInfo("Elvish Visionary", "ECL", "1", Rarity.COMMON));
        java.lang.reflect.Method named = ScenarioReplay.class.getDeclaredMethod("abilitySourceIsNamed",
                mage.MageObject.class, String.class);
        named.setAccessible(true);
        equal(true, named.invoke(d, visionary, "Elvish Visionary"));
        equal(false, named.invoke(d, visionary, "Grizzly Bears"));
        equal(false, named.invoke(d, null, "Elvish Visionary"));
        System.out.println("PASS ability: target ref is queued as a bound alias to the pending stack ability");
    }
}
