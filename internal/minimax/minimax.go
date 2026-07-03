package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Client talks to the MiniMax vision chat-completions API.
type Client struct {
	APIKey  string
	BaseURL string // e.g. https://api.minimax.io/v1
	Model   string // e.g. MiniMax-VL-01
	HTTP    *http.Client
}

// New builds a client with a sane HTTP timeout.
func New(apiKey, baseURL, model string) *Client {
	return &Client{
		APIKey:  apiKey,
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type message struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type thinkingOpt struct {
	Type string `json:"type"` // "disabled" turns off <think> reasoning output
}

type request struct {
	Model     string       `json:"model"`
	Messages  []message    `json:"messages"`
	MaxTokens int          `json:"max_tokens,omitempty"`
	Thinking  *thinkingOpt `json:"thinking,omitempty"`
}

// thinkingDisabled is reused for all requests; MiniMax-M3 is a reasoning model
// and would otherwise wrap answers in <think>…</think> and spend the token budget.
var thinkingDisabled = &thinkingOpt{Type: "disabled"}

type response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	// OpenAI-compatible error object.
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
	// Native MiniMax status block (present on some responses).
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
}

// chat posts a chat-completions request to the OpenAI-compatible endpoint and
// returns the assistant message content.
func (c *Client) chat(ctx context.Context, body request) (string, error) {
	if c.APIKey == "" {
		return "", fmt.Errorf("MiniMax API key is not set")
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	var out response
	_ = json.Unmarshal(data, &out) // best-effort; fall back to raw body on error

	if out.Error != nil && out.Error.Message != "" {
		return "", fmt.Errorf("minimax: %s", out.Error.Message)
	}
	if out.BaseResp.StatusCode != 0 {
		return "", fmt.Errorf("minimax error %d: %s", out.BaseResp.StatusCode, out.BaseResp.StatusMsg)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("minimax http %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("minimax returned no choices")
	}
	return stripThink(out.Choices[0].Message.Content), nil
}

var thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>`)

// stripThink removes any leftover <think>…</think> reasoning block and trims.
func stripThink(s string) string {
	return strings.TrimSpace(thinkRe.ReplaceAllString(s, ""))
}

// Suggestion is the AI's proposed worklog entry.
type Suggestion struct {
	Description string `json:"description"`
	Type       string `json:"type"`
}

// Describe sends the image (a data URI) plus context and returns a one-line work
// description and, when types are provided, a chosen Type. systemPrompt guides
// tone/language; recent is a short list of previous entries for continuity.
func (c *Client) Describe(ctx context.Context, imageDataURI, systemPrompt string, recent, types []string) (Suggestion, error) {
	userText := "Here is a screenshot of my desktop for the last work interval. Describe the work in one sentence."
	if len(recent) > 0 {
		userText += "\n\nMy previous few entries (for continuity, do not repeat verbatim):\n- " + strings.Join(recent, "\n- ")
	}
	if len(types) > 0 {
		userText += "\n\nAlso classify the work into exactly one of these Type values: " +
			strings.Join(types, ", ") + "."
	}
	// Ask for JSON so we can pull out description + type reliably.
	userText += "\n\nRespond ONLY with a JSON object of the form " +
		`{"description": "...", "type": "..."} with no other text.`

	raw, err := c.chat(ctx, request{
		Model:     c.Model,
		MaxTokens: 500,
		Thinking:  thinkingDisabled,
		Messages: []message{
			{Role: "system", Content: []contentPart{{Type: "text", Text: systemPrompt}}},
			{Role: "user", Content: []contentPart{
				{Type: "text", Text: userText},
				{Type: "image_url", ImageURL: &imageURL{URL: imageDataURI}},
			}},
		},
	})
	if err != nil {
		return Suggestion{}, err
	}
	return parseSuggestion(raw, types), nil
}

var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// parseSuggestion extracts a Suggestion from the model output, tolerating stray
// prose or code fences around the JSON. Falls back to using the whole string as
// the description.
func parseSuggestion(raw string, types []string) Suggestion {
	match := jsonObjRe.FindString(raw)
	if match != "" {
		var s Suggestion
		if err := json.Unmarshal([]byte(match), &s); err == nil && s.Description != "" {
			s.Type = closestType(s.Type, types)
			return s
		}
	}
	return Suggestion{Description: strings.TrimSpace(raw)}
}

// closestType maps the model's type back onto an allowed value (case-insensitive),
// returning "" if there's no match.
func closestType(got string, types []string) string {
	got = strings.TrimSpace(got)
	for _, t := range types {
		if strings.EqualFold(t, got) {
			return t
		}
	}
	return ""
}

// CorrectText fixes spelling, diacritics and grammar in the given text without
// changing its meaning or language. Returns only the corrected text.
func (c *Client) CorrectText(ctx context.Context, text, language string) (string, error) {
	sys := "You are a meticulous proofreader. Fix spelling, diacritics (accents such as Czech háček/čárka) and grammar in the user's text. Keep the original language"
	if language != "" {
		sys += " (" + language + ")"
	}
	sys += ", keep the meaning, keep it a single work-log sentence. Return ONLY the corrected text with no quotes, labels or explanation."

	return c.chat(ctx, request{
		Model:     c.Model,
		MaxTokens: 400,
		Thinking:  thinkingDisabled,
		Messages: []message{
			{Role: "system", Content: []contentPart{{Type: "text", Text: sys}}},
			{Role: "user", Content: []contentPart{{Type: "text", Text: text}}},
		},
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
