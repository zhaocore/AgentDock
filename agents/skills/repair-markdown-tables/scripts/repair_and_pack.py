"""离线修复完整文件库目录并准备 ZIP，仅本地校验，不上传或导入。"""

import argparse
import json
from pathlib import Path
import subprocess
import tempfile

from repair_tables import repair_path


def repair_and_pack(root: Path, output: Path) -> None:
    """先验证工具与输入，再顺序执行修复及清单重建打包。"""
    if root.is_symlink() or not root.is_dir() or not (root / "confluence-library.json").is_file():
        raise ValueError(f"输入须为包含 confluence-library.json 的完整文件库目录：{root}")
    if output.exists() or output.is_symlink():
        raise FileExistsError(f"输出已存在，请指定新的 ZIP 路径：{output}")
    if output.suffix.lower() != ".zip" or not output.parent.is_dir():
        raise ValueError(f"输出须为现有目录下的新 .zip 文件：{output}")
    repository = Path(__file__).resolve().parents[4]
    with tempfile.TemporaryDirectory(prefix="confluence-repack-tool-") as directory:
        binary = Path(directory) / "repack"
        subprocess.run(["go", "-C", str(repository), "build", "-o", str(binary),
                        "./agents/skills/repair-markdown-tables/scripts"], check=True)
        print(json.dumps({"repair": repair_path(root)}, ensure_ascii=False), flush=True)
        subprocess.run([str(binary), str(root), str(output)], check=True)


def main() -> None:
    """组合命令的错误明确返回非零，修复备份保留供恢复。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path, help="含原始清单的完整文件库目录")
    parser.add_argument("output", type=Path, help="生成的新 ZIP 路径")
    args = parser.parse_args()
    try:
        repair_and_pack(args.root.absolute(), args.output.absolute())
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, json.dumps({"event": "repair_and_pack_failed", "error": str(error)}, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
