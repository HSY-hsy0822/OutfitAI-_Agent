package models

//用户信息
type UserProfile struct {
	Gender   string
	Age      string
	Height   string
	Weight   string
	BodyType string
	SkinTone string
	Style    string
	Occasion string
	Season   string
	Budget   string
}


//商品信息
type Product struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Price       float64  `json:"price"`
	Style       []string `json:"style"`
	Season      []string `json:"season"`
	Occasion    []string `json:"occasion"`
	URL         string   `json:"url"`
	Description string   `json:"description"`
	Platform    string   `json:"platform"` // 电商平台，如 jd、taobao
	LinkType    string   `json:"linkType"` // product=具体商品页，search=关键词搜索页
	Gender      string   `json:"gender"`   // 男、女、中性
}

// OutfitItem 是这套搭配里的单件。
// Product 为 nil 表示这一件没能在本地衣橱库匹配到商品（只展示建议，没有链接）。
type OutfitItem struct {
	Category string
	Reason   string
	Product  *Product
}

// Recommendation 是一套完整搭配：单品清单 + 整套理由 + 合计价。
type Recommendation struct {
	Summary    string
	Items      []OutfitItem
	Reason     string
	TotalPrice float64
	OverBudget bool
	Tips       string
}
