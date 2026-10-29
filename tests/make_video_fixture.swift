// QA only: synthetic colored clips to verify real HTML video playback.
import Foundation
import AVFoundation
import CoreVideo
import CoreGraphics

let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
for index in 1...3 {
    let url = root.appendingPathComponent("test-\(index).mp4")
    let writer = try AVAssetWriter(outputURL: url, fileType: .mp4)
    let input = AVAssetWriterInput(mediaType: .video, outputSettings: [AVVideoCodecKey:AVVideoCodecType.h264, AVVideoWidthKey:320, AVVideoHeightKey:180])
    input.expectsMediaDataInRealTime = false
    let adaptor = AVAssetWriterInputPixelBufferAdaptor(assetWriterInput: input, sourcePixelBufferAttributes: [kCVPixelBufferPixelFormatTypeKey as String:kCVPixelFormatType_32ARGB, kCVPixelBufferWidthKey as String:320, kCVPixelBufferHeightKey as String:180])
    writer.add(input)
    guard writer.startWriting() else { fatalError("Could not start fixture encoder") }
    writer.startSession(atSourceTime: .zero)
    for frame in 0..<24 {
        while !input.isReadyForMoreMediaData { Thread.sleep(forTimeInterval:0.005) }
        var optional: CVPixelBuffer?
        CVPixelBufferCreate(kCFAllocatorDefault, 320, 180, kCVPixelFormatType_32ARGB, [kCVPixelBufferCGImageCompatibilityKey:true, kCVPixelBufferCGBitmapContextCompatibilityKey:true] as CFDictionary, &optional)
        guard let buffer = optional else { fatalError("No pixel buffer") }
        CVPixelBufferLockBaseAddress(buffer, [])
        let context = CGContext(data:CVPixelBufferGetBaseAddress(buffer), width:320, height:180, bitsPerComponent:8, bytesPerRow:CVPixelBufferGetBytesPerRow(buffer), space:CGColorSpaceCreateDeviceRGB(), bitmapInfo:CGImageAlphaInfo.noneSkipFirst.rawValue)!
        context.setFillColor(red: CGFloat(index == 1 ? 0.7 : 0.1), green:CGFloat(index == 2 ? 0.6 : 0.2), blue:CGFloat(index == 3 ? 0.7 : 0.15), alpha:1)
        context.fill(CGRect(x:0,y:0,width:320,height:180))
        context.setFillColor(red:1,green:1,blue:1,alpha:1)
        context.fill(CGRect(x:frame*10,y:75,width:30,height:30))
        CVPixelBufferUnlockBaseAddress(buffer, [])
        guard adaptor.append(buffer, withPresentationTime:CMTime(value:Int64(frame),timescale:12)) else { fatalError("Could not append fixture frame") }
    }
    input.markAsFinished()
    let done = DispatchSemaphore(value:0)
    writer.finishWriting { done.signal() }
    done.wait()
    guard writer.status == .completed else { fatalError("Fixture did not finish") }
    print(url.path)
}
