package membership

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	// 任务描述里必须出现的 5 倍额度标记（zh_cn / en_us 各一份）。
	// 用「按 5 倍额度消耗」这种自然措辞当标记，是为了允许描述里其他文字自由调整，
	// 只要求额度说明本身在位；改口径时（例如不再叫 5 倍）需同步这里与三条任务描述。
	zhQuotaNoteMarker = "按 5 倍额度消耗"
	enQuotaNoteMarker = "consumes runtime quota at 5x"

	// entry → 任务定义 的资产根目录（相对本包目录 taskersink/membership）。
	assetsDirFromMembership = "../../../../assets"
)

// 分级表把「高级任务」的判定放在 entry 上，而用户只能看到任务描述，
// 因此描述必须写明 5 倍额度消耗：本测试把两侧强绑定，漏写即失败。
func TestHighConsumptionTaskDescriptionsNoteBilling(t *testing.T) {
	highEntries := HighConsumptionEntries()
	entriesByTask := highConsumptionTasksByEntry(t)

	for _, entry := range highEntries {
		task, ok := entriesByTask[entry]
		if !ok {
			t.Fatalf("taskTierByEntry 标记了高级任务 entry %q，但 assets/tasks 下没有任务使用该 entry", entry)
		}
		for _, lang := range []string{"zh_cn", "en_us"} {
			note := quotaNoteMarker(lang)
			description, ok := taskDescription(t, lang, task)
			if !ok {
				t.Fatalf("task.%s.description 在 %s 中缺失", task, lang)
			}
			if !strings.Contains(description, note) {
				t.Fatalf("高级任务 %s（entry %s）的 %s 描述缺少额度说明 %q，请在任务 description 中写明 5 倍额度消耗", task, entry, lang, note)
			}
		}
	}

	for entry, task := range entriesByTask {
		if slices.Contains(highEntries, entry) {
			continue
		}
		for _, lang := range []string{"zh_cn", "en_us"} {
			description, ok := taskDescription(t, lang, task)
			if !ok {
				continue
			}
			if strings.Contains(description, quotaNoteMarker(lang)) {
				t.Fatalf("任务 %s（entry %s）不是高级任务，但 %s 描述写了 5 倍额度说明，请同步 taskTierByEntry", task, entry, lang)
			}
		}
	}
}

func quotaNoteMarker(lang string) string {
	if lang == "en_us" {
		return enQuotaNoteMarker
	}
	return zhQuotaNoteMarker
}

// highConsumptionTasksByEntry 扫描 assets/tasks/**.json，返回 entry → 任务名 的映射。
func highConsumptionTasksByEntry(t *testing.T) map[string]string {
	t.Helper()

	dir := filepath.Join(assetsDirFromMembership, "tasks")
	taskFiles, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob %s failed: %v", dir, err)
	}
	if len(taskFiles) == 0 {
		t.Fatalf("no task json found under %s", dir)
	}

	entries := make(map[string]string)
	for _, path := range taskFiles {
		var interfaceFile struct {
			Task []struct {
				Name  string `json:"name"`
				Entry string `json:"entry"`
			} `json:"task"`
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s failed: %v", path, err)
		}
		if err := json.Unmarshal(data, &interfaceFile); err != nil {
			t.Fatalf("parse %s failed: %v", path, err)
		}
		for _, task := range interfaceFile.Task {
			if task.Entry == "" {
				continue
			}
			entries[task.Entry] = task.Name
		}
	}
	return entries
}

// taskDescription 读取界面语言文件里的任务描述（$task.<name>.description 的引用目标）。
func taskDescription(t *testing.T, lang, task string) (string, bool) {
	t.Helper()

	path := filepath.Join(assetsDirFromMembership, "locales", "interface", lang+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s failed: %v", path, err)
	}
	var messages map[string]string
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatalf("parse %s failed: %v", path, err)
	}

	description, ok := messages["task."+task+".description"]
	return description, ok
}
