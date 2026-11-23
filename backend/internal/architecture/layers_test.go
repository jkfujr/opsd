package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 这些规则全部是"当前已经成立"的事实，用测试把它们钉住，防止后续改动
// 悄悄把依赖方向反过来。规则里没有一条需要修改现有代码才能通过。
//
// 只统计生产源码（跳过 _test.go 与 testdata）：测试可以跨层引用构造夹具，
// 那不属于模块的依赖方向。

const modulePrefix = "github.com/jkfujr/opsd/internal/"

// 叶子层：协议、身份、存储、构建信息、请求状态不依赖任何其他 internal 包。
var leafPackages = []string{"protocol", "identity", "storage", "buildinfo", "requeststate"}

// 控制面之外的包一律不得依赖控制面；控制面自身不得依赖命令行入口。
var forbidden = map[string][]string{
	"control":      {"cli"},
	"executor":     {"control", "cli"},
	"installation": {"control", "cli"},
}

// 观测面与传输层不得依赖业务枢纽（cluster）或控制面。
var observability = map[string][]string{
	"monitoring":   {"cluster", "control", "executor", "cli"},
	"metricsstore": {"cluster", "control", "executor", "cli"},
	"telemetry":    {"cluster", "control", "executor", "cli"},
	"objectstore":  {"cluster", "control", "executor", "cli"},
	"terminal":     {"cluster", "control", "executor", "cli"},
	"transport":    {"cluster", "control", "executor", "cli"},
}

// importsOf 解析 backend/internal 下所有包的直接 import，返回 包名 -> 目标包集合。
func importsOf(t *testing.T) map[string]map[string]bool {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件")
	}
	// backend/internal/architecture/layers_test.go -> backend/internal
	root := filepath.Dir(filepath.Dir(file))
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]map[string]bool{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		targets := map[string]bool{}
		err = filepath.WalkDir(filepath.Join(root, name), func(path string, item os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// 只看生产源码：测试文件与测试夹具可以跨层引用，不构成依赖方向。
			if item.IsDir() {
				if item.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				value, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}
				if !strings.HasPrefix(value, modulePrefix) {
					continue
				}
				if target := strings.TrimPrefix(value, modulePrefix); target != name {
					targets[target] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		result[name] = targets
	}
	if len(result) < 20 {
		t.Fatalf("只解析到 %d 个包，测试可能定位错了目录", len(result))
	}
	return result
}

func TestLeafPackagesHaveNoInternalDependencies(t *testing.T) {
	imports := importsOf(t)
	for _, name := range leafPackages {
		targets, ok := imports[name]
		if !ok {
			t.Fatalf("缺少包 %s", name)
		}
		for _, target := range sorted(targets) {
			t.Errorf("%s 属于叶子层，却依赖 %s", name, target)
		}
	}
}

func TestNothingDependsOnConsoleOrCLI(t *testing.T) {
	imports := importsOf(t)
	for name, targets := range imports {
		if name == "cli" {
			continue // 命令行入口只被 main 使用，允许它依赖其他包。
		}
		if targets["control"] && name != "cli" {
			t.Errorf("%s 依赖控制面 control；只有 cli 可以", name)
		}
		if targets["cli"] {
			t.Errorf("%s 依赖命令行入口 cli", name)
		}
	}
}

func TestObservabilityAvoidsBusinessHub(t *testing.T) {
	imports := importsOf(t)
	for name, banned := range observability {
		targets, ok := imports[name]
		if !ok {
			t.Fatalf("缺少包 %s", name)
		}
		for _, target := range banned {
			if targets[target] {
				t.Errorf("%s 属于观测/传输层，却依赖 %s", name, target)
			}
		}
	}
}

func TestExecutorAndInstallationAvoidConsole(t *testing.T) {
	imports := importsOf(t)
	for name, banned := range forbidden {
		targets, ok := imports[name]
		if !ok {
			continue
		}
		for _, target := range banned {
			if targets[target] {
				t.Errorf("%s 不应依赖 %s", name, target)
			}
		}
	}
}

func sorted(set map[string]bool) []string {
	list := make([]string, 0, len(set))
	for value := range set {
		list = append(list, value)
	}
	sort.Strings(list)
	return list
}