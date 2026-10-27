#!/usr/bin/env python3
"""按 Confluence 原生页面树顺序导出快照及差异，仅使用 Python 标准库。"""
from __future__ import annotations

import argparse
from collections import deque
from concurrent.futures import FIRST_COMPLETED, Future, ThreadPoolExecutor, wait
from datetime import datetime, timezone
from html.parser import HTMLParser
import json
from pathlib import Path
import re
import sys
import time
from typing import TypedDict
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urljoin, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


class Element(TypedDict):
    tag: str
    attrs: dict[str, str]
    children: list[Element]
    text: str


class Node(TypedDict):
    id: str
    title: str
    url: str
    version: int
    last_viewed_at: str | None
    last_modified_at: str | None
    children: list[Node]


class Source(TypedDict):
    base_url: str
    space_key: str
    root_page_id: str
    no_root: str


class Snapshot(TypedDict):
    schema_version: int
    source: Source
    captured_at: str
    nodes: list[Node]


class IndexedNode(TypedDict):
    id: str
    title: str
    url: str
    version: int
    last_viewed_at: str | None
    last_modified_at: str | None
    parent_id: str | None
    position: int


class Modification(TypedDict):
    id: str
    fields: list[str]
    before: IndexedNode
    after: IndexedNode


class Difference(TypedDict):
    schema_version: int
    initial: bool
    previous_captured_at: str | None
    captured_at: str
    source: Source
    added: list[IndexedNode]
    modified: list[Modification]
    deleted: list[IndexedNode]


class TreeHTML(HTMLParser):
    """解析服务端树片段，不执行站点脚本。"""

    def __init__(self, html: str):
        super().__init__(convert_charrefs=True)
        self.root: Element = {"tag": "root", "attrs": {}, "children": [], "text": ""}
        self.stack = [self.root]
        self.feed(html)

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        node: Element = {"tag": tag, "attrs": {k: v or "" for k, v in attrs}, "children": [], "text": ""}
        self.stack[-1]["children"].append(node)
        if tag not in {"input", "meta", "link", "img", "br", "hr", "source", "wbr", "area", "base", "embed", "param", "track", "col"}:
            self.stack.append(node)

    def handle_endtag(self, tag: str) -> None:
        for index in range(len(self.stack) - 1, 0, -1):
            if self.stack[index]["tag"] == tag:
                del self.stack[index:]
                break

    def handle_data(self, data: str) -> None:
        self.stack[-1]["children"].append({"tag": "#text", "attrs": {}, "children": [], "text": data})


def elements(root: Element) -> list[Element]:
    """返回文档顺序的元素。"""
    return [root, *(item for child in root["children"] for item in elements(child))]


def text_content(root: Element) -> str:
    """提取页面标题文本。"""
    return root["text"] + "".join(text_content(child) for child in root["children"])


def object_value(value: object, label: str) -> dict[str, object]:
    """校验外部 JSON 对象。"""
    if not isinstance(value, dict):
        raise ValueError(f"{label} 必须为 JSON 对象")
    return value


def string_value(value: object, label: str) -> str:
    """校验必需字符串。"""
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{label} 必须为非空字符串")
    return value


def timestamp_value(value: object, label: str) -> str:
    """校验含时区的 ISO 8601 时间，保留源时间字符串。"""
    text = string_value(value, label)
    try:
        parsed = datetime.fromisoformat(text.replace("Z", "+00:00"))
    except ValueError:
        raise ValueError(f"{label} 必须为含时区的 ISO 8601 时间") from None
    if parsed.tzinfo is None:
        raise ValueError(f"{label} 必须包含时区")
    return text


class NoRedirect(HTTPRedirectHandler):
    """拒绝重定向，防止登录跳转或 Cookie 跨站泄露。"""

    def redirect_request(self, req: Request, fp: object, code: int, msg: str, headers: object, newurl: str) -> None:
        raise ValueError(f"HTTP {code} 重定向：请检查实例地址、Cookie 和登录状态")


