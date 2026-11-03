package utils

import (
	"errors"
	"net/url"	//解析编码URL
	"os/exec"
	"runtime"
	"strings"
)

// SearchURL 把商品名拼成对应平台的关键词搜索页。
// 用途：具体商品页（SKU）下架后，用户仍能在同一平台搜到同款。
func SearchURL(platform, keyword string) string {
	keyword = strings.TrimSpace(keyword)//用于去除字符串首尾的空白字符，包括空格、换行符等
	for _, suffix := range []string{"（京东搜索）", "(京东搜索)", "（示例）", "(示例)"} {
		keyword = strings.TrimSuffix(keyword, suffix)//用于删除字符串末尾指定的后缀
	}
	//QueryEscape，URL 的查询参数不能随意直接拼接中文和特殊字符，所以需要进行编码
	//strings.ReplaceAll() 会将字符串中所有的 + 替换为 %20。
	kw := strings.ReplaceAll(url.QueryEscape(strings.TrimSpace(keyword)), "+", "%20")
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "taobao":
		return "https://s.taobao.com/search?q=" + kw
	case "tmall":
		return "https://list.tmall.com/sear2ch_product.htm?q=" + kw
	case "pdd":
		return "https://mobile.yangkeduo.com/search_result.html?search_key=" + kw
	case "vip":
		return "https://search.vip.com/?keyword=" + kw
	default: // jd 及未知平台
		return "https://search.jd.com/Search?keyword=" + kw
	}
}

//接收一个完整的 URL，检查它是否合法，然后尝试调用系统命令打开默认浏览器。
func OpenBrowser(raw string) error {
	u, err := url.ParseRequestURI(strings.TrimSpace(raw))
	
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("链接无效：需要完整的 http/https URL")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	case "darwin":
		cmd = exec.Command("open", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	return cmd.Start()
}
