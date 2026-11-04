-- herdr: follows the Herdr layout on screen (see lua/herdr_theme.lua).
package.loaded.herdr_theme = nil
local t = require 'herdr_theme'
t.apply()
t.watch()
