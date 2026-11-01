"""在独立打包目录中，从简体源生成繁体界面字典和文档。"""

import argparse
import json
import re
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from opencc import OpenCC


SOURCE_ASSETS = Path(__file__).resolve().parents[1] / "assets"
DOCUMENT_KEYS = ("description.file", "contact.file", "welcome.file")

# 游戏内的「代码」不是软件代码，避免 s2twp 将其改成「程式碼」。
VALUE_OVERRIDES = {
    "option.ArenaShopItemList.ArenaShopFireCodeManual": "H.S.T.A.代碼手冊",
    "option.ArenaShopItemList.ArenaShopWaterCodeManual": "P.S.I.D.代碼手冊",
    "option.ArenaShopItemList.ArenaShopWindCodeManual": "A.N.M.I.代碼手冊",
    "option.ArenaShopItemList.ArenaShopElectricCodeManual": "Z.E.U.S.代碼手冊",
    "option.ArenaShopItemList.ArenaShopIronCodeManual": "D.M.T.R.代碼手冊",
    "option.ArenaShopItemList.ArenaShopCodeManualSelectionBox": "代碼手冊選擇寶箱",
    "option.EquipmentRerollQuota.affix.ElementalDamage.label": "優越代碼傷害增加",
    "option.EquipmentRerollSingleWant.affix.ElementalDamage.label": "優越代碼傷害增加",
}

# 只转换可读正文；链接目标、代码和占位符即使含中文也必须保持原样。
# 每项的第二个值指定真正需要保护的捕获组，避免连链接文字一起跳过。
PROTECTED_TEXT = (
    (
        re.compile(r"(?m)^ {0,3}(`{3,}|~{3,})[^\n]*\n[\s\S]*?^ {0,3}\1[ \t]*(?:\n|$)"),
        0,
    ),
    (re.compile(r"(`+)[^\n]*?\1(?!`)"), 0),
    (re.compile(r"\]\(\s*(<[^>\n]*>|(?:[^()\s\\]|\\.|\([^()\n]*\))+)"), 1),
    (re.compile(r"(?m)^ {0,3}\[[^\]\n]+\]:[ \t]*(<[^>\n]+>|\S+)"), 1),
    (re.compile(r"\b(?:https?://|mailto:)[^\s<>\[\]，。；！？、]+"), 0),
    (re.compile(r"<[^>\n]+>|\{[^{}\n]*\}"), 0),
)


def convert_text(text: str, converter: "OpenCC") -> str:
    spans = sorted(
        match.span(group)
        for pattern, group in PROTECTED_TEXT
        for match in pattern.finditer(text)
    )
    parts = []
    cursor = 0
    for start, end in spans:
        if end <= cursor:
            continue
        if start > cursor:
            parts.append(converter.convert(text[cursor:start]))
        parts.append(text[max(cursor, start) : end])
        cursor = end
    parts.append(converter.convert(text[cursor:]))
    return "".join(parts)


def write_json(path: Path, value: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(value, ensure_ascii=False, indent=4) + "\n", encoding="utf-8"
    )


def generate_zh_tw(asset_dir: Path) -> None:
    """处理已复制的资源包；拒绝源 assets 及逃出包目录的开发链接。"""
    asset_dir = asset_dir.resolve()
    if asset_dir.is_relative_to(SOURCE_ASSETS.resolve()):
        raise ValueError("繁体资源只能生成到独立打包目录，不能写入源 assets。")

    def staged_path(relative: str) -> Path:
        path = asset_dir / relative
        if not path.resolve().is_relative_to(asset_dir):
            raise ValueError(f"资源路径必须位于独立打包目录中，不能使用开发链接: {path}")
        return path

    interface_path = staged_path("interface.json")
    interface = json.loads(interface_path.read_text(encoding="utf-8"))
    source_path = staged_path(interface["languages"]["zh_cn"])
    source = json.loads(source_path.read_text(encoding="utf-8"))
    if not isinstance(source, dict) or any(not isinstance(v, str) for v in source.values()):
        raise ValueError("简体界面字典必须是字符串键值对象。")

    # Android 元数据和 Agent 构建也会导入本模块，只有准备资源时才需要 OpenCC。
    from opencc import OpenCC

    converter = OpenCC("s2twp")
    translated = {
        key: value if key.endswith(".file") else convert_text(value, converter)
        for key, value in source.items()
    }
    for key, value in VALUE_OVERRIDES.items():
        if key in translated:
            translated[key] = value

    # 先读取并检查所有目标，再开始写入，避免缺文件或开发链接留下半套语言。
    documents = {}
    for key in DOCUMENT_KEYS:
        relative = source[key]
        if not relative.endswith(".zh_cn.md"):
            raise ValueError(f"{key} 必须引用 .zh_cn.md 简体源文档。")
        target = relative.removesuffix(".zh_cn.md") + ".zh_tw.md"
        text = staged_path(relative).read_text(encoding="utf-8")
        documents[staged_path(target)] = convert_text(text, converter)
        translated[key] = target

    locale_path = staged_path("locales/interface/zh_tw.json")
    interface["languages"]["zh_tw"] = "locales/interface/zh_tw.json"
    for path, text in documents.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
    write_json(locale_path, translated)
    write_json(interface_path, interface)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("asset_dir", type=Path, help="已复制的打包资源根目录，例如 install")
    args = parser.parse_args()
    try:
        generate_zh_tw(args.asset_dir)
    except (OSError, ValueError, KeyError) as error:
        parser.exit(1, f"生成繁体资源失败: {error}\n")
    print(f"已生成繁体界面资源: {args.asset_dir.resolve()}")


if __name__ == "__main__":
    main()
