# AGENTS.md — abrsim

`abrsim` (`github.com/Allan-Nava/abrsim`) takes a real HLS ladder and a network
trace, simulates what an ABR player would do, and reports what it cost the
viewer. One static Go binary, zero dependencies. `cmd/abrsim` is the CLI,
`internal/trace` is the network, `internal/manifest` reads the ladder,
`internal/abr` holds the adaptation algorithms, `internal/sim` is the playback
model, `internal/analyze` turns a run into findings, `internal/finding` is the
result model, `internal/output` renders.

This file is canonical. [CLAUDE.md](CLAUDE.md) holds the same rules for Claude
Code — when they disagree, this file wins and the other gets fixed.

## Working rules (ALWAYS)

- **Every feature earns its place against one sentence**: *what does this ladder
  cost a viewer on this network*. A check that only reads the manifest belongs
  in [checkfleet](https://github.com/Allan-Nava/checkfleet); one that reads
  segment bytes belongs in
  [segcheck](https://github.com/Allan-Nava/segcheck); one about how the ladder
  was encoded belongs in
  [ladder-bench](https://github.com/Allan-Nava/ladder-bench).
- **Zero dependencies.** `go.mod` has no `require` block, `go.sum` stays empty,
  CI enforces both. No player, no browser, no cgo, no shelling out — including
  in the tooling (`scripts/` is POSIX sh + awk for the same reason).
- **Determinism is the product.** Two runs of the same inputs must be
  byte-identical, on any machine and any Go release. Nothing in a simulation
  path may call a clock, `math/rand`, or iterate a map for anything that reaches
  the output. The built-in traces are generated from a fixed-seed integer hash
  precisely because `math/rand`'s stability is not a promise worth resting on.
- **Exit 0 whenever the simulation ran.** Findings are output, not failure. Only
  `--exit-on` produces a non-zero exit; a crash or a usage error exits non-zero.
- **Worst findings first**, in every renderer. The first line is the thing the
  operator must look at.
- **Never invent a measurement.** `(value, false)` is the protocol for "I could
  not measure this" — `trace.Download` returns it when no finite time completes
  a transfer, and callers must check the bool rather than use the zero value.
- **A simulation reported as a measurement is the one way this tool can lie.**
  Segment sizes derived from `AVERAGE-BANDWIDTH` are estimates and must travel
  as such: `Segment.Measured` follows every byte count into every renderer, and
  the summary line says so unprompted.
- **A limit of this tool is not a defect in the stream.** A trace that ran out,
  a manifest format not yet read, a `HEAD` a CDN refused — all get an honest
  OK-level or ERROR finding saying *abrsim* could not look, never a BAD that
  sends someone hunting a phantom.
- **No secrets on the command line.** Credentials go in `--header` values the
  caller reads from the environment.
- **Every idea goes in `BACKLOG.md`** with a stable `AB-n` id — never a scattered
  TODO comment. After editing it run `scripts/backlog.sh roadmap` and commit the
  regenerated `ROADMAP.md`, or CI fails. Commits and CHANGELOG entries reference
  the id.
- **Test first, always.** The failing test lands before the implementation. A
  test written afterwards asserts what the code does instead of what the
  simulation means.
- **Align everything**: a new or changed check lands in the same commit as its
  `README.md` row, its row on the Pages site (`docs/index.html`), its `--help`
  text, its tests, the `BACKLOG.md` tick and the `CHANGELOG.md` line.
  `scripts/docs.sh check` is the gate on the last of those three documents
  agreeing with the binary, and it runs in CI.
- **Every commit is a tagged release.** A commit lands together with its own
  `CHANGELOG.md` section (Keep a Changelog) and an annotated tag on it:
  `git tag -a vX.Y.Z -m "Release X.Y.Z"`. Bump `minor` for anything substantive
  — a new check, flag, algorithm, renderer, doc or a removal — and `patch` for a
  fix, a threshold correction or a docs pass. Do it without being asked; the
  `[Unreleased]` heading is for work not yet committed and nothing else.
  **Never `git push`** — branch and tags go out when the maintainer says so. No
  `Co-Authored-By` trailers. **Exempt** (no tag, no CHANGELOG entry): commits
  that touch only `.claude/settings.json` or a `graphify-out/` artefact, both of
  which are local tooling state rather than the product.

## Pattern for adding a check

1. **Backlog first**: an `AB-n` with a milestone, `prio`, `size`, `labels`.
   Regenerate `ROADMAP.md`.
2. **Red first**: build the ladder and the trace so the answer is known by
   construction — a one-rung ladder at 1 Mbps with four-second segments is
   exactly 500000 bytes a segment, and the whole timeline can be worked out on
   paper. Run the test, watch it fail *for the right reason*, then implement.
3. **The check emits a real `finding.Finding`**: `Target` names the exact rung
   or rung pair, `Message` names the defect, `Value`/`Unit` carry the
   measurement so machine consumers never parse prose, `Hint` says what it means
   for the viewer.
4. **Two tests, minimum**: one that plants the defect and asserts it is found
   *and correctly attributed*, and a clean-stream test asserting nothing above
   OK. A checker that cries wolf is worse than no checker.
5. **`go test -race ./...`** — the size measurement fans out across goroutines.
6. **Real streams before the tag.** Build the binary and run the smoke test.
   This is where the design gets corrected, not where it gets confirmed.

## The release ritual (every commit)

Run in this order; a step that fails stops the commit rather than getting a
follow-up commit.

1. `scripts/backlog.sh lint && scripts/backlog.sh check` — the `AB-n` ticked and
   `ROADMAP.md` regenerated from `BACKLOG.md`. A stale roadmap fails CI.
   `scripts/docs.sh check` — the site and the README name what the binary has.
2. `gofmt -l ./cmd ./internal` (empty), `go vet ./...`, `go test -race ./...`.
3. **Real streams**: build the binary and run the smoke test
   (`go test -tags smoke ./internal/analyze`). This is where the design gets
   corrected, not where it gets confirmed.
4. The `CHANGELOG.md` section for the version about to be tagged: dated, Keep a
   Changelog headings, every entry naming its `AB-n`, and the `Unreleased`/
   version compare links at the foot of the file updated.
5. Commit — a subject in the imperative naming the `AB-n` — then
   `git tag -a vX.Y.Z -m "Release X.Y.Z"` **on that commit**.
6. Stop. The push is the maintainer's (`git push origin main --follow-tags`) —
   and the push is what *publishes*: `.github/workflows/release.yml` fires on the
   tag, runs the smoke test against the reference streams again, then goreleaser
   builds the six archives, the checksums and the Homebrew cask. A tag that never
   leaves the workstation ships nothing, which is the intended default.

Version numbers are also milestone targets (M2 aims at `v0.2.0`, M3 at `v0.3.0`,
M6 at `v0.5.0`): a docs-only or tooling-only release takes the next **patch** so
it cannot consume a minor a milestone has its name on.

## Known traps / technical rules

- **Time is `float64` seconds everywhere, never `time.Duration`.** The simulator
  adds thousands of intervals and a nanosecond rounding on each accumulates into
  a drift the checks then report as a defect. It is also the unit every
  published ABR simulation uses, which is what makes our numbers comparable with
  theirs.
- **A trace holds its edge rates.** Before the first sample the first rate
  applies and after the last the last one does — a recording is not a promise
  about the times it did not cover, and holding is the only extrapolation that
  invents no event. But a session that outlives its trace has that much of its
  network assumed, and `coverage` says so above 25%.
- **The buffer cap is where the player stops *requesting*, not a ceiling on the
  level.** You cannot fetch a four-second segment without overshooting a
  threshold by up to four seconds, so the invariant is `cap + segment duration`.
  Asserting a hard ceiling asserts a player nobody ships.
- **The wait for the first frame is startup, never rebuffering.** Counting it as
  both double-reports the number an operator is most likely to act on. The
  *second* segment draining the buffer dry is a rebuffer and has to stay one.
- **BOLA's parameters are dash.js's, not a derivation of our own.** The first
  version here solved the paper's two requirements — an empty buffer picks the
  lowest rung, a full one the highest — directly against the ladder. Correct
  arithmetic, broken player: with only two rungs those requirements pin the
  crossover at exactly the buffer cap, and finding it subtracts two numbers near
  1.7e6 whose difference is near 6e-4, so the answer carried six significant
  figures and the player never left the bottom rung. A two-rung ladder is not
  exotic here — it is what a stream with a hole in it looks like. Where an
  external authority exists, use it.
- **BOLA on its own is unusable and dash.js does not ship it that way.** Buffer
  level alone sends the player past its crossover onto a rung it cannot afford;
  the buffer empties, it drops, it climbs again — 59 stalls in 60 segments on an
  ordinary 3 Mbps cell. Below its minimum buffer it must defer to the throughput
  estimate. With no estimate yet there is nothing to defer to, and treating that
  as "no bandwidth" would pin the start of every session to the bottom rung.
- **An audio-only variant is not a rung.** A player does not adapt between a
  picture and no picture. Apple's own reference stream ships several alongside
  the video ladder, and letting one in puts a 41 kbps step at the bottom that
  every startup and ladder-gap figure is then measured from. A variant with no
  recognisable `CODECS` is kept: unknown is not audio, and dropping a rung on
  suspicion silently edits somebody's ladder.
- **`BANDWIDTH` is a declared upper bound, not a measurement.** Comparing it
  against real bandwidth produced an `efficiency` reading of 129% on Apple's
  advanced example, at OK severity, where it read as fine. Anything divided by
  available bandwidth uses `Result.DeliveredBitrate` — the bytes that actually
  crossed the wire. `MeanRungBitrate` is a label for the summary line and
  nothing else.
- **The ramp-up is always below the top rung and always looks wasteful.** Any
  buffer-based algorithm spends its first several segments climbing, so a
  severity computed over "playback below the top rung" fires on healthy streams.
  This is why `efficiency` reports a number and makes no judgement (AB-34): a
  measurement with an honest "no opinion" beats a severity that cries wolf.
- **`ladder-gap` has two exclusions and they matter more than the arithmetic.**
  A player capped by the network is not evidence of a missing rung, and one
  already on the top rung has run out of ladder rather than fallen into a hole.
  Reporting either sends somebody to encode a rung nothing would ever choose.
- **A quoted comma in an attribute list is the classic HLS trap.** Splitting
  `CODECS="avc1.640028,mp4a.40.2"` on commas loses the audio codec and produces
  a key of `mp4a.40.2"` that a lenient parser then ignores in silence.
- **The `go` directive is the version CI builds and ships, not a floor.**
  `setup-go` with `go-version-file: go.mod` installs *exactly* what the directive
  names, so `go 1.25.0` produced binaries whose standard library carried every
  advisory published since 1.25.0 — five of them, in `crypto/tls`, `net/url`,
  `encoding/asn1` and `net/textproto`, all reachable from `manifest.get`, all
  fixed by 1.25.13. `govulncheck` had therefore been red since the repository's
  first commit, and the fix was one line. When that job goes red, bump the patch
  in `go.mod` and check it locally with the matching toolchain
  (`go install golang.org/dl/go1.X.Y@latest && go1.X.Y download`, then run
  `govulncheck` with it on `PATH`) rather than pushing and hoping. It is never a
  false positive for a tool that takes credentials in `--header` and speaks TLS.
- **Homebrew stages the binary with `com.apple.quarantine`, and ours is only
  ad-hoc signed** — that is the Go linker on arm64, not a Developer ID signature.
  Gatekeeper then kills the first run with SIGKILL, exit 137, **and no output at
  all**: not the "developer cannot be verified" dialog a GUI app gets, nothing. In
  a terminal that reads as a broken build, so whoever hits it reports the wrong
  bug. The cask's postflight strips the attribute; the reasons it is
  `/usr/bin/xattr` by absolute path, `-dr` rather than `-d`, and
  `.exit_status.zero?` rather than `== 0` are each a scar and are written next to
  the code in `.goreleaser.yaml`. AB-44 is the real fix and needs a paid Apple
  account.
- **`HOMEBREW_TAP_TOKEN` expires, and the failure is quiet where it matters.**
  Fine-grained PATs must expire. When it does, a release publishes its archives
  and *then* fails at the cask step: the artifacts are fine, the tap is stale, and
  `brew install` keeps installing the old version without saying so. A release
  that went red after the archives uploaded is this, not a broken build.
- **`goreleaser check` belongs in CI, not in the release.** Deprecated stanzas
  fail it, and without the CI step the first pushed tag is where that is
  discovered — with the tag already public and the release half-done. It runs on
  every commit, where the fix costs nothing.
- **Not every `--flag` in the docs is ours.** `docs.sh` reads `--word` tokens out
  of the page to catch a flag documented after the CLI dropped it, and the install
  block introduced `brew install --cask`: the check promptly reported `--cask` as
  an abrsim flag that had gone missing. Lines invoking somebody else's program are
  skipped now. Same lesson as the stylesheet below, twice in two releases.
- **A `--name` in a stylesheet is not a flag.** `docs.sh` reads `--word` tokens
  out of the page to catch a flag documented after the CLI dropped it, and CSS
  custom properties are spelled the same way: the first run reported sixteen
  missing flags, all of them colours. The `<style>` block is skipped for that
  reason. The failure was the extractor's, which is the point of watching a gate
  fail before trusting it.
- **A round trip against our own builders cannot catch a shared misreading.**
  Where an external authority exists, use it: the BOLA paper for the algorithm,
  a real reference stream for everything else. Three of this project's design
  errors were found by the smoke test on its first run and none by a unit test.

## Backlog and roadmap

`BACKLOG.md` is the source of truth; `ROADMAP.md` is **generated** and must never
be hand-edited. Items carry an invisible metadata comment:

```
- [ ] **AB-13 — DASH input**: what it is and why it earns its place.
  <!-- ab: prio=high size=L labels=manifest -->
```

`scripts/backlog.sh`:

| Command | What it does |
|---|---|
| `lint` | ids unique and gap-free, metadata valid, done items carry `ver=` |
| `roadmap` | regenerate `ROADMAP.md` |
| `check` | fail if `ROADMAP.md` is stale (the CI gate) |
| `stats` | one-line summary |
| `next [n]` | the n highest-priority open items |

`scripts/docs.sh`:

| Command | What it does |
|---|---|
| `check` | the checks, traces, algorithms and flags in the source are named in `docs/index.html` and `README.md`; no flag is documented that the CLI has lost; every install command is backed by what ships it (cask, module path, image); the page stays self-contained and its assets exist (the CI gate) |
| `names` | print what the source says exists |

Milestones: **M1** the deterministic core (v0.1.0) · **M2** faithfulness
(v0.2.0) · **M3** comparison and CI (v0.3.0) · **M4** integration (v0.4.0) ·
**M5** project and release (ongoing). Ids are stable forever: retire an item by
marking it done, never by deleting it and reusing the number.

## Pointers

- `.goreleaser.yaml` + `.github/workflows/release.yml` — the release: six
  archives, checksums and a Homebrew **cask** (a formula describes something built
  from source; this is a prebuilt binary), published on a pushed `v*` tag after the
  smoke test passes. Two things live outside the repository and cannot be fixed
  from inside it: the `HOMEBREW_TAP_TOKEN` secret (fine-grained PAT on
  `Allan-Nava/homebrew-tap`, Contents: read and write) and the Pages source
  setting. The cask is macOS-only on purpose — `go install` and the archives cover
  everything else, and a second packaging path exists to become the stale one.
- `docs/index.html` — the Pages site: one hand-written file, no build step and
  nothing fetched at view time, served from `docs/` by
  `.github/workflows/pages.yml` (the repository's Pages source has to be set to
  *GitHub Actions*). Live at <https://allan-nava.github.io/abrsim/>.
- `docs/assets/logo.svg` — the mark: a ladder of four rungs with the third one
  missing, under the line the network carried. It carries its own dark tile
  rather than inheriting the page's, so it reads on a light README and a dark
  one; `favicon.svg` is the same mark with two rungs, which is all that survives
  at 32 pixels.
- `internal/sim/sim.go` — the playback model, with what it does *not* model
  named in the package comment rather than hidden
- `internal/analyze/analyze.go` — every check, and every threshold in one named
  struct because each is a judgement someone is entitled to disagree with
- `internal/trace/builtin.go` — the generated traces; nothing here may call a
  clock or `math/rand`
- `internal/analyze/smoke_test.go` — the reference-stream baseline (build tag
  `smoke`)
- Related: [segcheck](https://github.com/Allan-Nava/segcheck) ·
  [checkfleet](https://github.com/Allan-Nava/checkfleet) ·
  [ladder-bench](https://github.com/Allan-Nava/ladder-bench) ·
  [crowdsim](https://github.com/HiWay-Media/crowdsim)
- License: PolyForm Noncommercial 1.0.0

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, use the installed graphify skill or instructions before doing anything else.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

The graph is built from the Go sources by AST alone (`graphify extract . --code-only`),
which is why it costs nothing and reruns deterministically — and why the markdown
in this repository is **not** in it. The rules, the traps and the backlog live in
AGENTS.md and BACKLOG.md: read those files, do not ask the graph about them.
`graphify-out/` is gitignored, regenerable, and never a reason to tag a release.
