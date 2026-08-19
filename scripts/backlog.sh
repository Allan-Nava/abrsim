#!/bin/sh
# backlog.sh — lint BACKLOG.md and generate ROADMAP.md from it.
#
# BACKLOG.md is the single source of truth: every planned item lives there with a
# stable AB-n id and a trailing `<!-- ab: ... -->` metadata comment. ROADMAP.md is
# a generated view of the same data, grouped by milestone.
#
#   scripts/backlog.sh lint       validate BACKLOG.md (ids, metadata, milestones)
#   scripts/backlog.sh roadmap    regenerate ROADMAP.md
#   scripts/backlog.sh check      fail if ROADMAP.md is stale (CI gate)
#   scripts/backlog.sh stats      one-line summary
#   scripts/backlog.sh next [n]   the n highest-priority open items (default 5)
#   scripts/backlog.sh issues     plan the GitHub issue sync (see below)
#     --apply | --milestones M11,M12 | --body AB-n | --title AB-n
#
# And the half that writes. Every one of these lints the result before it replaces
# anything, regenerates ROADMAP.md on success, and leaves both files untouched when
# the edit would not have linted:
#
#   scripts/backlog.sh add <Mn> "<Name>" --prio p --size s --labels a,b [--body ...]
#   scripts/backlog.sh done <AB-n> --ver X.Y.Z [--note "what shipped"]
#   scripts/backlog.sh milestone <Mn> "<Title>" --target vX.Y.Z|ongoing --phase p [--intro ...]
#   scripts/backlog.sh retarget <Mn> [--target ...] [--phase ...]
#
# They exist because doing this by hand meant picking the next id by eye, writing
# the metadata comment from memory and remembering to regenerate the roadmap — three
# ways to break a file whose ids are promised to be stable forever, none of which a
# reviewer would notice.
#
# POSIX sh and awk only — this repository has no dependencies, and neither does
# its tooling.
#
# BACKLOG_FILE, ROADMAP_FILE and BACKLOG_ISSUES_SNAPSHOT override the two file
# paths and the source of "which issues exist already". They exist so the tooling
# can be tested against a fixture — without a network call, without creating
# anything on a public repository, and without overwriting this repository's own
# ROADMAP.md, which the first run of scripts/backlog_test.sh promptly did.

set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
backlog="${BACKLOG_FILE:-$root/BACKLOG.md}"
roadmap="${ROADMAP_FILE:-$root/ROADMAP.md}"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/abrsim-backlog.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT HUP TERM

# ---------------------------------------------------------------------------
# Parse BACKLOG.md into tab-separated records:
#   M <msid> <mstitle> <target> <phase> <order>
#   I <msid> <id> <num> <status> <prio> <size> <labels> <ver> <title>
# Diagnostics go to stderr with a line number and exit non-zero.
# ---------------------------------------------------------------------------
parse() {
	awk '
	function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }

	# The text between the first pair of ** on the line.
	function bold(s,   p, rest, q) {
		p = index(s, "**"); if (!p) return ""
		rest = substr(s, p + 2)
		q = index(rest, "**"); if (!q) return ""
		return substr(rest, 1, q - 1)
	}

	# The body of a `<!-- tag: ... -->` comment, or "".
	function comment(s, tag,   p, rest, q) {
		p = index(s, "<!-- " tag ":"); if (!p) return ""
		rest = substr(s, p + length("<!-- " tag ":"))
		q = index(rest, "-->"); if (!q) return ""
		return trim(substr(rest, 1, q - 1))
	}

	function err(n, msg) { printf "BACKLOG.md:%d: %s\n", n, msg > "/dev/stderr"; bad++ }

	function flush(   b, id, ttl, dash, meta, n, kv, i, eq, k, v) {
		if (buf == "") return
		b = buf; buf = ""

		ttl = bold(b)
		dash = index(ttl, " \342\200\224 ")          # em dash, byte-literal
		if (!dash) { err(bufline, "item title must read `**AB-n — Name**`"); return }
		id = substr(ttl, 1, dash - 1)
		ttl = trim(substr(ttl, dash + length(" \342\200\224 ")))
		if (id !~ /^AB-[0-9]+$/) { err(bufline, "bad id `" id "`"); return }
		if (ttl == "") { err(bufline, id ": empty title"); return }
		if (msid == "") { err(bufline, id ": item outside any milestone"); return }

		prio = ""; size = ""; labels = ""; ver = ""
		meta = comment(b, "ab")
		if (meta == "") { err(bufline, id ": missing `<!-- ab: ... -->` metadata"); return }
		n = split(meta, kv, /[ \t]+/)
		for (i = 1; i <= n; i++) {
			eq = index(kv[i], "=")
			if (!eq) { err(bufline, id ": metadata `" kv[i] "` is not key=value"); continue }
			k = substr(kv[i], 1, eq - 1); v = substr(kv[i], eq + 1)
			if (k == "prio") prio = v
			else if (k == "size") size = v
			else if (k == "labels") labels = v
			else if (k == "ver") ver = v
			else err(bufline, id ": unknown metadata key `" k "`")
		}
		printf "I\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n", \
			msid, id, substr(id, 4) + 0, bufstatus, prio, size, labels, ver, ttl
		printf "L\t%d\t%s\n", bufline, id       # id -> line, for lint messages
	}

	/^```/ { fence = !fence; next }
	fence  { next }

	/^## / {
		flush()
		line = $0
		head = substr(line, 4)
		p = index(head, "<!--"); if (p) head = substr(head, 1, p - 1)
		head = trim(head)
		sub(/ \342\234\205$/, "", head)                 # tolerate a trailing check mark
		dash = index(head, " \342\200\224 ")
		if (head !~ /^M[0-9]+([ \t]|$)/) { msid = ""; next }   # a prose section
		if (!dash) { printf "BACKLOG.md:%d: milestone must read `## Mn — Title`\n", NR > "/dev/stderr"; bad++; msid = ""; next }
		msid = substr(head, 1, dash - 1)
		mstitle = trim(substr(head, dash + length(" \342\200\224 ")))
		target = ""; phase = ""
		meta = comment(line, "ms")
		if (meta == "") { printf "BACKLOG.md:%d: %s: missing `<!-- ms: ... -->` metadata\n", NR, msid > "/dev/stderr"; bad++ }
		n = split(meta, kv, /[ \t]+/)
		for (i = 1; i <= n; i++) {
			eq = index(kv[i], "=")
			if (!eq) continue
			k = substr(kv[i], 1, eq - 1); v = substr(kv[i], eq + 1)
			if (k == "target") target = v
			else if (k == "phase") phase = v
			else { printf "BACKLOG.md:%d: %s: unknown milestone key `%s`\n", NR, msid, k > "/dev/stderr"; bad++ }
		}
		order++
		printf "M\t%s\t%s\t%s\t%s\t%d\n", msid, mstitle, target, phase, order
		printf "N\t%d\t%s\n", NR, msid
		next
	}

	/^- \[[ x]\] / {
		flush()
		bufline = NR
		bufstatus = (substr($0, 4, 1) == "x") ? "done" : "open"
		buf = $0
		next
	}

	# A continuation line of the current item: indented, and not a new bullet.
	buf != "" && /^[ \t]+[^ \t]/ { buf = buf " " $0; next }
	buf != "" { flush() }

	END { flush(); exit (bad ? 1 : 0) }
	' "$backlog"
}

