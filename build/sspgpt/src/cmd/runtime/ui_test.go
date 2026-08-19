package main

import "testing"

func TestInputHeightForLineCount(t *testing.T) {
	tests := []struct {
		lines int
		want  int
	}{
		{0, 45},
		{1, 45},
		{2, 45},
		{3, 69},
		{8, 189},
		{20, 189},
	}
	for _, tt := range tests {
		if got := inputHeightForLineCount(tt.lines); got != tt.want {
			t.Fatalf("lines=%d: got %d want %d", tt.lines, got, tt.want)
		}
	}
}
