#!/usr/bin/env python3
"""Exercise crash-harness process races using only disposable owned children."""
import importlib.util
import itertools
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import threading
import unittest
from types import SimpleNamespace
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('crash_security', ROOT / 'scripts/crash-security.py')
crash = importlib.util.module_from_spec(spec)
spec.loader.exec_module(crash)


class OwnedCrashProcessTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory(prefix='wallet-crash-helper-')
        self.addCleanup(self.folder.cleanup)

    def child(self, code='import time; time.sleep(60)'):
        process = subprocess.Popen([sys.executable, '-c', code],
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            start_new_session=True)
        def cleanup():
            if process.poll() is None:
                process.kill()
            process.wait(timeout=5)
            process.stdout.close()
        self.addCleanup(cleanup)
        return process

    def client(self, process):
        client = crash.Client.__new__(crash.Client)
        client.process = process
        client.home = Path(self.folder.name)
        client.signers = set()
        client.signer_handles = {}
        client.canaries = []
        client.stopped = False
        client.discover_signers = mock.Mock()
        client.reader = threading.Thread(target=lambda: None)
        client.reader.start()
        self.addCleanup(client.close_handles)
        return client

    def test_child_exiting_between_poll_and_signal_is_reaped_without_cleanup_failure(self):
        process = self.child()
        client = self.client(process)
        original_poll = process.poll
        raced = False
        def stale_poll():
            nonlocal raced
            result = original_poll()
            if result is None and not raced:
                raced = True
                # The kernel has removed this exact owned child by the time
                # stop receives the formerly accurate "still running" result.
                os.kill(process.pid, signal.SIGTERM)
                process.wait(timeout=5)
                return None
            return result
        with mock.patch.object(process, 'poll', side_effect=stale_poll):
            client.stop()
        self.assertTrue(raced)
        self.assertTrue(client.stopped)
        self.assertEqual(process.returncode, -signal.SIGTERM)
        self.assertFalse(client.reader.is_alive())

    def test_actual_sigkill_reaps_child_and_releases_its_writer_lock(self):
        home = Path(self.folder.name)
        lock = home / 'config/cryptowallet/mainnet/writer.lock'
        lock.parent.mkdir(parents=True)
        code = ('import fcntl,os,sys,time; '
                'fd=os.open(sys.argv[1],os.O_CREAT|os.O_RDWR,0o600); '
                'fcntl.flock(fd,fcntl.LOCK_EX); print("ready",flush=True); time.sleep(60)')
        process = subprocess.Popen([sys.executable, '-c', code, str(lock)],
            stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            start_new_session=True)
        self.addCleanup(lambda: process.stdout.close())
        def cleanup():
            if process.poll() is None:
                process.kill()
            process.wait(timeout=5)
        self.addCleanup(cleanup)
        self.assertEqual(process.stdout.readline(), b'ready\n')
        client = self.client(process)
        client.stop(signal.SIGKILL)
        self.assertEqual(process.returncode, -signal.SIGKILL)
        self.assertTrue(client.stopped)
        self.assertTrue(lock.is_file())

    def test_pinned_signer_signal_kills_only_the_observed_owned_child(self):
        core = self.child()
        signer = self.child()
        client = self.client(core)
        client.signers.add(signer.pid)
        handle = os.pidfd_open(signer.pid)
        client.signer_handles[signer.pid] = handle
        client.crash_signers(signal.SIGKILL)
        self.assertEqual(signer.wait(timeout=5), -signal.SIGKILL)
        self.assertIsNone(core.poll())
        client.close_handles()
        with self.assertRaises(OSError):
            os.fstat(handle)

    def test_exited_original_handle_never_signals_a_replacement_numeric_target(self):
        original = self.child()
        handle = os.pidfd_open(original.pid)
        original.kill()
        original.wait(timeout=5)
        replacement = self.child()
        client = self.client(replacement)
        # Model a recycled PID label while retaining the original process's
        # kernel handle. Only that original handle may receive a crash signal.
        client.signers.add(replacement.pid)
        client.signer_handles[replacement.pid] = handle
        with self.assertRaisesRegex(crash.DrillFailure, 'Expected signer exited'):
            client.crash_signers(signal.SIGKILL)
        self.assertIsNone(replacement.poll())

    def test_live_signer_after_parent_exit_remains_a_hard_failure(self):
        client = self.client(self.child())
        signer = self.child()
        client.signers.add(signer.pid)
        # Advance only the helper's bounded wait clock. The owned signer really
        # remains alive, so cleanup must fail rather than treat it as ESRCH.
        clock = SimpleNamespace(monotonic=itertools.count(0, 3).__next__, sleep=lambda _: None)
        with mock.patch.object(crash, 'time', clock):
            with self.assertRaisesRegex(crash.DrillFailure, 'Signer remained live'):
                client.stop()
        self.assertIsNone(signer.poll())
        self.assertFalse(client.stopped)

    def test_disappearing_cleanup_task_is_allowed_only_after_actual_owned_child_exit(self):
        client = self.client(self.child())
        signer = self.child()
        client.signers.add(signer.pid)
        original_read = Path.read_text
        raced = False
        def retire_task(path, *args, **kwargs):
            nonlocal raced
            if str(path).startswith('/proc/' + str(signer.pid) + '/task/'):
                raced = True
                signer.kill()
                signer.wait(timeout=5)
                raise ProcessLookupError('disposable owned task exited')
            return original_read(path, *args, **kwargs)
        with mock.patch.object(Path, 'read_text', autospec=True, side_effect=retire_task):
            client.stop()
        self.assertTrue(raced)
        self.assertEqual(signer.returncode, -signal.SIGKILL)
        self.assertTrue(client.stopped)

    def test_disappearing_required_process_during_active_probe_is_a_hard_failure(self):
        code = ('import resource,time; resource.setrlimit(resource.RLIMIT_CORE,(0,0)); '
                'print("ready",flush=True); time.sleep(60)')
        core = self.child(code)
        signer = self.child(code)
        self.assertEqual(core.stdout.readline(), b'ready\n')
        self.assertEqual(signer.stdout.readline(), b'ready\n')
        client = self.client(core)
        client.signers.add(signer.pid)
        original_read = Path.read_bytes
        def disappeared(path):
            if path == Path('/proc', str(signer.pid), 'cmdline'):
                raise ProcessLookupError('disposable active probe target exited')
            return original_read(path)
        with mock.patch.object(Path, 'read_bytes', autospec=True, side_effect=disappeared):
            with self.assertRaisesRegex(crash.DrillFailure, 'disappeared during active inspection'):
                client.inspect_processes()

    def test_failure_location_never_retains_exception_text_or_locals(self):
        private_fixture = 'PUBLIC-SYNTHETIC-PROMPT-CANARY'
        try:
            crash.require(False, private_fixture)
        except crash.DrillFailure as error:
            site = crash.failure_site(error)
        self.assertEqual(site['function'], 'require')
        self.assertGreater(site['line'], 0)
        self.assertNotIn(private_fixture, json.dumps(site))


if __name__ == '__main__':
    unittest.main(verbosity=2)
