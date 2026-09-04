package doctor

import (
	"sort"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// remotePeer is one remote host `vitals net` is talking to, ranked by how many
// established TCP connections point at it — the "who" behind raw rx/tx bytes.
type remotePeer struct {
	Host  string
	Count int
}

// topRemotePeers returns the n remote hosts with the most established TCP
// connections. Presentation-only (not part of the JSON snapshot): connection
// tables are a live OS view, not something worth freezing into a schema.
func topRemotePeers(n int) []remotePeer {
	return topRemotePeersWith(func() ([]gnet.ConnectionStat, error) { return gnet.Connections("tcp") }, n)
}

// topRemotePeersWith is topRemotePeers' testable core: connections is
// injected (gnet.Connections("tcp") in production) so a test can
// substitute a fixed connection list instead of the real OS connection
// table.
func topRemotePeersWith(connections func() ([]gnet.ConnectionStat, error), n int) []remotePeer {
	conns, err := connections()
	if err != nil {
		return nil
	}
	counts := map[string]int{}
	for _, c := range conns {
		if c.Status != "ESTABLISHED" || c.Raddr.IP == "" {
			continue
		}
		counts[c.Raddr.IP]++
	}
	out := make([]remotePeer, 0, len(counts))
	for host, count := range counts {
		out = append(out, remotePeer{Host: host, Count: count})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > n {
		out = out[:n]
	}
	return out
}
