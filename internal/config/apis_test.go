package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestProjectAsALabelOnly(t *testing.T) {
	var r APIRegistry
	r.SetProject("checkout", "")

	base, ok := r.Project("checkout")
	if !ok {
		t.Fatal("a project declared with no base URL should still exist")
	}
	if base != "" {
		t.Errorf("base = %q, want empty", base)
	}
	if got := r.ProjectNames(); !reflect.DeepEqual(got, []string{"checkout"}) {
		t.Errorf("ProjectNames = %v, want [checkout]", got)
	}
}

func TestProjectNamesAreCaseFolded(t *testing.T) {
	var r APIRegistry
	r.SetProject("Checkout", "http://localhost:3000")

	if _, ok := r.Project("checkout"); !ok {
		t.Error("Project should be found regardless of the case it was declared with")
	}
	if got := r.ProjectNames(); !reflect.DeepEqual(got, []string{"checkout"}) {
		t.Errorf("ProjectNames = %v, want [checkout] (lowercased)", got)
	}
}

func TestRemoveProjectUnbindsItsHosts(t *testing.T) {
	var r APIRegistry
	r.SetProject("checkout", "http://localhost:3000")
	r.SetDomain("localhost:3000", "checkout")
	r.SetDomain("localhost:8080", "scratch") // a different project, must survive

	r.RemoveProject("checkout")

	if _, ok := r.Project("checkout"); ok {
		t.Error("checkout should no longer be a project")
	}
	if _, ok := r.DomainFor("localhost:3000"); ok {
		t.Error("removing a project should unbind its hosts")
	}
	if domain, ok := r.DomainFor("localhost:8080"); !ok || domain != "scratch" {
		t.Errorf("an unrelated project's binding should be untouched, got %q, %v", domain, ok)
	}
}

// A project's entry in config.yaml must survive a save/reload cycle even with
// an empty base URL — that empty string is the fact "this project exists but
// has not seen a request yet," not a value worth pruning.
func TestProjectsRoundTripThroughTheStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	st, err := OpenAt(path, Default())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	err = st.Update(func(c *Config) {
		c.APIs.SetProject("checkout", "http://localhost:3000")
		c.APIs.SetProject("scratch", "")
		c.APIs.SetDomain("localhost:3000", "checkout")
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	reloaded, err := OpenAt(path, Default())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	apis := reloaded.Current().APIs

	if base, ok := apis.Project("checkout"); !ok || base != "http://localhost:3000" {
		t.Errorf("checkout = (%q, %v), want (http://localhost:3000, true)", base, ok)
	}
	if base, ok := apis.Project("scratch"); !ok || base != "" {
		t.Errorf("scratch = (%q, %v), want (\"\", true)", base, ok)
	}
	if domain, ok := apis.DomainFor("localhost:3000"); !ok || domain != "checkout" {
		t.Errorf("localhost:3000 = (%q, %v), want (checkout, true)", domain, ok)
	}
}
