"""通过真实临时目录验证队列、目录同步和断点恢复。"""
import copy
import json
import shutil
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import batch


def node(identifier: str, title: str, version: int, children: list[dict]) -> dict:
    return {"id": identifier, "title": title, "url": f"https://confluence.example.com/pages/viewpage.action?pageId={identifier}", "version": version, "children": children, "last_viewed_at": None, "last_modified_at": None}


def tree(nodes: list[dict], stamp: str) -> dict:
    return {"schema_version": 2, "source": {"base_url": "https://confluence.example.com", "space_key": "A", "root_page_id": "1", "no_root": "false"}, "captured_at": stamp, "nodes": nodes}


class BatchTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "output"
        self.trees = Path(self.temp.name) / "trees"
        self.trees.mkdir()
        self.current = tree([node("1", "根", 1, [node("2", "页", 1, []), node("3", "另页", 1, [])])], "t1")
        self.write_tree(self.current, None)
        self.script = Path(batch.__file__).resolve()

    def write_tree(self, current: dict, previous: dict | None) -> None:
        (self.trees / "latest.json").write_text(json.dumps(current))
        diff = batch.compare(previous, current)
        (self.trees / f"{current['captured_at']}.diff.json").write_text(json.dumps(diff))

    def cli(self, *args: str) -> subprocess.CompletedProcess:
        return subprocess.run([sys.executable, str(self.script), *args, "--output-dir", str(self.root)], text=True, capture_output=True)

    def start(self, mode: str) -> dict:
        result = self.cli("start", "--mode", mode, "--tree-dir", str(self.trees))
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def next(self) -> dict:
        result = self.cli("next")
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def staged(self, job: dict) -> Path:
        page = job["page"]
        directory = Path(job["staging_root"]) / batch.title_name(page["title"])
        directory.mkdir(exist_ok=True)
        (directory / "assets").mkdir(exist_ok=True)
        (directory / "assets/a.png").write_bytes(b"image")
        text = "---\n" + "\n".join(f"{key}: {json.dumps(value, ensure_ascii=False)}" for key, value in {"url": page["url"], "title": page["title"], "confluence_page_id": page["id"], "confluence_version": page["version"]}.items()) + "\n---\n\n正文 ![图](./assets/a.png)\n"
        (directory / f"{batch.title_name(page['title'])}.md").write_text(text)
        return directory

    def submit(self, job: dict) -> None:
        source = self.staged(job)
        result = self.cli("complete", "--job-index", str(job["job_index"]), "--source", str(source))
        self.assertEqual(result.returncode, 0, result.stderr)

    def export_all(self) -> None:
        self.start("full")
        while True:
            job = self.next()
            if job.get("job") is None and "page" not in job:
                break
            self.submit(job)

    def test_resume_skip_update_and_progress(self) -> None:
        self.assertEqual(self.start("full")["progress"], "0/2")
        job = self.next()
        failure = self.cli("fail", "--error", "HTTP 403 page=2")
        self.assertNotEqual(failure.returncode, 0)
        self.assertEqual(json.loads(self.cli("status").stdout)["progress"]["errors"], 1)
        newer = copy.deepcopy(self.current)
        newer["captured_at"] = "t2"
        newer["nodes"][0]["children"][0]["version"] = 2
        self.write_tree(newer, self.current)
        self.start("incremental")
        self.assertEqual(self.next()["page"]["version"], 1)
        self.submit(job)
        self.submit(self.next())
        update = self.next()
        self.assertEqual(update["progress"]["progress"], "2/3")
        self.assertEqual(update["page"]["version"], 2)
        old = self.root / "根/页/页.md"
        self.assertIn("confluence_version: 1", old.read_text())
        source = self.staged(update)
        (source / "assets/a.png").unlink()
        failed = self.cli("complete", "--job-index", str(update["job_index"]), "--source", str(source))
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("confluence_version: 1", old.read_text())
        self.submit(update)
        self.assertEqual(self.next()["progress"]["progress"], "3/3")
        self.start("full")
        self.assertEqual(self.next()["progress"]["status"], "complete")
        self.assertTrue(all(job["result"] == "skipped" for job in batch.read_state(self.root)["jobs"]))

    def test_incremental_parent_move_delete_and_accumulated_changes(self) -> None:
        self.export_all()
        intermediate = tree([node("1", "新根", 1, [node("2", "页", 2, []), node("3", "另页", 1, [])])], "t2")
        self.write_tree(intermediate, self.current)
        final = tree([node("1", "新根", 1, [node("2", "页", 2, [])])], "t3")
        self.write_tree(final, intermediate)
        self.assertEqual(self.start("incremental")["total"], 2)
        job = self.next()
        self.assertFalse((self.root / "根/另页").exists())
        self.assertEqual(job["page"]["version"], 2)
        self.submit(job)
        self.next()
        self.assertTrue((self.root / "新根/页/页.md").is_file())
        self.assertFalse((self.root / "根").exists())
        same = tree([node("1", "移动根", 1, [node("2", "页", 2, [])])], "t4")
        self.write_tree(same, final)
        self.start("incremental")
        self.assertEqual(self.next()["progress"]["status"], "complete")
        self.assertTrue((self.root / "移动根/页/页.md").is_file())

    def test_leaf_branch_transitions(self) -> None:
        self.export_all()
        current = tree([node("1", "根", 1, [node("2", "页", 1, [node("4", "子页", 1, [])])])], "t2")
        self.write_tree(current, self.current)
        self.start("incremental")
        job = self.next()
        self.assertFalse((self.root / "根/页/页.md").exists())
        self.submit(job)
        self.next()
        final = tree([node("1", "根", 1, [])], "t3")
        self.write_tree(final, current)
        self.start("incremental")
        job = self.next()
        self.assertEqual(job["page"]["id"], "1")
        self.submit(job)
        self.next()
        self.assertTrue((self.root / "根/根.md").exists())

    def test_conflicts_and_safe_deletion(self) -> None:
        self.export_all()
        local = self.root / "根/页/页.md"
        local.write_text(local.read_text().replace("confluence_version: 1", "confluence_version: 9"))
        self.start("full")
        failure = self.cli("next")
        self.assertNotEqual(failure.returncode, 0)
        self.assertIn("本地版本大于树版本", failure.stderr)
        local.write_text(local.read_text().replace("confluence_version: 9", "confluence_version: 1"))
        self.next()
        unrelated = self.root / "根/页/keep.txt"
        unrelated.write_text("私有笔记")
        final = tree([node("1", "根", 1, [node("3", "另页", 1, [])])], "t2")
        self.write_tree(final, self.current)
        self.start("incremental")
        self.assertNotEqual(self.cli("next").returncode, 0)
        self.assertTrue(unrelated.exists())
        unrelated.unlink()
        (self.root / "根/页/assets/unsafe").symlink_to(self.trees, target_is_directory=True)
        self.assertNotEqual(self.cli("next").returncode, 0)
        self.assertTrue(local.exists())
        with self.assertRaises(ValueError):
            batch.safe_path(self.root, "../trees")

    def test_initial_incremental_and_name_collision(self) -> None:
        other = tree([node("1", "根", 1, [node("2", "a/b", 1, []), node("3", "a:b", 1, [])])], "t2")
        self.write_tree(other, self.current)
        self.assertEqual(self.start("incremental")["total"], 2)
        job = self.next()
        self.assertEqual(job["page"]["path"], "根/a-b [2]")
        self.submit(job)
        self.submit(self.next())
        self.next()
        self.assertTrue((self.root / "根/a-b [2]/a-b.md").exists())
        self.assertTrue((self.root / "根/a-b [3]/a-b.md").exists())

    def test_crash_after_publication_and_corrupt_state(self) -> None:
        self.start("full")
        job = self.next()
        source = self.staged(job)
        destination = self.root / job["page"]["path"]
        destination.parent.mkdir(parents=True)
        source.rename(destination)
        following = self.next()
        self.assertEqual(following["progress"]["completed"], 1)
        self.assertEqual(following["page"]["id"], "3")
        (self.root / ".batch/state.json").write_text("{")
        self.assertNotEqual(self.cli("start").returncode, 0)
        self.assertEqual((self.root / ".batch/state.json").read_text(), "{")

    def test_existing_url_skill_export_and_publish(self) -> None:
        self.assertIsNotNone(shutil.which("bun"), "验收需要已有 URL 技能使用的 Bun")
        self.start("full")
        job = self.next()
        page = job["page"]
        response = {"success": True, "data": {"id": page["id"], "title": page["title"], "version": {"number": page["version"]}, "_links": {"base": "https://confluence.example.com"}, "body": {"view": {"value": "<h1>真实转换</h1><p>正文</p>"}}}}
        source = Path(self.temp.name) / "page.json"
        source.write_text(json.dumps(response))
        attachments = Path(self.temp.name) / "attachments"
        attachments.mkdir()
        exporter = self.script.parents[2] / "url-to-markdown/scripts/confluence.ts"
        result = subprocess.run(["bun", str(exporter), "export", str(source), "--url", page["url"], "--attachments-dir", str(attachments), "--output-dir", job["staging_root"]], text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        result = self.cli("complete", "--job-index", str(job["job_index"]), "--source", str(Path(result.stdout.strip()).parent))
        self.assertEqual(result.returncode, 0, result.stderr)
        markdown = self.root / page["path"] / f"{batch.title_name(page['title'])}.md"
        self.assertIn("真实转换", markdown.read_text())
        self.assertEqual(json.loads(self.cli("status").stdout)["progress"]["completed"], 1)

    def test_final_reconcile_latest_tree_before_complete(self) -> None:
        self.start("full")
        self.submit(self.next())
        self.submit(self.next())
        self.assertEqual(json.loads(self.cli("status").stdout)["progress"]["status"], "verifying")
        latest = tree([node("1", "最新根", 1, [node("2", "最新页", 1, []), node("4", "新增", 1, [])])], "t2")
        self.write_tree(latest, self.current)
        job = self.next()
        self.assertEqual(job["page"]["id"], "4")
        self.assertFalse((self.root / "根").exists())
        self.assertTrue((self.root / "最新根/最新页/最新页.md").exists())
        self.submit(job)
        done = self.next()
        self.assertEqual(done["progress"]["status"], "complete")
        self.assertEqual(done["progress"]["progress"], "5/5")
        state = batch.read_state(self.root)
        self.assertEqual(state["snapshot"]["captured_at"], "t2")
        self.assertEqual(set(state["records"]), {"2", "4"})

    def test_final_reconcile_actual_disk_and_missing_directory(self) -> None:
        self.export_all()
        relocated = self.root / "错误父目录/错误页"
        relocated.parent.mkdir()
        (self.root / "根/页").rename(relocated)
        shutil.rmtree(self.root / "根/另页")
        (self.root / "多余/空目录").mkdir(parents=True)
        job = self.next()
        self.assertEqual(job["page"]["id"], "3")
        self.assertTrue((self.root / "根/页/页.md").exists())
        self.submit(job)
        self.assertEqual(self.next()["progress"]["status"], "complete")
        self.assertFalse((self.root / "错误父目录").exists())
        self.assertFalse((self.root / "多余").exists())

    def test_incremental_final_skeleton_preserves_download_scope(self) -> None:
        latest = copy.deepcopy(self.current)
        latest["captured_at"] = "t2"
        latest["nodes"][0]["children"].append(node("4", "新增", 1, []))
        self.write_tree(latest, self.current)
        self.assertEqual(self.start("incremental")["total"], 1)
        job = self.next()
        self.assertEqual(job["page"]["id"], "4")
        self.submit(job)
        self.assertEqual(self.next()["progress"]["progress"], "1/1")
        self.assertTrue((self.root / "根/页").is_dir())
        self.assertFalse(any((self.root / "根/页").iterdir()))
        self.assertTrue((self.root / "根/另页").is_dir())
        self.start("full")
        self.assertEqual(self.next()["page"]["id"], "2")

    def test_final_reconcile_failure_keeps_verifying_and_resumes(self) -> None:
        self.start("full")
        self.submit(self.next())
        self.submit(self.next())
        unrelated = self.root / "保留.txt"
        unrelated.write_text("不能删除")
        failed = self.cli("next")
        self.assertNotEqual(failed.returncode, 0)
        status = json.loads(self.cli("status").stdout)
        self.assertEqual(status["progress"]["status"], "verifying")
        self.assertEqual(status["progress"]["errors"], 1)
        self.assertIn("非导出文件", status["verification_error"])
        self.assertEqual(self.start("incremental")["status"], "verifying")
        self.assertTrue(unrelated.exists())
        unrelated.unlink()
        self.assertEqual(self.next()["progress"]["status"], "complete")
        self.assertEqual(json.loads(self.cli("status").stdout)["progress"]["errors"], 0)

    def test_final_reconcile_swapped_directories(self) -> None:
        self.export_all()
        latest = tree([node("1", "根", 1, [node("2", "另页", 1, []), node("3", "页", 1, [])])], "t2")
        self.write_tree(latest, self.current)
        self.assertEqual(self.next()["progress"]["status"], "complete")
        self.assertIn('confluence_page_id: "2"', (self.root / "根/另页/另页.md").read_text())
        self.assertIn('confluence_page_id: "3"', (self.root / "根/页/页.md").read_text())

    def test_relocation_checkpoint_interruption_resumes(self) -> None:
        self.export_all()
        latest = tree([node("1", "根", 1, [node("2", "另页", 1, []), node("3", "页", 1, [])])], "t2")
        self.write_tree(latest, self.current)
        self.start("full")
        parked = self.root / ".batch/relocations/2"
        parked.parent.mkdir()
        (self.root / "根/页").rename(parked)
        self.assertEqual(self.next()["progress"]["status"], "complete")
        self.assertIn('confluence_page_id: "2"', (self.root / "根/另页/另页.md").read_text())
        self.assertFalse(parked.exists())

    def test_final_reconcile_empty_branch_becomes_leaf(self) -> None:
        initial = copy.deepcopy(self.current)
        initial["captured_at"] = "t2"
        self.write_tree(initial, self.current)
        self.assertEqual(self.start("incremental")["total"], 0)
        self.assertEqual(self.next()["progress"]["status"], "complete")
        latest = tree([node("1", "根", 1, [])], "t3")
        self.write_tree(latest, initial)
        job = self.next()
        self.assertEqual(job["page"]["id"], "1")
        self.submit(job)
        self.assertEqual(self.next()["progress"]["status"], "complete")
        self.assertTrue((self.root / "根/根.md").exists())

    def test_missing_diff_and_scope_change(self) -> None:
        for file in self.trees.glob("*.diff.json"):
            file.unlink()
        result = self.cli("start", "--mode", "incremental", "--tree-dir", str(self.trees))
        self.assertNotEqual(result.returncode, 0)
        self.write_tree(self.current, None)
        self.export_all()
        other = copy.deepcopy(self.current)
        other["source"]["space_key"] = "B"
        (self.trees / "latest.json").write_text(json.dumps(other))
        self.assertNotEqual(self.cli("start", "--tree-dir", str(self.trees)).returncode, 0)


if __name__ == "__main__":
    unittest.main()
