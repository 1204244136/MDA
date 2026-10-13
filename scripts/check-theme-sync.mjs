/**
 * check-theme-sync.mjs —— 活动主题路由一致性护栏
 *
 * 背景：`LargeEventTheme` / `SmallEventTheme` 的「当前活动」（CurrentEvent）不再携带
 * `pipeline_override`，而是靠 resource 侧 base 节点承载最新主题的模板；用户选中
 * CurrentEvent 时会自动跟随下次更新。
 *
 * 由此产生三类必须自动守住的不变量：
 *
 *   1. 模板类节点（override 内含 `template`）：所有「往期主题」case 必须**逐个覆盖**
 *      同一批节点。

 *      否则：base 换成新主题后，未覆盖的往期主题会静默继承新主题模板，导致回选
 *      老活动时识别失败（且不会报错，只是永远匹配不上）。
 *
 *   2. base 节点的动作参数（`action.param.target_offset` 点击偏移）与模板同理：
 *      base 一旦带上「当前活动」专属的非零偏移，所有「往期主题」case 必须**逐个显式声明**
 *      同一节点的 `target_offset`（不需要偏移就写 [0, 0, 0, 0]）。
 *
 *      否则：往期主题的点击会跟着当前活动一起偏移，落到目标之外（点偏、漏打）。
 *
 *      范围由 TARGETS 的 `offsetCarryingNodes` 显式登记 —— 只有「为补某个主题的
 *      识别框与点击区错位」的偏移才算主题专属；全主题通用的调参不登记，也不要求
 *      往期主题声明（否则会让老主题白白丢掉本来就该继承的偏移）。
 *
 *   3. CurrentEvent 必须**没有** `pipeline_override`（或为空对象）。
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
        /** base 节点所在 resource 文件（用于读取「当前活动」写在 base 上的模板与动作参数）。 */
        basePipelineFiles: [
            "assets/resource/pipeline/Event/SmallEvent/SmallEvent.json",
            "assets/resource/pipeline/Event/SmallEvent/SmallEventStory.json",
        ],
        expectedTemplateNodes: [
            "SmallEventEnterMainPage",
            "SmallEventClickStage",
            "SmallEventClickStageRepeatable",
        ],
        /**
         * base 上承载「当前活动专属」点击偏移的节点：往期主题必须逐个显式声明 target_offset，
         * 否则会静默继承当前活动的偏移而点偏目标。
         *
         * 注意区分「主题专属」与「全主题通用」：`SmallEventEnterMainPage` 的 [30, 0, 0, 0]
         * 是所有主题共用的入口调参（e003aa9），往期主题本就该继承，因此**不列在这里**；
         * 只有「偏移是为了补某个主题的识别框与点击区错位」时才登记。
         */
        offsetCarryingNodes: [
            "SmallEventClickStageRepeatable",
        ],
    },
    {
        file: "assets/tasks/LargeEvent.json",
        optionName: "LargeEventTheme",
        basePipelineFiles: [
            "assets/resource/pipeline/Event/LargeEvent/LargeEvent.json",
            "assets/resource/pipeline/Event/LargeEvent/LargeEventStory.json",
        ],
        expectedTemplateNodes: [
            "LargeEventEnterMainPage",
            "LargeEventClickStoryStage",
            "LargeEventClickStoryStageRepeatable",
        ],
        expectedRoutingNodes: [
            "LargeEventMiniGame",
        ],
        /** LargeEvent 暂无主题专属点击偏移。 */
        offsetCarryingNodes: [],
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

/** 汇总 base pipeline 中的节点定义：节点名 -> 节点 JSON。 */
function loadBaseNodes(target) {
    const nodes = new Map();
    for (const rel of target.basePipelineFiles ?? []) {
        try {
            const doc = parseJsonc(readFileSync(join(REPO_ROOT, rel), "utf8"));
            for (const [
                name,
                node,
            ] of Object.entries(doc)) {
                nodes.set(name, node);
            }
        } catch (err) {
            errors.push(`[${target.optionName}] 无法解析 base pipeline ${rel}：${err.message}`);
        }
    }
    return nodes;
}

/** 读取节点上的点击偏移（兼容 `action.param.target_offset` 与旧式顶层 `target_offset`）。 */
function targetOffsetOf(node) {
    if (!node || typeof node !== "object") return null;
    const nested = node?.action?.param?.target_offset;
    if (nested !== undefined) return nested;
    return node.target_offset === undefined ? null : node.target_offset;
}

/** 偏移是否为非默认值：任一分量不为 0 即算（兼容「每段一个偏移」的嵌套数组写法）。 */
function isNonZeroOffset(offset) {
    return Array.isArray(offset) && offset.flat(2).some((v) => typeof v === "number" && v !== 0);
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

    // ---- 断言 D：base 上带「当前活动专属」点击偏移的节点，往期主题必须显式声明 target_offset ----
    // 与断言 B 同源：base 承载的是「当前活动」的主题专属参数。除了 template，
    // 动作参数（action.param.target_offset）同样会被静默继承（框架按 prop 深合并 override）。
    const baseNodes = loadBaseNodes(target);
    for (const nodeName of target.offsetCarryingNodes ?? []) {
        const baseNode = baseNodes.get(nodeName);
        if (!baseNode) {
            errors.push(
                `[${target.optionName}] base pipeline 中找不到节点 "${nodeName}"，` +
                    `请检查 basePipelineFiles 配置与节点是否已改名。`,
            );
            continue;
        }
        const baseOffset = targetOffsetOf(baseNode);
        if (!isNonZeroOffset(baseOffset)) {
            errors.push(
                `[${target.optionName}] 节点 "${nodeName}" 被登记为「主题专属点击偏移」节点，` +
                    `但 base 上没有非零 target_offset。` +
                    `若新主题不再需要该偏移，请把此节点从脚本 TARGETS 的 offsetCarryingNodes 移除。`,
            );
            continue;
        }

        const declaredWith = new Map();
        const missing = [];
        for (const name of pastThemes) {
            const po = byName.get(name)?.pipeline_override ?? {};
            const value = targetOffsetOf(po[nodeName]);
            if (value === null) {
                missing.push(name);
                continue;
            }
            const key = JSON.stringify(value);
            declaredWith.set(key, [
                ...(declaredWith.get(key) ?? []),
                name,
            ]);
        }

        if (missing.length > 0) {
            errors.push(
                `[${target.optionName}] 节点 "${nodeName}" 的 base 带非零 target_offset ` +
                    `${JSON.stringify(baseOffset)}，但以下往期主题未显式声明 target_offset：` +
                    `${missing.join(", ")}。` +
                    `这些主题会静默继承当前活动的点击偏移而点偏目标，` +
                    `请在 case 里显式写 "target_offset": [0, 0, 0, 0]。`,
            );
        } else {
            const detail = [...declaredWith]
                .map(([value, names]) => `${names.join("/")} = ${value}`)
                .join("；");
            notes.push(
                `[${target.optionName}] 节点 "${nodeName}"：base = ${JSON.stringify(baseOffset)}；` +
                    `往期主题均显式声明 —— ${detail}。`,
            );
        }
    }

    // ---- 断言 C：路由类节点覆盖校验（如 LargeEventMiniGame）----
    if (target.expectedRoutingNodes && target.expectedRoutingNodes.length > 0) {
        // 检查范围包括 pastThemes 以及 Other（除了 CurrentEvent 之外的全部 case）
        const casesToCheck = cases.filter((c) => c.name !== "CurrentEvent").map((c) => c.name);
        for (const nodeName of target.expectedRoutingNodes) {
            const missing = casesToCheck.filter((name) => {
                const po = byName.get(name)?.pipeline_override ?? {};
                const node = po[nodeName];
                return !node || !Array.isArray(node.next) || node.next.length === 0;
            });
            if (missing.length > 0) {
                errors.push(
                    `[${target.optionName}] 节点 "${nodeName}" 未被以下主题 case 显式覆盖 next：` +
                        `${missing.join(", ")}。` +
                        `base 承载最新主题小游戏后，未覆盖的主题会静默继承导致误跑当前活动小游戏。`,
                );
            }
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
