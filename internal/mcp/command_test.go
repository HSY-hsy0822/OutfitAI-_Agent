package mcp

// 构建产物验证：构建真实可执行文件，用 CommandTransport 启动其 mcp 子命令，
// 验证外部 MCP 客户端通过 stdio 接入的完整链路（initialize 握手 → tools/call）。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStdioCommandTransport(t *testing.T) {
	ctx := context.Background()
	projectRoot := filepath.Join("..", "..")

	exeName := "outfit-agent.exe"
	if runtime.GOOS != "windows" {
		exeName = "outfit-agent"
	}
	exePath := filepath.Join(projectRoot, exeName)
	if _, err := os.Stat(exePath); err != nil {
		build := exec.Command("go", "build", "-o", exePath, ".")
		build.Dir = projectRoot
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("构建可执行文件失败：%v\n%s", err, out)
		}
	}

	// 转绝对路径：设置 cmd.Dir 后，相对 exe 路径会基于新工作目录解析。
	absExe, err := filepath.Abs(exePath)
	if err != nil {
		t.Fatal(err)
	}
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(absExe, "mcp")
	cmd.Dir = absRoot // 让 exe 能找到 data/wardrobe.json

	cli := mcp.NewClient(&mcp.Implementation{Name: "stdio-probe", Version: "v1.0.0"}, nil)
	session, err := cli.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal("连接 stdio server 失败：", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_wardrobe",
		Arguments: SearchInput{Category: "鞋", Gender: "女", Limit: 3},
	})
	if err != nil {
		t.Fatal("CallTool 失败：", err)
	}
	out := decode[SearchOutput](t, res)
	if out.Total == 0 {
		t.Fatal("stdio 链路下应返回女鞋结果")
	}
	if len(out.Products) > 3 {
		t.Fatalf("limit=3 未生效，返回 %d 条", len(out.Products))
	}
	for _, p := range out.Products {
		if !strings.EqualFold(p.Category, "鞋") {
			t.Fatalf("返回了非鞋类：%+v", p)
		}
	}
}
