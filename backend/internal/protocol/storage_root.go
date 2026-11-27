package protocol

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const (
	StorageArchive  = "archive"
	StoragePoolData = "pool_data"
	StorageDatabase = "database_data"
)

var storageRootID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type StorageRootInput struct {
	Action           string `json:"action"`
	MountPoint       string `json:"mount_point,omitempty"`
	RelativeDir      string `json:"relative_dir,omitempty"`
	Component        string `json:"component,omitempty"`
	ComponentID      string `json:"component_id,omitempty"`
	TargetRootID     string `json:"target_root_id,omitempty"`
	ExpectedRevision uint64 `json:"expected_revision,omitempty"`
}

func (r StorageRootInput) Validate() error {
	switch r.Action {
	case "register":
		if r.Component == "" && r.ComponentID == "" && r.TargetRootID == "" &&
			path.IsAbs(r.MountPoint) && r.MountPoint != "/" && cleanStoragePath(r.MountPoint) &&
			(r.RelativeDir == "" || !path.IsAbs(r.RelativeDir) && cleanStoragePath(r.RelativeDir)) {
			return nil
		}
	case "move":
		if r.MountPoint == "" && r.RelativeDir == "" && storageRootID.MatchString(r.TargetRootID) &&
			storageRootID.MatchString(r.ComponentID) &&
			(r.Component == StorageArchive || r.Component == StoragePoolData || r.Component == StorageDatabase) {
			return nil
		}
	}
	return fmt.Errorf("存储根操作、路径或组件无效")
}

func cleanStoragePath(p string) bool {
	return len(p) <= 1024 && path.Clean(p) == p && p != "." && p != ".." &&
		!strings.HasPrefix(p, "../") && !strings.ContainsAny(p, "\x00\r\n:\\")
}

type StorageRoot struct {
	ID          string `json:"root_id"`
	MountPoint  string `json:"mount_point"`
	RelativeDir string `json:"relative_dir"`
	UUID        string `json:"fs_uuid"`
	Type        string `json:"fs_type"`
	Base        string `json:"base_path"`
	State       string `json:"state,omitempty"`
}

type StorageBinding struct {
	Component   string `json:"component"`
	ComponentID string `json:"component_id"`
	RootID      string `json:"root_id"`
	Directory   string `json:"directory"`
}

type StorageRootProgress struct {
	ID          string         `json:"id"`
	Phase       string         `json:"phase"`
	Source      StorageBinding `json:"source"`
	Target      StorageBinding `json:"target"`
	WriteIntent bool           `json:"write_intent"`
	Error       string         `json:"error,omitempty"`
}

type StorageRootView struct {
	NodeID       string               `json:"node_id"`
	Revision     uint64               `json:"revision"`
	MetadataRoot string               `json:"metadata_root"`
	Roots        []StorageRoot        `json:"roots"`
	Bindings     []StorageBinding     `json:"bindings"`
	Pending      *StorageRootProgress `json:"pending,omitempty"`
}

type StorageRootRead struct{}

func (*StorageRootRead) Kind() Kind { return "storage.root.read" }
func (*StorageRootRead) isPayload() {}
func (*StorageRootView) Kind() Kind { return "storage.root.view" }
func (*StorageRootView) isPayload() {}

type StorageRootWrite struct {
	ID        string           `json:"id"`
	Input     StorageRootInput `json:"input"`
	CheckOnly bool             `json:"check_only,omitempty"`
}

func (*StorageRootWrite) Kind() Kind { return "storage.root.write" }
func (*StorageRootWrite) isPayload() {}

type StorageRootResult struct {
	State string `json:"state"`
}

func (*StorageRootResult) Kind() Kind { return "storage.root.result" }
func (*StorageRootResult) isPayload() {}
