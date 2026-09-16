<!--
SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
SPDX-License-Identifier: GPL-3.0-only
-->

# Arch User Repository

**Recipe:** the `aurs` section of [`.goreleaser.yaml`](../../.goreleaser.yaml),
which generates a `PKGBUILD` from the release's own checksums. Not
duplicated here; see [`../README.md`](../README.md) for why.

## Not yet published

Pushing to `aur.archlinux.org` needs an SSH key registered with the AUR,
which this repository does not hold. The release workflow therefore
generates the `PKGBUILD` and **attaches it to the GitHub release** as an
asset, and stops there. Until a maintainer registers `scout-bin` and
adds the key as an `AUR_KEY` secret, the package is not installable
with `yay` or `paru`.

This is stated here rather than hidden because a package page that says
"install with yay" while the package does not exist is worse than none.

## Package

`scout-bin`: the released binary, not a source build. The `-bin`
suffix is the Arch convention and it is accurate: the package installs
the artefact this project's release workflow built and signed, rather
than compiling from source on the user's machine.

## Install from the attached PKGBUILD

```bash
mkdir scout-bin && cd scout-bin
curl -fsSLO "https://github.com/sebastienrousseau/scout/releases/download/v<version>/PKGBUILD"
makepkg -si
```

`makepkg` checks the `sha256sums` in the `PKGBUILD`. For the signature
and provenance, see [`../VERIFY.md`](../VERIFY.md).
