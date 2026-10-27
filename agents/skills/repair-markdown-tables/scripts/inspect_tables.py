"""只读检查 Markdown 目录，输出可修复项目及可交给源版本修复器的失败清单。"""

import argparse
from dataclasses import asdict, dataclass
import json
from pathlib import Path

from repair_tables import analyze_file


@dataclass(frozen=True)
class PlannedFile:
    """记录确定可离线修复的文件。"""

    path: str
    tables: int


@dataclass(frozen=True)
class FailedFile:
    """记录需要源版本恢复的文件和具体原因。"""

    path: str
    error: str


@dataclass(frozen=True)
class Inspection:
    """完整只读预检结果，不包含正文。"""

    root: str
    scanned: int
    planned: list[PlannedFile]
    unresolved: list[FailedFile]


def inspect_directory(root: Path) -> Inspection:
    """检查所有 Markdown，不因单个结构失败而丢失其他检查结果。"""
    if root.is_symlink() or not root.is_dir():
        raise ValueError(f"输入须为普通目录：{root}")
    files = sorted(root.rglob("*.md"))
    planned: list[PlannedFile] = []
    unresolved: list[FailedFile] = []
    for file in files:
        relative = str(file.relative_to(root))
        try:
            repair = analyze_file(file)
        except (ValueError, OSError) as error:
            unresolved.append(FailedFile(relative, str(error)))
            continue
        if repair.original != repair.updated:
            planned.append(PlannedFile(relative, repair.tables))
    return Inspection(str(root), len(files), planned, unresolved)


def main() -> None:
    """保存完整预检清单，不覆盖已有报告，不修改 Markdown。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("root", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    try:
        report = inspect_directory(args.root.absolute())
        with args.output.open("x", encoding="utf-8") as stream:
            json.dump(asdict(report), stream, ensure_ascii=False, indent=2)
        print(json.dumps({"scanned": report.scanned, "planned": len(report.planned),
                          "unresolved": len(report.unresolved), "report": str(args.output.absolute())}, ensure_ascii=False))
    except (OSError, ValueError) as error:
        parser.exit(1, str(error) + "\n")


if __name__ == "__main__":
    main()
