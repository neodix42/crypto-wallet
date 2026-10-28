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

from PIL import Image, ImageOps

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
        self.desktop.grab_server = lambda: self.scope['mod'].X.XGrabServer(self.desktop.d)
        self.desktop.ungrab_server = lambda: (self.scope['mod'].X.XUngrabServer(self.desktop.d),
                                               self.scope['mod'].X.XFlush(self.desktop.d))
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
            for contrast in (False, True):
                with self.subTest(title=title, contrast=contrast):
                    self.desktop.windows.return_value = self.main + [(2, title)]
                    with self.assertRaises(self.scope['PrivateWindowVisible']):
                        self.scope['public_rows'](contrast=contrast)
        self.desktop.image.assert_not_called()
        decoder.assert_not_called()
        self.assertEqual(x.XGrabServer.call_count, 8)
        self.assertEqual(x.XUngrabServer.call_count, 8)

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


class WailsAppearanceCapture(unittest.TestCase):
    def setUp(self):
        tree = ast.parse((ROOT / 'scripts/wails-wallet-test.py').read_text())
        functions = [node for node in tree.body if isinstance(node, ast.FunctionDef)
                     and node.name in ('palette', 'masked_field_rectangles', 'public_picture')]
        self.x = SimpleNamespace(**{name: mock.Mock() for name in
                                 ('XGrabServer', 'XSync', 'XUngrabServer', 'XFlush')})
        self.desktop = SimpleNamespace(d=object(), secret_window=mock.Mock(return_value=False),
                                       image=mock.Mock())
        self.scope = {'appearance': {'theme': 'light', 'fontSize': 18},
                      'mod': SimpleNamespace(X=self.x), 'desktop': self.desktop,
                      'PrivateWindowVisible': type('PrivateWindowVisible', (RuntimeError,), {})}
        self.desktop.grab_server = lambda: self.x.XGrabServer(self.desktop.d)
        self.desktop.ungrab_server = lambda: (self.x.XUngrabServer(self.desktop.d), self.x.XFlush(self.desktop.d))
        exec(compile(ast.Module(body=functions, type_ignores=[]), 'wails-appearance-test-helper', 'exec'), self.scope)

    def test_both_themes_locate_two_whole_masked_fields_and_exclude_footer(self):
        for theme in ('light', 'dark'):
            with self.subTest(theme=theme):
                self.scope['appearance']['theme'] = theme
                colors = self.scope['palette']()
                picture = Image.new('RGB', (800, 700), colors['page'])
                bounds = (50, 30, 680, 528)
                for top in (250, 360):
                    picture.paste(colors['input'], (76, top, 704, top + 54))
                # A footer with the same surface color must not be mistaken
                # for a third password field, even in the light theme.
                picture.paste(colors['input'], (50, 484, 730, 558))
                self.assertEqual(self.scope['masked_field_rectangles'](picture, bounds, colors['input']),
                                 [(250, 303), (360, 413)])
                picture.paste(colors['page'], (76, 360, 704, 414))
                self.assertEqual(self.scope['masked_field_rectangles'](picture, bounds, colors['input']),
                                 [(250, 303)], 'a missing or clipped secret field cannot be invented')

    def test_private_dialog_blocks_new_appearance_capture_before_any_pixels(self):
        self.desktop.secret_window.return_value = True
        with self.assertRaises(self.scope['PrivateWindowVisible']):
            self.scope['public_picture']()
        self.desktop.image.assert_not_called()
        self.x.XGrabServer.assert_called_once_with(self.desktop.d)
        self.x.XUngrabServer.assert_called_once_with(self.desktop.d)

    def test_capture_and_conversion_exception_always_release_owned_server(self):
        self.desktop.image.side_effect = RuntimeError('synthetic capture failure')
        with self.assertRaisesRegex(RuntimeError, 'synthetic capture failure'):
            self.scope['public_picture']()
        self.x.XUngrabServer.assert_called_once_with(self.desktop.d)
        self.x.XFlush.assert_called_once_with(self.desktop.d)


