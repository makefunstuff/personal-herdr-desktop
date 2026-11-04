// herdr-keyclick (macOS): key sounds for Herdr Desktop, the Linux herdr-keyclick's Mac side.
// Cherry MX Blue recordings (Mechvibes' "CherryMX Blue - PBT keycaps" pack, MIT) from
// ~/.local/share/herdr-typing/mxblue: a press and its release ~85 ms later, highs rolled off (two-pole low-pass,
// HERDR_KEYS_CUTOFF=<Hz>, 0 for the raw recording). Space, Enter and Backspace have their own; other keys a random
// letter. Only while Herdr (dev.herdr.app) gets the keys. Mute: touch ~/.local/state/herdr-typing-muted
// (herdr-typing-toggle). Listens to key presses with a listen-only event tap, so macOS asks once for Input
// Monitoring (System Settings > Privacy & Security > Input Monitoring: herdr-keyclick). Runs as a LaunchAgent.
//
//   swiftc -O -o ~/.local/bin/herdr-keyclick herdr-keyclick.swift
import AVFoundation
import AppKit
import CoreGraphics
import Foundation

let RATE = 44100.0
let home = FileManager.default.homeDirectoryForCurrentUser.path
let SOUNDS = "\(home)/.local/share/herdr-typing/mxblue"
let MUTED = "\(home)/.local/state/herdr-typing-muted"
let CUTOFF = Double(ProcessInfo.processInfo.environment["HERDR_KEYS_CUTOFF"] ?? "") ?? 2800
let HERDR = "dev.herdr.app"

func log(_ s: String) { FileHandle.standardError.write((s + "\n").data(using: .utf8)!) }

func samples(_ name: String) -> [Float] {
  guard let f = try? AVAudioFile(forReading: URL(fileURLWithPath: "\(SOUNDS)/\(name).wav")),
        let b = AVAudioPCMBuffer(pcmFormat: f.processingFormat, frameCapacity: AVAudioFrameCount(f.length)),
        (try? f.read(into: b)) != nil, let ch = b.floatChannelData else { return [] }
  return Array(UnsafeBufferPointer(start: ch[0], count: Int(b.frameLength)))
}

func lowpass(_ x: [Float], _ hz: Double, gain: Float = 1.15) -> [Float] {
  if hz <= 0 { return x }
  let a = Float(1 - exp(-2 * Double.pi * hz / RATE))
  var y1: Float = 0, y2: Float = 0
  return x.map { v in y1 += a * (v - y1); y2 += a * (y1 - y2); return max(-1, min(1, y2 * gain)) }
}

// A press and its release ~85 ms later, as the Linux daemon mixes them.
func mx(_ key: String) -> [Float] {
  let press = samples("\(key)-0"), rel = samples("\(key)-1")
  if press.isEmpty { return [] }
  let off = max(press.count, Int(RATE * 0.085))
  var out = [Float](repeating: 0, count: off + rel.count)
  for (i, v) in press.enumerated() { out[i] = v * 0.8 }
  for (i, v) in rel.enumerated() { out[off + i] = max(-1, min(1, out[off + i] + v * 0.55)) }
  return lowpass(out, CUTOFF)
}

let format = AVAudioFormat(standardFormatWithSampleRate: RATE, channels: 1)!
func buffer(_ s: [Float]) -> AVAudioPCMBuffer? {
  guard !s.isEmpty, let b = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(s.count)) else { return nil }
  b.frameLength = AVAudioFrameCount(s.count)
  s.withUnsafeBufferPointer { b.floatChannelData![0].update(from: $0.baseAddress!, count: s.count) }
  return b
}

let letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ".map { buffer(mx("Key\($0)")) }.compactMap { $0 }
let special: [Int64: AVAudioPCMBuffer] = [49: "Space", 36: "Enter", 76: "Enter", 51: "Backspace"]
  .compactMapValues { buffer(mx($0)) }
if letters.isEmpty { log("herdr-keyclick: no sounds in \(SOUNDS)"); exit(1) }

// Several voices, so fast typing overlaps like the real thing.
let engine = AVAudioEngine()
let voices = (0..<8).map { _ in AVAudioPlayerNode() }
for v in voices { engine.attach(v); engine.connect(v, to: engine.mainMixerNode, format: format) }
var next = 0
func startEngine() { if !engine.isRunning { do { try engine.start(); voices.forEach { $0.play() } } catch { log("herdr-keyclick: audio: \(error)") } } }
startEngine()
NotificationCenter.default.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine, queue: .main) { _ in startEngine() }

// Herdr's processes, refreshed every few seconds: a key counts when it goes to one of them.
var herdrPids = Set<pid_t>()
func refreshPids() { herdrPids = Set(NSRunningApplication.runningApplications(withBundleIdentifier: HERDR).map { $0.processIdentifier }) }
refreshPids()
Timer.scheduledTimer(withTimeInterval: 3, repeats: true) { _ in refreshPids() }

func click(_ code: Int64) {
  if access(MUTED, F_OK) == 0 { return }
  guard let b = special[code] ?? letters.randomElement() else { return }
  startEngine()
  let v = voices[next]; next = (next + 1) % voices.count
  v.scheduleBuffer(b, at: nil, options: [], completionHandler: nil)
  if !v.isPlaying { v.play() }
}

var tap: CFMachPort?
let callback: CGEventTapCallBack = { _, type, event, _ in
  if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
    if let t = tap { CGEvent.tapEnable(tap: t, enable: true) }
  } else if type == .keyDown, event.getIntegerValueField(.keyboardEventAutorepeat) == 0 {
    let pid = pid_t(event.getIntegerValueField(.eventTargetUnixProcessID))
    let front = NSWorkspace.shared.frontmostApplication?.bundleIdentifier
    if herdrPids.contains(pid) || front == HERDR { click(event.getIntegerValueField(.keyboardEventKeycode)) }
  }
  return Unmanaged.passUnretained(event)
}

// Without Input Monitoring the tap can't be made: ask once (macOS shows its prompt), then try again every 5 s.
func makeTap() -> Bool {
  guard let t = CGEvent.tapCreate(tap: .cgSessionEventTap, place: .headInsertEventTap, options: .listenOnly,
                                  eventsOfInterest: CGEventMask(1 << CGEventType.keyDown.rawValue), callback: callback, userInfo: nil)
  else { return false }
  tap = t
  CFRunLoopAddSource(CFRunLoopGetMain(), CFMachPortCreateRunLoopSource(nil, t, 0), .commonModes)
  CGEvent.tapEnable(tap: t, enable: true)
  log("herdr-keyclick: listening")
  return true
}
if !makeTap() {
  log("herdr-keyclick: needs Input Monitoring (System Settings > Privacy & Security > Input Monitoring: herdr-keyclick)")
  if !CGPreflightListenEventAccess() { _ = CGRequestListenEventAccess() }
  Timer.scheduledTimer(withTimeInterval: 5, repeats: true) { t in if makeTap() { t.invalidate() } }
}
RunLoop.main.run()
