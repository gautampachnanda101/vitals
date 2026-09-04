package doctor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// --- network -----------------------------------------------------------------

type netReading struct {
	name   string
	rx, tx uint64
}

func netCounters(src source) []netReading {
	cs, err := src.netIOCounters(true)
	if err != nil {
		return nil
	}
	out := make([]netReading, 0, len(cs))
	for _, c := range cs {
		if c.BytesRecv == 0 && c.BytesSent == 0 {
			continue
		}
		if strings.HasPrefix(c.Name, "lo") {
			continue
		}
		out = append(out, netReading{name: c.Name, rx: c.BytesRecv, tx: c.BytesSent})
	}
	return out
}

// netDelta turns two cumulative readings into per-interface throughput.
func netDelta(prev, curr []netReading, dt time.Duration) []NetIface {
	idx := make(map[string]netReading, len(prev))
	for _, p := range prev {
		idx[p.name] = p
	}
	secs := dt.Seconds()
	var out []NetIface
	for _, c := range curr {
		n := NetIface{Name: c.name}
		if p, ok := idx[c.name]; ok && secs > 0 {
			if c.rx >= p.rx {
				n.RxBytesPerSec = float64(c.rx-p.rx) / secs
			}
			if c.tx >= p.tx {
				n.TxBytesPerSec = float64(c.tx-p.tx) / secs
			}
		}
		out = append(out, n)
	}
	return out
}

// --- power / battery -------------------------------------------------------

func collectPower() Power {
	switch runtime.GOOS {
	case "darwin":
		p := Power{}
		if out, ok := runCmd("pmset", "-g", "batt"); ok {
			p = parsePmsetBatt(out)
		}
		if out, ok := runCmd("pmset", "-g"); ok {
			if on, found := parseLowPowerMode(out); found {
				p.LowPowerMode = on
			}
		}
		return p
	case "linux":
		return readLinuxBattery("/sys/class/power_supply")
	}
	return Power{}
}

func runCmd(name string, args ...string) (string, bool) {
	if _, err := exec.LookPath(name); err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", false
	}
	return string(b), true
}

// lowPowerModeRE matches pmset -g's "lowpowermode  0|1" line. Older macOS
// versions and Intel Macs never print this line at all, which is why the
// caller gets an explicit found bool rather than a guessed default.
var lowPowerModeRE = regexp.MustCompile(`(?m)^\s*lowpowermode\s+(\d)\s*$`)

// parseLowPowerMode reads whether macOS Low Power Mode is on from `pmset -g`
// output. found is false when the line is simply absent (nothing to report,
// not a parse failure) rather than assuming a value.
func parseLowPowerMode(s string) (on bool, found bool) {
	m := lowPowerModeRE.FindStringSubmatch(s)
	if m == nil {
		return false, false
	}
	return m[1] == "1", true
}

var (
	pmsetPct  = regexp.MustCompile(`(\d+)%`)
	pmsetTime = regexp.MustCompile(`(\d+):(\d\d)\s+remaining`)
)

// parsePmsetBatt reads `pmset -g batt` output, e.g.:
//
//	Now drawing from 'Battery Power'
//	 -InternalBattery-0 (id=...)	54%; discharging; 3:12 remaining present: true
func parsePmsetBatt(s string) Power {
	var p Power
	p.OnBattery = strings.Contains(s, "'Battery Power'")
	if m := pmsetPct.FindStringSubmatch(s); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			p.Percent = v
		}
	}
	if m := pmsetTime.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		p.MinutesLeft = h*60 + mm
	}
	if strings.Contains(s, "discharging") {
		p.ChargeRateW = -1 // sign only; pmset gives no watts. On AC this means
		// the charger can't keep up; analyzePower keys off !OnBattery.
	}
	return p
}

// readLinuxBattery reads the first BAT* under /sys/class/power_supply.
func readLinuxBattery(base string) Power {
	entries, err := os.ReadDir(base)
	if err != nil {
		return Power{}
	}
	var bat string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "BAT") {
			bat = filepath.Join(base, e.Name())
			break
		}
	}
	if bat == "" {
		return Power{}
	}
	read := func(f string) string {
		b, _ := os.ReadFile(filepath.Join(bat, f))
		return strings.TrimSpace(string(b))
	}
	return parseLinuxBattery(map[string]string{
		"capacity":           read("capacity"),
		"status":             read("status"),
		"energy_now":         read("energy_now"),
		"energy_full":        read("energy_full"),
		"energy_full_design": read("energy_full_design"),
		"power_now":          read("power_now"),
	})
}

func parseLinuxBattery(m map[string]string) Power {
	var p Power
	if v, err := strconv.ParseFloat(m["capacity"], 64); err == nil {
		p.Percent = v
	}
	status := strings.ToLower(m["status"])
	p.OnBattery = status == "discharging"
	if full, _ := strconv.ParseFloat(m["energy_full"], 64); full > 0 {
		if design, _ := strconv.ParseFloat(m["energy_full_design"], 64); design > 0 {
			p.DesignCapacityF = full / design
		}
	}
	if pw, err := strconv.ParseFloat(m["power_now"], 64); err == nil && pw > 0 {
		w := pw / 1e6 // µW -> W
		if p.OnBattery {
			p.ChargeRateW = -w
		} else if status == "charging" {
			p.ChargeRateW = w
		}
	}
	if p.OnBattery && p.ChargeRateW < 0 {
		if now, _ := strconv.ParseFloat(m["energy_now"], 64); now > 0 {
			hours := now / (-p.ChargeRateW * 1e6)
			p.MinutesLeft = int(hours * 60)
		}
	}
	return p
}
