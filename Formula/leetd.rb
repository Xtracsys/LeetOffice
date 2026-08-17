# Homebrew formula for LeetOffice. No tap yet — install from this file:
#   brew install --formula https://raw.githubusercontent.com/Xtracsys/LeetOffice/main/Formula/leetd.rb
#
# Checksums from https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.1/checksums-0.1.1.txt
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/Xtracsys/LeetOffice"
  version "0.1.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.1/leetd-0.1.1-darwin-arm64"
      sha256 "4afe4a3cf110ddfc690888132cf3a264ee66f61550d8600c9dc78288e7a86c83"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.1/leetd-0.1.1-darwin-amd64"
      sha256 "00b6d2913d5d87bec4194d22cabaca6706682dfbddda9b539fc08f093a1df59c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.1/leetd-0.1.1-linux-arm64"
      sha256 "fc020f01a3c1e9be383c7eaeabb11c8dd3054b2a542d7b4f7ad4fa83a993d478"
    end
    on_intel do
      url "https://github.com/Xtracsys/LeetOffice/releases/download/v0.1.1/leetd-0.1.1-linux-amd64"
      sha256 "56e7ee36aca29ee7f37eb12076c59ac17ebf355ad5aed458023d685e89c8cd56"
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
