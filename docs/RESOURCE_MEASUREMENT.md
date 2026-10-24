# Local Synthetic Resource Measurement

**Date:** 2026-10-25
**Tasks:** `OPS-006`, supporting `OPS-007`/`OPS-008`
**Environment:** macOS arm64 development machine, Go `1.27.3`, real `neda`
binary, temporary SQLite database and encryption key, deterministic fake
provider. **Not** the target Ubuntu server; no live MrBilit, Telegram, Push,
production data, or server access.

## Method

- Seeded 40 monitors, one active fake-provider target each (40
  monitor-provider pairs), departure date 2027-06-01, all due at the first poll.
- Poll interval: 300 seconds (default); scheduler/provider spacing: existing
  production behavior; 2 workers; no live provider calls.
- Started the real binary with `NEDA_FAKE_PROVIDER_ENABLED=true`, a fresh SQLite
  database, a fresh 32-byte key, and `NEDA_VAPID_SUBJECT`.
- Sampled once per minute for 60 minutes:
  - `ps -o rss= -p <pid>` (KiB)
  - `ps -o %cpu= -p <pid>`
  - `stat -f%z <db>` and `<db>-wal`
- Counted `check_runs` at the end; stopped the process with `SIGTERM`.

Representative command shape:

```sh
NEDA_ENV=development NEDA_HTTP_ADDR=127.0.0.1:<port> \
NEDA_DB_PATH=<tmp>/data/neda.db \
NEDA_ENCRYPTION_KEY_FILE=<tmp>/secrets/enc.key \
NEDA_VAPID_SUBJECT=mailto:resource@example.com \
NEDA_FAKE_PROVIDER_ENABLED=true NEDA_LOG_LEVEL=warn NEDA_LOG_FORMAT=json \
<tmp>/neda serve
```

The database was seeded directly with 40 `monitors` and 40
`monitor_providers` rows using valid fake route IDs; no production data was
used.

## Results (60 samples, 60 minutes)

| Metric | Min | Max | Average |
|---|---:|---:|---:|
| RSS | 18,144 KiB (17.7 MiB) | 24,096 KiB (23.5 MiB) | 23,188 KiB (22.6 MiB) |
| CPU | 0.0% | 0.9% | 0.12% of one core |
| SQLite main DB | 229,376 B (224 KiB) | 356,352 B (348 KiB) | 306,859 B (300 KiB) |
| SQLite WAL | 107,152 B (105 KiB) | 4,128,272 B (3.94 MiB) | 3,949,876 B (3.77 MiB) |

`check_runs` completed during the run: **482** (40 targets, roughly 12 cycles).
No application errors were logged (warning-level log file empty).

## Pass/fail against the design targets

| Target | Requirement | Result |
|---|---|---|
| Idle RSS | ≤ 64 MiB | **PASS** — observed active minimum 17.7 MiB, average 22.6 MiB |
| Active RSS | ≤ 128 MiB | **PASS** — observed maximum 23.5 MiB |
| CPU | < 5% of one core average at 20 monitor-provider pairs / 5 min | **PASS** — 0.12% average, 0.9% maximum with 40 pairs (2× the reference) |
| DB size | < 200 MiB at 20×2 pairs, 14-day checks + 180-day events | **PASS for the measured hour** (300 KiB average); 14-day growth is not proven by this short run |
| WAL | below the 64 MiB configured cap | **PASS** — 3.94 MiB maximum |
| Image | < 60 MiB compressed | **PASS** — Docker CI run 40133170885: 7,250,978 bytes compressed (16,331,188 uncompressed) |
| Logs | < 50 MiB retained | **PASS by configuration** — Compose json-file 10 MiB × 3 = 30 MiB; proxy logs are separate |
| Network | bounded by checks and caps | Not measured externally; fake provider performs no network I/O |

## Interpretation and limitations

- These numbers are a **local synthetic** measurement, not a promise for the
  target server. The target may have different CPU, memory, disk, filesystem,
  and SQLite cache behavior.
- Only one hour of check history was generated. The 14-day/180-day database
  growth target remains an extrapolation; measure it on the target host during
  the approved deployment/soak window.
- The fake provider does not exercise real MrBilit latency, response sizes,
  retries, or rate limits.
- No Telegram/Push deliveries were configured, so notification network/CPU cost
  is not included.
- The measured RSS is the whole process (Go runtime, SQLite, scheduler,
  retention worker, delivery worker, HTTP server, embedded SPA).
- The sample interval is one minute; brief bursts between samples may be missed.
  CPU is the `ps` process utilization at each sample, not a profiler trace.
- No production server was accessed and no claim is made that the target host
  meets the same numbers.