class WailsOwnedServerGuard(unittest.TestCase):
    def setUp(self):
        tree = ast.parse((ROOT / 'scripts/wails-wallet-test.py').read_text())
        desktop = next(node for node in tree.body if isinstance(node, ast.ClassDef) and node.name == 'Desktop')
        methods = [node for node in desktop.body if isinstance(node, ast.FunctionDef)
                   and node.name in ('grab_server', 'ungrab_server', 'windows')]
        copied = ast.ClassDef(name='Desktop', bases=[ast.Name(id='Base', ctx=ast.Load())],
                              keywords=[], body=methods, decorator_list=[])
        public = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'public_rows')
        self.events = []
        self.fail_enumeration = False
        owner = self

        class Base:
            def windows(self):
                owner.assertGreater(self._server_grab_depth, 0)
                owner.events.append('enumerate')
                if owner.fail_enumeration:
                    raise RuntimeError('synthetic retiring-window failure')
                return [(1, 'CryptoWallet')]

        self.x = SimpleNamespace(XGrabServer=mock.Mock(side_effect=lambda _: self.events.append('grab')),
                                 XUngrabServer=mock.Mock(side_effect=lambda _: self.events.append('ungrab')),
                                 XFlush=mock.Mock(), XSync=mock.Mock())
        self.scope = {'Base': Base, 'mod': SimpleNamespace(X=self.x), 'Image': Image,
                      'ImageOps': ImageOps, 'appearance': {'theme': 'light', 'fontSize': 18},
                      'io': io, 'subprocess': SimpleNamespace(check_output=mock.Mock(return_value=b'header\n'), DEVNULL=subprocess.DEVNULL),
                      'PrivateWindowVisible': type('PrivateWindowVisible', (RuntimeError,), {})}
        module = ast.fix_missing_locations(ast.Module(body=[copied, public], type_ignores=[]))
        exec(compile(module, 'wails-owned-server-test-helper', 'exec'), self.scope)
        self.desktop = self.scope['Desktop']()
        self.desktop.d = object()
        self.scope['desktop'] = self.desktop

        def private():
            self.desktop.windows()
            return False

        def pixels():
            self.assertEqual(self.desktop._server_grab_depth, 1)
            self.assertNotIn('ungrab', self.events)
            self.events.append('pixels')
            return Image.new('RGB', (4, 4), 'white')

        self.desktop.secret_window = private
        self.desktop.image = mock.Mock(side_effect=pixels)

    def test_nested_enumeration_keeps_public_guard_held_until_pixel_capture(self):
        self.assertEqual(self.scope['public_rows'](), [])
        self.assertEqual(self.events, ['grab', 'enumerate', 'pixels', 'ungrab'])
        self.assertEqual(self.desktop._server_grab_depth, 0)
        self.x.XGrabServer.assert_called_once_with(self.desktop.d)
        self.x.XUngrabServer.assert_called_once_with(self.desktop.d)

    def test_enumeration_failure_releases_only_its_depth_and_outer_finally_releases_once(self):
        self.fail_enumeration = True
        self.desktop.grab_server()
        with self.assertRaisesRegex(RuntimeError, 'retiring-window'):
            self.desktop.windows()
        self.assertEqual(self.desktop._server_grab_depth, 1)
        self.x.XUngrabServer.assert_not_called()
        self.desktop.ungrab_server()
        self.assertEqual(self.desktop._server_grab_depth, 0)
        self.x.XUngrabServer.assert_called_once_with(self.desktop.d)
        self.desktop.image.assert_not_called()

    def test_sync_failure_restores_standalone_guard_and_unbalanced_release_is_rejected(self):
        self.x.XSync.side_effect = RuntimeError('synthetic sync failure')
        with self.assertRaisesRegex(RuntimeError, 'sync failure'):
            self.desktop.windows()
        self.assertEqual(self.desktop._server_grab_depth, 0)
        self.x.XUngrabServer.assert_called_once_with(self.desktop.d)
        with self.assertRaisesRegex(RuntimeError, 'not held'):
            self.desktop.ungrab_server()
        self.x.XUngrabServer.assert_called_once_with(self.desktop.d)


