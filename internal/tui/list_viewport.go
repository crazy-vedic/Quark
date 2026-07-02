package tui

import (
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/search"
)

type listViewport struct {
	Scroll      int
	SelectedRow int
	TotalRows   int
	VisibleRows int
	Direction   int
	LeadingPad  int
	TrailingPad int
}

func adjustListViewport(v listViewport) int {
	if v.VisibleRows < 1 {
		v.VisibleRows = 1
	}
	if v.TotalRows <= 0 {
		return 0
	}
	if v.SelectedRow < 0 {
		v.SelectedRow = 0
	}
	if v.SelectedRow >= v.TotalRows {
		v.SelectedRow = v.TotalRows - 1
	}

	maxScroll := max(0, v.TotalRows-v.VisibleRows)
	if v.Scroll > maxScroll {
		v.Scroll = maxScroll
	}
	if v.Scroll < 0 {
		v.Scroll = 0
	}

	leadingPad := max(0, v.LeadingPad)
	trailingPad := max(0, v.TrailingPad)
	if leadingPad+trailingPad >= v.VisibleRows {
		leadingPad = 0
		trailingPad = 0
	}

	switch {
	case v.Direction > 0 && v.SelectedRow >= v.Scroll+v.VisibleRows-trailingPad:
		v.Scroll = v.SelectedRow - (v.VisibleRows - trailingPad) + 1
	case v.Direction < 0 && v.SelectedRow < v.Scroll+leadingPad:
		v.Scroll = v.SelectedRow - leadingPad
	}

	if v.SelectedRow < v.Scroll {
		v.Scroll = v.SelectedRow
	}
	if v.SelectedRow >= v.Scroll+v.VisibleRows {
		v.Scroll = v.SelectedRow - v.VisibleRows + 1
	}

	if v.Scroll < 0 {
		v.Scroll = 0
	}
	if v.Scroll > maxScroll {
		v.Scroll = maxScroll
	}
	return v.Scroll
}

type helpRowKind int

const (
	helpGroupRow helpRowKind = iota
	helpSpacerRow
	helpBindingRow
)

type helpRow struct {
	kind  helpRowKind
	group string
	entry keybindings.Entry
}

func buildHelpRows(entries []keybindings.Entry, selectedEntry int) ([]helpRow, int) {
	rows := make([]helpRow, 0, len(entries)*2)
	selectedRow := 0
	var lastGroup string
	for i, entry := range entries {
		if entry.Group != lastGroup {
			if lastGroup != "" {
				rows = append(rows, helpRow{kind: helpSpacerRow})
			}
			rows = append(rows, helpRow{kind: helpGroupRow, group: entry.Group})
			lastGroup = entry.Group
		}
		rows = append(rows, helpRow{kind: helpBindingRow, entry: entry})
		if i == selectedEntry {
			selectedRow = len(rows) - 1
		}
	}
	return rows, selectedRow
}

type sidebarRowKind int

const (
	sidebarCollectionRow sidebarRowKind = iota
	sidebarRequestRow
)

type sidebarRow struct {
	kind     sidebarRowKind
	colIndex int
	reqIndex int
}

func (m Model) buildSidebarRows() ([]sidebarRow, int) {
	rows := make([]sidebarRow, 0, len(m.collections))
	selectedRow := 0
	for colIdx, col := range m.collections {
		rows = append(rows, sidebarRow{kind: sidebarCollectionRow, colIndex: colIdx, reqIndex: -1})
		if m.colCursor == colIdx && m.reqCursor == -1 {
			selectedRow = len(rows) - 1
		}
		if !m.expanded[col.ID] {
			continue
		}
		reqs := m.collectionRequests[col.ID]
		for reqIdx := range reqs {
			rows = append(
				rows,
				sidebarRow{kind: sidebarRequestRow, colIndex: colIdx, reqIndex: reqIdx},
			)
			if m.colCursor == colIdx && m.reqCursor == reqIdx {
				selectedRow = len(rows) - 1
			}
		}
	}
	return rows, selectedRow
}

type searchRow struct {
	hit *search.SearchHit
}

func buildSearchRows(hits []*search.SearchHit, selected int) ([]searchRow, int) {
	rows := make([]searchRow, 0, len(hits))
	for _, hit := range hits {
		rows = append(rows, searchRow{hit: hit})
	}
	if len(rows) == 0 {
		return rows, 0
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= len(rows) {
		selected = len(rows) - 1
	}
	return rows, selected
}

type envVarRow struct {
	variable envVar
}

func buildEnvVarRows(vars []envVar, selected int) ([]envVarRow, int) {
	rows := make([]envVarRow, 0, len(vars))
	for _, variable := range vars {
		rows = append(rows, envVarRow{variable: variable})
	}
	if len(rows) == 0 {
		return rows, 0
	}
	if selected < 0 {
		selected = 0
	}
	if selected >= len(rows) {
		selected = len(rows) - 1
	}
	return rows, selected
}
