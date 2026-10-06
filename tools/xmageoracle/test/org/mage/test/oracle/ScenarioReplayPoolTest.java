package org.mage.test.oracle;

import java.lang.reflect.Field;
import java.util.List;
import java.util.UUID;
import mage.ConditionalMana;
import mage.Mana;
import mage.MageObjectImpl;
import mage.players.ManaPool;
import mage.players.ManaPoolItem;

/** Executable check of the driver's pool snapshot, without H2 or a game.
 * Run scripts/xmage-oracle-test-pool.sh. Boommobile (restricted to abilities)
 * and Desolation of Smaug (restricted to Dragon spells) add ConditionalMana,
 * which ManaPool.getWhite() and the other colour getters do not count. */
public final class ScenarioReplayPoolTest {
    private static void equal(Object want, Object got) {
        if (!want.equals(got)) {
            throw new AssertionError("expected " + want + ", got " + got);
        }
    }

    private static final class Source extends MageObjectImpl {
        Source() {
            this.name = "Restricted source";
        }

        @Override
        public Source copy() {
            return this;
        }
    }

    @SuppressWarnings("unchecked")
    private static void add(ManaPool pool, ManaPoolItem item) throws Exception {
        Field f = ManaPool.class.getDeclaredField("manaItems");
        f.setAccessible(true);
        ((List<ManaPoolItem>) f.get(pool)).add(item);
    }

    private static void conditional(ManaPool pool, Mana mana) throws Exception {
        Source src = new Source();
        add(pool, new ManaPoolItem(new ConditionalMana(mana), src, UUID.randomUUID()));
    }

    public static void main(String[] args) throws Exception {
        ManaPool empty = new ManaPool(UUID.randomUUID());
        equal("", ScenarioReplay.pool(empty));

        // Boommobile: four mana of one colour, spendable only on abilities.
        ManaPool boom = new ManaPool(UUID.randomUUID());
        conditional(boom, new Mana(4, 0, 0, 0, 0, 0, 0, 0));
        equal(1, boom.getConditionalMana().size());
        equal(4, boom.getConditionalMana().get(0).getWhite());
        equal(0, boom.getWhite());
        equal("WWWW", ScenarioReplay.pool(boom));
        System.out.println("PASS conditional WWWW is counted although getWhite() is 0");

        // Desolation of Smaug: four mana in any combination.
        ManaPool smaug = new ManaPool(UUID.randomUUID());
        conditional(smaug, new Mana(1, 1, 1, 1, 0, 0, 0, 0));
        equal("WUBR", ScenarioReplay.pool(smaug));
        System.out.println("PASS conditional WUBR");

        // Plain and conditional mana are summed, in WUBRGC order.
        ManaPool mixed = new ManaPool(UUID.randomUUID());
        add(mixed, new ManaPoolItem(0, 0, 0, 1, 0, 1, new Source(), UUID.randomUUID(), false));
        conditional(mixed, new Mana(1, 0, 0, 0, 1, 0, 0, 1));
        equal("WWGCC", ScenarioReplay.pool(mixed));
        System.out.println("PASS plain + conditional");

        // Multiple conditional entries, every colour, and generic folded into C.
        ManaPool all = new ManaPool(UUID.randomUUID());
        add(all, new ManaPoolItem(1, 1, 1, 1, 1, 1, new Source(), UUID.randomUUID(), false));
        equal(0, all.getConditionalMana().size());
        equal("WUBRGC", ScenarioReplay.pool(all));
        conditional(all, new Mana(1, 2, 3, 4, 5, 1, 0, 2));
        conditional(all, new Mana(1, 1, 1, 1, 1, 0, 0, 1));
        equal(2, all.getConditionalMana().size());
        equal(1, all.getWhite());
        equal(1, all.getBlue());
        equal(1, all.getBlack());
        equal(1, all.getRed());
        equal(1, all.getGreen());
        equal(1, all.getColorless());
        equal("WWWUUUUBBBBBRRRRRRGGGGGGGCCCCC", ScenarioReplay.pool(all));
        System.out.println("PASS all colours + multiple conditional entries + generic");
    }
}
