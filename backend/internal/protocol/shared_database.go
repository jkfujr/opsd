package protocol

import (
	"fmt"
	"regexp"
)

const SharedManagementVersion = 1
const SharedDatabaseName = "opsd_core"

var sharedNodeID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type SharedDatabaseAction struct {
	Mode         string `json:"mode"`
	Cluster      string `json:"cluster"`
	Database     string `json:"database"`
	NodeID       string `json:"node_id,omitempty"`
	PasswordHash string `json:"password_hash,omitempty"`
	MonitorHash  string `json:"monitor_hash,omitempty"`
}

func (a SharedDatabaseAction) Validate() error {
	if a.Database != SharedDatabaseName || a.Cluster == "" {
		return fmt.Errorf("OPSD 数据库名称或身份无效")
	}
	switch a.Mode {
	case "initialize":
		return nil
	case "provision":
		if sharedNodeID.MatchString(a.NodeID) && databasePasswordHash.MatchString(a.PasswordHash) && databasePasswordHash.MatchString(a.MonitorHash) {
			return nil
		}
	case "revoke", "repair":
		if sharedNodeID.MatchString(a.NodeID) {
			return nil
		}
	}
	return fmt.Errorf("OPSD 数据维护动作无效")
}
func SharedUsername(node string) string    { return "opsd_node_" + node }
func SharedMonitorUser(node string) string { return "opsd_mon_" + node }
