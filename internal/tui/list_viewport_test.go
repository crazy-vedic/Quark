package tui

import "testing"

func TestAdjustListViewport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   listViewport
		want int
	}{
		{
			name: "empty rows reset scroll",
			in: listViewport{
				Scroll:      5,
				SelectedRow: 2,
				TotalRows:   0,
				VisibleRows: 4,
			},
			want: 0,
		},
		{
			name: "negative values clamp to zero",
			in: listViewport{
				Scroll:      -3,
				SelectedRow: -2,
				TotalRows:   6,
				VisibleRows: 3,
			},
			want: 0,
		},
		{
			name: "selected row beyond bounds clamps to last visible page",
			in: listViewport{
				Scroll:      0,
				SelectedRow: 99,
				TotalRows:   6,
				VisibleRows: 3,
			},
			want: 3,
		},
		{
			name: "oversized padding is ignored",
			in: listViewport{
				Scroll:      0,
				SelectedRow: 2,
				TotalRows:   8,
				VisibleRows: 2,
				Direction:   1,
				LeadingPad:  4,
				TrailingPad: 4,
			},
			want: 1,
		},
		{
			name: "downward motion respects trailing pad",
			in: listViewport{
				Scroll:      0,
				SelectedRow: 4,
				TotalRows:   10,
				VisibleRows: 5,
				Direction:   1,
				LeadingPad:  1,
				TrailingPad: 1,
			},
			want: 1,
		},
		{
			name: "upward motion respects leading pad",
			in: listViewport{
				Scroll:      4,
				SelectedRow: 4,
				TotalRows:   10,
				VisibleRows: 5,
				Direction:   -1,
				LeadingPad:  1,
				TrailingPad: 1,
			},
			want: 3,
		},
		{
			name: "scroll clamps to max page after adjustment",
			in: listViewport{
				Scroll:      20,
				SelectedRow: 5,
				TotalRows:   8,
				VisibleRows: 3,
			},
			want: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := adjustListViewport(tt.in); got != tt.want {
				t.Fatalf("adjustListViewport(%+v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
