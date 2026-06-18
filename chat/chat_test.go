package chat

import "testing"

func TestRoomAddAssignsSeqAndTrims(t *testing.T) {
	r := NewRoom()

	a := r.Add("alice", "hi")
	b := r.Add("bob", "yo")
	if a.Seq != 1 || b.Seq != 2 {
		t.Fatalf("seqs = %d, %d; want 1, 2", a.Seq, b.Seq)
	}
	if got := r.All(); len(got) != 2 || got[0].Text != "hi" || got[1].Text != "yo" {
		t.Fatalf("All() = %+v; want [hi, yo] in order", got)
	}

	// Push well past the cap; only the most recent maxHistory survive, and Seq
	// keeps climbing monotonically.
	for i := 0; i < maxHistory+50; i++ {
		r.Add("u", "m")
	}
	all := r.All()
	if len(all) != maxHistory {
		t.Fatalf("len(All()) = %d; want %d (capped)", len(all), maxHistory)
	}
	if last := all[len(all)-1]; last.Seq != r.seq {
		t.Fatalf("last.Seq = %d; want %d", last.Seq, r.seq)
	}
	if all[0].Seq >= all[len(all)-1].Seq {
		t.Fatalf("history not in ascending Seq order: %d..%d", all[0].Seq, all[len(all)-1].Seq)
	}
}
