package operations

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"

	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/protocol"
)

func LockResources(directory string, action protocol.OperationAction) (func(), error) {
	unlocks := []func(){}
	release := func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
	for _, claim := range protocol.ActionResources(action) {
		name := fmt.Sprintf("resource-%x.lock", sha256.Sum256([]byte(claim.Key)))
		if claim.Key == "firewall" {
			name = "firewall-config.lock"
		}
		if action.Shell != nil {
			name = "task-" + action.Shell.TaskID + ".lock"
		}
		unlock, err := localstate.LockMode(filepath.Join(directory, name), claim.Shared)
		if err != nil {
			release()
			return nil, err
		}
		unlocks = append(unlocks, unlock)
	}
	return release, nil
}
