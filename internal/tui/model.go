// Package tui implements pogo, the terminal UI over pogo's request history.
//
// The model owns no files and shells out to nothing: reading and writing
// history goes through internal/store, and running a request goes through
// internal/capture, which is the same path pogo itself takes. Everything here
// is state and rendering.
package tui

import (
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmpato/pogo/internal/apis"
	"github.com/rmpato/pogo/internal/capture"
	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/environment"
	"github.com/rmpato/pogo/internal/history"
	"github.com/rmpato/pogo/internal/selfupdate"
	"github.com/rmpato/pogo/internal/store"
	"github.com/rmpato/pogo/internal/ui"
	"github.com/rmpato/pogo/internal/version"
)

type screen int

const (
	screenList screen = iota
	screenDetail
	screenEdit
	screenDiff
	screenAPIs
	screenSettings
	screenHelp
)

type overlay int

const (
	overlayNone overlay = iota
	overlayCopy
	overlayConfirm
	overlayUpdate
	overlayEnv
	overlayCollection
	overlayAPIName
	overlayPalette
)

// focusArea is which pane the arrow keys drive on the list screen.
type focusArea int

const (
	focusList focusArea = iota
	focusSidebar
)

// Panes appear as the terminal can afford them: the list always, the sidebar
// next, the preview last. Both thresholds are absolute rather than relative to
// each other, so hiding the sidebar only ever gives columns *to* the list —
// a toggle that made the list narrower would be baffling.
const (
	minSidebarWidth = 108
	minPreviewWidth = 132
)

// groupMode decides how the list is organized. History gets noisy fast, so
// grouping is a keystroke rather than something the user has to set up.
type groupMode int

const (
	// By API is the default: it is the grouping that matches how the requests
	// were actually made, and a flat river of two thousand rows is not.
	groupAPI groupMode = iota
	groupNone
	groupHost
	groupCollection
)

func (g groupMode) String() string {
	switch g {
	case groupAPI:
		return "by API"
	case groupHost:
		return "by host"
	case groupCollection:
		return "by collection"
	default:
		return "chronological"
	}
}

// next cycles grouping. API first, because that is where someone lands.
func (g groupMode) next() groupMode {
	switch g {
	case groupAPI:
		return groupNone
	case groupNone:
		return groupHost
	case groupHost:
		return groupCollection
	default:
		return groupAPI
	}
}

// row is one line of the history list. A row is either a group header or an
// entry, which keeps grouped and flat rendering on a single code path.
type row struct {
	entry  *history.Entry
	header bool
	group  string
	count  int
}

// Model is the pogo application state.
type Model struct {
	// cfgStore owns the config file. A preference changed on a screen is
	// written on the keypress that changed it; cfg is the value it last wrote,
	// cached so that renderers do not go through the store on every frame.
	cfgStore *config.Store[config.Config]
	cfg      config.Config

	st  *store.Store
	rec *capture.Recorder

	// project is the API a blank new request defaults to filing under, when
	// pogo was opened with one (`pogo checkout`, `pogo checkout new`).
	project string

	entries []*history.Entry
	rows    []row
	cursor  int
	top     int

	width, height int
	screen        screen
	overlay       overlay
	prevScreen    screen

	group     groupMode
	collapsed map[string]bool

	// refCache memoises which API each URL belongs to; see apis.go.
	refCache map[string]apis.Ref

	// sidebar shows filters, APIs and collections down the left, so the shape
	// of the history is visible instead of hidden behind mode keys. preview
	// shows the request under the cursor down the right, so choosing between
	// two rows does not mean opening both.
	sidebar    bool
	preview    bool
	focus      focusArea
	railCursor int
	rail       []railItem

	// palette is how someone finds a feature they do not yet know the key for.
	// It is the kit's component: it owns the keyboard while it is open.
	palette ui.Palette

	search    textinput.Model
	searching bool
	query     Query

	detail detailModel
	editor textarea.Model
	editID string
	edit   editState

	// pendingEditBody records that the editor opened before the request body
	// had been read off disk, so the field can be filled in when it arrives.
	pendingEditBody bool

	// envSet holds every environment. Which variables apply is not a property
	// of the set alone: an environment name is global, its values belong to an
	// API, so the answer depends on the request being run. See recorderFor.
	envSet environment.Set

	// replaySource is the request a running replay came from. When the replay
	// lands it becomes the comparison mark, so "run it again and see what
	// changed" is two keys rather than four.
	replaySource *history.Entry

	diffA     *history.Entry
	diffVP    viewport.Model
	diffTitle string

	copyCursor     int
	envCursor      int
	apiCursor      int
	settingsCursor int
	confirmID      string

	// collectionInput names a collection while the overlay is open.
	collectionInput textinput.Model

	// updateVersion is a newer release the user has been told about. Nothing is
	// installed until they press u and confirm.
	updateVersion string
	updating      bool

	// pendingSelect moves the cursor onto an entry that does not exist yet,
	// which is how a replay lands on its own result once the reload arrives.
	pendingSelect string

	// exit records why the program stopped, so Run knows whether the user quit
	// or went up to the home shell.
	exit exitTo

	spinner spinner.Model
	busy    string

	// helpScroll is how far down the keyboard reference has been moved, when
	// it is longer than the terminal is tall.
	helpScroll int
	helpMax    int

	status    string
	statusErr bool
	statusTok int
	reveal    bool
	loading   bool
	loadErr   error
	skipped   int
	now       time.Time
}

