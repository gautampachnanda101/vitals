package doctor

import (
	"errors"
	"testing"

	gnet "github.com/shirou/gopsutil/v4/net"
)

func TestTopRemotePeersWithRanksByConnectionCount(t *testing.T) {
	conns := []gnet.ConnectionStat{
		{Status: "ESTABLISHED", Raddr: gnet.Addr{IP: "1.1.1.1"}},
		{Status: "ESTABLISHED", Raddr: gnet.Addr{IP: "2.2.2.2"}},
		{Status: "ESTABLISHED", Raddr: gnet.Addr{IP: "2.2.2.2"}},
		{Status: "ESTABLISHED", Raddr: gnet.Addr{IP: "2.2.2.2"}},
		{Status: "LISTEN", Raddr: gnet.Addr{IP: "3.3.3.3"}}, // not established — excluded
		{Status: "ESTABLISHED", Raddr: gnet.Addr{IP: ""}},   // no remote address — excluded
	}
	got := topRemotePeersWith(func() ([]gnet.ConnectionStat, error) { return conns, nil }, 5)
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct peers, got %+v", got)
	}
	if got[0].Host != "2.2.2.2" || got[0].Count != 3 {
		t.Errorf("top peer = %+v, want 2.2.2.2 with count 3", got[0])
	}
}

func TestTopRemotePeersWithCapsAtN(t *testing.T) {
	conns := []gnet.ConnectionStat{
		{Status: "ESTABLISHED", Raddr: gnet.Addr{IP: "1.1.1.1"}},
		{Status: "ESTABLISHED", Raddr: gnet.Addr{IP: "2.2.2.2"}},
		{Status: "ESTABLISHED", Raddr: gnet.Addr{IP: "3.3.3.3"}},
	}
	got := topRemotePeersWith(func() ([]gnet.ConnectionStat, error) { return conns, nil }, 2)
	if len(got) != 2 {
		t.Errorf("topRemotePeersWith(n=2) returned %d peers, want 2", len(got))
	}
}

func TestTopRemotePeersWithReturnsNilOnError(t *testing.T) {
	got := topRemotePeersWith(func() ([]gnet.ConnectionStat, error) { return nil, errors.New("boom") }, 5)
	if got != nil {
		t.Errorf("topRemotePeersWith on error = %v, want nil", got)
	}
}

func TestTopRemotePeersGoesThroughTheRealConnectionTable(t *testing.T) {
	// One real end-to-end call through the actual gnet.Connections("tcp")
	// wiring — no assertion on the result (machine-dependent), just that
	// the real call site doesn't panic.
	_ = topRemotePeers(5)
}
