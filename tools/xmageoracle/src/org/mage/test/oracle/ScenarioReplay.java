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
import mage.abilities.Mode;
import mage.abilities.common.AttacksEachCombatStaticAbility;
import mage.abilities.common.SimpleStaticAbility;
import mage.abilities.mana.ActivatedManaAbilityImpl;
import mage.abilities.costs.AlternativeSourceCosts;
import mage.abilities.costs.OptionalAdditionalSourceCosts;
import mage.abilities.costs.OrCost;
import mage.abilities.keyword.LeylineAbility;
import mage.abilities.keyword.SpreeAbility;
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
import mage.util.RandomUtil;
import mage.players.Player;
import mage.filter.FilterCard;
import mage.target.TargetCard;
import mage.util.CardUtil;
import mage.watchers.common.PermanentsEnteredBattlefieldWatcher;
import org.mage.test.player.PlayerAction;
import org.mage.test.player.TestPlayer;
import org.mage.test.serverside.base.CardTestPlayerBase;

import java.io.BufferedReader;
import java.io.FileReader;
import java.io.PrintWriter;
import java.io.FileWriter;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeMap;
import java.util.UUID;
import java.util.function.BiPredicate;

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
    private boolean attackAdvancedTurn = false;
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
        return new ScriptedChoicePlayer(new org.mage.test.player.TestComputerPlayer(name, rangeOfInfluence), this);
    }

    // The gorge-side "choose" picks of the cast step being queued (its
    // recorded answers), so an either-or cost ask XMage poses can be answered
    // the way gorge answered it. Empty when the step recorded none.
    private List<String> castCostPicks = new ArrayList<>();

    // Whether the cast step being replayed elected a mode whose cost is a
    // cast-time optional additional cost (Bargain). XMage asks that cost
    // through OptionalAdditionalSourceCosts; the ask is answered Yes so the
    // mode's reduction is in force, and the sacrifice it adds is paid from
    // this cast's own recorded choose picks.
    private boolean bargainedCast = false;

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
                switch (optionalAdditionalCostAnswer(owner.bargainedCast, getChoices())) {
                    case PAY:
                        // The step elected Bargain: pay the cost. This is the
                        // SAME ask every optional additional cost poses
                        // (kicker, offspring, waterbend); only the mode differs.
                        return true;
                    case CONSUME:
                        return super.chooseUse(outcome, message, secondMessage, trueText, falseText, source, game);
                    default:
                        return false;
                }
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

        // Tokens may be created after scripted() queues an answer. Resolve a
        // token ref against the LIVE choice game, not a prematurely bound or
        // stale alias. Other choice values and the target path stay unchanged.
        private Game choiceGame;

        /** Also: the paid additional cost's sacrifice is posed as a target-group
         * `choose`, which reads XMage's CHOICE queue, not the target queue
         * (TargetSacrifice.pay -> TargetImpl.choose -> Player.choose ->
         * makeChoose over getChoices()). Push this cast's recorded pick so
         * makeChoose consumes it and strict mode records the decision, rather
         * than letting XMage's AI auto-choose a legal object. */
        @Override
        public boolean choose(Outcome outcome, mage.target.Target target, Ability source, Game game) {
            Game previous = choiceGame;
            choiceGame = game;
            try {
                if (isSacrificeChoice(target)) {
                    UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                    List<UUID> candidates = new ArrayList<>(target.possibleTargets(abilityControllerId, source, game));
                    for (String pick : owner.castCostPicks) {
                        if (getChoices().contains(pick)) {
                            continue;
                        }
                        // Queue only a pick that names a legal sacrifice; an
                        // unmatched pick is left out, so makeChoose's strict
                        // "invalid target" failure names it rather than silently
                        // paying the cost with some other object.
                        if (costPickChoice(java.util.Collections.singletonList(pick), candidates,
                                (id, answer) -> hasObjectTargetNameOrAlias(game.getPermanent(id), answer)) != null) {
                            addChoice(pick);
                        }
                    }
                }
                return super.choose(outcome, target, source, game);
            } finally {
                choiceGame = previous;
            }
        }

        @Override
        public boolean choose(Outcome outcome, Cards cards, mage.target.TargetCard target, Ability source, Game game) {
            Game previous = choiceGame;
            choiceGame = game;
            try {
                return super.choose(outcome, cards, target, source, game);
            } finally {
                choiceGame = previous;
            }
        }

        @Override
        public boolean hasObjectTargetNameOrAlias(mage.MageObject object, String value) {
            if (choiceGame != null && value != null && value.startsWith("@")
                    && isScenarioRef(value.substring(1)) && value.contains(":token:")) {
                String ref = value.substring(1);
                Permanent chosen = tokenChoice(ref, owner.seat(refSeat(ref)).getId(),
                        choiceGame.getBattlefield().getAllPermanents());
                return object != null && chosen != null && chosen.getId().equals(object.getId());
            }
            return super.hasObjectTargetNameOrAlias(object, value);
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
            // Cards#getCards is a Set and does not promise library order.
            // Reconstruct the looked-at prefix from Library's ordered view.
            List<Card> lookedAtOrder = new ArrayList<>();
            for (Card card : getLibrary().getCards(game)) {
                if (cards.contains(card.getId())) {
                    lookedAtOrder.add(card);
                }
            }
            Set<Card> available = new java.util.LinkedHashSet<>(lookedAtOrder);
            List<String> queue = librarySelectionQueue(getChoices(), getTargets(),
                    answer -> findByName(available, answer) != null);
            if (queue == null) {
                return null;
            }
            Cards selected = new CardsImpl();
            boolean scripted = false;
            boolean selectionEnded = false;
            while (!queue.isEmpty()) {
                String answer = queue.get(0);
                if (isLibrarySelectionSkip(answer)) {
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

        /**
         * Library selection is exposed by XMage's chooser as a target-shaped
         * generator answer, but surveil/scry are implemented here through the
         * choice queue. Move only a leading target skip into that queue when no
         * card/skip answer already addresses this library selection; ordinary
         * target skips remain on their target queue.
         */
        static List<String> librarySelectionQueue(List<String> choices, List<String> targets,
                java.util.function.Predicate<String> namesLookedAtCard) {
            if (!choices.isEmpty() && (TestPlayer.CHOICE_SKIP.equals(choices.get(0))
                    || namesLookedAtCard.test(choices.get(0)))) {
                return choices;
            }
            if (!targets.isEmpty() && TestPlayer.TARGET_SKIP.equals(targets.get(0))) {
                targets.remove(0);
                choices.add(0, TestPlayer.CHOICE_SKIP);
                return choices;
            }
            return choices.isEmpty() ? null : choices;
        }

        private static boolean isLibrarySelectionSkip(String answer) {
            return TestPlayer.CHOICE_SKIP.equals(answer) || TestPlayer.TARGET_SKIP.equals(answer);
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

        /** XMage represents attach prompts either with a "to attach" hint or,
         * as One Last Job does, an unhinted non-targeting TargetPermanent ask. */
        static boolean isAttachmentChoice(mage.target.Target target) {
            return target.getChooseHint() != null && target.getChooseHint().startsWith("to attach ")
                    || target instanceof mage.target.TargetPermanent && target.isNotTarget()
                    && target.getTargetName().contains("can be attached to");
        }

        /** A cast-time sacrifice ask: the non-targeting TargetSacrifice a
         * paid additional cost poses (Bargain, or an OrCost's sacrifice half).
         * XMage hints it "to sacrifice", which is what the target carries
         * structurally, not a card name. */
        static boolean isSacrificeChoice(mage.target.Target target) {
            return target != null && target.getChooseHint() != null
                    && target.getChooseHint().startsWith("to sacrifice");
        }

        /** The decision a cast-time optional additional cost ask gets,
         * independent of any Game so the contract test can exercise all three
         * outcomes: a bargained cast PAYs it; otherwise the generator's own
         * leading "No" is CONSUMED by the caller so strict mode sees it used;
         * every other ask is DECLINEd (gorge casts for the mana cost only). */
        enum OptionalCostAnswer { PAY, CONSUME, DECLINE }

        static OptionalCostAnswer optionalAdditionalCostAnswer(boolean bargainedCast, List<String> choices) {
            if (bargainedCast) {
                return OptionalCostAnswer.PAY;
            }
            if (!choices.isEmpty() && choices.get(0).equals("No")) {
                return OptionalCostAnswer.CONSUME;
            }
            return OptionalCostAnswer.DECLINE;
        }

        /** The candidate a cost's recorded choose picks name, or null when
         * none matches. Unlike attachmentChoice there is no automatic pick:
         * a sacrifice cost is paid only when the scenario named its object,
         * so a missing pick surfaces as the base's strict unused-command/
         * no-target error rather than a guess. */
        static UUID costPickChoice(List<String> picks, List<UUID> candidates,
                BiPredicate<UUID, String> matches) {
            for (String pick : picks) {
                for (UUID id : candidates) {
                    if (matches.test(id, pick)) {
                        return id;
                    }
                }
            }
            return null;
        }

        /** The candidate an XMage attach ask should take, or null when the ask
         * must fall through to the base player. A scripted answer at the queue
         * front is honored first and consumed, so an attachment ask never
         * silently overrides the scenario or leaves its answer queued for a
         * later decision. Automatic selection happens only for an unscripted
         * ask whose candidate set is uniquely determined. A scripted answer
         * that names no candidate is left in place, so the base's strict
         * unused-command check still reports the scenario error. */
        static UUID attachmentChoice(List<String> queue, List<UUID> candidates,
                BiPredicate<UUID, String> matches) {
            if (!queue.isEmpty() && !TestPlayer.TARGET_SKIP.equals(queue.get(0))) {
                String answer = queue.get(0);
                for (UUID id : candidates) {
                    if (matches.test(id, answer)) {
                        queue.remove(0);
                        return id;
                    }
                }
                return null;
            }
            if (queue.isEmpty() && candidates.size() == 1) {
                return candidates.get(0);
            }
            return null;
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
            if (isAttachmentChoice(target)) {
                UUID abilityControllerId = target.getAffectedAbilityControllerId(this.getId());
                List<UUID> candidates = new ArrayList<>(target.possibleTargets(abilityControllerId, source, game));
                UUID chosen = attachmentChoice(getTargets(), candidates,
                        (id, answer) -> hasObjectTargetNameOrAlias(game.getPermanent(id), answer));
                if (chosen != null) {
                    target.addTarget(chosen, source, game);
                    return true;
                }
            }
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
        int n;
        try (BufferedReader in = new BufferedReader(new FileReader(args[0]));
             PrintWriter out = new PrintWriter(new FileWriter(args[1]))) {
            n = replayLines(r, in, out);
        }
        System.err.println("ScenarioReplay: " + n + " scenarios");
        System.exit(0);
    }

    /** Replay every scenario line, writing one result row each. ANY throwable
     * while handling a scenario -- including in replay or in replayOnce's own
     * catch/alignment path -- becomes a harness row for that scenario id, and
     * the loop goes on to the next. Returns the row count. */
    static int replayLines(ScenarioReplay r, BufferedReader in, PrintWriter out) throws Exception {
        int n = 0;
        String line;
        while ((line = in.readLine()) != null) {
            if (line.trim().isEmpty()) {
                continue;
            }
            JsonObject sc = JsonParser.parseString(line).getAsJsonObject();
            long t0 = System.nanoTime();
            JsonObject res;
            // XMage shuffles and picks "at random" from one static Random.
            // Seed it from the scenario id so a rerun replays the same luck
            // and a verdict cannot flip between identical runs.
            RandomUtil.setSeed(scenarioSeed(sc));
            try {
                res = r.replay(sc);
            } catch (Throwable t) {
                res = harnessRow(sc, true, t);
            }
            if (res.has("harness") && res.get("harness").isJsonPrimitive()) {
                res.addProperty("harness", stableMessage(res.get("harness").getAsString()));
            }
            res.addProperty("ms", (System.nanoTime() - t0) / 1_000_000);
            out.println(GSON.toJson(res));
            out.flush();
            n++;
        }
        return n;
    }

    /** The per-scenario RNG seed: the id's (or, without one, the name's)
     * String.hashCode, which the JLS fixes, so it is the same on every JVM. */
    static long scenarioSeed(JsonObject sc) {
        String key = sc.has("id") && sc.get("id").isJsonPrimitive() ? sc.get("id").getAsString() : str(sc, "name");
        return key == null ? 0L : key.hashCode();
    }

    private static final java.util.regex.Pattern OBJECT_ID = java.util.regex.Pattern.compile(" object_id='[0-9a-f-]+'");
    private static final java.util.regex.Pattern SHORT_ID = java.util.regex.Pattern.compile(" \\[[0-9a-f]{3}\\]");

    /** A harness message with XMage's per-run object ids (UUIDs from
     * UUID.randomUUID, which no seed reaches) removed: the full object_id
     * attribute and the three-hex short id XMage prints after a name. The
     * message is otherwise unchanged, so a rerun writes the same verdict. */
    static String stableMessage(String msg) {
        return SHORT_ID.matcher(OBJECT_ID.matcher(msg).replaceAll("")).replaceAll("");
    }

    /** Null-safe read of a scenario's "steps". A missing key, an explicit
     * JSON null or any non-array value yields an empty array instead of the
     * ClassCastException that used to escape replayOnce and kill the JVM, so
     * a malformed scenario fails its own row and the batch goes on. */
    static JsonArray steps(JsonObject sc) {
        JsonElement e = sc.get("steps");
        return e != null && e.isJsonArray() ? e.getAsJsonArray() : new JsonArray();
    }

    /** The result row a scenario gets when handling it throws -- including a
     * throw inside replayOnce's own catch/alignment path. Same shape as an
     * ordinary harness error so the comparator reads it unchanged. */
    private static JsonObject harnessRow(JsonObject sc, boolean strict, Throwable t) {
        JsonObject res = new JsonObject();
        res.addProperty("strict", strict);
        res.addProperty("name", str(sc, "name"));
        if (sc.has("id")) {
            res.add("id", sc.get("id"));
        }
        String msg = t.getClass().getSimpleName() + ": " + t.getMessage();
        res.addProperty("harness", msg.length() > 800 ? msg.substring(0, 800) : msg);
        res.add("snapshots", new JsonArray());
        return res;
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
        JsonObject res;
        try {
            res = replayOnce(sc, true);
        } catch (Throwable t) {
            return harnessRow(sc, true, t);
        }
        String h = res.has("harness") ? res.get("harness").getAsString() : "";
        if (h.contains("Missing") && h.contains("def for turn")) {
            JsonObject loose;
            try {
                loose = replayOnce(sc, false);
            } catch (Throwable t) {
                return harnessRow(sc, false, t);
            }
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
            // A cast-mode election is scoped to one scenario: every step's
            // actions are queued before execute(), so the cast case below
            // records it during queueing and the asks read it during execute.
            // Reset here so an earlier scenario's Bargain cannot answer this
            // one's optional additional costs.
            bargainedCast = false;
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
            attackAdvancedTurn = false;
            queueSetupChoices(sc);
            build(sc);
            if (TURN == 1) {
                // Turn 1's first priority is in upkeep, before any gameplay
                // entry (Bitterblossom's token is still on the stack). Age
                // only the seeded setup permanents recorded by build().
                java.util.Map<String, Integer> seeded = new java.util.HashMap<>(setupBattlefield);
                runCode("setup entry history", TURN, PhaseStep.UPKEEP, playerA,
                        (info, p, g) -> clearSetupEntryHistory(g, seeded));
            }
            runCode("setup", TURN, MAIN, playerA, (info, p, g) -> {
                applySetupState(g);
                registerAliases(g);
                snaps.add(snapshot(info, g));
            });
            JsonArray steps = steps(sc);
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
                queuePassCommands(i);
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
            int stepCount = steps(sc).size();
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
                    String op = str(steps(sc).get(skipped).getAsJsonObject(), "op");
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

    /**
     * CardTestPlayerAPIImpl cheats setup permanents onto the battlefield
     * before the game starts, but XMage leaves their turnsOnBattlefield at
     * zero (so EnteredThisTurnPredicate matches them) and the ETB watcher
     * shifts them into its last-turn history. Run only at turn 1's first
     * priority, before any gameplay entry: age the seeded permanents and
     * drop their last-turn history. Both passes match the recorded
     * (controller id, XMage name) multiset, so a permanent that genuinely
     * entered this turn -- an upkeep token -- keeps its zero age and its
     * watcher entry. At a later requested turn the setup permanents age
     * naturally, so this is not called.
     */
    private static void clearSetupEntryHistory(Game game, java.util.Map<String, Integer> seeded) {
        try {
            // Remaining seeded (controller|name) counts, decremented as
            // permanents are matched so duplicate names each age exactly once.
            java.util.Map<String, Integer> remaining = new java.util.HashMap<>(seeded);
            java.lang.reflect.Field turns = mage.game.permanent.PermanentImpl.class
                    .getDeclaredField("turnsOnBattlefield");
            turns.setAccessible(true);
            for (Permanent permanent : game.getBattlefield().getAllPermanents()) {
                String key = permanent.getControllerId() + "|" + permanent.getName();
                Integer left = remaining.get(key);
                if (left == null || left <= 0) {
                    continue; // genuinely entered this turn, or not a setup card
                }
                remaining.put(key, left - 1);
                turns.setInt(permanent, Math.max(1, permanent.getTurnsOnBattlefield()));
            }
            PermanentsEnteredBattlefieldWatcher watcher = game.getState()
                    .getWatcher(PermanentsEnteredBattlefieldWatcher.class);
            if (watcher != null) {
                java.lang.reflect.Field last = PermanentsEnteredBattlefieldWatcher.class
                        .getDeclaredField("enteringBattlefieldLastTurn");
                last.setAccessible(true);
                @SuppressWarnings("unchecked")
                java.util.Map<java.util.UUID, java.util.List<Permanent>> lastTurn =
                        (java.util.Map<java.util.UUID, java.util.List<Permanent>>) last.get(watcher);
                // Drop only the seeded setup entries recorded pre-game; any
                // other last-turn entry is left for its owning card.
                java.util.Map<String, Integer> watcherRemaining = new java.util.HashMap<>(seeded);
                for (java.util.List<Permanent> list : lastTurn.values()) {
                    java.util.Iterator<Permanent> it = list.iterator();
                    while (it.hasNext()) {
                        Permanent entry = it.next();
                        String key = entry.getControllerId() + "|" + entry.getName();
                        Integer left = watcherRemaining.get(key);
                        if (left != null && left > 0) {
                            watcherRemaining.put(key, left - 1);
                            it.remove();
                        }
                    }
                }
            }
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException("cannot normalize setup entry history", e);
        }
    }

    private TestPlayer seat(int i) {
        return i == 0 ? playerA : playerB;
    }

    private void build(JsonObject sc) {
        buildCounts.clear();
        setupBattlefield.clear();
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

    /** The setup's per-seat "counters" and "speed" (gorge's runner emits the same
     * CounterChange / SpeedChange events when it places the cards). Runs inside
     * the "setup" checkpoint, before the first snapshot, because a counter or a
     * speed needs a live game. "counters" is card name -> counter kind (the
     * CounterType enum name: CHARGE, P1P1, M1M1, ...) -> amount, applied to every
     * battlefield placement of the name, like "tapped"; a name or kind XMage does
     * not know fails the scenario rather than placing nothing. */
    private void applySetupState(Game g) {
        JsonObject setup = sc0.has("setup") ? sc0.getAsJsonObject("setup") : new JsonObject();
        boolean changed = false;
        for (int i = 0; i < 2; i++) {
            if (!setup.has("p" + i)) {
                continue;
            }
            JsonObject s = setup.getAsJsonObject("p" + i);
            TestPlayer pl = seat(i);
            if (s.has("counters")) {
                for (Map.Entry<String, JsonElement> byCard : s.getAsJsonObject("counters").entrySet()) {
                    String name = xmageSpelling(byCard.getKey());
                    boolean placed = false;
                    for (Permanent perm : g.getBattlefield().getAllPermanents()) {
                        // A back-face-staged permanent is still named by its front (setupNames).
                        if (!pl.getId().equals(perm.getControllerId()) || !setupNames.getOrDefault(perm.getId(), perm.getName()).equals(name)) {
                            continue;
                        }
                        for (Map.Entry<String, JsonElement> byKind : byCard.getValue().getAsJsonObject().entrySet()) {
                            CounterType kind = xmageCounter(byKind.getKey());
                            perm.addCounters(kind.createInstance(byKind.getValue().getAsInt()), pl.getId(), null, g);
                        }
                        placed = true;
                        changed = true;
                    }
                    if (!placed) {
                        throw new IllegalArgumentException("counters name " + byCard.getKey() + ", which is not on p" + i + "'s battlefield");
                    }
                }
            }
            if (s.has("speed") && s.get("speed").getAsInt() > 0) {
                // CR 702.179: speed starts at 1 and rises one step at a time to 4.
                pl.initSpeed(g);
                for (int k = 1; k < s.get("speed").getAsInt(); k++) {
                    pl.increaseSpeed(g);
                }
                changed = true;
            }
        }
        if (changed) {
            // A counter-gated static -- a Spacecraft's StationLevelAbility, "has
            // indestructible as long as it has a divinity counter on it" -- is a
            // ContinuousEffect whose condition reads the source's counters when
            // XMage last applied effects, which was before these setup adds.
            // addCounters does not itself recompute, and the snapshot below reads
            // the stale set, so XMage would show the card still uncounted (no P/T,
            // no keyword) where gorge shows it live. Apply once with the counters
            // in place, exactly as the game loop does at the start of a step.
            g.applyEffects();
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
            String xmageName = xmageSpelling(n);
            addCard(zone, p, xmageName, 1, tapped.contains(n));
            if (backFace.contains(n)) {
                stageBackFace(p, n);
                xmageName = backFaceNames.get("p" + i + ":" + xmageName);
            }
            if (zone == Zone.BATTLEFIELD) {
                // Record the seeded permanents by (controller id, current XMage
                // name), including staged back faces, so the entry-history
                // normalizer ages exactly these, never a genuine turn-1 entry.
                setupBattlefield.merge(p.getId() + "|" + xmageName, 1, Integer::sum);
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

    /** Maps a gorge counter kind (P1P1, M1M1, LOYALTY, CHARGE, ...) to XMage's
     * type: the CounterType enum constant of that name, else the counter whose
     * display name it spells. */
    private static CounterType xmageCounter(String kind) {
        try {
            return CounterType.valueOf(kind);
        } catch (IllegalArgumentException notAConstant) {
            // fall through to the display-name lookup
        }
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

    /** True when s is a scenario object ref ("p0:Name", "p1:token:Name#2").
     * A seat target ("p1") and a skip token ("[target_skip]") are not refs. */
    static boolean isScenarioRef(String s) {
        int i = s.indexOf(':');
        return !s.startsWith("[") && i > 0 && s.substring(0, i).matches("p[0-9]+");
    }

    /**
     * Binds the "@&lt;ref&gt;" alias an answer names if it is not already bound.
     * registerAliases covers every setup object; an object that entered after
     * setup (a token copy) has no alias yet, so a same-name answer naming it
     * by exact ref could not be matched. Locate it by the same counting rule
     * gorange's objRef uses: a token ranks among its controller's battlefield
     * tokens whose name CONTAINS the ref name; a card ranks among its owner's
     * live objects of that name in registerAliases' zone order. Additive and
     * idempotent: it never rebinds a ref registerAliases already bound.
     */
    private void bindAnswerAlias(String value) {
        for (String ref : answerRefs(value)) {
            bindOneAlias(ref);
        }
    }

    /** Tokens' gorge refs rank the controller's live tokens in creation order.
     * Battlefield iteration is UUID/hash order in XMage, not entry order. */
    static Permanent tokenChoice(String ref, UUID controller, Iterable<Permanent> battlefield) {
        String name = refName(ref).toLowerCase(java.util.Locale.ROOT);
        int wanted = 1;
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            wanted = Integer.parseInt(ref.substring(hash + 1));
        }
        List<Permanent> candidates = new ArrayList<>();
        for (Permanent permanent : battlefield) {
            if (permanent.isToken() && controller.equals(permanent.getControllerId())
                    && permanent.getName().toLowerCase(java.util.Locale.ROOT).contains(name)) {
                candidates.add(permanent);
            }
        }
        candidates.sort(java.util.Comparator.comparingInt(Permanent::getCreateOrder));
        return wanted > 0 && wanted <= candidates.size() ? candidates.get(wanted - 1) : null;
    }

    /** The seat index of a scenario ref: "p1:Forest#2" -> 1. */
    static int refSeat(String ref) {
        return Integer.parseInt(ref.substring(1, ref.indexOf(':')));
    }

    /** The scenario refs an answer value names. A multi-pick answer joins its
     * picks with "^" ("@p0:Wastes#27^@p0:Wastes#39"); each segment loses its
     * "@" alias marker, and a segment that is not a ref ("X=2", a label) is
     * dropped. */
    static List<String> answerRefs(String value) {
        List<String> refs = new ArrayList<>();
        for (String seg : value.split("\\^")) {
            String ref = seg.startsWith("@") ? seg.substring(1) : seg;
            if (isScenarioRef(ref)) {
                refs.add(ref);
            }
        }
        return refs;
    }

    private void bindOneAlias(String ref) {
        if (refAlias.containsKey(ref) || currentGame == null) {
            return;
        }
        int seatIndex = refSeat(ref);
        String name = refName(ref);
        boolean token = ref.contains(":token:");
        int wanted = 1;
        int hash = ref.lastIndexOf('#');
        if (hash >= 0 && ref.substring(hash + 1).matches("[0-9]+")) {
            wanted = Integer.parseInt(ref.substring(hash + 1));
        }
        mage.MageObject match = null;
        int seen = 0;
        if (token) {
            for (Permanent perm : currentGame.getBattlefield().getAllPermanents()) {
                if (!perm.isToken() || !seat(seatIndex).getId().equals(perm.getControllerId())
                        || !perm.getName().contains(name)) {
                    continue;
                }
                if (++seen == wanted) {
                    match = perm;
                    break;
                }
            }
        } else {
            List<mage.MageObject> objs = new ArrayList<>();
            Player pl = seat(seatIndex);
            for (Permanent perm : currentGame.getBattlefield().getAllPermanents()) {
                if (pl.getId().equals(perm.getControllerId())) {
                    objs.add(perm);
                }
            }
            for (Card c : pl.getHand().getCards(currentGame)) objs.add(c);
            for (Card c : pl.getGraveyard().getCards(currentGame)) objs.add(c);
            for (Card c : currentGame.getExile().getAllCards(currentGame)) {
                if (pl.getId().equals(c.getOwnerId())) objs.add(c);
            }
            for (Card c : pl.getLibrary().getCards(currentGame)) objs.add(c);
            for (mage.MageObject o : objs) {
                if (!setupNames.getOrDefault(o.getId(), o.getName()).equals(name)) {
                    continue;
                }
                if (++seen == wanted) {
                    match = o;
                    break;
                }
            }
        }
        if (match == null) {
            return;
        }
        refAlias.put(ref, "@" + ref);
        for (int j = 0; j < 2; j++) {
            try {
                seat(j).addAlias(ref, match.getId());
            } catch (IllegalArgumentException ignored) {
                // already bound on this player
            }
        }
    }

    /**
     * The choice-queue value for an answer that may name a scenario ref. The
     * driver matches an object by its "@ref" alias; an answer the generator
     * already emits as "@ref" is passed through, and a bare ref is mapped to
     * its alias here (the choice queue has no targetName step). Any other
     * value (a label, a yes/no, a skip token) is unchanged.
     */
    private String aliasChoiceValue(String v) {
        if (v.startsWith("@")) {
            bindAnswerAlias(v);
            return v;
        }
        if (isScenarioRef(v)) {
            bindAnswerAlias(v);
            return targetName(v);
        }
        return v;
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

    /** The card's spell ability, or null when the name does not resolve. */
    private static Ability spellAbility(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        return c == null ? null : c.getSpellAbility();
    }

    /**
     * Whether the named spell is a Spree card. XMage models Spree as a
     * SpreeAbility on the card (it sets the spell's modes to 1..unbounded), so
     * this is exact and structural: every Spree card is covered, including one
     * printed after this driver. The old hardcoded name set listed 11 of the
     * 21 Spree cards the corpus carries (One Last Job among the missing), so
     * its cast never got the closing mode skip and XMage failed the scenario
     * with "Missing MODE def".
     */
    private static boolean isSpreeSpell(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        return isSpreeCard(c);
    }

    /** Whether the card itself is a Spree card: XMage models Spree as a
     * SpreeAbility on it, so this is exact and structural. Card-based and
     * package-private so a contract test can pin it without the card
     * repository (which needs H2). */
    static boolean isSpreeCard(Card c) {
        return c != null && c.getAbilities().containsClass(SpreeAbility.class);
    }

    /** The XMage Zone a `move` step's lowercase destination names. XMage's
     * enum member is EXILED, not EXILE, so a bare valueOf(upper) throws on
     * gorge's "exile"; every other zone name upper-cases straight to its
     * member. Static and package-private for the driver contract test. */
    static Zone moveDestination(String to) {
        return to.equals("exile") ? Zone.EXILED : Zone.valueOf(to.toUpperCase(java.util.Locale.ROOT));
    }

    /** The Card-bearing zones a `move` step's source card may start in, in
     * search order. gorge's generator currently stages every moved card in
     * hand (candidates.go emits hand -> destination), but the op is not
     * hand-specific: a future shape that moves a card already in a graveyard,
     * library or exile must not fall into a hand-only search and throw. A
     * battlefield permanent is a Permanent, not a Card, so it is not a move
     * source (XMage moves those with Permanent.moveToZone). Package-private
     * and pure so the driver contract test can pin the coverage. */
    static List<Zone> moveSourceZones() {
        return Arrays.asList(Zone.HAND, Zone.GRAVEYARD, Zone.LIBRARY, Zone.EXILED);
    }

    /** The first card named {@code name} in any of {@link #moveSourceZones()},
     * or null. Hand is searched first, so today's generated hand -> zone move
     * is unchanged. */
    private static Card findMoveCard(Player player, Game game, String name) {
        for (Zone zone : moveSourceZones()) {
            if (zone == Zone.HAND) {
                for (Card c : player.getHand().getCards(game)) {
                    if (c.getName().equals(name)) {
                        return c;
                    }
                }
            } else if (zone == Zone.GRAVEYARD) {
                for (Card c : player.getGraveyard().getCards(game)) {
                    if (c.getName().equals(name)) {
                        return c;
                    }
                }
            } else if (zone == Zone.LIBRARY) {
                for (Card c : player.getLibrary().getCards(game)) {
                    if (c.getName().equals(name)) {
                        return c;
                    }
                }
            } else if (zone == Zone.EXILED) {
                for (Card c : game.getExile().getCardsOwned(game, player.getId())) {
                    if (c.getName().equals(name)) {
                        return c;
                    }
                }
            }
        }
        return null;
    }

    /**
     * Whether a modal spell's first target lives in a later mode: the card's
     * first mode (the one XMage's up-front {@code $target=} check reads, since
     * the scenario's chosen modes are not selected yet) has no target, but
     * another mode does. Inline {@code $target=} then throws "Ability has no
     * targets" (Cosmium Confluence: modes 1 and 2 are targetless, mode 3
     * destroys target enchantment). The target is queued instead and consumed
     * when the chosen mode's target is asked for. A card whose first mode has a
     * target already validates inline, and a targetless card has no later mode
     * target, so neither is rerouted.
     */
    static boolean firstTargetInLaterMode(Ability ability) {
        if (ability == null || ability.getModes().size() < 2 || ability.getModes().getMode() == null
                || !ability.getModes().getMode().getTargets().isEmpty()) {
            return false;
        }
        for (Mode m : ability.getModes().values()) {
            if (!m.getTargets().isEmpty()) {
                return true;
            }
        }
        return false;
    }

    /** Whether an ability has a divided-amount target (TargetAmount). */
    static boolean targetsDivided(Ability ability) {
        if (ability == null) {
            return false;
        }
        for (mage.target.Target t : ability.getAllSelectedTargets()) {
            if (t instanceof mage.target.TargetAmount) {
                return true;
            }
        }
        return false;
    }

    /**
     * The whole amount a lone recipient of a divided spell takes, or null when
     * the inline {@code $target=} form already works or the amount is not a
     * fixed number. XMage's TestPlayer sets a TargetCreaturePermanentAmount's
     * entire amount on the one named target inline (Biogenic Upgrade), but for
     * any other TargetAmount (TargetAnyTargetAmount: Twin Bolt) the inline
     * form adds the target with no share and the chooser then asks for more.
     */
    static Integer soleRecipientAmount(Ability ability) {
        if (ability == null) {
            return null;
        }
        for (mage.target.Target t : ability.getAllSelectedTargets()) {
            if (t instanceof mage.target.TargetAmount
                    && !(t instanceof mage.target.common.TargetCreaturePermanentAmount)
                    && ((mage.target.TargetAmount) t).getAmount() instanceof mage.abilities.dynamicvalue.common.StaticValue) {
                return ((mage.target.TargetAmount) t).getAmount().calculate(null, null, null);
            }
        }
        return null;
    }

    /** The queue name for a target-amount recipient: a player by name, else
     * the setup alias or XMage-spelled card name. Shared by the scripted
     * "<ref>^X=<share>" answers and the lone-recipient cast. */
    private String amountTargetName(String ref) {
        return isSeatRef(ref) ? "targetPlayer=" + seat(seatOf(ref)).getName() : targetName(ref);
    }

    /** Whether the card's spell ability has a divided-amount target. */
    private static boolean spellTargetsDivided(String name) {
        return targetsDivided(spellAbility(name));
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

    /** Whether a cast step's cast_mode is one this driver elects: Bargain,
     * "optionalcost" and the face-down Disguise/Morph/Megamorph casts are wired, and an absent or empty mode is the ordinary
     * cast. Any other mode is a scenario the driver cannot replay and must
     * reject loudly rather than cast at face value. */
    static boolean castModeSupported(String mode) {
        return mode == null || mode.isEmpty()
                || mode.equals("bargained") || mode.equals("optionalcost")
                || mode.equals("disguised") || mode.equals("morphed") || mode.equals("megamorphed");
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
    // Seeded setup permanents keyed "controllerId|xmageName" -> count (build()).
    private final java.util.Map<String, Integer> setupBattlefield = new java.util.HashMap<>();
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

    /** The front half of a split/Room card's "A // B" name, or the name
     * unchanged when it is not a split name. */
    static String frontHalf(String n) {
        int i = n.indexOf(" // ");
        return i < 0 ? n : n.substring(0, i);
    }

    /** The back half of a split/Room card's "A // B" name, or the name
     * unchanged when it is not a split name. */
    static String backHalf(String n) {
        int i = n.indexOf(" // ");
        return i < 0 ? n : n.substring(i + 4);
    }

    /** The spelling XMage's cast command matches: the name of the card's
     * SpellAbility, which differs from the card object's name for a split or
     * Room card. XMage names a half's ability "Cast <half>" (SplitCard splits
     * the set info name on " // "), while the physical card added to hand is
     * the whole "A // B"; casting the whole name finds no ability ("Can't
     * find ability to activate command: Cast Walk-In Closet"). So a cast
     * step names the scenario's face (gorgeName) and setup still deals the
     * whole card through xmageSpelling. A whole-name cast of a card that is
     * NOT the card under test -- a generator probe Room dealt by setup
     * (roomUnlockProbe) -- is the front half's ability the same way; the
     * under-test whole name maps to gorgeName so an alias spelling is kept.
     * Returns null when the name is not a split spelling and the caller
     * falls through to xmageSpelling. */
    static String castSpellingRule(String gorgeName, String xmageName, String n) {
        if (!xmageName.isEmpty() && n.equals(gorgeName) && xmageName.contains(" // ")) {
            return gorgeName;
        }
        if (n.contains(" // ")) {
            return n.equals(xmageName) ? gorgeName : frontHalf(n);
        }
        return null;
    }

    private String castSpelling(String n) {
        String s = castSpellingRule(gorgeName, xmageName, n);
        return s != null ? s : xmageSpelling(n);
    }

    /** One snapshot value's spelling rewrite: gorge spells every card object
     * by its front face, so XMage's whole "A // B" object name for a
     * split/Room card (the card under test's whole name, a probe's, an MDFC
     * land's) is rewritten to its front half, and the spell of a face-1 cast
     * -- which XMage names by the back half's ability -- is rewritten to the
     * front the way gorge names the spell. The card under test's back half
     * is the only back half a scenario can name, so it is the only alias. */
    static String gorgeSpellingRule(String gorgeName, String xmageName, String s) {
        if (!xmageName.isEmpty()) {
            if (s.equals(xmageName)) {
                return gorgeName;
            }
            if (xmageName.contains(" // ") && s.equals(backHalf(xmageName))) {
                return gorgeName;
            }
        }
        return frontHalf(s);
    }

    /** Rewrites XMage's spelling of the card under test back to the scenario's
     * (gorge) spelling throughout a value, so the comparator sees one name. */
    private JsonElement gorgeSpellings(JsonElement e) {
        if (e.isJsonPrimitive() && e.getAsJsonPrimitive().isString()) {
            String s = e.getAsString();
            String r = gorgeSpellingRule(gorgeName, xmageName, s);
            return r.equals(s) ? e : new JsonPrimitive(r);
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
            return activationCommandText(xabilities.get(i).getAsString());
        }
        return "";
    }

    /**
     * CrewAbility and SaddleAbility render their rule prefix followed by
     * HTML-wrapped reminder text. Keep the identifying ability-class prefix:
     * TestPlayer matches activated commands against ability.toString().startsWith
     * (command), and these reminders have differed between Forge and XMage.
     */
    static String activationCommandText(String text) {
        java.util.regex.Matcher keyword = java.util.regex.Pattern
                .compile("^(Crew|Saddle)\\s+\\d+\\b", java.util.regex.Pattern.CASE_INSENSITIVE)
                .matcher(text);
        if (keyword.find()) {
            return keyword.group();
        }
        return text;
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

    static final int PASS_PAIR_FIRST = 1;
    static final int PASS_HANDOFF = 2;
    static final int PASS_PAIR_SECOND = 3;

    /** The XMage commands a pass step queues on the active player, the seat
     * that carries every checkpoint. */
    static final String CMD_REQUIRE_STACK = "pass pair needs a stack object";
    static final String CMD_WAIT_RESOLVE_ONE = "waitStackResolved:1";
    static final String CMD_YIELD_PRIORITY = "pass handoff to the opponent";

    private static boolean isOp(JsonArray steps, int index, String op) {
        return index >= 0 && index < steps.size() && str(steps.get(index).getAsJsonObject(), "op").equals(op);
    }

    private static int seatOfStep(JsonArray steps, int index) {
        JsonObject st = steps.get(index).getAsJsonObject();
        return st.has("seat") ? st.get("seat").getAsInt() : 0;
    }

    /** Classify only the pass shapes emitted by Level-B scenarios; anything
     * else fails loudly. Gorge's pass answers one priority decision of the
     * named seat. Two opposing passes (p0 then p1) resolve the stack's top
     * object, so p0's pass leaves the stack as it was and only p1's pass
     * resolves. A lone p0 pass hands priority to the p1 cast that follows. */
    static int passAction(JsonArray steps, int index) {
        if (index < 0 || index >= steps.size()) {
            throw new IllegalArgumentException("pass step index out of range: " + index);
        }
        if (!isOp(steps, index, "pass")) {
            throw new IllegalArgumentException("pass action requested for non-pass step " + index);
        }
        int seat = seatOfStep(steps, index);
        if (isOp(steps, index - 1, "pass")) {
            if (seatOfStep(steps, index - 1) == 0 && seat == 1
                    && !isOp(steps, index - 2, "pass") && !isOp(steps, index + 1, "pass")) {
                return PASS_PAIR_SECOND;
            }
            throw new IllegalArgumentException("unsupported pass sequence at step " + index);
        }
        if (isOp(steps, index + 1, "pass")) {
            if (seat == 0 && seatOfStep(steps, index + 1) == 1 && !isOp(steps, index + 2, "pass")) {
                return PASS_PAIR_FIRST;
            }
            throw new IllegalArgumentException("unsupported pass pair at step " + index);
        }
        if (seat == 0 && isOp(steps, index + 1, "cast") && seatOfStep(steps, index + 1) == 1) {
            return PASS_HANDOFF;
        }
        throw new IllegalArgumentException("unsupported pass pattern at step " + index);
    }

    /** The commands step {@code index} queues before its own actions and
     * checkpoint, in queue order. XMage runs every queued action of the
     * active player at one priority in order and only then lets the opponent
     * act, so each pass effect has to sit between the right two checkpoints:
     * the first pass of a pair queues nothing (its checkpoint still sees the
     * spell, as gorge's does); the second queues the one-object resolution
     * ahead of its checkpoint; the step after a lone p0 pass starts by
     * yielding priority, so the opponent's cast is on the stack before that
     * step's checkpoint. Passes need the active player to be seat 0. */
    static List<String> passCommands(JsonArray steps, int index, int activeSeat) {
        List<String> cmds = new ArrayList<>();
        boolean passHere = isOp(steps, index, "pass");
        boolean afterHandoff = isOp(steps, index - 1, "pass")
                && passAction(steps, index - 1) == PASS_HANDOFF;
        if ((passHere || afterHandoff) && activeSeat != 0) {
            throw new IllegalArgumentException("pass at step " + index + " needs seat 0 to be the active player");
        }
        if (afterHandoff) {
            cmds.add(CMD_YIELD_PRIORITY);
        }
        if (passHere && passAction(steps, index) == PASS_PAIR_SECOND) {
            cmds.add(CMD_REQUIRE_STACK);
            cmds.add(CMD_WAIT_RESOLVE_ONE);
        }
        return cmds;
    }

    private void queuePassCommands(int stepIdx) {
        for (String cmd : passCommands(steps(sc0), stepIdx, activeSeat)) {
            switch (cmd) {
                case CMD_REQUIRE_STACK:
                    runCode(cmd, turn, phase, playerA, (info, pl, g) -> {
                        if (g.getStack().isEmpty()) {
                            throw new IllegalStateException(cmd + ": the stack is empty");
                        }
                    });
                    break;
                case CMD_WAIT_RESOLVE_ONE:
                    waitStackResolved(turn, phase, playerA, true);
                    // The pair resolves the stack's top object, so a cast
                    // step's spell has left the stack: a later cast that
                    // names it targets its permanent, not a spell that will
                    // never be on the stack (see dropLastCast).
                    dropLastCast();
                    break;
                case CMD_YIELD_PRIORITY:
                    runCode(cmd, turn, phase, playerA, (info, pl, g) -> pl.pass(g));
                    break;
                default:
                    throw new IllegalArgumentException("unknown pass command " + cmd);
            }
        }
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
                // gorge's attack op passes the current player's turn when the
                // scripted attacker belongs to the other seat.
                if (seatIdx != activeSeat) {
                    turn++;
                    activeSeat = seatIdx;
                    attackAdvancedTurn = true;
                }
                // XMage's attack() queues a selectAttackers command at
                // DECLARE_ATTACKERS; the checkpoint stays on this turn.
                List<String> attackers = names(st, "attackers");
                if (!attackers.isEmpty() && allMustAttack(attackers)) {
                    // Every attacker carries an AttacksEachCombatStaticAbility:
                    // XMage's checkAttackRequirements declares and taps it
                    // before selectAttackers runs, so getAvailableAttackers is
                    // empty on the first pass and selectAttackers (which would
                    // consume the queued attack command) is skipped. Queue no
                    // attack(): XMage declares them itself. Fail loudly if the
                    // scenario names a defender XMage would not force -- a
                    // must-attack effect (not goad) attacks any legal defender,
                    // which in this two-seat harness is the opponent.
                    int defenderSeat = seatOf(str(st, "defender"));
                    if (defenderSeat == seatIdx) {
                        throw new IllegalArgumentException("attack step " + stepIdx
                                + " is a must-attack creature with its own controller as defender");
                    }
                    phase = PhaseStep.DECLARE_ATTACKERS;
                    return;
                }
                for (String a : attackers) {
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
                        // An off-turn attack has already advanced to its
                        // attacker's turn; its matching main2 checkpoint must
                        // not advance a second time.
                        if (attackAdvancedTurn && nextActiveSeat == activeSeat) {
                            attackAdvancedTurn = false;
                        } else {
                            // pass_to asks for the next turn on this seat, not merely
                            // the next distinct seat: p0 after p0 skips p1's turn too.
                            turn += nextActiveSeat == activeSeat ? 2 : 1;
                            activeSeat = nextActiveSeat;
                            attackAdvancedTurn = false;
                        }
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
            case "move": {
                String ref = str(st, "card");
                String name = xmageSpelling(refName(ref));
                // gorge names the exile zone "exile"; XMage's enum member is EXILED.
                String to = str(st, "to");
                Zone destination = moveDestination(to);
                runCode("move " + ref + " to " + destination, turn, phase, p, (info, pl, g) -> {
                    Card moving = findMoveCard(pl, g, name);
                    if (moving == null) {
                        throw new IllegalArgumentException("move card " + ref + " is not in "
                                + pl.getName() + "'s " + moveSourceZones());
                    }
                    if (!pl.moveCards(moving, destination, null, g)) {
                        throw new IllegalStateException("move " + ref + " to " + destination + " refused");
                    }
                });
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
                if (st.has("kicked")) {
                    throw new IllegalArgumentException("kicked unsupported");
                }
                // cast_mode elects a mode whose cost is a cast-time optional
                // additional cost. Bargain's is wired here: its yes/no ask is
                // answered from bargainedCast and the sacrifice it adds is
                // matched against this step's recorded choose picks.
                // "optionalcost" is a plain cast on this side: XMage poses the
                // "pay the additional cost?" chooseUse itself, so its yes and
                // the cost's picks come from xmage_answers. Any other mode
                // fails loudly rather than being silently dropped (the legacy
                // whole-rejection this replaces).
                String castMode = st.has("cast_mode") ? str(st, "cast_mode") : "";
                if (!castModeSupported(castMode)) {
                    throw new IllegalArgumentException("cast_mode " + castMode + " unsupported");
                }
                bargainedCast = "bargained".equals(castMode);
                // "disguised" casts the card face down for {3}; XMage selects
                // that cast by suffixing the card name ("<card> using
                // Disguise", DisguiseTest/CovetedFalconTest). "morphed" and
                // "megamorphed" are the same face-down cast for the Morph and
                // Megamorph families; XMage defines no Megamorph spelling, as
                // both set SpellAbilityCastMode.MORPH, so both suffix " using
                // Morph" (MorphAbility.java:79-88; MegamorphTest.java:24).
                String mode = castMode;
                if (!sc0.has("xmage_answers")) {
                    answers(st, p);
                }
                // Spree permits choosing further modes after the first. Its
                // generated answer records the chosen mode, not the decision
                // to stop, so close XMage's repeated mode prompt explicitly.
                if (isSpreeSpell(xmageName.isEmpty() ? refName(str(st, "card")) : xmageName)) {
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
                String cardName = castSpelling(refName(str(st, "card")));
                // A face-down Morph/Megamorph/Disguise cast names the
                // SpellAbility by its XMage cast spelling; every plain cast
                // names the card itself. Morph and Megamorph share "using
                // Morph".
                String castSuffix = "morphed".equals(mode) || "megamorphed".equals(mode)
                        ? " using Morph"
                        : "disguised".equals(mode) ? " using Disguise" : "";
                String card = cardName + castSuffix;
                List<String> tg = targets(st);
                if (hasAlternativeSourceCost(cardName)) {
                    // A plain cast step: gorge paid the mana cost, so decline
                    // the alternative cost (evoke, impending, dash, ...)
                    // XMage offers through its "Cast with no alternative
                    // cost" choice, rather than leave it to the AI.
                    setChoice(p, "Cast with no alternative cost");
                }
                List<Integer> skips = castTargetSkipsAt(stepIdx, cardName, tg.size());
                if (splitScripted && spellTargetsDivided(cardName) && skips.isEmpty()) {
                    // The scripted "<ref>^X=<share>" answers name the targets
                    // and gorge's split; a target string here would be a
                    // second, unconsumed set.
                    castSpell(turn, phase, p, card);
                    cast.add(cardName);
                    return;
                }
                Ability castAbility = spellAbility(cardName);
                if (!skips.isEmpty()) {
                    // The generator named every empty optional target object
                    // and where it falls between the filled ones, so the queue
                    // is exactly the plan: no blind trailing skip.
                    boolean queued = castAbility != null
                            && (needsQueuedCastTargets(castAbility) || firstTargetInLaterMode(castAbility));
                    if (spellTargetsDivided(cardName) || queueAdjustedCastTargets(queued, false, tg.size())) {
                        throw new IllegalArgumentException("cast step " + stepIdx + " carries xmage_target_skips for " + cardName
                                + ", a divided, adjusted or later-mode-target spell the explicit skip plan does not cover");
                    }
                    queueCastTargetsWithSkips(p, tg, skips);
                    castSpell(turn, phase, p, card);
                } else if (!tg.isEmpty() && castQueuedTargets(turn, phase, p, cardName, tg, castAbility)) {
                    // Cast with its targets queued: see castQueuedTargets.
                } else if (tg.size() == 1 && isSeatRef(tg.get(0)) && !hasGift(cardName)) {
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
                    cast.add(cardName);
                    return;
                } else if (tg.size() == 1 && cast.contains(castSpelling(refName(tg.get(0))))) {
                    // Targeting a spell cast by an earlier step: wait for it
                    // on the stack. cast holds cast-command names and the
                    // target is a scenario ref; the setup alias names the
                    // card in hand, not the spell, so target by name.
                    String spell = castSpelling(refName(tg.get(0)));
                    castSpell(turn, phase, p, card, spell, spell);
                } else if (tg.size() == 1) {
                    // A single target goes through XMage's own string form. A
                    // divided target whose inline form XMage cannot read
                    // (Twin Bolt) was queued by castQueuedTargets above.
                    castSpell(turn, phase, p, card, targetName(tg.get(0)));
                } else {
                    // Two or more targets: queue each through addTarget and
                    // cast with no $target, so an "up to N" slot stays open
                    // and same-name permanents are told apart by the alias
                    // each ref carries. A trailing skip closes any slot XMage
                    // offers that this scenario did not fill (a reflexive
                    // sub-ability with no legal target, say).
                    if (spellTargetsDivided(cardName)) {
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
                        cast.add(cardName);
                        return;
                    }
                    for (String t : tg) {
                        queueCastTarget(p, t);
                    }
                    if (!singleTargetFilled(cardName, tg.size())) {
                        // Close an "up to N" slot the scenario left short,
                        // or a later slot (Rhino's Rampage's reflexive
                        // trigger). A skip after the one multi-target slot
                        // is filled is rejected (Pull Through the Weft).
                        addTarget(p, TestPlayer.TARGET_SKIP);
                    }
                    castSpell(turn, phase, p, card);
                }
                cast.add(cardName);
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
            case "pass":
                // Validated here; its XMage commands are queued around the
                // checkpoint by queuePassCommands, which the replay loop runs before this step.
                passAction(steps(sc0), stepIdx);
                return;
            case "resolve":
                // gorge's resolve op passes priority until the stack is empty.
                waitStackResolved(turn, phase, p);
                // waitStackResolved empties the stack, so every cast recorded
                // on it is gone. A later cast targeting one of those names is
                // targeting the permanent (the setup alias still names it),
                // not a spell that is no longer on the stack.
                cast.clear();
                return;
            default:
                throw new IllegalArgumentException("op " + op + " unsupported");
        }
    }

    /** Queue as-enters choices before addCard places setup permanents. */
    private void queueSetupChoices(JsonObject scenario) {
        if (!scenario.has("xmage_answers") || !scenario.get("xmage_answers").isJsonArray()) {
            return;
        }
        JsonArray answers = scenario.getAsJsonArray("xmage_answers");
        if (answers.size() == 0 || !answers.get(0).isJsonArray()) {
            return;
        }
        for (JsonElement e : answers.get(0).getAsJsonArray()) {
            JsonObject a = e.getAsJsonObject();
            if (!str(a, "kind").equals("setup_choice")) {
                continue;
            }
            queueSetupChoices(seat(a.get("seat").getAsInt()), str(a, "value"));
        }
    }

    static void queueSetupChoices(TestPlayer player, String value) {
        queueChoice(player, value);
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
                        bindAnswerAlias(parts[0]);
                        addTarget(p, amountTargetName(parts[0]) + "^X=" + parts[1]);
                    } else {
                        if (isScenarioRef(v)) {
                            // A same-name target the generator names by its
                            // exact ref: bind its @alias if it entered after
                            // setup, so targetName resolves it.
                            bindAnswerAlias(v);
                        }
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
                case "setup_choice":
                    // Queued before setup placement; never enqueue it again at
                    // the corresponding gameplay step.
                    break;
                case "choice":
                    if (v.equals("yes") || v.equals("no")) {
                        setChoice(p, v.equals("yes"));
                    } else if (isSeatRef(v)) {
                        setChoice(p, seat(seatOf(v)).getName());
                    } else {
                        queueChoice(p, aliasChoiceValue(v));
                    }
                    break;
                default:
                    throw new IllegalArgumentException("xmage answer kind " + kind);
            }
        }
    }

    /** Queue one label answer on the player's choice queue (FIFO). A mana
     * ability that adds a colour of the controller's choice, and a permanent's
     * "as it enters, choose a color", both pose XMage's ChoiceColor dialog,
     * which TestPlayer answers from this queue by colour name ("White"); with
     * the queue empty it picks a colour at random. */
    static void queueChoice(TestPlayer p, String v) {
        p.addChoice(v.equals("[choice_skip]") ? TestPlayer.CHOICE_SKIP : v);
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

    /**
     * Cast through the target queue when the inline {@code $target=} form
     * cannot work: an adjuster may add the SpellAbility's target slots only
     * after cast setup, and a modal spell's first target may live in a mode
     * after the first. Inline $target is validated too early (against the
     * unadjusted ability's first mode, which has no target), so the scenario
     * targets are queued for the chooser that runs during casting and the
     * cast carries none. Returns false, doing nothing, for every other cast.
     */
    boolean castQueuedTargets(int turn, PhaseStep phase, TestPlayer p, String card, List<String> tg,
            Ability ability) {
        // Derive every routing flag HERE, from the one ability, so a caller
        // cannot drop the modal-first-target-in-a-later-mode term: the test
        // drives this method with a real card's spell ability, and a routing
        // term removed anywhere in it makes that test fail.
        boolean adjusted = needsQueuedCastTargets(ability);
        boolean firstTargetInLaterMode = firstTargetInLaterMode(ability);
        boolean divided = targetsDivided(ability);
        Integer soleShare = tg.size() == 1 ? soleRecipientAmount(ability) : null;
        if (soleShare != null) {
            // A divided target XMage's inline $target= cannot fill (Twin
            // Bolt's TargetAnyTargetAmount: "selected 1 of 2"): the only
            // recipient takes the whole amount, which chooseTargetAmount
            // reads from the queue as "<name>^X=<amount>" while casting.
            addTarget(p, amountTargetName(tg.get(0)) + "^X=" + soleShare);
            castSpell(turn, phase, p, card);
            return true;
        }
        if (!queueAdjustedCastTargets(adjusted || firstTargetInLaterMode, divided, tg.size())) {
            return false;
        }
        for (String t : tg) {
            queueCastTarget(p, t);
        }
        if (adjusted) {
            adjustedCasts.add(card);
        }
        castSpell(turn, phase, p, card);
        return true;
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

    /** Whether every named attacker is a card XMage forces to attack (an
     * AttacksEachCombatStaticAbility), so checkAttackRequirements declares and
     * taps it before selectAttackers, which then skips the queued attack
     * command on its first pass. Anything that is not such a card means the
     * ordinary attack() path must run. */
    private boolean allMustAttack(List<String> attackers) {
        for (String ref : attackers) {
            if (!isMustAttackName(combatName(ref))) {
                return false;
            }
        }
        return true;
    }

    /** Whether the named card carries XMage's AttacksEachCombatStaticAbility.
     * Structural and name-based, the way spellAbility and isSpreeCard resolve
     * a name through CardRepository, so a card printed after this driver is
     * covered by what it actually is, not a list of names. */
    private static boolean isMustAttackName(String name) {
        CardInfo info = CardRepository.instance.findCard(name);
        Card c = info == null ? null : info.createCard();
        return isMustAttackCard(c);
    }

    /** Card-based and package-private so a contract test can pin it without
     * the card repository (which needs H2). */
    static boolean isMustAttackCard(Card c) {
        return c != null && c.getAbilities().containsClass(AttacksEachCombatStaticAbility.class);
    }

    /** Forget a cast whose spell has left the stack, so a later cast that
     * names it falls through to the setup alias naming its permanent. */
    private void dropLastCast() {
        if (!cast.isEmpty()) {
            cast.remove(cast.size() - 1);
        }
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
