package storageroot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jkfujr/opsd/internal/dockerengine"
	"github.com/jkfujr/opsd/internal/localstate"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/storagepool"
	"github.com/moby/moby/api/types/container"
	dockerMount "github.com/moby/moby/api/types/mount"
	"golang.org/x/sys/unix"
)

const poolControlComponent = "pool_control"
const poolConfigDir = "/etc/seaweedfs"
const poolActive = poolConfigDir + "/active"
const poolEtcdPeerDir = "/etc/opsd-etcd-peer"
const poolS3Config = "/etc/opsd-s3.json"
const poolRoleUID = 65534

func poolTemplate(d poolNodeRecord, config, role, rel, image, program string, args []string) dockerengine.StoragePoolTemplate {
	sum := sha256.Sum256([]byte(d.PoolID + "\x00" + d.Member.NodeID))
	return dockerengine.StoragePoolTemplate{
		Name: fmt.Sprintf("opsd-pool-%x-%s", sum[:12], role),
		Config: container.Config{
			Image: image, Entrypoint: []string{program}, Cmd: args,
			WorkingDir: "/data", User: fmt.Sprintf("%d:%d", poolRoleUID, poolRoleUID), Env: []string{"HOME=/data"},
		},
		Host: container.HostConfig{
			NetworkMode: "host",
			Mounts: []dockerMount.Mount{
				{Type: dockerMount.TypeBind, Source: rel, Target: "/data"},
				{Type: dockerMount.TypeBind, Source: config, Target: poolConfigDir, ReadOnly: true},
			},
			LogConfig: container.LogConfig{Type: "json-file", Config: map[string]string{"max-size": "10m", "max-file": "3"}},
		},
	}
}

func poolMasters(nodes []protocol.PoolNodePlan) (masters, clients, peers string) {
	var m, c, p []string
	for _, n := range nodes {
		if !n.Control() {
			continue
		}
		m = append(m, storagepool.Address(n.PrivateIP, n.Ports.Master))
		c = append(c, "https://"+storagepool.Address(n.PrivateIP, n.Ports.EtcdClient))
		p = append(p, n.NodeID+"=https://"+storagepool.Address(n.PrivateIP, n.Ports.EtcdPeer))
	}
	return strings.Join(m, ","), strings.Join(c, ","), strings.Join(p, ",")
}

func poolFilers(nodes []protocol.PoolNodePlan) string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, storagepool.Address(n.PrivateIP, n.Ports.Filer))
	}
	return strings.Join(out, ",")
}

func poolWeedRole(d poolNodeRecord, config, role, rel string, port, grpc int, args ...string) dockerengine.StoragePoolTemplate {
	ip := d.Member.PrivateIP
	cmd := []string{role, "-ip=" + ip, "-ip.bind=" + ip, "-port=" + strconv.Itoa(port), "-port.grpc=" + strconv.Itoa(grpc)}
	cmd = append(cmd, args...)
	image := d.Member.ECImage
	if image == "" {
		image = protocol.PoolSeaweedImage
	}
	return poolTemplate(d, config, role, rel, image, "weed", cmd)
}

func poolVolumeTemplate(d poolNodeRecord, config string) dockerengine.StoragePoolTemplate {
	m := d.Member
	masters, _, _ := poolMasters(d.Members)
	// SeaweedFS 用 rack 表达跨故障域的第二副本。
	rack := sha256.Sum256([]byte(m.FailureDomain))
	args := []string{"-master=" + masters, "-dir=/data", "-dataCenter=opsd", fmt.Sprintf("-rack=%x", rack[:12]), "-max=0", "-minFreeSpace=10"}
	if m.ECImage != "" {
		args[1] = "-dir=/data/replica,/data/ec"
		args = append(args, "-disk=hdd,ssd")
	}
	return poolWeedRole(d, config, "volume", "data/volume", m.Ports.Volume, m.Ports.VolumeGRPC, args...)
}

