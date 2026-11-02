# Contributing to Moko

Thanks for helping. Bug reports, reports from other Sharp models and focused pull
requests are all welcome. Issues and PRs can be written in English or Chinese.

## Build

You need Go 1.27.3 or later. The MyGo CLI is pinned in `go.mod` as a tool, so there is
nothing else to install.

```sh
go tool mygo dev      # run with live reload
go test ./...
go run ./cmd/mokoctl  # print what the purifier reports; it only reads
```

To build release-style apps without the maintainer's Developer ID (ad hoc signature, no
notarization):

```sh
go tool mygo build -platform darwin/arm64,windows/amd64 -sign - -skip-notarize
```

- `build/darwin-arm64/Moko.app` runs on the Mac that built it.
- `build/windows-amd64/` holds `Moko.exe` and, if NSIS is installed (`brew install
  makensis` on macOS), the installer.

Everything cross-compiles from one Mac; no cgo is involved. Official releases are built
with `scripts/release.sh`, which needs the maintainer's Developer ID identity,
notarization profile and update key.

To see every screen with made-up data, without a purifier:

```sh
MOKO_RENDER=$PWD/docs/assets go test ./internal/view -run TestRender  # screenshots
MOKO_ASSETS=$PWD/assets go test ./internal/view -run TestAssets       # tray icons
```

`assets/icon.svg` is the source of the icon. `TestAssets` also draws the art behind the
disk image's window; `swift scripts/make-dmg-background.swift
assets/dmg/background-source.png assets/dmg/background.tiff` adds its words.

## Before you open a pull request

- `gofmt -l .` prints nothing.
- `GOOS=darwin GOARCH=arm64 go vet ./...` and `GOOS=windows GOARCH=amd64 go vet ./...`
  pass. CI runs the same checks.
- `go mod tidy -diff` prints nothing, and every new dependency is credited in
  THIRD-PARTY-NOTICES.md; CI checks that.
- `go test ./...` passes. Tests must not talk to a real purifier or to the internet.
- UI text exists in Chinese, Japanese and English; update all three.
- Keep pull requests small and about one thing.

## Writing to the device

The layout of Sharp's private properties is undocumented. See
[docs/device-echonet.md](docs/device-echonet.md) for what has been confirmed on a real
device, and how.

- Never write EPC `F4`, whose meaning is unknown.
- Commands to `F3` are built byte by byte, with only the mask and the target fields set.
  Never send back what was read.
- Every write is confirmed by reading it back.
- Reading is always fine: learn a new field by reading while the official app changes it.

When you report a problem or a new field, leave out IP and MAC addresses, node IDs and
the raw contents of `F0`.

## License

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE).
