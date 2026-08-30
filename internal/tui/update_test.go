package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmpato/pogo/internal/history"
	"github.com/rmpato/pogo/internal/selfupdate"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
}

func sampleEntries() []*history.Entry {
	mk := func(method, url string, status int, ago time.Duration) *history.Entry {
		e := &history.Entry{
			ID:        history.NewID(),
			CreatedAt: time.Now().Add(-ago),
			Source:    history.SourceRun,
			Command:   history.Command{Args: []string{"-X", method, url}},
			Request:   history.Request{Method: method, URL: url},
			Duration:  history.Duration(20 * time.Millisecond),
		}
		if status > 0 {
			e.Response = &history.Response{Blocks: []history.Block{{Status: status, Reason: "OK"}}}
		}
		return e
	}
	return []*history.Entry{
		mk("GET", "https://api.foo.com/users", 200, time.Second),
		mk("POST", "https://api.foo.com/login", 201, 2*time.Minute),
		mk("GET", "https://api.bar.com/things/42", 404, 5*time.Minute),
		mk("DELETE", "https://api.bar.com/things/41", 204, 8*time.Minute),
	}
}

func TestNavigationMovesSelection(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	if got := m.selected().Request.URL; !strings.Contains(got, "/users") {
		t.Fatalf("selection starts at %q, want the newest request", got)
	}

	press(m, "j")
	if !strings.Contains(m.selected().Request.URL, "/login") {
		t.Errorf("j should move down, selection is %q", m.selected().Request.URL)
	}
	press(m, "k")
	if !strings.Contains(m.selected().Request.URL, "/users") {
		t.Errorf("k should move up, selection is %q", m.selected().Request.URL)
	}

	// Movement must clamp rather than wrap or run off the ends. The top row is
	// a group header, and headers are labels rather than things you select, so
	// "as far up as it goes" is the first request under the first heading.
	press(m, "k", "k", "k")
	if m.rows[m.cursor].header {
		t.Errorf("cursor = %d, which is a group header", m.cursor)
	}
	if !strings.Contains(m.selected().Request.URL, "/users") {
		t.Errorf("the top selection is %q, want the newest request", m.selected().Request.URL)
	}
	press(m, "G")
	if m.cursor != len(m.rows)-1 {
		t.Errorf("G should jump to the last row, cursor = %d", m.cursor)
	}
	press(m, "j", "j")
	if m.cursor != len(m.rows)-1 {
		t.Errorf("cursor ran past the end: %d", m.cursor)
	}
}

func TestEnterOpensAndEscapeCloses(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	press(m, "enter")
	if m.screen != screenDetail {
		t.Fatal("enter should open the detail screen")
	}
	press(m, "esc")
	if m.screen != screenList {
		t.Error("esc should go back to the list")
	}
}

func TestDetailTabSwitching(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	press(m, "enter")

	press(m, "tab")
	if m.detail.tab != tabRequest {
		t.Errorf("tab = %v, want Request", m.detail.tab)
	}
	press(m, "4")
	if m.detail.tab != tabTiming {
		t.Errorf("4 should jump to Timing, got %v", m.detail.tab)
	}
	// Cycling past the last tab wraps around rather than sticking.
	press(m, "5", "tab")
	if m.detail.tab != tabOverview {
		t.Errorf("tab should wrap to Overview, got %v", m.detail.tab)
	}
}

func TestSearchFiltersAsYouType(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	press(m, "/")
	if !m.searching {
		t.Fatal("/ should open the search input")
	}

	press(m, "l", "o", "g", "i", "n")
	if got := m.visibleEntries(); got != 1 {
		t.Errorf("%d entries match \"login\", want 1", got)
	}
	if !strings.Contains(m.selected().Request.URL, "/login") {
		t.Errorf("selection should follow the filter, got %q", m.selected().Request.URL)
	}

	press(m, "enter")
	if m.searching {
		t.Error("enter should close the search input but keep the filter")
	}
	if m.visibleEntries() != 1 {
		t.Error("the filter should survive closing the input")
	}

	press(m, "esc")
	if m.visibleEntries() != len(sampleEntries()) {
		t.Error("esc on the list should clear the filter")
	}
}

