# Keyboard Text Selection Implementation - Handoff Document

**Branch:** `feature/keyboard-text-selection`  
**Issue:** #48 - Keyboard word navigation and selection in text input fields  
**Status:** Phase 1-7 Complete, Ready for Extension  
**Last Updated:** November 15, 2026

---

## Quick Start

This implementation adds keyboard text selection to Quark's text input fields, matching standard editor behavior (VS Code, Postman, browsers). All phases are complete and tested.

**What works now:**
- ✅ Full keyboard selection in **URL field** (Shift+arrows, Ctrl+A, Ctrl+C/X, double-click)
- ✅ Selection rendering with visual feedback
- ✅ Clipboard operations (copy/cut/paste)
- ✅ Double-click word selection
- ✅ Configurable keybindings
- ✅ 50+ tests passing, zero regressions

**What's next:**
- Extend selection handlers to body, headers, auth, search, import fields
- Follow the **"Extension Pattern"** (section below)

---

## Architecture Overview

### Core Components

| File | Purpose | Status |
|------|---------|--------|
| `internal/tui/selection.go` | textSelection struct (anchor/cursor positions) | ✅ Complete |
| `internal/tui/word_bounds.go` | Word boundary detection & rune/byte conversions | ✅ Complete |
| `internal/tui/selection_keys.go` | handleSelectionKeyForInput() key routing | ✅ Complete |
| `internal/tui/model.go` | Selection state fields for all editors + renderInputWithSelection() | ✅ Complete |
| `internal/tui/update.go` | Phase 2 integration: URL field handler + copy/cut/paste | ✅ Complete |
| `internal/tui/view.go` | Phase 3 rendering: renderInputWithSelection() usage for URL | ✅ Complete |
| `internal/tui/mouse_editors.go` | Phase 5: Double-click word selection in URL field | ✅ Complete |
| `internal/keybindings/actions.go` | Phase 6: Selection action definitions | ✅ Complete |
| `internal/keybindings/defaults.go` | Phase 6: Default keybinding mappings | ✅ Complete |
| `internal/tui/keyboard_selection_integration_test.go` | E2E tests (all phases) | ✅ Complete |
| `internal/tui/selection_keys_test.go` | Unit tests for key handling | ✅ Complete (40+ tests) |
| `README.md` | Phase 7: User documentation | ✅ Complete |
| `KEYBOARD_SELECTION_PROGRESS.md` | Detailed phase tracking | ✅ Complete |

### Design Principles

1. **Rune-based offsets**: All selection logic uses rune offsets (not byte offsets) for correct Unicode handling. This is why `byteOffsetToRuneOffset()` and `runeOffsetToByteOffset()` converters exist.

2. **No bubbles fork**: Rather than forking the bubbles textinput/textarea components, we intercept keys in `update.go` **before** passing them to bubbles. This keeps dependencies clean.

3. **Per-field selection state**: Each text editor has its own `textSelection` field in `Model` (lines ~353-365 in model.go). They're independent and managed separately.

4. **Word definitions**: Words are sequences of letters, numbers, underscores, hyphens (liberal definition that works for HTTP headers, JSON, env vars, URLs).

5. **Graceful degradation**: Clipboard operations fail silently with error notifications on SSH/Wayland. Terminal key support varies; fallback shortcuts documented.

---

## How Selection Currently Works (URL Field Example)

### 1. **User presses Shift+Left**
   - `update.go` line ~965: URL field handler intercepts before bubbles
   - Calls `handleSelectionKeyForInput(msg, &m.urlSelection, &m.urlInput, m.urlInput.Position())`
   - Returns `handled=true, shouldUpdateComponent=false`
   - Input cursor position updated to match selection cursor

### 2. **User presses regular character**
   - Selection key handler returns `handled=false, shouldUpdateComponent=true`
   - Bubbles textinput processes the key normally
   - After bubbles updates, selection is cleared (`m.urlSelection.Clear(m.urlInput.Position())`)

### 3. **User presses Ctrl+C**
   - Before other keys, copy handler checks for selection
   - Extracts text via `copySelectionText(m.urlInput.Value(), m.urlSelection)`
   - Returns `copyToClipboardCmd(text)` which returns a tea.Cmd
   - Clipboard error handler shows toast notification

