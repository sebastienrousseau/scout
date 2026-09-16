// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package probe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sebastienrousseau/scout/diagnostics"
	"github.com/sebastienrousseau/scout/internal/telemetry"
	"github.com/sebastienrousseau/scout/transport"
)

// ToolPerf is the latency profile of one tool over repeated calls.
type ToolPerf struct {
	Name    string `json:"name"`
	Samples int    `json:"samples"`
	Cold    Millis `json:"cold_ms"`
	P50     Millis `json:"p50_ms"`
	P95     Millis `json:"p95_ms"`
	Max     Millis `json:"max_ms"`
	Errors  int    `json:"errors"`
}

// ConcurrencyResult is the outcome of a parallel burst.
type ConcurrencyResult struct {
	Tool        string        `json:"tool"`
	Workers     int           `json:"workers"`
	Calls       int           `json:"calls"`
	OK          int           `json:"ok"`
	Errors      int           `json:"errors"`
	RateLimited int           `json:"rate_limited"`
	RetryAfter  bool          `json:"retry_after_header"`
	Wall        Millis  `json:"wall_ms"`
	Throughput  float64 `json:"calls_per_second"`
	P50         Millis  `json:"p50_ms"`
	P95         Millis  `json:"p95_ms"`
	Max         Millis  `json:"max_ms"`
}

// PerfResult is the performance phase output.
type PerfResult struct {
	Ping        *ToolPerf          `json:"ping,omitempty"`
	Tools       []ToolPerf         `json:"tools,omitempty"`
	Concurrency *ConcurrencyResult `json:"concurrency,omitempty"`
}

