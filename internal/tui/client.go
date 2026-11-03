package tui

// 大模型客户端：把对话历史发给模型，并把模型的回答/工具调用解析回来。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type toolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatClient struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

func newChatClientFromEnv() *chatClient {
	base := strings.TrimSpace(os.Getenv("LLM_BASE_URL"))
	if base == "" {
		base = "https://ark.cn-beijing.volces.com/api/v3"
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasSuffix(base, "/chat/completions") {
		base += "/chat/completions"
	}
	return &chatClient{
		baseURL: base,
		apiKey:  strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		model:   strings.TrimSpace(os.Getenv("LLM_MODEL")),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

var toolDefinitions = []map[string]any{
	{
		"type": "function",
		"function": map[string]any{
			"name":        "recommend_outfits",
			"description": "根据用户资料，从本地衣橱库生成一套完整穿搭。用户想要方案、想换一套、调整某件或预算时调用。",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"gender":          map[string]string{"type": "string", "description": "性别，如 男 / 女"},
					"age":             map[string]string{"type": "string", "description": "年龄，如 20"},
					"height":          map[string]string{"type": "string", "description": "身高cm，如 163"},
					"weight":          map[string]string{"type": "string", "description": "体重kg，如 55"},
					"body_type":       map[string]string{"type": "string", "description": "身材，如 梨形 / H型"},
					"skin_tone":       map[string]string{"type": "string", "description": "肤色"},
					"style":           map[string]string{"type": "string", "description": "风格，如 休闲 / 通勤 / 甜美"},
					"occasion":        map[string]string{"type": "string", "description": "场景，如 上课 / 通勤 / 约会"},
					"season":          map[string]string{"type": "string", "description": "季节，如 秋季"},
					"budget":          map[string]string{"type": "string", "description": "整套预算元，如 300"},
					"special_request": map[string]string{"type": "string", "description": "特殊调整，如 鞋子要便宜、不要裙子"},
				},
			},
		},
	},
}

func (c *chatClient) chat(messages []chatMessage) (string, []toolCall, error) {
	if c.apiKey == "" || c.model == "" {
		return "", nil, fmt.Errorf("未配置大模型：请确认项目根目录有 .env 且填好 LLM_API_KEY、LLM_MODEL")
	}
	payload := map[string]any{
		"model":       c.model,
		"messages":    messages,
		"temperature": 0.7,
		"tools":       toolDefinitions,
		"tool_choice": "auto",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("模型接口返回：%s", resp.Status)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []toolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, err
	}
	if result.Error != nil {
		return "", nil, fmt.Errorf("%s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", nil, fmt.Errorf("接口没有返回 choices")
	}
	msg := result.Choices[0].Message
	return strings.TrimSpace(msg.Content), msg.ToolCalls, nil
}
