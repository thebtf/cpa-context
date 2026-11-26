package main

import (
	"encoding/json"
	"strings"
	"unicode"
)

// Every prompt token lands in exactly one category: the dashboard draws them as
// one stacked bar, so a category total is the sum of its items.
const (
	CatSystem     = "system"
	CatTools      = "tools"
	CatUser       = "user"
	CatAssistant  = "assistant"
	CatThinking   = "thinking"
	CatToolCall   = "toolCall"
	CatToolResult = "toolResult"
	CatInjected   = "injected"
	CatImage      = "image"
	CatOther      = "other"
)

var categoryOrder = []string{
	CatSystem, CatTools, CatUser, CatAssistant, CatThinking,
	CatToolCall, CatToolResult, CatInjected, CatImage, CatOther,
}

// item is one addressable piece of the prompt: a message block, a tool schema,
// a system block. The dashboard expands items, so each keeps the exact text.
type item struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	Role     string `json:"role,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Chars    int    `json:"chars"`
	Text     string `json:"text,omitempty"`
	// Bytes covers payloads whose size is not text length: images, opaque JSON.
	Bytes int `json:"bytes,omitempty"`
}

// composition is the parsed shape of one request body.
type composition struct {
	Format       string
	Items        []item
	MessageCount int
	ToolCount    int
	Model        string
	// SystemText is the concatenated instruction prefix, kept because prompt
	// caching treats it as a separate segment.
	SystemText string
	Stream     bool
	MaxTokens  int
	// Title is the first user message, trimmed to a readable length. CPA resolves
	// session ids but not titles, so the conversation has to name itself.
	Title string
}

// maxTitleRunes keeps a card header to one line without cutting mid-word.
const maxTitleRunes = 90

// setTitle records the conversation title from the first user message.
func (c *composition) setTitle(text string) {
	if c.Title != "" {
		return
	}
	c.Title = titleFromText(text)
}

// titleFromText picks the line of a prompt that best names it.
//
// A first user message is often several paragraphs, a pasted file, or a fenced
// code block. The whole thing is not a title. The rule is: take the first
// paragraph, then its first line that carries words, strip list and heading
// markers, stop at the first sentence if that line is long, and finally cut on a
// word boundary. When nothing readable is left the title stays empty and the
// caller falls back to the session id.
func titleFromText(text string) string {
	for _, paragraph := range splitParagraphs(withoutCodeBlocks(text)) {
		for _, line := range strings.Split(paragraph, "\n") {
			candidate := stripMarkdownPrefix(line)
			if !hasWords(candidate) {
				continue
			}
			return shortenTitle(candidate)
		}
	}
	return ""
}

// splitParagraphs splits on blank lines, so a four-paragraph prompt yields the
// paragraph that was written first rather than all four joined together.
func splitParagraphs(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	blocks := strings.Split(normalized, "\n\n")
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block) != "" {
			out = append(out, block)
		}
	}
	return out
}

func stripMarkdownPrefix(line string) string {
	trimmed := strings.TrimSpace(line)
	for {
		switch {
		case strings.HasPrefix(trimmed, "#"):
			trimmed = strings.TrimLeft(trimmed, "#")
		case strings.HasPrefix(trimmed, ">"):
			trimmed = strings.TrimPrefix(trimmed, ">")
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
			trimmed = trimmed[2:]
		default:
			return strings.TrimSpace(trimmed)
		}
	}
}

// withoutCodeBlocks drops fenced blocks, so a prompt that opens with code is
// named by the prose around it instead of by a line of Go.
func withoutCodeBlocks(text string) string {
	var out strings.Builder
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			out.WriteString("\n")
			continue
		}
		if inFence {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

func hasWords(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// shortenTitle stops at the first sentence boundary when the line is long, then
// cuts on a word boundary.
func shortenTitle(line string) string {
	runes := []rune(line)
	if len(runes) <= maxTitleRunes {
		return line
	}
	// Prefer a real sentence end, but only one far enough in to be informative.
	cut := -1
	for i, r := range runes {
		if i < 24 {
			continue
		}
		if r == '.' || r == '?' || r == '!' {
			cut = i + 1
			break
		}
	}
	if cut > 0 && cut <= maxTitleRunes+40 {
		return string(runes[:cut])
	}
	limit := runes[:maxTitleRunes]
	// Back up to the last space so a word is not split in half.
	for i := len(limit) - 1; i > maxTitleRunes/2; i-- {
		if limit[i] == ' ' {
			return string(limit[:i]) + "…"
		}
	}
	return string(limit) + "…"
}

func newComposition(format string) *composition {
	return &composition{Format: format}
}

func (c *composition) add(it item) {
	if it.Chars == 0 && it.Bytes == 0 {
		return
	}
	if it.Category == "" {
		it.Category = CatOther
	}
	c.Items = append(c.Items, it)
}

// parseRequestBody dispatches on the format the host recorded and falls back to
// an honest "unparsed" item rather than inventing a breakdown.
func parseRequestBody(sourceFormat string, body []byte) *composition {
	if len(body) == 0 {
		return newComposition(sourceFormat)
	}
	switch normalizeFormat(sourceFormat) {
	case "claude":
		if c := parseClaude(body); c != nil {
			return c
		}
	case "openai":
		if c := parseOpenAIChat(body); c != nil {
			return c
		}
	case "openai-response":
		if c := parseOpenAIResponses(body); c != nil {
			return c
		}
	case "gemini":
		if c := parseGemini(body); c != nil {
			return c
		}
	}
	return parseGeneric(sourceFormat, body)
}

func normalizeFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "claude", "anthropic", "claude-messages":
		return "claude"
	case "openai", "openai-chat", "chat":
		return "openai"
	case "openai-response", "openai-responses", "responses":
		return "openai-response"
	case "gemini", "google", "vertex":
		return "gemini"
	}
	return strings.ToLower(strings.TrimSpace(format))
}

func parseClaude(body []byte) *composition {
	var payload struct {
		Model     string          `json:"model"`
		Stream    bool            `json:"stream"`
		MaxTokens int             `json:"max_tokens"`
		System    json.RawMessage `json:"system"`
		Messages  []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	c := newComposition("claude")
	c.Model = payload.Model
	c.Stream = payload.Stream
	c.MaxTokens = payload.MaxTokens

	for _, block := range decodeClaudeBlocks(payload.System) {
		c.add(item{Category: CatSystem, Label: "system instruction", Text: block.text, Chars: len(block.text), Bytes: block.bytes})
		c.SystemText += block.text
	}

	for _, message := range payload.Messages {
		role := message.Role
		for _, block := range decodeClaudeBlocks(message.Content) {
			c.add(claudeBlockItem(role, block))
			if role == "user" && block.kind == "text" {
				c.setTitle(block.text)
			}
		}
		c.MessageCount++
	}

	for _, tool := range payload.Tools {
		schema := compactJSON(tool.InputSchema)
		text := "name: " + tool.Name + "\n" + tool.Description + "\n" + schema
		c.add(item{Category: CatTools, Label: tool.Name, Detail: tool.Description, Text: text, Chars: len(text)})
		c.ToolCount++
	}
	return c
}

type claudeBlock struct {
	kind     string
	text     string
	toolName string
	bytes    int
}

func decodeClaudeBlocks(raw json.RawMessage) []claudeBlock {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []claudeBlock{{kind: "text", text: text}}
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Thinking  string          `json:"thinking"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		Content   json.RawMessage `json:"content"`
		ToolUseID string          `json:"tool_use_id"`
		Source    *struct {
			Data      string `json:"data"`
			MediaType string `json:"media_type"`
		} `json:"source"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return []claudeBlock{{kind: "text", text: string(raw), bytes: len(raw)}}
	}
	out := make([]claudeBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "text":
			out = append(out, claudeBlock{kind: "text", text: block.Text})
		case "thinking", "redacted_thinking":
			out = append(out, claudeBlock{kind: "thinking", text: block.Thinking})
		case "tool_use":
			out = append(out, claudeBlock{kind: "tool_use", text: compactJSON(block.Input), toolName: block.Name})
		case "tool_result":
			out = append(out, claudeBlock{kind: "tool_result", text: compactJSON(block.Content), toolName: block.ToolUseID, bytes: len(block.Content)})
		case "image":
			size := len(block.Source.Data)
			if size == 0 {
				size = len(block.Source.MediaType)
			}
			out = append(out, claudeBlock{kind: "image", bytes: size})
		default:
			out = append(out, claudeBlock{kind: block.Type, text: string(raw), bytes: len(raw)})
		}
	}
	return out
}

func claudeBlockItem(role string, block claudeBlock) item {
	switch block.kind {
	case "thinking":
		return item{Category: CatThinking, Role: role, Label: "thinking", Text: block.text, Chars: len(block.text)}
	case "tool_use":
		return item{Category: CatToolCall, Role: role, Label: block.toolName, Text: block.text, Chars: len(block.text)}
	case "tool_result":
		return item{Category: CatToolResult, Role: role, Label: block.toolName, Text: block.text, Chars: len(block.text), Bytes: block.bytes}
	case "image":
		return item{Category: CatImage, Role: role, Label: "image", Bytes: block.bytes}
	}
	if role == "assistant" {
		return item{Category: CatAssistant, Role: role, Label: "assistant reply", Text: block.text, Chars: len(block.text)}
	}
	return item{Category: CatUser, Role: role, Label: "user message", Text: block.text, Chars: len(block.text)}
}

func parseOpenAIChat(body []byte) *composition {
	var payload struct {
		Model     string `json:"model"`
		Stream    bool   `json:"stream"`
		MaxTokens int    `json:"max_completion_tokens"`
		Messages  []struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			Name       string          `json:"name"`
			ToolCallID string          `json:"tool_call_id"`
			ToolCalls  []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	c := newComposition("openai")
	c.Model = payload.Model
	c.Stream = payload.Stream
	c.MaxTokens = payload.MaxTokens
	if c.MaxTokens == 0 {
		var legacy struct {
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.Unmarshal(body, &legacy)
		c.MaxTokens = legacy.MaxTokens
	}

	for _, message := range payload.Messages {
		role := message.Role
		for _, part := range decodeOpenAIContent(message.Content) {
			c.add(openAIPartItem(role, message.Name, message.ToolCallID, part))
			if role == "user" && part.kind == "text" {
				c.setTitle(part.text)
			}
		}
		for _, call := range message.ToolCalls {
			text := call.Function.Name + "(" + call.Function.Arguments + ")"
			c.add(item{Category: CatToolCall, Role: role, Label: call.Function.Name, Text: text, Chars: len(text)})
		}
		c.MessageCount++
	}

	for _, tool := range payload.Tools {
		schema := compactJSON(tool.Function.Parameters)
		text := "name: " + tool.Function.Name + "\n" + tool.Function.Description + "\n" + schema
		c.add(item{Category: CatTools, Label: tool.Function.Name, Detail: tool.Function.Description, Text: text, Chars: len(text)})
		c.ToolCount++
	}
	return c
}

type openAIPart struct {
	kind     string
	text     string
	imageURL string
}

func decodeOpenAIContent(raw json.RawMessage) []openAIPart {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return []openAIPart{{kind: "text", text: text}}
	}
	var parts []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Refusal  string          `json:"refusal"`
		ImageURL json.RawMessage `json:"image_url"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return []openAIPart{{kind: "text", text: string(raw)}}
	}
	out := make([]openAIPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text", "input_text", "output_text":
			out = append(out, openAIPart{kind: "text", text: part.Text})
		case "refusal":
			out = append(out, openAIPart{kind: "text", text: part.Refusal})
		case "image_url", "input_image":
			url := part.ImageURL
			var plain string
			if err := json.Unmarshal(part.ImageURL, &plain); err == nil {
				url = []byte(plain)
			} else {
				var wrapped struct {
					URL string `json:"url"`
				}
				if json.Unmarshal(part.ImageURL, &wrapped) == nil {
					url = []byte(wrapped.URL)
				}
			}
			out = append(out, openAIPart{kind: "image", imageURL: string(url)})
		default:
			out = append(out, openAIPart{kind: part.Type, text: string(raw)})
		}
	}
	return out
}

