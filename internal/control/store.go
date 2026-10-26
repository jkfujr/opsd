package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/opsd-labs/opsd/internal/account"
	"github.com/opsd-labs/opsd/internal/httpapi"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/protocol"
)

var ErrToken = errors.New("接入码无效、已消费或已过期")
var ErrRevoked = errors.New("节点身份已撤销")

func digest(value string) []byte { sum := sha256.Sum256([]byte(value)); return sum[:] }

func Initialize(ctx context.Context, db *sql.DB, username, password, entrance, localPublic, hostname string) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM account").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	if err := account.Validate(username, entrance); err != nil {
		return err
	}
	hash, err := account.Hash(password)
	if err != nil {
		return err
	}
	key, err := identity.New()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO account(id,username,password_hash,entrance) VALUES(1,?,?,?)", username, hash, entrance); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO control_identity(id,private_key,public_key) VALUES(1,?,?)", key.Private, key.Public); err != nil {
		return err
	}
	if localPublic != "" {
		if _, err = identity.Decode(localPublic); err != nil {
			return err
		}
		caps, _ := json.Marshal(protocol.Capabilities{})
		if _, err = tx.ExecContext(ctx, "INSERT INTO nodes(id,name,public_key,local,capabilities) VALUES(?,?,?,1,?)", identity.Random(), hostname, localPublic, string(caps)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func LoadKey(ctx context.Context, db *sql.DB) (identity.Key, error) {
	var private, public []byte
	if err := db.QueryRowContext(ctx, "SELECT private_key,public_key FROM control_identity WHERE id=1").Scan(&private, &public); err != nil {
		return identity.Key{}, fmt.Errorf("读取控制端身份：%w", err)
	}
	return identity.Load(private, public)
}

func (s *Server) register(ctx context.Context, public string, open *protocol.SessionOpen) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id string
	var revoked sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT id,revoked_at FROM nodes WHERE public_key=?", public).Scan(&id, &revoked)
	if err == nil && revoked.Valid {
		return "", ErrRevoked
	}
	if errors.Is(err, sql.ErrNoRows) {
		name := strings.TrimSpace(open.Hostname)
		if name == "" || len(name) > 255 {
			return "", fmt.Errorf("主机名无效")
		}
		now := s.now().Unix()
		result, e := tx.ExecContext(ctx, "UPDATE enrollment_tokens SET consumed_at=? WHERE token_hash=? AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>?", now, digest(open.EnrollmentToken), now)
		if e != nil {
			return "", e
		}
		affected, e := result.RowsAffected()
		if e != nil {
			return "", e
		}
		if affected != 1 {
			return "", ErrToken
		}
		id = identity.Random()
		caps, _ := json.Marshal(open.Capabilities)
		if _, err = tx.ExecContext(ctx, "INSERT INTO nodes(id,name,public_key,capabilities) VALUES(?,?,?,?)", id, name, public, string(caps)); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	caps, err := json.Marshal(open.Capabilities)
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE nodes SET capabilities=?,last_seen_at=? WHERE id=? AND revoked_at IS NULL", string(caps), s.now().Unix(), id); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (s *Server) nodes(ctx context.Context) ([]httpapi.Node, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,public_key,local,revoked_at,last_seen_at,capabilities FROM nodes ORDER BY local DESC,name,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []httpapi.Node{}
	for rows.Next() {
		var node httpapi.Node
		var revoked, last sql.NullInt64
		var caps string
		if err := rows.Scan(&node.Id, &node.Name, &node.PublicKey, &node.Local, &revoked, &last, &caps); err != nil {
			return nil, err
		}
		if revoked.Valid {
			value := time.Unix(revoked.Int64, 0).UTC()
			node.RevokedAt = &value
		}
		if last.Valid {
			value := time.Unix(last.Int64, 0).UTC()
			node.LastSeenAt = &value
		}
		var capabilities httpapi.Capabilities
		if err := json.Unmarshal([]byte(caps), &capabilities); err != nil {
			return nil, err
		}
		node.Capabilities = &capabilities
		s.mu.Lock()
		conn := s.connections[node.Id]
		node.Connected = conn != nil && !revoked.Valid
		node.Writable = node.Connected && conn.writable
		s.mu.Unlock()
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *Server) findNode(ctx context.Context, id string) (httpapi.Node, error) {
	nodes, err := s.nodes(ctx)
	if err != nil {
		return httpapi.Node{}, err
	}
	for _, node := range nodes {
		if node.Id == id {
			return node, nil
		}
	}
	return httpapi.Node{}, sql.ErrNoRows
}
