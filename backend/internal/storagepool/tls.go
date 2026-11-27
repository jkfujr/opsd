package storagepool

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/jkfujr/opsd/internal/protocol"
)

const LeafLifetime = 90 * 24 * time.Hour
const RenewBefore = 30 * 24 * time.Hour

func TLSGeneration(m protocol.PoolMaterial) string {
	sum := sha256.Sum256([]byte(m.CA + "\x00" + m.Cert + "\x00" + m.PeerCert))
	return fmt.Sprintf("tls-%x", sum[:12])
}

func CertificateExpiry(cert string) (time.Time, error) {
	block, _ := pem.Decode([]byte(cert))
	if block == nil || block.Type != "CERTIFICATE" {
		return time.Time{}, fmt.Errorf("证书格式无效")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.NotAfter, nil
}

type Authority struct {
	Cert string `json:"cert"`
	Key  string `json:"key"`
}

func NewAuthority(pool string, now time.Time) (Authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Authority{}, err
	}
	template := &x509.Certificate{
		Subject: pkix.Name{CommonName: "OPSD " + pool}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(5, 0, 0),
		IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	cert, encodedKey, err := certificate(template, template, key, key)
	return Authority{cert, encodedKey}, err
}

func (a Authority) Issue(node, ip, filerKey, seal string, now time.Time) (protocol.PoolMaterial, error) {
	authority, err := tls.X509KeyPair([]byte(a.Cert), []byte(a.Key))
	if err != nil {
		return protocol.PoolMaterial{}, err
	}
	ca, err := x509.ParseCertificate(authority.Certificate[0])
	if err != nil || !ca.IsCA || !now.Before(ca.NotAfter) {
		return protocol.PoolMaterial{}, fmt.Errorf("池 CA 已过期或无效")
	}
	template := &x509.Certificate{
		Subject: pkix.Name{CommonName: "opsd-pool-" + node}, IPAddresses: []net.IP{net.ParseIP(ip)},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: earlier(now.Add(LeafLifetime), ca.NotAfter),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	out := protocol.PoolMaterial{CA: a.Cert, FilerKey: filerKey, CASeal: seal}
	out.Cert, out.Key, err = issuePoolLeaf(template, ca, authority.PrivateKey)
	if err == nil && seal != "" {
		template.Subject.CommonName = EtcdPeerName(node)
		out.PeerCert, out.PeerKey, err = issuePoolLeaf(template, ca, authority.PrivateKey)
	}
	return out, err
}

func EtcdPeerName(node string) string { return "opsd-etcd-" + node }

func issuePoolLeaf(template, ca *x509.Certificate, signer any) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	return certificate(template, ca, key, signer)
}

func VerifyEtcdPeer(m protocol.PoolMaterial, node, ip string, now time.Time) error {
	if m.CASeal == "" {
		if m.PeerCert == "" && m.PeerKey == "" {
			return nil
		}
		return fmt.Errorf("普通成员不能持有 etcd 对等证书")
	}
	if _, _, err := VerifyMaterial(protocol.PoolMaterial{CA: m.CA, Cert: m.PeerCert, Key: m.PeerKey}, ip, now); err != nil {
		return err
	}
	block, _ := pem.Decode([]byte(m.PeerCert))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || cert.Subject.CommonName != EtcdPeerName(node) {
		return fmt.Errorf("etcd 对等证书身份无效")
	}
	return nil
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func certificate(template, issuer *x509.Certificate, key *ecdsa.PrivateKey, signer any) (string, string, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	template.SerialNumber = serial
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, key.Public(), signer)
	if err != nil {
		return "", "", err
	}
	secret, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: secret})), nil
}

func VerifyMaterial(m protocol.PoolMaterial, ip string, now time.Time) (*tls.Config, time.Time, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(m.CA)) {
		return nil, time.Time{}, fmt.Errorf("存储池 CA 无效")
	}
	pair, err := tls.X509KeyPair([]byte(m.Cert), []byte(m.Key))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("存储池证书或密钥无效")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, time.Time{}, err
	}
	opts := x509.VerifyOptions{Roots: roots, DNSName: ip, CurrentTime: now}
	if _, err = leaf.Verify(opts); err != nil {
		return nil, time.Time{}, err
	}
	opts.KeyUsages = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	if _, err = leaf.Verify(opts); err != nil {
		return nil, time.Time{}, err
	}
	return &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, leaf.NotAfter, nil
}
