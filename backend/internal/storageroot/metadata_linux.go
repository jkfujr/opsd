package storageroot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/storage"
	"golang.org/x/sys/unix"
)

var metadataServices = []string{"opsd-executor.service", "opsd.service"}
var metadataUnits = []string{"opsd-executor.service", "opsd.service", "opsd-operation@.service", "opsd-recover.service"}

type metadataCopy struct {
	Stage string `json:"stage"`
	Inode uint64 `json:"inode"`
}

type metadataRollback struct {
	ID          string                  `json:"id"`
	Phase       string                  `json:"phase"`
	DeadlineHit bool                    `json:"systemd_timeout,omitempty"`
	WriteIntent bool                    `json:"write_intent"`
	Error       string                  `json:"error,omitempty"`
	Copies      map[string]metadataCopy `json:"copies,omitempty"`
	Times       map[string]int64        `json:"step_ms,omitempty"`
}

type metadataInstance struct {
	DataDir          string `json:"data_dir"`
	ExecutorDir      string `json:"executor_dir"`
	Binary           string `json:"binary"`
	Socket           string `json:"socket"`
	ControlSocket    string `json:"control_socket"`
	StorageMountUUID string `json:"storage_mount_uuid"`
}

var metadataSource = metadataInstance{DataDir: storage.NodeDir, ExecutorDir: storage.ExecutorDir}
var metadataTarget = metadataInstance{DataDir: "/var/lib/opsd/node", ExecutorDir: "/var/lib/opsd/executor"}