class WailsPrivateCopyNavigation(unittest.TestCase):
    def setUp(self):
        tree = ast.parse((ROOT / 'scripts/wails-wallet-test.py').read_text())
        function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'private_click')
        self.desktop = SimpleNamespace(d=object(), wait=mock.Mock(return_value=(7, 'Back up recovery phrase')),
                                       geometry=mock.Mock(return_value=(460, 220, 680, 660)), click=mock.Mock())
        self.rows = mock.Mock()
        self.motion, self.button, self.flush = mock.Mock(), mock.Mock(), mock.Mock()
        self.scope = {'desktop': self.desktop, 'private_rows': self.rows,
                      'mod': SimpleNamespace(T=SimpleNamespace(XTestFakeMotionEvent=self.motion, XTestFakeButtonEvent=self.button),
                                             X=SimpleNamespace(XFlush=self.flush)),
                      'time': SimpleNamespace(sleep=mock.Mock())}
        exec(compile(ast.Module(body=[function], type_ignores=[]), 'wails-private-copy-test-helper', 'exec'), self.scope)

    def test_only_authenticated_backup_copy_can_scroll_and_click_its_actual_label(self):
        copy = ['5', '1', '1', '1', '1', '1', '100', '200', '40', '20', '99', 'Copy']
        phrase = copy[:]
        phrase[11] = 'phrase'
        self.rows.side_effect = [[], [copy, phrase]]
        self.scope['private_click']('Back up recovery phrase', 'Copy phrase')
        self.rows.assert_has_calls([mock.call('Back up recovery phrase')] * 2)
        self.motion.assert_called_once_with(self.desktop.d, -1, 1122, 550, 0)
        self.assertEqual(self.button.call_count, 6)
        self.desktop.click.assert_called_once_with(580, 430)

    def test_unknown_private_prompt_or_action_is_rejected_before_pixels_or_input(self):
        for prefix, words in (('Record recovery phrase', 'Copy phrase'),
                              ('Back up recovery phrase', 'Done'),
                              ('Unlock local wallet', 'Continue')):
            with self.subTest(prefix=prefix, words=words):
                with self.assertRaisesRegex(RuntimeError, 'Unexpected private control'):
                    self.scope['private_click'](prefix, words)
        self.rows.assert_not_called()
        self.desktop.wait.assert_not_called()
        self.desktop.click.assert_not_called()
        self.motion.assert_not_called()

    def test_missing_control_has_a_finite_body_scroll_bound_and_never_clicks(self):
        self.rows.return_value = []
        with self.assertRaisesRegex(RuntimeError, 'no private OCR diagnostic retained'):
            self.scope['private_click']('Back up recovery phrase', 'Copy phrase')
        self.assertEqual(self.rows.call_count, 8)
        self.assertEqual(self.motion.call_count, 8)
        self.desktop.click.assert_not_called()


