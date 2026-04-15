package runtime

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"iter"
	"strings"
	"sync"
	"time"

	agenterrclass "github.com/ethpandaops/agent-sdk-observability/errclass"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	sdkerrors "github.com/ethpandaops/openrouter-agent-sdk-go/internal/errors"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/hook"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/mcp"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/message"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/observability"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/permission"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/session"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/tools"
	upstreamgenai "go.opentelemetry.io/otel/semconv/v1.40.0/genaiconv"
)

type pendingToolCall struct {
	ID   string
	Name string
	Args strings.Builder
}

func attachAuditEnvelope(msg message.Message, eventType, subtype string, payload any) {
	audit, err := message.NewAuditEnvelope(eventType, subtype, payload)
	if err != nil {
		return
	}

	switch typed := msg.(type) {
	case *message.UserMessage:
		typed.Audit = audit
	case *message.AssistantMessage:
		typed.Audit = audit
	case *message.SystemMessage:
		typed.Audit = audit
	case *message.ResultMessage:
		typed.Audit = audit
	case *message.StreamEvent:
		typed.Audit = audit
	}
}

func attachRawAuditEnvelope(
	msg message.Message, eventType, subtype string, payload map[string]any,
) {
	if len(payload) == 0 {
		return
	}

	attachAuditEnvelope(msg, eventType, subtype, payload)
}

// QueryRunner executes prompt/query flows over OpenRouter transport.
type QueryRunner struct {
	opts      *config.Options
	transport config.Transport
	sessions  *session.Manager
	hooks     *hook.Dispatcher
	registry  *tools.Registry
	executor  *tools.Executor
	obs       *observability.Observer
}

// NewQueryRunner creates a QueryRunner.
func NewQueryRunner(
	opts *config.Options,
	sessions *session.Manager,
	obs *observability.Observer,
) *QueryRunner {
	if sessions == nil {
		sessions = session.NewManager()
	}
	if obs == nil {
		obs = observability.Noop()
	}
	opts.ApplyDefaults()
	registry := tools.NewRegistry(opts)
	return &QueryRunner{
		opts:      opts,
		transport: opts.Transport,
		sessions:  sessions,
		hooks:     NewHookDispatcher(opts),
		registry:  registry,
		executor:  tools.NewExecutor(opts, registry),
		obs:       obs,
	}
}

// MCPServerStatuses returns MCP server readiness discovered during registry init.
func (r *QueryRunner) MCPServerStatuses() map[string]mcp.ServerStatus {
	if r == nil || r.registry == nil {
		return map[string]mcp.ServerStatus{}
	}
	return r.registry.ServerStatuses()
}

// SetPermissionMode updates the active permission mode used by tool execution.
func (r *QueryRunner) SetPermissionMode(mode string) {
	if r == nil {
		return
	}
	if r.opts != nil {
		r.opts.PermissionMode = mode
	}
	if r.executor != nil && mode != "" {
		r.executor.SetMode(permission.Mode(mode))
	}
}

// NewHookDispatcher is split for testability.
func NewHookDispatcher(opts *config.Options) *hook.Dispatcher {
	if opts == nil {
		return hook.NewDispatcher(nil)
	}
	return hook.NewDispatcher(opts.Hooks)
}

// runHookWithObs wraps a hook dispatch call with observability instrumentation,
// creating a span and recording the hook execution duration.
func (r *QueryRunner) runHookWithObs(
	ctx context.Context,
	event hook.Event,
	toolName string,
	input hook.Input,
	toolUseID *string,
) ([]hook.JSONOutput, error) {
	hookCtx, hookSpan := r.obs.StartHookSpan(ctx, string(event))
	start := time.Now()
	outs, err := r.hooks.Run(hookCtx, event, toolName, input, toolUseID)
	outcome := "ok"
	if err != nil {
		outcome = "error"
		hookSpan.RecordError(err)
	} else {
		hookSpan.SetAttributes(observability.Outcome(outcome))
	}
	hookSpan.End()
	r.obs.RecordHookDuration(hookCtx, time.Since(start).Seconds(), string(event), outcome)

	return outs, err
}

// RunPrompt runs a simple user-content turn and streams message outputs.
func (r *QueryRunner) RunPrompt(
	ctx context.Context,
	sessionID string,
	content message.UserMessageContent,
) (<-chan message.Message, <-chan error) {
	msg := message.StreamingMessage{
		Type: "user",
		Message: message.StreamingMessageContent{
			Role:    "user",
			Content: content,
		},
		SessionID: sessionID,
	}
	return r.RunMessages(ctx, sessionID, []message.StreamingMessage{msg})
}

