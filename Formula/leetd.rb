# Homebrew formula for LeetOffice (for the leetoffice/tap repository).
# Once the tap repo exists: brew tap leetoffice/tap && brew install leetd
class Leetd < Formula
  desc "100% local multi-human, multi-agent workspace (chat, docs, git-audited store)"
  homepage "https://github.com/leetoffice/leetoffice"
  url "https://github.com/leetoffice/leetoffice/releases/download/v0.1.0/leetd-0.1.0-darwin-arm64"
  version "0.1.0"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000" # fill per release
  license "Apache-2.0"

  def install
    bin.install "leetd-#{version}-#{OS.mac? ? "darwin" : "linux"}-#{Hardware::CPU.intel? ? "amd64" : "arm64"}" => "leetd"
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
