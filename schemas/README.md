# JSON Schema Contracts

These files are the machine-readable wire contracts for AI Prison, written
against [JSON Schema draft 2020-12](https://json-schema.org/draft/2020-12/schema).
They are kept in lock-step with the Go types in `internal/domain` and the
public aliases in `pkg/contracts`.

| Schema | Go type | Purpose |
|---|---|---|
| `agent-card.schema.json` | `domain.AgentCard` | Public agent identity profile (voided at L5) |
| `violation.schema.json` | `domain.Violation` | Detected incident / case opening |
| `sentence.schema.json` | `domain.Sentence` | Adjudicated disposition incl. votes and accountability |
| `accountability-record.schema.json` | `domain.AccountabilityRecord` | Human-side responsibility coupling |
| `credential.schema.json` | `domain.Credential` | Revocable token |
| `revocation-entry.schema.json` | `revocation.Entry` (v0.2) | Read-only global revocation set entry |
| `event.schema.json` | `domain.Event` | Immutable hash-chain event |
| `receipt.schema.json` | `domain.Receipt` | Signed node acknowledgement |
| `compliance-report.schema.json` | `contracts.ComplianceReport` | Per-hop vs end-to-end compliance |

## Cardinal rules encoded here

1. **Revocation is checked before expiry.** A credential that is inside its
   validity window but present in the revocation set is rejected.
2. **L0–L5 are the only valid grades.** Appeal suspension applies to L0–L3;
   L4 requires recertification; L5 is permanent and non-recoverable.
3. **Every sentence carries at least one human accountability record.**
4. **Receipts are explicit.** A missing acknowledgement is `missing`, the
   node is treated as untrusted, and its agents are frozen as a group.
