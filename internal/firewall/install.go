package firewall

import (
	"context"
	"fmt"
	"github.com/opsd-labs/opsd/internal/protocol"
	"strings"
)

type InstallState struct {
	Backend          protocol.Backend `json:"backend"`
	Stage            string           `json:"stage"`
	MaskOwned        bool             `json:"mask_owned"`
	AlreadyInstalled bool             `json:"already_installed"`
}

func (m *Manager) installable() bool {
	b, e := m.Host.Read("/etc/os-release")
	if e != nil {
		return false
	}
	id := configValue(string(b), "ID")
	return (id == "debian" || id == "ubuntu") && m.Host.LookPath("apt-get")
}
func (m *Manager) Install(ctx context.Context, state *InstallState, persist func() error) error {
	if !m.installable() {
		return Unsupported("自动安装仅支持 Debian 或 Ubuntu 的系统软件源")
	}
	packages := map[protocol.Backend]string{"ufw": "ufw", "firewalld": "firewalld", "nftables": "nftables"}
	pkg := packages[state.Backend]
	if pkg == "" {
		return Unsupported("安装目标无效")
	}
	unit := pkg + ".service"
	set := func(stage string) error { state.Stage = stage; return persist() }
	if state.Stage == "complete" {
		return nil
	}
	backend, e := m.Detect(ctx)
	if e != nil {
		return e
	}
	if backend != "none" && backend != state.Backend {
		return Conflict("已有其他管理器配置，拒绝自动切换后端")
	}
	if b, _ := m.Host.Run(ctx, "systemctl", "is-active", unit); strings.TrimSpace(string(b)) == "active" {
		return Conflict("目标服务已在运行，安装操作不接管或停止它")
	}
	installed := func() bool {
		b, e := m.Host.Run(ctx, "dpkg-query", "-W", "-f=${Status}", pkg)
		return e == nil && strings.TrimSpace(string(b)) == "install ok installed"
	}
	if state.Stage == "" {
		state.AlreadyInstalled = installed()
		if state.AlreadyInstalled {
			return set("complete")
		}
		if e = set("prepared"); e != nil {
			return e
		}
	}
	if state.Stage == "prepared" {
		b, _ := m.Host.Run(ctx, "systemctl", "is-enabled", unit)
		if strings.TrimSpace(string(b)) == "masked" {
			state.MaskOwned = false
			if e = set("masked"); e != nil {
				return e
			}
		} else {
			state.MaskOwned = true
			if e = set("mask_intent"); e != nil {
				return e
			}
		}
	}
	if state.Stage == "mask_intent" {
		if b, _ := m.Host.Run(ctx, "systemctl", "is-enabled", unit); strings.TrimSpace(string(b)) == "masked" {
			return Conflict("屏蔽意图中断后发现已有屏蔽，无法确认归属；请在本机处理后重试")
		}
		if _, e = m.Host.Run(ctx, "systemctl", "mask", unit); e != nil {
			return e
		}
		b, _ := m.Host.Run(ctx, "systemctl", "is-enabled", unit)
		if strings.TrimSpace(string(b)) != "masked" {
			return fmt.Errorf("目标服务未被持久屏蔽，不能继续安装")
		}
		if e = set("masked"); e != nil {
			return e
		}
	}
	if state.Stage == "masked" || state.Stage == "installing" {
		b, _ := m.Host.Run(ctx, "systemctl", "is-enabled", unit)
		if strings.TrimSpace(string(b)) != "masked" {
			return Conflict("目标服务屏蔽已被外部改变，停止安装")
		}
		if !installed() {
			if e = set("installing"); e != nil {
				return e
			}
			if _, e = m.Host.Run(ctx, "apt-get", "install", "--yes", "--no-install-recommends", pkg); e != nil {
				return e
			}
		}
		if !installed() {
			return fmt.Errorf("软件包尚未完成安装")
		}
		if e = set("disabling"); e != nil {
			return e
		}
	}
	if state.Stage == "disabling" {
		if b, _ := m.Host.Run(ctx, "systemctl", "is-active", unit); strings.TrimSpace(string(b)) == "active" {
			return Conflict("安装期间目标服务意外启动，须先在本机处理")
		}
		if _, e = m.Host.Run(ctx, "systemctl", "disable", unit); e != nil {
			return e
		}
		if e = set("unmasking"); e != nil {
			return e
		}
	}
	if state.Stage == "unmasking" {
		if state.MaskOwned {
			b, _ := m.Host.Run(ctx, "systemctl", "is-enabled", unit)
			mode := strings.TrimSpace(string(b))
			if mode == "masked" {
				if _, e = m.Host.Run(ctx, "systemctl", "unmask", unit); e != nil {
					return e
				}
			} else if mode != "disabled" && mode != "not-found" && mode != "static" {
				return Conflict("目标服务状态被外部改变，不能移除屏蔽")
			}
		}
		if b, _ := m.Host.Run(ctx, "systemctl", "is-active", unit); strings.TrimSpace(string(b)) == "active" {
			return Conflict("目标服务已启动，不能报告仅安装成功")
		}
		if state.Backend == "ufw" {
			b, e := m.Host.Read("/etc/ufw/ufw.conf")
			if e != nil {
				return e
			}
			if configValue(string(b), "ENABLED") == "yes" {
				return Conflict("UFW 已启用，不能报告仅安装成功")
			}
		}
		return set("complete")
	}
	return Unsupported("安装阶段无法解释")
}
