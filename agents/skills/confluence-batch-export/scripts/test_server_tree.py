"""后台树接缝只返回内存结果，复用既有层级规划。"""
import unittest
from unittest.mock import patch

from server_tree import capture


class ServerTreeTest(unittest.TestCase):
    def test_capture_preserves_full_snapshot_and_leaf_plan(self) -> None:
        nodes = [{"id": "1", "title": "父目录", "url": "https://example.com/pages/1", "version": 1,
                  "last_viewed_at": "2026-10-22T00:00:00Z", "last_modified_at": "2026-10-21T00:00:00Z", "children": [
                      {"id": "2", "title": "叶子", "url": "https://example.com/pages/2", "version": 2,
                       "last_viewed_at": "2026-10-22T00:00:01Z", "last_modified_at": "2026-10-21T00:00:00Z", "children": []}]}]
        settings = {"spaceKey": "TEST", "rootPageId": "1", "noRoot": "false"}
        with patch("server_tree.Client") as client, patch("server_tree.tree_settings", return_value=settings), patch("server_tree.collect", return_value=nodes):
            result = capture("https://example.com", "secret", "https://example.com/pages/1")
        self.assertEqual(result["nodes"], nodes)
        self.assertEqual(result["snapshot"]["schema_version"], 2)
        self.assertEqual(result["pages"][0]["path"], "父目录/叶子")
        self.assertEqual(len(result["pages"]), 1)
        self.assertNotIn("cookie", str(result))
        client.assert_called_once_with("https://example.com", "secret")


if __name__ == "__main__":
    unittest.main()
