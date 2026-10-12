class Lm < Formula
  desc "Local task tracker for AI coding agents"
  homepage "https://github.com/loombeading/loom"
  version "0.2610.1"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/loombeading/loom/releases/download/v0.2610.1/lm_darwin_arm64"
      sha256 "9324d7408514d81eafe8cc70c0336f216332fa95461f7cf4a01d2a07c0bf07da"
    end
    on_intel do
      url "https://github.com/loombeading/loom/releases/download/v0.2610.1/lm_darwin_amd64"
      sha256 "f8a22e29c104b58c38aeb8efb9ef5b329ff12ecbf4605d998086ed4f7de2a1a2"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/loombeading/loom/releases/download/v0.2610.1/lm_linux_arm64"
      sha256 "31f6e97015c4b809241cadc567f95b0541cbd1503d62db25180bb47f81bd8792"
    end
    on_intel do
      url "https://github.com/loombeading/loom/releases/download/v0.2610.1/lm_linux_amd64"
      sha256 "a20da4b29d83af6eef11a25440106a7c613ad4b9166f5c469f68c4f0b1865118"
    end
  end

  def install
    bin.install Dir["lm_*"].first => "lm"
  end

  test do
    assert_match "lm v0.2610.1 ", shell_output("#{bin}/lm version")
  end
end
