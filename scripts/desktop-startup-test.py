#!/usr/bin/env python3
"""Pre-input diagnostics must explain startup failures without retaining workflow logs."""
import ast
import argparse
import contextlib
import hashlib
import io
import json
import os
import pathlib
import re
import select
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import time
import unittest
from types import SimpleNamespace
from unittest import mock

from PIL import Image, ImageOps, ImageDraw

ROOT = pathlib.Path(__file__).resolve().parents[1]
CURRENT_UI = '--current-ui' in sys.argv
if not CURRENT_UI:
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


class WailsDisplayStartup(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/test-wails-security.py').read_text())
        names=('x11_setup_ready','wait_private_x11','private_x11_display')
        functions=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in names]
        self.scope={'contextlib':contextlib,'os':os,'re':re,'select':select,'signal':signal,
                    'socket':socket,'struct':struct,'subprocess':subprocess,'sys':sys,
                    'tempfile':tempfile,'time':time,'Path':pathlib.Path}
        exec(compile(ast.Module(body=functions,type_ignores=[]),'owned-x11-startup-helper','exec'),self.scope)
        self.folder=tempfile.TemporaryDirectory(prefix='wallet-owned-x11-')
        self.addCleanup(self.folder.cleanup)
        self.report={}

    def emitter(self,body):
        path=pathlib.Path(self.folder.name)/'owned-display'
        path.write_text('#!'+sys.executable+'\nimport os,sys,time,signal\nfd=int(sys.argv[sys.argv.index("-displayfd")+1])\n'+body+'\n')
        path.chmod(0o700)
        return str(path)

    def test_stderr_larger_than_pipe_capacity_cannot_block_announcement_and_is_reaped(self):
        emitter=self.emitter('os.write(2,b"PUBLIC_XVFB_DIAGNOSTIC\\n"*16384)\nos.write(fd,b"181\\n")\ntime.sleep(60)')
        self.scope['x11_setup_ready']=mock.Mock(return_value=True)
        with self.scope['private_x11_display'](emitter,self.report) as number:
            self.assertEqual(number,'181')
            self.assertGreater(self.report['displayStartup']['stderrBytes'],65536)
        self.assertEqual(self.report['displayCleanup']['status'],'PASS')
        self.assertEqual(self.report['displayStartup']['status'],'READY')
        self.assertNotIn('stderrTail',self.report['displayStartup'])

    def test_partial_newline_and_socket_readiness_are_both_required(self):
        emitter=self.emitter('os.write(fd,b"18")\ntime.sleep(.02)\nos.write(fd,b"1\\n")\ntime.sleep(60)')
        self.scope['x11_setup_ready']=mock.Mock(side_effect=[False,False,True])
        with self.scope['private_x11_display'](emitter,self.report) as number:self.assertEqual(number,'181')
        self.assertEqual(self.scope['x11_setup_ready'].call_count,3)
        self.assertTrue(all(call.args[0]=='181' and call.args[2]>1 for call in self.scope['x11_setup_ready'].call_args_list))

    def test_missing_newline_fails_within_budget_without_any_ambient_display_probe(self):
        emitter=self.emitter('os.write(fd,b"181")\ntime.sleep(60)')
        self.scope['x11_setup_ready']=mock.Mock(return_value=True)
        with self.assertRaisesRegex(RuntimeError,'did not announce readiness'):
            with self.scope['private_x11_display'](emitter,self.report,seconds=.1):self.fail('must not yield')
        self.scope['x11_setup_ready'].assert_not_called()
        self.assertFalse(self.report['displayStartup']['announcementReceived'])
        self.assertEqual(self.report['displayCleanup']['status'],'PASS')

    def test_invalid_oversized_or_multiple_announcements_never_reach_x11(self):
        self.scope['x11_setup_ready']=mock.Mock(return_value=True)
        for announcement in (b'../181\n',b'65536\n',b'181\n182\n',b'1'*17):
            emitter=self.emitter('os.write(fd,'+repr(announcement)+')\ntime.sleep(60)')
            with self.subTest(announcement=announcement),self.assertRaisesRegex(RuntimeError,'invalid announcement|oversized announcement'):
                with self.scope['private_x11_display'](emitter,self.report):self.fail('must not yield')
        self.scope['x11_setup_ready'].assert_not_called()

    def test_early_child_exit_reports_bounded_public_stderr_and_precise_exit_code(self):
        emitter=self.emitter('os.write(2,b"PUBLIC_STARTUP_ONLY\\n"*2048+b"PUBLIC_TAIL_CANARY\\n")\nos._exit(23)')
        self.scope['x11_setup_ready']=mock.Mock(return_value=True)
        with self.assertRaisesRegex(RuntimeError,'before readiness|before becoming ready'):
            with self.scope['private_x11_display'](emitter,self.report):self.fail('must not yield')
        diagnostic=self.report['displayStartup']
        # Closing the announcement FD may wake the parent just before the
        # exiting child becomes waitable; cleanup must still reap exact23.
        self.assertIn(diagnostic['exitCode'],(None,23))
        self.assertEqual(self.report['displayCleanup']['exitCode'],23)
        self.assertTrue(diagnostic['stderrTruncated'])
        self.assertLessEqual(len(diagnostic['stderrTail']),4096)
        self.assertIn('PUBLIC_TAIL_CANARY',diagnostic['stderrTail'])
        self.scope['x11_setup_ready'].assert_not_called()

    def test_diagnostic_tail_does_not_move_the_childs_shared_file_offset(self):
        server=SimpleNamespace(poll=lambda:None)
        with tempfile.TemporaryFile() as diagnostic:
            diagnostic.write(b'public diagnostic '*1000);diagnostic.flush()
            offset=diagnostic.tell()
            with self.assertRaisesRegex(RuntimeError,'did not announce readiness'):
                self.scope['wait_private_x11'](server,-1,diagnostic,self.report,seconds=0)
            self.assertEqual(diagnostic.tell(),offset)

    def connection(self,reply,peer_pid=1234,peer_uid=None):
        pending=bytearray(reply)
        connection=mock.MagicMock()
        connection.__enter__.return_value=connection
        connection.getsockopt.return_value=struct.pack('=iII',peer_pid,os.geteuid() if peer_uid is None else peer_uid,os.getegid())
        def read(count):
            chunk=bytes(pending[:min(count,3)]);del pending[:len(chunk)];return chunk
        connection.recv.side_effect=read
        return connection

    def reply(self,width=1280,height=800,accepted=1,major=11,units=None):
        body=bytearray(72);body[20]=1
        struct.pack_into('<HH',body,52,width,height)
        return struct.pack('<BBHHH',accepted,0,major,0,len(body)//4 if units is None else units)+body

    def test_owned_unix_socket_requires_complete_x11_setup_and_expected_screen(self):
        connection=self.connection(self.reply())
        with mock.patch.object(pathlib.Path,'is_socket',return_value=True),mock.patch.object(socket,'socket',return_value=connection):
            self.assertTrue(self.scope['x11_setup_ready']('181',1,1234))
        connection.connect.assert_called_once_with('/tmp/.X11-unix/X181')
        connection.sendall.assert_called_once_with(struct.pack('<BBHHHHH',ord('l'),0,11,0,0,0,0))

    def test_slow_fragmented_setup_uses_remaining_startup_budget_not_a_shorter_retry_timeout(self):
        connection=self.connection(self.reply())
        original=connection.recv.side_effect
        delayed=False
        def read(count):
            nonlocal delayed
            if not delayed:time.sleep(.15);delayed=True
            return original(count)
        connection.recv.side_effect=read
        announcement,writer=os.pipe()
        os.write(writer,b'181\n');os.close(writer)
        server=SimpleNamespace(pid=1234,poll=lambda:None)
        try:
            with tempfile.TemporaryFile() as diagnostic,mock.patch.object(pathlib.Path,'is_socket',return_value=True),mock.patch.object(socket,'socket',return_value=connection):
                self.assertEqual(self.scope['wait_private_x11'](server,announcement,diagnostic,self.report,seconds=2),'181')
        finally:os.close(announcement)
        self.assertGreaterEqual(self.report['displayStartup']['elapsedSeconds'],.15)
        self.assertTrue(self.report['displayStartup']['x11SetupVerified'])

    def test_foreign_pid_or_uid_is_rejected_before_any_setup_or_clipboard_request(self):
        for pid,uid in ((4321,os.geteuid()),(1234,os.geteuid()+1)):
            connection=self.connection(self.reply(),pid,uid)
            with self.subTest(pid=pid,uid=uid),mock.patch.object(pathlib.Path,'is_socket',return_value=True),mock.patch.object(socket,'socket',return_value=connection):
                self.assertFalse(self.scope['x11_setup_ready']('181',1,1234))
            connection.sendall.assert_not_called()
            connection.recv.assert_not_called()

    def test_rejected_truncated_oversized_wrong_protocol_or_wrong_screen_never_becomes_ready(self):
        for reply in (self.reply(accepted=0),self.reply(major=12),self.reply(width=1600),
                      self.reply(units=16385),self.reply()[:12],b''):
            connection=self.connection(reply)
            with self.subTest(reply=reply[:8]),mock.patch.object(pathlib.Path,'is_socket',return_value=True),mock.patch.object(socket,'socket',return_value=connection):
                self.assertFalse(self.scope['x11_setup_ready']('181',1,1234))
        with mock.patch.object(pathlib.Path,'is_socket',return_value=False),mock.patch.object(socket,'socket') as creator:
            self.assertFalse(self.scope['x11_setup_ready']('181',1,1234))
            creator.assert_not_called()

    def test_remaining_stubborn_descendant_is_killed_even_after_leader_exits(self):
        marker=pathlib.Path(self.folder.name)/'unexpected-child-marker'
        emitter=self.emitter('r,w=os.pipe()\npid=os.fork()\nif pid==0:\n os.close(r)\n signal.signal(signal.SIGTERM,signal.SIG_IGN)\n os.write(w,b"ready")\n os.close(w)\n time.sleep(.6)\n open('+repr(str(marker))+',"w").write("not cleaned")\n os._exit(0)\nos.close(w)\nos.read(r,5)\nos.close(r)\nos.write(fd,b"181\\n")\ntime.sleep(60)')
        self.scope['x11_setup_ready']=mock.Mock(return_value=True)
        with self.scope['private_x11_display'](emitter,self.report):pass
        time.sleep(.65)
        self.assertFalse(marker.exists())
        self.assertTrue(self.report['displayCleanup']['forced'])

    def test_cleanup_kill_reaps_leader_and_cannot_mask_the_primary_failure(self):
        self.scope['wait_private_x11']=mock.Mock(return_value='181')
        server=SimpleNamespace(pid=54321,returncode=-9,wait=mock.Mock(side_effect=[subprocess.TimeoutExpired('owned',5),-9]))
        with mock.patch.object(subprocess,'Popen',return_value=server) as popen,mock.patch.object(os,'killpg') as kill:
            with self.scope['private_x11_display']('owned-test-server',self.report):pass
        self.assertTrue(popen.call_args.kwargs['start_new_session'])
        self.assertIn(mock.call(54321,signal.SIGKILL),kill.call_args_list)
        self.assertEqual(server.wait.call_count,2)
        self.assertEqual(self.report['displayCleanup']['status'],'PASS')
        server.wait.side_effect=subprocess.TimeoutExpired('owned',5)
        with mock.patch.object(subprocess,'Popen',return_value=server),mock.patch.object(os,'killpg'):
            with self.assertRaisesRegex(RuntimeError,'original policy failure'):
                with self.scope['private_x11_display']('owned-test-server',self.report):raise RuntimeError('original policy failure')
        self.assertEqual(self.report['displayCleanup']['status'],'FAIL')

    def test_spawn_failure_closes_both_owned_pipe_descriptors(self):
        original=os.pipe
        pairs=[]
        def pipe():
            pair=original();pairs.append(pair);return pair
        with mock.patch.object(os,'pipe',side_effect=pipe),mock.patch.object(subprocess,'Popen',side_effect=OSError('public spawn failure')):
            with self.assertRaisesRegex(OSError,'public spawn failure'):
                with self.scope['private_x11_display']('owned-test-server',self.report):self.fail('must not yield')
        for descriptor in pairs[0]:
            with self.assertRaises(OSError):os.fstat(descriptor)


class WailsInWindowDialogs(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        names=('palette','dialog_panel_bounds','dialog_heading_matches')
        functions=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in names]
        desktop=next(node for node in tree.body if isinstance(node,ast.ClassDef) and node.name=='Desktop')
        methods=[node for node in desktop.body if isinstance(node,ast.FunctionDef) and node.name in ('wait','secret_window','assert_dialog_owner')]
        self.clock=SimpleNamespace(now=0)
        self.clock.monotonic=lambda:self.clock.now
        self.clock.sleep=lambda value:setattr(self.clock,'now',self.clock.now+value)
        self.decoder=mock.Mock(return_value=b'Unlock local wallet\nEnter the requested information to continue.')
        self.report={}
        self.scope={'appearance':{'theme':'light','fontSize':18},'Image':Image,'ImageOps':ImageOps,'io':io,'re':re,
                    'subprocess':SimpleNamespace(check_output=self.decoder,DEVNULL=subprocess.DEVNULL),
                    'time':self.clock,'report':self.report,'owned_application_processes':mock.Mock(return_value=[42,43])}
        exec(compile(ast.Module(body=functions+methods,type_ignores=[]),'in-window-dialogs','exec'),self.scope)
        self.bounds=(80,32,1440,1036)

    def fixture(self,width=680,height=400,dim=True):
        colors=self.scope['palette']()
        blend=lambda color:tuple(round((a+b)/2) for a,b in zip(color,(8,16,24))) if dim else color
        picture=Image.new('RGB',(1600,1100),'black')
        x,y,w,h=self.bounds
        picture.paste(blend(colors['page']),(x,y,x+w,y+h))
        picture.paste(blend(colors['chrome']),(x,y,x+w,y+77))
        picture.paste(blend(colors['surface']),(x,y+h-31,x+w,y+h))
        left=x+(w-width)//2;top=y+(h-height)//2
        ImageDraw.Draw(picture).rounded_rectangle((left,top,left+width-1,top+height-1),radius=10,
                                                  fill=colors['surface'],outline=colors['border'])
        return picture,(left,top,width,height)

    def test_modal_frames_are_measured_inside_the_same_owned_main_window_in_both_themes(self):
        for theme in ('light','dark'):
            self.scope['appearance']['theme']=theme
            for width,height in ((680,320),(760,460),(980,800),(1040,900)):
                with self.subTest(theme=theme,width=width):
                    picture,expected=self.fixture(width,height)
                    self.assertEqual(self.scope['dialog_panel_bounds'](picture,self.bounds),expected)

    def test_public_cards_missing_borders_or_unowned_pixels_never_become_dialogs(self):
        picture,_=self.fixture(dim=False)
        self.assertIsNone(self.scope['dialog_panel_bounds'](picture,self.bounds))
        picture,box=self.fixture()
        left,top,width,height=box
        ImageDraw.Draw(picture).line((left,top+12,left,top+height-12),fill=self.scope['palette']()['surface'])
        with self.assertRaisesRegex(RuntimeError,'without a complete frame'):
            self.scope['dialog_panel_bounds'](picture,self.bounds)
        with self.assertRaisesRegex(RuntimeError,'owned application bounds'):
            self.scope['dialog_panel_bounds'](picture,(80,32,1600,1100))

    def test_wait_resolves_the_actual_dialog_heading_to_its_unchanged_main_window_id(self):
        picture,box=self.fixture()
        desktop=SimpleNamespace(dialog_capture=mock.Mock(return_value=(17,picture,box)))
        self.assertEqual(self.scope['wait'](desktop,'Unlock local wallet',seconds=1),(17,'Unlock local wallet'))
        self.assertEqual(self.report['inWindowDialogs']['Unlock local wallet'],
                         {'window':17,'bounds':list(box),'sameApplicationWindow':True})
        self.decoder.return_value=b'Confirm app password\nEnter the requested information to continue.'
        with self.assertRaisesRegex(RuntimeError,'Expected in-window dialog heading'):
            self.scope['wait'](desktop,'Unlock local wallet',seconds=.2)
        self.assertLess(self.clock.now,.4)

    def test_modal_presence_blocks_artifact_capture_even_without_a_separate_native_window(self):
        desktop=SimpleNamespace(windows=lambda:[(17,'CryptoWallet')],dialog_capture=mock.Mock(return_value=(17,object(),(460,220,680,400))))
        self.assertTrue(self.scope['secret_window'](desktop))
        desktop.dialog_capture.return_value=None
        self.assertFalse(self.scope['secret_window'](desktop))
        desktop.windows=lambda:[(17,'CryptoWallet'),(18,'Unexpected native window')]
        self.assertTrue(self.scope['secret_window'](desktop))

    def test_dialog_owner_requires_one_unchanged_window_pid_and_no_removed_prompt_process(self):
        desktop=SimpleNamespace(_application_window=17,_application_pid=42,
                                window_metadata=mock.Mock(return_value=[{'window':17,'pid':42}]))
        identity=self.scope['assert_dialog_owner'](desktop,17)
        self.assertTrue(identity['sameApplicationProcess'])
        self.assertTrue(identity['removedPromptProcessAbsent'])
        for rows in ([{'window':17,'pid':55}],[{'window':18,'pid':42}],
                     [{'window':17,'pid':42},{'window':18,'pid':42}]):
            desktop.window_metadata.return_value=rows
            with self.subTest(rows=rows),self.assertRaisesRegex(RuntimeError,'same application window and process'):
                self.scope['assert_dialog_owner'](desktop,17)
        desktop.window_metadata.return_value=[{'window':17,'pid':42}]
        self.scope['owned_application_processes'].side_effect=RuntimeError('Application created a removed separate prompt process')
        with self.assertRaisesRegex(RuntimeError,'removed separate prompt process'):
            self.scope['assert_dialog_owner'](desktop,17)


class WailsPrivateStartup(unittest.TestCase):
    def setUp(self):
        tree = ast.parse((ROOT / 'scripts/wails-wallet-test.py').read_text())
        functions = [node for node in tree.body if isinstance(node, ast.FunctionDef)
                     and node.name in ('receive_dialog_state', 'reset_public_clipboard', 'public_copy_feedback', 'request_receive_copy', 'public_rows')]
        desktop = next(node for node in tree.body if isinstance(node, ast.ClassDef) and node.name == 'Desktop')
        functions.append(next(node for node in desktop.body if isinstance(node, ast.FunctionDef) and node.name == 'secret_window'))
        functions.append(next(node for node in desktop.body if isinstance(node, ast.FunctionDef) and node.name == 'park_pointer'))
        self.clock = SimpleNamespace(now=0)
        self.clock.monotonic = lambda: self.clock.now
        self.clock.sleep = lambda seconds: setattr(self.clock, 'now', self.clock.now + seconds)
        ticks = iter(range(1, 10000))
        self.clock.monotonic_ns = lambda: next(ticks)
        self.desktop = SimpleNamespace(windows=mock.Mock(), image=mock.Mock(), park_pointer=mock.Mock(), dialog_capture=mock.Mock(return_value=None), d=object())
        self.click = mock.Mock(side_effect=lambda *_: self.clock.now)
        self.address = '0x1111111111111111111111111111111111111111'
        self.clipboard_value = self.address
        def clipboard(value=None):
            if value is not None:
                self.clipboard_value = value
            return self.clipboard_value
        self.clipboard = mock.Mock(side_effect=clipboard)
        self.status = mock.Mock(return_value='Copy address')
        self.report = {}
        self.scope = {'time': self.clock, 'desktop': self.desktop, 'click': self.click, 'report': self.report,
                      'clipboard': self.clipboard, 'text': self.status,
                      'PrivateWindowVisible': type('PrivateWindowVisible', (RuntimeError,), {})}
        self.desktop.secret_window = lambda: self.scope['secret_window'](self.desktop)
        self.desktop.grab_server = lambda: self.scope['mod'].X.XGrabServer(self.desktop.d)
        self.desktop.ungrab_server = lambda: (self.scope['mod'].X.XUngrabServer(self.desktop.d),
                                               self.scope['mod'].X.XFlush(self.desktop.d))
        exec(compile(ast.Module(body=functions, type_ignores=[]), 'wails-startup-test-helper', 'exec'), self.scope)
        self.main = [(1, 'CryptoWallet · 0.1.1 · mainnet')]
        self.desktop.windows.return_value = self.main

    def sequence(self, *states):
        pending = iter(states)
        last = states[-1]
        def windows():
            nonlocal last
            last = next(pending, last)
            return last
        self.desktop.windows.side_effect = windows

    def complete_copy(self, *_):
        self.clipboard_value = self.address
        self.status.return_value = 'Address copied.'
        return self.clock.now

    def test_direct_copy_returns_a_fresh_write_and_status_without_a_private_prompt(self):
        self.click.side_effect = self.complete_copy
        self.assertEqual(self.scope['request_receive_copy'](seconds=1), self.address)
        self.click.assert_called_once_with('Copy address')
        self.desktop.park_pointer.assert_called_once_with()
        self.desktop.image.assert_not_called()
        self.assertTrue(self.clipboard.call_args_list[0].args[0].startswith('cryptowallet-public-test-sentinel-'))
        self.assertEqual(self.desktop.windows.return_value, self.main)

    def test_old_address_and_success_status_cannot_replace_a_fresh_clipboard_write(self):
        self.status.return_value = 'Address copied.'
        with self.assertRaisesRegex(RuntimeError, 'bounded deadline'):
            self.scope['request_receive_copy'](seconds=1)
        self.assertLess(self.clock.now, 1.1)
        self.assertEqual(self.click.call_args_list, [mock.call('Copy address')] * 2)
        self.assertNotEqual(self.clipboard_value, self.address)
        self.desktop.image.assert_not_called()

    def test_clipboard_write_without_success_status_does_not_claim_copy_success(self):
        self.click.side_effect = lambda *_: (setattr(self, 'clipboard_value', self.address), self.clock.now)[1]
        with self.assertRaisesRegex(RuntimeError, 'bounded deadline'):
            self.scope['request_receive_copy'](seconds=.2)
        self.assertEqual(self.clipboard_value, self.address)
        self.click.assert_called_once_with('Copy address')

    def test_each_request_uses_a_distinct_public_sentinel_and_requires_its_write_ack(self):
        first = self.scope['reset_public_clipboard']()
        second = self.scope['reset_public_clipboard']()
        self.assertNotEqual(first, second)
        self.assertEqual(self.clipboard_value, second)
        self.clipboard.side_effect = None
        self.clipboard.return_value = self.address
        with self.assertRaisesRegex(RuntimeError, 'fresh public sentinel'):
            self.scope['request_receive_copy'](seconds=.2)
        self.click.assert_not_called()

    def test_confirmation_message_cannot_pass_even_with_a_fresh_exact_address_and_success_text(self):
        def copied_with_confirmation(*_):
            self.clipboard_value = self.address
            self.status.return_value = 'Waiting for wallet confirmation… Address copied.'
            return self.clock.now
        self.click.side_effect = copied_with_confirmation
        with self.assertRaisesRegex(RuntimeError, 'incorrectly entered wallet confirmation'):
            self.scope['request_receive_copy'](seconds=1)
        self.assertNotIn('addressCopyTiming', self.report)

    def test_native_clipboard_timing_is_recorded_before_slow_status_ocr_without_a_latency_gate(self):
        sentinel = self.scope['reset_public_clipboard']()
        self.clipboard_value = self.address
        self.clock.now = 10
        timing = {'clickedAt': 9.75}
        def slow_ocr():
            self.clock.now += 5
            return 'Address copied.'
        self.status.side_effect = slow_ocr
        self.assertEqual(self.scope['public_copy_feedback'](sentinel,timing),self.address)
        self.assertEqual(timing['nativeClipboardObservedSeconds'],.25)
        self.assertEqual(timing['feedbackReadSeconds'],5)
        self.assertTrue(timing['walletConfirmationAbsent'])
        self.assertEqual(self.clock.now,15)

    def test_pending_status_keeps_the_first_fresh_native_observation_time(self):
        sentinel = self.scope['reset_public_clipboard']()
        self.clipboard_value = self.address
        timing = {'clickedAt': 0}
        self.clock.now = .4
        self.assertIsNone(self.scope['public_copy_feedback'](sentinel,timing))
        self.clock.now = 3
        self.status.return_value = 'Address copied.'
        self.assertEqual(self.scope['public_copy_feedback'](sentinel,timing),self.address)
        self.assertEqual(timing['nativeClipboardObservedSeconds'],.4)

    def test_feedback_needs_a_nonempty_new_value_and_status_and_checks_private_state_first(self):
        sentinel = self.scope['reset_public_clipboard']()
        self.status.return_value = 'Address copied.'
        for value in ('', sentinel):
            self.clipboard_value = value
            self.assertIsNone(self.scope['public_copy_feedback'](sentinel))
        self.clipboard_value = self.address
        self.status.return_value = 'Could not copy address.'
        self.assertIsNone(self.scope['public_copy_feedback'](sentinel))
        self.status.return_value = 'Address copied.'
        self.assertEqual(self.scope['public_copy_feedback'](sentinel), self.address)
        self.desktop.windows.return_value = self.main + [(2, 'Back up recovery phrase')]
        self.clipboard.reset_mock()
        self.status.reset_mock()
        with self.assertRaises(self.scope['PrivateWindowVisible']):
            self.scope['public_copy_feedback'](sentinel)
        self.clipboard.assert_not_called()
        self.status.assert_not_called()

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

    def test_any_private_window_after_the_click_aborts_without_replacement_click_or_pixels(self):
        for title in ('cryptowallet', 'Verify receive address', 'Confirm app password'):
            with self.subTest(title=title):
                self.click.reset_mock()
                self.sequence(self.main, self.main + [(2, title)])
                with self.assertRaisesRegex(RuntimeError, 'Unexpected'):
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


class WailsMainRowCopy(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        names=('main_row_copy_target','request_main_row_copy','main_row_copy','reveal_main_row_copy','receive_dialog_state','reset_public_clipboard','public_copy_feedback','public_words_position','palette')
        functions=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in names]
        self.folder=tempfile.TemporaryDirectory(prefix='wallet-main-row-')
        self.addCleanup(self.folder.cleanup)
        self.geometry=(80,100,1440,900)
        self.clock=SimpleNamespace(now=0)
        self.clock.monotonic=lambda:self.clock.now
        self.clock.sleep=lambda seconds:setattr(self.clock,'now',self.clock.now+seconds)
        ticks=iter(range(1,10000))
        self.clock.monotonic_ns=lambda:next(ticks)
        self.main=[(1,'CryptoWallet · 0.1.1 · mainnet')]
        self.desktop=SimpleNamespace(windows=mock.Mock(return_value=self.main),
                                     wait=mock.Mock(return_value=self.main[0]),
                                     geometry=mock.Mock(side_effect=lambda window:self.geometry),
                                     focus=mock.Mock(),click=mock.Mock(),accept=mock.Mock(),park_pointer=mock.Mock(),
                                     secret_window=mock.Mock(return_value=False),d=object())
        self.report={}
        self.rows=mock.Mock()
        self.address='0x1111111111111111111111111111111111111111'
        self.clipboard_value=self.address
        def clipboard(value=None):
            if value is not None:self.clipboard_value=value
            return self.clipboard_value
        self.clipboard=mock.Mock(side_effect=clipboard)
        self.status=mock.Mock(return_value='Account group')
        self.scope={'time':self.clock,'re':re,'desktop':self.desktop,'report':self.report,
                    'clipboard':self.clipboard,'text':self.status,
                    'public_rows':self.rows,'appearance':{'theme':'light'},
                    'REPORTS':pathlib.Path(self.folder.name),'args':SimpleNamespace(report_name='owned-row'),
                    'hashlib':hashlib,'stage':mock.Mock(),
                    'PrivateWindowVisible':type('PrivateWindowVisible',(RuntimeError,),{})}
        exec(compile(ast.Module(body=functions,type_ignores=[]),'wails-main-row-copy-helper','exec'),self.scope)

    def row(self,word,x,y,line=1):
        return ['5','1',str(line),'1','1','1',str(x),str(y),'95','14','99',word]

    def overview(self):
        return [self.row('Account',180,300,1),self.row('group',240,300,1),self.row('Ethereum',178,552,2)]

    def button_picture(self,button_x=1400,button_y=542,state='enabled'):
        picture=Image.new('RGB',(1600,1100),'white')
        colors=self.scope['palette']()
        fill=colors['button'] if state=='enabled' else colors['disabled']
        border=colors['border'] if state=='enabled' else tuple(round(.45*a+.55*b) for a,b in zip(colors['border'],colors['surface']))
        ImageDraw.Draw(picture).rounded_rectangle((button_x,button_y,button_x+33,button_y+33),radius=7,fill=fill,outline=border)
        return picture

    def test_measures_complete_copy_button_at_hosted_origin_instead_of_window_edge(self):
        picture=self.button_picture()
        point=self.scope['main_row_copy_target'](self.overview(),picture,self.geometry)
        self.assertEqual(point,(1416,558))
        self.assertNotEqual(point,(1465,559))
        self.assertEqual(self.report['mainRowCopy']['controlState'],'enabled')

    def test_new_window_offset_and_width_remeasure_actual_row_and_button(self):
        self.geometry=(20,60,1540,950)
        rows=[self.row('Account',120,260),self.row('group',180,260),self.row('Ethereum',118,512,2)]
        picture=self.button_picture(1470,502)
        self.assertEqual(self.scope['main_row_copy_target'](rows,picture,self.geometry),(1486,518))

    def test_disabled_copy_is_observed_as_pending_without_an_enabled_point(self):
        self.assertIsNone(self.scope['main_row_copy_target'](self.overview(),self.button_picture(state='disabled'),self.geometry))
        self.assertEqual(self.report['mainRowCopy']['controlState'],'disabled')

    def test_right_clipping_requires_reveal_and_never_exposes_a_partial_click_target(self):
        picture=self.button_picture(1462)
        ImageDraw.Draw(picture).rectangle((1487,500,1519,600),fill='white')
        self.assertIsNone(self.scope['main_row_copy_target'](self.overview(),picture,self.geometry))
        self.assertEqual(self.report['mainRowCopy']['controlState'],'clipped')

    def test_wrong_public_context_or_wallet_page_is_rejected_before_pixels(self):
        picture=mock.Mock(width=1600,height=1100)
        for rows in ([self.row('Ethereum',178,552)],self.overview()+[self.row('Copy',900,400,3),self.row('address',950,400,3)]):
            with self.subTest(rows=rows),self.assertRaisesRegex(RuntimeError,'left the account overview'):
                self.scope['main_row_copy_target'](rows,picture,self.geometry)
        picture.getpixel.assert_not_called()

    def test_other_row_controls_fill_patches_and_ambiguous_buttons_cannot_supply_a_point(self):
        pictures=[self.button_picture(button_y=616),Image.new('RGB',(1600,1100),'white')]
        plain=Image.new('RGB',(1600,1100),'white')
        ImageDraw.Draw(plain).rectangle((1400,542,1433,575),fill=self.scope['palette']()['button'])
        pictures.append(plain)
        multiple=self.button_picture(1360)
        ImageDraw.Draw(multiple).rounded_rectangle((1440,542,1473,575),radius=7,fill=self.scope['palette']()['button'],outline=self.scope['palette']()['border'])
        pictures.append(multiple)
        for picture in pictures:
            with self.subTest(),self.assertRaisesRegex(RuntimeError,'not uniquely identified'):
                self.scope['main_row_copy_target'](self.overview(),picture,self.geometry)

    def test_geometry_drift_is_rejected_before_any_pixel_or_pointer_use(self):
        picture=mock.Mock()
        with self.assertRaisesRegex(RuntimeError,'geometry changed'):
            self.scope['main_row_copy_target'](self.overview(),picture,(0,0,1440,900))
        picture.getpixel.assert_not_called()
        self.desktop.click.assert_not_called()

    def targets(self,*states):
        pending=iter(states)
        last=states[-1]
        def target(*values):
            nonlocal last
            last=next(pending,last)
            self.report.setdefault('mainRowCopy',{}).update(controlState=last,ethereumRow=[178,552,95,14])
            return (1416,558) if last=='enabled' else None
        self.scope['main_row_copy_target']=mock.Mock(side_effect=target)
        self.rows.return_value=(self.overview(),object(),self.geometry)

    def complete_copy(self,*_):
        self.clipboard_value=self.address
        self.status.return_value='Account group Address copied.'

    def test_disabled_control_waits_then_direct_copy_requires_no_private_prompt(self):
        self.targets('disabled','enabled')
        self.desktop.click.side_effect=self.complete_copy
        self.assertEqual(self.scope['request_main_row_copy'](seconds=2),self.address)
        self.desktop.click.assert_called_once_with(1416,558)
        self.assertEqual(self.rows.call_count,3)
        self.assertEqual(self.report['mainRowCopy']['attempts'],1)
        self.assertTrue(self.report['addressCopyTiming'][0]['walletConfirmationAbsent'])
        self.desktop.accept.assert_not_called()

    def test_copy_cannot_succeed_if_its_icon_becomes_disabled_after_the_native_write(self):
        self.targets('enabled','disabled')
        self.desktop.click.side_effect=self.complete_copy
        with self.assertRaisesRegex(RuntimeError,'became unavailable after direct address copy'):
            self.scope['request_main_row_copy'](seconds=1)
        self.desktop.click.assert_called_once_with(1416,558)
        self.assertNotIn('addressCopyTiming',self.report)

    def test_main_copy_rejects_wallet_confirmation_even_when_clipboard_and_status_succeed(self):
        self.targets('enabled')
        def confirmed_copy(*_):
            self.clipboard_value=self.address
            self.status.return_value='Account group Waiting for wallet confirmation… Address copied.'
        self.desktop.click.side_effect=confirmed_copy
        with self.assertRaisesRegex(RuntimeError,'incorrectly entered wallet confirmation'):
            self.scope['request_main_row_copy'](seconds=1)
        self.assertNotIn('addressCopyTiming',self.report)

    def test_disabled_control_cannot_copy_or_claim_success_from_old_clipboard_and_status(self):
        self.targets('disabled')
        self.status.return_value='Account group Address copied.'
        with self.assertRaisesRegex(RuntimeError,'bounded deadline'):
            self.scope['request_main_row_copy'](seconds=.2)
        self.desktop.click.assert_not_called()
        self.assertNotEqual(self.clipboard_value,self.address)

    def test_only_readonly_icon_can_be_retried_before_any_private_window(self):
        self.targets('enabled')
        with self.assertRaisesRegex(RuntimeError,'bounded deadline'):
            self.scope['request_main_row_copy'](seconds=1.6)
        self.assertEqual(self.desktop.click.call_args_list,[mock.call(1416,558)]*3)

    def test_private_or_loading_window_never_permits_public_pixels(self):
        self.targets('enabled')
        for title in ('Unlock local wallet','Verify receive address','cryptowallet'):
            self.desktop.windows.return_value=self.main+[(2,title)]
            with self.subTest(title=title),self.assertRaisesRegex(RuntimeError,'Unexpected'):
                self.scope['request_main_row_copy'](seconds=.2)
        self.rows.assert_not_called()
        self.desktop.click.assert_not_called()
        self.assertEqual(list(pathlib.Path(self.folder.name).iterdir()),[])

    def test_private_window_appearing_after_target_measurement_prevents_input(self):
        self.targets('enabled')
        self.desktop.secret_window.return_value=True
        with self.assertRaises(self.scope['PrivateWindowVisible']):
            self.scope['request_main_row_copy'](seconds=.2)
        self.rows.assert_called_once_with(with_picture=True,with_geometry=True)
        self.desktop.click.assert_not_called()
        self.desktop.accept.assert_not_called()

    def test_copy_feedback_without_both_fresh_write_and_success_status_stays_pending(self):
        for write,status in ((False,True),(True,False)):
            with self.subTest(write=write,status=status):
                self.clock.now=0
                self.desktop.click.reset_mock()
                self.targets('enabled')
                self.status.return_value='Account group Address copied.' if status else 'Account group'
                if write:
                    self.desktop.click.side_effect=lambda *_:setattr(self,'clipboard_value',self.address)
                else:
                    self.desktop.click.side_effect=None
                with self.assertRaisesRegex(RuntimeError,'bounded deadline'):
                    self.scope['request_main_row_copy'](seconds=.2)
                self.desktop.click.assert_called_once_with(1416,558)

    def test_reveal_is_horizontal_only_and_next_capture_rechecks_full_target(self):
        self.targets('clipped','enabled')
        reveal=mock.Mock()
        self.scope['reveal_main_row_copy']=reveal
        self.desktop.click.side_effect=self.complete_copy
        self.assertEqual(self.scope['request_main_row_copy'](seconds=1),self.address)
        reveal.assert_called_once_with(self.geometry)
        self.desktop.click.assert_called_once_with(1416,558)
        self.assertEqual(self.rows.call_count,3)

    def test_actual_reveal_uses_only_owned_horizontal_wheel_and_rejects_private_window(self):
        motion,button,flush=mock.Mock(),mock.Mock(),mock.Mock()
        self.scope['mod']=SimpleNamespace(T=SimpleNamespace(XTestFakeMotionEvent=motion,XTestFakeButtonEvent=button),X=SimpleNamespace(XFlush=flush))
        self.report['mainRowCopy']={'ethereumRow':[178,552,95,14]}
        self.scope['reveal_main_row_copy'](self.geometry)
        self.assertEqual(button.call_args_list,[mock.call(self.desktop.d,7,1,0),mock.call(self.desktop.d,7,0,0)])
        self.desktop.click.assert_not_called()
        self.desktop.secret_window.return_value=True
        with self.assertRaises(self.scope['PrivateWindowVisible']):self.scope['reveal_main_row_copy'](self.geometry)
        self.assertEqual(button.call_count,2)

    def test_exact_fixture_address_is_required_from_the_direct_ack_without_clipboard_reread(self):
        self.scope.update(request_main_row_copy=mock.Mock(return_value=self.address),text=lambda:'Account group',
                          wait=lambda predicate:predicate())
        self.report['mainRowCopy']={}
        for expected,succeeds in [('0x1111111111111111111111111111111111111111',True),('0x2222222222222222222222222222222222222222',False)]:
            with self.subTest(expected=expected):
                if succeeds:
                    self.scope['passed']=mock.Mock()
                    self.assertEqual(self.scope['main_row_copy'](expected),expected)
                else:
                    with self.assertRaisesRegex(RuntimeError,'independently derived'):
                        self.scope['main_row_copy'](expected)
        self.desktop.accept.assert_not_called()
        self.clipboard.assert_not_called()

    def test_malformed_direct_ack_or_wallet_navigation_cannot_pass_the_main_row_check(self):
        self.scope.update(request_main_row_copy=mock.Mock(),wait=lambda predicate:predicate(),passed=mock.Mock())
        self.report['mainRowCopy']={}
        for value in ('','0x1111','not-an-address','0x'+'1'*41):
            with self.subTest(value=value):
                self.scope['request_main_row_copy'].return_value=value
                with self.assertRaisesRegex(RuntimeError,'complete Ethereum address'):
                    self.scope['main_row_copy']()
        self.scope['request_main_row_copy'].return_value=self.address
        self.status.return_value='Account group Copy address'
        with self.assertRaisesRegex(RuntimeError,'opened a wallet page'):
            self.scope['main_row_copy']()
        self.scope['passed'].assert_not_called()
        self.desktop.accept.assert_not_called()


class WailsPublicKeyboardCopy(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        names=('address_selection_target','native_copy_shortcut','public_words_position')
        functions=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in names]
        self.desktop=SimpleNamespace(d=object())
        self.clock=SimpleNamespace(sleep=mock.Mock())
        self.events=mock.Mock()
        codes={b'Control_L':37,b'Shift_L':50,b'c':54,b'Insert':118}
        self.keyboard=SimpleNamespace(XStringToKeysym=mock.Mock(side_effect=lambda name:name),
                                      XKeysymToKeycode=mock.Mock(side_effect=lambda _,name:codes.get(name,0)),
                                      XFlush=mock.Mock())
        self.scope={'re':re,'desktop':self.desktop,'time':self.clock,
                    'mod':SimpleNamespace(X=self.keyboard,T=SimpleNamespace(XTestFakeKeyEvent=self.events))}
        exec(compile(ast.Module(body=functions,type_ignores=[]),'wails-public-keyboard-copy','exec'),self.scope)
        self.geometry=(80,100,1440,900)
        self.address='0x1111111111111111111111111111111111111111'

    def word(self,text,x,y,width=90,line=1):
        return ['5','1',str(line),'1','1','1',str(x),str(y),str(width),'18','99',text]

    def fixture(self,context):
        if context=='overview':
            labels=[self.word('Account',180,300,line=1),self.word('group',270,300,line=1),self.word('Ethereum',178,552,line=2)]
            top=552
        elif context=='wallet':
            labels=[self.word('Copy',1100,400,line=1),self.word('address',1195,400,line=1),
                    self.word('Receive',180,490,line=2),self.word('address',275,490,line=2)]
            top=490
        else:
            labels=[self.word('Copy',1100,520,line=1),self.word('address',1195,520,line=1)]
            top=520
        return labels+[self.word(self.address,500,top,700,line=3)],(850,top+9)

    def test_selection_targets_the_visible_public_address_in_each_actual_context(self):
        for context in ('overview','wallet','receive'):
            with self.subTest(context=context):
                rows,point=self.fixture(context)
                self.assertEqual(self.scope['address_selection_target'](context,rows,self.geometry),point)
                self.assertEqual(rows[-1][11],self.address)

    def test_wrong_context_or_an_address_from_another_row_cannot_supply_a_target(self):
        rows,_=self.fixture('overview')
        with self.assertRaisesRegex(RuntimeError,'Unknown public'):
            self.scope['address_selection_target']('password',rows,self.geometry)
        with self.assertRaisesRegex(RuntimeError,'left the wallet'):
            self.scope['address_selection_target']('wallet',rows,self.geometry)
        rows[-1][7]='650'
        with self.assertRaisesRegex(RuntimeError,'not uniquely visible'):
            self.scope['address_selection_target']('overview',rows,self.geometry)
        self.events.assert_not_called()

    def test_split_ocr_prefix_only_locates_selection_and_excludes_the_lower_summary(self):
        rows,point=self.fixture('receive')
        rows[-1][11]=self.address[:8];rows[-1][8]='82'
        rows.append(self.word(self.address,500,650,700,line=4))
        self.assertEqual(self.scope['address_selection_target']('receive',rows,self.geometry),(541,point[1]))
        rows[-2][11]='0x12';rows[-2][8]='35'
        with self.assertRaisesRegex(RuntimeError,'not uniquely visible'):
            self.scope['address_selection_target']('receive',rows,self.geometry)

    def test_ambiguous_address_tokens_clipped_tokens_and_unowned_bounds_are_rejected(self):
        rows,_=self.fixture('overview')
        for extra in (self.word('Ethereum',178,552,line=4),self.word(self.address,510,552,700,line=4)):
            with self.subTest(extra=extra[11]),self.assertRaisesRegex(RuntimeError,'not uniquely visible'):
                self.scope['address_selection_target']('overview',rows+[extra],self.geometry)
        for left in ('70','1000'):
            clipped=[list(row) for row in rows];clipped[-1][6]=left
            with self.subTest(left=left),self.assertRaisesRegex(RuntimeError,'not uniquely visible'):
                self.scope['address_selection_target']('overview',clipped,self.geometry)
        with self.assertRaisesRegex(RuntimeError,'owned display'):
            self.scope['address_selection_target']('overview',rows,(80,100,1600,1100))
        self.events.assert_not_called()

    def test_supported_native_shortcuts_release_all_keys_in_reverse_order(self):
        for shortcut,codes in (('Ctrl+C',(37,54)),('Ctrl+Shift+C',(37,50,54)),('Ctrl+Insert',(37,118))):
            with self.subTest(shortcut=shortcut):
                self.events.reset_mock();self.keyboard.XFlush.reset_mock()
                self.scope['native_copy_shortcut'](shortcut)
                self.assertEqual(self.events.call_args_list,
                                 [mock.call(self.desktop.d,code,1,0) for code in codes]+
                                 [mock.call(self.desktop.d,code,0,0) for code in reversed(codes)])
                self.keyboard.XFlush.assert_called_once_with(self.desktop.d)

    def test_unsupported_or_unmapped_shortcuts_never_send_partial_key_input(self):
        with self.assertRaisesRegex(RuntimeError,'Unsupported public'):
            self.scope['native_copy_shortcut']('Ctrl+V')
        self.keyboard.XStringToKeysym.assert_not_called()
        self.keyboard.XKeysymToKeycode.side_effect=lambda _,name:0 if name==b'Insert' else 37
        with self.assertRaisesRegex(RuntimeError,'lacks a required copy key'):
            self.scope['native_copy_shortcut']('Ctrl+Insert')
        self.events.assert_not_called()
        self.keyboard.XFlush.assert_not_called()

    def test_failed_key_press_still_releases_already_pressed_modifiers(self):
        def inject(_,code,pressed,__):
            if code==50 and pressed:raise RuntimeError('owned test key injection failed')
        self.events.side_effect=inject
        with self.assertRaisesRegex(RuntimeError,'owned test key injection failed'):
            self.scope['native_copy_shortcut']('Ctrl+Shift+C')
        self.assertEqual(self.events.call_args_list,[mock.call(self.desktop.d,37,1,0),
                                                    mock.call(self.desktop.d,50,1,0),
                                                    mock.call(self.desktop.d,37,0,0)])
        self.keyboard.XFlush.assert_called_once_with(self.desktop.d)


class WailsNearTokenPresentation(unittest.TestCase):
    def setUp(self):
        self.tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        names=('near_native_token_row','wait_near_token_import_failure','public_words_position')
        functions=[node for node in self.tree.body if isinstance(node,ast.FunctionDef) and node.name in names]
        self.clock=SimpleNamespace(now=0)
        self.clock.monotonic=lambda:self.clock.now
        self.clock.sleep=lambda seconds:setattr(self.clock,'now',self.clock.now+seconds)
        self.desktop=SimpleNamespace(secret_window=mock.Mock(return_value=False),
                                     wait=mock.Mock(return_value=(17,'CryptoWallet')),
                                     focus=mock.Mock(),key=mock.Mock())
        self.status=mock.Mock(return_value=False)
        self.visible=mock.Mock(return_value='NEAR mainnet · NEAR')
        self.scope={'time':self.clock,'desktop':self.desktop,'app':None,'text':self.visible,
                    'public_status_contains':self.status}
        exec(compile(ast.Module(body=functions,type_ignores=[]),'near-token-public-observation','exec'),self.scope)
        self.bounds=(80,32,1440,1036)

    def words(self,text,x,y,line):
        return [['5','1',str(line),'1','1',str(index),str(x+60*index),str(y),'50','18','99',word]
                for index,word in enumerate(text.split())]

    def native_table(self):
        return (self.words('Assets on NEAR mainnet',130,500,1)
                +self.words('Token',130,550,2)+self.words('Balance',700,550,2)+self.words('Decimals',1100,550,2)
                +self.words('NEAR',140,600,3)+self.words('Unknown',700,600,3)+self.words('24',1100,600,3)
                +self.words('Track a NEAR token',130,690,4))

    def test_native_row_requires_exact_near_table_columns_and_its_own_unknown_balance(self):
        self.assertEqual(self.scope['near_native_token_row'](self.native_table(),self.bounds),609)
        for word in ('Token','Balance','Decimals','Unknown'):
            rows=[row for row in self.native_table() if row[11]!=word]
            with self.subTest(word=word):self.assertIsNone(self.scope['near_native_token_row'](rows,self.bounds))
        rows=self.native_table()
        next(row for row in rows if row[11]=='Unknown')[7]='640'
        self.assertIsNone(self.scope['near_native_token_row'](rows,self.bounds))

    def test_wrong_network_extra_token_or_split_columns_cannot_pass_native_only_observation(self):
        wrong=self.native_table()
        next(row for row in wrong if row[11]=='mainnet')[11]='testnet'
        split=self.native_table()
        next(row for row in split if row[11]=='Balance')[7]='570'
        extra=self.native_table()+self.words('SPAM',140,645,5)+self.words('Unknown',700,645,5)
        for rows in (wrong,split,extra):
            with self.subTest(rows=rows):self.assertIsNone(self.scope['near_native_token_row'](rows,self.bounds))

    def test_top_tab_labels_or_out_of_bounds_text_cannot_supply_the_native_row(self):
        rows=[row for row in self.native_table() if row[11]!='NEAR' or int(row[7])!=600]
        rows+=self.words('NEAR',140,95,10)+self.words('NEAR',1540,600,11)
        self.assertIsNone(self.scope['near_native_token_row'](rows,self.bounds))
        for bounds in ((-1,0,1440,900),(80,32,1600,1036),(80,32,1440,1200)):
            with self.subTest(bounds=bounds),self.assertRaisesRegex(RuntimeError,'owned display'):
                self.scope['near_native_token_row'](self.native_table(),bounds)

    def test_real_metadata_failure_needs_explicit_error_instead_of_busy_completed_or_old_status(self):
        self.status.side_effect=[False,False,True]
        self.scope['wait_near_token_import_failure'](seconds=1)
        self.assertEqual(self.status.call_args_list,[mock.call('Request failed or is unavailable.')]*3)
        self.desktop.key.assert_not_called()
        self.status.side_effect=None
        self.status.return_value=False
        self.visible.return_value='Request completed. Waiting for wallet confirmation…'
        with self.assertRaisesRegex(RuntimeError,'bounded deadline'):
            self.scope['wait_near_token_import_failure'](seconds=.3)

    def test_unexpected_dialog_is_cancelled_in_owned_main_window_without_reading_or_accepting(self):
        self.desktop.secret_window.return_value=True
        with self.assertRaisesRegex(RuntimeError,'unexpectedly requested a dialog; cancelled'):
            self.scope['wait_near_token_import_failure'](seconds=1)
        self.desktop.focus.assert_called_once_with(17)
        self.desktop.key.assert_called_once_with('Escape')
        self.visible.assert_not_called()
        self.status.assert_not_called()

    def test_prepared_operation_or_exited_app_cannot_be_counted_as_metadata_failure(self):
        self.status.return_value=True
        for visible in ('Prepared payment','Original prepared token registration'):
            self.visible.return_value=visible
            with self.subTest(visible=visible),self.assertRaisesRegex(RuntimeError,'prepared a payment or registration'):
                self.scope['wait_near_token_import_failure'](seconds=1)
        self.status.assert_not_called()
        self.scope['app']=SimpleNamespace(poll=lambda:23)
        with self.assertRaisesRegex(RuntimeError,'exited'):
            self.scope['wait_near_token_import_failure'](seconds=1)

    def actual_parser_and_targeted_route(self):
        nodes=[]
        for node in self.tree.body:
            if isinstance(node,ast.Assign) and any(isinstance(target,ast.Name) and target.id in ('parser','mode') for target in node.targets):
                nodes.append(node)
            elif isinstance(node,ast.Expr) and isinstance(node.value,ast.Call) and isinstance(node.value.func,ast.Attribute) and isinstance(node.value.func.value,ast.Name) and node.value.func.value.id in ('parser','mode'):
                nodes.append(node)
        scope={'argparse':argparse,'pathlib':pathlib,'ROOT':ROOT,'__doc__':'Owned test-mode routing'}
        exec(compile(ast.Module(body=nodes,type_ignores=[]),'actual-wallet-acceptance-arguments','exec'),scope)
        routes=[node.test for node in ast.walk(self.tree) if isinstance(node,ast.If) and isinstance(node.test,ast.BoolOp)
                and any(isinstance(item,ast.Attribute) and item.attr=='near_tokens_only' for item in node.test.values)
                and any(isinstance(item,ast.Attribute) and item.attr=='quick_import' for item in node.test.values)]
        self.assertEqual(len(routes),1)
        return scope['parser'],compile(ast.Expression(routes[0]),'actual-targeted-fixture-route','eval')

    def test_all_explicit_targeted_modes_keep_their_fixture_route_and_default_keeps_full_recovery(self):
        parser,route=self.actual_parser_and_targeted_route()
        self.assertFalse(eval(route,{'args':parser.parse_args([])}))
        for flag in ('--quick-import','--staking-only','--tokens-only','--near-tokens-only','--history-layout-only','--arbitrum-copy-only','--appearance-only','--main-row-copy-only','--recovery-layout-only','--clipboard-only'):
            with self.subTest(flag=flag):self.assertTrue(eval(route,{'args':parser.parse_args([flag])}))

    def test_near_token_mode_cannot_silently_combine_with_another_acceptance_mode(self):
        parser,_=self.actual_parser_and_targeted_route()
        for flag in ('--tokens-only','--staking-only','--clipboard-only'):
            with self.subTest(flag=flag),contextlib.redirect_stderr(io.StringIO()),self.assertRaises(SystemExit):
                parser.parse_args(['--near-tokens-only',flag])


class WailsNearNavigation(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        names=('near_navigation_state','wait_near_navigation','public_words_position')
        functions=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in names]
        self.clock=SimpleNamespace(now=0)
        self.clock.monotonic=lambda:self.clock.now
        self.clock.sleep=lambda seconds:setattr(self.clock,'now',self.clock.now+seconds)
        self.desktop=SimpleNamespace(secret_window=mock.Mock(return_value=False),
                                     wait=mock.Mock(return_value=(1,'CryptoWallet')),
                                     geometry=mock.Mock(return_value=(80,100,1440,900)),
                                     focus=mock.Mock(),click=mock.Mock(),
                                     park_pointer=mock.Mock())
        self.rows=mock.Mock()
        self.click=self.desktop.click
        self.report={}
        self.scope={'time':self.clock,'re':re,'desktop':self.desktop,'app':None,
                    'public_rows':self.rows,'click':self.click,'report':self.report,
                    'verify_near_public_contract':mock.Mock(return_value=True),'stage':mock.Mock(),
                    'save':mock.Mock(),'PrivateWindowVisible':type('PrivateWindowVisible',(RuntimeError,),{})}
        exec(compile(ast.Module(body=functions,type_ignores=[]),'near-public-navigation-helper','exec'),self.scope)

    def row(self,words,y,line):
        return [['5','1',str(line),'1','1',str(i),str(120+40*i),str(y),'35','20','99',word]
                for i,word in enumerate(words.split())]

    def overview(self):
        return self.row('Account group',280,1)+self.row('NEAR',650,2)

    def wallet(self,network='NEAR'):
        return (self.row(network+' mainnet · '+network,280,1)+self.row('Copy address',400,2)
                +(self.row('Wallet Staking',550,3) if network=='NEAR' else []))

    def staking(self):
        return self.wallet()+self.row('AstroStakers',650,4)+self.row('astro-stakers.poolv1.near',690,5)

    def test_same_line_exact_heading_accepts_ocr_separator_without_weakening_words(self):
        for separator in ('·','-','•','.'):
            rows=self.row('NEAR mainnet '+separator+' NEAR',280,1)+self.row('Copy address',400,2)+self.row('Wallet Staking',550,3)
            with self.subTest(separator=separator):
                self.assertTrue(self.scope['near_navigation_state'](rows)['wallet'])
        for heading in ('NEAR testnet · NEAR','Ethereum mainnet · NEAR','NEAR mainnet · ETH'):
            rows=self.row(heading,280,1)+self.row('Copy address',400,2)+self.row('Wallet Staking',550,3)
            with self.subTest(heading=heading):
                self.assertFalse(self.scope['near_navigation_state'](rows)['wallet'])

    def test_near_heading_words_cannot_be_combined_across_ocr_lines(self):
        rows=self.row('NEAR mainnet',280,1)+self.row('NEAR',330,2)+self.row('Copy address',400,3)+self.row('Wallet Staking',550,4)
        self.assertFalse(self.scope['near_navigation_state'](rows)['wallet'])

    def test_dropped_first_wallet_selection_is_reobserved_and_retried_without_other_actions(self):
        self.rows.side_effect=[self.overview()]*9+[self.wallet()]
        self.scope['wait_near_navigation']('wallet',seconds=2)
        self.assertEqual(self.click.call_args_list,[mock.call(137,660)]*2)
        self.assertEqual(self.report['nearNavigation']['wallet']['attempts'],2)
        self.assertTrue(self.report['nearNavigation']['wallet']['observed']['wallet'])
        self.assertEqual(self.desktop.park_pointer.call_count,2)

    def test_staking_waits_for_exact_near_wallet_then_complete_validator_and_contract(self):
        partial=self.wallet()+self.row('AstroStakers',650,4)
        self.rows.side_effect=[self.wallet()]*9+[partial,self.staking()]
        self.scope['wait_near_navigation']('staking',seconds=2)
        self.assertEqual(self.click.call_args_list,[mock.call(177,560)]*2)
        self.assertEqual(self.report['nearNavigation']['staking']['attempts'],2)
        self.assertTrue(self.report['nearNavigation']['staking']['observed']['contract'])

    def test_another_wallet_or_near_word_in_top_tabs_is_never_accepted_as_near(self):
        for rows in (self.wallet('ETH')+self.row('NEAR',205,10),
                     self.wallet('ETH')+self.row('AstroStakers astro-stakers.poolv1.near',650,11),
                     self.row('NEAR mainnet · NEAR Copy address',280,1)+self.row('Wallet',500,2)+self.row('Staking',550,3)):
            with self.subTest(rows=rows):
                self.clock.now=0;self.rows.return_value=rows
                with self.assertRaisesRegex(RuntimeError,'bounded deadline'):
                    self.scope['wait_near_navigation']('staking',seconds=.3)
                self.assertFalse(self.report['nearNavigation']['staking']['observed']['wallet'])
        self.click.assert_not_called()

    def test_unexpected_private_window_aborts_before_pixels_or_another_click(self):
        self.rows.return_value=self.overview()
        self.desktop.secret_window.side_effect=[False,False,True]
        with self.assertRaises(self.scope['PrivateWindowVisible']):
            self.scope['wait_near_navigation']('wallet',seconds=2)
        self.rows.assert_called_once_with()
        self.click.assert_called_once_with(137,660)

    def test_missing_contract_cannot_pass_or_retry_an_operation(self):
        self.rows.return_value=self.wallet()+self.row('AstroStakers astro-stakers.poolv1.near.evil',650,4)
        self.scope['verify_near_public_contract'].return_value=False
        # The complete address is a separate exact OCR token, not a prefix.
        with self.assertRaisesRegex(RuntimeError,'could not be verified exactly'):
            self.scope['wait_near_navigation']('staking',seconds=.3)
        self.assertLess(self.clock.now,.5)
        self.assertFalse(self.report['nearNavigation']['staking']['observed']['contract'])
        self.click.assert_not_called()

    def test_unknown_public_page_cannot_select_a_near_label(self):
        for rows in (self.row('Settings NEAR',280,1),self.wallet('ETH')):
            with self.subTest(rows=rows):
                self.clock.now=0;self.rows.return_value=rows
                with self.assertRaisesRegex(RuntimeError,'bounded deadline'):
                    self.scope['wait_near_navigation']('wallet',seconds=.3)
        self.click.assert_not_called()

    def test_private_window_after_observation_blocks_pointer_input(self):
        self.rows.return_value=self.overview()
        self.desktop.secret_window.side_effect=[False,True]
        with self.assertRaises(self.scope['PrivateWindowVisible']):
            self.scope['wait_near_navigation']('wallet',seconds=2)
        self.rows.assert_called_once_with()
        self.click.assert_not_called()

    def test_unknown_target_is_rejected_before_any_capture_or_action(self):
        for target in ('Stake','Withdraw all','Send','Refresh staking state','NEAR'):
            with self.subTest(target=target),self.assertRaisesRegex(RuntimeError,'Unexpected'):
                self.scope['wait_near_navigation'](target)
        self.rows.assert_not_called();self.click.assert_not_called()

    def test_exit_does_not_become_a_retry_or_success(self):
        self.scope['app']=SimpleNamespace(poll=lambda:23)
        with self.assertRaisesRegex(RuntimeError,'exited'):
            self.scope['wait_near_navigation']('wallet')
        self.rows.assert_not_called();self.click.assert_not_called()


class WailsPublicContractOCR(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        names=('near_contract_crop','decode_near_contract','verify_near_public_contract')
        functions=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in names]
        self.folder=tempfile.TemporaryDirectory(prefix='wallet-public-contract-')
        self.addCleanup(self.folder.cleanup)
        self.desktop=SimpleNamespace(wait=mock.Mock(return_value=(1,'CryptoWallet')),
                                     geometry=mock.Mock(return_value=(80,100,1440,900)))
        self.report={}
        self.rows=mock.Mock()
        self.scroll=mock.Mock()
        self.scope={'Image':Image,'ImageOps':ImageOps,'io':io,'hashlib':hashlib,
                    'time':time,'subprocess':subprocess,'desktop':self.desktop,
                    'appearance':{'theme':'light'},'report':self.report,
                    'public_rows':self.rows,'scroll_public':self.scroll,
                    'REPORTS':pathlib.Path(self.folder.name),
                    'args':SimpleNamespace(report_name='owned-contract')}
        exec(compile(ast.Module(body=functions,type_ignores=[]),'wails-public-contract-helper','exec'),self.scope)
        self.fixture=ROOT/'tests/e2e/fixtures/near-public-contract.png'

    def label(self,y=700):
        return [['5','1','1','1','1',str(i),str(160+i*55),str(y),'50','18','99',word]
                for i,word in enumerate(('Exact','contract'))]

    def test_actual_guarded_public_fixture_has_exact_identity_without_decoder_hints(self):
        metadata=json.loads(self.fixture.with_suffix('.json').read_text())
        self.assertEqual(hashlib.sha256(self.fixture.read_bytes()).hexdigest(),metadata['fixtureSHA256'])
        with Image.open(self.fixture) as picture:
            self.assertTrue(self.scope['decode_near_contract'](picture,time.monotonic()+10))
        self.assertEqual(list(pathlib.Path(self.folder.name).iterdir()),[])

    def test_actual_decoder_rejects_visually_confusable_wrong_public_contracts(self):
        metadata=json.loads(self.fixture.with_suffix('.json').read_text())
        for name,negative in metadata['negativeFixtures'].items():
            path=self.fixture.parent/name
            with self.subTest(fixture=name):
                self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(),negative['fixtureSHA256'])
                with Image.open(path) as picture:
                    self.assertFalse(self.scope['decode_near_contract'](picture,time.monotonic()+10))

    def test_only_whitespace_can_change_in_exact_contract_decoder(self):
        for value,expected in ((b'astro-stakers.poolv1.near\n',True),
                               (b'astro-stakers. poolv1.near\n',True),
                               (b'astro-stakers.poolvl.near\n',False),
                               (b'astro-stakers.poolvI.near\n',False),
                               (b'astro-stakers.poolv7.near\n',False),
                               (b'astro-stakers.poolv1.near.evil\n',False),
                               (b'astro-stakers.poolv1.near other\n',False),
                               (b'astro.stakers.poolv1.near\n',False)):
            with self.subTest(value=value),mock.patch.object(subprocess,'check_output',return_value=value) as decoder:
                self.assertEqual(self.scope['decode_near_contract'](Image.new('RGB',(523,52),'white'),time.monotonic()+10),expected)
                for call in decoder.call_args_list:
                    self.assertEqual(call.args[0],['tesseract','stdin','stdout','--psm','7'])
                    self.assertLessEqual(call.kwargs['timeout'],10)
        self.assertTrue(all(set(row)=={'scale','psm','matched','recognizedBytes','recognizedSHA256'} for row in self.report['nearContractOCR']))

    def test_decoder_timeout_or_expired_budget_cannot_become_a_match(self):
        with mock.patch.object(subprocess,'check_output') as decoder:
            self.assertFalse(self.scope['decode_near_contract'](Image.new('RGB',(523,52)),time.monotonic()-1))
            decoder.assert_not_called()
        for error in (subprocess.TimeoutExpired('tesseract',1),subprocess.CalledProcessError(1,'tesseract')):
            with mock.patch.object(subprocess,'check_output',side_effect=error),self.assertRaises(type(error)):
                self.scope['decode_near_contract'](Image.new('RGB',(523,52)),time.monotonic()+1)
        self.assertNotIn('nearContractOCR',self.report)

    def test_crop_requires_unique_readonly_label_and_complete_public_viewport(self):
        picture=Image.new('RGB',(1600,1100),'white')
        crop,bounds=self.scope['near_contract_crop'](self.label(),picture)
        self.assertEqual(bounds,(157,720,680,772))
        self.assertEqual(crop.size,(523,52))
        self.assertIsNone(self.scope['near_contract_crop']([],picture))
        self.assertIsNone(self.scope['near_contract_crop'](self.label(930),picture))
        self.assertIsNone(self.scope['near_contract_crop'](self.label(),Image.new('RGB',(500,700))))
        with self.assertRaisesRegex(RuntimeError,'ambiguous'):
            self.scope['near_contract_crop'](self.label()+self.label(800),picture)
        self.desktop.geometry.return_value=(500,100,1000,900)
        self.assertIsNone(self.scope['near_contract_crop'](self.label(),picture))

    def test_failed_decoder_saves_only_immutable_anchored_public_region_and_hash(self):
        picture=Image.new('RGB',(1600,1100),'white')
        self.rows.return_value=(self.label(),picture)
        self.scope['decode_near_contract']=mock.Mock(return_value=False)
        self.assertFalse(self.scope['verify_near_public_contract'](time.monotonic()+10))
        self.rows.assert_called_once_with(with_picture=True)
        self.scroll.assert_not_called()
        files=list(pathlib.Path(self.folder.name).iterdir())
        self.assertEqual([path.name for path in files],['owned-contract-near-public-contract.png'])
        with Image.open(files[0]) as saved:self.assertEqual(saved.size,(523,52))
        self.assertEqual(self.report['nearContractPublicRegion']['failureArtifactSHA256'],hashlib.sha256(files[0].read_bytes()).hexdigest())

    def test_clipped_readonly_line_scrolls_without_navigation_or_transaction_actions(self):
        picture=Image.new('RGB',(1600,1100),'white')
        self.rows.side_effect=[(self.label(930),picture),(self.label(),picture)]
        self.scope['decode_near_contract']=mock.Mock(return_value=True)
        self.assertTrue(self.scope['verify_near_public_contract'](time.monotonic()+10))
        self.scroll.assert_called_once_with(5,2)
        self.assertEqual(list(pathlib.Path(self.folder.name).iterdir()),[])

    def test_private_guard_failure_cannot_create_a_public_failure_artifact(self):
        private=type('PrivateWindowVisible',(RuntimeError,),{})
        self.rows.side_effect=private('private pixels refused')
        self.scope['decode_near_contract']=mock.Mock()
        with self.assertRaises(private):self.scope['verify_near_public_contract'](time.monotonic()+10)
        self.scope['decode_near_contract'].assert_not_called()
        self.scroll.assert_not_called()
        self.assertEqual(list(pathlib.Path(self.folder.name).iterdir()),[])


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
                    ImageDraw.Draw(picture).rounded_rectangle((76,top,703,top+53),radius=6,fill=colors['input'],outline=colors['border'])
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

    def test_focused_empty_input_caret_cannot_split_a_real_entry_into_missing_fields(self):
        for theme in ('light','dark'):
            with self.subTest(theme=theme):
                self.scope['appearance']['theme']=theme
                colors=self.scope['palette']()
                picture=Image.new('RGB',(800,700),colors['surface'])
                for top in (250,360):
                    ImageDraw.Draw(picture).rounded_rectangle((76,top,703,top+53),radius=6,fill=colors['input'],outline=colors['border'])
                # The old x+40 probe falls exactly on the ordinary empty
                # input caret (26px body inset + border +12px text padding).
                ImageDraw.Draw(picture).line((90,265,90,289),fill='white' if theme=='dark' else 'black')
                self.assertEqual(self.scope['masked_field_rectangles'](picture,(50,30,680,528),colors['input']),
                                 [(250,303),(360,413)])

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

    def test_one_to_one_public_ocr_preserves_exact_word_coordinates_and_guard(self):
        decoder=self.scope['subprocess'].check_output
        decoder.return_value=b'header\n5\t1\t1\t1\t1\t1\t1\t2\t3\t4\t99\tUse\n'
        rows=self.scope['public_rows'](ocr_scale=1)
        self.assertEqual(rows[0][6:10],['1','2','3','4'])
        self.assertEqual(self.events,['grab','enumerate','pixels','ungrab'])
        self.assertEqual(decoder.call_args.args[0],['tesseract','stdin','stdout','--psm','11','tsv'])
        with Image.open(io.BytesIO(decoder.call_args.kwargs['input'])) as capture:self.assertEqual(capture.size,(4,4))

    def test_unexpected_public_ocr_scale_is_rejected_before_any_pixels(self):
        for scale in (0,3,-1):
            with self.subTest(scale=scale),self.assertRaisesRegex(RuntimeError,'OCR scale'):
                self.scope['public_rows'](ocr_scale=scale)
        self.desktop.image.assert_not_called()
        self.scope['subprocess'].check_output.assert_not_called()
        self.assertEqual(self.events,[])

    def test_geometry_and_original_public_pixels_are_captured_under_the_same_guard(self):
        def geometry(window):
            self.assertEqual(window,1)
            self.assertEqual(self.desktop._server_grab_depth,1)
            self.assertNotIn('ungrab',self.events)
            self.events.append('geometry')
            return (0,0,4,4)
        self.desktop.geometry=mock.Mock(side_effect=geometry)
        rows,picture,geometry=self.scope['public_rows'](contrast=True,with_picture=True,with_geometry=True)
        self.assertEqual(rows,[])
        self.assertEqual(geometry,(0,0,4,4))
        self.assertEqual(picture.mode,'RGB','the returned immutable picture must precede any contrast transform')
        self.assertEqual(self.events,['grab','enumerate','enumerate','geometry','pixels','ungrab'])
        self.assertEqual(self.desktop._server_grab_depth,0)

    def test_geometry_capture_never_reads_private_pixels_or_window_geometry(self):
        self.desktop.secret_window=mock.Mock(return_value=True)
        self.desktop.geometry=mock.Mock()
        with self.assertRaises(self.scope['PrivateWindowVisible']):
            self.scope['public_rows'](with_picture=True,with_geometry=True)
        self.desktop.geometry.assert_not_called()
        self.desktop.image.assert_not_called()
        self.scope['subprocess'].check_output.assert_not_called()
        self.assertEqual(self.events,['grab','ungrab'])
        self.assertEqual(self.desktop._server_grab_depth,0)

    def test_geometry_capture_rejects_ambiguous_public_windows_before_pixels(self):
        self.desktop.windows=mock.Mock(return_value=[(1,'CryptoWallet'),(2,'CryptoWallet · another')])
        self.desktop.geometry=mock.Mock()
        with self.assertRaisesRegex(RuntimeError,'not unique'):
            self.scope['public_rows'](with_picture=True,with_geometry=True)
        self.desktop.geometry.assert_not_called()
        self.desktop.image.assert_not_called()
        self.assertEqual(self.events,['grab','ungrab'])
        self.assertEqual(self.desktop._server_grab_depth,0)

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
                                       prompt_geometry=mock.Mock(return_value=(460, 220, 680, 660)), click=mock.Mock())
        self.rows = mock.Mock()
        self.motion, self.button, self.flush = mock.Mock(), mock.Mock(), mock.Mock()
        self.scope = {'desktop': self.desktop, 'private_rows': self.rows,
                      'mod': SimpleNamespace(T=SimpleNamespace(XTestFakeMotionEvent=self.motion, XTestFakeButtonEvent=self.button),
                                             X=SimpleNamespace(XFlush=self.flush)),
                      'time': SimpleNamespace(sleep=mock.Mock())}
        exec(compile(ast.Module(body=[function], type_ignores=[]), 'wails-private-copy-test-helper', 'exec'), self.scope)

    def test_recovery_copy_scrolls_and_clicks_its_actual_label_in_the_main_window(self):
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
        for prefix, words in (('Verify receive address', 'Copy phrase'),
                              ('Back up recovery phrase', 'Done'),
                              ('Unlock local wallet', 'Continue')):
            with self.subTest(prefix=prefix, words=words):
                with self.assertRaisesRegex(RuntimeError, 'Unexpected private control'):
                    self.scope['private_click'](prefix, words)
        self.rows.assert_not_called()
        self.desktop.wait.assert_not_called()
        self.desktop.click.assert_not_called()
        self.motion.assert_not_called()

    def test_initial_setup_copy_is_an_ordinary_explicit_dialog_action_too(self):
        copy=['5','1','1','1','1','1','100','200','40','20','99','Copy']
        phrase=copy[:];phrase[11]='phrase'
        self.rows.return_value=[copy,phrase]
        self.scope['private_click']('Record recovery phrase','Copy phrase')
        self.rows.assert_called_once_with('Record recovery phrase')
        self.desktop.click.assert_called_once_with(580,430)
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


