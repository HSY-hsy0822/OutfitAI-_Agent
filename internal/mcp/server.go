package mcp

// MCP server：把现有 Agent 的能力（穿搭推荐 + 衣橱查询）通过 Model Context Protocol 暴露出去。
// 传输方式：stdio（换行分隔的 JSON-RPC），可被任意 MCP 客户端（Claude Desktop、Cursor 等）接入。
// 本包只做协议封装，搭配算法仍在 agent 包，商品数据仍在 repository 包，核心逻辑不重复实现。

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"outfit-agent/internal/agent"
	"outfit-agent/internal/models"
	"outfit-agent/internal/repository"
)

// ---------- recommend_outfits 的输入 / 输出 ----------

// RecommendInput 与 TUI 里 recommend_outfits 的参数保持一致，jsonschema 标签会变成工具参数描述。
type RecommendInput struct {
	Gender         string `json:"gender" jsonschema:"性别，如 男 / 女"`
	Age            string `json:"age" jsonschema:"年龄，如 20"`
	Height         string `json:"height" jsonschema:"身高cm，如 163"`
	Weight         string `json:"weight" jsonschema:"体重kg，如 55"`
	BodyType       string `json:"body_type" jsonschema:"身材，如 梨形 / H型"`
	SkinTone       string `json:"skin_tone" jsonschema:"肤色"`
	Style          string `json:"style" jsonschema:"风格，如 休闲 / 通勤 / 甜美"`
	Occasion       string `json:"occasion" jsonschema:"场景，如 上课 / 通勤 / 约会"`
	Season         string `json:"season" jsonschema:"季节，如 秋季"`
	Budget         string `json:"budget" jsonschema:"整套预算元，如 300"`
	SpecialRequest string `json:"special_request" jsonschema:"特殊调整，如 鞋子要便宜、不要裙子"`
}

// OutfitItemOut 是方案里的单件；匹配不到本地商品时 Name 为空。
type OutfitItemOut struct {
	Category string  `json:"category"`
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	Reason   string  `json:"reason"`
	URL      string  `json:"url"`
}

// RecommendOutput 是结构化的整套搭配。
type RecommendOutput struct {
	Summary    string          `json:"summary"`
	Reason     string          `json:"reason"`
	TotalPrice float64         `json:"total_price"`
	OverBudget bool            `json:"over_budget"`
	Items      []OutfitItemOut `json:"items"`
	Tips       string          `json:"tips"`
}

// ---------- search_wardrobe 的输入 / 输出 ----------

type SearchInput struct {
	Category string `json:"category" jsonschema:"品类，如 上衣 / 裤装 / 鞋 / 外套"`
	Keyword  string `json:"keyword" jsonschema:"商品名关键词"`
	Gender   string `json:"gender" jsonschema:"性别过滤，如 女"`
	Limit    int    `json:"limit" jsonschema:"最多返回条数，默认 10"`
}

type ProductOut struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Price    float64 `json:"price"`
	Platform string  `json:"platform"`
	URL      string  `json:"url"`
}

type SearchOutput struct {
	Total    int          `json:"total"`
	Products []ProductOut `json:"products"`
}

// NewServer 从衣橱文件路径加载并构造 MCP server。
func NewServer(wardrobePath string) (*mcp.Server, error) {
	w, err := repository.LoadWardrobe(wardrobePath)
	if err != nil {
		return nil, err
	}
	return NewServerWithWardrobe(w), nil
}

