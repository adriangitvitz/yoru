package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Run drives the reasoning loop and returns the final text response. When
// OutputSchema is set, the reply must parse as JSON matching it; validation
// failures retry up to RetryInvalidOutput times before returning an error
// whose message contains "agent_output_invalid" (the interpreter bridge
// surfaces this as a typed Result.Err).
func (a *Agent) Run(userPrompt string) (string, error) {
	messages := []Message{
		{Role: "user", Content: []ContentBlock{{Type: "text", Text: userPrompt}}},
	}

	maxTurns := a.Config.MaxTurns
	if maxTurns == 0 {
		maxTurns = 10
	}

	maxTokens := a.Config.BudgetTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	system := a.Config.System
	if a.Config.OutputSchema != nil {
		system = augmentSystemPromptWithSchema(system, a.Config.OutputSchema)
	}

	// Default 2 retries (3 total calls); negative disables.
	retriesLeft := a.Config.RetryInvalidOutput
	if a.Config.OutputSchema != nil && a.Config.RetryInvalidOutput == 0 {
		retriesLeft = 2
	}

	for turn := 0; turn < maxTurns; turn++ {
		req := CompletionRequest{
			Model:     a.Config.Model,
			System:    system,
			Messages:  messages,
			Tools:     a.Config.Tools,
			MaxTokens: maxTokens,
		}

		resp, err := a.Client.Complete(req)
		if err != nil {
			return "", fmt.Errorf("LLM error on turn %d: %w", turn, err)
		}

		messages = append(messages, Message{Role: "assistant", Content: resp.Content})

		if resp.StopReason == "end_turn" || resp.StopReason == "" {
			text := extractText(resp.Content)
			if a.Config.OutputSchema == nil {
				return text, nil
			}
			cleaned, validateErr := validateAgainstSchema(text, a.Config.OutputSchema)
			if validateErr == nil {
				return cleaned, nil
			}
			if retriesLeft <= 0 {
				return "", fmt.Errorf("agent_output_invalid: %w", validateErr)
			}
			retriesLeft--
			messages = append(messages, Message{
				Role: "user",
				Content: []ContentBlock{{Type: "text", Text: "Your previous response did not match the required schema: " +
					validateErr.Error() + ". Respond again with valid JSON matching the schema."}},
			})
			continue
		}

		if resp.StopReason == "tool_use" {
			toolResults := a.processToolCalls(resp.Content)
			messages = append(messages, Message{Role: "user", Content: toolResults})
			continue
		}
	}

	return "", fmt.Errorf("agent exceeded max_turns (%d)", maxTurns)
}

// augmentSystemPromptWithSchema appends a JSON-shape instruction as a
// provider-agnostic fallback for structured output.
func augmentSystemPromptWithSchema(system string, schema *OutputSchema) string {
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return system
	}
	hint := "\n\nIMPORTANT: Respond with ONLY valid JSON matching this schema. " +
		"Do not include prose, explanation, or markdown fencing.\n\nSchema:\n" + string(schemaJSON)
	return strings.TrimSpace(system) + hint
}

var fencedJSON = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// validateAgainstSchema parses text as JSON (stripping markdown fences) and
// checks every Required field is present. Returns the cleaned JSON on success.
func validateAgainstSchema(text string, schema *OutputSchema) (string, error) {
	candidate := strings.TrimSpace(text)
	if m := fencedJSON.FindStringSubmatch(candidate); len(m) == 2 {
		candidate = m[1]
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(candidate), &parsed); err != nil {
		return "", fmt.Errorf("not valid JSON object: %s", err.Error())
	}
	for _, req := range schema.Required {
		if _, ok := parsed[req]; !ok {
			return "", fmt.Errorf("missing required field %q", req)
		}
	}
	return candidate, nil
}

func (a *Agent) processToolCalls(content []ContentBlock) []ContentBlock {
	var results []ContentBlock

	for _, block := range content {
		if block.Type != "tool_use" {
			continue
		}

		var resultText string
		var isError bool

		if a.Registry == nil {
			resultText = "error: no tool registry configured"
			isError = true
		} else {
			result, err := a.Registry.Invoke(block.ToolName, json.RawMessage(block.Input))
			if err != nil {
				resultText = fmt.Sprintf("error: %s", err)
				isError = true
			} else {
				resultText = result
			}
		}

		results = append(results, ContentBlock{
			Type:         "tool_result",
			ToolResultID: block.ToolUseID,
			Content:      resultText,
			IsError:      isError,
		})
	}

	return results
}

func extractText(content []ContentBlock) string {
	var text strings.Builder
	for _, block := range content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}
