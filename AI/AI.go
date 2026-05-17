package ai

import ( // Import packages needed for OpenRouter API requests and prompt building.
	"bytes"         // Build HTTP request body from JSON bytes.
	"encoding/json" // Marshal request payload and unmarshal response payload.
	"fmt"           // Format prompt text and wrapped errors.
	"io"            // Read HTTP response body bytes.
	"log"           // Optional Gemini payload debug logging.
	"net/http"      // Call Gemini REST API over HTTPS.
	"os"            // Read Gemini configuration from environment.
	"strings"       // Normalize prompt strings and validate values.
	"time"          // Configure HTTP client timeout.

	"whatsapp-bot/common"
	"whatsapp-bot/db"
) // End import block.

const defaultOpenRouterModel = "google/gemini-2.0-flash-001" // Define default OpenRouter model when env is not set.
const defaultOpenRouterURL = "https://openrouter.ai/api/v1/chat/completions"

const defaultSystemPrompt = `You are a helpful WhatsApp assistant.
Reply clearly and briefly.
If memory contains useful context, use it.
If user asks something unsafe or illegal, refuse politely.` // Define default system prompt used for every AI reply.

// Keep log import compile-safe when debug log line is temporarily commented.
var _ = log.Printf

// IMPORTANT: Set your own prompt in the AI_SYSTEM_PROMPT env variable.
// Example in .env:
// AI_SYSTEM_PROMPT=You are my research assistant. Answer in Bahasa Indonesia.

type openRouterGenerateRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterGenerateResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func GenerateAIResponse(incomingText string, memory []common.Message, surveyContext string, phaseContext string) (string, error) { // Generate response text using OpenRouter with DB memory context.
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) // Read OpenRouter API key from environment.
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) // Backward-compatible fallback.
	}
	if apiKey == "" { // Validate API key configuration.
		return "", fmt.Errorf("OPENROUTER_API_KEY is required") // Return explicit config error when key is missing.
	}

	model := strings.TrimSpace(db.GetProjectSettingString("OPENROUTER_MODEL", "")) // Prefer model from project_setting for admin configurability.
	if model == "" {
		model = strings.TrimSpace(os.Getenv("OPENROUTER_MODEL")) // Backward-compatible env fallback.
	}
	if model == "" { // Fallback when model is not set.
		model = strings.TrimSpace(os.Getenv("GEMINI_MODEL")) // Backward-compatible model fallback.
	}
	if model == "" {
		model = defaultOpenRouterModel // Use default model value.
	}

	systemPrompt := strings.TrimSpace(db.GetProjectSettingString("AI_SYSTEM_PROMPT", "")) // Read custom system prompt from project_setting.
	if systemPrompt == "" {                                                               // Fallback when custom prompt is not configured.
		systemPrompt = defaultSystemPrompt // Use built-in default system prompt.
	}

	ragContext, ragDebug, err := BuildRAGContextWithDebug(incomingText)
	if err != nil {
		ragDebug = "rag_error=" + err.Error()
		ragContext = ""
	}
	userPrompt := buildUserPrompt(incomingText, memory, surveyContext, phaseContext, ragContext) // Build user prompt containing incoming message and memory.
	log.Printf(
		"OpenRouter request payload\nmodel: %s\nsystem_prompt:\n%s\nsurvey_context:\n%s\nphase_context:\n%s\nrag_debug:\n%s\nrag_context:\n%s\nuser_prompt:\n%s",
		model,
		systemPrompt,
		strings.TrimSpace(surveyContext),
		strings.TrimSpace(phaseContext),
		strings.TrimSpace(ragDebug),
		strings.TrimSpace(ragContext),
		userPrompt,
	)

	requestPayload := openRouterGenerateRequest{ // Create request payload object for OpenRouter API.
		Model: model,
		Messages: []openRouterMessage{
			{Role: "system", Content: systemPrompt},
			{
				Role:    "user",     // Mark content role as user input.
				Content: userPrompt, // Put user prompt text in one message.
			},
		},
	}

	requestJSON, err := json.Marshal(requestPayload) // Convert request payload to JSON bytes.
	if err != nil {                                  // Check JSON marshal failure.
		return "", fmt.Errorf("marshal OpenRouter request: %w", err) // Return wrapped marshal error.
	}

	url := strings.TrimSpace(os.Getenv("OPENROUTER_URL"))
	if url == "" {
		url = defaultOpenRouterURL
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}                               // Create HTTP client with reasonable timeout.
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(requestJSON)) // Build HTTP POST request.
	if err != nil {                                                                     // Check request construction errors.
		return "", fmt.Errorf("create OpenRouter request: %w", err) // Return wrapped request creation error.
	}
	request.Header.Set("Content-Type", "application/json") // Declare JSON request body.
	request.Header.Set("Authorization", "Bearer "+apiKey)
	if referer := strings.TrimSpace(os.Getenv("OPENROUTER_SITE_URL")); referer != "" {
		request.Header.Set("HTTP-Referer", referer)
	}
	if title := strings.TrimSpace(os.Getenv("OPENROUTER_APP_NAME")); title != "" {
		request.Header.Set("X-Title", title)
	}

	response, err := httpClient.Do(request) // Send HTTP request to OpenRouter API.
	if err != nil {                         // Check transport/network failures.
		return "", fmt.Errorf("call OpenRouter API: %w", err) // Return wrapped API call error.
	}
	defer response.Body.Close() // Ensure response body stream is closed.

	bodyBytes, err := io.ReadAll(response.Body) // Read complete response body bytes.
	if err != nil {                             // Check body read failure.
		return "", fmt.Errorf("read OpenRouter response: %w", err) // Return wrapped body read error.
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 { // Validate successful HTTP status.
		return "", fmt.Errorf("OpenRouter API error status %d: %s", response.StatusCode, strings.TrimSpace(string(bodyBytes))) // Return API error details.
	}

	var result openRouterGenerateResponse                      // Allocate response struct for unmarshal target.
	if err := json.Unmarshal(bodyBytes, &result); err != nil { // Parse JSON response body.
		return "", fmt.Errorf("unmarshal OpenRouter response: %w", err) // Return wrapped unmarshal error.
	}

	if len(result.Choices) == 0 { // Ensure at least one generated choice exists.
		return "", fmt.Errorf("OpenRouter returned no choices") // Return explicit no-output error.
	}
	reply := strings.TrimSpace(result.Choices[0].Message.Content) // Extract and normalize first choice text.
	if reply == "" {                                              // Validate non-empty AI output.
		return "", fmt.Errorf("OpenRouter returned blank response") // Return explicit blank-output error.
	}

	return reply, nil // Return generated reply text.
} // End GenerateAIResponse function.

