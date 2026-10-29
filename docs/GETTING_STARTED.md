# First session in Storyboard Desk

## Learn the board without an API key

Run `python3 launch.py` from the checkout and open the three-shot demo. Pick a shot and edit **Purpose**, **Composition**, **Action** and **Camera**. Save it, then insert a shot before or after it. The shot's identity stays stable when you reorder it.

Import an image as a composition candidate or finished image. These are different stages: a rough composition helps with framing; a finished image is the reviewed starter for animation. Select the take you want. Approving an image does not generate a video.

Import a short H.264 MP4 and review it under **Clips**. Save its in/out range or keep several options. **Review sequence** previews the board; **Export package** is a source/prompt handoff, not a finished combined movie.

## Try the music-video workflow

Create a project with **Music video** enabled. Use **Music & lyrics** to attach a PCM WAV and paste your lyrics. Text is retained as supplied; shot allocations and musical alignment are separate choices.

Import your clips, then open **Song sections**. You can name a section such as Chorus 1, or use **Split time range** for rough fifteen-second working portions and label them later. A time boundary is a guide you provide, not something the app inferred from the recording.

Use **Collect footage** to add source clips from **Media Library**. The coverage bin holds alternatives independently from the optional rough assembly. Edit an option to set its preferred range, angle, musical tag and notes. Reservation is optional; it can keep a clip exclusive to one section, but is never applied automatically.

Choose **Preferred source ranges** and export a selects reel. You get one silent MP4 plus source-cut maps. For example, collect 24 seconds for a 15-second chorus, then split and trim it in iMovie. The app does not fit the footage to fifteen seconds for you. Keep the original song as your soundtrack.

The separate **Export editor package** action can include individual silent source ranges, handles, song/cue metadata and Final Cut XML. Actual import behavior depends on your editor; the simple MP4 reel is the most direct route for the external editing workflow.

## Generate an image, then a video

1. Start a session with your own key using `python3 launch.py --ask-openrouter-key`. No key is saved by this command.
2. In **Generate**, check the OpenRouter connection and review the production brief. Set a modest project allowance.
3. Choose a shot and an image model. Attach actual project reference images; a text description or web URL alone does not condition identity.
4. Edit the image prompt. Review its exact references and reservation, then confirm that one request.
5. Inspect the returned image in the shot editor. Select and approve it only if its identity, pose, props and framing are useful.
6. Prepare a video from that starter. Edit the motion prompt around one simple visible action. Video requests use audio off.
7. Review the quote, confirm, and wait. A new clip arrives as an unselected option. Inspect it before collecting a preferred range into a section.

Look at the whole take. A good starter does not guarantee stable hands, props, faces or endings. Trim before drift or reference-sheet tails. Different angles and takes can help the edit without requiring a perfect match.

## Common questions

**Where are my projects?** Windows: `%LOCALAPPDATA%\Storyboard Desk`; Mac: `~/Library/Application Support/Storyboard Desk`; Linux: `~/.local/share/storyboard-desk` (or your XDG data home). The app shows the active data path. The Git checkout is separate from project storage.

**Why is video export unavailable?** Install FFmpeg and ffprobe on PATH. On Mac, you can alternatively run `python3 build_native.py` after installing Apple Command Line Tools. See [platform setup](PORTABILITY.md).

**Why does Draw locally need extra setup?** It is a separate optional Draw Things / model / Mini Meter route. Imported media and cloud generation do not depend on it.

**Why did an image need approval again?** Approval is tied to the selected image and visual context. Changing shared references can invalidate existing approvals. Reinspect the same image before renewing approval; this step itself does not spend credits.

**Did a failed request cost money?** A missing reported cost means unsettled, not free. Its reservation stays held. Inspect provider activity and the existing job before considering another paid request.

**Can I use this as an editor?** SBD plans, organizes, generates and prepares footage. The currently qualified music-video workflow finishes the edit in iMovie or another editor. Automatic audio synchronization, a complete editing timeline and publishing are future work.
