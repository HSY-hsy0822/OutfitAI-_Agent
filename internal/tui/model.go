package tui

// Bubble Tea 的三件套都在这里：Init（启动）、Update（处理消息）、View（渲染界面）。
// 交互层使用官方 bubbles 组件：textinput（输入框）、viewport（可滚动对话区）、spinner（等待动画）。
// 大模型协议、工具调用、搭配算法都在 client.go / helpers.go / agent 包，本文件不改动那些逻辑。

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"outfit-agent/internal/agent"
	"outfit-agent/internal/models"
	"outfit-agent/internal/repository"
	"outfit-agent/internal/utils"
)

// ---------- 界面样式（暖色系，不使用紫/靛色） ----------

var (
	userLabel = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4EC9B0")).
			Render("你")

	assistantLabel = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFD580")).
			Render("助手")

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")).
			Bold(true)

	noticeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9AA7B0"))

	conversationBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#6C9A8B")).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9AA7B0"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D8890"))

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFD580"))
)

// Model 是 Bubble Tea 的状态：保存对话历史、输入框、滚动区、依赖等。
type Model struct {
	client   *chatClient
	wardrobe *repository.Wardrobe
	stylist  *agent.Agent

	messages []chatMessage // 全部历史 = 记忆
	busy     bool          // 是否正在等模型
	errMsg   string
	products []models.Product // 最近一套方案的商品，供 /open、/search

	textInput textinput.Model
	viewport  viewport.Model
	spinner   spinner.Model

	width, height int
	ready         bool // 收到第一次 WindowSizeMsg 后才开始按窗口尺寸渲染
}

// responseMsg 是后台命令跑完一轮后发回的消息。
type responseMsg struct {
	messages []chatMessage
	products []models.Product
	err      error
}

// New 加载配置和衣橱，构造初始 Model。
func New() Model {
	loadEnv(firstExistingPath(".env", "../.env", "../../.env"))

	systemPrompt := "你是一个中文穿搭顾问 Agent，通过自然语言多轮聊天帮用户搭配衣服。\n" +
		"规则：\n" +
		"1. 像朋友聊天一样了解需求，每次只问最关键的 1~2 个问题，不要一次罗列所有问题；\n" +
		"2. 用户想要推荐、且关键信息基本明确时，调用 recommend_outfits 工具，严禁编造商品名、价格和链接；\n" +
		"3. 拿到工具结果后，用大白话讲清整体思路、每件为什么选、价格和预算；\n" +
		"4. 用户不满意（太贵、想换风格、不喜欢某件）时，带着更新后的信息再次调用工具；\n" +
		"5. 只是问穿搭常识就正常回答，不用调用工具。"

	ti := textinput.New()
	ti.Placeholder = "说说你的穿搭需求，回车发送（斜杠命令如 /help）"
	ti.Prompt = "❯ "
	ti.Width = 74
	ti.Focus()

	// 先给一套 80x24 的默认布局，WindowSizeMsg 到达后再覆盖，保证任何环境下都能渲染。
	vp := viewport.New(76, 17)
	vp.SetContent("")

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD580"))

	m := Model{
		client:    newChatClientFromEnv(),
		stylist:   agent.New(),
		messages:  []chatMessage{{Role: "system", Content: systemPrompt}},
		textInput: ti,
		viewport:  vp,
		spinner:   sp,
		width:     80,
		height:    24,
		ready:     true,
	}
	w, err := repository.LoadWardrobe(firstExistingPath("data/wardrobe.json", "../data/wardrobe.json", "../../data/wardrobe.json"))
	if err != nil {
		m.errMsg = "衣橱库加载失败：" + err.Error()
		return m
	}
	m.wardrobe = w
	return m
}

// Init 启动时执行一次：让输入框光标闪烁、让 spinner 转起来。
func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

// Update 处理所有消息（窗口尺寸、键盘、spinner 节拍、模型响应），返回新状态和要执行的命令。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.ready = msg.Width, msg.Height, true
		// 高度预算：标题 1 + 对话边框 2 + 输入 1 + 状态 1 + 快捷键 1 + 余量 1
		m.viewport.Width = max(10, msg.Width-4)
		m.viewport.Height = max(5, msg.Height-7)
		m.textInput.Width = max(10, msg.Width-6)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case responseMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.viewport.GotoBottom()
			return m, nil
		}
		m.messages = msg.messages
		if msg.products != nil {
			m.products = msg.products
		}
		m.errMsg = ""
		m.viewport.GotoBottom() // 新消息到达后自动滚到最底
		return m, nil

	case tea.KeyMsg:
		// 任何时候都允许 Ctrl+C 退出
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		if m.busy {
			return m, nil // 思考中先不接受输入
		}
		if msg.Type == tea.KeyEnter {
			text := strings.TrimSpace(m.textInput.Value())
			if text == "" {
				return m, nil
			}
			m.textInput.Reset()
			if strings.HasPrefix(text, "/") {
				return m.handleCommand(text)
			}
			m.messages = append(m.messages, chatMessage{Role: "user", Content: text})
			m.busy = true
			m.errMsg = ""
			m.viewport.GotoBottom()
			return m, m.requestCmd(copyMessages(m.messages))
		}
		// 其余按键（左右移动、退格、上下翻页等）交给输入框和滚动区各自处理
		var tiCmd, vpCmd tea.Cmd
		m.textInput, tiCmd = m.textInput.Update(msg)
		m.viewport, vpCmd = m.viewport.Update(msg)
		return m, tea.Batch(tiCmd, vpCmd)
	}
	return m, nil
}

