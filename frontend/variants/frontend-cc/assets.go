package frontend

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Assets 返回已构建的界面资源；生产服务由控制端统一提供。
func Assets() fs.FS {
	assets, _ := fs.Sub(embedded, "dist")
	return assets
}

func Version() string {
	value, err := fs.ReadFile(Assets(), "version.txt")
	if err != nil {
		return "未知"
	}
	return strings.TrimSpace(string(value))
}
