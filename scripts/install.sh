#!/bin/sh
# Install a sitewarm release: download the archive for this machine, check it
# against the release's checksums.txt, and put the binary in a directory.
#
#   curl -fsSL https://raw.githubusercontent.com/sitewarm/sitewarm/master/scripts/install.sh | sh
#
# Environment:
#   SITEWARM_VERSION      release to install, without the leading v
#                         (default: the latest stable release)
#   SITEWARM_INSTALL_DIR  where to put the binary (default: $HOME/.local/bin)
set -eu

repo=sitewarm/sitewarm
dir=${SITEWARM_INSTALL_DIR:-$HOME/.local/bin}

fail() {
	echo "install.sh: $*" >&2
	exit 1
}

need() {
	tool=$1
	command -v "$tool" >/dev/null 2>&1 || fail "$tool is required"
}

# Every download uses HTTPS only, including after redirects.
fetch() {
	curl --proto '=https' --tlsv1.2 -fsSL "$@"
}

need curl
need tar
need uname

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "unsupported OS $(uname -s); download a release archive by hand (Windows: use the .zip)" ;;
esac

case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "unsupported architecture $(uname -m)" ;;
esac

version=${SITEWARM_VERSION:-}
if [ -z "$version" ]; then
	# /releases/latest skips pre-releases, so this is the latest stable one.
	url=$(fetch -I -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		fail "could not look up the latest release; set SITEWARM_VERSION"
	version=${url##*/v}
	case $version in
	*[!0-9A-Za-z.-]* | "") fail "no stable release yet; set SITEWARM_VERSION (for example 1.0.0-rc.1)" ;;
	*) ;;
	esac
fi
version=${version#v}

file=sitewarm_${version}_${os}_${arch}.tar.gz
base=https://github.com/$repo/releases/download/v$version

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading sitewarm $version for $os/$arch"
fetch -o "$tmp/$file" "$base/$file" || fail "could not download $base/$file"
fetch -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "could not download checksums.txt"

want=$(awk -v f="$file" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || fail "$file is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$file" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/$file" | awk '{ print $1 }')
else
	fail "sha256sum or shasum is required to check the download"
fi
[ "$got" = "$want" ] || fail "checksum mismatch for $file (got $got, want $want)"

tar -xzf "$tmp/$file" -C "$tmp" sitewarm
mkdir -p "$dir"
mv "$tmp/sitewarm" "$dir/sitewarm"
chmod 755 "$dir/sitewarm"

echo "Installed $dir/sitewarm"
"$dir/sitewarm" --version
case ":$PATH:" in
*":$dir:"*) ;;
*) echo "Add $dir to your PATH to run it as 'sitewarm'." ;;
esac
