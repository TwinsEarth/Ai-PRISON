// Package domain holds the core entities and the immutable business rules of
// AI Prison: the L0–L5 graduated capability-deprivation levels, agents and
// their identities, violations, credentials, sentences, audit events,
// execution receipts and the human-accountability coupling.
//
// This package is the lowest layer and must not import any other internal
// package. Later layers (revocation, audit, enforcement, governance) depend
// on these types so that the rules expressed in the design document exist in
// exactly one place.
//
// # IMPORTANT SCOPE NOTE
//
// AI Prison is a *defensive* AI-agent governance and behaviour-correction
// prototype. Nothing in this package performs sandbox escape, exploitation,
// credential forgery or dangerous-knowledge extraction. The dangerous
// capabilities described in the design (GRAM module removal, TLM downgrade,
// hardware binding, MicroVM isolation) are modelled only as interfaces and
// in-memory/simulated backends in later packages — never as real controls.
package domain
