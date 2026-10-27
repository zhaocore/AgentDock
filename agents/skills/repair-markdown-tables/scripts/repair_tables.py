"""离线修复旧版导出器生成的断行表格，原地写入前保留备份。"""

import argparse
from dataclasses import dataclass
import json
import os
from pathlib import Path
import re
import stat
import tempfile


@dataclass(frozen=True)
class Repair:
    """记录单个文件的原始字节和修复结果。"""

    path: Path
    original: bytes
    updated: bytes
    tables: int


def pipe_positions(text: str) -> list[int]:
    """定位未转义的竖线，保留反斜线与内容原值。"""
    positions: list[int] = []
    escaped = False
    for index, character in enumerate(text):
        if escaped:
            escaped = False
        elif character == "\\":
            escaped = True
        elif character == "|":
            positions.append(index)
    return positions


def prose_lines(lines: list[str]) -> list[bool]:
    """排除 front matter、围栏代码和缩进代码。"""
    result: list[bool] = []
    front_matter = bool(lines and lines[0].strip() == "---")
    fence = ""
    html_depth = 0
    for index, line in enumerate(lines):
        if front_matter:
            result.append(False)
            if index > 0 and line.strip() in {"---", "..."}:
                front_matter = False
            continue
        if html_depth or (not fence and re.match(r"^\s*<table(?:\s|>)", line, re.IGNORECASE)):
            result.append(False)
            html_depth += len(re.findall(r"<table\b", line, re.IGNORECASE)) - len(re.findall(r"</table\s*>", line, re.IGNORECASE))
            continue
        marker = re.match(r"^ {0,3}(`{3,}|~{3,})(.*)$", line)
        if fence:
            result.append(False)
            if marker and marker[1][0] == fence[0] and len(marker[1]) >= len(fence) and not marker[2].strip():
                fence = ""
        elif marker:
            result.append(False)
            fence = marker[1]
        else:
            result.append(not line.startswith(("    ", "\t")))
    return result


def normalized_row(text: str, columns: int) -> str:
    """按确定的列边界还原一行，歧义内容明确报错。"""
    positions = pipe_positions(text)
    if not 2 <= len(positions) <= columns + 1 or text[:positions[0]].strip() or text[positions[-1] + 1:].strip():
        raise ValueError(f"表格列数或闭合竖线不明确，期望 {columns} 列；请核对未转义的 | 或重新导出")
    cells = [re.sub(r"\s*\n+\s*", "<br>", text[left + 1:right].strip())
             for left, right in zip(positions, positions[1:])]
    return "| " + " | ".join(cells) + " |"


def repair_markdown(text: str) -> tuple[str, int]:
    """只替换被拆断的表格行，其余字节对应文本保持不变。"""
    lines = text.splitlines(keepends=True)
    eligible = prose_lines(lines)
    replacements: list[tuple[int, int, str]] = []
    tables = 0
    processed_until = 0
    for index, line in enumerate(lines):
        if index < processed_until or not eligible[index] or not re.fullmatch(r"\|\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)*\|\s*", line):
            continue
        columns = len(pipe_positions(line)) - 1
        start = index - 1
        while start >= 0 and eligible[start] and not lines[start].startswith("|"):
            start -= 1
        if start < 0 or not eligible[start]:
            continue
        if len(pipe_positions("".join(lines[start:index]))) != columns + 1:
            raise ValueError(f"第 {start + 1} 行表头与分隔行列数不一致；请人工核对或重新导出")
        rows: list[tuple[int, int]] = [(start, index)]
        cursor = index + 1
        while cursor < len(lines) and eligible[cursor] and lines[cursor].startswith("|"):
            end = cursor + 1
            while True:
                source = "".join(lines[cursor:end]).rstrip()
                positions = pipe_positions(source)
                closed = len(positions) >= 2 and positions[-1] == len(source) - 1
                following = end
                while following < len(lines) and not lines[following].strip():
                    following += 1
                continuation = following < len(lines) and lines[following].startswith((" ", "\t")) and lines[following].lstrip().startswith("|")
                # GFM 允许已闭合的短行；历史合并信息已丢失时不猜测缺失列位置。
                if closed and (end == len(lines) or lines[end].startswith("|") or
                               (len(positions) == columns + 1 and not lines[end].strip() and not continuation)):
                    break
                if end == len(lines) or lines[end].startswith("|"):
                    raise ValueError(f"第 {cursor + 1} 行表格缺少闭合竖线；需按源版本恢复")
                if not eligible[end]:
                    raise ValueError(f"第 {cursor + 1} 行表格包含代码块等块级内容；需按源版本恢复")
                end += 1
            following = end
            while following < len(lines) and not lines[following].strip():
                following += 1
            if following < len(lines) and lines[following].startswith((" ", "\t")) and lines[following].lstrip().startswith("|"):
                raise ValueError(f"第 {cursor + 1} 行存在额外列边界，无法唯一恢复；请核对未转义的 | 或重新导出")
            rows.append((cursor, end))
            cursor = end
        processed_until = cursor
        changed = False
        for first, end in rows:
            source = "".join(lines[first:end])
            row = normalized_row(source, columns)
            if end - first > 1:
                newline = "\r\n" if source.endswith("\r\n") else "\n" if source.endswith("\n") else ""
                replacements.append((first, end, row + newline))
                changed = True
        tables += int(changed)
    updated = list(lines)
    for first, end, replacement in reversed(replacements):
        updated[first:end] = [replacement]
    return "".join(updated), tables


