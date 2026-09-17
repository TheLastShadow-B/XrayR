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
