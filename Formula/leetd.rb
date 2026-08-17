# Homebrew formula for LeetOffice. No tap yet — install from this file:
#   brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
#
# Checksums from https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.2/checksums-0.1.2.txt
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/Xtracsys/LeetOffice"
  version "0.1.2"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.2/leetd-0.1.2-darwin-arm64"
      sha256 "c2da8382307ce520d684bc3939a1707582480274b987eddbebfb509e8693b34d"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.2/leetd-0.1.2-darwin-amd64"
      sha256 "62c94a0de463b261bee00c44a50f98aee5a9e319f505d0683fb5e86d36af2cf1"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.2/leetd-0.1.2-linux-arm64"
      sha256 "43c564dbfb41e28872eb1769d9633d90e2778096562b2a17bff631dfd7d432c5"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.2/leetd-0.1.2-linux-amd64"
      sha256 "ec4f31a60166e618b1b756741dfbe0f2a2d6f8839eb0c0b7a2bb8062abe9912e"
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
