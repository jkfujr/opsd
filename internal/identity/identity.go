package identity

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

type Key struct {
	Private []byte
	Public  []byte
}

func New() (Key, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Key{}, err
	}
	return Key{Private: key.Bytes(), Public: key.PublicKey().Bytes()}, nil
}

func Load(private, public []byte) (Key, error) {
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil || len(public) != 32 {
		return Key{}, fmt.Errorf("身份密钥缺失或损坏")
	}
	if base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()) != Encode(public) {
		return Key{}, fmt.Errorf("身份公私钥不匹配")
	}
	return Key{Private: private, Public: public}, nil
}

func Encode(key []byte) string { return base64.StdEncoding.EncodeToString(key) }

func Decode(value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("公钥必须是 32 字节的 Base64 编码")
	}
	return key, nil
}

func Random() string {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(value)
}
