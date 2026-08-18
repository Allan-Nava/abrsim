# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.1] — 2026-08-19

Nothing the simulator does changed. What changed is how this repository releases
itself, what the agents working in it are told, and what the next feature
milestone is for.

### Added

- **Every commit is a tagged release**, written down in `AGENTS.md` (canonical)
  and `CLAUDE.md`: a commit lands with its own `CHANGELOG.md` section and an
  annotated `vX.Y.Z` tag on it — `minor` for anything substantive, `patch` for a
  fix or a docs pass — and the push stays the maintainer's. The convention, and
  the exemption for commits that touch only local agent state, is the one
  `devops_hiway` has run on since 2026-07-03; this repository now states it
  rather than leaving it to be inferred from a single tag. A **release ritual**
  section lists the gates in the order they have to pass, and records why a
  docs-only release takes a patch: the minors are spoken for by the milestones
  (`v0.2.0` is M2's, `v0.5.0` is M6's), so a docs pass must not eat one.
- **M6 — The audience, not the session** (AB-36…AB-41), the next feature
  milestone, targeting `v0.5.0`. One run over one trace is an anecdote, and a
  rung is deleted or defended by the tail of the audience rather than its median:
  a deterministic viewer population (AB-36), percentiles with the p95 reported
  before the mean is reported at all (AB-37), per-rung attribution of the
  playback each rung actually served (AB-38), the egress that ladder costs per
  viewer-hour (AB-39), the device ceiling that decides whether the top rung buys
  anyone anything (AB-40), and the literature's linear QoE score with its weights
  printed beside it (AB-41). Every one of them keeps the existing rules: no
  severity that cannot be defended on a healthy stream, no audience invented
  where none was stated, and the same fixed-seed hash the built-in traces use so
  `--viewers 500` is the same 500 viewers everywhere.
- **A graphify knowledge graph** of the Go sources — 291 nodes, 824 edges, 12
  named communities — with the query-first rules for agents in both `AGENTS.md`
  and `CLAUDE.md`. It is built by AST alone, which is what keeps it free and
  deterministic and also what keeps this repository's markdown out of it: the
  rules and the backlog get read from source, and the section says so rather than
  letting an agent trust a graph that never saw them. `graphify-out/` stays
  gitignored and regenerable, and `.codex/` joins `.claude/settings.json` there —
  hooks and permissions belong to a workstation, the rules belong in `AGENTS.md`.

## [0.1.0] — 2026-08-18

The deterministic core: enough to answer *what does this ladder cost a viewer on
this network*, reproducibly and without a player.

### Added

- **Network traces** (AB-1, AB-2). A trace is a piecewise-constant bandwidth
  function of time, read from CSV or taken from a library of six generated
  built-ins. The one non-trivial operation is integrating it — how long `n`
  bytes take starting at `t`, across any number of sample boundaries — and it is
  the first thing tested, because everything else is arithmetic on top of it. A
  transfer no finite time completes returns `(0, false)` rather than the time up
  to the outage: reporting a permanent stall as a completed request is the one
  error that would make every downstream check optimistic. The built-ins are
  generated from a fixed-seed integer hash rather than `math/rand`, whose
  stability across Go releases is not a promise worth resting reproducibility on.
- **HLS ladder reader** (AB-3, AB-4). Master and media playlists, attribute
  lists with quoted commas, `EXT-X-BYTERANGE`, `EXT-X-MAP`. Segment sizes come
  from `AVERAGE-BANDWIDTH × duration ÷ 8` — an estimate, and `Segment.Measured`
  carries that fact into every renderer — or from a `HEAD` per segment under
  `--sizes measured`. A `HEAD` the CDN refuses falls back to the estimate rather
  than failing the run: a limit of this tool is not a defect in the stream.
  I-frame variants are excluded, being trick-play rather than rungs.
- **The playback simulator** (AB-5). Buffer level, startup threshold, download
  time from the trace, stalls when the buffer empties mid-download, and a buffer
  cap that stops the player requesting and moves it along the trace. It emits a
  per-segment timeline, not just totals — a check that cannot point at the
  moment it went wrong is not worth printing. Time is `float64` seconds
  throughout rather than `time.Duration`: a nanosecond rounding on each of
  thousands of intervals accumulates into a drift the checks would then report
  as a defect.
- **Three adaptation algorithms** (AB-6, AB-7, AB-8): throughput with an EWMA,
  BBA-style buffer-based, and BOLA. Three deliberately — one algorithm's opinion
  is not a measurement, and where the throughput rule stalls and the buffer rule
  does not, the difference is the estimator rather than the network. BOLA uses
  the parametrisation dash.js ships, including the low-buffer throughput
  safeguard dash.js wraps around it: deriving the parameters ourselves from the
  paper's two requirements was correct arithmetic and a broken player, and BOLA
  without the safeguard death-spirals to 59 stalls in 60 segments on an ordinary
  3 Mbps cell. Where an external authority exists, use it.
- **Seven checks** (AB-9, AB-10). `ladder-gap` is the one the tool exists for:
  it names both ends of a hole, the seconds of playback spent stuck below it and
  the bitrate a rung there would have had. Its two exclusions matter more than
  its arithmetic — a player capped by the network is not evidence of a missing
  rung, and one already on the top rung has run out of ladder rather than fallen
  into a hole. Alongside it: `rebuffer`, `startup`, `switches`, `sizes`,
  `coverage` and `efficiency`.
- **Renderers and CLI** (AB-11, AB-12). Worst findings first, colour only on a
  TTY with `NO_COLOR` honoured, and a JSON document carrying the full
  per-request timeline. Exit 0 whenever the simulation ran — findings are
  output, not failure — with `--exit-on` for pipelines. Credentials go in
  `--header`, never a flag value that lands in shell history.
- **Reference-stream smoke test** (AB-33), asserting a per-stream baseline of
  the checks allowed to exceed OK plus the checks that must not fall silent.
  Three design errors were found by it on its first run, none by a unit test:
  audio-only variants sitting at the bottom of the ladder, an `efficiency` ratio
  built on declared bitrates that read 129% while looking fine, and a top-rung
  exclusion resting on a magic threshold.

### Known limitations

Each of these makes the model kinder to the stream than reality is, so every
figure is a floor on the trouble rather than a ceiling. They are backlog items,
not omissions: request latency (AB-14), audio sharing the connection (AB-15), a
live edge (AB-17), DASH input (AB-13), and the startup rule dash.js applies
before BOLA's own judgement takes over (AB-35). `efficiency` reports a number and makes
no judgement (AB-34) — both attempts at a severity fired on healthy reference
streams, and a measurement with an honest "no opinion" is worth more than a
severity that cries wolf.

[Unreleased]: https://github.com/Allan-Nava/abrsim/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/Allan-Nava/abrsim/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Allan-Nava/abrsim/releases/tag/v0.1.0
