package cluster

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/jkfujr/opsd/internal/identity"
)

type StorageStatus struct {
	Enabled      bool                `json:"enabled"`
	Selected     string              `json:"selected"`
	Pending      int                 `json:"pending"`
	State        string              `json:"state"`
	Replications []ReplicationStatus `json:"replications"`
}
type ReplicationStatus struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	ErrorCount int    `json:"error_count"`
}

func (n *Node) adminDatabase(ctx context.Context) (*database, error) {
	c, e := n.Config(ctx)
	if e != nil {
		return nil, e
	}
	if !c.Storage.Enabled {
		return nil, fault("dependency_unavailable", "未启用数据库")
	}
	auth, e := n.savedCredentials(ctx, "admin")
	if e != nil {
		return nil, e
	}
	return &database{ID: n.ID, URL: storageURL(c.Storage), Region: c.Region, Auth: auth, Client: dbClient()}, nil
}
func (n *Node) databaseCredentials(ctx context.Context, id string) (Credentials, error) {
	if !n.Active(ctx, id) {
		return Credentials{}, fault("forbidden", "节点已撤销")
	}
	d, e := n.adminDatabase(ctx)
	if e != nil {
		return Credentials{}, e
	}
	c, e := n.savedCredentials(ctx, "user:"+id)
	if e != nil {
		c = Credentials{Username: "opsd_" + id, Password: identity.Random()}
		if e = n.saveCredentials(ctx, "user:"+id, c); e != nil {
			return c, e
		}
	}
	path := "/_users/" + url.PathEscape("org.couchdb.user:"+c.Username)
	var doc map[string]any
	status, e := d.request(ctx, "GET", path, nil, &doc)
	if e != nil && status != 404 {
		return c, e
	}
	if status == 404 {
		_, e = d.request(ctx, "PUT", path, map[string]any{"_id": "org.couchdb.user:" + c.Username, "name": c.Username, "type": "user", "password": c.Password, "roles": []string{"opsd_member"}}, nil)
	}
	return c, e
}
func (n *Node) ensureDatabase(ctx context.Context, d *database, name string) error {
	status, e := d.request(ctx, "PUT", "/"+name, nil, nil)
	if e != nil && status != 412 {
		return e
	}
	if _, e = d.request(ctx, "PUT", "/"+name+"/_security", map[string]any{"admins": map[string]any{"names": []string{}, "roles": []string{}}, "members": map[string]any{"names": []string{}, "roles": []string{"opsd_member"}}}, nil); e != nil {
		return e
	}
	if name == "opsd_operations" {
		path := "/" + name + "/_design/opsd"
		var old map[string]any
		status, e = d.request(ctx, "GET", path, nil, &old)
		if e != nil && status != 404 {
			return e
		}
		if status == 404 {
			_, e = d.request(ctx, "PUT", path, map[string]any{"_id": "_design/opsd", "language": "javascript", "views": map[string]any{"latest": map[string]string{"map": "function(doc){if(doc.kind==='operation' && doc.owner && doc.queue_sequence){emit([doc.owner,doc.queue_sequence],doc);}}", "reduce": "function(keys,values,rereduce){return values.reduce(function(a,b){return !a||b.sequence>a.sequence?b:a;},null);}"}}}, nil)
			if e != nil {
				return e
			}
		}
	}
	return nil
}
func (n *Node) manageDatabase(ctx context.Context) error {
	d, e := n.adminDatabase(ctx)
	if e != nil {
		return e
	}
	for _, name := range []string{"_users", "_replicator", "_global_changes"} {
		status, err := d.request(ctx, "PUT", "/"+name, nil, nil)
		if err != nil && status != 412 {
			return err
		}
	}
	names := []string{"opsd_state", "opsd_operations"}
	for i := 0; i < 7; i++ {
		names = append(names, "opsd_metrics_"+n.Now().UTC().AddDate(0, 0, -i).Format("20060102"))
	}
	for _, name := range names {
		if e = n.ensureDatabase(ctx, d, name); e != nil {
			return e
		}
	}
	dir, e := n.Directory(ctx)
	if e != nil {
		return e
	}
	for _, r := range dir.Revocations {
		username := "opsd_" + r.ID
		path := "/_users/" + url.PathEscape("org.couchdb.user:"+username)
		var doc struct {
			Rev string `json:"_rev"`
		}
		status, err := d.request(ctx, "GET", path, nil, &doc)
		if err == nil {
			if _, err = d.request(ctx, "DELETE", path+"?rev="+url.QueryEscape(doc.Rev), nil, nil); err != nil {
				return err
			}
		} else if status != 404 {
			return err
		}
	}
	var all []string
	if _, e = d.request(ctx, "GET", "/_all_dbs", nil, &all); e != nil {
		return e
	}
	var jobs struct {
		Rows []struct {
			ID  string         `json:"id"`
			Doc map[string]any `json:"doc"`
		} `json:"rows"`
	}
	if _, e = d.request(ctx, "GET", "/_replicator/_all_docs?include_docs=true", nil, &jobs); e != nil {
		return e
	}
	active := map[string]bool{}
	for _, m := range dir.Members {
		if m.Storage != "" && !n.Revoked(ctx, m.ID) {
			active[m.ID] = true
		}
	}
	for _, row := range jobs.Rows {
		if !strings.HasPrefix(row.ID, "opsd_") {
			continue
		}
		target, _ := row.Doc["opsd_target"].(string)
		name, _ := row.Doc["opsd_database"].(string)
		if !active[target] || (strings.HasPrefix(name, "opsd_metrics_") && !activeMetricDatabase(name, n.Now())) {
			rev, _ := row.Doc["_rev"].(string)
			if _, e = d.request(ctx, "DELETE", "/_replicator/"+url.PathEscape(row.ID)+"?rev="+url.QueryEscape(rev), nil, nil); e != nil {
				return e
			}
		}
	}
	for _, name := range all {
		if strings.HasPrefix(name, "opsd_metrics_") && !activeMetricDatabase(name, n.Now()) {
			if _, e = d.request(ctx, "DELETE", "/"+name, nil, nil); e != nil {
				return e
			}
		}
	}
	localUser, e := n.databaseCredentials(ctx, n.ID)
	if e != nil {
		return e
	}
	existing := map[string]bool{}
	for _, row := range jobs.Rows {
		existing[row.ID] = true
	}
	for _, m := range dir.Members {
		if m.ID == n.ID || !active[m.ID] {
			continue
		}
		auth, err := n.savedCredentials(ctx, "access:"+m.ID)
		if err != nil {
			readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err = n.Call(readCtx, m.ID, "storage.credentials", nil, &auth)
			cancel()
			if err != nil {
				continue
			}
			if err = n.saveCredentials(ctx, "access:"+m.ID, auth); err != nil {
				return err
			}
		}
		for _, name := range names {
			id := "opsd_" + m.ID + "_" + name
			if existing[id] {
				continue
			}
			job := map[string]any{"_id": id, "source": map[string]any{"url": d.URL + "/" + name, "headers": map[string]string{"Authorization": basicAuth(localUser)}}, "target": map[string]any{"url": m.Storage + "/" + name, "headers": map[string]string{"Authorization": basicAuth(auth)}}, "continuous": true, "create_target": false, "opsd_target": m.ID, "opsd_database": name}
			if _, e = d.request(ctx, "PUT", "/_replicator/"+url.PathEscape(id), job, nil); e != nil {
				return e
			}
		}
	}
	return nil
}
func (n *Node) StorageStatus(ctx context.Context) (StorageStatus, error) {
	c, e := n.Config(ctx)
	result := StorageStatus{Enabled: c.Storage.Enabled, State: "ready", Replications: []ReplicationStatus{}}
	if e != nil {
		return result, e
	}
	if e = n.DB.QueryRowContext(ctx, "SELECT count(*) FROM records WHERE uploaded=0").Scan(&result.Pending); e != nil {
		return result, e
	}
	n.storageMu.Lock()
	if n.storage != nil {
		result.Selected = n.storage.ID
	} else {
		result.State = "unavailable"
	}
	if n.storageError != "" {
		result.State = "pending"
	}
	n.storageMu.Unlock()
	if c.Storage.Enabled {
		d, err := n.adminDatabase(ctx)
		if err != nil {
			result.State = "unavailable"
			return result, nil
		}
		var response struct {
			Docs []struct {
				ID     string `json:"doc_id"`
				State  string `json:"state"`
				Errors int    `json:"error_count"`
			} `json:"docs"`
		}
		if _, err = d.request(ctx, "GET", "/_scheduler/docs", nil, &response); err != nil {
			result.State = "unavailable"
			return result, nil
		}
		for _, doc := range response.Docs {
			result.Replications = append(result.Replications, ReplicationStatus{ID: doc.ID, State: doc.State, ErrorCount: doc.Errors})
		}
	}
	return result, nil
}
func (n *Node) ImportAccountRecord(ctx context.Context, r Record) (Account, error) {
	if e := n.Verify(ctx, r); e != nil {
		return Account{}, e
	}
	var a Account
	e := json.Unmarshal(r.Data, &a)
	return a, e
}
