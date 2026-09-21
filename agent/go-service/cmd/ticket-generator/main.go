package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/1204244136/MDA/agent/go-service/taskersink/membership"
)

const (
	defaultValidDays = 7
	maxValidDays     = 3650
	couponIDBytes    = 16
	goServiceModule  = "github.com/1204244136/MDA/agent/go-service"
)

var generatorBeijingLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type generatorInput struct {
	DurationSeconds int64
	TaskEntry       string
	SponsorURL      string
	ValidDays       int
	RefillType      membership.QuotaRefillType
}

type packageData struct {
	Coupon       membership.QuotaRefillCoupon
	ValidThrough string
	RefillLabel  string
	ScopeLabel   string
}

var refillMainTemplate = template.Must(template.New("refill-main").Parse(`package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"

	"github.com/1204244136/MDA/agent/go-service/taskersink/membership"
)

var coupon = membership.QuotaRefillCoupon{
	ID: {{ printf "%q" .Coupon.ID }},
	IssuedOn: {{ printf "%q" .Coupon.IssuedOn }},
	ValidDays: {{ .Coupon.ValidDays }},
	RefillType: membership.QuotaRefillType({{ printf "%q" .Coupon.RefillType }}),
	SponsorURL: {{ printf "%q" .Coupon.SponsorURL }},
 DurationSeconds: {{ .Coupon.DurationSeconds }},
 TaskEntry: {{ printf "%q" .Coupon.TaskEntry }},
}

const validThrough = {{ printf "%q" .ValidThrough }}
const refillLabel = {{ printf "%q" .RefillLabel }}
const scopeLabel = {{ printf "%q" .ScopeLabel }}

func main() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("MDA 额度兑换券")
	fmt.Println("识别码:", coupon.ID)
	fmt.Println("票券类型:", refillLabel)
	fmt.Println("适用范围:", scopeLabel)
 if coupon.RefillType == membership.QuotaRefillTypeEvent { fmt.Printf("活动额度：%d 分钟；任务入口：%s（空表示不限任务）\n", coupon.DurationSeconds/60, coupon.TaskEntry) }
	fmt.Printf("兑换有效期: %s 至 %s（含）\n", coupon.IssuedOn, validThrough)
	fmt.Println()
	if !waitForConfirmation(reader, "按回车键兑换...") {
		fmt.Println()
		fmt.Println("未收到回车确认，已取消兑换。")
		return
	}

	result, err := membership.RedeemQuotaRefillCoupon(coupon)
	if err != nil {
		switch {
		case errors.Is(err, membership.ErrRefillNotYetValid):
			fmt.Println("票券尚未生效，生效日期:", coupon.IssuedOn)
		case errors.Is(err, membership.ErrRefillExpired):
			fmt.Println("票券已过期，兑换有效期截止:", validThrough)
		case errors.Is(err, membership.ErrRefillDeviceMismatch):
			fmt.Println("票券不适用于当前设备。")
		case errors.Is(err, membership.ErrRefillStateMismatch):
			fmt.Println("额度状态与当前设备不一致，请先运行一次 MDA 后重试。")
		case errors.Is(err, membership.ErrRefillAlreadyRedeemed):
			fmt.Println("票券已经兑换，不能重复使用。")
		case errors.Is(err, membership.ErrRefillInvalidCoupon):
			fmt.Println("票券内容无效。")
		default:
			fmt.Println("票券兑换失败:", err)
		}
		waitForExit(reader, "按回车键退出...")
		os.Exit(1)
	}

	if coupon.RefillType == membership.QuotaRefillTypeEvent {
 fmt.Printf("已添加活动额度：%d 分钟，任务入口：%s（空表示不限任务）。\n", coupon.DurationSeconds/60, coupon.TaskEntry)
 } else { fmt.Println(refillLabel + "完成。") }
	fmt.Println("识别码:", result.CouponID)
	fmt.Println("额度文件:", result.Path)
	waitForExit(reader, "按回车键退出...")
}

func waitForConfirmation(reader *bufio.Reader, prompt string) bool {
	fmt.Print(prompt)
	_, err := reader.ReadString('\n')
	return err == nil
}

func waitForExit(reader *bufio.Reader, prompt string) {
	fmt.Print(prompt)
	_, _ = reader.ReadString('\n')
}
`))

