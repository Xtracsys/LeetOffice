# Homebrew formula for LeetOffice. No tap yet — install from this file:
#   brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
#
# Checksums from https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.7/checksums-0.1.7.txt
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/Xtracsys/LeetOffice"
  version "0.1.7"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.7/leetd-0.1.7-darwin-arm64"
      sha256 "2b845e8f8861734e2da057ced0cea67d414e17d2b1078e78f1b368bca7a6d5cc"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.7/leetd-0.1.7-darwin-amd64"
      sha256 "c402bcf44cc1b5623a09e05816536ed3a6a73ee4c95f4b669d1ba6ea9c56f7b4"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.7/leetd-0.1.7-linux-arm64"
      sha256 "f27730371760bed606419abacff8ab0c5eebc126286267c795793d3e133fa95e"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.7/leetd-0.1.7-linux-amd64"
      sha256 "068223b4c5a5e74051dc2d480ee1b636e73bd0d66457d006d753bc77b3f90fc8"
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
