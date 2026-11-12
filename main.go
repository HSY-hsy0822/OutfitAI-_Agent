package main

// 程序入口：
//   默认（无参数）：启动 Bubble Tea 交互界面，Init/Update/View 在 internal/tui 包。
//   mcp 子命令：启动 MCP stdio server，把 Agent 能力通过 MCP 协议暴露给外部客户端。

import (
	"context"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "outfit-agent/internal/mcp"
	"outfit-agent/internal/tui"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		runMCP()
		return
	}
	// WithMouseCellMotion：启用鼠标事件（含滚轮），viewport 才能收到滚轮消息并滚动。
	p := tea.NewProgram(tui.New(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

// runMCP 启动 stdio 形式的 MCP server，阻塞直到客户端断开。
func runMCP() {
	mcpserver.LoadEnvFile(firstExistingPath(".env", "../.env", "../../.env"))
	srv, err := mcpserver.NewServer(firstExistingPath("data/wardrobe.json", "../data/wardrobe.json", "../../data/wardrobe.json"))
	if err != nil {
		log.Fatal(err)
	}
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

// firstExistingPath 返回第一个存在的路径；都不存在时返回第一个，让后续报错带上它。
func firstExistingPath(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return paths[0]
}