func main() {
	outputDir := flag.String("out-dir", ".", "输出目录")
	keepTemp := flag.Bool("keep-temp", false, "保留临时票券源码目录")
	flag.Parse()

	input, err := readGeneratorInput(bufio.NewReader(os.Stdin), os.Stdout)
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(os.Stderr, "已取消生成。")
			return
		}
		fmt.Fprintln(os.Stderr, "读取生成参数失败:", err)
		os.Exit(1)
	}
	id, err := generateCouponID(rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成识别码失败:", err)
		os.Exit(1)
	}
	coupon := membership.QuotaRefillCoupon{
		ID:              id,
		IssuedOn:        time.Now().In(generatorBeijingLocation).Format("2006-01-02"),
		ValidDays:       input.ValidDays,
		RefillType:      input.RefillType,
		SponsorURL:      input.SponsorURL,
		DurationSeconds: input.DurationSeconds,
		TaskEntry:       input.TaskEntry,
	}

	validThrough, _ := membership.QuotaRefillValidThrough(coupon.IssuedOn, coupon.ValidDays)
	fmt.Println()
	fmt.Println("正在生成票券：")
	fmt.Println("  识别码:", coupon.ID)
	fmt.Println("  票券类型:", refillTypeLabel(coupon.RefillType))
	fmt.Printf("  兑换有效期: %s 至 %s（含）\n", coupon.IssuedOn, validThrough)
	fmt.Println("  适用范围:", scopeLabel(coupon.SponsorURL))
	if coupon.RefillType == membership.QuotaRefillTypeEvent {
		fmt.Printf("  活动额度: %d 分钟；任务入口: %s（空表示不限任务）\n", coupon.DurationSeconds/60, coupon.TaskEntry)
	}

	output, err := buildCoupon(coupon, *outputDir, *keepTemp)
	if err != nil {
		fmt.Fprintln(os.Stderr, "票券生成失败:", err)
		os.Exit(1)
	}
	fmt.Println("票券已生成:", output)
}

func readGeneratorInput(reader *bufio.Reader, writer io.Writer) (generatorInput, error) {
	fmt.Fprintln(writer, "MDA 票券生成器")

	refillType, err := promptRefillType(reader, writer)
	if err != nil {
		return generatorInput{}, err
	}
	var durationSeconds int64
	var taskEntry string
	if refillType == membership.QuotaRefillTypeEvent {
		for {
			value, err := promptLine(reader, writer, "活动额度时长（分钟，1~5256000）: ")
			if err != nil {
				return generatorInput{}, err
			}
			minutes, err := strconv.ParseInt(value, 10, 64)
			if err == nil && minutes > 0 && minutes <= 5256000 {
				durationSeconds = minutes * 60
				break
			}
			fmt.Fprintln(writer, "请输入有效的正整数分钟数。")
		}
		taskEntry, err = promptLine(reader, writer, "限定任务入口（如 MapPushingFlow，留空=不限任务）: ")
		if err != nil {
			return generatorInput{}, err
		}
	}
	validDays, err := promptValidDays(reader, writer)
	if err != nil {
		return generatorInput{}, err
	}
	sponsorURL, err := promptSponsorURL(reader, writer)
	if err != nil {
		return generatorInput{}, err
	}
	return generatorInput{
		SponsorURL:      sponsorURL,
		DurationSeconds: durationSeconds,
		TaskEntry:       taskEntry,
		ValidDays:       validDays,
		RefillType:      refillType,
	}, nil
}

func promptRefillType(reader *bufio.Reader, writer io.Writer) (membership.QuotaRefillType, error) {
	for {
		value, err := promptLine(reader, writer, "票券类型 [1=常规额度重置，2=专项额度重置，3=活动额度发放，默认 1]: ")
		if err != nil {
			return "", err
		}
		switch strings.ToLower(value) {
		case "", "1", "regular", "常规", "daily", "每日":
			return membership.QuotaRefillTypeDaily, nil
		case "2", "special", "专项", "monthly", "每月", "月度":
			return membership.QuotaRefillTypeMonthly, nil
		case "3", "event", "活动":
			return membership.QuotaRefillTypeEvent, nil
		default:
			fmt.Fprintln(writer, "请输入 1、2 或 3。")
		}
	}
}

func promptValidDays(reader *bufio.Reader, writer io.Writer) (int, error) {
	for {
		value, err := promptLine(reader, writer, "兑换有效天数 [默认 7]: ")
		if err != nil {
			return 0, err
		}
		if value == "" {
			return defaultValidDays, nil
		}
		days, err := strconv.Atoi(value)
		if err == nil && days >= 1 && days <= maxValidDays {
			return days, nil
		}
		fmt.Fprintf(writer, "请输入 1 到 %d 之间的整数。\n", maxValidDays)
	}
}

func promptSponsorURL(reader *bufio.Reader, writer io.Writer) (string, error) {
	for {
		value, err := promptLine(reader, writer, "绑定设备的赞助链接 [留空=全部设备]: ")
		if err != nil {
			return "", err
		}
		if value == "" {
			return "", nil
		}
		if _, err := membership.DeviceHashFromSponsorURL(value); err == nil {
			return value, nil
		} else {
			fmt.Fprintln(writer, "赞助链接无效:", err)
		}
	}
}

