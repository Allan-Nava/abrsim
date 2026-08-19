package manifest

import "testing"

func TestCapHeight_DropsWhatTheScreenCannotShow(t *testing.T) {
	l := Ladder{Source: "test://x", Renditions: []Rendition{
		{Name: "360p", Bandwidth: 800_000, Height: 360},
		{Name: "720p", Bandwidth: 2_500_000, Height: 720},
		{Name: "1080p", Bandwidth: 5_000_000, Height: 1080},
	}}
	got := l.CapHeight(720)
	if len(got.Renditions) != 2 {
		t.Fatalf("%d rungs left, want 2: %+v", len(got.Renditions), got.Renditions)
	}
	if got.Renditions[1].Name != "720p" {
		t.Errorf("the tallest rung left is %s, want 720p", got.Renditions[1].Name)
	}
	if len(l.Renditions) != 3 {
		t.Error("CapHeight modified the ladder it was given: a per-viewer cap must not edit the stream")
	}
}

func TestCapHeight_ZeroIsNoCapAndUnknownIsNotTooBig(t *testing.T) {
	l := Ladder{Renditions: []Rendition{
		{Name: "360p", Height: 360},
		{Name: "unknown", Height: 0},
		{Name: "1080p", Height: 1080},
	}}
	if got := l.CapHeight(0); len(got.Renditions) != 3 {
		t.Errorf("a ceiling of 0 dropped %d rungs — 0 means no cap", 3-len(got.Renditions))
	}
	got := l.CapHeight(360)
	if len(got.Renditions) != 2 {
		t.Fatalf("%d rungs left, want 2 — a rung whose height nobody declared is kept, because unknown is not too big", len(got.Renditions))
	}
	if got.Renditions[1].Name != "unknown" {
		t.Errorf("the kept rung is %q", got.Renditions[1].Name)
	}
}

func TestCapHeight_NeverEmptiesTheLadder(t *testing.T) {
	// A phone against a ladder that starts at 1080p: capping everything away
	// would report a stream nobody can watch, which is a lie about the stream.
	// The shortest rung survives — a viewer on a small screen still watches.
	l := Ladder{Renditions: []Rendition{
		{Name: "1080p", Bandwidth: 5_000_000, Height: 1080},
		{Name: "2160p", Bandwidth: 15_000_000, Height: 2160},
	}}
	got := l.CapHeight(720)
	if len(got.Renditions) != 1 || got.Renditions[0].Name != "1080p" {
		t.Errorf("capping left %+v, want just the shortest rung", got.Renditions)
	}
}
