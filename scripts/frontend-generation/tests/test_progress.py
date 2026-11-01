"""Console preparation progress is available for downloads, ZIPs, local copies."""
import io
import tempfile
import unittest
import zipfile
from pathlib import Path
from contextlib import redirect_stdout
import sys
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import prepare

class ProgressTests(unittest.TestCase):
    def test_bar_prints_bytes_and_percent(self):
        output=io.StringIO()
        with redirect_stdout(output):
            bar=prepare.Progress('Загрузка оригинала',8*1048576,'байт')
            bar.draw(4*1048576,True)
            bar.done(8*1048576)
        self.assertIn('50.0%',output.getvalue())
        self.assertIn('8.0 / 8.0 MiB',output.getvalue())
    def test_zip_extract_counts_files(self):
        with tempfile.TemporaryDirectory() as td:
            tmp=Path(td)
            file=tmp/'test.zip'
            with zipfile.ZipFile(file,'w') as z:
                for i in range(3):z.writestr(f'cosmosimulator-v0.23.0/internal/server/web/test-{i}.txt','hello')
            old_target=prepare.TARGET
            try:
                prepare.TARGET=tmp/'original-web'
                output=io.StringIO()
                with self.assertRaises(ValueError),redirect_stdout(output):prepare.from_archive(file)
                self.assertIn('3 / 3',output.getvalue())
                self.assertIn('100.0%',output.getvalue())
            finally:prepare.TARGET=old_target
if __name__=='__main__':unittest.main()
