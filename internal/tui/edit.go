package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/rmpato/pogo/internal/curlargs"
	"github.com/rmpato/pogo/internal/curledit"
	"github.com/rmpato/pogo/internal/environment"
	"github.com/rmpato/pogo/internal/history"
)

// editKind identifies what a row in the editor edits.
type editKind int

const (
	editMethod editKind = iota
	editURL
	editQuery
	editHeader
	editBody
	editAddQuery
	editAddHeader
	editSection // a non-focusable heading
)

// editRow is one line of the structured editor.
type editRow struct {
	kind  editKind
	index int    // position within Query or Headers
	label string // for section headings
}

func (r editRow) focusable() bool { return r.kind != editSection }

// methods are offered in the order people reach for them.
var methods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// editState holds everything the editor needs.
//
// The editor works on a Form, but the *command* stays authoritative: on run,
// the difference between the original form and the edited one is applied to the
// original argv. That is what keeps --cacert, --resolve and every other option
// pogo does not model from vanishing when someone changes a header.
type editState struct {
	entryID string
	args    []string      // the original command, untouched
	have    curledit.Form // the form as read from args
	form    curledit.Form // the form being edited

	// api is the project or API a blank request will be filed under when it
	// runs. It is empty for a request opened from an existing entry, whose
	// domain is read off that entry instead; see Model.recorderFor.
	api string

	rows   []editRow
	cursor int

	input   textinput.Model
	editing bool

	raw    bool // editing the curl command as text instead of fields
	inBody bool // the body textarea has focus
}

// startEdit opens the editor on a copy of an entry's request.
//
// Payloads live outside the index, so the body may not be in hand yet; the
// returned command fetches it and the field fills in when it arrives.
func (m *Model) startEdit(e *history.Entry) tea.Cmd {
	if e == nil {
		return nil
	}

	spec := curlargs.Parse(e.Command.Args)
	body := string(m.detail.reqBody)
	if m.detail.entryID != e.ID {
		body = "" // payload not loaded yet; the field fills in when it arrives
	}

	form := curledit.FormOf(spec, body)

	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 0

	m.edit = editState{
		entryID: e.ID,
		args:    append([]string(nil), e.Command.Args...),
		have:    form,
		form:    cloneForm(form),
		input:   in,
	}
	m.edit.rebuild()

	m.editID = e.ID
	m.screen = screenEdit
	m.editor.SetValue(e.Command.Multiline())
	m.layout()

	if m.detail.entryID == e.ID && m.detail.loaded {
		return nil
	}
	m.pendingEditBody = true
	m.detail.entryID = e.ID
	m.detail.loaded = false
	m.detail.req.reset()
	m.detail.res.reset()
	return loadBodies(m.st, e)
}

// startNewRequest opens the editor on a blank request, to be filed under api
// when it runs — the entry point for composing something pogo has never seen,
// rather than changing something it already recorded. base prefills the URL
// when the project it belongs to has one on file.
func (m *Model) startNewRequest(api, base string) tea.Cmd {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 0

	m.edit = editState{
		have:  curledit.Form{Method: "GET"},
		form:  curledit.Form{Method: "GET", URL: base},
		api:   api,
		input: in,
	}
	m.edit.rebuild()
	m.edit.cursor = 1 // the URL row; the method is rarely what needs typing first

	m.editID = ""
	m.screen = screenEdit
	m.editor.SetValue("")
	m.layout()
	return nil
}

func cloneForm(f curledit.Form) curledit.Form {
	c := f
	c.Query = append([]curledit.Param(nil), f.Query...)
	c.Headers = append([]curlargs.Header(nil), f.Headers...)
	return c
}

// rebuild recomputes the row list after a structural change.
func (s *editState) rebuild() {
	rows := []editRow{
		{kind: editMethod},
		{kind: editURL},
		{kind: editSection, label: "QUERY"},
	}
	for i := range s.form.Query {
		rows = append(rows, editRow{kind: editQuery, index: i})
	}
	rows = append(rows, editRow{kind: editAddQuery},
		editRow{kind: editSection, label: "HEADERS"})
	for i := range s.form.Headers {
		rows = append(rows, editRow{kind: editHeader, index: i})
	}
	rows = append(rows, editRow{kind: editAddHeader},
		editRow{kind: editSection, label: "BODY"},
		editRow{kind: editBody})

	s.rows = rows
	if s.cursor >= len(rows) {
		s.cursor = len(rows) - 1
	}
	if !rows[s.cursor].focusable() {
		s.moveCursor(1)
	}
}

