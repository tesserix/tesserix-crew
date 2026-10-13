#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""A successful notarytool process must not publish an Invalid submission."""
import base64
from pathlib import Path
import os
import subprocess
import tempfile
import unittest


class SigningTests(unittest.TestCase):
    def run_signing(self, status):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tools = root / 'tools'
            tools.mkdir()
            (root / 'dist').mkdir()
            archive = root / 'dist' / 'crew_0.4.1_darwin_arm64.tar.gz'
            archive.write_bytes(b'original archive')
            mocks = {
                'security': 'exit 0',
                'codesign': 'exit 0',
                'ditto': 'exit 0',
                'openssl': 'printf test-keychain-password',
                'xcrun': 'printf \'{"status":"' + status + '"}\'',
                'tar': '''if [ "$1" = '-xzf' ]; then exit 0; fi
printf 'signed archive' > "$2"''',
            }
            for name, body in mocks.items():
                path = tools / name
                path.write_text('#!/bin/sh\n' + body + '\n')
                path.chmod(0o700)
            env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ['PATH'])
            fake = base64.b64encode(b'fixture-credential').decode()
            env.update(MACOS_CERT_P12_BASE64=fake, MACOS_CERT_PASSWORD='fixture-password',
                       MACOS_SIGNING_IDENTITY='fixture-identity', APPLE_NOTARY_KEY_BASE64=fake,
                       APPLE_NOTARY_KEY_ID='fixture-id', APPLE_NOTARY_ISSUER_ID='fixture-issuer')
            script = Path(__file__).with_name('sign_macos.sh').resolve()
            result = subprocess.run(['bash', str(script), '0.4.1', 'arm64'], cwd=root,
                                    env=env, capture_output=True, text=True)
            return result.returncode, archive.read_bytes()

    def test_invalid_submission_cannot_replace_archive(self):
        code, archive = self.run_signing('Invalid')
        self.assertNotEqual(code, 0)
        self.assertEqual(archive, b'original archive')

    def test_accepted_submission_produces_signed_archive(self):
        code, archive = self.run_signing('Accepted')
        self.assertEqual(code, 0)
        self.assertEqual(archive, b'signed archive')


if __name__ == '__main__':
    unittest.main()