class Client:
    """Confluence 只读 HTTP 接缝。"""

    def __init__(self, base: str, cookie: str):
        self.base = base
        self.cookie = cookie

    def get(self, relative: str) -> str:
        url = urljoin(self.base + "/", relative)
        if urlsplit(url).netloc != urlsplit(self.base).netloc or urlsplit(url).scheme != "https":
            raise ValueError("拒绝向其他实例或非 HTTPS 地址发送 Cookie")
        # 每次调用独立构建 opener，避免并发请求共享可变处理器。
        opener = build_opener(NoRedirect())
        for attempt in range(3):
            try:
                request = Request(url, headers={"Cookie": self.cookie, "Accept": "application/json,text/html"})
                with opener.open(request, timeout=30) as response:
                    return response.read().decode("utf-8")
            except HTTPError as error:
                # 错误正文可能含登录信息，不写入日志。
                if error.code not in {429, 500, 502, 503, 504} or attempt == 2:
                    raise ValueError(f"Confluence 请求失败 status={error.code} url={url}；检查权限、Cookie 或服务状态") from None
                print(f"重试 Confluence 请求 attempt={attempt + 1} status={error.code} url={url}", file=sys.stderr)
            except (URLError, TimeoutError) as error:
                if attempt == 2:
                    reason = error.reason if isinstance(error, URLError) else str(error)
                    raise ValueError(f"Confluence 网络请求失败 url={url} type={type(error).__name__} reason={reason}；检查网络、证书和服务可达性") from None
                print(f"重试 Confluence 请求 attempt={attempt + 1} type={type(error).__name__} url={url}", file=sys.stderr)
            time.sleep(attempt + 1)
        raise RuntimeError("请求重试未正常结束")


def tree_settings(html: str, base: str) -> dict[str, str]:
    """从原生页面树获取请求参数，避免猜测空间或根页面。"""
    trees = [el for el in elements(TreeHTML(html).root) if "plugin_pagetree" in el["attrs"].get("class", "").split()]
    if not trees:
        raise ValueError("未找到 .plugin_pagetree；可能已跳转登录页或站点 DOM 已变化")
    settings = {el["attrs"]["name"]: el["attrs"].get("value", "") for el in elements(trees[0]) if el["tag"] == "input" and "name" in el["attrs"]}
    for key in ("treeRequestId", "treePageId", "rootPageId", "noRoot", "spaceKey"):
        string_value(settings.get(key), f"页面树 {key}")
    if settings["noRoot"] not in {"true", "false"} or not settings["rootPageId"].isdigit() or not settings["treePageId"].isdigit():
        raise ValueError("页面树根标记或页面 ID 格式无效")
    endpoint = urljoin(base + "/", settings["treeRequestId"])
    if urlsplit(endpoint).netloc != urlsplit(base).netloc:
        raise ValueError("页面树接口不属于配置实例")
    return settings


def child_entries(html: str) -> list[tuple[str, str, bool]]:
    """读取一层树节点，保留服务器返回的 position 顺序。"""
    root = TreeHTML(html).root
    lists = [el for el in root["children"] if el["tag"] == "ul" and "plugin_pagetree_children_list" in el["attrs"].get("class", "").split()]
    if len(lists) != 1:
        raise ValueError("树接口未返回唯一节点列表；检查登录状态、权限或接口结构")
    result: list[tuple[str, str, bool]] = []
    for li in lists[0]["children"]:
        if li["tag"] != "li":
            continue
        own = [el for child in li["children"] if "plugin_pagetree_children_container" not in child["attrs"].get("class", "").split() for el in elements(child)]
        spans = [el for el in own if "plugin_pagetree_children_span" in el["attrs"].get("class", "").split()]
        if len(spans) != 1:
            raise ValueError("页面树节点缺少唯一标题元素")
        match = re.fullmatch(r"childrenspan(\d+)-\d+", spans[0]["attrs"].get("id", ""))
        if not match:
            raise ValueError("页面树节点 ID 格式变化")
        links = [el for el in elements(spans[0]) if el["tag"] == "a"]
        if len(links) != 1:
            raise ValueError("页面树节点缺少唯一标题链接")
        title = string_value(text_content(links[0]), "节点 title")
        expandable = any(el["attrs"].get("data-type") == "toggle" for el in own)
        result.append((match.group(1), title, expandable))
    return result


