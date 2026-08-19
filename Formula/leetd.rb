# Homebrew formula for LeetOffice. No tap yet — install from this file:
#   brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
#
# Checksums from https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.8/checksums-0.1.8.txt
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/Xtracsys/LeetOffice"
  version "0.1.8"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.8/leetd-0.1.8-darwin-arm64"
      sha256 "600ecb7291dba8fde8c5cd21d83fa3213b1b3590c80d8c2e076aec2b0f2108c1"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.8/leetd-0.1.8-darwin-amd64"
      sha256 "b3961f01bf7ac489c712add6c50157482f541d324e8d08cad35311777a4243a7"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.8/leetd-0.1.8-linux-arm64"
      sha256 "8bc6a80abc8657dac0ac2028cb90fd6bfa09018517a411c022ca26dfab6ac60d"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.8/leetd-0.1.8-linux-amd64"
      sha256 "63a875133bd5fd21ececa7c470cf1ca5daf8419bc2c4401d92601d5ddde9c0f8"
    end
  end

  def install
    os = OS.mac? ? "darwin" : "linux"
    arch = Hardware::CPU.intel? ? "amd64" : "arm64"
    bin.install "leetd-#{version}-#{os}-#{arch}" => "leetd"
    if OS.mac?
      system "codesign", "--force", "--sign", "-", "--identifier", "dev.leetoffice.leetd",
             "--timestamp=none", bin/"leetd"
    end
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