### 4. **Rendering selected text**
   - In `view.go` line ~507: `m.renderInputWithSelection(m.urlInput.Value(), m.urlSelection)`
   - This function (model.go ~1451-1480) builds the output with ANSI styling:
     - Unselected text: plain
     - Selected text: wrapped with `m.styles.selectionStyle.Render()`
     - Returns styled string to display

---

## Extension Pattern: Adding Selection to Other Fields

When extending to body/headers/auth/search/import/env fields, follow this exact pattern:

### Step 1: Wire Into Key Handler

Find the field's key handler in `update.go`. For example, `handleBodyFieldKey()` is at line ~2033.

**Add these checks at the START of the handler:**

```go
case bodyField:
    // Handle copy/cut before selection keys
    if msg.Type == tea.KeyCtrlC {
        text := copySelectionText(m.bodyTextarea.Value(), m.bodySelection)
        if text != "" {
            return m, copyToClipboardCmd(text)
        }
        return m, nil
    }
    if msg.Type == tea.KeyCtrlX {
        newValue, newCursorPos := cutSelection(m.bodyTextarea.Value(), m.bodySelection)
        m.bodyTextarea.SetValue(newValue)
        m.bodyTextarea.SetCursor(newCursorPos)
        m.bodySelection.Clear(newCursorPos)
        return m, nil
    }

    // Handle selection keys
    handled, shouldUpdateComponent := handleSelectionKeyForInput(
        msg, 
        &m.bodySelection, 
        m.bodyTextarea, // Note: textarea instead of textinput
        m.bodyTextarea.Cursor(), // or .Position() depending on component
    )
    if handled && !shouldUpdateComponent {
        // Update cursor to match selection
        _, selEnd := m.bodySelection.Range()
        m.bodyTextarea.SetCursor(selEnd)
        return m, nil
    }
    
    // ... rest of handler
```

### Step 2: Add Rendering in view.go

Find where the field is rendered. For body, it's around line ~567:

```go
// OLD:
bodyDisplay := m.bodyTextarea.View()

// NEW:
bodyDisplay := m.renderInputWithSelection(m.bodyTextarea.Value(), m.bodySelection)
```

### Step 3: Clear Selection on Field Exit

In `request_editors.go`, add selection clearing to `cancelActiveFieldEdit()`:

```go
case bodyField:
    m.activeField = noneField
    m.bodyTextarea.Blur()
    m.bodyTextarea.SetValue(m.preEditBody)
    m.bodySelection.Clear(0)  // ← Add this line
```

### Step 4: Add Double-Click Handler (if applicable)

In `mouse_editors.go`, add double-click word selection like the URL field (lines ~95-104):

```go
if m.isDoubleTextClick(msg, "request-body") {  // Use unique source string
    runes := stringToRunes(m.bodyTextarea.Value())
    start, end := wordBoundsAt(runes, cursorPos)
    m.bodySelection.SelectWord(start, end)
    m.bodyTextarea.SetCursor(end)
}
```

### Step 5: Add Tests

Add integration test to `keyboard_selection_integration_test.go`:

```go
func TestBodyFieldKeyboardSelection(t *testing.T) {
    m := newModel(defaultConfig()).
        WithFocus(tui.RequestPane).
        WithActiveField(tui.BodyField).
        WithBodyValue("line1\nline2\nline3")

    // Test Ctrl+A
    m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
    sel := m.BodySelection()
    start, end := sel.Range()
    assert.Equal(t, 0, start)
    assert.Equal(t, len([]rune("line1\nline2\nline3")), end)
}
```

---

## Key Functions Reference

### Core Selection API

| Function | File | Purpose |
|----------|------|---------|
| `handleSelectionKeyForInput()` | selection_keys.go:25 | Main key handler (Shift+arrows, Ctrl+A, etc.) |
| `copySelectionText()` | selection_keys.go:151 | Extract selected text |
| `cutSelection()` | selection_keys.go:165 | Remove selected text, return new value |
| `wordBoundsAt()` | word_bounds.go:16 | Find word start/end at position |
| `byteOffsetToRuneOffset()` | word_bounds.go:72 | Convert byte offset to rune offset |
| `runeOffsetToByteOffset()` | word_bounds.go:82 | Convert rune offset to byte offset |
| `renderInputWithSelection()` | model.go:1451 | Render text with selection styling |
| `copyToClipboardCmd()` | update.go:2593 | Return tea.Cmd for clipboard write |

