import SwiftUI

// RingSectorShape is adapted from OpenUsage by Robin Ebers.
// MIT License, Copyright (c) 2026 Robin Ebers.
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

struct CostRingArc: Equatable, Identifiable, Sendable {
    let id: String
    let accountNumber: Int
    let startFraction: Double
    let endFraction: Double
}

enum CostRingArcLayout {
    static func arcs(from shares: [UsagePresentation.Share]) -> [CostRingArc] {
        let positive = shares.compactMap { share -> (UsagePresentation.Share, Double)? in
            let width = share.endFraction - share.startFraction
            return width > 0 ? (share, width) : nil
        }
        let total = positive.reduce(0) { $0 + $1.1 }
        guard total > 0 else { return [] }

        var cursor = 0.0
        return positive.map { entry in
            let width = entry.1 / total
            let start = cursor
            cursor += width
            return CostRingArc(
                id: entry.0.id,
                accountNumber: entry.0.accountNumber,
                startFraction: start,
                endFraction: cursor
            )
        }
    }
}

/// A filled annular sector with hairline gaps and rounded corners.
/// Fractions run clockwise from noon.
struct RingSectorShape: Shape {
    var startFraction: Double
    var endFraction: Double
    var innerRadiusRatio: CGFloat = 0.618
    var gapWidth: CGFloat = 1.6
    var cornerRadius: CGFloat = 3

    var animatableData: AnimatablePair<Double, Double> {
        get { AnimatablePair(startFraction, endFraction) }
        set {
            startFraction = newValue.first
            endFraction = newValue.second
        }
    }

    func path(in rect: CGRect) -> Path {
        let outer = Double(min(rect.width, rect.height) / 2)
        let inner = outer * Double(innerRadiusRatio)
        let centre = CGPoint(x: rect.midX, y: rect.midY)
        let top = -Double.pi / 2
        let halfGap = Double(gapWidth) / outer / 2
        let start = top + startFraction * 2 * .pi + halfGap
        let end = top + endFraction * 2 * .pi - halfGap
        let width = end - start
        guard width > 0.001 else { return Path() }

        let sine = sin(min(width / 2, .pi / 2))
        var corner = min(Double(cornerRadius), (outer - inner) / 2)
        corner = min(corner, outer * sine / (1 + sine))
        if sine < 1 {
            corner = min(corner, inner * sine / (1 - sine))
        }
        guard corner >= 0.25 else {
            return plainWedge(centre: centre, inner: inner, outer: outer, start: start, end: end)
        }
        return roundedWedge(
            centre: centre,
            inner: inner,
            outer: outer,
            start: start,
            end: end,
            corner: corner
        )
    }

    private func plainWedge(
        centre: CGPoint,
        inner: Double,
        outer: Double,
        start: Double,
        end: Double
    ) -> Path {
        var path = Path()
        path.addArc(center: centre, radius: outer, startAngle: .radians(start), endAngle: .radians(end), clockwise: false)
        path.addArc(center: centre, radius: inner, startAngle: .radians(end), endAngle: .radians(start), clockwise: true)
        path.closeSubpath()
        return path
    }

    private func roundedWedge(
        centre: CGPoint,
        inner: Double,
        outer: Double,
        start: Double,
        end: Double,
        corner: Double
    ) -> Path {
        let outerOffset = asin(min(1, corner / (outer - corner)))
        let innerOffset = asin(min(1, corner / (inner + corner)))

        func polar(_ radius: Double, _ angle: Double) -> CGPoint {
            CGPoint(x: centre.x + radius * cos(angle), y: centre.y + radius * sin(angle))
        }
        func around(_ point: CGPoint, _ radius: Double, _ angle: Double) -> CGPoint {
            CGPoint(x: point.x + radius * cos(angle), y: point.y + radius * sin(angle))
        }

        var path = Path()
        path.addArc(
            center: centre,
            radius: outer,
            startAngle: .radians(start + outerOffset),
            endAngle: .radians(end - outerOffset),
            clockwise: false
        )
        let trailingOuter = polar(outer - corner, end - outerOffset)
        path.addArc(
            center: trailingOuter,
            radius: corner,
            startAngle: .radians(end - outerOffset),
            endAngle: .radians(end + .pi / 2),
            clockwise: false
        )
        let trailingInner = polar(inner + corner, end - innerOffset)
        path.addLine(to: around(trailingInner, corner, end + .pi / 2))
        path.addArc(
            center: trailingInner,
            radius: corner,
            startAngle: .radians(end + .pi / 2),
            endAngle: .radians(end - innerOffset + .pi),
            clockwise: false
        )
        path.addArc(
            center: centre,
            radius: inner,
            startAngle: .radians(end - innerOffset),
            endAngle: .radians(start + innerOffset),
            clockwise: true
        )
        let leadingInner = polar(inner + corner, start + innerOffset)
        path.addArc(
            center: leadingInner,
            radius: corner,
            startAngle: .radians(start + innerOffset + .pi),
            endAngle: .radians(start + 3 * .pi / 2),
            clockwise: false
        )
        let leadingOuter = polar(outer - corner, start + outerOffset)
        path.addLine(to: around(leadingOuter, corner, start - .pi / 2))
        path.addArc(
            center: leadingOuter,
            radius: corner,
            startAngle: .radians(start + 3 * .pi / 2),
            endAngle: .radians(start + outerOffset),
            clockwise: false
        )
        path.closeSubpath()
        return path
    }
}
