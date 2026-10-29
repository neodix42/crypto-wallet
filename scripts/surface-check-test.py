#!/usr/bin/env python3
"""Surface-check routing and authority regressions without launching either UI."""
import contextlib
import importlib.util
import io
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('wallet_surface', Path(__file__).with_name('surface-check.py'))
surface = importlib.util.module_from_spec(spec)
spec.loader.exec_module(surface)


class SurfaceRouting(unittest.TestCase):
    def test_default_retains_legacy_and_current_mode_never_calls_it(self):
        for arguments, current in [([], False), (['--current-ui'], True)]:
            with self.subTest(arguments=arguments), mock.patch.object(surface, 'check_go_policy') as shared, \
                    mock.patch.object(surface, 'check_current_ui') as wails, \
                    mock.patch.object(surface, 'check_legacy_desktop') as legacy:
                surface.main(arguments)
            shared.assert_called_once_with()
            self.assertEqual(wails.call_count, int(current))
            self.assertEqual(legacy.call_count, int(not current))

    def test_current_runtime_commands_include_go_and_react_and_no_legacy_or_measurement(self):
        with mock.patch.object(surface.subprocess, 'run') as execute, contextlib.redirect_stdout(io.StringIO()):
            surface.check_current_ui()
        self.assertEqual([call.args[0] for call in execute.call_args_list], [
            ['go', 'test', './internal/webui'],
            ['npm', '--prefix', 'internal/webui/frontend', 'test']])
        for call in execute.call_args_list:
            self.assertTrue(call.kwargs['check'])
            self.assertEqual(call.kwargs['cwd'], surface.root)

    def test_retired_prompt_process_fails_before_any_runtime_command(self):
        for mechanism in ('runPromptProcess', 'PrivateBridge', '--internal-prompt'):
            with self.subTest(mechanism=mechanism), tempfile.TemporaryDirectory(prefix='surface-policy-') as temporary:
                root = Path(temporary)
                folder = root / 'internal/webui'
                folder.mkdir(parents=True)
                for path in (surface.root / 'internal/webui').glob('*.go'):
                    if not path.name.endswith('_test.go'):
                        shutil.copy2(path, folder / path.name)
                path = folder / 'prompt.go'
                path.write_text(path.read_text() + '\n// ' + mechanism + '\n')
                with mock.patch.object(surface, 'root', root), mock.patch.object(surface.subprocess, 'run') as execute, \
                        self.assertRaisesRegex(AssertionError, 'retired isolated UI mechanism'):
                    surface.check_current_ui()
                execute.assert_not_called()


if __name__ == '__main__':
    unittest.main(verbosity=2)