// NewServerWithWardrobe 用已加载的衣橱构造 MCP server，并注册两个工具。
func NewServerWithWardrobe(w *repository.Wardrobe) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "outfit-agent", Version: "1.0.0"}, nil)
	stylist := agent.New()

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "recommend_outfits",
		Description: "根据用户资料，从本地衣橱库生成一套完整穿搭（4~6 件）。用户想要方案、换一套、调整预算或某件单品时调用。",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in RecommendInput) (*mcp.CallToolResult, RecommendOutput, error) {
		p := models.UserProfile{
			Gender: in.Gender, Age: in.Age, Height: in.Height, Weight: in.Weight,
			BodyType: in.BodyType, SkinTone: in.SkinTone, Style: in.Style,
			Occasion: in.Occasion, Season: in.Season, Budget: in.Budget,
		}

		candidates := w.Candidates(p, 40)
		rec := stylist.Recommend(p, candidates)
		rec = applyCheaperRequest(rec, p, w.Products, in.SpecialRequest)

		out := RecommendOutput{
			Summary:    rec.Summary,
			Reason:     rec.Reason,
			TotalPrice: rec.TotalPrice,
			OverBudget: rec.OverBudget,
			Tips:       rec.Tips,
			Items:      make([]OutfitItemOut, 0, len(rec.Items)),
		}
		for _, it := range rec.Items {
			io := OutfitItemOut{Category: it.Category, Reason: it.Reason}
			if it.Product != nil {
				io.Name, io.Price, io.URL = it.Product.Name, it.Product.Price, it.Product.URL
			}
			out.Items = append(out.Items, io)
		}
		return nil, out, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_wardrobe",
		Description: "按品类、商品名关键词、性别检索本地衣橱库，返回商品清单（含价格和链接），不编造任何商品。",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		category := strings.TrimSpace(in.Category)
		keyword := strings.ToLower(strings.TrimSpace(in.Keyword))

		out := SearchOutput{Products: make([]ProductOut, 0, limit)}
		for _, pr := range w.Products {
			if !repository.GenderOK(pr.Gender, in.Gender) {
				continue
			}
			if category != "" && !strings.EqualFold(strings.TrimSpace(pr.Category), category) {
				continue
			}
			if keyword != "" && !strings.Contains(strings.ToLower(pr.Name), keyword) {
				continue
			}
			out.Products = append(out.Products, ProductOut{
				ID: pr.ID, Name: pr.Name, Category: pr.Category, Price: pr.Price,
				Platform: pr.Platform, URL: pr.URL,
			})
			if len(out.Products) >= limit {
				break
			}
		}
		out.Total = len(out.Products)
		return nil, out, nil
	})

	return srv
}

// applyCheaperRequest 处理特殊要求里的"某件换便宜点"：在同品类里找更便宜的替换。
// 与 TUI helpers 中的处理规则一致（独立实现，避免跨包依赖私有函数）。
func applyCheaperRequest(rec models.Recommendation, p models.UserProfile, all []models.Product, request string) models.Recommendation {
	req := strings.ToLower(strings.TrimSpace(request))
	if req == "" {
		return rec
	}
	if !strings.Contains(req, "便宜") && !strings.Contains(req, "平价") && !strings.Contains(req, "省钱") {
		return rec
	}
	for i, it := range rec.Items {
		if it.Product == nil || !categoryMentioned(it.Category, req) {
			continue
		}
		var best *models.Product
		for j := range all {
			c := all[j]
			if !strings.EqualFold(c.Category, it.Category) || c.ID == it.Product.ID || c.Price >= it.Product.Price {
				continue
			}
			if !repository.GenderOK(c.Gender, p.Gender) {
				continue
			}
			if best == nil || c.Price < best.Price {
				cc := c
				best = &cc
			}
		}
		if best != nil {
			oldPrice := it.Product.Price
			it.Product = best
			it.Reason = fmt.Sprintf("按你要求换成更便宜的「%s」，省 ¥%.0f，风格依然搭。", best.Name, oldPrice-best.Price)
			rec.TotalPrice += best.Price - oldPrice
			rec.Items[i] = it
		}
	}
	return rec
}

func categoryMentioned(category, req string) bool {
	aliases := map[string][]string{
		"上衣": {"上衣"}, "裤装": {"裤"}, "裙装": {"裙"}, "外套": {"外套"},
		"鞋": {"鞋"}, "包": {"包"}, "配饰": {"配饰"},
	}
	for _, a := range aliases[category] {
		if strings.Contains(req, a) {
			return true
		}
	}
	return false
}

// LoadEnvFile 简单解析 .env（已存在的环境变量不覆盖）；文件不存在时静默返回。
func LoadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"'`)
		if key != "" {
			if _, ok := os.LookupEnv(key); !ok {
				_ = os.Setenv(key, val)
			}
		}
	}
}
