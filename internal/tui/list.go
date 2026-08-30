package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rmpato/pogo/internal/history"
	"github.com/rmpato/pogo/internal/ui"
)

// rebuildRows applies the current query and grouping to the loaded history.
func (m *Model) rebuildRows() {
	m.rows = m.rows[:0]

	filtered := make([]int, 0, len(m.entries))
	for i, e := range m.entries {
		if m.query.Empty() || m.query.Match(e) {
			filtered = append(filtered, i)
		}
	}

	if m.group == groupNone {
		for _, i := range filtered {
			m.rows = append(m.rows, row{entry: m.entries[i]})
		}
		m.clampCursor()
		return
	}

	// Groups are ordered by their most recent request. History is already
	// newest-first, so first appearance is the right order and nothing needs
	// sorting by time.
	order := make([]string, 0, 8)
	members := map[string][]int{}
	for _, i := range filtered {
		host := m.groupKey(m.entries[i])
		if _, seen := members[host]; !seen {
			order = append(order, host)
		}
		members[host] = append(members[host], i)
	}

	// The unfiled bucket sorts last: it is a leftover, not a collection, and
	// putting it between named groups makes the list read like a mistake.
	if m.group == groupCollection {
		for i, name := range order {
			if name == noCollection && i != len(order)-1 {
				order = append(append(order[:i], order[i+1:]...), noCollection)
				break
			}
		}
	}

	for _, host := range order {
		idx := members[host]
		m.rows = append(m.rows, row{header: true, group: host, count: len(idx)})
		if m.collapsed[host] {
			continue
		}
		for _, i := range idx {
			m.rows = append(m.rows, row{entry: m.entries[i], group: host})
		}
	}
	m.clampCursor()
}

// groupKey is the heading a request belongs under.
// noCollection labels requests that have not been filed anywhere.
const noCollection = "(no collection)"

func (m *Model) groupKey(e *history.Entry) string {
	if label := m.groupLabel(e); label != "" {
		if m.group == groupCollection && e.Collection == "" {
			return noCollection
		}
		return label
	}
	return "(unknown)"
}

func (m *Model) clampCursor() {
	if m.cursor >= len(m.rows) {
		m.cursor = maxInt(0, len(m.rows)-1)
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.snapCursor(1)
	m.ensureVisible()
}

// selectable reports whether the cursor can rest on a row.
//
// An open group's heading is a label, not a row you act on: leaving the cursor
// there would mean opening pogo with nothing selected and half the keys inert,
// which reads as a broken program rather than a grouped list.
//
// A *closed* group's heading is the opposite — it is the only row that group
// has, standing in for everything inside it — so it is selectable, and space
// on it opens the group again.
func (m *Model) selectable(r row) bool {
	return !r.header || m.collapsed[r.group]
}

// snapCursor moves the cursor off an unselectable row in the given direction.
func (m *Model) snapCursor(dir int) {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return
	}
	if m.selectable(m.rows[m.cursor]) {
		return
	}
	for i := m.cursor; i >= 0 && i < len(m.rows); i += dir {
		if m.selectable(m.rows[i]) {
			m.cursor = i
			return
		}
	}
	// Nothing that way: try the other, so the last group's final row is
	// reachable from below.
	for i := m.cursor; i >= 0 && i < len(m.rows); i -= dir {
		if m.selectable(m.rows[i]) {
			m.cursor = i
			return
		}
	}
}

func (m *Model) ensureVisible() {
	h := m.contentHeight()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	if m.top < 0 {
		m.top = 0
	}
	if max := len(m.rows) - h; m.top > max {
		m.top = maxInt(0, max)
	}
}

func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = clampInt(m.cursor+delta, 0, len(m.rows)-1)
	dir := 1
	if delta < 0 {
		dir = -1
	}
	m.snapCursor(dir)
	m.ensureVisible()
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// The list is laid out with the kit's column arithmetic (ui.ColumnWidths,
// ui.TableRow) rather than ui.Table itself. The table renders every cell in one
// color, and color is how a pogo row is read at a glance: the method says how
// much damage the request could do and the status says how it went. So the row
// is assembled twice — plain for the selected line, where an inner color would
// end the highlight partway across, and colored for every other line. That is
// the kit's own Plain-variant rule (SYSTEM_DESIGN.md §5.3), applied here.

