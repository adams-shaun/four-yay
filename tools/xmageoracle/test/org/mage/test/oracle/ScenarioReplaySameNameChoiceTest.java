package org.mage.test.oracle;

import java.lang.reflect.Proxy;
import java.util.List;
import java.util.UUID;
import mage.game.permanent.Permanent;

/** No H2: prove that token refs rank by creation, not battlefield iteration. */
public final class ScenarioReplaySameNameChoiceTest {
    private static Permanent object(UUID controller, boolean token, int order) {
        UUID id = UUID.randomUUID();
        return (Permanent) Proxy.newProxyInstance(Permanent.class.getClassLoader(),
                new Class<?>[]{Permanent.class}, (proxy, method, args) -> {
                    switch (method.getName()) {
                        case "getId": return id;
                        case "getControllerId": return controller;
                        case "getName": return "Grizzly Bears";
                        case "isToken": return token;
                        case "getCreateOrder": return order;
                        default: throw new AssertionError("unexpected call " + method);
                    }
                });
    }

    private static void chosen(Permanent want, Permanent got) {
        if (want != got) {
            throw new AssertionError("choice selected the wrong object");
        }
    }

    private static void set(Object target, Class<?> type, String name, Object value) throws Exception {
        java.lang.reflect.Field f = type.getDeclaredField(name);
        f.setAccessible(true);
        f.set(target, value);
    }

    /** Exercise the production name matcher, including a pre-bound STALE
     * alias. Only the choice path may override that alias by live position. */
    private static void liveMatcher() throws Exception {
        java.lang.reflect.Field f = sun.misc.Unsafe.class.getDeclaredField("theUnsafe");
        f.setAccessible(true);
        sun.misc.Unsafe unsafe = (sun.misc.Unsafe) f.get(null);
        ScenarioReplay owner = (ScenarioReplay) unsafe.allocateInstance(ScenarioReplay.class);
        Class<?> type = Class.forName("org.mage.test.oracle.ScenarioReplay$ScriptedChoicePlayer");
        java.lang.reflect.Constructor<?> ctor = type.getDeclaredConstructor(
                org.mage.test.player.TestComputerPlayer.class, ScenarioReplay.class);
        ctor.setAccessible(true);
        org.mage.test.player.TestPlayer player = (org.mage.test.player.TestPlayer) ctor.newInstance(
                new org.mage.test.player.TestComputerPlayer("PlayerA", mage.constants.RangeOfInfluence.ALL), owner);
        // playerA lives on an ancestor of the driver.
        for (Class<?> c = ScenarioReplay.class; c != null; c = c.getSuperclass()) {
            try {
                set(owner, c, "playerA", player);
                break;
            } catch (NoSuchFieldException absent) { }
        }
        UUID own = player.getId();
        Permanent first = object(own, true, 1), second = object(own, true, 2);
        mage.game.permanent.Battlefield board = new mage.game.permanent.Battlefield();
        board.addPermanent(second);
        player.addAlias("p0:token:Grizzly Bears", first.getId());
        if (!player.hasObjectTargetNameOrAlias(first, "@p0:token:Grizzly Bears")) {
            throw new AssertionError("precondition: base matcher must see the stale alias outside a choice");
        }
        mage.game.Game game = (mage.game.Game) Proxy.newProxyInstance(mage.game.Game.class.getClassLoader(),
                new Class<?>[]{mage.game.Game.class}, (proxy, method, args) -> {
                    if (method.getName().equals("getBattlefield")) return board;
                    throw new AssertionError("unexpected game call " + method);
                });
        set(player, type, "choiceGame", game);
        if (player.hasObjectTargetNameOrAlias(first, "@p0:token:Grizzly Bears")
                || !player.hasObjectTargetNameOrAlias(second, "@p0:token:Grizzly Bears")) {
            throw new AssertionError("live choice must select the remaining token, not the stale alias");
        }
        set(player, type, "choiceGame", null);
        if (!player.hasObjectTargetNameOrAlias(first, "@p0:token:Grizzly Bears")) {
            throw new AssertionError("non-choice alias matching changed");
        }
        System.out.println("PASS production choice matcher overrides stale token aliases only inside a choice");
    }

    public static void main(String[] args) throws Exception {
        UUID own = UUID.randomUUID(), other = UUID.randomUUID();
        Permanent original = object(own, false, 1);
        Permanent first = object(own, true, 2);
        Permanent second = object(own, true, 3);
        Permanent opponent = object(other, true, 4);
        if (first.getId().equals(second.getId()) || first.getCreateOrder() >= second.getCreateOrder()
                || !first.isToken() || original.isToken()) {
            throw new AssertionError("invalid duplicate-token fixture");
        }
        // Both iteration orders select the exact same second-created token.
        for (List<Permanent> battlefield : List.of(List.of(second, opponent, original, first),
                List.of(first, original, opponent, second))) {
            chosen(first, ScenarioReplay.tokenChoice("p0:token:Grizzly Bears", own, battlefield));
            chosen(second, ScenarioReplay.tokenChoice("p0:token:Grizzly Bears#2", own, battlefield));
            chosen(opponent, ScenarioReplay.tokenChoice("p1:token:Grizzly Bears", other, battlefield));
        }
        // Refs are positional among LIVE tokens: after a sacrifice the former
        // second token becomes #1. An old, setup-time alias must not shadow it.
        chosen(second, ScenarioReplay.tokenChoice("p0:token:Grizzly Bears", own, List.of(second)));
        chosen(null, ScenarioReplay.tokenChoice("p0:token:Grizzly Bears#2", own, List.of(second)));
        System.out.println("PASS exact token choice is creation-ordered, controller-scoped and live");
        liveMatcher();
    }
}
