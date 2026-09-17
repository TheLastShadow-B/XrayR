package tcpprobe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func fixture() Config {
	return Config{Enabled: true, Hash: strings.Repeat("a", 64), Targets: []Target{{ID: 1, IP: "1.1.1.1", Port: 443}}}
}

type countedConn struct {
	net.Conn
	closed *atomic.Int32
}

func (c countedConn) Close() error { c.closed.Add(1); return c.Conn.Close() }

func TestMeasureUsesTCPConnectAndClosesEverySocket(t *testing.T) {
	var closed atomic.Int32
	report, err := Measure(context.Background(), fixture(), func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp4" || address != "1.1.1.1:443" {
			t.Error("unexpected target")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 3*time.Second {
			t.Error("missing bounded deadline")
		}
		a, b := net.Pipe()
		b.Close()
		return countedConn{a, &closed}, nil
	})
	if err != nil || closed.Load() != 3 || len(report.Results[0].Samples) != 3 {
		t.Fatalf("measurement failed: %+v %v", report, err)
	}
	for _, sample := range report.Results[0].Samples {
		if sample.Error != nil || sample.LatencyMS == nil {
			t.Fatal("successful sample malformed")
		}
	}
}

func TestMeasureClassifiesFailures(t *testing.T) {
	for _, tc := range []struct {
		err  error
		kind string
	}{{syscall.ECONNREFUSED, "refused"}, {context.DeadlineExceeded, "timeout"}, {syscall.ENETUNREACH, "unreachable"}} {
		t.Run(tc.kind, func(t *testing.T) {
			r, err := Measure(context.Background(), fixture(), func(context.Context, string, string) (net.Conn, error) { return nil, tc.err })
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range r.Results[0].Samples {
				if s.LatencyMS != nil || s.Error == nil || *s.Error != tc.kind {
					t.Fatal("incorrect failure classification")
				}
			}
		})
	}
}

func TestMeasureCancellationAndLocalFailureDoNotReportNetworkOutage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Measure(ctx, fixture(), func(context.Context, string, string) (net.Conn, error) { return nil, context.Canceled })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, err = Measure(context.Background(), fixture(), func(context.Context, string, string) (net.Conn, error) { return nil, syscall.EADDRNOTAVAIL })
	if err == nil {
		t.Fatal("local bind failure must discard round")
	}
}

func TestRejectPrivateTargets(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "224.0.0.1"} {
		c := fixture()
		c.Targets[0].IP = ip
		if Validate(c) == nil {
			t.Fatalf("accepted %s", ip)
		}
	}
}

type disabledClient struct{ reportCalls int }

func (*disabledClient) GetTCPProbeConfig(context.Context) (Config, error) { return Config{}, nil }
func (c *disabledClient) ReportTCPProbe(context.Context, Report) error    { c.reportCalls++; return nil }
func TestDisabledConfigDoesNotMeasureOrReport(t *testing.T) {
	c := &disabledClient{}
	if err := RunOnce(context.Background(), c, ""); err != nil || c.reportCalls != 0 {
		t.Fatal("disabled probe reported")
	}
}

func TestMeasureDynamicTargetsKeepsAllIDsAndBoundsConcurrency(t *testing.T) {
	config := fixture()
	config.Targets = nil
	for i := 0; i < 32; i++ {
		config.Targets = append(config.Targets, Target{ID: 100 + i, IP: "1.1.1.1", Port: 443})
	}
	var active, maximum, calls atomic.Int32
	report, err := Measure(context.Background(), config, func(ctx context.Context, network, address string) (net.Conn, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for previous := maximum.Load(); n > previous; previous = maximum.Load() {
			if maximum.CompareAndSwap(previous, n) {
				break
			}
		}
		calls.Add(1)
		time.Sleep(time.Millisecond)
		return nil, syscall.ECONNREFUSED
	})
	if err != nil || len(report.Results) != 32 || calls.Load() != 96 || maximum.Load() > 6 {
		t.Fatalf("incomplete or unbounded round: results=%d calls=%d concurrency=%d err=%v", len(report.Results), calls.Load(), maximum.Load(), err)
	}
	for i, result := range report.Results {
		if result.TargetID != 100+i || len(result.Samples) != 3 {
			t.Fatal("target lost or ID changed")
		}
	}
}

