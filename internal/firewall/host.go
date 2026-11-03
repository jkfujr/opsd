// Package firewall 封装原生防火墙；控制端不能通过此包执行主机命令。
package firewall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Host 只隔离原生系统边界，供后端格式与失败契约测试使用。
type Host interface {
	Run(context.Context, string, ...string) ([]byte, error)
	Read(string) ([]byte, error)
	Replace(string, []byte, []byte) error
	Exists(string) bool
	LookPath(string) bool
}

type NativeHost struct{}

func (NativeHost) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return runNative(ctx, nil, name, args...)
}

// RunInput 向原生命令传递规则事务，不经过 Shell 或临时脚本。
func (NativeHost) RunInput(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	return runNative(ctx, input, name, args...)
}

func runNative(ctx context.Context, input []byte, name string, args ...string) ([]byte, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("原生防火墙仅支持 Linux")
	}
	duration := 30 * time.Second
	if name == "apt-get" {
		duration = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "DEBIAN_FRONTEND=noninteractive")
	configureCommand(cmd)
	var output cappedBuffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if output.exceeded {
		return nil, fmt.Errorf("原生命令输出超过 8 MiB，结果不完整")
	}
	if err != nil {
		return output.Bytes(), fmt.Errorf("%s 执行失败：%w：%s", name, err, strings.TrimSpace(output.String()))
	}
	return output.Bytes(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	exceeded bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > 8*1024*1024 {
		b.exceeded = true
		return n, nil
	}
	return b.Buffer.Write(p)
}
func (NativeHost) Read(path string) ([]byte, error) { return os.ReadFile(path) }
func (NativeHost) Exists(path string) bool          { _, err := os.Stat(path); return err == nil }
func (NativeHost) LookPath(name string) bool        { _, err := exec.LookPath(name); return err == nil }
func (NativeHost) Replace(path string, before, after []byte) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("配置不是普通文件，拒绝改写")
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return Conflict("配置在写入前发生变化")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".opsd-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(info.Mode().Perm()); err == nil {
		err = preserveOwner(f, info)
	}
	if err == nil {
		_, err = f.Write(after)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	current, err = os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return Conflict("配置在提交前发生变化")
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

type Fault struct {
	Code    string
	Message string
}

func (e *Fault) Error() string         { return e.Message }
func Conflict(message string) error    { return &Fault{Code: "conflict", Message: message} }
func Unsupported(message string) error { return &Fault{Code: "invalid_request", Message: message} }
func Code(err error) string {
	var e *Fault
	if errors.As(err, &e) {
		return e.Code
	}
	return "dependency_unavailable"
}
