package control

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jkfujr/opsd/internal/webassets"
)

// 复用 Go HTTP 服务与现有 Playwright，在生产构建资源上验证整套主题。
func TestConsoleThemeBrowser(t *testing.T) {
	if os.Getenv("OPSD_THEME_UI") != "1" {
		t.Skip("设置 OPSD_THEME_UI=1 运行主题浏览器验证")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	run := func(mode string, environment ...string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), "node", "scripts/check-themes.mjs", mode)
		command.Dir = root
		command.Env = append(os.Environ(), environment...)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("主题浏览器 %s 失败：%v\n%s", mode, err, output)
		}
		t.Log(string(output))
	}
	run("build")
	for _, name := range []string{"react", "vue", "broken"} {
		packageThemeDirectory(t, filepath.Join(root, "artifacts", "theme-ui", name), filepath.Join(root, "artifacts", "theme-ui", name+".zip"))
	}
	c := openThemeConsole(t, webassets.Assets())
	c.node.Start()
	run("check", "OPSD_THEME_URL="+c.web.URL)
	command := func(arguments []string, environment ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "node", arguments...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), environment...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("界面契约检查失败：%v\n%s", err, output)
		}
		t.Log(string(output))
	}
	command([]string{"frontend/gpt2/node_modules/vite/bin/vite.js", "build", "frontend/gpt2"})
	command([]string{"frontend/gpt2/scripts/package-theme.mjs"})
	browser := os.Getenv("OPSD_THEME_BROWSER")
	if browser == "" {
		browser = "chromium"
	}
	for _, mode := range []string{"theme", "builtin", "recovery"} {
		t.Run(mode, func(t *testing.T) {
			instance := openThemeConsole(t, webassets.Assets())
			instance.node.Start()
			arguments := []string{"frontend/gpt2/scripts/check-ui.mjs", "--backend-contract"}
			if mode != "theme" {
				arguments = append(arguments, "--"+mode)
			}
			cmd := exec.CommandContext(t.Context(), "node", arguments...)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "OPSD_GPT2_URL="+instance.web.URL, "OPSD_GPT2_BROWSER="+browser)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s 界面契约检查失败：%v\n%s", mode, err, output)
			} else {
				t.Log(string(output))
			}
		})
	}
}

func packageThemeDirectory(t *testing.T, directory, destination string) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	write := func(filename string) error {
		name, err := filepath.Rel(directory, filename)
		if err != nil {
			return err
		}
		source, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer source.Close()
		target, err := writer.Create(filepath.ToSlash(name))
		if err != nil {
			return err
		}
		_, err = io.Copy(target, source)
		return err
	}
	if err = write(filepath.Join(directory, "theme.json")); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(filepath.Join(directory, "dist"), func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		return write(name)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
}
