package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func ResourceVersion(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func ContainerRevision(value ContainerSummary) string {
	value.Health = ""
	value.ReadOnlyReason = ""
	value.Ports = append([]ContainerPort{}, value.Ports...)
	sort.Slice(value.Ports, func(i, j int) bool { return ResourceVersion(value.Ports[i]) < ResourceVersion(value.Ports[j]) })
	return ResourceVersion(value)
}
