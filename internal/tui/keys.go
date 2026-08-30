package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds every binding in the application.
//
// Bindings are declared once, with their help text attached, so the footer and
// the help screen are generated from the same source as the behavior. A
// shortcut cannot silently drift out of the documentation.
type keyMap struct {
	Up     key.Binding
	Down   key.Binding
	Top    key.Binding
	Bottom key.Binding
	PageUp key.Binding
	PageDn key.Binding

	Enter key.Binding
	Back  key.Binding
	Quit  key.Binding
	Help  key.Binding

	Replay key.Binding
	Edit   key.Binding
	New    key.Binding
	Search key.Binding
	Copy   key.Binding
	Star   key.Binding
	Delete key.Binding
	Diff   key.Binding
	Group  key.Binding
	Reveal key.Binding

	NextTab key.Binding
	PrevTab key.Binding
	Body    key.Binding
	Toggle  key.Binding

	Run        key.Binding
	Editor     key.Binding
	EditToggle key.Binding
	EditDelete key.Binding

	Env        key.Binding
	Collection key.Binding
	Palette    key.Binding
	Sidebar    key.Binding
	Update     key.Binding
	APIs       key.Binding
	Home       key.Binding
	FoldAll    key.Binding
	Preview    key.Binding
}

var keys = keyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Top:    key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
	Bottom: key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
	PageUp: key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("ctrl+u", "page up")),
	PageDn: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("ctrl+d", "page down")),

	Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "inspect")),
	Back:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:  key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:  key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),

	Replay: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "replay")),
	Edit:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
	New:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new request")),
	Search: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Copy:   key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy")),
	Star:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "star")),
	Delete: key.NewBinding(key.WithKeys("x", "delete"), key.WithHelp("x", "delete")),
	Diff:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
	Group:  key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "group")),
	Reveal: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "reveal secrets")),

	NextTab: key.NewBinding(key.WithKeys("tab", "l", "right"), key.WithHelp("tab", "next pane")),
	PrevTab: key.NewBinding(key.WithKeys("shift+tab", "h", "left"), key.WithHelp("⇧tab", "prev pane")),
	Body:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "body view")),
	Toggle:  key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "fold")),

	Run:        key.NewBinding(key.WithKeys("ctrl+r", "ctrl+enter"), key.WithHelp("ctrl+r", "run")),
	Editor:     key.NewBinding(key.WithKeys("ctrl+e"), key.WithHelp("ctrl+e", "$EDITOR")),
	EditToggle: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "raw / fields")),
	EditDelete: key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "remove row")),

	Env:        key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "environment")),
	Palette:    key.NewBinding(key.WithKeys("ctrl+k", ":"), key.WithHelp("ctrl+k", "commands")),
	Sidebar:    key.NewBinding(key.WithKeys("\\"), key.WithHelp("\\", "sidebar")),
	Update:     key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update")),
	Collection: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "collection")),
	APIs:       key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "APIs")),
	Home:       key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "home")),
	FoldAll:    key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "fold all")),
	Preview:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "preview")),
}

// hint is one footer entry.
type hint struct {
	key  string
	desc string
}

// footerHints returns the bindings worth advertising for the current screen.
// The footer is the discoverability mechanism, so it shows what is useful here
// and now rather than everything the program can do.
func (m *Model) footerHints() []hint {
	switch m.overlay {
	case overlayPalette:
		return []hint{{"↑↓", "choose"}, {"⏎", "run"}, {"esc", "close"}}
	case overlayCopy:
		return []hint{{"↑↓", "choose"}, {"⏎", "copy"}, {"a–j", "pick directly"}, {"esc", "cancel"}}
	case overlayConfirm:
		return []hint{{"y", "delete"}, {"any other key", "cancel"}}
	case overlayUpdate:
		return []hint{{"y", "update"}, {"any other key", "not now"}}
	case overlayEnv:
		return []hint{{"↑↓", "choose"}, {"⏎", "activate"}, {"esc", "cancel"}}
	case overlayCollection, overlayAPIName:
		return []hint{{"⏎", "save"}, {"esc", "cancel"}}
	}

	switch m.screen {
	case screenDetail:
		return []hint{
			{"tab", "pane"}, {"v", "body view"}, {"r", "replay"}, {"e", "edit"},
			{"y", "copy"}, {"d", "diff"}, {"esc", "back"}, {"ctrl+k", "commands"},
		}
	case screenEdit:
		if m.edit.raw {
			return []hint{{"ctrl+r", "run"}, {"ctrl+t", "fields"}, {"ctrl+e", "$EDITOR"}, {"esc", "cancel"}}
		}
		if m.edit.inBody {
			return []hint{{"ctrl+r", "run"}, {"esc", "back to fields"}}
		}
		if m.edit.editing {
			return []hint{{"⏎", "accept"}, {"esc", "discard"}}
		}
		return []hint{
			{"↑↓", "field"}, {"⏎", "edit"}, {"ctrl+d", "remove"},
			{"ctrl+t", "raw"}, {"ctrl+r", "run"}, {"esc", "cancel"},
		}
	case screenDiff:
		return []hint{{"↑↓", "scroll"}, {"d", "clear"}, {"esc", "back"}, {"q", "quit"}}
	case screenAPIs:
		return []hint{
			{"↑↓", "choose"}, {"⏎", "show requests"}, {"p", "pin environment"},
			{"n", "name"}, {"x", "hide"}, {"esc", "back"},
		}
	case screenSettings:
		return []hint{{"↑↓", "choose"}, {"⏎", "change"}, {"esc", "back"}}
	case screenHelp:
		if m.helpMax > 0 {
			return []hint{{"↑↓", "scroll"}, {"esc", "back"}, {"q", "quit"}}
		}
		return []hint{{"esc", "back"}, {"q", "quit"}}
	default:
		if m.searching {
			return []hint{{"⏎", "apply"}, {"esc", "cancel"}}
		}
		if m.focus == focusSidebar && m.showSidebar() {
			return []hint{
				{"↑↓", "choose"}, {"⏎", "filter"}, {"tab", "back to list"},
				{"ctrl+k", "commands"},
			}
		}
		hints := []hint{
			{"↑↓", "navigate"}, {"⏎", "inspect"}, {"r", "replay"}, {"e", "edit"},
			{"n", "new"}, {"/", "search"}, {"A", "APIs"},
		}
		if m.showSidebar() {
			hints = append(hints, hint{"tab", "sidebar"})
		}
		if m.width >= minPreviewWidth {
			hints = append(hints, hint{"p", "preview"})
		}
		// The palette is advertised on every screen: it is the answer to "what
		// else can this do?", and it only helps if it is visible. `?` is not
		// listed here because the footer already ends with the help hint.
		return append(hints, hint{"ctrl+k", "commands"})
	}
}
