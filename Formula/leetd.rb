# Homebrew formula for LeetOffice. No tap yet — install from this file:
#   brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
#
# Checksums from https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.6/checksums-0.1.6.txt
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/Xtracsys/LeetOffice"
  version "0.1.6"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.6/leetd-0.1.6-darwin-arm64"
      sha256 "830e5678fbc61341fb887656f50926d4267762378fb27193132e6400cf84cc47"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.6/leetd-0.1.6-darwin-amd64"
      sha256 "7b0567bd6e5f8ad19203802507ff4b5326ae2a4a5f0f7201bf1d2a78952fefbb"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.6/leetd-0.1.6-linux-arm64"
      sha256 "0902c84bf7f7cce88d9fdfcc938fdab13abdf2394015066a470a43a3cde61ef3"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.6/leetd-0.1.6-linux-amd64"
      sha256 "74e4d04d8570c0aa45de52893b700311377390437baa47619ebac1e51fbc15ee"
    end
  end

  def install
    os = OS.mac? ? "darwin" : "linux"
    arch = Hardware::CPU.intel? ? "amd64" : "arm64"
    bin.install "leetd-#{version}-#{os}-#{arch}" => "leetd"
  end

  def caveats
    <<~EOS
      Run `leetd` once — the first-run wizard opens at http://127.0.0.1:7667.
      Click "make always-on" in Settings to register it as a login service.
    EOS
  end

  test do
    assert_match "leetoffice", shell_output("#{bin}/leetd version")
  end
end
