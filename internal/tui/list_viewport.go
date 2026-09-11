package tui

import (
	"sort"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
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
	depth    int
}

func (m Model) buildSidebarRows() ([]sidebarRow, int) {
	rows := make([]sidebarRow, 0, len(m.collections))
	selectedRow := 0
	children := make(map[string][]int, len(m.collections))
	byID := make(map[string]*domain.Collection, len(m.collections))
	roots := make([]int, 0, len(m.collections))
	for i, col := range m.collections {
		if col != nil {
			byID[col.ID] = col
		}
		if col == nil || col.ParentID == "" {
			roots = append(roots, i)
			continue
		}
		children[col.ParentID] = append(children[col.ParentID], i)
	}
	byName := func(indices []int) {
		sort.SliceStable(indices, func(i, j int) bool {
			return m.collections[indices[i]].Name < m.collections[indices[j]].Name
		})
	}
	byName(roots)
	for _, indices := range children {
		byName(indices)
	}
	var appendCollection func(int, int, map[int]bool)
	appendCollection = func(colIdx, depth int, visiting map[int]bool) {
		if colIdx < 0 || colIdx >= len(m.collections) || visiting[colIdx] {
			return
		}
		visiting[colIdx] = true
		col := m.collections[colIdx]
		rows = append(
			rows,
			sidebarRow{kind: sidebarCollectionRow, colIndex: colIdx, reqIndex: -1, depth: depth},
		)
		if m.colCursor == colIdx && m.reqCursor == -1 {
			selectedRow = len(rows) - 1
		}
		if m.expanded[col.ID] {
			reqs := m.collectionRequests[col.ID]
			for reqIdx := range reqs {
				rows = append(
					rows,
					sidebarRow{
						kind:     sidebarRequestRow,
						colIndex: colIdx,
						reqIndex: reqIdx,
						depth:    depth + 1,
					},
				)
				if m.colCursor == colIdx && m.reqCursor == reqIdx {
					selectedRow = len(rows) - 1
				}
			}
			for _, childIdx := range children[col.ID] {
				appendCollection(childIdx, depth+1, visiting)
			}
		}
		delete(visiting, colIdx)
	}
	for _, colIdx := range roots {
		appendCollection(colIdx, 0, make(map[int]bool))
	}
	// Broken or cyclic parent references should remain visible instead of being
	// silently dropped from the sidebar. Legitimate descendants hidden by a
	// collapsed ancestor are deliberately excluded from this fallback.
	for colIdx := range m.collections {
		// A valid child whose parent is collapsed is intentionally absent from
		// rows; it must not be re-added by the malformed-tree fallback below.
		parentExists := false
		if m.collections[colIdx] != nil && m.collections[colIdx].ParentID != "" {
			for _, parent := range m.collections {
				if parent != nil && parent.ID == m.collections[colIdx].ParentID {
					parentExists = true
					break
				}
			}
		}
		if parentExists {
			continue
		}
		found := false
		for _, row := range rows {
			if row.kind == sidebarCollectionRow && row.colIndex == colIdx {
				found = true
				break
			}
		}
		if !found && hasBrokenOrCyclicCollectionAncestry(m.collections[colIdx], byID) {
			appendCollection(colIdx, 0, make(map[int]bool))
		}
	}
	return rows, selectedRow
}

// collapseCollectionSubtree closes a collection and all of its descendants.
// Descendants may be hidden while their parent is collapsed, so walking the
// collection tree rather than only the visible rows is intentional. Request
// data remains cached so reopening a collection does not require a reload.
func (m Model) collapseCollectionSubtree(collectionID string) {
	children := make(map[string][]string, len(m.collections))
	for _, col := range m.collections {
		if col != nil && col.ParentID != "" {
			children[col.ParentID] = append(children[col.ParentID], col.ID)
		}
	}

	stack := []string{collectionID}
	seen := make(map[string]bool)
	for len(stack) > 0 {
		last := len(stack) - 1
		id := stack[last]
		stack = stack[:last]
		if seen[id] {
			continue
		}
		seen[id] = true
		m.expanded[id] = false
		stack = append(stack, children[id]...)
	}
}

