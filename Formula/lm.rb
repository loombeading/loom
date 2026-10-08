class Lm < Formula
  desc "Local task tracker for AI coding agents"
  homepage "https://github.com/loombeading/loom"
  version "0.2610.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/loombeading/loom/releases/download/v0.2610.0/lm_darwin_arm64"
      sha256 "28eac81c667d7a5f11c3f79a886a2b73cdf1067a531d8d994ed35f302c0c579d"
    end
    on_intel do
      url "https://github.com/loombeading/loom/releases/download/v0.2610.0/lm_darwin_amd64"
      sha256 "d57044d8da6154c0f7cada8db99e89bbab203f8112ae1b6648cc4c4f8b44f42d"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/loombeading/loom/releases/download/v0.2610.0/lm_linux_arm64"
      sha256 "0be012032f5edea02c5413a70624a3c48192a094aef93a1507c9c6b57f560fb7"
    end
    on_intel do
      url "https://github.com/loombeading/loom/releases/download/v0.2610.0/lm_linux_amd64"
      sha256 "3f7954bd2323e98547fbd4987037ddb79a49d2b1b6246ce7d99039e5bfc08fb9"
    end
  end

  def install
    bin.install Dir["lm_*"].first => "lm"
  end

  test do
    assert_match "lm v0.2610.0 ", shell_output("#{bin}/lm version")
  end
end
