package cluster

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/jkfujr/opsd/internal/protocol"
)

// describeOperation 在受理事务内保存动作和原目标，不在读取历史时猜测或补写。
func (n *Node) describeOperation(ctx context.Context, tx *sql.Tx, r Request, name string, op *Operation) error {
	logs := r.Kind == "task_run" || r.Kind == "container" || r.Kind == "compose"
	op.SupportsLogs = &logs
	op.Action = r.Kind
	op.TargetName = name
	op.Resource = &OperationResource{Kind: "node", NodeID: n.ID, ID: n.ID}
	read := func(query string, id string, out any) error {
		var body string
		err := tx.QueryRowContext(ctx, query, id).Scan(&body)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal([]byte(body), out)
	}
	switch r.Kind {
	case "container":
		var action protocol.ContainerAction
		if err := json.Unmarshal(r.Payload, &action); err != nil {
			return err
		}
		op.Action = action.Action
		op.ResourceID = action.ContainerID
		op.Resource = &OperationResource{Kind: "container", NodeID: n.ID, ID: action.ContainerID, EngineID: action.EngineID}
	case "compose":
		var action protocol.ComposeAction
		if err := json.Unmarshal(r.Payload, &action); err != nil {
			return err
		}
		op.Action, op.TargetName = action.Action, action.Project.Name
		op.Resource = &OperationResource{Kind: "compose", NodeID: n.ID, ID: action.Project.ID, EngineID: action.Project.EngineID}
	case "compose_register":
		var selection ComposeSelection
		if err := json.Unmarshal(r.Payload, &selection); err != nil {
			return err
		}
		var ref protocol.ComposeRef
		if err := read("SELECT body FROM compose_projects WHERE id=?", selection.ID, &ref); err != nil {
			return err
		}
		op.TargetName = ref.Name
		op.Resource = &OperationResource{Kind: "compose", NodeID: n.ID, ID: selection.ID, EngineID: selection.EngineID}
	case "task_run":
		var action protocol.ShellAction
		if err := json.Unmarshal(r.Payload, &action); err != nil {
			return err
		}
		op.Action, op.TargetName = "run", action.Name
		op.Resource = &OperationResource{Kind: "task", NodeID: n.ID, ID: action.TaskID}
	case "task_save":
		var task ScheduledTask
		if err := read("SELECT body FROM scheduled_tasks WHERE id=?", r.ResourceID, &task); err != nil {
			return err
		}
		if task.ID == "" {
			if err := json.Unmarshal(r.Payload, &task); err != nil {
				return err
			}
		}
		op.Action, op.TargetName = "save", task.Name
		op.Resource = &OperationResource{Kind: "task", NodeID: n.ID, ID: r.ResourceID}
	case "database":
		var input DatabaseSubmission
		if err := json.Unmarshal(r.Payload, &input); err != nil {
			return err
		}
		op.Action, op.TargetName = input.Change.Kind, input.Cluster.Name
		op.Resource = &OperationResource{Kind: "database", NodeID: n.ID, ID: input.Cluster.ID}
	case "database_step":
		var action protocol.DatabaseAction
		if err := json.Unmarshal(r.Payload, &action); err != nil {
			return err
		}
		var cluster ManagedDatabaseCluster
		if err := read("SELECT body FROM managed_database_clusters WHERE id=?", r.ResourceID, &cluster); err != nil {
			return err
		}
		op.Action, op.TargetName = action.Kind, cluster.Name
		op.Resource = &OperationResource{Kind: "database", NodeID: n.ID, ID: r.ResourceID}
	case "component":
		var action protocol.ComponentAction
		if err := json.Unmarshal(r.Payload, &action); err != nil {
			return err
		}
		op.Action, op.TargetName = action.Action, action.Component
		op.Resource = &OperationResource{Kind: "component", NodeID: n.ID, ID: action.Component}
	case "firewall":
		var plan protocol.ChangePlan
		if err := json.Unmarshal(r.Payload, &plan); err != nil {
			return err
		}
		op.Action = plan.Change.Kind
		op.Resource = &OperationResource{Kind: "component", NodeID: n.ID, ID: "firewall"}
		var body string
		if err := tx.QueryRowContext(ctx, "SELECT config FROM node_identity WHERE id=1").Scan(&body); err != nil {
			return err
		}
		var config Config
		if err := json.Unmarshal([]byte(body), &config); err != nil {
			return err
		}
		op.TargetName = config.Name
	default:
		var body string
		if err := tx.QueryRowContext(ctx, "SELECT config FROM node_identity WHERE id=1").Scan(&body); err != nil {
			return err
		}
		var config Config
		if err := json.Unmarshal([]byte(body), &config); err != nil {
			return err
		}
		op.TargetName = config.Name
	}
	return nil
}
