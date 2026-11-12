package tui

// 回归测试：对话区滚动 + 自动贴底 + 布局高度。
// 之前的三处真实故障：
//   1. SetContent 被写在 View()（值接收者）里，真实 viewport 内容永远是空的 → YOffset 恒为 0，滚不动；
//   2. GotoBottom 在 SetContent 之前调用，按旧内容高度算偏移 → 新回答的下半截留在屏幕外；
//   3. View() 行数正好等于终端高度 → 每帧把顶部标题挤出屏幕；思考中还不让按 ↑↓/PgUp/PgDn。

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newSizedModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := New()
	if m.errMsg != "" {
		t.Skip("衣橱未加载，跳过：" + m.errMsg)
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return next.(Model)
}

func longHistory(m Model) Model {
	m.messages = append(m.messages,
		chatMessage{Role: "user", Content: "第一轮问题"},
		chatMessage{Role: "assistant", Content: strings.Repeat("很早以前的回答，用来占满屏幕。\n", 40)},
	)
	return m
}

// 内容真的写进了 viewport，并且新内容到达后会自动贴到底部。
func TestViewportHoldsContentAndFollowsBottom(t *testing.T) {
	m := longHistory(newSizedModel(t, 100, 30))
	m.follow = true
	m.refreshViewport()

	if m.viewport.TotalLineCount() <= m.viewport.Height {
		t.Fatalf("viewport 内容没写进去：总行数=%d，可视高度=%d", m.viewport.TotalLineCount(), m.viewport.Height)
	}

	m.messages = append(m.messages,
		chatMessage{Role: "user", Content: "最新发出去的一句"},
		chatMessage{Role: "assistant", Content: "最新的回答尾巴-TAIL"},
	)
	m.refreshViewport()

	view := m.View()
	if !strings.Contains(view, "最新发出去的一句") {
		t.Fatalf("最新的用户消息应该在可视区里，实际界面：\n%s", view)
	}
	if !strings.Contains(view, "最新的回答尾巴-TAIL") {
		t.Fatalf("最新回答的结尾应该在可视区里，实际界面：\n%s", view)
	}
	if !m.viewport.AtBottom() {
		t.Fatalf("新内容到达后应贴到底部，YOffset=%d", m.viewport.YOffset)
	}
}

// ↑↓/PgUp/PgDn 必须能在"思考中"翻历史；用户自己往上翻之后不应被强行拽回底部。
func TestScrollKeysWorkWhileBusy(t *testing.T) {
	m := longHistory(newSizedModel(t, 100, 30))
	m.follow = true
	m.refreshViewport()
	bottom := m.viewport.YOffset
	if bottom == 0 {
		t.Fatal("前置条件：历史应已超出可视高度")
	}

	m.busy = true // 正在等模型返回
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = next.(Model)
	if m.viewport.YOffset >= bottom {
		t.Fatalf("思考中按 PgUp 应该能往上翻，YOffset=%d（之前=%d）", m.viewport.YOffset, bottom)
	}
	if m.follow {
		t.Fatal("用户手动往上翻后不应继续自动贴底")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(Model)
	if m.viewport.YOffset != bottom {
		t.Fatalf("按 PgDown 应该能回到原来的位置，YOffset=%d（期望 %d）", m.viewport.YOffset, bottom)
	}
}

// 在输入框里打字（尤其是 j/k/d/u/g 这些字母）不能再顺便滚动对话区。
func TestTypingDoesNotScroll(t *testing.T) {
	m := longHistory(newSizedModel(t, 100, 30))
	m.follow = true
	m.refreshViewport()
	before := m.viewport.YOffset

	for _, r := range "judge 这句话里有 g、j、d、u、k" {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	if m.viewport.YOffset != before {
		t.Fatalf("打字不应该改变滚动位置：%d → %d", before, m.viewport.YOffset)
	}
	if !strings.Contains(m.textInput.Value(), "judge") {
		t.Fatalf("输入框应该收到按键，实际值：%q", m.textInput.Value())
	}
}

// View() 的行数必须严格小于终端高度、每行的显示宽度不能超过终端宽度，
// 否则首行（标题）会被挤出可视区域，或者边框被终端自动折行弄乱。
func TestViewFitsTerminalHeight(t *testing.T) {
	cases := []struct{ w, h int }{{120, 20}, {120, 30}, {120, 45}, {80, 24}, {70, 24}}
	for _, c := range cases {
		m := longHistory(newSizedModel(t, c.w, c.h))
		m.follow = true
		m.refreshViewport()
		view := m.View()

		lines := strings.Count(view, "\n") + 1
		if lines >= c.h {
			t.Fatalf("%dx%d：View() 渲染了 %d 行，必须小于终端高度，否则标题会被顶掉", c.w, c.h, lines)
		}
		if !strings.Contains(view, "穿搭推荐 Agent") {
			t.Fatalf("%dx%d：标题不见了", c.w, c.h)
		}
		for _, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > c.w {
				t.Fatalf("%dx%d：有一行显示宽度 %d 超过终端宽度：%q", c.w, c.h, w, line)
			}
		}
	}
}
