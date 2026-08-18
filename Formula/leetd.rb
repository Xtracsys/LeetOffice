# Homebrew formula for LeetOffice. No tap yet — install from this file:
#   brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
#
# Checksums from https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.3/checksums-0.1.3.txt
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/Xtracsys/LeetOffice"
  version "0.1.3"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.3/leetd-0.1.3-darwin-arm64"
      sha256 "2f6a2efd1091bf0959f61ab7994eb081413156a1adcab5fc8cdf5ee12ffc0773"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.3/leetd-0.1.3-darwin-amd64"
      sha256 "e8ff7345609037f7baae8d9345ad63b6a5e32559f286f583b15b9a54f0c14c46"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.3/leetd-0.1.3-linux-arm64"
      sha256 "657a2f2b7cf3986337460a7673fb38133fe8ba8618813698905d1731e2bd702e"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.3/leetd-0.1.3-linux-amd64"
      sha256 "7a38970eeb30ad38a8a1f5ffb32ec7d0c9b75aeec28b0d2897cb8e08887ca043"
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