// RunMessageIterator incrementally consumes streaming input and processes each
// user message against shared session state.
func (r *QueryRunner) RunMessageIterator(
	ctx context.Context,
	sessionID string,
	inputs iter.Seq[message.StreamingMessage],
) (<-chan message.Message, <-chan error) {
	out := make(chan message.Message, 64)
	errs := make(chan error, 8)

	go func() {
		defer close(out)
		defer close(errs)

		sid := sessionID
		if sid == "" {
			sid = "default"
		}
		inputCh := make(chan message.StreamingMessage, 16)
		var ingestWG sync.WaitGroup
		ingestWG.Add(1)
		go func() {
			defer ingestWG.Done()
			defer close(inputCh)
			for in := range inputs {
				select {
				case inputCh <- in:
				case <-ctx.Done():
					return
				}
			}
		}()
		defer ingestWG.Wait()

		for {
			var in message.StreamingMessage
			var ok bool
			select {
			case in, ok = <-inputCh:
				if !ok {
					return
				}
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
			runSID := sid
			if in.SessionID != "" {
				runSID = in.SessionID
				sid = runSID
			}

			msgs, runErrs := r.RunMessages(ctx, runSID, []message.StreamingMessage{in})
			msgClosed := false
			errClosed := false

			for !msgClosed || !errClosed {
				select {
				case msg, ok := <-msgs:
					if !ok {
						msgClosed = true
						continue
					}
					select {
					case out <- msg:
					case <-ctx.Done():
						errs <- ctx.Err()
						return
					}
				case err, ok := <-runErrs:
					if !ok {
						errClosed = true
						continue
					}
					if err != nil {
						select {
						case errs <- err:
						case <-ctx.Done():
						}
						return
					}
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				}
			}
		}
	}()

	return out, errs
}

// RunMessages runs a conversation turn from one or more user messages.
func (r *QueryRunner) RunMessages(
	ctx context.Context,
	sessionID string,
	inputs []message.StreamingMessage,
) (<-chan message.Message, <-chan error) {
	out := make(chan message.Message, 64)
	errs := make(chan error, 8)

	go func() {
		defer close(out)
		defer close(errs)

		if r.transport == nil {
			errs <- fmt.Errorf("transport is nil")
			return
		}
		if err := r.transport.Start(ctx); err != nil {
			errs <- err
			return
		}

		if sessionID == "" {
			sessionID = "default"
		}
		s := r.sessions.GetOrCreate(sessionID)
		runStarted := time.Now()

		// Start query span for the entire conversation turn.
		model := pickModel(r.opts)
		ctx, querySpan := r.obs.StartQuerySpan(ctx, upstreamgenai.OperationNameChat, model, sessionID)
		var errType string
		defer func() {
			duration := time.Since(runStarted).Seconds()
			r.obs.RecordOperationDuration(ctx, duration, upstreamgenai.OperationNameChat, model, agenterrclass.Class(errType))
			if errType != "" {
				querySpan.MarkError(agenterrclass.Class(errType))
			}
			// Unset span status implies success — no explicit Ok needed.
			querySpan.End()
		}()

		history := session.Clone(s.Messages)
		if len(history) == 0 {
			if sys := effectiveSystemPrompt(r.opts); sys != "" {
				systemMsg := &message.SystemMessage{
					Type:    "system",
					Subtype: "init",
					Data: map[string]any{
						"prompt": sys,
					},
				}
				attachAuditEnvelope(systemMsg, "system", "init", map[string]any{
					"type":    "system",
					"subtype": "init",
					"data":    systemMsg.Data,
				})
				select {
				case out <- systemMsg:
				case <-ctx.Done():
					errType = "cancelled"
					errs <- ctx.Err()
					return
				}
				history = append(history, map[string]any{
					"role":    "system",
					"content": sys,
				})
			}
		}

		for _, in := range inputs {
			if r.opts != nil && r.opts.OpenRouterAPIMode == config.OpenRouterAPIModeResponses && in.Message.Content.HasNonTextBlocks() {
				errType = "invalid_request"
				errs <- fmt.Errorf("multimodal input requires chat/completions api mode")
				return
			}
			if in.Message.Role == "" {
				in.Message.Role = "user"
			}
			userID := fmt.Sprintf("%s-u%d", sessionID, s.UserTurns+1)
			s.UserTurns++

			um := &message.UserMessage{
				Type:    "user",
				Content: in.Message.Content,
				UUID:    &userID,
			}
			attachAuditEnvelope(um, "user", "", um)
			select {
			case out <- um:
			case <-ctx.Done():
				errType = "cancelled"
				errs <- ctx.Err()
				return
			}

			hookOuts, err := r.runHookWithObs(ctx, hook.EventUserPromptSubmit, "", &hook.UserPromptSubmitInput{
				BaseInput:     baseInput(sessionID, r.opts),
				HookEventName: string(hook.EventUserPromptSubmit),
				Prompt:        in.Message.Content.String(),
			}, nil)
			if err != nil {
				errType = "hook_error"
				errs <- err
				return
			}
			if err := validateHookOutputs(hook.EventUserPromptSubmit, hookOuts); err != nil {
				errType = "hook_error"
				errs <- err
				return
			}

			history = append(history, map[string]any{
				"role":    in.Message.Role,
				"content": historyUserContent(in.Message.Content),
			})

			if r.opts.EnableFileCheckpointing {
				r.sessions.SetState(sessionID, history, s.UserTurns)
				r.sessions.Snapshot(ctx, sessionID, userID)
				if r.opts.Cwd != "" {
					if err := r.sessions.SnapshotFiles(sessionID, userID, r.opts.Cwd); err != nil {
						errType = "checkpoint_error"
						errs <- err
						return
					}
				}
			}
		}

		maxTurns := r.opts.MaxTurns
		if maxTurns <= 0 {
			maxTurns = r.opts.MaxToolIterations
		}
		if maxTurns <= 0 {
			maxTurns = 8
		}
		totalCost := 0.0

		for turn := 0; turn < maxTurns; turn++ {
			models := []string{pickModel(r.opts)}
			if r.opts != nil && r.opts.FallbackModel != "" && r.opts.FallbackModel != models[0] {
				models = append(models, r.opts.FallbackModel)
			}

			reqModel := models[0]
			calls := map[int]*pendingToolCall{}
			assistantTextStr := ""
			assistantImages := []message.ImageBlock{}
			var turnUsage *message.Usage
			var turnCost *float64
			var terminalEvent map[string]any
			var runErr error
			turnTotalCost := 0.0
			turnCostSeen := false

			for i, model := range models {
				reqModel = model
				req := &config.ChatRequest{
					Model:              model,
					Models:             requestModels(r.opts),
					Messages:           history,
					Tools:              r.registry.OpenAITools(),
					Stream:             true,
					ToolChoice:         requestToolChoice(r.opts),
					MaxTokens:          requestMaxTokens(r.opts),
					MaxOutputTokens:    requestMaxOutputTokens(r.opts),
					Temperature:        requestTemperature(r.opts),
					TopP:               requestTopP(r.opts),
					TopK:               requestTopK(r.opts),
					PresencePenalty:    requestPresencePenalty(r.opts),
					FrequencyPenalty:   requestFrequencyPenalty(r.opts),
					Seed:               requestSeed(r.opts),
					Stop:               requestStop(r.opts),
					Logprobs:           requestLogprobs(r.opts),
					TopLogprobs:        requestTopLogprobs(r.opts),
					ParallelToolCalls:  requestParallelToolCalls(r.opts),
					ResponseFormat:     r.opts.OutputFormat,
					ResponseText:       requestResponseText(r.opts),
					Metadata:           requestMetadata(r.opts),
					Provider:           requestProvider(r.opts),
					Plugins:            requestPlugins(r.opts),
					Route:              requestRoute(r.opts),
					Reasoning:          requestReasoning(r.opts),
					SessionID:          requestSessionID(r.opts),
					Trace:              requestTrace(r.opts),
					Modalities:         requestModalities(r.opts),
					ImageConfig:        requestImageConfig(r.opts),
					User:               requestUser(r.opts),
					Instructions:       requestInstructions(r.opts),
					PreviousResponseID: requestPreviousResponseID(r.opts),
					PromptCacheKey:     requestPromptCacheKey(r.opts),
					MaxToolCalls:       requestMaxToolCalls(r.opts),
					ServiceTier:        requestServiceTier(r.opts),
					Truncation:         requestTruncation(r.opts),
					Include:            requestInclude(r.opts),
					Background:         requestBackground(r.opts),
					SafetyIdentifier:   requestSafetyIdentifier(r.opts),
					Store:              requestStore(r.opts),
					Prompt:             requestPrompt(r.opts),
					Extra:              requestExtra(r.opts),
				}
				var emitted bool
				assistantTextStr, assistantImages, calls, _, turnUsage, turnCost, terminalEvent, runErr, emitted = r.runStream(ctx, sessionID, req, out, errs)
				if turnCost != nil {
					turnTotalCost += *turnCost
					turnCostSeen = true
				}
				if runErr == nil {
					break
				}
				// If stream already emitted output, do not retry with fallback to avoid
				// duplicate/partial mixed turn output.
				if emitted {
					errType = "transport_error"
					errs <- runErr
					return
				}
				if i == len(models)-1 {
					errType = "transport_error"
					errs <- runErr
					return
				}
			}

			// Update model to reflect the actual model used (may differ
			// from initial pick after fallback).
			model = reqModel

			// Record token usage from the completed stream.
			if turnUsage != nil {
				r.obs.RecordTokenUsage(ctx, int64(turnUsage.InputTokens),
					upstreamgenai.TokenTypeInput, upstreamgenai.OperationNameChat, reqModel)
				r.obs.RecordTokenUsage(ctx, int64(turnUsage.OutputTokens),
					upstreamgenai.TokenTypeOutput, upstreamgenai.OperationNameChat, reqModel)
				if turnUsage.ReasoningOutputTokens > 0 {
					r.obs.RecordTokenUsage(ctx, int64(turnUsage.ReasoningOutputTokens),
						upstreamgenai.TokenTypeAttr("thinking"), upstreamgenai.OperationNameChat, reqModel)
				}
			}

			if turnCostSeen {
				totalCost += turnTotalCost
			}
			if r.opts != nil && r.opts.MaxBudgetUSD != nil && turnCostSeen && totalCost > *r.opts.MaxBudgetUSD {
				errType = "budget_exceeded"
				msg := fmt.Sprintf("max budget exceeded: spent %.6f > budget %.6f", totalCost, *r.opts.MaxBudgetUSD)
				res := &message.ResultMessage{
					Type:          "result",
					Subtype:       "error_max_budget_usd",
					NumTurns:      turn + 1,
					SessionID:     sessionID,
					IsError:       true,
					DurationMs:    elapsedMs(runStarted),
					DurationAPIMs: elapsedMs(runStarted),
					TotalCostUSD:  ptrFloat(totalCost),
					Usage:         turnUsage,
					Result:        &msg,
					StopReason:    ptrString("max_budget"),
				}
				if terminalEvent != nil {
					attachRawAuditEnvelope(
						res, "result", res.Subtype, terminalEvent,
					)
				} else {
					attachAuditEnvelope(
						res, "result", res.Subtype, res,
					)
				}
				select {
				case out <- res:
				case <-ctx.Done():
					errs <- ctx.Err()
				}
				return
			}

			// Build assistant message for history.
			assistantHistory := map[string]any{"role": "assistant"}
			if assistantTextStr != "" {
				assistantHistory["content"] = assistantTextStr
			} else {
				assistantHistory["content"] = nil
			}
			if len(assistantImages) > 0 {
				assistantHistory["images"] = encodeHistoryImages(assistantImages)
			}
			if !r.opts.IncludePartialMessages {
				assistantBlocks := assistantContentBlocks(assistantTextStr, assistantImages)
				if len(assistantBlocks) > 0 {
					am := &message.AssistantMessage{
						Type:    "assistant",
						Model:   reqModel,
						Content: assistantBlocks,
					}
					attachAuditEnvelope(am, "assistant", "final_text", am)
					select {
					case out <- am:
					case <-ctx.Done():
						errType = "cancelled"
						errs <- ctx.Err()
						return
					}
				}
			}

			orderedCalls := orderCalls(calls)
			if len(orderedCalls) > 0 {
				toolCalls := make([]map[string]any, 0, len(orderedCalls))
				toolResultsHistory := make([]map[string]any, 0, len(orderedCalls))
				for _, c := range orderedCalls {
					argsMap := map[string]any{}
					if strings.TrimSpace(c.Args.String()) != "" {
						_ = json.Unmarshal([]byte(c.Args.String()), &argsMap)
					}

					assistantToolMsg := &message.AssistantMessage{
						Type:  "assistant",
						Model: reqModel,
						Content: []message.ContentBlock{
							&message.ToolUseBlock{
								Type:  message.BlockTypeToolUse,
								ID:    c.ID,
								Name:  c.Name,
								Input: argsMap,
							},
						},
					}
					attachAuditEnvelope(assistantToolMsg, "assistant", "tool_use", assistantToolMsg)
					select {
					case out <- assistantToolMsg:
					case <-ctx.Done():
						errType = "cancelled"
						errs <- ctx.Err()
						return
					}

					toolUseID := c.ID
					hookOuts, err := r.runHookWithObs(ctx, hook.EventPermissionRequest, c.Name, &hook.PermissionRequestInput{
						BaseInput:             baseInput(sessionID, r.opts),
						HookEventName:         string(hook.EventPermissionRequest),
						ToolName:              c.Name,
						ToolInput:             argsMap,
						PermissionSuggestions: nil,
					}, &toolUseID)
					if err != nil {
						errType = "hook_error"
						errs <- err
						return
					}
					if err := validateHookOutputs(hook.EventPermissionRequest, hookOuts); err != nil {
						errType = "hook_error"
						errs <- err
						return
					}
					decision := permissionDecisionFromHookOutputs(hookOuts, argsMap)
					if len(decision.updatedPermissions) > 0 {
						r.executor.ApplyPermissionUpdates(decision.updatedPermissions)
					}
					if decision.updatedInput != nil {
						argsMap = decision.updatedInput
					}
					if decision.deny {
						denyErr := &sdkerrors.ToolPermissionDeniedError{
							ToolName:  c.Name,
							Message:   decision.message,
							Interrupt: decision.interrupt,
						}
						toolCtx, toolSpan := r.obs.StartToolSpan(ctx, c.Name, c.ID)
						toolSpan.RecordError(denyErr)
						toolSpan.SetAttributes(observability.Outcome("denied"))
						toolSpan.End()
						r.obs.RecordToolCall(toolCtx, c.Name, "denied")
						v := denyErr.Interrupt
						postFailureOuts, _ := r.runHookWithObs(ctx, hook.EventPostToolUseFailure, c.Name, &hook.PostToolUseFailureInput{
							BaseInput:     baseInput(sessionID, r.opts),
							HookEventName: string(hook.EventPostToolUseFailure),
							ToolName:      c.Name,
							ToolInput:     argsMap,
							ToolUseID:     c.ID,
							Error:         denyErr.Error(),
							IsInterrupt:   &v,
						}, &toolUseID)
						if err := validateHookOutputs(hook.EventPostToolUseFailure, postFailureOuts); err != nil {
							errType = "hook_error"
							errs <- err
							return
						}
						if denyErr.Interrupt {
							errType = "permission_denied"
							msg := denyErr.Error()
							res := &message.ResultMessage{
								Type:       "result",
								Subtype:    "error",
								NumTurns:   turn + 1,
								SessionID:  sessionID,
								IsError:    true,
								DurationMs: elapsedMs(runStarted),
								Result:     &msg,
								StopReason: ptrString("interrupted"),
							}
							attachAuditEnvelope(res, "result", res.Subtype, res)
							select {
							case out <- res:
							case <-ctx.Done():
								errs <- ctx.Err()
							}
							return
						}
						errType = "permission_denied"
						errs <- denyErr
						return
					}

					preToolOuts, err := r.runHookWithObs(ctx, hook.EventPreToolUse, c.Name, &hook.PreToolUseInput{
						BaseInput:     baseInput(sessionID, r.opts),
						HookEventName: string(hook.EventPreToolUse),
						ToolName:      c.Name,
						ToolInput:     argsMap,
						ToolUseID:     c.ID,
					}, &toolUseID)
					if err != nil {
						errType = "hook_error"
						errs <- err
						return
					}
					if err := validateHookOutputs(hook.EventPreToolUse, preToolOuts); err != nil {
						errType = "hook_error"
						errs <- err
						return
					}

					toolStart := time.Now()
					toolCtx, toolSpan := r.obs.StartToolSpan(ctx, c.Name, c.ID)
					toolOut, err := r.executor.ExecuteWithSuggestions(toolCtx, c.Name, argsMap, decision.suggestions)
					toolDuration := time.Since(toolStart).Seconds()
					r.obs.RecordToolCallDuration(toolCtx, toolDuration, c.Name)
					if err != nil {
						outcome := "error"
						var denyErr *sdkerrors.ToolPermissionDeniedError
						var isInterrupt *bool
						if stderrors.As(err, &denyErr) {
							v := denyErr.Interrupt
							isInterrupt = &v
							outcome = "denied"
						}
						toolSpan.RecordError(err)
						toolSpan.SetAttributes(observability.Outcome(outcome))
						toolSpan.End()
						r.obs.RecordToolCall(toolCtx, c.Name, outcome)
						postFailureOuts, _ := r.runHookWithObs(ctx, hook.EventPostToolUseFailure, c.Name, &hook.PostToolUseFailureInput{
							BaseInput:     baseInput(sessionID, r.opts),
							HookEventName: string(hook.EventPostToolUseFailure),
							ToolName:      c.Name,
							ToolInput:     argsMap,
							ToolUseID:     c.ID,
							Error:         err.Error(),
							IsInterrupt:   isInterrupt,
						}, &toolUseID)
						if hookErr := validateHookOutputs(hook.EventPostToolUseFailure, postFailureOuts); hookErr != nil {
							errType = "hook_error"
							errs <- hookErr
							return
						}
						if denyErr != nil && denyErr.Interrupt {
							errType = "permission_denied"
							msg := denyErr.Error()
							res := &message.ResultMessage{
								Type:       "result",
								Subtype:    "error",
								NumTurns:   turn + 1,
								SessionID:  sessionID,
								IsError:    true,
								DurationMs: elapsedMs(runStarted),
								Result:     &msg,
								StopReason: ptrString("interrupted"),
							}
							attachAuditEnvelope(res, "result", res.Subtype, res)
							select {
							case out <- res:
							case <-ctx.Done():
								errs <- ctx.Err()
							}
							return
						}
						errType = "tool_error"
						errs <- err
						return
					}
					// Unset span status implies success.
					toolSpan.End()
					r.obs.RecordToolCall(toolCtx, c.Name, "ok")

					postToolOuts, err := r.runHookWithObs(ctx, hook.EventPostToolUse, c.Name, &hook.PostToolUseInput{
						BaseInput:     baseInput(sessionID, r.opts),
						HookEventName: string(hook.EventPostToolUse),
						ToolName:      c.Name,
						ToolInput:     argsMap,
						ToolUseID:     c.ID,
						ToolResponse:  toolOut,
					}, &toolUseID)
					if err != nil {
						errType = "hook_error"
						errs <- err
						return
					}
					if err := validateHookOutputs(hook.EventPostToolUse, postToolOuts); err != nil {
						errType = "hook_error"
						errs <- err
						return
					}

					toolResMsg := &message.AssistantMessage{
						Type:  "assistant",
						Model: reqModel,
						Content: []message.ContentBlock{
							&message.ToolResultBlock{
								Type:      message.BlockTypeToolResult,
								ToolUseID: c.ID,
								Content: []message.ContentBlock{
									&message.TextBlock{Type: message.BlockTypeText, Text: toolOut},
								},
							},
						},
					}
					attachAuditEnvelope(toolResMsg, "assistant", "tool_result", toolResMsg)
					select {
					case out <- toolResMsg:
					case <-ctx.Done():
						errs <- ctx.Err()
						return
					}

					toolCalls = append(toolCalls, map[string]any{
						"id":   c.ID,
						"type": "function",
						"function": map[string]any{
							"name":      c.Name,
							"arguments": c.Args.String(),
						},
					})
					toolResultsHistory = append(toolResultsHistory, map[string]any{
						"role":         "tool",
						"tool_call_id": c.ID,
						"content":      toolOut,
					})
				}
				assistantHistory["tool_calls"] = toolCalls
				history = append(history, assistantHistory)
				history = append(history, toolResultsHistory...)

				// Tool calls mean continue conversation with another request.
				continue
			}

			history = append(history, assistantHistory)

			resText := strings.TrimSpace(assistantTextStr)
			var resultText *string
			if resText != "" {
				resultText = ptrString(resText)
			}
			structured := parseStructuredOutput(resText, r.opts)
			result := &message.ResultMessage{
				Type:             "result",
				Subtype:          "success",
				NumTurns:         turn + 1,
				SessionID:        sessionID,
				IsError:          false,
				DurationMs:       elapsedMs(runStarted),
				DurationAPIMs:    elapsedMs(runStarted),
				TotalCostUSD:     totalCostPtr(totalCost, turnCostSeen),
				Usage:            turnUsage,
				Result:           resultText,
				StopReason:       ptrString("end_turn"),
				StructuredOutput: structured,
			}
			if terminalEvent != nil {
				attachRawAuditEnvelope(
					result, "result", result.Subtype, terminalEvent,
				)
			} else {
				attachAuditEnvelope(
					result, "result", result.Subtype, result,
				)
			}
			select {
			case out <- result:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}

			r.sessions.SetState(sessionID, history, s.UserTurns)
			stopOuts, _ := r.runHookWithObs(ctx, hook.EventStop, "", &hook.StopInput{
				BaseInput:     baseInput(sessionID, r.opts),
				HookEventName: string(hook.EventStop),
			}, nil)
			if err := validateHookOutputs(hook.EventStop, stopOuts); err != nil {
				errType = "hook_error"
				errs <- err
				return
			}
			return
		}

		errType = "max_turns"
		errs <- fmt.Errorf("max turns reached without terminal response")
	}()

	return out, errs
}

func ptrString(s string) *string  { return &s }
func ptrFloat(v float64) *float64 { return &v }
func totalCostPtr(total float64, seen bool) *float64 {
	if !seen {
		return nil
	}
	return ptrFloat(total)
}

func elapsedMs(start time.Time) int {
	ms := int(time.Since(start).Milliseconds())
	if ms <= 0 {
		return 1
	}
	return ms
}

func pickModel(opts *config.Options) string {
	if opts == nil {
		return "openai/gpt-4o-mini"
	}
	if opts.Model != "" {
		return opts.Model
	}
	return "openai/gpt-4o-mini"
}

func baseInput(sessionID string, opts *config.Options) hook.BaseInput {
	var mode *string
	if opts != nil && opts.PermissionMode != "" {
		m := opts.PermissionMode
		mode = &m
	}
	cwd := ""
	if opts != nil {
		cwd = opts.Cwd
	}
	return hook.BaseInput{SessionID: sessionID, Cwd: cwd, PermissionMode: mode}
}

func requestMetadata(opts *config.Options) map[string]any {
	if opts == nil {
		return nil
	}
	meta := cloneMap(opts.OpenRouterMetadata)
	if u := strings.TrimSpace(opts.User); u != "" {
		if meta == nil {
			meta = map[string]any{}
		}
		if _, exists := meta["user"]; !exists {
			meta["user"] = u
		}
	}
	return meta
}

func requestUser(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	return strings.TrimSpace(opts.User)
}

func requestModels(opts *config.Options) []string {
	if opts == nil || len(opts.OpenRouterModels) == 0 {
		return nil
	}
	return append([]string(nil), opts.OpenRouterModels...)
}

func requestToolChoice(opts *config.Options) any {
	if opts == nil {
		return nil
	}
	if opts.OpenRouterToolChoice != nil {
		return opts.OpenRouterToolChoice
	}
	return nil
}

func requestMaxTokens(opts *config.Options) *int {
	if opts == nil {
		return nil
	}
	if opts.OpenRouterMaxTokens != nil {
		v := *opts.OpenRouterMaxTokens
		return &v
	}
	if opts.OpenRouterMaxOutputTokens != nil {
		v := *opts.OpenRouterMaxOutputTokens
		return &v
	}
	return nil
}

func requestMaxOutputTokens(opts *config.Options) *int {
	if opts == nil || opts.OpenRouterMaxOutputTokens == nil {
		return nil
	}
	v := *opts.OpenRouterMaxOutputTokens
	return &v
}

func requestTemperature(opts *config.Options) *float64 {
	if opts == nil || opts.OpenRouterTemperature == nil {
		return nil
	}
	v := *opts.OpenRouterTemperature
	return &v
}

func requestTopP(opts *config.Options) *float64 {
	if opts == nil || opts.OpenRouterTopP == nil {
		return nil
	}
	v := *opts.OpenRouterTopP
	return &v
}

func requestTopK(opts *config.Options) *float64 {
	if opts == nil || opts.OpenRouterTopK == nil {
		return nil
	}
	v := *opts.OpenRouterTopK
	return &v
}

func requestPresencePenalty(opts *config.Options) *float64 {
	if opts == nil || opts.OpenRouterPresencePenalty == nil {
		return nil
	}
	v := *opts.OpenRouterPresencePenalty
	return &v
}

func requestFrequencyPenalty(opts *config.Options) *float64 {
	if opts == nil || opts.OpenRouterFrequencyPenalty == nil {
		return nil
	}
	v := *opts.OpenRouterFrequencyPenalty
	return &v
}

func requestSeed(opts *config.Options) *int64 {
	if opts == nil || opts.OpenRouterSeed == nil {
		return nil
	}
	v := *opts.OpenRouterSeed
	return &v
}

func requestStop(opts *config.Options) []string {
	if opts == nil || len(opts.OpenRouterStop) == 0 {
		return nil
	}
	return append([]string(nil), opts.OpenRouterStop...)
}

func requestLogprobs(opts *config.Options) *bool {
	if opts == nil || opts.OpenRouterLogprobs == nil {
		return nil
	}
	v := *opts.OpenRouterLogprobs
	return &v
}

func requestTopLogprobs(opts *config.Options) *int {
	if opts == nil || opts.OpenRouterTopLogprobs == nil {
		return nil
	}
	v := *opts.OpenRouterTopLogprobs
	return &v
}

func requestParallelToolCalls(opts *config.Options) *bool {
	if opts == nil || opts.OpenRouterParallelToolCalls == nil {
		return nil
	}
	v := *opts.OpenRouterParallelToolCalls
	return &v
}

func requestProvider(opts *config.Options) map[string]any {
	if opts == nil {
		return nil
	}
	return cloneMap(opts.OpenRouterProvider)
}

func requestPlugins(opts *config.Options) []map[string]any {
	if opts == nil {
		return nil
	}
	if len(opts.OpenRouterPlugins) > 0 {
		return cloneMapSlice(opts.OpenRouterPlugins)
	}
	if len(opts.Plugins) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(opts.Plugins))
	for _, p := range opts.Plugins {
		if p == nil {
			continue
		}
		out = append(out, map[string]any{
			"type": p.Type,
			"path": p.Path,
		})
	}
	return out
}

