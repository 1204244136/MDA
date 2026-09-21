package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1204244136/MDA/agent/go-service/taskersink/membership"
)

func generatorTestSponsorURL() string {
	return membership.SponsorURL(&membership.MembershipStatus{
		DeviceCode: membership.DeviceCodeV7{
			CPUHash:   "cpu",
			UUIDHash:  "uuid",
			BIOSHash:  "bios",
			BoardHash: "board",
			DiskHash:  "disk",
			GUIDHash:  "guid",
		},
	})
}

func TestReadGeneratorInputUsesDefaults(t *testing.T) {
	var output bytes.Buffer
	input, err := readGeneratorInput(bufio.NewReader(strings.NewReader("\n\n\n")), &output)
	if err != nil {
		t.Fatalf("readGeneratorInput() failed: %v", err)
	}
	if input.RefillType != membership.QuotaRefillTypeDaily {
		t.Fatalf("RefillType = %q, want daily", input.RefillType)
	}
	if input.ValidDays != defaultValidDays {
		t.Fatalf("ValidDays = %d, want %d", input.ValidDays, defaultValidDays)
	}
	if input.SponsorURL != "" {
		t.Fatalf("SponsorURL = %q, want empty", input.SponsorURL)
	}
	if !strings.Contains(output.String(), "留空=全部设备") {
		t.Fatal("prompt does not explain the empty sponsor URL default")
	}
}

func TestReadGeneratorInputRetriesInvalidValues(t *testing.T) {
	var output bytes.Buffer
	answers := strings.Join([]string{
		"invalid",
		"2",
		"0",
		"8",
		"not-a-sponsor-url",
		generatorTestSponsorURL(),
		"",
	}, "\n")
	input, err := readGeneratorInput(bufio.NewReader(strings.NewReader(answers)), &output)
	if err != nil {
		t.Fatalf("readGeneratorInput() failed: %v", err)
	}
	if input.RefillType != membership.QuotaRefillTypeMonthly {
		t.Fatalf("RefillType = %q, want monthly", input.RefillType)
	}
	if input.ValidDays != 8 {
		t.Fatalf("ValidDays = %d, want 8", input.ValidDays)
	}
	if input.SponsorURL != generatorTestSponsorURL() {
		t.Fatalf("SponsorURL = %q", input.SponsorURL)
	}
	if !strings.Contains(output.String(), "请输入 1、2 或 3") || !strings.Contains(output.String(), "赞助链接无效") {
		t.Fatal("invalid input feedback was not written")
	}
}

func TestReadGeneratorInputReturnsEOFWhenCancelled(t *testing.T) {
	_, err := readGeneratorInput(bufio.NewReader(strings.NewReader("")), io.Discard)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestGenerateCouponIDUses128Bits(t *testing.T) {
	data := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	id, err := generateCouponID(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("generateCouponID() failed: %v", err)
	}
	if id != "00112233445566778899aabbccddeeff" {
		t.Fatalf("ID = %s", id)
	}
}

func TestCouponFilenameContainsExpiryAndShortID(t *testing.T) {
	tests := []struct {
		name   string
		coupon membership.QuotaRefillCoupon
		want   string
	}{
		{
			name: "daily coupon for all members",
			coupon: membership.QuotaRefillCoupon{
				ID:         "00112233445566778899aabbccddeeff",
				IssuedOn:   "2026-07-17",
				ValidDays:  7,
				RefillType: membership.QuotaRefillTypeDaily,
			},
			want: "MDA常规额度重置券_兑换截止2026-07-23_001122.exe",
		},
		{
			name: "monthly coupon for one member",
			coupon: membership.QuotaRefillCoupon{
				ID:         "ffeeddccbbaa99887766554433221100",
				IssuedOn:   "2026-07-17",
				ValidDays:  30,
				RefillType: membership.QuotaRefillTypeMonthly,
				SponsorURL: generatorTestSponsorURL(),
			},
			want: "MDA专项额度重置券_兑换截止2026-08-15_FFEEDD.exe",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := couponFilename(tt.coupon); got != tt.want {
				t.Fatalf("couponFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGeneratedCouponPromptsBeforeRedeeming(t *testing.T) {
	coupon := membership.QuotaRefillCoupon{
		ID:         "00112233445566778899aabbccddeeff",
		IssuedOn:   "2026-07-17",
		ValidDays:  7,
		RefillType: membership.QuotaRefillTypeDaily,
		SponsorURL: generatorTestSponsorURL(),
	}
	var source bytes.Buffer
	err := refillMainTemplate.Execute(&source, packageData{
		Coupon:       coupon,
		ValidThrough: "2026-07-23",
		RefillLabel:  refillTypeLabel(coupon.RefillType),
		ScopeLabel:   scopeLabel(coupon.SponsorURL),
	})
	if err != nil {
		t.Fatalf("template Execute() failed: %v", err)
	}
	text := source.String()
	promptIndex := strings.Index(text, `if !waitForConfirmation(reader, "按回车键兑换...")`)
	redeemIndex := strings.Index(text, "membership.RedeemQuotaRefillCoupon(coupon)")
	if promptIndex < 0 || redeemIndex < 0 || promptIndex > redeemIndex {
		t.Fatal("generated coupon redeems before waiting for Enter")
	}
	if !strings.Contains(text, coupon.SponsorURL) {
		t.Fatal("generated coupon did not embed the sponsor URL")
	}
}

func TestBuildCouponCreatesExecutable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping nested Go build in short mode")
	}
	issuedOn := time.Now().In(generatorBeijingLocation).Format("2006-01-02")
	coupon := membership.QuotaRefillCoupon{
		ID:         "00112233445566778899aabbccddeeff",
		IssuedOn:   issuedOn,
		ValidDays:  7,
		RefillType: membership.QuotaRefillTypeDaily,
	}
	output, err := buildCoupon(coupon, t.TempDir(), false)
	if err != nil {
		t.Fatalf("buildCoupon() failed: %v", err)
	}
	if filepath.Base(output) != couponFilename(coupon) {
		t.Fatalf("output = %q", output)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatalf("Stat() failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("generated executable is empty")
	}

	configDir := t.TempDir()
	cmd := exec.Command(output)
	cmd.Stdin = strings.NewReader("")
	cmd.Env = append(os.Environ(), "APPDATA="+configDir, "XDG_CONFIG_HOME="+configDir)
	consoleOutput, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run generated executable with closed stdin: %v\n%s", err, consoleOutput)
	}
	if !strings.Contains(string(consoleOutput), "已取消兑换") {
		t.Fatalf("generated executable did not cancel on EOF:\n%s", consoleOutput)
	}
	statePath := filepath.Join(configDir, "MDA", "go-service", "membership-quota.json")
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quota state was created without Enter confirmation: %v", err)
	}
}

func TestFindModuleRootSkipsUnrelatedModule(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+goServiceModule+"\n"), 0644); err != nil {
		t.Fatalf("write root go.mod: %v", err)
	}
	nested := filepath.Join(root, "tools", "other-module", "child")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "tools", "other-module", "go.mod"), []byte("module example.com/other\n"), 0644); err != nil {
		t.Fatalf("write nested go.mod: %v", err)
	}
	if got := findModuleRoot(nested); got != root {
		t.Fatalf("findModuleRoot() = %q, want %q", got, root)
	}
}

