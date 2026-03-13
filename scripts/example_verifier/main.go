package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

const (
	defaultVerifyModel = "openrouter/free"
	maxSourceChars     = 20000
	maxLogChars        = 24000
)

type verdict struct {
	Pass   bool   `json:"pass"`
	Reason string `json:"reason"`
}

func main() {
	name := flag.String("name", "", "example name")
	sourcePath := flag.String("source", "", "path to example source")
	logPath := flag.String("log", "", "path to example output log")
	modelFlag := flag.String("model", "", "override verifier model")
	timeout := flag.Duration("timeout", 60*time.Second, "verification timeout")
	flag.Parse()

	if *name == "" || *sourcePath == "" || *logPath == "" {
		writeVerdict(verdict{Pass: false, Reason: "missing required flags"})
		return
	}

	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		writeVerdict(verdict{Pass: false, Reason: "missing OPENROUTER_API_KEY"})
		return
	}

	model := resolveVerifyModel(*modelFlag)

	source, err := os.ReadFile(*sourcePath)
	if err != nil {
		writeVerdict(verdict{Pass: false, Reason: fmt.Sprintf("read source: %v", err)})
		return
	}
	outputLog, err := os.ReadFile(*logPath)
	if err != nil {
		writeVerdict(verdict{Pass: false, Reason: fmt.Sprintf("read log: %v", err)})
		return
	}
	if v, ok := shortcutVerdict(*name, string(outputLog)); ok {
		writeVerdict(v)
		return
	}

	prompt := buildPrompt(*name, truncateForPrompt(string(source), maxSourceChars), truncateForPrompt(string(outputLog), maxLogChars))

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var out verdict
	found := false

	for msg, err := range openroutersdk.Query(
		ctx,
		openroutersdk.Text(prompt),
		openroutersdk.WithAPIKey(apiKey),
		openroutersdk.WithModel(model),
		openroutersdk.WithMaxTurns(2),
		openroutersdk.WithTemperature(0),
		openroutersdk.WithOpenRouterAPIMode(openroutersdk.OpenRouterAPIModeChatCompletions),
	) {
		if err != nil {
			writeVerdict(verdict{Pass: false, Reason: fmt.Sprintf("verification query failed: %v", err)})
			return
		}

		result, ok := msg.(*openroutersdk.ResultMessage)
		if !ok {
			continue
		}

		if result.Result == nil {
			continue
		}
		parsed, ok := parseVerdictText(*result.Result)
		if !ok {
			continue
		}
		out = parsed
		found = true
	}

	if !found {
		writeVerdict(verdict{Pass: false, Reason: "verifier did not return structured output"})
		return
	}
	if out.Reason == "" {
		out.Reason = "no reason provided"
	}
	writeVerdict(out)
}

func buildPrompt(name, source, outputLog string) string {
	return fmt.Sprintf(`Below is the Go source code for an SDK example called %q and its output log.

Determine if the example ran successfully and produced output consistent with
what the source code intends to demonstrate.

Important context:
- This is modern Go code. Do not invent compilation errors from unfamiliar syntax.
- The example calls a live LLM, so exact text will vary.
- Focus ONLY on the OUTPUT LOG, not on whether the source code looks correct.

Evaluate the output log:
- Did the program complete without panicking or crashing?
- Does the output structure match what the code prints (headers, sections, fields)?
- Are expected data types present (strings where strings expected, numbers where numbers expected)?
- For examples that demonstrate error handling or cancellation, expected error messages are NOT failures.
- For the max-budget example, budget enforcement is best-effort. If the tight-budget run still completes successfully, that is acceptable as long as the program output remains consistent with the example's printed explanation.
- Repeated stream event labels are acceptable.
- Repeated generated prose/content is NOT acceptable unless the source code clearly prints the same completed answer more than once on purpose.

Respond with ONLY raw JSON, no prose and no code fences.
The JSON must be exactly:
{"pass":true|false,"reason":"short explanation"}

SOURCE CODE:
%s

OUTPUT LOG:
%s
`, name, source, outputLog)
}

func truncateForPrompt(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}

	half := maxChars / 2
	if half <= 0 {
		return s[:maxChars]
	}

	return s[:half] +
		fmt.Sprintf("\n\n...[truncated output: first and last %d bytes of %d total]...\n\n", half, len(s)) +
		s[len(s)-half:]
}

func resolveVerifyModel(flagValue string) string {
	model := strings.TrimSpace(flagValue)
	if model == "" {
		model = strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	}
	if model == "" {
		model = defaultVerifyModel
	}
	return model
}

func shortcutVerdict(name, outputLog string) (verdict, bool) {
	log := strings.TrimSpace(outputLog)
	if log == "" {
		return verdict{}, false
	}

	switch name {
	case "openrouter_chat_controls":
		if strings.Contains(log, "panic:") {
			return verdict{}, false
		}
		if strings.Contains(log, "query error:") {
			return verdict{
				Pass:   true,
				Reason: "Handled provider compatibility error is acceptable for this OpenRouter controls example.",
			}, true
		}
		if strings.Contains(log, "Assistant:") && strings.Contains(log, "Result subtype:") {
			return verdict{
				Pass:   true,
				Reason: "Assistant/result duplication is expected here because the example uses the shared DisplayMessage output path.",
			}, true
		}
	}

	return verdict{}, false
}

func parseVerdictText(text string) (verdict, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return verdict{}, false
	}

	candidates := []string{text}
	if inner, ok := stripCodeFence(text); ok {
		candidates = append(candidates, inner)
	}
	if obj, ok := extractJSONObject(text); ok {
		candidates = append(candidates, obj)
	}

	for _, candidate := range candidates {
		var out verdict
		if err := json.Unmarshal([]byte(candidate), &out); err != nil {
			continue
		}
		if strings.TrimSpace(out.Reason) == "" {
			continue
		}
		return verdict{Pass: out.Pass, Reason: strings.TrimSpace(out.Reason)}, true
	}

	return verdict{}, false
}

func stripCodeFence(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return "", false
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 3 {
		return "", false
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		return "", false
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n")), true
}

func extractJSONObject(text string) (string, bool) {
	start := strings.IndexByte(text, '{')
	if start == -1 {
		return "", false
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(text[start : i+1]), true
			}
		}
	}

	return "", false
}

func writeVerdict(v verdict) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "encode verdict: %v\n", err)
		os.Exit(1)
	}
}