if ! parse >"$tmp/data.tsv"; then
	echo "backlog.sh: BACKLOG.md does not parse — fix the errors above" >&2
	exit 1
fi

# ---------------------------------------------------------------------------
lint() {
	awk -F'\t' '
	function err(msg) { print "BACKLOG.md: " msg > "/dev/stderr"; bad++ }

	BEGIN {
		split("high med low", p, " ");    for (i in p) okprio[p[i]] = 1
		split("S M L XL", s, " ");        for (i in s) oksize[s[i]] = 1
		split("shipped now next later ongoing", f, " "); for (i in f) okphase[f[i]] = 1
		split("trace manifest abr sim check output cli delivery integration tests docs release project", l, " ")
		for (i in l) oklabel[l[i]] = 1
	}

	$1 == "L" { line[$3] = $2; next }
	$1 == "N" { msline[$3] = $2; next }

	$1 == "M" {
		nms++
		if ($2 in seenms) err($2 " is declared twice")
		seenms[$2] = 1
		msphase[$2] = $5; mstarget[$2] = $4
		if (!($5 in okphase)) err($2 ": phase `" $5 "` is not shipped|now|next|later|ongoing")
		if ($4 == "") err($2 ": no target release")
		if ($4 != "ongoing" && $4 !~ /^v[0-9]+\.[0-9]+\.[0-9]+$/) err($2 ": target `" $4 "` is not vX.Y.Z or ongoing")
		next
	}

	$1 == "I" {
		id = $3; num = $4 + 0; nitems++
		if (id in seen) err(id " is used twice (ids are stable and unique)")
		seen[id] = 1
		if (num > max) max = num
		ms = $2; status = $5; prio = $6; size = $7; labels = $8; ver = $9

		if (!(prio in okprio)) err(id ": prio `" prio "` is not high|med|low")
		if (!(size in oksize)) err(id ": size `" size "` is not S|M|L|XL")
		if (labels == "") err(id ": no labels")
		n = split(labels, ls, ",")
		for (i = 1; i <= n; i++)
			if (!(ls[i] in oklabel)) err(id ": unknown label `" ls[i] "`")

		if (status == "done" && ver == "") err(id " is checked off but carries no `ver=` — say which release shipped it")
		if (status == "open" && ver != "") err(id " is open but carries `ver=" ver "` — check it off or drop the version")
		if (ver != "" && ver != "unreleased" && ver !~ /^[0-9]+\.[0-9]+\.[0-9]+$/) err(id ": ver `" ver "` is not X.Y.Z or unreleased")

		if (msphase[ms] == "shipped" && status == "open") err(id " is open inside " ms ", which is marked shipped")

		# Counted per milestone and judged in END. Judging it per *item* advised
		# marking M6 shipped because AB-36 was done, while four of its items were
		# still open — advice that, followed, makes lint fail with "open inside a
		# milestone marked shipped". A milestone now ships item by item, one
		# release at a time, so the only actionable case is the one where every
		# item is done and nobody moved the phase. A note that cries wolf on
		# every CI run is worse than no note.
		if (status == "done") msdone[ms]++
		else msopen[ms]++
		next
	}

	END {
		for (i = 1; i <= max; i++)
			if (!("AB-" i in seen)) err("AB-" i " is missing — ids run 1..N with no gaps; retire an item with status done, never by deleting it")
		if (bad) { printf "\n%d problem(s) in BACKLOG.md\n", bad > "/dev/stderr"; exit 1 }
		for (ms in msdone)
			if (msopen[ms] == 0 && msphase[ms] != "shipped" && msphase[ms] != "ongoing")
				finished[ms] = 1
		# Sorted, because a note ordered by however awk walks its hash is a note
		# that differs between runs of the same file.
		n = 0
		for (ms in finished) sorted[++n] = ms
		for (i = 1; i < n; i++)
			for (j = i + 1; j <= n; j++)
				if (sorted[j] < sorted[i]) { t = sorted[i]; sorted[i] = sorted[j]; sorted[j] = t }
		for (i = 1; i <= n; i++)
			warn = warn "\n  " sorted[i] " is finished — every item is done, so mark it `phase=shipped`: scripts/backlog.sh retarget " sorted[i] " --phase shipped"
		if (warn != "") printf "note:%s\n", warn
		printf "BACKLOG.md OK — %d items across %d milestones\n", nitems, nms
	}
	' "$tmp/data.tsv"
}

