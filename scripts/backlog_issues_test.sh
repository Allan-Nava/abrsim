#!/bin/sh
# backlog_issues_test.sh — tests for `backlog.sh issues`.
#
# The design under test separates deciding *what* to do from doing it: the planner
# reads BACKLOG.md plus a snapshot of the issues that already exist and prints a
# plan; only `--apply` talks to GitHub. So the interesting half — which items
# become issues, which get closed, which are left alone — is assertable without a
# network call and without creating anything on a public repository.
#
# Getting it wrong is not cosmetic. It either opens a duplicate issue for every
# item on every push, or closes issues for work that is still open. Both have
# happened to the sibling repository this planner came from, which is why the
# comments inside it read like an incident report.
#
# POSIX sh and awk only, like the rest of scripts/.

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
script="$root/scripts/backlog.sh"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/abrsim-issues-test.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT HUP TERM

failures=0
checks=0
fail() { failures=$((failures + 1)); echo "FAIL: $1" >&2; }

assert_line() {      # <name> <expected line> <file>
	checks=$((checks + 1))
	if grep -qxF -- "$2" "$3"; then
		echo "ok   $1"
	else
		fail "$1 — expected the line: $2"
		sed 's/^/       /' "$3" >&2
	fi
}

assert_absent() {    # <name> <needle> <file>
	checks=$((checks + 1))
	if grep -qF -- "$2" "$3"; then
		fail "$1 — did not expect: $2"
		sed 's/^/       /' "$3" >&2
	else
		echo "ok   $1"
	fi
}

assert_contains() {  # <name> <needle> <file>
	checks=$((checks + 1))
	if grep -qF -- "$2" "$3"; then
		echo "ok   $1"
	else
		fail "$1 — expected to find: $2"
		sed 's/^/       /' "$3" >&2
	fi
}

cat >"$tmp/BACKLOG.md" <<'EOF'
# Backlog — fixture

## M1 — The core <!-- ms: target=v0.1.0 phase=shipped -->

- [x] **AB-1 — Shipped with an issue**: it went out.
  <!-- ab: prio=high size=S labels=trace ver=0.1.0 -->
- [x] **AB-2 — Shipped without one**: nobody ever opened it.
  <!-- ab: prio=med size=S labels=sim ver=0.1.0 -->

## M2 — In flight <!-- ms: target=v0.2.0 phase=now -->

- [ ] **AB-3 — Open with an issue**: already tracked.
  <!-- ab: prio=high size=M labels=check -->
- [ ] **AB-4 — Open with none**: needs one.
  <!-- ab: prio=med size=L labels=cli,output -->
- [ ] **AB-5 — Open, issue was closed early**: somebody closed it by hand.
  <!-- ab: prio=low size=S labels=docs -->
- [ ] **AB-6 — Title drifted | with a pipe in it**: the title changed in the backlog.
  <!-- ab: prio=med size=M labels=sim -->
EOF

# The snapshot is `id <tab> number <tab> state <tab> title`, which is what
# existing_issues produces from `gh issue list`.
cat >"$tmp/snapshot.tsv" <<'EOF'
AB-1	101	closed	AB-1 — Shipped with an issue
AB-3	103	open	AB-3 — Open with an issue
AB-5	105	closed	AB-5 — Open, issue was closed early
AB-6	106	open	AB-6 — Something else entirely
EOF

# plan <snapshot> [extra args for `issues`]. The snapshot is shifted off before
# the rest is passed through: leaving it in $@ handed the planner a file path as a
# flag, and every assertion below failed against a usage message.
plan() {
	snapshot=$1
	shift
	BACKLOG_FILE="$tmp/BACKLOG.md" ROADMAP_FILE="$tmp/ROADMAP.md" BACKLOG_ISSUES_SNAPSHOT="$snapshot" \
		"$script" issues "$@" >"$tmp/plan.txt" 2>&1 || true
}

echo "== the plan =="
plan "$tmp/snapshot.tsv"
assert_line "an open item with no issue is created" "CREATE	AB-4	-" "$tmp/plan.txt"
assert_line "an open item that already has one is left alone" "OK	AB-3	103" "$tmp/plan.txt"
assert_line "an open item whose issue was closed is reopened" "REOPEN	AB-5	105" "$tmp/plan.txt"
assert_line "a done item whose issue is already closed is left alone" "OK	AB-1	101" "$tmp/plan.txt"
assert_line "a done item that never had an issue is skipped" "SKIP	AB-2	-" "$tmp/plan.txt"
assert_line "a drifted title is corrected" "RETITLE	AB-6	106" "$tmp/plan.txt"
assert_absent "and nothing is created twice" "CREATE	AB-3" "$tmp/plan.txt"
assert_contains "the plan counts what it will do" "1 to create" "$tmp/plan.txt"
assert_contains "and says it changed nothing" "dry run" "$tmp/plan.txt"

echo "== a settled repository is a no-op =="
cat >"$tmp/settled.tsv" <<'EOF'
AB-1	101	closed	AB-1 — Shipped with an issue
AB-3	103	open	AB-3 — Open with an issue
AB-4	104	open	AB-4 — Open with none
AB-5	105	open	AB-5 — Open, issue was closed early
AB-6	106	open	AB-6 — Title drifted | with a pipe in it
EOF
plan "$tmp/settled.tsv"
for verb in CREATE CLOSE REOPEN RETITLE; do
	assert_absent "nothing to $verb when the issues already match" "$verb	AB-" "$tmp/plan.txt"
done
assert_contains "the counts say so" "0 to create, 0 to close, 0 to reopen, 0 to retitle" "$tmp/plan.txt"

echo "== a shipped item with an open issue gets closed =="
cat >"$tmp/stale.tsv" <<'EOF'
AB-1	101	open	AB-1 — Shipped with an issue
EOF
plan "$tmp/stale.tsv"
assert_line "the issue for shipped work is closed" "CLOSE	AB-1	101" "$tmp/plan.txt"

echo "== a title with a pipe survives, because AB-6 has one =="
BACKLOG_FILE="$tmp/BACKLOG.md" ROADMAP_FILE="$tmp/ROADMAP.md" BACKLOG_ISSUES_SNAPSHOT="$tmp/snapshot.tsv" \
	"$script" issues --title AB-6 >"$tmp/title.txt" 2>&1 || true
assert_contains "the whole title is kept" "AB-6 — Title drifted | with a pipe in it" "$tmp/title.txt"

echo "== the body carries the item, not a placeholder =="
BACKLOG_FILE="$tmp/BACKLOG.md" ROADMAP_FILE="$tmp/ROADMAP.md" BACKLOG_ISSUES_SNAPSHOT="$tmp/snapshot.tsv" \
	"$script" issues --body AB-4 >"$tmp/body.txt" 2>&1 || true
assert_contains "the prose is there" "needs one" "$tmp/body.txt"
assert_contains "and it points back at the backlog" "BACKLOG.md" "$tmp/body.txt"
assert_contains "with the milestone it belongs to" "In flight" "$tmp/body.txt"

echo "== --milestones narrows it =="
plan "$tmp/snapshot.tsv" --milestones M1
assert_absent "an item outside the filter is not planned" "AB-4" "$tmp/plan.txt"
assert_line "and one inside it still is" "SKIP	AB-2	-" "$tmp/plan.txt"

echo
if [ "$failures" -gt 0 ]; then
	echo "backlog_issues_test.sh: $failures of $checks checks failed" >&2
	exit 1
fi
echo "backlog_issues_test.sh: $checks checks passed"
