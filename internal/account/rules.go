package account

import (
	"fmt"
	"regexp"
	"strings"
)

var entrancePattern = regexp.MustCompile(`^/[A-Za-z0-9_-]{6,128}$`)

func Validate(username, entrance string) error {
	if strings.TrimSpace(username) == "" || len(username) > 128 {
		return fmt.Errorf("账号不能为空且不能超过 128 字节")
	}
	reserved := map[string]bool{"/settings": true, "/firewall": true, "/containers": true, "/storage": true, "/assets": true, "/events": true}
	if !entrancePattern.MatchString(entrance) || reserved[entrance] {
		return fmt.Errorf("安全入口须为 / 加 6 至 128 位字母、数字、下划线或短横线，且不能使用保留路径")
	}
	return nil
}
