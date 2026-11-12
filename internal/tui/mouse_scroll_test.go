package tui

// 验证对话区滚动：鼠标滚轮（MouseMsg）和键盘上下键都应改变 viewport 的 YOffset。
// 用一个明显超出可视高度的长内容，保证有滚动空间。

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newScrollableModel(t *testing.T) Model {
	t.Helper()
	m := New()
	m.viewport.Width = 76
	m.viewport.Height = 10
	m.viewport.SetContent(strings.Repeat("对话历史行\n", 60))
	if m.viewport.YOffset != 0 {
		t.Fatal("初始滚动位置应在顶部")
	}
	return m
}

func TestMouseWheelScroll(t *testing.T) {
	m := newScrollableModel(t)

	// 滚轮向下：viewport 收到 MouseMsg 后 YOffset 应增加（默认每次滚 3 行）。
	down := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
	next, cmd := m.Update(down)
	m = next.(Model)
	if m.viewport.YOffset <= 0 {
		t.Fatalf("滚轮向下应离开顶部，YOffset=%d", m.viewport.YOffset)
	}
	if cmd != nil {
		t.Fatal("普通滚动不需要返回命令")
	}

	// 滚轮向上：应回到顶部。
	up := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp}
	next, _ = m.Update(up)
	m = next.(Model)
	if m.viewport.YOffset != 0 {
		t.Fatalf("滚轮向上应回到顶部，YOffset=%d", m.viewport.YOffset)
	}
}

func TestKeyboardScroll(t *testing.T) {
	m := newScrollableModel(t)

	// 方向键向下：KeyMsg 经 Update 转发给 viewport，YOffset 增加 1。
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.viewport.YOffset != 1 {
		t.Fatalf("按向下键应滚动 1 行，YOffset=%d", m.viewport.YOffset)
	}

	// 方向键向上：回到 0。
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)
	if m.viewport.YOffset != 0 {
		t.Fatalf("按向上键应回到顶部，YOffset=%d", m.viewport.YOffset)
	}
}
