package databaseengine

import "testing"

func TestDatabasePrivilegePatternEscapesExactSchema(t *testing.T) {
	if pattern := databasePrivilegePattern("opsd_core"); pattern != `opsd\_core` || databasePrivilegeName("opsd_core") != "`"+pattern+"`" {
		t.Fatal("库级授权未保持精确库名", pattern)
	}
}
