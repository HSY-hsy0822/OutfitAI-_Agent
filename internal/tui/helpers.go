package tui

// 工具执行和各种辅助函数：出搭配、换便宜货、读 .env 等。

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"outfit-agent/internal/agent"
	"outfit-agent/internal/models"
	"outfit-agent/internal/repository"
)

// runRecommendTool 执行 recommend_outfits：取候选 → 生成搭配 → 处理特殊要求。
// 返回给模型的 JSON 文本，以及本方案的商品列表（供 /open、/search 使用）。
func runRecommendTool(tc toolCall, wardrobe *repository.Wardrobe, stylist *agent.Agent) (string, []models.Product) {
	var args struct {
		Gender         string `json:"gender"`
		Age            string `json:"age"`
		Height         string `json:"height"`
		Weight         string `json:"weight"`
		BodyType       string `json:"body_type"`
		SkinTone       string `json:"skin_tone"`
		Style          string `json:"style"`
		Occasion       string `json:"occasion"`
		Season         string `json:"season"`
		Budget         string `json:"budget"`
		SpecialRequest string `json:"special_request"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return `{"error":"工具参数不是合法 JSON"}`, nil
	}
	p := models.UserProfile{
		Gender: args.Gender, Age: args.Age, Height: args.Height, Weight: args.Weight,
		BodyType: args.BodyType, SkinTone: args.SkinTone, Style: args.Style,
		Occasion: args.Occasion, Season: args.Season, Budget: args.Budget,
	}

	candidates := wardrobe.Candidates(p, 40)
	rec := stylist.Recommend(p, candidates)
	rec = applySpecialRequest(rec, p, wardrobe.Products, args.SpecialRequest)

	return renderRecForModel(rec), productsOf(rec.Items)
}

// applySpecialRequest 处理"某件换便宜点"：在全量衣橱同品类里找更便宜的替换。
func applySpecialRequest(rec models.Recommendation, p models.UserProfile, all []models.Product, request string) models.Recommendation {
	req := strings.ToLower(strings.TrimSpace(request))
	if req == "" {
		return rec
	}
	wantsCheaper := strings.Contains(req, "便宜") || strings.Contains(req, "平价") || strings.Contains(req, "省钱")
	if !wantsCheaper {
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

func productsOf(items []models.OutfitItem) []models.Product {
	out := make([]models.Product, 0, len(items))
	for _, it := range items {
		if it.Product != nil {
			out = append(out, *it.Product)
		}
	}
	return out
}

func renderRecForModel(rec models.Recommendation) string {
	type itemOut struct {
		Category string  `json:"category"`
		Name     string  `json:"name"`
		Price    float64 `json:"price"`
		Reason   string  `json:"reason"`
		URL      string  `json:"url"`
	}
	items := make([]itemOut, 0, len(rec.Items))
	for _, it := range rec.Items {
		io := itemOut{Category: it.Category, Reason: it.Reason}
		if it.Product != nil {
			io.Name, io.Price, io.URL = it.Product.Name, it.Product.Price, it.Product.URL
		}
		items = append(items, io)
	}
	out := struct {
		Summary    string    `json:"summary"`
		TotalPrice float64   `json:"total_price"`
		Items      []itemOut `json:"items"`
		Tips       string    `json:"tips"`
	}{Summary: rec.Summary, TotalPrice: rec.TotalPrice, Items: items, Tips: rec.Tips}
	b, err := json.Marshal(out)
	if err != nil {
		return `{"error":"方案序列化失败"}`
	}
	return string(b)
}

func firstExistingPath(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return paths[0]
}

// loadEnv 简单解析 .env，不引第三方依赖；已存在的环境变量不覆盖。
func loadEnv(path string) {
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
