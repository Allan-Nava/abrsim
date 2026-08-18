package trace

import (
	"sort"
	"testing"
)

func TestBuiltins_AreWellFormed(t *testing.T) {
	names := Names()
	if len(names) < 5 {
		t.Fatalf("only %d built-in traces: %v — a user with no measurement of their own has nothing to run", len(names), names)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("Names() is not sorted: %v", names)
	}

	for _, name := range names {
		tr, ok := Builtin(name)
		if !ok {
			t.Errorf("%s: listed by Names() but Builtin says it does not exist", name)
			continue
		}
		if tr.Name != name {
			t.Errorf("%s: Name = %q", name, tr.Name)
		}
		if Describe(name) == "" {
			t.Errorf("%s: no description — a trace nobody can explain is a number nobody can defend", name)
		}
		if len(tr.Samples) < 2 {
			t.Errorf("%s: %d samples, want at least 2", name, len(tr.Samples))
		}
		if got := tr.Span(); got < 120 {
			t.Errorf("%s: spans %.0fs, want at least 120s — a trace shorter than a startup plus a few segments cannot exercise anything", name, got)
		}
		for i, s := range tr.Samples {
			if s.BPS < 0 {
				t.Errorf("%s: sample %d has a negative rate %v", name, i, s.BPS)
			}
			if i > 0 && s.At <= tr.Samples[i-1].At {
				t.Errorf("%s: sample %d at %v does not follow %v", name, i, s.At, tr.Samples[i-1].At)
			}
		}
		if last := tr.Samples[len(tr.Samples)-1]; last.BPS <= 0 {
			t.Errorf("%s: the trace ends on zero bandwidth, so every download past it is unfinishable — end on a real rate and let a dip in the middle do the damage", name)
		}
	}
}

func TestBuiltins_AreDeterministic(t *testing.T) {
	// The whole promise of the tool is that two runs are identical. A built-in
	// generated from a clock or an unseeded source would break that in a way
	// nothing else in the test suite would catch.
	for _, name := range Names() {
		a, _ := Builtin(name)
		b, _ := Builtin(name)
		if len(a.Samples) != len(b.Samples) {
			t.Fatalf("%s: two calls produced %d and %d samples", name, len(a.Samples), len(b.Samples))
		}
		for i := range a.Samples {
			if a.Samples[i] != b.Samples[i] {
				t.Fatalf("%s: sample %d differs between two calls: %+v vs %+v", name, i, a.Samples[i], b.Samples[i])
			}
		}
	}
}

func TestBuiltin_UnknownNameListsTheAlternatives(t *testing.T) {
	if _, ok := Builtin("no-such-trace"); ok {
		t.Fatal("Builtin invented a trace")
	}
}

func TestBuiltin_FlatIsFlat(t *testing.T) {
	tr, ok := Builtin("flat-5m")
	if !ok {
		t.Fatal("flat-5m is missing — it is the control every other trace is read against")
	}
	for _, s := range tr.Samples {
		if s.BPS != 5e6 {
			t.Fatalf("flat-5m carries a %v sample: it is the trace that must produce no findings at all", s.BPS)
		}
	}
}

func TestBuiltin_StepsDownWalksTheWholeLadder(t *testing.T) {
	// The trace whose job is to make the player visit every rung: strictly
	// descending, from above any sane top rung to below any sane bottom one.
	tr, ok := Builtin("steps-down")
	if !ok {
		t.Fatal("steps-down is missing")
	}
	for i := 1; i < len(tr.Samples); i++ {
		if tr.Samples[i].BPS >= tr.Samples[i-1].BPS {
			t.Fatalf("sample %d (%v) does not descend from %v", i, tr.Samples[i].BPS, tr.Samples[i-1].BPS)
		}
	}
	if first, last := tr.Samples[0].BPS, tr.Samples[len(tr.Samples)-1].BPS; first < 8e6 || last > 4e5 {
		t.Fatalf("steps-down runs %v..%v, want it to start above 8 Mbps and end below 400 kbps", first, last)
	}
}

func TestBuiltin_TrainGoesDark(t *testing.T) {
	// A tunnel is the shape that produces a rebuffer no ABR algorithm can avoid,
	// which is what makes it the trace that tells buffer policy from bitrate
	// policy. Without a real outage the rebuffer check is never exercised.
	tr, ok := Builtin("train")
	if !ok {
		t.Fatal("train is missing")
	}
	var longest float64
	for i := 0; i < len(tr.Samples)-1; i++ {
		if tr.Samples[i].BPS == 0 {
			if d := tr.Samples[i+1].At - tr.Samples[i].At; d > longest {
				longest = d
			}
		}
	}
	if longest < 2 {
		t.Fatalf("longest blackout is %.1fs, want at least 2s", longest)
	}
}
