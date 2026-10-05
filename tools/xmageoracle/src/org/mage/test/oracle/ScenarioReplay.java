package org.mage.test.oracle;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.google.gson.JsonPrimitive;
import mage.Mana;
import mage.abilities.Ability;
import mage.abilities.common.SimpleStaticAbility;
import mage.abilities.costs.AlternativeSourceCosts;
import mage.cards.repository.CardInfo;
import mage.cards.repository.CardRepository;
import mage.abilities.effects.common.InfoEffect;
import mage.cards.Card;
import mage.cards.Cards;
import mage.cards.CardsImpl;
import mage.constants.CardType;
import mage.constants.Outcome;
import mage.constants.PhaseStep;
import mage.constants.Zone;
import mage.counters.Counter;
import mage.game.Game;
import mage.game.events.GameEvent;
import mage.game.permanent.Permanent;
import mage.game.stack.Spell;
import mage.game.stack.StackObject;
import mage.players.ManaPool;
import mage.players.Player;
import mage.filter.FilterCard;
import mage.target.TargetCard;
import mage.util.CardUtil;
import org.mage.test.player.TestPlayer;
import org.mage.test.serverside.base.CardTestPlayerBase;

import java.io.BufferedReader;
import java.io.FileReader;
import java.io.PrintWriter;
import java.io.FileWriter;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.UUID;

/**
 * Replays gorge oracle scenarios (rules/testdata/oracle schema) in XMage and
 * writes one result line per scenario: the canonical snapshot after setup and
 * after every step. Run with main(in.jsonl, out.jsonl) from Mage.Tests, so the
 * card database and deck files resolve. One JVM serves a whole batch; each
 * scenario starts from reset().
 *
 * Gorge-side vocabulary (step names, counter names, colours) is normalized by
 * the Go comparator, not here: this class reports what XMage sees.
 */
public class ScenarioReplay extends CardTestPlayerBase {

    private static final Gson GSON = new GsonBuilder().disableHtmlEscaping().serializeNulls().create();
    private static final int TURN = 1;
    private static final PhaseStep MAIN = PhaseStep.PRECOMBAT_MAIN;

    private final List<JsonObject> snaps = new ArrayList<>();
    // The step a cast/resolve/checkpoint is registered at. MAIN until an
    // attack or block op moves the scenario into combat; gorge plays the
    // cast in the declare-attackers (attacking-only) or declare-blockers
    // (blocking) step, so the driver must too.
    private PhaseStep phase = MAIN;

    // Whether the scenario scripts a divided-damage split ("<ref>^X=<n>"
    // target answers, oraclegen's damageSplitAnswers). Such a cast queues no
    // target names of its own: the split answers are the targets.
    private boolean splitScripted;

    // Override the factory the base class calls BEFORE it adds the player to
    // the game. Wrapping createPlayer(Game, ...)'s result instead copies a
    // player the game already holds, so scripted actions go to a player the
    // game never runs and every scenario ends with no snapshots.
    @Override
    protected TestPlayer createPlayer(String name, mage.constants.RangeOfInfluence rangeOfInfluence) {
        return new ScriptedChoicePlayer(new org.mage.test.player.TestComputerPlayer(name, rangeOfInfluence));
    }

    /** TestPlayer normally delegates these library decisions directly to its AI,
     * bypassing the scripted target/choice queues. Route them through this player. */
    private static final class ScriptedChoicePlayer extends TestPlayer {
        ScriptedChoicePlayer(org.mage.test.player.TestComputerPlayer computerPlayer) {
            super(computerPlayer);
        }

        ScriptedChoicePlayer(final ScriptedChoicePlayer player) {
            super(player);
        }

        @Override
        public ScriptedChoicePlayer copy() {
            return new ScriptedChoicePlayer(this);
        }

        @Override
        public boolean scry(int value, Ability source, Game game) {
            if (game.getTurnNum() == 1 && game.getStep() == null) {
                return false;
            }
            GameEvent event = new GameEvent(GameEvent.EventType.SCRY, getId(), source, getId(), value, true);
            if (game.replaceEvent(event)) {
                return false;
            }
            game.informPlayers(getLogName() + " scries " + event.getAmount() + CardUtil.getSourceLogName(game, source));
            Cards cards = new CardsImpl();
            cards.addAllCards(getLibrary().getTopCards(game, event.getAmount()));
            if (!cards.isEmpty()) {
                TargetCard target = new TargetCard(0, cards.size(), Zone.LIBRARY,
                        new FilterCard("card" + (cards.size() == 1 ? "" : "s") + " to PUT on the BOTTOM of your library (Scry)"));
                Cards selected = scriptedLibrarySelection(cards, game);
                if (selected == null) {
                    chooseTarget(Outcome.Benefit, cards, target, source, game);
                    selected = new CardsImpl(target.getTargets());
                }
                putCardsOnBottomOfLibrary(selected, game, source, true);
                if (!selected.isEmpty()) {
                    game.fireEvent(GameEvent.getEvent(GameEvent.EventType.SCRY_TO_BOTTOM, getId(), source, getId(), selected.size()));
                }
                cards.removeAll(selected);
                putCardsOnTopOfLibrary(cards, game, source, true);
            }
            game.fireEvent(new GameEvent(GameEvent.EventType.SCRIED, getId(), source, getId(), event.getAmount(), true));
            return true;
        }

