package protocol

// ActionRequiresArbitraryCode 报告一个操作动作是否可能执行任意代码，
// 或换上调用方指定的镜像（等价于任意代码）。
//
// 判据按实际执行效果，而不是动作名字：
//   - Shell 动作直接执行脚本；
//   - Compose 的 start 会用 up --no-recreate 启动缺失容器，update／recreate 会换镜像；
//   - 单容器 update 会拉取镜像并创建替换容器；
//   - 数据库 deploy_database 会用请求里的 Image 拉取并启动容器。
//
// 其余动作（只读查询、受管变更、防火墙与互信写入）不需要这一档能力。
func ActionRequiresArbitraryCode(action OperationAction) bool {
	if action.Shell != nil {
		return true
	}
	if action.Compose != nil {
		switch action.Compose.Action {
		case "start", "update", "recreate":
			return true
		}
	}
	if action.Container != nil && action.Container.Action == "update" {
		return true
	}
	if action.Database != nil && action.Kind == "deploy_database" && action.Deployment != nil {
		return true
	}
	return false
}