func requestRoute(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	return strings.TrimSpace(opts.OpenRouterRoute)
}

func requestReasoning(opts *config.Options) map[string]any {
	if opts == nil {
		return nil
	}
	reasoning := cloneMap(opts.OpenRouterReasoning)
	if reasoning == nil {
		reasoning = map[string]any{}
	}
	if opts.Effort != nil {
		reasoning["effort"] = string(*opts.Effort)
	}
	switch t := opts.Thinking.(type) {
	case config.ThinkingConfigDisabled:
		reasoning["enabled"] = false
	case *config.ThinkingConfigDisabled:
		reasoning["enabled"] = false
	case config.ThinkingConfigAdaptive:
		if _, ok := reasoning["effort"]; !ok {
			reasoning["effort"] = "medium"
		}
	case *config.ThinkingConfigAdaptive:
		if _, ok := reasoning["effort"]; !ok {
			reasoning["effort"] = "medium"
		}
	case config.ThinkingConfigEnabled:
		reasoning["enabled"] = true
		if t.BudgetTokens > 0 {
			reasoning["max_tokens"] = t.BudgetTokens
		}
	case *config.ThinkingConfigEnabled:
		reasoning["enabled"] = true
		if t != nil && t.BudgetTokens > 0 {
			reasoning["max_tokens"] = t.BudgetTokens
		}
	}
	if len(reasoning) == 0 {
		return nil
	}
	return reasoning
}

