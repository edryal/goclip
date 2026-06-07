### Dependencies:

- gtk4
- cliphist
- wl-clipboard

Add `goclip` to your `$GOPATH/bin`:
```bash
go install .
```

### Hyprland:

Autostart `goclip` like this:
```lua
hl.on("hyprland.start", function()
    ...
	hl.exec_cmd("goclip")
    ...
end)
```

And then toggle it using a keybind:
```lua
hl.bind("F2", hl.dsp.exec_cmd("goclip --toggle"))
```

Add this to your windowrules to make `goclip` appear floating on your cursor instead of as a tiled window:
```lua
hl.window_rule({
	name = "spawn-goclip-on-cursor",
	match = { class = "^(com.github.edryal.goclip)$" },
	pin = true,
	float = true,
	border_size = 0,
	move = { "cursor_x - 30", "cursor_y - 20" },
})
```
