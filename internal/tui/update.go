package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/curlargs"
	"github.com/rmpato/pogo/internal/curledit"
	"github.com/rmpato/pogo/internal/version"
)

// handleKey routes a keypress to the active overlay or screen. Overlays are
// modal and get first refusal, which is what makes "esc always goes back" true.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.overlay != overlayNone {
		return m.handleOverlayKey(msg)
	}
	if m.searching {
		return m.handleSearchKey(msg)
	}

	switch m.screen {
	case screenEdit:
		return m.handleEditKey(msg)
	case screenDetail:
		return m.handleDetailKey(msg)
	case screenDiff:
		return m.handleDiffKey(msg)
	case screenAPIs:
		if handled, cmd := m.handleGlobalKey(msg); handled {
			return m, cmd
		}
		return m.handleAPIKey(msg)
	case screenSettings:
		if handled, cmd := m.handleGlobalKey(msg); handled {
			return m, cmd
		}
		return m.handleSettingsKey(msg)
	case screenHelp:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Down):
			m.helpScroll = clampInt(m.helpScroll+1, 0, m.helpMax)
			return m, nil
		case key.Matches(msg, keys.Up):
			m.helpScroll = clampInt(m.helpScroll-1, 0, m.helpMax)
			return m, nil
		case key.Matches(msg, keys.PageDn):
			m.helpScroll = clampInt(m.helpScroll+m.contentHeight()/2, 0, m.helpMax)
			return m, nil
		case key.Matches(msg, keys.PageUp):
			m.helpScroll = clampInt(m.helpScroll-m.contentHeight()/2, 0, m.helpMax)
			return m, nil
		default:
			m.screen = m.prevScreen
			m.helpScroll = 0
			return m, nil
		}
	default:
		return m.handleListKey(msg)
	}
}

// handleGlobalKey covers the few keys that mean the same thing on every screen.
// A workspace that swallowed ctrl+k or ? would be a place the user could get
// stuck, which is the one thing the key grammar exists to prevent.
func (m *Model) handleGlobalKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit) && msg.String() != "q":
		return true, tea.Quit
	case key.Matches(msg, keys.Home):
		return true, m.doHome()
	case key.Matches(msg, keys.Palette):
		return true, m.doPalette()
	case key.Matches(msg, keys.Help):
		return true, m.doHelp()
	}
	return false, nil
}

// --- list ------------------------------------------------------------------

func (m *Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The sidebar takes the arrow keys while it has focus, so one pair of keys
	// drives whichever pane you are in.
	if m.focus == focusSidebar && m.showSidebar() {
		switch {
		case key.Matches(msg, keys.Up):
			m.moveRail(-1)
			return m, nil
		case key.Matches(msg, keys.Down):
			m.moveRail(1)
			return m, nil
		case key.Matches(msg, keys.Enter):
			return m, m.applyRail()
		case key.Matches(msg, keys.NextTab), key.Matches(msg, keys.PrevTab), msg.Type == tea.KeyEsc:
			m.focus = focusList
			return m, nil
		}
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Palette):
		return m, m.doPalette()

	case key.Matches(msg, keys.Help):
		return m, m.doHelp()

	case key.Matches(msg, keys.NextTab), key.Matches(msg, keys.PrevTab):
		if m.showSidebar() {
			m.focus = focusSidebar
		}
		return m, nil

	case key.Matches(msg, keys.Up):
		m.move(-1)
		return m, m.previewCmd()
	case key.Matches(msg, keys.Down):
		m.move(1)
		return m, m.previewCmd()
	case key.Matches(msg, keys.PageUp):
		m.move(-m.contentHeight() / 2)
		return m, m.previewCmd()
	case key.Matches(msg, keys.PageDn):
		m.move(m.contentHeight() / 2)
		return m, m.previewCmd()
	case key.Matches(msg, keys.Top):
		m.cursor = 0
		m.ensureVisible()
		return m, m.previewCmd()
	case key.Matches(msg, keys.Bottom):
		m.cursor = maxInt(0, len(m.rows)-1)
		m.ensureVisible()
		return m, m.previewCmd()

	case key.Matches(msg, keys.Enter):
		return m, m.doInspect()

	case key.Matches(msg, keys.Search):
		return m, m.doSearch()

	case key.Matches(msg, keys.New):
		return m, m.doNew()

	case key.Matches(msg, keys.Back):
		return m, m.doClearSearch()

	case key.Matches(msg, keys.Toggle):
		return m, m.doFold()

	case key.Matches(msg, keys.FoldAll):
		return m, m.doFoldAll()

	case key.Matches(msg, keys.Group):
		return m, m.doGroup()

	case key.Matches(msg, keys.Sidebar):
		return m, m.doToggleSidebar()

	case key.Matches(msg, keys.Env):
		return m, m.doEnv()

	case key.Matches(msg, keys.Update):
		return m, m.doUpdate()

	default:
		return m.handleEntryAction(msg)
	}
}

