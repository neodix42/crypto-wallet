#!/usr/bin/env python3
"""Pre-input diagnostics must explain startup failures without retaining workflow logs."""
import ast
import io
import pathlib
import subprocess
import sys
import tempfile
import time
import unittest
from types import SimpleNamespace
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
tree = ast.parse((ROOT / 'scripts/packaged-recovery.py').read_text())
node = next(n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == 'wait_for_startup')
scope = {'time': time}
exec(compile(ast.Module(body=[node], type_ignores=[]), 'startup-test-helper', 'exec'), scope)
wait_for_startup = scope['wait_for_startup']


class StartupDiagnostics(unittest.TestCase):
    def test_exited_process_retains_bounded_preinput_diagnostic_and_exit(self):
        with tempfile.TemporaryFile(mode='w+') as log:
            process = subprocess.Popen([sys.executable, '-c', 'import sys; print("public startup sandbox prerequisite failed",file=sys.stderr); sys.exit(23)'], stdout=log, stderr=log)
            process.wait(timeout=5)
            report = {}
            with self.assertRaisesRegex(RuntimeError, 'startupFailures'):
                wait_for_startup(SimpleNamespace(windows=lambda: []), process, log, report)
            failure = report['startupFailures'][0]
            self.assertEqual(failure['exitCode'], 23)
            self.assertEqual(failure['phase'], 'before credential or phrase entry')
            self.assertIn('sandbox prerequisite failed', failure['log'])
            self.assertFalse(failure['logTruncated'])

    def test_successful_public_startup_never_retains_workflow_log(self):
        class NoRead:
            def flush(self):
                raise AssertionError('must not read a successful workflow log')
        report = {}
        wait_for_startup(SimpleNamespace(windows=lambda: [(1, 'CryptoWallet')]), SimpleNamespace(poll=lambda: None), NoRead(), report)
        self.assertEqual(report, {})

    def test_timeout_and_large_diagnostics_stay_bounded(self):
        report = {}
        with self.assertRaisesRegex(RuntimeError, 'public window'):
            wait_for_startup(SimpleNamespace(windows=lambda: []), SimpleNamespace(poll=lambda: None), io.StringIO('x' * 20000), report, seconds=0)
        self.assertEqual(len(report['startupFailures'][0]['log']), 16384)
        self.assertTrue(report['startupFailures'][0]['logTruncated'])
        self.assertIsNone(report['startupFailures'][0]['exitCode'])