func TestSearchWithStructuredFilter(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	press(m, "/")
	for _, r := range "status:4xx" {
		press(m, string(r))
	}
	if got := m.visibleEntries(); got != 1 {
		t.Errorf("%d entries match status:4xx, want 1", got)
	}
}

// History arrives grouped by API, because that is how the requests were
// actually made — api.foo.com and api.bar.com are two APIs, not one river.
func TestGroupingByAPIIsTheDefault(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	if m.group != groupAPI {
		t.Fatalf("default grouping = %v, want by API", m.group)
	}
	var headers []string
	for _, r := range m.rows {
		if r.header {
			headers = append(headers, r.group)
		}
	}
	if len(headers) != 2 {
		t.Fatalf("got %d API headings %v, want 2", len(headers), headers)
	}
	for _, h := range headers {
		if !strings.Contains(h, "prod") {
			t.Errorf("heading %q should name the environment it reached", h)
		}
	}
	// The heading is the API, not the host it was reached through.
	if !strings.Contains(headers[0], "foo.com") || strings.Contains(headers[0], "api.foo.com") {
		t.Errorf("heading = %q, want the registrable domain", headers[0])
	}
}

func TestGroupingByHost(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	press(m, "t", "t")
	if m.group != groupHost {
		t.Fatalf("t twice should reach grouping by host, got %v", m.group)
	}

	var headers, entries int
	for _, r := range m.rows {
		if r.header {
			headers++
		} else {
			entries++
		}
	}
	if headers != 2 {
		t.Errorf("got %d group headers, want 2 (api.foo.com and api.bar.com)", headers)
	}
	if entries != 4 {
		t.Errorf("got %d entry rows, want 4", entries)
	}

	// Space folds the group the cursor is in. Headings are labels rather than
	// rows you land on, so folding hangs off the request under the cursor.
	press(m, " ")
	if m.screen == screenDetail {
		t.Fatal("space should fold, not inspect")
	}
	var afterFold int
	for _, r := range m.rows {
		if !r.header {
			afterFold++
		}
	}
	if afterFold != 2 {
		t.Errorf("%d entries visible after folding one group, want 2", afterFold)
	}

	// And it opens again, leaving the cursor on a request either way.
	press(m, " ")
	if m.selected() == nil {
		t.Error("unfolding should leave a request selected")
	}

	// z folds everything, and then opens everything.
	press(m, "z")
	for _, r := range m.rows {
		if !r.header {
			t.Fatal("z should have collapsed every group")
		}
	}
	press(m, "z")
	if m.selected() == nil {
		t.Error("z again should reopen the groups")
	}

	press(m, "t")
	if m.group != groupCollection {
		t.Error("t again should group by collection")
	}
	press(m, "t")
	if m.group != groupAPI {
		t.Error("t should cycle back round to grouping by API")
	}
}

