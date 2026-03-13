package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

type chunkDelta struct {
	Content    string
	Images     []imageDelta
	ToolDeltas []toolDelta
	Finish     string
}

type imageDelta struct {
	URL       string
	MediaType string
}

type toolDelta struct {
	Index int
	ID    string
	Name  string
	Args  string
}

func parseChunk(raw map[string]any) ([]chunkDelta, error) {
	// Check for error events (e.g. upstream 429 sent inside the SSE stream).
	if errObj, ok := raw["error"].(map[string]any); ok {
		if msg, ok := errObj["message"].(string); ok && msg != "" {
			code, _ := errObj["code"].(float64)
			return nil, fmt.Errorf("openrouter stream error (code %.0f): %s", code, msg)
		}

		return nil, fmt.Errorf("openrouter stream error: %v", errObj)
	}

	choicesAny, ok := raw["choices"]
	if !ok {
		return parseResponsesChunk(raw)
	}
	choices, ok := choicesAny.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid choices type")
	}
	out := make([]chunkDelta, 0, len(choices))
	for _, c := range choices {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		d := chunkDelta{}
		if fr, ok := cm["finish_reason"].(string); ok {
			d.Finish = fr
		}
		delta, _ := cm["delta"].(map[string]any)
		if delta != nil {
			if content, ok := delta["content"].(string); ok {
				d.Content = content
			}
			d.Images = append(d.Images, parseImageDeltas(delta)...)
			if tcs, ok := delta["tool_calls"].([]any); ok {
				for _, t := range tcs {
					tm, ok := t.(map[string]any)
					if !ok {
						continue
					}
					td := toolDelta{}
					if idx, ok := tm["index"].(float64); ok {
						td.Index = int(idx)
					}
					if id, ok := tm["id"].(string); ok {
						td.ID = id
					}
					if fn, ok := tm["function"].(map[string]any); ok {
						if name, ok := fn["name"].(string); ok {
							td.Name = name
						}
						if args, ok := fn["arguments"].(string); ok {
							td.Args = args
						}
					}
					d.ToolDeltas = append(d.ToolDeltas, td)
				}
			}
		}
		out = append(out, d)
	}
	return out, nil
}

func parseResponsesChunk(raw map[string]any) ([]chunkDelta, error) {
	typ, _ := raw["type"].(string)
	switch typ {
	case "":
		return nil, nil
	case "error", "response.error":
		return nil, parseResponsesError("stream error", raw)
	case "response.created", "response.in_progress":
		return nil, nil
	case "response.incomplete":
		return []chunkDelta{{Finish: "length"}}, nil
	case "response.output_text.delta":
		if delta, ok := raw["delta"].(string); ok {
			return []chunkDelta{{Content: delta}}, nil
		}
		return nil, nil
	case "response.output_text.done":
		if text, ok := raw["text"].(string); ok && text != "" {
			return []chunkDelta{{Content: text}}, nil
		}
		return nil, nil
	case "response.refusal.delta":
		if delta, ok := raw["delta"].(string); ok {
			return []chunkDelta{{Content: delta}}, nil
		}
		return nil, nil
	case "response.refusal.done":
		if refusal, ok := raw["refusal"].(string); ok && refusal != "" {
			return []chunkDelta{{Content: refusal}}, nil
		}
		return nil, nil
	case "response.output_text.annotation.added":
		return nil, nil
	case "response.content_part.added", "response.content_part.done":
		part, _ := raw["part"].(map[string]any)
		if part == nil {
			return nil, nil
		}
		if images := parseImageDeltas(part); len(images) > 0 {
			return []chunkDelta{{Images: images}}, nil
		}
		pType, _ := part["type"].(string)
		switch pType {
		case "output_text":
			if text, ok := part["text"].(string); ok && text != "" {
				return []chunkDelta{{Content: text}}, nil
			}
		case "refusal":
			if text, ok := part["refusal"].(string); ok && text != "" {
				return []chunkDelta{{Content: text}}, nil
			}
		}
		return nil, nil
	case "response.function_call_arguments.delta":
		td := toolDelta{Index: intFromAny(raw["output_index"])}
		if id, ok := raw["call_id"].(string); ok {
			td.ID = id
		}
		if name, ok := raw["name"].(string); ok {
			td.Name = name
		}
		if args, ok := raw["delta"].(string); ok {
			td.Args = args
		}
		return []chunkDelta{{ToolDeltas: []toolDelta{td}}}, nil
	case "response.function_call_arguments.done":
		td := toolDelta{Index: intFromAny(raw["output_index"])}
		if id, ok := raw["call_id"].(string); ok {
			td.ID = id
		}
		if name, ok := raw["name"].(string); ok {
			td.Name = name
		}
		td.Args = argsToString(raw["arguments"])
		return []chunkDelta{{ToolDeltas: []toolDelta{td}}}, nil
	case "response.output_item.added", "response.output_item.done":
		item, _ := raw["item"].(map[string]any)
		if item == nil {
			return nil, nil
		}
		if images := parseImageDeltas(item); len(images) > 0 {
			return []chunkDelta{{Images: images}}, nil
		}
		if td, ok := parseOutputItemToolDelta(raw, item); ok {
			return []chunkDelta{{ToolDeltas: []toolDelta{td}}}, nil
		}
		if text := parseOutputItemText(item); text != "" {
			return []chunkDelta{{Content: text}}, nil
		}
		return nil, nil
	case "response.reasoning_text.delta",
		"response.reasoning_text.done",
		"response.reasoning_summary_part.added",
		"response.reasoning_summary_part.done",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done":
		return nil, nil
	case "response.image_generation_call.in_progress",
		"response.image_generation_call.generating":
		return nil, nil
	case "response.image_generation_call.partial_image",
		"response.image_generation_call.completed":
		if images := parseImageDeltas(raw); len(images) > 0 {
			return []chunkDelta{{Images: images}}, nil
		}
		return nil, nil
	case "response.completed":
		return []chunkDelta{{Finish: "stop"}}, nil
	case "response.failed":
		return nil, parseResponsesError("responses api failed", raw)
	default:
		return nil, nil
	}
}