// recorderFor returns the capture recorder bound to the environment this
// request should run under, so a replay resolves {{variables}} against
// whatever is selected right now rather than whatever was set when the request
// was first made — and against the right API's values, since "staging" means
// one thing for acme.com and another for the payments API.
func (m *Model) recorderFor(e *history.Entry) *capture.Recorder {
	return m.recorderForDomain(m.domainOf(e))
}

// recorderForDomain is recorderFor without a stored entry to read the domain
// from — the case a blank new request is in, where the domain came from the
// project it was composed under instead.
func (m *Model) recorderForDomain(domain string) *capture.Recorder {
	if m.rec == nil {
		return nil
	}
	rec := m.rec
	if domain != "" {
		rec = rec.WithAPI(domain)
	}
	if m.envSet.Active == "" {
		return rec
	}
	return rec.WithEnvironment(m.envSet.Active, m.envSet.Vars(domain, m.envSet.Active))
}

// New builds the application model.
func New(opts Options) *Model {
	cfg := opts.Config.Current().WithEnv()
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "url, method:POST, status:4xx, host:api.example.com, is:starred"
	ti.CharLimit = 200

	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 0

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styMuted

	ci := textinput.New()
	ci.Prompt = "› "
	ci.CharLimit = 60
	ci.Placeholder = "collection name (empty to clear)"

	return &Model{
		collectionInput: ci,
		sidebar:         true,
		preview:         true,
		cfgStore:        opts.Config,
		cfg:             cfg,
		st:              opts.Store,
		rec:             opts.Recorder,
		project:         opts.Project,
		search:          ti,
		editor:          ta,
		spinner:         sp,
		collapsed:       map[string]bool{},
		loading:         true,
		now:             time.Now(),
		width:           80,
		height:          24,
	}
}

// Init starts the first history load.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		loadEntries(m.st),
		m.spinner.Tick,
		tickNow(),
		checkForUpdate(m.cfg.Dir(), version.Version,
			m.cfg.Update.CheckInterval(), !m.cfg.Update.Disabled),
		loadEnvironments(),
	)
}

// Update routes a message to the active screen.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case spinner.TickMsg:
		if m.busy == "" && !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case nowMsg:
		m.now = time.Time(msg)
		return m, tickNow()

	case entriesMsg:
		return m, m.applyEntries(msg)

	case envLoadedMsg:
		m.envSet = msg.set
		return m, nil

	case bodiesMsg:
		m.detail.setBodies(msg)
		if m.pendingEditBody && m.screen == screenEdit && msg.id == m.edit.entryID {
			m.pendingEditBody = false
			m.edit.have.Body = string(msg.request)
			m.edit.form.Body = string(msg.request)
		}
		m.layout()
		return m, nil

	case replayMsg:
		return m, m.applyReplay(msg)

	case diffResultMsg:
		if msg.err != nil {
			m.flashErr("could not compare: " + msg.err.Error())
			return m, clearStatus(m.statusTok)
		}
		m.diffTitle = msg.title
		m.status, m.statusErr = "", false // the "marked" hint has served its purpose
		m.diffVP.SetContent(msg.body)
		m.diffVP.GotoTop()
		m.screen = screenDiff
		m.layout()
		return m, nil

	case mutationMsg:
		m.busy = ""
		if msg.err != nil {
			m.flashErr(msg.err.Error())
			return m, nil
		}
		if msg.status != "" {
			m.flash(msg.status)
		}
		return m, loadEntries(m.st)

	case copiedMsg:
		m.busy = ""
		if msg.err != nil {
			m.flashErr("copy failed: " + msg.err.Error())
		} else {
			m.flash(msg.what + " copied")
		}
		return m, nil

	case updateAvailableMsg:
		m.updateVersion = msg.version
		return m, nil

	case updateDoneMsg:
		m.updating = false
		m.busy = ""
		switch {
		case errors.Is(msg.err, selfupdate.ErrUpToDate):
			m.updateVersion = ""
			m.flash("already up to date")
		case msg.err != nil:
			m.flashErr("update failed: " + msg.err.Error())
		default:
			m.updateVersion = ""
			m.flash(fmt.Sprintf("updated to %s — restart pogo to run the new version", msg.result.To))
		}
		return m, clearStatus(m.statusTok)

	case statusClearMsg:
		if int(msg) == m.statusTok {
			m.status, m.statusErr = "", false
		}
		return m, nil

	case editorDoneMsg:
		if msg.err != nil {
			m.flashErr(msg.err.Error())
			return m, nil
		}
		m.editor.SetValue(msg.text)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// applyEntries installs a freshly loaded history, keeping the selection on the
