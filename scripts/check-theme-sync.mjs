/**
 * check-theme-sync.mjs —— 活动主题路由一致性护栏
 *
 * 背景：`LargeEventTheme` / `SmallEventTheme` 的「当前活动」（CurrentEvent）不再携带
 * `pipeline_override`，而是靠 resource 侧 base 节点承载最新主题的模板；用户选中
 * CurrentEvent 时会自动跟随下次更新。
 *
 * 由此产生两类必须自动守住的不变量：
 *
 *   1. 模板类节点（override 内含 `template`）：所有「往期主题」case 必须**逐个覆盖**
 *      同一批节点。

 *      否则：base 换成新主题后，未覆盖的往期主题会静默继承新主题模板，导致回选
 *      老活动时识别失败（且不会报错，只是永远匹配不上）。
 *
 *   2. CurrentEvent 必须**没有** `pipeline_override`（或为空对象）。
 *      否则：又回到"写两遍"，且 override 会盖住 base，自动跟随失效。
 *
 *      例外：`option`（子选项列表）不算 override，必须保留。
 *
 * 用法：
 *   node scripts/check-theme-sync.mjs
 *   npm run check:theme
 *
 * 退出码：0 = 全部通过；1 = 存在违规。
 */

import {readFileSync} from "node:fs";
import {fileURLToPath} from "node:url";
import {dirname, join, resolve} from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(HERE, "..");

/** 定义「主题 option」：tasks 文件 + option 名 + 必须全量覆盖的模板类节点。 */
const TARGETS = [
    {
        file: "assets/tasks/SmallEvent.json",
        optionName: "SmallEventTheme",
        expectedTemplateNodes: [
            "SmallEventEnterMainPage",
            "SmallEventClickStage",
            "SmallEventClickStageRepeatable",
        ],
    },
    {
        file: "assets/tasks/LargeEvent.json",
        optionName: "LargeEventTheme",
        expectedTemplateNodes: [
            "LargeEventEnterMainPage",
            "LargeEventClickStoryStage",
            "LargeEventClickStoryStageRepeatable",
        ],
    },
];

/** 不属于「主题 case」的保留名：不参与覆盖完整性校验。 */
const NON_THEME_CASES = new Set([
    "CurrentEvent",
    "Other",
]);

/**
 * 从一段含注释（`// ...`）的 JSON 文本中解析出对象。
 * MDA 的 pipeline / tasks 允许行注释，标准 JSON.parse 会拒绝。
 */
function parseJsonc(text) {
    // 逐字符扫描去掉 // 注释，同时跳过字符串字面量内部的 //。
    let out = "";
    let inString = false;
    let escaped = false;
    for (let i = 0; i < text.length; i++) {
        const ch = text[i];
        if (inString) {
            out += ch;
            if (escaped) escaped = false;
            else if (ch === "\\") escaped = true;
            else if (ch === '"') inString = false;
            continue;
        }
        if (ch === '"') {
            inString = true;
            out += ch;
            continue;
        }
        if (ch === "/" && text[i + 1] === "/") {
            while (i < text.length && text[i] !== "\n") i++;
            if (i < text.length) out += "\n";
            continue;
        }
        if (ch === "/" && text[i + 1] === "*") {
            i += 2;
            while (i < text.length && !(text[i] === "*" && text[i + 1] === "/")) i++;
            i++;
            continue;
        }
        out += ch;
    }
    return JSON.parse(out);
}

/** 判断一个 override 片段是否「承载 template」。 */
function carriesTemplate(overrideValue) {
    if (!overrideValue || typeof overrideValue !== "object") return false;
    const param = overrideValue?.recognition?.param;
    return Boolean(param && Object.prototype.hasOwnProperty.call(param, "template"));
}

const errors = [];
const notes = [];

for (const target of TARGETS) {
    const abs = join(REPO_ROOT, target.file);
    let doc;
    try {
        doc = parseJsonc(readFileSync(abs, "utf8"));
    } catch (err) {
        errors.push(`[${target.optionName}] 无法解析 ${target.file}：${err.message}`);
        continue;
    }

    const themeOption = doc?.option?.[target.optionName];
    if (!themeOption) {
        errors.push(`[${target.optionName}] ${target.file} 中找不到 option.${target.optionName}`);
        continue;
    }

    const cases = themeOption.cases ?? [];
    const byName = new Map(
        cases.map((c) => [
            c.name,
            c,
        ]),
    );

    // ---- 断言 A：CurrentEvent 不得携带 pipeline_override ----
    const current = byName.get("CurrentEvent");
    if (!current) {
        errors.push(`[${target.optionName}] 缺少 CurrentEvent case（首个 case 且应为 default_case）。`);
    } else {
        if (themeOption.default_case !== "CurrentEvent") {
            errors.push(
                `[${target.optionName}] default_case 应为 "CurrentEvent"，实际为 ${JSON.stringify(
                    themeOption.default_case,
                )}。`,
            );
        }
        const override = current.pipeline_override;
        if (override && Object.keys(override).length > 0) {
            errors.push(
                `[${target.optionName}] CurrentEvent 不应携带 pipeline_override，` +
                    `实际覆盖了 ${Object.keys(override).join(", ")}。` +
                    `请把模板沉淀到 resource 侧 base 节点，让 CurrentEvent 靠 base 自动跟随。`,
            );
        } else {
            notes.push(`[${target.optionName}] CurrentEvent 无 override —— 自动跟随已生效。`);
        }
    }

    // ---- 断言 B：所有往期主题必须逐个覆盖同一批模板类节点 ----
    const pastThemes = cases.filter((c) => !NON_THEME_CASES.has(c.name)).map((c) => c.name);

    if (pastThemes.length === 0) {
        notes.push(`[${target.optionName}] 暂无往期主题 case，跳过覆盖完整性校验。`);
        continue;
    }

    for (const nodeName of target.expectedTemplateNodes) {
        const missing = pastThemes.filter((name) => {
            const po = byName.get(name)?.pipeline_override ?? {};
            return !carriesTemplate(po[nodeName]);
        });
        if (missing.length > 0) {
            errors.push(
                `[${target.optionName}] 节点 "${nodeName}" 未被以下往期主题覆盖 template：` +
                    `${missing.join(", ")}。` +
                    `base 承载最新主题后，这些主题会静默继承新模板导致识别失败。`,
            );
        }
    }

    // ---- 记录：被显式覆盖但不在模板清单内的节点（属颜色/阈值特例，仅提示） ----
    const templateNodesSet = new Set(target.expectedTemplateNodes);
    const extraCovered = new Set();
    for (const name of pastThemes) {
        const po = byName.get(name)?.pipeline_override ?? {};
        for (const [
            nodeName,
            value,
        ] of Object.entries(po)) {
            if (!templateNodesSet.has(nodeName) && carriesTemplate(value)) {
                extraCovered.add(nodeName);
            }
        }
    }
    if (extraCovered.size > 0) {
        notes.push(
            `[${target.optionName}] 以下节点承载 template 但未纳入清单，请确认是否为有意为之：` +
                `${[...extraCovered].join(", ")}。`,
        );
    }
}

// ---- 输出 ----
const label = (s) => `\u001b[${s}m`;
for (const n of notes) console.log(`${label(36)}note${label(0)}  ${n}`);
for (const e of errors) console.error(`${label(31)}error${label(0)} ${e}`);

if (errors.length > 0) {
    console.error(`\n${label(31)}主题路由一致性校验失败：${errors.length} 项违规。${label(0)}`);
    process.exit(1);
}
console.log(`\n${label(32)}主题路由一致性校验通过：${TARGETS.length} 个 option。${label(0)}`);
