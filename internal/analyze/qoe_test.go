package analyze

import (
	"math"
	"testing"

	"github.com/Allan-Nava/abrsim/internal/sim"
)

// AB-41. The score is the linear QoE the published ABR literature optimises
// against, expressed so a human can say what it means: "as good as watching a
// steady N Mbps with no stalls and no switches". Every test here pins one term of
// it, because a score nobody can decompose is a score nobody can argue with.

func steady(mbps float64, segments int) sim.Result {
	res := sim.Result{Bitrates: []int64{int64(mbps * 1e6)}, Names: []string{"only"}}
	for i := 0; i < segments; i++ {
		res.Requests = append(res.Requests, sim.Request{Rung: 0, Duration: 4})
		res.Media += 4
	}
	return res
}

func TestQoE_ASteadySessionScoresItsOwnBitrate(t *testing.T) {
	got, ok := QoE(steady(2, 10), DefaultQoEWeights())
	if !ok {
		t.Fatal("no score for a session that played")
	}
	if math.Abs(got-2) > 1e-9 {
		t.Errorf("QoE = %v, want 2 — an unbroken 2 Mbps session is worth exactly its bitrate, which is what makes the number readable", got)
	}
}

func TestQoE_AStallCostsExactlyItsWeight(t *testing.T) {
	w := DefaultQoEWeights()
	res := steady(2, 10)      // 40s of media
	res.Requests[3].Stall = 2 // two seconds frozen
	res.StallTime = 2
	got, _ := QoE(res, w)
	want := 2 - w.Rebuffer*2/40
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("QoE = %v, want %v (2 Mbps less %v per frozen second over 40s of playback)", got, want, w.Rebuffer)
	}
	if got >= 2 {
		t.Error("a stall did not cost anything")
	}
}

func TestQoE_ASwitchCostsTheSizeOfTheStep(t *testing.T) {
	w := DefaultQoEWeights()
	res := sim.Result{
		Bitrates: []int64{1_000_000, 3_000_000},
		Names:    []string{"1000k", "3000k"},
		Media:    40,
	}
	// Five segments low, five high: one step of 2 Mbps.
	for i := 0; i < 5; i++ {
		res.Requests = append(res.Requests, sim.Request{Rung: 0, Duration: 4})
	}
	for i := 0; i < 5; i++ {
		res.Requests = append(res.Requests, sim.Request{Rung: 1, Duration: 4})
	}
	got, _ := QoE(res, w)
	utility := (5*1.0*4 + 5*3.0*4) / 40.0
	want := utility - w.Switch*2/40
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("QoE = %v, want %v — one 2 Mbps step charged at %v per Mbps over 40s", got, want, w.Switch)
	}
}

func TestQoE_TheWeightsAreAJudgementSomebodyCanChange(t *testing.T) {
	res := steady(2, 10)
	res.Requests[3].Stall = 2
	res.StallTime = 2
	strict, _ := QoE(res, QoEWeights{Rebuffer: 10, Switch: 1})
	lenient, _ := QoE(res, QoEWeights{Rebuffer: 1, Switch: 1})
	if !(strict < lenient) {
		t.Errorf("a heavier rebuffer penalty scored no worse: %v against %v", strict, lenient)
	}
	if w := DefaultQoEWeights(); w.Rebuffer != 4.3 || w.Switch != 1 {
		t.Errorf("the defaults are %+v — they are the literature's (Pensieve), and changing them silently would make every published comparison wrong", w)
	}
}

func TestQoE_NothingWatchedIsNotAScoreOfZero(t *testing.T) {
	if v, ok := QoE(sim.Result{}, DefaultQoEWeights()); ok {
		t.Errorf("an empty run scored %v — nothing was watched, so there is nothing to score", v)
	}
}