func openAIPartItem(role, name, toolCallID string, part openAIPart) item {
	if part.kind == "image" {
		return item{Category: CatImage, Role: role, Label: "image", Bytes: len(part.imageURL)}
	}
	switch role {
	case "system", "developer":
		return item{Category: CatSystem, Role: role, Label: role + " message", Text: part.text, Chars: len(part.text)}
	case "tool":
		return item{Category: CatToolResult, Role: role, Label: firstNonEmpty(name, toolCallID, "tool result"), Text: part.text, Chars: len(part.text)}
	case "assistant":
		return item{Category: CatAssistant, Role: role, Label: "assistant reply", Text: part.text, Chars: len(part.text)}
	case "user":
		return item{Category: CatUser, Role: role, Label: "user message", Text: part.text, Chars: len(part.text)}
	}
	return item{Category: CatInjected, Role: role, Label: firstNonEmpty(role, "injected"), Text: part.text, Chars: len(part.text)}
}

func parseOpenAIResponses(body []byte) *composition {
	var payload struct {
		Model        string            `json:"model"`
		Stream       bool              `json:"stream"`
		MaxOutput    int               `json:"max_output_tokens"`
		Instructions json.RawMessage   `json:"instructions"`
		Input        []json.RawMessage `json:"input"`
		Tools        []struct {
			Type        string          `json:"type"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	c := newComposition("openai-response")
	c.Model = payload.Model
	c.Stream = payload.Stream
	c.MaxTokens = payload.MaxOutput

	if len(payload.Instructions) > 0 && string(payload.Instructions) != "null" {
		var text string
		if json.Unmarshal(payload.Instructions, &text) == nil {
			c.add(item{Category: CatSystem, Label: "instructions", Text: text, Chars: len(text)})
			c.SystemText += text
		}
	}

	for _, raw := range payload.Input {
		var typed struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			Name    string          `json:"name"`
			Output  json.RawMessage `json:"output"`
			Input   string          `json:"input"`
			Args    string          `json:"arguments"`
			CallID  string          `json:"call_id"`
		}
		if err := json.Unmarshal(raw, &typed); err != nil {
			continue
		}
		switch typed.Type {
		case "function_call":
			text := typed.Name + "(" + firstNonEmpty(typed.Args, typed.Input) + ")"
			c.add(item{Category: CatToolCall, Label: typed.Name, Text: text, Chars: len(text)})
		case "function_call_output":
			output := compactJSON(typed.Output)
			c.add(item{Category: CatToolResult, Label: firstNonEmpty(typed.CallID, "tool result"), Text: output, Chars: len(output)})
		case "", "message":
			role := typed.Role
			for _, part := range decodeOpenAIContent(typed.Content) {
				c.add(openAIPartItem(role, typed.Name, typed.CallID, part))
				if role == "user" && part.kind == "text" {
					c.setTitle(part.text)
				}
			}
			c.MessageCount++
		default:
			text := compactJSON(raw)
			c.add(item{Category: CatInjected, Label: typed.Type, Text: text, Chars: len(text)})
		}
	}

	for _, tool := range payload.Tools {
		if tool.Name == "" {
			continue
		}
		schema := compactJSON(tool.Parameters)
		text := "name: " + tool.Name + "\n" + tool.Description + "\n" + schema
		c.add(item{Category: CatTools, Label: tool.Name, Detail: tool.Description, Text: text, Chars: len(text)})
		c.ToolCount++
	}
	return c
}

func parseGemini(body []byte) *composition {
	type geminiPart struct {
		Text             string          `json:"text"`
		Thought          bool            `json:"thought"`
		FunctionCall     json.RawMessage `json:"functionCall"`
		FunctionResponse json.RawMessage `json:"functionResponse"`
		InlineData       *struct {
			Data     string `json:"data"`
			MimeType string `json:"mime_type"`
		} `json:"inlineData"`
	}
	type geminiContent struct {
		Role  string       `json:"role"`
		Parts []geminiPart `json:"parts"`
	}
	var payload struct {
		SystemInstruction      *geminiContent  `json:"systemInstruction"`
		SystemInstructionSnake *geminiContent  `json:"system_instruction"`
		Contents               []geminiContent `json:"contents"`
		Tools                  []struct {
			FunctionDeclarations []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Parameters  json.RawMessage `json:"parameters"`
			} `json:"functionDeclarations"`
		} `json:"tools"`
		GenerationConfig struct {
			MaxOutputTokens int  `json:"maxOutputTokens"`
			Stream          bool `json:"stream"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	c := newComposition("gemini")

	system := payload.SystemInstruction
	if system == nil {
		system = payload.SystemInstructionSnake
	}
	if system != nil {
		for _, part := range system.Parts {
			c.add(item{Category: CatSystem, Label: "system instruction", Text: part.Text, Chars: len(part.Text)})
			c.SystemText += part.Text
		}
	}

	for _, content := range payload.Contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}
		for _, part := range content.Parts {
			switch {
			case part.Thought:
				c.add(item{Category: CatThinking, Role: role, Label: "thought", Text: part.Text, Chars: len(part.Text)})
			case len(part.FunctionCall) > 0:
				text := compactJSON(part.FunctionCall)
				c.add(item{Category: CatToolCall, Role: role, Label: "functionCall", Text: text, Chars: len(text)})
			case len(part.FunctionResponse) > 0:
				text := compactJSON(part.FunctionResponse)
				c.add(item{Category: CatToolResult, Role: role, Label: "functionResponse", Text: text, Chars: len(text)})
			case part.InlineData != nil:
				c.add(item{Category: CatImage, Role: role, Label: "inlineData", Bytes: len(part.InlineData.Data)})
			default:
				c.add(openAIPartItem(role, "", "", openAIPart{kind: "text", text: part.Text}))
				if role == "user" {
					c.setTitle(part.Text)
				}
			}
		}
		c.MessageCount++
	}

	for _, tool := range payload.Tools {
		for _, decl := range tool.FunctionDeclarations {
			schema := compactJSON(decl.Parameters)
			text := "name: " + decl.Name + "\n" + decl.Description + "\n" + schema
			c.add(item{Category: CatTools, Label: decl.Name, Detail: decl.Description, Text: text, Chars: len(text)})
			c.ToolCount++
		}
	}
	c.MaxTokens = payload.GenerationConfig.MaxOutputTokens
	c.Stream = payload.GenerationConfig.Stream
	return c
}

func parseGeneric(format string, body []byte) *composition {
	c := newComposition(format)
	c.add(item{Category: CatOther, Label: "unparsed body", Text: string(body), Chars: len(body)})
	return c
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(encoded)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
