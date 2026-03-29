package openroutersdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	internalmcp "github.com/ethpandaops/openrouter-agent-sdk-go/internal/mcp"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/message"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/userinput"
)

func ensureUserInputTool(cfg *config.Options) {
	if cfg == nil || cfg.OnUserInput == nil {
		return
	}

	toolName := strings.TrimSpace(cfg.PermissionPromptToolName)
	if toolName == "" {
		toolName = "stdio"
	}

	tool := NewTool(toolName, "Request user input from the SDK callback", userInputToolSchema(), func(
		ctx context.Context,
		input map[string]any,
	) (map[string]any, error) {
		req := parseUserInputRequest(input)
		resp, err := cfg.OnUserInput(ctx, req)
		if err != nil {
			return nil, err
		}
		return serializeUserInputResponse(resp), nil
	})

	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]MCPServerConfig, 1)
	}
	if existing, ok := cfg.MCPServers["sdk"].(*MCPSdkServerConfig); ok && existing != nil {
		if server, ok := existing.Instance.(*internalmcp.SDKServer); ok && server != nil {
			schema := mapToJSONSchema(tool.InputSchema())
			server.AddTool(internalmcp.NewTool(tool.Name(), tool.Description(), schema), toolToMCPHandler(tool))
		} else {
			cfg.MCPServers["sdk"] = createSDKToolServer([]Tool{tool})
		}
	} else {
		cfg.MCPServers["sdk"] = createSDKToolServer([]Tool{tool})
	}

	publicName := "mcp__sdk__" + toolName
	if !containsString(cfg.AllowedTools, publicName) {
		cfg.AllowedTools = append(cfg.AllowedTools, publicName)
	}
}

func userInputToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"item_id":   map[string]any{"type": "string"},
			"thread_id": map[string]any{"type": "string"},
			"turn_id":   map[string]any{"type": "string"},
			"questions": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":        map[string]any{"type": "string"},
						"header":    map[string]any{"type": "string"},
						"question":  map[string]any{"type": "string"},
						"is_other":  map[string]any{"type": "boolean"},
						"is_secret": map[string]any{"type": "boolean"},
						"options": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label":       map[string]any{"type": "string"},
									"description": map[string]any{"type": "string"},
								},
								"required": []string{"label"},
							},
						},
					},
					"required": []string{"id", "question"},
				},
			},
		},
		"required": []string{"questions"},
	}
}

func parseUserInputRequest(input map[string]any) *userinput.Request {
	req := &userinput.Request{
		ItemID:   stringValue(input["item_id"]),
		ThreadID: stringValue(input["thread_id"]),
		TurnID:   stringValue(input["turn_id"]),
	}
	if payload, err := message.NewAuditEnvelope("sdk_tool", "user_input_request", input); err == nil {
		req.Audit = payload
	}

	rawQuestions, ok := input["questions"].([]any)
	if !ok {
		if q := parseUserInputQuestion(input, 0); q != nil {
			req.Questions = []userinput.Question{*q}
		}
		return req
	}

	req.Questions = make([]userinput.Question, 0, len(rawQuestions))
	for i, raw := range rawQuestions {
		qMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if q := parseUserInputQuestion(qMap, i); q != nil {
			req.Questions = append(req.Questions, *q)
		}
	}
	return req
}

func parseUserInputQuestion(raw map[string]any, index int) *userinput.Question {
	question := stringValue(raw["question"])
	if question == "" {
		return nil
	}

	id := strings.TrimSpace(stringValue(raw["id"]))
	if id == "" {
		id = fmt.Sprintf("question_%d", index+1)
	}

	out := &userinput.Question{
		ID:       id,
		Header:   stringValue(raw["header"]),
		Question: question,
		IsOther:  boolValue(raw["is_other"]),
		IsSecret: boolValue(raw["is_secret"]),
	}

	rawOptions, ok := raw["options"].([]any)
	if !ok {
		return out
	}
	out.Options = make([]userinput.QuestionOption, 0, len(rawOptions))
	for _, rawOption := range rawOptions {
		optMap, ok := rawOption.(map[string]any)
		if !ok {
			continue
		}
		label := stringValue(optMap["label"])
		if label == "" {
			continue
		}
		out.Options = append(out.Options, userinput.QuestionOption{
			Label:       label,
			Description: stringValue(optMap["description"]),
		})
	}
	return out
}

func serializeUserInputResponse(resp *userinput.Response) map[string]any {
	if resp == nil || len(resp.Answers) == 0 {
		return map[string]any{"answers": map[string]any{}}
	}

	answers := make(map[string]any, len(resp.Answers))
	for key, answer := range resp.Answers {
		if answer == nil {
			answers[key] = []string{}
			continue
		}
		answers[key] = append([]string(nil), answer.Answers...)
	}

	if payload, err := json.Marshal(map[string]any{"answers": answers}); err == nil {
		resp.Audit = &message.AuditEnvelope{
			EventType: "sdk_tool",
			Subtype:   "user_input_response",
			Payload:   payload,
		}
	}

	return map[string]any{"answers": answers}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