func requestSessionID(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	return strings.TrimSpace(opts.OpenRouterSessionID)
}

func requestTrace(opts *config.Options) *bool {
	if opts == nil || opts.OpenRouterTrace == nil {
		return nil
	}
	v := *opts.OpenRouterTrace
	return &v
}

func requestModalities(opts *config.Options) []string {
	if opts == nil || len(opts.OpenRouterModalities) == 0 {
		return nil
	}
	return append([]string(nil), opts.OpenRouterModalities...)
}

func requestImageConfig(opts *config.Options) map[string]any {
	if opts == nil {
		return nil
	}
	return cloneMap(opts.OpenRouterImageConfig)
}

func requestInstructions(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	return strings.TrimSpace(opts.OpenRouterInstructions)
}

func requestPreviousResponseID(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	return strings.TrimSpace(opts.OpenRouterPreviousResponseID)
}

func requestPromptCacheKey(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	return strings.TrimSpace(opts.OpenRouterPromptCacheKey)
}

func requestPrompt(opts *config.Options) map[string]any {
	if opts == nil {
		return nil
	}
	return cloneMap(opts.OpenRouterPrompt)
}

func requestResponseText(opts *config.Options) map[string]any {
	if opts == nil {
		return nil
	}
	return cloneMap(opts.OpenRouterText)
}

