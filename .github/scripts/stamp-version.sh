#!/usr/bin/env bash
# Writes the release version into wails.json, which is where the Windows file
# properties and the macOS Info.plist read it from, without the build reporting
# itself as dirty.
#
# Go stamps vcs.modified from `git status`, and About shows it (see
# buildinfo.go). Editing a tracked file, or Wails writing build/darwin/ or the
# NSIS project under build/windows/installer/, would make every release call
# itself a modified build. So both are hidden from git status here, in this
# checkout only.
set -euo pipefail

version="$1"

jq --arg v "$version" '.info.productVersion = $v' wails.json > wails.json.tmp
mv wails.json.tmp wails.json
git update-index --assume-unchanged wails.json

echo "build/darwin/" >> .git/info/exclude
echo "build/linux/" >> .git/info/exclude
echo "build/windows/installer/" >> .git/info/exclude
