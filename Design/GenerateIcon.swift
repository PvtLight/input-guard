import AppKit

@main enum GenerateIcon {
    static func main() throws {
        let folder = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        for base in [16, 32, 128, 256, 512] {
            for density in [1, 2] {
                let pixels = base * density
                guard let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
                    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0),
                    let context = NSGraphicsContext(bitmapImageRep: bitmap) else { fatalError("Icon canvas unavailable") }
                NSGraphicsContext.saveGraphicsState()
                NSGraphicsContext.current = context
                Brand.icon(size: CGFloat(pixels)).draw(in: NSRect(x: 0, y: 0, width: pixels, height: pixels))
                NSGraphicsContext.restoreGraphicsState()
                let suffix = density == 2 ? "@2x" : ""
                guard let png = bitmap.representation(using: .png, properties: [:]) else { fatalError("Icon encoding failed") }
                try png.write(to: folder.appendingPathComponent("icon_\(base)x\(base)\(suffix).png"))
            }
        }
    }
}
