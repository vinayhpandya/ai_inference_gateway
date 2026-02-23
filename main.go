package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// httpClient is a shared HTTP client with sensible timeouts and connection limits.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   false,
	},
}

// Request types (OpenAI-style)
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionRequest struct {
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream,omitempty"`
}

// Response types (OpenAI-style)
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/v1/chat/completions", chatCompletionsHandler)

	log.Printf("Starting inference gateway on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func chatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	// Only accept POST
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get or generate request ID
	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = r.Header.Get("Request-Id")
	}
	if requestID == "" {
		requestID = uuid.New().String()
	}

	// Set request ID in response header
	w.Header().Set("X-Request-ID", requestID)
	w.Header().Set("Content-Type", "application/json")

	// Parse request body
	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Extract the last user message as the prompt
	prompt := extractLastUserMessage(req.Messages)

	// Check if backend is configured
	backendURL := os.Getenv("BACKEND_URL")

	var response ChatCompletionResponse
	var err error

	if backendURL != "" {
		response, err = forwardToBackend(backendURL, req, requestID)
		if err != nil {
			log.Printf("Backend error: %v", err)
			http.Error(w, fmt.Sprintf("Backend error: %v", err), http.StatusBadGateway)
			return
		}
	} else {
		// Echo mode
		response = createEchoResponse(requestID, prompt)
	}

	// Ensure the response ID matches our request ID
	response.ID = requestID

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func extractLastUserMessage(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func createEchoResponse(requestID, prompt string) ChatCompletionResponse {
	replyContent := fmt.Sprintf("Echo: %s", prompt)

	// Approximate token count (roughly 4 chars per token)
	promptTokens := approximateTokens(prompt)
	completionTokens := approximateTokens(replyContent)

	return ChatCompletionResponse{
		ID:     requestID,
		Object: "chat.completion",
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: replyContent,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

func forwardToBackend(backendURL string, req ChatCompletionRequest, requestID string) (ChatCompletionResponse, error) {
	// Ensure we're not requesting streaming from backend
	req.Stream = false

	reqBody, err := json.Marshal(req)
	if err != nil {
		return ChatCompletionResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build the full URL
	url := strings.TrimSuffix(backendURL, "/") + "/v1/chat/completions"

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return ChatCompletionResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Request-ID", requestID)

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return ChatCompletionResponse{}, fmt.Errorf("failed to forward request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ChatCompletionResponse{}, fmt.Errorf("backend returned status %d: %s", resp.StatusCode, string(body))
	}

	var response ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return ChatCompletionResponse{}, fmt.Errorf("failed to decode backend response: %w", err)
	}

	return response, nil
}

func approximateTokens(text string) int {
	// Simple approximation: ~4 characters per token
	if len(text) == 0 {
		return 0
	}
	return (len(text) + 3) / 4
}
