package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Template URL/model logic handles 4 cases depending on environment variables and platform.
// This simplifies switching between local and cloud models without manual reconfiguration.
var template = map[[2]bool][2]string{
	{false, true}:  {"http://localhost:1234/v1/chat/completions", "lmstudio-community/Qwen3-4B-MLX-8bit"},
	{false, false}: {"http://localhost:1234/v1/chat/completions", "qwen/qwen3-4b"},
	{true, false}:  {"https://api.openai.com/v1/chat/completions", "gpt-4.1-mini"},
	{true, true}:   {"https://api.openai.com/v1/chat/completions", "gpt-4.1-mini"},
}[[2]bool{os.Getenv("OPENAI_API_KEY") != "", runtime.GOOS == "darwin"}]

var (
	// 'mission' encapsulates user intent and is reused across turns if not explicitly cleared.
	// This supports multi-step planning without forcing repeated input.
	mission = flag.String("mission", "", "Mission to complete")

	apiURL = flag.String("url", template[0], "API URL")
	model  = flag.String("model", template[1], "Model to use (e.g., gpt-4.1-mini)")
)

func main() {
	flag.Parse()

	// Initial LLM warm-up query ensures that the model is online and responsive before continuing,
	// avoiding long feedback loops later in the interactive loop.
	fmt.Printf("\033[37m=== Warming up \033[35m%s\033[37m... ", *model)
	res, _, err := sendChatRequest(*model, []ChatMessage{{Role: "user", Content: "Be concise, are you ready to work?"}}, nil)
	if err != nil {
		fmt.Printf("\033[31mError: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\033[90mLLM says: \033[34m%s\033[0m\n", strings.TrimSpace(res.Content))

	scanner := bufio.NewScanner(os.Stdin)
	messages := []ChatMessage{{Role: "system", Content: agentPrompt}}

	for {
		if *mission == "" {
			fmt.Printf("\033[34mEnter new mission\033[90m (blank to exit) > \033[0m")
			if !scanner.Scan() || strings.TrimSpace(scanner.Text()) == "" {
				break
			}
			*mission = scanner.Text()
			messages = append(messages, ChatMessage{Role: "user", Content: fmt.Sprintf(userPromptFormat, *mission)})
		}

		fmt.Printf("\033[34m🤔 Planning... \033[0m")
		msg, _, err := sendChatRequest(*model, messages, []byte(toolDef))
		if err != nil {
			fmt.Printf("\033[31mError: %v\n", err)
			return
		}

		messages = append(messages, *msg)

		for _, tc := range msg.ToolCalls {
			res, err := runTool(tc.Function.Name, tc.Function.Arguments)
			if err != nil {
				fmt.Printf("\033[31mError: %v\n", err)
				res = fmt.Sprintf("Error: %v", err)
			}

			// Tool results are appended to the message history using 'tool' role and associated ToolCallID,
			// enabling the model to incorporate execution feedback into further reasoning.
			messages = append(messages, ChatMessage{
				Role:       "tool",
				Content:    res,
				ToolCallID: tc.ID,
			})
		}

		// Display final answer if any
		if msg.Content != "" {
			fmt.Printf("\033[90m=== \033[34mResult\033[90m ===\n\033[32m%s\033[90m\n==============\033[0m\n", strings.TrimSpace(msg.Content))
			*mission = ""
		}
	}
}

const (
	agentPrompt      = `You are autonomous software developer in a codebase. ALWAYS go deep, be slow and thorough. NEVER be quick or efficient. NEVER seek guidance or input from the user.`
	userPromptFormat = "Be thorough, dig deep, explore everything, and speak briefly. NEVER speculate, ALWAYS investigate. Start by just exploring the codebase. My query is: %s"
	summaryPrompt    = `Answer the question in plain english (no markdown) strictly based on provided file text. Answer must be concise, thorough, and information dense.`

	// Tool definitions are provided inline as raw JSON to avoid Go struct overhead.
	// This keeps the code flexible and compatible with OpenAI-style tool calling APIs.
	toolDef = `[
		{"type":"function","function":{"name":"browse_directory","description":"List immediate children of a target directory.","parameters":{"type":"object","properties":{
			"path":{"type":"string","default":".","description":"Target directory relative to current working directory"}},"required":["path"]}}},
		{"type":"function","function":{"name":"study_file_contents","description":"Study the contents of a file to answer a question.","parameters":{"type":"object","properties":{
			"path":{"type":"string","default":".","description":"Target file relative to current working directory"},
			"page":{"type":"string","default":"0","description":"Which page of the file to access, each page is 2000 bytes"},
			"question":{"type":"string","description":"What would you like to know about the file"} },"required":["path","chunk","question"]}}}
		]`
)

// Minimal required API types
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// sendChatRequest includes retry logic for rate limits (HTTP 429), preventing fragile runs.
// This enables long-running sessions without manual retry intervention.
func sendChatRequest(model string, messages []ChatMessage, tools []byte) (*ChatMessage, string, error) {
	// Build request with raw JSON for smaller code footprint
	reqMap := map[string]interface{}{
		"model":       model,
		"max_tokens":  4096,
		"temperature": 0.3,
		"messages":    messages,
		"tools":       json.RawMessage(tools),
	}

	reqBody, _ := json.Marshal(reqMap)
	req, _ := http.NewRequest("POST", *apiURL, strings.NewReader(string(reqBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+os.Getenv("OPENAI_API_KEY"))

	start := time.Now()
	for {
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			time.Sleep(time.Second)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("API error: %s", resp.Status)
		}

		var result struct {
			Choices []struct {
				Message ChatMessage `json:"message"`
			}
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			}
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, "", fmt.Errorf("failed to decode response: %v", err)
		}
		if len(result.Choices) == 0 {
			return nil, "", fmt.Errorf("no response")
		}

		cost := float64(result.Usage.PromptTokens)*(0.10/1_000_000) + float64(result.Usage.CompletionTokens)*(0.40/1_000_000)
		fmt.Printf("\033[90mDone in %.1fs for \033[35m%.2fc\033[90m (%d/%d tokens)\033[0m\n", time.Since(start).Seconds(), cost*100, result.Usage.PromptTokens, result.Usage.CompletionTokens) // keep purple

		msg := result.Choices[0].Message

		// Thoughts are parsed and separated from final content using a custom `</think>` marker.
		// This allows optional introspection/debugging of the model's reasoning phase.
		if i := strings.LastIndex(msg.Content, `</think>`); i != -1 {
			thoughts := msg.Content[:i+7]
			msg.Content = msg.Content[i+8:]
			return &msg, strings.TrimSpace(thoughts), nil
		}

		return &msg, "This model provided no thoughts.", nil
	}
}

// fileType uses UTF-8 validity as a fast heuristic to distinguish text from binary files.
// This avoids incorrect LLM inputs from non-text content, which could break prompt context.
func fileType(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("Error opening file: %v", err)
	}
	defer file.Close()

	header := make([]byte, 512)
	n, err := file.Read(header)
	if err != nil {
		return fmt.Sprintf("Error reading file header: %v", err)
	}
	header = header[:n]
	if utf8.Valid(header) {
		return "text"
	}
	return "binary"
}

