//go:build !linux

package operations

import "fmt"

func SystemClock() (string, int64, error) {
	return "", 0, fmt.Errorf("原生操作执行需要 Linux 启动时钟")
}
