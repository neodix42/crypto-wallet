#!/usr/bin/env python3
"""Pre-input diagnostics must explain startup failures without retaining workflow logs."""
import ast
import hashlib
import io
import json
import pathlib
import re
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


class WailsMainRowCopy(unittest.TestCase):
    def setUp(self):
        tree=ast.parse((ROOT/'scripts/wails-wallet-test.py').read_text())
        names=('main_row_copy_target','request_main_row_copy','main_row_copy','reveal_main_row_copy','receive_dialog_state','public_words_position','palette')
        functions=[node for node in tree.body if isinstance(node,ast.FunctionDef) and node.name in names]
        self.folder=tempfile.TemporaryDirectory(prefix='wallet-main-row-')
        self.addCleanup(self.folder.cleanup)
        self.geometry=(80,100,1440,900)
        self.clock=SimpleNamespace(now=0)
        self.clock.monotonic=lambda:self.clock.now
        self.clock.sleep=lambda seconds:setattr(self.clock,'now',self.clock.now+seconds)
        self.main=[(1,'CryptoWallet · 0.1.1 · mainnet')]
        self.desktop=SimpleNamespace(windows=mock.Mock(return_value=self.main),
                                     wait=mock.Mock(return_value=self.main[0]),
                                     geometry=mock.Mock(side_effect=lambda window:self.geometry),
                                     focus=mock.Mock(),click=mock.Mock(),accept=mock.Mock(),park_pointer=mock.Mock(),
                                     secret_window=mock.Mock(return_value=False),d=object())
        self.report={}
        self.rows=mock.Mock()
        self.scope={'time':self.clock,'re':re,'desktop':self.desktop,'report':self.report,
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

    def test_slow_prompt_start_waits_through_disabled_state_without_replacement_click(self):
        self.targets('enabled','disabled')
        self.desktop.windows.side_effect=lambda:self.main if self.clock.now<.8 else self.main+[(2,'cryptowallet' if self.clock.now<1 else 'Verify receive address')]
        self.scope['request_main_row_copy'](seconds=2)
        self.desktop.click.assert_called_once_with(1416,558)
        self.assertEqual(self.rows.call_count,2)
        self.assertEqual(self.report['mainRowCopy']['attempts'],1)

    def test_disabled_control_cannot_become_success_without_its_verified_prompt(self):
        self.targets('disabled')
        with self.assertRaisesRegex(RuntimeError,'bounded deadline'):
            self.scope['request_main_row_copy'](seconds=.2)
        self.desktop.click.assert_not_called()

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

    def test_reveal_is_horizontal_only_and_next_capture_rechecks_full_target(self):
        self.targets('clipped','enabled')
        reveal=mock.Mock()
        self.scope['reveal_main_row_copy']=reveal
        self.desktop.windows.side_effect=lambda:self.main if self.clock.now<.1 else self.main+[(2,'Verify receive address')]
        self.scope['request_main_row_copy'](seconds=1)
        reveal.assert_called_once_with(self.geometry)
        self.desktop.click.assert_called_once_with(1416,558)
        self.assertEqual(self.rows.call_count,2)

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

    def test_exact_fixture_address_is_required_after_real_dialog_acknowledgment(self):
        self.scope.update(request_main_row_copy=mock.Mock(),text=lambda:'Account group',
                          wait=lambda predicate:predicate(),clipboard=lambda:'0x1111111111111111111111111111111111111111')
        self.report['mainRowCopy']={}
        for expected,succeeds in [('0x1111111111111111111111111111111111111111',True),('0x2222222222222222222222222222222222222222',False)]:
            with self.subTest(expected=expected):
                if succeeds:
                    self.scope['passed']=mock.Mock()
                    self.assertEqual(self.scope['main_row_copy'](expected),expected)
                else:
                    with self.assertRaisesRegex(RuntimeError,'independently derived'):
                        self.scope['main_row_copy'](expected)
        self.assertEqual(self.desktop.accept.call_count,2)


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


def load_tests(loader, tests, pattern):
    if not CURRENT_UI:
        return tests
    current = unittest.TestSuite()
    for case in (WailsPrivateStartup, WailsMainRowCopy, WailsNearNavigation, WailsPublicContractOCR, WailsAppearanceCapture, WailsOwnedServerGuard, WailsPrivateCopyNavigation):
        current.addTests(loader.loadTestsFromTestCase(case))
    return current


if __name__ == '__main__':
    if CURRENT_UI:
        sys.argv.remove('--current-ui')
    unittest.main()
