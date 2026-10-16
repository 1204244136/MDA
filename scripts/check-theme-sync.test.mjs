import assert from "node:assert/strict";
import {spawnSync} from "node:child_process";
import {cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from "node:fs";
import {tmpdir} from "node:os";
import {dirname, join, resolve} from "node:path";
import {fileURLToPath} from "node:url";
import test from "node:test";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const tasks = "assets/tasks/SmallEvent.json";
const story = "assets/resource/pipeline/Event/SmallEvent/SmallEventStory.json";
const themes = "assets/resource/pipeline/Event/SmallEvent/Themes";
const route = "SmallEventEventStageQuickBattleFlow";
const normal = "SmallEventLittleWitchTrickClickStageNormalRepeatable";
const hard = "SmallEventLittleWitchTrickClickStageHardRepeatable";

function fixture(t) {
    const dir = mkdtempSync(join(tmpdir(), "mda-theme-check-"));
    t.after(() => {
        assert.equal(dirname(dir), resolve(tmpdir()));
        rmSync(dir, {recursive: true, force: true});
    });
    for (const path of [
        "scripts/check-theme-sync.mjs",
        tasks,
        "assets/tasks/LargeEvent.json",
        "assets/resource/pipeline/Event/SmallEvent",
        "assets/resource/pipeline/Event/LargeEvent",
    ]) {
        cpSync(join(root, path), join(dir, path), {recursive: true});
    }
    return dir;
}

function edit(dir, path, change) {
    // Fixture files contain only ordinary JSON strings and line comments.
    const doc = JSON.parse(readFileSync(join(dir, path), "utf8").replace(/\/\/[^\n]*/g, ""));
    change(doc);
    writeFileSync(join(dir, path), JSON.stringify(doc));
}

function check(dir, expected) {
    const result = spawnSync(process.execPath, [join(dir, "scripts/check-theme-sync.mjs")], {encoding: "utf8"});
    const output = result.stdout + result.stderr;
    if (expected) {
        assert.equal(result.status, 1, output);
        assert.match(output, expected);
    } else assert.equal(result.status, 0, output);
}

test("现有全部主题通过路由和模板校验", (t) => check(fixture(t)));

test("具体主题漏覆盖路由不能继承当前活动", (t) => {
    const dir = fixture(t);
    edit(
        dir,
        tasks,
        (doc) => delete doc.option.SmallEventTheme.cases.find((c) => c.name === "BSideIdol").pipeline_override[route],
    );
    check(dir, /BSideIdol 必须完整声明扫荡 next/);
});

test("具体主题不能引用别的主题点击节点", (t) => {
    const dir = fixture(t);
    edit(
        dir,
        tasks,
        (doc) =>
            (doc.option.SmallEventTheme.cases.find((c) => c.name === "BSideIdol").pipeline_override[route].next[0] =
                hard),
    );
    check(dir, /BSideIdol 的扫荡路由.*引用了其他主题/);
});

test("base 换入口模板但忘记切换扫荡路由会失败", (t) => {
    const dir = fixture(t);
    edit(dir, "assets/resource/pipeline/Event/SmallEvent/SmallEvent.json", (doc) => {
        doc.SmallEventEnterMainPage.recognition.param.template = ["SmallEvent/B-SideIdol/B-SideIdolLogo.png"];
    });
    check(dir, /CurrentEvent\/base 的扫荡路由.*引用了其他主题/);
});

test("Normal 的非零偏移不能直接复用于新增 Hard 模板", (t) => {
    const dir = fixture(t);
    edit(dir, `${themes}/LittleWitchTrick.json`, (doc) =>
        doc[normal].recognition.param.template.push(doc[hard].recognition.param.template[0]),
    );
    check(dir, /Normal\/Hard 模板携带非零偏移/);
});

test("当前活动不能遗漏具体主题已有的 Hard 分支", (t) => {
    const dir = fixture(t);
    edit(
        dir,
        story,
        (doc) =>
            (doc[route].next = [
                normal,
                "SmallEventGoBack",
            ]),
    );
    check(dir, /不能遗漏难度分支/);
});

test("主题节点不能换成其他主题模板", (t) => {
    const dir = fixture(t);
    edit(
        dir,
        `${themes}/LittleWitchTrick.json`,
        (doc) => (doc[hard].recognition.param.template = ["SmallEvent/B-SideIdol/B-SideIdolStageHardRepeatable.png"]),
    );
    check(dir, /必须使用 LittleWitchTrick 自己的模板/);
});

test("主题偏移不能重新放回共用路由", (t) => {
    const dir = fixture(t);
    edit(
        dir,
        story,
        (doc) =>
            (doc[route].action = {
                type: "Click",
                param: {
                    target_offset: [
                        -80,
                        -30,
                        0,
                        0,
                    ],
                },
            }),
    );
    check(dir, /必须是无识别、无点击动作的路由节点/);
});

test("theme case 不能通过 override 修改叶子动作", (t) => {
    const dir = fixture(t);
    edit(
        dir,
        tasks,
        (doc) =>
            (doc.option.SmallEventTheme.cases.find((c) => c.name === "BSideIdol").pipeline_override[hard] = {
                action: {
                    param: {
                        target_offset: [
                            0,
                            0,
                            0,
                            0,
                        ],
                    },
                },
            }),
    );
    check(dir, /不应覆盖扫荡节点/);
});

test("点击后仍应进入共用战斗页面或返回流程", (t) => {
    const dir = fixture(t);
    edit(dir, `${themes}/LittleWitchTrick.json`, (doc) => (doc[hard].next = []));
    check(dir, /必须执行点击并接回共用战斗页面/);
});

test("CurrentEvent 与 Other 保持无 override", (t) => {
    const dir = fixture(t);
    edit(dir, tasks, (doc) => {
        for (const c of doc.option.SmallEventTheme.cases.filter((c) =>
            [
                "CurrentEvent",
                "Other",
            ].includes(c.name),
        )) {
            c.pipeline_override = {
                [route]: {
                    next: [
                        hard,
                        "SmallEventGoBack",
                    ],
                },
            };
        }
    });
    check(dir, /CurrentEvent 不应携带 pipeline_override/);
    check(dir, /Other 应保持无 override/);
});

test("升级未来主题只新增自身节点和切换路由，保留旧主题偏移", (t) => {
    const dir = fixture(t);
    const nodeName = "SmallEventFutureThemeClickStageRepeatable";
    const next = [
        nodeName,
        "SmallEventGoBack",
    ];
    const leafPath = `${themes}/LittleWitchTrick.json`;
    const oldLeaves = readFileSync(join(dir, leafPath), "utf8");
    const tpl = "SmallEvent/FutureTheme/FutureThemeStageNormalRepeatable.png";
    writeFileSync(
        join(dir, themes, "FutureTheme.json"),
        JSON.stringify({
            [nodeName]: {
                recognition: {type: "TemplateMatch", param: {template: [tpl]}},
                action: {type: "Click"},
                next: [
                    "SmallEventEventStageBattlePage",
                    "SmallEventGoBack",
                ],
            },
        }),
    );
    const logo = ["SmallEvent/FutureTheme/FutureThemeLogo.png"];
    edit(dir, tasks, (doc) =>
        doc.option.SmallEventTheme.cases.push({
            name: "FutureTheme",
            pipeline_override: {
                SmallEventEnterMainPage: {recognition: {param: {template: logo}}},
                SmallEventClickStage: {
                    recognition: {param: {template: ["SmallEvent/FutureTheme/FutureThemeStageNormal.png"]}},
                },
                [route]: {next},
            },
        }),
    );
    edit(
        dir,
        "assets/resource/pipeline/Event/SmallEvent/SmallEvent.json",
        (doc) => (doc.SmallEventEnterMainPage.recognition.param.template = logo),
    );
    edit(dir, story, (doc) => {
        doc[route].next = next;
        doc.SmallEventClickStage.recognition.param.template = ["SmallEvent/FutureTheme/FutureThemeStageNormal.png"];
    });
    check(dir);
    assert.equal(readFileSync(join(dir, leafPath), "utf8"), oldLeaves);
});
