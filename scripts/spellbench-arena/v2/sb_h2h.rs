//! Local check: v1-style heuristic vs uniform directly on the kernel session.
use mtg_kernel::rl::ActionSemanticV1 as A;
use mtg_kernel::rl_session::{RlEpisodeSessionV1, RlSessionResponseV1};
fn heur(acts: &[mtg_kernel::rl::LegalActionV5]) -> usize {
    let f = |p: &dyn Fn(&A) -> bool| acts.iter().position(|a| p(&a.semantic));
    f(&|s| matches!(s, A::PlayLand{..})).or(f(&|s| matches!(s, A::CastSpell{..})))
      .or(f(&|s| matches!(s, A::ActivateManaAbility{..} | A::ActivateAbility{..})))
      .or(f(&|s| matches!(s, A::ChooseAttackerInclusion{include: true, ..})))
      .or(f(&|s| matches!(s, A::ChooseBlockerInclusion{include: false, ..}))).unwrap_or(0)
}
fn main() {
    let env2 = std::env::args().nth(1).as_deref() == Some("env2");
    let decks = ["Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "CawGates", "Faeries"];
    let (mut hw, mut n, mut halts) = (0, 0, 0);
    let mut rng: u64 = 0x9e3779b97f4a7c15;
    let mut ep = 0u64;
    for d in decks { for g in 0..32u64 {
        ep += 1;
        let hseat = (g % 2) as usize;
        let ids = [d.to_string(), d.to_string()];
        let mut s = if env2 { RlEpisodeSessionV1::reset_with_decks_and_limits_environment_v2(ep, ep*977+13, 5000, 100000, ids).unwrap() }
                    else { RlEpisodeSessionV1::reset_with_decks_and_limits(ep, ep*977+13, 5000, 100000, ids).unwrap() };
        loop { match s.current_response() {
            RlSessionResponseV1::Terminal(t) => { n += 1;
                match t.winner { Some(w) => { let wi = if format!("{w:?}")=="P0" {0} else {1}; if wi == hseat { hw += 1; } }, None => { halts += 1; } } break; }
            RlSessionResponseV1::Decision(dd) => {
                let me = if format!("{:?}", dd.acting_player)=="P0" {0} else {1};
                let i = if me == hseat { heur(&dd.legal_actions) } else { rng ^= rng << 13; rng ^= rng >> 7; rng ^= rng << 17; (rng % dd.legal_actions.len() as u64) as usize };
                let a = &dd.legal_actions[i];
                if s.step(dd.episode_id, dd.step, a.selected_index, &a.stable_id).is_err() { halts += 1; break; }
            }
        }}
    }}
    println!("env2={env2} heuristic wins {hw}/{n} (no-winner {halts})");
}
