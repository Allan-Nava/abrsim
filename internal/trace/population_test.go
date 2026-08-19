package trace

import "testing"

// A population is only worth having if it is the same population everywhere. The
// first four tests are the properties the derivation has to keep; the fifth is
// the one that makes `--viewers 1` a safe default.

func TestPopulation_OneViewerIsTheTraceItself(t *testing.T) {
	base, _ := Builtin("mobile-4g")
	got := Population(base, 1)
	if len(got) != 1 {
		t.Fatalf("Population(base, 1) returned %d traces", len(got))
	}
	if got[0].Name != base.Name {
		t.Errorf("name = %q, want %q", got[0].Name, base.Name)
	}
	if len(got[0].Samples) != len(base.Samples) {
		t.Fatalf("%d samples, want %d", len(got[0].Samples), len(base.Samples))
	}
	for i, s := range got[0].Samples {
		if s != base.Samples[i] {
			t.Fatalf("sample %d = %+v, want %+v — one viewer has to be the trace as measured, or --viewers 1 quietly stops being today's run", i, s, base.Samples[i])
		}
	}
}

func TestPopulation_IsDeterministic(t *testing.T) {
	base, _ := Builtin("dsl-evening")
	a, b := Population(base, 12), Population(base, 12)
	for v := range a {
		for i, s := range a[v].Samples {
			if s != b[v].Samples[i] {
				t.Fatalf("viewer %d sample %d: %+v then %+v — a population nobody can regenerate is a benchmark nobody can argue with", v, i, s, b[v].Samples[i])
			}
		}
	}
}

func TestPopulation_KeepsTheShapeAndTheClock(t *testing.T) {
	base, _ := Builtin("mobile-4g")
	for v, tr := range Population(base, 8) {
		if len(tr.Samples) != len(base.Samples) {
			t.Fatalf("viewer %d has %d samples, want %d", v, len(tr.Samples), len(base.Samples))
		}
		for i, s := range tr.Samples {
			if s.At != base.Samples[i].At {
				t.Errorf("viewer %d sample %d at %v, want %v — a viewer on the same cell sees it change at the same moments", v, i, s.At, base.Samples[i].At)
			}
			if s.BPS <= 0 {
				t.Errorf("viewer %d sample %d has rate %v", v, i, s.BPS)
			}
			if lo, hi := base.Samples[i].BPS*0.5, base.Samples[i].BPS*1.8; s.BPS < lo || s.BPS > hi {
				t.Errorf("viewer %d sample %d: %.0f bps is outside [%.0f, %.0f] of the base — the population is a spread around one network, not a different network", v, i, s.BPS, lo, hi)
			}
		}
	}
}

func TestPopulation_ATunnelIsATunnelForEveryone(t *testing.T) {
	base, _ := Builtin("train")
	zeros := 0
	for i, s := range base.Samples {
		if s.BPS != 0 {
			continue
		}
		zeros++
		for v, tr := range Population(base, 6) {
			if tr.Samples[i].BPS != 0 {
				t.Errorf("viewer %d has %.0f bps inside the tunnel at %vs — scaling nothing has to leave nothing", v, tr.Samples[i].BPS, s.At)
			}
		}
	}
	if zeros == 0 {
		t.Fatal("the train trace has no outage in it any more, so this test proves nothing")
	}
}

func TestPopulation_ViewersAreNotAllTheSameViewer(t *testing.T) {
	base, _ := Builtin("flat-5m")
	pop := Population(base, 30)
	seen := map[float64]int{}
	for _, tr := range pop {
		seen[tr.Samples[0].BPS]++
	}
	if len(seen) < 20 {
		t.Errorf("30 viewers produced %d distinct opening rates — a population of clones is one anecdote costing thirty times as much", len(seen))
	}
	slow, fast := 0, 0
	for _, tr := range pop {
		switch {
		case tr.Samples[0].BPS < base.Samples[0].BPS*0.9:
			slow++
		case tr.Samples[0].BPS > base.Samples[0].BPS*1.1:
			fast++
		}
	}
	if slow < 5 || fast < 5 {
		t.Errorf("%d viewers clearly below the base rate and %d clearly above it — the tail is the whole point of a population", slow, fast)
	}
}

func TestPopulation_NoViewersIsNoTraces(t *testing.T) {
	base, _ := Builtin("flat-5m")
	for _, n := range []int{0, -1} {
		if got := Population(base, n); got != nil {
			t.Errorf("Population(base, %d) = %d traces, want nil", n, len(got))
		}
	}
}

func TestPopulation_SpansTheRangeEvenWhenSmall(t *testing.T) {
	base, _ := Builtin("flat-5m")
	for _, n := range []int{8, 30, 200} {
		pop := Population(base, n)
		lo, hi := pop[1].Samples[0].BPS, pop[1].Samples[0].BPS
		for _, tr := range pop[1:] {
			r := tr.Samples[0].BPS
			if r < lo {
				lo = r
			}
			if r > hi {
				hi = r
			}
		}
		b := base.Samples[0].BPS
		if lo > b*0.8 {
			t.Errorf("n=%d: the worst line is %.0f bps against a base of %.0f — no bottom tail, so the p95 this exists to report would be a guess", n, lo, b)
		}
		if hi < b*1.2 {
			t.Errorf("n=%d: the best line is %.0f bps against a base of %.0f — no top tail", n, hi, b)
		}
	}
}

func TestPopulation_IsNotSortedByLuck(t *testing.T) {
	base, _ := Builtin("flat-5m")
	pop := Population(base, 60)
	ascending, descending := true, true
	for v := 2; v < len(pop); v++ {
		if pop[v].Samples[0].BPS < pop[v-1].Samples[0].BPS {
			ascending = false
		}
		if pop[v].Samples[0].BPS > pop[v-1].Samples[0].BPS {
			descending = false
		}
	}
	if ascending || descending {
		t.Error("the viewers come out in rate order — anybody who looks at the first ten of them is looking at the ten best or the ten worst lines and will not know it")
	}
}
