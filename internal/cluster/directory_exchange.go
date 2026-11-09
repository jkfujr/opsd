package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type DirectoryExchange struct {
	Summary map[string]string `json:"summary"`
	Delta   Directory         `json:"delta"`
}

func directoryItems(d Directory, visit func(string, any)) {
	for _, v := range d.Admissions {
		visit("admission:"+v.ID, v)
	}
	for _, v := range d.Members {
		visit("member:"+v.ID, v)
	}
	for _, v := range d.Revocations {
		visit("revocation:"+v.ID, v)
	}
	for _, v := range d.Addresses {
		visit("address:"+v.NodeID, v)
	}
	for _, v := range d.Databases {
		visit("database:"+v.ID, v)
	}
	if d.Trust != nil {
		visit("trust", d.Trust)
	}
}

func directoryHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func directorySummary(d Directory) map[string]string {
	out := map[string]string{}
	directoryItems(d, func(k string, v any) { out[k] = directoryHash(v) })
	return out
}

func directoryDelta(d Directory, summary map[string]string) Directory {
	out := Directory{}
	directoryItems(d, func(k string, v any) {
		if summary[k] == directoryHash(v) {
			return
		}
		switch value := v.(type) {
		case Member:
			if len(k) > 10 && k[:10] == "admission:" {
				out.Admissions = append(out.Admissions, value)
			} else {
				out.Members = append(out.Members, value)
			}
		case Revocation:
			out.Revocations = append(out.Revocations, value)
		case AddressRecord:
			out.Addresses = append(out.Addresses, value)
		case ManagedDatabaseCluster:
			out.Databases = append(out.Databases, value)
		case *TrustPolicy:
			out.Trust = value
		}
	})
	return out
}

func emptyDirectory(d Directory) bool {
	return len(d.Admissions)+len(d.Members)+len(d.Revocations)+len(d.Addresses)+len(d.Databases) == 0 && d.Trust == nil
}
