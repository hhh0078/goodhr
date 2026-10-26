// 文件作用：仅覆盖已验证隔离 Camoufox 的可见窗口，使用独立 AppKit 浮动窗制造真实遮挡；不激活应用、不截屏、不读取窗口标题或页面。
import AppKit
import CoreGraphics
import Foundation

/** fail 输出结构化错误后退出；错误发生时不创建窗口。 */
func fail(_ message: String) -> Never {
    emit(["ready": false, "error_message": message])
    exit(1)
}

/** emit 输出一行 JSON，供父级 Node 辅助确认夹具已准备好。 */
func emit(_ value: [String: Any]) {
    guard let data = try? JSONSerialization.data(withJSONObject: value, options: [.sortedKeys]) else { exit(1) }
    FileHandle.standardOutput.write(data)
    FileHandle.standardOutput.write(Data([10]))
}

/** isolatedBrowserPath 再次校验目标 PID 对应的真实可执行路径，拒绝用户正式浏览器。 */
func isolatedBrowserPath(_ pid: Int32) -> String {
    let process = Process()
    let pipe = Pipe()
    process.executableURL = URL(fileURLWithPath: "/bin/ps")
    process.arguments = ["-p", String(pid), "-o", "comm="]
    process.standardOutput = pipe
    process.standardError = FileHandle.nullDevice
    do { try process.run() } catch { fail("无法再次核对测试浏览器 PID") }
    let data = pipe.fileHandleForReading.readDataToEndOfFile()
    process.waitUntilExit()
    let command = String(data: data, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
    let real = URL(fileURLWithPath: command).resolvingSymlinksInPath().path
    let normalized = real.lowercased()
    guard process.terminationStatus == 0, command.hasPrefix("/"), normalized.contains("/camoufox-upgrade/"),
          !normalized.contains("/library/application support/goodhr/"),
          normalized.hasSuffix("/camoufox.app/contents/macos/camoufox") else { fail("目标不是 camoufox-upgrade 内的隔离 Camoufox，未创建遮挡窗") }
    return real
}

/** largestVisibleBounds 仅按 PID、层级和边界选择最大可见主窗口；不访问标题字段。 */
func largestVisibleBounds(_ pid: Int32) -> CGRect {
    guard let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] else {
        fail("无法读取测试浏览器的公开窗口边界")
    }
    let candidates = windows.compactMap { window -> CGRect? in
        guard (window[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == pid,
              (window[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
              (window[kCGWindowAlpha as String] as? NSNumber)?.doubleValue ?? 0 > 0,
              let dictionary = window[kCGWindowBounds as String] as? [String: Any],
              let bounds = CGRect(dictionaryRepresentation: dictionary as CFDictionary),
              bounds.width.isFinite, bounds.height.isFinite, bounds.width > 100, bounds.height > 100 else { return nil }
        return bounds
    }
    guard let largest = candidates.max(by: { $0.width * $0.height < $1.width * $1.height }) else {
        fail("指定测试浏览器没有可见的主窗口，未创建遮挡窗")
    }
    return largest
}

/** appKitBounds 将 CoreGraphics 顶部原点边界转换为 AppKit 全局坐标，兼容多显示器负坐标。 */
func appKitBounds(_ bounds: CGRect, mainHeight: CGFloat) -> NSRect {
    NSRect(x: bounds.minX, y: mainHeight - bounds.maxY, width: bounds.width, height: bounds.height)
}

/** coverIsOnScreen 只核对本夹具窗口编号及 PID，确认自己的遮挡窗已进入可见窗口列表。 */
func coverIsOnScreen(_ number: Int) -> Bool {
    let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []
    return windows.contains { window in
        (window[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == ProcessInfo.processInfo.processIdentifier &&
        (window[kCGWindowNumber as String] as? NSNumber)?.intValue == number
    }
}

/** CoverWindow 永不成为键盘或主窗口，只提供不拦截鼠标的真实遮挡。 */
final class CoverWindow: NSWindow {
    /** canBecomeKey 禁止夹具抢占键盘焦点。 */
    override var canBecomeKey: Bool { false }
    /** canBecomeMain 禁止夹具成为应用主窗口。 */
    override var canBecomeMain: Bool { false }
}

/** main 读取经过 Node 核对的 PID，创建自然色遮挡窗，并在指定秒数后自行关闭。 */
func main() {
    let args = Array(CommandLine.arguments.dropFirst())
    if args == ["--self-test"] {
        let converted = appKitBounds(CGRect(x: -120, y: 80, width: 1000, height: 700), mainHeight: 900)
        guard converted == NSRect(x: -120, y: 120, width: 1000, height: 700) else { fail("窗口坐标转换自检失败") }
        emit(["ready": false, "self_test": "passed", "window_operations": 0])
        return
    }
    guard args.count == 4, args[0] == "--browser-pid", let pid = Int32(args[1]), pid > 0,
          args[2] == "--seconds", let seconds = Int(args[3]), seconds > 0, seconds <= 300 else {
        fail("参数必须为 --browser-pid 正整数 --seconds 1到300")
    }
    let binary = isolatedBrowserPath(pid)
    let target = largestVisibleBounds(pid)
    let app = NSApplication.shared
    app.setActivationPolicy(.accessory)
    let window = CoverWindow(contentRect: appKitBounds(target, mainHeight: CGDisplayBounds(CGMainDisplayID()).height),
                             styleMask: [.borderless], backing: .buffered, defer: false)
    window.level = .floating
    window.isOpaque = true
    window.alphaValue = 1
    window.backgroundColor = NSColor(calibratedRed: 0.96, green: 0.94, blue: 0.87, alpha: 1)
    window.ignoresMouseEvents = true
    window.hidesOnDeactivate = false
    window.isReleasedWhenClosed = false
    window.collectionBehavior = [.fullScreenAuxiliary, .ignoresCycle]
    let label = NSTextField(labelWithString: "GoodHR 遮挡测试，\(seconds) 秒后关闭")
    label.alignment = .center
    label.textColor = NSColor(calibratedWhite: 0.23, alpha: 1)
    label.font = NSFont.systemFont(ofSize: 20, weight: .medium)
    label.frame = NSRect(x: 20, y: max(20, target.height / 2 - 20), width: max(100, target.width - 40), height: 45)
    window.contentView?.addSubview(label)
    app.finishLaunching()
    window.orderFrontRegardless()
    DispatchQueue.main.asyncAfter(deadline: .now() + 0.4) {
        guard window.isVisible && coverIsOnScreen(window.windowNumber) else { fail("遮挡窗没有确认显示，不能当作遮挡复现证据") }
        emit(["ready": true, "covered": true, "cover_pid": ProcessInfo.processInfo.processIdentifier, "browser_pid": pid,
              "browser_binary": binary, "seconds": seconds, "window_number": window.windowNumber,
              "ignores_mouse_events": true, "activates_application": false,
              "target_bounds": ["x": target.minX, "y": target.minY, "width": target.width, "height": target.height]])
        FileHandle.standardOutput.closeFile()
    }
    DispatchQueue.main.asyncAfter(deadline: .now() + .seconds(seconds)) {
        window.orderOut(nil)
        window.close()
        app.terminate(nil)
    }
    app.run()
}

main()
