#!/usr/bin/env bash
# Signs the Windows app and its installer through Azure Trusted Signing, the
# certificate RapidRabbit's releases carry, so neither SmartScreen nor the
# installer's UAC prompt calls the publisher unknown.
#
#   package-windows.sh <path/to/aimusicmaker.exe> <path/to/installer.exe>
#
# Run after `wails build -nsis`. The app is signed first and the installer is
# then rebuilt around it, because the installer Wails made holds the unsigned
# copy; the rebuilt installer is signed last.
#
# RapidRabbit signs on a Windows runner with azure/trusted-signing-action. This
# repository builds Windows on Linux to halve the bill, so it signs with jsign,
# which speaks to the same Trusted Signing service from any OS.
#
# Needs AZURE_TENANT_ID, AZURE_CLIENT_ID and AZURE_CLIENT_SECRET (an app
# registration with the Trusted Signing Certificate Profile Signer role), and
# AZURE_TRUSTED_SIGNING_ENDPOINT, _ACCOUNT and _CERT_PROFILE. Without all six
# the installer ships unsigned, with a warning.
set -euo pipefail

exe="$1"
installer="$2"

# Pinned by hash: this jar handles the signing credential.
jsign_version=7.5
jsign_sha256=602a51c3545a6dc4fb99bd2ea7152b26d1345916d0c93ddfbd5936cb735af91c

warn() {
	if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::warning::$1"; else echo "warning: $1" >&2; fi
}

for v in AZURE_TENANT_ID AZURE_CLIENT_ID AZURE_CLIENT_SECRET \
	AZURE_TRUSTED_SIGNING_ENDPOINT AZURE_TRUSTED_SIGNING_ACCOUNT AZURE_TRUSTED_SIGNING_CERT_PROFILE; do
	if [ -z "${!v:-}" ]; then
		warn "$v is not set: the Windows app and installer are not signed."
		exit 0
	fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
jar="$work/jsign.jar"
curl -fsSL -o "$jar" "https://github.com/ebourg/jsign/releases/download/$jsign_version/jsign-$jsign_version.jar"
echo "$jsign_sha256  $jar" | sha256sum -c --quiet -

# A token for the signing service, from the app registration's secret. It lasts
# about an hour, which covers both signatures.
# Not curl -f: on a refusal Azure's body says why (a wrong tenant, an expired
# secret), and that is the line worth printing.
response=$(curl -sS "https://login.microsoftonline.com/$AZURE_TENANT_ID/oauth2/v2.0/token" \
	--data-urlencode "client_id=$AZURE_CLIENT_ID" \
	--data-urlencode "client_secret=$AZURE_CLIENT_SECRET" \
	--data-urlencode "grant_type=client_credentials" \
	--data-urlencode "scope=https://codesigning.azure.net/.default")
token=$(jq -r '.access_token // empty' <<<"$response")
if [ -z "$token" ]; then
	echo "Azure refused the sign-in: $(jq -r '.error_description // .' <<<"$response" | head -n1)" >&2
	exit 1
fi
if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::add-mask::$token"; fi

# jsign wants the endpoint as a bare host; the variable is the URL Azure shows.
endpoint="${AZURE_TRUSTED_SIGNING_ENDPOINT#https://}"
endpoint="${endpoint%/}"

sign() {
	echo "Signing $1"
	java -jar "$jar" --storetype TRUSTEDSIGNING --keystore "$endpoint" --storepass "$token" \
		--alias "$AZURE_TRUSTED_SIGNING_ACCOUNT/$AZURE_TRUSTED_SIGNING_CERT_PROFILE" \
		--alg SHA-256 --tsaurl http://timestamp.acs.microsoft.com --tsmode RFC3161 \
		"$1"
}

sign "$exe"

# The same makensis call Wails makes, reusing the wails_tools.nsh and WebView2
# bootstrapper it left in build/windows/installer.
(cd build/windows/installer && makensis -DARG_WAILS_AMD64_BINARY="$(cd "$(dirname "$exe")" && pwd)/$(basename "$exe")" project.nsi)

sign "$installer"