# ---------------------------------------------------------------------------
generate() {
	awk -F'\t' '
	function bar(done, total,   filled, i, out) {
		if (total == 0) return "`----------` n/a"
		filled = int(done * 10 / total + 0.5)
		out = ""
		for (i = 0; i < 10; i++) out = out (i < filled ? "#" : ".")
		return "`" out "` " int(done * 100 / total + 0.5) "%"
	}
	function badge(phase) {
		if (phase == "shipped") return "shipped"
		if (phase == "now")     return "**now**"
		if (phase == "next")    return "next"
		if (phase == "later")   return "later"
		return "ongoing"
	}
	function prank(p) { return (p == "high") ? 1 : (p == "med") ? 2 : 3 }

	# A pipe inside a table cell ends the cell, and a backtick does not protect
	# it — `--profile apple|dash-if|none` would silently split one row into
	# three columns. Built by concatenation rather than gsub because the meaning
	# of a backslash in a gsub replacement is not portable.
	function esc(s,   out, i, c) {
		out = ""
		for (i = 1; i <= length(s); i++) {
			c = substr(s, i, 1)
			out = out ((c == "|") ? "\\|" : c)
		}
		return out
	}

	function key(status, prio, num,   n) {
		n = sprintf("%03d", num)
		return ((status == "open") ? "0" : "1") prank(prio) n
	}

	$1 == "M" {
		o = $6 + 0
		ord[o] = $2; mstitle[$2] = $3; target[$2] = $4; phase[$2] = $5
		nms = (o > nms) ? o : nms
		next
	}
	$1 == "I" {
		ms = $2; id = $3; num = $4 + 0; status = $5; prio = $6; size = $7; labels = $8; ver = $9; title = $10
		k = key(status, prio, num)
		cnt[ms]++
		row[ms, k] = sprintf("| **%s** — %s | %s | %s | %s | %s |", \
			id, esc(title), prio, size, esc(labels), \
			(status != "done") ? "open" : \
			(ver == "unreleased") ? "done, unreleased" : "shipped `" ver "`")
		keys[ms] = keys[ms] " " k
		if (status == "done") { done[ms]++; ndone++ } else { open[ms]++; nopen++ }
		total++
		n = split(labels, ls, ",")
		for (i = 1; i <= n; i++) {
			lab[ls[i]]++
			if (status == "open") labopen[ls[i]]++
		}
		if (status == "open" && (phase[ms] == "now" || phase[ms] == "next" || phase[ms] == "ongoing")) {
			upk = prank(prio) sprintf("%03d", num)
			up[upk] = sprintf("- **%s** — %s · `%s` · size `%s` · %s (%s, target `%s`)", \
				id, title, prio, size, labels, ms, target[ms])
			nup++
		}
		next
	}

	function emit_ms(ms,   n, ks, i, j, t, sorted) {
		n = split(keys[ms], ks, " ")
		# ks[] has a leading empty field from the leading space; sort what is there.
		for (i = 1; i <= n; i++)
			for (j = i + 1; j <= n; j++)
				if (ks[j] != "" && (ks[i] == "" || ks[i] > ks[j])) { t = ks[i]; ks[i] = ks[j]; ks[j] = t }
		print ""
		printf "### %s — %s\n\n", ms, mstitle[ms]
		printf "Target `%s` · %s · %d open · %d shipped · %s\n\n", \
			target[ms], badge(phase[ms]), open[ms] + 0, done[ms] + 0, bar(done[ms] + 0, cnt[ms] + 0)
		print "| Item | Priority | Size | Labels | Status |"
		print "|---|---|---|---|---|"
		for (i = 1; i <= n; i++) if (ks[i] != "") print row[ms, ks[i]]
	}

	END {
		print "# Roadmap — abrsim"
		print ""
		print "<!-- GENERATED by scripts/backlog.sh roadmap — do not edit by hand. -->"
		print ""
		print "> This page is **generated** from [BACKLOG.md](BACKLOG.md), the single source"
		print "> of truth for planned work. Regenerate it with `scripts/backlog.sh roadmap`"
		print "> after editing the backlog — CI fails when the two disagree."
		print ""
		printf "**%d items · %d shipped · %d open · %d milestones.**\n", total, ndone, nopen, nms
		print ""
		print "## At a glance"
		print ""
		print "| Milestone | Target | Phase | Progress | Open | Shipped |"
		print "|---|---|---|---|---|---|"
		for (o = 1; o <= nms; o++) {
			ms = ord[o]
			printf "| **%s** — %s | `%s` | %s | %s | %d | %d |\n", \
				ms, esc(mstitle[ms]), target[ms], badge(phase[ms]), \
				bar(done[ms] + 0, cnt[ms] + 0), open[ms] + 0, done[ms] + 0
		}
		print ""
		print "## Next up"
		print ""
		print "The open items with the highest priority in the milestones that are in flight."
		print ""
		n = asortkeys()
		shown = 0
		for (i = 1; i <= n && shown < 8; i++) { print up[sk[i]]; shown++ }
		if (shown == 0) print "_Nothing in flight._"
		print ""
		print "## Milestones"
		for (o = 1; o <= nms; o++) emit_ms(ord[o])
		print ""
		print "## By label"
		print ""
		print "| Label | Items | Open |"
		print "|---|---|---|"
		n = 0
		for (l in lab) { n++; ls[n] = l }
		for (i = 1; i <= n; i++)
			for (j = i + 1; j <= n; j++)
				if (labopen[ls[i]] + 0 < labopen[ls[j]] + 0 || \
				    (labopen[ls[i]] + 0 == labopen[ls[j]] + 0 && ls[i] > ls[j])) { t = ls[i]; ls[i] = ls[j]; ls[j] = t }
		for (i = 1; i <= n; i++) printf "| `%s` | %d | %d |\n", ls[i], lab[ls[i]], labopen[ls[i]] + 0
	}

	function asortkeys(   k, n, i, j, t) {
		n = 0
		for (k in up) { n++; sk[n] = k }
		for (i = 1; i <= n; i++)
			for (j = i + 1; j <= n; j++)
				if (sk[i] > sk[j]) { t = sk[i]; sk[i] = sk[j]; sk[j] = t }
		return n
	}
	' "$tmp/data.tsv"
}

