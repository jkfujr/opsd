package databaseengine

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

// ReadPrivateConfig 只读取已有配置中的字面值，不执行 Shell 内容。
func ReadPrivateConfig(path string) map[string]string {
	f, e := os.Open(path)
	if e != nil {
		return nil
	}
	defer f.Close()
	out := map[string]string{}
	scanner := bufio.NewScanner(io.LimitReader(f, 64*1024))
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(scanner.Text()), "export "))
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '\'' && value[len(value)-1] == '\'' || value[0] == '"' && value[len(value)-1] == '"') {
			value = value[1 : len(value)-1]
		}
		if !strings.ContainsAny(value, "`\x00\r\n") && !strings.Contains(value, "$(") {
			out[strings.TrimSpace(key)] = value
		}
	}
	return out
}
func CompleteDiscovered(items []protocol.DatabaseCandidate) []protocol.DatabaseCandidate {
	for i := range items {
		c := &items[i].Connection
		if c.Kind == "galera" {
			for _, file := range []string{"/root/galera-secrets/root.env", "/root/galera-secrets/mariadb.env", "/root/galera-secrets/root-client.cnf"} {
				v := ReadPrivateConfig(file)
				for _, key := range []string{"MARIADB_ROOT_PASSWORD", "MYSQL_ROOT_PASSWORD", "password"} {
					if c.Password == "" && v[key] != "" {
						c.Password = v[key]
					}
				}
			}
			if _, e := os.Stat("/data1/server/db/backups"); e == nil {
				c.BackupRoot = "/data1/server/db/backups"
			}
		} else {
			v := ReadPrivateConfig("/root/proxysql-secrets/admin.env")
			if c.Password == "" {
				c.Password = v["PROXYSQL_ADMIN_PASSWORD"]
			}
		}
	}
	return items
}
func BackupStatus(ctx context.Context, root string) []protocol.DatabaseBackup {
	out := []protocol.DatabaseBackup{}
	if root == "" || runtime.GOOS != "linux" {
		return out
	}
	for _, tier := range []string{"hourly", "daily", "weekly"} {
		item := protocol.DatabaseBackup{Name: tier, State: "unknown", Artifacts: []string{}}
		unit := "galera-backup-" + tier
		result, e := exec.CommandContext(ctx, "systemctl", "show", unit+".service", "--property=LoadState,Result,ExecMainStartTimestamp,ExecMainStatus").Output()
		values := map[string]string{}
		for _, line := range strings.Split(string(result), "\n") {
			k, v, ok := strings.Cut(line, "=")
			if ok {
				values[k] = v
			}
		}
		if values["LoadState"] == "not-found" {
			item.State = "not_configured"
		}
		if e == nil && values["LoadState"] != "not-found" {
			if v := values["ExecMainStartTimestamp"]; v != "" {
				if t, err := time.ParseInLocation("Mon 2006-01-02 15:04:05 MST", v, time.Local); err == nil {
					item.LastRun = &t
				}
			}
			if item.LastRun != nil && values["Result"] == "success" {
				item.State = "succeeded"
			} else if values["Result"] != "" && values["Result"] != "success" {
				item.State = "failed"
				item.Error = fmt.Sprintf("%s（退出状态 %s）", values["Result"], values["ExecMainStatus"])
			}
		}
		if b, e := exec.CommandContext(ctx, "systemctl", "show", unit+".timer", "--property=TimersCalendar", "--value").Output(); e == nil {
			item.Schedule = strings.TrimSpace(string(b))
		}
		if info, e := os.Stat(filepath.Join(root, "status", tier+".success")); e == nil {
			t := info.ModTime().UTC()
			item.LastSuccess = &t
		}
		entries, e := os.ReadDir(filepath.Join(root, tier))
		if e == nil {
			names := []string{}
			for _, entry := range entries {
				if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
					names = append(names, entry.Name())
				}
			}
			sort.Sort(sort.Reverse(sort.StringSlice(names)))
			if len(names) > 0 {
				files, _ := os.ReadDir(filepath.Join(root, tier, names[0]))
				for _, f := range files {
					if !f.IsDir() {
						item.Artifacts = append(item.Artifacts, filepath.Join(root, tier, names[0], f.Name()))
					}
				}
			}
		}
		out = append(out, item)
	}
	return out
}