// previewCmd loads payloads for the side preview, but only when there is one.
func (m *Model) previewCmd() tea.Cmd {
	if m.previewWidth() == 0 {
		return nil
	}
	return m.loadDetail()
}

// handleEntryAction covers the verbs that work on the selected request from
// both the list and the detail view.
func (m *Model) handleEntryAction(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Replay):
		return m, m.doReplay()
	case key.Matches(msg, keys.Edit):
		return m, m.doEdit()
	case key.Matches(msg, keys.Star):
		return m, m.doStar()
	case key.Matches(msg, keys.Preview):
		return m, m.doTogglePreview()

	case key.Matches(msg, keys.APIs):
		return m, m.doAPIs()

	case key.Matches(msg, keys.Home):
		return m, m.doHome()

	case key.Matches(msg, keys.Collection):
		return m, m.doCollection()
	case key.Matches(msg, keys.Delete):
		return m, m.doDelete()
	case key.Matches(msg, keys.Copy):
		return m, m.doCopy()
	case key.Matches(msg, keys.Diff):
		return m, m.doDiff()
	case key.Matches(msg, keys.Reveal):
		return m, m.doReveal()
	}
	return m, nil
}

// --- search ----------------------------------------------------------------

func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.search.Blur()
		return m, nil
	case "enter":
		m.searching = false
		m.search.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)

	// Filtering happens on every keystroke: search that waits for Enter feels
	// broken when the whole point is to find something in a hundred rows.
	m.query = ParseQuery(m.search.Value()).WithRegistry(m.cfg.APIs)
	id := m.selectedID()
	m.rebuildRows()
	m.selectID(id)
	return m, cmd
}

// --- detail ----------------------------------------------------------------

func (m *Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Back):
		m.screen = screenList
		m.layout()
		return m, nil

	case key.Matches(msg, keys.Palette):
		return m, m.doPalette()

	case key.Matches(msg, keys.Help):
		return m, m.doHelp()

	case key.Matches(msg, keys.NextTab):
		m.detail.tab = (m.detail.tab + 1) % detailTab(len(tabNames))
		m.detail.vp.GotoTop()
		return m, nil
	case key.Matches(msg, keys.PrevTab):
		m.detail.tab = (m.detail.tab + detailTab(len(tabNames)) - 1) % detailTab(len(tabNames))
		m.detail.vp.GotoTop()
		return m, nil

	case msg.String() >= "1" && msg.String() <= "5" && len(msg.String()) == 1:
		m.detail.tab = detailTab(msg.String()[0] - '1')
		m.detail.vp.GotoTop()
		return m, nil

	case key.Matches(msg, keys.Body):
		return m, m.doBodyMode()

	case key.Matches(msg, keys.Toggle):
		if m.detail.mode == bodyTree {
			m.toggleTreeNode()
			return m, nil
		}

	case key.Matches(msg, keys.Down):
		if m.treeNavigable() {
			m.moveTreeCursor(1)
			return m, nil
		}
		m.detail.vp.ScrollDown(1)
		return m, nil

	case key.Matches(msg, keys.Up):
		if m.treeNavigable() {
			m.moveTreeCursor(-1)
			return m, nil
		}
		m.detail.vp.ScrollUp(1)
		return m, nil

	case key.Matches(msg, keys.PageDn):
		m.detail.vp.HalfPageDown()
		return m, nil
	case key.Matches(msg, keys.PageUp):
		m.detail.vp.HalfPageUp()
		return m, nil
	case key.Matches(msg, keys.Top):
		m.detail.vp.GotoTop()
		return m, nil
	case key.Matches(msg, keys.Bottom):
		m.detail.vp.GotoBottom()
		return m, nil
	}

	return m.handleEntryAction(msg)
}

