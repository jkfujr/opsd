package cluster

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"io"

	"filippo.io/age"
)

func seal(plain []byte, recipients []string) (Secret, error) {
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return Secret{}, e
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return Secret{}, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return Secret{}, e
	}
	r := Secret{Nonce: make([]byte, g.NonceSize())}
	if _, e = rand.Read(r.Nonce); e != nil {
		return r, e
	}
	r.Ciphertext = g.Seal(nil, r.Nonce, plain, nil)
	keys := make([]age.Recipient, 0, len(recipients))
	for _, s := range recipients {
		k, e := age.ParseX25519Recipient(s)
		if e != nil {
			return r, e
		}
		keys = append(keys, k)
	}
	var dst bytes.Buffer
	w, e := age.Encrypt(&dst, keys...)
	if e != nil {
		return r, e
	}
	if _, e = w.Write(key); e != nil {
		return r, e
	}
	if e = w.Close(); e != nil {
		return r, e
	}
	r.Keys = dst.Bytes()
	return r, nil
}
func unseal(r Secret, id *age.X25519Identity) ([]byte, error) {
	reader, e := age.Decrypt(bytes.NewReader(r.Keys), id)
	if e != nil {
		return nil, e
	}
	key, e := io.ReadAll(io.LimitReader(reader, 33))
	if e != nil {
		return nil, e
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	if len(r.Nonce) != g.NonceSize() {
		return nil, fault("conflict", "密文无效")
	}
	return g.Open(nil, r.Nonce, r.Ciphertext, nil)
}
