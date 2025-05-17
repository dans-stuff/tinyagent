package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

// defaults depending if we're on mac and if an OPENAI_API_KEY is set
var template = map[[2]bool][2]string{
	{false, true}:  {"http://localhost:1234/v1/chat/completions", "lmstudio-community/Qwen3-4B-MLX-8bit"},
	{false, false}: {"http://localhost:1234/v1/chat/completions", "qwen/qwen3-4b"},
	{true, false}:  {"https://api.openai.com/v1/chat/completions", "gpt-4.1-mini"},
	{true, true}:   {"https://api.openai.com/v1/chat/completions", "gpt-4.1-mini"},
}[[2]bool{os.Getenv("OPENAI_API_KEY") != "", runtime.GOOS == "darwin"}]

var (
	apiURL  = flag.String("url", template[0], "API URL")
	model   = flag.String("model", template[1], "Model to use (e.g., gpt-4.1-mini)")
	mission = flag.String("mission", "", "Mission to complete")
)

func main() {
	flag.Parse()
	fmt.Printf("\033[37m=== Warming up... ")
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
			// fmt.Printf("\033[36mResults: %s\033[0m\n", res)
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
	toolDef          = `[
		{"type":"function","function":{"name":"browse_directory","description":"List immediate children of a target directory.","parameters":{"type":"object","properties":{
			"path":{"type":"string","default":".","description":"Target directory relative to current working directory"}},"required":["path"]}}},
		{"type":"function","function":{"name":"study_file_contents","description":"Study the contents of a file to answer a question.","parameters":{"type":"object","properties":{
			"path":{"type":"string","default":".","description":"Target file relative to current working directory"},
			"question":{"type":"string","description":"What would you like to know about the file"} },"required":["path","question"]}}}
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

// sendChatRequest sends a conversation to the API and returns the response (and possible thoughts) from the LLM
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
		if i := strings.LastIndex(msg.Content, `</think>`); i != -1 {
			thoughts := msg.Content[:i+7]
			msg.Content = msg.Content[i+8:]
			return &msg, strings.TrimSpace(thoughts), nil
		}

		return &msg, "This model provided no thoughts.", nil
	}
}

func fileType(path string) string {
	// Check if file is text using MIME detection
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

	fmt.Printf("\033[90m🧠 Look at `\033[35m%v\033[90m`. %s ", params["path"], params["question"])
	if !filepath.IsLocal(params["path"]) {
		return "", fmt.Errorf("Permanent Error: Path %s is outside of current working directory", params["path"])
	}
	if contentType := fileType(params["path"]); contentType != "text" {
		return "", fmt.Errorf("Not a text file (detected: %s)", contentType)
	}

	stat, err := os.Stat(params["path"])
	if err != nil {
		return "", fmt.Errorf("Error getting file info: %v", err)
	}

	file, err := os.Open(params["path"])
	if err != nil {
		return "", fmt.Errorf("Error opening file: %v", err)
	}
	defer file.Close()

	// Read file content (safely limited)
	note := ""
	start := int64(0)
	if stat.Size() > 1000 {
		start = rand.Int63n(stat.Size()/1000) * 1000
		note = fmt.Sprintf("TRUNCATED FILE. Bytes %d to %d. Analyzing again will use a different random section.", start, start+1000)
	}
	content, _ := io.ReadAll(io.NewSectionReader(file, start, 1000))

	// Simple request for analysis
	msg, _, err := sendChatRequest(*model, []ChatMessage{
		{Role: "system", Content: summaryPrompt},
		{Role: "user", Content: string(content) + "\nThe question: " + params["question"]},
	}, nil)

	if err != nil {
		return "", fmt.Errorf("Error analyzing file: %v", err)
	}

	return fmt.Sprintf("study_file_contents %v results\nQuestion: %s\nAnswer: %s.%s", params["path"], params["question"], msg.Content, note), nil
}
