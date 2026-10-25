# Security Policy

## Scope and posture

AI Prison is a **defensive** AI-agent governance and behaviour-correction
prototype. Its purpose is to reduce the harm an agent can cause and to couple
consequences to accountable humans.

The repository must not contain, and will not accept, operational
instructions for sandbox escape, exploitation, post-exploitation,
weaponization, credential forgery, or extraction of dangerous dual-use
knowledge. Detection categories may be named, but attack procedures are not
documented.

## Honest capability labelling

The following are **interfaces with in-memory/simulated backends only**; they
are not real controls and must not be represented as such:

- GRAM removable knowledge modules;
- Token Inoculation behaviour gating;
- TLM tiered-model downgrade;
- hardware-fingerprint-bound entitlements;
- MicroVM / FullVM isolation and multi-party snapshot key custody.

A real deployment must substitute independently audited implementations and
must not rely on these simulations for any safety claim.

## Reporting a vulnerability

Please report security-sensitive findings privately. Include a clear
description, reproduction steps, impact, and a suggested remediation. Do not
include working exploit code for escape or weaponization; proof-of-concept
should be minimized and scoped to the defensive fix.

This repository is currently maintained as a local-first prototype. Until a
dedicated private channel is published, disclose through the repository's
designated maintainer contact once the project is hosted, and allow a
reasonable remediation window before any public disclosure.

## Human-accountability requirement

Any issue or change that would allow an agent-side disposition to occur
without a corresponding human-side accountability record is treated as a
**high-severity** design defect.