        @Override
        public Player.SurveilResult doSurveil(int value, Ability source, Game game) {
            GameEvent event = new GameEvent(GameEvent.EventType.SURVEIL, getId(), source, getId(), value, true);
            if (game.replaceEvent(event) || event.getAmount() < 1) {
                return Player.SurveilResult.noSurveil();
            }
            game.informPlayers(getLogName() + " surveils " + event.getAmount() + CardUtil.getSourceLogName(game, source));
            Cards cards = new CardsImpl();
            cards.addAllCards(getLibrary().getTopCards(game, event.getAmount()));
            Cards graveyard = new CardsImpl();
            Cards top = new CardsImpl();
            if (!cards.isEmpty()) {
                TargetCard target = new TargetCard(0, cards.size(), Zone.LIBRARY,
                        new FilterCard("card" + (cards.size() == 1 ? "" : "s") + " to PUT into your GRAVEYARD (Surveil)"));
                Cards selected = scriptedLibrarySelection(cards, game);
                if (selected == null) {
                    chooseTarget(Outcome.Benefit, cards, target, source, game);
                    selected = new CardsImpl(target.getTargets());
                }
                if (!selected.isEmpty()) {
                    graveyard.addAllCards(moveCardsToGraveyardWithInfo(selected.getCards(game), source, game, Zone.LIBRARY));
                }
                cards.removeAll(selected);
                putCardsOnTopOfLibrary(cards, game, source, true);
                top.addAll(cards);
            }
            game.fireEvent(new GameEvent(GameEvent.EventType.SURVEILED, getId(), source, getId(), event.getAmount(), true));
            return Player.SurveilResult.surveil(graveyard, top);
        }

        /** Consume the generator's choice-queue selection and kept-card order.
         * Null means this decision was scripted through the ordinary target queue. */
        private Cards scriptedLibrarySelection(Cards cards, Game game) {
            List<String> queue = getChoices();
            if (queue.isEmpty()) {
                return null;
            }
            // Cards#getCards is a Set and does not promise library order.
            // Reconstruct the looked-at prefix from Library's ordered view.
            List<Card> lookedAtOrder = new ArrayList<>();
            for (Card card : getLibrary().getCards(game)) {
                if (cards.contains(card.getId())) {
                    lookedAtOrder.add(card);
                }
            }
            Set<Card> available = new java.util.LinkedHashSet<>(lookedAtOrder);
            Cards selected = new CardsImpl();
            boolean scripted = false;
            boolean selectionEnded = false;
            while (!queue.isEmpty()) {
                String answer = queue.get(0);
                if (TestPlayer.CHOICE_SKIP.equals(answer)) {
                    queue.remove(0);
                    scripted = true;
                    selectionEnded = true;
                    break;
                }
                Card match = findByName(available, answer);
                if (match == null) {
                    break;
                }
                queue.remove(0);
                available.remove(match);
                selected.add(match);
                scripted = true;
            }
            if (!scripted) {
                return null;
            }

            // The remaining labels are the chosen top-card ordering. The
            // generator repeats labels from the arrange decision here; a label
            // may therefore name a card already selected for the graveyard.
            // Match against the original look set to distinguish an answer for
            // this order prompt from an answer belonging to a later choice,
            // while only adding cards still available to the top of the library.
            Set<Card> lookedAt = new java.util.LinkedHashSet<>(lookedAtOrder);
            List<Card> ordered = new ArrayList<>();
            // A partial arrange emits each pick once for selection, then a
            // skip and exactly those same picks for order. The look set can
            // be larger than the pick set; counting it steals later answers.
            // A forced bottom order emits picks only once (no skip), so it
            // has no separate order answers to consume.
            int orderAnswersRemaining = selectionEnded ? selected.size() : 0;
            while (orderAnswersRemaining > 0 && !queue.isEmpty()) {
                String answer = queue.get(0);
                Card match = findByName(available, answer);
                if (match != null) {
                    queue.remove(0);
                    available.remove(match);
                    ordered.add(match);
                    orderAnswersRemaining--;
                    continue;
                }
                if (findByName(lookedAt, answer) == null) {
                    break;
                }
                // This order label names a card consumed during selection.
                // Consume it even though that card cannot be put back on top.
                queue.remove(0);
                orderAnswersRemaining--;
            }
            cards.clear();
            for (Card card : ordered) {
                cards.add(card);
            }
            // Any kept card without a distinct ordering label retains its
            // original look order; never let Set iteration determine library order.
            for (Card card : lookedAtOrder) {
                if (available.remove(card)) {
                    cards.add(card);
                }
            }
            return selected;
        }

        private Card findByName(Set<Card> cards, String name) {
            for (Card card : cards) {
                if (card.getName().equals(name)) {
                    return card;
                }
            }
            return null;
        }
    }

