package main

import "testing"

func TestMedianAndRatio(t *testing.T) {
	if got := median([]float64{9, 1, 5}); got != 5 {
		t.Fatalf("median=%v", got)
	}
	if got := ratio(0, 0); got != 1 {
		t.Fatalf("zero ratio=%v", got)
	}
	if got := summarize([]sample{{ns: 3, bytes: 2, allocs: 1}, {ns: 1, bytes: 4, allocs: 3}}); got.ns != 2 || got.bytes != 3 || got.allocs != 2 {
		t.Fatalf("summary=%+v", got)
	}
}
