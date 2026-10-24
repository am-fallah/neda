# Neda — Dependency and Vulnerability Review

**Date:** 2026-10-25
**Task:** `SEC-004` (dependency/vulnerability review, pinning, licenses)
**Scope:** Go module (`go.mod`/`go.sum`) and frontend (`web/package-lock.json`).
No dependency was added without a concrete product need.

## 1. Reproducibility and pinning

| Ecosystem | Pin | Verification |
|---|---|---|
| Go | `go 1.27.3`; direct and indirect versions in `go.mod`; checksums in `go.sum` | `go mod tidy`; `git diff --exit-code -- go.mod go.sum` in CI |
| Node/npm | `web/package-lock.json`; `npm ci` only; Node 24 in CI | `npm ci`; no floating install in CI |
| Container base | Not yet added (`OPS-001`) | Pending |

No `replace` directives, Git-ref requirements, or unverified lifecycle scripts
are used. The one `npm ci` install-script warning is `fsevents` (optional macOS
watch dependency); it is not required for build/test CI and no script was
approved or executed by this review.

## 2. Go dependency inventory

| Module | Version | Use | License |
|---|---|---|---|
| `github.com/SherClockHolmes/webpush-go` | v1.4.0 | RFC 8291 encryption + VAPID headers; no hand-rolled Push crypto | MIT |
| `github.com/golang-jwt/jwt/v5` | v5.2.2 | Transitive VAPID JWT signing | MIT |
| `github.com/google/uuid` | v1.6.0 | Monitor/subscription/delivery identifiers | BSD-3-Clause |
| `github.com/yaa110/go-persian-calendar` | v1.3.0 | Gregorian ↔ Persian calendar conversion for display | MIT |
| `golang.org/x/crypto` | v0.58.0 | Argon2id password hashing | BSD-3-Clause |
| `golang.org/x/term` | v0.47.0 | No-echo password CLI | BSD-3-Clause |
| `golang.org/x/text` | v0.43.0 | Transitive Unicode support | BSD-3-Clause |
| `golang.org/x/sys` | v0.49.0 | Transitive/syscall support | BSD-3-Clause |
| `modernc.org/sqlite` | v1.61.0 | Pure-Go SQLite (no CGO) | BSD-3-Clause |
| `modernc.org/libc`, `modernc.org/mathutil`, `modernc.org/memory` | pinned | SQLite transitive | BSD-3-Clause |
| `github.com/ncruces/go-strftime` | v1.1.0 | SQLite transitive | MIT |
| `github.com/dustin/go-humanize` | v1.1.0 | SQLite transitive | MIT |
| `github.com/mattn/go-isatty` | v0.0.24 | Terminal detection | MIT |
| `github.com/remyoudompheng/bigfft` | pinned | SQLite transitive | BSD-3-Clause |

## 3. Frontend dependency inventory

Runtime (production) dependencies are React 19, React DOM, React Router,
Radix UI primitives, `class-variance-authority`, `clsx`, `tailwind-merge`,
`lucide-react`, `sonner`, `react-hook-form`, `zod`, and `jalaali-js`. All are
permissive licenses (MIT/ISC/Apache-2.0/BSD/0BSD). The project package itself is
private and marked `UNLICENSED`; it is not published.

`license-checker --summary` on the full installed tree reports:

```
MIT 247, Apache-2.0 19, ISC 13, BSD-2-Clause 8, MPL-2.0 4,
BSD-3-Clause 3, MIT-0 2, BlueOak-1.0.0 2, CC-BY-4.0 1,
CC0-1.0 1, UNLICENSED 1, 0BSD 1
```

The production-only tree reports `MIT 60, Apache-2.0 1, ISC 1, UNLICENSED 1,
0BSD 1`. The four MPL-2.0 packages are dev/build-only and are not bundled into
the runtime SPA. No GPL/AGPL runtime dependency was found.

## 4. Vulnerability scans

### Go

Command:

```
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Result on 2026-10-25:

```
No vulnerabilities found.
Your code is affected by 0 vulnerabilities.
This scan also found 0 vulnerabilities in packages you import and 1
vulnerability in modules you require, but your code doesn't appear to call these
vulnerabilities.
```

`-show verbose` identifies the remaining module-level advisory:

- `GO-2026-5932`: `golang.org/x/crypto/openpgp` is unmaintained and unsafe by
  design. Neda does not import or call `openpgp`; it uses `x/crypto/argon2` and
  `x/crypto/hkdf` paths. No fixed version exists. **Exception: accepted because
  the affected package is not imported or reachable.**

A called-path advisory was found during the initial review and fixed before
merge: `GO-2025-3553` in `golang-jwt/jwt/v5` v5.2.1. The module was upgraded to
v5.2.2; the rescan reports no called vulnerabilities.

### Frontend

Command:

```
cd web && npm audit --audit-level=low
```

Result on 2026-10-25: `found 0 vulnerabilities`.

`npm outdated`/version freshness is not treated as a security result. Major
upgrades are deliberate and tested; the lockfile is the deployment artifact.

## 5. Maintenance and review notes

- `webpush-go` is the only library that handles Push encryption. Its VAPID key
  generation and RFC 8291 payload encryption are used directly; Neda adds the
  URL/SSRF policy, transport, retry classification, and subscription storage.
- `yaa110/go-persian-calendar` is MIT, pure Go, and covered by known-date tests in
  `internal/jalali`; no hand-written calendar arithmetic is used.
- `modernc.org/sqlite` avoids CGO and is pinned; database migrations and foreign
  keys are tested independently of the driver.
- Argon2id parameters are explicit in `internal/auth/password.go` and recorded
  in the architecture. Password hashing concurrency is bounded.
- Any dependency addition must update this document, pass `govulncheck` and
  `npm audit`, preserve lockfiles, and avoid install scripts without review.

## 6. Residual risk

- Third-party supply-chain compromise cannot be eliminated by scanning. Builds
  use pinned lockfiles and CI, but no reproducible-build attestation exists yet.
- The openpgp module-level advisory remains visible to scanners even though the
  package is not imported. Do not use `golang.org/x/crypto/openpgp`.
- `npm audit` covers the npm advisory database only; browser/platform Push
  behavior and provider services are outside its scope.
- Docker base-image scanning is not yet available because `OPS-001`/`OPS-003`
  are pending.