// runTool executes any tool the LLM requests. It loosely prevents escaping the current working directory.
func runTool(name, args string) (string, error) {
	params := map[string]string{}
	json.Unmarshal([]byte(args), &params)

	// Handle directory
	if name == "browse_directory" {
		fmt.Printf("\033[90m🔍 Analyzing directory `\033[35m%s\033[90m`...\n", params["path"])
		if !filepath.IsLocal(params["path"]) {
			return "", fmt.Errorf("Permanent Error: Path %s is outside of current working directory", params["path"])
		}
		entries, err := os.ReadDir(params["path"])
		if err != nil {
			return "", fmt.Errorf("Error reading directory: %v", err)
		}

		filesByType := make(map[string][]string)
		for _, entry := range entries {
			fullPath := filepath.Join(params["path"], entry.Name())
			if typ := fileType(fullPath); !entry.IsDir() {
				filesByType[typ+" files"] = append(filesByType[typ+" files"], "`"+fullPath+"`")
			} else {
				filesByType["subdirectories"] = append(filesByType["subdirectories"], "`"+fullPath+"`")
			}
		}

		parts := make([]string, 0)
		for typ, files := range filesByType {
			parts = append(parts, fmt.Sprintf("- %s: %s", typ, files))
		}
		return fmt.Sprintf("analyze_path `%s` results:\n%s", params["path"], strings.Join(parts, "\n")), nil
	}

	start, _ := strconv.Atoi(params["page"])
	fmt.Printf("\033[90m🧠 Look at `\033[35m%v page %d\033[90m`. %s ", params["path"], start, params["question"])
	if !filepath.IsLocal(params["path"]) {
		return "", fmt.Errorf("Permanent Error: Path %s is outside of current working directory", params["path"])
	}
	if contentType := fileType(params["path"]); contentType != "text" {
		return "", fmt.Errorf("Not a text file (detected: %s)", contentType)
	}

	file, err := os.Open(params["path"])
	if err != nil {
		return "", fmt.Errorf("Error opening file: %v", err)
	}
	defer file.Close()

	// file.Read is paginated using fixed byte chunks (2000 bytes per page) to safely handle large files.
	// This prevents memory exhaustion and fits prompt size constraints for LLM input.
	content, _ := io.ReadAll(io.NewSectionReader(file, int64(start*2000), 2000))

	// Simple request for analysis
	msg, _, err := sendChatRequest(*model, []ChatMessage{
		{Role: "system", Content: summaryPrompt},
		{Role: "user", Content: string(content) + "\nThe question: " + params["question"]},
	}, nil)

	if err != nil {
		return "", fmt.Errorf("Error analyzing file: %v", err)
	}

	return fmt.Sprintf("study_file_contents %v results\nQuestion: %s\nAnswer: %s", params["path"], params["question"], msg.Content), nil
}