class WailsPrivateStartup(unittest.TestCase):
    def setUp(self):
        tree = ast.parse((ROOT / 'scripts/wails-wallet-test.py').read_text())
        functions = [node for node in tree.body if isinstance(node, ast.FunctionDef)
                     and node.name in ('receive_dialog_state', 'request_receive_copy', 'public_rows')]
        desktop = next(node for node in tree.body if isinstance(node, ast.ClassDef) and node.name == 'Desktop')
        functions.append(next(node for node in desktop.body if isinstance(node, ast.FunctionDef) and node.name == 'secret_window'))
        functions.append(next(node for node in desktop.body if isinstance(node, ast.FunctionDef) and node.name == 'park_pointer'))
        self.clock = SimpleNamespace(now=0)
        self.clock.monotonic = lambda: self.clock.now
        self.clock.sleep = lambda seconds: setattr(self.clock, 'now', self.clock.now + seconds)
        self.desktop = SimpleNamespace(windows=mock.Mock(), image=mock.Mock(), park_pointer=mock.Mock(), d=object())
        self.click = mock.Mock()
        self.scope = {'time': self.clock, 'desktop': self.desktop, 'click': self.click,
                      'PrivateWindowVisible': type('PrivateWindowVisible', (RuntimeError,), {})}
        exec(compile(ast.Module(body=functions, type_ignores=[]), 'wails-startup-test-helper', 'exec'), self.scope)
        self.main = [(1, 'CryptoWallet · 0.1.1 · mainnet')]

    def sequence(self, *states):
        pending = iter(states)
        last = states[-1]
        def windows():
            nonlocal last
            last = next(pending, last)
            return last
        self.desktop.windows.side_effect = windows

    def test_delayed_executable_title_waits_without_another_click_or_any_pixels(self):
        self.sequence(self.main, self.main + [(2, 'cryptowallet')],
                      self.main + [(2, 'cryptowallet')], self.main + [(2, 'Verify receive address')])
        self.scope['request_receive_copy'](seconds=1)
        self.click.assert_called_once_with('Copy address')
        self.desktop.park_pointer.assert_called_once_with()
        self.desktop.image.assert_not_called()
        self.assertGreaterEqual(self.clock.now, .15)

    def test_loading_window_never_becomes_a_verified_receive_without_its_exact_title(self):
        self.sequence(self.main, self.main + [(2, 'cryptowallet')])
        with self.assertRaisesRegex(RuntimeError, 'bounded deadline'):
            self.scope['request_receive_copy'](seconds=1)
        self.assertLess(self.clock.now, 1.1)
        self.click.assert_called_once_with('Copy address')
        self.desktop.image.assert_not_called()

    def test_unknown_multiple_or_unrequested_private_windows_fail_closed(self):
        for windows in (self.main + [(2, 'cryptowallet')], self.main + [(2, 'Verify receive address')],
                        self.main + [(2, 'Unlock local wallet')],
                        self.main + [(2, 'Verify receive address extra')],
                        self.main + [(2, 'cryptowallet'), (3, 'Verify receive address')]):
            with self.subTest(titles=[title for _, title in windows]):
                self.desktop.windows.side_effect = None
                self.desktop.windows.return_value = windows
                with self.assertRaisesRegex(RuntimeError, 'Unexpected'):
                    self.scope['request_receive_copy'](seconds=1)
        self.click.assert_not_called()
        self.desktop.image.assert_not_called()

    def test_loading_window_that_closes_or_changes_to_another_prompt_is_not_retried(self):
        for final in (self.main, self.main + [(2, 'Confirm app password')]):
            with self.subTest(titles=[title for _, title in final]):
                self.click.reset_mock()
                self.sequence(self.main, self.main + [(2, 'cryptowallet')], final)
                with self.assertRaisesRegex(RuntimeError, 'closed before|Unexpected'):
                    self.scope['request_receive_copy'](seconds=1)
                self.click.assert_called_once_with('Copy address')
        self.desktop.image.assert_not_called()

    def test_only_the_read_only_public_request_can_be_retried_before_any_private_window(self):
        self.sequence(self.main)
        with self.assertRaisesRegex(RuntimeError, 'bounded deadline'):
            self.scope['request_receive_copy'](seconds=1.6)
        self.assertEqual(self.click.call_args_list, [mock.call('Copy address')] * 3)
        self.desktop.image.assert_not_called()

    def test_loading_verified_and_unknown_windows_all_block_public_pixel_capture_and_ocr(self):
        x = SimpleNamespace(**{name: mock.Mock() for name in ('XGrabServer', 'XSync', 'XUngrabServer', 'XFlush')})
        decoder = mock.Mock()
        self.scope.update(mod=SimpleNamespace(X=x), subprocess=SimpleNamespace(check_output=decoder))
        self.desktop.secret_window = lambda: self.scope['secret_window'](self.desktop)
        for title in ('cryptowallet', 'Verify receive address', 'Record recovery phrase', 'Unexpected private title'):
            with self.subTest(title=title):
                self.desktop.windows.return_value = self.main + [(2, title)]
                with self.assertRaises(self.scope['PrivateWindowVisible']):
                    self.scope['public_rows']()
        self.desktop.image.assert_not_called()
        decoder.assert_not_called()
        self.assertEqual(x.XGrabServer.call_count, 4)
        self.assertEqual(x.XUngrabServer.call_count, 4)

    def test_hover_dismissal_moves_only_to_owned_empty_root_without_click_focus_or_pixels(self):
        motion, flush = mock.Mock(), mock.Mock()
        self.scope['mod'] = SimpleNamespace(T=SimpleNamespace(XTestFakeMotionEvent=motion),
                                           X=SimpleNamespace(XFlush=flush))
        self.scope['park_pointer'](self.desktop)
        motion.assert_called_once_with(self.desktop.d, -1, 1590, 1090, 0)
        flush.assert_called_once_with(self.desktop.d)
        self.click.assert_not_called()
        self.desktop.image.assert_not_called()
        self.desktop.windows.assert_not_called()
        self.assertEqual(self.clock.now, .15)


if __name__ == '__main__':
    unittest.main()
