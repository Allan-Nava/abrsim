# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.3] — 2026-08-19

`brew install --cask allan-nava/tap/abrsim`. Which is to say: AB-31, and the gate
that stops that sentence from being a lie.

### Added

- **The release pipeline** (AB-31): `.goreleaser.yaml` builds six archives
  (linux, darwin, windows × amd64, arm64), a `checksums.txt`, and a Homebrew
  **cask** into `Allan-Nava/homebrew-tap` — a cask rather than a formula, because
  a formula describes something built from source and `brews` is deprecated
  anyway. `-trimpath` and `-X main.version` are not cosmetic: a release binary
  that reports itself as `dev` is a bug report nobody can trace to a commit.
  Verified end to end locally with `goreleaser release --snapshot`: six archives,
  the cask with its postflight, and `abrsim version` printing the stamped version
  rather than `dev`.
- **`.github/workflows/release.yml`**, whose *first* job is the reference-stream
  smoke test. "Real streams before the tag" was this repository's rule and a
  manual step, which is the kind that gets skipped on the day it would have
  mattered — three design errors were found there and none by a unit test. A tag
  no longer ships without it. `goreleaser check` moved into `ci.yml` on every
  commit for the same reason in reverse: a deprecated stanza found on the first
  pushed tag is found with the tag already public.
- **Install, three ways**, in the README and on the site: the cask on macOS,
  `go install` everywhere, prebuilt archives for anyone who wants neither. The
  cask is macOS-only on purpose and the docs say so — `go install` and the
  archives already cover Linux and Windows, and a second packaging path exists to
  become the one that goes stale.
- **`docs.sh` now gates install claims too** (AB-42). An install command is a
  claim about something that ships, so `brew install` is checked against the cask
  in `.goreleaser.yaml` *and* against the exact `owner/tap/name` coordinate
  goreleaser will publish, `go install` against the module path in `go.mod`, and
  `docker run` against an image the release actually builds — there is none, so
  that claim is currently unmakeable, which is the point. A command that fails on
  the reader's machine is worse than an undocumented one: they blame the tool.
  Verified by breaking each one in a copy: no cask in the config, the wrong tap
  coordinate, a foreign module path and a phantom image all fail the gate.
- **Two follow-ups, in the backlog rather than in this release**: AB-43 (cosign
  signature over the checksums and a syft SBOM, so "zero dependencies" is
  verifiable rather than asserted) and AB-44 (a Developer ID signature and
  notarisation, which needs a paid Apple account). Each addition is another way
  for a first release to fail half-published, and the archives are worth more than
  the signature until somebody is downloading them.

### Fixed

- The reverse flag check in `docs.sh` read `--cask` out of `brew install --cask`
  and reported it as an abrsim flag the CLI had lost. Lines that invoke somebody
  else's program are skipped now. Second time in two releases that a `--word` on
  the page was not ours — the first was a CSS custom property — and both are traps
  in `AGENTS.md`.

### Known limitations

The cask installs a binary Gatekeeper does not trust on its own: Homebrew stages
it with `com.apple.quarantine`, the Go linker's arm64 signature is ad-hoc, and the
first run would die on SIGKILL with **no output at all** — which reads as a broken
build rather than an unsigned one. The cask's postflight strips the attribute, so
Homebrew installs work; anyone who downloads the archive directly on macOS still
has to clear it themselves until AB-44.

Nothing is published until the tag is pushed, and the cask lands only if
`HOMEBREW_TAP_TOKEN` is set on this repository. Without it a release will publish
its archives and then fail at the cask step — the artifacts fine, the tap stale,
and `brew install` quietly installing the previous version.

## [0.1.2] — 2026-08-19

A page and a mark. No simulator code changed; what changed is that the tool now
has somewhere to send someone who has not installed it.

### Added

- **The Pages site** (AB-30) at `docs/index.html`, served from `docs/` by
  `.github/workflows/pages.yml` — <https://allan-nava.github.io/abrsim/>. One
  hand-written file: no build step, no framework, and nothing fetched at view
  time, which is the same promise the binary makes and now a gated one rather
  than a boast. It leads on the finding the tool exists for — an inline diagram
  of a ladder with a 4.2 Mbps hole in it, the line that carried 2.9 Mbps above
  it, and the player pinned to 360p underneath for 116 of 240 seconds — then the
  seven checks, the seven traces, the three algorithms, the flags, and the list
  of what the model does *not* do, because a floor on the trouble is only honest
  if the reader is told it is a floor.
- **A logo** (`docs/assets/logo.svg`, with `favicon.svg` as its 32-pixel
  reduction): a ladder of four rungs with the third one drawn as a dashed
  absence, under an amber line for what the network actually carried. The missing
  rung *is* the product, so it is in the mark rather than implied by it. The tile
  is dark in both files rather than transparent, so the mark reads the same on a
  light README and a dark one.
- **`scripts/docs.sh`** — the gate that keeps AB-30's "both moving in the same
  commit" from being a good intention. It reads the check, trace, algorithm and
  flag names out of the Go sources and fails when `docs/index.html` or
  `README.md` does not name them; fails the other way too when the page
  documents a flag the CLI no longer has; and fails if the page grows an
  off-origin `<script>`, a CDN stylesheet or a reference to an asset that is not
  there. Wired into `ci.yml` and into the Pages deploy, so a docs-only push
  cannot publish a page that names a check that no longer exists. POSIX sh and
  awk, like `backlog.sh`.

### Fixed

- The 0.1.0 entry called the built-in library **six** traces. There are seven —
  `flat-5m`, `steps-down`, `steps-up`, `mobile-4g`, `train`, `office-wifi`,
  `dsl-evening` — as `abrsim traces` has said all along. The README's trace table
  put `steps-down` and `steps-up` in one row and the prose counted rows.

### Changed

- `AGENTS.md` and `CLAUDE.md`: the alignment rule now names the Pages row
  alongside the README row and the `--help` text, the release ritual runs
  `scripts/docs.sh check`, and the traps gained the one this script's first run
  produced — a CSS custom property is spelled `--accent`, so reading `--word`
  tokens out of an HTML file reports sixteen flags the CLI never had unless the
  `<style>` block is skipped. Watching a gate fail before trusting it is the
  rule, and it earned its place again.

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

[Unreleased]: https://github.com/Allan-Nava/abrsim/compare/v0.1.3...HEAD
[0.1.3]: https://github.com/Allan-Nava/abrsim/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/Allan-Nava/abrsim/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/Allan-Nava/abrsim/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Allan-Nava/abrsim/releases/tag/v0.1.0
