package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/1204244136/MDA/agent/go-service/taskersink/membership"
)

type packageData struct {
	ValidDate  string
	SponsorURL string
}

var refillMainTemplate = template.Must(template.New("refill-main").Parse(`package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"

	"github.com/1204244136/MDA/agent/go-service/taskersink/membership"
)

const validDate = {{ printf "%q" .ValidDate }}
const sponsorURL = {{ printf "%q" .SponsorURL }}

func main() {
	result, err := membership.RefillQuotaForSponsorURL(validDate, sponsorURL)
	if err != nil {
		switch {
		case errors.Is(err, membership.ErrRefillDateMismatch):
			fmt.Println("补充包仅限", validDate, "当天使用。")
		case errors.Is(err, membership.ErrRefillDeviceMismatch):
			fmt.Println("补充包不适用于当前设备。")
		default:
			fmt.Println("额度补充失败:", err)
		}
		waitForEnter()
		os.Exit(1)
	}
	fmt.Println("额度已清空。")
	fmt.Println("生效日期:", result.BusinessDate)
	fmt.Println("额度文件:", result.Path)
	waitForEnter()
}

func waitForEnter() {
	fmt.Println()
	fmt.Print("按回车键退出...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}
`))

func main() {
	date := flag.String("date", "", "适用日期，格式 YYYY-MM-DD")
	sponsorURL := flag.String("sponsor-url", "", "用户赞助链接")
	output := flag.String("out", "", "输出 exe 路径，默认 quota-refill-YYYY-MM-DD.exe")
	keepTemp := flag.Bool("keep-temp", false, "保留临时补充包源码目录")
	flag.Parse()

	if err := run(*date, *sponsorURL, *output, *keepTemp); err != nil {
		fmt.Fprintln(os.Stderr, "补充包生成失败:", err)
		os.Exit(1)
	}
}

func run(validDate, sponsorURL, output string, keepTemp bool) error {
	validDate = strings.TrimSpace(validDate)
	sponsorURL = strings.TrimSpace(sponsorURL)
	if _, err := time.Parse("2006-01-02", validDate); err != nil {
		return fmt.Errorf("适用日期必须是 YYYY-MM-DD: %w", err)
	}
	if _, err := membership.DeviceHashFromSponsorURL(sponsorURL); err != nil {
		return fmt.Errorf("赞助链接无效: %w", err)
	}
	if output == "" {
		output = "quota-refill-" + validDate + ".exe"
	}
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absOutput), 0755); err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", "mda-quota-refill-*")
	if err != nil {
		return err
	}
	if !keepTemp {
		defer os.RemoveAll(tempDir)
	}
	mainPath := filepath.Join(tempDir, "main.go")
	file, err := os.Create(mainPath)
	if err != nil {
		return err
	}
	if err := refillMainTemplate.Execute(file, packageData{ValidDate: validDate, SponsorURL: sponsorURL}); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	cmd := exec.Command("go", "build", "-trimpath", "-o", absOutput, mainPath)
	cmd.Dir = moduleRoot()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	fmt.Println("补充包已生成:", absOutput)
	if keepTemp {
		fmt.Println("临时源码目录:", tempDir)
	}
	return nil
}

func moduleRoot() string {
	dir, err := os.Getwd()
	if err == nil {
		if root := findModuleRoot(dir); root != "" {
			return root
		}
	}
	exe, err := os.Executable()
	if err == nil {
		if root := findModuleRoot(filepath.Dir(exe)); root != "" {
			return root
		}
	}
	return "."
}

func findModuleRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