### Selection State Methods (selection.go)

| Method | Purpose |
|--------|---------|
| `Active()` | Returns true if anchor != cursor |
| `Range()` | Returns normalized [start, end) tuple |
| `Clear(pos)` | Sets anchor and cursor to same position |
| `Extend(newCursor)` | Moves cursor without moving anchor |
| `SelectAll(length)` | Sets selection to span entire value |
| `SelectWord(start, end)` | Sets selection to word bounds |

---

## Testing

### Run All Tests
```bash
go test ./internal/tui -race -v
```

### Run Only Selection Tests
```bash
go test ./internal/tui -run "Selection\|EndToEnd\|KeyboardNavigation" -v
```

### Run With Coverage
```bash
go test ./internal/tui -cover
```

**Current Status:** 50+ tests passing, 100% pass rate with -race flag

---

## Known Issues & Workarounds

### Issue 1: Terminal Key Support
**Problem:** Ctrl+Shift+Left/Right (word selection) doesn't work on all terminals.  
**Mitigation:** Defaults use Alt+Shift+Left/Right as fallback. Document in README which terminals work best.  
**Workaround:** User can configure custom keybindings in `~/.quark/keybindings.toml`.

### Issue 2: Clipboard on SSH/Wayland
**Problem:** `atotto/clipboard` fails silently on headless systems.  
**Mitigation:** Clipboard errors return `clipboardErrorMsg` which shows toast notification instead of crashing.  
**Workaround:** Document in README that clipboard may not work on remote systems.

### Issue 3: Wrapped Textarea Selection Rendering
**Problem:** Body textarea wraps at terminal width; selection rendering across wrapped lines looks odd.  
**Mitigation:** Not yet implemented. Current approach: simple background overlay (same as viewer match highlighting).  
**Future:** May need custom textarea renderer if overlay fails.

### Issue 4: Selection Persistence
**Problem:** When user switches collections/requests, selection state persists in Model.  
**Mitigation:** Selection is cleared when `activeField` changes (in `cancelActiveFieldEdit()`).  
**Note:** Currently working as designed; no bug.

---

## Configuration & Customization

### Default Keybindings

All defined in `internal/keybindings/defaults.go` (~line 128):

```go
SelectCharLeft:  "shift+left",
SelectCharRight: "shift+right",
SelectWordLeft:  "alt+shift+left",
SelectWordRight: "alt+shift+right",
SelectLineStart: "shift+home",
SelectLineEnd:   "shift+end",
SelectAll:       "ctrl+a",
Copy:            "ctrl+c",
Cut:             "ctrl+x",
```

### User Customization

Users can override in `~/.quark/keybindings.toml`:

```toml
[keybindings]
select_char_left = "shift+left"
copy = "ctrl+c"
```

All action names defined in `internal/keybindings/actions.go` (~line 29).

---

## File Structure

```
feature/keyboard-text-selection/
├── internal/tui/
│   ├── selection.go (44 lines) - Core struct
│   ├── selection_test.go (276 lines) - Unit tests
│   ├── word_bounds.go (153 lines) - Helpers
│   ├── word_bounds_test.go (383 lines) - Unit tests
│   ├── selection_keys.go (176 lines) - Key handler
│   ├── selection_keys_test.go (213 lines) - Unit tests
│   ├── model.go (selected lines) - Selection fields + renderInputWithSelection()
│   ├── update.go (selected lines) - Phase 2-4 integration
│   ├── view.go (line ~507) - Phase 3 rendering
│   ├── mouse_editors.go (lines ~91-107) - Phase 5 double-click
│   ├── request_editors.go (selected lines) - Selection clearing
│   ├── test_api.go (selected lines) - Selection getters
│   ├── keyboard_selection_integration_test.go (140+ lines) - E2E tests
│   ├── theme.go (selected lines) - Selection color
│
├── internal/keybindings/
│   ├── actions.go (line ~29) - Action definitions
│   ├── defaults.go (line ~128) - Default keybindings
│   ├── resolver.go (line ~125) - Keybindings struct fields
│
├── README.md (section added) - User documentation
├── KEYBOARD_SELECTION_PROGRESS.md - Detailed progress
├── HANDOFF.md - This file
```

