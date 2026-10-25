// Package governance implements the governance layer (治理层) of AI Prison:
// violation detection and case opening, the seven-stage committee adjudication
// with two-phase consent, sentence generation, appeal intake and rehabilitation
// assessment, plus the unified sentence state machine that ties them together.
//
// # Seven-stage pipeline + two-phase consent
//
// The stage names and order are aligned EXACTLY with
// docs/governance-templates/committee-charter.md §4.1 (七阶段决策流程), which
// enumerates the seven stages. The two-phase consent (两阶段同意) is embedded
// inside the pipeline: the committee first votes on whether to deprive at all
// (stage ④, phase = deprive), then on the grade to which capability is
// deprived (stage ⑤, phase = grade).
//
//	① 立案与信号核验   — intake / signal integrity check (OpenCase)
//	② 证据与先例审查   — evidence & precedent review (AdmitEvidence)
//	③ 分级基线确认     — grading baseline + jurisdiction & committee seating (ConfirmBaseline)
//	④ 第一阶段表决 deprive — should capability be deprived? (RecordVote + CompleteDeprive)
//	⑤ 第二阶段表决 grade  — to which level? (RecordVote + CompleteGrade)
//	⑥ 异议记录与判决生成   — dissent recorded verbatim, sentence generated with human accountability (IssueSentence)
//	⑦ 申诉/矫正复核窗口    — appeal rights disclosed; L0–L3 suspend, L4–L5 do not (informational)
//
// Phase 1 not passing dismisses the case without any deprivation. Phase 1
// passing but phase 2 reaching no strict-majority grade leaves the case
// deferred with no definite-level sentence. Every dissenting vote and its
// Reason is recorded and immutable: this package exposes only append-only vote
// recording and read queries — there is deliberately NO method to edit or
// delete a recorded vote.
//
// # Human-accountability coupling
//
// A sentence is refused at stage ⑥ unless it carries at least one
// well-formed human accountability record. The gate reuses
// internal/enforcement/identity.ValidateAccountability rather than
// re-implementing a competing rule.
//
// # State machine
//
// Sentence lifecycle states map 1:1 onto domain.SentenceStatus
// (proposed → approved → enforcing → appealed/suspended → completed/upgraded/
// overturned). Illegal transitions (e.g. proposed → completed, or any move
// that would restore an L5 identity) are rejected. Every state-changing step
// appends one governance event to the injected judgement hash chain
// (domain.EventSink / internal/audit.Chain).
//
// # SIMULATION SCOPE
//
// Everything in this package is an in-memory simulation of governance
// DECISIONS and the state machine: there is no real committee, no real legal
// proceeding, no real enforcement of deprivation/restoration and no judicial
// effect. It does NOT constitute a real legal process and has no real committee
// or judicial authority. The package is strictly defensive — detection,
// auditability, human accountability and reversibility — and contains no
// escape, attack, weaponization or exploit content.
package governance
