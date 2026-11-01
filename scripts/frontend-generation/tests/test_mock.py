import sys
import ast
import unittest
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'mock'))
import data

class MockApi(unittest.TestCase):
    def test_snapshot_contract(self):
        for endpoint in ['state','profile','expedition','fleet','gameplay','workbench','notifications']:
            self.assertIsNotNone(data.api_data(endpoint), endpoint)
        self.assertIn('user',data.api_data('state'))
        self.assertIn('system',data.api_data('expedition'))
        self.assertIn('ship',data.api_data('expedition'))
        self.assertIn('ships',data.api_data('fleet'))
        self.assertIn('research',data.api_data('workbench'))
        self.assertIn('settings',data.api_data('gameplay'))
    def test_fake_world(self):
        w=data.world()
        self.assertEqual(len(w['stars']),80)
        self.assertEqual(w['stars'][0]['id'],1)
        self.assertTrue(all('from' in lane and 'to' in lane for lane in w['lanes']))
        self.assertTrue(all(s['class'] in 'OBAFGKM' for s in w['stars']))
    def test_fake_company_is_list(self):
        self.assertIsInstance(data.api_data('companies'),list)
    def test_colony_mock_uses_real_frontend_shape(self):
        data_projects = data.api_data('planet/projects')
        self.assertIsInstance(data_projects['projects'], list)
        self.assertIsInstance(data_projects['refunds'], list)
        self.assertGreater(len(data_projects['projects']), 0)
        self.assertTrue(all({'id','surface','star','planet','sector','name','owner','status'} <= set(p) for p in data_projects['projects']))
        detail = data.colony_detail(data_projects['projects'][0]['id'])
        self.assertIsInstance(detail['project']['required'], dict)
        self.assertIsInstance(detail['contributions'], list)
        self.assertIsInstance(detail['events'], list)

    def test_all_program_python_sources_compile(self):
        root = Path(__file__).resolve().parents[1]
        for directory in (root, root/'mock', root/'tests'):
            for source in directory.glob('*.py'):
                with self.subTest(source=str(source.relative_to(root))):
                    ast.parse(source.read_text(encoding='utf-8'), filename=str(source))

    def test_sources(self):
        self.assertTrue(any(e['source']=='purchased' for e in data.KNOWN))
        self.assertTrue(any(e['source']=='survey' for e in data.KNOWN))

if __name__=='__main__': unittest.main()