func hasBrokenOrCyclicCollectionAncestry(
	col *domain.Collection,
	byID map[string]*domain.Collection,
) bool {
	seen := make(map[string]bool)
	for col != nil && col.ParentID != "" {
		if seen[col.ID] {
			return true
		}
		seen[col.ID] = true
		parent, ok := byID[col.ParentID]
		if !ok {
			return true
		}
		col = parent
	}
	return false
}

func orderCollectionsTree(collections []*domain.Collection) []*domain.Collection {
	if len(collections) < 2 {
		return collections
	}
	children := make(map[string][]*domain.Collection, len(collections))
	roots := make([]*domain.Collection, 0, len(collections))
	byID := make(map[string]*domain.Collection, len(collections))
	for _, col := range collections {
		if col == nil {
			continue
		}
		byID[col.ID] = col
	}
	for _, col := range collections {
		if col == nil || col.ParentID == "" || byID[col.ParentID] == nil {
			roots = append(roots, col)
		} else {
			children[col.ParentID] = append(children[col.ParentID], col)
		}
	}
	byName := func(cols []*domain.Collection) {
		sort.SliceStable(cols, func(i, j int) bool { return cols[i].Name < cols[j].Name })
	}
	byName(roots)
	for _, cols := range children {
		byName(cols)
	}
	ordered := make([]*domain.Collection, 0, len(collections))
	var visit func(*domain.Collection, map[string]bool)
	visit = func(col *domain.Collection, visiting map[string]bool) {
		if col == nil || visiting[col.ID] {
			return
		}
		visiting[col.ID] = true
		ordered = append(ordered, col)
		for _, child := range children[col.ID] {
			visit(child, visiting)
		}
		delete(visiting, col.ID)
	}
	for _, root := range roots {
		visit(root, make(map[string]bool))
	}
	for _, col := range collections {
		found := false
		for _, existing := range ordered {
			if existing == col {
				found = true
				break
			}
		}
		if !found {
			visit(col, make(map[string]bool))
		}
	}
	return ordered
}

type searchRow struct {
	kind     searchRowKind
	group    string
	hit      *search.SearchHit
	hitIndex int
}

type searchRowKind int

const (
	searchGroupRow searchRowKind = iota
	searchSpacerRow
	searchRequestRow
)

func (m Model) buildSearchRows(hits []*search.SearchHit, selected int) ([]searchRow, int) {
	rows := make([]searchRow, 0, len(hits)*2)
	if len(hits) == 0 {
		return rows, 0
	}

	groups := make([]string, 0)
	groupSeen := make(map[string]bool)
	groupForHit := make([]string, len(hits))
	for i, hit := range hits {
		group := m.searchHitTopLevelGroup(hit)
		groupForHit[i] = group
		if !groupSeen[group] {
			groupSeen[group] = true
			groups = append(groups, group)
		}
	}

	selected = max(0, min(selected, len(hits)-1))
	selectedRow := 0
	for groupIndex, group := range groups {
		if groupIndex > 0 {
			rows = append(rows, searchRow{kind: searchSpacerRow})
		}
		rows = append(rows, searchRow{kind: searchGroupRow, group: group})
		for hitIndex, hit := range hits {
			if groupForHit[hitIndex] != group {
				continue
			}
			rowIndex := len(rows)
			rows = append(rows, searchRow{
				kind:     searchRequestRow,
				hit:      hit,
				hitIndex: hitIndex,
			})
			if hitIndex == selected {
				selectedRow = rowIndex
			}
		}
	}
	return rows, selectedRow
}

func (m Model) searchHitTopLevelGroup(hit *search.SearchHit) string {
	if hit == nil || hit.Request == nil {
		return "Other requests"
	}
	collectionID := hit.Request.CollectionID
	if collectionID == "" && hit.Collection != nil {
		collectionID = hit.Collection.ID
	}

	byID := make(map[string]*domain.Collection, len(m.collections))
	for _, col := range m.collections {
		if col != nil {
			byID[col.ID] = col
		}
	}
	current := byID[collectionID]
	seen := make(map[string]bool)
	for current != nil && current.ParentID != "" && !seen[current.ID] {
		seen[current.ID] = true
		parent := byID[current.ParentID]
		if parent == nil {
			break
		}
		current = parent
	}
	if current != nil && strings.TrimSpace(current.Name) != "" {
		return current.Name
	}
	if hit.Collection != nil && strings.TrimSpace(hit.Collection.Name) != "" {
		return hit.Collection.Name
	}
	return "Other requests"
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
