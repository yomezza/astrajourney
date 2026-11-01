import hashlib,json,sys,unittest
from pathlib import Path
BASE=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(BASE))
from check_embedded_assets import SHAS
from mock.data import FLEET_CLASSES,SHIP,OTHER_SHIP

class AssetIntegration(unittest.TestCase):
 def test_all_svg_match_upstream(self):
  self.assertEqual(len(SHAS),33)
  for name,expected in SHAS.items():
   with self.subTest(name=name):
    path=BASE/'embedded-original-assets'/name
    self.assertTrue(path.is_file())
    b=path.read_bytes()
    self.assertEqual(hashlib.sha1(b'blob '+str(len(b)).encode()+b'\0'+b).hexdigest(),expected)
    self.assertIn(b'<svg',b)
    if name!='favicon.svg':self.assertIn(b'<animate ',b)
 def test_mock_models_reference_actual_svg(self):
  for model in FLEET_CLASSES:
   with self.subTest(model=model['id']):
    self.assertTrue((BASE/'embedded-original-assets'/model['model']).exists())
  for ship in [SHIP,OTHER_SHIP]:
   self.assertTrue((BASE/'embedded-original-assets'/ship['spec']['model']).exists())
 def test_approved_revisions_preserved(self):
  status=json.loads((BASE/'STATUS.json').read_text('utf-8'))
  for name,expected in [('chat',status['chat_v13_sha256']),('studies',status['studies_v13_sha256'])]:
   filename='approved-chat-v13.html' if name=='chat' else 'approved-studies-v13-offline.html'
   data=(BASE/'prototype'/filename).read_bytes()
   self.assertEqual(hashlib.sha256(data).hexdigest(),expected)
 def test_all_asset_paths_covered(self):
  inventory=json.loads((BASE/'graphics-inventory.json').read_text('utf-8'))
  self.assertEqual(sum(len(x) for x in inventory['assets'].values()),58)
  self.assertEqual(len(inventory['assets']['ships']),15)
  self.assertEqual(len(inventory['assets']['objects']),17)
  self.assertEqual(len(inventory['assets']['planet_art'])+len(inventory['assets']['ground']),25)

if __name__=='__main__': unittest.main()