func requestMaxToolCalls(opts *config.Options) *int {
	if opts == nil || opts.OpenRouterMaxToolCalls == nil {
		return nil
	}
	v := *opts.OpenRouterMaxToolCalls
	return &v
}

func requestServiceTier(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	return strings.TrimSpace(opts.OpenRouterServiceTier)
}

func requestTruncation(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	return strings.TrimSpace(opts.OpenRouterTruncation)
}

func requestInclude(opts *config.Options) []string {
	if opts == nil || len(opts.OpenRouterInclude) == 0 {
		return nil
	}
	return append([]string(nil), opts.OpenRouterInclude...)
}

func requestBackground(opts *config.Options) *bool {
	if opts == nil || opts.OpenRouterBackground == nil {
		return nil
	}
	v := *opts.OpenRouterBackground
	return &v
}

func requestSafetyIdentifier(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	return strings.TrimSpace(opts.OpenRouterSafetyIdentifier)
}

func requestStore(opts *config.Options) *bool {
	if opts == nil || opts.OpenRouterStore == nil {
		return nil
	}
	v := *opts.OpenRouterStore
	return &v
}

func requestExtra(opts *config.Options) map[string]any {
	if opts == nil {
		return nil
	}
	return cloneMap(opts.OpenRouterExtra)
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneMapSlice(in []map[string]any) []map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(in))
	for _, m := range in {
		out = append(out, cloneMap(m))
	}
	return out
}

