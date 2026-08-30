package tui

import (
	"strconv"
	"strings"

	"github.com/rmpato/pogo/internal/apis"
	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/history"
)

// Query is a parsed search string.
//
// Search is plain substring matching by default, because that is what people
// reach for: typing "users/42" should just work. Structured filters are
// available for when you know exactly what you want, and are recognized only
// when a token has the "field:value" shape, so a search for "http://a:8080"
// stays a search.
type Query struct {
	Terms       []string // free text, all must match
	Methods     []string
	Hosts       []string
	APIs        []string
	Envs        []string
	Collections []string
	Status      []statusFilter
	Starred     bool
	Failed      bool
	Raw         string

	// reg is the user's corrections to how hosts group into APIs. api: and
	// env: are the only filters that are not a plain property of an entry —
	// they are a conclusion about it — so the query has to carry what that
	// conclusion is drawn from.
	reg config.APIRegistry
}

// WithRegistry returns the query with API overrides applied, so that a host
// pinned to "staging" matches env:staging.
func (q Query) WithRegistry(reg config.APIRegistry) Query {
	q.reg = reg
	return q
}

// statusFilter matches either an exact code (404) or a class (4xx).
type statusFilter struct {
	code  int
	class int // 0 when exact, otherwise 2 for 2xx, 4 for 4xx, ...
}

func (f statusFilter) match(code int) bool {
	if f.class > 0 {
		return code/100 == f.class
	}
	return code == f.code
}

// ParseQuery turns a search string into a Query.
func ParseQuery(s string) Query {
	q := Query{Raw: s}

	for _, tok := range strings.Fields(s) {
		field, value, ok := strings.Cut(tok, ":")
		if !ok || value == "" {
			q.Terms = append(q.Terms, strings.ToLower(tok))
			continue
		}

		switch strings.ToLower(field) {
		case "method", "m":
			q.Methods = append(q.Methods, strings.ToUpper(value))
		case "host", "h":
			q.Hosts = append(q.Hosts, strings.ToLower(value))
		case "api", "a":
			q.APIs = append(q.APIs, strings.ToLower(value))
		case "env", "e":
			q.Envs = append(q.Envs, strings.ToLower(value))
		case "collection", "col":
			q.Collections = append(q.Collections, strings.ToLower(value))
		case "status", "s", "code":
			if f, ok := parseStatusFilter(value); ok {
				q.Status = append(q.Status, f)
			}
		case "is":
			switch strings.ToLower(value) {
			case "starred", "star", "fav", "favorite":
				q.Starred = true
			case "failed", "error", "err":
				q.Failed = true
			}
		default:
			// Not a filter pogo knows: treat the whole token as free text so a
			// URL with a scheme or a port still searches the way it looks.
			q.Terms = append(q.Terms, strings.ToLower(tok))
		}
	}
	return q
}

func parseStatusFilter(v string) (statusFilter, bool) {
	v = strings.ToLower(v)
	if strings.HasSuffix(v, "xx") && len(v) == 3 {
		if class, err := strconv.Atoi(v[:1]); err == nil {
			return statusFilter{class: class}, true
		}
		return statusFilter{}, false
	}
	if code, err := strconv.Atoi(v); err == nil {
		return statusFilter{code: code}, true
	}
	return statusFilter{}, false
}

// Empty reports whether the query would match everything.
func (q Query) Empty() bool {
	return len(q.Terms) == 0 && len(q.Methods) == 0 && len(q.Hosts) == 0 &&
		len(q.APIs) == 0 && len(q.Envs) == 0 &&
		len(q.Collections) == 0 && len(q.Status) == 0 && !q.Starred && !q.Failed
}

// Match reports whether an entry satisfies the query. All conditions are ANDed;
// repeated values of the same field are ORed, so "method:GET method:POST" reads
// the way it looks.
func (q Query) Match(e *history.Entry) bool {
	if q.Starred && !e.Favorite {
		return false
	}
	if q.Failed && e.OK() {
		return false
	}
	if len(q.Methods) > 0 && !containsFold(q.Methods, e.Request.Method) {
		return false
	}
	if len(q.Hosts) > 0 {
		host := strings.ToLower(e.Host())
		matched := false
		for _, h := range q.Hosts {
			if strings.Contains(host, h) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(q.APIs) > 0 || len(q.Envs) > 0 {
		ref := q.classify(e)
		if len(q.APIs) > 0 && !anyContains(q.APIs, strings.ToLower(ref.Domain)) &&
			!anyContains(q.APIs, strings.ToLower(ref.Name)) {
			return false
		}
		if len(q.Envs) > 0 && !containsFold(q.Envs, ref.Env) {
			return false
		}
	}
	if len(q.Collections) > 0 {
		col := strings.ToLower(e.Collection)
		matched := false
		for _, c := range q.Collections {
			if col != "" && strings.Contains(col, c) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if len(q.Status) > 0 {
		code := e.Status()
		matched := false
		for _, f := range q.Status {
			if f.match(code) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if len(q.Terms) > 0 {
		hay := searchText(e)
		for _, term := range q.Terms {
			if !strings.Contains(hay, term) {
				return false
			}
		}
	}
	return true
}

// searchText is the haystack a free-text term is matched against. It covers
// what someone would remember about a request -- the method, where it went,
// what came back -- but not header values, which would make a search for
// "token" match every authenticated request.
func searchText(e *history.Entry) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(e.Request.Method))
	b.WriteByte(' ')
	b.WriteString(strings.ToLower(e.Request.URL))
	b.WriteByte(' ')
	if s := e.Status(); s > 0 {
		b.WriteString(strconv.Itoa(s))
		b.WriteByte(' ')
	}
	if e.Note != "" {
		b.WriteString(strings.ToLower(e.Note))
		b.WriteByte(' ')
	}
	if e.Collection != "" {
		b.WriteString(strings.ToLower(e.Collection))
		b.WriteByte(' ')
	}
	if e.Error != "" {
		b.WriteString(strings.ToLower(e.Error))
	}
	return b.String()
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// classify resolves an entry to its API, falling back to what it ran as when
// the URL is templated and cannot be read now, or — while the project is
// still declared — when it was filed under a project label live
// classification cannot see on its own. See tui.withStored for why the
// project must still exist.
func (q Query) classify(e *history.Entry) apis.Ref {
	ref := apis.Classify(e.Request.URL, q.reg)
	trust := !ref.Bound && e.API != "" && e.API != ref.Domain
	if trust && ref.Domain != "" {
		_, trust = q.reg.Project(e.API)
	}
	if trust {
		ref.Domain = e.API
		ref.Name = q.reg.Name(e.API)
	}
	if ref.Env == "" {
		ref.Env = e.Env
	}
	return ref
}

// anyContains reports whether target contains any of the needles. Filters
// match on substrings so that "api:acme" finds acme.com without anyone having
// to type the suffix.
func anyContains(needles []string, target string) bool {
	if target == "" {
		return false
	}
	for _, n := range needles {
		if strings.Contains(target, n) {
			return true
		}
	}
	return false
}