    public static void main(String[] args) throws Exception {
        if (args.length != 2) {
            System.err.println("usage: ScenarioReplay <in.jsonl> <out.jsonl>");
            System.exit(2);
        }
        ScenarioReplay r = new ScenarioReplay();
        int n = 0;
        try (BufferedReader in = new BufferedReader(new FileReader(args[0]));
             PrintWriter out = new PrintWriter(new FileWriter(args[1]))) {
            String line;
            while ((line = in.readLine()) != null) {
                if (line.trim().isEmpty()) {
                    continue;
                }
                JsonObject sc = JsonParser.parseString(line).getAsJsonObject();
                long t0 = System.nanoTime();
                JsonObject res = r.replay(sc);
                res.addProperty("ms", (System.nanoTime() - t0) / 1_000_000);
                out.println(GSON.toJson(res));
                out.flush();
                n++;
            }
        }
        System.err.println("ScenarioReplay: " + n + " scenarios");
        System.exit(0);
    }

    /**
     * Strict first: every XMage decision must be scripted. When XMage poses
     * a decision the script (derived from gorge's decisions) does not cover,
     * the scenario is replayed with strict mode off so XMage's own player
     * answers it, and the result says so: the outcome is still compared.
     * Leftover scripted answers alone (a decision gorge posed and XMage did
     * not) do not void a run that reached every checkpoint.
     */
    JsonObject replay(JsonObject sc) {
        JsonObject res = replayOnce(sc, true);
        String h = res.has("harness") ? res.get("harness").getAsString() : "";
        if (h.contains("Missing") && h.contains("def for turn")) {
            JsonObject loose = replayOnce(sc, false);
            loose.addProperty("strict_miss", h.length() > 300 ? h.substring(0, 300) : h);
            return loose;
        }
        return res;
    }

    JsonObject replayOnce(JsonObject sc, boolean strict) {
        JsonObject res = new JsonObject();
        res.addProperty("strict", strict);
        res.addProperty("name", str(sc, "name"));
        if (sc.has("id")) {
            res.add("id", sc.get("id"));
        }
        snaps.clear();
        try {
            reset();
            skipInitShuffling();
            setStrictChooseMode(strict);
            sc0 = sc;
            gorgeName = str(sc, "card");
            xmageName = str(sc, "xmage_name");
            cast.clear();
            refAlias.clear();
            phase = MAIN;
            build(sc);
            runCode("setup", TURN, MAIN, playerA, (info, p, g) -> {
                registerAliases(g);
                snaps.add(snapshot(info, g));
            });
            JsonArray steps = sc.has("steps") ? sc.getAsJsonArray("steps") : new JsonArray();
            JsonArray xans = sc.has("xmage_answers") && sc.get("xmage_answers").isJsonArray()
                    ? sc.getAsJsonArray("xmage_answers") : new JsonArray();
            splitScripted = xans.toString().contains("^X=");
            for (int i = 0; i < steps.size(); i++) {
                JsonObject st = steps.get(i).getAsJsonObject();
                String op = str(st, "op");
                if (i < xans.size() && xans.get(i).isJsonArray()) {
                    scripted(xans.get(i).getAsJsonArray());
                }
                step(st, op);
                String cp = "step " + i + " (" + op + ")";
                runCode(cp, TURN, phase, playerA, (info, p, g) -> snaps.add(snapshot(info, g)));
            }
            // EndTurn skips the remaining turn-1 checkpoints. Let the game reach
            // the next upkeep so a skipped final checkpoint can be represented
            // by the actual post-turn state rather than an unused-action error.
            setStopAt(TURN + 1, PhaseStep.UPKEEP);
            execute();
        } catch (Throwable t) {
            String msg = t.getClass().getSimpleName() + ": " + t.getMessage();
            int stepCount = sc.has("steps") ? sc.getAsJsonArray("steps").size() : 0;
            if (msg.contains("must have 0 actions but found")
                    && currentGame != null && currentGame.getTurnNum() > TURN
                    && snaps.size() < stepCount + 1) {
                int skipped = snaps.size() - 1;
                if (skipped >= 0 && skipped < stepCount) {
                    String op = str(sc.getAsJsonArray("steps").get(skipped).getAsJsonObject(), "op");
                    snaps.add(snapshot("step " + skipped + " (" + op + ")", currentGame));
                    msg = null;
                }
            }
            if (msg != null) {
                if (!xmageName.isEmpty()) {
                    // Name the card the way the scenario (and gorge) does.
                    msg = msg.replace(xmageName, gorgeName);
                }
                int want = stepCount + 1;
                if (snaps.size() == want && msg.contains("Count are not equal")) {
                    res.addProperty("leftover", msg.length() > 300 ? msg.substring(0, 300) : msg);
                } else {
                    res.addProperty("harness", msg.length() > 800 ? msg.substring(0, 800) : msg);
                }
            }
        }
        JsonArray arr = new JsonArray();
        for (JsonObject s : snaps) {
            arr.add(s);
        }
        res.add("snapshots", arr);
        return res;
    }

    // ---- setup -----------------------------------------------------------

    private TestPlayer seat(int i) {
        return i == 0 ? playerA : playerB;
    }