// listColumns describes a history row. A width of 0 flexes: the path takes
// whatever the fixed columns leave, because the path is what distinguishes one
// row from the next.
func (m *Model) listColumns(width int, grouped bool) []ui.Column {
	cols := []ui.Column{
		{Title: "", Width: 2},       // star and cursor
		{Title: "Method", Width: 6}, //
	}
	if !grouped {
		cols = append(cols, ui.Column{Title: "Host", Width: clampInt(width/5, 10, 24)})
	}
	cols = append(cols,
		ui.Column{Title: "Path", Width: 0},
		ui.Column{Title: "Status", Width: 3, Align: ui.AlignRight},
		ui.Column{Title: "Time", Width: 7, Align: ui.AlignRight},
	)
	// The last two columns are useful rather than necessary. A narrow pane
	// spends its cells on the path instead.
	if width >= 72 {
		cols = append(cols, ui.Column{Title: "Size", Width: 6, Align: ui.AlignRight})
	}
	if width >= 60 {
		cols = append(cols, ui.Column{Title: "Age", Width: 4, Align: ui.AlignRight})
	}
	return cols
}

// renderList draws the history list, with a scrollbar column down its edge.
func (m *Model) renderList(width, height int) string {
	switch {
	case m.loading:
		return m.centerNotice(width, height, m.spinner.View()+" reading history…")
	case m.loadErr != nil:
		return m.centerNotice(width, height, styErr.Render("could not read history: ")+m.loadErr.Error())
	case len(m.entries) == 0:
		return m.emptyHistory(width, height)
	case len(m.rows) == 0:
		return ui.EmptyState("⌕", "Nothing matches "+m.query.Raw,
			ui.Keycap("esc")+" clears the search · "+ui.Keycap("/")+" changes it", width, height)
	}

	// One column of the pane belongs to the scrollbar, always — a track that
	// appears only once a list is long enough would shift every row sideways
	// the moment it did.
	listWidth := maxInt(20, width-2)
	cols := m.listColumns(listWidth, m.group != groupNone)
	widths := ui.ColumnWidths(cols, listWidth)

	start, end := ui.Window(m.cursor, height, len(m.rows))
	m.top = start

	lines := make([]string, 0, height)
	for i := start; i < end; i++ {
		lines = append(lines, m.renderRow(m.rows[i], i == m.cursor, cols, widths, listWidth))
	}

	bar := ui.Scrollbar(start, height, len(m.rows), height)
	return ui.JoinColumns(strings.Join(lines, "\n"), bar, listWidth, 1, 1, height)
}

// rowCells is one row as plain text, in column order. Building the cells once
// and coloring them afterwards is what keeps the plain and colored forms
// exactly the same width.
func (m *Model) rowCells(e *history.Entry, cols []ui.Column) []string {
	mark := " "
	if e.Favorite {
		mark = "★"
	}
	cells := make([]string, 0, len(cols))
	for _, c := range cols {
		switch c.Title {
		case "":
			cells = append(cells, mark)
		case "Method":
			cells = append(cells, e.Request.Method)
		case "Host":
			cells = append(cells, m.displayHost(e))
		case "Path":
			cells = append(cells, m.displayPath(e))
		case "Status":
			cells = append(cells, statusText(e.Status(), e.Exit))
		case "Time":
			cells = append(cells, e.Duration.String())
		case "Size":
			size := "—"
			if e.Response != nil && e.Response.Body != nil {
				size = bytesHuman(e.Response.Body.Size)
			}
			cells = append(cells, size)
		case "Age":
			cells = append(cells, age(e.CreatedAt, m.now))
		}
	}
	return cells
}

