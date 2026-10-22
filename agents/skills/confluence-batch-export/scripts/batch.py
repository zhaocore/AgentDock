#!/usr/bin/env python3
"""持久化 Confluence 叶子页导出队列，由编码代理调用已有 URL 导出技能。"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import fcntl
import json
from pathlib import Path
import re
import shutil
import sys
from typing import Iterator, Literal, TypedDict, cast
from urllib.parse import urlsplit

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "confluence-page-tree/scripts"))
from export_tree import Snapshot, compare, flatten, legacy_nodes, object_value, string_value


class Page(TypedDict):
    id: str
    title: str
    url: str
    version: int
    path: str


class Job(TypedDict):
    page: Page
    action: Literal["export", "delete"]
    status: Literal["pending", "done"]
    result: str
    error: str


class State(TypedDict):
    schema_version: int
    mode: str
    tree_dir: str
    verified: bool
    verification_error: str
    snapshot: Snapshot
    records: dict[str, Page]
    jobs: list[Job]


def load_json(path: Path) -> object:
    """读取 JSON，保留文件路径便于定位损坏输入。"""
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as error:
        raise ValueError(f"JSON 损坏 path={path}: {error}") from error


def snapshot_value(raw: object) -> Snapshot:
    """复用页面树校验，同时校验批量导出必需的实例信息。"""
    data = object_value(raw, "树快照")
    if data.get("schema_version") not in (1, 2):
        raise ValueError("不支持的树 schema_version")
    source = object_value(data.get("source"), "树 source")
    for key in ("base_url", "space_key", "root_page_id", "no_root"):
        string_value(source.get(key), f"source.{key}")
    string_value(data.get("captured_at"), "captured_at")
    nodes = legacy_nodes(data.get("nodes")) if data["schema_version"] == 1 else data.get("nodes")
    indexed = flatten(nodes, None)
    base = urlsplit(str(source["base_url"]))
    if base.scheme != "https" or not base.netloc or base.username or base.password:
        raise ValueError("树 base_url 必须为无凭据 HTTPS URL")
    for node in indexed.values():
        url = urlsplit(node["url"])
        if (url.scheme, url.netloc) != (base.scheme, base.netloc) or url.username or url.password:
            raise ValueError(f"页面 URL 与树实例不符 id={node['id']}")
    return cast(Snapshot, data | {"nodes": nodes})


def title_name(title: str) -> str:
    """与 URL 导出技能使用相同的安全标题规则。"""
    name = re.sub(r'[<>:"/\\|?*\x00-\x1f\x7f]', "-", title).strip().rstrip(". ")
    if not name:
        raise ValueError("页面标题不能形成安全文件名")
    return "_" + name if re.match(r"^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)", name, re.I) else name


def leaf_pages(snapshot: Snapshot) -> dict[str, Page]:
    """保留祖先层级；同级安全名称冲突时为全部冲突项加 ID。"""
    indexed = flatten(snapshot["nodes"], None)
    parents = {node["parent_id"] for node in indexed.values()}
    groups: dict[tuple[str | None, str], list[str]] = {}
    for key, node in indexed.items():
        groups.setdefault((node["parent_id"], title_name(node["title"]).casefold()), []).append(key)
    names = {key: title_name(node["title"]) + (f" [{key}]" if len(groups[(node["parent_id"], title_name(node["title"]).casefold())]) > 1 else "") for key, node in indexed.items()}
    siblings: set[tuple[str | None, str]] = set()
    for key, name in names.items():
        sibling = (indexed[key]["parent_id"], name.casefold())
        if sibling in siblings:
            raise ValueError(f"标题加 ID 后仍有同级目录冲突 id={key}")
        siblings.add(sibling)
    result: dict[str, Page] = {}
    paths: set[str] = set()
    for key, node in indexed.items():
        if key in parents:
            continue
        chain = [names[key]]
        parent = node["parent_id"]
        while parent is not None:
            chain.insert(0, names[parent])
            parent = indexed[parent]["parent_id"]
        relative = "/".join(chain)
        if relative.casefold() in paths or chain[0].casefold() == ".batch":
            raise ValueError(f"标题目录冲突 id={key} path={relative}")
        paths.add(relative.casefold())
        result[key] = {"id": key, "title": node["title"], "url": node["url"], "version": node["version"], "path": relative}
    return result


def safe_path(root: Path, relative: str) -> Path:
    """禁止绝对路径、父级越界及任意层符号链接。"""
    parts = Path(relative).parts
    if not parts or Path(relative).is_absolute() or ".." in parts:
        raise ValueError(f"输出路径越界 path={relative}")
    if root.is_symlink():
        raise ValueError("导出根目录禁止符号链接")
    current = root
    for part in parts:
        current = current / part
        if current.is_symlink():
            raise ValueError(f"输出路径禁止符号链接 path={current}")
    return current


def page_value(raw: object) -> Page:
    """校验磁盘上的页面记录。"""
    page = object_value(raw, "页面记录")
    for key in ("id", "title", "url", "path"):
        string_value(page.get(key), key)
    if not str(page["id"]).isdigit() or type(page.get("version")) is not int or cast(int, page["version"]) < 1:
        raise ValueError("页面记录 ID/version 非法")
    parts = Path(str(page["path"])).parts
    if not parts or (parts[0].casefold() == ".batch" and parts != (".batch", "relocations", str(page["id"]))):
        raise ValueError("页面路径禁止指向状态目录")
    return cast(Page, page)


def read_state(root: Path) -> State:
    """拒绝损坏状态，不静默重建进度。"""
    raw = object_value(load_json(safe_path(root, ".batch/state.json")), "批量状态")
    if raw.get("schema_version") != 1 or raw.get("mode") not in ("full", "incremental"):
        raise ValueError("批量状态 schema_version/mode 无效")
    snapshot_value(raw.get("snapshot"))
    records = object_value(raw.get("records"), "records")
    for key, record in records.items():
        page = page_value(record)
        if key != page["id"]:
            raise ValueError("records 键与页面 ID 不匹配")
        safe_path(root, page["path"])
    if not isinstance(raw.get("jobs"), list):
        raise ValueError("jobs 必须为数组")
    for item in cast(list[object], raw["jobs"]):
        job = object_value(item, "任务")
        safe_path(root, page_value(job.get("page"))["path"])
        if job.get("action") not in ("export", "delete") or job.get("status") not in ("pending", "done") or not isinstance(job.get("error"), str) or not isinstance(job.get("result"), str):
            raise ValueError("任务状态无效")
    tree_dir = raw.get("tree_dir", str(Path(__file__).resolve().parents[2] / "confluence-page-tree/output"))
    string_value(tree_dir, "tree_dir")
    verified = raw.get("verified", False)
    error = raw.get("verification_error", "")
    if type(verified) is not bool or not isinstance(error, str):
        raise ValueError("目录校准状态无效")
    return cast(State, raw | {"tree_dir": tree_dir, "verified": verified, "verification_error": error})


def save_state(root: Path, state: State) -> None:
    """原子更新检查点，读进度不会遇到半写入 JSON。"""
    temporary = safe_path(root, ".batch/state.tmp")
    temporary.write_text(json.dumps(state, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    temporary.replace(safe_path(root, ".batch/state.json"))


@contextmanager
def locked(root: Path) -> Iterator[None]:
    """进程退出自动释放锁，避免强杀后遗留锁阻断恢复。"""
    if root.is_symlink():
        raise ValueError("导出根目录禁止符号链接")
    safe_path(root, ".batch").mkdir(parents=True, exist_ok=True)
    with safe_path(root, ".batch/lock").open("a") as handle:
        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield


def progress(state: State) -> dict[str, int | str]:
    """返回可直接展示的已完成数量及总量。"""
    done = sum(job["status"] == "done" for job in state["jobs"])
    status = ("complete" if state["verified"] else "verifying") if done == len(state["jobs"]) else "pending"
    return {"completed": done, "total": len(state["jobs"]), "progress": f"{done}/{len(state['jobs'])}", "status": status, "errors": sum(bool(job["error"]) for job in state["jobs"]) + bool(state["verification_error"])}


def start(root: Path, tree_dir: Path, mode: str) -> State:
    """优先续跑冻结队列；新批次才读取 latest 与匹配 diff。"""
    previous = read_state(root) if safe_path(root, ".batch/state.json").exists() else None
    if previous and progress(previous)["status"] != "complete":
        return previous
    current = snapshot_value(load_json(tree_dir / "latest.json"))
    if previous and previous["snapshot"]["source"] != current["source"]:
        raise ValueError("实例/空间/树范围不同，请使用独立输出目录")
    records = previous["records"] if previous else {}
    leaves = leaf_pages(current)
    changed: set[str] = set()
    if mode == "incremental":
        matching = [object_value(load_json(file), "diff") for file in sorted(tree_dir.glob("*.diff.json"))]
        matches = [diff for diff in matching if diff.get("captured_at") == current["captured_at"] and diff.get("source") == current["source"]]
        if len(matches) != 1:
            raise ValueError("最新树缺少唯一配对 diff，不能执行增量")
        diff = compare(previous["snapshot"], current) if previous else matches[0]
        for key in ("added", "modified", "deleted"):
            if not isinstance(diff.get(key), list):
                raise ValueError(f"diff.{key} 必须为数组")
            for item in diff[key]:
                identifier = string_value(object_value(item, "差异节点").get("id"), "diff.id")
                changed.add(identifier)
        indexed = flatten(current["nodes"], None)
        for key in leaves:
            parent = indexed[key]["parent_id"]
            while parent is not None:
                if parent in changed:
                    changed.add(key)
                    break
                parent = indexed[parent]["parent_id"]
    jobs: list[Job] = []
    for key, page in records.items():
        if key not in leaves:
            jobs.append({"page": page, "action": "delete", "status": "pending", "result": "", "error": ""})
    old_leaves = leaf_pages(previous["snapshot"]) if previous else {}
    for key, page in leaves.items():
        if mode == "full" or key in changed or (previous and key not in old_leaves) or (key in records and records[key]["path"] != page["path"]):
            jobs.append({"page": page, "action": "export", "status": "pending", "result": "", "error": ""})
    state: State = {"schema_version": 1, "mode": mode, "tree_dir": str(tree_dir.absolute()), "verified": False, "verification_error": "", "snapshot": current, "records": records, "jobs": jobs}
    save_state(root, state)
    return state


def markdown_info(directory: Path, page: Page) -> tuple[Path, int]:
    """读取既有技能生成的 JSON 标量 frontmatter，不引入 YAML 依赖。"""
    files = list(directory.glob("*.md"))
    if len(files) != 1 or files[0].is_symlink():
        raise ValueError(f"页面目录须有唯一 Markdown path={directory}")
    text = files[0].read_text(encoding="utf-8")
    if not text.startswith("---\n") or "\n---\n" not in text[4:]:
        raise ValueError(f"Markdown 缺少 frontmatter path={files[0]}")
    header = text[4:].split("\n---\n", 1)[0]
    fields = {line.split(":", 1)[0]: line.split(":", 1)[1].strip() for line in header.splitlines() if ":" in line}
    if json.loads(fields.get("confluence_page_id", "null")) != page["id"] or json.loads(fields.get("url", "null")) != page["url"]:
        raise ValueError(f"Markdown 页面身份不匹配 id={page['id']} path={directory}")
    version = json.loads(fields.get("confluence_version", "null"))
    if type(version) is not int or version < 1:
        raise ValueError(f"Markdown 缺少合法版本 path={files[0]}")
    for entry in directory.rglob("*"):
        if entry.is_symlink():
            raise ValueError(f"页面目录禁止符号链接 path={entry}")
    for entry in directory.iterdir():
        if entry != files[0] and entry.name != "assets":
            raise ValueError(f"页面目录包含非导出文件，拒绝覆盖 path={entry}")
    for match in re.finditer(r"\./assets/([^\s)\]>\"']+)", text):
        if not safe_path(directory, "assets/" + match[1]).is_file():
            raise ValueError(f"Markdown 资源缺失 path={directory} asset={match[1]}")
    return files[0], version


def remove_page(root: Path, page: Page) -> None:
    """只删除身份匹配的已登记页面目录，不递归删除祖先。"""
    directory = safe_path(root, page["path"])
    if directory.exists():
        markdown_info(directory, page)
        shutil.rmtree(directory)
    prune_empty(root, directory.parent)


def prune_empty(root: Path, parent: Path) -> None:
    """仅移除空祖先，保持目录树与当前层级一致。"""
    while parent != root and parent.exists() and not any(parent.iterdir()):
        parent.rmdir()
        parent = parent.parent


def finish(root: Path, state: State, index: int, result: str, record: Page | None) -> State:
    """提交单项结果与目录登记，返回新状态。"""
    jobs = [job.copy() for job in state["jobs"]]
    jobs[index] = jobs[index] | {"status": "done", "result": result, "error": ""}
    records = state["records"].copy()
    if record is None:
        records.pop(jobs[index]["page"]["id"], None)
    else:
        records[record["id"]] = record
    updated = state | {"jobs": jobs, "records": records}
    save_state(root, updated)
    return updated


def disk_pages(root: Path, known: dict[str, Page]) -> tuple[dict[str, Page], set[str]]:
    """扫描实际页面位置，不进入附件或状态目录，不删除未知文件。"""
    pages: dict[str, Page] = {}
    directories: set[str] = set()
    pending = [root]
    while pending:
        directory = pending.pop()
        entries = list(directory.iterdir())
        for entry in entries:
            if entry.is_symlink():
                raise ValueError(f"目录校准禁止符号链接 path={entry}")
        markdowns = [entry for entry in entries if entry.is_file() and entry.suffix == ".md"]
        if markdowns:
            match = re.search(r"^confluence_page_id: (.+)$", markdowns[0].read_text(encoding="utf-8").split("\n---\n", 1)[0], re.M)
            identifier = json.loads(match[1]) if match else None
            if not isinstance(identifier, str) or identifier not in known or identifier in pages:
                raise ValueError(f"目录校准发现未知或重复页面 ID path={directory}")
            page = known[identifier]
            _, version = markdown_info(directory, page)
            if directory == root:
                raise ValueError("Markdown 必须位于树节点目录中")
            relative = directory.relative_to(root).as_posix()
            pages[identifier] = page | {"path": relative, "version": version}
            directories.add(relative)
            continue
        for entry in entries:
            if directory == root and entry.name == ".batch":
                continue
            if not entry.is_dir():
                raise ValueError(f"目录校准发现非导出文件 path={entry}")
            directories.add(entry.relative_to(root).as_posix())
            pending.append(entry)
    return pages, directories


def reconcile(root: Path, state: State) -> State:
    """以最新树和实际目录追加修复任务，零差异才提交完成状态。"""
    current = snapshot_value(load_json(Path(state["tree_dir"]) / "latest.json"))
    if current["source"] != state["snapshot"]["source"]:
        raise ValueError("目录校准的实例/空间/树范围变化，请恢复原树输入")
    leaves = leaf_pages(current)
    old_leaves = leaf_pages(state["snapshot"])
    actual, directories = disk_pages(root, leaves | state["records"])
    records = state["records"] | actual
    desired = set(leaves) if state["mode"] == "full" else (set(records) & set(leaves)) | {key for key in leaves if leaves[key] != old_leaves.get(key)}
    jobs: list[Job] = []
    for key, page in records.items():
        if key not in leaves:
            jobs.append({"page": page, "action": "delete", "status": "pending", "result": "", "error": ""})
    for key in leaves:
        if key not in desired:
            continue
        page = leaves[key]
        existing = actual.get(key)
        if existing and existing["version"] > page["version"]:
            raise ValueError(f"目录校准发现本地版本大于最新树 id={key}")
        if existing != page or not (safe_path(root, page["path"]) / (title_name(page["title"]) + ".md")).is_file():
            jobs.append({"page": page, "action": "export", "status": "pending", "result": "", "error": ""})
    updated = state | {"snapshot": current, "records": records, "jobs": state["jobs"] + jobs, "verified": False, "verification_error": ""}
    save_state(root, updated)
    expected = {parent.as_posix() for page in leaves.values() for parent in [Path(page["path"]), *Path(page["path"]).parents] if parent != Path(".")}
    for relative in sorted(directories - expected, key=lambda value: len(Path(value).parts), reverse=True):
        directory = safe_path(root, relative)
        if not any(directory.iterdir()):
            directory.rmdir()
    if jobs:
        return updated
    for relative in sorted(expected, key=lambda value: len(Path(value).parts)):
        safe_path(root, relative).mkdir(parents=True, exist_ok=True)
    checked_pages, checked = disk_pages(root, leaves | records)
    if checked != expected or any(checked_pages.get(key) != leaves[key] for key in desired):
        raise ValueError("目录校准复核仍有差异，请检查导出目录是否被其他进程修改")
    if snapshot_value(load_json(Path(state["tree_dir"]) / "latest.json")) != current:
        raise ValueError("目录校准期间 latest.json 再次更新，请继续 next 重新校准")
    verified = updated | {"verified": True}
    save_state(root, verified)
    return verified


def park_moves(root: Path, state: State) -> State:
    """先暂存待移动页面，支持目录互换及移动过程中断恢复。"""
    for job in state["jobs"]:
        page = job["page"]
        old = state["records"].get(page["id"])
        if job["status"] != "pending" or job["action"] != "export" or old is None or old["path"] == page["path"]:
            continue
        relative = f".batch/relocations/{page['id']}"
        if old["path"] == relative:
            continue
        source, parked = safe_path(root, old["path"]), safe_path(root, relative)
        if source.exists():
            _, version = markdown_info(source, old)
            if version > page["version"]:
                raise ValueError(f"本地版本大于树版本，拒绝移动 id={page['id']}")
            if parked.exists():
                raise FileExistsError(f"移动暂存目录已存在 path={parked}")
            parked.parent.mkdir(parents=True, exist_ok=True)
            source.rename(parked)
        elif not parked.exists():
            continue
        records = state["records"] | {page["id"]: old | {"path": relative}}
        state = state | {"records": records}
        save_state(root, state)
        prune_empty(root, source.parent)
    return state


def next_job(root: Path, state: State) -> dict[str, object]:
    """执行本地删除/同版跳过，返回首个需要调用 URL 技能的页面。"""
    while True:
        state = park_moves(root, state)
        for index, job in enumerate(state["jobs"]):
            if job["status"] == "done":
                continue
            page = job["page"]
            if job["action"] == "delete":
                remove_page(root, page)
                state = finish(root, state, index, "deleted", None)
                continue
            destination = safe_path(root, page["path"])
            old = state["records"].get(page["id"])
            existing = safe_path(root, old["path"]) if old else destination
            if existing.is_dir() and not any(existing.iterdir()):
                existing.rmdir()
            if existing.exists():
                markdown, version = markdown_info(existing, old or page)
                if version > page["version"]:
                    raise ValueError(f"本地版本大于树版本 id={page['id']} local={version} tree={page['version']}，请刷新页面树")
                if version == page["version"] and (old is None or old["url"] == page["url"]):
                    new_name = title_name(page["title"]) + ".md"
                    if markdown.name != new_name:
                        markdown.rename(existing / new_name)
                    if existing != destination:
                        if destination.is_dir() and not any(destination.iterdir()):
                            destination.rmdir()
                        if destination.exists():
                            raise FileExistsError(f"移动目标已存在 path={destination}")
                        destination.parent.mkdir(parents=True, exist_ok=True)
                        existing.rename(destination)
                        prune_empty(root, existing.parent)
                    state = finish(root, state, index, "skipped" if existing == destination else "moved", page)
                    continue
            staging = safe_path(root, f".batch/staging/{page['id']}")
            staging.mkdir(parents=True, exist_ok=True)
            return {"progress": progress(state), "job_index": index, "page": page, "staging_root": str(staging), "error": job["error"]}
        state = reconcile(root, state)
        if progress(state)["status"] == "complete":
            return {"progress": progress(state), "job": None}


def complete(root: Path, state: State, index: int, source: Path) -> State:
    """验证完整暂存输出后，先删旧目录再发布，保留暂存作为中断恢复点。"""
    pending = next((i for i, job in enumerate(state["jobs"]) if job["status"] == "pending"), None)
    if pending != index or state["jobs"][index]["action"] != "export":
        raise ValueError("只能提交当前首个待导出任务")
    page = state["jobs"][index]["page"]
    staging = safe_path(root, f".batch/staging/{page['id']}")
    if source.is_symlink() or source.parent != staging:
        raise ValueError("提交目录必须为当前任务 staging_root 的直接子目录")
    _, version = markdown_info(source, page)
    if version != page["version"]:
        raise ValueError(f"导出版本与树不同 id={page['id']} exported={version} tree={page['version']}，请核对树快照")
    old = state["records"].get(page["id"])
    destination = safe_path(root, page["path"])
    for entry in (old, page):
        if entry and safe_path(root, entry["path"]).exists():
            _, local = markdown_info(safe_path(root, entry["path"]), entry)
            if local > page["version"]:
                raise ValueError("目标版本比待发布版本新，拒绝覆盖")
    publication = safe_path(root, f".batch/publication/{page['id']}")
    if publication.exists():
        shutil.rmtree(publication)
    publication.parent.mkdir(parents=True, exist_ok=True)
    shutil.copytree(source, publication)
    if old:
        remove_page(root, old)
    if destination.exists():
        remove_page(root, page)
    destination.parent.mkdir(parents=True, exist_ok=True)
    # 同文件系统重命名发布，避免中断留下半份目标目录。
    publication.rename(destination)
    updated = finish(root, state, index, "exported", page)
    shutil.rmtree(staging)
    return updated


def main() -> None:
    """提供批次启动、领取、完成、错误记录及只读进度命令。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("start", "next", "complete", "fail", "status"))
    parser.add_argument("--output-dir", type=Path)
    parser.add_argument("--tree-dir", type=Path)
    parser.add_argument("--mode", choices=("full", "incremental"))
    parser.add_argument("--job-index", type=int)
    parser.add_argument("--source", type=Path)
    parser.add_argument("--error")
    args = parser.parse_args()
    skill = Path(__file__).resolve().parents[1]
    root = (args.output_dir or skill / "output").absolute()
    if args.command == "status":
        state = read_state(root)
        current = next((job for job in state["jobs"] if job["status"] == "pending"), None)
        print(json.dumps({"progress": progress(state), "current": current, "verification_error": state["verification_error"]}, ensure_ascii=False))
        return
    with locked(root):
        if args.command == "start":
            state = start(root, args.tree_dir or skill.parent / "confluence-page-tree/output", args.mode or "full")
            result: object = progress(state)
        else:
            state = read_state(root)
            try:
                if args.command == "next":
                    result = next_job(root, state)
                elif args.command == "complete":
                    if args.job_index is None or args.source is None:
                        raise ValueError("complete 需要 --job-index 与 --source")
                    result = progress(complete(root, state, args.job_index, args.source.absolute()))
                else:
                    if not args.error:
                        raise ValueError("fail 需要 --error，禁止包含凭据或正文")
                    raise RuntimeError(args.error)
            except (ValueError, OSError, RuntimeError) as error:
                fresh = read_state(root)
                jobs = [job.copy() for job in fresh["jobs"]]
                pending = next((i for i, job in enumerate(jobs) if job["status"] == "pending"), None)
                if pending is not None:
                    jobs[pending]["error"] = str(error)
                    save_state(root, fresh | {"jobs": jobs})
                else:
                    save_state(root, fresh | {"verified": False, "verification_error": str(error)})
                raise
        print(json.dumps(result, ensure_ascii=False))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, RuntimeError) as error:
        print(json.dumps({"event": "batch_export_failed", "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        sys.exit(1)