func poolControlTemplates(d poolNodeRecord, config string, includeAPI bool) []dockerengine.StoragePoolTemplate {
	m := d.Member
	masters, _, peers := poolMasters(d.Members)
	out := []dockerengine.StoragePoolTemplate{}
	if m.Control() {
		client := "https://" + storagepool.Address(m.PrivateIP, m.Ports.EtcdClient)
		peer := "https://" + storagepool.Address(m.PrivateIP, m.Ports.EtcdPeer)
		cert, key, ca := poolActive+"/tls.crt", poolActive+"/tls.key", poolActive+"/ca.crt"
		var peerNames []string
		for _, member := range d.Members {
			if member.Control() {
				peerNames = append(peerNames, storagepool.EtcdPeerName(member.NodeID))
			}
		}
		peerTLS := poolEtcdPeerDir + "/active"
		template := poolTemplate(d, config, "etcd", "control/etcd", protocol.PoolEtcdImage, "/usr/local/bin/etcd",
			[]string{"--name=" + m.NodeID, "--data-dir=/data/member", "--initial-cluster=" + peers,
				"--initial-cluster-token=" + d.PoolID, "--initial-cluster-state=new",
				"--listen-client-urls=" + client, "--advertise-client-urls=" + client,
				"--listen-peer-urls=" + peer, "--initial-advertise-peer-urls=" + peer,
				"--cert-file=" + cert, "--key-file=" + key, "--trusted-ca-file=" + ca, "--client-cert-auth=true",
				"--peer-cert-file=" + peerTLS + "/etcd-peer.crt", "--peer-key-file=" + peerTLS + "/etcd-peer.key",
				"--peer-trusted-ca-file=" + peerTLS + "/ca.crt", "--peer-client-cert-auth=true",
				"--peer-cert-allowed-cn=" + strings.Join(peerNames, ","), "--peer-skip-client-san-verification=true",
				"--heartbeat-interval=250", "--election-timeout=2500", "--quota-backend-bytes=2147483648",
				"--auto-compaction-mode=periodic", "--auto-compaction-retention=1h"})
		template.Host.Mounts = append(template.Host.Mounts, dockerMount.Mount{
			Type: dockerMount.TypeBind, Source: config + "-etcd", Target: poolEtcdPeerDir, ReadOnly: true,
		})
		out = append(out, template)
		out = append(out, poolWeedRole(d, config, "master", "control/master", m.Ports.Master, m.Ports.MasterGRPC,
			"-peers="+masters, "-mdir=/data", "-volumeSizeLimitMB=1024", "-defaultReplication=010", "-telemetry=false"))
	}
	if includeAPI {
		out = append(out, poolWeedRole(d, config, "filer", "control/filer", m.Ports.Filer, m.Ports.FilerGRPC,
			"-master="+masters, "-defaultStoreDir="+poolConfigDir+"/no-local-filer-store",
			"-defaultReplicaPlacement=010", "-filerGroup="+d.PoolID))
		template := poolWeedRole(d, config, "s3", "control/s3", m.Ports.S3, m.Ports.S3GRPC,
			"-filer="+poolFilers(d.Members), "-config="+poolS3Config,
			"-cert.file="+poolActive+"/tls.crt", "-key.file="+poolActive+"/tls.key",
			"-iam=false", "-autoCreateBucket=false", "-port.iceberg=0", "-port.lance=0")
		template.Host.Mounts = append(template.Host.Mounts, dockerMount.Mount{
			Type: dockerMount.TypeBind, Source: poolS3ConfigPath(config), Target: poolS3Config, ReadOnly: true,
		})
		out = append(out, template)
	}
	return out
}

func poolS3ConfigPath(config string) string { return filepath.Join(filepath.Dir(config), "s3.json") }

func makePoolRoleDir(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&07777 != 0700 || (st.Uid != 0 || st.Gid != 0) && (st.Uid != poolRoleUID || st.Gid != poolRoleUID) {
		return fault("conflict", "池组件写入目录权限或属主无效：%s", path)
	}
	if st.Uid == 0 {
		return unix.Fchown(fd, poolRoleUID, poolRoleUID)
	}
	return nil
}

func makePoolConfigDir(path string) error {
	if err := os.Mkdir(path, 0750); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := os.Chown(path, 0, poolRoleUID); err != nil {
		return err
	}
	return os.Chmod(path, 0750)
}

func writePoolFilerConfig(dir, id, clients string) error {
	filer := fmt.Sprintf("[etcd]\nenabled = true\nservers = %q\nkey_prefix = %q\ntimeout = \"3s\"\ntls_ca_file = %q\ntls_client_crt_file = %q\ntls_client_key_file = %q\n",
		clients, id+"/", poolActive+"/ca.crt", poolActive+"/tls.crt", poolActive+"/tls.key")
	return writePoolFile(filepath.Join(dir, "filer.toml"), filer, poolRoleUID)
}