func promptLine(reader *bufio.Reader, writer io.Writer, prompt string) (string, error) {
	fmt.Fprint(writer, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if errors.Is(err, io.EOF) && line == "" {
		return "", io.EOF
	}
	return line, nil
}

func generateCouponID(reader io.Reader) (string, error) {
	data := make([]byte, couponIDBytes)
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func buildCoupon(coupon membership.QuotaRefillCoupon, outputDir string, keepTemp bool) (string, error) {
	validThrough, err := membership.QuotaRefillValidThrough(coupon.IssuedOn, coupon.ValidDays)
	if err != nil {
		return "", err
	}
	if coupon.SponsorURL != "" {
		if _, err := membership.DeviceHashFromSponsorURL(coupon.SponsorURL); err != nil {
			return "", fmt.Errorf("赞助链接无效: %w", err)
		}
	}
	if coupon.RefillType != membership.QuotaRefillTypeDaily && coupon.RefillType != membership.QuotaRefillTypeMonthly && coupon.RefillType != membership.QuotaRefillTypeEvent {
		return "", fmt.Errorf("未知票券类型 %q", coupon.RefillType)
	}
	if _, err := hex.DecodeString(coupon.ID); err != nil || len(coupon.ID) != couponIDBytes*2 {
		return "", errors.New("识别码必须是 32 位十六进制字符串")
	}

	if coupon.RefillType == membership.QuotaRefillTypeEvent && (coupon.DurationSeconds <= 0 || coupon.DurationSeconds > 315360000) {
		return "", errors.New("活动额度时长无效")
	}
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absOutputDir, 0755); err != nil {
		return "", err
	}
	output := filepath.Join(absOutputDir, couponFilename(coupon))
	tempDir, err := os.MkdirTemp("", "mda-quota-refill-*")
	if err != nil {
		return "", err
	}
	if !keepTemp {
		defer os.RemoveAll(tempDir)
	}

	mainPath := filepath.Join(tempDir, "main.go")
	file, err := os.Create(mainPath)
	if err != nil {
		return "", err
	}
	data := packageData{
		Coupon:       coupon,
		ValidThrough: validThrough,
		RefillLabel:  refillTypeLabel(coupon.RefillType),
		ScopeLabel:   scopeLabel(coupon.SponsorURL),
	}
	if err := refillMainTemplate.Execute(file, data); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}

	root := moduleRoot()
	if root == "" {
		return "", errors.New("未找到 MDA go-service 模块，请在项目源码目录中运行生成器")
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", output, mainPath)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	if keepTemp {
		fmt.Println("临时源码目录:", tempDir)
	}
	return output, nil
}

func couponFilename(coupon membership.QuotaRefillCoupon) string {
	validThrough, err := membership.QuotaRefillValidThrough(coupon.IssuedOn, coupon.ValidDays)
	if err != nil {
		validThrough = "未知日期"
	}
	if coupon.RefillType == membership.QuotaRefillTypeEvent {
		return fmt.Sprintf("MDA活动额度发放券_%d分钟_兑换截止%s_%s.exe", coupon.DurationSeconds/60, validThrough, shortCouponID(coupon.ID))
	}
	return fmt.Sprintf("MDA%s券_兑换截止%s_%s.exe", refillTypeLabel(coupon.RefillType), validThrough, shortCouponID(coupon.ID))
}

func refillTypeLabel(refillType membership.QuotaRefillType) string {
	if refillType == membership.QuotaRefillTypeEvent {
		return "活动额度发放"
	}
	if refillType == membership.QuotaRefillTypeMonthly {
		return "专项额度重置"
	}
	return "常规额度重置"
}

func scopeLabel(sponsorURL string) string {
	if strings.TrimSpace(sponsorURL) == "" {
		return "全部设备"
	}
	return "指定设备（按赞助链接绑定）"
}

func shortCouponID(id string) string {
	id = strings.ToUpper(strings.TrimSpace(id))
	const visibleLength = 6
	if len(id) > visibleLength {
		return id[:visibleLength]
	}
	return id
}

func moduleRoot() string {
	candidates := make([]string, 0, 3)
	dir, err := os.Getwd()
	if err == nil {
		candidates = append(candidates, dir)
	}
	if _, sourceFile, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Dir(sourceFile))
	}
	exe, err := os.Executable()
	if err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	for _, candidate := range candidates {
		if root := findModuleRoot(candidate); root != "" {
			return root
		}
	}
	return ""
}

func findModuleRoot(dir string) string {
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if isGoServiceModule(goModPath) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func isGoServiceModule(goModPath string) bool {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`) == goServiceModule
		}
	}
	return false
}