class WailsEVMAssetSelector(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        definition=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='evm_asset_button_target')
        self.colors={'button':(233,238,245),'disabled':(245,247,250)}
        scope={'palette':lambda:self.colors}
        exec(compile(ast.Module(body=[definition],type_ignores=[]),'asset-selector-helper','exec'),scope)
        self.target=scope['evm_asset_button_target']
        self.picture=Image.new('RGB',(400,240),'white')
        for yy in range(100,144):
            for xx in range(40,126):self.picture.putpixel((xx,yy),self.colors['button'])
        def word(text,left,width):return ['5','1','1','1','1','1',str(left),'114',str(width),'14','95',text]
        self.rows=[word('USDT',55,45),word('Unknown',145,75),word('USDT',230,45)]
        self.bounds=(20,20,360,200)

    def locate(self,require_enabled=True):
        return self.target('USDT',self.rows,self.picture,self.bounds,80,require_enabled)

    def test_exact_symbol_in_bounded_button_left_of_balance_is_selected(self):
        self.assertEqual(self.locate(),(77,121))

    def test_value_only_balance_cell_does_not_repeat_the_token_symbol(self):
        self.rows=self.rows[:2]
        self.assertEqual(self.locate(),(77,121))

    def test_ocr_box_merged_with_border_is_rejected_without_weakening_clean_glyph_bounds(self):
        clean=list(self.rows[0])
        self.rows[0][7]='99';self.rows[0][9]='44'
        self.assertIsNone(self.locate())
        self.rows[0]=clean
        self.assertEqual(self.locate(),(77,121))

    def test_balance_glyph_cannot_replace_missing_button_text(self):
        self.rows=self.rows[1:]
        self.assertIsNone(self.locate())

    def test_wrong_symbol_is_not_repaired_or_substituted(self):
        self.rows[0][11]='usSDT'
        self.assertIsNone(self.locate())

    def test_candidate_outside_owned_bounds_is_rejected(self):
        self.rows[0][7]='4'
        self.assertIsNone(self.locate())

    def test_disabled_selector_can_be_observed_but_not_clicked(self):
        for yy in range(100,144):
            for xx in range(40,126):self.picture.putpixel((xx,yy),self.colors['disabled'])
        self.assertIsNone(self.locate())
        self.assertEqual(self.locate(False),(77,121))

    def test_unbounded_fill_is_not_a_button(self):
        self.picture=Image.new('RGB',(400,240),self.colors['button'])
        self.assertIsNone(self.locate())


