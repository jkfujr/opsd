package protocol

import (
	"fmt"
	"net/url"
	"strings"
)

type S3Connection struct {
	Endpoint        string `json:"endpoint"`
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	AccessStyle     string `json:"access_style"`
	DedicatedBucket bool   `json:"dedicated_bucket"`
}
type S3Credential struct {
	AccessKey    string `json:"access_key"`
	SecretKey    string `json:"secret_key"`
	SessionToken string `json:"session_token,omitempty"`
}
type S3Credentials struct {
	Receive S3Credential `json:"receive"`
	Store   S3Credential `json:"store"`
	Compact S3Credential `json:"compact"`
}

func (c S3Connection) Validate() error {
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || u.Scheme != "https" && u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return fmt.Errorf("S3 Endpoint 必须是 HTTP 或 HTTPS 服务地址")
	}
	if len(c.Bucket) < 3 || len(c.Bucket) > 63 || strings.ContainsAny(c.Bucket, "/\\\x00\r\n ") || len(c.Region) > 128 || strings.ContainsAny(c.Region, "\x00\r\n") || c.AccessStyle != "path" && c.AccessStyle != "virtual_host" {
		return fmt.Errorf("S3 Bucket、Region 或访问方式无效")
	}
	return nil
}
func (c S3Credentials) Validate() error {
	for _, v := range []S3Credential{c.Receive, c.Store, c.Compact} {
		if v.AccessKey == "" || v.SecretKey == "" || len(v.AccessKey) > 256 || len(v.SecretKey) > 512 || len(v.SessionToken) > 16384 {
			return fmt.Errorf("请提供 Receive、Store Gateway 和 Compactor 的 S3 凭据")
		}
	}
	return nil
}
