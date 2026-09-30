#!/bin/sh
# Installs the meshstack binary from the GitHub releases of meshcloud/meshstack-cli.
#
#   curl -fsSL https://raw.githubusercontent.com/meshcloud/meshstack-cli/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/meshcloud/meshstack-cli/main/install.sh | sh -s -- --version v0.2.1 --dir ~/bin
#
# The script is plain POSIX sh on purpose, so that dash, busybox ash, the Bourne-compatible sh of
# macOS and the bash 3 of old systems, Cygwin and Git Bash all run it: no `local`, no arrays, no
# `[[`, and printf instead of echo. Check it with `shellcheck --shell=sh install.sh`.
#
# The archive and checksum names follow name_template in .goreleaser.yml and must stay in step
# with it.
#
# Everything runs inside main, which the last line calls, so a download that breaks off halfway
# runs nothing.

set -u

REPO=meshcloud/meshstack-cli
PROJECT=meshstack-cli
BINARY=meshstack

say() {
  printf '%s\n' "$*"
}

warn() {
  printf 'warning: %s\n' "$*" >&2
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<EOF
Usage: install.sh [--version vX.Y.Z] [--dir DIRECTORY]

Downloads the meshstack binary for this system from https://github.com/$REPO/releases,
checks its SHA-256 checksum and installs it.

  --version  The release to install. Defaults to the latest release.
  --dir      The directory to install into. Defaults to the first user-writable
             directory on PATH, or to \$HOME/.local/bin.

The environment variables MESHSTACK_INSTALL_VERSION and MESHSTACK_INSTALL_DIR set the
same values as the flags.
EOF
}

has() {
  command -v "$1" >/dev/null 2>&1
}

fetch() {
  curl -fsSL --retry 3 -o "$2" "$1"
}

detect_os() {
  os_name=$(uname -s 2>/dev/null) || die "uname is missing, so the operating system is unknown"
  case $os_name in
    Linux | linux) printf 'linux' ;;
    Darwin | darwin) printf 'darwin' ;;
    MINGW* | MSYS* | CYGWIN* | Windows_NT) printf 'windows' ;;
    *) die "no meshstack release exists for the operating system '$os_name'" ;;
  esac
}

# uname -m names the architecture of the kernel, not of the userland. That is the right question
# here: the release binaries are statically linked, so an arm64 binary also runs under a 32-bit
# userland on a 64-bit kernel.
detect_arch() {
  arch_name=$(uname -m 2>/dev/null) || die "uname is missing, so the CPU architecture is unknown"
  case $arch_name in
    x86_64 | x86-64 | x64 | amd64 | AMD64) arch=amd64 ;;
    aarch64 | aarch64_be | arm64 | ARM64 | armv8* | arm64e) arch=arm64 ;;
    *) die "no meshstack release exists for the CPU architecture '$arch_name'" ;;
  esac

  # A shell that runs under Rosetta 2 reports x86_64 on an Apple Silicon Mac.
  if [ "$1" = darwin ] && [ "$arch" = amd64 ]; then
    if [ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = 1 ] ||
      [ "$(sysctl -n hw.optional.arm64 2>/dev/null)" = 1 ]; then
      arch=arm64
    fi
  fi

  # No windows/arm64 release exists, and Windows on ARM runs the amd64 binary under emulation.
  if [ "$1" = windows ] && [ "$arch" = arm64 ]; then
    arch=amd64
  fi

  printf '%s' "$arch"
}

# GitHub answers /releases/latest with a redirect to /releases/tag/<tag>. Following it avoids the
# REST API, its rate limit and any JSON parsing.
latest_version() {
  latest_url=$(curl -fsSLI --retry 3 -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
    die "could not reach https://github.com/$REPO/releases/latest"
  latest_tag=${latest_url##*/}
  case $latest_tag in
    v[0-9]*) printf '%s' "$latest_tag" ;;
    *) die "could not read the latest release from '$latest_url'; pass --version vX.Y.Z" ;;
  esac
}

