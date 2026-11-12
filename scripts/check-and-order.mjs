/**
 * And 取框顺序护栏：
 * `all_of` 同时承担两个职责——执行顺序（串行、首个未命中即中断）与取框来源（box_index）。
 * 当某个子识别用「节点名 roi」引用了同一 all_of 里的兄弟节点，它就只能在那个兄弟之后执行；
 * 若它被排到前面，本轮拿不到 ROI，整个 And 必然失败（897db5b 事故：SoloRaid 关卡选择）。
 *
 * 检出不变量：
 * 1. all_of 内不许出现后向依赖——被引用的兄弟必须排在引用者之前；
 * 2. box_index 必须落在 all_of 范围内。
 *
 * 跨节点引用（引用不在同一 all_of 的节点）不在此校验范围：它依赖 runtime cache，
 * 是否成立取决于上游是否跑过，静态无法判定。
 *
 * 用法：npm run check:and；也可只检查指定文件：node scripts/check-and-order.mjs <file...>
 */

import {readFileSync, readdirSync, existsSync} from "node:fs";
import {fileURLToPath} from "node:url";
import {dirname, join, resolve, relative, basename} from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(HERE, "..");
const PIPELINE_DIR = join(REPO_ROOT, "assets", "resource", "pipeline");

/** 剥掉 JSONC 注释，保留偏移与换行，便于 JSON.parse。 */
function stripComments(text) {
    let out = "";
    let inString = false;
    let escaped = false;
    for (let i = 0; i < text.length; i += 1) {
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
            while (i < text.length && text[i] !== "\n") {
                out += " ";
                i += 1;
            }
            if (i < text.length) out += "\n";
            continue;
        }
        if (ch === "/" && text[i + 1] === "*") {
            out += "  ";
            i += 2;
            while (i < text.length && !(text[i] === "*" && text[i + 1] === "/")) {
                out += text[i] === "\n" ? "\n" : " ";
                i += 1;
            }
            out += "  ";
            i += 1;
            continue;
        }
        out += ch;
    }
    return out;
}

/** 收集所有 pipeline JSON 文件（可被命令行参数覆盖）。 */
function collectFiles(argv) {
    const explicit = argv.filter((a) => !a.startsWith("--"));
    if (explicit.length > 0) return explicit.map((p) => resolve(REPO_ROOT, p));
    const files = [];
    const walk = (dir) => {
        for (const entry of readdirSync(dir, {withFileTypes: true})) {
            const full = join(dir, entry.name);
            if (entry.isDirectory()) walk(full);
            else if (entry.name.endsWith(".json")) files.push(full);
        }
    };
    if (existsSync(PIPELINE_DIR)) walk(PIPELINE_DIR);
    return files;
}

/** 从 recognition.param.roi 里取出被引用的节点名；非节点引用返回 null。 */
function roiNodeRef(node) {
    const roi = node?.recognition?.param?.roi;
    if (typeof roi === "string") return roi;
    if (roi && typeof roi === "object" && !Array.isArray(roi)) {
        const param = roi.param;
        if (typeof param === "string") return param;
        if (Array.isArray(param) && typeof param[0] === "string") return param[0];
    }
    return null;
}

/**
 * 校验一批文件，返回 {errors, warnings, scanned, andCount}。
 * 供命令行与测试复用。
 */
export function checkFiles(files) {
    const errors = [];
    const warnings = [];
    const nodes = new Map();
    const parsed = [];

    for (const file of files) {
        const relPath = relative(REPO_ROOT, file).replaceAll("\\", "/");
        const rel = relPath.startsWith("..") ? basename(file) : relPath;
        let data;
        try {
            data = JSON.parse(stripComments(readFileSync(file, "utf8")));
        } catch (err) {
            errors.push(`${rel}：JSONC 解析失败（${err.message}）`);
            continue;
        }
        if (!data || typeof data !== "object" || Array.isArray(data)) continue;
        parsed.push({rel, data});
        for (const [
            name,
            node,
        ] of Object.entries(data)) {
            if (!node || typeof node !== "object" || Array.isArray(node)) continue;
            nodes.set(name, node);
        }
    }

    let andCount = 0;
    for (const {rel, data} of parsed) {
        for (const [
            name,
            node,
        ] of Object.entries(data)) {
            if (!node || typeof node !== "object" || Array.isArray(node)) continue;
            const recognition = node.recognition;
            if (!recognition || recognition.type !== "And") continue;
            andCount += 1;

            const param = recognition.param ?? {};
            const allOf = Array.isArray(param.all_of) ? param.all_of : [];
            const names = allOf.filter((item) => typeof item === "string");

            for (const [
                i,
                sub,
            ] of names.entries()) {
                const dep = roiNodeRef(nodes.get(sub));
                if (!dep) continue;
                const j = names.indexOf(dep);
                if (j < 0) continue; // 跨节点引用，静态无法判定
                if (j > i) {
                    errors.push(
                        `${rel} ${name}：all_of[${i}]「${sub}」的 roi 引用了「${dep}」，` +
                            `但后者排在 all_of[${j}]，执行顺序反了——本轮拿不到 ROI，And 必然失败。` +
                            `改法：把「${dep}」排到前面，并用 box_index 指向「${sub}」的序号`,
                    );
                }
            }

            if ("box_index" in param) {
                const boxIndex = param.box_index;
                if (!Number.isInteger(boxIndex) || boxIndex < 0 || boxIndex >= names.length) {
                    errors.push(`${rel} ${name}：box_index=${boxIndex} 越界（all_of 有 ${names.length} 项）`);
                }
            }
        }
    }

    if (andCount === 0 && files.length > 0) {
        warnings.push("没有扫描到任何 And 节点，检查路径是否正确");
    }

    return {errors, warnings, scanned: files.length, andCount};
}

const label = (s) => `\u001b[${s}m`;

function main() {
    const result = checkFiles(collectFiles(process.argv.slice(2)));
    for (const w of result.warnings) console.warn(`${label(33)}warn${label(0)}  ${w}`);
    for (const e of result.errors) console.error(`${label(31)}error${label(0)} ${e}`);

    if (result.errors.length > 0) {
        console.error(
            `\n${label(31)}And 取框顺序校验失败：${result.errors.length} 项错误（` +
                `扫描 ${result.scanned} 个文件、${result.andCount} 个 And 节点）。${label(0)}`,
        );
        process.exit(1);
    }
    console.log(
        `\n${label(32)}And 取框顺序校验通过：${result.scanned} 个文件、` +
            `${result.andCount} 个 And 节点，无后向依赖。${label(0)}`,
    );
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
