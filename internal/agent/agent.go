package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"outfit-agent/internal/models"
	"outfit-agent/internal/repository"
)

type Agent struct{ LLM *LLMClient }

func New() *Agent { return &Agent{LLM: NewLLMClientFromEnv()} }

// Recommend 先让豆包给一套搭配；只要它没配好、超时、返回的不是合法 JSON，
// 或者给的商品名在本地衣橱库里匹配不到，就整段退回本地规则。
// 它永远返回一个能显示的 Recommendation，不会 panic、也不返回错误。
func (a *Agent) Recommend(p models.UserProfile, candidates []models.Product) models.Recommendation {
	if a.LLM != nil {
		if raw, err := a.LLM.SuggestOutfit(p, candidates); err == nil {
			if rec, ok := parseOutfit(raw, candidates, p); ok {
				return rec
			}
		}
	}
	return localOutfit(p, candidates)
}

// ---------- 豆包返回结果的解析 ----------

// parseOutfit 解析豆包返回的 JSON，并把每件的 keyword 匹配到本地商品。
// 返回 ok=false 时调用方回退本地规则。
func parseOutfit(raw string, candidates []models.Product, p models.UserProfile) (models.Recommendation, bool) {
	text := extractJSON(raw)
	if text == "" {
		return models.Recommendation{}, false
	}
	var payload struct {
		Summary string `json:"summary"`
		Reason  string `json:"reason"`
		Tips    string `json:"tips"`
		Items   []struct {
			Category string `json:"category"`
			Keyword  string `json:"keyword"`
			Reason   string `json:"reason"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return models.Recommendation{}, false
	}

	rec := models.Recommendation{
		Summary: strings.TrimSpace(payload.Summary),
		Reason:  strings.TrimSpace(payload.Reason),
		Tips:    strings.TrimSpace(payload.Tips),
	}
	matched := 0
	for _, it := range payload.Items {
		item := models.OutfitItem{Category: strings.TrimSpace(it.Category), Reason: strings.TrimSpace(it.Reason)}
		if pr, ok := matchProduct(item.Category, it.Keyword, candidates); ok {
			item.Product = &pr
			rec.TotalPrice += pr.Price
			matched++
		}
		rec.Items = append(rec.Items, item)
	}
	if len(rec.Items) == 0 || matched < 2 {
		return models.Recommendation{}, false
	}

	budget := repository.ParseBudget(p.Budget)
	if budget > 0 && rec.TotalPrice > budget {
		rec.OverBudget = true
	}
	if rec.Summary == "" {
		rec.Summary = "豆包生成的搭配"
	}
	if rec.Tips == "" {
		rec.Tips = "商品名称、价格和跳转链接均来自本地衣橱库，不由大模型编造。"
	}
	return rec, true
}

// extractJSON 从模型输出里截出 JSON 主体，容忍 ```json 代码块和前后废话。
func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ""
	}
	return raw[start : end+1]
}

// matchProduct 把模型给的关键词匹配到本地商品：
// 品类一致且名称有明显重叠才算命中，避免张冠李戴。
func matchProduct(category, keyword string, candidates []models.Product) (models.Product, bool) {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		return models.Product{}, false
	}
	tokens := strings.FieldsFunc(kw, func(r rune) bool {
		return r == ' ' || r == '　' || r == '/' || r == ',' || r == '，' || r == '、' || r == '·' || r == '-'
	})

	best := models.Product{}
	bestScore := 0
	for _, c := range candidates {
		name := strings.ToLower(c.Name)
		score := 0
		categoryOK := strings.TrimSpace(category) != "" &&
			strings.EqualFold(strings.TrimSpace(c.Category), strings.TrimSpace(category))
		if categoryOK {
			score += 5
		}
		if strings.Contains(name, kw) || strings.Contains(kw, name) {
			score += 10
		}
		for _, t := range tokens {
			if len([]rune(t)) >= 2 && strings.Contains(name, t) {
				score += 3
			}
		}
		if categoryOK && strings.Contains(strings.ToLower(c.Category), kw) {
			score += 2
		}
		if score > bestScore || (score == bestScore && score > 0 && best.ID != "" && c.Price < best.Price) {
			bestScore, best = score, c
		}
	}
	if bestScore < 8 {
		return models.Product{}, false
	}
	return best, true
}

// ---------- 本地规则：挑一套搭配 ----------

// slot 是搭配里的一个位置（品类）；essential 表示缺少它这套就不成立。
type slot struct {
	category  string
	essential bool
}

