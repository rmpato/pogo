package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// The verbs. Every one of these is reachable three ways — its key, the command
// palette, and the help reference — and all three call the method here, so they
// cannot drift apart.

func (m *Model) doInspect() tea.Cmd {
	// On a closed group's heading, the natural meaning of "open" is open.
	if m.cursor < len(m.rows) && m.rows[m.cursor].header {
		return m.doFold()
	}
	if m.selected() == nil {
		return nil
	}
	m.screen = screenDetail
	m.layout()
	return m.loadDetail()
}

func (m *Model) doReplay() tea.Cmd {
	e := m.selected()
	if e == nil {
		return nil
	}
	m.busy = "replaying"
	m.replaySource = e
	return tea.Batch(replay(m.recorderFor(e), e), m.spinner.Tick)
}

func (m *Model) doNew() tea.Cmd {
	api := m.newRequestAPI()
	base, _ := m.cfg.APIs.Project(api)
	return m.startNewRequest(api, base)
}

// newRequestAPI is what a blank request defaults to filing under: the project
// pogo was opened with, or the one API the list is already narrowed to.
// Neither is a guarantee — the URL you type can still say otherwise — just the
// obvious guess when there is one.
func (m *Model) newRequestAPI() string {
	if m.project != "" {
		return m.project
	}
	if len(m.query.APIs) == 1 {
		return m.query.APIs[0]
	}
	return ""
}

func (m *Model) doEdit() tea.Cmd {
	return m.startEdit(m.selected())
}

func (m *Model) doCopy() tea.Cmd {
	if m.selected() == nil {
		return nil
	}
	m.overlay = overlayCopy
	m.copyCursor = 0
	return nil
}

func (m *Model) doDiff() tea.Cmd {
	e := m.selected()
	if e == nil {
		return nil
	}
	return m.toggleDiff(e)
}

func (m *Model) doDelete() tea.Cmd {
	e := m.selected()
	if e == nil {
		return nil
	}
	m.overlay = overlayConfirm
	m.confirmID = e.ID
	return nil
}

func (m *Model) doStar() tea.Cmd {
	e := m.selected()
	if e == nil {
		return nil
	}
	return setFavorite(m.st, e.ID, !e.Favorite)
}

func (m *Model) doCollection() tea.Cmd {
	e := m.selected()
	if e == nil {
		return nil
	}
	m.overlay = overlayCollection
	m.collectionInput.SetValue(e.Collection)
	m.collectionInput.CursorEnd()
	return m.collectionInput.Focus()
}

func (m *Model) doGroup() tea.Cmd {
	m.group = m.group.next()
	id := m.selectedID()
	m.rebuildRows()
	m.selectID(id)
	m.flash(m.group.String())
	return clearStatus(m.statusTok)
}

func (m *Model) doSearch() tea.Cmd {
	m.screen = screenList
	m.searching = true
	m.search.SetValue(m.query.Raw)
	m.search.CursorEnd()
	return m.search.Focus()
}

func (m *Model) doClearSearch() tea.Cmd {
	if m.query.Raw == "" {
		return nil
	}
	m.query = Query{}
	m.search.SetValue("")
	m.rebuildRows()
	m.flash("search cleared")
	return clearStatus(m.statusTok)
}

// applyFilter runs a canned search, which is how the palette offers things like
// "show failures only" without the user learning the filter syntax first. The
// syntax then shows up in the search bar, which is how they learn it.
func (m *Model) applyFilter(expr string) tea.Cmd {
	m.screen = screenList
	m.query = ParseQuery(expr).WithRegistry(m.cfg.APIs)
	m.search.SetValue(expr)
	id := m.selectedID()
	m.rebuildRows()
	m.selectID(id)
	m.flash("filter: " + expr)
	return clearStatus(m.statusTok)
}

func (m *Model) doToggleSidebar() tea.Cmd {
	m.sidebar = !m.sidebar
	if !m.sidebar {
		m.focus = focusList
	}
	m.layout()
	if m.sidebar && m.width < minSidebarWidth {
		m.flash("the terminal is too narrow for the sidebar")
		return clearStatus(m.statusTok)
	}
	return nil
}