def collect(client: Client, settings: dict[str, str]) -> list[Node]:
    """最多十个节点并行采集，全部成功后按原始顺序组装完整树。"""

    def children(page_id: str) -> list[tuple[str, str, bool]]:
        endpoint = settings["treeRequestId"]
        params = {"hasRoot": "false" if page_id == "0" else "true", "spaceKey": settings["spaceKey"], "treeId": "0", "startDepth": "0", "mobile": "false", "treePageId": settings["treePageId"], "pageId": page_id}
        # 不传 ancestors，避免接口预先展开其他层级。
        html = client.get(endpoint + ("&" if "?" in endpoint else "?") + urlencode(params))
        return child_entries(html)

    def page(identifier: str, title: str, expandable: bool) -> tuple[Node, list[tuple[str, str, bool]]]:
        data = object_value(json.loads(client.get(f"rest/api/content/{identifier}?expand=version")), "页面响应")
        version_data = object_value(data.get("version"), f"page_id={identifier} version")
        version = version_data.get("number")
        if data.get("id") != identifier or data.get("title") != title or type(version) is not int or version < 1:
            raise ValueError(f"页面身份、标题或版本与树不一致 page_id={identifier}；抓取期间可能发生变更，请重新运行")
        last_modified_at = timestamp_value(version_data.get("when"), f"page_id={identifier} version.when")
        last_viewed_at = datetime.now(timezone.utc).isoformat()
        result: Node = {"id": identifier, "title": title, "url": f"{client.base}/pages/viewpage.action?pageId={identifier}", "version": version, "last_viewed_at": last_viewed_at, "last_modified_at": last_modified_at, "children": []}
        return result, children(identifier) if expandable else []

    if settings["noRoot"] == "true":
        roots = children("0")
    else:
        root_id = settings["rootPageId"]
        data = object_value(json.loads(client.get(f"rest/api/content/{root_id}")), "根页面")
        roots = [(root_id, string_value(data.get("title"), "根标题"), True)]
    ready: deque[tuple[str, str, bool]] = deque(roots)
    seen: set[str] = set()
    nodes: dict[str, Node] = {}
    child_ids: dict[str, list[str]] = {}
    pending: dict[Future[tuple[Node, list[tuple[str, str, bool]]]], str] = {}
    executor = ThreadPoolExecutor(max_workers=10)
    try:
        while ready or pending:
            while ready and len(pending) < 10:
                identifier, title, expandable = ready.popleft()
                if identifier in seen:
                    raise ValueError(f"树包含重复节点或循环 page_id={identifier}")
                seen.add(identifier)
                pending[executor.submit(page, identifier, title, expandable)] = identifier
            completed, _ = wait(pending, return_when=FIRST_COMPLETED)
            for future in completed:
                identifier = pending.pop(future)
                result, entries = future.result()
                nodes[identifier] = result
                child_ids[identifier] = [entry[0] for entry in entries]
                ready.extend(entries)
    finally:
        executor.shutdown(wait=True, cancel_futures=True)
    # 顺序只取原始接口结果，不取线程完成顺序；工作线程不等待子任务。
    for identifier, identifiers in child_ids.items():
        nodes[identifier]["children"] = [nodes[child] for child in identifiers]
    return [nodes[entry[0]] for entry in roots]


def flatten(nodes: object, parent: str | None) -> dict[str, IndexedNode]:
    """校验快照并以页面 ID 建立位置索引。"""
    if not isinstance(nodes, list):
        raise ValueError("快照 nodes/children 必须为数组")
    result: dict[str, IndexedNode] = {}
    for position, raw in enumerate(nodes):
        node = object_value(raw, "快照节点")
        identifier = string_value(node.get("id"), "快照节点 id")
        if not identifier.isdigit() or identifier in result:
            raise ValueError(f"快照节点 ID 非法或重复 id={identifier}")
        for field in ("title", "url"):
            string_value(node.get(field), field)
        if type(node.get("version")) is not int or node["version"] < 1:
            raise ValueError(f"快照版本无效 id={identifier}")
        for field in ("last_viewed_at", "last_modified_at"):
            if field not in node:
                raise ValueError(f"快照缺少 {field} id={identifier}")
            if node[field] is not None:
                timestamp_value(node[field], f"id={identifier} {field}")
        descendants = flatten(node.get("children"), identifier)
        if identifier in descendants or result.keys() & descendants.keys():
            raise ValueError("快照包含重复页面 ID")
        result[identifier] = {field: node[field] for field in ("id", "title", "url", "version", "last_viewed_at", "last_modified_at")} | {"parent_id": parent, "position": position}
        result.update(descendants)
    return result


def legacy_nodes(nodes: object) -> list[Node]:
    """转换旧节点字段，缺失历史时间明确记为未知，不推算时间。"""
    if not isinstance(nodes, list):
        raise ValueError("旧快照 nodes/children 必须为数组")
    result: list[Node] = []
    for raw in nodes:
        node = object_value(raw, "旧快照节点")
        result.append({field: node.get(field) for field in ("id", "title", "url", "version")} | {
            "last_viewed_at": None, "last_modified_at": None, "children": legacy_nodes(node.get("children"))})
    return result


