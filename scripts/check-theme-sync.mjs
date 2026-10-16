/**
 * 活动主题一致性护栏：CurrentEvent 无 override；具体主题完整覆盖模板或路由。
 * 小活动扫荡节点按主题保存模板与动作，禁止共用节点继承主题专属偏移。
 * 用法：npm run check:theme；回归测试：npm run test:theme。
 */

import {readFileSync, readdirSync} from "node:fs";
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
        ],
        repeatableRoutingNode: "SmallEventEventStageQuickBattleFlow",
        themePipelineDir: "assets/resource/pipeline/Event/SmallEvent/Themes",
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

/** 检查路由的实际目标及模板归属，避免换主题时遗留旧路由或把不同难度的偏移绑在一起。 */
function checkRepeatableRoutes(target, cases, baseNodes) {
    const routeName = target.repeatableRoutingNode;
    const fail = (message) => errors.push(`[${target.optionName}] ${message}`);
    const themeNodes = new Map();
    try {
        for (const file of readdirSync(join(REPO_ROOT, target.themePipelineDir))) {
            if (!file.endsWith(".json")) continue;
            const theme = file.slice(0, -5);
            const doc = parseJsonc(readFileSync(join(REPO_ROOT, target.themePipelineDir, file), "utf8"));
            for (const [
                name,
                node,
            ] of Object.entries(doc)) {
                if (themeNodes.has(name) || baseNodes.has(name)) fail(`扫荡节点 ${name} 重复声明。`);
                if (!name.startsWith(`SmallEvent${theme}ClickStage`)) fail(`${file} 中的 ${name} 不属于该主题。`);
                themeNodes.set(name, {theme, node});
            }
        }
    } catch (err) {
        fail(`无法读取主题扫荡节点：${err.message}`);
        return;
    }

    const concrete = cases.filter((c) => !NON_THEME_CASES.has(c.name));
    const baseLogo = baseNodes.get("SmallEventEnterMainPage")?.recognition?.param?.template;
    const currentTheme = concrete.find(
        (c) =>
            JSON.stringify(c.pipeline_override?.SmallEventEnterMainPage?.recognition?.param?.template) ===
            JSON.stringify(baseLogo),
    );
    if (!currentTheme) fail("base 入口模板找不到对应的具体主题 case，无法确认当前扫荡路由归属。");

    const used = new Set();
    function checkRoute(route, themeCase, label) {
        if (!route || route.action || route.recognition || route.target_offset) {
            fail(`${label} 的 ${routeName} 必须是无识别、无点击动作的路由节点。`);
        }
        const next = route?.next;
        if (
            !Array.isArray(next) ||
            next.length < 2 ||
            next.at(-1) !== "SmallEventGoBack" ||
            next.some((name) => typeof name !== "string") ||
            new Set(next).size !== next.length
        ) {
            fail(`${label} 必须完整声明扫荡 next：主题点击节点 + SmallEventGoBack。`);
            return;
        }
        const logo = themeCase.pipeline_override?.SmallEventEnterMainPage?.recognition?.param?.template?.[0];
        const folder = typeof logo === "string" ? logo.slice(0, logo.lastIndexOf("/") + 1) : "";
        for (const name of next.slice(0, -1)) {
            const item = themeNodes.get(name);
            if (!item || item.theme !== themeCase.name) {
                fail(`${label} 的扫荡路由 ${name} 缺失或引用了其他主题。`);
                continue;
            }
            used.add(name);
            const node = item.node;
            const templates = node.recognition?.param?.template;
            if (
                node.recognition?.type !== "TemplateMatch" ||
                !folder ||
                !Array.isArray(templates) ||
                templates.length === 0 ||
                templates.some((tpl) => typeof tpl !== "string" || !tpl.startsWith(folder) || tpl.includes(".."))
            ) {
                fail(`${name} 必须使用 ${themeCase.name} 自己的模板。`);
            }
            if (
                node.action?.type !== "Click" ||
                node.enabled === false ||
                node.action.param?.target === false ||
                JSON.stringify(node.next) !==
                    JSON.stringify([
                        "SmallEventEventStageBattlePage",
                        "SmallEventGoBack",
                    ])
            ) {
                fail(`${name} 必须执行点击并接回共用战斗页面/返回流程。`);
            }
            const offset = node.action?.param?.target_offset ?? [
                0,
                0,
                0,
                0,
            ];
            if (!Array.isArray(offset) || offset.length !== 4 || offset.some((v) => !Number.isInteger(v))) {
                fail(`${name} 的 target_offset 必须是四个整数。`);
            } else if (
                offset.some((v) => v !== 0) &&
                Array.isArray(templates) &&
                templates.some((tpl) => /StageNormal/.test(tpl)) &&
                templates.some((tpl) => /StageHard/.test(tpl))
            ) {
                fail(`${name} 的 Normal/Hard 模板携带非零偏移，应按点击策略拆分节点。`);
            }
        }
    }

    for (const themeCase of concrete) {
        const overrides = themeCase.pipeline_override ?? {};
        checkRoute(overrides[routeName], themeCase, themeCase.name);
        for (const name of Object.keys(overrides)) {
            if (themeNodes.has(name) || name === "SmallEventClickStageRepeatable") {
                fail(`${themeCase.name} 不应覆盖扫荡节点 ${name} 的模板/动作，请只切换路由。`);
            }
        }
    }
    if (currentTheme) {
        checkRoute(baseNodes.get(routeName), currentTheme, "CurrentEvent/base");
        if (
            JSON.stringify(baseNodes.get(routeName)?.next) !==
            JSON.stringify(currentTheme.pipeline_override?.[routeName]?.next)
        ) {
            fail("CurrentEvent/base 扫荡路由必须与当前具体主题一致，不能遗漏难度分支。");
        }
        for (const name of target.expectedTemplateNodes) {
            if (
                JSON.stringify(baseNodes.get(name)?.recognition?.param?.template) !==
                JSON.stringify(currentTheme.pipeline_override?.[name]?.recognition?.param?.template)
            ) {
                fail(`CurrentEvent/base 的 ${name} 模板必须与当前具体主题一致。`);
            }
        }
    }
    const other = cases.find((c) => c.name === "Other");
    if (other?.pipeline_override && Object.keys(other.pipeline_override).length > 0) {
        fail("Other 应保持无 override，兼容既有跟随当前活动的行为。");
    }
    if (baseNodes.has("SmallEventClickStageRepeatable")) fail("共用 SmallEventClickStageRepeatable 应迁移到主题节点。");
    for (const name of themeNodes.keys()) {
        if (!used.has(name)) fail(`主题扫荡节点 ${name} 未被自己的主题路由引用。`);
    }
    notes.push(`[${target.optionName}] ${concrete.length} 个主题的扫荡模板与动作按主题隔离。`);
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

    // ---- 断言 D：小活动扫荡按主题路由，模板与动作属于同一个主题节点 ----
    if (target.repeatableRoutingNode) {
        checkRepeatableRoutes(target, cases, loadBaseNodes(target));
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
