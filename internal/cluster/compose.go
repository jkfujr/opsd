package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jkfujr/opsd/internal/protocol"
)

type ComposeSelection struct {
	ID       string `json:"id"`
	EngineID string `json:"engine_id"`
	Action   string `json:"action,omitempty"`
}

func (n *Node) registeredCompose(ctx context.Context, id string) (protocol.ComposeRef, error) {
	var body string
	var ref protocol.ComposeRef
	err := n.DB.QueryRowContext(ctx, "SELECT body FROM compose_projects WHERE id=?", id).Scan(&body)
	if err == nil {
		err = json.Unmarshal([]byte(body), &ref)
	}
	return ref, err
}
func (n *Node) composeProjects(ctx context.Context) ([]protocol.ComposeProject, error) {
	response, err := n.executeRead(ctx, &protocol.ComposeRead{})
	if err != nil {
		return nil, err
	}
	result, ok := response.(*protocol.ComposeReadResult)
	if !ok {
		return nil, fmt.Errorf("Compose 应答无效")
	}
	rows, err := n.DB.QueryContext(ctx, "SELECT body FROM compose_projects ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	registered := map[string]protocol.ComposeRef{}
	for rows.Next() {
		var body string
		var ref protocol.ComposeRef
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(body), &ref); err != nil {
			return nil, err
		}
		registered[ref.ID] = ref
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	projects := result.Projects
	if projects == nil {
		projects = []protocol.ComposeProject{}
	}
	for i := range projects {
		if _, ok := registered[projects[i].ID]; ok {
			projects[i].Registered = true
			delete(registered, projects[i].ID)
		}
	}
	for _, ref := range registered {
		projects = append(projects, protocol.ComposeProject{ComposeRef: ref, Registered: true, Services: []protocol.ContainerSummary{}, ReadOnlyReason: "已登记项目的实际容器或定义标签不可用", CheckedAt: n.Now().UTC()})
	}
	return projects, nil
}
func (n *Node) registerCompose(ctx context.Context, plain []byte, op *Operation) error {
	var selection ComposeSelection
	if err := json.Unmarshal(plain, &selection); err != nil {
		return err
	}
	response, err := n.executeRead(ctx, &protocol.ComposeRead{})
	if err != nil {
		return err
	}
	result, ok := response.(*protocol.ComposeReadResult)
	if !ok {
		return fmt.Errorf("Compose 应答无效")
	}
	var ref *protocol.ComposeRef
	for _, project := range result.Projects {
		if project.ID == selection.ID && project.EngineID == selection.EngineID {
			if project.ReadOnlyReason != "" {
				return fault("conflict", project.ReadOnlyReason)
			}
			value := project.ComposeRef
			ref = &value
		}
	}
	if ref == nil {
		return fault("not_found", "实际 Compose 项目不存在")
	}
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	body, _ := json.Marshal(ref)
	if _, err = tx.ExecContext(ctx, "INSERT INTO compose_projects(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", ref.ID, string(body)); err != nil {
		return err
	}
	op.State = protocol.Succeeded
	op.Revision++
	op.ResourceID = ref.ID
	op.UpdatedAt = n.Now().UTC()
	record, _ := json.Marshal(op)
	if _, err = tx.ExecContext(ctx, "UPDATE queue SET body=?,phase='finished',terminal=1 WHERE id=? AND terminal=0", string(record), op.ID); err != nil {
		return err
	}
	if err = n.appendTx(ctx, tx, "operation", op.ID, op, n.Now()); err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		n.notifyChange(Change{Resource: "compose", NodeID: n.ID})
		n.notifyChange(Change{Resource: "operations", NodeID: n.ID})
	}
	return err
}
