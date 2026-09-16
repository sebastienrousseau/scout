// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sebastienrousseau/scout"
)

// Model is the language model the agentic probe drives. Implementations
// adapt a vendor SDK; the package ships none so it stays dependency-free.
type Model interface {
	// Step returns the model's next action given the conversation so far.
	Step(ctx context.Context, conv *Conversation) (Action, error)
}

// Conversation is the transcript handed to the model on each turn.
type Conversation struct {
	System string
	Task   string
	Tools  []scout.Tool
	Turns  []Turn
}

// Turn is one model action and, for tool calls, the observed result.
type Turn struct {
	Action Action
	Result string
	Error  string
}

// Action is what the model decided to do.
type Action struct {
	// ToolName, when set, requests a tool call with Arguments.
	ToolName  string
	Arguments map[string]any
	// Final, when ToolName is empty, is the model's answer.
	Final string
}

// AgentBudget bounds the agentic probe.
type AgentBudget struct {
	MaxTurns     int
	MaxToolCalls int
	// MaxRepeats is how many identical (tool, args) calls are tolerated
	// before the loop is declared stuck.
	MaxRepeats int
	// TurnTimeout bounds one model step plus tool call.
	TurnTimeout time.Duration
}

// DefaultAgentBudget is conservative enough for third-party servers.
var DefaultAgentBudget = AgentBudget{MaxTurns: 5, MaxToolCalls: 8, MaxRepeats: 2, TurnTimeout: 60 * time.Second}

// AgentOutcome summarises the agentic probe.
type AgentOutcome struct {
	Completed   bool     `json:"completed"`
	Turns       int      `json:"turns"`
	ToolCalls   int      `json:"tool_calls"`
	FailedCalls int      `json:"failed_calls"`
	Final       string   `json:"final,omitempty"`
	Findings    []string `json:"findings,omitempty"`
	Transcript  []Turn   `json:"transcript,omitempty"`
}

// ErrBudgetExhausted is recorded (not returned) when the loop is halted.
var ErrBudgetExhausted = errors.New("llm budget exhausted")

// caller abstracts the client so the agent loop is testable.
type caller interface {
	CallTool(ctx context.Context, name string, args any) (*scout.CallToolResult, error)
}

// runAgent drives model against tools through client, under budget and
// policy. Policy decisions apply to the model's calls exactly as they do
// to synthetic ones: a refused call is fed back to the model as an error.
func runAgent(ctx context.Context, client caller, model Model, tools []scout.Tool, task string, budget AgentBudget, policy Policy, limiter *Limiter) *AgentOutcome {
	out := &AgentOutcome{}
	conv := &Conversation{
		System: "You are validating an MCP server. Use the available tools to accomplish the task. Stop when done or when tools are not useful.",
		Task:   task,
		Tools:  tools,
	}
	byName := map[string]scout.Tool{}
	for _, t := range tools {
		byName[t.Name] = t
	}
	seen := map[string]int{}
	for out.Turns < budget.MaxTurns {
		out.Turns++
		tctx, cancel := context.WithTimeout(ctx, budget.TurnTimeout)
		act, err := model.Step(tctx, conv)
		if err != nil {
			cancel()
			out.Findings = append(out.Findings, "model error: "+err.Error())
			return out
		}
		if act.ToolName == "" {
			cancel()
			out.Completed, out.Final = true, act.Final
			conv.Turns = append(conv.Turns, Turn{Action: act})
			out.Transcript = conv.Turns
			return out
		}
		turn := Turn{Action: act}
		tool, known := byName[act.ToolName]
		switch {
		case !known:
			turn.Error = "unknown tool"
			out.Findings = append(out.Findings, fmt.Sprintf("agent called unknown tool %q: descriptions may be misleading", act.ToolName))
		case !policy.Decide(tool).Execute:
			turn.Error = "tool blocked by safety policy"
		default:
			if issues := validateArgs(tool, act.Arguments); len(issues) > 0 {
				out.Findings = append(out.Findings, fmt.Sprintf("agent produced invalid arguments for %s: %v (schema may be unclear)", tool.Name, issues))
			}
			key := act.ToolName + ":" + stableJSON(act.Arguments)
			seen[key]++
			if seen[key] > budget.MaxRepeats {
				cancel()
				out.Findings = append(out.Findings, fmt.Sprintf("agent repeated %s with identical arguments %d times: %v", act.ToolName, seen[key], ErrBudgetExhausted))
				conv.Turns = append(conv.Turns, turn)
				out.Transcript = conv.Turns
				return out
			}
			if out.ToolCalls >= budget.MaxToolCalls {
				cancel()
				out.Findings = append(out.Findings, fmt.Sprintf("tool call cap %d reached: %v", budget.MaxToolCalls, ErrBudgetExhausted))
				out.Transcript = conv.Turns
				return out
			}
			if err := limiter.Wait(tctx); err != nil {
				cancel()
				out.Findings = append(out.Findings, "cancelled: "+err.Error())
				out.Transcript = conv.Turns
				return out
			}
			out.ToolCalls++
			res, err := client.CallTool(tctx, act.ToolName, act.Arguments)
			switch {
			case err != nil:
				out.FailedCalls++
				turn.Error = err.Error()
			case res.IsError:
				out.FailedCalls++
				turn.Error = "tool error: " + res.Text()
			default:
				turn.Result = res.Text()
				if len(res.StructuredContent) > 0 {
					turn.Result += "\n" + string(res.StructuredContent)
				}
			}
		}
		cancel()
		conv.Turns = append(conv.Turns, turn)
	}
	out.Findings = append(out.Findings, fmt.Sprintf("turn cap %d reached without a final answer: %v", budget.MaxTurns, ErrBudgetExhausted))
	out.Transcript = conv.Turns
	return out
}

func validateArgs(t scout.Tool, args map[string]any) []string {
	if len(t.InputSchema) == 0 {
		return nil
	}
	b, err := json.Marshal(args)
	if err != nil {
		return []string{err.Error()}
	}
	return Validate(t.InputSchema, b)
}

func stableJSON(v any) string {
	b, _ := json.Marshal(v) // encoding/json sorts map keys
	return string(b)
}
