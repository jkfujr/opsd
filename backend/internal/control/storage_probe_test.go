package control

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"filippo.io/age"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/objectstore"
	"github.com/jkfujr/opsd/internal/protocol"
)

func TestStorageProbeHTTP(t *testing.T) {
	const secret = "fixture-secret-for-s3"
	var lists atomic.Int32
	s3 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method != http.MethodGet || strings.TrimSuffix(r.URL.Path, "/") != "/probe-bucket" ||
			q.Get("list-type") != "2" || q.Get("max-keys") != "1" || !strings.Contains(r.Header.Get("Authorization"), "Credential=fixture-store/") {
			t.Error("探测应使用 Store 凭据执行限定为一条的 S3 列举")
			w.WriteHeader(400)
			return
		}
		lists.Add(1)
		w.Header().Set("Content-Type", "application/xml")
		if q.Get("prefix") == "denied" {
			w.WriteHeader(403)
			fmt.Fprintf(w, "<Error><Code>AccessDenied</Code><Message>%s</Message></Error>", secret)
			return
		}
		io.WriteString(w, "<ListBucketResult><Name>probe-bucket</Name><KeyCount>0</KeyCount><MaxKeys>1</MaxKeys><IsTruncated>false</IsTruncated></ListBucketResult>")
	}))
	defer s3.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s3.Certificate().Raw}))
	portal := openAPINode(t, "存储入口", "日本", 2, 5)
	owner := openAPINode(t, "目标所属节点", "美国", 2, 8)
	joinAPINodes(t, portal, owner)
	seedProbeCredential(t, owner.node, secret)
	target := objectstore.Target{
		ID: "s3-probe", NodeID: owner.node.ID, Name: "S3 探测", Enabled: true, Revision: 1,
		CredentialsAvailable: true, CredentialRef: "fixture-ref",
		S3Connection: protocol.S3Connection{Endpoint: s3.URL, Bucket: "probe-bucket", Region: "us-east-1", AccessStyle: "path", CAPEM: ca},
	}
	save := func() {
		t.Helper()
		raw, _ := json.Marshal(target)
		for _, n := range []*apiNode{portal, owner} {
			if _, err := n.node.DB.Exec("INSERT INTO storage_targets(id,revision,body) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,body=excluded.body", target.ID, target.Revision, string(raw)); err != nil {
				t.Fatal(err)
			}
		}
	}
	save()
	client, csrf := apiAdmin(t, portal)
	path := "/api/v2/storage/targets/" + target.ID
	call := func(method, path, tag, key, body string, want int) (http.Header, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, portal.web.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", portal.web.URL)
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("If-Match", tag)
		req.Header.Set("Idempotency-Key", key)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		validateTestResponse(t, req, res)
		raw, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want || bytes.Contains(raw, []byte(secret)) {
			t.Fatal("存储接口应答无效", res.StatusCode, err)
		}
		return res.Header, raw
	}
	headers, _ := call("GET", path, "", "", "", 200)
	tag := headers.Get("ETag")
	call("POST", path+"/probe", "", "no-match", "{}", 428)
	call("POST", path+"/probe", tag, "bad-prefix", "{\"prefix\":\"\\n\"}", 422)
	_, empty := call("POST", path+"/probe", tag, "probe-empty", "{}", 200)
	var probe httpapi.StorageProbe
	if err := json.Unmarshal(empty, &probe); err != nil || probe.State != "ready" || probe.Prefix != "" {
		t.Fatal("空桶探测失败", probe, err)
	}
	call("POST", path+"/probe", tag, "probe-empty", "{}", 200)
	call("POST", path+"/probe", tag, "probe-empty", "{\"prefix\":\"other\"}", 409)
	call("POST", path+"/check", tag, "probe-empty", "{\"prefix\":\"archive/\"}", 409)
	_, denied := call("POST", path+"/probe", tag, "probe-denied", "{\"prefix\":\"denied\"}", 200)
	if err := json.Unmarshal(denied, &probe); err != nil || probe.State != "failed" || probe.Error == nil || *probe.Error != "S3 拒绝访问" {
		t.Fatal("权限不足的探测应答无效", probe, err)
	}
	_, read := call("GET", path, "", "", "", 200)
	var current httpapi.StorageTarget
	if err := json.Unmarshal(read, &current); err != nil || current.Check != nil || current.ConnectionCheck == nil || current.ConnectionCheck.State != "failed" || current.CaPem == nil || *current.CaPem != ca {
		t.Fatal("目标读取未返回当前连接状态或混入归档检查", err)
	}
	_, listed := call("GET", "/api/v2/storage/targets", "", "", "", 200)
	var targets httpapi.StorageTargetList
	if err := json.Unmarshal(listed, &targets); err != nil || len(targets.Items) != 1 || targets.Items[0].ConnectionCheck == nil || targets.Items[0].ConnectionCheck.State != "failed" {
		t.Fatal("列表未读回最新探测状态", err)
	}
	call("GET", "/api/v2/requests/probe-empty?node_id="+owner.node.ID, "", "", "", 200)
	target.Revision++
	save()
	_, replay := call("POST", path+"/probe", tag, "probe-empty", "{}", 200)
	if !bytes.Equal(replay, empty) || lists.Load() != 2 {
		t.Fatal("重放改变了应答或多次访问 S3", lists.Load())
	}
	call("POST", path+"/probe", tag, "stale-revision", "{}", 412)
	fresh, newer := call("GET", path, "", "", "", 200)
	if bytes.Contains(newer, []byte("connection_check")) {
		t.Fatal("读回了旧版本的探测结果")
	}
	_ = owner.node.Close()
	owner.peer.Close()
	call("POST", path+"/probe", fresh.Get("ETag"), "owner-offline", "{}", 503)
}

func seedProbeCredential(t *testing.T, node *cluster.Node, secret string) {
	t.Helper()
	member, err := node.Self(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	recipient, err := age.ParseX25519Recipient(member.Recipient)
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	value := protocol.S3Credential{AccessKey: "fixture-store", SecretKey: secret}
	keys, _ := json.Marshal(protocol.S3Credentials{Receive: value, Store: value, Compact: value})
	var sealed bytes.Buffer
	w, err := age.Encrypt(&sealed, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(key); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(cluster.Secret{Keys: sealed.Bytes(), Nonce: nonce, Ciphertext: gcm.Seal(nil, nonce, keys, nil)})
	if _, err := node.DB.Exec("INSERT INTO object_storage_credentials(id,target_id,secret) VALUES('fixture-ref','s3-probe',?)", string(raw)); err != nil {
		t.Fatal(err)
	}
}