func (m *Model) renderRow(r row, selected bool, cols []ui.Column, widths []int, width int) string {
	if r.header {
		return m.renderGroupHeader(r, selected, width)
	}
	e := r.entry
	cells := m.rowCells(e, cols)

	if selected {
		return ui.SelectedRowStyle.Render(ui.FitLine(ui.TableRow(cols, widths, cells), width))
	}

	// Color each cell after it has been padded to its column, so the styles
	// wrap text that is already the right width and nothing shifts.
	styled := make([]string, len(cells))
	for i, c := range cols {
		cell := ui.TableRow([]ui.Column{c}, []int{widths[i]}, []string{cells[i]})
		switch c.Title {
		case "":
			styled[i] = styStar.Render(cell)
		case "Method":
			styled[i] = methodStyle(cells[i]).Render(cell)
		case "Host":
			styled[i] = styMuted.Render(cell)
		case "Path":
			styled[i] = styText.Render(cell)
		case "Status":
			styled[i] = statusStyle(e.Status()).Render(cell)
		case "Time":
			styled[i] = styMuted.Render(cell)
		default:
			styled[i] = styFaint.Render(cell)
		}
	}
	return ui.FitLine(strings.Join(styled, "  "), width)
}

// renderGroupHeader draws the labeled divider a group hangs under. It is the
// kit's Rule, with the count on the right — the heading says what these
// requests have in common, and how many of them there are.
func (m *Model) renderGroupHeader(r row, selected bool, width int) string {
	if m.collapsed[r.group] {
		// Closed, the heading *is* the group: it says how many requests it
		// stands for, and it can be selected to open them again.
		label := "▸ " + strings.ToUpper(r.group)
		count := pluralize(r.count, "request") + " "
		if selected {
			return ui.SelectedRowStyle.Render(ui.StatusBar(" "+label, count, width))
		}
		return ui.StatusBar(" "+ui.LabelStyle.Bold(true).Render(label),
			ui.SubtitleStyle.Render(count), width)
	}

	count := ui.SubtitleStyle.Render(" " + itoa(r.count))
	head := ui.Rule(r.group, maxInt(4, width-lipgloss.Width(count)))
	return ui.FitLine(head+count, width)
}

// displayURL applies the redaction policy before anything reaches the screen,
// so a token embedded in a query string is masked in the list as well as in the
// detail view. Pressing S reveals the originals.
func (m *Model) displayURL(e *history.Entry) string {
	if m.reveal {
		return e.Request.URL
	}
	return m.cfg.Redact.MaskURL(e.Request.URL)
}

func (m *Model) displayHost(e *history.Entry) string { return history.HostOf(m.displayURL(e)) }
func (m *Model) displayPath(e *history.Entry) string { return history.PathOf(m.displayURL(e)) }

func pluralize(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// emptyHistory is what a new user sees. An empty state is a teaching moment,
// so it shows the one command that fills the screen with something.
func (m *Model) emptyHistory(width, height int) string {
	lines := []string{
		styHeading.Render("No requests yet"),
		"",
		styMuted.Render("pogo shows what ") + styKey.Render("pogo") + styMuted.Render(" has run. Try:"),
		"",
		"    " + styKey.Render("pogo https://api.github.com/zen"),
		"",
		styMuted.Render("pogo passes everything through to curl, so any curl"),
		styMuted.Render("command works and every one of them lands here."),
		"",
		styMuted.Render("Or press ") + styKey.Render("n") + styMuted.Render(" to start a blank request right here."),
		"",
		styFaint.Render("history: " + m.st.Path()),
	}
	return m.centerNotice(width, height, strings.Join(lines, "\n"))
}

func (m *Model) centerNotice(width, height int, content string) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

// knownCollections lists the collections already in use, so the prompt can
// suggest them instead of making the user remember exact spelling.
func (m *Model) knownCollections() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range m.entries {
		if e.Collection != "" && !seen[e.Collection] {
			seen[e.Collection] = true
			out = append(out, e.Collection)
		}
	}
	sort.Strings(out)
	if len(out) > 6 {
		out = append(out[:6], "…")
	}
	return out
}
