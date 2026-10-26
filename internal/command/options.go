package command

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opsd-labs/opsd/internal/account"
	"github.com/opsd-labs/opsd/internal/identity"
	"golang.org/x/term"
)

const DefaultInstance = "/etc/opsd/instance.json"
const DefaultAgentDir = "/var/lib/opsd-agent"

func Flags(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	// 默认值可能来自密码环境变量，不让 flag 帮助输出这些值。
	set.SetOutput(io.Discard)
	return set
}

func Parse(set *flag.FlagSet, args []string) error {
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return fmt.Errorf("请参阅 README 中的 %s 用法", set.Name())
		}
		return fmt.Errorf("命令参数无效：%w", err)
	}
	if set.NArg() != 0 {
		return fmt.Errorf("命令包含多余参数")
	}
	return nil
}

func Prompt(label, initial string, hidden bool) (string, error) {
	fmt.Fprintf(os.Stdout, "%s", label)
	if initial != "" {
		fmt.Fprintf(os.Stdout, " [%s]", initial)
	}
	fmt.Fprint(os.Stdout, "：")
	var value string
	if hidden {
		data, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stdout)
		if err != nil {
			return "", err
		}
		value = string(data)
	} else {
		var data []byte
		for {
			var b [1]byte
			_, err := os.Stdin.Read(b[:])
			if err != nil {
				return "", err
			}
			if b[0] == '\n' {
				break
			}
			if b[0] != '\r' {
				data = append(data, b[0])
			}
		}
		value = strings.TrimSpace(string(data))
	}
	if value == "" {
		return initial, nil
	}
	return value, nil
}

type InstallOptions struct {
	Directory, Port, Entrance, Username, Password string
	Address, Public, Token                        string
	NonInteractive                                bool
}

func ReadInstall(args []string, agentOnly bool) (InstallOptions, error) {
	var value InstallOptions
	set := Flags("install")
	set.StringVar(&value.Directory, "install-dir", os.Getenv("OPSD_INSTALL_DIR"), "")
	if agentOnly {
		set.StringVar(&value.Address, "control-url", os.Getenv("OPSD_CONTROL_URL"), "")
		set.StringVar(&value.Public, "control-public-key", os.Getenv("OPSD_CONTROL_PUBLIC_KEY"), "")
		set.StringVar(&value.Token, "enrollment-token", os.Getenv("OPSD_ENROLLMENT_TOKEN"), "")
	} else {
		set.StringVar(&value.Port, "port", os.Getenv("OPSD_PORT"), "")
		set.StringVar(&value.Entrance, "entrance", os.Getenv("OPSD_ENTRANCE"), "")
		set.StringVar(&value.Username, "username", os.Getenv("OPSD_USERNAME"), "")
		set.StringVar(&value.Password, "password", os.Getenv("OPSD_PASSWORD"), "")
	}
	set.BoolVar(&value.NonInteractive, "non-interactive", value.NonInteractive, "")
	if err := Parse(set, args); err != nil {
		return value, err
	}
	provided := map[string]bool{}
	set.Visit(func(f *flag.Flag) { provided[f.Name] = true })
	if !provided["non-interactive"] && os.Getenv("OPSD_NON_INTERACTIVE") != "" {
		var err error
		value.NonInteractive, err = strconv.ParseBool(os.Getenv("OPSD_NON_INTERACTIVE"))
		if err != nil {
			return value, fmt.Errorf("OPSD_NON_INTERACTIVE 须为 true 或 false")
		}
	}
	if !value.NonInteractive && !term.IsTerminal(int(os.Stdin.Fd())) {
		return value, fmt.Errorf("无交互终端，请显式使用 --non-interactive 或 OPSD_NON_INTERACTIVE=true")
	}
	ask := func(name, environment, label string, target *string, initial string, hidden bool) error {
		_, inEnvironment := os.LookupEnv(environment)
		if value.NonInteractive || provided[name] || inEnvironment {
			return nil
		}
		result, err := Prompt(label, initial, hidden)
		if err == nil {
			*target = result
		}
		return err
	}
	if err := ask("install-dir", "OPSD_INSTALL_DIR", "安装目录", &value.Directory, "/opt/opsd", false); err != nil {
		return value, err
	}
	if value.Directory == "" {
		value.Directory = "/opt/opsd"
	}
	if !filepath.IsAbs(value.Directory) || filepath.Clean(value.Directory) == string(filepath.Separator) || strings.ContainsAny(value.Directory, "\n\r\x00") {
		return value, fmt.Errorf("安装目录须为明确的绝对目录，且不能是根目录")
	}
	value.Directory = filepath.Clean(value.Directory)
	if agentOnly {
		for _, item := range []struct {
			name, environment, label string
			target                   *string
		}{{"control-url", "OPSD_CONTROL_URL", "控制端地址", &value.Address}, {"control-public-key", "OPSD_CONTROL_PUBLIC_KEY", "控制端公钥", &value.Public}, {"enrollment-token", "OPSD_ENROLLMENT_TOKEN", "接入码", &value.Token}} {
			if err := ask(item.name, item.environment, item.label, item.target, "", item.name == "enrollment-token"); err != nil {
				return value, err
			}
		}
		if value.Address == "" || value.Public == "" || value.Token == "" {
			return value, fmt.Errorf("Agent 安装须提供控制端地址、公钥和接入码")
		}
		return value, nil
	}
	for _, item := range []struct {
		name, environment, label string
		target                   *string
		hidden                   bool
	}{{"port", "OPSD_PORT", "监听端口（留空自动选择）", &value.Port, false}, {"entrance", "OPSD_ENTRANCE", "安全入口（留空生成）", &value.Entrance, false}, {"username", "OPSD_USERNAME", "管理员账号（留空生成）", &value.Username, false}, {"password", "OPSD_PASSWORD", "管理员密码（留空生成）", &value.Password, true}} {
		if err := ask(item.name, item.environment, item.label, item.target, "", item.hidden); err != nil {
			return value, err
		}
	}
	if value.Port == "" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return value, err
		}
		value.Port = strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
		listener.Close()
	}
	number, err := strconv.Atoi(value.Port)
	if err != nil || number < 1 || number > 65535 {
		return value, fmt.Errorf("端口须为 1 至 65535 的整数")
	}
	if value.Entrance == "" {
		value.Entrance = "/s_" + identity.Random()
	}
	if value.Username == "" {
		value.Username = "admin_" + identity.Random()[:8]
	}
	if value.Password == "" {
		value.Password = identity.Random()
	}
	if err := account.Validate(value.Username, value.Entrance); err != nil {
		return value, err
	}
	if len(value.Password) < 8 || len(value.Password) > 1024 {
		return value, fmt.Errorf("密码长度须为 8 至 1024 字节")
	}
	return value, nil
}
