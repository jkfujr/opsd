package storageroot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/firewall"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/operations"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storage"
)

type Manager struct {
	Config, Binary, NodeDir, ExecutorDir string
	Docker                               *dockerengine.Manager
	Store                                operations.Store
}

type binding struct {
	protocol.StorageBinding
	Identity protocol.StorageRoot           `json:"identity"`
	Guarded  bool                           `json:"guarded,omitempty"`
	Record   string                         `json:"record,omitempty"`
	Archive  *protocol.MonitoringDeployment `json:"archive,omitempty"`
	Paused   bool                           `json:"paused,omitempty"`
	Instance string                         `json:"instance,omitempty"`
}

type registry struct {
	Revision uint64                 `json:"revision"`
	Roots    []protocol.StorageRoot `json:"roots"`
	Bindings []binding              `json:"bindings"`
	Pending  string                 `json:"pending_id,omitempty"`
}

type record struct {
	protocol.StorageRootProgress
	Input         protocol.StorageRootInput            `json:"input"`
	Registration  *protocol.StorageRoot                `json:"registration,omitempty"`
	Original      binding                              `json:"original"`
	From          binding                              `json:"from"`
	To            binding                              `json:"to"`
	Definition    definition                           `json:"deployment"`
	Before        []dockerengine.StorageContainerState `json:"containers_before,omitempty"`
	StopIntent    bool                                 `json:"stop_intent,omitempty"`
	Reverse       bool                                 `json:"reverse,omitempty"`
	ReversePhase  string                               `json:"previous_phase,omitempty"`
	StepTimes     map[string]int64                     `json:"step_ms,omitempty"`
	StageInode    uint64                               `json:"stage_inode,omitempty"`
	OriginalInode uint64                               `json:"original_inode,omitempty"`
	Copied        inventory                            `json:"verified_data,omitempty"`
	Restored      bool                                 `json:"restored,omitempty"`
}

func fault(code, format string, args ...any) error {
	return &firewall.Fault{Code: code, Message: fmt.Sprintf(format, args...)}
}

func (m *Manager) stateDir() string { return filepath.Join(filepath.Dir(m.Config), "storage-root") }

func (m *Manager) paths(ctx context.Context) (registry, error) {
	r := registry{Revision: 1, Roots: []protocol.StorageRoot{}, Bindings: []binding{}}
	err := localstate.Read(filepath.Join(m.stateDir(), "roots.json"), &r)
	if !errors.Is(err, os.ErrNotExist) {
		return r, err
	}
	point, err := covering(storage.DefaultDatasetDir)
	if err == nil {
		dir, _ := filepath.Rel(point.Point, storage.DefaultDatasetDir)
		if dir == "." {
			dir = ""
		}
		if root, err := captureRoot(ctx, "default", point.Point, dir); err == nil {
			r.Roots = append(r.Roots, root)
		}
	}
	return r, nil
}

func (m *Manager) save(r *registry) error {
	r.Revision++
	return localstate.Write(filepath.Join(m.stateDir(), "roots.json"), r, 0600)
}

func (m *Manager) readRecord(id string) (record, error) {
	var r record
	if !operations.ValidID(id) {
		return r, fault("invalid_request", "存储迁移标识无效")
	}
	err := localstate.Read(filepath.Join(m.stateDir(), "requests", id+".json"), &r)
	return r, err
}

func (m *Manager) saveRecord(r *record) error {
	r.Source, r.Target = r.From.StorageBinding, r.To.StorageBinding
	return localstate.Write(filepath.Join(m.stateDir(), "requests", r.ID+".json"), r, 0600)
}

func (r registry) lookup(component, id string) (binding, bool) {
	for _, b := range r.Bindings {
		if b.Component == component && b.ComponentID == id {
			return b, true
		}
	}
	return binding{}, false
}

func (r *registry) setBinding(b binding) {
	for i := range r.Bindings {
		if r.Bindings[i].Component == b.Component && r.Bindings[i].ComponentID == b.ComponentID {
			r.Bindings[i] = b
			return
		}
	}
	r.Bindings = append(r.Bindings, b)
}