# ---------------------------------------------------------------------------
stats() {
	awk -F'\t' '
	$1 == "I" { total++; if ($5 == "done") d++; else { o++; prio[$6]++ } }
	$1 == "M" { ms++ }
	END { printf "%d items · %d shipped · %d open (%d high, %d med, %d low) · %d milestones\n", \
		total, d, o, prio["high"] + 0, prio["med"] + 0, prio["low"] + 0, ms }
	' "$tmp/data.tsv"
}

next_items() {
	n=${1:-5}
	awk -F'\t' -v want="$n" '
	function prank(p) { return (p == "high") ? 1 : (p == "med") ? 2 : 3 }
	$1 == "M" { phase[$2] = $5; target[$2] = $4 }
	$1 == "I" && $5 == "open" {
		k = prank($6) sprintf("%03d", $4)
		if (phase[$2] == "later") k = "9" k
		line[k] = sprintf("%-7s %-4s %-2s  %-28s %s", $3, $6, $7, $10, $2 " → " target[$2])
		n++; ks[n] = k
	}
	END {
		for (i = 1; i <= n; i++)
			for (j = i + 1; j <= n; j++)
				if (ks[i] > ks[j]) { t = ks[i]; ks[i] = ks[j]; ks[j] = t }
		for (i = 1; i <= n && i <= want; i++) print line[ks[i]]
	}
	' "$tmp/data.tsv"
}

# ---------------------------------------------------------------------------
# GitHub issue sync.
#
# BACKLOG.md stays the source of truth; the issues are a view of it, the same way
# ROADMAP.md is. Deciding what to do is kept separate from doing it: `issues`
# prints a plan and touches nothing, `issues --apply` executes that plan. The
# planner is what the tests assert, because the failure modes are all in the
# decision — a sync that is not idempotent opens a duplicate for every item on
# every push, and one that misreads a tick closes issues for work still open.
# ---------------------------------------------------------------------------

# item_bodies emits `<id> <tab> <prose>` — the item's text with the metadata
# comment and the bold title removed, whitespace collapsed onto one line.
item_bodies() {
	awk '
	function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }

	function flush(   b, p, rest, q, id, dash) {
		if (buf == "") return
		b = buf; buf = ""
		p = index(b, "**"); if (!p) return
		rest = substr(b, p + 2)
		q = index(rest, "**"); if (!q) return
		id = substr(rest, 1, q - 1)
		dash = index(id, " \342\200\224 ")
		if (!dash) return
		id = substr(id, 1, dash - 1)

		# Everything after the closing ** of the title, minus a leading colon.
		rest = substr(rest, q + 2)
		sub(/^[ \t]*:[ \t]*/, "", rest)
		# Drop the metadata comment wherever it sits.
		p = index(rest, "<!-- ab:")
		if (p) rest = substr(rest, 1, p - 1)
		gsub(/[ \t]+/, " ", rest)
		printf "%s\t%s\n", id, trim(rest)
	}

	/^- \[[ x]\] \*\*AB-/ { flush(); buf = $0; next }
	buf != "" && /^[ \t]+[^ \t]/ { buf = buf " " $0; next }
	buf != "" { flush() }
	END { flush() }
	' "$backlog"
}

# existing_issues emits `<id> <tab> <number> <tab> <state> <tab> <title>` for the
# issues that already exist. A snapshot file stands in for GitHub under test.
existing_issues() {
	if [ -n "${BACKLOG_ISSUES_SNAPSHOT:-}" ]; then
		cat "$BACKLOG_ISSUES_SNAPSHOT"
		return
	fi
	# The id is the title prefix, which is the only durable link back to the
	# backlog: issue numbers are assigned by GitHub and ids never change.
	gh issue list --state all --limit 500 \
		--json number,title,state \
		--jq '.[] | "\(.title)\t\(.number)\t\(.state | ascii_downcase)"' |
		awk -F'\t' '{
			ttl = $1
			dash = index(ttl, " \342\200\224 ")
			id = dash ? substr(ttl, 1, dash - 1) : ttl
			if (id ~ /^AB-[0-9]+$/) printf "%s\t%s\t%s\t%s\n", id, $2, $3, ttl
		}'
}

# plan_issues prints one action line per item, plus a human-readable summary on
# stderr-free trailing lines. Actions:
#   CREATE <id> -        an open item with no issue
#   REOPEN <id> <num>    an open item whose issue was closed
#   CLOSE  <id> <num>    a shipped item whose issue is still open
#   RETITLE <id> <num>   the issue exists but its title no longer matches the item
#   OK     <id> <num>    already in the right state
#   SKIP   <id> -        shipped and never had an issue: do not create one now
#
# RETITLE exists for two reasons: renaming a backlog item should rename its issue,
# and it is how the issues opened with an empty name — a field-index bug in
# issue_meta — get repaired without closing and reopening anything.
plan_issues() {
	awk -v filter="$1" -F'\t' -v exfile="$tmp/existing.tsv" '
	BEGIN {
		n = split(filter, f, ",")
		for (i = 1; i <= n; i++) if (f[i] != "") want[f[i]] = 1
	}
	FILENAME == exfile { ex_num[$1] = $2; ex_state[$1] = $3; ex_title[$1] = $4; next }
	$1 == "M" { ms_title[$2] = $3; ms_target[$2] = $4; next }
	$1 == "I" {
		msid = $2; id = $3; status = $5
		if (length(want) && !(msid in want)) next
		num = (id in ex_num) ? ex_num[id] : ""
		st = (id in ex_state) ? ex_state[id] : ""
		# A drifted title is corrected whatever state the item is in: a shipped
		# item can have a mistitled issue too. Emitting this before the state line
		# means an item needing both gets both.
		want_title = id " \342\200\224 " $10
		if (num != "" && ex_title[id] != want_title) printf "RETITLE\t%s\t%s\n", id, num
		if (status == "open") {
			if (num == "")            printf "CREATE\t%s\t-\n", id
			else if (st == "closed")  printf "REOPEN\t%s\t%s\n", id, num
			else                      printf "OK\t%s\t%s\n", id, num
		} else {
			if (num == "")            printf "SKIP\t%s\t-\n", id
			else if (st == "open")    printf "CLOSE\t%s\t%s\n", id, num
			else                      printf "OK\t%s\t%s\n", id, num
		}
	}
	' "$tmp/existing.tsv" "$tmp/data.tsv"
}

