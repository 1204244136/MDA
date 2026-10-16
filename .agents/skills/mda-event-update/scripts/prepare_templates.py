"""按清单将活动截图/已裁图合流成命名模板；默认仅写外部工作目录。依赖 opencv-python。"""

import argparse
import hashlib
import json
import math
from pathlib import Path
import re

import cv2
import numpy as np


SLOTS = {"StageNormal", "StageHard", "Story1Stage", "Story2StageNormal", "Story2StageHard"}


def rect(value, size, label):
    if (not isinstance(value, list) or len(value) != 4 or
            any(type(v) is not int for v in value)):
        raise ValueError(f"{label} 必须为四个整数 [x,y,w,h]")
    x, y, w, h = value
    if min(x, y) < 0 or min(w, h) <= 0 or x + w > size[0] or y + h > size[1]:
        raise ValueError(f"{label} 越界或为空：{value} / {size}")
    return x, y, w, h


def target_size(size, short_side):
    w, h = size
    # 与 ControllerAgent 的 std::round、短边缩放保持一致；不把宽屏强行拉伸成 1280x720。
    scale = short_side / min(w, h)
    return max(1, math.floor(w * scale + 0.5)), max(1, math.floor(h * scale + 0.5))


def read_image(path):
    image = cv2.imdecode(np.fromfile(path, dtype=np.uint8), cv2.IMREAD_COLOR)
    if image is None:
        raise ValueError(f"无法读取图片：{path}")
    return image


