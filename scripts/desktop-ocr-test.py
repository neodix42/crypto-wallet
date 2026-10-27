#!/usr/bin/env python3
"""Regress the hosted-runner public tooltip failure; never start a wallet."""
import ast
import hashlib
import json
import pathlib
import subprocess
import tempfile
import unittest
from types import SimpleNamespace
from unittest import mock

from PIL import Image, ImageOps

ROOT = pathlib.Path(__file__).resolve().parents[1]
FIXTURE = ROOT / 'tests/e2e/fixtures/decred-public-tooltip.png'


class PublicTooltipOCR(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory(prefix='wallet-public-ocr-')
        self.addCleanup(self.folder.cleanup)
        self.capture = mock.Mock(return_value=FIXTURE)
        self.secret = mock.Mock(return_value=False)
        self.scope = {'Image': Image, 'ImageOps': ImageOps, 'subprocess': subprocess,
                      'reports': pathlib.Path(self.folder.name), 'desktop': object(),
                      'phrase_ui': SimpleNamespace(secret_window=self.secret),
                      'capture': self.capture}
        tree = ast.parse((ROOT / 'scripts/packaged-recovery.py').read_text())
        node = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == 'textboxes')
        exec(compile(ast.Module(body=[node], type_ignores=[]), 'public-ocr-helper', 'exec'), self.scope)
        self.read = self.scope['textboxes']

    def test_actual_hosted_failure_pixels_preserve_exact_node_guidance(self):
        metadata = json.loads(FIXTURE.with_suffix('.json').read_text())
        self.assertEqual(hashlib.sha256(FIXTURE.read_bytes()).hexdigest(), metadata['fixtureSHA256'])
        rows = self.read(scale=3, threshold=None, psm=11)
        observed = ' '.join(row[11] for row in rows).lower()
        for fragment in metadata['expectedFragments']:
            self.assertIn(fragment.lower(), observed)
        self.assertIn('no spv proof claim', observed)
        for row in rows:
            self.assertGreaterEqual(int(row[6]), 0)
            self.assertLess(int(row[6]), 575)
            self.assertGreaterEqual(int(row[7]), 0)
            self.assertLess(int(row[7]), 43)

    def test_region_filter_uses_original_screen_coordinates_after_scaling(self):
        rows = self.read(scale=3, threshold=None, psm=11)
        first_line = [row for row in rows if int(row[7]) < 20]
        self.assertTrue(first_line)
        self.assertEqual(self.read((0, 0, 575, 20), scale=3, threshold=None, psm=11), first_line)
        self.assertEqual(self.read((575, 0, 600, 43), scale=3, threshold=None, psm=11), [])

    def test_credential_guard_runs_before_any_pixel_capture_or_ocr(self):
        self.secret.return_value = True
        with mock.patch.object(subprocess, 'check_output') as run:
            with self.assertRaisesRegex(RuntimeError, 'credential window'):
                self.read(scale=3, threshold=None, psm=11)
        self.capture.assert_not_called()
        run.assert_not_called()
        self.assertEqual(list(pathlib.Path(self.folder.name).iterdir()), [])

    def test_ordinary_text_keeps_existing_threshold_mode_and_coordinate_scale(self):
        output = 'header\n5\t1\t1\t1\t1\t1\t12\t14\t20\t10\t99\tPublic\n'
        with mock.patch.object(subprocess, 'check_output', return_value=output) as run:
            rows = self.read()
        self.assertEqual(rows[0][6:10], ['6', '7', '10', '5'])
        self.assertEqual(run.call_args.args[0][-3:], ['--psm', '3', 'tsv'])
        self.assertEqual(run.call_args.kwargs['timeout'], 20)
        self.assertEqual(rows[0][11], 'Public')

    def test_decoder_timeout_and_failed_process_do_not_become_success(self):
        for error in [subprocess.TimeoutExpired('tesseract', 20),
                      subprocess.CalledProcessError(1, 'tesseract')]:
            with mock.patch.object(subprocess, 'check_output', side_effect=error):
                with self.assertRaises(type(error)):
                    self.read(scale=3, threshold=None, psm=11)


if __name__ == '__main__':
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(PublicTooltipOCR))
    sources = ['scripts/desktop-ocr-test.py', 'scripts/packaged-recovery.py', 'Makefile',
               'tests/e2e/fixtures/decred-public-tooltip.png', 'tests/e2e/fixtures/decred-public-tooltip.json']
    report = {'status': 'PASS' if result.wasSuccessful() else 'FAIL', 'testsRun': result.testsRun,
              'scope': 'Actual hosted failure tooltip pixels, exact text, original coordinates, credential guard and bounded OCR failure propagation',
              'sourceSHA256': {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in sources}}
    (ROOT / 'reports').mkdir(exist_ok=True)
    (ROOT / 'reports/desktop-ocr.json').write_text(json.dumps(report, indent=2) + '\n')
    raise SystemExit(0 if result.wasSuccessful() else 1)
