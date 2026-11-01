import sys, json, threading, unittest
from http.server import ThreadingHTTPServer
from pathlib import Path
from urllib.request import urlopen, Request
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from server import DemoHandler

class Handler(DemoHandler):
    def log_message(self,*args):pass

class HttpContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.http=ThreadingHTTPServer(('127.0.0.1',0),Handler)
        cls.thread=threading.Thread(target=cls.http.serve_forever,daemon=True)
        cls.thread.start()
        cls.origin='http://127.0.0.1:'+str(cls.http.server_port)
    @classmethod
    def tearDownClass(cls):
        cls.http.shutdown();cls.http.server_close();cls.thread.join(timeout=1)
    def get(self,path):
        with urlopen(self.origin+path,timeout=3) as res:
            return json.load(res)
    def test_all_snapshot_api(self):
        for path in ['state','profile','expedition','fleet','gameplay','workbench','notifications']:
            with self.subTest(path=path):self.assertIsNotNone(self.get('/api/'+path))
    def test_guide_read(self):
        self.assertEqual(len(self.get('/api/galaxy')['stars']),80)
        self.assertEqual(self.get('/api/system?id=2')['star']['id'],2)
        self.assertIn('path',self.get('/api/estimate?to=15'))
    def test_nonmutating_mock_action(self):
        req=Request(self.origin+'/api/workbench',data=b'{"action":"pause_research","ship":13}',headers={'Content-Type':'application/json'},method='POST')
        with urlopen(req,timeout=3) as res:self.assertTrue(json.load(res)['ok'])

if __name__=='__main__':unittest.main()