func TestMeasureDynamicTargetsCancelsQueuedWorkOnLocalFailure(t *testing.T) {
	config := fixture()
	for i := 2; i <= 40; i++ {
		config.Targets = append(config.Targets, Target{ID: i, IP: "1.1.1.1", Port: 443})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := Measure(ctx, config, func(context.Context, string, string) (net.Conn, error) { return nil, syscall.EMFILE })
	if err == nil || ctx.Err() != nil {
		t.Fatalf("local failure blocked dispatch: %v", err)
	}
}

func TestTCPProbeAdaptiveInterval(t *testing.T) {
	for count, want := range map[int]time.Duration{0: time.Minute, 9: time.Minute, 24: time.Minute, 25: 2 * time.Minute, 32: 2 * time.Minute, 60: 2 * time.Minute, 61: 3 * time.Minute, 1000: 29 * time.Minute} {
		if got := Interval(count, Config{}.Tuning()); got != want {
			t.Errorf("%d targets: got %s want %s", count, got, want)
		}
		if Interval(count, Config{}.Tuning())-15*time.Second < time.Duration((count+5)/6)*9400*time.Millisecond {
			t.Fatal("insufficient timeout budget")
		}
	}
}

func TestTuningFallsBackWhenPanelOmitsFields(t *testing.T) {
	// Panels predating these fields send nothing; zero values must not disable measurement.
	if got := (Config{}).Tuning(); got != (Tuning{Attempts: 3, Timeout: 3 * time.Second}) {
		t.Fatalf("older panels lost the built-in defaults: %+v", got)
	}
}

func TestTuningClampsOutOfRangePanelValues(t *testing.T) {
	for _, tc := range []struct {
		config Config
		want   Tuning
	}{
		{Config{Attempts: 99, TimeoutMS: 99999, IntervalSeconds: 99999}, Tuning{Attempts: 10, Timeout: 10 * time.Second, Interval: time.Hour}},
		{Config{Attempts: -1, TimeoutMS: 1, IntervalSeconds: 1}, Tuning{Attempts: 3, Timeout: 200 * time.Millisecond, Interval: time.Minute}},
	} {
		if got := tc.config.Tuning(); got != tc.want {
			t.Errorf("%+v: got %+v want %+v", tc.config, got, tc.want)
		}
	}
}

func TestMeasureHonorsPanelAttemptsAndTimeout(t *testing.T) {
	config := fixture()
	config.Attempts, config.TimeoutMS = 5, 1000
	var calls atomic.Int32
	report, err := Measure(context.Background(), config, func(ctx context.Context, _, _ string) (net.Conn, error) {
		calls.Add(1)
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > time.Second {
			t.Error("attempt deadline ignores panel timeout_ms")
		}
		return nil, syscall.ECONNREFUSED
	})
	if err != nil || calls.Load() != 5 || len(report.Results[0].Samples) != 5 {
		t.Fatalf("panel attempts ignored: calls=%d samples=%+v err=%v", calls.Load(), report.Results, err)
	}
}

func TestIntervalHonorsPanelValueButNeverBreaksSafetyFloor(t *testing.T) {
	// The panel may stretch the period.
	if got := Interval(9, Config{IntervalSeconds: 300}.Tuning()); got != 5*time.Minute {
		t.Errorf("panel interval ignored: got %s want 5m", got)
	}
	// It may not shrink it below the time a full round actually needs.
	if got := Interval(60, Config{IntervalSeconds: 60}.Tuning()); got != 2*time.Minute {
		t.Errorf("panel interval broke the safety floor: got %s want 2m", got)
	}
	// A longer per-attempt timeout widens that floor.
	if got := Interval(9, Config{Attempts: 5, TimeoutMS: 9000}.Tuning()); got != 2*time.Minute {
		t.Errorf("floor ignores panel timeout: got %s want 2m", got)
	}
}

func TestConfigDecodesPanelTuningFields(t *testing.T) {
	payload := `{"enabled":true,"threshold_ms":250,"interval_seconds":120,"timeout_ms":1500,"attempts":5,` +
		`"targets":[{"id":1,"carrier":"telecom","label":"BJ","ip":"219.141.150.166","port":65499}],` +
		`"config_hash":"` + strings.Repeat("a", 64) + `"}`
	var config Config
	if err := json.Unmarshal([]byte(payload), &config); err != nil {
		t.Fatal(err)
	}
	// threshold_ms is the panel's colouring cutoff: decoded, never consumed here.
	if config.ThresholdMS != 250 {
		t.Error("threshold_ms did not decode")
	}
	if got := config.Tuning(); got != (Tuning{Attempts: 5, Timeout: 1500 * time.Millisecond, Interval: 2 * time.Minute}) {
		t.Fatalf("panel tuning not applied: %+v", got)
	}
}
