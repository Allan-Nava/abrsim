package device

import "testing"

func TestClasses_EachOneHasACeilingAndADescription(t *testing.T) {
	names := Names()
	if len(names) < 4 {
		t.Fatalf("only %d device classes: %v", len(names), names)
	}
	for _, n := range names {
		c, ok := Class(n)
		if !ok {
			t.Errorf("%s is listed by Names() and Class says it does not exist", n)
			continue
		}
		if c.Describe == "" {
			t.Errorf("%s has no description — a ceiling nobody can explain is a number nobody can defend", n)
		}
		if c.Ceiling < 0 {
			t.Errorf("%s has a negative ceiling", n)
		}
	}
	if tv, _ := Class("tv"); tv.Ceiling != 0 {
		t.Errorf("tv caps at %d — a television is the one screen that wants everything the ladder has", tv.Ceiling)
	}
	if phone, _ := Class("phone"); phone.Ceiling != 720 {
		t.Errorf("phone caps at %d, want 720", phone.Ceiling)
	}
}

func TestParse_TheMixIsAnInputAndItHasToAddUp(t *testing.T) {
	mix, err := Parse("phone:50,tv:30,desktop:20")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(mix.Shares) != 3 {
		t.Fatalf("%d shares, want 3", len(mix.Shares))
	}
	if mix.Shares[0].Name != "phone" || mix.Shares[0].Percent != 50 {
		t.Errorf("first share = %+v", mix.Shares[0])
	}
	if mix.String() != "phone:50,tv:30,desktop:20" {
		t.Errorf("String() = %q — the report has to be able to say what it was asked for", mix.String())
	}

	for _, bad := range []string{
		"phone:50",             // adds up to half an audience
		"phone:50,tv:60",       // and this to more than one
		"phone:100,wardrobe:0", // a class that does not exist
		"phone",                // no share at all
		"phone:abc,tv:100",     // a share that is not a number
		"",                     // nothing
		"phone:-10,tv:110",     // a negative audience
	} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) was accepted — inventing an audience is inventing a measurement", bad)
		}
	}
}

func TestAssign_IsDeterministicStratifiedAndNotInOrder(t *testing.T) {
	mix, err := Parse("phone:50,tv:25,desktop:25")
	if err != nil {
		t.Fatal(err)
	}
	a, b := mix.Assign(40), mix.Assign(40)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("viewer %d is %q then %q — the same audience twice, or the report is not reproducible", i, a[i], b[i])
		}
	}

	// Stratified: 50% of forty viewers is twenty phones, not "about twenty".
	count := map[string]int{}
	for _, n := range a {
		count[n]++
	}
	if count["phone"] != 20 || count["tv"] != 10 || count["desktop"] != 10 {
		t.Errorf("the mix came out as %v over 40 viewers, want 20/10/10 — a share that only holds on average is a share nobody can check", count)
	}

	// And not handed out in blocks: somebody looking at the first ten viewers of
	// two hundred must not be looking at ten phones.
	first := a[0]
	same := 0
	for _, n := range a[:10] {
		if n == first {
			same++
		}
	}
	if same == 10 {
		t.Error("the first ten viewers are all the same device — the classes are being handed out in blocks")
	}
}

func TestAssign_AudiencesTooSmallToSplitStillGetSomebody(t *testing.T) {
	mix, _ := Parse("phone:50,tv:50")
	for _, n := range []int{1, 2, 3} {
		got := mix.Assign(n)
		if len(got) != n {
			t.Fatalf("Assign(%d) gave %d classes", n, len(got))
		}
		for i, name := range got {
			if _, ok := Class(name); !ok {
				t.Errorf("viewer %d of %d got %q, which is not a class", i, n, name)
			}
		}
	}
}
