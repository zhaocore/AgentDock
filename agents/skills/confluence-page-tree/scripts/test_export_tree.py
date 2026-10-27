"""验证树解析、快照差异与基线保护。"""
from copy import deepcopy
from datetime import datetime, timezone
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

from export_tree import Client, Node, Snapshot, child_entries, collect, compare, flatten, save_snapshot, tree_settings


def node(identifier: str, title: str, version: int, children: list[Node]) -> Node:
    """生成可变更的页面样本。"""
    return {"id": identifier, "title": title, "url": f"https://example.com/pages/viewpage.action?pageId={identifier}", "version": version, "last_viewed_at": "2026-10-21T09:00:00+00:00", "last_modified_at": "2026-10-20T10:00:00+08:00", "children": children}


def snapshot(nodes: list[Node]) -> Snapshot:
    """生成同一范围的快照。"""
    return {"schema_version": 2, "source": {"base_url": "https://example.com", "space_key": "A", "root_page_id": "0", "no_root": "true"}, "captured_at": "2026-10-21", "nodes": nodes}


class ExportTests(unittest.TestCase):
    """覆盖对外导出与比较契约。"""

    def test_tree_order_nested_titles_and_login_rejection(self) -> None:
        html = '''<ul class="plugin_pagetree_children_list"><li>
        <div><a data-type="toggle" data-page-id="2"></a></div>
        <div><span class="plugin_pagetree_children_span" id="childrenspan2-0"><a>A <span>&amp;</span> B</a></span></div>
        <div class="plugin_pagetree_children_container"><ul><li><span class="plugin_pagetree_children_span" id="childrenspan9-0">nested</span></li></ul></div>
        </li><li><div><span class="plugin_pagetree_children_span" id="childrenspan1-0">  <a>&nbsp;&nbsp;中文 </a>  </span></div></li></ul>'''
        self.assertEqual(child_entries(html), [("2", "A & B", True), ("1", "\u00a0\u00a0中文 ", False)])
        with self.assertRaisesRegex(ValueError, "唯一节点列表"):
            child_entries('<html>Login</html>')
        with self.assertRaisesRegex(ValueError, "plugin_pagetree"):
            tree_settings('<html>Login</html>', 'https://example.com')

    def test_added_modified_deleted_move_and_no_change(self) -> None:
        before = snapshot([node("1", "Root", 1, [node("2", "Old", 2, []), node("3", "Gone", 1, [])])])
        after = snapshot([node("1", "Root", 1, [node("4", "New", 1, [])]), node("2", "Renamed", 3, [])])
        original = deepcopy(before)
        diff = compare(before, after)
        self.assertEqual([entry['id'] for entry in diff['added']], ['4'])
        self.assertEqual([entry['id'] for entry in diff['deleted']], ['3'])
        changed = diff['modified'][0]
        self.assertEqual(changed['id'], '2')
        self.assertEqual(set(changed['fields']), {'title', 'version', 'parent_id', 'position'})
        self.assertEqual(before, original)
        unchanged = compare(before, dict(before, captured_at='later'))
        self.assertEqual([unchanged[key] for key in ('added', 'modified', 'deleted')], [[], [], []])
        self.assertTrue(compare(None, after)['initial'])
        self.assertEqual(len(compare(None, after)['added']), 3)
        with self.assertRaisesRegex(ValueError, '范围不同'):
            compare(before, dict(after, source={}))

    def test_order_and_invalid_snapshot(self) -> None:
        before = snapshot([node('1', 'One', 1, []), node('2', 'Two', 1, [])])
        after = snapshot(list(reversed(before['nodes'])))
        self.assertEqual([item['fields'] for item in compare(before, after)['modified']], [['position'], ['position']])
        with self.assertRaisesRegex(ValueError, '重复'):
            flatten([node('1', 'One', 1, [node('1', 'Again', 1, [])])], None)

    def test_view_time_does_not_mark_page_modified(self) -> None:
        before = snapshot([node('1', 'One', 1, [])])
        after = deepcopy(before)
        after['nodes'][0]['last_viewed_at'] = '2026-10-21T11:00:00Z'
        self.assertEqual(compare(before, after)['modified'], [])
        after['nodes'][0]['last_modified_at'] = '2026-10-21T10:00:00+08:00'
        diff = compare(before, after)
        self.assertEqual(diff['modified'][0]['fields'], ['last_modified_at'])
        self.assertEqual(diff['modified'][0]['after']['last_viewed_at'], '2026-10-21T11:00:00Z')
        after['nodes'][0]['last_modified_at'] = '2026-10-21T10:00:00'
        with self.assertRaisesRegex(ValueError, '时区'):
            compare(before, after)

    def test_legacy_snapshot_keeps_unknown_times_and_detects_updates(self) -> None:
        before = snapshot([node('1', 'One', 1, []), node('2', 'Gone', 1, [])])
        before['schema_version'] = 1
        for entry in before['nodes']:
            del entry['last_viewed_at']
            del entry['last_modified_at']
            entry['modification_count'] = 0
        original = deepcopy(before)
        after = snapshot([node('1', 'One', 1, [])])
        diff = compare(before, after)
        self.assertEqual(diff['schema_version'], 2)
        self.assertEqual(diff['modified'], [])
        self.assertIsNone(diff['deleted'][0]['last_viewed_at'])
        self.assertIsNone(diff['deleted'][0]['last_modified_at'])
        self.assertNotIn('modification_count', diff['deleted'][0])
        after['nodes'][0]['version'] = 2
        changed = compare(before, after)['modified'][0]
        self.assertEqual(changed['fields'], ['version'])
        self.assertIsNone(changed['before']['last_modified_at'])
        self.assertEqual(before, original)

    def test_collect_reads_each_page_time_and_requires_server_modified_time(self) -> None:
        html = '<ul class="plugin_pagetree_children_list">' + ''.join(
            f'<li><span class="plugin_pagetree_children_span" id="childrenspan{i}-0"><a>Page {i}</a></span></li>'
            for i in ('1', '2')) + '</ul>'
        client = Mock(spec=Client)
        client.base = 'https://example.com'
        responses = {
            '1': {'id': '1', 'title': 'Page 1', 'version': {'number': 3, 'when': '2026-10-20T10:00:00+08:00'}},
            '2': {'id': '2', 'title': 'Page 2', 'version': {'number': 1, 'when': '2026-10-19T10:00:00Z'}},
        }
        client.get.side_effect = lambda relative: html if relative.startswith('/tree') else json.dumps(responses[relative.split('/')[-1].split('?')[0]])
        settings = {'treeRequestId': '/tree', 'spaceKey': 'A', 'treePageId': '1', 'noRoot': 'true', 'rootPageId': '0'}
        started = datetime.now(timezone.utc)
        nodes = collect(client, settings)
        finished = datetime.now(timezone.utc)
        self.assertEqual([entry['last_modified_at'] for entry in nodes], ['2026-10-20T10:00:00+08:00', '2026-10-19T10:00:00Z'])
        for entry in nodes:
            self.assertTrue(started <= datetime.fromisoformat(entry['last_viewed_at']) <= finished)
            self.assertNotIn('modification_count', entry)
        responses['1']['version'] = {'number': 3}
        with self.assertRaisesRegex(ValueError, r'page_id=1 version.when'):
            collect(client, settings)

    def test_baseline_advances_only_after_diff_written(self) -> None:
        before = snapshot([node('1', 'One', 1, [])])
        after = snapshot([node('1', 'One', 2, [])])
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            first_tree, first_diff = save_snapshot(output, before)
            baseline = (output / 'latest.json').read_bytes()
            original_write = Path.write_text

            def fail_diff(path: Path, data: str) -> int:
                if path.name.endswith('.diff.json'):
                    raise OSError('disk full')
                return original_write(path, data)

            with patch.object(Path, 'write_text', fail_diff), self.assertRaisesRegex(OSError, 'disk full'):
                save_snapshot(output, after)
            self.assertEqual((output / 'latest.json').read_bytes(), baseline)
            self.assertTrue(first_tree.exists())
            self.assertTrue(json.loads(first_diff.read_text())['initial'])
            tree, diff = save_snapshot(output, after)
            self.assertEqual(json.loads((output / 'latest.json').read_text()), after)
            self.assertEqual(json.loads(diff.read_text())['modified'][0]['after']['version'], 2)
            self.assertNotEqual(tree, first_tree)


if __name__ == '__main__':
    unittest.main()