func buildUserPrompt(incomingText string, memory []common.Message, surveyContext string, phaseContext string, ragContext string) string { // Build prompt text combining memory and latest user message.
	lines := make([]string, 0, len(memory)+8)                         // Pre-allocate lines slice for prompt content.
	lines = append(lines, "You are replying in a WhatsApp chat.")     // Add high-level task instruction.
	lines = append(lines, "Use the memory records below as context.") // Instruct model to use memory context.
	if strings.TrimSpace(surveyContext) != "" {
		lines = append(lines, "Use the completed survey responses below as participant profile context.")
		lines = append(lines, "COMPLETED SURVEY RESPONSES:")
		lines = append(lines, strings.TrimSpace(surveyContext))
		lines = append(lines, "")
	}
	if strings.TrimSpace(phaseContext) != "" {
		lines = append(lines, "Use the active phase prompts below as additional intervention guidance.")
		lines = append(lines, strings.TrimSpace(phaseContext))
		lines = append(lines, "")
	}
	if strings.TrimSpace(ragContext) != "" {
		lines = append(lines, "Use the external document knowledge below when relevant.")
		lines = append(lines, strings.TrimSpace(ragContext))
		lines = append(lines, "")
	}
	lines = append(lines, "MEMORY (oldest to newest):") // Add memory section label.

	for _, msg := range memory { // Convert each memory record into one compact prompt line.
		line := fmt.Sprintf( // Build readable memory line with direction and participants.
			"- [%s] [%s] from=%s to=%s: %s",  // Template for one memory row including timestamp.
			strings.TrimSpace(msg.Timestamp), // Insert message timestamp for temporal context.
			strings.TrimSpace(msg.Direction), // Insert message direction.
			strings.TrimSpace(msg.Sender),    // Insert sender phone.
			strings.TrimSpace(msg.Receiver),  // Insert receiver phone.
			strings.TrimSpace(msg.Content),   // Insert message content.
		)
		lines = append(lines, line) // Append memory line into prompt.
	}

	lines = append(lines, "")                                            // Add empty line separator before latest message.
	lines = append(lines, "LATEST USER MESSAGE:")                        // Label latest incoming user message section.
	lines = append(lines, strings.TrimSpace(incomingText))               // Insert incoming message text.
	lines = append(lines, "")                                            // Add empty line separator before instruction.
	lines = append(lines, "Write one helpful WhatsApp reply text only.") // Force concise single-reply output.

	return strings.Join(lines, "\n") // Join prompt lines into final multi-line prompt text.
} // End buildUserPrompt function.
