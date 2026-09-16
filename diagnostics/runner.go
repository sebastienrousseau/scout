// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sebastienrousseau/scout"
	"github.com/sebastienrousseau/scout/trace"
)

// Options configures a Runner.
type Options struct {
	Policy Policy
	// RequestsPerSecond throttles tool calls. Defaults to 2; zero or
	// negative disables throttling (only do this against your own servers).
	RequestsPerSecond float64
	// CallTimeout bounds each synthetic tool call. Default 30s.
	CallTimeout time.Duration
	// Seed makes generated arguments reproducible.
	Seed uint64
	// FillOptional also populates optional schema properties.
	FillOptional bool
	// OutlierFactor and OutlierFloor define a latency outlier: at least
	// factor times the median and above floor. Defaults 4x and 500ms.
	OutlierFactor float64
	OutlierFloor  time.Duration
	// Concurrency is how many tools are exercised at once. The rate limit
	// still applies across all of them, so this trades wall-clock for
	// nothing the server can tell apart from a slower serial run. Defaults
	// to 1, which preserves the original strictly-serial behaviour.
	Concurrency int
	// Model, when set, enables the agentic probe.
	Model     Model
	AgentTask string
	Budget    AgentBudget
}

// Runner executes the diagnostic suite.
type Runner struct {
	opts Options
}

// NewRunner applies defaults to opts.
func NewRunner(opts Options) *Runner {
	if opts.RequestsPerSecond == 0 {
		opts.RequestsPerSecond = 2
	}
	if opts.CallTimeout == 0 {
		opts.CallTimeout = 30 * time.Second
	}
	if opts.OutlierFactor == 0 {
		opts.OutlierFactor = 4
	}
	if opts.OutlierFloor == 0 {
		opts.OutlierFloor = 500 * time.Millisecond
	}
	if opts.Budget == (AgentBudget{}) {
		opts.Budget = DefaultAgentBudget
	}
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}
	if opts.AgentTask == "" {
		opts.AgentTask = "Use the available read-only tools to learn what this server can do and summarise it in three sentences."
	}
	return &Runner{opts: opts}
}

// ToolResult records one synthetic invocation.
type ToolResult struct {
	Name         string         `json:"name"`
	Executed     bool           `json:"executed"`
	SkipReason   string         `json:"skip_reason,omitempty"`
	Arguments    map[string]any `json:"arguments,omitempty"`
	Duration     time.Duration  `json:"duration_ns,omitempty"`
	OK           bool           `json:"ok"`
	ToolError    string         `json:"tool_error,omitempty"`
	ProtoError   string         `json:"protocol_error,omitempty"`
	SchemaIssues []string       `json:"schema_issues,omitempty"`
	Outlier      bool           `json:"outlier,omitempty"`
}

// Report is the observability output of one run.
type Report struct {
	TraceID   string                  `json:"trace_id"`
	StartedAt time.Time               `json:"started_at"`
	Duration  time.Duration           `json:"duration_ns"`
	Server    *scout.InitializeResult `json:"server,omitempty"`

	ToolsDiscovered int          `json:"tools_discovered"`
	ToolsExecuted   int          `json:"tools_executed"`
	ToolsSkipped    int          `json:"tools_skipped"`
	Successful      int          `json:"successful"`
	ToolErrors      int          `json:"tool_errors"`
	ProtocolErrors  int          `json:"protocol_errors"`
	Results         []ToolResult `json:"results"`

	Latency          Summary  `json:"latency"`
	Outliers         []string `json:"outliers,omitempty"`
	SchemaMismatches []string `json:"schema_mismatches,omitempty"`
	MissingSchemas   []string `json:"missing_output_schemas,omitempty"`
	Unannotated      []string `json:"unannotated_tools,omitempty"`

	Agent *AgentOutcome `json:"agent,omitempty"`

	QualityScore float64  `json:"quality_score"`
	Deductions   []string `json:"deductions,omitempty"`
}

