import unittest
from render_formula import TARGETS, render


class FormulaTests(unittest.TestCase):
    def test_all_platform_archives_have_checksums(self):
        hashes = {target: f"{i + 1:064x}" for i, target in enumerate(TARGETS)}
        formula = render("0.1.0", hashes)
        for target, digest in hashes.items():
            self.assertIn(f"crew_0.1.0_{target}.tar.gz", formula)
            self.assertIn(f'sha256 "{digest}"', formula)
        self.assertIn('bin.install "crew"', formula)
        self.assertIn("on_macos", formula)
        self.assertIn("on_linux", formula)

    def test_invalid_versions_and_checksums_are_rejected(self):
        for version in ("v0.1.0", "0.1", "0.1.0\nputs 'bad'", "../0.1.0"):
            with self.assertRaises(ValueError):
                render(version, {target: "a" * 64 for target in TARGETS})
        with self.assertRaises(ValueError):
            render("0.1.0", {})


if __name__ == "__main__":
    unittest.main()
