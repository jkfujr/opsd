package command

import (
	"errors"
	"flag"
	"fmt"
	"golang.org/x/term"
	"io"
	"os"
	"strings"
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