// Collections are the promised evolution of stars: a name you can file a
// request under, filter by, and group by.
func TestCollections(t *testing.T) {
	entries := sampleEntries()
	m := newTestModel(t, entries...)
	id := m.selectedID()

	press(m, "c")
	if m.overlay != overlayCollection {
		t.Fatal("c should open the collection prompt")
	}
	for _, r := range "auth" {
		press(m, string(r))
	}
	press(m, "enter")
	if m.overlay != overlayNone {
		t.Error("enter should close the prompt")
	}

	// Apply the mutation the way the runtime would, then reload.
	if err := m.st.SetCollection(id, "auth"); err != nil {
		t.Fatal(err)
	}
	res, _ := m.st.Load()
	m.Update(entriesMsg{entries: res.Entries})

	var filed *history.Entry
	for _, e := range m.entries {
		if e.ID == id {
			filed = e
		}
	}
	if filed == nil || filed.Collection != "auth" {
		t.Fatalf("entry was not filed: %+v", filed)
	}

	// It is findable by filter...
	if !ParseQuery("collection:auth").Match(filed) {
		t.Error("collection:auth should match the filed entry")
	}
	if ParseQuery("collection:auth").Match(m.entries[len(m.entries)-1]) {
		t.Error("collection:auth should not match an unfiled entry")
	}

	// ...and it groups.
	m.group = groupCollection
	m.rebuildRows()
	var headers []string
	for _, r := range m.rows {
		if r.header {
			headers = append(headers, r.group)
		}
	}
	if len(headers) != 2 {
		t.Fatalf("group headers = %v, want auth and the unfiled group", headers)
	}
}

func TestDeleteAsksForConfirmation(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	press(m, "x")
	if m.overlay != overlayConfirm {
		t.Fatal("x should ask before deleting")
	}
	if !strings.Contains(m.View(), "Delete this request?") {
		t.Error("the confirmation should be visible")
	}

	press(m, "n") // any key other than y cancels
	if m.overlay != overlayNone {
		t.Error("a non-confirming key should cancel")
	}
	if len(m.entries) != 4 {
		t.Error("nothing should have been deleted")
	}
}

func TestCopyMenuOpensAndCloses(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	press(m, "y")
	if m.overlay != overlayCopy {
		t.Fatal("y should open the copy menu")
	}
	view := m.View()
	for _, want := range []string{"curl command", "URL", "response body"} {
		if !strings.Contains(view, want) {
			t.Errorf("copy menu is missing %q", want)
		}
	}

	press(m, "down")
	if m.copyCursor != 1 {
		t.Errorf("copyCursor = %d, want 1", m.copyCursor)
	}
	press(m, "esc")
	if m.overlay != overlayNone {
		t.Error("esc should close the copy menu")
	}
}

func TestStarMarksEntry(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	id := m.selectedID()

	press(m, "s")
	// The mutation runs as a command; apply its effect the way the runtime does.
	if err := m.st.SetFavorite(id, true); err != nil {
		t.Fatal(err)
	}
	res, _ := m.st.Load()
	m.Update(entriesMsg{entries: res.Entries})

	found := false
	for _, e := range m.entries {
		if e.ID == id && e.Favorite {
			found = true
		}
	}
	if !found {
		t.Error("the entry should come back starred")
	}
}

func TestRevealTogglesSecretMasking(t *testing.T) {
	e := sampleEntries()[0]
	e.Request.URL = "https://api.foo.com/users?access_token=supersecret"
	m := newTestModel(t, e)

	if strings.Contains(m.View(), "supersecret") {
		t.Error("the token should be masked by default")
	}
	press(m, "S")
	if !strings.Contains(m.View(), "supersecret") {
		t.Error("S should reveal the real value")
	}
	if !strings.Contains(m.View(), "secrets visible") {
		t.Error("revealing secrets should be visible in the header")
	}
	press(m, "S")
	if strings.Contains(m.View(), "supersecret") {
		t.Error("S should hide secrets again")
	}
}

func TestDiffMarksThenCompares(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	press(m, "d")
	if m.diffA == nil {
		t.Fatal("d should mark the selected request for comparison")
	}
	if !strings.Contains(m.View(), "compare") {
		t.Error("the marked request should be shown in the header")
	}

	// Pressing d on the same request clears the mark rather than diffing it
	// against itself.
	press(m, "d")
	if m.diffA != nil {
		t.Error("d on the same entry should clear the mark")
	}
}

