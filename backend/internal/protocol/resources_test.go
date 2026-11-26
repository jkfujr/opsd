package protocol

import "testing"

func TestResourceClaimsFollowProjectAndInstance(t *testing.T) {
	container := func(id, project string) OperationAction {
		return OperationAction{Kind: "container", Container: &ContainerAction{ContainerRef: ContainerRef{EngineID: "engine", ContainerID: id}, Project: project, ProjectKnown: true}}
	}
	compose := OperationAction{Kind: "compose", Compose: &ComposeAction{Project: ComposeRef{EngineID: "engine", Name: "web"}}}
	database := OperationAction{Kind: "database", Database: &DatabaseAction{Connection: DatabaseConnection{Kind: "galera", Address: "127.0.0.1:3306", ContainerID: "db"}, ClusterID: "cluster"}}
	for _, item := range []struct {
		name     string
		a, b     OperationAction
		conflict bool
	}{
		{"同一容器", container("one", "web"), container("one", "web"), true},
		{"不同容器", container("one", ""), container("two", ""), false},
		{"同项目独立容器", container("one", "web"), container("two", "web"), false},
		{"项目覆盖容器", compose, container("one", "web"), true},
		{"不同项目", compose, container("one", "other"), false},
		{"数据库容器", database, container("db", ""), true},
		{"数据库与其他容器", database, container("other", ""), false},
		{"Shell 与防火墙", OperationAction{Kind: "shell", Shell: &ShellAction{TaskID: "job"}}, OperationAction{Kind: "firewall"}, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := ResourcesConflict(ActionResources(item.a), ActionResources(item.b)); got != item.conflict {
				t.Fatalf("资源冲突=%v，预期=%v", got, item.conflict)
			}
		})
	}
}

func TestContainerRevisionMatchesDisplayedImage(t *testing.T) {
	detail := ContainerDetail{ContainerSummary: ContainerSummary{
		ContainerRef: ContainerRef{EngineID: "engine", ContainerID: "container"}, Image: "app:latest", State: "running",
	}, ImageID: "sha256:actual"}
	list := detail.ContainerSummary
	if !detail.MatchesRevision(ContainerRevision(list)) {
		t.Fatal("容器标签版本失效")
	}
	list.Image = detail.ImageID
	if !detail.MatchesRevision(ContainerRevision(list)) {
		t.Fatal("镜像 ID 列表版本失效")
	}
	list.State = "exited"
	if detail.MatchesRevision(ContainerRevision(list)) {
		t.Fatal("镜像 ID 版本绕过了容器状态检查")
	}
}
