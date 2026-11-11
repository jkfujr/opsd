package consoletheme

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/requeststate"
)

type Installation struct {
	InstallationID string    `json:"installation_id"`
	Manifest       Manifest  `json:"manifest"`
	InstalledAt    time.Time `json:"installed_at"`
	Source         string    `json:"source"`
	Available      bool      `json:"available"`
	Problem        string    `json:"problem,omitempty"`
	PreviewURL     string    `json:"preview_url,omitempty"`
	files          []archiveFile
}

type List struct {
	Items                   []Installation `json:"items"`
	ActiveInstallationID    string         `json:"active_installation_id"`
	EffectiveInstallationID string         `json:"effective_installation_id"`
	BuiltinURL              string         `json:"builtin_url"`
}

type Manager struct {
	db      *sql.DB
	root    string
	version string
	client  *http.Client
	mu      sync.RWMutex
}

// Open 在节点持有本机运行锁后调用，清理未登记的安装和未完成的删除。
func Open(ctx context.Context, db *sql.DB, directory, currentVersion string) (*Manager, error) {
	root, err := filepath.Abs(directory)
	if err != nil || directory == "" || filepath.Dir(root) == root {
		return nil, invalid("主题目录无效")
	}
	m := &Manager{db: db, root: root, version: currentVersion, client: downloadClient()}
	if err := os.MkdirAll(filepath.Join(root, "installed"), 0700); err != nil {
		return m, err
	}
	if err := m.cleanup(ctx); err != nil {
		return m, err
	}
	return m, nil
}

const columns = "installation_id,manifest_json,files_json,source,installed_at"

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Installation, error) {
	var item Installation
	var manifest, files string
	var installed int64
	if err := row.Scan(&item.InstallationID, &manifest, &files, &item.Source, &installed); err != nil {
		return item, err
	}
	if !installationID.MatchString(item.InstallationID) {
		return item, invalid("主题安装记录无效")
	}
	if err := json.Unmarshal([]byte(manifest), &item.Manifest); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(files), &item.files); err != nil {
		return item, err
	}
	item.InstalledAt = time.Unix(installed, 0).UTC()
	return item, nil
}

func (m *Manager) directory(id string) string { return filepath.Join(m.root, "installed", id) }
func AssetBase(id string) string              { return AssetsPath + id + "/" }

func (m *Manager) inspect(item *Installation) []byte {
	if err := item.Manifest.compatible(m.version); err != nil {
		item.Problem = err.Error()
		return nil
	}
	root, err := os.OpenRoot(m.directory(item.InstallationID))
	if err != nil {
		item.Problem = "主题文件不可用"
		return nil
	}
	defer root.Close()
	if len(item.files) < 2 {
		item.Problem = "主题文件记录无效"
		return nil
	}
	for _, file := range item.files {
		if !validPath(file.Path) {
			item.Problem = "主题文件记录无效"
			return nil
		}
		info, err := root.Lstat(filepath.FromSlash(file.Path))
		if err != nil || !info.Mode().IsRegular() || info.Size() != file.Size {
			item.Problem = "主题文件缺失或大小已改变"
			return nil
		}
	}
	f, err := root.Open(filepath.FromSlash("dist/index.html"))
	if err != nil {
		item.Problem = "主题入口不可用"
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxIndexSize+1))
	if err != nil || len(data) > maxIndexSize {
		item.Problem = "主题入口不可用"
		return nil
	}
	data, err = injectBase(data, AssetBase(item.InstallationID))
	if err != nil {
		item.Problem = err.Error()
		return nil
	}
	item.Available = true
	if item.Manifest.Preview != "" {
		u := url.URL{Path: AssetBase(item.InstallationID) + item.Manifest.Preview}
		item.PreviewURL = u.EscapedPath()
	}
	return data
}

func (m *Manager) List(ctx context.Context) (List, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := List{Items: []Installation{}, BuiltinURL: BuiltinPath}
	rows, err := m.db.QueryContext(ctx, "SELECT "+columns+" FROM console_themes ORDER BY installed_at DESC,installation_id")
	if err != nil {
		return list, err
	}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			rows.Close()
			return list, err
		}
		list.Items = append(list.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return list, err
	}
	if err := m.db.QueryRowContext(ctx, "SELECT COALESCE(installation_id,'') FROM console_theme_selection WHERE id=1").Scan(&list.ActiveInstallationID); err != nil {
		return list, err
	}
	for i := range list.Items {
		m.inspect(&list.Items[i])
		if list.Items[i].Available && list.Items[i].InstallationID == list.ActiveInstallationID {
			list.EffectiveInstallationID = list.ActiveInstallationID
		}
	}
	return list, nil
}

