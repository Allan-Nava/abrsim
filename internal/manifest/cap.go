package manifest

// CapHeight returns the ladder as one screen can use it: rungs taller than px are
// dropped, because past that height the extra pixels cost bytes and buy the viewer
// nothing (AB-40). px of 0 is no cap.
//
// Two rules that matter more than the filtering:
//
//   - **A rung whose height nobody declared is kept.** Unknown is not "too big",
//     and dropping a rung on suspicion silently edits somebody's ladder — the same
//     reasoning that keeps a variant with unrecognisable CODECS in the ladder.
//   - **The shortest rung always survives.** A phone against a ladder that starts
//     at 1080p would otherwise be reported as a stream nobody can watch, which is
//     a claim about the stream rather than about the screen. A viewer on a small
//     screen still watches; they just get more pixels than they can see.
//
// The receiver is not modified: a per-viewer cap must not edit the stream.
func (l Ladder) CapHeight(px int) Ladder {
	if px <= 0 || len(l.Renditions) == 0 {
		return l
	}
	out := Ladder{Source: l.Source}
	for _, r := range l.Renditions {
		if r.Height == 0 || r.Height <= px {
			out.Renditions = append(out.Renditions, r)
		}
	}
	if len(out.Renditions) == 0 {
		// Renditions are sorted ascending, so the first is the shortest.
		out.Renditions = append(out.Renditions, l.Renditions[0])
	}
	return out
}
