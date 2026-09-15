#!/bin/sh
# Faultline installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/josipmusa/faultline/main/install.sh | sh
#
# It downloads the release archive for this machine, checks it against the
# checksums published with that release, and puts the binary on the PATH. It
# never uses sudo: as a normal user it installs under $HOME, and the only way
# it writes to a system directory is if you are already root or you asked for
# one by name.
#
# Environment:
#   FAULTLINE_VERSION      Tag to install, such as v0.1.0. Default: the newest
#                          non-prerelease.
#   FAULTLINE_INSTALL_DIR  Where to put the binary. Default: /usr/local/bin as
#                          root, ~/.local/bin otherwise.

set -eu

REPO=josipmusa/faultline
BINARY=faultline

main() {
	need_tools

	os=$(detect_os)
	arch=$(detect_arch)
	version=${FAULTLINE_VERSION:-$(latest_version)}
	# Release tags carry a leading v and the archive names do not.
	bare=${version#v}
	archive="${BINARY}_${bare}_${os}_${arch}.tar.gz"
	base="https://github.com/${REPO}/releases/download/${version}"

	workdir=$(mktemp -d)
	# Cleanup runs on the way out however we leave, so a failed download does
	# not leave half an archive in the temp directory.
	trap 'rm -rf "$workdir"' EXIT INT TERM

	say "downloading ${archive} (${version})"
	download "${base}/${archive}" "${workdir}/${archive}" ||
		die "no archive for ${os}/${arch} in ${version}. Check https://github.com/${REPO}/releases"
	download "${base}/checksums.txt" "${workdir}/checksums.txt" ||
		die "release ${version} has no checksums.txt, refusing to install unverified"

	verify "${workdir}" "${archive}"
	say "checksum ok"

	tar -xzf "${workdir}/${archive}" -C "${workdir}" "${BINARY}" ||
		die "could not unpack ${archive}"

	dir=$(install_dir)
	mkdir -p "$dir" || die "could not create ${dir}"
	[ -w "$dir" ] || die "${dir} is not writable. Set FAULTLINE_INSTALL_DIR to somewhere you own"
	# cp then chmod rather than install(1): the flags install(1) takes are not
	# the same on macOS and Linux.
	cp "${workdir}/${BINARY}" "${dir}/${BINARY}" || die "could not write ${dir}/${BINARY}"
	chmod 755 "${dir}/${BINARY}"

	say "installed ${dir}/${BINARY}"
	"${dir}/${BINARY}" version || die "${dir}/${BINARY} did not run"
	warn_if_off_path "$dir"
}

detect_os() {
	uname_s=$(uname -s)
	case "$uname_s" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	*) die "unsupported operating system: ${uname_s}. Windows: take the zip from https://github.com/${REPO}/releases/latest" ;;
	esac
}

detect_arch() {
	uname_m=$(uname -m)
	case "$uname_m" in
	x86_64 | amd64) echo amd64 ;;
	arm64 | aarch64) echo arm64 ;;
	*) die "unsupported architecture: ${uname_m}. Faultline publishes amd64 and arm64" ;;
	esac
}

install_dir() {
	if [ -n "${FAULTLINE_INSTALL_DIR:-}" ]; then
		echo "$FAULTLINE_INSTALL_DIR"
	elif [ "$(id -u)" -eq 0 ]; then
		echo /usr/local/bin
	else
		echo "${HOME}/.local/bin"
	fi
}

# Resolving the tag through the redirect on /releases/latest rather than the
# API keeps the installer off the 60-per-hour unauthenticated rate limit, which
# a shared CI address reaches on its own. wget cannot report the URL it landed
# on, so that half falls back to the API.
latest_version() {
	if command -v curl >/dev/null 2>&1; then
		resolved=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
			"https://github.com/${REPO}/releases/latest" 2>/dev/null) || resolved=
		tag=${resolved##*/tag/}
		case "$tag" in
		v*) echo "$tag" && return ;;
		esac
	else
		tag=$(http_get "https://api.github.com/repos/${REPO}/releases/latest" |
			sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
		case "$tag" in
		v*) echo "$tag" && return ;;
		esac
	fi
	die "no published release yet. Pick one from https://github.com/${REPO}/releases and set FAULTLINE_VERSION"
}

verify() {
	dir=$1
	name=$2
	# GoReleaser writes "<sha256>  <filename>" a line at a time.
	expected=$(grep -E "[[:space:]]\*?${name}\$" "${dir}/checksums.txt" | awk '{print $1}' | head -n 1)
	[ -n "$expected" ] || die "${name} is not listed in checksums.txt"

	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "${dir}/${name}" | awk '{print $1}')
	else
		actual=$(shasum -a 256 "${dir}/${name}" | awk '{print $1}')
	fi

	[ "$expected" = "$actual" ] ||
		die "checksum mismatch for ${name}: expected ${expected}, got ${actual}"
}

need_tools() {
	command -v tar >/dev/null 2>&1 || die "tar is required"
	command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 ||
		die "curl or wget is required"
	command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 ||
		die "sha256sum or shasum is required to verify the download"
}

http_get() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1"
	else
		wget -qO- "$1"
	fi
}

download() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	else
		wget -qO "$2" "$1"
	fi
}

warn_if_off_path() {
	case ":${PATH}:" in
	*":$1:"*) ;;
	*) say "note: $1 is not on your PATH. Add it, for example: export PATH=\"$1:\$PATH\"" ;;
	esac
}

say() { echo "faultline: $*" >&2; }
die() {
	echo "faultline: $*" >&2
	exit 1
}

main "$@"
