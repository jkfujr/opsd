package cli

import (
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/cluster"
)

func TestInitialAccountOutput(t *testing.T) {
	account := cluster.AccountChange{Username: "admin", Entrance: "/signin", Password: "test-only-password"}
	for _, tc := range []struct {
		name                string
		generated, terminal bool
	}{
		{"交互随机密码", true, true},
		{"自动化随机密码", true, false},
		{"已提供密码", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output strings.Builder
			printInitialAccount(&output, account, tc.generated, tc.terminal)
			text := output.String()
			if !strings.Contains(text, account.Username) || !strings.Contains(text, account.Entrance) {
				t.Fatal("缺少初始登录资料")
			}
			if strings.Contains(text, account.Password) != (tc.generated && tc.terminal) {
				t.Fatal("默认密码交付渠道错误")
			}
			if tc.generated && !tc.terminal && !strings.Contains(text, "opsd account") {
				t.Fatal("缺少本机设置密码的入口")
			}
		})
	}
}