func (r registry) root(id string) (protocol.StorageRoot, error) {
	for _, root := range r.Roots {
		if root.ID == id {
			return root, nil
		}
	}
	return protocol.StorageRoot{}, fault("invalid_request", "存储根 %s 未登记", id)
}

func (m *Manager) pending(r registry) (*record, error) {
	if r.Pending == "" {
		return nil, nil
	}
	p, err := m.readRecord(r.Pending)
	if err != nil {
		return nil, err
	}
	switch p.Phase {
	case "registered", "complete", "rolled_back":
		return nil, nil
	}
	return &p, nil
}

func (m *Manager) Read(ctx context.Context) (protocol.StorageRootView, error) {
	out := protocol.StorageRootView{MetadataRoot: filepath.Dir(m.NodeDir), Roots: []protocol.StorageRoot{}, Bindings: []protocol.StorageBinding{}}
	r, err := m.paths(ctx)
	if err != nil {
		return out, err
	}
	out.Revision = r.Revision
	for _, root := range r.Roots {
		root.State = "ready"
		if m.checkRoot(ctx, root) != nil {
			root.State = "unavailable"
		}
		out.Roots = append(out.Roots, root)
	}
	for _, b := range r.Bindings {
		out.Bindings = append(out.Bindings, b.StorageBinding)
	}
	deployed, err := m.deployed(ctx)
	if err != nil {
		return out, err
	}
	for _, d := range deployed {
		component, id := d.key()
		if _, found := r.lookup(component, id); !found && d.enabled() {
			out.Bindings = append(out.Bindings, protocol.StorageBinding{Component: component, ComponentID: id,
				Directory: d.directory(), RootID: r.containing(d.directory()).ID})
		}
	}
	pending, err := m.pending(r)
	if pending != nil {
		out.Pending = &pending.StorageRootProgress
	}
	return out, err
}

// Directory 是既有部署器在创建绑定或打开写入前的挂载检查。
func (m *Manager) Directory(ctx context.Context, component, id string, write bool) (string, error) {
	r, err := m.paths(ctx)
	if err != nil {
		return "", err
	}
	if write {
		p, err := m.pending(r)
		if err != nil {
			return "", err
		}
		if p != nil && p.From.Component == component && p.From.ComponentID == id {
			return "", fault("node_busy", "组件存在待处理存储迁移 %s", p.ID)
		}
	}
	b, found := r.lookup(component, id)
	if !found {
		return "", nil
	}
	initialArchive := component == protocol.StorageArchive && b.Archive == nil && b.Record == ""
	if err = m.validateBinding(ctx, b, initialArchive); err != nil {
		return b.Directory, err
	}
	return b.Directory, nil
}

