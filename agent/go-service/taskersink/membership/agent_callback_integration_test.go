package membership

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/1204244136/MDA/agent/go-service/taskersink/aspectratio"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestAgentQuotaCallbacks 使用真实 Agent IPC 和空白控制器，不联网、不连接手机。
// 设置 MDA_MAA_LIB 为匹配版本的框架库目录后运行。
func TestAgentQuotaCallbacks(t *testing.T) {
	lib := os.Getenv("MDA_MAA_LIB")
	if lib == "" {
		t.Skip("set MDA_MAA_LIB to run real Agent callback/quota integration")
	}
	statePath := isolateQuotaState(t)
	work := t.TempDir()
	if err := maa.Init(maa.WithLibDir(lib), maa.WithJSONEncoder(json.Marshal), maa.WithJSONDecoder(json.Unmarshal)); err != nil {
		t.Fatal(err)
	}
	defer maa.Release()
	if err := maa.ConfigInitOption(work, "{}"); err != nil {
		t.Fatal(err)
	}
	ctrl, err := maa.NewBlankController()
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Destroy()
	if !ctrl.PostConnect().Wait().Success() {
		t.Fatal("blank controller connection failed")
	}
	res, err := maa.NewResource()
	if err != nil {
		t.Fatal(err)
	}
	defer res.Destroy()
	if err := os.MkdirAll(filepath.Join(work, "pipeline"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "pipeline", "smoke.json"), []byte(`{"AgentQuotaSmoke":{"action":"DoNothing","post_delay":1500}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !res.PostBundle(work).Wait().Success() {
		t.Fatal("smoke resource load failed")
	}
	tasker, err := maa.NewTasker()
	if err != nil {
		t.Fatal(err)
	}
	defer tasker.Destroy()
	if err := tasker.BindController(ctrl); err != nil {
		t.Fatal(err)
	}
	if err := tasker.BindResource(res); err != nil {
		t.Fatal(err)
	}
	client, err := maa.NewAgentClient()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Destroy()
	if err := client.BindResource(res); err != nil {
		t.Fatal(err)
	}
	if err := client.RegisterTaskerSink(*tasker); err != nil {
		t.Fatal(err)
	}
	if err := client.SetTimeout(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	identifier, err := client.Identifier()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentQuotaCallbackHelper$")
	ready := filepath.Join(work, "agent-ready")
	cmd.Env = append(os.Environ(), "MDA_AGENT_QUOTA_HELPER=1", "MDA_AGENT_IDENTIFIER="+identifier,
		"MDA_AGENT_READY="+ready, "MDA_AGENT_WORK="+filepath.Join(work, "agent"),
		`PI_CONTROLLER={"name":"ADB","type":"Adb"}`)
	output, err := os.Create(filepath.Join(work, "agent-output.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Agent helper did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer client.Disconnect()
	if !tasker.PostTask("AgentQuotaSmoke").Wait().Success() {
		t.Fatal("Agent smoke task failed")
	}
	// 事件发送为异步，允许 Agent 完成最终结算；不依赖精确的秒边界。
	deadline = time.Now().Add(3 * time.Second)
	for {
		state, err := loadQuotaState(statePath)
		if err == nil && state.Pools[string(quotaPoolRegularDaily)].UsedSeconds > 0 {
			break
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(output.Name())
			t.Fatalf("Agent callback did not charge quota: %v\n%s", err, data)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAgentQuotaCallbackHelper(t *testing.T) {
	if os.Getenv("MDA_AGENT_QUOTA_HELPER") != "1" {
		return
	}
	if err := maa.Init(maa.WithLibDir(os.Getenv("MDA_MAA_LIB")), maa.WithJSONEncoder(json.Marshal), maa.WithJSONDecoder(json.Unmarshal)); err != nil {
		t.Fatal(err)
	}
	defer maa.Release()
	work := os.Getenv("MDA_AGENT_WORK")
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	if err := maa.ConfigInitOption(work, "{}"); err != nil {
		t.Fatal(err)
	}
	SetVersion("2.5.0")
	cacheStatus(testStatus(10, "agent-ipc-fixture"))
	// 顺序与真实 Agent 一致：分辨率检查先进行嵌套反向调用，再分发会员计时。
	aspectratio.Register()
	Register()
	if err := maa.AgentServerStartUp(os.Getenv("MDA_AGENT_IDENTIFIER")); err != nil {
		t.Fatal(err)
	}
	defer maa.AgentServerShutDown()
	if err := os.WriteFile(os.Getenv("MDA_AGENT_READY"), []byte("ready"), 0644); err != nil {
		t.Fatal(err)
	}
	maa.AgentServerJoin()
}