def compare(previous: object, current: Snapshot) -> Difference:
    """按稳定 ID 比较增改删，移动和排序作为修改。"""
    new = flatten(current["nodes"], None)
    old: dict[str, IndexedNode] = {}
    if previous is not None:
        baseline = object_value(previous, "上次快照")
        if baseline.get("schema_version") not in {1, 2} or baseline.get("source") != current["source"]:
            raise ValueError("上次快照的版本或实例/树范围不同；请使用独立输出目录")
        string_value(baseline.get("captured_at"), "上次快照 captured_at")
        nodes = legacy_nodes(baseline.get("nodes")) if baseline["schema_version"] == 1 else baseline.get("nodes")
        old = flatten(nodes, None)
    modified: list[Modification] = []
    for key in new:
        if key not in old:
            continue
        fields = [field for field in new[key] if field != "last_viewed_at"
                  and not (field == "last_modified_at" and (new[key][field] is None or old[key][field] is None))
                  and new[key][field] != old[key][field]]
        if fields:
            modified.append({"id": key, "fields": fields, "before": old[key], "after": new[key]})
    return {"schema_version": 2, "initial": previous is None, "previous_captured_at": previous.get("captured_at") if isinstance(previous, dict) else None, "captured_at": current["captured_at"], "source": current["source"], "added": [new[key] for key in new if key not in old], "modified": modified, "deleted": [old[key] for key in old if key not in new]}


def save_snapshot(output: Path, current: Snapshot) -> tuple[Path, Path]:
    """写入独立快照和 diff，最后原子推进成功基线。"""
    latest = output / "latest.json"
    previous = json.loads(latest.read_text()) if latest.exists() else None
    diff = compare(previous, current)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    snapshot_path, diff_path = output / f"{stamp}.tree.json", output / f"{stamp}.diff.json"
    snapshot_text = json.dumps(current, ensure_ascii=False, indent=2) + "\n"
    snapshot_path.write_text(snapshot_text)
    diff_path.write_text(json.dumps(diff, ensure_ascii=False, indent=2) + "\n")
    temporary = output / "latest.json.tmp"
    temporary.write_text(snapshot_text)
    temporary.replace(latest)
    return snapshot_path, diff_path


def main() -> None:
    """读取本地 Cookie 配置并输出树和差异文件。"""
    directory = Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=directory / "config.local.json")
    parser.add_argument("--output-dir", type=Path, default=directory / "output")
    args = parser.parse_args()
    config = object_value(json.loads(args.config.read_text()), "配置")
    base = string_value(config.get("base_url"), "base_url").rstrip("/")
    parsed = urlsplit(base)
    if parsed.scheme != "https" or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("base_url 必须为无凭据、查询和片段的 HTTPS 实例地址")
    cookie = string_value(config.get("cookie"), "cookie")
    if any(ord(char) < 32 or ord(char) == 127 for char in cookie) or any("=" not in pair or not re.fullmatch(r"[!#$%&'*+\-.^_`|~0-9A-Za-z]+", pair.split("=", 1)[0].strip()) for pair in cookie.split(";")):
        raise ValueError("cookie 必须为 name=value; name=value 请求头字符串，不含 Cookie: 前缀或换行")
    page_url = string_value(config.get("page_url"), "page_url")
    page = urlsplit(page_url)
    if page.scheme != "https" or page.netloc != parsed.netloc or not page.path.startswith(parsed.path.rstrip("/") + "/"):
        raise ValueError("page_url 必须属于 base_url 实例")
    args.output_dir.mkdir(parents=True, exist_ok=True)
    lock = args.output_dir / ".export.lock"
    with lock.open("x"):
        try:
            client = Client(base, cookie)
            settings = tree_settings(client.get(page_url), base)
            source: Source = {"base_url": base, "space_key": settings["spaceKey"], "root_page_id": settings["rootPageId"], "no_root": settings["noRoot"]}
            current: Snapshot = {"schema_version": 2, "source": source, "captured_at": datetime.now(timezone.utc).isoformat(), "nodes": collect(client, settings)}
            paths = save_snapshot(args.output_dir, current)
            print(json.dumps({"tree": str(paths[0].resolve()), "diff": str(paths[1].resolve()), "nodes": len(flatten(current["nodes"], None))}, ensure_ascii=False))
        finally:
            lock.unlink()


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, RecursionError) as error:
        print(f"导出失败：{error}", file=sys.stderr)
        sys.exit(1)
