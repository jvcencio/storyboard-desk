import Foundation
import AVFoundation
struct Segment:Decodable {let path:String;let start:Double;let end:Double}
struct Request:Decodable {let segments:[Segment];let width:Double;let height:Double}
func fail(_ value:String)->Never {fputs(value+"\n",stderr);exit(1)}
guard CommandLine.arguments.count==3 else {fail("Manifest and output are required")}
let request=try JSONDecoder().decode(Request.self,from:Data(contentsOf:URL(fileURLWithPath:CommandLine.arguments[1])))
guard !request.segments.isEmpty else {fail("Choose footage for the reel")}
let composition=AVMutableComposition()
guard let track=composition.addMutableTrack(withMediaType:.video,preferredTrackID:kCMPersistentTrackID_Invalid) else {fail("Video track unavailable")}
var cursor=CMTime.zero;var instructions=[AVMutableVideoCompositionInstruction]()
for segment in request.segments {
 let asset=AVURLAsset(url:URL(fileURLWithPath:segment.path))
 guard segment.start>=0,segment.end>segment.start,segment.end<=CMTimeGetSeconds(asset.duration)+0.001,let source=asset.tracks(withMediaType:.video).first else {fail("Invalid source range")}
 let duration=CMTime(seconds:segment.end-segment.start,preferredTimescale:60000)
 try track.insertTimeRange(CMTimeRange(start:CMTime(seconds:segment.start,preferredTimescale:60000),duration:duration),of:source,at:cursor)
 let bounds=CGRect(origin:.zero,size:source.naturalSize).applying(source.preferredTransform)
 guard bounds.width>0,bounds.height>0 else {fail("Source dimensions unavailable")}
 let scale=min(request.width/bounds.width,request.height/bounds.height)
 var transform=source.preferredTransform.concatenating(CGAffineTransform(translationX:-bounds.minX,y:-bounds.minY))
 transform=transform.concatenating(CGAffineTransform(scaleX:scale,y:scale)).concatenating(CGAffineTransform(translationX:(request.width-bounds.width*scale)/2,y:(request.height-bounds.height*scale)/2))
 let layer=AVMutableVideoCompositionLayerInstruction(assetTrack:track);layer.setTransform(transform,at:cursor)
 let instruction=AVMutableVideoCompositionInstruction();instruction.timeRange=CMTimeRange(start:cursor,duration:duration);instruction.layerInstructions=[layer];instructions.append(instruction);cursor=CMTimeAdd(cursor,duration)
}
let picture=AVMutableVideoComposition();picture.renderSize=CGSize(width:request.width,height:request.height);picture.frameDuration=CMTime(value:1,timescale:24);picture.instructions=instructions
let target=URL(fileURLWithPath:CommandLine.arguments[2])
guard !FileManager.default.fileExists(atPath:target.path),let exporter=AVAssetExportSession(asset:composition,presetName:AVAssetExportPresetHighestQuality) else {fail("Exporter unavailable")}
exporter.videoComposition=picture;exporter.outputURL=target;exporter.outputFileType = .mp4;exporter.shouldOptimizeForNetworkUse=true
let done=DispatchSemaphore(value:0);exporter.exportAsynchronously{done.signal()}
guard done.wait(timeout:.now()+230) == .success,exporter.status == .completed else {exporter.cancelExport();fail("Reel export failed")}
let result=AVURLAsset(url:target);let info:[String:Any] = ["duration_seconds":CMTimeGetSeconds(result.duration),"audio_tracks":result.tracks(withMediaType:.audio).count,"width":request.width,"height":request.height]
print(String(data:try JSONSerialization.data(withJSONObject:info),encoding:.utf8)!)
