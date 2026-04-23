"""Optimize PNG images in the assets directory using oxipng."""

import subprocess
import sys
from pathlib import Path


def find_png_images(root_dir: str = "assets") -> list[Path]:
    """Find all PNG images under the given root directory."""
    root = Path(root_dir)
    if not root.exists():
        print(f"Directory {root_dir} does not exist, skipping.")
        return []
    return sorted(root.rglob("*.png"))


def optimize_image(image_path: Path) -> bool:
    """Optimize a single PNG image using oxipng. Returns True if file was changed."""
    original_size = image_path.stat().st_size

    try:
        subprocess.run(
            [
                "oxipng",
                "-o", "4",       # optimization level 4 (good balance)
                "--strip", "safe", # strip safe metadata chunks
                "--alpha",        # optimize alpha channel
                str(image_path),
            ],
            check=True,
            capture_output=True,
            text=True,
        )
    except subprocess.CalledProcessError as e:
        print(f"Error optimizing {image_path}: {e.stderr}", file=sys.stderr)
        return False

    new_size = image_path.stat().st_size
    if new_size < original_size:
        saved = original_size - new_size
        pct = (saved / original_size) * 100
        print(f"  Optimized: {image_path} ({original_size} -> {new_size}, -{pct:.1f}%)")
        return True

    return False


def main() -> None:
    root_dir = "assets"
    images = find_png_images(root_dir)

    if not images:
        print("No PNG images found.")
        return

    print(f"Found {len(images)} PNG image(s) to check.")

    optimized_count = 0
    for img in images:
        if optimize_image(img):
            optimized_count += 1

    print(f"\nDone. Optimized {optimized_count}/{len(images)} image(s).")


if __name__ == "__main__":
    main()