// treeNavigable reports whether j/k should move a JSON cursor rather than
// scroll the pane.
func (m *Model) treeNavigable() bool {
	return m.detail.mode == bodyTree &&
		m.detail.tab != tabTiming && m.detail.tab != tabRaw &&
		len(m.detail.focused().lines) > 0
}

func (m *Model) moveTreeCursor(delta int) {
	ts := m.detail.focused()
	if len(ts.lines) == 0 {
		return
	}
	ts.cursor = clampInt(ts.cursor+delta, 0, len(ts.lines)-1)

	// Keep the cursor inside the viewport as it moves.
	if ts.cursor < m.detail.vp.YOffset {
		m.detail.vp.SetYOffset(ts.cursor)
	}
	if bottom := m.detail.vp.YOffset + m.detail.vp.Height - 1; ts.cursor > bottom {
		m.detail.vp.SetYOffset(ts.cursor - m.detail.vp.Height + 1)
	}
}

// toggleTreeNode folds or unfolds the node under the cursor. Pressing space on
// a leaf folds its parent, which is what "collapse this bit" usually means.
func (m *Model) toggleTreeNode() {
	ts := m.detail.focused()
	if ts.cursor >= len(ts.lines) {
		return
	}
	line := ts.lines[ts.cursor]
	if line.node.container() {
		line.node.expanded = !line.node.expanded
		return
	}
	if parent := findParent(ts.tree, line.node); parent != nil {
		parent.expanded = false
		for i, l := range ts.lines {
			if l.node == parent {
				ts.cursor = i
				break
			}
		}
	}
}

func findParent(root, target *jnode) *jnode {
	for _, c := range root.children {
		if c == target {
			return root
		}
		if p := findParent(c, target); p != nil {
			return p
		}
	}
	return nil
}

// --- edit ------------------------------------------------------------------

func (m *Model) handleEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Raw mode is the escape hatch: the command as text, for what a form cannot
	// express. It keeps the same run and cancel keys.
	if m.edit.raw {
		switch {
		case key.Matches(msg, keys.Run):
			return m, m.runEditor()
		case key.Matches(msg, keys.Editor):
			return m, openEditor(m.editor.Value())
		case key.Matches(msg, keys.EditToggle):
			m.editFromRaw()
			return m, nil
		case msg.Type == tea.KeyEsc:
			return m, m.closeEditor()
		}
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd
	}

	// The body is multi-line, so it gets the textarea rather than a one-line
	// input; escape returns to the field list.
	if m.edit.inBody {
		switch {
		case key.Matches(msg, keys.Run):
			m.edit.form.Body = m.editor.Value()
			return m, m.runEditor()
		case msg.Type == tea.KeyEsc:
			m.edit.form.Body = m.editor.Value()
			m.edit.inBody = false
			m.editor.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd
	}

	// Inline editing of a single field.
	if m.edit.editing {
		switch msg.Type {
		case tea.KeyEnter:
			m.edit.commit(m.edit.rows[m.edit.cursor], m.edit.input.Value())
			m.edit.editing = false
			m.edit.input.Blur()
			return m, nil
		case tea.KeyEsc:
			m.edit.editing = false
			m.edit.input.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.edit.input, cmd = m.edit.input.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Run):
		return m, m.runEditor()

	case key.Matches(msg, keys.EditToggle):
		// Switching to raw shows the command the form would produce, so the two
		// views never disagree about what will run.
		m.edit.raw = true
		m.editor.SetValue(curlargs.Render(m.edit.Args(), true))
		m.editor.CursorEnd()
		m.layout()
		m.editor.Focus()
		return m, nil

	case key.Matches(msg, keys.Editor):
		return m, openEditor(curlargs.Render(m.edit.Args(), true))

	case key.Matches(msg, keys.EditDelete):
		m.edit.remove()
		return m, nil

	case key.Matches(msg, keys.Up):
		m.edit.moveCursor(-1)
		return m, nil
	case key.Matches(msg, keys.Down):
		m.edit.moveCursor(1)
		return m, nil

	case msg.Type == tea.KeyLeft:
		if m.edit.rows[m.edit.cursor].kind == editMethod {
			m.edit.cycleMethod(-1)
		}
		return m, nil
	case msg.Type == tea.KeyRight:
		if m.edit.rows[m.edit.cursor].kind == editMethod {
			m.edit.cycleMethod(1)
		}
		return m, nil

	case msg.Type == tea.KeyEnter:
		row := m.edit.rows[m.edit.cursor]
		switch row.kind {
		case editAddQuery, editAddHeader:
			m.edit.add(row.kind)
			m.startFieldEdit()
		case editBody:
			m.edit.inBody = true
			m.editor.SetValue(m.edit.form.Body)
			m.editor.CursorEnd()
			m.layout()
			m.editor.Focus()
		default:
			m.startFieldEdit()
		}
		return m, nil

	case msg.Type == tea.KeyEsc:
		return m, m.closeEditor()
	}
	return m, nil
}

