package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/jkfujr/opsd/internal/protocol"
)

// 每次事务最多处理 200 条，保留每次真实变化的首条及连续状态的最新一条。
// 删除仅限已由上传流程确认的本机副本，不改签名内容、CouchDB 或操作记录。
func (n *Node) compactFirewallRecords(ctx context.Context) error {
	tx, err := n.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cursor uint64
	var fingerprint, anchor, previous string
	if err = tx.QueryRowContext(ctx, "SELECT sequence,fingerprint,anchor_id,previous_id FROM state_cleanup WHERE name='firewall'").Scan(&cursor, &fingerprint, &anchor, &previous); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,sequence,body FROM records WHERE kind='firewall' AND object=? AND sequence>? ORDER BY sequence LIMIT 200", n.ID, cursor)
	if err != nil {
		return err
	}
	type entry struct {
		id       string
		sequence uint64
		body     string
	}
	items := []entry{}
	for rows.Next() {
		var v entry
		if err = rows.Scan(&v.id, &v.sequence, &v.body); err != nil {
			break
		}
		items = append(items, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		var record Record
		var state protocol.ManagedFirewallState
		if err = json.Unmarshal([]byte(item.body), &record); err != nil {
			return err
		}
		if err = json.Unmarshal(record.Data, &state); err != nil {
			return err
		}
		sum := sha256.Sum256(firewallContent(state))
		current := hex.EncodeToString(sum[:])
		if current == fingerprint && previous != "" && previous != anchor {
			if _, err = tx.ExecContext(ctx, "DELETE FROM records WHERE id=? AND uploaded=1 AND kind='firewall'", previous); err != nil {
				return err
			}
		} else if current != fingerprint {
			anchor = item.id
		}
		cursor, fingerprint, previous = item.sequence, current, item.id
	}
	if len(items) > 0 {
		if _, err = tx.ExecContext(ctx, "UPDATE state_cleanup SET sequence=?,fingerprint=?,anchor_id=?,previous_id=? WHERE name='firewall'", cursor, fingerprint, anchor, previous); err != nil {
			return err
		}
	}
	return tx.Commit()
}
