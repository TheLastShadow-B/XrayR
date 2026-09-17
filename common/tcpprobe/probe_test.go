package tcpprobe

import (
	"context"
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
		if got := Interval(count); got != want {
			t.Errorf("%d targets: got %s want %s", count, got, want)
		}
		if Interval(count)-15*time.Second < time.Duration((count+5)/6)*9400*time.Millisecond {
			t.Fatal("insufficient timeout budget")
		}
	}
}
