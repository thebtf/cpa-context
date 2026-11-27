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
	// CatSkill is injected context that a skill load produced. The client shows
	// it as its own category, so a skill's cost is not hidden inside injections.
	CatSkill = "skill"
	CatImage = "image"
	CatOther = "other"
)

var categoryOrder = []string{
	CatSystem, CatTools, CatUser, CatAssistant, CatThinking,
	CatToolCall, CatToolResult, CatInjected, CatSkill, CatImage, CatOther,
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
	// Tool is the tool's own name for a call or a result. Claude puts the name on
	// the call and only an id on the result, so results resolve it by that id.
	Tool string `json:"tool,omitempty"`
	// Target is what the call acted on: a file path, a command, a search
	// pattern. It is read from the arguments, which the prompt replays.
	Target string `json:"target,omitempty"`
	// Err reports that a tool result carries an error.
	Err bool `json:"err,omitempty"`
	// Added and Removed count the lines a write added and removed.
	Added   int `json:"added,omitempty"`
	Removed int `json:"removed,omitempty"`
	// Hits is how many matches a search found.
	Hits int `json:"hits,omitempty"`
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
	// seenPrompt accumulates the user's own text while no block has named the
	// conversation yet. It exists only until a title is found.
	seenPrompt string
}

// maxTitleRunes keeps a card header to one line without cutting mid-word.
const maxTitleRunes = 90

// setTitle records the conversation title from the user's own text.
//
// A prompt arrives as several blocks, and the first one is often not the one
// that names the work: an id wrapper, a pasted file, a reminder. The title is
// therefore taken from the first block that yields a readable name, and a later
// block is ignored once one is found.
func (c *composition) setTitle(text string) {
	if c.Title != "" {
		return
	}
	c.Title = titleFromText(text)
	if c.Title == "" {
		// Keep the whole prompt: a later block may still name the work, and the
		// most descriptive line across the message is a better title than the
		// session id.
		c.seenPrompt += "\n\n" + text
	}
}

// finishTitle fills a title that no single block produced, using the whole user
// text collected across the message.
func (c *composition) finishTitle() {
	if c.Title != "" || c.seenPrompt == "" {
		return
	}
	c.Title = titleFromText(c.seenPrompt)
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
	cleaned := withoutHarnessBlocks(withoutCodeBlocks(text))
	if candidate, ok := nameFromFirstParagraph(cleaned); ok {
		return shortenTitle(candidate)
	}
	// Nothing in the opening paragraph named the work: a prompt can begin with a
	// pasted block, an id, or a table. The most descriptive line anywhere in the
	// prompt is a better answer than the session id.
	if candidate, ok := bestLineOf(cleaned); ok {
		return shortenTitle(candidate)
	}
	return ""
}

// nameFromFirstParagraph takes the first usable line of the opening paragraph,
// which is where a person states what they want.
func nameFromFirstParagraph(text string) (string, bool) {
	for _, paragraph := range splitParagraphs(text) {
		for _, line := range strings.Split(paragraph, "\n") {
			candidate := stripMarkdownPrefix(stripTags(line))
			if !readableTitle(candidate) {
				continue
			}
			return candidate, true
		}
	}
	return "", false
}

// bestLineOf picks the line that says the most about the work: the one with the
// most words, preferring a sentence over a fragment.
func bestLineOf(text string) (string, bool) {
	best := ""
	bestWords := 0
	for _, line := range strings.Split(text, "\n") {
		candidate := stripMarkdownPrefix(stripTags(line))
		if !readableTitle(candidate) {
			continue
		}
		words := len(strings.Fields(candidate))
		if words > bestWords {
			best, bestWords = candidate, words
		}
	}
	return best, bestWords > 0
}

// readableTitle reports a line that can name a conversation. An identifier is
// not a name: "m44881" or "abc123def" carries no meaning, while a line with a
// few real words does.
func readableTitle(candidate string) bool {
	if !hasWords(candidate) || looksLikeHarnessTag(candidate) || looksLikeRoleLabel(candidate) {
		return false
	}
	return hasWordWithLetters(candidate)
}

// hasWordWithLetters reports at least one token that reads as a word rather than
// a hash: three or more letters, not a run of hex or digits. Letters are counted
// in any script, because a title can be written in Cyrillic, Greek or Han as
// easily as in Latin, and a rule that only knows a-z turns every non-English name
// into an identifier.
func hasWordWithLetters(line string) bool {
	for _, token := range strings.Fields(line) {
		letters := 0
		digits := 0
		for _, r := range token {
			switch {
			case unicode.IsLetter(r):
				letters++
			case unicode.IsDigit(r):
				digits++
			}
		}
		// A Han or Kana token is short and dense: two characters already say more
		// than a hex run ever will.
		if letters >= 3 && letters > digits {
			return true
		}
		if letters >= 2 && digits == 0 && !isLatin(rune(token[0])) {
			return true
		}
	}
	return false
}

