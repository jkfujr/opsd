package protocol

type StorageConfig struct {
	NativeCluster bool   `json:"native_cluster"`
	Enabled       bool   `json:"enabled"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
}

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
