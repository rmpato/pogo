package tui

import (
	"github.com/rmpato/pogo/internal/apis"
	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/history"
)

// The list is organized by API, and an API is a thing pogo works out rather
// than something the user files by hand. Classification happens live, off the
// URL, so that correcting a grouping applies to everything already recorded —
// pin a host to "staging" and the six weeks of history that used it move too.
//
// Live classification cannot work for a request whose host was a {{variable}},
// because the stored command still says {{base}}. Those fall back to the API
// they ran under, which capture wrote down at the time for exactly this reason.

// apiRef classifies one entry, memoised by URL: the same handful of URLs
// repeat down the whole list, and a public-suffix lookup per row per frame is
// work nobody asked for.
func (m *Model) apiRef(e *history.Entry) apis.Ref {
	if e == nil {
		return apis.Ref{}
	}
	if ref, ok := m.refCache[e.Request.URL]; ok {
		return m.withStored(ref, e)
	}
	ref := apis.Classify(e.Request.URL, m.cfg.APIs)
	if m.refCache == nil {
		m.refCache = map[string]apis.Ref{}
	}
	m.refCache[e.Request.URL] = ref
	return m.withStored(ref, e)
}

// withStored fills in what a templated URL could not say, and restores what a
// request ran under when live classification disagrees with it — the case a
// project label produces against a host Classify can otherwise resolve on its
// own (a public one, say). A URL with nothing live to go on always trusts the
// stored value; a resolvable one only does while the project it names is
// still declared, so `pogo project rm` really does return its old entries to
// being classified from the host, the way they would have been without it.
func (m *Model) withStored(ref apis.Ref, e *history.Entry) apis.Ref {
	trust := !ref.Bound && e.API != "" && e.API != ref.Domain
	if trust && ref.Domain != "" {
		_, trust = m.cfg.APIs.Project(e.API)
	}
	if trust {
		ref.Domain = e.API
		ref.Name = m.cfg.APIs.Name(e.API)
		if ref.Env == "" {
			ref.Env = e.Env
		}
	}
	return ref
}

// forgetAPIs drops the classification cache. Anything that changes the
// registry has to call this, or a correction would not show until restart.
func (m *Model) forgetAPIs() { m.refCache = nil }

// domainOf is the API key an entry belongs to.
func (m *Model) domainOf(e *history.Entry) string { return m.apiRef(e).Domain }

// apiLabel is what an API is called on screen: its name if it has been given
// one, its domain otherwise.
func (m *Model) apiLabel(e *history.Entry) string {
	ref := m.apiRef(e)
	if ref.Name != "" {
		return ref.Name
	}
	return ref.Domain
}

// envOf is the environment an entry reached.
func (m *Model) envOf(e *history.Entry) string { return m.apiRef(e).Env }

// apiSummary folds the loaded history into the API list the sidebar draws.
func (m *Model) apiSummary() []apis.API {
	refs := make([]apis.Ref, 0, len(m.entries))
	for _, e := range m.entries {
		refs = append(refs, m.apiRef(e))
	}
	return apis.Summarize(refs, m.cfg.APIs)
}

// groupLabel is the header a row is filed under for the current grouping.
func (m *Model) groupLabel(e *history.Entry) string {
	switch m.group {
	case groupAPI:
		label := m.apiLabel(e)
		if label == "" {
			return "unknown"
		}
		if env := m.envOf(e); env != "" {
			return label + " · " + env
		}
		return label
	case groupHost:
		if h := m.displayHost(e); h != "" {
			return h
		}
		return "unknown"
	case groupCollection:
		if e.Collection != "" {
			return e.Collection
		}
		return "unfiled"
	}
	return ""
}

// setAPIOverride records a correction and makes it visible immediately.
// Writing on the same keypress is the whis rule (§11): there is no "unsaved
// grouping" state, because there is no such state to get wrong.
func (m *Model) setAPIOverride(mutate func(*config.Config)) error {
	if err := m.cfgStore.Update(mutate); err != nil {
		return err
	}
	m.cfg = m.cfgStore.Current().WithEnv()
	m.forgetAPIs()
	m.rebuildRows()
	m.buildRail()
	return nil
}
