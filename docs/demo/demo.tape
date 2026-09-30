Output docs/demo.gif

Require go
Require git

Set Shell bash
Set FontSize 16
Set Width 1100
Set Height 720
Set Padding 24
Set TypingSpeed 40ms
Set Theme "Catppuccin Mocha"

Hide
Type "source docs/demo/setup.sh && clear"
Enter
Wait@2m
Show

Sleep 800ms
Type "meshstack login"
Sleep 300ms
Enter
Wait+Screen /Select a profile/
Sleep 1s
Down@300ms 1
Up@300ms 2
Down@300ms 2
Sleep 400ms
Enter

Wait+Screen@20s /Select a workspace/
Sleep 1s
Down@200ms 3
Up@200ms 1
Sleep 300ms
Type "/"
Sleep 300ms
Type@100ms "sec"
Sleep 1.2s
Enter
Wait+Screen@20s /Token expires/
Sleep 2s

Hide
Type "clear"
Enter
Show

Sleep 500ms
Type "meshstack buildingblock list -o ndjson --limit 5"
Sleep 300ms
Enter
Wait+Screen@20s /listed the first 5 of/
Sleep 3s

Hide
Type "clear"
Enter
Show

Sleep 500ms
Type "meshstack api '/api/meshobjects/meshbuildingblocks?size=1'"
Sleep 300ms
Enter
Wait+Screen@20s /"totalPages"/
Sleep 2.5s

# Ctrl+L clears the screen and, unlike clear, leaves the api command the last one of the history.
Ctrl+L
Sleep 300ms
Up
Sleep 500ms
Ctrl+A
Right@15ms 13
Sleep 300ms
Type@120ms "-docs"
Sleep 500ms
Enter
Wait@20s
Sleep 2.5s

Ctrl+L
Sleep 300ms
Type "meshstack api-docs --describe bb.list"
Sleep 300ms
Enter
Wait@20s
Sleep 2s

Hide
Type "clear"
Enter
Show

Sleep 500ms
Type "meshstack profile"
Sleep 300ms
Enter
Wait+Screen@20s /meshStack CLI profiles/
Sleep 1s
Up@300ms 2
Down@300ms 2
Sleep 300ms
Enter
Wait+Screen@20s /Edit profile/
Sleep 1.8s
Escape
Sleep 500ms
Type "q"

Hide
Type "clear"
Enter
Show

Sleep 500ms
Type "meshstack profile add"
Sleep 300ms
Enter
Wait+Screen@20s /Add a profile/
Sleep 600ms
Type "https://api.s"
Sleep 500ms
Tab
Sleep 600ms
Enter
Sleep 500ms
Type@100ms "pay"
Sleep 600ms
Tab
Sleep 600ms
Enter
Sleep 1s
Enter
Sleep 2s

Hide
Type `kill "$demo_server"; rm -rf "$demo_dir"`
Enter
