package main

import "testing"

func TestTwinIndex(t *testing.T) {
	w := "jashwanth-s-macbook-pro-sample1-night-test-meshy-eth"
	for label, want := range map[string]int{w: 0, w + "-1": 1, w + "-12": 12} {
		if n, ok := twinIndex(label, w); !ok || n != want {
			t.Fatalf("%s → %d %v", label, n, ok)
		}
	}
	for _, bad := range []string{w + "-x", w + "-", "jashwanth-s-macbook-pro-sample1-night-test-meshy-eth2", "other"} {
		if _, ok := twinIndex(bad, w); ok {
			t.Fatalf("%s should not match", bad)
		}
	}
}
