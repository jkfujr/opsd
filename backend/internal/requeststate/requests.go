// Package requeststate 保存所属节点上的写入回执，不保存秘密原文。
package requeststate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrConflict = errors.New("同一请求键不能用于不同参数")

type Write struct {
	Actor  string `json:"actor"`
	Key    string `json:"key"`
	Digest string `json:"digest"`
}
type Receipt struct {
	Write
	Kind       string          `json:"kind"`
	ResourceID string          `json:"resource_id"`
	Result     json.RawMessage `json:"result"`
	Secret     bool            `json:"secret"`
	CreatedAt  time.Time       `json:"created_at"`
}
type Querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
type key struct{}

func With(ctx context.Context, write *Write) context.Context {
	if write == nil {
		return ctx
	}
	return context.WithValue(ctx, key{}, write)
}
func Current(ctx context.Context) *Write { w, _ := ctx.Value(key{}).(*Write); return w }
func Lookup(ctx context.Context, q Querier, write Write) (Receipt, error) {
	var r Receipt
	var at int64
	var body string
	err := q.QueryRowContext(ctx, "SELECT digest,kind,resource_id,result_json,secret,created_at FROM api_requests WHERE actor=? AND request_key=?", write.Actor, write.Key).Scan(&r.Digest, &r.Kind, &r.ResourceID, &body, &r.Secret, &at)
	if errors.Is(err, sql.ErrNoRows) {
		var digest []byte
		err = q.QueryRowContext(ctx, "SELECT request_digest,body FROM queue WHERE origin=? AND request_key=?", write.Actor, write.Key).Scan(&digest, &body)
		if err == nil {
			var operation struct {
				ID        string    `json:"id"`
				CreatedAt time.Time `json:"created_at"`
			}
			if err = json.Unmarshal([]byte(body), &operation); err != nil {
				return r, err
			}
			r.Digest = fmt.Sprintf("%x", digest)
			r.Kind, r.ResourceID, r.CreatedAt = "operation", operation.ID, operation.CreatedAt
			at = operation.CreatedAt.Unix()
		}
	}
	r.Actor, r.Key = write.Actor, write.Key
	r.Result = json.RawMessage(body)
	r.CreatedAt = time.Unix(at, 0).UTC()
	if err == nil && write.Digest != "" && r.Digest != write.Digest {
		return r, ErrConflict
	}
	return r, err
}
func Save(ctx context.Context, tx Executor, r Receipt) error {
	_, e := tx.ExecContext(ctx, "INSERT INTO api_requests(actor,request_key,digest,kind,resource_id,result_json,secret,created_at) VALUES(?,?,?,?,?,?,?,?)", r.Actor, r.Key, r.Digest, r.Kind, r.ResourceID, string(r.Result), r.Secret, r.CreatedAt.Unix())
	return e
}

func Finish(ctx context.Context, tx Executor, r Receipt) error {
	_, e := tx.ExecContext(ctx, "UPDATE api_requests SET result_json=?,resource_id=?,secret=? WHERE actor=? AND request_key=? AND digest=?", string(r.Result), r.ResourceID, r.Secret, r.Actor, r.Key, r.Digest)
	return e
}