func (m *Manager) LegacyRecord(id string) (bool, error) {
	if !operations.ValidID(id) {
		return false, fmt.Errorf("迁移标识无效")
	}
	if _, err := os.Lstat(filepath.Join(m.stateDir(), "requests", id+".json")); !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	s, err := os.Lstat(filepath.Join(m.stateDir(), id, "result.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !s.Mode().IsRegular() {
		return false, fault("conflict", "I0.6 结果记录不是普通文件")
	}
	return true, nil
}

func (m *Manager) MetadataRollback(ctx context.Context, id string, status bool) (out any, err error) {
	dir := filepath.Join(m.stateDir(), id)
	if err = trustedDir(dir); err != nil {
		return nil, err
	}
	var saved struct{ Status string }
	if err = localstate.Read(filepath.Join(dir, "result.json"), &saved); err != nil || saved.Status != "migrated" {
		return nil, fault("conflict", "I0.6 原迁移缺少完成记录")
	}
	path := filepath.Join(dir, "rollback.json")
	v := metadataRollback{ID: id, Phase: "complete", Times: map[string]int64{}}
	if err = localstate.Read(path, &v); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if status || v.Phase == "rolled_back" {
		return v, nil
	}
	unlock, err := localstate.Lock("/run/opsd-metadata-migration.lock")
	if err != nil {
		return nil, err
	}
	defer unlock()
	unlock, err = localstate.Lock(filepath.Join(m.stateDir(), "lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()
	r, err := m.paths(ctx)
	if err != nil {
		return nil, err
	}
	if p, err := m.pending(r); err != nil {
		return nil, err
	} else if p != nil {
		return nil, fault("node_busy", "先处理组件迁移 %s", p.ID)
	}
	var pre struct {
		NodeID string   `json:"node_id"`
		Added  []string `json:"added_files"`
	}
	var before, active metadataInstance
	var activeJSON map[string]json.RawMessage
	for _, read := range []struct {
		path string
		data any
	}{{filepath.Join(dir, "preflight.json"), &pre}, {filepath.Join(dir, "instance.before.json"), &before},
		{m.Config, &active}, {m.Config, &activeJSON}} {
		if err := localstate.Read(read.path, read.data); err != nil {
			return nil, err
		}
	}
	target, source := metadataTarget, metadataSource
	if pre.NodeID == "" || before.DataDir != target.DataDir || before.ExecutorDir != target.ExecutorDir ||
		active.Binary != before.Binary || active.Socket != before.Socket || active.ControlSocket != before.ControlSocket {
		return nil, fault("conflict", "I0.6 快照、程序或套接字与当前实例不一致")
	}
	currentlyOnSource := active.DataDir == source.DataDir && active.ExecutorDir == source.ExecutorDir
	if !currentlyOnSource && (active.DataDir != target.DataDir || active.ExecutorDir != target.ExecutorDir || len(v.Copies) != 2) ||
		currentlyOnSource && v.WriteIntent {
		return nil, fault("conflict", "当前实例路径与回退阶段不同")
	}
	for _, p := range []string{source.DataDir, source.ExecutorDir, target.DataDir, target.ExecutorDir} {
		if err = realDir(p, !v.WriteIntent && len(v.Copies) == 2 && (p == target.DataDir || p == target.ExecutorDir)); err != nil {
			return nil, err
		}
	}
	for _, unit := range metadataUnits {
		drop := metadataDrop(unit)
		if !slices.Contains(pre.Added, drop) {
			return nil, fault("conflict", "迁移记录缺少 %s", drop)
		}
	}
	if currentlyOnSource && len(v.Copies) == 0 {
		if v.Phase == "complete" {
			if err = metadataReadback(ctx, active.ControlSocket, pre.NodeID, source); err != nil {
				return nil, err
			}
		}
		if err = checkMetadataQueue(ctx, source); err != nil {
			return nil, err
		}
		for _, p := range []string{source.DataDir, source.ExecutorDir} {
			if _, err = output(ctx, "sync", "-f", p); err != nil {
				return nil, err
			}
		}
	}
	save := func() error { return localstate.Write(path, v, 0600) }
	step := func(phase string, f func() error) error {
		v.Phase = phase
		if err := save(); err != nil {
			return err
		}
		then := time.Now()
		err := f()
		v.Times[phase] = time.Since(then).Milliseconds()
		return errors.Join(err, save())
	}
	defer func() {
		if err != nil {
			if currentlyOnSource && len(v.Copies) == 0 && !errors.Is(err, systemdTimeout) && ctx.Err() == nil {
				if again := drain(ctx, drainBudget); again != nil {
					err = errors.Join(err, again)
				} else {
					err = errors.Join(err, changeSystemd(ctx, startBudget, append([]string{"start"}, metadataServices...)...))
				}
			}
			v.Phase, v.Error = "needs_recovery", err.Error()
			v.DeadlineHit = errors.Is(err, systemdTimeout)
			err = errors.Join(err, save())
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err = drain(ctx, drainBudget); err != nil {
		return nil, err
	}
	if !v.WriteIntent {
		if err = step("prepare", func() error {
			if e := changeSystemd(ctx, stopBudget, append([]string{"stop"}, metadataServices...)...); e != nil {
				return e
			}
			if e := drain(ctx, drainBudget); e != nil {
				return e
			}
			if e := assertMetadataStopped(ctx); e != nil {
				return e
			}
			if e := checkMetadataQueue(ctx, source); e != nil {
				return e
			}
			return checkWriters(source.DataDir, source.ExecutorDir, target.DataDir, target.ExecutorDir)
		}); err != nil {
			return nil, err
		}
		if len(v.Copies) == 0 {
			if err = step("copy", func() error {
				var e error
				v.Copies, e = stageMetadata(ctx, source, target)
				return e
			}); err != nil {
				return nil, err
			}
		}
		if err = step("switch", func() error {
			for _, dest := range []string{target.DataDir, target.ExecutorDir} {
				if e := publishMetadata(id, dest, v.Copies[dest]); e != nil {
					return e
				}
			}
			if e := switchMetadataUnits(dir); e != nil {
				return e
			}
			if currentlyOnSource {
				activeJSON["data_dir"], _ = json.Marshal(target.DataDir)
				activeJSON["executor_dir"], _ = json.Marshal(target.ExecutorDir)
				delete(activeJSON, "storage_mount_uuid")
				if e := writeMetadataConfig(m.Config, activeJSON); e != nil {
					return e
				}
			}
			if e := drain(ctx, drainBudget); e != nil {
				return e
			}
			if e := changeSystemd(ctx, reloadBudget, "daemon-reload"); e != nil {
				return e
			}
			actual, e := output(ctx, "systemctl", "show", "-p", "ReadWritePaths", "--value", metadataServices[1])
			if e != nil || actual != target.DataDir {
				return fault("conflict", "节点单元写路径须为 %s，实际为 %s：%v", target.DataDir, actual, e)
			}
			v.WriteIntent = true
			return save()
		}); err != nil {
			return nil, err
		}
	}
	if err = step("cleanup", func() error {
		if e := changeSystemd(ctx, startBudget, append([]string{"start"}, metadataServices...)...); e != nil {
			return e
		}
		if e := metadataReadback(ctx, active.ControlSocket, pre.NodeID, target); e != nil {
			return e
		}
		return checkWriters(source.DataDir, source.ExecutorDir)
	}); err != nil {
		return nil, err
	}
	v.Phase, v.Error, v.DeadlineHit = "rolled_back", "", false
	return v, save()
}

func checkMetadataQueue(ctx context.Context, s metadataInstance) error {
	for _, item := range []struct{ path, query string }{
		{filepath.Join(s.DataDir, "node.db"), "SELECT count(*) FROM queue WHERE terminal=0"},
		{filepath.Join(s.ExecutorDir, "executor.db"), "SELECT count(*) FROM operations WHERE terminal=0"},
	} {
		db, err := storage.ReadOnly(ctx, item.path)
		if err != nil {
			return err
		}
		var count int
		err = db.QueryRowContext(ctx, item.query).Scan(&count)
		_ = db.Close()
		if err != nil {
			return err
		}
		if count != 0 {
			return fault("node_busy", "%s 有 %d 项未结束操作", item.path, count)
		}
	}
	return nil
}

func assertMetadataStopped(ctx context.Context) error {
	for _, name := range metadataServices {
		state, err := output(ctx, "systemctl", "show", "--property=ActiveState", "--value", name)
		if err != nil || state != "inactive" && state != "failed" {
			return fault("conflict", "%s 尚未停止：%s %v", name, state, err)
		}
	}
	active, err := output(ctx, "systemctl", "list-units", "--state=active,activating,deactivating", "--no-legend", "--plain",
		strings.Replace(metadataUnits[2], "@.", "@*.", 1), metadataUnits[3])
	if err != nil || active != "" {
		return fault("node_busy", "仍有执行单元活动：%s %v", active, err)
	}
	return nil
}

func stageMetadata(ctx context.Context, source, target metadataInstance) (map[string]metadataCopy, error) {
	out := map[string]metadataCopy{}
	for _, v := range []struct {
		from, to, tree string
		db             []string
	}{{source.DataDir, target.DataDir, "themes", []string{"node.db", "metrics.sqlite", "monitoring/history.db"}},
		{source.ExecutorDir, target.ExecutorDir, "logs", []string{"executor.db"}}} {
		stage, err := os.MkdirTemp(filepath.Dir(v.to), ".opsd-metadata-rollback-")
		if err != nil {
			return nil, err
		}
		_, err = output(ctx, "rsync", "-aHAXx", "--numeric-ids", "--no-specials", "--no-devices", "--safe-links",
			"--exclude=*.lock", "--exclude=*.sock", "--", v.to+"/", stage+"/")
		if err != nil {
			return nil, err
		}
		for i, name := range v.db {
			from, dest := filepath.Join(v.from, name), filepath.Join(stage, name)
			err := copyMetadataDB(ctx, from, dest)
			if err != nil && !(i > 0 && errors.Is(err, os.ErrNotExist)) {
				return nil, err
			}
		}
		from, to := filepath.Join(v.from, v.tree), filepath.Join(stage, v.tree)
		if err = realMetadataPath(from); err == nil {
			if err = realMetadataPath(to); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			if _, err = output(ctx, "rsync", "-aHAXx", "--numeric-ids", "--no-specials", "--no-devices", "--safe-links",
				"--", from+"/", to+"/"); err != nil {
				return nil, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if _, err = output(ctx, "sync", "-f", stage); err != nil {
			return nil, err
		}
		ino, err := inodeOf(stage)
		if err != nil {
			return nil, err
		}
		out[v.to] = metadataCopy{Stage: stage, Inode: ino}
	}
	return out, nil
}

func realMetadataPath(p string) error {
	actual, err := filepath.EvalSymlinks(p)
	if err == nil && actual != p {
		return fault("conflict", "元数据路径包含符号链接：%s", p)
	}
	return err
}

func copyMetadataDB(ctx context.Context, from, to string) (err error) {
	if err = realMetadataPath(from); err != nil {
		return err
	}
	var st unix.Stat_t
	if err = unix.Lstat(from, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		return fault("conflict", "元数据库不是普通文件：%s", from)
	}
	if err = realMetadataPath(filepath.Dir(to)); errors.Is(err, os.ErrNotExist) {
		if err = os.Mkdir(filepath.Dir(to), 0700); err != nil {
			return err
		}
		if err = os.Chown(filepath.Dir(to), int(st.Uid), int(st.Gid)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err = os.Remove(to + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	file, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_ = file.Close()
	dst, err := storage.BackupSQLite(ctx, from, to)
	if err != nil {
		return err
	}
	if err := verifyMetadataDB(ctx, dst); err != nil {
		_ = dst.Close()
		return err
	}
	if err = dst.Close(); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err = os.Chown(to+suffix, int(st.Uid), int(st.Gid)); err == nil {
			if err = os.Chmod(to+suffix, 0600); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) || suffix == "" {
			return err
		}
	}
	return nil
}

func verifyMetadataDB(ctx context.Context, db *sql.DB) error {
	var check string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		return fault("conflict", "元数据库校验失败：%s %v", check, err)
	}
	return nil
}

func publishMetadata(id, target string, data metadataCopy) error {
	current, err := inodeOf(target)
	if err == nil && current == data.Inode && data.Inode != 0 {
		return nil
	}
	if !containsPath(filepath.Dir(target), data.Stage) || filepath.Dir(target) != filepath.Dir(data.Stage) {
		return fault("conflict", "元数据暂存路径无效")
	}
	ino, e := inodeOf(data.Stage)
	if e != nil || ino != data.Inode {
		return fault("conflict", "元数据暂存目录身份不同：%s", data.Stage)
	}
	prior := target + ".pre-rollback-" + id
	if err == nil {
		if _, err := os.Lstat(prior); !errors.Is(err, os.ErrNotExist) {
			return fault("conflict", "旧元数据保留目录已存在：%s", prior)
		}
		if err = os.Rename(target, prior); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	} else if _, err := inodeOf(prior); err != nil {
		return fault("conflict", "原元数据目录及保留副本缺失")
	}
	if err := os.Rename(data.Stage, target); err != nil {
		return err
	}
	return localstate.SyncDirectory(filepath.Dir(target))
}

func metadataDrop(unit string) string {
	return filepath.Join(unitDirectory, unit+".d", "60-opsd-storage-root.conf")
}

func switchMetadataUnits(dir string) error {
	for _, name := range metadataUnits {
		path := metadataDrop(name)
		expected := "RequiresMountsFor=" + metadataSource.ExecutorDir
		if name == metadataServices[1] {
			expected = "ReadWritePaths=" + metadataSource.DataDir
		}
		body, err := os.ReadFile(path)
		backup := filepath.Join(dir, name+".i06-dropin")
		if !errors.Is(err, os.ErrNotExist) {
			if err != nil || !strings.Contains(string(body), expected) || !strings.Contains(string(body), "Requires=opsd-storage-check.service") {
				return fault("conflict", "I0.6 单元覆盖已变化：%s", path)
			}
			if _, e := os.Lstat(backup); !errors.Is(e, os.ErrNotExist) {
				return fault("conflict", "单元原始覆盖记录已存在：%s", backup)
			}
			if err = os.Rename(path, backup); err != nil {
				return err
			}
			if err = localstate.SyncDirectory(filepath.Dir(path)); err != nil {
				return err
			}
		} else if _, e := os.Lstat(backup); e != nil {
			return fault("conflict", "单元原始覆盖缺失：%s", path)
		}
	}
	return localstate.SyncDirectory(dir)
}

func writeMetadataConfig(path string, value map[string]json.RawMessage) error {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&0022 != 0 {
		return fault("conflict", "实例配置文件权限或身份不同")
	}
	temp := path + ".storage-root-rollback"
	if err := localstate.Write(temp, value, 0600); err != nil {
		return err
	}
	if err := os.Chown(temp, int(st.Uid), int(st.Gid)); err != nil {
		return err
	}
	if err := os.Chmod(temp, os.FileMode(st.Mode&0777)); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		return err
	}
	return localstate.SyncDirectory(filepath.Dir(path))
}

func metadataReadback(ctx context.Context, sock, id string, target metadataInstance) error {
	if sock == "" {
		sock = "/run/opsd-node/control.sock"
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://local/self", nil)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var actual map[string]string
	if err = json.NewDecoder(resp.Body).Decode(&actual); err != nil || resp.StatusCode != 200 || actual["id"] != id {
		return fault("conflict", "回退后节点身份读回不同")
	}
	for i, p := range []string{filepath.Join(target.ExecutorDir, "executor.db"), filepath.Join(target.DataDir, "node.db")} {
		pid, err := output(ctx, "systemctl", "show", "--property=MainPID", "--value", metadataServices[i])
		if err != nil || pid == "" || pid == "0" {
			return fault("dependency_unavailable", "回退后服务 %s 未运行", metadataServices[i])
		}
		fds, err := os.ReadDir(filepath.Join("/proc", pid, "fd"))
		if err != nil {
			return err
		}
		opened := slices.ContainsFunc(fds, func(fd os.DirEntry) bool {
			point, _ := os.Readlink(filepath.Join("/proc", pid, "fd", fd.Name()))
			return point == p
		})
		if !opened {
			return fault("conflict", "回退后服务 %s 未打开 %s", metadataServices[i], p)
		}
	}
	return nil
}
