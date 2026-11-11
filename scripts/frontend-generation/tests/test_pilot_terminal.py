"""Isolated pilot-terminal POC contracts and executable pure-model tests.

Run: python -m unittest tests.test_pilot_terminal -v
Browser: python tests/browser_pilot_terminal_smoke.py (running localhost demo).
"""
import re
import shutil
import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BASE = ROOT / "mock" / "pilot-terminal"


class PilotTerminalTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.model = (BASE / "model.js").read_text("utf-8")
        cls.controller = (BASE / "terminal.js").read_text("utf-8")
        cls.modules = (BASE / "modules.js").read_text("utf-8")
        cls.fixtures = (BASE / "fixtures.js").read_text("utf-8")
        cls.css = (BASE / "terminal.css").read_text("utf-8")

    def test_production_is_not_imported_by_adapters(self):
        self.assertNotIn('createFleet({', self.modules)
        self.assertNotIn('createMining(', self.modules)
        for forbidden in (
            '/features/fleet/index.js', '/features/mining/index.js',
            "Game.performExpedition", "select_ship", 'api("expedition"',
            'api("workbench"', "fetch(", "WebSocket(",
            "Session.data.expedition.ship =",
        ):
            with self.subTest(forbidden=forbidden):
                self.assertNotIn(forbidden, self.controller + self.modules)
        self.assertIn('import {Modules,UI} from "/ui/runtime.js"', self.controller)
        self.assertIn('const root=Modules.create(id,', self.controller)

    def test_safe_fixture_and_model(self):
        self.assertRegex(self.fixtures, r'id:41,name:"Пилигрим"')
        self.assertRegex(self.fixtures, r'id:82,name:"Пилигрим"')
        self.assertIn("new Map(SHIPS.map(ship=>[ship.id", self.fixtures)
        self.assertIn('MAX_WORKSPACES = 10', self.model)
        self.assertIn('granularity:"grapheme"', self.model)
        self.assertIn('version:VERSION', self.model)
        self.assertIn('"fixed"', self.model)
        self.assertIn('"workspace"', self.model)
        self.assertIn('"none"', self.model)

    def test_storage_lifecycle_and_scope(self):
        for item in (
            'Preferences.key("pilot-terminal",{account,version:STORAGE_VERSION})',
            '"module:"+this.id(ws,ins)', "Modules.close(id);Modules.destroy(id)",
            "Preferences.remove", 'window.addEventListener("pagehide"',
            'localStorage.getItem(this.key)!==JSON.stringify(this.state)',
            'window.addEventListener("signedout"',
            'Modules.focus(this.id(ws,existing))',
        ):
            self.assertIn(item, self.controller)
        self.assertIn("overscroll-behavior:contain", self.css)
        self.assertIn("ptdemo-u", self.model)

    def test_demo_entry_and_no_prod_changes(self):
        overlay = (ROOT / "mock/ui-overlay.js").read_text("utf-8")
        self.assertIn('import("/mock/pilot-terminal/terminal.js")', overlay)
        self.assertIn("bootChat", overlay)
        self.assertIn("bootUiLab", overlay)
        self.assertIn("openColonyCatalogue", overlay)
        self.assertIn("bootPilotTerminal", overlay)

    @unittest.skipUnless(shutil.which("node"), "Node not installed")
    def test_pure_model_with_node(self):
        code = r"""
import fs from 'node:fs';
import {webcrypto} from 'node:crypto';
globalThis.crypto = globalThis.crypto || webcrypto;
const src=fs.readFileSync(process.argv[1],'utf8');
const m=await import('data:text/javascript,'+encodeURIComponent(src));
function check(cond,msg){if(!cond)throw Error(msg);}
const s=m.seeded();
check(m.validateState(s),'seed');
check(s.workspaces.length===2,'spaces');
check(s.workspaces[0].shipId===41&&s.workspaces[1].shipId===82,'ship ids');
const items=s.workspaces.flatMap(ws=>ws.instances.map(i=>m.windowId('7',ws,i)));
check(new Set(items).size===items.length,'unique window IDs');
check(m.validName('🚀'.repeat(32))&&!m.validName('🚀'.repeat(33)),'grapheme limits');
check(!m.validName('   '),'blank');
const ships=new Map([[41,{id:41}],[82,{id:82}]]);
const a=s.workspaces[0],b=s.workspaces[1];
check(m.resolveShip(a.instances[0],a,ships).ship.id===41,'fixed #41');
a.shipId=82;
check(m.resolveShip(a.instances[0],a,ships).ship.id===41,'fixed stays');
check(m.resolveShip(a.instances[1],a,ships).ship.id===82,'workspace follows');
check(m.resolveShip(b.instances[1],b,ships).ship.id===82,'other space intact');
check(m.resolveShip(a.instances[3],a,ships).status==='unbound','guide none');
ships.delete(82);
check(m.resolveShip(a.instances[1],a,ships).status==='unavailable','missing ship');
const clone=m.duplicateWorkspace(a);
check(clone.id!==a.id&&clone.instances.every((x,i)=>x.id!==a.instances[i].id),'clone identities');
check(m.validName(clone.name),'clone name valid');
for(let i=2;i<10;i++)s.workspaces.push(m.newWorkspace('Test '+i,41));
check(m.validateState(s),'ten valid');
s.workspaces.push(m.newWorkspace('Eleventh',41));
check(!m.validateState(s),'eleven rejected');
const revived=JSON.parse(JSON.stringify(s));
revived.workspaces.pop();
check(m.validateState(revived),'JSON persisted');
console.log('PASS: PT01 PT02 PT03 PT05 PT06 PT07 + JSON state');
"""
        result = subprocess.run(
            ["node", "--input-type=module", "--eval", code, str(BASE / "model.js")],
            capture_output=True, text=True, timeout=20
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