func TestEditOpensWithTheRealCommand(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	press(m, "e")
	if m.screen != screenEdit {
		t.Fatal("e should open the editor")
	}
	if !strings.Contains(m.editor.Value(), "api.foo.com/users") {
		t.Errorf("editor should hold the request's command, got %q", m.editor.Value())
	}
	if !strings.Contains(m.editor.Value(), "curl") {
		t.Error("the editable representation should be a curl command")
	}

	press(m, "esc")
	if m.screen != screenList {
		t.Error("esc should leave the editor without running anything")
	}
}

// Editing must never write back to the entry it started from.
func TestEditDoesNotMutateOriginal(t *testing.T) {
	entries := sampleEntries()
	m := newTestModel(t, entries...)
	before := strings.Join(entries[0].Command.Args, " ")

	press(m, "e")
	m.editor.SetValue("curl https://api.foo.com/somewhere-else")

	if got := strings.Join(entries[0].Command.Args, " "); got != before {
		t.Errorf("the original command changed: %q -> %q", before, got)
	}
}

func TestNewOpensABlankEditor(t *testing.T) {
	m := newTestModel(t) // an empty history: composing must not need a selection

	press(m, "n")
	if m.screen != screenEdit {
		t.Fatal("n should open the editor")
	}
	if m.editID != "" {
		t.Error("a new request has no parent entry")
	}
	if !strings.Contains(m.View(), "NEW REQUEST") {
		t.Error("the heading should say this is a new request, not an edit")
	}
	if m.edit.form.Method != "GET" {
		t.Errorf("method = %q, want GET", m.edit.form.Method)
	}
}

// n defaults to filing under whatever the list is already narrowed to — the
// project pogo was opened with, or a single api: filter — so composing from
// inside a project does not require retyping it.
func TestNewDefaultsToTheActiveProjectFilter(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	m.applyFilter("api:checkout")

	press(m, "n")
	if m.edit.api != "checkout" {
		t.Errorf("edit.api = %q, want checkout", m.edit.api)
	}
}

// A project declared with a base URL prefills it, so the common case —
// hitting the same local service repeatedly — is nothing but a path to type.
func TestNewPrefillsTheProjectsBaseURL(t *testing.T) {
	m := newTestModel(t)
	m.project = "checkout"
	m.cfg.APIs.SetProject("checkout", "http://localhost:3000")

	press(m, "n")
	if m.edit.form.URL != "http://localhost:3000" {
		t.Errorf("prefilled URL = %q, want the project's base", m.edit.form.URL)
	}
}

// Composing a blank request and running it takes the same path an edited one
// does: back to the list, busy, with the run handed off as a command.
func TestNewRunsLikeAnEditedRequest(t *testing.T) {
	m := newTestModel(t)

	press(m, "n")
	m.edit.form.URL = "http://localhost:3000/orders"
	press(m, "ctrl+r")

	if m.screen != screenList {
		t.Error("running should return to the list")
	}
	if m.busy == "" {
		t.Error("running should show a busy indicator while it is in flight")
	}
}

func TestEmptyHistoryShowsGuidance(t *testing.T) {
	m := newTestModel(t)
	view := m.View()

	if !strings.Contains(view, "No requests yet") {
		t.Error("an empty history should say so")
	}
	if !strings.Contains(view, "pogo https://") {
		t.Error("the empty state should show how to record the first request")
	}
}

func TestNoMatchesShowsRecovery(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	press(m, "/")
	for _, r := range "zzzznope" {
		press(m, string(r))
	}
	view := m.View()
	if !strings.Contains(view, "Nothing matches") {
		t.Error("an empty result should be explained")
	}
	if !strings.Contains(view, "esc") {
		t.Error("the empty result should say how to get back")
	}
}

func TestLoadErrorIsShown(t *testing.T) {
	m := newTestModel(t)
	m.Update(entriesMsg{err: errTest})

	if !strings.Contains(m.View(), "history unreadable") {
		t.Error("a load failure should be visible in the header")
	}
	if !strings.Contains(m.View(), errTest.Error()) {
		t.Error("the reason should be shown, not swallowed")
	}
}

