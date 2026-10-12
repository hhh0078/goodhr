// 文件作用说明：使用 macOS 内置 Vision 识别本地图片，并按现有 OCR 逐行 JSON 协议返回文字。

import Foundation
import Vision

private struct OCRInput: Decodable {
    let image_path: String
}

private struct OCROutput: Encodable {
    let text: String
}

@main
struct VisionOCR {
    /// main 持续读取 Go 主程序发送的图片路径，每个请求只写回一行 JSON。
    static func main() {
        while let line = readLine(strippingNewline: true) {
            let text = autoreleasepool { recognizeLine(line) }
            writeResponse(text: text)
        }
    }

    /// recognizeLine 解析单行请求并调用系统文字识别，失败时返回空文字。
    private static func recognizeLine(_ line: String) -> String {
        guard let input = try? JSONDecoder().decode(OCRInput.self, from: Data(line.utf8)),
              input.image_path.hasPrefix("/") else {
            return ""
        }
        do {
            return try recognizeImage(at: URL(fileURLWithPath: input.image_path))
        } catch {
            fputs("Vision OCR 无法识别当前图片\n", stderr)
            return ""
        }
    }

    /// recognizeImage 用系统 Vision 识别简体中文和英文，并按页面阅读顺序拼接结果。
    private static func recognizeImage(at url: URL) throws -> String {
        let request = VNRecognizeTextRequest()
        request.recognitionLevel = .accurate
        request.recognitionLanguages = ["zh-Hans", "en-US"]
        request.usesLanguageCorrection = true
        try VNImageRequestHandler(url: url, options: [:]).perform([request])
        let observations = (request.results ?? []).sorted { left, right in
            if abs(left.boundingBox.midY - right.boundingBox.midY) < 0.02 {
                return left.boundingBox.minX < right.boundingBox.minX
            }
            return left.boundingBox.midY > right.boundingBox.midY
        }
        return observations.compactMap { $0.topCandidates(1).first?.string }.joined(separator: "\n")
    }

    /// writeResponse 将识别结果立即写入标准输出，避免常驻进程的缓冲阻塞 Go 请求。
    private static func writeResponse(text: String) {
        let payload = (try? JSONEncoder().encode(OCROutput(text: text))) ?? Data("{\"text\":\"\"}".utf8)
        FileHandle.standardOutput.write(payload + Data([0x0A]))
    }
}