func (m *Manager) Install(ctx context.Context, archive io.Reader, source string) (Installation, error) {
	var item Installation
	stage, err := os.MkdirTemp(m.root, ".stage-")
	if err != nil {
		return item, err
	}
	defer os.RemoveAll(stage)
	filename := filepath.Join(stage, "package.zip")
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return item, err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(archive, MaxArchiveSize+1))
	closeErr := f.Close()
	if copyErr != nil {
		return item, copyErr
	}
	if closeErr != nil {
		return item, closeErr
	}
	if n > MaxArchiveSize {
		return item, invalid("主题 ZIP 不能超过 100 MiB")
	}
	write := requeststate.Current(ctx)
	if write != nil && source == "upload" {
		copy := *write
		digest := sha256.Sum256(append([]byte(copy.Digest+"/zip/"), hash.Sum(nil)...))
		copy.Digest = hex.EncodeToString(digest[:])
		write = &copy
	}
	if write != nil {
		if item, ok, err := m.replayInstallation(ctx, m.db, *write); ok || err != nil {
			return item, err
		}
	}
	content := filepath.Join(stage, "content")
	manifest, files, err := extract(ctx, filename, content, m.version)
	if err != nil {
		return item, err
	}
	if err := ctx.Err(); err != nil {
		return item, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return item, err
	}
	defer tx.Rollback()
	if write != nil {
		if item, ok, err := m.replayInstallation(ctx, tx, *write); ok || err != nil {
			return item, err
		}
	}
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM console_themes WHERE theme_id=? AND version=?", manifest.ID, manifest.Version).Scan(&exists); err != nil {
		return item, err
	}
	if exists != 0 {
		return item, &Error{Code: "conflict", Message: "该主题版本已安装，请重新读取列表"}
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return item, err
	}
	id := hex.EncodeToString(random[:])
	destination := m.directory(id)
	if err := os.Rename(content, destination); err != nil {
		return item, err
	}
	manifestJSON, _ := json.Marshal(manifest)
	filesJSON, _ := json.Marshal(files)
	installed := time.Now().UTC().Truncate(time.Second)
	_, err = tx.ExecContext(ctx, "INSERT INTO console_themes(installation_id,theme_id,version,manifest_json,files_json,source,installed_at) VALUES(?,?,?,?,?,?,?)", id, manifest.ID, manifest.Version, string(manifestJSON), string(filesJSON), source, installed.Unix())
	item = Installation{InstallationID: id, Manifest: manifest, InstalledAt: installed, Source: source, files: files}
	if err == nil && write != nil {
		data, _ := json.Marshal(item)
		err = requeststate.Save(ctx, tx, requeststate.Receipt{Write: *write, Kind: "theme", ResourceID: id, Result: data, CreatedAt: installed})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		// 数据库尚未登记；该目录不会用于页面或资源请求。
		os.RemoveAll(destination)
		return item, err
	}
	m.inspect(&item)
	return item, nil
}

func (m *Manager) replayInstallation(ctx context.Context, q requeststate.Querier, write requeststate.Write) (Installation, bool, error) {
	var item Installation
	prior, err := requeststate.Lookup(ctx, q, write)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	if errors.Is(err, requeststate.ErrConflict) {
		return item, false, &Error{Code: "conflict", Message: err.Error()}
	}
	if err != nil {
		return item, false, err
	}
	if prior.Kind != "theme" {
		return item, false, &Error{Code: "conflict", Message: "请求键已用于其他写入"}
	}
	err = json.Unmarshal(prior.Result, &item)
	return item, true, err
}

