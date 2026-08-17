# Homebrew formula for LeetOffice. No tap yet — install from this file:
#   brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
#
# Checksums from https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.0/checksums-0.1.0.txt
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/Xtracsys/LeetOffice"
  version "0.1.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.0/leetd-0.1.0-darwin-arm64"
      sha256 "a2e6d131d6a5b920fa9422e53062444f9ab52efde813d2020f40f0310d139ea1"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.0/leetd-0.1.0-darwin-amd64"
      sha256 "05e5697def088d5ed383bab40e983eb6cc5534de0f182aa49dfd156870526a08"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.0/leetd-0.1.0-linux-arm64"
      sha256 "8823d869e50a4c926b32cc3d588ba999ceac194fed0471066e8de9e711a290dd"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.0/leetd-0.1.0-linux-amd64"
      sha256 "0c8f63d8be3c1f5cb42db2dfa41fa7445453191c41da99fc3a30ddaf6f5a3ff3"
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
