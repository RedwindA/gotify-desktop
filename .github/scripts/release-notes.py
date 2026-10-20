"""Extract one version's CHANGELOG section for GitHub Releases."""

import pathlib
import sys


def release_notes(changelog: str, version: str) -> str:
    sections = []
    current = None
    for line in changelog.splitlines():
        if line.startswith("## "):
            current = [] if line[3:].strip() == version else None
            if current is not None:
                sections.append(current)
        elif current is not None:
            current.append(line)
    if len(sections) != 1:
        raise ValueError(f"Expected exactly one CHANGELOG section for {version}")
    body = "\n".join(sections[0]).strip()
    if not body:
        raise ValueError(f"Empty CHANGELOG section for {version}")
    return f"## Changes\n\n{body}\n"


if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit("Usage: release-notes.py <version>")
    try:
        changelog = pathlib.Path("CHANGELOG.md").read_text(encoding="utf-8-sig")
        notes = release_notes(changelog, sys.argv[1])
    except (OSError, ValueError) as error:
        sys.exit(str(error))
    sys.stdout.buffer.write(notes.encode("utf-8"))
