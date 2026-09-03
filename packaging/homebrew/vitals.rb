# typed: false
# frozen_string_literal: true

# Template — the release workflow substitutes __VERSION__, __BASE__ and the
# __SHA_*__ placeholders and commits the result to Formula/vitals.rb in the tap.
class Vitals < Formula
  desc "Local-first system diagnostics: names your bottleneck and the fix"
  homepage "https://github.com/gautampachnanda101/vitals"
  version "__VERSION__"
  license "MIT"

  on_macos do
    on_intel do
      url "__BASE__/vitals_Darwin_x86_64.tar.gz"
      sha256 "__SHA_DARWIN_X86_64__"
    end
    on_arm do
      url "__BASE__/vitals_Darwin_arm64.tar.gz"
      sha256 "__SHA_DARWIN_ARM64__"
    end
  end

  on_linux do
    on_intel do
      url "__BASE__/vitals_Linux_x86_64.tar.gz"
      sha256 "__SHA_LINUX_X86_64__"
    end
    on_arm do
      url "__BASE__/vitals_Linux_arm64.tar.gz"
      sha256 "__SHA_LINUX_ARM64__"
    end
  end

  def install
    bin.install "vitals"
    generate_completions_from_executable(bin/"vitals", "completion")
    # Vitals.app (roadmap item 004, macOS archives only) isn't installed
    # to /Applications — that's a Homebrew Cask's job, not a Formula's;
    # dropped in the keg's prefix instead, see caveats for how to use it.
    prefix.install "Vitals.app" if File.directory?("Vitals.app")
  end

  def caveats
    <<~EOS
      Get a verdict for this machine:
        vitals doctor

      Full guide:  vitals guide   |   per-command help:  vitals help <cmd>

      A double-clickable dashboard launcher is at:
        #{opt_prefix}/Vitals.app
      Copy it to /Applications if you want it there — Homebrew Formulae
      don't install GUI apps to /Applications themselves. It's unsigned;
      the first open needs right-click -> Open to get past Gatekeeper.
    EOS
  end

  test do
    assert_match "vitals", shell_output("#{bin}/vitals version")
    assert_match "doctor", shell_output("#{bin}/vitals help")
  end
end
