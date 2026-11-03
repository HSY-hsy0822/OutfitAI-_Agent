package mcp

// 端到端测试：用 NewInMemoryTransports 在进程内连通 MCP server 和 client，
// 真实走一遍 JSON-RPC 协议（initialize → tools/list → tools/call），不依赖网络、不依赖大模型。
// 测试环境没有 LLM_API_KEY，agent.Recommend 会自动回退本地规则，结果稳定可复算。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"outfit-agent/internal/models"
	"outfit-agent/internal/repository"
)

const wardrobePath = "../../data/wardrobe.json"

// newConnectedSession 加载真实衣橱，启动 in-memory server 并返回已连接的 client session。
func newConnectedSession(t *testing.T) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ctx := context.Background()

	w, err := repository.LoadWardrobe(wardrobePath)
	if err != nil {
		t.Fatal("加载衣橱失败：", err)
	}
	srv := NewServerWithWardrobe(w)
	cli := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal("server 连接失败：", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	cs, err := cli.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal("client 连接失败：", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return ctx, cs
}

// decode 把工具返回的第一个文本块（JSON）解析成指定输出类型。
func decode[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	if res.IsError {
		t.Fatal("工具返回了业务错误")
	}
	if len(res.Content) == 0 {
		t.Fatal("结果没有 content")
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("第一个 content 不是文本，实际 %T", res.Content[0])
	}
	var out T
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatal("解析工具输出 JSON 失败：", err, "\n原文：", text.Text)
	}
	return out
}

func TestListTools(t *testing.T) {
	ctx, cs := newConnectedSession(t)

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal("ListTools 失败：", err)
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	if len(res.Tools) != 2 || !names["recommend_outfits"] || !names["search_wardrobe"] {
		t.Fatalf("应注册 recommend_outfits 和 search_wardrobe 两个工具，实际：%v", names)
	}
}

func TestRecommendTool(t *testing.T) {
	ctx, cs := newConnectedSession(t)

	// 预算 500 的甜美约会秋装：本地规则穷举失败后走 cheapestEach 兜底（裙装+鞋+外套各取最便宜）。
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "recommend_outfits",
		Arguments: RecommendInput{
			Gender: "女", Age: "20", Height: "163", Weight: "55",
			Style: "甜美", Occasion: "约会", Season: "秋季", Budget: "500",
		},
	})
	if err != nil {
		t.Fatal("CallTool 失败：", err)
	}
	out := decode[RecommendOutput](t, res)

	if len(out.Items) < 3 {
		t.Fatalf("兜底方案应至少含裙装、鞋、外套 3 件，实际 %d 件", len(out.Items))
	}

	// 每件都必须匹配到本地真实商品：有名字、有价、有链接，不允许模型编造。
	var sum float64
	for _, it := range out.Items {
		if it.Name == "" || it.Price <= 0 || it.URL == "" {
			t.Fatalf("单品未匹配到本地商品：%+v", it)
		}
		sum += it.Price
	}
	if sum != out.TotalPrice {
		t.Fatalf("总价应等于各件之和：各件 %.0f，总价 %.0f", sum, out.TotalPrice)
	}

	// 预算标记必须自洽：超预算时 over_budget=true，且确实超过 500。
	if out.OverBudget && out.TotalPrice <= 500 {
		t.Fatal("总价没超 500 却标了 over_budget")
	}
	if !out.OverBudget && out.TotalPrice > 500 {
		t.Fatal("总价超过 500 却没标 over_budget")
	}
	if out.Summary == "" || out.Tips == "" {
		t.Fatal("方案摘要和提示不应为空")
	}
}

// TestApplyCheaperRequest 直接验证"换便宜"规则（同包可测私有函数），结果完全确定。
func TestApplyCheaperRequest(t *testing.T) {
	w, err := repository.LoadWardrobe(wardrobePath)
	if err != nil {
		t.Fatal("加载衣橱失败：", err)
	}
	p := models.UserProfile{Gender: "女"}

	// 用一双 299 的女鞋作为初始方案，要求换便宜。
	shoe := models.Product{ID: "w-shoe-001", Name: "啄木鸟黑色粗跟单鞋", Category: "鞋", Price: 299, Gender: "女"}
	rec := models.Recommendation{
		Items:      []models.OutfitItem{{Category: "鞋", Product: &shoe}},
		TotalPrice: 299,
	}

	got := applyCheaperRequest(rec, p, w.Products, "鞋子要便宜点")
	newShoe := got.Items[0].Product
	if newShoe.Price >= 299 {
		t.Fatalf("应替换成更便宜的女鞋，实际价格 %.0f", newShoe.Price)
	}
	if !strings.EqualFold(newShoe.Category, "鞋") {
		t.Fatal("替换品必须仍是鞋类")
	}
	if got.TotalPrice != newShoe.Price {
		t.Fatalf("总价应同步为新鞋价 %.0f，实际 %.0f", newShoe.Price, got.TotalPrice)
	}

	// 与"便宜"无关的要求不应触发替换（用全新的方案，避免切片底层数组被上一次调用改写）。
	fresh := models.Recommendation{
		Items:      []models.OutfitItem{{Category: "鞋", Product: &shoe}},
		TotalPrice: 299,
	}
	same := applyCheaperRequest(fresh, p, w.Products, "想要红色的")
	if same.Items[0].Product.Price != 299 {
		t.Fatal("非便宜类要求不应替换商品")
	}
}

func TestSearchWardrobe(t *testing.T) {
	ctx, cs := newConnectedSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_wardrobe",
		Arguments: SearchInput{Category: "鞋", Gender: "女", Limit: 5},
	})
	if err != nil {
		t.Fatal("CallTool 失败：", err)
	}
	out := decode[SearchOutput](t, res)

	if out.Total == 0 {
		t.Fatal("女鞋检索应返回结果")
	}
	if len(out.Products) > 5 {
		t.Fatalf("limit=5 未生效，返回了 %d 条", len(out.Products))
	}
	for _, p := range out.Products {
		if !strings.EqualFold(p.Category, "鞋") {
			t.Fatalf("返回了非鞋类商品：%+v", p)
		}
		if p.Name == "" || p.URL == "" || p.Price <= 0 {
			t.Fatalf("商品信息不完整：%+v", p)
		}
	}

	// 不存在的关键词：返回空清单而不是报错。
	res2, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_wardrobe",
		Arguments: SearchInput{Keyword: "绝对不存在的商品名xyz123"},
	})
	if err != nil {
		t.Fatal("空结果不应报错：", err)
	}
	out2 := decode[SearchOutput](t, res2)
	if out2.Total != 0 || len(out2.Products) != 0 {
		t.Fatal("不存在的关键词应返回 0 条")
	}
}

func TestUnknownTool(t *testing.T) {
	ctx, cs := newConnectedSession(t)
	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "not_exist_tool"}); err == nil {
		t.Fatal("调用未知工具应返回错误")
	}
}