func orderCalls(calls map[int]*pendingToolCall) []*pendingToolCall {
	if len(calls) == 0 {
		return nil
	}
	max := -1
	for i := range calls {
		if i > max {
			max = i
		}
	}
	out := make([]*pendingToolCall, 0, len(calls))
	for i := 0; i <= max; i++ {
		if c, ok := calls[i]; ok {
			out = append(out, c)
		}
	}
	return out
}

func effectiveSystemPrompt(opts *config.Options) string {
	if opts == nil {
		return ""
	}
	if opts.SystemPromptPreset != nil {
		if opts.SystemPromptPreset.Append != nil {
			return strings.TrimSpace(*opts.SystemPromptPreset.Append)
		}
		return ""
	}
	return strings.TrimSpace(opts.SystemPrompt)
}

func parseStructuredOutput(text string, opts *config.Options) any {
	if opts == nil || len(opts.OutputFormat) == 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil
	}
	return parsed
}

func historyUserContent(content message.UserMessageContent) any {
	if content.IsString() {
		return content.String()
	}
	return content.Blocks()
}

func normalizeAssistantContent(current, incoming string) string {
	if incoming == "" {
		return ""
	}
	if current == "" {
		return incoming
	}
	if strings.HasPrefix(incoming, current) {
		return incoming[len(current):]
	}
	return incoming
}

