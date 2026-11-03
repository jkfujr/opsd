package cluster

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCouchDBRegionalReplicationAndRecovery(t *testing.T) {
	addresses := strings.Split(os.Getenv("OPSD_TEST_COUCHDBS"), ",")
	if len(addresses) != 3 {
		t.Skip("未提供三个本地 CouchDB 实例")
	}
	credentials := Credentials{Username: os.Getenv("OPSD_TEST_COUCHDB_USER"), Password: os.Getenv("OPSD_TEST_COUCHDB_PASSWORD")}
	if credentials.Username == "" || credentials.Password == "" {
		t.Fatal("缺少本地数据库凭据")
	}
	nodes := make([]*Node, 3)
	admins := make([]*database, 3)
	for i, address := range addresses {
		nodes[i], _ = openTestNode(t)
		u, e := url.Parse(address)
		if e != nil {
			t.Fatal(e)
		}
		host, port, e := net.SplitHostPort(u.Host)
		if e != nil {
			t.Fatal(e)
		}
		number, e := strconv.Atoi(port)
		if e != nil {
			t.Fatal(e)
		}
		n := nodes[i]
		c, _ := n.Config(t.Context())
		c.Region = strconv.Itoa(i)
		c.Storage = StorageConfig{Enabled: true, Address: host, Port: number}
		c.PreferredStorage = n.ID
		b, _ := json.Marshal(c)
		tx, e := n.DB.BeginTx(t.Context(), nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec("UPDATE node_identity SET config=? WHERE id=1", string(b)); e != nil {
			t.Fatal(e)
		}
		if e = n.publishMemberTx(t.Context(), tx, c); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
		if e = n.saveCredentials(t.Context(), "admin", credentials); e != nil {
			t.Fatal(e)
		}
		admins[i] = &database{ID: n.ID, URL: address, Auth: credentials, Client: dbClient()}
		waitFor(t, 20*time.Second, func() bool { _, e := admins[i].request(t.Context(), "GET", "/_up", nil, nil); return e == nil })
	}
	joinTest(t, nodes[1], nodes[0])
	joinTest(t, nodes[2], nodes[0])
	for _, n := range nodes {
		n.exchange()
	}
	for range 2 {
		for _, n := range nodes {
			if e := n.manageDatabase(t.Context()); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := nodes[0].Rewrap(t.Context()); e != nil {
		t.Fatal(e)
	}
	d, e := nodes[0].selectDatabase(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if e = nodes[0].upload(t.Context(), d); e != nil {
		t.Fatal(e)
	}
	waitFor(t, 30*time.Second, func() bool {
		r, e := nodes[2].latest(t.Context(), nodes[0].ID, "account", nodes[0].ID)
		if e != nil {
			return false
		}
		var a Account
		if json.Unmarshal(r.Data, &a) != nil {
			return false
		}
		plain, e := unseal(a.Secret, nodes[2].age)
		return e == nil && string(plain) == "test-password"
	})
	names := strings.Split(os.Getenv("OPSD_TEST_COUCHDB_CONTAINERS"), ",")
	if len(names) == 3 {
		command := append([]string{"stop"}, names...)
		if output, e := exec.CommandContext(t.Context(), "docker", command...).CombinedOutput(); e != nil {
			t.Fatalf("停止本地测试数据库：%v %s", e, output)
		}
		op, e := nodes[0].Submit(t.Context(), nodes[0].ID, Request{ID: NewOperationID(nodes[0].ID), Kind: "name", Payload: payload("数据库断开后仍可控制")})
		if e != nil {
			t.Fatal(e)
		}
		nodes[0].advance()
		op, e = nodes[0].GetOperation(t.Context(), op.ID)
		if e != nil || !terminalState(op.State) {
			t.Fatal(op, e)
		}
		if output, e := exec.CommandContext(t.Context(), "docker", append([]string{"start"}, names...)...).CombinedOutput(); e != nil {
			t.Fatalf("恢复本地测试数据库：%v %s", e, output)
		}
		for _, admin := range admins {
			waitFor(t, 20*time.Second, func() bool { _, e := admin.request(t.Context(), "GET", "/_up", nil, nil); return e == nil })
		}
		if e = nodes[0].upload(t.Context(), d); e != nil {
			t.Fatal(e)
		}
		waitFor(t, 30*time.Second, func() bool {
			r, e := nodes[2].latest(t.Context(), nodes[0].ID, "member", nodes[0].ID)
			if e != nil {
				return false
			}
			var m Member
			return json.Unmarshal(r.Data, &m) == nil && m.Name == "数据库断开后仍可控制"
		})
	}
	expired := "opsd_metrics_" + time.Now().UTC().AddDate(0, 0, -8).Format("20060102")
	if _, e = admins[2].request(t.Context(), "PUT", "/"+expired, nil, nil); e != nil {
		t.Fatal(e)
	}
	if e = nodes[2].manageDatabase(t.Context()); e != nil {
		t.Fatal(e)
	}
	status, e := admins[2].request(t.Context(), "GET", "/"+expired, nil, nil)
	if e == nil || status != http.StatusNotFound {
		t.Fatal("过期数据库未清理", status, e)
	}
	oldAuth, e := nodes[0].databaseCredentials(t.Context(), nodes[1].ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = nodes[0].Revoke(t.Context(), nodes[1].ID); e != nil {
		t.Fatal(e)
	}
	if e = nodes[0].manageDatabase(t.Context()); e != nil {
		t.Fatal(e)
	}
	denied := &database{URL: addresses[0], Auth: oldAuth, Client: dbClient()}
	status, e = denied.request(t.Context(), "GET", "/opsd_state", nil, nil)
	if e == nil || status != 401 && status != 403 {
		t.Fatal("撤销成员仍可访问数据库", status, e)
	}
}
func waitFor(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		select {
		case <-t.Context().Done():
			t.Fatal(context.Cause(t.Context()))
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("等待本地状态超时")
}
