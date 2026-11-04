-- Herdr Desktop typing sounds: while Herdr is focused, each key press appends a
-- byte to $XDG_RUNTIME_DIR/herdr-keys; ~/.local/bin/herdr-keyclick plays it.
-- Binds are non-consuming, so keys still reach the app. Mute: SUPER ALT + K.
local trigger = (os.getenv("XDG_RUNTIME_DIR") or "/tmp") .. "/herdr-keys"

local function tap(kind)
  return function()
    local w = hl.get_active_window()
    if not (w and w.class == "herdr-app") then return end
    local f = io.open(trigger, "ab")
    if f then f:write(kind); f:close() end
  end
end

local opts = { non_consuming = true, ignore_mods = true, repeating = true }
local keys = {}
for c in ("abcdefghijklmnopqrstuvwxyz0123456789"):gmatch(".") do keys[#keys + 1] = c end
for _, k in ipairs({ "TAB", "MINUS", "EQUAL", "BRACKETLEFT", "BRACKETRIGHT", "SEMICOLON",
  "APOSTROPHE", "GRAVE", "COMMA", "PERIOD", "SLASH", "BACKSLASH", "UP", "DOWN", "LEFT", "RIGHT" }) do
  keys[#keys + 1] = k
end
for _, k in ipairs(keys) do hl.bind(k, tap("k"), opts) end
hl.bind("SPACE", tap("s"), opts)
hl.bind("RETURN", tap("e"), opts)
hl.bind("BACKSPACE", tap("b"), opts)

o.bind("SUPER + ALT + K", "Herdr typing sounds on/off", "herdr-typing-toggle")
