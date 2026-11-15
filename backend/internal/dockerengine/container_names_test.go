package dockerengine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/moby/moby/api/types/container"
)

func TestUpdateNamesUseProgramAndKeepTargetIdentity(t *testing.T) {
	for _, image := range []string{"registry.example:5000/team/My_App:1.2", "team/my_app@sha256:" + strings.Repeat("a", 64)} {
		id := strings.Repeat("a", 64)
		encoded, _ := json.Marshal(map[string]any{"Id": id, "Name": "/business", "HostConfig": map[string]any{}, "State": map[string]bool{"Running": true}, "Config": map[string]string{"Image": image}})
		var raw container.InspectResponse
		if err := json.Unmarshal(encoded, &raw); err != nil {
			t.Fatal(err)
		}
		state, err := prepareUpdate(protocol.ContainerRef{EngineID: "engine", ContainerID: id}, raw, "stable_ID")
		if err != nil || state.Name != "business" || state.OldID != id || state.TempName != "opsd-update-new-my-app-stable_ID" || state.RetiredName != "opsd-update-old-my-app-stable_ID" {
			t.Fatal("镜像更新命名或目标身份错误", state, err)
		}
	}
}
