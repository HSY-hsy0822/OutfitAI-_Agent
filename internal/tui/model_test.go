package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// 模拟键盘输入一整句话并回车，然后执行后台命令，验证 Bubble Tea 的完整更新链路。
func TestMultiTurnFlow(t *testing.T) {
	m := New()
	if m.errMsg != "" {
		t.Skip("衣橱未加载，跳过：" + m.errMsg)
	}

	// 第一轮：输入"你好，我是思雅"并回车
	m, cmd := feed(t, m, "你好，我是思雅")
	if cmd == nil || !m.busy {
		t.Fatal("回车后应进入等待状态并返回后台命令")
	}
	m = runPendingCmd(t, m, cmd)
	if !strings.Contains(m.View(), "思雅") {
		t.Fatalf("界面应包含用户输入，实际：\n%s", m.View())
	}

	// 第二轮：验证记忆——问"你记得我叫什么吗"
	m, cmd = feed(t, m, "你记得我叫什么吗")
	m = runPendingCmd(t, m, cmd)
	if !strings.Contains(m.View(), "思雅") {
		t.Fatalf("第二轮回答应记得名字，实际：\n%s", m.View())
	}
	t.Log("多轮对话与记忆验证通过，最终界面：\n" + m.View())
}

// feed 模拟输入字符串然后按回车，返回新模型和回车产生的后台命令。
func feed(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	m = m2.(Model)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return m2.(Model), cmd
}

// runPendingCmd 执行后台命令，并把返回的 responseMsg 喂回 Update。
func runPendingCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	msg := cmd()
	resp, ok := msg.(responseMsg)
	if !ok {
		t.Fatalf("期望 responseMsg，得到 %T", msg)
	}
	if resp.err != nil {
		t.Fatal("后台命令出错：" + resp.err.Error())
	}
	m2, _ := m.Update(resp)
	return m2.(Model)
}