func (m *Manager) Write(ctx context.Context, req protocol.StorageRootWrite) (protocol.StorageRootResult, error) {
	if err := req.Input.Validate(); err != nil {
		return protocol.StorageRootResult{}, fault("invalid_request", "%v", err)
	}
	if !operations.ValidID(req.ID) || req.Input.ExpectedRevision == 0 {
		return protocol.StorageRootResult{}, fault("invalid_request", "存储操作标识或修订无效")
	}
	if !req.CheckOnly {
		unlock, err := localstate.Lock(filepath.Join(m.stateDir(), "lock"))
		if err != nil {
			return protocol.StorageRootResult{}, fault("node_busy", "%v", err)
		}
		defer unlock()
	}
	r, err := m.paths(ctx)
	if err != nil {
		return protocol.StorageRootResult{}, err
	}
	rec, err := m.readRecord(req.ID)
	found := err == nil
	if found {
		if rec.Input != req.Input {
			return protocol.StorageRootResult{}, fault("conflict", "原申请的参数不同")
		}
		if r.Pending == req.ID || rec.Phase == "registered" || rec.Phase == "complete" || rec.Phase == "rolled_back" {
			return protocol.StorageRootResult{State: rec.Phase}, nil
		}
		if rec.Registration != nil && slices.Contains(r.Roots, *rec.Registration) {
			rec.Phase = "registered"
			if !req.CheckOnly {
				if err := m.saveRecord(&rec); err != nil {
					return protocol.StorageRootResult{}, err
				}
			}
			return protocol.StorageRootResult{State: rec.Phase}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return protocol.StorageRootResult{}, err
	}
	if p, err := m.pending(r); err != nil {
		return protocol.StorageRootResult{}, err
	} else if p != nil {
		return protocol.StorageRootResult{}, fault("conflict", "节点已有存储迁移 %s（%s）", p.ID, p.Phase)
	}
	if r.Revision != req.Input.ExpectedRevision {
		return protocol.StorageRootResult{}, fault("precondition_failed", "存储根修订已经改变")
	}
	rec.ID, rec.Input = req.ID, req.Input
	if req.Input.Action == "register" {
		return m.register(ctx, &r, &rec, req.CheckOnly)
	}
	if !found {
		if err = m.prepareRequest(ctx, r, &rec); err != nil {
			return protocol.StorageRootResult{}, err
		}
	}
	if !req.CheckOnly {
		if err := m.saveRecord(&rec); err != nil {
			return protocol.StorageRootResult{}, err
		}
		r.Pending = rec.ID
		if err := m.save(&r); err != nil {
			return protocol.StorageRootResult{}, err
		}
	}
	return protocol.StorageRootResult{State: "pending_restart"}, nil
}

func (m *Manager) register(ctx context.Context, r *registry, rec *record, dry bool) (protocol.StorageRootResult, error) {
	in := rec.Input
	hash := sha256.Sum256([]byte(filepath.Join(in.MountPoint, in.RelativeDir)))
	root, err := captureRoot(ctx, fmt.Sprintf("root-%x", hash[:12]), in.MountPoint, in.RelativeDir)
	if err != nil {
		return protocol.StorageRootResult{}, err
	}
	if rec.Registration != nil && *rec.Registration != root {
		return protocol.StorageRootResult{}, fault("dependency_unavailable", "登记期间挂载身份发生变化")
	}
	for _, p := range []string{m.NodeDir, m.ExecutorDir, filepath.Dir(m.Config), "/etc/systemd/system", "/var/lib/docker"} {
		if overlaps(p, root.Base) {
			return protocol.StorageRootResult{}, fault("invalid_request", "存储根 %s 与系统或元数据目录 %s 重叠", root.Base, p)
		}
	}
	present := false
	for _, prior := range r.Roots {
		if prior == root {
			present = true
		} else if overlaps(root.Base, prior.Base) || root.UUID == prior.UUID && root.MountPoint != prior.MountPoint {
			return protocol.StorageRootResult{}, fault("conflict", "存储根与已有登记 %s 重叠或 UUID 重复", prior.ID)
		}
	}
	if !dry {
		if !present {
			rec.Phase, rec.Registration = "registering", &root
			if err := m.saveRecord(rec); err != nil {
				return protocol.StorageRootResult{}, err
			}
			if err := os.MkdirAll(root.Base, 0755); err != nil {
				return protocol.StorageRootResult{}, err
			}
			if err := m.checkRoot(ctx, root); err != nil {
				return protocol.StorageRootResult{}, err
			}
			r.Roots = append(r.Roots, root)
			if err := m.save(r); err != nil {
				return protocol.StorageRootResult{}, err
			}
		}
		rec.Phase = "registered"
		if err := m.saveRecord(rec); err != nil {
			return protocol.StorageRootResult{}, err
		}
	}
	return protocol.StorageRootResult{State: "registered"}, nil
}

func (m *Manager) prepareRequest(ctx context.Context, r registry, rec *record) error {
	in := rec.Input
	root, err := r.root(in.TargetRootID)
	if err != nil {
		return err
	}
	if err = m.checkRoot(ctx, root); err != nil {
		return err
	}
	d, err := m.findDeployment(ctx, in.Component, in.ComponentID)
	if err != nil {
		return err
	}
	if !d.enabled() {
		return fault("conflict", "组件 %s 已停用", in.ComponentID)
	}
	from, exists := r.lookup(in.Component, in.ComponentID)
	if !exists {
		path := d.directory()
		identity := r.containing(path)
		if identity.ID == "" {
			at, e := covering(path)
			if e != nil {
				return e
			}
			identity, e = captureRoot(ctx, "", at.Point, "")
			if e != nil {
				return e
			}
		}
		from = binding{StorageBinding: protocol.StorageBinding{Component: in.Component, ComponentID: in.ComponentID, RootID: identity.ID, Directory: path}, Identity: identity}
	}
	to := from
	to.RootID, to.Directory, to.Identity, to.Guarded = root.ID, d.target(root.Base), root, true
	to.Record = rec.ID
	if overlaps(from.Directory, to.Directory) {
		return fault("conflict", "源与目标目录重叠：%s", to.Directory)
	}
	for _, prior := range r.Bindings {
		if overlaps(prior.Directory, to.Directory) {
			return fault("conflict", "目标目录已被 %s 占用", prior.ComponentID)
		}
	}
	rec.Original, rec.From, rec.To, rec.Definition = from, from, to, d
	rec.Phase = "pending_restart"
	return m.preflight(ctx, rec)
}

func (r registry) containing(path string) protocol.StorageRoot {
	for _, candidate := range r.Roots {
		if containsPath(candidate.Base, path) {
			return candidate
		}
	}
	return protocol.StorageRoot{}
}

func (m *Manager) validateBinding(ctx context.Context, b binding, absentOK bool) error {
	if !containsPath(b.Identity.Base, b.Directory) {
		return fault("invalid_request", "组件目录超出登记根：%s", b.Directory)
	}
	if err := m.checkRoot(ctx, b.Identity); err != nil {
		return err
	}
	if err := realDir(b.Directory, absentOK); err != nil {
		return err
	}
	if b.Component == protocol.StoragePoolData {
		d, err := m.pool(b.ComponentID)
		if err != nil {
			return err
		}
		if _, err = d.Pool.activeLoop(ctx, b.Directory); err != nil {
			return err
		}
		all, err := mounts()
		if err != nil {
			return err
		}
		for _, at := range all {
			if at.Point != b.Identity.MountPoint && at.Point != filepath.Join(b.Directory, "data") && overlaps(at.Point, b.Directory) &&
				containsPath(b.Identity.MountPoint, at.Point) {
				return fault("conflict", "池数据目录含非受管挂载 %s", at.Point)
			}
		}
		return nil
	}
	return noNestedMount(b.Directory, b.Identity.MountPoint, true)
}

func (m *Manager) deployed(ctx context.Context) ([]definition, error) {
	if m.Store.DB == nil {
		return nil, fmt.Errorf("执行器部署记录不可用")
	}
	rows, err := m.Store.DB.QueryContext(ctx, `SELECT action FROM operations WHERE terminal=1
		AND json_extract(record,'$.state')='succeeded'
		AND (json_type(action,'$.monitoring')='object' OR json_type(action,'$.database.deployment')='object')
		ORDER BY rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []definition{}
	for rows.Next() {
		var raw string
		var a protocol.OperationAction
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &a); err != nil {
			return nil, err
		}
		d := definition{Archive: a.Monitoring}
		if a.Database != nil {
			d.Database = a.Database.Deployment
		}
		component, id := d.key()
		if !slices.ContainsFunc(out, func(s definition) bool { c, k := s.key(); return c == component && k == id }) {
			out = append(out, d)
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(m.ExecutorDir, "pools"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if d, err := m.pool(entry.Name()); err == nil {
				out = append(out, d)
			}
		}
	}
	return out, nil
}

func (m *Manager) findDeployment(ctx context.Context, component, id string) (definition, error) {
	if component == protocol.StoragePoolData {
		return m.pool(id)
	}
	deployed, err := m.deployed(ctx)
	if err != nil {
		return definition{}, err
	}
	for _, d := range deployed {
		if c, k := d.key(); component == c && id == k {
			return d, nil
		}
	}
	return definition{}, fault("conflict", "组件 %s/%s 缺少已完成的受管部署记录", component, id)
}
