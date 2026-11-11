package databaseengine

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

type connectionPool struct {
	db    *sql.DB
	used  time.Time
	users int
}

var pools = struct {
	sync.Mutex
	entries map[string]*connectionPool
}{entries: map[string]*connectionPool{}}

func pooledDatabase(key string, config *mysql.Config) (*sql.DB, func(), error) {
	pools.Lock()
	defer pools.Unlock()
	now := time.Now()
	for k, v := range pools.entries {
		if now.Sub(v.used) > 2*time.Minute && v.users == 0 {
			v.db.Close()
			delete(pools.entries, k)
		}
	}
	if entry := pools.entries[key]; entry != nil {
		entry.used = now
		entry.users++
		return entry.db, poolRelease(entry), nil
	}
	if len(pools.entries) >= 32 {
		oldestKey := ""
		var oldest *connectionPool
		for k, v := range pools.entries {
			if v.users == 0 && (oldest == nil || v.used.Before(oldest.used)) {
				oldestKey, oldest = k, v
			}
		}
		if oldest == nil {
			return nil, nil, fmt.Errorf("数据库连接繁忙")
		}
		oldest.db.Close()
		delete(pools.entries, oldestKey)
	}
	connector, err := mysql.NewConnector(config)
	if err != nil {
		return nil, nil, fmt.Errorf("数据库连接配置无效")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	db.SetConnMaxIdleTime(time.Minute)
	db.SetConnMaxLifetime(5 * time.Minute)
	entry := &connectionPool{db: db, used: now, users: 1}
	pools.entries[key] = entry
	return db, poolRelease(entry), nil
}

func poolRelease(entry *connectionPool) func() {
	var once sync.Once
	return func() { once.Do(func() { pools.Lock(); entry.users--; entry.used = time.Now(); pools.Unlock() }) }
}
