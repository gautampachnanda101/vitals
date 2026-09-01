package doctor

import (
	"testing"
	"time"
)

func TestNetDelta(t *testing.T) {
	prev := []netReading{{name: "en0", rx: 1000, tx: 500}}
	curr := []netReading{{name: "en0", rx: 5000, tx: 1500}}
	got := netDelta(prev, curr, 2*time.Second)
	if len(got) != 1 {
		t.Fatalf("want 1 iface, got %d", len(got))
	}
	if got[0].RxBytesPerSec != 2000 || got[0].TxBytesPerSec != 500 {
		t.Errorf("rates = %+v", got[0])
	}

	// counter reset -> no negative
	reset := netDelta(
		[]netReading{{name: "en0", rx: 9999, tx: 9999}},
		[]netReading{{name: "en0", rx: 1, tx: 1}}, time.Second)
	if reset[0].RxBytesPerSec < 0 || reset[0].TxBytesPerSec < 0 {
		t.Errorf("negative rate after reset: %+v", reset[0])
	}
}

func TestParsePmsetBatt(t *testing.T) {
	onBattery := `Now drawing from 'Battery Power'
 -InternalBattery-0 (id=12345)	54%; discharging; 3:12 remaining present: true`
	p := parsePmsetBatt(onBattery)
	if !p.OnBattery || p.Percent != 54 || p.MinutesLeft != 3*60+12 {
		t.Errorf("on-battery parse = %+v", p)
	}
	if p.ChargeRateW >= 0 {
		t.Errorf("discharging should set a negative charge rate: %+v", p)
	}

	onAC := `Now drawing from 'AC Power'
 -InternalBattery-0 (id=12345)	100%; charged; 0:00 remaining present: true`
	p = parsePmsetBatt(onAC)
	if p.OnBattery || p.Percent != 100 {
		t.Errorf("on-AC parse = %+v", p)
	}
}

func TestParseLinuxBattery(t *testing.T) {
	p := parseLinuxBattery(map[string]string{
		"capacity":           "42",
		"status":             "Discharging",
		"energy_now":         "21000000",
		"energy_full":        "40000000",
		"energy_full_design": "50000000",
		"power_now":          "15000000", // 15 W
	})
	if !p.OnBattery || p.Percent != 42 {
		t.Errorf("parse = %+v", p)
	}
	if p.ChargeRateW != -15 {
		t.Errorf("charge rate = %v, want -15", p.ChargeRateW)
	}
	if p.DesignCapacityF < 0.79 || p.DesignCapacityF > 0.81 {
		t.Errorf("design capacity fraction = %v, want ~0.8", p.DesignCapacityF)
	}
	if p.MinutesLeft <= 0 {
		t.Errorf("expected a runtime estimate, got %d", p.MinutesLeft)
	}
}
