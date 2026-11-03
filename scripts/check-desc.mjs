/**
 * Pipeline desc 与注释护栏：
 * - desc 只写一句话名片，解释性内容归 // 注释；
 * - // 注释必须写在被说明那一行的行尾，不允许独立成行。
 * 规则见 docs/pipeline-desc-and-comments.md。
 * 用法：npm run check:desc；也可只检查指定文件：node scripts/check-desc.mjs <file...>。
 */

import {readFileSync, readdirSync, existsSync} from "node:fs";
import {fileURLToPath} from "node:url";
import {dirname, join, resolve, relative} from "node:path";

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(HERE, "..");
const PIPELINE_DIR = join(REPO_ROOT, "assets", "resource", "pipeline");
const ALLOWLIST_FILE = join(REPO_ROOT, "docs", "pipeline-desc-allowlist.json");

/** desc 长度上限（按字符计，中文一个字算一个）。 */
const MAX_LEN = 24;

/** desc 中不应出现的实现细节词。命中只提示，不阻断，除非 --strict。 */
const IMPL_PATTERN =
    /ROI|roi|阈值|坐标|像素|模板|偏移|attach|pipeline_override|override|Custom|OCR|TemplateMatch|ColorMatch|\bGo\b|\bAgent\b/;

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

/** 读取白名单：{"allow": ["assets/.../X.json::NodeName"]}。 */
function loadAllowlist() {
    if (!existsSync(ALLOWLIST_FILE)) return new Set();
    const data = JSON.parse(readFileSync(ALLOWLIST_FILE, "utf8"));
    return new Set(Array.isArray(data.allow) ? data.allow : []);
}

/** 找到节点 key 在原文中的行号。 */
function lineOfNode(raw, nodeName) {
    const re = new RegExp(`^\\s{4}"${nodeName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}"\\s*:`, "m");
    const before = raw.search(re);
    if (before < 0) return 0;
    return raw.slice(0, before).split("\n").length;
}

const argv = process.argv.slice(2);
const strict = argv.includes("--strict");
const allowlist = loadAllowlist();
const files = collectFiles(argv);

const errors = [];
const warnings = [];
const notes = [];

for (const file of files) {
    const rel = relative(REPO_ROOT, file).replaceAll("\\", "/");
    const raw = readFileSync(file, "utf8");
    let data;
    try {
        data = JSON.parse(stripComments(raw));
    } catch (err) {
        errors.push(`${rel}：JSONC 解析失败（${err.message}）`);
        continue;
    }
    if (!data || typeof data !== "object") continue;

    // 注释必须写在行尾：独立成行的 // 视为违规（规范 C1）
    raw.split("\n").forEach((line, idx) => {
        if (line.trim().startsWith("//")) {
            errors.push(`${rel}:${idx + 1}：独立行注释，请改成被说明那一行的行尾注释`);
        }
    });

    for (const [nodeName, node] of Object.entries(data)) {
        if (!node || typeof node !== "object" || Array.isArray(node)) continue;
        if (!("desc" in node)) continue;

        const key = `${rel}::${nodeName}`;
        const line = lineOfNode(raw, nodeName);
        const where = `${rel}:${line} ${nodeName}`;
        const desc = node.desc;

        if (typeof desc !== "string") {
            errors.push(`${where}：desc 必须是字符串`);
            continue;
        }
        if (allowlist.has(key)) {
            notes.push(`${where}：命中白名单，已跳过`);
            continue;
        }

        const chars = [...desc].length;
        if (chars > MAX_LEN) errors.push(`${where}：desc 长度 ${chars} > ${MAX_LEN}`);
        if (/[。；;]/.test(desc)) errors.push(`${where}：desc 含句号或分号，应拆成一句话`);
        if ((desc.match(/，/g) ?? []).length >= 2) errors.push(`${where}：desc 含多个逗号，应拆分到注释`);
        if (/^\s|\s$/.test(desc)) warnings.push(`${where}：desc 首尾有空白`);
        if (IMPL_PATTERN.test(desc)) warnings.push(`${where}：desc 疑似写了实现细节（${desc}）`);
    }
}

const label = (s) => `\u001b[${s}m`;
for (const n of notes) console.log(`${label(36)}note${label(0)}  ${n}`);
for (const w of warnings) console.warn(`${label(33)}warn${label(0)}  ${w}`);
for (const e of errors) console.error(`${label(31)}error${label(0)} ${e}`);

const scanned = files.length;
if (errors.length > 0) {
    console.error(`\n${label(31)}desc 规范校验失败：${errors.length} 项错误，${warnings.length} 项警告（${scanned} 个文件）。${label(0)}`);
    process.exit(1);
}
if (strict && warnings.length > 0) {
    console.error(`\n${label(31)}desc 规范校验失败（--strict）：${warnings.length} 项警告。${label(0)}`);
    process.exit(1);
}
console.log(`\n${label(32)}desc 规范校验通过：${scanned} 个文件，${warnings.length} 项警告。${label(0)}`);
