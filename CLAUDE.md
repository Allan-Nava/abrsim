# CLAUDE.md — abrsim

The operating brief for this repository lives in [AGENTS.md](AGENTS.md), which
is canonical. Read it before touching anything here.

The one-sentence version: **abrsim takes a real HLS ladder and a network trace,
simulates what an ABR player would do, and reports what it cost the viewer** —
deterministically, offline, zero dependencies. Every feature earns its place
against that sentence.

Five rules that are violated most often and cost most when they are:

- **Test first.** The failing test lands before the implementation, and it is
  run and seen failing *for the right reason*.
- **Determinism is the product.** No clock, no `math/rand`, no map iteration
  reaching the output from any simulation path.
- **Never invent a measurement.** `(value, false)` means "I could not measure
  this"; a simulation reported as a measurement is the one way this tool can lie.
- **Real streams before the tag.** Build the binary and run the smoke test —
  three design errors were found there on its first run and none by a unit test.
- **Every commit is a tagged release.** No commit lands without its
  `CHANGELOG.md` section and an annotated `vX.Y.Z` tag on it: `minor` for a
  feature, a check, a flag or a removal, `patch` for a fix or a docs pass. Do it
  without being asked. **Never `git push`** — the tags and the branch go out when
  the maintainer says so. No `Co-Authored-By` trailers.

The release ritual, in order: `scripts/backlog.sh lint && scripts/backlog.sh check`
· `scripts/docs.sh check` · `gofmt -l ./cmd ./internal` · `go vet ./...` · `go test -race ./...` · the smoke
test against real streams · tick the `AB-n` in `BACKLOG.md` and regenerate
`ROADMAP.md` · write the `CHANGELOG.md` section · commit · `git tag -a vX.Y.Z -m
"Release X.Y.Z"`. A commit whose `ROADMAP.md` is stale fails CI; a commit with no
tag breaks the one promise this repository makes about its own history.

When AGENTS.md and this file disagree, AGENTS.md wins and this file gets fixed.

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

The graph is built from the Go sources by AST alone (`graphify extract . --code-only`),
which is why it costs nothing and reruns deterministically — and why the markdown
in this repository is **not** in it. The rules, the traps and the backlog live in
AGENTS.md and BACKLOG.md: read those files, do not ask the graph about them.
`graphify-out/` is gitignored, regenerable, and never a reason to tag a release.
