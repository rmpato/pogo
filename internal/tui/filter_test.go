package tui

import (
	"testing"
	"time"

	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/curlargs"
	"github.com/rmpato/pogo/internal/history"
)

func entryFor(method, url string, status, exit int, fav bool) *history.Entry {
	e := &history.Entry{
		ID:        history.NewID(),
		CreatedAt: time.Now(),
		Request:   history.Request{Method: method, URL: url},
		Exit:      exit,
		Favorite:  fav,
	}
	if status > 0 {
		e.Response = &history.Response{Blocks: []history.Block{{Status: status}}}
	}
	return e
}

func TestQueryFreeTextMatching(t *testing.T) {
	e := entryFor("GET", "https://api.foo.com/users/42", 200, 0, false)

	for _, q := range []string{"users", "users/42", "api.foo.com", "get", "200", "FOO"} {
		if !ParseQuery(q).Match(e) {
			t.Errorf("query %q should match %s", q, e.Request.URL)
		}
	}
	for _, q := range []string{"posts", "404", "bar.com"} {
		if ParseQuery(q).Match(e) {
			t.Errorf("query %q should not match %s", q, e.Request.URL)
		}
	}
}

// Multiple bare terms narrow the result; they are ANDed, like every other
// search box a developer uses.
func TestQueryTermsAreAnded(t *testing.T) {
	e := entryFor("POST", "https://api.foo.com/login", 201, 0, false)

	if !ParseQuery("api login").Match(e) {
		t.Error("both terms match, so the entry should match")
	}
	if ParseQuery("api logout").Match(e) {
		t.Error("one term does not match, so the entry should not match")
	}
}

func TestQueryStructuredFilters(t *testing.T) {
	get200 := entryFor("GET", "https://api.foo.com/users", 200, 0, false)
	post404 := entryFor("POST", "https://api.bar.com/users", 404, 0, false)
	failed := entryFor("GET", "https://api.baz.com/x", 0, 7, false)
	starred := entryFor("GET", "https://api.foo.com/me", 200, 0, true)

	tests := []struct {
		query string
		entry *history.Entry
		want  bool
	}{
		{"method:POST", post404, true},
		{"method:POST", get200, false},
		{"method:post", post404, true},
		{"status:404", post404, true},
		{"status:404", get200, false},
		{"status:4xx", post404, true},
		{"status:2xx", get200, true},
		{"status:5xx", post404, false},
		{"host:api.foo.com", get200, true},
		{"host:api.foo.com", post404, false},
		{"host:foo", get200, true},
		{"is:starred", starred, true},
		{"is:starred", get200, false},
		{"is:failed", failed, true},
		{"is:failed", get200, false},
		{"method:GET status:2xx host:foo", get200, true},
		{"method:GET status:2xx host:bar", get200, false},
	}
	for _, tt := range tests {
		if got := ParseQuery(tt.query).Match(tt.entry); got != tt.want {
			t.Errorf("ParseQuery(%q).Match(%s %s) = %v, want %v",
				tt.query, tt.entry.Request.Method, tt.entry.Request.URL, got, tt.want)
		}
	}
}

// Repeating a field ORs its values, which is how "show me writes" reads.
func TestQueryRepeatedFieldsAreOred(t *testing.T) {
	q := ParseQuery("method:POST method:DELETE")
	if !q.Match(entryFor("POST", "https://x.com", 200, 0, false)) {
		t.Error("POST should match")
	}
	if !q.Match(entryFor("DELETE", "https://x.com", 204, 0, false)) {
		t.Error("DELETE should match")
	}
	if q.Match(entryFor("GET", "https://x.com", 200, 0, false)) {
		t.Error("GET should not match")
	}
}

// A URL typed into the search box contains colons but is not a filter.
func TestQueryDoesNotMistakeURLsForFilters(t *testing.T) {
	e := entryFor("GET", "http://localhost:8080/users", 200, 0, false)

	if !ParseQuery("localhost:8080").Match(e) {
		t.Error("an unknown field prefix should fall back to free text")
	}
	if !ParseQuery("http://localhost:8080/users").Match(e) {
		t.Error("a full URL should match as free text")
	}
}

func TestEmptyQueryMatchesEverything(t *testing.T) {
	q := ParseQuery("")
	if !q.Empty() {
		t.Error("an empty string should produce an empty query")
	}
	if !q.Match(entryFor("GET", "https://x.com", 200, 0, false)) {
		t.Error("an empty query should match everything")
	}
	if !ParseQuery("   ").Empty() {
		t.Error("whitespace should produce an empty query")
	}
}

// A project label filed a request under itself even though the URL resolves
// fine on its own (a public host). That should win over the guess — but only
// for as long as the project is still declared; once it is removed, the old
// entry should read exactly as it would have without the project ever
// existing.
func TestClassifyTrustsAProjectLabelOnlyWhileItExists(t *testing.T) {
	e := entryFor("GET", "https://example.com/x", 200, 0, false)
	e.API = "checkout"

	var reg config.APIRegistry
	reg.SetProject("checkout", "")
	q := ParseQuery("").WithRegistry(reg)
	if ref := q.classify(e); ref.Domain != "checkout" {
		t.Errorf("domain = %q, want checkout while the project exists", ref.Domain)
	}

	reg.RemoveProject("checkout")
	q = ParseQuery("").WithRegistry(reg)
	if ref := q.classify(e); ref.Domain != "example.com" {
		t.Errorf("domain = %q, want example.com once the project is removed", ref.Domain)
	}
}

// Header values are deliberately not searched: a search for "token" would
// otherwise match every authenticated request.
func TestSearchDoesNotCoverHeaderValues(t *testing.T) {
	e := entryFor("GET", "https://api.foo.com/users", 200, 0, false)
	e.Request.Headers = append(e.Request.Headers,
		curlargs.Header{Name: "Authorization", Value: "Bearer supersecretvalue"})

	if ParseQuery("supersecretvalue").Match(e) {
		t.Error("header values must not be searchable")
	}
}
