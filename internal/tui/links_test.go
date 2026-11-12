package tui

// 链接兜底：商品和链接来自工具返回，模型偶尔会把 url 吃掉。
// 这时界面要自己把链接块渲染出来，保证用户永远拿得到可打开的链接。

import (
	"strings"
	"testing"

	"outfit-agent/internal/models"
)

func withProducts(m Model) Model {
	m.products = []models.Product{
		{ID: "w-top-007", Name: "黑白条纹薄款短款针织开衫", Category: "上衣", Price: 89, URL: "https://item.m.jd.com/product/10235009849662.html"},
		{ID: "w-casual-pdd-001", Name: "高腰阔腿牛仔裤", Category: "裤装", Price: 89, URL: "https://mobile.yangkeduo.com/search_result.html?search_key=x"},
	}
	m.messages = append(m.messages, chatMessage{Role: "assistant", Content: "黑白条纹针织开衫（89元）配高腰阔腿牛仔裤（89元），一共178。"})
	m.refreshViewport() // 真实程序里刷新由 Update 负责，测试里手动补一次
	return m
}

func TestLinkListRenderedWhenModelOmitsLinks(t *testing.T) {
	m := withProducts(newSizedModel(t, 100, 40))
	view := m.View()
	if !strings.Contains(view, "item.m.jd.com") || !strings.Contains(view, "mobile.yangkeduo.com") {
		t.Fatalf("模型没给链接时应由本地商品数据补出链接块，实际界面：\n%s", view)
	}
	if !strings.Contains(view, "/open 序号") {
		t.Fatalf("链接块应提示 /open 用法，实际界面：\n%s", view)
	}
}

func TestLinkListNotDuplicatedWhenModelGivesLinks(t *testing.T) {
	m := withProducts(newSizedModel(t, 100, 40))
	m.messages[len(m.messages)-1].Content += "\nhttps://item.m.jd.com/product/10235009849662.html"
	m.refreshViewport()
	view := m.View()
	if strings.Contains(view, "链接（/open 序号") {
		t.Fatalf("模型已经给了链接，不应再重复渲染链接块：\n%s", view)
	}
}

func TestNoLinkListWithoutPlan(t *testing.T) {
	m := newSizedModel(t, 100, 40)
	m.messages = append(m.messages, chatMessage{Role: "assistant", Content: "还没有方案，先告诉我你的需求～"})
	m.refreshViewport()
	if strings.Contains(m.View(), "链接（/open 序号") {
		t.Fatal("没有方案时不应出现链接块")
	}
}
