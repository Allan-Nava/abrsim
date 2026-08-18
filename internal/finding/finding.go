// Package finding is the result model: one Finding is one observation about one
// target, a Result aggregates a run. The severity order and the worst-first
// sort are the two rules every renderer is built on.
package finding

import "sort"

// Status of a single finding. Severity order: OK < WARN < BAD < ERROR.
type Status string

const (
	OK    Status = "OK"
	WARN  Status = "WARN"
	BAD   Status = "BAD"
	ERROR Status = "ERROR" // the check itself could not run
)

var severity = map[Status]int{OK: 0, WARN: 1, BAD: 2, ERROR: 3}

// Severity is the numeric rank of s.
func Severity(s Status) int { return severity[s] }

// AtLeast reports whether s is at or above threshold. An empty threshold is
// satisfied by anything, since severity[""] is the zero value — a caller that
// means "no threshold at all" has to test for "" itself.
func AtLeast(s, threshold Status) bool { return severity[s] >= severity[threshold] }

// Finding is one observation about one target.
//
// Check is the analysis that produced it, Target what was looked at — a
// rendition, a rung pair, the session. Value and Unit carry the measurement so
// a machine consumer never has to parse Message, and Hint says what it means
// for the viewer, which is the only reason an operator reads any of this.
type Finding struct {
	Check   string   `json:"check"`
	Target  string   `json:"target"`
	Status  Status   `json:"status"`
	Message string   `json:"message"`
	Value   *float64 `json:"value,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Hint    string   `json:"hint,omitempty"`
}

// Num returns a pointer to v, for setting Finding.Value inline.
func Num(v float64) *float64 { return &v }

// Summarize counts findings per status.
func Summarize(fs []Finding) map[Status]int {
	out := map[Status]int{OK: 0, WARN: 0, BAD: 0, ERROR: 0}
	for _, f := range fs {
		out[f.Status]++
	}
	return out
}

// Worst returns the highest severity present, or OK for no findings.
func Worst(fs []Finding) Status {
	worst := OK
	for _, f := range fs {
		if AtLeast(f.Status, worst) {
			worst = f.Status
		}
	}
	return worst
}

// SortWorstFirst orders findings by descending severity, then by check and
// target so two identical runs render byte-identically.
func SortWorstFirst(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if a, b := severity[fs[i].Status], severity[fs[j].Status]; a != b {
			return a > b
		}
		if fs[i].Check != fs[j].Check {
			return fs[i].Check < fs[j].Check
		}
		return fs[i].Target < fs[j].Target
	})
}