// Run lists tools, invokes the ones the policy allows with generated
// arguments, validates structured results, optionally drives the agentic
// probe, and scores the outcome.
func (r *Runner) Run(ctx context.Context, client *scout.Client) (*Report, error) {
	ctx = trace.Ensure(ctx)
	rep := &Report{TraceID: trace.FromContext(ctx), StartedAt: time.Now(), Server: client.ServerInfo()}
	defer func() { rep.Duration = time.Since(rep.StartedAt) }()

	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("diagnostics: tools/list: %w", err)
	}
	rep.ToolsDiscovered = len(tools)

	limiter := NewLimiter(r.opts.RequestsPerSecond, 1)
	var lat Latencies

	// Plan first, so argument generation (which is seeded and therefore
	// order-dependent) stays deterministic regardless of Concurrency.
	gen := NewGenerator(r.opts.Seed)
	gen.FillOptional = r.opts.FillOptional
	results := make([]ToolResult, len(tools))
	var runnable []int
	for i, t := range tools {
		if t.Annotations == nil {
			rep.Unannotated = append(rep.Unannotated, t.Name)
		}
		if len(t.OutputSchema) == 0 {
			rep.MissingSchemas = append(rep.MissingSchemas, t.Name)
		}
		tr := ToolResult{Name: t.Name}
		if d := r.opts.Policy.Decide(t); !d.Execute {
			tr.SkipReason = d.Reason
			rep.ToolsSkipped++
			results[i] = tr
			continue
		}
		args, err := gen.Arguments(t.InputSchema)
		if err != nil {
			tr.SkipReason = err.Error()
			rep.ToolsSkipped++
			rep.SchemaMismatches = append(rep.SchemaMismatches, fmt.Sprintf("%s: inputSchema unparsable: %v", t.Name, err))
			results[i] = tr
			continue
		}
		tr.Executed, tr.Arguments = true, args
		rep.ToolsExecuted++
		results[i] = tr
		runnable = append(runnable, i)
	}

	var mu sync.Mutex
	var runErr error
	work := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < r.opts.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				t, tr := tools[i], &results[i]
				if err := limiter.Wait(ctx); err != nil {
					mu.Lock()
					if runErr == nil {
						runErr = err
					}
					mu.Unlock()
					return
				}
				cctx, cancel := context.WithTimeout(ctx, r.opts.CallTimeout)
				start := time.Now()
				res, err := client.CallTool(cctx, t.Name, tr.Arguments)
				d := time.Since(start)
				cancel()

				mu.Lock()
				tr.Duration = d
				lat.Add(d)
				switch {
				case err != nil:
					rep.ProtocolErrors++
					tr.ProtoError = err.Error()
					if errors.Is(err, context.DeadlineExceeded) {
						tr.ProtoError = "timeout after " + r.opts.CallTimeout.String()
					}
				case res.IsError:
					rep.ToolErrors++
					tr.ToolError = res.Text()
				default:
					tr.OK = true
					rep.Successful++
					if len(t.OutputSchema) > 0 {
						if len(res.StructuredContent) == 0 {
							tr.SchemaIssues = []string{"outputSchema declared but structuredContent missing"}
						} else {
							tr.SchemaIssues = Validate(t.OutputSchema, res.StructuredContent)
						}
						for _, is := range tr.SchemaIssues {
							rep.SchemaMismatches = append(rep.SchemaMismatches, t.Name+": "+is)
						}
					}
				}
				mu.Unlock()
			}
		}()
	}
	failed := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return runErr != nil
	}
producer:
	for _, i := range runnable {
		select {
		case work <- i:
		case <-ctx.Done():
			mu.Lock()
			if runErr == nil {
				runErr = ctx.Err()
			}
			mu.Unlock()
			break producer
		}
		if failed() {
			break
		}
	}
	close(work)
	wg.Wait()
	rep.Results = results
	if runErr != nil {
		return rep, runErr
	}

	// Deterministic ordering regardless of completion order.
	sort.SliceStable(rep.SchemaMismatches, func(i, j int) bool { return rep.SchemaMismatches[i] < rep.SchemaMismatches[j] })
	rep.Latency = lat.Summarize()
	for i := range rep.Results {
		tr := &rep.Results[i]
		if tr.Executed && lat.IsOutlier(tr.Duration, r.opts.OutlierFactor, r.opts.OutlierFloor) {
			tr.Outlier = true
			rep.Outliers = append(rep.Outliers, fmt.Sprintf("%s took %s (p50 %s)", tr.Name, tr.Duration, rep.Latency.P50))
		}
	}

	if r.opts.Model != nil {
		rep.Agent = runAgent(ctx, client, r.opts.Model, tools, r.opts.AgentTask, r.opts.Budget, r.opts.Policy, limiter)
	}

	rep.QualityScore, rep.Deductions = Score(rep)
	return rep, nil
}

// Score computes the 0–100 quality rating and the reasons for each
// deduction. Weights: call failures up to 50, schema contract breaks 5 each
// (cap 20), slow tail 10, missing annotations/schemas up to 10, agent
// usability findings 5 each (cap 20).
func Score(rep *Report) (float64, []string) {
	score := 100.0
	var why []string
	ded := func(pts float64, reason string) {
		score -= pts
		why = append(why, fmt.Sprintf("-%.0f %s", pts, reason))
	}
	if rep.ToolsDiscovered == 0 {
		ded(100, "no tools discovered")
		return 0, why
	}
	if rep.ToolsExecuted > 0 {
		failed := rep.ToolErrors + rep.ProtocolErrors
		if failed > 0 {
			ded(float64(failed)/float64(rep.ToolsExecuted)*50, fmt.Sprintf("%d of %d executed tools failed", failed, rep.ToolsExecuted))
		}
	}
	if n := len(rep.SchemaMismatches); n > 0 {
		ded(min(20, float64(n)*5), fmt.Sprintf("%d schema contract violations", n))
	}
	if rep.Latency.P99 > 2*time.Second {
		ded(10, "p99 latency above 2s")
	}
	if n := len(rep.Unannotated); n > 0 {
		ded(min(5, float64(n)), fmt.Sprintf("%d tools without annotations", n))
	}
	if n := len(rep.MissingSchemas); n > 0 {
		ded(min(5, float64(n)), fmt.Sprintf("%d tools without outputSchema", n))
	}
	if rep.Agent != nil {
		if n := len(rep.Agent.Findings); n > 0 {
			ded(min(20, float64(n)*5), fmt.Sprintf("%d agent usability findings", n))
		}
	}
	if score < 0 {
		score = 0
	}
	return score, why
}
