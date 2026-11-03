package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"outfit-agent/internal/models"
)

type LLMClient struct {
	BaseURL, APIKey, Model string
	Client                 *http.Client
}

func NewLLMClientFromEnv() *LLMClient {
	key, model := strings.TrimSpace(os.Getenv("LLM_API_KEY")), strings.TrimSpace(os.Getenv("LLM_MODEL"))
	if key == "" || model == "" {
		return nil
	}
	base := strings.TrimSpace(os.Getenv("LLM_BASE_URL"))
	if base == "" {
		base = "https://ark.cn-beijing.volces.com/api/v3"
	}
	// 允许只填基准地址（.../api/v3），也允许填完整端点（.../chat/completions），两种都不会重复拼接。
	base = strings.TrimRight(base, "/")
	if !strings.HasSuffix(base, "/chat/completions") {
		base += "/chat/completions"
	}
	return &LLMClient{BaseURL: base, APIKey: key, Model: model, Client: &http.Client{Timeout: 30 * time.Second}}
}

// SuggestOutfit 让豆包给出**一套**搭配，返回原始文本（JSON），由 agent 解析。
// 候选商品会一起发给模型，并明确要求"只能从候选里挑"，以免它编造商品名和链接。
func (c *LLMClient) SuggestOutfit(p models.UserProfile, candidates []models.Product) (string, error) {
	if c == nil {
		return "", errors.New("LLM 未配置")
	}
	profile, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	budget := strings.TrimSpace(p.Budget)
	if budget == "" {
		budget = "未指定"
	}
	system := "你是中文穿搭顾问。只输出一个 JSON 对象，不要任何解释文字，不要 markdown 代码块。" +
		"严禁编造商品名称、价格、库存或链接：items 里的 keyword 必须原样抄写候选商品列表里的商品名。"

	user := fmt.Sprintf(`用户资料：%s候选商品（只能从这些里挑）：%s
	请给出 1 套完整穿搭，输出 JSON，格式：
	{"summary":"一句话总体思路",
	"reason":"整套为什么这么搭、颜色和版型怎么呼应",
	"items":[{"category":"上衣","keyword":"候选列表里的商品名原文","reason":"为什么选它"}],
	"tips":"注意事项"}
	要求：
	1. 共 4~6 件，category 只能用：上衣、裤装、裙装、外套、鞋、包、配饰；
	2. 合计价格尽量不超过 ¥%s（用户预算，未指定则忽略）；
	3. reason 要具体：说清颜色、版型、场合之间的关系，不要空话；
	4. 只输出 JSON，第一个字符必须是 {。`, string(profile), 
	renderCandidates(candidates), budget)

	payload := map[string]any{"model": c.Model, 
	"messages": []map[string]string{
		{"role": "system", "content": system},
		{"role": "user", "content": user},
	}, "temperature": 0.6}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("豆包接口返回：" + resp.Status)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", errors.New(result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", errors.New("接口没有返回 choices")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// renderCandidates 把候选商品渲染成紧凑清单，控制 token 消耗。
func renderCandidates(candidates []models.Product) string {
	var b strings.Builder
	for i, p := range candidates {
		fmt.Fprintf(&b, "%d. %s | %s | ¥%.0f | 风格:%s | 季节:%s | 场景:%s\n",
			i+1, p.Name, p.Category, p.Price,
			strings.Join(p.Style, "/"), strings.Join(p.Season, "/"), strings.Join(p.Occasion, "/"))
	}
	return b.String()
}
