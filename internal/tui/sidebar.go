package tui

import (
	"sort"

	"github.com/crazy-vedic/quark/internal/domain"
)

// sidebarState is the collection tree the sidebar shows: the loaded collections,
// which ones are expanded, the cached requests per collection, and the cursor and
// scroll offset. Model holds one as m.sidebar.
type sidebarState struct {
	collections []*domain.Collection
	colCursor   int
	expanded    map[string]bool // collectionID → expanded
	requests    []*domain.Request
	reqCursor   int
	offset      int // BUG-011: scroll offset for long collection lists
	// collectionRequests stores loaded requests for all expanded collections.
	// Key is collection ID; value is the list of requests.
	collectionRequests      map[string][]*domain.Request
	initialSelectionPending bool
}

// selectedCollection returns the collection currently under the cursor, or nil.
func (s sidebarState) selectedCollection() *domain.Collection {
	if len(s.collections) == 0 || s.colCursor >= len(s.collections) {
		return nil
	}
	return s.collections[s.colCursor]
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

func (s sidebarState) buildRows() ([]sidebarRow, int) {
	rows := make([]sidebarRow, 0, len(s.collections))
	selectedRow := 0
	children := make(map[string][]int, len(s.collections))
	byID := make(map[string]*domain.Collection, len(s.collections))
	roots := make([]int, 0, len(s.collections))
	for i, col := range s.collections {
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
			return s.collections[indices[i]].Name < s.collections[indices[j]].Name
		})
	}
	byName(roots)
	for _, indices := range children {
		byName(indices)
	}
	var appendCollection func(int, int, map[int]bool)
	appendCollection = func(colIdx, depth int, visiting map[int]bool) {
		if colIdx < 0 || colIdx >= len(s.collections) || visiting[colIdx] {
			return
		}
		visiting[colIdx] = true
		col := s.collections[colIdx]
		rows = append(
			rows,
			sidebarRow{kind: sidebarCollectionRow, colIndex: colIdx, reqIndex: -1, depth: depth},
		)
		if s.colCursor == colIdx && s.reqCursor == -1 {
			selectedRow = len(rows) - 1
		}
		if s.expanded[col.ID] {
			reqs := s.collectionRequests[col.ID]
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
				if s.colCursor == colIdx && s.reqCursor == reqIdx {
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
	for colIdx := range s.collections {
		// A valid child whose parent is collapsed is intentionally absent from
		// rows; it must not be re-added by the malformed-tree fallback below.
		parentExists := false
		if s.collections[colIdx] != nil && s.collections[colIdx].ParentID != "" {
			for _, parent := range s.collections {
				if parent != nil && parent.ID == s.collections[colIdx].ParentID {
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
		if !found && hasBrokenOrCyclicCollectionAncestry(s.collections[colIdx], byID) {
			appendCollection(colIdx, 0, make(map[int]bool))
		}
	}
	return rows, selectedRow
}

// collapseSubtree closes a collection and all of its descendants.
// Descendants may be hidden while their parent is collapsed, so walking the
// collection tree rather than only the visible rows is intentional. Request
// data remains cached so reopening a collection does not require a reload.
func (s sidebarState) collapseSubtree(collectionID string) {
	children := make(map[string][]string, len(s.collections))
	for _, col := range s.collections {
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
		s.expanded[id] = false
		stack = append(stack, children[id]...)
	}
}

// selectRow moves the cursor onto row and reloads the cached requests of its collection.
func (s sidebarState) selectRow(row sidebarRow) sidebarState {
	s.colCursor = row.colIndex
	s.reqCursor = -1
	if row.kind == sidebarRequestRow {
		s.reqCursor = row.reqIndex
	}
	if row.colIndex >= 0 && row.colIndex < len(s.collections) {
		s.requests = s.collectionRequests[s.collections[row.colIndex].ID]
	}
	return s
}