func parseResponsesError(prefix string, raw map[string]any) error {
	if e, ok := raw["error"].(map[string]any); ok {
		if msg, ok := e["message"].(string); ok && msg != "" {
			return fmt.Errorf("%s: %s", prefix, msg)
		}
	}
	return fmt.Errorf("%s", prefix)
}

func parseOutputItemToolDelta(raw map[string]any, item map[string]any) (toolDelta, bool) {
	itype, _ := item["type"].(string)
	if itype != "function_call" {
		return toolDelta{}, false
	}
	td := toolDelta{Index: intFromAny(raw["output_index"])}
	if callID, ok := item["call_id"].(string); ok && callID != "" {
		td.ID = callID
	} else if id, ok := item["id"].(string); ok {
		td.ID = id
	}
	if name, ok := item["name"].(string); ok {
		td.Name = name
	}
	td.Args = argsToString(item["arguments"])
	return td, true
}

func parseOutputItemText(item map[string]any) string {
	itype, _ := item["type"].(string)
	if itype != "message" {
		return ""
	}
	parts, _ := item["content"].([]any)
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		pt, _ := pm["type"].(string)
		switch pt {
		case "output_text":
			if txt, ok := pm["text"].(string); ok {
				b.WriteString(txt)
			}
		case "refusal":
			if txt, ok := pm["refusal"].(string); ok {
				b.WriteString(txt)
			}
		}
	}
	return b.String()
}

func parseImageDeltas(v any) []imageDelta {
	seen := map[string]struct{}{}
	out := collectImageDeltas(v, seen)
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectImageDeltas(v any, seen map[string]struct{}) []imageDelta {
	switch x := v.(type) {
	case []any:
		out := make([]imageDelta, 0, len(x))
		for _, item := range x {
			out = append(out, collectImageDeltas(item, seen)...)
		}
		return out
	case map[string]any:
		out := make([]imageDelta, 0, 2)
		if img, ok := parseSingleImageDelta(x); ok {
			if _, exists := seen[img.URL]; !exists {
				seen[img.URL] = struct{}{}
				out = append(out, img)
			}
		}
		for _, value := range x {
			out = append(out, collectImageDeltas(value, seen)...)
		}
		return out
	default:
		return nil
	}
}

func parseSingleImageDelta(m map[string]any) (imageDelta, bool) {
	if nested, ok := m["image_url"].(map[string]any); ok {
		if img, ok := imageDeltaFromURLMap(nested, stringValue(m["media_type"])); ok {
			return img, true
		}
	}
	if images, ok := m["images"].([]any); ok {
		for _, raw := range images {
			if nested, ok := raw.(map[string]any); ok {
				if img, ok := parseSingleImageDelta(nested); ok {
					return img, true
				}
			}
		}
	}
	if b64, ok := m["b64_json"].(string); ok && strings.TrimSpace(b64) != "" {
		mediaType := stringValue(m["media_type"])
		if mediaType == "" {
			mediaType = "image/png"
		}
		return imageDelta{
			URL:       "data:" + mediaType + ";base64," + b64,
			MediaType: mediaType,
		}, true
	}
	if url, ok := m["url"].(string); ok && looksImageURL(url) {
		return imageDelta{
			URL:       strings.TrimSpace(url),
			MediaType: mediaTypeFromURL(strings.TrimSpace(url), stringValue(m["media_type"])),
		}, true
	}
	return imageDelta{}, false
}

func imageDeltaFromURLMap(m map[string]any, fallbackMediaType string) (imageDelta, bool) {
	url, ok := m["url"].(string)
	if !ok || !looksImageURL(url) {
		return imageDelta{}, false
	}
	return imageDelta{
		URL:       strings.TrimSpace(url),
		MediaType: mediaTypeFromURL(strings.TrimSpace(url), fallbackMediaType),
	}, true
}

func looksImageURL(v string) bool {
	v = strings.TrimSpace(v)
	return strings.HasPrefix(v, "data:image/") ||
		strings.HasPrefix(v, "https://") ||
		strings.HasPrefix(v, "http://")
}

func mediaTypeFromURL(url string, fallback string) string {
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	if !strings.HasPrefix(url, "data:") {
		return ""
	}
	meta, _, ok := strings.Cut(url, ",")
	if !ok {
		return ""
	}
	meta = strings.TrimPrefix(meta, "data:")
	meta = strings.TrimSuffix(meta, ";base64")
	return strings.TrimSpace(meta)
}

func stringValue(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func argsToString(v any) string {
	switch a := v.(type) {
	case nil:
		return ""
	case string:
		return a
	default:
		b, err := json.Marshal(a)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
