#!/usr/bin/env python3
"""Exercise init lifecycle locking without touching the host service/network."""
import fcntl
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class InitLockTest(unittest.TestCase):
    def setUp(self):
        if not shutil.which('flock'):
            self.skipTest('flock is unavailable')
        self.temp = tempfile.TemporaryDirectory(prefix='nadkop-init-lock-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.runtime = self.root / 'runtime'
        self.runtime.mkdir()
        self.lock = self.runtime / 'ui.lock'
        self.init = self.root / 'nadkop.init'
        source = (ROOT / 'openwrt/nadkop/files/nadkop.init').read_text()
        source = source.replace('/var/run/nadkop', str(self.runtime))
        source = source.replace('/usr/bin/nadkop network stop',
                                'printf "network-stop\\n"; read -r release')
        self.init.write_text(source)

    def command(self, action='stop', locked=False):
        # Match rc.common: source init first, then call stop_service and kill
        # processes regardless of its return code.
        return ['sh', '-c', 'action="$1"; NADKOP_UI_LOCKED="$2"; '
                'logger() { :; }; . "$3"; stop_service; printf "procd-kill\\n"',
                'test-init', action, '1' if locked else '', str(self.init)]

    def test_busy_stop_exits_before_network_and_procd(self):
        with self.lock.open('w') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            for action in ('stop', 'shutdown'):
                result = subprocess.run(self.command(action), input='release\n',
                                        capture_output=True, text=True, timeout=5)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(result.stdout, '')

    def test_lock_held_during_network_stop(self):
        child = subprocess.Popen(self.command(), stdin=subprocess.PIPE,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            self.assertEqual(child.stdout.readline(), 'network-stop\n')
            with self.lock.open('w') as lock:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            out, err = child.communicate('release\n', timeout=5)
            self.assertEqual(child.returncode, 0, err)
            self.assertEqual(out, 'procd-kill\n')
            with self.lock.open('w') as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        finally:
            if child.poll() is None:
                child.kill()
                child.communicate()

    def test_rpc_stop_uses_parent_lock(self):
        with self.lock.open('w') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            result = subprocess.run(self.command(locked=True), input='release\n',
                                    capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, 'network-stop\nprocd-kill\n')


if __name__ == '__main__':
    unittest.main()
