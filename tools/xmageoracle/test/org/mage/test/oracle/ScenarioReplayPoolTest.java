package org.mage.test.oracle;

import java.lang.reflect.Field;
import java.util.List;
import java.util.UUID;
import mage.ConditionalMana;
import mage.Mana;
import mage.MageObjectImpl;
import mage.cards.Card;
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
    }
}
