# OutfitAI Agent · 穿搭推荐 Agent

> 一个基于 Go 的中文穿搭推荐 Agent：既能作为 **终端交互式对话助手**（Bubble Tea TUI）使用，也能作为 **MCP Server** 暴露给任意 MCP 客户端（Claude Desktop、Cursor 等）。
>
> A Go-based Chinese outfit-recommendation Agent that runs both as an **interactive terminal chatbot** (Bubble Tea TUI) and as an **MCP Server** that any MCP client (Claude Desktop, Cursor, etc.) can consume.

---

## 功能特性 / Features

- 🗣️ **自然语言多轮对话**：像朋友一样聊穿搭需求，每次只问最关键的 1~2 个问题，逐步收集性别、风格、场合、季节、预算等信息。
  **Natural-language multi-turn chat**: gathers your needs conversationally, one or two key questions at a time.
- 🧠 **大模型驱动 + 本地规则兜底**：默认调用豆包（Doubao，OpenAI 兼容接口）生成搭配；当模型未配置、超时、返回非法 JSON 或匹配不到本地商品时，自动回退到本地规则算法，**永远能给出可展示的方案**。
  **LLM-driven with local-rule fallback**: uses Doubao (OpenAI-compatible) by default; falls back to a local algorithm whenever the model fails or is unconfigured — so a presentable outfit is always returned.
- 🔧 **工具调用（Function Calling）**：通过 `recommend_outfits` 工具，把「生成搭配」这件事交给本地算法，模型只负责理解意图和解释结果，**不编造商品名、价格和链接**。
  **Tool calling**: the `recommend_outfits` tool delegates outfit generation to local code; the model only understands intent and explains results, never fabricating products, prices, or links.
- 🧥 **本地衣橱库 + 匹配打分**：从 `data/wardrobe.json` 加载商品，按「风格 4 分 / 季节 3 分 / 场景 2 分」打分，再按性别过滤、按匹配分降序取候选。
  **Local wardrobe with scoring**: loads products from `data/wardrobe.json`, scores them (style +4 / season +3 / occasion +2), filters by gender, and ranks candidates.
- 💰 **整套预算控制**：预算约束作用于**整套总价**，穷举各单品组合，挑出「不超预算且总分最高」的一套；预算不够时兜底取各品类最便宜的一件，超预算会明确提示。
  **Outfit-level budget control**: budget applies to the whole outfit; it exhaustively picks the best-scoring combination under budget, falls back to the cheapest item per category, and flags over-budget results.
- ✨ **特殊要求处理**：支持「某件换便宜点 / 省钱 / 平价」这类口语化要求，会在同品类里自动替换成更便宜的商品。
  **Special requests**: understands phrases like "make the shoes cheaper" and swaps in a cheaper item from the same category.
- 🔗 **打开商品链接 / 搜同款**：可一键在浏览器打开商品页，或在对应电商平台搜索同款（京东 / 淘宝 / 天猫 / 拼多多 / 唯品会）。
  **Open link / search similar**: opens a product page in the browser, or searches the same item on JD / Taobao / Tmall / PDD / VIP.
- 🧩 **MCP Server 模式**：以 `stdio`（JSON-RPC）方式暴露 `recommend_outfits` 和 `search_wardrobe` 两个工具，可被任何 MCP 客户端接入。
  **MCP Server mode**: exposes `recommend_outfits` and `search_wardrobe` over stdio JSON-RPC, consumable by any MCP client.

---

## 项目结构 / Project Structure

```
outfit-agent/
├── main.go                     # 入口：默认启动 TUI，`mcp` 子命令启动 MCP server
├── data/
│   └── wardrobe.json           # 本地衣橱库（商品名、价格、风格、链接等）
├── internal/
│   ├── agent/                  # 核心：Agent、LLM 客户端、结果解析、本地规则搭配
│   │   ├── agent.go
│   │   └── llm.go
│   ├── mcp/                    # MCP server：协议封装 + 两个工具
│   │   └── server.go
│   ├── models/                 # 数据结构：UserProfile / Product / Recommendation
│   │   └── models.go
│   ├── repository/             # 衣橱加载、匹配打分、候选筛选、预算解析
│   │   └── wardrobe.go
│   ├── tui/                    # Bubble Tea 交互界面 + 大模型对话客户端
│   │   ├── model.go
│   │   ├── client.go
│   │   └── helpers.go
│   └── utils/                  # 打开浏览器、拼搜索链接
│       └── browser.go
├── .env.example                # 环境变量模板（已脱敏）
└── go.mod / go.sum
```

