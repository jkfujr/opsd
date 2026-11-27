//go:build !linux

package installation

import "fmt"

func prepareNodeConfigDir(string, bool) error {
	return fmt.Errorf("实例配置目录权限设置需要 Linux")
}