def analyze_file(path: Path) -> Repair:
    """验证文件并在内存计算变更，不接触附件或网络。"""
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"仅处理普通 Markdown 文件，拒绝符号链接：{path}")
    original = path.read_bytes()
    try:
        updated, tables = repair_markdown(original.decode("utf-8-sig"))
        repeated, repeated_tables = repair_markdown(updated)
        if repeated != updated or repeated_tables:
            raise ValueError("修复结果未通过重复运行零变化检查，拒绝写入")
    except (ValueError, UnicodeError) as error:
        raise ValueError(f"无法修复 {path}：{error}") from error
    content = updated.encode("utf-8")
    if original.startswith(b"\xef\xbb\xbf"):
        content = b"\xef\xbb\xbf" + content
    return Repair(path, original, content, tables)


def write_repair(repair: Repair) -> None:
    """创建不可覆盖的备份，再用同目录临时文件原子替换。"""
    if repair.path.read_bytes() != repair.original:
        raise ValueError(f"分析后文件已变化，拒绝覆盖：{repair.path}")
    mode = stat.S_IMODE(repair.path.stat().st_mode)
    backup = repair.path.with_name(repair.path.name + ".bak")
    with backup.open("xb") as stream:
        os.chmod(backup, mode)
        stream.write(repair.original)
    with tempfile.NamedTemporaryFile(dir=repair.path.parent, delete=False) as stream:
        temporary = Path(stream.name)
        try:
            stream.write(repair.updated)
            stream.flush()
            os.chmod(temporary, mode)
            os.replace(temporary, repair.path)
        finally:
            temporary.unlink(missing_ok=True)


def repair_path(root: Path) -> dict[str, int | list[str]]:
    """先完成整批分析与备份冲突检查，再写入变化文件。"""
    if root.is_symlink() or not root.exists():
        raise ValueError(f"输入必须是现有普通文件或目录，拒绝符号链接：{root}")
    paths = sorted(root.rglob("*.md")) if root.is_dir() else [root]
    if not root.is_dir() and root.suffix != ".md":
        raise ValueError(f"输入文件必须为 .md：{root}")
    repairs = (analyze_file(path) for path in paths)
    changed = [item for item in repairs if item.original != item.updated]
    for item in changed:
        backup = item.path.with_name(item.path.name + ".bak")
        if backup.exists() or backup.is_symlink():
            raise FileExistsError(f"备份已存在，拒绝覆盖；请先核对备份：{backup}")
    for item in changed:
        write_repair(item)
    return {"scanned": len(paths), "changed": len(changed), "tables": sum(item.tables for item in changed),
            "files": [str(item.path) for item in changed]}


def main() -> None:
    """执行单文件或目录修复，错误返回非零且不输出正文。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("path", type=Path, help="已导出的 .md 文件或目录")
    args = parser.parse_args()
    try:
        print(json.dumps(repair_path(args.path.absolute()), ensure_ascii=False))
    except (OSError, ValueError) as error:
        parser.exit(1, json.dumps({"event": "markdown_table_repair_failed", "error": str(error)}, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
