# Changelog

All notable changes to `originchain-go` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.1] - 2026-07-21

### Fixed

- Free-tier endpoints: `NewClient` now re-encodes a UUID hostname label
  (e.g. `<uuid>.free.originchain.ai`) into the engine's ULID tenant-id form,
  matching the path every `/v1/tenants/:tenant/...` request expects. Before
  this, free-tier clients that relied on hostname auto-derivation sent the raw
  UUID and got a 400 "invalid tenant id". Dedicated endpoints (whose label is
  already the tenant id) are unaffected. Set `Config.Tenant` explicitly to
  override in either case.

### Added

- `originchain-go/engine compatibility` table in the README corrected to the
  current SDK version.

## [0.4.0] - 2026-07-15

### Added

- `Client.Usage()` for `GET /v1/tenants/:t/usage`: live counters, the
  per-schema breakdown, and the tenant's neutral compute configuration.
- New types: `TenantUsage`, `SchemaUsage`, `TenantConfiguration`.

### Fixed

- `Version` constant now matches the released tag (it had stayed at
  `0.1.0` through the 0.3.0 release).
- README quickstart import path corrected to
  `github.com/originchain-ai/originchain-go` (was missing the `-ai`
  and did not compile).

### Notes

- Release lineage restored: v0.3.0 was tagged on a commit chain that a
  later history rewrite left unreachable from `main`. From v0.4.0 on,
  every release tag points at a merge commit on `main`.

## [0.3.0] - 2026-05-22

### Added

- Auto-generated `Idempotency-Key` header on every mutating call
  (POST / PUT / PATCH / DELETE). Network retries of the same logical
  call are now safe by default — the engine's idempotency cache
  (LRU-bounded, 24 h TTL) deduplicates them. GET requests still send
  no header, so reads do not consume cache slots.
- `newIdempotencyKey()` produces canonical UUIDv4 strings using
  `crypto/rand` — zero new third-party dependencies, `go.sum` stays
  empty.

### Notes

- Versioned 0.3.0 to keep parity with the Python and TypeScript SDKs
  released on the same date. Behaviorally a single-feature bump over
  0.1.0; SemVer-pure consumers would see this as 0.2.0.

## [0.1.0] - 2026-05-02

### Added

- Initial release of the official Go SDK for OriginChain.
- `Client` with constructor `NewClient(Config)` and `Config{BaseURL, Bearer, Tenant, HTTP}`.
- SQL surface: `SQL`, `SQLOne`.
- Vector surface: `VectorPut`, `VectorTopK`, plus `VectorMode` constants
  `ModeFast` and `ModeHighRecall`.
- Full-text surface: `FTSIndex`, `FTSSearch` (boolean / bm25 / phrase modes).
- Graph surface (`Client.Graph()` namespace): `Neighbors`,
  `ReverseNeighbors`, `BFS`, `Path`, `Dijkstra`.
- Natural-language ask: `Ask`.
- Typed errors: `APIError` for any non-2xx response and
  `AddonRequiredError` for HTTP 402 with the canonical add-on envelope.
  Both implement `errors.As` / `errors.Is`.
- Engine-compatibility constants `EngineMin = "1.0.0"`, `EngineMax = "1.x"`.
- Runnable godoc examples covering each public method plus an
  `Example_multiShape` that exercises SQL → Vector → Ask in sequence.
