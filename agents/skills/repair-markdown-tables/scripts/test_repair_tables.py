"""验证离线表格修复、文件保护及批量写入边界。"""

from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from repair_tables import repair_markdown, repair_path
from inspect_tables import inspect_directory


BROKEN = """前文

| 
ID

 | 

Rule

 | 

Remark

 |
| --- | --- | --- |
| 1 | 第一段

第二段 `a\\|b` |  
 |
| 2 | [文档](./assets/spec.pdf) | 正常 |
|  
 |  
 |  
 |

后文
"""
FIXED = """前文

| ID | Rule | Remark |
| --- | --- | --- |
| 1 | 第一段<br>第二段 `a\\|b` |  |
| 2 | [文档](./assets/spec.pdf) | 正常 |
|  |  |  |

后文
"""


class RepairTablesTest(unittest.TestCase):
    """使用真实临时文件验证批量修复，不依赖网络。"""

    def test_multiline_cells_and_idempotency(self) -> None:
        """断行表头与空备注恢复，正常行及转义保留。"""
        self.assertEqual(repair_markdown(BROKEN), (FIXED, 1))
        self.assertEqual(repair_markdown(FIXED), (FIXED, 0))

    def test_closed_short_row_does_not_block_later_repairs(self) -> None:
        """合并信息丢失后的已闭合短行保持原值，不误报缺少竖线。"""
        source = "| 问题 | 描述 | 方案 |\n| --- | --- | --- |\n| 音频 | 404 | 解码 |\n| 再次播放失败 | 不清除资源 |\n| 帧数 | \\- |  \n |\n"
        expected = source.replace("| 帧数 | \\- |  \n |", "| 帧数 | \\- |  |")
        self.assertEqual(repair_markdown(source), (expected, 1))
        self.assertEqual(repair_markdown(expected), (expected, 0))

    def test_separator_like_data_row_is_not_another_table(self) -> None:
        """表格正文中的短横线单元格不能在二次运行时误识别为新表头。"""
        source = "| A | B |\n| --- | --- |\n| \n---\n |\n| C | D |\n"
        expected = "| A | B |\n| --- | --- |\n| --- |\n| C | D |\n"
        self.assertEqual(repair_markdown(source), (expected, 1))
        self.assertEqual(repair_markdown(expected), (expected, 0))

    def test_last_cell_closed_on_continuation_line(self) -> None:
        """累计列数够但末列尚未结束时继续读取，不能将中途换行误当行尾。"""
        source = "| A | B |\n| --- | --- |\n| X | 第一段\n\n第二段\n\n |\n\n正文\n"
        expected = "| A | B |\n| --- | --- |\n| X | 第一段<br>第二段 |\n\n正文\n"
        self.assertEqual(repair_markdown(source), (expected, 1))
        self.assertEqual(repair_markdown(expected), (expected, 0))

    def test_html_table_content_is_not_markdown_table(self) -> None:
        """HTML 复杂表格内的代码竖线不能触发 Markdown 表格修复。"""
        source = "<table>\n<tr><td><pre>\n| A |\n| --- |\n| X | Y |\n</pre></td></tr>\n</table>\n"
        self.assertEqual(repair_markdown(source), (source, 0))
        fenced = "```html\n<table>\n```\n\n"
        self.assertEqual(repair_markdown(fenced + BROKEN), (fenced + FIXED, 1))

    def test_inspection_collects_errors_without_writing(self) -> None:
        """预检完整列出成功与失败，不修改正文或创建备份。"""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "safe.md"
            source.write_text(BROKEN, encoding="utf-8")
            (root / "bad.md").write_text("| A | B |\n| --- | --- |\n| X | Y | Z |\n", encoding="utf-8")
            report = inspect_directory(root)
            self.assertEqual(report.scanned, 2)
            self.assertEqual([item.path for item in report.planned], ["safe.md"])
            self.assertEqual([item.path for item in report.unresolved], ["bad.md"])
            self.assertEqual(source.read_text(encoding="utf-8"), BROKEN)
            self.assertEqual(list(root.glob("*.bak")), [])

    def test_protected_blocks_and_regular_content(self) -> None:
        """元数据、两类围栏、缩进代码与普通正文逐字保留。"""
        protected = ("---\nexample: |\n  | A | B |\n  | --- | --- |\n---\n\n"
                     + "```markdown\n" + BROKEN + "```\n\n"
                     + "~~~\n" + BROKEN + "~~~\n\n"
                     + "".join("    " + line for line in BROKEN.splitlines(keepends=True))
                     + '\nexample:\n{"command": "sum(a|b)"}\n\n![图](./assets/a.png)\n')
        self.assertEqual(repair_markdown(protected + BROKEN), (protected + FIXED, 1))

    def test_recursive_cli_backup_and_second_run(self) -> None:
        """CLI 递归修复并保留原始备份、权限与附件，重复执行不写入。"""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            nested = root / "中文 空格"
            nested.mkdir()
            source = nested / "rules.md"
            source.write_text(BROKEN, encoding="utf-8")
            source.chmod(0o600)
            asset = nested / "asset.png"
            asset.write_bytes(b"image-content")
            normal = root / "normal.md"
            normal.write_text(FIXED, encoding="utf-8")
            subprocess.run([sys.executable, str(Path(__file__).with_name("repair_tables.py")), str(root)],
                           check=True, capture_output=True, text=True)
            self.assertEqual(source.read_text(encoding="utf-8"), FIXED)
            self.assertEqual(source.with_name("rules.md.bak").read_text(encoding="utf-8"), BROKEN)
            self.assertEqual(source.stat().st_mode & 0o777, 0o600)
            self.assertFalse(root.joinpath("normal.md.bak").exists())
            self.assertEqual(asset.read_bytes(), b"image-content")
            before = source.stat().st_mtime_ns
            self.assertEqual(repair_path(root), {"scanned": 2, "changed": 0, "tables": 0, "files": []})
            self.assertEqual(source.stat().st_mtime_ns, before)

    def test_bom_and_crlf(self) -> None:
        """保留原有 BOM 和 CRLF，不改写无关正文。"""
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "rules.md"
            original = b"\xef\xbb\xbf" + BROKEN.replace("\n", "\r\n").encode()
            source.write_bytes(original)
            self.assertEqual(repair_path(source)["changed"], 1)
            self.assertEqual(source.read_bytes(), b"\xef\xbb\xbf" + FIXED.replace("\n", "\r\n").encode())
            self.assertEqual(source.with_name("rules.md.bak").read_bytes(), original)

    def test_ambiguous_columns_and_missing_delimiters_abort_batch(self) -> None:
        """歧义与不完整表格导致整批预检失败，不写前面的有效文件。"""
        for broken in [BROKEN.replace("a\\|b", "a|b"), BROKEN.replace("第二段 `a\\|b` |  \n |", "第二段")]:
            with self.subTest(broken=broken), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                first = root / "a.md"
                first.write_text(BROKEN, encoding="utf-8")
                last = root / "z.md"
                last.write_text(broken, encoding="utf-8")
                with self.assertRaisesRegex(ValueError, "无法修复"):
                    repair_path(root)
                self.assertEqual(first.read_text(encoding="utf-8"), BROKEN)
                self.assertEqual(list(root.glob("*.bak")), [])

    def test_existing_backup_and_symlink_rejected(self) -> None:
        """备份不可覆盖，文件符号链接不能越过输入范围。"""
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "rules.md"
            source.write_text(BROKEN, encoding="utf-8")
            backup = source.with_name("rules.md.bak")
            backup.write_text("已有备份", encoding="utf-8")
            with self.assertRaisesRegex(FileExistsError, "备份已存在"):
                repair_path(root)
            self.assertEqual(source.read_text(encoding="utf-8"), BROKEN)
            self.assertEqual(backup.read_text(encoding="utf-8"), "已有备份")
            link = root / "link.md"
            link.symlink_to(source)
            with self.assertRaisesRegex(ValueError, "符号链接"):
                repair_path(link)


if __name__ == "__main__":
    unittest.main()
