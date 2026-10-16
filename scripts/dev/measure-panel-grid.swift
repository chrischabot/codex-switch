// Measures the account-row grid on a light panel render: for every bar row,
// the left and right edges of the bar track, the percent text, the flame and
// the time text, and the pitch between rows. Every edge should repeat row to
// row, and the pitch should be one number.
//
//   swift scripts/dev/measure-panel-grid.swift RENDER.png
//
// Written by AI on 2026.10.17 by model Claude Fable 5.1.
import AppKit

guard CommandLine.arguments.count == 2,
      let image = NSImage(contentsOfFile: CommandLine.arguments[1]),
      let tiff = image.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiff) else {
    FileHandle.standardError.write(Data("usage: measure-panel-grid RENDER.png\n".utf8))
    exit(2)
}

let width = bitmap.pixelsWide
let height = bitmap.pixelsHigh
let scale = CGFloat(width) / image.size.width

enum Ink { case bar, track, dark, red, other }

func ink(_ x: Int, _ y: Int) -> Ink {
    guard let colour = bitmap.colorAt(x: x, y: y) else { return .other }
    let r = colour.redComponent, g = colour.greenComponent, b = colour.blueComponent
    if b > 0.8 && r < 0.3 && g < 0.6 { return .bar }               // system blue fill
    if r > 0.8 && g < 0.4 && b < 0.4 { return .red }               // system red fill or flame
    let luminance = 0.299 * r + 0.587 * g + 0.114 * b
    if luminance < 0.55 { return .dark }                           // text
    if luminance < 0.92 && abs(r - g) < 0.03 && abs(g - b) < 0.03 { return .track }
    return .other
}

/// Horizontal runs of a given ink on one pixel row, as [start, end] in points.
func runs(y: Int, matching: (Ink) -> Bool, minimumLength: Int = 2) -> [(Int, Int)] {
    var found: [(Int, Int)] = []
    var start: Int?
    for x in 0..<width {
        if matching(ink(x, y)) {
            if start == nil { start = x }
        } else if let s = start {
            if x - s >= minimumLength { found.append((s, x - 1)) }
            start = nil
        }
    }
    return found
}

struct BarRow {
    let centreY: Int
    let barLeft: Int, barRight: Int
    let percentLeft: Int, percentRight: Int
    let flameLeft: Int?, flameRight: Int?
    let timeLeft: Int, timeRight: Int
}

/// Runs of bar ink with anti-aliased gaps of up to three pixels closed.
func barRuns(y: Int) -> [(Int, Int)] {
    var merged: [(Int, Int)] = []
    for run in runs(y: y, matching: { $0 == .bar || $0 == .track || $0 == .red }, minimumLength: 1) {
        if let last = merged.last, run.0 <= last.1 + 4 {
            merged[merged.count - 1] = (last.0, run.1)
        } else {
            merged.append(run)
        }
    }
    return merged.filter { $0.1 - $0.0 >= 60 }
}

/// The bar on a pixel row: a long run that starts where the bar column does.
func bar(onLine y: Int) -> (Int, Int)? {
    barRuns(y: y).first { CGFloat($0.0) / scale > 40 && CGFloat($0.0) / scale < 70 }
}

var rows: [BarRow] = []
var y = 0
while y < height {
    guard let first = bar(onLine: y) else { y += 1; continue }
    var bottom = y
    while bottom + 1 < height, bar(onLine: bottom + 1) != nil { bottom += 1 }
    let centre = (y + bottom) / 2
    let track = bar(onLine: centre) ?? first
    // Text to the right of the bar sits on the lines around the bar's centre (text is taller than the bar).
    var groups: [(Int, Int, Bool)] = []   // (left, right, isRed)
    for line in max(0, centre - 7 * Int(scale))...min(height - 1, centre + 7 * Int(scale)) {
        for run in runs(y: line, matching: { $0 == .dark || $0 == .red }, minimumLength: 1) where run.0 > track.1 + 4 {
            groups.append((run.0, run.1, ink(run.0, line) == .red))
        }
    }
    groups.sort { $0.0 < $1.0 }
    var merged: [(Int, Int, Bool)] = []
    for group in groups {
        if let last = merged.last, group.0 <= last.1 + Int(5 * scale) {
            merged[merged.count - 1] = (last.0, max(last.1, group.1), last.2 || group.2)
        } else {
            merged.append(group)
        }
    }
    if merged.count >= 2 {
        let percent = merged[0]
        let time = merged[merged.count - 1]
        let flame = merged.count >= 3 ? merged[1] : nil
        rows.append(BarRow(centreY: centre, barLeft: track.0, barRight: track.1,
                           percentLeft: percent.0, percentRight: percent.1,
                           flameLeft: flame?.0, flameRight: flame?.1,
                           timeLeft: time.0, timeRight: time.1))
    } else {
        rows.append(BarRow(centreY: centre, barLeft: track.0, barRight: track.1, percentLeft: 0, percentRight: 0,
                           flameLeft: nil, flameRight: nil, timeLeft: 0, timeRight: 0))
    }
    y = bottom + 1
}

func pt(_ pixels: Int) -> String { String(format: "%.1f", CGFloat(pixels) / scale) }

print("row  centre  bar L..R   %% L..R    flame L..R   time L..R")
for (index, row) in rows.enumerated() {
    let flame = row.flameLeft.map { "\(pt($0))..\(pt(row.flameRight!))" } ?? "-"
    print(String(format: "%-4d %-7@ %@..%@   %@..%@   %-12@ %@..%@", index + 1, pt(row.centreY) as NSString,
                 pt(row.barLeft) as NSString, pt(row.barRight) as NSString,
                 pt(row.percentLeft) as NSString, pt(row.percentRight) as NSString,
                 flame as NSString, pt(row.timeLeft) as NSString, pt(row.timeRight) as NSString))
}
let pitches = zip(rows, rows.dropFirst()).map { CGFloat($1.centreY - $0.centreY) / scale }
print("")
print("bar left edges:    " + Set(rows.map { pt($0.barLeft) }).sorted().joined(separator: ", "))
print("bar right edges:   " + Set(rows.map { pt($0.barRight) }).sorted().joined(separator: ", "))
print("% right edges:     " + Set(rows.filter { $0.percentRight > 0 }.map { pt($0.percentRight) }).sorted().joined(separator: ", "))
print("time right edges:  " + Set(rows.filter { $0.timeRight > 0 }.map { pt($0.timeRight) }).sorted().joined(separator: ", "))
print("row pitches (pt):  " + pitches.map { String(format: "%.1f", $0) }.joined(separator: ", "))
