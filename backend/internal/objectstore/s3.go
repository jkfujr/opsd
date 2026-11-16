// Package objectstore 负责 S3 连接、前缀归属与受限连接检查。
package objectstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Target struct {
	protocol.S3Connection
	ID                   string `json:"id"`
	Revision             uint64 `json:"revision"`
	NodeID               string `json:"node_id"`
	Name                 string `json:"name"`
	Enabled              bool   `json:"enabled"`
	CredentialsAvailable bool   `json:"credentials_available"`
	CredentialRef        string `json:"credential_ref,omitempty"`
	Check                *Check `json:"check,omitempty"`
}
type Input struct {
	Target
	Credentials *protocol.S3Credentials `json:"credentials,omitempty"`
}
type Check struct {
	TargetRevision   uint64    `json:"target_revision"`
	Prefix           string    `json:"prefix"`
	ObjectKey        string    `json:"object_key,omitempty"`
	State            string    `json:"state"`
	Error            string    `json:"error,omitempty"`
	CheckedAt        time.Time `json:"checked_at"`
	PrefixRestricted bool      `json:"prefix_restricted"`
	LifecycleChecked bool      `json:"lifecycle_checked"`
}

func (t Target) Validate() error {
	if t.ID == "" || len(t.ID) > 128 || t.NodeID == "" || t.Name == "" || len(t.Name) > 128 {
		return fmt.Errorf("存储目标标识、所属节点或名称无效")
	}
	return t.S3Connection.Validate()
}
func New(c protocol.S3Connection, key protocol.S3Credential) (*minio.Client, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	u, _ := url.Parse(c.Endpoint)
	lookup := minio.BucketLookupPath
	if c.AccessStyle == "virtual_host" {
		lookup = minio.BucketLookupDNS
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 10 * time.Second
	return minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(key.AccessKey, key.SecretKey, key.SessionToken), Secure: u.Scheme == "https", Region: c.Region, BucketLookup: lookup, Transport: transport, MaxRetries: 1})
}
func Read(ctx context.Context, c *minio.Client, bucket, key string) ([]byte, error) {
	o, err := c.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer o.Close()
	data, err := io.ReadAll(io.LimitReader(o, 1<<20))
	return data, err
}
func missing(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NoSuchObject" || code == "NotFound"
}
func denied(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == "AccessDenied" || code == "AllAccessDisabled"
}

var blockID = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

// VerifyOwner 保留原有对象，首次绑定只接受空前缀或具有相同集群标签的数据块。
func VerifyOwner(ctx context.Context, c *minio.Client, bucket, prefix, cluster string) error {
	key := prefix + ".opsd-owner.json"
	data, err := Read(ctx, c, bucket, key)
	if err == nil {
		var owner struct {
			Cluster string `json:"cluster_id"`
		}
		if json.Unmarshal(data, &owner) != nil || owner.Cluster != cluster {
			return fmt.Errorf("归档目录已经属于其他集群")
		}
		return nil
	}
	if !missing(err) {
		return err
	}
	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	count := 0
	blocks := map[string]bool{}
	for object := range c.ListObjects(listCtx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			return object.Err
		}
		count++
		if count > 100000 {
			return fmt.Errorf("归档目录对象过多，请指定空目录")
		}
		rel := strings.TrimPrefix(object.Key, prefix)
		if strings.HasPrefix(rel, ".opsd-check-") {
			continue
		}
		parts := strings.Split(rel, "/")
		if len(parts) < 2 || !blockID.MatchString(parts[0]) {
			return fmt.Errorf("归档目录含有普通文件，请选择独立目录")
		}
		blocks[parts[0]] = true
	}
	for id := range blocks {
		data, err = Read(ctx, c, bucket, prefix+id+"/meta.json")
		if err != nil {
			return err
		}
		var meta struct {
			Thanos struct {
				Labels map[string]string `json:"labels"`
			} `json:"thanos"`
		}
		if json.Unmarshal(data, &meta) != nil || meta.Thanos.Labels["cluster_id"] != cluster {
			return fmt.Errorf("归档目录含有其他集群或未知来源的数据块")
		}
	}
	data, _ = json.Marshal(map[string]string{"cluster_id": cluster})
	options := minio.PutObjectOptions{ContentType: "application/json", DisableMultipart: true}
	options.SetMatchETagExcept("*")
	_, err = c.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), options)
	if err != nil {
		actual, readErr := Read(ctx, c, bucket, key)
		if readErr == nil && bytes.Equal(actual, data) {
			return nil
		}
		return fmt.Errorf("保存归档目录归属失败：%w", err)
	}
	return nil
}

