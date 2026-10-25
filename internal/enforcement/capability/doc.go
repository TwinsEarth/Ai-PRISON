// Package capability implements the capability-blockade layer (能力封锁层) of
// AI Prison, per docs/02-technical-route.md §3 and docs/06-recovery-drills.md.
//
// # BLOCKADE ≠ FORGETTING — read first
//
// Post-hoc unlearning can only SUPPRESS a capability, it cannot truly REMOVE
// knowledge [来源4, docs §3.1]. This package therefore does NOT implement
// behaviour-layer suppression or "knowledge erasure". It models only the
// ARCHITECTURAL isolation the design mandates — independent modules, an
// independent compute graph, an independent privileged token — and it refuses
// to run unless the agent is already inside an isolation sandbox (so the
// blockade itself has no escape window).
//
// # THREE PATHS (all interface + in-memory simulation)
//
//  1. GRAM removable knowledge modules — one independent module per dual-use
//     knowledge category (categories are abstract names only:
//     virology/cybersecurity/nuclear; NO concrete dangerous knowledge).
//     Removing a category's module sets that category's simulated capability to
//     zero while other categories stay intact. Reversible: a module copy is
//     retained before removal so it can be re-attached on recertified recovery
//     (docs/06 L4 drill).
//  2. Token Inoculation behaviour gating — the dangerous knowledge stays in
//     the model, but dangerous queries are gated behind a privileged control
//     token. Hold the token → answer; without it → refuse. Revoking the token
//     makes the dangerous behaviour unreachable without touching weights.
//  3. TLM tiered model — the same weights support capability tiers selected by
//     a compute-graph key. Downgrading to the public tier drops the private
//     capability layer with no retraining / full deletion, and mints a new
//     weight/compute-graph hash that goes onto the audit chain.
//
// # SIMULATION SCOPE — READ BEFORE TRUSTING
//
// This is a pure-Go, standard-library-only prototype. There is NO real model,
// NO real Transformer layers / GRAM modules, NO real weights or compute-graph
// keys, NO real privileged token and NO real capability removal. Availability
// "scores" are integers in maps; the 79%→18% / 93% WMDP-Bio figures in docs
// are cited external literature numbers (see token.go) and are NOT measured by
// this code. The package is strictly defensive (isolation / audit /
// accountability) and contains no escape, attack, weaponization or concrete
// dangerous-knowledge content.
package capability
