package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/command"
	"github.com/jkfujr/opsd/internal/installation"
)

func capabilityCommand(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "check" {
		return fmt.Errorf("用法：opsd capability check --config 配置文件 [--require 授权清单]")
	}
	set := command.Flags("capability check")
	path := set.String("config", command.DefaultInstance, "")
	require := set.String("require", "", "")
	if err := command.Parse(set, args[1:]); err != nil {
		return err
	}
	f, err := installation.NodePaths(*path)
	if err != nil {
		return err
	}
	grants, err := cluster.ParseSourceCapabilities(f.SourceCapabilities)
	if err != nil {
		return err
	}
	var report strings.Builder
	if len(f.SourceCapabilities) == 0 {
		fmt.Fprintln(&report, "缺少 source_capabilities，成员仅可使用默认权限")
	}
	fmt.Fprintln(&report, "来源\t显式\t有效")
	for _, grant := range grants.Grants() {
		fmt.Fprintf(&report, "%s\t%s\t%s\n", grant.Origin, strings.Join(grant.Explicit, ","), strings.Join(grant.Effective, ","))
	}
	if _, err = io.WriteString(out, report.String()); err != nil {
		return err
	}
	if *require == "" {
		return nil
	}
	raw, err := os.ReadFile(*require)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("授权清单为空")
	}
	required, err := cluster.ParseSourceCapabilities(raw)
	if err != nil {
		return fmt.Errorf("授权清单无效：%w", err)
	}
	var missing []string
	for _, grant := range required.Grants() {
		for _, name := range grant.Explicit {
			if !grants.Has(grant.Origin, cluster.Capability(name)) {
				missing = append(missing, grant.Origin+"/"+name)
			}
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("缺少必需能力：%s", strings.Join(missing, "、"))
	}
	return nil
}