---

## Common Tasks

### Add Selection to a New Field

Follow the **Extension Pattern** section above. Typical effort: 30 minutes.

### Test a Field After Adding Selection

1. Build: `go build ./cmd/quark`
2. Run: `./quark`
3. Navigate to field (e.g., press `u` for URL, `b` for body)
4. Try: Shift+Left/Right, Ctrl+A, Ctrl+C, double-click
5. Test exit: Press Esc, verify selection clears
6. Add test case to `keyboard_selection_integration_test.go`

### Fix a Linting Error

Most are cosmetic (gci formatting). Run:
```bash
make lint
```

For import ordering, ensure this order:
1. Standard library (fmt, strings, time, etc.)
2. Third-party (github.com/charmbracelet/*, github.com/google/*)
3. Local imports (github.com/crazy-vedic/quark/internal/*)

### Debug Selection State

Add this to update.go temporarily:

```go
if msg.Type == tea.KeyCtrlD {  // Debug key
    log.Printf("Selection: active=%v, range=[%d,%d], value=%q",
        m.urlSelection.Active(),
        m.urlSelection.anchor, m.urlSelection.cursor,
        m.urlInput.Value())
}
```

---

## Git History

17 commits on `feature/keyboard-text-selection`:

1. **e1371db** - textSelection model
2. **bfba729** - Word boundary helpers
3. **2917750** - Selection state to Model + theme
4. **45d7a9a** - Selection-aware key handling
5. **5f25157** - Integration test skeletons
6. **e7247d6** - Fix Unicode test
7. **4c7efbc** - ✨ Phase 2: Update loop integration
8. **38505e1** - ✨ Phase 3: Selection rendering
9. **02a5617** - Fix gocritic warning
10. **6bc8aa0** - ✨ Phase 4: Copy/Cut/Paste
11. **049569c** - ✨ Phase 5: Double-click
12. **bb22955** - ✨ Phase 6: Keybindings config
13. **0258e6d** - ✨ Phase 7: README docs
14. **388900f** - E2E comprehensive tests
15. **7be230f** - Mark completion
16. **390670c** - Fix linting
17. **d96464a** - Final linting fixes

---

## Next Steps (Priority Order)

### 1. Extend to Body Field (Medium Effort)
- Add key handler in `handleBodyFieldKey()` (update.go ~line 2033)
- Add rendering in view.go (~line 567)
- Add double-click in mouse.go
- Add integration test
- Verify selection rendering in wrapped textarea

### 2. Extend to Header Fields (Medium Effort)
- Headers have both key and value inputs
- Add handlers in `handleHeadersFieldKey()` (update.go ~line 2148)
- Add rendering for both fields
- Add double-click
- Add tests

### 3. Extend to Auth Field (Medium Effort)
- Auth has multiple sub-fields (token, username, password, API key)
- Follow same pattern for each
- Add rendering for active sub-field only

### 4. Extend to Search & Import Fields (Low Effort)
- Search input is single-line
- Import textarea is wrapped
- Use same pattern

### 5. Terminal Compatibility Testing
- Test on: macOS Terminal, iTerm2, GNOME Terminal, Windows Terminal, SSH
- Document which keys work on which terminals
- Update README with matrix

### 6. Polish & Edge Cases
- Test with long URLs (wrapping)
- Test with emoji and CJK characters
- Test clipboard on different systems
- Test keybinding conflicts

---

## References

- **Issue:** #48 on GitHub (Keyboard word navigation)
- **Design Doc:** KEYBOARD_SELECTION_PROGRESS.md (comprehensive architecture)
- **Tests:** keyboard_selection_integration_test.go (e2e examples)
- **User Guide:** README.md section "Text Selection & Editing"

---

## Contact & Questions

If stuck:
1. Check KEYBOARD_SELECTION_PROGRESS.md (architecture section)
2. Look at URL field implementation (complete, tested example)
3. Review unit tests in selection_keys_test.go (all scenarios covered)
4. Run with debug output (add log statements)

Good luck! The foundation is solid and extensible. 🚀