func assistantContentBlocks(text string, images []message.ImageBlock) []message.ContentBlock {
	blocks := make([]message.ContentBlock, 0, 1+len(images))
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, &message.TextBlock{
			Type: message.BlockTypeText,
			Text: text,
		})
	}
	for _, image := range images {
		img := image
		blocks = append(blocks, &img)
	}
	return blocks
}

func encodeHistoryImages(images []message.ImageBlock) []map[string]any {
	if len(images) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(images))
	for _, image := range images {
		out = append(out, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": image.URL,
			},
			"media_type": image.MediaType,
		})
	}
	return out
}

func appendNewAssistantImages(existing []message.ImageBlock, incoming []imageDelta) []message.ImageBlock {
	if len(incoming) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(existing))
	for _, image := range existing {
		seen[image.URL] = struct{}{}
	}
	out := make([]message.ImageBlock, 0, len(incoming))
	for _, image := range incoming {
		if image.URL == "" {
			continue
		}
		if _, exists := seen[image.URL]; exists {
			continue
		}
		seen[image.URL] = struct{}{}
		out = append(out, message.ImageBlock{
			Type:      message.BlockTypeImage,
			URL:       image.URL,
			MediaType: image.MediaType,
		})
	}
	return out
}

func (r *QueryRunner) runStream(
	ctx context.Context,
	sessionID string,
	req *config.ChatRequest,
	out chan<- message.Message,
	errs chan<- error,
) (string, []message.ImageBlock, map[int]*pendingToolCall, string, *message.Usage, *float64, map[string]any, error, bool) {
	stream, streamErrs := r.transport.CreateStream(ctx, req)

	streamStart := time.Now()
	ttftRecorded := false
	var assistantText strings.Builder
	assistantImages := []message.ImageBlock{}
	calls := map[int]*pendingToolCall{}
	finishReason := ""
	var usage *message.Usage
	var totalCost *float64
	var terminalEvent map[string]any
	emitted := false

	processEvent := func(ev map[string]any) (bool, error) {
		se := &message.StreamEvent{UUID: "", SessionID: sessionID, Event: ev}
		attachRawAuditEnvelope(se, "stream_event", "", ev)
		select {
		case out <- se:
			emitted = true
		case <-ctx.Done():
			return false, ctx.Err()
		}
		usage, totalCost = parseUsageAndCost(ev, usage, totalCost)

		chunks, err := parseChunk(ev)
		if err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
				return false, ctx.Err()
			}
			return true, nil
		}
		for _, ch := range chunks {
			if !ttftRecorded && (ch.Content != "" || len(ch.Images) > 0 || len(ch.ToolDeltas) > 0) {
				ttftRecorded = true
				r.obs.RecordTTFT(ctx, time.Since(streamStart).Seconds(), req.Model)
			}
			if ch.Content != "" {
				content := normalizeAssistantContent(assistantText.String(), ch.Content)
				if content != "" {
					assistantText.WriteString(content)
				}
				if r.opts.IncludePartialMessages && content != "" {
					am := &message.AssistantMessage{
						Type:  "assistant",
						Model: req.Model,
						Content: []message.ContentBlock{
							&message.TextBlock{Type: message.BlockTypeText, Text: content},
						},
					}
					attachAuditEnvelope(am, "assistant", "partial_text", am)
					select {
					case out <- am:
						emitted = true
					case <-ctx.Done():
						return false, ctx.Err()
					}
				}
			}
			if len(ch.Images) > 0 {
				newImages := appendNewAssistantImages(assistantImages, ch.Images)
				if len(newImages) > 0 {
					assistantImages = append(assistantImages, newImages...)
					if r.opts.IncludePartialMessages {
						blocks := make([]message.ContentBlock, 0, len(newImages))
						for _, image := range newImages {
							img := image
							blocks = append(blocks, &img)
						}
						am := &message.AssistantMessage{
							Type:    "assistant",
							Model:   req.Model,
							Content: blocks,
						}
						attachAuditEnvelope(am, "assistant", "partial_image", am)
						select {
						case out <- am:
							emitted = true
						case <-ctx.Done():
							return false, ctx.Err()
						}
					}
				}
			}
			for _, td := range ch.ToolDeltas {
				pc := calls[td.Index]
				if pc == nil {
					pc = &pendingToolCall{}
					calls[td.Index] = pc
				}
				if td.ID != "" {
					pc.ID = td.ID
				}
				if td.Name != "" {
					pc.Name = td.Name
				}
				if td.Args != "" {
					pc.Args.WriteString(td.Args)
				}
			}
			if ch.Finish != "" {
				finishReason = ch.Finish
				terminalEvent = ev
			}
		}
		return true, nil
	}

	streamCh := stream
	errCh := streamErrs
	for streamCh != nil || errCh != nil {
		// Prefer already-available stream chunks before handling stream errors
		// so fallback decisions correctly account for emitted output.
		if streamCh != nil {
			select {
			case ev, ok := <-streamCh:
				if !ok {
					streamCh = nil
					continue
				}
				handled, err := processEvent(ev)
				if err != nil {
					return "", nil, nil, "", usage, totalCost, terminalEvent, err, emitted
				}
				if handled {
					continue
				}
			default:
			}
		}

		select {
		case ev, ok := <-streamCh:
			if !ok {
				streamCh = nil
				continue
			}
			if _, err := processEvent(ev); err != nil {
				return "", nil, nil, "", usage, totalCost, terminalEvent, err, emitted
			}
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				return "", nil, nil, "", usage, totalCost, terminalEvent, err, emitted
			}
		case <-ctx.Done():
			return "", nil, nil, "", usage, totalCost, terminalEvent, ctx.Err(), emitted
		}
	}

	return strings.TrimSpace(assistantText.String()), assistantImages, calls, finishReason, usage, totalCost, terminalEvent, nil, emitted
}

