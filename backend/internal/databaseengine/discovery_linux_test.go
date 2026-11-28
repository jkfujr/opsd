package databaseengine

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBackupStatusStaysUnderConfiguredRoot(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	day := filepath.Join(root, "daily", "2026-11-28")
	for _, dir := range []string{day, filepath.Join(root, "status"), filepath.Join(other, "fixture")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	valid := filepath.Join(day, "backup.sql.gz")
	outside := filepath.Join(other, "fixture", "outside.sql.gz")
	for _, file := range []string{valid, outside, filepath.Join(root, "status", "daily.success")} {
		if err := os.WriteFile(file, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		filepath.Join(root, "hourly"):                   other,
		filepath.Join(root, "status", "hourly.success"): outside,
		filepath.Join(day, "outside.sql.gz"):            outside,
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	rows := BackupStatus(t.Context(), root)
	if len(rows) != 3 || !slices.Equal(rows[1].Artifacts, []string{valid}) || rows[1].LastSuccess == nil {
		t.Fatal("既有绝对目录下的备份和时间未读回", rows)
	}
	if len(rows[0].Artifacts) != 0 || rows[0].LastSuccess != nil {
		t.Fatal("备份读取超出了配置根")
	}
}
