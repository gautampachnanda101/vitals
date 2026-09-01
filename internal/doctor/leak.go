package doctor

// leakDropTolerance allows small measurement noise (e.g. a GC pause between
// samples) without breaking a growth streak — only a drop bigger than this
// fraction of the running peak counts as "came back down," which disqualifies
// the candidate as a genuine climber rather than noise around a plateau.
const leakDropTolerance = 0.05

// DetectMemoryGrowth looks across recorded history points for a process that
// was vitals' top RSS consumer repeatedly, with memory use that only ever
// went up (allowing small noise) and grew by at least minGrowthBytes over at
// least minSamples appearances. It reports the single largest such grower, if
// any — a lightweight, history-only stand-in for real leak detection: it
// can't see a process's memory between the times it happened to be the top
// consumer, so it only ever flags the most obvious, sustained case.
func DetectMemoryGrowth(points []HistoryPoint, minSamples int, minGrowthBytes uint64) (ProcRef, uint64, bool) {
	type series struct {
		name    string
		samples []uint64
	}
	byPID := map[int32]*series{}
	var order []int32
	for _, p := range points {
		if p.TopMemPID == 0 {
			continue
		}
		s, ok := byPID[p.TopMemPID]
		if !ok {
			s = &series{name: p.TopMemName}
			byPID[p.TopMemPID] = s
			order = append(order, p.TopMemPID)
		}
		s.samples = append(s.samples, p.TopMemRSS)
	}

	var bestPID int32
	var bestGrowth uint64
	found := false

	for _, pid := range order {
		s := byPID[pid]
		if len(s.samples) < minSamples {
			continue
		}
		if !nonDecreasing(s.samples, leakDropTolerance) {
			continue
		}
		first, last := s.samples[0], s.samples[len(s.samples)-1]
		if last <= first {
			continue
		}
		growth := last - first
		if growth < minGrowthBytes {
			continue
		}
		if !found || growth > bestGrowth {
			bestPID, bestGrowth, found = pid, growth, true
		}
	}

	if !found {
		return ProcRef{}, 0, false
	}
	return ProcRef{PID: bestPID, Name: byPID[bestPID].name, RSSBytes: byPID[bestPID].samples[len(byPID[bestPID].samples)-1]}, bestGrowth, true
}

// nonDecreasing reports whether samples never drops by more than tolerance
// fraction of the peak-so-far — a real climb allowed to wobble slightly, as
// opposed to a process that happens to spike occasionally but isn't
// sustained.
func nonDecreasing(samples []uint64, tolerance float64) bool {
	peak := samples[0]
	for _, v := range samples[1:] {
		if v > peak {
			peak = v
			continue
		}
		drop := peak - v
		if float64(drop) > float64(peak)*tolerance {
			return false
		}
	}
	return true
}
