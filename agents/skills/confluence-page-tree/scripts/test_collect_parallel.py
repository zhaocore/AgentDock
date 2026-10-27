"""本机 HTTP 验证十节点并发、目录顺序及失败传播，不访问真实文档。"""
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from threading import Barrier, Event, Lock, Thread
from typing import Iterator
import unittest
from urllib.error import HTTPError
from urllib.parse import parse_qs, urljoin, urlsplit
from urllib.request import urlopen

from export_tree import Client, collect


class FixtureClient(Client):
    """仅将合成请求发送到本机 HTTP 测试服务。"""

    def __init__(self, address: str):
        super().__init__('https://example.com', 'synthetic')
        self.address = address

    def get(self, relative: str) -> str:
        with urlopen(urljoin(self.address + "/", relative), timeout=10) as response:
            return response.read().decode('utf-8')


@contextmanager
def tree_service(children: dict[str, list[str]], failures: set[str], gate: Barrier | None) -> Iterator[tuple[FixtureClient, list[int], list[str]]]:
    """提供有序目录及逐页元数据，记录真实请求并发数和完成顺序。"""
    lock = Lock()
    active: list[int] = [0]
    peaks: list[int] = [0]
    finished: list[str] = []
    release_first = Event()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, format: str, *args: object) -> None:
            """测试服务不输出请求日志。"""

        def do_GET(self) -> None:
            request = urlsplit(self.path)
            if request.path == '/tree':
                page_id = parse_qs(request.query)['pageId'][0]
                entries: list[str] = []
                for identifier in children.get(page_id, []):
                    toggle = '<a data-type="toggle"></a>' if identifier in children else ''
                    entries.append(f'<li>{toggle}<span class="plugin_pagetree_children_span" id="childrenspan{identifier}-0"><a>Page {identifier}</a></span></li>')
                data = ('<ul class="plugin_pagetree_children_list">' + ''.join(entries) + '</ul>').encode()
                self.send_response(200)
                self.end_headers()
                self.wfile.write(data)
                return
            identifier = request.path.rsplit('/', 1)[-1]
            if identifier in failures:
                self.send_response(500)
                self.end_headers()
                return
            with lock:
                active[0] += 1
                peaks[0] = max(peaks[0], active[0])
            try:
                if gate is not None and int(identifier) <= 10:
                    gate.wait(timeout=8)
                    if identifier == '1':
                        if not release_first.wait(timeout=8):
                            raise TimeoutError('并发请求没有完成')
                data = json.dumps({'id': identifier, 'title': f'Page {identifier}', 'version': {'number': 1, 'when': '2026-10-22T00:00:00Z'}}).encode()
                with lock:
                    finished.append(identifier)
                if identifier == '10':
                    release_first.set()
                self.send_response(200)
                self.end_headers()
                self.wfile.write(data)
            finally:
                with lock:
                    active[0] -= 1

    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    # 允许十个客户端同时建连，避免默认小队列干扰并发验证。
    server.socket.listen(32)
    thread = Thread(target=server.serve_forever)
    thread.start()
    try:
        yield FixtureClient(f'http://127.0.0.1:{server.server_port}'), peaks, finished
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


def settings() -> dict[str, str]:
    """返回只用于本机合成页面的树参数。"""
    return {'treeRequestId': '/tree', 'spaceKey': 'TEST', 'treePageId': '1', 'noRoot': 'true', 'rootPageId': '0'}


class ParallelCollectTests(unittest.TestCase):
    def test_ten_in_flight_and_original_order(self) -> None:
        identifiers = [str(i) for i in range(1, 25)]
        with tree_service({'0': identifiers}, set(), Barrier(10)) as (client, peaks, finished):
            nodes = collect(client, settings())
        self.assertEqual(peaks[0], 10)
        self.assertLess(finished.index('10'), finished.index('1'))
        self.assertEqual([node['id'] for node in nodes], identifiers)

    def test_deep_branches_and_root_keep_hierarchy(self) -> None:
        children = {'0': ['1', '99'], **{str(i): [str(i+1)] for i in range(1, 35)}}
        with tree_service(children, set(), None) as (client, _, _):
            nodes = collect(client, settings())
            self.assertEqual([node['id'] for node in nodes], ['1', '99'])
            current = nodes[0]
            for identifier in range(1, 36):
                self.assertEqual(current['id'], str(identifier))
                if identifier < 35:
                    current = current['children'][0]
            rooted = dict(settings(), noRoot='false', rootPageId='1')
            self.assertEqual(collect(client, rooted)[0]['id'], '1')

    def test_duplicate_cycle_and_request_failure_do_not_return_partial_tree(self) -> None:
        for children in ({'0': ['1', '2'], '1': ['2']}, {'0': ['1'], '1': ['1']}):
            with tree_service(children, set(), None) as (client, _, _):
                with self.assertRaisesRegex(ValueError, '重复节点或循环'):
                    collect(client, settings())
        with tree_service({'0': ['1', '2']}, {'2'}, None) as (client, _, _):
            with self.assertRaises(HTTPError):
                collect(client, settings())