# issue_body composes the markdown body for one item.
issue_body() {
	awk -v want="$1" -F'\t' \
		-v bodyfile="$tmp/bodies.tsv" \
		-v repo_blob="https://github.com/Allan-Nava/abrsim/blob/main" '
	FILENAME == bodyfile { body[$1] = $2; next }
	$1 == "M" { ms_title[$2] = $3; ms_target[$2] = $4; next }
	$1 == "I" && $3 == want {
		printf "%s\n\n", body[want]
		print "---"
		print ""
		printf "Planned work, tracked in [BACKLOG.md](%s/BACKLOG.md) as `%s` under **%s — %s**, targeted at %s (priority %s, size %s).\n",
			repo_blob, want, $2, ms_title[$2], ms_target[$2], $6, $7
		print ""
		print "`BACKLOG.md` is the single source of truth: it carries the stable `AB-n` id that commits and the CHANGELOG reference, and [ROADMAP.md](" repo_blob "/ROADMAP.md) is generated from it. This issue is a view of that item, kept in step by `scripts/backlog.sh issues --apply`, so closing it means ticking the item in the backlog and regenerating the roadmap in the same commit."
	}
	' "$tmp/bodies.tsv" "$tmp/data.tsv"
}

# issue_meta prints `<title> <tab> <labels> <tab> <milestone title>` for one item.
#
# Tab-separated, not pipe-separated: a pipe is legal inside an item title, and
# AB-63 is literally named `--profile apple|dash-if|none`. Splitting on it truncated
# that issue title at the first pipe.
#
# The item record is `I msid id num status prio size labels ver title`, so the
# title is $10 and $9 is the version. Reading $9 here is what opened 44 issues
# titled "AB-n — " with nothing after the dash: `ver` is empty for an open item, so
# the mistake was invisible on every shipped item and on every unit test that
# looked at the plan or the body rather than the title.
issue_meta() {
	awk -v want="$1" -F'\t' '
	$1 == "M" { ms_title[$2] = $3; next }
	$1 == "I" && $3 == want {
		printf "%s \342\200\224 %s\t%s,prio-%s\t%s \342\200\224 %s\n",
			$3, $10, $8, $6, $2, ms_title[$2]
	}
	' "$tmp/data.tsv"
}

# ensure_milestone creates the GitHub milestone for an M-n if it is missing, and
# is quiet when it already exists.
ensure_milestone() {
	title=$1
	if gh api "repos/:owner/:repo/milestones?state=all&per_page=100" \
		--jq '.[].title' 2>/dev/null | grep -qxF "$title"; then
		return 0
	fi
	gh api "repos/:owner/:repo/milestones" -f title="$title" \
		-f description="Backlog milestone $title. Source of truth: BACKLOG.md" >/dev/null
	echo "  created milestone $title"
}

# ensure_labels creates the label vocabulary if it is missing. Without this a
# fresh clone — or a fork — fails on the first `--apply` with an unknown label,
# and `gh issue create` would otherwise be capable of opening the issue with the
# labels silently dropped.
ensure_labels() {
	gh label list --limit 200 --json name --jq '.[].name' >"$tmp/labels.txt" 2>/dev/null || : >"$tmp/labels.txt"
	# Keep in step with the vocabulary lint enforces, plus the priorities.
	for spec in \
		"trace|1d76db|Network traces: readers, generators, the built-in library" \
		"manifest|0052cc|HLS and DASH ladder readers" \
		"abr|0e8a16|Adaptation algorithms" \
		"sim|006b75|The playback simulator itself" \
		"check|5319e7|An analysis over a simulation result" \
		"output|8a2be2|Renderers: terminal, JSON, markdown" \
		"cli|fbca04|Flags, exit codes, usage" \
		"delivery|c2e0c6|Docker image, packaging, install" \
		"integration|bfd4f2|Using abrsim from other systems" \
		"tests|d4c5f9|Test coverage and test tooling" \
		"docs|0075ca|Documentation and the Pages site" \
		"release|b60205|Tagging, artefacts, signing" \
		"project|6a737d|Backlog, roadmap, repo hygiene" \
		"prio-high|b60205|High priority in BACKLOG.md" \
		"prio-med|fbca04|Medium priority in BACKLOG.md" \
		"prio-low|c2e0c6|Low priority in BACKLOG.md"; do
		name=${spec%%|*}
		rest=${spec#*|}
		colour=${rest%%|*}
		desc=${rest#*|}
		if ! grep -qxF "$name" "$tmp/labels.txt"; then
			gh label create "$name" --color "$colour" --description "$desc" >/dev/null
			echo "  created label $name"
		fi
	done
}

apply_issues() {
	ensure_labels
	while IFS="$(printf '\t')" read -r action id num; do
		case "$action" in
		CREATE)
			meta=$(issue_meta "$id")
			tab=$(printf '\t')
			ttl=${meta%%"$tab"*}
			rest=${meta#*"$tab"}
			labels=${rest%%"$tab"*}
			ms=${rest#*"$tab"}
			ensure_milestone "$ms"
			issue_body "$id" >"$tmp/body.md"
			set -- gh issue create --title "$ttl" --body-file "$tmp/body.md" --milestone "$ms"
			# One --label per name; a label that does not exist is a hard error
			# rather than a silently unlabelled issue.
			old_ifs=$IFS
			IFS=,
			for l in $labels; do
				[ -n "$l" ] && set -- "$@" --label "$l"
			done
			IFS=$old_ifs
			url=$("$@")
			echo "  created $id  $url"
			;;
		RETITLE)
			meta=$(issue_meta "$id")
			ttl=${meta%%"$(printf '\t')"*}
			gh issue edit "$num" --title "$ttl" >/dev/null
			echo "  retitled $id  #$num  -> $ttl"
			;;
		CLOSE)
			gh issue close "$num" \
				--comment "Shipped: the backlog item is ticked in BACKLOG.md. Closed by \`scripts/backlog.sh issues --apply\`." >/dev/null
			echo "  closed $id  #$num"
			;;
		REOPEN)
			gh issue reopen "$num" \
				--comment "Reopened: the backlog item is open again in BACKLOG.md. Reopened by \`scripts/backlog.sh issues --apply\`." >/dev/null
			echo "  reopened $id  #$num"
			;;
		esac
	done
}

