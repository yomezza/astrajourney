"""Cross-module UI mock contracts: maps, studies and market are one world."""
import sys
import json
import unittest
import threading
from pathlib import Path
from http.server import ThreadingHTTPServer
from urllib.request import Request, urlopen
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT))
from mock import registry, data
from server import DemoHandler

class Silent(DemoHandler):
    def log_message(self,*args): pass

class RegistryMarketTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.http=ThreadingHTTPServer(('127.0.0.1',0),Silent)
        cls.thread=threading.Thread(target=cls.http.serve_forever,daemon=True)
        cls.thread.start()
        cls.base=f'http://127.0.0.1:{cls.http.server_port}'
    @classmethod
    def tearDownClass(cls):
        cls.http.shutdown();cls.http.server_close();cls.thread.join(timeout=2)
    def get(self,path):
        with urlopen(self.base+path,timeout=3) as result:return json.load(result)
    def post(self,path,body):
        payload=json.dumps(body).encode('utf-8')
        with urlopen(Request(self.base+path,data=payload,method='POST',headers={'Content-Type':'application/json'}),timeout=3) as result:return json.load(result)
    def test_maps_and_research_share_identical_stars(self):
        stars=self.get('/api/galaxy')['stars']
        reg=self.get('/api/demo/registry')
        self.assertEqual(stars,reg['stars'])
        self.assertEqual(len(stars),80)
        self.assertEqual(reg['ship_star'],data.SHIP['star'])
        for star in stars:
            self.assertIn(star['class'],'OBAFGKM')
            self.assertGreater(star['mass'],0)
            self.assertGreater(star['luminosity'],0)
        for number in (1,3,5,19,48):
            detail=self.get('/api/system?id='+str(number))
            self.assertEqual(detail['star'],stars[number-1])
            self.assertEqual(detail['region'],registry.REGIONS[detail['star']['region']-1]['name'])
        for known in reg['known']:
            self.assertEqual(registry.STAR_BY_ID[known['star']]['id'],known['star'])
    def test_ports_are_in_systems_and_market(self):
        global_market=self.get('/api/market/global')
        ports=global_market['ports']
        self.assertGreater(len(ports),1)
        self.assertIsInstance(global_market['reservations'],list)
        workbench=self.get('/api/workbench')
        self.assertEqual(len(ports),len(workbench['stations']))
        for port in ports:
            star=port['star'];station=port['station']
            self.assertIn(station,[site['id'] for site in registry.SYSTEM_SITES[star]])
            self.assertTrue(port['offers'])
            for offer in port['offers']:
                self.assertIn(offer['resource'],{r['id'] for r in data.RESOURCES})
        self.assertEqual(self.get('/api/expedition')['offers'],registry.OFFERS[(1,104)])
    def test_global_reservation_cancel_updates_shared_stock(self):
        offer=registry.OFFERS[(1,104)][0]
        initial=offer['stock'];balance=data.USER['credits'];old=list(registry.RESERVATIONS)
        try:
            response=self.post('/api/market/global',{'action':'reserve','star':1,'station':104,'resource':offer['resource'],'quantity':2,'expected_price':offer['buy']})
            self.assertTrue(response['ok'])
            self.assertEqual(offer['stock'],initial-2)
            self.assertEqual(self.get('/api/market/global')['reservations'][-1]['id'],response['id'])
            self.post('/api/market/global',{'action':'cancel','id':response['id']})
            self.assertEqual(offer['stock'],initial)
            self.assertEqual(data.USER['credits'],balance)
        finally:
            offer['stock']=initial
            data.USER['credits']=balance
            registry.RESERVATIONS[:]=old
    def test_studies_navigation_uses_parent_map(self):
        source=(ROOT/'prototype/studies/app.js').read_text(encoding='utf-8')
        overlay=(ROOT/'mock/ui-overlay.js').read_text(encoding='utf-8')
        self.assertIn("fetch('/api/demo/registry'",source)
        self.assertIn("cosmosimulator-demo:show-star",source)
        self.assertIn("cosmosimulator-demo:show-star",overlay)
        self.assertIn("Modules.open('galaxy')",overlay)

if __name__=='__main__':unittest.main()