class WailsEVMContractTooltip(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        definition=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='exact_evm_contract_text')
        scope={'re':re}
        exec(compile(ast.Module(body=[definition],type_ignores=[]),'exact-contract-helper','exec'),scope)
        self.decode=scope['exact_evm_contract_text']
        self.contract='0xdAC17F958D2ee523a2206206994597C13D831ec7'

    def decimal(self,rows,picture,bounds,row_y,expected):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        definition=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='public_decimal_cell')
        scope={'ImageOps':ImageOps,'Image':Image,'io':io,'subprocess':subprocess}
        exec(compile(ast.Module(body=[definition],type_ignores=[]),'decimal-cell-helper','exec'),scope)
        return scope['public_decimal_cell'](rows,picture,bounds,row_y,expected)

    def test_full_exact_contract_survives_only_spacing_and_hex_case(self):
        self.assertTrue(self.decode(('ERC-20 contract · ethereum-mainnet\n'+self.contract).encode(),self.contract))
        self.assertTrue(self.decode(('0x'+self.contract[2:].upper()).encode(),self.contract))
        self.assertTrue(self.decode((self.contract[:22]+' '+self.contract[22:]).encode(),self.contract))

    def test_confusable_characters_are_never_repaired(self):
        for value in [self.contract.replace('1','l',1),self.contract.replace('1','I',1),self.contract.replace('0x','Ox',1)]:
            with self.subTest(value=value):self.assertFalse(self.decode(value.encode(),self.contract))

    def test_partial_longer_prefixed_or_suffixed_contracts_are_rejected(self):
        for value in [self.contract[:-1],self.contract+'0','0'+self.contract,'g'+self.contract,self.contract+'g']:
            with self.subTest(value=value):self.assertFalse(self.decode(value.encode(),self.contract))

    def test_wrong_address_and_ambiguous_multiple_contracts_are_rejected(self):
        self.assertFalse(self.decode((self.contract[:-1]+'8').encode(),self.contract))
        self.assertFalse(self.decode((self.contract+'\n'+self.contract).encode(),self.contract))

    def test_precision_cell_requires_exact_unrestricted_integer_ocr(self):
        header=['5','1','1','1','1','1','90','20','80','14','95','Decimals']
        picture=Image.new('RGB',(220,150),'white')
        with mock.patch.object(subprocess,'check_output',return_value=b'16\n') as decoder:
            self.assertFalse(self.decimal([header],picture,(0,0,220,150),70,'6'))
            self.assertEqual(decoder.call_count,2)
            for call in decoder.call_args_list:self.assertEqual(call.args[0],['tesseract','stdin','stdout','--psm','7'])
        with mock.patch.object(subprocess,'check_output',return_value=b'6\n'):
            self.assertTrue(self.decimal([header],picture,(0,0,220,150),70,'6'))

    def test_precision_cell_without_a_unique_header_never_reaches_ocr(self):
        with mock.patch.object(subprocess,'check_output') as decoder:
            with self.assertRaisesRegex(RuntimeError,'Unique asset precision'):
                self.decimal([],Image.new('RGB',(220,150),'white'),(0,0,220,150),70,'6')
            decoder.assert_not_called()

    def test_contract_block_requires_observed_label_and_bounded_tooltip_fill(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        definition=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='evm_contract_tooltip_block')
        scope={'palette':lambda:{'chrome':(237,241,246)}}
        exec(compile(ast.Module(body=[definition],type_ignores=[]),'tooltip-block-helper','exec'),scope)
        block=scope['evm_contract_tooltip_block']
        picture=Image.new('RGB',(400,320),'white')
        ImageDraw.Draw(picture).rectangle((60,150,299,224),fill=(237,241,246))
        def word(text,left):return ['5','1','1','1','1','1',str(left),'167','60','14','95',text]
        rows=[word('ERC-20',75),word('contract',145)]
        captured=block(rows,picture,(0,0,400,320),(100,260))
        self.assertEqual(captured[1],(60,150,300,225))
        self.assertEqual(captured[0].size,(240,75))
        self.assertIsNone(block(rows+rows,picture,(0,0,400,320),(100,260)))
        rows[0][11]='ERC2O'
        self.assertIsNone(block(rows,picture,(0,0,400,320),(100,260)))
        with self.assertRaisesRegex(RuntimeError,'owned bounds'):
            block(rows,picture,(0,0,401,320),(100,260))


