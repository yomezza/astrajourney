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
    def test_colony_projects_have_go_contract(self):
        data = self.get('/api/planet/projects')
        self.assertIsInstance(data['projects'], list)
        self.assertIsInstance(data['refunds'], list)
        self.assertGreaterEqual(len(data['projects']), 2)
        project_id = data['projects'][0]['id']
        detail = self.get('/api/planet/project?id=' + str(project_id))
        self.assertEqual(detail['project']['id'], project_id)
        self.assertIsInstance(detail['contributions'], list)
        self.assertIsInstance(detail['events'], list)

    def test_notifications_can_be_deleted_without_reappearing(self):
        from mock import data
        saved = [dict(x) for x in data.NOTICES]
        try:
            notices = self.get('/api/notifications')
            self.assertNotIn('Новые торговые предложения на маяке', [x['message'] for x in notices['items']])
            if notices['items']:
                identifier = notices['items'][0]['id']
                self.assertEqual(notices['count'], len(notices['items']))
                req = Request(self.origin + '/api/notifications',
                    data=json.dumps({'action':'delete','ids':[identifier]}).encode(),
                    headers={'Content-Type':'application/json'}, method='POST')
                with urlopen(req, timeout=3) as res:
                    self.assertTrue(json.load(res)['ok'])
                after = self.get('/api/notifications')
                self.assertNotIn(identifier, [x['id'] for x in after['items']])
                self.assertNotIn(identifier, [x['id'] for x in self.get('/api/notifications')['items']])
        finally:
            data.NOTICES[:] = saved

    def test_version_endpoint_and_no_browser_cache(self):
        with urlopen(self.origin+'/demo/version') as response:
            status=json.load(response)
        self.assertEqual(status['chat_mode'], 'one-window')
        self.assertFalse(status['module_window_for_chat'])
        self.assertIn('commit', status)
        with urlopen(self.origin+'/mock/ui-overlay.js') as response:
            self.assertIn('no-store', response.headers.get('Cache-Control',''))
            source=response.read().decode('utf-8')
        self.assertNotIn("Modules.create('chat-v13-mock'",source)

    def test_nonmutating_mock_action(self):
        req=Request(self.origin+'/api/workbench',data=b'{"action":"pause_research","ship":13}',headers={'Content-Type':'application/json'},method='POST')
        with urlopen(req,timeout=3) as res:self.assertTrue(json.load(res)['ok'])

if __name__=='__main__':unittest.main()
