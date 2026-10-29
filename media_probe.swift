import Foundation
import AVFoundation
import ImageIO
import UniformTypeIdentifiers
let source=URL(fileURLWithPath:CommandLine.arguments[1])
let folder=URL(fileURLWithPath:CommandLine.arguments[2],isDirectory:true)
let asset=AVURLAsset(url:source)
let duration=CMTimeGetSeconds(asset.duration)
let generator=AVAssetImageGenerator(asset:asset)
generator.appliesPreferredTrackTransform=true
generator.maximumSize=CGSize(width:960,height:960)
let frame=try generator.copyCGImage(at:CMTime(seconds:min(0.1,duration/2),preferredTimescale:600),actualTime:nil)
let poster=folder.appendingPathComponent("poster.jpg")
let writer=CGImageDestinationCreateWithURL(poster as CFURL,UTType.jpeg.identifier as CFString,1,nil)!
CGImageDestinationAddImage(writer,frame,[kCGImageDestinationLossyCompressionQuality:0.85] as CFDictionary)
guard CGImageDestinationFinalize(writer) else { exit(2) }
let result:[String:Any] = ["duration_seconds":duration,"audio_tracks":asset.tracks(withMediaType:.audio).count,"poster":poster.path,"width":frame.width,"height":frame.height]
print(String(data:try JSONSerialization.data(withJSONObject:result),encoding:.utf8)!)
