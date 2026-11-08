import AppKit

// Shared vector artwork for the app icon, settings, and working notice.
enum Brand {
    static let canvas = NSColor(name: nil) { appearance in
        NSColor(calibratedWhite: appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua ? 0.10 : 0.955, alpha: 1)
    }
    static let cardBackground = NSColor(name: nil) { appearance in
        appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
            ? NSColor(calibratedWhite: 0.16, alpha: 1) : .white
    }

    static func icon(size: CGFloat) -> NSImage {
        NSImage(size: NSSize(width: size, height: size), flipped: false) { rect in
            let scale = rect.width / 100
            let transform = NSAffineTransform()
            transform.scale(by: scale)
            transform.concat()
            let tile = NSBezierPath(roundedRect: NSRect(x: 5, y: 5, width: 90, height: 90), xRadius: 22, yRadius: 22)
            NSColor(calibratedRed: 0.08, green: 0.22, blue: 0.20, alpha: 1).setFill()
            tile.fill()
            // A pause mark inside a soft shield.
            let shield = NSBezierPath()
            shield.move(to: NSPoint(x: 26, y: 70))
            shield.curve(to: NSPoint(x: 50, y: 80), controlPoint1: NSPoint(x: 34, y: 72), controlPoint2: NSPoint(x: 43, y: 77))
            shield.curve(to: NSPoint(x: 74, y: 70), controlPoint1: NSPoint(x: 57, y: 77), controlPoint2: NSPoint(x: 66, y: 72))
            shield.line(to: NSPoint(x: 74, y: 51))
            shield.curve(to: NSPoint(x: 50, y: 22), controlPoint1: NSPoint(x: 74, y: 37), controlPoint2: NSPoint(x: 63, y: 27))
            shield.curve(to: NSPoint(x: 26, y: 51), controlPoint1: NSPoint(x: 37, y: 27), controlPoint2: NSPoint(x: 26, y: 37))
            shield.close()
            shield.lineWidth = 4.5
            shield.lineJoinStyle = .round
            NSColor(calibratedRed: 0.53, green: 0.83, blue: 0.71, alpha: 1).setStroke()
            shield.stroke()
            NSColor(calibratedRed: 0.87, green: 0.96, blue: 0.91, alpha: 1).setFill()
            for x: CGFloat in [39, 54] {
                NSBezierPath(roundedRect: NSRect(x: x, y: 41, width: 7, height: 22), xRadius: 3.5, yRadius: 3.5).fill()
            }
            return true
        }
    }

    static func imageView(size: CGFloat) -> NSImageView {
        let view = NSImageView(image: icon(size: size))
        view.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([view.widthAnchor.constraint(equalToConstant: size), view.heightAnchor.constraint(equalToConstant: size)])
        view.setAccessibilityLabel("Input Guard")
        return view
    }
}