// startFieldEdit puts the focused row into an inline text input.
func (m *Model) startFieldEdit() {
	row := m.edit.rows[m.edit.cursor]
	// Editing always shows the real value: a masked one cannot be edited, and
	// silently sending back the mask would be worse than showing the secret.
	m.edit.input.SetValue(m.edit.value(row))
	m.edit.input.CursorEnd()
	m.edit.input.Width = maxInt(20, m.width-20)
	m.edit.editing = true
	m.edit.input.Focus()
}

// editFromRaw parses the raw buffer back into fields, so the two views stay in
// step when the user switches back.
func (m *Model) editFromRaw() {
	args, err := curlargs.Split(m.editor.Value())
	if err != nil {
		m.flashErr(err.Error())
		return
	}
	args = curlargs.StripCurl(args)
	if len(args) == 0 {
		m.flashErr("no curl arguments found")
		return
	}

	spec := curlargs.Parse(args)
	form := curledit.FormOf(spec, m.edit.form.Body)

	m.edit.args = args
	m.edit.have = form
	m.edit.form = cloneForm(form)
	m.edit.raw = false
	m.editor.Blur()
	m.edit.rebuild()
}

func (m *Model) closeEditor() tea.Cmd {
	m.screen = screenList
	m.edit.editing = false
	m.edit.inBody = false
	m.editor.Blur()
	m.edit.input.Blur()
	m.layout()
	return nil
}

// runEditor executes what the editor describes, as a new entry.
//
// The command is produced by applying the form's changes to the original argv,
// so options the form does not model survive. In raw mode the buffer itself is
// authoritative.
func (m *Model) runEditor() tea.Cmd {
	var args []string

	if m.edit.raw {
		text := strings.TrimSpace(m.editor.Value())
		if text == "" {
			m.flashErr("nothing to run")
			return clearStatus(m.statusTok)
		}
		parsed, err := curlargs.Split(text)
		if err != nil {
			m.flashErr(err.Error())
			return clearStatus(m.statusTok)
		}
		args = curlargs.StripCurl(parsed)
	} else {
		if m.edit.inBody {
			m.edit.form.Body = m.editor.Value()
		}
		args = m.edit.Args()
	}

	if len(args) == 0 {
		m.flashErr("no curl arguments found")
		return clearStatus(m.statusTok)
	}

	parent := m.entryByID(m.editID)
	rec := m.recorderFor(parent)
	if parent == nil && m.edit.api != "" {
		rec = m.recorderForDomain(m.edit.api)
	}
	m.screen = screenList
	m.edit.editing, m.edit.inBody = false, false
	m.editor.Blur()
	m.edit.input.Blur()
	m.busy = "running"
	m.layout()
	return tea.Batch(runEdited(rec, parent, args), m.spinner.Tick)
}

// --- diff ------------------------------------------------------------------

func (m *Model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Back):
		m.screen = screenList
		m.layout()
		return m, nil
	case key.Matches(msg, keys.Diff):
		m.diffA = nil
		m.screen = screenList
		m.flash("comparison cleared")
		m.layout()
		return m, clearStatus(m.statusTok)
	}
	var cmd tea.Cmd
	m.diffVP, cmd = m.diffVP.Update(msg)
	return m, cmd
}

