// herdr-timetrack (macOS): where the person's time goes, sampled every 30 s, kept on this machine only; the
// Linux herdr-timetrack's Mac side, in the same format for the Time panel (mods/px-time).
//
// Each sample: the frontmost app and its focused window's title, and when it is Herdr, the space and tab shown;
// idle (no input for 5 min) and locked are marked. One JSON line per sample in
// ~/.local/share/herdr-timetrack/<YYYY-MM-DD>.jsonl. Window titles come through Accessibility, which macOS asks
// for once (System Settings > Privacy & Security > Accessibility: herdr-timetrack); without it the app is
// still recorded, with an empty title. Runs as a LaunchAgent.
//
//   swiftc -O -o ~/.local/bin/herdr-timetrack herdr-timetrack.swift
import AppKit
import ApplicationServices
import CoreGraphics
import Foundation

let EVERY = 30.0, IDLE_AFTER = 300.0
let home = FileManager.default.homeDirectoryForCurrentUser.path
let DIR = "\(home)/.local/share/herdr-timetrack"
let HERDR_APP = ["\(home)/.local/bin/herdr-app", "/opt/homebrew/bin/herdr-app", "/usr/local/bin/herdr-app"]
  .first { FileManager.default.isExecutableFile(atPath: $0) }

func run(_ path: String, _ args: [String]) -> Any? {
  let p = Process(), out = Pipe()
  p.executableURL = URL(fileURLWithPath: path); p.arguments = args
  p.standardOutput = out; p.standardError = FileHandle.nullDevice
  guard (try? p.run()) != nil else { return nil }
  let data = out.fileHandleForReading.readDataToEndOfFile()
  p.waitUntilExit()
  return p.terminationStatus == 0 ? try? JSONSerialization.jsonObject(with: data) : nil
}

// space id -> name, tab id -> name (asked every few minutes).
var spaceNames: [String: String] = [:], tabNames: [String: String] = [:], namesAt = 0.0
func herdrNames() {
  guard let h = HERDR_APP, let d = run(h, ["ls"]) as? [String: Any] else { return }
  spaceNames = [:]; tabNames = [:]
  for s in d["spaces"] as? [[String: Any]] ?? [] {
    if let id = s["id"] as? String { spaceNames[id] = s["name"] as? String }
    for t in s["tabs"] as? [[String: Any]] ?? [] { if let id = t["id"] as? String { tabNames[id] = t["name"] as? String } }
  }
}

func windowTitle(_ pid: pid_t) -> String {
  let app = AXUIElementCreateApplication(pid)
  var win: CFTypeRef?
  guard AXUIElementCopyAttributeValue(app, kAXFocusedWindowAttribute as CFString, &win) == .success, let w = win else { return "" }
  var title: CFTypeRef?
  guard AXUIElementCopyAttributeValue(w as! AXUIElement, kAXTitleAttribute as CFString, &title) == .success else { return "" }
  return String((title as? String ?? "").prefix(200))
}

func locked() -> Bool {
  guard let d = CGSessionCopyCurrentDictionary() as? [String: Any] else { return false }
  return (d["CGSSessionScreenIsLocked"] as? Bool) ?? ((d["CGSSessionScreenIsLocked"] as? Int) == 1)
}

// Ask for Accessibility once (the prompt shows only while it isn't granted).
_ = AXIsProcessTrustedWithOptions([kAXTrustedCheckOptionPrompt.takeUnretainedValue() as String: true] as CFDictionary)
try? FileManager.default.createDirectory(atPath: DIR, withIntermediateDirectories: true)
let dayFormat = DateFormatter(); dayFormat.dateFormat = "yyyy-MM-dd"

func sample() {
  let t0 = Date().timeIntervalSince1970
  let front = NSWorkspace.shared.frontmostApplication
  let isHerdr = front?.bundleIdentifier == "dev.herdr.app"
  // The names the Time panel knows: Herdr as on Linux, other apps by their lowercased name (firefox, slack...).
  let app = isHerdr ? "herdr-app" : (front?.localizedName ?? "").lowercased()
  var rec: [String: Any] = ["t": Int(t0), "app": app, "title": front.map { windowTitle($0.processIdentifier) } ?? ""]
  if isHerdr, let h = HERDR_APP {
    if t0 - namesAt > 180 { herdrNames(); namesAt = t0 }
    let st = run(h, ["status"]) as? [String: Any] ?? [:]
    if let s = st["space"] as? String { rec["space"] = spaceNames[s] ?? s }
    if let t = st["tab"] as? String { rec["tab"] = tabNames[t] ?? t }
    if let l = st["layout"] { rec["layout"] = l }
  }
  let idle = CGEventSource.secondsSinceLastEventType(.combinedSessionState, eventType: CGEventType(rawValue: ~0)!)
  if locked() { rec["locked"] = true } else if idle >= IDLE_AFTER { rec["idle"] = true }
  guard let line = try? JSONSerialization.data(withJSONObject: rec, options: [.sortedKeys]) else { return }
  let path = "\(DIR)/\(dayFormat.string(from: Date(timeIntervalSince1970: t0))).jsonl"
  if !FileManager.default.fileExists(atPath: path) { FileManager.default.createFile(atPath: path, contents: nil) }
  if let f = FileHandle(forWritingAtPath: path) { f.seekToEndOfFile(); f.write(line + "\n".data(using: .utf8)!); f.closeFile() }
}

sample()
Timer.scheduledTimer(withTimeInterval: EVERY, repeats: true) { _ in sample() }
RunLoop.main.run()