issues_cmd() {
	apply=0
	filter=""
	body_of=""
	title_of=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--apply) apply=1 ;;
		--dry-run) apply=0 ;;
		--milestones)
			shift
			filter="${1:-}"
			[ -n "$filter" ] || {
				echo "backlog.sh: --milestones needs a value, e.g. --milestones M11,M12" >&2
				exit 2
			}
			;;
		--milestones=*) filter="${1#--milestones=}" ;;
		--title)
			shift
			title_of="${1:-}"
			[ -n "$title_of" ] || {
				echo "backlog.sh: --title needs an AB-n id" >&2
				exit 2
			}
			;;
		--body)
			shift
			body_of="${1:-}"
			[ -n "$body_of" ] || {
				echo "backlog.sh: --body needs an AB-n id" >&2
				exit 2
			}
			;;
		*)
			echo "backlog.sh: unknown option for issues: $1" >&2
			exit 2
			;;
		esac
		shift
	done

	item_bodies >"$tmp/bodies.tsv"

	if [ -n "$title_of" ]; then
		meta=$(issue_meta "$title_of")
		printf '%s\n' "${meta%%"$(printf '\t')"*}"
		return 0
	fi

	if [ -n "$body_of" ]; then
		issue_body "$body_of"
		return 0
	fi

	existing_issues >"$tmp/existing.tsv"
	plan_issues "$filter" >"$tmp/plan.tsv"

	create=$(grep -c '^CREATE' "$tmp/plan.tsv" || true)
	close=$(grep -c '^CLOSE' "$tmp/plan.tsv" || true)
	reopen=$(grep -c '^REOPEN' "$tmp/plan.tsv" || true)
	retitle=$(grep -c '^RETITLE' "$tmp/plan.tsv" || true)

	cat "$tmp/plan.tsv"
	echo ""
	echo "plan: $create to create, $close to close, $reopen to reopen, $retitle to retitle"

	if [ "$apply" -eq 0 ]; then
		if [ "$((create + close + reopen + retitle))" -gt 0 ]; then
			echo "dry run — nothing was changed. Re-run with --apply to execute."
		else
			echo "nothing to do: the issues already match BACKLOG.md"
		fi
		return 0
	fi

	if [ "$((create + close + reopen + retitle))" -eq 0 ]; then
		echo "nothing to do"
		return 0
	fi
	echo "applying:"
	grep -E '^(CREATE|CLOSE|REOPEN|RETITLE)' "$tmp/plan.tsv" | apply_issues
}

# ---------------------------------------------------------------------------
# The writing half.
#
# Shape shared by all four: build the new file in $tmp, lint *that*, and only then
# move it into place. A backlog half-edited by a command that then failed is worse
# than one nobody automated, because the next reader has no way to tell which half
# is the intention.
# ---------------------------------------------------------------------------

# commit_edit <candidate> <what it did>
commit_edit() {
	if ! BACKLOG_FILE="$1" ROADMAP_FILE="$tmp/roadmap-probe.md" "$0" lint >/dev/null 2>"$tmp/lint.err"; then
		echo "backlog.sh: the edit would not lint, so nothing was written:" >&2
		sed 's/^/  /' "$tmp/lint.err" >&2
		exit 1
	fi
	cat "$1" >"$backlog"
	# data.tsv was parsed before the edit, so generating from it would describe
	# the file as it used to be — a roadmap that is stale the instant it is
	# written, and CI would be the one to notice.
	parse >"$tmp/data.tsv"
	generate >"$tmp/ROADMAP.new"
	cat "$tmp/ROADMAP.new" >"$roadmap"
	echo "$2"
	echo "wrote BACKLOG.md and ROADMAP.md — $(stats)"
}

# wrap <indent> <first-prefix> — wrap stdin to 80 columns, awk only.
wrap() {
	awk -v ind="$1" -v first="$2" '
	BEGIN { RS = "\0"; width = 80 }
	{
		gsub(/[ \t\n]+/, " ")
		sub(/^ /, ""); sub(/ $/, "")
		n = split($0, w, " ")
		line = first
		pad = ind
		for (i = 1; i <= n; i++) {
			cand = (line == first && line ~ / $/) ? line w[i] : line " " w[i]
			if (line == first) cand = line w[i]
			if (length(cand) > width && line != first) {
				print line
				line = pad w[i]
			} else if (length(cand) > width && line == first) {
				print line w[i]
				line = pad
			} else {
				line = cand
			}
		}
		if (line != pad) print line
	}'
}

need_value() {  # need_value <flag> <value>
	if [ -z "${2:-}" ]; then
		echo "backlog.sh: $1 needs a value" >&2
		exit 2
	fi
}

milestone_exists() { grep -q "^## $1 " "$backlog"; }

