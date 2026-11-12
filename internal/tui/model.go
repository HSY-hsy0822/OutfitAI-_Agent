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

// ---------- 界面样式 ----------

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

	// follow 为 true 时新内容到达后自动贴到底部；用户手动往上翻历史时置 false，
	// 这样滚动历史时不会被新消息强行拽回底部。
	follow bool

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
		"2. 用户想要推荐、且关键信息基本明确时，调用 recommend_outfits 工具；商品名、价格、链接只能原样使用工具返回的字段，严禁编造，也不要把链接简写或自己拼；\n" +
		"3. 拿到工具结果后，用大白话讲清整体思路、每件为什么选、价格和预算；每件单品后面都要原样附上工具返回的 url，写成「名称（¥价格）链接」的形式，不要省略链接；\n" +
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
		follow:    true,
		width:     80,
		height:    24,
		ready:     true,
	}
	w, err := repository.LoadWardrobe(firstExistingPath("data/wardrobe.json", "../data/wardrobe.json", "../../data/wardrobe.json"))
	if err != nil {
		m.errMsg = "衣橱库加载失败：" + err.Error()
	} else {
		m.wardrobe = w
	}
	m.refreshViewport() // 首帧就把内容写进 viewport，否则它一直是空内容、滚不动
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
		// 高度预算：标题 1 + 对话边框 2 + 输入 2 + 状态 1 + 快捷键 1 = 7 行，
		// 再留 1 行余量：View() 的行数必须严格小于终端高度，否则每帧都会把
		// 第一行（标题）挤出屏幕，观感像"界面在抖"。
		m.viewport.Width = max(10, msg.Width-4)
		m.viewport.Height = max(5, msg.Height-8)
		m.textInput.Width = max(10, msg.Width-6)
		m.refreshViewport() // 宽度变了要重新折行，滚动位置也要重算
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case responseMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.refreshViewport()
			return m, nil
		}
		m.messages = msg.messages
		if msg.products != nil {
			m.products = msg.products
		}
		m.errMsg = ""
		m.follow = true // 有新回答：自动贴到底部
		m.refreshViewport()
		return m, nil

	case tea.MouseMsg:
		// 鼠标消息（滚轮）直接转发给 viewport；思考中也允许翻历史，所以不检查 busy。
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.follow = m.viewport.AtBottom() // 自己翻到中间就别再自动贴底
		return m, cmd

	case tea.KeyMsg:
		// 任何时候都允许 Ctrl+C 退出
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		// 滚动键先处理：思考中（busy）也允许翻历史，否则恰好在你最想往上翻的时候滚不动。
		// 只拦 ↑↓/PgUp/PgDn，其它键一律交给输入框——否则输入框里打 j/k/d/u 会被
		// viewport 当成"往下翻"，边打字边乱滚。
		if isScrollKey(msg) {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			m.follow = m.viewport.AtBottom()
			return m, cmd
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
			m.follow = true
			m.refreshViewport()
			return m, m.requestCmd(copyMessages(m.messages))
		}
		// 其余按键（左右移动、退格、删除等）只交给输入框
		var tiCmd tea.Cmd
		m.textInput, tiCmd = m.textInput.Update(msg)
		return m, tiCmd
	}
	return m, nil
}

// isScrollKey 判断按键是不是专门用来翻对话历史的（不涉及输入框的编辑操作）。
func isScrollKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
		return true
	}
	return false
}