func (m *Manager) Activate(ctx context.Context, id string) error {
	return m.ActivateRevision(ctx, id, nil)
}
func (m *Manager) ActivateRevision(ctx context.Context, id string, expected *uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var value any
	if id != "" {
		if !installationID.MatchString(id) {
			return invalid("主题安装 ID 无效")
		}
		item, err := scan(m.db.QueryRowContext(ctx, "SELECT "+columns+" FROM console_themes WHERE installation_id=?", id))
		if err != nil {
			return err
		}
		m.inspect(&item)
		if !item.Available {
			return invalid(item.Problem)
		}
		value = id
	}
	var result sql.Result
	var err error
	if expected == nil {
		result, err = m.db.ExecContext(ctx, "UPDATE console_theme_selection SET installation_id=?,revision=revision+1 WHERE id=1", value)
	} else {
		result, err = m.db.ExecContext(ctx, "UPDATE console_theme_selection SET installation_id=?,revision=revision+1 WHERE id=1 AND revision=?", value, *expected)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return &Error{Code: "precondition_failed", Message: "主题选择已经改变，请重新读取"}
	}
	return err
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	return m.DeleteRevision(ctx, id, nil)
}
func (m *Manager) DeleteRevision(ctx context.Context, id string, expected *uint64) error {
	if !installationID.MatchString(id) {
		return invalid("主题安装 ID 无效")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active, existing string
	var installed int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(installation_id,'') FROM console_theme_selection WHERE id=1").Scan(&active); err != nil {
		return err
	}
	if active == id {
		return &Error{Code: "conflict", Message: "请先切换主题，再删除当前版本"}
	}
	if err := tx.QueryRowContext(ctx, "SELECT installation_id,installed_at FROM console_themes WHERE installation_id=?", id).Scan(&existing, &installed); err != nil {
		return err
	}
	if expected != nil && uint64(installed) != *expected {
		return &Error{Code: "precondition_failed", Message: "主题版本已经改变，请重新读取"}
	}
	trash := filepath.Join(m.root, ".trash-"+id)
	renamed := false
	if err := os.Rename(m.directory(id), trash); err == nil {
		renamed = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM console_themes WHERE installation_id=?", id); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if renamed {
			if restoreErr := os.Rename(trash, m.directory(id)); restoreErr != nil {
				return errors.Join(err, restoreErr)
			}
		}
		return err
	}
	if renamed {
		return os.RemoveAll(trash)
	}
	return nil
}

// ActiveIndex 在每次 HTML 请求时核对登记文件，缺失时交由调用者提供内置界面。
func (m *Manager) ActiveIndex(ctx context.Context) (Installation, []byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, err := scan(m.db.QueryRowContext(ctx, "SELECT "+columns+" FROM console_themes WHERE installation_id=(SELECT installation_id FROM console_theme_selection WHERE id=1)"))
	if errors.Is(err, sql.ErrNoRows) {
		return Installation{}, nil, nil
	}
	if err != nil {
		return item, nil, err
	}
	data := m.inspect(&item)
	return item, data, nil
}

func (m *Manager) Close() { m.client.CloseIdleConnections() }

func (m *Manager) OpenAsset(ctx context.Context, id, name string) (*os.File, error) {
	if !installationID.MatchString(id) || !validPath(name) {
		return nil, os.ErrNotExist
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var existing string
	if err := m.db.QueryRowContext(ctx, "SELECT installation_id FROM console_themes WHERE installation_id=?", id).Scan(&existing); err != nil {
		return nil, err
	}
	f, err := os.OpenInRoot(m.directory(id), filepath.Join("dist", filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

func (m *Manager) cleanup(ctx context.Context) error {
	registered := map[string]bool{}
	rows, err := m.db.QueryContext(ctx, "SELECT installation_id FROM console_themes")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		registered[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		full := filepath.Join(m.root, name)
		if strings.HasPrefix(name, ".stage-") {
			if err := os.RemoveAll(full); err != nil {
				return err
			}
		} else if id, ok := strings.CutPrefix(name, ".trash-"); ok && installationID.MatchString(id) {
			if registered[id] {
				if _, err := os.Lstat(m.directory(id)); errors.Is(err, os.ErrNotExist) {
					if err := os.Rename(full, m.directory(id)); err != nil {
						return err
					}
					continue
				} else if err != nil {
					return err
				}
			}
			if err := os.RemoveAll(full); err != nil {
				return err
			}
		}
	}
	entries, err = os.ReadDir(filepath.Join(m.root, "installed"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if installationID.MatchString(entry.Name()) && !registered[entry.Name()] {
			if err := os.RemoveAll(m.directory(entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