// localOutfit 挑一套完整搭配：
//  1. 必需位置（上衣 + 下装 + 鞋，秋冬再加外套）各取候选前 8 件；
//  2. 穷举组合，挑出"总价不超预算、匹配分最高"的一套——这样不会出现钱花在
//     上衣上、结果鞋子买不起的情况；
//  3. 预算还有余量时，再补包和配饰（且只补和风格/场合对得上的）。
func localOutfit(p models.UserProfile, candidates []models.Product) models.Recommendation {
	pool := make([]models.Product, 0, len(candidates))
	for _, c := range candidates {
		if repository.GenderOK(c.Gender, p.Gender) {
			pool = append(pool, c)
		}
	}
	if len(pool) == 0 {
		return models.Recommendation{
			Summary: "衣橱库里没有适合的商品",
			Tips:    "请检查 data/wardrobe.json 里商品的 gender 字段。",
		}
	}

	budget := repository.ParseBudget(p.Budget)
	required, optional := splitSlots(planSlots(p))

	lists := make([][]models.Product, 0, len(required))
	for _, s := range required {
		lists = append(lists, topN(p, pool, s.category, 8))
	}
	picked, ok := bestCombo(p, lists, budget)
	if !ok {
		picked = cheapestEach(pool, required) // 预算实在不够：各品类取最便宜的
	}

	rec := models.Recommendation{Summary: "本地规则搭配（豆包未配置或调用失败时使用）"}
	used := map[string]bool{}
	for _, pr := range picked {
		rec.Items = append(rec.Items, newItem(p, pr))
		rec.TotalPrice += pr.Price
		used[pr.ID] = true
	}
	for _, s := range optional { // 预算还有余量才加
		if budget > 0 && rec.TotalPrice >= budget {
			break // 没余量了，别再加
		}
		limit := -1.0
		if budget > 0 {
			limit = budget - rec.TotalPrice
		}
		pr, ok := pickOne(p, pool, s.category, limit, used)
		if !ok || repository.Score(p, pr) < 5 {
			continue // 可选单品至少要跟风格或场合对得上，宁可不加
		}
		rec.Items = append(rec.Items, newItem(p, pr))
		rec.TotalPrice += pr.Price
		used[pr.ID] = true
	}

	if len(rec.Items) == 0 {
		rec.Summary = "衣橱库里没有匹配到合适的商品"
		rec.Tips = "请检查 data/wardrobe.json 的分类（上衣/裤装/裙装/外套/鞋/包/配饰）是否完整。"
		return rec
	}
	if budget > 0 && rec.TotalPrice > budget {
		rec.OverBudget = true
	}
	rec.Reason = outfitReason(p, rec)
	rec.Tips = "商品名称、价格和跳转链接全部来自本地衣橱库；预算是整套总价。"
	return rec
}

// planSlots 按用户资料决定这套搭配要哪些位置。
// 裙装方案里上衣是可选的：连衣裙本身就成套，半身裙配上衣则看预算有没有余量。
func planSlots(p models.UserProfile) []slot {
	female := mentions(p.Gender, "女")
	dressy := female && (mentions(p.Style, "甜美", "御姐", "轻熟", "日系") || mentions(p.Occasion, "约会"))
	cold := mentions(p.Season, "秋", "冬")

	out := make([]slot, 0, 6)
	if dressy {
		out = append(out, slot{"裙装", true}, slot{"鞋", true}, slot{"上衣", false})
	} else {
		out = append(out, slot{"上衣", true}, slot{"裤装", true}, slot{"鞋", true})
	}
	if cold {
		out = append(out, slot{"外套", true}) // 秋冬不穿外套不成套
	}
	return append(out, slot{"包", false}, slot{"配饰", false})
}

// splitSlots 把位置分成"必需"和"可选"两组。
func splitSlots(slots []slot) (required, optional []slot) {
	for _, s := range slots {
		if s.essential {
			required = append(required, s)
		} else {
			optional = append(optional, s)
		}
	}
	return required, optional
}

// topN 取某个品类里匹配分最高的前 n 件（同分挑便宜的）。
func topN(p models.UserProfile, pool []models.Product, category string, n int) []models.Product {
	list := make([]models.Product, 0, len(pool))
	for _, pr := range pool {
		if strings.EqualFold(strings.TrimSpace(pr.Category), strings.TrimSpace(category)) {
			list = append(list, pr)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		si, sj := repository.Score(p, list[i]), repository.Score(p, list[j])
		if si != sj {
			return si > sj
		}
		return list[i].Price < list[j].Price
	})
	if n > 0 && len(list) > n {
		list = list[:n]
	}
	return list
}