func (s *editState) moveCursor(delta int) {
	if len(s.rows) == 0 {
		return
	}
	i := s.cursor
	for n := 0; n < len(s.rows); n++ {
		i += delta
		if i < 0 {
			i = 0
			delta = 1
		}
		if i >= len(s.rows) {
			i = len(s.rows) - 1
			delta = -1
		}
		if s.rows[i].focusable() {
			s.cursor = i
			return
		}
	}
}

// value renders the current text of a row, which is also what inline editing
// starts from.
func (s *editState) value(r editRow) string {
	switch r.kind {
	case editMethod:
		return s.form.Method
	case editURL:
		return s.form.URL
	case editQuery:
		p := s.form.Query[r.index]
		if p.Value == "" {
			return p.Key
		}
		return p.Key + "=" + p.Value
	case editHeader:
		h := s.form.Headers[r.index]
		return h.Name + ": " + h.Value
	case editBody:
		return s.form.Body
	}
	return ""
}

// commit writes an edited line back into the form.
func (s *editState) commit(r editRow, text string) {
	switch r.kind {
	case editMethod:
		s.form.Method = strings.ToUpper(strings.TrimSpace(text))
	case editURL:
		s.form.URL = strings.TrimSpace(text)
	case editQuery:
		k, v, _ := strings.Cut(text, "=")
		s.form.Query[r.index] = curledit.Param{Key: strings.TrimSpace(k), Value: v}
	case editHeader:
		n, v, _ := strings.Cut(text, ":")
		s.form.Headers[r.index] = curlargs.Header{
			Name: strings.TrimSpace(n), Value: strings.TrimSpace(v),
		}
	case editBody:
		s.form.Body = text
	}
}

// remove deletes the focused query parameter or header.
func (s *editState) remove() {
	r := s.rows[s.cursor]
	switch r.kind {
	case editQuery:
		s.form.Query = append(s.form.Query[:r.index], s.form.Query[r.index+1:]...)
	case editHeader:
		s.form.Headers = append(s.form.Headers[:r.index], s.form.Headers[r.index+1:]...)
	default:
		return
	}
	s.rebuild()
}

// add appends an empty row and puts the cursor on it, ready to type.
func (s *editState) add(kind editKind) {
	switch kind {
	case editAddQuery:
		s.form.Query = append(s.form.Query, curledit.Param{})
		s.rebuild()
		for i, r := range s.rows {
			if r.kind == editQuery && r.index == len(s.form.Query)-1 {
				s.cursor = i
			}
		}
	case editAddHeader:
		s.form.Headers = append(s.form.Headers, curlargs.Header{})
		s.rebuild()
		for i, r := range s.rows {
			if r.kind == editHeader && r.index == len(s.form.Headers)-1 {
				s.cursor = i
			}
		}
	}
}

// cycleMethod steps through the common methods without typing.
func (s *editState) cycleMethod(delta int) {
	at := -1
	for i, m := range methods {
		if m == s.form.Method {
			at = i
		}
	}
	if at < 0 {
		s.form.Method = methods[0]
		return
	}
	s.form.Method = methods[(at+delta+len(methods))%len(methods)]
}

// Args produces the command the editor would run.
func (s *editState) Args() []string {
	return curledit.Apply(append([]string(nil), s.args...), s.have, s.form)
}

// Changed reports whether anything would differ from the original request.
func (s *editState) Changed() bool {
	return strings.Join(s.Args(), "\x00") != strings.Join(s.args, "\x00")
}

// --- rendering -------------------------------------------------------------

func (m *Model) renderEdit(width, height int) string {
	var b strings.Builder

	parent := m.entryByID(m.editID)
	switch {
	case parent != nil:
		b.WriteString(styHeading.Render("EDIT & RUN"))
		b.WriteString(styFaint.Render("  ·  based on "))
		b.WriteString(methodStyle(parent.Request.Method).Render(parent.Request.Method))
		b.WriteString(" ")
		b.WriteString(styMuted.Render(truncateMiddle(m.displayURL(parent), maxInt(20, width-46))))
		b.WriteString("\n")
		b.WriteString(styFaint.Render("The original stays untouched; running this records a new entry."))
	case m.edit.api != "":
		b.WriteString(styHeading.Render("NEW REQUEST"))
		b.WriteString(styFaint.Render("  ·  will be filed under "))
		b.WriteString(styMuted.Render(m.edit.api))
		b.WriteString("\n")
		b.WriteString(styFaint.Render("Running this records it as a new entry."))
	default:
		b.WriteString(styHeading.Render("NEW REQUEST"))
		b.WriteString("\n")
		b.WriteString(styFaint.Render("Running this records it as a new entry, filed under whatever the URL says."))
	}
	b.WriteString("\n\n")

	if m.edit.raw {
		b.WriteString(m.renderEditRaw(width, height-lipgloss.Height(b.String())))
		return b.String()
	}
	b.WriteString(m.renderEditForm(width, height-lipgloss.Height(b.String())))
	return b.String()
}

