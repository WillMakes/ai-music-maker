#!/usr/bin/env bash
# Signs the macOS app with a Developer ID, packages it in a disk image, and has
# Apple notarize the image, the way RapidRabbit ships. This is distribution
# outside the Mac App Store: the App Store would need the app sandboxed, which
# the in-app updater and the free run of the music folder do not survive.
#
#   package-macos.sh <path/to/aimusicmaker.app> <path/to/output.dmg>
#
# Used by the release workflow and by hand, after `wails build`. What it can do
# depends on what it is given, and it says which:
#
#   Signing        APPLE_DEVELOPER_ID, e.g. "Developer ID Application: Name
#                  (TEAMID)". Unset, the first Developer ID Application identity
#                  in the keychain is used. With none at all the app keeps
#                  Wails' ad-hoc signature and nothing below happens.
#   Notarization   NOTARY_PROFILE, a profile saved with `xcrun notarytool
#                  store-credentials` (the local way), or APPLE_ID,
#                  APPLE_APP_PASSWORD and APPLE_TEAM_ID (the CI way). Without
#                  either the image is signed but not notarized, and Gatekeeper
#                  will warn whoever downloads it.
#
# Notarizing the image rather than the app covers both: the ticket lists the
# app's code hash, so an app dragged out of a stapled image opens cleanly too.
# The in-app updater downloads and opens this image, so a release that is not
# notarized is one every Mac user is warned about on update.
set -euo pipefail

app="$1"
dmg="$2"

# A GitHub annotation in CI, a plain line locally.
warn() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::warning::$1"; else echo "warning: $1" >&2; fi
}

identity="${APPLE_DEVELOPER_ID:-}"
if [ -z "$identity" ]; then
	identity=$(security find-identity -v -p codesigning |
		sed -n 's/.*"\(Developer ID Application: .*\)"$/\1/p' | head -n1)
fi

# The app is signed before it goes into the image: an image cannot be changed
# once made, and notarization checks the app inside it. Hardened runtime and a
# secure timestamp are both required for notarization. No entitlements: the app
# uses no camera, microphone or JIT, and WKWebView runs its JavaScript in a
# separate, Apple-signed process.
if [ -n "$identity" ]; then
	echo "Signing $app as $identity"
	codesign --force --options runtime --timestamp --sign "$identity" "$app"
	codesign --verify --strict --verbose=2 "$app"
else
	warn "No Developer ID Application identity: the app is left ad-hoc signed and will not be notarized."
fi

# ditto, not cp, keeps the bundle's symlinks and extended attributes, and the
# copy is named after the product so Applications lists it the way the app
# calls itself.
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
ditto "$app" "$staging/AIMusicMaker.app"
ln -s /Applications "$staging/Applications"
mkdir -p "$(dirname "$dmg")"
hdiutil create -volname AIMusicMaker -srcfolder "$staging" -fs HFS+ -format UDZO -ov "$dmg"

[ -n "$identity" ] || exit 0
codesign --force --timestamp --sign "$identity" "$dmg"

if [ -n "${NOTARY_PROFILE:-}" ]; then
	auth=(--keychain-profile "$NOTARY_PROFILE")
elif [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_APP_PASSWORD:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ]; then
	auth=(--apple-id "$APPLE_ID" --password "$APPLE_APP_PASSWORD" --team-id "$APPLE_TEAM_ID")
else
	warn "No notarization credentials: $dmg is signed but not notarized."
	exit 0
fi

# --wait exits 0 even when Apple rejects the submission, so the verdict is read
# from the result rather than the exit code. A rejection prints Apple's log,
# which names the file and the reason.
echo "Submitting $dmg for notarization. This usually takes a few minutes."
result=$(xcrun notarytool submit "$dmg" "${auth[@]}" --wait --timeout 20m --output-format json)
echo "$result"
status=$(plutil -extract status raw -o - - <<<"$result")
if [ "$status" != "Accepted" ]; then
	id=$(plutil -extract id raw -o - - <<<"$result")
	xcrun notarytool log "$id" "${auth[@]}" || true
	echo "Notarization finished with status: $status" >&2
	exit 1
fi

xcrun stapler staple "$dmg"
xcrun stapler validate "$dmg"
spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