add_cmd() {
	ms=${1:-}; shift 2>/dev/null || true
	name=${1:-}; shift 2>/dev/null || true
	prio=""; size=""; labels=""; body=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--prio)   need_value --prio "${2:-}"; prio=$2; shift 2 ;;
		--size)   need_value --size "${2:-}"; size=$2; shift 2 ;;
		--labels) need_value --labels "${2:-}"; labels=$2; shift 2 ;;
		--body)   need_value --body "${2:-}"; body=$2; shift 2 ;;
		--body-file)
			need_value --body-file "${2:-}"
			body=$(cat "$2"); shift 2 ;;
		-) body=$(cat); shift ;;
		*) echo "backlog.sh add: unexpected argument $1" >&2; exit 2 ;;
		esac
	done
	if [ -z "$ms" ] || [ -z "$name" ] || [ -z "$prio" ] || [ -z "$size" ] || [ -z "$labels" ]; then
		echo 'usage: scripts/backlog.sh add <Mn> "<Name>" --prio p --size s --labels a,b [--body "..."]' >&2
		exit 2
	fi
	case "$ms" in
	M[0-9]*) ;;
	*) echo "backlog.sh add: $ms is not a milestone id" >&2; exit 2 ;;
	esac
	if ! milestone_exists "$ms"; then
		echo "backlog.sh add: no milestone $ms in BACKLOG.md" >&2
		exit 2
	fi
	[ -n "$body" ] || body="TODO: what it is, why it earns its place, and what it has to touch."

	# The next id is one past the highest that exists. Never a gap that was left
	# by a retired item: ids are stable forever, and reusing one silently
	# reassigns every commit message that mentions it.
	next=$(awk -F'\t' '$1 == "I" { if ($4 + 0 > max) max = $4 + 0 } END { print max + 1 }' "$tmp/data.tsv")
	id="AB-$next"

	{
		printf '%s' "$body" | wrap "  " "- [ ] **$id — $name**: "
		echo "  <!-- ab: prio=$prio size=$size labels=$labels -->"
	} >"$tmp/item.md"

	# Insert after the last non-blank line of the milestone's own section. Doing
	# it with a line number rather than by streaming the file keeps the blank
	# lines exactly where they were: the first attempt buffered them and ate
	# every one inside the section it touched.
	at=$(awk -v ms="$ms" '
		/^## / { inms = ($2 == ms); next }
		inms && $0 != "" { last = NR }
		END { print last }
	' "$backlog")
	if [ -z "$at" ] || [ "$at" -eq 0 ] 2>/dev/null; then
		# An empty milestone: put the item straight after its heading.
		at=$(awk -v ms="$ms" '/^## / && $2 == ms { print NR; exit }' "$backlog")
	fi
	awk -v at="$at" -v itemfile="$tmp/item.md" '
	{ print }
	NR == at {
		while ((getline line < itemfile) > 0) print line
		close(itemfile)
	}
	' "$backlog" >"$tmp/BACKLOG.new"

	commit_edit "$tmp/BACKLOG.new" "added $id — $name  ($ms, prio=$prio size=$size labels=$labels)"
}

done_cmd() {
	id=${1:-}; shift 2>/dev/null || true
	ver=""; note=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--ver)  need_value --ver "${2:-}"; ver=$2; shift 2 ;;
		--note) need_value --note "${2:-}"; note=$2; shift 2 ;;
		*) echo "backlog.sh done: unexpected argument $1" >&2; exit 2 ;;
		esac
	done
	if [ -z "$id" ] || [ -z "$ver" ]; then
		echo 'usage: scripts/backlog.sh done <AB-n> --ver X.Y.Z [--note "what shipped"]' >&2
		exit 2
	fi
	case "$ver" in
	[0-9]*.[0-9]*.[0-9]* | unreleased) ;;
	*) echo "backlog.sh done: --ver is X.Y.Z or unreleased, not $ver" >&2; exit 2 ;;
	esac
	if ! awk -F'\t' -v id="$id" '$1 == "I" && $3 == id { found = 1 } END { exit found ? 0 : 1 }' "$tmp/data.tsv"; then
		echo "backlog.sh done: no $id in BACKLOG.md" >&2
		exit 2
	fi
	# `exit 0` inside a rule still runs END, whose `exit 1` then wins — the first
	# version of this guard never fired for that reason, and `done` cheerfully
	# reported success on an item that was already ticked. Set a flag, exit once.
	if awk -F'\t' -v id="$id" '$1 == "I" && $3 == id && $5 == "done" { d = 1 } END { exit d ? 0 : 1 }' "$tmp/data.tsv"; then
		echo "backlog.sh done: $id is already done — a shipped item stays shipped" >&2
		exit 2
	fi

	printf '%s' "$note" | wrap "  " "  " >"$tmp/note.md"

	awk -v id="$id" -v ver="$ver" -v notefile="$tmp/note.md" -v hasnote="${note:+1}" '
	index($0, "**" id " —") && /^- \[ \] / { initem = 1; sub(/^- \[ \] /, "- [x] "); print; next }
	initem && /<!-- ab:/ {
		if (hasnote != "") {
			while ((getline line < notefile) > 0) print line
			close(notefile)
		}
		sub(/ -->/, " ver=" ver " -->")
		print
		initem = 0
		next
	}
	{ print }
	' "$backlog" >"$tmp/BACKLOG.new"

	# A command that reports success while changing nothing is the worst of the
	# three outcomes: the caller believes the backlog now says something it does
	# not.
	if cmp -s "$backlog" "$tmp/BACKLOG.new"; then
		echo "backlog.sh done: $id was found but nothing changed — the item's line does not match the shape this can edit" >&2
		exit 1
	fi
	commit_edit "$tmp/BACKLOG.new" "ticked $id, shipped in $ver"
}

