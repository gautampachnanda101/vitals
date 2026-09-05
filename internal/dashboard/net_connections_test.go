package dashboard

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"
	"vitals/internal/monitor"
)

func withFakeConnCache(t *testing.T, fn func() ([]gnet.ConnectionStat, error)) {
	t.Helper()
	old := defaultConnCache
	defaultConnCache = &connCache{ttl: time.Hour, fetch: fn}
	t.Cleanup(func() { defaultConnCache = old })
}

// stubResourceExtras neutralises every cache a resource page consults for
// its optional sections (process table, active connections, disk scan)
// so renderCPU/renderMem/renderDisk/renderNet/renderPower tests stay
// fast and deterministic and don't touch the real machine.
func stubResourceExtras(t *testing.T) {
	t.Helper()
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	withFakeConnCache(t, func() ([]gnet.ConnectionStat, error) { return nil, nil })
	withFakeDiskUsageCache(t, func() (diskScanResult, error) { return diskScanResult{}, nil })
}

func conn(pid int32, rip string, rport uint32, status string) gnet.ConnectionStat {
	return gnet.ConnectionStat{
		Pid:    pid,
		Laddr:  gnet.Addr{IP: "10.0.0.2", Port: 5555},
		Raddr:  gnet.Addr{IP: rip, Port: rport},
		Status: status,
	}
}

func TestActiveConnectionsSectionShowsLiveRemoteSessions(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) {
		return monitor.Snapshot{Processes: []monitor.ProcInfo{{PID: 42, Name: "curl"}}}, nil
	})
	withFakeConnCache(t, func() ([]gnet.ConnectionStat, error) {
		return []gnet.ConnectionStat{
			conn(42, "93.184.216.34", 443, "ESTABLISHED"),
			conn(999, "1.1.1.1", 53, "ESTABLISHED"),
		}, nil
	})

	out := activeConnectionsSection()
	for _, want := range []string{"Active connections", "curl (pid 42)", "93.184.216.34:443", "ESTABLISHED", "pid 999"} {
		if !strings.Contains(out, want) {
			t.Errorf("activeConnectionsSection missing %q, got: %s", want, out)
		}
	}
}

func TestActiveConnectionsSectionFiltersNoiseAndLoopback(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	withFakeConnCache(t, func() ([]gnet.ConnectionStat, error) {
		return []gnet.ConnectionStat{
			conn(1, "127.0.0.1", 8080, "ESTABLISHED"),                             // loopback
			conn(2, "10.1.1.1", 443, "CLOSED"),                                    // dead
			conn(3, "10.1.1.1", 443, "TIME_WAIT"),                                 // waiting
			{Pid: 4, Laddr: gnet.Addr{IP: "0.0.0.0", Port: 22}, Status: "LISTEN"}, // listening, no remote
		}, nil
	})
	if out := activeConnectionsSection(); out != "" {
		t.Errorf("activeConnectionsSection should be empty when nothing is a live remote session, got: %s", out)
	}
}

func TestActiveConnectionsSectionCapsAndCounts(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) { return monitor.Snapshot{}, nil })
	withFakeConnCache(t, func() ([]gnet.ConnectionStat, error) {
		var cs []gnet.ConnectionStat
		for i := 0; i < connsDisplayCap+7; i++ {
			cs = append(cs, conn(int32(i+1), "203.0.113.5", uint32(1000+i), "ESTABLISHED"))
		}
		return cs, nil
	})
	out := activeConnectionsSection()
	if !strings.Contains(out, "showing 15") {
		t.Errorf("expected a truncation caption, got: %s", out)
	}
	if n := strings.Count(out, "203.0.113.5:"); n != connsDisplayCap {
		t.Errorf("expected %d rows, got %d: %s", connsDisplayCap, n, out)
	}
}

func TestActiveConnectionsSectionSilentOnError(t *testing.T) {
	withFakeConnCache(t, func() ([]gnet.ConnectionStat, error) { return nil, errors.New("no permission") })
	if out := activeConnectionsSection(); out != "" {
		t.Errorf("activeConnectionsSection should be blank on error, got: %s", out)
	}
}

func TestActiveConnectionsSectionEscapesProcessName(t *testing.T) {
	withFakeProcessCache(t, func() (monitor.Snapshot, error) {
		return monitor.Snapshot{Processes: []monitor.ProcInfo{{PID: 7, Name: "<script>x</script>"}}}, nil
	})
	withFakeConnCache(t, func() ([]gnet.ConnectionStat, error) {
		return []gnet.ConnectionStat{conn(7, "198.51.100.9", 443, "ESTABLISHED")}, nil
	})
	if strings.Contains(activeConnectionsSection(), "<script>x</script>") {
		t.Error("activeConnectionsSection did not escape a crafted process name")
	}
}

func TestConnCacheIsSingleFlightAndCached(t *testing.T) {
	var calls atomic.Int64
	var release = make(chan struct{})
	c := &connCache{ttl: time.Hour, fetch: func() ([]gnet.ConnectionStat, error) {
		calls.Add(1)
		<-release
		return []gnet.ConnectionStat{conn(1, "1.2.3.4", 80, "ESTABLISHED")}, nil
	}}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.Get() }()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Errorf("fetch called %d times, want 1 (single-flight)", got)
	}
	if _, err := c.Get(); err != nil {
		t.Fatalf("cached Get returned error: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("second Get within TTL re-fetched; calls = %d", calls.Load())
	}
}

func TestConnCacheDefaultFetchHitsRealGopsutil(t *testing.T) {
	// One real call through newConnCache's own wiring — same "exercise
	// the real thing once" style as the process cache's own test.
	if _, err := newConnCache().Get(); err != nil {
		t.Skipf("net.Connections unavailable in this environment: %v", err)
	}
}

func TestIsLoopbackAndLiveState(t *testing.T) {
	if !isLoopback("127.0.0.1") || !isLoopback("::1") {
		t.Error("isLoopback should recognise loopback addresses")
	}
	if isLoopback("8.8.8.8") || isLoopback("not-an-ip") {
		t.Error("isLoopback should reject non-loopback / invalid")
	}
	if !liveState("ESTABLISHED") || liveState("CLOSE_WAIT") || liveState("") {
		t.Error("liveState classification wrong")
	}
}
