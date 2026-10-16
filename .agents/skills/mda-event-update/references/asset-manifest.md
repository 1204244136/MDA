# 素材清单与命令

`prepare_templates.py` 依赖 `opencv-python`；不需要设备。清单相对路径以清单目录解析，输出目录放在仓库外。以下命令从 MDA 仓库根目录运行；如使用本机安装版，把 `$skillDir` 改为实际 skill 目录。AI 根据看到的画面自行生成 box，用户只需给图。

## 一张截图中的两种标记

下面只是格式示例，坐标必须来自本次图片。`StageHard` 表示难度，`stage` / `repeatable` 表示标记角色。

```json
{
    "event": "SmallEvent",
    "theme": "LittleWitchTrick",
    "short_side": 720,
    "assets": [
        {
            "source": "screen.png",
            "kind": "screenshot",
            "role": "stage",
            "slot": "StageHard",
            "space": "source",
            "box": [
                695,
                476,
                37,
                15
            ]
        },
        {
            "source": "screen.png",
            "kind": "screenshot",
            "role": "repeatable",
            "slot": "StageHard",
            "box": [
                596,
                333,
                24,
                23
            ],
            "target_offset": [
                -40,
                45,
                0,
                0
            ]
        }
    ]
}
```

截图 `frame` 可指定 `[x,y,w,h]`，表示原文件中的完整游戏画面。`space: source` 的 box 始终是原文件坐标；`space: pipeline` 则是 frame 按短边缩放后的坐标。正尺寸按框架方式四舍五入，缩放使用 OpenCV `INTER_AREA`；裁剪左上向下取整、右下向上取整，包含选择的像素范围。应先查看结果，再决定是否微调到更紧的整像素框。

## 已裁图合流

将某个 asset 换为以下形式即可，角色、slot、最终文件名规则完全相同：

```json
{
    "source": "user-cropped-repeatable.png",
    "kind": "crop",
    "role": "repeatable",
    "slot": "StageHard",
    "source_frame_size": [
        1920,
        1080
    ]
}
```

`source_frame_size` 是裁图来自的整张游戏画面尺寸，不能填裁图自己的尺寸。未知时省略，工具保持原像素并在报告标记 `scale_verified: false`；AI 应通过识别回放检查，不擅自声称已验证尺度。

## 输出和安装

```powershell
$skillDir = (Resolve-Path ".agents\skills\mda-event-update").Path
uv run --with opencv-python python "$skillDir\scripts\prepare_templates.py" --manifest "C:\Temp\event-input\plan.json" --output-dir "C:\Temp\event-preview"
# 查看候选 PNG 和 report.json、完成匹配与落点核对后安装：
uv run --with opencv-python python "$skillDir\scripts\prepare_templates.py" --manifest "C:\Temp\event-input\plan.json" --output-dir "C:\Temp\event-preview" --install "C:\Users\12042\Documents\GitHub\MDA"
```

输出路径为 `assets/resource/image/{SmallEvent|LargeEvent}/{Theme}/{Theme}{Slot}[Repeatable].png`。Logo 的 role 为 `logo`、无需 slot，输出 `{Theme}Logo.png`。

小活动 slot 支持 `StageNormal`、`StageHard`、`Story1Stage`、`Story2StageNormal`、`Story2StageHard`；大活动仅接受 Story 命名。不要因 slot 可用就创建尚未开放的模式。

报告记录原图路径/尺寸、缩放后的尺寸、裁剪框、输出路径、PNG SHA256 和可选偏移。`--install` 只复制清单涉及的 PNG；不修改 Pipeline，也不删/移动原图。全部输入与覆盖冲突通过后才写入。已存在且内容不同的预览/模板默认拒绝覆盖；核对后用 `--replace` 明确替换本批素材。
