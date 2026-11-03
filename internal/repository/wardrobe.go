package repository

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"outfit-agent/internal/models"
)

type Wardrobe struct{ Products []models.Product }

func LoadWardrobe(path string) (*Wardrobe, error) {
	data, err := os.ReadFile(path)	//读取文件
	if err != nil {
		return nil, fmt.Errorf("读取衣橱库失败：%w", err)
	}
	var products []models.Product
	if err := json.Unmarshal(data, &products); err != nil {	
		//作用：把 JSON 格式的数据解析到 Go 变量或结构体中。
		return nil, fmt.Errorf("解析 wardrobe.json 失败：%w", err)
	}
	if len(products) == 0 {
		return nil, fmt.Errorf("衣橱库为空")
	}
	for i, p := range products {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Name) == "" {
			return nil, fmt.Errorf("第 %d 条商品缺少 id 或 name", i+1)
		}
	}
	return &Wardrobe{Products: products}, nil
}

// Score 是单件商品的匹配分：风格 4 分、季节 3 分、场景 2 分。
func Score(p models.UserProfile, product models.Product) int {
	score := 0
	if MatchTags(product.Style, p.Style) {
		score += 4
	}
	if MatchTags(product.Season, p.Season) {
		score += 3
	}
	if MatchTags(product.Occasion, p.Occasion) {
		score += 2
	}
	return score
}

// Candidates 返回交给搭配算法的候选池：先按性别过滤，再按匹配分降序、同分按价格升序，取前 limit 件。
// 这里刻意不按预算砍单件——预算是"整套"的约束，交给 stylist 去分配。
func (w *Wardrobe) Candidates(p models.UserProfile, limit int) []models.Product {
	type item struct {
		p     models.Product
		score int
	}
	items := make([]item, 0, len(w.Products))
	for _, product := range w.Products {
		if !GenderOK(product.Gender, p.Gender) {
			continue
		}
		items = append(items, item{product, Score(p, product)})
	}
	//SliceStable稳定排序
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].p.Price < items[j].p.Price
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	out := make([]models.Product, 0, len(items))
	for _, it := range items {
		out = append(out, it.p)
	}
	return out
}

// MatchTags 判断商品标签与用户输入是否匹配（双向包含，用户多打几个字也能匹配上）。
func MatchTags(tags []string, input string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return false
	}
	for _, tag := range tags {
		tag = strings.ToLower(tag)
		//Contain判断字符串是否包含指定内容
		if strings.Contains(input, tag) || strings.Contains(tag, input) {
			return true
		}
	}
	return false
}

// normalizeGender 把用户输入的“男/男生/男士/女/女生/女士”收敛成“男”“女”或空。
func normalizeGender(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return ""
	case strings.Contains(s, "男"):
		return "男"
	case strings.Contains(s, "女"):
		return "女"
	default:
		return ""
	}
}

// genderMatches 判断商品是否适合该性别；商品性别为空或“中性”时一律放行。
func genderMatches(productGender, want string) bool {
	if want == "" {
		return true
	}
	g := normalizeGender(productGender)
	if g == "" {
		return true
	}
	return g == want
}

// GenderOK 是给其它包用的性别判断入口。
func GenderOK(productGender, rawInput string) bool {
	return genderMatches(productGender, normalizeGender(rawInput))
}

// ParseBudget 把“¥500 元”“1,200”这类输入洗成数字；返回 0 表示不限。
func ParseBudget(s string) float64 {
	//创建一个字符串替换器，可以一次性替换多个字符串内容。
	s = strings.NewReplacer("￥", "", "¥", "", ",", "", "元", "", " ", "").Replace(s)
	var n float64
	_, _ = fmt.Sscanf(s, "%f", &n)
	return n
}
