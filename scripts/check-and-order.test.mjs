import assert from "node:assert/strict";
import {mkdtempSync, rmSync, writeFileSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import test from "node:test";

import {checkFiles} from "./check-and-order.mjs";

/** 每个用例在临时目录里写一份 pipeline JSON，直接喂给 checkFiles。 */
function run(t, doc) {
    const dir = mkdtempSync(join(tmpdir(), "mda-and-check-"));
    t.after(() => rmSync(dir, {recursive: true, force: true}));
    const file = join(dir, "fixture.json");
    writeFileSync(file, JSON.stringify(doc, null, 4));
    return checkFiles([file]);
}

/** 造一个「引用了别的节点的 roi」的子识别。 */
const refNode = (roiNode) => ({
    recognition: {
        type: "ColorMatch",
        param: {
            roi: roiNode,
            lower: [
                0,
                0,
                0,
            ],
            upper: [
                1,
                1,
                1,
            ],
        },
    },
});

const plainNode = {
    recognition: {
        type: "TemplateMatch",
        param: {
            roi: [
                0,
                0,
                10,
                10,
            ],
            template: ["x.png"],
        },
    },
};

test("后向依赖要报错（897db5b 的 SoloRaid 形态）", (t) => {
    const {errors} = run(t, {
        Visible: plainNode,
        Available: refNode("Visible"),
        // 取框者是 Available，但它的 roi 依赖 Visible，却被排到了前面
        Select: {
            recognition: {
                type: "And",
                param: {
                    all_of: [
                        "Available",
                        "Visible",
                    ],
                },
            },
            action: {type: "Click"},
        },
    });
    assert.equal(errors.length, 1);
    assert.match(errors[0], /执行顺序反了/);
    assert.match(errors[0], /box_index/);
});

test("依赖在前 + box_index 指向取框者，不报错", (t) => {
    const {errors} = run(t, {
        Visible: plainNode,
        Available: refNode("Visible"),
        Select: {
            recognition: {
                type: "And",
                param: {
                    all_of: [
                        "Visible",
                        "Available",
                    ],
                    box_index: 1,
                },
            },
            action: {type: "Click"},
        },
    });
    assert.deepEqual(errors, []);
});

test("无依赖时调换顺序并删掉 box_index，不报错", (t) => {
    const {errors} = run(t, {
        RedDot: plainNode,
        Icon: plainNode,
        Select: {
            recognition: {
                type: "And",
                param: {
                    all_of: [
                        "Icon",
                        "RedDot",
                    ],
                },
            },
            action: {type: "Click"},
        },
    });
    assert.deepEqual(errors, []);
});

test("box_index 越界要报错", (t) => {
    const {errors} = run(t, {
        Visible: plainNode,
        Available: refNode("Visible"),
        Select: {
            recognition: {
                type: "And",
                param: {
                    all_of: [
                        "Visible",
                        "Available",
                    ],
                    box_index: 2,
                },
            },
        },
    });
    assert.equal(errors.length, 1);
    assert.match(errors[0], /越界/);
});

test("跨节点引用（不在同一 all_of 内）不判错", (t) => {
    const {errors} = run(t, {
        SomePanel: plainNode,
        Available: refNode("SomePanel"),
        Select: {
            recognition: {type: "And", param: {all_of: ["Available"]}},
        },
    });
    assert.deepEqual(errors, []);
});
