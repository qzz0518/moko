#!/usr/bin/env swift
// Moko 的 DMG 背景：把 assets/dmg/background-source.png（TestAssets 用应用自己的 Moko 画出）
// 裁成 660x400，画上标题、副标题和一道当箭头用的风（沿用 Charker 的 make-dmg-background.swift）。
// 钴蓝是中间调，Finder 的图标名无论黑字白字都看得清，所以不需要底板。
// 1x、2x 各画一张，用 tiffutil 合成多分辨率 TIFF，Retina 屏上 Finder 取 2x。
//
//   swift scripts/make-dmg-background.swift assets/dmg/background-source.png assets/dmg/background.tiff
import AppKit
import Foundation

private let arguments = CommandLine.arguments
guard arguments.count == 3 else {
    fputs("usage: make-dmg-background.swift <source.png> <output.tiff>\n", stderr)
    exit(2)
}

let sourceURL = URL(fileURLWithPath: arguments[1])
let outputURL = URL(fileURLWithPath: arguments[2])
guard let source = NSImage(contentsOf: sourceURL) else {
    fputs("cannot read \(sourceURL.path)\n", stderr)
    exit(1)
}

let size = NSSize(width: 660, height: 400)
let targetAspect = size.width / size.height
let sourceAspect = source.size.width / source.size.height
let sourceRect: NSRect
if sourceAspect > targetAspect {
    let width = source.size.height * targetAspect
    sourceRect = NSRect(
        x: (source.size.width - width) / 2,
        y: 0,
        width: width,
        height: source.size.height
    )
} else {
    let height = source.size.width / targetAspect
    sourceRect = NSRect(
        x: 0,
        y: (source.size.height - height) / 2,
        width: source.size.width,
        height: height
    )
}

// render 按点画出背景，位图像素是点数的 scale 倍。
func render(scale: Int) -> Data {
    guard let bitmap = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: Int(size.width) * scale,
        pixelsHigh: Int(size.height) * scale,
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    ) else {
        fputs("cannot create DMG background canvas\n", stderr)
        exit(1)
    }
    // 先定点数再建上下文，上下文才会按 scale 缩放。
    bitmap.size = size
    guard let context = NSGraphicsContext(bitmapImageRep: bitmap) else {
        fputs("cannot create DMG background canvas\n", stderr)
        exit(1)
    }
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = context
    context.imageInterpolation = .high
    source.draw(
        in: NSRect(origin: .zero, size: size),
        from: sourceRect,
        operation: .copy,
        fraction: 1
    )

    let centered = NSMutableParagraphStyle()
    centered.alignment = .center

    let title = "拖动安装  ·  Drag to install"
    let titleAttributes: [NSAttributedString.Key: Any] = [
        .font: NSFont.systemFont(ofSize: 21, weight: .semibold),
        .foregroundColor: NSColor.white,
        .paragraphStyle: centered,
        .kern: 0.2,
    ]
    title.draw(
        in: NSRect(x: 40, y: 329, width: 580, height: 30),
        withAttributes: titleAttributes
    )

    let subtitle = "将 Moko 拖到“应用程序”文件夹  ·  Drag Moko to the Applications folder"
    let subtitleAttributes: [NSAttributedString.Key: Any] = [
        .font: NSFont.systemFont(ofSize: 12, weight: .medium),
        .foregroundColor: NSColor.white.withAlphaComponent(0.94),
        .paragraphStyle: centered,
        .kern: 0.1,
    ]
    subtitle.draw(
        in: NSRect(x: 40, y: 306, width: 580, height: 20),
        withAttributes: subtitleAttributes
    )

    // 一道风，尾巴打个卷，吹向“应用程序”。Finder 把图标中心放在 y = 196（从上往下量）。
    let y: CGFloat = 400 - 196
    let wind = NSBezierPath()
    wind.move(to: NSPoint(x: 252, y: y + 15))
    wind.curve(to: NSPoint(x: 238, y: y + 8), controlPoint1: NSPoint(x: 248, y: y + 24), controlPoint2: NSPoint(x: 235, y: y + 20))
    wind.curve(to: NSPoint(x: 252, y: y), controlPoint1: NSPoint(x: 240, y: y + 2), controlPoint2: NSPoint(x: 246, y: y))
    wind.line(to: NSPoint(x: 352, y: y))
    wind.move(to: NSPoint(x: 340, y: y + 9))
    wind.line(to: NSPoint(x: 352, y: y))
    wind.line(to: NSPoint(x: 340, y: y - 9))
    wind.lineWidth = 3.5
    wind.lineCapStyle = .round
    wind.lineJoinStyle = .round
    NSColor.white.withAlphaComponent(0.92).setStroke()
    wind.stroke()
    NSGraphicsContext.restoreGraphicsState()

    guard let png = bitmap.representation(using: .png, properties: [:]) else {
        fputs("cannot encode DMG background\n", stderr)
        exit(1)
    }
    return png
}

let work = FileManager.default.temporaryDirectory
    .appendingPathComponent("moko-dmg-background-\(getpid())")
try FileManager.default.createDirectory(at: work, withIntermediateDirectories: true)
defer { try? FileManager.default.removeItem(at: work) }
let pngs = [1, 2].map { work.appendingPathComponent("background@\($0)x.png") }
try render(scale: 1).write(to: pngs[0])
try render(scale: 2).write(to: pngs[1])

let tiffutil = Process()
tiffutil.executableURL = URL(fileURLWithPath: "/usr/bin/tiffutil")
tiffutil.arguments = ["-cathidpicheck", pngs[0].path, pngs[1].path, "-out", outputURL.path]
try tiffutil.run()
tiffutil.waitUntilExit()
guard tiffutil.terminationStatus == 0 else {
    fputs("tiffutil failed\n", stderr)
    exit(1)
}
print("wrote \(outputURL.path) (\(Int(size.width))x\(Int(size.height)) @1x, @2x)")
