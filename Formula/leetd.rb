# Homebrew formula for LeetOffice. No tap yet — install from this file:
#   brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
#
# Checksums from https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.5/checksums-0.1.5.txt
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/Xtracsys/LeetOffice"
  version "0.1.5"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.5/leetd-0.1.5-darwin-arm64"
      sha256 "33e93e5198a99f0139ebbe39e33c12aad69e32eb447542cd83ab657a32950a36"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.5/leetd-0.1.5-darwin-amd64"
      sha256 "79c435367ec5f5c7398df89ed70b5b61bedadc8c9a8fdc3ae9549ca5036be262"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.5/leetd-0.1.5-linux-arm64"
      sha256 "c8609a935aa48d5c6a8ed69585f58c347a5c35ef69c1eb1c825be5ee09c3e119"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.5/leetd-0.1.5-linux-amd64"
      sha256 "b349824d1c400b780ea8bf286a98f808f06e7c8b1697f3dd91df6650a3e39c28"
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
