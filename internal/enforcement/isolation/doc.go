// Package isolation implements the isolation layer (隔离层) of AI Prison:
// the graduated sandbox strength continuum — Wasmtime → Docker → Firecracker
// MicroVM → Full VM — mapped to disposition levels, escalated strictly
// stepwise, and paired with a behaviour monitor that freezes an agent DURING
// an anomaly (not after a hypothetical escape has completed).
//
// The design rule is deliberate: the project does not put every agent in the
// heaviest sandbox. Daily monitoring runs in a lightweight sandbox; isolation
// is escalated rung by rung only when a threshold is crossed, and migration
// into a heavier sandbox is never an instant switch — the new controlled
// environment is established and confirmed ready BEFORE the old environment's
// resources are cut, so no escape window or state loss exists mid-switch.
//
// # SIMULATION SCOPE
//
// Every backend in this package is an in-memory simulation. There is NO real
// Wasmtime runtime, NO real Docker/container runtime, NO real Firecracker /
// KVM MicroVM and NO real hardware isolation or guest kernel. Start delays are
// recorded, not waited on; "establish" and "release" only touch Go maps. The
// syscall / network / file-access signals are defensive category names only —
// they carry no exploit detail, and the package contains no escape, attack,
// weaponization or dangerous-knowledge-extraction content.
package isolation