// phasePerformance measures repeat-call latency for tools that succeeded
// and runs a bounded parallel burst.
func phasePerformance(ctx context.Context, s *Session) []Finding {
	var out []Finding
	s.Perf = &PerfResult{}
	pctx := func(label string) context.Context { return telemetry.WithPhase(ctx, "performance", label) }
	limiter := diagnostics.NewLimiter(s.Opts.RPS, 1)

	// ping baseline
	{
		var lat diagnostics.Latencies
		live, liveParams := s.liveness()
		tp := ToolPerf{Name: live, Samples: s.Opts.Samples}
		for i := 0; i < s.Opts.Samples; i++ {
			if err := limiter.Wait(ctx); err != nil {
				return out
			}
			t0 := time.Now()
			if err := s.Client.Call(pctx(live), live, liveParams, nil); err != nil {
				tp.Errors++
				continue
			}
			d := time.Since(t0)
			if i == 0 {
				tp.Cold = Millis(d)
			}
			lat.Add(d)
		}
		tp.P50, tp.P95, tp.Max = Millis(lat.Percentile(50)), Millis(lat.Percentile(95)), Millis(lat.Max())
		s.Perf.Ping = &tp
		c := s.check("performance.ping", "Round-trip baseline ("+live+")")
		switch {
		case tp.Errors == s.Opts.Samples:
			out = append(out, c.fail(Major, "every "+live+" failed", ""))
		case tp.P50.Duration() > 500*time.Millisecond:
			out = append(out, c.warn(fmt.Sprintf("p50 %s p95 %s over %d samples", ms(tp.P50), ms(tp.P95), lat.Len()), "a slow no-op round trip points at the transport or auth layer, not the tools"))
		default:
			out = append(out, c.pass(fmt.Sprintf("p50 %s p95 %s max %s over %d samples", ms(tp.P50), ms(tp.P95), ms(tp.Max), lat.Len())))
		}
	}

	// repeat successful, allowed tools
	var candidates []ToolResult
	for _, r := range s.ToolResults {
		if r.Executed && r.OK {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		out = append(out, s.check("performance.tools", "Tool latency profile").skip("no tool completed successfully in the execution phase"))
	} else {
		var slow []string
		var allLat diagnostics.Latencies
		for _, r := range candidates {
			var lat diagnostics.Latencies
			tp := ToolPerf{Name: r.Name, Samples: s.Opts.Samples, Cold: r.Duration}
			for i := 0; i < s.Opts.Samples; i++ {
				if err := limiter.Wait(ctx); err != nil {
					return out
				}
				cctx, cancel := context.WithTimeout(pctx("repeat "+r.Name), s.Opts.CallTimeout)
				t0 := time.Now()
				res, err := s.Client.CallTool(cctx, r.Name, r.Arguments)
				d := time.Since(t0)
				cancel()
				if err != nil || res.IsError {
					tp.Errors++
					continue
				}
				lat.Add(d)
				allLat.Add(d)
			}
			tp.P50, tp.P95, tp.Max = Millis(lat.Percentile(50)), Millis(lat.Percentile(95)), Millis(lat.Max())
			s.Perf.Tools = append(s.Perf.Tools, tp)
			if tp.P95.Duration() > 2*time.Second {
				slow = append(slow, fmt.Sprintf("%s (p95 %.1fs)", r.Name, tp.P95.Duration().Seconds()))
			}
		}
		c := s.check("performance.tools", "Tool latency profile")
		summary := fmt.Sprintf("%d tools × %d samples: p50 %s p95 %s max %s", len(candidates), s.Opts.Samples, ms(allLat.Percentile(50)), ms(allLat.Percentile(95)), ms(allLat.Max()))
		if len(slow) > 0 {
			out = append(out, c.warn(summary+"; slow: "+strings.Join(slow, ", "), "p95 above 2s makes agents time out or retry; look at what those tools do on each call (indexing, unbounded scans) and cache or bound it"))
		} else {
			out = append(out, c.pass(summary))
		}
		c = s.check("performance.warmup", "Cold vs warm call")
		var coldDelta []string
		for _, tp := range s.Perf.Tools {
			if tp.P50 > 0 && tp.Cold > 4*tp.P50 && tp.Cold.Duration() > 500*time.Millisecond {
				coldDelta = append(coldDelta, fmt.Sprintf("%s cold %s vs p50 %s", tp.Name, ms(tp.Cold), ms(tp.P50)))
			}
		}
		if len(coldDelta) > 0 {
			out = append(out, c.info("first call much slower: "+fmt.Sprint(coldDelta)+" (lazy indexing or cache warm-up)"))
		} else {
			out = append(out, c.pass("no significant warm-up penalty"))
		}
	}

	// concurrency burst on the fastest successful tool (or ping)
	if s.Opts.Concurrency > 1 {
		tool := ""
		var args map[string]any
		best := Millis(1<<62 - 1)
		for _, tp := range s.Perf.Tools {
			if tp.Errors == 0 && tp.P50 < best {
				best, tool = tp.P50, tp.Name
			}
		}
		for _, r := range candidates {
			if r.Name == tool {
				args = r.Arguments
			}
		}
		workers := s.Opts.Concurrency
		per := s.Opts.Samples
		cr := ConcurrencyResult{Tool: tool, Workers: workers, Calls: workers * per}
		if tool == "" {
			cr.Tool = s.livenessName()
		}
		var mu sync.Mutex
		var lat diagnostics.Latencies
		var wg sync.WaitGroup
		wall := time.Now()
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < per; i++ {
					if !s.Opts.AllowLoad {
						if err := limiter.Wait(ctx); err != nil {
							return
						}
					}
					cctx, cancel := context.WithTimeout(pctx("burst "+cr.Tool), s.Opts.CallTimeout)
					t0 := time.Now()
					var err error
					var isErr bool
					if tool == "" {
						lm, lp := s.liveness()
						err = s.Client.Call(cctx, lm, lp, nil)
					} else {
						var res *transportCallResult
						res, err = callTool(cctx, s, tool, args)
						isErr = res != nil && res.IsError
					}
					d := time.Since(t0)
					cancel()
					mu.Lock()
					var he *transport.HTTPStatusError
					switch {
					case err != nil && asHTTP(err, &he) && he.StatusCode == http.StatusTooManyRequests:
						cr.RateLimited++
						if he.Header.Get("Retry-After") != "" {
							cr.RetryAfter = true
						}
					case err != nil || isErr:
						cr.Errors++
					default:
						cr.OK++
						lat.Add(d)
					}
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		cr.Wall = Millis(time.Since(wall))
		if cr.Wall > 0 {
			cr.Throughput = float64(cr.OK+cr.Errors+cr.RateLimited) / cr.Wall.Duration().Seconds()
		}
		cr.P50, cr.P95, cr.Max = Millis(lat.Percentile(50)), Millis(lat.Percentile(95)), Millis(lat.Max())
		s.Perf.Concurrency = &cr
		c := s.check("performance.concurrency", fmt.Sprintf("Parallel burst (%d workers × %d calls on %s)", workers, per, cr.Tool))
		detail := fmt.Sprintf("%d ok, %d errors, %d rate-limited in %s (%.1f calls/s); p50 %s p95 %s", cr.OK, cr.Errors, cr.RateLimited, ms(cr.Wall), cr.Throughput, ms(cr.P50), ms(cr.P95))
		switch {
		case cr.Errors > 0:
			out = append(out, c.fail(Major, detail, "errors under modest concurrency indicate shared-state or connection-handling bugs"))
		case cr.RateLimited > 0 && !cr.RetryAfter:
			out = append(out, c.warn(detail+"; 429 without Retry-After", "send Retry-After so clients back off correctly"))
		case cr.RateLimited > 0:
			out = append(out, c.pass(detail+"; 429 with Retry-After"))
		default:
			out = append(out, c.pass(detail))
		}
		if !s.Opts.AllowLoad && s.Opts.RPS > 0 {
			out = append(out, s.check("performance.throttle", "Burst was throttled").info(fmt.Sprintf("capped at %.0f req/s by scout; pass --allow-load to test rate limiting for real", s.Opts.RPS)))
		}
		if s.Opts.AllowLoad && cr.RateLimited == 0 {
			out = append(out, s.check("performance.rate_limit", "Server rate-limits an unthrottled burst").warn("no 429 observed", "if this is a shared tenant, consider per-client rate limits"))
		}
	}
	// keep the slice sorted for stable reports
	sort.Slice(s.Perf.Tools, func(i, j int) bool { return s.Perf.Tools[i].Name < s.Perf.Tools[j].Name })
	return out
}

type transportCallResult struct{ IsError bool }

func callTool(ctx context.Context, s *Session, name string, args map[string]any) (*transportCallResult, error) {
	res, err := s.Client.CallTool(ctx, name, args)
	if err != nil {
		return nil, err
	}
	return &transportCallResult{IsError: res.IsError}, nil
}

// asHTTP reports whether err wraps an HTTP status error, and binds it.
// errors.As already walks the chain, so there is nothing to unwrap by hand.
func asHTTP(err error, target **transport.HTTPStatusError) bool {
	return errors.As(err, target)
}