def prepare(manifest_path, output_dir, repo=None, replace=False):
    manifest_path, output_dir = Path(manifest_path).resolve(), Path(output_dir).resolve()
    spec = json.loads(manifest_path.read_text(encoding="utf-8-sig"))
    event, theme = spec["event"], spec["theme"]
    if (event not in {"SmallEvent", "LargeEvent"} or theme in {"CurrentEvent", "Other"} or
            not re.fullmatch(r"[A-Z][A-Za-z0-9]*", theme)):
        raise ValueError("event 应为 SmallEvent/LargeEvent，theme 应为 PascalCase case 标识")
    short_side = spec.get("short_side", 720)
    if type(short_side) is not int or short_side <= 0:
        raise ValueError("short_side 必须为正整数，默认 720")
    assets = spec["assets"]
    if not isinstance(assets, list) or not assets:
        raise ValueError("assets 应为非空数组")
    if repo:
        repo = Path(repo).resolve()
        if output_dir.is_relative_to(repo):
            raise ValueError("预览和清单输出目录必须在源仓库外")
        interface_path = repo / "assets/interface.json"
        interface = json.loads(interface_path.read_text(encoding="utf-8-sig"))
        resource_root = (repo / "assets/resource").resolve()
        declared = {(interface_path.parent / p).resolve()
                    for resource in interface["resource"] for p in resource["path"]}
        if resource_root not in declared:
            raise ValueError("当前工具只安装到 Interface 已声明的 assets/resource，请核实项目布局")

    plans, targets = [], set()
    for item in assets:
        role, slot = item["role"], item.get("slot")
        if role == "logo":
            suffix = "Logo"
        elif role in {"stage", "repeatable"} and slot in SLOTS:
            if event == "LargeEvent" and slot.startswith("Stage"):
                raise ValueError("大活动关卡应按实际 Story 命名，而非 StageNormal/StageHard")
            suffix = slot + ("Repeatable" if role == "repeatable" else "")
        else:
            raise ValueError("role 应为 logo/stage/repeatable，关卡素材必须指定合法 slot")
        relative = Path("assets/resource/image") / event / theme / f"{theme}{suffix}.png"
        if relative in targets:
            raise ValueError(f"同一清单重复生成模板：{relative}")
        targets.add(relative)
        source = (manifest_path.parent / item["source"]).resolve()
        if source.is_relative_to(output_dir):
            raise ValueError("输入图片必须位于输出目录外，避免覆盖原件")
        image = read_image(source)
        source_size = [image.shape[1], image.shape[0]]
        kind = item["kind"]
        evidence = {"source": str(source), "source_size": source_size, "kind": kind}
        if kind == "screenshot":
            frame = rect(item.get("frame", [0, 0, *source_size]), source_size, "frame")
            fx, fy, fw, fh = frame
            size = target_size((fw, fh), short_side)
            screen = cv2.resize(image[fy:fy + fh, fx:fx + fw], size, interpolation=cv2.INTER_AREA)
            box = item["box"]
            space = item.get("space", "source")
            if space == "source":
                x, y, w, h = rect(box, source_size, "box")
                rect([x - fx, y - fy, w, h], (fw, fh), "frame 内的 box")
                left, top = math.floor((x - fx) * size[0] / fw), math.floor((y - fy) * size[1] / fh)
                right = math.ceil((x + w - fx) * size[0] / fw)
                bottom = math.ceil((y + h - fy) * size[1] / fh)
                box = [left, top, right - left, bottom - top]
            elif space != "pipeline":
                raise ValueError("space 应为 source/pipeline")
            x, y, w, h = rect(box, size, "pipeline box")
            crop = screen[y:y + h, x:x + w].copy()
            evidence.update(frame=list(frame), pipeline_size=list(size), box=[x, y, w, h])
        elif kind == "crop":
            if "box" in item or "frame" in item:
                raise ValueError("已裁图不接受 box/frame；使用 source_frame_size 指定原始整屏尺寸")
            frame_size = item.get("source_frame_size")
            if frame_size is not None:
                if (not isinstance(frame_size, list) or len(frame_size) != 2 or
                        any(type(v) is not int or v <= 0 for v in frame_size)):
                    raise ValueError("source_frame_size 应为原始整屏的 [width,height]")
                if any(crop_dim > frame_dim for crop_dim, frame_dim in zip(source_size, frame_size)):
                    raise ValueError("source_frame_size 不能小于已裁图尺寸")
                scale = short_side / min(frame_size)
                crop_size = tuple(max(1, math.floor(v * scale + 0.5)) for v in source_size)
                crop = cv2.resize(image, crop_size, interpolation=cv2.INTER_AREA)
                evidence.update(source_frame_size=frame_size, scale_verified=True)
            else:
                crop = image.copy()
                evidence.update(scale_verified=False, note="未提供来源整屏尺寸，保留裁图像素；需识别回放核实比例")
        else:
            raise ValueError("kind 应为 screenshot/crop")

        ok, encoded = cv2.imencode(".png", crop)
        if not ok:
            raise ValueError(f"PNG 编码失败：{source}")
        data = encoded.tobytes()
        evidence.update(path=relative.as_posix(), role=role, slot=slot,
                        size=[crop.shape[1], crop.shape[0]], sha256=hashlib.sha256(data).hexdigest())
        if role == "repeatable" and "target_offset" in item:
            offset = item["target_offset"]
            if not isinstance(offset, list) or len(offset) != 4 or any(type(v) is not int for v in offset):
                raise ValueError("target_offset 必须为四个整数；它是动作参数，不改变模板像素")
            evidence["target_offset"] = offset
        preview = output_dir / relative
        if preview.exists() and preview.read_bytes() != data and not replace:
            raise ValueError(f"预览已存在且内容不同，检查后使用 --replace：{preview}")
        destination = repo / relative if repo else None
        if destination and destination.exists() and destination.read_bytes() != data and not replace:
            raise ValueError(f"模板已存在且内容不同，检查后使用 --replace：{destination}")
        plans.append((preview, destination, data, evidence))

    # 全部输入与覆盖冲突通过后才写入，避免第二个素材错误时已修改第一个。
    report = {"event": event, "theme": theme, "short_side": short_side,
              "installed": bool(repo), "assets": []}
    for preview, destination, data, evidence in plans:
        preview.parent.mkdir(parents=True, exist_ok=True)
        preview.write_bytes(data)
        if destination:
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(data)
        report["assets"].append(evidence)
    output_dir.mkdir(parents=True, exist_ok=True)
    (output_dir / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--install", type=Path, metavar="REPO", help="验证后将模板写入目标 MDA 仓库")
    parser.add_argument("--replace", action="store_true", help="允许替换清单涉及的已有不同内容模板")
    args = parser.parse_args()
    try:
        result = prepare(args.manifest, args.output_dir, args.install, args.replace)
    except (ValueError, KeyError, OSError, cv2.error) as err:
        parser.exit(1, f"素材准备失败：{err}\n")
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