sha256_of() {
  if has sha256sum; then
    sha256sum "$1" | cut -d ' ' -f 1
  elif has shasum; then
    shasum -a 256 "$1" | cut -d ' ' -f 1
  elif has openssl; then
    # Prints "SHA2-256(file)= <hash>" or "SHA256(file)= <hash>", depending on the version.
    openssl dgst -sha256 "$1" | sed 's/^.*= *//'
  else
    return 1
  fi
}

verify_checksum() {
  expected=$(grep " \*\{0,1\}$2\$" "$3" | cut -d ' ' -f 1)
  [ -n "$expected" ] || die "the checksum file has no entry for $2"
  if ! actual=$(sha256_of "$1"); then
    warn "found none of sha256sum, shasum or openssl, so the checksum of $2 is not verified"
    return 0
  fi
  [ "$actual" = "$expected" ] || die "the checksum of $2 is $actual, but the release says $expected"
}

extract() {
  case $1 in
    *.tar.gz)
      # gzip and tar apart, because an old tar has no -z.
      gzip -dc "$1" | (cd "$2" && tar -xf -) || die "could not extract $1"
      ;;
    *.zip)
      if has unzip; then
        unzip -oq "$1" -d "$2" || die "could not extract $1"
      else
        # The tar of Windows 10 and later is bsdtar, which reads zip files.
        (cd "$2" && tar -xf "$1") || die "could not extract $1; install unzip and run again"
      fi
      ;;
  esac
}

is_writable_dir() {
  [ -n "$1" ] && [ -d "$1" ] && [ -w "$1" ]
}

# Another tool rewrites these directories and may remove a file it did not put there.
is_managed_dir() {
  case $1 in
    */shims | */shims/* | */.nix-profile/* | /nix/* | */node_modules/* | */.asdf/* | */mise/*) return 0 ;;
    *) return 1 ;;
  esac
}

on_path() {
  case ":$PATH:" in
    *":$1:"* | *":$1/:"*) return 0 ;;
    *) return 1 ;;
  esac
}

find_install_dir() {
  home=${HOME:-}
  candidates=
  if [ "$(id -u 2>/dev/null)" = 0 ]; then
    candidates=/usr/local/bin
  fi
  if [ -n "$home" ]; then
    candidates="$candidates:$home/.local/bin:$home/bin:$home/.bin"
  fi
  in_home=
  elsewhere=
  old_ifs=$IFS
  IFS=:
  for dir in $PATH; do
    dir=${dir%/}
    if ! is_writable_dir "$dir" || is_managed_dir "$dir"; then
      continue
    fi
    if [ -n "$home" ] && [ "${dir#"$home"/}" != "$dir" ]; then
      in_home="$in_home:$dir"
    else
      elsewhere="$elsewhere:$dir"
    fi
  done
  for dir in $candidates; do
    if is_writable_dir "$dir" && on_path "$dir"; then
      IFS=$old_ifs
      printf '%s' "$dir"
      return 0
    fi
  done
  for dir in $in_home $elsewhere; do
    if [ -n "$dir" ]; then
      IFS=$old_ifs
      printf '%s' "$dir"
      return 0
    fi
  done
  IFS=$old_ifs
}

print_path_hint() {
  say ""
  say "$1 is not on your PATH. Add it for your shell, then open a new terminal:"
  case ${SHELL:-} in
    */zsh)
      say "  printf '%s\\n' 'export PATH=\"$1:\$PATH\"' >> ~/.zshrc"
      ;;
    */bash)
      if [ "$(uname -s)" = Darwin ]; then
        say "  printf '%s\\n' 'export PATH=\"$1:\$PATH\"' >> ~/.bash_profile"
      else
        say "  printf '%s\\n' 'export PATH=\"$1:\$PATH\"' >> ~/.bashrc"
      fi
      ;;
    */fish)
      say "  fish_add_path '$1'"
      ;;
    *)
      say "  printf '%s\\n' 'export PATH=\"$1:\$PATH\"' >> ~/.profile"
      ;;
  esac
}

