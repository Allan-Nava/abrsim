# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.0] — 2026-08-19

**The p95, before anything else** (AB-37) — the second item of M6. A population
that reports a median is a population telling you about the viewer who was fine.

### Added

- **Every check now carries its severity at p50, p95 and p99**, worst percentile
  first, and the checks are ordered by **what happens at the p95** rather than by
  what happened to somebody somewhere. "At the 95th percentile of your audience
  this is BAD" is the sentence a ladder decision is made from; `rebuffer p99 BAD ·
  p95 BAD · p50 WARN` on one line is the median hiding the tail, made visible.
- **Each loud check says the percentile it starts firing from.** Quiet for 44% of
  the audience means it fires from p44 up — one number that places the defect in
  the distribution instead of leaving "56% of viewers" to be converted by the
  reader.
- **The distribution table gained p95 and p99 columns**, and the summary line
  quotes the p95 of the two figures an operator acts on first: seconds to the first
  frame, and seconds frozen. No mean appears anywhere in a population report. A
  mean startup of 2.1s hides the viewer who waited nine seconds and left, and that
  viewer is the entire reason the report exists.

### Changed

- **Percentiles are nearest-rank order statistics, and the median moved with
  them.** `[1,2,3,4]` now reports a p50 of 2, not the interpolated 2.5: every
  figure in the table is a value some real viewer actually had. An interpolated
  percentile is a number nobody in the audience experienced, which makes it a
  measurement this tool invented — the one thing it must not do. The `median` field
  in the population JSON is now `p50`, alongside `p95`, `p99` and `viewers`.
- **The numeric table stays ascending** (`min p50 p95 p99 max`) even though AB-37
  asked for the p99 line before the p50 one. A table of numbers read out of order
  is a table people misread; "worst first" is honoured where a reader looks first —
  the check lines, which lead with the p95, and the ordering of those lines.

### Known limitations

- **A percentile the audience cannot support is not reported at all.** A "p95" over
  ten viewers is the maximum wearing a better name, so a p95 needs 20 viewers and a
  p99 needs 100. Below that the column is an em dash, the report says what it would
  have needed, and the JSON carries an explicit `null` — the same `(value, false)`
  protocol the rest of the codebase uses for "I could not measure this". A limit of
  the audience is never dressed up as a limit of the stream.
- Still no weighting: every viewer counts once, so a p95 is the 95th percentile of
  *the simulated audience*, not of a real one. What that audience should look like —
  device mix, screen ceiling — is AB-40.

## [0.2.2] — 2026-08-19

The backlog and the release stopped being a paragraph in `AGENTS.md` that somebody
follows by hand (AB-52). This entry, its section heading, both of its compare links
and the `AB-52` tick above it were produced by the scripts it is about.

### Added

- **`backlog.sh` grew the half that writes**: `add` (next free id, metadata,
  wrapped prose, inserted inside the right milestone), `done` (tick and stamp the
  release, optionally appending what shipped), `milestone` and `retarget`. Doing
  this by hand meant picking the next id by eye, writing the `<!-- ab: … -->`
  comment from memory and remembering to regenerate `ROADMAP.md` — three ways to
  break a file whose ids are promised to be stable forever, and none of them
  something a reviewer would catch.
- **Every write is transactional**: the candidate is built in a temporary file and
  **linted there**; only a candidate that lints replaces `BACKLOG.md`, and then
  `ROADMAP.md` is regenerated in the same breath. A refused write leaves both files
  byte-identical, which is the property the test suite spends most of its
  assertions on. A backlog half-edited by a command that then failed is worse than
  one nobody automated, because the next reader cannot tell which half was the
  intention.
- **`scripts/release.sh`**: `next` (patch/minor arithmetic off the latest tag),
  `changelog` (the top released version, and a refusal if its section is
  unfinished), `prepare` (the dated section plus **both** compare links — those
  were hand-edited six times before this existed), `check` (backlog gates, tooling
  tests, `docs.sh`, `goreleaser check`, `gofmt`, `vet`, `go test -race`, and a
  confirmation that the changelog is written, in one command) and `tag` (re-runs
  every gate, refuses an existing tag, commits with the message file it is given,
  tags that commit, and never pushes).
- **`scripts/backlog_test.sh` (48 checks) and `scripts/release_test.sh` (18)**,
  both POSIX sh, both in CI, both against fixtures — no git writes, no network, and
  no chance of overwriting this repository's own files. The CI step runs them under
  the runner's `/bin/sh`, which is dash rather than the bash-in-POSIX-mode a Mac
  develops against.

### Fixed

Three bugs in the new tooling, each found by its own tests before it touched
anything real:

- **`awk`'s `exit` inside a rule still runs `END`**, whose `exit 1` then wins — so
  the guard against re-ticking an already-shipped item never fired, and `done`
  reported success on an item it had not changed. A command that claims success
  while changing nothing is the worst of the three outcomes: the caller believes
  the file now says something it does not, so `done` also refuses when its edit
  produced a byte-identical file.
- **The first `add` ate every blank line in the section it touched**, because it
  buffered blanks and re-emitted them with an off-by-one `substr`. Insertion is by
  line number now.
- **The roadmap was generated from the parse taken *before* the edit**, so an
  `add` wrote a `ROADMAP.md` that was stale the instant it was written and CI would
  have been the one to notice.
- A test-harness trap worth writing down: in POSIX sh a variable assignment
  prefixed to a **function** call stays in effect after the function returns, so
  `LAST=v0.9.9 assert_eq …` leaked into every later test and three `prepare` cases
  were quietly asserting against the wrong version.

## [0.2.1] — 2026-08-19

### Added

- **M7 — A ladder you can defend** (AB-46…AB-51), targeting `v0.6.0`. Everything
  the tool does today *names* a defect; an encoding team then has to decide what
  to do about it, and that decision — add a rung, drop one, move two — is where
  the money is and where abrsim currently stops. `ladder-gap` says a rung is
  missing and AB-26 proposes one; neither answers *what should this ladder be*.
  So: candidate ladders generated deterministically from the one in hand (AB-46),
  each scored over the viewer population rather than a session (AB-47), and the
  result reported as a **Pareto front of viewer cost against delivery cost**
  (AB-48) — never as a single winner, because what a gigabyte is worth is a
  commercial question and this tool does not know the contract. Plus the smallest
  ladder that still meets the budgets (AB-49), every recommendation explained in
  the units of the complaint it removes (AB-50), and a hold-out check that says
  when a winner only wins on the traces it was searched against (AB-51) — the
  difference between a recommendation and a coincidence.
- The milestone deliberately **waits on M6 and M3** rather than inventing its own
  scoring: a search is exactly as trustworthy as the numbers it optimises, so it
  consumes AB-41's QoE, AB-39's egress and AB-23's budgets instead of growing a
  private definition of "better".

## [0.2.0] — 2026-08-19

**An audience instead of a viewer** (AB-36) — the first item of M6, and the first
release where abrsim can answer a question about a ladder that a single session
cannot: not *what did this cost a viewer*, but *how many of them, and how badly*.

### Added

- **`--viewers N`** simulates N people on variations of the same network and
  reports the spread. Viewer 0 is the trace exactly as measured, so `--viewers 1`
  is byte-for-byte the run this tool has always done — there is a CLI test whose
  only job is to keep that true. Sessions fan out across goroutines and land in a
  slice indexed by viewer, so nothing in the output depends on which finished
  first, and two runs of the same population are byte-identical.
- **`internal/trace.Population`**: the derivation. A per-viewer scale, a
  per-sample jitter, and one rule that is not negotiable — **a tunnel is a tunnel
  for everyone**: no scale applied to zero bandwidth may produce bandwidth, or the
  one trace no adaptation can save would quietly become survivable. Both draws
  come from `wobble`, the same fixed-seed integer hash the built-in traces are
  generated from, so `--viewers 500` is the same 500 viewers on every machine and
  every Go release.
- **`internal/population`**: the runner and the report. Per check, how much of the
  audience it went loud for — "29 of 50 viewers (58%)" is a sentence a ladder
  decision can be argued from, where a bare `BAD` is not — plus the worst case and
  the viewer it belongs to, so somebody can go and look at that exact session.
  Then min, median and max for startup, frozen seconds, stalls, switch rate and
  delivered bitrate.
- **`--exit-on` judges the whole audience**, not its median: a gate that read only
  the middle viewer would pass a ladder that freezes for one person in twenty.
- **What it found on a real stream, which is the point**: Apple's `bipbop`
  reference stream over `steps-down`, fifty viewers. Viewer 0 — the trace as
  measured, the run every previous version of this tool would have reported —
  **froze for nothing at all and every check came back OK**. Across the audience,
  29 of 50 viewers rebuffered, the median lost 1.7 seconds and the worst lost
  **69 seconds of 210** to a frozen picture. The anecdote was not wrong; it was
  one viewer.

### Fixed

- **Two defects found before the tag, both by running against a real stream
  rather than by a unit test**, which is now three releases in a row:
  1. The headline "worst" line named the *first* viewer to cross a threshold, so
     it read "2.4s frozen" while its own table said the worst viewer froze for 69
     seconds. Within a severity, the worst finding is now the largest
     measurement. A report that disagrees with itself teaches an operator to
     distrust both halves of it.
  2. A quiet check printed one viewer's sentence with nothing to say it was one
     viewer's, which reads as a statement about the whole audience — the exact
     mistake this feature exists to stop making. Those lines now say
     `viewer 0:`.
- **The population's scales are stratified, not drawn independently.** The obvious
  seeding was uniform over hundreds of viewers and clumped over the first thirty:
  a mean scale of 1.14, so `--viewers 30` was thirty people on a *better* line
  than the one measured, with no bottom tail — the entire reason to simulate an
  audience. The n−1 derived viewers now take evenly spaced quantiles of the scale
  range, permuted by the hash so that the first ten viewers of two hundred are not
  the ten worst lines. A test asserts the tails, never the mean.

### Changed

- **The milestones were re-targeted rather than re-numbered.** M6 shipped before
  M2 because that is what was asked for, so M6 aims at `v0.2.0`, M2 at `v0.3.0`,
  M3 at `v0.4.0` and M4 at `v0.5.0`. An `Mn` is an identity, not a position in a
  queue, and the backlog says so where a reader will see it.
- README and the Pages site gained a section on the audience, with the real
  fifty-viewer run in it; the site's list of limits now says what is still missing
  (percentiles) instead of claiming abrsim only models one viewer.

### Known limitations

- **Min, median and max — not percentiles.** A p95 over eight viewers is
  arithmetic pretending to be a measurement. Percentiles, and findings that carry
  the percentile they fired at, are AB-37.
- **A viewer's scale depends on how many viewers there are**: viewer 5 of 30 is
  not viewer 5 of 200. That is the price of stratifying, and it is stated in the
  package comment, the README and this entry rather than left to be discovered.
- **The population `--json` carries per-viewer summaries, not their request
  timelines.** Two hundred timelines is not a document anybody reads; run a single
  viewer for the full one.
- The variation is in the *bandwidth*, and only there. Two viewers still start at
  the same instant, fetch the same ladder and share a device: the screen is AB-40,
  and it matters, because a phone cannot see the difference between the 1080p rung
  and the 720p one.

## [0.1.5] — 2026-08-19

The Pages site, rebuilt: three times the page it was, and every picture on it
drawn from real data rather than from a description of the data.

### Added

- **A timeline figure of an actual run** — the buffer sawtoothing against the
  30-second cap, the bandwidth the cell carried underneath it, and the moment
  just before 81 seconds where the collapse to 400 kbps empties the buffer
  mid-download and freezes the picture for 0.7 seconds. Every coordinate comes
  out of the `--json` timeline of
  `--trace mobile-4g --abr throughput --play 300s` against Apple's public
  `bipbop` stream, and the command is in an HTML comment beside the figure so
  anyone can reproduce it.
- **A card per built-in trace**, each chart drawn from that trace's own samples
  as a **step function** — a trace is piecewise constant, and a smooth line would
  show interpolation the model never performs. Each is scaled to its own peak
  with a shared dashed line at 5 Mbps, so shapes are legible *and* comparable;
  the earlier shared-scale sparklines made everything except `office-wifi`
  unreadable noise.
- **The detail the page was missing**: the anatomy of a finding (check, target,
  value+unit, hint); a four-step diagram of what the tool actually does; the real
  severity thresholds for every check (2s and 4s for `startup`, 0.1% and 1% of
  playback for `rebuffer`, 4/min and 10/min for `switches`, ≥15% of playback and
  1.8×/2.5× for `ladder-gap`, ≥25% extrapolation for `coverage`) rather than a
  vague "turns loud eventually"; the `--json` document with its real field names;
  a CI-gate snippet; a CSV example with the `k`/`M` suffixes; the BOLA
  six-significant-figures story; and a seven-question FAQ.
- **Install as three tabs** — cask, `go install`, prebuilt archive — with no
  JavaScript: they are radio inputs, and the disclosures are `<details>`. The
  page still fetches nothing from anywhere.

### Fixed

- **`docs.sh` read `var(--bg)` as a CLI flag.** Skipping the `<style>` block was
  not enough once inline SVG started carrying custom properties in plain
  attributes; the extractor now strips `var(--…)` wherever it appears. Third time
  a `--word` on the page has not been ours — CSS variables, then `brew`'s
  `--cask`, now custom properties in attributes — and each one was caught by
  running the gate rather than by reading it.
- **The gate also caught the traces losing their names**: moving them from a table
  cell into a card heading dropped the `<code>` wrapper, so `docs.sh` correctly
  reported that seven traces were no longer *named* on the page.

### Known limitations

The figures are generated from real data by a script that is **not** in the
repository (AB-45), so a built-in trace that changes shape leaves a picture that
quietly disagrees with it. `docs.sh` checks names, not geometry. The commands are
recorded beside each figure until the generator lands.

## [0.1.4] — 2026-08-19

CI has been red since this repository's first commit, and the cause was one line.

### Fixed

- **`go.mod` now names a patched toolchain: `go 1.25.13`, was `go 1.25.0`.**
  `setup-go` with `go-version-file: go.mod` installs *exactly* the version the
  directive names, so every CI run — and every release binary — was built against
  the 1.25.0 standard library and carried the five advisories published since:
  `crypto/tls` (GO-2026-6090, GO-2026-5856), `net/url` (GO-2026-6218),
  `encoding/asn1` (GO-2026-5972) and `net/textproto` (GO-2026-5039), all reachable
  from `manifest.get` and all fixed by 1.25.13. `govulncheck` was reporting them
  correctly; the only failing job in CI was the one doing its job. Not a
  formality for a tool that speaks TLS and takes credentials in `--header`.
  Verified the way the traps file now says to: the 1.25.13 toolchain downloaded
  locally, `govulncheck` run against it — *No vulnerabilities found* — and the full
  `-race` suite green under it, rather than pushed and hoped for.
- **Every action bumped to its current major**: `checkout` v4→v7, `setup-go`
  v5→v7, `goreleaser-action` v6→v7, `configure-pages` v5→v6,
  `upload-pages-artifact` v3→v5, `deploy-pages` v4→v5. The runner was already
  forcing the Node 20 ones onto Node 24 and saying so on every run. Each bumped
  action's `action.yml` was read first to confirm the inputs we pass still exist
  (`fetch-depth`, `go-version-file`, `version`/`args`, `path`).
- **`docs/.nojekyll` was about to start disappearing from the Pages artifact**:
  `upload-pages-artifact` v4 and up exclude dotfiles, so the bump needed
  `include-hidden-files: true`. Deploying through Actions never runs Jekyll, so
  the file is inert today — it matters the day somebody switches Pages back to
  serving the branch, which is exactly when nobody remembers it was dropped.

### Known limitations

`v0.1.3` published its six archives and `checksums.txt` and then failed at the
cask step with `401 Bad credentials` — the documented failure mode, one release
earlier than expected: `HOMEBREW_TAP_TOKEN` is not set on this repository, so
goreleaser could not write to `Allan-Nava/homebrew-tap`. The archives are fine,
the tap has no cask yet, and `brew install --cask allan-nava/tap/abrsim` does not
work until the secret exists and a tag is pushed with it in place. Nothing in the
repository can fix that from the inside.

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

[Unreleased]: https://github.com/Allan-Nava/abrsim/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/Allan-Nava/abrsim/compare/v0.2.2...v0.3.0
[0.2.2]: https://github.com/Allan-Nava/abrsim/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/Allan-Nava/abrsim/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/Allan-Nava/abrsim/compare/v0.1.5...v0.2.0
[0.1.5]: https://github.com/Allan-Nava/abrsim/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/Allan-Nava/abrsim/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/Allan-Nava/abrsim/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/Allan-Nava/abrsim/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/Allan-Nava/abrsim/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Allan-Nava/abrsim/releases/tag/v0.1.0
