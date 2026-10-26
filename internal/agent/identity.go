package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/opsd-labs/opsd/internal/identity"
)

type Identity struct {
	Key             identity.Key
	ControlURL      string
	ControlPublic   []byte
	EnrollmentToken string
	NodeID          string
}

func ValidateAddress(address string) error {
	value, err := url.Parse(address)
	if err != nil || value.Scheme != "http" || value.Host == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" || (value.Path != "" && value.Path != "/") {
		return fmt.Errorf("控制端地址须为不含路径及凭据的 HTTP 地址")
	}
	return nil
}

func Initialize(ctx context.Context, db *sql.DB, address, public, token string) (identity.Key, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM agent_identity").Scan(&count); err != nil {
		return identity.Key{}, err
	}
	if count != 0 {
		value, err := Load(ctx, db)
		return value.Key, err
	}
	if address != "" {
		if err := ValidateAddress(address); err != nil {
			return identity.Key{}, err
		}
	}
	if public != "" {
		if _, err := identity.Decode(public); err != nil {
			return identity.Key{}, err
		}
	}
	key, err := identity.New()
	if err != nil {
		return identity.Key{}, err
	}
	_, err = db.ExecContext(ctx, "INSERT INTO agent_identity(id,private_key,public_key,control_url,control_public_key,enrollment_token) VALUES(1,?,?,?,?,?)", key.Private, key.Public, strings.TrimRight(address, "/"), public, token)
	return key, err
}

func Load(ctx context.Context, db *sql.DB) (Identity, error) {
	var value Identity
	var private, public []byte
	var controlPublic string
	err := db.QueryRowContext(ctx, "SELECT private_key,public_key,control_url,control_public_key,enrollment_token,node_id FROM agent_identity WHERE id=1").Scan(&private, &public, &value.ControlURL, &controlPublic, &value.EnrollmentToken, &value.NodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return value, fmt.Errorf("Agent 身份不存在，请先安装；不会自动生成替代身份")
	}
	if err != nil {
		return value, err
	}
	value.Key, err = identity.Load(private, public)
	if err != nil {
		return value, err
	}
	if err = ValidateAddress(value.ControlURL); err != nil {
		return value, err
	}
	value.ControlPublic, err = identity.Decode(controlPublic)
	return value, err
}
