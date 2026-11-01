"""渲染 EquipmentReroll 的「效果数值档位对照表」公告图。

数据唯一的来源是 agent/go-service/equipmentreroll/tier.go 的 effectTiers，
脚本直接解析该 Go 字面量，避免图片与识别校准表各写一份而漂移。

版面受 MXU 的展示规则约束（src/services/contentResolver.ts 的 image 渲染 +
src/components/OptionEditor.tsx 的 max-w-[60%] + text-xs 正文）：

- 描述图 `<img class="max-w-full rounded">`，宽度被限制在选项行的 60%，
  按 150% 缩放、1727px 宽的窗口实测约 403 CSS px；图片没有点击放大。
- 因此 PNG 的像素宽度≈CSS 宽度，必须按「设计宽度 ≈ 400 CSS px」排版，
  再按 2 倍渲染（≈800px）保证高 DPI 下仍清晰；做成 2000px 宽的横表只会被压到
  0.19 倍，字小到看不清。
- 6 列（命中率／攻击力／蓄力伤害／防御力 数值相同，合并为一列）× 15 档，
  是「12px 字号 + 400px 宽」下能塞下的最大信息量。

配色照抄游戏内「改造装备效果」区（2026-11-01 实机截图取色）：
普通行 #EAEAEA 底 + #363636 字，第 12～15 档蓝字 #03A3EE，第 15 档黑底 #232323。

用法（仓库根目录）：
    python tools/render_equipment_value_tier.py
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

REPO_ROOT = Path(__file__).resolve().parent.parent
TIER_GO = REPO_ROOT / "agent" / "go-service" / "equipmentreroll" / "tier.go"
OUT_DIR = REPO_ROOT / "assets" / "resource" / "announcement" / "images"

SCALE = 2  # 2 倍渲染：设计宽度 ≈400 CSS px → 成图 ≈800px

# 游戏内取色（装备详情页「改造装备效果」）
BG = "#FCFCFC"        # 卡片底色
ROW = "#EAEAEA"       # 效果行底
INK = "#363636"       # 普通数值字色
STRONG = "#232323"    # 标题 / 黑底
MUTED = "#4A4A4A"     # 辅助说明字色
BLUE = "#03A3EE"      # 第 12～15 档蓝字
BAR = "#232323"       # 第 15 档黑底

# 合并列成员：这四种效果在 effectTiers 里数值完全一致。
SHARED_EFFECTS = ("命中率增加", "攻击力增加", "蓄力伤害增加", "防御力增加")

EFFECTS_EN = {
    "优越代码伤害增加": ("Elemental", "DMG"),
    "命中率增加": ("Hit Rate",),
    "最大装弹数增加": ("Max", "Ammo"),
    "攻击力增加": ("ATK",),
    "蓄力伤害增加": ("Charge", "DMG"),
    "蓄力速度增加": ("Charge", "Speed"),
    "暴击率增加": ("Crit", "Rate"),
    "暴击伤害增加": ("Crit", "DMG"),
    "防御力增加": ("DEF",),
}

EFFECTS_ZH = {
    "优越代码伤害增加": ("优越代码", "伤害"),
    "最大装弹数增加": ("最大装弹数",),
    "蓄力速度增加": ("蓄力速度",),
    "暴击率增加": ("暴击率",),
    "暴击伤害增加": ("暴击伤害",),
}


@dataclass(frozen=True)
class Lang:
    code: str
    tier_head: str
    initial_badge: str
    shared_label: tuple[str, ...]
    effect_labels: dict[str, tuple[str, ...]]

    font_regular: Path
    font_bold: Path


ZH = Lang(
    code="zh_cn",
    tier_head="档位",
    initial_badge="初始",
    shared_label=("命中率／攻击力", "蓄力伤害／防御力"),
    effect_labels=EFFECTS_ZH,

    font_regular=Path("C:/Windows/Fonts/msyh.ttc"),
    font_bold=Path("C:/Windows/Fonts/msyhbd.ttc"),
)

EN = Lang(
    code="en_us",
    tier_head="Tier",
    initial_badge="init",
    shared_label=("Hit Rate / ATK", "Charge DMG / DEF"),
    effect_labels=EFFECTS_EN,

    font_regular=Path("C:/Windows/Fonts/segoeui.ttf"),
    font_bold=Path("C:/Windows/Fonts/segoeuib.ttf"),
)


def load_effect_tiers() -> dict[str, list[float]]:
    """解析 tier.go 里的 effectTiers，保持源码中的声明顺序。"""
    source = TIER_GO.read_text(encoding="utf-8")
    block = re.search(
        r"effectTiers\s*=\s*map\[string\]\[15\]float64\{(?P<body>.*?)\n\}",
        source,
        re.S,
    )
    if not block:
        raise SystemExit("未能从 tier.go 解析出 effectTiers")

    tiers: dict[str, list[float]] = {}
    for name, values in re.findall(r'"([^"]+)"\s*:\s*\{([^}]*)\}', block.group("body")):
        parsed = [float(v) for v in re.findall(r"\d+(?:\.\d+)?", values)]
        if len(parsed) != 15:
            raise SystemExit(f"{name} 的档位不是 15 个，而是 {len(parsed)} 个")
        tiers[name] = parsed
    if len(tiers) != 9:
        raise SystemExit(f"effectTiers 应有 9 种效果，实际 {len(tiers)} 种")

    shared = tiers[SHARED_EFFECTS[0]]
    for name in SHARED_EFFECTS[1:]:
        if tiers[name] != shared:
            raise SystemExit(f"{name} 与 {SHARED_EFFECTS[0]} 数值不同，无法合并为一列")
    return tiers


def font(path: Path, pt: float) -> ImageFont.FreeTypeFont:
    return ImageFont.truetype(str(path), round(pt * SCALE))


def render(lang: Lang, tiers: dict[str, list[float]]) -> Path:
    margin = 8 * SCALE
    label_w = 34 * SCALE
    shared_w = 78 * SCALE
    cell_w = 60 * SCALE
    head_h = 34 * SCALE
    row_h = 19 * SCALE
    gap = 2 * SCALE
    radius = 5 * SCALE

    f_head = font(lang.font_bold, 10)
    f_badge = font(lang.font_bold, 6.5)
    f_value = font(lang.font_bold, 11)
    f_label = font(lang.font_bold, 10)

    columns: list[tuple[tuple[str, ...], list[float], int]] = []
    for name, values in tiers.items():
        if name in SHARED_EFFECTS:
            if name != SHARED_EFFECTS[0]:
                continue
            columns.append((lang.shared_label, values, shared_w))
            continue
        columns.append((lang.effect_labels[name], values, cell_w))

    table_w = label_w + sum(w for _, _, w in columns)
    width = table_w + margin * 2

    rows_h = len(tiers[SHARED_EFFECTS[0]]) * row_h + (len(tiers[SHARED_EFFECTS[0]]) - 1) * gap
    table_h = head_h + gap + rows_h
    height = margin + table_h + margin

    img = Image.new("RGB", (width, height), BG)
    draw = ImageDraw.Draw(img)
    x0 = margin
    body_y = margin

    # 表头
    draw.rounded_rectangle([x0, body_y, x0 + table_w, body_y + head_h], radius=radius, fill=ROW)
    mid_y = body_y + head_h // 2
    draw.text((x0 + label_w // 2, mid_y), lang.tier_head, font=f_head, fill=STRONG, anchor="mm")

    cx = x0 + label_w
    for label, _, w in columns:
        lines = list(label)
        step = 12 * SCALE
        top = mid_y - (len(lines) - 1) * step // 2
        for k, line in enumerate(lines):
            draw.text(
                (cx + w // 2, top + k * step),
                line,
                font=f_head,
                fill=STRONG,
                anchor="mm",
            )
        cx += w

    # 数据行
    row_tops = []
    values = tiers[SHARED_EFFECTS[0]]
    for r in range(15):
        tier = r + 1
        top = body_y + head_h + gap + r * (row_h + gap)
        row_tops.append(top)
        draw.rounded_rectangle([x0, top, x0 + table_w, top + row_h], radius=radius, fill=ROW)
        mid = top + row_h // 2

        if tier == 11:
            draw.text((x0 + 7 * SCALE, mid), str(tier), font=f_label, fill=STRONG, anchor="lm")
            draw.text((x0 + 21 * SCALE, mid), lang.initial_badge, font=f_badge, fill=MUTED, anchor="lm")
        else:
            draw.text((x0 + 7 * SCALE, mid), str(tier), font=f_label, fill=STRONG, anchor="lm")

        cx = x0 + label_w
        for _, col_values, w in columns:
            if tier == 15:
                draw.text((cx + w // 2, mid), f"{col_values[r]:.2f}%", font=f_value, fill=BLUE, anchor="mm")
            else:
                fill = BLUE if tier >= 12 else INK
                draw.text((cx + w // 2, mid), f"{col_values[r]:.2f}%", font=f_value, fill=fill, anchor="mm")
            cx += w

    # 第 15 档整行黑底（对应游戏内最高档的黑底行）
    bar_y = row_tops[14]
    draw.rounded_rectangle([x0, bar_y, x0 + table_w, bar_y + row_h], radius=radius, fill=BAR)
    draw.text((x0 + 7 * SCALE, bar_y + row_h // 2), "15", font=f_label, fill=BG, anchor="lm")
    cx = x0 + label_w
    for _, col_values, w in columns:
        draw.text((cx + w // 2, bar_y + row_h // 2), f"{col_values[14]:.2f}%", font=f_value, fill=BLUE, anchor="mm")
        cx += w

    out = OUT_DIR / f"EquipmentRerollValueTier.{lang.code}.png"
    img.save(out, optimize=True)
    print(f"{out.relative_to(REPO_ROOT)}  {img.width}x{img.height}  ({img.width / img.height:.2f}:1)")
    return out


def main() -> None:
    tiers = load_effect_tiers()
    for lang in (ZH, EN):
        render(lang, tiers)


if __name__ == "__main__":
    main()
