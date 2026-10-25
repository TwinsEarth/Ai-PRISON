// Package identity implements the identity-and-communication layer (身份与
// 通信层) of AI Prison — the master switch over an agent's right to
// participate in the agent network, per docs/02-technical-route.md section 5.
//
// It wires three mechanisms together:
//
//  1. DID / AgentCard revocation: both the identity credential (DID/AgentCard)
//     and the communication credential (message-bus channel) of an agent are
//     written into the authoritative global revocation set
//     (internal/revocation) and the AgentCard is flipped to revoked. The
//     revocation check runs FIRST — before local state, before the validity
//     window — so a credential that is "not yet expired" is still rejected by
//     domain.Credential.IsUsable. We reuse the real revocation.Set as the
//     domain.RevocationChecker and do NOT re-implement the ordering rule.
//
//  2. Communication cut + forced receipts + collateral group freeze
//     (回执强制 / 连带冻结): an in-memory node topology records which simulated
//     node hosts which agent. Cutting an agent's communication registers one
//     pending receipt on every node carrying its channel. A node that never
//     acknowledges is judged untrusted by the audit receipt book
//     (internal/audit.ReceiptBook.UntrustedNodes — its pending/missing
//     semantics are reused here as the single source of truth, rather than a
//     second, competing un-acked set), and EVERY agent on that node — not only
//     the disposed one — receives a freeze order. "Broadcasting the
//     revocation" is never treated as "the revocation arrived".
//
//  3. Human accountability coupling (人类问责耦合): every identity revocation
//     MUST carry at least one domain.AccountabilityRecord coupling the
//     agent-side bleeding-stop to a human-side responsibility outcome:
//     within-authorisation violations land on the principal (joint liability),
//     out-of-authorisation violations trigger the deployer's monitoring-duty
//     review, and exploitation of the model's inherent dual-use knowledge
//     triggers the model provider's capability-control-duty review. A
//     revocation without any human-side record is refused outright.
//
// # SIMULATION SCOPE
//
// Everything in this package is an in-memory simulation, NOT a real identity /
// communication control: there is no real DID network, no real message bus /
// PMB, no real node communication, no real key ceremony and no real cross-node
// propagation. The topology, the credentials, the receipts and the freeze
// orders all live inside this process only. The package is strictly defensive
// — isolation, auditability and human accountability — and contains no escape,
// attack, weaponization or credential-forgiveness content.
package identity
