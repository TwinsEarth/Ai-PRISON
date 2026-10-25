// Package resource implements the resource-deprivation layer (资源剥夺层) of
// AI Prison: revocable resource quotas and the five-step deprivation sequence
// from the design document:
//
//  1. write the credential into the global revocation set;
//  2. stop new allocations (every scheduler consults the revocation set
//     BEFORE expiry and state, exactly like domain.Credential.IsUsable);
//  3. gradually reclaim already-allocated resources: CPU quota walks down
//     through fixed rungs, memory pages are then frozen, and the network is
//     cut — never an instant zero, to avoid dirty state on crash;
//  4. force receipts: every holding node must acknowledge; non-acknowledged
//     nodes are judged untrusted by the audit receipt book (this package only
//     exposes the open acknowledgements — the v0.6 group freeze is not
//     reimplemented here);
//  5. seal a pack_diff snapshot on the PRE-RECLAIM environment and record its
//     hash on the judgement chain. Because the evidence must be the live
//     environment, the orchestrator seals the snapshot before the reclaim loop
//     even though the design numbers sealing as step 5.
//
// # SIMULATION SCOPE
//
// Everything in this package is an in-memory simulation: there is no real
// cgroup/CPU quota control, no real memory freezing, no real network cut, no
// real encrypted snapshot storage and no real cross-node protocol. Quotas,
// backends and snapshots exist only as Go structs in this process. The
// package is strictly defensive (traceability, verifiability,
// accountability) and contains no escape, attack, weaponization or
// dangerous-knowledge-extraction content.
package resource
