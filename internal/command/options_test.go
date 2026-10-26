package command

import "testing"

func TestInstallParametersHaveExplicitPriority(t *testing.T) {
	t.Setenv("OPSD_NON_INTERACTIVE", "invalid")
	t.Setenv("OPSD_PORT", "invalid")
	t.Setenv("OPSD_USERNAME", "environment")
	t.Setenv("OPSD_PASSWORD", "environment-password")
	t.Setenv("OPSD_ENTRANCE", "/environment-entry")
	value, err := ReadInstall([]string{"--non-interactive", "--install-dir", t.TempDir(), "--port", "34567", "--username", "command", "--password", "command-password", "--entrance", "/command-entry"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !value.NonInteractive || value.Port != "34567" || value.Username != "command" || value.Password != "command-password" || value.Entrance != "/command-entry" {
		t.Fatal("命令行参数未覆盖环境变量")
	}
	if _, err := ReadInstall([]string{"--non-interactive", "--install-dir", t.TempDir()}, false); err == nil {
		t.Fatal("显式无效环境端口被静默换成默认值")
	}
}