func TestReadEventCouponInput(t *testing.T) {
	input, err := readGeneratorInput(bufio.NewReader(strings.NewReader("3\n0\n90\nMapPushingFlow\n7\n\n")), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if input.RefillType != membership.QuotaRefillTypeEvent || input.DurationSeconds != 5400 || input.TaskEntry != "MapPushingFlow" {
		t.Fatalf("wrong event input: %+v", input)
	}
	var source bytes.Buffer
	coupon := membership.QuotaRefillCoupon{RefillType: membership.QuotaRefillTypeEvent, DurationSeconds: 5400, TaskEntry: "MapPushingFlow"}
	if err := refillMainTemplate.Execute(&source, packageData{Coupon: coupon}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source.String(), "DurationSeconds: 5400") || !strings.Contains(source.String(), `TaskEntry: "MapPushingFlow"`) {
		t.Fatal("generated coupon lost event payload")
	}
}

func TestGeneratedEventCouponRedeemsOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("nested Go build")
	}
	coupon := membership.QuotaRefillCoupon{ID: "ffeeddccbbaa99887766554433221100", IssuedOn: time.Now().In(generatorBeijingLocation).Format("2006-01-02"), ValidDays: 7, RefillType: membership.QuotaRefillTypeEvent, DurationSeconds: 5400, TaskEntry: "MapPushingFlow"}
	output, err := buildCoupon(coupon, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	configDir := t.TempDir()
	run := func() ([]byte, error) {
		cmd := exec.Command(output)
		cmd.Stdin = strings.NewReader("\n\n")
		cmd.Env = append(os.Environ(), "APPDATA="+configDir, "XDG_CONFIG_HOME="+configDir)
		return cmd.CombinedOutput()
	}
	if console, err := run(); err != nil {
		t.Fatalf("redeem: %v\n%s", err, console)
	}
	statePath := filepath.Join(configDir, "MDA", "go-service", "membership-quota.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		EventGrants []struct {
			TaskEntry    string `json:"task_entry"`
			LimitSeconds int64  `json:"limit_seconds"`
		} `json:"event_grants"`
	}
	if err := json.Unmarshal(before, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.EventGrants) != 1 || state.EventGrants[0].TaskEntry != "MapPushingFlow" || state.EventGrants[0].LimitSeconds != 5400 {
		t.Fatalf("payload not redeemed: %s", before)
	}
	if console, err := run(); err == nil || !strings.Contains(string(console), "已经兑换") {
		t.Fatalf("duplicate allowed: %v\n%s", err, console)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("duplicate modified quota")
	}
}