func TestDamagedRecordsAreReported(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	m.Update(entriesMsg{entries: sampleEntries(), skipped: 3})

	if !strings.Contains(m.View(), "3 damaged records skipped") {
		t.Error("skipped records should be surfaced rather than hidden")
	}
}

func TestReplayResultIsReported(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	m.busy = "replaying"

	m.Update(replayMsg{id: "NEW", summary: "→ 200 OK · 18ms"})

	if m.busy != "" {
		t.Error("the busy indicator should clear when the replay finishes")
	}
	if !strings.Contains(m.View(), "→ 200 OK · 18ms") {
		t.Error("the replay result should be shown immediately")
	}
	if m.pendingSelect != "NEW" {
		t.Error("the new entry should be selected once history reloads")
	}
}

func TestReplayFailureIsReported(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	m.Update(replayMsg{err: errTest})

	if !strings.Contains(m.View(), errTest.Error()) {
		t.Error("a failed replay should say why")
	}
}

func TestHelpScreenListsShortcuts(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	press(m, "?")
	if m.screen != screenHelp {
		t.Fatal("? should open help")
	}
	// Case-insensitive: the reference is generated from command titles, which
	// are capitalized. What matters is that the concept is present.
	view := strings.ToLower(m.View())
	for _, want := range []string{"replay", "status:4xx", "is:starred", "$editor", "palette"} {
		if !strings.Contains(view, want) {
			t.Errorf("help does not mention %q", want)
		}
	}

	press(m, "esc")
	if m.screen != screenList {
		t.Error("esc should leave help")
	}
}

// A terminal smaller than the UI can honestly use should say so rather than
// render a scrambled frame.
func TestTinyTerminalIsHandled(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	m.Update(tea.WindowSizeMsg{Width: 20, Height: 5})

	if view := m.View(); !strings.Contains(view, "bigger") {
		t.Errorf("expected a clear message, got %q", view)
	}
}

var errTest = &testError{"boom"}

type testError struct{ s string }

func (e *testError) Error() string { return e.s }

// Updating rewrites the binaries on disk, so it must never happen without an
// explicit yes — and the dialog must say where it is about to write.
func TestUpdateAlwaysAsksFirst(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)

	// With nothing to update, the key is inert rather than confusing.
	press(m, "u")
	if m.overlay != overlayNone {
		t.Error("u should do nothing when no update is available")
	}

	m.Update(updateAvailableMsg{version: "9.9.9"})
	if !strings.Contains(m.View(), "update 9.9.9") {
		t.Error("an available release should be visible in the header")
	}

	press(m, "u")
	if m.overlay != overlayUpdate {
		t.Fatal("u should open the confirmation")
	}
	view := m.View()
	for _, want := range []string{"Update available", "9.9.9", "Replaces pogo in", "checksums"} {
		if !strings.Contains(view, want) {
			t.Errorf("the confirmation should mention %q", want)
		}
	}

	press(m, "n")
	if m.overlay != overlayNone || m.updating {
		t.Error("declining must not start an update")
	}
}

func TestUpdateResultIsReported(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	m.Update(updateAvailableMsg{version: "9.9.9"})
	m.updating, m.busy = true, "updating"

	m.Update(updateDoneMsg{result: selfupdate.Result{To: "9.9.9", Updated: []string{"pogo", "pogo"}}})

	if m.updating || m.busy != "" {
		t.Error("the busy indicator should clear when the update finishes")
	}
	if !strings.Contains(m.View(), "restart pogo") {
		t.Error("the user should be told the running process is still the old one")
	}
	if m.updateVersion != "" {
		t.Error("the badge should clear once the update is installed")
	}
}

func TestUpdateFailureIsReported(t *testing.T) {
	m := newTestModel(t, sampleEntries()...)
	m.Update(updateDoneMsg{err: errTest})

	if !strings.Contains(m.View(), "update failed") {
		t.Error("a failed update should say so")
	}
}