main() {
  version=${MESHSTACK_INSTALL_VERSION:-}
  install_dir=${MESHSTACK_INSTALL_DIR:-}
  while [ $# -gt 0 ]; do
    case $1 in
      --version | -v)
        [ $# -ge 2 ] || die "$1 needs a value"
        version=$2
        shift 2
        ;;
      --version=*) version=${1#*=} && shift ;;
      --dir | -d)
        [ $# -ge 2 ] || die "$1 needs a value"
        install_dir=$2
        shift 2
        ;;
      --dir=*) install_dir=${1#*=} && shift ;;
      --help | -h) usage && exit 0 ;;
      *) usage >&2 && die "unknown argument '$1'" ;;
    esac
  done

  for tool in curl uname gzip tar grep cut sed mkdir cp chmod mv rm; do
    has "$tool" || die "this script needs $tool, which is not on PATH"
  done

  os=$(detect_os) || exit 1
  arch=$(detect_arch "$os") || exit 1

  if [ -z "$version" ]; then
    version=$(latest_version) || exit 1
  fi
  case $version in
    v*) ;;
    *) version=v$version ;;
  esac

  exe=$BINARY
  ext=tar.gz
  if [ "$os" = windows ]; then
    exe=$BINARY.exe
    ext=zip
  fi
  archive=${PROJECT}_${version#v}_${os}_${arch}.$ext
  checksums=${PROJECT}_${version#v}_SHA256SUMS
  base_url=https://github.com/$REPO/releases/download/$version

  tmp=$(mktemp -d 2>/dev/null) || {
    tmp=${TMPDIR:-/tmp}/meshstack-install.$$
    (umask 077 && mkdir "$tmp") || die "could not create a temporary directory"
  }
  trap 'rm -rf "$tmp"' 0
  trap 'exit 1' 1 2 15

  say "Downloading meshstack $version for $os/$arch"
  fetch "$base_url/$archive" "$tmp/$archive" || die "could not download $base_url/$archive"
  fetch "$base_url/$checksums" "$tmp/$checksums" || die "could not download $base_url/$checksums"
  verify_checksum "$tmp/$archive" "$archive" "$tmp/$checksums"

  mkdir "$tmp/extracted" || die "could not create a temporary directory"
  extract "$tmp/$archive" "$tmp/extracted"
  [ -f "$tmp/extracted/$exe" ] || die "the archive $archive holds no $exe"

  if [ -z "$install_dir" ]; then
    install_dir=$(find_install_dir)
  fi
  if [ -z "$install_dir" ]; then
    [ -n "${HOME:-}" ] || die "HOME is not set; pass --dir"
    install_dir=$HOME/.local/bin
  fi
  mkdir -p "$install_dir" || die "could not create $install_dir; pass another --dir"
  is_writable_dir "$install_dir" || die "$install_dir is not writable; pass another --dir, or run as a user who may write to it"

  # Copy next to the target and rename, so a running meshstack is replaced and not overwritten.
  staged=$install_dir/.$exe.$$
  cp "$tmp/extracted/$exe" "$staged" || die "could not write to $install_dir"
  if ! chmod 755 "$staged" || ! mv -f "$staged" "$install_dir/$exe"; then
    rm -f "$staged"
    die "could not install $install_dir/$exe"
  fi

  say "Installed $("$install_dir/$exe" --version 2>/dev/null || printf 'meshstack version %s' "$version") to $install_dir/$exe"

  if ! on_path "$install_dir"; then
    print_path_hint "$install_dir"
  else
    found=$(command -v "$exe" 2>/dev/null || true)
    if [ -n "$found" ] && [ "$found" != "$install_dir/$exe" ]; then
      warn "$found comes first on your PATH, so '$BINARY' still runs that one"
    fi
  fi
}

main "$@"
