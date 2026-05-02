# Changelog

All notable changes to `originchain-go` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the
project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
