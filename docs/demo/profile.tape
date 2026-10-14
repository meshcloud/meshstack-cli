Output docs/profile.gif

Require go
Require git

Set Shell bash
Set FontSize 17
Set Width 1100
Set Height 440
Set Padding 24
Set TypingSpeed 35ms
Set Theme "Catppuccin Mocha"

# The demo starts with no profile at all.
Hide
Type `source docs/demo/setup.sh && rm "$MESHSTACK_CONFIG_DIR/profiles.json" && clear`
Enter
Wait@2m
Show

Sleep 500ms
Type "meshstack profile"
Sleep 400ms
Enter
Wait+Screen@20s /There is no profile yet/
Sleep 1.2s

Type "a"
Wait+Screen@20s /Add a profile/
Sleep 500ms
Type "http://l"
Sleep 600ms
Tab
Sleep 800ms
Enter
Sleep 300ms
Enter
Sleep 700ms
Enter
Wait+Screen@20s /Added profile/
Sleep 900ms

Type "a"
Wait+Screen@20s /Add a profile/
Sleep 500ms
Type "https://api.meshstack.example.com"
Sleep 400ms
Enter
Sleep 300ms
Enter
Sleep 700ms
Enter
Wait+Screen@20s /Added profile/
Sleep 900ms

Up
Sleep 700ms
Down
Sleep 700ms
Type "u"
Wait+Screen@20s /is the current one/
Sleep 1s

Type "d"
Wait+Screen@20s /Delete profile/
Sleep 1s
Left
Sleep 700ms
Enter
Wait+Screen@20s /warning was logged/
Sleep 2.5s

Type "q"
Sleep 2s

Hide
Type `kill "$demo_server"; rm -rf "$demo_dir"`
Enter
