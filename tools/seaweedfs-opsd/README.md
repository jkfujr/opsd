# SeaweedFS OPSD 衍生镜像

`4.55-opsd.1` 基于 SeaweedFS 4.55 的 `a98c9e27185c`。固定构建器、上游源和运行基础镜像，包中的 `weed opsd-ec` 由池的 root 执行器调用。用镜像摘要写入建池计划。四个或更多故障域且成员均具备镜像的池可以启用 10+4 转换。 [池 API](../../docs/API.md#集群-s3-池)

## 构建

在 OPSD-CI 的 Linux 仓库根目录执行，使用支持容器镜像存储的 Docker 和 Buildx。Dockerfile 在解包前校验 Go 模块归档的 SHA-256。

```bash
mkdir -p artifacts/seaweedfs-opsd/upstream
proxy=${GOPROXY_MIRROR:-https://goproxy.cn}
module=github.com/seaweedfs/seaweedfs/@v/v0.0.0-20261123030105-a98c9e27185c.zip
curl -fSL "$proxy/$module" -o artifacts/seaweedfs-opsd/upstream/go-module.zip
sha256sum tools/seaweedfs-opsd/Dockerfile tools/seaweedfs-opsd/managed-ec.patch tools/seaweedfs-opsd/opsd_ec_*.go
buildx_config=()
if [[ -x artifacts/opsd-i6/docker-config/cli-plugins/docker-buildx ]]; then
  buildx_config=(--config "$PWD/artifacts/opsd-i6/docker-config")
fi
docker "${buildx_config[@]}" buildx build --no-cache --platform linux/amd64 --provenance=false --sbom=false \
  --build-arg SOURCE_DATE_EPOCH=1795402865 \
  --build-context upstream=artifacts/seaweedfs-opsd/upstream \
  --output 'type=image,name=opsd/seaweedfs:4.55-opsd.1,rewrite-timestamp=true,unpack=false' \
  tools/seaweedfs-opsd
docker image inspect --format '{{.Id}} {{.Created}}' opsd/seaweedfs:4.55-opsd.1
docker run --rm --entrypoint /bin/sh opsd/seaweedfs:4.55-opsd.1 -c 'sha256sum /usr/bin/weed && weed version'
```

两次执行构建，比较 BuildKit 输出的 manifest、config 摘要和 `/usr/bin/weed` 的 SHA-256。预览前在各池成员上准备同平台镜像。

## 卷恢复

从池详情核对原卷、代际和 `cleanup_pending`。原步骤按同一代际重新核对，清理前保留已有布局。分片修复需要至少 10 个内容匹配的分片，以及恢复成员仍匹配原代际的卷信息和索引。发现错代际或同名文件摘要不一致时保留文件和执行回执。 [构建与隔离验收命令](../../docs/VALIDATION.md)
