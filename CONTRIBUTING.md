# Contributing to AI Prison

Thanks for your interest. AI Prison is a **defensive** governance project;
please read the scope and safety expectations below before contributing.

## What we accept

- Improvements to the governance state machine, grading rules and auditing.
- Correctness tests, especially for the cardinal rules (revocation-before-
  expiry, grouped node freezing, gradual downgrade, hash-chain integrity).
- Clarifications and translations that keep docs, schemas and code in
  agreement.
- Additional defensive isolation / accountability adapters.

## What we will not accept

- Any operational content for sandbox escape, exploitation, post-exploitation,
  weaponization, credential forgery or dangerous-knowledge extraction.
- Features that weaken the human-accountability coupling, bypass approvals,
  remove audit trails, or make dispositions inappealable without record.
- Code that presents a simulated/frontier control (GRAM, TLM, hardware
  binding, MicroVM) as a real, shipped control.

## Development setup

Requirements: Go 1.27+. The project intentionally uses only the standard
library.

```bash
make check   # gofmt + vet + test (-race) + build
```

## Pull-request checklist

- [ ] `make check` is green.
- [ ] New behaviour has targeted unit tests.
- [ ] Grading / sequence / principle changes are reflected in `docs/`,
      `schemas/` and `internal/domain` together.
- [ ] Simulated vs real capabilities are labelled honestly in code and docs.
- [ ] `CHANGELOG.md` has an entry under `Unreleased`.
- [ ] No offensive content; no external dependency without justification.

## Commit style

Use short, imperative subjects (`revocation: check membership before
expiry`). Group changes by package. Keep each version's gate green at the
commit that carries its tag.

## Reporting vulnerabilities

Do **not** open a public issue for security-sensitive reports; follow
[SECURITY.md](SECURITY.md).