    private void build(JsonObject sc) {
        buildCounts.clear();
        String format = str(sc, "format");
        if (!format.isEmpty() && !format.equals("constructed")) {
            throw new IllegalArgumentException("unsupported format " + format);
        }
        JsonObject setup = sc.has("setup") ? sc.getAsJsonObject("setup") : new JsonObject();
        for (int i = 0; i < 2; i++) {
            TestPlayer p = seat(i);
            removeAllCardsFromLibrary(p);
            removeAllCardsFromHand(p);
            JsonObject s = setup.has("p" + i) ? setup.getAsJsonObject("p" + i) : new JsonObject();
            int named = 0;
            named += add(s, "battlefield", Zone.BATTLEFIELD, p);
            named += add(s, "hand", Zone.HAND, p);
            named += add(s, "graveyard", Zone.GRAVEYARD, p);
            named += add(s, "exile", Zone.EXILED, p);
            named += add(s, "library", Zone.LIBRARY, p);
            if (s.has("command")) {
                throw new IllegalArgumentException("command zone setup unsupported");
            }
            // library_top: first = top. addCard(LIBRARY) puts on top, so add
            // them last, bottom-most first, after the filler.
            List<String> top = names(s, "library_top");
            named += top.size();
            int filler = Math.max(0, 40 - named);
            if (filler > 0) {
                addCard(Zone.LIBRARY, p, "Wastes", filler);
            }
            for (int k = top.size() - 1; k >= 0; k--) {
                addCard(Zone.LIBRARY, p, top.get(k));
            }
            if (s.has("life")) {
                setLife(p, s.get("life").getAsInt());
            }
        }
    }

    private int add(JsonObject s, String key, Zone zone, TestPlayer p) {
        List<String> ns = names(s, key);
        // "tapped" names battlefield cards that start tapped (gorge's runner
        // taps every placement whose name it lists, oraclegen's ".tapped"
        // target slots: Push // Pull, Keep Out, Radiant Strike).
        List<String> tapped = zone == Zone.BATTLEFIELD ? names(s, "tapped") : new ArrayList<>();
        int i = p == playerA ? 0 : 1;
        for (String n : ns) {
            int k = buildCounts.merge(i + "|" + n, 1, Integer::sum);
            String ref = "p" + i + ":" + n + (k > 1 ? "#" + k : "");
            refAlias.put(ref, "@" + ref);
            addCard(zone, p, xmageSpelling(n), 1, tapped.contains(n));
        }
        return ns.size();
    }

    /** Binds one XMage alias per setup ref (the string build() recorded) to
     * the setup object itself, so a target ref naming two same-name cards
     * ("p0:Grizzly Bears#2") reaches exactly one of them. Counts follow the
     * zone order build() added them in. */
    private void registerAliases(Game g) {
        for (int i = 0; i < 2; i++) {
            TestPlayer pl = seat(i);
            Map<String, Integer> counts = new HashMap<>();
            Map<String, Integer> scenarioCounts = new HashMap<>();
            List<mage.MageObject> objs = new ArrayList<>();
            for (Permanent perm : g.getBattlefield().getAllPermanents()) {
                if (pl.getId().equals(perm.getControllerId())) {
                    objs.add(perm);
                }
            }
            for (Card c : pl.getHand().getCards(g)) objs.add(c);
            for (Card c : pl.getGraveyard().getCards(g)) objs.add(c);
            for (Card c : g.getExile().getAllCards(g)) {
                if (pl.getId().equals(c.getOwnerId())) objs.add(c);
            }
            for (Card c : pl.getLibrary().getCards(g)) objs.add(c);
            for (mage.MageObject o : objs) {
                String xmageCardName = o.getName();
                int k = counts.merge(xmageCardName, 1, Integer::sum);
                String xmageRef = "p" + i + ":" + xmageCardName + (k > 1 ? "#" + k : "");
                String scenarioCardName = !xmageName.isEmpty() && xmageCardName.equals(xmageName)
                        ? gorgeName : xmageCardName;
                int scenarioOccurrence = scenarioCounts.merge(scenarioCardName, 1, Integer::sum);
                String scenarioRef = "p" + i + ":" + scenarioCardName
                        + (scenarioOccurrence > 1 ? "#" + scenarioOccurrence : "");
                refAlias.put(scenarioRef, "@" + scenarioRef);
                refAlias.putIfAbsent(xmageRef, "@" + scenarioRef);
                // Both players must know every alias: the choosing player
                // resolves the target string, and it may be either seat.
                for (int j = 0; j < 2; j++) {
                    try {
                        seat(j).addAlias(scenarioRef, o.getId());
                    } catch (IllegalArgumentException ignored) {
                        // already bound on this player
                    }
                    if (!xmageRef.equals(scenarioRef)) {
                        try {
                            seat(j).addAlias(xmageRef, o.getId());
                        } catch (IllegalArgumentException ignored) {
                            // already bound on this player
                        }
                    }
                }
            }
        }
    }

    /** Whether the card's spell ability has a divided-amount target. */
    private static boolean spellTargetsDivided(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        if (c == null) {
            return false;
        }
        for (mage.target.Target t : c.getSpellAbility().getAllSelectedTargets()) {
            if (t instanceof mage.target.TargetAmount) {
                return true;
            }
        }
        return false;
    }

