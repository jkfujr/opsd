// Package apiauth 保存 API 令牌摘要并统一判断模块与资源权限。
package apiauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("令牌无效、已停用或已过期")
var ErrInvalid = errors.New("令牌名称、权限或资源范围无效")
var ErrConflict = errors.New("令牌已经改变，请刷新")

type Grant struct {
	Module    string   `json:"module"`
	Level     string   `json:"level"`
	Scope     string   `json:"scope"`
	Resources []string `json:"resources"`
}
type Token struct {
	ID        string     `json:"id"`
	Owner     string     `json:"owner"`
	Name      string     `json:"name"`
	Enabled   bool       `json:"enabled"`
	Revision  uint64     `json:"revision"`
	Grants    []Grant    `json:"grants"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	SourceIPs []string   `json:"source_ips"`
	CreatedAt time.Time  `json:"created_at"`
}
type Input struct {
	Name      string     `json:"name"`
	Enabled   bool       `json:"enabled"`
	Revision  uint64     `json:"revision"`
	Grants    []Grant    `json:"grants"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	SourceIPs []string   `json:"source_ips"`
}
type Store struct {
	DB    Database
	Owner string
	Now   func() time.Time
}

type Database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
func random() string {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func hash(raw string) []byte { h := sha256.Sum256([]byte(raw)); return h[:] }
func Owner(raw string) string {
	parts := strings.Split(raw, ".")
	if len(parts) != 4 || parts[0] != "opsd" || len(parts[1]) != 32 || len(parts[2]) != 64 || len(parts[3]) != 64 {
		return ""
	}
	for _, p := range parts[1:] {
		if _, e := hex.DecodeString(p); e != nil {
			return ""
		}
	}
	return parts[1]
}
func Validate(in Input) error {
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 128 || len(in.Grants) == 0 || len(in.Grants) > 7 || len(in.SourceIPs) > 32 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, g := range in.Grants {
		if !slices.Contains([]string{"monitoring", "nodes", "containers", "firewall", "databases", "tasks", "compose"}, g.Module) || seen[g.Module] || !slices.Contains([]string{"read", "manage"}, g.Level) || g.Module == "monitoring" && g.Level != "read" {
			return ErrInvalid
		}
		seen[g.Module] = true
		if !slices.Contains([]string{"all", "nodes", "regions", "clusters"}, g.Scope) || g.Module == "databases" && g.Scope != "all" && g.Scope != "clusters" || g.Module != "databases" && g.Scope == "clusters" || g.Scope != "all" && len(g.Resources) == 0 || len(g.Resources) > 1000 {
			return ErrInvalid
		}
		for _, r := range g.Resources {
			if r == "" || len(r) > 255 {
				return ErrInvalid
			}
		}
	}
	for _, ip := range in.SourceIPs {
		if _, e := netip.ParsePrefix(ip); e != nil {
			if _, e = netip.ParseAddr(ip); e != nil {
				return ErrInvalid
			}
		}
	}
	return nil
}
func (t Token) Allows(module, level, id, region string) bool {
	for _, g := range t.Grants {
		if g.Module != module || level == "manage" && g.Level != "manage" {
			continue
		}
		if g.Scope == "all" || (g.Scope == "nodes" || g.Scope == "clusters") && slices.Contains(g.Resources, id) || g.Scope == "regions" && slices.Contains(g.Resources, region) {
			return true
		}
	}
	return false
}
func (t Token) Has(module, level string) bool {
	for _, g := range t.Grants {
		if g.Module == module && (level == "read" || g.Level == "manage") {
			return true
		}
	}
	return false
}
func (t Token) Actor() string { return "token:" + t.Owner + ":" + t.ID }
func normalize(in Input) Input {
	if in.SourceIPs == nil {
		in.SourceIPs = []string{}
	}
	for i := range in.Grants {
		if in.Grants[i].Resources == nil {
			in.Grants[i].Resources = []string{}
		}
	}
	return in
}
func (s Store) List(ctx context.Context) ([]Token, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT body FROM api_tokens ORDER BY created_at DESC,id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		var b string
		var t Token
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &t); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s Store) Get(ctx context.Context, id string) (Token, error) {
	var b string
	var t Token
	e := s.DB.QueryRowContext(ctx, "SELECT body FROM api_tokens WHERE id=?", id).Scan(&b)
	if e == nil {
		e = json.Unmarshal([]byte(b), &t)
	}
	return t, e
}
func (s Store) Create(ctx context.Context, in Input) (Token, string, error) {
	if e := Validate(in); e != nil {
		return Token{}, "", e
	}
	in = normalize(in)
	t := Token{ID: random(), Owner: s.Owner, Name: strings.TrimSpace(in.Name), Enabled: in.Enabled, Revision: 1, Grants: in.Grants, ExpiresAt: in.ExpiresAt, SourceIPs: in.SourceIPs, CreatedAt: s.now()}
	if t.SourceIPs == nil {
		t.SourceIPs = []string{}
	}
	raw := "opsd." + s.Owner + "." + t.ID + "." + random()
	b, _ := json.Marshal(t)
	_, e := s.DB.ExecContext(ctx, "INSERT INTO api_tokens(id,token_hash,body,created_at,revision) VALUES(?,?,?,?,?)", t.ID, hash(raw), string(b), t.CreatedAt.Unix(), t.Revision)
	return t, raw, e
}
func (s Store) Update(ctx context.Context, id string, in Input) (Token, error) {
	if e := Validate(in); e != nil {
		return Token{}, e
	}
	in = normalize(in)
	t, e := s.Get(ctx, id)
	if e != nil {
		return t, e
	}
	if t.Revision != in.Revision {
		return Token{}, ErrConflict
	}
	t.Name, t.Enabled, t.Grants, t.ExpiresAt, t.SourceIPs = strings.TrimSpace(in.Name), in.Enabled, in.Grants, in.ExpiresAt, in.SourceIPs
	t.Revision++
	b, _ := json.Marshal(t)
	r, e := s.DB.ExecContext(ctx, "UPDATE api_tokens SET body=?,revision=? WHERE id=? AND revision=?", string(b), t.Revision, id, in.Revision)
	if e == nil {
		count, _ := r.RowsAffected()
		if count != 1 {
			e = ErrConflict
		}
	}
	return t, e
}
func (s Store) Regenerate(ctx context.Context, id string, revision uint64) (Token, string, error) {
	t, e := s.Get(ctx, id)
	if e != nil {
		return t, "", e
	}
	if t.Revision != revision {
		return t, "", ErrConflict
	}
	t.Revision++
	raw := "opsd." + s.Owner + "." + t.ID + "." + random()
	b, _ := json.Marshal(t)
	r, e := s.DB.ExecContext(ctx, "UPDATE api_tokens SET token_hash=?,body=?,revision=? WHERE id=? AND revision=?", hash(raw), string(b), t.Revision, id, revision)
	if e == nil {
		count, _ := r.RowsAffected()
		if count != 1 {
			e = ErrConflict
		}
	}
	return t, raw, e
}
func (s Store) Delete(ctx context.Context, id string, revision uint64) error {
	r, e := s.DB.ExecContext(ctx, "DELETE FROM api_tokens WHERE id=? AND revision=?", id, revision)
	if e == nil {
		count, _ := r.RowsAffected()
		if count != 1 {
			return ErrConflict
		}
	}
	return e
}
func (s Store) Verify(ctx context.Context, raw, sourceIP string) (Token, error) {
	if Owner(raw) != s.Owner {
		return Token{}, ErrUnauthorized
	}
	var b string
	var t Token
	if e := s.DB.QueryRowContext(ctx, "SELECT body FROM api_tokens WHERE token_hash=?", hash(raw)).Scan(&b); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return t, ErrUnauthorized
		}
		return t, e
	}
	if e := json.Unmarshal([]byte(b), &t); e != nil {
		return t, e
	}
	if !t.Enabled || t.ExpiresAt != nil && !s.now().Before(*t.ExpiresAt) {
		return Token{}, ErrUnauthorized
	}
	if len(t.SourceIPs) > 0 {
		ip, e := netip.ParseAddr(sourceIP)
		if e != nil {
			return Token{}, ErrUnauthorized
		}
		allowed := false
		for _, entry := range t.SourceIPs {
			if p, e := netip.ParsePrefix(entry); e == nil {
				allowed = allowed || p.Contains(ip.Unmap())
			} else if a, e := netip.ParseAddr(entry); e == nil {
				allowed = allowed || a.Unmap() == ip.Unmap()
			}
		}
		if !allowed {
			return Token{}, ErrUnauthorized
		}
	}
	return t, nil
}
