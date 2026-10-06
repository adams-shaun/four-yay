package org.mage.test.oracle;

import com.google.gson.Gson;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.google.gson.JsonPrimitive;
import mage.ConditionalMana;
import mage.Mana;
import mage.abilities.Ability;
import mage.abilities.common.SimpleStaticAbility;
import mage.abilities.mana.ActivatedManaAbilityImpl;
import mage.abilities.costs.AlternativeSourceCosts;
import mage.abilities.costs.OptionalAdditionalSourceCosts;
import mage.abilities.costs.OrCost;
import mage.abilities.keyword.LeylineAbility;
import mage.cards.repository.CardInfo;
import mage.cards.repository.CardRepository;
import mage.abilities.effects.common.EndTurnEffect;
import mage.abilities.effects.common.InfoEffect;
import mage.cards.Card;
import mage.cards.Cards;
import mage.cards.CardsImpl;
import mage.cards.DoubleFacedCard;
import mage.constants.CardType;
import mage.constants.Outcome;
import mage.constants.PhaseStep;
import mage.constants.Zone;
import mage.counters.Counter;
import mage.counters.CounterType;
import mage.game.Game;
import mage.game.PutToBattlefieldInfo;
import mage.game.events.GameEvent;
import mage.game.permanent.Permanent;
import mage.game.stack.Spell;
import mage.game.stack.StackObject;
import mage.players.ManaPool;
import mage.players.Player;
import mage.filter.FilterCard;
import mage.target.TargetCard;
import mage.util.CardUtil;
import org.mage.test.player.PlayerAction;
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
    private int TURN = 1;
    private int turn = 1;
    private int activeSeat = 0;
    private static final PhaseStep MAIN = PhaseStep.PRECOMBAT_MAIN;
    private static final Set<String> SPREE_CARDS = Set.of(
            "Dance of the Tumbleweeds", "Getaway Glamer", "Great Train Heist",
            "Insatiable Avarice", "Jailbreak Scheme", "Lively Dirge",
            "Metamorphic Blast", "Rush of Dread", "Shifting Grift",
            "Smuggler's Surprise", "Unfortunate Accident");

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
        return new ScriptedChoicePlayer(new org.mage.test.player.TestComputerPlayer(name, rangeOfInfluence), this);
    }

    // The gorge-side "choose" picks of the cast step being queued (its
    // recorded answers), so an either-or cost ask XMage poses can be answered
    // the way gorge answered it. Empty when the step recorded none.
    private List<String> castCostPicks = new ArrayList<>();

    /** TestPlayer normally delegates these library decisions directly to its AI,
     * bypassing the scripted target/choice queues. Route them through this player. */
    private static final class ScriptedChoicePlayer extends TestPlayer {
        // The replay that built this player: it holds the cast step's recorded
        // cost picks. Carried through copy(), as XMage copies players freely.
        private final ScenarioReplay owner;

        ScriptedChoicePlayer(org.mage.test.player.TestComputerPlayer computerPlayer, ScenarioReplay owner) {
            super(computerPlayer);
            this.owner = owner;
        }

        ScriptedChoicePlayer(final ScriptedChoicePlayer player) {
            super(player);
            this.owner = player.owner;
        }

        /** Whether the current ask is posed from inside the named method of a
         * class of the given type (a keyword's cast-time cost hook). */
        private static boolean askedBy(Class<?> type, String method) {
            return StackWalker.getInstance(StackWalker.Option.RETAIN_CLASS_REFERENCE).walk(frames ->
                    frames.anyMatch(f -> f.getMethodName().equals(method) && type.isAssignableFrom(f.getDeclaringClass())));
        }

        private boolean scriptedYesNoNext() {
            return !getChoices().isEmpty() && (getChoices().get(0).equals("Yes") || getChoices().get(0).equals("No"));
        }

        /**
         * The yes/no asks a plain cast step implies. gorge's cast-resolve
         * scenario casts for the mana cost only, so each of these is answered
         * as gorge played it, keyed on the ask's type, not the card:
         * the opening-hand Leyline ask (LeylineAbility) is No; every optional
         * additional cost (kicker, offspring, waterbend: the
         * OptionalAdditionalSourceCosts hook; XMage's mana costs always report
         * canPay, so it asks even when the pool is short) is No, consuming the
         * generator's own leading "No" when it scripted one; an either-or
         * additional cost (OrCost) takes the cost gorge recorded for the step.
         * Every other ask stays with the scripted queue.
         */
        @Override
        public boolean chooseUse(Outcome outcome, String message, String secondMessage, String trueText, String falseText, Ability source, Game game) {
            if (source instanceof LeylineAbility) {
                return false;
            }
            if (askedBy(OptionalAdditionalSourceCosts.class, "addOptionalAdditionalCosts")) {
                if (!getChoices().isEmpty() && getChoices().get(0).equals("No")) {
                    return super.chooseUse(outcome, message, secondMessage, trueText, falseText, source, game);
                }
                return false;
            }
            if (askedBy(OrCost.class, "pay") && !scriptedYesNoNext()) {
                // The generator scripts the boolean itself when two costs were
                // payable for gorge; with one payable it scripts none, but
                // XMage still asks, so answer the cost gorge recorded.
                Boolean first = owner.recordedCostIsFirst(trueText, falseText);
                if (first != null) {
                    return first;
                }
            }
            return super.chooseUse(outcome, message, secondMessage, trueText, falseText, source, game);
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

        /**
         * The base TestPlayer handles a TargetSpellOrPermanent's stack half
         * only: its "stack" branch in chooseTarget searches game.getStack()
         * and then asserts when the queue is still non-empty, so a
         * battlefield permanent answer can never be matched through the
         * addTarget queue (Aang, Swift Savior's airbend "up to one other
         * target creature or spell" is a resolve-step trigger; Jeskai
         * Revelation's multi-target cast is the same gap). The cast/activate
         * path (handleNonPlayerTargetTarget) already matches such an answer
         * by name or alias against the target's own possibleTargets, so
         * mirror that here for the battlefield half the base omits, and leave
         * every other class -- including a spell on the stack, which the base
         * does handle -- to super. This lives in the tracked driver, not in
         * the out-of-tree TestPlayer, so it survives an XMAGE_REF bump.
         */
        @Override
        public boolean chooseTarget(Outcome outcome, mage.target.Target target, Ability source, Game game) {
            // TestPlayer.chooseTarget runs its zone matcher over every queued
            // answer and rejects a "[target_skip]" that is not at the queue
            // front (checkTargetDefinitionMarksSupport), so a skip queued for
            // a LATER target object (Rise from the Wreck's empty Mount slot)
            // is hidden from this ask and restored, in order, afterwards.
            List<String> later = hideAfterNextSkip(getTargets());
            try {
                return chooseTargetInSegment(outcome, target, source, game);
            } finally {
                getTargets().addAll(later);
            }
        }

        /** Detaches and returns the queue's suffix that starts at its next
         * "[target_skip]" so only the contiguous segment that precedes it is
         * visible. A skip at the front (consumed by this ask) or no skip at all
         * detaches nothing. */
        static List<String> hideAfterNextSkip(List<String> queue) {
            int next = queue.indexOf(TestPlayer.TARGET_SKIP);
            if (next <= 0) {
                return new ArrayList<>();
            }
            List<String> tail = queue.subList(next, queue.size());
            List<String> hidden = new ArrayList<>(tail);
            tail.clear();
            return hidden;
        }

        private boolean chooseTargetInSegment(Outcome outcome, mage.target.Target target, Ability source, Game game) {
            // An adjusted spell's open slot is closed only when XMage asks
            // again after the scenario's answers: a slot whose candidates are
            // exhausted never asks, so a skip queued up front would be left
            // unused and fail assertAllCommandsUsed.
            closeAskedAgain(getTargets(), owner.isAdjustedSpellAsk(source, game));
            mage.target.Target orig = target.getOriginalTarget();
            if (orig instanceof mage.target.common.TargetSpellOrPermanent
                    && !getTargets().isEmpty()
                    && !TestPlayer.TARGET_SKIP.equals(getTargets().get(0))) {
                // The base's own controller derivation (TestPlayer.chooseTarget):
                // the target's ability controller when set, else this choosing
                // player. A trigger made on another player's behalf must filter
                // its legal set by the ability's controller, not by the source
                // controller, or the permanent can fall outside possibleTargets.
                UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                Permanent match = findBattlefieldTarget(target, abilityControllerId, source, game, getTargets().get(0));
                if (match != null) {
                    target.addTarget(match.getId(), source, game);
                    getTargets().remove(0);
                    return true;
                }
            }
            return super.chooseTarget(outcome, target, source, game);
        }

        /** The battlefield permanent in the target's own legal set that the
         * queued name or alias names, or null when the answer is not a
         * permanent (a spell on the stack, which the base handles). */
        private Permanent findBattlefieldTarget(mage.target.Target target, UUID abilityControllerId, Ability source, Game game, String name) {
            for (UUID id : target.possibleTargets(abilityControllerId, source, game)) {
                Permanent p = game.getPermanent(id);
                if (p == null || target.contains(id)) {
                    continue;
                }
                if (hasObjectTargetNameOrAlias(p, name)) {
                    return p;
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

    // Match encoding/json's *int field: absent/null defaults to 1; otherwise
    // require an integer JSON number. Gson's getAsInt truncates fractions and
    // wraps oversized numbers, potentially replaying a different turn.
    private static int scenarioTurn(JsonObject sc) {
        JsonElement value = sc.get("turn");
        if (value == null || value.isJsonNull()) {
            return 1;
        }
        try {
            if (value.isJsonPrimitive() && value.getAsJsonPrimitive().isNumber()
                    && value.getAsString().matches("-?[0-9]+")) {
                int turn = Integer.parseInt(value.getAsString());
                if (turn >= 1 && turn <= 100) {
                    return turn;
                }
            }
        } catch (NumberFormatException ignored) {
            // Overflow is invalid, not an invitation to wrap to another turn.
        }
        throw new IllegalArgumentException("invalid scenario turn " + value + " (want integer 1..100)");
    }

    JsonObject replayOnce(JsonObject sc, boolean strict) {
        JsonObject res = new JsonObject();
        res.addProperty("strict", strict);
        res.addProperty("name", str(sc, "name"));
        if (sc.has("id")) {
            res.add("id", sc.get("id"));
        }
        snaps.clear();
        boolean endTurnScenario = false;
        // Preserve the exact actions each step queues (including mana and
        // combat sub-actions), not just its final checkpoint.
        List<List<PlayerAction>> queuedA = new ArrayList<>();
        List<List<PlayerAction>> queuedB = new ArrayList<>();
        List<List<String>> choicesA = new ArrayList<>();
        List<List<String>> choicesB = new ArrayList<>();
        List<List<String>> targetsA = new ArrayList<>();
        List<List<String>> targetsB = new ArrayList<>();
        try {
            reset();
            skipInitShuffling();
            setStrictChooseMode(strict);
            sc0 = sc;
            gorgeName = str(sc, "card");
            xmageName = str(sc, "xmage_name");
            endTurnScenario = hasEndTurnEffect(xmageName.isEmpty() ? gorgeName : xmageName);
            cast.clear();
            adjustedCasts.clear();
            refAlias.clear();
            phase = MAIN;
            TURN = scenarioTurn(sc);
            turn = TURN;
            activeSeat = (TURN - 1) % 2;
            build(sc);
            runCode("setup", TURN, MAIN, playerA, (info, p, g) -> {
                addSetupCounters(g);
                registerAliases(g);
                snaps.add(snapshot(info, g));
            });
            JsonArray steps = sc.has("steps") ? sc.getAsJsonArray("steps") : new JsonArray();
            JsonArray xans = sc.has("xmage_answers") && sc.get("xmage_answers").isJsonArray()
                    ? sc.getAsJsonArray("xmage_answers") : new JsonArray();
            splitScripted = xans.toString().contains("^X=");
            xabilities = sc.has("xmage_ability") && sc.get("xmage_ability").isJsonArray()
                    ? sc.getAsJsonArray("xmage_ability") : new JsonArray();
            xtargetSkips = readTargetSkips(sc, steps);
            for (int i = 0; i < steps.size(); i++) {
                JsonObject st = steps.get(i).getAsJsonObject();
                String op = str(st, "op");
                int beforeChoicesA = playerA.getChoices().size();
                int beforeChoicesB = playerB.getChoices().size();
                int beforeTargetsA = playerA.getTargets().size();
                int beforeTargetsB = playerB.getTargets().size();
                if (i < xans.size() && xans.get(i).isJsonArray()) {
                    // Queue answers by their recorded seat before registering
                    // actions. XMage consumes these queues while executing the
                    // cast; the action registration itself is deferred.
                    scripted(xans.get(i).getAsJsonArray());
                }
                int beforeA = playerA.getActions().size();
                int beforeB = playerB.getActions().size();
                step(st, op, i);
                String cp = "step " + i + " (" + op + ")";
                runCode(cp, turn, phase, playerA, (info, p, g) -> snaps.add(snapshot(info, g)));
                queuedA.add(new ArrayList<>(playerA.getActions().subList(beforeA, playerA.getActions().size())));
                queuedB.add(new ArrayList<>(playerB.getActions().subList(beforeB, playerB.getActions().size())));
                choicesA.add(new ArrayList<>(playerA.getChoices().subList(beforeChoicesA, playerA.getChoices().size())));
                choicesB.add(new ArrayList<>(playerB.getChoices().subList(beforeChoicesB, playerB.getChoices().size())));
                targetsA.add(new ArrayList<>(playerA.getTargets().subList(beforeTargetsA, playerA.getTargets().size())));
                targetsB.add(new ArrayList<>(playerB.getTargets().subList(beforeTargetsB, playerB.getTargets().size())));
            }
            // Only EndTurn scenarios need the extended boundary. Ordinary
            // replays retain the original turn-1 stop and cannot encounter
            // unrelated turn-2 upkeep triggers or decisions.
            setStopAt(endTurnScenario ? turn + 1 : turn,
                    endTurnScenario ? PhaseStep.UPKEEP : PhaseStep.END_TURN);
            execute();
        } catch (Throwable t) {
            String msg = t.getClass().getSimpleName() + ": " + t.getMessage();
            int stepCount = sc.has("steps") ? sc.getAsJsonArray("steps").size() : 0;
            int completedSteps = snaps.size() - 1;
            if (endTurnScenario && unusedActionCount(msg) >= 0
                    && completedSteps >= 0 && completedSteps < stepCount
                    && currentGame != null && currentGame.getTurnNum() > turn
                    && skippedActionsMatch(completedSteps, queuedA, queuedB)
                    && skippedAnswersMatch(completedSteps, choicesA, choicesB, targetsA, targetsB)) {
                // Only the actions for skipped steps remain. An unconsumed
                // action from an earlier step is still a harness error. Record
                // every skipped label from the post-turn state for alignment.
                for (int skipped = completedSteps; skipped < stepCount; skipped++) {
                    String op = str(sc.getAsJsonArray("steps").get(skipped).getAsJsonObject(), "op");
                    snaps.add(snapshot("step " + skipped + " (" + op + ")", currentGame));
                }
                msg = null;
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

    private boolean skippedActionsMatch(int first, List<List<PlayerAction>> queuedA,
                                        List<List<PlayerAction>> queuedB) {
        if (queuedA.size() != queuedB.size() || first >= queuedA.size()) {
            return false;
        }
        List<PlayerAction> remainingA = new ArrayList<>();
        List<PlayerAction> remainingB = new ArrayList<>();
        for (int i = first; i < queuedA.size(); i++) {
            remainingA.addAll(queuedA.get(i));
            remainingB.addAll(queuedB.get(i));
        }
        // XMage may copy TestPlayer, but its copy retains the same PlayerAction
        // objects. Match the entire queue on both seats, not just its length.
        return ((TestPlayer) currentGame.getPlayer(playerA.getId())).getActions().equals(remainingA)
                && ((TestPlayer) currentGame.getPlayer(playerB.getId())).getActions().equals(remainingB);
    }

    private boolean skippedAnswersMatch(int first, List<List<String>> choicesA, List<List<String>> choicesB,
                                        List<List<String>> targetsA, List<List<String>> targetsB) {
        TestPlayer actualA = (TestPlayer) currentGame.getPlayer(playerA.getId());
        TestPlayer actualB = (TestPlayer) currentGame.getPlayer(playerB.getId());
        // assertAllCommandsUsed checks actions first. Do not hide its failure
        // if a completed step also left an unused choice or target behind.
        return remainingIsSkipped(actualA.getChoices(), choicesA, first)
                && remainingIsSkipped(actualB.getChoices(), choicesB, first)
                && remainingIsSkipped(actualA.getTargets(), targetsA, first)
                && remainingIsSkipped(actualB.getTargets(), targetsB, first);
    }

    private static <T> boolean remainingIsSkipped(List<T> actual, List<List<T>> queued, int first) {
        if (first < 0 || first >= queued.size()) {
            return false;
        }
        List<T> expected = new ArrayList<>();
        for (int i = first; i < queued.size(); i++) {
            expected.addAll(queued.get(i));
        }
        return actual.equals(expected);
    }

    private static int unusedActionCount(String message) {
        String marker = "must have 0 actions but found ";
        if (message == null || !message.contains(marker)) {
            return -1;
        }
        try {
            return Integer.parseInt(message.substring(message.indexOf(marker) + marker.length()).trim());
        } catch (NumberFormatException ignored) {
            return -1;
        }
    }

    private static boolean hasEndTurnEffect(String name) {
        if (name == null || name.isEmpty()) {
            return false;
        }
        CardInfo info = CardRepository.instance.findCard(name);
        if (info == null) {
            return false;
        }
        Card card = info.createCard();
        return card.getSpellAbility() != null && abilityHasEndTurnEffect(card.getSpellAbility());
    }

    private static boolean abilityHasEndTurnEffect(Ability ability) {
        if (ability.getEffects().stream().anyMatch(EndTurnEffect.class::isInstance)) {
            return true;
        }
        for (Ability sub : ability.getSubAbilities()) {
            if (abilityHasEndTurnEffect(sub)) {
                return true;
            }
        }
        return false;
    }

    // ---- setup -----------------------------------------------------------

    private TestPlayer seat(int i) {
        return i == 0 ? playerA : playerB;
    }

    private void build(JsonObject sc) {
        buildCounts.clear();
        setupNames.clear();
        backFaceNames.clear();
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
            // Same draw reserve as oracleRun.build: preserve turn 1 exactly,
            // otherwise allow TURN/2 draws plus one card at the checkpoint,
            // even when all named setup cards are outside the library.
            if (TURN > 1) {
                filler = Math.max(filler, TURN / 2 + 1);
            }
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
        List<String> backFace = zone == Zone.BATTLEFIELD ? names(s, "back_face") : new ArrayList<>();
        int i = p == playerA ? 0 : 1;
        for (String n : ns) {
            int k = buildCounts.merge(i + "|" + n, 1, Integer::sum);
            String ref = "p" + i + ":" + n + (k > 1 ? "#" + k : "");
            refAlias.put(ref, "@" + ref);
            addCard(zone, p, xmageSpelling(n), 1, tapped.contains(n));
            if (backFace.contains(n)) {
                stageBackFace(p, n);
            }
        }
        return ns.size();
    }

    /** Setup is not a transform action: place the right half without firing
     * TRANSFORMED triggers. XMage's transforming and modal DFCs both extend
     * DoubleFacedCard; its test framework accepts either half directly. */
    private void stageBackFace(TestPlayer p, String name) {
        List<PutToBattlefieldInfo> placements = getBattlefieldCards(p);
        int last = placements.size() - 1;
        PutToBattlefieldInfo placed = placements.get(last);
        Card card = placed.getMainCard();
        if (!(card instanceof DoubleFacedCard)) {
            throw new IllegalArgumentException("setup back_face: " + name + " is not double-faced");
        }
        Card back = ((DoubleFacedCard) card).getRightHalfCard();
        if (!back.isPermanent()) {
            throw new IllegalArgumentException("setup back_face: " + name + " has no permanent back face");
        }
        placements.set(last, new PutToBattlefieldInfo(back, placed.isTapped()));
        // Scenario refs/counters still name the front, even though snapshots
        // and XMage's attack/block commands must name the current back face.
        setupNames.put(back.getId(), xmageSpelling(name));
        backFaceNames.put("p" + (p == playerA ? 0 : 1) + ":" + xmageSpelling(name), back.getName());
    }

    /** Puts each seat's setup "counters" (card name -> gorge counter kind ->
     * n) on every battlefield permanent of that name the seat controls,
     * added to what it entered with, exactly as gorge's runner does at setup
     * (a planeswalker's loyalty headroom, a +1/+1-counter target fixture). */
    private void addSetupCounters(Game g) {
        JsonObject setup = sc0.has("setup") ? sc0.getAsJsonObject("setup") : new JsonObject();
        for (int i = 0; i < 2; i++) {
            JsonObject s = setup.has("p" + i) ? setup.getAsJsonObject("p" + i) : new JsonObject();
            if (!s.has("counters")) {
                continue;
            }
            TestPlayer pl = seat(i);
            for (Map.Entry<String, JsonElement> card : s.getAsJsonObject("counters").entrySet()) {
                String name = xmageSpelling(card.getKey());
                boolean placed = false;
                for (Permanent perm : g.getBattlefield().getAllPermanents()) {
                    if (!pl.getId().equals(perm.getControllerId()) || !setupNames.getOrDefault(perm.getId(), perm.getName()).equals(name)) {
                        continue;
                    }
                    placed = true;
                    for (Map.Entry<String, JsonElement> c : card.getValue().getAsJsonObject().entrySet()) {
                        perm.addCounters(xmageCounter(c.getKey()).createInstance(c.getValue().getAsInt()), pl.getId(), null, g);
                    }
                }
                if (!placed) {
                    throw new IllegalArgumentException("setup counters: p" + i + " controls no " + name);
                }
            }
        }
    }

    /** Maps a gorge counter kind (P1P1, M1M1, LOYALTY) to XMage's type. */
    private static CounterType xmageCounter(String kind) {
        String n;
        switch (kind) {
            case "P1P1":
                n = "+1/+1";
                break;
            case "M1M1":
                n = "-1/-1";
                break;
            default:
                n = kind.toLowerCase();
        }
        CounterType t = CounterType.findByName(n);
        if (t == null) {
            throw new IllegalArgumentException("setup counters: unknown counter kind " + kind);
        }
        return t;
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
                String xmageCardName = setupNames.getOrDefault(o.getId(), o.getName());
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

    /** Whether the ask belongs to the spell ability of a card the scenario cast
     * through a target adjuster. */
    private boolean isAdjustedSpellAsk(Ability source, Game game) {
        if (source == null || source.getAbilityType() != mage.constants.AbilityType.SPELL) {
            return false;
        }
        mage.MageObject object = source.getSourceObject(game);
        return object != null && adjustedCasts.contains(object.getName());
    }

    /** An ask for an adjusted spell's target with no scenario answer left skips
     * the slot (an "up to" slot with candidates remaining). */
    static void closeAskedAgain(List<String> queue, boolean adjustedSpellAsk) {
        if (adjustedSpellAsk && queue.isEmpty()) {
            queue.add(TestPlayer.TARGET_SKIP);
        }
    }

    /** An adjusted spell must receive targets through the cast-time chooser. */
    static boolean queueAdjustedCastTargets(boolean hasAdjuster, boolean divided, int targetCount) {
        return hasAdjuster && !divided && targetCount > 0;
    }

    /** Whether an ability gets its targets from a target adjuster. */
    static boolean hasTargetAdjuster(Ability ability) {
        return ability != null && ability.getTargetAdjuster() != null;
    }

    /**
     * Whether a spell ability is *targetless until its adjuster runs*: it has a
     * target adjuster AND no base target. Only this shape breaks the inline
     * {@code $target=} form -- the up-front check reads the unadjusted ability
     * ({@code handleNonPlayerTargetTarget}'s empty {@code selectedMode.getTargets()})
     * and throws "Ability has no targets" (The Eagles Are Coming!: no base
     * target, {@code ConditionalTargetAdjuster} supplies it during casting).
     * An adjuster card that already declares a base target (Dominate,
     * Distorting Wake) validates inline and keeps its old path, so this change
     * cannot reroute it.
     */
    static boolean needsQueuedCastTargets(Ability ability) {
        return hasTargetAdjuster(ability) && ability.getAllSelectedTargets().isEmpty();
    }

    /** Whether the card's spell ability is targetless until its adjuster runs. */
    private static boolean spellNeedsQueuedCastTargets(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        return c != null && needsQueuedCastTargets(c.getSpellAbility());
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
        return !targetSlotNeedsSkip(c.getSpellAbility().getAllSelectedTargets(), n);
    }

    /** An unfilled or open target slot needs an explicit skip to finish casting. */
    static boolean targetSlotNeedsSkip(List<mage.target.Target> targets, int supplied) {
        return targets.size() != 1 || supplied < targets.get(0).getMaxNumberOfTargets();
    }

    /** Normalises a cost label so gorge's pick ("Sacrifice artifact or
     * creature", "Pay 4") and XMage's button text ("Sacrifice an artifact or
     * creature", "{4}") compare equal. */
    private static String costKey(String label) {
        return label.toLowerCase().replaceAll("[{}]", "").replaceFirst("^pay ", "")
                .replaceAll("\\b(a|an|the)\\b", "").replaceAll("\\s+", " ").trim();
    }

    /** Which side of XMage's OrCost ask (true text = first cost) the cast
     * step's recorded gorge pick names; null when none of them matches. */
    private Boolean recordedCostIsFirst(String trueText, String falseText) {
        for (String pick : castCostPicks) {
            String key = costKey(pick);
            if (falseText != null && key.equals(costKey(falseText))) {
                return false;
            }
            if (trueText != null && key.equals(costKey(trueText))) {
                return true;
            }
        }
        return null;
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

    /** Resolve a named setup permanent ref, preserving duplicate suffixes. */
    private Permanent permanentRef(Game g, String ref) {
        int seatIndex = Integer.parseInt(ref.substring(1, ref.indexOf(':')));
        String name = xmageSpelling(refName(ref));
        int wanted = 1;
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            wanted = Integer.parseInt(ref.substring(hash + 1));
        }
        int seen = 0;
        for (Permanent perm : g.getBattlefield().getAllPermanents()) {
            if (perm.getControllerId().equals(seat(seatIndex).getId())
                    && setupNames.getOrDefault(perm.getId(), perm.getName()).equals(name)) {
                if (++seen == wanted) {
                    return perm;
                }
            }
        }
        throw new IllegalArgumentException("permanent ref " + ref + " is not on the battlefield");
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

    /** Cards cast through a target adjuster: the cast-time chooser may ask for
     * another target after the scenario's answers run out (see
     * ScriptedChoicePlayer.chooseTarget). */
    private final Set<String> adjustedCasts = new java.util.HashSet<>();
    // Setup objects the scenario can name by ref ("p0:Grizzly Bears#2"): the
    // XMage alias each ref is registered under, so two same-name permanents
    // are told apart. Filled during setup, read by step targeting.
    private final java.util.Map<String, String> refAlias = new java.util.LinkedHashMap<>();
    // Per-seat per-name occurrence counter build() uses to spell setup refs
    // ("p0:Grizzly Bears", "p0:Grizzly Bears#2") in the same order XMage
    // adds the cards, so registerAliases can bind each to its object.
    private final java.util.Map<String, Integer> buildCounts = new java.util.HashMap<>();
    private final Map<UUID, String> setupNames = new HashMap<>();
    private final Map<String, String> backFaceNames = new HashMap<>();
    // The card under test's two spellings: the scenario's (gorge/corpus) name
    // and XMage's card-database name when they differ (Forge prints "Dáin
    // Ironfoot", XMage stores "Dain Ironfoot"). xmageName is empty when equal.
    private String gorgeName = "";
    private String xmageName = "";

    /** The spelling to give XMage for a scenario card: the card under test's
     * XMage spelling, else the name unchanged. This is the spelling of the
     * card OBJECT (what addCard stores, what a target alias names), which for
     * a split/Room card is the whole "A // B" card. */
    private String xmageSpelling(String n) {
        return (!xmageName.isEmpty() && n.equals(gorgeName)) ? xmageName : n;
    }

    /** The spelling XMage's cast command matches: the name of the card's
     * SpellAbility, which differs from the card object's name for a split or
     * Room card. XMage names a half's ability "Cast <half>" (SplitCard splits
     * the set info name on " // "), while the physical card added to hand is
     * the whole "A // B"; casting the whole name finds no ability ("Can't
     * find ability to activate command: Cast Walk-In Closet"). So a cast
     * step names the scenario's face (gorgeName) and setup still deals the
     * whole card through xmageSpelling. */
    private String castSpelling(String n) {
        if (!xmageName.isEmpty() && n.equals(gorgeName) && xmageName.contains(" // ")) {
            return gorgeName;
        }
        return xmageSpelling(n);
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

    // The item's per-step XMage ability text (parallel to the steps; empty
    // except on activate steps). H1, confirmed against the XMage source:
    // TestPlayer.hasAbilityTargetNameOrAlias (Mage.Tests .../player/
    // TestPlayer.java) selects an activated ability with
    // `ability.toString().startsWith(nameOrAlias)`, and AbilityImpl.toString()
    // is getRule(), so the item's value is the printed rule text's prefix
    // ("{T}", "Equip {2}"). The activate: / manaActivate: handlers route it
    // through that match before activating.
    private JsonArray xabilities = new JsonArray();

    /** Step i's XMage ability text, or "" when the item carries none. */
    private String xabilityAt(int i) {
        if (i < xabilities.size() && xabilities.get(i).isJsonPrimitive()) {
            return xabilities.get(i).getAsString();
        }
        return "";
    }

    // The item's xmage_target_skips: parallel to the steps, entry i lists the
    // empty optional target objects of step i's cast as {"at": n, "slot": k}.
    // "at" is the number of filled targets that precede the object, so the
    // skip is queued before target n (consecutive empty objects repeat n, a
    // trailing one has n == the target count). "slot" identifies the XMage
    // object too: both engines must have the same independent 0..1 shape.
    private JsonArray xtargetSkips = new JsonArray();

    /** The queue offsets of step i's explicit target skips, validated against
     * the cast's target count; empty when the item carries none. A malformed
     * plan fails the replay rather than queueing a speculative skip. */
    static JsonArray readTargetSkips(JsonObject sc, JsonArray steps) {
        if (!sc.has("xmage_target_skips")) {
            return new JsonArray();
        }
        JsonElement raw = sc.get("xmage_target_skips");
        if (!raw.isJsonArray() || raw.getAsJsonArray().size() != steps.size()) {
            throw new IllegalArgumentException("xmage_target_skips must be parallel to steps");
        }
        JsonArray plan = raw.getAsJsonArray();
        for (int i = 0; i < plan.size(); i++) {
            JsonElement entry = plan.get(i);
            if (!entry.isJsonNull() && (!entry.isJsonArray()
                    || (entry.getAsJsonArray().size() > 0 && !str(steps.get(i).getAsJsonObject(), "op").equals("cast")))) {
                throw new IllegalArgumentException("step " + i + " has invalid xmage_target_skips");
            }
        }
        return plan;
    }

    private List<Integer> castTargetSkipsAt(int i, String card, int targetCount) {
        if (i >= xtargetSkips.size() || xtargetSkips.get(i).isJsonNull()
                || xtargetSkips.get(i).getAsJsonArray().size() == 0) {
            return new ArrayList<>();
        }
        CardInfo info = CardRepository.instance.findCard(card);
        Card c = info == null ? null : info.createCard();
        if (c == null || hasTargetAdjuster(c.getSpellAbility())) {
            throw new IllegalArgumentException("cannot establish explicit target objects for " + card);
        }
        return validateTargetSkips(xtargetSkips.get(i).getAsJsonArray(),
                c.getSpellAbility().getAllSelectedTargets(), targetCount);
    }

    /** Check correspondence against XMage itself before closing an object. */
    static List<Integer> validateTargetSkips(JsonArray plan, List<mage.target.Target> targets, int targetCount) {
        if (targetCount < 0 || targets.size() != targetCount + plan.size()) {
            throw new IllegalArgumentException("explicit target plan does not account for every object");
        }
        for (mage.target.Target t : targets) {
            if (t.getMinNumberOfTargets() != 0 || t.getMaxNumberOfTargets() != 1 || t instanceof mage.target.TargetAmount) {
                throw new IllegalArgumentException("explicit target plan requires independent optional 0..1 objects");
            }
        }
        List<Integer> out = new ArrayList<>();
        int prev = 0;
        for (JsonElement e : plan) {
            if (!e.isJsonObject()) {
                throw new IllegalArgumentException("invalid target skip: " + e);
            }
            int at = targetSkipIndex(e.getAsJsonObject(), "at");
            int slot = targetSkipIndex(e.getAsJsonObject(), "slot");
            if (at < prev || at > targetCount || slot != at + out.size()) {
                throw new IllegalArgumentException("target skip is out of order or beyond its objects: " + e);
            }
            prev = at;
            out.add(at);
        }
        return out;
    }

    private static int targetSkipIndex(JsonObject skip, String key) {
        JsonElement value = skip.get(key);
        if (value == null || !value.isJsonPrimitive() || !value.getAsJsonPrimitive().isNumber()) {
            throw new IllegalArgumentException("target skip requires integer " + key);
        }
        try {
            return value.getAsBigDecimal().intValueExact();
        } catch (ArithmeticException ex) {
            throw new IllegalArgumentException("target skip requires integer " + key, ex);
        }
    }

    /** Queue the cast's targets with one "[target_skip]" before each offset. */
    private void queueCastTargetsWithSkips(TestPlayer p, List<String> tg, List<Integer> skips) {
        int next = 0;
        for (int k = 0; k <= tg.size(); k++) {
            while (next < skips.size() && skips.get(next) == k) {
                addTarget(p, TestPlayer.TARGET_SKIP);
                next++;
            }
            if (k < tg.size()) {
                queueCastTarget(p, tg.get(k));
            }
        }
    }

    /** Whether the named card has an activated mana ability whose rule text
     * starts with text; XMage activates those through activateManaAbility. */
    private static boolean isManaAbilityText(String name, String text) {
        CardInfo info = CardRepository.instance.findCard(name, true);
        Card c = info == null ? null : info.createCard();
        if (c == null) {
            return false;
        }
        if (c instanceof DoubleFacedCard && ((DoubleFacedCard) c).getRightHalfCard().getName().equals(name)) {
            c = ((DoubleFacedCard) c).getRightHalfCard();
        }
        for (Ability a : c.getAbilities()) {
            if (a.toString().startsWith(text)) {
                return a instanceof ActivatedManaAbilityImpl;
            }
        }
        return false;
    }

    private void step(JsonObject st, String op, int stepIdx) {
        int seatIdx = st.has("seat") ? st.get("seat").getAsInt() : 0;
        TestPlayer p = seat(seatIdx);
        switch (op) {
            case "mana": {
                String mana = str(st, "mana");
                runCode("mana " + mana, turn, phase, p, (info, pl, g) -> addPool(pl, g, mana));
                return;
            }
            case "attack": {
                // gorge's attack op declares the listed creatures attacking
                // the defender. XMage's attack() queues a selectAttackers
                // command at DECLARE_ATTACKERS; the scenario's later cast and
                // checkpoint happen in that same step.
                for (String a : names(st, "attackers")) {
                    attack(turn, p, combatName(a), seat(seatOf(str(st, "defender"))));
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
                if (st.has("active")) {
                    String active = str(st, "active");
                    int nextActiveSeat = active.equals("p1") ? 1 : active.equals("p0") ? 0 : -1;
                    if (nextActiveSeat >= 0) {
                        // pass_to asks for the next turn on this seat, not merely
                        // the next distinct seat: p0 after p0 skips p1's turn too.
                        turn += nextActiveSeat == activeSeat ? 2 : 1;
                        activeSeat = nextActiveSeat;
                    }
                }
                if (decision.equals("blockers") || stepName.equals("declare-blockers")) {
                    phase = PhaseStep.DECLARE_BLOCKERS;
                } else if (stepName.equals("declare-attackers")) {
                    phase = PhaseStep.DECLARE_ATTACKERS;
                } else if (stepName.equals("begin-combat")) {
                    phase = PhaseStep.BEGIN_COMBAT;
                } else if (stepName.equals("end-combat")) {
                    phase = PhaseStep.END_COMBAT;
                } else if (stepName.equals("main2")) {
                    phase = PhaseStep.POSTCOMBAT_MAIN;
                } else if (stepName.equals("upkeep")) {
                    phase = PhaseStep.UPKEEP;
                } else if (stepName.equals("draw")) {
                    phase = PhaseStep.DRAW;
                } else if (stepName.equals("end")) {
                    phase = PhaseStep.END_TURN;
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
                    block(turn, p, combatName(pair.get(0).getAsString()), combatName(pair.get(1).getAsString()));
                }
                phase = PhaseStep.DECLARE_BLOCKERS;
                return;
            }
            case "attach": {
                String card = str(st, "card");
                String bearer = str(st, "attached_to");
                // Queued on the active player: a runCode for the non-active seat would
                // run only when it next gets priority, after this step's snapshot.
                runCode("attach " + card + " to " + bearer, turn, phase, playerA, (info, pl, g) -> {
                    Permanent attachment = permanentRef(g, card);
                    Permanent target = permanentRef(g, bearer);
                    // Card.addAttachment links both sides (the bearer's
                    // attachment list and the attachment's attachedTo) and
                    // applies XMage's own legality checks.
                    if (!target.addAttachment(attachment.getId(), null, g)) {
                        throw new IllegalStateException("attach " + card + " to " + bearer + " refused");
                    }
                });
                return;
            }
            case "cast": {
                if (st.has("mana")) {
                    String mana = str(st, "mana");
                    runCode("mana " + mana, turn, phase, p, (info, pl, g) -> addPool(pl, g, mana));
                }
                if (st.has("kicked") || st.has("cast_mode")) {
                    throw new IllegalArgumentException("kicked/cast_mode unsupported");
                }
                if (!sc0.has("xmage_answers")) {
                    answers(st, p);
                }
                // Spree permits choosing further modes after the first. Its
                // generated answer records the chosen mode, not the decision
                // to stop, so close XMage's repeated mode prompt explicitly.
                if (SPREE_CARDS.contains(xmageName.isEmpty() ? refName(str(st, "card")) : xmageName)) {
                    setModeChoice(p, TestPlayer.MODE_SKIP);
                }
                castCostPicks = new ArrayList<>();
                if (st.has("answers")) {
                    for (JsonElement e : st.getAsJsonArray("answers")) {
                        JsonObject a = e.getAsJsonObject();
                        if (str(a, "kind").equals("choose")) {
                            castCostPicks.addAll(names(a, "pick"));
                        }
                    }
                }
                // The cast command names the SpellAbility, not the card
                // object: a split/Room half is cast by its half name while
                // the hand holds the whole "A // B" card (castSpelling).
                String card = castSpelling(refName(str(st, "card")));
                List<String> tg = targets(st);
                if (hasAlternativeSourceCost(card)) {
                    // A plain cast step: gorge paid the mana cost, so decline
                    // the alternative cost (evoke, impending, dash, ...)
                    // XMage offers through its "Cast with no alternative
                    // cost" choice, rather than leave it to the AI.
                    setChoice(p, "Cast with no alternative cost");
                }
                List<Integer> skips = castTargetSkipsAt(stepIdx, card, tg.size());
                if (splitScripted && spellTargetsDivided(card) && skips.isEmpty()) {
                    // The scripted "<ref>^X=<share>" answers name the targets
                    // and gorge's split; a target string here would be a
                    // second, unconsumed set.
                    castSpell(turn, phase, p, card);
                    cast.add(card);
                    return;
                }
                if (!skips.isEmpty()) {
                    // The generator named every empty optional target object
                    // and where it falls between the filled ones, so the queue
                    // is exactly the plan: no blind trailing skip.
                    if (spellTargetsDivided(card) || queueAdjustedCastTargets(spellNeedsQueuedCastTargets(card), false, tg.size())) {
                        throw new IllegalArgumentException("cast step " + stepIdx + " carries xmage_target_skips for " + card
                                + ", a divided or adjusted spell the explicit skip plan does not cover");
                    }
                    queueCastTargetsWithSkips(p, tg, skips);
                    castSpell(turn, phase, p, card);
                } else if (!tg.isEmpty() && queueAdjustedCastTargets(spellNeedsQueuedCastTargets(card), spellTargetsDivided(card), tg.size())) {
                    // An adjuster may add the SpellAbility's target slots only
                    // after cast setup. Inline $target is validated too early
                    // (against the unadjusted, targetless ability), so queue
                    // scenario targets for the chooser that runs during casting.
                    for (String t : tg) {
                        queueCastTarget(p, t);
                    }
                    adjustedCasts.add(card);
                    castSpell(turn, phase, p, card);
                } else if (tg.size() == 1 && isSeatRef(tg.get(0)) && !hasGift(card)) {
                    castSpell(turn, phase, p, card, seat(seatOf(tg.get(0))));
                } else if (tg.size() == 1 && isSeatRef(tg.get(0))) {
                    // A Gift spell (Mind Spiral, Sazacap's Brew): the castSpell
                    // player form binds the wrong ask, so queue the spell's
                    // own player target and close the rest.
                    addTarget(p, seat(seatOf(tg.get(0))));
                    addTarget(p, TestPlayer.TARGET_SKIP);
                    castSpell(turn, phase, p, card);
                } else if (tg.isEmpty()) {
                    castSpell(turn, phase, p, card);
                    cast.add(card);
                    return;
                } else if (tg.size() == 1 && cast.contains(castSpelling(refName(tg.get(0))))) {
                    // Targeting a spell cast by an earlier step: wait for it
                    // on the stack. cast holds cast-command names and the
                    // target is a scenario ref; the setup alias names the
                    // card in hand, not the spell, so target by name.
                    String spell = castSpelling(refName(tg.get(0)));
                    castSpell(turn, phase, p, card, spell, spell);
                } else if (tg.size() == 1) {
                    // A single target goes through XMage's own string form, so
                    // a divided-damage target (TargetAmount) still lets XMage
                    // pick the split as it always did.
                    castSpell(turn, phase, p, card, targetName(tg.get(0)));
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
                        castSpell(turn, phase, p, card, String.join("^", names));
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
                    castSpell(turn, phase, p, card);
                }
                cast.add(card);
                return;
            }
            case "activate": {
                String text = xabilityAt(stepIdx);
                if (text.isEmpty()) {
                    throw new IllegalArgumentException("activate step " + stepIdx + " has no xmage_ability");
                }
                if (st.has("mana")) {
                    String mana = str(st, "mana");
                    runCode("mana " + mana, turn, phase, p, (info, pl, g) -> addPool(pl, g, mana));
                }
                if (!sc0.has("xmage_answers")) {
                    answers(st, p);
                }
                String card = battlefieldName(str(st, "card"));
                if (isManaAbilityText(card, text)) {
                    activateManaAbility(turn, phase, p, text);
                    return;
                }
                for (String t : targets(st)) {
                    queueCastTarget(p, t);
                }
                activateAbility(turn, phase, p, text);
                return;
            }
            case "play":
                playLand(turn, phase, p, xmageSpelling(refName(str(st, "card"))));
                return;
            case "resolve":
                // gorge's resolve op passes priority until the stack is empty.
                waitStackResolved(turn, phase, p);
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
                    // Some GenericChoice effects are recorded as a gorge
                    // mode decision but XMage asks the player through
                    // chooseUse. Route their boolean answer to the player's
                    // CHOICE queue, not the numeric mode queue.
                    if (v.equalsIgnoreCase("yes") || v.equalsIgnoreCase("no")) {
                        setChoice(p, v.equalsIgnoreCase("yes"));
                    } else {
                        setModeChoice(p, v);
                    }
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
        } else if (cast.contains(castSpelling(refName(t)))) {
            // A spell an earlier step cast: its setup alias names the card
            // in hand, not the spell. The target string is the cast-command
            // name (a split/Room half), matching what castSpell queued.
            addTarget(p, castSpelling(refName(t)));
        } else {
            addTarget(p, targetName(t));
        }
    }

    /** The name form XMage's attack/block command takes. Unlike a cast
     * target, the command does not accept the driver's "@" aliases, so it
     * must be the card name, with the legacy zero-based "<name>:<index>"
     * suffix for a duplicate ("p0:Grizzly Bears#2" -> "Grizzly Bears:1"). */
    private String combatName(String ref) {
        String n = battlefieldName(ref);
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            int k = Integer.parseInt(ref.substring(hash + 1)) - 1;
            if (k > 0) {
                return n + ":" + k;
            }
        }
        return n;
    }

    /** Front-name setup refs keep their identity; name-based battlefield
     * commands instead need the face setup actually placed on that seat. */
    private String battlefieldName(String ref) {
        String n = xmageSpelling(refName(ref));
        int colon = ref.indexOf(':');
        if (colon >= 0 && ref.substring(0, colon).matches("p[0-9]+")) {
            return backFaceNames.getOrDefault(ref.substring(0, colon + 1) + n, n);
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

    /** The permanent's keyword names, sorted and de-duplicated. Spec H5
     * (confirmed against the XMage source): a keyword ability's getRule() is
     * its lower-case name ("flying", "first strike"), which the comparator
     * case-folds onto its evergreen set (compliance/oraclediff/keywords.go).
     * Some rules carry HTML-wrapped reminder text ("menace <i>(This creature
     * can't be blocked ...)</i>", MenaceAbility.showAbilityHint), so tags are
     * stripped and the rule is cut at the first "(". getAbilities(g) includes
     * abilities gained from continuous effects, so a granted keyword shows
     * here. Only classes of mage.abilities.keyword count, so
     * triggered/activated/static rule text never reaches the list. */
    private static List<String> keywordNames(Permanent perm, Game g) {
        java.util.TreeSet<String> names = new java.util.TreeSet<>();
        for (Ability a : perm.getAbilities(g)) {
            if (!a.getClass().getName().startsWith("mage.abilities.keyword.")) {
                continue;
            }
            String rule = a.getRule().replaceAll("<[^>]*>", "");
            int paren = rule.indexOf("(");
            if (paren >= 0) {
                rule = rule.substring(0, paren);
            }
            rule = rule.trim();
            if (!rule.isEmpty()) {
                names.add(rule);
            }
        }
        return new ArrayList<>(names);
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
            o.add("keywords", GSON.toJsonTree(keywordNames(perm, g)));
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

    // ManaPool.getWhite() and the other colour getters sum only unconditional
    // pool items: a restricted add ("spend only on Dragon spells") is a
    // ConditionalMana whose ManaPoolItem keeps its plain counters at zero. Add
    // those in too, so the snapshot matches gorge's letters-only pool. The
    // restriction itself is not compared.
    static String pool(ManaPool mp) {
        int w = mp.getWhite();
        int u = mp.getBlue();
        int bl = mp.getBlack();
        int r = mp.getRed();
        int g = mp.getGreen();
        int c = mp.getColorless();
        for (ConditionalMana cm : mp.getConditionalMana()) {
            w += cm.getWhite();
            u += cm.getBlue();
            bl += cm.getBlack();
            r += cm.getRed();
            g += cm.getGreen();
            // ManaPool.addMana folds generic into colorless for plain mana.
            c += cm.getColorless() + cm.getGeneric();
        }
        StringBuilder b = new StringBuilder();
        rep(b, 'W', w);
        rep(b, 'U', u);
        rep(b, 'B', bl);
        rep(b, 'R', r);
        rep(b, 'G', g);
        rep(b, 'C', c);
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
