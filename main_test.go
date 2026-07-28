package main

import "testing"

func TestListWindow(t *testing.T) {
	cases := []struct {
		name                  string
		total, cursor, height int
		wantStart, wantEnd    int
	}{
		{"tall terminal shows everything", 5, 0, 40, 0, 5},
		{"unknown height shows everything", 5, 4, 0, 0, 5},
		{"cursor pulls window down", 20, 10, 24, 1, 11},
		{"cursor at top keeps window at top", 20, 0, 24, 0, 10},
		{"cursor at bottom pins window to end", 20, 19, 24, 10, 20},
		{"short terminal keeps a 3-row minimum", 20, 0, 10, 0, 3},
		{"minimum never exceeds a short list", 2, 0, 10, 0, 2},
		{"single cluster in a short terminal", 1, 0, 10, 0, 1},
		{"cursor on last of a short list", 2, 1, 10, 0, 2},
		{"empty list", 0, 0, 15, 0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := listWindow(tc.total, tc.cursor, tc.height)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("listWindow(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tc.total, tc.cursor, tc.height, start, end, tc.wantStart, tc.wantEnd)
			}
			// the window is sliced straight into the cluster list — out-of-range panics the TUI
			if start < 0 || end > tc.total || start > end {
				t.Fatalf("window (%d, %d) out of range for %d items", start, end, tc.total)
			}
			if tc.cursor < tc.total && (tc.cursor < start || tc.cursor >= end) {
				t.Fatalf("cursor %d not visible in window (%d, %d)", tc.cursor, start, end)
			}
		})
	}
}
