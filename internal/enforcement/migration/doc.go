// Package migration implements the migration-suppression layer (迁移抑制层)
// of AI Prison, per docs/02-technical-route.md §4.
//
// The goal is deliberately NOT "block migration" — in an open-weight /
// open-source ecosystem that is impossible. The goal is to make a migrated
// copy worthless:
//
//  1. Hardware fingerprint binding (硬件绑定) — a capability credential bound
//     to a simulated hardware fingerprint only validates on the machine whose
//     fingerprint matches; a copy that lands on another machine fails
//     validation. This only deters casual copying; it is NOT an unbreakable
//     hardware root of trust.
//  2. Portable work-record anchor key (可迁移工作记录的锚定密钥) — an agent's
//     lessons, results and reputation are anchored to an agent-owned simulated
//     key. Normally the record travels with the key; at L5 the anchor key is
//     revoked, so a migrated copy cannot carry the original identity's
//     reputation and history (a reputation-less agent has ~zero collaboration
//     opportunity in the agent economy).
//  3. Multi-party snapshot custody (快照多方加密封存) — the environment
//     snapshot (pack_diff) is split across N simulated custodians behind an
//     M-of-N threshold. Loading the sealed snapshot needs at least M shares;
//     below the threshold unseal fails, so a migrated copy cannot inherit the
//     accumulated collaboration state / intermediate results.
//
// The orchestrator SuppressMigration(agentID, level) applies the combination
// the level profile mandates: hardware binding at L3+, snapshot custody and
// anchor-key revocation at L5.
//
// # SIMULATION SCOPE — READ BEFORE TRUSTING
//
// EVERYTHING here is an interface + in-memory simulation, not a real control:
//
//   - the hardware fingerprint is a plain string; there is NO real hardware
//     root of trust, TPM, Secure Enclave or signed attestation — the binding
//     only deters casual copying;
//   - the "anchor key" is a simulated key id in a Go map, not a real key
//     custody / PKI ecosystem;
//   - the M-of-N "shamir" is a simulated threshold bookkeeping over custodian
//     names; there is NO real secret sharing / threshold cryptography /
//     multi-party encryption;
//   - snapshots exist only as hashes in this process.
//
// The package is strictly defensive (isolation / audit / accountability): it
// contains no escape, attack, weaponization or real dangerous-knowledge
// extraction.
package migration