// handleCommand 处理斜杠命令。
func (m Model) handleCommand(line string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(line)
	switch parts[0] {
	case "/quit", "/exit":
		return m, tea.Quit

	case "/reset":
		m.messages = m.messages[:1] // 只留 system，清空记忆
		m.products = nil
		return m.withNotice("已清空对话记忆，重新开始。"), nil

	case "/help":
		return m.withNotice("命令：/reset 清空记忆 | /open 序号 打开链接 | /search 序号 搜同款 | /quit 退出"), nil

	case "/open", "/search":
		if len(m.products) == 0 {
			return m.withNotice("还没有方案，先跟我要一套穿搭。"), nil
		}
		idx := 0
		if len(parts) >= 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil {
				idx = n - 1
			}
		}
		if idx < 0 || idx >= len(m.products) {
			return m.withNotice(fmt.Sprintf("序号要在 1~%d 之间。", len(m.products))), nil
		}
		p := m.products[idx]
		target := p.URL
		if parts[0] == "/search" {
			target = utils.SearchURL(p.Platform, p.Name)
		}
		if err := utils.OpenBrowser(target); err != nil {
			return m.withNotice("打开失败：" + err.Error()), nil
		}
		return m.withNotice("已打开：" + p.Name), nil

	default:
		return m.withNotice("不认识的命令，输入 /help 查看。"), nil
	}
}

// requestCmd 是后台命令：在 goroutine 里完成完整一轮（含可能的多次工具调用），
// 不阻塞界面，跑完通过 responseMsg 把结果送回 Update。
func (m Model) requestCmd(history []chatMessage) tea.Cmd {
	client, wardrobe, stylist := m.client, m.wardrobe, m.stylist
	return func() tea.Msg {
		msgs := copyMessages(history)
		var products []models.Product
		for round := 0; round < 6; round++ {
			content, toolCalls, err := client.chat(msgs)
			if err != nil {
				return responseMsg{messages: msgs, err: err}
			}
			if len(toolCalls) == 0 {
				msgs = append(msgs, chatMessage{Role: "assistant", Content: content})
				return responseMsg{messages: msgs, products: products}
			}
			msgs = append(msgs, chatMessage{Role: "assistant", Content: content, ToolCalls: toolCalls})
			for _, tc := range toolCalls {
				result, prods := runRecommendTool(tc, wardrobe, stylist)
				if prods != nil {
					products = prods
				}
				msgs = append(msgs, chatMessage{Role: "tool", Name: tc.Function.Name, ToolCallID: tc.ID, Content: result})
			}
		}
		return responseMsg{messages: msgs, err: fmt.Errorf("工具调用轮次过多，换个说法试试")}
	}
}

// View 根据当前状态渲染整个界面，每次有变化都会重画。
func (m Model) View() string {
	if !m.ready {
		return "正在初始化终端界面…"
	}

	m.viewport.SetContent(m.renderConversation())
	header := headerStyle.Render("穿搭推荐 Agent · Bubble Tea 版")
	box := conversationBox.
		Width(m.viewport.Width + 2).
		Render(m.viewport.View())

	input := lipgloss.JoinVertical(lipgloss.Left, "", m.textInput.View())
	status := m.renderStatus()
	help := helpStyle.Render("Ctrl+C 退出 · /help 帮助 · /reset 清空 · /open 序号 打开 · /search 序号 搜同款 · ↑/↓ PgUp/PgDn 滚动")

	return lipgloss.JoinVertical(lipgloss.Left, header, box, input, status, help)
}

// renderConversation 把对话历史渲染成对话区文本。
func (m Model) renderConversation() string {
	contentWidth := max(10, m.viewport.Width-2)
	var b strings.Builder

	if m.wardrobe != nil && len(m.messages) == 1 {
		b.WriteString(noticeStyle.Width(contentWidth).Render(
			"你好，我是你的穿搭顾问。直接告诉我你的性别、风格、场合、季节和预算，我就会从本地衣橱里给你配一整套；也可以先随便聊聊。") + "\n\n")
	}

	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			b.WriteString(userLabel + "\n")
			b.WriteString(lipgloss.NewStyle().Width(contentWidth).Render(msg.Content) + "\n\n")
		case "assistant":
			if msg.Content != "" {
				b.WriteString(assistantLabel + "\n")
				b.WriteString(lipgloss.NewStyle().Width(contentWidth).Render(msg.Content) + "\n\n")
			}
		}
	}

	if m.busy {
		b.WriteString(m.spinner.View() + " 助手正在思考…\n")
	}
	if m.errMsg != "" {
		b.WriteString(errorStyle.Width(contentWidth).Render("【错误】"+m.errMsg) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderStatus 渲染底部状态行：运行状态、大模型配置、当前方案件数。
func (m Model) renderStatus() string {
	state := "就绪"
	if m.busy {
		state = "思考中"
	}
	llm := "未配置（请在 .env 填 LLM_API_KEY、LLM_MODEL）"
	if m.client != nil && m.client.apiKey != "" && m.client.model != "" {
		llm = "已配置 · 模型 " + m.client.model
	}
	plan := "暂无方案"
	if len(m.products) > 0 {
		plan = fmt.Sprintf("当前方案 %d 件（可用 /open、/search）", len(m.products))
	}
	return statusStyle.Render(fmt.Sprintf("● %s | 大模型：%s | %s", state, llm, plan))
}

func (m Model) withNotice(text string) Model {
	m.messages = append(m.messages, chatMessage{Role: "assistant", Content: text})
	return m
}

func copyMessages(src []chatMessage) []chatMessage {
	dst := make([]chatMessage, len(src))
	copy(dst, src)
	return dst
}