    /** Whether the card's spell ability has exactly one target object and
     * n answers already reach its maximum, so no slot is left to skip. */
    private static boolean singleTargetFilled(String name, int n) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        if (c == null) {
            return false;
        }
        List<mage.target.Target> ts = c.getSpellAbility().getAllSelectedTargets();
        return ts.size() == 1 && n >= ts.get(0).getMaxNumberOfTargets();
    }

    /** Whether the card carries the Gift keyword (CR 702.174). */
    private static boolean hasGift(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        return c != null && c.getAbilities().stream().anyMatch(a -> a.getClass().getSimpleName().startsWith("Gift"));
    }

    /** Whether the card carries an alternative cost XMage offers on a plain
     * cast (EvokeAbility, ImpendingAbility, DashAbility, ...). */
    private static boolean hasAlternativeSourceCost(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        if (info == null) {
            return false;
        }
        Card c = info.createCard();
        return c != null && c.getAbilities().stream().anyMatch(a -> a instanceof AlternativeSourceCosts);
    }

    /** The XMage target string for one scenario target ref: its alias when
     * setup bound one, else the stripped, XMage-spelled card name. */
    private String targetName(String ref) {
        String a = refAlias.get(ref);
        return a != null ? a : xmageSpelling(refName(ref));
    }

    private static List<String> names(JsonObject o, String key) {
        List<String> out = new ArrayList<>();
        if (o.has(key)) {
            for (JsonElement e : o.getAsJsonArray(key)) {
                out.add(e.getAsString());
            }
        }
        return out;
    }

    // ---- steps -----------------------------------------------------------

    private JsonObject sc0 = new JsonObject();
    private final List<String> cast = new ArrayList<>();
    // Setup objects the scenario can name by ref ("p0:Grizzly Bears#2"): the
    // XMage alias each ref is registered under, so two same-name permanents
    // are told apart. Filled during setup, read by step targeting.
    private final java.util.Map<String, String> refAlias = new java.util.LinkedHashMap<>();
    // Per-seat per-name occurrence counter build() uses to spell setup refs
    // ("p0:Grizzly Bears", "p0:Grizzly Bears#2") in the same order XMage
    // adds the cards, so registerAliases can bind each to its object.
    private final java.util.Map<String, Integer> buildCounts = new java.util.HashMap<>();
    // The card under test's two spellings: the scenario's (gorge/corpus) name
    // and XMage's card-database name when they differ (Forge prints "Dáin
    // Ironfoot", XMage stores "Dain Ironfoot"). xmageName is empty when equal.
    private String gorgeName = "";
    private String xmageName = "";

    /** The spelling to give XMage for a scenario card: the card under test's
     * XMage spelling, else the name unchanged. */
    private String xmageSpelling(String n) {
        return (!xmageName.isEmpty() && n.equals(gorgeName)) ? xmageName : n;
    }

    /** Rewrites XMage's spelling of the card under test back to the scenario's
     * (gorge) spelling throughout a value, so the comparator sees one name. */
    private JsonElement gorgeSpellings(JsonElement e) {
        if (xmageName.isEmpty()) {
            return e;
        }
        if (e.isJsonPrimitive() && e.getAsJsonPrimitive().isString()) {
            return e.getAsString().equals(xmageName) ? new JsonPrimitive(gorgeName) : e;
        }
        if (e.isJsonArray()) {
            JsonArray a = new JsonArray();
            for (JsonElement x : e.getAsJsonArray()) {
                a.add(gorgeSpellings(x));
            }
            return a;
        }
        if (e.isJsonObject()) {
            JsonObject o = new JsonObject();
            for (Map.Entry<String, JsonElement> en : e.getAsJsonObject().entrySet()) {
                o.add(en.getKey(), gorgeSpellings(en.getValue()));
            }
            return o;
        }
        return e;
    }

    private void step(JsonObject st, String op) {
        int seatIdx = st.has("seat") ? st.get("seat").getAsInt() : 0;
        TestPlayer p = seat(seatIdx);
        switch (op) {
            case "mana": {
                String mana = str(st, "mana");
                runCode("mana " + mana, TURN, phase, p, (info, pl, g) -> addPool(pl, g, mana));
                return;
            }
            case "attack": {
                // gorge's attack op declares the listed creatures attacking
                // the defender. XMage's attack() queues a selectAttackers
                // command at DECLARE_ATTACKERS; the scenario's later cast and
                // checkpoint happen in that same step.
                for (String a : names(st, "attackers")) {
                    attack(TURN, p, combatName(a), seat(seatOf(str(st, "defender"))));
                }
                phase = PhaseStep.DECLARE_ATTACKERS;
                return;
            }
            case "pass_to": {
                // Only the phase matters here; the scenario has already run
                // the ops that reach it. gorge's generated scenarios never
                // emit pass_to (a pre-block declare-blockers checkpoint is
                // not observable in XMage, whose engine selects blockers in
                // beginStep before any priority), so this is for completeness.
                String stepName = str(st, "step");
                String decision = str(st, "decision");
                if (decision.equals("blockers") || stepName.equals("declare-blockers")) {
                    phase = PhaseStep.DECLARE_BLOCKERS;
                } else if (stepName.equals("declare-attackers")) {
                    phase = PhaseStep.DECLARE_ATTACKERS;
                } else if (stepName.equals("main2")) {
                    phase = PhaseStep.POSTCOMBAT_MAIN;
                } else if (stepName.equals("main1") || stepName.isEmpty()) {
                    phase = MAIN;
                }
                return;
            }
            case "block": {
                // gorge's blocks are [blocker, attacker] pairs. XMage's
                // block() queues a declareBlockers command at DECLARE_BLOCKERS.
                for (JsonElement e : st.getAsJsonArray("blocks")) {
                    JsonArray pair = e.getAsJsonArray();
                    block(TURN, p, combatName(pair.get(0).getAsString()), combatName(pair.get(1).getAsString()));
                }
                phase = PhaseStep.DECLARE_BLOCKERS;
                return;
            }
            case "cast": {
                if (st.has("mana")) {
                    String mana = str(st, "mana");
                    runCode("mana " + mana, TURN, phase, p, (info, pl, g) -> addPool(pl, g, mana));
                }
                if (st.has("kicked") || st.has("cast_mode")) {
                    throw new IllegalArgumentException("kicked/cast_mode unsupported");
                }
                if (!sc0.has("xmage_answers")) {
                    answers(st, p);
                }
                String card = xmageSpelling(refName(str(st, "card")));
                List<String> tg = targets(st);
                if (hasAlternativeSourceCost(card)) {
                    // A plain cast step: gorge paid the mana cost, so decline
                    // the alternative cost (evoke, impending, dash, ...)
                    // XMage offers through its "Cast with no alternative
                    // cost" choice, rather than leave it to the AI.
                    setChoice(p, "Cast with no alternative cost");
                }
                if (splitScripted && spellTargetsDivided(card)) {
                    // The scripted "<ref>^X=<share>" answers name the targets
                    // and gorge's split; a target string here would be a
                    // second, unconsumed set.
                    castSpell(TURN, phase, p, card);
                    cast.add(card);
                    return;
                }
                if (tg.size() == 1 && isSeatRef(tg.get(0)) && !hasGift(card)) {
                    castSpell(TURN, phase, p, card, seat(seatOf(tg.get(0))));
                } else if (tg.size() == 1 && isSeatRef(tg.get(0))) {
                    // A Gift spell (Mind Spiral, Sazacap's Brew): the castSpell
                    // player form binds the wrong ask, so queue the spell's
                    // own player target and close the rest.
                    addTarget(p, seat(seatOf(tg.get(0))));
                    addTarget(p, TestPlayer.TARGET_SKIP);
                    castSpell(TURN, phase, p, card);
                } else if (tg.isEmpty()) {
                    castSpell(TURN, phase, p, card);
                    cast.add(card);
                    return;
                } else if (tg.size() == 1 && cast.contains(xmageSpelling(refName(tg.get(0))))) {
                    // Targeting a spell cast by an earlier step: wait for it
                    // on the stack. cast holds spelled card names and the
                    // target is a scenario ref; the setup alias names the
                    // card in hand, not the spell, so target by name.
                    String spell = xmageSpelling(refName(tg.get(0)));
                    castSpell(TURN, phase, p, card, spell, spell);
                } else if (tg.size() == 1) {
                    // A single target goes through XMage's own string form, so
                    // a divided-damage target (TargetAmount) still lets XMage
                    // pick the split as it always did.
                    castSpell(TURN, phase, p, card, targetName(tg.get(0)));
                } else {
                    // Two or more targets: queue each through addTarget and
                    // cast with no $target, so an "up to N" slot stays open
                    // and same-name permanents are told apart by the alias
                    // each ref carries. A trailing skip closes any slot XMage
                    // offers that this scenario did not fill (a reflexive
                    // sub-ability with no legal target, say).
                    if (spellTargetsDivided(card)) {
                        // A divided-amount target (TargetAmount: Biogenic
                        // Upgrade, Synchronized Charge) takes the whole set
                        // as one castSpell string and lets XMage split it,
                        // as the single-target form does; addTarget's alias
                        // answers are rejected ("Must be target amount").
                        List<String> names = new ArrayList<>();
                        for (String t : tg) {
                            names.add(xmageSpelling(refName(t)));
                        }
                        castSpell(TURN, phase, p, card, String.join("^", names));
                        cast.add(card);
                        return;
                    }
                    for (String t : tg) {
                        queueCastTarget(p, t);
                    }
                    if (!singleTargetFilled(card, tg.size())) {
                        // Close an "up to N" slot the scenario left short,
                        // or a later slot (Rhino's Rampage's reflexive
                        // trigger). A skip after the one multi-target slot
                        // is filled is rejected (Pull Through the Weft).
                        addTarget(p, TestPlayer.TARGET_SKIP);
                    }
                    castSpell(TURN, phase, p, card);
                }
                cast.add(card);
                return;
            }
            case "play":
                playLand(TURN, phase, p, xmageSpelling(refName(str(st, "card"))));
                return;
            case "resolve":
                // gorge's resolve op passes priority until the stack is empty.
                waitStackResolved(TURN, phase, p);
                return;
            default:
                throw new IllegalArgumentException("op " + op + " unsupported");
        }
    }

    /** Applies the generator's scripted answers (oraclegen.XAnswer). */
    private void scripted(JsonArray as) {
        for (JsonElement e : as) {
            JsonObject a = e.getAsJsonObject();
            TestPlayer p = seat(a.get("seat").getAsInt());
            String kind = str(a, "kind");
            String v = str(a, "value");
            switch (kind) {
                case "target":
                    if (isSeatRef(v)) {
                        addTarget(p, seat(seatOf(v)));
                    } else if (v.contains("^X=")) {
                        // Divided damage: the ref carries its share.
                        String[] parts = v.split("\\^X=", 2);
                        String nm = isSeatRef(parts[0]) ? "targetPlayer=" + seat(seatOf(parts[0])).getName() : targetName(parts[0]);
                        addTarget(p, nm + "^X=" + parts[1]);
                    } else {
                        addTarget(p, v.equals("[target_skip]") ? TestPlayer.TARGET_SKIP : targetName(v));
                    }
                    break;
                case "amount":
                    setChoiceAmount(p, Integer.parseInt(v));
                    break;
                case "mode":
                    setModeChoice(p, v);
                    break;
                case "choice":
                    if (v.equals("yes") || v.equals("no")) {
                        setChoice(p, v.equals("yes"));
                    } else if (isSeatRef(v)) {
                        setChoice(p, seat(seatOf(v)).getName());
                    } else {
                        setChoice(p, v.equals("[choice_skip]") ? TestPlayer.CHOICE_SKIP : v);
                    }
                    break;
                default:
                    throw new IllegalArgumentException("xmage answer kind " + kind);
            }
        }
    }

    private void answers(JsonObject st, TestPlayer p) {
        if (!st.has("answers")) {
            return;
        }
        for (JsonElement e : st.getAsJsonArray("answers")) {
            JsonObject a = e.getAsJsonObject();
            String kind = str(a, "kind");
            List<String> pick = names(a, "pick");
            switch (kind) {
                case "target":
                    for (String t : pick) {
                        if (isSeatRef(t)) {
                            addTarget(p, seat(seatOf(t)));
                        } else {
                            addTarget(p, targetName(t));
                        }
                    }
                    break;
                case "yesno":
                case "trigger_optional":
                    for (String t : pick) {
                        setChoice(p, t.equalsIgnoreCase("yes") || t.equalsIgnoreCase("true"));
                    }
                    break;
                case "modes":
                case "choose":
                    // gorge-side answers; XMage's come from xmage_answers.
                    break;
                default:
                    throw new IllegalArgumentException("answer kind " + kind + " unsupported");
            }
        }
    }

    private List<String> targets(JsonObject st) {
        return new ArrayList<>(names(st, "targets"));
    }

    private static boolean isSeatRef(String s) {
        return s.matches("p[0-9]+");
    }

    private static int seatOf(String s) {
        return Integer.parseInt(s.substring(1));
    }

    /** Queue one cast target ref on the target queue. */
    private void queueCastTarget(TestPlayer p, String t) {
        if (isSeatRef(t)) {
            addTarget(p, seat(seatOf(t)));
        } else if (cast.contains(xmageSpelling(refName(t)))) {
            // A spell an earlier step cast: its setup alias names the card
            // in hand, not the spell.
            addTarget(p, xmageSpelling(refName(t)));
        } else {
            addTarget(p, targetName(t));
        }
    }

    /** The name form XMage's attack/block command takes. Unlike a cast
     * target, the command does not accept the driver's "@" aliases, so it
     * must be the card name, with the legacy zero-based "<name>:<index>"
     * suffix for a duplicate ("p0:Grizzly Bears#2" -> "Grizzly Bears:1"). */
    private static String combatName(String ref) {
        String n = refName(ref);
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            int k = Integer.parseInt(ref.substring(hash + 1)) - 1;
            if (k > 0) {
                return n + ":" + k;
            }
        }
        return n;
    }

    /** "p1:token:Name#2" -> "Name". */
    static String refName(String ref) {
        // Only a leading seat ref ("p0:") is a prefix; a card name may carry
        // its own colon ("Summon: Bahamut").
        int i = ref.indexOf(':');
        String n = i >= 0 && ref.substring(0, i).matches("p[0-9]+") ? ref.substring(i + 1) : ref;
        if (n.startsWith("token:")) {
            n = n.substring("token:".length());
        }
        int j = n.lastIndexOf('#');
        if (j >= 0 && n.substring(j + 1).matches("[0-9]+")) {
            n = n.substring(0, j);
        }
        return n;
    }

    private static final Ability MANA_SOURCE = new SimpleStaticAbility(Zone.ALL, new InfoEffect("oracle scenario mana"));

    private static void addPool(Player pl, Game g, String letters) {
        Mana m = new Mana();
        for (char c : letters.toUpperCase().toCharArray()) {
            switch (c) {
                case 'W': m.increaseWhite(); break;
                case 'U': m.increaseBlue(); break;
                case 'B': m.increaseBlack(); break;
                case 'R': m.increaseRed(); break;
                case 'G': m.increaseGreen(); break;
                case 'C': m.increaseColorless(); break;
                default: throw new IllegalArgumentException("mana letter " + c);
            }
        }
        pl.getManaPool().addMana(m, g, MANA_SOURCE);
    }

    // ---- snapshot ----------------------------------------------------------

    private int seatIndex(Game g, UUID id) {
        if (id == null) {
            return -1;
        }
        return id.equals(playerA.getId()) ? 0 : id.equals(playerB.getId()) ? 1 : -1;
    }

    private JsonObject snapshot(String checkpoint, Game g) {
        JsonObject s = new JsonObject();
        s.addProperty("checkpoint", checkpoint);
        s.addProperty("turn", g.getTurnNum());
        s.addProperty("step", g.getTurnStepType() == null ? "" : g.getTurnStepType().name());
        s.addProperty("active", seatIndex(g, g.getActivePlayerId()));
        s.addProperty("priority", seatIndex(g, g.getPriorityPlayerId()));
        s.addProperty("over", g.hasEnded());
        JsonArray players = new JsonArray();
        for (int i = 0; i < 2; i++) {
            Player p = g.getPlayer(seat(i).getId());
            JsonObject po = new JsonObject();
            po.addProperty("seat", i);
            po.addProperty("life", p.getLife());
            JsonObject pc = new JsonObject();
            for (Counter c : p.getCountersAsCopy().values()) {
                pc.addProperty(c.getName(), c.getCount());
            }
            if (pc.size() > 0) {
                po.add("counters", pc);
            }
            po.add("hand", sortedNames(p.getHand().getCards(g)));
            JsonArray gy = new JsonArray();
            for (Card c : p.getGraveyard().getCards(g)) {
                gy.add(c.getName());
            }
            po.add("graveyard", gy);
            List<String> ex = new ArrayList<>();
            for (Card c : g.getExile().getAllCards(g)) {
                if (p.getId().equals(c.getOwnerId())) {
                    ex.add(c.getName());
                }
            }
            Collections.sort(ex);
            po.add("exile", GSON.toJsonTree(ex));
            po.add("command", new JsonArray());
            po.addProperty("library_count", p.getLibrary().size());
            JsonArray top = new JsonArray();
            List<Card> lib = p.getLibrary().getCards(g);
            for (int k = 0; k < lib.size() && k < 5; k++) {
                top.add(lib.get(k).getName());
            }
            po.add("library_top", top);
            po.addProperty("pool", pool(p.getManaPool()));
            players.add(po);
        }
        s.add("players", players);
        JsonArray perms = new JsonArray();
        for (Permanent perm : g.getBattlefield().getAllPermanents()) {
            JsonObject o = new JsonObject();
            o.addProperty("name", perm.getName());
            o.addProperty("controller", seatIndex(g, perm.getControllerId()));
            o.addProperty("owner", seatIndex(g, perm.getOwnerId()));
            o.addProperty("token", perm.isToken());
            o.addProperty("tapped", perm.isTapped());
            o.addProperty("face_down", perm.isFaceDown(g));
            if (perm.isCreature(g)) {
                o.addProperty("pt", perm.getPower().getValue() + "/" + perm.getToughness().getValue());
            }
            o.addProperty("damage", perm.getDamage());
            JsonObject cs = new JsonObject();
            for (Counter c : perm.getCounters(g).values()) {
                cs.addProperty(c.getName(), c.getCount());
            }
            if (cs.size() > 0) {
                o.add("counters", cs);
            }
            List<String> types = new ArrayList<>();
            for (CardType t : perm.getCardType(g)) {
                types.add(t.toString());
            }
            perm.getSubtype(g).forEach(st -> types.add(st.toString()));
            perm.getSuperType(g).forEach(st -> types.add(st.toString()));
            Collections.sort(types);
            o.add("types", GSON.toJsonTree(types));
            if (perm.isAllCreatureTypes(g)) {
                // Changeling / "is every creature type": the type list above
                // is the printed subtypes only, so the comparator needs this
                // marker to see the all-types state.
                o.addProperty("all_creature_types", true);
            }
            o.addProperty("colors", perm.getColor(g).toString());
            if (perm.getAttachedTo() != null) {
                Permanent to = g.getPermanent(perm.getAttachedTo());
                if (to != null) {
                    o.addProperty("attached_to", to.getName());
                } else {
                    int si = seatIndex(g, perm.getAttachedTo());
                    o.addProperty("attached_to", si >= 0 ? "p" + si : "?");
                }
            }
            o.addProperty("attacking", perm.isAttacking());
            o.addProperty("blocking", perm.getBlocking() > 0);
            perms.add(o);
        }
        s.add("permanents", perms);
        JsonArray stack = new JsonArray();
        for (StackObject so : g.getStack()) { // top first
            JsonObject o = new JsonObject();
            o.addProperty("kind", so instanceof Spell ? "spell" : "ability");
            String src = so.getName();
            if (!(so instanceof Spell)) {
                mage.MageObject mo = g.getObject(so.getSourceId());
                src = mo == null ? "?" : mo.getName();
            }
            o.addProperty("source", src);
            o.addProperty("controller", seatIndex(g, so.getControllerId()));
            stack.add(o);
        }
        s.add("stack", stack);
        return (JsonObject) gorgeSpellings(s);
    }

    private static JsonArray sortedNames(java.util.Collection<Card> cs) {
        List<String> ns = new ArrayList<>();
        for (Card c : cs) {
            ns.add(c.getName());
        }
        Collections.sort(ns);
        JsonArray a = new JsonArray();
        ns.forEach(a::add);
        return a;
    }

    private static String pool(ManaPool mp) {
        StringBuilder b = new StringBuilder();
        rep(b, 'W', mp.getWhite());
        rep(b, 'U', mp.getBlue());
        rep(b, 'B', mp.getBlack());
        rep(b, 'R', mp.getRed());
        rep(b, 'G', mp.getGreen());
        rep(b, 'C', mp.getColorless());
        return b.toString();
    }

    private static void rep(StringBuilder b, char c, int n) {
        for (int i = 0; i < n; i++) {
            b.append(c);
        }
    }

    private static String str(JsonObject o, String k) {
        return o.has(k) && !o.get(k).isJsonNull() ? o.get(k).getAsString() : "";
    }
}
