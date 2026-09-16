# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only

# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
#
# Runtime image for scout.
#
# The binary is expected to already be built by goreleaser and dropped at
# the build context root as `scout`. Do NOT compile from source in this
# Dockerfile: goreleaser's dockers: section produces one image per arch
# reusing the same statically-linked binary it publishes as a tar.gz.
#
# The base is distroless/static: scout is a static binary with CGO off, and
# needs nothing from the image but CA certificates and a non-root user,
# both of which distroless ships. It is pinned by digest, which OpenSSF
# Scorecard checks and which release-config in ci.yml refuses to release
# without. The digest below is a PLACEHOLDER: resolve the real one with
#   docker buildx imagetools inspect gcr.io/distroless/static-debian12:nonroot
# and replace it before the first release. Dependabot keeps it fresh after.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

COPY scout /usr/local/bin/scout

LABEL org.opencontainers.image.source="https://github.com/sebastienrousseau/scout" \
      org.opencontainers.image.description="scout: onboard, test and diagnose remote MCP servers end to end" \
      org.opencontainers.image.licenses="GPL-3.0-only" \
      org.opencontainers.image.title="scout" \
      org.opencontainers.image.vendor="Sebastien Rousseau"

# distroless nonroot is uid/gid 65532. Numeric, so Kubernetes runAsNonRoot
# can verify it.
USER 65532:65532
WORKDIR /home/nonroot

ENTRYPOINT ["/usr/local/bin/scout"]
CMD ["--help"]
