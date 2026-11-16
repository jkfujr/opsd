//go:build !linux

package dockerengine

import (
	"context"
	"fmt"
)

func monitoringDisk(context.Context, string) (int64, int64, error) {
	return 0, 0, fmt.Errorf("监控部署仅支持 Linux")
}