// renderEditRaw is the escape hatch: the command as text, for the cases a form
// cannot express.
func (m *Model) renderEditRaw(width, height int) string {
	m.editor.SetWidth(maxInt(20, width-2))
	m.editor.SetHeight(maxInt(3, height-2))
	return styFaint.Render("editing the command directly · ctrl+t returns to fields") + "\n" +
		m.editor.View()
}

func (m *Model) renderEditForm(width, height int) string {
	s := &m.edit
	var b strings.Builder

	const labelWidth = 10
	valueWidth := maxInt(20, width-labelWidth-6)

	for i, r := range s.rows {
		if r.kind == editSection {
			b.WriteString("\n" + styHeading.Render(r.label) + "\n")
			continue
		}

		focused := i == s.cursor
		cursor := "  "
		if focused {
			cursor = styCursor.Render("▌ ")
		}

		label, value := "", ""
		switch r.kind {
		case editMethod:
			label = "method"
			value = methodStyle(s.form.Method).Render(s.form.Method)
			if focused && !s.editing {
				value += styFaint.Render("   ← → to change")
			}
		case editURL:
			label = "url"
			value = styText.Render(truncate(s.form.URL, valueWidth))
		case editQuery:
			p := s.form.Query[r.index]
			label = ""
			value = styJSONKey.Render(pad(p.Key, 18)) + " " + styText.Render(truncate(p.Value, valueWidth-20))
		case editHeader:
			h := s.form.Headers[r.index]
			value = styJSONKey.Render(pad(h.Name, 18)) + " " + styText.Render(truncate(m.maskHeader(h), valueWidth-20))
		case editAddQuery, editAddHeader:
			value = styFaint.Render("+ add")
		case editBody:
			if s.form.Body == "" {
				value = styFaint.Render("—")
			} else {
				value = styText.Render(truncate(firstLine(s.form.Body), valueWidth))
				if strings.Contains(s.form.Body, "\n") {
					value += styFaint.Render(fmt.Sprintf("  (%d lines)", strings.Count(s.form.Body, "\n")+1))
				}
			}
		}

		if focused && s.editing {
			b.WriteString(cursor + styKey.Render(pad(label, labelWidth)) + m.edit.input.View() + "\n")
			continue
		}
		b.WriteString(clampLine(cursor+styKey.Render(pad(label, labelWidth))+value, width) + "\n")
	}

	// Variables the command references, and whether the active environment can
	// satisfy them. A request that will fail should say so before it runs.
	if refs := environment.References(s.Args()); len(refs) > 0 {
		b.WriteString("\n" + styHeading.Render("VARIABLES") + "\n")
		for _, name := range refs {
			value, ok := m.editVars()[name]
			mark, detail := styErr.Render("✗"), styErr.Render("not set in "+m.envName())
			if ok {
				mark, detail = styOK.Render("✓"), styFaint.Render(previewValue(value))
			}
			b.WriteString("  " + mark + " " + styJSONKey.Render(pad("{{"+name+"}}", 20)) + " " + detail + "\n")
		}
	}

	if s.Changed() {
		b.WriteString("\n" + styOK.Render("modified") + styFaint.Render(" · ctrl+r runs it as a new entry"))
	}
	return b.String()
}

// maskHeader hides a secret while it is merely being displayed, but never while
// it is being edited: you cannot edit a value you cannot see.
func (m *Model) maskHeader(h curlargs.Header) string {
	if m.reveal || !m.cfg.Redact.SensitiveHeader(h.Name) {
		return h.Value
	}
	return history.MaskValue(h.Name, h.Value)
}

// previewValue shows enough of a variable to recognize it, never all of it.
func previewValue(v string) string {
	if len(v) <= 8 {
		return "set"
	}
	return "set (" + fmt.Sprintf("%d chars", len(v)) + ")"
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// envName returns the active environment's name for display.
func (m *Model) envName() string {
	if m.envSet.Active == "" {
		return "no environment"
	}
	return m.envSet.Active
}

// editVars is the variable set the request being edited would resolve
// against: the active environment, read for the API this request belongs to.
func (m *Model) editVars() environment.Vars {
	if m.envSet.Active == "" {
		return nil
	}
	return m.envSet.Vars(m.domainOf(m.entryByID(m.editID)), m.envSet.Active)
}