// --- overlays --------------------------------------------------------------

func (m *Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayPalette:
		_, cmd := m.handlePaletteKey(msg)
		return m, cmd

	case overlayConfirm:
		switch msg.String() {
		case "y", "Y", "enter":
			id := m.confirmID
			m.overlay = overlayNone
			m.confirmID = ""
			if m.screen == screenDetail {
				m.screen = screenList
			}
			return m, deleteEntry(m.st, id)
		default:
			m.overlay = overlayNone
			m.confirmID = ""
			return m, nil
		}

	case overlayUpdate:
		switch msg.String() {
		case "y", "Y", "enter":
			m.overlay = overlayNone
			m.updating = true
			m.busy = "updating"
			return m, tea.Batch(applyUpdate(version.Version), m.spinner.Tick)
		default:
			m.overlay = overlayNone
			return m, nil
		}

	case overlayEnv:
		names := append([]string{""}, m.envSet.Names()...)
		switch {
		case key.Matches(msg, keys.Down):
			m.envCursor = clampInt(m.envCursor+1, 0, len(names)-1)
			return m, nil
		case key.Matches(msg, keys.Up):
			m.envCursor = clampInt(m.envCursor-1, 0, len(names)-1)
			return m, nil
		case key.Matches(msg, keys.Enter):
			m.overlay = overlayNone
			chosen := names[m.envCursor]
			m.envSet.Active = chosen
			if chosen == "" {
				m.flash("environment cleared")
			} else {
				m.flash("environment: " + chosen)
			}
			return m, tea.Batch(saveActiveEnvironment(m.envSet, chosen), clearStatus(m.statusTok))
		default:
			m.overlay = overlayNone
			return m, nil
		}

	case overlayCollection:
		switch msg.Type {
		case tea.KeyEnter:
			m.overlay = overlayNone
			m.collectionInput.Blur()
			name := strings.TrimSpace(m.collectionInput.Value())
			if e := m.selected(); e != nil {
				return m, setCollection(m.st, e.ID, name)
			}
			return m, nil
		case tea.KeyEsc:
			m.overlay = overlayNone
			m.collectionInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.collectionInput, cmd = m.collectionInput.Update(msg)
		return m, cmd

	case overlayAPIName:
		switch msg.Type {
		case tea.KeyEnter:
			m.overlay = overlayNone
			m.collectionInput.Blur()
			name := strings.TrimSpace(m.collectionInput.Value())
			row, ok := m.selectedAPIRow()
			if !ok {
				return m, nil
			}
			if err := m.setAPIOverride(func(c *config.Config) {
				c.APIs.SetName(row.domain, name)
			}); err != nil {
				m.flashErr("could not save: " + err.Error())
				return m, clearStatus(m.statusTok)
			}
			if name == "" {
				m.flash(row.domain + " is shown by domain again")
			} else {
				m.flash(row.domain + " is now " + name)
			}
			return m, clearStatus(m.statusTok)
		case tea.KeyEsc:
			m.overlay = overlayNone
			m.collectionInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.collectionInput, cmd = m.collectionInput.Update(msg)
		return m, cmd

	case overlayCopy:
		items := m.copyItems()
		switch {
		case key.Matches(msg, keys.Down):
			m.copyCursor = clampInt(m.copyCursor+1, 0, len(items)-1)
			return m, nil
		case key.Matches(msg, keys.Up):
			m.copyCursor = clampInt(m.copyCursor-1, 0, len(items)-1)
			return m, nil
		case key.Matches(msg, keys.Enter):
			m.overlay = overlayNone
			return m, m.runCopy(items[m.copyCursor])
		case msg.String() == "esc" || key.Matches(msg, keys.Copy):
			m.overlay = overlayNone
			return m, nil
		default:
			// Direct selection by the item's own letter.
			for _, it := range items {
				if msg.String() == it.key {
					m.overlay = overlayNone
					return m, m.runCopy(it)
				}
			}
			return m, nil
		}
	}
	m.overlay = overlayNone
	return m, nil
}