type permissionDecision struct {
	deny               bool
	message            string
	interrupt          bool
	updatedInput       map[string]any
	suggestions        []*permission.Update
	updatedPermissions []*permission.Update
}

func permissionDecisionFromHookOutputs(outputs []hook.JSONOutput, fallbackInput map[string]any) permissionDecision {
	out := permissionDecision{updatedInput: fallbackInput}
	for _, o := range outputs {
		syncOut, ok := o.(*hook.SyncJSONOutput)
		if !ok || syncOut == nil {
			continue
		}
		spec, ok := syncOut.HookSpecificOutput.(*hook.PermissionRequestSpecificOutput)
		if !ok || spec == nil || spec.Decision == nil {
			continue
		}
		d := spec.Decision
		if behavior, ok := d["behavior"].(string); ok && behavior == "deny" {
			out.deny = true
		}
		if msg, ok := d["message"].(string); ok && msg != "" {
			out.message = msg
		}
		if intr, ok := d["interrupt"].(bool); ok {
			out.interrupt = intr
		}
		if in, ok := d["updatedInput"].(map[string]any); ok {
			out.updatedInput = in
		}
		if ups, ok := d["updatedPermissions"]; ok {
			out.updatedPermissions = parsePermissionUpdates(ups)
		}
		if sgs, ok := d["suggestions"]; ok {
			out.suggestions = parsePermissionUpdates(sgs)
		}
	}
	return out
}

func parsePermissionUpdates(v any) []*permission.Update {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	updates := make([]*permission.Update, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		up := &permission.Update{}
		if updateType, ok := m["type"].(string); ok {
			up.Type = permission.UpdateType(updateType)
		}
		if mode, ok := m["mode"].(string); ok {
			mv := permission.Mode(mode)
			up.Mode = &mv
		}
		if behavior, ok := m["behavior"].(string); ok {
			bv := permission.Behavior(behavior)
			up.Behavior = &bv
		}
		if dest, ok := m["destination"].(string); ok {
			dv := permission.UpdateDestination(dest)
			up.Destination = &dv
		}
		if dirs, ok := m["directories"].([]any); ok {
			up.Directories = make([]string, 0, len(dirs))
			for _, d := range dirs {
				if ds, ok := d.(string); ok {
					up.Directories = append(up.Directories, ds)
				}
			}
		}
		if rules, ok := m["rules"].([]any); ok {
			up.Rules = make([]*permission.RuleValue, 0, len(rules))
			for _, rr := range rules {
				rm, ok := rr.(map[string]any)
				if !ok {
					continue
				}
				rule := &permission.RuleValue{}
				if tn, ok := rm["toolName"].(string); ok {
					rule.ToolName = tn
				}
				if rc, ok := rm["ruleContent"].(string); ok {
					rcv := rc
					rule.RuleContent = &rcv
				}
				up.Rules = append(up.Rules, rule)
			}
		}
		updates = append(updates, up)
	}
	return updates
}

func parseUsageAndCost(
	raw map[string]any,
	prevUsage *message.Usage,
	prevCost *float64,
) (*message.Usage, *float64) {
	usage := prevUsage
	cost := prevCost

	parseUsageMap := func(m map[string]any) {
		in := numberFromAny(m["input_tokens"])
		out := numberFromAny(m["output_tokens"])
		if in == 0 && out == 0 {
			in = numberFromAny(m["prompt_tokens"])
			out = numberFromAny(m["completion_tokens"])
		}

		cachedIn := numberFromAny(m["cached_input_tokens"])
		reasoningOut := numberFromAny(m["reasoning_output_tokens"])

		// OpenAI-format nested details.
		if ptd, ok := m["prompt_tokens_details"].(map[string]any); ok {
			if v := numberFromAny(ptd["cached_tokens"]); v != 0 && cachedIn == 0 {
				cachedIn = v
			}
		}
		if ctd, ok := m["completion_tokens_details"].(map[string]any); ok {
			if v := numberFromAny(ctd["reasoning_tokens"]); v != 0 && reasoningOut == 0 {
				reasoningOut = v
			}
		}

		if in != 0 || out != 0 {
			usage = &message.Usage{
				InputTokens:           in,
				OutputTokens:          out,
				CachedInputTokens:     cachedIn,
				ReasoningOutputTokens: reasoningOut,
			}
		}
		if v, ok := floatFromAny(m["total_cost_usd"]); ok {
			cost = &v
		}
	}

	if u, ok := raw["usage"].(map[string]any); ok {
		parseUsageMap(u)
	}
	if v, ok := floatFromAny(raw["total_cost_usd"]); ok {
		cost = &v
	}
	if response, ok := raw["response"].(map[string]any); ok {
		if u, ok := response["usage"].(map[string]any); ok {
			parseUsageMap(u)
		}
		if v, ok := floatFromAny(response["total_cost_usd"]); ok {
			cost = &v
		}
	}

	return usage, cost
}

func numberFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func floatFromAny(v any) (float64, bool) {
	switch n := v.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