func (m *Model) doReveal() tea.Cmd {
	m.reveal = !m.reveal
	if m.reveal {
		m.flashErr("secrets revealed — press S to hide")
	} else {
		m.flash("secrets hidden")
	}
	return clearStatus(m.statusTok)
}

func (m *Model) doBodyMode() tea.Cmd {
	m.detail.mode = (m.detail.mode + 1) % 3
	m.flash("body view: " + bodyModeNames[m.detail.mode])
	return clearStatus(m.statusTok)
}

func (m *Model) doEnv() tea.Cmd {
	if len(m.envSet.Names()) == 0 {
		m.flashErr("no environments defined — see docs/environments.md")
		return clearStatus(m.statusTok)
	}
	m.overlay = overlayEnv
	m.envCursor = 0
	for i, name := range m.envSet.Names() {
		if name == m.envSet.Active {
			m.envCursor = i + 1 // the first row is "(none)"
		}
	}
	return nil
}

func (m *Model) doUpdate() tea.Cmd {
	if m.updateVersion == "" {
		m.flash("no update available")
		return clearStatus(m.statusTok)
	}
	m.overlay = overlayUpdate
	return nil
}

func (m *Model) doHelp() tea.Cmd {
	if m.screen != screenHelp {
		m.prevScreen = m.screen
	}
	m.screen = screenHelp
	return nil
}

// doPalette opens the command palette: the answer to "what can this thing do?"
// without having to already know.
func (m *Model) doPalette() tea.Cmd {
	m.openPalette()
	return nil
}

// doHome leaves for the shell above the list. The program stops and Run takes
// over, which is what keeps the shell a level rather than a screen: it is not
// somewhere the list can be inside of.
func (m *Model) doHome() tea.Cmd {
	m.exit = exitHome
	return tea.Quit
}

// doFold collapses or expands the group the cursor is in.
//
// Headings are labels rather than rows you land on, so folding hangs off the
// request under the cursor instead: at two hundred requests across six APIs,
// collapsing the ones you are not working on is the difference between a list
// and a wall.
func (m *Model) doFold() tea.Cmd {
	if m.group == groupNone || m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	group := m.rows[m.cursor].group
	if group == "" {
		return nil
	}

	m.collapsed[group] = !m.collapsed[group]
	m.rebuildRows()

	// Stay with the group that was just folded, rather than with the request
	// that has gone. Otherwise a second press lands on whatever slid up under
	// the cursor and folds *that*, which reads as the key doing something
	// different every other time.
	m.selectGroup(group)
	return nil
}

// selectGroup puts the cursor on a group: its first request when it is open,
// and its heading when it is closed — which is the row that group has become.
func (m *Model) selectGroup(group string) {
	for i, r := range m.rows {
		if r.group != group {
			continue
		}
		m.cursor = i
		m.snapCursor(1)
		m.ensureVisible()
		return
	}
	m.clampCursor()
}

// doFoldAll collapses every group, or expands them when all are already closed.
func (m *Model) doFoldAll() tea.Cmd {
	if m.group == groupNone {
		return nil
	}
	anyOpen := false
	for _, r := range m.rows {
		if r.header && !m.collapsed[r.group] {
			anyOpen = true
			break
		}
	}

	id := m.selectedID()
	for _, r := range m.rows {
		if r.header {
			m.collapsed[r.group] = anyOpen
		}
	}
	m.rebuildRows()
	m.selectID(id)
	m.clampCursor()
	return nil
}

// doTogglePreview shows or hides the panel down the right.
func (m *Model) doTogglePreview() tea.Cmd {
	m.preview = !m.preview
	m.layout()
	if m.preview && m.width < minPreviewWidth {
		m.flash("the terminal is too narrow for the preview")
		return clearStatus(m.statusTok)
	}
	// Turning it on has to fetch what it is going to show.
	return m.previewCmd()
}
