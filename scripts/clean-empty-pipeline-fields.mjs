import fs from "node:fs";
import path from "node:path";

const pipelineRoot = path.resolve("assets/resource/pipeline");

function isPlainObject(value) {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}

function collectJsonFiles(dir) {
    if (!fs.existsSync(dir)) {
        return [];
    }

    const entries = fs.readdirSync(dir, {withFileTypes: true});
    const files = [];

    for (const entry of entries) {
        const fullPath = path.join(dir, entry.name);
        if (entry.isDirectory()) {
            files.push(...collectJsonFiles(fullPath));
            continue;
        }

        if (entry.isFile() && fullPath.endsWith(".json")) {
            files.push(fullPath);
        }
    }

    return files;
}

function cleanNode(node) {
    if (Array.isArray(node)) {
        for (const item of node) {
            cleanNode(item);
        }
        return;
    }

    if (!isPlainObject(node)) {
        return;
    }

    for (const [
        key,
        value,
    ] of Object.entries(node)) {
        cleanNode(value);

        if (key === "next" && Array.isArray(value) && value.length === 0) {
            delete node[key];
            continue;
        }

        if (key === "param" && isPlainObject(value) && Object.keys(value).length === 0) {
            delete node[key];
        }
    }
}

function processFile(filePath) {
    const original = fs.readFileSync(filePath, "utf-8");
    const parsed = JSON.parse(original);
    cleanNode(parsed);

    const cleaned = `${JSON.stringify(parsed, null, 4)}\n`;
    if (cleaned !== original) {
        fs.writeFileSync(filePath, cleaned, "utf-8");
        return true;
    }

    return false;
}

const jsonFiles = collectJsonFiles(pipelineRoot);
let changedCount = 0;

for (const file of jsonFiles) {
    if (processFile(file)) {
        changedCount += 1;
    }
}

console.log(`clean-empty-pipeline-fields: processed ${jsonFiles.length} files, updated ${changedCount} files.`);
