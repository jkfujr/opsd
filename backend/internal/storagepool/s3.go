package storagepool

import (
	"github.com/jkfujr/opsd/internal/objectstore"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/minio/minio-go/v7"
)

func S3(endpoint, ca string, keys protocol.PoolCredentials) (*minio.Client, error) {
	return objectstore.NewDirect(protocol.S3Connection{
		Endpoint: endpoint, CAPEM: ca, Bucket: "opsd-pool-check", AccessStyle: "path", Region: "us-east-1",
	}, protocol.S3Credential{AccessKey: keys.AccessKey, SecretKey: keys.SecretKey})
}