// CheckConnection 的键在调用前持久登记；失败后的检查继续使用原键。
func CheckConnection(ctx context.Context, target Target, keys protocol.S3Credentials, prefix, key string) (Check, error) {
	out := Check{Prefix: prefix, ObjectKey: key, State: "checking", CheckedAt: time.Now().UTC()}
	if !strings.HasPrefix(key, prefix+".opsd-check-") || !strings.HasSuffix(prefix, "/") || prefix == "/" {
		return out, fmt.Errorf("连接检查对象必须位于指定前缀")
	}
	finish := func(err error) (Check, error) {
		if err != nil {
			out.State = "failed"
			out.Error = err.Error()
		} else {
			out.State = "ready"
			out.ObjectKey = ""
		}
		return out, err
	}
	if err := keys.Validate(); err != nil {
		return finish(err)
	}
	compact, err := New(target.S3Connection, keys.Compact)
	if err != nil {
		return finish(err)
	}
	receive, err := New(target.S3Connection, keys.Receive)
	if err != nil {
		return finish(err)
	}
	store, err := New(target.S3Connection, keys.Store)
	if err != nil {
		return finish(err)
	}
	body := []byte("OPSD S3 connection check: " + key)
	options := minio.PutObjectOptions{ContentType: "text/plain", DisableMultipart: true}
	// PUT 应答未知时读取同一键，确认原内容后继续。
	actual, readErr := Read(ctx, compact, target.Bucket, key)
	if readErr != nil && !missing(readErr) {
		return finish(readErr)
	}
	if readErr == nil && !bytes.Equal(actual, body) {
		return finish(fmt.Errorf("连接检查对象内容已被修改"))
	}
	if missing(readErr) {
		_, err = receive.PutObject(ctx, target.Bucket, key, bytes.NewReader(body), int64(len(body)), options)
		if err != nil {
			actual, readErr = Read(ctx, compact, target.Bucket, key)
			if readErr != nil || !bytes.Equal(actual, body) {
				return finish(err)
			}
		}
	}
	for _, client := range []*minio.Client{receive, store, compact} {
		actual, e := Read(ctx, client, target.Bucket, key)
		if e != nil {
			return finish(e)
		}
		if !bytes.Equal(actual, body) {
			return finish(fmt.Errorf("S3 读取内容与检查对象不一致"))
		}
	}
	// 所有验证写入都限制在已登记的临时对象键内。
	_, err = store.PutObject(ctx, target.Bucket, key, bytes.NewReader(body), int64(len(body)), options)
	if !denied(err) {
		return finish(fmt.Errorf("Store Gateway 凭据需要只读权限"))
	}
	if err = store.RemoveObject(ctx, target.Bucket, key, minio.RemoveObjectOptions{}); !denied(err) {
		return finish(fmt.Errorf("Store Gateway 凭据需要禁用删除权限"))
	}
	err = receive.RemoveObject(ctx, target.Bucket, key, minio.RemoveObjectOptions{})
	if !denied(err) {
		return finish(fmt.Errorf("Receive 凭据需要禁用删除权限"))
	}
	if _, err = compact.PutObject(ctx, target.Bucket, key, bytes.NewReader(body), int64(len(body)), options); err != nil {
		return finish(err)
	}
	for _, client := range []*minio.Client{receive, store, compact} {
		listCtx, cancel := context.WithCancel(ctx)
		for item := range client.ListObjects(listCtx, target.Bucket, minio.ListObjectsOptions{Prefix: prefix, MaxKeys: 1}) {
			if item.Err != nil {
				cancel()
				return finish(item.Err)
			}
			break
		}
		cancel()
		if !target.DedicatedBucket {
			listCtx, cancel = context.WithCancel(ctx)
			restricted := false
			for item := range client.ListObjects(listCtx, target.Bucket, minio.ListObjectsOptions{MaxKeys: 1}) {
				restricted = denied(item.Err)
				break
			}
			cancel()
			if !restricted {
				return finish(fmt.Errorf("共享桶的列举权限须限定到监控前缀；请调整权限或使用专用监控桶"))
			}
		}
	}
	out.PrefixRestricted = !target.DedicatedBucket
	lifecycle, err := compact.GetBucketLifecycle(ctx, target.Bucket)
	if err != nil && minio.ToErrorResponse(err).Code != "NoSuchLifecycleConfiguration" {
		return finish(fmt.Errorf("读取桶生命周期失败：%w", err))
	}
	if lifecycle != nil {
		for _, rule := range lifecycle.Rules {
			if rule.Status != "Enabled" {
				continue
			}
			rulePrefix := rule.Prefix
			if rule.RuleFilter.Prefix != "" {
				rulePrefix = rule.RuleFilter.Prefix
			}
			if rule.RuleFilter.And.Prefix != "" {
				rulePrefix = rule.RuleFilter.And.Prefix
			}
			if !strings.HasPrefix(prefix, rulePrefix) && !strings.HasPrefix(rulePrefix, prefix) {
				continue
			}
			if !rule.Expiration.IsDaysNull() || !rule.Expiration.IsDateNull() || !rule.Transition.IsNull() {
				return finish(fmt.Errorf("监控前缀存在到期或存储层转换规则，请由 Compactor 管理保留策略"))
			}
		}
	}
	out.LifecycleChecked = true
	if err = compact.RemoveObject(ctx, target.Bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return finish(err)
	}
	_, err = compact.StatObject(ctx, target.Bucket, key, minio.StatObjectOptions{})
	if !missing(err) {
		return finish(fmt.Errorf("检查对象的删除结果尚未确认"))
	}
	return finish(nil)
}
