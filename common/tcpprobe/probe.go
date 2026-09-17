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
	// Measurement budget. Absent fields fall back to the built-in defaults.
	Attempts        int `json:"attempts"`
	TimeoutMS       int `json:"timeout_ms"`
	IntervalSeconds int `json:"interval_seconds"`
	// ThresholdMS is the panel's colouring cutoff. Decoded for completeness only:
	// XrayR measures, the panel judges.
	ThresholdMS int `json:"threshold_ms"`
}

const (
	maxWorkers      = 6
	attemptGap      = 200 * time.Millisecond
	apiBudget       = 15 * time.Second
	defaultAttempts = 3
	maxAttempts     = 10
	defaultTimeout  = 3 * time.Second
	minTimeout      = 200 * time.Millisecond
	maxTimeout      = 10 * time.Second
	minInterval     = time.Minute
	maxInterval     = time.Hour
)

// Tuning is the normalized measurement budget for one round.
type Tuning struct {
	Attempts int
	Timeout  time.Duration
	Interval time.Duration // Zero derives the period from the target count.
}

// Tuning clamps the panel's parameters into a range that can neither stall the node
// nor truncate a round. A typo in the panel degrades one setting, never the whole probe.
func (c Config) Tuning() Tuning {
	tuning := Tuning{Attempts: defaultAttempts, Timeout: defaultTimeout}
	if c.Attempts > 0 {
		tuning.Attempts = min(c.Attempts, maxAttempts)
	}
	if c.TimeoutMS > 0 {
		tuning.Timeout = min(max(time.Duration(c.TimeoutMS)*time.Millisecond, minTimeout), maxTimeout)
	}
	if c.IntervalSeconds > 0 {
		tuning.Interval = min(max(time.Duration(c.IntervalSeconds)*time.Second, minInterval), maxInterval)
	}
	return tuning
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
	if len(config.Hash) != 64 {
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

// Interval reserves enough time for every target even when all connections time out.
// The panel's interval_seconds may stretch that period but never shrink it past the
// reservation. Keep this calculation in sync with SSPanel's TcpProbe::interval.
func Interval(targetCount int, tuning Tuning) time.Duration {
	// Round the per-target worst case up to a whole second, matching the panel's arithmetic.
	perTarget := time.Duration(tuning.Attempts)*tuning.Timeout + time.Duration(tuning.Attempts-1)*attemptGap
	perTarget = (perTarget + time.Second - 1).Truncate(time.Second)
	batches := (targetCount + maxWorkers - 1) / maxWorkers
	period := time.Duration(batches)*perTarget + apiBudget
	period = max(period, tuning.Interval)
	minutes := int64((period + time.Minute - 1) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	return time.Duration(minutes) * time.Minute
}

func Measure(ctx context.Context, config Config, dial DialFunc) (Report, error) {
	if err := Validate(config); err != nil {
		return Report{}, err
	}
	tuning := config.Tuning()
	ctx, cancelRound := context.WithCancel(ctx)
	defer cancelRound()
	report := Report{Hash: config.Hash, MeasuredAt: time.Now().Unix(), Results: make([]Result, len(config.Targets))}
	var wg sync.WaitGroup
	var localError error
	var mu sync.Mutex
	jobs := make(chan int)
	workers := maxWorkers
	if len(config.Targets) < workers {
		workers = len(config.Targets)
	}
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				target := config.Targets[i]
				result := Result{TargetID: target.ID, Samples: make([]Sample, 0, tuning.Attempts)}
				for attempt := 0; attempt < tuning.Attempts; attempt++ {
					if attempt > 0 {
						timer := time.NewTimer(attemptGap)
						select {
						case <-timer.C:
						case <-ctx.Done():
							timer.Stop()
							return
						}
					}
					attemptCtx, cancel := context.WithTimeout(ctx, tuning.Timeout)
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
							// Cancel the entire round; never turn local failure into an outage.
							cancelRound()
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
			}
		}()
	}
dispatch:
	for i := range config.Targets {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if localError != nil {
		return Report{}, localError
	}
	if ctx.Err() != nil {
		return Report{}, ctx.Err()
	}
	return report, nil
}

func RunOnce(ctx context.Context, client Client, sourceIP string) error {
	_, err := runOnce(ctx, client, sourceIP)
	return err
}

func runOnce(ctx context.Context, client Client, sourceIP string) (time.Duration, error) {
	configCtx, cancelConfig := context.WithTimeout(ctx, 10*time.Second)
	config, err := client.GetTCPProbeConfig(configCtx)
	cancelConfig()
	tuning := config.Tuning()
	interval := Interval(len(config.Targets), tuning)
	if err != nil || !config.Enabled || len(config.Targets) == 0 {
		return time.Minute, err
	}
	roundCtx, cancel := context.WithTimeout(ctx, interval-apiBudget)
	defer cancel()
	dialer := &net.Dialer{Timeout: tuning.Timeout}
	if sourceIP != "" && sourceIP != "0.0.0.0" && sourceIP != "::" {
		ip := net.ParseIP(sourceIP)
		if ip == nil || ip.To4() == nil {
			return interval, errors.New("TCP probe SendIP must be an IPv4 address")
		}
		dialer.LocalAddr = &net.TCPAddr{IP: ip}
	}
	report, err := Measure(roundCtx, config, dialer.DialContext)
	if err != nil {
		return interval, err
	}
	return interval, client.ReportTCPProbe(roundCtx, report)
}

// Run schedules non-overlapping rounds, with bounded concurrency and an adaptive interval.
// Config is fetched independently of node-info polling, without rebuilding inbounds.
func Run(ctx context.Context, client Client, sourceIP string, nodeID int, logError func(error)) {
	next := time.Now().Truncate(time.Minute).Add(time.Duration(nodeID%10+1) * time.Second)
	if !next.After(time.Now()) {
		next = next.Add(time.Minute)
	}
	for {
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		interval, err := runOnce(ctx, client, sourceIP)
		if err != nil && ctx.Err() == nil {
			logError(fmt.Errorf("TCP probe: %w", err))
		}
		next = next.Add(interval)
		// Do not issue catch-up rounds after a delayed API call or system suspend.
		for !next.After(time.Now()) {
			next = next.Add(interval)
		}
	}
}
