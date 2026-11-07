package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
)

type NavigationPreferences struct {
	Order    []string `json:"order"`
	Revision uint64   `json:"revision"`
}

var defaultNavigation = []string{"overview", "nodes", "containers", "databases", "storage", "firewall", "settings"}

func (n *Node) Navigation(ctx context.Context) (NavigationPreferences, error) {
	value := NavigationPreferences{Order: slices.Clone(defaultNavigation)}
	var order string
	e := n.DB.QueryRowContext(ctx, "SELECT revision,order_json FROM account_navigation WHERE account_id=1").Scan(&value.Revision, &order)
	if e == sql.ErrNoRows {
		return value, nil
	}
	if e == nil {
		e = json.Unmarshal([]byte(order), &value.Order)
	}
	return value, e
}

func (n *Node) SaveNavigation(ctx context.Context, value NavigationPreferences) (NavigationPreferences, error) {
	seen := map[string]bool{}
	for _, item := range value.Order {
		if !slices.Contains(defaultNavigation, item) || seen[item] {
			return value, fault("invalid_request", "菜单顺序须包含全部菜单且无重复")
		}
		seen[item] = true
	}
	if len(seen) != len(defaultNavigation) {
		return value, fault("invalid_request", "菜单顺序须包含全部菜单且无重复")
	}
	order, e := json.Marshal(value.Order)
	if e != nil {
		return value, e
	}
	tx, e := n.DB.BeginTx(ctx, nil)
	if e != nil {
		return value, e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, "UPDATE account_navigation SET order_json=?,revision=revision+1 WHERE account_id=1 AND revision=?", string(order), value.Revision)
	if e != nil {
		return value, e
	}
	count, e := result.RowsAffected()
	if e != nil {
		return value, e
	}
	if count == 0 && value.Revision == 0 {
		result, e = tx.ExecContext(ctx, "INSERT INTO account_navigation(account_id,revision,order_json) VALUES(1,1,?) ON CONFLICT(account_id) DO NOTHING", string(order))
		if e != nil {
			return value, e
		}
		count, e = result.RowsAffected()
		if e != nil {
			return value, e
		}
	}
	if count != 1 {
		return value, fault("conflict", "菜单顺序已改变，请重新读取")
	}
	if e = tx.Commit(); e != nil {
		return value, e
	}
	value.Revision++
	n.notifyChange(Change{Resource: "navigation", NodeID: n.ID})
	return value, nil
}