class PublicStatusTiming(unittest.TestCase):
    def setUp(self):
        tree = ast.parse((ROOT / 'scripts/unlock-layout.py').read_text())
        functions = [node for node in tree.body if isinstance(node, ast.FunctionDef)
                     and node.name in ('capture_public_status', 'status_text', 'observe_status_expiry')]
        self.clock = SimpleNamespace(now=100.0)
        self.clock.monotonic = lambda: self.clock.now
        self.clock.sleep = lambda seconds: setattr(self.clock, 'now', self.clock.now + seconds)
        self.bounds = (20, 30, 800, 600)
        self.locked = False
        self.capture_times = []
        self.expires_at = 107.0

        def grab(display):
            self.assertFalse(self.locked)
            self.locked = True

        def release(display):
            self.assertTrue(self.locked)
            self.locked = False

        def image():
            self.assertTrue(self.locked)
            self.capture_times.append(self.clock.now)
            picture = Image.new('RGB', (1000, 800), 'red')
            x, y, width, height = self.bounds
            # The public producer follows its own clock. Red pixels above
            # the status bar must never reach the status decoder.
            color = 'white' if self.clock.now < self.expires_at else 'black'
            picture.paste(color, (x, y + height - 52, x + width, y + height))
            return picture

        self.x = SimpleNamespace(XGrabServer=mock.Mock(side_effect=grab), XSync=mock.Mock(),
                                 XUngrabServer=mock.Mock(side_effect=release), XFlush=mock.Mock())
        self.desktop = SimpleNamespace(d=object(), secret_window=mock.Mock(return_value=False),
                                       windows=mock.Mock(return_value=[(17, 'CryptoWallet')]),
                                       geometry=mock.Mock(return_value=self.bounds), image=mock.Mock(side_effect=image))
        self.scope = {'mod': SimpleNamespace(X=self.x), 'desktop': self.desktop, 'time': self.clock,
                      'Image': Image, 'ImageOps': ImageOps, 'io': io, 'subprocess': subprocess}
        exec(compile(ast.Module(body=functions, type_ignores=[]), 'unlock-status-test-helper', 'exec'), self.scope)
        self.decode_starts = []

        def decode(picture):
            self.assertFalse(self.locked)
            self.assertEqual(picture.size, (800, 52))
            self.assertEqual(len(self.capture_times), 2, 'both public captures must precede OCR')
            self.decode_starts.append(self.clock.now)
            visible = picture.getpixel((0, 0)) == (255, 255, 255)
            self.clock.now += 3.4
            return 'status request cancelled.' if visible else 'status'

        self.decoder = mock.Mock(side_effect=decode)
        self.scope['status_text'] = self.decoder

    def observe(self):
        return self.scope['observe_status_expiry'](100.0, self.bounds)

    def test_slow_ocr_cannot_change_fixed_public_observation_times(self):
        result = self.observe()
        self.assertTrue(result['visibleBeforeExpiry'])
        self.assertTrue(result['absentAfterExpiry'])
        self.assertAlmostEqual(result['preExpiryCaptureSecondsAfterCancel'], 6.8)
        self.assertAlmostEqual(result['postExpiryCaptureSecondsAfterCancel'], 8.0)
        self.assertEqual(result['captureIntervalsSecondsAfterCancel'], [[6.8, 6.8], [8.0, 8.0]])
        self.assertEqual(result['maximumAbsenceCaptureSeconds'], 8.5)
        self.assertEqual(result['ocrCompletedSecondsAfterCancel'], 14.8)
        self.assertEqual(result['ocrSeconds'], 6.8)
        self.assertEqual(len(self.capture_times), 2)
        self.assertGreaterEqual(self.decode_starts[0], self.capture_times[1])

    def test_real_early_expiry_and_persistent_messages_remain_failures(self):
        for expires, visible, absent in ((106.5, False, True), (120.0, True, False)):
            with self.subTest(expires=expires):
                self.clock.now = 100.0
                self.capture_times.clear()
                self.expires_at = expires
                result = self.observe()
                self.assertEqual(result['visibleBeforeExpiry'], visible)
                self.assertEqual(result['absentAfterExpiry'], absent)
                self.assertFalse(result['visibleBeforeExpiry'] and result['absentAfterExpiry'])

    def test_ambiguous_or_unreadable_postexpiry_text_cannot_prove_expiry(self):
        for value in ('', 'unrelated error', 'status request cancellect.'):
            with self.subTest(text=value):
                self.clock.now = 100.0
                self.capture_times.clear()
                self.decoder.side_effect = ['status request cancelled.', value]
                result = self.observe()
                self.assertTrue(result['visibleBeforeExpiry'])
                self.assertFalse(result['absentAfterExpiry'])

    def test_slow_capture_cannot_be_reported_as_on_time(self):
        original = self.desktop.image.side_effect
        def slow_image():
            picture = original()
            self.clock.now += .25
            return picture
        self.desktop.image.side_effect = slow_image
        with self.assertRaisesRegex(RuntimeError, 'bounded observation time'):
            self.observe()
        self.decoder.assert_not_called()
        self.assertEqual(len(self.capture_times), 1)
        self.assertFalse(self.locked)

    def test_late_postexpiry_capture_is_rejected_before_any_decoding(self):
        original = self.desktop.image.side_effect
        def delayed_second_image():
            picture = original()
            if len(self.capture_times) == 2:
                self.clock.now += .6
            return picture
        self.desktop.image.side_effect = delayed_second_image
        with self.assertRaisesRegex(RuntimeError, 'bounded observation time'):
            self.observe()
        self.decoder.assert_not_called()
        self.assertEqual(len(self.capture_times), 2)
        self.assertFalse(self.locked)

    def test_private_prompt_guard_blocks_all_pixel_capture_and_decoding(self):
        self.desktop.secret_window.return_value = True
        with self.assertRaisesRegex(RuntimeError, 'private prompt'):
            self.observe()
        self.desktop.image.assert_not_called()
        self.decoder.assert_not_called()
        self.assertEqual(self.x.XUngrabServer.call_count, 1)
        self.assertFalse(self.locked)

    def test_private_prompt_between_samples_blocks_the_second_capture_and_all_ocr(self):
        self.desktop.secret_window.side_effect = [False, True]
        with self.assertRaisesRegex(RuntimeError, 'private prompt'):
            self.observe()
        self.assertEqual(self.desktop.image.call_count, 1)
        self.decoder.assert_not_called()
        self.assertEqual(self.x.XUngrabServer.call_count, 2)
        self.assertFalse(self.locked)

    def test_unexpected_or_additional_windows_never_reach_pixel_capture(self):
        for windows in ([], [(18, 'cryptowallet')], [(18, 'CryptoWallet — Unlock local wallet')],
                        [(17, 'CryptoWallet'), (18, 'Record recovery phrase')],
                        [(17, 'CryptoWallet'), (18, 'CryptoWallet')]):
            with self.subTest(windows=windows):
                self.desktop.windows.return_value = windows
                with self.assertRaisesRegex(RuntimeError, 'Unexpected window'):
                    self.scope['capture_public_status'](self.bounds)
                self.assertFalse(self.locked)
        self.desktop.image.assert_not_called()
        self.decoder.assert_not_called()

    def test_window_geometry_drift_fails_before_pixels(self):
        self.desktop.geometry.return_value = (21, 30, 800, 600)
        with self.assertRaisesRegex(RuntimeError, 'geometry changed'):
            self.scope['capture_public_status'](self.bounds)
        self.desktop.image.assert_not_called()
        self.decoder.assert_not_called()
        self.assertFalse(self.locked)

    def test_capture_exception_releases_only_the_owned_server(self):
        self.desktop.image.side_effect = RuntimeError('controlled public capture failure')
        with self.assertRaisesRegex(RuntimeError, 'capture failure'):
            self.observe()
        self.assertEqual(self.x.XUngrabServer.call_count, 1)
        self.assertEqual(self.x.XFlush.call_count, 1)
        self.assertFalse(self.locked)
        self.decoder.assert_not_called()

    def test_sync_exception_releases_the_owned_server_before_any_pixels(self):
        self.x.XSync.side_effect = RuntimeError('controlled owned-server sync failure')
        with self.assertRaisesRegex(RuntimeError, 'sync failure'):
            self.observe()
        self.assertEqual(self.x.XUngrabServer.call_count, 1)
        self.assertEqual(self.x.XFlush.call_count, 1)
        self.assertFalse(self.locked)
        self.desktop.image.assert_not_called()
        self.decoder.assert_not_called()

    def test_bounded_ocr_receives_only_the_immutable_public_crop(self):
        tree = ast.parse((ROOT / 'scripts/unlock-layout.py').read_text())
        function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'status_text')
        exec(compile(ast.Module(body=[function], type_ignores=[]), 'unlock-status-decoder', 'exec'), self.scope)
        picture = Image.new('RGB', (800, 52), 'black')
        def decode(command, **options):
            self.assertEqual(command, ['tesseract', 'stdin', 'stdout', '--psm', '7'])
            self.assertEqual(options['timeout'], 20)
            with Image.open(io.BytesIO(options['input'])) as image:
                self.assertEqual(image.size, (1600, 104))
            return b'Status\n'
        with mock.patch.object(subprocess, 'check_output', side_effect=decode):
            self.assertEqual(self.scope['status_text'](picture), 'status')
        for error in (subprocess.TimeoutExpired('tesseract', 20), subprocess.CalledProcessError(1, 'tesseract')):
            with mock.patch.object(subprocess, 'check_output', side_effect=error):
                with self.assertRaises(type(error)):
                    self.scope['status_text'](picture)
        self.desktop.image.assert_not_called()


if __name__ == '__main__':
    unittest.main()