// bestCombo 穷举各位置的候选，找出"总价不超预算且总分最高"的组合。
// 位置数量只有 3~4 个、每个位置最多 8 件，穷举开销可以忽略。
func bestCombo(p models.UserProfile, lists [][]models.Product, budget float64) ([]models.Product, bool) {
	bestScore, bestPrice := -1, 0.0
	var best []models.Product

	var walk func(i int, cur []models.Product, score int, price float64)
	walk = func(i int, cur []models.Product, score int, price float64) {
		if i == len(lists) {
			if budget > 0 && price > budget {
				return
			}
			if score > bestScore || (score == bestScore && (best == nil || price < bestPrice)) {
				bestScore, bestPrice = score, price
				best = append([]models.Product(nil), cur...)
			}
			return
		}
		for _, pr := range lists[i] {
			walk(i+1, append(cur, pr), score+repository.Score(p, pr), price+pr.Price)
		}
	}
	walk(0, nil, 0, 0)
	return best, best != nil
}

// cheapestEach 是预算不足时的兜底：每个必需品类取价格最低的一件。
func cheapestEach(pool []models.Product, slots []slot) []models.Product {
	out := make([]models.Product, 0, len(slots))
	for _, s := range slots {
		best := models.Product{}
		for _, pr := range pool {
			if !strings.EqualFold(strings.TrimSpace(pr.Category), strings.TrimSpace(s.category)) {
				continue
			}
			if best.ID == "" || pr.Price < best.Price {
				best = pr
			}
		}
		if best.ID != "" {
			out = append(out, best)
		}
	}
	return out
}

// pickOne 在某个品类里挑一件：匹配分最高优先，同分挑便宜的；limit 为负表示不限价，used 里的会被跳过。
func pickOne(p models.UserProfile, pool []models.Product, category string, limit float64, used map[string]bool) (models.Product, bool) {
	list := make([]models.Product, 0, len(pool))
	for _, pr := range pool {
		if used[pr.ID] {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(pr.Category), strings.TrimSpace(category)) {
			continue
		}
		if limit >= 0 && pr.Price > limit {
			continue
		}
		list = append(list, pr)
	}
	if len(list) == 0 {
		return models.Product{}, false
	}
	sort.SliceStable(list, func(i, j int) bool {
		si, sj := repository.Score(p, list[i]), repository.Score(p, list[j])
		if si != sj {
			return si > sj
		}
		return list[i].Price < list[j].Price
	})
	return list[0], true
}

// newItem 把商品包成搭配里的一件，并生成"为什么选它"。
func newItem(p models.UserProfile, pr models.Product) models.OutfitItem {
	item := models.OutfitItem{Category: pr.Category, Reason: itemReason(p, pr)}
	item.Product = &pr
	return item
}

// itemReason 说明"为什么选这一件"。
func itemReason(p models.UserProfile, pr models.Product) string {
	why := make([]string, 0, 3)
	if strings.TrimSpace(p.Style) != "" && repository.MatchTags(pr.Style, p.Style) {
		why = append(why, "对上你选的"+p.Style+"风格")
	}
	if strings.TrimSpace(p.Occasion) != "" && repository.MatchTags(pr.Occasion, p.Occasion) {
		why = append(why, "适合"+p.Occasion)
	}
	if strings.TrimSpace(p.Season) != "" && repository.MatchTags(pr.Season, p.Season) {
		why = append(why, p.Season+"能穿")
	}
	if len(why) == 0 {
		why = append(why, "基础百搭，和这套其它单品都好配")
	}
	return strings.Join(why, "、") + "。"
}

// outfitReason 生成整套搭配的理由。
func outfitReason(p models.UserProfile, rec models.Recommendation) string {
	style := strings.TrimSpace(p.Style)
	if style == "" {
		style = "简约"
	}
	occasion := strings.TrimSpace(p.Occasion)
	if occasion == "" {
		occasion = "日常"
	}
	season := strings.TrimSpace(p.Season)
	if season == "" {
		season = "当季"
	}
	return fmt.Sprintf("整套走%s路线，面向%s、%s：共 %d 件，合计约 ¥%.0f。上装与下装保持深浅平衡，鞋和包尽量同色系，整体更干净利落。",
		style, occasion, season, len(rec.Items), rec.TotalPrice)
}

// mentions 判断输入里是否出现了任一关键词（大小写不敏感）。
func mentions(input string, words ...string) bool {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return false
	}
	for _, w := range words {
		if strings.Contains(input, strings.ToLower(w)) {
			return true
		}
	}
	return false
}