milestone_cmd() {
	ms=${1:-}; shift 2>/dev/null || true
	title=${1:-}; shift 2>/dev/null || true
	target=""; phase=""; intro=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--target) need_value --target "${2:-}"; target=$2; shift 2 ;;
		--phase)  need_value --phase "${2:-}"; phase=$2; shift 2 ;;
		--intro)  need_value --intro "${2:-}"; intro=$2; shift 2 ;;
		*) echo "backlog.sh milestone: unexpected argument $1" >&2; exit 2 ;;
		esac
	done
	if [ -z "$ms" ] || [ -z "$title" ] || [ -z "$target" ] || [ -z "$phase" ]; then
		echo 'usage: scripts/backlog.sh milestone <Mn> "<Title>" --target vX.Y.Z|ongoing --phase shipped|now|next|later|ongoing [--intro "..."]' >&2
		exit 2
	fi
	case "$ms" in M[0-9]*) ;; *) echo "backlog.sh milestone: $ms is not a milestone id" >&2; exit 2 ;; esac
	if milestone_exists "$ms"; then
		echo "backlog.sh milestone: $ms already exists — milestone ids are identities, not slots" >&2
		exit 2
	fi
	case "$target" in
	ongoing | v[0-9]*.[0-9]*.[0-9]*) ;;
	*) echo "backlog.sh milestone: --target is vX.Y.Z or ongoing, not $target" >&2; exit 2 ;;
	esac
	case "$phase" in
	shipped | now | next | later | ongoing) ;;
	*) echo "backlog.sh milestone: --phase is shipped|now|next|later|ongoing, not $phase" >&2; exit 2 ;;
	esac

	{
		echo "## $ms — $title <!-- ms: target=$target phase=$phase -->"
		echo ""
		if [ -n "$intro" ]; then
			printf '%s' "$intro" | wrap "" ""
			echo ""
		fi
	} >"$tmp/ms.md"

	# Before the first ongoing milestone, so the housekeeping section stays last;
	# at the end of the file when there is none.
	anchor=$(awk '/^## M[0-9]+ /  && /phase=ongoing/ { print NR; exit }' "$backlog")
	if [ -n "$anchor" ]; then
		awk -v at="$anchor" -v msfile="$tmp/ms.md" 'NR == at {
			while ((getline line < msfile) > 0) print line
			close(msfile)
		} { print }' "$backlog" >"$tmp/BACKLOG.new"
	else
		{ cat "$backlog"; echo ""; cat "$tmp/ms.md"; } >"$tmp/BACKLOG.new"
	fi

	commit_edit "$tmp/BACKLOG.new" "created $ms — $title  (target=$target phase=$phase)"
}

retarget_cmd() {
	ms=${1:-}; shift 2>/dev/null || true
	target=""; phase=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--target) need_value --target "${2:-}"; target=$2; shift 2 ;;
		--phase)  need_value --phase "${2:-}"; phase=$2; shift 2 ;;
		*) echo "backlog.sh retarget: unexpected argument $1" >&2; exit 2 ;;
		esac
	done
	if [ -z "$ms" ] || { [ -z "$target" ] && [ -z "$phase" ]; }; then
		echo 'usage: scripts/backlog.sh retarget <Mn> [--target vX.Y.Z|ongoing] [--phase shipped|now|next|later|ongoing]' >&2
		exit 2
	fi
	if ! milestone_exists "$ms"; then
		echo "backlog.sh retarget: no milestone $ms in BACKLOG.md" >&2
		exit 2
	fi
	if [ -n "$target" ]; then
		case "$target" in
		ongoing | v[0-9]*.[0-9]*.[0-9]*) ;;
		*) echo "backlog.sh retarget: --target is vX.Y.Z or ongoing, not $target" >&2; exit 2 ;;
		esac
	fi
	if [ -n "$phase" ]; then
		case "$phase" in
		shipped | now | next | later | ongoing) ;;
		*) echo "backlog.sh retarget: --phase is shipped|now|next|later|ongoing, not $phase" >&2; exit 2 ;;
		esac
	fi

	awk -v ms="$ms" -v target="$target" -v phase="$phase" '
	$0 ~ "^## " ms " " {
		if (target != "") { sub(/target=[^ ]+/, "target=" target) }
		if (phase  != "") { sub(/phase=[^ ->]+/, "phase=" phase) }
	}
	{ print }
	' "$backlog" >"$tmp/BACKLOG.new"

	commit_edit "$tmp/BACKLOG.new" "retargeted $ms${target:+ target=$target}${phase:+ phase=$phase}"
}

case "${1:-lint}" in
lint)
	lint
	;;
roadmap)
	generate >"$tmp/ROADMAP.md"
	mv "$tmp/ROADMAP.md" "$roadmap"
	echo "wrote ROADMAP.md — $(stats)"
	;;
check)
	lint >/dev/null
	generate >"$tmp/ROADMAP.md"
	if ! diff -u "$roadmap" "$tmp/ROADMAP.md"; then
		echo "" >&2
		echo "ROADMAP.md is stale: run scripts/backlog.sh roadmap and commit the result" >&2
		exit 1
	fi
	echo "ROADMAP.md is up to date"
	;;
stats)
	stats
	;;
next)
	next_items "${2:-5}"
	;;
issues)
	shift
	issues_cmd "$@"
	;;
add)
	shift
	add_cmd "$@"
	;;
done)
	shift
	done_cmd "$@"
	;;
milestone)
	shift
	milestone_cmd "$@"
	;;
retarget)
	shift
	retarget_cmd "$@"
	;;
*)
	echo "usage: scripts/backlog.sh [lint|roadmap|check|stats|next [n]]" >&2
	echo "       scripts/backlog.sh issues [--apply] [--milestones M11,M12] [--body AB-n]" >&2
	echo '       scripts/backlog.sh add <Mn> "<Name>" --prio p --size s --labels a,b [--body "..."]' >&2
	echo '       scripts/backlog.sh done <AB-n> --ver X.Y.Z [--note "..."]' >&2
	echo '       scripts/backlog.sh milestone <Mn> "<Title>" --target vX.Y.Z|ongoing --phase p [--intro "..."]' >&2
	echo '       scripts/backlog.sh retarget <Mn> [--target ...] [--phase ...]' >&2
	exit 2
	;;
esac