// isLatin reports a Latin letter, which needs more of them than a denser script
// before a token counts as a word.
func isLatin(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// harnessTags are the wrapper elements a harness puts into a user turn. Their
// content is machine instruction, not the person's own words, and naming a
// conversation after one of them tells the operator nothing about the work.
var harnessTags = []string{
	"system-reminder", "system_reminder", "reminder", "instructions",
	"tool_result", "tool_use", "file_content", "attachment", "thinking",
	"antml:thinking", "antml:function_calls", "function_results",
}

// withoutHarnessBlocks drops the content of every harness wrapper, including an
// unclosed one that runs to the end of the prompt.
func withoutHarnessBlocks(text string) string {
	out := text
	for _, tag := range harnessTags {
		out = stripTagBlock(out, "<"+tag+">", "</"+tag+">")
	}
	return out
}

func stripTagBlock(text, open, close string) string {
	for {
		start := strings.Index(text, open)
		if start < 0 {
			return text
		}
		rest := text[start:]
		end := strings.Index(rest, close)
		if end < 0 {
			// An unclosed wrapper swallows the rest: nothing after it can be
			// trusted to be the person's own text.
			return text[:start]
		}
		text = text[:start] + rest[end+len(close):]
	}
}

// looksLikeHarnessTag reports a line that is only a tag, such as "</user>" or
// "<topic>", which carries no meaning as a title.
func looksLikeHarnessTag(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || !strings.HasPrefix(trimmed, "<") || !strings.HasSuffix(trimmed, ">") {
		return false
	}
	inner := strings.Trim(trimmed, "<>/ ")
	return inner != "" && !strings.ContainsAny(inner, " \t")
}

// roleLabels are the bare markers a client writes above a block to say what the
// block is. They name the envelope, not the request, so they make a useless
// title when a prompt happens to start with one.
var roleLabels = map[string]struct{}{
	"topic": {}, "user": {}, "assistant": {}, "system": {}, "human": {},
	"message": {}, "prompt": {}, "request": {}, "context": {}, "input": {},
}

// looksLikeRoleLabel reports a line that only names the block it introduces.
func looksLikeRoleLabel(line string) bool {
	trimmed := strings.Trim(strings.TrimSpace(line), "#*>_- \t")
	if trimmed == "" {
		return false
	}
	_, ok := roleLabels[strings.ToLower(trimmed)]
	return ok
}

// stripTags removes wrapper tags from a line, keeping the text between them.
func stripTags(line string) string {
	var out strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] != '<' {
			out.WriteByte(line[i])
			continue
		}
		end := strings.IndexByte(line[i:], '>')
		if end < 0 {
			out.WriteString(line[i:])
			break
		}
		i += end
	}
	return out.String()
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

	// Claude names a tool on its call and only repeats an id on the result, so
	// the names are collected first and results are labelled by the call they
	// answer. Without this pass a tool result is labelled with a bare id, which
	// is what made tool rows unreadable.
	names := map[string]string{}
	for _, message := range payload.Messages {
		for _, block := range decodeClaudeBlocks(message.Content) {
			if block.kind == "tool_use" && block.toolUseID != "" {
				names[block.toolUseID] = block.toolName
			}
		}
	}
	for _, message := range payload.Messages {
		role := message.Role
		for _, block := range decodeClaudeBlocks(message.Content) {
			c.add(claudeBlockItem(role, block, names))
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
	c.finishTitle()
	return c
}

// toolCallItem builds the item for one tool call: the name the model chose, the
// arguments the prompt replays, and the target those arguments name.
func toolCallItem(name, args, text, role string) item {
	call := item{Category: CatToolCall, Role: role, Label: name, Tool: name, Text: text, Chars: len(text)}
	target, added, removed := toolTarget(name, args)
	call.Target = target
	call.Added = added
	call.Removed = removed
	return call
}

type claudeBlock struct {
	kind      string
	text      string
	toolName  string
	toolUseID string
	args      string
	isError   bool
	bytes     int
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
		ID        string          `json:"id"`
		IsError   bool            `json:"is_error"`
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
			out = append(out, claudeBlock{
				kind:      "tool_use",
				text:      compactJSON(block.Input),
				args:      compactJSON(block.Input),
				toolName:  block.Name,
				toolUseID: block.ID,
			})
		case "tool_result":
			out = append(out, claudeBlock{
				kind:      "tool_result",
				text:      compactJSON(block.Content),
				toolUseID: block.ToolUseID,
				isError:   block.IsError,
				bytes:     len(block.Content),
			})
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

func claudeBlockItem(role string, block claudeBlock, names map[string]string) item {
	switch block.kind {
	case "thinking":
		return item{Category: CatThinking, Role: role, Label: "thinking", Text: block.text, Chars: len(block.text)}
	case "tool_use":
		call := toolCallItem(block.toolName, block.args, block.text, role)
		return call
	case "tool_result":
		name := names[block.toolUseID]
		if name == "" {
			name = "tool result"
		}
		return item{
			Category: CatToolResult, Role: role, Label: name, Tool: name,
			Text: block.text, Chars: len(block.text), Bytes: block.bytes,
			Err: block.isError,
		}
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
				ID       string `json:"id"`
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
	callNames := map[string]string{}
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
			c.add(openAIPartItem(role, message.Name, message.ToolCallID, part, callNames))
			if role == "user" && part.kind == "text" {
				c.setTitle(part.text)
			}
		}
		for _, call := range message.ToolCalls {
			text := call.Function.Name + "(" + call.Function.Arguments + ")"
			c.add(toolCallItem(call.Function.Name, call.Function.Arguments, text, role))
			callNames[call.ID] = call.Function.Name
		}
		c.MessageCount++
	}

	for _, tool := range payload.Tools {
		schema := compactJSON(tool.Function.Parameters)
		text := "name: " + tool.Function.Name + "\n" + tool.Function.Description + "\n" + schema
		c.add(item{Category: CatTools, Label: tool.Function.Name, Detail: tool.Function.Description, Text: text, Chars: len(text)})
		c.ToolCount++
	}
	c.finishTitle()
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

func openAIPartItem(role, name, toolCallID string, part openAIPart, callNames map[string]string) item {
	if part.kind == "image" {
		return item{Category: CatImage, Role: role, Label: "image", Bytes: len(part.imageURL)}
	}
	switch role {
	case "system", "developer":
		return item{Category: CatSystem, Role: role, Label: role + " message", Text: part.text, Chars: len(part.text)}
	case "tool":
		// A tool message carries the id of the call it answers, and only some
		// providers also repeat the tool's name, so the earlier call is the
		// authority on what to label this row.
		label := firstNonEmpty(name, callNames[toolCallID], "tool result")
		return item{Category: CatToolResult, Role: role, Label: label, Tool: label, Text: part.text, Chars: len(part.text)}
	case "assistant":
		return item{Category: CatAssistant, Role: role, Label: "assistant reply", Text: part.text, Chars: len(part.text)}
	case "user":
		return item{Category: CatUser, Role: role, Label: "user message", Text: part.text, Chars: len(part.text)}
	}
	// Injected context is where a skill load lands, if it is one.
	if skill, ok := isSkillInjection(role, part.text); ok {
		return item{Category: CatSkill, Role: role, Label: skill, Text: part.text, Chars: len(part.text)}
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
	callNames := map[string]string{}
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
			args := firstNonEmpty(typed.Args, typed.Input)
			text := typed.Name + "(" + args + ")"
			c.add(toolCallItem(typed.Name, args, text, ""))
			callNames[typed.CallID] = typed.Name
		case "function_call_output":
			output := compactJSON(typed.Output)
			name := callNames[typed.CallID]
			if name == "" {
				name = "tool result"
			}
			c.add(item{
				Category: CatToolResult, Label: name, Tool: name,
				Text: output, Chars: len(output),
			})
		case "", "message":
			role := typed.Role
			for _, part := range decodeOpenAIContent(typed.Content) {
				c.add(openAIPartItem(role, typed.Name, typed.CallID, part, callNames))
				if role == "user" && part.kind == "text" {
					c.setTitle(part.text)
				}
			}
			c.MessageCount++
		default:
			text := compactJSON(raw)
			if skill, ok := isSkillInjection(typed.Type, text); ok {
				c.add(item{Category: CatSkill, Label: skill, Text: text, Chars: len(text)})
			} else {
				c.add(item{Category: CatInjected, Label: typed.Type, Text: text, Chars: len(text)})
			}
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
	c.finishTitle()
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
				c.add(openAIPartItem(role, "", "", openAIPart{kind: "text", text: part.Text}, nil))
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
	c.finishTitle()
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
