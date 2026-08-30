package tui

import tea "github.com/charmbracelet/bubbletea"

// command is one thing pogo can do.
//
// Actions are data rather than a switch statement because three separate parts
// of the UI need the same list: the key handler dispatches into it, the palette
// searches it, and the help screen is generated from it. When those three drift
// apart the result is a feature nobody can find — which is the problem this
// whole registry exists to prevent.
type command struct {
	id    string
	title string
	desc  string
	keys  string
	group string

	// motion marks navigation that belongs in the help reference but would only
	// be noise in a palette: nobody searches for "move down".
	motion bool

	// when reports whether the command applies right now. Unavailable commands
	// are shown greyed rather than hidden, so the palette teaches what exists
	// even when it cannot be used yet.
	when func(*Model) bool

	run func(*Model) tea.Cmd
}

func (c command) available(m *Model) bool { return c.when == nil || c.when(m) }

// hasSelection is the usual precondition: most commands act on a request.
func hasSelection(m *Model) bool { return m.selected() != nil }

// commands is the single source of truth for what pogo can do.
func (m *Model) commands() []command {
	return []command{
		// --- the request under the cursor ---
		{
			id: "inspect", group: "Request", keys: "⏎",
			title: "Inspect request",
			desc:  "request, response, headers, bodies, timing",
			when:  hasSelection,
			run:   func(m *Model) tea.Cmd { return m.doInspect() },
		},
		{
			id: "replay", group: "Request", keys: "r",
			title: "Replay request",
			desc:  "run it again, recorded as a new entry",
			when:  hasSelection,
			run:   func(m *Model) tea.Cmd { return m.doReplay() },
		},
		{
			id: "edit", group: "Request", keys: "e",
			title: "Edit and run",
			desc:  "change method, URL, query, headers or body, then run it",
			when:  hasSelection,
			run:   func(m *Model) tea.Cmd { return m.doEdit() },
		},
		{
			id: "new", group: "Request", keys: "n",
			title: "New request",
			desc:  "compose a blank one, filed under a project or API",
			run:   func(m *Model) tea.Cmd { return m.doNew() },
		},
		{
			id: "copy", group: "Request", keys: "y",
			title: "Copy…",
			desc:  "curl command, URL, headers, request or response body",
			when:  hasSelection,
			run:   func(m *Model) tea.Cmd { return m.doCopy() },
		},
		{
			id: "diff", group: "Request", keys: "d",
			title: "Compare with…",
			desc:  "mark this request, then press d on another to diff them",
			when:  hasSelection,
			run:   func(m *Model) tea.Cmd { return m.doDiff() },
		},
		{
			id: "delete", group: "Request", keys: "x",
			title: "Delete request",
			desc:  "remove it and its payloads from history",
			when:  hasSelection,
			run:   func(m *Model) tea.Cmd { return m.doDelete() },
		},

		// --- organizing ---
		{
			id: "star", group: "Organize", keys: "s",
			title: "Star / unstar",
			desc:  "keep the requests that matter; they survive the history cap",
			when:  hasSelection,
			run:   func(m *Model) tea.Cmd { return m.doStar() },
		},
		{
			id: "collection", group: "Organize", keys: "c",
			title: "Add to collection…",
			desc:  "file this request under a name you can filter and group by",
			when:  hasSelection,
			run:   func(m *Model) tea.Cmd { return m.doCollection() },
		},
		{
			id: "group", group: "Organize", keys: "t",
			title: "Change grouping",
			desc:  "by API, chronological, by host, by collection",
			run:   func(m *Model) tea.Cmd { return m.doGroup() },
		},
		{
			id: "fold", group: "Organize", keys: "space",
			title: "Fold this group",
			desc:  "collapse the API you are in, or open it again",
			when:  func(m *Model) bool { return m.group != groupNone },
			run:   func(m *Model) tea.Cmd { return m.doFold() },
		},
		{
			id: "fold-all", group: "Organize", keys: "z",
			title: "Fold every group",
			desc:  "collapse all of them, or open all of them",
			when:  func(m *Model) bool { return m.group != groupNone },
			run:   func(m *Model) tea.Cmd { return m.doFoldAll() },
		},
		{
			id: "apis", group: "Organize", keys: "A",
			title: "APIs and environments",
			desc:  "what pogo worked out about your hosts, and how to correct it",
			run:   func(m *Model) tea.Cmd { return m.doAPIs() },
		},
		{
			id: "pin-env", group: "Organize", keys: "",
			title: "Pin this host's environment",
			desc:  "stop pogo guessing: state which environment this host is",
			when:  hasSelection,
			run:   func(m *Model) tea.Cmd { return m.pinSelectedHost() },
		},

		// --- finding ---
		{
			id: "search", group: "Find", keys: "/",
			title: "Search…",
			desc:  "free text, or method: status: host: collection: is:",
			run:   func(m *Model) tea.Cmd { return m.doSearch() },
		},
		{
			id: "clear", group: "Find", keys: "esc",
			title: "Clear search",
			desc:  "show everything again",
			when:  func(m *Model) bool { return m.query.Raw != "" },
			run:   func(m *Model) tea.Cmd { return m.doClearSearch() },
		},
		{
			id: "this-api", group: "Find", keys: "",
			title: "Show this API only",
			desc:  "everything that went to the same domain, in every environment",
			when:  hasSelection,
			run: func(m *Model) tea.Cmd {
				return m.applyFilter("api:" + m.domainOf(m.selected()))
			},
		},
		{
			id: "this-env", group: "Find", keys: "",
			title: "Show this API in this environment only",
			desc:  "the same domain, narrowed to the environment under the cursor",
			when:  hasSelection,
			run: func(m *Model) tea.Cmd {
				ref := m.apiRef(m.selected())
				return m.applyFilter("api:" + ref.Domain + " env:" + ref.Env)
			},
		},
		{
			id: "starred", group: "Find", keys: "",
			title: "Show starred only",
			desc:  "the same as searching is:starred",
			run:   func(m *Model) tea.Cmd { return m.applyFilter("is:starred") },
		},
		{
			id: "failed", group: "Find", keys: "",
			title: "Show failures only",
			desc:  "anything that failed or came back 400 or worse",
			run:   func(m *Model) tea.Cmd { return m.applyFilter("is:failed") },
		},

		// --- view ---
		{
			id: "sidebar", group: "View", keys: "\\",
			title: "Toggle sidebar",
			desc:  "filters, APIs and collections down the left",
			run:   func(m *Model) tea.Cmd { return m.doToggleSidebar() },
		},
		{
			id: "preview", group: "View", keys: "p",
			title: "Toggle preview",
			desc:  "the request under the cursor, down the right",
			run:   func(m *Model) tea.Cmd { return m.doTogglePreview() },
		},
		{
			id: "reveal", group: "View", keys: "S",
			title: "Reveal / hide secrets",
			desc:  "show masked tokens, cookies and API keys in full",
			run:   func(m *Model) tea.Cmd { return m.doReveal() },
		},
		{
			id: "body", group: "View", keys: "v",
			title: "Change body view",
			desc:  "JSON tree, pretty-printed, or raw bytes",
			when:  func(m *Model) bool { return m.screen == screenDetail },
			run:   func(m *Model) tea.Cmd { return m.doBodyMode() },
		},

		// --- application ---
		{
			id: "settings", group: "App", keys: "",
			title: "Settings",
			desc:  "theme, what gets redacted, release checks, where things live",
			run:   func(m *Model) tea.Cmd { return m.doSettings() },
		},
		{
			id: "theme", group: "App", keys: "",
			title: "Change theme",
			desc:  "cycle the palette; the choice is remembered",
			run:   func(m *Model) tea.Cmd { return m.cycleTheme() },
		},
		{
			id: "env", group: "App", keys: "E",
			title: "Switch environment…",
			desc:  "resolve {{variables}} from a different environment",
			run:   func(m *Model) tea.Cmd { return m.doEnv() },
		},
		{
			id: "update", group: "App", keys: "u",
			title: "Install update",
			desc:  "download and verify the newer release",
			when:  func(m *Model) bool { return m.updateVersion != "" },
			run:   func(m *Model) tea.Cmd { return m.doUpdate() },
		},
		{
			id: "home", group: "App", keys: "H",
			title: "Home",
			desc:  "the shell above the list: APIs, settings, the walkthrough",
			run:   func(m *Model) tea.Cmd { return m.doHome() },
		},
		{
			id: "help", group: "App", keys: "?",
			title: "Keyboard reference",
			desc:  "every key, grouped by what it is for",
			run:   func(m *Model) tea.Cmd { return m.doHelp() },
		},
		{
			id: "quit", group: "App", keys: "q",
			title: "Quit",
			desc:  "",
			run:   func(m *Model) tea.Cmd { return tea.Quit },
		},

		// --- inspecting and editing: keys that only exist inside a screen, so
		// they belong in the reference rather than in a palette of verbs ---
		{id: "panes", group: "View", keys: "tab / 1–5", title: "Move between panes", motion: true},
		{id: "json-fold", group: "View", keys: "space", title: "Fold a JSON node (inspecting)", motion: true},

		{id: "edit-field", group: "Edit", keys: "⏎", title: "Edit the focused field", motion: true},
		{id: "edit-method", group: "Edit", keys: "← →", title: "Change the method", motion: true},
		{id: "edit-remove", group: "Edit", keys: "ctrl+d", title: "Remove a header or parameter", motion: true},
		{id: "edit-raw", group: "Edit", keys: "ctrl+t", title: "Switch between fields and raw curl", motion: true},
		{id: "edit-editor", group: "Edit", keys: "ctrl+e", title: "Hand the command to $EDITOR", motion: true},
		{id: "edit-run", group: "Edit", keys: "ctrl+r", title: "Run it as a new entry", motion: true},

		// --- motions: in the reference, not in the palette ---
		{id: "api-name", group: "Organize", keys: "n", title: "Name the API (on the APIs screen)", motion: true},
		{id: "api-pin", group: "Organize", keys: "p", title: "Pin its hosts (on the APIs screen)", motion: true},
		{id: "api-hide", group: "Organize", keys: "x", title: "Hide the API (on the APIs screen)", motion: true},

		{id: "up", group: "Navigate", keys: "↑ / k", title: "Previous request", motion: true},
		{id: "down", group: "Navigate", keys: "↓ / j", title: "Next request", motion: true},
		{id: "top", group: "Navigate", keys: "g", title: "Jump to the newest", motion: true},
		{id: "bottom", group: "Navigate", keys: "G", title: "Jump to the oldest", motion: true},
		{id: "page", group: "Navigate", keys: "ctrl+u / ctrl+d", title: "Half page up / down", motion: true},
		{id: "focus", group: "Navigate", keys: "tab", title: "Move between sidebar and list", motion: true},
		{id: "back", group: "Navigate", keys: "esc", title: "Back, or close what is open", motion: true},
		{id: "palette", group: "Navigate", keys: "ctrl+k", title: "Command palette", motion: true},
	}
}

// paletteItems returns the commands the palette offers, most useful first.
func (m *Model) paletteItems() []command {
	var out []command
	for _, c := range m.commands() {
		if !c.motion {
			out = append(out, c)
		}
	}
	return out
}

// Ranking lives in internal/ui: one fuzzy matcher for the palette, the `/`
// filter and every picker, so a search that works in one place works the same
// in the others. See ui.FuzzyMatch and internal/tui/palette.go.