// refreshViewport 把最新的对话内容写进滚动区。
// 必须在 Update 里调用，不能放在 View 里：View 是值接收者，在 View 里 SetContent
// 只改到副本，真实 Model 的 viewport 内容永远是空的——那样 YOffset 恒为 0，
// 滚轮和 ↑↓/PgUp/PgDn 全部无效（之前"滚不动、看不到下面"的根因）。
// GotoBottom 必须放在 SetContent 之后：它按当前内容的高度算最大偏移，
// 先调它只会按旧内容的矮高度算，新内容的下半截仍然留在屏幕外。
func (m *Model) refreshViewport() {
	m.viewport.SetContent(m.renderConversation())
	if m.follow {
		m.viewport.GotoBottom()
	}
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

// View 只负责把当前状态画出来（不在这里改状态，理由见 refreshViewport 的注释）。
func (m Model) View() string {
	if !m.ready {
		return "正在初始化终端界面…"
	}

	header := headerStyle.Render("穿搭推荐 Agent · Bubble Tea 版")
	box := conversationBox.
		Width(m.viewport.Width + 2).
		Render(m.viewport.View())

	input := lipgloss.JoinVertical(lipgloss.Left, "", m.textInput.View())
	status := m.renderStatus()
	// MaxWidth：窄终端下把这一行截断，而不是折行——折行会让整体高度多出 1 行，
	// 又会把顶部的标题挤出屏幕。
	help := helpStyle.MaxWidth(m.width).Render("Ctrl+C 退出 · /help 帮助 · /reset 清空 · /open 序号 打开 · /search 序号 搜同款 · 滚轮/↑↓/PgUp/PgDn 滚动")

	return lipgloss.JoinVertical(lipgloss.Left, header, box, input, status, help)
}

// renderConversation 把对话历史渲染成对话区文本。
func (m Model) renderConversation() string {
	contentWidth := max(10, m.viewport.Width-2)
	var b strings.Builder

	if m.wardrobe != nil && len(m.messages) == 1 {
		writePara(&b, noticeStyle.Width(contentWidth).Render(
			"你好，我是你的穿搭顾问。直接告诉我你的性别、风格、场合、季节和预算，我就会从本地衣橱里给你配一整套；也可以先随便聊聊。"))
	}

	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			writeLine(&b, userLabel)
			writePara(&b, lipgloss.NewStyle().Width(contentWidth).Render(msg.Content))
		case "assistant":
			if msg.Content != "" {
				writeLine(&b, assistantLabel)
				writePara(&b, lipgloss.NewStyle().Width(contentWidth).Render(msg.Content))
			}
		}
	}

	if m.busy {
		b.WriteString(m.spinner.View())
		writeLine(&b, " 助手正在思考…")
	}
	if m.errMsg != "" {
		writeLine(&b, errorStyle.Width(contentWidth).Render("【错误】"+m.errMsg))
	}
	b.WriteString(m.renderLinkList())
	return strings.TrimRight(b.String(), "\n")
}

// writeLine 写一行文本：内容和换行分开写进 Builder。
// 不用 b.WriteString(text + "\n")，因为那样会为整串（可能很长的）文本多复制一份
// 只为了拼一个换行符——Go 的字符串是不可变的，a + "\n" 必然发生一次分配和拷贝。
// 这也是 GoLand 在 model.go 里提示 "Inefficient string concatenation in call to
// WriteString" 的原因（效率提示，不是编译错误）。
func writeLine(b *strings.Builder, text string) {
	b.WriteString(text)
	b.WriteString("\n")
}

// writePara 写一段文本（末尾空一行），同样避免拼接带来的额外拷贝。
func writePara(b *strings.Builder, text string) {
	b.WriteString(text)
	b.WriteString("\n\n")
}

// renderLinkList 是链接兜底：商品和链接都在工具返回里，但模型（尤其 lite 小模型）
// 经常只复述名称和价格、把 url 吃掉。这里直接用商品数据把链接块渲染出来，
// 保证界面上一定能看到、能用 /open 打开的链接。
// 如果模型已经把链接写进回答里，就不再重复渲染。
func (m Model) renderLinkList() string {
	if len(m.products) == 0 || strings.Contains(lastAssistantContent(m.messages), "http") {
		return ""
	}
	var b strings.Builder
	writeLine(&b, noticeStyle.Render(fmt.Sprintf("链接（/open 序号 在浏览器打开 · /search 序号 搜同款），共 %d 件：", len(m.products))))
	for i, p := range m.products {
		writeLine(&b, fmt.Sprintf("%d. %s ¥%.0f", i+1, p.Name, p.Price))
		writeLine(&b, "   "+p.URL)
	}
	b.WriteString("\n")
	return b.String()
}

// lastAssistantContent 返回最后一条助手消息的正文，用来判断模型是否已经给了链接。
func lastAssistantContent(msgs []chatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i].Content
		}
	}
	return ""
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
	return statusStyle.MaxWidth(m.width).Render(fmt.Sprintf("● %s | 大模型：%s | %s", state, llm, plan))
}

func (m Model) withNotice(text string) Model {
	m.messages = append(m.messages, chatMessage{Role: "assistant", Content: text})
	m.follow = true
	m.refreshViewport()
	return m
}

func copyMessages(src []chatMessage) []chatMessage {
	dst := make([]chatMessage, len(src))
	copy(dst, src)
	return dst
}
