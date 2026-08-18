# Homebrew formula for LeetOffice. No tap yet — install from this file:
#   brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
#
# Checksums from https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.4/checksums-0.1.4.txt
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/Xtracsys/LeetOffice"
  version "0.1.4"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.4/leetd-0.1.4-darwin-arm64"
      sha256 "59edbf58eab56dcb83722d4ccfddee6d2836f54f5c0d34869491573e2d01a3b2"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.4/leetd-0.1.4-darwin-amd64"
      sha256 "b19f3c1823c4d31dc8c7b39ea99162baf22e9da4bb04e295c41e95e6c06bc868"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.4/leetd-0.1.4-linux-arm64"
      sha256 "31b3c1c6204d69f7efdd4d4009dcf687d6bef0fe220a596fe80e29964d4ea66e"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.4/leetd-0.1.4-linux-amd64"
      sha256 "82ce49e1f6e0356f29c2dd7735f812ae070d464e53b2f1bb684c006eab45cd39"
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
