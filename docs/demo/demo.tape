Output docs/demo.gif

Require go
Require git

Set Shell bash
Set FontSize 17
Set Width 1100
Set Height 540
Set Padding 24
Set TypingSpeed 70ms
Set Theme "Catppuccin Mocha"

Hide
Type "source docs/demo/setup.sh && clear"
Enter
Wait@2m
Show

Sleep 1.5s
Type "meshstack login"
Sleep 600ms
Enter
Wait+Screen /Select a profile/
Sleep 1.5s
Down@400ms 1
Up@400ms 2
Down@400ms 2
Sleep 800ms
Enter

Wait+Screen@20s /Select a workspace/
Sleep 1.5s
Down@300ms 3
Up@300ms 1
Sleep 500ms
Type "/"
Sleep 600ms
Type@150ms "sec"
Sleep 2s
Enter
Wait+Screen@20s /logged in at meshStack/
Sleep 3s

Hide
Type "clear"
Enter
Show

Sleep 1s
Type "meshstack buildingblock list -o ndjson --limit 5"
Sleep 600ms
Enter
Wait+Screen@20s /listed the first 5 of/
Sleep 6s

Hide
Type `kill "$demo_server"; rm -rf "$demo_dir"`
Enter
