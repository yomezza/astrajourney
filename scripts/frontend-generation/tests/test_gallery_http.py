import sys,threading,unittest,json
from pathlib import Path
from urllib.request import urlopen
from http.server import ThreadingHTTPServer
BASE=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(BASE))
from server import DemoHandler
class Handler(DemoHandler):
 def log_message(self,*args):pass
class GalleryContract(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
  cls.http=ThreadingHTTPServer(('127.0.0.1',0),Handler)
  cls.thread=threading.Thread(target=cls.http.serve_forever,daemon=True);cls.thread.start()
  cls.origin='http://127.0.0.1:'+str(cls.http.server_port)
 @classmethod
 def tearDownClass(cls): cls.http.shutdown();cls.http.server_close();cls.thread.join(timeout=1)
 def test_gallery_and_svg(self):
  with urlopen(self.origin+'/demo/assets/') as response:
   html=response.read().decode();self.assertIn('реестр оригинальной графики',html)
  with urlopen(self.origin+'/pilgrim.svg') as response:
   svg=response.read().decode();self.assertIn('Pilgrim hologram',svg);self.assertIn('<animate',svg)
  with urlopen(self.origin+'/object-star-g.svg') as response:
   self.assertIn('star-g hologram',response.read().decode())
  with urlopen(self.origin+'/demo/assets.json') as response:
   data=json.load(response);self.assertEqual(len(data['assets']['ships']),15)
 def test_startup_missing_frontend_is_informative(self):
  with urlopen(self.origin+'/') as response:
   html=response.read().decode();self.assertIn('prepare.py',html)

if __name__=='__main__':unittest.main()
