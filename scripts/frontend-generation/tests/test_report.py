import unittest,sys
from pathlib import Path
BASE=Path(__file__).resolve().parents[1]
class Reports(unittest.TestCase):
 def test_files_and_original_backup(self):
  for name in ('AUDIT.ru.md','README.md','STATUS.json','graphics-inventory.json','ui-registry-scan.py','check_embedded_assets.py','prototype/approved-chat-v13.html','prototype/approved-studies-v13-offline.html'):
   with self.subTest(name=name):self.assertTrue((BASE/name).is_file())
if __name__=='__main__':unittest.main()