func writePoolConfig(dir string, d poolNodeRecord, material protocol.PoolMaterial, credentials protocol.PoolCredentials) error {
	if err := makePoolConfigDir(dir); err != nil {
		return err
	}
	_, clients, _ := poolMasters(d.Members)
	if err := writePoolFilerConfig(dir, d.PoolID, clients); err != nil {
		return err
	}
	security := fmt.Sprintf("[grpc]\nca = %q\nverify_server_host = true\n", poolActive+"/ca.crt")
	for _, component := range []string{"master", "volume", "filer", "s3", "client"} {
		security += fmt.Sprintf("[grpc.%s]\ncert = %q\nkey = %q\n", component, poolActive+"/tls.crt", poolActive+"/tls.key")
	}
	for _, component := range []string{"master", "volume", "filer", "client"} {
		security += fmt.Sprintf("[https.%s]\ncert = %q\nkey = %q\nca = %q\n",
			component, poolActive+"/tls.crt", poolActive+"/tls.key", poolActive+"/ca.crt")
		if component == "client" {
			security += "enabled = true\n"
		}
	}
	security += fmt.Sprintf("[jwt.filer_signing]\nkey = %q\nexpires_after_seconds = 60\n", material.FilerKey)
	for name, body := range map[string]string{
		"security.toml": security,
		"master.toml":   "[master.volume_growth]\ncopy_1=1\ncopy_2=1\ncopy_3=1\ncopy_other=1\n",
	} {
		if err := writePoolFile(filepath.Join(dir, name), body, poolRoleUID); err != nil {
			return err
		}
	}
	identity := []map[string]any{{"name": "opsd-admin", "credentials": []map[string]string{{"accessKey": credentials.AccessKey, "secretKey": credentials.SecretKey}},
		"actions": []string{"Admin", "Read", "List", "Write", "Tagging"}}}
	body, err := json.Marshal(map[string]any{"identities": identity})
	if err != nil {
		return err
	}
	if err := writePoolFile(poolS3ConfigPath(dir), string(body), poolRoleUID); err != nil {
		return err
	}
	_, err = switchPoolMaterial(dir, material)
	return err
}

func writePoolFile(path, body string, owner int) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".opsd-pool-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.WriteString(body); err == nil {
		err = file.Chown(owner, owner)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	return localstate.SyncDirectory(filepath.Dir(path))
}

func switchPoolMaterial(config string, m protocol.PoolMaterial) (string, error) {
	generation := storagepool.TLSGeneration(m)
	if err := writePoolGeneration(config, generation, map[string]string{"ca.crt": m.CA, "tls.crt": m.Cert, "tls.key": m.Key}); err != nil {
		return "", err
	}
	if m.PeerCert != "" {
		if err := makePoolConfigDir(config + "-etcd"); err != nil {
			return "", err
		}
		if err := writePoolGeneration(config+"-etcd", generation, map[string]string{"ca.crt": m.CA, "ca.sealed": m.CASeal,
			"etcd-peer.crt": m.PeerCert, "etcd-peer.key": m.PeerKey}); err != nil {
			return "", err
		}
	}
	old, _ := os.Readlink(filepath.Join(config, "active"))
	if old == generation {
		return old, nil
	}
	return old, activatePoolMaterial(config, generation)
}

func writePoolGeneration(config, generation string, files map[string]string) error {
	target := filepath.Join(config, generation)
	if err := makePoolConfigDir(target); err != nil {
		return err
	}
	for name, body := range files {
		owner := poolRoleUID
		if name == "ca.sealed" {
			owner = 0
		}
		if err := writePoolFile(filepath.Join(target, name), body, owner); err != nil {
			return err
		}
	}
	return nil
}

func activatePoolMaterial(config, generation string) error {
	if _, err := os.Stat(filepath.Join(config+"-etcd", generation)); err == nil {
		if err := activatePoolGeneration(config+"-etcd", generation); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return activatePoolGeneration(config, generation)
}

func activatePoolGeneration(config, generation string) error {
	temp := filepath.Join(config, ".active")
	if err := os.Remove(temp); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(generation, temp); err != nil {
		return err
	}
	if err := os.Rename(temp, filepath.Join(config, "active")); err != nil {
		return err
	}
	return localstate.SyncDirectory(config)
}

func readPoolMaterial(config string) (protocol.PoolMaterial, error) {
	out := protocol.PoolMaterial{}
	for name, value := range map[string]*string{"ca.crt": &out.CA, "tls.crt": &out.Cert, "tls.key": &out.Key} {
		body, err := os.ReadFile(filepath.Join(config, "active", name))
		if err != nil {
			return out, err
		}
		*value = string(body)
	}
	return out, nil
}

func (m *Manager) ensurePoolImages(ctx context.Context, member protocol.PoolNodePlan) error {
	var images []string
	if member.ECImage == "" {
		images = append(images, protocol.PoolSeaweedImage)
	} else {
		image, err := m.Docker.StoragePoolECImage(ctx)
		if err != nil {
			return err
		}
		if image != member.ECImage {
			return fault("precondition_failed", "本机 EC 镜像已变化，请重新预览")
		}
	}
	if member.Control() {
		images = append(images, protocol.PoolEtcdImage)
	}
	for _, img := range images {
		ok, err := m.Docker.DeploymentImageReady(ctx, img)
		if err != nil {
			return err
		}
		if !ok {
			if err := m.Docker.PullDeploymentImage(ctx, img); err != nil {
				return err
			}
		}
	}
	return nil
}