---

## 快速开始 / Quick Start

### 1. 环境要求 / Requirements

- Go 1.25+
- 一个火山引擎 Ark 账号（或任意 OpenAI 兼容接口）；不配置也能用本地规则跑通

### 2. 配置 / Configuration

复制模板并按需填写（`LLM_API_KEY` 留空则自动使用本地规则）：

```bash
cp .env.example .env
```

`.env` 内容示例（**请勿提交真实密钥到公开仓库**）：

```ini
LLM_BASE_URL="https://ark.cn-beijing.volces.com/api/v3"
LLM_API_KEY="your-api-key-here"
LLM_MODEL="your-model-or-endpoint-id"
```

### 3. 运行 / Run

**交互式 TUI（对话助手）：**

```bash
go run . 
```

**MCP Server（供 MCP 客户端接入）：**

```bash
go run . mcp
```

---

## 交互命令 / TUI Commands

| 命令 / Command | 作用 / Description |
| --- | --- |
| `/help` | 查看命令帮助 / show help |
| `/reset` | 清空对话记忆，重新开始 / clear conversation memory |
| `/open <序号>` | 用默认浏览器打开某件商品的详情页 / open a product page |
| `/search <序号>` | 在商品所属平台搜索同款 / search similar items on the platform |
| `/quit` 或 `/exit` | 退出 / quit |
| `Ctrl+C` | 任意时刻退出 / quit anytime |

---

## MCP 工具 / MCP Tools

### `recommend_outfits`

根据用户资料从本地衣橱生成一套完整穿搭（4~6 件）。

输入参数（均为字符串，可选）：

| 参数 / Param | 说明 / Description |
| --- | --- |
| `gender` | 性别，如 男 / 女 |
| `age` | 年龄，如 20 |
| `height` | 身高 cm，如 163 |
| `weight` | 体重 kg，如 55 |
| `body_type` | 身材，如 梨形 / H型 |
| `skin_tone` | 肤色 |
| `style` | 风格，如 休闲 / 通勤 / 甜美 |
| `occasion` | 场景，如 上课 / 通勤 / 约会 |
| `season` | 季节，如 秋季 |
| `budget` | 整套预算（元），如 300 |
| `special_request` | 特殊调整，如 鞋子要便宜、不要裙子 |

返回：`summary`（思路）、`reason`（搭配理由）、`total_price`（合计价）、`over_budget`（是否超预算）、`items`（单品清单含名称/价格/链接）、`tips`。

### `search_wardrobe`

按品类、商品名关键词、性别检索本地衣橱库，返回商品清单（含价格与链接），**不编造任何商品**。

| 参数 / Param | 说明 / Description |
| --- | --- |
| `category` | 品类，如 上衣 / 裤装 / 鞋 / 外套 |
| `keyword` | 商品名关键词 |
| `gender` | 性别过滤，如 女 |
| `limit` | 最多返回条数，默认 10 |

---

## 工作原理 / How It Works

1. **对话理解**：TUI 模式下，聊天客户端把对话历史 + 工具定义发给大模型，模型决定何时调用 `recommend_outfits`。
2. **候选筛选**：`repository.Candidates` 先按性别过滤，再按「风格 4 分 / 季节 3 分 / 场景 2 分」打分，取前 N 件作为候选池。
3. **搭配生成**：优先让大模型从候选里挑一套并输出 JSON；解析时要求 `keyword` 必须与本地商品名匹配（`matchProduct`），避免张冠李戴。若解析失败，回退到本地规则：按「上衣 + 下装 + 鞋（秋冬再加外套）」搭必需位置，穷举组合挑最优，预算有余再补包和配饰。
4. **结果渲染**：结构化结果带商品名、价格、跳转链接和选择理由返回给用户。

> **反幻觉设计**：系统提示词明确「严禁编造商品名、价格、库存或链接，keyword 必须原样抄写候选商品名」，且解析后还会再次校验商品是否真的存在于本地库。

---

## 安全提示 / Security Note

- `.env` 已被 `.gitignore` 忽略，**切勿**把真实 `LLM_API_KEY` 写入会被提交的文件（如 `.env.example` 或代码注释）。
- `data/wardrobe.json` 是本地商品数据，可自行增删替换；如需分享，注意其中是否包含你不想公开的链接。

---

## 技术栈 / Tech Stack

- [Go](https://go.dev/) 1.25
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) + Bubbles + Lip Gloss（TUI）
- [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk)（MCP）
- 火山引擎 Ark / Doubao（OpenAI 兼容接口）
