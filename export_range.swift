import Foundation
import AVFoundation
let args=CommandLine.arguments
func fail(_ message:String)->Never { fputs(message+"\n",stderr);exit(1) }
guard args.count==5,let start=Double(args[3]),let end=Double(args[4]),start>=0,end>start else {fail("Invalid clip range")}
let asset=AVURLAsset(url:URL(fileURLWithPath:args[1]))
guard end<=CMTimeGetSeconds(asset.duration)+0.001,let source=asset.tracks(withMediaType:.video).first else {fail("Source video is unavailable")}
let composition=AVMutableComposition()
guard let track=composition.addMutableTrack(withMediaType:.video,preferredTrackID:kCMPersistentTrackID_Invalid) else {fail("Could not create video track")}
do {try track.insertTimeRange(CMTimeRange(start:CMTime(seconds:start,preferredTimescale:60000),duration:CMTime(seconds:end-start,preferredTimescale:60000)),of:source,at:.zero)}catch {fail("Could not read clip range")}
track.preferredTransform=source.preferredTransform
let target=URL(fileURLWithPath:args[2])
guard !FileManager.default.fileExists(atPath:target.path),let exporter=AVAssetExportSession(asset:composition,presetName:AVAssetExportPresetHighestQuality) else {fail("MP4 exporter unavailable")}
exporter.outputURL=target;exporter.outputFileType = .mp4;exporter.shouldOptimizeForNetworkUse=true
let done=DispatchSemaphore(value:0);exporter.exportAsynchronously {done.signal()}
guard done.wait(timeout:.now()+110) == .success,exporter.status == .completed else {exporter.cancelExport();fail("MP4 export failed")}
let result=AVURLAsset(url:target)
let info:[String:Any] = ["duration_seconds":CMTimeGetSeconds(result.duration),"audio_tracks":result.tracks(withMediaType:.audio).count]
print(String(data:try JSONSerialization.data(withJSONObject:info),encoding:.utf8)!)