class WailsEVMContractDecoder(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        names=('palette','exact_evm_contract_text','evm_contract_address_line','evm_dotted_zero_structure',
               'decode_evm_contract_tooltip','verify_evm_public_contract')
        functions=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in names]
        self.folder=tempfile.TemporaryDirectory(prefix='wallet-public-evm-contract-')
        self.addCleanup(self.folder.cleanup)
        self.report={}
        self.capture=mock.Mock()
        self.block=mock.Mock()
        self.scope={'Image':Image,'ImageOps':ImageOps,'io':io,'time':time,'hashlib':hashlib,'re':re,
                    'subprocess':subprocess,'appearance':{'theme':'light'},'report':self.report,
                    'public_rows':self.capture,'evm_contract_tooltip_block':self.block,
                    'REPORTS':pathlib.Path(self.folder.name),'args':SimpleNamespace(report_name='owned-contract')}
        exec(compile(ast.Module(body=functions,type_ignores=[]),'wails-public-evm-helper','exec'),self.scope)
        self.metadata=json.loads((ROOT/'tests/e2e/fixtures/evm-public-contract.json').read_text())
        self.contract=self.metadata['contract']
        self.fixture=ROOT/'tests/e2e/fixtures'/self.metadata['actualMono']['file']

    def decode(self,picture):
        return self.scope['decode_evm_contract_tooltip'](picture,self.contract,time.monotonic()+10)

    def test_actual_mono_and_proportional_crops_pass_the_complete_combined_decoder(self):
        for kind in ('actualMono','actualProportional'):
            metadata=self.metadata[kind];path=self.fixture.parent/metadata['file']
            with self.subTest(kind=kind):
                self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(),metadata['fixtureSHA256'])
                self.report.clear()
                with Image.open(path) as picture:self.assertTrue(self.decode(picture))
                self.assertEqual(self.report['ethereumContractLine']['dottedZeroPrefix'],kind=='actualMono')
                last=self.report['ethereumContractOCR'][-1]
                self.assertEqual((last['region'],last['scale'],last['psm']),('line',4,7) if kind=='actualMono' else ('block',3,6))
                self.assertTrue(all(set(row)=={'region','scale','psm','matched','recognizedBytes','recognizedSHA256'} for row in self.report['ethereumContractOCR']))
        self.assertEqual(list(pathlib.Path(self.folder.name).iterdir()),[])

    def test_complete_decoder_rejects_all_rendered_negative_images(self):
        control=self.metadata['syntheticMono'];path=self.fixture.parent/control['file']
        self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(),control['fixtureSHA256'])
        with Image.open(path) as picture:self.assertTrue(self.decode(picture),'negative controls must use a supported positive rendering')
        for name,metadata in self.metadata['negativeFixtures'].items():
            path=self.fixture.parent/name
            with self.subTest(fixture=name):
                self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(),metadata['fixtureSHA256'])
                self.report.clear()
                with Image.open(path) as picture:self.assertFalse(self.decode(picture))
                if name.endswith('capital-o-prefix.png'):
                    self.assertFalse(self.report['ethereumContractLine']['dottedZeroPrefix'])
                    self.assertGreater(self.report['ethereumContractLine']['otherDottedZeroGlyphs'],0)
                    self.assertNotIn('ethereumContractOCR',self.report,'structurally contradicted Ox must reject before every OCR mode')

    def test_single_line_decoder_preserves_every_nonwhitespace_byte_and_caps_ocr(self):
        values=((self.contract+'\n',True),('0x'+self.contract[2:].upper(),True),
                (self.contract[:22]+' '+self.contract[22:],True),
                (self.contract.replace('1','l',1),False),(self.contract.replace('1','I',1),False),
                (self.contract.replace('0x','Ox',1),False),('@'+self.contract,False),
                (self.contract+'0',False),('0'+self.contract,False),
                (self.contract+'\n'+self.contract,False))
        for value,expected in values:
            with self.subTest(value=value),mock.patch.object(subprocess,'check_output',side_effect=[b'not a contract\n']*4+[value.encode()]) as decoder:
                with Image.open(self.fixture) as picture:self.assertEqual(self.decode(picture),expected)
                self.assertEqual(decoder.call_count,5)
                self.assertEqual([call.args[0] for call in decoder.call_args_list],
                                 [['tesseract','stdin','stdout','--psm',str(psm)] for psm in (6,6,7,7,7)])
                self.assertTrue(all(0<call.kwargs['timeout']<=10 for call in decoder.call_args_list))
                with Image.open(io.BytesIO(decoder.call_args.kwargs['input'])) as prepared:self.assertEqual(prepared.size,(2784,184))

    def test_address_line_requires_two_complete_unclipped_ink_bands_and_supported_fill(self):
        with Image.open(self.fixture) as source:picture=source.copy()
        captured=self.scope['evm_contract_address_line'](picture)
        self.assertEqual(captured[1],(8,39,680,61))
        variants=[]
        blank=Image.new('L',picture.size,240);variants.append(blank)
        missing=picture.copy();missing.paste(240,(8,35,picture.width-8,65));variants.append(missing)
        third=picture.copy();ImageDraw.Draw(third).rectangle((20,65,80,70),fill=39);variants.append(third)
        clipped=picture.copy();ImageDraw.Draw(clipped).rectangle((1,44,8,55),fill=39);variants.append(clipped)
        wrong_fill=picture.copy();wrong_fill.putpixel((picture.width//2,2),239);variants.append(wrong_fill)
        variants.extend((picture.convert('RGB'),Image.new('L',(711,74),240),Image.new('L',(688,141),240)))
        for variant in variants:
            with self.subTest(size=variant.size,mode=variant.mode):self.assertIsNone(self.scope['evm_contract_address_line'](variant))

    def test_dark_supported_fill_is_normalized_before_structural_observation(self):
        with Image.open(self.fixture) as picture:
            dark=picture.point(lambda value:max(0,min(255,round(25+(240-value)*195/201))))
        self.assertIsNone(self.scope['evm_contract_address_line'](dark),'dark pixels cannot masquerade as Light')
        self.scope['appearance']['theme']='dark'
        captured=self.scope['evm_contract_address_line'](dark)
        self.assertIsNotNone(captured)
        self.assertEqual(self.scope['evm_dotted_zero_structure'](captured[0]),(True,2))

    def glyph(self):
        picture=Image.new('L',(100,24),240)
        draw=ImageDraw.Draw(picture)
        draw.rectangle((5,4,13,18),outline=39,width=2)
        draw.rectangle((8,10,9,11),fill=39)
        return picture

    def test_dotted_zero_requires_a_complete_ring_and_one_detached_enclosed_dot(self):
        self.assertEqual(self.scope['evm_dotted_zero_structure'](self.glyph()),(True,0))
        variants=[]
        opened=self.glyph();ImageDraw.Draw(opened).rectangle((8,3,10,7),fill=240);variants.append(opened)
        hollow=self.glyph();ImageDraw.Draw(hollow).rectangle((8,10,9,11),fill=240);variants.append(hollow)
        outside=hollow.copy();ImageDraw.Draw(outside).rectangle((18,10,19,11),fill=39);variants.append(outside)
        attached=self.glyph();ImageDraw.Draw(attached).rectangle((6,10,9,11),fill=39);variants.append(attached)
        fake=Image.new('L',(100,24),240);ImageDraw.Draw(fake).rectangle((8,10,9,11),fill=39);variants.append(fake)
        extra=self.glyph();extra.putpixel((10,14),39);variants.append(extra)
        clipped=Image.new('L',(100,24),240);clipped.paste(self.glyph(),(-5,0));variants.append(clipped)
        variants.extend((Image.new('L',(695,24),240),Image.new('L',(100,41),240),self.glyph().convert('RGB')))
        for variant in variants:
            with self.subTest(size=variant.size,mode=variant.mode):self.assertFalse(self.scope['evm_dotted_zero_structure'](variant)[0])

    def test_expired_budget_and_ocr_failure_cannot_become_a_match_or_extend_timeout(self):
        with Image.open(self.fixture) as picture,mock.patch.object(subprocess,'check_output') as decoder:
            self.assertFalse(self.scope['decode_evm_contract_tooltip'](picture,self.contract,time.monotonic()-1))
            decoder.assert_not_called()
        for error in (subprocess.TimeoutExpired('tesseract',1),subprocess.CalledProcessError(1,'tesseract')):
            with Image.open(self.fixture) as picture,mock.patch.object(subprocess,'check_output',side_effect=error) as decoder:
                with self.assertRaises(type(error)):self.scope['decode_evm_contract_tooltip'](picture,self.contract,time.monotonic()+.1)
                self.assertLessEqual(decoder.call_args.kwargs['timeout'],.1)
        with mock.patch.object(subprocess,'check_output') as decoder:
            for picture in (Image.new('L',(711,74)),Image.new('L',(688,141)),Image.new('RGB',(688,74))):self.assertFalse(self.decode(picture))
            decoder.assert_not_called()

    def test_only_already_guarded_bounded_public_crop_is_saved_on_failure(self):
        with Image.open(self.fixture) as picture:crop=picture.copy()
        self.capture.return_value=([],Image.new('RGB',(1600,1100)),(80,32,1440,1036))
        self.block.return_value=(crop,(159,543,847,617))
        self.scope['decode_evm_contract_tooltip']=mock.Mock(return_value=False)
        self.scope['time']=SimpleNamespace(monotonic=mock.Mock(side_effect=[0,1,2]),sleep=mock.Mock())
        self.assertIsNone(self.scope['verify_evm_public_contract']((200,647),self.contract,1))
        self.capture.assert_called_once_with(with_picture=True,with_geometry=True)
        files=list(pathlib.Path(self.folder.name).iterdir())
        self.assertEqual([path.name for path in files],['owned-contract-evm-public-contract.png'])
        with Image.open(files[0]) as saved:self.assertEqual(saved.tobytes(),crop.tobytes())
        self.assertEqual(self.report['ethereumContractPublicRegion']['failureArtifactSHA256'],hashlib.sha256(files[0].read_bytes()).hexdigest())
        self.scope['time'].sleep.assert_called_once_with(0)

    def test_private_capture_error_never_creates_a_failure_artifact_even_after_a_public_crop(self):
        private=type('PrivateWindowVisible',(RuntimeError,),{})
        with Image.open(self.fixture) as picture:crop=picture.copy()
        public=([],Image.new('RGB',(1600,1100)),(80,32,1440,1036))
        self.block.return_value=(crop,(159,543,847,617))
        self.scope['decode_evm_contract_tooltip']=mock.Mock(return_value=False)
        self.scope['time']=SimpleNamespace(monotonic=lambda:0,sleep=mock.Mock())
        for observations in ([private('private pixels refused')],[public,private('private pixels refused')]):
            self.capture.side_effect=observations
            with self.assertRaises(private):self.scope['verify_evm_public_contract']((200,647),self.contract,1)
            self.assertEqual(list(pathlib.Path(self.folder.name).iterdir()),[])

    def test_ocr_timeout_keeps_only_guarded_numeric_region_metadata_and_no_new_capture(self):
        with Image.open(self.fixture) as picture:crop=picture.copy()
        self.capture.return_value=([],Image.new('RGB',(1600,1100)),(80,32,1440,1036))
        self.block.return_value=(crop,(159,543,847,617))
        self.scope['decode_evm_contract_tooltip']=mock.Mock(side_effect=subprocess.TimeoutExpired('tesseract',.1))
        with self.assertRaises(subprocess.TimeoutExpired):self.scope['verify_evm_public_contract']((200,647),self.contract,time.monotonic()+.1)
        self.capture.assert_called_once_with(with_picture=True,with_geometry=True)
        self.assertEqual(self.report,{'ethereumContractPublicRegion':{'bounds':[159,543,847,617],'size':[688,74]}})
        self.assertEqual(list(pathlib.Path(self.folder.name).iterdir()),[])


class WailsHostedFontFamilies(unittest.TestCase):
    def test_font_profile_has_no_blanket_family_replacement_and_preserves_monospace(self):
        import xml.etree.ElementTree as ET
        path=ROOT/'tests/e2e/fixtures/hosted-font-families.conf'
        profile=ET.parse(path).getroot()
        for match in profile.findall('match'):
            self.assertTrue(match.findall("test[@name='family']"),'unscoped font replacement can silently turn mono into proportional text')
        env=dict(os.environ,FONTCONFIG_FILE=str(path))
        for family,expected in ((r'sans\-serif','DejaVu Sans'),(r'system\-ui','DejaVu Sans'),
                                (r'ui\-monospace','DejaVu Sans Mono'),(r'SFMono\-Regular','DejaVu Sans Mono'),
                                ('Consolas','DejaVu Sans Mono'),('monospace','DejaVu Sans Mono')):
            with self.subTest(family=family):
                self.assertEqual(subprocess.check_output(['fc-match','-f','%{family}',family],env=env,timeout=5).decode(),expected)




class WailsPrivatePhraseGrid(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        helper=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='private_phrase_grid')
        self.colors={'page':(245,247,250),'border':(197,207,219),'button':(233,238,245)}
        self.scope={'palette':lambda:self.colors}
        exec(compile(ast.Module(body=[helper],type_ignores=[]),'private-grid-geometry','exec'),self.scope)
        self.measure=self.scope['private_phrase_grid']

    def fixture(self,width=920,count=12,height=42):
        picture=Image.new('RGB',(width,980),self.colors['page'])
        columns=4 if width>=760 else 3
        cell=(width-52-(columns-1)*8)/columns
        boxes=[]
        for row in range((count+columns-1)//columns):
            for column in range(columns):
                left=int(26+column*(cell+8)+.5)
                right=int(26+(column+1)*cell+column*8+.5)-1
                top=200+row*(height+8)
                box=(left,top,right,top+height-1);boxes.append(box)
                ImageDraw.Draw(picture).rounded_rectangle(box,radius=6,fill=self.colors['button'],outline=self.colors['border'])
        return picture,boxes

    def test_fractional_columns_measure_real_edges_independently_for_twelve_and_twenty_four(self):
        for width in (560,736,760,761,880,881,920,1040):
            for count in (12,24):
                with self.subTest(width=width,count=count):
                    picture,boxes=self.fixture(width,count)
                    cells=self.measure(picture,count)
                    self.assertEqual(len(cells),count)
                    self.assertEqual([(c[0],c[2]-1) for c in cells],[(b[0],b[2]) for b in boxes])
        picture,_=self.fixture()
        self.assertEqual([c[0] for c in self.measure(picture,12)[:4]],[26,245,464,683])

    def test_supported_font_card_heights_and_dark_palette_keep_complete_rows(self):
        for height in (34,42,50):
            picture,_=self.fixture(height=height)
            self.assertEqual(len(self.measure(picture,12)),12)
        self.colors={'page':(16,19,25),'border':(52,60,74),'button':(34,41,51)}
        picture,_=self.fixture(count=24)
        self.assertEqual(len(self.measure(picture,24)),24)

    def test_partial_extra_wrong_palette_and_centered_legacy_panels_are_rejected(self):
        picture,boxes=self.fixture()
        ImageDraw.Draw(picture).rectangle(boxes[-1],fill=self.colors['page'])
        self.assertEqual(self.measure(picture,12),[])
        picture,_=self.fixture(count=24)
        self.assertEqual(self.measure(picture,12),[])
        picture,_=self.fixture()
        self.colors['border']=(100,110,120)
        self.assertEqual(self.measure(picture,12),[])
        self.colors['border']=(197,207,219)
        inner,_=self.fixture(width=880)
        outer=Image.new('RGB',(920,980),self.colors['page']);outer.paste(inner,(20,0))
        self.assertEqual(self.measure(outer,12),[])

    def test_edges_require_both_borders_and_real_button_interiors(self):
        picture,boxes=self.fixture()
        for left,top,right,bottom in boxes:
            ImageDraw.Draw(picture).rectangle((left+1,top+1,right-1,bottom-1),fill=self.colors['page'])
        self.assertEqual(self.measure(picture,12),[])
        picture,boxes=self.fixture()
        for left,top,right,bottom in boxes[::4]:
            ImageDraw.Draw(picture).rectangle((right-1,top,right+1,bottom),fill=self.colors['page'])
        self.assertEqual(self.measure(picture,12),[])

    def test_wrong_count_mode_and_out_of_app_bounds_cannot_supply_cells(self):
        with self.assertRaisesRegex(RuntimeError,'word count'):
            self.measure(Image.new('RGB',(920,650)),13)
        for picture in (Image.new('RGB',(559,650)),Image.new('RGB',(920,249)),Image.new('RGBA',(920,650))):
            self.assertEqual(self.measure(picture,12),[])


class WailsAddressBookPublicObservation(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        names=('public_words_position','address_book_use_matches','address_book_use_position','address_book_recipient_loaded','address_book_composer_cleared','public_status_contains')
        functions=[n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name in names]
        self.capture=mock.Mock()
        self.decoder=mock.Mock(return_value=b'')
        self.scope={'public_rows':self.capture,'palette':lambda:{'input':(255,255,255)},'appearance':{'theme':'light'},'Image':Image,'ImageOps':ImageOps,'io':io,'subprocess':SimpleNamespace(check_output=self.decoder,DEVNULL=subprocess.DEVNULL)}
        exec(compile(ast.Module(body=functions,type_ignores=[]),'address-book-public-helper','exec'),self.scope)
        self.recipient='0x'+'2'*40

    def word(self,text,x,y,line):
        return ['5','1',str(line),'1','1','1',str(x),str(y),'80','14','99',text]

    def recipient_rows(self,value=None,y=546):
        return [self.word('Recipient',143,504,1),self.word('address',232,504,1),self.word(value or self.recipient,155,y,2)]

    def test_recipient_is_exactly_observed_below_its_own_input_label(self):
        self.capture.return_value=self.recipient_rows()
        self.assertTrue(self.scope['address_book_recipient_loaded'](self.recipient))
        self.capture.assert_called_once_with(ocr_scale=1)

    def test_extra_confusable_or_receive_identity_cannot_become_loaded_recipient(self):
        for rows in (self.recipient_rows('0xX'+'2'*40),self.recipient_rows(self.recipient+'2'),self.recipient_rows(y=287),self.recipient_rows('0x'+'3'*40)):
            with self.subTest(rows=rows):
                self.capture.return_value=rows
                self.assertFalse(self.scope['address_book_recipient_loaded'](self.recipient))
        rows=self.recipient_rows();rows[0][11]='Receive';self.capture.return_value=rows
        self.assertFalse(self.scope['address_book_recipient_loaded'](self.recipient))

    def test_use_label_falls_back_to_second_scale_without_repairing_wrong_glyphs(self):
        wrong=[self.word('Yse',900,650,1),self.word('address',950,650,1)]
        exact=[self.word('Use',900,650,1),self.word('address',950,650,1)]
        self.capture.side_effect=[wrong,exact]
        self.assertEqual(self.scope['address_book_use_position'](),(965,657))
        self.assertEqual(self.capture.call_args_list,[mock.call(ocr_scale=1),mock.call(ocr_scale=2)])
        self.capture.side_effect=[wrong,wrong]
        self.assertIsNone(self.scope['address_book_use_position']())

    def test_duplicate_saved_use_controls_cannot_pass_an_identical_retry(self):
        rows=[self.word('Use',900,650,1),self.word('address',950,650,1),self.word('Use',900,720,2),self.word('address',950,720,2)]
        self.capture.return_value=rows
        with self.assertRaisesRegex(RuntimeError,'multiple selectable'):
            self.scope['address_book_use_position']()

    def test_fresh_completed_save_requires_a_blank_composer_in_the_owned_public_field(self):
        picture=Image.new('RGB',(1600,1100),'white')
        rows=[self.word('Name',143,405,1)]
        self.capture.return_value=(rows,picture,(80,32,1440,1036))
        self.assertTrue(self.scope['address_book_composer_cleared']())
        picture.putpixel((160,445),(29,41,56))
        self.assertFalse(self.scope['address_book_composer_cleared']())
        self.capture.return_value=([],picture,(80,32,1440,1036))
        self.assertFalse(self.scope['address_book_composer_cleared']())
        self.capture.return_value=([self.word('Name',143,1050,1)],picture,(80,32,1440,1036))
        with self.assertRaisesRegex(RuntimeError,'owned public bounds'):
            self.scope['address_book_composer_cleared']()

    def test_exact_feedback_is_read_from_owned_footer_instead_of_page_body(self):
        picture=Image.new('RGB',(1600,1100),'white')
        self.capture.return_value=([self.word('Expected feedback.',115,1046,1)],picture,(80,32,1440,1036))
        self.assertTrue(self.scope['public_status_contains']('Expected feedback.'))
        self.decoder.assert_not_called()
        self.capture.return_value=([self.word('Expected feedback.',115,650,1)],picture,(80,32,1440,1036))
        self.assertFalse(self.scope['public_status_contains']('Expected feedback.'))
        self.assertEqual(self.decoder.call_count,2)
        for call in self.decoder.call_args_list:
            self.assertEqual(call.args[0],['tesseract','stdin','stdout','--psm','7'])
            self.assertEqual(call.kwargs['timeout'],10)

    def test_footer_single_line_fallback_requires_exact_unrepaired_message(self):
        picture=Image.new('RGB',(1600,1100),'white')
        self.capture.return_value=([],picture,(80,32,1440,1036))
        self.decoder.side_effect=[b'wrong feedback.\n',b'Expected feedback.\n']
        self.assertTrue(self.scope['public_status_contains']('Expected feedback.'))
        with Image.open(io.BytesIO(self.decoder.call_args.kwargs['input'])) as crop:
            self.assertEqual(crop.size,((1440-42)*2+24,29*2+24))
        self.decoder.side_effect=[b'Expected feedbaek.\n',b'Expected feedbaek.\n']
        self.assertFalse(self.scope['public_status_contains']('Expected feedback.'))
        self.capture.return_value=([],picture,(80,32,1700,1036))
        with self.assertRaisesRegex(RuntimeError,'owned public bounds'):
            self.scope['public_status_contains']('Expected feedback.')

    def test_private_guard_failure_cannot_become_a_public_match(self):
        self.capture.side_effect=RuntimeError('private pixels refused')
        for name,args in (('address_book_use_position',()),('address_book_recipient_loaded',(self.recipient,)),('public_status_contains',('Expected feedback.',))):
            with self.subTest(name=name),self.assertRaisesRegex(RuntimeError,'private pixels'):
                self.scope[name](*args)


def load_tests(loader, tests, pattern):
    if not CURRENT_UI:
        return tests
    current = unittest.TestSuite()
    for case in (WailsDisplayStartup, WailsInWindowDialogs, WailsPrivateStartup, WailsMainRowCopy, WailsPublicKeyboardCopy, WailsNearTokenPresentation, WailsNearNavigation, WailsPublicContractOCR, WailsAppearanceCapture, WailsOwnedServerGuard, WailsPrivateCopyNavigation, WailsEVMAssetSelector, WailsEVMContractTooltip, WailsEVMContractDecoder, WailsHostedFontFamilies, WailsAddressBookPublicObservation, WailsPrivatePhraseGrid):
        current.addTests(loader.loadTestsFromTestCase(case))
    return current


if __name__ == '__main__':
    if CURRENT_UI:
        sys.argv.remove('--current-ui')
    unittest.main()
