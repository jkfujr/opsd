package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapabilityCheckEffectiveGrants(t *testing.T) {
	const origin = "0123456789abcdef0123456789abcdef"
	dir := t.TempDir()
	config := filepath.Join(dir, "instance.json")
	manifest := filepath.Join(dir, "required.json")
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(config, `{"data_dir":"/node","executor_dir":"/executor","socket":"/executor.sock","source_capabilities":{"`+origin+`":["manage_monitoring"]}}`)
	args := []string{"check", "--config", config, "--require", manifest}
	var out strings.Builder
	write(manifest, `{"`+origin+`":["read_monitoring"]}`)
	if err := capabilityCommand(args, &out); err != nil || !strings.Contains(out.String(), origin+"\tmanage_monitoring\tmanage_monitoring,read_monitoring") {
		t.Fatalf("预检应按有效授权放行：%v，%s", err, &out)
	}
	for _, requirement := range []string{`{"` + origin + `":["terminal"]}`, `{"` + origin + `":["unknown"]}`, ""} {
		out.Reset()
		write(manifest, requirement)
		if err := capabilityCommand(args, &out); err == nil {
			t.Errorf("预检应拒绝清单 %q", requirement)
		}
	}
	args[4] = filepath.Join(dir, "missing.json")
	if err := capabilityCommand(args, &out); err == nil {
		t.Error("预检应拒绝无法读取的清单")
	}
}
