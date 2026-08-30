package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// lipgloss pads every line of a rendered block to the block's width, so a
// newline inside Render appends a run of trailing spaces:
//
//	styFaint.Render("hello\n")  =>  "hello\n     "
//
// Those spaces shift whatever is written next, which silently wrecks the
// alignment of anything built by appending to a strings.Builder. It has bitten
// this package twice, so the pattern is banned rather than remembered.
func TestNoNewlinesInsideRender(t *testing.T) {
	// Confirm the underlying behavior still exists; if lipgloss ever changes
	// it, this test should be revisited rather than silently kept.
	if got := stripANSI(styFaint.Render("hello\n")); got == "hello\n" {
		t.Skip("lipgloss no longer pads trailing lines; this guard can be removed")
	}

	pattern := regexp.MustCompile(`\.Render\([^)]*\\n`)

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if pattern.MatchString(line) {
				t.Errorf("%s:%d: newline inside Render(); put it outside:\n\t%s",
					file, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// The layout contract, asserted on every screen and every dialog: a renderer
// returns exactly the block it was asked for (whis SYSTEM_DESIGN.md §5).
//
// Bubble Tea repaints the whole screen each frame, so a View() that is one row
// short does not shift — it leaves the previous frame's row on screen. That
// artifact is the single most visible way a TUI looks broken, and it is only
// ever caught by measuring.
func TestEveryFrameIsExactlyTheTerminal(t *testing.T) {
	sizes := []struct{ w, h int }{
		{40, 10},  // the smallest pogo claims to work at
		{80, 24},  // the historical default
		{100, 30}, //
		{120, 34}, // wide enough for the sidebar
		{170, 48}, // wide enough for the preview pane too
		{240, 60}, // a full-screen terminal on a large display
		{61, 17},  // deliberately odd numbers: rounding lives here
		{200, 11}, // very wide and very short
		{41, 40},  // very narrow and very tall
	}

	screens := map[string]func(*Model){
		"list":     func(m *Model) { m.screen = screenList },
		"detail":   func(m *Model) { m.screen = screenDetail },
		"edit":     func(m *Model) { m.startEdit(m.selected()) },
		"new":      func(m *Model) { m.doNew() },
		"diff":     func(m *Model) { m.screen = screenDiff },
		"apis":     func(m *Model) { m.doAPIs() },
		"settings": func(m *Model) { m.doSettings() },
		"help":     func(m *Model) { m.doHelp() },

		"palette":    func(m *Model) { m.openPalette() },
		"copy":       func(m *Model) { m.overlay = overlayCopy },
		"confirm":    func(m *Model) { m.confirmID = m.selectedID(); m.overlay = overlayConfirm },
		"update":     func(m *Model) { m.updateVersion = "9.9.9"; m.overlay = overlayUpdate },
		"env":        func(m *Model) { m.overlay = overlayEnv },
		"collection": func(m *Model) { m.overlay = overlayCollection },
		"api-name":   func(m *Model) { m.doAPIs(); m.overlay = overlayAPIName },

		"searching": func(m *Model) { m.searching = true; m.search.SetValue("status:4xx") },
		"toast":     func(m *Model) { m.flash("something happened") },
		"busy":      func(m *Model) { m.busy = "replaying" },
		"empty":     func(m *Model) { m.entries, m.rows = nil, nil },
	}

	for name, setup := range screens {
		for _, size := range sizes {
			m := newTestModel(t, sampleEntries()...)
			m.Update(tea.WindowSizeMsg{Width: size.w, Height: size.h})
			setup(m)
			m.layout()

			view := m.View()
			lines := strings.Split(view, "\n")

			if len(lines) != size.h {
				t.Errorf("%s at %dx%d: %d rows, want %d",
					name, size.w, size.h, len(lines), size.h)
			}
			for i, line := range lines {
				if w := lineWidth(line); w != size.w {
					t.Errorf("%s at %dx%d: row %d is %d cells, want %d: %q",
						name, size.w, size.h, i, w, size.w, stripANSI(line))
					break
				}
			}
		}
	}
}
