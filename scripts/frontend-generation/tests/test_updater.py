import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import sys
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import update


def blob(data):
    return hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()


class MirrorUpdateTests(unittest.TestCase):
    def test_remote_manifest_filters_out_other_paths(self):
        sources={'update.py':b'print("updated")\n','README.md':b'Readme\n'}
        tree=[{'path':'scripts/frontend-generation/'+name,'type':'blob','size':len(body),'sha':blob(body)} for name,body in sources.items()]
        tree += [{'path':'internal/server/web/index.html','type':'blob','size':1,'sha':'a'*40},
                 {'path':'scripts/frontend-generation/original-web/secret.py','type':'blob','size':1,'sha':'b'*40},
                 {'path':'scripts/frontend-generation/../README.md','type':'blob','size':1,'sha':'c'*40}]
        def fetch(url):
            if '/branches/' in url:return json.dumps({'commit':{'sha':'f'*40}}).encode()
            if '/git/trees/' in url:return json.dumps({'tree':tree,'truncated':False}).encode()
            raise AssertionError(url)
        commit, result=update.remote_manifest(fetch)
        self.assertEqual(commit, 'f'*40)
        self.assertEqual(set(result),set(sources))

    def test_update_checks_hashes_then_installs_with_backup(self):
        original=update.ROOT
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)
            (root/'update.py').write_text('old script','utf-8')
            payload={'update.py':b'print("new")\n','README.md':b'New readme'}
            tree=[{'path':'scripts/frontend-generation/'+name,'type':'blob','size':len(data),'sha':blob(data)} for name,data in payload.items()]
            def fetch(url):
                if '/branches/' in url:return json.dumps({'commit':{'sha':'e'*40}}).encode()
                if '/git/trees/' in url:return json.dumps({'tree':tree,'truncated':False}).encode()
                return payload[url.rsplit('/',1)[-1]]
            with mock.patch.object(update,'ROOT',root),mock.patch.object(update,'STATE',root/'.mirror-update-state.json'):
                self.assertEqual(update.update(fetch=fetch,announce=lambda _:None),2)
                self.assertEqual((root/'update.py').read_bytes(),b'print("new")\n')
                self.assertTrue(list((root/'.mirror-backup').rglob('update.py')))
                self.assertEqual(update.update(fetch=fetch,announce=lambda _:None),0)
        self.assertEqual(update.ROOT,original)

    def test_rejects_malformed_python_before_installation(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)
            (root/'update.py').write_text('old script','utf-8')
            payload={'update.py':b'if True print("invalid")\n','README.md':b'New readme'}
            tree=[{'path':'scripts/frontend-generation/'+name,'type':'blob','size':len(data),'sha':blob(data)} for name,data in payload.items()]
            def fetch(url):
                if '/branches/' in url:return json.dumps({'commit':{'sha':'e'*40}}).encode()
                if '/git/trees/' in url:return json.dumps({'tree':tree,'truncated':False}).encode()
                return payload[url.rsplit('/',1)[-1]]
            with mock.patch.object(update,'ROOT',root), mock.patch.object(update,'STATE',root/'.mirror-update-state.json'):
                with self.assertRaisesRegex(update.UpdateError,'Ошибка синтаксиса'):
                    update.update(fetch=fetch,announce=lambda _:None)
                self.assertEqual((root/'update.py').read_text('utf-8'),'old script')
                self.assertFalse((root/'README.md').exists())

    def test_modified_file_not_written_if_hash_invalid(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder)
            (root/'update.py').write_text('untouched','utf-8')
            payload={'update.py':b'correct','README.md':b'correct'}
            tree=[{'path':'scripts/frontend-generation/'+name,'type':'blob','size':len(data),'sha':blob(data)} for name,data in payload.items()]
            def fetch(url):
                if '/branches/' in url:return json.dumps({'commit':{'sha':'d'*40}}).encode()
                if '/git/trees/' in url:return json.dumps({'tree':tree,'truncated':False}).encode()
                return b'incorrect'
            with mock.patch.object(update,'ROOT',root),mock.patch.object(update,'STATE',root/'.mirror-update-state.json'):
                with self.assertRaises(update.UpdateError):update.update(fetch=fetch,announce=lambda _:None)
                self.assertEqual((root/'update.py').read_text(),'untouched')

if __name__=='__main__':unittest.main()
