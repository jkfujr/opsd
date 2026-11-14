//go:build !linux

package databaseengine

import "fmt"

func deploymentFreeBytes(string) (uint64, error) {
	return 0, fmt.Errorf("数据库部署需要 Linux 节点")
}
