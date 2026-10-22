#!/usr/bin/env python3
"""后台树接缝：仅用内存和标准输入输出，不创建本地文档。"""
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from batch import leaf_pages, snapshot_value
from export_tree import Client, collect, tree_settings


def capture(base: str, cookie: str, url: str) -> dict[str, object]:
    """复用原树采集、快照校验和层级叶子规划。"""
    client = Client(base, cookie)
    settings = tree_settings(client.get(url), base)
    snapshot = snapshot_value({
        "schema_version": 2,
        "source": {"base_url": base, "space_key": settings["spaceKey"], "root_page_id": settings["rootPageId"], "no_root": settings["noRoot"]},
        "captured_at": datetime.now(timezone.utc).isoformat(),
        "nodes": collect(client, settings),
    })
    return {"snapshot": snapshot, "nodes": snapshot["nodes"], "pages": list(leaf_pages(snapshot).values())}


if __name__ == "__main__":
    config = json.load(sys.stdin)
    try:
        print(json.dumps(capture(config["base_url"], config["cookie"], config["url"]), ensure_ascii=False))
    except (ValueError, OSError, RecursionError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
