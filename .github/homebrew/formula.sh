#!/usr/bin/env bash
# Prints the Homebrew formula for one release, from that release's SHA256SUMS file:
#
#   .github/homebrew/formula.sh v1.2.3 meshstack-cli_1.2.3_SHA256SUMS > Formula/meshstack-cli.rb
#
# A formula rather than a cask, because Homebrew quarantines only what a cask downloads, and an
# unquarantined binary runs without an Apple signature. The release workflow writes the result into
# meshcloud/homebrew-tap; the archive names follow `archives.name_template` in .goreleaser.yml.
set -euo pipefail

tag="$1"
sums="$2"
version="${tag#v}"

sha256() {
  local archive="meshstack-cli_${version}_$1.tar.gz"
  local sum
  sum="$(awk -v f="$archive" '$2 == f { print $1 }' "$sums")"
  if [[ -z "$sum" ]]; then
    echo "no checksum for $archive in $sums" >&2
    exit 1
  fi
  echo "$sum"
}

url() {
  echo "https://github.com/meshcloud/meshstack-cli/releases/download/${tag}/meshstack-cli_${version}_$1.tar.gz"
}

# Resolved before the heredoc, because a failing command substitution inside one does not stop
# the script, even under `set -e`.
darwin_arm64="$(sha256 darwin_arm64)"
darwin_amd64="$(sha256 darwin_amd64)"
linux_arm64="$(sha256 linux_arm64)"
linux_amd64="$(sha256 linux_amd64)"

cat <<EOF
# Written by the release workflow of meshcloud/meshstack-cli on every release, which overwrites any
# change made here.
class MeshstackCli < Formula
  desc "Command-line interface for meshStack"
  homepage "https://github.com/meshcloud/meshstack-cli"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "$(url darwin_arm64)"
      sha256 "${darwin_arm64}"
    end
    on_intel do
      url "$(url darwin_amd64)"
      sha256 "${darwin_amd64}"
    end
  end

  on_linux do
    on_arm do
      url "$(url linux_arm64)"
      sha256 "${linux_arm64}"
    end
    on_intel do
      url "$(url linux_amd64)"
      sha256 "${linux_amd64}"
    end
  end

  def install
    bin.install "meshstack"
    generate_completions_from_executable(bin/"meshstack", "completion")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/meshstack --version")
  end
end
EOF
