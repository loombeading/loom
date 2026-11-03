class Lm < Formula
  desc "Local task tracker for AI coding agents"
  homepage "https://github.com/loombeading/loom"
  version "0.2611.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/loombeading/loom/releases/download/v0.2611.0/lm_darwin_arm64"
      sha256 "93f009dc6fe8c42f5764edb222f79c2d882f3b5485e41eb540ea76423fe4cb6b"
    end
    on_intel do
      url "https://github.com/loombeading/loom/releases/download/v0.2611.0/lm_darwin_amd64"
      sha256 "7e5efac098e4ae013451db9f870ef29efe8aab43ffcdaa744811088445a4a5e5"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/loombeading/loom/releases/download/v0.2611.0/lm_linux_arm64"
      sha256 "1722bd7493ef4aa411717b2ff37b862f3157867a4f5b1a8e13a1a7a24e3492c9"
    end
    on_intel do
      url "https://github.com/loombeading/loom/releases/download/v0.2611.0/lm_linux_amd64"
      sha256 "67be6152ca67a13ee6243f24fc25fdf252ac80a2c7233b15d3fc0ee4fcb90375"
    end
  end

  def install
    bin.install Dir["lm_*"].first => "lm"
  end

  test do
    assert_match "lm v0.2611.0 ", shell_output("#{bin}/lm version")
  end
end
