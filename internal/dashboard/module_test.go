package dashboard

import (
	"testing"

	"vitals/internal/doctor"
)

// withRegistry replaces the package-level registry for the duration of a
// test and restores it after — real modules self-register via init(), so
// tests need isolation from both those and each other.
func withRegistry(t *testing.T, modules []Module) {
	t.Helper()
	old := registry
	registry = append([]Module(nil), modules...)
	t.Cleanup(func() { registry = old })
}

func TestRegisterPanicsOnDuplicateSlug(t *testing.T) {
	// A second module silently shadowing the first on a slug collision
	// would be permanently unreachable dead code with no error anywhere —
	// http.ServeMux.Handle panics on a duplicate pattern for the same
	// reason; match that here.
	withRegistry(t, nil)
	Register(Module{Slug: "cpu"})

	defer func() {
		if recover() == nil {
			t.Error("Register should panic on a duplicate slug, it did not")
		}
	}()
	Register(Module{Slug: "cpu"})
}

func TestSortedModulesOrdersByOrderThenRegistration(t *testing.T) {
	withRegistry(t, []Module{
		{Slug: "c", Order: 5},
		{Slug: "a", Order: 1},
		{Slug: "b", Order: 1},
	})
	got := sortedModules()
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if got[i].Slug != w {
			t.Errorf("position %d = %q, want %q (got order %v)", i, got[i].Slug, w, sluglist(got))
		}
	}
}

func sluglist(ms []Module) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Slug
	}
	return out
}

func TestAvailableModulesFiltersByAvailable(t *testing.T) {
	withRegistry(t, []Module{
		{Slug: "always", Order: 1, Available: Always},
		{Slug: "never", Order: 2, Available: func(PageContext) bool { return false }},
	})
	got := availableModules(PageContext{})
	if len(got) != 1 || got[0].Slug != "always" {
		t.Errorf("availableModules = %v, want only \"always\"", sluglist(got))
	}
}

func TestFindModuleDistinguishesMissingFromUnavailable(t *testing.T) {
	withRegistry(t, []Module{
		{Slug: "advice", Available: func(PageContext) bool { return false }},
	})

	if _, exists, _ := findModule("nope", PageContext{}); exists {
		t.Error("findModule should report a slug with no registered module as not existing")
	}
	m, exists, available := findModule("advice", PageContext{})
	if !exists {
		t.Fatal("advice module should exist")
	}
	if available {
		t.Error("advice module's Available returns false — findModule should report that, not true")
	}
	if m.Slug != "advice" {
		t.Errorf("returned module slug = %q, want \"advice\"", m.Slug)
	}
}

func TestHasGPU(t *testing.T) {
	if HasGPU(PageContext{}) {
		t.Error("no GPUs should report false")
	}
	if !HasGPU(PageContext{Snapshot: doctor.Snapshot{GPUs: []doctor.GPU{{Name: "test"}}}}) {
		t.Error("one GPU should report true")
	}
}

func TestHasBattery(t *testing.T) {
	if HasBattery(PageContext{}) {
		t.Error("zero Power value should report false")
	}
	if !HasBattery(PageContext{Snapshot: doctor.Snapshot{Power: doctor.Power{Percent: 80}}}) {
		t.Error("a nonzero charge percent should report true")
	}
	if !HasBattery(PageContext{Snapshot: doctor.Snapshot{Power: doctor.Power{OnBattery: true}}}) {
		t.Error("OnBattery alone should report true even at 0%% (unusual but real)")
	}
}
