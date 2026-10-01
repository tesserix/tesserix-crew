#!/usr/bin/env python3
"""Collect the license notices of downloaded Go modules into release archives."""
import argparse
import json
import pathlib
import subprocess


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True, type=pathlib.Path)
    args = parser.parse_args()
    data = subprocess.check_output(["go", "list", "-m", "-json", "all"], text=True)
    decoder = json.JSONDecoder()
    notices = ["Tesserix Crew — third-party Go module license notices\n"]
    while data.strip():
        module, end = decoder.raw_decode(data.lstrip())
        data = data.lstrip()[end:]
        if module.get("Main") or not module.get("Dir"):
            continue
        root = pathlib.Path(module["Dir"])
        files = sorted(
            p for p in root.iterdir()
            if p.is_file() and p.name.upper().split(".")[0] in
            ("LICENSE", "LICENCE", "COPYING", "NOTICE")
        )
        for path in files:
            notices.append(
                f"\n{'=' * 72}\n{module['Path']} {module.get('Version', '')} — {path.name}\n\n"
                + path.read_text(errors="replace")
            )
    args.output.write_text("\n".join(notices))


if __name__ == "__main__":
    main()