// same entry when it still exists so a background reload does not move the
// cursor under the user's hands.
func (m *Model) applyEntries(msg entriesMsg) tea.Cmd {
	m.loading = false
	m.loadErr = msg.err
	if msg.err != nil {
		return nil
	}

	selected := m.selectedID()
	m.entries = msg.entries
	m.skipped = msg.skipped
	m.rebuildRows()
	m.buildRail()
	if m.pendingSelect != "" {
		selected, m.pendingSelect = m.pendingSelect, ""
	}
	if selected != "" {
		m.selectID(selected)
	}
	m.layout()

	if m.screen == screenDetail {
		return m.loadDetail()
	}
	return nil
}

func (m *Model) applyReplay(msg replayMsg) tea.Cmd {
	m.busy = ""
	if msg.err != nil {
		m.flashErr(msg.err.Error())
		return nil
	}
	// Arm the comparison against what was replayed. Doing it here rather than
	// asking the user to mark both sides turns the most common follow-up —
	// "what changed?" — into a single keypress, and the hint says so.
	summary := msg.summary
	if m.replaySource != nil && m.diffA == nil && msg.id != "" {
		m.diffA = m.replaySource
		summary += styFaint.Render("   press ") + styKey.Render("d") + styFaint.Render(" to compare with the original")
	}
	m.replaySource = nil

	m.flash(summary)
	m.pendingSelect = msg.id
	return tea.Batch(loadEntries(m.st), clearStatus(m.statusTok))
}

// layout recomputes child component sizes for the current terminal size.
func (m *Model) layout() {
	innerW, _ := m.frameSize()
	m.search.Width = maxInt(20, innerW-2)

	bodyH := m.contentHeight()
	m.detail.resize(m.detailWidth(), bodyH)

	m.diffVP.Width = innerW
	m.diffVP.Height = bodyH

	m.editor.SetWidth(maxInt(20, innerW-2))
	m.editor.SetHeight(maxInt(3, bodyH-8))
}

// previewWidth returns the width of the list's side preview, or 0 when the
// terminal is too narrow to justify one. Below this width the list itself needs
// every column it can get.
// showPreview reports whether the preview pane is both wanted and affordable.
func (m *Model) showPreview() bool {
	return m.preview && m.width >= minPreviewWidth && m.screen == screenList
}

func (m *Model) previewWidth() int {
	if !m.showPreview() {
		return 0
	}
	innerW, _ := m.frameSize()
	// Wide enough for a JSON line and a definition list, and never so wide
	// that the list it is previewing loses its path column.
	return clampInt(innerW/3, 40, 58)
}

func (m *Model) detailWidth() int {
	innerW, _ := m.frameSize()
	if w := m.previewWidth(); w > 0 && m.screen == screenList {
		return w - 2
	}
	return innerW
}

func (m *Model) listWidth() int {
	innerW, _ := m.frameSize()
	w := innerW - m.sidebarWidth()
	if p := m.previewWidth(); p > 0 {
		w -= p + 1
	}
	return maxInt(20, w)
}

// --- selection -------------------------------------------------------------

func (m *Model) selected() *history.Entry {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].entry
}

func (m *Model) selectedID() string {
	if e := m.selected(); e != nil {
		return e.ID
	}
	return ""
}

func (m *Model) selectID(id string) {
	for i, r := range m.rows {
		if r.entry != nil && r.entry.ID == id {
			m.cursor = i
			m.ensureVisible()
			return
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = maxInt(0, len(m.rows)-1)
	}
}

func (m *Model) entryByID(id string) *history.Entry {
	for _, e := range m.entries {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// --- status ----------------------------------------------------------------

func (m *Model) flash(text string)    { m.setStatus(text, false) }
func (m *Model) flashErr(text string) { m.setStatus(text, true) }

func (m *Model) setStatus(text string, isErr bool) {
	m.status, m.statusErr = text, isErr
	m.statusTok++
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
