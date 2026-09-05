package doctor

import (
	"strings"
	"testing"
)

func TestFocusDetailDiskShowsSMART(t *testing.T) {
	s := Snapshot{Disks: []Disk{
		{Mount: "/", UsedPct: 40, FreeBytes: 100 << 30,
			SMART: &DiskSMART{Passed: true, TempC: 39, WearPct: 11}},
	}}
	out := captureStdout(t, func() { focusDetail("disk", s, false) })
	for _, want := range []string{"S.M.A.R.T. PASSED", "39", "11% life used"} {
		if !strings.Contains(out, want) {
			t.Errorf("disk focus SMART line missing %q, got:\n%s", want, out)
		}
	}

	bad := captureStdout(t, func() {
		focusDetail("disk", Snapshot{Disks: []Disk{{Mount: "/", SMART: &DiskSMART{Passed: false}}}}, false)
	})
	if !strings.Contains(bad, "S.M.A.R.T. FAILED") {
		t.Errorf("failing SMART should print FAILED, got:\n%s", bad)
	}

	none := captureStdout(t, func() {
		focusDetail("disk", Snapshot{Disks: []Disk{{Mount: "/", UsedPct: 40}}}, false)
	})
	if strings.Contains(none, "S.M.A.R.T.") {
		t.Errorf("no SMART data -> no line, got:\n%s", none)
	}
}
