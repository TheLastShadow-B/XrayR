// Package tcpprobe measures TCP connection establishment from the node's host.
// It never sends application payloads and does not enter Xray routing rules.
package tcpprobe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type Target struct {
	ID      int    `json:"id"`
	Carrier string `json:"carrier"`
	Label   string `json:"label"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
}

type Config struct {
	Enabled bool     `json:"enabled"`
	Hash    string   `json:"config_hash"`
	Targets []Target `json:"targets"`
}

type Sample struct {
	LatencyMS *float64 `json:"latency_ms"`
	Error     *string  `json:"error"`
}

type Result struct {
	TargetID int      `json:"target_id"`
	Samples  []Sample `json:"samples"`
}

type Report struct {
	Hash       string   `json:"config_hash"`
	MeasuredAt int64    `json:"measured_at"`
	Results    []Result `json:"results"`
}

// Client is optional: existing panel implementations need not implement it.
type Client interface {
	GetTCPProbeConfig(context.Context) (Config, error)
	ReportTCPProbe(context.Context, Report) error
}

type DialFunc func(context.Context, string, string) (net.Conn, error)

func Validate(config Config) error {
	if len(config.Hash) != 64 || len(config.Targets) > 9 {
		return errors.New("invalid TCP probe configuration")
	}
	seen := map[int]bool{}
	for _, target := range config.Targets {
		ip := net.ParseIP(target.IP)
		if target.ID < 1 || seen[target.ID] || ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
			target.Port < 1 || target.Port > 65535 {
			return errors.New("TCP probe requires unique targets with public IPv4 addresses and valid ports")
		}
		seen[target.ID] = true
	}
	return nil
}

func Measure(ctx context.Context, config Config, dial DialFunc) (Report, error) {
	if err := Validate(config); err != nil {
		return Report{}, err
	}
	report := Report{Hash: config.Hash, MeasuredAt: time.Now().Unix(), Results: make([]Result, len(config.Targets))}
	var wg sync.WaitGroup
	var localError error
	var mu sync.Mutex
	semaphore := make(chan struct{}, 6)
	for i, target := range config.Targets {
		wg.Add(1)
		go func(i int, target Target) {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			result := Result{TargetID: target.ID, Samples: make([]Sample, 0, 3)}
			for attempt := 0; attempt < 3; attempt++ {
				if attempt > 0 {
					timer := time.NewTimer(200 * time.Millisecond)
					select {
					case <-timer.C:
					case <-ctx.Done():
						timer.Stop()
						return
					}
				}
				attemptCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				start := time.Now()
				conn, err := dial(attemptCtx, "tcp4", net.JoinHostPort(target.IP, strconv.Itoa(target.Port)))
				elapsed := float64(time.Since(start).Microseconds()) / 1000
				cancel()
				if conn != nil {
					conn.Close()
				}
				sample := Sample{}
				if err == nil {
					sample.LatencyMS = &elapsed
				} else {
					// A broken local bind or exhausted FD table is not a failed route.
					if errors.Is(err, syscall.EADDRNOTAVAIL) || errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) {
						mu.Lock()
						localError = errors.New("TCP probe local socket/source address unavailable")
						mu.Unlock()
						return
					}
					kind := "other"
					var netError net.Error
					switch {
					case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netError) && netError.Timeout()):
						kind = "timeout"
					case errors.Is(err, syscall.ECONNREFUSED):
						kind = "refused"
					case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
						kind = "unreachable"
					}
					sample.Error = &kind
				}
				result.Samples = append(result.Samples, sample)
			}
			report.Results[i] = result
		}(i, target)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return Report{}, ctx.Err()
	}
	if localError != nil {
		return Report{}, localError
	}
	return report, nil
}

func RunOnce(ctx context.Context, client Client, sourceIP string) error {
	config, err := client.GetTCPProbeConfig(ctx)
	if err != nil || !config.Enabled || len(config.Targets) == 0 {
		return err
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	if sourceIP != "" && sourceIP != "0.0.0.0" && sourceIP != "::" {
		ip := net.ParseIP(sourceIP)
		if ip == nil || ip.To4() == nil {
			return errors.New("TCP probe SendIP must be an IPv4 address")
		}
		dialer.LocalAddr = &net.TCPAddr{IP: ip}
	}
	report, err := Measure(ctx, config, dialer.DialContext)
	if err != nil {
		return err
	}
	return client.ReportTCPProbe(ctx, report)
}

// Run schedules one bounded round per minute. Config is fetched independently
// of node-info polling so target edits never rebuild a proxy inbound.
func Run(ctx context.Context, client Client, sourceIP string, nodeID int, logError func(error)) {
	for {
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Duration(nodeID%10+1) * time.Second)
		if !next.After(now) {
			next = next.Add(time.Minute)
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		roundCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := RunOnce(roundCtx, client, sourceIP)
		cancel()
		if err != nil && ctx.Err() == nil {
			logError(fmt.Errorf("TCP probe: %w", err))
		}
	}
}
